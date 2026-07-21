# Event Registry

Wire format: **protojson** of the messages in `proto/medisync/events/v1/events.proto`. Every payload carries `trace_id`; it must be threaded into audit logs and downstream events.

## Streams

| Stream | Subjects | Retention | Notes |
|---|---|---|---|
| `RX` | `rx.prescription.created` | WorkQueue | Inbound prescriptions; one core dispensing consumer group drains it. |
| `RX_RESULTS` | `rx.prescription.dispense_result` | Limits, 30 days | Terminal results; each producer may use an independent durable consumer and filter by payload `source_system`. |
| `MEDISYNC` | `medisync.>` | Limits, 7 days | Internal domain events + DLQ. |

## Subjects

| Subject | Message | Producer | Consumer | Status |
|---|---|---|---|---|
| `rx.prescription.created` | `PrescriptionCreated` | hospital feeder (external team); `cmd/feeder` mock in dev | core/dispensing | **live (M1)** |
| `rx.prescription.dispense_result` | `PrescriptionDispenseResult` | core/dispensing transactional outbox | originating hospital producer | **live (software); external acceptance pending** |
| `medisync.dispense.requested` | `DispenseRequested` | core/dispensing | core/fulfillment | M3 |
| `medisync.dispense.completed` | `DispenseCompleted` | core/fulfillment | core/dispensing | M3 |
| `medisync.dispense.failed` | `DispenseFailed` | core/fulfillment | core/dispensing | M3 |
| `medisync.print.requested` | `PrintRequested` | core/dispensing | core/printing | M3 |
| `medisync.print.completed` | `PrintCompleted` | core/printing | core/dispensing | M3 |
| `medisync.fulfillment.requested` | `FulfillmentRequested` | core/dispensing (future) | core/vending | M3 |
| `medisync.fulfillment.completed` | `FulfillmentCompleted` | core/vending | core/dispensing (future) | M3 |
| `medisync.stock.changed` | `StockChanged` | core/inventory | admin app, audit | M2 |
| `medisync.stock.low` | `StockLow` | core/inventory | admin app | M2 |
| `medisync.dlq.<original>` | raw bytes of the rejected message | any consumer | operators (manual) | **live (M1)** |

## Contract for the external feeder team

- Subject: `rx.prescription.created` on the shared NATS (JetStream enabled)
- Payload: protojson of `medisync.events.v1.PrescriptionCreated` — schema file: `proto/medisync/events/v1/events.proto` (self-contained; only imports `google/protobuf/timestamp.proto`)
- Required fields: `prescription_id`, `source_system`, immutable 4-digit `project_code`, and at least one item with `drug_code` and positive `quantity`
- `project_code` is resolved to an internal foreign key by core; external producers must not send a project UUID or kiosk row ID
- Idempotency: `(prescription_id, source_system)` — replays are safe and ignored; additionally set the `Nats-Msg-Id` header to `<source_system>/<prescription_id>` for broker-level dedupe
- Invalid payloads are terminated to `medisync.dlq.rx.prescription.created` with an audit record — they are **not** retried
- Reference implementation: `services/core/cmd/feeder`

## Result contract for the external producer team

- Subject: `rx.prescription.dispense_result`
- Payload: protojson of `medisync.events.v1.PrescriptionDispenseResult`
- Correlation/routing key: `prescription_id + source_system`; `dispense_id` identifies the MediSync transaction and `kiosk_code` identifies the exact cabinet
- Emission: exactly one transactional-outbox row when a normal Prescription dispense becomes hardware-confirmed `DISPENSED` or `FAILED`
- Item outcomes include requested quantity, actual dispensed quantity, and terminal item status; failures include machine-readable code and diagnostic detail
- Emergency transactions do not emit this event because they have no originating Prescription producer
- Consumers must still be idempotent by `dispense_id` because JetStream delivery is at-least-once
