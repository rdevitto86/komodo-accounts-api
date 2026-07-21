# Product Requirements Document (PRD) - Komodo Accounts API

## Owners

- **Author(s):** Komodo
- **Status:** Draft — V1 scope in progress

---

## 1. Core Mission

### 1.1 Problem Statement

This product is the single, authoritative place where Komodo stores everything that identifies a customer's account — their profile details, the public half of their passwordless sign-in credentials, their preferences, saved addresses, and references to saved payment methods. If it's a piece of account information meant to persist over time (as opposed to a short-lived sign-in artifact), it lives here and nowhere else duplicates it. Every decision made in building this product prioritizes protecting and correctly storing that data first, then how quickly customers can retrieve it, then cost.

### 1.2 Goals & Success Metrics

- Single source of truth for account identity — profile, credentials, preferences, and saved addresses/payment references; no other system keeps its own copy.
- Store the public half of each customer's passwordless sign-in credential on behalf of the login system; the private half never touches Komodo's servers.
- Give customers full, flexible control over their own profile and account setup.
- Fast, reliable retrieval of account data.
- Privacy-first design: account data is protected at rest and access to it is tightly controlled.
- Support the customer's legal right to be forgotten under Europe's GDPR and California's consumer privacy law: closing an account is a single action that fully clears every piece of data tied to it.

### 1.3 Non-Goals

- **Signing customers in** — handled by the login system; this product is a data store, not a sign-in system.
- **Deciding what a signed-in customer or service is allowed to do** — that belongs to Komodo's permissions system; issuing sign-in tokens belongs to the login system.
- **Storing passwords as the primary way to sign in** — Komodo's primary sign-in method is passwordless (device-based credentials plus one-time codes); password sign-in exists only as a backup. Turning a password into a securely stored value is the login system's job; this product only stores that value, and keeps it walled off from systems that don't need it.
- **The loyalty program** — a separate system.
- **Order history** — a separate system.
- **Checking that a submitted address is real** — a separate address-validation system does that; this product stores whatever address the customer chooses to save, as submitted.

---

## 2. Functional Capabilities

### 2.1 Key Actions & System Rules

Unless noted otherwise, changes to any of the records below are partial — a customer only needs to send the fields they're changing; everything left out stays as it was.

#### A. Customer Identity & Profile

- **Account profile** — customers can view and update their core profile details (name, email, phone, profile photo reference, and similar identity fields).
- **Profile photo upload** — customers can upload a photo through a time-limited, single-use upload link. See §3.2 for the size, format, and storage protections that apply.

#### B. Saved Commerce Data

- **Address book** — customers can maintain multiple saved addresses, with exactly one marked as the default at any time, and each optionally given a friendly label (e.g. "Home", "Work").
- **Saved payment methods** — customers can maintain multiple saved payment methods, each shown only as a masked, display-safe summary (card type and last four digits); one method can be marked as the default. See §3.2 for how the underlying card data is protected.

#### C. Preferences & Account Settings

- **Communication & locale preferences** — customers can set their language, timezone, and transactional communication choices across a defined set of communication channels; a request naming a channel outside that set is rejected. Marketing opt-in/opt-out is tracked separately, in the consent log described in the appendix, not as part of this preference set.
- **Account settings** — tracks verification status (whether email and phone have been confirmed) and the account's lifecycle state (a defined set of states such as active, closed, and pending deletion), plus internal labels ("tags") other Komodo systems attach to the account. Each label category is restricted to the one system that owns it, so one team can't overwrite another's labels.

#### D. Passwordless Sign-in Credentials

Stores the public half of each device-based sign-in credential a customer registers, on behalf of the login system, and serves the fast, purpose-built lookup the login system calls during every sign-in attempt to complete authentication. That lookup sits on the critical sign-in path and carries the tightest speed target in this product (see §3.1). See §3.2 for how the private half of the credential is protected.

#### E. Privacy & Compliance

- **Account closure & right to erasure** — closing an account starts a 30-day recoverable window during which the customer can reverse the closure; once the window passes, all data tied to the account is permanently and completely removed. This satisfies the erasure rights guaranteed under Europe's GDPR, California's consumer privacy law, and several other US state privacy laws.
- **Data download & right to access** — customers can request a complete, portable copy of their account data (profile, settings, preferences, addresses, payment method summaries, consent history, and credential records with sensitive material removed), delivered as a time-limited download link.
- **Public unsubscribe** — a customer can opt out of a communications channel using a limited-purpose, time-limited link, consistent with federal messaging laws (CAN-SPAM and TCPA). Komodo's messaging system requests these links on the customer's behalf.

### 2.2 Inputs & Expected Outputs

