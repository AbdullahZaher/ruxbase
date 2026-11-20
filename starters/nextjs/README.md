# Ruxbase Next.js App Router starter

This example uses **Next.js 16.4.6**, **React 19.3.1** and **pocketbase 0.28.1**. The App Router page uses a client component so each mounted browser app gets its own in-memory SDK auth store. The `next.config.mjs` rewrite proxies only `/api` to the trusted backend; it does not forward database URLs or a superuser token.

## Run in the monorepo with Docker Compose

From the repository root, prepare `.env.quickstart` with a strong URL-safe password (see [Quickstart](../../docs/public/quickstart.md)). Then:

```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/nextjs/compose.yaml up -d --build --wait nextjs
```

Visit http://127.0.0.1:3000. The Next.js server forwards same-origin `/api` to the private `http://ruxbase:8090` service. PostgreSQL is never published. Stop without deleting the local data:

```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/nextjs/compose.yaml down
```

## Run standalone

Start Ruxbase at http://127.0.0.1:8090, then:

```sh
npm install
npm run dev
```

For local frontend development, the trusted default `RUXBASE_INTERNAL_URL` is http://127.0.0.1:8090. In the Compose overlay the server-only variable is explicitly set to http://ruxbase:8090. Never use a URL from an untrusted request to build a rewrite.

Configure a disposable `posts` collection and ordinary user login with [the example schema](../../docs/public/integrations/example-app.md). This client-only starter is not an authenticated SSR route-handler example. For protected server-side rendering use request-scoped SDK sessions, CSRF controls and private caching described in the [SSR guide](../../docs/public/integrations/server-side.md).

## Common setup failures

- **API request 502:** backend container may not be ready; inspect Compose services and `/api/health?ready`.
- **Posts collection missing:** create the example schema with owner rules.
- **Login rejected:** verify password authentication for an ordinary user.
- **Port 3000 occupied:** stop the competing service or change the host port.
