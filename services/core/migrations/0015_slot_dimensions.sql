-- 0015: Slot dimensions (cm) + capacity calculation + slot groups
-- Enables auto-calculating max drug capacity per slot and multi-slot spanning.

-- Add physical dimensions to slot (in centimeters)
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS width_cm  REAL NOT NULL DEFAULT 0;
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS depth_cm  REAL NOT NULL DEFAULT 0;
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS height_cm REAL NOT NULL DEFAULT 0;

-- Add physical dimensions to drug (per unit, in centimeters)
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS width_cm  REAL NOT NULL DEFAULT 0;
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS depth_cm  REAL NOT NULL DEFAULT 0;
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS height_cm REAL NOT NULL DEFAULT 0;

-- Slot groups for multi-slot spanning (one drug occupying adjacent slots)
CREATE TABLE IF NOT EXISTS inventory.slot_group (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL DEFAULT '',
    cabinet_id UUID NOT NULL REFERENCES cabinet.cabinet(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS inventory.slot_group_member (
    group_id UUID NOT NULL REFERENCES inventory.slot_group(id) ON DELETE CASCADE,
    slot_id  UUID NOT NULL REFERENCES inventory.slot(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,  -- order within group (left-to-right)
    PRIMARY KEY (group_id, slot_id)
);

CREATE INDEX IF NOT EXISTS idx_slot_group_member_slot ON inventory.slot_group_member(slot_id);
