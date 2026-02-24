package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---- newLogger ----

func TestNewLogger_Levels(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"", slog.LevelInfo},      // unknown → default info
		{"trace", slog.LevelInfo}, // unrecognised → default info
	}
	for _, tt := range tests {
		log := newLogger(tt.input)
		if log == nil {
			t.Errorf("newLogger(%q) returned nil", tt.input)
		}
		if !log.Enabled(context.TODO(), tt.want) {
			t.Errorf("newLogger(%q): level %v not enabled", tt.input, tt.want)
		}
		// One level above the configured level must not be enabled (except error,
		// which is the maximum standard level).
		if tt.want < slog.LevelError && log.Enabled(context.TODO(), tt.want-1) {
			t.Errorf("newLogger(%q): level below threshold (%v) should not be enabled", tt.input, tt.want-1)
		}
	}
}

// ---- envOr ----

func TestEnvOr_Unset_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_OR_UNSET", "")
	if got := envOr("TEST_ENV_OR_UNSET", "default"); got != "default" {
		t.Errorf("got %q, want %q", got, "default")
	}
}

func TestEnvOr_Set_ReturnsValue(t *testing.T) {
	t.Setenv("TEST_ENV_OR_SET", "hello")
	if got := envOr("TEST_ENV_OR_SET", "default"); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

// ---- envOrInt ----

func TestEnvOrInt_Unset_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_INT_UNSET", "")
	if got := envOrInt("TEST_ENV_INT_UNSET", 42); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestEnvOrInt_Valid_ReturnsParsed(t *testing.T) {
	t.Setenv("TEST_ENV_INT_VALID", "99")
	if got := envOrInt("TEST_ENV_INT_VALID", 0); got != 99 {
		t.Errorf("got %d, want 99", got)
	}
}

func TestEnvOrInt_Invalid_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_INT_INVALID", "notanumber")
	if got := envOrInt("TEST_ENV_INT_INVALID", 7); got != 7 {
		t.Errorf("got %d, want 7 (fallback)", got)
	}
}

// ---- envOrInt64 ----

func TestEnvOrInt64_Unset_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_INT64_UNSET", "")
	if got := envOrInt64("TEST_ENV_INT64_UNSET", 300); got != 300 {
		t.Errorf("got %d, want 300", got)
	}
}

func TestEnvOrInt64_Valid_ReturnsParsed(t *testing.T) {
	t.Setenv("TEST_ENV_INT64_VALID", "3600")
	if got := envOrInt64("TEST_ENV_INT64_VALID", 0); got != 3600 {
		t.Errorf("got %d, want 3600", got)
	}
}

func TestEnvOrInt64_Invalid_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_INT64_INVALID", "bad")
	if got := envOrInt64("TEST_ENV_INT64_INVALID", 5); got != 5 {
		t.Errorf("got %d, want 5 (fallback)", got)
	}
}

// ---- envOrBool ----

func TestEnvOrBool_Unset_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_BOOL_UNSET", "")
	if got := envOrBool("TEST_ENV_BOOL_UNSET", true); got != true {
		t.Errorf("got %v, want true", got)
	}
}

func TestEnvOrBool_True_ReturnsParsed(t *testing.T) {
	t.Setenv("TEST_ENV_BOOL_TRUE", "true")
	if got := envOrBool("TEST_ENV_BOOL_TRUE", false); got != true {
		t.Errorf("got %v, want true", got)
	}
}

func TestEnvOrBool_False_ReturnsParsed(t *testing.T) {
	t.Setenv("TEST_ENV_BOOL_FALSE", "false")
	if got := envOrBool("TEST_ENV_BOOL_FALSE", true); got != false {
		t.Errorf("got %v, want false", got)
	}
}

func TestEnvOrBool_Invalid_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_BOOL_INVALID", "yes-please")
	if got := envOrBool("TEST_ENV_BOOL_INVALID", true); got != true {
		t.Errorf("got %v, want true (fallback)", got)
	}
}

// ---- envOrDuration ----

func TestEnvOrDuration_Unset_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_DUR_UNSET", "")
	if got := envOrDuration("TEST_ENV_DUR_UNSET", 60*time.Second); got != 60*time.Second {
		t.Errorf("got %v, want 60s", got)
	}
}

func TestEnvOrDuration_Valid_ReturnsParsed(t *testing.T) {
	t.Setenv("TEST_ENV_DUR_VALID", "30s")
	if got := envOrDuration("TEST_ENV_DUR_VALID", 0); got != 30*time.Second {
		t.Errorf("got %v, want 30s", got)
	}
}

