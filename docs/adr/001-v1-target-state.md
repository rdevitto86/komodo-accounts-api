# ADR 001 — V1 Target State

- **Status:** Active
- **Date:** 2026-06-23 (original) · amended 2026-06-29 (Phase 9 merge)
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
| 9 | V1 finalization (routes, US-deletion, schema, concurrency, infra ownership) | Open — see D1–D10 below |

Every phase exits on the same gate: `go build ./... && go vet ./... && TEST_TIER=component go test -race ./... && golangci-lint run`, plus an `openapi.yaml` lint.

---

## Decisions

### D0 — Dual-mode auth (passwordless-primary, password-backup)

Komodo leans into passwordless (passkeys + OTP) as the primary experience but **retains password login as a backup**. Consequences for accounts-api:

- `password_hash` remains stored identity data. It stays out of every **public** request/response body (`json:"-"` on `models.Account`), and is returned only on the private `GET /v1/accounts/credentials` route that auth-api consumes.
- **Hashing stays out of accounts-api** (PRD non-goal: not an authenticator). auth-api hashes plaintext (Argon2id) and writes the result via `PUT /v1/accounts/{id}/credentials`. accounts-api never accepts a client-supplied precomputed hash and never hashes on a public route.
- New accounts default to passwordless-primary (`auth_methods=[]`); `password` is appended only when one is actually set.

### D1 — Unsubscribe routes move under `/communications`

`POST /v1/communications/unsubscribe` (public, HMAC-authenticated). Mint endpoint: `POST /internal/v1/accounts/{id}/communications/unsubscribe-token`.

- **Why:** the unscoped `/v1/unsubscribe` is ambiguous about what is being unsubscribed (account? newsletter? channel?). `/v1/communications/*` signals scope.
- **Why not under `/me/*`:** the public verify endpoint is **unauthenticated** (mail-client click, no session). `/me/*` implies an authenticated subject; using it for an HMAC-bearer flow is misleading.
- **Coordination:** comms-api is the mint caller; the rename ships in lockstep before any production cutover.

### D2 — All resource updates are partial (pointer-DTO + DDB partial UpdateItem)

`PUT /v1/me/addresses/{id}`, `PUT /v1/me/preferences`, `PUT /v1/accounts/{id}/settings`, and `PUT /v1/me/profile` are all partial. Payment `PUT /v1/me/payments` stays full-replace upsert by ID (intentional — payment-method records are write-once metadata blobs from the PSP).

- **Why:** full-replace silently wipes any field the client didn't echo back. Pointer DTOs distinguish "absent" from "zero". BFFs don't have to round-trip the whole resource.

### D3 — US-deletion reframe + 30-day soft-delete window

`DELETE /v1/me/profile` no longer hard-erases. It sets `Settings.status=pending_deletion`, `status_changed_at=now`, emits `account.deletion_requested`, returns 202. `POST /v1/me/profile/restore` (authenticated, inside window) reverses. A scheduled worker hard-erases at `status_changed_at + 30d` (Query + chunked `TransactWriteItems` ≤100 + S3 exports + S3 avatars + cache invalidation, emits `account.deleted`).

- **Why:** US state privacy laws (CCPA/CPRA, VCDPA, CPA, …) require a verifiable deletion workflow; GDPR is a superset. A 30-day window reduces accidental-click support load and matches CCPA's "verifiable consumer request" cadence.
- Surfaces relabeled in PRD + UI copy: "Account closure / right-to-delete" and "Data download / right-to-access". No API shape change for export.
- Worker placement (CloudWatch Events Lambda owned here vs. scheduled events-api consumer) is an implementation-time choice; this ADR does not bind.

### D4 — Avatar upload via pre-signed S3

`POST /v1/me/profile/avatar` returns a 15-min pre-signed S3 PUT URL into `s3://komodo-accounts-avatars-<env>/<account_id>/<ksuid>.{jpg|png|webp}`. Client uploads direct to S3 (≤2 MB, `Content-Type` constrained), then `PUT /v1/me/profile {avatar_url: ...}`. Reads are also pre-signed GETs (no public objects).

- **Why:** the API never sees image bytes — no bandwidth tax, no SSRF/MIME-sniff surface, no per-pod multipart parsing.
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

---

## Consequences

- Risk-first sequencing (0–5) front-loaded the build blocker and observable-correctness bugs before feature work (6) and quality (7–8). Phase 9 is contract-and-schema finalization on top of a stable base.
- D0 keeps accounts-api a pure data store: auth-api owns hashing and verification, accounts-api owns storage and the read/write contract. Preserves the PRD's "not an authenticator" non-goal while supporting password login.
- D1 is the only **breaking** route change in V1 (BFF + comms-api coordination required). The schema prunings (D5) are breaking only in the sense that fields disappear from response payloads — clients that ignored them are unaffected.
- D3's soft-delete window adds a scheduled worker dependency; choosing CloudWatch Events vs. events-api scheduled consumer is the only open implementation decision at the start of Phase 9.
- D7's optimistic concurrency adds a 409 path that BFFs must handle (refetch + retry). Worth it for settings; explicitly **not** applied to profile/preferences where the extra round-trip would cost more than the race risk.
- D8 makes this repo the single source of truth for the accounts table schema. Any future schema change starts in `data-model.md` and the CDK in the same PR.
- The cross-review gate (`AGENTS.md`) applies: every change `software-engineer` produces is reviewed by `quality-assurance` against `~/.claude/standards/` and this ADR before it's called done.
- After Phase 9, accounts-api enters GA-candidate state. Forward-looking work (avatar transforms, cursor pagination, shareable wishlists, multi-region cache) is V2 and out of scope here.
