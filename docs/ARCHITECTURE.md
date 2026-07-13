# MediSync Architecture

Hospital medication dispensing platform. Go core, event-driven via NATS JetStream, proto-first contracts, React/TS frontends. Composes two existing systems (`vending-3d-ctl-agent`, `print_ops`) instead of rewriting them.

## System landscape

```
                                  ┌─────────────────────────┐
  Hospital central DB ──► external│ prescription-feeder      │ (other team)
                                  │ publishes to JetStream   │
                                  └───────────┬─────────────┘
                                              │ NATS JetStream
                                              ▼
┌───────────────────────────── medisync (this repo) ─────────────────────────────┐
│                                                                                 │
│  apps/kiosk (React/TS) ──┐                                                      │
│                          │ Connect-RPC (proto over HTTP)                        │
│  apps/admin (React/TS) ──┤                                                      │
│                          ▼                                                      │
│                 services/core (Go, modular monolith)                            │
│                 ┌──────────────────────────────────────┐                        │
│                 │ identity   │ catalog    │ inventory   │                       │
│                 │ dispensing │ fulfillment│ printing    │                       │
│                 └──────────────────────────────────────┘                        │
│                      │              │              │                            │
│                      │ PostgreSQL   │ HTTP         │ HTTP (X-Api-Key)           │
│                      ▼              ▼              ▼                            │
│                  postgres    vending-3d-ctl-agent  print_ops                    │
│                              (existing, serial HW) (existing, sticker printer)  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Services (the five from the brief, mapped)

| # | Brief item | Realization |
|---|---|---|
| 1 | Hardware vending control | **Reuse `vending-3d-ctl-agent`** (Node/Express, serial). Core's `fulfillment` module is the only caller, via its HTTP API (`POST /api/v1/serial/vending/write`, health at `GET /api/v1/health`). |
| 2 | Kiosk app (cabinet UI) | **`apps/kiosk`** — React + Vite + TS, Connect-Web client, touch-first per DESIGN.md. Withdraw + refill flows. |
| 3 | Printer service | **Reuse `print_ops`** via `POST /api/v1/print-jobs` with `X-Api-Key` + idempotent `request_id`. Core's `printing` module owns the call; sticker payload built from prescription data. |
| 4 | Admin app | **`apps/admin`** — React + Vite + TS. Drug catalog, slot mapping, stock, users, ward-scoped roles. |
| 5 | Backend API | **`services/core`** — single Go binary, modular monolith with DDD bounded contexts. gRPC-ready internals; frontends talk Connect-RPC generated from proto. |

**Why modular monolith, not microservices, for the 10-day target:** one binary + one DB + one compose file removes deployment/network failure modes while keeping module boundaries (each bounded context has its own package, its own proto service, communicates through interfaces + domain events). Any module can be split into its own gRPC process later without touching the proto contracts.

## Bounded contexts (DDD)

- **identity** — users, credentials, QR/NFC card binding, roles (ADMIN / PHARMACIST / NURSE / REFILLER), ward-scoped permissions. Every request context carries `(user, ward)`; authorization is `can(user, action, ward)`.
- **catalog** — drug master data (code, name, form, strength, sticker template fields).
- **inventory** — cabinet slots, drug↔slot mapping, stock counts, low-stock thresholds. Stock mutations only via domain events from dispensing/refill — never direct edits without audit.
- **dispensing** — the heart. Prescription aggregate + state machine: `RECEIVED → READY → DISPENSING → DISPENSED | FAILED | CANCELLED | EXPIRED`. Consumes prescription events from JetStream, coordinates fulfillment + printing, emits domain events for every transition.
- **fulfillment** — anti-corruption layer over `vending-3d-ctl-agent`. Translates "dispense slot 12 × 2" into vending write payloads, interprets hardware responses/timeouts (504 = no RX), reports hardware truth back to dispensing.
- **printing** — anti-corruption layer over `print_ops`. Builds sticker content, submits idempotent print jobs, tracks job outcome.

Shared kernel: domain event envelope, IDs, audit log writer. Rule (borrowed from print_ops): **every state-mutating service writes an AuditLog entry, publishes the DomainEvent, and threads `trace_id` through both.**

## Contracts (proto-first)

`proto/` is the single source of truth, managed with **buf**. Generated code: Go (core), TS (kiosk/admin via Connect-Web).

```
proto/
  medisync/identity/v1/identity.proto      # Login, WhoAmI, card auth
  medisync/catalog/v1/catalog.proto        # Drug CRUD
  medisync/inventory/v1/inventory.proto    # Slots, stock, refill
  medisync/dispensing/v1/dispensing.proto  # Prescription queries, dispense commands
  medisync/events/v1/events.proto          # Domain event payloads on JetStream
