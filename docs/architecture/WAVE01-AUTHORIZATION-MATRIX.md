# Wave 01 Milestone M03 — Cross-Path Authorization Coverage Matrix

This document provides the authoritative cross-path authorization coverage matrix and fail-closed security specification for Ruxbase under **Wave 01 Milestone M03: Unified Authorization Architecture & Cross-Path Enforcement** (W01-M03-01, W01-M03-02, W01-M03-03).

---

## 1. Executive Summary & Policy Contract

Ruxbase delivers a unified, PostgreSQL-native security architecture. In contrast to fragmented platforms where HTTP APIs, direct database connections, realtime streams, file access, and AI tool invocations operate under disjoint authorization engines, Ruxbase compiles and enforces declarative collection access rules consistently across **all six access paths**:

1. **REST API** (`/api/collections/:collection/records...`)
2. **PostgreSQL Direct / SQL** (`ruxbase_anon`, `ruxbase_authenticated`, `ruxbase_api` using native PostgreSQL RLS)
3. **Realtime SSE & Cluster** (`/api/realtime`, cluster notify event broadcast)
4. **File Storage** (`/api/files/:collection/:id/:filename`, protected file tokens)
5. **Search & Vector Search** (`/api/collections/:collection/records`, `/api/collections/:collection/vector-search`)
6. **Model Context Protocol (MCP)** (`/mcp`, AI agent tool discovery and execution)

### Fundamental Authorization Invariants

- **Fail-Closed by Default:** Any access rule that is `nil` permits access **only to superusers**. Non-superusers receive explicit denial (`403 Forbidden`, `404 Not Found`, or SQL row denial).
- **Consistent Rule Mapping:** Every access path evaluating an operation maps directly to the collection's corresponding rule (`ListRule`, `ViewRule`, `CreateRule`, `UpdateRule`, `DeleteRule`).
- **Deny-by-Default for Unsupported Conditions:** If an access rule contains request-bound variables (e.g. `@request.headers.*`, `@request.body.*`, `@request.query.*`, `:isset`, `:changed`) that cannot be translated into PostgreSQL RLS, direct SQL access compiles to `FALSE` (denying all rows). If a rule expression contains syntax errors or references non-existent columns, all paths fail closed immediately.
- **Strict Authority Separation:** Database roles `ruxbase_anon`, `ruxbase_authenticated`, and `ruxbase_api` are `NOLOGIN`, `NOINHERIT`, non-superuser, and possess no `BYPASSRLS`. System tables (`_superusers`, `_collections`, `_params`, `_externalAuths`) have all permissions revoked from these roles.

---

## 2. Principal & Identity Hierarchy

Ruxbase identifies four distinct principal classes across the system:

| Principal Class | Description | Representation in REST/Realtime/Storage/MCP | Representation in Direct PostgreSQL (SQL) |
|---|---|---|---|
| **Superuser / Platform Admin** | Elevated administrative identity. Bypasses collection access rules. Managed exclusively in `_superusers`. | Valid JWT signed by superuser token key (`@request.auth` is superuser record). | Application connection owner role or database superuser (bypasses RLS). |
| **Authenticated Record** | Normal user or entity authenticated against an auth collection. | Valid JWT signed by record token key (`@request.auth` is user record). | External login role with `ruxbase_authenticated` role + verified RLS token set via `ruxbase_rls.set_claims()`. |
| **Anonymous / Guest** | Unauthenticated public caller. | No `Authorization` header (`@request.auth = nil`). | External login role with `ruxbase_anon` role (`ruxbase.token` is unset or empty). |
| **Strict API Worker** | Internal query executor in strict mode (`RUXBASE_RLS_STRICT=true`). | Internal application request context. | `ruxbase_api` role with short-lived RLS token and explicit rule selector (`ruxbase.rule`). |

---

## 3. Cross-Path Operation & Rule Mapping Matrix

The following matrix maps each operation across all six access paths to its authoritative collection access rule and enforcement mechanism:

