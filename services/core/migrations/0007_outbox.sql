-- 0007_outbox.sql
-- Add outbox table for reliable event publishing within dispensing transactions.
-- Events are inserted in the same transaction as the state mutation; a separate
-- publisher goroutine reads and publishes to JetStream.

CREATE TABLE dispensing.outbox (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject     text        NOT NULL,
    payload     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    published   boolean     NOT NULL DEFAULT false
);

CREATE INDEX outbox_unpublished_idx ON dispensing.outbox (created_at) WHERE NOT published;
