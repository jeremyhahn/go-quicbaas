# Scheduler QUIC API Reference

## Overview

The QUIC transport provides high-performance, low-latency access to the scheduler API with benefits including:

- **0-RTT connection establishment** for reduced latency
- **Stream multiplexing** without head-of-line blocking
- **Connection migration** for mobile clients
- **Built-in encryption** via TLS 1.3

## Connection

| Property | Value |
|----------|-------|
| Host | `localhost` |
| Port | `9444` |
| Protocol | QUIC over UDP |
| TLS | TLS 1.3 (required) |

## Wire Protocol

### Request Format

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

### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| `status` | 1 byte | Status code |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | N bytes | JSON-encoded response |

## Opcodes

### Primary Opcode

The scheduler uses a single dispatch opcode with sub-operations:

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpScheduler` | `0x00` | Scheduler dispatch opcode |

### Sub-Opcodes

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

## Status Codes

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

## Operations

### Create Job (0x01)

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

### Get Job (0x02)

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

### Update Job (0x03)

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

### Delete Job (0x04)

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

### List Jobs (0x05)

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

### Trigger Job (0x06)

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

### Pause Job (0x07)

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

### Resume Job (0x08)

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

### Cancel Job (0x09)

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

### Get Job History (0x0A)

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

### Get Scheduler Status (0x0B)

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

## Client Example (Go)

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

## Protocol Limits

| Limit | Value |
|-------|-------|
| Max Payload Size | 16 MB (16,777,216 bytes) |
| Max Token Size | 8 KB (8,192 bytes) |

## Error Handling

Errors are returned with a non-zero status code and optional JSON error payload:

```json
{
  "code": "SCHEDULER_JOB_NOT_FOUND",
  "message": "Job with ID 12345 not found"
}
```

Check the status byte first, then parse the error payload if status is not `0x00`.
