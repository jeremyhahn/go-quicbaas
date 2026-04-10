# Scheduler CLI Reference

## Overview

The `quicbaas scheduler` commands manage scheduled jobs via the CLI. All commands communicate with the QuicBaaS server through the configured protocol (default: Unix socket).

## Global Flags

```
--protocol   Protocol to use (rest, grpc, quic, unix) [default: unix]
--server     Server address [default: /run/quicbaas.sock]
--tenant     Tenant ID for tenant-scoped operations
--format     Output format (table, json, yaml) [default: table]
```

## Commands

### scheduler job create

Create a new scheduled job.

```bash
quicbaas scheduler job create [flags]
```

**Flags:**
```
--name           Job name (required)
--handler        Handler type (required)
--schedule       Schedule specification (required)
--params         Job parameters as JSON
--retry-max      Maximum retry attempts [default: 3]
--retry-interval Initial retry interval [default: 1m]
```

**Examples:**

```bash
# Create a one-time job
quicbaas scheduler job create \
  --name "disable-key-v1" \
  --handler "kms.grace_period" \
  --schedule "once:2024-01-15T10:00:00Z" \
  --params '{"key_id": "key_abc", "version_id": "v1"}'

# Create a cron job
quicbaas scheduler job create \
  --name "daily-compaction" \
  --handler "db.compaction" \
  --schedule "cron:0 2 * * *"

# Create an interval job
quicbaas scheduler job create \
  --name "hourly-cleanup" \
  --handler "audit.cleanup" \
  --schedule "interval:1h" \
  --params '{"retention_days": 90}'
```

### scheduler job list

List scheduled jobs with optional filtering.

```bash
quicbaas scheduler job list [flags]
```

**Flags:**
```
--state      Filter by state (pending, running, completed, failed, paused, canceled)
--handler    Filter by handler type
--limit      Maximum number of results [default: 100]
--offset     Pagination offset
```

**Examples:**

```bash
# List all jobs
quicbaas scheduler job list

# List pending jobs
quicbaas scheduler job list --state pending

# List KMS grace period jobs as JSON
quicbaas scheduler job list --handler kms.grace_period --format json
```

**Output:**
```
ID            NAME                 HANDLER            STATE     NEXT RUN
job_abc123    disable-key-v1       kms.grace_period   pending   2024-01-15T10:00:00Z
job_def456    daily-compaction     db.compaction      pending   2024-01-09T02:00:00Z
job_ghi789    hourly-cleanup       audit.cleanup      running   -
```

### scheduler job get

Get details of a specific job.

```bash
quicbaas scheduler job get <job-id> [flags]
```

**Examples:**

```bash
# Get job details
quicbaas scheduler job get job_abc123

# Get job details as YAML
quicbaas scheduler job get job_abc123 --format yaml
```

### scheduler job delete

Delete a scheduled job.

```bash
quicbaas scheduler job delete <job-id> [flags]
```

**Flags:**
```
--force    Delete without confirmation
```

**Examples:**

```bash
# Delete with confirmation
quicbaas scheduler job delete job_abc123

# Force delete
quicbaas scheduler job delete job_abc123 --force
```

### scheduler job trigger

Trigger immediate execution of a job.

```bash
quicbaas scheduler job trigger <job-id> [flags]
```

**Flags:**
```
--wait    Wait for job completion and show result
```

**Examples:**

```bash
# Trigger job
quicbaas scheduler job trigger job_abc123

# Trigger and wait for result
quicbaas scheduler job trigger job_abc123 --wait
```

### scheduler job pause

Pause a scheduled job.

```bash
quicbaas scheduler job pause <job-id>
```

**Examples:**

```bash
quicbaas scheduler job pause job_abc123
```

### scheduler job resume

Resume a paused job.

```bash
quicbaas scheduler job resume <job-id>
```

**Examples:**

```bash
quicbaas scheduler job resume job_abc123
```

### scheduler job cancel

Cancel a job permanently.

```bash
quicbaas scheduler job cancel <job-id> [flags]
```

**Flags:**
```
--force    Cancel without confirmation
```

**Examples:**

```bash
quicbaas scheduler job cancel job_abc123
```

### scheduler job history

View execution history for a job.

```bash
quicbaas scheduler job history <job-id> [flags]
```

**Flags:**
```
--limit    Maximum number of entries [default: 10]
```

**Examples:**

```bash
# View last 10 executions
quicbaas scheduler job history job_abc123

# View last 50 executions
quicbaas scheduler job history job_abc123 --limit 50
```

**Output:**
```
EXECUTION        STARTED                  COMPLETED                STATE       MESSAGE
exec_001         2024-01-08T10:00:00Z     2024-01-08T10:00:05Z     completed   Key version disabled
exec_002         2024-01-07T10:00:00Z     2024-01-07T10:00:03Z     completed   Key version disabled
exec_003         2024-01-06T10:00:00Z     2024-01-06T10:00:10Z     failed      Connection timeout
```

### scheduler status

Show scheduler status and statistics.

```bash
quicbaas scheduler status [flags]
```

**Examples:**

```bash
quicbaas scheduler status
```

**Output:**
```
Scheduler Status
================
Running:    true
Node ID:    node-1
Uptime:     72h15m30s

Job Statistics
--------------
Pending:    15
Running:    2
Completed:  1250
Failed:     3
Paused:     1
Canceled:   0

Registered Handlers
-------------------
- kms.grace_period
- db.compaction
- audit.cleanup
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Job not found |
| 3 | Invalid arguments |
| 4 | Permission denied |
| 5 | Connection error |
