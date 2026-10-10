# Phase 0 — Architecture, Decisions and the Weekend Plan

> **Where you are:** the very beginning. Nothing to install yet in this document; read it once end to end (≈30 min).
> Then go to [`01-prerequisites.md`](01-prerequisites.md) and install your tools.

---

## 1. What we are building, in one picture

```
                        ┌────────────────────────── AWS (one region, e.g. us-east-1) ──────────────────────────┐
                        │  VPC 10.0.0.0/16                                                                      │
  ┌─────────┐  HTTPS    │  ┌─ public subnets (2 AZs) ─┐     ┌──────── private subnets (2 AZs) ─────────────┐   │
  │ Postman │ ────────► │  │  Application Load        │     │  EKS worker nodes (Graviton, managed group)  │   │
  │  / curl │           │  │  Balancer (ALB)          │ ──► │   ┌──────────┐ ┌──────────┐ ┌──────────┐     │   │
  └─────────┘           │  │  NAT Gateway             │     │   │ api pod  │ │ api pod  │ │ api pod  │ ... │   │
                        │  └──────────────────────────┘     │   └────┬─────┘ └────┬─────┘ └────┬─────┘     │   │
                        │                                   │        │ 1. check cache          │           │   │
                        │                                   │        ▼                         ▼           │   │
                        │                                   │  ┌──────────────┐   2. on miss ┌─────────┐   │   │
                        │                                   │  │ ElastiCache  │ ◄──────────  │  RDS    │   │   │
                        │                                   │  │ (Redis, TLS) │   3. fill    │Postgres │   │   │
                        │                                   │  └──────────────┘              └─────────┘   │   │
                        │                                   └──────────────────────────────────────────────┘   │
                        │  Secrets Manager · ECR (images) · CloudWatch (logs, metrics, alarms) · IAM            │
                        └───────────────────────────────────────────────────────────────────────────────────────┘
   GitHub ──(OIDC, no stored AWS keys)──► GitHub Actions ──► build/test/scan ──► ECR ──► deploy to EKS
   Terraform ──► creates/updates everything inside the AWS box above
```

### The life of one request (the "cache‑aside" pattern)

1. You send `GET /api/v1/owners/7/performance?...` from Postman.
2. DNS resolves to the **ALB**. The ALB only forwards to pods that are passing their **readiness check**.
3. One **pod** (one running copy of our Go program) receives it. Middleware runs first: assign a request ID, check the API key, check the rate limit, start a timer.
4. The handler **validates** the input. Bad input → `400` immediately; the database is never touched.
5. The service builds a **cache key** from the normalized request and asks **Redis**.
   - **Hit** → return the stored JSON (≈1 ms). Response header `X-Cache: HIT`.
   - **Miss** → run the SQL query against **PostgreSQL**, build the response, store it in Redis with a TTL (5 min), return it. `X-Cache: MISS`.
   - **Redis is down** → skip the cache, go straight to Postgres, log a warning. `X-Cache: BYPASS`. The API stays up — the cache is an optimisation, never a dependency.
6. A structured JSON log line and Prometheus metrics are emitted for the request.

---

## 2. Each component, explained for a beginner

