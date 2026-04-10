# MCP Tools Reference

MCP tool naming conventions and complete reference for orchestration tools.

## Naming Convention

QuicBaaS exposes two MCP tool namespaces:

| Prefix | Scope | Description |
|--------|-------|-------------|
| `quicbaas_agent_*` | Session/Fleet management | Existing tools for agent lifecycle, project discovery, session management |
| `quicbaas_orch_*` | Orchestration coordination | New tools for inter-agent coordination, file locking, cost tracking |

The `quicbaas_agent_*` prefix refers to operations on the QuicBaaS agent infrastructure (the binary running on remote systems). The `quicbaas_orch_*` prefix refers to operations that orchestrated AI agents (Claude Code, Codex, OpenCode) use to coordinate with each other.

## Existing quicbaas_agent_* Tools

| Tool | Description |
|------|-------------|
| `quicbaas_agent_discover` | Scan directory tree for agent-enabled projects |
| `quicbaas_agent_register` | Register a project for orchestration |
| `quicbaas_agent_list` | List registered projects (paginated) |
| `quicbaas_agent_spawn` | Create a persistent session on a backend |
| `quicbaas_agent_prompt` | Send a prompt to a session or backend |
| `quicbaas_agent_status` | Query session state, uptime, last activity |
| `quicbaas_agent_close` | Terminate an active session |
| `quicbaas_agent_list_sessions` | List active sessions (filterable by backend) |
| `quicbaas_agent_list_backends` | List registered backends and capabilities |
| `quicbaas_agent_decompose` | Decompose a task into prioritized work items |
| `quicbaas_agent_health` | Check health of all active sessions |
| `quicbaas_agent_broadcast` | Publish a message to a coordination channel |
| `quicbaas_agent_cache_put` | Store analysis result in KV (project-scoped) |
| `quicbaas_agent_cache_get` | Retrieve cached result by project and key |

## New quicbaas_orch_* Tools

### Peer Messaging

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox | `to`, `action`, `content`, `channel` (optional), `metadata` (optional) |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages | `since` (optional, unix nanos) |

### File Locking

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path | `file_path`, `project_id` |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock | `lock_key` |
| `quicbaas_orch_file_lock_status` | Check lock status for a file or list active locks | `file_path` (optional), `project_id` |

### Observability (Metrics)

These are exposed as Prometheus metrics, not MCP tools, but are relevant to orchestration observability:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_orch_peer_messages_total` | Counter | Total peer messages sent |
| `quicbaas_orch_peer_inbox_size` | Gauge | Current inbox size per agent |
| `quicbaas_orch_file_locks_active` | Gauge | Number of active file locks |
| `quicbaas_orch_file_lock_wait_duration_seconds` | Histogram | Time spent waiting for lock release |
| `quicbaas_orch_agent_tokens_total` | Counter | Total tokens consumed per agent |
| `quicbaas_orch_job_budget_utilization` | Gauge | Budget utilization percentage per job |
| `quicbaas_orch_agent_restarts_total` | Counter | Total agent restarts |
