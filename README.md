# komodo-accounts-api

The canonical store for Komodo account identity: profiles, account settings, passkey public keys,
preferences, address books, payment-method summaries, and consent history.

It is a **data store, not an authenticator**. auth-api issues every token and verifies credentials by
reading them over the private plane; this service stores what auth-api tells it to and serves the
lookup on the sign-in hot path. See `docs/PRD.md` for scope and `docs/sdd.md` for design.

> ⚠️ **Pre-release, and currently NO-GO for staging.** Five blockers are open — most importantly, every
> authenticated public route returns 400 today because the shared SDK's header validator has no JWT
> client injected. See `TODO.md` § STG blockers before assuming any `/v1/me/*` route works.

---

## Planes and ports

One binary, two listeners. The listener is the plane boundary — there is no path prefix distinguishing
them, and the private listener is never exposed through the load balancer.

| Plane | Port | Env var | Reachable from | Auth |
|---|---|---|---|---|
| Public | 7051 | `PORT` | Internet, via ALB + WAF | Customer JWT, audience `komodo-apis:user` |
| Private | 7052 | `PORT_PRIVATE` | Inside the VPC only, via Cloud Map `accounts-api.komodo.internal` | Service JWT, audience `komodo-apis:service` |

---

## Routes

Generated from `cmd/server/setup.go`. `openapi.yaml` is the contract source of truth for request and
response shapes; this table is a map of what exists.

Routes marked **stub, returns 501** are registered and published in the contract but not implemented:
the handler authenticates the caller, then returns `501 Not Implemented`. Treat them as reserved
surface. Implementation work is tracked in `TODO.md` §26.

### Public (`:7051`)

| Method | Path | Chain | Description |
|---|---|---|---|
| `GET` | `/health` | none | Liveness — always 200, checks nothing |
| `GET` | `/health/ready` | none | Readiness — verifies DynamoDB is reachable |
| `GET` | `/v1/accounts/exists` | unauth + own limiter | Is this email registered (rate-limited enumeration oracle) |
| `POST` | `/v1/communications/unsubscribe` | unauth | Verify unsubscribe token, record consent opt-out |
| `GET` | `/v1/me/profile` | read | Get authenticated account's profile |
| `POST` | `/v1/me/profile` | write | Create account record on registration |
| `PUT` | `/v1/me/profile` | write | Update profile (partial) |
| `DELETE` | `/v1/me/profile` | write | **Immediate, irreversible erasure.** Requires `confirm_email` matching the account |
| `POST` | `/v1/me/profile/export` | write | Data download / right-to-access |
| `PUT` | `/v1/me/profile/email` | write | Change email address — **stub, returns 501** |
| `POST` | `/v1/me/profile/avatar` | write | Get a 15-minute pre-signed upload URL |
| `DELETE` | `/v1/me/profile/avatar` | write | Remove the avatar and delete the stored object — **stub, returns 501** |
| `GET` | `/v1/me/consents` | read | Own marketing consent history — **stub, returns 501** |
| `POST` | `/v1/me/consents` | write | Record an own opt-in / opt-out decision — **stub, returns 501** |
| `GET` | `/v1/me/addresses` | read | List addresses |
| `POST` | `/v1/me/addresses` | write | Add an address |
| `GET` | `/v1/me/addresses/{id}` | read | Get one address — **stub, returns 501** |
| `PUT` | `/v1/me/addresses/{id}` | write | Update an address (partial) |
| `DELETE` | `/v1/me/addresses/{id}` | write | Delete an address |
| `GET` | `/v1/me/payments` | read | List saved payment methods (display-safe summaries) |
| `PUT` | `/v1/me/payments` | write | Add or update a payment method (full-replace upsert) |
| `GET` | `/v1/me/payments/{id}` | read | Get one payment method — **stub, returns 501** |
| `DELETE` | `/v1/me/payments/{id}` | write | Remove a payment method |
| `GET` | `/v1/me/preferences` | read | Get preferences |
| `PUT` | `/v1/me/preferences` | write | Update preferences (partial) |
| `GET` | `/v1/me/settings` | read | Own verified flags + lifecycle status, tags excluded — **stub, returns 501** |

