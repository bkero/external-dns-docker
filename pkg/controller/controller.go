// Package controller implements the DNS reconciliation loop.
package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/bkero/external-dns-docker/pkg/plan"
	"github.com/bkero/external-dns-docker/pkg/provider"
	"github.com/bkero/external-dns-docker/pkg/source"
)

// Prometheus metrics registered on the default registry.
var (
	reconciliationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "external_dns_docker_reconciliations_total",
		Help: "Total number of reconciliation cycles by result.",
	}, []string{"result"})

	reconciliationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "external_dns_docker_reconciliation_duration_seconds",
		Help:    "Duration of reconciliation cycles in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	recordsManaged = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "external_dns_docker_records_managed",
		Help: "Current number of DNS records managed by this instance.",
	})

	dnsOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "external_dns_docker_dns_operations_total",
		Help: "Total number of DNS operations by type and result.",
	}, []string{"op", "result"})
)

// Config holds controller tuning parameters.
type Config struct {
	// Interval is the periodic reconciliation interval. Default: 60s.
	Interval time.Duration
	// DebounceDuration is the quiet period after a Docker event before
	// reconciliation is triggered. Default: 5s.
	DebounceDuration time.Duration
	// BackoffBase is the starting duration for exponential backoff on
	// consecutive reconciliation failures. Default: 5s.
	BackoffBase time.Duration
	// BackoffMax is the ceiling for exponential backoff. Default: 5m.
	BackoffMax time.Duration
	// DryRun logs planned changes without calling ApplyChanges.
	DryRun bool
	// Once causes the controller to run exactly one reconciliation cycle then exit.
	Once bool
	// OwnerID is the ownership identifier written to TXT records.
	// Uses plan.DefaultOwnerID if empty.
	OwnerID string
}

// applyDefaults fills in zero-value fields with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 60 * time.Second
	}
	if c.DebounceDuration <= 0 {
		c.DebounceDuration = 5 * time.Second
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 5 * time.Second
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 5 * time.Minute
	}
}

// Controller orchestrates periodic and event-driven DNS reconciliation.
type Controller struct {
	source   source.Source
	provider provider.Provider
	plan     *plan.Plan
	log      *slog.Logger
	cfg      Config
	ready    atomic.Bool // set true after first successful reconcile
}

// IsReady reports whether at least one reconciliation cycle has completed successfully.
// Used by the health server to gate the readiness endpoint.
func (c *Controller) IsReady() bool {
	return c.ready.Load()
}

// backoffDuration returns the backoff duration for the nth consecutive failure.
// It doubles with each failure, capped at BackoffMax.
func (c *Controller) backoffDuration(consecutiveErrors int) time.Duration {
	shift := consecutiveErrors - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 20 { // cap shift to prevent overflow: 2^20 > 1M
		shift = 20
	}
	d := c.cfg.BackoffBase * time.Duration(1<<uint(shift))
	if d > c.cfg.BackoffMax {
		d = c.cfg.BackoffMax
	}
	return d
}

