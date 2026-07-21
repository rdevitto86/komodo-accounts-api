# Data Strategy — Komodo Accounts

> Physical schema, storage layout, and infrastructure details are maintained separately in the engineering data model specification. This document is the conceptual and governance view intended for product, compliance, legal, and senior engineering stakeholders.

---

## 1. Data Strategy Summary

The Accounts platform is the **authoritative system of record for customer identity** across Komodo. It owns the definitive version of who a customer is, how they authenticate, where they receive goods, how they pay, and how they have chosen to be contacted. When any other part of the business needs to confirm an identity fact, this platform is the source of truth.

Because it holds directly identifying and sensitive personal information, this platform also carries the organization's core **data-governance and privacy responsibilities** for customer records, including:

- Serving as the single, reconciled record of each customer's identity and account state.
- Honoring the customer's **right to access** their data (data export on request).
- Honoring the customer's **right to erasure** ("right to be forgotten") under **GDPR Article 17** and **CCPA** deletion rights.
- Maintaining a durable, tamper-evident **history of marketing consent** to demonstrate lawful basis for contact under **GDPR** and **CAN-SPAM / CASL**.
- Ensuring highly sensitive material (credentials, payment identifiers) is never exposed through customer-facing channels.

All customer data is encrypted both while stored and while moving between systems, and the record store is continuously backed up so that any point in recent history can be recovered.

---

## 2. Conceptual Data Entities

Each customer account is a single logical record composed of the following business entities. Every entity below belongs to exactly one customer and is never shared across customers.

| Entity | What it represents | Cardinality per customer |
|---|---|---|
| **Profile** | The customer's core identity: name, username, email, phone, avatar image, and which sign-in methods they use. The anchor all other entities attach to. | One |
| **Settings** | The account's operational state: whether email and phone are verified, whether the account is active/suspended/closed/pending deletion, and internal classification labels. | One |
| **Passkeys** | Registered passwordless sign-in devices (fingerprint readers, security keys, phones). Each is a device the customer has enrolled to prove their identity. | Zero or many |
| **Addresses** | The customer's saved shipping and billing addresses, one of which may be marked as the default. | Zero or many |
| **Payment Methods** | The customer's saved ways to pay, one of which may be marked as the default. Card display details are kept; the sensitive payment credential is handled as write-only (see §3). | Zero or many |
| **Preferences** | The customer's language, time zone, and their opt-in choices for **transactional** messaging (order, account, and security notices only — never marketing). | One |
| **Consent Records** | A permanent, append-only ledger of every marketing opt-in and opt-out the customer has made, with when, how, and where it originated. The legal evidence base for marketing contact. | Growing history |

**Design principle:** transactional messaging preferences and marketing consent are deliberately kept as separate entities and are never merged. A customer agreeing to receive order updates has *not* consented to marketing, and the two are governed by different rules.

---

## 3. Data Security & Privacy (PII Classification)

Customer data is classified by sensitivity, and each class is governed by an explicit handling rule. The guiding posture is: **the most sensitive material can be written and used internally, but can never be read back out through any customer-facing channel.**

| Data class | Examples | Handling rule |
|---|---|---|
| **Secret — write-only** | Stored password verifier; payment processor credential ("token") | Accepted and stored, but **never returned on any public route**. The password verifier is exposed only on a restricted internal identity route; the payment credential is exposed only on a restricted internal payments route. Both are stripped from every customer-facing response. |
| **Sensitive — restricted use** | Passkey device keys | Only the **public** half of each passkey is ever stored. The private key never leaves the customer's own device and is never transmitted to or held by Komodo. Even so, only the safe portions are surfaced to customers. |
| **Directly identifying** | Name, email, phone, addresses, avatar | Stored and returned to the authenticated owner only. Encrypted at rest and in transit. Subject to export and erasure obligations. |
| **Contextual / audit** | Consent origin data such as the network address and device signature recorded at opt-in time | Captured to prove lawful basis and stored in the consent ledger. Never used for any purpose other than compliance evidence. |
| **Non-sensitive display** | Card brand, last four digits, expiry | Safe to display so the customer can recognize their own saved method. Carries no ability to transact on its own. |

**Key guarantees in plain terms:**

- **Passwords are never returned on public routes.** The stored verifier can only be checked internally; it cannot be read back by any customer-facing request.
- **Payment credentials are write-only.** A saved payment method can be *used* internally but its sensitive credential can never be *read back* through a customer-facing channel.
- **Only public passkey keys are stored.** Komodo never possesses the secret that would let it impersonate the customer's device.

