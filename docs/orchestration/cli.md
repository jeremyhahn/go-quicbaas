# Orchestration — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every verb and flag of the orchestration engine's command groups, with
> runnable examples: `quicbaas orchestrator` and `quicbaas fleet`. Feature
> folder: `docs/orchestration/`. Conflicts with this document are defects.

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
  [job-admission.md](job-admission.md#the-attempt-history-a-task-can-be-read-back-through).

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

### orchestrator spend-policy

Show or replace the tenant's own spend limits (go-quicbaas#1058): sliding
windows, in tokens and/or list-price dollars, over every turn charged to the
tenant. `get` shows each window's usage; `set` takes the shared
`--spend-window` flags (the set replaces the limits) or `--clear-spend`.
Setting requires `spend_policy:write`.

```bash
quicbaas orchestrator spend-policy get
quicbaas orchestrator spend-policy set --spend-window name=monthly,size=720h,usd=2000
quicbaas orchestrator spend-policy set --clear-spend
```

### orchestrator cost

Show one scope of the consumption ledger: tokens, list-price equivalent and
billed cost, local energy (and how much was measured), and a breakdown by host.
The scope is a path — each flag needs every one above it. `--host` narrows to
the turns one host ran (`-` for turns that name none). `--agent` narrows to the
turns of the sessions one tenant Agent ran (`-` for raw-backend sessions) and
prints that Agent's own spend windows. See
[cost-enforcement.md](../software-factory/cost-enforcement.md#reading-the-ledger-the-spend-report).

```bash
quicbaas orchestrator cost
quicbaas orchestrator cost --host gpu-host:9091
quicbaas orchestrator cost --agent claude-coder
quicbaas orchestrator cost --project p1 --task t1 --from 2026-09-01T00:00:00Z --output json
```

### orchestrator cost-settings

Show or replace how the tenant's local inference is costed: the electricity
price and each engine's device draw. A local turn costs `watts × seconds /
3.6e6 kWh × $/kWh`. See
[cost-enforcement.md](../software-factory/cost-enforcement.md#local-inference-is-costed-from-electricity).

```bash
quicbaas orchestrator cost-settings get
quicbaas orchestrator cost-settings set --electricity-price 0.15 --engine-watts openvino=12 --engine-watts ollama=250
quicbaas orchestrator cost-settings set --clear   # back to the configuration
```

`set` replaces the settings. Give every value you want kept.

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

## Fleet Management

### agent register

Register a new agent with the fleet registry.

```bash
quicbaas fleet register --name worker-1 --host node01:9090 --type claude
quicbaas fleet register --name worker-2 --host node02:9090 --type ollama --backend ollama --model llama3
quicbaas fleet register --name worker-3 --host node03:9090 --type claude --tenant my-tenant --output json
quicbaas fleet register --name worker-4 --host node04:9090 --type claude --work-dir /data/agents
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

List registered fleet nodes with optional filters.

```bash
quicbaas fleet list --tenant dev-team
quicbaas fleet list --tenant dev-team --status running
quicbaas fleet list --tenant dev-team --host node01:9090 --output json
quicbaas fleet list --tenant dev-team --type claude --backend claude
quicbaas fleet list --tenant dev-team --all
```

Flags (plus the [paging flags](#paging-sorting-and-search)):
- `--host <address>` - Filter by host address
- `--status <status>` - Filter by status: registered, running, idle, offline, error
- `--type <type>` - Filter by agent type
- `--backend <name>` - Filter by backend name
- `--tenant <id>` - Tenant ID (default: "default")

### agent status

Get detailed status for a specific agent.

```bash
quicbaas fleet status agent_abc123 --tenant dev-team
quicbaas fleet status agent_abc123 --tenant dev-team --output json
```

Arguments:
- `<agentID>` - Agent ID to check

### agent remove

Remove an agent from the fleet registry.

```bash
quicbaas fleet remove agent_abc123 --tenant dev-team
```

Arguments:
- `<agentID>` - Agent ID to remove

### agent heartbeat

Send a heartbeat to keep an agent marked as running.

```bash
quicbaas fleet heartbeat agent_abc123 --tenant dev-team
```

Arguments:
- `<agentID>` - Agent ID to send heartbeat for

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
| `agents list` | List fleet node daemons currently in the fleet registry |
| `agents register` | Register an agent (mTLS-bootstrapped, normally via `fleet deploy`) |
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

List the fleet node daemons currently registered in the fleet:

```
ID                  NAME         HOST                  TYPE   STATUS   LAST SEEN
agt_abc123def456    gpu-node-01  gpu-host.example.com  acp    running  2026-04-25T17:12:59Z
```

The same data is rendered live by the GUI under **Orchestration →
Fleet**.


---

## quicbaas fleet

Operate against the fleet node layer: deploy daemons to remote
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
| `list` | List registered fleet node daemons (alias for `orchestrator agents list`) |
| `inspect <id>` | Show identity, certificate serial, last heartbeat, served backends |
| `events <id>` | Stream the agent's event log live |
| `update <id>` | Push a new static CLI binary and restart the daemon |
| `delete <id>` | Deregister and tear down a remote daemon |

### quicbaas fleet deploy

Provisions a QuicBaaS fleet node daemon on a remote host. See the
[deploy reference](cli.md) for the full step-by-step
breakdown of what this command does on the wire.

#### Flags

| Flag | Description |
|------|-------------|
| `--spec <file>` | Node spec (YAML) naming the node and the daemon's whole config; flags set on the command line override it. See [fleet.md](fleet.md) |
| `--ssh user@host[:port]` | SSH target (required without `--spec`) |
| `--ssh-key-name <name>` | xkms-stored SSH key (required) |
| `--ssh-key-scope user\|tenant\|service` | Defaults to `user` |
| `--ssh-key-owner <id>` | Defaults to `$USER` for `user` scope |
| `--ssh-insecure-host-key` | Skip remote host-key verification (operator opt-in) |
| `--server https://host:port` | URL the agent will dial (required) — must be LAN-routable from the target |
| `--tenant <id>` | Tenant the agent serves; defaults to `default` |
| `--backend <kind>` (repeatable) | Backends the node serves, by name (`claude`, `codex`, `opencode`, `openvino`, `ollama-agent`, `vllm`, `ollama`) or the `acp` group; see [fleet](fleet.md#the-node-spec) |
| `--binary <path>` | Local path to the static CLI (default `bin/quicbaas-static`) |
| `--install-dir <dir>` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir <dir>` | Remote data dir (default `~/.quicbaas/agent`) |
| `--bind <addr:port>` | Local bind on the remote host (default `0.0.0.0:9091`); needs a fixed numeric port whenever `--wait-online` is non-zero |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online <duration>` | Max time to wait for online status (default `2m`) |
| `--max-concurrent-jobs <n>` | The node's declared capacity (`agent.max_concurrent_jobs`); required unless the spec declares it |
| `--projects-root <dir>` | Remote directory job workspaces are provisioned under |
| `--preflight` | Run the preflight checks only; change nothing |
| `--skip-preflight` | Deploy without running the preflight checks first |

`quicbaas fleet stop`, `fleet start` and `fleet undeploy` take the same
`--spec` / `--ssh` / credential / directory flags; see
[fleet.md](fleet.md#lifecycle).

#### Example

```bash
# 1. Stage an SSH key in xkms (one-time per operator).
quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner "$USER"
# → copy the printed public key to ~/.ssh/authorized_keys on the target.

# 2. Deploy.
quicbaas fleet deploy \
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
2. **Registers** — `POST /api/v1/fleet/enroll` with the client cert. The server returns OAuth2 credentials AND the **gRPC event-ingest URL** (e.g., `https://server-host:19090`). Both are persisted to `credentials.json`.
3. **Opens the event-forwarding gRPC stream** — dials the gRPC ingest URL learned at registration, NOT the REST URL. This guarantees `h2` ALPN succeeds without dual-publishing gRPC behind the REST listener.
4. **Heartbeats** — periodic OAuth2 token refresh + status update.
5. **Serves prompts/inference** — exposes the `proto.FleetRuntime` gRPC service on its bind address (`0.0.0.0:9091` by default) for the central to dial.

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

### Embedded fleet node vs. standalone daemon

`quicbaas fleet serve` starts a **standalone** fleet node daemon — a separate process
that connects to a remote quicbaasd over the network using mTLS.

`quicbaasd --embedded-fleet-node` starts an **embedded** agent inside the quicbaasd
process itself. It registers through the same fleet registry as standalone daemons
but communicates via an in-process bufconn transport (no network hop). Use this
when the orchestrator host itself has Ollama and you want zero-latency inference
without deploying a separate process.

See [embedded fleet node mode](engineering.md#embedded-fleet-node-mode) for full details.

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

`fleet serve --config <file>` reads every setting from an `agent.Config`
YAML file (the file `fleet deploy` renders); a flag set explicitly on the
command line overrides the file, and keys the file omits take the flag
defaults. `--max-concurrent-jobs` declares the host's share of fleet
capacity.

There is no attempt budget: an agent that gave up would only be restarted
by its supervisor into the same wait. See
[troubleshooting](engineering.md#the-daemon-is-waiting-to-enroll).

### See also

- [Fleet quick start](user.md) — full E2E walkthrough
- [Daemon design](engineering.md) — agent runtime internals
- [Deploy reference](cli.md) — every flag, every step
- [Inference reference](user.md) — what `fleet deploy --backend ollama` enables


---

## Remote Fleet Node Deploy

`quicbaas fleet deploy` provisions a QuicBaaS fleet node daemon on a remote
host over SSH. It mints an mTLS client certificate for the daemon via
the server's embedded CA, uploads the static CLI and identity material
over SFTP, installs a systemd `--user` unit (or a nohup fallback), and
waits for the agent to appear online in the fleet registry.

The same workflow is available from the Wails desktop GUI
(`Orchestrator → Agents → Deploy Remote Fleet Node`) and the Go SDK
(`FleetDeployService`). Every transport other than Wails rejects the
TypeScript SDK `orchestrator.deployFleetNode` call with
`DeployNotSupportedError` — deploys are operator-side and must be run
from the CLI or the desktop app.

### Prerequisites

1. **Static CLI binary** — produced by `make build-static-cli`. The
   default path the CLI expects is `bin/quicbaas-static`.
2. **SSH access** to the remote host. SSH private key stored
   in the xkms-backed key store (preferred).
3. **The target's SSH host key pinned** in the known-hosts store — see
   [Host-key verification](#host-key-verification).
4. **Run from the QuicBaaS server host** so the embedded CA can mint
   the agent identity. Operators do not stage any pre-shared secret.
5. **Lingering enabled for the remote login user**, or passwordless
   `sudo` so the deploy can enable it — see
   [Lingering](#lingering-systemd---user) below. Only a systemd
   `--user` install needs it.

### Lingering (systemd --user)

A `systemd --user` manager belongs to its user's login sessions. With
`Linger=no`, logind stops that manager — and every unit it supervises —
when the user's last session ends. The deploy's own SSH session is such a
session, so an agent installed and started over it is killed on
disconnect: it enrolls, logs `ready`, heartbeats once, and is gone, while
the fleet registry still shows `running` from that last heartbeat.

The deploy therefore reads the property after installing the unit and
before starting it:

```sh
loginctl show-user alice -p Linger     # the deploy reads this: Linger=no
sudo -n loginctl enable-linger alice   # so the deploy runs this
```

Enabling lingering needs root. When `sudo -n` is unavailable or would
prompt, the deploy stops with

```
deploy: systemd --user lingering is not enabled for alice; the agent
would be stopped when the deploy's ssh session closes; enable it with:
sudo loginctl enable-linger alice
```

Run that command on the target as an administrator and re-deploy. A
deploy never reports an agent `Online` when it knows the agent will die
with the session. The nohup fallback (`--systemd no`) does not install a
unit and is not checked; it depends instead on logind's default
`KillUserProcesses=no`, which leaves a detached process running after
logout.

### Host-key verification

The deploy verifies the remote host's SSH host key against the pins in
the SSH known-hosts store before it dials. Pin the target once:

```sh
ssh-keyscan -t ed25519 node01 | cut -d' ' -f2- > node01.pub   # the host key
quicbaas ssh known-hosts add --host node01 --file node01.pub
```

`quicbaas ssh known-hosts list` shows the pins. The deploy reads that
same store, under the **same scope/owner as the SSH key**: pins default
to `--scope user` and `--owner $USER`, and `fleet deploy` looks them up
with `--ssh-key-scope` / `--ssh-key-owner` (which default the same way).
A pin stores the full host key (`--key` takes an authorized_keys-format
line, `--file` reads one); its algorithm and SHA256 fingerprint are derived
from that key, and a presented key is accepted only when both match a pin.
Several pins may exist for one host — that is how a host key is rotated
without a deploy window — and any of them is sufficient.

The two failure modes are distinct, because they demand opposite
responses:

- **Unpinned host** — the deploy stops before dialing with
  `service: no ssh host-key pin for node01 in scope=user owner=alice;
  pin it first: quicbaas ssh known-hosts add --host node01 --key
  '<authorized_keys line>' (or pass --ssh-insecure-host-key to deploy
  without verification)`.
- **Key does not match the pin** — the handshake fails with
  `service: ssh host-key mismatch for node01: presented ssh-ed25519
  SHA256:<offered>, pinned SHA256:<pinned>`. Find out why the host's key
  changed before re-pinning it.

`--ssh-insecure-host-key` skips the lookup and the verification
entirely. It is an explicit operator opt-out for lab hosts; production
deploys pin.

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
quicbaas fleet deploy \
  --ssh alice@node01 \
  --ssh-key-name alice-laptop \
  --server https://quicbaas.example.com:8443 \
  --tenant acme \
  --backend acp --backend ollama \
  --binary bin/quicbaas-static
```

The deploy executes the following steps in order:

0. **Preflight** — on an SSH session of its own, checks the node without
   changing it and stops with every failure and its fix if any check fails
   (skip with `--skip-preflight`; run alone with `--preflight`). The checks
   are listed in [fleet.md](fleet.md#preflight).
1. **Issue agent certificate** — requests a fresh mTLS identity from
   the server's CA with `OU=agents, O=<tenant>, EKU=ClientAuth` and
   365-day default validity. Fails fast if the CA rejects the
   request — the remote host is never touched.
2. **SSH dial** — resolves `node01`'s pinned host key from the
   known-hosts store, then establishes a session to `alice@node01`
   using the stored key named `alice-laptop`. An unpinned host fails
   here, before any connection is made.
3. **SFTP upload** — streams `bin/quicbaas-static` to
   `$HOME/.quicbaas/bin/quicbaas` and installs `agent.crt`,
   `agent.key`, `server-ca.crt` into `$HOME/.quicbaas/agent`, then writes
   the daemon config rendered from the spec's `agent:` section to
   `$HOME/.quicbaas/bin/agent.yaml` (0600). The unit and the start script
   run `quicbaas fleet serve --config` on that file.
4. **Supervisor install**
   - With `--systemd yes` the workflow writes
     `$HOME/.config/systemd/user/quicbaas-agent.service`, reloads,
     ensures the login user lingers (see
     [Lingering](#lingering-systemd---user)), and starts the unit.
   - With `--systemd no` it uses a nohup-backed start script at
     `$HOME/.quicbaas/bin/start-quicbaas-agent.sh`.
   - With `--systemd auto` (default) it probes `systemctl --user` and
     falls back to nohup when the probe fails.
5. **Wait online** — **after the SSH session is closed**, polls the
   fleet registry every 2 seconds until the agent reports `running`,
   `idle`, or `ready`, and confirms the agent's own runtime port
   (`<host>:<--bind port>`) accepts a connection. Both are required: a
   registration keeps the status of its last heartbeat after the process
   behind it is gone, so the registry alone cannot tell a live agent from
   one that died when the deploy disconnected. Disable with
   `--wait-online 0`.

Because the confirmation is a dial, a non-zero `--wait-online` requires
a `--bind` the deploy can dial back: `host:port` with a fixed numeric
port. A named port (`0.0.0.0:grpc`), a unix socket, or the ephemeral
`0.0.0.0:0` — whose real port only the remote process learns — is
refused up front with

```
deploy: invalid spec field=node_bind: waiting for the agent to come
online requires a bind address the deploy can dial back, i.e. host:port
with a fixed numeric port such as 0.0.0.0:9091; got "0.0.0.0:0"
```

and nothing is installed. Either name a fixed port or pass
`--wait-online 0` and verify the agent yourself; the deploy never
reports `Online` on the registry's word alone.

The command prints a result that includes the issued certificate
serial so operators can correlate the deploy against the CA's
certificate store.

### Agent-side authentication

Once installed, the daemon authenticates to the server over mTLS on
every call:

- **Registration** (first start): `POST /api/v1/fleet/enroll`
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
| `--spec file` | Node spec; flags set on the command line override it |
| `--ssh user@host[:port]` | SSH target (required without `--spec`) |
| `--ssh-key-name name` | Name of an xkms-stored SSH key (required) |
| `--ssh-key-scope scope` | Scope for the key **and host-key pin** lookup (default `user`) |
| `--ssh-key-owner owner` | Owner for the key **and host-key pin** lookup (default `$USER`) |
| `--ssh-insecure-host-key` | Skip host-key verification (operator opt-out) |
| `--server https://...` | QuicBaaS server the agent will dial (required) |
| `--tenant id` | Tenant the agent serves |
| `--backend kind` | Agent backend (repeatable) |
| `--binary path` | Local static CLI binary |
| `--install-dir dir` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir dir` | Remote data dir (default `~/.quicbaas/agent`) |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online dur` | Max time to wait for online status |
| `--max-concurrent-jobs n` | Declared capacity; required unless the spec declares it |
| `--projects-root dir` | Remote workspace root |
| `--preflight` / `--skip-preflight` | Check only / deploy without checking |

## Other command groups

`quicbaas project`, `quicbaas account`, `quicbaas sdlc` and `quicbaas prompts`
belong to the Software Factory and are in
[../software-factory/cli.md](../software-factory/cli.md).
