package httpapi

import "net/http"

// newRouter maps "METHOD /path" patterns to handlers.
//
// Since Go 1.22 the standard library's ServeMux understands methods and path
// parameters, e.g. "GET /api/v1/owners/{ownerId}/performance" and
// r.PathValue("ownerId"). That's why we don't need a third-party router.
func newRouter(readiness *Readiness) http.Handler {
	mux := http.NewServeMux()

	// Operational endpoints (no auth; not routed through the public ALB).
	mux.HandleFunc("GET /healthz", liveness)
	mux.HandleFunc("GET /readyz", readiness.handle)

	// Phase 3 adds:
	//   GET /api/v1/accounts/performance
	//   GET /api/v1/owners/{ownerId}/performance

	return jsonFallback(mux)
}

// jsonFallback makes "no such route" (404) and "wrong method" (405) answers
// use our JSON error format. ServeMux on its own replies in plain text.
func jsonFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			// A route matched. Call mux.ServeHTTP (not h directly) so the mux
			// fills in path parameters for r.PathValue().
			mux.ServeHTTP(w, r)
			return
		}

		// No route matched. Let the mux's built-in handler decide between
		// 404 and 405 (it also computes the Allow header), capture its
		// decision, and answer in JSON instead.
		probe := &probeWriter{header: http.Header{}}
		h.ServeHTTP(probe, r)

		if probe.status == http.StatusMethodNotAllowed {
			if allow := probe.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			writeError(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
				"method "+r.Method+" is not allowed on this path", nil)
			return
		}
		writeError(w, r, http.StatusNotFound, CodeNotFound, "no route matches this path", nil)
	})
}

// probeWriter is a throwaway ResponseWriter that records the status code and
// headers but discards the body.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header         { return p.header }
func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *probeWriter) WriteHeader(code int)        { p.status = code }
