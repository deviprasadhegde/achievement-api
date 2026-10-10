package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/deviprasadhegde/achievement-api/internal/logging"
)

// Middleware wraps a handler with extra behaviour that runs before and/or
// after it — like layers of an onion around the actual endpoint.
type Middleware func(http.Handler) http.Handler

// chain applies middlewares so that the FIRST one listed is the OUTERMOST:
// chain(h, a, b, c) handles a request as a → b → c → h.
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// ── Request ID ──────────────────────────────────────────────────────────────

// HeaderRequestID is the header used to correlate a request across services.
const HeaderRequestID = "X-Request-ID"

// Accept a caller's ID only if it is short and safe; otherwise an attacker
// could inject newlines or megabytes of text into our logs.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type requestIDKey struct{}

// RequestIDFrom returns the current request's ID ("" outside a request).
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestID assigns every request an ID, echoes it in the response header and
// stores a logger that carries it, so all log lines for the request match.
func requestID(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if !validRequestID.MatchString(id) {
				id = newRequestID()
			}
			w.Header().Set(HeaderRequestID, id)

			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			ctx = logging.WithLogger(ctx, base.With("request_id", id))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newRequestID returns a random UUID (version 4) using only the stdlib.
func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])      // crypto/rand.Read never fails on supported platforms
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ── Access log ──────────────────────────────────────────────────────────────

// statusRecorder remembers the status code and body size a handler wrote,
// because http.ResponseWriter doesn't let you read them back.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLog writes one structured line per request after it completes.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		level := slog.LevelInfo
		switch {
		case isProbe(r.URL.Path) && rec.status < 500:
			// Kubernetes calls the probes every few seconds; at info level
			// they would drown out real traffic.
			level = slog.LevelDebug
		case isProbe(r.URL.Path):
			// A failing probe (e.g. "shutting_down") is worth noticing but is
			// not an application error, so it must not trigger error alarms.
			level = slog.LevelWarn
		case rec.status >= 500:
			level = slog.LevelError
		}
		logging.FromContext(r.Context()).LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("query", r.URL.RawQuery),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

func isProbe(path string) bool {
	return path == "/healthz" || path == "/readyz" || path == "/metrics"
}

// ── Panic recovery ──────────────────────────────────────────────────────────

// recoverer turns a panic (a crash inside one handler) into a clean 500 for
// that request, logs the stack trace, and keeps the server running.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
				panic(v) // deliberate abort; let net/http handle it
			}
			logging.FromContext(r.Context()).Error("panic recovered",
				"panic", fmt.Sprint(v),
				"stack", string(debug.Stack()),
			)
			writeError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error", nil)
		}()
		next.ServeHTTP(w, r)
	})
}

// ── Security headers ────────────────────────────────────────────────────────

// securityHeaders sets safe defaults for a JSON API.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff") // browsers must trust our Content-Type
		h.Set("X-Frame-Options", "DENY")           // never render inside a frame
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store") // contract: caching happens server-side only
		next.ServeHTTP(w, r)
	})
}

// ── Request timeout ─────────────────────────────────────────────────────────

// timeout gives every request a deadline through its context. Anything that
// honours the context (pgx, go-redis, http clients) stops when it expires.
func timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
