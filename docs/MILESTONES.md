# MediSync Milestones

**Updated:** 2026-07-21

## Definition of done

1. A project-scoped Prescription arrives through JetStream and is idempotently stored.
2. A Sticker scan at one authenticated kiosk creates a tracked transaction and reserves stock only in that kiosk.
3. Staff authentication queues one hardware route; hardware truth determines success/failure and stock consumption.
4. Normal and emergency dispensing are separately auditable and reportable.
5. The external producer receives the terminal normal-dispense result.
6. Admin configuration is persisted by immutable business codes, not UI-only state.
7. Compose starts the complete development stack and automated tests/builds pass.
8. Physical camera, cabinet, and printer acceptance is recorded before production sign-off.

## Status

| Milestone | Scope | Status |
|---|---|---|
| M1 | Repository, proto, PostgreSQL, NATS, audit, feeder | Complete |
| M2 | Identity, project scope, catalog, inventory, dispensing domain | Complete |
| M3 | Kiosk-code hardware routing, completion handling, stock and result outbox | Software complete; physical acceptance pending |
| M4A | Kiosk withdrawal UI, independent tester bridge, recovery, queue, slot detail | Complete |
| M4B | Emergency withdrawal UI and transaction flow | Complete |
| M4C | Kiosk refill UI | Next scope |
| M5 | Admin CRUD, emergency configuration, normal/emergency reports | Complete for withdrawal scope |
| M6 | Real camera, cabinet and printer integration/failure drills | Pending hardware window |
| M7 | Production deployment and installed-device acceptance | Pending |

## Exit evidence for withdrawal scope

- Proto lint/generation, Go unit suite, Kiosk 43-test suite, Admin tests, and both frontend production builds.
- Tagged dispensing integration includes the full vending pipeline and validates the outbound Prescription result outbox; inventory integration now runs against the unified `medisync` schema with project/kiosk-scoped fixtures and stable cursor coverage.
- Kiosk recovery and tester event delivery are isolated by immutable kiosk code.
- Hardware readiness comes from the mapped vending agent, not the existence of browser data.

## Next milestone: refill

The refill implementation must reuse the same cabinet identity and audit rules. It must model lot/expiry input, physical slot selection, before/after quantities, staff identity, failure/retry behavior, and reportable refill transactions. It must not mutate stock solely in browser state.
