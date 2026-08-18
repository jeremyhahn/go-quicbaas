# Barrier — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to initialising, unsealing, sealing and recovering the
> system barrier. Feature folder: `docs/barrier/`.

The barrier is what makes data at rest unreadable to anyone holding only the
disk. A server whose barrier is sealed is running, reachable, and unable to
answer a single data request — deliberately.

Everything on this page is about the **system** barrier, the one protecting the
control plane. A tenant's own barrier is a different object with a different
lifecycle: [`../multi-tenancy/`](../multi-tenancy/user.md).

## Prerequisites

- A server that has completed its initialisation ceremony
  ([`../bootstrap/`](../bootstrap/user.md)). Before that, there is no barrier
  to operate.
- The credential its seal strategy requires: a passphrase, or a quorum of
  Shamir shares held by different officers.

## The single most important thing

**Decide the seal strategy before there is data, and record how a quorum is
reassembled.** A Shamir barrier with shares nobody can locate is data you have
crypto-shredded by accident. Passphrase is simpler and puts the whole barrier
behind one secret; Shamir splits custody so no single person can unseal alone.
Neither can be swapped for the other afterwards without a rekey.

## First run

```bash
# Where does the barrier stand?
quicbaas barrier status

# Passphrase strategy:
quicbaas barrier init

# Or split custody across officers:
quicbaas barrier init-shamir
```

`status` is the verb to reach for first, every time. It answers initialised or
not, sealed or not, and — for Shamir — how many shares of the quorum have been
submitted.

## Common tasks

### Unseal after a restart

Passphrase:

```bash
quicbaas barrier unseal
```

Shamir — each officer contributes, and nothing happens until the threshold is
met:

```bash
quicbaas barrier unseal-shares
quicbaas barrier status        # watch the share count climb
```

A restart always leaves the barrier sealed. That is not a failure state; it is
the property you bought.

### Seal deliberately

```bash
quicbaas barrier seal
```

Sealing makes the data unreadable immediately. Use it when you have decided the
host is compromised and unreadable is better than available. Do not use it to
"restart cleanly" — you will need the unseal credential to come back.

### Rotate the barrier key

```bash
quicbaas barrier rekey
```

Rekey re-wraps the barrier under new key material. Do it on a schedule, and do
it after any event that might have exposed the unseal credential. Redistribute
new Shamir shares as part of the same operation, not afterwards.

### Prepare for losing the credential

```bash
quicbaas barrier recovery generate
quicbaas barrier recovery status
```

Generate the recovery material **before** you need it and store it somewhere
that survives the loss you are insuring against. Recovery material stored on the
same host as the barrier insures nothing.

```bash
quicbaas barrier recovery recover
```

### Issue a root token

```bash
quicbaas barrier root-token
```

A root token is an emergency credential. Generate one for a specific incident,
use it, and revoke it. A standing root token defeats every other access control
in the system.

### Turn on encryption for a database

```bash
quicbaas barrier enable-encryption
```

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Every data request fails after a restart | The barrier is sealed. `barrier status`, then unseal. |
| Unseal with the right passphrase fails | The strategy is Shamir, not passphrase. Check `status` and collect a quorum. |
| Shares submitted, still sealed | The threshold is not met. `status` reports how many of how many. |
| `barrier init` says already initialised | It is. Initialisation is once per barrier; you want `unseal`. |
| Restricting `/api/v1/barrier` did not lock the surface down | `/api/v1/seal` is a second mounted family reaching the same lifecycle. See [api.md](api.md). |
| `quicbaas barrier` rejects a tenant argument | Wrong group. A tenant's barrier is `quicbaas iam tenant barrier`. |

## What this feature does not do

- **No automatic unseal.** By construction: a server that can unseal itself is a
  server whose disk is enough to read the data.
- **No operator override.** There is no path that unseals without the
  credential. That is the security property, and it means an unseal credential
  nobody holds is unrecoverable.
- **It is not per-tenant.** See [`../multi-tenancy/`](../multi-tenancy/user.md).
