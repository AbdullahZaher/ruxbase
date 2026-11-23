# W11-05: 168-Hour Continuous Staging Soak Evidence & Status Ledger

**Document:** `docs/architecture/W11-05-STAGING-SOAK-EVIDENCE.md`  
**Requirement ID:** `W11-05 / P1 / GATE`  
**Status:** **`PENDING_STAGING_EVIDENCE`** (`LAUNCH BLOCKED — DEDICATED STAGING VPS REQUIRED`)  
**Machine-Readable Status Record:** [`bench/soak/campaign-status.json`](../../bench/soak/campaign-status.json)  
**Main Candidate Baseline:** `d7b9a3dcd55ea14beadeeecaec065b23eeb5f345` (tree `891c2e1ac1d22b37a3b1cfa09de650c593ce8a1e`, post-PR #67 & PR #68, 17/17 CI green)  
**Date:** `2026-11-23`

---

## 1. Formal Gate Disposition: `PENDING_STAGING_EVIDENCE` (`BLOCKED` on Isolated Staging Host)

Per the Ruxbase Master Roadmap, Phase 0 Mandatory Isolation Boundaries, and Release Qualification Mandate:
- **Never simulate, extrapolate, or compress a 168-hour (7-day) continuous soak test.**
- **Never execute disruptive soak workloads or fault injections on a shared production VPS.**
- **Phase 0 Infrastructure Discovery Result (2026-11-23):**
  - The current execution VPS was inspected and confirmed to be a **shared production host** (2 vCPU, 7.8 GiB RAM with 2.1 GiB swap in active use, 29 GB free disk / 70% full, load average 1.5–4.2) running 27 active Docker containers (multiple live production web/API/worker/PostgreSQL/Redis stacks) plus 5 PM2 production applications and host Nginx.
  - No separate authorized staging VPS, isolated VM, or cloud provisioning credentials were present in the execution environment.
  - In strict compliance with Phase 0 Mandatory Isolation and Critical Stop Conditions, live staging deployment, the 60-minute pilot, and the 168-hour soak launch are formally recorded as **`BLOCKED`** pending provisioning of a dedicated staging VPS (recommended >= 4 vCPU, 8 GiB RAM, >= 100 GB SSD).
- Because 168 consecutive wall-clock hours have not elapsed on a dedicated external staging environment for this release candidate, `W11-05` remains **`PENDING_STAGING_EVIDENCE`**, and production deployment (`W11-08`) remains **`PRODUCTION_NO_GO`**.

---

## 2. Verified Soak Infrastructure, Continuous Workload Engine & Telemetry Readiness

All feasible repository engineering work for `W11-05` has been implemented and unit/configuration-verified in [`bench/soak/`](../../bench/soak/README.md) so the campaign can launch immediately once a dedicated staging VPS is available:

| Capability | Implementation Artifact | Verification Evidence |
| --- | --- | --- |
| **Persistent Isolated Staging Stack** | `bench/soak/deploy/docker-compose.staging.yml`, `bench/soak/deploy/postgres.conf`, `bench/soak/deploy/init-staging-roles.sql`, `bench/soak/deploy/systemd/*.service`, `*.timer` | Named persistent volumes, explicit CPU/RAM limits, loopback-only bindings, separated roles (`ruxbase_app` without `SUPERUSER`/`BYPASSRLS`, `ruxbase_migrator`, `ruxbase_wal`, `ruxbase_monitor`), dedicated `postgres-restore-target`, and unattended systemd supervision. Validated via `docker compose config`. |
| **7-Day Diurnal Continuous Workload Engine (Journeys A–H)** | `bench/soak/cmd/soak/`, `bench/soak/internal/schedule/`, `bench/soak/internal/journeys/` | Open-loop coordinated-omission-aware pacer + 7-day diurnal curve (10–85% of pilot-qualified capacity, >0 floor) + Auth, CRUD, Multi-Tenant RLS + continuous negative cross-tenant checks, persistent SSE + reconnects + delta sync, Durable Jobs/Workflows/Outbox worker (`soak worker`), HTTP + concurrent Multipart Storage + two-stage orphan GC, pgvector search, and periodic `EXPLAIN (ANALYZE, BUFFERS)`. Verified via `go test -race ./bench/soak/...`. |
| **60s PostgreSQL, WAL & Ruxbase Runtime Monitoring** | `bench/soak/internal/telemetry/`, `bench/soak/monitoring/prometheus.yml`, `bench/soak/monitoring/alerts.yml`, `bench/soak/monitoring/grafana/dashboards/ruxbase-w11-05-soak.json` | Samples `pg_stat_activity`, `pg_stat_database`, `pg_stat_wal`, `pg_stat_checkpointer`/`bgwriter`, `pg_replication_slots`, `pg_stat_replication` (explicitly emitting `NOT_CONFIGURED` when absent), `pg_stat_statements`, `/api/ops/metrics`, `/api/ops/alerts`, `/api/health?ready`, and host disk/FD headroom; 11 critical Prometheus alerts and 12-panel Grafana dashboard. |
| **Durable Campaign Clock, Safety Guardrails & Verifier** | `bench/soak/internal/state/`, `bench/soak/internal/safety/`, `bench/soak/internal/verifier/` | Immutable `officialStartUtc` / `earliestValidEndUtc` (never reset on restart), expected-results ledger reconciliation, fail-closed emergency stop (0 cross-tenant leaks, 0 checksum corruption, disk <15%, WAL >900 MiB), shared-production host isolation check, Phase 11 168h reconciler (5% heap drift gate), SHA-256 artifact manifest, and read-only A11 auditor (`soak audit`). Verified via `go test -race ./bench/soak/...`. |
| **Scheduled Fault Injection & Online Backup/Restore Automation** | `bench/soak/scripts/deploy-staging.sh`, `bench/soak/scripts/backup-verify.sh`, `bench/soak/scripts/fault-inject.sh`, `bench/soak/scripts/run-pilot-and-launch.sh` | Enforces host isolation preflight, dry-runs `./scripts/install-selfhosted.sh`, automates Journey H online backup -> isolated restore verification, and dispatches Hour 24 (worker `SIGKILL`), Hour 72 (bounded staging `LISTEN`/WAL disconnect), and Hour 120 (online backup/restore + `CheckReleaseBlockers`). Syntax-checked with `bash -n`. |
| **Core Ruxbase Ops & Resilience Instrumentation** | `core/ops.go`, `apis/ops.go`, `apis/studio_control_plane.go`, `core/jobs.go`, `core/storage.go` | Verified in CI (17/17 checks green on `d7b9a3dc` across PostgreSQL 17 and 18). |

---

## 3. Exact Blocked Deployment & Launch Steps for Authorized Operator

Once an authorized **dedicated staging VPS** (clean Linux host with Docker + Compose v2, Go 1.27, systemd, >= 4 vCPU, >= 8 GiB RAM, >= 100 GB SSD, zero unrelated production workloads) is provisioned:

1. **Clone & Deploy Isolated Staging Stack:**
   ```bash
   git clone https://github.com/AbdullahZaher/ruxbase.git /srv/ruxbase-soak/repo
   cd /srv/ruxbase-soak/repo
   ./bench/soak/scripts/deploy-staging.sh
   ```
2. **Run Genuine 60-Minute Pilot, Freeze Candidate & Launch 168-Hour Unattended Soak:**
   ```bash
   ./bench/soak/scripts/run-pilot-and-launch.sh
   ```
   This script enforces:
   - `soak preflight --check-isolation=true`
   - 60-minute continuous pilot (`soak pilot --duration=60m`) + online backup/restore rehearsal (`backup-verify.sh`)
   - Pilot Exit Gate (`soak reconcile --required-hours=1.0`)
   - Candidate provenance freeze (`soak freeze` -> `candidate-manifest.json`)
   - Systemd installation & activation (`ruxbase-soak.service`, `ruxbase-soak-backup.timer`, `ruxbase-soak-faults.timer`)
3. **Post-168-Hour Reconciliation & Independent A11 Certification:**
   ```bash
   /srv/ruxbase-soak/soak reconcile \
     --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" \
     --required-hours 168

   # Independent A11 read-only audit (conducted separately after evidence freeze):
   /srv/ruxbase-soak/soak audit \
     --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" \
     --required-hours 168
   ```
4. **Update Gate Disposition:**
   - Only after A11 issues `W11-05_VERIFIED` on a complete 168-hour frozen evidence bundle may `W11-05` be proposed for closure, after which `W11-08` still requires explicit human owner authorization and a signed immutable tag.
