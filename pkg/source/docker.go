package source

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

const (
	labelPrefix     = "external-dns.io/"
	labelHostname   = labelPrefix + "hostname"
	labelTarget     = labelPrefix + "target"
	labelTTL        = labelPrefix + "ttl"
	labelRecordType = labelPrefix + "record-type"
)

// dockerAPI is the subset of the Docker client used by DockerSource.
// Defined as an interface so tests can inject a mock.
type dockerAPI interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
}

// DockerSource implements Source by watching the Docker daemon.
type DockerSource struct {
	client        dockerAPI
	log           *slog.Logger
	handlers      []func()
	reconnectWait time.Duration // how long to wait between reconnect attempts
}

// NewDockerSource returns a DockerSource that connects via the environment
// (DOCKER_HOST, DOCKER_TLS_VERIFY, etc.) or the default Unix socket.
// Additional dockerclient.Opt values are appended after the defaults and
// override env-based settings where they conflict (e.g. WithHost overrides
// DOCKER_HOST).
func NewDockerSource(log *slog.Logger, extraOpts ...dockerclient.Opt) (*DockerSource, error) {
	opts := []dockerclient.Opt{
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	}
	opts = append(opts, extraOpts...)
	c, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &DockerSource{client: c, log: log, reconnectWait: 5 * time.Second}, nil
}

// newDockerSourceWithClient constructs a DockerSource with an injected client
// for unit testing.
func newDockerSourceWithClient(client dockerAPI, log *slog.Logger) *DockerSource {
	if log == nil {
		log = slog.Default()
	}
	return &DockerSource{client: client, log: log, reconnectWait: 0}
}

// Endpoints lists running containers and extracts DNS endpoints from their labels.
func (s *DockerSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	containers, err := s.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	var eps []*endpoint.Endpoint
	for _, c := range containers {
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		eps = append(eps, s.endpointsFromLabels(id, c.Labels)...)
	}
	return eps, nil
}

// AddEventHandler registers a function called when a relevant Docker event occurs.
func (s *DockerSource) AddEventHandler(_ context.Context, handler func()) {
	s.handlers = append(s.handlers, handler)
}

// Watch subscribes to Docker Events and calls registered handlers on container
// lifecycle events. Reconnects automatically on stream errors. Blocks until ctx
// is cancelled.
func (s *DockerSource) Watch(ctx context.Context) {
	for {
		s.runEventLoop(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.reconnectWait):
			s.log.Warn("reconnecting to Docker event stream")
		}
	}
}

func (s *DockerSource) runEventLoop(ctx context.Context) {
	f := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "start"),
		filters.Arg("event", "stop"),
		filters.Arg("event", "die"),
		filters.Arg("event", "update"),
	)
	msgs, errs := s.client.Events(ctx, events.ListOptions{Filters: f})
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errs:
			if err != nil {
				s.log.Warn("docker event stream error", "err", err)
			}
			return
		case <-msgs:
			s.notify()
		}
	}
}

func (s *DockerSource) notify() {
	for _, h := range s.handlers {
		h()
	}
}

// endpointsFromLabels parses DNS labels from a container's label map.
// containerID is used only for log messages.
func (s *DockerSource) endpointsFromLabels(containerID string, labels map[string]string) []*endpoint.Endpoint {
	var eps []*endpoint.Endpoint

	// Non-indexed single record.
	if hostname, ok := labels[labelHostname]; ok {
		if ep := s.parseSingle(containerID, hostname, labels[labelTarget], labels[labelTTL], labels[labelRecordType]); ep != nil {
			eps = append(eps, ep)
		}
	}

	// Indexed records: external-dns.io/hostname-0, external-dns.io/target-0, …
	for i := 0; ; i++ {
		hostnameKey := fmt.Sprintf("%shostname-%d", labelPrefix, i)
		hostname, ok := labels[hostnameKey]
		if !ok {
			break
		}
		targetKey := fmt.Sprintf("%starget-%d", labelPrefix, i)
		ttlKey := fmt.Sprintf("%sttl-%d", labelPrefix, i)
		rtKey := fmt.Sprintf("%srecord-type-%d", labelPrefix, i)
		if ep := s.parseSingle(containerID, hostname, labels[targetKey], labels[ttlKey], labels[rtKey]); ep != nil {
			eps = append(eps, ep)
		}
	}

	return eps
}

// parseSingle builds one Endpoint from raw label strings.
// Returns nil and logs a warning when required labels are absent or invalid.
func (s *DockerSource) parseSingle(containerID, hostname, target, rawTTL, rawRecordType string) *endpoint.Endpoint {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil
	}

	target = strings.TrimSpace(target)
	if target == "" {
		s.log.Warn("container missing target label, skipping",
			"container", containerID, "hostname", hostname)
		return nil
	}

	ttl := endpoint.DefaultTTL
	if rawTTL != "" {
		v, err := strconv.ParseInt(strings.TrimSpace(rawTTL), 10, 64)
		if err != nil || v < 0 {
			s.log.Warn("container has invalid TTL, skipping",
				"container", containerID, "hostname", hostname, "ttl", rawTTL)
			return nil
		}
		ttl = v
	}

	recordType := strings.TrimSpace(rawRecordType)
	if recordType == "" {
		recordType = endpoint.InferRecordType(target)
	}

	return endpoint.New(hostname, []string{target}, recordType, ttl, nil)
}
