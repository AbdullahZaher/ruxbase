---
title: API reference
description: Explore health, record CRUD and vector search contracts.
---

**Development documentation.** Scoped reference, not a certified production release.

The [OpenAPI 3.1 JSON contract](/ruxbase/openapi.json) covers **seven operations**: health, record CRUD and vector search. It is not a complete platform API specification. Authenticated administrative diagnostics are outside the current schema examples.

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

See [operations](/ruxbase/operations/overview/). Other REST operations, authentication flows, SSE, MCP and custom routes remain outside this OpenAPI increment. Collection schemas and permissions are application-specific.

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

## Scope

Authentication, file downloads/uploads, administrative APIs, realtime, batch, MCP and custom routes are not fully represented. The contract describes selected success responses; error details and collection-specific schemas remain outside this increment.
