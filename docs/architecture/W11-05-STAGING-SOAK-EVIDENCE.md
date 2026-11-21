# W11-05: 168-Hour Continuous Staging Soak Evidence & Status Ledger

**Document:** `docs/architecture/W11-05-STAGING-SOAK-EVIDENCE.md`  
**Requirement ID:** `W11-05 / P1 / GATE`  
**Status:** **`PENDING_STAGING_EVIDENCE`**  
**Candidate Commit Baseline:** `6b443e4b2aaa9ee50276416af5830ba9daf11db6` (`release/final-qualification-20261122`)  
**Date:** `2026-11-22`  

---

## 1. Formal Gate Disposition: `PENDING_STAGING_EVIDENCE`

Per the Ruxbase Master Roadmap and Release Qualification Mandate:
- **Never simulate, extrapolate, or compress a 168-hour (7-day) continuous soak test.**
- Because 168 consecutive wall-clock hours have not elapsed on a dedicated external staging environment for this release candidate, `W11-05` is formally recorded as **`PENDING_STAGING_EVIDENCE`**, which automatically blocks a `PRODUCTION_GO_AUTHORIZED` verdict until completed.

---

## 2. Verified Soak Automation, Fault-Injection & Telemetry Readiness

While the 168-hour wall-clock timer requires a dedicated staging deployment window, all engineering instrumentation, fault-injection harnesses, and automated verification checks required to run and evaluate the soak are implemented and verified:

| Capability | Implementation Artifact | Verification Evidence |
|---|---|---|
| **Prometheus & JSON Runtime Telemetry** | `core/ops.go` (`CollectOpsMetrics`, `FormatPrometheusMetrics`), `apis/ops.go` (`GET /api/ops/metrics`) | Exports `ruxbase_runtime_goroutines`, `ruxbase_runtime_heap_alloc_bytes`, `ruxbase_db_pool_open_connections`, `ruxbase_db_pool_in_use`, `ruxbase_db_pool_wait_count`, `ruxbase_jobs_pending`, `ruxbase_jobs_dead`, and `ruxbase_outbox_unpublished`. Verified in `TestOps_ObservabilityMetricsCapacityAndAlertsSecurity`. |
| **Automated Anomaly & Alert Evaluation** | `core/ops.go` (`EvaluateOpsAlerts`), `apis/ops.go` (`GET /api/ops/alerts`) | Triggers deterministic warnings/critical alerts on `db_pool_saturation`, `db_pool_wait_contention`, `dlq_non_empty`, `outbox_backlog`, and `goroutine_spike`. Verified in `TestOps_ObservabilityMetricsCapacityAndAlertsSecurity`. |
| **Automated Release Blocker & Storage Integrity Gate** | `core/ops.go` (`CheckReleaseBlockers`), `apis/studio_control_plane.go` (`GET /api/studio/overview` -> `releaseBlockers`) | Verifies non-superuser runtime role (`runtime_superuser_violation`), complete `_migrations` registration (`pending_migration_*`), and SHA-256 byte integrity of every storage object (`storage_corruption_*`). Verified in `TestOps_ReleaseBlockersAndStorageCorruptionDetection` and `TestRelease_CoreSelfHostedIntegratedJourney`. |
| **Worker Lease Crash & Reclamation Fault Injection** | `core/jobs.go` (`ClaimNext`, `ReclaimStaleLeases`), `tests/e2e/durable_jobs_test.go`, `tests/e2e/durable_resilience_test.go` | Verified in `TestDurableJobs_CrashRecoveryAndLeaseReclamation`, `TestDurableResilience_PoisonMessageIsolation`, and `TestDurableResilience_ConcurrentWorkerLeaseContention`. |
| **PostgreSQL Connection Drop & Leader Failover Fault Injection** | `core/cluster/cluster.go`, `core/cluster/locks.go`, `core/cluster/replication.go`, `tests/e2e/realtime_failover_test.go`, `tests/e2e/replication_test.go` | Verified in `TestRealtime_ClusterListenReconnectUnderDisconnect` (`pg_terminate_backend` on active `LISTEN` connection -> automatic reconnect and message delivery), `TestRealtime_AdvisoryLockLeaderHandover`, and `TestReplicationLeaderFailover`. |

---

## 3. Staging Execution Runbook to Close `W11-05`

To transition `W11-05` from `PENDING_STAGING_EVIDENCE` to `VERIFIED` on a dedicated staging host:

1. **Provision Isolated Staging Stack:**
   ```bash
   ./scripts/install-selfhosted.sh --dir /srv/ruxbase-soak --domain soak.internal.example
   docker compose -f /srv/ruxbase-soak/docker-compose.prod.yml up -d
   ```
2. **Start Continuous Workload & Prometheus Scraper (168 Hours):**
   - Run steady mixed traffic (authenticated CRUD, RLS list queries, SSE subscriptions, durable job enqueue/worker leases, and isolated function calls) and scrape `GET /api/ops/metrics?format=prometheus` + `GET /api/ops/alerts` every 60 seconds into time-series storage.
3. **Execute Scheduled Fault Injections:**
   - **Hour 24:** `SIGKILL` active worker/app container and verify lease reclamation (`ReclaimStaleLeases`) and zero lost jobs.
   - **Hour 72:** Terminate PostgreSQL `LISTEN` and replication connections (`pg_terminate_backend`) and verify automatic reconnect (`GET /api/health?ready`).
   - **Hour 120:** Execute online backup and query `GET /api/studio/overview` (`releaseBlockers` / `core.CheckReleaseBlockers`) + `GET /api/ops/alerts` to confirm zero `storage_corruption_*` or `dlq_non_empty` blockers.
4. **Record Final 168-Hour Artifacts:**
   - Commit the 168-hour Prometheus metrics export, alert log, and `GET /api/studio/overview` (`"ready": true`, `"releaseBlockers": []`) output to this file and request independent verification.
