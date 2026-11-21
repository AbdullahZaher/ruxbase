# Ruxbase Wave 02 — Authorized SQL-First Introspection & Deny-by-Default Table Exposure

**Document Reference:** `docs/architecture/WAVE02-AUTHORIZED-INTROSPECTION.md`  
**Milestone:** W02-02 / P0 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Core Principle

In PostgreSQL, database tables exist as relational entities in namespaces (`public`, `auth`, etc.). However, **a table in the PostgreSQL database must NEVER be automatically exposed to REST APIs, GraphQL, SDKs, or client applications simply because it exists**.

Wave 02 establishes a strict **Authorized Introspection and Explicit Exposure Model**:
1. **Zero-Trust Introspection:** Only authenticated superusers / administrators possess authority to introspect the PostgreSQL catalog (`information_schema`, `pg_catalog`). Anonymous callers and regular authenticated users are rejected with HTTP 403 Forbidden.
2. **Explicit Exposure Declaration:** Native tables are exposed to the unified Ruxbase API only through an explicit registration action that assigns explicit collection rules (`listRule`, `viewRule`, `createRule`, `updateRule`, `deleteRule`).
3. **Deny-by-Default:** Newly introspected or registered tables default to `nil` rules (strict superuser-only access). Anonymous or unprivileged access is denied unless an explicit rule (e.g. `@request.auth.id != ""`) is intentionally defined and audited.
4. **Catalog Protection:** System catalogs (`pg_*`, `information_schema`) and internal Ruxbase metadata tables (`_collections`, `_params`, `_migrations`, `_mfas`, `_otps`, `_externalAuths`) are permanently prohibited from public API exposure.

---

## 2. Introspection Security Lifecycle

```
[ PostgreSQL Database Catalog ]
           │
           │ (pg_class, pg_attribute, pg_constraint)
           ▼
[ Authorized Introspection Service ] <── Auth Check: Superuser ONLY
           │                             (Anon/Auth Users -> HTTP 403)
           ▼
[ Native Table Definition & Column Map ]
           │
           │ Explicit Exposure Request
           ▼
[ Security Boundary Validation ]
    ├── Reject system schemas (pg_catalog, information_schema)
    ├── Reject internal tables (_collections, _params, etc.)
    └── Require valid primary key definition
           │
           ▼
[ Registered Collection Definition ]
    ├── Fields mapped via MapNativeColumnToField
    ├── Deny-by-default rules (nil / admin-only)
    └── RLS Policy Sync (FORCE ROW LEVEL SECURITY)
           │
           ▼
[ Unified Ruxbase Data API & SDKs ]
```

---

## 3. Threat Matrix & Countermeasures

| Threat ID | Threat Vector | Countermeasure |
|---|---|---|
| **T-02-01** | Anonymous user queries `/api/ruxbase/tables` to discover DB structure | Deny-by-default: Route explicitly requires `apis.RequireSuperuserAuth()` or equivalent admin check. |
| **T-02-02** | Authenticated tenant user attempts to introspect peer tables | Introspection endpoints reject non-admin tokens with 403 Forbidden. |
| **T-02-03** | Exposure of internal tables (`_params`, `_collections`) leading to token/hash theft | `ValidateExposedTable` blocks any table beginning with `_` or in system schemas. |
| **T-02-04** | Table created via raw SQL automatically accessible via REST API | Pure SQL tables remain unexposed (returning 404 in `/api/collections/:table/records`) until explicitly registered. |
| **T-02-05** | Native table exposed without primary key causing ambiguous update/delete | `ValidateExposedTable` requires at least one primary key column. |
| **T-02-06** | Stale schema drift between native PostgreSQL table and exposed collection | Introspection compares catalog fingerprint; schema updates require explicit synchronization. |

---

## 4. API Specification for Table Introspection & Exposure

### 4.1 `GET /api/ruxbase/tables`
Lists all candidate native tables and views in the database.
- **Authorization:** Superuser / Platform Administrator only.
- **Query Parameters:** `schema` (optional, defaults to `public`).
- **Response Shape:**
  ```json
  [
    {
      "schema": "public",
      "name": "articles",
      "kind": "r",
      "isExposed": true,
      "columnsCount": 8,
      "hasPrimaryKey": true,
      "rowSecurity": true
    }
  ]
  ```

### 4.2 `GET /api/ruxbase/tables/{name}`
Inspects a specific table, returning full column definitions, constraints, indexes, and suggested Collection field mappings.
- **Authorization:** Superuser / Platform Administrator only.
- **Response Shape:**
  ```json
  {
    "table": { ... },
    "suggestedCollection": {
      "name": "articles",
      "type": "base",
      "fields": [ ... ],
      "listRule": null,
      "viewRule": null
    }
  }
  ```

### 4.3 `POST /api/ruxbase/tables/expose`
Exposes an existing native PostgreSQL table as an active Collection with explicit access rules.
- **Authorization:** Superuser / Platform Administrator only.
- **Request Body:**
  ```json
  {
    "tableName": "articles",
    "collectionName": "articles",
    "listRule": "@request.auth.id != \"\"",
    "viewRule": "@request.auth.id != \"\"",
    "createRule": null,
    "updateRule": null,
    "deleteRule": null
  }
  ```
- **Post-Conditions:**
  - Collection created in `_collections`.
  - Native table DDL is **not** recreated or modified.
  - Native table has `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY` verified.
  - API endpoint `/api/collections/articles/records` becomes available immediately.

---

## 5. Verification & Acceptance Criteria
1. Anonymous and standard authenticated requests to `/api/ruxbase/tables` MUST fail with HTTP 401/403.
2. Superuser requests to `/api/ruxbase/tables` MUST return valid native tables.
3. System catalogs and internal Ruxbase tables MUST be rejected from exposure.
4. An unexposed native PostgreSQL table MUST return HTTP 404 on `/api/collections/{name}/records`.
5. Once exposed with explicit rules, the table MUST be queryable through `/api/collections/{name}/records` with full RLS and rule evaluation.
