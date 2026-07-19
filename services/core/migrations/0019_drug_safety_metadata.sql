-- 0019: Drug presentation and safety classification used by Admin and Kiosk.
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS manufacturer TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog.drug ADD COLUMN IF NOT EXISTS safety_classification TEXT NOT NULL DEFAULT 'NORMAL';

ALTER TABLE catalog.drug
  ADD CONSTRAINT catalog_drug_safety_classification_valid
  CHECK (safety_classification IN ('NORMAL', 'LASA', 'HIGH_ALERT'));
