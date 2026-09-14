# Dynamic secrets and credential leases

> **Status: NORMATIVE** · Last verified: 2026-09-12
> The lease subsystem behind `Security().DynamicSecrets()`, where its state
> lives, and what a lease does and does not enforce. Verbs are in
> [api.md](api.md). Conflicts with this document are defects.

## What was wrong

`Security().DynamicSecrets()` was **nil in every deployment**. `pkg/service`
declared the field and `DynamicSecrets()` returned it; nothing ever assigned it.
`NewDynamicSecretsHandler` and `lease.NewManager` had no non-test callers, so all
eight verbs reported the capability as unsupported on all five transports.

The blocker was storage. `pkg/lease` and the dynamic-secrets handler depended on
`pkg/storage.Storage` — a **local engine** interface (batches, snapshots,
bidirectional iterators) that a BaaS-mode server never instantiates. A comment
in `pkg/server/lifecycle.go` recorded that as "no local pebble storage", and
nothing was built.

## Where lease state lives

**In the embedded qrdb node, through `interfaces.KVStore`** — the one storage
contract every durable quicbaas component already uses (`pkg/orchestration`'s
stores, `pkg/iam`'s backends, the scheduler's storage). In BaaS mode that is
`pkg/server`'s qrdb-SDK-backed store; in a unit test it is
`kvstore.NewMemory()`, which is qrdb's own in-memory engine through the SDK
re-export.

Four properties follow, and each is load-bearing for a lease specifically:

| Property | Why a lease needs it |
| --- | --- |
| Durable | A lease must outlive the process holding the credential it governs — that is the case where revocation matters most. |
| Replicated | A revocation is a fleet-wide fact, not one node's opinion. |
| Barrier-encrypted at rest | Lease metadata gets the same protection as every other record. |
| Tenant-routed | One tenant's leases are not in another's keyspace. |

`pkg/lease` and the handler therefore consume `interfaces.KVStore` and no longer
import `pkg/storage`. This is a narrowing, not a new abstraction: a lease
manager only ever does point reads, point writes and one prefix scan. Building a
local store instead would have been a parallel implementation of something qrdb
owns (CLAUDE.md, Layer Ownership) and would have made revocation node-local — a
security control that does not survive a restart is not one.

## Revocation is a tombstone

`RevokeLease` **marks** `RevokedAt` and persists; it does not delete. Deleting
made `LeaseStateRevoked` unreachable in production — a revoked lease was
indistinguishable from one that never existed — and left nothing to audit.

- `Validate` can therefore refuse with a reason: "revoked at 14:02", not "no
  such lease".
- Revocation is idempotent in both directions: revoking an already-revoked lease
  keeps the original revocation time, and revoking an absent one succeeds with
  nothing to do. A retrying session conclusion converges.
- `ReapExpired` purges a tombstone once `TombstoneRetention` (default 24h) has
  passed, so the keyspace stays bounded.

## Revocation reaches the thing that was leased

`RevokeLease` asks the generator that produced the credential to tear it down
**before** it marks the lease, and only then marks it. Both halves matter:

- Before this, revocation was a bookkeeping entry. The lease record changed and
  the database user, cloud role or whatever else the generator had created stayed
  live and usable forever — and `credentials.CredentialGenerator.Revoke`, the SPI
  verb that exists precisely to undo a `Generate`, had **no caller anywhere in
  the product**.
- The **order** is what makes a failure repairable. A revoked lease is terminal;
  nothing revisits it. Marking it first and then failing to drop the login would
  leave live material behind a record saying it is gone. Destroying first means a
  generator failure leaves the lease `active`, the caller sees the refusal, and a
  retry converges.

The teardown is driven off the lease's own metadata (`credential_id`,
`generator`), which `GenerateCredential` wrote there — so the lease stays the one
handle a caller needs and there is no second index from lease to credential.

Four absences are tolerated, each for its own reason:

| Absence | Answer | Why |
| --- | --- | --- |
| Lease carries no `credential_id` | Revoke the lease only | It is not a generated credential. An account credential **delivery** grant is the common case and has no backing material. |
| Credential record already gone | Revoke the lease only | Already torn down; that is the desired end state. |
| Generator no longer registered | Revoke the lease, **keep** the record, warn | The login cannot be dropped, and refusing would make the lease permanently unrevocable on a deployment that dropped the plugin. The record is the only description of the orphan, so it is kept. |
| Lease does not exist | Nothing to do | `RevokeLease` reports its own answer. |

What is **not** tolerated is the generator refusing. That is live material still
standing, and it fails the call.

## Enforcement, not bookkeeping

Two verbs, deliberately different:

- **`GetLease`** returns the record in whatever state it is in. It is the
  observability verb; nothing may authorize on its result.
- **`Validate`** returns the lease **only while it is active**, and otherwise
  reports `LeaseNotFoundError`, `LeaseRevokedError` or `LeaseExpiredError`.

Expiry is answered from the record's own `ExpiresAt`, **not** from the sweep: a
lease that lapsed one millisecond ago is refused even though no pass has visited
it. `ReapExpired` (and the ticker around it, `StartExpirationLoop`) converts
lapsed leases into tombstones and purges aged-out ones — it is a reaper, never
the authority. A failed reaper degrades bookkeeping, not authorization.

`ReapExpired` is exported and synchronous so the sweep is testable as a
function rather than as a race against a ticker.

## Wiring

`Server.startDynamicSecrets` (`pkg/server/lifecycle_dynamic_secrets.go`) runs
after `startService` — the facade must exist to attach to — and **before**
`startOrchestration`, which leases the agent pool's credential deliveries
through the same lease manager.

It is not gated on a config flag: dynamic secrets is part of the locked
`Security()` surface, every transport already routes its eight verbs, and the
leases that govern delivery of provider credentials live here. The only
background cost is one reaper tick.

A server with **no durable store** (the sealed window, a bare test server)
leaves the capability unconfigured rather than building a manager over a store
that is not there — a manager that accepted grants it could not revoke would be
worse than none.

`Server.LeaseManager()` exposes the one lease authority in the process, so the
account credential leaser composes it rather than creating a second one. Every
lease in the platform is therefore in one keyspace, visible to one
`ListLeases`, revocable by one `RevokeLease`.

### The generator registry

`composeDynamicSecrets` builds a real `credentials.GeneratorRegistry` and
registers **nothing**. `pkg/credentials/database` ships PostgreSQL and MySQL
generators, but both take a `DBConnector` and this repository has no production
implementation of one; registering them against a nil connector would give an
operator a role that fails at generation time rather than at creation time. An
unregistered type is refused by `CreateRole` with "generator not found", which
is the correct answer.

Nothing else about the capability depends on a generator. Leases, their renewal,
their revocation and their listing — which is what the agent credential
lifecycle is built on — work with an empty registry.

## Related

- [architecture.md](architecture.md) — the secrets engine this sits beside
- [../software-factory/accounts.md](../software-factory/accounts.md#credential-delivery-leases)
  — the credential delivery lease, which composes this subsystem
