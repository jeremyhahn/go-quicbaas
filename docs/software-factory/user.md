# Software Factory — User Guide

> **Status: NORMATIVE** · Last verified: 2026-09-12
> Task-oriented usage, in the order tasks are done: spawn a session, register
> a project, bind the SDLC module, hand over an issue, watch the job, tune the
> prompts, manage pooled accounts, run a workflow. Feature folder:
> `docs/software-factory/`. Conflicts with this document are defects.

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
    backend: "claude"
    model: "claude-3-5-sonnet-20241022"
    system_prompt: "You are an expert software engineer"
  heartbeat_timeout: 90s
```

Or via environment variables:

```bash
export QUICBAAS_ORCHESTRATION_ENABLED=true
export QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND=claude
export QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL=claude-3-5-sonnet-20241022
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

## 7. Register Agents in Fleet

Register agent workers on remote machines:

```bash
# Register agent with default model
quicbaas agent register --name worker-1 --host 203.0.113.10:9090 --type claude

# Register with custom model
quicbaas agent register --name worker-2 --host 203.0.113.11:9090 --type claude \
  --model claude-3-opus-20250219

# Register Ollama agent for local inference
quicbaas agent register --name worker-local --host localhost:9090 --type ollama \
  --backend ollama --model llama3
```

## 8. List Fleet Agents

View registered agents:

```bash
quicbaas agent list

# Output:
# ID                                      NAME            HOST                TYPE        STATUS
# a1b2c3d4-e5f6-7890-abcd-ef1234567890  worker-1        203.0.113.10:9090   claude      running
# e5f6-7890-abcd-ef1234567890-a1b2c3d4  worker-2        203.0.113.11:9090   claude      registered
```

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


## Discover and register agentic projects

