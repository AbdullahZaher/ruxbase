# Ruxbase documentation candidate

See [the master plan](../docs/architecture/documentation-platform.md).
This scaffold is a development preview candidate, not deployed or certified.

Run dependency-free foundation checks from the repository root:
```sh
node --test website/tests/foundation.test.mjs website/tests/legacy-links.test.mjs
node website/scripts/prepare.mjs
```

The scripts verify original brand PNG bytes and stage only explicitly approved
public Markdown. Existing documents stay at their original paths.
Do not edit the generated directories.

The candidate pins Astro 7.4.13 and Starlight 0.43.1 (official upstream
package metadata indicates compatible peers). Node >=22.23.4 is required;
use the project's compatible Node 25 toolchain or another approved pinned
version. The registry-resolved lockfile is committed; run `npm --prefix website ci` and
`npm --prefix website run build`. Do not publish before the full gates pass.

The public source manifest now includes ten existing root guides. The build stages derived Markdown, adds a development notice, and uses a remark AST transform to rewrite cross-guide links while keeping source files unchanged. The candidate workflow validates the committed lockfile, installs with npm ci, audits dependencies and builds without deployment permissions.

## AI and static search outputs

`npm run build` now generates Markdown siblings, `llms.txt`, `llms-full.txt`, `version.json` and `documentation-manifest.json` into dist after Astro. All exports use the staged allowlisted sources, exact checked-out commit/tree and development status. The full context is capped at 1 MiB. Run `npm run test:ai`, `node scripts/check-build.mjs` and `node scripts/check-ai.mjs` from website after npm ci/build. Direct dependency declarations promote existing locked transitive packages without changing versions or integrity; clean npm ci and audit verify the root-only lock update. No release tag is inferred; project Pages cannot configure origin-root robots.txt. Browser validation and public deployment remain pending.

The build exports scoped reference contracts from docs/contracts and checks exact evidence blobs and reference table consistency. Source changes invalidate the reference until reviewed. Full REST inventory, general OpenAPI validation and Scalar remain open.

Browser smoke: after build, run `CHROME_BIN=chromium node scripts/browser-smoke.mjs` from website with a local Chromium installation. CI retains screenshots and a report. Deployment is manual from reviewed main; see [publishing runbook](../docs/architecture/documentation-publishing.md).

API scope expanded to seven operations. Candidate CI additionally runs openapi-spec-validator 0.7.2 in an isolated Python environment. Its transitive dependency lock is not yet committed; this is an independent schema check, not full contract runtime certification.

Scalar candidate: CI downloads the exact 1.69.2 standalone package archive without installing or executing package scripts, records its integrity and stages a noindex self-hosted viewer into the candidate artifact. Browser smoke checks the rendered operations with test requests hidden. This experimentation is not yet in the release build: approve archive integrity, integrate the viewer and independent validator with reproducible dependencies, then expose the navigation link. Static HTML and Markdown remain authoritative.

Next increment: browser launch has a longer bounded timeout, validator uses exact observed versions with pip check and runs in both build and deployment workflows, and docs-quickstart exercises the real binary and documented flags on disposable Linux. Exact Python versions are not yet package-hash pins. Scalar remains isolated from deployment until browser and dependency review pass.

## Integrated development API viewer

CI 44737871458 verified Scalar 1.69.2 rendering; Quickstart 44737871615 also passed. The full build now stages the same version and verified archive, writes per-file provenance, links the viewer from the static API reference, and checks the viewer in all build/deploy browser runs. The script retains its historical candidate filename. Bash, Python 3 and network access to npm are required for full build. The viewer is noindex and not a canonical content source. Separate review of bundled dependencies and licenses is still needed; npm audit measures the npm lockfile, not the standalone vendor bundle. Accessibility CI uses test-only axe-core 4.11.1, verified against upstream v4.11.1 sri-history.json, with five representative configurations and a serious/critical gate; it does not claim complete WCAG compliance.

Expanded scoped API contract to 19 operations, including selected record authentication/account management and file token creation. Source evidence and human operation table are checked; browser smoke additionally requires the new auth and file-token operations to render. OAuth2, impersonation, file download/multipart, administration and other families remain excluded. This is not a runtime contract test of each operation.

The scoped OpenAPI reference now covers 31 operations, adding collections, settings, backup metadata and file reads. Schema validation and evidence guards apply; administrative operations explicitly require superuser credentials. Collection and settings models remain partial open-object schemas. Do not treat the reference as exhaustive or a runtime certification.
