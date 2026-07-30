# ADR 001 — V1 Target State

- **Status:** Active
- **Date:** 2026-06-23 (original) · amended 2026-06-29 (Phase 9 merge) · amended 2026-07-29 (D12, public-surface audit)
- **Deciders:** rad
- **Supersedes:** —
- **Related:** `docs/prd.md`, `docs/hld.md`, `docs/data-model.md`, `../openapi.yaml`, `TODO.md` (itemized work), auth-api ADR 001 (token verification / M2M contract)

## Context

Accounts-api is the sole owner of account identity data for Komodo: profiles, account settings, passkey credential public keys, preferences, address books, payment-method references, consent history. It is a **data store, not an authenticator**: auth-api verifies credentials by reading them over the private plane (`GET /v1/accounts/credentials`) and issues all tokens (auth-api ADR 001).

V1 takes the service from a mid-refactor state to production-ready. The service is pre-prod — the only live contract is auth-api's credential/passkey read path — so breaking changes are cheap and sequencing optimizes for risk reduction, not backward compatibility. This document records the **forward-looking target state** (what V1 looks like when done) and the **decisions** that shaped it. Itemized work, including historical phase checklists, lives in `TODO.md`.

---

## Phase overview

| Phase | Name | Status |
|---|---|---|
| 0 | Restore green build + config migration | Done (2026-06-23) |
| 1 | Correctness & logic flaws | Done (2026-06-26) |
| 2 | Dual-mode auth (passwordless-primary, password-backup) | Done (2026-06-23) |
| 3 | Security hardening | Done (2026-06-26) |
| 4 | Performance (hot-path cache) | Done (2026-06-26) |
| 5 | Error handling & observability | Done (2026-06-26) |
| 6 | Account-domain rename + business logic (settings, consent, export, unsubscribe, CDC) | Done (2026-06-27) |
| 7 | Test coverage retrofit | Done (2026-06-28) |
| 8 | Code-quality & CI alignment | In Progress |
| 9 | V1 finalization (routes, US-deletion, schema, concurrency, infra ownership) | Open — see D1–D10, D12 below |
| 18 | Test tier build-out — component + integration | Open — see D11 below |

Every phase exits on the same gate: `go build ./... && go vet ./... && TEST_TIER=component go test -race ./... && golangci-lint run`, plus an `openapi.yaml` lint.

---

## Decisions

### D0 — Dual-mode auth (passwordless-primary, password-backup)

Komodo leans into passwordless (passkeys + OTP) as the primary experience but **retains password login as a backup**. Consequences for accounts-api:

- `password_hash` remains stored identity data. It stays out of every **public** request/response body (`json:"-"` on `models.Account`), and is returned only on the private `GET /v1/accounts/credentials` route that auth-api consumes.
- **Hashing stays out of accounts-api** (PRD non-goal: not an authenticator). auth-api hashes plaintext (Argon2id) and writes the result via `PUT /v1/accounts/{id}/credentials`. accounts-api never accepts a client-supplied precomputed hash and never hashes on a public route.
- New accounts default to passwordless-primary (`auth_methods=[]`); `password` is appended only when one is actually set.

### D1 — Unsubscribe routes move under `/communications`

`POST /v1/communications/unsubscribe` (public, HMAC-authenticated). Mint endpoint: `POST /v1/accounts/{id}/communications/unsubscribe-token` (private plane).

**Amended 2026-07-28 — the `/internal/` path prefix is dropped.** The mint endpoint was originally specified as `POST /internal/v1/accounts/{id}/...`. The listener port is the plane boundary — private routes bind `PORT_PRIVATE` and are unreachable through the ALB — so a path prefix adds no protection and implies a third privacy tier that does not exist. It was also the only one of the 16 private routes carrying it, which forced that route out of the shared `/v1` router group.

- **Why:** the unscoped `/v1/unsubscribe` is ambiguous about what is being unsubscribed (account? newsletter? channel?). `/v1/communications/*` signals scope.
- **Why not under `/me/*`:** the public verify endpoint is **unauthenticated** (mail-client click, no session). `/me/*` implies an authenticated subject; using it for an HMAC-bearer flow is misleading.
- **Coordination:** comms-api is the mint caller; the rename ships in lockstep before any production cutover.

