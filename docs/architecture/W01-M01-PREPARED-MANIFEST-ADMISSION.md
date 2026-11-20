# W01-M01 — reviewed prepared manifest admission

Baseline: 63efbd3db7c1aa9464295baeeb703e4862034348
(tree 34ae71aa74bb131a01da631cbe99be92a8a87a96).

A new explicit Go configuration field, BaseAppConfig.PreparedManifestJSON,
accepts a reviewed JSON document containing main and auxiliary GrantManifest
objects. Both identities must match PreparedRuntimeRole, and each manifest
must specify extended schema contracts. Unknown JSON fields and trailing
documents are rejected. Setting a manifest without a prepared runtime identity
cannot fall through to legacy bootstrap. An omitted manifest retains the
previous metadata-only diagnostic profile; neither profile authorizes HTTP.

Both guarded pools independently verify their schema inventories and effective
privileges before loading settings and collections. A failed check aborts the
prepared bootstrap and clears partial initialization. The expected manifest
is supplied by the trusted caller, not inferred or approved from observed
runtime grants. Each grant inspection normalizes its transaction-local search
path to pg_catalog for consistent routine argument signatures across pools.

The disposable application fixture uses the reviewed metadata-table SELECT
and schema USAGE policy plus expected default PUBLIC EXECUTE for public routine
identities in that test fixture only. It enumerates routine names, not actual
runtime ACLs, to construct its expectation. It tests positive initialization,
independent main/auxiliary schema mismatch rejection, metadata grant drift,
missing settings rejection, and the unchanged HTTP and migration restrictions.
This fixture is not a production serving grant manifest or a recommendation
to expose routines in production.

The manifest check is an admission-time snapshot per database, not a single
atomic cross-database transaction or continuous grant enforcement. The guarded
connector still rechecks its conservative identity and authority screen when
connections are acquired, not all manifest entries. Role administration,
default ACLs, other object classes, RLS semantics, security-definer routine
review, and in-flight grant drift remain separate requirements. Runtime
authentication, CRUD, background services and restricted HTTP admission
remain unimplemented in this experimental profile.

Legacy serving, operator preparation, production privileges and main are
unchanged. Independent certification and Wave 00 owner gates remain open.

## CI remediation and structural RLS readiness

Initial candidate CI #44715927602 failed formatting and both PG17/18
prepared manifest integration tests: the supposed matching main manifest
was rejected before the auxiliary-specific assertion. The fixture now
canonicalizes the explicitly reviewed column identifiers using PostgreSQL
identifier formatting and independently checks the expected manifest before
bootstrap with fixture-only drift evidence. Expected privileges are not
populated from observed ACLs. The exact remaining mismatch, if any, requires
the new CI evidence; no success is claimed before that run completes.

Prepared metadata inspection also checks one-to-one coverage of the RLS
state entries and registered collections in its existing read-only snapshot,
relation presence and expected relation kind, and enabled RLS for non-view
collections. The disposable fixture verifies that an incomplete marker is
rejected and that restoration of the original fixture state permits
initialization. This does not validate policy expressions, function bodies,
policy roles, view execution privileges, forced RLS or tenant isolation.
HTTP remains disabled; no change to production or legacy serving.

## Integrated closure candidate — remaining M01 work

The subsequent CI diagnostics on PG17/18 identified the original fixture
mismatch: migration 1640988000_aux_init.go installs copies of
dbutils.PostgresFunctionsSQL in _aux as well as public. The fixture policy
now explicitly accounts for default PUBLIC EXECUTE in both schemas; routine
identity enumeration does not read runtime ACLs. No checker relaxation.

The offline preparation executable now accepts --preprovisioned-roles with
--apply-system. This opt-in requires a non-administrative migration
principal and previously provisioned RLS roles with SET but not effective
inherited authority. The bootstrap checks before its migration and worker
initialization; InstallRLS does not invoke the legacy role-creation/grant
repair path in this mode. The existing administrative CI owner must be
rejected without creating a ledger. This is a partial separation: a
positive full migration on a separate database owner, explicit provisioning
lifecycle/grants, and replication authority separation are not yet proven.

The prepared fixture also exercises the original HTTP router and handlers
in-process with an explicit test-local route allowlist, two disposable
collections, a restricted runtime role, fixture-specific row policies,
and a reviewed grants manifest. The test asserts password login/rejection,
authenticated record create/read/update/delete, guest denial, persistence,
and absence of administrative routes from the test-local allowlist. It
does not start a public server or modify the legacy router. Fixture-specific
policies do not establish generic native RLS semantics or a complete
restricted serving boundary. No production credentials, role grants or
deployment are changed. The original serving entrypoint remains blocked
for the prepared profile.

Acceptance remains open until the exact integrated candidate passes CI,
the remaining operator/replication/serving responsibilities are separated,
the compatibility and cost are measured, and independent review accepts
the evidence. M01 must not be closed on a test-only HTTP fixture.
