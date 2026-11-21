#!/usr/bin/env bash
# Reproducible one-command core self-hosted installer for Ruxbase.
# Generates private-by-default credentials without exposing secrets to stdout.
set -euo pipefail

TARGET_DIR="."
DRY_RUN=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)
      TARGET_DIR="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--dir <directory>] [--dry-run]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

generate_secret() {
  local bytes="${1:-24}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    LC_ALL=C tr -dc 'a-f0-9' < /dev/urandom | head -c $((bytes * 2))
  fi
}

mkdir -p "$TARGET_DIR"
ENV_FILE="$TARGET_DIR/.env"
DATA_DIR="$TARGET_DIR/ruxbase_data"

if [[ ! -f "$ENV_FILE" ]]; then
  PG_PASS="$(generate_secret 24)"
  APP_PASS="$(generate_secret 24)"
  ENC_KEY="$(generate_secret 32)"

  (
    umask 077
    cat > "$ENV_FILE" <<EOF
# Ruxbase Self-Hosted Environment (private-by-default, generated $(date -u +%Y-%m-%dT%H:%M:%SZ))
POSTGRES_USER=admin
POSTGRES_PASSWORD=$PG_PASS
APP_PASSWORD=$APP_PASS
DATABASE_URL=postgres://ruxbase_app:$APP_PASS@127.0.0.1:5432/ruxbase?sslmode=disable
RUXBASE_ENCRYPTION_KEY=$ENC_KEY
RUXBASE_HTTP_ADDR=127.0.0.1:8090
RUXBASE_DB_MAX_CONNS=20
EOF
  )
  chmod 600 "$ENV_FILE"
  echo "Initialized private environment file at $ENV_FILE (mode 600, credentials redacted)."
else
  echo "Existing environment file preserved at $ENV_FILE."
fi

mkdir -p "$DATA_DIR"
chmod 750 "$DATA_DIR"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "Dry-run complete: workspace initialized at $TARGET_DIR (no containers started)."
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Error: docker is required for full installation." >&2
  exit 1
fi

echo "Starting PostgreSQL 18 + pgvector service via Docker Compose..."
docker compose -f "$TARGET_DIR/docker-compose.dev.yml" up -d
echo "Ruxbase self-hosted infrastructure started safely on loopback interface."
