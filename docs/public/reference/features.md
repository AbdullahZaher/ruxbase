---
title: Feature availability
description: Scoped index of development capabilities and their documented constraints.
---

**Development documentation.** Scoped reference, not a certified production release.

All entries describe documented development capabilities, not stable or production-certified features. Absence does not imply implementation or lack of support. See [machine-readable manifest](/ruxbase/feature-manifest.json).

| Feature | Status | Scope |
| --- | --- | --- |
| [embedded-development](/ruxbase/getting-started/development/) | documented-development | Embedded PostgreSQL for development; review platform limitations. |
| [pocketbase-compatibility](/ruxbase/migration/compatibility/) | documented-development | Compatibility is bounded by documented differences. |
| [health-readiness](/ruxbase/operations/overview/) | documented-development | Separate liveness and readiness semantics. |
| [clustering](/ruxbase/operations/clustering/) | documented-development | Multi-instance operation with documented constraints. |
| [row-level-security](/ruxbase/authorization/row-level-security/) | documented-development | Only supported rules and scopes; not a universal isolation guarantee. |
| [realtime](/ruxbase/guides/realtime/) | documented-development | Hooks or logical replication with documented limitations. |
| [full-text-search](/ruxbase/data/full-text-search/) | documented-development | PostgreSQL full-text search. |
| [vector-search](/ruxbase/data/vector-search/) | documented-development | Requires supported pgvector setup. |
| [mcp](/ruxbase/ai/mcp/) | documented-development | Optional integration; review its access rules. |
| [pocketbase-import](/ruxbase/migration/from-pocketbase/) | documented-development | Follow supported import scope and limitations. |
