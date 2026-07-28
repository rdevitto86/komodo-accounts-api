# Unsubscribe Token Secret Rotation

Accounts-api does not sign or verify JWTs — it verifies auth-api-issued tokens against a public key (`JWT_PUBLIC_KEY`) that auth-api owns and rotates; see auth-api's own key-rotation runbook for that key. The one secret this service mints and verifies with itself is `UNSUBSCRIBE_TOKEN_SECRET`, the HMAC key behind the unsubscribe-token flow (`MintUnsubscribeTokenHandler` / `UnsubscribeHandler`, `internal/api/account.go`).

## Before you rotate: this is a hard cutover, not a graceful one

Unlike auth-api's JWT signing key (two-slot overlap, old key still verifies until the next rotation), the unsubscribe HMAC has **no key-ID or multi-key support** — there is exactly one `s.unsubscribeKey` used for both minting and verification. Rotating it **immediately invalidates every outstanding unsubscribe token**, including ones already sent in emails that a recipient hasn't clicked yet. Tokens carry a 30-day TTL by design (`TODO.md` decisions log), so at any given time there can be up to 30 days of live links in inboxes.

**Also note:** secrets are fetched once at boot (`bootstrap()`, `cmd/server/setup.go`) — there is no hot-reload. A rotation does not take effect until every running task restarts.

**Implication:** treat this as a scheduled, coordinated rotation, not something to do casually. If it must happen (suspected key compromise), accept that outstanding links break and that comms-api will need to be told to stop sending links minted with the old key and to re-trigger any mint it wants honored post-rotation.

## When to rotate

| Trigger | Urgency | Notes |
|---------|---------|-------|
| Scheduled | Low priority | No forcing function today — this key has no rotation cadence requirement; treat as opportunistic, coordinated with comms-api |
| Suspected key compromise | Emergency | See below |

## Rotation steps

1. **Generate a new secret:**
   ```bash
   NEW_SECRET=$(openssl rand -base64 32)
   ```

2. **Update Secrets Manager:**
   ```bash
   ENV=production  # or staging, dev
   aws secretsmanager put-secret-value \
     --secret-id "komodo/${ENV}/accounts-api" \
     --secret-string "$(aws secretsmanager get-secret-value --secret-id "komodo/${ENV}/accounts-api" --query SecretString --output text | jq --arg s "$NEW_SECRET" '.UNSUBSCRIBE_TOKEN_SECRET = $s')"
   ```
   This patches just the one key, preserving every other secret in the bundle (`JWT_PUBLIC_KEY`, `JWT_AUDIENCE`, `JWT_ISSUER`, `ACCOUNTS_API_CLIENT_ID`/`SECRET`, `DYNAMODB_TABLE`, IP allow/deny lists, rate-limit config, bucket names — see `secretKeys` in `cmd/server/setup.go`).

3. **Restart every running task** (required — no hot-reload):
   ```bash
   aws ecs update-service --cluster komodo-<env> --service accounts-api --force-new-deployment
   ```

4. **Coordinate with comms-api** *before* step 2 if possible: any unsubscribe link already sent (or in-flight to be sent) with the old key becomes unverifiable the moment every task has restarted. Comms-api should stop minting new links a few minutes ahead of the rotation and resume only after all tasks report the new secret is live.

5. **Verify:**
   ```bash
   curl -f http://localhost:7051/health
   # Mint a token against a real test account, then verify it round-trips:
   curl -X POST http://accounts-internal.<env>:7052/internal/v1/accounts/<test-id>/communications/unsubscribe-token \
     -H "Authorization: Bearer <service-jwt>" \
     -H 'Content-Type: application/json' \
     -d '{"channel": "email"}'
   # then POST the returned token to /v1/communications/unsubscribe and confirm 204
   ```

## Emergency rotation (compromised secret)

Follow all steps above, plus:

1. **Accept the invalidation blast radius immediately** rather than trying to soften it — there is no overlap mechanism to fall back on. Every outstanding unsubscribe link (up to 30 days' worth) breaks the moment the last task restarts.
2. **Notify comms-api** to halt minting immediately (before you rotate, if there's any lead time at all) and to be ready to re-mint for any recipient who complains a link doesn't work post-rotation — that is expected, not a bug, until comms-api re-sends.
3. **File an incident report** per the [incident response runbook](./incident-response.md).

## Rollback

If the new secret needs to be reverted (e.g. rotated by mistake, or comms-api wasn't ready):

1. Re-upload the previous secret value to Secrets Manager (you must have retained it — there's no way to recover it from the running service otherwise).
2. Force a new deployment to pick it up (same restart requirement as any rotation).
3. Any token minted during the brief window the new secret was active will fail verification once you roll back — same hard-cutover behavior in reverse.

## Longer-term fix worth considering

If this secret rotates often enough for the hard-cutover behavior to become painful, the real fix is a key-ID scheme mirroring auth-api's JWKS overlap (embed a short key version in the minted token, verify against either the current or previous secret keyed by that version). Not built today — this runbook documents the process as the code actually behaves, not the process we'd prefer.
