package main

import (
	"log/slog"
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
		{"", slog.LevelInfo},    // unknown → default info
		{"trace", slog.LevelInfo}, // unrecognised → default info
	}
	for _, tt := range tests {
		log := newLogger(tt.input)
		if log == nil {
			t.Errorf("newLogger(%q) returned nil", tt.input)
		}
		if !log.Enabled(nil, tt.want) {
			t.Errorf("newLogger(%q): level %v not enabled", tt.input, tt.want)
		}
		// One level above the configured level must not be enabled (except error,
		// which is the maximum standard level).
		if tt.want < slog.LevelError && log.Enabled(nil, tt.want-1) {
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
