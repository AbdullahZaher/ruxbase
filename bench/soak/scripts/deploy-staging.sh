#!/usr/bin/env bash
# Deploy the persistent, isolated Ruxbase W11-05 staging stack on an authorized dedicated staging VPS.
# Refuses to run on a shared host hosting unrelated/production Docker containers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SOAK_ROOT="${SOAK_ROOT:-/srv/ruxbase-soak}"
ENV_FILE="$SOAK_ROOT/.env.staging"
SECRETS_DIR="$SOAK_ROOT/secrets"
EVIDENCE_DIR="$SOAK_ROOT/evidence"
COMPOSE_FILE="$REPO_ROOT/bench/soak/deploy/docker-compose.staging.yml"
ALLOW_NON_EMPTY_HOST="${ALLOW_NON_EMPTY_HOST:-0}"

generate_secret() {
  local bytes="${1:-24}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    LC_ALL=C tr -dc 'a-f0-9' < /dev/urandom | head -c $((bytes * 2))
  fi
}

echo "[deploy-staging] Phase 0: Enforcing mandatory staging host isolation..."
if [[ "$ALLOW_NON_EMPTY_HOST" != "1" ]]; then
  UNRELATED="$(docker ps --format '{{.Names}}' | grep -vE '^ruxbase-soak-' || true)"
  if [[ -n "$UNRELATED" ]]; then
    echo "ERROR: ISOLATION BLOCKER — unrelated/production containers detected on this host:" >&2
    echo "$UNRELATED" >&2
    echo "W11-05 staging soak MUST run on a dedicated staging VPS or isolated VM." >&2
    exit 1
  fi
fi

AVAIL_PCT="$(df -P / | awk 'NR==2 {gsub("%","",$5); print 100 - $5}')"
if [[ "$AVAIL_PCT" -lt 25 ]]; then
  echo "ERROR: Insufficient free disk space (${AVAIL_PCT}% free; require >= 25% before 168h staging soak)." >&2
  exit 1
fi

echo "[deploy-staging] Validating scripts/install-selfhosted.sh in dry-run mode..."
"$REPO_ROOT/scripts/install-selfhosted.sh" --dir "$SOAK_ROOT/selfhosted-check" --dry-run

mkdir -p "$SOAK_ROOT" "$SECRETS_DIR" "$EVIDENCE_DIR"
chmod 750 "$SOAK_ROOT" "$EVIDENCE_DIR"
chmod 750 "$SECRETS_DIR"
chown -R 10001:10001 "$EVIDENCE_DIR" "$SECRETS_DIR" 2>/dev/null || true

if [[ ! -f "$ENV_FILE" ]]; then
  (
    umask 077
    cat > "$ENV_FILE" <<EOF
# Ruxbase W11-05 Staging Environment (generated $(date -u +%Y-%m-%dT%H:%M:%SZ), mode 600)
RUXBASE_PG_IMAGE=pgvector/pgvector:0.8.7-pg18-trixie
RUXBASE_CANDIDATE_VERSION=$(git -C "$REPO_ROOT" rev-parse --short HEAD)
RUXBASE_STAGING_IMAGE=ruxbase:w11-05-$(git -C "$REPO_ROOT" rev-parse --short HEAD)
SOAK_ENGINE_IMAGE=ruxbase-soak:w11-05-$(git -C "$REPO_ROOT" rev-parse --short HEAD)

POSTGRES_SUPERUSER_PASSWORD=$(generate_secret 24)
POSTGRES_RESTORE_PASSWORD=$(generate_secret 24)
RUXBASE_APP_PASSWORD=$(generate_secret 24)
RUXBASE_MIGRATOR_PASSWORD=$(generate_secret 24)
RUXBASE_WAL_PASSWORD=$(generate_secret 24)
RUXBASE_MONITOR_PASSWORD=$(generate_secret 24)
RUXBASE_ENCRYPTION_KEY=$(generate_secret 32)

SOAK_SUPERUSER_EMAIL=soak-admin@staging.invalid
SOAK_SUPERUSER_PASSWORD=$(generate_secret 20)
GRAFANA_ADMIN_USER=soak_admin
GRAFANA_ADMIN_PASSWORD=$(generate_secret 18)

RUXBASE_DB_MAX_CONNS=30
RUXBASE_REALTIME_SOURCE=replication
RUXBASE_RLS_STRICT=false
SOAK_CAPACITY_RPS=40
SOAK_DURATION=168h
SOAK_CAMPAIGN_ID=UNBOUND
SOAK_EVIDENCE_HOST_DIR=$EVIDENCE_DIR
SOAK_SECRETS_HOST_DIR=$SECRETS_DIR
SOAK_PROM_TOKEN_FILE=$SECRETS_DIR/ruxbase-ops.token
EOF
  )
  chmod 600 "$ENV_FILE"
  echo "[deploy-staging] Generated private staging environment at $ENV_FILE (mode 600)."
else
  echo "[deploy-staging] Preserving existing environment file at $ENV_FILE."
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

echo "[deploy-staging] Step 1: Starting PostgreSQL primary and isolated restore target..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --wait postgres postgres-restore-target

echo "[deploy-staging] Step 2: Provisioning separated least-privilege PostgreSQL roles..."
docker exec ruxbase-soak-postgres psql -U postgres -v ON_ERROR_STOP=1 \
  -v app_password="$RUXBASE_APP_PASSWORD" \
  -v migrator_password="$RUXBASE_MIGRATOR_PASSWORD" \
  -v wal_password="$RUXBASE_WAL_PASSWORD" \
  -v monitor_password="$RUXBASE_MONITOR_PASSWORD" \
  -f /opt/ruxbase/init-staging-roles.sql

echo "[deploy-staging] Step 3: Building frozen Ruxbase and soak-engine container images..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" build ruxbase soak-worker

echo "[deploy-staging] Step 4: Starting Ruxbase staging application server..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --wait ruxbase

echo "[deploy-staging] Step 5: Bootstrapping staging superuser and Prometheus scrape token..."
docker exec ruxbase-soak-app ruxbase superuser upsert "$SOAK_SUPERUSER_EMAIL" "$SOAK_SUPERUSER_PASSWORD" --dir=/app/ruxbase_data
SUPER_TOKEN="$(curl -fsS -X POST "http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}/api/collections/_superusers/auth-with-password" \
  -H "Content-Type: application/json" \
  -d "{\"identity\":\"${SOAK_SUPERUSER_EMAIL}\",\"password\":\"${SOAK_SUPERUSER_PASSWORD}\"}" | jq -r '.token')"

(
  umask 077
  printf '%s' "$SUPER_TOKEN" > "$SECRETS_DIR/ruxbase-ops.token"
)
chown 10001:10001 "$SECRETS_DIR/ruxbase-ops.token" 2>/dev/null || true
chmod 644 "$SECRETS_DIR/ruxbase-ops.token"

echo "[deploy-staging] Step 6: Starting monitoring stack (worker remains stopped until campaign ID is bound)..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --wait postgres-exporter node-exporter prometheus grafana

echo "[deploy-staging] Staging stack deployed cleanly (loopback-only)."
echo "  Ruxbase API: http://127.0.0.1:${RUXBASE_HOST_PORT:-18090}/api/health?ready"
echo "  Prometheus:  http://127.0.0.1:${PROMETHEUS_HOST_PORT:-19090}"
echo "  Grafana:     http://127.0.0.1:${GRAFANA_HOST_PORT:-13000}"