The orchestrator discovers agent-enabled projects by scanning a directory tree
for convention-based marker files. Discovery is the first step before
registration, session spawning, or task decomposition; creating the Project
record itself is `quicbaas project create` ([cli.md](cli.md#quicbaas-project)),
and the record it creates is described in [projects.md](projects.md).

### How It Works

The `quicbaas_orchestrator_discover` MCP tool calls `AgentService.DiscoverProjects`, which:

1. Validates and resolves the `root_path` to an absolute path
2. Reads the immediate children of the root directory
3. For each child directory, calls `scanProjectDir` which checks for:
   - `CLAUDE.md` in the directory root (project root marker)
   - `.claude/agents/*.md` files (agent definitions)
4. Also checks the root directory itself as a potential project
5. Returns all directories that contain at least one marker

A directory is considered a project if it has a `CLAUDE.md` file, agent definition files, or both.

### Marker Files

| File | Purpose |
|------|---------|
| `CLAUDE.md` | Marks a directory as a project root. Contains project instructions and conventions. |
| `.claude/agents/<name>.md` | Defines a subagent. Each `.md` file in this directory is collected as an agent file. |

### Discovered Project Fields

Each discovered project contains:

```json
{
  "name": "software",
  "path": "/workspace/software",
  "has_claude_md": true,
  "has_agents": true,
  "agent_files": [
    "/workspace/software/.claude/agents/go-quicbaas.md"
  ]
}
```

### Example Directory Structure

```
/workspace/
  software/
    CLAUDE.md
    .claude/agents/
      backend-architect.md
      golang-pro.md
  another-project/
    CLAUDE.md
  not-a-project/
    main.go
```

Scanning `/workspace/` discovers `software` (has both markers) and `another-project` (has `CLAUDE.md`). The `not-a-project` directory is skipped.

### MCP Tool

```
Tool:     quicbaas_orchestrator_discover
Required: root_path (string) - absolute path to scan
```

### Registration

After discovery, projects are registered via `quicbaas_orchestrator_register` which stores them in a `sync.Map` keyed by a generated project ID. Registration adds configuration that discovery does not capture:

- `dependencies` - IDs of projects this project depends on
- `default_backend` - preferred agent backend (e.g., "claude", "ollama")
- `default_model` - preferred model for sessions
- `labels` - key-value metadata for categorization

Registered projects are stored in-memory. Tenant-scoped persistence is planned for a future release.

### Scan Depth

Discovery scans one level deep from the root path plus the root itself. It does not recurse into nested subdirectories. To discover projects in a deep hierarchy, call discover on each parent directory or restructure projects as immediate children of the scan root.

### Build Tags

Project discovery is always compiled. The MCP tool layer requires the `mcp` build tag.

## Bind a project and follow one issue

The SDLC module turns a labelled forge issue into a branch, a pull request, a
review and a merge. The design is [sdlc.md](sdlc.md); this is the order the
steps are done in on a live node.

1. **Register the Project** (once), with its repository, default branch,
   commit identity, signing key, the repositories the worker reads beside it,
   and the number of issues that may run at once. No `default_workflow` — an
   `sdlc` job has no workflow, and `bind` turns autonomy on for you.

   ```
   quicbaas project create --tenant acme --name go-truststrap \
     --git-repo ssh://git@git.example.com/jeremyhahn/go-truststrap.git \
     --git-branch main \
     --git-author-name factory-bot --git-author-email factory-bot@example.internal \
     --git-signing --git-signing-key go-truststrap-signing \
     --context-repo name=conventions,url=https://git.example.com/jeremyhahn/conventions,ref=main \
     --workspace-path /var/lib/quicbaas/projects/go-truststrap \
     --max-concurrency 1
   ```

   `--git-signing` is the switch and `--git-signing-key` is the key: naming
   the key alone leaves signing off, `create` accepts it silently, and `bind`
   then refuses the project because its commits would be unsigned.

   `git.example.com` is a placeholder throughout this walkthrough. The forge
   host is the deployment's own and is never hardcoded in this repository.

   `--workspace-path` is not optional in practice. It is the directory the
   attempt's checkout is mounted from, and a project without one is created
   happily and then refused by the runner on its first attempt with `project
   <project-id> has no workspace for its worker's checkout`
   (`runnerRun.workspaceRoot`). Give it an absolute directory under a root
   the daemon admits in `orchestration.sandbox.allowed_mount_roots`.

   The context repositories are cloned **by the worker's container**, beside
   the project (`/workspace/conventions` next to `/workspace/go-truststrap`), not
   by the daemon. `--autonomous` sets the switch by hand for a project that is
   not going to be bound; see
   [Autonomous projects](autonomy-and-dispatch.md).

2. **Store the forge token and webhook secret** in the tenant's secrets
   (`quicbaas secrets put <path>`), then **bind**:

   ```
   quicbaas sdlc bind <project-id> --tenant acme --file go-truststrap-binding.yaml
   ```

   ```yaml
   # go-truststrap-binding.yaml
   forge:
     type: gitea                                # gitea | github | gitlab
     base_url: https://git.example.com/api/v1   # the API ROOT, version segment included
     owner: jeremyhahn
     repo: go-truststrap
     trigger_label: task:ready
     labels: {in_progress: factory:in-progress, in_review: factory:in-review,
              merged: factory:merged, blocked: factory:blocked,
              needs_info: factory:needs-info, escalated: factory:escalated}
     token_secret_ref: sdlc/forge/go-truststrap/token
     webhook_secret_ref: sdlc/webhook/go-truststrap
     ssh_host_key: ssh-ed25519 AAAAC3Nza...
                                # the forge's OWN host key, from
                                # `ssh-keyscan -t ed25519 -p <port> <host>`
                                # with the leading host field dropped.
                                # Required when git.repo_url is ssh://
   strategy: github-flow               # github-flow | trunk-based | gitflow
   worker:
     backend: claude
     model: claude-opus-5
     image: quicbaas-software-factory-worker:0.5.1-alpha
                                # `make build-worker-image`; the image the
                                # runner clones, verifies and publishes in
   verification: [make build, make test, make lint]
   review:
     required_approvals: 1
     require_independent_reviewer: true
     require_green_checks: true
     reviewers: {codex: openai}
   max_review_rounds: 3
   ```

   Bind refuses a project whose repository host or signing policy the
   pipeline could not deliver, and a binding naming a strategy or forge this
   build does not carry — each refusal names what is available. Only `gitea`
   has an adapter today (GitHub #617, GitLab #618). It registers no workflow;
   the one thing it sets on the project is `autonomous: true`.

   **Bind also onboards the repository.** It provisions, on the forge, the
   webhook that delivers to this node and the labels the binding declares —
   nothing is committed to the repository, so changing how orchestration
   works never means editing every repository it drives. It is idempotent,
   and it never renames, recolours or removes a label that already exists.
   The node's `sdlc.webhook.public_url` is what the webhook points at.

   Check it, and repair it:

   ```
   quicbaas sdlc conformance <project-id> --tenant acme   # reports; exits non-zero when not conformant
   quicbaas sdlc onboard <project-id> --tenant acme       # repairs what provisioning can
   ```

   `conformance` names each problem distinctly — a missing, disabled or
   misdirected webhook; a label the repository does not define; an
   orchestration workflow still committed to the repository; a protected
   branch that does not require the status contexts QuicBaaS writes — with
   the fix for each. It changes nothing. See
   [sdlc.md](sdlc.md#onboarding-a-repository).

   `ssh_host_key` is what the worker's ssh verifies the forge against. A
   project cloned over `ssh://` with no key pinned is refused before its
   first container starts; there is no accept-new. See
   [sdlc.md](sdlc.md#ssh_host_key--the-forge-is-pinned-never-learned).

   `base_url` is the forge's **API root**, version segment included
   (`/api/v1` for Gitea) — not the site origin. The origin alone satisfies
   `ForgeConfig.Validate`, which only asks that the field name something, and
   then 404s every call the adapter makes.

   The six display labels in `labels:`, and the trigger label, must **already
   exist on the repository**. `GiteaBackend.resolveLabelIDs` resolves names
   through the repository's own label vocabulary and creates nothing, so a
   binding naming a label the repository does not define binds cleanly and
   then fails at the first label move with `repository <owner>/<repo> defines
   no label named <name>`. Create the vocabulary on the repository before
   binding.

3. **Point the forge's webhook** at `POST /api/v1/sdlc/webhook/<project-id>`
   with the same secret (optional; the poller sweeps on
   `orchestration.sweep_interval` regardless).

4. **Label an issue** with the binding's trigger label (`task:ready`).

5. **Follow it** through the engine's surfaces:

   ```
   quicbaas orchestrator task list --project <project-id>      # Task with source forge_issue owner/repo#n, runner sdlc, spawned_job_id
   quicbaas orchestrator job status <job-id>                    # initializing → executing → validating → reviewing → completing
   quicbaas orchestrator sessions                               # THE worker session — an sdlc job has exactly one
   quicbaas sdlc delivery <task-id>                             # branch, head, pull request, reviews, checks
   quicbaas sdlc conformance <project-id>                       # is the forge still configured for us to drive it?
   ```

   The issue shows the display labels move (`in_progress` → `in_review` →
   `merged`); the pull request and head appear as the Task's artifacts.

   A second labelled issue past `max_concurrency` is adopted as a Task and
   stays `pending` with `dispatch_reason` saying `project at capacity: 1 of 1
   concurrent jobs are running`. It dispatches on its own when the first job
   ends — no operator action.

6. **When the task stops** — for any reason at all — the issue gets one
   comment naming the outcome and the evidence, and its label moves off
   `in_progress`. The comment's `**Outcome:**` line says which ending it was:
   `escalated` (review did not converge), `needs_info`, `blocked` (an
   environment fault, or a condition the task waits on), `abandoned` (the node
   died with no attempts left) or `exhausted` (the budget is spent). The full
   table, including which endings a readmit can undo, is in
   [sdlc.md](sdlc.md#outcomes-every-way-a-task-stops-and-what-reaches-the-issue).

   A park also says **Unblocks when** and **Cleared by**, both derived from
   the park's typed unblock condition — see
   [the block lifecycle](sdlc.md#the-block-lifecycle).

   - **Cleared by the owner / an operator** — answer on the issue, then
     `quicbaas orchestrator task readmit <task-id>`; the readmission is
     commented on the issue too, the label is cleared, and the next attempt
     continues the branch and reads the discussion.
   - **Cleared by the Software Factory's own sweep** — nothing to do. The
     condition (`pipeline_green`, `forge_issue_state`) is checked on every
     sweep and the task returns to the queue on its own. A task whose
     attempt budget is spent is the exception: the comment says so, and it
     needs a fresh issue or a raised budget.

   An issue still carrying `in_progress` with no comment means the daemon
   could not reach the forge — look for `concluded … but was not surfaced` in
   the daemon log. A park the RUN decided is the one conclusion that writes
   nothing through that seam — the `Blocked` moment already posted it with the
   worker's own text — so for those look for `the park reason did not reach
   issue` instead.

   **When the worker changed nothing**, the job record says why. Every turn
   is on the job as an `sdlc:agent_turn` event carrying the reply itself:

   ```
   quicbaas orchestrator job status <job-id>    # sdlc:agent_turn — attempt, elapsed, tool_uses, outcome, the reply
   ```

   The `outcome=` field on that event, and on the `sdlc:verification_skipped`
   event beside it, is the whole diagnosis:

   | outcome | what it means | what to do |
   |---|---|---|
   | `question` / `owner_ruling` | the worker declared a stop; the task is blocked `needs_info` under an `owner_ruling` condition and its question is the issue comment | answer on the issue, then `task readmit` |
   | `environment` | the worker could not work in the container it was given; the task is blocked under an `operator_repair` condition | fix the workspace, image or network, then `task readmit` |
   | `unreadable` | the worker declared a stop with a code this build does not know | read the reply on the event; it is a charter/binary version mismatch |
   | `silent` | the worker answered with no text, twice | look at the backend and the container: nothing recorded a reason because nothing was said |
   | `claimed_work` | the worker reported edits on a tree it did not touch. **The only one still treated as a failure of the work** | read the reply; usually the worker worked outside the checkout |

   Neither condition a worker's stop produces is machine-checkable, so the
   comment always says **Cleared by the owner** or **Cleared by an
   operator** — the sweep reports these tasks rather than releasing them,
   and the readmission is yours to make once you have answered.

   Every one of these spends the attempt it ran — a turn that reached the
   model made a judgement about the work, which is what the budget counts
   (`orchestration/task_attempt.go`). What changed is that only
   `claimed_work` puts the task back in the queue: the rest PARK it, so the
   remaining budget is spent on the attempt that follows the answer instead
   of on re-running the question.

   If the task is out of budget and the fix is elsewhere (a dispatcher bug,
   a broken account), fix that and re-apply `task:ready` to the issue: a task
   that spent its budget is terminal, so its source reservation no longer
   binds and the next sweep adopts a fresh task, recording the spent one in
   `metadata.predecessor_task`. Nothing has to be deleted.

   If the answer is that the task should not have been adopted at all,
   `quicbaas orchestrator task delete <task-id>` removes it and releases the
   issue's source reservation, so a later sweep can adopt the same issue
   again. It is refused while a job holds a live lease on the task — cancel
   the job first.

7. **If the daemon dies mid-attempt**: a standalone node owns its job leases
   in-process, so the restarted node resumes its own interrupted job at the
   state its runner names (`JobResumer`, or the runner's first state), and
   the module's `Prepare`, idempotent for one attempt, re-plans the same
   branch and rewrites the same `Delivery`. In cluster modes the dead node's
   lease is foreign to its peers: the reaper
   (`orchestration.reap_lease_ttl`) concludes the job on the next sweep and
   requeues the task under its budget, and the next attempt's `Prepare`
   continues the pushed branch. There is nothing on the daemon's disk to
   reclaim either way — the dead attempt's container took its checkout with
   it. A requeued task is re-dispatched by the task dispatcher on its next
   sweep, with no operator action.

## Tune the prompts

**Not available from a running node yet.** Every instruction the Software
Factory sends an agent — the worker charter, the review framing, the lead's
decomposition prompt — is now a markdown file embedded in the binary rather
than a Go constant, so changing one is editing
`pkg/sdlc/promptdefaults/sdlc/*.md` or
`pkg/orchestration/promptdefaults/workflow/*.md` and rebuilding. Those files
are the system tenant's defaults. The tenant- and project-scoped, editable,
versioned store that will let you change one from the node or the GUI is
specified in [prompt-sets.md](prompt-sets.md).

## Manage pooled provider accounts

A tenant can hold a pool of provider logins — Anthropic/Claude seats,
OpenAI/Codex keys, OpenCode provider configs — and spawn a session against one
instead of the host's shared identity. Import one, make it the default and
spawn against it:

```bash
claude setup-token > ./token.txt
jq -n --arg t "$(cat ./token.txt)" '{access_token: $t}' \
  | quicbaas agent account create --provider claude \
      --display-name team-seat-1 --refresh-policy none --credential-file -

quicbaas orchestrator spawn --backend claude --account-auto
```

The verbs are in [cli.md](cli.md#quicbaas-agent-account); how the credential
reaches the worker host, how a limit hold rotates a seat and how cost is
attributed are in [accounts.md](accounts.md).

## Multi-Machine Agent Setup

Deploy agents on multiple machines:

### On Node 1 (203.0.113.10)

```bash
# Register the agent
quicbaas agent register --name prod-agent-01 --host 203.0.113.10:9090 --type claude

# Start agent process
quicbaas orchestrator spawn --backend claude &
```

### On Node 2 (203.0.113.11)

```bash
quicbaas agent register --name prod-agent-02 --host 203.0.113.11:9090 --type claude

quicbaas orchestrator spawn --backend claude &
```

### Coordination

All agents automatically register with the central orchestrator. The orchestrator tracks heartbeats and marks agents offline if no heartbeat received within `heartbeat_timeout` (default: 90s).

## Environment Variables

Lead agent configuration via environment:

```bash
# Backend to use for task decomposition and intelligent orchestration
QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND=claude

# Model for the lead agent
QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL=claude-3-5-sonnet-20241022

# How long to wait before marking an agent as offline (default: 90s)
QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT=120s

# Enable/disable orchestration service
QUICBAAS_ORCHESTRATION_ENABLED=true
```

## Next Steps

- See CLI Reference for complete command documentation
- See Fleet Management for agent lifecycle details
- See Overview for architecture and design principles


---

## Fleet Quick Start: Deploy an Agent and Run Inference

This guide walks through the full end-to-end flow: starting the
QuicBaaS server, staging an SSH key, deploying a fleet agent that
serves a local Ollama instance, and running stateless inference
across **all five transports** (REST, gRPC, QUIC, MCP, Unix socket).
Every step is exercised through the SDK's transport abstraction, so
the same `quicbaas orchestrator inference` command works identically
regardless of which transport you select.

### 1. Start the server

> **Prerequisite: `iam.spiffe_trust_domain` must be set.** TLS is on by default,
> so a fresh data dir brings up the CA and runs first-boot, which issues the
> platform admin's mTLS certificate. The CA stamps the tenant SPIFFE SAN onto
> that certificate — and login resolves the tenant from it — only when a trust
> domain is configured, so the daemon **fails closed** without one rather than
> handing you a credential that cannot authenticate.
>
> ```yaml
> # config.yaml
> iam:
>   spiffe_trust_domain: quicbaas.example.test
> ```
>
> See [docs/cli/init.md](../bootstrap/cli.md#prerequisite-iamspiffe_trust_domain-required)
> for the full rationale.

```bash
quicbaasd start \
  --http-port 18080 --http-address 0.0.0.0 \
  --grpc-port 19090 \
  --unix-socket /tmp/quicbaas.sock \
  --data-dir ./data \
  --log-level info
```

The server publishes:

| Transport | Address                                  | Notes                              |
|-----------|------------------------------------------|------------------------------------|
| REST      | `https://<host>:18080`                   | TLS, `http/1.1` ALPN               |
| gRPC      | `<host>:19090`                           | TLS, `h2` ALPN                     |
| MCP       | `wss://<host>:11443`                     | TLS, JSON-RPC over WebSocket       |
| QUIC      | `<host>:10443`                           | TLS 1.3, multiplexed streams       |
| Unix      | `/tmp/quicbaas.sock`                     | gRPC over UDS, no TLS              |

Each transport carries the **same SDK contract**. Fleet inference
works identically on every one of them — the test harness in
`test/integration/cli/orchestrator_inference_test.go` exercises all
five.

### 2. Stage an SSH key (one-time, per operator)

The server's xkms-backed key store keeps deploy SSH keys mlocked
in-process. Private material never touches the operator's filesystem.

```bash
# Generate a fresh ed25519 key inside xkms.
quicbaas --address https://localhost:18080 ssh keys generate \
  --name fleet-deploy --type ed25519 --scope user

# Print the public key.
quicbaas --address https://localhost:18080 ssh keys export-public \
  --name fleet-deploy --scope user --owner "$USER"
```

Add the printed public key to `~/.ssh/authorized_keys` on every host
you intend to deploy to. The server holds the private key; only the
public half is ever copied.

### 3. Deploy an agent

```bash
quicbaas --address https://localhost:18080 agent deploy \
  --ssh user@target-host \
  --ssh-key-name fleet-deploy \
  --ssh-key-scope user \
  --server https://<server-lan-ip>:18080 \
  --backend ollama \
  --binary bin/quicbaas-static \
  --wait-online 2m
```

What happens:

1. The server's CA mints an mTLS client certificate for the agent
   (`OU=agents, O=<tenant>, EKU=ClientAuth`, 365-day default validity).
2. The static CLI binary, the agent identity (`agent.crt` /
   `agent.key`), and the server CA bundle are uploaded over SFTP to
   `~/.quicbaas/agent` on the target.
3. A systemd `--user` unit (or a `nohup` fallback) starts the daemon,
   which immediately registers via `POST /api/v1/agents/register`. A
   `--user` unit also needs **lingering** enabled for the login user, or
   the agent is stopped when the deploy's SSH session closes; the deploy
   enables it over `sudo -n` and fails with the command to run when it
   cannot ([cli.md](cli.md#lingering-systemd---user)).
4. The register response advertises the **gRPC event-ingest URL**
   (e.g., `https://<server-lan-ip>:19090`) — the agent persists this
   into `credentials.json` and uses it for the long-lived `PushEvents`
   bidi stream. **Events never traverse the REST port.**
5. The deploy closes the SSH session, then polls the fleet registry
   every 2 s until the agent reports `running`, `idle`, or `ready` **and**
   its runtime port answers a dial, and prints `Online At: <timestamp>`.
   The dial is what distinguishes a live agent from a registration left
   behind by one that died on disconnect.

Verify:

```bash
quicbaas --address https://localhost:18080 agent list
```

The agent should appear with `STATUS=running` and a timestamp under
`LAST SEEN`.

### 4. Run inference

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

### 5. Use the GUI

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

### 6. What's actually happening on the wire

```
operator CLI / GUI
  └─ SDK transport (REST | gRPC | QUIC | MCP | Unix)
       └─ QuicBaaS server (orchestrator service)
            └─ RemoteACPBackend → mTLS gRPC :19090
                 └─ agent runtime (target host)
                      └─ OllamaBackend → http://127.0.0.1:11434
```

- The agent's local Ollama port (`11434`) is bound to `127.0.0.1`
  only — it has **no** external listener.
- The agent's only outbound connection is the mTLS gRPC stream to
  the control plane's gRPC ingest URL.
- The control plane's REST listener never receives gRPC traffic, so the
  REST listener can keep advertising only `http/1.1` ALPN without
  breaking anything.

### Troubleshooting

| Symptom | Resolution |
|---------|------------|
| Deploy times out at "Wait online" | Verify `--server` is reachable from the target host (LAN IP, not `localhost`). Check `~/.quicbaas/agent/quicbaas-agent.log` on the target. |
| Agent log shows `tls: no application protocol` | The agent is dialing the REST port for events. Pull the latest CLI; the deploy now embeds the gRPC ingest URL in the register response. |
| `agent list` is empty after deploy | The agent registered but failed its first heartbeat. Check the agent's clock skew vs. the server (>5 min skew breaks JWT validation). |
| `inference` returns 422 | The chosen backend is session-only. Use `quicbaas orchestrator session spawn` then `prompt` instead. |
| `inference` returns "no online agents" | No agent serves the chosen backend in the chosen tenant. Run `agent list` and confirm at least one agent has `backend=ollama`. |
| Streaming hangs in the browser | Network proxy buffering NDJSON. Set `Cache-Control: no-cache` on the proxy or run the GUI directly against the daemon. |

### Related docs

- Inference reference — full RPC, REST, SDK, GUI reference.
- Agent deploy reference — every flag, every step.
- Daemon design — agent runtime, registration, event forwarding.
- GUI: Desktop & Web — Wails desktop and browser UI.
- [API](../multiprotocol-api) — REST / gRPC / QUIC / MCP protocol details.


---

## Fleet-Routed Stateless Inference & Embeddings

### Overview

Fleet-routed stateless inference lets any QuicBaaS client (CLI, REST, gRPC, SDK) submit a single-turn prompt to a named backend (e.g., Ollama) running on a fleet-resident agent daemon without first creating a session. The orchestrator resolves the target daemon via the existing `RemoteBackendResolver`, forwards the prompt over the mTLS-authenticated gRPC connection, and streams the response back.

**Architecture boundary (L4 ↔ L2):**
- **LLM text generation** (chat, completion, code generation) — quicbaas (L4) owns the Ollama client and LLM orchestration.
- **Embedding generation** — delegated to qrdb's embedding provider (L2) via the SDK; quicbaas wraps it with a convenience single-text API (`pkg/embedding/provider.go`).

### Architecture

```
operator CLI
  → REST POST /api/v1/orchestrator/inference
      → agentServiceImpl.Infer (pkg/orchestration)
          → RemoteBackendResolver → RemoteACPBackend.Infer
              → proto.AgentRuntime/Infer (gRPC server-streaming, :9091 mTLS)
                  → Runtime.Infer (pkg/agent)
                      → OllamaBackend.Prompt → local ollama 127.0.0.1:11434
```

Session-based backends (ACP, PersistentSessions=true) are rejected with HTTP 422 / gRPC Unimplemented. Use `SpawnSession` + `PromptSession` for those backends.

### gRPC RPC

`proto.AgentRuntime` service, defined in `api/proto/agent_runtime.proto`:

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
| `pkg/agent`                            | `Runtime.Infer`        | Agent-side: delegates to OllamaBackend.Prompt |
| `pkg/agent`                            | `GRPCSpawnServer.Infer`| gRPC streaming handler on the daemon          |
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
- **Host affinity** — populated from `GET /api/v1/orchestrator/agents` filtered to
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
| No backends registered | Empty-state message with link to Agent Fleet |
| No agents for chosen backend | Empty-state below host selector with link to Agent Fleet |
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

Every entrypoint script performs the same registration lifecycle:

1. **Register**: `POST /api/v1/orchestrator/agents` with hostname, agent type, backend, work directory
2. **Heartbeat**: Background loop every 30s via `POST /api/v1/orchestrator/agents/{id}/heartbeat`
3. **Deregister**: `DELETE /api/v1/orchestrator/agents/{id}` on exit (via `trap EXIT`)

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
