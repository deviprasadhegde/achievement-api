# API Contract — v1

This is the agreement between the API and its clients. **We write it before any code**, and in Phase 5 the tests check the code against it. If code and contract disagree, one of them is a bug.

---

## 1. Conventions (apply to every endpoint)

| Topic | Rule |
|---|---|
| Base path | `/api/v1` — the version is in the URL so v2 can live beside it. |
| Format | JSON, UTF‑8. Responses always carry `Content-Type: application/json`. |
| Dates | `YYYY-MM-DD` (ISO 8601 calendar date), no time zone. Ranges are **inclusive** on both ends. |
| IDs | Positive integers (`1 … 9223372036854775807`). |
| Lists in query strings | Comma‑separated: `accountIds=101,102,103`. Duplicates are removed and the list is sorted before use. |
| Authentication | Header `X-API-Key: <key>` on every `/api/v1/*` call. |
| Request ID | Send `X-Request-ID` (≤ 64 chars, `[A-Za-z0-9_-]`) or the server generates one. It is always echoed in the response header and in error bodies, and appears in every log line for that request. |
| Cache info | Response header `X-Cache: HIT \| MISS \| BYPASS` (header only — never in the body, because the body itself is what gets cached). |
| Client caching | `Cache-Control: no-store`. Caching happens on the server (Redis), not in clients. |
| Rate limit headers | `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (seconds); `Retry-After` on `429`. |
| Timeouts | The server gives each request 5 s total and each database query 3 s. |

### Why `GET` with query parameters?

Both endpoints *read* data and change nothing, so `GET` is the correct HTTP method: it's safe, idempotent, retryable, and visible in access logs. The alternative — `POST` with a JSON body — is common when filter lists get huge, but our lists are capped (100 accounts, 50 KPIs), which keeps URLs well under the ~8 KB limit that load balancers enforce.

---

## 2. Aggregation rules (the most important section)

### 2.1 What a "data point" is
One row in `kpi_values` = the value of **one KPI** for **one account** on **one date**.

### 2.2 The options and why they matter
KPIs are not all alike, and the correct way to combine them depends on what they measure:

| KPI kind | Example | Correct roll‑up over days/accounts | Wrong roll‑up |
|---|---|---|---|
| **Additive / flow** | revenue, orders, signups | `sum` | avg hides volume |
| **Rate / ratio** | conversion %, NPS | `avg` (ideally *weighted* by the denominator) | `sum` of percentages is meaningless |
| **Gauge / snapshot** | headcount, account balance | latest value, or `avg` | `sum` across days double‑counts |
| **Extremes** | peak latency, max daily users | `min` / `max` | — |

The spec gives us KPI **IDs** but no metadata saying which kind each KPI is. If we picked one roll‑up (say `sum`), we'd be silently wrong for every rate or gauge KPI.

### 2.3 Default we adopt
**For every requested KPI we return all five statistics, computed over the matching data points:**

| Field | Definition | Type |
|---|---|---|
| `count` | number of data points | integer |
| `sum` | Σ value | decimal |
| `avg` | `sum / count` — the mean **per data point** (rounded to 4 dp) | decimal |
| `min` | smallest value | decimal |
| `max` | largest value | decimal |

The client picks the statistic that suits the KPI. That's the most honest default.

Precise rules:

1. **Totals are computed from raw data points, never from per‑account results.** "Average of averages" is wrong whenever accounts have different numbers of days: account A has 1 day at 100, account B has 9 days at 0 → avg of averages = 50, true per‑point avg = 10. We return 10.
2. **Every requested KPI appears in the output**, even with no data. In that case `count = 0` and `sum`, `avg`, `min`, `max` are `null`. ("No data" and "zero" are different facts, so we keep them apart.)
3. **Decimals** are stored as `NUMERIC(20,4)` and serialised as JSON numbers from their exact decimal form (no float rounding), with up to 4 decimal places.
4. **Ordering** is deterministic: accounts by `accountId` ascending, KPIs by `kpiId` ascending. Same request → byte‑identical response, which also makes caching safe.

**Upgrade path (not in v1):** add a `kpi_definitions` table with an `aggregation` column (`sum|avg|last|max…`) and a `value` field that applies it, plus a weighted average for ratio KPIs.

---

## 3. Endpoint 1 — Account performance

```
GET /api/v1/accounts/performance
```

Aggregated KPI performance for a list of accounts over a date range.

### Query parameters

| Name | Required | Rules |
|---|---|---|
| `accountIds` | yes | 1–100 unique positive integers, comma‑separated |
| `kpiIds` | yes | 1–50 unique positive integers, comma‑separated |
| `startDate` | yes | valid `YYYY-MM-DD` |
| `endDate` | yes | valid `YYYY-MM-DD`, `≥ startDate`, range `≤ 366` days (inclusive) |

Unknown query parameters → `400` (catches typos like `accountId=`).

### Example request

```bash
curl -s "http://localhost:8080/api/v1/accounts/performance?accountIds=101,102,999&kpiIds=1,2&startDate=2026-01-01&endDate=2026-01-31" \
  -H "X-API-Key: dev-key-123"
