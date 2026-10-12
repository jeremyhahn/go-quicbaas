# Orchestration — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Task-oriented usage, in the order tasks are done: start the server, spawn a
> session, prompt it, read or attach to its conversation, create and
> decompose tasks, run a workflow, run inference on a fleet node. Adding and
> running fleet nodes is in [../agent-fleet/user.md](../agent-fleet/user.md).
> Feature folder: `docs/orchestration/`. Conflicts with this document are
> defects.

Get started with QuicBaaS agent orchestration in minutes.

## Prerequisites

- QuicBaaS server running with orchestration enabled (default)
- CLI tool built: `make build`
- Backend configured (ACP for Claude, Ollama for local inference)

## 1. Start the Server

Orchestration is enabled by default. No configuration needed for basic setup.

```bash
quicbaas server start
```

Check health:

```bash
quicbaas orchestrator health
```

## 2. Check Available Backends

List registered backends and capabilities:

```bash
quicbaas orchestrator backends

# Output:
# NAME        TYPE        STATUS      AVAILABLE
# claude      acp         running     true
# ollama      ollama      running     true
```

## 3. Configure Backends (Optional)

If backends are not auto-discovered, add to your config file or environment:

```yaml
orchestration:
  enabled: true
  lead_agent:
    agent_id: "claude-lead"     # a system-tenant Agent with the lead role, or pool_id: <pool>
  heartbeat_timeout: 90s
```

Or via environment variables:

```bash
export QUICBAAS_ORCHESTRATION_ENABLED=true
export QUICBAAS_ORCHESTRATION_LEAD_AGENT_AGENT_ID=claude-lead
export QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=90s
```

## 4. Spawn an Agent Session

Create a persistent session bound to a backend:

```bash
quicbaas orchestrator spawn --backend claude

# Output:
# Session ID:     sess_abc123def456
# Backend:        claude
```

Save the session ID for later use.

## 5. Send a Prompt

Send a prompt to the active session:

```bash
quicbaas orchestrator prompt sess_abc123def456 "Write a Go function to calculate Fibonacci"

# Agent responds with code and explanation
```

## 6. Check Session Status

Query the session state:

```bash
quicbaas orchestrator status sess_abc123def456

# Output:
# Session ID:     sess_abc123def456
# Backend:        claude
# Status:         idle
# Created:        2026-03-31T14:20:00Z
# Last Active:    2026-03-31T14:22:30Z
```

## 6b. Read the Session's Conversation

A session keeps a durable transcript. To read it back — who said what, which
tools ran, and what each answer cost:

```bash
curl -s --cacert "$QUICBAAS_CA" \
  "https://localhost:8443/api/v1/orchestrator/sessions/sess_abc123def456/history?limit=20" | jq
```

```json
{
  "session_id": "sess_abc123def456",
  "turns": [
    {"sequence": 2, "role": "user", "text": "What files are in the current directory?"},
    {"sequence": 3, "role": "assistant", "text": "There are 4 files...",
     "model": "claude-sonnet-4-5", "stop_reason": "end_turn",
     "tools": [{"name": "Bash", "result_bytes": 118}],
     "usage": {"input_tokens": 1840, "output_tokens": 312}}
  ],
  "next_cursor": 0,
  "oldest_sequence": 1
}
```

Page with `?cursor=<next_cursor>` until `next_cursor` is `0`. The web UI's
session page (`#/sessions/{id}`) renders the same transcript and live-tails
new turns while you are attached; a job's detail page links straight to it.

Two things worth knowing:

- **A session keeps its last 200 turns by default.** `oldest_sequence` above
  `1` means older turns have been dropped, so what you are reading is the
  beginning of what remains rather than the beginning of the conversation. An
  operator can raise, lower or disable the bound per tenant — see
  `orchestration.session_history` in the engineering guide.
- **Very long text is clipped at 64 KiB per field.** A clipped turn ends in a
  visible `[truncated N bytes]` marker and reports the same count in
  `truncated_bytes`, so a shortened answer never looks like a complete one.

For the raw event stream instead — every fragment, thought, tool argument and
presence change, with a live SSE tail — use
`quicbaas orchestrator attach <session-id>` or
`GET /api/v1/orchestrator/sessions/{id}/events`.

## 7. Fleet Nodes

Sessions run on fleet nodes. Adding, listing, checking and removing them is
covered in the [Agent Fleet user guide](../agent-fleet/user.md); `quicbaas
fleet list` shows what is online.

## 9. Create and Assign Tasks

Create tasks for agents to claim:

```bash
# Create a task
quicbaas orchestrator task create --subject "Build authentication module" --team backend

# List pending tasks
quicbaas orchestrator task list --status pending

# Agent claims the task
quicbaas agent claim task_abc123 --agent worker-1
```

