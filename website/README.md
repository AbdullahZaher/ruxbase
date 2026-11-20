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
