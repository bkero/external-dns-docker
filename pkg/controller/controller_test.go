package controller

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
	fake_provider "github.com/bkero/external-dns-docker/pkg/provider/fake"
	fake_source "github.com/bkero/external-dns-docker/pkg/source/fake"
)

// helpers

func ep(name, target string) *endpoint.Endpoint {
	return endpoint.New(name, []string{target}, endpoint.RecordTypeA, 300, nil)
}

// ownerTXT returns the ownership sidecar TXT endpoint for the given DNS name
// using the default owner ID. Mirrors what plan.ownershipTXTFor produces.
func ownerTXT(name string) *endpoint.Endpoint {
	return endpoint.New(
		"external-dns-docker-owner."+name,
		[]string{"heritage=external-dns-docker,external-dns-docker/owner=external-dns-docker"},
		endpoint.RecordTypeTXT,
		300,
		nil,
	)
}

// errSource is a Source that always errors on Endpoints.
type errSource struct {
	err error
}

func (e *errSource) Endpoints(_ context.Context) ([]*endpoint.Endpoint, error) {
	return nil, e.err
}
func (e *errSource) AddEventHandler(_ context.Context, _ func()) {}

// errRecordsProvider is a Provider whose Records call always errors.
type errRecordsProvider struct {
	err error
}

func (p *errRecordsProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	return nil, p.err
}
func (p *errRecordsProvider) ApplyChanges(_ context.Context, _ *plan.Changes) error {
	return nil
}

// errApplyProvider is a Provider whose ApplyChanges call always errors.
type errApplyProvider struct {
	err error
}

func (p *errApplyProvider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	return nil, nil
}
func (p *errApplyProvider) ApplyChanges(_ context.Context, _ *plan.Changes) error {
	return p.err
}

// --- Prometheus metrics ---

func TestReconcile_MetricsIncrementOnSuccess(t *testing.T) {
	before := testutil.ToFloat64(reconciliationsTotal.WithLabelValues("success"))

	src := fake_source.New([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	after := testutil.ToFloat64(reconciliationsTotal.WithLabelValues("success"))
	if after <= before {
		t.Errorf("reconciliations_total{result=success} did not increment: before=%v after=%v", before, after)
	}
}

func TestReconcile_MetricsIncrementOnError(t *testing.T) {
	before := testutil.ToFloat64(reconciliationsTotal.WithLabelValues("error"))

	src := &errSource{err: errors.New("docker unavailable")}
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})
	_ = c.Run(context.Background())

	after := testutil.ToFloat64(reconciliationsTotal.WithLabelValues("error"))
	if after <= before {
		t.Errorf("reconciliations_total{result=error} did not increment: before=%v after=%v", before, after)
	}
}

func TestReconcile_RecordsManagedGauge(t *testing.T) {
	src := fake_source.New([]*endpoint.Endpoint{
		ep("a.example.com", "1.1.1.1"),
		ep("b.example.com", "2.2.2.2"),
	})
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	got := testutil.ToFloat64(recordsManaged)
	if got != 2 {
		t.Errorf("records_managed = %v, want 2", got)
	}
}

// --- applyDefaults ---

func TestApplyDefaults_FillsZeroValues(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", cfg.Interval)
	}
	if cfg.DebounceDuration != 5*time.Second {
		t.Errorf("DebounceDuration = %v, want 5s", cfg.DebounceDuration)
	}
	if cfg.BackoffBase != 5*time.Second {
		t.Errorf("BackoffBase = %v, want 5s", cfg.BackoffBase)
	}
	if cfg.BackoffMax != 5*time.Minute {
		t.Errorf("BackoffMax = %v, want 5m", cfg.BackoffMax)
	}
}

func TestApplyDefaults_PreservesNonZero(t *testing.T) {
	cfg := Config{
		Interval:         30 * time.Second,
		DebounceDuration: 2 * time.Second,
		BackoffBase:      1 * time.Second,
		BackoffMax:       1 * time.Minute,
	}
	cfg.applyDefaults()
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", cfg.Interval)
	}
	if cfg.DebounceDuration != 2*time.Second {
		t.Errorf("DebounceDuration = %v, want 2s", cfg.DebounceDuration)
	}
	if cfg.BackoffBase != 1*time.Second {
		t.Errorf("BackoffBase = %v, want 1s", cfg.BackoffBase)
	}
	if cfg.BackoffMax != 1*time.Minute {
		t.Errorf("BackoffMax = %v, want 1m", cfg.BackoffMax)
	}
}

// --- backoffDuration ---

