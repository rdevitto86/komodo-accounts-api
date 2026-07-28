# Accounts-API Incident Response

## Severity classification

| Severity | Condition | Impact |
|----------|-----------|--------|
| **P1** | Accounts-api down (both listeners) | No profile reads/writes; auth-api's credentials lookup fails, blocking login for password/OTP flows |
| **P1** | DynamoDB unreachable | All reads/writes fail; `/health/ready` reports not-ready (`health.DynamoDBChecker`) |
| **P2** | S3 unreachable (exports or avatars bucket) | GDPR export (`POST /v1/me/profile/export`) and avatar upload fail; profile/address/payment CRUD unaffected |
| **P2** | Elevated 5xx rate (`Account5xxAlarm`) | Degraded service for a subset of requests |
| **P2** | `AccountNotFoundAlarm` firing at volume | Either a real outage on the credentials hot path or a caller (auth-api) pointed at the wrong route/id shape — see GO-gate history in `TODO.md` for this exact failure class recurring twice |
| **P3** | `/v1/accounts/exists` abuse | Public, unauthenticated, per-IP rate-limited (1 rps / burst 5) but the limiter map never evicts idle entries (`TODO.md` 13.3) — sustained abuse from IP churn grows memory, not just request volume |
| **P3** | `LatencyP99Alarm` silent | Known-broken: the CloudWatch dimension uses `loadBalancerArn` instead of `loadBalancerFullName` (`TODO.md` 13.5), so this alarm does not fire even under real p99 blowouts — treat p99 as unmonitored until that's fixed |

## Immediate actions by severity

### P1: Accounts-api down (both listeners)

Single Fargate task serves both ports — public `7051` (external, via ALB) and private `7052` (internal, via Cloud Map `accounts-api.komodo.internal`).

1. **Container/task health:**
   ```bash
   aws ecs describe-services --cluster komodo-<env> --services accounts-api
   # local: docker ps --filter "name=accounts-api"
   ```

2. **Check logs for panic/OOM/boot failure:**
   ```bash
   docker logs accounts-api --tail 200
   # CloudWatch: filter by service=komodo-accounts-api, level=ERROR|FATAL
   ```
   Boot failures are common here — `bootstrap()` (`cmd/server/setup.go`) calls `logger.Fatal` on any secret-fetch, JWT-verifier-init, DynamoDB-client-init, or S3-client-init failure, and `cmd/server/main.go` fails fast if `PORT`/`PORT_PRIVATE` aren't set. A crash-loop on deploy is almost always one of those four, not application logic.

