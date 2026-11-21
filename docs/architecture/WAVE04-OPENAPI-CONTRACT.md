# Wave 04: Unified Data API & Developer Experience — OpenAPI 3.1 & Machine-Readable Contract Coverage

```
Document ID:     RUXBASE-WAVE04-OPENAPI-CONTRACT
Version:         1.0.0
Milestone:       W04-03 (Tested OpenAPI and machine-readable contract coverage; no undocumented privilege exposure)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Scope

Milestone W04-03 governs the machine-readable API contract architecture for Ruxbase.
Ruxbase provides a fully validated OpenAPI 3.1.0 specification (`docs/contracts/openapi.json`) alongside deterministic route coverage validation (`docs/contracts/route-coverage.json` and `docs/contracts/reference.json`).

The contract architecture enforces:
1. **OpenAPI 3.1.0 Conformance**: All schemas, routes, parameter descriptors, and payload envelopes adhere to the official OpenAPI 3.1.0 specification standard.
2. **Deterministic Route Coverage**: 100% of built-in HTTP operations are registered, categorized, and covered either via scoped OpenAPI endpoints or explicit protocol guides (e.g. MCP SSE/Stdio). Zero routes are left undocumented.
3. **Zero Undocumented Privilege Exposure**: Every administrative or protected endpoint explicitly declares its required security scheme (`SuperuserToken`, `UserToken`, `ServiceToken`, `FileToken`). Non-authenticated or regular user requests to administrative paths must fail closed with standardized HTTP 401/403 responses.
4. **Normalized Error Schemas**: All documented error responses reference the standardized `ApiErrorEnvelope` schema containing status, message, error data, and optional machine-readable error codes.

---

## 2. Security Scheme Taxonomy

Ruxbase OpenAPI defines four canonical security schemes:

| Scheme Key | Type | In | Description |
|---|---|---|---|
| `SuperuserToken` | `apiKey` | `header` (`Authorization`) | Root administrator token for platform configuration, table exposure, schema migrations, backups, and logs. |
| `RecordToken` | `apiKey` | `header` (`Authorization`) | Authenticated record principal token (human user or service account) for collection record access. |
| `FileToken` | `apiKey` | `query` (`token`) | Short-lived query token for protected file downloads. |
| `SuperuserFileToken` | `apiKey` | `query` (`token`) | Short-lived query token for administrative backup downloads. |

### Enforced Rule on Administrative Paths

Administrative paths (such as `/api/collections`, `/api/settings`, `/api/backups`, `/api/logs`, `/api/crons`, `/api/sql`, `/api/ruxbase/tables`) **MUST** strictly require `SuperuserToken`:
```json
"security": [
  {
    "SuperuserToken": []
  }
]
```
Any route missing an explicit security declaration or permitting unauthenticated access to system state is rejected as an undocumented privilege exposure defect.

---

## 3. Machine-Readable Verification & Drift Prevention

Automated testing in `tests/e2e/openapi_contract_test.go` and static validation in `website/scripts/export-references.mjs` guarantee that:
- Every operation has a unique `operationId`.
- Every operation declares a successful `2xx` response and appropriate `4xx`/`5xx` error responses.
- Administrative routes fail closed with HTTP 401 Unauthorized or HTTP 403 Forbidden when requested by anonymous or non-superuser callers.
- All file hashes in `docs/contracts/reference.json` match current repository Git blob SHAs.
