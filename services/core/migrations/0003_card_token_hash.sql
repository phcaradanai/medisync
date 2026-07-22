-- Add card_token_hash for deterministic keyed-hash lookup of card tokens.
-- The raw card_token column is kept for backward compatibility; new code
-- should populate card_token_hash via HMAC-SHA256 and query by hash.

ALTER TABLE identity.users ADD COLUMN card_token_hash BYTEA;

CREATE UNIQUE INDEX users_card_token_hash_unique
    ON identity.users (card_token_hash) WHERE card_token_hash IS NOT NULL;