### Task lifecycle

A task moves through `pending -> claimed -> in_progress -> completed | failed`
when an **agent** claims it, and `pending -> in_progress -> completed | failed`
when the **engine** dispatches it — an auto-spawned task is claimed by the job
the engine started for it, so a running delivery reads `in_progress` rather than
`pending` for as long as its job lasts.
It can also be **blocked** from any non-terminal state when a person must
answer something (`needs_info`), an external condition is outstanding
(`blocked`), or the engine has given up on it (`escalated`):

```bash
# Park a task with a reason; it is not dispatched until readmitted
quicbaas orchestrator task block task_abc123 --kind needs_info --reason "which branch?"

# See what is waiting on a person in one project
quicbaas orchestrator task list --project proj_1 --statuses blocked

# Return it to the queue
quicbaas orchestrator task readmit task_abc123
```

A blocked task can be failed (`task complete` is refused) or readmitted.
Readmission clears the block, the assignment and any retry hold.

Every task also records where it came from (`source.kind` is one of
`forge_issue`, `scheduler`, `operator`, `job`, with `source.ref` identifying
the origin). The same source is never adopted twice within a tenant and
project *while a task is still working it*: a second create is refused and
names the task that holds it, along with the state that task is in.

A reservation binds only while its holder can still work the source. A task
that reached a terminal status — `completed`, `failed` (a spent attempt
budget), `cancelled` — will never run again, so the next sweep takes the
reservation over: it adopts a fresh task, records the terminal task in
`metadata.predecessor_task`, and leaves the terminal task in place as history.
Re-applying the trigger label to an issue whose run spent its budget is
therefore all an operator does; nothing has to be deleted first.

Deleting a task also gives its reservation back, which is what to do when the
task should never have been adopted at all:

```bash
# Frees jeremyhahn/go-truststrap#25 for the next sweep to adopt
quicbaas orchestrator task delete task_abc123
```

The sweep summary counts what it saw: `seen=N adopted=N reserved=N refused=N`.
`reserved` is the offers a live task already holds — the ordinary outcome of
re-polling. An offer held by a task that is *finished* with it and could not be
taken over is logged as a warning naming the holder and its state.

A task a job holds a live lease on is refused; a job that has already
concluded keeps its `task_id` pointing at the deleted task, because that is a
record of what ran. Attempt accounting (`attempts`, `attempt_budget`,
`environment_faults`, `retry_not_before`) and the job binding
(`spawned_job_id`; on the job, `task_id` and `attempt`) are visible on the
task and job records over every transport.

Why a task stopped is typed, not prose. Every task representation (REST, gRPC,
the Go and TypeScript SDKs) carries:

| Field | Meaning |
|-------|---------|
| `charged_attempts` | `attempts` less the refunded ones (`environment_faults` + `question_attempts`). Derived on read. |
| `effective_budget` | `attempt_budget`, or the engine default when the task declares none. Derived on read. |
| `conclusion_reason` | The typed reason the task stopped (`budget_exhausted`, `fault_bounded`, `abandoned`, `parked_needs_info`, …); empty while open. |
| `fault_class` | The fault class behind the latest concluded attempt; for a blocked task, its block's class. Derived on read. |
| `fault_origin` | Whose failure stopped it: `environment` (the fleet, an account pool, a forge), `work`, or `undiagnosed` (its node stopped); empty when it did not stop on a failure. Derived on read. |
| `block_kind` / `blocked_reason` / `block_fault_class` | The park: `needs_info` waits on the owner, `blocked` with a fault class waits on operator repair. |

`result.output` remains the detail behind these. The store keeps the job ids of
every attempt (`metadata.attempt_history`) but not each attempt's outcome, so
per-attempt history is read from the jobs themselves.

## 10. Decompose Complex Tasks

Break down a large task into work items using the lead agent:

```bash
quicbaas orchestrator decompose "Build multi-tenant REST API with authentication and caching"

# Output (JSON):
# {
#   "work_items": [
#     {
#       "id": "wi_001",
#       "title": "Design API schema",
#       "priority": 1,
#       "dependencies": []
#     },
#     {
#       "id": "wi_002",
#       "title": "Implement authentication layer",
#       "priority": 2,
#       "dependencies": ["wi_001"]
#     }
#   ]
# }
```

## 11. Close Sessions

Clean up resources when done:

```bash
quicbaas orchestrator close sess_abc123def456
```


## Projects, issues, the attention inbox, prompts and accounts

Registering agentic projects, binding the SDLC module and following an issue,
the attention inbox, tuning prompts and managing pooled provider accounts are
in [../software-factory/user.md](../software-factory/user.md).

