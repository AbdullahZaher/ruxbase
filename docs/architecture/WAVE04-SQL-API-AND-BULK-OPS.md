# Wave 04: Unified Data API & Developer Experience — Explicit SQL Exposure & Bounded Bulk Operations

```
Document ID:     RUXBASE-WAVE04-SQL-API-AND-BULK-OPS
Version:         1.0.0
Milestone:       W04-02 (Explicit SQL API Exposure, Authorized RPC, Filter/Sort/Pagination Semantics and Bounded Bulk Operations)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W04-02 establishes a secure, high-performance REST query gateway for native PostgreSQL tables and stored routines alongside collection records:
1. **Exposed Table REST Interface**: Standardized paginated, sorted, and filtered read access for explicitly exposed native tables.
2. **Authorized SQL RPC**: Parameterized stored procedure execution governed by explicit exposure rules and invoker security.
3. **Bounded Bulk Operations**: High-throughput batch mutations bounded by strict size limits ($N \le 500$) and atomic transaction timeouts.

---

## 2. Table Query Semantics

When querying an exposed table via `/api/sql/tables/:table`:
- **Table Exposure Gate**: The table MUST be registered with `is_exposed = true` in `_table_exposure`. If not, returns `404 Not Found`.
- **Authorization Gate**: Evaluates `access_rule` (e.g. `@request.auth.id != ""`, superuser only, or public).
- **Pagination**:
  - `page`: default 1, min 1.
  - `perPage`: default 30, max 500 (strictly clamped to prevent memory exhaustion).
- **Sorting**:
  - Parameter format: `?sort=-created,+id`. Quoted as identifiers, verified against table column catalog.
- **Filtering**:
  - Parameter format: `?filter=status='active'`. Parameterized using existing search tokenizer and parser.

---

## 3. Bounded Bulk Operations

Bulk operations (`/api/sql/tables/:table/bulk`):
1. **Size Bound**: Payloads exceeding 500 records are rejected with `400 Bad Request` (`bulk_limit_exceeded`).
2. **Transaction Isolation**: Executes within `RunInTransaction` with `lock_timeout = 3s` and `statement_timeout = 15s`.
3. **Audit Envelope**:
```json
{
  "affected": 42,
  "ids": ["rec_1", "rec_2", ...],
  "duration_ms": 14
}
```
