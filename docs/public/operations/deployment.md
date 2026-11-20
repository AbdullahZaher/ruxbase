---
title: "Deploy with Docker or a binary"
description: "Plan external PostgreSQL deployment, TLS, persistent storage and readiness checks."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Before deployment
This is a configuration guide, not a production-readiness approval. Provision external PostgreSQL, DNS/TLS, database backups, storage and a dedicated app identity. Use a reviewed build of the exact commit and keep secrets outside Git.

## Docker build
```sh
docker build -t ruxbase:local .
```
The checked-in Dockerfile builds the server and separate importer, installs PostgreSQL client utilities and runs as uid/gid 10001. For a local disposable smoke with DATABASE_URL already securely provided in the host environment:
```sh
docker run --rm -p 127.0.0.1:8090:8090 -e DATABASE_URL \
  -v ruxbase_docs_data:/app/ruxbase_data ruxbase:local
```
The database must be reachable inside the container; localhost inside a container differs from the host database. This command is an illustrative local smoke, not a production compose stack. Production requires reviewed persistent volumes, a restart policy, HTTPS termination/reverse proxy, restricted dashboard access and network boundaries. Do not publish example development credentials.

## Operations gates
Check liveness and readiness separately. Use the ready health endpoint before routing. Plan pool capacity for all instances and use direct PostgreSQL or a compatible session pooler; transaction-mode app pooling is not supported. Review [operations](/ruxbase/operations/overview/) and [recovery](/ruxbase/operations/recovery/).

## Evidence
[Source: Dockerfile](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/Dockerfile), [Source: docs/OPERATIONS.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/OPERATIONS.md), [configuration](/ruxbase/reference/configuration/). No new live deployment, reverse-proxy configuration or infrastructure certification is performed by this documentation.
