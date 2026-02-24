// Command external-dns-docker watches Docker containers and manages DNS records
// via an RFC2136-compatible server based on container labels.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	dockerclient "github.com/docker/docker/client"

	"github.com/bkero/external-dns-docker/pkg/controller"
	"github.com/bkero/external-dns-docker/pkg/provider/rfc2136"
	"github.com/bkero/external-dns-docker/pkg/source"
)

func main() {
	// ---- RFC2136 provider flags ----
	rfc2136Host := flag.String("rfc2136-host",
		envOr("EXTERNAL_DNS_RFC2136_HOST", ""),
		"RFC2136 DNS server host (required)")
	rfc2136Port := flag.Int("rfc2136-port",
		envOrInt("EXTERNAL_DNS_RFC2136_PORT", 53),
		"RFC2136 DNS server port")
	rfc2136Zone := flag.String("rfc2136-zone",
		envOr("EXTERNAL_DNS_RFC2136_ZONE", ""),
		"DNS zone to manage (required)")
	rfc2136TSIGKey := flag.String("rfc2136-tsig-key",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_KEY", ""),
		"TSIG key name")
	rfc2136TSIGSecret := flag.String("rfc2136-tsig-secret",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_SECRET", ""),
		"TSIG secret (base64-encoded)")
	rfc2136TSIGAlg := flag.String("rfc2136-tsig-alg",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_ALG", "hmac-sha256"),
		"TSIG algorithm (e.g. hmac-sha256, hmac-sha512)")
	rfc2136MinTTL := flag.Int64("rfc2136-min-ttl",
		envOrInt64("EXTERNAL_DNS_RFC2136_MIN_TTL", 0),
		"Minimum TTL enforced on all DNS records (0 = disabled)")
	rfc2136Timeout := flag.Duration("rfc2136-timeout",
		envOrDuration("EXTERNAL_DNS_RFC2136_TIMEOUT", 10*time.Second),
		"Timeout for RFC2136 DNS operations (AXFR and UPDATE)")

	// ---- Docker source flags ----
	dockerHost := flag.String("docker-host",
		envOr("EXTERNAL_DNS_DOCKER_HOST", ""),
		"Docker daemon address (e.g. unix:///var/run/docker.sock, tcp://host:2376)")
	dockerTLSCA := flag.String("docker-tls-ca",
		envOr("EXTERNAL_DNS_DOCKER_TLS_CA", ""),
		"Path to Docker CA certificate for TLS connections")
	dockerTLSCert := flag.String("docker-tls-cert",
		envOr("EXTERNAL_DNS_DOCKER_TLS_CERT", ""),
		"Path to Docker client TLS certificate")
	dockerTLSKey := flag.String("docker-tls-key",
		envOr("EXTERNAL_DNS_DOCKER_TLS_KEY", ""),
		"Path to Docker client TLS key")

	// ---- Controller flags ----
	interval := flag.Duration("interval",
		envOrDuration("EXTERNAL_DNS_INTERVAL", 60*time.Second),
		"Periodic reconciliation interval")
	debounce := flag.Duration("debounce",
		envOrDuration("EXTERNAL_DNS_DEBOUNCE", 5*time.Second),
		"Event debounce duration (quiet period after Docker events before reconciling)")
	once := flag.Bool("once",
		envOrBool("EXTERNAL_DNS_ONCE", false),
		"Run exactly one reconciliation cycle and exit")
	dryRun := flag.Bool("dry-run",
		envOrBool("EXTERNAL_DNS_DRY_RUN", false),
		"Log planned DNS changes without applying them")
	ownerID := flag.String("owner-id",
		envOr("EXTERNAL_DNS_OWNER_ID", ""),
		"Ownership identifier written to TXT records (default: external-dns-docker)")

	// ---- Health check flags ----
	healthPort := flag.Int("health-port",
		envOrInt("EXTERNAL_DNS_HEALTH_PORT", 8080),
		"Port for the HTTP health check server (0 to disable)")

	// ---- Shutdown flags ----
	shutdownTimeout := flag.Duration("shutdown-timeout",
		envOrDuration("EXTERNAL_DNS_SHUTDOWN_TIMEOUT", 30*time.Second),
		"Maximum time to wait for graceful shutdown after SIGTERM")

	// ---- Logging flags ----
	logLevel := flag.String("log-level",
		envOr("EXTERNAL_DNS_LOG_LEVEL", "info"),
		"Log level: debug, info, warn, error")

	flag.Parse()

	log := newLogger(*logLevel)

	// ---- Validate required configuration ----
	if *rfc2136Host == "" {
		log.Error("--rfc2136-host is required (or set EXTERNAL_DNS_RFC2136_HOST)")
		os.Exit(1)
	}
	if *rfc2136Zone == "" {
		log.Error("--rfc2136-zone is required (or set EXTERNAL_DNS_RFC2136_ZONE)")
		os.Exit(1)
	}

	// ---- Build Docker source ----
	var dockerOpts []dockerclient.Opt
	if *dockerHost != "" {
		dockerOpts = append(dockerOpts, dockerclient.WithHost(*dockerHost))
	}
	if *dockerTLSCert != "" || *dockerTLSKey != "" || *dockerTLSCA != "" {
		dockerOpts = append(dockerOpts,
			dockerclient.WithTLSClientConfig(*dockerTLSCA, *dockerTLSCert, *dockerTLSKey))
	}

	src, err := source.NewDockerSource(log, dockerOpts...)
	if err != nil {
		log.Error("failed to create Docker source", "err", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := src.Close(); cerr != nil {
			log.Warn("error closing Docker client", "err", cerr)
		}
	}()

	// ---- Build RFC2136 provider ----
	prov := rfc2136.New(rfc2136.Config{
		Host:          *rfc2136Host,
		Port:          *rfc2136Port,
		Zone:          *rfc2136Zone,
		TSIGKeyName:   *rfc2136TSIGKey,
		TSIGSecret:    *rfc2136TSIGSecret,
		TSIGSecretAlg: *rfc2136TSIGAlg,
		MinTTL:        *rfc2136MinTTL,
		Timeout:       *rfc2136Timeout,
	}, log)

	// ---- Build controller ----
	ctrl := controller.New(src, prov, log, controller.Config{
		Interval:         *interval,
		DebounceDuration: *debounce,
		DryRun:           *dryRun,
		Once:             *once,
		OwnerID:          *ownerID,
	})

	// ---- Graceful shutdown ----
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// ---- Health check server ----
	startHealthServer(ctx, *healthPort, ctrl, log)

	// Start the Docker event watcher in the background (not needed for once mode).
	var watchWg sync.WaitGroup
	if !*once {
		watchWg.Add(1)
		go func() {
			defer watchWg.Done()
			src.Watch(ctx)
		}()
	}

	// ---- Run ----
	log.Info("starting external-dns-docker",
		"rfc2136-host", *rfc2136Host,
		"rfc2136-zone", *rfc2136Zone,
		"interval", interval.String(),
		"dry-run", *dryRun,
		"once", *once,
	)

	if err := ctrl.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("controller exited with error", "err", err)
		os.Exit(1)
	}

	// Wait for the Watch goroutine to exit, bounded by the shutdown timeout.
	watchDone := make(chan struct{})
	go func() {
		watchWg.Wait()
		close(watchDone)
	}()
	select {
	case <-watchDone:
		log.Info("shutdown complete")
	case <-time.After(*shutdownTimeout):
		log.Warn("shutdown timeout exceeded, forcing exit", "timeout", shutdownTimeout.String())
	}
}

// startHealthServer starts an HTTP server exposing /healthz (liveness) and
// /readyz (readiness) on the given port. A port of 0 disables the server.
// The server is shut down gracefully when ctx is cancelled.
func startHealthServer(ctx context.Context, port int, ctrl *controller.Controller, log *slog.Logger) {
	if port == 0 {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ctrl.IsReady() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ready")
		}
	})
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warn("health server shutdown error", "err", err)
		}
	}()
	go func() {
		log.Info("health server listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server error", "err", err)
		}
	}()
}

// newLogger returns a JSON logger writing to stderr at the given level.
func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

// envOr returns the value of the environment variable named key, or fallback
// if the variable is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrInt returns the environment variable named key parsed as int, or fallback.
func envOrInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// envOrInt64 returns the environment variable named key parsed as int64, or fallback.
func envOrInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// envOrBool returns the environment variable named key parsed as bool, or fallback.
func envOrBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// envOrDuration returns the environment variable named key parsed as
// time.Duration, or fallback.
func envOrDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
