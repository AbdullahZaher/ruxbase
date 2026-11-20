---
title: Next.js App Router starter tutorial
description: Build and run a client-scoped Ruxbase SDK frontend in Next.js, with a local API rewrite and real integration tests.
---

> Development documentation. This is a small client-only example, not a secure authenticated SSR template.

The source is in `starters/nextjs/`. To create your own runnable project, start Ruxbase separately using the [Docker Compose Quickstart](/ruxbase/getting-started/quickstart/) and run:

```sh
node scripts/create-ruxbase-app.mjs nextjs my-next-app
cd my-next-app
npm install
npm run dev
```

Visit http://127.0.0.1:3000. The Next.js App Router page imports a client component with a **per-mounted-app in-memory PocketBase SDK**. The `/api/:path*` rewrite sends API traffic to the configured **trusted** backend origin. This demo does not use or share server user credentials.

For an in-repository Docker environment, use the root Compose stack with `starters/nextjs/compose.yaml`; see [Create an application](/ruxbase/getting-started/create-app/).

## Validate two users

Create the [posts schema and rules](/ruxbase/integrations/example-app/) and two ordinary test users. Create a draft as Alice, then verify Bob and a guest cannot list it. A client-side list filter is not a substitute for server authorization. The UI presents a monochrome design and a true-black dark mode.

## Security and evidence

The Phase 3 CI launches and tests the actual Next.js toolchain and proxy against the Ruxbase API, with browser interaction and separate-user rule checks. It does **not** certify CSRF, concurrent authenticated server route handlers, cookie refresh, OAuth, production CDN caching or MFA. For protected SSR behavior use [request-scoped sessions](/ruxbase/integrations/server-side/) and app-specific CSRF protections.