### Private (`:7052`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness |
| `GET` | `/health/ready` | Readiness |
| `GET` | `/v1/accounts/credentials` | Credential lookup by email — **auth-api sign-in hot path** |
| `PUT` | `/v1/accounts/{id}/credentials` | Set or rotate credentials (auth-api supplies the hash) |
| `GET` | `/v1/accounts/{id}` | Get profile by account ID |
| `GET` | `/v1/accounts/{id}/addresses` | Addresses for an account |
| `GET` | `/v1/accounts/{id}/preferences` | Preferences for an account |
| `GET` | `/v1/accounts/{id}/payments` | Payment methods for an account |
| `GET` | `/v1/accounts/{id}/passkeys` | List passkey credentials |
| `POST` | `/v1/accounts/{id}/passkeys` | Register a passkey credential |
| `PATCH` | `/v1/accounts/{id}/passkeys/{credential_id}` | Update a passkey (sign count, last used) |
| `DELETE` | `/v1/accounts/{id}/passkeys/{credential_id}` | Remove a passkey |
| `GET` | `/v1/accounts/{id}/settings` | Get account settings |
| `PUT` | `/v1/accounts/{id}/settings` | Update settings (partial, version-guarded) |
| `PUT` | `/v1/accounts/{id}/settings/tags` | Update tags (namespace-restricted per calling service) |
| `POST` | `/v1/accounts/{id}/communications/unsubscribe-token` | Mint a stateless unsubscribe token (comms-api) |

> Public routes are additionally gated by an explicit path allowlist on the ALB
> (`deploy/cdk/main.ts`). A new public route works locally and 404s in AWS until a matching listener
> rule ships alongside it.

---

## Middleware chains

Defined in `cmd/server/setup.go`.

| Chain | Composition (outermost first) |
|---|---|
| `publicRead` | RequestID → Telemetry → RateLimiter → CORS → SecurityHeaders → Auth (user JWT) → CSRF → Normalization → RuleValidation → Sanitization |
| `publicWrite` | `publicRead` + Idempotency |
| `publicUnauth` | RequestID → Telemetry → RateLimiter → CORS → SecurityHeaders |
| `internal` | RequestID → Telemetry → Auth (service JWT) → Scope |

Both `/health` endpoints are registered bare, with no chain at all.

---

## Configuration

### Process environment

Set by the CDK task definition in deployed environments, and by `docker-compose.yaml` locally.

| Variable | Required | Description |
|---|---|---|
| `PORT` | **Yes** | Public listener address, e.g. `:7051`. Boot fails without it |
| `PORT_PRIVATE` | **Yes** | Private listener address, e.g. `:7052`. Boot fails without it |
| `DYNAMODB_TABLE` | **Yes** | Table name. Boot fails without it |
| `S3_EXPORT_BUCKET` | **Yes** | Data-export bucket. Boot fails without it |
| `S3_AVATAR_BUCKET` | **Yes** | Avatar bucket. Boot fails without it |
| `AWS_SECRET_PATH` | Yes | Secrets Manager path, e.g. `komodo/stg/accounts-api` |
| `AWS_REGION` | Yes | Region — `us-east-2` in both deployed environments |
| `AWS_ENDPOINT` | Local only | LocalStack endpoint. **Setting this swaps in static test credentials** — never set it in a deployed environment |
| `EVAL_RULES_PATH` | Yes | Path to `validation_rules.yaml`. If the file is unreadable, every validated route returns 400 |
| `APP_NAME`, `VERSION` | No | Logging and image metadata |
| `LOG_LEVEL` | No | `debug`, `info`, `error`. Currently absent from the CDK task definition, so deployed environments take the SDK default |

> ⚠️ **Do not set `ENV`.** The SDK rate limiter treats `ENV=prod` or `ENV=staging` as a signal that a
> distributed (Redis) limiter is configured. None is, and fail-open is off, so setting it makes every
> public request return 500. Tracked as `TODO.md` 24.5. It is deliberately unset in the CDK.

**Tuning knobs**, all optional and all currently unset everywhere, so every environment runs on the
in-code defaults: `JWT_AUDIENCE_USER`, `JWT_AUDIENCE_SERVICE`, `BOOTSTRAP_TIMEOUT`,
`SERVER_READ_TIMEOUT`, `SERVER_WRITE_TIMEOUT`, `SERVER_IDLE_TIMEOUT`, `SERVER_READ_HEADER_TIMEOUT`,
`SERVER_SHUTDOWN_TIMEOUT`, `S3_PRESIGN_TTL`, `UNSUBSCRIBE_TOKEN_TTL`, `PROFILE_CACHE_TTL`,
`PROFILE_CACHE_MAX_ENTRIES`, `EXISTS_RATE_LIMIT_MAX_ENTRIES`, `EXISTS_RATE_LIMIT_BUCKET_TTL_SEC`,
`TRUSTED_PROXY_DEPTH`.

