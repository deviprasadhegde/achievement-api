# Phase 1B — Git Repo + Go Service Skeleton

> **Goal:** a version‑controlled Go service that starts, logs in structured form, answers `/healthz` and `/readyz`, returns contract‑shaped JSON errors, tags every request with an ID, and shuts down gracefully. Everything in later phases plugs into this skeleton.
> **Time:** ~90 min.
> **Run commands from:** the project root, unless stated:
> ```bash
> cd "/Users/deviprasad/All Data/Data/Personal_Projects/AchivementAPI"
> ```

---

## 1. Concepts you need first

### Go modules, packages and `internal/`
- A **module** is your whole project. Its name ("module path") lives in `go.mod`, and by convention it's where the code is hosted: `github.com/<you>/achievement-api`. Every import of our own code starts with that path.
- A **package** is one folder of `.go` files. `internal/config` is package `config`.
- `cmd/api/` holds the `main` package — the program's entry point. Putting it under `cmd/` leaves room for other binaries later (e.g. `cmd/seed`).
- `internal/` is special to the Go compiler: code there **cannot be imported by other modules**. It keeps our internals private and lets us refactor freely.

### How the pieces we're writing fit together

```
            ┌──────────────────────── cmd/api/main.go ────────────────────────┐
            │ config.Load() → logging.New() → httpapi.NewServer() → Serve()   │
            │                 wait for SIGINT/SIGTERM → graceful Shutdown()   │
            └──────────────────────────────────────────────────────────────────┘
 request ─► requestID ─► accessLog ─► recoverer ─► securityHeaders ─► timeout ─► router ─► handler
            (ID+logger)  (time+log)   (panic→500)  (safe headers)    (deadline)  (/healthz, /readyz,
                                                                                 JSON 404/405)
```

### Middleware
A middleware is a function that wraps a handler: it does something *before* (assign an ID, start a timer) and/or *after* (log the result) the real endpoint runs. Stacking them like an onion keeps cross‑cutting concerns out of business code. In Phase 3/4 we add **auth** and **rate limiting** as middlewares on the `/api/v1` routes only.

### `context.Context`
Every request carries a `context`. It holds **request‑scoped values** (request ID, logger) and a **deadline**. When the deadline passes or the client disconnects, the context is cancelled — and pgx/go‑redis calls that received it stop immediately, so a slow query doesn't keep running for a user who's gone. That's "context propagation": pass `r.Context()` down into every call.

### Liveness vs readiness
| Probe | Question | On failure Kubernetes… | So it checks… |
|---|---|---|---|
| `/healthz` (liveness) | Is the process alive? | **restarts** the container | nothing external |
| `/readyz` (readiness) | Should I get traffic? | **stops routing** traffic to it | the database (Phase 2+) |

If liveness checked the DB, a 30‑second DB blip would restart *every* pod at once — turning a small problem into an outage.

### Graceful shutdown
When Kubernetes replaces a pod (every deploy!), it sends `SIGTERM`. A naive server dies instantly and drops the requests it was handling. Ours:
1. flips `/readyz` to `503` so the load balancer stops sending new requests,
2. keeps serving for `SHUTDOWN_DELAY` (5 s in K8s) while that change propagates,
3. calls `Shutdown()`, which stops accepting connections and **waits for in‑flight requests** to finish (max `SHUTDOWN_TIMEOUT`),
4. exits.

Result: zero failed requests during deploys.

### Server timeouts
`http.ListenAndServe(":8080", h)` — the version in most tutorials — has **no timeouts**. A client can open a connection and send one byte per minute forever (a "slowloris" attack), exhausting the server. We set read‑header, read, write and idle timeouts, plus a 5 s budget per request.

---

## 2. Create the Git repository and push Phase 0

### 2.1 Initialise Git locally

```bash
git init
git add .
git status          # review: docs/, scripts/, cmd/, internal/, Makefile … — and NO .env
git commit -m "Phase 0–1B: architecture, API contract, prerequisites, Go skeleton"
```

> Even though the Go files are already here, we finish setting them up in §3 before running anything. Committing now is fine — it's just text.

### 2.2 Create the GitHub repo and push

