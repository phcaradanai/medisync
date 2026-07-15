-- identity.kiosks: provisioned kiosk terminals.
-- Kiosks authenticate with code + PIN (bcrypt hash). The PIN is never
-- stored or retrievable in plaintext — it is revealed only once at
-- creation or reset via the admin handler response.

CREATE TABLE identity.kiosks (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    code          text        NOT NULL,
    display_name  text        NOT NULL DEFAULT '',
    pin_hash      text        NOT NULL,
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT kiosks_code_unique UNIQUE (code),
    CONSTRAINT kiosks_code_length CHECK (char_length(code) >= 3),
    CONSTRAINT kiosks_pin_hash_not_empty CHECK (char_length(pin_hash) > 0)
);

CREATE INDEX kiosks_code_idx ON identity.kiosks (code);
