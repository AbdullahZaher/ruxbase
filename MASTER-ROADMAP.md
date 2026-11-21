---
document_id: RUXBASE-MASTER-ROADMAP
version: 1.0
status: canonical-direction-proposed-for-merge
last_reviewed: 2026-11-21
owner: Ruxbase project owner
scope: waves-00-through-11
reference_competitor: supabase-self-hosted-only
progress_source: MASTER-CHECKLIST.md
agent_protocol: docs/roadmap/AI-EXECUTION-PROTOCOL.md
---

# Ruxbase — Master Engineering Roadmap (Wave 00–11)

> **Product direction:** Powerful by Design. Lightweight by Default.
>
> **Mission:** Deliver an open-source, PostgreSQL-native, self-hosted Backend-as-a-Service alternative to **Supabase Self-Hosted** with simpler deployment, strong security, reliable data services, and an extensible developer experience.

**This is the master *strategic and architectural* roadmap.** [MASTER-CHECKLIST.md](MASTER-CHECKLIST.md) records acceptance and actual progress. [AI execution protocol](docs/roadmap/AI-EXECUTION-PROTOCOL.md) defines how to resume work. The existing code, authoritative milestone specifications, issues, approved decisions, and passing tests determine what is *actually implemented*. A future milestone appearing in this roadmap is **not** an implementation claim or permission to deploy.

## 1. Scope, comparison and exclusions

- **Target comparison:** official **Supabase Self-Hosted**, deployed with documented Docker configuration on comparable hardware. Do not use Supabase Cloud commercial/managed offerings as the feature baseline.
- **No commercial SaaS competition mandate:** do not introduce billing, subscription plans, hosted organization administration, or a managed cloud service merely to match Supabase Cloud.
- **Product:** self-hosted, open source, MIT, independently maintained Go modular monolith; external PostgreSQL 17+ (17/18 test targets); embedded PostgreSQL for development only; SQLite driver only in the isolated PocketBase importer.
- **Sole upstream for inherited application code:** official `pocketbase/pocketbase`. No Popgres baseline, synchronization, or dependency. Use selective, reviewed upstream ports.
- **Two complementary data experiences:** PocketBase-compatible **Collections-first** and PostgreSQL-native **SQL-first**. These must share authorization, transaction and policy contracts; never create two inconsistent security engines.
- **Minimal footprint:** one Ruxbase application binary plus PostgreSQL for the core deployment. Workers, object storage and multi-project management are optional as workloads demand. No mandatory Redis, Kafka, Kubernetes or unrelated microservices.
- **Compatible, not identical:** retain documented PocketBase API/hook/SDK contracts; preserve intentional PostgreSQL differences; do not claim binary compatibility with Supabase APIs.
- **Production:** **NO-GO** until applicable release gates and explicit owner authorization. No milestone or CI success alone constitutes release certification.

### Explicitly out of the core parity gate

Supabase Cloud control plane, managed branching, commercial billing, managed PITR services, provider-operated infrastructure, and other managed-only capabilities. An **optional local/multi-project control plane**, offline synchronization and advanced AI features remain ambitious long-term differentiators, not prerequisites for the first secure self-hosted core release.

### Feature-parity interpretation

Target common *user journeys*, not identical component implementations or protocol names. For example, a Ruxbase Go implementation may replace several Supabase services if it meets the corresponding access, reliability and lifecycle guarantees. For gaps such as Broadcast/Presence, RPC, resumable uploads and isolated functions, explicitly decide whether they are required for the chosen release profile or are documented optional extensions.

## 2. Architectural commitments

1. Preserve existing `core/`, `apis/`, `migrations/`, `plugins/`, `tools/`, `ui/`, `tests/`, `cmd/` and upstream-friendly inherited layouts; add modules only when functional requirements justify them.
2. Treat PostgreSQL as source of truth for shared transactional state. Distinguish ephemeral notifications from durable events and jobs.
3. Apply least privilege and fail-closed admission. Offline provisioning and migrations must not lend elevated authority to the serving runtime.
4. Give REST, direct SQL, SDK, storage, realtime, search and MCP one testable authorization contract. An unsupported policy must never silently allow access.
5. Use explicit identity and transaction scope: no leaked session state, authorization context or partial writes across pooled connections.
6. Build native Postgres SQL-first features additively while preserving approved Collections-first semantics.
7. Make untrusted user code isolated from the primary process. Keep hooks that execute trusted application code separate from an untrusted function runtime.
8. Verify multi-node consistency; do not equate LISTEN/NOTIFY or UNLOGGED tables with durable delivery.
9. Make upgrades, schema changes, backup and restore first-class tested workflows, not operational afterthoughts.
10. Use version-pinned, evidence-backed CI, performance and security comparisons; do not claim superiority based on service count or a mock benchmark.

