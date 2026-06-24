# ADR 001 — V1 Phased Implementation Plan

- **Status:** Active (Phase 0 is a build blocker; all phases open)
- **Date:** 2026-06-23
- **Deciders:** rad
- **Supersedes:** —
- **Related:** `docs/PRD.md`, `docs/data-model.md`, auth-api ADR 001 (token verification / M2M contract), auth-api ADR 004 (phased-plan template this mirrors)

## Context

The User API is the sole owner of user identity data for Komodo — the canonical store for profiles, passkey public keys, preferences, address books, and payment-method references (PRD). It is a **data store, not an authenticator**: auth-api verifies credentials by reading them over the private plane (`GET /v1/users/credentials`) and issues all tokens (auth-api ADR 001). V1 takes the service from its current mid-refactor state to production-ready. The service is pre-prod — the only live contract is auth-api's credential/passkey read path — so breaking changes are cheap and sequencing optimizes for risk reduction, not backward compatibility.

**Entry state (2026-06-23):**

- **Build is red.** An `internal/repo` → `internal/db` rename plus an `internal/config` deletion is half-applied: `internal/db/user.go` still declares `package repo` and imports the deleted `internal/config`; `internal/api/user.go:11` and both `cmd/*/main.go` still import `komodo-user-api/internal/repo` and `.../internal/config`. `go build ./...` fails.
- **Tests are stubs.** Every `internal/api/*_test.go` except passkeys is `// TODO: Add tests`; only passkey handler/db and `e2e/` carry real tests.
- **Spec drift.** The PRD declared the service passwordless. Product direction (recorded below) is dual-mode — passwordless-primary with password as a supported backup. The PRD non-goal, not the code, is the thing that is wrong.

Work is organized into Phases 0–8, sequenced by risk. Every phase exits on the same gate: `go build ./... && go vet ./... && TEST_TIER=component go test -race ./...` green.

## Decision — dual-mode auth (passwordless-primary, password-backup)

Komodo leans into passwordless (passkeys + OTP) as the primary experience but **retains password login as a backup**, because most customers are not yet ready for passwordless-only. Consequences for user-api:

- `password_hash` remains stored identity data. It stays out of every **public** request/response body (`json:"-"` on `models.User`), and is returned only on the private `GET /v1/users/credentials` route that auth-api consumes.
- **Hashing stays out of user-api** (PRD non-goal: not an authenticator). auth-api hashes plaintext (Argon2id; forge-sdk `security/hashing`, possibly gated) and writes the resulting hash via a new private write path. user-api never accepts a client-supplied precomputed hash and never hashes on a public route — this closes the original auth-api audit finding (client choosing its own stored hash) without making user-api an authenticator.
- New accounts default to a passwordless-primary posture; `password` is appended to `auth_methods` only when a password is actually set. Today `CreateUser` wrongly defaults `auth_methods=["password"]` (`internal/api/user.go:39`).
- The PRD "passwordless / no password fields" non-goal is corrected to document dual-mode.

## Phase Overview

| Phase | Name | Status | Primary areas |
|---|---|---|---|
| 0 | Restore green build + config migration | ⛔ Blocker | Bug sweep, Code quality |
| 1 | Correctness & logic flaws | Open | Logic flaws, Bug sweep, Error handling |
| 2 | Dual-mode auth (passwordless-primary, password-backup) | Open | Logic flaws, Security, Missing features |
| 3 | Security hardening | Open | Security, GDPR business logic |
| 4 | Performance | Open | Performance |
| 5 | Error handling & logger/observability | Open | Error handling, Logger coverage |
| 6 | Missing features / business logic | Open | Missing features & business logic |
| 7 | Test coverage retrofit | Open | Test coverage, Code quality |
| 8 | Code-quality & CI alignment | Open | Code quality |

Itemized, sequenced checklists with file:line references live in `TODO.md`. This ADR records the rationale and the sequencing.

---

## Phase 0 — Restore green build + config migration (blocker)

Nothing else can proceed until the tree compiles.

