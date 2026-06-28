# High-Level Design — Komodo Customer API

> **Status:** V1 in progress.
> **Companions:** `PRD.md` (scope, non-goals) · `../openapi.yaml` (contract source of truth) · `data-model.md` (schema spec) · `adr/001-v1-phased-plan.md` (phase rationale) · `../README.md` (operations).

---

## 1. System context

Customer-api is the canonical store for customer identity data: profiles, account settings, passkey public keys, preferences, address books, payment-method references, consent history. It is a data store, not an authenticator — auth-api owns token issuance and credential verification.

![System context](diagrams/system-context.png)

---

## 2. Access planes

| | Public | Private |
|---|---|---|
| Binary | `cmd/public` | `cmd/private` |
| Port | 7051 | 7052 |
| Exposure | Internet-facing via ALB + WAF | VPC-internal only (service mesh / private subnet) |
| Callers | UI/BFF, authenticated end users | Komodo services (auth-api, payments-api, order-api, communications-api) |
| Auth | User bearer JWT (RS256, JWKS verify) | Service bearer JWT (`client_credentials`, RS256) |
| Surface | profile, settings, addresses, payments (metadata only), preferences, consent, GDPR export/delete, unsubscribe verify | credentials read/write, passkey CRUD, payment-method full read (token visible), tag write ACL, unsubscribe-token mint |
| Rate limiting | Global + per-route (WAF + in-process) | Trusted callers; no public limiter |

---

## 3. Component view

| Component | Responsibility |
|---|---|
| `cmd/public/main.go`, `cmd/private/main.go` | Bootstrap, secrets load, dependency wiring, route registration, server start |
| `internal/api/*_handler.go` | HTTP handlers per resource (profile, settings, passkey, address, payment, preferences, consent, export, unsubscribe) |
| `internal/api/service.go` | Service-layer orchestration: invariants (`is_default` singleton, namespace ACL), cross-entity mutations, event-trigger conditions |
| `internal/db/user.go` | DynamoDB adapter: single-table access patterns, GSI1 queries, chunked `TransactWriteItems` |
| `internal/cache/cache.go` | In-process TTL cache (`sync.Map` + envelope, background evictor); profile + credentials cached 60s |
| `internal/models` | DTOs, error codes, JSON contracts |
| `komodo-forge-sdk-go` | `http/server`, `http/middleware` (auth, rate limit, request-id, CORS), `http/errors`, `aws/secrets-manager`, `aws/dynamodb`, `security/jwt`, `logging`, `api/handlers/health` |

---

## 4. Primary flows

### 4.1 Credentials lookup (auth-api hot path, p99 ≤ 100 ms)

![Credentials lookup flow](diagrams/flow-credentials-lookup.png)

Errors: missing account → `401`; lookup failure → `503`. Cache invalidation on credential update is omitted (60s eventual consistency acceptable; update path has only `customer_id`, not email).

### 4.2 Passkey CRUD (private, auth-api only)

`GET /v1/users/{id}/passkeys` lists all credentials for a customer. `POST` stores a new COSE public key + AAGUID + transports (`attribute_not_exists(SK)` guards against duplicate `credential_id`). `PATCH` updates `sign_count` and `last_used_at` after each successful assertion. `DELETE` removes a single credential. Public keys only — private keys never reach the server.

### 4.3 Profile CRUD

Create / read / update on `SK=PROFILE`. `created_at` is set once at insert; `updated_at` is server-stamped on every mutation. Email changes are detected by comparing old/new images and emit `customer.email_changed` via the CDC path (§6).

### 4.4 GDPR erasure

![GDPR erasure flow](diagrams/flow-gdpr-erasure.png)

Each chunked transaction is atomic; chunks across batches are not. Partitions >100 items log a warning. S3 export blobs are deleted in the same handler so the 7-day lifecycle window cannot leak data post-erasure.

### 4.5 GDPR profile export

`POST /v1/me/profile/export` → service collects all items under `PK=CUSTOMER#<id>` (redacting payment `token` and passkey `public_key`), writes one JSON blob to `s3://komodo-customer-exports-<env>/exports/<customer_id>/<export_id>.json`, returns a pre-signed URL (15-min TTL).

