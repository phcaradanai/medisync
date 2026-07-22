-- 0009_cabinet: physical vending machine registry.

CREATE SCHEMA IF NOT EXISTS cabinet;

CREATE TABLE IF NOT EXISTS cabinet.cabinet (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