## Multi-Machine Agent Setup

Deploy agents on multiple machines:

### On Node 1 (203.0.113.10)

```bash
# Register the agent
quicbaas fleet register --name prod-agent-01 --host 203.0.113.10:9090 --type claude

# Start agent process
quicbaas orchestrator spawn --backend claude &
```

### On Node 2 (203.0.113.11)

```bash
quicbaas fleet register --name prod-agent-02 --host 203.0.113.11:9090 --type claude

quicbaas orchestrator spawn --backend claude &
```

### Coordination

All agents automatically register with the central orchestrator. The orchestrator tracks heartbeats and marks agents offline if no heartbeat received within `heartbeat_timeout` (default: 90s).

## Environment Variables

Lead agent configuration via environment:

```bash
# The system-tenant Agent (or pool, with _POOL_ID) the lead agent runs as
QUICBAAS_ORCHESTRATION_LEAD_AGENT_AGENT_ID=claude-lead

# How long to wait before marking an agent as offline (default: 90s)
QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=120s

# Enable/disable orchestration service
QUICBAAS_ORCHESTRATION_ENABLED=true
```

## Finding things: search, filters, sort, drill-down

Every list in the web UI — jobs, tasks, sessions, workflows, the fleet,
projects, accounts, and the security, data, AI and async lists — carries the
same four controls, in the same places, doing the same thing.

**Search** is the box at the top left. It is debounced and, on a paged list,
runs server-side over the listing's text fields, so it searches the whole
collection and not the twenty-five rows on screen.

**Filters** are chip groups under the search box, one per facet the listing
narrows by, each chip carrying the COUNT of records behind it:

| List      | Facets                                   |
|-----------|------------------------------------------|
| Tasks     | status (many) · dispatch · project · agent |
| Jobs      | state (many) · runner · project · agent  |
| Sessions  | status · backend · role · project · agent |
| Workflows | mode · project                           |
| Fleet     | status · backend                         |
| Projects  | dispatch (autonomous / manual)           |
| Accounts  | status · provider                        |

A bounded vocabulary is chips; an unbounded one — a project, an agent — is a
picker, because a chip per project would be a thousand chips. Status on Tasks
and state on Jobs are MULTI-select: lighting `pending` and `blocked` asks one
question ("pending or blocked"), which is what the endpoint's `statuses`
parameter takes. Clicking a lit chip switches it off. Whatever is narrowing
the list is repeated as removable chips above it, with a per-field `×` and a
**Clear all**; every chip is a button, so Tab reaches it and Enter presses it.

Counts come from the listing's `facets` and are taken over the searched set
BEFORE the facets are applied, so `shut 2` still reads 2 while `open` is
selected — the count tells you what picking it would do. A daemon that sends
no `facets` gets the same chips without counts rather than no chips.

**Sort** is the arrow beside a column header: `⇅` when the column sorts but
is not sorting, `▲` / `▼` when it is. A header with no arrow does not sort,
which is how the two are told apart at a glance. Clicking cycles ascending →
descending → off, and only the fields the endpoint orders by are sortable —
sorting one page of twenty-five would look exactly like sorting the
collection and would not be.

**The pager is at the top AND the bottom** of every table, driving one piece
of state: `1–25 of 1,987`, first / previous / next / last, and a rows-per-page
selector (25 / 50 / 100) at the top. It appears once there is more than one
page, and stays once you have chosen a size.

**Drill-down.** Every id-like cell is a link: a task's project opens that
project's tasks, its agent opens the agent, its job opens the attempt; an
agent's detail page lists the sessions and jobs that ran on THAT agent (not
merely on its backend) and offers each as a full page of the corresponding
list. A detail page's **Back** returns to the list as you left it — narrowed,
sorted, on your page.

**A narrowed list is a link.** All of it lives in the route hash, prefixed
with the table's own key so two tables on one route never read each other's
state:

```
#/tasks?tasks_status=pending,blocked&tasks_project=<id>&tasks_sort=updated_at:desc&tasks_page=3&tasks_size=50&tasks_q=barrier
       └ facet, comma-separated for a multi-select field
                                     └ sort, `<field>` or `<field>:desc`
                                                             └ 1-based page
```

Paste that into an issue and the next person opens exactly what you were
looking at. The hash is rewritten with `replaceState`: paging is not
navigation, so Back leaves the page rather than walking through forty pages.


## Next Steps

- See CLI Reference for complete command documentation
- See Fleet Management for agent lifecycle details
- See Overview for architecture and design principles


---

## Fleet Quick Start: Run Inference

