-- 0013_drug_barcode_slot_expiry.sql
-- Adds barcode to catalog.drug and expiry_date to inventory.slot.

ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS barcode TEXT NOT NULL DEFAULT '';

ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS expiry_date TIMESTAMPTZ;
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS shelf INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS row_num INTEGER NOT NULL DEFAULT 0;