func TestEnvOrDuration_Invalid_ReturnsFallback(t *testing.T) {
	t.Setenv("TEST_ENV_DUR_INVALID", "forever")
	if got := envOrDuration("TEST_ENV_DUR_INVALID", 5*time.Minute); got != 5*time.Minute {
		t.Errorf("got %v, want 5m (fallback)", got)
	}
}

// ---- loadZoneConfigsFromEnv ----

// clearZoneEnv removes any leftover EXTERNAL_DNS_RFC2136_ZONE_* vars from the
// test process environment before each test.
func clearZoneEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		k, _, _ := splitKV(kv)
		if len(k) > len("EXTERNAL_DNS_RFC2136_ZONE_") && k[:len("EXTERNAL_DNS_RFC2136_ZONE_")] == "EXTERNAL_DNS_RFC2136_ZONE_" {
			t.Setenv(k, "") // t.Setenv registers cleanup; set empty to suppress detection
		}
	}
}

func splitKV(kv string) (string, string, bool) {
	for i, c := range kv {
		if c == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}

func TestLoadZoneConfigsFromEnv_NoVars_ReturnsFalse(t *testing.T) {
	clearZoneEnv(t)
	configs, active, err := loadZoneConfigsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Error("expected active=false when no matching env vars are set")
	}
	if len(configs) != 0 {
		t.Errorf("got %d configs, want 0", len(configs))
	}
}

func TestLoadZoneConfigsFromEnv_TwoZones_DetectedAndGrouped(t *testing.T) {
	clearZoneEnv(t)
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_BKERO_HOST", "ns2.bke.ro")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_BKERO_ZONE", "bke.ro.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_BKERO_TSIG_KEY", "bke-key")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_BKERO_TSIG_SECRET", "c2VjcmV0Mg==")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST", "ns1.example.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_ZONE", "example.com.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_TSIG_KEY", "prod-key")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_TSIG_SECRET", "c2VjcmV0")

	configs, active, err := loadZoneConfigsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Error("expected active=true")
	}
	if len(configs) != 2 {
		t.Fatalf("got %d configs, want 2", len(configs))
	}
	// Alphabetical ordering: BKERO < PROD
	if configs[0].Zone != "bke.ro." {
		t.Errorf("configs[0].Zone = %q, want bke.ro. (alphabetical first)", configs[0].Zone)
	}
	if configs[1].Zone != "example.com." {
		t.Errorf("configs[1].Zone = %q, want example.com.", configs[1].Zone)
	}
}

func TestLoadZoneConfigsFromEnv_AlphabeticalOrdering(t *testing.T) {
	clearZoneEnv(t)
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_ZZZ_HOST", "ns3.zzz.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_ZZZ_ZONE", "zzz.com.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_AAA_HOST", "ns1.aaa.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_AAA_ZONE", "aaa.com.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_MMM_HOST", "ns2.mmm.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_MMM_ZONE", "mmm.com.")

	configs, active, err := loadZoneConfigsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Error("expected active=true")
	}
	if len(configs) != 3 {
		t.Fatalf("got %d configs, want 3", len(configs))
	}
	want := []string{"aaa.com.", "mmm.com.", "zzz.com."}
	for i, zc := range configs {
		if zc.Zone != want[i] {
			t.Errorf("configs[%d].Zone = %q, want %q", i, zc.Zone, want[i])
		}
	}
}

func TestLoadZoneConfigsFromEnv_MissingHost_ReturnsError(t *testing.T) {
	clearZoneEnv(t)
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_ZONE", "example.com.")
	// HOST deliberately omitted

	_, _, err := loadZoneConfigsFromEnv()
	if err == nil {
		t.Error("expected error for missing HOST, got nil")
	}
}

func TestLoadZoneConfigsFromEnv_MissingZone_ReturnsError(t *testing.T) {
	clearZoneEnv(t)
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST", "ns1.example.com")
	// ZONE deliberately omitted

	_, _, err := loadZoneConfigsFromEnv()
	if err == nil {
		t.Error("expected error for missing ZONE, got nil")
	}
}

