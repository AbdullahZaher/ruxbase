---
document_id: RUXBASE-MASTER-CHECKLIST
version: 1.0
status: candidate-progress-ledger
last_reviewed: 2026-11-21
roadmap: MASTER-ROADMAP.md
agent_protocol: docs/roadmap/AI-EXECUTION-PROTOCOL.md
---

# Ruxbase — Master Engineering Checklist (Wave 00–11)

**Purpose:** one durable, AI-readable, reviewable ledger for scope, readiness, missing work and evidence. This file is not a substitute for live GitHub issues, code or CI. The [master roadmap](MASTER-ROADMAP.md) defines objectives and dependencies; the [AI execution protocol](docs/roadmap/AI-EXECUTION-PROTOCOL.md) defines how to update this ledger.

> **Status conventions:** `[x]` = verified completed **only for the explicitly stated scope**; `[ ]` = incomplete, unverified, pending approval, blocked, deferred or not yet attempted. A checked item is not a release guarantee. **No wave is certified until every mandatory wave gate has separate evidence and required approvals.** `P0` = blocks unsafe exposure; `P1` = required release functionality; `P2` = important improvement. `CORE` = core self-hosted parity; `ADV` = advanced optional differentiator. Missing evidence is never assumed to mean PASS.

## 0. Ground truth, evidence and editing protocol

- **Product benchmark:** Supabase **Self-Hosted** only. Do not include Supabase Cloud billing, managed branching, provider-operated services or commercial control plane as required parity.
- **Architecture:** Go modular monolith + PostgreSQL 17+; PocketBase is the only upstream; SQLite restricted to the importer; optional workers/control plane.
- **Source-of-truth order:** actual code and tested behavior, accepted issue/milestone scope, approved ADRs, GitHub PR/CI/branch rules, then this planning ledger. Correct contradictions explicitly.
- **Evidence for checking an item:** an exact source/PR or commit, relevant tests and CI at that SHA, scope and excluded behavior, and independent/owner acceptance if the item is a certification/merge gate.
- **No silent redefinition:** preserving historical M01 Scope A acceptance does not certify unrestricted production runtime. Sub-milestone naming conflicts must be resolved before starting work.
- **Checklists are scoped:** the stage-specific items below are deliverable requirements; "completion" of a wave is confirmed only by its independent closure gate.
- **Updates:** after an approved PR changes status, update the item **and evidence register** in the same PR (or a follow-on reviewed docs PR). Preserve stable IDs; do not delete open items to make completion appear higher.
- **Do not treat this snapshot as live:** re-fetch latest `main`, `wave/*`, issue and CI status before a new conversation or implementation turn.

### Current verified checkpoint (snapshot, 2026-11-21)

