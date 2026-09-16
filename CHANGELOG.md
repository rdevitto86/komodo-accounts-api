# Changelog

All notable changes to this project are documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/). This
project is pre-1.0 (`0.x.x`): minor versions cover large and/or multi-feature
changes, patch versions cover small, non-breaking changes.

## [Unreleased]
### Changed
- Bumped `komodo-forge-sdk-go` from `v0.27.2` to `v0.38.0` and dropped the `replace` directive that pointed it at a local checkout, per `standards-go`'s pinned-published-version rule.
- `internal/server/mux.go` now handles the `api/routes.NewRouter`/`(*RouteGroup).Group` signature change (`(*RouteGroup, error)`) via a `mustGroup` helper that panics on the startup-only, can't-happen error case.
- `internal/api/profile.go`/`settings.go` now read the authenticated subject and scopes via the SDK's `GetSubject`/`GetScopes` context accessors instead of reaching for the now-renamed `USER_ID_KEY`/`SCOPES_KEY` constants directly.
- `cmd/server/setup.go` now owns `JWT_PUBLIC_KEY`/`JWT_ISSUER` as local Secrets Manager key constants — the SDK's `security` package no longer exports them — and uses the SDK's PascalCase `http` package constants (`RateLimitRPS`, `RateLimitBurst`, `IdempotencyTTLSec`) in place of the removed `SCREAMING_SNAKE_CASE` ones.
- Regenerated `test/mocks/mock_dynamodb.go` for the SDK's new `aws/dynamodb.API.UpdateWithVersion` method.
### Fixed
- `cmd/server/setup_test.go` fixtures were missing `ACCOUNTS_API_CLIENT_SECRET`, failing `setBootstrapSecrets`'s required-key check independent of the SDK bump.

## [0.8.3] - 2026-09-15
### Added
- Outbound service-JWT contract implemented: `internal/clients/auth.go` and `internal/clients/events.go` are real clients (JWT-authenticated auth-api calls, event publishing) rather than one-line stubs.
- A unit test asserting a denied S3 avatar-delete surfaces as a 500 and leaves the DynamoDB account rows untouched.
### Fixed
- WAF rule construction now uses the SDK's typed `rateLimitRules` prop instead of a hardcoded `Rules.5` index override, removing a silent-overwrite risk on future rule changes.
### Changed
- `openapi.yaml` now documents that `email`/`username` are immutable via `PUT /v1/me/profile`.
- Replaced `TODO.md` with `BACKLOG.md`: re-derived every open item against current code (much of `TODO.md` predated the 0.8.0 handler/db refactor and cited moved or already-fixed code) and reshaped it into the epic/task-group/task hierarchy the toolkit's backlog skill expects.

## [0.8.2] - 2026-08-18
### Changed
- Added CDK `node_modules` exclusion and a formatters section to the golangci-lint config.

## [0.8.1] - 2026-08-10
### Fixed
- Unblocked pre-push lint: module boundary violations, complexity, and gosec findings.

## [0.8.0] - 2026-08-10
### Changed
- Consolidated individual request/response handlers into per-resource modules (address, credentials, consent, export, passkey, payments, preferences, settings, unsubscribe).
- Simplified `db/account.go`: removed the conversion layer, inlined types, cut 1000+ LOC.
- Removed the in-process cache; DB-backed expiry is now the primary strategy.
- Pruned documentation (HLD, data-model, domain-events consolidated into `sdd.md`).
- Updated validation rules and generated models.
- Tightened linter rules.

## [0.7.0] - 2026-07-30
### Added
- New placeholder routes.
### Changed
- Cleaned up setup/config files and documentation.

## [0.6.0] - 2026-07-28
### Changed
- Switched models to `oapi-codegen`.
- Added config and mocks.
- Hardened CI and infrastructure.

## [0.5.1] - 2026-07-21
### Changed
- Deleted stray `internal/.DS_Store`.