Password hashing is performed by the separate authentication platform; this platform only stores the result. It never generates or interprets the secret itself.

---

## 4. Data Lifecycle & Retention

Different categories of data have deliberately different lifespans, driven by either customer convenience or legal obligation.

| Data | Lifecycle rule | Business rationale |
|---|---|---|
| **Data exports** (right-to-access downloads) | Automatically and permanently deleted **7 days** after they are generated. | An export is a temporary convenience copy. Keeping it longer would create an unnecessary standing copy of the customer's full record. Download links themselves expire within minutes. |
| **Avatar images** | **Permanent** — retained for the life of the account and removed only when the account is fully erased. | The avatar is durable account content the customer expects to persist. |
| **Account erasure** (right-to-be-forgotten) | **30-day soft-delete window.** A deletion request immediately marks the account as *pending deletion* but wipes nothing. After 30 days, all customer data — records, exports, and avatars — is permanently and irreversibly destroyed. | Satisfies **GDPR Article 17** and **CCPA** deletion rights while protecting customers from accidental or malicious deletion. The customer can **restore** their account any time inside the 30 days; after that, recovery is impossible. |
| **Consent records** | Retained as a permanent, append-only history for the life of the account; removed only on full account erasure. | Provides durable legal evidence of marketing consent, as required to demonstrate lawful basis for contact. |
| **Identity and account records** | Retained for the life of the account. | These are the system of record. |

Every account record is continuously backed up so recent history can be recovered, and in production environments the stored data is protected against accidental deletion at the infrastructure level.

---

## 5. Core Business Invariants

These are the rules the platform enforces at all times, regardless of how a request arrives. They are stated here as business guarantees, independent of how they are implemented.

| Invariant | Guarantee in plain terms |
|---|---|
| **Exactly one default, always** | Within a customer's addresses (and separately, their payment methods), at most one item is the default. Switching the default is a single, all-or-nothing action: the old default is demoted and the new one promoted together, so a customer can never momentarily end up with **zero** defaults or **two** defaults. |
| **No lost updates on account state** | Multiple internal teams (loyalty, marketing, support) can update the same account. If two updates collide, the platform rejects the second rather than silently overwriting the first, forcing it to re-read the latest state and retry. Concurrent edits can never quietly clobber one another. |
| **Consent is append-only** | Marketing consent is recorded as a permanent ledger. Entries are never edited or deleted (except when the whole account is erased). The customer's current consent is simply the most recent entry per channel, and the full history remains provable. |
| **Marketing and transactional are never conflated** | Transactional messaging opt-ins (order, account, security) and marketing consent are governed separately and are never reconciled into a single flag. Consenting to one is never treated as consenting to the other. |
| **Deletion is reversible for 30 days, then absolute** | A deletion request never destroys data immediately. It is fully reversible for 30 days via account restoration, and irreversible thereafter — at which point every trace of the customer, including exports and avatars, is destroyed. |
| **Sensitive credentials are one-way** | Password verifiers and payment credentials can be written and used internally but can never be read back through customer-facing channels. |
| **Only recognized contact channels are accepted** | Messaging and consent choices are validated against the approved set of channels (email, SMS, push, postal). Any unrecognized channel is rejected outright. |
| **Classification labels stay within their owner** | Internal account labels are namespaced by owning team (loyalty, marketing, support, system), capped in number and length, and one team can never write into another team's namespace. |
| **Unsubscribe links are self-contained and time-limited** | One-click unsubscribe links carry their own tamper-proof proof of authenticity and expire after 30 days. No stored record is needed to honor them, and a tampered or expired link is rejected. |

---

## Appendix — Terminology for Non-Technical Readers

| Term | Plain meaning |
|---|---|
| **System of record / source of truth** | The one authoritative place a fact is stored; if other systems disagree, this one wins. |
| **PII** | Personal information that can identify a specific individual. |
| **Right to access** | A customer's legal right to obtain a copy of the data held about them. |
| **Right to erasure / right to be forgotten** | A customer's legal right to have their data deleted. |
| **Soft delete** | Marking data for deletion but not destroying it yet, allowing recovery within a grace period. |
| **Append-only ledger** | A record that can only be added to, never edited or deleted — so its history is trustworthy. |
| **Write-only** | Data that can be stored and used by the system but never read back out. |
