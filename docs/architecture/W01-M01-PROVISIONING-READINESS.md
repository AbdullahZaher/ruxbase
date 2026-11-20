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

PR #21 serial microbenchmark (CI #44766304961): three trials per PG
version, 200 ordinary and 200 guarded samples per trial. Median trial
p95 was ordinary/guarded 0.132419/0.962523 ms (PG17) and
0.132375/0.933673 ms (PG18). No production workload SLO is asserted.
Compatibility parity accounted for 5112 reference tests: 5097 passed,
15 skipped/renamed/removed and zero missing. The 15 exclusions require
independent review.

This is a repeatable manual operator lifecycle and executable readiness
gate, not a generalized automated provisioning or grant reconciliation
engine. Actual deployment manifests, approved permission changes,
recovery rehearsal, full serving compatibility and independent acceptance
remain outstanding. Legacy serving retains its separate behavior.
Do not expose the experimental loopback server through a proxy or tunnel.
Wave 00 owner gates are unaffected. M01 remains open.
