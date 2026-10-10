// Command api is the entry point of the Achievement API service.
//
// main.go only WIRES things together: load config → build logger → build
// dependencies → start server → wait for a stop signal → shut down cleanly.
// Business logic lives under internal/.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deviprasadhegde/achievement-api/internal/config"
	"github.com/deviprasadhegde/achievement-api/internal/httpapi"
	"github.com/deviprasadhegde/achievement-api/internal/logging"
)

// version is overwritten at build time:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/api
var version = "dev"

func main() {
	if err := run(); err != nil {
		// Logger may not exist yet (e.g. bad config), so use stderr.
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Configuration — fail fast with every problem listed.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config:\n%w", err)
	}

	// 2. Logger — every line carries service/env/version.
	logger := logging.New(os.Stdout, cfg.LogFormat, cfg.LogLevel,
		"service", "achievement-api", "env", cfg.Env, "version", version)
	slog.SetDefault(logger)

	// 3. Stop signals. Ctrl+C sends SIGINT; Kubernetes sends SIGTERM when it
	//    wants a pod to stop (deploys, scale-down, node drain).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 4. Dependencies. Phase 2/3 adds Postgres, Phase 4 adds Redis:
	//      readiness.AddCheck("postgres", pool.Ping)
	readiness := httpapi.NewReadiness()
	srv := httpapi.NewServer(cfg, logger, readiness)

	// 5. Bind the port BEFORE starting the goroutine, so "address already in
	//    use" is reported immediately as a startup error.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", ln.Addr().String())
		// Serve blocks until Shutdown is called (returns ErrServerClosed)
		// or something goes badly wrong.
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	// 6. Wait for a stop signal or a server crash.
	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}
	stop() // a second Ctrl+C now kills the process immediately

	// 7. Graceful shutdown.
	//    a) Fail readiness so the load balancer stops sending new requests.
	//    b) Keep serving for ShutdownDelay while that change propagates.
	//    c) Shutdown(): stop accepting connections, let in-flight requests
	//       finish (up to ShutdownTimeout), then return.
	logger.Info("shutdown signal received", "drain_delay", cfg.ShutdownDelay.String())
	readiness.SetShuttingDown()
	time.Sleep(cfg.ShutdownDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	// Phase 2+: close DB pool and Redis client here, AFTER the server stopped.

	logger.Info("shutdown complete")
	return nil
}
