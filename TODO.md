# TODO

> **Current Version:** V1 (pre-release). Work is organized into Phases 0–8, sequenced by risk and
> detailed in `docs/adr/001-v1-phased-plan.md`. Phase 0 is a build blocker; Phases 1–3 are
> correctness/security and should land before feature work (6) and the test retrofit (7). Every
> phase exits on the same gate: `go build ./... && go vet ./... && TEST_TIER=component go test -race ./...`
> green, plus `golangci-lint run`.

> **Auth posture (decided 2026-06-23):** dual-mode — passwordless-primary (passkeys + OTP) with
> **password as a supported backup**. user-api stores `password_hash` but never hashes (auth-api
> owns Argon2id) and never exposes it on a public route. See ADR 001 → "Decision".

---

## Phase 0 — Restore green build + config migration ✅ (done 2026-06-23)

- **[x]** Settled package name (`db`); fixed `package repo` → `package db` in `internal/db/*.go`; updated imports `internal/repo` → `internal/db` in `internal/api/user.go` and both `cmd/*/main.go`.
- **[x]** Config migration + **SDK bump v0.15.1 → v0.19.1** (mandatory baseline for all APIs going forward). Deleted `internal/config`. v0.19.1 removed the `constants` and `crypto/jwt` packages: shared keys now come from `sdkaws` (`/aws`), `sdkhttp` (`/http`), `sdklog` (`/logging`), `jwt` (`/security/jwt`), and `sdkapi` (`/api`, for `PORT`/`PORT_PRIVATE`). Service-local keys (`DYNAMODB_TABLE`, `USER_API_CLIENT_ID`/`SECRET`) declared as local `const` in each main. Bootstrap rewritten to mirror auth-api: `logger.Init(logger.Config{...})`, single `AWS_SECRET_PATH` secret bundle via `awsSM.GetSecrets(ctx, keys)`, `awsddb.New(ctx, Config{Region, Endpoint})` on the AWS credential chain, and an explicit JWT verifier (`jwt.New(ctx, Config{PublicKeyPEM, Issuer, Audience})`) passed into `mw.AuthMiddleware(jwtClient)` (which is now parameterized). CDK env contract updated `AWS_SECRET_PREFIX` → `AWS_SECRET_PATH` (`deploy/cdk/main.ts`); CDK vitest 18/18 green.
- **[x]** Fixed package-init capture bug: retired the `DynDB` and `table` globals; added `db.New(client, table)` constructor wired in `main()`. Bootstrap moved out of `func init()` into `func bootstrap()` called as the first line of `main()` (mirrors auth-api) so the table name is read strictly after secrets load and the test binary no longer triggers secrets-manager bootstrap + `os.Exit`.
- **[x]** Repaired `.golangci.yaml` (v2 schema): moved `gofmt`/`goimports` into a `formatters:` block, dropped `gosimple` (merged into `staticcheck` in v2). Then aligned to auth-api's canonical config to reach green: `gosec` excludes G101/G104, `revive var-naming` disabled, `errcheck` excluded on `main.go`. Replaced 14 inline `json.NewEncoder(wtr).Encode(...)` with a checked `writeJSON(wtr, v)` helper on `Service` (mirrors auth-api `service.go`).
- Gate fully green: `go build` / `go vet` / `TEST_TIER=component go test -race ./...` / `golangci-lint run` (0 issues).

## Phase 1 — Correctness & logic flaws (mostly done 2026-06-23)

- **[x] 404 → 500 on every by-key read.** v0.19.1 exposes a real sentinel `dynamodb.ErrNotFound`; aliased `db.ErrNotFound = dynamodb.ErrNotFound` so every SDK miss (wrapped via `%w`) is recognized by `isNotFound` → correct 404 on missing profile/address/payment/preference/passkey.
- **[x] `updated_at` clobbered on PUT.** `UpdateProfile` now stamps `UpdatedAt = time.Now().UTC()` server-side; `CreatedAt`/`UpdatedAt` are `json:"-"` (no longer client-settable or serialized).
- **[x] Ghost address on update.** `UpdateAddress` writes with `attribute_exists(SK)` condition; a missing ID now maps `ConditionalCheckFailed` → `ErrNotFound` (404) instead of creating an orphan.
- **[x] `is_default` singleton enforced** across addresses/payments. New repo methods `SetAddressDefault`/`SetPaymentDefault` use partial `UpdateItem` (sets only `is_default`, **preserving the payment token** that read paths zero); service-layer `demoteOtherDefault*` helpers demote prior defaults on create/update when `is_default=true`. NOTE: non-atomic (list+update); Phase 3 transactions will harden.
- **[x] `username`** added to `models.User`, persisted in `CreateUser`, returned via `toModel()`, and reflected in `openapi.yaml` (User + CreateUserRequest required). NOTE: not in the `UpdateUser` mutable-merge set — treated as set-at-create.
- **[~] `resolveUserID` precedence footgun** — **DEFERRED** ([L]). Clean split needs route-aware handlers, but the same `Service` handler methods serve both `/me/*` (public, JWT) and `/users/{id}` (private, path). Safe today (`/me/*` defines no `{id}`, so it falls through to JWT). Revisit alongside a handler refactor (Phase 7/8).
- **[x] `UpdateUser` immutable fields** — **DECIDED: silent-drop**, documented in `openapi.yaml` (`UpdateUserRequest` description lists ignored immutable/server-managed fields). Chosen over 400 to keep clients able to round-trip the full object.
- Gate green: `go build` / `go vet` / `test -race` / `golangci-lint` (0 issues); `openapi.yaml` valid.

## Phase 2 — Dual-mode auth (passwordless-primary, password-backup) ✅ (done 2026-06-23)

