# NOTICE

**ruxbase** is a fork of [PocketBase](https://github.com/pocketbase/pocketbase)
v0.40.6 (commit `9d281cc`), created and maintained by Gani Georgiev and its
contributors under the MIT License. The original copyright and license are kept
unchanged in [`LICENSE.md`](LICENSE.md).

ruxbase is an independent project. It is not affiliated with, endorsed by, or
supported by PocketBase or its author. Please do not report ruxbase issues to
the PocketBase project.

## What changes in this fork

- PostgreSQL 17+ is the only supported database. SQLite is removed from the main
  binary; it is used only by the separate `ruxbase-import` tool to read existing
  PocketBase data.
- Horizontal scaling (multiple instances sharing one database), API rules
  compiled to Postgres row level security, realtime through logical replication,
  an embedded Postgres for development, a vector field type (pgvector) and a
  built-in MCP server.
- The binary is named `ruxbase`, the Go module is
  `github.com/AbdullahZaher/ruxbase` and the default data directory is
  `ruxbase_data`.

The REST API and the official PocketBase SDKs are kept compatible.

PocketBase is the sole ongoing upstream for Ruxbase. The reference version is
v0.40.6 (commit `9d281ccaba65f2686eb7b0b614ccda72d1116a1b`).
The shared Git ancestry has not yet been established in the published branch;
see [UPSTREAM.md](UPSTREAM.md) for the verification and integration policy.
All applicable inherited copyright and license notices must be retained.
