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

This is not a complete authority manifest. It does not inspect every object
grant, routine, membership administration option, column ACL, default grant,
or RLS policy. Authority may change while a connection is already held;
the connector does not enforce policy on every statement or support a
transaction pooler that changes backend identity mid-session. Prepared
bootstrap remains metadata-only. No HTTP serving, production readiness or
runtime authorization certification is claimed. Catalog checks on checkout
add round trips; measure their cost before choosing a serving admission
strategy. The existing lightweight legacy serving profile is unchanged.

Next: complete the declared application grant manifest, prove RLS and
transaction context invariants, and implement a separately gated functional
serving profile with disposable integration evidence.
