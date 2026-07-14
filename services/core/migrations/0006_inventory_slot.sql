-- inventory.slot: cabinet slot inventory for the inventory bounded context.
-- Schema inventory.* is created by 0001_init.sql; this migration adds
-- the slot table with concurrency-safe stock operations.

CREATE TABLE inventory.slot (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    cabinet_id    text        NOT NULL,
    code          text        NOT NULL,
    drug_id       text        NOT NULL DEFAULT '',
    drug_code     text        NOT NULL DEFAULT '',
    drug_name     text        NOT NULL DEFAULT '',
    capacity      int         NOT NULL DEFAULT 0 CHECK (capacity >= 0),
    quantity      int         NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    low_threshold int         NOT NULL DEFAULT 0 CHECK (low_threshold >= 0),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT slot_cabinet_code_unique UNIQUE (cabinet_id, code)
);

CREATE INDEX slot_cabinet_id_idx ON inventory.slot (cabinet_id);
CREATE INDEX slot_low_threshold_idx ON inventory.slot (low_threshold) WHERE quantity <= low_threshold;
