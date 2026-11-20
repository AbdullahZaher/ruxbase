#!/usr/bin/env bash
#
# Runs the ruxbase benchmarks against PocketBase v0.40.6 (built from the
# official upstream in an isolated checkout) in Docker (bench/docker-compose.yml)
# and writes the JSON results to bench/results/<date>.jsonl.
#
# Requires go, git and docker.
#
#   ./bench/run.sh
#
# Options (env variables): BENCH_DURATION (20s), BENCH_CONCURRENCY ("50 200"),
# BENCH_RUNS (3), BENCH_RECORDS (10000), BENCH_CLIENTS (500), BENCH_EVENTS (200),
# BENCH_RATE (20), BENCH_BASE_IMAGE (the base image of the containers),
# BENCH_KEEP (keep the services running), BENCH_TARGETS ("pocketbase ruxbase ruxbase-3-nodes").

set -euo pipefail

cd "$(dirname "$0")"

# keep the container paths as they are on Windows (Git Bash)
export MSYS_NO_PATHCONV=1

DURATION="${BENCH_DURATION:-20s}"
CONCURRENCY="${BENCH_CONCURRENCY:-50 200}"
RUNS="${BENCH_RUNS:-3}"
TARGETS="${BENCH_TARGETS:-pocketbase ruxbase ruxbase-3-nodes}"
RECORDS="${BENCH_RECORDS:-10000}"
CLIENTS="${BENCH_CLIENTS:-500}"
EVENTS="${BENCH_EVENTS:-200}"
RATE="${BENCH_RATE:-20}"
EMAIL="bench@example.com"
PASSWORD="bench-password-123"

mkdir -p .bin results
RESULTS="results/$(date +%Y%m%d-%H%M%S).jsonl"

compose() {
  docker compose -f docker-compose.yml "$@"
}

cleanup() {
  if [ -n "${BENCH_KEEP:-}" ]; then
    echo "the services are kept running (docker compose -f bench/docker-compose.yml down -v)"
    return
  fi
  compose down -v >/dev/null 2>&1 || true
  if [ -n "${PB_SOURCE:-}" ]; then rm -rf -- "$PB_SOURCE"; fi
}
trap cleanup EXIT

echo "== building the binaries"
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64

(cd .. && go build -trimpath -ldflags "-s -w" -o bench/.bin/ruxbase ./examples/base)
(cd .. && go build -trimpath -ldflags "-s -w" -o bench/.bin/bench ./bench)

# Build the official reference only if requested. Never assume Ruxbase contains
# an upstream tag or that a local v0.40.6 tag belongs to PocketBase.
if [[ " $TARGETS " == *" pocketbase "* ]]; then
  PB_SOURCE="$(mktemp -d)"
  git -C "$PB_SOURCE" init -q
  git -C "$PB_SOURCE" fetch --no-tags --depth=1 https://github.com/pocketbase/pocketbase.git refs/tags/v0.40.6
  PB_COMMIT="9d281ccaba65f2686eb7b0b614ccda72d1116a1b"
  test "$(git -C "$PB_SOURCE" rev-parse 'FETCH_HEAD^{commit}')" = "$PB_COMMIT"
  git -C "$PB_SOURCE" checkout --detach "$PB_COMMIT"
  PB_BINARY="$(pwd)/.bin/pocketbase"
  (cd "$PB_SOURCE" && go build -trimpath -ldflags "-s -w" -o "$PB_BINARY" ./examples/base)
  rm -rf -- "$PB_SOURCE"
  PB_SOURCE=
fi

unset CGO_ENABLED GOOS GOARCH

echo "== starting the services"
compose down -v >/dev/null 2>&1 || true
compose up -d postgres
for _ in $(seq 1 60); do
  compose exec -T postgres pg_isready -U postgres -q && break
  sleep 1
done
compose exec -T postgres psql -U postgres -qc "CREATE DATABASE single" -c "CREATE DATABASE cluster"

# the superusers (the ruxbase command also applies the migrations)
if [[ " $TARGETS " == *" pocketbase "* ]]; then
  compose run --rm -T pocketbase /bench/pocketbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data >/dev/null
fi
compose run --rm -T ruxbase /bench/ruxbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data --dev=false >/dev/null
compose run --rm -T node1 /bench/ruxbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data --dev=false >/dev/null

compose up -d runner

wait_http() {
  for _ in $(seq 1 60); do
    if compose exec -T runner bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "timeout waiting for $1:$2" && return 1
}

bench() {
  compose exec -T runner /bench/bench "$@"
}

# wait_slot waits (up to 10 minutes) for the realtime replication slot to
# decode all changes and records the time in the results.
wait_slot() {
  local name="$1" phase="$2" started lag seconds
  started=$(date +%s)

  for _ in $(seq 1 600); do
    lag=$(compose exec -T postgres psql -U postgres -Atc "SELECT COALESCE(max(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)), 0)::bigint FROM pg_replication_slots WHERE database = 'cluster'" 2>/dev/null || echo 0)
    [ "${lag:-0}" -lt 65536 ] && break
    sleep 1
  done

  seconds=$(( $(date +%s) - started ))
  if [ "$seconds" -gt 1 ]; then
    echo "{\"target\":\"$name\",\"scenario\":\"replication catch up $phase\",\"durationSeconds\":$seconds}" | tee -a "$RESULTS"
  fi
}

# run_target starts only the services of the target (so that the other targets
# don't use the shared resources, eg. the Postgres server) and runs the scenarios.
run_target() {
  local name="$1" url="$2"
  shift 2
  local services=("$@")

  case " $TARGETS " in
    *" $name "*) ;;
    *) return 0 ;;
  esac

  compose up -d "${services[@]}"
  for service in "${services[@]}"; do wait_http "$service" 8090; done

  echo "== $name: setup"
  bench -scenario setup -url "${url%%,*}" -email "$EMAIL" -password "$PASSWORD" -records "$RECORDS" -c 50

  [ "${#services[@]}" -gt 1 ] && wait_slot "$name" "after the setup"

  for c in $CONCURRENCY; do
    for run in $(seq 1 "$RUNS"); do
      echo "== $name: reads (concurrency $c, run $run)"
      bench -target "$name" -scenario reads -url "$url" -c "$c" -d "$DURATION" | tee -a "$RESULTS"
    done
  done

  for c in $CONCURRENCY; do
    for run in $(seq 1 "$RUNS"); do
      echo "== $name: writes (concurrency $c, run $run)"
      bench -target "$name" -scenario writes -url "$url" -c "$c" -d "$DURATION" | tee -a "$RESULTS"
    done
  done

  [ "${#services[@]}" -gt 1 ] && wait_slot "$name" "after the writes"

  echo "== $name: realtime"
  bench -target "$name" -scenario realtime -url "$url" \
    -clients "$CLIENTS" -events "$EVENTS" -rate "$RATE" | tee -a "$RESULTS"

  compose stop "${services[@]}" >/dev/null
}

run_target "pocketbase" "http://pocketbase:8090" pocketbase
run_target "ruxbase" "http://ruxbase:8090" ruxbase
# the load generator spreads the requests over the 3 nodes (client side load balancing)
run_target "ruxbase-3-nodes" "http://node1:8090,http://node2:8090,http://node3:8090" node1 node2 node3

echo "== results: bench/$RESULTS"
(cd .. && go run ./bench -scenario report -in "bench/$RESULTS")
