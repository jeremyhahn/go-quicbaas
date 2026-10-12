# Orchestration — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Design, component boundaries, data flow and rejected alternatives for the
> orchestration engine (`pkg/orchestration`): agent sessions, backends, the job
> envelope, fleet, containment and the transports that reach them. The modules
> built on the engine are in
> [../software-factory/architecture.md](../software-factory/architecture.md).
> Feature folder: `docs/orchestration/`. Conflicts with this document are
> defects.

## Purpose

The engine lets one QuicBaaS node discover, register, spawn, prompt and monitor
AI coding agents (Claude Code, Codex, OpenCode, Ollama and inference servers)
across projects and hosts, and run autonomous jobs over them. It owns sessions,
backends, jobs and their runners, workflows, tasks, fleet and containment. It
owns nothing the stack already provides.

## Design principle

Minimal new code: the engine is a coordination surface over existing QuicBaaS
services.

| Reused primitive    | Engine use                                      |
|---------------------|-------------------------------------------------|
| KV store            | Sessions, jobs, tasks, the session event log, agent cache |
| Messaging           | Broadcast and coordination channels              |
| RAG + vector store  | Codebase indexing and context enrichment         |
| Scheduler           | Periodic task orchestration                      |
| IAM                 | Tenant scoping, RBAC, per-object ACL             |
| Secrets store       | Backend API keys and credentials                 |
| Parameter store     | Backend configuration and model defaults         |

Identity, tenancy, secrets, keys and the barrier are go-qrdb's and go-xkms's,
consumed through the SDK edge. The engine holds a tenant ID and asks.

## Architecture

```
  MCP clients / REST / gRPC / QUIC / Unix / embedded SDK
                         |
            transports (pkg/api/*, sdk/go/transport/*)
                         |
                  AgentService  (orchestration.AgentService)
        +----------------+-----------------+----------------+
        |                |                 |                |
   Backends          Job envelope      Session stores   Fleet registry
   - ACP (claude,    - JobRunner       - AgentStore     - node daemons
     codex, opencode)  seam            - event log      - remote ACP
   - remote ACP      - lease, reaper   - project,       - inference
   - codex exec      - task dispatch     task stores      routing
   - Ollama, API,
     OpenVINO
        |
   Containment: sandbox axis + trust axis, resolved before a spawn
```

Every verb is wired across REST, gRPC, QUIC, MCP and Unix plus the in-process
embedded transport; the per-transport routes are in [api.md](api.md).

## Build tags

The ACP and agent service packages are always compiled in. Only RAG indexing
and MCP tool registration need optional tags:

```bash
make build WITH_RAG=1 WITH_VECTOR=1 WITH_MCP=1
```

This sets `rag`, `vector`, `qrdb_vector` and `mcp`. Without `WITH_RAG=1` and
`WITH_VECTOR=1` the RAG tools are excluded; without `WITH_MCP=1` the MCP tool
layer is excluded.

## Package layout

The engine is a single package, `pkg/orchestration`. There is no `pkg/acp`: the
ACP backend and its siblings are files within it.

```
pkg/orchestration/
    orchestrator.go            AgentService: spawn, prompt, jobs, tasks
    orchestrator_types.go      AgentService interface, request/response types
    orchestrator_resume.go     Session reconstitution and trust re-resolution
    errors.go                  Typed errors

    backend.go                 AgentBackend interface, SessionState
    acp_*.go                   ACP backend (JSON-RPC over stdio): container, resume, account, events
    remote_acp_backend*.go     Backend dispatched to a fleet node daemon
    codex_exec_backend.go      Codex exec backend
    ollama_backend.go          Ollama stateless backend
    api_inference_backend.go   Hosted-API inference backend
    openvino_backend.go        OpenVINO inference backend
    inference/                 Inference subpackage (stateless inference contract)

    session_store.go           AgentStore, SessionRecord, KV-backed store
    session_log*.go            Durable session event log (transcript source of truth)
    session_events.go          Event types appended to the log
    session_history.go         Paged reads of the log (history, conversation)
    session_resume.go          Resumable sessions across daemon restart
    session_reconcile.go       Boot-time reconciliation of stored sessions
    session_rotation.go        Moving a session to another pooled account
    session_conclusion.go      Concluding a session and its audit line

    job_runner.go              JobRunner seam and registry; the "workflow" runner
    job_run*.go                *JobRun envelope and its lease
    job_envelope.go            launchJob, the one start path, fresh or resumed
    job_status_record.go       JobStatus record and amendment merge
    control_loop*.go           The workflow runner's loop: lead decomposition into a DAG
    job_hooks.go               JobLifecycleHooks, JobContext, stage timeouts
    task_source.go             TaskSource seam, registry, per-project poller
    task_dispatcher.go         Starts jobs for pending tasks of autonomous projects
    dag_scheduler.go           Task dependency scheduling
    validators.go              Validator registry

    page*.go                   List contract: paging, filters, facets, specs
    health.go                  AgentHealthMonitor

    sandbox_*.go, mount_policy.go          Sandbox axis
    trust_policy.go, container_*.go        Trust axis, runtime, attestation

pkg/api/mcp/                   MCP tools (orchestrator, fleet, cache, RAG, account, project)
sdk/go/client/orchestrator/    Go SDK client
sdk/go/orchestration/          Type-alias re-export of the container-trust surface
```

Projects and pooled accounts (`project.go`, `account_*.go`) live in the same
package but belong to the Software Factory; see
[../projects/architecture.md](../projects/architecture.md) and
[../accounts/architecture.md](../accounts/architecture.md).

## Session persistence (AgentStore)

`AgentStore` abstracts session persistence. There is exactly one
implementation, `sessionStore`, and it keeps no copy of the data it serves:
every method reads and writes an injected `interfaces.KVStore`. Durability,
replication and encryption at rest are properties of that store alone.
`NewAgentStore(kv)` takes any KV; `NewInMemoryAgentStore()` is a memory-backed
binding of the same code, not a second implementation.

Every method is tenant-explicit, so one tenant can neither read nor mutate
another's records. `ListAllSessions` is the single deliberate exception: the
boot-time recovery scan, which runs before any tenant context exists.

The transcript is not stored on the record. It lives in the session event log,
the single source of truth for conversation content; the record only points at
it.

## Session event log and attach

Every event a session produces (prompts, assistant output, tool calls, file
operations, operator joins, close) is appended to the session's durable event
log with a monotonic sequence number. Two readers sit over it:

- **History** pages the log (`session_history`), and the conversation view
  folds it into turns.
- **Attach** replays from a chosen sequence and then tails live, so a client
  that disconnects resumes from the last sequence it saw. Any number of
  operators can attach to one session and see the same order. Interrupting an
  attach never stops a turn; the turn runs on the server.

## Agent health monitoring

`AgentHealthMonitor` periodically checks active sessions through the
`AgentService`. It uses `atomic.Bool` for lock-free running state and reports
per-session health via `HealthCheckReport`. Sessions in `active`, `idle` or
`resumable` state are healthy (`isHealthySessionState`, `health.go`); `error`
and `closed` are flagged.

## Backend kinds

Backends are classified by `BackendKind`: **agent** (ACP-compatible,
persistent sessions, tool use) or **model_server** (stateless prompt and
response). Stateless inference, including embeddings, is routable to a fleet
node daemon; see [Inference Backends](engineering.md).

## Job records

A job's record has more than one writer: the run publishes whole snapshots, and
child spawns and session conclusions amend the same record in between. Writes
are compare-and-swap with the amendments folded into the snapshot, not plain
puts; see [Job Records and Amendments](engineering.md).

## Real-time event streaming

`SubscribeJobEvents` streams events from running jobs. The CLI subscribes when
it starts a job and falls back to polling if streaming is unavailable. Jobs run
server-side and survive the CLI disconnecting.

## List contract

