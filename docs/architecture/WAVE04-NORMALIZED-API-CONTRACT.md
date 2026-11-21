# Wave 04: Unified Data API & Developer Experience — Normalized Authorization & Error Contract

```
Document ID:     RUXBASE-WAVE04-NORMALIZED-API-CONTRACT
Version:         1.0.0
Milestone:       W04-01 (One Normalized Authorization and Error Contract for Collections-First and SQL-First REST)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Objective

Milestone W04-01 unifies the authorization, execution context, and error response semantics across both **Collections-first** (`/api/collections/...`) and **SQL-first** (`/api/sql/...`, `/api/rpc/...`) REST interfaces.

Developers interacting with Ruxbase receive consistent HTTP status codes, structured error envelopes, and uniform credential evaluation regardless of whether they access data through collection records or direct SQL routines.

---

## 2. Standardized Error Envelope

All API errors return standard `application/json` with HTTP status headers and the following payload format:

```json
{
  "status": 403,
  "code": "tenant_access_denied",
  "message": "Access to tenant denied",
  "data": {}
}
```

### 2.1 Standard Error Code Catalog & PostgreSQL Mapping

| PostgreSQL SQLSTATE / Internal Condition | HTTP Status | Standard Error Code | Client Message |
|---|---|---|---|
| `42501` (insufficient_privilege / RLS violation) | `403 Forbidden` | `rls_policy_violation` | Operation denied by row-level security policy |
| `23505` (unique_violation) | `400 Bad Request` | `unique_constraint_violation` | Unique constraint violation |
| `23503` (foreign_key_violation) | `400 Bad Request` | `foreign_key_violation` | Foreign key constraint violation |
| `23514` (check_violation) | `400 Bad Request` | `check_constraint_violation` | Check constraint violation |
| `42P01` (undefined_table) | `404 Not Found` | `resource_not_found` | Requested table or collection does not exist |
| `ErrTenantAccessDenied` | `403 Forbidden` | `tenant_access_denied` | Access to tenant denied |
| `ErrTenantSuspended` | `403 Forbidden` | `tenant_suspended` | Tenant is suspended |
| `ErrServiceAccountExpired` | `401 Unauthorized` | `service_account_expired` | Service account API key has expired |
| `ErrServiceAccountRevoked` | `401 Unauthorized` | `service_account_revoked` | Service account has been revoked |
| Missing / Invalid Token | `401 Unauthorized` | `unauthorized` | The request requires valid authentication |

---

## 3. Normalized Principal Resolution Contract

1. **Bearer Token Extraction**:
   Inspect `Authorization` header (`Bearer <token>`).
   - If token begins with `rx_svc_`: route to `AuthenticateServiceAccount`.
   - If token contains JWT structure (`header.payload.signature`): route to `VerifyTenantAuthToken` or standard PB auth record verification.
   - If `X-Token` or `x-api-key` header present: parse as secondary authentication channel.
2. **Context Binding**:
   Inject verified `Principal` into request context via `RequestEventWithPrincipal(ctx, principal)`.
3. **Execution Sandbox**:
   Execute data operations inside `RunInScopedTransaction` with `TenantID = principal.TenantId()` and PostgreSQL role matching authentication level (`ruxbase_authenticated`, `ruxbase_anon`, or `ruxbase_api`).
