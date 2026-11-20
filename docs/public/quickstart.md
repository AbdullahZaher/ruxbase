---
title: Quickstart
description: Build Ruxbase from source and start a local development instance.
---

> Development documentation. These instructions target the current source, not an approved production release.

## Prerequisites

- Go 1.27 or newer (see the repository's go.mod).
- An available local port 8090.
- Internet access for dependency and embedded PostgreSQL downloads.

## Build and run locally

From an existing Ruxbase source checkout:

```sh
go build -o ruxbase ./examples/base
./ruxbase serve --dev
```

When DATABASE_URL is not set, development mode uses embedded PostgreSQL. Open
the administration dashboard at [http://localhost:8090/_/](http://localhost:8090/_/)
and follow the first-superuser setup instructions.

Development mode is not a production deployment configuration. For supported
platforms, restrictions and database behavior, see [Development mode](/ruxbase/getting-started/development/).

## Next steps

[Choose your SDK or HTTP client](/ruxbase/integrations/overview/) and [configure the example posts collection](/ruxbase/integrations/example-app/) before connecting a frontend.

Review [PocketBase compatibility](/ruxbase/migration/compatibility/),
[operations](/ruxbase/operations/overview/), and
[row-level security](/ruxbase/authorization/row-level-security/) before deploying.

The docs-quickstart workflow checks the build and development startup on a disposable Linux runner, including CLI help, health, readiness and dashboard HTML. This does not establish production readiness.
