# MediSync — Goals & Milestones (10-day plan)

**Goal:** end-to-end hospital medication dispensing — prescription arrives via NATS JetStream, staff withdraws drugs at the kiosk, cabinet dispenses, sticker prints, stock and audit stay correct — running from one docker compose, in 10 days.

**Definition of done (MVP):**
1. Mock (then real) `rx.prescription.created` event → prescription visible on kiosk
2. Nurse authenticates (login; QR/NFC if hardware window allows), confirms withdraw → cabinet dispenses via `vending-3d-ctl-agent` → sticker prints via `print_ops` → stock decremented → audit row written
3. Refill staff restocks via kiosk refill mode; counts update with audit
4. Admin manages drugs, slot mapping, users, ward permissions
5. `docker compose up` starts postgres, NATS, core, kiosk, admin
6. Ward-scoped authorization enforced server-side; all state changes audited

## Milestones

### M1 — Foundations (Day 1–2)
- [x] Repo scaffolding: Go module, buf setup, Vite apps, docker compose (postgres + NATS JetStream)
- [x] Proto contracts v1 for identity, catalog, inventory, dispensing + event payloads
- [x] `platform` package: config, logging, DB pool, NATS connection, audit writer, migrations runner
- [x] Mock prescription feeder (publishes `rx.prescription.created`)
- [x] Prescription schema adoption decision recorded; current v1 contract is canonical (see `docs/APPROVAL.md`)
- Exit: `docker compose up` green; proto generates Go + TS; mock event lands in a consumer log

**M1 status: COMPLETE (accepted 2026-07-13).**

**M1 verification (2026-07-13, corrected by Team 3):**
- `buf lint` — PASS
- `buf generate` — PASS (Go + TS)
- M1-scoped Go unit tests — PASS (30 tests: 9 dispensing + 9 audit + 7 feeder + 5 config; no PostgreSQL or NATS required)
- Repository-wide `go test ./...` — PASS after Team 4 Identity foundation repair (`t_3e11bdeb`)
- `go test -tags=integration -count=1 ./...` — PASS (10 integration tests: 5 store + 5 audit; transaction-rolled-back, no data pollution)
- `npm run build` — PASS (kiosk + admin)
- `docker compose up -d --wait` — PASS (postgres + NATS healthy)
- Smoke test: core consumes `rx.prescription.created` → stores in PostgreSQL (READY) → writes audit. Duplicate event deduplicated (count=1). Malformed event rejected → DLQ + audit record.
- **Test design corrected:** Store and audit.Writer accept a narrow `testutil.Execer` interface; unit tests inject `FakeExecer`; integration tests use `//go:build integration` with tx rollback. No `t.Skip` anywhere — database-dependent tests fail clearly (not silently) when infrastructure is missing.
- **Decision:** Project owner accepted the current event schema as canonical v1. Producer-team confirmation remains integration follow-up, not an M1 blocker.

### M2 — Core domain (Day 2–4) ✅ COMPLETE (2026-07-14)
- [x] identity: users, roles, ward permissions, login (JWT), seed admin, hashed card-token storage (Review R2 PASS)
- [x] catalog: drug CRUD, pg_trgm search (Team 10 PASS WITH FINDINGS)
- [x] inventory: slots, drug↔slot mapping, stock counts, refill command, `stock.changed`/`stock.low` events (Team 11 PASS WITH FINDINGS)
- [x] dispensing: prescription aggregate + state machine, JetStream consumer with idempotent upsert, outbox publisher, ward-scoped auth (Team 12 PASS WITH FINDINGS — 294 tests, 49 state edges, zero races)
- Exit: prescription event → `READY` prescription queryable via Connect-RPC; unit tests on state machine + authorization

### M3 — Hardware & printing bridges (Day 4–5)
- [x] Initial vending-3d-ctl-agent client/consumer and fake mode implemented
- [x] Initial print_ops client/consumer and completion handling implemented
- [ ] Project/cabinet-scoped happy path verified: `READY → DISPENSING → DISPENSED | FAILED`, stock, sticker, and audit (`t_68fb1a06`)
- Exit: one command demo — publish mock event, call dispense API, watch state machine complete with fake HW + real print_ops (fake printer adapter)

### M4 — Kiosk app (Day 5–7)
- [x] Code/PIN authentication, session restore, expiry, and logout foundation
- [x] Initial withdraw flow: prescription list → confirm → status polling → done/failed acknowledgment
- [x] Initial refill mode: slot list, count entry, confirm
- [ ] Per DESIGN.md: kiosk scale tokens, ≥48px targets, Thai-first copy
- Exit: complete withdraw + refill on touch-sized viewport against dev backend

### M5 — Admin app (Day 6–8, overlaps M4)
- [x] Login and application shell foundation
- [x] Initial users, kiosks, drugs, and inventory pages
- [ ] Project-scoped projects, users/permissions, inventory, and audit acceptance (`t_07f326b9`, `t_3101e0c5`, `t_fe376f28`)
- Exit: admin can fully provision a new drug into a slot and see it dispensable on kiosk

### M6 — Hardware integration & hardening (Day 8–9)
- [ ] Real cabinet test window: real serial dispense via vending-3d-ctl-agent, tune timeouts, jam/failure handling
- [ ] Real sticker printer through print_ops
- [ ] QR/NFC auth via the agent's MQTT stream (if card format available; else post-MVP)
- [ ] Failure drills: NATS down, agent down, printer down — system degrades loudly, never silently
- Exit: end-to-end on real hardware, failure modes acknowledged on kiosk

### M7 — Ship (Day 10)
- [ ] Compose production profile (restart policies, volumes, reverse proxy/TLS if crossing machines)
- [ ] Seed/ops runbook, EVENTS.md payload registry finalized
- [ ] Final pass: `/impeccable audit` kiosk + admin (contrast, touch targets, reduced motion)
- Exit: fresh-machine deploy from README in <30 min

## Connected Application Kanban (2026-07-16)

Execution order: security foundation, parallel Admin/Kiosk/backend lanes, operational workflows, then independent acceptance. Every card uses an isolated worktree and has test plus live-browser exit criteria.

1. `t_8ae8ba59` — project scope and permission foundation
2. `t_07f326b9`, `t_3101e0c5`, `t_53f8c8ef`, `t_68fb1a06` — parallel application lanes
3. `t_f9aed710`, `t_f7b11503`, `t_fe376f28` — refill, withdrawal, and operations workflows
4. `t_a19913c6` — fresh Compose, isolation, fake hardware, printing, and browser release gate

## Working agreement (codex / claude / hermes)

- **codex** — overview lane: owns this plan, reviews milestone exits, catches architectural drift
- **claude** — advisor lane: guides hermes, reviews designs/PRs against ARCHITECTURE.md + DESIGN.md, unblocks decisions
- **hermes** — worker lane: implements tasks cut from milestones; each task lands with tests + a verifiable exit criterion
- Cadence: milestone exit = demo + review before the next lane opens; no lane self-approves its own work

## Out of scope (post-MVP)
- Multi-cabinet fleet management, HIS write-back, patient-facing self-service, dark mode, offline kiosk mode, analytics dashboards
