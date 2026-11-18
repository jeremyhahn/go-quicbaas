# Agents and pools

> **Status: REFERENCE** · Last verified: 2026-09-29
> Software Factory agent and pool definitions.

An **Agent** is a named, tenant-scoped definition of *who works*: a runtime
(`backend`), a `model`, the provider endpoint that serves it, a container
`image`, a reasoning effort, an account policy and a prompt. It also declares
the **roles** it serves. A **pool** is an ordered list of Agents that a slot
fails over along.

A role slot holds a **reference** to an Agent or a pool, never a raw
backend and model:

| slot | field | role required |
|---|---|---|
| a binding's worker | `worker: {agent_id \| pool_id}` (required) | `coding` |
| a binding's triage | `triage: {agent_id \| pool_id}` | `triage` |
| a review seat | `reviewer.agent_id`, `reviewers[].agent_id` | `review` |
| a project's default lead | `default_lead_agent: {agent_id \| pool_id}` | `lead` |
| a job's lead | `StartJobRequest.lead` | `lead` |
| the node's lead agent | `orchestration.lead_agent.{agent_id,pool_id}` | `lead` |
| the node's triage default | `sdlc.triage_agent.{agent_id,pool_id}` | `triage` |

These are not the fleet's agent daemons, which serve backends; those are
[agent-fleet](../agent-fleet/user.md).

Source: `pkg/orchestration/agent_catalog*.go`, `agent_directory.go`,
`agent_spawn.go`, `agent_migration.go`, `role_charter.go`,
`prompt_append.go`; `pkg/orchestration/sdlc/agents.go`, `agent_migration.go`.

## The Agent

```yaml
id: codex-reviewer            # [a-z0-9][a-z0-9._-]{0,63}
name: Codex reviewer
roles: [review]               # coding | review | triage | lead | project_manager | general
backend: codex                # must be served by the fleet when saved
model: gpt-5.5                # required; checked against the backend's catalog
image: quicbaas-software-factory-worker:0.11.0-alpha-cc2.1.280-cx0.156.0
reasoning_effort: medium      # minimal | low | medium | high | xhigh
provider: {name: local, base_url: http://llm:8000/v1, wire_api: responses}
account: {strategy: least_usage, accounts: [codex-1, codex-2]}
prompt: {system: "…"}         # optional; or charter_set / charter
spend: {windows: [{name: daily, size: 86400000000000, max_tokens: 5000000}]}  # optional
max_concurrent: 2
placement: {prefer: review, reserve: 1}
```

A resolved Agent **always has a model**.

The catalog refuses the following:

- an Agent whose backend no node serves;
- a model outside the backend's curated list;
- an account the tenant does not hold;
- an update that drops a role a slot still needs;
- a pool naming an Agent that does not exist, or a member that lacks a role
  the pool is used for;
- deleting an Agent or pool that is still referenced. The refusal (409) names
  every user, the same list `used-by` returns.

A reference is checked when it is saved, so a binding, project or job cannot
name something the tenant does not define.

## The prompt

An Agent's prompt is **optional**. A spawn by Agent resolves its system prompt
in this order:

1. the prompt the spawn itself names, such as an SDLC runner's role charter or
   a workflow job's `charter_<role>` (see
   [prompt-sets.md](prompt-sets.md#role-charters));
2. the Agent's own `prompt`;
3. the built-in charter of the role the spawn is for, when that role has one;
4. no system prompt, so the runtime's own default applies.

| role | built-in charter |
|---|---|
| `coding` | the SDLC worker charter (`sdlc/worker_charter`) |
| `review` | the SDLC reviewer charter (`sdlc/reviewer_charter`) |
| `triage` | the SDLC triage charter |
| `lead`, `project_manager`, `general` | none |

The SDLC runners and the workflow control loop always name their role's charter. The charter decides what
the runner parses back (a verdict, a triage declaration), so an Agent's prompt
never replaces it. To add to a charter, use run-time prompt material.

## Run-time prompt material

The Agent's prompt is a shared header. Every place that runs an Agent may pass
`prompt_append`, which is dynamic content appended after the resolved prompt:

| where | field | reaches |
|---|---|---|
| a session spawn | `SpawnSessionRequest.prompt_append` | that session |
| a job | `StartJobRequest.prompt_append` | every session the job spawns, the lead's included |
| a task | `CreateTaskRequest.prompt_append` | every session each attempt's job spawns (the worker's, for a module runner) |
| a workflow role | `agents[].prompt_append` | every session of that role |
| an SDLC worker | binding `worker_prompt_append` | every worker session |
| an SDLC triage | binding `triage_prompt_append` | every triage session |
| an SDLC review seat | `reviewer.prompt_append`, `reviewers[].prompt_append` | every review that seat runs |

Composition happens in one place, `SpawnSession`. The resolved prompt comes
first, then the slot's material, then the job's (a task's material is the job's
material for its attempts). This way the most specific content is read last.
Each block is separated by `\n\n---\n\n`, and blank blocks are dropped.