| Component | What it is | Why we need it |
|---|---|---|
| **Go REST API** | A compiled program that listens for HTTP requests and returns JSON. | The actual product. Go is fast, compiles to one small binary, and has great concurrency — ideal for containers. |
| **PostgreSQL on RDS** | A relational database. RDS = AWS runs it for you (patching, backups, failover). | Source of truth for accounts and KPI values. |
| **Redis on ElastiCache** | An in‑memory key/value store; reads in microseconds. | Repeated identical queries don't hit Postgres. Protects the DB under load. |
| **Docker** | Packages the binary + its minimal runtime into an *image* that runs the same everywhere. | "Works on my machine" → works on every node. |
| **ECR** | AWS's private Docker image registry. | Where CI pushes images and where EKS pulls them from. |
| **Kubernetes / EKS** | A system that runs containers across many machines, restarts them when they die, rolls out new versions, and scales them. EKS = AWS runs the Kubernetes control plane. | Self‑healing, rolling deploys, horizontal scaling. |
| **ALB + AWS Load Balancer Controller** | The controller watches Kubernetes `Ingress` objects and creates/configures a real AWS Application Load Balancer. | A single public HTTPS entry point that spreads traffic across healthy pods. |
| **HPA** (Horizontal Pod Autoscaler) | Adds/removes *pods* based on CPU (or other metrics). | Handles load spikes. |
| **Cluster Autoscaler** | Adds/removes *EC2 nodes* when pods can't be scheduled / nodes are idle. | Pods need machines to run on. |
| **Secrets Manager + External Secrets Operator** | AWS stores the secrets; the operator copies them into Kubernetes Secrets. | No passwords in Git, in images, or in YAML. |
| **Terraform** | Code that describes infrastructure; `terraform apply` makes AWS match the code. | Repeatable, reviewable, destroyable infrastructure. |
| **GitHub Actions + OIDC** | CI/CD runners. OIDC lets a workflow get short‑lived AWS credentials without storing keys. | Every push is tested, scanned, built and deployed the same way. |
| **CloudWatch** | AWS logs, metrics, dashboards, alarms. | Seeing what's happening and getting told when it breaks. |

### Three kinds of "scaling" — they are different things

| | What it scales | Trigger | Speed |
|---|---|---|---|
| **Load balancing** (ALB) | Nothing — it *distributes* requests across the pods that already exist. | Every request. | Instant |
| **Pod autoscaling** (HPA) | Number of pods (copies of the app). | Avg CPU > 70 % of request. | ~15–60 s |
| **Infrastructure autoscaling** (Cluster Autoscaler) | Number of EC2 nodes. | Pods stuck `Pending` because no node has room. | ~2–4 min |

They chain together: traffic rises → pods get busy → HPA adds pods → no room → Cluster Autoscaler adds a node → new pods start → readiness passes → ALB starts sending them traffic. We will *watch this happen* in Phase 9.

---

## 3. Architecture decisions (with trade‑offs)

Each line is a choice we will stick with unless something forces a change.

