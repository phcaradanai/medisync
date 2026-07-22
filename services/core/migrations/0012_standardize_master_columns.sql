-- 0012_standardize_master_columns.sql
-- Add standardized id / code / display_name to all master data tables.
-- Non-breaking: existing columns preserved for backward compatibility.

-- ── Drug: add display_name (copy from name) ──────────────────────

ALTER TABLE catalog.drug
    ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';

UPDATE catalog.drug SET display_name = name WHERE display_name = '';

-- ── Slot: add display_name ────────────────────────────────────────

ALTER TABLE inventory.slot
    ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';

UPDATE inventory.slot SET display_name = drug_name WHERE display_name = '';

-- ── Cabinet: add display_name (copy from name) ────────────────────

ALTER TABLE cabinet.cabinet
    ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';

UPDATE cabinet.cabinet SET display_name = name WHERE display_name = '';

-- ── Project: add code (copy from slug) + display_name (copy from name)

ALTER TABLE identity.projects
    ADD COLUMN IF NOT EXISTS code text NOT NULL DEFAULT '';

ALTER TABLE identity.projects
    ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';

UPDATE identity.projects
   SET code = slug,
       display_name = name
 WHERE code = '';

CREATE UNIQUE INDEX IF NOT EXISTS projects_code_unique ON identity.projects (code);
