# Audit Logging — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to reading and relying on the audit trail. Feature
> folder: `docs/audit-logging/`.

The audit trail records who did what, to which resource, and whether it
succeeded. It exists to answer questions after the fact — during an incident,
during an access review, during an audit — so the properties that matter are
completeness and integrity, not throughput.

## Prerequisites

- IAM enabled. Audit events are IAM records and the trail is reached under the
  `iam` command tree.
- Audit configured in the server config. The settings that matter operationally
  are the storage backend, whether signing is on (`sign_logs`), the signing key,
  and the rotation policy. `engineering.md` is the reference; the two decisions
  worth making deliberately are below.
- A principal holding the audit read permission. It is separately grantable
  from read access to the objects the events describe, so an auditor can be
  given the trail without being given the data.

## Two decisions to make before you rely on this

**1. Turn signing on, or accept that the trail is not evidence.** With
`sign_logs` enabled each record carries an HMAC, and tampering is detectable.
With it off, an audit trail is a log file: useful for debugging, not for
demonstrating anything to anyone who did not already trust you.

**2. Know where the events live.** `pkg/iam/audit_store.go` includes a
`MemoryAuditStorage` implementation. A deployment that ends up on it has an
audit trail that does not survive a restart. Confirm the configured backend is
a persistent one before treating the trail as a record.

## First run

```bash
# Prove events are being written at all: do something auditable, then look.
quicbaas iam user list
quicbaas iam audit query --limit 5
```

If the second command returns nothing, no events are being recorded — check
that audit is enabled before investigating anything else.

## Common tasks

### Investigate a suspected credential compromise

```bash
# Every failed authentication in the window.
quicbaas iam audit query \
  --category authentication --outcome failure \
  --start-time 2026-08-18T00:00:00Z --end-time 2026-08-19T00:00:00Z

# Then everything that principal actually did.
quicbaas iam audit query --actor user-4f2a --limit 200
```

Work outward from the actor, not from the resource: a compromised principal
usually touches things nobody thought to watch.

### Review who has been reaching a sensitive resource

```bash
quicbaas iam audit query --resource kms-key-prod-signing --limit 200
```

### Produce an evidence bundle

```bash
quicbaas iam audit query \
  --category authorization --outcome failure \
  --start-time 2026-07-01T00:00:00Z --end-time 2026-08-01T00:00:00Z \
  --output json > authz-denials-2026-07.json
```

Page with `--offset` until a call returns fewer than `--limit` rows. There is no
cursor and no fetch-all flag; a script that issues one call and stops has
silently truncated its evidence.

### Follow one event to its detail

```bash
quicbaas iam audit query --event-type policy.deleted --limit 10
quicbaas iam audit get <event-id> --output json
```

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `query` returns nothing, ever | Audit is disabled, or the configured storage is memory-backed and the process restarted. |
| Results stop at exactly 100 | `--limit` defaults to 100. That is a per-call cap, not the number of matching events. Page with `--offset`. |
| A time filter returns nothing | `--start-time` and `--end-time` require RFC 3339 (`2026-08-18T00:00:00Z`). A bare date is rejected, and the error names the flag, not the format. |
| `permission denied` | Audit read is its own permission. Holding read on the underlying resource does not grant it. |
| Signature verification passes on a record with no signature | Known defect: verification is fail-open when `sign_logs` is enabled but the stored signature is empty. Treat an unsigned record in a signed trail as suspect, not as verified. |
| Events appear after a delay | `RecordAsync` exists alongside `Record`. An asynchronously recorded event is not readable the instant the action returns. |

## What this feature does not do

- **It is not a log aggregator.** There is no search over free text, no
  full-text index, and no retention tiering. The filters in `cli.md` are the
  query language.
- **It does not alert.** Nothing here watches for a pattern and tells you. That
  belongs to whatever consumes the trail.
- **It does not audit reads of the trail itself.** Do not assume querying audit
  is recorded in audit.
