// Package config loads application settings from environment variables.
//
// Why environment variables? They are the standard way to configure a
// container (the "12-factor app" rule): the same image runs locally, in dev
// and in prod, and only the environment changes. Secrets (DB password, Redis
// token, API keys) will also arrive as env vars, injected by Kubernetes from
// AWS Secrets Manager — never baked into the image or committed to Git.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

// Config holds every setting the service needs. It is built once at startup
// and then passed (by value) to whoever needs it, so nothing reads os.Getenv
// deep inside the code — that keeps the code testable.
type Config struct {
	Env       string     // APP_ENV: local | dev | prod
	HTTPAddr  string     // HTTP_ADDR: host:port to listen on
	LogLevel  slog.Level // LOG_LEVEL: debug | info | warn | error
	LogFormat string     // LOG_FORMAT: json (machines) | text (humans)

	// RequestTimeout is the total time budget for one API request. Handlers
	// receive it through the request context, so a slow DB query is
	// cancelled instead of piling up.
	RequestTimeout time.Duration // REQUEST_TIMEOUT

	// Server-level timeouts protect against slow or malicious clients
	// (e.g. "slowloris" attacks that open connections and send bytes slowly).
	ReadHeaderTimeout time.Duration // READ_HEADER_TIMEOUT
	ReadTimeout       time.Duration // READ_TIMEOUT
	WriteTimeout      time.Duration // WRITE_TIMEOUT
	IdleTimeout       time.Duration // IDLE_TIMEOUT

	// ShutdownDelay: after SIGTERM, how long we keep serving while reporting
	// "not ready", so the load balancer stops sending us new traffic first.
	ShutdownDelay time.Duration // SHUTDOWN_DELAY
	// ShutdownTimeout: max time to let in-flight requests finish.
	ShutdownTimeout time.Duration // SHUTDOWN_TIMEOUT
}

// Load reads the real process environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

// load takes the lookup function as a parameter so tests can pass a fake
// environment instead of mutating the real one.
func load(lookup func(string) (string, bool)) (Config, error) {
	p := &parser{lookup: lookup}

	cfg := Config{
		Env:               p.oneOf("APP_ENV", "local", "local", "dev", "prod"),
		HTTPAddr:          p.str("HTTP_ADDR", ":8080"),
		LogLevel:          p.logLevel("LOG_LEVEL", "info"),
		LogFormat:         p.oneOf("LOG_FORMAT", "json", "json", "text"),
		RequestTimeout:    p.positiveDuration("REQUEST_TIMEOUT", 5*time.Second),
		ReadHeaderTimeout: p.positiveDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       p.positiveDuration("READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      p.positiveDuration("WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       p.positiveDuration("IDLE_TIMEOUT", 60*time.Second),
		ShutdownDelay:     p.duration("SHUTDOWN_DELAY", 0),
		ShutdownTimeout:   p.positiveDuration("SHUTDOWN_TIMEOUT", 20*time.Second),
	}

	// Cross-field rule: if the server's write deadline fired before the
	// request budget ran out, clients would see a dropped connection instead
	// of our clean JSON timeout error.
	if cfg.WriteTimeout <= cfg.RequestTimeout {
		p.fail(fmt.Errorf("WRITE_TIMEOUT (%s) must be greater than REQUEST_TIMEOUT (%s)",
			cfg.WriteTimeout, cfg.RequestTimeout))
	}

	// Report every problem at once, not just the first one.
	if len(p.errs) > 0 {
		return Config{}, errors.Join(p.errs...)
	}
	return cfg, nil
}

// parser collects errors instead of stopping at the first one.
type parser struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (p *parser) fail(err error) { p.errs = append(p.errs, err) }

func (p *parser) str(key, def string) string {
	if v, ok := p.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (p *parser) oneOf(key, def string, allowed ...string) string {
	v := strings.ToLower(p.str(key, def))
	if !slices.Contains(allowed, v) {
		p.fail(fmt.Errorf("%s=%q: must be one of %s", key, v, strings.Join(allowed, ", ")))
	}
	return v
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	raw := p.str(key, "")
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		p.fail(fmt.Errorf("%s=%q: must be a non-negative duration such as 500ms, 5s or 1m", key, raw))
		return def
	}
	return d
}

func (p *parser) positiveDuration(key string, def time.Duration) time.Duration {
	d := p.duration(key, def)
	if d == 0 {
		p.fail(fmt.Errorf("%s: must be greater than zero", key))
	}
	return d
}

func (p *parser) logLevel(key, def string) slog.Level {
	raw := p.str(key, def)
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		p.fail(fmt.Errorf("%s=%q: must be one of debug, info, warn, error", key, raw))
		return slog.LevelInfo
	}
	return lvl
}
