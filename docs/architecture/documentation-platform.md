# Ruxbase documentation platform — implementation plan v1.1

Status: approved for implementation by the project owner; publication gates open.
Baseline: 0de513e09c35763f2e1814f4b99e8f42d955753c (tree 57dba8a3da1d5041877763af7157ef9992a06cd2).
Integration: `chore/documentation-platform` → `main` via reviewed PR, per [branch strategy](../BRANCH-STRATEGY.md). No production release authorization is implied.

## Decisions

- Astro Starlight, static HTML, Pagefind, npm with a committed lockfile, GitHub Pages from GitHub Actions.
- Existing repository Markdown remains authoritative. Use an explicit public-source manifest; do not duplicate existing guides or publish engineering reports. Preserve existing source paths. New public narrative lives in docs/public. The website build stages selected content and assets into ignored directories.
- English first, links and slugs stable; no custom domain purchase assumed. Initial site/base: https://abdullahzaher.github.io and /ruxbase/. A future verified custom domain removes the base. Validate both modes before enabling domain migration.
- Preserve the three existing brand PNGs in place and the R geometry and uppercase wordmark. Neutral light/dark design, blue interactive accent subject to contrast tests. No redesign, no raster embedded in a file represented as vector. Export inspected genuine vector artwork and optimized raster derivatives only after visual equivalence review. A temporary original PNG is acceptable for the development preview, not the final asset gate.
- No GitHub Releases returned at baseline. Until an approved Ruxbase release exists, root is explicitly development documentation. Do not infer release approval from the inherited changelog. Later promote a verified release snapshot to root, publish main at next/, retain selected release snapshots under versions/, and associate every output with exact source commit. Development/archival indexing policy is explicit; do not block crawler access to pages that need noindex.
- Markdown exports, llms.txt and bounded per-topic/full bundles are generated from the published source set only. Prefer one generator, no manual copies. llms.txt is optional ecosystem guidance, not a search ranking guarantee. No training crawler policy is assumed beyond owner decision; project Pages cannot govern origin-root robots.txt.
- OpenAPI 3.1 covers verified platform REST contracts, not hypothetical user-specific collection fields. Scalar is a viewer; provide static HTML reference content as well. SSE, MCP and dynamic schemas need separate explanations. Derive CLI/config references from verified definitions; no invented endpoints or capabilities.
- PocketBase remains sole ongoing upstream; no new runtime dependencies or changes to application behavior for docs.
- No hosted chatbot, vector search service, separate docs repo, public development PR previews or new documentation MCP service in this scope.

## Batch 00 — brand and design foundation

B00.1 inventory originals and record dimensions, Git blob IDs and intended roles.
B00.2 define shared neutral palette and theme tokens, provenance and usage rules.
B00.3 create true vector equivalents and minimal favicon variants; keep original PNGs unchanged.
B00.4 validate small-size 16/24/32 px, light/dark backgrounds, contrast, geometry, asset size and use in docs header. Require maintainer visual review; no invented sign-off.

## Batch 01 — content and site foundation

B01.1 scaffold Starlight and establish source staging; keep generated files out of Git.
B01.2 pin compatible runtime/package versions; generate lockfile using registry and verify npm ci on a clean checkout. Review dependencies and audit findings.
B01.3 inventory and migrate public docs through manifest: DEV, COMPATIBILITY, CLUSTER, RLS, REALTIME, SEARCH, VECTOR, MCP, OPERATIONS, MIGRATING, selected README and upstream policy sections. Rewrite internal links including relative GitHub source paths with a Markdown-aware transform; preserve anchors, verify all links. Do not publish unreviewed text. No manual duplicate copy.
B01.4 build tested Quickstart, navigation, Pagefind, mobile/header/footer and source links. Make documentation CI required. Test base path, no-JS document content, search and accessibility.
B01.5 configure Pages Actions source, restricted deployment environment, pinned Actions, and publish development preview with clear version status. Smoke test exact artifact and only then link README/repository website.

