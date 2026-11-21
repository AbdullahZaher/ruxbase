# Wave 04: Unified Data API & Developer Experience — CLI Workflows & Safe Project Configuration

```
Document ID:     RUXBASE-WAVE04-CLI-WORKFLOWS
Version:         1.0.0
Milestone:       W04-04 (CLI for init, dev, migration, check, deploy guidance and safe project configuration)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Commands Architecture

Milestone W04-04 establishes a unified, operator-safe CLI suite for the entire application lifecycle: project initialization, local development, database preflight diagnostics, schema migrations, backup restoration, and production deployment guidance.

All commands enforce strict safety guarantees:
- Non-destructive execution on read-only commands (`check`, `deploy`).
- Offline operation without database dependencies for scaffolding and guidance commands (`init`, `deploy`, `dev`).
- Machine-readable `--json` output format on all diagnostic and operational commands.
- Zero embedding or transmission of credentials in command flags or output logs.

---

## 2. Command Suite Reference

| Command | Category | Database Required | Description |
|---|---|---|---|
| `ruxbase init [dir]` | Project Scaffolding | No | Initializes workspace, creates `ruxbase_data/`, writes safe `.env.example`, and presents frontend starter commands. |
| `ruxbase dev` | Local Development | Optional | Runs embedded dev PostgreSQL mode or connects to local dev instance with detailed query/HTTP logging. |
| `ruxbase check [--json]` | Preflight Diagnostics | Yes | Verifies live PostgreSQL connection, checks PG17+ compatibility, pgvector extension availability, migration state, and data dir without modifying state. |
| `ruxbase deploy [--json]` | Production Guidance | No | Outputs production infrastructure requirements, security preflight checklist, least-privilege role boundaries, and recommended environment flags. |
| `ruxbase migrate [up\|down]` | Schema Lifecycle | Yes | Executes deterministic database migrations with checksum drift verification. |
| `ruxbase serve` | Serving Runtime | Yes | Starts HTTP/REST/SSE server with connection pool limits, graceful signal handling, and optional cluster forwarding. |
| `ruxbase superuser` | Administration | Yes | Creates, updates, upserts, or deletes administrative superuser credentials. |
| `ruxbase restore` | Disaster Recovery | Yes | Restores backup archive to database. |
| `create-ruxbase-app` | Starter Generator | No | Scaffolds verified React or Next.js starter templates with pre-configured SDK clients and TypeScript contracts. |
| `ruxbase-doctor` | Frontend Diagnostic | No | Probes local HTTP backend health and collection responsiveness without mutating state. |
