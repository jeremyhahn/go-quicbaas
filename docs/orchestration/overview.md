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
          | (45 tools)       |        | (5 tools)        |
          +--------+---------+        +--------+---------+
                   |                           |
          +--------v---------------------------v---------+
          |             Agent Service                     |
          |  (orchestration.AgentService interface)       |
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

This sets the `rag`, `vector`, `qrdb_vector`, and `mcp` build tags (`WITH_VECTOR=1` sets both `vector` and `qrdb_vector`). Without `WITH_RAG=1` and `WITH_VECTOR=1`, RAG indexing tools are excluded. Without `WITH_MCP=1`, the MCP tool layer is excluded.

## Package Layout

The orchestration layer lives in a single package, `pkg/orchestration`. There is
no `pkg/acp`: the ACP backend and its siblings are files within
`pkg/orchestration`, alongside the service, the stores and the policy axes.

```
pkg/orchestration/
    orchestrator.go            AgentService implementation
    orchestrator_types.go      AgentService interface, request/response types
    orchestrator_resume.go     Session reconstitution and trust re-resolution
    errors.go                  Typed errors and sentinels

    backend.go                 AgentBackend interface
    acp_backend.go             ACP backend (Claude Code, Codex, OpenCode via JSON-RPC/stdio)
    remote_acp_backend.go      Daemon-dispatched ACP backend
    ollama_backend.go          Ollama stateless backend
    codex_exec_backend.go      Codex exec backend
    callbacks.go               Filesystem callback handler with path validation
    types.go                   ACP JSON-RPC message types

    session_store.go           AgentStore interface, SessionRecord, KV-backed store
    session_log.go             Durable session event log (transcript source of truth)
    health.go                  AgentHealthMonitor, HealthCheckReport, SessionHealthResult
    project_store.go           Registered project catalog
    task_store.go              Task tracking

    control_loop.go            Job control loop
    dag_scheduler.go           Task dependency scheduling
    workflow_types.go          Workflow definitions

    account_*.go               Pooled provider accounts (resolution, rotation, limits)

    sandbox_policy.go          Sandbox axis: containment
    sandbox_network.go         Sandbox axis: network isolation
    mount_policy.go            Sandbox axis: bind-mount allowlist
    trust_policy.go            Trust axis: TrustLevel / TrustPolicy / resolution
    container_trust.go         Tier 0: digest pinning, config digest
    container_attestation.go   Tiers 1-2: host quote, measured launch
    container_runtime.go       Exec-backed container runtime
    container_runtime_digest.go  Digest resolution and post-launch verification

pkg/api/mcp/
    orchestrator_tools.go        27 orchestration and fleet MCP tools
    account_tools.go             9 pooled-account MCP tools
    project_tools.go             5 project MCP tools
    agent_admin_tools.go         4 project/agent grant MCP tools
    orchestrator_cache_tools.go  2 agent cache MCP tools (KV-backed)
    rag_index_tools.go           3 RAG indexing MCP tools (vector-backed)

sdk/go/orchestration/
    types.go                   Type-alias re-export of the container-trust surface
```

## MCP Tools (50 total)

