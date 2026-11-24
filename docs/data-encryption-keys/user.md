# Data Encryption Keys — User Guide

> **Status: NORMATIVE** · Last verified: 2026-11-24
> Task-oriented guide to creating, rotating and retiring DEKs. Feature folder:
> `docs/data-encryption-keys/`.

A data encryption key encrypts records. The KMS wraps it, the barrier protects
the wrapping, and losing a DEK means losing the data under it. That last
sentence is the whole reason this feature has a safety interlock.

## Prerequisites

- An initialised and **unsealed** barrier ([`../barrier/`](../barrier/user.md)).
  Every operation here fails while sealed, by design.
- A principal with the DEK permissions. They are per verb, so creating and
  deleting are separately grantable — and they should be.

## First run

```bash
# Create a DEK at a path.
quicbaas dek create tenants/acme/records

# Confirm it.
quicbaas dek get tenants/acme/records
quicbaas dek status tenants/acme/records

# See everything.
quicbaas dek list
```

DEKs are addressed by **path**, not by an opaque id. Choose the path
deliberately: it is how every later operation names the key, and it is what
appears in the audit trail.

## Common tasks

### Nominate a default

```bash
quicbaas dek set-default tenants/acme/records
```

The default is used when a write names no DEK. Changing it does not re-encrypt
anything already written — new records use the new default, old records keep
their version.

### Rotate

```bash
quicbaas dek rotate tenants/acme/records
```

Rotation creates a new version. Existing records stay on the old one until you
re-encrypt. Rotation alone therefore improves nothing about already-written
data; it limits the exposure of *future* writes.

### Re-encrypt existing data onto the new version

```bash
quicbaas dek reencrypt tenants/acme/records
quicbaas dek reencrypt-progress tenants/acme/records
```

This is **asynchronous**. `reencrypt` returns when the operation is accepted,
not when it is done. Poll `reencrypt-progress` until it reports completion
before you consider the old version retired — and certainly before you delete
anything.

### Engage the safety interlock

```bash
quicbaas dek set-safety tenants/acme/records --max-operations 1000000 --max-bytes 68719476736 --warning-percent 80
```

An AEAD key is only safe for a bounded number of encryptions. The interlock
sets that bound for one path-addressed DEK, and the server's AEAD safety
tracker holds it, so it holds against the API as well as the CLI. Past
`--warning-percent` the server warns; the use counted against that DEK that
reaches the bound, and every use after it, is refused. `quicbaas dek status`
shows the usage counted so far.

What the interlock bounds today:

- **The path-addressed DEK only.** The bound lives in the server's tracker for
  the DEK you name.
- **The path-addressed store performs no encryption of its own.** No
  `quicbaas dek` verb encrypts under a path-addressed DEK, so nothing in this
  release counts a use against it on its own.
- **The tenant keyring has no per-version use bound yet.** The keyring that
  `quicbaas dek tenant` drives (see [Tenant keyring](#tenant-keyring)) is not
  covered by `set-safety`. Rotate it on your own schedule.

**Rotate to clear a refusal.** Rotation gives a new key version whose usage
starts at zero; it is never refused by the interlock.

```bash
quicbaas dek clear-safety tenants/acme/records
```

Clearing drops the DEK's own bound back to the server default. Treat it as a
deliberate, audited step.

### Retire a DEK

```bash
# 1. Rotate, re-encrypt, and confirm completion first.
quicbaas dek reencrypt-progress tenants/acme/records

# 2. Then, if the key is genuinely unused:
quicbaas dek clear-safety tenants/acme/records
quicbaas dek delete tenants/acme/records
```

**There is no undo.** Data still encrypted under a deleted DEK is gone. The
sequence above is the only safe order.

## Tenant keyring

Each tenant also has its own versioned DEK keyring, which does the real data
encryption. `quicbaas dek tenant` drives it:

```bash
# Show the keyring: the current version and the versions still held.
quicbaas dek tenant status

# Rotate to a new current version.
quicbaas dek tenant rotate

# Seal data under the current version; the output is a base64 ciphertext.
quicbaas dek tenant encrypt --plaintext 'card 4242' --aad order-17

# Open it again (the same --aad it was sealed with).
quicbaas dek tenant decrypt --ciphertext <base64> --aad order-17

# Move a ciphertext onto the current version.
quicbaas dek tenant rewrap --ciphertext <base64> --aad order-17

# Retire an old version once nothing is sealed under it.
quicbaas dek tenant retire --version 1
```

- **Every verb acts only on the caller's own tenant.** There is no flag to name
  another tenant's keyring.
- **`rewrap` moves a ciphertext to the current version.** Rewrap everything
  sealed under a version before you retire it.
- **`retire` refuses the current version.** Rotate first, then retire the old
  one.
- **Over REST and MCP, the data verbs need an https address.** `encrypt`,
  `decrypt` and `rewrap` carry plaintext or ciphertext, so the SDK refuses to
  send them over `http://` and never follows a redirect for them. `status`,
  `rotate` and `retire` never send the platform bearer over http or to another
  host. Over MCP every keyring verb needs an https address, because the session
  sends the bearer from its first request; the SDK refuses the call before
  anything is sent.
- **No use bound.** The safety interlock does not cover this keyring yet; see
  [Engage the safety interlock](#engage-the-safety-interlock).

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Every DEK command fails | The barrier is sealed. `quicbaas barrier status`. |
| Encryption refused with an AEAD limit error | The path-addressed DEK reached its safety bound. That is a different error from a permission denial — rotate the DEK, do not go looking for a missing grant. |
| Re-encryption "finished" but records are still on the old version | `reencrypt` returned acceptance, not completion. Poll `reencrypt-progress`. |
| Rotating did not protect old data | Correct. Rotation affects new writes; re-encryption moves old ones. |
| Records unreadable after a delete | They were still encrypted under the deleted key. There is no recovery. |
| `set-default` did not change anything visible | It affects writes that name no DEK. Existing records are unaffected. |

## What this feature does not do

- **It does not manage the wrapping key.** That is [`../kms/`](../kms/user.md).
- **It does not protect against a lost barrier.** DEKs live under the barrier;
  see [`../barrier/user.md`](../barrier/user.md).
- **It has no undelete and no key escrow.** Deletion is final.
