# ruxbase server image.
#
#   docker build -t ruxbase:local .
#   docker run --rm -p 8090:8090 -e DATABASE_URL=postgres://... ruxbase:local
#
# The image includes the Postgres 18 client tools (pg_dump/pg_restore) used by the backups
# and the PocketBase importer (ruxbase-import, see docs/MIGRATING.md).
#
# The server runs as the non-root user "ruxbase" (uid/gid 10001), so a bind mounted
# data directory must be writable by it (eg. chown -R 10001:10001 ./ruxbase_data).
#
# The base images are pinned by digest (updated by Dependabot).

FROM golang:1.27-trixie@sha256:7b072c590a326b9d2a43b165ae247f6e8eafff32e1ad154ab97139e89f053be9 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/AbdullahZaher/ruxbase.Version=${VERSION} -X github.com/AbdullahZaher/ruxbase/apis.MCPVersion=${VERSION}" \
    -o /out/ruxbase ./examples/base \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ruxbase-import ./cmd/ruxbase-import

FROM debian:trixie-slim@sha256:89c1ea91e897f0fe0a3731595330bc45b8bdef4047464a07116f891a94054b31

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates postgresql-common \
    && /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y \
    && apt-get install -y --no-install-recommends postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 ruxbase \
    && useradd --system --uid 10001 --gid ruxbase --home-dir /app --shell /usr/sbin/nologin ruxbase \
    && mkdir -p /app/ruxbase_data \
    && chown -R ruxbase:ruxbase /app

ENV RUXBASE_PG_DUMP=/usr/lib/postgresql/18/bin/pg_dump \
    RUXBASE_PG_RESTORE=/usr/lib/postgresql/18/bin/pg_restore

COPY --from=build /out/ruxbase /out/ruxbase-import /usr/local/bin/
COPY LICENSE.md NOTICE.md /usr/share/doc/ruxbase/
COPY THIRD-PARTY-LICENSES/ /usr/share/doc/ruxbase/THIRD-PARTY-LICENSES/

WORKDIR /app

USER 10001:10001

EXPOSE 8090

ENTRYPOINT ["ruxbase"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/app/ruxbase_data"]
