# PRD — Komodo Customer API

> **Status:** Draft — V1 scope in progress.
> **Contract:** `openapi.yaml` is the source of truth for request/response shapes. This PRD is the source of truth for scope and data posture. Implementation sequencing lives in `TODO.md`.

## Mission

The Customer API is the **sole owner of customer identity data** for Komodo: the canonical store for user profiles, passkey credential public keys, preferences, address books, and payment method references. It is the record-of-origin for every persistent user-attributed data point that is not a transient authentication artifact. Every design decision resolves ties in favor of data integrity and privacy, then latency, then cost.

## Goals

- Single source of truth for user identity — profile, credentials, preferences, and address/payment references; no other service duplicates this data.
- Serve auth-api's credential-resolution hot path at p99 ≤ 100ms end-to-end.
- Own passkey credential public keys on behalf of auth-api; private keys never exist server-side.
- Support GDPR/CCPA right-to-erasure: account deletion is a single operation that clears all user-partitioned data.
- Provide comprehensive user profile management with flexible account configurations.

## Non-Goals (explicit)

- **Authentication** — handled by `komodo-auth-api`; this service is a data store, not an authenticator.
- **Authorization / RBAC** — scope enforcement belongs to `komodo-access-api`; token issuance to `komodo-auth-api`.
- **Password storage as primary auth** — Komodo is passwordless-primary (passkeys + OTP); password is supported as a backup mode. Hashing stays in auth-api; this service stores the hash on the private plane only.
- **Loyalty program** — handled by `komodo-loyalty-api`.
- **Order history** — handled by `komodo-order-api`.
- **Address validation** — validation logic belongs to `komodo-address-api`; this service stores user-chosen addresses.

## Functional Requirements — V1

1. **Customer profile CRUD** — create, read, update, delete customer profile records (`first_name`, `last_name`, `email`, `phone`, `avatar_url`). Profile updates are **partial** (pointer-field DTO); absent fields are unchanged. `middle_initial` is **not** part of identity (if a future shipping label needs it, model it on `Address`).
2. **Passkey credential storage (DONE 2026-06-13)** — own WebAuthn credential records keyed to `CUSTOMER#<id>`: credential ID, COSE public key, sign count, transports, AAGUID, created/last-used. CRUD on the private plane (`GET/POST /v1/users/{id}/passkeys`, `PATCH/DELETE /v1/users/{id}/passkeys/{credential_id}`). Public keys only — passkey private keys never exist server-side.
3. **Credentials lookup contract** — `GET /v1/users/credentials?email=` returns the `CredentialsResponse` shape auth-api consumes. Login hot path: auth-api maps lookup errors → 503 and missing accounts → 401, inside a ~100 ms p99 end-to-end budget.
4. **Address book** — list, create, update, delete addresses per user; `is_default` flag (atomic via `TransactWriteItems`); alias support. `PUT` is **partial**.
5. **Payment method references** — store processor tokens (e.g. Stripe `pm_xxx`) as write-only references; `last4`, brand, and expiry surfaced for display; token never returned in API responses. `is_default` flag atomic via `TransactWriteItems`.
6. **Customer preferences** — `PUT` is **partial**. Fields: `language`, `timezone`, and `communication` (transactional opt-ins keyed by enum `{email, sms, push, postal}` → bool; unknown keys → 400). **No `marketing` map** — all marketing consent flows through ConsentLog.
7. **Account settings management** — verification status (`email_verified`, `phone_verified`), lifecycle `status` (`active | suspended | closed | pending_deletion`), namespaced `tags` (loyalty/marketing/support/system). Writes use **optimistic concurrency** (`version` attribute + ConditionExpression). `segments` map is **not** in V1 (no producer/consumer).
8. **Account closure & right-to-delete (US + GDPR)** — `DELETE /v1/me/profile` initiates a **30-day soft-delete window** (sets `Settings.status=pending_deletion`, emits `customer.deletion_requested`). `POST /v1/me/profile/restore` reverses inside the window. Hard erase runs at `status_changed_at + 30d` (Query + chunked `TransactWriteItems` ≤100 per atomic batch + S3 export blob deletion). Satisfies GDPR Art. 17, CCPA/CPRA, VCDPA, CPA, and other US state privacy laws as a superset.
9. **Data download & right-to-access (US + GDPR)** — `POST /v1/me/profile/export` writes a portable JSON blob (profile, settings, preferences, addresses, payment-method metadata with tokens redacted, consent history, passkey metadata with public keys redacted) to S3 and returns a 15-min pre-signed URL.
10. **Avatar upload** — `POST /v1/me/profile/avatar` returns a 15-min pre-signed S3 PUT URL (≤2 MB, `image/{jpeg,png,webp}` enforced via signed `Content-Type`). Client uploads direct to S3, then `PUT /v1/me/profile` with the resulting URL. New bucket `komodo-customer-avatars-<env>`.
11. **Public unsubscribe (CAN-SPAM/TCPA)** — `POST /v1/communications/unsubscribe` verifies a stateless HMAC token (`{customer_id, channel, exp, jti}`, 30-day TTL) and appends to ConsentLog. Mint endpoint `POST /internal/v1/customers/{id}/communications/unsubscribe-token` issued to comms-api.
12. **Customer activity tracking** — `created_at` / `updated_at` on every entity; `last_used_at` on passkey credentials; `status_changed_at` on settings.
13. **M2M auth per ADR 001** — verify svc-scoped bearer JWTs locally (forge-sdk `security/jwt`); never call introspect on the hot path; obtain service tokens via `client_credentials`.

