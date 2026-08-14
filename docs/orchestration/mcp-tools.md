# MCP Tool Reference

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
