# Plan: QuicBaaS LLM Orchestration Fabric + E2E Tests

## Context

Claude Code has built-in agent teams (experimental, local-only) with task lists, mailboxes, and teammate coordination. However, teams are **strictly local** - file-based storage at `~/.claude/teams/` and `~/.claude/tasks/`, subprocesses on the same machine. They cannot span physical hosts.

QuicBaaS fills this gap as the **distributed coordination layer** by integrating with Claude Code through three official extension points:

1. **Hooks** (`TaskCreated`, `TaskCompleted`, `TeammateIdle`) - HTTP hooks POST task events to QuicBaaS
2. **Channels** - QuicBaaS runs as an MCP channel server, pushing events INTO Claude Code sessions
3. **MCP Tools** - Claude Code agents use QuicBaaS tools for RAG, KV, secrets, messaging, vector search

## Naming Convention: Disambiguating "Agent"

"Agent" is overloaded in QuicBaaS. We distinguish:

| Term | Meaning | Package |
|------|---------|---------|
| **LLM Agent** | An AI model session (Claude Code, Ollama) managed by the orchestrator | `pkg/orchestration/` (NEW) |
| **Orchestrator** | The QuicBaaS service that coordinates LLM agents | `pkg/orchestration/` (NEW) |
| **ACP Backend** | Protocol layer for communicating with LLM agent subprocesses | `pkg/acp/` (existing) |
| **QuicBaaS Agent** | A QuicBaaS node acting as a service participant (unrelated to LLM) | Existing infra |
| **MCP Tools** | Renamed from `quicbaas_agent_*` to `quicbaas_orchestrator_*` | `pkg/api/mcp/` |

### Package Restructuring

**Everything** related to LLM agent orchestration moves to `pkg/orchestration/`. This includes the ACP protocol layer, backends, transport, callbacks - all of it.

#### What moves to `pkg/orchestration/`

| Current Location | New Location | Description |
|-----------------|--------------|-------------|
| `pkg/acp/backend.go` | `pkg/orchestration/backend.go` | `AgentBackend` interface |
| `pkg/acp/acp_backend.go` | `pkg/orchestration/acp_backend.go` | ACP (Claude Code/Codex) backend |
| `pkg/acp/ollama_backend.go` | `pkg/orchestration/ollama_backend.go` | Ollama stateless backend |
| `pkg/acp/transport.go` | `pkg/orchestration/transport.go` | StdioTransport (JSON-RPC over stdio) |
| `pkg/acp/callbacks.go` | `pkg/orchestration/callbacks.go` | File system callback handler |
| `pkg/acp/types.go` | `pkg/orchestration/types.go` | ACP JSON-RPC message types |
| `pkg/acp/errors.go` | `pkg/orchestration/errors.go` | ACP typed errors (merge with service errors) |
| `pkg/service/agent_service.go` | `pkg/orchestration/orchestrator.go` | OrchestratorService |
| `pkg/service/agent_types.go` | `pkg/orchestration/orchestrator_types.go` | Service request/response types |
| `pkg/service/agent_store.go` | `pkg/orchestration/session_store.go` | Session persistence |
| `pkg/service/agent_health.go` | `pkg/orchestration/health.go` | Health monitoring |
| `pkg/api/mcp/agent_tools.go` | `pkg/api/mcp/orchestrator_tools.go` | MCP tools (renamed) |
| `pkg/api/mcp/agent_cache_tools.go` | `pkg/api/mcp/orchestrator_cache_tools.go` | Cache MCP tools (renamed) |
| All corresponding `*_test.go` files | Move with their source | Tests follow source |

#### New files in `pkg/orchestration/`

| File | Description |
|------|-------------|
| `task_store.go` | Distributed task store (QuicBaaS KV backed) |
| `task_types.go` | Task data types |
| `channel_server.go` | MCP channel server for Claude Code |
| `container_runtime.go` | Simple OCI container exec (docker/podman) |
| `container_types.go` | ContainerSpec, ContainerRuntime interface |

#### What gets deleted

| File | Reason |
|------|--------|
| `pkg/acp/` (entire directory) | Moved to `pkg/orchestration/` |
| `pkg/service/agent_service.go` | Moved to `pkg/orchestration/orchestrator.go` |
| `pkg/service/agent_types.go` | Moved to `pkg/orchestration/orchestrator_types.go` |
| `pkg/service/agent_store.go` | Moved to `pkg/orchestration/session_store.go` |
| `pkg/service/agent_health.go` | Moved to `pkg/orchestration/health.go` |

#### What stays in `pkg/service/`

The `QuicBaaS` service facade (`pkg/service/quicbaas.go`) keeps a reference to the orchestration package but delegates all LLM operations to `pkg/orchestration/`. The facade methods that currently call `AgentService` will call `orchestration.OrchestratorService` instead.

#### MCP tool renaming

| Old name | Registered name | Status |
|----------|-----------------|--------|
| `quicbaas_agent_discover` | `quicbaas_orchestrator_discover` | Renamed |
| `quicbaas_agent_spawn` | `quicbaas_orchestrator_spawn` | Renamed |
| `quicbaas_agent_prompt` | `quicbaas_orchestrator_prompt` | Renamed |
| `quicbaas_agent_*` (all 12) | `quicbaas_orchestrator_*` | Renamed |
| `quicbaas_agent_cache_*` (2) | `quicbaas_orchestrator_cache_*` | Renamed |

The rename is complete: the old `quicbaas_agent_*` names are NOT registered and
carry no alias. The stack is pre-v1 with a no-cruft policy, and the old names
were never reachable under `RequireAuthz` (they had no qrdb permission mapping,
so the default-deny gate filtered them out of `tools/list`). The fleet surface
keeps its `quicbaas_agent_fleet_*` names.

#### CLI subcommand

`quicbaas orchestrator` (not `quicbaas agent`) to match the package name and avoid ambiguity.

## Deployment Modes

The architecture works at every scale - from a single laptop to a multi-datacenter Raft cluster. Agents do NOT need to run on QuicBaaS servers.

### Mode 1: Standalone (Single Machine, No Raft)

A single QuicBaaS instance (PebbleDB backend) coordinates agents on the same machine. No cluster needed. This is the `quicbaasd start` mode.

```
┌─ Laptop / Workstation ──────────────────────────────────┐
│                                                          │
│  Claude Code (lead + workers)                            │
│       │                                                  │
│       ├── Hooks → HTTP → QuicBaaS (standalone, PebbleDB) │
│       ├── MCP Tools → QuicBaaS (RAG, KV, secrets)        │
│       └── Channel ← QuicBaaS pushes events               │
│                                                          │
│  Ollama (local GPU or CPU)                               │
└──────────────────────────────────────────────────────────┘
```

**Use case**: Solo developer, local agent teams, no network required.

### Mode 2: Remote Agents (Agents NOT on QuicBaaS servers)