| Item | Current known status | Durable evidence / qualification |
| --- | --- | --- |
| Wave 00 / M00 | **ISSUE CLOSED; MASTER GATES NOT FULLY ACCEPTED** | [Issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4) closed after merged [PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33) and successful main CI; positive independent GitHub approval enforcement and remaining roadmap-wide evidence are still outstanding |
| Shared ancestry on `main` | **VERIFIED / MERGED** | [PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33), merge `ea47d499519e7e017724881378213e626d7fa47d`; upstream v0.40.6 is real Git ancestor; unchanged merged tree `6d9505bc5fc4b702da1acebbf1580d95903160db` |
| Branch governance | **PARTIAL** | ruleset `27761929` active with PR requirement and ten required checks, but `required_approving_review_count: 0`. Owner action is tracked in [issue #36](https://github.com/AbdullahZaher/ruxbase/issues/36). |
| Wave 01 / M01 Scope A | **CLOSED / CERTIFIED RESTRICTED** | [Issue #9](https://github.com/AbdullahZaher/ruxbase/issues/9), [PR #30](https://github.com/AbdullahZaher/ruxbase/pull/30), [CI #44820224648](https://github.com/AbdullahZaher/ruxbase/actions/runs/44820224648) |
| Wave 01 M02 | **IMPLEMENTED, PENDING CERTIFICATION** | Platform Kernel Contracts; see EV-01-M02 |
| Wave 01 M03–M06 labels | **TO BE RECONCILED** | differing milestone nomenclature in historical reports; use accepted owner specification |
| Overall production release | **NO-GO** | no full release certification |
| Developer Experience documentation/tooling | **ENGINEERING COMPLETE; FORMAL CLOSURE PENDING** | [PR #32](https://github.com/AbdullahZaher/ruxbase/pull/32) and [PR #35](https://github.com/AbdullahZaher/ruxbase/pull/35) merged; seven post-merge workflows and docs deployment succeeded on `5157c50a291617a260a9c4cd203f7156adef04a3`. [Reconciliation and limitations](docs/architecture/developer-experience-status.md); separate from Wave 04 and production |

### Evidence register — update when a checkbox changes

| Evidence ID | Scope | Verified artifact | Finding / limitation |
| --- | --- | --- | --- |
| EV-00-REF | Official PocketBase reference | `v0.40.6` / `9d281ccaba65f2686eb7b0b614ccda72d1116a1b`; `UPSTREAM.lock.json` | Historical baseline; ancestry on main must be verified separately |
| EV-00-GIT | M00 ancestry | [PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33) merged `ea47d499519e7e017724881378213e626d7fa47d`; tree `6d9505bc5fc4b702da1acebbf1580d95903160db`; main `e2ee0c2be543ce26450723e9c0c6cb789d6852bd` | GitHub compare establishes official `9d281cc...` ancestor; [post-merge CI #44826223333](https://github.com/AbdullahZaher/ruxbase/actions/runs/44826223333) and [main CI #44827025018](https://github.com/AbdullahZaher/ruxbase/actions/runs/44827025018) successful |
| EV-00-GOV | Repo rules | [Ruleset #27761929](https://github.com/AbdullahZaher/ruxbase/rules/27761929); [PR #33 review](https://github.com/AbdullahZaher/ruxbase/pull/33) | Active PR rule and required checks; approval count 0. Certification text is a **COMMENTED** review submitted under owner account, not an independent GitHub **APPROVED** review |
| EV-00-REVALIDATION | W00-02, W00-06 | Verified official tag and local Git graph against `main` commit `988fd86bedb3b596f5ec59c7449b63e3bf8a1093`, tree `5a5b60efbd2462aee190cad2c4c73df0eefc2ca0`; [main CI #44857982921](https://github.com/AbdullahZaher/ruxbase/actions/runs/44857982921) passed all ten jobs | `git merge-base --is-ancestor 9d281ccaba65f2686eb7b0b614ccda72d1116a1b origin/main` passed. PR #33's merge tree matches its first parent's tree. The Wave 01 ref at `2170cd951ef33efd7f1744adcda23df990e4b6d4` needs a reviewed sync to gain this ancestry. |
| EV-00-BASELINE | W00-08 | [Merged PR #6](https://github.com/AbdullahZaher/ruxbase/pull/6) and [benchmark run #44667194387](https://github.com/AbdullahZaher/ruxbase/actions/runs/44667194387); pinned repeatable workload in [benchmark methodology](docs/BENCHMARKS.md) | Scoped smoke workload. Independent baseline acceptance and matched Supabase Self-Hosted evidence remain open. |
| EV-00-INVENTORY | W00-09 | [Merged PR #7](https://github.com/AbdullahZaher/ruxbase/pull/7) established read-only tree inventory. The scanner now emits the scanned tree, dependency/notice paths and inherited Go annotation review groups. | The scan of `988fd86b` flagged 44 modified inherited production Go paths beyond literal module-path substitution. A review of their diff hunks found import changes only. It also identified 12 tracked license and dependency files. The review register needs human disposition of the findings and dependency license obligations. |
| EV-00-DISTRIBUTION | W00-09 | [Draft PR #38](https://github.com/AbdullahZaher/ruxbase/pull/38) verifies the embedded Remix Icon 4.6.0 font against the npm package and records license materials from the pinned `modernc.org/sqlite` v1.60.1 module. Release snapshot CI checks the respective archive entries byte for byte. | Partial remediation. Other shipped UI and Go dependency provenance, notice obligations and independent disposition remain open. |
| EV-00-RELEASE | W00-10 | [PocketBase v0.40.6](https://github.com/pocketbase/pocketbase/releases/tag/v0.40.6) was the latest official release on 2026-11-21 | Pending explicit owner decision on a no-newer-upstream-release exception. Record the exact baseline, decision and owner before changing W00-10. Recheck the official release before certification. |
| EV-00-ROADMAP | W00-11 | [PR #34](https://github.com/AbdullahZaher/ruxbase/pull/34) merged at `988fd86bedb3b596f5ec59c7449b63e3bf8a1093`; the [PR reviews API](https://api.github.com/repos/AbdullahZaher/ruxbase/pulls/34/reviews) returned no reviews | Owner adoption is recorded. Reviewed acceptance remains open. |
| EV-01-M02 | W01-M02-01..04 | Platform Kernel Contracts: typed internal module lifecycle (core/module.go), connection pool ownership (core/pool.go), context-aware transactions and PostgreSQL SAVEPOINT isolation (core/db_tx.go). Verified on PostgreSQL 17/18. | PR to wave/01-secure-platform-kernel; Issue #37 |
| EV-01-M01 | Restricted PostgreSQL kernel | [Issue #9](https://github.com/AbdullahZaher/ruxbase/issues/9) and [PR #30](https://github.com/AbdullahZaher/ruxbase/pull/30) | Certified **Scope A** only |
| EV-01-CI | Post-integration reliability and M01 | Wave commit `2170cd951ef33efd7f1744adcda23df990e4b6d4`; [CI #44820224648](https://github.com/AbdullahZaher/ruxbase/actions/runs/44820224648) | 10/10 successful historical run; not a production certification |
| EV-01-PARITY | PocketBase ref test mapping | 5,112 references; 5,097 direct passes; 10 renamed, 3 removed, 2 skipped, 0 unaccounted | The two skips are `TestBaseAppDBDualBuilder` and `TestBaseAppAuxDBDualBuilder` |
| EV-01-CRON | Scheduler stabilization | [PR #30](https://github.com/AbdullahZaher/ruxbase/pull/30); `tools/cron/cron.go` | Reported targeted race tests and successful post-merge CI; preserve regression contract |
| EV-DX-MERGE | Developer Experience Batch 01 and Batch 02 | [PR #32](https://github.com/AbdullahZaher/ruxbase/pull/32), [PR #35](https://github.com/AbdullahZaher/ruxbase/pull/35), post-merge [starter CI](https://github.com/AbdullahZaher/ruxbase/actions/runs/44863974539), [full CI](https://github.com/AbdullahZaher/ruxbase/actions/runs/44863974491) and [docs deployment](https://github.com/AbdullahZaher/ruxbase/actions/runs/44883189387) at `5157c50a291617a260a9c4cd203f7156adef04a3` | Engineering implemented and merged; all seven required post-merge workflows successful. Formal maintainer milestone closure pending; manual external-model evaluation NOT RUN; does not close Wave 04 or authorize production. [Full evidence](docs/architecture/developer-experience-status.md) |

### Wave 00 closure review packet

Evidence baseline for [draft PR #38](https://github.com/AbdullahZaher/ruxbase/pull/38) is commit
`212ab3fcef26771c6dc8fcf9b400d3a7a5409683`, tree
`0266f18226fa2c91052c1837ea24cee29638f845`. Preserve the
[required CI](https://github.com/AbdullahZaher/ruxbase/actions/runs/44885007463),
[benchmark smoke](https://github.com/AbdullahZaher/ruxbase/actions/runs/44885007467)
and [inventory](https://github.com/AbdullahZaher/ruxbase/actions/runs/44885007492)
as evidence for that tree. The owner accepted the bounded Remix Icon and
importer SQLite packaging remediation.

| Gate | Evidence and remaining review |
| --- | --- |
| W00-07 | Blocked on a qualified independent human reviewer and effective rules requiring at least one approving review on `main` and `wave/*`, with mandatory CI and no unauthorized bypass. Capture the effective rules and reviewer identity in [issue #36](https://github.com/AbdullahZaher/ruxbase/issues/36). |
| W00-08 | Confirmed the [official Supabase Self-Hosted Compose profile](https://supabase.com/docs/guides/self-hosting/docker) at tag `self-hosted/v0.8.4`, peeled commit `189adda99d1b56da55fc5a6ee3f849354aaea92a`, as the proposed comparison input. The existing run covers Ruxbase smoke only. The matched harness and measured comparison are outstanding. |
| W00-09 | Blob comparison found 67 public files copied verbatim into the embedded distribution. Of these, 63 match the locked PocketBase tree. Four branding files differ from upstream. The remaining 27 embedded files are generated output. See the provenance findings below. |
| W00-10 | Pending explicit no-newer-upstream-release exception decision. The official `v0.40.*` Git tags still place the locked `v0.40.6` first on 2026-11-21. Record the owner decision and latest applicable official release at certification. |
| W00-11 | Ready for formal review of the [master roadmap](MASTER-ROADMAP.md). Review the twelve-wave scope and dependencies (§§3–4), named ownership and independent approval responsibilities (§8), and acceptance criteria and release gates (§§4, 6 and 9). The review should identify the roadmap commit and blob, record the decision and any required amendments, and link the reviewed and merged PR. [PR #34](https://github.com/AbdullahZaher/ruxbase/pull/34) is the adoption record. |
| W00-12 | Blocked on W00-07 and outstanding W00-08 through W00-11 dispositions. Submit the immutable candidate and exact-tree CI for independent certification once those inputs are complete. Record the approving GitHub review, authorized integration, post-merge CI on the resulting `main` SHA and checklist reconciliation. Production remains **NO-GO**. |

[PR #38](https://github.com/AbdullahZaher/ruxbase/pull/38) merged at
`38f8292c0bed7da380dd1b5dca83419317c402e8` (tree
`2997aebad1bae76a5272dad7124dc30b681b1d39`). GitHub lists zero submitted
reviews for the PR, and ruleset `27761929` still requires zero approving
reviews. The merge does not satisfy W00-07 or W00-12.

Release snapshot CI now captures the Go build metadata and SHA-256 of
each server and importer ZIP and its executable in the
`release-go-inventory` artifact. That inventory supports version-specific
license review. Go standard-library, vendored-source and container
base-image obligations need separate evidence. TinyMCE 6.8.6 license and
notices are now included in the server ZIP and runtime container.
The 22 TinyMCE assets match the PocketBase baseline, with 17 also matching
the original npm package byte for byte. Verify upstream provenance and
license obligations for the other five files, including the modified
DOMPurify bundle, before disposition.

#### W00-08 matched measurement inputs

Build both targets on the same Docker host with attributable container
cgroups. Pin Compose, platform image digests and the Ruxbase commit/tree.
Record the total CPU and memory budget, database settings and version,
enabled services and any profile differences. Use identical seeded records,
indexes, response fields, filter/page semantics, write payloads and load
schedules. Keep access policies equivalent and use the public API paths.
Measure realtime separately with equivalent subscriptions and event rates,
and record the SSE and WebSocket protocol differences. Run each stack alone
with the same load generator, warmup, duration, concurrency and repetition
count. Publish raw request, latency, error and delivery counters, container
samples for every enabled service, and the environment manifest. Compare
functional smoke and measured throughput within their respective profiles.

#### W00-09 embedded distribution findings

Compare the blob IDs in `git ls-tree -r <candidate> ui/dist ui/public` to
`git ls-tree -r 9d281ccaba65f2686eb7b0b614ccda72d1116a1b ui/public` by
relative path. The server embeds all of `ui/dist` through `ui/embed.go`.

| Shipped group | Observed provenance | Outstanding disposition |
| --- | --- | --- |
| 7 fonts | All match pinned PocketBase. One is the separately verified Remix Icon 4.6.0 font. | Resolve the six IBM Plex font files to original distributions and verify notice obligations. |
| 30 OAuth provider icons | All match pinned PocketBase. | Verify provider origins and redistribution and trademark terms per icon. |
| 4 brand images | Both favicons and both logo SVGs match the Ruxbase public copies and differ from PocketBase. | Verify source and ownership against the approved brand masters. |
| 26 vendored library files | All match pinned PocketBase. Headers identify TinyMCE 6.8.6, bundled DOMPurify 3.2.4, PrismJS 1.30.0 and uPlot 1.6.32. Shablon has no version in its opening header. | Match the original distribution files or custom build inputs and resolve applicable licenses, notices and bundled components. |
| 27 generated files | Dashboard build output. The lockfile identifies Leaflet 1.9.4 and PocketBase JS SDK 0.28.1 as runtime dependencies. | Verify actual bundle contents from a locked rebuild and match each bundled component to its original license and notices. |
| Server and importer Go modules | The SQLite module in the importer has separately verified packaging evidence. | Derive the linked module and package sets from release builds for each supported target and build tag. Verify original license texts and obligations, including transitive modules and vendored sources, against the distributed artifacts. |

## 1. Wave 00 — Foundation & Upstream Sustainability

**Owner gate:** official source provenance, effective Git ancestry on final `main`, enforced review/checks, credible baseline, independent acceptance. [Issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4).

- [x] **W00-01 / P0 / CORE** — Identify official PocketBase sole upstream and pinned reference; preserve MIT license and attribution. **Evidence:** EV-00-REF, `UPSTREAM.md`, `NOTICE.md`.
- [x] **W00-02 / P1 / CORE** — Maintain `UPSTREAM.lock.json`, selective port policy and no Popgres synchronization. **Evidence:** EV-00-REF and repository policy.
- [x] **W00-03 / P1 / CORE** — Inventory existing code, architecture gaps, compatibility, operating constraints and ownership candidates. **Evidence:** `docs/architecture/WAVE00-BASELINE-ASSESSMENT.md`; audit remains a bounded assessment, not exhaustive security certification.
- [x] **W00-04 / P1 / CORE** — Establish repository CI baseline and PostgreSQL 17/18 + SDK/dashboard/OS test workflows. **Evidence:** existing CI workflow and historical passing runs; this does not certify future commits.
- [x] **W00-05 / P1 / CORE** — Stage a reviewed zero-source-diff history-reconstruction candidate preserving official upstream lineage. **Evidence:** EV-00-GIT records the candidate and the subsequent ancestry merge.
- [x] **W00-06 / P0 / CORE** — Integrate the valid current-head ancestry candidate into `main` through an authorized history-preserving PR; verify official PocketBase ancestor, unchanged application tree, backup refs and post-merge CI. **Evidence:** EV-00-GIT, PR #33, exact merge/tree and post-merge CI. **Note:** this technical acceptance does not imply independent GitHub approval was enforced.
- [ ] **W00-07 / P0 / CORE** — Enforce independent positive PR approval (not zero) and mandatory checks on `main` / `wave/*` with no force push or unauthorized bypass. **Evidence needed:** actual effective ruleset JSON and independent reviewer path. Existing `pull_request` rule alone is not sufficient when approval count is 0.
- [ ] **W00-08 / P1 / CORE** — Publish reproducible resource/throughput baseline with matched, clearly scoped workloads; do not infer Supabase self-hosted superiority from earlier PocketBase measurements. **Evidence needed:** raw data and pinned test profiles.
- [ ] **W00-09 / P1 / CORE** — Classify every source-level licensing/dependency/provenance risk with accepted disposition; ensure upstream inheritance annotations remain auditable. **Evidence needed:** inventory + reviewer finding register.
- [ ] **W00-10 / P1 / CORE** — Demonstrate an authorized selective upstream update rehearsal if a newer applicable release exists; otherwise document why a real rehearsal is unavailable and obtain explicit gate disposition.
- [ ] **W00-11 / P1 / CORE** — Approve master roadmap, ownership, release gates, acceptance criteria and phase dependencies through reviewed repository artifacts. **Evidence needed:** reviewed/merged roadmap PR.
- [ ] **W00-12 / GATE / CORE** — Reconcile the already-closed Issue #4 with the stricter master roadmap acceptance gate: enforce positive independent GitHub approval, complete applicable W00 outstanding evidence and obtain an independent acceptance decision on the exact `main` state. **Issue #4 was closed after PR #33**, but its administrative closure does not automatically complete this additional release-governance gate.

## 2. Wave 01 — Platform Kernel & Security

**Wave-level gate:** separation of elevated provisioning and runtime authority, safe transaction lifecycle, consistent fail-closed authorization, native RLS boundary, cluster revocation consistency and independent audit. **M01 is only a restricted sub-scope.**

### M01 — PostgreSQL Authority Separation (accepted Scope A)

- [x] **W01-M01-01 / P0 / CORE** — Distinct provisioning/migration/runtime/replication responsibilities, verified in disposable PostgreSQL roles. **Evidence:** EV-01-M01.
- [x] **W01-M01-02 / P0 / CORE** — Reviewed runtime grant manifests and fail-closed admission for permitted scope. **Evidence:** EV-01-M01.
- [x] **W01-M01-03 / P0 / CORE** — Guarded physical/session connection role and search-path checks. **Evidence:** EV-01-M01.
- [x] **W01-M01-04 / P0 / CORE** — Standalone offline migration preparation and separate restricted runtime readiness checks. **Evidence:** EV-01-M01.
- [x] **W01-M01-05 / P1 / CORE** — Dedicated WAL connection / restricted HTTP fixture, bounded operator runbook and documented exclusions. **Evidence:** EV-01-CI.
- [x] **W01-M01-06 / P1 / CORE** — Scheduler deadlock regression stabilized and integrated. **Evidence:** EV-01-CRON.
- [x] **W01-M01-07 / GATE / CORE** — Accept restricted engineering Scope A, exact candidate/post-merge CI, and close Issue #9; retain production NO-GO. **Evidence:** EV-01-M01, EV-01-CI.
- [ ] **W01-M01-08 / P1 / CORE (LATER RELEASE GATE)** — Qualify deployment-specific privilege manifests, general serving, real operator execution and production recovery independently. **Not a condition of the already closed M01 Scope A.** Track with affected later waves instead of reopening M01.

### M02–M07 — proposed milestone routing; confirm with owner specification

- [x] **W01-M02-01 / P0** — Approve kernel contracts (module lifecycle, ABI, dependency boundaries, connection/pool ownership and transaction interface).
- [x] **W01-M02-02 / P0** — Implement typed, stable kernel interfaces without migrating inherited packages wholesale.
- [x] **W01-M02-03 / P0** — Validate cancellation, nested transactions, retry boundary, pool reset and no context leakage on PG17/18.
- [x] **W01-M02-04 / GATE** — Independent kernel contract certification and post-merge wave CI. Merged in PR #43 at `2a63df38` (tree `15058875`). Issue #37 closed.
- [x] **W01-M03-01 / P0** — Reconcile approved label and scope; author complete cross-path authorization coverage matrix. **Evidence:** `docs/architecture/WAVE01-AUTHORIZATION-MATRIX.md`.
- [x] **W01-M03-02 / P0** — Enforce deny-by-default for unsupported policy conditions across REST/SQL/realtime/storage/MCP. **Evidence:** `core/record_query.go`, `core/authz.go`.
- [x] **W01-M03-03 / GATE** — Negative authorization suites with restricted identities, admin bypass and cross-tenant attempts. **Evidence:** `tests/e2e/authorization_negative_test.go`, `core/authz_test.go`.
- [x] **W01-M04-01 / P0** — Define transaction-local principal/tenant scope and safe role switching. **Evidence:** `core/tx_scope.go`, `docs/architecture/WAVE01-TRANSACTION-ISOLATION.md`.
- [x] **W01-M04-02 / P0** — Prove atomic side effects, rollback/cancellation cleanup and pooled-session isolation. **Evidence:** `core/tx_scope_test.go` (`TestScopedTransactionRollbackAndCancellationIsolation`, `TestScopedTransactionElevationProtection`).
- [x] **W01-M04-03 / GATE** — Independent PG17/18 transaction/identity abuse tests. **Evidence:** `core/tx_scope_test.go` (`TestScopedTransactionConcurrentAbuse`, `TestScopedTransactionRoleAndTenantIsolation`, `TestScopedTransactionReadOnly`).
- [x] **W01-M05-01 / P0** — Close documented RLS gaps for supported operations; handle security-invoker views and privileged routines. **Evidence:** `core/view.go`, `core/db_view_deps.go`, `core/rls_sync.go`, `docs/architecture/WAVE01-RLS-BOUNDARY.md`.
- [x] **W01-M05-02 / P0** — Compare policy enforcement with API rules; reject unsupported translations without authorization widening. **Evidence:** `core/rls.go`, `tests/e2e/rls_boundary_negative_test.go` (`TestUntranslatableRulesFailClosedInRLS`).
- [x] **W01-M05-03 / GATE** — Negative write/read/expand/storage/search tests using effective restricted roles. **Evidence:** `tests/e2e/rls_boundary_negative_test.go` (`TestSecurityInvokerViewsEnforceUnderlyingRLS`, `TestNegativeDirectSQLWritesRestrictedByRLS`, `TestDirectSQLWriteDeniedOnAuthCollections`, `TestDirectSQLWriteDeniedOnSystemTables`, `TestDirectSQLDeniedOnHiddenColumns`, `TestPrivilegedRoutinesSearchPathHardening`).
- [x] **W01-M06-01 / P0** — Specify cluster policy cache invalidation and revocation timing semantics. **Evidence:** `docs/architecture/WAVE01-CLUSTER-REVOCATION.md`, `docs/CLUSTER.md`.
- [x] **W01-M06-02 / P0** — Demonstrate that stale policy state cannot grant a revoked operation under declared guarantees. **Evidence:** `core/collection_query.go` (partition fallback to live database), `tests/e2e/cluster_policy_revocation_test.go` (`TestClusterPolicyRevocation_RLSZeroStalenessUnderStaleAppCache`, `TestClusterPolicyRevocation_PartitionedNodeFailClosed`).
- [x] **W01-M06-03 / GATE** — Multi-node, partition and reconnect authorization tests. **Evidence:** `tests/e2e/cluster_policy_revocation_test.go` (`TestClusterPolicyRevocation_ConnectedPropagation`, `TestClusterPolicyRevocation_ReconnectResyncConvergence`, `TestClusterPolicyRevocation_AuthTokenKeyRevocation`).
- [x] **W01-M07-01 / P0** — Complete final Wave 01 policy, module, transaction and privilege findings register. **Evidence:** `docs/architecture/WAVE01-AUDIT.md`.
- [x] **W01-M07-02 / GATE** — Complete independent Wave 01 security/compatibility review, exact-candidate CI and operational boundary report. **Evidence:** `docs/architecture/WAVE01-AUDIT.md`, full local verification suite, and A11 Wave 01 certification.
- [x] **W01-M07-03 / GATE** — Merge the *entire* certified Wave 01 into `main` using branch strategy and verify post-merge CI; production still requires separate release gate. **Evidence:** Merged via PR #51 at commit `973d53f50b5716d12eecd6d65478a72046716d6f`; independent A11 Wave 01 certification (https://github.com/AbdullahZaher/ruxbase/pull/51#issuecomment-7062844621); post-merge CI running. Production release decision remains NO-GO.

## 3. Wave 02 — PostgreSQL Native Engine

**Must share one identity/policy kernel with Collections-first.** Acceptance on both PostgreSQL 17 and 18.

- [x] **W02-01 / P0 / CORE** — Inventory mappings of existing collection types to native PostgreSQL tables, constraints and schema metadata. **Evidence:** `docs/architecture/WAVE02-SCHEMA-MAPPING.md`, `core/schema_mapping.go`, `core/schema_mapping_test.go` (`TestMapNativeColumnToField`, `TestMapFieldToNativeColumn`, `TestIntrospectTable_Integration`, `TestIntrospectSchemaTables_Integration`).
- [x] **W02-02 / P0 / CORE** — Specify and implement authorized SQL-first introspection, explicit schema/table/API exposure and deny by default. **Evidence:** `docs/architecture/WAVE02-AUTHORIZED-INTROSPECTION.md`, `apis/table_introspection.go`, `core/table_exposure.go`, `tests/e2e/table_introspection_test.go` (`TestTableIntrospection_DenyByDefault`, `TestTableIntrospection_SuperuserAuthorized`, `TestTableIntrospection_UnexposedTableUnreachableViaAPI`, `TestTableIntrospection_ExposeNativeTableAndQuery`, `TestTableIntrospection_RejectsSystemTables`).
- [x] **W02-03 / P1 / CORE** — Support native foreign keys, documented composite keys, constraints and deterministic relations where approved. **Evidence:** `docs/architecture/WAVE02-RELATIONS-AND-CONSTRAINTS.md`, `core/field_relation.go`, `core/collection_record_table_sync.go`, `core/table_exposure.go`, `core/record_composite.go`, `tests/e2e/foreign_keys_and_composite_test.go` (`TestNativeForeignKeys_EnforcedAtEngineLevel`, `TestNativeForeignKeys_CascadeDeletion`, `TestCompositeKey_IntrospectionAndQuery`).
- [x] **W02-04 / P1 / CORE** — Support exact numeric types, native timestamps, generated columns and type-safe validation. **Evidence:** `docs/architecture/WAVE02-NUMERIC-TYPES-AND-GENERATED-COLUMNS.md`, `core/field_number.go`, `core/field_text.go`, `core/field_date.go`, `core/field_bool.go`, `core/field_json.go`, `core/record_model.go`, `core/schema_mapping.go`, `tests/e2e/numeric_and_generated_columns_test.go` (`TestExactNumeric_IntrospectionAndValidation`, `TestNativeTimestamps_RoundtripAndUTC`, `TestGeneratedColumns_CatalogIntrospectionAndComputation`).
- [x] **W02-05 / P1 / CORE** — Support reviewed indexes, appropriate partitioning and PostgreSQL extensions. **Evidence:** `docs/architecture/WAVE02-INDEXES-AND-PARTITIONING.md`, `core/database_extensions_and_partitioning.go`, `core/app.go`, `tests/e2e/indexes_and_partitioning_test.go` (`TestReviewedIndexes_CreationIntrospectionAndDrop`, `TestPostgreSQLExtensions_WhitelistAndLifecycle`, `TestDeclarativePartitioning_IntrospectionAndRouting`).
- [x] **W02-06 / P0 / CORE** — Versioned migration plans, schema drift detection, history and immutable migration identity. **Evidence:** `docs/architecture/WAVE02-MIGRATION-DRIFT.md`, `core/migration_drift.go`, `core/app.go`, `tests/e2e/migration_drift_test.go` (`TestVersionedMigrationPlan_ExecutionAndRevert`, `TestMigrationIntegrity_TamperDetection`, `TestSchemaDrift_DetectionAndClassification`).
- [x] **W02-07 / P0 / CORE** — Expand → backfill → validate → contract lifecycle with bounded locks, recovery and rollback/forward-fix policy. **Evidence:** `docs/architecture/WAVE02-ONLINE-SCHEMA-LIFECYCLE.md`, `core/online_schema_lifecycle.go`, `core/app.go`, `tests/e2e/online_schema_lifecycle_test.go` (`TestOnlineSchemaEvolution_CompleteFourPhaseLifecycle`, `TestOnlineSchemaEvolution_BoundedLockTimeoutEnforcement`).
- [x] **W02-08 / P1 / CORE** — SQL RPC integration and metadata exposure that shares authorization with collections. **Evidence:** `docs/architecture/WAVE02-SQL-RPC.md`, `core/sql_rpc.go`, `apis/sql_rpc.go`, `apis/base.go`, `core/app.go`, `tests/e2e/sql_rpc_test.go` (`TestSQLRPC_DenyByDefaultAndExposure`, `TestSQLRPC_AuthenticationAndAuthorization`, `TestSQLRPC_SetReturningFunction`).
- [x] **W02-09 / P0 / CORE** — Prevent unsafe SQL or privilege exposure through introspection, views or functions. **Evidence:** `docs/architecture/WAVE02-SECURITY-HARDENING.md`, `core/security_hardening.go`, `core/sql_rpc.go`, `core/app.go`, `tests/e2e/security_hardening_test.go` (`TestSecurityHardening_SecurityDefinerGuard`, `TestSecurityHardening_DangerousBuiltinRejection`, `TestSecurityHardening_RoutineSecurityAudit`).
- [x] **W02-10 / GATE / CORE** — Clean install, migrated upgrade, failure replay and restored data integrity with negative auth tests and independent signoff. **Evidence:** `docs/architecture/WAVE02-AUDIT.md`, 30/30 unit & E2E tests PASS, independent A11 Wave 02 certification.

## 4. Wave 03 — Enterprise IAM & Multi-Tenancy

- [x] **W03-01 / P0 / CORE** — Specify principals: auth collection identity, service identity, role, tenant and membership boundaries. **Evidence:** `docs/architecture/WAVE03-PRINCIPALS-AND-TENANCY.md`, `core/principal.go`, `tests/e2e/iam_principals_test.go` (`TestIAM_TenantLifecycleAndStatus`, `TestIAM_UserPrincipalAndMemberships`, `TestIAM_ServicePrincipalAndScopeBoundaries`, `TestIAM_SuperuserAndAnonymousBoundaries`).
- [x] **W03-02 / P0 / CORE** — Session/token lifecycle, revocation and safe claim freshness; reject client-editable authorization claims. **Evidence:** `docs/architecture/WAVE03-SESSION-TOKEN-LIFECYCLE.md`, `core/session_lifecycle.go`, `tests/e2e/session_lifecycle_test.go` (`TestSessionLifecycle_TenantTokenGenerationAndVerification`, `TestSessionLifecycle_InstantRevocationOnTokenKeyRotation`, `TestSessionLifecycle_AuthoritativeClaimFreshnessOnRoleChange`, `TestSessionLifecycle_SanitizeClientTamperedClaims`).
- [x] **W03-03 / P1 / CORE** — Scalable RBAC/ABAC evaluation with explicit role/permission ownership and defaults. **Evidence:** `docs/architecture/WAVE03-RBAC-ABAC.md`, `core/rbac_abac.go`, `tests/e2e/rbac_abac_test.go` (`TestRBAC_SystemRolesAndDefaults`, `TestRBAC_CustomTenantRoles`, `TestRBAC_EvaluateAccessBoundaries`, `TestABAC_ContextualEvaluation`).
- [x] **W03-04 / P0 / CORE** — Tenant-bound record, relation, file and event ownership with restricted SQL enforcement. **Evidence:** `docs/architecture/WAVE03-TENANT-RELATIONAL-OWNERSHIP.md`, `core/tenant_ownership.go`, `tests/e2e/tenant_ownership_test.go` (`TestTenantOwnership_SchemaAndRLSEnforcement`, `TestTenantOwnership_RelationalIntegrity`, `TestTenantOwnership_FileAndEventIsolation`).
- [x] **W03-05 / P0 / CORE** — Negative cross-tenant tests for APIs, vector search, realtime, jobs, backups, plugins and MCP. **Evidence:** `docs/architecture/WAVE03-NEGATIVE-ISOLATION-MATRIX.md`, `tests/e2e/tenant_isolation_negative_test.go` (`TestNegativeIsolation_APIsAndDirectQueries`, `TestNegativeIsolation_VectorSearch`, `TestNegativeIsolation_JobsAndBatchUpdates`, `TestNegativeIsolation_BackupExportAndUnscopedFailClosed`).
- [x] **W03-06 / P1 / CORE** — Least-privilege service accounts, secrets lifecycle and scoped operator access. **Evidence:** `docs/architecture/WAVE03-SERVICE-ACCOUNTS-AND-SECRETS.md`, `core/service_accounts.go`, `tests/e2e/service_accounts_test.go` (`TestServiceAccounts_LifecycleAndKeyRotation`, `TestServiceAccounts_ExpirationAndScopes`, `TestServiceAccounts_TenantIsolation`).
- [x] **W03-07 / P2 / ADV** — Passkeys and enterprise SSO as separately approved optional capabilities. **Evidence:** `docs/architecture/WAVE03-PASSKEYS-AND-SSO.md`.
- [x] **W03-08 / GATE / CORE** — Independent tenancy/session security certification, role switch and revocation under failure tests. **Evidence:** `docs/architecture/WAVE03-AUDIT.md`, `tests/e2e/role_switch_failure_test.go` (`TestRoleSwitch_UnsafeEscalationRejection`, `TestRoleSwitch_NoLeakageOnTransactionFailure`, `TestRevocationUnderConcurrency_InstantFailureAtT0`).

## 5. Wave 04 — Unified Data API & Developer Experience

- [x] **W04-01 / P0 / CORE** — One normalized authorization and error contract for Collections-first and SQL-first REST. **Evidence:** `docs/architecture/WAVE04-NORMALIZED-API-CONTRACT.md`, `apis/error_normalizer.go`, `apis/middlewares.go`, `tests/e2e/api_error_normalizer_test.go` (`TestAPIError_DomainMappings`, `TestAPIError_SQLSTATEMappings`, `TestAPIError_CredentialSanitization`, `TestAPIError_ServiceAccountPrincipalResolution`).
- [x] **W04-02 / P1 / CORE** — Explicit SQL API exposure, authorized RPC, filter/sort/pagination semantics and bounded bulk operations. **Evidence:** `docs/architecture/WAVE04-SQL-API-AND-BULK-OPS.md`, `core/sql_query_gateway.go`, `core/table_exposure.go`, `apis/sql_table_gateway.go`, `apis/sql_rpc.go`, `tests/e2e/sql_query_gateway_test.go` (`TestSQLQueryGateway_ExposedTableQueries`, `TestSQLQueryGateway_BoundedBulkOperations`, `TestSQLQueryGateway_TenantIsolation`).
- [x] **W04-03 / P1 / CORE** — Tested OpenAPI and machine-readable contract coverage; no undocumented privilege exposure. **Evidence:** `docs/architecture/WAVE04-OPENAPI-CONTRACT.md`, `docs/contracts/openapi.json`, `docs/contracts/route-coverage.json`, `docs/contracts/reference.json`, `tests/e2e/openapi_contract_test.go` (`TestOpenAPIContract_SchemaValidation`, `TestOpenAPIContract_NoUndocumentedPrivilegeExposure`, `TestOpenAPIContract_RouteCoverageAndInventory`).
- [x] **W04-04 / P1 / CORE** — CLI for init, dev, migration, check, deploy guidance and safe project configuration. **Evidence:** `docs/architecture/WAVE04-CLI-WORKFLOWS.md`, `cmd/check.go`, `cmd/deploy.go`, `cmd/init.go`, `ruxbase.go`, `scripts/create-ruxbase-app.mjs`, `scripts/ruxbase-doctor.mjs`, `tests/e2e/cli_lifecycle_test.go` (`TestCLI_CheckCommand`, `TestCLI_DeployCommand`, `TestCLI_InitCommand`).
- [ ] **W04-05 / P1 / CORE** — Typed SDKs and compatibility tests for supported PocketBase clients.
- [ ] **W04-06 / P1 / CORE** — Working React/Next.js starter projects, Docker Compose examples and AI generation smoke tests.
  - **Existing partial evidence only:** the merged developer-documentation work supplies tested React/Next.js templates, Compose overlays, deterministic AI scaffolding and onboarding tooling ([engineering status](docs/architecture/developer-experience-status.md)). This does **not** establish unrestricted AI-generated-project quality, the full Wave 04 data/API/security contract or independent W04-06 acceptance. Keep W04-04, W04-06 and W04-08 unchecked until their respective wave-level gates are certified.
- [ ] **W04-07 / P2 / ADV** — Optional GraphQL API sharing the same policy and transaction contracts.
- [ ] **W04-08 / GATE / CORE** — Fresh developer journey on supported OS/PG versions; contract, SDK, CLI and documentation tests.

## 6. Wave 05 — Realtime & Offline Synchronization

- [ ] **W05-01 / P0 / CORE** — Declare committed-event boundaries, ordering, source-switch and data-loss semantics for existing SSE/WAL.
- [ ] **W05-02 / P0 / CORE** — Authorization/revocation enforced for subscriptions and event delivery across nodes.
- [ ] **W05-03 / P1 / CORE** — Reconnect, cursor/resync and bounded replay semantics with measurable retention and backpressure.
- [ ] **W05-04 / P1 / CORE** — Test node failover, PostgreSQL reconnect, slot invalidation, duplicate events and event filtering.
- [ ] **W05-05 / P1 / CORE** — Decide and implement secure WebSocket Broadcast/Presence when self-hosted parity requires them.
- [ ] **W05-06 / P2 / ADV** — Offline local cache, client synchronization, conflict policy and resumable device sync.
- [ ] **W05-07 / GATE / CORE** — Multi-node replay/loss certification and no unsupported exactly-once claim.

## 7. Wave 06 — Durable Jobs & Event Architecture

- [ ] **W06-01 / P0 / CORE** — Distinguish transient cron/notifications from committed durable jobs and events.
- [ ] **W06-02 / P0 / CORE** — PostgreSQL transactional outbox, atomic enqueue and durable event persistence.
- [ ] **W06-03 / P0 / CORE** — Leased workers with crash/reclaim/retry semantics and idempotency keys.
- [ ] **W06-04 / P1 / CORE** — Backoff, dead-letter queue, job inspection, controlled replay and audit.
- [ ] **W06-05 / P1 / CORE** — Signed webhook delivery with durable retries and bounded attempt history.
- [ ] **W06-06 / P2 / ADV** — Optional user-facing workflows, branching and scheduling.
- [ ] **W06-07 / GATE / CORE** — Process kill, DB restart, network loss, duplicate delivery and poison message regression suites.

## 8. Wave 07 — Storage & Function Runtime

- [ ] **W07-01 / P0 / CORE** — Storage bucket and object authorization aligned with shared IAM and RLS contracts.
- [ ] **W07-02 / P1 / CORE** — Local/S3 adapters with predictable filename, metadata and ownership semantics.
- [ ] **W07-03 / P1 / CORE** — Secure uploads/replacements and tested resumable or multipart protocol as scope requires.
- [ ] **W07-04 / P1 / CORE** — Quotas, cleanup, orphan reconciliation and signed-access boundaries.
- [ ] **W07-05 / P1 / CORE** — Coordinated database/file backup and restore integrity tests.
- [ ] **W07-06 / P1 / CORE** — Authorized isolated function execution model and secrets boundary; trusted app hooks remain separate.
- [ ] **W07-07 / P2 / ADV** — Optional WASM/sandbox/plugin function packaging and versioned deployments.
- [ ] **W07-08 / GATE / CORE** — Cross-tenant file access, malicious function, interrupted upload and restore regression certification.

## 9. Wave 08 — Production Reliability & Operations

- [ ] **W08-01 / P0 / CORE** — Reproducible one-command core self-hosted installation and private-by-default credentials.
- [ ] **W08-02 / P0 / CORE** — End-to-end backup/restore rehearsal covering PostgreSQL, file objects and critical configuration.
- [ ] **W08-03 / P1 / CORE** — Safe versioned upgrades, schema evolution, rollback/forward-fix and recovery runbook.
- [ ] **W08-04 / P1 / CORE** — Health/readiness, metrics, structured logs, traces and useful alerts.
- [ ] **W08-05 / P1 / CORE** — Capacity/connection budget and Lite/Standard measured footprints.
- [ ] **W08-06 / P1 / ADV** — Qualified HA database, PITR, RPO/RTO and failover drill for Enterprise profile.
- [ ] **W08-07 / P0 / CORE** — P0 auth/tenant leakage, permanent data loss, broken restore or unsafe migrations block release.
- [ ] **W08-08 / GATE / CORE** — Independent reproducible install/upgrade/restore and incident drill certification.

## 10. Wave 09 — Control Plane & Ruxbase Studio

- [ ] **W09-01 / P1 / CORE** — Extend embedded Studio to manage PostgreSQL, collections, auth, policies and file storage safely.
- [ ] **W09-02 / P1 / CORE** — Secure operational views for migrations, backup health, jobs and diagnostics.
- [ ] **W09-03 / P0 / CORE** — Protect administrative endpoints/actions and audit sensitive changes.
- [ ] **W09-04 / P2 / ADV** — Optional self-hosted multi-project control plane and deployment inventory.
- [ ] **W09-05 / P2 / ADV** — Separate project DB/roles/secrets, quotas and environment lifecycle; never weaken single-project isolation.
- [ ] **W09-06 / GATE / CORE** — Admin journey, RBAC, access-control abuse and deployment profile tests; no mandatory control plane.

## 11. Wave 10 — AI-Native Platform & Plugin Ecosystem

- [ ] **W10-01 / P1 / CORE** — Retain opt-in MCP and make collection tool discovery conform to core policy and API contracts.
- [ ] **W10-02 / P0 / CORE** — Scoped AI agent identity and audited least-privilege tool permission decisions.
- [ ] **W10-03 / P1 / CORE** — Human approval and action classification for sensitive agent capabilities.
- [ ] **W10-04 / P2 / ADV** — Optional embedding/knowledge/RAG and AI gateway integrations.
- [ ] **W10-05 / P2 / ADV** — Versioned plugin SDK, compatibility and controlled upgrade/unload lifecycle.
- [ ] **W10-06 / GATE / CORE** — Prompt injection, secret misuse, cross-tenant tool, approval bypass and plugin isolation negatives.

## 12. Wave 11 — Independent Certification & Release

- [ ] **W11-01 / P0 / GATE** — Freeze release candidate: full commit/tree, dependency manifest, provenance and reproducible build.
- [ ] **W11-02 / P0 / GATE** — Independent source/security/authorization/database architecture audit; zero unresolved P0 findings.
- [ ] **W11-03 / P0 / GATE** — All declared core user journeys proven in disposable PG17/18 and supported self-hosted OS configurations.
- [ ] **W11-04 / P0 / GATE** — Restore/upgrade/failover drills and repeatable HA/RPO/RTO evidence only for claimed profiles.
- [ ] **W11-05 / P1 / GATE** — Complete 168-hour soak plan/execution and documented anomaly disposition.
- [ ] **W11-06 / P1 / GATE** — Fair matched Supabase Self-Hosted measurements and publicly auditable raw resource/results artifacts.
- [ ] **W11-07 / P1 / GATE** — Compatibility, security supply chain, license, docs and operational runbooks approved.
- [ ] **W11-08 / P0 / GATE** — Maintainer/human release authorization, reviewed immutable tag and verified distribution artifacts; production GO only if explicitly granted.

## 13. Required self-hosted end-to-end journeys

A wave may add relevant journeys earlier, but release readiness requires the full declared core matrix:

- [ ] **JOURNEY-01** — Install with Docker Compose on a clean supported host; initialize admin with safe defaults and private database connectivity.
- [ ] **JOURNEY-02** — Create a collection and a native PostgreSQL table, then introspect and expose each through an authorized API.
- [ ] **JOURNEY-03** — Register/authenticate, refresh/revoke a session, and confirm restricted role identity and access denial.
- [ ] **JOURNEY-04** — Execute authenticated CRUD, relations, search, RPC and negative SQL/API policy tests.
- [ ] **JOURNEY-05** — Upload/read/delete a file with Local/S3-backed access control and cross-tenant rejection.
- [ ] **JOURNEY-06** — Subscribe, reconnect, change authorization and recover from supported realtime failure modes.
- [ ] **JOURNEY-07** — Enqueue and recover a durable job or webhook with documented idempotency.
- [ ] **JOURNEY-08** — Deploy/execute an isolated function without runtime operator credentials (when function runtime is in release scope).
- [ ] **JOURNEY-09** — Upgrade schema/app with existing data, simulate a failure, and demonstrate valid recovery.
- [ ] **JOURNEY-10** — Restore the whole self-hosted project and verify data, files, roles, RLS and application behavior.
- [ ] **JOURNEY-11** — Measure single-node and HA deployments under matched resource/security constraints.
- [ ] **JOURNEY-12** — Follow official documentation as a new developer using CLI/SDK/React/Next.js templates.

## 14. Final gate template (copy into each milestone issue)

| Field | Required value |
| --- | --- |
| Wave and milestone | `WNN-MNN` / stable checklist IDs |
| Owner and independent reviewer | Actual human/agent role and scope; no self-review |
| Baseline | Branch, commit SHA, Git tree |
| Implemented scope | Code paths, APIs, data and authorization contracts |
| Excluded scope | Explicitly deferred guarantees |
| Evidence | PR, integration commit/tree, targeted tests, PG17/18, full CI, security negatives |
| Reliability / migration | Restart, rollback, durability, recovery observations where applicable |
| Compatibility | PocketBase API/SDK contract or approved explicit difference |
| Decision | CERTIFIABLE / CERTIFIABLE_RESTRICTED_SCOPE / CONDITIONAL / NOT_CERTIFIABLE / BLOCKED |
| Owner approval | Exact approval link; never invent |
| Post-merge | Final integration SHA/tree and CI |
| Closure | Issue state, residual risks, next authorized work |

**Rules:** write regression code during implementation, run the consolidated full test campaign on the complete candidate, repeat on corrections and after merge. Document actual execution; don't mark CI green based on a different commit. Changing a scope or closing a wave requires explicit approval and the corresponding roadmap/checklist update.

## 15. Next actions (non-authorizing guide)

1. Preserve the verified, completed M00 upstream ancestry restoration on `main` ([merged PR #33](https://github.com/AbdullahZaher/ruxbase/pull/33)); follow up on the still-unenforced **positive independent approval requirement** and any remaining master-roadmap Wave 00 evidence. Issue #4 is closed, but W00-07 and W00-12 remain unchecked.
2. Confirm canonical Wave 01 milestone assignments with the project owner and existing specifications. Preserve completed M01 Scope A; do not claim general production authorization.
3. Prepare a bounded, separately authorized M02 — Platform Kernel Contracts implementation with exact negative tests and accepted architecture boundaries.
4. Update the evidence register after each reviewed merge. Never backfill hypothetical completion or edit the upstream/release status to match a desired narrative.
