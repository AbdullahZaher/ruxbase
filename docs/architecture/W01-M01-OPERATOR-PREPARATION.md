# M01 — standalone system preparation

The new `cmd/ruxbase-prepare` executable runs the existing system bootstrap
in a separate explicitly invoked process. It never registers HTTP serving
or user app migrations. No production execution is authorized by this PR.

The command requires an explicit confirmation (`--apply-system`) and
`RUXBASE_PREPARE_DATABASE_URL`, `RUXBASE_PREPARE_ROLE`, and
`RUXBASE_PREPARE_DATA_DIR`. The optional `RUXBASE_PREPARE_LOGS_URL` defaults
to the explicit operator DSN, not the serving environment. Optional
`RUXBASE_PREPARE_ENCRYPTION_ENV` identifies the settings encryption environment
variable. Use the same reviewed settings encryption configuration as the app.
Do not put credentials in command arguments, source control or logs.
The caller must stop serving instances, ensure backups and obtain the
appropriate migration authorization before running against any persistent
database. The integration tests operate only on a disposable CI fixture.

The bootstrap pool connector verifies session and current role before
returning each pool. The existing system migrations and RLS repair path
are reused. Application hooks and user JS migrations are not loaded by this
entry point. Identity checks are not per physical connection enforcement.
Transient bootstrap workers may still start. Existing bootstrap transactions
and error/rollback semantics are unchanged. This is not a new migration
engine or a runtime security boundary.

The original serving bootstrap still runs system migrations and RLS repair.
Do not deploy this as a least-privilege serving solution. Remaining M01 work:
explicit provisioning and migration authorities, complete grant manifest,
read-only readiness and drift checks, pool-wide authority admission,
removal of privileged startup actions and certification on real PG17/18
evidence. This increment does not certify recovery or production readiness.

## Read-only system ledger inspection

Select `--check-system` instead of `--apply-system` to inspect the
registered system migration history using an explicit connection and role.
The checker uses a bounded read-only repeatable-read transaction against
`public._migrations` and does not create a ledger when one is absent.
It rejects missing migrations, unverified conditional migrations and
identity mismatches. It prints JSON evidence and exits nonzero on rejection.

This is not complete readiness: app migrations, unknown historical entries,
checksums, auxiliary schema, privileges, RLS drift and DDL consistency
are separate gates. A ledger match does not authorize serving. Tests exercise
an empty disposable fixture, check that inspection creates no ledger, and
verify history after preparation. Existing serving is unchanged.