## 3. Canonical twelve-wave map

| Wave | Name | Primary deliverable | Gate focus | Core/advanced |
| --- | --- | --- | --- | --- |
| **00** | Foundation & Upstream Sustainability | Verifiable provenance, reviewed Git ancestry, branch governance, baseline, roadmap | Exact history, license, CI, inventory | Core |
| **01** | Platform Kernel & Security | Safe authority, kernel contracts, transactions, unified enforcement | Negative authorization/transaction tests | Core |
| **02** | PostgreSQL Native Engine | SQL-first alongside Collections-first, introspection, safe schema migration | Data correctness, drift, replay, rollback | Core |
| **03** | Enterprise IAM & Multi-Tenancy | Identity, sessions, service identities, roles, tenant boundaries | Cross-tenant negative tests, revocation | Core foundation / advanced SSO |
| **04** | Unified Data API & Developer Experience | REST/RPC, API exposure, CLI, OpenAPI, typed SDKs, optional GraphQL | Contract, API exposure, SDK/E2E | Core / optional GraphQL |
| **05** | Realtime & Offline Synchronization | Authenticated streaming, resume, presence/broadcast where required | Ordering, reconnect, authorization, loss model | Core realtime / advanced offline |
| **06** | Durable Jobs & Event Architecture | Transactional outbox, persistent queues, retries, DLQ, webhook delivery | Crash/replay/idempotency | Core reliability |
| **07** | Storage & Function Runtime | Authorized file/bucket operations, S3, multipart/resume, isolated functions | File isolation and untrusted-code containment | Core storage / optional runtime |
| **08** | Production Reliability & Operations | HA, verified restore/PITR, observability, secure upgrades | RPO/RTO, failure drills, upgrade compatibility | Core release |
| **09** | Control Plane & Ruxbase Studio | Comprehensive local studio; optional multi-project/self-hosted control plane | Admin authorization and deployment isolation | Core studio / advanced control plane |
| **10** | AI-Native Platform & Plugin Ecosystem | Scoped MCP, agent identities, governed tools, RAG/AI plugins | Least-privilege tools, approval/audit, plugin ABI | Optional advanced |
| **11** | Independent Certification & Release | Evidence-based self-hosted release decision and comparative benchmarks | Complete acceptance and independent audit | Release gate |

**Do not collapse Wave 02 into M02:** **M02 is an increment inside Wave 01**. Wave numbers and milestone numbers have different scopes.

### Cross-wave work

The [merged Developer Experience tooling and documentation](docs/architecture/developer-experience-status.md) provide React/Next.js starters, a repository-scoped scaffolding CLI, Compose examples and tested onboarding. This is an engineering-complete **documentation/tooling stream**, not an acceptance of Wave 04's full API, SDK, SQL-first or release gates. Keep Wave 04 scope and checklist IDs intact.

Security, tests, operations, performance measurements, user documentation, upgrade safety and developer experience are continuous requirements. Wave 08 and Wave 11 **assemble and certify** these capabilities; they do not defer essential risk prevention until late in development. P0 security defects block exposure of affected APIs regardless of scheduled wave.

## 4. Wave-by-wave engineering contracts

### Wave 00 — Foundation & Upstream Sustainability

**Objective:** establish a trustworthy source/history/test foundation before broad feature expansion.

**Required:** source and licensing inventory; official PocketBase reference and `UPSTREAM.lock.json`; correct attribution; reviewed selective-port process; auditable common Git ancestry; enforcement of required CI and independent PR review; test baseline PostgreSQL 17/18, dashboard, SDK and parity; architecture and gap inventory; reproducible resource baseline; reviewed master roadmap and release gates.

