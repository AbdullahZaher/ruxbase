# W01-M01 — combined authority fixture and checkout cost

Baseline: 182c3f0e818124908032426c507bc970e37ed282.

PR #19 exact CI #44742085427 passed all ten jobs. The guarded checkout
microbenchmark reported PG17 ordinary p50/p95 83.636/143.658 microseconds,
guarded 1.081665/1.672426 milliseconds; PG18 ordinary p50/p95
141.563/642.565 microseconds, guarded 1.700920/4.740551 milliseconds.

This candidate combines the authority and connection-context queries
without caching or removing the existing checks. It also compares the
actual database name. The existing benchmark reruns in CI; improvement
must be measured, not assumed. This is not a workload benchmark.

An optional reviewed identity contract in prepared manifest JSON
(identities) and in operatorprep.Config binds the declared distinct
runtime, migration, provisioning and optional WAL names. The read-only
separation check verifies actual role existence, session identity,
declared service/migration attributes and lack of membership between
declared non-provisioner principals. It does not certify arbitrary role
administration, all indirect privileges, endpoint identity, or provisioning
of all application grants. The existing guarded runtime admission and
migration authority checks remain in place.

The existing disposable separate-migration fixture is extended to create
a distinct runtime login, bind the migration operation to the same
contract, verify the actual identities including the CI WAL role when
configured, and load metadata with the restricted runtime on that same
database. It verifies rejection of an incorrect or missing identity. No
production database or role is involved. The metadata bootstrap test
does not use the full application grant manifest or start HTTP/WAL
together. Those integrated lifecycle and production-equivalent
compatibility/performance checks and independent review still remain.

The earlier loopback and dedicated WAL tests retain their restricted
experimental scope. No production deployment or M01 closure is claimed.

## Combined fixture continuation

Post-merge CI #44747346563 on PR #19 failed a Linux embedded Postgres
download with HTTP 429. The failed job was rerun without source changes
and attempt 2 succeeded. PR #20 initial CI #44747808258 reported a Go
formatting failure and PG18 fixture cleanup failure (permission denied
to terminate process, followed by role dependency errors). The PG18
checkout measurement was ordinary p50/p95 128.692/588.443 microseconds
and guarded p50/p95 1.098664/2.382706 milliseconds. These are preliminary
measurements on a failed candidate, not acceptance evidence or a controlled
performance improvement claim.

The existing reviewed manifest and real loopback HTTP fixture are now
shared test helpers. The separate migration test requires the complete
fixture grant manifest with declared role identities during bootstrap
and runs the same TCP password-authentication and CRUD lifecycle after
migration. The fixture collection preparation uses the separate migration
identity with offline worker-free configuration. The original core
fixture continues using the same shared test assertions. Cleanup waits
for database sessions to depart before dropping the disposable database;
no additional termination authority is granted. CI must validate these
changes before merging.

The WAL identity remains checked in this combined fixture when configured;
the actual WAL streaming test is still separate. General serving and
replication together with all four principals, production provisioning,
workload compatibility/performance and independent review remain open.
