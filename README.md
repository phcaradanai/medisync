# MediSync

Hospital medication dispensing platform: automated vending cabinet + kiosk UI + admin app, event-driven around NATS JetStream, Go core, React/TS frontends.

## What it does

A prescription flows in from the hospital (external feeder service → `rx.prescription.created` on JetStream). Ward staff authenticate at the cabinet kiosk, confirm the withdrawal, the cabinet dispenses the medication (via the existing `vending-3d-ctl-agent`), a sticker prints (via the existing `print_ops` gateway), stock is decremented, and every step is audited.

## Documents

| Doc | Purpose |
|---|---|
| [PRODUCT.md](PRODUCT.md) | Users, purpose, brand, design principles |
| [DESIGN.md](DESIGN.md) | Visual system (inherits PrintOps "Lab Notebook", adds kiosk scale) |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Services, bounded contexts, proto contracts, event flow, security |
| [docs/MILESTONES.md](docs/MILESTONES.md) | 10-day plan, definition of done, working agreement |

## Stack

- **Core:** Go modular monolith (DDD bounded contexts), Connect-RPC (proto-first, buf), PostgreSQL, NATS JetStream
- **Frontends:** React + Vite + TypeScript (kiosk, admin), Connect-Web clients
- **Hardware:** `../vending-3d-ctl-agent` (serial cabinet control), `../print_ops` (sticker printing) — reused, not rewritten

## Layout

```
proto/               # buf-managed contracts (source of truth)
services/core/       # Go backend (modular monolith)
apps/kiosk/          # cabinet touch UI (React + Vite)
apps/admin/          # management UI (React + Vite)
packages/proto-ts/   # generated TS types shared by both apps
infra/               # docker compose (postgres + NATS JetStream)
docs/                # architecture, milestones, event registry
```

## Development

Prerequisites: Go ≥1.26, Node ≥20, Docker, [buf](https://buf.build).

```bash
npm install                 # workspaces (apps + packages)
npm run infra:up            # postgres:5432 + NATS:4222 (JetStream)
npm run core                # Go core: migrations, streams, consumers
npm run feeder -- -count 3  # publish mock prescriptions (see docs/EVENTS.md)
npm run dev:kiosk           # http://localhost:5173
npm run dev:admin           # http://localhost:5174
npm run proto               # regenerate after editing proto/ (buf lint + generate)
npm run test:core           # Go tests
```

Default DB: `postgres://medisync:medisync@localhost:5432/medisync`. Core config via env — see `services/core/internal/platform/config`.
