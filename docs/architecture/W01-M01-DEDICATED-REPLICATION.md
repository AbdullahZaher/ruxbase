# W01-M01 — dedicated WAL identity

Baseline: ef6cd7bd6db60663055aab3c8838f88f96456859
(tree 29e800827ef6807491fd8dc8fc2597ab6ddfe48a).

The opt-in Go configuration BaseAppConfig.ReplicationIdentity declares a
separate WAL connection URL and role. Empty configuration preserves legacy
behavior. Partial configuration never substitutes the application login for
WAL consumption. The dedicated connection requires a different declared
login at the same configured database endpoint. It checks actual SQL
session identity, logical WAL support, REPLICATION and absence of the
listed administrative attributes, then checks session identity again on
the WAL connection before starting decoding. The regular SQL/event pool
and leader lock continue to use the application connection.

With the dedicated identity configured, publication preparation by the
replication leader is replaced by read-only verification of registered
base/auth collections, publication membership, full replica identity and
unfiltered publication tables. Publication creation and modifications
remain an explicit operator responsibility. Existing collection DDL paths
and the legacy bootstrap have not been separated here.

The configuration tests reject shared logins, incomplete settings, different
database endpoints and preselected replication protocol mode. Positive
streaming with an actual separately provisioned REPLICATION login requires
a disposable cluster whose provisioner is authorized to create that login.
The application CI account is not such an authority; do not elevate
it just to satisfy this test. The existing disposable container administrator
can separately provision a test-only WAL login. No production grants, credentials or
persistent databases are changed.

This is partial M01 closure, not a certified serving or replication
security boundary. Endpoint equality does not prove cluster identity
through DNS or proxies. Role membership administration, ownership,
publication column inventory, ongoing authority changes and isolated worker
processes remain to be reviewed. The metadata-only prepared profile still
does not start HTTP or replication. Full restricted serving, actual WAL
integration, performance and independent review remain open.

## Experimental prepared loopback HTTP lifecycle

The separate Go entrypoint apis.ServePreparedLoopback owns a fresh prepared
BaseApp and a caller-supplied TCP loopback listener. It requires explicit
main/auxiliary connection URLs, runtime role, a reviewed manifest, data
directory, hooks-only configuration and non-system auth/base collection
names. It verifies metadata and the manifest before accepting requests,
then binds the existing API middleware and handlers behind a restrictive
method/path allowlist. Password login and base record CRUD are the only
admitted route families. Other auth collections, system collections,
administrative endpoints, query parameters, non-JSON writes, file fields,
MFA and auth-alert-enabled auth collections are outside this experiment.
The code never calls the normal Serve or migration paths. Context
cancellation drains the listener and closes the app pools; a bounded
shutdown error is reported rather than ignored.

The previously in-process prepared HTTP fixture now makes actual TCP HTTP
requests to this entrypoint. It exercises authentication and CRUD, guest
denial, persistence, administrative route exclusion and server shutdown.
Additional unit tests cover route admission and absent manifest rejection.
These tests are awaiting CI, not a production certification. A loopback
listener is not a sandbox; never expose it via a proxy or tunnel. The
profile is not a full compatible PocketBase server, excludes replication
and clustered operation, and uses fixture-specific row policies rather
than a certified generalized RLS authorization engine. It does not
satisfy the remaining complete provisioning, dedicated WAL, performance,
compatibility and independent certification gates.

## Separate executable and verification evidence

The experimental command cmd/ruxbase-prepared starts only the loopback
profile, with an explicit TCP port, auth collection, base collection list,
main and auxiliary prepared URLs, runtime role, data directory and reviewed
manifest file. It validates required settings, caps manifest input at 4 MiB,
and uses a cancellation context for process shutdown. It neither chooses
legacy credentials nor runs migrations. The configuration test covers
missing explicit settings. This is still not a general serving command.

The PostgreSQL 17/18 CI jobs already create throwaway databases using the
container administrator. That same fixture provisioner now creates a
dedicated test-only WAL login, without changing the application role's
attributes or granting the WAL login schema DDL. The new dedicated WAL
integration prepares publication metadata using the disposable fixture
owner, starts a node with explicit WAL credentials, verifies the active
slot session identity and observes a record update delivered over realtime.
The legacy fixture's SQL application account is still administrative;
this test isolates and verifies the actual WAL connection, not all four
principals together in a restricted production deployment. The CI-only
login is never configured in the legacy default or prepared loopback
server.

The existing disposable restricted-role test now records 200 alternating
warmed SELECT 1 checkout samples per pool, with baseline and guarded
p50/p95, surfaced in CI logs and the test-output artifact. This is a
microbenchmark of checkout/round-trip overhead, not the complete HTTP
workload or Supabase comparison. No latency threshold or result is claimed
before numerical evidence and review. All new tests are pending exact
candidate CI. Production provisioning and combined full runtime/replication
isolation remain open.
