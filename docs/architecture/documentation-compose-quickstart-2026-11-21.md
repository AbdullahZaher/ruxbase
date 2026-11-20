# Docker Compose Quickstart — engineering acceptance

Baseline: `95a38af7c92870e5a54770d77d245767ba3b1480`; tree `aa86f70634273fd72517c24b5009b524af4eefe9`.

## Scope

Local development-only first-class `compose.yaml`: actual Go Ruxbase container built from the existing Dockerfile plus PostgreSQL 18 and pgvector, with both application and PostgreSQL health checks. No application/runtime source or production role contract changed.

The user must supply a unique URL-safe random password in ignored `.env.quickstart`; never commit secrets. Passwords are expanded into an internal DATABASE_URL and can be exposed to Docker inspect/config authorities. The development DB owner is privileged and `sslmode=disable` is scoped to the private Compose network; **do not use this file as a production security template**.

Only application port 8090 binds to host loopback. No database host port. Application and database use separate named volumes. `down` preserves volumes; `down -v` intentionally deletes them. The separate `docker-compose.dev.yml` remains a database-only option.

## Acceptance

Existing Go/embedded PostgreSQL quickstart is unchanged and remains tested. The new independent `compose` job in `docs-quickstart.yml` builds the real dashboard and Docker application image, requires valid rendered config and isolated network, checks both services healthy, HTTP readiness/dashboard, PostgreSQL 18 pgvector availability, and persists CI-only data across container down/up without `-v`. It then destroys only its own disposable project volumes.

Docker is not present in the assistant runtime. The full Compose acceptance must be established by GitHub Actions; no local container success is claimed. Standard six CI workflows and main-only reviewed deployment rules remain in force. Production certification remains NO-GO.