func TestBackoffDuration_FirstFailure(t *testing.T) {
	c := New(fake_source.New(nil), fake_provider.New(nil), slog.Default(), Config{
		BackoffBase: 5 * time.Second,
		BackoffMax:  5 * time.Minute,
	})
	if got := c.backoffDuration(1); got != 5*time.Second {
		t.Errorf("backoffDuration(1) = %v, want 5s", got)
	}
}

func TestBackoffDuration_Doubles(t *testing.T) {
	c := New(fake_source.New(nil), fake_provider.New(nil), slog.Default(), Config{
		BackoffBase: 5 * time.Second,
		BackoffMax:  5 * time.Minute,
	})
	if got := c.backoffDuration(2); got != 10*time.Second {
		t.Errorf("backoffDuration(2) = %v, want 10s", got)
	}
	if got := c.backoffDuration(3); got != 20*time.Second {
		t.Errorf("backoffDuration(3) = %v, want 20s", got)
	}
}

func TestBackoffDuration_CapsAtMax(t *testing.T) {
	c := New(fake_source.New(nil), fake_provider.New(nil), slog.Default(), Config{
		BackoffBase: 5 * time.Second,
		BackoffMax:  5 * time.Minute,
	})
	got := c.backoffDuration(100)
	if got != 5*time.Minute {
		t.Errorf("backoffDuration(100) = %v, want 5m (max)", got)
	}
}

