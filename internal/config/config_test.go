package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeEnv turns a map into the lookup function that load() expects.
func fakeEnv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := load(fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "local" || cfg.HTTPAddr != ":8080" || cfg.LogFormat != "json" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %v, want 5s", cfg.RequestTimeout)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := load(fakeEnv(map[string]string{
		"APP_ENV":         "PROD", // case-insensitive
		"HTTP_ADDR":       "127.0.0.1:9090",
		"LOG_LEVEL":       "debug",
		"LOG_FORMAT":      "text",
		"REQUEST_TIMEOUT": "2s",
		"SHUTDOWN_DELAY":  "5s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "prod" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.LogFormat != "text" {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.RequestTimeout != 2*time.Second || cfg.ShutdownDelay != 5*time.Second {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_ReportsAllErrorsAtOnce(t *testing.T) {
	_, err := load(fakeEnv(map[string]string{
		"APP_ENV":         "staging",
		"LOG_LEVEL":       "loud",
		"REQUEST_TIMEOUT": "five seconds",
		"IDLE_TIMEOUT":    "0s",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"APP_ENV", "LOG_LEVEL", "REQUEST_TIMEOUT", "IDLE_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s; got:\n%v", want, err)
		}
	}
}

func TestLoad_WriteTimeoutMustExceedRequestTimeout(t *testing.T) {
	_, err := load(fakeEnv(map[string]string{
		"REQUEST_TIMEOUT": "10s",
		"WRITE_TIMEOUT":   "10s",
	}))
	if err == nil || !strings.Contains(err.Error(), "WRITE_TIMEOUT") {
		t.Fatalf("expected WRITE_TIMEOUT error, got %v", err)
	}
}
