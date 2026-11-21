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
The reference is a verified Git ancestor of canonical `main` through
[Ruxbase PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33).
See [UPSTREAM.md](UPSTREAM.md) for the verification and selective-port policy.
All applicable inherited copyright and license notices must be retained.

## Bundled component notices

- The server embeds the Remix Icon 4.6.0 font. Its Apache 2.0 license is in
  `THIRD-PARTY-LICENSES/remixicon/4.6.0/LICENSE`, copied from the npm package
  whose font matches the embedded file. [Remix Icon package](https://www.npmjs.com/package/remixicon/v/4.6.0)
- The server embeds TinyMCE 6.8.6 editor assets. Its MIT license and upstream
  third-party notices are in `THIRD-PARTY-LICENSES/tinymce/6.8.6/`, copied from
  the [TinyMCE npm package](https://www.npmjs.com/package/tinymce/v/6.8.6).
  The inherited TinyMCE distribution includes modified assets.
- The standalone importer links `modernc.org/sqlite` v1.60.1. Its BSD 3-Clause
  license and upstream third-party notices are in
  `THIRD-PARTY-LICENSES/modernc-sqlite/v1.60.1/`, copied from the pinned Go
  module. [SQLite Go module](https://pkg.go.dev/modernc.org/sqlite@v1.60.1)

The server and importer archives include their respective notices. The runtime
container includes both binaries and both sets of notices.
