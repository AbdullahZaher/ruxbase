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
# BENCH_RATE (20), BENCH_BASE_IMAGE and BENCH_POSTGRES_IMAGE (image overrides),
# BENCH_POSTGRES_CPUS and BENCH_RUNNER_CPUS (4 each by default),
# BENCH_PROJECT_NAME (ruxbase-bench; use a dedicated project for isolated runs),
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
PROJECT="${BENCH_PROJECT_NAME:-ruxbase-bench}"
EMAIL="bench@example.com"
PASSWORD="bench-password-123"

mkdir -p .bin results
RESULTS="results/$(date -u +%Y%m%d-%H%M%S).jsonl"
RESOURCES="${RESULTS%.jsonl}.resources.jsonl"
ENVIRONMENT="${RESULTS%.jsonl}.environment.txt"

compose() {
  docker compose --project-name "$PROJECT" -f docker-compose.yml "$@"
}

cleanup() {
  if [ -n "${PB_SOURCE:-}" ]; then rm -rf -- "$PB_SOURCE"; fi
  if [ "${compose_started:-false}" = false ]; then return; fi
  if [ -n "${BENCH_KEEP:-}" ]; then
    echo "the services are kept running (docker compose --project-name $PROJECT -f bench/docker-compose.yml down -v)"
  else
    compose down -v >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "== building the binaries"
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64

source_status=$(git -C .. status --porcelain -- . ':!bench/results')
{
  printf 'sourceCommit=%s\n' "$(git rev-parse HEAD)"
  printf 'sourceTree=%s\n' "$(git rev-parse 'HEAD^{tree}')"
  if [ -n "$source_status" ]; then
    printf 'sourceClean=false\n%s\n' "$source_status"
  else
    printf 'sourceClean=true\n'
  fi
  printf 'targets=%s\n' "$TARGETS"
  printf 'duration=%s\nconcurrency=%s\nruns=%s\nrecords=%s\n' "$DURATION" "$CONCURRENCY" "$RUNS" "$RECORDS"
  printf 'clients=%s\nevents=%s\nrate=%s\n' "$CLIENTS" "$EVENTS" "$RATE"
  printf 'startedAt=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  go version
  uname -srm
  docker info --format 'docker={{.ServerVersion}} architecture={{.Architecture}} cpus={{.NCPU}} memoryBytes={{.MemTotal}}'
  docker compose version
} > "$ENVIRONMENT"

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
  printf 'pocketbaseCommit=%s\n' "$PB_COMMIT" >> "$ENVIRONMENT"
  git -C "$PB_SOURCE" checkout --detach "$PB_COMMIT"
  PB_BINARY="$(pwd)/.bin/pocketbase"
  (cd "$PB_SOURCE" && go build -trimpath -ldflags "-s -w" -o "$PB_BINARY" ./examples/base)
  rm -rf -- "$PB_SOURCE"
  PB_SOURCE=
fi

unset CGO_ENABLED GOOS GOARCH

echo "== starting the services"
compose_started=true
compose down -v >/dev/null 2>&1 || true

record_containers() {
  docker inspect --format 'container={{.Name}} image={{.Config.Image}} imageID={{.Image}} nanoCPUs={{.HostConfig.NanoCpus}} memoryBytes={{.HostConfig.Memory}}' "$@" >> "$ENVIRONMENT"
}

prepare_postgres() {
  compose up -d postgres
  for _ in $(seq 1 60); do
    if compose exec -T postgres pg_isready -h 127.0.0.1 -U postgres -q; then
      compose exec -T postgres psql -XAt -U postgres -v ON_ERROR_STOP=1 -c "
        SELECT name || '=' || current_setting(name)
        FROM pg_settings
        WHERE name IN ('server_version', 'wal_level', 'max_connections', 'shared_buffers', 'effective_cache_size')
        ORDER BY name" >> "$ENVIRONMENT"
      return
    fi
    sleep 1
  done
  echo "timeout waiting for PostgreSQL" >&2
  return 1
}

compose up -d runner
record_containers "$(compose ps -q runner)"

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

  # Initialize only the selected target. Do not run PostgreSQL during the
  # PocketBase reference workload. Ruxbase upsert also applies its migrations.
  case "$name" in
    pocketbase)
      compose run --rm -T pocketbase /bench/pocketbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data >/dev/null
      ;;
    ruxbase)
      prepare_postgres
      compose exec -T postgres psql -Xq -U postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE single"
      compose run --rm -T ruxbase /bench/ruxbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data --dev=false >/dev/null
      ;;
    ruxbase-3-nodes)
      prepare_postgres
      compose exec -T postgres psql -Xq -U postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE cluster"
      compose run --rm -T node1 /bench/ruxbase superuser upsert "$EMAIL" "$PASSWORD" --dir=/data --dev=false >/dev/null
      ;;
  esac

  compose up -d "${services[@]}"
  for service in "${services[@]}"; do wait_http "$service" 8090; done

  # Capture the complete application stack (including PostgreSQL for Ruxbase).
  # Stats are Docker-reported CPU and memory usage, not process RSS.
  local measured_services=("${services[@]}")
  if [ "$name" != "pocketbase" ]; then measured_services+=(postgres); fi
  local ids=()
  mapfile -t ids < <(compose ps -q "${measured_services[@]}")
  if [ "${#ids[@]}" -ne "${#measured_services[@]}" ]; then
    echo "missing benchmark containers for $name" >&2
    return 1
  fi
  printf 'measurementTarget=%s\n' "$name" >> "$ENVIRONMENT"
  record_containers "${ids[@]}"
  capture_stats() {
    local phase="$1" raw sampled
    raw=$(docker stats --no-stream --format '{{json .}}' "${ids[@]}")
    sampled=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    while IFS= read -r one; do
      [ -n "$one" ] || continue
      printf '{"target":"%s","phase":"%s","sampleTime":"%s","stats":%s}\n' "$name" "$phase" "$sampled" "$one" >> "$RESOURCES"
    done <<< "$raw"
  }

  # Sampling is deliberately separate from latency JSON and is diagnostic only.
  # Start a bounded sampling pass during each load scenario.
  measured_bench() {
    local phase="$1" sampler rc=0
    shift
    (sleep 1; capture_stats "$phase") &
    sampler=$!
    bench "$@" | tee -a "$RESULTS" || rc=$?
    wait "$sampler" || return 1
    return "$rc"
  }

  echo "== $name: setup"
  bench -scenario setup -url "${url%%,*}" -email "$EMAIL" -password "$PASSWORD" -records "$RECORDS" -c 50

  [ "${#services[@]}" -gt 1 ] && wait_slot "$name" "after the setup"
  capture_stats idle

  for c in $CONCURRENCY; do
    for run in $(seq 1 "$RUNS"); do
      echo "== $name: reads (concurrency $c, run $run)"
      measured_bench "reads-c${c}-run${run}" -target "$name" -scenario reads -url "$url" -c "$c" -d "$DURATION"
    done
  done

  for c in $CONCURRENCY; do
    for run in $(seq 1 "$RUNS"); do
      echo "== $name: writes (concurrency $c, run $run)"
      measured_bench "writes-c${c}-run${run}" -target "$name" -scenario writes -url "$url" -c "$c" -d "$DURATION"
    done
  done

  [ "${#services[@]}" -gt 1 ] && wait_slot "$name" "after the writes"

  echo "== $name: realtime"
  measured_bench realtime -target "$name" -scenario realtime -url "$url" \
    -clients "$CLIENTS" -events "$EVENTS" -rate "$RATE"

  compose stop "${services[@]}" >/dev/null
}

run_target "pocketbase" "http://pocketbase:8090" pocketbase
run_target "ruxbase" "http://ruxbase:8090" ruxbase
# the load generator spreads the requests over the 3 nodes (client side load balancing)
run_target "ruxbase-3-nodes" "http://node1:8090,http://node2:8090,http://node3:8090" node1 node2 node3

echo "== results: bench/$RESULTS"
echo "== resource samples: bench/$RESOURCES"
echo "== environment and workload: bench/$ENVIRONMENT"
(cd .. && go run ./bench -scenario report -in "bench/$RESULTS")
