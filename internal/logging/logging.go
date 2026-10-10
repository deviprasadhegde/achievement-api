// Package logging builds the application's structured logger.
//
// "Structured" means every log line is key=value data (JSON in the cloud),
// not free text. CloudWatch Logs Insights can then filter and aggregate it:
//
//	fields @timestamp, msg, status, duration_ms
//	| filter request_id = "c1f3a7e2-..."
//
// We use log/slog from the Go standard library — no extra dependency.
package logging

import (
	"context"
	"io"
	"log/slog"
)

// New returns a logger that writes to w in the chosen format.
// attrs are attached to every line (e.g. service name, environment, version).
func New(w io.Writer, format string, level slog.Level, attrs ...any) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(w, opts) // readable on your laptop
	} else {
		h = slog.NewJSONHandler(w, opts) // parseable in CloudWatch
	}
	return slog.New(h).With(attrs...)
}

type ctxKey struct{}

// WithLogger stores a logger in the context. The request-ID middleware uses
// this to give every request its own logger that already carries
// request_id, so every line logged while handling that request is correlated.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the request-scoped logger, or the default logger if
// none was stored (e.g. in background code or tests).
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