func TestLoadZoneConfigsFromEnv_TSIGSecretFile_Resolved(t *testing.T) {
	clearZoneEnv(t)
	secret := "base64secretvalue"
	f, err := os.CreateTemp(t.TempDir(), "tsig")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(secret + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_TEST_HOST", "ns1.example.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_TEST_ZONE", "example.com.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_TEST_TSIG_SECRET_FILE", f.Name())

	configs, active, err := loadZoneConfigsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active || len(configs) != 1 {
		t.Fatalf("active=%v, len=%d; want active=true, len=1", active, len(configs))
	}
	if configs[0].TSIGSecret != secret {
		t.Errorf("TSIGSecret = %q, want %q", configs[0].TSIGSecret, secret)
	}
	if configs[0].TSIGSecretFile != "" {
		t.Error("TSIGSecretFile should be cleared after resolution")
	}
}

func TestLoadZoneConfigsFromEnv_UnknownFieldSuffix_Ignored(t *testing.T) {
	clearZoneEnv(t)
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST", "ns1.example.com")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_ZONE", "example.com.")
	t.Setenv("EXTERNAL_DNS_RFC2136_ZONE_PROD_UNKNOWN_FIELD", "somevalue") // should be ignored

	configs, active, err := loadZoneConfigsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error for unknown field: %v", err)
	}
	if !active || len(configs) != 1 {
		t.Fatalf("active=%v, len=%d; want active=true, len=1", active, len(configs))
	}
}

// ---- loadZoneConfigsFromFile ----

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "zones.yaml")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadZoneConfigsFromFile_TwoZones(t *testing.T) {
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
    zone: example.com.
    tsig-key: prod-key
    tsig-secret: c2VjcmV0
    tsig-alg: hmac-sha256
  - host: ns2.bke.ro
    zone: bke.ro.
    tsig-key: bke-key
    tsig-secret: c2VjcmV0Mg==
`)
	configs, err := loadZoneConfigsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("got %d configs, want 2", len(configs))
	}
	if configs[0].Host != "ns1.example.com" {
		t.Errorf("configs[0].Host = %q, want ns1.example.com", configs[0].Host)
	}
	if configs[1].Zone != "bke.ro." {
		t.Errorf("configs[1].Zone = %q, want bke.ro.", configs[1].Zone)
	}
}

func TestLoadZoneConfigsFromFile_MissingHost_ReturnsError(t *testing.T) {
	path := writeYAML(t, `
zones:
  - zone: example.com.
`)
	_, err := loadZoneConfigsFromFile(path)
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

func TestLoadZoneConfigsFromFile_MissingZone_ReturnsError(t *testing.T) {
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
`)
	_, err := loadZoneConfigsFromFile(path)
	if err == nil {
		t.Error("expected error for missing zone, got nil")
	}
}

func TestLoadZoneConfigsFromFile_BothSecretFields_ReturnsError(t *testing.T) {
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
    zone: example.com.
    tsig-secret: c2VjcmV0
    tsig-secret-file: /run/secrets/tsig
`)
	_, err := loadZoneConfigsFromFile(path)
	if err == nil {
		t.Error("expected error for both tsig-secret and tsig-secret-file, got nil")
	}
}

func TestLoadZoneConfigsFromFile_TSIGSecretFile_Resolved(t *testing.T) {
	secret := "mybase64secret"
	sf := filepath.Join(t.TempDir(), "tsig.secret")
	if err := os.WriteFile(sf, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
    zone: example.com.
    tsig-key: k
    tsig-secret-file: `+sf+`
`)
	configs, err := loadZoneConfigsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("got %d configs, want 1", len(configs))
	}
	if configs[0].TSIGSecret != secret {
		t.Errorf("TSIGSecret = %q, want %q", configs[0].TSIGSecret, secret)
	}
}

func TestLoadZoneConfigsFromFile_InvalidTimeout_ReturnsError(t *testing.T) {
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
    zone: example.com.
    timeout: "not-a-duration"
`)
	_, err := loadZoneConfigsFromFile(path)
	if err == nil {
		t.Error("expected error for invalid timeout, got nil")
	}
}

func TestLoadZoneConfigsFromFile_Timeout_Parsed(t *testing.T) {
	path := writeYAML(t, `
zones:
  - host: ns1.example.com
    zone: example.com.
    timeout: "5s"
`)
	configs, err := loadZoneConfigsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configs[0].Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", configs[0].Timeout)
	}
}

func TestLoadZoneConfigsFromFile_FileNotFound_ReturnsError(t *testing.T) {
	_, err := loadZoneConfigsFromFile("/nonexistent/path/zones.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
