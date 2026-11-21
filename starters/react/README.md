# Ruxbase React + Vite starter

This sample uses **React 19.3.1**, **Vite 8.3.8** and **pocketbase 0.28.1**. It renders the official white/black Ruxbase SVG in the corresponding theme. Night mode background is pure black.

## Run inside the Ruxbase source checkout with Docker Compose

This section applies to the starter **inside the Ruxbase repository**. A generated standalone project does not include the root Compose stack; use the standalone npm instructions below after starting Ruxbase separately.

From the repository root, create the [local Quickstart credentials](https://abdullahzaher.github.io/ruxbase/getting-started/quickstart/) and start:

```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/react/compose.yaml up -d --build --wait react
```

Visit http://127.0.0.1:5173. Only the frontend (5173) and Ruxbase (8090) publish loopback ports; PostgreSQL stays private. The Vite server forwards same-origin `/api` to `http://ruxbase:8090`. To stop without deleting database data, run:

```sh
docker compose --env-file .env.quickstart -f compose.yaml -f starters/react/compose.yaml down
```

## Run standalone on your laptop

Start Ruxbase separately at http://127.0.0.1:8090 using the root Quickstart. Then from this folder:

```sh
npm ci
npm run dev
```

The local Vite proxy targets 127.0.0.1:8090 by default. Copying this starter outside the repository also works for npm development; the repository's `compose.yaml` overlay is designed for use **inside** this monorepo.

## Prerequisites for example data

In the Ruxbase dashboard create the `posts` collection and the ordinary `users` from [the documented schema and owner rules](https://abdullahzaher.github.io/ruxbase/integrations/example-app/). Create two ordinary test users. Do **not** put superuser credentials or PostgreSQL URLs in browser code. The browser uses an in-memory SDK auth store: refresh signs out. Do not interpret this minimal demo as MFA, production session, CSRF, OAuth or row-level security certification.

## Common setup failures

- **Backend not connected:** run `docker compose --env-file .env.quickstart ps` or check `/api/health?ready`.
- **Posts collection missing:** create `posts` and apply list/view/create rules in the example guide.
- **Login rejected:** verify normal user credentials and enabled password auth; never sign in as a superuser.
- **Port in use:** stop conflicting local containers or change the host port mapping.
