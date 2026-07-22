-- 0020: Unify all bounded-context schemas into a single `medisync` schema.
--
-- Background: the original schema-per-context design (identity, catalog,
-- inventory, dispensing, audit, cabinet) caused data duplication and
-- confusion — especially the parallel identity.kiosks (auth) vs
-- cabinet.cabinet (physical registry) which represented the same real-world
-- machine with no link between them.
--
-- This migration:
--   1. Creates a single `medisync` schema (idempotent).
--   2. Moves every table from legacy schemas into medisync via ALTER TABLE
--      SET SCHEMA. Each statement is guarded by a DO block so the migration
--      is idempotent — if a table was already moved (e.g. medisync.kiosks
--      from a prior partial run), the statement is skipped.
--   3. Merges cabinet.cabinet rows into medisync.kiosks (adding a `name`
--      column that only cabinet had).
--   4. Redirects the slot_group FK from cabinet.cabinet to medisync.kiosks.
--   5. Drops the now-empty legacy schemas.

-- ── Step 1: Create unified schema ──────────────────────────────────
CREATE SCHEMA IF NOT EXISTS medisync;

-- pgcrypto provides crypt()/gen_salt() for hashing cabinet-origin PINs.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ── Step 2: Move every legacy table into medisync (idempotent) ──────
-- ALTER TABLE SET SCHEMA errors if the source table doesn't exist, so we
-- guard each one with a check against information_schema. PostgreSQL
-- automatically updates every FK that references a moved table.

DO $$
BEGIN
    -- identity.kiosks → medisync.kiosks (may already be moved from prior run)
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='identity' AND table_name='kiosks') THEN
        ALTER TABLE identity.kiosks SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='identity' AND table_name='users') THEN
        ALTER TABLE identity.users SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='identity' AND table_name='projects') THEN
        ALTER TABLE identity.projects SET SCHEMA medisync;
    END IF;
    -- catalog
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='catalog' AND table_name='drug') THEN
        ALTER TABLE catalog.drug SET SCHEMA medisync;
    END IF;
    -- inventory (slot checked before slot_batch/group to preserve FK ordering)
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='inventory' AND table_name='slot') THEN
        ALTER TABLE inventory.slot SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='inventory' AND table_name='slot_batch') THEN
        ALTER TABLE inventory.slot_batch SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='inventory' AND table_name='slot_group') THEN
        ALTER TABLE inventory.slot_group SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='inventory' AND table_name='slot_group_member') THEN
        ALTER TABLE inventory.slot_group_member SET SCHEMA medisync;
    END IF;
    -- dispensing
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='dispensing' AND table_name='prescription') THEN
        ALTER TABLE dispensing.prescription SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='dispensing' AND table_name='outbox') THEN
        ALTER TABLE dispensing.outbox SET SCHEMA medisync;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='dispensing' AND table_name='emergency_log') THEN
        ALTER TABLE dispensing.emergency_log SET SCHEMA medisync;
    END IF;
    -- audit
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='audit' AND table_name='audit_log') THEN
        ALTER TABLE audit.audit_log SET SCHEMA medisync;
    END IF;
END $$;

-- ── Step 3: Merge cabinet.cabinet into medisync.kiosks ─────────────
-- cabinet.cabinet had a `name` column that kiosks lacked; add it first.
ALTER TABLE medisync.kiosks ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';

-- Drop the old FK from slot_group → cabinet.cabinet before we drop cabinet.
-- The FK may already have been moved to reference medisync.kiosks, so guard.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'slot_group_cabinet_id_fkey'
          AND conrelid = 'medisync.slot_group'::regclass
    ) THEN
        ALTER TABLE medisync.slot_group DROP CONSTRAINT slot_group_cabinet_id_fkey;
    END IF;
END $$;

-- Insert cabinet rows that don't already exist in kiosks (matched by code).
-- Cabinet-origin rows get a random unknown PIN hash so they can never be
-- logged into directly — they are physical machines, not auth terminals.
-- Guard: only run if cabinet.cabinet still exists (idempotent).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='cabinet' AND table_name='cabinet') THEN
        INSERT INTO medisync.kiosks (code, display_name, name, pin_hash, active, project_id, created_at, updated_at)
        SELECT
            c.code,
            COALESCE(NULLIF(c.display_name, ''), c.name),
            c.name,
            -- bcrypt hash of a per-row random UUID — valid format, unknowable value.
            crypt(gen_random_uuid()::text, gen_salt('bf')),
            c.active,
            c.project_id,
            c.created_at,
            c.updated_at
        FROM cabinet.cabinet c
        WHERE NOT EXISTS (
            SELECT 1 FROM medisync.kiosks k WHERE k.code = c.code
        );
    END IF;
END $$;

-- For cabinet rows whose code DOES match an existing kiosk, backfill name.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='cabinet' AND table_name='cabinet') THEN
        UPDATE medisync.kiosks k
        SET name = c.name
        FROM cabinet.cabinet c
        WHERE k.code = c.code AND k.name = '' AND c.name <> '';
    END IF;
END $$;

-- ── Step 4: Re-point slot_group FK to medisync.kiosks ─────────────
-- slot_group.cabinet_id is UUID; medisync.kiosks.id is UUID. Clean FK.
-- Rows whose cabinet_id no longer resolves are cleaned up first.
DELETE FROM medisync.slot_group
WHERE cabinet_id NOT IN (SELECT id FROM medisync.kiosks);

ALTER TABLE medisync.slot_group
    ADD CONSTRAINT slot_group_cabinet_id_fkey
    FOREIGN KEY (cabinet_id) REFERENCES medisync.kiosks(id);

-- ── Step 5: Drop cabinet table + all legacy schemas ───────────────
DROP TABLE IF EXISTS cabinet.cabinet;
DROP SCHEMA IF EXISTS cabinet;
DROP SCHEMA IF EXISTS identity;
DROP SCHEMA IF EXISTS catalog;
DROP SCHEMA IF EXISTS inventory;
DROP SCHEMA IF EXISTS dispensing;
DROP SCHEMA IF EXISTS audit;
