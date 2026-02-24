package source

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
)

// mockDockerClient implements dockerAPI for tests.
type mockDockerClient struct {
	containers []container.Summary
	listErr    error
	// eventCh and errCh are returned by Events(). Tests send on them to simulate events.
	eventCh chan events.Message
	errCh   chan error
}

func newMockClient(containers []container.Summary) *mockDockerClient {
	return &mockDockerClient{
		containers: containers,
		eventCh:    make(chan events.Message, 10),
		errCh:      make(chan error, 1),
	}
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
	return m.containers, m.listErr
}

func (m *mockDockerClient) Events(_ context.Context, _ events.ListOptions) (<-chan events.Message, <-chan error) {
	return m.eventCh, m.errCh
}

func newTestSource(containers []container.Summary) (*DockerSource, *mockDockerClient) {
	mock := newMockClient(containers)
	log := slog.Default()
	src := newDockerSourceWithClient(mock, log)
	return src, mock
}

// --- Endpoint discovery tests ---

func TestDockerSource_ValidLabels(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "10.0.0.1",
			},
		},
	})

	eps, err := src.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].DNSName != "app.example.com" {
		t.Errorf("DNSName = %q, want app.example.com", eps[0].DNSName)
	}
	if len(eps[0].Targets) != 1 || eps[0].Targets[0] != "10.0.0.1" {
		t.Errorf("Targets = %v, want [10.0.0.1]", eps[0].Targets)
	}
	if eps[0].RecordType != "A" {
		t.Errorf("RecordType = %q, want A", eps[0].RecordType)
	}
	if eps[0].TTL != 300 {
		t.Errorf("TTL = %d, want 300", eps[0].TTL)
	}
}

func TestDockerSource_NoHostnameLabel_Skipped(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID:     "abc123",
			Labels: map[string]string{"external-dns.io/target": "10.0.0.1"},
		},
	})

	eps, err := src.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0", len(eps))
	}
}

func TestDockerSource_NoTargetLabel_SkippedWithWarning(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID:     "abc123",
			Labels: map[string]string{"external-dns.io/hostname": "app.example.com"},
		},
	})

	eps, err := src.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (no target)", len(eps))
	}
}

func TestDockerSource_TTLLabel(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "10.0.0.1",
				"external-dns.io/ttl":      "3600",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].TTL != 3600 {
		t.Errorf("TTL = %d, want 3600", eps[0].TTL)
	}
}

func TestDockerSource_InvalidTTL_Skipped(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "10.0.0.1",
				"external-dns.io/ttl":      "bad",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (invalid TTL)", len(eps))
	}
}

func TestDockerSource_NegativeTTL_Skipped(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "10.0.0.1",
				"external-dns.io/ttl":      "-1",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (negative TTL)", len(eps))
	}
}

func TestDockerSource_RecordTypeOverride(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname":    "app.example.com",
				"external-dns.io/target":      "10.0.0.1",
				"external-dns.io/record-type": "CNAME",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].RecordType != "CNAME" {
		t.Errorf("RecordType = %q, want CNAME", eps[0].RecordType)
	}
}

func TestDockerSource_IPv6Target_AAAA(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "2001:db8::1",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].RecordType != "AAAA" {
		t.Errorf("RecordType = %q, want AAAA", eps[0].RecordType)
	}
}

func TestDockerSource_HostnameTarget_CNAME(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "backend.internal",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].RecordType != "CNAME" {
		t.Errorf("RecordType = %q, want CNAME", eps[0].RecordType)
	}
}

// --- Multi-record indexed label tests ---

func TestDockerSource_IndexedLabels_MultipleEndpoints(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname-0": "a.example.com",
				"external-dns.io/target-0":   "1.2.3.4",
				"external-dns.io/hostname-1": "b.example.com",
				"external-dns.io/target-1":   "5.6.7.8",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}
	names := map[string]bool{eps[0].DNSName: true, eps[1].DNSName: true}
	if !names["a.example.com"] || !names["b.example.com"] {
		t.Errorf("got DNS names %v, want a.example.com and b.example.com", names)
	}
}