### Secrets

Fetched once at boot from `AWS_SECRET_PATH` — **no hot reload**, so a rotation requires a task
restart. Exactly six keys are read (`secretKeys`, `cmd/server/setup.go`):

| Key | Required | Description |
|---|---|---|
| `JWT_PUBLIC_KEY` | Yes | RSA public key (PEM) from auth-api, verifies incoming tokens |
| `JWT_ISSUER` | Yes | Expected issuer claim |
| `UNSUBSCRIBE_TOKEN_SECRET` | Yes | HMAC key for unsubscribe tokens. Rotation is a hard cutover — see `docs/runbooks/key-rotation.md` |
| `IDEMPOTENCY_TTL_SEC` | No | Defaults to 300 |
| `RATE_LIMIT_RPS` | No | Defaults to 10 |
| `RATE_LIMIT_BURST` | No | Defaults to 20 |

Nothing else in the bundle is read. This service holds **no** private signing key — it verifies
tokens, it never issues them. Table and bucket names are plain process configuration, not secrets.

Per-environment `ACCOUNTS_API_CLIENT_ID` / `ACCOUNTS_API_CLIENT_SECRET` are planned, for minting this
service's own outbound service tokens (`TODO.md` 24.36).

---

## Local development

> ⚠️ **`make build` and `make run` do not currently work.** The compose build context points at a
> directory with no `go.mod`, the referenced `env.shared` file does not exist, `BUILD_TARGET` is
> consumed by nothing, and neither compose service sets all the variables required to boot. Tracked as
> `TODO.md` 24.42. Run the Go toolchain directly until that is fixed.

```bash
make build_go        # go build ./cmd/... ./internal/... ./test/...
make vet
make test            # go test -race
make test_component  # TEST_TIER=component
make lint            # golangci-lint
```

The full phase-exit gate is `make build_go && make vet && make test_component && make lint`.

### cURL examples

Tokens come from auth-api. The public plane needs a customer token; the private plane needs a service
token whose scopes include an `svc:` entry.

```bash
BASE=http://localhost:7051
PRIV=http://localhost:7052

# Health
curl -f $BASE/health
curl -f $BASE/health/ready

# Profile
curl -s $BASE/v1/me/profile -H "Authorization: Bearer $TOKEN"

curl -s -X PUT $BASE/v1/me/profile \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"first_name":"Ada","last_name":"Lovelace"}'

# Account closure — immediate and irreversible, confirmation required
curl -s -X DELETE $BASE/v1/me/profile \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"confirm_email":"ada@example.com"}'

# Addresses
curl -s $BASE/v1/me/addresses -H "Authorization: Bearer $TOKEN"

curl -s -X POST $BASE/v1/me/addresses \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"line1":"1 Main St","city":"Columbus","region":"OH","postal_code":"43201","country":"US","is_default":true}'

# Preferences
curl -s -X PUT $BASE/v1/me/preferences \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"language":"en-US","timezone":"America/New_York","communication":{"email":true,"sms":false}}'

# Private plane — auth-api's sign-in hot path
curl -s "$PRIV/v1/accounts/credentials?email=ada@example.com" \
  -H "Authorization: Bearer $SVC_TOKEN"

curl -s $PRIV/v1/accounts/acct_abc123/settings -H "Authorization: Bearer $SVC_TOKEN"
```

Every public write route requires an `Idempotency-Key` header matching `^[A-Za-z0-9_\-]{8,64}$`.

---

## Documentation

| Document | Contents |
|---|---|
| `docs/PRD.md` | Product scope, capabilities, non-goals |
| `docs/sdd.md` | System design, critical paths, security posture, infrastructure, data strategy, entities, PII classification, retention, business invariants, event catalogue and change-capture derivation contract |
| `docs/adr/001-v1-target-state.md` | V1 decisions D0–D11 |
| `docs/adr/002-v2-target-state.md` | V2 scope: distributed idempotency, telemetry and metrics |
| `docs/adr/003-cloud-map-namespace-ownership.md` | Why this service owns `komodo.internal` |
| `docs/runbooks/incident-response.md` | Severity ladder and first response |
| `docs/runbooks/key-rotation.md` | Unsubscribe secret rotation (hard cutover) |
| `openapi.yaml` | Request/response contract and error catalogue |
| `TODO.md` | Open work, ranked, with blockers first |
