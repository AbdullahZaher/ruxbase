# Wave 08: Production Reliability, Operations & Release Gates

**Document:** `docs/architecture/WAVE08-PRODUCTION-RELIABILITY-AND-OPERATIONS.md`  
**Target Wave:** Wave 08 — Production Reliability & Operations  
**Status:** Authoritative Engineering & Operational Runbook  

---

## 1. Self-Hosted Installation & Private-by-Default Credentials (W08-01)

Ruxbase supports single-command reproducible installation via `scripts/install-selfhosted.sh`:
1. Generates cryptographically random `POSTGRES_PASSWORD`, `APP_PASSWORD`, and 64-hex-character `RUXBASE_ENCRYPTION_KEY` into `.env` with `chmod 0600` permissions (never printed to stdout or committed).
2. Enforces loopback-only database exposure (`127.0.0.1`) unless explicitly bound to a private VPC interface.
3. Separates PostgreSQL runtime role (`ruxbase_app`, `NOSUPERUSER NOBYPASSRLS`) from offline DDL migration (`ruxbase_migrator`) and initial provisioning (`ruxbase_provisioner`).

---

## 2. Backup, Restore & Storage Integrity Rehearsal (W08-02)

- Database backups leverage `pg_dump` custom format (`data.pg.dmp`) packaged atomically alongside `storage/` / `ruxbase_data` blobs via `app.CreateBackup` under cluster advisory lock (`_cluster` lock prevents concurrent backups).
- Restores execute via `ruxbase restore <archive.zip>` (`core.RestoreBackupArchive`), restoring both PostgreSQL state and data-directory objects into clean target databases.
- Post-restore verification MUST confirm:
  1. `_migrations` ledger matches active build migrations (`CheckMigrationSafety`).
  2. Every row in `_storage_objects` resolves to a readable object whose SHA-256 checksum matches `etag`.
  3. Zero P0 privilege regressions (runtime connection is `NOSUPERUSER` and RLS policies remain enabled).

---

## 3. Versioned Upgrades, Schema Evolution & Rollback Runbook (W08-03)

1. **Pre-Upgrade Preflight:** Take an atomic backup (`ruxbase backup`), verify disk space, and run `ruxbase check --json`.
2. **Offline Migration Gate:** Execute schema upgrades under the dedicated `ruxbase_migrator` identity before routing traffic to new application binaries. DDL operations respect `CollectionDDLLockTimeout`.
3. **Rollback vs Forward-Fix:**
   - **Application-only defect:** Drain traffic, revert container image tag / binary to previous release.
   - **Incompatible schema failure:** Do not run destructive ad-hoc `DROP` commands; restore the pre-upgrade PostgreSQL + storage backup snapshot via `ruxbase restore`, or deploy a forward-fix additive migration once verified on a disposable database clone.

---

## 4. Observability, Metrics, Structured Logs & Alerting (W08-04)

Superuser-authenticated endpoints registered under `/api/ops/*` expose operational telemetry without leaking host state to anonymous or tenant callers:
- `GET /api/ops/metrics` (`?format=prometheus` or JSON): Exposes Go runtime heap/goroutines, PostgreSQL pool saturation (`openConns`, `inUse`, `idle`, `waitCount`, `waitMs`), database size, `_jobs` pending/failed/dead counts, `_outbox_events` backlog, and cluster connectivity.
- `GET /api/ops/capacity`: Evaluates configured PostgreSQL connection budget against node footprint profiles (`Lite` vs `Standard`).
- `GET /api/ops/alerts`: Evaluates actionable operational alerts:
  - `dlq_non_empty` (Warning): Dead-letter jobs awaiting operator inspection/replay.
  - `outbox_failed` (Warning): Failed transactional outbox events requiring attention.
  - `db_pool_wait` (Warning/Critical): Requests blocking on PostgreSQL pool exhaustion.
  - `wal_retention_high` (Warning): Replication slot WAL retention exceeding `cluster.RetainedWALWarningSize`.
  - `cluster_listen_down` (Critical): Cluster LISTEN channel disconnected in multi-node deployment.

---

## 5. Capacity & Connection Budgeting: Lite vs Standard Footprints (W08-05)

Per instance PostgreSQL connection budget (`docs/OPERATIONS.md`):
$$\text{Node Connections} = \text{DataMaxOpenConns} + \text{AuxMaxOpenConns} + 1\ (\text{LISTEN}) + 2\ (\text{Replication Leader Only})$$

| Profile | Nodes | Main Pool / Node | Aux Pool / Node | Max Total Postgres Conns | Recommended Host Footprint |
|---|---|---|---|---|---|
| **Lite (Single Node)** | 1 | 20 | 5 | 28 | 1–2 vCPU, 1–2 GB RAM, local or managed PG 17/18 |
| **Standard (HA App)** | 3 | 20 | 5 | 80 (fits default PG `max_connections=100`) | 2–4 vCPU/node, 4 GB RAM/node, managed PG 17/18 |

*Note: Measured runtime allocations and active pool counts are emitted dynamically at runtime via `/api/ops/capacity` and `/api/ops/metrics`; no unmeasured throughput benchmarks are claimed.*

---

## 6. Qualified Enterprise HA, PITR, RPO & RTO Profile (W08-06)

- **Scope Boundary:** A single PostgreSQL server provides durability (`wal_level=logical` / synchronous commit) but not zero-downtime host failover.
- **Enterprise HA Requirement:** Requires synchronous or asynchronous streaming replication primary + standby (e.g., Patroni or cloud-managed Multi-AZ PostgreSQL 17/18) paired with continuous WAL archiving (`archive_command` to encrypted S3) for Point-In-Time Recovery (PITR).
- **Honest RPO / RTO Qualification:**
  - **Lite Profile (Periodic Snapshots):** RPO equals snapshot interval (e.g., up to 24h for daily backups); RTO bounded by `pg_restore` + storage restore duration.
  - **Enterprise WAL/PITR Profile (When external continuous WAL archiving is configured):** Target RPO <= 5 minutes (archive interval), Target RTO <= 15 minutes for standby promotion or PITR recovery. Exact guarantees depend on verified periodic restore drills against the target infrastructure.

---

## 7. Release-Blocking P0 Conditions (W08-07)

Release promotion MUST be blocked automatically if any of the following conditions is detected by `core.CheckReleaseBlockers` or pre-release drills:
1. Runtime database role holds `SUPERUSER` or `BYPASSRLS` privileges.
2. Unapplied pending system migrations exist or `_migrations` history diverges.
3. Storage objects fail checksum/existence verification against `_storage_objects`.
4. Unauthenticated requests can read superuser `/api/ops/*` telemetry or cross-tenant data.
