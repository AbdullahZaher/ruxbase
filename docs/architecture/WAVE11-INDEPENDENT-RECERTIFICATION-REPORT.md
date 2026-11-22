# Ruxbase Enterprise Stress Test Findings Remediation, Concurrency Hardening & Independent Recertification Report

**Document:** `docs/architecture/WAVE11-INDEPENDENT-RECERTIFICATION-REPORT.md`  
**Repository:** `AbdullahZaher/ruxbase`  
**Operational Release Branch:** `release/operational-gates-20261122` (PR #67)  
**Engineering Implementation Candidate Commit:** `0fa9fc683ad9b24af531ce693a517ce39fb7f8cb`  
**Engineering Implementation Git Tree:** `987b470e25533579eece533a980fb12242be8092` (`candidateClean = true`)  
**Documentation Reconciliation Commit:** Recorded separately as the follow-up docs-only commit on `release/operational-gates-20261122` (no self-referential commit SHA claim inside this file)  
**Historical Baseline Campaign:** `20261122-131525` on commit `feaa7451eb783b4e96ba02c3417497868482d2cd` (local workstation archive; gitignored under `bench/stress/results/`)  
**Historical Standard-Mode Regression Campaign:** `20261122-172848` on commit `afd2201fffcfceb2229fc87a449a830007570ae7` (local workstation archive; gitignored under `bench/stress/results/`)  
**Superseded Experimental Campaign:** `20261122-173839` on commit `1073bfa2a4ed2c138d298f1ed3c9474c40d4afd1` (contained experimental `set_claims_direct`, which was permanently reverted; **NOT used to justify Strict RLS throughput claims on `0fa9fc68`**)  
**Engineering Execution Lead:** A01 — Principal Software Architect, Staff PostgreSQL Engineer, Distributed Systems Reliability Lead  
**Independent Certification Auditor:** A11 — Strictly Read-Only Independent Engineering Certification Auditor (conducted separately after candidate freeze)  
**Date:** `2026-11-23`  

---

## 1. Executive Summary & Formal Governance Verdicts

```
========================================================================================
                 RELEASE QUALIFICATION & GOVERNANCE STATUS
========================================================================================
  1. Engineering Merge Readiness:
     >>> READY_FOR_MAINTAINER_REVIEW <<<
     (Pending independent A11 read-only verification on frozen candidate + 18/18 CI green)

  2. Software Engineering & Concurrency Remediation Status:
     >>> ENGINEERING_CERTIFIED (PENDING INDEPENDENT A11 FREEZE AUDIT) <<<
     All P1 functional & concurrency defects (multipart session-bound crash recovery,
     two-stage orphan GC, transactional outbox lease lifecycle, batch expiration guard,
     and real DB failure injection) are remediated, race-tested, and verified on
     PostgreSQL 17 and PostgreSQL 18.

  3. Production Deployment Authorization:
     >>> PRODUCTION_NO_GO (ENGINEERING_COMPLETE_RELEASE_GATES_PENDING) <<<
     Production deployment remains strictly blocked pending completion of:
       - W11-05: Genuine 168-hour continuous staging soak (PENDING_STAGING_EVIDENCE)
       - W11-08: Explicit human owner approval, independent GitHub review, and
                 GPG-signed release tag (PENDING_OWNER_AUTHORIZATION)
========================================================================================
```

### Executive Summary of Remediations on Implementation Candidate `0fa9fc68`
1. **P1 — Multipart Upload Session-Bound Crash Recovery & Two-Stage Orphan GC (`core/storage.go`, `migrations/1762200000_ruxbase_storage_multipart_lifecycle.go`):**
   - Replaced bucket/name/size heuristic recovery with strict durable session binding (`_storage_objects.upload_id`, `_storage_objects.completion_token`, `_storage_uploads.expected_sha256`, `_storage_uploads.completed_object_id`) and a completion lease (`completion_token`, `completion_leased_until`).
   - Enforced cluster-wide target serialization via partial unique index `idx__storage_uploads_completing_target` on `_storage_uploads (bucket_id, name) WHERE status = 'completing'`, preventing concurrent sessions from overwriting each other's final blob.
   - Recovery always reads and verifies actual stored blob bytes (SHA-256 + length) from the filesystem/S3 backend before marking a session `completed`; missing or corrupted blobs fail closed (`ErrStorageIntegrityMismatch`).
   - Unrelated preexisting objects or objects from other sessions are never adopted or silently overwritten (`ErrStorageObjectConflict`).
   - Replaced unsafe timestamp-only orphan deletion with a **two-stage lifecycle**:
     - **Stage A (DB-coordinated expiration, no object-store I/O in tx):** Atomically locks stale `pending` sessions via `FOR UPDATE SKIP LOCKED`, journals chunk keys into `_storage_upload_cleanup`, and transitions sessions to `status = 'expired'` with `parts = '[]'` so expired sessions cannot resume (`ErrStorageUploadExpired`) and never hold dangling references to deleted chunks.
     - **Stage B (Idempotent object-store cleanup outside DB tx):** Drains `_storage_upload_cleanup` and scans legacy untracked keys safely while strictly preserving active `pending` and `completing` sessions. Deletion failures are tracked (`attempts`, `last_error`) and returned as errors rather than counted as purged.
   - Verified across **22 deterministic multipart tests** (including fault-injected crash recovery, live completion protection against both concurrent multipart and single-shot `UploadObject` overwrites, competing sessions, concurrent `UploadPart`/`Complete`/`Abort` vs GC, multi-worker GC idempotency, legacy keys, and a live S3-compatible HTTP backend test via `filesystem.NewS3`) under Go's `-race` detector.

2. **P1 — Transactional Outbox Lease Lifecycle, Batch Expiration Guard & Real DB Failure Injection (`core/jobs.go`, `migrations/1762100000_ruxbase_outbox_leasing.go`):**
   - Preserves atomic single-statement claiming (`UPDATE ... RETURNING` with `FOR UPDATE SKIP LOCKED`), unique per-claim fencing tokens (`claim_token = gen_random_uuid()::text`), and lease expiration (`leased_until < now()`).
   - Upgraded lease precision to milliseconds (`INTERVAL '1 millisecond'`) to support deterministic wall-clock lease tests without SQL timestamp manipulation.
   - Added `ProcessPendingOutboxWithOptions` supporting optional background heartbeat lease renewal (`RenewInterval > 0`) that extends `leased_until` while long-running publications execute and cancels the publisher context immediately if ownership is lost.
   - Implemented **batch lease expiration guard**: before publishing subsequent events in a claimed batch (or when lease headroom drops below half), performs a fenced pre-publish check & refresh (`WHERE id = :id AND claim_token = :token AND status = 'processing' AND leased_until > now()`) and keeps still-valid tail events alive; already-expired batch entries are skipped instead of blindly published.
   - Eliminated silent swallowing of completion/retry `UPDATE` database errors — all DB errors now propagate and are never counted as successful publications.
   - **Delivery Semantics Contract:** Strictly **at-least-once delivery** (NOT exactly-once). If a lease expires while an external dispatch remains in flight, or if a network failure occurs after external dispatch before PostgreSQL acknowledges `status = 'published'`, another worker may reclaim and re-deliver the event. Consumers MUST deduplicate using `OutboxEvent.Id` or an application idempotency key.
   - Verified across **14 deterministic outbox tests** under `-race`, including real wall-clock lease expiration with barrier channels, background heartbeat renewal (>2x lease duration), batch lease expiration guard, multi-instance concurrency, and real PostgreSQL failure injection across all 7 required failure modes (claim failure, completion UPDATE failure, retry UPDATE failure, post-claim connection loss, post-publish connection loss, unknown transaction outcome, and stale worker fencing).

3. **Strict RLS Security Preservation & Realtime Sync Cursor Consistency (`core/rls_strict.go`, `core/rls_sync.go`, `apis/record_crud.go`, `apis/realtime_sync.go`):**
   - Confirmed **0 occurrences** of `set_claims_direct` or unverified claim caches across the entire repository.
   - Retained canonical cryptographically signed JWT generation (`auth.NewRLSToken`) verified per transaction by PL/pgSQL `ruxbase_rls.auth()` via `pgcrypto.hmac` with transaction-local `set_config(..., true)`.
   - Preserved indexed rule predicates (`query.AndWhere(expr)`) in `apis/record_crud.go` so PostgreSQL can use B-Tree indexes while kernel RLS enforces tenant boundaries.
   - Updated `apis/realtime_sync.go` to derive `syncedAt` cursors from PostgreSQL `clock_timestamp()`, eliminating multi-node and host-vs-container clock skew.

---

## 2. Historical Evidence Reconciliation & Artifact Availability

Previous report revisions conflated local-workstation-only historical stress campaigns with later commits and omitted the fact that `bench/stress/.gitignore` excludes `bench/stress/results/*` from GitHub. This table reconciles every artifact with exact provenance:

| Artifact / Campaign | GitHub-Tracked? | Exact Tested Commit / Scope | Honest Status & Provenance |
| :--- | :---: | :--- | :--- |
| **Matched Supabase Benchmark** (`bench/results/20261122-122216-*`, `20261122-124102-*`) | **YES (Tracked in Git)** | PR #67 matched cgroup v2 benchmark vs Supabase Self-Hosted `self-hosted/v0.8.4` | Publicly reviewable directly in GitHub PR #67; SHA-256 verified below. |
| **Historical Baseline Stress Campaign** (`bench/stress/results/20261122-131525/`) | **NO (Local workstation only; gitignored by `bench/stress/.gitignore`)** | Historical baseline commit `feaa7451eb783b4e96ba02c3417497868482d2cd` | Preserved locally on benchmark host; NOT in GitHub. Uncovered initial P1 multipart race (`1,835,008 bytes lost`) and P1 outbox race (`60 duplicate deliveries`). |
| **Historical Standard-Mode Stress Campaign** (`bench/stress/results/20261122-172848/`) | **NO (Local workstation only; gitignored)** | Historical commit `afd2201fffcfceb2229fc87a449a830007570ae7` | Preserved locally on benchmark host; NOT in GitHub. Measured Standard mode peak throughput (`4,849 RPS` at 500 VU, `0` errors at 1,000 VU) and Strict index scan fix. Historical measurement — NOT newly re-run on `0fa9fc68`. |
| **Superseded Strict-RLS Experimental Campaign** (`bench/stress/results/20261122-173839/`) | **NO (Local workstation only; gitignored)** | Historical commit `1073bfa2a4ed2c138d298f1ed3c9474c40d4afd1` | **SUPERSEDED / NON-CANONICAL:** Included experimental `set_claims_direct`, which was permanently reverted. Its Strict RLS throughput numbers MUST NOT be cited as current performance of `0fa9fc68`. |
| **Fresh `corestress` Subsystem Run on `0fa9fc68`** | **Reproducible command** (executed locally on PG 18.7) | **Exact candidate `0fa9fc683ad9b24af531ce693a517ce39fb7f8cb`** | Freshly executed on `0fa9fc68`: `all 12 system migrations applied`, `outboxDuplicateDeliveries: 0`, `multipartConcurrentLostBytes: 0`, `103/103` SHA-256 verified, `crossTenantDenied: 100`, `crossTenantLeaked: 0`, `passed: true`. |

> [!NOTE]
> Full 10k / 100k / 1M-row Strict RLS throughput re-benchmarking under canonical cryptographic JWT verification (post-revert of `set_claims_direct`) has not been re-run as a fresh multi-hour stress campaign on `0fa9fc68`. Therefore, this report **does not claim** that the historical Strict RLS throughput overhead of per-transaction role switching and PL/pgSQL HMAC verification has been eliminated; it documents Strict RLS as a defense-in-depth mode with canonical cryptographic verification and indexed list predicates (`query.AndWhere(expr)`).

---

## 3. Remediation Findings & Root-Cause Matrix

| Finding ID | Severity | Subsystem | Root Cause | Implemented Remediation (`0fa9fc68`) | Verification Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **REM-01A** | **P1 (High)** | `core/storage.go` (Multipart Concurrency & Session-Bound Recovery) | Unprotected JSONB RMW, shared mutable chunk paths, and heuristic crash recovery matching only `(bucket_id, name, size)` risked adopting unrelated preexisting objects or overwriting concurrent completions. | 1. Immutable per-attempt chunk keys (`part_<num>_<token>`) + `FOR UPDATE` row locking.<br>2. Additive migration `1762200000_ruxbase_storage_multipart_lifecycle.go` adds `completion_token`, `completion_leased_until`, `expected_sha256`, `completed_object_id`, partial unique index `(bucket_id, name) WHERE status = 'completing'`, and `_storage_objects.upload_id` / `completion_token`.<br>3. Recovery strictly requires `existing.UploadId == session.Id`, validates metadata and **actual stored blob SHA-256 bytes**, and fails closed on missing/corrupt blobs or unrelated objects (`ErrStorageObjectConflict`). | 22 deterministic tests in `core/storage_test.go` PASS under `-race` (local + S3-compatible HTTP backend); fresh `corestress` on `0fa9fc68`: `multipartConcurrentLostBytes: 0` (2,097,152 / 2,097,152 bytes). |
| **REM-01B** | **P1 (High)** | `core/storage.go` (Two-Stage Multipart Orphan GC) | Timestamp-only GC deleted temporary chunks of stale `pending` sessions while leaving sessions `pending` in PostgreSQL with dangling chunk metadata, racing active uploads and silently ignoring deletion errors. | Implemented two-stage `CleanupOrphanedUploads` backed by durable journal `_storage_upload_cleanup`:<br>- **Stage A (short TX):** Locks stale `pending` sessions (`FOR UPDATE SKIP LOCKED`), journals keys, transitions `status = 'expired'` with `parts = '[]'` (zero dangling refs).<br>- **Stage B (outside TX):** Drains journal + safe legacy scan, strictly preserving active `pending`/`completing` sessions and propagating deletion errors without counting failed deletes as purged. | Deterministic negative tests in `core/storage_test.go` PASS under `-race`: stale expiry + resume rejection, `UploadPart`/GC race, `Complete`/GC race, `Abort`/GC race, delete I/O failure recording + retry, Stage A/B crash recovery, 4 concurrent GC workers, and legacy keys. |
| **REM-02** | **P1 (High)** | `core/jobs.go` (Transactional Outbox Lease Lifecycle & Failure Injection) | Autocommit claiming, creation-time stale reclamation, swallowed DB ack errors, and lack of batch lease expiration guards / real DB failure tests. | 1. Migration `1762100000_ruxbase_outbox_leasing.go` (`claim_token`, `leased_at`, `leased_until`, composite index).<br>2. Atomic `UPDATE ... RETURNING` + `FOR UPDATE SKIP LOCKED` with millisecond lease precision.<br>3. Pre-publish batch lease refresh & expired-entry skip guard.<br>4. Optional `RenewInterval` background heartbeat canceling publisher ctx on ownership loss.<br>5. Strict DB error propagation (never counted as published).<br>6. Certified **at-least-once delivery** semantics. | 14 deterministic tests in `core/jobs_test.go` PASS under `-race` (wall-clock lease expiry barriers, >2x lease heartbeat renewal, batch expiration guard, multi-instance, and real DB failure injection across all 7 failure modes); fresh `corestress` on `0fa9fc68`: `outboxDuplicateDeliveries: 0`. |
| **REM-03** | **P2 (Medium)** | `apis/record_crud.go` (Strict RLS Index Predicates) | Skipping `query.AndWhere(expr)` in strict mode prevented PostgreSQL from using B-Tree indexes on multi-tenant list queries. | Restored `query.AndWhere(expr)` across all list operations while PostgreSQL kernel RLS independently enforces tenant isolation under `ruxbase_api`. | `RUXBASE_RLS_STRICT=true go test ./apis/... ./tests/...` PASS on PG 17 (CI) and PG 18 (local + CI). |
| **REM-04** | **P2 (Medium)** | `core/rls_strict.go`, `core/rls_sync.go` (Canonical JWT Enforcement) | Previous experimental `set_claims_direct` shortcut risked bypassing cryptographic JWT validation. | Permanently removed `set_claims_direct` and unlogged `_auth_cache`; preserved canonical `auth.NewRLSToken` + PL/pgSQL `pgcrypto.hmac` verification with transaction-local `set_config(..., true)`. | `git grep set_claims_direct` = 0 matches; full Strict RLS & Golden API test suites PASS on PG 17 and PG 18. |
| **REM-05** | **P3 (Low)** | `tools/search/provider.go` (PocketBase Parity) | Preserving upstream `COUNT(DISTINCT [[id]])` semantics for SDK parity while guiding keyset pagination for deep >100k row traversal. | Retained exact PocketBase SQL count syntax and contract hash in `docs/contracts/reference.json`. | 19/19 PocketBase JS SDK tests PASS; `reference.json` 49/49 blob hashes matched. |
| **REM-06** | **P2 (Medium)** | `apis/realtime_sync.go` (Sync Cursor Monotonicity) | `time.Now()` host timestamps diverged from PostgreSQL `_realtime_events.created` (`now()`) under host-vs-container or multi-node clock drift. | Sample `syncedAt` directly from PostgreSQL `SELECT clock_timestamp()`. | `TestRealtime_OfflineDeltaSync` and full `./tests/e2e/...` PASS on PG 17 (CI) and PG 18 (local + CI). |

---

## 4. Verification Execution Matrix (CI vs Local-Only)

### A. Tests Executed Locally on Implementation Candidate `0fa9fc68`
Environment: macOS arm64, Go `1.27.0`, PostgreSQL `18.7` (`pgvector/pgvector:0.8.7-pg18-trixie`, `max_connections=300`, non-superuser `ruxbase_app`).

| Verification Command | Scope | Result | Notes |
| :--- | :--- | :---: | :--- |
| `go build ./...` | Full Go workspace compilation | **PASS** | 0 errors |
| `go vet -structtag=false ./...` | CI vet configuration | **PASS** | 0 warnings/errors |
| `./scripts/check-sql-patterns.sh` | PostgreSQL SQL portability guard | **PASS** | No SQLite-only SQL found |
| `go test -v -race -count=1 ./core -run 'TestJobsManager_Outbox\|TestStorageManager_Multipart'` | 36 Outbox (14) & Multipart (22) negative, crash, GC, lease, fault-injection, closed-pool, and S3 backend tests | **PASS** | 36/36 PASS, 0 data races |
| `go test -count=1 -race ./tools/filesystem/...` | Local & S3 blob driver race tests | **PASS** | All packages PASS |
| `go test -count=1 ./core/...` | Full `core`, `core/cluster`, `core/validators` suites | **PASS** | All packages PASS |
| `go test -count=1 ./tests/e2e/...` | Full E2E integration suite (PG 18) | **PASS** | All tests PASS |
| `RUXBASE_RLS_STRICT=true go test -count=1 -p 2 ./apis/... ./tests/...` | Full Strict RLS API + E2E suite (PG 18) | **PASS** | All packages PASS |
| `./scripts/sdk-test.sh` | Official PocketBase JS SDK compatibility | **PASS** | 19/19 tests PASS |
| `go run ./bench/stress/cmd/corestress --db ...` | Live subsystem stress & integrity verification (PG 18.7) | **PASS** | 12/12 migrations, 0 outbox dupes, 0 lost multipart bytes, 103/103 SHA-256 objects, 100/100 cross-tenant denied |
| `python3 scripts/check_release_evidence.py` & `unittest` | Checklist/evidence accounting | **PASS** | 125/127 complete; 2 unchecked operational gates (`W11-05`, `W11-08`); 7/7 unit tests PASS |

### B. Tests Executed in GitHub Actions CI (PostgreSQL 17 & PostgreSQL 18 Matrix)
Executed remotely on the exact pushed PR #67 head commit (all 18 required checks):
- `ci/build`, `ci/lint` (`golangci-lint` v2), `ci/ui`, `ci/sdk`, `ci/release-snapshot`
- `ci/dev` (`ubuntu-latest`, `macos-latest`, `windows-latest`)
- `ci/test (17)` — Full suite (`./scripts/ci-test.sh`, PocketBase parity 5097/5112, 3 checkout trials) + Strict RLS (`RUXBASE_RLS_STRICT=true ./apis/... ./tests/...`) on **PostgreSQL 17**
- `ci/test (18)` — Full suite + Strict RLS on **PostgreSQL 18**
- `benchmark baseline/ruxbase`, `docs-build-candidate`, `docs-foundation`, `docs-quickstart/compose`, `docs-quickstart/quickstart`, `docs-starters/starter-integration`, `docs-performance/lighthouse`, `docs-brand-review`

---

## 5. Authorization & RLS Security Posture

- **Mode 1: Standard Application-Compiled RLS (`RUXBASE_RLS_STRICT=false`):** Compiles PocketBase rules into parameterized PostgreSQL SQL (`query.AndWhere(expr)`) under least-privilege `ruxbase_app`. Recommended default for high-throughput multi-tenant SaaS.
- **Mode 2: Strict PostgreSQL Kernel RLS (`RUXBASE_RLS_STRICT=true`):** Defense-in-depth mode executing per-request transactions with `SET LOCAL ROLE ruxbase_api`, transaction-local `set_config('ruxbase.token', ..., true)`, and canonical cryptographic HS256 JWT verification inside PostgreSQL (`ruxbase_rls.auth()`). Superuser bypass is explicitly blocked by `RunAsRLSRole`. Preserves indexed list predicates without compromising kernel policy enforcement.

---

## 6. Exact Remaining Operational Gates & `PRODUCTION_NO_GO` Status

Production deployment remains strictly **`PRODUCTION_NO_GO (ENGINEERING_COMPLETE_RELEASE_GATES_PENDING)`** until both external operational gates are completed by authorized humans:

1. **Gate `W11-05` — 168-Hour Continuous Staging Soak (`PENDING_STAGING_EVIDENCE`):**
   - Governing file: `docs/architecture/W11-05-STAGING-SOAK-EVIDENCE.md`.
   - Requires 168 uninterrupted wall-clock hours on a dedicated external staging deployment. Cannot be simulated or substituted by short stress runs.
2. **Gate `W11-08` — Formal Maintainer Release Sign-Off (`PENDING_OWNER_AUTHORIZATION`):**
   - Governing file: `docs/architecture/W11-08-PRODUCTION-GO-NOGO.md`.
   - Requires independent human PR review (`vibress`), explicit written release authorization from repository owner `AbdullahZaher`, and a GPG-signed immutable release tag.

---

## 7. Verifiable Artifact & Cryptographic Provenance Manifest

```
========================================================================================
                    VERIFIABLE CANDIDATE & ARTIFACT MANIFEST
========================================================================================
Engineering Implementation Commit: 0fa9fc683ad9b24af531ce693a517ce39fb7f8cb
Engineering Implementation Tree:   987b470e25533579eece533a980fb12242be8092
Operational Release Branch:        release/operational-gates-20261122
Pull Request:                      https://github.com/AbdullahZaher/ruxbase/pull/67

1. Git-Tracked Benchmark Artifacts (Publicly Reviewable in Repository — bench/results/):
da807f4b712049c78fa237914b6ef156710695fd7dc4707204c28bbc780368d5  bench/results/20261122-122216-matched-supabase.environment.txt
f398a23edd6c961da76bfc28d34053fcd0d3e7105d3e2b436a400027a7d92627  bench/results/20261122-122216-matched-supabase.jsonl
884afb5044c06cd6b5c335dbef178456d8618d5455e70037fd67e4ba4976bcca  bench/results/20261122-122216-matched-supabase.resources.jsonl
a787d3adc6cf559862b4e2f54672c7e99f6ec91b315e7e346a8605db070d21fc  bench/results/20261122-124102-matched-supabase-tuned.environment.txt
55982ffd58c227afcc811c603dcba9744ff7d02f9e7de5d3eb7102eae9b2b741  bench/results/20261122-124102-matched-supabase-tuned.jsonl
6c05d2a0a23d815e47d872aa01bb40e071b4ac04a7ed3dcdd1759761f097224f  bench/results/20261122-124102-matched-supabase-tuned.resources.jsonl

2. Local-Workstation Historical Stress Archives (Gitignored by bench/stress/.gitignore;
   NOT committed to GitHub — verified on local benchmark workstation):
   - Campaign 20261122-131525 (baseline commit feaa7451eb783b4e96ba02c3417497868482d2cd)
   - Campaign 20261122-172848 (historical commit afd2201fffcfceb2229fc87a449a830007570ae7):
     17c502b400bc84156bc6d95d12e4dbe57880d1b750a6e76b39dcced6d1a7dc34  environment.txt
     bf6fa70052c7aecaf16cc2dc507a54f52fb0e7269d48e127e4a5f4ef4eecefef  metrics.jsonl
     64d392d4bfdfde2fa9131becd24e0188caf4e5d802620a4d8e5b6618c95bf2ad  metrics.resources.jsonl
   - Campaign 20261122-173839 (SUPERSEDED commit 1073bfa2a4ed2c138d298f1ed3c9474c40d4afd1;
     reverted set_claims_direct — archived for historical audit trail only)
========================================================================================
```
