-- 0011_backfill_project.sql
-- Phase 2: create a default project and backfill all existing data.
-- Also promotes the existing admin user to SYSADMIN (project_id = NULL).
-- Run only after 0010_projects_rbac.sql.

-- ── Create default project ──────────────────────────────────────

INSERT INTO identity.projects (id, name, slug)
VALUES ('00000000-0000-0000-0000-000000000001', 'Default Project', 'default')
ON CONFLICT (slug) DO NOTHING;

-- ── Backfill users with default project ─────────────────────────
-- SYSADMIN (existing admin) keeps project_id = NULL — they are cross-project.
-- All other users get assigned to the default project.

UPDATE identity.users
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL
   AND role != 'ADMIN';

-- ── Backfill kiosks ─────────────────────────────────────────────

UPDATE identity.kiosks
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill catalog drugs ──────────────────────────────────────

UPDATE catalog.drug
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill inventory slots ────────────────────────────────────

UPDATE inventory.slot
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill prescriptions ──────────────────────────────────────

UPDATE dispensing.prescription
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill audit entries ──────────────────────────────────────
-- Historical entries get default project; post-migration entries
-- will have real project_id from the interceptor.

UPDATE audit.audit_log
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill cabinets ───────────────────────────────────────────

UPDATE cabinet.cabinet
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Backfill outbox ─────────────────────────────────────────────

UPDATE dispensing.outbox
   SET project_id = '00000000-0000-0000-0000-000000000001'
 WHERE project_id IS NULL;

-- ── Add NOT NULL constraints (Phase 3 — after all backfills) ────
-- Only on tables where project_id is always required.

-- Users: SYSADMIN is NULL, so this stays nullable. All others have project_id.
-- No NOT NULL on users.project_id.

ALTER TABLE identity.kiosks
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE catalog.drug
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE inventory.slot
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE cabinet.cabinet
    ALTER COLUMN project_id SET NOT NULL;

-- dispensing.prescription and audit.audit_log stay nullable —
-- NATS events and legacy data may arrive without project context.
-- Middleware enforces project_id at the application layer.