```bash
gh repo create achievement-api --public --source=. --remote=origin --push \
  --description "Production-oriented Go REST API on AWS EKS (learning project)"
```

Expected (with your username):
```
✓ Created repository <you>/achievement-api on GitHub
✓ Added remote https://github.com/<you>/achievement-api.git
✓ Pushed commits to https://github.com/<you>/achievement-api.git
```

> **Prefer the website?** github.com → **+** (top right) → **New repository** → name `achievement-api` → **Public** → do *not* tick "Add a README" (we have one) → **Create repository**, then run the two commands it shows under "…push an existing repository" (`git remote add origin …` and `git push -u origin main`).

✅ **Verify:** `gh repo view --web` opens the repo; you see the README and `docs/`.

**Why public?** (Decision D18.) Free plans only support approval gates on public repos, and nothing secret will ever be in this repo.

---

## 3. Initialise the Go module

```bash
# 1. Your GitHub username, read from the gh CLI
GH_USER=$(gh api user --jq .login)
echo "$GH_USER"

# 2. Create go.mod
go mod init "github.com/$GH_USER/achievement-api"

# 3. The Go files I provided use the placeholder deviprasadhegde in their
#    import paths. Replace it with your username in every .go file:
grep -rl --include='*.go' 'deviprasadhegde' . | xargs sed -i '' "s/deviprasadhegde/$GH_USER/g"

# 4. Check nothing is left, then tidy and build
grep -rn 'deviprasadhegde' . --include='*.go' || echo "placeholder replaced ✅"
go mod tidy
go build ./...
```

Expected: `go: creating new go.mod: module github.com/<you>/achievement-api`, then `placeholder replaced ✅`, and `go build` prints **nothing** (in Go, silence = success).

Open `go.mod` — it's tiny:
```
module github.com/<you>/achievement-api

go 1.2x.x
```
The `go` line is the minimum Go version. There are no `require` lines yet because **this phase uses only the standard library**. Dependencies (pgx, go‑redis…) arrive in Phase 2–4.

---

## 4. Tour of the files (read alongside the code — it's heavily commented)

```
cmd/api/main.go                 entry point: wiring + graceful shutdown
internal/config/config.go       env vars → typed Config, validated, all errors at once
internal/config/config_test.go  tests for defaults, overrides, error reporting
internal/logging/logging.go     slog JSON/text logger + logger-in-context helpers
internal/httpapi/server.go      *http.Server with timeouts + middleware order
internal/httpapi/middleware.go  request ID, access log, panic recovery, headers, timeout
internal/httpapi/routes.go      ServeMux routes + JSON 404/405 fallback
internal/httpapi/health.go      /healthz and /readyz (+ shutdown flag, pluggable checks)
internal/httpapi/respond.go     writeJSON / writeError — the one error format
internal/httpapi/httpapi_test.go tests for all of the above
Makefile                        make run / test / lint / check …
.env.example                    local settings template
.golangci.yml                   linter configuration
```

Things worth noticing:

| File | Detail | Why |
|---|---|---|
| `config.go` | `load(lookup func…)` takes the env lookup as a parameter | Tests pass a fake map instead of mutating real env vars. |
| `config.go` | `errors.Join` of every problem | You fix all config mistakes in one go, not one per restart. |
| `config.go` | `WRITE_TIMEOUT > REQUEST_TIMEOUT` rule | Otherwise clients get a dropped connection instead of a clean error. |
| `middleware.go` | Incoming `X-Request-ID` accepted only if `^[A-Za-z0-9_-]{1,64}$` | Prevents log injection. |
| `middleware.go` | Probes logged at `debug` | Kubernetes hits them every few seconds; they'd bury real traffic. |
| `middleware.go` | `recoverer` returns a generic message | Panic text can contain internals; it goes to logs only. |
| `routes.go` | `mux.ServeHTTP` (not `h.ServeHTTP`) when a route matched | Only the mux fills in `r.PathValue("ownerId")` — needed in Phase 3. |
| `respond.go` | Marshal before `WriteHeader` | Once a status is sent it can't be changed. |
| `health.go` | `/readyz` says `"error"`, not the error text | The reason is logged; responses don't leak hostnames. |
| `main.go` | `ListenConfig.Listen` before the goroutine | "Port already in use" fails startup immediately and clearly. |
| `main.go` | `-X main.version=…` | The running version appears in every log line. |

