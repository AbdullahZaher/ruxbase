# Ruxbase Wave 02 — Native Foreign Keys, Composite Keys & Relational Integrity

**Document Reference:** `docs/architecture/WAVE02-RELATIONS-AND-CONSTRAINTS.md`  
**Milestone:** W02-03 / P1 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

Wave 02 bridges PocketBase's application-layer relations with PostgreSQL's relational engine guarantees:
1. **Native Foreign Keys:** Single-relation fields can enforce true database engine foreign keys (`CONSTRAINT fk_... FOREIGN KEY (col) REFERENCES target(id) ON DELETE ...`).
2. **Deterministic Referential Actions:** Engine-level enforcement of `CASCADE`, `RESTRICT`, and `SET NULL` guarantees that referential integrity is preserved even if changes are made via direct SQL, external services, or background migrations.
3. **Documented Composite Keys:** Native PostgreSQL tables with composite primary keys (`PRIMARY KEY (tenant_id, id)` or `PRIMARY KEY (source_id, tag_id)`) are fully introspected and accessible via typed composite query helpers.
4. **Zero Orphan Invariant:** Invalid foreign keys are rejected at the database engine level with SQLSTATE 23503 (`foreign_key_violation`), preventing orphan or dangling references.

---

## 2. Foreign Key Referential Actions Specification

| Action (`OnDelete`) | PostgreSQL Clause | Engine Behavior on Parent Deletion |
|---|---|---|
| **`CASCADE`** | `ON DELETE CASCADE` | Child rows are deleted atomically within the parent deletion transaction. |
| **`RESTRICT`** | `ON DELETE RESTRICT` | Parent deletion is aborted with SQLSTATE 23503 if any referencing child rows exist. |
| **`SET NULL`** | `ON DELETE SET NULL` | Child foreign key column is updated to NULL (requires column to be nullable). |
| **`NO ACTION`** | `ON DELETE NO ACTION` | Default PostgreSQL deferred check; aborts if reference remains invalid at commit. |

---

## 3. Composite Primary Key Model & Query Architecture

### 3.1 Composite Key Structure
Native tables often use compound primary keys to represent multi-tenancy, partition sharding, or many-to-many junction tables:
```sql
CREATE TABLE tenant_members (
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    role text NOT NULL DEFAULT 'member',
    created timestamptz DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id),
    CONSTRAINT fk_tm_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT fk_tm_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

### 3.2 Composite Key Introspection & Access
- **Catalog Detection:** `IntrospectTable` detects multiple columns where `is_primary_key = true`.
- **Primary Key List:** `table.PrimaryKeyColumns()` returns ordered composite columns (e.g. `["tenant_id", "user_id"]`).
- **Composite Query Helper:**
  ```go
  record, err := core.FindRecordByCompositeKey(app, "tenant_members", map[string]any{
      "tenant_id": "tenant_123",
      "user_id":   "user_456",
  })
  ```
  The query executes parameterized composite equality:
  ```sql
  SELECT * FROM "tenant_members" WHERE "tenant_id" = {:k0} AND "user_id" = {:k1} LIMIT 1
  ```

---

## 4. Verification Matrix & Negative Constraints

1. **Foreign Key Violation Prevention:**
   Attempting to insert or update a child record with a non-existent parent ID MUST fail with SQLSTATE 23503.
2. **Cascade Deletion Verification:**
   Deleting a parent record MUST delete all referencing child records across all native FK relationships with `CASCADE`.
3. **Restrict Deletion Verification:**
   Deleting a parent record with child rows linked via `RESTRICT` MUST be blocked with SQLSTATE 23503.
4. **Composite Key Precision:**
   Composite key queries MUST return the exact single matching record or `sql.ErrNoRows`.
