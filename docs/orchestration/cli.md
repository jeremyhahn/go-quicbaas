# Orchestration — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every verb and flag of the orchestration engine's command groups, with
> runnable examples: `quicbaas orchestrator`. The `quicbaas fleet` group is in
> [../agent-fleet/cli.md](../agent-fleet/cli.md). Feature folder:
> `docs/orchestration/`. Conflicts with this document are defects.

Complete reference for QuicBaaS orchestration commands. All commands use the SDK client with multi-protocol support (gRPC, REST, QUIC, Unix, MCP).

## Output Formats

All commands support `--output` flag with values: `table` (default), `json`, `yaml`.

```bash
quicbaas orchestrator <command> --output json
quicbaas fleet <command> --output yaml
```

## Global Flags

- `--tenant <tenant>` - Tenant ID (default: "default")
- `--output <format>` - Output format: json, yaml, table (default: table)
- `--timeout <duration>` - Command timeout (default: 30s)

## Paging, Sorting and Search

Every server-backed list command — `orchestrator job list`, `orchestrator task
list`, `orchestrator sessions`, `fleet list`, `project list` and `account
list` — reads one page at a time and takes the same flags, which map onto the
list endpoints' `limit`, `offset`, `sort` and `q` parameters
([api.md](api.md)):

- `--limit <n>` - Page size. Omitted, the server answers 25; it caps any
  request at 100.
- `--offset <n>` - Records to skip.
- `--sort <field>` / `--sort <field>:desc` - Order by a field the endpoint
  documents. An unknown field is refused by the server with a 400, which the
  command reports as an invalid-input error.
- `--search <text>` - Case-insensitive substring match over the endpoint's
  text fields.
- `--all` - Follow `next_offset` from `--offset` to the last page and print
  every record. Without `--limit` it asks for the server's maximum page.

Table and text output end with a footer read from the response envelope:

```
Showing 26–50 of 132. Next page: --offset 50 (or --all for every page).
```

JSON and YAML output keep the envelope (`total`, `limit`, `offset`,
`next_offset`, `facets`) beside the records. With `--all` the envelope
describes the whole walk and names no next page.

`orchestrator workflow list` reads the local `./workflows/` directory, not the
server, and takes none of these flags.

---

## Orchestration Jobs

### orchestrator run

Start an autonomous orchestration control loop. Decomposes a task, schedules work items, assigns agents, executes work, and reviews results. The control loop runs server-side and jobs continue running independently of the CLI -- disconnecting or timing out does not cancel the job.

The CLI auto-subscribes to real-time event streaming via `SubscribeJobEvents`. Events (state transitions, task progress, agent output) are displayed as they occur. If the event stream is unavailable, the CLI falls back to polling at `--poll-interval`.

```bash
quicbaas orchestrator run "Build a tic-tac-toe game"
quicbaas orchestrator run "Implement user authentication" --lead agent:claude-lead
quicbaas orchestrator run "Add caching layer" --lead pool:leads
quicbaas orchestrator run "Build the REST API" --project proj_123 --max-iterations 5
quicbaas orchestrator run "Build a game" --workflow software-dev
quicbaas orchestrator run "Custom task" --workflow-file ./my-workflow.yaml
```

Arguments:
- `<description>` - Task description (required)

Flags:
- `--lead <ref>` - The [Agent or pool](../software-factory/agents.md) the lead runs as:
  `<agent-id>`, `agent:<id>` or `pool:<id>`. The default is the project's
  default lead.
- `--project <id>` - Project ID to scope the job
- `--workflow <name>` - Use a built-in workflow by name (e.g., software-dev)
- `--workflow-file <path>` - Path to a custom workflow YAML file
- `--max-iterations <int>` - Maximum re-delegation iterations (default: 10)
- `--poll-interval <duration>` - Interval between status polls (default: 5s)
- `--runner <name>` - Job runner driving the job's states (default:
  `workflow`); see [job-runners.md](job-runners.md)
- `--verbosity <level>` - Display verbosity: `info` (default), `verbose` or
  `debug`