| # | Decision | Chosen | Alternatives considered | Why |
|---|---|---|---|---|
| D1 | AWS region | **us-east-1** (one variable to change) | me-central-1 (UAE, closest to you), eu-central-1 | Cheapest prices, every service/instance type available, most tutorials match. Latency doesn't matter for learning. |
| D2 | Kubernetes compute | **EKS managed node group** | Fargate; EKS Auto Mode | Fargate: no DaemonSets, slower scale, per‑pod cost. Auto Mode: great, but hides exactly the things you want to learn and adds a per‑node fee. Managed node groups = AWS patches nodes, you still see everything. |
| D3 | Node CPU architecture | **Graviton (arm64), `t4g.medium`** | x86 `t3.medium` | ~20 % cheaper; if your Mac is Apple Silicon your local images are already arm64. CI builds multi‑arch images anyway. |
| D4 | Node autoscaling | **Cluster Autoscaler** | Karpenter | Karpenter is faster and smarter but has more moving parts (its own IAM, interruption queue). CA is simple and enough here. Karpenter is in the hardening list. |
| D5 | Load balancer | **ALB via AWS Load Balancer Controller + `Ingress`** | NLB `Service type=LoadBalancer`; NGINX ingress | ALB is HTTP‑aware: path routing, health checks on `/readyz`, TLS termination, WAF later. |
| D6 | Redis | **ElastiCache (Redis OSS engine), 1 × `cache.t4g.micro`, TLS + AUTH** | Redis pod in the cluster; ElastiCache Valkey; Serverless | Managed, private, encrypted, ~$12/month. A Redis pod is cheaper but you'd own persistence/upgrades. Valkey is a drop‑in, slightly cheaper option later. |
| D7 | Database | **RDS PostgreSQL, `db.t4g.micro`, single‑AZ, encrypted, private, 7‑day backups** | Aurora; Multi‑AZ RDS | Multi‑AZ doubles cost; we document the switch (one variable) in hardening. Aurora is overkill and more expensive at this size. |
| D8 | Secrets | **AWS Secrets Manager → External Secrets Operator → K8s Secret** | Secrets Store CSI driver; plain K8s Secrets from CI | ESO is the most common pattern, supports rotation refresh, and keeps secrets out of Git and CI logs. |
| D9 | Pod → AWS permissions | **EKS Pod Identity** | IRSA (OIDC‑based) | Newer and simpler than IRSA; no per‑cluster OIDC trust policy juggling. |
| D10 | NAT | **1 NAT Gateway** | One per AZ; no NAT + VPC endpoints | One per AZ is the HA answer but triples a ~$32/month line. Documented as hardening. |
| D11 | Terraform state | **S3 backend with native S3 locking (`use_lockfile`)** | S3 + DynamoDB lock table; Terraform Cloud | Native S3 locking (Terraform ≥ 1.10) removes the DynamoDB table. |
| D12 | DB migrations | **golang-migrate, SQL up/down files, run as a K8s `Job` before each deploy** | Run on app startup; Atlas; Goose | Startup migrations race when 3 pods start at once. A Job runs once, and a failure stops the deploy. |
| D13 | HTTP router | **Go standard library `net/http` (`ServeMux` with method + path patterns)** | chi, gin, echo | Modern Go's stdlib router supports `GET /owners/{id}/performance`. Fewer dependencies, and you learn the real thing. |
| D14 | Go libraries | `pgx/v5` (Postgres + pool), `go-redis/v9`, `log/slog` (JSON logs), `prometheus/client_golang`, `testcontainers-go` | `database/sql` + lib/pq, zap, logrus | Current best‑in‑class, actively maintained, minimal. |
| D15 | API authentication | **API keys** (`X-API-Key`), stored hashed in Secrets Manager | JWT via Cognito/OIDC; mTLS | Right fit for machine‑to‑machine on a weekend. JWT/OIDC with scopes is the documented upgrade path. |
| D16 | Rate limiting | **Per‑API‑key limit in Redis** (shared across pods) | Per‑pod in‑memory; AWS WAF rate rules | A per‑pod limiter lets a client get N × limit with N pods. Redis makes it correct; WAF is added in hardening as an outer layer. |
| D17 | Observability | **slog JSON → stdout → CloudWatch (Container Insights)**, `/metrics` Prometheus endpoint, CloudWatch alarms | Prometheus + Grafana stack; Datadog | Zero extra servers to run. Managed Prometheus/Grafana is a hardening step. |
| D18 | GitHub repo visibility | **Public** | Private | On free plans, *required‑reviewer approval gates* on environments only work for public repos. Nothing secret goes in the repo — that's the point of OIDC + Secrets Manager. |

---

## 4. The data model (logical → physical)

Your logical schema maps to two tables (full DDL, comments and migrations come in Phase 2):

```sql
CREATE TABLE accounts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    owner_id   BIGINT      NOT NULL CHECK (owner_id > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_accounts_owner_id ON accounts (owner_id);

CREATE TABLE kpi_values (
    account_id BIGINT        NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    kpi_id     INTEGER       NOT NULL CHECK (kpi_id > 0),
    date       DATE          NOT NULL,
    value      NUMERIC(20,4) NOT NULL,
    PRIMARY KEY (account_id, kpi_id, date)
);
```

