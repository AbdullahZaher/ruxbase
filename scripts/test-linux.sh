#!/usr/bin/env bash
#
# Runs "go test" in a Linux container (like the CI) against the Postgres
# server of RUXBASE_TEST_URL, reached from the container through host.docker.internal.
#
# The arguments are passed to "go test" (default: ./...).
#
#   RUXBASE_TEST_URL=postgres://user:pass@localhost:5433/ruxbase_test?sslmode=disable \
#     ./scripts/test-linux.sh -count=1 ./tests/e2e/...

set -euo pipefail

cd "$(dirname "$0")/.."

: "${RUXBASE_TEST_URL:?set RUXBASE_TEST_URL (eg. postgres://user:pass@localhost:5433/ruxbase_test?sslmode=disable)}"

IMAGE="ruxbase-test-runner:local"

export MSYS_NO_PATHCONV=1

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker build -t "$IMAGE" scripts/test-runner
fi

# localhost inside the container is the container itself
CONTAINER_URL="${RUXBASE_TEST_URL//localhost/host.docker.internal}"
CONTAINER_URL="${CONTAINER_URL//127.0.0.1/host.docker.internal}"

SRC="$(pwd)"
if command -v cygpath >/dev/null 2>&1; then
  SRC="$(cygpath -w "$SRC")"
fi

ARGS=("$@")
if [ ${#ARGS[@]} -eq 0 ]; then
  ARGS=(./...)
fi

# the tests run on a copy because some of them write in the working directory
docker run --rm \
  --add-host host.docker.internal:host-gateway \
  -v "$SRC:/src:ro" \
  -v ruxbase_gomod:/go/pkg/mod \
  -v ruxbase_gocache:/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false \
  -e RUXBASE_TEST_URL="$CONTAINER_URL" \
  -e RUXBASE_RLS_STRICT="${RUXBASE_RLS_STRICT:-}" \
  "$IMAGE" \
  sh -c 'mkdir /work && cd /src && tar --exclude=./.git -cf - . | tar -C /work -xf - && cd /work && go test "$@"' -- "${ARGS[@]}"
