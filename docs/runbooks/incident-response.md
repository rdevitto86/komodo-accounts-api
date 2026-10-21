# Accounts-API Incident Response

## Severity classification

| Severity | Condition | Impact |
|----------|-----------|--------|
| **P1** | Accounts-api down (both listeners) | No profile reads/writes; auth-api's credentials lookup fails, blocking login for password/OTP flows |
| **P1** | DynamoDB unreachable | All reads/writes fail; `/health/ready` reports not-ready (`health.DynamoDBChecker`) |
| **P2** | S3 unreachable (exports or avatars bucket) | GDPR export (`POST /v1/me/profile/export`) and avatar upload fail; profile/address/payment CRUD unaffected |
| **P2** | Elevated 5xx rate (`Account5xxAlarm`) | Degraded service for a subset of requests |
| **P2** | `AccountNotFoundAlarm` firing at volume | Either a real outage on the credentials hot path or a caller (auth-api) pointed at the wrong route/id shape — see GO-gate history in `TODO.md` for this exact failure class recurring twice |
| **P1** | Every authenticated public route returns 400 | Known-broken (`TODO.md` 24.1): the shared SDK's header validator has no JWT client injected, so `Authorization` fails validation on every `/v1/me/*` request. Symptom is a blanket `"request contents invalid"` with no route working and no 5xx to alarm on. Not a config problem — do not chase secrets or DynamoDB. Fixed by the SDK release tracked in 24.37 |
| **P2** | Rate limiting appears global rather than per-client | Known-broken (`TODO.md` 24.4): `TRUSTED_PROXY_DEPTH` is unset, so the limiter keys on the ALB's IP and every internet client shares one bucket. A single caller can exhaust the limit for everyone, and the 1 rps enumeration guard on `/v1/accounts/exists` does not function. If 429s spike across unrelated customers at once, this is why |
| **P3** | `LatencyP99Alarm`, `UnhealthyTargetsAlarm`, `High5xxErrorAlarm` silent | Known-broken (`TODO.md` 24.3): all three set the CloudWatch dimension to `loadBalancerArn` where `loadBalancerFullName` is required, so they sit permanently in OK. Treat p99, unhealthy hosts, and load-balancer 5xx as unmonitored until fixed |
| **P3** | No alarm reaches a person | Known-broken (`TODO.md` 24.3): no SNS topic exists and no alarm carries a notification action. Every alarm below is a dashboard colour change only — incidents are currently found by someone looking |

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

**On any "every public route returns 400" report**, work this order — the failure class has three known causes, two historical and one currently live:

1. **The SDK header-validator gap (`TODO.md` 24.1) — live today.** No JWT client is injected into `api/headers`, so `Authorization` fails validation on every `/v1/me/*` route. This is the default explanation until the SDK fix lands; it is not environmental and will not resolve on restart.
2. **`validation_rules.yaml` missing or unreadable at `EVAL_RULES_PATH`.** The rule loader fails closed: no rules means no matching rule, which means a blanket 400. Has caused this exact symptom twice historically. Confirm the file is present in the image and that `EVAL_RULES_PATH` matches.
3. **A recent deploy or config change**, or secret-format corruption in the bundle.

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
4. Note: `BatchGetItem` `UnprocessedKeys` **are** retried — the SDK retries up to 5 times with backoff (`aws/dynamodb/operations.go:146-170,195`) and errors out if keys remain unprocessed. Earlier versions of this runbook said otherwise; that is no longer true.

   The real hazard on this path is different (`TODO.md` 24.13): if the `SETTINGS` item is missing from the batch response, the code **defaults the account's status to `active`** rather than failing. The read is not strongly consistent, so this is reachable under eventual consistency, not only on genuine absence. Symptom: a suspended or closed account signing in successfully. Correlate with recent settings writes, not with throttling.

### P2: S3 unreachable (exports / avatars buckets)

**Impact:** `POST /v1/me/profile/export` (GDPR data-download) and the avatar presign flow fail. Everything else (profile, address, payments, preferences, settings, passkeys) is unaffected — S3 is not on those paths.

1. **Check bucket accessibility:**
   ```bash
   aws s3api head-bucket --bucket komodo-accounts-exports-<env>
   aws s3api head-bucket --bucket komodo-accounts-avatars-<env>
   ```
