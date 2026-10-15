# Scheduler — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/scheduler/`.



## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.Scheduler` — SDK package `sdk/go/client/scheduler`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 23; MCP tools registered server-side: 23.

Every transport except MCP reaches the server through one mapping,
`scheduler.Dispatch` (`sdk/go/client/scheduler/dispatch.go`): the embedded
transport hands it the server's own `SchedulerService` and `PlacementService`,
the gRPC (and Unix) transport hands it `pkg/api/grpc`'s `Jobs()` and
`Placement()` clients, and the REST (and QUIC) transport hands it an adapter over
the routes below. Until go-quicbaas#1143 the job methods were served over MCP
only; gRPC, Unix and embedded fell through to an untyped call and REST never
substituted `{id}`.

The SDK job types carry what the service returns: `JobInfo.Handler`,
`Priority` and `LastError`; `CreateJobRequest.Priority`;
`ListJobsOptions.Handler` and `Cursor`, with `ListJobsResponse.NextCursor`;
`TriggerJobResponse.Status`; and `OldStatus`/`NewStatus` on pause, resume and
cancel. `DeleteJobWithOptions` sends `Force` and returns `Deleted`; it rides
the `DeleteJob` method above. `pkg/apimodule/module_scheduler_parity_test.go`
round-trips these fields over every transport, MCP included: the SDK MCP handler
sends the argument keys the `quicbaas_scheduler_*` tools read and decodes their
results through the same `pkg/service` types the tools return.

MCP tools: `quicbaas_scheduler_cancel_job`, `quicbaas_scheduler_cordon_node`, `quicbaas_scheduler_create_job`, `quicbaas_scheduler_delete_job`, `quicbaas_scheduler_deregister_node`, `quicbaas_scheduler_drain_node`, `quicbaas_scheduler_get_job`, `quicbaas_scheduler_get_node`, `quicbaas_scheduler_get_placement`, `quicbaas_scheduler_job_history`, `quicbaas_scheduler_list_jobs`, `quicbaas_scheduler_list_nodes`, `quicbaas_scheduler_list_placements`, `quicbaas_scheduler_pause_job`, `quicbaas_scheduler_place`, `quicbaas_scheduler_register_node`, `quicbaas_scheduler_release`, `quicbaas_scheduler_renew_node`, `quicbaas_scheduler_resume_job`, `quicbaas_scheduler_status`, `quicbaas_scheduler_trigger_job`, `quicbaas_scheduler_uncordon_node`, `quicbaas_scheduler_update_job`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `CancelJob` | `POST /api/v1/scheduler/jobs/{id}/cancel` | yes | yes | yes | yes | `scheduler:cancel` |
| `CreateJob` | `POST /api/v1/scheduler/jobs` | yes | yes | yes | yes | `scheduler:create` |
| `DeleteJob` | `DELETE /api/v1/scheduler/jobs/{id}` | yes | yes | yes | yes | `scheduler:delete` |
| `GetJob` | `GET /api/v1/scheduler/jobs/{id}` | yes | yes | yes | yes | `scheduler:read` |
| `GetJobHistory` | `GET /api/v1/scheduler/jobs/{id}/history` | yes | yes | yes | yes | `scheduler:history` |
| `GetStatus` | `GET /api/v1/scheduler/status` | yes | yes | yes | yes | `scheduler:status` |
| `ListJobs` | `GET /api/v1/scheduler/jobs` | yes | yes | yes | yes | `scheduler:list` |
| `PauseJob` | `POST /api/v1/scheduler/jobs/{id}/pause` | yes | yes | yes | yes | `scheduler:pause` |
| `ResumeJob` | `POST /api/v1/scheduler/jobs/{id}/resume` | yes | yes | yes | yes | `scheduler:resume` |
| `TriggerJob` | `POST /api/v1/scheduler/jobs/{id}/trigger` | yes | yes | yes | yes | `scheduler:trigger` |
| `UpdateJob` | `PUT /api/v1/scheduler/jobs/{id}` | yes | yes | yes | yes | `scheduler:update` |
| `CordonNode` | `POST /api/v1/scheduler/nodes/{id}/cordon` | yes | yes | yes | yes | `placement:manage` |
| `DeregisterNode` | `DELETE /api/v1/scheduler/nodes/{id}` | yes | yes | yes | yes | `placement:manage` |
| `DrainNode` | `POST /api/v1/scheduler/nodes/{id}/drain` | yes | yes | yes | yes | `placement:manage` |
| `GetNode` | `GET /api/v1/scheduler/nodes/{id}` | yes | yes | yes | yes | `placement:read` |
| `GetPlacement` | `GET /api/v1/scheduler/placements/{id}` | yes | yes | yes | yes | `placement:read` |
| `ListNodes` | `GET /api/v1/scheduler/nodes` | yes | yes | yes | yes | `placement:read` |
| `ListPlacements` | `GET /api/v1/scheduler/placements` | yes | yes | yes | yes | `placement:read` |
| `Place` | `POST /api/v1/scheduler/placements` | yes | yes | yes | yes | `placement:place` |
| `RegisterNode` | `POST /api/v1/scheduler/nodes` | yes | yes | yes | yes | `placement:manage` |
| `Release` | `DELETE /api/v1/scheduler/placements/{id}` | yes | yes | yes | yes | `placement:place` |
| `RenewNode` | `POST /api/v1/scheduler/nodes/{id}/renew` | yes | yes | yes | yes | `placement:manage` |
| `UncordonNode` | `POST /api/v1/scheduler/nodes/{id}/uncordon` | yes | yes | yes | yes | `placement:manage` |


## Protocol Overview

The scheduler API is exposed via all supported protocols for maximum interoperability.

| Protocol | Transport | Port | Use Case |
|----------|-----------|------|----------|
| REST | HTTP/HTTPS | 8443 | Web clients, external systems |
| gRPC | TCP | 10443 | High-performance service-to-service |
| QUIC | UDP | 9444 | Low-latency, mobile clients |
| MCP | stdio/HTTP | 9445 | AI/LLM integration |
| Unix | Socket | /run/quicbaas.sock | Local IPC, CLI |

