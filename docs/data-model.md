# Data Model — Komodo Customer API

> **Status:** V1 in progress. Customer-api is the sole owner of customer identity data: profiles, account settings, passkey credential public keys, preferences, address books, payment-method references, and consent history. Profile exports (GDPR) are written to S3; unsubscribe tokens are stateless HMAC and not persisted.

---

## 1. DynamoDB — `komodo-customers-<env>`

### 1.1 Table config

> **Ownership:** the CDK Table resource itself is the schema source of truth. `deploy/cdk/main.ts` creates the table via `new dynamodb.Table(...)` — **never** `Table.fromTableName(...)`. Any future schema change starts in this document and the CDK in the same PR.

| Property | Value |
|---|---|
| Table name | `komodo-customers-<env>` (env var `DYNAMODB_TABLE`) |
| Billing mode | `PAY_PER_REQUEST` |
| Partition key (HASH) | Attribute `PK`, type `String` |
| Sort key (RANGE) | Attribute `SK`, type `String` |
| Streams | `NEW_AND_OLD_IMAGES` — consumed by `komodo-events-api` CDC Lambda |
| TTL attribute | None |
| Point-in-Time Recovery | Enabled (all envs) |
| Encryption at rest | AWS-managed KMS (`aws/dynamodb`) |
| Encryption in transit | TLS (VPC endpoints in `stg`/`prod`) |
| Deletion protection | On in `stg`/`prod`; off in `dev` |
| Removal policy | `RETAIN` in `stg`/`prod`; `DESTROY` in `dev` |

### 1.2 Primary key map

| Entity | PK | SK |
|---|---|---|
| CustomerProfile | `CUSTOMER#<customer_id>` | `PROFILE` |
| AccountSettings | `CUSTOMER#<customer_id>` | `SETTINGS` |
| Passkey credential | `CUSTOMER#<customer_id>` | `PASSKEY#<credential_id>` |
| Address | `CUSTOMER#<customer_id>` | `ADDR#<address_id>` |
| Payment method | `CUSTOMER#<customer_id>` | `PAY#<payment_id>` |
| Preferences | `CUSTOMER#<customer_id>` | `PREFS` |
| Consent event | `CUSTOMER#<customer_id>` | `CONSENT#<channel>#<recorded_at>` |

Example values: `PK = "CUSTOMER#cust_2KqA8x3pZ4MqYZbCq7H8Vk7Lq3p"`, `SK = "ADDR#addr_2Lp7n9…"`, `SK = "CONSENT#email#2026-04-19T10:00:00.123Z"`.

### 1.3 GSI1 — email lookup (sparse, PROFILE only)

| Property | Value |
|---|---|
| Index name | `GSI1` |
| `GSI1PK` | `EMAIL#<email>` (lowercased) — type `String` |
| `GSI1SK` | `PROFILE` — type `String` |
| Projection | `INCLUDE` with non-key `customer_id` |

Example: `GSI1PK = "EMAIL#alice@example.com"`, `GSI1SK = "PROFILE"` → `{ customer_id: "cust_…" }`.

DynamoDB does not support modifying a GSI projection after creation — widening means dropping and recreating the GSI. Decide projection scope at table-create time.

---

## 2. Item schemas

### 2.1 CustomerProfile (`SK=PROFILE`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `PROFILE` | Singleton |
| `GSI1PK` | S | `EMAIL#<email>` (lowercased) | |
| `GSI1SK` | S | `PROFILE` | |
| `customer_id` | S | `cust_<KSUID>` | Also projected on GSI1 |
| `email` | S | RFC 5322 (lowercased at write) | |
| `phone` | S | E.164 | Optional |
| `first_name` | S | string | |
| `last_name` | S | string | |
| `username` | S | string | Set at create; not mutable via update |
| `avatar_url` | S | URL | Optional |
| `auth_methods` | SS | subset of `[password, passkey, otp, google, apple]` | Defaults to `[]` |
| `password_hash` | S | Argon2id encoded | Optional. `json:"-"` on public; surfaced only on private `GET /v1/users/credentials` |
| `created_at` | S | RFC 3339 | Set once |
| `updated_at` | S | RFC 3339 | Server-stamped on every mutation |