This guide runs stateless inference across **all five transports** (REST,
gRPC, QUIC, MCP, Unix socket) on a fleet node. Every step goes through the
SDK's transport abstraction, so the same `quicbaas orchestrator inference`
command works identically on each. Starting the server, staging an SSH key
and deploying the node are in the
[Agent Fleet quick start](../agent-fleet/user.md#quick-start); the server
publishes REST on `:18080`, gRPC on `:19090`, QUIC on `:10443`, MCP on
`:11443` and a Unix socket, each carrying the same SDK contract.

### Run inference

The agent's local Ollama is **not** exposed to the network — every
prompt is routed through the QuicBaaS server over the agent's mTLS
gRPC channel. Operators only ever talk to the control plane.

#### Default (REST)

```bash
quicbaas --address https://localhost:18080 \
  orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences"
```

#### Streaming (NDJSON over REST)

```bash
quicbaas --address https://localhost:18080 \
  orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "tell me a haiku about mTLS" \
  --stream
```

#### gRPC

```bash
quicbaas --transport grpc --address localhost:19090 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

#### QUIC

```bash
quicbaas --transport quic --address localhost:10443 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

#### MCP

```bash
quicbaas --transport mcp --address localhost:11443 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

#### Unix socket

```bash
quicbaas --transport unix --address /tmp/quicbaas.sock \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

### Use the GUI

Open the QuicBaaS web UI (or the Wails desktop build) and navigate to
**Software Factory → Inference**. The view loads the registered backends
via `GET /api/v1/orchestrator/backends`, lists the deployed agents
serving the chosen backend, and lets you submit prompts with
streaming on or off. Session-only backends (`claude`, `codex`,
`opencode`) appear in the dropdown but are disabled — for those, use
the **Sessions** view instead.

The GUI keeps a per-session history of the last 10 inference calls in
a collapsible card so you can re-read recent results without leaving
the view.

### Troubleshooting

Deploy and enrollment problems are in the
[Agent Fleet user guide](../agent-fleet/user.md#troubleshooting).

| Symptom | Resolution |
|---------|------------|
| `inference` returns 422 | The chosen backend is session-only. Use `quicbaas orchestrator session spawn` then `prompt` instead. |
| `inference` returns "no online agents" | No agent serves the chosen backend in the chosen tenant. Run `fleet list` and confirm at least one agent has `backend=ollama`. |
| Streaming hangs in the browser | Network proxy buffering NDJSON. Set `Cache-Control: no-cache` on the proxy or run the GUI directly against the daemon. |

### Related docs

- Inference reference — full RPC, REST, SDK, GUI reference.
- [Agent fleet CLI](../agent-fleet/cli.md) — every deploy flag, every step.
- [Agent fleet architecture](../agent-fleet/architecture.md) — node runtime, enrollment, event forwarding.
- GUI: Desktop & Web — Wails desktop and browser UI.
- [API](../multiprotocol-api) — REST / gRPC / QUIC / MCP protocol details.


---

## Fleet-Routed Stateless Inference & Embeddings

### Overview

Fleet-routed stateless inference lets any QuicBaaS client (CLI, REST, gRPC, SDK) submit a single-turn prompt to a named backend (e.g., Ollama) running on a fleet-resident fleet node daemon without first creating a session. The orchestrator resolves the target daemon via the existing `RemoteBackendResolver`, forwards the prompt over the mTLS-authenticated gRPC connection, and streams the response back.

**Architecture boundary (L4 ↔ L2):**
- **LLM text generation** (chat, completion, code generation) — quicbaas (L4) owns the Ollama client and LLM orchestration.
- **Embedding generation** — delegated to qrdb's embedding provider (L2) via the SDK; quicbaas wraps it with a convenience single-text API (`pkg/embedding/provider.go`).

### Architecture

```
operator CLI
  → REST POST /api/v1/orchestrator/inference
      → agentServiceImpl.Infer (pkg/orchestration)
          → RemoteBackendResolver → RemoteACPBackend.Infer
              → proto.FleetRuntime/Infer (gRPC server-streaming, :9091 mTLS)
                  → Runtime.Infer (pkg/fleet)
                      → OllamaBackend.Prompt → local ollama 127.0.0.1:11434
```

Session-based backends (ACP, PersistentSessions=true) are rejected with HTTP 422 / gRPC Unimplemented. Use `SpawnSession` + `PromptSession` for those backends.

### gRPC RPC

`proto.FleetRuntime` service, defined in `api/proto/fleet_runtime.proto`:

```protobuf
rpc Infer(InferRequest) returns (stream InferResponse);

message InferRequest {
  string backend       = 1;
  string model         = 2;
  string prompt        = 3;
  string system_prompt = 4;
  string tenant_id     = 5;
  map<string, string> metadata = 6;
}

message InferResponse {
  repeated ContentBlock delta = 1;
  string model                = 2;
  string stop_reason          = 3;
  bool   final                = 4;
}
```

### REST Endpoint

`POST /api/v1/orchestrator/inference`

Query parameters:
- `tenant_id` — target tenant; defaults to `"default"`.

Request body (JSON):

| Field          | Type              | Required | Description                                     |
|----------------|-------------------|----------|-------------------------------------------------|
| `backend`      | string            | yes      | Named backend (e.g., `"ollama"`)                |
| `prompt`       | string            | yes      | User prompt text                                |
| `model`        | string            | no       | Model identifier; backend default if omitted    |
| `system_prompt`| string            | no       | System message prepended to the conversation    |
| `host_affinity`| string            | no       | Pin to a daemon by Name or Host                 |
| `stream`       | bool              | no       | `true` for NDJSON streaming; default `false`    |
| `metadata`     | map[string]string | no       | Opaque key-value pairs forwarded to the backend |

#### Aggregated response (`stream=false`, default)

```json
{
  "content": [{"type": "text", "text": "..."}],
  "model": "qwen3:1.7b",
  "stop_reason": "end_turn"
}
```

#### Streaming response (`stream=true`)

Content-Type: `application/x-ndjson`. One JSON object per line; the final line carries `"final": true`.

```json
{"delta":[{"type":"text","text":"hello "}],"final":false}
{"delta":[{"type":"text","text":"world"}],"model":"qwen3:1.7b","stop_reason":"end_turn","final":true}
```

#### Error responses

| Status | Condition                                   |
|--------|---------------------------------------------|
| 400    | Missing or invalid JSON; missing backend/prompt |
| 422    | Backend requires a session (ACP/persistent)  |
| 5xx    | Daemon unreachable or internal error         |

### CLI

```bash
# Aggregated response (default)
quicbaas orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "write a haiku about mTLS"

# Streaming response
quicbaas orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences" \
  --stream

# Pin to a specific daemon
quicbaas orchestrator inference \
  --backend ollama \
  --host-affinity gpu-node-01 \
  --model qwen3:1.7b \
  --prompt "summarise the NIST CSF"

# JSON output
quicbaas orchestrator inference \
  --backend ollama \
  --prompt "hello" \
  --output json
```

### Go SDK

```go
orch := sdk.Orchestrator()

// Aggregated
resp, err := orch.Inference(ctx, &orchestrator.InferenceRequest{
    Backend: "ollama",
    Model:   "qwen3:1.7b",
    Prompt:  "write a haiku about mTLS",
})

// Streaming
chunks, err := orch.InferenceStream(ctx, &orchestrator.InferenceRequest{
    Backend: "ollama",
    Prompt:  "explain QUIC",
})
for chunk := range chunks {
    fmt.Print(chunk.Delta[0].Text)
}
```

### Key Types

| Package                                | Type                   | Role                                          |
|----------------------------------------|------------------------|-----------------------------------------------|
| `pkg/orchestration`                    | `InferRequest`         | Domain request (backend, prompt, tenant, etc.)|
| `pkg/orchestration`                    | `InferChunk`           | One streaming delta from any backend          |
| `pkg/orchestration`                    | `InferResponse`        | Aggregated result of `AccumulateChunks`       |
| `pkg/orchestration`                    | `InferUnsupportedError`| Returned for session-required backends        |
| `pkg/orchestration`                    | `InferBackendError`    | Wraps a backend Prompt failure                |
| `pkg/orchestration`                    | `AccumulateChunks`     | Drains `<-chan *InferChunk` to `*InferResponse`|
| `pkg/orchestration`                    | `AgentService.Infer`   | Service interface method                      |
| `pkg/fleet`                            | `Runtime.Infer`        | Agent-side: delegates to OllamaBackend.Prompt |
| `pkg/fleet`                            | `GRPCSpawnServer.Infer`| gRPC streaming handler on the daemon          |
| `sdk/go/client/orchestrator`           | `InferenceRequest`     | SDK wire type                                 |
| `sdk/go/client/orchestrator`           | `InferenceChunk`       | SDK streaming chunk                           |

### Scope

The `AgentScopeRuntimeInfer = "agent:runtime:infer"` scope authorises daemon tokens for the Infer RPC. The handler also accepts the existing `AgentScopeRuntimePrompt` scope for backward compatibility with tokens issued before this feature was deployed.

### Embedding Providers

Embeddings for RAG (Retrieval-Augmented Generation) workflows are sourced from
go-qrdb's embedding provider interface via the SDK. The qrdb embedding provider
is a tenant-scoped, provider-agnostic proxy that supports:

- **Ollama** — local open-source models via HTTP
- **OpenAI** — `text-embedding-3-small`, `text-embedding-3-large`, `text-embedding-ada-002`
- **LMStudio** — local models via HTTP
- **Voyage** — commercial embeddings API

quicbaas wraps the qrdb provider to add a single-text convenience API
(`pkg/embedding/bridge.go` — `providerBridgeIn` / `providerBridgeOut`). The wrapper
is thin: it adapts between quicbaas's `Embed(text) -> []float32` interface and
qrdb's batch-oriented `Embed(texts, model) -> EmbeddingResult` interface.

**Key pattern:** When quicbaas code needs embeddings, it obtains the provider
via the SDK (`qrdbsdk.EmbeddingManager()`) rather than maintaining a private
embedding client. This ensures:
- **Consistent tenant scoping** across all embedding operations.
- **Unified provider configuration** — one source of truth in qrdb.
- **No provider duplication** — qrdb's Ollama/OpenAI/Voyage adapters are the only implementations.

See [Embedding Provider Integration](../embeddings/user.md) for configuration and usage.

### Session Events

Two session-event kinds are emitted when the runtime Infer path is active:

| Kind                    | Payload                                        |
|-------------------------|------------------------------------------------|
| `inference_submitted`   | `InferenceSubmittedPayload` (backend, model, prompt) |
| `inference_completed`   | `InferenceCompletedPayload` (backend, model, stop_reason) |

Sessionless inference does not have an operator-visible session, so the
agent runtime synthesises a per-call SessionID of the form
`inference-<uuid>` and emits both lifecycle events under that key. This
satisfies the control plane event-log's session-id validation while still
letting operators correlate the two halves of the same call. Query
back via `SubscribeSessionEvents(tenant, "inference-<uuid>")`.

These events appear in the session event stream and can be consumed via `SubscribeSessionEvents`.

### Using the GUI

The QuicBaaS web UI and Wails desktop app expose inference through a dedicated
**Inference** view reachable at `#/inference` (sidebar: ORCHESTRATION > Inference).

#### Layout

The view is structured as four daisyUI cards stacked vertically:

**Card 1 — Target**

Select the backend, host affinity, and model:

- **Backend** — populated from `GET /api/v1/orchestrator/backends`. Session-only
  backends (`claude`, `codex`, `opencode`) are visible but disabled with a tooltip
  explaining that they require a session. Use the Sessions view for those.
- **Host affinity** — populated from `GET /api/v1/fleet` filtered to
  agents whose `backend` field or `capabilities` list includes the chosen backend.
  Select "Any" to let the orchestrator pick the best available daemon.
- **Model** — free-text input. Defaults to `qwen3:1.7b` for the `ollama` backend.
  The model must be installed on the chosen agent's local Ollama instance.

**Card 2 — Prompt**

- **System prompt** — collapsible; defaults to collapsed. Provides an optional system
  message prepended to the conversation.
- **Prompt** — required. Press Enter inside the form or click **Send**.
- **Stream** toggle — enabled by default. When on, the UI appends delta chunks as they
  arrive from the NDJSON stream. When off, waits for the full aggregated response.
- **Send** button — disabled while a request is in-flight.
- **Cancel** button — visible only when a request is in-flight; aborts the stream
  immediately while preserving any partial output already displayed.

**Card 3 — Response**

- Live output area with `aria-live="polite"` so screen readers announce streaming
  chunks progressively.
- Footer shows model name, stop reason, and wall-clock elapsed time once the
  response is complete.
- **Copy** button — copies the full response text to the clipboard.
- **Clear** button — resets the response area for a fresh prompt (does not clear the
  prompt field itself).

**Card 4 — Session History**

Displays the last 10 inference calls from the current browser session as collapsible
rows. Each row shows the timestamp, backend, model, host, prompt summary, and elapsed
time. Expanding a row reveals the full prompt and response text. History survives view
re-mounts (it lives in module-level Svelte 5 `$state`) but is cleared on page reload.

#### Error handling

| Scenario | Behaviour |
|----------|-----------|
| No backends registered | Empty-state message with link to Fleet |
| No agents for chosen backend | Empty-state below host selector with link to Fleet |
| 4xx from server | Field-level error below the prompt + error toast |
| 5xx from server | Error toast; prompt text preserved |
| Stream interrupted mid-response | Partial output retained; warning toast |
| >30 s generating | Spinner with "Generating..." label on the Send button |
| User cancels | Partial output retained; info toast |

#### Wails desktop mode

In the Wails desktop app the streaming path falls back to a single aggregate
`OrchestratorBinding.Inference` call because Wails IPC does not support chunked
HTTP. The response is yielded as a single `InferenceChunk` with `final: true`,
so the Svelte streaming loop terminates correctly and the UI behaves identically
to the streaming REST path from the user's perspective.


---

## Container Setup

Docker containers for running orchestrated AI agents (Claude Code, Codex, OpenCode) with QuicBaaS integration.

Source: `deployments/agents/`

### Architecture Overview

```
docker-compose.yml
    |
    +-- quicbaas (server)          :8080 REST/MCP, :50051 gRPC, :4433 QUIC
    +-- ollama (local LLM)         :11434
    +-- claude-code-agent          Claude Code CLI + MCP + gopls + Serena
    +-- codex-agent                Codex CLI + MCP + gopls + Serena
    +-- opencode-agent             OpenCode CLI + MCP + gopls + Serena
```

All agent containers follow the same pattern: Alpine 3.21 base, non-root `agent` user (UID 1001), workspace at `/workspace`, MCP config auto-generated at startup.

### Container Details

#### Claude Code Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 + Node.js 20 LTS |
| CLI | `@anthropic-ai/claude-code@latest` |
| LSP | `gopls` (built in Go 1.23 stage) |
| Semantic | Serena MCP server (Python) |
| Auth | `ANTHROPIC_API_KEY` env or mounted `~/.claude` OIDC credentials |
| Config dir | `/home/agent/.claude` |

#### Codex Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 + Node.js 20 LTS |
| CLI | `@openai/codex@latest` |
| LSP | `gopls` |
| Semantic | Serena MCP server |
| Auth | `OPENAI_API_KEY` env |
| Config dir | `/home/agent/.codex` |

#### OpenCode Agent

| Component | Details |
|-----------|---------|
| Base | Alpine 3.21 (multi-stage with Go 1.23) |
| CLI | `github.com/opencode-ai/opencode@latest` (built from source) |
| LSP | `gopls` |
| Semantic | Serena MCP server |
| Auth | `OPENAI_API_KEY` env (required) |
| Config dir | `/home/agent/.config/opencode` |

### Fleet Registration and Heartbeat

This describes the ad-hoc container entrypoint scripts only. A node run by
`quicbaas fleet serve` enrolls with its certificate instead, as described in
[Agent Fleet](../agent-fleet/architecture.md#identity). Every entrypoint script
performs the same registration lifecycle:

1. **Register**: `POST /api/v1/fleet` with hostname, agent type, backend, work directory
2. **Heartbeat**: Background loop every 30s via `POST /api/v1/fleet/{id}/heartbeat`
3. **Deregister**: `DELETE /api/v1/fleet/{id}` on exit (via `trap EXIT`)

Agent ID format: `{hostname}-{unix_timestamp}`

Registration is non-fatal: if QuicBaaS is unreachable, the agent starts anyway and retries on next heartbeat.

### Bind Mount Configuration

#### Workspace

Each agent mounts its workspace directory:

```yaml
volumes:
  - ${CLAUDE_WORKSPACE:-./workspaces/claude-code}:/workspace
```

For shared isolation mode, all agents should mount the same workspace directory. For worktree isolation mode, each gets a separate directory.

#### Credentials (Claude Code)

```yaml
volumes:
  - ${HOME}/.claude:/home/agent/.claude
  - ${HOME}/.cache/claude:/home/agent/.cache/claude
  - ${HOME}/.local/share/claude:/home/agent/.local/share/claude
  - ${HOME}/.config/claude:/home/agent/.config/claude
```

This mounts host OIDC credentials into the container, avoiding the need for API keys.

### MCP Configuration

Each entrypoint writes `/workspace/.mcp.json` with:

```json
{
  "mcpServers": {
    "quicbaas": {
      "type": "http",
      "url": "http://quicbaas:8080/mcp"
    },
    "serena": {
      "type": "stdio",
      "command": "serena",
      "args": ["--workspace", "/workspace"]
    }
  }
}
```

Serena is auto-detected: if the `serena` binary is not found, its block is omitted.

### gopls and Serena Integration

All containers include:

- **gopls**: Go Language Server Protocol for code intelligence (installed in a separate build stage to keep the runtime image small)
- **Serena**: Semantic code MCP server that provides symbol navigation, rename, find references via LSP. Runs as a stdio MCP server connected to gopls.

### GPU Compose Override

For GPU-accelerated Ollama inference:

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

The GPU override adds NVIDIA device reservations to the Ollama container:

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          capabilities: [gpu, compute, utility]
```

### Environment Variables

| Variable | Required | Default | Used By |
|----------|----------|---------|---------|
| `QUICBAAS_ADDRESS` | Yes | - | All agents |
| `ANTHROPIC_API_KEY` | No (OIDC fallback) | - | Claude Code |
| `ANTHROPIC_BASE_URL` | No | `https://api.anthropic.com` | Claude Code |
| `OPENAI_API_KEY` | Yes (Codex/OpenCode) | - | Codex, OpenCode |
| `OPENAI_BASE_URL` | No | `https://api.openai.com` | Codex, OpenCode |
| `QUICBAAS_TENANT_ID` | No | `default` | All agents |
| `CLAUDE_E2E_MODE` | No | `true` | Claude Code (skips permissions) |
| `CODEX_E2E_MODE` | No | `true` | Codex (enables --full-auto) |

### Resource Limits

| Service | CPU Limit | Memory Limit |
|---------|-----------|-------------|
| QuicBaaS server | 2 | 1G |
| Ollama | 4 | 8G |
| Each agent | 1 | 512M |


---

## Claude Code Setup Guide

### Prerequisites

- Claude Code installed and authenticated
- QuicBaaS binary built with orchestration support:
  ```bash
  make build WITH_RAG=1 WITH_VECTOR=1 WITH_MCP=1
  ```
- QuicBaaS server running (standalone or cluster mode)

### Step 1: Start QuicBaaS

Start a standalone instance for local development:

```bash
quicbaasd start
```

By default, QuicBaaS listens on `localhost:9090` with PebbleDB storage. For cluster mode, see the multi-host documentation.

### Step 2: Configure Hooks

Create or edit `.claude/settings.json` in your project root to send task lifecycle events to QuicBaaS.

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ],
    "TaskCompleted": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/task-completed"
          }
        ]
      }
    ],
    "TeammateIdle": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:9090/api/v1/hooks/teammate-idle"
          }
        ]
      }
    ]
  }
}
```

For remote servers, replace `localhost:9090` with the QuicBaaS server address. See hooks integration for full details on payload formats and error handling.

### Step 3: Configure MCP Server

Create or edit `.mcp.json` in your project root to register QuicBaaS MCP tools (RAG, KV, secrets, messaging):

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve"],
      "type": "stdio"
    }
  }
}
```