- `--prompt-append <text>` / `--prompt-append-file <path>` - Prompt material
  appended to every session the job spawns, the lead's included. `-` reads
  stdin. At most 64 KiB. See [agents.md](../software-factory/agents.md#run-time-prompt-material).

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
quicbaas orchestrator job list --search deploy --limit 50
quicbaas orchestrator job list --all
```

Flags (plus the [paging flags](#paging-sorting-and-search)):
- `--task <id>` - Narrow the listing to the attempts at one task — every job
  that ran it, concluded ones included. That is the task's attempt history.
  The server applies the narrowing; see
  [../accounts/architecture.md](../accounts/architecture.md#reading-the-attempt-history-back).

### orchestrator job cancel

Cancel a running orchestration job.

```bash
quicbaas orchestrator job cancel job_abc123
```

Arguments:
- `<jobID>` - Job ID to cancel (required)

---

## Agents

`quicbaas agents list|get|create -f|update <id> -f|delete|used-by` and
`quicbaas agents pools list|get|create -f|update <id> -f|delete|used-by`
manage the tenant's [Agents and pools](../software-factory/agents.md). Definitions are YAML or
JSON. Deleting an Agent or pool that something still references is refused,
and the refusal lists every user.

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

### orchestrator spend-policy, cost, cost-settings

The tenant spend policy, the spend report and the local-inference cost
settings are documented in
[../accounts/cli.md](../accounts/cli.md#quicbaas-orchestrator-spend-policy).

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

Create a persistent agent session from a tenant [Agent or pool](../software-factory/agents.md),
or on a raw backend.

```bash
quicbaas orchestrator spawn --agent claude-coder --role coding
quicbaas orchestrator spawn --pool reviewers --role review
quicbaas orchestrator spawn --backend claude
quicbaas orchestrator spawn --backend ollama --model llama3
quicbaas orchestrator spawn --backend claude --project proj_123
quicbaas orchestrator spawn --backend claude --system-prompt "You are a Go expert"
quicbaas orchestrator spawn --agent claude-coder --prompt-append-file ./context.md
```

Flags:
- `--agent <id>` / `--pool <id>` / `--backend <name>` - Exactly one is
  required. A pool fails over along its Agents.
- `--role <role>` - The slot the session fills; an Agent must declare it
- `--model <model>` - Model identifier, used with `--backend` only
- `--system-prompt <text>` - Custom system prompt
- `--prompt-append <text>` / `--prompt-append-file <path>` - Prompt material
  appended after the session's resolved prompt. `-` reads stdin. At most
  64 KiB.
- `--project <id>` - Project ID to associate with session
- `--sandbox <mode>` - `default` (server policy) | `required` | `none`
- `--host-affinity <name|host>` - Pin the session to one fleet node daemon
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

Move a running session onto a different pooled provider account. See
[../accounts/cli.md](../accounts/cli.md#quicbaas-orchestrator-rotate-account).

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

### orchestrator attach

Stream a session's durable event log: a replay of history from the chosen
sequence, then a live tail until ctrl-c. Any number of operators can attach to
one session and all see the same order.

```bash
quicbaas orchestrator attach <sessionID> [--from <sequence>] [--no-follow] [--format pretty|json]
```

Flags:
- `--from <sequence>` - Start at this sequence number (default `0`, the
  beginning). Use the last sequence seen to resume after a disconnect.
- `--no-follow` - Return once history is drained instead of live-tailing.
- `--format` - `pretty` (default, one readable line per event) or `json` (one
  object per line, for `jq`).

```bash
# Dump the full transcript and exit
quicbaas orchestrator attach sess_abc123 --no-follow --format json > transcript.jsonl
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

List every session across all backends, in any state — closed and errored
sessions included — one page at a time; takes the
[paging flags](#paging-sorting-and-search).

```bash
quicbaas orchestrator sessions
quicbaas orchestrator sessions --output json
quicbaas orchestrator sessions --all
```

Response (table format):
```
SESSION ID          BACKEND     STATUS      LAST ACTIVE
sess_abc123         claude      idle        2026-03-31T14:22:30Z
sess_def456         ollama      active      2026-03-31T14:23:15Z
Showing 1–2 of 2.
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
- `--prompt-append <text>` / `--prompt-append-file <path>` - Prompt material
  every attempt at the task carries to the sessions it spawns (the worker's,
  for a module runner). `-` reads stdin. At most 64 KiB.

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
quicbaas orchestrator task list --search login --sort created_at:desc
```

Flags (plus the [paging flags](#paging-sorting-and-search)):
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

A block also ends the job the task has running, and the job publishes
nothing. A job on the node that took the block is aborted at once. A job on
another node is aborted by its own lease heartbeat, which re-reads the task,
within a third of the lease TTL. Either way the lease is released and the
container is torn down. Until then the delivery runner re-reads its task
before every state, every push and every pull request, so a block that races a
publication wins (go-quicbaas#1054).

Both checks ask whether the task still names **this job** as its open attempt,
not only whether it is blocked. Readmission moves the old job's binding into
the attempt history. So a block followed by a readmit, even one that lands
between two heartbeats, still ends the old job, and it never publishes. A
block's own abort ends a job only while the task is still blocked, so a block
that is readmitted and dispatched again before the abort runs leaves the new
job alone.

A job started directly for a task (`StartJob` with `task_id`) binds the task to
itself before it runs. A pending task is claimed and charged the attempt, and a
task an agent already holds is bound to the job. A task that already names a
job, or is parked or concluded, is refused, and a start that fails after the
binding releases it.

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

## REST API (Alternative to CLI)

Every command above is also a REST route. The verified route table, with the
permission each requires, is in [api.md](api.md#transports).

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
| `session spawn` | Open a persistent ACP session against a session-based backend |
| `session prompt` | Send a prompt into an existing session |
| `session attach` | Replay history + tail live events for a session |
| `session list` | List sessions filterable by tenant / backend / status |
| `inference` | Send a stateless prompt — no session needed |
| `jobs list` / `jobs start` | Queue and inspect orchestration jobs |
| `workflows list` / `workflows run` | Named multi-step workflow execution |

### quicbaas orchestrator inference

Stateless prompt routed to a fleet-resident backend (e.g., the Ollama
instance bundled with each fleet node daemon). The orchestrator picks an
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

- [Fleet quick start](../agent-fleet/user.md#quick-start) — deploy a node.
- [agents/inference.md](user.md) — RPC, REST, SDK, and GUI reference.
- [Deploy reference](../agent-fleet/cli.md#fleet-deploy) — how to put a node on a remote host.

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

---

## quicbaas fleet

Every `quicbaas fleet` verb — the registry verbs, `fleet serve`, `fleet
deploy` and its preflight, the `stop` / `start` / `undeploy` lifecycle,
enrollment retry, and the embedded node — is in
[../agent-fleet/cli.md](../agent-fleet/cli.md); the runbook is
[../agent-fleet/user.md](../agent-fleet/user.md).

## Other command groups

`quicbaas project`, `quicbaas account`, `quicbaas sdlc` and `quicbaas prompts`
belong to the Software Factory and are in
[../software-factory/cli.md](../software-factory/cli.md).
