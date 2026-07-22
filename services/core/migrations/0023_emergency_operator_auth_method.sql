-- 0023: record how the emergency operator was authenticated.

ALTER TABLE medisync.emergency_dispense_transaction
    ADD COLUMN IF NOT EXISTS operator_auth_method text NOT NULL DEFAULT 'EMPLOYEE_CODE';

ALTER TABLE medisync.emergency_dispense_transaction
    DROP CONSTRAINT IF EXISTS emergency_dispense_operator_auth_method_check;

ALTER TABLE medisync.emergency_dispense_transaction
    ADD CONSTRAINT emergency_dispense_operator_auth_method_check
    CHECK (operator_auth_method IN ('CARD', 'EMPLOYEE_CODE'));

CREATE INDEX IF NOT EXISTS emergency_dispense_auth_method_created_idx
    ON medisync.emergency_dispense_transaction (operator_auth_method, created_at DESC);
