# Scheduler Unix Socket API Reference

## Overview

The Unix socket interface provides high-performance local IPC for CLI tools and local services communicating with the scheduler.

## Connection

| Property | Value |
|----------|-------|
| Socket Path | `/run/quicbaas.sock` |
| Protocol | Binary (same as QUIC wire protocol) |
| Authentication | Process UID/GID verification |

## Wire Protocol

The Unix socket uses the same binary protocol as QUIC for consistency.

### Request Format

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

### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| `status` | 1 byte | Status code |
| `payloadLen` | 4 bytes | Payload length (big-endian) |
| `payload` | N bytes | JSON-encoded response |

## Authentication

Unix socket connections authenticate via:

1. **Process credentials** - UID/GID are verified via `SO_PEERCRED`
2. **Optional token** - JWT token for additional authorization

Root processes (`UID 0`) have full access. Non-root processes require explicit permissions.

## Opcodes

### Primary Opcode

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpScheduler` | `0x00` | Scheduler dispatch opcode |

### Sub-Opcodes

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

## Status Codes

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

## Operations

### Create Job (0x01)

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

### Get Job (0x02)

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

### Update Job (0x03)

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

### Delete Job (0x04)

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

### List Jobs (0x05)

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

### Trigger Job (0x06)

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

### Pause Job (0x07)

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

### Resume Job (0x08)

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

### Cancel Job (0x09)

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

### Get Job History (0x0A)

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

### Get Scheduler Status (0x0B)

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

## Client Example (Go)

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

## CLI Integration

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

## Protocol Limits

| Limit | Value |
|-------|-------|
| Max Payload Size | 16 MB (16,777,216 bytes) |
| Socket Timeout | 30 seconds |
| Max Connections | Limited by system resources |

## Security Considerations

1. **File permissions** - Socket file should be owned by `quicbaas` user/group with `0660` permissions
2. **SELinux/AppArmor** - Appropriate policies should restrict socket access
3. **Namespace isolation** - Use mount namespaces in containers to isolate socket access
4. **Credential verification** - Always verify `SO_PEERCRED` for process identity

## Error Handling

```go
if status != StatusOK {
    var errResp struct {
        Code    string `json:"code"`
        Message string `json:"message"`
    }
    json.Unmarshal(respPayload, &errResp)

    switch errResp.Code {
    case "SCHEDULER_JOB_NOT_FOUND":
        // Handle not found
    case "SCHEDULER_UNAUTHORIZED":
        // Handle permission denied
    default:
        // Handle other errors
    }
}
```
