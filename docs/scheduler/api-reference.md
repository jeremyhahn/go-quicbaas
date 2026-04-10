# Scheduler API Reference

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

- [REST API Reference](api-reference-rest.md)
- [gRPC API Reference](api-reference-grpc.md)
- [QUIC API Reference](api-reference-quic.md)
- [MCP API Reference](api-reference-mcp.md)
- [Unix Socket API Reference](api-reference-unix.md)

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

## Error Codes

| Code | Description |
|------|-------------|
| `JOB_NOT_FOUND` | Job ID does not exist |
| `INVALID_STATE_TRANSITION` | Invalid state change requested |
| `HANDLER_NOT_FOUND` | Unknown handler type |
| `INVALID_SCHEDULE` | Malformed schedule specification |
| `PERMISSION_DENIED` | Insufficient permissions |
| `TENANT_MISMATCH` | Job belongs to different tenant |
