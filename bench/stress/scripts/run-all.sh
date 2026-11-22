#!/usr/bin/env bash
# Reproducible orchestrator for the Ruxbase Enterprise Stress Testing,
# Scalability Limits & Independent Performance Certification campaign.
#
# Safety guarantees:
# - Uses isolated Docker Compose project name (STRESS_PROJECT, default ruxbase-stress)
# - Always connects Ruxbase as restricted non-superuser role `ruxbase_app`
# - Always cleans up disposable containers and volumes on exit unless STRESS_KEEP=1
# - Never targets production or shared databases
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
STRESS_DIR="$(cd "$SCRIPT_DIR/.." && pwd -P)"
REPO_ROOT="$(cd "$STRESS_DIR/../.." && pwd -P)"

PROJECT="${STRESS_PROJECT:-ruxbase-stress}"
CANDIDATE_SRC="${STRESS_CANDIDATE_SRC:-$REPO_ROOT}"
RUN_ID="${STRESS_RUN_ID:-$(date -u +%Y%m%d-%H%M%S)}"
OUT_DIR="${STRESS_OUT_DIR:-$STRESS_DIR/results/$RUN_ID}"
PHASES="${STRESS_PHASES:-build env test-a test-b test-d test-f test-c test-e test-g profile pg17 test-h analyze}"

PG_SUPER_PASS="${STRESS_PG_SUPER_PASSWORD:-stress-super-disposable}"
APP_PASS="${STRESS_APP_PASSWORD:-stress-app-disposable}"
WAL_PASS="${STRESS_WAL_PASSWORD:-stress-wal-disposable}"
ADMIN_EMAIL="stress-admin@example.com"
ADMIN_PASS="StressAdminPass!2026"

PG18_IMAGE="${STRESS_PG18_IMAGE:-pgvector/pgvector:0.8.7-pg18-trixie}"
PG17_IMAGE="${STRESS_PG17_IMAGE:-pgvector/pgvector:0.8.7-pg17-trixie}"

STAGE_DUR="${STRESS_STAGE_DUR:-10s}"
WARMUP_DUR="${STRESS_WARMUP_DUR:-3s}"
ENDURANCE_DUR="${STRESS_ENDURANCE_DUR:-600s}"
TEST_C_SIZES="${STRESS_TEST_C_SIZES:-10000,100000,1000000}"

mkdir -p "$STRESS_DIR/.bin" "$OUT_DIR" "$OUT_DIR/profiles"

METRICS_JSONL="$OUT_DIR/metrics.jsonl"
RESOURCES_JSONL="$OUT_DIR/metrics.resources.jsonl"
ENV_FILE="$OUT_DIR/environment.txt"

MONITOR_PID=""

compose() {
  docker compose --project-name "$PROJECT" -f "$STRESS_DIR/docker-compose.yml" "$@"
}

stop_monitor() {
  if [ -n "${MONITOR_PID:-}" ]; then
    kill "$MONITOR_PID" 2>/dev/null || true
    wait "$MONITOR_PID" 2>/dev/null || true
    MONITOR_PID=""
  fi
}

start_monitor() {
  local phase="$1"
  stop_monitor
  "$SCRIPT_DIR/monitor.sh" "$PROJECT" "$RESOURCES_JSONL" 2 "$phase" &
  MONITOR_PID=$!
}

cleanup() {
  stop_monitor
  if [ -z "${STRESS_KEEP:-}" ]; then
    compose down -v >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

has_phase() {
  case " $PHASES " in
    *" $1 "*) return 0 ;;
    *) return 1 ;;
  esac
}

wait_pg() {
  for _ in $(seq 1 60); do
    if compose exec -T postgres pg_isready -h 127.0.0.1 -U postgres -q 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "ERROR: PostgreSQL failed to become ready" >&2
  return 1
}

wait_http() {
  local host="$1" port="${2:-8090}"
  for _ in $(seq 1 60); do
    if compose exec -T runner bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "ERROR: $host:$port failed to become reachable" >&2
  return 1
}

