# MCP Tools Reference

MCP tool naming conventions and the complete registered inventory of
orchestration tools with their authorization mappings. Request and response
schemas live in [orchestration/mcp-tools.md](../orchestration/mcp-tools.md).

Registration sites: `pkg/api/mcp/orchestrator_tools.go` (27),
`orchestrator_cache_tools.go` (2), `account_tools.go` (9),
`project_tools.go` (5), `agent_admin_tools.go` (4) — 47 orchestration
tools in total, all behind the `mcp` build tag.

## Naming Convention

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

## quicbaas_orchestrator_* Tools

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

## quicbaas_agent_fleet_* Tools

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

## Planned: `quicbaas_orch_*` coordination tools

**Not implemented.** No `quicbaas_orch_*` tool is registered anywhere in
`pkg/api/mcp` — the only `quicbaas_orch_*` identifiers that exist in the
codebase are Prometheus metric names in `pkg/metrics/metrics.go` (listed
below). The tools in this section are a design sketch for a namespace that
orchestrated AI agents (Claude Code, Codex, OpenCode) would use to coordinate
with each other. Treat them as planned, not callable.

### Peer messaging (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox | `to`, `action`, `content`, `channel` (optional), `metadata` (optional) |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages | `since` (optional, unix nanos) |

### File locking (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path | `file_path`, `project_id` |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock | `lock_key` |
| `quicbaas_orch_file_lock_status` | Check lock status for a file or list active locks | `file_path` (optional), `project_id` |

## Observability metrics

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
