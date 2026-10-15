# Scheduler — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-14
> Every verb and flag, with runnable examples. Feature folder: `docs/scheduler/`.

## Overview

The `quicbaas scheduler` commands manage scheduled jobs and place workloads.
Every command goes through the SDK's `Async().Scheduler()` on the transport
the global `--transport` flag selects, so each verb works the same over
`unix` (default), `grpc`, `rest`, `quic`, `mcp` and `embedded`.

## Flags shared by every scheduler command

```
--transport  Transport protocol (grpc, rest, quic, unix, mcp, embedded) [default: unix]
--address    Server address (Unix socket path or host:port)
--tenant     Tenant ID [default: system]
--timeout    Command timeout [default: 30s]
--output     Output format (table, json, yaml, raw) [default: table]
```

`json` and `yaml` use snake_case keys, and enums print as the flag values
(`one-shot`, `pending`, `tenant`). `raw` prints one line per item.

**Tenant.** `job create` makes the job owned by the `--tenant` tenant, and
`job list` lists that tenant's jobs. `job delete` sends the tenant with the
request.

## Job commands

### scheduler job create

```bash
quicbaas scheduler job create <name> --handler <handler> [flags]
```

**Flags:**
```
--handler      Job handler name (required)
--type         Job type (one-shot, recurring, delayed) [default: one-shot]
--description  Job description
--schedule     Cron schedule for recurring jobs
--execute-at   Execution time for one-shot jobs (RFC3339)
--delay        Delay for delayed jobs (Go duration, e.g. 24h)
--payload      Job payload (JSON)
--priority     Job priority [default: 0]
--timeout      Job timeout [default: 5m]; on this command it sets the job's timeout, not the command's
--max-retries  Maximum retry attempts [default: 3]
```

**Examples:**

```bash
quicbaas scheduler job create backup --type recurring --schedule "0 0 * * *" --handler backup.full
quicbaas scheduler job create grace-disable --type one-shot --execute-at "2025-01-20T00:00:00Z" --handler kms.grace_period
quicbaas scheduler job create cleanup --type delayed --delay 24h --handler audit.cleanup
```

**Output** — the server answers a create with the new job's ID and first run
only, so that is what is printed (`id`, `name`, `next_run`):
```
ID:        42
Name:      backup
Next Run:  2026-10-02T00:00:00Z
```

### scheduler job list

```bash
quicbaas scheduler job list [flags]
```

**Flags:**
```
--status   Filter by status (pending, running, completed, failed, paused, canceled)
--handler  Filter by handler name
--limit    Maximum number of jobs [default: 100]
--cursor   Pagination cursor from a previous page's "Next cursor"
```

**Output:**
```
ID  NAME     TYPE       HANDLER  STATUS   NEXT RUN
42  nightly  recurring  noop     pending  2026-10-01T13:00:00Z

Total: 1 jobs
Next cursor: <cursor>
```

`json` and `yaml` return `jobs`, `total_count`, `has_more` and `next_cursor`.

### scheduler job get

```bash
quicbaas scheduler job get <job-id>
```

Prints the job: ID, name, description, type, handler, status, schedule, next
and last run, run and fail counts, last error, priority, max retries and
creation time. `json` and `yaml` add `owner_type`, `owner_id`, `delay`,
`payload`, `timeout`, `labels` and `updated_at`.

### scheduler job delete

```bash
quicbaas scheduler job delete <job-id> [--force]
```

`--force` deletes the job even while it is running. Prints the job ID and
whether it was deleted.

### scheduler job trigger

```bash
quicbaas scheduler job trigger <job-id>
```

Runs the job now. Prints the job ID and the status it moved to.

### scheduler job pause | resume | cancel

```bash
quicbaas scheduler job pause 42
quicbaas scheduler job resume 42
quicbaas scheduler job cancel 42 --reason "No longer needed"
```

`cancel` stops future runs without deleting the job; `--reason` records why.
Each prints the job ID with its old and new status:
```
Job ID:      42
Old Status:  pending
New Status:  canceled
```

### scheduler job history

```bash
quicbaas scheduler job history <job-id> [--limit 50]
```

**Flags:**
```
--limit  Maximum number of executions [default: 50]
```

History has no cursor: when more executions exist than `--limit`, the output
says so; raise `--limit` to see them.

**Output:**
```
ID  STATUS     STARTED               DURATION  ERROR
7   completed  2026-10-01T12:00:00Z  1s        -
```

Errors longer than 50 characters are cut to fit. `json` and `yaml` give each
execution's full `error` and its `output`.

### scheduler status

```bash
quicbaas scheduler status
```

**Output:**
```
Status:         running
Total Jobs:     5
Running Jobs:   1
Success Count:  3
Failure Count:  1
Uptime:         72h15m30s
Last Tick:      2026-10-01T12:00:00Z
```

`json` and `yaml` give `running`, `total_jobs`, `running_jobs`,
`success_count`, `failure_count`, `uptime` and `last_tick`.

## Workload placement commands

All go through the SDK's `Async().Scheduler()` on the `--transport` selected.
`--output` is `table` (default), `json` or `yaml`.

**Tenant.** These verbs act in the tenant you authenticated to — the server
reads it from your credentials. The global `--tenant` flag has **no effect**
on them: placement takes no tenant from the request, and a request with no
authenticated tenant is refused (403 / `PermissionDenied`), never served as the
system tenant.

### scheduler node register

```bash
quicbaas scheduler node register n1 --capacity cpu_millis=8000 --capacity memory_bytes=34359738368 \
  --label arch=arm64 --label zone=a --ttl 30
```

Registers or refreshes a node's facts under a lease of `--ttl` seconds. Repeat
it, or run `node renew`, inside the TTL.

### scheduler node renew | deregister | get | cordon | uncordon | drain

```bash
quicbaas scheduler node renew n1
quicbaas scheduler node cordon n1
quicbaas scheduler node drain n1      # lists the placements to move
quicbaas scheduler node uncordon n1
quicbaas scheduler node deregister n1
quicbaas scheduler node list
```

### scheduler placement place

```bash
quicbaas scheduler placement place web-1 --group web --resource cpu_millis=500 \
  --spec '{"constraints":[{"key":"arch","operator":"In","values":["arm64"]}],
           "anti_affinity":[{"group":"web","required":true}],
           "spread":{"topology_key":"zone","max_skew":1,"required":true}}'
```

`--spec` is a JSON placement request; `--group`, `--resource`, `--label` and
`--strategy` (`binpack`, `spread`) override its fields.

### scheduler placement get | list | release

```bash
quicbaas scheduler placement list --node n1
quicbaas scheduler placement list --group web
quicbaas scheduler placement get web-1
quicbaas scheduler placement release web-1
```
