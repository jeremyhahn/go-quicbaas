# Audit Logging — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag for reading the audit trail. Feature folder:
> `docs/audit-logging/`.

Audit has **no top-level command group**. It is reached as `quicbaas iam audit`,
registered by `newIAMAuditCommand` (`cmd/quicbaas/iam.go:92`) under the `iam`
tree, because an audit event is an IAM record: the trail exists to answer "who
did what", and "who" is an IAM principal.

Two verbs exist. Writing audit events from the CLI is deliberately not one of
them — see the end of this file.

## `quicbaas iam audit query`

Search the audit trail.

```bash
quicbaas iam audit query [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|---|---|---|---|---|
| `--event-type` | string | none | no | Exact event type, e.g. `user.login`, `policy.created` |
| `--category` | string | none | no | Event category, e.g. `authentication`, `authorization`, `management` |
| `--outcome` | string | none | no | `success` or `failure` |
| `--actor` | string | none | no | Actor (principal) id |
| `--resource` | string | none | no | Resource id the event acted on |
| `--start-time` | string | none | no | RFC 3339 timestamp; events strictly after it |
| `--end-time` | string | none | no | RFC 3339 timestamp; events strictly before it |
| `--limit` | int | `100` | no | Maximum events returned |
| `--offset` | int | `0` | no | Pagination offset |

Filters combine with AND. With no filters at all the query returns the most
recent `--limit` events.

### Examples

```bash
# The last 100 events, newest first.
quicbaas iam audit query

# Every failed authentication in a window.
quicbaas iam audit query \
  --category authentication \
  --outcome failure \
  --start-time 2026-08-18T00:00:00Z \
  --end-time 2026-08-19T00:00:00Z

# What one principal did, paged.
quicbaas iam audit query --actor user-4f2a --limit 50 --offset 0
quicbaas iam audit query --actor user-4f2a --limit 50 --offset 50

# Everything that touched one resource.
quicbaas iam audit query --resource kms-key-prod-signing

# Machine-readable, for an evidence bundle.
quicbaas iam audit query --category authorization --outcome failure --output json
```

`--limit` is a hard cap per call, not a total. An investigation that needs more
than `--limit` events pages with `--offset`; there is no cursor and no
"fetch all" flag, so a script that stops at the first short page is correct and
one that assumes a single call returned everything is not.

## `quicbaas iam audit get`

Fetch one event by id.

```bash
quicbaas iam audit get <event-id>
```

### Examples

```bash
quicbaas iam audit get evt-01J9X2Z8QK
quicbaas iam audit get evt-01J9X2Z8QK --output json
```

`query` returns the ids; `get` is how you retrieve an event's full payload once
you have one.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The query ran. **An empty result set is exit 0**, not an error. |
| `1` | The query could not run: bad flag value, unparseable timestamp, transport failure, or the caller lacks the permission. |

A script checking "did anything suspicious happen" must look at the result, not
the exit code — no matches and a successful query are the same exit status.

## Permissions

Both verbs are permission-gated per route. The exact `resource:action` pair for
each audit route is in [api.md](api.md), taken from
`pkg/api/rest/routes_permissions_generated.go`. Read access to the audit trail
is separately grantable from read access to the objects it describes, which is
the point: an auditor can be given the trail without being given the data.

## What has no CLI verb

- **Recording an event.** `quicbaas.IAM.Audit` exposes `Record` and
  `RecordAsync` over the API, and there is no CLI equivalent. That is
  deliberate: a hand-written audit event from an operator's shell is
  indistinguishable, after the fact, from one the system emitted, and an audit
  trail whose entries can be typed is not evidence.
- **Deleting an event.** `Delete` exists on the service for retention
  enforcement. It is not exposed as a verb, for the same reason.

Both are reachable through the API by a caller holding the permission; the
absence here is a CLI-surface decision, not a capability gap.
