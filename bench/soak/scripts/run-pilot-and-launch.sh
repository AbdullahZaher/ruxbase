#!/usr/bin/env bash
# End-to-end Phase 6 -> Phase 7 -> Phase 8 operator launcher for an authorized dedicated staging VPS:
#   1. Enforces mandatory staging isolation & readiness via `soak preflight`.
#   2. Freezes Pilot provenance BEFORE running the pilot.
#   3. Binds soak-worker to the Pilot Campaign ID and runs a genuine 60-minute continuous pilot.
#   4. Executes online backup & isolated restore verification while writes are active (fail-closed).
#   5. Evaluates Pilot Exit Gate (`reconcile` + read-only `audit`); stops pilot worker cleanly.
#   6. Freezes Official candidate provenance (verifying pilot PASS & deployed binary SHA-256).
#   7. Launches supervised systemd 168-hour soak service + backup & fault-injection timers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SOAK_ROOT="${SOAK_ROOT:-/srv/ruxbase-soak}"
ENV_FILE="$SOAK_ROOT/.env.staging"
COMPOSE_FILE="$REPO_ROOT/bench/soak/deploy/docker-compose.staging.yml"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found. Run bench/soak/scripts/deploy-staging.sh first." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

EVIDENCE_ROOT="${SOAK_EVIDENCE_HOST_DIR:-$SOAK_ROOT/evidence}"
PILOT_ID="W11-05-PILOT-$(date -u +%Y%m%dT%H%M%SZ)"
OFFICIAL_ID="W11-05-168H-$(date -u +%Y%m%dT%H%M%SZ)"

echo "[launch] Building soak CLI and reproducible Linux candidate binary..."
CGO_ENABLED=0 go -C "$REPO_ROOT" build -trimpath -o "$SOAK_ROOT/soak" ./bench/soak/cmd/soak
CGO_ENABLED=0 go -C "$REPO_ROOT" build -trimpath -o "$SOAK_ROOT/ruxbase-candidate-linux" ./examples/base

echo "[launch] Phase 0/1 Preflight verification (strict isolation enforced)..."
"$SOAK_ROOT/soak" preflight \
  --base-url="http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --check-isolation=true

cleanup_pilot_worker() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" stop -t 20 soak-worker >/dev/null 2>&1 || true
}
trap cleanup_pilot_worker EXIT

echo "[launch] Phase 6: Freezing Pilot candidate manifest BEFORE pilot execution ($PILOT_ID)..."
"$SOAK_ROOT/soak" freeze \
  --campaign-id="$PILOT_ID" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --repo-dir="$REPO_ROOT" \
  --binary="$SOAK_ROOT/ruxbase-candidate-linux" \
  --deployed-container=ruxbase-soak-app \
  --postgres-container=ruxbase-soak-postgres \
  --capacity-rps="${SOAK_CAPACITY_RPS:-40}"
chown -R 10001:10001 "$EVIDENCE_ROOT/$PILOT_ID" 2>/dev/null || true

echo "[launch] Phase 6: Starting campaign-bound soak-worker for Pilot ($PILOT_ID)..."
SOAK_CAMPAIGN_ID="$PILOT_ID" docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --wait soak-worker

echo "[launch] Phase 6: Executing genuine 60-minute continuous pilot ($PILOT_ID)..."
SOAK_CAMPAIGN_ID="$PILOT_ID" docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" --profile soak run \
  --name "ruxbase-soak-pilot-${PILOT_ID}" --rm -p "127.0.0.1:${SOAK_METRICS_HOST_PORT:-19108}:9108" soak-engine \
  pilot \
  --campaign-id="$PILOT_ID" \
  --base-url=http://ruxbase:8090 \
  --db="postgres://ruxbase_app:${RUXBASE_APP_PASSWORD}@postgres:5432/ruxbase_staging?sslmode=disable" \
  --monitor-db="postgres://ruxbase_monitor:${RUXBASE_MONITOR_PASSWORD}@postgres:5432/ruxbase_staging?sslmode=disable" \
  --data-dir=/app/ruxbase_data \
  --evidence-dir=/srv/ruxbase-soak/evidence \
  --superuser-email="$SOAK_SUPERUSER_EMAIL" \
  --superuser-password="$SOAK_SUPERUSER_PASSWORD" \
  --prom-token-file=/srv/ruxbase-soak/secrets/ruxbase-ops.token \
  --capacity-rps="${SOAK_CAPACITY_RPS:-40}" \
  --duration=60m \
  --metrics-addr=:9108 &
PILOT_PID=$!

# Run online backup & isolated restore rehearsal after 5 minutes while pilot writes are active
sleep 300
if kill -0 "$PILOT_PID" 2>/dev/null; then
  echo "[launch] Phase 6: Executing pilot online backup + isolated restore rehearsal..."
  SOAK_CAMPAIGN_ID="$PILOT_ID" "$SCRIPT_DIR/backup-verify.sh"
fi

wait "$PILOT_PID"
cleanup_pilot_worker

echo "[launch] Phase 6: Evaluating Pilot Exit Gate (reconcile + read-only A11 audit)..."
"$SOAK_ROOT/soak" reconcile --evidence-dir="$EVIDENCE_ROOT/$PILOT_ID" --required-hours=0.98
"$SOAK_ROOT/soak" audit --evidence-dir="$EVIDENCE_ROOT/$PILOT_ID" --required-hours=0.98

echo "[launch] Phase 7: Freezing candidate provenance for $OFFICIAL_ID (verifying pilot PASS & deployed binary)..."
"$SOAK_ROOT/soak" freeze \
  --campaign-id="$OFFICIAL_ID" \
  --pilot-dir="$EVIDENCE_ROOT/$PILOT_ID" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --repo-dir="$REPO_ROOT" \
  --binary="$SOAK_ROOT/ruxbase-candidate-linux" \
  --deployed-container=ruxbase-soak-app \
  --postgres-container=ruxbase-soak-postgres \
  --capacity-rps="${SOAK_CAPACITY_RPS:-40}"
chown -R 10001:10001 "$EVIDENCE_ROOT/$OFFICIAL_ID" 2>/dev/null || true

grep -v '^SOAK_CAMPAIGN_ID=' "$ENV_FILE" > "${ENV_FILE}.tmp" || true
echo "SOAK_CAMPAIGN_ID=$OFFICIAL_ID" >> "${ENV_FILE}.tmp"
mv "${ENV_FILE}.tmp" "$ENV_FILE"
chmod 600 "$ENV_FILE"

echo "[launch] Phase 7/8: Installing supervised systemd units for unattended 168-hour soak..."
cp "$REPO_ROOT/bench/soak/deploy/systemd/"*.service /etc/systemd/system/
cp "$REPO_ROOT/bench/soak/deploy/systemd/"*.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now ruxbase-soak.service ruxbase-soak-backup.timer ruxbase-soak-faults.timer

echo "[launch] Official 168-hour W11-05 campaign launched: $OFFICIAL_ID"
echo "  Monitor service: systemctl status ruxbase-soak.service"
echo "  Evidence path:   $EVIDENCE_ROOT/$OFFICIAL_ID"
