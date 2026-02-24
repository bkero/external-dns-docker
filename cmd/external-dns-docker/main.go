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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.yaml.in/yaml/v2"

	"github.com/bkero/external-dns-docker/pkg/controller"
	"github.com/bkero/external-dns-docker/pkg/provider"
	"github.com/bkero/external-dns-docker/pkg/provider/rfc2136"
	"github.com/bkero/external-dns-docker/pkg/source"
)

// preflightProvider is satisfied by both *rfc2136.Provider and *rfc2136.MultiProvider.
type preflightProvider interface {
	Preflight(ctx context.Context) error
}

func main() {
	// ---- RFC2136 provider flags (Mode 1: single-zone) ----
	rfc2136Host := flag.String("rfc2136-host",
		envOr("EXTERNAL_DNS_RFC2136_HOST", ""),
		"RFC2136 DNS server host (single-zone mode)")
	rfc2136Port := flag.Int("rfc2136-port",
		envOrInt("EXTERNAL_DNS_RFC2136_PORT", 53),
		"RFC2136 DNS server port")
	rfc2136Zone := flag.String("rfc2136-zone",
		envOr("EXTERNAL_DNS_RFC2136_ZONE", ""),
		"DNS zone to manage (single-zone mode)")
	rfc2136TSIGKey := flag.String("rfc2136-tsig-key",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_KEY", ""),
		"TSIG key name")
	rfc2136TSIGSecret := flag.String("rfc2136-tsig-secret",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_SECRET", ""),
		"TSIG secret (base64-encoded); mutually exclusive with --rfc2136-tsig-secret-file")
	rfc2136TSIGSecretFile := flag.String("rfc2136-tsig-secret-file",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_SECRET_FILE", ""),
		"Path to file containing base64-encoded TSIG secret; mutually exclusive with --rfc2136-tsig-secret")
	rfc2136TSIGAlg := flag.String("rfc2136-tsig-alg",
		envOr("EXTERNAL_DNS_RFC2136_TSIG_ALG", "hmac-sha256"),
		"TSIG algorithm (e.g. hmac-sha256, hmac-sha512)")
	rfc2136MinTTL := flag.Int64("rfc2136-min-ttl",
		envOrInt64("EXTERNAL_DNS_RFC2136_MIN_TTL", 0),
		"Minimum TTL enforced on all DNS records (0 = disabled)")
	rfc2136Timeout := flag.Duration("rfc2136-timeout",
		envOrDuration("EXTERNAL_DNS_RFC2136_TIMEOUT", 10*time.Second),
		"Timeout for RFC2136 DNS operations (AXFR and UPDATE)")

	// ---- RFC2136 provider flags (Mode 3: YAML config file) ----
	rfc2136ConfigFile := flag.String("rfc2136-config-file",
		envOr("EXTERNAL_DNS_RFC2136_CONFIG_FILE", ""),
		"Path to YAML file defining multiple RFC2136 zones (mutually exclusive with single-zone flags)")

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

	skipPreflight := flag.Bool("skip-preflight",
		envOrBool("EXTERNAL_DNS_SKIP_PREFLIGHT", false),
		"Skip the startup DNS connectivity and TSIG credential check")

	backoffBase := flag.Duration("reconcile-backoff-base",
		envOrDuration("EXTERNAL_DNS_RECONCILE_BACKOFF_BASE", 5*time.Second),
		"Base duration for exponential backoff on consecutive reconciliation failures")
	backoffMax := flag.Duration("reconcile-backoff-max",
		envOrDuration("EXTERNAL_DNS_RECONCILE_BACKOFF_MAX", 5*time.Minute),
		"Maximum backoff duration for reconciliation failures")

	// ---- Health check flags ----
	healthPort := flag.Int("health-port",
		envOrInt("EXTERNAL_DNS_HEALTH_PORT", 8080),
		"Port for the HTTP health check server (0 to disable)")
	metricsPath := flag.String("metrics-path",
		envOr("EXTERNAL_DNS_METRICS_PATH", "/metrics"),
		"HTTP path for Prometheus metrics endpoint")

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

	// ---- Mode detection and mutual-exclusivity ----
	//
	// Priority: Mode 3 (YAML file) > Mode 2 (env prefix) > Mode 1 (single-zone flags)
	// Mixing any two modes is an error.

	singleZoneFlagsSet := *rfc2136Host != "" || *rfc2136Zone != ""

	envConfigs, envModeActive, err := loadZoneConfigsFromEnv()
	if err != nil {
		log.Error("invalid multi-zone env var configuration", "err", err)
		os.Exit(1)
	}

	var (
		prov   provider.Provider
		pfProv preflightProvider
		mode   string // for startup log
		zones  int    // for startup log (multi-zone only)
	)

	switch {
	case *rfc2136ConfigFile != "":
		// Mode 3: YAML config file
		if singleZoneFlagsSet {
			log.Error("--rfc2136-config-file is mutually exclusive with --rfc2136-host / --rfc2136-zone")
			os.Exit(1)
		}
		if envModeActive {
			log.Error("--rfc2136-config-file is mutually exclusive with EXTERNAL_DNS_RFC2136_ZONE_* env vars")
			os.Exit(1)
		}
		configs, ferr := loadZoneConfigsFromFile(*rfc2136ConfigFile)
		if ferr != nil {
			log.Error("failed to load zone config file", "path", *rfc2136ConfigFile, "err", ferr)
			os.Exit(1)
		}
		mp := rfc2136.NewMulti(configs, log)
		prov = mp
		pfProv = mp
		mode = "multi-zone (yaml-file)"
		zones = len(configs)

	case envModeActive:
		// Mode 2: environment variable prefixes
		if singleZoneFlagsSet {
			log.Error("EXTERNAL_DNS_RFC2136_ZONE_* env vars are mutually exclusive with --rfc2136-host / --rfc2136-zone")
			os.Exit(1)
		}
		mp := rfc2136.NewMulti(envConfigs, log)
		prov = mp
		pfProv = mp
		mode = "multi-zone (env-prefix)"
		zones = len(envConfigs)

	case *rfc2136Host != "" && *rfc2136Zone != "":
		// Mode 1: single-zone flags (original behaviour — fully backward compatible)
		if *rfc2136TSIGSecret != "" && *rfc2136TSIGSecretFile != "" {
			log.Error("--rfc2136-tsig-secret and --rfc2136-tsig-secret-file are mutually exclusive")
			os.Exit(1)
		}
		tsigSecret := *rfc2136TSIGSecret
		if *rfc2136TSIGSecretFile != "" {
			data, rerr := os.ReadFile(*rfc2136TSIGSecretFile)
			if rerr != nil {
				log.Error("failed to read TSIG secret file", "path", *rfc2136TSIGSecretFile, "err", rerr)
				os.Exit(1)
			}
			tsigSecret = strings.TrimSpace(string(data))
		}
		sp := rfc2136.New(rfc2136.Config{
			Host:          *rfc2136Host,
			Port:          *rfc2136Port,
			Zone:          *rfc2136Zone,
			TSIGKeyName:   *rfc2136TSIGKey,
			TSIGSecret:    tsigSecret,
			TSIGSecretAlg: *rfc2136TSIGAlg,
			MinTTL:        *rfc2136MinTTL,
			Timeout:       *rfc2136Timeout,
		}, log)
		prov = sp
		pfProv = sp
		mode = "single-zone"

	default:
		log.Error("no RFC2136 configuration provided; use --rfc2136-host/--rfc2136-zone, " +
			"EXTERNAL_DNS_RFC2136_ZONE_* env vars, or --rfc2136-config-file")
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

	// ---- Preflight DNS connectivity check ----
	if !*skipPreflight {
		preflightCtx, preflightCancel := context.WithTimeout(context.Background(), *rfc2136Timeout)
		defer preflightCancel()
		if err := pfProv.Preflight(preflightCtx); err != nil {
			log.Error("DNS preflight check failed — use --skip-preflight to bypass", "err", err)
			os.Exit(1)
		}
		log.Info("DNS preflight check passed")
	}

	// ---- Build controller ----
	ctrl := controller.New(src, prov, log, controller.Config{
		Interval:         *interval,
		DebounceDuration: *debounce,
		BackoffBase:      *backoffBase,
		BackoffMax:       *backoffMax,
		DryRun:           *dryRun,
		Once:             *once,
		OwnerID:          *ownerID,
	})

	// ---- Graceful shutdown ----
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// ---- Health check server ----
	startHealthServer(ctx, *healthPort, *metricsPath, ctrl, log)

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
	if zones > 0 {
		log.Info("starting external-dns-docker",
			"mode", mode,
			"zones", zones,
			"interval", interval.String(),
			"dry-run", *dryRun,
			"once", *once,
		)
	} else {
		log.Info("starting external-dns-docker",
			"mode", mode,
			"rfc2136-host", *rfc2136Host,
			"rfc2136-zone", *rfc2136Zone,
			"interval", interval.String(),
			"dry-run", *dryRun,
			"once", *once,
		)
	}

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

// startHealthServer starts an HTTP server exposing /healthz (liveness),
// /readyz (readiness), and a Prometheus metrics endpoint on the given port.
// A port of 0 disables the server. The server shuts down when ctx is cancelled.
func startHealthServer(ctx context.Context, port int, metricsPath string, ctrl *controller.Controller, log *slog.Logger) {
	if port == 0 {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ctrl.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready")
		}
	})
	mux.Handle(metricsPath, promhttp.Handler())
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
		log.Info("health server listening", "port", port, "metrics", metricsPath)
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

// zoneFieldSetter maps an env var suffix to a setter function for ZoneConfig.
// Longer suffixes must appear before shorter ones that are prefixes of them
// (e.g. TSIG_SECRET_FILE before TSIG_SECRET).
type zoneFieldSetter struct {
	suffix string
	set    func(zc *rfc2136.ZoneConfig, val string) error
}

var zoneFieldSetters = []zoneFieldSetter{
	{"TSIG_SECRET_FILE", func(zc *rfc2136.ZoneConfig, val string) error { zc.TSIGSecretFile = val; return nil }},
	{"TSIG_SECRET", func(zc *rfc2136.ZoneConfig, val string) error { zc.TSIGSecret = val; return nil }},
	{"TSIG_KEY", func(zc *rfc2136.ZoneConfig, val string) error { zc.TSIGKey = val; return nil }},
	{"TSIG_ALG", func(zc *rfc2136.ZoneConfig, val string) error { zc.TSIGAlg = val; return nil }},
	{"MIN_TTL", func(zc *rfc2136.ZoneConfig, val string) error {
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid MIN_TTL %q: %w", val, err)
		}
		zc.MinTTL = n
		return nil
	}},
	{"TIMEOUT", func(zc *rfc2136.ZoneConfig, val string) error {
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid TIMEOUT %q: %w", val, err)
		}
		zc.Timeout = d
		return nil
	}},
	{"HOST", func(zc *rfc2136.ZoneConfig, val string) error { zc.Host = val; return nil }},
	{"PORT", func(zc *rfc2136.ZoneConfig, val string) error {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid PORT %q: %w", val, err)
		}
		zc.Port = n
		return nil
	}},
	{"ZONE", func(zc *rfc2136.ZoneConfig, val string) error { zc.Zone = val; return nil }},
}

