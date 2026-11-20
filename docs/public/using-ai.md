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

## Integration-specific context

Use [llms-integrations.txt](/ruxbase/llms-integrations.txt) for client setup, collection rules, JavaScript, SSR, REST and mobile questions before loading the full context. Each guide states its evidence boundary. Use the published source identity when checking whether SDK methods match your installed server. Do not invent a Ruxbase-specific SDK or claim framework/device certification from generic SDK tests.

## Topic-specific AI bundles

- [Core concepts](/ruxbase/llms-concepts.txt) — schema, records, rules and authentication.
- [Integration and tutorials](/ruxbase/llms-integrations.txt) — frontend, server and mobile clients.
- [HTTP and machine references](/ruxbase/llms-api.txt) — operation scope and examples.
- [Server extensions](/ruxbase/llms-extensions.txt) — Go and embedded JavaScript.
- [PostgreSQL and operations](/ruxbase/llms-operations.txt) — data features, authorization and deployment.

Start from llms.txt and select the smallest topic that answers the question. All bundles have exact source identity and are built only from the approved public corpus. A guide that labels a framework example illustrative must not be used as evidence of tested framework support.

## Working in the source repository

Read the project's [AGENTS.md](https://github.com/AbdullahZaher/ruxbase/blob/main/AGENTS.md), upstream policy and relevant tests. Review [compatibility](/ruxbase/migration/compatibility/) before relying on upstream SDK behavior.

The [Ruxbase MCP server](/ruxbase/ai/mcp/) is an optional runtime integration for application collections, not a documentation crawler. Do not give an assistant production credentials merely to read these public docs.

The text index improves direct retrieval, but does not promise that any search engine or model will index, train on, or recommend Ruxbase.

## Machine-readable references

[OpenAPI JSON](/ruxbase/openapi.json) covers 57 direct non-MCP HTTP registrations with scoped schemas; it is not exhaustive runtime certification. [Feature manifest](/ruxbase/feature-manifest.json), [CLI flags](/ruxbase/cli-reference.json), and [configuration settings](/ruxbase/configuration-reference.json) record their scope and source identity.

The scoped OpenAPI now represents the 57 non-MCP static HTTP registrations. The three MCP transport methods are covered separately by the MCP guide; the coverage report distinguishes protocol documentation from OpenAPI operations. Neither route counts nor schema validation imply runtime certification.
