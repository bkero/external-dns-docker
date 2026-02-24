// Package controller implements the DNS reconciliation loop.
package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bkero/external-dns-docker/pkg/plan"
	"github.com/bkero/external-dns-docker/pkg/provider"
	"github.com/bkero/external-dns-docker/pkg/source"
)

// Config holds controller tuning parameters.
type Config struct {
	// Interval is the periodic reconciliation interval. Default: 60s.
	Interval time.Duration
	// DebounceDuration is the quiet period after a Docker event before
	// reconciliation is triggered. Default: 5s.
	DebounceDuration time.Duration
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

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	// Run one initial reconciliation synchronously before entering the loop.
	if err := c.reconcile(ctx); err != nil {
		c.log.Error("reconciliation failed", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.reconcile(ctx); err != nil {
				c.log.Error("reconciliation failed", "err", err)
			}
		case <-reconcileCh:
			if err := c.reconcile(ctx); err != nil {
				c.log.Error("reconciliation failed", "err", err)
			}
		}
	}
}

// reconcile executes one full fetch → diff → apply cycle.
func (c *Controller) reconcile(ctx context.Context) (retErr error) {
	defer func() {
		if retErr == nil {
			c.ready.Store(true)
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
		return fmt.Errorf("apply changes: %w", err)
	}
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
