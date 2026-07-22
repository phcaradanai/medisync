-- 0010_projects_rbac.sql
-- Multi-tenant RBAC: projects table + project_id columns.
-- Phase 1 of 3-phase migration: add nullable columns first.

-- ── Projects table ──────────────────────────────────────────────

CREATE TABLE identity.projects (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text        NOT NULL,
    slug        text        NOT NULL,
    active      boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT projects_slug_unique UNIQUE (slug),
    CONSTRAINT projects_name_not_empty CHECK (char_length(name) > 0),
    CONSTRAINT projects_slug_not_empty CHECK (char_length(slug) > 0)
);

CREATE INDEX projects_active_idx ON identity.projects (active);

-- ── User project scoping (nullable — backfilled in next migration) ──

ALTER TABLE identity.users
    ADD COLUMN project_id uuid REFERENCES identity.projects(id);

CREATE INDEX users_project_id_idx ON identity.users (project_id);

-- ── Kiosk project scoping ───────────────────────────────────────

ALTER TABLE identity.kiosks
    ADD COLUMN project_id uuid REFERENCES identity.projects(id);

CREATE INDEX kiosks_project_id_idx ON identity.kiosks (project_id);

-- ── Drug catalog project scoping ────────────────────────────────

ALTER TABLE catalog.drug
    ADD COLUMN project_id uuid REFERENCES identity.projects(id);

-- Drop old single-code unique constraint; replaced with composite.
ALTER TABLE catalog.drug
    DROP CONSTRAINT IF EXISTS drug_code_unique;

-- Re-add as composite: drug code unique per project.
ALTER TABLE catalog.drug
    ADD CONSTRAINT drug_code_project_unique UNIQUE (code, project_id);

CREATE INDEX drug_project_id_idx ON catalog.drug (project_id);

-- ── Inventory slot project scoping ──────────────────────────────

ALTER TABLE inventory.slot
    ADD COLUMN project_id uuid REFERENCES identity.projects(id);

CREATE INDEX slot_project_id_idx ON inventory.slot (project_id);

-- ── Dispensing prescription project scoping ─────────────────────

ALTER TABLE dispensing.prescription
    ADD COLUMN project_id uuid;

CREATE INDEX prescription_project_id_idx ON dispensing.prescription (project_id);

-- ── Audit log project scoping ───────────────────────────────────

ALTER TABLE audit.audit_log
    ADD COLUMN project_id uuid;

CREATE INDEX audit_log_project_id_idx ON audit.audit_log (project_id);

-- ── Cabinet project scoping ─────────────────────────────────────

ALTER TABLE cabinet.cabinet
    ADD COLUMN project_id uuid REFERENCES identity.projects(id);

CREATE INDEX cabinet_project_id_idx ON cabinet.cabinet (project_id);

-- ── Outbox (future-proofing — nullable for now) ─────────────────

ALTER TABLE dispensing.outbox
    ADD COLUMN project_id uuid;
