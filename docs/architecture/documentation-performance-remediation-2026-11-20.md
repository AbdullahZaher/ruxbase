# Documentation performance remediation

Baseline main d9ee5fc7331fb31a07a12d9c69f4ede310c9330a, tree 4f403a6afcf37a42070168645e66d4059793a469; post-merge run 44810603897 attempt 1 failed. Retained reports artifact 14663719288 (SHA256 e261feb2a55cadcc5217ecc0a2fb9617b1370ce93118a78bb0fde95bbd9e8adb) and built site 14665885009 (SHA256 00fe95f9d8d02c470e06ce8408e0fd8f6dddb705bc763ec8246bdd9fa44524c5).

## Observed bottlenecks
- Median LCP home 1354.3822 ms, quickstart 2552.2641 ms, API 2554.2516 ms. Same-tree PR retry passed, so runner/simulation variance is real; not proof of a single root cause.
- Actual downloaded CSS common.CO17suEb.css 65,181 bytes (65,331 with transfer overhead); code CSS 18,292 bytes. Lighthouse identifies common CSS as render-blocking and estimates around 400–440ms opportunity on sampled slow pages.
- The baseline compiled Search component schedules UI import via requestIdleCallback even without search intent; ui-core.DUUfjXXi.js is 97,196 bytes (97,353 transferred). Shared CSS contains Pagefind UI styles. No custom webfonts in the observed request waterfall. The largest candidate on failing pages is text, not the header logo.
- No threshold or measurement environment changes are justified by the evidence.

## Implementation
Use Starlight's documented Search component override (https://starlight.astro.build/reference/overrides/) and locally generated Pagefind UI assets (the pinned build already emits pagefind-ui.js and pagefind-ui.css). A small native-dialog controller opens on click or keyboard shortcut and loads both resources only on user intent; loading/failure/retry, focus restoration, mobile layout and theme tokens preserved. No new dependency, CDN, CSS stripping, CSS async hack, or generated output edit. Search index/ranking, published content, Markdown, all topic bundles, brand assets and SEO remain in scope. Visual appearance of the search dialog may differ slightly from upstream; verify desktop/mobile and both themes.

## Acceptance
- All 43 HTML routes: non-print initial stylesheet budget 70 KiB (baseline code pages 83,473 bytes), including inline stylesheet text, and no eager search asset tags.
- Existing Chromium checks plus cold idle no-search load, on-demand actual search and focus restore/keyboard reopen without refetch.
- Every existing Lighthouse trial: no Pagefind UI or engine request without explicit search. Preserve simulated mobile profile, three independent cold loads each for homepage/quickstart/API, performance/SEO/accessibility >=.95, median LCP <=2500 ms, CLS <=.1; record all trials. A passed candidate is not proof of field Web Vitals or statistically guaranteed future performance.
- Exact-candidate six-workflow CI, retained artifacts, owner review, exact-main six checks then manual reviewed deploy. No bypass or automatic publish. Locally inspecting the baseline bundle is not a candidate build; report actual candidate results separately.
