# Agent Orchestration Overview

## Purpose

Agent orchestration extends go-quicbaas with lightweight management of AI coding agents (Claude Code, Codex, OpenCode, Ollama) across Go projects. A single quicbaas instance discovers, registers, spawns, prompts, and monitors agent sessions while reusing existing platform primitives for storage, messaging, security, and scheduling.

## Design Principle

Minimal new code. The orchestration layer is a thin coordination surface over existing quicbaas services:

| Reused Primitive    | Orchestration Use                              |
|---------------------|------------------------------------------------|
| KV Store            | Agent cache (shared analysis results)          |
| Messaging           | Agent broadcast/coordination channels          |
| RAG + Vector Store  | Codebase indexing and context enrichment        |
| Scheduler           | Periodic task orchestration                    |
| IAM                 | Tenant scoping, RBAC, per-object ACL           |
| Secrets Store       | Backend API keys and credentials               |
| Parameter Store     | Backend configuration and model defaults       |
| Object Encryption   | Encryption at rest for catalog entries          |

## Architecture

```
+------------------------+     +-----------------+     +-------------------+
|      MCP Clients       |     |   MCP Clients   |     |   MCP Clients     |
| (Claude,Codex,OpenCode)|     |   (Ollama)      |     | (Custom Agents)   |
+----------+-------------+     +--------+--------+     +---------+---------+
        |                        |                        |
        +----------+-------------+-------------+----------+
                   |                           |
          +--------v---------+        +--------v---------+
          | Agent MCP Tools  |        | RAG/Cache Tools  |
          | (12 tools)       |        | (5 tools)        |
          +--------+---------+        +--------+---------+
                   |                           |
          +--------v---------------------------v---------+
          |             Agent Service                     |
          |    (service.AgentService interface)           |
          +--------+----------------------------------+--+
                   |                                  |
          +--------v---------+            +-----------v------+
          | Backends         |            | QuicBaaS Services|
          | - ACPBackend     |            | - KV, Messaging  |
          |   (claude,codex, |            | - RAG, Vector    |
          |    opencode)     |            |                  |
          | - OllamaBackend  |            |                  |
          +------------------+            +------------------+
```

## Build Tags

The ACP and agent service packages are always compiled into the binary. Only RAG indexing and MCP tool registration require optional build tags:

```bash
make build WITH_RAG=1 WITH_VECTOR=1 WITH_MCP=1
```

This sets the `rag`, `vector`, and `mcp` build tags. Without `WITH_RAG=1` and `WITH_VECTOR=1`, RAG indexing tools are excluded. Without `WITH_MCP=1`, the MCP tool layer is excluded.

## Package Layout

```
pkg/acp/
    backend.go          AgentBackend interface, request/response types
    acp_backend.go      ACP-compatible backend (Claude Code, Codex via JSON-RPC/stdio)
    ollama_backend.go   Ollama stateless backend
    callbacks.go        File system callback handler with path validation
    types.go            ACP JSON-RPC message types
    errors.go           Typed errors

pkg/service/
    agent_types.go      AgentService interface, request/response types, sentinel errors
    agent_store.go      AgentStore interface, SessionRecord, in-memory implementation
    agent_health.go     AgentHealthMonitor, HealthCheckReport, SessionHealthResult

pkg/api/mcp/
    agent_tools.go      11 agent management MCP tools
    agent_cache_tools.go 2 agent cache MCP tools (KV-backed)
    rag_index_tools.go  3 RAG indexing MCP tools (vector-backed)
```

## MCP Tools (17 total)

### Agent Management (12 tools)

| Tool                             | Description                                     |
|----------------------------------|-------------------------------------------------|
| `quicbaas_agent_discover`        | Scan directory tree for agent-enabled projects   |
| `quicbaas_agent_register`        | Register a project for orchestration             |
| `quicbaas_agent_list`            | List registered projects (paginated)             |
| `quicbaas_agent_spawn`           | Create a persistent session on a backend         |
| `quicbaas_agent_prompt`          | Send a prompt to a session or backend            |
| `quicbaas_agent_status`          | Query session state, uptime, last activity       |
| `quicbaas_agent_close`           | Terminate an active session                      |
| `quicbaas_agent_list_sessions`   | List active sessions (filterable by backend)     |
| `quicbaas_agent_list_backends`   | List registered backends and capabilities        |
| `quicbaas_agent_decompose`       | Decompose a task into prioritized work items     |
| `quicbaas_agent_health`          | Check health of all active sessions              |
| `quicbaas_agent_broadcast`       | Publish a message to a coordination channel      |

### RAG Indexing (3 tools)

| Tool                             | Description                                     |
|----------------------------------|-------------------------------------------------|
| `quicbaas_rag_index_file`        | Index a single file into a vector namespace      |
| `quicbaas_rag_index_project`     | Batch index project files by glob pattern        |
| `quicbaas_rag_index_status`      | Query namespace vector count and dimensions      |

### Agent Cache (2 tools)

| Tool                             | Description                                     |
|----------------------------------|-------------------------------------------------|
| `quicbaas_agent_cache_put`       | Store analysis result in KV (project-scoped)     |
| `quicbaas_agent_cache_get`       | Retrieve cached result by project and key        |

## Session Persistence (AgentStore)

The `AgentStore` interface abstracts session persistence. The default implementation (`inMemoryAgentStore`) uses `sync.Map` for lock-free concurrent access. The interface supports pluggable backends (KV store, database) for durable session storage.

Operations: `PutSession`, `GetSession`, `DeleteSession`, `ListSessions`, `UpdateSessionStatus`, `UpdateSessionActivity`.

## Agent Health Monitoring

`AgentHealthMonitor` periodically checks all active sessions by querying their status through the `AgentService`. It uses `atomic.Bool` for lock-free running state management and reports per-session health via `HealthCheckReport`. Sessions in `active` or `idle` state are considered healthy; sessions in `error` or `closed` state are flagged. The `quicbaas_agent_health` MCP tool exposes on-demand health checks.

## Backend Types

Backends are classified by `BackendKind`: **agent** (ACP-compatible, persistent sessions, tool use) or **model_server** (stateless prompt/response). See [backends.md](backends.md) for details.

### ACP Backend (`acp`) -- Kind: `agent`

Communicates with ACP-compatible agents (Claude Code, Codex, OpenCode) via JSON-RPC 2.0 over stdio. Supports persistent sessions, tool use, file access, and streaming.

### Ollama Backend (`ollama`) -- Kind: `model_server`

Stateless HTTP backend for local LLM inference. No persistent sessions. Supports prompt/response only.

## Real-Time Event Streaming

The `SubscribeJobEvents` RPC streams real-time events from running orchestration jobs. The CLI auto-subscribes to the event stream when starting a job; if streaming is unavailable, it falls back to polling. Events include state transitions, task progress, and agent output. Jobs run server-side and survive after the CLI disconnects.

## Session Lifecycle

```
Spawn --> Active --> Idle --> Active --> ... --> Close
                      |
                      +--> Error
```

States: `active`, `idle`, `closed`, `error`.