Key points (we'll go deeper in Phase 2):

- **Naming:** Postgres folds unquoted names to lower case, so `AccountTable.OwnerID` becomes `accounts.owner_id` (snake_case). Quoted mixed‑case names are a common source of pain.
- **`NUMERIC(20,4)`, not `FLOAT`:** exact decimal maths; sums don't drift (`0.1 + 0.2 = 0.3`).
- **The composite primary key `(account_id, kpi_id, date)`** does three jobs: it guarantees one value per account/KPI/day, and its B‑tree index serves *both* of our queries (`account_id IN (…) AND kpi_id IN (…) AND date BETWEEN …`), so we don't need an extra index.
- **`accounts(owner_id)` index** makes "all accounts for owner 7" a quick index lookup.
- **There is no `owners` table** in your spec. So an owner "exists" if at least one account references it. We keep that (see assumption A3).

---

## 5. Essential now vs. production hardening later

Honest framing: by Sunday night you'll have a **production‑*oriented*** system that follows real‑world patterns. It is **not production‑certified** — that needs things a weekend can't give you (load history, security review, DR drills, on‑call).

| Area | ✅ Built this weekend | 🔜 Hardening backlog (prioritised in Phase 12) |
|---|---|---|
| App | Clean layering, config via env, validation, consistent errors, timeouts, context propagation, graceful shutdown, health/readiness/liveness | OpenAPI spec + generated clients, pagination for very large owners |
| Data | Migrations with up/down, indexes, pooled TLS connections, encrypted storage, automated backups | Multi‑AZ, read replica, PITR restore drill, separate migration vs app DB roles, partitioning `kpi_values` by date at scale |
| Cache | Cache‑aside, TTL, key normalisation, version‑based invalidation, fail‑open, stampede protection (singleflight) | Replication group with automatic failover |
| Security | API keys, Redis‑backed rate limiting, non‑root distroless image, image scanning, `govulncheck`, private subnets, SG least access, OIDC CI, secrets in Secrets Manager | HTTPS with your own domain (ACM), AWS WAF, JWT/OIDC auth, NetworkPolicies, secret rotation, cosign image signing, admission policies |
| K8s | Requests/limits, probes, HPA, Cluster Autoscaler, PDB, rolling update, rollback | Karpenter, topology spread across AZs, GitOps (Argo CD), canary deploys |
| Observability | JSON logs in CloudWatch with request IDs, Prometheus metrics, CloudWatch alarms | Managed Prometheus + Grafana dashboards, OpenTelemetry tracing, SLOs/error budgets |
| Ops | Terraform for everything, CI/CD with approval gate, smoke tests, documented teardown | Separate dev/prod AWS accounts, drift detection, cost anomaly alerts, runbooks |

---

## 6. Cost — read this before creating anything in AWS

Approximate **us-east-1 on‑demand** prices (check the [AWS Pricing Calculator](https://calculator.aws/) — prices change):

| Resource | ≈ per hour | ≈ per 24 h |
|---|---|---|
| EKS control plane | $0.10 | $2.40 |
| 2 × `t4g.medium` nodes | $0.067 | $1.61 |
| NAT Gateway (+ small data) | $0.045 | $1.08 |
| ALB | ~$0.025 | ~$0.60 |
| RDS `db.t4g.micro` + 20 GB gp3 | ~$0.018 | ~$0.45 |
| ElastiCache `cache.t4g.micro` | ~$0.016 | ~$0.40 |
| Public IPv4 addresses, Secrets Manager, CloudWatch, ECR | small | ~$0.50 |
| **Total** | **≈ $0.30/h** | **≈ $7/day** |

**Rules we follow:**
1. A **budget alert** is created *before* any resource (Phase 1 prerequisites).
2. AWS infrastructure goes up on **Day 2** and comes down at the end with `terraform destroy` (Phase 13). Leaving it up for a month ≈ $200.
3. Everything is tagged `Project=achievement-api` so the bill is easy to read.

---

## 7. Realistic schedule

An experienced engineer can do this in a long weekend. **For someone learning each tool as they go, plan on ~3 full days (≈ 24 hours of focused work).** I'd rather tell you that now than have you rush the parts that matter. The phases are ordered so you always have something working end‑to‑end before adding the next layer, and you can stop safely after any checkpoint.

| Session | Phases | Hours | You end the session with… |
|---|---|---|---|
| **Day 1 — Local** | 0 Architecture · 1 Setup & Go skeleton · 2 Postgres schema & migrations · 3 Both APIs · 4 Redis caching · 5 Tests & Postman · 6 Docker | ~8 | The full API running locally in Docker Compose (API + Postgres + Redis), tested, called from Postman. **$0 AWS spend.** |
| **Day 2 — AWS** | 7 Terraform (state, VPC, EKS, RDS, ElastiCache, ECR, IAM, secrets) · 8 Deploy to EKS (LB controller, ESO, manifests, migration Job) · 9 Autoscaling + load test | ~8 | Public ALB URL serving the API from multiple pods; HPA and node scaling demonstrated. |
| **Day 3 — Automate & harden** | 10 GitHub Actions (CI, deploy, Terraform) · 11 Monitoring & alarms · 12 Production‑readiness review · 13 Teardown | ~7 | Push‑to‑deploy pipeline with approval gate, alarms, final checklist, everything destroyed (or kept on purpose). |

## 8. Phase map

| Phase | Deliverable | Checkpoint (you must see this before moving on) |
|---|---|---|
| 0 | This doc + API contract | You can explain the request lifecycle and the 3 kinds of scaling. |
| 1 | Tools installed, AWS + GitHub ready, Go module with `/healthz` | `scripts/check-prereqs.sh` all green; `curl localhost:8080/healthz` → `{"status":"ok"}` |
| 2 | `migrations/`, seed data, `docker-compose.yml` (Postgres) | `psql` shows both tables and seeded rows |
| 3 | `internal/…` repository/service/handlers for both endpoints | Correct JSON for both endpoints; `400` on bad input |
| 4 | Redis cache layer | First call `X-Cache: MISS`, second `HIT`; stop Redis → `BYPASS`, still 200 |
| 5 | Unit + integration tests, Postman collection | `make test` green; Postman collection all passing |
| 6 | Dockerfile, full Compose stack | Image < 30 MB, runs as non‑root, whole stack via `docker compose up` |
| 7 | `infra/terraform/…` | `terraform apply` clean; `kubectl get nodes` shows 2 Ready nodes |
| 8 | `deploy/k8s/…` | `curl https://<alb>/api/v1/...` works; 3 pods serving |
| 9 | HPA + Cluster Autoscaler | Under load: pods 2 → N, a node added; then scale back down |
| 10 | `.github/workflows/…` | Push to `main` → tests → scan → ECR → approval → EKS → smoke test |
| 11 | Dashboards & alarms | A deliberately broken deploy triggers an alarm and a rollback |
| 12 | Readiness checklist + hardening backlog | Every item ticked or consciously deferred |
| 13 | Teardown | `terraform destroy` clean; Cost Explorer trending to $0 |

---

## 9. Assumptions — tell me if any are wrong

| ID | Assumption | Effect if wrong |
|---|---|---|
| A1 | `KpiId` identifies a **KPI type** (e.g. 1 = revenue, 2 = signups), not a unique row ID. | The API's "list of KPI IDs" input only makes sense this way. |
| A2 | There is **at most one value per account, per KPI, per day**. | If there can be several, we drop it from the primary key and add a surrogate ID. |
| A3 | Owners have no table of their own; an owner exists if they own ≥ 1 account. | Owner with no accounts → `404`. |
| A4 | KPI values are written by some other process (an ingestion job), **not** by this API. | That's why the API is read‑only and cache invalidation is TTL + version‑bump based. |
| A5 | Your Mac is **Apple Silicon** (M1/M2/M3/M4). | If Intel, nothing breaks — CI builds both architectures — but local images will be amd64. |

Next: the precise API definition → [`api-contract.md`](api-contract.md).
