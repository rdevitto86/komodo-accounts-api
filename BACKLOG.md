# Project Backlog

## Convention Legend
* **Priority Tagging:** `[C]` Critical | `[H]` High | `[M]` Medium | `[L]` Low
* **Status Indicators:** `[TODO]` | `[IN_PROGRESS]` | `[BLOCKED]` | `[DONE]`
* **Hierarchy ID:** `EPIC-XX` -> `TG-XX.Y` (Task Group) -> `TSK-XX.Y.Z` (Task) -> `SUB-XX.Y.Z.N` (Subtask)
* **Owner (optional):** an `Owner` row in a task's field table marks work only a person can resolve — a policy call, a risk-acceptance decision. Absent means agent-executable by default.

---

## [EPIC-01] Now, V1
*Goal: a correct, observable, deployable V1 of the accounts service that clears its own STG blockers.*

### [TG-01.1] Cross-Cutting
* **Target Release:** V1

#### [TSK-01.1.1] Key the rate limiter on the real client IP, not the ALB node, and cap its entry count [P: C] [TODO]
`GetClientKey` (SDK `api/request/parser.go`) only consults `X-Forwarded-For` when `TRUSTED_PROXY_DEPTH > 0`, which is unset anywhere in this repo — behind the internet-facing ALB the general 10rps limit becomes a global throttle any one client can exhaust, the 1rps `/v1/accounts/exists` enumeration guard doesn't function, and `ConsentLog.IpAddress` records the ALB's IP instead of the caller's. Fold in the general limiter's missing entry cap (`ratelimit.New` at `cmd/server/setup.go:131` has no `MaxEntries`), which becomes a live unbounded-map risk the moment this lands.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.1.1` | `[Impl]` | Set `TRUSTED_PROXY_DEPTH=1` in the CDK task environment (`deploy/cdk/main.ts`) and add `MaxEntries`/`BucketTTLSecond` to the general limiter config in `cmd/server/setup.go:131` | `go build ./... && cd deploy/cdk && npx vitest run main.test.ts -t "sets TRUSTED_PROXY_DEPTH"` |
| `SUB-01.1.1.2` | `[UnitTest]` | Add a regression test asserting the exists-limiter and general limiter both honor `X-Forwarded-For` at depth 1 and reject a request once the entry cap is hit | `go test -run TestRateLimiter_TrustedProxyDepth ./internal/...` |

#### [TSK-01.1.2] Guard against `ENV=prod\|staging` 500ing every public request [P: C] [TODO]
SDK `api/ratelimit/limiter.go` errors on every `Allow` call in those two envs unless a Redis client is configured; neither limiter has one, `RATE_LIMIT_FAIL_OPEN` is unset, and nothing sets `ENV` today — so the trap is dormant but live the moment someone sets it. Add a boot-time guard (fail loudly, not silently) that refuses to start in `prod`/`stg` without either a configured Redis client or `RATE_LIMIT_FAIL_OPEN=true`.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.2.1` | `[Impl]` | Add the boot-time guard in `cmd/server/setup.go`'s `bootstrap()` | `go build ./...` |
| `SUB-01.1.2.2` | `[UnitTest]` | Assert bootstrap fails fast for `ENV=stg`/`ENV=prod` with no Redis client and no fail-open flag | `go test -run TestBootstrap_RateLimitEnvGuard ./cmd/server/...` |

#### [TSK-01.1.3] Wire request-body size limits into every public and internal chain [P: H] [TODO]
`mw.MaxContentLengthMiddleware` exists in the SDK but is wired into no chain (`internal/server/mux.go`). As of SDK `v0.37.0`, `RedactionMiddleware`/`SanitizationMiddleware` bound their own reads via `http.MaxBytesReader` against `headers.ResolveMaxContentLength()` — no longer the unbounded `io.ReadAll` this task was originally filed against — but with no `MAX_CONTENT_LENGTH` env var set anywhere in this repo, that resolves to the SDK's 4096-byte default, so every public/internal write route now 413s any legitimate body over 4KB until this is wired. `decodeStrict` still has no limit of its own, and `rules.isValidBody` still early-returns for any route with no `body:` rule.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.3.1` | `[Impl]` | Wire `MaxContentLengthMiddleware` into `publicRead`, `publicWrite`, `publicUnauth`, and `internal` chains in `internal/server/mux.go` | `go build ./...` |
| `SUB-01.1.3.2` | `[UnitTest]` | Add a component test asserting an oversized body on `/v1/communications/unsubscribe` is rejected before it reaches the handler | `go test -run TestPublicUnauth_RejectsOversizedBody ./test/component/...` |

#### [TSK-01.1.4] Run the container as non-root and bound its filesystem [P: M] [TODO]
`Dockerfile` has no `USER` directive and defaults to UID 0 on `gcr.io/distroless/base-debian12`; the `:nonroot` variant is unused, and the Fargate task definition sets no `user`/`readonlyRootFilesystem`/`linuxParameters`. Fold in `Dockerfile:11`'s hardcoded `GOARCH=amd64` (confirm against the real Fargate task architecture) and add `-trimpath` plus version stamping while the file is open.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.4.1` | `[Impl]` | Switch to the `:nonroot` distroless variant, add `USER`, set `readonlyRootFilesystem`/`linuxParameters` in `deploy/cdk/main.ts`'s Fargate task def, confirm `GOARCH`, add `-trimpath` | `docker build -f Dockerfile --target build .` |

