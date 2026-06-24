# Data Model — Komodo User API

> **Status:** Draft — V1 in progress. User-api holds **all durable user data** — profiles, passkey credential public keys, preferences, address books, and payment method references. Auth-api's ephemeral state (OTPs, challenges, JTI denylist) lives in its own Redis; this service owns none of it.

## DynamoDB — `komodo-users` (primary store)

### Table overview

| Property | Value |
|----------|-------|
| Table name | `komodo-users` (env var: `DYNAMODB_TABLE`) |
| Billing mode | PAY_PER_REQUEST (on-demand) |
| Primary key | `PK` (String) + `SK` (String) |
| TTL | None — user records are not time-limited |
| Streams | Not required at this stage |

Single-table design: all entity types (User, Passkey, Address, PaymentMethod, Preferences) live in one table, co-located under a user's partition key. This makes `DeleteUser` a single Query + BatchDelete with no cross-table coordination.

---

### Primary key design

| Entity | PK | SK |
|--------|----|----|
| User profile | `USER#<user_id>` | `PROFILE` |
| Passkey credential | `USER#<user_id>` | `PASSKEY#<credential_id>` |
| Address | `USER#<user_id>` | `ADDR#<address_id>` |
| Payment method | `USER#<user_id>` | `PAY#<payment_id>` |
| Preferences | `USER#<user_id>` | `PREFS` |

**Why this pattern:** `PK=USER#<id>` groups all items for a user under one partition key. A single `Query` on PK retrieves everything needed to close an account (`DeleteUser`). SK prefixes (`PASSKEY#`, `ADDR#`, `PAY#`, `PREFS`, `PROFILE`) allow range-key filtering with `begins_with`, so listing all addresses for a user is one `Query(begins_with(SK, "ADDR#"))` rather than a full-partition scan. `PROFILE` and `PREFS` are singleton SKs — only one of each per user.

---

### GSI definitions

#### GSI1 — email lookup

| Property | Value |
|----------|-------|
| Index name | `GSI1` |
| GSI1PK | `EMAIL#<email>` (normalised to lowercase) |
| GSI1SK | `PROFILE` (only the profile item carries this GSI key) |
| Projection | KEYS_ONLY + `user_id` |
| Purpose | Resolves a `user_id` from an email address for internal cross-service lookups (auth-api credential resolution, promotions-api correlation) |

Only the `PROFILE` item is written with `GSI1PK`/`GSI1SK`. Address, Passkey, PaymentMethod, and Preferences items do not carry these attributes — this is a **sparse GSI**.

**Forward-compatible usage:** The internal route `GET /users?email=<email>` is not yet implemented (not in `openapi.yaml`), but the GSI is included now because the cross-DB correlation requirement is already established in `apis/TODO.md`. The GSI has negligible storage cost — adding it later would require a table rebuild.

---

### Item schemas

#### User (PROFILE)

| Attribute | DynamoDB Type | Example | Notes |
|-----------|--------------|---------|-------|
| `PK` | S | `USER#usr_4a2b8c9d` | Partition key |
| `SK` | S | `PROFILE` | Sort key |
| `GSI1PK` | S | `EMAIL#alice@example.com` | GSI1 partition key; email lowercased |
| `GSI1SK` | S | `PROFILE` | GSI1 sort key |
| `user_id` | S | `usr_4a2b8c9d` | Duplicated for readability in Query results |
| `email` | S | `alice@example.com` | Lowercased at write time |
| `phone` | S | `+12125550100` | Optional; E.164 format |
| `first_name` | S | `Alice` | |
| `middle_initial` | S | `J` | Optional; max 1 char |
| `last_name` | S | `Smith` | |
| `avatar_url` | S | `https://cdn.example.com/…` | Optional |
| `created_at` | S | `2026-04-19T10:00:00Z` | RFC 3339; set once at creation |
| `updated_at` | S | `2026-04-19T11:23:00Z` | RFC 3339; updated on every mutation |

