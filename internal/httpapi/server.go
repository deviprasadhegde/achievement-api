// Package httpapi is the HTTP layer: routing, middleware, request parsing and
// JSON responses. It knows about HTTP but not about SQL or Redis — those live
// in their own packages and are passed in (added in Phases 3 and 4).
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/deviprasadhegde/achievement-api/internal/config"
)

// NewServer builds a fully configured *http.Server.
//
// Never use http.ListenAndServe(addr, handler) in production: it has NO
// timeouts, so a client can hold a connection open forever.
func NewServer(cfg config.Config, logger *slog.Logger, readiness *Readiness) *http.Server {
	handler := chain(newRouter(readiness),
		requestID(logger),           // 1. give the request an ID + logger
		accessLog,                   // 2. time it and log the outcome (sees panics as 500)
		recoverer,                   // 3. turn panics into 500s
		securityHeaders,             // 4. safe response headers
		timeout(cfg.RequestTimeout), // 5. deadline for everything below
	)

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, // time to receive the headers
		ReadTimeout:       cfg.ReadTimeout,       // time to receive the whole request
		WriteTimeout:      cfg.WriteTimeout,      // time to send the response
		IdleTimeout:       cfg.IdleTimeout,       // keep-alive connections between requests
		MaxHeaderBytes:    64 << 10,              // 64 KiB is plenty; default is 1 MiB
		// net/http's own internal errors (TLS handshakes, bad requests) go
		// through our structured logger too.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}
