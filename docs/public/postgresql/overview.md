---
title: "PostgreSQL-native developer setup"
description: "Choose PostgreSQL capabilities, extensions and connection boundaries for Ruxbase."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Database choice
Use PostgreSQL 17 or later as documented. Embedded development mode is disposable, not a durable deployment database. External database access is configured via DATABASE_URL on the server, never the browser. Provision pgcrypto and pgvector when required by the application. pgvector is absent from the embedded build.

## Native feature selection
| Need | Guide |
| --- | --- |
| Direct SQL authorization | [RLS](/ruxbase/authorization/row-level-security/) |
| Search by words and relevance | [full-text search](/ruxbase/data/full-text-search/) |
| Similarity over embeddings | [vector search](/ruxbase/data/vector-search/) |
| Events from committed direct DB changes | [logical replication source](/ruxbase/guides/realtime/) |
| Multiple server instances | [clustering](/ruxbase/operations/clustering/) |

Do not confuse the PostgreSQL connection credentials with the API user token. The app's database role and direct SQL roles have distinct capabilities. Use separate least-privilege credentials and only trusted administrators for DDL/extension setup. Do not expose the privileged SQL console to frontend users.

## Evidence
[Source: docs/COMPATIBILITY.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/COMPATIBILITY.md), [Source: docs/OPERATIONS.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/OPERATIONS.md), [Source: docs/DEV.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/DEV.md). Each linked feature guide contains its own supported scope.
