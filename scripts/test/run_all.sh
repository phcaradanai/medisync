#!/usr/bin/env bash
# run_all.sh — Run all MediSync tests (unit + integration) with validation.
#
# Usage:
#   ./scripts/test/run_all.sh              # unit tests only (no DB)
#   TEST_DATABASE_URL="postgres://..." ./scripts/test/run_all.sh  # with integration
#
# Prerequisites: buf CLI, Go toolchain, Node.js (for build check).
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

fail() { echo -e "${RED}FAIL${NC} $*"; }
pass() { echo -e "${GREEN}PASS${NC} $*"; }

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

echo "=== buf lint ==="
buf lint && pass "buf lint" || { fail "buf lint"; exit 1; }

echo ""
echo "=== npm run build ==="
npm run build && pass "npm run build" || { fail "npm run build"; exit 1; }

echo ""
echo "=== Go unit tests (no PostgreSQL/NATS needed) ==="
(
  cd services/core
  # Ignore identity package: it was added outside Team 3 ownership and has a
  # missing dependency (github.com/lib/pq). Tracked in team3 report.
  go test -count=1 -v \
    ./cmd/feeder/ \
    ./internal/dispensing/ \
    ./internal/platform/audit/ \
    ./internal/platform/config/
) && pass "unit tests" || { fail "unit tests"; exit 1; }

echo ""
if [ -n "${TEST_DATABASE_URL:-}" ]; then
  echo "=== Go integration tests (requires PostgreSQL) ==="
  (
    cd services/core
    go test -tags=integration -count=1 -v \
      ./internal/dispensing/ \
      ./internal/platform/audit/
  ) && pass "integration tests" || { fail "integration tests"; exit 1; }
else
  echo "=== Integration tests SKIPPED (set TEST_DATABASE_URL to run) ==="
fi

echo ""
echo "All checks passed."