- Sensitive information — full card numbers, private sign-in keys — is never included in anything this product returns; only safely shareable summaries (like a card's last four digits) are exposed.
- Preference and settings values must come from a known, defined list of options; values outside that list are rejected rather than silently accepted.
- Every data export and photo upload is delivered through a time-limited, single-purpose link rather than a permanent one.
- Every unsubscribe link is time-limited and usable only for the specific account and channel it was issued for.

### 2.3 Failure Scenarios & Error Messaging

Customers get a clear, actionable message when something they submit is invalid, when they attempt something they're not allowed to do, or when the product is temporarily unavailable. Internal systems calling this product receive a distinct signal for "the lookup service itself is having trouble" versus "that account simply doesn't exist" — an important distinction for the login system, which reacts differently to each. The full, itemized catalog of error conditions and their exact wording is maintained separately as part of the engineering-facing contract for this product.

---

## 3. Operational Requirements

### 3.1 Performance, Scaling, & Reliability

| Metric | Target |
|---|---|
| Sign-in credential lookup speed | Returns in about 100 milliseconds or less for nearly all requests — this sits on the critical sign-in path |
| Profile retrieval speed | Under 100 milliseconds for nearly all requests |
| Profile update speed | Under 200 milliseconds for nearly all requests |
| Account data accuracy | Better than 99.9% |
| Scale | Supports 10 million or more customer accounts |
| Availability | 99.9% target for V1 |

#### 3.1.1 Speed

Covered by the speed rows in the table above (credential lookup, profile retrieval, profile update).

#### 3.1.2 Traffic Volume

**NEEDS DECISION** — Target not yet set. No target has been defined for how many requests per second this product must sustain, either day-to-day or during peak periods.

#### 3.1.3 Guaranteed Capacity Agreements

**NEEDS DECISION** — Target not yet set. No formal agreement exists yet with any partner team guaranteeing a specific request volume this product will handle on their behalf.

#### 3.1.4 Caching

To keep the most frequently requested information (profile details and sign-in lookups) fast, recently retrieved results are held briefly before being refreshed from the source, so a change may take a short time to appear everywhere it's requested.

#### 3.1.5 Concurrency

**NEEDS DECISION** — Target not yet set. No target has been defined for how many requests this product should handle at the same time on a given instance.

### 3.2 Security, Compliance, & Governance

- Saved payment methods: the full card number is never stored — only a reference token is kept internally, and even that reference is never returned to a caller, only used behind the scenes.
- Passwordless sign-in credentials: only the public half of each credential is stored. The private half never leaves the customer's device and is never transmitted to or stored by any Komodo system.
- Profile photos are kept private by default, transferred securely, and stored encrypted; upload is capped at a small file size and restricted to standard image formats.
- No personal information appears in system logs — only account identifiers, and even those are partially masked where needed for troubleshooting.
- Right to erasure (Europe's GDPR and equivalent US state privacy laws): account closure must be complete and verifiable, with no data left behind anywhere once deletion finishes.
- When two changes to the same account happen at nearly the same moment — for example, two systems both updating account status, or two requests both trying to set a new default address — the product detects the conflict and asks the caller to retry with the latest data, rather than silently losing one of the changes.

#### 3.2.1 Who Can Access What

Every action requires proof of identity — either a signed-in customer or a verified internal system. Internal systems verify each other's identity directly and instantly, without a round-trip to a central check-in service, so the sign-in path stays fast. There are three exceptions to the identity requirement: basic health checks, the public unsubscribe action (which uses its own limited-purpose verification link instead), and a check for whether an account exists at all, which is capped at a low request rate per requester to prevent abuse.

### 3.3 Data Management & Infrastructure

| What | Business description |
|---|---|
| Primary account data store | Where all core account records live. This product fully owns and manages this store, including how it's backed up and protected against loss. |
| Data export storage | Temporary storage for account data-export files; each file automatically expires and is deleted after a limited period. |
| Profile photo storage | Permanent storage for customer profile photos, with no automatic expiration. |
| Fast-read cache | Short-term, in-memory cache for the most frequently requested account and sign-in data, refreshed frequently. |

- **V1 hosting:** runs on Komodo's standard managed hosting, with separate public-facing and internal-only access points.
- **Notifying other systems of changes:** account changes are published automatically to Komodo's central events system, which is responsible for telling any other system that needs to react to an account change. This product's only job is publishing the change; downstream reaction is handled elsewhere.
- **Record history:** every record tracks when it was created and last changed; sign-in credentials additionally track when they were last used.

---

## 4. Implementation Strategy

### 4.1 System Dependencies & Integrations

- **The login system** — looks up sign-in credentials and manages passwordless credential records; this product serves that lookup on the critical sign-in path.
- **The address-validation system** — verifies that a submitted address is real and correctly formatted; this product stores whatever address the customer saves, without judging its validity.
- **The payments system** — reads the stored payment method references this product keeps, through an internal-only connection.
- **The messaging system** — requests unsubscribe links on a customer's behalf, and listens for account changes to keep its own opt-in/opt-out records current.

### 4.2 Testing Plan

#### 4.2.1 Component-Level Checks

Each piece of this product's logic has its own automated correctness checks, run against a simulated version of the data store rather than a live one. These run automatically on every change before it ships.

#### 4.2.2 Feature-Area Tests

**NEEDS DECISION** — Target not yet set. A middle tier of testing — verifying a whole feature area works together, still without a live dependency — is planned but not yet built.

#### 4.2.3 Live-Dependency Integration Tests

**NEEDS DECISION** — Target not yet set. Testing this product against a realistic, live-like version of its data store is planned but has been deliberately deferred to the next major version rather than included in V1.

#### 4.2.4 End-to-End Tests

A separate suite of tests runs this product's key customer workflows against real, live dependencies rather than simulated ones, to catch issues that only surface in a real environment.

#### 4.2.5 Load & Resilience Tests

**NEEDS DECISION** — Target not yet set. No load testing (verifying the product holds up under heavy simultaneous traffic) or failure-injection testing (verifying the product recovers gracefully when a dependency breaks) exists yet.

### 4.3 Rollout Plan

**NEEDS DECISION** — Target not yet set. No formal staged-release process (for example, releasing to a small share of traffic first, or a documented way to reverse a bad release) has been defined.

### 4.4 Risks, Constraints, and Mitigations

**Risks**

- **Privacy breaches and incomplete legal deletions** — a partial erasure leaves data behind and creates legal exposure. *Mitigation:* account closure happens in two phases — a recoverable window, then a permanent, all-at-once removal designed to leave nothing behind (see §2.1).
- **Performance as the customer base grows toward and beyond the scale target (§3.1)** — the underlying data organization scales well in general, but uneven usage patterns could create pockets of slowdown for some customers. **NEEDS DECISION** — Target not yet set; no mitigation beyond the current data organization has been defined.
- **Data briefly out of sync with other systems** — this product does not guarantee a change is reflected everywhere in Komodo at the exact same instant. *Mitigation:* accepted as a trade-off; other systems are expected to tolerate a short delay rather than requiring perfect real-time consistency.
- **Dependency risk with the login system** — any change to the shape of the credential data or the sign-in lookup result has to be coordinated across both products at the same time. *Mitigation:* changes to that shared contract go through a coordinated release process across both teams.

**Constraints**

- Signing customers in and password handling are permanently out of scope — the login system owns that entirely; this product only stores what the login system tells it to.
- Verifying that an address is real and correctly formatted is not this product's responsibility — the address-validation system owns that; this product stores whatever the customer submits, as submitted.

---

## 5. Project Timelines

### 5.1 Version 1.0.0

Everything described in Section 2 (Functional Capabilities) above. Work is sequenced by risk and dependency order rather than by fixed dates.

**NEEDS DECISION** — Target not yet set. No calendar date has been committed for the V1 launch.

### 5.2 Version 2.0.0

Deferred to this next phase: more efficient browsing of large result lists; a second, geographically distributed layer of the fast-read cache; automatic resizing/transformation of uploaded profile photos; customer-owned shareable lists (such as wishlists or gift registries); and a more resilient way of preventing duplicate actions across regions.

**NEEDS DECISION** — Target not yet set. No calendar date has been committed for V2.

---

## 6. Appendix

### 6.1 Glossary

- **Soft-delete window** — the recoverable period after a customer closes their account (duration stated in §2.1) during which the closure can still be reversed, before deletion becomes permanent.
- **Pending deletion** — the status an account is in for the duration of the soft-delete window.
- **Consent log / audit trail** — the permanent, append-only record of every marketing opt-in and opt-out a customer has made. It's kept separate from day-to-day communication preferences so there's always a verifiable history of consent.
- **Default address / payment method** — the single saved address or payment method, per category, marked to be used automatically; only one item per account can hold this marker in each category.
- **Tag namespace** — the naming convention that determines which internal Komodo system is allowed to attach which labels to an account, so that, for example, the loyalty program can't accidentally overwrite a label owned by customer support.

### 6.2 References

**Regulations & standards this product must meet**

- Europe's data-protection law (GDPR) — customer's right to erasure and right to access.
- California's consumer privacy law — equivalent erasure and access rights.
- Other US state privacy laws — the same rights, treated as a broader superset.
- US federal messaging laws (CAN-SPAM, TCPA) — public unsubscribe obligations.

**Design & decision records**

- V1 target-state decision record — V1 target-state decisions.
- V2 target-state decision record — V2 target-state decisions.
- Future decision records are added to this list as they're written.
- High-level technical design.
- Interface contract and error catalog.
- Data model.
