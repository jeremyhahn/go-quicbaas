# Scheduler REST API Reference

## Base URL

```
/api/v1/scheduler
```

## Authentication

All endpoints require a valid Bearer token in the `Authorization` header:

```
Authorization: Bearer <jwt-token>
```

## Endpoints

### Create Job

Create a new scheduled job.

```
POST /jobs
```

**Request Body:**

```json
{
  "name": "daily-cleanup",
  "description": "Clean up expired sessions daily",
  "owner_type": "tenant",
  "owner_id": "tenant_123",
  "target_tenant_id": "tenant_123",
  "type": "recurring",
  "schedule": "0 0 * * *",
  "execute_at": "2025-01-20T10:00:00Z",
  "delay_seconds": 3600,
  "handler": "db.compaction",
  "payload": {"key": "value"},
  "priority": 10,
  "timeout_seconds": 300,
  "max_retries": 3,
  "labels": {
    "env": "production",
    "team": "platform"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Human-readable job name |
| `description` | string | No | Job description |
| `owner_type` | string | No | `system`, `tenant`, or `delegated` |
| `owner_id` | string | No | Owner identifier |
| `target_tenant_id` | string | No | Target tenant for tenant-scoped jobs |
| `type` | string | Yes | `one-shot`, `recurring`, or `delayed` |
| `schedule` | string | No | Cron expression (for `recurring`) |
| `execute_at` | string | No | RFC3339 timestamp (for `one-shot`) |
| `delay_seconds` | int | No | Delay in seconds (for `delayed`) |
| `handler` | string | Yes | Handler name to process the job |
| `payload` | object | No | Job-specific payload data |
| `priority` | int | No | Job priority (higher = more priority) |
| `timeout_seconds` | int | No | Max execution time in seconds |
| `max_retries` | int | No | Max retry attempts on failure |
| `labels` | object | No | Key-value labels for filtering |

**Response (201 Created):**

```json
{
  "job": {
    "id": 12345,
    "name": "daily-cleanup",
    "description": "Clean up expired sessions daily",
    "owner_type": "tenant",
    "owner_id": "tenant_123",
    "target_tenant_id": "tenant_123",
    "created_by": "user_456",
    "type": "recurring",
    "schedule": "0 0 * * *",
    "execute_at": 0,
    "delay_seconds": 0,
    "handler": "db.compaction",
    "payload": {"key": "value"},
    "priority": 10,
    "timeout_seconds": 300,
    "max_retries": 3,
    "status": "pending",
    "last_run": 0,
    "next_run": 1737417600,
    "run_count": 0,
    "fail_count": 0,
    "last_error": "",
    "labels": {"env": "production", "team": "platform"},
    "created_at": 1737331200,
    "updated_at": 1737331200
  },
  "timestamp": 1737331200
}
```

---

### List Jobs

List jobs with optional filtering and pagination.

```
GET /jobs
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `owner_type` | string | Filter by owner type |
| `owner_id` | string | Filter by owner ID |
| `target_tenant_id` | string | Filter by target tenant |
| `status` | string | Filter by status: `pending`, `running`, `completed`, `failed`, `paused`, `canceled` |
| `handler` | string | Filter by handler name |
| `limit` | int | Max results (1-1000, default: 100) |
| `cursor` | string | Pagination cursor |

**Response (200 OK):**

```json
{
  "jobs": [
    {
      "id": 12345,
      "name": "daily-cleanup",
      "status": "pending",
      ...
    }
  ],
  "next_cursor": "eyJpZCI6MTIzNDV9",
  "has_more": true,
  "total_count": 150,
  "timestamp": 1737331200
}
```

---

### Get Job

Retrieve a job by ID.

```
GET /jobs/{id}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Response (200 OK):**

```json
{
  "job": {
    "id": 12345,
    "name": "daily-cleanup",
    "status": "running",
    ...
  },
  "timestamp": 1737331200
}
```

---

### Update Job

Update an existing job's configuration.

```
PUT /jobs/{id}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Request Body:**

