# MediSync

Hospital medication dispensing platform: automated vending cabinet + kiosk UI + admin app. Event-driven around NATS JetStream, Go modular monolith core, React/TS frontends.

## What it does

A prescription flows from the hospital → `rx.prescription.created` on JetStream. At one code-scoped kiosk, staff scan its Sticker, review reserved stock, authenticate, and queue that cabinet's `vending-3d-ctl-agent`. Hardware truth updates stock and audit, and `rx.prescription.dispense_result` returns the terminal result to the originating producer. Emergency withdrawals without a Prescription use a separate transaction flow.

## Event Chain

```
rx.prescription.created → READY → Dispense RPC → DISPENSING
  → vending consumer → vending agent → dispense.completed
    → completion consumer → DISPENSED + stock.changed + print.requested
      → transactional outbox → rx.prescription.dispense_result
```

## Documents

| Doc | Purpose |
|---|---|
| [PRODUCT.md](PRODUCT.md) | Users, purpose, brand, design principles |
| [DESIGN.md](DESIGN.md) | Visual system (PrintOps "Lab Notebook" + kiosk scale) |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Services, bounded contexts, proto contracts, events, security |
| [docs/MILESTONES.md](docs/MILESTONES.md) | 10-day plan, definition of done |
| [RUNBOOK.md](RUNBOOK.md) | Deployment, operations, recovery, monitoring |
| [docs/EVENTS.md](docs/EVENTS.md) | NATS subject registry |

## Stack

- **Core:** Go 1.26 modular monolith (Connect-RPC, PostgreSQL, NATS JetStream)
- **Frontends:** React 19 + Vite 7 + TypeScript 5.9, `@bufbuild/protobuf`, UnoCSS
- **Hardware:** `../vending-3d-ctl-agent` (serial cabinet), `../print_ops` (sticker) — reused
- **Bundled:** `infra/docker-compose.yml` (postgres + nats + core)

## Layout

```
proto/               # buf-managed contracts (source of truth)
  medisync/
    identity/v1/     # users, roles, auth
    catalog/v1/      # drug master catalog
    inventory/v1/    # slots, stock, refill
    dispensing/v1/   # prescription state machine
    kiosk/v1/        # kiosk terminal registry
    cabinet/v1/      # physical cabinet registry
    events/v1/       # JetStream event payloads
services/core/       # Go backend (modular monolith)
  internal/
    platform/        # config, logging, postgres, nats, audit, ratelimit
    identity/        # auth, users, kiosk management
    catalog/         # drug CRUD
    inventory/       # slot mapping, stock, refill
    dispensing/      # prescription state machine + consumers
    vending/         # vending-3d-ctl-agent client + consumer
    printing/        # print_ops client + consumer
    cabinet/         # cabinet registry
apps/
  kiosk/             # cabinet touch UI (React + Vite, :5175 in docker, :5173 dev)
  admin/             # management UI (React + Vite, :5176 in docker, :5174 dev)
packages/proto-ts/   # generated TS types (shared)
infra/               # docker compose files
migrations/          # PostgreSQL migrations (0001-0009)
```

## Quick Start

Prerequisites: Go ≥1.26, Node ≥20, Docker.

```bash
npm install              # workspaces (apps + packages)

# ── Start everything ──
npm run dev:all          # postgres + nats + core (Docker) + admin + kiosk

# ── Or per layer ──
npm run infra:up         # postgres:5432 + nats:4222 + core:8080
npm run dev              # admin (:5174) + kiosk (:5173) dev servers

# ── Demo data (optional) ──
npm run seed:demo        # drugs, slots, kiosk for testing
```

### URLs

| Surface | URL | Login |
|---|---|---|
| Admin | http://localhost:5176 | `admin` / `medisync-local-admin-2026` |
| Kiosk | http://localhost:5175 | Code: `00010001` PIN: `123456` (after `seed:demo`) |
| Core API | http://localhost:8080 | Connect-RPC |

## Admin Features