### D2 — All resource updates are partial (pointer-DTO + DDB partial UpdateItem)

`PUT /v1/me/addresses/{id}`, `PUT /v1/me/preferences`, `PUT /v1/accounts/{id}/settings`, and `PUT /v1/me/profile` are all partial. Payment `PUT /v1/me/payments` stays full-replace upsert by ID (intentional — payment-method records are write-once metadata blobs from the PSP).

- **Why:** full-replace silently wipes any field the client didn't echo back. Pointer DTOs distinguish "absent" from "zero". BFFs don't have to round-trip the whole resource.

### D3 — Immediate erasure, guarded by confirmation

**Amended 2026-07-28. This reverses the original D3 (30-day soft-delete window + restore + scheduled worker).** The prior text is summarized under "Superseded reasoning" below so the reversal is legible.

`DELETE /v1/me/profile` hard-erases synchronously: Query the account partition + chunked `TransactWriteItems` ≤100 + S3 exports + S3 avatar + email-reservation release + cache invalidation, returns 204. There is no `pending_deletion` state, no restore route, and no scheduled worker.

**Confirmation is mandatory** — erasure is irreversible, so the request must prove intent:

| Stage | Control | Status |
|---|---|---|
| Now | `confirm_email` in the request body must equal the account's own email; 400 on absent or mismatched | Ships with this change |
| Later | `auth_time` claim within 5 minutes; 401 `reauthentication_required` otherwise | Blocked — see below |

- **Why immediate:** a recovery window is not a legal requirement. GDPR Art. 17 and the US state statutes require erasure to be complete and verifiable, not delayed. Holding customer data for 30 days after the customer asked for it deleted is the opposite of what the right conveys, and it forced a scheduled worker whose absence made the 202 response a promise nothing kept.
- **Why confirmation instead of a window:** the original window's stated benefit was reducing accidental-click support load. A confirmation gate addresses that directly, at the moment of intent, without retaining data or running a worker.
- **Why typed-email first:** `auth_time` does not exist today — not in auth-api's tokens and not in `komodo-forge-sdk-go`'s `CustomClaims` (`security/jwt/jwt.go:29-33`, which carries only `scp`, `adm`, and the registered claims). Shipping the `auth_time` check before auth-api issues the claim would 401 every delete request. Typed-email has no cross-team dependency and is a real control from day one; fresh-sign-in layers on once the claim lands, and whether both are kept is decided then.
- **Export is a prerequisite, not a step.** A customer who wants their data requests `POST /v1/me/profile/export` first and receives it; deletion is a separate, later action. accounts-api does not send email and does not hold a job open waiting on comms-api delivery confirmation.
- **Consequences for correctness:** erasure moves from a background job onto a customer-facing request. Three known defects rise in priority accordingly — cross-chunk transaction atomicity, the avatar object not being deleted, and the email reservation being skipped when the profile row carries no readable email (`TODO.md` 24.17, 24.7). At realistic per-account item counts (D10) the erase fits in one or two transactions, well inside the write timeout; the pathological case is what needs the guard.
- Surfaces stay labelled "Account closure / right-to-delete" and "Data download / right-to-access" in PRD + UI copy.

**Superseded reasoning (original D3, 2026-06-23):** the window was justified as matching CCPA's "verifiable consumer request" cadence and reducing accidental-click support load, with a scheduled worker hard-erasing at `status_changed_at + 30d`. The cadence argument conflated *verifying* a request with *delaying* it — the statutes bound how long you may take, not how long you must wait. The support-load argument was real and is now served by the confirmation gate.

### D4 — Avatar upload via pre-signed S3

`POST /v1/me/profile/avatar` returns a 15-min pre-signed S3 PUT URL. Client uploads direct to S3 (≤2 MB, `Content-Type` constrained to jpeg/png/webp), then `PUT /v1/me/profile {avatar_url: ...}`. Reads are also pre-signed GETs (no public objects).

**Amended 2026-07-28 — one object per account, at a fixed key.** The key is `avatars/<account_id>/avatar` in `komodo-accounts-avatars-<env>`. Every upload overwrites the previous one.

