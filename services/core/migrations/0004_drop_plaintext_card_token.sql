-- Remove the plaintext card_token column. Existing card tokens are
-- nullified first (invalidating any raw-token entries), then the column
-- and its partial index are dropped. Cards must be re-enrolled via the
-- hashed-write path (HMAC-SHA256 stored in card_token_hash).
--
-- This is non-reversible; it does not embed secrets in SQL.

-- Step 1: NULLify all existing card_token values.
-- This invalidates any enrolled cards before the column is dropped.
UPDATE identity.users SET card_token = NULL WHERE card_token IS NOT NULL;

-- Step 2: Drop the partial index that references card_token.
DROP INDEX IF EXISTS identity.users_card_token_idx;

-- Step 3: Drop the card_token column. The UNIQUE constraint is removed
-- with the column.
ALTER TABLE identity.users DROP COLUMN IF EXISTS card_token;