| Page | What you can do |
|---|---|
| **Drugs** | CRUD drug catalog, search by code/name, activate/deactivate |
| **Inventory** | Slot management — assign drugs, refill/adjust stock, configure emergency eligibility and limit by kiosk/slot code |
| **Users** | User management — create/edit, roles (admin/pharmacist/nurse/refiller), ward scopes |
| **Kiosks** | Register kiosk terminals, PIN management, activate/deactivate |
| **Cabinets** | Register physical vending machines |
| **Dispense Transactions** | Separate Prescription/Emergency reports, filters, allocation details, and CSV export |

## Kiosk Features

| Mode | Flow |
|---|---|
| **Withdraw** | Login → hardware check for this kiosk → scan Sticker → review cart → scan staff card → tracked hardware result |
| **Emergency** | HN + staff card/employee code → select configured emergency drug in this kiosk → separate tracked transaction |
| **Refill** | Toggle refill mode → low stock / all slots → enter qty → confirm → done |

## Scripts Reference

| Script | Purpose |
|---|---|
| `npm run dev:all` | Start everything (infra + admin + kiosk) |
| `npm run infra:up` | Start docker services |
| `npm run infra:down` | Stop docker services |
| `npm run core` | Run Go core locally (for dev) |
| `npm run core:dev` | infra (postgres+nats only) + core local + admin + kiosk |
| `npm run feeder` | Publish mock prescriptions to NATS |
| `npm run dev` | Start admin + kiosk |
| `npm run dev:admin` | Start admin only |
| `npm run dev:kiosk` | Start kiosk only |
| `npm run seed:demo` | Seed demo data (drugs, slots, kiosk) |
| `npm run seed:demo-reset` | Reset and re-seed demo data |
| `npm run proto` | Regenerate proto (buf lint + generate) |
| `npm run build` | Build all workspaces |
| `npm run test:core` | Run Go tests |
| `npm run test:all` | Run all Go tests |
| `npm run smoke:demo` | End-to-end smoke test |

## Test Status

| Suite | Result |
|---|---|
| Go: identity, catalog, inventory, dispensing, vending, printing, cabinet, platform | All pass |
| Admin (TS): login + drugs + emergency inventory configuration + dispense reports | 13/13 |
| Kiosk (TS): login + withdraw + emergency + queue/history | 44/44 |

## Milestone Status

| M | Scope | Status |
|---|---|---|
| M1 | Foundations | ✅ |
| M2 | Core domain | ✅ |
| M3 | Hardware & printing bridges | ✅ |
| M4 | Kiosk refill + withdraw | ✅ |
| M5 | Admin CRUD | ✅ |
| M6 | Hardware integration | ⬜ |
| M7 | Ship | ✅ |

## Config

Default DB: `postgres://medisync:***@localhost:5432/medisync`  
Core config via env — see `services/core/internal/platform/config/config.go`  
Env template: `.env.example` (dev) / `.env.production.example` (prod)

## Deployment

### Development (local Docker)
```bash
# From workspace root
cp .env .env.local && vim .env.local    # set dev secrets
docker compose --env-file .env.local up -d --build

# Seed demo data (optional)
docker compose exec medisync-core /app/demoseed --project default

# Verify
curl http://localhost:8080/health
open http://localhost:5175  # kiosk
open http://localhost:5176  # admin (login: admin / <ADMIN_BOOTSTRAP_PASSWORD>)
```

### Production
```bash
# 1. Copy and fill production secrets
cp .env.production .env.prod.local
vim .env.prod.local  # set ALL required passwords/keys

# 2. Generate secrets (example)
openssl rand -hex 32  # JWT_SECRET, CARD_TOKEN_HMAC_KEY
openssl rand -base64 32  # API keys

# 3. Deploy
docker compose -f docker-compose.prod.yml --env-file .env.prod.local up -d --build

# 4. Verify
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs medisync-core

# 5. Seed initial data
docker compose -f docker-compose.prod.yml exec medisync-core /app/demoseed --project default
```

### Backup
```bash
# Database
docker compose exec postgres pg_dump -U medisync medisync > backup-$(date +%Y%m%d).sql

# Restore
docker compose exec -T postgres psql -U medisync medisync < backup.sql
```

Fake clients: `PRINT_OPS_FAKE=true`, `VENDING_FAKE=true` for dev without external services

# rebuild แก้ core (clean)
docker compose up -d --build --force-recreate core
docker compose logs -f core