---

## 5. Run it

```bash
cp .env.example .env
make run
```

Expected (text format because `.env` sets `LOG_FORMAT=text`):

```
time=2026-10-11T10:00:00.000+04:00 level=INFO msg="http server listening" service=achievement-api env=local version=abc1234 addr=127.0.0.1:8080
```

The server is now running in this terminal. **Open a second Terminal tab** (⌘T) for the next steps.

---

## 6. Verify (second terminal)

### 6.1 Liveness — with headers

```bash
curl -i http://127.0.0.1:8080/healthz
```
```
HTTP/1.1 200 OK
Cache-Control: no-store
Content-Security-Policy: default-src 'none'; frame-ancestors 'none'
Content-Type: application/json; charset=utf-8
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-Request-Id: 458aab2b-e642-4697-8dfe-d6543bb95a70

{"status":"ok"}
```

### 6.2 Readiness

```bash
curl -s http://127.0.0.1:8080/readyz | jq
```
```json
{ "status": "ready" }
```
(No `checks` yet — Postgres is added in Phase 2/3.)

### 6.3 Unknown route → JSON 404

```bash
curl -s http://127.0.0.1:8080/nope | jq
```
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "no route matches this path",
    "requestId": "34b82a5d-c923-4f5c-a59e-3f3ebf1e0e68"
  }
}
```

### 6.4 Wrong method → JSON 405 with `Allow`

```bash
curl -si -X POST http://127.0.0.1:8080/healthz
```
```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
...
{"error":{"code":"METHOD_NOT_ALLOWED","message":"method POST is not allowed on this path","requestId":"…"}}
```

### 6.5 Request correlation

```bash
curl -s -D - -o /dev/null -H "X-Request-ID: devi-test-1" http://127.0.0.1:8080/healthz | grep -i request-id
```
→ `X-Request-Id: devi-test-1`. Now look at the **first terminal**: the log line carries `request_id=devi-test-1`. That's how, in production, you'll find every log line for a request a user complains about.

### 6.6 Same thing in Postman
1. Open Postman → **New** → **HTTP**.
2. Method **GET**, URL `http://127.0.0.1:8080/healthz` → **Send**.
3. Check the **Headers** tab in the response pane for `X-Request-Id`.
(We'll build a proper collection with tests in Phase 5.)

### 6.7 Watch a graceful shutdown

Stop the server (`Ctrl+C` in the first terminal), then start it with a drain delay:

```bash
make run SHUTDOWN_DELAY=10s
```

Variables written **after** `make run` override `.env`. (Putting them *before* `make` doesn't work here: the Makefile loads `.env`, and Makefile values win over the shell environment.) Now press **`Ctrl+C`**, and *within 10 seconds* in the second terminal:

```bash
curl -s http://127.0.0.1:8080/readyz
```
→ `{"status":"shutting_down"}` — the server is still answering but tells the load balancer "don't send me new work". After 10 s the first terminal shows:

```
level=INFO msg="shutdown signal received" ... drain_delay=10s
level=WARN msg="http request" ... path=/readyz status=503 ...
level=INFO msg="shutdown complete" ...
```

### 6.8 See JSON logs (what CloudWatch will receive)

```bash
make run LOG_FORMAT=json
```
→ `{"time":"…","level":"INFO","msg":"http server listening","service":"achievement-api",…}`. `Ctrl+C` to stop.

### 6.9 See config validation

```bash
make run APP_ENV=staging LOG_LEVEL=loud REQUEST_TIMEOUT=abc
```
```
fatal: load config:
APP_ENV="staging": must be one of local, dev, prod
LOG_LEVEL="loud": must be one of debug, info, warn, error
REQUEST_TIMEOUT="abc": must be a non-negative duration such as 500ms, 5s or 1m
```
All problems at once; the service refuses to start with bad config ("fail fast").

---

## 7. Tests, linting, vulnerability scan

```bash
make test
```
```
?   	github.com/<you>/achievement-api/cmd/api	[no test files]
ok  	github.com/<you>/achievement-api/internal/config	0.3s
ok  	github.com/<you>/achievement-api/internal/httpapi	0.4s
?   	github.com/<you>/achievement-api/internal/logging	[no test files]
```

```bash
make check     # vet + golangci-lint + race-detector tests + govulncheck
```
Expected to end with `0 issues.` from the linter and `No vulnerabilities found.` from govulncheck.

**What these tools are:**
- `go vet` — the compiler team's checks for suspicious code (wrong printf verbs, copied locks…).
- `golangci-lint` — runs many linters at once: unchecked errors, security issues (`gosec`), missing contexts (`noctx`), formatting. It already caught three real issues while I was preparing this phase. One was `net.Listen` without a context, which is why `main.go` uses `ListenConfig`.
- `-race` — runs tests with the race detector, which catches two goroutines touching the same memory unsafely.
- `govulncheck` — checks your code *and the Go standard library version you compiled with* against the Go vulnerability database, and reports only vulnerabilities in functions you actually call.

**Read a test:** open `internal/httpapi/httpapi_test.go` → `TestReadiness`. It's a *table‑driven test* — the idiomatic Go style: a slice of cases, one loop, `t.Run` per case. You'll write many of these in Phase 5.

---

## 8. Commit and push

```bash
git add .
git status        # go.mod and the updated .go files; still NO .env
git commit -m "Phase 1B: Go module, config, structured logging, health probes, graceful shutdown"
git push
```

✅ **Verify:** refresh the repo page on GitHub; `go.mod` is there and `.env` is **not**.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `package github.com/deviprasadhegde/... is not in std` | Placeholder not replaced | Re‑run step 3 (the `sed` line). |
| `main module (…) does not contain package github.com/<other>/...` | go.mod path ≠ import paths | `head -1 go.mod` and imports must match exactly; fix with `sed`. |
| `sed: 1: "./cmd/...": invalid command code` | GNU‑style `sed -i` on macOS | Use `sed -i ''` exactly as shown. |
| `fatal: listen on 127.0.0.1:8080: … address already in use` | Another process uses 8080 (often a server you didn't stop) | `lsof -i :8080` → `kill <PID>`, or `make run HTTP_ADDR=127.0.0.1:8081`. |
| `make: *** No rule to make target 'run'` | Wrong directory | `cd` to the project root (quotes around the path!). |
| `.env:…: *** missing separator` | Quotes/spaces in `.env` | Format is `KEY=value`, no quotes, no spaces around `=`. |
| macOS asks "Accept incoming network connections?" | Listening on all interfaces (`:8080`) | Use `HTTP_ADDR=127.0.0.1:8080` as in `.env.example`, or click Deny (localhost still works). |
| `golangci-lint: … unsupported version of the configuration` | golangci-lint v1 installed | `brew upgrade golangci-lint` (our config is v2 format). |
| `go test -race` fails with `cgo` errors | Xcode command line tools missing | `xcode-select --install` |
| `gh: command not found` / `HTTP 401` | Not logged in | `gh auth login` |

---

## ✅ Checkpoint 1B

- [ ] Repo `github.com/<you>/achievement-api` exists, public, with two commits
- [ ] `make run` starts; `curl 127.0.0.1:8080/healthz` → `{"status":"ok"}`
- [ ] `/nope` → JSON 404; `POST /healthz` → JSON 405 with `Allow`
- [ ] Your `X-Request-ID` is echoed in the response and appears in the log line
- [ ] During a `make run SHUTDOWN_DELAY=10s` shutdown, `/readyz` returns `shutting_down`
- [ ] `make check` passes (0 lint issues, no vulnerabilities)
- [ ] `.env` is not on GitHub

**Self‑check — can you answer these?**
1. Why must `/healthz` never check the database?
2. What happens to a request that's halfway through when the pod receives SIGTERM?
3. Where does the request ID come from if the client doesn't send one, and where can you see it?

**Next — Phase 2:** run PostgreSQL locally in Docker Compose, design the schema properly (types, keys, constraints, indexes), write versioned migrations with rollback, seed realistic test data, and connect the API to Postgres with a secure connection pool. `/readyz` will then really check the database.