| Operation | Access Path | Evaluated Rule | Enforcement Mechanism | Behavior when Rule is `nil` (Superusers Only) | Behavior when Rule is `""` (Public) | Behavior with Conditional Expression |
|---|---|---|---|---|---|---|
| **List Records** | REST API | `collection.ListRule` | Query filter injection via `RecordFieldResolver` | `403 Forbidden` | All visible columns returned | Only rows matching expression returned |
| **List Records** | Direct SQL | `collection.ListRule` | PostgreSQL Row-Level Security policy `ruxbase_list` | Table `SELECT` privilege revoked (`permission denied`) | Policy: `USING (true)` | Policy: `USING (compiled_expr)` |
| **List Records** | Realtime SSE | `collection.ListRule` | `CanAccessRecord` checked before broadcast for wildcard subscriptions | Broadcast dropped; no event delivered | Event delivered with public fields | Event delivered only if record matches expression |
| **List Records** | Vector Search | `collection.ListRule` | Query filter injection via `RecordFieldResolver` | `403 Forbidden` | Vector search across all records | Vector search filtered by expression |
| **List Records** | MCP Tool | `collection.ListRule` | `allowed()` tool gate + internal REST dispatch | Tool omitted from server; direct dispatch 403 | Tool `list_<collection>` exposed and callable | Tool exposed; results filtered by expression |
| **View / Get Record** | REST API | `collection.ViewRule` | `FindRecordById` with access check | `403 Forbidden` | Record returned | Record returned if matches; `404 Not Found` if not |
| **View / Get Record** | Direct SQL | `collection.ViewRule` | Enforced only for `ruxbase_api` in strict mode; external SQL roles use `ListRule` | Policy omitted; denied | Policy: `USING (true)` for `ruxbase_api` | Policy: `USING (compiled_expr)` for `ruxbase_api` |
| **View / Get Record** | Realtime SSE | `collection.ViewRule` | `CanAccessRecord` checked before broadcast for specific record subscriptions | Broadcast dropped; no event delivered | Event delivered | Event delivered only if record matches expression |
| **View / Get Record** | Protected File | `collection.ViewRule` | `CanAccessRecord` checked before file stream | `404 Not Found` | File served | File served if record matches; `404 Not Found` if not |
| **View / Get Record** | MCP Tool | `collection.ViewRule` | `allowed()` tool gate + internal REST dispatch | Tool omitted; direct dispatch 403 | Tool `get_<collection>` exposed and callable | Tool exposed; record returned or 404 |
| **Create Record** | REST API | `collection.CreateRule` | CTE expression evaluation against submitted record data | `403 Forbidden` | Record created | Evaluated against new record data; `400/403` if unsatisfied |
| **Create Record** | Direct SQL | `collection.CreateRule` | PostgreSQL RLS policy `ruxbase_create` (`WITH CHECK`) | Table `INSERT` privilege revoked | Policy: `WITH CHECK (true)` | Policy: `WITH CHECK (compiled_expr)` |
| **Create Record** | MCP Tool | `collection.CreateRule` | `allowed()` tool gate + internal REST dispatch | Tool omitted; direct dispatch 403 | Tool `create_<collection>` exposed | Record created or 400/403 |
| **Update Record** | REST API | `collection.UpdateRule` | `FindRecordById` rule check + form submission | `403 Forbidden` | Record updated | `404 Not Found` if existing record fails rule; error if invalid |
| **Update Record** | Direct SQL | `collection.UpdateRule` | PostgreSQL RLS policy `ruxbase_update` (`USING`) | Table `UPDATE` privilege revoked | Policy: `USING (true)` | Policy: `USING (compiled_expr)` |
| **Update Record** | MCP Tool | `collection.UpdateRule` | `allowed()` tool gate + internal REST dispatch | Tool omitted; direct dispatch 403 | Tool `update_<collection>` exposed | Record updated or 404 |
| **Delete Record** | REST API | `collection.DeleteRule` | `FindRecordById` rule check + record delete | `403 Forbidden` | Record deleted | `404 Not Found` if existing record fails rule |
| **Delete Record** | Direct SQL | `collection.DeleteRule` | PostgreSQL RLS policy `ruxbase_delete` (`USING`) | Table `DELETE` privilege revoked | Policy: `USING (true)` | Policy: `USING (compiled_expr)` |
| **Delete Record** | MCP Tool | `collection.DeleteRule` | `allowed()` tool gate + internal REST dispatch | Tool omitted; direct dispatch 403 | Tool `delete_<collection>` exposed | Record deleted or 404 |

---

## 4. Policy Condition Taxonomy & Cross-Path Support

