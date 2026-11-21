---
document_id: RUXBASE-AI-EXECUTION-PROTOCOL
version: 1.0
audience: coding-agents-reviewers-planning-assistants
status: proposed-repository-instruction
master_roadmap: ../../MASTER-ROADMAP.md
master_checklist: ../../MASTER-CHECKLIST.md
---

# AI Execution Protocol — Persistent Ruxbase Project Context

This document makes Ruxbase's long-horizon engineering plan **easy to resume across new conversations**. Repository files are durable context; a chat's memory or a previous assistant's conclusion is **not** authoritative.

## 1. Start-of-conversation reading order

For every request involving project strategy, architecture, implementation, roadmaps, milestone status or certification:

1. Read repository-root [AGENTS.md](../../AGENTS.md), then relevant nested agent instructions.
2. Read [MASTER-ROADMAP.md](../../MASTER-ROADMAP.md) for the permanent product direction, wave structure and release boundaries.
3. Read [MASTER-CHECKLIST.md](../../MASTER-CHECKLIST.md) to identify stable IDs, known acceptance evidence and open work.
4. Read the current accepted milestone specification, tracked issue(s), relevant ADRs, and code/tests.
5. Fetch live GitHub branch HEAD/tree, PR, review, ruleset and CI state. Reconcile with the checklist snapshot before claiming any item complete.
6. Read only the relevant implementation docs (`docs/RLS.md`, `docs/COMPATIBILITY.md`, `docs/OPERATIONS.md`, `docs/CLUSTER.md`, `docs/REALTIME.md`, etc.) and current published AI documentation if appropriate.

If files cannot be accessed, disclose the limitation and do not invent their contents.

### One-paragraph product frame for agents

> Ruxbase is a self-hosted, MIT, PostgreSQL-native, Go modular-monolith BaaS based on the official PocketBase upstream. Its competitive **reference is Supabase Self-Hosted, not Supabase Cloud**. Preserve Collections-first and add authorized SQL-first through shared platform/security contracts. Keep a single-binary core with external PostgreSQL for normal deployments and optional workers/storage/control plane. No Popgres upstream. Production NO-GO pending full independent release certification.

## 2. Do not confuse planning levels

`Wave 00–11` are the **twelve master roadmap waves**. `Wave 01` has its own **M01–M07 internal increments**. The phrase “M02 Platform Kernel Contracts” is **not** “Wave 02 PostgreSQL Native Engine.”

