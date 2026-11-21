#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
python3 -m venv "$tmp/venv"
"$tmp/venv/bin/pip" install --disable-pip-version-check --no-deps -r openapi-validator-requirements.txt
"$tmp/venv/bin/pip" check
"$tmp/venv/bin/openapi-spec-validator" dist/openapi.json
