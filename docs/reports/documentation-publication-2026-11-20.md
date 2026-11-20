# Ruxbase documentation — initial publication evidence

Date: 2026-11-20. Disposition: **DEVELOPMENT DOCUMENTATION LIVE — DISCOVERY FOLLOW-UP OPEN**. This is a documentation engineering closure record, not a production application certification or a stable release.

## Source and authority

- Implementation PR: [#18](https://github.com/AbdullahZaher/ruxbase/pull/18), merged by maintainer.
- Published main commit: `baeb10ccf4d86e65889002222f50ebd09d5b2bd1`.
- Git tree: `9820a98d52697d8810c82180128349b7227a6428`.
- Public URL: https://abdullahzaher.github.io/ruxbase/
- [Publication workflow #44788879672](https://github.com/AbdullahZaher/ruxbase/actions/runs/44788879672), attempt 2: SUCCESS (build and deploy). Post-deploy verification passed at 18:10:44 UTC on 2026-11-20.
- The first attempt failed closed due to the exact-main CI gate; PostgreSQL 17 TestSettingsSet event count failure cleared on rerun. No test waiver or root cause certification.

## Exact-main evidence

| Workflow | Run | Result |
| --- | --- | --- |
| Full CI | [44787427705](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427705) | SUCCESS, attempt 2 |
| Foundation | [44787427896](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427896) | SUCCESS |
| Brand | [44787427798](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427798) | SUCCESS |
| Quickstart | [44787427671](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427671) | SUCCESS |
| Docs build | [44787427689](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427689) | SUCCESS |
| Performance | [44787427660](https://github.com/AbdullahZaher/ruxbase/actions/runs/44787427660) | SUCCESS |

Deployment logs verified all six exact-main workflows, 93 sealed files, 30/30 deterministic retrieval questions (27 top-1, 30 top-3, zero critical misses), and byte-exact recovery of the 93-file artifact. Live manifest digest and eight representative file hashes passed. The manifest excludes itself from the file count.

## Deployment build quality

| Mobile lab median | Home | Quickstart | API |
| --- | ---: | ---: | ---: |
| Performance | 100 | 100 | 99 |
| Accessibility | 100 | 100 | 100 |
| SEO | 100 | 100 | 100 |
| Best practices | 100 | 100 | 100 |
| LCP (ms) | 1206.73 | 1205.55 | 2103.97 |
| CLS | 0 | 0 | 0 |

These are deployment build's Lighthouse lab measurements, not field or comprehensive accessibility certification.

## Retained evidence

- Published manifest SHA-256: `e8fd1277ffb896092de35e6c8bd5185884bd61f0e6bdfc24efd68d40ceb4fd65`.
- Reviewed snapshot artifact [14656341470](https://github.com/AbdullahZaher/ruxbase/actions/runs/44788879672/artifacts/14656341470); GitHub-reported archive SHA-256 `279d963b1ec3219a0f3c7c03c6b71fd640a3204a6b558f72b7dab559bf94e2a0`; expiry 2026-12-20 18:10 UTC. Archive independently downloaded in the follow-up review: ZIP SHA-256, manifest SHA-256, exact file set and all 93 listed file sizes and hashes matched. Retain beyond expiry in approved storage before relying on rollback.
- Pages artifact 14656516442; GitHub-reported SHA-256 `e8321a8991fc221d9057ec5c62b24f1d6683895ddd4433bdff9e1b8d9c370d89`; short-lived, not the recovery ZIP.

## Independent retained artifact discovery check

The archive contains 17 canonical sitemap URLs, each mapping to an existing HTML page. The sitemap index points to sitemap-0.xml. The AI index links all 17 Markdown pages, full context, build identity and scoped machine-readable references. version.json confirms the published commit/tree, clean development status and null release version. No api-viewer files are present. These checks establish the retained build content, not a separate live network check or search indexing evidence.

## Scope and unfinished external actions

The public site contains 17 approved documentation pages with 57 scoped OpenAPI operations and three MCP transport registrations represented separately. The initial profile excludes Scalar (pending license review). No stable release, exhaustive schema/runtime certification, field performance, search engine indexing, or remote rollback is asserted.

GitHub repository metadata directly verified Website = https://abdullahzaher.github.io/ruxbase/. No change necessary. Repository description is presently unset; the current connector has no repository metadata update action. Suggested description: PostgreSQL-native, lightweight, self-hosted backend platform with integrated authentication, data APIs, realtime, storage and developer tools.

Public direct browsing was unavailable in this review; the successful workflow's external HTTP/hash probes provide the publication evidence. Search Console/Bing ownership and sitemap submission require owner actions; no verified property or submitted sitemap receipt available. README/AGENTS discovery and maintenance policy are implemented in the follow-up PR and require review/merge. See [publishing and discovery runbook](../architecture/documentation-publishing.md).
