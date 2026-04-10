# Hooks Integration

## Overview

Claude Code fires hooks at key lifecycle events during agent team coordination. QuicBaaS exposes REST endpoints that receive these hook payloads, persist task state, and publish events to the messaging subsystem for cross-host coordination.

Three hook types are supported:

| Hook Event | Trigger | QuicBaaS Endpoint |
|---|---|---|
| `TaskCreated` | A teammate creates a new task | `POST /api/v1/hooks/task-created` |
| `TaskCompleted` | A teammate finishes a task | `POST /api/v1/hooks/task-completed` |
| `TeammateIdle` | A teammate has no work to do | `POST /api/v1/hooks/teammate-idle` |

## Configuration

### `.claude/settings.json`

Configure hooks to POST to QuicBaaS. For local (standalone) mode, use `localhost`. For remote agents, use the QuicBaaS server address.

**Local hooks:**

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ],
    "TaskCompleted": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-completed"
          }
        ]
      }
    ],
    "TeammateIdle": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/teammate-idle"
          }
        ]
      }
    ]
  }
}
```

**Remote hooks (agent on a different machine):**

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "https://quicbaas-server.internal:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ]
  }
}
```

## Request/Response Format

### HookPayload (request body)

All three endpoints accept the same payload shape. Required fields vary by hook type.

```json
{
  "hook_event_name": "TaskCreated",
  "task_id": "task_abc123",
  "task_subject": "Implement WebSocket transport",
  "task_description": "Add WebSocket upgrade handler in pkg/api/",
  "teammate_name": "backend-worker",
  "team_name": "platform-team",
  "session_id": "sess_x1y2z3",
  "cwd": "/home/user/sources/go-quicbaas"
}
```

**Required fields by hook type:**

| Field | TaskCreated | TaskCompleted | TeammateIdle |
|---|---|---|---|
| `hook_event_name` | required | required | required |
| `task_id` | required | required | -- |
| `team_name` | required | required | required |
| `teammate_name` | required | -- | required |

### HookResponse (response body)

```json
{
  "exit_code": 0,
  "stderr": ""
}
```

- `exit_code: 0` (or omitted) -- allow the action to proceed.
- `exit_code: 2` -- block the action with feedback in `stderr`.

## Endpoint Behavior

### POST /api/v1/hooks/task-created

1. Validates required fields (`hook_event_name`, `task_id`, `team_name`, `teammate_name`).
2. Creates a `Task` record in the distributed task store with status `pending`.
3. Publishes a `TaskCreated` event to messaging channel `orchestrator:tasks:<team_name>`.
4. Returns `exit_code: 0` to allow task creation.
5. If the task ID already exists, returns `exit_code: 2` with feedback.

### POST /api/v1/hooks/task-completed

1. Validates required fields (`hook_event_name`, `task_id`, `team_name`).
2. Marks the task as `completed` in the task store with a `TaskResult` containing session and teammate metadata.
3. Publishes a `TaskCompleted` event to messaging channel `orchestrator:tasks:<team_name>`.
4. Returns `exit_code: 0`.
5. Returns 404 if the task ID is not found.

### POST /api/v1/hooks/teammate-idle

1. Validates required fields (`hook_event_name`, `teammate_name`, `team_name`).
2. Queries the task store for pending tasks matching the team.
3. If pending tasks exist, returns `exit_code: 2` with `stderr: "New task available: <subject>"` to prevent the teammate from going idle.
4. If no pending tasks, returns `exit_code: 0` to allow idle.

## Tenant Resolution

The `X-Tenant-ID` request header sets the tenant namespace. If absent, the `team_name` field from the payload is used as the tenant ID.

## Error Handling

| Condition | HTTP Status | Response |
|---|---|---|
| Malformed JSON | 400 | Bad request message |
| Missing required field | 400 | Field-specific error |
| Task already exists (TaskCreated) | 200 | `exit_code: 2`, stderr message |
| Task not found (TaskCompleted) | 404 | Not found message |
| Task store not configured | 501 | Service not configured |
| Messaging not configured | 501 | Service not configured |
| Internal store failure | 500 | Processing error |

Messaging publish failures are logged but do not fail the hook response. The task is already persisted, so the hook returns success.

## Error Types

The hooks handler uses typed errors defined in `pkg/api/rest/handlers_hooks.go`:

- `HookPayloadError` -- invalid or missing required field.
- `HookProcessingError` -- task store or messaging failure (wraps cause for `errors.Is`/`errors.As`).
- `HookServiceNotConfiguredError` -- required service dependency (task store, messaging) is nil.