Agents run on machines that do NOT have QuicBaaS installed. They connect to a remote QuicBaaS server over gRPC/QUIC/REST. The agent machine only needs Claude Code and network access.

```
┌─ Mac with Secure Enclave ───┐     ┌─ x86 TPM Machine ──────────┐
│                              │     │                              │
│  Claude Code Agent           │     │  Claude Code Agent           │
│       │                      │     │       │                      │
│       ├── Hooks → HTTP ──────┤     │       ├── Hooks → HTTP ──────┤
│       ├── MCP → gRPC/QUIC ──┤     │       ├── MCP → gRPC/QUIC ──┤
│       └── Channel ← ────────┤     │       └── Channel ← ────────┤
│                              │     │                              │
│  (no QuicBaaS installed)     │     │  (no QuicBaaS installed)     │
└──────────────┬───────────────┘     └──────────────┬───────────────┘
               │ network                             │ network
               ▼                                     ▼
┌──────────────────────────────────────────────────────────────────┐
│  QuicBaaS Server (standalone OR cluster)                         │
│  KV, Messaging, RAG, Secrets, Task Store                         │
│  Ollama (GPU-accelerated inference)                              │
└──────────────────────────────────────────────────────────────────┘
```

**Use case**: Heterogeneous hardware (Mac + x86), specialized security enclaves, edge devices. Agents run anywhere with network access.

**How hooks work remotely**: Claude Code hooks support `type: http` with a URL. The URL points to the remote QuicBaaS server instead of localhost:
```json
{
  "hooks": {
    "TaskCreated": [{
      "hooks": [{
        "type": "http",
        "url": "https://quicbaas-server.internal:9090/api/v1/hooks/task-created"
      }]
    }]
  }
}
```

**How channels work remotely**: The QuicBaaS channel server runs as a local MCP process on the agent machine, but connects to the remote QuicBaaS server via gRPC/QUIC for event sourcing. The channel server is a thin proxy:
```
Claude Code ←stdio→ QuicBaaS Channel Proxy ←gRPC→ Remote QuicBaaS Server
```

### Mode 3: Multi-Node Raft Cluster

Full distributed mode with Raft consensus. Multiple QuicBaaS nodes, agents on any machine.

```
  Agent Machine 1          Agent Machine 2          Agent Machine N
  (any hardware)           (any hardware)           (any hardware)
  ┌────────────┐           ┌────────────┐           ┌────────────┐
  │Claude Code │           │Claude Code │           │Claude Code │
  │  Hooks→HTTP│           │  Hooks→HTTP│           │  Hooks→HTTP│
  │  MCP→gRPC  │           │  MCP→QUIC  │           │  MCP→REST  │
  │  Channel←  │           │  Channel←  │           │  Channel←  │
  └─────┬──────┘           └─────┬──────┘           └─────┬──────┘
        │                        │                        │
        ▼                        ▼                        ▼
  ┌──────────┐  Raft  ┌──────────┐  Raft  ┌──────────┐
  │QuicBaaS  │◄──────►│QuicBaaS  │◄──────►│QuicBaaS  │
  │Node 1    │        │Node 2    │        │Node 3    │
  └──────────┘        └──────────┘        └──────────┘
```

**Use case**: Production, HA, multi-datacenter, large agent teams across many machines.

### Mode 4: Remote Daemon with Container Sandboxing (DEFAULT)

A long-lived daemon (`quicbaas agent serve`) runs on remote GPU hosts and exposes an AgentRuntime gRPC service. The central orchestrator calls this service to spawn sandboxed (containerized) sessions. This is the **sandbox-first** default.

```
  CENTRAL QUICBAAS               REMOTE GPU HOST (198.51.100.91)
┌────────────────────────┐     ┌──────────────────────────────────────┐
│                        │     │                                       │
│ BackendDispatcher      │     │  quicbaas agent serve                 │
│  │                     │     │  (daemon process)                     │
│  ├─ LocalACPBackend    │     │  ┌─────────────────────────────────┐ │
│  │  (local spawn)      │     │  │ AgentRuntime gRPC Service       │ │
│  │                     │     │  │ (port 9091)                     │ │
│  ├─ RemoteACPBackend   │────────→│ ┌─────────────────────────────┐│ │
│  │  (call daemon)      │gRPC+QUIC││ Runtime (reuses pkg/orch):    ││ │
│  │                     │     │  │ │  - ACPBackend                ││ │
│  └─ OllamaBackend      │     │  │ │  - OllamaBackend             ││ │
│     (Ollama API)       │     │  │ │  - ContainerRuntime          ││ │
│                        │     │  │ │  - SandboxResolver           ││ │
│                        │     │  │ └─────────────────────────────┘│ │
│                        │     │  │ ┌─────────────────────────────┐│ │
│                        │     │  │ │ Enrolled identity (OAuth2)  ││ │
│                        │     │  │ │ JWT token scoped:           ││ │
│                        │     │  │ │  - agent:runtime:spawn      ││ │
│                        │     │  │ │  - agent:runtime:prompt     ││ │
│                        │     │  │ │  - agent:runtime:status     ││ │
│                        │     │  │ └─────────────────────────────┘│ │
│                        │     │  └─────────────────────────────────┘ │
│                        │     │          ↓                           │
│                        │     │  Docker daemon (host socket mount)   │
│                        │     │  Ollama (http://localhost:11434)    │
└────────────────────────┘     └──────────────────────────────────────┘
```

**Key features:**
- Daemon enrolls with central using OAuth2 Client Credentials (one-shot enrollment token)
- Heartbeats into fleet registry; central can spawn sessions remotely
- Sandbox policy: **required** (default; container-only) — raw exec forbidden unless explicitly allowed
- Reuses existing `ACPBackend`, `OllamaBackend`, `ContainerRuntime` (no parallel spawn logic)
- Session state encrypted with tenant DEK; audit logging per RPC
- Graceful shutdown drains in-flight sessions with configurable timeout

**Operators guide**: See `docs/agents/daemon.md` for deployment, enrollment, troubleshooting, and systemd integration.

**Use case**: Production GPU hosts, compliance-sensitive tenants (FedRAMP/HIPAA), central orchestrator needs direct control of session lifecycle.

### Mode 5: Remote Daemon with Raw Process Execution (OPT-IN)

Same architecture as Mode 4, but with sandbox policy `opt-in` or `preferred`, allowing clients to explicitly request raw process execution (no container). This is less secure but useful for legacy systems or trusted development environments.