Fifty on the default build. The three RAG indexing tools are gated behind
`mcp && rag`, so `WITH_RAG=0` leaves 47 — which is also the count the
[README's](../../README.md#mcp-tools-ai-agents) Orchestration category reports,
since it buckets RAG indexing under RAG rather than orchestration.

The tables below name the tools grouped by the file that registers them. Full
request and response schemas for the orchestrator, RAG indexing and cache
families are in [mcp-tools.md](mcp-tools.md); pooled-account tools are covered
in [accounts.md](accounts.md).

### Agent and Session Management (27 tools)

Registered by `pkg/api/mcp/orchestrator_tools.go`.

| Tool                                    | Description                                      |
|-----------------------------------------|--------------------------------------------------|
| `quicbaas_orchestrator_discover`        | Scan a directory tree for agent-enabled projects |
| `quicbaas_orchestrator_register`        | Register a project for orchestration             |
| `quicbaas_orchestrator_list`            | List registered projects (paginated)             |
| `quicbaas_orchestrator_spawn`           | Create a persistent session on a backend         |
| `quicbaas_orchestrator_prompt`          | Send a prompt to a session or backend            |
| `quicbaas_orchestrator_session_history` | Page through a session's durable event transcript |
| `quicbaas_orchestrator_status`          | Query session state, uptime, last activity       |
| `quicbaas_orchestrator_close`           | Terminate an active session                      |
| `quicbaas_orchestrator_rotate_account`  | Move a session onto a different pooled account   |
| `quicbaas_orchestrator_list_sessions`   | List active sessions (filterable by backend)     |
| `quicbaas_orchestrator_list_backends`   | List registered backends and capabilities        |
| `quicbaas_orchestrator_decompose`       | Decompose a task into prioritized work items     |
| `quicbaas_orchestrator_health`          | Check health of all active sessions              |
| `quicbaas_orchestrator_broadcast`       | Publish a message to a coordination channel      |
| `quicbaas_orchestrator_inference`       | Stateless inference against a fleet-resident daemon |
| `quicbaas_orchestrator_start_job`       | Start an autonomous, iterating orchestration job |
| `quicbaas_orchestrator_get_job_status`  | Query a job's iteration count and task progress  |
| `quicbaas_orchestrator_cancel_job`      | Cancel a running orchestration job               |
| `quicbaas_orchestrator_list_jobs`       | List jobs for the tenant, newest first           |
| `quicbaas_orchestrator_task_create`     | Create a task, optionally dispatching it at once |
| `quicbaas_task_get_artifact`            | Retrieve a named output artifact from a task     |
| `quicbaas_task_list_artifacts`          | List a task's output artifact metadata           |
| `quicbaas_agent_fleet_register`         | Register an agent with the fleet                 |
| `quicbaas_agent_fleet_list`             | List fleet agents for a tenant                   |
| `quicbaas_agent_fleet_status`           | Get one fleet agent's status                     |
| `quicbaas_agent_fleet_remove`           | Remove an agent from the fleet registry          |
| `quicbaas_agent_fleet_heartbeat`        | Update an agent's last-seen timestamp            |

### Pooled Provider Accounts (9 tools)

Registered by `pkg/api/mcp/account_tools.go`. See [accounts.md](accounts.md).

| Tool                                    | Description                                      |
|-----------------------------------------|--------------------------------------------------|
| `quicbaas_agent_account_create`         | Create a pooled provider account                 |
| `quicbaas_agent_account_get`            | Read account metadata (never credential material) |
| `quicbaas_agent_account_list`           | List accounts, filterable by provider and project |
| `quicbaas_agent_account_update`         | Update non-secret metadata                       |
| `quicbaas_agent_account_delete`         | Delete an account and its credential envelope    |
| `quicbaas_agent_account_rotate`         | Replace the stored credential envelope           |
| `quicbaas_agent_account_revoke`         | Exclude an account from every selection strategy |
| `quicbaas_agent_account_set_default`    | Mark an account as the tenant/project default    |
| `quicbaas_agent_account_usage`          | Report cumulative token usage for an account     |

### Projects and Agent Grants (9 tools)

Registered by `pkg/api/mcp/project_tools.go` and `agent_admin_tools.go`.

| Tool                    | Description                                              |
|-------------------------|----------------------------------------------------------|
| `project_create`        | Create a project within a tenant                         |
| `project_get`           | Retrieve a project by ID                                 |
| `project_list`          | List a tenant's projects                                 |
| `project_update`        | Update a project; only supplied fields change            |
| `project_delete`        | Delete a project by ID                                   |
| `project_grant_agent`   | Grant an agent a project role (lead/worker/observer/denied) |
| `project_revoke_agent`  | Set an agent's project role to denied                    |
| `agent_disable`         | Disable an agent globally; blocks claims and auth        |
| `agent_enable`          | Re-activate a disabled agent                             |

### RAG Indexing (3 tools)

| Tool                             | Description                                     |
|----------------------------------|-------------------------------------------------|
| `quicbaas_rag_index_file`        | Index a single file into a vector namespace      |
| `quicbaas_rag_index_project`     | Batch index project files by glob pattern        |
| `quicbaas_rag_index_status`      | Query namespace vector count and dimensions      |

### Agent Cache (2 tools)

| Tool                             | Description                                     |
|----------------------------------|-------------------------------------------------|
| `quicbaas_orchestrator_cache_put`       | Store analysis result in KV (project-scoped)     |
| `quicbaas_orchestrator_cache_get`       | Retrieve cached result by project and key        |

## Session Persistence (AgentStore)

The `AgentStore` interface abstracts session persistence. There is exactly one implementation, `sessionStore`, and it keeps no copy of the data it serves: every method reads from and writes to an injected `interfaces.KVStore`. Durability, replication and encryption at rest are therefore properties of the injected store alone. `NewAgentStore(kv)` takes any KV; `NewInMemoryAgentStore()` is a memory-backed binding of that same code for tests and embedded use, not a second implementation.

Every method is tenant-explicit, so one tenant can neither read nor mutate another's records. `ListAllSessions` is the single deliberate exception: it is the boot-time recovery scan, which by definition runs before any tenant context exists.

Operations: `PutSession`, `GetSession`, `DeleteSession`, `ListSessions`, `ListAllSessions`, `UpdateSessionStatus`, `UpdateSessionActivity`.

The transcript is not stored on the record. It lives in the session event log, which is the single source of truth for conversation content; the record only points at it.

## Agent Health Monitoring

`AgentHealthMonitor` periodically checks all active sessions by querying their status through the `AgentService`. It uses `atomic.Bool` for lock-free running state management and reports per-session health via `HealthCheckReport`. Sessions in `active`, `idle` or `resumable` state are considered healthy (`isHealthySessionState`, `health.go`); sessions in `error` or `closed` state are flagged. The `quicbaas_orchestrator_health` MCP tool exposes on-demand health checks.

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
             |        |
             |        +--> Error
             |
             +--> Resumable --> Active   (daemon restart; process recreated
                                          by the next prompt)
```

States: `active`, `idle`, `resumable`, `closed`, `error` (`SessionState`,
`pkg/orchestration/backend.go`).

`resumable` is the durable-record state: the session's record and transcript
survived a daemon restart but its agent process did not. The session stays
addressable, and the next prompt recreates the process and restores the
conversation before the turn runs.

## Execution Policy

A spawn is admitted along two orthogonal axes, both resolved at the same
pre-side-effect chokepoint and both deny-by-default:

| Axis | Question | Reference |
|------|----------|-----------|
| Sandbox | Is the agent isolated from the host? | [Remote Agent Daemon](../agents/daemon.md#sandbox-policy-reference) |
| Trust | Is the agent the code we approved, and can the host prove it? | [Container Trust](container-trust.md) |

The policy that admits a spawn on either axis is summarized in
[security.md](security.md).

## See Also

- [Container Trust](container-trust.md) — digest pinning, host attestation, measured launch.
- [Agent Orchestration Security](security.md) — multi-tenancy, ACLs, sandbox and trust policy.
- [MCP Tool Reference](mcp-tools.md) — request and response schemas.
- [Backends](backends.md) — backend kinds and capabilities.
