---
title: Quickstart
description: Start Ruxbase with Docker Compose and PostgreSQL 18, or run the lightweight Go development binary.
---

> Development documentation. These instructions target the current source, not an approved production release.

Ruxbase provides two ways to start locally. **Docker Compose is recommended** for most developers because it runs Ruxbase and PostgreSQL 18 with pgvector together, without requiring Go or a separately installed database. Both methods use localhost port 8090.

## Option A: Docker Compose (recommended)

**Prerequisites:** Docker Engine/Desktop with Compose v2, internet access to build the image and download dependencies, a free port 8090, and a checkout of the [Ruxbase source](https://github.com/AbdullahZaher/ruxbase). OpenSSL is optional for password generation.

Create a private credentials file. On macOS/Linux with OpenSSL:

```sh
umask 077
printf 'POSTGRES_USER=ruxbase_local\nPOSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 24)" > .env.quickstart
```

Alternatively, copy `.env.quickstart.example` to `.env.quickstart` and set a strong, unique **URL-safe** `POSTGRES_PASSWORD` by hand. Never commit this file. The connection string interpolates these variables directly, so use alphanumeric/hex credentials; special URL characters such as `@`, `/`, `#` or `%` require percent encoding.

Start the two services and wait for both health checks:

```sh
docker compose --env-file .env.quickstart up -d --build --wait
```

The first build compiles the Go server from the checked-out source. The default `compose.yaml` runs:

| Service | Purpose | Network exposure |
| --- | --- | --- |
| `ruxbase` | Ruxbase API and admin dashboard | `127.0.0.1:8090` only |
| `postgres` | PostgreSQL 18 with pgvector and logical-WAL configuration | Private Compose network; no host port |

The two persistent named volumes `postgres_data` and `ruxbase_data` hold the database and server files separately. Startup waits for PostgreSQL health; the Ruxbase health check uses the database-aware `GET /api/health?ready` endpoint. The disposable database is initialized with an owner role for local development; this is **not a production least-privilege or secret-management configuration**. `sslmode=disable` applies only to the isolated container network.

Visit the [local admin dashboard](http://localhost:8090/_/) and complete first-superuser setup. Check readiness:

```sh
curl --fail http://127.0.0.1:8090/api/health?ready
```

Stop containers while retaining application and database data:

```sh
docker compose --env-file .env.quickstart down
```

To restart without rebuilding, run `docker compose --env-file .env.quickstart up -d --wait`. Do **not** run `down -v` unless you intend to delete both named volumes and their contents. Keep the existing `.env.quickstart` unchanged while reusing initialized volumes; changing this file does not rotate an existing PostgreSQL password.

For diagnostics, use `docker compose --env-file .env.quickstart ps` and `docker compose --env-file .env.quickstart logs --tail=100 ruxbase postgres`; keep logs and connection credentials private.

The older [docker-compose.dev.yml](https://github.com/AbdullahZaher/ruxbase/blob/main/docker-compose.dev.yml) runs only a development PostgreSQL service. You do not need it for this Quickstart, and you should not start both stacks on the same database accidentally.

## Option B: Go + embedded PostgreSQL

**Prerequisites:**

- Go 1.27 or newer (see the repository's go.mod).
- An available local port 8090.
- Internet access for dependency and embedded PostgreSQL downloads.

From the Ruxbase checkout:

```sh
go build -o ruxbase ./examples/base
./ruxbase serve --dev
```

When `DATABASE_URL` is not set, development mode starts embedded PostgreSQL. Open the [local admin dashboard](http://localhost:8090/_/) and complete first-superuser setup.

The embedded database has `fsync=off` and **does not include pgvector**. It is for disposable development only, not production data. See [development mode](/ruxbase/getting-started/development/) for platform and durability limitations.

## Next steps

[Choose a client SDK](/ruxbase/integrations/overview/) and [create the example collections](/ruxbase/integrations/example-app/). Review [compatibility](/ruxbase/migration/compatibility/), [operations](/ruxbase/operations/overview/), [RLS](/ruxbase/authorization/row-level-security/) and the [deployment guide](/ruxbase/operations/deployment/) before moving beyond local development.

The docs-quickstart CI exercises **both** paths: compiled Go with embedded PostgreSQL, plus the real Compose application/PostgreSQL containers, readiness, dashboard, pgvector availability and persistence across container recreation. Development CI success does not establish production readiness.