```
  CENTRAL QUICBAAS               REMOTE HOST
┌────────────────────────┐     ┌──────────────────────────────────────┐
│                        │     │                                       │
│ BackendDispatcher      │     │  quicbaas agent serve                 │
│  │                     │     │  --sandbox-policy=opt-in              │
│  ├─ LocalACPBackend    │     │  (daemon process)                     │
│  │                     │     │  ┌─────────────────────────────────┐ │
│  ├─ RemoteACPBackend   │────────→│ AgentRuntime gRPC Service       │ │
│  │  (call daemon)      │     │  │ ┌─────────────────────────────┐│ │
│  │                     │     │  │ │ SandboxResolver:            ││ │
│  └─ OllamaBackend      │     │  │ │ - DEFAULT → raw exec        ││ │
│                        │     │  │ │ - REQUIRED → container ✓    ││ │
│                        │     │  │ │ - NONE → raw exec ✓         ││ │
│                        │     │  │ └─────────────────────────────┘│ │
│                        │     │  │ ┌─────────────────────────────┐│ │
│                        │     │  │ │ Execution paths:            ││ │
│                        │     │  │ │  - exec.Command() [raw]     ││ │
│                        │     │  │ │  - Docker [sandboxed]       ││ │
│                        │     │  │ └─────────────────────────────┘│ │
│                        │     │  └─────────────────────────────────┘ │
│                        │     │          ↓                           │
│                        │     │  stdio transport (local process)      │
│                        │     │  OR docker socket (containerized)     │
└────────────────────────┘     └──────────────────────────────────────┘
```

**Policy override by tenant:** Even if daemon is set to `opt-in`, the central can force `required` policy per tenant during enrollment, ensuring FedRAMP/HIPAA tenants always run in containers regardless of daemon setting.

**Security note:** Raw exec is less secure. Use only for:
- Development/testing environments
- Trusted code (internal teams, proprietary software)
- Legacy systems that require direct process access

**Use case**: Development environments, internal automation, mixed workloads where some sessions need sandbox and others don't.

### Backend-Agnostic Orchestration

The orchestrator is NOT married to Claude Code. It works with ANY backend implementing the `AgentBackend` interface (`pkg/acp/backend.go`):

| Backend | Protocol | Sessions | Tool Use | Extra Features |
|---------|----------|----------|----------|----------------|
| **ACP** (Claude Code, Codex) | JSON-RPC over stdio | Persistent | Yes | Hooks, Channels, Teams |
| **Ollama** | HTTP REST | Stateless | No | GPU inference |
| **Custom** | Implement `AgentBackend` | Configurable | Configurable | User-defined |

Claude Code-specific features (hooks, channels, permission relay) are additive enhancements that only activate when using an ACP backend. The core orchestration (task store, messaging coordination, RAG sharing, session management) works identically across ALL backends.

E2E tests validate **Claude Code, Ollama, AND Codex/OpenCode backends** to prove backend-agnostic operation.

### Containerized Agent Runtimes (Sandboxed Execution)

