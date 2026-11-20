# Wave 00 — Architecture assessment and execution gates

## Evidence and scope

Reviewed baseline: `048a71ab53c62746ac27039e3f9a413f8f61e575`
(tree `2f0e8f8f0ed2dcf97fc2de1c2c84ebebed933d7c`).
PocketBase is the sole ongoing upstream. The official v0.40.6 commit is
`9d281ccaba65f2686eb7b0b614ccda72d1116a1b`.

This is a bounded source/documentation assessment, not independent security
certification or a complete line-by-line audit. No local test suite or
benchmark was executed for this report. PR #3 candidate CI #44637758571 passed
all ten jobs; main CI #44640144469 must be checked independently.

Repository foundation remains OPEN in [issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4).
Do not expand authority or rewrite main to circumvent GitHub workflow
permissions. The source provenance and owner review remain unresolved.
The official release listing at review time has no newer release on the
v0.40 series than v0.40.6. Rehearse against the next actual applicable release
when available; do not label a synthetic rehearsal a completed upgrade.

## In-place architecture

Preserve the existing core, API, migrations, plugins, tools, UI, tests and
importer. Keep Go modular monolith plus external PostgreSQL as the default
deployment. Add stable contracts incrementally and avoid moving inherited
packages merely to create a new directory structure. Optional workers and
control plane must not be required for a small project.

| Area | Baseline source | Disposition |
| --- | --- | --- |
| Data connection | core/db_connect.go, go.mod | Retain pgx/dbx foundation |
| Transaction-local policy context | core/rls_strict.go | Retain concept, review full enforcement |
| API rule selection | apis/record_crud_rls.go | Extend through unified policy contract |
| Schema DDL | core/collection_ddl.go | Retain bounded lock handling, add migration lifecycle |
| Database retry | core/db_retry.go | Verify transaction versus statement retry semantics |
| Replication | core/cluster/replication.go | Retain CDC, separate durable event guarantees |
| Compatibility | docs/COMPATIBILITY.md, tests/golden | Preserve declared contracts |
| MCP | docs/MCP.md | Retain opt-in mode, review identity and scope |
| Operations | docs/OPERATIONS.md | Retain health metrics and connection budget |

## Verified gaps and acceptance criteria

These are documented limitations or architecture risks, not demonstrated
exploitable vulnerabilities.

| ID | Priority | Evidence at baseline | Required deliverable and gate |
| --- | --- | --- | --- |
| RB-SEC-01 | P0 | docs/RLS.md:138–162; core/rls_strict.go:25–57 | Separate provisioning/migration authority from runtime. Verify effective runtime authority and fail closed on invalid configuration. |
| RB-SEC-02 | P0 | docs/RLS.md:138–162; apis/record_crud_rls.go:12–23 | Explicit policy coverage matrix across reads, writes, expansions, hooks, realtime, storage, MCP and direct SQL. Define unsupported-rule behavior and prove authorization consistency with disposable DB tests. |
| RB-SEC-03 | P0 | docs/CLUSTER.md:64–79,101–112 | Define authorization-change consistency and revocation guarantees across nodes; verify stale state cannot authorize prohibited operations. |
| RB-DB-01 | P1 | docs/COMPATIBILITY.md:38–50 | Add SQL-first introspection contracts, exact numeric types, native relational constraints and explicit API exposure without breaking collection contracts. |
| RB-DB-02 | P1 | core/collection_ddl.go; docs/OPERATIONS.md:82–94 | Versioned migration plans, bounded locks, expand/backfill/validate/contract, drift checks and recovery evidence. |
| RB-DB-03 | P1 | core/db_retry.go; docs/OPERATIONS.md:35–79 | Establish retry boundaries and connection lifetime invariants; test cancellation and pool reuse. |
| RB-EVT-01 | P1 | docs/CLUSTER.md:43–62; core/cluster/replication.go:56 | Separate ephemeral coordination from durable jobs/events; prove committed operations survive supported crash cases. |
| RB-EVT-02 | P1 | docs/REALTIME.md:48–94 | Define delivery, ordering, replay windows and resync guarantees; test failover and reconnect without claiming exactly-once external delivery. |
| RB-OPS-01 | P1 | docs/COMPATIBILITY.md:149–165; docs/CLUSTER.md:81–85 | Verified restore with object reconciliation, PITR integration, version compatibility and controlled upgrades. |
| RB-AI-01 | P2 | docs/MCP.md:1–55 | Evaluate scoped agent identities, explicit tool exposure, approvals and auditing within the unified policy model. |

## Dependency-ordered execution

1. **Foundation:** close issue #4, record source provenance, protect main,
   baseline all jobs and establish reproducible performance measurements.
2. **Security/kernel:** RB-SEC-01/02/03, identity context, transaction boundary,
   explicit compatibility policy. No tenant/GraphQL/runtime feature expansion
   until the relevant security contracts are proven.
3. **PostgreSQL:** RB-DB-01/02/03, additive SQL-first support and safe migrations.
4. **Identity and tenancy:** session lifecycle, tenant isolation, scoped service
   accounts; preserve multiple auth collections where compatible.
5. **API and realtime:** typed REST, opt-in GraphQL/RPC, resumable streams
   (RB-EVT-02). Enforce common grants and policies.
6. **Jobs and runtime:** durable queue/outbox (RB-EVT-01), idempotency, workers,
   isolated functions and storage consistency.
7. **Operations/control plane:** RB-OPS-01, telemetry, independent projects,
   explicit privilege boundaries and optional deployment profiles.
8. **AI and ecosystem:** RB-AI-01, plugins and SDKs; build on certified contracts.

Each increment requires source changes, executable tests on PostgreSQL 17/18,
compatibility checks, documented limitations and rollback or recovery evidence.
Avoid a documentation-only certification. Preserve the baseline until the
owner accepts evidence.

## Benchmark admission

Use identical hardware or enforced CPU/RAM limits, dataset, access policies,
TLS and enabled features against pinned Supabase and Ruxbase versions. Record
idle and loaded RSS, CPU, throughput, P50/P95/P99 latency, error rates, WAL and
disk usage across CRUD, authentication, realtime fanout and recovery. Publish
reproducible configuration and raw results. No claim of superiority before
measurements. No performance optimization that disables authorization or
durability to improve the score.

## Decision

Foundation: OPEN. Read-only architecture and test design: GO.
New production authority or release: NO-GO.
No independent certification is claimed.