### 4.6 Public unsubscribe (stateless HMAC)

`POST /v1/unsubscribe` verifies `base64url(payload || HMAC-SHA256(secret, payload))` where `payload = {customer_id, channel, exp}`. On success, the handler records a `CONSENT#<channel>#<ts>` item with `action=opt_out`, `source=unsubscribe.token`, `source_ref=<token_id>`. No DynamoDB row for the token itself. Secret in Secrets Manager.

---

## 5. Data layer

DynamoDB single-table at `komodo-customers-<env>` plus S3 exports bucket. Full schema, key map, GSI projections, ID formats, and invariants in `data-model.md`. In-process TTL cache fronts profile (60 s) and credentials (60 s) reads only.

---

## 6. Cross-domain integration

### 6.1 Consumer map

| Consumer | Pattern | Surface |
|---|---|---|
| auth-api | Private REST (hot path) | `GET /v1/users/credentials`, `PUT /v1/users/{id}/credentials`, passkey CRUD |
| payments-api | Private REST | `GET /internal/v1/customers/{id}/payments` (token visible only here) |
| order-api | Private REST | `GET /internal/v1/customers/{id}/addresses`, `…/profile` |
| communications-api | Private REST + events | profile, consent, preferences reads; `POST /internal/v1/customers/{id}/unsubscribe-token` mint; subscribes to `customer.*` events |
| loyalty-api | Events + private REST | Subscribes to `customer.registered`, `customer.deleted`, `customer.status_changed`; writes `loyalty.*` tags via `PUT /v1/customers/{id}/settings/tags` |
| promotions-api | Events + private REST | Subscribes to `customer.consent_changed`, `customer.tags_changed`; writes `marketing.*` tags |
| insights-api | Events | Subscribes to all `customer.*` events |

### 6.2 Stream → events-api fan-out

![Stream to events-api fan-out](diagrams/event-fan-out.png)

Customer-api owns the stream ARN export only. Events-api owns CDC Lambda + SNS topic + SQS queues + DLQs. Envelope per `komodo-events-api/README.md`. No synchronous `POST /events` calls from customer-api.

### 6.3 Domain event catalogue

`source = "komodo-customer-api"`, `version = "1"` on all events. The CDC Lambda diffs OLD vs NEW images to derive change sets.

| Event | Trigger | Payload key fields |
|---|---|---|
| `customer.registered` | `PutItem PROFILE` | `customer_id`, `email`, `created_at` |
| `customer.deleted` | Final erasure transaction | `customer_id`, `deleted_at` |
| `customer.profile_updated` | `UpdateItem PROFILE` (excluding email/phone) | `customer_id`, `changed_fields[]`, `updated_at` |
| `customer.email_changed` | `UpdateItem PROFILE` where `email` differs | `customer_id`, `old_email`, `new_email`, `updated_at` |
| `customer.phone_changed` | `UpdateItem PROFILE` where `phone` differs | `customer_id`, `old_phone`, `new_phone`, `updated_at` |
| `customer.consent_changed` | `PutItem CONSENT#…` | `customer_id`, `channel`, `action`, `source`, `recorded_at` |
| `customer.preferences_updated` | `PutItem PREFS` | `customer_id`, `language`, `timezone`, `communication`, `updated_at` |
| `customer.status_changed` | `UpdateItem SETTINGS` where `status` differs | `customer_id`, `old_status`, `new_status`, `status_reason`, `status_changed_at` |
| `customer.tags_changed` | `UpdateItem SETTINGS` where `tags` differs | `customer_id`, `added[]`, `removed[]`, `updated_at` |
| `customer.passkey_added` | `PutItem PASSKEY#…` | `customer_id`, `credential_id`, `aaguid`, `transports`, `created_at` |
| `customer.passkey_removed` | `DeleteItem PASSKEY#…` | `customer_id`, `credential_id`, `removed_at` |

### 6.4 Tag namespace ACL

`PUT /v1/customers/{id}/settings/tags` accepts `{ add: [], remove: [] }`. Caller's service identity is resolved from the client-credentials JWT; the handler enforces tag prefix against an allowed-namespace map:

