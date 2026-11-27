# W11-05: 168-Hour Continuous Staging Soak Evidence & Status Ledger

**Document:** `docs/architecture/W11-05-STAGING-SOAK-EVIDENCE.md`  
**Requirement ID:** `W11-05 / P1 / GATE`  
**Status:** **`PENDING_STAGING_EVIDENCE`** (`PILOT_BLOCKED_PENDING_ISOLATED_STAGING`)  
**Machine-Readable Status Record:** [`bench/soak/campaign-status.json`](../../bench/soak/campaign-status.json)  
**Main Candidate Baseline:** `d7b9a3dcd55ea14beadeeecaec065b23eeb5f345` (tree `891c2e1ac1d22b37a3b1cfa09de650c593ce8a1e`, post-PR #67 & PR #68, 17/17 CI green)  
**Active Remediation Branch / PR:** `chore/w11-05-staging-soak-harness-20261123` (PR #69)  
**Date:** `2026-11-27`

---

## 1. Formal Gate Disposition: `PENDING_STAGING_EVIDENCE` (`PILOT_BLOCKED_PENDING_ISOLATED_STAGING`)

Per the Ruxbase Master Roadmap, Phase 0 Mandatory Isolation Boundaries, and Release Qualification Mandate:
- **Never simulate, extrapolate, or compress a 168-hour (7-day) continuous soak test.**
- **Never bypass mandatory staging isolation on a shared production VPS.**
- Because 168 consecutive wall-clock hours have not elapsed on a dedicated, isolated external staging environment for this release candidate, `W11-05` remains **`PENDING_STAGING_EVIDENCE`** and production deployment (`W11-08`) remains **`PRODUCTION_NO_GO`**.

---

## 2. Forensic Reconciliation of Historical Pilot `W11-05-PILOT-20261123T110530Z` (`PILOT_FAILED`)

An initial user-authorized 60-minute pilot on shared host `srv1037165` (`W11-05-PILOT-20261123T110530Z`, evidence preserved read-only at `/srv/ruxbase-soak/evidence/W11-05-PILOT-20261123T110530Z/`) triggered emergency stop after 15 minutes (`2026-11-23T11:20:44Z`) with `107 committed ledger row(s) missing from PostgreSQL`, `10.04%` reported heap drift, and missing backup verification evidence. That campaign remains permanently recorded as **`PILOT_FAILED`**.

Independent PostgreSQL forensic reconciliation executed against `ruxbase_staging` conclusively established the root causes (`ROOT_CAUSE_CONCLUSIVELY_ESTABLISHED`):

1. **107-Record Forensic Classification (`0` genuinely missing):**
   - Total `ledger.jsonl` entries: `397` (`215` creates, `182` deletes). Exact verifier tail window (`250` entries minus `deletedSet`) = **`107` records**.
   - **`83 / 107`**: Physically present in `public.soak_items` with exact `tenant_id`, `title`, and create `checksum` (`sha256(op_id + "|" + title)`).
   - **`24 / 107`**: Physically present in `public.soak_items` as valid post-create updates via `runCRUDUpdate` (all `24` match `tenant_id`, `updated <= 2026-11-23T11:20:44.401Z`, and update `checksum = sha256(id + "|" + title)`).
   - **`0 / 107`**: Genuinely missing, corrupted, or cross-tenant exposed (`null_tenants = 0`, `forged_rows = 0`, all `182` committed deletes confirmed absent).
   - **Root Cause:** `cmdRunCampaign` discarded `--db` (`ruxbase_app`, table owner) in favor of `--monitor-db` (`ruxbase_monitor`, `pg_monitor` without `BYPASSRLS` or table ownership). Because `soak_items` has PostgreSQL Row-Level Security enabled (`relrowsecurity = t` with policies scoped to `{ruxbase_anon, ruxbase_api, ruxbase_authenticated}`), queries executed by `ruxbase_monitor` returned `0` rows (`pgx.ErrNoRows`), which `verifier.RunIntegrityCheck` misclassified as `MissingCommittedRows` while `runCRUDUpdate` neither wrote ledger entries nor shared a canonical checksum formula.
2. **Memory Drift Investigation (`10.04%` Pilot Drift vs Actual Stability):**
   - `ruxbase-soak-app` over 4.4 days of continuous uptime remained completely flat (`HeapAllocBytes`: `3.04 MiB` -> `3.58 MiB`, `Goroutines`: `18` -> `17`, `VmRSS`: `23.25 MiB`, `OpenFDs`: `14`, `dbPool.waitCount`: `0`).
   - During the 15-minute pilot, `computeTrends` compared two single unsmoothed instantaneous `HeapAllocBytes` snapshots (`5.38 MiB` at tick 1 vs `5.92 MiB` at tick 15) inside a ±35% Go GC sawtooth (`3.77–8.57 MiB` across 343 GC cycles).
   - A separate **real unbounded memory leak** (`117.4 MiB` RSS over 4 days) was identified in `bench/soak/internal/journeys/core_worker.go` (`seenJobEffects` and `seenOutbox` unbounded `sync.Map`s), and remediated with bounded O(1) `boundedIDSet` ring caches.
3. **Additional Operational Defects Identified & Remediated:**
   - `backup-verify.sh`: `.env.staging` overwrote caller `SOAK_CAMPAIGN_ID` (`pilot-pending`), and multi-statement `psql -c "DROP DATABASE ...; CREATE DATABASE ...;"` failed with `ERROR: DROP DATABASE cannot run inside a transaction block`.
   - `cmdReconcile`: Mutated and rewrote `final-reconciliation.json` *after* generating `SHA256SUMS.txt`, causing post-freeze manifest verification failure.
   - `telemetry.go`: Misclassified Ruxbase's own logical realtime WAL stream in `pg_stat_replication` as `standbyStatus: CONFIGURED`.
   - `soak-engine` / Prometheus: `ruxbase-ops.token` expired after 24 hours without refresh on disk (`401 Unauthorized` on Prometheus scrapes).
   - `soak-worker`: Ran unbound (`--campaign-id=pilot-pending`) and remained active after pilot termination (`228,614` jobs / `190,511` outbox / `423` storage objects processed cleanly without touching `soak_items`). Worker was safely suspended (`docker stop -t 30 ruxbase-soak-worker`) after full state capture, preserving all database volumes and unrelated production containers.

---

## 3. Verified P1 Remediation & Engineering Evidence (PR #69)

| Capability | Remediation Artifact | Verification Evidence |
| --- | --- | --- |
| **Post-GC Live Heap & Process Telemetry** | `core/ops.go` (`readLiveHeapBytes` via `runtime/metrics` `/gc/heap/live:bytes`, `HeapInuseBytes`, `RSSBytes`, `OpenFDs`), `tests/e2e/ops_reliability_test.go` | Verified in `TestOps_ObservabilityMetricsCapacityAndAlertsSecurity` on PG 17 and PG 18 (standard & `RUXBASE_RLS_STRICT=true`). |
| **RLS-Aware Verifier & Error Attribution** | `bench/soak/internal/verifier/verifier.go` (`RunIntegrityCheck`, `ReconstructExpectedLedger`) | Distinguishes `OWNER_VISIBLE`, `HIDDEN_BY_RLS`, `NO_SELECT_PRIVILEGE`, SQLSTATE `42501`, connection/query errors, resurrected deletes, checksum mismatches, and confirmed missing rows; performs live HTTP + strict-RLS (`SET LOCAL ROLE ruxbase_api`) verification. Verified via `TestPostgreSQL_SoakRLSVerifierLifecycleAndFailures` (`-race`) on PG 17 & PG 18. |
| **Durable Intent/Ack Ledger & Canonical Checksums** | `bench/soak/internal/state/state.go`, `bench/soak/internal/journeys/journeys.go` | Records durable `fsync` intent & ack entries for 100% of creates/updates/deletes, propagates ledger write failures, uses single `CanonicalRecordChecksum(opID, title)`, serializes per-record mutations, and removes no-op fakes. |
| **Worker Leak Fix & Campaign Binding** | `bench/soak/internal/journeys/core_worker.go` | Replaces unbounded `sync.Map`s with O(1) `boundedIDSet`, rejects placeholder campaign IDs, emits campaign-scoped `worker-heartbeat.json`. |
| **Backup, Manifest, Token & Qualification Gates** | `bench/soak/scripts/backup-verify.sh`, `run-pilot-and-launch.sh`, `bench/soak/cmd/soak/main.go`, `telemetry.go` | Preserves caller `SOAK_CAMPAIGN_ID`, separates `DROP/CREATE DATABASE` transactions, writes `SHA256SUMS.txt` immutably last, refreshes `ruxbase-ops.token`, separates logical WAL consumers from physical standbys, verifies deployed binary SHA-256, and blocks reusing failed pilots/campaigns. |

---

## 4. Exact Unblock Steps for Controlled Pilot Requalification & 168-Hour Launch

Once an authorized **dedicated staging VPS** (clean Linux host with Docker + Compose v2, Go 1.27, `jq`, `systemd`, >= 4 vCPU, >= 8 GiB RAM, >= 100 GB SSD, zero unrelated production workloads) is provisioned:

1. **Deploy Persistent Isolated Staging Stack:**
   ```bash
   git clone https://github.com/AbdullahZaher/ruxbase.git /srv/ruxbase-soak/repo
   cd /srv/ruxbase-soak/repo
   ./bench/soak/scripts/deploy-staging.sh
   ```
2. **Execute Pilot Requalification -> Freeze -> 168-Hour Unattended Soak:**
   ```bash
   ./bench/soak/scripts/run-pilot-and-launch.sh
   ```
3. **Post-168-Hour Reconciliation & Strictly Read-Only A11 Audit:**
   ```bash
   /srv/ruxbase-soak/soak reconcile --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" --required-hours 168
   /srv/ruxbase-soak/soak audit     --evidence-dir "/srv/ruxbase-soak/evidence/<CAMPAIGN_ID>" --required-hours 168
   ```
4. **Formal Closure:**
   - `W11-05` remains `OPEN` until 168 consecutive wall-clock hours complete on isolated staging and independent A11 issues `W11-05_VERIFIED`. Production remains **`PRODUCTION NO-GO`** until `W11-05` and human owner authorization (`W11-08`) are satisfied.
