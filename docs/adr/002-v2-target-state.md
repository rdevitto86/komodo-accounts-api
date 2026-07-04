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
regional cluster is shared platform infrastructure, not a customer-api-owned resource; DynamoDB
conditional writes remain the correctness backstop regardless of cache availability.