init_pg_stack() {
  local pg_image="$1"
  local pg_cpus="${2:-2.5}"
  local pg_mem="${3:-3584m}"
  export STRESS_POSTGRES_IMAGE="$pg_image"
  export STRESS_PG_CPUS="$pg_cpus"
  export STRESS_PG_MEM="$pg_mem"
  echo "== Starting isolated PostgreSQL ($pg_image, cpus=$pg_cpus, mem=$pg_mem)"
  compose down -v >/dev/null 2>&1 || true
  STRESS_PG_SUPER_PASSWORD="$PG_SUPER_PASS" STRESS_APP_PASSWORD="$APP_PASS" \
    compose up -d postgres runner
  wait_pg
  compose exec -T postgres psql -U postgres -v ON_ERROR_STOP=1 \
    -v app_password="$APP_PASS" -v wal_password="$WAL_PASS" -f /stress/init-stress-db.sql >/dev/null
}

bootstrap_ruxbase_db() {
  local svc="$1" db="$2"
  STRESS_SINGLE_DB="$db" STRESS_APP_PASSWORD="$APP_PASS" \
    compose run --rm -T "$svc" /bench/ruxbase superuser upsert "$ADMIN_EMAIL" "$ADMIN_PASS" --dir=/data --dev=false >/dev/null
}

run_stress() {
  compose exec -T runner /bench/stress "$@"
}