func TestDockerSource_IndexedLabel_MissingTarget_Skipped(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname-0": "a.example.com",
				// no target-0
				"external-dns.io/hostname-1": "b.example.com",
				"external-dns.io/target-1":   "5.6.7.8",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	// index 0 skipped (no target), index 1 valid
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].DNSName != "b.example.com" {
		t.Errorf("DNSName = %q, want b.example.com", eps[0].DNSName)
	}
}

func TestDockerSource_NonIndexedAndIndexed_Coexist(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname":   "main.example.com",
				"external-dns.io/target":     "1.1.1.1",
				"external-dns.io/hostname-0": "extra.example.com",
				"external-dns.io/target-0":   "2.2.2.2",
			},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}
}

func TestDockerSource_NoLabels_NoEndpoints(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{ID: "abc123", Labels: map[string]string{}},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0", len(eps))
	}
}

func TestDockerSource_MultipleContainers(t *testing.T) {
	src, _ := newTestSource([]container.Summary{
		{
			ID:     "aaa",
			Labels: map[string]string{"external-dns.io/hostname": "a.example.com", "external-dns.io/target": "1.1.1.1"},
		},
		{
			ID:     "bbb",
			Labels: map[string]string{}, // no DNS labels
		},
		{
			ID:     "ccc",
			Labels: map[string]string{"external-dns.io/hostname": "c.example.com", "external-dns.io/target": "3.3.3.3"},
		},
	})

	eps, _ := src.Endpoints(context.Background())
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}
}

// --- Event handler tests ---

func TestDockerSource_EventTriggers_Handler(t *testing.T) {
	src, mock := newTestSource(nil)

	called := 0
	src.AddEventHandler(context.Background(), func() { called++ })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		src.runEventLoop(ctx)
		close(done)
	}()

	// Send a container start event.
	mock.eventCh <- events.Message{Type: "container", Action: "start"}

	// Give the goroutine time to dequeue and process the event before we
	// cancel the context (eventCh is buffered, so the send returns immediately).
	time.Sleep(20 * time.Millisecond)

	cancel()
	<-done

	if called == 0 {
		t.Error("event handler was not called after Docker event")
	}
}

func TestDockerSource_StreamError_ExitsLoop(t *testing.T) {
	src, mock := newTestSource(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		src.runEventLoop(ctx)
		close(done)
	}()

	// Inject an error to terminate the loop.
	mock.errCh <- context.Canceled

	select {
	case <-done:
		// loop exited as expected
	case <-time.After(time.Second):
		t.Fatal("event loop did not exit after stream error")
	}
}

func TestDockerSource_Watch_ReconnectsAfterStreamError(t *testing.T) {
	// First call returns a stream that immediately errors; second call blocks
	// until the context is cancelled.
	firstErrCh := make(chan error, 1)
	firstErrCh <- context.Canceled // triggers immediate reconnect

	blockCh := make(chan events.Message)
	blockErrCh := make(chan error)
	reconnected := make(chan struct{}, 1) // signalled when second Events call happens

	mock := &reconnectMockClient{
		firstErrCh:  firstErrCh,
		blockCh:     blockCh,
		blockErrCh:  blockErrCh,
		reconnected: reconnected,
	}

	src := newDockerSourceWithClient(mock, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		src.Watch(ctx) // reconnectWait=0, so reconnect is immediate
		close(done)
	}()

	// Wait until Watch has made the second Events call (i.e. reconnected).
	select {
	case <-reconnected:
	case <-time.After(time.Second):
		t.Fatal("Watch did not reconnect within 1s")
	}

	cancel()
	<-done
}