- **[x]** Correct PRD non-goal: dual-mode documented in ADR 001 header and TODO posture note. `openapi.yaml` CredentialsResponse description updated from "bcrypt" to "Argon2id", "OAuth-only" to "passwordless-only".
- **[x] Fix write-impossible `password_hash`.** Added `PUT /v1/users/{id}/credentials` (private-server only) with `UpdateCredentialsRequest` model, `UpdateUserCredentials` repo method (DynamoDB `UpdateItem` with `attribute_exists(SK)` condition), `UpdateCredentials` service method (validates at least one field present), and `UpdateCredentialsHandler`. Route registered in `cmd/private/main.go`.
- **[x]** Changed `CreateUser` default from `auth_methods=["password"]` to empty slice `[]string{}`; caller (auth-api) now owns specifying auth_methods on user creation.
- **[x]** Extended `auth_methods` enum in `openapi.yaml` from `[password, google, apple]` to `[password, passkey, otp, google, apple]` in both `CredentialsResponse` and `UserExistsResponse`. Added `UpdateCredentialsRequest` schema and `/v1/users/{id}/credentials` PUT path.
- **[x]** Cross-repo: `CredentialsResponse` includes `user_id` field that auth-api's OTP handler consumes. Read+write contract ratified: GET returns credentials by email, PUT sets/rotates by user ID.

## Phase 3 — Security hardening

- **[M] Rate-limit the enumeration oracle.** `GET /v1/users/exists?email=` (public, no JWT) is an intentional login-UI oracle. Apply a strict per-route per-IP limit (tighter than default); consider CAPTCHA/PoW before GA; document the tradeoff.
- **[M] GDPR erasure non-atomic.** `DeleteUser` (`internal/db/user.go:226`) is Query + BatchDelete; partial failure orphans items (violates PRD FR#8). Migrate to `TransactWriteItems`.
- **[M] Unbounded reads.** `QueryAllAs` on addresses/payments/passkeys has no `Limit`/cursor. Cap + paginate.
- **[L]** Preserve payment `Token`-zeroing on all read paths through refactors (`internal/db/user.go:416,439`).
- **[M]** Log-redaction audit: user-ids only, no PII in log values.

## Phase 4 — Performance

- **[M]** Hot-path read cache for `/v1/me/profile` + credentials lookup (in-proc LRU + optional Redis L2, ~60s TTL, invalidated on `PUT`/`DELETE` profile). Holds the p99 ≤ 100ms budget.
- **[L]** Drop the second round-trip on the *exists* path: `getUserByEmail` does GSI query → GetItem, but exists only needs the GSI projection.
- **[L]** Pagination/`Limit` on list endpoints (pairs with Phase 3 unbounded-read cap).

## Phase 5 — Error handling & logger/observability

- **[M]** Convert ~33 `repo.FuncName:`-prefixed error strings in `internal/db/user.go` to verb phrases — hard-rule violation.
- **[L]** Dedicated error code/message for passkey 409 conflicts (currently reuses `models.Err.AlreadyExists` "User already exists", `internal/models/errors.go:26`).
- **[M]** Logger coverage: add request-scoped structured logging at boundaries (auth outcome, not-found vs internal, DynamoDB failures), user-id-only redaction.
- **[M]** Wire `GET /health/ready` with a DynamoDB checker in both entrypoints (forge-sdk `api/handlers/health`).

## Phase 6 — Missing features / business logic

- **[M]** GDPR profile export (PRD FR#9) — not implemented.
- **[M]** Public unsubscribe endpoint `POST /v1/unsubscribe` (HMAC token: user_id + channel, 30-day TTL) + private `POST /internal/v1/users/{id}/unsubscribe-token` mint for communications-api (CAN-SPAM/TCPA).
- **[L]** Account settings (PRD FR#7): verification status, user segmentation/tags.
- **[L]** Complete `openapi.yaml` for any added endpoints.

## Phase 7 — Test coverage retrofit

- **[M]** testify + gomock; generate repo-interface mocks; convert stub `*_test.go` (all `internal/api/*` except passkeys are `// TODO: Add tests`) to table-driven `t.Run` subtests; `httptest` at the handler layer; `testcontainers-go` (LocalStack DynamoDB) for integration; `TEST_TIER`-gated tiers.
- **[M]** 100% on security-critical paths: `resolveUserID` ownership, not-found mapping, password write path, GDPR delete completeness, payment-token redaction.
- **[M]** Per-package 80% floor enforced in CI. Reference auth-api as the canonical pattern.

## Phase 8 — Code-quality & CI alignment

- **[M]** Strip all doc/inline comments — global zero-comment hard rule (name-leading + multi-paragraph docs throughout `internal/db/user.go` and the handlers).
- **[L]** Finalize the Phase 0 package rename; retire package-level globals for injected instances.
- **[H]** CI merge gate: build / vet / golangci-lint / `test -race` / govulncheck / per-package coverage floor.
- **[L]** Revisit lint relaxations adopted in Phase 0 to reach green (`revive var-naming` disabled repo-wide, `errcheck`/`G104` excluded on `main.go`) — confirm these still match the canonical auth-api posture at CI-gate time rather than silently masking new issues.

---

## Deferred / future (V2+)

- Wishlist — sharable named lists (gift registry / family sharing): own DynamoDB table, visibility controls, public share URL. Do not conflate with cart-api "saved for later".

## Invariants to preserve through refactors

- Payment `Token` is zeroed on all read paths (`internal/db/user.go:416,439`).
- `password_hash` is excluded from `userRecord.toModel()` and from all public JSON (`json:"-"`); it surfaces only on the private `GET /v1/users/credentials` route.
