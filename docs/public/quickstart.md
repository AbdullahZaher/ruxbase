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

Review [PocketBase compatibility](/ruxbase/migration/compatibility/),
[operations](/ruxbase/operations/overview/), and
[row-level security](/ruxbase/authorization/row-level-security/) before deploying.

The build and startup example is based on existing repository instructions;
an automated end-to-end quickstart verification is still pending.
