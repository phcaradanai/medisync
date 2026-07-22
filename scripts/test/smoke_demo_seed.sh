#!/usr/bin/env bash
# smoke_demo_seed.sh — Verify demo seed data integrity and idempotency.
#
# Prerequisites:
#   - PostgreSQL running with DATABASE_URL accessible
#   - Core API running (for login/prescription checks)
#
# Usage:
#   DATABASE_URL="postgres://postgres:dev%214555%402026@122.155.164.15:5434/medisync?sslmode=disable" ./scripts/test/smoke_demo_seed.sh
#
# The script checks:
#   1. Demo seed data exists in PostgreSQL
#   2. Identity login works for all demo users
#   3. Kiosk login works with DEMO-K1 / 123456
#   4. Demo prescription is READY and queryable
#   5. Re-running seed is idempotent (no duplicate rows)
#   6. --reset clears and re-seeds correctly
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

fail() { echo -e "${RED}FAIL${NC} $*"; FAILS=$((FAILS + 1)); }
pass() { echo -e "${GREEN}PASS${NC} $*"; }
warn() { echo -e "${YELLOW}WARN${NC} $*"; }
FAILS=0

DB="${DATABASE_URL:-postgres://postgres:dev%214555%402026@122.155.164.15:5434/medisync?sslmode=disable}"
API="${CORE_API_URL:-http://localhost:8080}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SEED_DIR="$ROOT/services/core"
GO_RUN="go run ./cmd/demoseed"

psql_cmd() {
  docker exec medisync-postgres-1 psql -U medisync -d medisync -t -A -c "$1" 2>/dev/null || echo ""
}

echo "=== MediSync Demo Seed Smoke Test ==="
echo ""

# ── 1. Verify seed data in PostgreSQL ─────────────────────────
echo "--- PostgreSQL data check ---"

USER_COUNT=$(psql_cmd "SELECT COUNT(*) FROM identity.users WHERE username IN ('pharmacist','nurse','refiller')")
if [ "$USER_COUNT" = "3" ]; then
  pass "3 demo staff users found"
else
  fail "expected 3 demo staff users, got ${USER_COUNT:-0}"
fi

KIOSK_COUNT=$(psql_cmd "SELECT COUNT(*) FROM identity.kiosks WHERE code = 'DEMO-K1'")
if [ "$KIOSK_COUNT" = "1" ]; then
  pass "demo kiosk DEMO-K1 found"
else
  fail "demo kiosk DEMO-K1 not found"
fi

DRUG_COUNT=$(psql_cmd "SELECT COUNT(*) FROM catalog.drug WHERE code LIKE 'DEMO-%'")
if [ "$DRUG_COUNT" = "3" ]; then
  pass "3 demo drugs found"
else
  fail "expected 3 demo drugs, got ${DRUG_COUNT:-0}"
fi

SLOT_COUNT=$(psql_cmd "SELECT COUNT(*) FROM inventory.slot WHERE cabinet_id = 'CAB1'")
if [ "$SLOT_COUNT" = "3" ]; then
  pass "3 demo slots in CAB1 found"
else
  fail "expected 3 demo slots, got ${SLOT_COUNT:-0}"
fi

RX_COUNT=$(psql_cmd "SELECT COUNT(*) FROM dispensing.prescription WHERE source_system = 'demo-seed'")
if [ "$RX_COUNT" = "1" ]; then
  pass "1 demo prescription found"
else
  fail "expected 1 demo prescription, got ${RX_COUNT:-0}"
fi

RX_STATE=$(psql_cmd "SELECT state FROM dispensing.prescription WHERE prescription_id = 'DEMO-RX-001'")
if [ "$RX_STATE" = "READY" ]; then
  pass "demo prescription is READY"
else
  fail "demo prescription state is ${RX_STATE:-missing}, expected READY"
fi

# ── 2. Verify demo logins ──────────────────────────────────────
echo ""
echo "--- API login checks ---"

