# Scheduler MCP API Reference

## Overview

The Model Context Protocol (MCP) interface enables AI/LLM integration with the QuicBaaS scheduler. Tools are exposed for programmatic job management through AI agents.

## Connection

| Property | Value |
|----------|-------|
| Transport | stdio or HTTP |
| Port (HTTP) | 9445 |
| Protocol | JSON-RPC 2.0 |

## Authentication

MCP connections use session-based authentication established during the initial handshake.

## Available Tools

### quicbaas_scheduler_create_job

Create a new scheduled job with scheduling parameters, handlers, and optional payload.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Human-readable job name"
    },
    "description": {
      "type": "string",
      "description": "Optional job description"
    },
    "owner_type": {
      "type": "string",
      "enum": ["system", "tenant", "delegated"],
      "description": "Job ownership type"
    },
    "owner_id": {
      "type": "string",
      "description": "Owner identifier (optional, depends on owner_type)"
    },
    "target_tenant_id": {
      "type": "string",
      "description": "Target tenant ID for tenant-scoped jobs (optional)"
    },
    "type": {
      "type": "string",
      "enum": ["one-shot", "recurring", "delayed"],
      "description": "Scheduling type"
    },
    "schedule": {
      "type": "string",
      "description": "Cron expression for recurring jobs (e.g., '0 0 * * *')"
    },
    "execute_at": {
      "type": "string",
      "description": "RFC3339 timestamp for one-shot execution"
    },
    "delay": {
      "type": "string",
      "description": "Duration string for delayed jobs (e.g., '1h30m')"
    },
    "handler": {
      "type": "string",
      "description": "Handler name that processes this job"
    },
    "payload": {
      "type": "string",
      "description": "Base64-encoded job payload data (optional)"
    },
    "priority": {
      "type": "integer",
      "description": "Job priority (higher = more priority, default: 0)"
    },
    "timeout": {
      "type": "string",
      "description": "Maximum execution time as duration string"
    },
    "max_retries": {
      "type": "integer",
      "description": "Maximum retry attempts on failure (default: 0)"
    },
    "labels": {
      "type": "object",
      "description": "Key-value labels for categorization"
    }
  },
  "required": ["name", "owner_type", "type", "handler"]
}
```

**Example:**

```json
{
  "name": "daily-backup",
  "owner_type": "system",
  "type": "recurring",
  "schedule": "0 2 * * *",
  "handler": "backup.full",
  "timeout": "1h",
  "max_retries": 3,
  "labels": {"env": "production"}
}
```

**Response:**

```json
{
  "job": {
    "id": 12345,
    "name": "daily-backup",
    "status": "pending",
    ...
  },
  "timestamp": 1737331200
}
```

---

### quicbaas_scheduler_get_job

Retrieve detailed information about a scheduled job by ID.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    }
  },
  "required": ["job_id"]
}
```

**Example:**

```json
{
  "job_id": 12345
}
```

---

### quicbaas_scheduler_list_jobs

List scheduled jobs with optional filtering by owner, status, handler, or labels.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "owner_type": {
      "type": "string",
      "enum": ["system", "tenant", "delegated"],
      "description": "Filter by ownership type (optional)"
    },
    "owner_id": {
      "type": "string",
      "description": "Filter by owner identifier (optional)"
    },
    "target_tenant_id": {
      "type": "string",
      "description": "Filter by target tenant ID (optional)"
    },
    "status": {
      "type": "string",
      "enum": ["pending", "running", "completed", "failed", "paused", "canceled"],
      "description": "Filter by job status (optional)"
    },
    "handler": {
      "type": "string",
      "description": "Filter by handler name (optional)"
    },
    "labels": {
      "type": "object",
      "description": "Filter by label key-value pairs (optional)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum jobs to return (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor from previous response"
    }
  }
}
```

**Example:**

```json
{
  "status": "pending",
  "handler": "db.compaction",
  "limit": 50
}
```

**Response:**

```json
{
  "jobs": [ ... ],
  "next_cursor": "eyJpZCI6MTIzNDV9",
  "has_more": true,
  "total_count": 150,
  "timestamp": 1737331200
}
```

---

### quicbaas_scheduler_delete_job

Delete a scheduled job permanently.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    },
    "force": {
      "type": "boolean",
      "description": "Force delete even if job is running (default: false)"
    }
  },
  "required": ["job_id"]
}
```

