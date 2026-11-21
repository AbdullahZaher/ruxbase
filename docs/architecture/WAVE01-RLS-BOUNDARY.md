# Wave 01 Milestone M05 — Native PostgreSQL RLS Boundary & Security-Invoker Views

## 1. Executive Summary

Milestone M05 establishes the native PostgreSQL Row-Level Security (RLS) boundary across the Ruxbase platform. Ruxbase compiles PocketBase-compatible collection API rules into native PostgreSQL RLS policies and table/column grants, enforcing identical security guarantees whether accessed via the HTTP REST API (with strict mode) or via direct PostgreSQL connections (e.g., BI tools, ETL pipelines, read replicas, and external microservices).

This milestone delivers:
1. **Security-Invoker Views**: All PostgreSQL views created in Ruxbase specify `WITH (security_invoker = true)`, guaranteeing that row-level security policies and table permissions on underlying base relations are enforced against the invoking role rather than executing with definer privileges.
2. **Privileged Routines Security & Search-Path Isolation**: `SECURITY DEFINER` routines in schema `ruxbase_rls` operate with immutable, hardened `SET search_path = pg_catalog, <schema>, pg_temp`, preventing search_path shadowing or privilege escalation attacks.
3. **Deny-by-Default Policy Compilation**: Collection rules containing API-only context (e.g. `@request.headers`, `@request.body`, `:isset`, `:changed`) are compiled to restrictive `USING (false)` / `WITH CHECK (false)` policies in PostgreSQL, preventing authorization widening.
4. **Column-Level Least Privilege**: External roles are granted column-level `SELECT`, `INSERT`, and `UPDATE` only on visible, non-hidden, non-generated columns; auth collection `email` and password credentials remain inaccessible via direct SQL.

---

## 2. Security-Invoker Views Architecture

### 2.1 The View Privilege Escalation Gap
In standard PostgreSQL (prior to PostgreSQL 15 or without `security_invoker = true`), SQL views execute with the privileges of the view owner (definer semantics). In Ruxbase, the view owner is the application database role (`ruxbase_app`), which owns the underlying tables and bypasses row-level security. Consequently, if an external role (`ruxbase_authenticated` or `ruxbase_api`) was granted access to a standard view, queries against that view would bypass all RLS policies on the underlying tables.

### 2.2 Security-Invoker Invariant
Under Milestone M05, every view created or managed by Ruxbase is defined with:
```sql
CREATE VIEW "view_name" WITH (security_invoker = true) AS SELECT ...
```
For existing and reloaded views, `ALTER VIEW "view_name" SET (security_invoker = true)` is enforced during RLS policy synchronization (`applyRLSDefinition`).

#### Behavioral Guarantees:
- **Base Table RLS Active**: When an external role or `ruxbase_api` queries a security-invoker view, PostgreSQL evaluates the RLS policies of each underlying base table using the session's active role and claims.
- **Permission Check**: If the querying role has not been granted `SELECT` on an underlying relation, PostgreSQL rejects the query immediately with `SQLSTATE 42501` (`permission denied for table ...`).
- **Row Filtering**: If the querying role has table access, only rows allowed by the underlying base tables' `ruxbase_list` / `ruxbase_view` policies are returned.

---

## 3. Privileged Routines & Search-Path Hardening

Ruxbase installs cryptographic token verification and relation traversal functions in the `ruxbase_rls` schema. Because policy functions for relation rules (`@collection.rel.*`) and `ruxbase_rls.auth()` execute as `SECURITY DEFINER`, they are potential targets for search_path hijacking if untrusted schemas precede system catalogs.

### Search-Path Guarantees
1. **`ruxbase_rls.auth()`**:
   ```sql
   CREATE OR REPLACE FUNCTION ruxbase_rls.auth() RETURNS jsonb
   LANGUAGE plpgsql STABLE SECURITY DEFINER
   SET search_path = pg_catalog, public, pg_temp ...
   ```
