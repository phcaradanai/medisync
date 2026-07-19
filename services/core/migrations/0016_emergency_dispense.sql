-- 0016: Emergency dispensing support
-- Enables sticker-less emergency medication access via card verification.

-- Add emergency_access flag to users (who can access emergency meds)
ALTER TABLE identity.users ADD COLUMN IF NOT EXISTS emergency_access BOOLEAN NOT NULL DEFAULT FALSE;

-- Add emergency flag to slots (which drugs are available for emergency)
ALTER TABLE inventory.slot ADD COLUMN IF NOT EXISTS emergency_drug BOOLEAN NOT NULL DEFAULT FALSE;

-- Emergency dispense audit log
CREATE TABLE IF NOT EXISTS dispensing.emergency_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES identity.users(id),
    slot_id         UUID NOT NULL REFERENCES inventory.slot(id),
    drug_code       TEXT NOT NULL,
    quantity        INTEGER NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    kiosk_id        TEXT NOT NULL DEFAULT '',
    card_token_hash TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_emergency_log_user ON dispensing.emergency_log(user_id);
CREATE INDEX IF NOT EXISTS idx_emergency_log_created ON dispensing.emergency_log(created_at DESC);
