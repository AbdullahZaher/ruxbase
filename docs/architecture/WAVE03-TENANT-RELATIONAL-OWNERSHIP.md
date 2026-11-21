# Wave 03: Enterprise IAM & Multi-Tenancy — Tenant-Bound Ownership & Restricted SQL Enforcement

```
Document ID:     RUXBASE-WAVE03-TENANT-RELATIONAL-OWNERSHIP
Version:         1.0.0
Milestone:       W03-04 (Tenant-Bound Record, Relation, File and Event Ownership with Restricted SQL Enforcement)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W03-04 establishes deterministic multi-tenant isolation across all data layers:
1. **Relational Boundaries**: Schema enforcement of `tenant_id` fields, foreign keys, and cross-tenant link prevention.
2. **PostgreSQL RLS Enforcement**: Native database policies ensuring un-scoped SQL queries fail closed.
3. **File System Isolation**: Tenant-scoped storage hierarchy preventing cross-tenant file exfiltration.
4. **Realtime Isolation**: Event topic isolation preventing cross-tenant notification leakage.

---

## 2. Relational & Schema Boundaries

### 2.1 Tenant-Bound Field & Constraints
When a collection is designated as tenant-scoped:
- The system automatically provisions and indexes a `tenant_id VARCHAR(15) REFERENCES _tenants(id) ON DELETE CASCADE` column.
- Records inserted without an explicit `tenant_id` inherit the active session's `tenant_id`.
- Records inserted with an explicit `tenant_id` differing from the caller's authorized context are strictly rejected.

### 2.2 Cross-Tenant Relation Prevention
When establishing relational links (e.g. Foreign Keys or Relation fields):
- The target record must reside within the exact same `tenant_id`.
- Any attempt to link records across differing tenant boundaries is rejected with `ErrCrossTenantRelationForbidden`.

---

## 3. Native PostgreSQL RLS SQL Policies

For each tenant-scoped table `t`:
```sql
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON t
  AS RESTRICTIVE
  FOR ALL
  TO ruxbase_authenticated, ruxbase_anon, ruxbase_app
  USING (tenant_id = NULLIF(current_setting('ruxbase.tenant_id', true), ''))
  WITH CHECK (tenant_id = NULLIF(current_setting('ruxbase.tenant_id', true), ''));
```

When `ruxbase.tenant_id` is not set or empty:
- `current_setting('ruxbase.tenant_id', true)` evaluates to `''` or NULL.
- `NULLIF('', '')` evaluates to NULL.
- `tenant_id = NULL` evaluates to FALSE (SQL ternary logic).
- Result: **0 rows visible, all mutations rejected.**

---

## 4. File Storage & Realtime Event Isolation

### 4.1 Storage Key Hierarchy
Tenant files are persisted under:
```
<storage_root>/tenants/<tenant_id>/<collection>/<record_id>/<filename>
```
File resolution verifies that the active principal's `tenant_id` matches the storage path prefix.

### 4.2 Realtime Dispatcher Filtering
Realtime notifications emitted via PostgreSQL `LISTEN/NOTIFY` or in-memory bus include:
```json
{
  "action": "create",
  "collection": "orders",
  "record_id": "rec_123",
  "tenant_id": "ten_abc"
}
```
The dispatcher filters client subscriptions: events with `tenant_id != client.tenant_id` are dropped before transmission.
