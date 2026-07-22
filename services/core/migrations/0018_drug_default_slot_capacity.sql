-- 0018: Per-drug recommended capacity for a single cabinet slot.
-- Existing assigned slots remain unchanged; Admin uses this value as the
-- default when assigning the drug to a new slot.
ALTER TABLE catalog.drug
  ADD COLUMN IF NOT EXISTS default_slot_capacity INTEGER NOT NULL DEFAULT 100;

ALTER TABLE catalog.drug
  ADD CONSTRAINT catalog_drug_default_slot_capacity_positive
  CHECK (default_slot_capacity > 0);
