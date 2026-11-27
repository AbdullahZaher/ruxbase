#!/usr/bin/env bash
# Executes Journey H / Hour 120 online backup under active writes, restores into
# the isolated postgres-restore-target container, verifies schema/RLS/records/checksums,
# and always appends structured JSON evidence to <evidence>/<campaignId>/backups.jsonl.
set -euo pipefail

CALLER_CAMPAIGN_ID="${SOAK_CAMPAIGN_ID:-}"
SOAK_ROOT="${SOAK_ROOT:-/srv/ruxbase-soak}"
ENV_FILE="${ENV_FILE:-$SOAK_ROOT/.env.staging}"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# Caller-provided SOAK_CAMPAIGN_ID takes precedence over .env.staging so pilot vs official runs never collide.
CAMPAIGN_ID="${CALLER_CAMPAIGN_ID:-${SOAK_CAMPAIGN_ID:-}}"
if [[ -z "$CAMPAIGN_ID" || "$CAMPAIGN_ID" == "pilot-pending" || "$CAMPAIGN_ID" == "UNBOUND" || "$CAMPAIGN_ID" == "staging" ]]; then
  echo "ERROR: Valid SOAK_CAMPAIGN_ID is required (got '${CAMPAIGN_ID}')." >&2
  exit 2
fi

EVIDENCE_DIR="${SOAK_EVIDENCE_HOST_DIR:-$SOAK_ROOT/evidence}/$CAMPAIGN_ID"
BACKUP_DIR="$EVIDENCE_DIR/backups"
mkdir -p "$BACKUP_DIR"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
START_EPOCH="$(date -u +%s)"
DUMP_FILE="$BACKUP_DIR/ruxbase_staging_${TS}.dump"
RESTORE_DB="restore_verify_${TS,,}"

DUMP_SHA256=""
DUMP_BYTES=0
RESTORE_RC=1
MIG_COUNT=0
PRIMARY_MIG_COUNT=0
ITEM_COUNT=0
NULL_TENANTS=0
MISSING_CHECKSUMS=0
STORAGE_COUNT=0
RLS_ENABLED="f"
PASSED="false"
ERR_DETAIL=""

cleanup_restore_db() {
  docker exec ruxbase-soak-restore-pg psql -U postgres -c "DROP DATABASE IF EXISTS \"$RESTORE_DB\";" >/dev/null 2>&1 || true
}
trap cleanup_restore_db EXIT

echo "[backup-verify] Campaign=$CAMPAIGN_ID Starting online PostgreSQL backup under active writes: $DUMP_FILE"
PRIMARY_MIG_COUNT="$(docker exec ruxbase-soak-postgres psql -U postgres -d ruxbase_staging -Atc "SELECT count(*) FROM _migrations;" || echo 0)"

