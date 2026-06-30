# Domain Event Catalogue — Komodo Customer API

## Envelope

All events share this envelope structure.

| Field | Type | Value |
|---|---|---|
| `id` | string | KSUID, unique per event |
| `type` | string | Event type string (e.g. `customer.registered`) |
| `version` | string | `"1"` for all V1 events |
| `source` | string | `"komodo-customer-api"` |
| `occurred_at` | string | RFC 3339 UTC timestamp derived from the DynamoDB stream record |
| `payload` | object | Event-specific object; see per-type sections below |

---

## Event types

### customer.registered

**Trigger:** A new `SK=PROFILE` item appears in the stream with no OLD image (`eventName=INSERT`).

**CDC derivation:** No diff required. The NEW image provides all profile fields directly.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | `cust_<KSUID>` |
| `email` | string | Lowercased RFC 5322 |
| `username` | string | |
| `first_name` | string | |
| `last_name` | string | |
| `auth_methods` | string[] | Subset of `[password, passkey, otp, google, apple]` |
| `created_at` | string | RFC 3339 |

---

### customer.deleted

**Trigger:** A `SK=PROFILE` item is removed from the stream (`eventName=REMOVE`) as part of a hard-erase operation.

**CDC derivation:** No diff required. The OLD image provides the customer identifier. The Lambda emits this event on the PROFILE row removal only; PASSKEY and other row removals under the same partition during a hard-erase do not produce additional events.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | `cust_<KSUID>` from OLD image |

---

### customer.profile_updated

**Trigger:** An existing `SK=PROFILE` item is modified (`eventName=MODIFY`) and at least one attribute other than `email` or `phone` changed between OLD and NEW images.

**CDC derivation:** The Lambda diffs OLD and NEW image attributes. Each attribute key whose value changed is included in `changed_fields`. Internal DynamoDB keys (`PK`, `SK`, `GSI1PK`, `GSI1SK`) are excluded. When the same MODIFY record also contains an `email` or `phone` change, the Lambda emits `customer.email_changed` or `customer.phone_changed` alongside this event.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `changed_fields` | string[] | Attribute names that differ between OLD and NEW image, excluding `email`, `phone`, and internal keys |
| `updated_at` | string | RFC 3339; from NEW image |

---

### customer.email_changed

**Trigger:** A `SK=PROFILE` MODIFY event where the `email` attribute differs between OLD and NEW images.

**CDC derivation:** Lambda compares `OLD.email` vs `NEW.email` and emits this event when they differ. Emitted alongside `customer.profile_updated` when other non-email attributes also changed in the same record.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `old_email` | string | Lowercased RFC 5322; from OLD image |
| `new_email` | string | Lowercased RFC 5322; from NEW image |

---

### customer.phone_changed

**Trigger:** A `SK=PROFILE` MODIFY event where the `phone` attribute differs between OLD and NEW images, including cases where phone is added for the first time or removed.

**CDC derivation:** Lambda compares `OLD.phone` vs `NEW.phone` and emits this event when they differ. `old_phone` is omitted when no phone was previously set; `new_phone` is omitted when phone is cleared. Emitted alongside `customer.profile_updated` when other non-phone attributes also changed.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `old_phone` | string | E.164; from OLD image; omitted if previously unset |
| `new_phone` | string | E.164; from NEW image; omitted if cleared |

---

### customer.consent_changed

**Trigger:** A new `SK=CONSENT#<channel>#<recorded_at>` item is inserted (`eventName=INSERT`). ConsentLog is append-only; MODIFY and REMOVE events for consent items do not occur outside of full hard-erase.

**CDC derivation:** No diff required. The NEW image provides all consent fields directly.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `channel` | string | `email`, `sms`, `push`, or `postal` |
| `action` | string | `opt_in` or `opt_out` |
| `source` | string | `customer.preference_update`, `unsubscribe.token`, `admin.action`, `auto.bounce_handling`, or `auto.import` |
| `source_ref` | string | Optional; token id, admin user id, or import job id |
| `recorded_at` | string | RFC 3339 |

---

### customer.preferences_updated

**Trigger:** An existing `SK=PREFS` item is modified (`eventName=MODIFY`).

**CDC derivation:** The NEW image provides the full preferences state. Consumers treat the payload as a snapshot of current preferences rather than a delta.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `language` | string | BCP 47 (e.g. `en-US`) |
| `timezone` | string | IANA timezone identifier (e.g. `America/New_York`) |
| `communication` | object | `string → bool` map; keys ∈ `{email, sms, push, postal}` |
| `updated_at` | string | RFC 3339; from NEW image |

---

### customer.status_changed

**Trigger:** A `SK=SETTINGS` MODIFY event where the `status` attribute differs between OLD and NEW images.

**CDC derivation:** Lambda compares `OLD.status` vs `NEW.status` and emits this event only when they differ.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `old_status` | string | From OLD image; `active`, `suspended`, `closed`, or `pending_deletion` |
| `new_status` | string | From NEW image |
| `status_reason` | string | From NEW image; present when `new_status != active` |
| `status_changed_at` | string | RFC 3339; from NEW image |

---

### customer.tags_changed

**Trigger:** A `SK=SETTINGS` MODIFY event where the `tags` string set attribute differs between OLD and NEW images.

**CDC derivation:** Lambda computes the symmetric diff of `OLD.tags` and `NEW.tags`. Tags present in NEW but not OLD populate `added`; tags present in OLD but not NEW populate `removed`. Both arrays are always present; each is empty when there is no movement in that direction.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `added` | string[] | Tag values added; empty array if none |
| `removed` | string[] | Tag values removed; empty array if none |

---

### customer.passkey_added

**Trigger:** A new `SK=PASSKEY#<credential_id>` item is inserted (`eventName=INSERT`).

**CDC derivation:** No diff required. The NEW image provides all passkey fields directly. `public_key` bytes are never included in the event payload.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `credential_id` | string | base64url WebAuthn credential identifier |
| `aaguid` | string | UUID |
| `transports` | string[] | Subset of `[internal, hybrid, usb, nfc, ble]` |
| `created_at` | string | RFC 3339 |

---

### customer.passkey_removed

**Trigger:** A `SK=PASSKEY#<credential_id>` item is removed (`eventName=REMOVE`) via an explicit delete — not during a full hard-erase (see `customer.deleted`).

**CDC derivation:** No diff required. The OLD image provides the credential identifier.

| Field | Type | Notes |
|---|---|---|
| `customer_id` | string | |
| `credential_id` | string | base64url; from OLD image |

---

## CDC Lambda notes

The CDC Lambda is owned by `komodo-events-api`. This repo exports the stream ARN as the CloudFormation output `${env}-CustomersTableStreamArn`. The following notes define the derivation contract.

- **Stream view:** the DynamoDB table is configured with `NEW_AND_OLD_IMAGES`, guaranteeing that both OLD and NEW images are present on all MODIFY and REMOVE records.
- **`changed_fields` derivation:** for `customer.profile_updated`, the Lambda iterates all attribute keys present in either image and collects any key whose value differs. DynamoDB internal keys (`PK`, `SK`, `GSI1PK`, `GSI1SK`) are excluded from this set.
- **Email and phone co-emission:** a single PROFILE MODIFY record that changes `email` (or `phone`) alongside other attributes causes the Lambda to emit both the dedicated change event and `customer.profile_updated`. Both events share the same `occurred_at` timestamp from the stream record.
- **`customer_id` extraction:** derived from the `PK` attribute by stripping the `CUSTOMER#` prefix. Present on OLD image for REMOVE records, NEW image for INSERT records, and either image for MODIFY records.
