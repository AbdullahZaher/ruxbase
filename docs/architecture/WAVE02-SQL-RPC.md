# Ruxbase Wave 02 — SQL RPC Integration & Shared Authorization

**Document Reference:** `docs/architecture/WAVE02-SQL-RPC.md`  
**Milestone:** W02-08 / P1 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

PostgreSQL stored procedures and functions provide expressive server-side business logic.
Wave 02 bridges PostgreSQL functions into the Ruxbase API layer with strict security guarantees:
1. **Shared Policy Kernel:** RPC functions share the exact same authorization and policy evaluation model as Collections.
2. **Deny-by-Default:** Stored procedures are never reachable via API simply because they exist in the database. Functions must be explicitly registered and exposed by an authorized superuser.
3. **Structured Parameter Binding:** RPC callers provide arguments as JSON key-value pairs. Ruxbase maps arguments to function parameters by name, type-checks inputs, and executes parameterized SQL (`SELECT * FROM func(:arg1, :arg2)`), preventing SQL injection.
4. **Metadata Introspection:** Superusers can discover functions, signatures, return types, volatility, and exposure status via administrative API endpoints.

---

## 2. API Endpoints Specification

| Method | Path | Authorization | Purpose |
|---|---|---|---|
| `GET` | `/api/ruxbase/rpc/functions` | Superuser Only | Discover candidate functions in PostgreSQL schema. |
| `GET` | `/api/ruxbase/rpc/functions/{name}` | Superuser Only | Detailed introspection of parameter signatures and return types. |
| `POST` | `/api/ruxbase/rpc/expose` | Superuser Only | Register an exposed function with explicit access rule. |
| `POST` | `/api/ruxbase/rpc/unexpose` | Superuser Only | Revoke API exposure of a function. |
| `POST` | `/api/ruxbase/rpc/{name}` | Policy-Controlled | Execute an exposed PostgreSQL function. |

---

## 3. Authorization Rules Specification

The `access_rule` on an exposed RPC function maps directly to Ruxbase collection rule semantics:
- **`nil` (Default):** Superuser Only. Anonymous or regular authenticated users receive `403 Forbidden` (or `401 Unauthorized`).
- **`""` (Empty string):** Public Access. Anyone (including anonymous callers) may invoke the function.
- **`@request.auth.id != ""`:** Authenticated Only. Requires a valid user token.
- **Custom Rule Expressions:** Evaluated against `@request.auth.*` claims using Ruxbase's rule evaluation engine.
