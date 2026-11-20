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
