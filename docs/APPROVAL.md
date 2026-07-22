# Schema Adoption Decision - rx.prescription.created

**Status: ACCEPTED_INTERNAL**

M1 may proceed with the current `rx.prescription.created` schema as the canonical v1 contract. This is a project-owner decision, not external-team approval.

## Decision Record

- Decision date: 2026-07-13
- Authority: project owner, recorded through the coordinator session
- M1 impact: schema gate accepted; M1 may be marked complete
- Integration direction: the prescription producer team will implement the current v1 contract
- Compatibility rule: prefer additive changes; any breaking change requires a new event/schema version and migration plan
- External confirmation: not yet recorded and no external approval evidence is claimed

## Schema Under Review

Subject: `rx.prescription.created`
Wire format: protojson of `medisync.events.v1.PrescriptionCreated`
Schema file: `proto/medisync/events/v1/events.proto` (self-contained; only imports `google/protobuf/timestamp.proto`)

## Sample Payload

```json
{
  "prescriptionId": "RX-20260713-001",
  "sourceSystem": "mock-his",
  "hn": "HN100000",
  "patientName": "Test Patient 01",
  "wardId": "WARD-3A",
  "items": [
    {
      "drugCode": "PARA500",
      "drugName": "Paracetamol 500 mg",
      "quantity": 10,
      "dosageText": "รับประทานครั้งละ 1 เม็ด ทุก 6 ชั่วโมง เวลาปวดหรือมีไข้"
    }
  ],
  "issuedAt": "2026-07-13T10:00:00Z",
  "traceId": "feeder-RX-20260713-001"
}
```

## Required Fields

| Field | Type | Required | Notes |
|---|---|---|---|
| `prescription_id` | string | Yes | Idempotency key (with `source_system`) |
| `source_system` | string | Yes | Idempotency key (with `prescription_id`) |
| `items` | repeated PrescriptionItem | Yes | At least one item required |
| `items[].drug_code` | string | Yes | Per-item required |
| `items[].quantity` | int32 | Yes | Must be positive (> 0) |
| `ward_id` | string | Recommended | Ward for routing/scoping |
| `hn` | string | Optional | Hospital number |
| `patient_name` | string | Optional | Patient display name |
| `items[].drug_name` | string | Optional | Human-readable drug name |
| `items[].dosage_text` | string | Optional | Dosage instructions |
| `issued_at` | google.protobuf.Timestamp | Optional | When prescription was issued |
| `trace_id` | string | Optional | For distributed tracing |

## Idempotency Contract

- Per-message: `Nats-Msg-Id` header set to `<source_system>/<prescription_id>` for broker-level dedupe
- Per-store: `(prescription_id, source_system)` composite key, upsert-safe
- Invalid payloads are terminated (not retried) to `medisync.dlq.rx.prescription.created`

## Follow-up Checklist

- [ ] Give the producer team `events.proto`, sample payload, subject, and idempotency rules
- [ ] Record producer-team contact and implementation date
- [ ] Run a contract test with a real producer payload before production
- [ ] Record any requested changes and classify them as additive or breaking

## Reference Implementation

`services/core/cmd/feeder/main.go` — mock feeder that publishes events in the exact wire format.