## Security Requirements

- All routes require M2M or user bearer tokens (forge-sdk local verify). Exceptions: `/health`, `/health/ready`, `/v1/communications/unsubscribe` (HMAC-authenticated), `/v1/users/exists` (rate-limited oracle, 1 RPS/IP burst-5).
- Payment processor tokens (`token` field) are stored but never returned via API; excluded from JSON serialization; accessible only by payments-api via internal route.
- Passkey credential material is COSE public keys only — private keys are never transmitted to or stored by any server.
- Avatars bucket: BlockPublicAccess (all four flags), enforceSSL, SSE-S3; pre-signed PUT URLs only, 15-min TTL, server-side `Content-Type` constraint to `image/{jpeg,png,webp}`.
- Logging: no PII in log values; customer IDs only, partially redacted where needed for triage.
- US + GDPR right-to-delete: account closure must be complete and verifiable; no shadow records or orphaned items after hard erase. 30-day soft-delete window is the customer-recoverable phase.
- Optimistic concurrency on `AccountSettings` and `is_default` flips (Address, PaymentMethod) prevents cross-service write loss; 409 → caller refetch + retry.

## Non-Functional Requirements

| Metric | Target |
|---|---|
| Credentials lookup latency | p99 ≤ 100ms (auth-api hot path budget) |
| Profile retrieval latency | p95 ≤ 100ms |
| Profile update latency | p95 ≤ 200ms |
| User data accuracy | > 99.9% |
| Scale | 10M+ user accounts |
| Availability | 99.9% V1 |

## Deployment

ECS Fargate via CDK (`deploy/cdk/main.ts`); public port 7051, private port 7052.

## Dependencies

- `komodo-auth-api` — caller for credential resolution and passkey credential CRUD; this service serves auth-api on the login hot path.
- `komodo-address-api` — address validation (customer-api stores, address-api validates).
- `komodo-payments-api` — consumer of stored payment method tokens via internal route.
- `komodo-communications-api` — caller of `POST /internal/v1/customers/{id}/communications/unsubscribe-token`; recipient of `customer.*` events for opt-in/out state.
- DynamoDB (`komodo-customers-<env>` table) — primary data store; single-table design. **This service owns the CDK Table resource and its full spec** (PK/SK, GSI1, streams, PITR, KMS, deletion-protection).
- S3 `komodo-customer-exports-<env>` (export blobs, 7-day lifecycle) and `komodo-customer-avatars-<env>` (durable; no lifecycle expiry) — both owned by this service's CDK.
- Cache for hot reads — in-process TTL cache (profile + credentials, 60 s TTL, 100k entry cap, sample-and-drop eviction). Per-container; cross-container staleness bounded by TTL.
- Event bus for customer lifecycle events — DynamoDB Streams → events-api CDC Lambda (events-api owns the Lambda + SNS + SQS; this service exports only the stream ARN).

## Roadmap

V1 implementation phases are rationalized in `docs/adr/001-v1-target-state.md`
V2 implementation phases are rationalized in `docs/adr/002-v2-target-state.md`

**V2 deferred:** cursor pagination on list endpoints; multi-region cache (Redis L2); avatar transformation/resizing; shareable customer-owned lists (wishlists/registries).

## Risks

- User data privacy breaches and GDPR/CCPA compliance — erasure must be complete and auditable; no orphaned items across the partition.
- Performance with large user base (10M+ accounts) — DynamoDB single-table design scales horizontally but hot partitions are possible with poor key distribution.
- Profile data inconsistency — no distributed transaction across customer-api and downstream consumers; callers must tolerate eventual consistency.
- auth-api dependency risk — changes to the passkey credential or `CredentialsResponse` shape require coordinated updates across both services.