Every list verb (sessions, jobs, backends, fleet nodes, projects) shares one
paging, sorting, filtering and facet contract implemented once in `page*.go`
and applied identically on every transport. A malformed page request is a typed
`MalformedPageError`, never an empty page. The fields per endpoint are in
[api.md](api.md#list-paging-sorting-and-search).

## Session lifecycle

```
Spawn --> Active --> Idle --> Active --> ... --> Close
             |        |
             |        +--> Error
             |
             +--> Resumable --> Active   (daemon restart; process recreated
                                          by the next prompt)
```

States: `active`, `idle`, `resumable`, `closed`, `error` (`SessionState`,
`backend.go`). `resumable` is the durable-record state: the record and
transcript survived a daemon restart but the agent process did not. The session
stays addressable, and the next prompt recreates the process and restores the
conversation before the turn runs.

## Execution policy

A spawn is admitted along two orthogonal axes, both resolved at the same
pre-side-effect chokepoint and both deny-by-default:

| Axis | Question | Reference |
|------|----------|-----------|
| Sandbox | Is the agent isolated from the host? | [sandbox.md](sandbox.md) |
| Trust | Is the agent the code we approved, and can the host prove it? | Container Trust, below |

## Rejected alternatives

- **Claude Code agent teams as the coordination layer.** They are
  file-based and local to one machine, so they cannot span hosts. The engine
  keeps coordination in the shared KV and messaging instead.
- **Run git and verification on the daemon.** Both run inside the worker's
  container through `ContainerRuntime.Exec`; a job with no containerized
  session is refused rather than run locally.
- **Model a delivery pipeline as a Workflow.** A Workflow is a lead agent
  decomposing work into a DAG; an issue is not a DAG. Other shapes of job plug
  in at the `JobRunner` seam ([job-runners.md](job-runners.md)) rather than
  bending the workflow runner.
- **Keep the transcript on the session record.** Conversation content has one
  source of truth, the append-only event log, which is what makes sequence
  replay and live attach possible; the record only points at it.
- **Plain put of a job snapshot.** It erases whatever another writer amended
  since the run last read the record. Guarded compare-and-swap with a merge
  makes both writers' work survive; a store that cannot do it is refused
  rather than silently weakened.
- **A second store implementation for tests.** The in-memory store is the same
  code over a memory KV, so tests exercise the production paths.
- **Per-transport list logic.** One paging contract implemented once, so the
  same request returns the same page over REST, gRPC, MCP and the CLI.

## See also

- Container Trust, below: digest pinning, host attestation, measured launch.
- Agent Orchestration Security, below: multi-tenancy, ACLs, sandbox and trust
  policy.
- [Job Admission, Attempt Accounting, and Reaping](../accounts/architecture.md#job-admission):
  durable job leases, environment-fault refunds, the reaper and stall detector.
- [Autonomous Projects](autonomy-and-dispatch.md): `Project.Autonomous`, the
  task dispatcher, context repositories and the container workspace contract.
- [Fleet](fleet.md) and [Job runners](job-runners.md).
- The Software Factory modules: [SDLC](../sdlc/architecture.md),
  [projects](../projects/architecture.md),
  [pooled accounts](../accounts/architecture.md) and
  [prompt sets](../software-factory/prompt-sets.md).


---

## Distributed coordination: why QuicBaaS, and how it connects

### Context

Claude Code has built-in agent teams (experimental, local-only) with task lists, mailboxes, and teammate coordination. However, teams are **strictly local** - file-based storage at `~/.claude/teams/` and `~/.claude/tasks/`, subprocesses on the same machine. They cannot span physical hosts.

QuicBaaS fills this gap as the **distributed coordination layer** by integrating with Claude Code through three official extension points:

1. **Hooks** (`TaskCreated`, `TaskCompleted`, `TeammateIdle`) - HTTP hooks POST task events to QuicBaaS
2. **Channels** - QuicBaaS runs as an MCP channel server, pushing events INTO Claude Code sessions
3. **MCP Tools** - Claude Code agents use QuicBaaS tools for RAG, KV, secrets, messaging, vector search

The naming convention that separates a QuicBaaS agent from an AI agent is in
[engineering.md](engineering.md#naming-convention).

### Deployment Modes

The architecture works at every scale - from a single laptop to a multi-datacenter Raft cluster. Agents do NOT need to run on QuicBaaS servers.

#### Mode 1: Standalone (Single Machine, No Raft)

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

#### Mode 2: Remote Agents (Agents NOT on QuicBaaS servers)

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

**How workspaces work remotely** (go-quicbaas#981): a factory spawn *names*
its workspace (tenant, project, key) instead of giving a server path. The agent
that runs the session provisions it under **its own** projects root
(`quicbaas fleet serve --projects-root`, default `<--work-dir>/projects`),
writes the workspace spec and forge token beside it, and reports the directory
back; the server records it on the session. The job's later sessions — review
rounds, a rolled-over worker, the respawn after a restart — are pinned to that
agent with strict host affinity, and a pinned agent that is gone is a typed
refusal (`WorkspaceFleetNodeUnavailableError`), never a fresh clone elsewhere. The
checkout, commit and push all run inside the session's container on that
agent. See [sandbox.md § Agent-local workspaces](sandbox.md#agent-local-workspaces);
the KMS signing agent stays daemon-local: a spawn that signs or clones over
ssh mounts it on an agent co-located with the daemon, and on any other agent
advertising `signing-relay` it is served over the agent's FleetRuntime link
(`SigningRelay` stream), the key never leaving the daemon host
(go-quicbaas#1013; [sandbox.md](sandbox.md#agent-local-workspaces)). Placement
ranks every eligible agent by live jobs against its `max_concurrent_jobs` and
takes the least loaded, skipping a full one; host affinity stays strict. An
agent silent past `orchestration.heartbeat_timeout` is not dispatched to.

#### Mode 3: Multi-Node Raft Cluster

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

#### Mode 4: Remote Daemon with Container Sandboxing (DEFAULT)

A long-lived daemon (`quicbaas fleet serve`) runs on remote GPU hosts and exposes a FleetRuntime gRPC service. The central orchestrator calls this service to spawn sandboxed (containerized) sessions. This is the **sandbox-first** default.

```
  CENTRAL QUICBAAS               REMOTE GPU HOST (gpu-host.example.com)
┌────────────────────────┐     ┌──────────────────────────────────────┐
│                        │     │                                       │
│ BackendDispatcher      │     │  quicbaas fleet serve                 │
│  │                     │     │  (daemon process)                     │
│  ├─ LocalACPBackend    │     │  ┌─────────────────────────────────┐ │
│  │  (local spawn)      │     │  │ FleetRuntime gRPC Service       │ │
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
│                        │     │  │ │ JWT scp = the GRANT on its   ││ │
│                        │     │  │ │ app record (never requested):││ │
│                        │     │  │ │  - agent:runtime:spawn      ││ │
│                        │     │  │ │  - agent:runtime:prompt     ││ │
│                        │     │  │ │  - agent:runtime:status     ││ │
│                        │     │  │ │  - orchestrator:agent_hea…  ││ │
│                        │     │  │ │  - orchestrator:hook        ││ │
│                        │     │  │ └─────────────────────────────┘│ │
│                        │     │  └─────────────────────────────────┘ │
│                        │     │          ↓                           │
│                        │     │  Docker daemon (host socket mount)   │
│                        │     │  Ollama (http://localhost:11434)    │
└────────────────────────┘     └──────────────────────────────────────┘
```

**Key features:**
- Daemon enrolls with central using OAuth2 Client Credentials (one-shot enrollment token)
- Enrollment **grants** the permission set onto the daemon's OAuth2 application
  record (`iam.FleetRuntimeGrant`). A `client_credentials` token carries that
  grant; the daemon sends no `scope` parameter, so it cannot name authority it
  was not given, and a grant change reaches it on its next token refresh
  without re-enrollment.
- Enrollment **also grants the same set to the identity the daemon's CLIENT
  CERTIFICATE authenticates as** (`iam.IdentityStore.EnrollFleetNodeCertIdentity`).
  A daemon holds two credentials for one identity, and go-qrdb's authenticator
  chain is first-success-wins with mTLS FIRST: any request carrying a client
  certificate is authenticated as the certificate's subject, and the bearer
  fallback is not consulted at all. Because the daemon presents its certificate
  on every call (it needs one for `/oauth2/token-mtls`), the application grant
  alone left every control-plane call refused 403 — go-quicbaas#663. The
  enrollment is the same ceremony go-qrdb performs for the platform admin
  certificate at first boot: a user record whose username is the certificate
  CN, a dedicated tenant-scoped `agent-runtime` role carrying the grant, and a
  binding to the certificate's canonical SPKI thumbprint. The record is a
  machine identity — active, `AuthMethods: ["mtls"]`, no email login key — so
  it is unreachable by any human login ceremony, and a certificate whose
  tenant-scoped SPIFFE SAN names a different tenant than the registration is
  refused rather than enrolled where the authenticator would never look.
- A registration that cannot confer that grant FAILS (500): the response
  advertises the grant, and credentials the control plane will refuse are not a
  registration. An identity an administrator disabled is never resurrected by a
  re-registration.
- A spawned **session** gets its OWN principal, not the daemon's. A daemon is
  long-lived and holds control-plane authority; an agent session is one process
  for one job reaching the platform only over MCP, so it is enrolled separately
  through `iam.IdentityStore.EnrollAgentSessionIdentity` against the per-session
  certificate `pkg/server.AgentMCPPresetResolver` mints, holds the narrower
  tenant-scoped `agent-session` role (`iam.AgentSessionGrant`), and is REVOKED
  when the session closes. The two grants are disjoint, so neither enrollment is
  a route to the other's authority. See `docs/software-factory/engineering.md`
  §MCP Preconfiguration for Spawned Agents.
- The fleet-registry record is written under the **record's own tenant**
  (`orchestration.fleetTenantContext`). `POST /api/v1/fleet/enroll` is a
  public route — the mTLS handshake is the authentication — so it carries no
  identity and its context names no tenant, while every read of the registry is
  made under the tenant the request names. Without the scoping the row landed in
  the system namespace and no tenant-scoped read could see it: registration
  reported `agent already exists` while the heartbeat got
  `fleet registry: not found` for the same agent (go-quicbaas#663).
- Heartbeats into fleet registry; central can spawn sessions remotely
- Sandbox policy: **required** (default; container-only) — raw exec forbidden unless explicitly allowed
- Reuses existing `ACPBackend`, `OllamaBackend`, `ContainerRuntime` (no parallel spawn logic)
- Session state encrypted with tenant DEK; audit logging per RPC
- Graceful shutdown drains in-flight sessions with configurable timeout

**Operators guide**: See `docs/agents/daemon.md` for deployment, enrollment, troubleshooting, and systemd integration.

**Use case**: Production GPU hosts, compliance-sensitive tenants (FedRAMP/HIPAA), central orchestrator needs direct control of session lifecycle.

#### Mode 5: Remote Daemon with Raw Process Execution (OPT-IN)

Same architecture as Mode 4, but with sandbox policy `opt-in` or `preferred`, allowing clients to explicitly request raw process execution (no container). This is less secure but useful for legacy systems or trusted development environments.

```
  CENTRAL QUICBAAS               REMOTE HOST
┌────────────────────────┐     ┌──────────────────────────────────────┐
│                        │     │                                       │
│ BackendDispatcher      │     │  quicbaas fleet serve                 │
│  │                     │     │  --sandbox-policy=opt-in              │
│  ├─ LocalACPBackend    │     │  (daemon process)                     │
│  │                     │     │  ┌─────────────────────────────────┐ │
│  ├─ RemoteACPBackend   │────────→│ FleetRuntime gRPC Service       │ │
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

#### Backend-Agnostic Orchestration

The orchestrator is NOT married to Claude Code. It works with ANY backend implementing the `AgentBackend` interface (`pkg/acp/backend.go`):

| Backend | Protocol | Sessions | Tool Use | Extra Features |
|---------|----------|----------|----------|----------------|
| **ACP** (Claude Code, Codex) | JSON-RPC over stdio | Persistent | Yes | Hooks, Channels, Teams |
| **Ollama** | HTTP REST | Stateless | No | GPU inference |
| **Custom** | Implement `AgentBackend` | Configurable | Configurable | User-defined |

Claude Code-specific features (hooks, channels, permission relay) are additive enhancements that only activate when using an ACP backend. The core orchestration (task store, messaging coordination, RAG sharing, session management) works identically across ALL backends.

E2E tests validate **Claude Code, Ollama, AND Codex/OpenCode backends** to prove backend-agnostic operation.

#### Containerized Agent Runtimes (Sandboxed Execution)

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
- **Remote fleet nodes**: QuicBaaS gRPC/QUIC session management

The containers are ALSO usable in production for sandboxed agent execution with strict security controls.

#### Container Runtime: Simple OCI Exec (Docker/Podman)

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

### How Every Piece Communicates (Detailed)

#### Integration Architecture

```
HOST A                                         HOST B
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

#### Data Flow: Task Created on Host A → Executed on Host B

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

#### Data Flow: Shared RAG Across Hosts

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

#### Data Flow: Direct Agent-to-Agent Messaging

```
Step 1: Agent A broadcasts via MCP tool
        └─► quicbaas_orchestrator_broadcast(channel="agent:review-request", message="Review PR #42")
        └─► QuicBaaS publishes to messaging channel
        └─► Replicated to all nodes

Step 2: QuicBaaS Channel Server on Host B pushes to Claude Code
        └─► Agent B receives: <channel source="quicbaas" type="broadcast">Review PR #42</channel>
        └─► Agent B acts on the message
```

### Configuration Files (What Gets Installed)

#### Per-Project `.claude/settings.json` (Claude Code hooks → QuicBaaS)

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

#### Per-Project `.mcp.json` (QuicBaaS as MCP server + Channel)

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

#### Claude Code launch command (channels enabled)

```bash
claude --channels server:quicbaas \
       --model qwen3:8b \
       --agent software-developer
```

### Security Architecture: Enterprise Agent Authorization

QuicBaaS already has production-grade security infrastructure. The agent orchestration engine integrates with ALL of it:

#### MCP OAuth 2.1 Authorization (per spec at modelcontextprotocol.io)

QuicBaaS ALREADY implements:
- OAuth2 Authorization Code Grant + Client Credentials Grant (`pkg/iam/oidc.go`)
- RFC 8414 OAuth AS Metadata (`pkg/api/mcp/oauth_metadata.go`)
- JWT token generation with ES256 signing, xkms-held key (`pkg/iam/oidc_tokens.go`, `pkg/iam/oidc_keymanager.go`)
- Token validation and introspection
- MCP-specific scopes: `mcp:tools`, `mcp:resources`, `mcp:admin`

**What needs to be added for agents:**
- RFC 9728 Protected Resource Metadata endpoint (`/.well-known/oauth-protected-resource`)
- RFC 7591 Dynamic Client Registration (for agent self-registration)
- Agent-specific OAuth scopes: `agent:spawn`, `agent:prompt`, `agent:task`, `agent:broadcast`
- Per-agent service accounts with Client Credentials Grant (machine-to-machine auth)
- Audience validation for agent tokens (ensures token was meant for THIS QuicBaaS instance)

#### ABAC/PEP for Agent Operations

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

#### Multi-Tenant LLM Agent Isolation (FedRAMP / HIPAA / PCI DSS Compliant)

**STRICT ISOLATION - NO EXCEPTIONS:**
- Each tenant gets its own orchestrator namespace (tasks, sessions, messaging channels)
- Per-tenant DEK encrypts ALL orchestrator data at rest (`pkg/iam/tenant_dek.go`)
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

#### Configurable Authentication per Backend/Session

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
| `oauth2` | Standard for cloud/remote fleet nodes | OAuth 2.1 Client Credentials + JWT |
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

#### Device Certificate Authentication (TPM 2.0 / Secure Enclave)

For agents on specialized hardware:
- Agent authenticates with device certificate signed by TPM-backed CA
- QuicBaaS validates certificate chain via existing KMS/XKMS infrastructure
- Device certificate maps to service account in IAM
- mTLS between agent and QuicBaaS (already supported in all transports)
- **Auto-detection**: if machine has `/dev/tpmrm0` (Linux) or Secure Enclave (macOS), default to device cert

#### Enterprise Security Controls

| Control | Mechanism | Existing Code |
|---------|-----------|---------------|
| Agent authentication | OAuth2 Client Credentials + JWT | `pkg/iam/oidc.go` |
| Tool authorization | ABAC policies per agent role | `pkg/iam/pep*.go` |
| Tenant isolation | Per-tenant namespaces + DEK | `pkg/iam/tenant*.go` |
| Message encryption | Per-tenant DEK for agent data | `pkg/iam/tenant_dek.go` |
| Audit trail | All agent operations logged | `pkg/security/audit.go` |
| Rate limiting | Per-client-IP token bucket | `pkg/api/rest/middleware.go` |
| Device auth | TPM 2.0 / Secure Enclave certs | `pkg/config/kms.go` (TPM config) |
| Permission control | Configurable tool allowlists per agent | `pkg/api/mcp/authz.go` |
| Token scoping | Agent-specific OAuth scopes | `pkg/api/mcp/oauth_metadata.go` |
| Session management | JWT with nonce + refresh + revocation | `pkg/iam/oidc_tokens.go` |


## Communication Topology

Inter-agent messaging topology that controls how orchestrated AI agents (Claude Code, Codex, OpenCode) communicate during a workflow.

Source: `pkg/orchestration/workflow_types.go`, `pkg/orchestration/communication_policy.go`

### Topology Modes

| Mode | Constant | Worker-to-Worker | Use Case |
|------|----------|-----------------|----------|
| `hub_and_spoke` | `TopologyHubAndSpoke` | Blocked | Default. Safest. All coordination flows through the lead agent. |
| `hybrid` | `TopologyHybrid` | Advisory only | Workers share observations (progress, findings, conflict warnings) directly; control actions route through lead. |
| `peer_to_peer` | `TopologyPeerToPeer` | Unrestricted | Full mesh. Workers can assign, cancel, or request work from each other. |

### When to Use Each

**hub_and_spoke** -- Use when you need deterministic control flow or are running untrusted agent configurations. The lead agent sees every message and can enforce ordering.

```
     +---------+
     |  Lead   |
     +----+----+
      /   |   \
     v    v    v
  +---+ +---+ +---+
  | W1| | W2| | W3|
  +---+ +---+ +---+
  (no cross-links)
```

**hybrid** -- Use when workers benefit from sharing progress or conflict warnings without waiting for the lead to relay. Control flow (assign, cancel, complete) still goes through the lead.

```
     +---------+
     |  Lead   |   <-- control flow
     +----+----+
      /   |   \
     v    v    v
  +---+ +---+ +---+
  | W1|<-->| W2|<-->| W3|   <-- advisory only
  +---+ +---+ +---+
```

**peer_to_peer** -- Use for autonomous workflows where agents self-organize. Requires careful budget and lock configuration to prevent runaway behavior.

```
  +---+     +---+
  | W1|<--->| W2|
  +---+\   /+---+
    ^   \ /   ^
    |    X    |
    v   / \   v
  +---+/   \+---+
  | W3|<--->|Lead|
  +---+     +----+
```

### Advisory vs Control Actions

The `advisoryActions` map classifies which actions are informational (no acknowledgment required):

| Action | Type | Description |
|--------|------|-------------|
| `advisory` | Advisory | General informational message |
| `progress` | Advisory | Status update on current work |
| `finding` | Advisory | Report a discovery or observation |
| `conflict_warning` | Advisory | Warn about potential file/resource conflict |
| `assign` | Control | Assign a task to an agent |
| `complete` | Control | Signal task completion |
| `cancel` | Control | Cancel an agent's current work |
| `feedback` | Control | Provide review feedback |

In `hybrid` mode, worker-to-worker messages are allowed only for advisory actions. Any control action between workers returns `ErrNonAdvisoryPeerMessage`.

### Communication Policies (Deadlock Prevention)

Policies define whether a message is fire-and-forget or blocking:

| Direction | Action | Policy | Rationale |
|-----------|--------|--------|-----------|
| Lead -> Worker | `assign` | Fire-and-forget | Prevents hub-spoke deadlock |
| Lead -> Worker | `feedback` | Fire-and-forget | Lead must not block on worker |
| Lead -> Worker | `cancel` | Fire-and-forget | Cancellation is best-effort |
| Worker -> Lead | `complete` | Blocking | Completion must be acknowledged |
| Worker -> Lead | `failed` | Blocking | Failure must be acknowledged |
| Worker -> Lead | `question` | Fire-and-forget | Questions are advisory |
| Validator -> Lead | `result` | Blocking | Review results must be acknowledged |

Unknown combinations default to fire-and-forget to prevent accidental deadlocks. Blocking messages time out after `MaxBlockDuration` (5 minutes).

### Configuration

Set in `WorkflowSettings`:

```yaml
settings:
  topology: hybrid   # hub_and_spoke | hybrid | peer_to_peer
```

Default: `hub_and_spoke` (applied by `ApplyDefaults()`).

Validation rejects unknown values via `ValidateTopology()`.


---

## Coordination Modes

### Overview

QuicBaaS supports multiple coordination patterns for orchestrating agent teams. The pattern determines how tasks are assigned, claimed, and tracked across agents.

### Direct Orchestration

A lead agent decomposes work and assigns tasks directly to specific teammates.

```
Lead Agent
    |
    +-- assign task A --> Worker 1
    +-- assign task B --> Worker 2
    +-- assign task C --> Worker 3
```

**How it works:**

1. Lead agent calls `quicbaas_orchestrator_decompose` to break work into prioritized items.
2. Lead agent calls `quicbaas_orchestrator_spawn` for each worker session.
3. Lead agent calls `quicbaas_orchestrator_prompt` to assign specific tasks to specific workers.
4. Workers fire `TaskCompleted` hooks when done. Lead receives completion events via channel server.
5. Lead assigns follow-up work based on results.

**Best for**: Well-defined tasks with clear ownership. The lead agent has full visibility and control.

### Pub/Sub Coordination

Tasks are broadcast to a coordination channel. Any available agent claims the work.

```
Task Store (pending tasks)
    |
    +-- broadcast --> Channel: orchestrator:tasks:team-x
                        |
                        +-- Worker 1 (claims task A)
                        +-- Worker 2 (claims task B)
                        +-- Worker 3 (idle, waiting)
```

**How it works:**

1. Tasks enter the task store via hooks or direct API calls (status: `pending`).
2. QuicBaaS publishes task events to the team's messaging channel.
3. Channel servers push events into all active Claude Code sessions for that team.
4. The `TeammateIdle` hook checks for pending tasks. When a teammate goes idle, QuicBaaS responds with `exit_code: 2` and the next available task, preventing idle state.
5. The teammate picks up the task and fires `TaskCompleted` when done.

**Best for**: Elastic workloads where any worker can handle any task. Self-balancing across available agents.

### Hybrid Coordination

The lead agent assigns high-priority or specialized tasks directly. Remaining tasks go into the pub/sub pool for workers to claim.

```
Lead Agent
    |
    +-- assign critical task --> Worker 1 (specialist)
    |
    +-- publish remaining tasks --> Channel: orchestrator:tasks:team-x
                                      |
                                      +-- Worker 2 (claims)
                                      +-- Worker 3 (claims)
```

**How it works:**

1. Lead agent decomposes work, tagging items with priority and required capabilities.
2. High-priority or specialized tasks are assigned directly to specific workers.
3. General tasks are left in `pending` status for pub/sub claiming.
4. `TeammateIdle` hooks distribute pending work to free agents automatically.
5. Lead agent monitors progress via channel events and reassigns as needed.

**Best for**: Teams with specialized agents (e.g., one agent for database work, others for general tasks).

### Task Dependencies and Blocking

Tasks support dependency chains via the `dependencies` field on work items produced by `quicbaas_orchestrator_decompose`.

```json
{
  "work_items": [
    {"id": "wi_001", "title": "Define interface", "priority": 1, "dependencies": []},
    {"id": "wi_002", "title": "Implement handler", "priority": 2, "dependencies": ["wi_001"]},
    {"id": "wi_003", "title": "Add tests", "priority": 3, "dependencies": ["wi_002"]}
  ]
}
```

**Dependency resolution:**

- A task with unmet dependencies stays in `pending` status but is not assigned to workers.
- When a dependency completes (`TaskCompleted` hook), the task store evaluates blocked tasks.
- Newly unblocked tasks are published to the messaging channel for assignment or claiming.
- Circular dependencies are rejected at decomposition time.

### Agent-to-Agent Messaging

Agents communicate directly through QuicBaaS messaging channels using the broadcast tool and channel server reply tool.

#### Broadcasting

Any agent can publish to a coordination channel:

```json
{
  "tool": "quicbaas_orchestrator_broadcast",
  "arguments": {
    "channel": "agent-updates",
    "message": "Found breaking API change in pkg/api/, all agents should re-read interfaces.",
    "metadata": {
      "session_id": "sess_x1y2z3",
      "severity": "high"
    }
  }
}
```

All agents subscribed to `agent-updates` receive this via their channel servers.

#### Replying

Agents with the reply tool enabled can send messages back through the channel server:

```json
{
  "name": "reply",
  "arguments": {
    "channel": "agent-updates",
    "message": "Acknowledged, re-reading pkg/api/ interfaces."
  }
}
```

#### Channel Naming Conventions

| Channel Pattern | Purpose |
|---|---|
| `orchestrator:tasks:<team>` | Task lifecycle events (created, completed) |
| `agent-updates` | Cross-agent status broadcasts |
| `orchestrator:health:<team>` | Health check results |

### Choosing a Coordination Mode

| Factor | Direct | Pub/Sub | Hybrid |
|---|---|---|---|
| Task assignment control | High | Low | Medium |
| Load balancing | Manual | Automatic | Mixed |
| Specialized workers | Supported | All workers equal | Supported |
| Lead agent required | Yes | No | Yes |
| Complexity | Low | Low | Medium |
| Best scale | Small teams | Elastic pools | Mixed workloads |


---

## State Machine

Two-level state machine for tracking orchestrated AI agent lifecycle and task execution.

Source: `pkg/orchestration/state_machine.go`

### Why Two Levels

A single state machine cannot capture both "is this agent available?" and "what is this agent doing right now?" without an explosion of combined states. The two-level design separates:

- **MemberStatus** (fleet-level): Is the agent registered, busy, shutting down, or crashed?
- **ExecutionStatus** (task-level): Is the agent idle, processing, calling a tool, or done?

This means an agent can be `busy` (fleet) and `calling_tool` (execution) simultaneously, and each level transitions independently.

### MemberStatus (Fleet-Level)

5 states representing the agent's lifecycle within the fleet.

```
                  +-------+
           +----->| ready |<-----+
           |      +---+---+      |
           |        |   |        |
           |        v   v        |
           |   +------+ +---+   |
           +---| busy | |err|---+
               +--+---+ +---+
                  |   |
                  v   v
         +-----------+----------+
         | shutdown_  | shutdown |
         | requested  |          |
         +-----+------+----------+
               |
               v
          +---------+
          |shutdown  |
          +---------+
```

#### Transitions

| From | To | Trigger |
|------|----|---------|
| `ready` | `busy` | Task assigned |
| `ready` | `shutdown_requested` | Graceful shutdown initiated |
| `ready` | `error` | Health check failure |
| `busy` | `ready` | Task completed |
| `busy` | `shutdown_requested` | Graceful shutdown during work |
| `busy` | `error` | Agent crash or timeout |
| `shutdown_requested` | `shutdown` | Shutdown complete |
| `shutdown_requested` | `error` | Shutdown failure |
| `shutdown` | (terminal) | No transitions out |
| `error` | `ready` | Recovery / restart |

### ExecutionStatus (Task-Level)

10 states representing the agent's current execution phase.

```
        +------+
  +---->| idle |<---------+---------+
  |     +--+---+          |         |
  |        |              |         |
  |        v              |         |
  |  +-----------+        |         |
  |  | awaiting  |        |         |
  |  | _input    |        |         |
  |  +-----+-----+        |         |
  |        |              |         |
  |        v              |         |
  |  +------------+       |         |
  |  | processing |<--+   |         |
  |  +--+--+--+---+   |   |         |
  |     |  |  |       |   |         |
  |     v  v  v       |   |         |
  |  +----+ +-----+  |   |         |
  |  |call| |gen-  |  |   |         |
  |  |tool| |erate |--+   |         |
  |  +-+--+ +--+---+      |         |
  |    |        |          |         |
  |    +---+----+          |         |
  |        |               |         |
  |        v               |         |
  |  +----------+    +-----------+   |
  |  | completed|--->|           |   |
  |  +----------+    |           |   |
  |                  |  (back    |   |
  |  +----------+    |  to idle) |   |
  +--| cancelled|<---|           |   |
     +----------+    +-----------+   |
                                     |
     +-------+                       |
     | error |-->--------------------+
     +-------+
```

#### Transitions

| From | Valid Targets |
|------|--------------|
| `idle` | `awaiting_input`, `processing`, `error` |
| `awaiting_input` | `processing`, `cancelling`, `error` |
| `processing` | `calling_tool`, `generating`, `paused`, `cancelling`, `completed`, `error` |
| `calling_tool` | `processing`, `cancelling`, `error` |
| `generating` | `processing`, `completed`, `cancelling`, `error` |
| `paused` | `processing`, `cancelling`, `error` |
| `cancelling` | `cancelled`, `error` |
| `cancelled` | `idle` |
| `completed` | `idle` |
| `error` | `idle` |

### Transition Options

```go
type TransitionOption struct {
    Guard bool  // Idempotent: same-state transition returns nil
    Force bool  // Bypass validation (post-crash recovery only)
}
```

#### Guard Flag

When `Guard` is true and `from == to`, the transition succeeds with no-op. This prevents errors when setting a state that is already set (idempotent operations).

Use case: Heartbeat updates that re-confirm the current state.

#### Force Flag

When `Force` is true, all transitions are allowed regardless of the transition map. This is exclusively for crash recovery where the state machine may be in an inconsistent state.

Use case: Moving a crashed agent from any state to `error`.

### Validation Functions

```go
ValidateMemberTransition(from, to MemberStatus, opts TransitionOption) error
ValidateExecTransition(from, to ExecutionStatus, opts TransitionOption) error
```

Both use O(1) map lookups against pre-built transition tables. Invalid transitions return `MemberTransitionError` or `ExecTransitionError` with the sentinel errors `ErrInvalidMemberTransition` or `ErrInvalidExecTransition`.

### Example

```go
// Normal transition
err := ValidateMemberTransition(MemberStatusReady, MemberStatusBusy, TransitionOption{})

// Idempotent guard
err := ValidateExecTransition(ExecStatusProcessing, ExecStatusProcessing, TransitionOption{Guard: true})
// err == nil (no-op)

// Crash recovery
err := ValidateMemberTransition(MemberStatusBusy, MemberStatusError, TransitionOption{Force: true})
// err == nil (always succeeds)
```


---

## Container Trust

Container trust answers one question about an orchestrated agent: **is it
running the code we approved, and can the host prove it?**

That is a different question from the one the sandbox answers ("is the agent
isolated from the host?"), and neither answer implies the other. A container can
be perfectly isolated while running an image someone swapped under a mutable
tag, and a digest-pinned image can run with no sandbox at all. QuicBaaS
therefore resolves the two on separate axes. This document covers the trust
axis; see [Sandbox Policy](engineering.md#sandbox-policy-reference) for the
other one, and the [orthogonal sandbox axis](#the-orthogonal-sandbox-axis)
section below for how they interact.

Source: `pkg/orchestration/trust_policy.go`, `container_trust.go`,
`container_attestation.go`, `container_runtime_digest.go`.

### Trust Levels

`TrustLevel` is the per-request integrity tier. It is the trust-axis
counterpart of `SandboxMode`.

| Level | Wire name | Tier | Requires |
|-------|-----------|------|----------|
| `TrustLevelDefault` | `default` | — | Nothing; defers to the server's `TrustPolicy`. Zero value. |
| `TrustLevelNone` | `none` | — | Nothing. Mutable tags allowed, no attestation. What every execution did before the trust axis existed. |
| `TrustLevelPinned` | `pinned` | 0 | Launch from an immutable content digest (`repository@sha256:<hex>`), verified after launch. No hardware. |
| `TrustLevelAttested` | `attested` | 1 | Tier 0 plus a host TPM quote verified against the spec's golden PCRs. |
| `TrustLevelMeasured` | `measured` | 2 | Tier 1 plus measured launch: image and configuration digest extended into a PCR *before* the container starts. |

Because `TrustLevelDefault` is the Go zero value and `TrustPolicyOptIn` is the
zero-value policy, a deployment that has never been configured for trusted
computing behaves exactly as it did before the trust axis existed.

#### Capabilities

`TrustCapabilities` describes what a target host can actually prove. It has one
field per tier:

| Field | Tier | Meaning |
|-------|------|---------|
| `DigestPinning` | 0 | The container runtime can launch and verify an image by content digest. |
| `HostTPM` | 1 | The host exposes a TPM the runtime can quote. |
| `MeasuredLaunch` | 2 | The host permits extending the measurement PCR before launch. |

The zero value advertises no capability at all, which is the correct posture for
a host that has not been probed: a required tier then fails closed rather than
silently downgrading. A remote (daemon-dispatched) spawn advertises no local
capability, so it is refused above `opt-in` until that daemon proves the tier
itself.

### Resolution

`ResolveTrustLevel(requested, serverPolicy, tenantOverride, caps)` produces the
effective tier. It is deny-by-default in **both** directions:

- A tenant override may only **tighten** the server policy. A weaker override is
  ignored, so a tenant with a stricter compliance obligation can raise the floor
  without the operator tightening the whole deployment, and a tenant record can
  never lower it.
- An explicit level **below** the effective floor is rejected with
  `TrustLevelViolationError`. A caller cannot opt out of a tier the operator
  requires.
- A level the host **cannot prove** is rejected rather than downgraded. The one
  exception is a `default` request under `TrustPolicyOptIn`, which resolves to
  `none` because opt-in means "no trust requirement at all".

The server policy is configuration:

```yaml
orchestration:
  trust_policy: pinned   # opt-in (default) | pinned | attested | measured
```

Each policy sets the floor that `default` resolves to and that nothing may fall
below: `opt-in` → `none`, `pinned` → Tier 0, `attested` → Tier 1, `measured` →
Tier 2.

`SpawnSession` resolves the level at the same pre-side-effect chokepoint that
resolves the sandbox mode, and the **resolved** level — never the caller's
request — is what reaches the container spec and the backend.

#### Re-resolution on resume

The resolved level is persisted on the session record, and a resume
**re-resolves** it rather than replaying it
(`orchestrator_resume.go`). The recorded level is what the session was
originally admitted at, but a floor the operator has raised since must apply to
the process being started *now*. A session that no longer clears the floor fails
to resume instead of quietly reconstituting at its old tier.

### Tier 0 — Image Identity Pinning

Tier 0 requires no hardware and no host cooperation, so it is the only tier the
default `ExecContainerRuntime` implements. It answers exactly one question: is
the agent running the bytes we approved?

**Pinning.** `ContainerSpec.ImageDigest` pins the container to a canonical OCI
content digest, or the digest may be supplied inline as
`repository@sha256:<hex>` in `Image`. When both are present they must agree —
disagreement is an `ImageDigestMismatchError` at validation time rather than a
silent preference for one of the two. `ValidateImageDigest` accepts only SHA-256
in canonical lowercase hex; OCI permits other algorithms in principle, but
accepting one keeps the comparison surface small and matches every registry in
practice.

**Post-launch verification.** `VerifyPinnedImage` confirms the container the
runtime actually created came from the pinned image. Note that a registry
*manifest* digest and a *local image ID* are different values, so the check
resolves the pinned reference to its local image ID first and compares like with
like — both values coming from the runtime's own inspect output. A substituted
image, which is the exact attack digest pinning exists to stop, produces a
`ContainerImageDigestMismatchError`.

The check is skipped for unpinned specs, and skipped when the container cannot
be inspected yet: an interactive container is created asynchronously by the
foreground run process, and failing a launch because the runtime had not
finished creating the container would be a false positive. The command-line pin
still applies in that window, since the runtime cannot resolve
`repository@digest` to anything other than that digest.

**Detached signatures.** `ImageSignature` optionally carries endorsement
material for the pinned digest: a signature over the ASCII digest string plus
the signer's certificate chain, leaf first. The signed payload is the digest
string rather than a repository name, so an endorsement is portable across
registries and mirrors — it binds the approving key to image *content*.
Verification is delegated to the go-xkms attestation verifier through
`Security().Attestation()`; nothing in `pkg/orchestration` reimplements a
signature check or shells out to an external tool.

This is deliberately **not** registry-native (cosign / sigstore) verification.
That needs a registry client and a Rekor transparency-log client, neither of
which is available in-repo, and adding either belongs in its own change rather
than behind a stub.

#### The configuration digest

`ContainerConfigDigest(spec)` is a deterministic SHA-256 over the parts of a
spec that decide what the container executes, what it can see, and what it is
allowed to do: the pinned image reference, entrypoint, command, args,
environment, mounts (including each mount's kind), ports, working directory,
network, user, GPU passthrough, auto-remove, labels, the trust label, **the
privileged flag, and the capability add/drop sets**.

Privilege is measured for the same reason image content is. A container flipped
to `Privileged`, or granted `CAP_SYS_ADMIN`, is a different workload from the
one an operator approved, and a digest that ignored the difference would certify
the safe configuration while the dangerous one ran.

Determinism is the whole point, because this digest is extended into a PCR at
measured launch and two launches of the same approved configuration must produce
the same value:

- Every map is serialized in sorted key order. Go randomizes map iteration order
  per process, so a naive range over `Env` or `Labels` would silently make
  measurements non-reproducible.
- Every field is length-prefixed, so no combination of values can alias another.
- Capability lists are sorted before hashing, and the runtime emits them in that
  same sorted order, so the measured spec and the launched container cannot
  disagree.
- Mounts are hashed in declaration order, because they are order-significant on
  the command line.

Two things are deliberately **excluded**: `Name`, a per-instance identifier that
is frequently random; and the trust-verification metadata itself (`TrustLevel`,
`ExpectedPCRs`, `ImageSignature`, `MeasurementPCR` / `MeasurementBank`), which
describes how a launch is *checked* rather than what is launched. Including the
latter would make the measurement depend on the policy that verifies it.

`TrustLabel` **is** included: it is an operator-assigned part of the approved
identity, so one image and configuration can serve two distinct measured
identities.

### Tiers 1 and 2 — Host Attestation and Measured Launch

Both tiers compose go-xkms `pkg/tpm2`. Nothing in QuicBaaS reimplements a TPM
primitive.

`HostTPM` is the narrow capability the attesting runtime needs — `Quote`,
`ReadPCRs`, `ExtendPCR`. It is satisfied as-is by go-xkms `*tpm2.TPM2` and by
anything implementing xkms's `TrustedPlatformModule`, including its software
simulator, so no adapter type exists between the two projects.

`TPMAttestingRuntime` is the reference `AttestingContainerRuntime`. It wraps a
base runtime (normally `ExecContainerRuntime`, which supplies Tier 0) and adds
attestation on top. Both are required at construction: an attesting runtime with
no TPM would silently degrade to Tier 0 while advertising more.

#### Tier 1 — the host quote

`Attest` quotes the union of the spec's expected PCR indices and, for a measured
launch, the measurement PCR. A host whose firmware, bootloader or kernel changed
produces different PCRs and is refused before an agent is scheduled onto it
(`PCRMismatchError`). A missing quoted PCR is a mismatch, not a pass — absence
of evidence is not evidence of a match.

#### Tier 2 — measured launch

Immediately **before** the container starts, `LaunchMeasurement` is extended
into the measurement PCR. A measurement taken afterwards would not be a measured
launch; it would be an assertion that something already running was approved.

The measurement is a newline-free, NUL-separated record so a verifier can
reconstruct it from the spec alone:

```
"quicbaas-container-launch/v1" \0 <imageDigest> \0 <configDigest> \0 <trustLabel>
```

PCR 16 (`MeasurementPCRDefault`) is the default target. It is the TCG PC Client
"debug" register: resettable, not consumed by firmware or the bootloader, and
the conventional home for application-supplied measurements, so extending it
cannot disturb a platform's boot-integrity PCRs. `MeasurementBank` defaults to
`sha256`, the mandatory bank on every TPM 2.0 PC Client platform.

The resulting quote proves not merely that the host booted correctly, but that
*this exact configuration* was the thing launched on it — a property no post-hoc
inspection of a running container can establish.

#### Binding the PCR read to the signed quote

This is the subtlest part of the design and the reason the golden-value
comparison sits *downstream* of a binding step.

A `TPM2_Quote` signs a `TPMS_ATTEST` structure whose `TPMS_QUOTE_INFO` carries
the PCR selection and a single `pcrDigest` — `H(PCR[i1] || PCR[i2] || …)` over
the selected registers in selection order. It does **not** carry the individual
PCR values, so those must come from a separate, unsigned `TPM2_PCR_Read`.

Comparing golden values against that read alone establishes nothing: the read
carries no signature, and a PCR can be extended in the window between the quote
and the read. `bindQuotedPCRs` closes both gaps before any golden value is
looked at:

- **Freshness.** The nonce must be inside the signed body, not merely alongside
  it. `VerifyQualifyingData` compares `extraData` exactly.
- **Agreement.** `pcrDigest` is recomputed over the values that were read, in
  the order the *signed* selection names them, and must equal the digest inside
  the signed body. A TOCTOU change between quote and read, or a tampered read
  path, makes the recomputation differ and is rejected as
  `QuotedPCRDigestMismatchError`.

Two further failure modes are errors rather than fallbacks. A PCR selected in
more than one bank with differing values would make the index-to-value map
ambiguous, and a golden comparison against an ambiguous value is not a
comparison. A bank that is genuinely absent from the read is
`PCRBankUnavailableError`, never a fallback to whichever bank happened to be
first — reading PCRs from an unintended bank produces measurements that look
valid and mean nothing.

What binding does **not** establish is that the signature over the quote body
verifies under a trusted attestation key. That needs the AK public key and its
certificate chain, neither of which the narrow `HostTPM` seam exposes, and it
belongs to the remote verifier that consumes `ContainerAttestation.Quoted` and
`.Signature`. Locally, the quote body is trusted because it came from the host's
own TPM handle; what is being defended against is a divergence between that body
and the separate PCR read.

#### The evidence record

`ContainerAttestation` is self-contained — a remote verifier needs the quote,
the nonce it was bound to, the PCR values, and the identity of what was
launched, and all four are present. Every value in `PCRs` has been bound to
`Quoted`; values that fail that check never reach a caller. `PCRBank` names the
bank the *signed selection* dictated, not one the spec guessed at, so a verifier
does not have to infer it.

### Spec Fields

Every trust field on `ContainerSpec` is inert at its zero value, so a spec
written before the trust dimension existed launches byte-for-byte the same
command line it always did.

| Field | Tier | Purpose |
|-------|------|---------|
| `ImageDigest` | 0 | Canonical `sha256:<hex>` content pin. |
| `ImageSignature` | 0 | Detached endorsement over the digest, plus signer chain. |
| `TrustLevel` | — | Required tier; `default` defers to server policy. |
| `TrustLabel` | 0+ | Operator-assigned workload identity; folded into the config digest. |
| `ExpectedPCRs` | 1 | Golden PCR set, keyed by index. Required at `attested` and above. |
| `MeasurementPCR` | 2 | PCR extended at measured launch. Defaults to 16. |
| `MeasurementBank` | 2 | `sha1` \| `sha256` \| `sha384` \| `sha512`. Defaults to `sha256`. |

Validation is offline and deterministic: a spec requiring Tier 0 or above
without a pin is rejected at `Validate` time (`ImageNotPinnedError`), not at
launch, and `attested` without any expected PCR is rejected the same way.

### Errors

Every failure is a typed error wrapping an `Err*` sentinel, so callers can match
either the specific condition or the class with `errors.Is`.

| Sentinel | Raised when |
|----------|-------------|
| `ErrImageNotPinned` | A tier requires a digest pin but the spec supplies only a mutable tag. |
| `ErrImageDigestMismatch` | A resolved image digest disagrees with the pinned one. |
| `ErrImageDigestUnresolved` | The runtime could not determine an image's content digest. |
| `ErrAttestationUnavailable` | The capability a tier requires is not present on this host. |
| `ErrPCRMismatch` | A quoted PCR does not match its golden value. |
| `ErrMeasurementFailed` | The measured-launch PCR extension failed, so the container was not started. |
| `ErrQuoteFailed` | The host TPM refused or failed to produce a quote. |
| `ErrQuoteNotBound` | PCR values could not be tied to the signed quote body. |
| `ErrQuotedPCRDigestMismatch` | The recomputed `pcrDigest` disagrees with the signed one. Also reports `ErrQuoteNotBound`. |
| `ErrPCRBankUnavailable` | A required PCR bank is not among those the TPM reported. |

### What QuicBaaS Owns, and What It Does Not

`TPMAttestingRuntime` is the **standalone** implementation and it is complete on
its own: QuicBaaS has no dependency on go-trusted-platform, and a single-node
deployment needs nothing further.

Fleet-grade trust is a different problem, and it is go-trusted-platform's: a
device registry that resolves hosts to their approved identities, scheduled
re-attestation, golden-PCR policy, and quarantine for hosts that drift. None of
that belongs in QuicBaaS.

Two seams exist for it to fill:

- **`AttestingContainerRuntime`** (`pkg/orchestration/container_attestation.go`),
  re-exported for SDK-bound consumers as `sdk/go/orchestration`. That package is
  pure type aliases with no behavior, so a value produced by `pkg/orchestration`
  is the same value there and `errors.Is`, `errors.As` and type assertions work
  across the boundary unchanged. Its completeness contract is closure, not
  totality:
  every exported identifier reachable from an alias — through an interface
  method signature, an exported struct field, or a constructor parameter — is
  itself aliased, so a consumer that can name a type can always name every
  argument it must supply.
- **`ContainerAttestor`**, registered through the
  `quicbaas.WithContainerAttestor` server option. It mirrors the `DANEPublisher`
  seam exactly: QuicBaaS declares the interface, ships a nil default, and returns
  `ErrAttestationNoAttestor` until an embedder registers one.
  `NewRuntimeContainerAttestor` fills the seam with the standalone tiers for
  deployments that do not need a fleet. Implementations **must** fail closed: a
  host that cannot prove its state returns an error, never an unattested success.

### The Orthogonal Sandbox Axis

The sandbox axis asks whether the agent is isolated from the host. It resolves
with the same shapes as the trust axis — a per-request mode, a server policy, an
optional tenant override that may only tighten — and it also denies by default,
but it is a separate decision throughout.

| Dimension | Type | File | Question |
|-----------|------|------|----------|
| Containment | `SandboxMode` / `SandboxPolicy` | `sandbox_policy.go` | Is the agent in a container at all? |
| Network | `SandboxNetwork` | `sandbox_network.go` | Can the agent reach the host's loopback services? |
| Privilege | `SandboxSecurity` | `sandbox_security.go` | What does root inside the container hold, and how many processes may it run? |
| Filesystem | mount allowlist | `mount_policy.go` | Which host paths may be bind-mounted in? |

Network isolation is worth calling out because it is easy to lose: a container
run with `--network host` joins the host's own network namespace, so every
loopback-bound service reaches it — the daemon's admin listeners, local metrics
endpoints, a `127.0.0.1` datastore — precisely the services whose host firewall
rules assume no container can reach them. The default is therefore an isolated
namespace (`bridge`), and host networking is available only as an explicit
operator decision in configuration, never as something a spawn request can
select.

Bind mounts are deny-by-default: only sources under the requesting tenant's
subtree of the managed projects root, or under an operator-configured allowlist
root, are permitted.

Full operator reference: [Remote Fleet Node Daemon](engineering.md) —
[Sandbox Policy](engineering.md#sandbox-policy-reference),
[Sandbox Network](engineering.md#sandbox-network-reference),
[Bind-Mount Allowlist](engineering.md#bind-mount-allowlist-reference).

### See Also

- Agent Orchestration Security — the security surface this
  document expands on.
- Agent Orchestration Overview — the orchestration layer as a
  whole.
- [Remote Fleet Node Daemon](engineering.md) — operator reference for the
  sandbox axis.


---

## Agent Orchestration Security

### Multi-Tenancy

All orchestration data is tenant-scoped. The tenant ID is extracted from the request context via `iam.TenantFromContext(ctx)`. Projects, sessions, cache entries, and vector namespaces are isolated per tenant. Cross-tenant access is not possible through the MCP tool interface.

### Encryption at Rest

| Data Type          | Storage Backend    | Encryption                          |
|--------------------|--------------------|-------------------------------------|
| Project catalog    | KV Store           | ObjectEncryption (AES-256-GCM)      |
| Backend credentials| Secrets Store      | Envelope encryption                 |
| Backend config     | Parameter Store    | Encrypted parameters                |
| Cache entries      | KV Store           | ObjectEncryption (AES-256-GCM)      |
| Vector embeddings  | Vector Store       | Namespace-level encryption           |

Backend API keys (Anthropic, Ollama endpoints) are stored in the Secrets Store and never exposed through MCP tool responses.

### Access Control

#### Per-Object ACL

Every stored object (project registrations, cache entries) supports per-object ACL through the standard quicbaas ObjectEncryption and ACL mechanisms. The service layer enforces ACL checks before returning data.

#### RBAC Permissions

The following permissions govern agent orchestration operations:

| Permission            | Grants Access To                              |
|-----------------------|-----------------------------------------------|
| `agent:discover`      | Scan directories for agent-enabled projects    |
| `agent:register`      | Register and list projects                     |
| `agent:spawn`         | Create persistent agent sessions               |
| `agent:prompt`        | Send prompts to sessions or backends           |
| `agent:close`         | Terminate active sessions                      |
| `agent:decompose`     | Decompose tasks into work items                |
| `agent:cache:read`    | Read from the agent cache                      |
| `agent:cache:write`   | Write to the agent cache                       |
| `agent:admin`         | Full access including backend registration     |

Assign permissions through IAM roles. A typical developer role includes `agent:discover`, `agent:register`, `agent:spawn`, `agent:prompt`, `agent:close`, and `agent:cache:read`. Admin operations like `agent:admin` should be restricted to platform operators.

### Callback Path Validation

When ACP backends (Claude Code, Codex) request file system operations via callbacks, the `CallbackHandler` enforces strict path-based access control.

#### Validation Steps

1. **Path cleaning** -- `filepath.Clean()` normalizes the requested path, removing `.` and collapsing `..` segments.

2. **Symlink resolution** -- `filepath.EvalSymlinks()` resolves the real path. If the target file does not exist yet (write operations), the parent directory is resolved instead.

3. **Traversal detection** -- `filepath.Rel()` computes the relative path from each allowed directory to the resolved path. If the relative path starts with `..`, access is denied.

4. **Allowed directory check** -- The handler is initialized with a list of allowed paths (typically project work directories). Only paths within these directories pass validation.

#### Supported Callbacks

| Callback                       | Path Validated | Action                      |
|--------------------------------|----------------|-----------------------------|
| `fs/read_text_file`            | Yes            | Read file contents          |
| `fs/write_text_file`           | Yes            | Write file contents         |
| `session/request_permission`   | Yes (if path)  | Approve/deny path access    |

#### Example

```go
// pkg/orchestration/callbacks.go — there is no pkg/acp; the callback
// handler is a file inside pkg/orchestration.
handler := orchestration.NewCallbackHandler([]string{
    "/home/user/projects/my-service",
    "/home/user/projects/shared-lib",
})
// Requests to /home/user/projects/my-service/main.go --> allowed
// Requests to /etc/passwd --> denied
// Requests to /home/user/projects/my-service/../../etc/passwd --> denied
```

### Sandbox Bind Mounts

Every host path a spawn request would bind-mount into an agent container is
authorized by `orchestration.MountPolicy` inside `SpawnSession`, before any side
effect. A rejected spawn has started no container and called no backend.

Three request fields reach a container runtime, and all three are
caller-controlled:

| Field | When it is authorized | Where it would be mounted |
|-------|----------------------|---------------------------|
| `work_dir` | Containerized spawns | `/workspace` (ACP backends) or the same path (orchestrator sandbox path), read-write |
| `container_config.mounts` | Containerized spawns | Verbatim, as the caller wrote them |
| `extra_mounts` | Always | Appended to the container spec |

A source is permitted only when it lies under:

- `<orchestration.projects_root>/<tenant_id>/` -- the **requesting tenant's own**
  subtree of the managed projects root, or
- one of `orchestration.sandbox.allowed_mount_roots`, or
- the sdlc signing socket itself, at
  `<orchestration.sockets_root>/<session>/agent.sock` -- admitted **by
  construction**, not by an operator entry (see below).

Everything else is denied with a typed `MountNotAllowedError`, including every
mount when no policy is wired at all.

Four properties are load-bearing:

1. **Tenant scoping.** The projects root is shared
   (`<root>/<tenant_id>/<project_id>/workspace`), so the grant is the tenant's
   subtree, never the root. Directory permissions cannot substitute for this:
   the sandbox container runs as the daemon's own uid, which owns every tenant's
   workspace.
2. **Resolved, not lexical.** Each source is resolved with
   `filepath.EvalSymlinks` and re-tested against the roots, so an agent that
   plants a symlink inside its own workspace cannot have the symlink's target
   mounted on the next spawn. A source that does not exist is refused.
3. **Read-write, as the daemon user.** This is why an unchecked `work_dir` of
   `/` is host root read-write for the agent, and why the check lives in
   `SpawnSession` rather than in either backend.
4. **The sockets-root grant is the socket, not the root.** The daemon binds
   each sdlc attempt's KMS-backed signing agent under
   `orchestration.sockets_root` because a unix socket address does not fit
   under a managed workspace, so the policy has to admit it without the
   operator remembering to. What is admitted is only a path one directory
   below that root, named `agent.sock`, that is actually a socket on disk,
   and only as a **read-only** mount. The root itself, the session directory,
   and anything beside or below the socket stay denied -- which is also what
   stops a caller mounting the root to read the session names inside it. See
   [SDLC configuration](../sdlc/engineering.md#configuration).

Raw (non-sandboxed) execution bind-mounts nothing, so its work directory is not
a mount source and is not policy-checked. What isolates a raw agent from the
host is the sandbox axis (`sandbox_policy`), not the work directory.

```yaml
orchestration:
  projects_root: /var/lib/quicbaas/projects
  # Per-session agent sockets. Short on purpose: a unix socket path is
  # capped at 107 bytes. Empty derives $XDG_RUNTIME_DIR/quicbaas/<instance>,
  # else <storage.data_dir>/s. <instance> is 12 hexadecimal characters
  # derived from the absolute storage.data_dir, because $XDG_RUNTIME_DIR is
  # per-USER and two daemons of one user must not share a sockets root.
  # It is NOT an allowed_mount_roots entry.
  sandbox:
    # Shared deployment-wide; NOT tenant-scoped. Do not list a directory
    # holding one tenant's data.
    allowed_mount_roots:
      - /srv/models
```

Operator notes:

- A workflow agent spec that pairs `sandboxed: true` with a `workspace_path`
  outside the projects root needs that path in `allowed_mount_roots`, otherwise
  the spawn is refused.
- A spawn dispatched to a remote fleet node daemon is authorized **twice**: against
  the control plane's roots here, and again on the daemon against its own
  `allowed_mount_roots` (see
  [fleet node daemon](engineering.md#bind-mount-allowlist-reference)). Both must
  permit the path. The daemon's check is not a duplicate — a spawn whose
  control-plane-resolved mode is not `required` skips the `work_dir`
  authorization above, while the daemon's own `sandbox_policy` may still
  containerize it — and the daemon's filesystem layout is its own. A daemon with
  no roots configured refuses every sandboxed spawn that names a work
  directory.
- The in-process **embedded** agent enforces this same policy, so a spawn
  dispatched over its bufconn cannot bypass the check.

#### Policy denials on the wire

A refused mount, sandbox mode, network, or trust tier is an authorization
decision about the request, not a server fault, and every transport says so.
`orchestration.IsPolicyDenial` is the single classifier all of them share:

| Transport | Status | Message |
|-----------|--------|---------|
| REST | `403 Forbidden` | The typed error's own text |
| gRPC | `PermissionDenied` | The typed error's own text |
| QUIC | `StatusPermissionDenied` | The typed error's own text |
| MCP | `OperationNotAllowed` (-32010) | The typed error's own text |
| Fleet node daemon (gRPC) | `PermissionDenied` for a refused mount; the control plane re-surfaces it as 403 | The typed error's own text |

The message names the offending field (`work_dir`, `extra_mounts`,
`container_config.mounts`), the tenant, the path, and the reason, so an operator
can tell a missing `allowed_mount_roots` entry from an outage. MCP deliberately
uses `OperationNotAllowed` rather than `PermissionDenied`: the latter answers
with an OAuth `insufficient_scope` challenge, which would send a client off to
acquire a broader token for a denial no token can lift.

### Sandbox Network

Container isolation has a second dimension: a container run with
`--network host` joins the host's own network namespace and can dial every
loopback-bound service on it, so a "sandboxed" agent is only half sandboxed.
Sandboxed sessions therefore default to an isolated namespace with egress
(`bridge` -- see `orchestration.SandboxNetwork`), and host networking is
reachable only as an explicit operator decision
(`ACPBackendConfig.SandboxNetwork`).

`SpawnSession` clamps the request side of this, next to the mount and trust
checks:

- A `container_config` naming `host` (in any spelling) fails the spawn with a
  typed `SandboxNetworkNotAllowedError`, before any container is created.
- Any other caller-supplied network is replaced by the operator-resolved value.
  The network is configuration, so the request has no say in it -- the only
  thing it can do is fail.

`SpawnRequest` carries no network field at all, so no tenant path reaches the
backend's own container launch either.

### Trust Policy

The trust axis is orthogonal to the sandbox axis: `sandbox_policy` answers "is
the agent isolated from the host", `trust_policy` answers "is the agent the code
we approved, and can the host prove it".

```yaml
orchestration:
  trust_policy: pinned   # opt-in (default) | pinned | attested | measured
```

| Policy | Floor | Requires |
|--------|-------|----------|
| `opt-in` | none | nothing (default; a session gets a tier only if it asks) |
| `pinned` | Tier 0 | container launched from an immutable image digest, verified after launch |
| `attested` | Tier 1 | Tier 0 plus a host TPM quote verified against the session's expected PCRs |
| `measured` | Tier 2 | Tier 1 plus measured launch (image + launch config extended into a PCR before start) |

`SpawnSession` resolves every request against this floor next to the sandbox
mode, and the **resolved** level -- never the caller's -- is what reaches the
container spec and the backend:

- A request naming no level runs at the floor.
- A request naming a **weaker** level is refused (`TrustLevelViolationError`),
  so a caller cannot opt out of the operator's baseline.
- A tier the target host cannot prove is refused, never downgraded. Raising the
  policy beyond what the deployment's runtime supports therefore fails spawns
  closed. A remote (daemon-dispatched) spawn advertises no local capability, so
  it is refused above `opt-in` until that daemon proves the tier itself.
- The resolved level is persisted on the session record and **re-resolved** on
  resume, so a floor raised after a session started applies to the process being
  restarted.

A tenant policy override, on either axis, may only **tighten** the server
policy. A weaker override is ignored.

This section covers the policy that admits a spawn. The tiers themselves --
digest pinning and detached signatures, the deterministic configuration digest,
the host quote and its binding to the PCR read, measured launch, and the SDK
seams a fleet operator fills -- are documented in
Container Trust.

### Service Integration Pattern

Agent orchestration follows the same service integration pattern as Secrets and Scheduler:

1. The `AgentService` interface is defined in `pkg/service/`
2. The implementation resides behind the service layer with proper encapsulation
3. MCP tools in `pkg/api/mcp/` call the service layer (never business logic directly)
4. IAM enforcement occurs at the service layer before any data access
5. All errors are typed (never inline `fmt.Errorf`)

### Sandboxed Session Isolation

A sandboxed agent session is isolated along three independent dimensions:

| Dimension | Control | Default |
|---|---|---|
| Process/filesystem | `sandbox_policy` (`pkg/orchestration/sandbox_policy.go`) | container-sandboxed on fleet node daemons |
| Network namespace | `sandbox_network` (`pkg/orchestration/sandbox_network.go`) | `bridge` — own namespace with egress |
| Privilege | `sandbox_cap_add` / `sandbox_pids_limit` / `sandbox_ulimits` (`pkg/orchestration/sandbox_security.go`) | `--cap-drop ALL` + CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID; `--pids-limit 4096`; `--ulimit memlock=4294967296:4294967296` |

No dimension is caller-selectable in the unsafe direction: a spawn request
can ask for a *stricter* sandbox mode but carries no network field at all, so no
tenant or workflow can move a session onto the host network namespace. Only
operator configuration can (`--sandbox-network host`), and doing so is logged at
WARN.
The privilege policy is applied over whatever spec the spawn carries, so a
caller's `ContainerConfig` cannot ask for `--privileged`, extra capabilities or
a looser pids bound.

Provider credentials in a sandboxed session's environment are passed to the
container runtime through a private `0600` `--env-file`, never as command-line
arguments, because `/proc/<pid>/cmdline` is world readable on a default Linux
host. See `docs/orchestration/accounts.md` and
`pkg/orchestration/container_env_file.go`.

### Recommendations

- Scope allowed callback paths to project work directories only
- Prefer managed project workspaces over `allowed_mount_roots`: a managed
  workspace is already tenant-scoped, an allowlist root is not
- Set `orchestration.trust_policy: pinned` wherever the container runtime can
  pin, so a mutable tag cannot be swapped under a running fleet
- Use separate tenants for production and development agent workloads
- Store all backend credentials in the Secrets Store, not in config files
- Assign minimal RBAC permissions per role
- Monitor session lifecycle events for anomalous activity
- Leave `sandbox_network` at `bridge`; reach for `host` only when the host's
  loopback services are already inside the agent's trust boundary
- Set TTL on cache entries to limit data retention


---

## Workspace Isolation

Controls how orchestrated AI agents share or isolate their working copies of the codebase.

Source: `pkg/orchestration/workflow_types.go`

### Modes

| Mode | Constant | Description |
|------|----------|-------------|
| `shared` | `IsolationShared` | All agents edit the same files in a single workspace. |
| `worktree` | `IsolationWorktree` | Each agent works in its own checkout, merged back on completion. |

Default: `shared` (applied by `ApplyDefaults()`).

**File locks are mandatory in every mode.** `Acquire` refuses a path whose
lease is live and held by another agent whoever asks, and the isolation mode
is recorded on the lock as provenance rather than consulted to decide it
(`pkg/orchestration/file_lock_store.go`). Worktree mode once made the lock
advisory — a second agent took it with only a warning logged — on the
reasoning that separate worktrees cannot collide on a file. Three things are
wrong with that reasoning:

- The service locks an opaque `(tenant, project, path)` triple. It cannot tell
  a path a per-agent worktree privatises from one that stays shared, and the
  shared surfaces are not marginal: refs, the object database and a
  submodule's checkout are shared by construction however the working
  directories are split.
- An override rebound the lock key to the newcomer's lease while the original
  holder, told nothing, still believed it held the lock. The original holder's
  `Release` then revoked the *newcomer's* lease and deleted the newcomer's
  lock mid-edit; its `Renew` extended a lease it did not hold; and the fencing
  token recorded for the key described only the last writer.
- Nothing in the daemon provisions a worktree. The daemon hands a container a
  workspace spec and the container clones (`pkg/orchestration/workspace_spec.go`),
  so `isolation: worktree` names a mechanism that is not there. Weakening a
  lock against that promise is the worse failure, because it is silent.

The semantics are asserted under both modes in
`pkg/orchestration/file_lock_test.go`; reintroducing the mode-check fails
`TestFileLock_Acquire_LiveLockOfAnotherAgentIsRefusedInEveryIsolationMode`.

### Shared Mode

All agents operate on the same filesystem directory. Concurrent edits to the same file will cause data corruption unless coordinated.

**Requirements:**
- File locking must be enforced (use `quicbaas_orch_file_lock` before editing)
- Agents must check lock status before writing
- The lead agent should assign non-overlapping file sets when possible

**When to use:**
- Simple workflows where agents work on different files
- Small teams (2-3 agents) with clear task boundaries
- When merge conflicts are unacceptable

```
/workspace/
  main.go        <-- locked by agent-1
  handler.go     <-- locked by agent-2
  service.go     <-- unlocked (available)
```

### Worktree Mode

Each agent receives its own git worktree branched from the main working tree. Changes are merged back after task completion.

**Behavior:**
- File locks are enforced exactly as in shared mode (see above)
- Each agent's edits are isolated until merge
- Merge conflicts are surfaced at merge time, not during editing

**When to use:**
- Large workflows with many agents editing overlapping files
- When agents need to experiment without affecting others
- When merge-at-completion is acceptable

```
/workspace/              <-- main worktree (lead agent)
/workspace/.worktrees/
  agent-1/               <-- isolated copy
  agent-2/               <-- isolated copy
  agent-3/               <-- isolated copy
```

### Merge Conflict Handling (Worktree Mode)

When a worktree is merged back:

1. The orchestrator attempts a fast-forward merge
2. If fast-forward fails, a three-way merge is attempted
3. If conflicts occur, the lead agent (or a designated validator) receives the conflict details
4. Resolution strategy depends on workflow configuration:
   - **Manual**: Lead agent resolves conflicts
   - **Rebase**: Worker rebases onto the latest main and retries
   - **Abort**: The worker's changes are discarded

### Configuration

```yaml
settings:
  isolation: shared    # shared | worktree
  file_locking:
    lease_ttl: 5m      # More important in shared mode
    heartbeat_interval: 30s
    max_renewals: 60
```

### Decision Matrix

| Factor | Shared | Worktree |
|--------|--------|----------|
| File conflict risk | High (mitigated by locks) | Low (isolated until merge) |
| Lock enforcement | Mandatory | Mandatory |
| Disk usage | 1x | Nx (one copy per agent) |
| Merge step required | No | Yes |
| Agent count | 2-3 recommended | Any |
| Real-time visibility | Immediate | At merge time |


---

## Multi-Host Coordination

### Overview

QuicBaaS supports three deployment modes that scale from a single laptop to a multi-datacenter Raft cluster. Agents (Claude Code, Codex, Ollama) do not need to run on QuicBaaS servers -- they connect over the network.

### Mode 1: Standalone (Single Machine)

A single QuicBaaS instance with PebbleDB storage coordinates all agents on the same machine. No cluster or network required.

```
+-- Laptop / Workstation ------------------------------------------+
|                                                                   |
|  Claude Code (lead + workers)                                     |
|       |                                                           |
|       +-- Hooks -> HTTP -> QuicBaaS (standalone, PebbleDB)        |
|       +-- MCP Tools -> QuicBaaS (RAG, KV, secrets)                |
|       +-- Channel <- QuicBaaS pushes events                       |
|                                                                   |
|  Ollama (local GPU or CPU)                                        |
+-------------------------------------------------------------------+
```

Start with:

```bash
quicbaasd start
```

All communication is localhost. Hooks target `http://localhost:9090/api/v1/hooks/*`. The channel server runs in-process with no `--address` flag.

### Mode 2: Remote Agents

Agents run on machines without QuicBaaS installed. They connect to a remote QuicBaaS server over the network.

```
+-- Agent Machine (Mac) ---+     +-- Agent Machine (x86) --+
|                           |     |                          |
|  Claude Code              |     |  Claude Code             |
|    Hooks -> HTTP ---------|--+  |    Hooks -> HTTP --------|--+
|    MCP -> gRPC/QUIC ------|--+  |    MCP -> gRPC/QUIC -----|--+
|    Channel <- ------------|--+  |    Channel <- -----------|--+
|                           |  |  |                          |  |
|  (no QuicBaaS installed)  |  |  |  (no QuicBaaS installed) |  |
+---------------------------+  |  +--------------------------+  |
                               v                                v
         +--------------------------------------------------+
         |  QuicBaaS Server (standalone or cluster)          |
         |  KV, Messaging, RAG, Secrets, Task Store          |
         +--------------------------------------------------+
```

#### Hook Configuration (Remote)

Point hook URLs to the remote QuicBaaS server:

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

#### Workspace Configuration (Remote Fleet Node)

A fleet node provisions every job workspace it runs under its own projects
root; the server never names a path on the agent's host.

```bash
quicbaas fleet serve ... \
  --work-dir /var/lib/quicbaas-agent \
  --projects-root /srv/quicbaas/projects   # optional; default <work-dir>/projects
```

The root is admitted for mounts tenant-scoped, as the server's own projects
root is, so it needs no `--allowed-mount-root` entry. It must survive
restarts: a job whose checkout vanished is refused (`WorkspaceStaleError`)
rather than re-cloned. See [sandbox.md § Agent-local workspaces](sandbox.md#agent-local-workspaces).

#### Channel Configuration (Remote Proxy)

The channel server runs locally on the agent machine as a stdio MCP process but connects to the remote QuicBaaS server for event sourcing:

```
Claude Code <--stdio--> ChannelServer <--gRPC/QUIC--> Remote QuicBaaS
```

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": [
        "channel", "serve",
        "--address", "quicbaas-server.internal:9090",
        "--enable-reply"
      ],
      "type": "stdio"
    }
  }
}
```

#### MCP Tools (Remote)

The MCP tool server also supports remote mode, connecting to QuicBaaS over gRPC/QUIC:

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve", "--address", "quicbaas-server.internal:9090"],
      "type": "stdio"
    }
  }
}
```

### Mode 3: Multi-Node Raft Cluster

Full distributed mode with Raft consensus across multiple QuicBaaS nodes. Agents connect to any node.

```
  Agent Machine 1          Agent Machine 2          Agent Machine N
  +------------+           +------------+           +------------+
  |Claude Code |           |Claude Code |           |Claude Code |
  | Hooks->HTTP|           | Hooks->HTTP|           | Hooks->HTTP|
  | MCP->gRPC  |           | MCP->QUIC  |           | MCP->REST  |
  | Channel<-  |           | Channel<-  |           | Channel<-  |
  +-----+------+           +-----+------+           +-----+------+
        |                        |                        |
        v                        v                        v
  +----------+  Raft  +----------+  Raft  +----------+
  |QuicBaaS  |<------>|QuicBaaS  |<------>|QuicBaaS  |
  |Node 1    |        |Node 2    |        |Node 3    |
  +----------+        +----------+        +----------+
```

#### Raft Guarantees

- **Task store** is replicated via Raft consensus. A task created on Node 1 is readable from Node 2 after commit.
- **Messaging** channels are cluster-wide. Events published on any node reach all subscribers.
- **RAG vectors** are stored in the replicated KV layer.
- **Session metadata** (active sessions, health state) is replicated for failover.

#### Network Partition Handling

During a network partition:

- Nodes in the majority partition continue serving reads and writes normally.
- Nodes in the minority partition reject writes and serve stale reads (if configured for read-only degradation).
- Hooks targeting a minority-partition node receive 503 responses. Claude Code retries or the agent's hook configuration should include multiple QuicBaaS endpoints.
- When the partition heals, Raft reconciles state automatically.

### Cross-Host Data Flow

A task created on Host A and executed on Host B:

1. **Host A**: Claude Code fires `TaskCreated` hook to QuicBaaS Node 1.
2. **Node 1**: Stores task in Raft-replicated task store (status: `pending`). Publishes event to messaging channel `orchestrator:tasks:<team>`.
3. **Node 2**: Messaging subscriber receives the event. Channel server on Host B pushes the event into Claude Code's session.
4. **Host B**: Claude Code agent picks up the task, begins work.
5. **Host B**: Claude Code fires `TaskCompleted` hook to QuicBaaS Node 2.
6. **Node 2**: Marks task `completed` in the replicated store. Publishes completion event.
7. **Host A**: Lead agent receives completion notification through its channel server.

### Shared RAG Across Hosts

RAG vectors are stored in the replicated KV/vector store. Any agent on any host can:

- Index files via `quicbaas_rag_index_project` -- vectors replicate across nodes.
- Query context via RAG-enabled prompts -- reads from the local node's replica.

This means an agent on Host B benefits from codebase indexing performed by an agent on Host A without re-indexing.

### Deployment Recommendations

| Scale | Mode | Nodes | Notes |
|---|---|---|---|
| Solo developer | Standalone | 1 | No network, minimal config |
| Small team, mixed hardware | Remote fleet nodes | 1 | Single QuicBaaS server, agents on any machine |
| Production, HA | Raft cluster | 3+ | Odd number of nodes for quorum |
| Multi-datacenter | Raft cluster | 5+ | Cross-region replication, latency-aware routing |


---

## MCP Channel Server

### Overview

The channel server pushes events into active Claude Code sessions. While hooks flow from Claude Code to QuicBaaS (inbound), channels flow from QuicBaaS to Claude Code (outbound). This enables real-time coordination: task assignments, status updates, and cross-agent messaging arrive in the agent's context without polling.

```
QuicBaaS Messaging ──> ChannelServer ──stdio──> Claude Code Session
                                     <──stdio── (reply tool)
```

Events appear as `<channel source="quicbaas">` tags inside the Claude Code session.

### Architecture

`ChannelServer` (`pkg/orchestration/channel_server.go`) implements the MCP protocol over stdio using JSON-RPC 2.0. Claude Code spawns it as a subprocess and communicates via newline-delimited JSON on stdin/stdout.

The server runs two concurrent loops:

- **Read loop** -- reads JSON-RPC requests from stdin (initialize handshake, tool calls, pings).
- **Drain loop** -- dequeues buffered `ChannelEvent` structs and writes them as `notifications/claude/channel` JSON-RPC notifications to stdout.

Events are buffered until the MCP initialization handshake completes. The default buffer size is 256 events.

### Channel Server Modes

#### Local Mode (in-process)

The channel server subscribes directly to QuicBaaS messaging channels in the same process. No network required.

```
Claude Code <--stdio--> ChannelServer <--in-process--> QuicBaaS Messaging
```

Set `QuicBaaSAddress` to empty in the config.

#### Remote Proxy Mode

The channel server runs as a local process on the agent machine but connects to a remote QuicBaaS server via gRPC/QUIC for event sourcing. Acts as a thin proxy between Claude Code and the remote QuicBaaS instance.

```
Claude Code <--stdio--> ChannelServer <--gRPC/QUIC--> Remote QuicBaaS Server
```

Set `QuicBaaSAddress` to the remote server address.

### Configuration

#### ChannelServerConfig

```go
type ChannelServerConfig struct {
    ServerName            string // MCP server name (channel source identifier)
    Instructions          string // Added to Claude's system prompt
    EnableReply           bool   // Enable the reply tool for two-way communication
    EnablePermissionRelay bool   // Enable remote permission approval/denial
    EventBufferSize       int    // Event channel buffer size (default: 256)
    QuicBaaSAddress       string // Remote server address (empty = in-process)
}
```

#### `.mcp.json`

Register the channel server as an MCP server in your project:

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": ["channel", "serve", "--server-name", "quicbaas", "--enable-reply"],
      "type": "stdio"
    }
  }
}
```

For remote proxy mode:

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": [
        "channel", "serve",
        "--server-name", "quicbaas",
        "--enable-reply",
        "--address", "quicbaas-server.internal:9090"
      ],
      "type": "stdio"
    }
  }
}
```

### CLI Command

```bash
quicbaas channel serve [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--server-name` | `quicbaas` | MCP server name (appears as channel source) |
| `--enable-reply` | `false` | Enable the reply tool for two-way communication |
| `--enable-permission-relay` | `false` | Enable remote permission approval/denial |
| `--buffer-size` | `256` | Event channel buffer size |
| `--address` | (empty) | Remote QuicBaaS address (empty = in-process) |
| `--instructions` | (empty) | Instructions added to Claude's system prompt |

### Event Types

Events pushed through the channel carry a content string and optional metadata:

```go
type ChannelEvent struct {
    Content  string            `json:"content"`
    Metadata map[string]string `json:"meta,omitempty"`
}
```

Common event categories:

| Event | Source | Description |
|---|---|---|
| Task assignment | Task store | A new task is available for this agent |
| Task status change | Messaging | Another agent completed/failed a task |
| Agent broadcast | Messaging | Cross-agent coordination message |
| RAG update | Vector store | New codebase context indexed |

Events are serialized as JSON-RPC notifications:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel",
  "params": {
    "content": "New task available: Implement WebSocket transport",
    "meta": {
      "task_id": "task_abc123",
      "team_name": "platform-team",
      "priority": "1"
    }
  }
}
```

### Two-Way Communication (Reply Tool)

When `EnableReply` is true, the channel server exposes a `reply` tool that Claude Code can invoke to send messages back through QuicBaaS messaging:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "reply",
    "arguments": {
      "channel": "orchestrator:tasks:platform-team",
      "message": "Task task_abc123 accepted, starting work."
    }
  }
}
```

The `ReplyHandler` callback processes replies. Set it before calling `Start`:

```go
srv.SetReplyHandler(func(ctx context.Context, channel string, message string) error {
    return messagingSvc.Publish(ctx, channel, []byte(message))
})
```

### MCP Initialization Handshake

The channel server follows the standard MCP initialization sequence:

1. Claude Code sends `initialize` request.
2. Server responds with capabilities (including `claude/channel` experimental capability).
3. Claude Code sends `notifications/initialized` notification.
4. Server begins draining buffered events as channel notifications.

Events queued before initialization completes are re-buffered and delivered once the handshake finishes.

### Lifecycle

- `NewChannelServer(config, reader, writer, logger)` -- creates the server.
- `SetReplyHandler(handler)` -- registers the reply callback (before Start).
- `Start(ctx)` -- blocks, running read and drain loops until context cancellation or Stop.
- `PushEvent(ctx, event)` -- queues an event for delivery.
- `Stop()` -- gracefully shuts down (idempotent).
- `Initialized()` -- reports whether the MCP handshake completed.


---

## Orchestration Persistence

Projects, agent registrations and tasks are durable KV-backed records. They
survive a daemon restart, stay isolated per tenant by key construction, are
encrypted at rest by the owning tenant's barrier, and emit an audit event on
every create, update and delete.

### One implementation, two backings

There is exactly one implementation of each store. Whether state is durable is
decided solely by which `interfaces.KVStore` is injected:

```go
// Durable: qrdb-backed, replicated, barrier-encrypted.
store := orchestration.NewProjectStore(server.durableKVStore())

// Ephemeral: qrdb's in-memory engine, for unit tests and embedded callers.
store := orchestration.NewInMemoryProjectStore() // == NewProjectStore(kvstore.NewMemory())
```

`pkg/kvstore` is the single bridge from a qrdb storage engine
(`qrdbsdk.Storage`) to `interfaces.KVStore`. `kvstore.NewMemory()` wraps
`qrdbsdk.NewMemoryStorage()`, qrdb's ordered B-tree engine, so tests observe the
same lexical scan ordering the durable engines produce. QuicBaaS does not
implement a storage engine of its own.

| Constructor | Backing |
| --- | --- |
| `NewProjectStore(kv, opts...)` | caller's KV |
| `NewFleetRegistry(kv, opts...)` | caller's KV |
| `NewTaskStore(kv, opts...)` | caller's KV |
| `NewAgentStore(kv, opts...)` | caller's KV |
| `NewInMemoryProjectStore()` | `kvstore.NewMemory()` |
| `NewInMemoryFleetRegistry()` | `kvstore.NewMemory()` |
| `NewInMemoryTaskStore()` | `kvstore.NewMemory()` |
| `NewInMemoryAgentStore()` | `kvstore.NewMemory()` |
| `NewInMemoryTaskStoreWithKV(kv, limit)` | caller's KV, custom inline limit |

A store constructed with a nil KV fails closed on every verb. A nil KV is a
wiring bug, not a storage mode.

### Key layout

Every record's key puts the owning tenant in the first variable segment:

```
orch/project/{tenant}/{projectID}            -> Project (JSON)
orch/projectname/{tenant}/{name}             -> projectID (name index)
orch/agent/{tenant}/{agentID}                -> FleetNode (JSON)
orch/task/{tenant}/{taskID}                  -> Task (JSON)
orch/taskartifact/{tenant}/{taskID}/{name}   -> artifact payload (bytes)
orch/session/{tenant}/{sessionID}            -> SessionRecord (JSON)
```

Keys carry identifiers only — no secret, token or payload value is ever encoded
into a key, so keys stay safe to log and to expose in operator tooling.

### Project-name uniqueness

The name-index ordering and its invariants are in
[projects/architecture.md](../projects/architecture.md#name-uniqueness).

### Tenant isolation

Cross-tenant access is prevented by construction, not by convention:

1. Every method builds its key or scan prefix from the caller-supplied
   `tenantID`. No method accepts a bare entity ID.
2. A list is a prefix scan bounded to `orch/{entity}/{tenant}/`, so it cannot
   observe another tenant's subtree. `pkg/kvstore` implements `Scan` as a
   bounded iterator range rather than a filtered full-keyspace walk.
3. Identifiers that are empty or contain the `/` separator are rejected
   (`ErrInvalidKeySegment`). Without this a crafted ID such as
   `other-tenant/x` could escape its tenant's subtree.
4. After decoding a record the store re-checks `record.TenantID == tenantID`
   and treats a mismatch as not-found. This is defence in depth against key
   corruption, not the primary control.
5. A task artifact reference is dereferenced only when it points inside that
   task's own artifact prefix, so a caller-supplied `Ref.Path` cannot be turned
   into an arbitrary read of the shared keyspace.
6. The **context** tenant, not the key's tenant segment, is what the routing
   store beneath these keys selects a namespace by (see
   [Encryption at rest](#encryption-at-rest)). The tenant segment is a label
   the router never reads. A verb that names tenant T while its context is
   scoped to another tenant would therefore file T's record in the wrong
   namespace — the state go-quicbaas#535 measured: a pooled account tagged
   `tenant_id=automatethethings`, physically in the system namespace,
   listable by the admin who created it and invisible to the tenant's SDLC
   module. Two controls close it. `service.AccountService` scopes every
   request's context to the request's tenant on **both** keys the router
   honours (`accountTenantContext`), so the system admin provisioning a
   tenant's account writes into that tenant's namespace. And
   `AccountStore` refuses any verb whose context already carries a
   *different* tenant than the request names (`AccountTenantScopeError`),
   so a mis-scoped caller fails loudly instead of reading or writing the
   wrong namespace. An unscoped context is served — in-process embedded
   callers and tests carry none.

The store layer's own tests assert these properties directly, including the
case where one tenant ID is a lexical prefix of another (`t1` and `t10`), and
`pkg/service`'s tenant-namespace tests cross the boundary end to end: an
account created through the service under a system context is selected by
`AccountResolver` under the tenant's own context.

### Encryption at rest

The stores hold no crypto path of their own. Confidentiality comes from qrdb's
`StorageBarrier`, which encrypts everything written through the node's store.

The server binds them to `Server.durableKVStore()`
(`pkg/server/tenant_kvstore.go`), the same seam the IAM stores use. When the
embedded qrdb node exposes a per-tenant `StoreResolver`, that returns a
`tenantRoutingKVStore`: each operation routes to the context tenant's backend,
so a barrier-isolated tenant's orchestration records live inside that tenant's
own barrier with its own DEK and namespace. A sealed barrier tenant **fails
closed** — the operation errors and never falls back to the shared store.

Every qrdb node exposes a resolver since go-qrdb v0.10.0 (#41) — a
seal-disabled node routes through a confined `"{tenant}/"` view of the shared
store rather than a barrier — so the fallback to the raw platform shard is
reached only on a storage-less boot, where the tenant-first key layout still
keeps records separated. Anything reading IAM records from outside the IAM
components goes through `Server.IAMKVStore()`, which is the same seam.

The routing store reads the request tenant from either the IAM or the
orchestration context key, because the two packages deliberately keep separate
unexported keys carrying the same value.

### Audit

Every create, update and delete emits one `orchestration.StoreAuditEvent`
through the `StoreAuditor` sink. The server binds that sink to the platform
`iam.AuditLogger` (`pkg/server/orchestration_store_auditor.go`), so
orchestration mutations land on the same tamper-evident trail as IAM changes —
what FedRAMP AU-2/AU-3, PCI-DSS 10.2 and HIPAA 164.312(b) each require.

- Rejected mutations are recorded with `Success: false`; a denied change is
  itself an auditable event.
- Reads are not audited.
- Agent heartbeats are not audited — they are high-frequency telemetry, not a
  state change.
- Events carry identifiers only, never record contents, so the trail never
  becomes a second copy of the data.
- The logger is resolved per event rather than captured at construction,
  because the stores are built before IAM initialization publishes it.

### Concurrency

`interfaces.KVStore` exposes no compare-and-swap verb, and neither does qrdb's
re-exported `kvstore.KVStore`. Atomic transitions therefore use two mechanisms
together:

- a `keyedMutex` lock held across the whole read-modify-write, which makes the
  update atomic against other writers **in this process**; and
- the record's `Version` counter, which **detects** a writer that raced from
  another process or Raft node and surfaces `ErrTaskVersionConflict`.

Cross-record writes stay parallel. A true multi-writer CAS needs a
compare-and-swap verb that qrdb's SDK does not currently re-export; when it
does, the read-modify-write is replaced by it and `Version` keeps its meaning.

### Still in memory

Two orchestration components remain process-local and reset on restart:

- the job KV store (`pkg/server/lifecycle_orchestration.go`), which also backs
  the session event log — so `RecoverJobs` runs against a store that is empty
  by construction at boot; and
- the agent session store (`orchestration.NewInMemoryAgentStore`).

Both are separate from the project, agent-registry and task state described
here.

The session event log is the full live transcript of a turn: backends stream
`assistant_chunk`, `agent_thought` (chain-of-thought), `tool_invocation`, and
`tool_result` events into it as they happen, rather than recording only the
prompt and the final response.


### Session records

`AgentStore` (`NewAgentStore`) persists the durable description of an agent
session: enough to address it after a restart and, for a persistent session, to
reconstitute the agent process that served it. See
session-resume.md.

`ListAllSessions` is the one cross-tenant scan in the orchestration stores. It
exists for boot-time restoration, which runs before any tenant context is
available; every request-scoped caller uses the tenant-scoped `ListSessions`.


---

## Agent Identity and IAM Integration

Orchestration agents do not currently have the service-account provisioning lifecycle this section used to describe. The planned lifecycle is not implemented: `provisionAgentIdentity()` and `revokeAgentIdentity()` do not exist, and `OrchestratorAuthService.CreateServiceAccount()` has no production caller. Keep any future wiring tied to the SDK boundary decision for IAM and credential verification; do not treat the planned service-account flow as current behavior.

### What Fills `AgentIdentity` Today

`AgentIdentity` is populated by two different mechanisms today:

1. **Fleet daemon enrollment** -- `pkg/fleet.ProductionEnroller.buildIdentity` builds an `AgentIdentity` from persisted enrollment credentials and the token exchange that follows. It fills `AgentID`, `ServiceAccountID`, `TenantID`, `Role`, `Scopes`, `ClientID`, and `ClientSecret`; `OIDCSubject` remains empty. Those credentials come from `POST /api/v1/fleet/enroll`, where the REST handler registers or rotates an OIDC application with `pkg/iam.AppManager.RegisterAgentApp`. The live `client_credentials` flow validates the presented secret through `pkg/iam` OIDC application storage (`ValidateClientSecret`) against the OIDC application store, not through `OrchestratorAuthService` service accounts.
2. **Orchestration control loop attribution** -- `pkg/orchestration.ControlLoop.handleAssign` creates a role-keyed in-memory `AgentIdentity` when a job assigns work. It fills `AgentID` as `agent-<role>-<jobID>`, plus `TenantID`, `Role`, and `Scopes` from the workflow role. It leaves `ServiceAccountID`, `OIDCSubject`, `ClientID`, and `ClientSecret` empty. The control loop uses that record only for `turnAttribution`, so token usage is charged to a stable agent ID.

Spawned session authorization does not come from the control-loop `AgentIdentity`. It comes from the qrdb user record written by `pkg/iam.IdentityStore.EnrollAgentSessionIdentity` for the session certificate, with qrdb's canonical SPKI thumbprint binding and `AgentSessionGrant()` permissions.

### Planned Service-Account Lifecycle (Not Implemented)

The intended service-account lifecycle remains design intent only:

| Phase | Planned action | Current status |
|-------|----------------|----------------|
| Provision | `provisionAgentIdentity()` creates an IAM service account through `OrchestratorAuthService.CreateServiceAccount()` | Not implemented; no such orchestration entry point exists |
| Scope assignment | The service account receives role scopes from workflow YAML `mcp_scopes` | Not implemented for service accounts; the control loop only copies scopes into its in-memory attribution record |
| Token issuance | A short-lived OIDC token is issued through `client_credentials` for that service account | Not implemented for service accounts; current `client_credentials` validates OIDC applications through `ValidateClientSecret` |
| Session binding | The token is injected into the agent session context for MCP tool calls | Not implemented |
| Cleanup | `revokeAgentIdentity()` revokes the token and deletes the service account | Not implemented; no such orchestration entry point exists |

### Client Credential Storage

The dormant `OrchestratorAuthService` service-account path stores client secrets as PBKDF2-HMAC-SHA256 digests. If that path is wired in the future, `CreateServiceAccount` and `RotateCredentials` return the client secret to the caller exactly once; what the store keeps in `ServiceAccount.SecretHash` yields no usable credential if compromised.
The stored form is self-describing:

```
$pbkdf2-sha256$i=<iterations>$<base64 salt>$<base64 derived key>
```

Exactly one work factor is accepted, and a digest carrying any other iteration
count is refused. That is a security property, not a simplification: an
ineligible account is verified against a fixed-cost equalizer digest, so a
record holding a cheaper work factor would answer measurably faster than an
unknown client ID and disclose that it exists, while a dearer one would let a
tampered record decide how long a request worker is occupied.

**Raising the work factor therefore does not migrate existing secrets.** Digests
at the previous count stop parsing, so they are treated as legacy and fail
closed. The procedure is mandatory and deliberate:

1. Raise `pbkdf2Iterations` and regenerate `timingEqualizerDigest` to match —
   `TestTimingEqualizerDigest_MatchesCurrentParameters` fails until you do.
2. Run `InvalidateLegacySecretHashes(ctx, tenantID)` for each tenant. Every
   account written under the old count now reads as legacy and is cleared.
3. Reissue with `RotateCredentials(ctx, accountID)` and distribute the new
   secrets.

Between steps 1 and 3 the affected service accounts cannot authenticate. This
is cheap in practice because service accounts are short-lived machine
identities the orchestrator creates and deletes per job, but it is a real
outage for any long-lived account and has to be scheduled rather than
discovered.

PBKDF2 is also the only choice here that satisfies the repository's own
compliance gate: CI runs under `GODEBUG=fips140=only`, and PBKDF2-HMAC-SHA256
is approved under SP 800-132. The derivation uses the standard library's
`crypto/pbkdf2`, which is served by the Go Cryptographic Module.

Within the planned service-account path, `OrchestratorAuthService.AuthenticateServiceAccount(ctx, clientID, clientSecret)` is the verifier for a presented pair. It fails closed and is non-enumerating: an unknown client ID, a revoked or expired account, a legacy or cleared hash, and a wrong secret all return the same detail-free `*ServiceAccountAuthFailedError` and all perform exactly one key derivation, so neither the error nor the time taken reveals which client IDs exist. The specific reason goes only to the audit log. This is separate from today's OIDC application `client_credentials` path, which validates through `ValidateClientSecret`.

#### Migrating pre-KDF records

Service accounts written before the KDF landed stored the hex-encoded client
secret in `SecretHash`, which is a recoverable credential rather than a digest.
Those records are treated as compromised, never migrated in place:

1. `AuthenticateServiceAccount` rejects any account whose stored hash does not
   parse as a digest, and logs a warning naming the account.
2. `InvalidateLegacySecretHashes(ctx, tenantID)` clears every legacy hash in a
   tenant — revoked accounts included, since their stored secret is just as
   readable — and reports how many it cleared. The accounts survive; their
   credentials stop working.
3. `RotateCredentials(ctx, accountID)` issues a fresh secret, hashed with the
   current KDF, and hands the plaintext back to the operator once.

### Scope-Based MCP Permissions

Each agent role specifies `mcp_scopes` in the workflow YAML. These scopes follow the `resource:action` format:

```yaml
mcp_scopes: [orchestrator:read, kms:read, secrets:read]
```

The MCP server filters the tools/list response based on the agent's scopes. An agent only sees tools it is authorized to use. See MCP Permissions for details.

### Permission Matrix by Role

| Role | Typical Scopes | Purpose |
|------|---------------|---------|
| lead | `orchestrator:read`, `orchestrator:write`, `agent:spawn`, `agent:prompt`, `agent:close` | Full orchestration control, manages other agents |
| coder | `kms:read`, `secrets:read`, `orchestrator:read` | Read secrets and keys for development, read job status |
| reviewer | `orchestrator:read` | Read-only access to review task status and artifacts |

### Defining Custom Scopes in Workflow YAML

Scopes are defined per agent role in the workflow definition:

```yaml
agents:
  - role: lead
    backend: claude
    mcp_scopes:
      - orchestrator:read
      - orchestrator:write
      - agent:spawn
      - agent:prompt
      - agent:close
  - role: coder
    backend: claude
    mcp_scopes:
      - kms:read
      - secrets:read
      - orchestrator:read
  - role: auditor
    backend: ollama
    mcp_scopes:
      - orchestrator:read
      - audit:read
```

The `resource:action` format accepts any valid resource and action combination. The IAM system validates scopes against the registered resource types during service account creation.


---

## Agent Client Protocol (ACP)

ACP is an open standard ([agentclientprotocol.com](https://agentclientprotocol.com)) for communicating with AI coding agents. It defines a JSON-RPC 2.0 based protocol for session management and prompt execution over stdin/stdout.

go-quicbaas implements an ACP client through the `ACPBackend` and `StdioTransport` types in `pkg/orchestration` (`acp_backend.go` and `transport.go`).

### Architecture

```
QuicBaaS Server
    |
    v
ACPBackend (session management)
    |
    v
StdioTransport (JSON-RPC over stdio)
    |
    v
Agent subprocess (Claude Code, Codex, etc.)
```

The `ACPBackend` manages sessions as a `sync.Map` of `acpSession` structs. Each session owns a `StdioTransport` connected to a single agent subprocess.

### StdioTransport

`StdioTransport` manages bidirectional JSON-RPC communication with an agent subprocess.

**Wire format:** Newline-delimited JSON. Each message is a single JSON object terminated by `\n`.

**Concurrency model:**
- Writes to stdin are mutex-protected for safe concurrent use.
- An atomic counter (`nextID`) assigns unique request IDs.
- A `sync.Map` of pending channels dispatches responses to the correct caller.
- A background `readLoop` goroutine reads stdout and routes messages by ID.

**Key methods:**

| Method | Description |
|--------|-------------|
| `Send(ctx, method, params)` | Sends a request and blocks until the matching response arrives or context is cancelled. |
| `Notify(method, params)` | Sends a fire-and-forget notification (no ID, no response). |
| `Close()` | Closes stdin, sends SIGTERM, waits 5 seconds, then SIGKILL if needed. |
| `PID()` | Returns the subprocess PID. |

### Session Lifecycle

#### 1. Initialize

The first message sent after subprocess start. Establishes the ACP connection.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "client_info": {
      "name": "go-quicbaas",
      "version": "1.0.0"
    }
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "server_info": {
      "name": "claude-code",
      "version": "1.0.0"
    },
    "capabilities": {},
    "instructions": ""
  }
}
```

#### 2. Session New

Creates a named session with optional system prompt, model, MCP servers, and working directory.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/new",
  "params": {
    "system_prompt": "You are a Go backend engineer.",
    "model": "claude-sonnet-4-20250514",
    "work_dir": "/home/user/project",
    "mcp_servers": [
      {
        "name": "quicbaas",
        "command": "quicbaas",
        "args": ["mcp", "serve"]
      }
    ]
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "session_id": "sess_abc123"
  }
}
```

#### 3. Session Prompt

Sends a prompt to an existing session. The agent processes the prompt and returns content blocks.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "session_id": "sess_abc123",
    "prompt": "Add unit tests for the UserService."
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "session_id": "sess_abc123",
    "content": [
      {"type": "text", "text": "I've added tests in user_service_test.go..."}
    ],
    "model": "claude-sonnet-4-20250514",
    "stop_reason": "end_turn"
  }
}
```

#### 4. Session Close

Terminates a session. Sent as a notification (no response expected).

```json
{
  "jsonrpc": "2.0",
  "method": "session/close",
  "params": {
    "session_id": "sess_abc123"
  }
}
```

After the notification, the transport is closed (stdin EOF, SIGTERM, then SIGKILL after 5s timeout).

### Turn Contract

`PromptSession` is **synchronous**: the call returns only after the agent has
finished its turn. There is no deferred delivery — a successful response always
carries a completed turn, and a failure is an error, never an empty response.

#### Service wire shape

The orchestrator service answers with a structured turn on every transport:

```json
{
  "session_id": "ce4b6a40-...",
  "content": [{"type": "text", "text": "SELF-HOSTING ONLINE"}],
  "model": "claude-fable-5",
  "stop_reason": "end_turn",
  "timestamp": 1786207030175886792
}
```

#### SDK view

The SDK exposes the flattened `orchestrator.PromptSessionResponse`
(`response`, `done`, `model`, `stop_reason`). The two shapes share only
`session_id`, so **every transport must convert** through
`orchestrator.NewPromptSessionResponse` / the `*Wire` types in
`sdk/go/client/orchestrator/session_wire.go` — decoding the service payload
straight into the SDK type silently produces an empty `done: false`.

- `done` reports that the agent's turn completed. Because the call is
  synchronous it is true on every successful response.
- A response with neither content nor a stop reason is not a turn and yields
  `*orchestrator.EmptyTurnError`.
- The same rule applies to the sibling session responses: the service names the
  lifecycle field `state` (the SDK exposes it as `status`) and sends no `count`
  for `ListSessions`, both of which the `*Wire` converters map.

#### Stop reasons across dialects

| Backend dialect | Turn terminator | Stop reason on the wire |
|-----------------|-----------------|-------------------------|
| ACP JSON-RPC (opencode, codex) | `session/prompt` result | Reported by the agent |
| stream-json CLI (claude) | `{"type":"result","subtype":"success"}` | `stop_reason` is `null` on the assistant message |
| One-shot CLI (`--print`) | Process exit | Not reported |

The stream-json and one-shot paths normalise a completed turn to
`orchestration.StopReasonEndTurn` so every backend describes completion
identically. An agent-reported stop reason (for example `max_tokens`) is always
preserved; the CLI prints it whenever it is not an ordinary end of turn.

### Protocol Types

#### Core Message Types

| Type | Description |
|------|-------------|
| `JSONRPCMessage` | Base JSON-RPC 2.0 envelope with `jsonrpc`, `id`, `method`, `params`, `result`, `error`. |
| `JSONRPCError` | Error object with `code`, `message`, `data`. |
| `InitializeParams` | Client info sent during connection init. |
| `InitializeResult` | Server info and capabilities returned from init. |
| `SessionNewParams` | System prompt, model, MCP servers, work dir. |
| `SessionNewResult` | Contains the assigned `session_id`. |
| `SessionPromptParams` | Session ID and prompt text. |
| `SessionPromptResult` | Content blocks, model, stop reason. |
| `SessionCloseParams` | Session ID to close. |

#### Event Types

| Type | Description |
|------|-------------|
| `SessionUpdate` | Event from agent during prompt execution. Contains content or a tool call. |
| `ToolCall` | Agent requesting tool execution: `id`, `name`, `arguments`. |
| `ToolResult` | Response sent back to agent: `tool_call_id`, `content`, `is_error`. |
| `ContentBlock` | A unit of response content with `type` (e.g., `"text"`) and `text`. |

### Callback Handling

During prompt execution, agents may issue callbacks requesting file system operations or permissions. The `CallbackHandler` processes these.

#### Supported Callbacks

| Callback | Arguments | Description |
|----------|-----------|-------------|
| `fs/read_text_file` | `path` (string) | Read a file and return its contents. |
| `fs/write_text_file` | `path` (string), `content` (string) | Write content to a file. |
| `session/request_permission` | `path` (string, optional) | Request permission for an operation. Auto-approved if path is within allowed directories. |

#### IAM Enforcement

The `CallbackHandler` enforces path-based access control:

1. Allowed paths are configured at handler creation via `NewCallbackHandler(allowedPaths)`.
2. All paths are resolved through `filepath.EvalSymlinks` to prevent symlink traversal.
3. `filepath.Rel` is used to verify the target path does not escape allowed directories (no `..` prefix).
4. For files that do not yet exist (write operations), the parent directory is resolved instead.

Unauthorized access returns an error result: `"access denied: path outside allowed directories"`.

```go
handler := acp.NewCallbackHandler([]string{
    "/home/user/projects/myapp",
    "/tmp/agent-workspace",
})

result, err := handler.HandleCallback(ctx, toolCall)
```

### Transport Details

- **Protocol:** JSON-RPC 2.0 over newline-delimited JSON on stdin/stdout.
- **Stderr:** Discarded (piped to `io.Discard`) to prevent blocking.
- **ID generation:** Atomic int64 counter, monotonically increasing.
- **ID matching:** JSON numbers unmarshal as `float64`; the transport handles both `float64` and `int64` ID types.
- **Pending requests:** Stored in a `sync.Map` keyed by request ID, with buffered channels (capacity 1) for responses.
- **Shutdown sequence:** Close stdin, close done channel, drain pending channels, SIGTERM, wait 5s, SIGKILL.
- **Environment:** Subprocess inherits `os.Environ()` plus any additional env vars from the spawn request.