// New returns a Controller wired with the given source, provider, and config.
func New(src source.Source, prov provider.Provider, log *slog.Logger, cfg Config) *Controller {
	cfg.applyDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &Controller{
		source:   src,
		provider: prov,
		plan:     plan.New(cfg.OwnerID),
		log:      log,
		cfg:      cfg,
	}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
// When cfg.Once is true it runs a single cycle and returns immediately.
func (c *Controller) Run(ctx context.Context) error {
	if c.cfg.Once {
		return c.reconcile(ctx)
	}

	// reconcileCh is signalled by the debounce timer after Docker events.
	reconcileCh := make(chan struct{}, 1)

	var (
		mu            sync.Mutex
		debounceTimer *time.Timer
	)

	// Register the event handler; each event resets the debounce timer.
	c.source.AddEventHandler(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(c.cfg.DebounceDuration, func() {
			select {
			case reconcileCh <- struct{}{}:
			default:
			}
		})
	})

	// Timer-based scheduler: fires immediately for the first reconcile, then
	// resets to cfg.Interval on success or a computed backoff on failure.
	nextTimer := time.NewTimer(0)
	defer nextTimer.Stop()

	consecutiveErrors := 0

	// doReconcile runs one cycle and schedules the next tick.
	doReconcile := func() {
		if err := c.reconcile(ctx); err != nil {
			c.log.Error("reconciliation failed", "err", err)
			consecutiveErrors++
			b := c.backoffDuration(consecutiveErrors)
			c.log.Warn("backing off before next reconciliation",
				"backoff", b.String(), "consecutive_errors", consecutiveErrors)
			nextTimer.Reset(b)
		} else {
			consecutiveErrors = 0
			nextTimer.Reset(c.cfg.Interval)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-nextTimer.C:
			doReconcile()
		case <-reconcileCh:
			// Docker event: reconcile immediately, cancelling any pending tick.
			nextTimer.Stop()
			doReconcile()
		}
	}
}

// reconcile executes one full fetch → diff → apply cycle.
func (c *Controller) reconcile(ctx context.Context) (retErr error) {
	start := time.Now()
	defer func() {
		reconciliationDuration.Observe(time.Since(start).Seconds())
		if retErr == nil {
			reconciliationsTotal.WithLabelValues("success").Inc()
			c.ready.Store(true)
		} else {
			reconciliationsTotal.WithLabelValues("error").Inc()
		}
	}()

	desired, err := c.source.Endpoints(ctx)
	if err != nil {
		return fmt.Errorf("fetch desired endpoints: %w", err)
	}

	current, err := c.provider.Records(ctx)
	if err != nil {
		return fmt.Errorf("fetch current records: %w", err)
	}

	changes := c.plan.Calculate(desired, current)

	// Update the records-managed gauge to reflect current desired state.
	recordsManaged.Set(float64(len(desired)))

	if changes.IsEmpty() {
		c.log.Debug("reconcile: no changes")
		return nil
	}

	c.log.Info("reconcile: planned changes",
		"create", len(changes.Create),
		"update", len(changes.UpdateOld),
		"delete", len(changes.Delete),
	)

	if c.cfg.DryRun {
		c.log.Info("reconcile: dry-run enabled, skipping apply")
		logChanges(c.log, changes)
		return nil
	}

	if err := c.provider.ApplyChanges(ctx, changes); err != nil {
		dnsOperationsTotal.WithLabelValues("create", "error").Add(float64(len(changes.Create)))
		dnsOperationsTotal.WithLabelValues("update", "error").Add(float64(len(changes.UpdateNew)))
		dnsOperationsTotal.WithLabelValues("delete", "error").Add(float64(len(changes.Delete)))
		return fmt.Errorf("apply changes: %w", err)
	}

	dnsOperationsTotal.WithLabelValues("create", "success").Add(float64(len(changes.Create)))
	dnsOperationsTotal.WithLabelValues("update", "success").Add(float64(len(changes.UpdateNew)))
	dnsOperationsTotal.WithLabelValues("delete", "success").Add(float64(len(changes.Delete)))

	c.log.Info("reconcile: changes applied")
	return nil
}

// logChanges logs the planned changes at INFO level for dry-run inspection.
func logChanges(log *slog.Logger, changes *plan.Changes) {
	for _, ep := range changes.Create {
		log.Info("dry-run: would create",
			"name", ep.DNSName, "type", ep.RecordType, "targets", ep.Targets)
	}
	for i, old := range changes.UpdateOld {
		if i < len(changes.UpdateNew) {
			log.Info("dry-run: would update",
				"name", old.DNSName, "type", old.RecordType,
				"old_targets", old.Targets, "new_targets", changes.UpdateNew[i].Targets,
			)
		}
	}
	for _, ep := range changes.Delete {
		log.Info("dry-run: would delete",
			"name", ep.DNSName, "type", ep.RecordType, "targets", ep.Targets)
	}
}
