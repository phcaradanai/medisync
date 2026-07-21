# Project Status

**Last updated:** 2026-07-21

## Current position

The kiosk withdrawal surface is software-complete for the agreed MVP flow. The next product surface may be the refill flow, but physical camera/scanner, cabinet, and printer acceptance remain deployment gates rather than completed software claims.

## Withdrawal flow delivered

- Kiosk identity is the immutable 8-digit business code `PPPPKKKK` (`00010001`, `00010002`, `00020001`, ...). Prescription lookup, stock reservation, tester delivery, transaction, hardware routing, history, and reports are scoped by that code.
- The normal flow is Sticker scan → reserve stock in this kiosk → review cart → staff card authentication → queue one hardware route → hardware-confirmed `DISPENSED` or `FAILED`.
- Recovery uses `dispense_id`, and the browser recovery registry is isolated by kiosk code. Terminal rows never create a phantom queue count.
- Core checks the vending agent for the authenticated kiosk before prepare, confirm, or emergency dispense. The Kiosk UI polls and displays that server-side result separately from browser connectivity.
- Every normal dispense is stored in `dispense_transaction`, item, and allocation tables with operator, timestamps, slot/lot address, requested/dispensed quantity, hardware result, and failure detail.
- Core writes `rx.prescription.dispense_result` to the transactional outbox on hardware-confirmed success or failure. The payload carries `prescription_id + source_system`, the immutable kiosk code, item outcomes, trace ID, and failure data for the originating producer.
- Emergency withdrawal is a separate transaction type for cases with no Prescription/Sticker. It requires HN plus staff identity by card or employee code, only exposes drugs configured for that kiosk, and records a full hardware-backed audit trail.
- Admin can persist emergency eligibility and per-transaction maximum by `kiosk_code + slot_code`. Admin reports have separate Prescription and Emergency views and CSV exports.
- Slot detail history merges the latest normal and emergency outcomes for the selected medicine in the current kiosk, using status icons and a fixed seven-row layout.
- Kiosk dialogs support Escape, contained keyboard focus, and focus return. The live clock is isolated so the full cabinet grid is not rerendered every second.

## Verification status

| Check | Result |
|---|---|
| `npm run proto` | PASS |
| Core `go test ./...` | PASS |
| Kiosk Vitest | PASS — 44/44 |
| Admin Vitest | PASS — 13/13 |
| Kiosk production build | PASS |
| Admin production build | PASS |
| Tagged dispensing + inventory + audit integration | PASS |
| Parent Docker Compose rebuild + smoke | PASS — Core healthy, Kiosk/Admin 200, kiosk `00010001` agent READY, `RX_RESULTS` created |

## Remaining release gates

These items do not block starting work on the refill page, but they do block claiming physical production acceptance:

1. Hardware team connects the real camera/QR scanner and validates its output as a keyboard-wedge or the agreed adapter contract.
2. Run the same flow against the real `vending-3d-ctl-agent`, including agent unavailable, timeout, jam, partial failure, and recovery drills.
3. Validate the real printer path and confirm whether sticker printing occurs upstream before arrival at the kiosk or through MediSync for the deployment.
4. External producer team subscribes to `rx.prescription.dispense_result` and accepts the v1 result payload.
5. Run a touch-device/browser acceptance pass on the installed kiosk hardware. Automated browser control was unavailable in the current tool session, so builds/tests are not a substitute for this physical check.

## Next implementation scope

Proceed with the kiosk refill page. Preserve the same rules: immutable kiosk code, server-authoritative hardware state, auditable transactions, no cross-kiosk commands, and no UI-only persistence.
