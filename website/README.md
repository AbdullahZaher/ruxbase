# Ruxbase documentation website

The [official development documentation](https://abdullahzaher.github.io/ruxbase/) is live on GitHub Pages. See the [publishing and discovery runbook](../docs/architecture/documentation-publishing.md), [initial publication evidence](../docs/reports/documentation-publication-2026-11-20.md) and [architecture plan and implementation history](../docs/architecture/documentation-platform.md).

## Source and build

Astro 7.4.13 and Starlight 0.43.1 are locked in package-lock.json. Use the pinned CI Node toolchain, currently 25.2.1 (package minimum 22.23.4). Work in a clean reviewed checkout; build sealing rejects dirty tracked source. Public Markdown is staged from docs/public/manifest.json and authoritative guide files. Brand assets follow assets/brand/documentation-brand.json. Do not edit generated content or dist.

From the repository root:

```sh
npm --prefix website ci
npm --prefix website run test:foundation
npm --prefix website run test:ai
npm --prefix website run build
```

From website/ after building:

```sh
node scripts/check-build.mjs
node scripts/check-ai.mjs
npm run test:retrieval
bash scripts/validate-openapi.sh
node scripts/check-publication-scope.mjs
python3 tests/test-recover-docs-snapshot.py
python3 scripts/rehearse-docs-recovery.py
CHROME_BIN=chromium node scripts/browser-smoke.mjs
bash scripts/run-lighthouse.sh
node scripts/release-artifact.mjs verify "$(git rev-parse HEAD)"
```

CI supplies the digest-verified axe scanner for representative accessibility checks and the compatible browser/toolchain. Use the workflow commands and permissions as authoritative. The independent OpenAPI validator pins dependency versions, not all wheel hashes. Retrieval is a deterministic 30-question page-retrieval proxy, not a model-answer benchmark. Lighthouse is lab evidence, not real-user metrics. Local browser runs use the sandbox by default.

## Publication scope

The default build contains static HTML, Markdown siblings, Pagefind, canonical SEO metadata, sitemap, llms.txt/full, OpenAPI, API coverage, CLI/configuration/feature references, version catalog and an artifact manifest. The manifest seals every output file except itself. Development documentation does not imply a stable release or production certification. The bounded route inventory represents 57 HTTP operations in OpenAPI and three MCP transport methods in a separate guide; schemas and runtime contracts remain scoped.

Scalar 1.69.2 is an optional **local-only** preview: use `npm run build:scalar-preview` and `DOCS_SCALAR_PREVIEW=1 node scripts/browser-smoke.mjs`. Never upload the preview to Pages; bundled license/advisory review is unresolved. The static publication guard enforces exclusion. Historical implementation notes remain in Git history and the architecture plan.

## Deploy and recover

Use the reviewed main-only docs-deploy workflow after all six exact-main push workflows succeed. The public site can intentionally lag main; check [version.json](https://abdullahzaher.github.io/ruxbase/version.json). Deployment verifies the published manifest and representative file digests. Retain reviewed snapshots and external digests beyond the 30-day CI retention period. For local recovery use scripts/recover-docs-snapshot.py with trusted archive/manifest SHA-256 and expected source commit; actual remote rollback requires separate authorization. See the publishing runbook for owner-side search verification and maintenance.