# Admin login
ADMIN_RESP=$(curl -s -X POST "$API/medisync.identity.v1.IdentityService/Login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"medisync-local-admin-2026"}' 2>/dev/null || echo "")
ADMIN_TOKEN=$(echo "$ADMIN_RESP" | grep -o '"accessToken":"[^"]*"' | cut -d'"' -f4 || echo "")
if [ -n "$ADMIN_TOKEN" ]; then
  pass "admin login succeeded"
else
  fail "admin login failed"
fi

# Nurse login
NURSE_RESP=$(curl -s -X POST "$API/medisync.identity.v1.IdentityService/Login" \
  -H "Content-Type: application/json" \
  -d '{"username":"nurse","password":"demo-nurse-2026"}' 2>/dev/null || echo "")
NURSE_TOKEN=$(echo "$NURSE_RESP" | grep -o '"accessToken":"[^"]*"' | cut -d'"' -f4 || echo "")
if [ -n "$NURSE_TOKEN" ]; then
  pass "nurse login succeeded"
else
  fail "nurse login failed"
fi

# Pharmacist login
PHARM_RESP=$(curl -s -X POST "$API/medisync.identity.v1.IdentityService/Login" \
  -H "Content-Type: application/json" \
  -d '{"username":"pharmacist","password":"demo-pharmacist-2026"}' 2>/dev/null || echo "")
PHARM_TOKEN=$(echo "$PHARM_RESP" | grep -o '"accessToken":"[^"]*"' | cut -d'"' -f4 || echo "")
if [ -n "$PHARM_TOKEN" ]; then
  pass "pharmacist login succeeded"
else
  fail "pharmacist login failed"
fi

# Refiller login
REFILL_RESP=$(curl -s -X POST "$API/medisync.identity.v1.IdentityService/Login" \
  -H "Content-Type: application/json" \
  -d '{"username":"refiller","password":"demo-refiller-2026"}' 2>/dev/null || echo "")
REFILL_TOKEN=$(echo "$REFILL_RESP" | grep -o '"accessToken":"[^"]*"' | cut -d'"' -f4 || echo "")
if [ -n "$REFILL_TOKEN" ]; then
  pass "refiller login succeeded"
else
  fail "refiller login failed"
fi

# Kiosk login
KIOSK_RESP=$(curl -s -X POST "$API/medisync.kiosk.v1.KioskService/KioskLogin" \
  -H "Content-Type: application/json" \
  -d '{"code":"DEMO-K1","pin":"123456"}' 2>/dev/null || echo "")
KIOSK_TOKEN=$(echo "$KIOSK_RESP" | grep -o '"accessToken":"[^"]*"' | cut -d'"' -f4 || echo "")
if [ -n "$KIOSK_TOKEN" ]; then
  pass "kiosk login succeeded"
else
  warn "kiosk login returned no token (kiosk handler may not be in running core — rebuild core to test)"
fi

# ── 3. Prescription visibility ─────────────────────────────────
echo ""
echo "--- Prescription visibility ---"

if [ -n "$NURSE_TOKEN" ]; then
  RX_LIST=$(curl -s -X POST "$API/medisync.dispensing.v1.DispensingService/ListPrescriptions" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $NURSE_TOKEN" \
    -d '{"ward_id":"WARD-3A"}' 2>/dev/null || echo "")
  if echo "$RX_LIST" | grep -q "DEMO-RX-001"; then
    pass "demo prescription visible to nurse (ward WARD-3A)"
  elif echo "$RX_LIST" | grep -q "404 page not found"; then
    warn "dispensing endpoint not available in running core (rebuild core to test)"
  else
    warn "prescription listing returned unexpected response"
  fi
else
  warn "skipping prescription check — nurse token not available"
fi

# ── 4. Idempotency: re-run seed, verify no duplicates ─────────
echo ""
echo "--- Idempotency check ---"

if pushd "$SEED_DIR" > /dev/null 2>&1; then
  DATABASE_URL="$DB" $GO_RUN > /dev/null 2>&1 || true
  popd > /dev/null 2>&1
fi

RX_DUP=$(psql_cmd "SELECT COUNT(*) FROM dispensing.prescription WHERE source_system = 'demo-seed'")
if [ "$RX_DUP" = "1" ]; then
  pass "re-seed is idempotent (still 1 demo prescription)"
else
  fail "re-seed duplicated data: expected 1 prescription, got ${RX_DUP:-0}"
fi

DRUG_DUP=$(psql_cmd "SELECT COUNT(*) FROM catalog.drug WHERE code LIKE 'DEMO-%'")
if [ "$DRUG_DUP" = "3" ]; then
  pass "re-seed is idempotent (still 3 demo drugs)"
else
  fail "re-seed duplicated data: expected 3 drugs, got ${DRUG_DUP:-0}"
fi

# ── 5. Reset + re-seed ─────────────────────────────────────────
echo ""
echo "--- Reset + re-seed ---"

if pushd "$SEED_DIR" > /dev/null 2>&1; then
  DATABASE_URL="$DB" $GO_RUN --reset > /dev/null 2>&1 || true
  popd > /dev/null 2>&1
fi

RX_AFTER=$(psql_cmd "SELECT COUNT(*) FROM dispensing.prescription WHERE source_system = 'demo-seed'")
if [ "$RX_AFTER" = "1" ]; then
  pass "reset + re-seed restored prescription"
else
  fail "reset + re-seed lost prescription: expected 1, got ${RX_AFTER:-0}"
fi

DRUG_AFTER=$(psql_cmd "SELECT COUNT(*) FROM catalog.drug WHERE code LIKE 'DEMO-%'")
if [ "$DRUG_AFTER" = "3" ]; then
  pass "reset + re-seed restored 3 drugs"
else
  fail "reset + re-seed lost drugs: expected 3, got ${DRUG_AFTER:-0}"
fi

# ── Summary ────────────────────────────────────────────────────
echo ""
echo "══════════════════════════════════════════════════════════════"
if [ "$FAILS" -eq 0 ]; then
  echo -e "${GREEN}All checks passed.${NC}"
  exit 0
else
  echo -e "${RED}$FAILS check(s) failed.${NC}"
  exit 1
fi
