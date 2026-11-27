# Wave 11: 168-Hour Soak Plan & Matched Supabase Self-Hosted Parity Protocol

**Document:** `docs/architecture/WAVE11-SOAK-AND-PARITY-PLAN.md`  
**Target Wave:** Wave 11 — Independent Certification & Release (`W11-05`, `W11-06`)  

---

## 1. 168-Hour Continuous Soak Execution Plan (`W11-05`)

Before flipping a release candidate to production `GO` for high-availability SLA profiles, operators execute a continuous **168-hour (7-day) unattended soak** against a disposable staging deployment backed by PostgreSQL 17/18 + pgvector:

### 1.1 Soak Workload Profile
- **Steady Traffic Mix:** Authenticated CRUD reads/writes, RLS-filtered list queries, realtime SSE subscriptions + broadcast/presence ticks, periodic durable job enqueue/lease/completion, and isolated JS compute invocations.
- **Fault Injections (Scheduled at 24h, 72h, and 120h):**
  - Worker crash mid-lease (`ClaimNext` expiry -> `ReclaimStaleLeases` recovery).
  - Controlled PostgreSQL connection interruption (`LISTEN` reconnect & readiness recovery via `GET /api/health?ready`).
  - Periodic full atomic backup (`ruxbase backup`) + offline SHA-256 verification (`core.CheckReleaseBlockers`).

### 1.2 Pass / Anomaly Disposition Criteria
- Zero unbounded goroutine growth or monotonic heap leak (`ruxbase_runtime_goroutines`, `ruxbase_runtime_live_heap_bytes` / `ruxbase_runtime_heap_alloc_bytes` via `GET /api/ops/metrics`).
- Zero connection pool starvation (`ruxbase_db_pool_wait_count` stable).
- Zero uncommitted outbox events or un-reclaimed dead jobs not explicitly flagged by `dlq_non_empty`.
- *Local/CI Status:* Smoke soak harnesses execute deterministically inside `tests/e2e/` and `.github/workflows/benchmark-baseline.yml`; the continuous 168-hour wall-clock staging soak remains an explicit pre-GA operational gate prior to human maintainer tag signing (`W11-08`).

---

## 2. Fair Matched Supabase Self-Hosted Comparison Protocol (`W11-06`)

All competitive claims against **Supabase Self-Hosted** (never Supabase Cloud) MUST follow strict matched-constraint measurement rules extending `bench/run.sh` and `docs/BENCHMARKS.md`:
1. **Identical Hardware & Cgroup Budgets:** Identical CPU cores, RAM limits, disk IOPS, and PostgreSQL version/tuning.
2. **Production-Equivalent Authorization:** Both stacks enforce equivalent RLS / policy rules and authenticated JWT validation (not open public bypass tables).
3. **Full Dependency Accounting:** Total memory and CPU samples include EVERY mandatory container required for the stack (for Ruxbase: app node(s) + external PostgreSQL; for Supabase Self-Hosted: Postgres + PostgREST + GoTrue + Realtime + Storage + Kong + Meta/Studio).
4. **Raw Artifacts Published:** Commit exact `.environment.txt`, `.resources.jsonl`, and percentile latency histograms (P50/P95/P99) with zero fabricated extrapolations.
