# Wave 01 / M01 — runtime role preflight (increment 1)

Starting main: `74865d842ea0e99cad0c2f8fa247f58535296075`
(tree `2fe208a03c8a8a60484af71a41bfc62455473ffe`).
This isolated increment is part of RB-SEC-01, not its closure. No serving
behavior, migration path, RLS rules, grants, or authority are changed.

## Executable diagnostic

For a specifically authorized test or staging connection, set the environment
variable `RUXBASE_ROLE_CHECK_URL` using the existing secret management mechanism.
The tool does not fall back to DATABASE_URL, launch the application, start an
embedded database, or bootstrap migrations.

```sh
go run ./cmd/ruxbase-role-check
```

The command opens a bounded read-only transaction. JSON reports the session
and current identity, conservative privileged membership, database and
non-system schema ownership and creation rights, and relation/routine ownership.
It exits nonzero for failed screens or connection/catalog errors. It does not
print the connection string. Treat the report as internal operational metadata.

This is a conservative initial screen: membership is flagged even if its SET
or inheritance options disallow exercising the authority. An empty screen is
not proof of complete least privilege. Object grants, default privileges, role
administration, function execution, extension authority, RLS policy coverage,
replication, transaction context, startup integration and revocation need
separate review and tests. Do not use the screen as a production certification.

## Evidence

The pure report tests cover findings, missing input, and identity mismatch.
The PostgreSQL integration test uses an existing disposable CI database fixture
to verify that an owner identity fails the screen and cancellation is rejected.
It requires RUXBASE_TEST_URL; without the fixture the DB test is skipped.
The existing suite on PostgreSQL 17 and 18 must pass before integration.

## M01 remaining

- Separate runtime, provisioning/migration and replication identity contracts.
- Define exact allowed grants and lifecycle for supported deployment profiles.
- Prove restricted identity success and reject excess authority in disposable
  database tests, including relevant memberships and object privileges.
- Move privileged bootstrap and schema operations outside normal serving.
- Establish enforced startup admission, with explicit migration compatibility
  and recovery plan. Preserve legacy behavior until an approved transition.
- Independent review; no production release and no claim of tenant isolation.

Wave 00 issue #4 remains open. Work belongs in the Wave 01 integration branch,
not directly in main.
