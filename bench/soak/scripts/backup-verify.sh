#!/usr/bin/env bash
# Executes Journey H / Hour 120 online backup under active writes, restores into
# the isolated postgres-restore-target container, verifies schema/RLS/records/checksums,
# and appends structured JSON evidence to backups.jsonl.
set -euo pipefail

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
BACKUP_DIR="$EVIDENCE_DIR/backups"
mkdir -p "$BACKUP_DIR"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
START_EPOCH="$(date -u +%s)"
DUMP_FILE="$BACKUP_DIR/ruxbase_staging_${TS}.dump"
RESTORE_DB="restore_verify_${TS,,}"

echo "[backup-verify] Starting online PostgreSQL backup under active writes: $DUMP_FILE"
docker exec -e PGPASSWORD="$RUXBASE_APP_PASSWORD" ruxbase-soak-postgres \
  pg_dump -h 127.0.0.1 -U ruxbase_app -d ruxbase_staging -Fc --no-owner --no-acl \
  > "$DUMP_FILE"

DUMP_SHA256="$(sha256sum "$DUMP_FILE" | awk '{print $1}')"
DUMP_BYTES="$(stat -c%s "$DUMP_FILE")"

echo "[backup-verify] Restoring into isolated target database $RESTORE_DB on ruxbase-soak-restore-pg..."
docker exec ruxbase-soak-restore-pg psql -U postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$RESTORE_DB\"; CREATE DATABASE \"$RESTORE_DB\";"
docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -v ON_ERROR_STOP=1 -c "CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pgcrypto;"

set +e
docker exec -i ruxbase-soak-restore-pg pg_restore -U postgres -d "$RESTORE_DB" --no-owner --no-acl --exit-on-error < "$DUMP_FILE"
RESTORE_RC=$?
set -e

MIG_COUNT=0
ITEM_COUNT=0
NULL_TENANTS=0
RLS_ENABLED="f"
PASSED="false"

if [[ "$RESTORE_RC" -eq 0 ]]; then
  MIG_COUNT="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM _migrations;" || echo 0)"
  ITEM_COUNT="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM soak_items;" || echo 0)"
  NULL_TENANTS="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM soak_items WHERE tenant_id IS NULL OR tenant_id = '';" || echo 999)"
  RLS_ENABLED="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT relrowsecurity FROM pg_class WHERE relname = 'soak_items';" || echo f)"
  if [[ "$MIG_COUNT" -gt 0 && "$ITEM_COUNT" -gt 0 && "$NULL_TENANTS" -eq 0 && "$RLS_ENABLED" == "t" ]]; then
    PASSED="true"
  fi
fi

# Clean up disposable verification database on isolated restore container (never touches staging DB)
docker exec ruxbase-soak-restore-pg psql -U postgres -c "DROP DATABASE IF EXISTS \"$RESTORE_DB\";" >/dev/null 2>&1 || true

END_EPOCH="$(date -u +%s)"
RTO_SEC=$((END_EPOCH - START_EPOCH))

# Retain last 7 dumps to bound disk usage
ls -1t "$BACKUP_DIR"/ruxbase_staging_*.dump 2>/dev/null | tail -n +8 | xargs -r rm -f

RECORD="$(jq -nc \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg dump "$(basename "$DUMP_FILE")" \
  --arg sha "$DUMP_SHA256" \
  --argjson bytes "$DUMP_BYTES" \
  --argjson restoreRc "$RESTORE_RC" \
  --argjson migrations "$MIG_COUNT" \
  --argjson items "$ITEM_COUNT" \
  --argjson nullTenants "$NULL_TENANTS" \
  --argjson rlsEnabled "$( [[ "$RLS_ENABLED" == "t" ]] && echo true || echo false )" \
  --argjson rtoSec "$RTO_SEC" \
  --argjson passed "$PASSED" \
  '{timestampUtc:$ts, dumpFile:$dump, sha256:$sha, sizeBytes:$bytes, restoreExitCode:$restoreRc, migrationsCount:$migrations, restoredItemsCount:$items, nullTenantRows:$nullTenants, rlsEnabled:$rlsEnabled, rtoSeconds:$rtoSec, passed:$passed}')"

echo "$RECORD" >> "$EVIDENCE_DIR/backups.jsonl"
echo "[backup-verify] Result: $RECORD"

if [[ "$PASSED" != "true" ]]; then
  exit 1
fi