- **Why a fixed key rather than the original `<account_id>/<ksuid>.{ext}`:** an account has exactly one avatar (null when fresh or cleared) — there is no version history to serve. A per-upload KSUID key makes storage grow without bound per account, which is a direct cost-abuse vector: any client holding a valid 15-minute link can write unlimited distinct objects, none of which are ever read. A fixed key bounds an account's avatar footprint to one object no matter how many times the link is used.
- **Consequence — the size cap is the real control.** A pre-signed PUT URL is reusable until it expires, and SigV4 does not bind `Content-Length` on a body-less signing request, so the 2 MB limit is advisory today (`TODO.md` 24.6). With a fixed key the blast radius is request cost rather than storage growth, but the cap still needs enforcing.
- **Wording:** the link is **time-limited**, not single-use. PRD and HLD previously claimed single-use; a pre-signed PUT cannot deliver that without a server-side record of issued links, which was considered and rejected as disproportionate for a path that otherwise touches no database.
- **Why pre-signed at all:** the API never sees image bytes — no bandwidth tax, no SSRF/MIME-sniff surface, no per-pod multipart parsing.
- **Reuse:** presign helper lands in `komodo-forge-sdk-go/aws/s3presign` so order receipts, KYC docs, etc. share the pattern and bucket policy template.
- No transforms in V1; thumbnails / format normalization deferred to V2.

### D5 — Schema pruning

Drop the following before V1 ships:

- `Account.middle_initial` — no business logic depends on it; ~30% of US users have none; if shipping labels ever need it, model on `Address` (`recipient_middle_initial`), not on identity.
- `Preferences.marketing` (`map[string]string`) — dead field; all marketing consent flows through `ConsentLog`.
- `AccountSettings.segments` (`map[string]string`) — no producer, no defined keys, no consumer. `tags[]` already covers the namespaced bucketing use case.

- **Why:** every modeled field is a forever cost — schema, OpenAPI, validation, BFF, UI, mocks, tests, migration risk. Pruning before GA is order-of-magnitude cheaper than pruning after.

### D6 — Enum-validate `Preferences.communication` keys

Keys validated against `{email, sms, push, postal}` on write; unknown keys → 400. Same enum lives on `ConsentLog.channel`.

- **Why:** keeps schema flexibility (DDB Map, additive without migration) without sacrificing validation. A typo'd `"emaill": true` is silently wrong today.

### D7 — Optimistic concurrency on `AccountSettings`; atomic `is_default` flips

- `AccountSettings` carries a `version int` attribute. `UpdateSettings` / `UpdateSettingsTags` write with `ConditionExpression #v = :expected` + `SET #v = :v + 1`. 409 on conflict → caller refetch + retry.
- `SetAddressDefault` / `SetPaymentDefault` become a single `TransactWriteItems` (demote-old + promote-new atomically). The list-then-loop helper is retired.

- **Why:** Settings has multiple cross-service writers (loyalty, marketing, support, customer-servicing-api) — last-write-wins loses tag and verified-flag state. `is_default` had a race window that allowed two-defaults or zero-defaults.
- **Where it does NOT apply:** profile and preferences keep last-write-wins. Concurrent self-edit rate is effectively zero; the extra round-trip isn't worth the regression-test cost.

### D8 — This service owns the account DynamoDB table (CDK)

`deploy/cdk/main.ts` uses `new dynamodb.Table(...)` — **never** `Table.fromTableName(...)`. Canonical spec: PK/SK String, GSI1 with `INCLUDE` + `account_id`, `PAY_PER_REQUEST`, `NEW_AND_OLD_IMAGES` streams, PITR, AWS-managed KMS, deletion-protection on stg/prod, `RemovalPolicy.RETAIN` on stg/prod. Stream ARN exported as `${env}-AccountsTableStreamArn` for events-api consumption.

- **Why:** `fromTableName` makes the schema opaque to this stack — `cdk diff` cannot catch drift, stream ARN can't be cleanly exported, ownership of streams/PITR/encryption is ambiguous. The account domain owns its data store end-to-end.
- Same for the avatars bucket (D4) and exports bucket: both are CDK resources in this repo.

### D9 — Fargate-only (Lambda mode dropped)

Remove README's "Lambda Deployment (future)" section and any `httpadapter` / `AWS_LAMBDA_FUNCTION_NAME` references.