```

Frontend transport: **Connect-RPC** (connectrpc.com) — proto-defined services served by the Go core over HTTP/1.1+2, consumed in the browser without a gRPC-web proxy, and the same definitions serve as internal gRPC if/when modules split. This satisfies "gRPC between backend services, proto-based API for frontends" with one toolchain.

## Event flow (NATS JetStream)

Streams and subjects (versioned, `.v1` suffix on payload schema):

| Subject | Producer | Consumer | Purpose |
|---|---|---|---|
| `rx.prescription.created` | external prescription-feeder (other team) | core/dispensing | New prescription for sticker + dispense |
| `medisync.dispense.requested` | core/dispensing | core/fulfillment | Command: dispense slot/qty |
| `medisync.dispense.completed` | core/fulfillment | core/dispensing, audit | Hardware-confirmed result |
| `medisync.dispense.failed` | core/fulfillment | core/dispensing, audit | Timeout/jam/error detail |
| `medisync.print.requested` | core/dispensing | core/printing | Sticker job |
| `medisync.print.completed` | core/printing | core/dispensing | print_ops outcome |
| `medisync.stock.changed` | core/inventory | admin (SSE/stream), audit | Stock delta after dispense/refill |
| `medisync.stock.low` | core/inventory | admin notification | Threshold crossed |

JetStream config: durable consumers per module, explicit ack, DLQ subject (`medisync.dlq.>`) for poison messages, `rx.*` stream retention = work-queue. **Contract with the external team:** agree the `rx.prescription.created` payload schema early (Day 1–2); until then, a mock feeder publishes the same subject.

Idempotency: prescription events carry `prescription_id` + `source_system`; dispensing upserts by that key (same pattern print_ops uses with `request_id`).

## Data (PostgreSQL)

One database, schema-per-context (`identity.*`, `catalog.*`, `inventory.*`, `dispensing.*`, `audit.*`). Migrations are plain SQL files embedded in the binary and applied at startup by a small runner in `internal/platform/postgres` (advisory-lock serialized, one tx per file) — no external migration tool. Outbox table in `dispensing` for reliable event publish (write DB row + outbox in one tx, relay to JetStream).

## Security

- Frontend auth: short-lived JWT (login or QR/NFC via kiosk); kiosk sessions auto-expire after inactivity
- Ward scoping enforced server-side in every handler via identity module, never client-side only
- Service-to-service: vending agent and print_ops reachable only on the internal network; print_ops via `X-Api-Key`; secrets from env, never committed
- Full audit: who dispensed what, when, from which slot, for which prescription — immutable append-only table
- TLS terminated at reverse proxy (compose: Caddy/nginx) for anything crossing machines

## Repo layout

```
medisync/
  proto/                  # buf-managed contracts (source of truth)
  services/core/          # Go modular monolith
    cmd/core/             # main
    internal/identity/    # each bounded context: domain/ app/ infra/ transport/
    internal/catalog/
    internal/inventory/
    internal/dispensing/
    internal/fulfillment/ # vending-3d-ctl-agent bridge
    internal/printing/    # print_ops bridge
    internal/platform/    # db, nats, config, logging, audit
    migrations/
  apps/kiosk/             # React+Vite+TS, Connect-Web
  apps/admin/             # React+Vite+TS, Connect-Web
  infra/
    docker-compose.yml    # postgres, nats, core, kiosk, admin, (+ existing agents)
  docs/
    ARCHITECTURE.md       # this file
    MILESTONES.md
    EVENTS.md             # subject/payload registry (grows with implementation)
```

## Key risks (tracked against the 10-day plan)

1. **`rx.prescription.created` schema not agreed** with the external team → blocks real integration. Mitigation: mock feeder from Day 1, schema meeting by Day 2.
2. **Vending hardware protocol** — real dispense flow depends on the cabinet's hex protocol behind `vending-3d-ctl-agent`; the `drugDispenser` route there is currently a stub. Mitigation: fulfillment module targets the raw `serial/vending/write` API; fake adapter for dev; hardware test window reserved Day 8–9.
3. **print_ops production readiness** — its repos are in-memory (data lost on restart). Acceptable for stickers (fire-and-forget with idempotent retry), but confirm deployment plan.
4. **10 days is tight** — scope control: kiosk withdraw + refill, admin CRUD + permissions, one cabinet, one printer. Everything else is post-MVP.
