-- 0022: durable emergency dispensing, scoped by immutable kiosk code.
--
-- Emergency withdrawals are for cases where no prescription (and therefore no
-- prescription sticker) exists. They remain separate from prescription dispense
-- transactions and share only the hardware fulfillment pipeline.

ALTER TABLE medisync.users
    ADD COLUMN IF NOT EXISTS employee_code text;

WITH ranked AS (
    SELECT id,
           upper(username) AS base_code,
           row_number() OVER (PARTITION BY project_id, upper(username) ORDER BY id) AS duplicate_no
    FROM medisync.users
    WHERE employee_code IS NULL OR btrim(employee_code) = ''
)
UPDATE medisync.users u
SET employee_code = ranked.base_code ||
    CASE WHEN ranked.duplicate_no = 1 THEN '' ELSE '-' || left(u.id::text, 8) END
FROM ranked
WHERE u.id = ranked.id;

CREATE UNIQUE INDEX IF NOT EXISTS users_project_employee_code_unique
    ON medisync.users (project_id, upper(employee_code))
    WHERE project_id IS NOT NULL AND employee_code IS NOT NULL;

ALTER TABLE medisync.slot
    ADD COLUMN IF NOT EXISTS emergency_max_quantity integer NOT NULL DEFAULT 1
        CHECK (emergency_max_quantity > 0);

CREATE TABLE medisync.emergency_dispense_transaction (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kiosk_code            text NOT NULL REFERENCES medisync.kiosks(code) ON UPDATE RESTRICT,
    project_id            uuid NOT NULL REFERENCES medisync.projects(id),
    hn                    text NOT NULL,
    employee_code         text NOT NULL,
    operator_user_id      uuid NOT NULL REFERENCES medisync.users(id),
    operator_display_name text NOT NULL,
    slot_code             text NOT NULL,
    drug_code             text NOT NULL,
    drug_name             text NOT NULL DEFAULT '',
    requested_quantity    integer NOT NULL CHECK (requested_quantity > 0),
    dispensed_quantity    integer NOT NULL DEFAULT 0 CHECK (dispensed_quantity >= 0),
    reason                text NOT NULL DEFAULT '',
    status                text NOT NULL DEFAULT 'QUEUED'
        CHECK (status IN ('QUEUED', 'DISPENSING', 'DISPENSED', 'FAILED')),
    trace_id              text NOT NULL UNIQUE,
    failure_code          text NOT NULL DEFAULT '',
    failure_detail        text NOT NULL DEFAULT '',
    hardware_request      jsonb NOT NULL DEFAULT '{}'::jsonb,
    hardware_response     jsonb NOT NULL DEFAULT '{}'::jsonb,
    queued_at             timestamptz NOT NULL DEFAULT now(),
    started_at            timestamptz,
    completed_at          timestamptz,
    failed_at             timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX emergency_dispense_kiosk_created_idx
    ON medisync.emergency_dispense_transaction (kiosk_code, created_at DESC);
CREATE INDEX emergency_dispense_project_created_idx
    ON medisync.emergency_dispense_transaction (project_id, created_at DESC);
CREATE INDEX emergency_dispense_employee_created_idx
    ON medisync.emergency_dispense_transaction (employee_code, created_at DESC);
CREATE INDEX emergency_dispense_hn_created_idx
    ON medisync.emergency_dispense_transaction (hn, created_at DESC);
CREATE INDEX emergency_dispense_drug_created_idx
    ON medisync.emergency_dispense_transaction (drug_code, created_at DESC);

CREATE TABLE medisync.emergency_dispense_allocation (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    emergency_dispense_id uuid NOT NULL REFERENCES medisync.emergency_dispense_transaction(id) ON DELETE CASCADE,
    slot_id               uuid NOT NULL REFERENCES medisync.slot(id),
    slot_code             text NOT NULL,
    batch_id              uuid NOT NULL REFERENCES medisync.slot_batch(id),
    lot_number            text NOT NULL DEFAULT '',
    expiry_date           timestamptz,
    quantity              integer NOT NULL CHECK (quantity > 0),
    dispensed_quantity    integer NOT NULL DEFAULT 0 CHECK (dispensed_quantity >= 0),
    door_no               integer NOT NULL CHECK (door_no > 0),
    hardware_layer        integer NOT NULL CHECK (hardware_layer > 0),
    channel_start         integer NOT NULL CHECK (channel_start > 0),
    channel_end           integer NOT NULL CHECK (channel_end > 0),
    status                text NOT NULL DEFAULT 'RESERVED'
        CHECK (status IN ('RESERVED', 'DISPENSING', 'DISPENSED', 'FAILED', 'RELEASED')),
    hardware_attempted_at timestamptz,
    hardware_success      boolean,
    hardware_detail       text NOT NULL DEFAULT '',
    hardware_response     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX emergency_dispense_allocation_tx_idx
    ON medisync.emergency_dispense_allocation (emergency_dispense_id);
CREATE INDEX emergency_dispense_allocation_stock_idx
    ON medisync.emergency_dispense_allocation (slot_id, batch_id);
