---
title: Configuration reference
description: Selected PostgreSQL and runtime environment settings and defaults.
---

**Development documentation.** Scoped reference, not a certified production release.

Explicit programmatic configuration may override environment defaults. Never commit real connection strings or encryption keys.

| Name | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | required outside embedded development mode | Main PostgreSQL connection string; keep credentials private. |
| `RUXBASE_LOGS_URL` | main database | Optional separate PostgreSQL connection string for logs. |
| `RUXBASE_DB_MAX_CONNS` | 20 | Main pool connection limit when not set by Go configuration. |
| `RUXBASE_DB_CONN_MAX_LIFETIME` | 30m | Connection lifetime as a Go duration. |
| `RUXBASE_CLUSTER` | false | Enables cluster mode; see clustering constraints. |
| `RUXBASE_REALTIME_SOURCE` | hooks | Realtime source: hooks or replication; see requirements and fallback behavior. |
| `RUXBASE_RLS_STRICT` | false | Additional RLS enforcement for supported non-superuser list and view queries; see documented limitations. |

See [operations](/ruxbase/operations/overview/), [realtime](/ruxbase/guides/realtime/) and [authorization](/ruxbase/authorization/row-level-security/) for limitations.