- **Why:** the cost-savings story doesn't hold for steady-traffic APIs; per-Lambda IAM vs. per-task IAM adds operational complexity without payoff.

### D10 — Cursor pagination deferred to V2

Realistic per-account counts in V1: addresses ≤5, payment methods ≤3, passkeys ≤5, consent log ≤dozens. The 100-item cap sits well above the long tail. When V2 lands cursor pagination, ship `{items, next_cursor}` wrapper across all list endpoints in one contract change.

- **Why not ship the wrapper now:** shipping a `next_cursor` that is always empty invites premature client logic; the BFF unwraps the same shape regardless when the time comes.
- **Revisit triggers:** usage data shows any list >50 items; bulk-admin/CS views land; ConsentLog growth on long-tenured accounts approaches the cap.

### D11 — Component and integration tests are V1 scope

Decided 2026-07-20. Both tiers ship in V1. This **reverses** the prior deferral of integration tests to V2 (previously gated on "coverage-floor pressure or multi-region work"); component tests were never explicitly scoped at all, despite `TEST_TIER=component` appearing in every phase-exit gate in this ADR and in `TODO.md`.

The gap this closes: `test/component/`, `test/integration/`, `test/chaos/`, and `test/perf/` exist as empty directories, and no code in the repo reads `TEST_TIER`. The gate `TEST_TIER=component go test -race ./...` is currently identical in effect to `go test -race ./...`, so the tier has been asserted in documentation without ever being enforced.

- **Component** (`test/component/`, `testutil.Component(t)`): real `api.Service` over generated mocks, driven through a live `http.ServeMux` from `newPublicMux`/`newPrivateMux` — not by calling handlers directly. Route-registration collisions and the middleware chain are only observable this way; today a duplicate method+path panics at process boot and no test would catch it.
- **Integration** (`test/integration/`, `testutil.Integration(t)`): `testcontainers-go` + LocalStack DynamoDB against the real `internal/db.Repo`. Covers what mocks structurally cannot — `TransactWriteItems` cancellation reasons, conditional-write guards, `BatchGetItem` `UnprocessedKeys` under throttling, GSI1 email lookup, D7's version conflicts, and lexicographic SK ordering.
- **Why now rather than V2:** the correctness batch closed in Phase 12 (12.1–12.4, 12.6, 12.7) consists almost entirely of DynamoDB conditional-expression and transaction-cancellation semantics. Mocks assert the call shape, not DynamoDB's actual behavior, so those fixes are currently verified only by inspection. That is the coverage-floor pressure the original deferral was waiting for — it already arrived.
- **Cost of the reversal:** the tier folders and the SDK gate helpers already exist (`testing/testutil` at the pinned `v0.22.1`), so no SDK work is required; the cost is test authorship plus a LocalStack container in CI on the merge-to-`main` path.
- **Environment placement** is unchanged from `~/.claude/standards/testing.md` §1 — integration remains an STG-tier concern against full-stack infra; `testcontainers` makes the DynamoDB slice of it runnable locally and in CI without waiting on a deploy.

Tracked as Phase 18 in `TODO.md` (18.1–18.5).

### D12 — Public-surface completion: four gaps closed, two routes retired

Decided 2026-07-29, from a route-by-route audit of both planes against the PRD, the data model, and
comparable enterprise account APIs. The private plane came out of that audit unchanged — it maps
cleanly to its named consumers. The public plane did not.

**Added.** All six ship as **registered route + published contract + handler returning 501**, so
consumers can build against a stable shape while implementation lands (`TODO.md` §26):