### 2.2 AccountSettings (`SK=SETTINGS`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `SETTINGS` | Singleton |
| `email_verified` | BOOL | | |
| `email_verified_at` | S | RFC 3339 | Present only when `email_verified=true` |
| `phone_verified` | BOOL | | |
| `phone_verified_at` | S | RFC 3339 | Present only when `phone_verified=true` |
| `status` | S | enum `active \| suspended \| closed \| pending_deletion` | `pending_deletion` set by `DELETE /v1/me/profile`; hard-erase worker runs at `status_changed_at + 30d` |
| `status_reason` | S | free text, ≤128 chars | Present when `status != active` |
| `status_changed_at` | S | RFC 3339 | Also drives the soft-delete window timer |
| `tags` | SS | namespaced (see §5) | ≤20 per customer |
| `version` | N | uint64, starts at 1 | Optimistic concurrency — `UpdateSettings` / `UpdateSettingsTags` use `ConditionExpression #v = :expected` + `SET #v = :v + 1`; 409 on conflict |
| `created_at` | S | RFC 3339 | |
| `updated_at` | S | RFC 3339 | |

### 2.3 Passkey credential (`SK=PASSKEY#<credential_id>`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `PASSKEY#<credential_id>` | |
| `credential_id` | S | base64url | WebAuthn credential identifier |
| `public_key` | B | COSE-encoded bytes | Public key only — private keys never stored |
| `sign_count` | N | uint32 | Updated on every successful assertion |
| `transports` | SS | subset of `[internal, hybrid, usb, nfc, ble]` | |
| `aaguid` | S | UUID | |
| `created_at` | S | RFC 3339 | |
| `last_used_at` | S | RFC 3339 | Updated on every successful assertion |

### 2.4 Address (`SK=ADDR#<address_id>`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `ADDR#addr_…` | |
| `address_id` | S | `addr_<KSUID>` | |
| `alias` | S | string | Optional |
| `line1` | S | string | |
| `line2` | S | string | Optional |
| `city` | S | string | |
| `state` | S | string | Optional outside US/CA |
| `zip_code` | S | string | Optional outside US |
| `country` | S | ISO 3166-1 alpha-2 | |
| `is_default` | BOOL | | Service-layer singleton (see §5) |
| `created_at` | S | RFC 3339 | |
| `updated_at` | S | RFC 3339 | |

### 2.5 PaymentMethod (`SK=PAY#<payment_id>`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `PAY#pay_…` | |
| `payment_id` | S | `pay_<KSUID>` | |
| `provider` | S | enum `stripe \| …` | |
| `token` | S | processor token | Write-only — zeroed on all read paths; raw only via internal route |
| `last4` | S | 4-digit string | |
| `brand` | S | string | |
| `expiry_month` | N | 1–12 | |
| `expiry_year` | N | uint16 | |
| `is_default` | BOOL | | Service-layer singleton (see §5) |
| `created_at` | S | RFC 3339 | |
| `updated_at` | S | RFC 3339 | |

### 2.6 Preferences (`SK=PREFS`)

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `PREFS` | Singleton |
| `language` | S | BCP 47 (`en-US`) | |
| `timezone` | S | IANA (`America/New_York`) | |
| `communication` | M | `string → bool` | Keys enum-validated against `{email, sms, push, postal}` on write; unknown key → 400. Transactional opt-in only (see §5). |
| `created_at` | S | RFC 3339 | |
| `updated_at` | S | RFC 3339 | |

> **Removed:** `Preferences.marketing` (`M`, `string → string`). All marketing consent flows through `ConsentLog` — `Preferences` holds only transactional channel opt-ins.

### 2.7 ConsentLog (`SK=CONSENT#<channel>#<recorded_at>`)

Append-only. Latest record per channel is current state.

| Attribute | Type | Format / example | Notes |
|---|---|---|---|
| `PK` | S | `CUSTOMER#cust_…` | |
| `SK` | S | `CONSENT#email#2026-04-19T10:00:00.123Z` | `<channel>` ∈ `email`, `sms`, `push`, `postal`; ts RFC 3339 with ms |
| `channel` | S | enum | |
| `action` | S | enum `opt_in \| opt_out` | |
| `source` | S | enum `customer.preference_update \| unsubscribe.token \| admin.action \| auto.bounce_handling \| auto.import` | |
| `source_ref` | S | string | Optional — token id, admin user id, import job id |
| `ip_address` | S | IPv4/IPv6 | Optional |
| `user_agent` | S | string, ≤256 chars | Optional |
| `recorded_at` | S | RFC 3339 | |

---

## 3. ID formats

| ID | Format | Library |
|---|---|---|
| `customer_id` | `cust_<KSUID>` (32 chars) | `github.com/segmentio/ksuid` |
| `address_id` | `addr_<KSUID>` (32 chars) | same |
| `payment_id` | `pay_<KSUID>` (31 chars) | same |
| `credential_id` | base64url (WebAuthn-issued) | external |