## Protocol-Specific Documentation

- [REST API Reference](api.md)
- [gRPC API Reference](api.md)
- [QUIC API Reference](api.md)
- [MCP API Reference](api.md)
- [Unix Socket API Reference](api.md)

## Common Data Types

### Job

```json
{
  "id": "job_abc123",
  "tenant_id": "tenant_xyz",
  "name": "grace-period-keyv1",
  "handler_type": "kms.grace_period",
  "state": "pending",
  "owner_type": "tenant",
  "owner_id": "user_123",
  "schedule": {
    "type": "once",
    "run_at": "2024-01-15T10:00:00Z"
  },
  "params": {
    "key_id": "key_abc",
    "version_id": "v1"
  },
  "retry_policy": {
    "max_attempts": 3,
    "initial_interval": "1m",
    "max_interval": "1h",
    "multiplier": 2.0
  },
  "created_at": "2024-01-08T10:00:00Z",
  "updated_at": "2024-01-08T10:00:00Z",
  "last_run_at": null,
  "next_run_at": "2024-01-15T10:00:00Z"
}
```

### Schedule Types

**Once** - Execute at a specific time:
```json
{
  "type": "once",
  "run_at": "2024-01-15T10:00:00Z"
}
```

**Cron** - Execute on a cron schedule:
```json
{
  "type": "cron",
  "expression": "0 0 * * *"
}
```

**Interval** - Execute at fixed intervals:
```json
{
  "type": "interval",
  "every": "1h"
}
```

### JobHistory

```json
{
  "job_id": "job_abc123",
  "execution_id": "exec_def456",
  "started_at": "2024-01-15T10:00:00Z",
  "completed_at": "2024-01-15T10:00:05Z",
  "state": "completed",
  "result": {
    "success": true,
    "message": "Key version disabled successfully",
    "data": {}
  },
  "error": null
}
```

### SchedulerStatus

```json
{
  "running": true,
  "node_id": "node-1",
  "uptime": "72h15m30s",
  "jobs": {
    "pending": 15,
    "running": 2,
    "completed": 1250,
    "failed": 3,
    "paused": 1,
    "canceled": 0
  },
  "handlers": [
    "kms.grace_period",
    "db.compaction",
    "audit.cleanup"
  ]
}
```

## API Operations

| Operation | Description |
|-----------|-------------|
| Create Job | Schedule a new job |
| List Jobs | List jobs with filtering |
| Get Job | Get job details by ID |
| Delete Job | Delete a job |
| Trigger Job | Execute job immediately |
| Pause Job | Pause scheduled execution |
| Resume Job | Resume paused job |
| Cancel Job | Cancel job permanently |
| Job History | Get execution history |
| Scheduler Status | Get scheduler health |

## Authentication

All API requests require authentication via:
- **REST/QUIC**: Bearer token in `Authorization` header
- **gRPC**: Token in metadata
- **MCP**: Session-based authentication
- **Unix Socket**: Process UID/GID verification

## Errors

