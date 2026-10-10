package httpapi

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/YOUR_GITHUB_USER/achievement-api/internal/logging"
)

// Check reports whether one dependency is usable. It must respect ctx.
type Check func(ctx context.Context) error

// Readiness answers "should this pod receive traffic right now?".
//
//   - Liveness (/healthz) = "is the process alive?" If it fails, Kubernetes
//     RESTARTS the container. So it must never depend on the database:
//     a DB outage would otherwise restart every pod at once, fixing nothing.
//   - Readiness (/readyz) = "can I serve requests?" If it fails, Kubernetes
//     and the load balancer simply STOP SENDING traffic to this pod until it
//     passes again. Dependency checks belong here.
type Readiness struct {
	shuttingDown atomic.Bool
	checks       []namedCheck
	checkTimeout time.Duration
}

type namedCheck struct {
	name  string
	check Check
}

// NewReadiness creates a readiness tracker with no checks yet.
func NewReadiness() *Readiness {
	return &Readiness{checkTimeout: time.Second}
}

// AddCheck registers a dependency check. Call it during startup only,
// before the server starts serving (it is not safe for concurrent use).
func (rd *Readiness) AddCheck(name string, c Check) {
	rd.checks = append(rd.checks, namedCheck{name: name, check: c})
}

// SetShuttingDown makes /readyz fail from now on, so traffic drains away
// before the server stops.
func (rd *Readiness) SetShuttingDown() { rd.shuttingDown.Store(true) }

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

func (rd *Readiness) handle(w http.ResponseWriter, r *http.Request) {
	if rd.shuttingDown.Load() {
		writeJSON(w, r, http.StatusServiceUnavailable, healthResponse{Status: "shutting_down"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), rd.checkTimeout)
	defer cancel()

	status, code := "ready", http.StatusOK
	results := make(map[string]string, len(rd.checks))
	for _, c := range rd.checks {
		if err := c.check(ctx); err != nil {
			// The reason goes to the logs, never to the (possibly public)
			// response — error strings can leak hostnames and versions.
			logging.FromContext(r.Context()).Warn("readiness check failed", "check", c.name, "err", err)
			results[c.name] = "error"
			status, code = "not_ready", http.StatusServiceUnavailable
			continue
		}
		results[c.name] = "ok"
	}
	writeJSON(w, r, code, healthResponse{Status: status, Checks: results})
}
