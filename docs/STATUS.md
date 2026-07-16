# Project Status

**Last updated:** 2026-07-16

## Current Milestone

**Connected M3-M5 delivery** — M2 is complete. Hardware/printing bridges, kiosk authentication, withdraw/refill UI, Admin CRUD, and deterministic demo seed now exist, but release is not accepted until project-scoped authorization and a fresh Compose end-to-end run pass.

## Current Integration Status

- Project/tenant scope is the active blocker. The current kiosk JWT has kiosk identity but no project, cabinet, or ward authorization context.
- Admin has initial users, kiosks, drugs, and inventory surfaces; they need a shared project selector, permission-aware actions, and browser acceptance.
- Kiosk code/PIN login, session restore, withdrawal, and refill screens exist. They need scoped claims and live fulfillment verification.
- Core has vending and printing consumers plus fake hardware support. The stock/print/audit terminal-state path still needs independent Compose acceptance.
- Demo seed is available through `npm run seed:demo`, reset through `npm run seed:demo-reset`, and documented in `docs/DEMO_SEED.md`.

## Completed Work

### Repo Scaffolding
- Go module (`services/core`) with buf-managed proto contracts
- Vite + React + TypeScript apps: `apps/kiosk`, `apps/admin`
- Docker compose: PostgreSQL 16 + NATS 2.10 JetStream (healthy)

### Proto Contracts (v1)
- `proto/medisync/identity/v1/identity.proto`
- `proto/medisync/catalog/v1/catalog.proto`
- `proto/medisync/inventory/v1/inventory.proto`
- `proto/medisync/dispensing/v1/dispensing.proto`
- `proto/medisync/events/v1/events.proto` (self-contained, no service imports)

### Platform Package
- `config` — centralized env parsing with defaults and validation
- `logging` — structured JSON logger (slog)
- `postgres` — connection pool with retry, migration runner (advisory-lock serialized)
- `natsx` — NATS connection with retry, JetStream stream topology (RX + MEDISYNC)
- `audit` — append-only audit writer (action/entity validation, default actor, JSON detail)
- `testutil` — shared `Execer` interface + `FakeExecer` for deterministic unit testing

### Dispensing (M1 scope)
- `PrescriptionCreated` event consumer (durable, explicit ack, backoff)
- Validation: required fields, positive quantity, non-empty items
- Poison message handling: malformed payloads → DLQ + audit, no retry
- Idempotent upsert by `(prescription_id, source_system)`
- Audit trail: `prescription.received` + `prescription.rejected` entries

### Mock Feeder
- `cmd/feeder` publishes protojson `PrescriptionCreated` to JetStream
- `Nats-Msg-Id` header for broker-level dedupe
- Configurable count, ward, source, fixed ID

### Identity (M2 — complete)
- bcrypt password hashing and verification with input limits
- JWT issue/parse with exact HS256 enforcement and required expiry
- Password login, card login, and `WhoAmI` service flows
- Connect-RPC handlers with safe error mapping and secret-free responses
- Core HTTP wiring: Connect handler mounted, admin seeded with bcrypt from `ADMIN_BOOTSTRAP_PASSWORD`
- Graceful shutdown: signal handling, 10s deadline, NATS drain, DB pool close
- Login rate limiting (Team 9 PASS WITH FINDINGS)
- Live Docker smoke: Login→JWT, WhoAmI validates, restart idempotent, all error codes safe and uniform
- Card tokens stored only as HMAC-SHA256 raw 32-byte `BYTEA`; plaintext column and raw-token fallback removed
- Card lookup and enrollment fail closed without hashing configuration; normal reads never expose hashes
- Production environment validation rejects missing, placeholder, and short card-token keys without printing secrets
- Review R2 PASS; fresh-database migrations, PostgreSQL integration, Linux race, Compose, and Login/WhoAmI restart smoke verified

### Catalog (M2 — complete)
- Drug master CRUD: create, activate, deactivate, search with pg_trgm trigram fuzzy match
- `CREATE EXTENSION pg_trgm` in migration for fresh-DB compatibility
- Team 10 PASS WITH FINDINGS (`t_45f16032`)

### Inventory (M2 — complete)
- Cabinet slots, drug↔slot mapping, stock counts, low-stock thresholds
- Stock adjustments, refill commands, `stock.changed` and `stock.low` domain events
- Team 11 PASS WITH FINDINGS (`t_ff729106`)

### Dispensing (M2 — complete)
- Prescription aggregate + state machine (49 validated edges): `RECEIVED → READY → DISPENSING → DISPENSED | FAILED | CANCELLED | EXPIRED`
- JetStream consumer with idempotent upsert, outbox publisher (atomic DB row + outbox in one tx)
- Ward-scoped authorization enforced server-side on all queries/commands
- Team 12 PASS WITH FINDINGS (`t_cbed18c6`) — 294 tests on PG16, zero races. Findings: F2 MEDIUM (404 vs 403 ward enum leak), F1/F3 LOW

## Tests

### Unit Tests (no PostgreSQL or NATS required)

Repository-wide `go test ./...` passes. No infrastructure dependencies.

