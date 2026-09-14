# Software Factory — CLI

> **Status: NORMATIVE** · Last verified: 2026-09-10
> Every verb and flag of the Software Factory command groups, with runnable
> examples: `quicbaas orchestrator`, `quicbaas agent`,
> `quicbaas agent account`, `quicbaas project`, `quicbaas sdlc` and the
> planned `quicbaas prompts`. Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

Complete reference for QuicBaaS orchestration commands. All commands use the SDK client with multi-protocol support (gRPC, REST, QUIC, Unix, MCP).

## Output Formats

All commands support `--output` flag with values: `table` (default), `json`, `yaml`.

```bash
quicbaas orchestrator <command> --output json
quicbaas agent <command> --output yaml
```

## Global Flags

- `--tenant <tenant>` - Tenant ID (default: "default")
- `--output <format>` - Output format: json, yaml, table (default: table)
- `--timeout <duration>` - Command timeout (default: 30s)

---

## Orchestration Jobs

### orchestrator run

Start an autonomous orchestration control loop. Decomposes a task, schedules work items, assigns agents, executes work, and reviews results. The control loop runs server-side and jobs continue running independently of the CLI -- disconnecting or timing out does not cancel the job.

The CLI auto-subscribes to real-time event streaming via `SubscribeJobEvents`. Events (state transitions, task progress, agent output) are displayed as they occur. If the event stream is unavailable, the CLI falls back to polling at `--poll-interval`.

```bash
quicbaas orchestrator run "Build a tic-tac-toe game"
quicbaas orchestrator run "Implement user authentication" --backend claude
quicbaas orchestrator run "Add caching layer" --backend ollama --model llama3
quicbaas orchestrator run "Build the REST API" --project proj_123 --max-iterations 5
quicbaas orchestrator run "Build a game" --workflow software-dev
quicbaas orchestrator run "Custom task" --workflow-file ./my-workflow.yaml
```

Arguments:
- `<description>` - Task description (required)

Flags:
- `--backend <name>` - Lead agent backend name
- `--model <model>` - Lead agent model override
- `--project <id>` - Project ID to scope the job
- `--workflow <name>` - Use a built-in workflow by name (e.g., software-dev)
- `--workflow-file <path>` - Path to a custom workflow YAML file
- `--max-iterations <int>` - Maximum re-delegation iterations (default: 10)
- `--poll-interval <duration>` - Interval between status polls (default: 5s)

Press Ctrl+C to cancel a running job.

### orchestrator job status

Get the current status of an orchestration job.

```bash
quicbaas orchestrator job status job_abc123
quicbaas orchestrator job status job_abc123 --output json
```

Arguments:
- `<jobID>` - Job ID to check (required)

### orchestrator job list

List the tenant's orchestration jobs, newest first.

```bash
quicbaas orchestrator job list
quicbaas orchestrator job list --task 8bf2fb5f30e2f0c87223ed219d1418c7
quicbaas orchestrator job list --output json
```

