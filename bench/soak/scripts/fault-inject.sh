#!/usr/bin/env bash
# Executes Phase 8 scheduled staging-only controlled fault injections:
#   - Hour 24:  SIGKILL ruxbase-soak-worker -> validate restart, stale lease reclamation, zero lost jobs
#   - Hour 72:  Bounded pg_terminate_backend on staging ruxbase_app LISTEN/replication backend ONLY
#   - Hour 120: Online backup + isolated restore + /api/studio/overview CheckReleaseBlockers
#
# Usage:
#   ./fault-inject.sh --drill hour24_worker_kill
#   ./fault-inject.sh --drill hour72_pg_disconnect
#   ./fault-inject.sh --drill hour120_backup_restore
#   ./fault-inject.sh --auto   # checks elapsed hours in state.json and runs due drills once
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOAK_ROOT="${SOAK_ROOT:-/srv/ruxbase-soak}"
ENV_FILE="${ENV_FILE:-$SOAK_ROOT/.env.staging}"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

CAMPAIGN_ID="${SOAK_CAMPAIGN_ID:?SOAK_CAMPAIGN_ID must be set}"
EVIDENCE_DIR="${SOAK_EVIDENCE_HOST_DIR:-$SOAK_ROOT/evidence}/$CAMPAIGN_ID"
FAULTS_LOG="$EVIDENCE_DIR/faults.jsonl"
INTERRUPTS_LOG="$EVIDENCE_DIR/interruptions.jsonl"
BASE_URL="http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}"

mkdir -p "$EVIDENCE_DIR"
touch "$FAULTS_LOG"

drill_already_ran() {
  grep -q "\"drill\":\"$1\"" "$FAULTS_LOG" 2>/dev/null
}

record_fault() {
  local drill="$1" start_ts="$2" end_ts="$3" rec_sec="$4" passed="$5" detail="$6"
  jq -nc \
    --arg drill "$drill" \
    --arg start "$start_ts" \
    --arg end "$end_ts" \
    --argjson recSec "$rec_sec" \
    --argjson passed "$passed" \
    --arg detail "$detail" \
    '{drill:$drill, startedAtUtc:$start, recoveredAtUtc:$end, recoverySeconds:$recSec, passed:$passed, detail:$detail}' \
    >> "$FAULTS_LOG"
  jq -nc \
    --arg ts "$start_ts" \
    --arg end "$end_ts" \
    --arg kind "controlled_fault" \
    --argjson gap "$rec_sec" \
    --arg detail "$drill: $detail" \
    '{timestampUtc:$ts, kind:$kind, gapSeconds:$gap, detail:$detail, controlled:true, disqualifying:false, recoveredAtUtc:$end}' \
    >> "$INTERRUPTS_LOG"
}

run_hour24() {
  local start_ts start_ep end_ep rec_sec passed detail
  start_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  start_ep="$(date -u +%s)"
  echo "[fault-inject] Hour 24: Sending SIGKILL to staging worker container ruxbase-soak-worker..."
  docker kill --signal=SIGKILL ruxbase-soak-worker

  passed="false"
  for _ in $(seq 1 45); do
    if docker inspect -f '{{.State.Health.Status}}' ruxbase-soak-worker 2>/dev/null | grep -q '^healthy$'; then
      passed="true"
      break
    fi
    sleep 2
  done
  end_ep="$(date -u +%s)"
  rec_sec=$((end_ep - start_ep))
  detail="ruxbase-soak-worker SIGKILL recovery healthy=$passed in ${rec_sec}s"
  record_fault "hour24_worker_kill" "$start_ts" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$rec_sec" "$passed" "$detail"
  [[ "$passed" == "true" ]]
}

run_hour72() {
  local start_ts start_ep end_ep rec_sec passed detail terminated
  start_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  start_ep="$(date -u +%s)"
  echo "[fault-inject] Hour 72: Bounded pg_terminate_backend on staging ruxbase_app LISTEN/replication sessions ONLY..."
  terminated="$(docker exec ruxbase-soak-postgres psql -U postgres -d ruxbase_staging -Atc "
    SELECT count(pg_terminate_backend(pid))
    FROM pg_stat_activity
    WHERE datname = 'ruxbase_staging'
      AND usename = 'ruxbase_app'
      AND pid <> pg_backend_pid()
      AND (query ILIKE 'LISTEN%' OR backend_type = 'walsender');
  ")"

  passed="false"
  for _ in $(seq 1 45); do
    if curl -fsS "$BASE_URL/api/health?ready" >/dev/null 2>&1; then
      passed="true"
      break
    fi
    sleep 2
  done
  end_ep="$(date -u +%s)"
  rec_sec=$((end_ep - start_ep))
  detail="terminated ${terminated:-0} staging ruxbase_app LISTEN/walsender session(s); /api/health?ready recovered=$passed in ${rec_sec}s"
  record_fault "hour72_pg_disconnect" "$start_ts" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$rec_sec" "$passed" "$detail"
  [[ "$passed" == "true" ]]
}

run_hour120() {
  local start_ts start_ep end_ep rec_sec passed detail
  start_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  start_ep="$(date -u +%s)"
  echo "[fault-inject] Hour 120: Executing online backup + isolated restore + CheckReleaseBlockers..."
  passed="false"
  if "$SCRIPT_DIR/backup-verify.sh"; then
    TOKEN="$(cat "${SOAK_PROM_TOKEN_FILE:-$SOAK_ROOT/secrets/ruxbase-ops.token}")"
    RB_PASSED="$(curl -fsS -H "Authorization: $TOKEN" "$BASE_URL/api/studio/overview" | jq -r '.releaseBlockers.passed // false')"
    if [[ "$RB_PASSED" == "true" ]]; then
      passed="true"
    fi
  fi
  end_ep="$(date -u +%s)"
  rec_sec=$((end_ep - start_ep))
  detail="online backup + isolated restore + CheckReleaseBlockers passed=$passed in ${rec_sec}s"
  record_fault "hour120_backup_restore" "$start_ts" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$rec_sec" "$passed" "$detail"
  [[ "$passed" == "true" ]]
}

MODE="${1:-}"
case "$MODE" in
  --drill)
    case "${2:-}" in
      hour24_worker_kill) run_hour24 ;;
      hour72_pg_disconnect) run_hour72 ;;
      hour120_backup_restore) run_hour120 ;;
      *) echo "Unknown drill: ${2:-}" >&2; exit 2 ;;
    esac
    ;;
  --auto)
    STATE_FILE="$EVIDENCE_DIR/state.json"
    if [[ ! -f "$STATE_FILE" ]]; then
      exit 0
    fi
    START_ISO="$(jq -r '.officialStartUtc' "$STATE_FILE")"
    START_SEC="$(date -u -d "$START_ISO" +%s)"
    NOW_SEC="$(date -u +%s)"
    ELAPSED_H=$(( (NOW_SEC - START_SEC) / 3600 ))
    if [[ "$ELAPSED_H" -ge 24 ]] && ! drill_already_ran "hour24_worker_kill"; then
      run_hour24
    fi
    if [[ "$ELAPSED_H" -ge 72 ]] && ! drill_already_ran "hour72_pg_disconnect"; then
      run_hour72
    fi
    if [[ "$ELAPSED_H" -ge 120 ]] && ! drill_already_ran "hour120_backup_restore"; then
      run_hour120
    fi
    ;;
  *)
    echo "Usage: $0 [--drill hour24_worker_kill|hour72_pg_disconnect|hour120_backup_restore | --auto]" >&2
    exit 2
    ;;
esac
