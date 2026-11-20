#!/usr/bin/env bash
#
# Runs the whole test suite against RUXBASE_TEST_URL and checks the parity with
# the PocketBase v0.40.6 tests (tests/cmd/parity).
#
# The go test -json output is saved in test-output.jsonl.
#
#   RUXBASE_TEST_URL=postgres://... ./scripts/ci-test.sh

set -uo pipefail

cd "$(dirname "$0")/.."

OUT="test-output.jsonl"

go test -count=1 -p 4 -timeout 20m -json ./... > "$OUT"
test_status=$?

# Publish the bounded same-role checkout measurement, not connection data.
jq -r 'select(.Action == "output" and .Test == "TestRestrictedIdentityAdmissionPostgreSQL") | .Output' "$OUT" |
  grep 'M01_CHECKOUT samples=' || true

# A declared dedicated WAL fixture must actually execute the combined
# migration/HTTP/WAL journey. A skipped test cannot satisfy this gate.
if [ -n "${RUXBASE_TEST_WAL_URL:-}" ]; then
  if ! jq -r 'select(.Action == "output" and .Test == "TestPrepareDisposablePostgreSQL") | .Output' "$OUT" |
    grep 'M01_COMBINED_WAL: restricted HTTP create decoded by dedicated WAL session and persisted'; then
    echo "M01 combined WAL evidence missing"
    test_status=1
  fi
fi

# print the failed tests and their messages
if [ "$test_status" -ne 0 ]; then
  echo "failed tests:"
  jq -r 'select(.Action == "fail" and .Test != null) | "  \(.Package) \(.Test)"' "$OUT"
  echo
  jq -r 'select(.Action == "output" and .Test != null) | .Output' "$OUT" |
    grep -E -- '--- FAIL|_test\.go:[0-9]+:|golden\.go:[0-9]+:|api\.go:[0-9]+:|panic:' || true
  # the panics with their stack traces
  jq -r 'select(.Action == "output") | .Output' "$OUT" | awk '/^panic:/ { n = 80 } n > 0 { print; n-- }' || true
  # the build failures and package level errors
  jq -r 'select(.Action == "output" and .Test == null) | .Output' "$OUT" |
    grep -vE '^(ok|PASS|FAIL|coverage:)|^\s*$|^\[' || true
fi

go run ./tests/cmd/parity -in "$OUT"
parity_status=$?

if [ "$test_status" -ne 0 ] || [ "$parity_status" -ne 0 ]; then
  exit 1
fi

# Repeat the existing paired checkout measurement after the parallel suite.
# Use one test process and serial trials on the same disposable CI server;
# preserve the same runtime role and safety checks within each trial.
if [ -n "${RUXBASE_TEST_URL:-}" ]; then
  go test -count=3 -p 1 -parallel 1 -timeout 5m -v \
    -run '^TestRestrictedIdentityAdmissionPostgreSQL$' ./internal/pgrolecheck | tee checkout-measurements.txt
  measurement_status=$?
  if [ "$measurement_status" -ne 0 ]; then
    exit "$measurement_status"
  fi
  if [ "$(grep -c 'M01_CHECKOUT samples=200 ' checkout-measurements.txt)" -ne 3 ] ||
    [ "$(grep -c '^--- PASS: TestRestrictedIdentityAdmissionPostgreSQL ' checkout-measurements.txt)" -ne 3 ]; then
    echo "M01 checkout requires three completed measurement trials"
    exit 1
  fi
fi
