---
title: Health API reference
description: Read the documented liveness and readiness response contracts.
---

**Development documentation.** Scoped reference, not a certified production release.

The [OpenAPI 3.1 JSON contract](/ruxbase/openapi.json) covers **one GET operation** on the health endpoint. It is not a complete platform API specification. Authenticated administrative diagnostics are outside the current schema examples.

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
