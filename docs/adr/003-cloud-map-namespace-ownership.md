# ADR 003 — `komodo.internal` Cloud Map Namespace Ownership

- **Status:** Active
- **Date:** 2026-07-26
- **Deciders:** advisor (delegated — no org-level service-discovery owner exists to consult)
- **Supersedes:** —
- **Related:** `deploy/cdk/main.ts:164-176`, `TODO.md` item 17.10, ADR 001 (private-plane contract)

## Context

`deploy/cdk/main.ts:164` **creates** the `komodo.internal` private DNS namespace via
`new servicediscovery.PrivateDnsNamespace(...)`, then registers `accounts-api` into it so the
private/m2m port is reachable by service name rather than IP.

A CloudFormation stack that *creates* a namespace also *owns* it. A second service issuing the same
`PrivateDnsNamespace` create against the same VPC fails its deploy with a name conflict.

Two facts, verified across all Komodo repositories on 2026-07-26:

1. **No other service references Cloud Map at all.** Grepping every non-`node_modules` CDK entrypoint
   in all 14 org directories returns exactly one hit — this repo.
2. **Nothing consumes it.** No sibling service resolves `accounts-api.komodo.internal`; auth-api's
   private-plane credential reads (ADR 001) do not go through service discovery today.
3. **No shared infrastructure stack exists**, and `komodo-forge-sdk-ts` ships no namespace helper.

So the collision is **latent, not active**: the risk lands on whichever service adopts Cloud Map
second, and it lands as a failed deploy rather than a runtime fault.

## Decision

**Accounts-api retains ownership of the `komodo.internal` namespace for now.** It is not moved.

Consumers adopting service discovery later must **import**, never create:

```ts
servicediscovery.PrivateDnsNamespace.fromPrivateDnsNamespaceAttributes(stack, 'Namespace', {
  namespaceName: 'komodo.internal',
  namespaceId: /* from SSM or a cross-stack export */,
  namespaceArn: /* likewise */,
});
```

## Rationale

Moving the namespace to a shared platform stack is the structurally correct answer, and it is
**cheaper now than it will ever be** — one creator, zero consumers, no migration. It was still
rejected for V1 because the shared stack does not exist, and creating one to hold a single resource
is a larger commitment than the risk currently justifies.

| | Keep here (chosen) | Move to shared stack | Leave undocumented |
|---|---|---|---|
| Cost now | ~0 | Half a day + a new stack to own | 0 |
| Failure mode | Second adopter must import, and this ADR tells them how | None | Second adopter's deploy fails with no explanation |
| Cost to revisit | Low while consumers = 0; rises with each one | — | Same as "keep", minus the signpost |

## Consequences

- **Accounts-api's stack cannot be torn down** without first migrating the namespace, because
  deleting it would break every future consumer registered in it.
- **Deploy ordering becomes real** the moment a second service registers: accounts-api must deploy
  first.
- **Revisit trigger — treat as a hard gate, not a reminder:** when the *second* service needs service
  discovery, or when any shared platform/infrastructure stack is created, whichever comes first.
  At that point move the namespace out and supersede this ADR. Deferring past that point converts a
  half-day refactor into a coordinated multi-service migration.
- The namespace name `komodo.internal` is now effectively org-wide API surface. Renaming it is a
  breaking change for every registered consumer.
