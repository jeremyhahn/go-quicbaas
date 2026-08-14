# Orchestration CLI Reference

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
synchronous conversation. See [Asynchronous Agent Turns](async-turns.md).

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
- `--status <status>` - Filter by status: pending, claimed, in_progress, completed, failed, cancelled
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
