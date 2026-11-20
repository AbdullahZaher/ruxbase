# W01-M01 — guarded prepared runtime pools

Starting wave HEAD: 6456b7f6417621da034991517e0921c02ee1c50d
(tree 7e7f311dadaf4c3946fdac07846d8e1b09c344a3).

The prepared metadata-only profile now uses a dedicated pgx connector for
each of its main and auxiliary pools. It does not use a caller-provided
DBConnect override. Legacy bootstrap and ordinary DBConnect are unchanged.
The connector checks declared identity, the existing conservative authority
screen, and the expected search_path on new physical connections and
database/sql session reset. A failed reuse check marks the connection bad.
Both pools retain their configured size and lifetime settings.

Disposable PostgreSQL tests exercise three simultaneously held physical
connections, repeated checkout, excess authority rejection after a fixture
grant change, recovery after correction and owner/mismatched identity
rejection. The existing prepared bootstrap integration tests also exercise
the guarded pools and HTTP/migration refusal. No production grants are changed.

The pool checks remain a conservative initial authority screen, not a
complete grant manifest check. They do not enforce the new offline manifest
on checkout, membership administration options, default ACLs or RLS policy. Authority may change while a connection is already held;
the connector does not enforce policy on every statement or support a
transaction pooler that changes backend identity mid-session. Prepared
bootstrap remains metadata-only. No HTTP serving, production readiness or
runtime authorization certification is claimed. Catalog checks on checkout
add round trips; measure their cost before choosing a serving admission
strategy. The existing lightweight legacy serving profile is unchanged.

Next: complete the declared application grant manifest, prove RLS and
transaction context invariants, and implement a separately gated functional
serving profile with disposable integration evidence.

## Extended offline authority manifest

The existing single-schema JSON grant contract accepts `extended: true` to
include all effective column SELECT/INSERT/UPDATE/REFERENCES privileges and
routine EXECUTE privileges, including grant options. A table privilege also
appears on each applicable column; this is deliberate effective-authority
enumeration, not a copy of explicit ACL entries. Column objects use quoted
relation and column identifiers joined by a dot. Routine objects use the
quoted routine name and PostgreSQL identity argument signature; use the
reviewed connection search path consistently when constructing a manifest.
Default PUBLIC execution and inherited rights are included when the
PostgreSQL privilege functions report them. Legacy scoped contracts remain
unchanged unless extended mode is requested.

Set `RUXBASE_ROLE_CHECK_MANIFEST_FILE` instead of
`RUXBASE_ROLE_CHECK_GRANTS_FILE` to check a JSON manifest with `role` and
`schemas` (an array of explicit extended grant contracts). Each schema
contract must declare the same role, an explicit schema, `extended: true`,
and an array of grants. The checker compares all non-system schema names
and their scoped grants in one read-only repeatable-read transaction.
Missing/unexpected schemas or grants fail the diagnostic. The main and
auxiliary databases must be assessed separately if they are different
databases. Do not generate or approve expected grants from observations
without review.

Tests cover effective columns, routine execution, omitted schema detection
and manifest validation. Full application grant enumeration and a positive
end-to-end application manifest fixture are still outstanding. This offline
diagnostic does not inspect role administration, default ACLs, all other
database object classes, RLS integrity or authority drift during a checked-out
session. It does not enable HTTP or change any production privileges.