```

### `200 OK` response

```json
{
  "data": {
    "startDate": "2026-01-01",
    "endDate": "2026-01-31",
    "accounts": [
      {
        "accountId": 101,
        "accountName": "Acme Corp",
        "ownerId": 7,
        "kpis": [
          { "kpiId": 1, "count": 31, "sum": 15500.0000, "avg": 500.0000, "min": 120.5000, "max": 980.0000 },
          { "kpiId": 2, "count": 0,  "sum": null,      "avg": null,     "min": null,     "max": null }
        ]
      },
      {
        "accountId": 102,
        "accountName": "Globex",
        "ownerId": 7,
        "kpis": [
          { "kpiId": 1, "count": 31, "sum": 6200.0000, "avg": 200.0000, "min": 50.0000, "max": 410.0000 },
          { "kpiId": 2, "count": 31, "sum": 93.0000,   "avg": 3.0000,   "min": 1.0000,  "max": 5.0000 }
        ]
      }
    ],
    "totals": {
      "kpis": [
        { "kpiId": 1, "count": 62, "sum": 21700.0000, "avg": 350.0000, "min": 50.0000, "max": 980.0000 },
        { "kpiId": 2, "count": 31, "sum": 93.0000,    "avg": 3.0000,   "min": 1.0000,  "max": 5.0000 }
      ]
    },
    "notFoundAccountIds": [999]
  },
  "meta": { "requestId": "c1f3a7e2-6a3b-4d8e-9a51-2f0f4b9e7d10" }
}
```

**Unknown accounts:** this is a batch lookup, so unknown IDs don't fail the whole request. They are listed in `notFoundAccountIds` and excluded from `totals`. If *none* exist you still get `200` with `accounts: []`.

---

## 4. Endpoint 2 — Account‑owner performance

```
GET /api/v1/owners/{ownerId}/performance
```

KPI performance aggregated across **all accounts that belong to the owner**.

### Parameters

| Name | In | Required | Rules |
|---|---|---|---|
| `ownerId` | path | yes | positive integer |
| `kpiIds` | query | yes | 1–50 unique positive integers |
| `startDate` | query | yes | as above |
| `endDate` | query | yes | as above |
| `includeAccounts` | query | no | `true` / `false` (default `false`). When `true`, adds a per‑account breakdown. |

### Example request

```bash
curl -s "http://localhost:8080/api/v1/owners/7/performance?kpiIds=1,2&startDate=2026-01-01&endDate=2026-01-31&includeAccounts=true" \
  -H "X-API-Key: dev-key-123"