func TestRun_BackoffResetOnSuccess(t *testing.T) {
	// errSource fails once then succeeds; verify the loop continues to run
	// reconciles (i.e. it didn't get stuck waiting on a long backoff).
	callCount := 0
	src := &countingErrSource{failFirst: 1, onCall: func() { callCount++ }}
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{
		Interval:    5 * time.Millisecond,
		BackoffBase: 1 * time.Millisecond, // tiny backoff for test speed
		BackoffMax:  10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	// Allow time for initial fail + backoff + success + second interval + success
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-errCh

	if callCount < 2 {
		t.Errorf("expected at least 2 reconcile calls (fail then succeed), got %d", callCount)
	}
}

// countingErrSource fails its first N Endpoints calls then succeeds.
type countingErrSource struct {
	failFirst int
	calls     int
	onCall    func()
}

func (s *countingErrSource) Endpoints(_ context.Context) ([]*endpoint.Endpoint, error) {
	s.calls++
	if s.onCall != nil {
		s.onCall()
	}
	if s.calls <= s.failFirst {
		return nil, errors.New("transient error")
	}
	return nil, nil
}

func (s *countingErrSource) AddEventHandler(_ context.Context, _ func()) {}

// --- Once mode ---

func TestRun_OnceMode_CreatesRecords(t *testing.T) {
	src := fake_source.New([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	hist := prov.History()
	if len(hist) != 1 {
		t.Fatalf("expected 1 apply call, got %d", len(hist))
	}
	if len(hist[0].Create) == 0 {
		t.Error("expected creates in apply call")
	}
}

func TestRun_OnceMode_NoChanges(t *testing.T) {
	// desired == current (owned) → no changes → ApplyChanges not called.
	src := fake_source.New([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	prov := fake_provider.New([]*endpoint.Endpoint{
		ep("app.example.com", "1.2.3.4"),
		ownerTXT("app.example.com"),
	})
	c := New(src, prov, slog.Default(), Config{Once: true})

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(prov.History()) != 0 {
		t.Errorf("expected 0 apply calls for no-op, got %d", len(prov.History()))
	}
}

func TestRun_OnceMode_SourceError(t *testing.T) {
	src := &errSource{err: errors.New("docker unavailable")}
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRun_OnceMode_ProviderRecordsError(t *testing.T) {
	src := fake_source.New(nil)
	c := New(src, nil, slog.Default(), Config{Once: true})
	c.provider = &errRecordsProvider{err: errors.New("dns timeout")}

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error from provider.Records, got nil")
	}
}

func TestRun_OnceMode_ApplyChangesError(t *testing.T) {
	// desired has a record not in current → plan produces creates → ApplyChanges errors.
	src := fake_source.New([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	prov := &errApplyProvider{err: errors.New("nxdomain")}
	c := New(src, prov, slog.Default(), Config{Once: true})

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error from ApplyChanges, got nil")
	}
}

// --- Dry-run mode ---

func TestRun_DryRun_SkipsApply(t *testing.T) {
	src := fake_source.New([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true, DryRun: true})

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(prov.History()) != 0 {
		t.Errorf("expected 0 apply calls in dry-run, got %d", len(prov.History()))
	}
}

func TestRun_DryRun_LogsAllChangeTypes(t *testing.T) {
	// create: new.example.com (not in current)
	// update: upd.example.com (target differs)
	// delete: del.example.com (in current+owned, absent from desired)
	src := fake_source.New([]*endpoint.Endpoint{
		ep("new.example.com", "1.1.1.1"),
		ep("upd.example.com", "9.9.9.9"),
	})
	prov := fake_provider.New([]*endpoint.Endpoint{
		ep("upd.example.com", "1.2.3.4"),
		ownerTXT("upd.example.com"),
		ep("del.example.com", "4.4.4.4"),
		ownerTXT("del.example.com"),
	})
	c := New(src, prov, slog.Default(), Config{Once: true, DryRun: true})

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	// Dry-run must not call ApplyChanges.
	if len(prov.History()) != 0 {
		t.Errorf("expected 0 apply calls in dry-run, got %d", len(prov.History()))
	}
}

// --- Loop mode ---

func TestRun_ContextCancellation_ReturnsContextCanceled(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{
		Interval:         5 * time.Millisecond,
		DebounceDuration: 1 * time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRun_EventTriggeredReconcile(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{
		Interval:         1 * time.Hour, // disable periodic tick
		DebounceDuration: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	// Allow the initial no-op reconcile and handler registration to complete.
	time.Sleep(10 * time.Millisecond)

	// Load an endpoint and fire a Docker event.
	src.SetEndpoints([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	src.TriggerEvent()

	// Wait for the debounce timer (20ms) and a buffer.
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-errCh

	if len(prov.History()) == 0 {
		t.Error("expected at least one apply call from event-triggered reconcile")
	}
}

func TestRun_DebounceMultipleEvents(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{
		Interval:         1 * time.Hour,
		DebounceDuration: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	// Wait for the initial no-op reconcile.
	time.Sleep(10 * time.Millisecond)

	// Trigger 3 events within the 50ms debounce window.
	src.SetEndpoints([]*endpoint.Endpoint{ep("app.example.com", "1.2.3.4")})
	src.TriggerEvent()
	time.Sleep(10 * time.Millisecond)
	src.TriggerEvent()
	time.Sleep(10 * time.Millisecond)
	src.TriggerEvent()

	// Wait for the debounce timer to fire (50ms from last event) plus headroom.
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-errCh

	hist := prov.History()
	if len(hist) != 1 {
		t.Errorf("expected exactly 1 apply call (debounced), got %d", len(hist))
	}
}

func TestRun_LoopContinuesOnTransientError(t *testing.T) {
	// Source errors on every Endpoints call; loop must continue and return
	// context.Canceled when the context is cancelled.
	src := &errSource{err: errors.New("transient")}
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{
		Interval:         5 * time.Millisecond,
		DebounceDuration: 1 * time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	time.Sleep(40 * time.Millisecond)
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled after transient errors, got %v", err)
	}
}

// --- nil log branch in New ---

func TestNew_NilLog_UsesDefault(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	// Pass nil logger — New should fall back to slog.Default() without panicking.
	c := New(src, prov, nil, Config{Once: true})
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run with nil logger: %v", err)
	}
}

// --- IsReady ---

func TestIsReady_FalseBeforeFirstReconcile(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})
	if c.IsReady() {
		t.Error("IsReady() = true before first reconcile, want false")
	}
}

func TestIsReady_TrueAfterSuccessfulReconcile(t *testing.T) {
	src := fake_source.New(nil)
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !c.IsReady() {
		t.Error("IsReady() = false after successful reconcile, want true")
	}
}

func TestIsReady_FalseAfterFailedReconcile(t *testing.T) {
	src := &errSource{err: errors.New("docker unavailable")}
	prov := fake_provider.New(nil)
	c := New(src, prov, slog.Default(), Config{Once: true})

	_ = c.Run(context.Background()) // expect error; ignore it
	if c.IsReady() {
		t.Error("IsReady() = true after failed reconcile, want false")
	}
}

// --- reconcileCh error path ---

func TestRun_EventReconcileError_LogsAndContinues(t *testing.T) {
	// Provider.Records always errors; when a Docker event fires and the
	// debounce timer expires, the reconcile triggered via reconcileCh must
	// log the error and keep the loop running (context.Canceled on exit).
	src := fake_source.New(nil)
	errProv := &errRecordsProvider{err: errors.New("dns timeout")}
	c := New(src, errProv, slog.Default(), Config{
		Interval:         1 * time.Hour,
		DebounceDuration: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	// Let the (failing) initial reconcile complete, then trigger an event so
	// the debounce fires and the reconcileCh error branch is exercised.
	time.Sleep(20 * time.Millisecond)
	src.TriggerEvent()

	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
