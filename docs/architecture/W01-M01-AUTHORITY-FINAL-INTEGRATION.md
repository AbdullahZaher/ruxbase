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