Scheduler errors are not sent as scheduler-specific codes. Each transport
reports them through its own status: REST and QUIC return the flat REST error
body with an HTTP status (see [Error Responses](#error-responses)), gRPC and
Unix return a gRPC status code (see the gRPC [Error Handling](#error-handling)).
The shared lower-case error codes are listed in
[Multi-Protocol API: Error Handling](../multiprotocol-api/api.md#error-handling).


---

## Scheduler REST API Reference

### Base URL

```
/api/v1/scheduler
```

### Authentication

All endpoints require a valid Bearer token in the `Authorization` header:

```
Authorization: Bearer <jwt-token>
```

### Endpoints

#### Create Job

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
| `target_tenant_id` | string | No | Target tenant for tenant-scoped jobs. Must be the caller's own tenant (or omitted); naming another is refused for every caller, the system tenant included |
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

#### List Jobs

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

#### Get Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

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

#### Update Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

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

#### Delete Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |
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

#### Trigger Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

**Response (200 OK):**

```json
{
  "job_id": 12345,
  "status": "running",
  "timestamp": 1737331200
}
```

---

#### Pause Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

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

#### Resume Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

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

#### Cancel Job

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |

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

#### Get Job History

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
| `tenant_id` | string | Optional. The caller's own tenant; any other value is refused with 403 / `PermissionDenied` |
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

#### Get Scheduler Status

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

### Error Responses

Errors use the flat REST error body shared by every route (see
[Multi-Protocol API: Error Handling](../multiprotocol-api/api.md#error-handling)).
Scheduler errors carry no `code` field and are sent with `retryable: false`;
the HTTP status is the signal:

```json
{
  "error": "resource not found: job not found: 12345",
  "timestamp": "2025-01-15T10:30:00Z",
  "request_id": "req-abc123",
  "retryable": false
}
```

#### HTTP Status Codes

| Status | Scheduler condition |
|--------|---------------------|
| 400 | Invalid job definition or malformed schedule |
| 401 | Missing or invalid token |
| 403 | Caller is not authorized for the job |
| 404 | Job or handler not found |
| 409 | Job with the same ID already exists |
| 429 | Job exceeded its maximum retries |
| 500 | Handler or storage failure |
| 503 | Scheduler is stopped or unavailable |
| 504 | Job exceeded its timeout |


---

## Scheduler gRPC API Reference

### Service Definition

```protobuf
package quicbaas.scheduler.v1;

service SchedulerService {
    // Job Management
    rpc CreateJob(CreateJobRequest) returns (CreateJobResponse);
    rpc GetJob(GetJobRequest) returns (GetJobResponse);
    rpc UpdateJob(UpdateJobRequest) returns (UpdateJobResponse);
    rpc DeleteJob(DeleteJobRequest) returns (DeleteJobResponse);
    rpc ListJobs(ListJobsRequest) returns (ListJobsResponse);

    // Job Control
    rpc TriggerJob(TriggerJobRequest) returns (TriggerJobResponse);
    rpc PauseJob(PauseJobRequest) returns (PauseJobResponse);
    rpc ResumeJob(ResumeJobRequest) returns (ResumeJobResponse);
    rpc CancelJob(CancelJobRequest) returns (CancelJobResponse);

    // History and Status
    rpc GetJobHistory(GetJobHistoryRequest) returns (GetJobHistoryResponse);
    rpc GetSchedulerStatus(GetSchedulerStatusRequest) returns (GetSchedulerStatusResponse);
}
```

### Connection

| Property | Value |
|----------|-------|
| Host | `localhost` |
| Port | `10443` |
| TLS | Required |

### Authentication

Pass authentication token in gRPC metadata:

```go
md := metadata.New(map[string]string{
    "authorization": "Bearer <jwt-token>",
})
ctx := metadata.NewOutgoingContext(context.Background(), md)
```

### Enums

#### JobType

```protobuf
enum JobType {
    JOB_TYPE_UNSPECIFIED = 0;
    JOB_TYPE_ONE_SHOT = 1;    // Execute once at specific time
    JOB_TYPE_RECURRING = 2;   // Execute on cron schedule
    JOB_TYPE_DELAYED = 3;     // Execute after delay
}
```

#### JobStatus

```protobuf
enum JobStatus {
    JOB_STATUS_UNSPECIFIED = 0;
    JOB_STATUS_PENDING = 1;    // Awaiting execution
    JOB_STATUS_RUNNING = 2;    // Currently executing
    JOB_STATUS_COMPLETED = 3;  // Successfully finished
    JOB_STATUS_FAILED = 4;     // Execution failed
    JOB_STATUS_PAUSED = 5;     // Temporarily suspended
    JOB_STATUS_CANCELED = 6;   // Permanently stopped
}
```

#### OwnerType

```protobuf
enum OwnerType {
    OWNER_TYPE_UNSPECIFIED = 0;
    OWNER_TYPE_SYSTEM = 1;     // Internal system jobs
    OWNER_TYPE_TENANT = 2;     // User-created tenant jobs
    OWNER_TYPE_DELEGATED = 3;  // System job for tenant data
}
```

### Messages

#### Job

```protobuf
message Job {
    uint64 id = 1;
    string name = 2;
    string description = 3;
    OwnerType owner_type = 4;
    string owner_id = 5;
    string target_tenant_id = 6;
    string created_by = 7;
    JobType type = 8;
    string schedule = 9;           // Cron expression
    int64 execute_at = 10;         // Unix timestamp
    int64 delay_seconds = 11;
    string handler = 12;
    bytes payload = 13;
    int32 priority = 14;
    int64 timeout_seconds = 15;
    int32 max_retries = 16;
    JobStatus status = 17;
    int64 last_run = 18;           // Unix timestamp
    int64 next_run = 19;           // Unix timestamp
    uint64 run_count = 20;
    uint64 fail_count = 21;
    string last_error = 22;
    map<string, string> labels = 23;
    int64 created_at = 24;         // Unix timestamp
    int64 updated_at = 25;         // Unix timestamp
}
```

#### JobExecution

```protobuf
message JobExecution {
    uint64 id = 1;
    uint64 job_id = 2;
    JobStatus status = 3;
    int64 started_at = 4;          // Unix timestamp
    int64 ended_at = 5;            // Unix timestamp
    int64 duration_ms = 6;
    string error = 7;
    string output = 8;
}
```

#### SchedulerStats

```protobuf
message SchedulerStats {
    uint64 job_count = 1;
    uint64 running_jobs = 2;
    uint64 success_count = 3;
    uint64 failure_count = 4;
    int64 last_tick_time = 5;      // Unix timestamp
    int64 started_at = 6;          // Unix timestamp
}
```

### RPC Methods

#### CreateJob

Create a new scheduled job.

```protobuf
message CreateJobRequest {
    string name = 1;
    string description = 2;
    OwnerType owner_type = 3;
    string owner_id = 4;
    string target_tenant_id = 5;
    JobType type = 6;
    string schedule = 7;
    int64 execute_at = 8;
    int64 delay_seconds = 9;
    string handler = 10;
    bytes payload = 11;
    int32 priority = 12;
    int64 timeout_seconds = 13;
    int32 max_retries = 14;
    map<string, string> labels = 15;
}

message CreateJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

**Example (Go):**

```go
req := &proto.CreateJobRequest{
    Name:      "daily-backup",
    OwnerType: proto.OWNER_TYPE_SYSTEM,
    Type:      proto.JOB_TYPE_RECURRING,
    Schedule:  "0 2 * * *",
    Handler:   "backup.full",
    Priority:  10,
    Labels:    map[string]string{"env": "production"},
}

resp, err := client.CreateJob(ctx, req)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Created job: %d\n", resp.Job.Id)
```

---

#### GetJob

Retrieve job details by ID.

```protobuf
message GetJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message GetJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

**Example (Go):**

```go
resp, err := client.GetJob(ctx, &proto.GetJobRequest{
    JobId: 12345,
})
```

---

#### UpdateJob

Update an existing job.

```protobuf
message UpdateJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    string name = 3;
    string description = 4;
    string schedule = 5;
    int64 execute_at = 6;
    string handler = 7;
    bytes payload = 8;
    int32 priority = 9;
    int64 timeout_seconds = 10;
    int32 max_retries = 11;
    map<string, string> labels = 12;
}

message UpdateJobResponse {
    Job job = 1;
    int64 timestamp = 2;
}
```

---

#### DeleteJob

Delete a scheduled job.

```protobuf
message DeleteJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    bool force = 3;
}

message DeleteJobResponse {
    uint64 job_id = 1;
    bool deleted = 2;
    int64 timestamp = 3;
}
```

---

#### ListJobs

List jobs with optional filtering.

```protobuf
message ListJobsRequest {
    OwnerType owner_type = 1;
    string owner_id = 2;
    string target_tenant_id = 3;
    JobStatus status = 4;
    string handler = 5;
    map<string, string> labels = 6;
    int32 limit = 7;
    string cursor = 8;
}

message ListJobsResponse {
    repeated Job jobs = 1;
    string next_cursor = 2;
    bool has_more = 3;
    int32 total_count = 4;
    int64 timestamp = 5;
}
```

**Example (Go):**

```go
resp, err := client.ListJobs(ctx, &proto.ListJobsRequest{
    Status:  proto.JOB_STATUS_PENDING,
    Handler: "db.compaction",
    Limit:   50,
})

for _, job := range resp.Jobs {
    fmt.Printf("Job: %s (ID: %d)\n", job.Name, job.Id)
}
```

---

#### TriggerJob

Execute job immediately.

```protobuf
message TriggerJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message TriggerJobResponse {
    uint64 job_id = 1;
    JobStatus status = 2;
    int64 timestamp = 3;
}
```

---

#### PauseJob

Pause scheduled execution.

```protobuf
message PauseJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message PauseJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

#### ResumeJob

Resume paused job.

```protobuf
message ResumeJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
}

message ResumeJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

#### CancelJob

Cancel job permanently.

```protobuf
message CancelJobRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    string reason = 3;
}

message CancelJobResponse {
    uint64 job_id = 1;
    JobStatus old_status = 2;
    JobStatus new_status = 3;
    int64 timestamp = 4;
}
```

---

#### GetJobHistory

Get execution history.

```protobuf
message GetJobHistoryRequest {
    uint64 job_id = 1;
    string tenant_id = 2;
    int32 limit = 3;
    string cursor = 4;
}

message GetJobHistoryResponse {
    repeated JobExecution executions = 1;
    string next_cursor = 2;
    bool has_more = 3;
    int64 timestamp = 4;
}
```

---

#### GetSchedulerStatus

Get scheduler health and statistics.

```protobuf
message GetSchedulerStatusRequest {}

message GetSchedulerStatusResponse {
    bool running = 1;
    SchedulerStats stats = 2;
    int64 timestamp = 3;
}
```

### Error Handling

Scheduler errors are returned as a gRPC status; the status message is the
error text. There are no scheduler-specific codes on the wire:

| gRPC Code | Scheduler condition |
|-----------|---------------------|
| `INVALID_ARGUMENT` | Missing `job_id`, name or handler; invalid job definition or schedule |
| `NOT_FOUND` | Job or handler not found |
| `ALREADY_EXISTS` | Job with the same ID already exists |
| `PERMISSION_DENIED` | Caller is not authorized for the job |
| `DEADLINE_EXCEEDED` | Job exceeded its timeout |
| `RESOURCE_EXHAUSTED` | Job exceeded its maximum retries |
| `UNAVAILABLE` | Scheduler is stopped or the job service is not available |
| `INTERNAL` | Handler or storage failure |

**Example Error Handling (Go):**

```go
resp, err := client.GetJob(ctx, req)
if err != nil {
    st, ok := status.FromError(err)
    if ok {
        switch st.Code() {
        case codes.NotFound:
            fmt.Println("Job not found")
        case codes.PermissionDenied:
            fmt.Println("Access denied")
        default:
            fmt.Printf("Error: %s\n", st.Message())
        }
    }
}
```

### Client Examples

#### Go Client Setup

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
    proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Load TLS credentials
creds, err := credentials.NewClientTLSFromFile("ca.crt", "")
if err != nil {
    log.Fatal(err)
}

// Connect
conn, err := grpc.Dial("localhost:10443", grpc.WithTransportCredentials(creds))
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

// Create client
client := proto.NewSchedulerServiceClient(conn)
```

#### Python Client Setup

```python
import grpc
from quicbaas.scheduler.v1 import scheduler_pb2, scheduler_pb2_grpc

# Load credentials
with open('ca.crt', 'rb') as f:
    creds = grpc.ssl_channel_credentials(f.read())

# Connect
channel = grpc.secure_channel('localhost:10443', creds)
client = scheduler_pb2_grpc.SchedulerServiceStub(channel)

# Create job
response = client.CreateJob(scheduler_pb2.CreateJobRequest(
    name="daily-backup",
    owner_type=scheduler_pb2.OWNER_TYPE_SYSTEM,
    type=scheduler_pb2.JOB_TYPE_RECURRING,
    schedule="0 2 * * *",
    handler="backup.full",
))
print(f"Created job: {response.job.id}")
```


---

## Scheduler QUIC API Reference

### Overview

The QUIC transport provides high-performance, low-latency access to the scheduler API with benefits including:

- **0-RTT connection establishment** for reduced latency
- **Stream multiplexing** without head-of-line blocking
- **Connection migration** for mobile clients
- **Built-in encryption** via TLS 1.3

### Connection

| Property | Value |
|----------|-------|
| Host | `localhost` |
| Port | `9444` |
| Protocol | QUIC over UDP |
| TLS | TLS 1.3 (required) |

### Wire Protocol

#### Request Format

```
[opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M]
```

| Field | Size | Description |
|-------|------|-------------|
| `opcode` | 1 byte | Operation code (`0x00` for scheduler) |
| `tokenLen` | 2 bytes | Token length (big-endian) |
| `token` | N bytes | JWT authentication token |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | M bytes | JSON-encoded request (first byte is sub-opcode) |

#### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| `status` | 1 byte | Status code |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | N bytes | JSON-encoded response |

### Opcodes

#### Primary Opcode

The scheduler uses a single dispatch opcode with sub-operations:

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpScheduler` | `0x00` | Scheduler dispatch opcode |

#### Sub-Opcodes

Sub-opcodes are encoded in the first byte of the payload:

| Sub-Opcode | Hex | Operation |
|------------|-----|-----------|
| `SubOpSchedulerCreateJob` | `0x01` | Create job |
| `SubOpSchedulerGetJob` | `0x02` | Get job |
| `SubOpSchedulerUpdateJob` | `0x03` | Update job |
| `SubOpSchedulerDeleteJob` | `0x04` | Delete job |
| `SubOpSchedulerListJobs` | `0x05` | List jobs |
| `SubOpSchedulerTriggerJob` | `0x06` | Trigger job |
| `SubOpSchedulerPauseJob` | `0x07` | Pause job |
| `SubOpSchedulerResumeJob` | `0x08` | Resume job |
| `SubOpSchedulerCancelJob` | `0x09` | Cancel job |
| `SubOpSchedulerGetJobHistory` | `0x0A` | Get job history |
| `SubOpSchedulerGetStatus` | `0x0B` | Get scheduler status |

### Status Codes

| Code | Hex | Description |
|------|-----|-------------|
| `StatusOK` | `0x00` | Success |
| `StatusInvalidRequest` | `0x01` | Invalid request format |
| `StatusNotFound` | `0x02` | Resource not found |
| `StatusUnauthorized` | `0x03` | Authentication required |
| `StatusInternalError` | `0x04` | Internal server error |
| `StatusTimeout` | `0x05` | Operation timed out |
| `StatusAlreadyExists` | `0x06` | Resource already exists |
| `StatusPermissionDenied` | `0x07` | Insufficient permissions |
| `StatusResourceExhausted` | `0x08` | Rate limit exceeded |
| `StatusInvalidArgument` | `0x09` | Invalid argument |
| `StatusUnavailable` | `0x0A` | Service unavailable |

### Operations

#### Create Job (0x01)

Create a new scheduled job.

**Payload Structure:**

```
[0x01][JSON request body]
```

**Request JSON:**

```json
{
  "name": "daily-backup",
  "description": "Full database backup",
  "owner_type": "system",
  "owner_id": "",
  "target_tenant_id": "",
  "type": "recurring",
  "schedule": "0 2 * * *",
  "execute_at": 0,
  "delay": 0,
  "handler": "backup.full",
  "payload": "base64-encoded-data",
  "priority": 10,
  "timeout": 3600000000000,
  "max_retries": 3,
  "labels": {"env": "production"}
}
```

**Response JSON:**

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

#### Get Job (0x02)

Retrieve job by ID.

**Payload Structure:**

```
[0x02][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": ""
}
```

**Response JSON:**

```json
{
  "job": { ... },
  "timestamp": 1737331200
}
```

---

#### Update Job (0x03)

Update an existing job.

**Payload Structure:**

```
[0x03][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": "",
  "name": "updated-backup",
  "schedule": "0 3 * * *",
  "priority": 20
}
```

---

#### Delete Job (0x04)

Delete a scheduled job.

**Payload Structure:**

```
[0x04][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": "",
  "force": false
}
```

**Response JSON:**

```json
{
  "job_id": 12345,
  "deleted": true,
  "timestamp": 1737331200
}
```

---

#### List Jobs (0x05)

List jobs with filtering.

**Payload Structure:**

```
[0x05][JSON request body]
```

**Request JSON:**

```json
{
  "owner_type": "tenant",
  "owner_id": "tenant_123",
  "target_tenant_id": "",
  "status": "pending",
  "handler": "db.compaction",
  "labels": {"env": "production"},
  "limit": 100,
  "cursor": ""
}
```

**Response JSON:**

```json
{
  "jobs": [ ... ],
  "next_cursor": "eyJpZCI6MTIzNDV9",
  "has_more": true,
  "total_count": 250,
  "timestamp": 1737331200
}
```

---

#### Trigger Job (0x06)

Execute job immediately.

**Payload Structure:**

```
[0x06][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": ""
}
```

**Response JSON:**

```json
{
  "job_id": 12345,
  "status": "running",
  "timestamp": 1737331200
}
```

---

#### Pause Job (0x07)

Pause scheduled execution.

**Payload Structure:**

```
[0x07][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": ""
}
```

**Response JSON:**

```json
{
  "job_id": 12345,
  "old_status": "pending",
  "new_status": "paused",
  "timestamp": 1737331200
}
```

---

#### Resume Job (0x08)

Resume paused job.

**Payload Structure:**

```
[0x08][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": ""
}
```

---

#### Cancel Job (0x09)

Cancel job permanently.

**Payload Structure:**

```
[0x09][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": "",
  "reason": "No longer needed"
}
```

---

#### Get Job History (0x0A)

Get execution history.

**Payload Structure:**

```
[0x0A][JSON request body]
```

**Request JSON:**

```json
{
  "job_id": 12345,
  "tenant_id": "",
  "limit": 100,
  "cursor": ""
}
```

**Response JSON:**

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
      "output": "Success"
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "timestamp": 1737331200
}
```

---

#### Get Scheduler Status (0x0B)

Get scheduler health.

**Payload Structure:**

```
[0x0B]
```

No request body required (empty payload after sub-opcode).

**Response JSON:**

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

### Client Example (Go)

```go
package main

import (
    "context"
    "crypto/tls"
    "encoding/binary"
    "encoding/json"

    "github.com/quic-go/quic-go"
    "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
)

func main() {
    // TLS configuration
    tlsConf := &tls.Config{
        InsecureSkipVerify: false,
        NextProtos:         []string{"quicbaas"},
    }

    // Connect
    conn, err := quic.DialAddr(context.Background(), "localhost:9444", tlsConf, nil)
    if err != nil {
        panic(err)
    }
    defer conn.CloseWithError(0, "done")

    // Open stream
    stream, err := conn.OpenStreamSync(context.Background())
    if err != nil {
        panic(err)
    }

    // Build request payload
    reqBody := map[string]interface{}{
        "name":       "test-job",
        "owner_type": "system",
        "type":       "one-shot",
        "handler":    "test.handler",
    }
    bodyJSON, _ := json.Marshal(reqBody)

    // Prepend sub-opcode
    payload := append([]byte{quic.SubOpSchedulerCreateJob}, bodyJSON...)

    // Create request
    req := &quic.Request{
        Opcode:  quic.OpScheduler,
        Token:   "your-jwt-token",
        Payload: payload,
    }

    // Write request
    if err := quic.WriteRequest(stream, req); err != nil {
        panic(err)
    }

    // Read response
    resp, err := quic.ReadResponse(stream)
    if err != nil {
        panic(err)
    }

    if resp.Status == quic.StatusOK {
        var result map[string]interface{}
        json.Unmarshal(resp.Payload, &result)
        fmt.Printf("Job created: %v\n", result)
    } else {
        fmt.Printf("Error: status 0x%02X\n", resp.Status)
    }
}
```

### Protocol Limits

| Limit | Value |
|-------|-------|
| Max Payload Size | 16 MB (16,777,216 bytes) |
| Max Token Size | 8 KB (8,192 bytes) |

### Error Handling

The QUIC transport carries the REST requests over HTTP/3, so errors are the
same HTTP status and flat REST error body described in
[Error Responses](#error-responses).


---

## Scheduler MCP API Reference

### Overview

The Model Context Protocol (MCP) interface enables AI/LLM integration with the QuicBaaS scheduler. Tools are exposed for programmatic job management through AI agents.

### Connection

| Property | Value |
|----------|-------|
| Transport | stdio or HTTP |
| Port (HTTP) | 9445 |
| Protocol | JSON-RPC 2.0 |

### Authentication

MCP connections use session-based authentication established during the initial handshake.

### Available Tools

#### quicbaas_scheduler_create_job

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
    "node_affinity": {
      "type": "string",
      "description": "Node the job should run on (optional)"
    },
    "labels": {
      "type": "object",
      "description": "Key-value labels for categorization"
    }
  },
  "required": ["name", "type", "handler"]
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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

---

#### quicbaas_scheduler_get_job

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

#### quicbaas_scheduler_list_jobs

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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

---

#### quicbaas_scheduler_delete_job

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

#### quicbaas_scheduler_trigger_job

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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

---

#### quicbaas_scheduler_pause_job

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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

---

#### quicbaas_scheduler_resume_job

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

#### quicbaas_scheduler_cancel_job

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

#### quicbaas_scheduler_job_history

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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

---

#### quicbaas_scheduler_status

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
  "timestamp": "2025-01-20T00:00:00Z"
}
```

### Error Handling

MCP tools return errors in the standard JSON-RPC 2.0 error envelope; the
`message` is the scheduler error text. There are no scheduler-specific error
codes. See
[MCP Server API: Standard JSON-RPC Errors](../mcp-server/api.md#standard-json-rpc-errors).

### Usage Patterns

#### Creating a Recurring Job

```
Use quicbaas_scheduler_create_job with:
- name: "hourly-metrics"
- owner_type: "system"
- type: "recurring"
- schedule: "0 * * * *"
- handler: "metrics.collect"
```

#### Monitoring Job Status

```
1. Use quicbaas_scheduler_list_jobs with status: "running" to see active jobs
2. Use quicbaas_scheduler_get_job with job_id to check specific job
3. Use quicbaas_scheduler_job_history to view execution history
```

#### Managing Job Lifecycle

```
1. Create job with quicbaas_scheduler_create_job
2. Pause if needed with quicbaas_scheduler_pause_job
3. Resume with quicbaas_scheduler_resume_job
4. Cancel permanently with quicbaas_scheduler_cancel_job
5. Delete with quicbaas_scheduler_delete_job
```

#### Immediate Execution

```
Use quicbaas_scheduler_trigger_job to run a job immediately,
bypassing its normal schedule.
```


---

## Scheduler Unix Socket API Reference

### Overview

The Unix socket interface provides high-performance local IPC for CLI tools and local services communicating with the scheduler.

### Connection

| Property | Value |
|----------|-------|
| Socket Path | `/run/quicbaas.sock` |
| Protocol | Binary (same as QUIC wire protocol) |
| Authentication | Process UID/GID verification |

### Wire Protocol

The Unix socket uses the same binary protocol as QUIC for consistency.

#### Request Format

```
[opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M]
```

| Field | Size | Description |
|-------|------|-------------|
| `opcode` | 1 byte | Operation code (`0x00` for scheduler) |
| `tokenLen` | 2 bytes | Token length (big-endian, can be 0) |
| `token` | N bytes | Optional authentication token |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | M bytes | JSON-encoded request (first byte is sub-opcode) |

#### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| `status` | 1 byte | Status code |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | N bytes | JSON-encoded response |

### Authentication

Unix socket connections authenticate via:

1. **Process credentials** - UID/GID are verified via `SO_PEERCRED`
2. **Optional token** - JWT token for additional authorization

Root processes (`UID 0`) have full access. Non-root processes require explicit permissions.

### Opcodes

#### Primary Opcode

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpScheduler` | `0x00` | Scheduler dispatch opcode |

#### Sub-Opcodes

Encoded in the first byte of the payload:

| Sub-Opcode | Hex | Operation |
|------------|-----|-----------|
| `CreateJob` | `0x01` | Create job |
| `GetJob` | `0x02` | Get job by ID |
| `UpdateJob` | `0x03` | Update job |
| `DeleteJob` | `0x04` | Delete job |
| `ListJobs` | `0x05` | List jobs |
| `TriggerJob` | `0x06` | Trigger immediate execution |
| `PauseJob` | `0x07` | Pause job |
| `ResumeJob` | `0x08` | Resume job |
| `CancelJob` | `0x09` | Cancel job |
| `GetJobHistory` | `0x0A` | Get execution history |
| `GetStatus` | `0x0B` | Get scheduler status |

### Status Codes

| Code | Hex | Description |
|------|-----|-------------|
| `OK` | `0x00` | Success |
| `InvalidRequest` | `0x01` | Invalid request format |
| `NotFound` | `0x02` | Resource not found |
| `Unauthorized` | `0x03` | Authentication required |
| `InternalError` | `0x04` | Internal server error |
| `Timeout` | `0x05` | Operation timed out |
| `AlreadyExists` | `0x06` | Resource exists |
| `PermissionDenied` | `0x07` | Insufficient permissions |
| `ResourceExhausted` | `0x08` | Rate limited |
| `InvalidArgument` | `0x09` | Invalid argument |
| `Unavailable` | `0x0A` | Service unavailable |

### Operations

#### Create Job (0x01)

**Request:**

```
[0x00][0x00][0x00][len:4][0x01][JSON body]
```

**JSON Body:**

```json
{
  "name": "cleanup-job",
  "owner_type": "system",
  "type": "recurring",
  "schedule": "0 0 * * *",
  "handler": "cleanup.expired"
}
```

**Response:**

```json
{
  "job": {
    "id": 12345,
    "name": "cleanup-job",
    "status": "pending"
  },
  "timestamp": 1737331200
}
```

---

#### Get Job (0x02)

**Request:**

```
[0x00][0x00][0x00][len:4][0x02][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345
}
```

---

#### Update Job (0x03)

**Request:**

```
[0x00][0x00][0x00][len:4][0x03][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345,
  "name": "updated-name",
  "priority": 20
}
```

---

#### Delete Job (0x04)

**Request:**

```
[0x00][0x00][0x00][len:4][0x04][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345,
  "force": false
}
```

---

#### List Jobs (0x05)

**Request:**

```
[0x00][0x00][0x00][len:4][0x05][JSON body]
```

**JSON Body:**

```json
{
  "status": "pending",
  "handler": "cleanup.expired",
  "limit": 100
}
```

**Response:**

```json
{
  "jobs": [ ... ],
  "next_cursor": "...",
  "has_more": true,
  "total_count": 250,
  "timestamp": 1737331200
}
```

---

#### Trigger Job (0x06)

**Request:**

```
[0x00][0x00][0x00][len:4][0x06][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345
}
```

---

#### Pause Job (0x07)

**Request:**

```
[0x00][0x00][0x00][len:4][0x07][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345
}
```

---

#### Resume Job (0x08)

**Request:**

```
[0x00][0x00][0x00][len:4][0x08][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345
}
```

---

#### Cancel Job (0x09)

**Request:**

```
[0x00][0x00][0x00][len:4][0x09][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345,
  "reason": "No longer needed"
}
```

---

#### Get Job History (0x0A)

**Request:**

```
[0x00][0x00][0x00][len:4][0x0A][JSON body]
```

**JSON Body:**

```json
{
  "job_id": 12345,
  "limit": 50
}
```

---

#### Get Scheduler Status (0x0B)

**Request:**

```
[0x00][0x00][0x00][len:4][0x0B]
```

Empty JSON body or just `{}`.

**Response:**

```json
{
  "running": true,
  "stats": {
    "job_count": 250,
    "running_jobs": 5,
    "success_count": 12500,
    "failure_count": 15
  },
  "timestamp": 1737331200
}
```

### Client Example (Go)

```go
package main

import (
    "encoding/binary"
    "encoding/json"
    "fmt"
    "net"
)

const (
    OpScheduler          = 0x00
    SubOpCreateJob       = 0x01
    SubOpGetJob          = 0x02
    SubOpListJobs        = 0x05
    SubOpGetStatus       = 0x0B
    StatusOK             = 0x00
)

func main() {
    // Connect to Unix socket
    conn, err := net.Dial("unix", "/run/quicbaas.sock")
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    // Build request
    reqBody := map[string]interface{}{
        "name":       "test-job",
        "owner_type": "system",
        "type":       "one-shot",
        "handler":    "test.handler",
    }
    bodyJSON, _ := json.Marshal(reqBody)

    // Prepend sub-opcode
    payload := append([]byte{SubOpCreateJob}, bodyJSON...)

    // Build request frame
    // [opcode:1][tokenLen:2][token:0][payloadLen:4][payload:N]
    frame := make([]byte, 0, 1+2+4+len(payload))
    frame = append(frame, OpScheduler)
    frame = append(frame, 0x00, 0x00) // No token
    lenBuf := make([]byte, 4)
    binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))
    frame = append(frame, lenBuf...)
    frame = append(frame, payload...)

    // Send request
    _, err = conn.Write(frame)
    if err != nil {
        panic(err)
    }

    // Read response
    // [status:1][payloadLen:4][payload:N]
    statusBuf := make([]byte, 1)
    conn.Read(statusBuf)
    status := statusBuf[0]

    respLenBuf := make([]byte, 4)
    conn.Read(respLenBuf)
    respLen := binary.BigEndian.Uint32(respLenBuf)

    respPayload := make([]byte, respLen)
    conn.Read(respPayload)

    if status == StatusOK {
        var result map[string]interface{}
        json.Unmarshal(respPayload, &result)
        fmt.Printf("Response: %+v\n", result)
    } else {
        fmt.Printf("Error: status 0x%02X\n", status)
    }
}
```

### CLI Integration

The `dragonctl` CLI uses Unix socket for local operations:

```bash
# List pending jobs
dragonctl scheduler jobs list --status pending

# Create a job
dragonctl scheduler jobs create \
  --name "daily-backup" \
  --type recurring \
  --schedule "0 2 * * *" \
  --handler backup.full

# Trigger immediate execution
dragonctl scheduler jobs trigger 12345

# Get scheduler status
dragonctl scheduler status
```

### Protocol Limits

| Limit | Value |
|-------|-------|
| Max Payload Size | 16 MB (16,777,216 bytes) |
| Socket Timeout | 30 seconds |
| Max Connections | Limited by system resources |

### Security Considerations

1. **File permissions** - Socket file should be owned by `quicbaas` user/group with `0660` permissions
2. **SELinux/AppArmor** - Appropriate policies should restrict socket access
3. **Namespace isolation** - Use mount namespaces in containers to isolate socket access
4. **Credential verification** - Always verify `SO_PEERCRED` for process identity

### Error Handling

The Unix socket serves gRPC, so errors are the gRPC status codes listed in the
gRPC [Error Handling](#error-handling) section:

```go
if _, err := client.GetJob(ctx, req); err != nil {
    switch status.Code(err) {
    case codes.NotFound:
        // Handle not found
    case codes.PermissionDenied:
        // Handle permission denied
    default:
        // Handle other errors
    }
}
```

## Workload placement API

Placement shares the `quicbaas.Scheduler` service and the `SchedulerService`
gRPC service with the job API, and carries its own permission family because
go-qrdb owns `scheduler:*` (see `pkg/permission`):

| Permission | Verbs | Built-in roles |
|---|---|---|
| `placement:read` | `GetNode`, `ListNodes`, `GetPlacement`, `ListPlacements` | Admin, Operator |
| `placement:place` | `Place`, `Release` | Admin, Operator |
| `placement:manage` | `RegisterNode`, `RenewNode`, `DeregisterNode`, `CordonNode`, `UncordonNode`, `DrainNode` | Admin (node agents hold a custom role naming it) |

Every call is scoped to the authenticated request's tenant.

### Types

The JSON shapes are the `pkg/scheduler/placement` types; the SDK aliases them
(`scheduler.NodeFacts`, `NodeInfo`, `PlacementRequest`, `Placement`,
`PlaceResult`, `PlacementFilter`, `Constraint`, `AffinityTerm`,
`SpreadConstraint`) and re-exports them at the SDK root as `PlacementNodeFacts`,
`PlacementNodeInfo`, `PlacementRequest`, `Placement`, `PlaceResult`, ….

```json
// RegisterNode request (REST body; SDK scheduler.RegisterNodeRequest)
{"facts": {"node_id": "n1", "capacity": {"cpu_millis": 8000}, "labels": {"arch": "arm64", "zone": "a"}},
 "ttl_seconds": 30}

// NodeInfo
{"facts": {...}, "status": {"unschedulable": false, "draining": false},
 "lease_id": 7587, "ttl_seconds": 30, "allocated": {"cpu_millis": 2000},
 "placements": 1, "registered_at": "2026-10-01T12:00:00Z"}

// PlacementRequest
{"workload_id": "web-1", "group": "web", "resources": {"cpu_millis": 500},
 "constraints": [{"key": "arch", "operator": "In", "values": ["arm64"]}],
 "affinity": [{"group": "cache", "weight": 50}],
 "anti_affinity": [{"group": "web", "required": true}],
 "spread": {"topology_key": "zone", "max_skew": 1, "required": true},
 "strategy": "binpack"}

// PlaceResult
{"placement": {"workload_id": "web-1", "group": "web", "node_id": "n1",
  "resources": {"cpu_millis": 500}, "score": 31.25,
  "created_at": "2026-10-01T12:00:01Z", "orphaned": false},
 "existing": false}
```

Operators are `In`, `NotIn`, `Exists`, `DoesNotExist`; strategies are
`binpack` (default) and `spread`; a preferred term's `weight` is 1–100.

### REST

| Method | Route | Body / query | Response |
|---|---|---|---|
| POST | `/api/v1/scheduler/nodes` | RegisterNode request | `NodeInfo` |
| GET | `/api/v1/scheduler/nodes` | — | `{"nodes": [NodeInfo]}` |
| GET | `/api/v1/scheduler/nodes/{id}` | — | `NodeInfo` |
| DELETE | `/api/v1/scheduler/nodes/{id}` | — | `{"node_id", "deregistered": true}` |
| POST | `/api/v1/scheduler/nodes/{id}/renew` | — | `{"node_id", "renewed": true}` |
| POST | `/api/v1/scheduler/nodes/{id}/cordon` | — | `NodeInfo` |
| POST | `/api/v1/scheduler/nodes/{id}/uncordon` | — | `NodeInfo` |
| POST | `/api/v1/scheduler/nodes/{id}/drain` | — | `{"node_id", "placements": [Placement]}` |
| POST | `/api/v1/scheduler/placements` | `PlacementRequest` | `PlaceResult` |
| GET | `/api/v1/scheduler/placements` | `?node_id=&group=` | `{"placements": [Placement]}` |
| GET | `/api/v1/scheduler/placements/{id}` | — | `Placement` |
| DELETE | `/api/v1/scheduler/placements/{id}` | — | `{"workload_id", "released": true}` |

### gRPC

`quicbaas.scheduler.v1.SchedulerService` (`api/proto/scheduler.proto`):
`RegisterNode`, `RenewNode`, `DeregisterNode`, `GetNode`, `ListNodes`,
`CordonNode`, `UncordonNode`, `DrainNode`, `Place`, `Release`, `GetPlacement`,
`ListPlacements`. Messages are prefixed `Placement*` (all protos share one Go
package); times travel as `*_unix_nanos`. The service is registered whenever
either the job engine or the placement engine runs; a call to an absent half
answers `Unavailable`. Unix serves the same service over `AF_UNIX`.

### MCP

`quicbaas_scheduler_register_node` takes the facts flat (`node_id`,
`capacity`, `labels`, `ttl_seconds`); `quicbaas_scheduler_place` takes the
`PlacementRequest` fields; the node-keyed tools take `node_id`, the
workload-keyed ones `workload_id`. Results are the JSON above.

### Errors

| Condition | Engine type | Code | gRPC | HTTP |
|---|---|---|---|---|
| Malformed request | `InvalidRequestError` | `ErrInvalidInput` | InvalidArgument | 400 |
| Node not live | `NodeNotFoundError` | `ErrNotFound` | NotFound | 404 |
| Workload has no placement | `PlacementNotFoundError` | `ErrNotFound` | NotFound | 404 |
| No node passes the filters (reasons per node in the message) | `NoFeasibleNodeError` | `ErrPreconditionFailed` | FailedPrecondition | 412 |
| Workload already placed on a dead or draining node | `PlacementConflictError` | `ErrPreconditionFailed` | FailedPrecondition | 412 |
| Guarded write lost 16 times | `ContendedError` | `ErrAborted` (retryable) | Aborted | 409 |
| Stored record does not decode | `CorruptRecordError` | `ErrDataLoss` | DataLoss | 500 |

No-feasible-node is deliberately not `ResourceExhausted`: the SDK retries that
code, and re-asking an unchanged pool returns the same answer. REST echoes the
412 and 409 messages rather than sanitising them, because they describe the
request against the pool, not a server fault.

