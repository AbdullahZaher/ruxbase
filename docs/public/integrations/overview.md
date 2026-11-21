---
title: Choose an integration
description: Select a supported Ruxbase client, runtime and integration model.
sidebar:
  order: 1
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## Which SDK should I use?

The Ruxbase server exposes collection-based HTTP APIs. The application connects to the server URL; it does not connect directly to PostgreSQL. The official PocketBase JavaScript SDK is the tested client library, not a separate Ruxbase npm SDK.

| Client or runtime | Integration path | Evidence |
| --- | --- | --- |
| Browser SPA: React, Vue, Svelte | [JavaScript SDK](/ruxbase/integrations/javascript/) | Shared SDK operations tested; framework lifecycle not tested |
| Node.js or server-side rendering: Next.js, Nuxt, SvelteKit | [Server-side integration](/ruxbase/integrations/server-side/) | Per-request client pattern; framework adapters not certified |
| React Native / Flutter | [Mobile integration](/ruxbase/integrations/mobile/) | Upstream client guidance; device end-to-end tests not present |
| Any HTTP-capable language | [REST integration](/ruxbase/integrations/rest/) | Scoped OpenAPI and HTTP contract tests |

## First application

1. [Start Ruxbase locally](/ruxbase/getting-started/quickstart/).
2. [Create the example collections and API rules](/ruxbase/integrations/example-app/).
3. Select a client above. Use ordinary end-user credentials, not administrator credentials.
4. Test both permitted and denied access with different accounts.

## Connection boundary

Use the origin of your running Ruxbase application, for example http://127.0.0.1:8090 for local development, and HTTPS for deployed clients. The GitHub Pages documentation URL is not a Ruxbase API server. The administration dashboard path /_/ is not the SDK base URL.

PostgreSQL is a server-side dependency. The API rules govern SDK requests; direct database clients have a different security boundary. See [row-level security](/ruxbase/authorization/row-level-security/) and [compatibility](/ruxbase/migration/compatibility/).

## AI context

For integration questions, retrieve [llms-integrations.txt](/ruxbase/llms-integrations.txt) or an individual Markdown guide through [llms.txt](/ruxbase/llms.txt), check [version.json](/ruxbase/version.json), and verify implementation details in the matching source. Do not infer framework support from a successful generic SDK test.

## Evidence and limitations

The current JS SDK lock is 0.28.1. See [Ruxbase JS SDK test suite](https://github.com/AbdullahZaher/ruxbase/blob/d03ba38b8fa2063cd1479c9198c44de21a16de93/tests/sdk/js/sdk.test.js) and [PocketBase client integration guidance](https://pocketbase.io/docs/how-to-use/). Framework names identify possible client environments, not a claim that Ruxbase ships or certifies their adapters.