```

### `200 OK` response

```json
{
  "data": {
    "ownerId": 7,
    "startDate": "2026-01-01",
    "endDate": "2026-01-31",
    "accountCount": 2,
    "kpis": [
      { "kpiId": 1, "count": 62, "sum": 21700.0000, "avg": 350.0000, "min": 50.0000, "max": 980.0000 },
      { "kpiId": 2, "count": 31, "sum": 93.0000,    "avg": 3.0000,   "min": 1.0000,  "max": 5.0000 }
    ],
    "accounts": [
      { "accountId": 101, "accountName": "Acme Corp", "kpis": [ "…same shape as endpoint 1…" ] },
      { "accountId": 102, "accountName": "Globex",    "kpis": [ "…" ] }
    ]
  },
  "meta": { "requestId": "8d0c…" }
}
```

`accounts` is omitted when `includeAccounts=false`. `accountCount` counts every account the owner has, including those with no data in the range.

**Unknown owner:** no account has this `owner_id` → `404 OWNER_NOT_FOUND`.

---

## 5. Errors

Every error uses the same shape, so clients need one error handler:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "one or more parameters are invalid",
    "details": [
      { "field": "endDate",  "issue": "must be on or after startDate" },
      { "field": "kpiIds",   "issue": "must contain between 1 and 50 ids" }
    ],
    "requestId": "c1f3a7e2-…"
  }
}
```

Validation reports **all** problems at once, not just the first.

| Status | `code` | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Missing/malformed parameter, rule violated, unknown parameter |
| 401 | `UNAUTHORIZED` | Missing or invalid `X-API-Key` (same message for both, so callers can't probe which keys exist) |
| 404 | `OWNER_NOT_FOUND` | Owner has no accounts |
| 404 | `NOT_FOUND` | Unknown route |
| 405 | `METHOD_NOT_ALLOWED` | e.g. `POST` to a `GET` endpoint (`Allow` header included) |
| 429 | `RATE_LIMITED` | Over the per‑key limit (default 100 requests/min); `Retry-After` set |
| 500 | `INTERNAL_ERROR` | Unexpected bug. The message is generic; details go to logs only, linked by `requestId`. |
| 503 | `SERVICE_UNAVAILABLE` | Database unreachable or query timed out. Redis being down is **not** a 503 — we bypass the cache instead. |

---

## 6. Caching behaviour

| Aspect | Rule |
|---|---|
| Pattern | Cache‑aside: check Redis → on miss query Postgres → store → return. |
| Key | `perf:v{N}:accounts:{sha256(normalised params)}` and `perf:v{N}:owner:{ownerId}:{sha256(normalised params)}`. Normalised = deduped, sorted IDs + dates, so `accountIds=2,1` and `1,2` share an entry. Hashing keeps keys short and avoids putting raw input into key names. |
| Value | The exact JSON `data` object (compressed if large). |
| TTL | 300 s by default (`CACHE_TTL` env var). This is the maximum staleness. |
| Invalidation | `{N}` is a namespace version stored in Redis at `perf:version`. Whatever loads new KPI data runs `INCR perf:version`, and every old key becomes unreachable instantly (they expire on their own via TTL). No scanning or deleting thousands of keys. |
| Redis failure | Fail‑open: 50 ms timeout on cache calls, then query Postgres directly, `X-Cache: BYPASS`, warning log, `cache_errors_total` metric. |
| Stampede protection | Within a pod, concurrent identical misses are collapsed into one DB query (`singleflight`). |
| Not cached | Error responses (400/404/5xx). |

---

## 7. Operational endpoints (no API key; not exposed through the public load balancer)

| Endpoint | Used by | Behaviour |
|---|---|---|
| `GET /healthz` | Kubernetes **liveness** probe | `200 {"status":"ok"}` if the process is alive. Never checks dependencies — otherwise a DB blip would make Kubernetes restart every pod at once. |
| `GET /readyz` | Kubernetes **readiness** probe, ALB health check | `200` if Postgres answers a ping within 1 s; `503` otherwise, or while shutting down. Redis is *not* checked (fail‑open). |
| `GET /metrics` | Prometheus / CloudWatch agent | Prometheus text format: request counts, latency histograms, cache hit/miss, DB pool stats. |