---

## 4. S3 buckets

### 4.1 `komodo-customer-exports-<env>` — data download (right-to-access) blob store

| Property | Value |
|---|---|
| Bucket name | `komodo-customer-exports-<env>` |
| Object key | `exports/<customer_id>/<export_id>.json` (`export_id` = KSUID) |
| Encryption | SSE-S3 |
| Public access | Blocked (all four `BlockPublicAccess` flags) |
| TLS | Enforced via bucket policy |
| Versioning | Off |
| Lifecycle | Expire objects after 7 days |
| Removal policy | `RETAIN` in `stg`/`prod`; `DESTROY` in `dev` |
| Pre-signed URL TTL | 15 minutes |
| IAM | Public task role: `PutObject`, `DeleteObject`, `GetObject` (pre-signs both directions). Hard-erase worker: `DeleteObject` under `exports/<customer_id>/`. |

### 4.2 `komodo-customer-avatars-<env>` — profile avatar store

| Property | Value |
|---|---|
| Bucket name | `komodo-customer-avatars-<env>` |
| Object key | `<customer_id>/<ksuid>.{jpg\|png\|webp}` |
| Encryption | SSE-S3 |
| Public access | Blocked (all four `BlockPublicAccess` flags) |
| TLS | Enforced via bucket policy |
| Versioning | Off |
| Lifecycle | None (avatars are durable; pruned only on account hard-erase) |
| Removal policy | `RETAIN` in `stg`/`prod`; `DESTROY` in `dev` |
| Pre-signed URL TTL | 15 minutes (PUT and GET) |
| Upload constraints | ≤2 MB, `Content-Type ∈ {image/jpeg, image/png, image/webp}` enforced via signed headers |
| IAM | Public task role: `PutObject` + `GetObject` via presign. Hard-erase worker: `DeleteObject` under `<customer_id>/`. |

---

## 5. Invariants

- **Payment `token`** — excluded from all read-path responses (`json:"-"` and zeroed in list paths). Available raw only on `GET /internal/v1/customers/{id}/payments`.
- **`password_hash`** — excluded from all public responses (`json:"-"`). Surfaced only on private `GET /v1/users/credentials`. Hashing (Argon2id) is auth-api's responsibility; customer-api only stores.
- **`is_default`** (Address, PaymentMethod) — at most one item per category per customer has `is_default=true`. Enforced in the service layer, not at the DB.
- **`Preferences.communication`** — transactional opt-in only (account, order, security). All marketing opt-in lives in `ConsentLog`. The two are never reconciled.
- **`ConsentLog`** — append-only. No `UpdateItem` or `DeleteItem` outside of full-account erasure.
- **Tag namespace** — `<owner>.<tag>` where `<owner>` ∈ `loyalty`, `marketing`, `support`, `system`. Constraints: ≤32 chars, charset `[a-z0-9._]`, ≤20 tags per customer. Cross-namespace writes rejected at the handler.
- **Account closure & right-to-delete** — two-phase. (1) `DELETE /v1/me/profile` flips `Settings.status=pending_deletion` only; no data wiped. (2) Hard-erase worker at `status_changed_at + 30d` runs `Query(PK=CUSTOMER#<id>)` + chunked `TransactWriteItems` (≤100 per batch) + `DeleteObjects` under `exports/<customer_id>/` and `<customer_id>/` (avatars). `POST /v1/me/profile/restore` cancels inside the window.
- **Optimistic concurrency on AccountSettings** — every write to `SK=SETTINGS` carries a `version` attribute; mutations use `ConditionExpression #v = :expected` and `SET #v = :v + 1`. 409 on conflict so cross-service writers (loyalty, marketing, support) refetch + retry instead of clobbering.
- **`is_default` atomic flip** — Address + PaymentMethod default toggles are a single `TransactWriteItems` (demote-old + promote-new). Never list-then-loop.
- **`Preferences.communication` enum** — keys validated against `{email, sms, push, postal}` on write; unknown keys → 400. Same enum as `ConsentLog.channel`.
- **`UnsubscribeToken`** — stateless HMAC (`base64url(payload || HMAC-SHA256(secret, payload))`, `payload = {customer_id, channel, exp}`, 30-day TTL). Not a DynamoDB entity. Secret in Secrets Manager at `/komodo/<env>/customer-api/unsubscribe-token-secret`.
