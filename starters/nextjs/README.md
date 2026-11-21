# Ruxbase Next.js App Router starter

This example uses **Next.js 16.4.6**, **React 19.3.1** and **pocketbase 0.28.1**. The App Router page uses a client component so each mounted browser app gets its own in-memory SDK auth store. The `next.config.mjs` rewrite proxies only `/api` to the trusted backend; it does not forward database URLs or a superuser token.

## Run inside the Ruxbase source checkout with Docker Compose

This section applies to the starter **inside the Ruxbase repository**. A generated standalone project does not include the root Compose stack; use the standalone npm instructions below after starting Ruxbase separately.

From the repository root, prepare `.env.quickstart` with a strong URL-safe password (see [Quickstart](https://abdullahzaher.github.io/ruxbase/getting-started/quickstart/)). Then:

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
npm ci
npm run dev
```

For local frontend development, the trusted default `RUXBASE_INTERNAL_URL` is http://127.0.0.1:8090. In the Compose overlay the server-only variable is explicitly set to http://ruxbase:8090. Never use a URL from an untrusted request to build a rewrite.

Configure a disposable `posts` collection and ordinary user login with [the example schema](https://abdullahzaher.github.io/ruxbase/integrations/example-app/). The default home page remains a client-only example; the protected server handlers below are a separate optional example and do not make every route authenticated. For protected server-side rendering use request-scoped SDK sessions, CSRF controls and private caching described in the [SSR guide](https://abdullahzaher.github.io/ruxbase/integrations/server-side/).

## Common setup failures

- **API request 502:** backend container may not be ready; inspect Compose services and `/api/health?ready`.
- **Posts collection missing:** create the example schema with owner rules.
- **Login rejected:** verify password authentication for an ordinary user.
- **Port 3000 occupied:** stop the competing service or change the host port.

## Optional authenticated server route example

The `/protected` page and `/api/session/{login,me,posts,logout}` handlers demonstrate **request-scoped**, validated PocketBase SDK sessions. For a local Next.js production server, configure:

```sh
RUXBASE_APP_ORIGIN=http://127.0.0.1:3000 npm run build
RUXBASE_APP_ORIGIN=http://127.0.0.1:3000 npm run start
```

`RUXBASE_APP_ORIGIN` must match the browser-facing origin and is never selected from an untrusted request header. Protected POST routes reject missing/mismatched Origin, cross-site Fetch Metadata and non-JSON payloads. Cookies are HttpOnly and Secure in production, but this does not make plain HTTP suitable for public deployment: use HTTPS beyond loopback. Logout deletes the browser cookie only; it does not revoke the upstream bearer token. Do not consider this example independently production security-certified. See the [server session tutorial](https://abdullahzaher.github.io/ruxbase/tutorials/nextjs-protected-sessions/) for bounded tests and remaining work.