2. **Check IAM task role** has the expected `PutObject`/`GetObject`/`DeleteObject` grants — both buckets share the task role (`buildAccountExportsBucket`/`buildAvatarsBucket`, `deploy/cdk/main.ts`).
3. Erasure failures are **loud by design**: account closure fails the whole request with a 5xx if the S3-side cleanup fails, rather than succeeding with orphaned objects left behind. A spike in 5xx specifically on `DELETE /v1/me/profile` or `POST /v1/me/profile/export` points here first.

   **This matters more than it used to.** Closure now erases synchronously on the customer's request rather than in a background job, so an S3 outage turns directly into failed closures. Two known gaps compound it: the avatar object is not deleted at all (`TODO.md` 24.7), and the DynamoDB delete runs *before* the S3 cleanup, so a mid-operation failure can leave an account whose records are gone but whose files are not (`TODO.md` 24.17).
4. Avatar presign path (`AvatarUploadHandler` / `GetAvatarUploadURL`) has thin test coverage — if it's implicated, verify behaviour by hand against a real bucket rather than trusting existing tests to have caught a regression.

### P3: `/v1/accounts/exists` abuse or rate-limiter memory growth

1. Check the 429 rate on this route specifically — it carries its own limiter (1 rps, burst 5) independent of the global one.
2. **The limiter is not currently per-IP.** `TRUSTED_PROXY_DEPTH` is unset, so it keys on the ALB's address and every internet client shares one bucket (`TODO.md` 24.4). That means the enumeration guard this route exists for is not working, and it also means 429s here are not evidence of a single abusive client.
3. Memory growth is bounded on this limiter — it sets `MaxEntries` (50,000) and a 300 s bucket TTL (`cmd/server/setup.go`). Earlier versions of this runbook said it never evicts; that is no longer true. The **general** limiter is the uncapped one (`TODO.md` 19.6), and it becomes a real concern only once per-IP keying is fixed.
4. If sustained: check WAF rules on the ALB and consider an IP-based block at that layer. Note the two per-path WAF rate limits target paths that do not exist (`TODO.md` 17.3), so only the global 2000/5min WAF limit is currently doing anything.

## Idempotency caveats (operational, not an incident by itself)

The idempotency store **is** configured — `cmd/server/main.go:29` calls `idempotency.SetStore` with the `IDEMPOTENCY_TTL_SEC` value from the secret bundle. Earlier versions of this runbook said it was never called; that is no longer true.

Three real caveats remain when investigating a suspected duplicate write (e.g. two addresses created from one user action):

1. **The store is per-task and in-memory.** A retry that lands on a different task misses the cache entirely. With autoscaling above one task, cross-instance dedup does not exist — DynamoDB conditional writes are the actual correctness boundary, not the middleware.
2. **Keys are globally namespaced** (`TODO.md` 24.21) — scoped by the header value alone, not by account, method, or path. A 409 on a write the customer never retried may be another customer's key collision.
3. **A key is burned before the handler runs and never rolled back.** A write that 5xx'd still consumes its key for the full TTL, so the client's legitimate retry gets a 409 rather than a second attempt.

The private plane bypasses the middleware entirely, so no service-to-service write is covered by it.

## Post-incident

1. **Preserve logs.** Log group retention is `ONE_MONTH`, but **`RemovalPolicy.DESTROY`** (`deploy/cdk/main.ts`) — unlike the accounts table and the export/avatar buckets, the log group does **not** survive a stack teardown in any environment. Export relevant time ranges immediately if a teardown is anywhere on the horizon.
2. **Timeline:** detection time, first response action, mitigation time, full resolution time.
3. **Root cause analysis:** what failed, why, what would have caught it sooner.
4. **Action items:** file `TODO.md` entries for any gap surfaced. Every "known-broken" caveat above cites a live `TODO.md` item — when one of those closes, update this runbook in the same change. Stale reassurance in a runbook is worse than no runbook: this document previously told responders that four things were broken which had already been fixed, and one was fixed which was not.
5. **Communication:** notify auth-api (credentials hot-path consumer) and comms-api (unsubscribe-mint caller) if the incident affected either shared surface.