#### [TSK-01.1.5] Rate-limit and de-cost the unauthenticated `/health/ready` endpoint [P: L] [TODO]
Registered with a bare `mux.HandleFunc` on both muxes (`internal/server/mux.go:57,184`) — no rate limiter, no security headers, no telemetry, no request ID — and routed publicly by the ALB, touching DynamoDB per request (bounded only by a 10s cache).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.5.1` | `[Impl]` | Apply a lightweight rate limiter and telemetry middleware to `/health/ready` in `internal/server/mux.go` | `go test -run TestHealthReady_RateLimited ./test/component/...` |

#### [TSK-01.1.6] Give `/v1/me` group-level middleware instead of per-leaf `.Use(...)` [P: H] [TODO]
Every `/v1/me/*` leaf in `internal/server/mux.go` calls `.Use(publicRead...)`/`.Use(publicWrite...)` individually; the private mux does the opposite (`v1.Use(internal...)`, inherited by all children). A new `/v1/me` route that omits the call registers fully unauthenticated with no compile-time or test signal.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.6.1` | `[Impl]` | Invert `/v1/me`'s registration to group-level `.Use(...)`, matching the private mux's pattern, in `internal/server/mux.go` | `go build ./...` |
| `SUB-01.1.6.2` | `[UnitTest]` | Add a route-table test asserting every `/v1/me/*` route inherits auth middleware even without its own `.Use()` call | `go test -run TestPublicMux_MeGroupInheritsMiddleware ./internal/server/...` |

#### [TSK-01.1.7] Replace the hand-written error switch with a data-driven map, and confirm no `db`-package sentinel leaks through unwrapped [P: M] [TODO]
`internal/api/errors.go`'s `sendAccountError` is still a hand-written `switch`/`errors.Is` chain against `api`-package sentinels; `internal/db/account.go` declares its own duplicate sentinels separately. Most `db` errors now appear wrapped into `api.Err*` at each call site before reaching the switch — confirm none still reach it raw, then replace the switch with one `var errorMap = map[error]httpErr.ErrorCode{...}` iterated via `errors.Is` so the mapping is data.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.7.1` | `[Audit]` | Grep every `internal/db` sentinel's call sites and confirm each is wrapped into an `api.Err*` before reaching `sendAccountError` | `grep -rn "db\.Err" internal/api/*.go` |
| `SUB-01.1.7.2` | `[Impl]` | Replace `sendAccountError`'s switch with a data-driven `errorMap` in `internal/api/errors.go` | `go build ./...` |
| `SUB-01.1.7.3` | `[UnitTest]` | Assert every entry in `errorMap` round-trips through `sendAccountError` to its documented status code | `go test -run TestSendAccountError_ErrorMap ./internal/api/...` |

#### [TSK-01.1.8] Map transaction and conditional-write failure reasons instead of collapsing them to generic 500s [P: H] [TODO]
`AppendConsentLog` has no `ConditionalCheckFailedException` handling, `DeleteAccount`'s `TransactWrite` has no `ConditionFailureIndex` handling, `demoteAndPromoteDefault` maps any failing index to `ErrNotFound`, `CreateAccount` relies on the SDK returning the first failing index for its collision message, and nothing maps `TransactionConflict`/`ThrottlingError`/`ProvisionedThroughputExceeded` — all become unmapped 500s with no retry signal.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.8.1` | `[Impl]` | Add `ConditionFailureIndex`/cancellation-reason handling across `AppendConsentLog`, `DeleteAccount`, `demoteAndPromoteDefault`, `CreateAccount` in `internal/db/` | `go build ./...` |
| `SUB-01.1.8.2` | `[UnitTest]` | Add tests proving a simultaneous profile+email collision on `CreateAccount` returns `ErrEmailAlreadyExists`, and a throttled `TransactWrite` maps to a retryable error, not "not found" | `go test -run TestCreateAccount_TransactFailureMapping ./internal/db/...` |

#### [TSK-01.1.9] Bound every outbound AWS call with a deadline [P: H] [TODO]
No `context.WithTimeout` exists outside bootstrap and the CLI health probe — every handler passes `req.Context()` straight through to DynamoDB, S3, and Secrets Manager clients with no `HTTPClient`/retryer override/`RequestTimeout` configured. A hung dependency is bounded only by AWS SDK defaults.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.9.1` | `[Impl]` | Configure a `RequestTimeout`/`HTTPClient` on the DynamoDB, S3, and Secrets Manager client constructors in `cmd/server/setup.go` | `go build ./...` |
| `SUB-01.1.9.2` | `[UnitTest]` | Assert a hung mock AWS call is cancelled at the configured deadline rather than hanging indefinitely | `go test -run TestAWSClients_RequestTimeout ./cmd/server/...` |

#### [TSK-01.1.10] Scope and roll back idempotency keys [P: M] [TODO]
The SDK keys `CheckAndSet` on the raw `Idempotency-Key` header alone — unscoped by account/method/path/body — so any authenticated caller can squat a key and force another user's 409; it stores before the handler runs and never rolls back on a 5xx, burning the key for the full TTL; a replay returns 409 rather than the cached original response; and the service plane bypasses the middleware entirely.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.10.1` | `[Impl]` | Scope the idempotency key by account+method+path+body hash and roll back the stored key on a non-2xx handler response, in the idempotency middleware wiring in `internal/server/mux.go`/`cmd/server/setup.go` | `go build ./...` |
| `SUB-01.1.10.2` | `[UnitTest]` | Assert two different accounts can reuse the same raw `Idempotency-Key` header without colliding, and a 500 response frees the key for retry | `go test -run TestIdempotency_ScopedAndRolledBack ./test/component/...` |

#### [TSK-01.1.11] Paginate every truncating list read [P: H] [TODO]
Addresses, payments, passkeys, and consent-history reads all use one-page `QueryAs` and discard `LastEvaluatedKey`; `demoteAndPromoteDefault` has no `Limit` at all, so default candidates beyond the first page are never demoted — the exact "two defaults" failure the transactional design exists to prevent. `ExportProfile` assembles the GDPR export from these same calls, so an over-cap account's export silently under-reports.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.11.1` | `[Impl]` | Replace the five call sites (`internal/db/address.go`, `passkeys.go`, `payments.go`, `consent.go`, and `demoteAndPromoteDefault`) with the SDK's `QueryAllAs` paginating helper | `go build ./...` |
| `SUB-01.1.11.2` | `[UnitTest]` | Assert an account with 150 addresses gets a complete export and a correct single-default invariant after demotion | `go test -run TestExportProfile_NoTruncation ./internal/api/...` |

#### [TSK-01.1.12] Add `attribute_exists` conditions so delete/update on a missing sub-resource fails instead of lying [P: M] [TODO]
`DeleteAddress`, `DeletePayment`, `DeleteAccountPreferences`, and `DeletePasskey` all pass a `nil` condition to `DeleteItem` (confirmed still true at `internal/db/address.go:169`), so a delete on a nonexistent resource returns 204; `UpdateAddress` returns 200 without touching DDB when no fields changed; `UpdateAccountPreferences` upserts into a nonexistent account.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.12.1` | `[Impl]` | Add `attribute_exists` conditions to the four delete paths and the two update paths named above | `go build ./...` |
| `SUB-01.1.12.2` | `[UnitTest]` | Assert deleting/updating a nonexistent address/payment/preferences/passkey returns 404, not 200/204 | `go test -run TestDeleteNonexistentSubResource_Returns404 ./internal/api/...` |

#### [TSK-01.1.13] Extract the duplicated 401 preamble and response-write triple [P: M] [TODO]
`grep 'rejected unauthorized request' internal/api/*.go` now hits 30 sites (up from 25 pre-split) across the per-resource handler files; the response-write triple is duplicated the same way. Extract `requireAccountID(wtr, req) (string, bool)` and `respondJSON(wtr, status, v)`.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.13.1` | `[Impl]` | Add `requireAccountID`/`respondJSON` to `internal/api/service.go` and replace all 30 call sites | `go build ./... && grep -c 'rejected unauthorized request' internal/api/*.go \| awk -F: '{s+=$2} END{exit s>1}'` |

#### [TSK-01.1.14] Split path-value-vs-JWT ownership resolution to close the IDOR risk [P: H] [TODO]
Every per-resource handler (`address.go`, `payments.go`, `preferences.go`, `profile.go`, `settings.go`, `passkey.go`, `credentials.go`, `unsubscribe.go`) prefers `req.PathValue("id")` over the JWT subject with no ownership check in the service layer — safe today only because no *public* route registers `{id}` for those handlers, and `{id}` means account ID on the private mux but sub-resource ID on the public one. **Cross-review MUST.**

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.14.1` | `[Impl]` | Split each affected handler into an explicit `/me/*` (JWT-only) path and an `/accounts/{id}/*` (path-value, private-only) path | `go build ./...` |
| `SUB-01.1.14.2` | `[UnitTest]` | Add a test asserting a public-mux request carrying a foreign account's `{id}` in the path cannot read/write that account's data | `go test -run TestPublicHandlers_NoPathIDOverride ./internal/api/...` |

#### [TSK-01.1.15] Lowercase identifiers exported but used only inside their own package [P: L] [TODO]
`AccountAPIErrors` (`internal/models/errors.go:9`) is referenced nowhere outside its own file; `MaxAvatarBytes`/`ValidAvatarContentTypes` are the same shape. Leave `db.Repo` exported (real external callers exist in `cmd/server/main.go`).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.15.1` | `[Impl]` | Lowercase `AccountAPIErrors`, `MaxAvatarBytes`, `ValidAvatarContentTypes` and their references | `go build ./... && go vet ./...` |

#### [TSK-01.1.16] Add an injected clock [P: L] [TODO]
`Service` has no `Clock func() time.Time` field; 13 direct `time.Now()` call sites remain across `internal/api`/`internal/db` (down from 17), coupling timestamp/expiry logic to wall-clock time and blocking deterministic tests.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.16.1` | `[Impl]` | Add `Clock` to `ServiceConfig`, thread it through the remaining 13 call sites | `go build ./... && go test ./internal/... ` |

#### [TSK-01.1.17] Split the 29-method `repository` interface along its resource seams [P: L] [TODO]
The handler/db god-files are already split per-resource; the `repository` interface (`internal/api/repo.go`) is the same problem at the DI boundary — any handler test satisfies the whole interface, and one domain's signature change forces a full mock regen.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.17.1` | `[Impl]` | Split `repository` into per-resource interfaces in `internal/api/repo.go` and regenerate mocks | `go build ./... && go generate ./...` |

#### [TSK-01.1.18] Pin the `oapi-codegen` CLI version and add `generate`/`generate-check` Makefile targets [P: M] [TODO]
`go.mod` only pins the `oapi-codegen/runtime` library, not the CLI, and there is no `generate`/`generate-check` Makefile target — `internal/models/models.gen.go` can drift from `openapi.yaml` silently with nothing in CI to catch it.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.18.1` | `[Impl]` | Pin the `oapi-codegen` CLI version (tool dependency or `go.mod` tool directive) and add `generate`/`generate-check` targets to `Makefile` | `make generate-check` |
| `SUB-01.1.18.2` | `[Impl]` | Add a `generate-check` step to `.github/workflows/ci.yml` | `cd deploy/cdk 2>/dev/null; grep -n "generate-check" ../../.github/workflows/ci.yml` |

#### [TSK-01.1.19] Fix `TTLCache.Set`'s O(n) occupancy count [P: M] [TODO]
`internal/ttlcache/cache.go:50-58` `Range`s the whole map counting entries before every `Store`; both caches are configured with a 100k cap. Benchmarked at 1.5µs empty → 641µs at 50k entries. Lands on the credential/profile cache-miss hot path.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.19.1` | `[Impl]` | Replace the `Range`-count in `internal/ttlcache/cache.go` with an `atomic.Int64` counter incremented/decremented on insert/delete/evict | `go build ./...` |
| `SUB-01.1.19.2` | `[UnitTest]` | Add a benchmark asserting `Set` stays flat (not linear) as occupancy grows to 50k | `go test -bench BenchmarkTTLCache_Set ./internal/ttlcache/...` |

#### [TSK-01.1.20] Fix arbitrary cache eviction, its check-then-act race, and `Get`'s bare type assertion [P: M] [TODO] (after: "Fix `TTLCache.Set`'s O(n) occupancy count")
`evictOne` scans at most 32 entries for an expired one and otherwise deletes an arbitrary `firstKey` (`sync.Map.Range` order is unspecified) — neither LRU nor FIFO, can evict an entry written microseconds earlier. The size check at cap is check-then-act with no atomicity (two goroutines can both evict, or both overshoot). `Get` (`cache.go:39`) still does a bare `v.(entry[V])` unlike the comma-ok form used elsewhere in the file.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.20.1` | `[Impl]` | Replace arbitrary eviction with oldest-first (or LRU) selection, make the cap check atomic, and use comma-ok in `Get` (`internal/ttlcache/cache.go`) | `go build ./...` |
| `SUB-01.1.20.2` | `[UnitTest]` | Assert eviction always removes the oldest entry under concurrent load, never an entry written within the last second | `go test -race -run TestTTLCache_EvictionOrder ./internal/ttlcache/...` |

#### [TSK-01.1.21] Call `TTLCache.Stop()` outside tests [P: L] [TODO]
`Stop()` is only called from `cache_test.go`; `Service` has no `Close()`, so both eviction goroutines run for the life of the process and every `NewService` call leaks two goroutines. `cmd/server/main.go` already drains via `sdkserver.Serve`'s graceful shutdown — tie this in.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.21.1` | `[Impl]` | Add `Service.Close()` calling both caches' `Stop()`, and call it from the graceful-shutdown path in `cmd/server/main.go` | `go build ./...` |
| `SUB-01.1.21.2` | `[UnitTest]` | Assert `NewService` + `Close()` leaves no goroutines running (`goleak` or manual count) | `go test -run TestService_CloseStopsCacheGoroutines ./internal/api/...` |

#### [TSK-01.1.22] Parallelize the export tier's sequential reads [P: L] [TODO]
`gatherProfileExportData` (`internal/api/export.go:128-159`) still runs six sequential `s.repo.*` calls with no `errgroup`. Cold path — take only if touching the file anyway.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.22.1` | `[Impl]` | Parallelize the six reads in `gatherProfileExportData` with `errgroup` | `go test -run TestGatherProfileExportData ./internal/api/...` |

#### [TSK-01.1.23] Reconcile `validateStatus` with its own test's case-sensitivity expectation [P: L] [TODO]
`validateStatus` (`internal/api/settings.go:283-289`) lowercases its input with `strings.ToLower` before matching, so `"ACTIVE"` is accepted; `TestValidateStatus` (`internal/api/settings_test.go`) asserts `"ACTIVE"` is rejected. Pre-existing mismatch, unrelated to and uncovered by the `komodo-forge-sdk-go` upgrade to `v0.38.0` — decide which side is the intended contract (case-insensitive input vs. a stored-value invariant) and fix the other.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.1.23.1` | `[Impl]` | Align `validateStatus` and its test on one case-sensitivity contract | `go test -run TestValidateStatus ./internal/api/...` |

---

### [TG-01.2] Account Profile & Closure
* **Target Release:** V1

#### [TSK-01.2.1] Invalidate the credentials/profile cache on settings changes [P: H] [TODO]
`UpdateSettings` (`internal/api/settings.go:155-179`) never invalidates `credentialsCache`, which caches the settings-derived `status`. A private `PUT /v1/accounts/{id}/settings {status: closed|suspended}` leaves auth-api reading `active` for the full cache TTL. The pattern is already applied elsewhere (`UpdateCredentials`, `SoftDeleteProfile`).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.1.1` | `[Impl]` | Invalidate `credentialsCache`/`profileCache` at the end of `UpdateSettings` in `internal/api/settings.go` | `go build ./...` |
| `SUB-01.2.1.2` | `[UnitTest]` | Assert a settings update to `closed`/`suspended` is immediately reflected in a subsequent credentials read | `go test -run TestUpdateSettings_InvalidatesCache ./internal/api/...` |

#### [TSK-01.2.2] Return an error instead of defaulting to `"active"` when the settings row is missing [P: H] [TODO]
`internal/db/account.go:157-167`: when the `SETTINGS` item is absent from a `BatchGetItem` response, it logs a warning and defaults `status` to `"active"` (`EmailVerified` to `false`). The batch read is not `ConsistentRead`, so this is reachable on eventual consistency, not only genuine absence. This is what makes a half-erased account authenticate normally and read as healthy.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.2.1` | `[Impl]` | Return an error instead of defaulting `status`/`EmailVerified` when the `SETTINGS` item is absent, in `internal/db/account.go:157-167` | `go build ./...` |
| `SUB-01.2.2.2` | `[UnitTest]` | Assert a missing `SETTINGS` row surfaces as an error, not a fabricated `"active"` status, and that a half-erased account (missing `SETTINGS`, present `PROFILE`) fails credential lookup rather than authenticating | `go test -run TestGetAccountCredentials_MissingSettingsErrors ./internal/db/...` |

#### [TSK-01.2.3] Un-brick accounts whose profile row has no readable email [P: H] [TODO]
`buildErasurePlan` (`internal/db/account.go:26,386-415`) returns `errUnreadableProfileEmail` and deletes nothing when the PROFILE record has no readable `email`, so such an account 500s on every closure attempt with no operator signal. Add a CloudWatch metric filter + alarm on the error string, and a runbook entry describing the repair.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.3.1` | `[Impl]` | Add a metric filter + alarm for `failed to read the profile email required for erasure` in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "unreadable profile email alarm"` |
| `SUB-01.2.3.2` | `[Impl]` | Add a `docs/runbooks/` entry describing how to repair the row or manually delete the partition and its `EMAIL#` reservation | `ls docs/runbooks/ \| grep -i unreadable` |

#### [TSK-01.2.4] Return one error code for a failed `confirm_email`, and document the accepted input shape [P: M] [TODO] (Owner: human — needs a product decision on which single code to standardize on)
`DeleteProfileRequest.ConfirmEmail`'s `openapi_types.Email` decode failure returns `httpErr.Global.BadRequest` from `decodeStrict`; a well-formed but wrong address returns `models.Err.InvalidInput` from the service. Both are 400 and both erase nothing, but a BFF can't distinguish the two without parsing the message.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.4.1` | `[Impl]` | Standardize on one error code for both failure paths in `internal/api/profile.go`/`account.go`, and document the accepted input shape (including RFC 5322 display-name normalization) in `openapi.yaml`'s `DeleteProfileRequest` | `go build ./...` |

#### [TSK-01.2.5] Close the cross-replica stale-authentication window after account erasure [P: C] [TODO] (Owner: human — needs a decision on which mitigation to take)
`profileCache`/`credentialsCache` are process-local `sync.Map`s (`internal/ttlcache/cache.go`) with a 60s TTL; only the replica that served the `DELETE` clears its own entries. A customer erased via replica A can still authenticate and read full PII via replica B for up to 60s — and if the freed `EMAIL#` reservation is re-registered inside that window, a stale replica answers a credentials lookup for the *new* signup's email with the *old* account's `password_hash`.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.5.1` | `[Impl]` | Implement the chosen mitigation (shorten credentials-path TTL, skip the cache for credentials entirely, hold the `EMAIL#` reservation for one TTL after erasure, or pull the distributed-cache work forward) | `go build ./...` |
| `SUB-01.2.5.2` | `[UnitTest]` | Add a test proving a credentials lookup against a just-erased account fails within one cache TTL, not 60s later | `go test -run TestErasure_NoStaleReplicaAuth ./internal/api/...` |

#### [TSK-01.2.6] Implement `PUT /v1/me/profile/email` [P: H] [BLOCKED]
Email is silently dropped by `UpdateAccountRequest`, so no route can change it. `UpdateEmailHandler` (`internal/api/profile.go:138-146`) still returns 501 — `openapi.yaml`'s `/v1/me/profile/email` description confirms "NOT IMPLEMENTED — returns 501. Contract reserved ahead of implementation." Must re-check global uniqueness via GSI1, move the email reservation, reset `email_verified` to false, keep the old address until confirmed, emit `account.email_changed`.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | accounts-api does not send email; the confirmation step for a new address must be handed off to auth-api, and that handoff contract isn't agreed yet. |
| Citation | `docs/PRD.md` / ADR 001 D3's export/erasure split (same boundary) |
| Recheck | auth-api publishes (or this team agrees) an email-confirmation handoff contract |

#### [TSK-01.2.7] Implement `GET /v1/me/settings` and `DELETE /v1/me/profile/avatar` [P: M] [TODO]
Both still 501: `GetMySettingsHandler` (`internal/api/settings.go:54-62`) and `DeleteAvatarHandler` (`internal/api/profile.go:181-193`). Settings read must project only `email_verified`/`phone_verified`/`status` — `tags` and `version` must not leak. Avatar delete must clear `Account.avatar_url` **and** delete the S3 object at the account's fixed key, idempotently (204 with no avatar). The shared delete helper (`Service.deleteAvatarObject`/`avatarKey`) already exists from the erasure path — reuse it.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.7.1` | `[Impl]` | Implement `GetMySettingsHandler` as a read-only projection in `internal/api/settings.go` | `go build ./...` |
| `SUB-01.2.7.2` | `[Impl]` | Implement `DeleteAvatarHandler` reusing `deleteAvatarObject`/`avatarKey` in `internal/api/profile.go` | `go build ./...` |
| `SUB-01.2.7.3` | `[UnitTest]` | Assert `GetMySettingsHandler` never returns `tags`/`version`, and `DeleteAvatarHandler` is idempotent (204 on an account with no avatar) | `go test -run TestGetMySettingsHandler_NoLeakedFields\|TestDeleteAvatarHandler_Idempotent ./internal/api/...` |

#### [TSK-01.2.8] Bind Content-Length on the avatar presigned PUT [P: L] [TODO]
`internal/api/profile.go:198-213` validates `MaxAvatarBytes` server-side and passes `contentLength` to `PresignPut`, but SigV4 omits `content-length` from `SignedHeaders` when the signing request carries no body, so the cap is advisory metadata, not an enforced constraint. Content-Type is correctly bound. No longer an unbounded-storage vector since the D4 fixed-key amendment — this is request-cost and single-oversized-object risk only.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.2.8.1` | `[Impl]` | Bind `content-length` into `RequiredSignedHeaders` for the avatar presign in `internal/api/profile.go` | `go test -run TestAvatarPresign_BindsContentLength ./internal/api/...` |

---

### [TG-01.3] Addresses & Payments
* **Target Release:** V1

#### [TSK-01.3.1] Wire `GetAddressHandler`/`GetPaymentHandler` to real repository reads [P: M] [TODO]
Both routes are registered on the mux (`internal/server/mux.go:98-100,116-118`) but the handler bodies still return `httpErr.Global.NotImplemented` (`internal/api/address.go:40-53`, `internal/api/payments.go:40-53`) rather than calling `GetAddress`/`GetPayment`. Add both to the `repository` interface and regenerate mocks. Payment response must stay the display-safe summary only — the PSP token never leaves the private plane.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.3.1.1` | `[Impl]` | Wire `GetAddressHandler`/`GetPaymentHandler` to `GetAddress`/`GetPayment`, add both to `repository`, regenerate mocks | `go build ./... && go generate ./...` |
| `SUB-01.3.1.2` | `[UnitTest]` | Assert `GET /v1/me/addresses/{id}` and `GET /v1/me/payments/{id}` return real data, and the payment response omits the PSP token | `go test -run TestGetAddressHandler_ReturnsData\|TestGetPaymentHandler_NoPSPToken ./internal/api/...` |

#### [TSK-01.3.2] Add per-account creation caps for addresses and payment methods [P: L] [TODO]
No cap exists on create: addresses should cap at 20, payment methods at 10 (lists already truncate at 100 on read; caps keep that truncation unreachable and bound write cost).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.3.2.1` | `[Impl]` | Add a 20-address / 10-payment-method cap check to `AddAddress`/`UpsertPayment` in `internal/api/address.go`/`payments.go` | `go test -run TestAddAddress_CapEnforced\|TestUpsertPayment_CapEnforced ./internal/api/...` |

---

### [TG-01.4] Consent & Communications
* **Target Release:** V1

#### [TSK-01.4.1] Implement `GET`/`POST /v1/me/consents` [P: C] [TODO]
Highest priority in this backlog — GDPR Art. 7(3) requires withdrawal to be as easy as consent, and today a signed-in customer cannot opt in at all and cannot withdraw without a marketing email in hand. `GetConsentsHandler`/`RecordConsentHandler` (`internal/api/consent.go:11-31`) still return 501. Repo layer already exists (`AppendConsentLog`, `ListConsentHistory`, `GetLatestConsent`). Must keep the ledger append-only, capture `recorded_at`/`ip_address`/`user_agent` server-side (never from the client), emit `account.consent_changed`, enforce the `{email,sms,push,postal}` channel enum. **Cross-review MUST.**

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.4.1.1` | `[Impl]` | Implement `GetConsentsHandler`/`RecordConsentHandler` in `internal/api/consent.go` against the existing repo layer | `go build ./...` |
| `SUB-01.4.1.2` | `[UnitTest]` | Assert a recorded consent captures server-side evidence fields, rejects a client-supplied `recorded_at`/`ip_address`, and emits `account.consent_changed` | `go test -run TestRecordConsentHandler_ServerSideEvidence ./internal/api/...` |

#### [TSK-01.4.2] Add durable replay defense to the unsubscribe token [P: M] [TODO]
`VerifyAndRecordUnsubscribe` compares `jti` against `GetLatestConsent` only (`internal/db/consent.go:29`, `Limit:1`), so any intervening opt-in for that channel makes a captured link re-apply the opt-out repeatedly for the full 30-day TTL. Also missing: an account-exists recheck on verify (unlike mint), and `AppendConsentLog`'s `attribute_not_exists(SK)`-only write lets a valid token for a deleted account create an orphan partition. The HMAC construction itself is correct — this is a durability gap, not a cryptography one.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.4.2.1` | `[Impl]` | Add a consumed-`jti` store, an account-exists recheck on verify, and orphan-partition protection in `internal/api/unsubscribe.go`/`internal/db/consent.go` | `go build ./...` |
| `SUB-01.4.2.2` | `[UnitTest]` | Assert a replayed unsubscribe token after an intervening opt-in is rejected, and a token for a deleted account creates no orphan `ConsentLog` partition | `go test -run TestUnsubscribe_NoReplayAfterOptIn ./internal/api/...` |

#### [TSK-01.4.3] Harden consent sort-key generation against same-nanosecond collision and clock skew [P: M] [TODO]
`consentSKTimeFormat` (`internal/db/consent.go:14,117-118`) now uses one consistent fixed-width encoding — the "three different encodings in one package" half is fixed — but two writes for the same account+channel in the same nanosecond still collide on `attribute_not_exists(SK)` (hitting the still-unmapped CCF from `TSK-01.1.8`), and a backwards clock step still inverts what `GetLatestConsent` resolves as "latest."

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.4.3.1` | `[Impl]` | Add a monotonic or collision-resistant suffix to the consent SK in `internal/db/consent.go` | `go build ./...` |
| `SUB-01.4.3.2` | `[UnitTest]` | Assert two consent writes in the same nanosecond for the same account+channel do not collide | `go test -run TestConsentSK_NoCollisionSameNanosecond ./internal/db/...` |

#### [TSK-01.4.4] Mock comms-api in the component and integration tiers [P: M] [TODO]
Comms-api is the confirmed mint caller for the unsubscribe-token route but does not exist yet; no mocked counterpart exists in `test/component/` or `test/integration/` today. STG will point at the live comms-api once it ships — mocking here makes comms-api's existence a STG prerequisite for this route only, not the rest of the deploy.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.4.4.1` | `[Impl]` | Add a service-token-bearing mock caller for the unsubscribe-token mint route in `test/component/` | `go test -run TestUnsubscribeTokenMint_ServiceCaller ./test/component/...` |

---

### [TG-01.5] Auth & Scope Enforcement
* **Target Release:** V1

#### [TSK-01.5.1] Enforce per-group scope allowlists on the private mux (stages 2–3) [P: C] [BLOCKED]
Stage 1 is landed: `JWTScopeObservabilityMiddleware` (`internal/api/middleware/scopes.go:11`) observes and logs every private request's scope ahead of the still-blanket `mw.ScopeMiddleware` check (`internal/server/mux.go:178-179`), which currently accepts any `svc:`-prefixed token regardless of which service it names — a compromised low-value service can read/overwrite password hashes today. Stages 2 (per-group `RequireAnyScope` allowlist, config-driven) and 3 (remove the blanket check) are not implemented. **Cross-review MUST.**

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | The allowlist literals must come from the *observed* `svc:` scope distribution in real staging traffic (a CloudWatch Logs Insights query grouped by `route`/`scopes`), not guessed — picking wrong risks locking out every legitimate caller of a route as sensitive as `/v1/accounts/credentials`. That requires a live STG deployment, which is itself blocked on `TSK-01.7.1`/`TSK-01.7.2`, and `LOG_LEVEL=info` in that environment (`TSK-01.6.3`). |
| Citation | `internal/server/scope_observation.go` (stage 1); `test/component/private_scope_observation_test.go` |
| Recheck | A CloudWatch Logs Insights query against a deployed STG environment returns the real per-route `svc:` scope distribution |

#### [TSK-01.5.2] Pin the scope-naming convention and add a regression test [P: M] [TODO] (after: "Enforce per-group scope allowlists on the private mux (stages 2–3)")
`tagNamespaceMap` and the settings-status check (`internal/api/settings.go`) both match bare service names (`customer-servicing-api`, …); `callerServiceFromScopes` takes the first `svc:`-prefixed scope on an unordered list. If the registry issues a differently-shaped scope (e.g. `svc:komodo-marketing-api`), both allowlists fail closed silently and no test would notice.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.5.2.1` | `[Impl]` | Pin the scope-naming convention once the per-env `client_id` decision's real values are known | `go build ./...` |
| `SUB-01.5.2.2` | `[UnitTest]` | Add a test asserting every `tagNamespaceMap` key matches the pinned convention | `go test -run TestTagNamespaceMap_MatchesConvention ./internal/api/...` |

#### [TSK-01.5.3] Ship the `auth_time` freshness check once the claim exists [P: M] [BLOCKED]
ADR 001 D3 and `openapi.yaml` both record a second confirmation control requiring a fresh `auth_time` claim, but it cannot be built yet — the claim exists in neither auth-api's tokens nor the SDK's `CustomClaims` struct. Shipping the check early would 401 every closure request, the identical fail-closed trap as the SDK JWT-client defect this repo already hit once.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | `CustomClaims` (`komodo-forge-sdk-go/security/jwt/jwt.go`) carries only `scp`, `adm`, and the registered claims — no `auth_time`. Typed-email confirmation already ships as the real control in the interim. |
| Citation | `komodo-forge-sdk-go/security/jwt/jwt.go` `CustomClaims` struct |
| Recheck | `CustomClaims` gains an `AuthTime` field and auth-api populates it in issued tokens |

---

### [TG-01.6] Observability
* **Target Release:** V1

#### [TSK-01.6.1] Close the alarm coverage gaps beyond the notification-wiring fix [P: M] [TODO]
`buildAccountAlarms` (`deploy/cdk/main.ts:234-267`) defines exactly 3 alarms. Nothing alarms on: 4xx rate (the only 404 filter matches private-plane routes that never reach the ALB), DynamoDB throttles/system errors, DynamoDB stream iterator age, WAF blocked requests (metrics emitted, nothing consumes them), ALB rejected connections/target connection errors, task restart/deployment failure (no `deploymentCircuitBreaker`, no ECS-events rule), bootstrap crash-loop, certificate expiry.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.6.1.1` | `[Impl]` | Add the missing alarms to `buildAccountAlarms` in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "alarm coverage"` |

#### [TSK-01.6.2] Carry a request/correlation ID on every application log line [P: M] [TODO]
None of the ~56 `logger.Info/Warn/Error` sites across `internal/api/*.go` carry a request-ID attribute; correlating an application error line to its telemetry line requires timestamp guessing. Nine typed 4xx branches in `internal/api/errors.go` log nothing at all. `scopeObservationMiddleware`'s log line already carries `request_id` — use it as the pattern.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.6.2.1` | `[Impl]` | Add `request_id` to every `logger.*` call site in `internal/api/*.go`, and add logging to the nine silent 4xx branches in `errors.go` | `go build ./...` |
| `SUB-01.6.2.2` | `[UnitTest]` | Assert a sampled log line from each handler carries `request_id` | `go test -run TestHandlerLogs_CarryRequestID ./internal/api/...` |

#### [TSK-01.6.3] Set `LOG_LEVEL` deliberately per environment instead of relying on the SDK default [P: M] [TODO]
`bootstrap()` reads `LOG_LEVEL` from an env var never set in the CDK task environment (`cmd/server/setup.go:87`, `deploy/cdk/main.ts`'s env block), so deployed environments log at INFO by omission. Two per-request INFO lines depend on that default, including the scope-observation line `TSK-01.5.1` is blocked on. Decide deliberately whether staging stays INFO (it must, until stage 2 has its data) and whether prod needs the scope line at all.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.6.3.1` | `[Impl]` | Set `LOG_LEVEL` explicitly in `deploy/cdk/main.ts`'s environment block, per environment | `cd deploy/cdk && npx vitest run main.test.ts -t "LOG_LEVEL"` |

#### [TSK-01.6.4] Delete the duplicate private-mux test harness [P: L] [TODO]
`newScopeHarness` (`test/component/private_scope_observation_test.go:55`) duplicates the RSA key/`jwt.Client`/service/mock wiring already in `newHarness` (`test/component/suite_test.go:62`) because it needs a way to mint a token with arbitrary scopes and `suite_test.go` was outside the writable surface when stage 1 landed.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.6.4.1` | `[Impl]` | Add a scope-parameterized token minter to `newHarness`, point `newScopeHarness`'s callers at it, delete the duplicate | `go test ./test/component/...` |

#### [TSK-01.6.5] Point the ALB health check at `/health/ready`, not the shallow `/health` [P: M] [TODO]
`HEALTH_CHECK_PATH` (SDK default `/health`) feeds the ALB target-group health check (`deploy/cdk/main.ts:157`) and returns 200 unconditionally; `/health/ready` — the endpoint that would catch DynamoDB being unreachable — is routed by the ALB but never polled by it. Also register the SDK's `S3Checker` (unregistered today, so the export/avatar paths are unmonitored).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.6.5.1` | `[Impl]` | Point the ALB target-group health check at `/health/ready` and register `S3Checker` alongside the existing `DynamoDBChecker` | `cd deploy/cdk && npx vitest run main.test.ts -t "health check path"` |

---

### [TG-01.7] Deploy & Infra (CDK)
* **Target Release:** V1

#### [TSK-01.7.1] Provision real ACM certificate ARNs [P: C] [BLOCKED]
`deploy/cdk/main.ts` still carries literal `PLACEHOLDER-acm-cert-arn-<region>` strings for both STG and PROD, consumed by `acm.Certificate.fromCertificateArn` — `cdk deploy` fails at CloudFormation.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Real certificate ARNs are an external prerequisite (lead time for issuance), plus the AWS account ID and a `komodo-stg`-tagged VPC. |
| Citation | `deploy/cdk/main.ts` (STG/PROD cert ARN literals) |
| Recheck | No `PLACEHOLDER-acm-cert-arn` string remains in `deploy/cdk/main.ts` |

#### [TSK-01.7.2] Set a real `ALARM_EMAIL` and confirm the SNS subscription [P: H] [BLOCKED]
Alarm→SNS wiring is landed (`AlarmNotificationAspect`, `deploy/cdk/main.ts:213-232`, attaches to all alarms automatically), but `ALARM_EMAIL` still defaults to `PLACEHOLDER-alarm-email@komodo.com` (`main.ts:41`) — an unconfirmed subscription delivers nothing.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Needs a real ops distribution list address and a human to confirm the resulting SNS email subscription. |
| Citation | `deploy/cdk/main.ts:41` |
| Recheck | `ALARM_EMAIL` is a real address in `main.ts` and the SNS subscription shows `Confirmed` |

#### [TSK-01.7.3] Add a pre-drain step so deploys stop dropping requests [P: H] [TODO]
No pre-drain step exists: ECS deregisters the target and sends SIGTERM concurrently, nothing flips `/health/ready` to unready first, and the process keeps accepting connections while the ALB still routes to it. `deregistrationDelay` is unset on both target groups (AWS default 300s, a 10x mismatch against the 30s Go shutdown budget); ECS `stopTimeout` is unset (defaults to exactly the Go budget, making a clean shutdown and a SIGKILL indistinguishable in the logs); no `healthCheckGracePeriod`.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.3.1` | `[Impl]` | Add a pre-drain hook (flip `/health/ready` unready on SIGTERM before closing connections), set `deregistrationDelay`, `stopTimeout` > Go shutdown budget, and `healthCheckGracePeriod` in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "deregistration delay"` |

#### [TSK-01.7.4] Fix the WAF per-path rate-limit prefixes [P: L] [TODO]
The `WafWebAcl`'s `rateLimitRules` (`deploy/cdk/main.ts:204-207`) target `/v1/profile/` and `/v1/addresses/`; the real prefixes are `/v1/me/profile` and `/v1/me/addresses`. Both rules are dead — only the global limit applies.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.4.1` | `[Impl]` | Correct the path prefixes in `deploy/cdk/main.ts`'s `rateLimitRules` | `cd deploy/cdk && npx vitest run main.test.ts -t "WAF rate limit"` |

#### [TSK-01.7.5] Remove the duplicate target-group registration [P: M] [TODO]
The SDK's `httpsListener.addTargets('Target')` already registers the ECS service via one target group; `deploy/cdk/main.ts:141-158` overrides the listener's `DefaultActions` to a fixed 404 (orphaning that target group — it still exists, still health-checks, receives zero traffic) and registers the same service again in a second target group.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.5.1` | `[Impl]` | Remove the orphaned first target group; keep only the one actually receiving traffic | `cd deploy/cdk && npx vitest run main.test.ts -t "target group"` |

#### [TSK-01.7.6] Document (or derive) the public-route/listener-rule coupling [P: L] [TODO]
`deploy/cdk/main.ts:160` is a fixed 5-path allowlist with a fixed-404 default action — a new public route works locally and 404s in AWS with no signal.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.6.1` | `[Impl]` | Either document the coupling prominently next to the public route table, or derive the allowlist from the route registrations | `grep -n "publicPaths" deploy/cdk/main.ts` |

#### [TSK-01.7.7] Retain and CMK-encrypt the application log group [P: H] [TODO]
`deploy/cdk/main.ts:330-334`: the `LogGroup` is `RemovalPolicy.DESTROY` in every environment on a stack tagged `dataClassification: 'pii'`, unlike the accounts table and both buckets, which correctly `RETAIN`. No `encryptionKey` is passed, so logs are not CMK-encrypted.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.7.1` | `[Impl]` | Switch the log group to `RemovalPolicy.RETAIN` and pass a CMK `encryptionKey` in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "log group retention"` |

#### [TSK-01.7.8] Add a memory-based (or request-based) autoscaling policy and a scaling floor above one task [P: M] [TODO]
`main.ts:119` sets `desiredCount = minCapacity = 1`, and the vendored SDK's Fargate construct only offers a single CPU-70% scaling policy — no memory policy, no `ALBRequestCountPerTarget`, no cooldowns, no scheduled scaling. The prod baseline is a single task in a single AZ against a 99.9% availability target.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.8.1` | `[Impl]` | Raise `minCapacity`/`desiredCount` above 1 for PROD and add a second scaling metric in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "autoscaling"` |

#### [TSK-01.7.9] Add CORS configuration to both S3 buckets [P: M] [TODO]
Neither S3 bucket (`deploy/cdk/main.ts:288-318`) has a CORS configuration, but the avatar flow issues a presigned PUT intended for a browser-side upload — a cross-origin browser PUT fails preflight regardless of the API's own posture.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.9.1` | `[Impl]` | Add a CORS rule scoped to the expected origin(s) on the avatars bucket in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts -t "bucket CORS"` |

#### [TSK-01.7.10] Set the 14 tuning env vars explicitly in the CDK task environment [P: M] [TODO]
`JWT_AUDIENCE_USER`/`_SERVICE`, `BOOTSTRAP_TIMEOUT`, five `SERVER_*_TIMEOUT`s, `S3_PRESIGN_TTL`, `UNSUBSCRIBE_TOKEN_TTL`, `PROFILE_CACHE_TTL`, `PROFILE_CACHE_MAX_ENTRIES`, `EXISTS_RATE_LIMIT_*`, and `LOG_LEVEL` are all settable in code but absent from `deploy/cdk/main.ts:127-138`'s environment block — every environment runs on hardcoded defaults, including `SERVER_SHUTDOWN_TIMEOUT`, which is the drain window `TSK-01.7.3` depends on.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.10.1` | `[Impl]` | Add the 14 tuning vars to `deploy/cdk/main.ts`'s Fargate task environment block, with real per-environment values | `cd deploy/cdk && npx vitest run main.test.ts -t "task environment"` |

#### [TSK-01.7.11] Get `cdk synth` to run in a credential-less/context-pinned mode [P: L] [TODO]
`Vpc.fromLookup` (`deploy/cdk/main.ts:336`) needs live AWS credentials to resolve, and `deploy/cdk/cdk.context.json` is still `{}` — `cdk synth` exits non-zero without credentials, which blocks gating this in CI (`TSK-01.8.4`).

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.11.1` | `[Impl]` | Commit real VPC lookup context to `deploy/cdk/cdk.context.json`, or add a credential-less synth path | `cd deploy/cdk && npx cdk synth` |

#### [TSK-01.7.12] Clean up region-handling, `isProd` naming, synth error-swallowing, and Cloud Map's discarded args [P: L] [TODO]
`main.ts:133` hardcodes the container's `AWS_REGION` to `cfg.regions[0]` while `:379` allows a `--context region` override — override the region and the app talks to the wrong one; the second configured region is declared and never deployed. The `isProd = env !== 'dev'` naming (`:270,297,313`) is misleading (behavior is correct: STG and PROD both get RETAIN/deletion-protection). `:370-383` swallows the synth stack trace into `console.error` before `process.exit(1)`. `associateCloudMapService({ containerPort: 7052 })` (`:175-183`) silently discards both arguments for `DnsRecordType.A` — only the task ENI IP is registered, with no health-check config on the Cloud Map service.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.7.12.1` | `[Impl]` | Fix the region-override mismatch, rename `isProd`, stop swallowing the synth stack trace, and add Cloud Map health-check config in `deploy/cdk/main.ts` | `cd deploy/cdk && npx vitest run main.test.ts` |

#### [TSK-01.7.13] SDK: `SnsTopic` ignores `protocol`, so SMS alarm delivery silently becomes email [P: L] [BLOCKED]
Vendored `komodo-forge-sdk-ts`'s `snsTopic.js` always constructs an `EmailSubscription` regardless of each subscription's `protocol` field. Not blocking here — email covers the on-call path for V1 — but the 2026-07-28 "email/SMS" decision on alarms is only half-true today.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Fix belongs in `komodo-forge-sdk-ts`'s `SnsTopic` construct, not this repo. |
| Citation | vendored `komodo-forge-sdk-ts` `snsTopic.js` |
| Recheck | `deploy/cdk/package.json`'s pinned SDK version ships a `SnsTopic` that dispatches on `protocol` |

#### [TSK-01.7.14] SDK: `MetricFilterAlarm` exposes no `alarmActions` prop [P: L] [BLOCKED]
Vendored `metricFilterAlarm.js` still has no `alarmActions`/`okActions` prop — not blocking today since the alarm-notification aspect attaches the topic regardless, but any future per-alarm routing (a different topic for a different severity) has no prop to use.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Fix belongs in `komodo-forge-sdk-ts`'s `MetricFilterAlarm` construct. |
| Citation | vendored `komodo-forge-sdk-ts` `metricFilterAlarm.js` |
| Recheck | `deploy/cdk/package.json`'s pinned SDK version ships `alarmActions`/`okActions` on `MetricFilterAlarm` |

#### [TSK-01.7.15] SDK: bump past the unreleased ALB-alarm-dimension fix [P: M] [BLOCKED]
`deploy/cdk/package.json:26` still pins `komodo-forge-sdk-ts#v0.6.1`. Two defects are fixed in that SDK's working tree but unreleased: `fargate.ts` used the wrong ALB identifier for `UnhealthyTargetsAlarm`/`High5xxErrorAlarm` (they could never fire), and `alarm.ts` silently discarded `alarmActions`/`okActions`/`insufficientDataActions` passed to `cloudwatch.Alarm`.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Requires `komodo-forge-sdk-ts` to commit, tag, and release past `v0.6.1`. |
| Citation | `deploy/cdk/package.json:26` |
| Recheck | `deploy/cdk/package.json` pins a `komodo-forge-sdk-ts` version past `v0.6.1`, and `cdk synth` shows the two ALB alarms' dimensions resolve to `LoadBalancerFullName` |

---

### [TG-01.8] Test Infrastructure
* **Target Release:** V1

#### [TSK-01.8.1] Build out the integration test tier [P: H] [TODO]
`test/integration/` is still an empty directory. ADR 001 D11 requires both tiers before staging — target what mocks structurally can't cover: `TransactWriteItems` cancellation-reason mapping, conditional-write guards, `BatchGetItem` `UnprocessedKeys` under throttling, GSI1 email lookup, optimistic-concurrency version conflicts, lexicographic SK ordering. Design constraint: one LocalStack container per package (`TestMain`), per-test table naming for parallel runs, no cross-test ordering assumptions.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.8.1.1` | `[Impl]` | Add `testutil.Integration(t)` and a `TestMain`-scoped LocalStack DynamoDB container in `test/integration/` | `TEST_TIER=integration go test ./test/integration/...` |
| `SUB-01.8.1.2` | `[IntegTest]` | Write the first integration test proving `TransactWriteItems` cancellation-reason mapping against a real DynamoDB (LocalStack) | `TEST_TIER=integration go test -run TestTransactWrite_CancellationReasonMapping ./test/integration/...` |

#### [TSK-01.8.2] Decide whether `internal/server` needs its own coverage-floor entry [P: L] [TODO] (Owner: human)
Moving the mux builders out of `cmd/server` into `internal/server` moved them from CI's `/cmd/` exemption into the 80% default floor. `internal/server` currently measures 100%, so nothing blocks today, but any future untested wiring added there now risks blocking CI with no explicit, intentional floor recorded.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.8.2.1` | `[Impl]` | Add an explicit floor entry for `internal/server` in `.github/workflows/ci.yml`, alongside `internal/api`/`internal/db` | `grep -n "internal/server" .github/workflows/ci.yml` |

#### [TSK-01.8.3] Raise the `internal/db` coverage floor off its 41% ratchet [P: M] [TODO]
`.github/workflows/ci.yml` still pins ratchet floors at `internal/api`=76, `internal/db`=41 — a floor at the status quo, not a standard. `internal/db` is the largest untested surface. Decide whether 80% is the right bar for a data layer that's mostly thin DynamoDB marshalling, or whether it deserves an honest, explicit lower floor instead of an aspirational 80.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.8.3.1` | `[Impl]` | Add tests to raise `internal/db` coverage and update its floor in `.github/workflows/ci.yml` | `go test -coverprofile=coverage.out ./internal/db/... && go tool cover -func=coverage.out \| tail -1` |

#### [TSK-01.8.4] Add CDK synth/vitest/docker-build steps to CI [P: M] [TODO]
`.github/workflows/ci.yml` runs Go only (build/vet/lint/test/coverage/govulncheck). The CDK vitest suite exists and is never run in CI; there is no `cdk synth`, `docker build`, Dockerfile lint, or deploy job — which is exactly why the ACM placeholder and the local-dev breakage went undetected as long as they did. (after: "Get `cdk synth` to run in a credential-less/context-pinned mode")

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.8.4.1` | `[Impl]` | Add `cd deploy/cdk && npx vitest run`, `cdk synth`, and `docker build` steps to `.github/workflows/ci.yml` | `grep -n "vitest run\|cdk synth\|docker build" .github/workflows/ci.yml` |

#### [TSK-01.8.5] SDK: idempotency store is a process-wide global, shared across all tests in a package [P: L] [BLOCKED]
`api/idempotency/idempotency.go:25`, `var defaultStore *Store`. A component test reusing one `Idempotency-Key` across write-route tests gets a 409 instead of the real handler status — already worked around locally with a fresh random key per request. Worth considering whether `IdempotencyMiddleware` should take an injected store rather than a package singleton.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | The store is a package-level global in `komodo-forge-sdk-go`; an injectable-store API change belongs in the SDK. |
| Citation | `komodo-forge-sdk-go` `api/idempotency/idempotency.go:25` |
| Recheck | The SDK ships an injected-store constructor for `IdempotencyMiddleware` |

---

### [TG-01.9] Local Dev & Docs
* **Target Release:** V1

#### [TSK-01.9.1] Fix local development end-to-end (`make build`/`make run`) [P: H] [TODO]
Confirmed still broken on four independent fronts: (1) `docker-compose.yaml`'s `context: ..` plus a nonexistent `../../infra/local/env.shared` `env_file` on both services; (2) `BUILD_TARGET: public|private` is consumed by nothing — `Dockerfile` declares no such `ARG`, both images are byte-identical; (3) the public service's env block omits `PORT_PRIVATE`, the private service's omits `PORT`/`EVAL_RULES_PATH`, and neither sets `S3_EXPORT_BUCKET`/`S3_AVATAR_BUCKET`; (4) `Makefile`'s `APP_NAME := komodo-user-api` (wrong service), `AWS_REGION ?= us-east-1` (vs `us-east-2` elsewhere), and `AWS_SECRET_PREFIX` exported while the code reads `AWS_SECRET_PATH` and compose reads `SECRET_PATH`.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.9.1.1` | `[Impl]` | Fix `docker-compose.yaml`'s build context, `env_file` path (or remove/create it), `BUILD_TARGET` consumption, and the missing env vars on both services | `docker compose -f docker-compose.yaml config` |
| `SUB-01.9.1.2` | `[Impl]` | Fix `Makefile`'s `APP_NAME`, `AWS_REGION` default, and the `AWS_SECRET_PREFIX`/`AWS_SECRET_PATH`/`SECRET_PATH` naming mismatch | `make build` |

#### [TSK-01.9.2] Confirm whether `test/e2e/suite_test.go`'s package header is stale or intentional [P: L] [TODO] (Owner: human — user-authored comment, never delete without the user's own removal)
The header still documents an unrelated address-validation provider, an `ADDRESS_PROVIDER_API_KEY` secret, and a `just up api` command — none of which belong to accounts-api.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.9.2.1` | `[Audit]` | Confirm with the user whether the header is copy-paste drift or describes a planned future test target; update only if they say so | `head -20 test/e2e/suite_test.go` |

#### [TSK-01.9.3] Resolve the remaining `NEEDS DECISION` markers in `docs/PRD.md` [P: L] [TODO] (Owner: human — product/business decisions)
Five are still open: traffic-volume target, guaranteed-capacity agreements, per-instance concurrency target, load/failure-injection testing, and the staged rollout/rollback process — the last of which has real STG consequences, since there is currently no documented way to reverse a bad release beyond redeploying a previous image and no deployment circuit breaker in the CDK.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.9.3.1` | `[Audit]` | Bring each `NEEDS DECISION` marker to the user for a decision, then update `docs/PRD.md` | `grep -c "NEEDS DECISION" docs/PRD.md` |

#### [TSK-01.9.4] Finish the Go 1.27 migration and Dockerfile version pin [P: L] [TODO]
`go.mod`/`Dockerfile` bump to `1.27`/`golang:1.27.0` is present but still uncommitted in the working tree (`git status`) — commit it, then audit for new stdlib idioms, deprecated APIs, and toolchain-directive changes introduced in 1.27.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.9.4.1` | `[Impl]` | Commit the Go 1.27 bump and audit for deprecated APIs / new idioms | `go build ./... && go vet ./...` |

#### [TSK-01.9.5] Compute presigned-URL `expires_at` before signing, not after [P: L] [TODO]
Both the export download link (`internal/api/export.go:114,119`) and the avatar upload link compute `expires_at` after the presign call returns, overstating validity by the round-trip duration.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.9.5.1` | `[Impl]` | Capture the timestamp before calling `PresignGet`/`PresignPut` in `internal/api/export.go`/`profile.go` | `go build ./...` |

#### [TSK-01.9.6] SDK: guard the Secrets Manager blob cache and the `AWS_ENDPOINT` test-credential swap [P: L] [BLOCKED]
The Secrets Manager client caches the entire secret JSON blob and only then filters to the requested keys, so every key in the bundle sits in process memory for the cache TTL. Separately, `AWS_ENDPOINT`, if ever set in a deployed environment, makes the S3 and Secrets Manager clients swap in static `"test"/"test"` credentials — local-only by design, but worth a guard against accidental deployment.

| Field | Value |
|---|---|
| Blocked by | `external` |
| Reason (2026-09-15) | Both behaviors live in `komodo-forge-sdk-go`'s AWS client constructors, not this repo. |
| Citation | `komodo-forge-sdk-go/aws/secretsmanager/client.go`, `aws/s3/client.go` |
| Recheck | The SDK filters secrets before caching, and refuses (or warns loudly on) `AWS_ENDPOINT` outside a recognized local/test env |

---

### [TG-01.10] Quality Assurance & Epic Hardening
* **Target Release:** V1
* **Trigger:** Run once all functional Task Groups in EPIC-01 reach `[DONE]`.

#### [TSK-01.10.1] Security review [P: H] [TODO]
| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.10.1.1` | `[Audit]` | Run `/assess-security` against the full repo | `/assess-security` completes with no unresolved Critical/High findings |

#### [TSK-01.10.2] Bug sweep [P: H] [TODO]
| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.10.2.1` | `[Audit]` | Run `/assess-bugs` against the full repo | `/assess-bugs` completes with no unresolved Critical/High findings |

#### [TSK-01.10.3] Code smell [P: M] [TODO]
| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.10.3.1` | `[Audit]` | Run `/assess-simplify` and `/assess-code-quality` against the full repo | Both complete with no unresolved Critical/High findings |

#### [TSK-01.10.4] Performance [P: M] [TODO]
| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-01.10.4.1` | `[Audit]` | Run `/assess-performance` against the full repo | `/assess-performance` completes with no unresolved Critical/High findings |

---

## [EPIC-02] Next, V2
*Goal: scale-out and hardening work deliberately deferred past V1.*

### [TG-02.1] Cross-Cutting
* **Target Release:** V2

#### [TSK-02.1.1] Distributed idempotency via regional Redis/ElastiCache [P: L] [TODO]
`docs/adr/002-v2-target-state.md`. V1 stays local; V2 swaps to `NewDistributedStore` against one shared cluster per env/region. Note: the SDK's `NewStore(mode, ...)` currently ignores the `mode` argument entirely (always in-process), so this is not a one-line string change.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-02.1.1.1` | `[Impl]` | Wire `NewDistributedStore` once the SDK honors `mode` | `go build ./...` |

#### [TSK-02.1.2] Cursor pagination on list endpoints [P: L] [TODO]
Current per-account counts sit well under the 100-item read cap (`TSK-01.1.11` already removes silent truncation without changing the contract). Revisit when usage data shows any list over 50 items, bulk-admin/CS views land, or long-tenured accounts' `ConsentLog` growth triggers truncation.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-02.1.2.1` | `[Impl]` | Add cursor-based pagination to the public list endpoints | `go build ./...` |

#### [TSK-02.1.3] Implement CORS with a strict origin allowlist before any browser SPA calls this API [P: L] [TODO]
SDK CORS middleware is a no-op stub today — correctly low priority while every caller is a server-side front end. Re-raise the moment a browser SPA is pointed at a Komodo API, and implement with a strict allowlist, not a permissive header under deadline pressure.

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-02.1.3.1` | `[Impl]` | Implement CORS with a strict origin allowlist once a browser SPA caller is confirmed | `go build ./...` |

#### [TSK-02.1.4] Wishlist: sharable named lists (gift registry / family sharing) [P: L] [TODO]
Own DynamoDB table, visibility controls, public share URL. Do not conflate with cart-api's "saved for later."

| Subtask | Category | Work | Done when |
|---|---|---|---|
| `SUB-02.1.4.1` | `[Impl]` | Design and implement sharable named lists as a new feature | `go build ./...` |

---

## Archive
*Note: use this section strictly for abandoned, shelved, or deprecated initiatives — never for finished work, which is swept out on completion, not archived.*