#### Passkey credential (PASSKEY#)

| Attribute | DynamoDB Type | Example | Notes |
|-----------|--------------|---------|-------|
| `PK` | S | `USER#usr_4a2b8c9d` | |
| `SK` | S | `PASSKEY#<credential_id>` | |
| `credential_id` | S | base64url-encoded | WebAuthn credential identifier |
| `public_key` | B | CBOR bytes | COSE public key — **private keys never stored** |
| `sign_count` | N | `42` | Updated on each successful assertion |
| `transports` | SS | `["internal", "hybrid"]` | AuthenticatorTransport values |
| `aaguid` | S | UUID | Authenticator AAGUID |
| `created_at` | S | `2026-06-13T08:00:00Z` | RFC 3339 |
| `last_used_at` | S | `2026-06-14T12:30:00Z` | RFC 3339; updated on each successful assertion |

#### Address (ADDR#)

| Attribute | DynamoDB Type | Example | Notes |
|-----------|--------------|---------|-------|
| `PK` | S | `USER#usr_4a2b8c9d` | |
| `SK` | S | `ADDR#addr_9f3c1a2b` | |
| `address_id` | S | `addr_9f3c1a2b` | |
| `alias` | S | `Home` | Optional friendly label |
| `line1` | S | `123 Main St` | |
| `line2` | S | `Apt 4B` | Optional |
| `city` | S | `New York` | |
| `state` | S | `NY` | |
| `zip_code` | S | `10001` | |
| `country` | S | `US` | ISO 3166-1 alpha-2 |
| `is_default` | BOOL | `true` | |

#### PaymentMethod (PAY#)

| Attribute | DynamoDB Type | Example | Notes |
|-----------|--------------|---------|-------|
| `PK` | S | `USER#usr_4a2b8c9d` | |
| `SK` | S | `PAY#pay_7d8e2f3a` | |
| `payment_id` | S | `pay_7d8e2f3a` | |
| `provider` | S | `stripe` | Payment processor name |
| `token` | S | `pm_1ABC…` | Processor token; **write-only — never returned in API responses** |
| `last4` | S | `4242` | |
| `brand` | S | `visa` | |
| `expiry_month` | N | `12` | 1–12 |
| `expiry_year` | N | `2028` | |
| `is_default` | BOOL | `false` | |

#### Preferences (PREFS)

| Attribute | DynamoDB Type | Example | Notes |
|-----------|--------------|---------|-------|
| `PK` | S | `USER#usr_4a2b8c9d` | |
| `SK` | S | `PREFS` | Singleton per user |
| `language` | S | `en-US` | BCP 47 language tag |
| `timezone` | S | `America/New_York` | IANA timezone |
| `communication` | M | `{"email": true, "sms": false}` | Map of channel → bool |
| `marketing` | M | `{"frequency": "weekly"}` | Map of key → string |

---

### Access pattern table

