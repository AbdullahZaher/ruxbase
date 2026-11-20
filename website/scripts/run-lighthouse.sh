#!/usr/bin/env bash
# Test-only tool, isolated from the site lockfile and deployed artifact.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p performance-evidence
printf '{"name":"ruxbase-docs-lighthouse-test","private":true}\n' > "$tmp/package.json"
npm install --prefix "$tmp" --ignore-scripts --no-audit --no-fund --save-exact lighthouse@13.6.0
cp "$tmp/package-lock.json" performance-evidence/lighthouse-package-lock.json
npm audit --prefix "$tmp" --audit-level=high --json > performance-evidence/lighthouse-audit.json
node scripts/lighthouse-check.mjs "$tmp/node_modules/.bin/lighthouse"