3. **Secrets Manager accessibility:**
   ```bash
   aws secretsmanager get-secret-value --secret-id "komodo/<env>/accounts-api" --query 'Name' --output text
   ```
   Secrets are fetched **once at boot**, not polled — there is no hot-reload here (contrast with auth-api's `secretsmanager.Watch`). A secret fix requires a restart, not just a Secrets Manager update.

4. **DynamoDB reachability** (see P1 DynamoDB section below).

5. **Network:**
   - Security group: `7051` inbound from ALB SG only, `7052` inbound from VPC CIDR only (no internet path to `7052` by design)
   - Cloud Map service discovery resolving `accounts-api.komodo.internal`
   - ALB target health

6. **Restart:**
   ```bash
   aws ecs update-service --cluster komodo-<env> --service accounts-api --force-new-deployment
   # local: docker compose restart accounts-api
   ```

7. **Verify recovery:**
   ```bash
   curl -f http://localhost:7051/health
   curl -f http://localhost:7051/health/ready   # DynamoDB reachability check
   curl -f http://localhost:7052/health          # from within the VPC only
   ```

If persistent after restart: check for a recent deploy/config change, secret format corruption, or the `validation_rules.yaml` shipping/path issue that has broken every public route with a blanket 400 twice before (`TODO.md` 11.1/11.3, 17.1 — both closed, but the same failure class is worth ruling out first on any "every route 400s" report).

### P1: DynamoDB unreachable

**Impact:** every profile/address/payment/preferences/settings/passkey/consent read and write fails. `GET /health/ready` reports not-ready via `health.DynamoDBChecker`. The credentials hot path (`GET /v1/accounts/credentials`, called by auth-api on every login) fails, which blocks login service-wide even though accounts-api itself is "up."

1. **Check table status:**
   ```bash
   aws dynamodb describe-table --table-name <table-name> --query 'Table.TableStatus'
   ```
2. **Check for throttling** (CloudWatch `ConsumedReadCapacityUnits`/`ConsumedWriteCapacityUnits` vs. provisioned — table is `PAY_PER_REQUEST`, so sustained throttling usually means an AWS-side capacity event, not a config error on our side).
3. **Verify recovery:**
   ```bash
   curl -f http://localhost:7051/health/ready
   ```
4. Note: `BatchGetItem` `UnprocessedKeys` are not retried today (`TODO.md` 17.2) — under partial throttling, the credentials hot path can silently return a spurious 404 (profile item unprocessed) or `email_verified:false` (settings item unprocessed) instead of a clean error. If login failures correlate with DynamoDB throttling metrics but `/health/ready` still reports healthy, this is the likely mechanism — it is a known open gap, not new corruption.

### P2: S3 unreachable (exports / avatars buckets)

**Impact:** `POST /v1/me/profile/export` (GDPR data-download) and the avatar presign flow fail. Everything else (profile, address, payments, preferences, settings, passkeys) is unaffected — S3 is not on those paths.

1. **Check bucket accessibility:**
   ```bash
   aws s3api head-bucket --bucket komodo-accounts-exports-<env>
   aws s3api head-bucket --bucket komodo-accounts-avatars-<env>
   ```
2. **Check IAM task role** has the expected `PutObject`/`GetObject`/`DeleteObject` grants — both buckets share the task role (`buildAccountExportsBucket`/`buildAvatarsBucket`, `deploy/cdk/main.ts`).
3. Export failures are **loud by design**: `DeleteProfile` fails the whole request with a 5xx if the S3-side export cleanup fails, rather than silently succeeding with orphaned S3 objects (`TODO.md` Stage 1). A spike in 5xx specifically on `DELETE /v1/me/profile` or `POST /v1/me/profile/export` points here first.
4. Avatar presign path (`AvatarUploadHandler` / `GetAvatarUploadURL`) has zero test coverage today (`TODO.md` 17.13) — if it's implicated, verify behavior by hand against a real bucket rather than trusting existing tests to have caught a regression.

### P3: `/v1/accounts/exists` abuse or rate-limiter memory growth

1. Check the 429 rate on this route specifically — it's public, unauthenticated, and rate-limited per-IP (1 rps, burst 5), independent of the global rate limit.
2. If request volume is fine but container memory is climbing anyway: the per-IP limiter map (`newExistsRateLimiter`, `cmd/server/setup.go`) is a plain `sync.Map` with no eviction — IP churn (not just request rate) grows it unboundedly (`TODO.md` 13.3). A restart clears it; there is no runtime mitigation short of a code fix.
3. If sustained: check WAF rules on the ALB, consider an IP-based block at that layer rather than waiting on the in-process limiter.

## Idempotency caveat (operational, not an incident by itself)

`configureIdempotencyStore()` is never called (`TODO.md` 13.4) — the idempotency middleware is wired on all public write routes, but runs with the SDK's zero-TTL default in effect, not the configured `IDEMPOTENCY_TTL_SEC`. In practice this means a retried write (client timeout + retry, BFF double-submit) has a much smaller idempotency window than the config implies. If you're investigating a suspected duplicate-write report (e.g. two addresses created from one user action), check this before assuming a bug in the conditional-write logic — the 12.7 conditional-write guards (409 on a supplied/replayed ID) only cover ID-bearing retries; a retry with no ID at all depends entirely on this middleware.

## Post-incident

1. **Preserve logs.** Log group retention is `ONE_MONTH`, but **`RemovalPolicy.DESTROY`** (`deploy/cdk/main.ts`) — unlike the accounts table and the export/avatar buckets, the log group does **not** survive a stack teardown in any environment. Export relevant time ranges immediately if a teardown is anywhere on the horizon.
2. **Timeline:** detection time, first response action, mitigation time, full resolution time.
3. **Root cause analysis:** what failed, why, what would have caught it sooner.
4. **Action items:** file `TODO.md` entries for any gap surfaced (this runbook should stay in sync with `TODO.md`'s open 13.x/14.x/15.x/17.x items — several of the caveats above are direct links to still-open items there).
5. **Communication:** notify auth-api (credentials hot-path consumer) and comms-api (unsubscribe-mint caller) if the incident affected either shared surface.