2. **Policy Evaluation Functions (`rls_<collectionId>_<rule>`)**:
   ```sql
   CREATE FUNCTION ruxbase_rls.rls_<id>_<rule>(text) RETURNS boolean
   LANGUAGE sql STABLE SECURITY DEFINER
   SET search_path = pg_catalog, public, pg_temp ...
   ```
3. **`pg_temp` Placement**: `pg_temp` is placed strictly last, preventing temporary tables created in user sessions from shadowing collection tables or cryptographic functions.
4. **Execution Grants**: Execution privileges on all internal functions in `ruxbase_rls` are revoked from `PUBLIC` and granted strictly to `ruxbase_anon`, `ruxbase_authenticated`, and `ruxbase_api`.

---

## 4. Policy Compilation & Deny-by-Default Semantics

| Rule Type | API Expression Example | RLS Policy Status | Generated PostgreSQL Policy |
|---|---|---|---|
| **Public List** | `""` | `translated` (inline) | `FOR SELECT USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'view' AND (TRUE))` |
| **Filtered List** | `@request.auth.id != ""` | `translated` (inline) | `FOR SELECT USING (COALESCE(...) <> 'view' AND (auth_expr))` |
| **Relational List** | `@collection.roles.user ?= id` | `translated` (function) | `FOR SELECT USING (COALESCE(...) <> 'view' AND ruxbase_rls.rls_<id>_list(id))` |
| **Superuser Only** | `null` | `superusersOnly` | *No policy created* (PostgreSQL RLS default denies all access) |
| **Untranslatable Rule** | `@request.body.status = 'open'` | `denied` | `FOR SELECT USING (FALSE)` (Fail-closed) |
| **Untranslatable Modifier** | `status:changed = true` | `denied` | `FOR SELECT USING (FALSE)` (Fail-closed) |

---

## 5. Column-Level Grant Boundaries

Direct PostgreSQL connections are strictly partitioned from elevated application credentials:

1. **System Collections & Tables**:
   - `_superusers`, `_externalAuths`, `_mfas`, `_otps`, `_authOrigins`: `REVOKE ALL FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api`.
   - `_collections`, `_params`, `_realtime_events`: `REVOKE ALL FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api`.
2. **Auth Collections**:
   - `SELECT`: Granted only on non-hidden fields; `email`, `passwordHash`, `tokenKey` are strictly excluded from external grants.
   - `INSERT`, `UPDATE`, `DELETE`: Completely revoked. Auth records can only be created, authenticated, or mutated through the Go API.
3. **Base Collections**:
   - `SELECT`: Granted on visible non-hidden fields if `listRule != nil`.
   - `INSERT`: Granted on writable columns if `createRule != nil`.
   - `UPDATE`: Granted on writable columns (excluding primary key `id`) if `updateRule != nil`.
   - `DELETE`: Granted if `deleteRule != nil`.

---

## 6. Verification and Negative Test Coverage

The native RLS boundary is validated by an exhaustive automated negative test suite in `tests/e2e/rls_boundary_negative_test.go`:
- `TestSecurityInvokerViewsEnforceUnderlyingRLS`: Proves security-invoker views filter base rows under RLS.
- `TestSecurityInvokerViewsDenyUnauthorizedTableAccess`: Proves views fail closed when an external role lacks base table permissions.
- `TestNegativeDirectSQLWritesRestrictedByRLS`: Proves unauthorized INSERT, cross-user UPDATE, and cross-user DELETE fail closed under RLS.
- `TestDirectSQLWriteDeniedOnAuthCollections`: Proves direct SQL writes to auth tables raise `permission denied`.
- `TestDirectSQLWriteDeniedOnSystemTables`: Proves direct SQL access to system metadata tables raises `permission denied`.
- `TestDirectSQLDeniedOnHiddenColumns`: Proves queries on hidden columns fail closed.
- `TestUntranslatableRulesFailClosedInRLS`: Proves untranslatable rules generate `USING (false)` and return 0 rows.
- `TestPrivilegedRoutinesSearchPathHardening`: Confirms routine search_path immutability and lack of public execute grants.
