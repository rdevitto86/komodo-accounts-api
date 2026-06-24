# PRD — Komodo User API

> **Status:** Draft — V1 scope in progress.
> **Contract:** `openapi.yaml` is the source of truth for request/response shapes. This PRD is the source of truth for scope and data posture. Implementation sequencing lives in `TODO.md`.

## Mission

The User API is the **sole owner of user identity data** for Komodo: the canonical store for user profiles, passkey credential public keys, preferences, address books, and payment method references. It is the record-of-origin for every persistent user-attributed data point that is not a transient authentication artifact. Every design decision resolves ties in favor of data integrity and privacy, then latency, then cost.

## Goals

- Single source of truth for user identity — profile, credentials, preferences, and address/payment references; no other service duplicates this data.
- Serve auth-api's credential-resolution hot path at p99 ≤ 100ms end-to-end.
- Own passkey credential public keys on behalf of auth-api; private keys never exist server-side.
- Support GDPR/CCPA right-to-erasure: account deletion is a single operation that clears all user-partitioned data.
- Provide comprehensive user profile management with flexible account configurations.

## Non-Goals (explicit)

- **Authentication** — handled by `komodo-auth-api`; this service is a data store, not an authenticator.
- **Authorization / RBAC** — scope enforcement belongs to `komodo-access-api`; token issuance to `komodo-auth-api`.
- **Password storage** — Komodo is passwordless (passkeys + OTP); no password hashes or password fields exist anywhere in this service.
- **Loyalty program** — handled by `komodo-loyalty-api`.
- **Order history** — handled by `komodo-order-api`.
- **Address validation** — validation logic belongs to `komodo-address-api`; this service stores user-chosen addresses.

## Functional Requirements — V1

1. **User profile CRUD** — create, read, update, delete user profile records (`first_name`, `last_name`, `email`, `phone`, `avatar_url`).
2. **Passkey credential storage (V1 — auth-api dependency, DONE 2026-06-13)** — own WebAuthn credential records keyed to `USER#<id>`: credential ID, COSE public key, sign count, transports, AAGUID, created/last-used. CRUD on the private plane (`GET/POST /v1/users/{id}/passkeys`, `PATCH/DELETE /v1/users/{id}/passkeys/{credential_id}`), specced in `openapi.yaml`. Public keys only — passkey private keys never exist server-side.
3. **Credentials lookup contract (already consumed)** — `GET /v1/me/credentials?email=` returns the `CredentialsResponse` shape auth-api's generated client consumes (`UserId`, …), covered by `openapi.yaml`. This endpoint sits on the login hot path: auth-api maps lookup errors → 503 and missing accounts → 401, inside a ~100ms p99 end-to-end budget.
4. **Address book** — list, create, update, delete addresses per user; `is_default` flag; alias support.
5. **Payment method references** — store processor tokens (e.g. Stripe `pm_xxx`) as write-only references; `last4`, brand, and expiry surfaced for display; token never returned in API responses.
6. **User preferences** — full-replace (`PUT`) of language, timezone, communication, and marketing preference maps.
7. **Account settings management** — account verification status; user segmentation and tags.
8. **Account deletion (GDPR)** — `DELETE /me/profile` clears the entire `USER#<id>` DynamoDB partition in one Query + BatchDelete.
9. **Profile export (GDPR)** — produce a portable export of all user data on request.
10. **User activity tracking** — `created_at` / `updated_at` on profile; `last_used` on passkey credentials.
11. **M2M auth per ADR 001** — verify svc-scoped bearer JWTs locally (forge-sdk `auth.JWKSVerifier`); never call introspect on the hot path; obtain service tokens via `client_credentials` (`WithServiceAuth`).

## Security Requirements

- All routes require M2M or user bearer tokens (forge-sdk local verify). No unauthenticated routes.
- Payment processor tokens (`token` field) are stored but never returned via API; excluded from JSON serialization; accessible only by payments-api via internal route.
- Passkey credential material is COSE public keys only — private keys are never transmitted to or stored by any server.
- Logging: no PII in log values; user IDs only, partially redacted where needed for triage.
- GDPR/CCPA: account deletion must be complete and verifiable; no shadow records or orphaned items.

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

- **V1:** EC2 docker-compose (existing `deploy/` path).
- **V2:** ECS Fargate via `infra/deploy/cfn/`.
- Ports per enterprise allocation: TBD.

## Dependencies

- `komodo-auth-api` — caller for credential resolution and passkey credential CRUD; this service serves auth-api on the login hot path.
- `komodo-address-api` — address validation (user-api stores, address-api validates).
- `komodo-payments-api` — consumer of stored payment method tokens via internal route.
- DynamoDB (`komodo-users` table) — primary data store; single-table design.
- Cache for profile data (V2 consideration).
- Event bus for user lifecycle events (V2 consideration).

## Roadmap

- **Phase 1 — launch:** Basic profile management — user profile CRUD, credentials lookup, passkey credential storage. Definition of done: full credential-resolution and passkey CRUD flows green against LocalStack.
- **Phase 2:** Preferences and settings — user preferences, account settings, address book.
- **Phase 3:** Payment method references, profile export (GDPR).
- **Phase 4 — advanced:** User segmentation, activity tracking, event bus integration.

## Risks

- User data privacy breaches and GDPR/CCPA compliance — erasure must be complete and auditable; no orphaned items across the partition.
- Performance with large user base (10M+ accounts) — DynamoDB single-table design scales horizontally but hot partitions are possible with poor key distribution.
- Profile data inconsistency — no distributed transaction across user-api and downstream consumers; callers must tolerate eventual consistency.
- auth-api dependency risk — changes to the passkey credential or `CredentialsResponse` shape require coordinated updates across both services.
