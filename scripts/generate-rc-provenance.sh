#!/usr/bin/env bash
# Generates reproducible release-candidate provenance metadata and verifies supply-chain integrity.
set -euo pipefail

COMMIT_SHA="$(git rev-parse HEAD)"
TREE_SHA="$(git rev-parse 'HEAD^{tree}')"
GO_VERSION="$(go version)"
NODE_VERSION="$(node --version 2>/dev/null || echo 'unavailable')"

echo "Verifying Go module checksums..." >&2
go mod verify >&2

TMP_BIN="$(mktemp -t ruxbase-rc-bin.XXXXXX)"
trap 'rm -f "$TMP_BIN"' EXIT

echo "Building reproducible candidate binary (-trimpath)..." >&2
CGO_ENABLED=0 go build -trimpath -o "$TMP_BIN" ./examples/base

if command -v sha256sum >/dev/null 2>&1; then
  BIN_SHA="$(sha256sum "$TMP_BIN" | awk '{print $1}')"
else
  BIN_SHA="$(shasum -a 256 "$TMP_BIN" | awk '{print $1}')"
fi

cat <<EOF
{
  "commit": "$COMMIT_SHA",
  "tree": "$TREE_SHA",
  "goVersion": "$GO_VERSION",
  "nodeVersion": "$NODE_VERSION",
  "goModVerified": true,
  "reproducibleBinarySha256": "$BIN_SHA",
  "generatedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