## [0.5.0] - 2026-07-20
### Added
- `versioned()`/`versionedPaths()` route helpers so route paths are generated from a version list instead of hardcoded `/v1/...` literals, across the public route table, private route table, two standalone public routes, and the internal unsubscribe-token route (30 routes total). Adding a v2 for an unchanged route is now a one-line edit.
### Fixed
- Replaced the silent port fallback in `main.go` with a hard failure when `PORT`/`PORT_PRIVATE` aren't set. The old fallback (5051/5052) was stale against the deployed ports (7051/7052), so a broken env injection would previously bind silently to the wrong port instead of failing loudly on boot.

## [0.4.0] - 2026-07-04
### Changed
- Migrated CDK to `forge-sdk-ts` v0.5.2.
- Dropped the DEV stack.
- Tuned build performance.

## [0.3.0] - 2026-06-28 – 2026-06-29
V1 API launch: Customer domain, GDPR/consent, unsubscribe, testing & CI.

### Added
- Renamed the user domain to customer domain-wide: `CUSTOMER#` DynamoDB PK prefix, `cust_<KSUID>` IDs, customers table rebuilt in CDK with streams/PITR/KMS/GSI1, S3 exports bucket.
- `AccountSettings` entity (status, tags with namespace ACL, segments) and append-only `ConsentLog`.
- GDPR profile export to S3 with 15-minute pre-signed URLs.
- Stateless HMAC unsubscribe token mint/verify, and S3 blob cleanup on account erasure.
- Repository interface in the `api` package to enable mock-based unit testing; 55 tests covering all handlers and security-critical paths (IDOR, token redaction, GDPR delete, HMAC unsubscribe, tag namespace ACL).
- GitHub Actions CI gate (build/vet/lint/race/govulncheck/coverage floor).
- Completed `openapi.yaml` with settings, consent, export, and unsubscribe endpoints.
- Partial PUT for addresses and preferences (partial DTO updates).
- 30-day TTL for opt-out tokens.
- Communications channel allowlist.
- Avatar image upload to S3 (presign) via SDK.
- Domain events and CDC rules.
### Changed
- Finalized User → Customer rename across the domain.
- DynamoDB concurrency enhancements.
- Moved unsubscribe/comms routes to `/communications`.
- Combined public and private Fargate containers onto a single container (with CDK changes).
- Aligned PRD, HLD, ADR 001, and `AGENTS.md` to current state; stripped stray comments.
### Removed
- Removed middle initial from the DB schema.

## [0.2.2] - 2026-06-26
### Changed
- Removed ignored files from version control.

## [0.2.1] - 2026-06-26
### Changed
- Updated `.gitignore`.

## [0.2.0] - 2026-06-26
### Fixed
- **IDOR:** `UpdateAddressHandler` and `DeleteAddressHandler` were passing the address ID as the user ID; `DeletePaymentHandler` had the same bug with the payment ID. All three now use `userIDFromJWT` exclusively. Replaced `resolveUserID` with `userIDFromJWT` (JWT context only) and `userIDFromPath` (path value only); shared handlers use an inline path-then-JWT fallback; passkey handlers (private-only) use `userIDFromPath`.
- **Atomic delete:** `DeleteUser` migrated from Query+BatchDelete to `TransactWriteItems` in 100-item chunks, achieving process-crash atomicity for typical accounts; injected a `transactDDBAPI` interface into `Repo` for testability.
- **Unbounded reads:** `GetUserAddresses`, `ListPayments`, `GetUserPasskeys` capped at 100 items via `QueryAs` `Limit` to prevent unbounded RCU reads.
### Security
- Added a per-IP strict rate limiter (1 RPS / burst 5) on `GET /v1/users/exists` only, independent of the global rate limiter.
- Log redaction audit passed; no PII in log values.

## [0.1.0] - 2026-06-24
### Added
- Initial migration from monorepo.
