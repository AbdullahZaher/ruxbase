# W01-M01 — combined prepared HTTP and WAL fixture

Baseline d65ec4bfa99e3a47afe9c3b44f5604b07c4699cd, merged PR #20.
Exact CI #44748811403 passed all ten jobs. Guarded checkout p95 was
4.195955 ms (PG17), 6.202534 ms (PG18); ordinary p95 was 1.037445 ms
and 1.156903 ms respectively. Earlier lower measurements do not prove
a stable performance improvement. Controlled workload evidence remains
necessary.

An explicit PreparedSlotOnly mode admits an existing canonical logical
pgoutput failover slot in the same database and checks the recorded
slot marker. It requires a dedicated WAL configuration and a publication
verification callback, does not create or repair a slot and does not
update slot metadata. The callback remains a trusted Go boundary; legacy
behavior remains unchanged. Missing or invalid slots are refused rather
than repaired by this mode.

The integrated disposable migration/HTTP fixture now grants only event
insertion to the restricted runtime in addition to existing reviewed
fixture grants. The migration identity prepares a publication for the
fixture base collection, the CI-only WAL login prepares its slot, and the
existing cluster decoder uses the separate WAL login with a guarded
runtime event SQL pool and reviewed grant manifest. The test checks the
actual active slot login and requires an authenticated HTTP create to
produce a persisted replication event with the same record identifier.
Shutdown precedes slot removal and role/database cleanup. No new
administrative authority is granted to the runtime, and no persistent
deployment is accessed.

The new fixture runs only when a declared CI WAL login is configured;
the existing HTTP-only and legacy replication tests remain intact.
The listener remains loopback-only. This integration does not start
production realtime subscriptions on the prepared HTTP server, supply
a fully independent deployable worker, or prove durable recovery,
tenant isolation, full SDK/HTTP compatibility or a production
provisioning contract. The replication role inherently has high data
access; it remains separate from runtime. All new evidence is pending
exact candidate CI and independent review; M01 remains open.

## CI and operator contract closure increment

Post-merge CI #44751003194 on PR #20 succeeded. Initial PR #21 CI
#44751615582 passed both PostgreSQL jobs and seven other non-lint
checks (9/10 jobs successful); lint rejected formatting in
the WAL fixture callback. Named callbacks fix the formatting without
altering the verification behavior. The CI script now expressly
requires the combined journey evidence marker when the dedicated WAL
fixture environment is present, so a skipped or silently omitted
combined test cannot satisfy the test jobs.

The offline preparation command in explicit --preprovisioned-roles
mode now requires distinct declared runtime and provisioning role
names in addition to the existing migration role; the WAL role is
optional for a hooks-only deployment. These declarations are passed
to the existing reviewed identity contract and actual catalog checks.
RUXBASE_PREPARE_RUNTIME_ROLE, RUXBASE_PREPARE_PROVISIONING_ROLE and
RUXBASE_PREPARE_REPLICATION_ROLE specify names only, not credentials.
The legacy operator mode refuses to silently ignore those declarations.
This does not create roles or apply a generalized provisioning policy;
actual provisioning remains an authorized offline operator activity.

Initial PR #21 checkout p95 measured 2.127102/7.783905 ms
(ordinary/guarded PG17), and 0.147122/1.110594 ms (PG18).
These variable CI measurements are not a controlled workload benchmark.
Exact final CI and independent review remain pending.

## Verified combined journey and remaining CI remediation

CI #44753207035 on bfe9b23c2fb4de96dade0832412b47a176ff7c8a
emitted the mandatory M01_COMBINED_WAL evidence on both PG17 and PG18.
Both normal full suites and their PocketBase parity checks succeeded;
PG17 subsequently failed the strict-mode TestRecordCrudDelete because
asynchronous storage removal had not completed when inspected. The
overall result was 9/10, not a mergeable candidate. The test now
waits for actual empty/missing storage within five seconds and fails on
unexpected filesystem errors. It retains all delete and authorization
assertions; fixing the race in observation is not proof that storage
cleanup is universally reliable.

The same checkout test is now repeated three times serially after the
parallel full suite, preserving ordinary/guarded paired samples and
authority checks. This reduces parallel test interference but does not
eliminate environmental variability or establish workload performance.

The standalone prepared executable requires the declared role identity
contract in its manifest before opening its listener, using the existing
parser; actual identity and grant checks still happen during bootstrap.
Tests reject omitted, shared and malformed identity declarations. This
aligns the explicit executable with the separate migration CLI without
changing the older metadata-only library fixtures. The remaining
production provisioning lifecycle and independent certification are
not yet closed. No production role or database changes.
