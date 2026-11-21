---
title: Ruxbase documentation
description: Developer documentation for the PostgreSQL-native self-hosted backend platform.
---

**Development documentation.** This preview follows unreleased development
and does not represent a certified production release.

Ruxbase is an independent open-source backend platform built with Go and
PostgreSQL. It uses PocketBase as the sole ongoing upstream for inherited
application code. PostgreSQL-specific functionality is maintained in Ruxbase.

Powerful by design. Lightweight by default.

The development documentation is published from reviewed GitHub Pages snapshots.
For checkout-specific installation guidance and compatibility limits, consult the
[project README](https://github.com/AbdullahZaher/ruxbase/blob/main/README.md)
and the published [version identity](https://abdullahzaher.github.io/ruxbase/version.json).
Review [compatibility](/ruxbase/migration/compatibility/)
and [operational guidance](/ruxbase/operations/overview/)
before choosing a deployment configuration.

[Source code](https://github.com/AbdullahZaher/ruxbase) ·
[Releases](https://github.com/AbdullahZaher/ruxbase/releases)

[Start with the quickstart](/ruxbase/getting-started/quickstart/).

[Using Ruxbase with AI](/ruxbase/ai/using-ruxbase-with-ai/) — direct Markdown, AI context and version identity.

## Build an application

- [Choose an integration](/ruxbase/integrations/overview/)
- [Set up the example app](/ruxbase/integrations/example-app/)
- [JavaScript SDK](/ruxbase/integrations/javascript/) and [REST](/ruxbase/integrations/rest/)
- [Server-side integration](/ruxbase/integrations/server-side/) and [mobile integration](/ruxbase/integrations/mobile/)

## Learn and extend
- [Collections](/ruxbase/concepts/collections/) · [Field types](/ruxbase/concepts/fields/) · [Records](/ruxbase/concepts/records/) · [Authentication](/ruxbase/concepts/authentication/)
- [API rules](/ruxbase/concepts/rules/) · [Relations](/ruxbase/concepts/relations/) · [Files](/ruxbase/concepts/files/)
- [Browser SPA](/ruxbase/integrations/react/) · [Server adapters](/ruxbase/integrations/nextjs/) · [Complete local tutorial](/ruxbase/tutorials/posts-app/)
- [Extend with Go](/ruxbase/extend-go/overview/) · [Extend with JavaScript](/ruxbase/extend-js/overview/) · [PostgreSQL native](/ruxbase/postgresql/overview/) · [Deploy](/ruxbase/operations/deployment/) · [Recover](/ruxbase/operations/recovery/)

## Explore the guides

- [Development mode](/ruxbase/getting-started/development/) — Run Ruxbase locally with embedded PostgreSQL.
- [PocketBase compatibility](/ruxbase/migration/compatibility/) — Review compatibility targets and documented differences.
- [Running multiple instances](/ruxbase/operations/clustering/) — Understand multi-instance coordination and operational constraints.
- [Row-level security](/ruxbase/authorization/row-level-security/) — Understand supported PostgreSQL RLS rules, roles and limitations.
- [Realtime events](/ruxbase/guides/realtime/) — Understand realtime event sources and deployment constraints.
- [Full-text search](/ruxbase/data/full-text-search/) — Use and understand PostgreSQL text search.
- [Vector search](/ruxbase/data/vector-search/) — Understand pgvector search and supported behavior.
- [MCP server](/ruxbase/ai/mcp/) — Understand the optional Model Context Protocol integration.
- [Operations](/ruxbase/operations/overview/) — Review health checks, database connectivity and operations.
- [Migrating from PocketBase](/ruxbase/migration/from-pocketbase/) — Review supported import and migration requirements.

## Reference

- [API reference](/ruxbase/reference/api/)
- [CLI](/ruxbase/reference/cli/)
- [Configuration](/ruxbase/reference/configuration/)
- [Feature availability](/ruxbase/reference/features/)
