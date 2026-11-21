# Developer Experience — Batch 02: Production-Grade Developer Tooling

**Baseline:** main `e2ee0c2be543ce26450723e9c0c6cb789d6852bd`, Git tree `ba7e020aca937be985b89a31d3567baaf7db1db6`. Scope remains developer tooling, template authentication examples, documentation and CI. No Go service, database schema/migration or PostgreSQL authorization changes. Production deployment authorization is **not** part of this batch.

**Reconciled status (2026-11-21):** Engineering delivered and merged in [PR #35](https://github.com/AbdullahZaher/ruxbase/pull/35) on 2026-11-21. The seven required post-merge workflows succeeded on `5157c50a291617a260a9c4cd203f7156adef04a3`, and the reviewed development-documentation deployment workflow succeeded for that commit. The acceptance gates below record the original candidate contract, rather than an open implementation backlog. Formal documentation milestone closeout remains a separate maintainer decision; external paid-model evaluation was not run and is not claimed. See [current Developer Experience status](developer-experience-status.md).

## Deliverables and source of truth

| Capability | Source | Executable acceptance |
| --- | --- | --- |
| Reproducible React / Next.js | `starters/{react,nextjs}/package-lock.json`, `scripts/check-starter-locks.mjs` | Lock v3/root sync/registry integrity; generated projects installed with `npm ci`, audit, actual framework build and Chromium |
| Safer human/agent CLI | `scripts/create-ruxbase-app.mjs` | `--help`, `--list`, named and positional flags, `--dry-run`, `--json`, invalid names/options, symlink checks, exclusive mkdir and no-overwrite/cleanup |
| Local onboarding diagnostics | `scripts/ruxbase-doctor.mjs` | Read-only loopback readiness and example collection test, URL SSRF refusal, structured JSON, local disposable HTTP unit test |
| Protected Next.js cookie sessions | `starters/nextjs/src/server/session.mjs`, `app/api/session/**/route.js`, `app/protected/page.jsx` | Real backend authRefresh validation on each request, request-scoped SDK, HttpOnly/SameSite/Secure cookie, origin/Fetch-Site and JSON POST enforcement, no-store, 32 concurrent mixed user calls, owner-only records and SSR |
| AI actual-model evaluation | `scripts/eval-ai-projects.mjs`, `starters/ai-model-tasks.json`, `docs-ai-model-eval.yml` | Offline allowlist and prompt-injection fixture tests in required CI; separately **opt-in** real-model plan plus generated-app e2e only with owner-selected model/API key |
| Developer and AI docs | `getting-started/developer-tooling.md`, `tutorials/nextjs-protected-sessions.md` | 48-page public corpus, unchanged existing AI bundles, six new tooling lexical retrieval questions with threshold >=5/6, no critical failures |

## Engineering boundaries and threat model

- The generated projects use the real PocketBase JS SDK against Ruxbase and are installed from approved npm tarballs with checksummed lock metadata. Exact lockfiles freeze transitive selection; they do not make the npm registry, dependency post-install scripts or external supply chain inherently trustworthy. Record `npm audit` separately from integrity verification.
- The generator is distributed **inside the source checkout**, not as a published npm CLI. Do not tell developers to run `npm create ruxbase` until such a package has been independently released and provenance-verified.
- The protected Next.js route handler is a **bounded example**. It validates the user bearer cookie against the server using `authRefresh()` and never shares the user SDK across requests. Cookie auth endpoints require trusted `RUXBASE_APP_ORIGIN` and same-origin JSON POSTs; the server-facing `RUXBASE_INTERNAL_URL` is configuration, never user-supplied. In public production, HTTPS, trusted reverse-proxy semantics, rate limits, monitoring, token revocation policy, CSP and MFA/OAuth require independent review.
- Logout clears the browser cookie. **It does not revoke a copied Ruxbase bearer token at the backend.** The demo must never be represented as proof of stolen-token revocation or production readiness.
- The required CI never calls an external paid LLM or executes untrusted model-written commands. The optional manually triggered workflow selects a real model through a secret/API variable, validates `tool/framework/name` only, and then compiles/runs the selected approved scaffold in an independent job without the model API secret. This measures bounded model tool selection and scaffold correctness, **not** arbitrary free-form source generation or generalized prompt-injection security. A real-model outcome is **NOT RUN** until the explicit dispatch succeeds.
- All Docker/PostgreSQL tests use an isolated CI-owned project and named volumes, then delete **only their own disposable test data**. Existing source checkout, main branch and production are not modified.

## Acceptance gates (original candidate contract)

1. Exact candidate provenance (full SHA and tree); source and package-lock files preserved by Git without a workflow that can mutate the branch.
2. `docs-starters`: offline CLI/doctor/AI-plan tests, real `npm ci` for generated React+Next, `npm audit --omit=dev --audit-level=high`, two real builds, Chromium login/theme/owner-draft isolation, Next.js protected-session negative tests and Compose container integration.
3. Existing `ci` (PG17/PG18, release snapshot), `docs-foundation`, `docs-brand-review`, `docs-quickstart`, `docs-build-candidate`, `docs-performance` all succeed on **same exact candidate**; no thresholds weakened.
4. Documentation corpus and topic bundles remain bounded; all existing lexical retrieval thresholds retained and an extra six tooling questions pass >=5/6 with zero critical misses.
5. Human maintainer independent review before leaving Draft/merging; recheck all seven exact-main workflows after merge, then reviewed manual GitHub Pages deployment with exact reviewed main SHA, published manifest and hash verification.
6. Do not mark external model evaluation PASS without a successful explicit `docs-ai-model-eval` workflow and retained actual response/CI evidence.

## Non-goals / residual risks

No shipped npm generator package or remote install mechanism; no generic React server-side auth implementation; no fully certified production session revocation, MFA/OAuth or threat-model review; no independent multi-model benchmark or code-writing LLM evaluation; no declaration that green development CI authorizes production. Errors and actual results are recorded in the PR, not inferred from this file.
