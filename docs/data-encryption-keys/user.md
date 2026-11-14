# Data Encryption Keys — User Guide

> **Status: NORMATIVE** · Last verified: 2026-11-14
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
sets that bound for one DEK at the server, so it holds against the API as well
as the CLI. Past `--warning-percent` the server warns; the use that reaches the
bound, and every use after it, is refused. `quicbaas dek status` shows the
usage counted so far.

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

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Every DEK command fails | The barrier is sealed. `quicbaas barrier status`. |
| Encryption refused with an AEAD limit error | The DEK reached its safety bound. That is a different error from a permission denial — rotate the DEK, do not go looking for a missing grant. |
| Re-encryption "finished" but records are still on the old version | `reencrypt` returned acceptance, not completion. Poll `reencrypt-progress`. |
| Rotating did not protect old data | Correct. Rotation affects new writes; re-encryption moves old ones. |
| Records unreadable after a delete | They were still encrypted under the deleted key. There is no recovery. |
| `set-default` did not change anything visible | It affects writes that name no DEK. Existing records are unaffected. |

## What this feature does not do

- **It does not manage the wrapping key.** That is [`../kms/`](../kms/user.md).
- **It does not protect against a lost barrier.** DEKs live under the barrier;
  see [`../barrier/user.md`](../barrier/user.md).
- **It has no undelete and no key escrow.** Deletion is final.
