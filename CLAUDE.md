# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What This Is

MediSync — hospital medication dispensing platform. Go modular monolith (`services/core`) + React/TS kiosk and admin apps, proto-first contracts (buf + Connect-RPC), PostgreSQL, NATS JetStream. Reuses sibling repos `../vending-3d-ctl-agent` (cabinet serial control, HTTP API) and `../print_ops` (sticker printing, REST + `X-Api-Key`) — do not reimplement their responsibilities here.

Read before making changes:
- `../CLAUDE_HANDOFF.md` - current cross-agent status, decisions, and next task
- `docs/ARCHITECTURE.md` — bounded contexts, event subjects, repo layout, security rules
- `docs/MILESTONES.md` — current milestone, definition of done, scope boundaries
- `PRODUCT.md` / `DESIGN.md` — for any UI work (kiosk is touch-first, Thai-first; admin follows PrintOps register)

## Hard rules

- **Proto is the source of truth.** Change `proto/`, regenerate, then implement. Never hand-edit generated code.
- **Every state mutation** writes an AuditLog entry, publishes its DomainEvent, and threads `trace_id` through both.
- **Hardware truth over app state.** Dispensing is complete only when `vending-3d-ctl-agent` confirms; a 504/timeout is a FAILED path, never assumed success.
- **Ward-scoped authorization server-side** in every handler. Never trust the client for scoping.
- **Stock changes only through domain events** from dispensing/refill flows — no direct stock edits without audit.
- **Idempotency everywhere events enter:** upsert by `prescription_id + source_system`; print jobs use idempotent `request_id`.
- Config via env, parsed in one place (`internal/platform/config`); never read `process.env`/`os.Getenv` scattered.

## Team lanes

- **codex** — overview: plan ownership, milestone-exit review, architecture drift
- **claude** — advisor: guide hermes, review against ARCHITECTURE.md/DESIGN.md, unblock decisions
- **hermes** — worker: implement milestone tasks; every task lands with tests + verifiable exit criterion
- No lane self-approves its own work; milestone exit = demo + review.
