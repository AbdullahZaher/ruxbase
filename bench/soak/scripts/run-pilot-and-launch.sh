#!/usr/bin/env bash
# End-to-end Phase 6 -> Phase 7 -> Phase 8 operator launcher for an authorized dedicated staging VPS:
#   1. Runs preflight (host isolation, disk headroom, PG roles, Ruxbase readiness).
#   2. Runs a genuine 60-minute continuous pilot + online backup/restore rehearsal.
#   3. Reconciles pilot exit gate (aborts if any defect or missing evidence).
#   4. Freezes candidate provenance (Git SHA, tree, Linux binary SHA-256, PG/extensions/configs).
#   5. Installs and starts the supervised systemd 168-hour soak service + backup/fault timers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SOAK_ROOT="${SOAK_ROOT:-/srv/ruxbase-soak}"
ENV_FILE="$SOAK_ROOT/.env.staging"

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

echo "[launch] Building soak CLI binary..."
CGO_ENABLED=0 go -C "$REPO_ROOT" build -trimpath -o "$SOAK_ROOT/soak" ./bench/soak/cmd/soak
CGO_ENABLED=0 go -C "$REPO_ROOT" build -trimpath -o "$SOAK_ROOT/ruxbase-candidate-linux" ./examples/base

echo "[launch] Phase 0/1 Preflight verification..."
"$SOAK_ROOT/soak" preflight \
  --base-url="http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --check-isolation=true

echo "[launch] Phase 6: Starting genuine 60-minute continuous pilot ($PILOT_ID)..."
"$SOAK_ROOT/soak" pilot \
  --campaign-id="$PILOT_ID" \
  --base-url="http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --superuser-email="$SOAK_SUPERUSER_EMAIL" \
  --superuser-password="$SOAK_SUPERUSER_PASSWORD" \
  --capacity-rps="${SOAK_CAPACITY_RPS:-40}" \
  --duration=60m \
  --metrics-addr=127.0.0.1:19108

echo "[launch] Phase 6: Executing pilot online backup + isolated restore rehearsal..."
SOAK_CAMPAIGN_ID="$PILOT_ID" "$SCRIPT_DIR/backup-verify.sh"

echo "[launch] Phase 6: Evaluating Pilot Exit Gate..."
"$SOAK_ROOT/soak" reconcile --evidence-dir="$EVIDENCE_ROOT/$PILOT_ID" --required-hours=1.0

echo "[launch] Phase 7: Freezing candidate provenance for $OFFICIAL_ID..."
"$SOAK_ROOT/soak" freeze \
  --campaign-id="$OFFICIAL_ID" \
  --evidence-dir="$EVIDENCE_ROOT" \
  --repo-dir="$REPO_ROOT" \
  --binary="$SOAK_ROOT/ruxbase-candidate-linux" \
  --capacity-rps="${SOAK_CAPACITY_RPS:-40}"

# Pin official campaign ID in .env.staging
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