---

### quicbaas_scheduler_trigger_job

Immediately trigger execution of a scheduled job regardless of its schedule.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    }
  },
  "required": ["job_id"]
}
```

**Response:**

```json
{
  "job_id": 12345,
  "status": "running",
  "timestamp": 1737331200
}
```

---

### quicbaas_scheduler_pause_job

Pause a scheduled job to temporarily stop its execution.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    }
  },
  "required": ["job_id"]
}
```

**Response:**

```json
{
  "job_id": 12345,
  "old_status": "pending",
  "new_status": "paused",
  "timestamp": 1737331200
}
```

---

### quicbaas_scheduler_resume_job

Resume a paused job to restart its scheduled execution.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    }
  },
  "required": ["job_id"]
}
```

---

### quicbaas_scheduler_cancel_job

Cancel a scheduled job with an optional reason.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    },
    "reason": {
      "type": "string",
      "description": "Reason for cancellation (optional)"
    }
  },
  "required": ["job_id"]
}
```

**Example:**

```json
{
  "job_id": 12345,
  "reason": "Replaced by new job"
}
```

---

### quicbaas_scheduler_job_history

Retrieve execution history for a scheduled job including status, duration, and errors.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "integer",
      "description": "Unique job identifier"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for access control (optional)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum execution records to return (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor from previous response"
    }
  },
  "required": ["job_id"]
}
```

**Response:**

```json
{
  "executions": [
    {
      "id": 98765,
      "job_id": 12345,
      "status": "completed",
      "started_at": "2025-01-19T10:00:00Z",
      "ended_at": "2025-01-19T10:00:05Z",
      "duration": 5000000000,
      "error": "",
      "output": "Processed 150 records"
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "timestamp": 1737331200
}
```

---

### quicbaas_scheduler_status

Get scheduler service status including running state, job counts, and statistics.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {}
}
```

No input parameters required.

**Response:**

```json
{
  "running": true,
  "stats": {
    "job_count": 250,
    "running_jobs": 5,
    "success_count": 12500,
    "failure_count": 15,
    "last_tick_time": "2025-01-19T15:59:59Z",
    "started_at": "2025-01-18T00:00:00Z"
  },
  "timestamp": 1737331200
}
```

## Error Handling

MCP tools return errors in the standard JSON-RPC 2.0 error format:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "Job not found",
    "data": {
      "code": "SCHEDULER_JOB_NOT_FOUND",
      "job_id": 12345
    }
  }
}
```

### Error Codes

| Code | MCP Code | Description |
|------|----------|-------------|
| `SCHEDULER_JOB_NOT_FOUND` | -32001 | Job ID does not exist |
| `SCHEDULER_INVALID_JOB` | -32002 | Invalid job definition |
| `SCHEDULER_HANDLER_NOT_FOUND` | -32003 | Unknown handler |
| `SCHEDULER_UNAUTHORIZED` | -32004 | Access denied |
| `SCHEDULER_STOPPED` | -32005 | Scheduler not running |

## Usage Patterns

### Creating a Recurring Job

```
Use quicbaas_scheduler_create_job with:
- name: "hourly-metrics"
- owner_type: "system"
- type: "recurring"
- schedule: "0 * * * *"
- handler: "metrics.collect"
```

### Monitoring Job Status

```
1. Use quicbaas_scheduler_list_jobs with status: "running" to see active jobs
2. Use quicbaas_scheduler_get_job with job_id to check specific job
3. Use quicbaas_scheduler_job_history to view execution history
```

### Managing Job Lifecycle

```
1. Create job with quicbaas_scheduler_create_job
2. Pause if needed with quicbaas_scheduler_pause_job
3. Resume with quicbaas_scheduler_resume_job
4. Cancel permanently with quicbaas_scheduler_cancel_job
5. Delete with quicbaas_scheduler_delete_job
```

### Immediate Execution

```
Use quicbaas_scheduler_trigger_job to run a job immediately,
bypassing its normal schedule.
```