| Package | Tests | Type | Coverage |
|---|---|---|---|
| `cmd/core` | 10 | HTTP-level Connect (httptest) | Login, WhoAmI, bootstrap, mux routing |
| `cmd/feeder` | 7 | pure unit | required fields, unique IDs, marshal roundtrip |
| `internal/dispensing` | 9 | 3 validation + 6 store (fake DB) | validation, SQL args, insert/duplicate, DB errors, READY state |
| `internal/identity` | 118 | auth, handler, jwt, password, store, card hashing | LoginPassword/LoginCard/WhoAmI, HMAC hashing, hashed enrollment/lookup, fail-closed behavior, proto mapping, HS256 enforcement, bcrypt |
| `internal/platform/config` | 25 | pure unit | defaults, overrides, JWT expiry, required secret validation, card HMAC key validation |
| `internal/platform/audit` | 9 | pure unit (fake DB) | action/entity validation, default actor, detail serialization, DB errors |

### Integration Tests (27 total — requires PostgreSQL)

Run with: `TEST_DATABASE_URL="..." go test -tags=integration -count=1 ./...`

All integration tests use transactions with rollback — they leave zero rows behind.

| Package | Tests | Type |
|---|---|---|
| `internal/dispensing` | 5 | tx-rolled-back integration | insert, duplicate, different source, items roundtrip, READY state |
| `internal/platform/audit` | 5 | tx-rolled-back integration | default actor, explicit actor, detail roundtrip, empty detail, full audit log |
| `internal/identity` | 17 | tx-rolled-back integration | GetByUsername/ID/CardToken, hashed enrollment, plaintext-column removal, SeedAdmin, schema/table/constraints |

Integration tests read `TEST_DATABASE_URL` and fail with `t.Fatal` (not `t.Skip`) when the variable is unset or the database is unreachable.

### Smoke Test Evidence (2026-07-13)

1. **Core service starts** — DB migrations applied, NATS streams ensured, consumer registered
2. **Mock prescription consumed** — `RX-SMOKE-001` published → consumer received → PostgreSQL row created (state=READY)
3. **Audit record** — `prescription.received` entry written with ward_id, items count, source_system
4. **Duplicate idempotency** — Re-published `RX-SMOKE-001` → PostgreSQL count still 1 (no duplicate)
5. **Malformed rejection** — Empty payload published → consumer rejected → DLQ published → audit `prescription.rejected` recorded
6. **Identity admin seed** — "admin user created" on first start, "already exists" on restart
7. **Identity Login/WhoAmI** — Login returns JWT, WhoAmI validates, JWT expiry enforced, 401 on bad credentials (no user enumeration)
8. **Safe shutdown** — SIGTERM → 10s graceful drain → NATS drained → DB pool closed

## Verification Commands

```bash
buf lint                # PASS
buf generate            # PASS (Go + TS)
go test -count=1 ./...  # PASS (all packages)
go vet ./...            # PASS
go test -tags=integration -count=1 ./...  # PASS (23 integration tests, with TEST_DATABASE_URL)
npm run build           # PASS (kiosk + admin)
docker compose -f infra/docker-compose.yml up -d --wait  # PASS (core healthy)
```

## Known Data Pollution

Prior test runs (before Team 3 corrections) left persistent rows in the development database:

| Table | Polluted Rows | Source |
|---|---|---|
| `dispensing.prescription` | 37 rows | old store_test.go (no cleanup) |
| `audit.audit_log` | 30 rows (entity='test') | old audit_test.go (no cleanup) |

**Cleanup query (requires user approval before execution):**

```sql
DELETE FROM dispensing.prescription WHERE source_system LIKE 'test-%' OR prescription_id LIKE 'RX-%';
DELETE FROM audit.audit_log WHERE entity = 'test';
```

Current integration tests use transactions with explicit rollback and do not add to this pollution.

## Blockers

| Item | Status | Resolution |
|---|---|---|
| Producer-team confirmation of `rx.prescription.created` | **FOLLOW-UP** | Current v1 contract is canonical by project-owner decision; validate a real producer payload before production |

## Readiness for M2

**M1 technical slice: COMPLETE.** Infrastructure, proto contracts, platform, dispensing consumer, and feeder pass their scoped tests. Unit tests (30) are pure and infrastructure-independent; integration tests (10) are isolated with tx rollback. Smoke test confirms end-to-end event flow.

**Identity: COMPLETE.** Team 4 (`t_3e11bdeb`) added the foundation, Team 6 (`t_b955eab1`) added authentication and Connect handlers, Team 7 (`t_be252384`) integrated HTTP startup and admin seeding, and Teams 8/8b completed card-token hardening. Review R2 (`t_ac7e11f5`) returned PASS.

**Repository health: READY.** Repository-wide unit tests and 23 integration tests pass. Go vet, Linux race tests, frontend builds, production Compose validation, and local Compose runtime smoke pass.

**M1: COMPLETE.** Project owner accepted the current `rx.prescription.created` schema as canonical v1. No external approval is claimed. Future breaking changes require a new version and migration plan.

## Next Actions

1. `t_8ae8ba59` — project scope, memberships, permissions, and scoped kiosk claims.
2. `t_07f326b9`, `t_3101e0c5`, `t_53f8c8ef`, `t_68fb1a06` — parallel Admin, Kiosk, and fulfillment lanes after foundation review.
3. `t_f9aed710`, `t_f7b11503`, `t_fe376f28` — refill, withdrawal, and operations workflows.
4. `t_a19913c6` — independent fresh-Compose and browser acceptance.
5. Address Team 12 F2 and validate a real producer payload before production.
