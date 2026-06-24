# High-Level Design — Komodo User API

> **Status:** Draft — V1 in progress.
> **Companions:** `PRD.md` (scope + data posture) · `../openapi.yaml` (contract source of truth) · `DATA-MODEL.md` (state) · `../README.md` (operations).

## 1. System Context

User-api is the **canonical data store for all user identity data** in Komodo. It is the record-of-origin for user profiles, passkey credential public keys, preferences, address books, and payment method references. Auth-api consumes it on the authentication hot path (credential resolution and passkey CRUD); payments-api and other services consume it for user-attributed data.

```
[System context diagram — TBD]
```

## 2. Access Planes

| | Public | Private |
|---|---|---|
| Exposure | Internet-facing (user-owned data routes) | VPC private subnet — internal service routes |
| Callers | UI/BFF, authenticated users | Komodo services (auth-api, payments-api) |
| Surface | profile CRUD, preferences, address book, GDPR export/delete | credential lookup, passkey CRUD, payment token read |
| Middleware posture | TBD (enterprise-standard chain from auth-api) | RequestID → Telemetry → Auth (svc-scoped bearer) on every route |

## 3. Component View

| Component | Responsibility |
|---|---|
| `cmd/public`, `cmd/private` | Entrypoints; public plane for user-facing routes, private plane for internal service routes (credential lookup, passkey CRUD, payment token reads) |
| `internal/api` | HTTP handlers: profile CRUD, passkey credentials, address book, payment methods, preferences, GDPR delete/export |
| `internal/db` | DynamoDB adapter: single-table access patterns against `komodo-users`; GSI queries |
| `internal/clients` | Outbound adapters: address-api (validation, V2), event bus (V2) |
| forge-sdk | `security/oauth`, `db/dynamodb`, `http/client`, logging/middleware |

## 4. Primary Flows

**Credentials lookup (auth-api hot path)** — `GET /v1/me/credentials?email=` → GSI1 Query on `EMAIL#<email>` → return `CredentialsResponse` (`UserId`, …). Errors: missing account → 401; lookup failure → 503. Target p99 ≤ 100ms end-to-end.

**Passkey credential CRUD (private, auth-api only)** — `GET /v1/users/{id}/passkeys` returns all credential records; `POST` stores a new COSE public key + metadata; `PATCH` updates sign count / last-used after each successful assertion; `DELETE` removes a credential. Called by auth-api during WebAuthn registration and assertion ceremonies.

**Profile CRUD** — create / get / update / delete on the `PROFILE` sort key. Delete triggers a Query + BatchDelete over the entire `USER#<id>` partition (GDPR erasure — no orphaned items).

**Address book** — list/create/update/delete on `ADDR#<addr_id>` sort keys. `is_default` enforcement is a service-layer concern (clear previous default on new default set).

**Payment method references** — write processor token on create; `token` field is never returned in responses. Payments-api reads via internal private route.

**Preferences** — full-replace `PUT` on the `PREFS` singleton sort key.

## 5. Data Layer

All durable user data lives in DynamoDB (`komodo-users` table) — single-table design. Full schema in `DATA-MODEL.md`. No ephemeral/TTL state owned by this service. Cache layer is a V2 consideration.

## 6. State

Full schema in `DATA-MODEL.md`. Configuration loaded from Secrets Manager at boot.

## 7. External Dependencies & Failure Posture

| Dependency | Used for | On failure |
|---|---|---|
| DynamoDB | All user data | Service unavailable (5xx); no caching fallback in V1 |
| auth-api (JWKS) | JWT verification for incoming requests | SDK caches keys by `kid`; short auth-api downtime tolerable |
| address-api | Address validation (V2) | Degraded: skip validation, store address as-is |
| payments-api (consumer) | Reads payment tokens via internal route | payments-api owns its own failure handling |
| Event bus (V2) | User lifecycle events | Non-fatal in V1 |

## 8. Deployment

- **V1:** EC2 docker-compose; DynamoDB via LocalStack locally.
- **V2:** ECS Fargate via `infra/deploy/cfn/`.
- Ports per enterprise allocation: TBD.

## 9. Observability & Operations

Structured logs with strict redaction (no PII in log values — user IDs only, partially redacted). `/health` liveness + `/health/ready` with DynamoDB connectivity check. V2: CloudWatch dashboards/alarms (credential lookup latency, error rates, DynamoDB throttling).

## 10. Security Architecture (threat → control)

| Threat | Control |
|---|---|
| Unauthorized profile access | Bearer token required on all routes; users can only access their own data |
| Payment token exfiltration | `token` field excluded from JSON serialization; internal route requires svc-scoped M2M token |
| PII in logs | Logging redaction: user IDs only, partially redacted |
| GDPR erasure bypass | `DeleteUser` uses Query + BatchDelete over entire `USER#<id>` partition — no orphaned items possible |
| Passkey key material on server | COSE public keys only stored; private keys never transmitted or stored |
| Credential enumeration | Credential lookup returns 401 (no account) vs. 503 (error) — no email oracle beyond that |

## 11. Open Design Work

- Ports per enterprise allocation — TBD.
- Public vs. private middleware chain — adopt enterprise-standard chain from auth-api (`cmd/public/main.go`).
- Cache layer (profile data) — V2 design.
- Event bus integration — V2 design; which events (user created, deleted, profile updated) and schema TBD.
- Admin list-all route — if added, GSI design required (current: Scan acceptable at low scale).
- `is_default` invariant enforcement — service-layer TODO (clear previous default on new default set for both addresses and payment methods).