- M00 / Wave 00: foundation; [Issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4), which must be checked live.
- Wave 01 / M01: closed for **restricted Scope A** in [Issue #9](https://github.com/AbdullahZaher/ruxbase/issues/9); *not* a production grant.
- Wave 01 / M02: Platform Kernel Contracts, pending separate accepted authorization.
- Labels of the remaining Wave 01 increments have varied in historical reports. Reconcile authoritative decisions before implementing; do not silently rename or widen scope.
- Subsequent Waves 02–11 are deliverables, not claims that code is already written.

A proposed milestone in the roadmap cannot override an already-approved narrower issue specification. Escalate a conflict explicitly.

## 3. Source-of-truth and provenance hierarchy

Use the highest available evidence applicable to the claim:

1. Actual source and tests on an exact commit, observed behavior, valid Git graph.
2. Owner-approved milestone criteria, security boundaries, maintained ADR and release gates.
3. GitHub CI and signed/reviewed merge evidence at that exact SHA/tree.
4. Current approved repository documentation and tracking issues.
5. Master checklist for the accepted progress summary.
6. Historical reports and prior conversations as context only.

For execution status, prefer actual linked evidence over checkbox text; for future direction, prefer the approved master roadmap over ad hoc suggestions. A documentation snapshot on `main` may differ from active `wave/*` code.

## 4. Every task must have an execution envelope

Before implementation, capture:

| Field | Required |
| --- | --- |
| Goal | Exact user journey, change request or finding |
| Planning IDs | `WNN-XX` or `WNN-MNN-XX` checklist items |
| Branch | Exact base ref and commit/tree; branch protection strategy |
| Scope | Required behavior and explicit exclusions |
| Dependencies | Preconditions already met or genuinely blocking |
| Security contract | Caller, tenant, PostgreSQL identity, grants, RLS and trust boundaries |
| Data contract | Transaction boundary, retry/idempotency, migrations, event durability |
| Test plan | Positive/negative tests and applicable PG17/18, compatibility/SDK, OS and CI checks |
| Owner/reviewer | Real implementation authority, independent review, owner actions |
| Done definition | Code + tests + documentation + exact-head CI + reviewed merge/postmerge where required |

Implement only authorized work. Do not start unrelated waves to fill idle time. Keep M01 Scope A completion intact unless demonstrated regression.

## 5. How to update the checklist

Each checklist item has a stable ID. Use the exact ID in PRs, issue descriptions, evidence, and status updates.

**Rules to mark `[x]`:**
- Functionality exists in the target integration branch and satisfies the item's explicit scope.
- Applicable tests actually passed on the exact candidate and integration commit, or accepted evidence is linked.
- Required independent/human approvals are recorded, not impersonated.
- Exceptions, excluded guarantees and unresolved risks are stated.
- The evidence register contains traceable GitHub/source references.

**Rules to leave `[ ]`:**
- Planned, partial, blocked, deferred, untested or awaiting signoff.
- Code exists only on an open PR.
- CI passed but milestone acceptance is still absent.
- A stage was certified only for restricted Scope A while the item asks for production/general scope.

Do not use percentages based on checkbox counts as product readiness; not every item has the same risk or scope.

For a status change, update the row and evidence register in the same reviewed PR, then check live issue state again. Do not rewrite past certification artifacts to conceal earlier findings.

## 6. Testing, implementation and evidence

- Write all relevant code and test cases first when working on a large batch, then execute the comprehensive test campaign on the complete candidate, unless an earlier targeted check is required to safely proceed.
- Run the repository-defined real checks, using disposable PostgreSQL 17 and 18 for DB tests; use `make test-accept`, SDK/UI/strict RLS and CI jobs as appropriate.
- Run targeted regressions after fixes and full mandatory gates again on the final candidate. Never hide failures by modifying expectations or raising timeouts without an actual reason.
- Record `PASS`, `FAIL`, `NOT RUN`, `SKIPPED` or `BLOCKED` precisely; include SHA, job IDs and limitations. An attempt failure and successful rerun are distinct facts.
- Do not merge to `main` or `wave/*` directly; use reviewed PRs and the existing [branch strategy](../BRANCH-STRATEGY.md).
- Never self-issue independent certification. The proposed future **A11** role is independent; older reports calling an auditor **Agent 06** must retain their actual history.
- No production, external privilege alteration, branch ruleset mutation, destructive migration, force push or release action without applicable owner authorization.

## 7. When comparing with Supabase

- Use **official Supabase Self-Hosted** (Docker) only. Distinguish baseline services from optional modules, Cloud-only offerings and provider-operated features.
- Compare *end-to-end* Ruxbase+PostgreSQL+enabled adapters against the *equivalently enabled* Supabase self-hosted stack on constrained matching hardware.
- Treat memory/CPU savings, deployment time, availability and recovery objectives in the roadmap as **unverified targets** until raw reproducible test evidence exists.
- Explain whether a capability is currently implemented, limited, planned core, or optional advanced; do not mark a target as available.
- Favor simplicity, secure default operation, compatibility and recovery over copying service topology, GraphQL, SaaS billing or cloud control plane.

## 8. Short AI handoff template

Copy this into a PR/issue or a new-chat status note *after verifying against GitHub*:

```text
RUXBASE PROJECT HANDOFF
Roadmap: MASTER-ROADMAP.md (12 waves, Supabase Self-Hosted only)
Checklist: MASTER-CHECKLIST.md (stable IDs)
Agent guide: AGENTS.md + docs/roadmap/AI-EXECUTION-PROTOCOL.md
Target: <wave/milestone IDs>
Base branch / SHA / Git tree: <verified live values>
Current implementation scope: <supported and excluded behavior>
Acceptance criteria: <exact checklist IDs and approved issue/spec>
Verified results: <commands, PG versions, CI run, status, candidate SHA>
Unresolved blockers: <IDs, severity, owner, reason>
Independent review: <evidence or pending>
Next authorized action: <bounded implementation or closure>
Production: NO-GO unless separately certified by owner
```

## 9. New-conversation answer discipline

- If asked **“What next?”**, read the current checklist, verify live state, identify the first unmet authorized dependency, and propose/execute only that bounded work.
- If asked to **implement**, inspect current source, implement with tests and open a PR; avoid planning-only output while implementation is feasible.
- If asked to **review**, distinguish verified evidence, inferred risk and speculation. Cite current code/CI before declaring a milestone accepted.
- If asked for a **prompt**, include the relevant planning IDs, exact repository links and immutable starting evidence, with explicit approval/production boundaries.
- If asked about **competitiveness**, compare with Supabase Self-Hosted rather than commercial features; distinguish parity and optional future advantages.

**Maintenance:** Any change to the approved product direction, wave scope, security invariants or acceptance protocol requires synchronized updates to roadmap, checklist and (if persistent agent behavior changes) root AGENTS.md in a reviewed PR.