// loadZoneConfigsFromEnv scans os.Environ() for EXTERNAL_DNS_RFC2136_ZONE_<NAME>_<FIELD>
// variables, groups them by NAME (sorted alphabetically), resolves TSIGSecretFile,
// and validates required fields. The bool return is true when matching vars were found.
func loadZoneConfigsFromEnv() ([]rfc2136.ZoneConfig, bool, error) {
	const prefix = "EXTERNAL_DNS_RFC2136_ZONE_"
	configs := make(map[string]*rfc2136.ZoneConfig)

	for _, env := range os.Environ() {
		k, v, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]

		for _, f := range zoneFieldSetters {
			sfx := "_" + f.suffix
			if !strings.HasSuffix(rest, sfx) {
				continue
			}
			name := rest[:len(rest)-len(sfx)]
			if name == "" {
				break
			}
			if configs[name] == nil {
				configs[name] = &rfc2136.ZoneConfig{}
			}
			if serr := f.set(configs[name], v); serr != nil {
				return nil, true, fmt.Errorf("env %s: %w", k, serr)
			}
			break
		}
	}

	if len(configs) == 0 {
		return nil, false, nil
	}

	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]rfc2136.ZoneConfig, 0, len(names))
	for _, name := range names {
		zc := configs[name]
		if zc.Host == "" {
			return nil, true, fmt.Errorf("zone %s: HOST is required", name)
		}
		if zc.Zone == "" {
			return nil, true, fmt.Errorf("zone %s: ZONE is required", name)
		}
		if zc.TSIGSecretFile != "" {
			data, rerr := os.ReadFile(zc.TSIGSecretFile)
			if rerr != nil {
				return nil, true, fmt.Errorf("zone %s: reading TSIG_SECRET_FILE: %w", name, rerr)
			}
			zc.TSIGSecret = strings.TrimSpace(string(data))
			zc.TSIGSecretFile = ""
		}
		result = append(result, *zc)
	}

	return result, true, nil
}