The bound is **64 KiB** for everything appended to one spawn. More is refused
with `PromptAppendTooLargeError`, which names the field. It maps to `400` on
REST and `InvalidArgument` on gRPC. A binding or workflow whose own block is
over the bound is refused when it is saved.

**Audit.** The durable session record keeps the resolved prompt
(`system_prompt`) and the appended text (`prompt_append`) apart. The record
therefore shows exactly what ran, and a resume or an account rotation composes
the same system prompt again. A job records its material in its config and on
its status (`prompt_append`), and its trail records a `prompt:append` event with
the size of each spawn's append.

The CLI carries the field as `--prompt-append <text>` or
`--prompt-append-file <path>` (`-` reads stdin) on `orchestrator spawn`,
`orchestrator run` and `orchestrator task create`.

## Host worker shell commands

A Claude Code worker that runs on the daemon host is spawned with
`--permission-mode acceptEdits`: it may edit files and run nothing. The
commands it may run come from configuration:

```yaml
orchestration:
  agent_config_dir: /var/lib/quicbaas/agents/claude
  agent_shell_allow: [go, gofmt, make, "git status"]   # default: empty
  verification_commands: [make build, make test, make lint]
```

At boot the daemon writes `<agent_config_dir>/settings.json`. Each
`agent_shell_allow` entry is a command prefix and allows that command with or
without arguments (`go` allows `go build ./...`, not `gofmt`). Each
verification command is allowed exactly, so it is not listed again here. An
empty `agent_shell_allow` allows only the verification commands. An empty
entry, or one with a line break, is refused when the config loads.

The file is generated. Edit the config, not the file: a file that differs is
overwritten at the next boot with a WARN naming
`orchestration.agent_shell_allow`. With `agent_config_dir` unset nothing is
written. Container workers are unaffected; they run with full control inside
their sandbox.

## Accounts