Inspired by [cs50victor/claude-code-teams-mcp](https://github.com/cs50victor/claude-code-teams-mcp) which reverse-engineered Claude Code teams and supports both Claude Code and OpenCode, we create lightweight Docker containers for each agent type. These serve dual purposes: **secure production runtime** AND **integration test infrastructure**.

```
test/integration/containers/
├── claude-code/
│   ├── Dockerfile          # Alpine + Node.js + claude CLI + QuicBaaS MCP config
│   └── entrypoint.sh       # Configure Ollama backend, start agent
├── codex/
│   ├── Dockerfile          # Alpine + Node.js + codex CLI + QuicBaaS MCP config
│   └── entrypoint.sh
├── opencode/
│   ├── Dockerfile          # Alpine + Go + opencode binary + QuicBaaS MCP config
│   └── entrypoint.sh
└── docker-compose.agents.yml  # All agent containers + Ollama + QuicBaaS
```

**Container features:**
- Minimal Alpine base (small attack surface)
- Pre-configured `.mcp.json` pointing to QuicBaaS MCP server
- Pre-configured hooks (TaskCreated, TaskCompleted → QuicBaaS)
- Pre-configured channel server (QuicBaaS → agent)
- Auth credentials injected via environment/secrets
- GPU passthrough for Ollama inference
- Read-only filesystem except designated work directories
- Network restricted to QuicBaaS + Ollama only (no internet in production)
- Each agent runs as non-root user

**Integration test scenario:**
```
┌──────────────────────────────────────────────────────────────────┐
│ Docker Compose Test Environment                                   │
│                                                                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │ Claude Code  │  │   Codex     │  │  OpenCode   │              │
│  │ Container    │  │ Container   │  │ Container   │              │
│  │ (worker)     │  │ (worker)    │  │ (worker)    │              │
│  └──────┬───────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                  │                 │                     │
│         └──────────┬───────┴─────────────────┘                    │
│                    ▼                                               │
│  ┌──────────────────────────────────────────┐                    │
│  │ QuicBaaS (orchestrator + infra)          │                    │
│  │ Task Store, Messaging, RAG, Auth         │                    │
│  └──────────────┬───────────────────────────┘                    │
│                 ▼                                                  │
│  ┌──────────────────────────────────────────┐                    │
│  │ Ollama (GPU-accelerated inference)       │                    │
│  └──────────────────────────────────────────┘                    │
└──────────────────────────────────────────────────────────────────┘

Test: Each agent type takes a turn as team lead, delegates work to
the other two, and proves cross-backend orchestration works.
```

**Key insight from claude-code-teams-mcp**: The spawning mechanism uses tmux for local agents and HTTP API for OpenCode. We can adapt this:
- **Local agents**: tmux spawning (same as claude-code-teams-mcp)
- **Containerized agents**: Docker exec / API spawning
- **Remote agents**: QuicBaaS gRPC/QUIC session management

The containers are ALSO usable in production for sandboxed agent execution with strict security controls.

### Container Runtime: Simple OCI Exec (Docker/Podman)

go-quicbaas does NOT depend on any container orchestration library. It shells out to whatever OCI-compliant runtime is installed (docker, podman, etc.). go-trusted-platform can later extend this with its advanced `ContainerOrchestrator`, TPM attestation, and OPA policy engine via the `ContainerRuntime` interface.

**Interface designed for extensibility by go-trusted-platform:**
```go
// ContainerRuntime abstracts OCI container operations.
// Default implementation: exec.Command("docker"/"podman").
// go-trusted-platform can provide an advanced implementation with
// TPM attestation, OPA policies, and podman rootless support.
type ContainerRuntime interface {
    Run(ctx context.Context, spec *ContainerSpec) (containerID string, err error)
    Stop(ctx context.Context, containerID string) error
    Remove(ctx context.Context, containerID string) error
    Logs(ctx context.Context, containerID string) (io.ReadCloser, error)
    IsAvailable() bool  // checks if docker/podman is installed
}

// ContainerSpec - simple, no external deps
type ContainerSpec struct {
    Name       string
    Image      string
    Env        map[string]string
    Mounts     []Mount       // host:container:ro/rw
    Ports      []string      // host:container
    WorkDir    string
    Command    []string
    Labels     map[string]string
    Remove     bool          // --rm flag
}
```

**Default implementation**: `exec.CommandContext(ctx, "docker", "run", ...)` - no Go library dependencies, works anywhere docker or podman is installed. The runtime command is configurable (default: auto-detect docker → podman).

**Secrets Flow (Closed Loop):**
```
1. Admin stores agent secrets in QuicBaaS Secrets Manager:
   quicbaas secrets store "orchestrator/claude-code/anthropic-base-url" "http://ollama:11434"
   quicbaas secrets store "orchestrator/claude-code/auth-token" "ollama"
   quicbaas secrets store "orchestrator/openai-api-key" "sk-..."

2. Orchestrator receives SpawnSession request
   → Resolves secrets for the agent backend from QuicBaaS Secrets Manager
   → Builds ContainerSpec:
     - Image: quicbaas/claude-code-agent:latest
     - Env: resolved secrets (ANTHROPIC_BASE_URL, auth tokens)
     - Mounts: work dir (rw), .mcp.json (ro), .claude/settings.json (ro)
     - Labels: tenant-id, session-id, backend-type
   → Calls ContainerRuntime.Run() (shells out to docker/podman)

3. Container starts, agent launches
   → Reads .mcp.json pointing to QuicBaaS MCP server (host networking or NAT)
   → Reads hook config pointing to QuicBaaS
   → Authenticates to QuicBaaS via OAuth2 (credentials from env)
   → Ready to receive tasks

4. All subsequent operations go through QuicBaaS MCP tools
   → Agent never sees raw secrets beyond initial env injection
```

**Non-Sandboxed Mode:**
When `sandboxed: false`, the orchestrator spawns the agent as a local subprocess (existing ACP/StdioTransport path) instead of a container. Secrets injected via process environment. Works on machines without container runtime.

## How Every Piece Communicates (Detailed)

### Integration Architecture

```
HOST A (vm-host-1)                           HOST B (vm-host-3)
┌──────────────────────────────┐               ┌──────────────────────────────┐
│ Claude Code Session          │               │ Claude Code Session          │
│ (Lead Agent + Workers)       │               │ (Workers)                    │
│                              │               │                              │
│ ┌──────────────────────────┐ │               │ ┌──────────────────────────┐ │
│ │ .claude/settings.json    │ │               │ │ .claude/settings.json    │ │
│ │ hooks:                   │ │               │ │ hooks:                   │ │
│ │   TaskCreated:           │ │               │ │   TaskCreated:           │ │
│ │     type: http           │ │               │ │     type: http           │ │
│ │     url: localhost:9090  │ │               │ │     url: localhost:9090  │ │
│ │   TaskCompleted:         │ │               │ │   TaskCompleted:         │ │
│ │     type: http           │ │               │ │     type: http           │ │
│ │     url: localhost:9090  │ │               │ │     url: localhost:9090  │ │
│ │   TeammateIdle:          │ │               │ │   TeammateIdle:          │ │
│ │     type: http           │ │               │ │     type: http           │ │
│ │     url: localhost:9090  │ │               │ │     url: localhost:9090  │ │
│ └──────────────────────────┘ │               │ └──────────────────────────┘ │
│                              │               │                              │
│ ┌──────────────────────────┐ │               │ ┌──────────────────────────┐ │
│ │ QuicBaaS MCP Server      │ │               │ │ QuicBaaS MCP Server      │ │
│ │ (.mcp.json config)       │ │               │ │ (.mcp.json config)       │ │
│ │                          │ │               │ │                          │ │
│ │ Tools available:         │ │               │ │ Tools available:         │ │
│ │  quicbaas_orchestrator_* │ │               │ │  quicbaas_orchestrator_* │ │
│ │  quicbaas_rag_*          │ │               │ │  quicbaas_rag_*          │ │
│ │  quicbaas data ops       │ │               │ │  quicbaas data ops       │ │
│ │  quicbaas secrets        │ │               │ │  quicbaas secrets        │ │
│ │  quicbaas kms            │ │               │ │  quicbaas kms            │ │
│ └────────────┬─────────────┘ │               │ └────────────┬─────────────┘ │
│              │ MCP stdio     │               │              │ MCP stdio     │
│              ▼               │               │              ▼               │
│ ┌──────────────────────────┐ │               │ ┌──────────────────────────┐ │
│ │ QuicBaaS Channel Server  │ │               │ │ QuicBaaS Channel Server  │ │
│ │ (claude/channel MCP)     │ │               │ │ (claude/channel MCP)     │ │
│ │                          │ │               │ │                          │ │
│ │ Pushes:                  │ │               │ │ Pushes:                  │ │
│ │ - Cross-host task events │ │               │ │ - Cross-host task events │ │
│ │ - Agent messages         │ │               │ │ - Agent messages         │ │
│ │ - RAG results            │ │               │ │ - RAG results            │ │
│ │ - Permission relay       │ │               │ │ - Permission relay       │ │
│ └────────────┬─────────────┘ │               │ └────────────┬─────────────┘ │
└──────────────┼───────────────┘               └──────────────┼───────────────┘
               │                                               │
               ▼                                               ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                        QuicBaaS Distributed Services                        │
│                        (Raft-replicated across all nodes)                   │
│                                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────┐  ┌──────────┐            │
│  │ KV Store    │  │ Messaging   │  │ Vector    │  │ Secrets  │            │
│  │ (Tasks,     │  │ (Pub/Sub    │  │ (RAG      │  │ (API     │            │
│  │  Config,    │  │  Channels,  │  │  Pipeline,│  │  Keys,   │            │
│  │  State)     │  │  Broadcast) │  │  Search)  │  │  Certs)  │            │
│  └─────────────┘  └─────────────┘  └───────────┘  └──────────┘            │
│                                                                              │
│  Transport: gRPC + QUIC + REST + MCP + Unix                                │
│  Replication: Multi-Raft consensus                                          │
│  GPU: Ollama (NVIDIA RTX 6000 Pro) for local LLM inference                 │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Data Flow: Task Created on Host A → Executed on Host B

```
Step 1: Claude Code Lead Agent on Host A creates a task
        └─► Claude Code calls TaskCreate tool
            └─► TaskCreated hook fires (configured in .claude/settings.json)
                └─► HTTP POST to QuicBaaS at localhost:9090/hooks/task-created
                    Body: {"task_id":"t1", "task_subject":"Write hello world", "team_name":"dev"}

Step 2: QuicBaaS receives the hook event
        └─► Stores task in KV: key="agent:tasks:dev:t1" value={...}
        └─► Publishes to messaging channel: "agent:tasks:dev"
        └─► Raft replicates KV write + message to Host B

Step 3: QuicBaaS on Host B receives the replicated data
        └─► Channel server detects new task via messaging subscription
        └─► Pushes into Claude Code session on Host B:
            notification: {
              method: "notifications/claude/channel",
              params: {
                content: "New task available: Write hello world",
                meta: { task_id: "t1", team: "dev", type: "task_created" }
              }
            }

Step 4: Claude Code Worker on Host B receives the channel event
        └─► Sees <channel source="quicbaas" task_id="t1" type="task_created">
        └─► Uses QuicBaaS MCP tool to claim the task:
            quicbaas_agent_task_claim(task_id="t1", agent_id="worker-b")
        └─► Executes the work (writes code, runs tests)
        └─► Marks task complete via TaskCreate tool

Step 5: TaskCompleted hook fires on Host B
        └─► HTTP POST to QuicBaaS at localhost:9090/hooks/task-completed
        └─► QuicBaaS updates KV, publishes to messaging channel
        └─► Raft replicates to Host A

Step 6: QuicBaaS Channel Server on Host A pushes completion event
        └─► Lead Agent sees task completed, continues orchestration
```

### Data Flow: Shared RAG Across Hosts

```
Step 1: Agent on Host A indexes codebase
        └─► Uses MCP tool: quicbaas_rag_index_project(path="/project", namespace="codebase")
        └─► QuicBaaS chunks, embeds (via Ollama GPU), stores vectors
        └─► Vectors replicated to all nodes via Raft

Step 2: Agent on Host B queries the RAG pipeline
        └─► Uses MCP tool: quicbaas_rag_query(query="auth implementation", namespace="codebase")
        └─► QuicBaaS searches local vector store (same data via Raft)
        └─► Returns ranked results to the agent
```

### Data Flow: Direct Agent-to-Agent Messaging

```
Step 1: Agent A broadcasts via MCP tool
        └─► quicbaas_orchestrator_broadcast(channel="agent:review-request", message="Review PR #42")
        └─► QuicBaaS publishes to messaging channel
        └─► Replicated to all nodes

Step 2: QuicBaaS Channel Server on Host B pushes to Claude Code
        └─► Agent B receives: <channel source="quicbaas" type="broadcast">Review PR #42</channel>
        └─► Agent B acts on the message
```

## Configuration Files (What Gets Installed)

### Per-Project `.claude/settings.json` (Claude Code hooks → QuicBaaS)

```json
{
  "hooks": {
    "TaskCreated": [{
      "hooks": [{
        "type": "http",
        "url": "http://localhost:9090/api/v1/hooks/task-created",
        "timeout": 10
      }]
    }],
    "TaskCompleted": [{
      "hooks": [{
        "type": "http",
        "url": "http://localhost:9090/api/v1/hooks/task-completed",
        "timeout": 10
      }]
    }],
    "TeammateIdle": [{
      "hooks": [{
        "type": "http",
        "url": "http://localhost:9090/api/v1/hooks/teammate-idle",
        "timeout": 10
      }]
    }]
  }
}
```

### Per-Project `.mcp.json` (QuicBaaS as MCP server + Channel)

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve", "--address", "localhost:9090"],
      "env": {
        "QUICBAAS_ADDRESS": "localhost:9090"
      }
    }
  }
}
```

### Claude Code launch command (channels enabled)

```bash
claude --channels server:quicbaas \
       --model qwen3:8b \
       --agent software-developer
```

## Security Architecture: Enterprise Agent Authorization

QuicBaaS already has production-grade security infrastructure. The agent fabric integrates with ALL of it:

### MCP OAuth 2.1 Authorization (per spec at modelcontextprotocol.io)

QuicBaaS ALREADY implements:
- OAuth2 Authorization Code Grant + Client Credentials Grant (`pkg/iam/oidc.go`)
- RFC 8414 OAuth AS Metadata (`pkg/api/mcp/oauth_metadata.go`)
- JWT token generation with ES256/RS256 signing (`pkg/auth/jwt_utils.go`)
- Token validation and introspection
- MCP-specific scopes: `mcp:tools`, `mcp:resources`, `mcp:admin`

**What needs to be added for agents:**
- RFC 9728 Protected Resource Metadata endpoint (`/.well-known/oauth-protected-resource`)
- RFC 7591 Dynamic Client Registration (for agent self-registration)
- Agent-specific OAuth scopes: `agent:spawn`, `agent:prompt`, `agent:task`, `agent:broadcast`
- Per-agent service accounts with Client Credentials Grant (machine-to-machine auth)
- Audience validation for agent tokens (ensures token was meant for THIS QuicBaaS instance)

### ABAC/PEP for Agent Operations

QuicBaaS already has sub-5μs ABAC policy evaluation (`pkg/iam/pep*.go`). Add agent-specific policies:

```yaml
# Example: Agent can only access tools in its tenant namespace
policy:
  name: agent-tool-access
  subjects:
    - role: agent-worker
      tenant: engineering
  resources:
    - agent:task:*       # Can manage tasks
    - agent:broadcast:*  # Can broadcast messages
    - vector:namespace:codebase  # Can query RAG
    - kms:key:agent-dek  # Can use agent encryption key
  actions:
    - read
    - write
  conditions:
    - when: request.tenant == subject.tenant  # Tenant isolation enforced
```

### Multi-Tenant LLM Agent Isolation (FedRAMP / HIPAA / PCI DSS Compliant)

**STRICT ISOLATION - NO EXCEPTIONS:**
- Each tenant gets its own orchestrator namespace (tasks, sessions, messaging channels)
- Per-tenant DEK encrypts ALL orchestrator data at rest (`pkg/security/tenant_dek.go`)
- Cross-tenant LLM agent communication is **PROHIBITED** - no ABAC override, no exceptions
- **SuperAdmin CANNOT access LLM agent data across tenants** - this differs from other QuicBaaS services where SuperAdmin has cross-tenant access. LLM agents process potentially sensitive data (code, documents, PII) and must remain under full tenant control
- Tenant admin is the highest authority for their LLM orchestration namespace
- All LLM agent prompts, responses, task data, and session state are encrypted with tenant-specific DEKs
- Tenant deletion cascades to destroy ALL LLM agent data (sessions, tasks, RAG indices, cached embeddings)

**Compliance Controls:**

| Requirement | Control | Implementation |
|-------------|---------|----------------|
| **FedRAMP** Data Residency | Tenant data stays on tenant-assigned nodes | Raft shard pinning per tenant |
| **HIPAA** PHI Protection | All agent prompts/responses encrypted at rest | Per-tenant DEK via KMS |
| **HIPAA** Audit Trail | Every agent operation logged with tenant context | `pkg/security/audit.go` |
| **HIPAA** Access Control | Minimum necessary access per agent role | ABAC policies with tenant scope |
| **PCI DSS** Segmentation | Tenant network isolation for agent traffic | Per-tenant messaging channels |
| **PCI DSS** Key Management | Unique encryption keys per tenant | Tenant DEK provisioner |
| **SOC 2** Access Reviews | Agent permission audit reports | ABAC policy export + audit logs |
| **GDPR** Right to Deletion | Tenant deletion destroys all agent data | Cascade delete on tenant removal |
| **GDPR** Data Portability | Export all agent session/task data per tenant | Export API |

**What SuperAdmin CAN do (operational only):**
- View aggregate metrics (sessions count, task count) without data content
- Manage tenant quotas (max sessions, max tasks, rate limits)
- Monitor system health across tenants (without seeing prompts/responses)
- Enable/disable orchestration features per tenant

**What SuperAdmin CANNOT do (enforced at code level, not policy):**
- Read LLM agent prompts or responses from another tenant
- Access tenant-specific RAG indices or embeddings
- View task descriptions or results from another tenant
- Impersonate a tenant's LLM agent sessions
- Decrypt tenant agent data (no access to tenant DEK)

### Configurable Authentication per Backend/Session

Auth is configurable at three levels with cascading defaults:

```
Global Default → Backend Config → Session Override
```

**Configuration structure:**
```yaml
orchestration:
  auth:
    default_method: auto          # auto, oauth2, device_cert, api_key, mtls
    auto_strategy:                # when method=auto
      prefer_device_cert: true    # use TPM/Secure Enclave cert if available
      fallback: oauth2            # fall back to OAuth2 if no device cert
    oauth2:
      issuer: https://quicbaas.internal/oauth
      scopes: [orchestrator:spawn, orchestrator:prompt, orchestrator:task]
    device_cert:
      ca_cert: /path/to/ca.crt
      verify_chain: true
  backends:
    claude-code:
      auth_method: auto           # inherits global default if not set
    ollama:
      auth_method: api_key        # Ollama may use simpler auth
    codex:
      auth_method: oauth2         # Codex uses OAuth2
```

**Auth methods supported:**

| Method | Use Case | Mechanism |
|--------|----------|-----------|
| `auto` | Default - auto-detect best method | Try device cert → fall back to OAuth2 |
| `oauth2` | Standard for cloud/remote agents | OAuth 2.1 Client Credentials + JWT |
| `device_cert` | Hardware-bound identity (TPM, Secure Enclave) | mTLS with device cert → maps to service account |
| `mtls` | Mutual TLS without device binding | Client cert + server cert verification |
| `api_key` | Simple auth for local/trusted agents | API key in header (for Ollama, dev environments) |

**Per-session override:**
```go
SpawnSessionRequest{
    Backend: "claude-code",
    Auth: &SessionAuthConfig{
        Method: "device_cert",      // override backend default
        CertPath: "/path/to/cert",  // session-specific cert
    },
}
```

### Device Certificate Authentication (TPM 2.0 / Secure Enclave)

For agents on specialized hardware:
- Agent authenticates with device certificate signed by TPM-backed CA
- QuicBaaS validates certificate chain via existing KMS/XKMS infrastructure
- Device certificate maps to service account in IAM
- mTLS between agent and QuicBaaS (already supported in all transports)
- **Auto-detection**: if machine has `/dev/tpmrm0` (Linux) or Secure Enclave (macOS), default to device cert

### Enterprise Security Controls

| Control | Mechanism | Existing Code |
|---------|-----------|---------------|
| Agent authentication | OAuth2 Client Credentials + JWT | `pkg/iam/oidc.go` |
| Tool authorization | ABAC policies per agent role | `pkg/iam/pep*.go` |
| Tenant isolation | Per-tenant namespaces + DEK | `pkg/iam/tenant*.go` |
| Message encryption | Per-tenant DEK for agent data | `pkg/security/tenant_dek.go` |
| Audit trail | All agent operations logged | `pkg/security/audit.go` |
| Rate limiting | Per-agent token bucket | `pkg/auth/login_throttle.go` |
| Device auth | TPM 2.0 / Secure Enclave certs | `pkg/config/kms.go` (TPM config) |
| Permission control | Configurable tool allowlists per agent | `pkg/api/mcp/authz.go` |
| Token scoping | Agent-specific OAuth scopes | `pkg/api/mcp/oauth_metadata.go` |
| Session management | JWT with nonce + refresh + revocation | `pkg/auth/manager.go` |

## Implementation Phases

### Phase 0: MCP OAuth 2.1 Authorization Compliance

**Goal**: Full compliance with the MCP authorization spec.

**Modify: `pkg/api/mcp/server.go`**
- Add `/.well-known/oauth-protected-resource` endpoint (RFC 9728)
- Return Protected Resource Metadata pointing to QuicBaaS OAuth server
- Include `scopes_supported` with agent scopes

**Modify: `pkg/iam/oidc.go`**
- Add Dynamic Client Registration endpoint (RFC 7591)
- Support agent-specific scopes: `agent:spawn`, `agent:prompt`, `agent:task`, `agent:broadcast`
- Add audience validation for agent tokens

**New: `pkg/iam/agent_auth.go`**
- Agent service account management (create, rotate credentials, revoke)
- Agent-to-service-account mapping
- Device certificate → service account binding

**Modify: `pkg/api/mcp/authz.go`**
- Add agent tool resource mappings (quicbaas_agent_* tools)
- Add agent-specific permission checks

### Phase 1: Hook Receiver Endpoints

QuicBaaS REST API receives Claude Code hook HTTP POSTs.

**New: `pkg/api/rest/handlers_hooks.go`**
- `POST /api/v1/hooks/task-created` - receives TaskCreated hook data
- `POST /api/v1/hooks/task-completed` - receives TaskCompleted hook data
- `POST /api/v1/hooks/teammate-idle` - receives TeammateIdle hook data
- Stores task data in KV, publishes to messaging channels
- Returns appropriate exit codes (0 = allow, 2 = block with feedback)

### Phase 2: Distributed Task Store

**New: `pkg/service/agent_task_store.go`**
- `TaskStore` interface backed by QuicBaaS KV (Raft-replicated)
- CRUD: CreateTask, GetTask, UpdateTask, ListTasks
- Atomic claiming via CAS (compare-and-swap)
- Task states: pending, claimed, in_progress, completed, failed
- Task dependencies and blocking

### Phase 3: QuicBaaS Channel Server + Remote Proxy

**New: `pkg/acp/channel_server.go`**
- MCP server with `claude/channel` capability
- Subscribes to QuicBaaS messaging channels for cross-host events
- Pushes `notifications/claude/channel` events into Claude Code sessions
- Exposes `reply` tool for two-way communication
- Supports `claude/channel/permission` for remote tool approval
- Works in two modes:
  - **Local mode**: connects to QuicBaaS services in-process (when on a QuicBaaS node)
  - **Remote proxy mode**: connects to remote QuicBaaS via gRPC/QUIC (when on an agent-only machine)

**New: `cmd/quicbaas/channel.go`**
- `quicbaas channel serve` - starts the channel server as a standalone MCP process
- Accepts `--address` to connect to remote QuicBaaS server
- Claude Code spawns this via `.mcp.json`: `{"command": "quicbaas", "args": ["channel", "serve", "--address", "quicbaas-server:9090"]}`
- This enables agents on machines WITHOUT QuicBaaS installed to participate in distributed coordination

### Phase 4: Agent Service Enhancements

**Modify: `pkg/service/agent_service.go`**
- Wire to distributed TaskStore (replaces in-memory)
- Wire to messaging for cross-host coordination
- Support lead agent configuration (auto-orchestration vs manual)
- Forward `Env` in SpawnSession for Ollama config

**Modify: `pkg/service/agent_types.go`**
- Add `Env`, `WorkDir` to SpawnSessionRequest
- Add `TaskStore` interface types
- Add orchestrator config with lead agent

### Phase 5: Network Transport Exposure (gRPC + QUIC + REST)

**New: `pkg/api/grpc/server_agent.go`** - Agent service over gRPC
**New: `pkg/api/quic/handler_agent.go`** - Agent service over QUIC
**New: `pkg/api/rest/handlers_agent.go`** - Agent service over REST
- All agent operations: spawn, prompt, status, close, list, task CRUD
- Plus hook endpoints from Phase 1

### Phase 6: Agent CLI Subcommand

**New: `cmd/quicbaas/agent.go`**
- `quicbaas agent discover|register|spawn|prompt|status|close`
- `quicbaas agent task create|list|claim|complete`
- `quicbaas agent broadcast`
- Multi-protocol (gRPC, REST, QUIC, MCP, Unix)

### Phase 7: E2E Integration Tests

**Test 1: `TestE2E_HookReceiver`**
- Simulate Claude Code TaskCreated hook POST
- Verify task stored in KV
- Verify messaging event published

**Test 2: `TestE2E_ChannelServer`**
- Start QuicBaaS channel server
- Push event, verify Claude Code receives it
- Test two-way reply

**Test 3: `TestE2E_DistributedTasks`**
- Create task on node 1, verify replicated to node 2
- Claim task on node 2, verify state updated on node 1

**Test 4: `TestE2E_ClaudeCode_SingleAgent`**
- Spawn Claude Code via ACP, connect to Ollama (GPU)
- Prompt to write Go code, verify compilation + tests pass

**Test 5: `TestE2E_MultiAgent_SoftwareAndQA`** (flagship)
- Software Agent writes code
- QA Agent reviews via messaging coordination
- Full task lifecycle through distributed store

**Test 6: `TestE2E_SharedRAG`**
- Index on one node, query from another

**Test 7: `TestE2E_AgentCLI_MultiProtocol`**
- All `quicbaas agent` commands across gRPC, REST, QUIC, MCP, Unix

**Test 8: `TestE2E_AgentAuth_OAuth2`**
- Agent authenticates via OAuth2 Client Credentials Grant
- Receives scoped JWT token (agent:spawn, agent:prompt)
- Verifies authorized operations succeed
- Verifies unauthorized operations are denied (403)
- Tests token refresh and expiration

**Test 9: `TestE2E_AgentAuth_TenantIsolation`**
- Create two tenants with separate agent pools
- Agent in tenant A cannot access tenant B's tasks/messages
- Verify per-tenant DEK encrypts agent data separately
- Verify ABAC policies enforce isolation

**Test 10: `TestE2E_AgentAuth_DeviceCert`** (when TPM available)
- Agent authenticates with device certificate
- Certificate maps to service account
- Verify mTLS handshake succeeds
- Verify operations authorized via mapped identity

**Test 11: `TestE2E_MCPAuth_ProtectedResourceMetadata`**
- Fetch `/.well-known/oauth-protected-resource`
- Verify scopes, authorization_servers fields
- Complete OAuth2 flow to get token
- Access MCP tools with valid token
- Verify 401 without token

**Test 12: `TestE2E_ContainerizedAgents_CrossBackend`** (flagship multi-backend)
- Start Claude Code, Codex, and OpenCode containers via docker-compose
- Each container pre-configured with QuicBaaS MCP + hooks + channel
- Claude Code as lead: assigns "write hello world" task to Codex worker
- Codex writes code, marks task complete
- QuicBaaS routes completion to OpenCode (QA role) via messaging
- OpenCode reviews code, confirms tests pass
- Verify full task lifecycle in distributed task store
- Verify all messaging delivered cross-backend
- Verify all auth tokens scoped correctly per container

**Test 13: `TestE2E_ContainerizedAgents_LeadRotation`**
- Run same workflow 3 times, rotating which backend is team lead
- Proves any backend can orchestrate any other backend
- Codex as lead → Claude Code + OpenCode as workers
- OpenCode as lead → Claude Code + Codex as workers

**Test 14: `TestE2E_ContainerizedAgents_SecurityIsolation`**
- Two tenants, each with their own agent containers
- Verify tenant A's agents cannot see tenant B's tasks
- Verify tenant A's agents cannot read tenant B's messaging channels
- Verify tenant A's RAG namespace is inaccessible from tenant B
- Verify per-tenant DEK encrypts agent data independently

### Phase 8: Documentation

**New:**
- `docs/orchestration/distributed-fabric.md` - Architecture (this diagram)
- `docs/orchestration/hooks-integration.md` - Hook config for Claude Code
- `docs/orchestration/channels.md` - Channel server setup
- `docs/orchestration/claude-code-setup.md` - Claude Code + Ollama config
- `docs/orchestration/multi-host.md` - Cross-host coordination
- `docs/orchestration/task-store.md` - Distributed task management
- `docs/orchestration/e2e-testing.md` - Testing guide

**Updated:**
- `docs/orchestration/overview.md` - New architecture
- `docs/orchestration/backends.md` - Claude Code backend
- `docs/orchestration/quickstart.md` - Real examples

## Network Partition Handling (Simple Approach)

Keep it pragmatic - no complex reconciliation policy engine:

1. **Hooks are fire-and-forget**: If QuicBaaS is unreachable, Claude Code hooks fail silently. The local agent keeps working on its task.
2. **On reconnect, agent re-syncs**: Channel server detects reconnection, agent pushes local task status to QuicBaaS.
3. **Idempotent completion**: If task was reassigned during partition, the late completion is logged but the reassigned result takes precedence.
4. **Task ownership check**: Before accepting a completion, orchestrator verifies `task.assigned_to == completing_agent`. If not, log a conflict and keep the current assignment's result.
5. **No complex policy engine**: This mirrors how Raft handles stale leaders - they discover they're no longer leader and step down.

## OpenCode Support

OpenCode (`github.com/anomalyco/opencode`) is actively maintained (v1.3.4, 132k stars, TypeScript/Go):
- Client/server architecture with remote session support
- Model-agnostic (Claude, OpenAI, Google, local models)
- MCP integration possible
- The cs50victor/claude-code-teams-mcp project already supports OpenCode as a backend

Integration: QuicBaaS orchestrator supports OpenCode via HTTP API for session management (same pattern as claude-code-teams-mcp).

## Implementation Order

1. **Phase -1** (Package Refactor) - move agent_service → pkg/orchestration/, rename types and MCP tools
2. **Phase 0** (MCP OAuth 2.1 + Tenant Isolation) - security foundation, compliance controls
3. **Phase 1** (Hook Receivers) + **Phase 2** (Task Store) - coordination foundation
4. **Phase 3** (Channel Server + Proxy) - cross-host communication
5. **Phase 4** (Service Enhancements) - wire everything together
6. **Phase 5** (Network Transport) - remote access over all protocols
7. **Phase 6** (CLI) - `quicbaas orchestrator` command group
8. **Phase 7** (E2E + Unit + Integration Tests) - EXTENSIVE testing at every layer
9. **Phase 8** (Docs) - comprehensive documentation following existing conventions

### Testing Requirements (applies to ALL phases)

Every phase MUST include:
- **Unit tests**: 90%+ coverage, typed error handling, edge cases
- **Integration tests**: Multi-protocol table-driven tests (gRPC, REST, QUIC, MCP, Unix)
- **Security tests**: Auth enforcement, tenant isolation, ABAC policy evaluation
- **Documentation**: Package docs, API reference, examples in docs/ folder

## Files Summary

### Package Refactor (Phase -1)
| File | Action |
|------|--------|
| `pkg/orchestration/` | CREATE directory |
| `pkg/acp/*.go` → `pkg/orchestration/*.go` | MOVE all files |
| `pkg/service/agent_*.go` → `pkg/orchestration/` | MOVE + rename |
| `pkg/api/mcp/agent_tools.go` → `pkg/api/mcp/orchestrator_tools.go` | MOVE + rename |
| `pkg/api/mcp/agent_cache_tools.go` → `pkg/api/mcp/orchestrator_cache_tools.go` | MOVE + rename |
| `pkg/service/quicbaas.go` | MODIFY - update imports to pkg/orchestration |
| `pkg/service/service.go` | MODIFY - update imports to pkg/orchestration |
| `pkg/server/lifecycle.go` | MODIFY - update imports to pkg/orchestration |
| All test files that import pkg/acp or reference agent service | MODIFY - update imports |
| `pkg/acp/` | DELETE directory after move |

### Security (Phase 0)
| File | Action |
|------|--------|
| `pkg/api/mcp/server.go` | MODIFY - add Protected Resource Metadata endpoint |
| `pkg/iam/oidc.go` | MODIFY - add Dynamic Client Registration, orchestrator scopes |
| `pkg/iam/orchestrator_auth.go` | CREATE - orchestrator service account management |
| `pkg/api/mcp/authz.go` | MODIFY - orchestrator tool resource mappings |

### Hook + Task Store (Phases 1-2)
| File | Action |
|------|--------|
| `pkg/api/rest/handlers_hooks.go` | CREATE - hook receiver endpoints |
| `pkg/orchestration/task_store.go` | CREATE - distributed task store |
| `pkg/orchestration/task_types.go` | CREATE - task types |

### Channel Server (Phase 3)
| File | Action |
|------|--------|
| `pkg/orchestration/channel_server.go` | CREATE - MCP channel server |
| `cmd/quicbaas/channel.go` | CREATE - channel proxy CLI |

### Container Runtime (Phase 3)
| File | Action |
|------|--------|
| `pkg/orchestration/container_runtime.go` | CREATE - OCI exec (docker/podman) |
| `pkg/orchestration/container_types.go` | CREATE - ContainerSpec, ContainerRuntime interface |

### Network Transport Exposure (Phases 4-5)
| File | Action |
|------|--------|
| `pkg/orchestration/orchestrator.go` | MODIFY - wire task store, channels, containers |
| `pkg/orchestration/orchestrator_types.go` | MODIFY - add Env, WorkDir, ContainerConfig |
| `pkg/api/grpc/server_orchestrator.go` | CREATE - gRPC orchestrator handlers |
| `pkg/api/quic/handler_orchestrator.go` | CREATE - QUIC orchestrator handlers |
| `pkg/api/rest/handlers_orchestrator.go` | CREATE - REST orchestrator handlers |

### CLI (Phase 6)
| File | Action |
|------|--------|
| `cmd/quicbaas/orchestrator.go` | CREATE - `quicbaas orchestrator` subcommand |

### Agent Containers (Phase 7)
| File | Action |
|------|--------|
| `test/integration/containers/claude-code/Dockerfile` | CREATE |
| `test/integration/containers/codex/Dockerfile` | CREATE |
| `test/integration/containers/opencode/Dockerfile` | CREATE |
| `test/integration/containers/docker-compose.agents.yml` | CREATE |

### Tests (Phase 7)
| File | Action |
|------|--------|
| `pkg/orchestration/*_test.go` | Unit tests for all moved/new code (90%+ coverage) |
| `test/integration/orchestration/e2e_*.go` | CREATE - 14 E2E test cases |
| `test/integration/orchestration/main_test.go` | CREATE - test infrastructure |

### Documentation (Phase 8)
| File | Action |
|------|--------|
| `docs/orchestration/distributed-fabric.md` | CREATE |
| `docs/orchestration/hooks-integration.md` | CREATE |
| `docs/orchestration/channels.md` | CREATE |
| `docs/orchestration/claude-code-setup.md` | CREATE |
| `docs/orchestration/multi-host.md` | CREATE |
| `docs/orchestration/task-store.md` | CREATE |
| `docs/orchestration/e2e-testing.md` | CREATE |
| `docs/orchestration/security.md` | UPDATE - agent auth, MCP OAuth 2.1, device certs |
| `docs/orchestration/overview.md` | UPDATE - new architecture |
| `docs/orchestration/backends.md` | UPDATE - Claude Code backend |
| `docs/orchestration/quickstart.md` | UPDATE - real examples |
| `docs/orchestration/coordination-modes.md` | CREATE |

## Verification

1. `make ci` - all unit tests pass
2. `make integration-test` - 35/35 packages pass
3. `make integration-test-agent` - existing 20 Ollama tests still pass
4. `make integration-test-agent-e2e` - 11 new E2E tests with Claude Code + Ollama + GPU
5. MCP OAuth 2.1: `/.well-known/oauth-protected-resource` returns valid metadata
6. Agent auth: OAuth2 Client Credentials → JWT → tool access → ABAC enforcement
7. Tenant isolation: agents in different tenants cannot see each other's data
8. Hook integration: Claude Code TaskCreated hook → QuicBaaS → KV store → messaging
9. Channel integration: QuicBaaS pushes event → Claude Code session receives it
10. Multi-host: task created on Host A, executed on Host B, result returned
11. CLI: `quicbaas agent` commands work over gRPC, REST, QUIC, MCP, Unix
