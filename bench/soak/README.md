# Ruxbase W11-05 — 168-Hour Continuous Staging Soak Harness

Dedicated, persistent, production-representative staging soak platform for closing Release Gate **`W11-05`**.

Unlike `bench/stress/` (which runs disposable short-duration stress suites with `trap cleanup EXIT`), `bench/soak/` is designed to operate **unattended for 168 consecutive real wall-clock hours** on a **dedicated, isolated staging VPS** with persistent volumes, separated PostgreSQL roles, continuous diurnal customer journeys, 60-second telemetry, scheduled controlled fault injections, online backup/restore verification, fail-closed safety guardrails, and strictly read-only independent A11 certification.

---

## 1. Mandatory Isolation Boundaries

- **Dedicated staging VPS or isolated VM ONLY.** `deploy-staging.sh` and `soak preflight` fail closed if unrelated or production containers are detected on the host.
- **No public exposure** of PostgreSQL, Prometheus, Grafana, exporters, or admin endpoints (`127.0.0.1` loopback bindings only; PostgreSQL has no published host port).
- **Strict least-privilege PostgreSQL roles** provisioned by `deploy/init-staging-roles.sql`:
  - `ruxbase_app` (`NOSUPERUSER`, `NOBYPASSRLS`) — runtime serving & worker
  - `ruxbase_migrator` (`NOSUPERUSER`, `NOBYPASSRLS`) — schema migrations
  - `ruxbase_wal` (`REPLICATION`, `NOSUPERUSER`, `NOBYPASSRLS`) — logical replication
  - `ruxbase_monitor` (`pg_monitor`, `NOSUPERUSER`, `NOBYPASSRLS`) — read-only telemetry & `postgres_exporter`
- **Persistent named Docker volumes** — never auto-destroyed on runner exit.
- **Separate restore verification target** (`postgres-restore-target`) — online backup/restore drills never overwrite the running staging database.

---

## 2. Layout

| Path | Purpose |
| --- | --- |
| `deploy/docker-compose.staging.yml` | Persistent staging stack (`postgres`, `postgres-restore-target`, `ruxbase`, `soak-worker`, `soak-engine`, `postgres-exporter`, `node-exporter`, `prometheus`, `grafana`) |
| `deploy/postgres.conf` | PostgreSQL 17/18 staging config (`wal_level=logical`, `max_slot_wal_keep_size=1GB`, `pg_stat_statements`, autovacuum & lock telemetry) |
| `deploy/init-staging-roles.sql` | Separated least-privilege role & extension bootstrap (`vector`, `pgcrypto`, `pg_stat_statements`) |
| `deploy/systemd/` | Supervised `ruxbase-soak.service`, `ruxbase-soak-backup.timer`, and `ruxbase-soak-faults.timer` units for unattended 7-day operation |
| `monitoring/prometheus.yml` | 30s scrape config for Ruxbase `/api/ops/metrics?format=prometheus`, PostgreSQL, host, soak engine, and worker |
| `monitoring/alerts.yml` | 11 critical safety & integrity alert rules |
| `monitoring/grafana/` | Auto-provisioned Prometheus datasource & 12-panel W11-05 Grafana dashboard |
| `cmd/soak/` | Go CLI (`preflight`, `worker`, `pilot`, `freeze`, `run`, `verify`, `reconcile`, `audit`) |
| `internal/schedule/` | 24-hour / 7-day diurnal traffic curve (10%–85% of pilot-qualified capacity, jitter, bursts, >0 RPS floor) |
| `internal/journeys/` | Customer Journeys A–H (Auth, CRUD, Multi-Tenant RLS + negative attacks, persistent SSE + reconnects + sync, Durable Jobs/Workflows/Outbox, File & Multipart Storage + orphan GC, pgvector + `EXPLAIN (ANALYZE, BUFFERS)`) |
| `internal/telemetry/` | 60-second PostgreSQL (`pg_stat_*`, WAL, slots/standbys marked `NOT_CONFIGURED` when absent), Ruxbase ops, and host disk/FD collector |
| `internal/state/` | Durable checkpointing (never resets the 168h clock on restart), expected-results ledger, interruption & alert logs, SHA-256 manifest, Prometheus `/metrics` |
| `internal/safety/` | Fail-closed emergency stop guardrails & shared-production isolation check |
| `internal/verifier/` | Periodic integrity verifier, Phase 11 168-hour reconciler (5% memory drift gate), and strictly read-only A11 auditor |
| `scripts/deploy-staging.sh` | Isolation-checked persistent staging deployer |
| `scripts/backup-verify.sh` | Online `pg_dump` under active writes -> isolated `pg_restore` -> schema/RLS/row verification |
| `scripts/fault-inject.sh` | Scheduled Hour 24 (worker `SIGKILL`), Hour 72 (bounded staging `LISTEN`/WAL `pg_terminate_backend`), and Hour 120 (online backup/restore + `CheckReleaseBlockers`) drills |
| `scripts/run-pilot-and-launch.sh` | End-to-end gated operator launcher (Preflight -> 60m Pilot -> Exit Gate -> Candidate Freeze -> 168h systemd launch) |

---

## 3. Operator Execution Sequence (Dedicated Staging VPS)

Run on an authorized, dedicated staging VPS (recommended >= 4 vCPU, 8 GiB RAM, >= 100 GB SSD, Docker + Compose v2 + Go 1.27 + systemd):

```bash
# 1. Clone frozen candidate checkout into /srv/ruxbase-soak/repo
git clone https://github.com/AbdullahZaher/ruxbase.git /srv/ruxbase-soak/repo
cd /srv/ruxbase-soak/repo

# 2. Deploy persistent staging + monitoring stack (enforces host isolation)
./bench/soak/scripts/deploy-staging.sh

# 3. Execute genuine 60-minute pilot, backup rehearsal, candidate freeze, and launch 168-hour systemd campaign
./bench/soak/scripts/run-pilot-and-launch.sh
```

After **168 consecutive wall-clock hours** have elapsed:

```bash
# 4. A01 Final Reconciliation & Cryptographic Evidence Manifest
/srv/ruxbase-soak/soak reconcile \
  --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" \
  --required-hours 168

# 5. Strictly Read-Only Independent A11 Operational Certification
/srv/ruxbase-soak/soak audit \
  --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" \
  --required-hours 168
```

## 4. Non-Negotiable Qualification Rules

- Pilot time **never** counts toward the 168-hour qualification clock.
- Restarting `soak run` **never** resets `officialStartUtc` or `earliestValidEndUtc`; every restart or gap is logged to `interruptions.jsonl`.
- Any cross-tenant exposure, storage checksum corruption, or committed data loss immediately triggers emergency stop and disqualifies the campaign (`W11-05_FAILED`).
- Any missing mandatory evidence is marked `INCOMPLETE` / `W11-05_INCOMPLETE_EVIDENCE`, never silently passed.
- Production remains **`PRODUCTION NO-GO`** until both `W11-05` (`W11-05_VERIFIED`) and independent human owner authorization (`W11-08`) are complete.
