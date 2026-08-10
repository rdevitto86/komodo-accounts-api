# ADR 002 — V2 Target State

- **Status:** Draft (V2 scope collects here as decisions are made; sequencing begins once V1 is prod-validated)
- **Deciders:** rad
- **Supersedes:** —
- **Related:** ADR 001 (V1 target state), `TODO.md` § Deferred / future (V2+), auth-api ADR 005 (same pattern)

## Context

V1 ships with deliberate deferrals (cursor pagination, testcontainers integration tier, sharable
lists — see `TODO.md` § V2). This ADR is the landing place for V2 target-state decisions so they
are recorded when made rather than reconstructed later.

## Decisions

### Distributed idempotency via regional Redis/ElastiCache

**V1 state (by decision, 2026-07-03):** idempotency-key storage is local-only — the forge-sdk
`api/idempotency` `LocalCache` (in-process `sync.Map`) behind `mw.IdempotencyMiddleware` on all
public write routes (port 7051). Mirrors auth-api: `configureIdempotencyStore()` wires
`idempotency.SetStore(idempotency.NewStore("local", ttl))` with `IDEMPOTENCY_TTL_SEC` from the
secrets bundle. No Redis dependency in V1. Cross-instance dedup of actual side effects is carried
by DynamoDB conditional writes (create-profile `attribute_not_exists` transaction, passkey
conditional create, settings `version` check, conditional soft-delete), not the middleware — the
middleware is a client-retry UX layer, not the correctness boundary.

**V2 target:** incorporate the regional Redis/ElastiCache into idempotency handling — one shared
cluster per environment per region (`idem:<service>:<key>` namespacing; no per-service caches, no
cross-region replication) — directly (domain-level `SetNX` keys for side-effecting routes) and/or
via the SDK middleware's distributed path (`idempotency.NewDistributedStore` / `DistributedCache`,
already shipped in forge-sdk `api/idempotency`, wired by swapping the `idempotency.SetStore` call
in `cmd/server/main.go` behind an `IDEMPOTENCY_STORE=redis|local` switch, `local` default for
dev/docker-compose).

**Triggers to act (any of):**
- Auto-scaling beyond one task per region, where retries landing on other instances miss the local
  cache and the per-instance replay signal (`409` + `Idempotency-Replayed`) is no longer acceptable.
- New side-effecting routes whose dedup no DynamoDB conditional write covers (e.g. export enqueue
  fan-out, outbound event mint).
- A decision to add response-replay depth (store the response, not just the key) once the SDK
  middleware supports it.

**Constraints carried from V1:** keys stay small, self-expiring, and namespaced per service; the
regional cluster is shared platform infrastructure, not an accounts-api-owned resource; DynamoDB
conditional writes remain the correctness backstop regardless of cache availability.

### Telemetry and metrics — OTel export and event-derived trends

**Decided 2026-07-28.** Splitting metrics across V1 and V2 so the planned work is recorded rather than
lost, and so the V1 design doc stops claiming capability that does not exist.

**V1 baseline (in scope now):** log-derived metrics and alarms on errors. `TelemetryMiddleware` already
emits one structured JSON line per request carrying `request_id`, `method`, `path`, `status`, `bytes`,
`latency_ms`, `ip`, and `user_agent`; CloudWatch metric filters turn those into countable metrics, and
alarms fire from them. That is the whole of V1's measurement story — deliberately, and it is what
`sdd.md` §3.3.2 now says. The V1 work is making it *reliable*: alarms that notify a real destination,
dimensions that are correct, and coverage for the failure modes currently unwatched (`TODO.md` 24.3,
24.23).

**V2 target:** three sources, each with a distinct job.

| Source | Job | State today |
|---|---|---|
| SDK `logging/otel` | Ship structured logs over OTLP to a collector, so log search is not CloudWatch-only | Shipped in the SDK, unused here |
| SDK `aws/cloudwatch/metrics` (`PutMetricData`) | First-class operational metrics — request rate, latency histograms, dependency health — instead of inferring them from log text | Shipped in the SDK, unused here |
| Events-derived metrics store | Business and behavioural trends over time from the `account.*` domain event stream | Not built; depends on events-api's CDC consumer, which does not exist |

**Triggers to act (any of):**
- A log-shape change silently breaks a metric filter — the fragility that `PutMetricData` removes.
- Alarm coverage needs a signal that cannot be derived from a request log line (queue depth, cache hit
  rate, per-dependency latency).
- Distributed tracing becomes necessary, which is the point the OTLP exporter earns its wiring.
- The events-derived store lands and business trend questions start arriving.

**Constraint carried from V1:** metrics are never the mechanism that carries PII. The `RedactStrict`
posture on logs applies equally to metric dimensions — account identifiers only, never name, email, or
phone.

### Customer-facing passkey management deferred out of V1

**Decided 2026-07-29.** Customers will not read or manage their own passkey records through
accounts-api in V1. Passkey CRUD stays private-plane only (`GET|POST /v1/accounts/{id}/passkeys`,
`PATCH|DELETE /v1/accounts/{id}/passkeys/{credential_id}`), reachable by auth-api and no one else.
A customer-facing "my sign-in devices" screen is served by auth-api, which already owns those
records; if it needs the list it reads the private route it already consumes.

**Why not a read-only `GET /v1/me/passkeys` in V1** — the option was raised and rejected on scope,
not on difficulty:

- It contradicts the ownership line ADR 001 D0 draws. auth-api owns credential lifecycle;
  accounts-api stores what it is told to store. A second public surface over the same records makes
  two services answerable for one question.
- The customer-visible benefit is one settings screen avoiding one internal hop. That hop is not on
  the sign-in hot path and is invisible to the customer.
- Listing is the cheap half. The screen only becomes useful with rename and revoke, and **revoke is
  the part that carries real risk**: removing the last passkey on a passwordless-primary account can
  lock a customer out permanently. Guarding that ("refuse to delete the last remaining sign-in
  method") is authentication policy, which by D0 is auth-api's to enforce, not this service's.

**V2 target:** if a devices screen is built here rather than in auth-api, ship read plus rename plus
revoke as one contract change, with the last-credential guard specified and owned explicitly — not a
read route now and the dangerous half later.

**Triggers to revisit:**
- auth-api declines to own the devices screen.
- A support workflow needs device visibility that auth-api's surface does not provide.
- Passkey counts per account grow past the point where the private route's uncapped list is adequate.
