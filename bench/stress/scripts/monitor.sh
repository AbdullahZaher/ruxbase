#!/usr/bin/env bash
# Continuous Docker cgroup v2 CPU/memory/net/block-IO & FD sampler for stress tests.
# Usage: monitor.sh <project_name> <output_jsonl> <interval_sec> <phase_label>
set -euo pipefail

PROJECT="${1:?project required}"
OUTFILE="${2:?outfile required}"
INTERVAL="${3:-2}"
PHASE="${4:-run}"

while true; do
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  ids=$(docker ps --filter "label=com.docker.compose.project=${PROJECT}" -q)
  if [ -n "$ids" ]; then
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      cname=$(printf '%s' "$line" | jq -r '.Name' 2>/dev/null || echo "")
      fds="-1"
      if [ -n "$cname" ]; then
        fds=$(docker exec "$cname" sh -c 'ls /proc/1/fd 2>/dev/null | wc -l' 2>/dev/null | tr -d ' ' || echo "-1")
      fi
      printf '{"ts":"%s","phase":"%s","fds":%s,"stats":%s}\n' "$ts" "$PHASE" "${fds:--1}" "$line" >> "$OUTFILE"
    done < <(docker stats --no-stream --format '{{json .}}' $ids 2>/dev/null || true)
  fi
  sleep "$INTERVAL"
done