| Pattern | Operation | Key expression | Index |
|---------|-----------|----------------|-------|
| Get user by user_id | GetItem | `PK=USER#<id>`, `SK=PROFILE` | Table |
| Create user | PutItem | `PK=USER#<id>`, `SK=PROFILE` | Table |
| Update user profile | UpdateItem | `PK=USER#<id>`, `SK=PROFILE` | Table |
| Delete user (all items) | Query + BatchDelete | `PK=USER#<id>` | Table |
| Get user by email | Query | `GSI1PK=EMAIL#<email>`, `GSI1SK=PROFILE` | GSI1 |
| List passkeys for user | Query | `PK=USER#<id>` + `begins_with(SK, "PASSKEY#")` | Table |
| Get passkey by credential_id | GetItem | `PK=USER#<id>`, `SK=PASSKEY#<credential_id>` | Table |
| Create passkey credential | PutItem | `PK=USER#<id>`, `SK=PASSKEY#<credential_id>` | Table |
| Update passkey (sign count / last_used) | UpdateItem | `PK=USER#<id>`, `SK=PASSKEY#<credential_id>` | Table |
| Delete passkey credential | DeleteItem | `PK=USER#<id>`, `SK=PASSKEY#<credential_id>` | Table |
| List addresses for user | Query | `PK=USER#<id>` + `begins_with(SK, "ADDR#")` | Table |
| Get address by id | GetItem | `PK=USER#<id>`, `SK=ADDR#<addr_id>` | Table |
| Create address | PutItem | `PK=USER#<id>`, `SK=ADDR#<addr_id>` | Table |
| Update address | UpdateItem | `PK=USER#<id>`, `SK=ADDR#<addr_id>` | Table |
| Delete address | DeleteItem | `PK=USER#<id>`, `SK=ADDR#<addr_id>` | Table |
| List payment methods for user | Query | `PK=USER#<id>` + `begins_with(SK, "PAY#")` | Table |
| Get payment method by id | GetItem | `PK=USER#<id>`, `SK=PAY#<pay_id>` | Table |
| Upsert payment method | PutItem | `PK=USER#<id>`, `SK=PAY#<pay_id>` | Table |
| Delete payment method | DeleteItem | `PK=USER#<id>`, `SK=PAY#<pay_id>` | Table |
| Get preferences | GetItem | `PK=USER#<id>`, `SK=PREFS` | Table |
| Update preferences | PutItem (full replace) | `PK=USER#<id>`, `SK=PREFS` | Table |
| Delete preferences | DeleteItem | `PK=USER#<id>`, `SK=PREFS` | Table |

---

### Design decisions

**Why PutItem for Preferences instead of UpdateItem?**
Preferences is a shallow map-of-maps. A full replace (PutItem) is simpler, avoids complex attribute-path update expressions for nested maps, and the client always sends the full preferences object (`PUT /me/preferences` replaces the entire doc). If partial patching is added later, migrate to UpdateItem with a `SET` expression per top-level key.

**Why no GSI for listing all users?**
No admin list-all route exists. If one is added, a Scan is acceptable at low scale; a GSI would be designed at that point.

**Token on PaymentMethod — stored but never returned.**
The `token` field (e.g. Stripe `pm_xxx`) is stored so payments-api can execute charges on behalf of the user via the internal route `GET /users/{id}/payments`. It is excluded from JSON serialization via `json:"-"` in the model. The field is returned by internal routes as raw DynamoDB output to payments-api, which is the only consumer.

**Passkey credential — public key only.**
The `public_key` field stores the COSE-encoded public key only. WebAuthn ensures the private key never leaves the authenticator. `sign_count` must be updated on every successful assertion to detect cloned authenticators.

**`is_default` enforcement.**
The repo does not enforce "exactly one default" invariants at the DynamoDB layer — that is a service-layer concern. When `is_default=true` is set on a new address or payment method, the service is responsible for clearing the flag on any previously default item. This is currently a TODO in the service layer.

**TTL.**
No TTL attributes are defined on any user item. Account deletion is explicit (`DELETE /me/profile`), which runs a Query + BatchDelete over the entire `USER#<id>` partition.

---

## CloudFormation / LocalStack table definition

```yaml
Type: AWS::DynamoDB::Table
Properties:
  TableName: komodo-users
  BillingMode: PAY_PER_REQUEST
  AttributeDefinitions:
    - AttributeName: PK
      AttributeType: S
    - AttributeName: SK
      AttributeType: S
    - AttributeName: GSI1PK
      AttributeType: S
    - AttributeName: GSI1SK
      AttributeType: S
  KeySchema:
    - AttributeName: PK
      KeyType: HASH
    - AttributeName: SK
      KeyType: RANGE
  GlobalSecondaryIndexes:
    - IndexName: GSI1
      KeySchema:
        - AttributeName: GSI1PK
          KeyType: HASH
        - AttributeName: GSI1SK
          KeyType: RANGE
      Projection:
        ProjectionType: INCLUDE
        NonKeyAttributes:
          - user_id
```
