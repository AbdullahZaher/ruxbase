# Documentation phase 2 — integration and AI context

Baseline: d03ba38b8fa2063cd1479c9198c44de21a16de93 (tree b971186057ce8e178995cf1e80ac294603a5bc7f). Scope: additive documentation and tests, no runtime changes. Existing public slugs, publication scope, SEO and build gates remain unchanged.

## D01 first implementation batch
- Six public integration guides: choice, example collection/rules, JS/TS, server-side/SSR, REST, mobile.
- Evidence: checked-in SDK 0.28.1 compatibility suite, scoped HTTP contract, upstream client guidance. Framework and mobile examples explicitly untested.
- Two identical reusable documented functions imported by the SDK suite: filtered list and direct HTTP listing. Assert Markdown snippets match source. All existing SDK and runtime tests remain required.
- Generate bounded 256 KiB llms-integrations.txt from approved integration Markdown only. Validate exact bytes, identity, links and total size. Preserve the 1 MiB full context limit.
- Original 30-query retrieval fixture and gate retained; add 12 integration questions with independent >=11/12 and zero critical misses, ranked against all published Markdown (not just integrations). Authored lexical proxy, not independent LLM answer evaluation.
- Add integration navigation and README/AGENTS pointers. At merge these links require the subsequent authorized documentation deployment to become public; verify updated build identity.
- Initial source review: existing JS suite covers auth, CRUD, files, relations, rules and realtime. No Dart/device/SSR framework suite. Official upstream docs: https://pocketbase.io/docs/how-to-use/ and https://github.com/pocketbase/js-sdk; original examples written for the Ruxbase test schema rather than wholesale copying.

## Remaining phase 2 increments
D02: detailed collection types, field types, authentication, filters/rules, relations and files guides with verified examples.
D03: framework-specific SPA and SSR adapters, multitasking/session tests, mobile Dart/React Native validation where actual toolchains are available.
D04: per-family API examples and response/error contracts, including tested MFA/password-reset/file behavior.
D05/D06: Go and JS server extension guides, database operations/hooks/migrations with PostgreSQL-specific constraints and runnable examples.
D07/D08: PostgreSQL-native and deployment playbooks, tested tutorials and operations prerequisites; do not claim production readiness.
D09/D10: end-to-end sample applications, additional topic context and retrieval coverage, accessibility/performance checks, reviewed main publication.

All future docs follow allowlist, source evidence, exact CI and manual reviewed deployment. External indexing or AI ingestion is not guaranteed. No stable version promotion, Scalar distribution or runtime architecture change authorized.