Flags:
- `--task <id>` - Narrow the listing to the attempts at one task — every job
  that ran it, concluded ones included. That is the task's attempt history.
  The server applies the narrowing; see
  [job-admission.md](job-admission.md#the-attempt-history-a-task-can-be-read-back-through).

### orchestrator job cancel

Cancel a running orchestration job.

```bash
quicbaas orchestrator job cancel job_abc123
```

Arguments:
- `<jobID>` - Job ID to cancel (required)

---

## Workflow Management

### orchestrator workflow list

List all available orchestration workflow definitions. Workflows are loaded from the local `./workflows/` directory.

```bash
quicbaas orchestrator workflow list
quicbaas orchestrator workflow list --output json
```

### orchestrator workflow show

Display the full definition of a workflow by name.

```bash
quicbaas orchestrator workflow show software-dev
quicbaas orchestrator workflow show code-review --output json
quicbaas orchestrator workflow show autonomous --output yaml
```

Arguments:
- `<name>` - Workflow name (required)

---

## Health & Backends

### orchestrator health

Check orchestrator health and all active sessions.

```bash
quicbaas orchestrator health
quicbaas orchestrator health --output json
```

Response (table format):
```
Healthy:        true
Status:         ready
Timestamp:      2026-03-31T14:22:00Z

COMPONENT       STATUS
sessions        healthy
backends        healthy
```

### orchestrator backends

List available agent backends and their capabilities.

```bash
quicbaas orchestrator backends
quicbaas orchestrator backends --output json
```

Response (table format):
```
NAME        TYPE        KIND            STATUS      AVAILABLE
claude      acp         agent           running     true
codex       acp         agent           running     true
opencode    acp         agent           running     true
ollama      ollama      model_server    running     true

Total: 4 backends
```

---

## Session Management

### orchestrator spawn

Create a persistent agent session on a specified backend.

```bash
quicbaas orchestrator spawn --backend claude
quicbaas orchestrator spawn --backend ollama --model llama3
quicbaas orchestrator spawn --backend claude --project proj_123
quicbaas orchestrator spawn --backend claude --system-prompt "You are a Go expert"
```

Flags:
- `--backend <name>` (required) - Backend name (e.g., "claude", "ollama")
- `--model <model>` - Model identifier (overrides backend default)
- `--system-prompt <text>` - Custom system prompt
- `--project <id>` - Project ID to associate with session
- `--sandbox <mode>` - `default` (server policy) | `required` | `none`
- `--host-affinity <name|host>` - Pin the session to one agent daemon
- `--account-id <id>` - Pin the session to a pooled provider account. A bare
  pin is deliberate: the session is never rotated off it.
- `--account-auto` - Select a pooled account with the configured strategy and
  authorize rotation across the whole tenant/project/provider pool
- `--account-strategy <name>` - `static` | `least_usage`. Empty selects the
  registry default. Ignored unless `--account-auto` is set.
- `--account-allow <id>` - Restrict the session to these pooled account IDs and
  authorize it to rotate between them. Repeatable (or comma-separated).

Pooled accounts:
```bash
# Strategy-picked seat, rotatable across the whole pool.
quicbaas orchestrator spawn --backend claude --account-auto --account-strategy least_usage

# Start on a named seat, rotatable only within the declared pool.
quicbaas orchestrator spawn --backend claude \
  --account-id acct_a --account-allow acct_a --account-allow acct_b
```

Response (table format):
```
Session ID:     sess_abc123def456
Backend:        claude
Container ID:   container_xyz789
```

### orchestrator rotate-account

Move a running session onto a different pooled provider account.

The session keeps its identifier, transcript and cost history; the replacement
account opens a new attribution segment, so no account is billed for another's
turns. QuicBaaS also rotates automatically when a turn fails on a classified
provider limit — this is the operator's door to the same verb.

```bash
quicbaas orchestrator rotate-account sess_abc123
quicbaas orchestrator rotate-account sess_abc123 --reason "seat retired" --hold-outgoing
```

Arguments:
- `<sessionID>` - Session to move

Flags:
- `--reason <text>` - Recorded on the closed account segment and in the
  session transcript
- `--hold-outgoing` - Hold the outgoing account out of the pool for the
  server's default backoff so the next selection does not pick it again

Only a session spawned with `--account-auto` or `--account-allow` may rotate;
a bare `--account-id` pin is deliberate and is honored with a conflict error.

Response (table format):
```
Session ID:        sess_abc123
Previous Account:  acct_a
Account:           acct_b
Strategy:          least_usage
Mode:              resume
```

`Mode` is `in_place` when the credential was swapped with no process restart
(one-shot CLI sessions) or `resume` when the agent process was reconstituted
against the replacement and the transcript replayed.

### orchestrator prompt

Send a prompt to an active agent session and stream the agent's response.

The turn itself is asynchronous — the server accepts it and runs it in the
background, because a real agent turn outlasts any request timeout. The CLI then
attaches to the session's event stream so the command still reads as a
synchronous conversation. See Asynchronous Agent Turns.

```bash
quicbaas orchestrator prompt sess_abc123 "Write unit tests for auth handler"
quicbaas orchestrator prompt sess_abc123 "Refactor database layer" --follow=false --output json
```

Arguments:
- `<sessionID>` - Session ID from spawn command
- `<text>` - Prompt text (required)

Flags:
- `--follow` - Stream the turn's events until it completes (default `true`).
  `--follow=false` prints the acknowledgement and exits, which is what scripts
  and CI jobs usually want.

Interrupting the follow (ctrl-c) detaches the CLI only; the turn keeps running
on the server. Reattach with `quicbaas orchestrator attach <sessionID> --from <sequence>`.
A turn that fails exits non-zero with the agent's own reason.

Response with `--follow=false` (table format):
```
Session ID:     sess_abc123
Turn ID:        9f3c1e40-...
Status:         accepted

Agent is working. Follow it with:
  quicbaas orchestrator attach sess_abc123 --from 42
```

### orchestrator status

Get detailed status of an active session.

```bash
quicbaas orchestrator status sess_abc123
quicbaas orchestrator status sess_abc123 --output json
```

Arguments:
- `<sessionID>` - Session ID to check

Response (table format):
```
Session ID:     sess_abc123
Backend:        claude
Status:         idle
Container ID:   container_xyz789
Created:        2026-03-31T14:20:00Z
Last Active:    2026-03-31T14:22:30Z
```

### orchestrator close

Terminate an active session and release resources.

```bash
quicbaas orchestrator close sess_abc123
quicbaas orchestrator close sess_abc123 --output json
```

Arguments:
- `<sessionID>` - Session ID to close

Response (table format):
```
Session ID:     sess_abc123
Closed:         true
```

### orchestrator sessions

List all active sessions across all backends.

```bash
quicbaas orchestrator sessions
quicbaas orchestrator sessions --output json
```

Response (table format):
```
SESSION ID          BACKEND     STATUS      LAST ACTIVE
sess_abc123         claude      idle        2026-03-31T14:22:30Z
sess_def456         ollama      active      2026-03-31T14:23:15Z

Total: 2 sessions
```

---

## Task Management

### orchestrator task create

Create a new orchestration task.

```bash
quicbaas orchestrator task create --subject "Build the project"
quicbaas orchestrator task create --subject "Run tests" --team qa --description "Full test suite"
quicbaas orchestrator task create --subject "Deploy to staging" --team devops --assigned-to agent_001
```

Flags:
- `--subject <text>` (required) - Task subject
- `--description <text>` - Task description
- `--team <name>` - Team name
- `--assigned-to <agentID>` - Assign to specific agent

Response (table format):
```
ID:             task_abc123
Subject:        Build the project
Status:         pending
Team:           (empty)
Description:    (empty)
Assigned To:    (empty)
Version:        1
Created:        2026-03-31T14:20:00Z
```

### orchestrator task get

Retrieve details for a specific task.

```bash
quicbaas orchestrator task get task_abc123
quicbaas orchestrator task get task_abc123 --output json
```

Arguments:
- `<taskID>` - Task ID to retrieve

### orchestrator task list

List orchestration tasks with optional filters.

```bash
quicbaas orchestrator task list
quicbaas orchestrator task list --status pending
quicbaas orchestrator task list --team backend --output json
quicbaas orchestrator task list --assigned-to agent_001
```

Flags:
- `--status <status>` - Filter by status: pending, claimed, in_progress, completed, failed, cancelled, blocked
- `--statuses <a,b,...>` - Filter by any of several statuses (e.g. `blocked,pending`); composes with `--status`
- `--project <projectID>` - Filter by project
- `--team <name>` - Filter by team name
- `--assigned-to <agentID>` - Filter by assigned agent

### orchestrator task claim

Atomically claim a task for an agent. Task must be in pending status.

```bash
quicbaas orchestrator task claim task_abc123 --agent agent_001
```

Arguments:
- `<taskID>` - Task ID to claim

Flags:
- `--agent <agentID>` (required) - Agent ID claiming the task

### orchestrator task complete

Mark a task as completed with optional result.

```bash
quicbaas orchestrator task complete task_abc123
quicbaas orchestrator task complete task_abc123 --result "All tests passing"
```

Arguments:
- `<taskID>` - Task ID to complete

Flags:
- `--result <text>` - Task completion result/output

### orchestrator task block

Park a pending, claimed or in-progress task until a person readmits it. A
blocked task is never dispatched; the kind says why it waits and the reason is
surfaced to whoever it waits for.

```bash
quicbaas orchestrator task block task_abc123 --kind needs_info --reason "which branch?"
quicbaas orchestrator task block task_abc123 --kind escalated --reason "attempt budget exhausted"
```

Arguments:
- `<taskID>` - Task ID to block

Flags:
- `--kind <kind>` (required) - `needs_info`, `blocked` or `escalated`
- `--reason <text>` (required) - Human-readable reason

Requires `orchestrator:task_block`.

### orchestrator task readmit

Return a blocked task to the pending queue. The block, the assignment and any
retry hold are cleared together, so the task is claimable again immediately.

```bash
quicbaas orchestrator task readmit task_abc123
```

Arguments:
- `<taskID>` - Task ID to readmit

Requires `orchestrator:task_readmit`.

### orchestrator task delete

Remove a task, its artifacts, its claim mark and its source reservation. The
last of those is why the verb exists: while a task that can still run holds
it, the forge issue or ticket the task was adopted from cannot be adopted
again.

A task that reached a terminal status does not need deleting for that: its
reservation stops binding, and the next sweep takes it over, adopting a fresh
task that records the terminal one in `metadata.predecessor_task`. Delete a
task when it should never have been adopted, not to free an origin whose run
is over.

A task a job holds a live lease on is refused with a conflict — finish or
cancel the job first. A stale or released lease is no obstacle.

```bash
quicbaas orchestrator task delete task_abc123
```

Arguments:
- `<taskID>` - Task ID to delete

Requires `orchestrator:task_create` — deleting a task record is the inverse of
writing one, and the permission vocabulary go-qrdb owns carries no separate
delete action for the orchestrator family.

Jobs are not rewritten by a delete. A concluded job keeps the `task_id` naming
what it was an attempt at, because that is a fact about the past; resolving it
afterwards answers "task not found", which is the accurate answer.

---

## Task Decomposition

### orchestrator decompose

Use an agent to decompose a high-level task into structured work items.

```bash
quicbaas orchestrator decompose "Implement user authentication"
quicbaas orchestrator decompose "Build REST API" --backend ollama
quicbaas orchestrator decompose "Add caching layer" --backend claude --model claude-3-opus
quicbaas orchestrator decompose "Refactor storage" --project proj_123
```

Arguments:
- `<task>` - High-level task description (required)

Flags:
- `--backend <name>` - Backend name (omit to use lead agent)
- `--model <model>` - Model override for decomposition
- `--project <id>` - Project ID to scope decomposition

---

## Agent Fleet Management

### agent register

Register a new agent with the fleet registry.

```bash
quicbaas agent register --name worker-1 --host node01:9090 --type claude
quicbaas agent register --name worker-2 --host node02:9090 --type ollama --backend ollama --model llama3
quicbaas agent register --name worker-3 --host node03:9090 --type claude --tenant my-tenant --output json
quicbaas agent register --name worker-4 --host node04:9090 --type claude --work-dir /data/agents
```

Flags:
- `--name <name>` (required) - Agent name
- `--host <address>` (required) - Agent host address (IP:port)
- `--type <type>` (required) - Agent type (e.g., claude, ollama)
- `--backend <name>` - Backend name
- `--model <model>` - Model identifier
- `--work-dir <path>` - Working directory for agent
- `--tenant <id>` - Tenant ID (default: "default")

### agent list

List registered agents with optional filters.

```bash
quicbaas agent list --tenant dev-team
quicbaas agent list --tenant dev-team --status running
quicbaas agent list --tenant dev-team --host node01:9090 --output json
quicbaas agent list --tenant dev-team --type claude --backend claude
```

Flags:
- `--host <address>` - Filter by host address
- `--status <status>` - Filter by status: registered, running, idle, offline, error
- `--type <type>` - Filter by agent type
- `--backend <name>` - Filter by backend name
- `--tenant <id>` - Tenant ID (default: "default")

### agent status

Get detailed status for a specific agent.

```bash
quicbaas agent status agent_abc123 --tenant dev-team
quicbaas agent status agent_abc123 --tenant dev-team --output json
```

Arguments:
- `<agentID>` - Agent ID to check

### agent remove

Remove an agent from the fleet registry.

```bash
quicbaas agent remove agent_abc123 --tenant dev-team
```

Arguments:
- `<agentID>` - Agent ID to remove

### agent heartbeat

Send a heartbeat to keep an agent marked as running.

```bash
quicbaas agent heartbeat agent_abc123 --tenant dev-team
```

Arguments:
- `<agentID>` - Agent ID to send heartbeat for

---

## REST API (Alternative to CLI)

All commands also available via REST endpoints.

### Health Check

```bash
curl http://localhost:8080/api/v1/orchestrator/health \
  -H "X-Tenant-ID: dev-team"
```

### List Backends

```bash
curl http://localhost:8080/api/v1/orchestrator/backends \
  -H "X-Tenant-ID: dev-team"
```

### Agent Operations

```bash
# Register agent
curl -X POST http://localhost:8080/api/v1/orchestrator/agents \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "worker-1",
    "host": "node01:9090",
    "type": "claude",
    "backend": "claude"
  }'

# List agents
curl http://localhost:8080/api/v1/orchestrator/agents \
  -H "X-Tenant-ID: dev-team"

# Get agent details
curl http://localhost:8080/api/v1/orchestrator/agents/agent_abc123 \
  -H "X-Tenant-ID: dev-team"

# Send heartbeat
curl -X POST http://localhost:8080/api/v1/orchestrator/agents/agent_abc123/heartbeat \
  -H "X-Tenant-ID: dev-team"

# Remove agent
curl -X DELETE http://localhost:8080/api/v1/orchestrator/agents/agent_abc123 \
  -H "X-Tenant-ID: dev-team"
```

### Task Operations

```bash
# Create task
curl -X POST http://localhost:8080/api/v1/orchestrator/tasks \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{
    "subject": "Build the project",
    "team_name": "backend"
  }'

# List tasks
curl http://localhost:8080/api/v1/orchestrator/tasks \
  -H "X-Tenant-ID: dev-team"

# Get task
curl http://localhost:8080/api/v1/orchestrator/tasks/task_abc123 \
  -H "X-Tenant-ID: dev-team"

# Claim task
curl -X POST http://localhost:8080/api/v1/orchestrator/tasks/task_abc123/claim \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "agent_001"}'

# Complete task
curl -X POST http://localhost:8080/api/v1/orchestrator/tasks/task_abc123/complete \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{"result": {"output": "Completed successfully"}}'
```

### Job Operations

```bash
# Start a job
curl -X POST http://localhost:8080/api/v1/orchestrator/jobs \
  -H "X-Tenant-ID: dev-team" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "Build authentication service",
    "workflow_name": "software-dev",
    "max_iterations": 10
  }'

# Get job status
curl http://localhost:8080/api/v1/orchestrator/jobs/job_abc123 \
  -H "X-Tenant-ID: dev-team"

# Cancel job
curl -X POST http://localhost:8080/api/v1/orchestrator/jobs/job_abc123/cancel \
  -H "X-Tenant-ID: dev-team"
```


---

## quicbaas orchestrator

Operate against the orchestration layer: backends, agents, sessions,
prompts, jobs, workflows, and stateless inference. Every subcommand
goes through the SDK abstraction and works on every transport (REST,
gRPC, QUIC, MCP, Unix socket).

### Subcommands

| Command | Description |
|---------|-------------|
| `backends list` | List backends registered with the orchestrator |
| `agents list` | List agent daemons currently in the fleet registry |
| `agents register` | Register an agent (mTLS-bootstrapped, normally via `agent deploy`) |
| `session spawn` | Open a persistent ACP session against a session-based backend |
| `session prompt` | Send a prompt into an existing session |
| `session attach` | Replay history + tail live events for a session |
| `session list` | List sessions filterable by tenant / backend / status |
| `inference` | Send a stateless prompt — no session needed |
| `jobs list` / `jobs start` | Queue and inspect orchestration jobs |
| `workflows list` / `workflows run` | Named multi-step workflow execution |

### quicbaas orchestrator inference

Stateless prompt routed to a fleet-resident backend (e.g., the Ollama
instance bundled with each agent daemon). The orchestrator picks an
online agent serving the chosen backend, forwards the prompt over an
mTLS-authenticated gRPC channel, and streams the response back.

#### Flags

| Flag | Type | Description |
|------|------|-------------|
| `--backend` | string (required) | Backend name (`ollama`, etc.). Session-only backends (`claude`, `codex`, `opencode`) are rejected with `InferUnsupportedError`. |
| `--prompt` | string (required) | The user prompt. |
| `--model` | string | Backend model identifier. Defaults to the backend's `default_model`. |
| `--system-prompt` | string | Optional system message prepended to the prompt. |
| `--host-affinity` | string | Pin to an agent by name or hostname. |
| `--metadata` | `key=value` (repeatable) | Forwarded verbatim to the backend. |
| `--stream` | bool | Emit NDJSON chunks as they arrive instead of buffering. |
| `--tenant` | string | Defaults to the global `--tenant` flag (`default`). |
| `--timeout` | duration | Per-call timeout. Defaults to the global `--timeout`. |
| `--output` | `table\|json\|yaml` | Output format. Default `table`. |

#### Examples

```bash
# Default (REST) — auto-selected from the URL scheme.
quicbaas orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences"

# Streaming.
quicbaas orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "tell me a haiku about mTLS" \
  --stream

# Pin to a specific agent.
quicbaas orchestrator inference \
  --backend ollama --host-affinity gpu-node-01 \
  --prompt "summarise the NIST CSF"

# Same command over each transport.
quicbaas --transport grpc --address localhost:19090 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport quic --address localhost:10443 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport mcp --address localhost:11443 --tls-skip-verify \
  orchestrator inference --backend ollama --prompt "ping"

quicbaas --transport unix --address /tmp/quicbaas.sock \
  orchestrator inference --backend ollama --prompt "ping"
```

#### Output (`--output json`)

```json
{
  "content": [{"type": "text", "text": "QUIC is..."}],
  "model": "qwen3:1.7b",
  "stop_reason": "end_turn"
}
```

#### Streaming output (`--stream`)

NDJSON, one chunk per line:

```json
{"delta":[{"type":"text","text":"QUIC "}],"final":false}
{"delta":[{"type":"text","text":"is..."}],"final":false}
{"delta":[{"type":"text","text":""}],"model":"qwen3:1.7b","stop_reason":"end_turn","final":true}
```

#### Errors

| Exit | Cause |
|------|-------|
| 0 | Successful response. |
| 1 | Invalid flag combination, missing required flags. |
| 2 | Backend rejects sessionless prompts (use `session spawn` + `session prompt`). |
| 3 | No online agent serves the requested backend in this tenant. |
| 4 | Network / mTLS handshake failure. |
| 5 | Backend returned an error mid-stream. |

#### See also

- [Fleet quick start](user.md) — full deploy + inference walkthrough.
- [agents/inference.md](user.md) — RPC, REST, SDK, and GUI reference.
- [agents/deploy.md](cli.md) — how to put an agent on a remote host.

### quicbaas orchestrator backends list

List all backends the orchestrator knows about, including their
capabilities (session-based vs. sessionless, tool use, file access,
streaming).

```bash
quicbaas orchestrator backends list --output json
```

```json
[
  {"name":"ollama","type":"ollama","capabilities":{"persistent_sessions":false,"streaming":true}},
  {"name":"claude","type":"acp","capabilities":{"persistent_sessions":true,"tool_use":true,"file_access":true,"acp":true}},
  ...
]
```

### quicbaas orchestrator agents list

List the agent daemons currently registered in the fleet:

```
ID                  NAME         HOST                  TYPE   STATUS   LAST SEEN
agt_abc123def456    gpu-node-01  gpu-host.example.com  acp    running  2026-04-25T17:12:59Z
```

The same data is rendered live by the GUI under **Orchestration →
Agent Fleet**.


---

## quicbaas agent

Operate against the fleet agent layer: deploy daemons to remote
hosts, list registered daemons, inspect agent identity and
capabilities, and roll out updates.

The agent's local Ollama / ACP backends are **never** exposed to the
network — operators only ever talk to the central QuicBaaS server,
which routes prompts and inference calls over the agent's mTLS
gRPC channel.

### Subcommands

| Command | Description |
|---------|-------------|
| `deploy` | Provision a new agent on a remote host over SSH (full mTLS bootstrap) |
| `list` | List registered agent daemons (alias for `orchestrator agents list`) |
| `inspect <id>` | Show identity, certificate serial, last heartbeat, served backends |
| `events <id>` | Stream the agent's event log live |
| `update <id>` | Push a new static CLI binary and restart the daemon |
| `delete <id>` | Deregister and tear down a remote daemon |

### quicbaas agent deploy

Provisions a QuicBaaS agent daemon on a remote host. See the
[deploy reference](cli.md) for the full step-by-step
breakdown of what this command does on the wire.

#### Flags

| Flag | Description |
|------|-------------|
| `--ssh user@host[:port]` | SSH target (required) |
| `--ssh-key-name <name>` | xkms-stored SSH key (required) |
| `--ssh-key-scope user\|tenant\|service` | Defaults to `user` |
| `--ssh-key-owner <id>` | Defaults to `$USER` for `user` scope |
| `--ssh-insecure-host-key` | Skip remote host-key verification (operator opt-in) |
| `--server https://host:port` | URL the agent will dial (required) — must be LAN-routable from the target |
| `--tenant <id>` | Tenant the agent serves; defaults to `default` |
| `--backend <kind>` (repeatable) | Backends to enable (`ollama`, `acp`) |
| `--binary <path>` | Local path to the static CLI (default `bin/quicbaas-static`) |
| `--install-dir <dir>` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir <dir>` | Remote data dir (default `~/.quicbaas/agent`) |
| `--bind <addr:port>` | Local bind on the remote host (default `0.0.0.0:9091`) |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online <duration>` | Max time to wait for online status (default `2m`) |

#### Example

```bash
# 1. Stage an SSH key in xkms (one-time per operator).
quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner "$USER"
# → copy the printed public key to ~/.ssh/authorized_keys on the target.

# 2. Deploy.
quicbaas agent deploy \
  --ssh deploy@gpu-node-01 \
  --ssh-key-name fleet-deploy \
  --server https://quicbaas.example.com:18080 \
  --backend ollama \
  --binary bin/quicbaas-static \
  --wait-online 2m
```

The deploy ends with `Online At: <timestamp>` once the agent is
visible in the fleet registry.

### What the agent does on first start

1. **Loads identity** — reads its mTLS keypair from `~/.quicbaas/agent/agent.{crt,key}` and the server CA from `server-ca.crt`.
2. **Registers** — `POST /api/v1/agents/register` with the client cert. The server returns OAuth2 credentials AND the **gRPC event-ingest URL** (e.g., `https://server-host:19090`). Both are persisted to `credentials.json`.
3. **Opens the event-forwarding gRPC stream** — dials the gRPC ingest URL learned at registration, NOT the REST URL. This guarantees `h2` ALPN succeeds without dual-publishing gRPC behind the REST listener.
4. **Heartbeats** — periodic OAuth2 token refresh + status update.
5. **Serves prompts/inference** — exposes the `proto.AgentRuntime` gRPC service on its bind address (`0.0.0.0:9091` by default) for the central to dial.

The agent has **no** outbound listener for its embedded Ollama; that
process binds to `127.0.0.1:11434` only.

Registration is **idempotent** on the agent identity (tenant + certificate
Common Name). A daemon that registers again — after a restart, a dropped
response, or a supervisor retry loop — receives the **same** `client_id` and
`mtls_client_id` it had before, with a freshly rotated `client_secret`, and the
`self_signed_tls_client_auth` client is re-pinned to the certificate it is
presenting now. Re-registering therefore never leaves an orphan OAuth2 client
behind, and never invalidates the credential the daemon is about to persist. The
previous secret stops working the moment the new one is issued, so persist the
whole response.

See [OAuth 2.1 / OIDC architecture §2a](../oauth2-oidc/architecture.md) for the
client-identifier grammar and the tenant-homing rule the credentials depend on.

### Embedded agent vs. standalone daemon

`quicbaas agent serve` starts a **standalone** agent daemon — a separate process
that connects to a remote quicbaasd over the network using mTLS.

`quicbaasd --embedded-agent` starts an **embedded** agent inside the quicbaasd
process itself. It registers through the same fleet registry as standalone daemons
but communicates via an in-process bufconn transport (no network hop). Use this
when the orchestrator host itself has Ollama and you want zero-latency inference
without deploying a separate process.

See [embedded agent mode](engineering.md#embedded-agent-mode) for full details.

### Enrollment retry

A standalone daemon waits for its control plane instead of exiting when
the control plane is not answering yet. Enrollment failures are triaged
into two classes: **transient** (connection refused, host/network
unreachable, DNS failure, timeout, HTTP 502/503/504) are retried with
capped exponential backoff plus jitter, one INFO line per wait;
**terminal** (HTTP 4xx, OAuth2 `invalid_client`, an untrusted server
certificate, unusable key material, invalid configuration, or anything
unrecognized) exit 1 immediately, naming the class and reason.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--enroll-retry-base <dur>` | `1s` | First backoff wait; doubles per attempt |
| `--enroll-retry-max <dur>` | `1m` | Ceiling on a single backoff wait |

There is no attempt budget: an agent that gave up would only be restarted
by its supervisor into the same wait. See
[troubleshooting](engineering.md#the-daemon-is-waiting-to-enroll).

### See also

- [Fleet quick start](user.md) — full E2E walkthrough
- [Daemon design](engineering.md) — agent runtime internals
- [Deploy reference](cli.md) — every flag, every step
- [Inference reference](user.md) — what `agent deploy --backend ollama` enables


---

## Remote Agent Deploy

`quicbaas agent deploy` provisions a QuicBaaS agent daemon on a remote
host over SSH. It mints an mTLS client certificate for the daemon via
the server's embedded CA, uploads the static CLI and identity material
over SFTP, installs a systemd `--user` unit (or a nohup fallback), and
waits for the agent to appear online in the fleet registry.

The same workflow is available from the Wails desktop GUI
(`Orchestrator → Agents → Deploy Remote Agent`) and the Go SDK
(`AgentDeployService`). Every transport other than Wails rejects the
TypeScript SDK `orchestrator.deployAgent` call with
`DeployNotSupportedError` — deploys are operator-side and must be run
from the CLI or the desktop app.

### Prerequisites

1. **Static CLI binary** — produced by `make build-static-cli`. The
   default path the CLI expects is `bin/quicbaas-static`.
2. **SSH access** to the remote host. SSH private key stored
   in the xkms-backed key store (preferred).
3. **Run from the QuicBaaS server host** so the embedded CA can mint
   the agent identity. Operators do not stage any pre-shared secret.

### Identity bootstrap (mTLS)

The deploy flow issues a fresh X.509 client certificate for every
deployed daemon. The certificate:

- carries `OU=agents`, `O=<tenant>`, `CN=agent-<hostname>` by default;
- includes the `ClientAuth` extended key usage stamp;
- is signed by the QuicBaaS server's CA;
- ships with the server's CA bundle so the daemon can verify the
  server on every TLS handshake.

Both the certificate and private key are uploaded to the remote
host's data directory (`~/.quicbaas/agent/agent.crt`,
`~/.quicbaas/agent/agent.key`, 0600) along with the server CA bundle
(`~/.quicbaas/agent/server-ca.crt`, 0644). The deploy process wipes
the private-key buffer on the operator's host immediately after the
upload completes; there is no persisted copy of the key material on
the operator's machine.

### Run the deploy

```sh
quicbaas agent deploy \
  --ssh alice@node01 \
  --ssh-key-name alice-laptop \
  --server https://quicbaas.example.com:8443 \
  --tenant acme \
  --backend acp --backend ollama \
  --binary bin/quicbaas-static
```

The deploy executes the following steps in order:

1. **Issue agent certificate** — requests a fresh mTLS identity from
   the server's CA with `OU=agents, O=<tenant>, EKU=ClientAuth` and
   365-day default validity. Fails fast if the CA rejects the
   request — the remote host is never touched.
2. **SSH dial** — establishes a session to `alice@node01` using the
   stored key named `alice-laptop`.
3. **SFTP upload** — streams `bin/quicbaas-static` to
   `$HOME/.quicbaas/bin/quicbaas` and installs `agent.crt`,
   `agent.key`, `server-ca.crt` into `$HOME/.quicbaas/agent`.
4. **Supervisor install**
   - With `--systemd yes` the workflow writes
     `$HOME/.config/systemd/user/quicbaas-agent.service`, reloads, and
     starts the unit.
   - With `--systemd no` it uses a nohup-backed start script at
     `$HOME/.quicbaas/bin/start-quicbaas-agent.sh`.
   - With `--systemd auto` (default) it probes `systemctl --user` and
     falls back to nohup when the probe fails.
5. **Wait online** — polls the fleet registry every 2 seconds until
   the agent reports `running`, `idle`, or `ready`. Disable with
   `--wait-online 0`.

The command prints a result that includes the issued certificate
serial so operators can correlate the deploy against the CA's
certificate store.

### Agent-side authentication

Once installed, the daemon authenticates to the server over mTLS on
every call:

- **Registration** (first start): `POST /api/v1/agents/register`
  using the client certificate. The server returns OAuth2 client
  credentials the daemon caches at `$HOME/.quicbaas/agent/credentials.json`,
  along with the **gRPC event-ingest URL** the agent should dial for the
  long-lived `PushEvents` stream (e.g., `https://quicbaas.example.com:9443`).
  The agent persists this URL in `credentials.json` under
  `event_ingest_addr`.
- **Heartbeat + events** (every call): standard OAuth2 bearer token
  minted from the cached credentials via `POST /oauth2/token`. Events
  flow over the gRPC ingest URL learned at registration time — never
  over the REST port — so the gRPC `h2` ALPN handshake succeeds without
  having to dual-publish gRPC behind the REST listener.

The daemon reads its cert/key/CA from the files the deploy flow
installed. There is no shared-secret bootstrap token at any stage.

### Flag reference

| Flag | Description |
| --- | --- |
| `--ssh user@host[:port]` | SSH target (required) |
| `--ssh-key-name name` | Name of an xkms-stored SSH key (required) |
| `--server https://...` | QuicBaaS server the agent will dial (required) |
| `--tenant id` | Tenant the agent serves |
| `--backend kind` | Agent backend (repeatable) |
| `--binary path` | Local static CLI binary |
| `--install-dir dir` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir dir` | Remote data dir (default `~/.quicbaas/agent`) |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online dur` | Max time to wait for online status |

## quicbaas project

Manage agentic **projects** — the unit that binds a workspace, a git
repository, per-agent permissions, and resource quotas together. The design
is [projects.md](projects.md).

Projects are tenant-scoped. Every subcommand operates inside the tenant named
by the global `--tenant` flag (default: `system`). The command group goes
through the SDK `Project()` client, so it is the CLI counterpart of the REST
routes under `/api/v1/projects` and of the project views in the desktop/web
GUI.

| Command | Description |
|---------|-------------|
| `create` | Create a project (`--name` required) |
| `list` | List every project in the tenant |
| `get [projectID]` | Show one project by ID, or by name with `--name` |
| `update <projectID>` | Update a project (only the flags you pass are applied) |
| `delete <projectID>` | Delete a project |
| `grant-role <projectID>` | Set an agent's role in the project |
| `revoke-role <projectID>` | Remove an agent's access (sets role `denied`) |

`--output json|yaml|table` is available on the whole group (default `table`).

### quicbaas project create

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name <name>` | string | — | Project name, unique within the tenant (**required**) |
| `--description <text>` | string | `""` | Human-readable description |
| `--workspace-agent <id>` | string | `""` | ID of the agent that owns the workspace |
| `--workspace-path <path>` | string | `""` | Filesystem path to the workspace on the agent host |
| `--host-affinity <host>` | string | `""` | Pin project operations to a preferred agent host |
| `--default-lead-agent <id>` | string | `""` | Default lead agent ID for workflow runs |
| `--git-repo <url>` | string | `""` | Remote git repository URL |
| `--git-branch <branch>` | string | `""` | Default git branch (e.g. `main`) |
| `--default-workflow <name>` | string | `""` | Tenant-scoped workflow a Task auto-spawns into (must be registered) |
| `--autonomous` | bool | `false` | Turn autonomous development on: the poller adopts work and the dispatcher starts jobs for this project |
| `--context-repo <spec>` | string (repeatable) | — | Repository the worker clones beside the checkout, as `name=<name>,url=<repo-url>,ref=<branch\|tag\|sha>` |
| `--git-author-name <name>` | string | `""` | Author name recorded on commits made from the workspace |
| `--git-author-email <email>` | string | `""` | Author email recorded on commits made from the workspace |
| `--git-signing` | bool | `false` | Sign commits with the KMS-held signing key (requires `--git-signing-key`) |
| `--git-signing-algorithm <alg>` | string | `ED25519` | `ED25519` or `ECDSA-P256` (FIPS) |
| `--git-signing-key <name>` | string | `""` | KMS signing key name |
| `--git-signing-key-scope <scope>` | string | `""` | KMS signing key scope: `user`, `tenant`, `service` |
| `--git-signing-key-owner <owner>` | string | `""` | KMS signing key owner |
| `--git-ssh-key <name>` | string | `""` | KMS SSH key an `ssh://` repository is reached with (defaults to the signing key) |
| `--git-ssh-key-scope <scope>` | string | `""` | KMS SSH key scope: `user`, `tenant`, `service` |
| `--git-ssh-key-owner <owner>` | string | `""` | KMS SSH key owner |
| `--token-budget <n>` | int | `0` | Maximum LLM token budget (0 = unlimited) |
| `--gpu-minutes <n>` | int | `0` | Maximum GPU compute budget in minutes (0 = unlimited) |
| `--max-concurrency <n>` | int | `0` | Maximum concurrent workflow runs (0 = unlimited) |
| `--metadata k=v` | key=value (repeatable) | — | Caller-defined annotations |

```bash
quicbaas project create --name web-api \
  --description "Public API service" \
  --git-repo https://git.example.com/acme/web-api.git --git-branch main \
  --token-budget 1000000
```

`--git-signing` is the switch and `--git-signing-key` is the key: naming the
key alone leaves signing off, `create` accepts it silently, and
`quicbaas sdlc bind` then refuses the project because its commits would be
unsigned.

### quicbaas project update

Accepts every `create` flag plus:

| Flag | Type | Description |
|------|------|-------------|
| `--name <name>` | string | New project name |
| `--clear-description` | bool | Remove the description (an empty `--description` leaves it unchanged) |
| `--git-sha <sha>` | string | Record the latest observed commit SHA |
| `--clear-default-workflow` | bool | Remove the default workflow so Tasks are never auto-spawned |
| `--clear-context-repos` | bool | Remove every context repository from the project |

Fields you do not pass keep their current value. `--git-signing` is
tri-state: not passing it leaves signing unchanged.

```bash
quicbaas project update proj_abc123 --token-budget 500000 --gpu-minutes 120
quicbaas project update proj_abc123 --clear-description
quicbaas project update proj_abc123 --git-signing=false --clear-default-workflow
```

### quicbaas project get

```bash
quicbaas project get proj_abc123
quicbaas project get --name web-api --output yaml
```

Passing neither a project ID nor `--name` is an error — the command will not
issue a request it cannot scope.

### Agent roles

| Role | Meaning |
|------|---------|
| `lead` | Full control over the project and its workflows |
| `worker` | Can execute tasks, cannot change project configuration |
| `observer` | Read-only |
| `denied` | Explicitly prohibited (equivalent to `revoke-role`) |

```bash
quicbaas project grant-role proj_abc123 --agent agent_abc --role lead
quicbaas project revoke-role proj_abc123 --agent agent_abc
```

An unrecognized `--role` is rejected locally before any request is sent.

### Transport support

`project` routes through the SDK like every other command group. Dedicated
Project handlers exist on **REST** (`sdk/go/transport/rest/project_handler.go`),
**gRPC** (`sdk/go/transport/grpc/project_handler.go`), **MCP**
(`sdk/go/transport/mcp/project_handler.go`) and the **embedded** transport
(eight `quicbaas.Project/*` routes in
`sdk/go/transport/embedded/transport.go:349-356`); **Unix** delegates to gRPC
and **QUIC** carries the REST surface over HTTP/3.

*Correction (2026-09-10): the absorbed page claimed the Project service was
"implemented only on the REST and embedded transports" and that "gRPC, QUIC,
Unix and MCP have no Project service handler yet". All six now have one; the
transport matrix in [api.md](api.md) agrees.*

```bash
quicbaas --address https://quicbaas.example.com:8443 project list
```

## quicbaas agent account

Pooled provider accounts — one provider login an agent session can be spawned
against instead of the host's shared identity. The full model, the selection
strategies and the credential path are in [accounts.md](accounts.md).

| Command | Description |
|---------|-------------|
| `create` | Import a credential as a new account (`--provider`, `--display-name`, `--credential-file`) |
| `list` | List the tenant's accounts, filterable by `--provider` |
| `get <accountID>` | Show one account (identifiers only; never the credential) |
| `update <accountID>` | Change the display name, the project scope (`--project` / `--clear-project`) or the refresh policy |
| `set-default <accountID>` | Make this the provider's default account |
| `rotate <accountID>` | Replace the stored credential |
| `usage <accountID>` | Token and cost attribution for the account |
| `revoke <accountID>` | Revoke the account with `--reason` |
| `delete <accountID>` | Delete the account |

Credentials are read from a file or from stdin (`--credential-file -`), never
from a flag value: a command-line secret lands in shell history and the
process table.

```bash
quicbaas agent account create --provider claude --display-name alice-work \
  --refresh-policy oauth2 --credential-file -
quicbaas agent account list --provider claude --output json
```

## quicbaas sdlc

Bind a forge repository and its review policy to an orchestration project, so
that issues labelled with the binding's trigger label become tasks the
Software Factory branches, implements, reviews and merges
(`cmd/quicbaas/sdlc.go`). Tasks, jobs and sessions are
`quicbaas orchestrator`; readmitting a blocked task is
`quicbaas orchestrator task readmit`.

Bindings are tenant-scoped: every command operates within the tenant named by
the global `--tenant` flag.

| Command | Arguments | Description |
|---------|-----------|-------------|
| `bind <project-id>` | exactly 1 | Attach a forge binding to a project from a file |
| `unbind <project-id>` | exactly 1 | Detach a project's forge binding |
| `binding <project-id>` | exactly 1 | Show a project's forge binding |
| `delivery <task-id>` | exactly 1 | Show what a task's attempt delivered |

| Flag | Scope | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--output` | group | string | `table` | `json`, `yaml` or `table` |
| `--file`, `-f` | `bind` | string | — | The project binding as YAML or JSON (**required** for `bind`) |

Omitting `--file` on `bind` fails locally with `BindingFileRequiredError`
before any request is sent; an unreadable or unparseable file fails with
`BindingFileError` naming the path.

```bash
quicbaas sdlc bind go-truststrap --file go-truststrap-binding.yaml
quicbaas sdlc binding go-truststrap --output json
quicbaas sdlc delivery 1788567351228350294
quicbaas sdlc unbind go-truststrap
```

The `table` rendering of a binding is one line per decision an operator
checks — forge, trigger label, strategy, worker (`backend/model in image`),
verification commands, review policy, review rounds, whether a webhook secret
is configured, and the pinned SSH host key rendered as its algorithm and
comment only. `delivery` prints the branch, the base, the head SHA, the pull
request, then one line per review (reviewer, verdict, head, round) and one
per check.

The binding file's fields are documented in
[sdlc.md](sdlc.md#the-project-binding); the wire shape is in
[api.md](api.md#the-sdlc-module).

## quicbaas prompts

**Not implemented.** The planned command group for editing the prompt sets the
worker, the reviewer and the lead are given — `list`, `get`, `effective`,
`diff`, `history`, `put`, `rollback`, `reset` — is specified in
[prompt-sets.md](prompt-sets.md). The prompts themselves are now markdown
embedded in the binary rather than Go constants, but there is no store behind
them yet, so editing one is editing a file and rebuilding — nothing to
configure from the command line.