An Agent's `account` uses the same selection machinery that every pooled spawn
uses. `strategy` is one of the registered selection strategies (`static`,
`least_usage`), and `accounts` narrows the candidates exactly as a spawn's
`account_allow` does. Naming accounts also authorizes rotation between them.
See [../accounts/architecture.md](../accounts/architecture.md#selection).

**Precedence.** An Agent's account policy governs that Agent's runs, and the
project's pool covers everything else:

1. An explicit `account_id` pin on the spawn wins. A pin is a deliberate
   operator choice and is never rotated.
2. Otherwise, a spawn by Agent uses the Agent's policy. Its `strategy`
   replaces the default strategy, and its `accounts` replace any
   `account_allow` the caller sent.
3. Otherwise (no Agent policy, or a spawn by raw backend), the project's pool
   applies with the default strategy. That pool is the project's own accounts,
   falling back to the tenant-wide accounts.

**Scope is an isolation boundary, not a preference.** An Agent's list filters
the project's pool; it never widens it. An account scoped to project A is never
used for project B, even when an Agent used by both names it.

**Spend.** An Agent has spend limits of its own; see
[Spend limits](#spend-limits). The project's and the tenant's windows govern
every run as before, and each account's own limits apply when its credential is
released.

**Checked on save.** The catalog refuses each of the following:

- a strategy that is not registered;
- an account the tenant does not hold;
- an account of another provider than the Agent's backend draws on (for
  example, a claude account on a codex Agent);
- any account on a backend that pools none (for example, openvino).

## Spend limits

An Agent takes spend limits just as a project does, using the same
`SpendLimits` type: sliding windows, each capping tokens, list-price dollars or
both.

```yaml
spend:
  windows:
    - {name: daily, size: 86400000000000, max_equivalent_micros: 20000000}   # $20 a day
```

- **Measured over** every turn of every session that ran as the Agent, pooled
  or local, whichever project it served. Each turn's ledger entry carries
  `resolved_agent_id`, and a copy is written to the Agent quota plane
  ([../accounts/architecture.md](../accounts/architecture.md#five-keyspaces-over-one-record)).
- **Checked at spawn**, per Agent, as a spawn by reference walks its
  candidates. A pool passes an over-budget Agent to its next member, and the
  switch is audited with reason `over_budget`. A spawn naming the Agent alone is
  refused with `AgentSpendExhaustedError`, which names the window, the axis,
  the usage and when the window frees up. That error is 503 on REST and
  `ResourceExhausted` on gRPC, like an exhausted pool.
- **A task attempt defers.** The refusal is the fault `agent_over_budget`. It
  is not charged, and the task is requeued until the window frees up. It is
  never parked for a person.
- **Fail closed.** Limits that cannot be checked (no ledger wired, a failed
  read) refuse the spawn with `AgentSpendUndecidableError` (503 /
  `Unavailable`). That refusal does not fail over, because the next Agent's
  window would be read from the same ledger.
- **Project and tenant limits are unchanged.** Admission still defers a spent
  project or tenant, and every turn still counts against both.

A malformed window is refused when the Agent is saved (`agent.spend`, 400).
The spend report can be narrowed to one Agent (`agent`) to show its windows and
live usage; the Cost page edits them in place.

## Failover

A spawn by pool (`SpawnSession` with `pool_id`) tries the members in order.
It moves to the next member when one of these conditions stops the current
one:

| reason | when |
|---|---|
| `account_exhausted` | every account the Agent may use is at its limit |
| `account_expired` | the credential is expired or revoked |
| `rate_limited` | the provider answered 429 |
| `backend_unavailable` | no node serves the backend |
| `at_capacity` | the Agent is at `max_concurrent` |
| `held_for_role` | capacity is reserved for another role |
| `over_budget` | one of the Agent's own spend windows is spent |

The response names the Agent that ran (`resolved_agent_id`) and every member
it passed (`failovers`). Each switch is audited as
`orchestration:agent_failover`. When every member refuses, the spawn fails
with `AgentPoolExhaustedError`, which lists every attempt (HTTP 503; gRPC ResourceExhausted).

**Review seats** keep their own forge identity (`login`, `token_secret_ref`),
so failover between seats happens in the review runner rather than inside a
pool. A seat whose Agent cannot take the work passes it to the next seat, in
binding order, **of the same standing**: counting seats pass only to counting
seats, and advisory seats only to advisory seats. Each switch is audited as
`sdlc:review_seat_failover`.

The **independence rule** reads the model family of every Agent that the
worker reference can resolve to. A counting seat may not share a family with
any of them, and a Claude-family reviewer is refused outright.

**A per-PR reviewer override** (`quicbaas sdlc reviewer set`) never cancels
anything. A review already running for that pull request finishes normally,
and every new review dispatch for it uses the chosen seat. The selection
reports `redispatched: true` when a review by the chosen seat started at once,
because none was running. See
[review.md](../sdlc/review.md#the-per-pull-request-override).

## Surface

The verbs below manage the catalog. `prompt_append` travels on the spawn, job,
task and binding verbs of every transport (REST, gRPC, Unix, QUIC, MCP,
embedded, both SDKs and the CLI).

| transport | where |
|---|---|
| REST | `/api/v1/agents`, `/api/v1/agents/{id}`, `…/{id}/used-by`; `/api/v1/agent-pools` and the same under it. `?tenant_id=` targets a tenant; `agents:read` / `agents:write` |
| gRPC / Unix | `quicbaas.agents.v1.AgentCatalogService` (12 RPCs) |
| MCP | `quicbaas_agents_{list,get,create,update,delete,used_by}`, `quicbaas_agent_pools_{…}` |
| Go SDK | `client.Agents()` (`sdk/go/client/agents`); QUIC and embedded carry it too |
| TypeScript | `client.agents` (REST), `parseAgentRef` / `formatAgentRef` |
| CLI | `quicbaas agents list\|get\|create -f\|update <id> -f\|delete\|used-by`, `quicbaas agents pools …` |

The CLI and SDKs write a reference as `agent:<id>` or `pool:<id>`. A bare ID
is read as an Agent. Examples: `quicbaas orchestrator spawn --pool coders
--role coding`, `quicbaas orchestrator run --lead pool:leads`, and
`quicbaas project update --default-lead-agent agent:claude-lead`.

## Migration

The stack is pre-v1, so there is one wire format and no aliases. Records that
were stored before Agents existed are migrated **once, at boot**:

- **Bindings.** Each distinct `{backend, model, image, reasoning_effort}` in
  a tenant's bindings becomes one Agent, and every slot is rewritten to
  reference it. The Agent's ID is `<backend>-<role>` (for example
  `claude-coder` or `codex-reviewer`), with `-2`, `-3` and so on appended on a
  clash. A tuple that is used in several roles gains each role. An Agent an
  operator already defined with the same tuple is reused. When a tuple has no
  model, the backend's default is used: claude gets `claude-opus-5-5` and
  codex gets `gpt-5.5`. A tuple with no default and no model cannot be
  migrated; its binding is logged and left for an operator to rebind. A
  tenant whose bindings were migrated also gets a `claude-lead` Agent.
- **Projects.** A free-string `default_lead_agent` names no Agent. It is
  cleared, and the project is logged so an operator can set a reference.
- **Configuration.** `sdlc.triage_backend`, `sdlc.triage_model` and
  `orchestration.lead_agent.{backend,model,system_prompt}` are refused at
  load and the error names the replacement. Use `sdlc.triage_agent` and
  `orchestration.lead_agent.{agent_id,pool_id}`.

The migration is idempotent, because a migrated record holds references, and
it visits the system tenant and every IAM tenant. **A tenant whose barrier is
sealed at boot** is migrated when its barrier is unsealed. The per-tenant
barrier facade informs its unseal observers (`service.TenantUnsealHooks`)
before the unseal call returns, so the tenant's first request already reads
migrated records.
