package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deviprasadhegde/achievement-api/internal/config"
)

// newTestHandler builds the real server handler with a silent logger.
func newTestHandler(t *testing.T, rd *Readiness) http.Handler {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(cfg, logger, rd).Handler
}

func do(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return v
}

func TestLiveness(t *testing.T) {
	rec := do(newTestHandler(t, NewReadiness()), http.MethodGet, "/healthz", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := decode[healthResponse](t, rec).Status; got != "ok" {
		t.Errorf("status field = %q, want ok", got)
	}
	if rec.Header().Get(HeaderRequestID) == "" {
		t.Error("expected a generated X-Request-ID header")
	}
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Readiness)
		wantCode   int
		wantStatus string
	}{
		{"no checks", func(*Readiness) {}, 200, "ready"},
		{"passing check", func(r *Readiness) {
			r.AddCheck("db", func(context.Context) error { return nil })
		}, 200, "ready"},
		{"failing check", func(r *Readiness) {
			r.AddCheck("db", func(context.Context) error { return errors.New("connection refused") })
		}, 503, "not_ready"},
		{"shutting down", func(r *Readiness) { r.SetShuttingDown() }, 503, "shutting_down"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewReadiness()
			tc.setup(rd)
			rec := do(newTestHandler(t, rd), http.MethodGet, "/readyz", nil)
			if rec.Code != tc.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tc.wantCode)
			}
			body := decode[healthResponse](t, rec)
			if body.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", body.Status, tc.wantStatus)
			}
			if tc.name == "failing check" && body.Checks["db"] != "error" {
				t.Errorf("checks = %v, want db=error (and no error details leaked)", body.Checks)
			}
		})
	}
}

func TestUnknownRoute_Returns404JSON(t *testing.T) {
	rec := do(newTestHandler(t, NewReadiness()), http.MethodGet, "/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := decode[errorEnvelope](t, rec)
	if body.Error.Code != CodeNotFound || body.Error.RequestID == "" {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestWrongMethod_Returns405JSONWithAllow(t *testing.T) {
	rec := do(newTestHandler(t, NewReadiness()), http.MethodPost, "/healthz", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("expected an Allow header")
	}
	if code := decode[errorEnvelope](t, rec).Error.Code; code != CodeMethodNotAllowed {
		t.Errorf("code = %q", code)
	}
}

func TestRequestID_EchoedWhenValid_ReplacedWhenNot(t *testing.T) {
	h := newTestHandler(t, NewReadiness())

	rec := do(h, http.MethodGet, "/healthz", map[string]string{HeaderRequestID: "my-trace_123"})
	if got := rec.Header().Get(HeaderRequestID); got != "my-trace_123" {
		t.Errorf("valid id not echoed: %q", got)
	}

	rec = do(h, http.MethodGet, "/healthz", map[string]string{HeaderRequestID: "bad id with spaces"})
	if got := rec.Header().Get(HeaderRequestID); got == "bad id with spaces" || len(got) != 36 {
		t.Errorf("invalid id should be replaced by a UUID, got %q", got)
	}
}

func TestRecoverer_PanicBecomes500JSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := chain(boom, requestID(logger), recoverer)

	rec := do(h, http.MethodGet, "/", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decode[errorEnvelope](t, rec)
	if body.Error.Code != CodeInternal || body.Error.Message != "internal server error" {
		t.Errorf("unexpected body (must not leak panic text): %+v", body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(newTestHandler(t, NewReadiness()), http.MethodGet, "/healthz", nil)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