# ---------------------------------------------------------------------------
# Phase: build
# ---------------------------------------------------------------------------
if has_phase "build"; then
  echo "== Building native Linux stress binaries"
  DOCKER_ARCH=$(docker info --format '{{.Architecture}}')
  case "$DOCKER_ARCH" in
    aarch64|arm64) TARGET_GOARCH="arm64" ;;
    x86_64|amd64)  TARGET_GOARCH="amd64" ;;
    *) echo "Unsupported Docker arch: $DOCKER_ARCH" >&2; exit 1 ;;
  esac
  echo "   Docker arch=$DOCKER_ARCH -> CGO_ENABLED=0 GOOS=linux GOARCH=$TARGET_GOARCH"
  echo "   Candidate source=$CANDIDATE_SRC"

  (cd "$CANDIDATE_SRC" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags "-s -w" -o "$STRESS_DIR/.bin/ruxbase" ./examples/base)
  (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags "-s -w" -o "$STRESS_DIR/.bin/stress" ./bench/stress/cmd/stress)
  (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags "-s -w" -o "$STRESS_DIR/.bin/corestress" ./bench/stress/cmd/corestress)
  (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags "-s -w" -o "$STRESS_DIR/.bin/ruxbase-pprof" ./bench/stress/cmd/ruxbase-pprof)
  (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" go build -trimpath -ldflags "-s -w" -o "$STRESS_DIR/.bin/netproxy" ./bench/stress/cmd/netproxy)
fi

# ---------------------------------------------------------------------------
# Phase: env
# ---------------------------------------------------------------------------
if has_phase "env"; then
  echo "== Capturing complete environment & provenance manifest -> $ENV_FILE"
  {
    printf 'runId=%s\n' "$RUN_ID"
    printf 'startedAt=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'candidateSource=%s\n' "$CANDIDATE_SRC"
    printf 'candidateCommit=%s\n' "$(git -C "$CANDIDATE_SRC" rev-parse HEAD)"
    printf 'candidateTree=%s\n' "$(git -C "$CANDIDATE_SRC" rev-parse 'HEAD^{tree}')"
    cand_dirty=$(git -C "$CANDIDATE_SRC" status --porcelain)
    if [ -z "$cand_dirty" ]; then printf 'candidateClean=true\n'; else printf 'candidateClean=false\n%s\n' "$cand_dirty"; fi
    printf 'goVersion=%s\n' "$(go version)"
    printf 'hostOS=%s\n' "$(uname -a)"
    if command -v sysctl >/dev/null 2>&1; then
      printf 'hostCPU=%s\n' "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)"
      printf 'hostLogicalCPUs=%s\n' "$(sysctl -n hw.logicalcpu 2>/dev/null || true)"
      printf 'hostMemBytes=%s\n' "$(sysctl -n hw.memsize 2>/dev/null || true)"
    fi
    docker info --format 'dockerVersion={{.ServerVersion}} arch={{.Architecture}} cpus={{.NCPU}} memBytes={{.MemTotal}} cgroupDriver={{.CgroupDriver}} cgroupVersion={{.CgroupVersion}}'
    printf 'pg18Image=%s\n' "$PG18_IMAGE"
    printf 'pg17Image=%s\n' "$PG17_IMAGE"
    printf 'stageDuration=%s\nwarmupDuration=%s\nenduranceDuration=%s\ntestCSizes=%s\n' "$STAGE_DUR" "$WARMUP_DUR" "$ENDURANCE_DUR" "$TEST_C_SIZES"
    echo "--- binary sha256 ---"
    shasum -a 256 "$STRESS_DIR"/.bin/* 2>/dev/null || sha256sum "$STRESS_DIR"/.bin/*
  } > "$ENV_FILE"
fi

PG18_ADMIN_SINGLE="postgres://postgres:${PG_SUPER_PASS}@postgres:5432/stress_single?sslmode=disable"
PG18_APP_SINGLE="postgres://ruxbase_app:${APP_PASS}@postgres:5432/stress_single?sslmode=disable"
PG18_ADMIN_PGC="postgres://postgres:${PG_SUPER_PASS}@postgres:5432/stress_pgc?sslmode=disable"
PG18_APP_PGC="postgres://ruxbase_app:${APP_PASS}@postgres:5432/stress_pgc?sslmode=disable"
PG18_ADMIN_CLUSTER="postgres://postgres:${PG_SUPER_PASS}@postgres:5432/stress_cluster?sslmode=disable"
PG18_APP_CORE="postgres://ruxbase_app:${APP_PASS}@postgres:5432/stress_core?sslmode=disable"
PG18_ADMIN_END="postgres://postgres:${PG_SUPER_PASS}@postgres:5432/stress_endurance?sslmode=disable"

# ---------------------------------------------------------------------------
# Phase: test-a (PG18 Progressive Load: default pool=20, tuned pool=60, RLS strict=true)
# ---------------------------------------------------------------------------
if has_phase "test-a"; then
  echo "========== TEST A: Progressive Load Testing (PG18) =========="
  init_pg_stack "$PG18_IMAGE" 2.5 3584m
  # Record effective PG18 settings into environment.txt
  {
    echo "--- PG18 effective settings ---"
    compose exec -T postgres psql -XAt -U postgres -d stress_single -c "
      SELECT name || '=' || current_setting(name)
      FROM pg_settings
      WHERE name IN ('server_version','max_connections','shared_buffers','effective_cache_size','work_mem','wal_level','shared_preload_libraries','track_io_timing','deadlock_timeout','max_wal_size')
      ORDER BY name"
  } >> "$ENV_FILE"

  bootstrap_ruxbase_db ruxbase stress_single
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=20 STRESS_RLS_STRICT=false compose up -d ruxbase
  wait_http ruxbase 8090

  run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 5000 -state /tmp/state_single.json | tee -a "$METRICS_JSONL"

  # A-Default: RUXBASE_DB_MAX_CONNS=20, RUXBASE_RLS_STRICT=false
  start_monitor "test-a-pg18-pool20"
  run_stress -scenario test-a -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=false -pool-max=20 \
    -stages "10,50,100,250,500,1000,2000,5000" -repeat-stages "50,100,250,500" \
    -open-rates "250,500,1000,2000,3500,5000" -warmup "$WARMUP_DUR" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_single.json -label "A-PG18-POOL20" | tee -a "$METRICS_JSONL"

  # A-Tuned: RUXBASE_DB_MAX_CONNS=60, RUXBASE_RLS_STRICT=false
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  start_monitor "test-a-pg18-pool60"
  run_stress -scenario test-a -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=false -pool-max=60 \
    -stages "10,50,100,250,500,1000,2000" -repeat-stages "50,100,250" \
    -open-rates "500,1000,2000,3500,5000" -warmup "$WARMUP_DUR" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  # A-StrictRLS: RUXBASE_DB_MAX_CONNS=60, RUXBASE_RLS_STRICT=true
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=true compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  start_monitor "test-a-pg18-strict-rls"
  run_stress -scenario test-a -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=true -pool-max=60 \
    -stages "10,25,50,100" -repeat-stages "25,50" \
    -open-rates "5,15,30,60" -warmup "$WARMUP_DUR" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor
  sleep 2
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_single.json -label "A-PG18-STRICT" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-b (Breaking Point & Recovery)
# ---------------------------------------------------------------------------
if has_phase "test-b"; then
  echo "========== TEST B: Breaking Point & Recovery (PG18) =========="
  # Ensure stack is up from test-a or initialize cleanly
  if ! compose exec -T runner test -f /tmp/state_single.json 2>/dev/null; then
    init_pg_stack "$PG18_IMAGE" 2.5 3584m
    bootstrap_ruxbase_db ruxbase stress_single
    STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=20 STRESS_RLS_STRICT=false compose up -d ruxbase
    wait_http ruxbase 8090
    run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
      -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 5000 -state /tmp/state_single.json | tee -a "$METRICS_JSONL"
  fi

  # B1: Default pool=20
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=20 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  start_monitor "test-b-pool20"
  run_stress -scenario test-b -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=false -pool-max=20 -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  # B2: Tuned pool=60
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  start_monitor "test-b-pool60"
  run_stress -scenario test-b -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=false -pool-max=60 -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  # B3: Strict RLS pool=60
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=true compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  start_monitor "test-b-strict-rls"
  run_stress -scenario test-b -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=true -pool-max=60 -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_single.json -label "B-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-d (API & Authentication Stress + Official PocketBase JS SDK)
# ---------------------------------------------------------------------------
if has_phase "test-d"; then
  echo "========== TEST D: API, Auth & JS SDK Stress (PG18) =========="
  if ! compose exec -T runner test -f /tmp/state_single.json 2>/dev/null; then
    init_pg_stack "$PG18_IMAGE" 2.5 3584m
    bootstrap_ruxbase_db ruxbase stress_single
    STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase
    wait_http ruxbase 8090
    run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
      -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 5000 -state /tmp/state_single.json | tee -a "$METRICS_JSONL"
  fi
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090

  start_monitor "test-d-api-auth"
  run_stress -scenario test-d -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -rls-strict=false -pool-max=60 -c 100 -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_single.json -label "D-POST" | tee -a "$METRICS_JSONL"

  # Official PocketBase JS SDK concurrent stress via temporary loopback-mapped port on host Node
  echo "== Running Official PocketBase JS SDK concurrent stress"
  compose exec -T runner cat /tmp/state_single.json > "$OUT_DIR/state_single.tmp.json"
  docker run --rm --network "${PROJECT}_default" -p 127.0.0.1:18090:8090 --name "${PROJECT}-sdk-fwd" \
    -v "$STRESS_DIR/.bin:/bench:ro" --entrypoint /bench/netproxy "$PG18_IMAGE" \
    -listen 0.0.0.0:8090 -target ruxbase:8090 -admin 127.0.0.1:9091 >/dev/null 2>&1 &
  FWD_PID=$!
  sleep 2
  (cd "$REPO_ROOT" && node bench/stress/sdk_stress.mjs --url http://127.0.0.1:18090 --concurrency 25 --duration 10 --state "$OUT_DIR/state_single.tmp.json") | tee -a "$METRICS_JSONL" || true
  docker rm -f "${PROJECT}-sdk-fwd" >/dev/null 2>&1 || true
  wait "$FWD_PID" 2>/dev/null || true
  rm -f "$OUT_DIR/state_single.tmp.json"
fi

# ---------------------------------------------------------------------------
# Phase: test-f (Jobs, Storage, Functions, AI/Vector)
# ---------------------------------------------------------------------------
if has_phase "test-f"; then
  echo "========== TEST F: Jobs, Storage, Functions & Vector (PG18) =========="
  if ! compose exec -T runner test -f /tmp/state_single.json 2>/dev/null; then
    init_pg_stack "$PG18_IMAGE" 2.5 3584m
    bootstrap_ruxbase_db ruxbase stress_single
    STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase
    wait_http ruxbase 8090
    run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
      -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 5000 -state /tmp/state_single.json | tee -a "$METRICS_JSONL"
  fi
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090

  start_monitor "test-f-http"
  run_stress -scenario test-f-http -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_single.json -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  # Core subsystems (Durable Jobs, Outbox, StorageManager, Multipart, Functions)
  bootstrap_ruxbase_db ruxbase stress_core
  start_monitor "test-f-core"
  compose exec -T runner /bench/corestress -db "$PG18_APP_CORE" -dir /tmp/corestress_data -pool 30 -jobs 3000 -workers 20 -fn-calls 1000 | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_single.json -label "F-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-c (PostgreSQL Extreme Concurrency: 10k, 100k, 1M rows + deadlocks + EXPLAIN)
# ---------------------------------------------------------------------------
if has_phase "test-c"; then
  echo "========== TEST C: PostgreSQL Extreme Concurrency (10k / 100k / 1M) =========="
  init_pg_stack "$PG18_IMAGE" 2.5 3584m
  bootstrap_ruxbase_db ruxbase stress_pgc
  STRESS_SINGLE_DB=stress_pgc STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase
  wait_http ruxbase 8090
  run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_PGC" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 0 -state /tmp/state_pgc.json | tee -a "$METRICS_JSONL"

  start_monitor "test-c-pg18"
  run_stress -scenario test-c -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_PGC" -pg-app-url "$PG18_APP_PGC" \
    -state /tmp/state_pgc.json -sizes "$TEST_C_SIZES" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_PGC" -state /tmp/state_pgc.json -label "C-PG18-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-e (Realtime & Distributed Scaling: 1, 2, 3 Nodes)
# ---------------------------------------------------------------------------
if has_phase "test-e"; then
  echo "========== TEST E: Realtime & Horizontal Scaling (1, 2, 3 Nodes, Zero Oversubscription) =========="
  # Strict zero-oversubscription budget: PG=2.0 CPUs, each node=1.0 CPU (up to 3), runner=1.0 CPU => max 6.0 CPUs!
  init_pg_stack "$PG18_IMAGE" 2.0 3072m
  STRESS_RUNNER_CPUS=1.0 STRESS_RUNNER_MEM=1536m compose up -d --force-recreate runner

  STRESS_CLUSTER_DB_MAX_CONNS=25 STRESS_CLUSTER_RT_SOURCE=replication STRESS_APP_PASSWORD="$APP_PASS" \
    compose run --rm -T node1 /bench/ruxbase superuser upsert "$ADMIN_EMAIL" "$ADMIN_PASS" --dir=/data --dev=false >/dev/null

  # 1-Node baseline (node1 only, 1.0 CPU)
  STRESS_NODE_CPUS=1.0 STRESS_NODE_MEM=1024m STRESS_CLUSTER_DB_MAX_CONNS=25 STRESS_CLUSTER_RT_SOURCE=replication \
    compose up -d node1
  wait_http node1 8090
  run_stress -scenario setup -url http://node1:8090 -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 0 -state /tmp/state_cluster.json | tee -a "$METRICS_JSONL"
  sleep 2

  start_monitor "test-e-1node"
  run_stress -scenario test-e -url "http://node1:8090" -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -state /tmp/state_cluster.json -clients "50,200,500,1000" -events 80 -rate 20 -auth-sse=false -api-dur "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  run_stress -scenario test-e -url "http://node1:8090" -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -state /tmp/state_cluster.json -clients "50,200,500" -events 50 -rate 15 -auth-sse=true -api-dur 0s | tee -a "$METRICS_JSONL"
  stop_monitor

  # 2-Node cluster (node1 + node2, 1.0 CPU each)
  STRESS_NODE_CPUS=1.0 STRESS_NODE_MEM=1024m STRESS_CLUSTER_DB_MAX_CONNS=25 STRESS_CLUSTER_RT_SOURCE=replication \
    compose up -d node1 node2
  wait_http node2 8090
  sleep 3
  start_monitor "test-e-2nodes"
  run_stress -scenario test-e -url "http://node1:8090,http://node2:8090" -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -state /tmp/state_cluster.json -clients "50,200,500,1000" -events 80 -rate 20 -auth-sse=false -api-dur "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor

  # 3-Node cluster (node1 + node2 + node3, 1.0 CPU each)
  STRESS_NODE_CPUS=1.0 STRESS_NODE_MEM=1024m STRESS_CLUSTER_DB_MAX_CONNS=25 STRESS_CLUSTER_RT_SOURCE=replication \
    compose up -d node1 node2 node3
  wait_http node3 8090
  sleep 3
  start_monitor "test-e-3nodes"
  run_stress -scenario test-e -url "http://node1:8090,http://node2:8090,http://node3:8090" -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -state /tmp/state_cluster.json -clients "50,200,500,1000,1500" -events 80 -rate 20 -auth-sse=false -api-dur "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  run_stress -scenario test-e -url "http://node1:8090,http://node2:8090,http://node3:8090" -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -state /tmp/state_cluster.json -clients "50,200,500" -events 50 -rate 15 -auth-sse=true -api-dur 0s | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://node1:8090 -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_cluster.json -label "E-3NODES-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-g (Traffic Spikes & Controlled Chaos Drills)
# ---------------------------------------------------------------------------
if has_phase "test-g"; then
  echo "========== TEST G: Spike & Chaos Testing =========="
  init_pg_stack "$PG18_IMAGE" 2.0 3072m
  STRESS_CLUSTER_DB_MAX_CONNS=30 STRESS_CLUSTER_RT_SOURCE=replication STRESS_APP_PASSWORD="$APP_PASS" \
    compose run --rm -T node1 /bench/ruxbase superuser upsert "$ADMIN_EMAIL" "$ADMIN_PASS" --dir=/data --dev=false >/dev/null
  STRESS_NODE_CPUS=1.0 STRESS_NODE_MEM=1024m STRESS_CLUSTER_DB_MAX_CONNS=30 STRESS_CLUSTER_RT_SOURCE=replication \
    compose up -d node1 node2 node3 netproxy
  wait_http node1 8090; wait_http node2 8090; wait_http node3 8090

  run_stress -scenario setup -url http://node1:8090 -pg-admin-url "$PG18_ADMIN_CLUSTER" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 0 -state /tmp/state_chaos.json | tee -a "$METRICS_JSONL"

  # G1-G3: Traffic Spikes & Workload Shift
  start_monitor "test-g-spikes"
  run_stress -scenario test-g -submode spikes -url "http://node1:8090,http://node2:8090,http://node3:8090" \
    -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_chaos.json | tee -a "$METRICS_JSONL"
  stop_monitor

  # G4: Chaos Drill 1 — Restart node1 during active 3-node traffic
  echo "== Chaos Drill 1: Restarting node1 under load"
  start_monitor "test-g-chaos-restart"
  run_stress -scenario test-g -submode chaos-probe -url "http://node1:8090,http://node2:8090,http://node3:8090" \
    -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_chaos.json -d 28s > "$OUT_DIR/chaos_node_restart.tmp.jsonl" &
  PROBE_PID=$!
  sleep 6
  T_FAULT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  compose restart node1 >/dev/null
  wait_http node1 8090
  T_RECOV=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  wait "$PROBE_PID"
  jq -c --arg tf "$T_FAULT" --arg tr "$T_RECOV" '.testId="G4-NODE-RESTART" | .scenario="chaos-node1-restart" | .extras.faultAt=$tf | .extras.recoveredAt=$tr' "$OUT_DIR/chaos_node_restart.tmp.jsonl" | tee -a "$METRICS_JSONL"
  rm -f "$OUT_DIR/chaos_node_restart.tmp.jsonl"
  stop_monitor

  # G5: Chaos Drill 2 — Kill PostgreSQL LISTEN connection via pg_terminate_backend
  echo "== Chaos Drill 2: Terminating PostgreSQL LISTEN backends"
  start_monitor "test-g-chaos-listen"
  run_stress -scenario test-g -submode chaos-probe -url "http://node1:8090,http://node2:8090" \
    -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_chaos.json -d 20s > "$OUT_DIR/chaos_listen.tmp.jsonl" &
  PROBE_PID=$!
  sleep 5
  KILLED=$(compose exec -T postgres psql -XAt -U postgres -d stress_cluster -c "SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname='stress_cluster' AND query ILIKE 'LISTEN%'")
  wait "$PROBE_PID"
  jq -c --argjson k "${KILLED:-0}" '.testId="G5-LISTEN-KILL" | .scenario="chaos-pg-listen-terminate" | .extras.listenBackendsTerminated=$k' "$OUT_DIR/chaos_listen.tmp.jsonl" | tee -a "$METRICS_JSONL"
  rm -f "$OUT_DIR/chaos_listen.tmp.jsonl"
  stop_monitor

  # G6: Chaos Drill 3 — Temporary PostgreSQL unavailability (docker pause 8s -> unpause)
  echo "== Chaos Drill 3: Pausing PostgreSQL for 8 seconds then restoring"
  start_monitor "test-g-chaos-pg-pause"
  run_stress -scenario test-g -submode chaos-probe -url "http://node1:8090" \
    -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_chaos.json -d 30s > "$OUT_DIR/chaos_pg_pause.tmp.jsonl" &
  PROBE_PID=$!
  sleep 5
  compose pause postgres >/dev/null
  sleep 8
  compose unpause postgres >/dev/null
  wait "$PROBE_PID"
  jq -c '.testId="G6-PG-PAUSE" | .scenario="chaos-postgres-8s-outage"' "$OUT_DIR/chaos_pg_pause.tmp.jsonl" | tee -a "$METRICS_JSONL"
  rm -f "$OUT_DIR/chaos_pg_pause.tmp.jsonl"
  stop_monitor

  # G7: Chaos Drill 4 — Network Latency Injection (50ms +/- 10ms DB link via netproxy)
  echo "== Chaos Drill 4: Network latency injection (50ms +/- 10ms on PostgreSQL TCP link)"
  bootstrap_ruxbase_db ruxbase stress_single
  STRESS_SINGLE_DB=stress_single STRESS_DB_HOST=netproxy STRESS_DB_PORT=5432 STRESS_DB_MAX_CONNS=40 STRESS_RLS_STRICT=false \
    compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 5000 -vectors 0 -state /tmp/state_proxy.json >/dev/null

  start_monitor "test-g-net-latency"
  # 0ms proxy baseline vs 25ms one-way (50ms RTT) +/- 5ms jitter
  compose exec -T runner bash -c 'exec 3<>/dev/tcp/netproxy/9090; printf "POST /config HTTP/1.1\r\nHost: netproxy\r\nContent-Type: application/json\r\nContent-Length: 43\r\nConnection: close\r\n\r\n{\"latencyMs\":0,\"jitterMs\":0,\"dropNew\":false}" >&3; cat <&3 >/dev/null'
  run_stress -scenario test-g -submode chaos-probe -url "http://ruxbase:8090" \
    -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_proxy.json -d 10s | \
    jq -c '.testId="G7-NET-LATENCY-0MS" | .scenario="chaos-netproxy-0ms-baseline"' | tee -a "$METRICS_JSONL"

  compose exec -T runner bash -c 'exec 3<>/dev/tcp/netproxy/9090; printf "POST /config HTTP/1.1\r\nHost: netproxy\r\nContent-Type: application/json\r\nContent-Length: 44\r\nConnection: close\r\n\r\n{\"latencyMs\":25,\"jitterMs\":5,\"dropNew\":false}" >&3; cat <&3 >/dev/null'
  run_stress -scenario test-g -submode chaos-probe -url "http://ruxbase:8090" \
    -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_proxy.json -d 12s | \
    jq -c '.testId="G7-NET-LATENCY-50MS-RTT" | .scenario="chaos-netproxy-50ms-rtt-jitter"' | tee -a "$METRICS_JSONL"
  stop_monitor

  run_stress -scenario verify -url http://node1:8090 -pg-admin-url "$PG18_ADMIN_CLUSTER" -state /tmp/state_chaos.json -label "G-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: profile (Section 13 Go pprof capture under real stress)
# ---------------------------------------------------------------------------
if has_phase "profile"; then
  echo "========== SECTION 13: Go pprof Profiling Capture =========="
  init_pg_stack "$PG18_IMAGE" 2.5 3584m
  bootstrap_ruxbase_db ruxbase stress_single
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase-pprof
  wait_http ruxbase-pprof 8090
  run_stress -scenario setup -url http://ruxbase-pprof:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 2000 -state /tmp/state_pprof.json >/dev/null

  # Capture 15s CPU profile + heap + goroutine + mutex during 250-VU RLS mixed load
  run_stress -scenario test-a -url http://ruxbase-pprof:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_pprof.json -stages "250" -repeat-stages "" -open-rates "" -warmup 2s -d 18s >/dev/null &
  LOAD_PID=$!
  sleep 3
  python3 -c "
import urllib.request
for name, url in [
    ('cpu_rls_250vu.pprof', 'http://127.0.0.1:6060/debug/pprof/profile?seconds=12'),
    ('heap_rls_250vu.pprof', 'http://127.0.0.1:6060/debug/pprof/heap'),
    ('allocs_rls_250vu.pprof', 'http://127.0.0.1:6060/debug/pprof/allocs'),
    ('goroutine_rls_250vu.pprof', 'http://127.0.0.1:6060/debug/pprof/goroutine'),
    ('mutex_rls_250vu.pprof', 'http://127.0.0.1:6060/debug/pprof/mutex'),
]:
    try:
        with urllib.request.urlopen(url, timeout=30) as r:
            open('$OUT_DIR/profiles/' + name, 'wb').write(r.read())
    except Exception as e:
        print(f'pprof fetch {name} err: {e}')
"
  wait "$LOAD_PID" || true

  # Export concise human-readable text top tables and remove heavy binary pprofs if desired (keep compact summaries)
  for pf in "$OUT_DIR/profiles"/*.pprof; do
    [ -f "$pf" ] || continue
    go tool pprof -top -nodecount=30 "$pf" > "${pf%.pprof}.top.txt" 2>/dev/null || true
  done
fi

# ---------------------------------------------------------------------------
# Phase: pg17 (PostgreSQL 17 Parity Stress Matrix)
# ---------------------------------------------------------------------------
if has_phase "pg17"; then
  echo "========== PG17 PARITY STRESS MATRIX =========="
  init_pg_stack "$PG17_IMAGE" 2.5 3584m
  {
    echo "--- PG17 effective settings ---"
    compose exec -T postgres psql -XAt -U postgres -d stress_single -c "
      SELECT name || '=' || current_setting(name)
      FROM pg_settings
      WHERE name IN ('server_version','max_connections','shared_buffers','effective_cache_size','work_mem','wal_level','shared_preload_libraries')
      ORDER BY name"
  } >> "$ENV_FILE"

  bootstrap_ruxbase_db ruxbase stress_single
  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase
  wait_http ruxbase 8090
  run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 0 -state /tmp/state_pg17.json | tee -a "$METRICS_JSONL"

  start_monitor "pg17-parity"
  run_stress -scenario test-a -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_pg17.json -rls-strict=false -pool-max=60 \
    -stages "10,50,100,250,500" -repeat-stages "100" \
    -open-rates "500,1500,3000" -warmup "$WARMUP_DUR" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"

  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=true compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  run_stress -scenario test-a -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" \
    -state /tmp/state_pg17.json -rls-strict=true -pool-max=60 \
    -stages "10,25,50" -repeat-stages "" -open-rates "" -warmup "$WARMUP_DUR" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"

  STRESS_SINGLE_DB=stress_single STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d --force-recreate ruxbase
  wait_http ruxbase 8090
  run_stress -scenario test-c -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -pg-app-url "$PG18_APP_SINGLE" \
    -state /tmp/state_pg17.json -sizes "10000,100000" -d "$STAGE_DUR" | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_SINGLE" -state /tmp/state_pg17.json -label "PG17-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: test-h (Sustained Endurance & Memory Stability)
# ---------------------------------------------------------------------------
if has_phase "test-h"; then
  echo "========== TEST H: Sustained Endurance & Memory Stability ($ENDURANCE_DUR) =========="
  init_pg_stack "$PG18_IMAGE" 2.5 3584m
  bootstrap_ruxbase_db ruxbase stress_endurance
  STRESS_SINGLE_DB=stress_endurance STRESS_DB_MAX_CONNS=60 STRESS_RLS_STRICT=false compose up -d ruxbase
  wait_http ruxbase 8090
  run_stress -scenario setup -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_END" \
    -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" -records 10000 -vectors 0 -state /tmp/state_endurance.json | tee -a "$METRICS_JSONL"

  start_monitor "test-h-endurance"
  run_stress -scenario test-h -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_END" \
    -state /tmp/state_endurance.json -c 80 -d "$ENDURANCE_DUR" -sample 5s | tee -a "$METRICS_JSONL"
  stop_monitor
  run_stress -scenario verify -url http://ruxbase:8090 -pg-admin-url "$PG18_ADMIN_END" -state /tmp/state_endurance.json -label "H-ENDURANCE-POST" | tee -a "$METRICS_JSONL"
fi

# ---------------------------------------------------------------------------
# Phase: analyze
# ---------------------------------------------------------------------------
if has_phase "analyze"; then
  echo "========== ANALYZING RESULTS & GENERATING SHA-256 MANIFEST =========="
  python3 "$SCRIPT_DIR/analyze.py" "$OUT_DIR"
fi

echo "== Stress suite completed. Artifacts in: $OUT_DIR"
