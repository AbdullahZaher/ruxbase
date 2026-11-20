# Development documentation publishing

The public site has not been deployed yet. The workflow is deliberately manual
and main-only, and does not create a release or promote development docs to stable.

Before first dispatch: complete the outstanding brand/content review and browser
checks, obtain maintainer approval of PR and release scope, confirm full CI on
the merged main commit, then set Settings > Pages > Source to GitHub Actions.
Restrict the github-pages environment to main and require a reviewer where
available. The workflow needs the full reviewed main SHA as input. It builds
from that SHA, tests the static artifact, and grants Pages/OIDC authority only
to the deployment job. No PR can deploy. The published version.json must match
the input and the smoke probes must pass. If the run fails, inspect the actual
deployment state rather than assuming rollback. Rebuilding a known-good source
and rollback rehearsal are still required before release certification.

Chromium smoke runs with no-sandbox only on disposable GitHub-hosted CI for
public static documentation, without production credentials. Local runs use
the sandbox by default. This is a functional smoke, not full accessibility,
visual equivalence, security or browser-matrix certification.

The integrated vector symbol and favicon are review candidates: owner visual approval is required before publication. Scalar distribution approval is additionally enforced by check-scalar-release-review.mjs; missing archive notices or unresolved bundle provenance must not be treated as cleared.

## Snapshot identity and recovery

The root remains development-only. `versions.json` records development and explicitly null stable, with no historical releases. No /next or /vX routes are implied. A clean build seals `artifact-manifest.json` with sorted paths, sizes and SHA-256 per file, excluding itself. Verify against the exact checkout SHA before upload. CI retains candidate snapshots for 30 days; authorized deployment retains the reviewed snapshot and checks the published manifest digest against the build job output plus nine representative file hashes. This is integrity verification, not a signed provenance attestation.

For recovery, retrieve the previously reviewed snapshot and its independently retained manifest digest, verify source identity and every file, and obtain approval to redeploy via a trusted main-only recovery procedure. Do not repoint the public site to an unreviewed PR artifact. Local A-to-B-to-A atomic activation of two actual CI archives passed; see evidence/docs-rollback-local-2026-11-20.json. This rehearses byte-exact restoration, not the GitHub Pages deployment mechanism. Historical release publishing, durable retention beyond artifact expiry and actual Pages recovery remain unimplemented. No stable release is authorized.

## Initial public scope decision (2026-11-20)

Delegated brand review approved integrated SVGs, favicon and palette. The initial development documentation profile includes static HTML/Markdown, OpenAPI, Pagefind and AI references only. Scalar's unresolved bundle notice/dependency review is not waived: the default build does not pack or distribute Scalar, and the public-scope gate rejects viewer content before sealing or deployment. A separate `npm run build:scalar-preview` is available for local review, with `DOCS_SCALAR_PREVIEW=1` for viewer-specific checks. Never publish that artifact. Restoring Scalar requires separate completed distribution review and a reviewed publication-scope change. No stable releases or historical routes are claimed. Require exact new CI, merge and Pages settings before public deployment.

All six required checks now run on pushes to main, and deployment reads their latest push-triggered result for the reviewed exact SHA. It fails closed on absent, pending or failed checks. Pages authority remains confined to the deployment job. No public deployment can proceed until repository Pages settings and main CI are actually available. The six SVG assets are now retained as source-controlled brand files independent of expiring CI artifacts.

Main advanced during review. The documentation branch integrates main's canonical version 1.1 master artwork and keeps main's complete AGENTS.md. Current documentation-brand.json is the public approval record. The current site uses horizontal master SVG and dedicated 16/32 px favicons; prior flat trace remains archived. The static-only publication profile and all six exact-main CI checks remain required.