| Condition Type | Example Syntax | REST API | SQL / RLS | Realtime SSE | Storage / Files | Search / Vector | MCP | Fail-Closed Policy on Unsupported Path |
|---|---|---|---|---|---|---|---|---|
| **Column Literals & Operators** | `status = 'active' && count > 5` | Supported | Supported (inlined in policy) | Supported | Supported | Supported | Supported | N/A |
| **Authenticated ID** | `@request.auth.id != '' && owner = @request.auth.id` | Supported | Supported (`ruxbase_rls.auth() ->> 'id'`) | Supported | Supported | Supported | Supported | Evaluates to `NULL` / denied if unauthenticated |
| **Auth Record Fields** | `@request.auth.verified = true` | Supported | Supported (`SECURITY DEFINER` helper) | Supported | Supported | Supported | Supported | Evaluates to `NULL` / denied if unauthenticated |
| **Direct Relations** | `author.active = true` | Supported (SQL JOIN) | Supported (`SECURITY DEFINER` helper) | Supported | Supported | Supported | Supported | Missing relation row yields `NULL` (denied) |
| **Back Relations & Subqueries** | `@collection.demo.user ?= id` | Supported (EXISTS subquery) | Supported (`SECURITY DEFINER` helper) | Supported | Supported | Supported | Supported | Subquery failure evaluates to `FALSE` (denied) |
| **Request Headers** | `@request.headers.x_secret = '123'` | Supported | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **SQL RLS compiles to `FALSE` (all rows denied); Realtime/Storage/MCP evaluates to `NULL` (denied)** |
| **Request Body / Payload** | `@request.body.status = 'draft'` | Supported (Create/Update) | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **SQL RLS compiles to `FALSE` (denied); Other paths deny access** |
| **Request Query Params** | `@request.query.mode = 'admin'` | Supported | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **SQL RLS compiles to `FALSE` (denied); Other paths deny access** |
| **Request Context / Method** | `@request.method = 'GET'` | Supported | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **SQL RLS compiles to `FALSE` (denied); Other paths deny access** |
| **Change Modifiers** | `title:changed = true`, `slug:isset` | Supported (Create/Update) | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **Unsupported** | **SQL RLS compiles to `FALSE` (denied); Other paths deny access** |
| **Malformed / Invalid Rules** | `non_existent_column > 0`, syntax error | Fail-Closed (Error) | Fail-Closed (`FALSE`) | Fail-Closed (`false`) | Fail-Closed (`404`) | Fail-Closed (Error) | Fail-Closed (Error) | **Denied across 100% of paths** |

---

## 5. Security Boundaries & Threat Model Mitigations

### 5.1 Admin Bypass Prevention
- **System Table Isolation:** System tables `_superusers`, `_collections`, `_params`, and auth tables `_externalAuths`, `_mfas`, `_otps`, `_authOrigins` have all privileges revoked from `ruxbase_anon`, `ruxbase_authenticated`, and `ruxbase_api`. Direct SQL queries against these tables fail with `permission denied`.
- **JWT Key Separation:** Tokens are signed using secret keys specific to the auth record and collection. A normal user token cannot be forged into a superuser token.
- **Rule Nil Invariant:** Setting a rule to `nil` revokes table privileges in SQL and triggers `403 Forbidden` in the API, ensuring no non-superuser can access the resource under any circumstances.

### 5.2 Cross-Tenant Isolation
- Multi-tenant collections use tenant-bound access rules (e.g. `tenant = @request.auth.id`).
- When Tenant B attempts to read, modify, delete, or subscribe to Tenant A's records:
  - **REST:** Filter returns only Tenant B rows; single record requests return `404 Not Found`.
  - **SQL (RLS):** Policies filter rows at the database engine level. Tenant A rows do not exist in Tenant B's result set. `UPDATE` and `DELETE` affect 0 rows.
  - **Realtime:** Events are evaluated against subscriber's `RequestInfo`. Tenant B subscribers receive zero broadcast notifications for Tenant A changes.
  - **Storage:** Protected file requests verify `collection.ViewRule`. Tenant B receives `404 Not Found` when requesting Tenant A files.
  - **MCP:** Tool execution routes through internal HTTP handler with Tenant B's auth token. Returns `404 Not Found`.

### 5.3 PostgreSQL Session & Connection Pool Isolation
- External clients switch to restricted roles using `set_config('role', role, true)` (transaction-scoped).
- RLS tokens set via `ruxbase_rls.set_claims(token, true)` are bound strictly to the current transaction.
- When connections return to the pool, the session state is discarded/reset, preventing any identity or claim leakage to subsequent transactions.

---

## 6. Verification & Test Evidence

The cross-path authorization matrix is verified through executable automated tests:

1. **Unit & Compiler Tests (`core/rls_test.go`, `core/authz_test.go`):**
   - Verification of compilation statuses: `RLSStatusTranslated`, `RLSStatusSuperusersOnly`, `RLSStatusDenied`.
   - Verification of policy SQL generation and deny-by-default (`FALSE`) for untranslatable rules.
2. **End-to-End Negative Authorization Suite (`tests/e2e/authorization_negative_test.go`):**
   - Admin bypass prevention across REST, SQL, Realtime, Storage, and MCP.
   - Forged and malformed token rejection across REST and SQL.
   - Cross-tenant isolation matrix across records, files, realtime events, and MCP tools.
   - Unsupported policy condition fail-closed behavior across all paths.
   - System table protection against direct SQL access.
