<p align="center">
    <img src="assets/brand/ruxbase.png" alt="ruxbase" width="320" />
</p>

<p align="center">
    <strong>Powerful by design. Lightweight by default.</strong><br />
    A PostgreSQL-native, self-hosted backend platform.
</p>

<p align="center">
  <a href="https://github.com/AbdullahZaher/ruxbase/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/AbdullahZaher/ruxbase/actions/workflows/ci.yml/badge.svg?branch=main" /></a>
  <a href="LICENSE.md"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-blue" /></a>
  <a href="https://abdullahzaher.github.io/ruxbase/">Documentation</a>
  · <a href="https://abdullahzaher.github.io/ruxbase/getting-started/quickstart/">Quickstart</a>
  · <a href="https://abdullahzaher.github.io/ruxbase/reference/api/">API Reference</a>
  · <a href="https://github.com/AbdullahZaher/ruxbase/releases">Releases</a>
  · <a href="UPSTREAM.md">Upstream policy</a>
  · <a href="docs/COMPATIBILITY.md">Compatibility</a>
</p>

## What is Ruxbase?

Ruxbase is an independent, open-source backend platform based on
[PocketBase v0.40.6](https://github.com/pocketbase/pocketbase/tree/v0.40.6).
It replaces SQLite in the server with **PostgreSQL 17+** while retaining the
lightweight Go application, integrated administration dashboard and familiar
collection-based development experience.

Our goal is to deliver a full-featured alternative to larger self-hosted BaaS
stacks, including Supabase, without unnecessary operational complexity. The
official PocketBase repository is the **sole ongoing upstream** for inherited
application code. PostgreSQL integration and Ruxbase-specific features are
maintained independently. No third-party fork is required as a runtime
dependency or synchronization source.

## Documentation

[SDKs and integrations](https://abdullahzaher.github.io/ruxbase/integrations/overview/) — [JavaScript](https://abdullahzaher.github.io/ruxbase/integrations/javascript/), [REST](https://abdullahzaher.github.io/ruxbase/integrations/rest/), [integration context for AI](https://abdullahzaher.github.io/ruxbase/llms-integrations.txt).

The [public documentation](https://abdullahzaher.github.io/ruxbase/) describes the published development snapshot; it is not a stable release. Check [build identity](https://abdullahzaher.github.io/ruxbase/version.json) against your checkout before relying on version-specific behavior.

- [Quickstart](https://abdullahzaher.github.io/ruxbase/getting-started/quickstart/) and [API reference](https://abdullahzaher.github.io/ruxbase/reference/api/)
- [Using Ruxbase with AI](https://abdullahzaher.github.io/ruxbase/ai/using-ruxbase-with-ai/) — [llms.txt](https://abdullahzaher.github.io/ruxbase/llms.txt) and [full Markdown context](https://abdullahzaher.github.io/ruxbase/llms-full.txt)
- [OpenAPI](https://abdullahzaher.github.io/ruxbase/openapi.json), [API coverage](https://abdullahzaher.github.io/ruxbase/api-coverage.json), [feature manifest](https://abdullahzaher.github.io/ruxbase/feature-manifest.json), [CLI reference](https://abdullahzaher.github.io/ruxbase/cli-reference.json), and [configuration reference](https://abdullahzaher.github.io/ruxbase/configuration-reference.json)

The checked-out sources and tests remain authoritative when they differ from a previously published snapshot. API contracts are scoped, and MCP is documented separately.

## Current capabilities

| Capability | Description |
| --- | --- |
| Data API | Collection-based REST API, filtering and relations |
| Authentication | PocketBase-compatible authentication and auth collections |
| Administration | Integrated dashboard for data, users and configuration |
| PostgreSQL | PostgreSQL 17+, connection pooling and SQL tooling |
| Clustering | Multiple instances behind a load balancer, coordinated through PostgreSQL ([guide](docs/CLUSTER.md)) |
| Authorization | Collection API rules and supported PostgreSQL RLS policy compilation ([scope and limitations](docs/RLS.md)) |
| Realtime | SSE with an optional PostgreSQL logical replication source ([guide](docs/REALTIME.md)) |
| Search | Full-text search and pgvector similarity search ([text](docs/SEARCH.md), [vector](docs/VECTOR.md)) |
| File storage | Local and S3-compatible storage |
| AI integration | Built-in, opt-in MCP server using collection API rules ([guide](docs/MCP.md)) |
| Extensibility | Go framework, JavaScript hooks, custom routes and migrations |
| Development | Embedded PostgreSQL for local development ([guide](docs/DEV.md)) |
| Operations | Readiness checks, connection pools, logical backups and restore tooling ([guide](docs/OPERATIONS.md)) |

The server is built as a single Go binary, with PostgreSQL as an external
service in normal deployments. SQLite is reserved for the separate import
tool. Compatibility with the official [JavaScript](https://github.com/pocketbase/js-sdk)
and [Dart](https://github.com/pocketbase/dart-sdk) SDKs is a goal bounded by the
documented contracts and tested behavior; see [compatibility](docs/COMPATIBILITY.md).

## Project direction

The roadmap prioritizes secure PostgreSQL-native data access, consistent
authorization, safer schema migration, durable background jobs, expanded
realtime, improved developer tooling and simpler self-hosted operations.
Multi-project administration and additional AI capabilities are future
directions, not claims of implemented functionality.

Ruxbase is under active development. Do not treat a passing CI run as a
production security or reliability certification. Review the documented RLS,
cluster, realtime and recovery constraints before deploying sensitive workloads.

## Quick start

### Docker

```sh
docker build -t ruxbase .

docker run -d -p 8090:8090 \
  -e DATABASE_URL="postgres://user:pass@db-host:5432/ruxbase" \
  -v ruxbase_data:/app/ruxbase_data \
  ruxbase
```

Open `http://localhost:8090/_/` to create the first superuser. The image
includes the Postgres client tools for the backups and the `ruxbase-import`
command. The server runs as the non-root user `ruxbase` (uid 10001): a bind
mounted data directory must be writable by it.

### Binary

If a suitable published archive is available, download it from the [releases page](https://github.com/AbdullahZaher/ruxbase/releases) and follow [release verification](.github/SECURITY.md#verifying-a-release). Otherwise build it from the checked-out source with `go build -o ruxbase ./examples/base`. Then run:

```sh
DATABASE_URL="postgres://user:pass@localhost:5432/ruxbase" ./ruxbase serve
```

The database needs the `pgcrypto` extension (and `vector` for the vector
fields). The app role needs the `CREATEROLE` attribute for the RLS roles (see
[RLS.md](docs/RLS.md)).

### Development

```sh
./ruxbase serve --dev
```

Without a `DATABASE_URL`, the dev mode downloads and starts an embedded Postgres
in `ruxbase_data/.pg`.

## Migrating from PocketBase

```sh
DATABASE_URL="postgres://..." ruxbase-import -dir ./ruxbase_data ./pb_data
```

The importer is designed to migrate collections, records, settings, logs and files. Its report lists items requiring manual changes, such as SQLite-specific view queries. Validate data integrity, authentication and client compatibility in a disposable staging environment before cutover. See [MIGRATING.md](docs/MIGRATING.md).

## Benchmarks

Existing benchmark results describe specific configurations and workloads; the PostgreSQL and SQLite setups do not have equal resource allocations. See [BENCHMARKS.md](docs/BENCHMARKS.md) for methodology and limitations. Independent, resource-matched measurements of Ruxbase against Supabase remain future work. No universal performance or memory advantage is claimed.

## Use as a Go framework

Ruxbase is a Go library with familiar PocketBase extension APIs (hooks, routes,
migrations and the JS VM plugin), subject to the documented compatibility differences:

```go
package main

import (
    "log"

    "github.com/AbdullahZaher/ruxbase"
    "github.com/AbdullahZaher/ruxbase/core"
)

func main() {
    app := ruxbase.New()

    app.OnServe().BindFunc(func(se *core.ServeEvent) error {
        se.Router.GET("/hello", func(re *core.RequestEvent) error {
            return re.String(200, "Hello world!")
        })

        return se.Next()
    })

    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

The raw SQL of the hooks and migrations must be Postgres SQL (see
[COMPATIBILITY.md](docs/COMPATIBILITY.md)).

## Development of ruxbase

```sh
# Postgres 18 + pgvector for development (credentials in .env)
docker compose -f docker-compose.dev.yml up -d

# the test suite (RUXBASE_TEST_URL is the template database of the tests)
RUXBASE_TEST_URL="postgres://user:pass@localhost:5433/ruxbase_test" go test ./...

# the JS SDK compatibility tests against a server built from the sources
DATABASE_URL="postgres://..." ./scripts/sdk-test.sh

# the dashboard UI (Node 25+)
npm --prefix ui ci && npm --prefix ui run build
```

## Upstream and contributing

PocketBase is the only ongoing upstream. The reference tag and commit, integration status and review requirements are tracked in [UPSTREAM.lock.json](UPSTREAM.lock.json). Changes are selectively reviewed and adapted to PostgreSQL before integration; automatic upstream merges are not supported. See [UPSTREAM.md](UPSTREAM.md), [CONTRIBUTING.md](CONTRIBUTING.md) and the [ancestry recovery runbook](docs/UPSTREAM-ANCESTRY.md).

## License

ruxbase is distributed under the MIT license, the same as PocketBase. PocketBase
is created and maintained by Gani Georgiev; ruxbase is not affiliated with the
PocketBase project.
