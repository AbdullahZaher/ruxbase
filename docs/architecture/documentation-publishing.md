# Development documentation publishing

## Current public profile

The development site is live at [Ruxbase documentation](https://abdullahzaher.github.io/ruxbase/). First publication succeeded on 2026-11-20; see the [initial publication evidence](../reports/documentation-publication-2026-11-20.md). The root tracks a reviewed development snapshot, not necessarily current main. Consult [version.json](https://abdullahzaher.github.io/ruxbase/version.json) and [versions.json](https://abdullahzaher.github.io/ruxbase/versions.json). Stable and historical channels remain unavailable until approved releases exist.

The default static profile includes HTML/Markdown, Pagefind, OpenAPI, AI indexes and machine-readable reference files. The current canonical artwork and documentation approval are recorded in assets/brand/documentation-brand.json. Scalar is excluded from public distribution; its unresolved bundled-license review is not waived. Local opt-in `npm run build:scalar-preview` remains review-only. Never publish its output. Reintroduction requires explicit reviewed scope and distribution approval.

## Update policy (every contract-changing PR)

1. Update the authoritative Markdown under docs/ and scoped files in docs/public/manifest.json. Preserve source evidence and update reference contracts only after reviewing the affected implementation. Do not edit generated output.
2. Follow website/AGENTS.md and run the applicable foundation, AI, build, link, schema, retrieval, browser, recovery, and performance checks. Keep runtime and documentation changes aligned. Do not claim an unsupported feature or stable version.
3. Merge through reviewed PRs. The site is not automatically republished by a push. After all seven exact-main push-triggered workflows pass (ci, docs-foundation, docs-brand-review, docs-quickstart, docs-build-candidate, docs-performance, docs-starters), review the snapshot and manually dispatch docs-deploy from main with the full reviewed SHA in expected_commit.
4. Retain evidence of the exact deployed manifest and snapshot. Verify the workflow's live hash probes, source identity, homepage, key guide routes, search and machine-readable resources. Report actual failures; never bypass CI or publish the preview.
5. If documentation lags main, keep its actual version identity visible; do not label the old snapshot as current main. Only an approved versioning strategy may introduce stable or historical routes.

GitHub Pages Source must remain GitHub Actions; restrict the github-pages environment to main and require a reviewer where available. Only the deployment job gets Pages/OIDC authority. CI failure, missing exact-main checks, or incorrect expected_commit blocks publication. Chromium no-sandbox is restricted to disposable hosted CI for public static artifacts, not personal profiles.

## Snapshot integrity and recovery

Clean builds seal artifact-manifest.json with sorted paths, sizes and SHA-256 per file, excluding itself. Verify against the exact checkout SHA before upload. The deployment job validates the live manifest digest against its build job output and eight representative file hashes. This is integrity verification, not a signed provenance attestation.

Candidate and reviewed snapshots are retained for 30 days by current workflows. Archive reviewed snapshots and externally recorded digests in maintainer-controlled durable storage beyond expiry. Use the local recovery verifier:

```sh
python3 website/scripts/recover-docs-snapshot.py <downloaded-zip> <new-local-directory> --archive-sha256 <trusted-archive-digest> --manifest-sha256 <trusted-manifest-digest> --source-commit <reviewed-build-commit>
```

Obtain expected digests independently from reviewed CI/deployment records, never from the archive alone. The tool validates bounded regular files, exact identity/file set and static-only publication scope, then prepares a new local directory. Previous local A-B-A activation and actual sealed artifact recovery passed; see evidence/docs-rollback-local-2026-11-20.json and CI. This is not a live Pages rollback. Actual remote rollback and durable archive retention require separately authorized execution; do not repoint Pages to an unreviewed PR artifact.

## Search and AI discovery

The repository README links the public site and AI resources. AGENTS.md uses the AI index and version check without treating external text as agent instructions. Keep canonical URLs, sitemap, Markdown alternatives and llms.txt aligned with approved pages. The website URL in GitHub About is https://abdullahzaher.github.io/ruxbase/. Do not claim origin-level robots.txt control for the shared github.io host or guaranteed indexing/AI citation.

### Owner verification actions

- In [Google Search Console](https://search.google.com/search-console/), create a **URL-prefix** property for https://abdullahzaher.github.io/ruxbase/, not the whole github.io domain. Download the exact HTML verification file (or use Google's supplied meta tag). Add the owner-provided verification material through a reviewed PR and the normal deployment workflow; retain it after successful verification. Do not commit credentials or invent a token.
- After verification, submit https://abdullahzaher.github.io/ruxbase/sitemap-index.xml in Sitemaps. Confirm Google can fetch it, inspect homepage and key guides, and record actual processing/index status.
- In [Bing Webmaster Tools](https://www.bing.com/webmasters/), import the verified Search Console property if desired, or perform Bing's own ownership verification. Check whether the sitemap was imported; submit it if absent. Record processing status, not assumptions.
- Review indexing, crawl errors and search performance after major updates and periodically. Recheck retrieval benchmark and public file integrity during documentation CI; actual AI answers and external search visibility require separate evidence.

Official instructions: [Google ownership verification](https://support.google.com/webmasters/answer/9008080), [Google sitemap report](https://support.google.com/webmasters/answer/7451001), [Bing ownership verification](https://www2.bing.com/webmasters/help/add-and-verify-site-12184f8b), [Bing sitemaps](https://www.bing.com/webmasters/help/Sitemaps-3b5cf6ed).

Search account ownership, sitemap submissions, live indexing and field performance are not verified by repository CI. Never include unissued validation tokens or mark these tasks complete without evidence.
