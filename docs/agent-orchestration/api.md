# Agent Orchestration — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/agent-orchestration/`.

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

### `quicbaas.Orchestrator` — SDK package `sdk/go/client/orchestrator`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 33; MCP tools registered server-side: 27.

MCP tools: `quicbaas_agent_fleet_heartbeat`, `quicbaas_agent_fleet_list`, `quicbaas_agent_fleet_register`, `quicbaas_agent_fleet_remove`, `quicbaas_agent_fleet_status`, `quicbaas_orchestrator_broadcast`, `quicbaas_orchestrator_cancel_job`, `quicbaas_orchestrator_close`, `quicbaas_orchestrator_decompose`, `quicbaas_orchestrator_discover`, `quicbaas_orchestrator_get_job_status`, `quicbaas_orchestrator_health`, `quicbaas_orchestrator_inference`, `quicbaas_orchestrator_list`, `quicbaas_orchestrator_list_backends`, `quicbaas_orchestrator_list_jobs`, `quicbaas_orchestrator_list_sessions`, `quicbaas_orchestrator_prompt`, `quicbaas_orchestrator_register`, `quicbaas_orchestrator_rotate_account`, `quicbaas_orchestrator_session_history`, `quicbaas_orchestrator_spawn`, `quicbaas_orchestrator_start_job`, `quicbaas_orchestrator_status`, `quicbaas_orchestrator_task_create`, `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `AgentHeartbeat` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CancelJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ClaimTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CloseSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CompleteTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CreateTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CreateWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `DecomposeTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `DeleteWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `DisableAgent` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `EnableAgent` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetAgent` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetJobStatus` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetSessionHistory` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `GetTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetTaskArtifact` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `Health` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Inference` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `InferenceStream` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListAgents` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListBackendModels` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListBackends` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListJobs` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListSessions` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListTaskArtifacts` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListTasks` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListWorkflows` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `PromptSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RegisterAgent` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RemoveAgent` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RotateSessionAccount` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SessionStatus` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SpawnChildJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SpawnSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `StartJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubmitTaskArtifacts` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubscribeJobEvents` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubscribeSessionEvents` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `UpdateWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |


Request and response schemas for orchestration tools exposed via the Model Context Protocol. Agent tools are registered when the `mcp` build tag is active. RAG indexing tools additionally require the `rag` and `vector` build tags.

**This page is not exhaustive.** It covers 19 of the 32 tools registered by `orchestrator_tools.go` (27), `orchestrator_cache_tools.go` (2) and `rag_index_tools.go` (3). The following 13 registered tools have no schema section here yet: `quicbaas_orchestrator_start_job`, `_get_job_status`, `_cancel_job`, `_list_jobs`, `_task_create`, `_inference`, `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts`, and the five `quicbaas_agent_fleet_*` tools. For the complete registered inventory with permissions, see [../agents/mcp-tools.md](../agents/mcp-tools.md); for the pooled-account family see [accounts.md](accounts.md).

## Agent Tools

### quicbaas_orchestrator_discover

Discover agent-enabled projects by scanning a directory tree for `CLAUDE.md` and `.claude/agents/` files.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `root_path` | string | Yes | Root directory path to scan for projects |

**Example request:**
```json
{
  "root_path": "/home/user/sources"
}
```

**Example response:**
```json
{
  "projects": [
    {
      "name": "go-quicbaas",
      "path": "/home/user/sources/go-quicbaas",
      "has_claude_md": true,
      "has_agents_dir": true
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_register

Register a project for agent orchestration with optional backend and model defaults.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Human-readable project name |
| `path` | string | Yes | Absolute filesystem path to the project |
| `dependencies` | string[] | No | List of project IDs this project depends on |
| `default_backend` | string | No | Default backend name for this project |
| `default_model` | string | No | Default model for this project |
| `labels` | object | No | Key-value labels for project categorization |

**Example request:**
```json
{
  "name": "go-quicbaas",
  "path": "/home/user/sources/go-quicbaas",
  "default_backend": "claude",
  "default_model": "claude-sonnet-4-20250514",
  "labels": {"language": "go", "tier": "core"}
}
```

**Example response:**
```json
{
  "project_id": "proj_abc123",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_list

List all registered agent projects with optional pagination.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `page_size` | integer | No | Maximum number of projects to return per page |
| `page_token` | string | No | Pagination token from previous response |

**Example request:**
```json
{
  "page_size": 10
}
```

**Example response:**
```json
{
  "projects": [
    {
      "id": "proj_abc123",
      "name": "go-quicbaas",
      "path": "/home/user/sources/go-quicbaas"
    }
  ],
  "next_page_token": "",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_spawn

Spawn a persistent, multi-turn ACP agent session on a backend with project
context. Mirrors `POST /api/v1/orchestrator/sessions`. Permission:
`orchestrator:spawn`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `backend` | string | Yes | Backend name to spawn the session on (e.g., `"claude"`, `"codex"`) |
| `project_id` | string | Yes | Registered project ID to associate with the session |
| `model` | string | No | Model to use (falls back to project/backend default) |
| `system_prompt` | string | No | System prompt to configure the agent |
| `work_dir` | string | No | Working directory for the agent process (defaults to the project path) |
| `sandbox_mode` | string | No | `default` \| `required` \| `none`; empty defers to the server SandboxPolicy |
| `host_affinity` | string | No | Pin the session to a daemon by Name or Host |
| `host_affinity_strict` | boolean | No | Fail instead of falling back when the pinned host is offline |
| `gpu_required` | boolean | No | Restrict dispatch to daemons advertising GPU capability |
| `persistent` | boolean | No | Multi-turn session keeping context across prompts (**default `true`**) |
| `account_id` | string | No | Pin the session to a pooled provider account. A bare pin is deliberate: the session is never rotated off it. |
| `account_auto` | boolean | No | Select a pooled account with the named strategy, and authorize rotation across the whole pool |
| `account_strategy` | string | No | `static` \| `least_usage`; empty selects the registry default. Ignored unless `account_auto` is true. |
| `account_allow` | string[] | No | Restrict the session to these pooled account IDs and authorize it to rotate between them |
| `mcp_servers` | object[] | No | MCP servers to attach to the session |

Each MCP server object:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Server name |
| `command` | string | Executable command |
| `args` | string[] | Command arguments |
| `url` | string | Remote server URL (alternative to `command`) |
| `env` | object | Environment variables for the server process |

**Example request:**
```json
{
  "tenant_id": "acme",
  "backend": "claude",
  "project_id": "proj_abc123",
  "model": "claude-sonnet-4-20250514",
  "system_prompt": "You are a Go backend engineer.",
  "mcp_servers": [
    {"name": "quicbaas", "command": "quicbaas", "args": ["mcp", "serve"]}
  ]
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "backend": "acp",
  "model": "claude-sonnet-4-20250514",
  "tenant_id": "acme",
  "persistent": true,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

> `persistent` defaults to `true` because a one-shot spawn registers no durable
> session and a follow-up prompt would fail with "session not found". Use
> `quicbaas_orchestrator_inference` for stateless calls.

---

### quicbaas_orchestrator_prompt

Send a prompt to an agent session or backend. Provide `session_id` for existing sessions, or `backend` for stateless prompts.

Permission: `orchestrator:prompt`.

**A session prompt is asynchronous.** MCP has no server-push channel, so the
tool returns only an acknowledgement — it never carries the agent's output.
Read the turn back with `quicbaas_orchestrator_session_history`, paging from the
returned `sequence` until an event of kind `turn_completed` or `turn_failed`
carrying the same `turn_id` appears. See
[Asynchronous Agent Turns](async-turns.md).

A prompt with no `session_id` is a stateless one-shot: it has no event log to
publish to, so the completed turn is returned inline under `content`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `prompt` | string | Yes | The prompt text to send to the agent |
| `backend` | string | No | Backend name (optional if `session_id` provided) |
| `session_id` | string | No | Existing session ID to route the prompt to |
| `project_id` | string | No | Project ID for context resolution |
| `model` | string | No | Model to use (falls back to project/backend default) |
| `system_prompt` | string | No | System prompt to configure the agent |
| `use_rag` | boolean | No | Whether to use RAG for context enrichment (default: false) |

**Example request (session):**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789",
  "prompt": "Add error handling to the HTTP handler."
}
```

**Example request (stateless):**
```json
{
  "tenant_id": "acme",
  "backend": "ollama",
  "prompt": "Explain the builder pattern in Go.",
  "model": "llama3"
}
```

**Example response (session prompt — acknowledgement):**
```json
{
  "session_id": "sess_xyz789",
  "turn_id": "9f3c1e40-8a21-4c6f-9f0e-1d2b3c4d5e6f",
  "status": "accepted",
  "sequence": 42,
  "backend": "claude",
  "model": "claude-sonnet-4-20250514",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

**Example response (stateless prompt — completed turn):**
```json
{
  "session_id": "",
  "content": [
    {"type": "text", "text": "The builder pattern in Go..."}
  ],
  "model": "llama3",
  "stop_reason": "end_turn",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_session_history

Read a page of a session's durable event transcript: prompts, assistant output,
tool calls, and turn lifecycle events. Permission: `orchestrator:status`.

This is the MCP replacement for a streaming attach. Page from the `sequence`
returned by `quicbaas_orchestrator_prompt` until the turn's terminal event
appears.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session whose transcript to read |
| `from_sequence` | integer | No | Inclusive lower bound on the event sequence (default: start of session) |
| `limit` | integer | No | Maximum events to return (default: 256) |

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "tenant_id": "acme",
  "events": [
    {"sequence": 42, "kind": "turn_started", "turn_started": {"turn_id": "9f3c1e40-..."}},
    {"sequence": 43, "kind": "agent_thought", "thought": {"text": "The handler needs a nil check before..."}},
    {"sequence": 44, "kind": "assistant_chunk", "assistant_chunk": {"text": "I've updated the handler..."}},
    {"sequence": 45, "kind": "turn_completed", "turn_completed": {"turn_id": "9f3c1e40-...", "stop_reason": "end_turn"}}
  ],
  "next_seq": 0
}
```

---

### quicbaas_orchestrator_status

Get the status of an active agent session. Permission: `orchestrator:status`.

MCP is a request/response protocol with no server-initiated streaming channel,
so there is no `attach` tool. This tool reports the *session's* lifecycle state
(active, closed); it cannot tell you whether a particular turn has finished —
use `quicbaas_orchestrator_session_history` for that. Live event streaming stays
on the REST SSE and gRPC server-streaming endpoints.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session ID to query |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789"
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "state": "idle",
  "last_activity": 1710590400,
  "uptime": 3600,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_close

Close and terminate an active agent session. Permission: `orchestrator:close`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session ID to close |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789"
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_rotate_account

Move an agent session onto a different pooled provider account. Mirrors
`POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account`. Permission:
`orchestrator:spawn` — the rotation closes the agent process and reconstitutes
it against a newly selected pooled credential, which is a spawn's authority and
not a close's.

Only a session spawned with `account_auto` or `account_allow` may rotate; a
bare `account_id` pin is honored and answered with a not-rotatable error.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session to move |
| `reason` | string | No | Recorded on the closed account segment and in the session transcript |
| `hold_outgoing` | boolean | No | Hold the outgoing account out of the pool until `available_at` |
| `available_at` | string | No | RFC 3339 time the held account rejoins the pool; ignored unless `hold_outgoing` is true, empty applies the server default backoff |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789",
  "reason": "seat retired",
  "hold_outgoing": true
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "previous_account_id": "acct_a",
  "account_id": "acct_b",
  "strategy": "least_usage",
  "mode": "resume"
}
```

`mode` is `in_place` when the credential was swapped with no process restart,
or `resume` when the agent process was reconstituted and the transcript
replayed.

---

### quicbaas_orchestrator_list_sessions

List all active agent sessions for a tenant, optionally filtered by backend.
Permission: `orchestrator:list`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant whose sessions are listed |
| `backend` | string | No | Filter sessions by backend name |

**Example request:**
```json
{
  "tenant_id": "acme",
  "backend": "claude"
}
```

**Example response:**
```json
{
  "sessions": [
    {
      "id": "sess_xyz789",
      "project_id": "proj_abc123",
      "backend": "acp",
      "model": "claude-sonnet-4-20250514",
      "status": "idle"
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_list_backends

List all registered agent backends with their capabilities. Takes no parameters.

**Example request:**
```json
{}
```

**Example response:**
```json
{
  "backends": [
    {
      "name": "claude",
      "type": "acp",
      "capabilities": {
        "persistent_sessions": true,
        "tool_use": true,
        "file_access": true,
        "streaming": false,
        "acp": true
      }
    },
    {
      "name": "ollama",
      "type": "ollama",
      "capabilities": {
        "persistent_sessions": false,
        "tool_use": false,
        "file_access": false,
        "streaming": false,
        "acp": false
      }
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_decompose

Use an AI agent to decompose a complex task into discrete work items with dependencies and priorities.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `backend` | string | Yes | Backend to use for task decomposition |
| `task` | string | Yes | Task description to decompose |
| `model` | string | No | Model to use |
| `project_ids` | string[] | No | Target project IDs for context |
| `use_rag` | boolean | No | Whether to use RAG for context enrichment (default: false) |

**Example request:**
```json
{
  "backend": "claude",
  "task": "Implement RBAC with role hierarchy and permission inheritance",
  "project_ids": ["proj_abc123"],
  "use_rag": true
}
```

**Example response:**
```json
{
  "work_items": [
    {
      "title": "Define role and permission data models",
      "description": "Create Role, Permission, and RoleHierarchy types...",
      "priority": 1,
      "dependencies": []
    },
    {
      "title": "Implement role hierarchy resolution",
      "description": "Build a DAG-based resolver for inherited permissions...",
      "priority": 2,
      "dependencies": ["Define role and permission data models"]
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_broadcast

Broadcast a message to a messaging channel for agent coordination.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Messaging channel to broadcast to |
| `message` | string | Yes | Message content to broadcast |
| `metadata` | object | No | Additional metadata to include with the message |

**Example request:**
```json
{
  "channel": "agent-updates",
  "message": "Tests passing on go-quicbaas, ready for integration.",
  "metadata": {"project_id": "proj_abc123", "status": "complete"}
}
```

**Example response:**
```json
{
  "published": true,
  "channel": "agent-updates",
  "message_id": "msg_def456",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_health

Check health of all active agent sessions. Returns per-session status and summary counts. Takes no parameters.

**Example request:**
```json
{}
```

**Example response:**
```json
{
  "total_sessions": 3,
  "healthy": 2,
  "unhealthy": 0,
  "closed": 1,
  "results": [
    {
      "session_id": "sess_xyz789",
      "backend": "acp",
      "state": "idle",
      "healthy": true
    },
    {
      "session_id": "sess_abc456",
      "backend": "acp",
      "state": "active",
      "healthy": true
    },
    {
      "session_id": "sess_def012",
      "backend": "ollama",
      "state": "closed",
      "healthy": false
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

Sessions in `active` or `idle` state are considered healthy. Sessions in `error` or `closed` state are flagged as unhealthy.

---

## RAG Index Tools

Build tag: `mcp && rag`

### quicbaas_rag_index_file

Index a single file: read, chunk by language, generate embeddings, and upsert into the specified vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | Yes | Absolute path to the file to index |
| `namespace` | string | Yes | Vector namespace to store embeddings in |
| `model` | string | No | Embedding model to use (uses default if not specified) |

Supported languages for chunking: Go, Python, JavaScript, TypeScript, Rust, Java, C, C++, Ruby, Shell, Markdown, YAML, JSON, TOML, SQL. Unrecognized extensions fall back to `"text"`.

**Example request:**
```json
{
  "file_path": "/home/user/sources/go-quicbaas/pkg/orchestration/backend.go",
  "namespace": "go-quicbaas"
}
```

**Example response:**
```json
{
  "file_path": "/home/user/sources/go-quicbaas/pkg/orchestration/backend.go",
  "chunk_count": 8,
  "namespace": "go-quicbaas",
  "language": "go",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_rag_index_project

Index all matching files in a project directory: scan files, chunk by language, batch embed, and upsert into vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_path` | string | Yes | Absolute path to the project directory to index |
| `namespace` | string | Yes | Vector namespace to store embeddings in |
| `file_pattern` | string | No | Glob pattern for matching files (default: `*.go`) |
| `model` | string | No | Embedding model to use (uses default if not specified) |

**Example request:**
```json
{
  "project_path": "/home/user/sources/go-quicbaas",
  "namespace": "go-quicbaas",
  "file_pattern": "*.go"
}
```

**Example response:**
```json
{
  "project_path": "/home/user/sources/go-quicbaas",
  "file_count": 47,
  "total_chunk_count": 312,
  "namespace": "go-quicbaas",
  "file_pattern": "*.go",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_rag_index_status

Show indexing status for a vector namespace: vector count, dimension, last indexed timestamp.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `namespace` | string | Yes | Vector namespace to query status for |

**Example request:**
```json
{
  "namespace": "go-quicbaas"
}
```

**Example response:**
```json
{
  "namespace": "go-quicbaas",
  "vector_count": 312,
  "dimension": 1536,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

## Agent Cache Tools

Build tag: `mcp`

These tools store and retrieve expensive analysis results in the KV store for sharing between agent sessions. Keys are scoped by project ID using a composite key (`project_id:key`) hashed via FNV-1a.

### quicbaas_orchestrator_cache_put

Store an analysis result in the agent cache with optional TTL.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | Yes | Project identifier for scoping cached data |
| `key` | string | Yes | Cache key for the analysis result |
| `value` | string | Yes | Analysis result value to cache |
| `ttl_seconds` | integer | No | Time-to-live in seconds (0 means no expiry) |

**Example request:**
```json
{
  "project_id": "proj_abc123",
  "key": "dependency-analysis",
  "value": "{\"packages\": 47, \"external_deps\": 12, \"circular\": false}",
  "ttl_seconds": 3600
}
```

**Example response:**
```json
{
  "project_id": "proj_abc123",
  "key": "dependency-analysis",
  "stored": true,
  "version": 1,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_cache_get

Retrieve a cached analysis result by project and key.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | Yes | Project identifier for scoping cached data |
| `key` | string | Yes | Cache key to retrieve |

**Example request:**
```json
{
  "project_id": "proj_abc123",
  "key": "dependency-analysis"
}
```

**Example response (found):**
```json
{
  "project_id": "proj_abc123",
  "key": "dependency-analysis",
  "value": "{\"packages\": 47, \"external_deps\": 12, \"circular\": false}",
  "found": true,
  "version": 1,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

**Example response (not found):**
```json
{
  "project_id": "proj_abc123",
  "key": "dependency-analysis",
  "value": "",
  "found": false,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```


---

## MCP Tools Reference

MCP tool naming conventions and the complete registered inventory of
orchestration tools with their authorization mappings. Request and response
schemas live in [orchestration/mcp-tools.md](../orchestration/mcp-tools.md).

Registration sites: `pkg/api/mcp/orchestrator_tools.go` (27),
`orchestrator_cache_tools.go` (2), `account_tools.go` (9),
`project_tools.go` (5), `agent_admin_tools.go` (4) — 47 orchestration
tools in total, all behind the `mcp` build tag.

### Naming Convention

The orchestration surface uses six tool prefixes:

| Prefix | Count | Scope | Description |
|--------|-------|-------|-------------|
| `quicbaas_orchestrator_*` | 22 | Session / project / job management | Agent lifecycle, project discovery, ACP session management, jobs, agent cache |
| `quicbaas_agent_account_*` | 9 | Pooled provider accounts | Credential envelopes, rotation, usage — see [accounts.md](../orchestration/accounts.md) |
| `quicbaas_agent_fleet_*` | 5 | Fleet registry | Register / list / heartbeat the QuicBaaS agent daemons themselves |
| `project_*` | 7 | Project CRUD and agent grants | `project_create`/`get`/`list`/`update`/`delete`, `project_grant_agent`, `project_revoke_agent` |
| `quicbaas_task_*` | 2 | Task artifacts | `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts` |
| `agent_*` | 2 | Global agent admin | `agent_disable`, `agent_enable` |

The `quicbaas_orchestrator_*` prefix refers to operations on the orchestrator
surface (projects, ACP sessions, jobs, cache). The `quicbaas_agent_fleet_*`
prefix refers to the QuicBaaS agent infrastructure (the binary running on
remote systems).

Every one of these tools resolves to a qrdb-canonical permission in
`pkg/api/mcp/authz_qrdb.go`, but not all to the same family:
`quicbaas_orchestrator_*`, `quicbaas_agent_fleet_*`, `quicbaas_task_*` and
`agent_*` map to `orchestrator:<action>`; `project_*` maps to
`project:<action>`; `quicbaas_agent_account_*` maps to
`credentials:<action>`, because a pooled account is a stored credential
envelope. A tool with no mapping is default-denied and never appears in
`tools/list`.

### quicbaas_orchestrator_* Tools

Registered by `orchestrator_tools.go`, except the two cache tools, which come
from `orchestrator_cache_tools.go`.

| Tool | Permission | Description |
|------|------------|-------------|
| `quicbaas_orchestrator_discover` | `orchestrator:discover` | Scan directory tree for agent-enabled projects |
| `quicbaas_orchestrator_register` | `orchestrator:agent_register` | Register a project for orchestration |
| `quicbaas_orchestrator_list` | `orchestrator:list` | List registered projects (paginated) |
| `quicbaas_orchestrator_spawn` | `orchestrator:spawn` | Create a persistent session on a backend |
| `quicbaas_orchestrator_prompt` | `orchestrator:prompt` | Send a prompt to a session or backend |
| `quicbaas_orchestrator_session_history` | `orchestrator:status` | Page through a session's durable event transcript |
| `quicbaas_orchestrator_status` | `orchestrator:status` | Query session state, uptime, last activity |
| `quicbaas_orchestrator_close` | `orchestrator:close` | Terminate an active session |
| `quicbaas_orchestrator_rotate_account` | `orchestrator:spawn` | Move a session onto a different pooled account |
| `quicbaas_orchestrator_list_sessions` | `orchestrator:list` | List active sessions (filterable by backend) |
| `quicbaas_orchestrator_list_backends` | `orchestrator:backend_list` | List registered backends and capabilities |
| `quicbaas_orchestrator_decompose` | `orchestrator:task_decompose` | Decompose a task into prioritized work items |
| `quicbaas_orchestrator_health` | `orchestrator:health` | Check health of all active sessions |
| `quicbaas_orchestrator_broadcast` | `orchestrator:broadcast` | Publish a message to a coordination channel |
| `quicbaas_orchestrator_inference` | `orchestrator:inference` | Stateless inference against a fleet-resident daemon |
| `quicbaas_orchestrator_start_job` | `orchestrator:job_start` | Start an autonomous, iterating orchestration job |
| `quicbaas_orchestrator_get_job_status` | `orchestrator:job_read` | Query a job's iteration count and task progress |
| `quicbaas_orchestrator_cancel_job` | `orchestrator:job_cancel` | Cancel a running orchestration job |
| `quicbaas_orchestrator_list_jobs` | `orchestrator:job_list` | List jobs for the tenant, newest first |
| `quicbaas_orchestrator_task_create` | `orchestrator:task_create` | Create a task, optionally dispatching it at once |
| `quicbaas_orchestrator_cache_put` | `orchestrator:cache_write` | Store analysis result in KV (project-scoped) |
| `quicbaas_orchestrator_cache_get` | `orchestrator:cache_read` | Retrieve cached result by project and key |

### quicbaas_agent_fleet_* Tools

| Tool | Permission | Description |
|------|------------|-------------|
| `quicbaas_agent_fleet_register` | `orchestrator:agent_register` | Register an agent with the fleet |
| `quicbaas_agent_fleet_list` | `orchestrator:agent_list` | List fleet agents for a tenant |
| `quicbaas_agent_fleet_status` | `orchestrator:agent_read` | Get one fleet agent's status |
| `quicbaas_agent_fleet_remove` | `orchestrator:agent_remove` | Remove an agent from the fleet registry |
| `quicbaas_agent_fleet_heartbeat` | `orchestrator:agent_heartbeat` | Update an agent's last-seen timestamp |

Full request and response schemas for every tool above are in
[orchestration/mcp-tools.md](../orchestration/mcp-tools.md); the pooled-account
family is documented in [orchestration/accounts.md](../orchestration/accounts.md).

### Planned: `quicbaas_orch_*` coordination tools

**Not implemented.** No `quicbaas_orch_*` tool is registered anywhere in
`pkg/api/mcp` — the only `quicbaas_orch_*` identifiers that exist in the
codebase are Prometheus metric names in `pkg/metrics/metrics.go` (listed
below). The tools in this section are a design sketch for a namespace that
orchestrated AI agents (Claude Code, Codex, OpenCode) would use to coordinate
with each other. Treat them as planned, not callable.

#### Peer messaging (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox | `to`, `action`, `content`, `channel` (optional), `metadata` (optional) |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages | `since` (optional, unix nanos) |

#### File locking (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path | `file_path`, `project_id` |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock | `lock_key` |
| `quicbaas_orch_file_lock_status` | Check lock status for a file or list active locks | `file_path` (optional), `project_id` |

### Observability metrics

These are Prometheus metrics, not MCP tools. They are registered in
`pkg/metrics/metrics.go` and are live today.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_orch_peer_messages_total` | Counter | Total peer messages sent |
| `quicbaas_orch_peer_inbox_size` | Gauge | Current inbox size per agent |
| `quicbaas_orch_file_locks_active` | Gauge | Number of active file locks |
| `quicbaas_orch_file_lock_wait_duration_seconds` | Histogram | Time spent waiting for lock release |
| `quicbaas_orch_agent_tokens_total` | Counter | Total tokens consumed per agent |
| `quicbaas_orch_job_budget_utilization` | Gauge | Budget utilization percentage per job |
| `quicbaas_orch_agent_restarts_total` | Counter | Total agent restarts |
