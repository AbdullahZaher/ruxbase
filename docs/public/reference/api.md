---
title: API reference
description: Explore scoped data, authentication, file and administrative API contracts.
---

**Development documentation.** Scoped reference, not a certified production release.

The [OpenAPI 3.1 JSON contract](/ruxbase/openapi.json) covers **57 operations**: all direct built-in HTTP registrations outside the MCP transport, with scoped schemas. It is not a complete platform API specification. Authenticated administrative diagnostics are outside the current schema examples.

[Open the Scalar API viewer](/ruxbase/api-viewer/) to explore the same contract. The viewer is for documentation only and does not provide a live API request console. Static text and Markdown remain the authoritative readable references.

## GET /api/health

Without the `ready` query parameter, the handler reports liveness (HTTP 200 and an empty guest `data` object). With the parameter present, it checks database and cluster readiness. HTTP 503 signals failed readiness. Use readiness rather than liveness for routing decisions.

```json
{
  "message": "API is healthy.",
  "code": 200,
  "data": {}
}
```

```json
{
  "message": "API is not ready.",
  "code": 503,
  "data": {
    "ready": {
      "database": true,
      "cluster": false
    }
  }
}
```

See [operations](/ruxbase/operations/overview/). Other REST operations, including SSE, MCP and custom routes, remain outside this OpenAPI increment. Collection schemas and permissions are application-specific.

## Record operations

| Method | Path | Success |
| --- | --- | --- |
| GET | `/api/collections/{collection}/records` | 200 |
| POST | `/api/collections/{collection}/records` | 200 |
| GET | `/api/collections/{collection}/records/{id}` | 200 |
| PATCH | `/api/collections/{collection}/records/{id}` | 200 |
| DELETE | `/api/collections/{collection}/records/{id}` | 204 |
| POST | `/api/collections/{collection}/vector-search` | 200 |

Collection API rules control access to records. Guest access is conditional on the configured rule. Record fields and writable fields vary by collection, field selection and permissions; this contract intentionally does not prescribe application-specific schemas. Multipart uploads are not specified here.

List operations accept page, perPage, sort, filter, skipTotal, expand and fields. When totals are skipped, counts may be -1. Vector search requires field and vector, with optional k and filter. See [vector search](/ruxbase/data/vector-search/) and [authorization](/ruxbase/authorization/row-level-security/) for the supported behavior and limitations.

## Authentication and account management

Authentication is configured per auth collection. Password and OTP authentication may require a further MFA step. Protect authentication tokens, passwords and recovery tokens; use HTTPS outside local development. A successful account-recovery or verification request is not proof of account existence or delivery.

| Method | Path | Success |
| --- | --- | --- |
| GET | `/api/collections/{collection}/auth-methods` | 200 |
| POST | `/api/collections/{collection}/auth-refresh` | 200 |
| POST | `/api/collections/{collection}/auth-with-password` | 200 |
| POST | `/api/collections/{collection}/request-otp` | 200 |
| POST | `/api/collections/{collection}/auth-with-otp` | 200 |
| POST | `/api/collections/{collection}/request-password-reset` | 204 |
| POST | `/api/collections/{collection}/confirm-password-reset` | 204 |
| POST | `/api/collections/{collection}/request-verification` | 204 |
| POST | `/api/collections/{collection}/confirm-verification` | 204 |
| POST | `/api/collections/{collection}/request-email-change` | 204 |
| POST | `/api/collections/{collection}/confirm-email-change` | 204 |
| POST | `/api/files/token` | 200 |

The auth-refresh, request-email-change and file-token operations require authenticated context. Review each operation's JSON request schema and authentication requirements in the OpenAPI contract. Account-specific rules and application-defined fields remain authoritative.

## Collections, files and administration

Collection metadata, settings and backup metadata operations in the following table require superuser authentication. Administrative responses may contain sensitive configuration. Do not embed administrative credentials in browser applications or expose them through documentation. Review and back up data before making destructive schema changes. The file read operation follows the protection configured on the file field.

| Method | Path | Success |
| --- | --- | --- |
| GET | `/api/collections` | 200 |
| POST | `/api/collections` | 200 |
| GET | `/api/collections/{collection}` | 200 |
| PATCH | `/api/collections/{collection}` | 200 |
| DELETE | `/api/collections/{collection}` | 204 |
| GET | `/api/collections/{collection}/rls` | 200 |
| GET | `/api/collections/meta/scaffolds` | 200 |
| GET | `/api/collections/meta/oauth2-providers` | 200 |
| GET | `/api/settings` | 200 |
| PATCH | `/api/settings` | 200 |
| GET | `/api/backups` | 200 |
| GET | `/api/files/{collection}/{recordId}/{filename}` | 200 |

The collection and settings schemas are intentionally open objects, rather than exhaustive field-by-field contracts. The RLS inspection response is diagnostic; review [row-level security](/ruxbase/authorization/row-level-security/) for limits. Backup metadata does not establish restore readiness; see [operations](/ruxbase/operations/overview/). The OAuth2 provider metadata route is experimental.

## Operational administration

The following administrative operations require the appropriate superuser authorization. Review data impact, retention and recovery before authorized use. An asynchronous acknowledgement is not completion evidence.

| Method | Path | Success |
| --- | --- | --- |
| POST | `/api/backups` | 204 |
| PUT | `/api/collections/import` | 204 |
| DELETE | `/api/collections/{collection}/truncate` | 204 |
| GET | `/api/logs` | 200 |
| DELETE | `/api/logs` | 204 |
| GET | `/api/logs/{id}` | 200 |
| GET | `/api/logs/stats` | 200 |
| GET | `/api/crons` | 200 |
| POST | `/api/crons/{id}` | 204 |
| POST | `/api/backups/upload` | 204 |
| GET | `/api/backups/{key}` | 200 |
| DELETE | `/api/backups/{key}` | 204 |
| POST | `/api/backups/{key}/restore` | 204 |

## Additional HTTP operations

Follow the appropriate SDK and administrator documentation. Sensitive credentials and privileged requests are not intended for public client applications. These are scoped operation contracts, not executable test recipes.

| Method | Path | Success |
| --- | --- | --- |
| POST | `/api/settings/test/s3` | 204 |
| POST | `/api/settings/test/email` | 204 |
| POST | `/api/settings/apple/generate-client-secret` | 200 |
| POST | `/api/collections/meta/dry-run-view` | 200 |
| GET | `/api/oauth2-redirect` | 200 |
| POST | `/api/oauth2-redirect` | 200 |
| POST | `/api/collections/{collection}/auth-with-oauth2` | 200 |
| POST | `/api/collections/{collection}/impersonate/{id}` | 200 |
| POST | `/api/batch` | 200 |
| GET | `/api/realtime` | 200 |
| POST | `/api/realtime` | 204 |
| POST | `/api/sql` | 200 |
| POST | `/api/rls/token` | 200 |

## Scope

All 57 direct non-MCP registrations now have scoped OpenAPI operations. The remaining three registrations are the GET, POST and DELETE methods of the MCP transport, documented in the [MCP guide](/ruxbase/ai/mcp/), rather than represented as ordinary REST operations. See [route coverage](/ruxbase/api-coverage.json). Complete schema details, record multipart bodies, protocol negotiation, error behavior and runtime verification remain separate requirements. Dynamic custom routes and internal batch subrequests are outside the inventory.
