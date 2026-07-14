-- catalog.drug: drug master data for the catalog bounded context.
-- Schema catalog.* is created by 0001_init.sql; this migration adds
-- the drug table with a unique hospital code constraint.

-- The gin_trgm_ops indexes below require the pg_trgm extension.
-- It is available in all standard PostgreSQL images.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE TABLE catalog.drug (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    code         text        NOT NULL,
    name         text        NOT NULL,
    generic_name text        NOT NULL DEFAULT '',
    form         text        NOT NULL DEFAULT '',
    strength     text        NOT NULL DEFAULT '',
    unit         text        NOT NULL DEFAULT '',
    sticker_note text        NOT NULL DEFAULT '',
    active       boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT drug_code_unique UNIQUE (code)
);

CREATE INDEX drug_active_idx ON catalog.drug (active);
CREATE INDEX drug_name_trgm_idx ON catalog.drug USING gin (name gin_trgm_ops);
CREATE INDEX drug_generic_name_trgm_idx ON catalog.drug USING gin (generic_name gin_trgm_ops);
