# Demo Seed Data

Idempotent deterministic demo seed for local development and testing. Provides all the data needed to exercise the full MediSync flow: login, kiosk auth, prescription listing, withdrawal, and refill.

## Quick Start

```bash
# 1. Start infrastructure
npm run infra:up

# 2. Seed demo data
cd services/core
DATABASE_URL="postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable" \
  go run ./cmd/demoseed

# 3. Start the core (or rebuild the container)
npm run core
```

## Commands

```bash
# Seed (idempotent — safe to re-run)
cd services/core
go run ./cmd/demoseed

# Reset and re-seed (clears demo data first)
go run ./cmd/demoseed --reset

# Smoke test (requires running core + postgres)
DATABASE_URL="postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable" \
  bash scripts/test/smoke_demo_seed.sh
```

## Demo Credentials

> ⚠️ **LOCAL DEVELOPMENT ONLY — DO NOT USE IN PRODUCTION**
> All passwords below are dev-only. Never commit or deploy these credentials.

### Staff Logins

| Username | Password | Role | Wards |
|----------|----------|------|-------|
| `admin` | `medisync-local-admin-2026` | ADMIN | All (no ward scoping) |
| `pharmacist` | `demo-pharmacist-2026` | PHARMACIST | WARD-3A |
| `nurse` | `demo-nurse-2026` | NURSE | WARD-3A |
| `refiller` | `demo-refiller-2026` | REFILLER | WARD-3A |

The `admin` account is created by the core's bootstrap mechanism (`SeedAdmin`). The seed tool creates the remaining three via `ON CONFLICT DO NOTHING` so re-running is safe.

### Kiosk

| Field | Value |
|-------|-------|
| Code | `DEMO-K1` |
| PIN | `123456` |
| Cabinet | CAB1 |

The PIN is hashed with bcrypt before storage. The plaintext is never persisted — it is output only when the seed tool runs.

### URLs (Vite Dev)

- Core API: `http://localhost:8080`
- Admin app: `http://localhost:5173`
- Kiosk app: `http://localhost:5174`

## Seeded Data

### Drugs

| Code | Name | Generic Name (Thai) | Form | Strength | Unit |
|------|------|---------------------|------|----------|------|
| `DEMO-PARA500` | Paracetamol 500 mg | พาราเซตามอล 500 มก. | tablet | 500 mg | tablet |
| `DEMO-AMOX500` | Amoxicillin 500 mg | อะม็อกซีซิลลิน 500 มก. | capsule | 500 mg | capsule |
| `DEMO-OME20` | Omeprazole 20 mg | โอมีพราโซล 20 มก. | capsule | 20 mg | capsule |

### Slots (Cabinet CAB1)

| Slot | Drug | Capacity | Quantity | Low Threshold |
|------|------|----------|----------|---------------|
| S01 | DEMO-PARA500 | 100 | 80 | 20 |
| S02 | DEMO-AMOX500 | 100 | 60 | 20 |
| S03 | DEMO-OME20 | 50 | 45 | 10 |

### Prescription

| Field | Value |
|-------|-------|
| ID | `DEMO-RX-001` |
| Source System | `demo-seed` |
| Patient HN | `HN100001` |
| Patient Name | Demo Patient |
| Ward | `WARD-3A` |
| State | `READY` |
| Items | Paracetamol 500mg × 2, Amoxicillin 500mg × 3 |

## Idempotency

All inserts use `ON CONFLICT DO NOTHING` (or `ON CONFLICT ... DO UPDATE` for drugs/slots to refresh fields). Re-running the seed multiple times will not duplicate rows.

- `DEMO-RX-001` uses the `prescription_external_key` constraint `(prescription_id, source_system)`
- Users use the `username` unique constraint
- Kiosk uses the `code` unique constraint
- Drugs use the `code` unique constraint
- Slots use the `(cabinet_id, code)` unique constraint

## Reset

```bash
# Clear all demo data and re-seed
go run ./cmd/demoseed --reset
```

The reset deletes:
- Demo prescriptions (`source_system = 'demo-seed'`)
- Demo outbox entries
- Demo slots (`cabinet_id = 'CAB1'`)
- Demo drugs (`code LIKE 'DEMO-%'`)
- Demo staff users (`pharmacist`, `nurse`, `refiller`)
- Demo kiosk (`DEMO-K1`)
- Demo audit records

The `admin` user is NOT removed since it is managed by the core's own bootstrap mechanism.

## Fake Vending Requirement

For the full withdrawal and refill flows to work, `VENDING_FAKE=true` must be set. In the `infra/docker-compose.yml`, the core service already sets this. When running locally:

```bash
export VENDING_FAKE=true
export VENDING_URL=http://localhost:3000
```

Without fake vending, the dispense command will attempt to reach the real vending-3d-ctl-agent and fail if it's not available.

## Verification Flow

After seeding and starting the core:

```bash
# 1. Login as nurse
curl -s -X POST http://localhost:8080/medisync.identity.v1.IdentityService/Login \
  -H "Content-Type: application/json" \
  -d '{"username":"nurse","password":"demo-nurse-2026"}'

# 2. List prescriptions (use the token from step 1)
curl -s -X POST http://localhost:8080/medisync.dispensing.v1.DispensingService/ListPrescriptions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"ward_id":"WARD-3A"}'

# 3. Kiosk login
curl -s -X POST http://localhost:8080/medisync.kiosk.v1.KioskService/KioskLogin \
  -H "Content-Type: application/json" \
  -d '{"code":"DEMO-K1","pin":"123456"}'

# 4. Dispense (uses the kiosk token)
curl -s -X POST http://localhost:8080/medisync.dispensing.v1.DispensingService/Dispense \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <kiosk_token>" \
  -d '{"prescription_id":"DEMO-RX-001","source_system":"demo-seed","trace_id":"smoke-test-001"}'
```

## Cleanup

```bash
# Stop all services and remove volumes (DESTRUCTIVE)
docker compose -f infra/docker-compose.yml down -v

# Or just reset demo data:
cd services/core && go run ./cmd/demoseed --reset
```