| Route | Gap it closes |
|---|---|
| `GET`/`POST /v1/me/consents` | `ConsentLog` is a stored, permanently-retained entity with **no authenticated route at all** — the only writer was the unauthenticated HMAC unsubscribe (opt-out only), and the only reader was the export blob. A signed-in customer could not opt in, and could not withdraw without a marketing email in hand. GDPR Art. 7(3) requires withdrawal to be as easy as consent. Biggest gap found. |
| `PUT /v1/me/profile/email` | Email is silently dropped by `UpdateAccountRequest`, so **no route could change it**. Split out rather than folded into profile update: an email change re-checks GSI1 uniqueness, moves the email reservation, resets `email_verified`, and needs auth-api confirmation of the new address. It is also what makes `account.email_changed` reachable — that event could never fire. |
| `GET /v1/me/settings` | Verification flags and lifecycle status were private-plane only; the account owner could not see their own. Read-only projection of `email_verified`, `phone_verified`, `status`. `tags` and `version` stay excluded — those are cross-service machinery, not customer data. No public write counterpart: both fields are set by other services. |
| `DELETE /v1/me/profile/avatar` | No route removed a photo. Blanking `avatar_url` via profile update cleared the reference and orphaned the S3 object at the account's fixed key. |
| `GET /v1/me/addresses/{id}`, `GET /v1/me/payments/{id}` | Routes onto `GetAddress` / `GetPayment`, dead repo methods since Phase 6 (was `TODO.md` 14.3). Clients had to fetch a whole list to render one entry. |

**Retired.**

- `POST /v1/me/profile/restore` — **was live in `setup.go` but absent from `openapi.yaml`**, and D3's amendment removed the recovery window it existed to serve. An undocumented authenticated route attempting to reverse an irreversible erasure. Handler, service method, and `RestoreAccount` repo method are now dead code (`TODO.md` 26.7).
- `DELETE /v1/me/preferences` — no customer-facing meaning, and it left an account with no language or timezone. Resetting to defaults is `PUT` with default values. No comparable service offers it.

**`GET /v1/accounts/exists` — kept, reversing the audit's initial recommendation to remove it.**

The route is an account-existence oracle, which is why removal was proposed. Three things overturned
that:

- **Identifier-first sign-in genuinely needs it.** With passwordless as primary (D0), the sign-in
  screen must decide at the email step whether to route to sign-in or registration. Google,
  Microsoft, and Okta all expose existence at that step for exactly this reason and document it as an
  accepted trade-off. This is the norm for passwordless, not a deviation from it.
- **The dangerous half is already gone.** `auth_methods` was removed from the response 2026-07-26
  (item 13.2). A bare `{exists: bool}` reveals whether an address is registered — it no longer
  reveals *how* that account signs in, which was the materially worse leak.
- **Registration already rejects duplicates** via `EmailAlreadyExists`, so removal would not lose
  correctness — only the pre-submit hint. The cost is a worse sign-in screen for no security gain
  once the response is a bare boolean.

Kept, with hardening tracked rather than assumed: the ~1 rps/caller limiter does not hold against a
distributed caller, so the real controls are edge-level (WAF rate rules, bot detection) —
`TODO.md` 26.6. **auth-api does not call this route today** — it holds a `PathExists` constant and
generated types but no live caller (`internal/models/accounts/paths.go:5`), so nothing breaks either
way; the decision rests on the sign-in design, not on current consumers.

**Rejected in the same audit:** a re-download route for existing exports
(`GET /v1/me/profile/export`). The 15-minute link and 7-day retention window are themselves under
review (`TODO.md` 26.5); a convenience route over windows that may change is premature.

---

## Decisions rejected (recorded so they aren't relitigated)