| Service | Allowed prefix |
|---|---|
| `loyalty-api` | `loyalty.*` |
| `marketing-api`, `promotions-api` | `marketing.*` |
| `customer-servicing-api` | `support.*` |
| `customer-api` (self / admin) | `system.*` |

Cross-namespace writes → `403 forbidden_namespace`.

---

## 7. Deployment

![Deployment topology](diagrams/deployment-topology.png)

| Concern | V1 posture |
|---|---|
| Compute | ECS Fargate, dual service (public + private), `deploy/cdk/main.ts` |
| Edge | ALB + WAF (`AWSManagedRulesCommonRuleSet`, `AWSManagedRulesKnownBadInputsRuleSet`, global rate 2000, per-path 200 on `/v1/profile/`, `/v1/addresses/`); WAF skipped in `dev` |
| Data | DynamoDB single-table + S3 exports; both created by this repo's CDK |
| Secrets | Single `AWS_SECRET_PATH` bundle loaded at boot via `awsSM.GetSecrets`; includes JWT verifier keys, unsubscribe HMAC secret |
| Image | Single container image; binary selected by `cmd/` path; healthcheck `/komodo -healthcheck` |
| Local | `docker-compose.yaml` with LocalStack DynamoDB; shared `komodo-network` |

---

## 8. Observability & operations

| Concern | V1 posture |
|---|---|
| Logging | Structured JSON via `forge-sdk/logging/runtime`. PII redacted — `customer_id` only in log values. Levels: `Info` (mutating success), `Warn` (auth fail, not-found), `Error` (5xx, DDB failure) |
| Health | `GET /health` (liveness), `GET /health/ready` with DynamoDB connectivity check (both planes) |
| Alarms (`stg`/`prod`) | 5xx count threshold 10/period; 404 count on `/v1/users/*` threshold 100/period; ALB p99 latency > 500 ms over 2 periods |
| Metrics namespace | `KomodoCustomer` |
| Log group | `/ecs/komodo-customer-api-<env>`, 30-day retention |
| Tracing | Not in V1 |

---

## 9. Security architecture

| Threat | Control |
|---|---|
| Unauthorized profile access | Bearer JWT required on all routes (RS256, JWKS-verified); `customer_id` from JWT subject, never from path param on `/me/*`; path/JWT precedence split (`userIDFromJWT`, `userIDFromPath`) |
| Payment token exfiltration | `token` field `json:"-"` and zeroed on every read path except `GET /internal/v1/customers/{id}/payments`; that route requires svc-scoped client-credentials JWT |
| PII in logs | Redaction policy enforced: only `customer_id` in structured fields; no `email`/`phone`/`name` ever logged |
| GDPR erasure incomplete | `Query` + chunked `TransactWriteItems` (≤100 per atomic batch); S3 export blobs deleted in same handler; warning logged on partitions >100 items |
| Account enumeration | `GET /v1/users/exists?email=` (intentional login-UI oracle) wrapped in per-IP 1 RPS / burst-5 limiter as an outer middleware; tighter than the global limiter |
| Credential enumeration | `GET /v1/users/credentials?email=` returns `401` on missing account vs `503` on lookup failure — no email oracle beyond presence/absence |
| Passkey key material on server | COSE public keys only stored; WebAuthn private keys never leave the authenticator |
| Tag namespace abuse | `PUT /…/settings/tags` rejects cross-namespace writes (`403 forbidden_namespace`); caller identity from client-credentials JWT, never trusted from request body |
| Stream event PII leakage | SNS topic + SQS queues are VPC-internal, IAM-gated per consumer; envelope `payload` may carry email/phone — consumer-side redaction required for downstream logs |
| Unsubscribe token forgery | HMAC-SHA256 over `customer_id || channel || exp` with secret from Secrets Manager; 30-day TTL; constant-time compare on verify |
| S3 export blob exposure | Bucket `BlockPublicAccess` (all four flags), `enforceSSL`, 7-day lifecycle expiry, pre-signed URLs 15-min TTL, deleted on GDPR erasure |
| Unauthenticated routes | None except `/health`, `/health/ready`, `/v1/unsubscribe` (HMAC-authenticated), `/v1/users/exists` (rate-limited oracle) |