// reconnectMockClient returns an erroring stream on the first Events call,
// then a blocking stream on subsequent calls.
type reconnectMockClient struct {
	firstErrCh  chan error
	blockCh     chan events.Message
	blockErrCh  chan error
	reconnected chan struct{} // receives when second (reconnect) call happens
	calls       int           // only written from within Watch goroutine
}

func (m *reconnectMockClient) ContainerList(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
	return nil, nil
}

func (m *reconnectMockClient) Events(_ context.Context, _ events.ListOptions) (<-chan events.Message, <-chan error) {
	m.calls++
	if m.calls == 1 {
		msgCh := make(chan events.Message)
		return msgCh, m.firstErrCh
	}
	// Signal that we've reconnected (non-blocking to avoid stalling Watch).
	select {
	case m.reconnected <- struct{}{}:
	default:
	}
	return m.blockCh, m.blockErrCh
}

func TestDockerSource_AddEventHandler_FiltersNotApplied(t *testing.T) {
	// Verify that NewArgs builds a valid filter (smoke test — actual filtering
	// is done server-side; we just confirm the construction doesn't panic).
	f := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "start"),
	)
	if f.Len() == 0 {
		t.Error("expected non-empty filters")
	}
}

// --- NewDockerSource / newDockerSourceWithClient coverage ---

func TestNewDockerSource_Default(t *testing.T) {
	// NewDockerSource with nil log should succeed (Docker client creation does
	// not require a running daemon; it just wires up the client struct).
	src, err := NewDockerSource(nil)
	if err != nil {
		t.Fatalf("NewDockerSource() unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil DockerSource")
	}
}

func TestNewDockerSource_BadOpt_ReturnsError(t *testing.T) {
	// An extra Opt that always returns an error must cause NewDockerSource to
	// fail — covers the error-return branch inside NewDockerSource.
	badOpt := func(*dockerclient.Client) error {
		return fmt.Errorf("injected opt error")
	}
	_, err := NewDockerSource(nil, badOpt)
	if err == nil {
		t.Error("expected error from bad extra opt, got nil")
	}
}

func TestNewDockerSourceWithClient_NilLog_UsesDefault(t *testing.T) {
	mock := newMockClient(nil)
	src := newDockerSourceWithClient(mock, nil)
	if src.log == nil {
		t.Error("expected non-nil logger when nil is passed")
	}
}

// --- Endpoints error path ---

func TestDockerSource_Endpoints_ListError(t *testing.T) {
	mock := &mockDockerClient{
		listErr: fmt.Errorf("docker socket unavailable"),
		eventCh: make(chan events.Message, 10),
		errCh:   make(chan error, 1),
	}
	src := newDockerSourceWithClient(mock, slog.Default())
	_, err := src.Endpoints(context.Background())
	if err == nil {
		t.Error("expected error from Endpoints when ContainerList fails")
	}
}

// --- ID truncation path ---

func TestDockerSource_LongContainerID_Truncated(t *testing.T) {
	// Container IDs > 12 chars are truncated for log messages; the endpoint
	// still uses the hostname label, not the ID.
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abcdef1234567890", // 16 chars — triggers the len(id) > 12 branch
			Labels: map[string]string{
				"external-dns.io/hostname": "app.example.com",
				"external-dns.io/target":   "10.0.0.1",
			},
		},
	})

	eps, err := src.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].DNSName != "app.example.com" {
		t.Errorf("DNSName = %q, want app.example.com", eps[0].DNSName)
	}
}

// --- parseSingle empty hostname path ---

func TestDockerSource_WhitespaceHostname_Skipped(t *testing.T) {
	// A hostname label that is whitespace-only is trimmed to "" and skipped.
	src, _ := newTestSource([]container.Summary{
		{
			ID: "abc123",
			Labels: map[string]string{
				"external-dns.io/hostname": "   ",
				"external-dns.io/target":   "10.0.0.1",
			},
		},
	})

	eps, err := src.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (whitespace hostname)", len(eps))
	}
}
