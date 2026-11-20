---
title: Using Ruxbase with AI
description: Give AI assistants version-specific Ruxbase documentation and machine-readable context.
---

**Development documentation.** The sources describe ongoing development, not an approved production release.

## Choose your context

- [Documentation index for AI (llms.txt)](/ruxbase/llms.txt) lists direct Markdown links and short descriptions.
- [Full documentation context (llms-full.txt)](/ruxbase/llms-full.txt) combines the currently published pages. Use smaller individual pages when token budgets are limited.
- [Build identity (version.json)](/ruxbase/version.json) identifies the exact source commit, tree, and documentation status.
- Each HTML page links to a downloadable Markdown version beside the page content. These files are generated from the same staged Markdown, not handwritten copies.

Give your assistant the specific documentation page or the index, ask it to identify the source commit and cite the relevant section, and verify examples against your actual installed version. Treat features with documented limitations accordingly. Do not assume full PocketBase or Supabase compatibility.

## Working in the source repository

Read the project's [AGENTS.md](https://github.com/AbdullahZaher/ruxbase/blob/main/AGENTS.md), upstream policy and relevant tests. Review [compatibility](/ruxbase/migration/compatibility/) before relying on upstream SDK behavior.

The [Ruxbase MCP server](/ruxbase/ai/mcp/) is an optional runtime integration for application collections, not a documentation crawler. Do not give an assistant production credentials merely to read these public docs.

The text index improves direct retrieval, but does not promise that any search engine or model will index, train on, or recommend Ruxbase.

## Machine-readable references

[OpenAPI JSON](/ruxbase/openapi.json) covers health, records, vector search, selected authentication, files, collections, settings and backup metadata; not the complete API. [Feature manifest](/ruxbase/feature-manifest.json), [CLI flags](/ruxbase/cli-reference.json), and [configuration settings](/ruxbase/configuration-reference.json) record their scope and source identity.