```json
{
  "name": "updated-cleanup",
  "description": "Updated description",
  "schedule": "0 6 * * *",
  "handler": "db.compaction",
  "priority": 20,
  "timeout_seconds": 600,
  "max_retries": 5,
  "labels": {"env": "staging"}
}
```

All fields are optional. Only provided fields are updated.

**Response (200 OK):**

```json
{
  "job": {
    "id": 12345,
    "name": "updated-cleanup",
    ...
  },
  "timestamp": 1737331200
}
```

---

### Delete Job

Delete a scheduled job.

```
DELETE /jobs/{id}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |
| `force` | bool | Force delete running jobs |

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "deleted": true,
  "timestamp": 1737331200
}
```

---

### Trigger Job

Execute a job immediately, bypassing its schedule.

```
POST /jobs/{id}/trigger
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "status": "running",
  "timestamp": 1737331200
}
```

---

### Pause Job

Temporarily suspend a job's scheduled execution.

```
POST /jobs/{id}/pause
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "old_status": "pending",
  "new_status": "paused",
  "timestamp": 1737331200
}
```

---

### Resume Job

Resume a paused job.

```
POST /jobs/{id}/resume
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "old_status": "paused",
  "new_status": "pending",
  "timestamp": 1737331200
}
```

---

### Cancel Job

Cancel a job permanently.

```
POST /jobs/{id}/cancel
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |

**Request Body (optional):**

```json
{
  "reason": "No longer needed"
}
```

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "old_status": "pending",
  "new_status": "canceled",
  "timestamp": 1737331200
}
```

---

### Get Job History

Retrieve execution history for a job.

```
GET /jobs/{id}/history
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | uint64 | Job ID |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tenant_id` | string | Tenant ID for access control |
| `limit` | int | Max results (1-1000, default: 100) |
| `cursor` | string | Pagination cursor |

**Response (200 OK):**

```json
{
  "executions": [
    {
      "id": 98765,
      "job_id": 12345,
      "status": "completed",
      "started_at": 1737244800,
      "ended_at": 1737244805,
      "duration_seconds": 5,
      "error": "",
      "output": "Processed 150 records"
    }
  ],
  "next_cursor": "eyJpZCI6OTg3NjV9",
  "has_more": true,
  "timestamp": 1737331200
}
```

---

### Get Scheduler Status

Get scheduler health and statistics.

```
GET /status
```

**Response (200 OK):**

```json
{
  "running": true,
  "stats": {
    "job_count": 250,
    "running_jobs": 5,
    "success_count": 12500,
    "failure_count": 15,
    "last_tick_time": 1737331199,
    "started_at": 1737244800
  },
  "timestamp": 1737331200
}
```

---

## Error Responses

All endpoints return standard error responses:

```json
{
  "error": {
    "code": "SCHEDULER_JOB_NOT_FOUND",
    "message": "Job with ID 12345 not found"
  }
}
```

### HTTP Status Codes

| Status | Description |
|--------|-------------|
| 200 | Success |
| 201 | Created |
| 400 | Bad Request - Invalid input |
| 401 | Unauthorized - Invalid/missing token |
| 403 | Forbidden - Insufficient permissions |
| 404 | Not Found - Resource not found |
| 409 | Conflict - Invalid state transition |
| 500 | Internal Server Error |

### Error Codes

| Code | Description |
|------|-------------|
| `SCHEDULER_JOB_NOT_FOUND` | Job ID does not exist |
| `SCHEDULER_JOB_EXISTS` | Job with same ID already exists |
| `SCHEDULER_INVALID_JOB` | Invalid job definition |
| `SCHEDULER_INVALID_SCHEDULE` | Malformed cron expression |
| `SCHEDULER_HANDLER_NOT_FOUND` | Unknown handler type |
| `SCHEDULER_HANDLER_FAILED` | Handler execution failed |
| `SCHEDULER_JOB_TIMEOUT` | Job exceeded timeout |
| `SCHEDULER_MAX_RETRIES` | Max retries exceeded |
| `SCHEDULER_STOPPED` | Scheduler not running |
| `SCHEDULER_UNAUTHORIZED` | Insufficient permissions |
| `SCHEDULER_STORAGE_ERROR` | Storage operation failed |
