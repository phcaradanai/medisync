# Canonical End-to-End Flow

Updated 2026-07-21. This is the canonical prescription withdrawal flow.

## Normal prescription withdrawal

1. An external producer publishes `rx.prescription.created` with `prescription_id`, `source_system`, immutable 4-digit `project_code`, patient context, and medication items.
2. Core validates and stores the Prescription idempotently by `(prescription_id, source_system)`.
3. A physical Sticker carrying the Prescription identifier is printed and brought to a cabinet.
4. The Kiosk authenticates as one immutable 8-digit `kiosk_code` and checks the vending agent mapped to that exact code.
5. The user scans the Sticker at the cabinet. `PrepareDispense` resolves the Prescription within the kiosk project, reserves only stock belonging to that kiosk, and creates an `AWAITING_IDENTITY` dispense transaction.
6. The user reviews the medication cart and authenticates with a staff card. `ConfirmDispense` verifies both staff and kiosk JWTs, project and ward scope, and the current hardware health before moving the same transaction to `QUEUED`.
7. The outbox publishes a request containing `dispense_id`, `kiosk_code`, and allocation-level hardware addresses. The router selects one vending agent from `kiosk_code`; it never broadcasts to every cabinet.
8. Hardware outcomes move the transaction to `DISPENSED` or `FAILED`. Core consumes successful allocations, releases failed reservations, updates item/allocation details, stock, audit, and print events.
9. In the same database transaction as terminal state, Core writes `rx.prescription.dispense_result` to the outbox. The originating producer correlates it by `prescription_id + source_system`.

## Emergency withdrawal

Emergency withdrawal is deliberately separate. It is used when no Prescription exists and therefore no Sticker exists.

1. Admin explicitly enables a drug in a physical slot using `kiosk_code + slot_code` and sets the maximum quantity per emergency transaction.
2. At that kiosk, the operator enters HN and authenticates by staff card or employee code.
3. Core validates the operator project, selected configured slot, available stock, maximum quantity, and this kiosk's hardware health.
4. Core creates an `emergency_dispense_transaction`, queues only the selected kiosk agent, and records allocation/hardware outcomes.
5. Emergency results appear in slot history and the separate Emergency report/CSV. They do not create a Prescription and do not emit a producer Prescription result.

## Identity and routing rule

- Project code: immutable 4 digits (`0001`, `0002`, ...).
- Kiosk code: immutable `PPPPKKKK` (`00010001`, `00010002`, `00020001`, ...).
- Database UUIDs remain internal implementation identities. External messages, hardware routing, recovery scope, and operator-visible reports use business codes.

## Acceptance boundary

The software path and fake-agent contract are implemented and tested. Real camera, vending hardware, printer, and producer-consumer acceptance must be recorded separately; they are not implied by unit tests or fake mode.
