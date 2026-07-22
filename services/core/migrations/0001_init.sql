-- Schema-per-bounded-context. Cross-schema foreign keys are allowed inside
-- the modular monolith; they must be dropped if a context is ever split out.
CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS catalog;
CREATE SCHEMA IF NOT EXISTS inventory;
CREATE SCHEMA IF NOT EXISTS dispensing;
CREATE SCHEMA IF NOT EXISTS audit;

-- Append-only. No UPDATE/DELETE is ever issued against this table.
CREATE TABLE audit.audit_log (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trace_id   text        NOT NULL DEFAULT '',
    actor      text        NOT NULL DEFAULT 'system',
    action     text        NOT NULL,
    entity     text        NOT NULL,
    entity_id  text        NOT NULL DEFAULT '',
    detail     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_entity_idx ON audit.audit_log (entity, entity_id);
CREATE INDEX audit_log_created_at_idx ON audit.audit_log (created_at);

CREATE TABLE dispensing.prescription (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prescription_id text        NOT NULL,
    source_system   text        NOT NULL,
    hn              text        NOT NULL DEFAULT '',
    patient_name    text        NOT NULL DEFAULT '',
    ward_id         text        NOT NULL DEFAULT '',
    items           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    state           text        NOT NULL DEFAULT 'RECEIVED',
    failure_reason  text        NOT NULL DEFAULT '',
    issued_at       timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- Idempotency key: replayed feeder events must not create duplicates.
    CONSTRAINT prescription_external_key UNIQUE (prescription_id, source_system),
    CONSTRAINT prescription_state_check CHECK (
        state IN ('RECEIVED', 'READY', 'DISPENSING', 'DISPENSED', 'FAILED', 'CANCELLED', 'EXPIRED')
    )
);

CREATE INDEX prescription_ward_state_idx ON dispensing.prescription (ward_id, state);
