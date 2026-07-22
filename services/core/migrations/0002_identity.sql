-- identity.users: the user table for the identity bounded context.
-- Schema identity.* is created by 0001_init.sql; this migration adds
-- the users table with role validation and ward-scoping support.

CREATE TABLE identity.users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL,
    password_hash text        NOT NULL DEFAULT '',
    display_name  text        NOT NULL DEFAULT '',
    role          text        NOT NULL DEFAULT 'NURSE',
    ward_ids      text[]      NOT NULL DEFAULT '{}',
    card_token    text        UNIQUE,
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_username_unique UNIQUE (username),
    CONSTRAINT users_role_check CHECK (
        role IN ('ADMIN', 'PHARMACIST', 'NURSE', 'REFILLER')
    )
);

CREATE INDEX users_username_idx ON identity.users (username);
CREATE INDEX users_card_token_idx ON identity.users (card_token) WHERE card_token IS NOT NULL;
