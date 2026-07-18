-- 0014: Slot batch tracking for FIFO dispense by expiry
-- Each refill creates a batch; dispensing consumes oldest first.

CREATE TABLE IF NOT EXISTS inventory.slot_batch (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slot_id     UUID NOT NULL REFERENCES inventory.slot(id) ON DELETE CASCADE,
    lot_number  TEXT NOT NULL DEFAULT '',
    expiry_date TIMESTAMPTZ,
    quantity    INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_slot_batch_slot ON inventory.slot_batch(slot_id);
CREATE INDEX IF NOT EXISTS idx_slot_batch_expiry ON inventory.slot_batch(expiry_date ASC);

-- Add helper columns to slot
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS lot_number TEXT NOT NULL DEFAULT '';
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS drug_type TEXT NOT NULL DEFAULT '';
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS lasa_group TEXT NOT NULL DEFAULT '';
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS high_alert BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS image_url TEXT NOT NULL DEFAULT '';