if docker exec -e PGPASSWORD="$RUXBASE_APP_PASSWORD" ruxbase-soak-postgres \
  pg_dump -h 127.0.0.1 -U ruxbase_app -d ruxbase_staging -Fc --no-owner --no-acl \
  > "$DUMP_FILE"; then
  DUMP_SHA256="$(sha256sum "$DUMP_FILE" | awk '{print $1}')"
  DUMP_BYTES="$(stat -c%s "$DUMP_FILE")"

  echo "[backup-verify] Restoring into isolated target database $RESTORE_DB on ruxbase-soak-restore-pg..."
  # Separate -c flags avoid PostgreSQL ERROR: DROP/CREATE DATABASE cannot run inside a transaction block.
  if docker exec ruxbase-soak-restore-pg psql -U postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$RESTORE_DB\";" \
    && docker exec ruxbase-soak-restore-pg psql -U postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$RESTORE_DB\";" \
    && docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -v ON_ERROR_STOP=1 -c "CREATE EXTENSION IF NOT EXISTS vector;" -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"; then

    set +e
    docker exec -i ruxbase-soak-restore-pg pg_restore -U postgres -d "$RESTORE_DB" --no-owner --no-acl --exit-on-error < "$DUMP_FILE"
    RESTORE_RC=$?
    set -e

    if [[ "$RESTORE_RC" -eq 0 ]]; then
      MIG_COUNT="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM _migrations;" || echo 0)"
      ITEM_COUNT="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM soak_items;" || echo 0)"
      NULL_TENANTS="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM soak_items WHERE tenant_id IS NULL OR tenant_id = '';" || echo 999)"
      MISSING_CHECKSUMS="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM soak_items WHERE checksum IS NULL OR checksum = '';" || echo 999)"
      STORAGE_COUNT="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT count(*) FROM _storage_objects;" || echo 0)"
      RLS_ENABLED="$(docker exec ruxbase-soak-restore-pg psql -U postgres -d "$RESTORE_DB" -Atc "SELECT relrowsecurity FROM pg_class WHERE relname = 'soak_items';" || echo f)"

      if [[ "$MIG_COUNT" -gt 0 && "$MIG_COUNT" -ge "$PRIMARY_MIG_COUNT" && "$ITEM_COUNT" -gt 0 && "$NULL_TENANTS" -eq 0 && "$MISSING_CHECKSUMS" -eq 0 && "$RLS_ENABLED" == "t" ]]; then
        PASSED="true"
      else
        ERR_DETAIL="post-restore validation failed (mig=$MIG_COUNT/$PRIMARY_MIG_COUNT items=$ITEM_COUNT nullTenants=$NULL_TENANTS missingChecksums=$MISSING_CHECKSUMS rls=$RLS_ENABLED)"
      fi
    else
      ERR_DETAIL="pg_restore exited with code $RESTORE_RC"
    fi
  else
    ERR_DETAIL="failed preparing isolated restore target database $RESTORE_DB"
  fi
else
  ERR_DETAIL="pg_dump failed"
fi

END_EPOCH="$(date -u +%s)"
RTO_SEC=$((END_EPOCH - START_EPOCH))

# Retain last 7 dumps to bound disk usage
ls -1t "$BACKUP_DIR"/ruxbase_staging_*.dump 2>/dev/null | tail -n +8 | xargs -r rm -f

RECORD="$(jq -nc \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg cid "$CAMPAIGN_ID" \
  --arg dump "$(basename "$DUMP_FILE")" \
  --arg sha "$DUMP_SHA256" \
  --arg err "$ERR_DETAIL" \
  --argjson bytes "$DUMP_BYTES" \
  --argjson restoreRc "$RESTORE_RC" \
  --argjson migrations "$MIG_COUNT" \
  --argjson primaryMigrations "$PRIMARY_MIG_COUNT" \
  --argjson items "$ITEM_COUNT" \
  --argjson nullTenants "$NULL_TENANTS" \
  --argjson missingChecksums "$MISSING_CHECKSUMS" \
  --argjson storageObjects "$STORAGE_COUNT" \
  --argjson rlsEnabled "$( [[ "$RLS_ENABLED" == "t" ]] && echo true || echo false )" \
  --argjson rtoSec "$RTO_SEC" \
  --argjson passed "$PASSED" \
  '{timestampUtc:$ts, campaignId:$cid, dumpFile:$dump, sha256:$sha, sizeBytes:$bytes, restoreExitCode:$restoreRc, migrationsCount:$migrations, primaryMigrationsCount:$primaryMigrations, restoredItemsCount:$items, nullTenantRows:$nullTenants, missingChecksums:$missingChecksums, restoredStorageObjects:$storageObjects, rlsEnabled:$rlsEnabled, rtoSeconds:$rtoSec, passed:$passed, error:$err}')"

echo "$RECORD" >> "$EVIDENCE_DIR/backups.jsonl"
echo "[backup-verify] Result: $RECORD"

if [[ "$PASSED" != "true" ]]; then
  exit 1
fi
