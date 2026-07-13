# Card Re-enrollment

Migration `0004_drop_plaintext_card_token.sql` invalidates existing card assignments and removes the plaintext `identity.users.card_token` column. Card login remains unavailable for each user until that user's card is enrolled again.

## Preconditions

- Deploy migrations `0003_card_token_hash.sql` and `0004_drop_plaintext_card_token.sql` successfully.
- Set one stable, random `CARD_TOKEN_HMAC_KEY` of at least 32 bytes through the production secret manager.
- Back up that key. Losing or rotating it invalidates every enrolled card.
- Confirm the operator is authorized to manage the target user.

## Enrollment Path

Enrollment code must call `identity.Store.SetCardToken(ctx, userID, rawToken)`. This method HMAC-SHA256 hashes the token and writes only 32 raw hash bytes to `card_token_hash`. It rejects missing hash configuration, empty tokens, unknown users, and database errors.

No public card-enrollment API exists yet. A future admin handler or a controlled one-off command may invoke this store method. Do not write card values or hashes with ad hoc SQL, and never log the raw token.

## Procedure

1. Verify the user's identity and active status.
2. Read the card token once over a trusted workstation or reader.
3. Invoke the trusted enrollment adapter that calls `SetCardToken`.
4. Discard the raw token immediately after the call.
5. Test card login and verify it returns the intended user.
6. Record operator, user ID, timestamp, and outcome without token data.

Re-enrolling a user replaces the previous hash. The unique database index prevents one card hash from being assigned to multiple users.

## Verification

```sql
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_schema = 'identity'
  AND table_name = 'users'
  AND column_name IN ('card_token', 'card_token_hash');
```

Expected result: only `card_token_hash | bytea`. Do not select or export hash values during routine verification.
