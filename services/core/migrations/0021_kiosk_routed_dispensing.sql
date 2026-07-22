-- 0021: durable, reportable dispensing transactions routed by immutable kiosk code.
--
-- kiosk.code is the business identity of a physical cabinet. UUID ids remain
-- internal database implementation details and must never be used to route a
-- dispense command.

-- Project codes are four-digit immutable business keys. Existing projects are
-- numbered deterministically by creation order; new projects use a sequence.
ALTER TABLE medisync.projects
    ADD COLUMN IF NOT EXISTS code text,
    ADD COLUMN IF NOT EXISTS next_kiosk_sequence integer NOT NULL DEFAULT 1
        CHECK (next_kiosk_sequence BETWEEN 1 AND 10000);

WITH numbered AS (
    SELECT id, row_number() OVER (ORDER BY created_at, id)::integer AS project_no
    FROM medisync.projects
)
UPDATE medisync.projects p
SET code = lpad(numbered.project_no::text, 4, '0')
FROM numbered
WHERE p.id = numbered.id;

ALTER TABLE medisync.projects ALTER COLUMN code SET NOT NULL;
ALTER TABLE medisync.projects DROP CONSTRAINT IF EXISTS projects_code_unique;
DROP INDEX IF EXISTS medisync.projects_code_unique;
ALTER TABLE medisync.projects ADD CONSTRAINT projects_code_unique UNIQUE (code);
ALTER TABLE medisync.projects ADD CONSTRAINT projects_code_format CHECK (code ~ '^[0-9]{4}$' AND code <> '0000');

CREATE SEQUENCE IF NOT EXISTS medisync.project_code_seq MINVALUE 1 MAXVALUE 9999;
SELECT setval(
    'medisync.project_code_seq',
    GREATEST((SELECT COALESCE(MAX(code::integer), 0) FROM medisync.projects), 1),
    (SELECT COUNT(*) > 0 FROM medisync.projects)
);
ALTER TABLE medisync.projects
    ALTER COLUMN code SET DEFAULT lpad(nextval('medisync.project_code_seq')::text, 4, '0');

CREATE OR REPLACE FUNCTION medisync.prevent_project_code_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.code IS DISTINCT FROM OLD.code THEN
        RAISE EXCEPTION 'project code is immutable (old=%, new=%)', OLD.code, NEW.code
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS projects_code_immutable ON medisync.projects;
CREATE TRIGGER projects_code_immutable
    BEFORE UPDATE OF code ON medisync.projects
    FOR EACH ROW EXECUTE FUNCTION medisync.prevent_project_code_update();

-- Normalize legacy slot values to the kiosk UUID temporarily. Older databases
-- contain a mix of kiosk UUID strings and pre-0021 human codes.
UPDATE medisync.slot s
SET cabinet_id = k.id::text
FROM medisync.kiosks k
WHERE s.cabinet_id = k.code;

ALTER TABLE medisync.kiosks ADD COLUMN IF NOT EXISTS kiosk_sequence integer;
WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY project_id ORDER BY created_at, id)::integer AS kiosk_no
    FROM medisync.kiosks
)
UPDATE medisync.kiosks k
SET kiosk_sequence = numbered.kiosk_no
FROM numbered
WHERE k.id = numbered.id AND k.kiosk_sequence IS NULL;

UPDATE medisync.kiosks k
SET code = p.code || lpad(k.kiosk_sequence::text, 4, '0')
FROM medisync.projects p
WHERE p.id = k.project_id;

UPDATE medisync.projects p
SET next_kiosk_sequence = COALESCE((
    SELECT MAX(k.kiosk_sequence) + 1 FROM medisync.kiosks k WHERE k.project_id = p.id
), 1);

ALTER TABLE medisync.kiosks ALTER COLUMN kiosk_sequence SET NOT NULL;
ALTER TABLE medisync.kiosks
    ADD CONSTRAINT kiosks_sequence_range CHECK (kiosk_sequence BETWEEN 1 AND 9999),
    ADD CONSTRAINT kiosks_project_sequence_unique UNIQUE (project_id, kiosk_sequence),
    ADD CONSTRAINT kiosks_code_format CHECK (code ~ '^[0-9]{8}$');

-- Generates PPPPKKKK under a row lock on the project. Passing an explicit code
-- is reserved for deterministic seed/import jobs and is validated against the
-- selected project; normal application creation always passes an empty code.
CREATE OR REPLACE FUNCTION medisync.assign_kiosk_code()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_project_code text;
    v_sequence integer;
