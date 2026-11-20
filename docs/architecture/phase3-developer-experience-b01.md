# Phase 3 Developer Experience — Batch 01

Baseline main: `790e41f8a1c2b60428ba35e9aa3c62bfa379f223`, tree `6d9505bc5fc4b702da1acebbf1580d95903160db`. Scope is additive docs, templates, scaffold tool, independent CI and website visual presentation. No API/DB runtime changes, production role changes or release authorization.

## Deliverables

- Two real frontend starter source folders `starters/react` and `starters/nextjs`, pinned direct React 19.3.1, Next.js 16.4.6, Vite 8.3.8, PocketBase SDK 0.28.1. React uses a Vite `/api` proxy; Next.js uses a trusted-origin App Router rewrite; no superuser credentials or DB URLs exposed. Both render the approved horizontal black/white SVG logo, true-black night mode, white light mode and visible setup errors.
- Browser session is in-memory; on reload user signs out. Owner-only draft flow depends on the documented Ruxbase `posts` schema/rules. Neither sample represents a production-ready user session, OAuth/MFA, CSRF protection or tested server-authenticated SSR.
- Docker Compose overlays under each starter reference the **repository root Compose stack** with PostgreSQL 18 and pgvector. They are for in-repository local development. A generated project runs standalone with npm against an already-running backend; it cannot build the Ruxbase backend out of repo without Ruxbase source.
- Deterministic, shell-free `scripts/create-ruxbase-app.mjs` generator for safe npm package names; refuses overwrite/path traversal, copies only expected files, generates no secrets. `starters/ai-tasks.json` is an executable task specification, not an assertion of open-ended LLM autonomy. It provides AI agents a reliable action to create working scaffold source.
- Docs website monochrome: Starlight uses white/light and true-black dark surfaces, neutral accents and canonical logo variants. Brand master assets and approved artwork digests remain unchanged.
- Public `getting-started/create-app` and React/Next tutorial pages, referenced by existing integration guides and AI topic bundles, with explicit limitations and troubleshooting.

## Verification

A dedicated `docs-starters` workflow must compile both generated apps in their actual toolchains and boot both against a disposable root Docker Compose Ruxbase/PostgreSQL service. It provisions an exact posts schema and two ordinary users. Chromium drives login, draft creation, logout and both theme modes; API checks verify the draft remains hidden from another user and guests. Compose overlays are parsed and checked for loopback ports, private internal API target and readiness dependencies. Both overlays are then started as real frontend Docker containers; readiness, live API proxy and initial browser entrypoint are checked before their disposable volumes are deleted. No production creds or repositories are contacted. The test is destructive **only** to the ephemeral CI-owned Compose project and its named volumes.

Add authored AI starter retrieval questions as a separate 6-query proxy with 5/6 minimum and zero critical failures. Preserve all earlier retrieval, build sealing, site browser, accessibility, performance and licensing gates. The lexical retrieval benchmark and deterministic generator test do not measure arbitrary model-generated application quality.

## Release boundaries

Verify Git SHA and exact tree, pass 7/7 workflows (including the new starter CI), owner review before merge, all exact-main gates after merge and reviewed manual docs-deploy. Do not publish a stable version or claim production correctness from local starter acceptance. Lockfile reproducibility may need a post-CI dependency freeze and review.
