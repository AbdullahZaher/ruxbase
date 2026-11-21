# W01-M01 — provisioning and prepared readiness runbook

Baseline: 97873857823b155a846f545dec92317529cc642b.
Post-merge CI #44767986743: 10/10 successful. This runbook describes the
restricted experimental profile, not a production deployment authorization.

## Operator responsibilities and gates

| Principal | Responsibility | Required evidence |
|---|---|---|
| Provisioning | Authorized offline role and initial database preparation | Reviewed named identities, scoped permissions and backup/restore plan |
| Migration | Offline system and approved collection preparation | Separate identity, successful system history check and no serving workers |
| Runtime | Restricted prepared HTTP | Actual role admission, independently reviewed main and auxiliary manifests, accepted collections |
| Replication | Separate WAL session when explicitly enabled | Reviewed publication and prepared slot; isolated fixture event evidence |

Before any persistent environment operation, require named owner approval,
a verified backup and maintenance window. Keep all serving processes stopped
during schema or privilege reconciliation. Keep provisioning and migration
credentials out of the serving process and keep the WAL login separate.
No fixture passwords or fixture-only PUBLIC routine policy may be reused.

1. Have an authorized operator review the actual target database inventory,
   object ownership, default ACLs, routine execution privileges and the
   necessary application permissions. Establish the four distinct names
   (WAL is optional for hooks only). The operator supplies a reviewed
   environment-specific manifest; do not derive approved grants from actual
   observed runtime privileges or automatically grant missing rights.
2. Prepare the database with the existing explicit offline
   ruxbase-prepare separate migration profile. Inspect the system ledger
   using its existing check-system mode. Review any error before retry.
3. Apply only independently reviewed environment-specific grant changes
   using the authorized object owner. Record all changes and their approved
   reversal procedure. Recheck the main and auxiliary runtime grant manifests,
   identities, settings, collection metadata and allowed collection list.
4. Run ruxbase-prepared with the same explicit environment and collection
   flags as serving, replacing the port selection with --check-only.
   No TCP listener is opened. The mode reuses the prepared bootstrap and
   collection admission code, closes all owned pools and returns nonzero
   on rejection. It does not build or serve HTTP routes and does not start
   realtime or migration workers. It is point-in-time readiness only.
5. For an authorized disposable rehearsal, start the same restricted
   loopback profile using the reviewed manifest and explicit loopback port.
   Record successful authentication, CRUD and orderly shutdown. The existing
   combined CI fixture also proves an HTTP create reaches a persisted WAL
   event via the dedicated session; it does not enable realtime on the
   prepared HTTP server.

## Drift and recovery

On any admission mismatch or failed check, keep serving stopped. Preserve
sanitized evidence; identify missing or unexpected effective permissions
and schema changes against the reviewed expectation. Do not auto-approve
observed authority, broaden grants to pass tests, or automatically repair
production slots. The authorized operator must approve and apply the
smallest necessary correction, rerun the ledger and readiness checks, and
rehearse the relevant journey. If correctness cannot be established, use
the approved backup/recovery procedure. Never use this runbook as an
unreviewed destructive rollback script.

## Evidence and limits

The prepared CLI now supports an explicit listener-free readiness mode,
with configuration tests and a disposable integration check that verifies
success with the reviewed fixture and rejects an absent collection.
The same function performs admission for serving, preventing validation
drift between the two entry points. CI requires the readiness evidence
marker when the combined WAL fixture is configured. New checks await
exact candidate CI.

### Guarded connection pool microbenchmarks

Serial connection checkout measurements (three trials per PG version, 200 ordinary and 200 guarded samples per trial):

| PostgreSQL Version | Ordinary Pool Checkout (p95) | Guarded Pool Checkout (p95) | Overhead Factor |
|---|---|---|---|
| PostgreSQL 17 | 0.205 ms | 1.525 ms | ~7.4x |
| PostgreSQL 18 | 0.093 ms | 0.769 ms | ~8.2x |

The guarded pool ensures that every connection checked out from the pool runs within the verified non-superuser role and search_path contract before executing application queries. The observed overhead (~0.7ms - 1.3ms) reflects the necessary round-trip verification overhead. Proposed performance budget for guarded pool checkout latency: p95 < 2.0ms across supported PostgreSQL versions.

### 15-entry compatibility exception register

Parity accounting validates 5112 total reference tests against PocketBase v0.40.6: 5097 passed, 15 excluded (2 skipped, 10 renamed, 3 removed), and 0 missing.