BEGIN
    SELECT code INTO v_project_code
    FROM medisync.projects
    WHERE id = NEW.project_id
    FOR UPDATE;
    IF v_project_code IS NULL THEN
        RAISE EXCEPTION 'project % does not exist', NEW.project_id USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF NEW.code IS NULL OR NEW.code = '' THEN
        UPDATE medisync.projects
        SET next_kiosk_sequence = next_kiosk_sequence + 1
        WHERE id = NEW.project_id AND next_kiosk_sequence <= 9999
        RETURNING next_kiosk_sequence - 1 INTO v_sequence;
        IF v_sequence IS NULL THEN
            RAISE EXCEPTION 'project % has exhausted kiosk codes', v_project_code
                USING ERRCODE = 'program_limit_exceeded';
        END IF;
        NEW.kiosk_sequence := v_sequence;
        NEW.code := v_project_code || lpad(v_sequence::text, 4, '0');
    ELSE
        IF NEW.code !~ '^[0-9]{8}$' OR left(NEW.code, 4) <> v_project_code THEN
            RAISE EXCEPTION 'kiosk code % does not belong to project %', NEW.code, v_project_code
                USING ERRCODE = 'check_violation';
        END IF;
        v_sequence := right(NEW.code, 4)::integer;
        IF v_sequence < 1 THEN
            RAISE EXCEPTION 'kiosk code sequence must be between 0001 and 9999'
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.kiosk_sequence := v_sequence;
        UPDATE medisync.projects
        SET next_kiosk_sequence = GREATEST(next_kiosk_sequence, v_sequence + 1)
        WHERE id = NEW.project_id;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS kiosks_assign_code ON medisync.kiosks;
CREATE TRIGGER kiosks_assign_code
    BEFORE INSERT ON medisync.kiosks
    FOR EACH ROW EXECUTE FUNCTION medisync.assign_kiosk_code();

-- A kiosk code is provisioned once and cannot be renamed. This also protects
-- historical reports and routing configuration from silent reassignment.
CREATE OR REPLACE FUNCTION medisync.prevent_kiosk_code_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.code IS DISTINCT FROM OLD.code OR NEW.kiosk_sequence IS DISTINCT FROM OLD.kiosk_sequence THEN
        RAISE EXCEPTION 'kiosk code is immutable (old=%, new=%)', OLD.code, NEW.code
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS kiosks_code_immutable ON medisync.kiosks;
CREATE TRIGGER kiosks_code_immutable
    BEFORE UPDATE OF code, kiosk_sequence ON medisync.kiosks
    FOR EACH ROW EXECUTE FUNCTION medisync.prevent_kiosk_code_update();

-- slot.cabinet_id is retained for wire compatibility, but from this migration
-- onward its value is the immutable kiosk code, never the kiosk UUID.
UPDATE medisync.slot s
SET cabinet_id = k.code
FROM medisync.kiosks k
WHERE s.cabinet_id = k.id::text;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM medisync.slot s
        LEFT JOIN medisync.kiosks k ON k.code = s.cabinet_id
        WHERE k.code IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot migrate slot cabinet_id: one or more values do not resolve to kiosk.code';
    END IF;
END $$;

ALTER TABLE medisync.slot
    DROP CONSTRAINT IF EXISTS slot_cabinet_id_fkey;
ALTER TABLE medisync.slot
    ADD CONSTRAINT slot_cabinet_code_fkey
    FOREIGN KEY (cabinet_id) REFERENCES medisync.kiosks(code) ON UPDATE RESTRICT;
COMMENT ON COLUMN medisync.slot.cabinet_id IS
    'Immutable kiosk.code routing key; legacy column name retained for API compatibility.';

-- Physical address used by vending-3d-ctl-agent. Admins may remap a slot, but
-- every transaction snapshots these values before it enters the hardware queue.
ALTER TABLE medisync.slot
    ADD COLUMN IF NOT EXISTS door_no integer NOT NULL DEFAULT 1 CHECK (door_no > 0),
    ADD COLUMN IF NOT EXISTS hardware_layer integer NOT NULL DEFAULT 1 CHECK (hardware_layer > 0),
    ADD COLUMN IF NOT EXISTS channel_start integer NOT NULL DEFAULT 1 CHECK (channel_start > 0),
    ADD COLUMN IF NOT EXISTS channel_end integer NOT NULL DEFAULT 1 CHECK (channel_end > 0),
    ADD COLUMN IF NOT EXISTS reserved_quantity integer NOT NULL DEFAULT 0
        CHECK (reserved_quantity >= 0 AND reserved_quantity <= quantity);

ALTER TABLE medisync.slot_batch
    ADD COLUMN IF NOT EXISTS reserved_quantity integer NOT NULL DEFAULT 0
        CHECK (reserved_quantity >= 0 AND reserved_quantity <= quantity);

-- Existing demo/refill data predates batch tracking. Give every stocked slot a
-- real FIFO batch so reservation and completion use one consistent path.
INSERT INTO medisync.slot_batch (slot_id, lot_number, expiry_date, quantity)
SELECT s.id, COALESCE(NULLIF(s.lot_number, ''), 'MIGRATED'), s.expiry_date, s.quantity
FROM medisync.slot s
WHERE s.quantity > 0
  AND NOT EXISTS (SELECT 1 FROM medisync.slot_batch b WHERE b.slot_id = s.id);

