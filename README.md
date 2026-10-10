# Achievement API

A production‑oriented Go REST API that serves aggregated KPI performance for accounts and account owners, built as a hands‑on learning project.

```
Postman → ALB → Go API pods on EKS → Redis (ElastiCache) / PostgreSQL (RDS)
```

**Stack:** Go · PostgreSQL (RDS) · Redis (ElastiCache) · Docker · Kubernetes (EKS) · Terraform · GitHub Actions · Postman · AWS

## Endpoints (v1)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/accounts/performance?accountIds=&kpiIds=&startDate=&endDate=` | KPI stats for a list of accounts |
| `GET` | `/api/v1/owners/{ownerId}/performance?kpiIds=&startDate=&endDate=` | KPI stats across all of an owner's accounts |
| `GET` | `/healthz`, `/readyz`, `/metrics` | Liveness, readiness, Prometheus metrics |

Full specification, including aggregation rules: [`docs/api-contract.md`](docs/api-contract.md).

## Learning guide

| Phase | Guide | Status |
|---|---|---|
| 0 | [Architecture, decisions, plan](docs/00-architecture-and-plan.md) | ✅ |
| 1A | [Prerequisites: tools, AWS, GitHub](docs/01-prerequisites.md) | ✅ |
| 1B | [Repo + Go skeleton (config, logging, health, graceful shutdown)](docs/02-go-skeleton.md) | ⏳ in progress |
| 2 | PostgreSQL schema, migrations, seed data | ⬜ |
| 3 | Account & owner performance endpoints | ⬜ |
| 4 | Redis caching | ⬜ |
| 5 | Unit, integration & Postman tests | ⬜ |
| 6 | Docker image & local Compose stack | ⬜ |
| 7 | Terraform: VPC, EKS, RDS, ElastiCache, ECR, IAM, secrets | ⬜ |
| 8 | Deploy to EKS (ALB ingress, External Secrets, migrations Job) | ⬜ |
| 9 | Autoscaling (HPA + Cluster Autoscaler) & load testing | ⬜ |
| 10 | GitHub Actions CI/CD with OIDC | ⬜ |
| 11 | Monitoring, logging, alarms | ⬜ |
| 12 | Production‑readiness review & hardening backlog | ⬜ |
| 13 | Teardown & cost cleanup | ⬜ |

## Quick start (local)

```bash
cp .env.example .env
make run            # http://127.0.0.1:8080/healthz
make check          # vet, lint, race tests, vulnerability scan
```

## Repository layout (grows phase by phase)

```
.
├── cmd/api/                # main.go — wiring & startup          (Phase 1B)
├── internal/               # application code, not importable by others
│   ├── config/             # env-based configuration              (1B)
│   ├── httpapi/            # handlers, middleware, errors         (1B–3)
│   ├── performance/        # domain: validation, service, models  (3)
│   ├── store/postgres/     # SQL queries                          (3)
│   └── cache/              # Redis cache-aside                    (4)
├── migrations/             # golang-migrate SQL up/down files     (2)
├── test/                   # integration tests, Postman collection(5)
├── deploy/k8s/             # Kubernetes manifests                 (8)
├── infra/terraform/        # AWS infrastructure                   (7)
├── .github/workflows/      # CI/CD                                (10)
├── docs/                   # this learning guide
└── scripts/                # helper scripts
```

> ⚠️ **Cost:** AWS resources from Phase 7 onwards cost roughly **$7/day**. Always finish with Phase 13 (teardown) unless you mean to keep them.