| Index | Category | Reference Identifier | Ruxbase Identifier / Action | Documented Technical Justification |
|---|---|---|---|---|
| 1 | Skipped | `./core: TestBaseAppConcurrentDBRouting` | SKIPPED (`core/base_test.go:510`) | Unified PostgreSQL connection pool replaces dual SQLite concurrent/nonconcurrent builder routing. |
| 2 | Skipped | `./core: TestBaseAppAuxConcurrentDBRouting` | SKIPPED (`core/base_test.go:588`) | Unified PostgreSQL auxiliary pool replaces dual SQLite auxiliary builder routing. |
| 3 | Renamed | `./core: TestCollectionSaveViewWrapping/no_wrapping_-_datetime_field` | `wrapping_-_datetime_field` | Date column is TIMESTAMPTZ in PostgreSQL; view id is wrapped as text. |
| 4 | Renamed | `./core: TestCollectionSaveViewWrapping/no_wrapping_-_select_field` | `wrapping_-_select_field` | Multiple select column is JSONB in PostgreSQL; view id is wrapped as text. |
| 5 | Renamed | `./core: TestCollectionSaveViewWrapping/wrapping_-_expresion` | `no_wrapping_-_expresion` | Text literal is a native TEXT column in PostgreSQL. |
| 6 | Renamed | `./core: TestFindFirstExternalAuthByExpr/0_\`collectionRef\`={:p0}` | `0_"collectionRef"={:p0}` | PostgreSQL double-quoted identifier syntax in generated SQL. |
| 7 | Renamed | `./core: TestFindFirstExternalAuthByExpr/1_\`collectionRef\`={:p0}` | `1_"collectionRef"={:p0}` | PostgreSQL double-quoted identifier syntax in generated SQL. |
| 8 | Renamed | `./core: TestFindFirstExternalAuthByExpr/2_\`collectionRef\`={:p0}_AND_\`provider\`={:p1}` | `2_"collectionRef"={:p0}_AND_"provider"={:p1}` | PostgreSQL double-quoted identifier syntax in generated SQL. |
| 9 | Renamed | `./core/validators: TestNormalizeUniqueIndexError/unique_index_error_but_mismatched_fields` | `unique_violation_but_mismatched_fields` | PostgreSQL unique_violation (SQLSTATE 23505) error structure normalization. |
| 10 | Renamed | `./core/validators: TestNormalizeUniqueIndexError/unique_index_error_but_mismatched_table_name` | `unique_violation_but_mismatched_table_name` | PostgreSQL unique_violation (SQLSTATE 23505) error structure normalization. |
| 11 | Renamed | `./core/validators: TestNormalizeUniqueIndexError/unique_index_error_with_matching_table_name_and_field_starting_with_the_name_of_another_non-unique_field` | `unique_violation_with_a_field_name_prefix_of_another_field` | PostgreSQL unique_violation error normalization with field prefix. |
| 12 | Renamed | `./core/validators: TestNormalizeUniqueIndexError/unique_index_error_with_matching_table_name_and_fields` | `unique_violation_with_matching_table_name_and_fields` | PostgreSQL unique_violation error normalization with exact table and fields. |
| 13 | Removed | `./core: TestBaseLockRetry/3_&errors.errorString{s:"table_is_locked"}` | REMOVED (`parity-renamed.txt:6`) | SQLite string lock errors replaced by retryable PostgreSQL serialization/lock failure SQLSTATEs (40001, 40P01, 55P03). |
| 14 | Removed | `./core: TestCreateViewFieldsWithNumberOnlyInt/total` | REMOVED (`parity-renamed.txt:10`) | SQLite total() aggregate function does not exist in PostgreSQL (SUM/COUNT with explicit casts used). |
| 15 | Removed | `./core/validators: TestNormalizeUniqueIndexError/unique_index_error_with_table_name_suffix_matching_the_specified_one` | REMOVED (`parity-renamed.txt:18`) | PostgreSQL reports exact table name; SQLite table name suffix heuristics are obsolete. |

This is a repeatable manual operator lifecycle and executable readiness
gate, not a generalized automated provisioning or grant reconciliation
engine. Actual deployment manifests, approved permission changes,
recovery rehearsal, full serving compatibility and independent acceptance
remain outstanding. Legacy serving retains its separate behavior.
Do not expose the experimental loopback server through a proxy or tunnel.
Wave 00 owner gates are unaffected. M01 remains open.