## Batch 02 — search and AI

B02.1 semantic HTML, unique titles/descriptions, canonical IDs, sitemap, social preview and accessible navigation. Verify crawler policy with owner; register site in search console when authorized.
B02.2 generate per-page Markdown, llms.txt, bounded full/topic context, version.json and verified feature manifest; exclude internal sources and any unreleased claims from release outputs.
B02.3 inventory REST routes, create validated scoped OpenAPI contract and readable HTML/Scalar reference; disclose coverage and unsupported contracts. Document SDK compatibility, SSE, MCP and errors from evidence.
B02.4 AGENTS.md and Using Ruxbase with AI; validate machine links, content parity, version and retrieval benchmark (30 evidence-based questions, target 90% correct, no critical errors). External model scores are advisory, not guaranteed.

## Batch 03 — release integration and quality

B03.1 design immutable release snapshot assembly: stage approved tag and current next together; atomic Pages artifact, one authorizing deploy job, concurrency protection, rollback to retained trusted artifact. No automatic tag creation.
B03.2 derive CLI/config references and test examples in disposable development environment; verify documented API coverage against route inventory.
B03.3 implement docs CI and contributor rules: internal links, schema drift, metadata, content parity, accessibility, performance, secret checks, source allowlist, version isolation. Reuse existing pinned Actions policy; no PR deployment credentials.
B03.4 post-deploy smoke (HTML without JS, Markdown, search, sitemap, canonical URLs, OpenAPI, status), rollback rehearsal and evidence report.

## Acceptance

Zero broken internal links, missing required metadata, critical accessibility findings or unapproved published features; exact source/release match; 100% HTML/Markdown parity for published docs, 100% of declared public REST scope covered (inventory denominator and exclusions explicit); tested examples and CI green on candidate. Performance targets Lighthouse >=95 for representative pages in controlled tests, field LCP <=2.5s, CLS <=0.1, INP <=200ms when field data exists. Search indexing and model selection are external outcomes, not release guarantees.

## Checkpoint

Initial changes: source inventory, brand manifest, original-asset validation and staging, Starlight candidate scaffold, development homepage and dependency-free foundation tests.
Not complete: full brand export/review, npm lockfile and actual Astro build, source migration/link validation, AI/SEO, API coverage, Pages configuration/deployment, full CI and review. No claim of production readiness. Never promote a foundation check to a full docs release gate.

### Batch 00/01 increment 2

Ten existing public guides are explicitly mapped without moving or copying their authoritative sources. Candidate SVG symbol variants and favicon are created; visual sign-off and complete lockup/wordmark exports remain open. The read-only candidate build workflow resolves an uncommitted lockfile and retains it, installs dependencies, and verifies static output. This is an interim build experiment, not the reproducibility or deployment gate.

### Batch 02 increment — AI and SEO foundations

The prior immutable build 73f569e4e963b45a86a22c87924f02fdba0ee6fa passed npm ci, audit, six tests, Astro build, 12 pages and Pagefind (Actions 44726574375). Added a bounded Markdown/AI export pipeline, per-page discovery metadata, version and documentation manifest, AI guide and AGENTS.md; static checks cover expected HTML/Markdown pairs, canonical, metadata and sitemap. No verified feature registry, OpenAPI/Scalar, crawler registration, external AI benchmark, visual approval or Pages deployment is claimed. The initial development root is the sole documentation candidate and remains explicitly development; when stable documentation is introduced, next/ indexing and retention will be decided explicitly. No origin-root crawler policy is set by this repository. Publication remains gated.

AI build evidence: 44727412875 passed on af2b4191e1f0e70be05f40f8ff904b00908de5a0, generating 13 HTML/Markdown pairs and validating sitemap, metadata, AI links and exact checked-out identity. Foundation workflow 44727412925 failed because its wildcard included dependency-requiring AI tests without npm ci. The foundation workflow and script now explicitly select the six dependency-free tests; AI tests remain in the installed full-build workflow. Re-run both checks on the new ref before acceptance.
