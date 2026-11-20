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
The present CI service account is not such an authority; do not elevate
it just to satisfy this test. No production grants, credentials or
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
