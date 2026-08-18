# Multi-Tenancy — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to running more than one tenant on one deployment.
> Feature folder: `docs/multi-tenancy/`.

A tenant is the isolation unit: identities, policies, data and — where
entitled — its own encryption barrier. The interesting question is never "how
do I create a tenant"; it is "what exactly is isolated, and what is not".

## Prerequisites

- IAM enabled; tenants are IAM objects.
- A principal holding the tenant-management permissions. `api.md` maps each
  route to its `resource:action`.
- A decision, made before the first tenant is created, about whether tenants
  share the system barrier or get their own. See below.

## The two isolation postures

**Shared barrier.** Every tenant's data rests under the system barrier. Tenants
are separated by identity, policy and key scoping, not by cryptography an
operator cannot reach. `iam tenant barrier` calls are rejected as entitlement
violations, because there is no per-tenant barrier object.

**Dedicated barrier.** The tenant has its own barrier, under its own seal
strategy and credentials. An infrastructure operator holding neither the
passphrase nor a Shamir quorum cannot read the tenant's data, and sealing it
crypto-shreds that tenant without touching any other.

Choose deliberately. The second posture is what makes a compliance claim about
operator access defensible; the first is simpler and is the right answer when
all tenants are yours.

## First run

```bash
# Create a tenant.
quicbaas iam tenant create acme-corp

# Confirm it.
quicbaas iam tenant get acme-corp

# Work as that tenant.
quicbaas --tenant acme-corp iam user list
```

`--tenant` defaults to `system`. Almost every "why can't I see the data I just
wrote" question in a multi-tenant deployment is this flag.

## Common tasks

### Give a tenant its own barrier

```bash
quicbaas iam tenant barrier init --tenant acme-corp
quicbaas iam tenant barrier status --tenant acme-corp
```

Do this at tenant creation. Retrofitting a barrier onto a tenant with data is
not a configuration change.

### Unseal a tenant after a restart

```bash
quicbaas iam tenant barrier unseal --tenant acme-corp

# Or, with a Shamir quorum, once per officer:
quicbaas iam tenant barrier unseal-share --tenant acme-corp
quicbaas iam tenant barrier officer status --tenant acme-corp
```

A tenant with a dedicated barrier is **sealed after every restart** until
somebody unseals it. Its data is unreachable in the meantime. That is the
feature working, and it is the operational cost of the isolation posture.

### Offboard a tenant

```bash
quicbaas iam tenant barrier seal --tenant acme-corp   # crypto-shred
quicbaas iam tenant delete acme-corp --yes
```

Sealing first is what makes the deletion meaningful for a dedicated-barrier
tenant: the data becomes unreadable immediately rather than merely
unreferenced.

## What is isolated, and what is not

| Isolated per tenant | Shared across tenants |
|---|---|
| Identities, groups, roles, policies | The server process and its listeners |
| Data under a dedicated barrier | Data under a shared barrier (logically separated only) |
| Tenant-scoped keys | The system barrier and control plane |
| Audit events, by tenant scope | Host resources: CPU, memory, disk, connection limits |

**Object Storage and Dynamic Secrets tenant isolation is interface-only and
marked deferred.** Do not plan a compliance claim on cryptographic isolation of
those two surfaces without checking their current state first — this is
recorded because the gap sits behind an interface that looks complete.

**The embedding cache is not tenant-partitioned in any request path today.**
Tenant-scoped cache-key helpers exist and are tested, but nothing calls them.
See [`../embeddings/architecture.md`](../embeddings/architecture.md).

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Data written a moment ago is not visible | `--tenant` differs between the two commands. It defaults to `system`. |
| `iam tenant barrier` calls rejected | The tenant is on the shared barrier and has no barrier object. This is an entitlement rejection, not an authorization failure — do not go looking for a missing permission. |
| A tenant's data is unreachable after a restart | Its dedicated barrier is sealed. Unseal it. |
| Unseal fails with a correct passphrase | The tenant's strategy is Shamir, not passphrase. Check `officer status` and collect a quorum with `unseal-share`. |
| Deleting a tenant seems to leave data behind | Deletion removes the tenant object. For a shared-barrier tenant, seal-based shredding is not available; plan deletion accordingly. |

## What this feature does not do

- **No resource quotas or noisy-neighbour control.** CPU, memory and connection
  limits are per process, not per tenant.
- **No tenant-level network isolation.** All tenants share the listeners.
- **No cross-tenant queries.** By design; there is no "as any tenant" mode.