**Acceptance:** official upstream ancestor is reachable from the final `main` commit without losing Ruxbase history; branch rules genuinely enforce configured review/checks; correct tree/CI evidence exists. If no newer compatible PocketBase release exists, record that explicitly and do not fabricate a real-upgrade rehearsal. See [issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4).

### Wave 01 — Platform Kernel & Security

**Objective:** make the existing Go/PostgreSQL platform safe to extend and operate.

**M01 (historically accepted restricted engineering scope):** PostgreSQL role separation, reviewed runtime grant manifests, guarded pools, offline prepare, listener-free admission, dedicated WAL fixture and scheduler stabilization. [Issue #9](https://github.com/AbdullahZaher/ruxbase/issues/9) was closed for **Scope A only**; it does **not** certify general production serving.

**Remaining workstreams (proposed mapping; reconcile exact M03–M06 labels with accepted issue/spec before implementing):**
- **M02:** Stable platform contracts: module lifecycle, database and pool ownership, transaction and context boundaries, compatibility adapters, dependency direction.
- **M03:** Unified authorization and identity policy contract across APIs, SQL, search, storage, realtime and MCP; deny-by-default policy coverage.
- **M04:** Secure transaction context, safe role switching, cancellation, connection reuse, stale-context prevention and atomic side effects.
- **M05:** Native RLS enforcement for applicable reads/writes and supported policies, security-invoker views and privileged routine boundaries, with negative tests.
- **M06:** Cluster policy consistency, bounded revocation guarantees and security auditing; no stale cache may grant disallowed operations.
- **M07:** Integrated independent Wave 01 audit, security regression matrix, compatibility evidence and post-merge checks.

**Acceptance:** unsafe serving authority absent, cross-path access decisions consistent, mandatory negative tests pass using restricted PostgreSQL roles, documented unsupported rules fail closed, no cross-request identity leakage. Under the owner-approved Wave-Level Execution Policy, milestones M01–M07 are local engineering increments integrated, tested across the full wave matrix, independently certified by A11, and integrated as a complete wave into `main` via PR. Do not merge the Wave 01 integration branch into `main` before its *entire* wave is accepted.

### Wave 02 — PostgreSQL Native Engine

**Objective:** preserve Collections-first usability and add a genuine SQL-first experience through shared kernel/policy contracts.

**Deliver:** native tables and schemas, trustworthy introspection, explicit API exposure, foreign keys, composite keys where viable, `NUMERIC(p,s)`, constraints, generated columns, safe indexes and extensions, reviewed partitioning boundaries, typed SQL/RPC exposure, versioned schema plans, drift detection and expand/backfill/validate/contract upgrades. Support backwards-compatible collection metadata mappings.

**Acceptance:** create/import/introspect/migrate a real schema on PG17/18; reject invalid or unauthorized exposure; verify historical replay, partial failures, lock budgets and data-preserving rollback/recovery procedures. Never grant access to a table solely because it exists.

### Wave 03 — Enterprise IAM & Multi-Tenancy

**Objective:** robust user, session, service and tenant identities without weakening existing auth collections.

**Deliver:** explicit roles and permissions (RBAC/ABAC as justified), service accounts, session revocation, secure token claims, tenant organizations and memberships, tenant-aware relational ownership, bounded identity propagation; optional passkeys/SSO behind explicit requirements.

**Acceptance:** negative isolation tests across records, files, jobs, events, APIs, search, audit and AI metadata; fail closed for absent tenant context; stale-token and revocation coverage; scoped service identity with no privilege escalation.

### Wave 04 — Unified Data API & Developer Experience

**Objective:** one secure developer contract for collection and SQL-first workflows.

**Deliver:** stable REST and scoped RPC, authorized SQL schema exposure, API contract generation/OpenAPI, bulk operations with bounded transactions, SDK and CLI workflows, React/Next.js starter coverage and AI-readable documentation. Add GraphQL only when needed and behind the same authorization engine.

**Acceptance:** a fresh developer can install, create a schema, authenticate, perform CRUD/RPC and deploy an example using supported SDK/CLI; generated clients and documented errors match tests on PG17/18. Preserve official PocketBase SDK compatibility where contractually supported.

### Wave 05 — Realtime & Offline Synchronization

**Objective:** reliable, authorization-aware live updates across the supported deployment profiles.

**Deliver:** preserve existing SSE; close documented WAL/CDC and cluster gaps; define ordering, subscriber identity, reconnect and replay/resync windows; add WebSocket Broadcast/Presence if required for self-hosted feature coverage. Optional advanced offline local cache, cursor sync and conflict resolution must have separately tested semantics.

**Acceptance:** multi-node reconnect, permission revocation, failover, backpressure and event-loss simulations pass; never claim exactly-once delivery without evidence; keep clear limits for missed events and recovery.

### Wave 06 — Durable Jobs & Event Architecture

**Objective:** separate ephemeral scheduling from durable business work.

**Deliver:** PostgreSQL-backed durable queue/outbox, transactional enqueue, leasing, retries, exponential backoff, dead-letter handling, job idempotency, worker recovery, webhook signing/delivery/audit and optional workflows. Keep external queue infrastructure optional.

**Acceptance:** no loss of committed enqueue under tested crash modes; no duplicate external effect under the declared idempotency model; crash/lease/replay/poison-message tests; failed jobs inspectable and retryable.

### Wave 07 — Storage & Function Runtime

**Objective:** consistent file authorization and safely isolated compute extensions.

**Deliver:** bucket-level policies, object access ownership, secure local/S3 adapters, upload and overwrite semantics, multipart/resumable upload where required, quotas, lifecycle cleanup, verified file/DB backup coordination; optional sandboxed WASM/worker function runtime, secrets and deployment lifecycle.

**Acceptance:** cross-tenant/object access denied; upload cancellation/partial write tested; functions cannot access privileged runtime credentials or escape the approved sandbox; trusted Go/JS hooks are not mislabeled as untrusted execution isolation.

### Wave 08 — Production Reliability & Operations

**Objective:** harden and demonstrate operational correctness for a self-hosted release.

**Deliver:** tested provisioning and safe upgrades; complete PostgreSQL and object-storage restore; optional PITR/HA profile; backup integrity, operational rollback, monitoring/metrics/logs/tracing, capacity and connection budgeting, incident runbooks and recovery rehearsal.

**Acceptance:** clean install, upgrade, failure injection and full restore are independently reproducible; the claimed RPO/RTO applies only to the exact certified HA profile. Preserve simple one-node operation.

### Wave 09 — Control Plane & Ruxbase Studio

**Objective:** excellent single-project admin experience first; optional self-hosted multi-project operations second.

**Deliver:** secure dashboard for data, SQL, auth, policy diagnostics, storage, jobs and observability; optional independent control plane for multiple projects, environments, deployment and resource accounting. Never make the control plane mandatory for a single-project install.

**Acceptance:** management endpoints are independently authorized; admin actions audited; multiple projects isolated at the intended DB/process boundary; no privileged control plane credential leaks into application paths.

### Wave 10 — AI-Native Platform & Plugin Ecosystem

**Objective:** AI-friendly administration without creating a privileged bypass.

**Deliver:** extend opt-in MCP with scoped tools, agent identity, approvals and audit; plugin version/lifecycle contracts, knowledge/vector integrations, optional AI gateway and workflows. Documentation remains indexable and AI-readable.

**Acceptance:** prompt/tool injection, secret exposure, tenant cross-access and approval-bypass negative tests; extension compatibility and safe upgrade guarantees. AI capability is optional.

### Wave 11 — Independent Certification & Release

**Objective:** certify the exact, reproducible self-hosted release candidate.

**Deliver:** independent architecture/security/DB audit, PostgreSQL 17/18 and OS matrices, production deployment rehearsal, migration/restore/HA drills, performance/load/soak results, upstream compatibility register, licensing/supply chain verification and release decision.

**Acceptance:** no open P0 security defects, all declared supported user journeys pass, approved residual risks have owners, post-merge checks pass, provenance and release artifacts are signed/verified where required, and production has explicit owner approval.

## 5. Self-hosted deployment profiles and resource targets

| Profile | Intended configuration | Target (unverified until measured) | Guarantees |
| --- | --- | --- | --- |
| **Lite** | One Ruxbase binary + PostgreSQL + local files | **2 GB RAM** total stack goal | Same fundamental authorization and transaction rules; dev-embedded PostgreSQL is **not** production |
| **Standard** | Ruxbase + PostgreSQL + S3-capable storage + optional workers/metrics | **4 GB RAM** total stack goal | Durable operation where explicitly enabled/certified |
| **Enterprise / HA** | Multiple Ruxbase nodes + qualified PostgreSQL HA + optional control plane/workers | Workload-dependent | Availability, RPO, RTO are profile- and infrastructure-specific, never a binary-only promise |

All profiles must use the same core security and data semantics. No design goal is a benchmark result or a deployment recommendation without tests.

## 6. Competitive acceptance against Supabase Self-Hosted

Compare pinned **official self-hosted releases**, with equal CPU/RAM limits, datasets, TLS, query shape, authentication, security policies and enabled services. Publish the complete Compose/configuration manifests, hardware, raw samples and exact commits.

| Goal | Proposed measurable target | Important qualification |
| --- | --- | --- |
| Idle and loaded memory | Aspirational ≥50% lower RAM for *comparable core user journeys* | Measure **whole stack**, not only Go RSS |
| CPU | Aspirational ≥30% lower under matched throughput | Equal error rate, durability and policy enforcement |
| API latency | P95 no worse on equivalent operations | Also report P50/P99, errors and saturation |
| Throughput | At least equivalent under the same resource ceiling | Isolate CRUD, Auth, realtime, storage, search |
| Setup | Core local install journey in under 5 minutes on prepared host | Time from documented prerequisites; disclose downloads |
| Security | Zero open critical findings at release gate | Independent negative tests and scope documentation |
| Tenant isolation | 100% of required negative tests pass | No hidden superuser fixtures |
| Durability | No loss of acknowledged work within stated guarantees | Declare idempotency/replay model |
| HA availability | 99.99% *aspirational* for a qualified HA deployment | Not a Lite or single-node guarantee |
| Recovery | RPO < 1 min, RTO < 5 min *aspirational* for qualified HA | Fault model, backup/PITR and measurement required |
| Soak | 168-hour final stability exercise | Exact workloads, clocks and failure metrics recorded |
| Upgrade | Supported major-version upgrade and restore rehearsed | No undocumented destructive migration |

**Comparison exclusions:** do not score against Supabase Cloud-only management, commercial services or provider SLOs. Likewise, do not count unimplemented Ruxbase targets as delivered.

## 7. PocketBase upstream and compatibility policy

- **Baseline:** consult `UPSTREAM.lock.json` for the currently verified tag/commit. The original recorded reference is PocketBase `v0.40.6`, commit `9d281ccaba65f2686eb7b0b614ccda72d1116a1b`. Do not hard-code historical values as proof of current ancestry.
- **Upstream:** official PocketBase only. **Popgres is excluded** from upstream tracking and integration.
- **Classes:** `DIRECT_PORT`, `ADAPTED_PORT`, `RUXBASE_EQUIVALENT`, `NOT_APPLICABLE`, `DEFERRED`. Every change receives evidence and an owner; applicable security fixes receive priority.
- **Merge rule:** selective review plus relevant Go, PostgreSQL 17/18, SDK, golden/parity and independent tests. No automatic merge.
- **History rule:** a prospective ancestry commit is not evidence that new upstream features have been integrated. Never rewrite protected branch history or fabricate provenance.
- **Compatibility:** preserve documented PocketBase hooks, auth collections, REST/JSON responses, SDK contracts and importer paths. Intentional exceptions require tests and explicit mapping.

## 8. Team, ownership and approval separation

Proposed future role model; **role labels do not grant GitHub or human authority**:

| Role | Responsibility |
| --- | --- |
| A01 | Principal Architect / engineering direction / integration |
| A02 | Go kernel / PocketBase upstream adaptation |
| A03 | PostgreSQL engine / schema / DBA |
| A04 | Security / IAM / authorization |
| A05 | Distributed systems / realtime |
| A06 | Jobs / workflows / function isolation |
| A07 | REST / SQL RPC / GraphQL / CLI / SDK |
| A08 | Studio / optional control plane |
| A09 | Infrastructure / HA / observability |
| A10 | AI / MCP / plugin ecosystem |
| A11 | Independent engineering certification (no implementation of reviewed candidate) |

Historical reports naming an independent **Agent 06** retain their original identities; do not retroactively rename an auditor. Record who actually implemented, independently reviewed and owner-approved each release. AI assessments do not replace required human GitHub approval.

## 9. Shared delivery and release gates

Every executable milestone needs:

1. **Scope:** issue, baseline commit/tree, owner-approved acceptance criteria, explicit exclusions.
2. **Implementation:** source plus regression tests; relevant documentation, API and migration contracts.
3. **Security:** positive and negative permission tests using intended restricted roles, no privilege escalation, secret exposure, cross-tenant access or context leaks.
4. **Data:** PG17/18 integration; transaction, lock, cancellation, historical migration replay and restore checks as applicable.
5. **Compatibility:** exact permitted PocketBase API/SDK changes; documented exceptions.
6. **Reliability:** crash/restart/reconnect behavior, event/job loss semantics and operational observations as appropriate.
7. **Evidence:** exact candidate SHA/tree, full required CI, benchmark provenance, unresolved findings.
8. **Approval:** independent engineering review, human maintainer/owner authorization and compliant PR merge where required; post-merge CI.
9. **Release:** production remains NO-GO unless separately certified against a specific release candidate.

A full CI run is performed on the completed candidate (after writing all relevant code and test cases); rerun after fixes and again after merge. No approval or check may be inferred from another commit.

## 10. Current checkpoint (evidence snapshot; not a live status service)

- **M00 / Wave 00:** [Issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4) **closed** after [PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33) merged at `ea47d499519e7e017724881378213e626d7fa47d`. Official PocketBase v0.40.6 ancestry is verified in `main`; [CI #44826223333](https://github.com/AbdullahZaher/ruxbase/actions/runs/44826223333) and [#44827025018](https://github.com/AbdullahZaher/ruxbase/actions/runs/44827025018) succeeded. **Governance qualification:** ruleset `27761929` still requires zero approving reviews, and PR #33 has a `COMMENTED` owner-account certification rather than an independent `APPROVED` review. Track positive independent approval enforcement in the checklist; do not equate issue closure with satisfaction of stronger roadmap-wide gates.
- **Wave 01 M01:** [Issue #9](https://github.com/AbdullahZaher/ruxbase/issues/9) closed **for restricted Scope A**. [PR #30](https://github.com/AbdullahZaher/ruxbase/pull/30) merged scheduler stabilization, with [post-merge CI #44820224648](https://github.com/AbdullahZaher/ruxbase/actions/runs/44820224648) successful on Wave 01 commit `2170cd951ef33efd7f1744adcda23df990e4b6d4`. Scope B and production remain excluded.
- **Roadmap:** Wave 01 milestone labels beyond M02 have appeared inconsistently in historical reports. The proposed M03–M06 mapping above must be reconciled with accepted milestone specifications before implementation; a roadmap document does not silently amend existing scope.
- **Developer documentation:** may advance independently on `main`; published documentation snapshots are not proof of Wave 01 implementation.
- **Production:** NO-GO. Exact branch heads, PRs, workflow results, rulesets and issues can change; always verify live.

## 11. Maintenance rules

- Use [MASTER-CHECKLIST.md](MASTER-CHECKLIST.md) as the structured progress ledger, not repeated prose claims.
- Each checklist completion requires a merged integration SHA, exact test evidence, issue/PR reference and independent/owner approval where applicable.
- Do not mark a wave complete based on plans, generated documents, CI-only evidence or a restricted sub-scope.
- When decisions change, update this roadmap and checklist **together** in a reviewed PR, recording the rationale and impacts. Do not rewrite historical certification reports without an explicit dated correction.
- New AI sessions must read [AGENTS.md](AGENTS.md), this roadmap, the checklist and [AI protocol](docs/roadmap/AI-EXECUTION-PROTOCOL.md), then refresh current state from GitHub before doing work.

**Next responsible engineering steps:** close the remaining M00 gates without history loss; freeze the accepted Wave 01 milestone specifications; execute **M02 — Platform Kernel Contracts** in the active wave only after authorization. Do not confuse M02 with Wave 02.