CREATE TABLE medisync.dispense_transaction (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prescription_id       uuid NOT NULL REFERENCES medisync.prescription(id),
    prescription_ref      text NOT NULL,
    source_system         text NOT NULL DEFAULT '',
    kiosk_code             text NOT NULL REFERENCES medisync.kiosks(code) ON UPDATE RESTRICT,
    project_id             uuid REFERENCES medisync.projects(id),
    operator_user_id       uuid REFERENCES medisync.users(id),
    operator_display_name  text NOT NULL DEFAULT '',
    status                 text NOT NULL DEFAULT 'AWAITING_IDENTITY',
    trace_id               text NOT NULL,
    failure_code           text NOT NULL DEFAULT '',
    failure_detail         text NOT NULL DEFAULT '',
    hardware_request       jsonb NOT NULL DEFAULT '{}'::jsonb,
    hardware_response      jsonb NOT NULL DEFAULT '{}'::jsonb,
    sticker_scanned_at     timestamptz NOT NULL DEFAULT now(),
    identity_confirmed_at  timestamptz,
    queued_at              timestamptz,
    started_at             timestamptz,
    completed_at           timestamptz,
    failed_at              timestamptz,
    cancelled_at           timestamptz,
    expires_at             timestamptz NOT NULL DEFAULT (now() + interval '5 minutes'),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT dispense_transaction_trace_unique UNIQUE (trace_id),
    CONSTRAINT dispense_transaction_status_check CHECK (status IN (
        'AWAITING_IDENTITY', 'QUEUED', 'DISPENSING',
        'DISPENSED', 'FAILED', 'CANCELLED', 'EXPIRED'
    ))
);

-- At most one live attempt may reserve a prescription at any kiosk.
CREATE UNIQUE INDEX dispense_transaction_active_prescription_idx
    ON medisync.dispense_transaction (prescription_id)
    WHERE status IN ('AWAITING_IDENTITY', 'QUEUED', 'DISPENSING');
CREATE INDEX dispense_transaction_kiosk_created_idx
    ON medisync.dispense_transaction (kiosk_code, created_at DESC);
CREATE INDEX dispense_transaction_status_created_idx
    ON medisync.dispense_transaction (status, created_at DESC);
CREATE INDEX dispense_transaction_operator_created_idx
    ON medisync.dispense_transaction (operator_user_id, created_at DESC);
CREATE INDEX dispense_transaction_prescription_ref_idx
    ON medisync.dispense_transaction (prescription_ref);

CREATE TABLE medisync.dispense_transaction_item (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispense_id        uuid NOT NULL REFERENCES medisync.dispense_transaction(id) ON DELETE CASCADE,
    sequence_no        integer NOT NULL CHECK (sequence_no > 0),
    drug_code          text NOT NULL,
    drug_name          text NOT NULL DEFAULT '',
    requested_quantity integer NOT NULL CHECK (requested_quantity > 0),
    allocated_quantity integer NOT NULL DEFAULT 0 CHECK (allocated_quantity >= 0),
    dispensed_quantity integer NOT NULL DEFAULT 0 CHECK (dispensed_quantity >= 0),
    status             text NOT NULL DEFAULT 'RESERVED'
        CHECK (status IN ('RESERVED', 'DISPENSING', 'DISPENSED', 'FAILED', 'RELEASED')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dispense_transaction_item_sequence_unique UNIQUE (dispense_id, sequence_no)
);

CREATE INDEX dispense_transaction_item_drug_idx
    ON medisync.dispense_transaction_item (drug_code, created_at DESC);

CREATE TABLE medisync.dispense_allocation (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispense_id        uuid NOT NULL REFERENCES medisync.dispense_transaction(id) ON DELETE CASCADE,
    item_id            uuid NOT NULL REFERENCES medisync.dispense_transaction_item(id) ON DELETE CASCADE,
    slot_id            uuid NOT NULL REFERENCES medisync.slot(id),
    slot_code          text NOT NULL,
    batch_id           uuid NOT NULL REFERENCES medisync.slot_batch(id),
    lot_number         text NOT NULL DEFAULT '',
    expiry_date        timestamptz,
    quantity           integer NOT NULL CHECK (quantity > 0),
    dispensed_quantity integer NOT NULL DEFAULT 0 CHECK (dispensed_quantity >= 0),
    door_no            integer NOT NULL CHECK (door_no > 0),
    hardware_layer     integer NOT NULL CHECK (hardware_layer > 0),
    channel_start      integer NOT NULL CHECK (channel_start > 0),
    channel_end        integer NOT NULL CHECK (channel_end > 0),
    status             text NOT NULL DEFAULT 'RESERVED'
        CHECK (status IN ('RESERVED', 'DISPENSING', 'DISPENSED', 'FAILED', 'RELEASED')),
    hardware_attempted_at timestamptz,
    hardware_success  boolean,
    hardware_detail   text NOT NULL DEFAULT '',
    hardware_response jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dispense_allocation_dispense_idx ON medisync.dispense_allocation (dispense_id);
CREATE INDEX dispense_allocation_slot_batch_idx ON medisync.dispense_allocation (slot_id, batch_id);
