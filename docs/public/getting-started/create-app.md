---
title: Create an application
description: Generate a runnable Ruxbase React or Next.js frontend with a safe local proxy and a black-and-white starter UI.
---

> Development documentation. These examples target a disposable local Ruxbase backend, not a certified production release.

## Generate a project

Start the [Ruxbase Docker Compose Quickstart](/ruxbase/getting-started/quickstart/) first (API on `127.0.0.1:8090`), and configure the `posts` collection and ordinary users in [Example app](/ruxbase/integrations/example-app/).

The checked-in generator copies complete source templates. It does not execute generated code, access a database, require an AI account, or invent credentials. It refuses an existing destination or unsafe npm project name.

**React + Vite:**

```sh
node scripts/create-ruxbase-app.mjs react my-react-app
cd my-react-app
npm install
npm run dev
```

Open http://127.0.0.1:5173. The Vite development server forwards same-origin `/api` to the local Ruxbase backend. The React starter uses an in-memory SDK auth store and a simple login/draft/list flow.

**Next.js App Router:**

```sh
node scripts/create-ruxbase-app.mjs nextjs my-next-app
cd my-next-app
npm install
npm run dev
```

Open http://127.0.0.1:3000. The Next.js configuration proxies `/api` through a server-side rewrite to the trusted backend origin. The example uses a client component (not an authenticated server component) and does not share user auth between SSR requests.

Both starters use `pocketbase@0.28.1`, sample posts owner rules, the approved Ruxbase black/white SVG master assets, a true-black dark theme and a white light theme. No database URL, superuser token or password is embedded in the frontend.

## Run both services with Docker Compose

For an in-repository Docker developer environment, use the root backend stack plus the selected starter overlay from **repository root**:

React:
```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/react/compose.yaml up -d --build --wait react
```

Next.js:
```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/nextjs/compose.yaml up -d --build --wait nextjs
```

The overlays require the root checked-out Ruxbase source and are **not** standalone generated project Compose files. Generated applications run with npm against the already-started backend. All published host ports are bound to loopback; PostgreSQL remains private.

## What to do when setup fails

| Error | Check |
| --- | --- |
| Backend not connected or 502 | Verify `docker compose --env-file .env.quickstart ps` and `curl http://127.0.0.1:8090/api/health?ready`. |
| Posts collection missing (404) | Create `posts` and the documented list/view/create ownership rules before running the app. |
| Login rejected (400) | Use an ordinary `users` auth account; never an administrator token in browser JavaScript. |
| Port already in use | Stop old dev servers or change host port mapping. |
| App runs but drafts hidden | Recheck the `author` relation, published flag and owner rules. |
| Next.js rewrite fails | Set `RUXBASE_INTERNAL_URL` only to the trusted backend origin; do not use a request-controlled URL. |

## For AI development tools

Use the deterministic tool command rather than asking an AI assistant to invent integration APIs:

```text
Read https://abdullahzaher.github.io/ruxbase/llms-integrations.txt and the current repo docs.
Generate a React or Next.js project using scripts/create-ruxbase-app.mjs.
Start Ruxbase separately with the documented Docker Compose Quickstart.
Use the ordinary users/posts example schema, not a superuser client.
Build the generated app and verify /api/health and owner-only draft access.
Do not claim production readiness or real user session security from this starter.
```

See the checked-in [AI project tasks](https://github.com/AbdullahZaher/ruxbase/blob/main/starters/ai-tasks.json) and [Phase 3 acceptance](https://github.com/AbdullahZaher/ruxbase/blob/main/docs/architecture/phase3-developer-experience-b01.md). The CI tests deterministically generated project build, browser login, draft isolation, theme rendering and API proxy integration using a disposable Ruxbase/PostgreSQL setup. It is an executable scaffold acceptance proxy, **not** a measurement of open-ended LLM answer quality or tool-use reliability.