| Proposal | Decision | Reason |
|---|---|---|
| Public handlers forward to private endpoints over HTTP for DRY | Rejected | The service layer is already shared. An extra HTTP hop blows the credentials hot-path budget (p99 ≤ 100 ms) and couples the public outage surface to the private plane. |
| Replace in-process TTL cache with Redis/ElastiCache in V1 | Rejected | In-process at 100k entries + 60 s TTL meets the p99 budget at 10M accounts. Revisit at 50M or multi-region. |
| Drop the unsubscribe HMAC; require a logged-in session | Rejected | Most unsub clicks are from mail clients with no session. HMAC + `jti` is the correct construction for tamper + replay protection without a DB write at mint. |
| Strongly-type `Preferences.communication` as a struct | Rejected | Loses additive flexibility. D6's enum allowlist gives the validation without the migration cost. |
| Merge marketing and transactional consent into one preferences object | Rejected | Two systems of record by design — transactional is per-channel toggle (current state), marketing is append-only audit log (history). Reconciling them yearly is more expensive than keeping them apart. |
| Reject `UpdateAccount` requests that carry immutable fields with 400 | Rejected (silent-drop chosen) | Lets clients round-trip the full object without surgically stripping `account_id` / `email` / `created_at` / etc. Drop is documented in the OpenAPI `UpdateAccountRequest` description. |
| Keep a shorter recovery window (7 days) instead of removing it (2026-07-28) | Rejected | Retains every piece of the machinery the reversal exists to remove — `pending_deletion` state, restore route, scheduled worker, window-expiry check — for a benefit the confirmation gate already delivers. A shorter window is the same design at a different constant. |
| Remove `GET /v1/accounts/exists` as an enumeration oracle (2026-07-29) | Rejected — kept, see D12 | Identifier-first passwordless sign-in needs an existence check at the email step; the response is already reduced to a bare boolean, and registration rejects duplicates regardless. Removal costs sign-in UX and buys nothing once `auth_methods` is gone. Hardening belongs at the edge. |
| Read-only `GET /v1/me/passkeys` in V1 (2026-07-29) | Rejected — deferred to V2 (ADR 002) | Contradicts D0's ownership line, and listing is the cheap half: the screen is only useful with revoke, which can lock a passwordless-primary customer out and therefore needs a last-credential guard that is auth-api's policy to own. |
| `GET /v1/me/profile/export` to re-download an existing export (2026-07-29) | Rejected | The 15-minute link TTL and 7-day retention are themselves under review (`TODO.md` 26.5). A convenience route layered over windows that may change is premature. |
| Per-upload KSUID avatar keys, `<account_id>/<ksuid>.{ext}` (original D4, 2026-07-28) | Rejected | An account has one avatar and no version history to serve, so distinct keys accumulate objects that are never read. Any holder of a valid 15-minute link could write unbounded storage. A fixed overwritten key bounds the footprint to one object per account. |
| Genuinely single-use avatar upload links (2026-07-28) | Rejected | Requires a server-side record of every issued link plus a lookup to burn it, on a path that otherwise touches no database. Disproportionate once the fixed key bounds storage growth; enforcing the size cap is the proportionate control. |
| accounts-api orchestrates export → email → confirm delivery → delete (2026-07-28) | Rejected | accounts-api does not send email, so "confirm delivery" means holding a job open across two services and inventing a failure mode for when delivery never confirms. Export is a prerequisite the customer completes first, not a step inside deletion. |

---

## Consequences

- Risk-first sequencing (0–5) front-loaded the build blocker and observable-correctness bugs before feature work (6) and quality (7–8). Phase 9 is contract-and-schema finalization on top of a stable base.
- D0 keeps accounts-api a pure data store: auth-api owns hashing and verification, accounts-api owns storage and the read/write contract. Preserves the PRD's "not an authenticator" non-goal while supporting password login.
- D1 is the only **breaking** route change in V1 (BFF + comms-api coordination required). The schema prunings (D5) are breaking only in the sense that fields disappear from response payloads — clients that ignored them are unaffected.
- D3's amendment (2026-07-28) **removes** a dependency rather than adding one: no scheduled worker, no `pending_deletion` state, no restore route. The open "CloudWatch Events vs. events-api scheduled consumer" question it previously carried is closed by not needing either. What it adds instead is a confirmation gate on the delete request and a raised bar on erasure correctness, since the erase is now synchronous and customer-facing.
- D7's optimistic concurrency adds a 409 path that BFFs must handle (refetch + retry). Worth it for settings; explicitly **not** applied to profile/preferences where the extra round-trip would cost more than the race risk.
- D8 makes this repo the single source of truth for the accounts table schema. Any future schema change starts in `data-model.md` and the CDK in the same PR.
- The cross-review gate (`AGENTS.md`) applies: every change `software-engineer` produces is reviewed by `quality-assurance` against `~/.claude/standards/` and this ADR before it's called done.
- After Phase 9, accounts-api enters GA-candidate state. Forward-looking work (avatar transforms, cursor pagination, shareable wishlists, multi-region cache) is V2 and out of scope here.
- D11 makes the `TEST_TIER` gate in every phase-exit command above load-bearing for the first time. Until Phase 18.1 lands, that gate is a no-op and phases have been exiting on unit tests alone — worth knowing when reading the "Done" column above.
