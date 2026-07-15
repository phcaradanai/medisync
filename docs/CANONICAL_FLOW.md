# Canonical End-to-End Flow

Owner-stated 2026-07-14. This is the **one true flow** MediSync exists to serve. All M3+ work must implement exactly these steps. Do not add features or flows outside this without owner approval.

## The flow

1. **External producer program** (another team) sends prescription data to our backend over **NATS JetStream** (`rx.prescription.created`). Backend consumes, validates, records, and stores the prescription (**live, M1**).
2. Backend requests a sticker from **print_ops**; the printer prints a physical **sticker carrying a QR code** (M3, `medisync.print.requested` → printing module → print_ops HTTP `POST /api/v1/print-jobs`).
3. Staff carries the sticker to the **cabinet** and **scans the QR at the cabinet face** — this scan is the real-world dispense trigger.
4. Cabinet dispenses the drug via **vending-3d-ctl-agent** (M3, fulfillment module). Backend records the outcome: **dispense succeeded or failed** (state machine DISPENSING → DISPENSED | FAILED, driven by hardware truth).
5. Backend **sends the result back to the originating producer program over NATS JetStream** (reverse/result channel). This closes the loop so the producer knows the prescription was fulfilled or failed.
6. Flow ends. Everything (intake, print, dispense, result) is audited and logged.

## Where each step stands

| Step | What | Status |
|---|---|---|
| 1 | Producer → backend intake (`rx.prescription.created`) | **live (M1)** |
| — | catalog / inventory / dispensing domain + APIs | **done (M2)** |
| — | `medisync.dispense.requested` outbox event on withdraw | **done (M2, Team 12)** — fires, nothing consumes it yet |
| 2 | Print sticker via print_ops (`medisync.print.requested` → printing module) | **M3 — not built** |
| 3 | QR-scan-at-cabinet as dispense trigger | **M3 — not built** (kiosk M4 withdraw UI is the operator surface; QR scan is the production entry point) |
| 4 | Cabinet dispense via vending-3d-ctl-agent (fulfillment module) + record success/fail | **M3 — not built** |
| 5 | Result back to producer over JetStream (reverse channel) | **M3 — NOT YET DESIGNED**, see gap below |
| 6 | Full audit/log | partial — each context audits its own mutations (M2); end-to-end trace not yet stitched |

## Gaps vs current design docs (must resolve during M3 planning)

- **Reverse result channel (step 5) is undefined.** `docs/EVENTS.md` lists `dispense.completed`/`dispense.failed` only as *internal* subjects (fulfillment → dispensing). There is **no** outbound subject that sends the final result back to the external producer. M3 must add this event (subject + `events.proto` message) and document it in `EVENTS.md`.
- **QR-scan trigger (step 3) is unmodeled.** The dispensing `Dispense` RPC exists (kiosk button path) but there is no QR-scan intake. M3 must decide how the QR maps to a prescription/dispense command.
- **No cross-context end-to-end integration test** exists. Every bounded context is unit + integration tested in isolation, but nothing exercises the full chain. Recommended before wiring real hardware clients, so cross-context regressions are caught.

## Boundary rule

Do not build features outside these 6 steps. Anything that isn't intake → store → print → QR-dispense → result-back → log is out of scope until the owner says otherwise.