// yamlZonesFile is the top-level structure of the YAML zone config file.
type yamlZonesFile struct {
	Zones []yamlZoneEntry `yaml:"zones"`
}

type yamlZoneEntry struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Zone           string `yaml:"zone"`
	TSIGKey        string `yaml:"tsig-key"`
	TSIGSecret     string `yaml:"tsig-secret"`
	TSIGSecretFile string `yaml:"tsig-secret-file"`
	TSIGAlg        string `yaml:"tsig-alg"`
	MinTTL         int64  `yaml:"min-ttl"`
	Timeout        string `yaml:"timeout"` // e.g. "10s"; empty = use provider default
}

// loadZoneConfigsFromFile reads a YAML zone config file, resolves secret files,
// validates required fields, and returns a slice of ZoneConfig.
func loadZoneConfigsFromFile(path string) ([]rfc2136.ZoneConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var raw yamlZonesFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	configs := make([]rfc2136.ZoneConfig, 0, len(raw.Zones))
	for i, z := range raw.Zones {
		if z.Host == "" {
			return nil, fmt.Errorf("zone[%d]: host is required", i)
		}
		if z.Zone == "" {
			return nil, fmt.Errorf("zone[%d]: zone is required", i)
		}
		if z.TSIGSecret != "" && z.TSIGSecretFile != "" {
			return nil, fmt.Errorf("zone[%d]: tsig-secret and tsig-secret-file are mutually exclusive", i)
		}

		secret := z.TSIGSecret
		if z.TSIGSecretFile != "" {
			fileData, ferr := os.ReadFile(z.TSIGSecretFile)
			if ferr != nil {
				return nil, fmt.Errorf("zone[%d]: reading tsig-secret-file: %w", i, ferr)
			}
			secret = strings.TrimSpace(string(fileData))
		}

		var timeout time.Duration
		if z.Timeout != "" {
			var terr error
			timeout, terr = time.ParseDuration(z.Timeout)
			if terr != nil {
				return nil, fmt.Errorf("zone[%d]: invalid timeout %q: %w", i, z.Timeout, terr)
			}
		}

		configs = append(configs, rfc2136.ZoneConfig{
			Host:       z.Host,
			Port:       z.Port,
			Zone:       z.Zone,
			TSIGKey:    z.TSIGKey,
			TSIGSecret: secret,
			TSIGAlg:    z.TSIGAlg,
			MinTTL:     z.MinTTL,
			Timeout:    timeout,
		})
	}

	return configs, nil
}
