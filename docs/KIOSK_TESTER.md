# Kiosk flow tester (`cmd/kiosktester`)

Drives the kiosk withdraw/dispense flow end-to-end against a running core —
the same way real hardware does. Dev/testing only.

It uses the **real** contracts, so a pass here is a pass for a real feeder +
cabinet:

- **create** publishes `rx.prescription.created` to NATS (protojson of
  `medisync.events.v1.PrescriptionCreated`) — the exact path the external
  hospital producer uses. Items are pulled from a cabinet's real slots so the
  kiosk cart resolves drug positions/stock. Core stores the prescription
  **READY**, so it is immediately scannable at the kiosk. Prints the
  `prescription_id` — that string is the "sticker" payload.
- **confirm** logs in as staff and calls `DispensingService/Dispense` for a
  `prescription_id` — exactly what the kiosk sends after a QR scan + card
  confirm at the cabinet face — then polls `GetPrescription` until
  `DISPENSED`/`FAILED`.
- **flow** = create then confirm = full E2E like hardware.
- **serve** = a small local web console (no build step): change values in a form
  and click to run create / confirm / flow. The NATS publish + core calls happen
  server-side, since a browser cannot reach NATS directly.

## Web UI (easiest)

```bash
go run ./cmd/kiosktester -mode=serve        # → http://localhost:8899
```

Open the URL. Set cabinet / ward / drugs, then click:
- **โหลดยาในตู้** — list the cabinet's real drugs (click a row to add it)
- **สร้าง sticker** — create a READY prescription, shows the scannable id
- **ยืนยันจ่าย** — Dispense the id shown
- **รัน E2E flow** — create + confirm in one click → DISPENSED

Flags to point elsewhere: `-addr`, `-core`, `-nats`, `-kiosk-code`/`-kiosk-pin`,
`-admin`/`-admin-pass`.

## Prerequisites

Stack running (`docker compose up` from repo root) and demo data seeded:

```
npm run seed:demo          # kiosk DEMO-K1 / PIN 123456, cabinet CAB1, drugs+slots
```

Dev fakes `FULFILLMENT_FAKE=true` / `VENDING_FAKE=true` let confirm reach a
terminal `DISPENSED` without physical hardware.

## Usage

Run from `services/core`:

```bash
# Create a scannable request from cabinet CAB1 (auto-picks stocked drugs)
go run ./cmd/kiosktester -mode=create -cabinet=CAB1 -ward=WARD-3A

# Confirm an existing prescription at the cabinet (full dispense)
go run ./cmd/kiosktester -mode=confirm -id=RX-20260720-140500

# Full end-to-end in one shot
go run ./cmd/kiosktester -mode=flow -cabinet=CAB1

# Explicit items (codes must exist in the cabinet's slots)
go run ./cmd/kiosktester -mode=create -drugs=DEMO-PARA500:2,DEMO-AMOX500:3
```

Key flags: `-core` (default `http://localhost:8080`), `-nats`
(`nats://localhost:4222`), `-kiosk-code`/`-kiosk-pin`, `-admin`/`-admin-pass`,
`-cabinet`, `-ward`, `-hn`, `-patient`, `-source`, `-id`, `-items`, `-timeout`.

## Testing the kiosk UX by scanning

1. `-mode=create` → note the printed `prescription_id`.
2. At the kiosk **เบิกยา** screen (awaiting scan), feed that id. The screen
   reads a fast keystroke burst ending in Enter (keyboard-wedge scanner) — a
   USB barcode/QR scanner reading the id works directly. Manually *typing* it
   will not accumulate (the scan buffer clears every 150 ms between keys).
3. The cart loads → scan a staff card / enter the fallback code → the kiosk
   sends the same `Dispense` this tool's `confirm` mode sends.

To exercise the whole backend flow without the kiosk UI at all, use
`-mode=confirm` or `-mode=flow`.