- Settle the package name (`db`, to match the new directory); fix `package repo` in `internal/db/*.go`; update imports in `internal/api/user.go` and both `cmd/*/main.go` (`internal/repo` → `internal/db`).
- Reinstate config: the deleted `internal/config/config.go` was the env-key constant set (`APP_NAME`, `DYNAMODB_TABLE`, …); both mains still reference `config.*`. Re-add or inline.
- Fix the package-init capture bug while here: `var table = os.Getenv(config.DYNAMODB_TABLE)` (`internal/db/user.go:30`) reads at init, **before** `main.init()` loads secrets from Secrets Manager. Resolve the table name at call time or inject it via a repo constructor wired in `main()` after bootstrap — which also retires the `repo.DynDB` global (auth-api flagged the identical latent bug).

## Phase 1 — Correctness & logic flaws

In-process correctness with no infra dependency. Several of these change observable HTTP behavior.

- **404 → 500 on every by-key read (P0).** The forge-sdk `getItem` returns a plain `"item not found"` error (`aws/dynamodb/operations.go:30`), but `isNotFound` (`internal/api/user.go:187`) only recognizes `repo.ErrNotFound` or the substring `"ResourceNotFoundException"`. A missing profile / address / payment / preference / passkey therefore surfaces as **HTTP 500, not 404**. The email/GSI path 404s correctly (it returns the local `ErrNotFound`); the GetItem-by-key path does not. Map the SDK miss → `repo.ErrNotFound` at the repo boundary.
- **`updated_at` clobbered on PUT.** `UpdateProfile` (`internal/api/user.go:47`) never stamps `UpdatedAt`; the repo writes `update.UpdatedAt` (`internal/db/user.go:214`), which is the zero value (or a client-supplied value, since `models.User.UpdatedAt` is `json:"updated_at"`). Stamp server-side; make `CreatedAt`/`UpdatedAt` `json:"-"`.
- **Ghost address on update.** `UpdateAddress` is an unconditional PutItem (`internal/db/user.go:335`): updating a non-existent ID silently creates an orphan and never 404s. Add an `attribute_exists(SK)` condition.
- **`is_default` never enforced as a singleton** across addresses and payments — the repo writes as-is ("caller's responsibility") but no caller demotes the previous default, so every record can be `is_default=true`. Add demote-others logic on create/update when `is_default=true`.
- **`username` silently dropped.** `validation_rules.yaml` requires `username` on `POST /me/profile` and `userRecord` carries it (`internal/db/user.go:42`), but `models.User` has no `Username` field, so `CreateUser` never persists it and `toModel()` never returns it.
- **`resolveUserID` precedence footgun** (`internal/api/profile_handler.go:16`): the `{id}` path value beats the JWT subject — safe today only because `/me/*` defines no `{id}`. Split into `userIDFromJWT` (`/me/*`) and `userIDFromPath` (`/users/{id}`).
- **`UpdateUser` silently ignores immutable fields** (`internal/db/user.go:199-214`): `email`/`user_id`/`auth_methods`/etc. on the body are dropped without notice. Decide: reject with 400 when present, or document the silent-drop contract in `openapi.yaml`.

## Phase 2 — Dual-mode auth (passwordless-primary, password-backup)

Implements the decision above.

- Correct the PRD non-goal: document dual-mode (passkey/OTP primary, password backup); remove the "no password hashes/fields exist anywhere" language.
- Keep `password_hash` stored and `json:"-"` on the public surface; keep it on `CredentialsResponse` for the private route only.
- **Fix the write-impossible defect.** `password_hash` is `json:"-"` on `models.User`, so nothing can ever set it — the credentials endpoint can only return an empty hash. Add a private write path (recommend `PUT /v1/users/{id}/credentials` or `.../password`) so auth-api can set/rotate the hash after hashing plaintext with Argon2id. user-api stores; it never hashes on a public route and never accepts a client-supplied hash.
- Change the `CreateUser` default from `auth_methods=["password"]` to a passwordless-primary posture; append `password` only when a password is set.
- Extend the `auth_methods` enum in `openapi.yaml` to include `passkey`/`otp` (currently `[password, google, apple]`).
- Cross-repo: ratify the credentials read+write contract with auth-api (it owns Argon2id hashing and verification).

## Phase 3 — Security hardening

