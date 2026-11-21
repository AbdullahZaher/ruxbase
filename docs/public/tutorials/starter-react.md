---
title: React starter tutorial
description: Build and run the Ruxbase React starter with Vite, owner-only drafts, and Docker Compose.
---

> Development documentation, not a production template.

The runnable source is `starters/react/` and the generator is `scripts/create-ruxbase-app.mjs`. For a copied application and commands, follow [Create an application](/ruxbase/getting-started/create-app/).

## Local React development

```sh
node scripts/create-ruxbase-app.mjs react my-react-app
cd my-react-app
npm install
npm run dev
```

Ruxbase must already be available on `127.0.0.1:8090`. The Vite proxy forwards `/api` to the trusted local backend; only the public API is used. No superuser or PostgreSQL credentials go into the browser.

In repository Docker, combine root `compose.yaml` with `starters/react/compose.yaml`, as shown in [Create an application](/ruxbase/getting-started/create-app/). The app uses `pocketbase@0.28.1`, React 19 and Vite 8, an in-memory auth store, and `textContent`-equivalent React output escaping instead of untrusted HTML.

## Test the actual behavior

After creating the [documented posts collection](/ruxbase/integrations/example-app/), sign in as Alice and create an unpublished draft. Alice can view it. When Bob signs in through a separate session, Bob and guests cannot see Alice's draft. Sign out to clear local authentication. Colors follow the approved brand logo variants: **black background at night, white background in light mode**.

## Evidence limits

The `docs-starters` workflow builds and boots a generated React project, loads the page in Chromium, exercises login and draft creation against real Ruxbase/PostgreSQL and checks that unauthorized users cannot retrieve the draft. No OAuth/MFA, production cookie session, mobile framework or external network certification is implied. Use [server-side integration](/ruxbase/integrations/server-side/) for SSR protection.