This gives Claude Code access to the orchestration MCP tools (`quicbaas_orchestrator_*`), RAG indexing, and agent cache.

### Step 4: Configure Channel Server

Add the channel server as a second MCP entry in `.mcp.json` to receive push events from QuicBaaS:

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve"],
      "type": "stdio"
    },
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": ["channel", "serve", "--server-name", "quicbaas", "--enable-reply"],
      "type": "stdio"
    }
  }
}
```

See channels documentation for configuration options and remote proxy mode.

### Step 5: Test the Integration

#### Verify hooks are reachable

```bash
curl -s -X POST http://localhost:9090/api/v1/hooks/teammate-idle \
  -H "Content-Type: application/json" \
  -d '{
    "hook_event_name": "TeammateIdle",
    "teammate_name": "test-agent",
    "team_name": "test-team"
  }'
```

Expected response (no pending tasks):

```json
{"exit_code":0}
```

#### Verify MCP tools

Start a Claude Code session and invoke a tool:

```json
{
  "tool": "quicbaas_orchestrator_list_backends",
  "arguments": {}
}
```

This should return the configured backends (ACP, Ollama).

#### Verify channel server

Start a Claude Code session with the channel MCP configured. The channel server should complete the MCP initialization handshake. Check QuicBaaS logs for:

```
channel server: MCP initialization complete
```

### Step 6: Register Your Project

Use the MCP discover and register tools to set up orchestration:

```json
{
  "tool": "quicbaas_orchestrator_discover",
  "arguments": {
    "root_path": "/path/to/your/project"
  }
}
```

Then register:

```json
{
  "tool": "quicbaas_orchestrator_register",
  "arguments": {
    "name": "my-project",
    "path": "/path/to/your/project",
    "default_backend": "claude"
  }
}
```

### Common Issues

#### Hooks return 501 Not Implemented

The task store or messaging service is not configured. Ensure QuicBaaS is started with orchestration support enabled in the config YAML:

```yaml
orchestration:
  enabled: true
  backends:
    claude:
      type: acp
      command: claude
      args: ["--chat"]
```

#### Channel server does not receive events

- Confirm the channel server process is running (`ps aux | grep "quicbaas channel"`).
- Check that the MCP initialization handshake completed (look for `MCP initialization complete` in logs).
- In remote mode, verify the `--address` flag points to the correct QuicBaaS server.

#### MCP tools not available in Claude Code

- Verify `.mcp.json` is in the project root.
- Confirm the `quicbaas` binary is on `PATH`.
- Check that QuicBaaS was built with `WITH_MCP=1`.

#### Connection refused on hooks

- Ensure QuicBaaS is running and the port matches the hook URL.
- For remote setups, verify network connectivity and firewall rules.
- Check TLS configuration if using `https://` URLs.