- **Account-enumeration oracle.** Public `GET /v1/users/exists?email=` is an intentional login-UI oracle; without aggressive throttling it allows bulk harvesting. Apply a strict per-route per-IP rate limit (tighter than the default bucket); consider CAPTCHA / proof-of-work before GA; document the intentional tradeoff.
- **GDPR erasure is non-atomic.** `DeleteUser` (`internal/db/user.go:226`) is Query + BatchDelete; a mid-operation failure orphans items, violating PRD FR#8 ("complete and verifiable"). Migrate to `TransactWriteItems`.
- **Unbounded reads.** `QueryAllAs` on addresses/payments/passkeys has no `Limit`/cursor; a user can inflate their own partition to drive unbounded RCU. Cap + paginate.
- Preserve the payment `Token`-zeroing invariant on all read paths through any refactor (`internal/db/user.go:416,439`).
- Log-redaction audit: PRD requires user-ids only, no PII in log values.

## Phase 4 — Performance

The credentials lookup sits on auth-api's login hot path (PRD p99 ≤ 100ms end-to-end); profile retrieval targets p95 ≤ 100ms.

- Hot-path read cache for `/v1/me/profile` and the credentials lookup: in-process LRU + optional Redis L2, short TTL (~60s), invalidated on `PUT`/`DELETE` profile.
- Drop the second round-trip on the *exists* path: `getUserByEmail` does a GSI query then a main-table GetItem, but the exists check only needs the GSI projection.
- Pagination/`Limit` on list endpoints (pairs with Phase 3 unbounded-read cap).

## Phase 5 — Error handling & logger/observability

- Convert the ~33 `repo.FuncName:`-prefixed error strings in `internal/db/user.go` to verb phrases — **hard-rule violation** (error strings must not contain the function name). The service layer is already compliant.
- Dedicated error code/message for passkey conflicts: the 409 path currently reuses `models.Err.AlreadyExists` ("User already exists"), which is misleading for a duplicate passkey (`internal/models/errors.go:26`).
- Logger coverage: handlers/service/repo emit almost no structured logs. Add request-scoped logging at boundaries (auth outcome, not-found vs internal error, DynamoDB failures) with user-id-only redaction.
- Wire `GET /health/ready` with a DynamoDB checker in both entrypoints (forge-sdk `api/handlers/health`).

## Phase 6 — Missing features / business logic

- **GDPR profile export** (PRD FR#9) — not implemented.
- **Public unsubscribe endpoint** (CAN-SPAM/TCPA): `POST /v1/unsubscribe` with a short-lived HMAC-signed token (user_id + channel, 30-day TTL) + a private `POST /internal/v1/users/{id}/unsubscribe-token` mint endpoint for communications-api.
- **Account settings** (PRD FR#7): verification status, user segmentation/tags — not modeled.
- Complete `openapi.yaml` for any endpoints added above; confirm `CredentialsResponse` covers the `UserId` field auth-api's OTP handler consumes.

## Phase 7 — Test coverage retrofit

Today near-zero outside passkeys. Mirror auth-api Phase 4:

- testify + gomock; generate mocks from the repo interface; convert stub `*_test.go` to table-driven `t.Run` subtests; `httptest` at the handler layer; `testcontainers-go` (LocalStack DynamoDB) for integration; `TEST_TIER`-gated tiers.
- 100% coverage on security-critical paths: ownership resolution (`resolveUserID`), the not-found mapping, password write path (Phase 2), GDPR delete completeness, payment-token redaction.
- Per-package 80% floor enforced in CI.

## Phase 8 — Code-quality & CI alignment

- Strip all doc/inline comments — **global zero-comment hard rule**; the codebase is comment-heavy (name-leading and multi-paragraph docs throughout `internal/db/user.go` and the handlers).
- Finalize the package rename from Phase 0; retire package-level globals in favor of injected instances.
- CI merge gate: build / vet / golangci-lint / `test -race` / govulncheck / per-package coverage floor.

## Consequences

- Risk-first sequencing front-loads the build blocker (0) and observable-correctness bugs (1) before feature work (2, 6) and quality (7, 8).
- The dual-mode decision keeps user-api a pure data store: auth-api owns hashing and verification, user-api owns storage and the read/write contract. This preserves the PRD's "not an authenticator" non-goal while supporting password login.
- The 404→500 bug and the non-atomic GDPR delete are the two findings with direct external impact (auth-api error mapping; regulatory erasure guarantees) — both land before Phase 7 so tests lock the corrected behavior.
- Phases pass the swe→QA cross-review gate; the build gate (`build && vet && TEST_TIER=component test -race`) is the floor for every phase.
