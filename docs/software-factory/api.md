# Software Factory — API

> **Status: NORMATIVE** · Last verified: 2026-09-12
> Every capability of the Software Factory on every transport, with
> permissions: the orchestration engine, agentic projects, pooled provider
> accounts and the SDLC module. Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

The engine's own capabilities come first, then projects and accounts, then the
SDLC module's seven verbs. Prompt sets have no transport surface yet: the
prompts are embedded markdown with no store behind them, and the planned verbs
are in [prompt-sets.md](prompt-sets.md).


## List paging, sorting and search

Every list endpoint below — jobs, tasks, sessions, backends, runners, workflows,
artifacts, projects and accounts — takes the SAME four parameters and answers with the SAME
envelope. The burn-in tenant's job list is ~2,000 records; a listing that
answers with all of them is not an answer a browser can render.

| Parameter | Type | Default | Meaning |
|---|---|---|---|
| `limit` | int | 25 | Page size. Capped at 100 — a larger request is served the cap, not refused. `limit=0` and an absent `limit` both mean 25. |
| `offset` | int | 0 | Records to skip. |
| `sort` | string | the endpoint's natural order | `<field>` or `<field>:desc` (`:asc` is accepted too). The field must be one the endpoint documents below; anything else is **400** with a typed error naming the field and the allow-list. |
| `q` | string | — | Case-insensitive substring match over the endpoint's documented text fields. |

`limit` or `offset` that is not a non-negative number is a **400** naming the
parameter. Every filter COMPOSES with search and with paging, and `total`
counts the intersection rather than the page.

**An unknown filter VALUE is not an error — it simply matches nothing.** A
project id, an agent id or a runner name may legitimately name no record, and
an empty page is the honest answer to "what is agent-9 running". There are
exactly two exceptions, both of which are **400** with a typed error:

- a `statuses` value outside the state enum (`orchestration.ParseTaskStatuses`
  / `ParseJobStates`), because a state the engine does not have cannot be a
  question about this tenant's records — answering "there are none" would
  report the caller's typo as a fact. The refusal names the offending value and
  lists the states that exist;
- a `sort` field outside the endpoint's allow-list, as above.

`?status=` on the sessions listing likewise refuses a lifecycle state the
daemon does not have.

The response keeps the records under the key it always used and adds the
envelope beside it:

```json
{
  "jobs": [ ... ], "total": 1987, "limit": 25, "offset": 50, "next_offset": 75,
  "facets": {
    "state":      { "executing": 12, "completed": 1870, "failed": 105 },
    "runner":     { "sdlc": 1400, "triage": 500, "review": 87 },
    "project_id": { "go-quicbaas": 1987 },
    "agent_id":   { "agent-1": 900, "agent-2": 1087 }
  }
}
```

- `total` is the count AFTER filters and search, BEFORE paging.
- `limit` and `offset` are the values the server APPLIED, which is not always
  what was asked for.
- `next_offset` is ABSENT on the last page. Its absence is how a client knows
  it has reached the end; do not compute it from `total`.
- `facets` is what a filter bar renders beside each choice — see below.

The fleet listing also keeps its `count`, which is the number of records in
that response — `total` is how many matched.

### Per-endpoint fields

| Endpoint | Natural order | `sort` fields | `q` searches | Filters | Facets |
|---|---|---|---|---|---|
| `GET /api/v1/orchestrator/jobs` | `started_at:desc` | `agent_id`, `description`, `job_id`, `runner`, `started_at`, `state`, `task_id`, `updated_at` | job id, description, runner, state, task id | `task_id`, `runner`, `project_id`, `agent_id`, `statuses` (comma-separated), `job_ids` (comma-separated) | `state`, `runner`, `project_id`, `agent_id` |
| `GET /api/v1/orchestrator/tasks` | `created_at:desc` | `agent_id`, `attempts`, `created_at`, `id`, `project_id`, `status`, `subject`, `updated_at` | id, subject, status, project id | `statuses` (comma-separated), `project_id`, `agent_id`, `job_id`, `auto_spawn` (`true`/`false`), `team_name`, `assigned_to`, `created_by`, `status` | `status`, `project_id`, `agent_id` |
| `GET /api/v1/orchestrator/sessions` | `created_at` (**oldest first** — this listing has always answered in creation order; ask for `sort=created_at:desc` to invert it) | `agent_id`, `backend`, `created_at`, `last_activity`, `model`, `project_id`, `runner`, `session_id`, `state` | session id, backend, model, state | `backend`, `status`, `project_id`, `job_id`, `runner`, `agent_id` | `backend`, `state`, `runner`, `agent_id` |
| `GET /api/v1/orchestrator/backends` | `name` | `default_model`, `name`, `type` | name, type, default model | `tenant_id` | — |
| `GET /api/v1/orchestrator/runners` | `name` | `name` | runner name, state, label | — | — |
| `GET /api/v1/fleet` | `registered_at:desc` | `backend`, `host`, `id`, `last_seen`, `name`, `registered_at`, `status` | id, name, and every backend it serves (primary + capabilities) | `status`, `backend` (membership — see below), `host`, `agent_type` | `status`, `backend` |
| `GET /api/v1/orchestrator/workflows` | `name` (workflows carry no timestamp) | `mode`, `name`, `project_id` | name, which is also the identifier | `project_id`, `mode` | `project_id`, `mode` |
| `GET /api/v1/orchestrator/tasks/{taskID}/artifacts` | `name` | `kind`, `mime_type`, `name`, `size` | name, kind, MIME type | `tenant_id`, task id path segment | — |
| `GET /api/v1/projects` | `created_at:desc` | `autonomous`, `created_at`, `id`, `name`, `updated_at` | id, name | `tenant_id` (required), `autonomous` (`true`/`false`) | `autonomous` |
| `GET /api/v1/agent-accounts` | `created_at:desc` | `created_at`, `display_name`, `id`, `provider`, `status`, `updated_at` | id, display name, provider | `provider`, `status`, `project_id` | `provider`, `status` |
| `GET /api/v1/orchestrator/attention` | `status_changed_at` (**oldest first**) | `id`, `kind`, `project_id`, `status_changed_at`, `subject` | id, subject, project name, issue ref, question, recommendation, message | `project_id`, `kind` | `project_id`, `kind` |

The allow-lists are served from `orchestration.PageSpec` values in
`pkg/orchestration/page_specs.go` and the counts from the `orchestration.FacetSpec`
values beside them in `page_facets.go`; the parameters are parsed once in
`orchestration.ParsePage` / `ParseTaskStatuses` / `ParseJobStates` and applied
once in `orchestration.ApplyFacetedPage`.

**Three filters name something the RECORD DOES NOT CARRY**, and the daemon
resolves them for the listing:

- `agent_id` on the tasks listing is the fleet node of the task's LATEST
  ATTEMPT. A task record names no agent — `assigned_to` is filled only when an
  agent claims a task and the engine dispatches most of them — so the answer
  lives on the attempt's job and is resolved onto each row (which is also why
  a task record in a paged answer carries `agent_id`).
- `job_id` on the tasks listing is the reverse binding: the task whose latest
  attempt ran as that job. A CONCLUDED attempt does not match — the task is no
  longer running as it.
- `runner` and `agent_id` on the sessions listing come off the session's job,
  the same read that fills the row's `runner` badge.

**`backend` on the fleet listing is MEMBERSHIP, not equality.** A daemon
started with `--backends acp` (the group) serves claude, codex and opencode from one
record; `?backend=codex` matches it, and the `backend` facet counts it under
each backend it serves.

### Facets

Every list response carries a `facets` block: per faceted field, how many
records carry each value.

```json
"facets": { "status": { "pending": 12, "completed": 30 }, "project_id": { … } }
```

**The counts are taken over the set the SEARCH left but the FACETS did not
narrow** — the standard faceted-search reading, where each count answers "how
many would selecting this yield". Select `statuses=blocked` and the counts
beside `pending` and `completed` keep their values, so an operator can change
their mind without clearing the filter first; `total`, by contrast, is the
number of records actually selected. A value no record carries is absent from
the counts rather than present as zero, and an empty field is not counted at
all ("no project" is the absence of a choice, not a choice).

The block is ABSENT from an answer the server did not page — an in-process
caller that asks for the whole collection renders no filter bar, and computing
the counts for it would charge every internal sweep for a UI it does not draw.

**Two filters exist for the sake of a PAGE, not of a query.** `job_ids` on the
jobs listing is how a caller holding a page of something else resolves the jobs
its rows name in ONE request instead of listing the tenant (which paging
removed) or one request per row. The server answers it BY KEY — one read per
named job — and never by scanning the tenant's job history, whose cost on a
qrdb-backed node is the whole store's, not the prefix's. The tasks and sessions
listings resolve their rows' jobs the same way, so each task row already
carries `agent_id` (its latest attempt's agent) and needs no follow-up job
request. `auto_spawn` on the tasks listing is the
dispatch-mode filter the Tasks view offers: a tri-state boolean, where absent
narrows nothing, `true` is the tasks that asked for immediate dispatch and
`false` the ones queued for a claim. A value that is neither is a **400**
naming the parameter (`orchestration.ParseOptionalBool`).

### The session row says which role it played

Each record of `GET /api/v1/orchestrator/sessions` carries `job_id` — the
orchestration job the session works for — `runner`, the job runner that drove
it (`sdlc`, `review`, `triage`, or `workflow` for the engine's own), and
`agent_id`, the fleet node it ran on. All three are omitted for a session
that belongs to no job, and `runner`/`agent_id` are omitted when the job is no
longer on record: no runner means "not observed", never a guess.

The daemon resolves both off the session's job in ONE pass over the tenant's
jobs, BEFORE the page is cut — which is a change from when `runner` was only a
badge to render. They are filters and facets now, and a listing that resolved
them for the page alone could neither narrow to the reviewer's sessions (the
matches that fell off page one would never be found) nor count them. The UI's
Role badge reads `runner` and nothing else
(`cmd/quicbaas/frontend/src/lib/sessions.ts`).

### The job row names its forge issue

Each record of `GET /api/v1/orchestrator/jobs` carries `source` — the origin of
the TASK the job is an attempt at, in the task's own shape
(`{kind, ref, url}`), so `source.ref` reads `jeremyhahn/go-quicbaas#62` for a
job adopted from a forge issue. It is omitted for a job bound to no task, for a
task that records no origin, and for a task the daemon cannot read: no source
means "not observed", never a guess.

The daemon resolves it for the page it is answering, memoised per task, so a
page of twenty-five costs at most twenty-five task reads and a whole attempt
history of one task costs one. **The listing is the only place a consumer can
get this**: the jobs view is server-paged and holds no tasks, so before
go-quicbaas#976 it named a row's issue by hunting the job's task in whatever
page of tasks it happened to be holding — which worked on page 1 and blanked
on page 2. `GET /api/v1/orchestrator/jobs/{id}` carries the same field beside
the rest of the task provenance.

Every transport carries it, on the listing and on the detail read alike: REST
and the transports that delegate to it, the embedded transport, the MCP
`quicbaas_orchestrator_list_jobs` and `quicbaas_orchestrator_get_job_status`
tools (which leave the key out altogether when there is no origin to name),
both SDKs, and gRPC — `OrchestratorJobSummary.source` on `ListJobs` and
`OrchestratorGetJobStatusResponse.source` on `GetJobStatus`, both the same
`TaskSource` message a task already uses. The gRPC listing reads the same
paged verb with the request's page, so the source is resolved for the rows
answered, exactly as over REST.

**Other transports.** QUIC and Unix carry the REST contract unchanged (they
delegate to it). The Go SDK's list requests embed `page.Params` and carry each
listing's own narrowings as named fields; its list responses embed
`page.Envelope`, whose `Facets` member is the same counts block
(`sdk/go/client/page`). The embedded in-process transport honours both. The
MCP tools `quicbaas_orchestrator_list_jobs`,
`quicbaas_orchestrator_list_sessions` and `quicbaas_fleet_list` declare
the same argument names REST reads — `agent_id`, `runner`, `project_id`,
`job_id`, `backend`, `statuses` — and return the same envelope fields,
`facets` included. `project_list` and `quicbaas_agent_account_list` take the
same `limit`/`offset`/`sort`/`q` arguments (go-quicbaas#975) and answer with
the envelope under their existing `projects`/`accounts` key: a call without
arguments returns the newest 25, not the tenant's whole registry.

**gRPC** carries the PAGING half of the contract (go-quicbaas#973). The
`ListJobs`, `ListTasks`, `ListSessions` and `ListFleetNodes` requests
(`api/proto/orchestrator.proto`), `ListProjects` (`project.proto`) and
`ListAccounts` (`account.proto`) take `limit`, `offset`, `sort` and `q` as
request fields, read by the same `orchestration.ParsePage` and checked against
the same allow-lists (`pkg/orchestration/page_specs.go`); a negative `limit`
or `offset`, or a sort field outside the allow-list, is `InvalidArgument`
naming it. Their responses carry `total`, `limit`, `offset` and `next_offset`,
where `next_offset` is `0` on the last page — proto3 has no absent integer, and
no following page can start at offset 0. An RPC that names no paging gets the
default page of 25, as a REST request with no `?limit=` does. The existing
counts keep their meanings: `ListTasksResponse.total_count` equals `total`
(the matched count), and `ListFleetNodesResponse.count` is the size of the page.
The Go SDK gRPC transport — and the Unix transport, which delegates to it —
sends `page.Params` and fills `page.Envelope`. gRPC does NOT yet carry the
listings' newer narrowings (`agent_id`, `runner`, `statuses`, `job_ids`, …) or
the `facets` block; the proto has no workflows listing RPC. The Go SDK routes no
`ListWorkflows` verb on any transport, so the workflows narrowings are
reachable over REST, MCP and the TypeScript SDK but not through
`sdk/go/client/orchestrator`.

### TypeScript SDK

One params type and one envelope type for every listing (`sdk/typescript/src/page.ts`):

```ts
import type { PageParams, PageResult } from '@quicbaas/sdk';

const page = await client.orchestrator.listJobsPage(tenantID, {
  limit: 25, offset: 50, sort: 'started_at:desc', q: 'wire',
  statuses: ['executing'], runner: 'sdlc',       // the endpoint's own filters
  agent_id: 'agent-1',
});
page.items; page.total; page.limit; page.offset; page.next_offset;
page.facets?.['state']?.['failed'];              // what the bar renders
```

| Verb | Params | Endpoint |
|---|---|---|
| `orchestrator.listJobsPage(tenantID, page?)` | `JobPageParams` (`task_id`, `runner`, `project_id`, `agent_id`, `statuses[]`, `job_ids[]`) | jobs |
| `orchestrator.listTasksPage(tenantID, page?)` | `TaskPageParams` (`status`, `statuses[]`, `project_id`, `agent_id`, `job_id`, `team_name`, `assigned_to`, `created_by`, `auto_spawn`) | tasks |
| `orchestrator.listSessionsPage(tenantID, page?)` | `SessionPageParams` (`backend`, `status`, `project_id`, `job_id`, `runner`, `agent_id`) | sessions |
| `orchestrator.listFleetNodesPage(tenantID, page?)` | `FleetNodePageParams` (`host`, `status`, `node_type`, `backend`, `model`) | nodes |
| `orchestrator.listWorkflowsPage(tenantID, page?)` | `WorkflowPageParams` (`project_id`, `mode`) | workflows |
| `projects.listProjectsPage(tenantID, page?)` | `ProjectPageParams` (`autonomous`) | projects |
| `accounts.listAccountsPage({tenant_id, …})` | `AccountPageParams` (`provider`, `status`, `project_id`) | accounts |

`PageResult.facets` carries the counts, typed `Facets`
(`Record<string, Record<string, number>>`) and UNDEFINED when the transport
could not page — never an empty object a bar would render as "0 of
everything". A count that is not a number is dropped rather than passed
through.

`PageResult.items` holds the records whatever the wire calls them (`jobs`,
`tasks`, …): one envelope type per SDK rather than one per resource. The
sessions page maps the wire's `state`/`last_activity` onto `status`/`last_active`
exactly as the unpaged `listSessions` does, `runner` and `model` included.

**The unpaged methods still exist and are still one page.** `listJobs`,
`listTasks`, `listSessions`, `listFleetNodes` and `listWorkflows` are unchanged and
send no `limit` — which now means they receive the server's default page of 25
and cannot tell that from a complete answer. A caller that needs the count, or
the page after, asks the `*Page` verb.

**A transport that cannot page says so by answering the whole set.** The Wails
bindings take the SDK request structs verbatim, so the paging parameters DO
reach the daemon over desktop IPC; what comes back is a projected array with no
envelope, which the SDK reports as a single page (`total` = its length, no
`next_offset`) rather than as a first page of an unknown number. gRPC-Web,
QUIC, Unix and MCP have no TypeScript transport wired yet: their `*Page` verbs
delegate to their own unpaged sibling, so a caller gets that transport's typed
refusal rather than an invented page. None of them throws "paging not
supported".

A 200 whose body is not a listing — no records under the endpoint's key — is a
typed `MalformedPageError` rather than an empty page, because a table rendering
"0 of 0" for a failed read tells the operator something untrue.


## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.Orchestrator` — SDK package `sdk/go/client/orchestrator`

`ListJobs`, `ListTasks`, `ListSessions`, `ListFleetNodes`, `ListBackends`,
`ListRunners`, `ListWorkflows` and `ListTaskArtifacts` are
paged, sorted and searched per
[List paging, sorting and search](#list-paging-sorting-and-search).

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 38; MCP tools registered server-side: 33.

MCP tools: `quicbaas_fleet_heartbeat`, `quicbaas_fleet_list`, `quicbaas_fleet_register`, `quicbaas_fleet_remove`, `quicbaas_fleet_status`, `quicbaas_orchestrator_broadcast`, `quicbaas_orchestrator_cancel_job`, `quicbaas_orchestrator_close`, `quicbaas_orchestrator_cost`, `quicbaas_orchestrator_decompose`, `quicbaas_orchestrator_discover`, `quicbaas_orchestrator_get_job_status`, `quicbaas_orchestrator_health`, `quicbaas_orchestrator_inference`, `quicbaas_orchestrator_list`, `quicbaas_orchestrator_list_backends`, `quicbaas_orchestrator_list_jobs`, `quicbaas_orchestrator_list_runners`, `quicbaas_orchestrator_list_sessions`, `quicbaas_orchestrator_prompt`, `quicbaas_orchestrator_register`, `quicbaas_orchestrator_rotate_account`, `quicbaas_orchestrator_session_conversation`, `quicbaas_orchestrator_session_history`, `quicbaas_orchestrator_spawn`, `quicbaas_orchestrator_start_job`, `quicbaas_orchestrator_status`, `quicbaas_orchestrator_task_block`, `quicbaas_orchestrator_task_create`, `quicbaas_orchestrator_task_delete`, `quicbaas_orchestrator_task_readmit`, `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `FleetNodeHeartbeat` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `BlockTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CancelJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ClaimTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CloseSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CompleteTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CreateTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `CreateWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `DecomposeTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `DeleteTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `DeleteWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `DisableFleetNode` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `EnableFleetNode` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetFleetNode` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetJobStatus` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetSessionConversation` | `GET /api/v1/orchestrator/sessions/{sessionID}/history` | yes | yes | yes | yes | `orchestrator:status` |
| `GetSessionHistory` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `GetSpendReport` | `GET /api/v1/orchestrator/cost` | yes | yes | yes | yes | `orchestrator:status` |
| `GetCostSettings` | `GET /api/v1/orchestrator/cost-settings` | yes | yes | yes | yes | `orchestrator:status` |
| `SetCostSettings` | `PUT /api/v1/orchestrator/cost-settings` | yes | yes | yes | yes | `cost_settings:write` |
| `GetTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetTaskArtifact` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `Health` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Inference` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `InferenceStream` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListFleetNodes` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListBackendModels` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListBackends` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListJobs` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListRunners` | `GET /api/v1/orchestrator/runners` | yes | yes | yes | yes | `orchestrator:status` |
| `ListSessions` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListTaskArtifacts` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListTasks` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListWorkflows` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `PromptSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ReadmitTask` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RegisterFleetNode` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RemoveFleetNode` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RotateSessionAccount` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SessionStatus` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SpawnChildJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SpawnSession` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `StartJob` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubmitTaskArtifacts` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubscribeJobEvents` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `SubscribeSessionEvents` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |
| `UpdateWorkflow` | dedicated handler (path not statically recoverable) | ? | yes | yes | MISSING | — |

#### `GET /api/v1/orchestrator/runners`

Every registered `JobRunner` and the ordered, non-terminal states it declares,
so a consumer renders a job's progress from the runner that is driving it
instead of a hardcoded copy of the engine's states.

```json
{"runners": [{"name": "sdlc",
              "states": [{"state": "initializing", "label": "Initializing", "session_backed": false}]}],
 "count": 1, "total": 1, "limit": 25, "offset": 0}
```

Registry data, not tenant data: the runners a daemon can drive are the same
for every caller, so no tenant is derived. It is still gated on the
orchestrator service being present — a node that runs no orchestrator drives
no jobs — identically on REST, embedded and MCP. The listing accepts `limit`,
`offset`, `sort` and `q` per
[List paging, sorting and search](#list-paging-sorting-and-search). The permission is
`orchestrator:status`, what reading the job already requires, rather than a
new action in a resource family go-qrdb owns.

Reachable from **every** transport (#606): REST, gRPC
(`quicbaas.orchestrator.v1.OrchestratorService/ListRunners`), Unix — the same
gRPC wire over a domain socket — QUIC — the REST surface over HTTP/3 — MCP
(`quicbaas_orchestrator_list_runners`) and the embedded transport, plus the Go
SDK (`client.Orchestrator().ListRunners`), the TypeScript SDK and the Wails
desktop binding. The gRPC `OrchestratorListRunnersRequest` carries paging
fields but no tenant,
for the same reason the REST route derives none. The SDLC module's sibling
`GET /api/v1/sdlc/forges` (which forge adapters this build carries) has the
same shape and was wired across the same transports in the same change; see
[The SDLC module](#the-sdlc-module).

The seam itself is documented in [job-runners.md](job-runners.md).

#### `GET /api/v1/orchestrator/cost` — the consumption ledger, rolled up

One scope of the consumption ledger: the tokens spent under it, the drill-down
to the level below, and the spend-policy status of every pooled account that
paid.

```
GET /api/v1/orchestrator/cost
  ?tenant_id=   optional; must be the authenticated principal's own tenant (403 otherwise)
  &project_id=  narrow to one project
  &task_id=     narrow to one task      (requires project_id)
  &job_id=      narrow to one job       (requires task_id)
  &session_id=  narrow to one session   (requires job_id)
  &round=       narrow to one round     (requires session_id)
  &from=        only turns at or after this RFC 3339 instant
  &to=          only turns before this RFC 3339 instant (requires from;
                from alone ranges up to now)
  &host=        only turns that ran on this host ("-" for none)
  &agent=       only turns of sessions that ran as this tenant Agent ("-"
                for raw-backend sessions); the report then carries the
                Agent's own limits (agent_policy_status, agent_windows)
```

```json
{"level": "project", "tenant_id": "acme", "project_id": "p1",
 "project_name": "go-truststrap", "currency": "USD",
 "entries": 4, "input_tokens": 1000, "output_tokens": 1200,
 "cache_read_tokens": 3000, "cache_write_tokens": 0, "total_tokens": 5200,
 "equivalent_micros": 36500, "priced_entries": 4, "unpriced_entries": 0,
 "billed_micros": 0, "billed_entries": 4, "unbilled_entries": 0,
 "duration_ms": 184000, "successes": 4, "failures": 0,
 "child_level": "task",
 "children": [{"id": "task-1", "level": "task", "total_tokens": 5200, "equivalent_micros": 36500, "priced_entries": 4, "...": "the same totals"}],
 "accounts": [{"account_id": "a1", "display_name": "alice-work",
               "billing": {"mode": "subscription", "monthly_fee_micros": 200000000},
               "total_tokens": 5200, "equivalent_micros": 36500, "billed_micros": 0,
               "policy_status": "none"}],
 "models": [{"engine": "claude", "model": "claude-opus-4-5-20251101", "total_tokens": 5200, "equivalent_micros": 36500}]}
```

ONE VERB FOR EVERY LEVEL: the ledger's attribution chain is a PATH, so a
project's spend and a job's spend are the same prefix scan at two depths, and
each level may be supplied only if every level above it is. A scope that skips
one is refused (`400`) rather than answered by a filtered scan of the tenant's
history.

Every level of the report — the scope, each drill-down row, each account and
each model — carries the same totals, with TWO money figures: the list-price
`equivalent_micros` and the `billed_micros` actually added to the bill (zero
per turn on a subscription). A client MUST consult `priced_entries` and
`billed_entries` before rendering them: zero means unmeasured, rendered as a
dash, never as a zero; `unpriced_entries` / `unbilled_entries` beside them make
the figure a floor — see
[cost-enforcement.md](cost-enforcement.md#pricing-what-a-turn-costs-twice). A drill-down row at the project level carries the project's `name`, and a
report scoped to a project carries `project_name`; both are absent for a
project that no longer exists, whose spend stays under its id.

`from`/`to` narrow any scope to a time range, echoed as `from`/`to`. A tenant
or project scope is read from its time-indexed quota plane, so a ranged read
costs what the range is worth. A **tenant-level** report also carries each
subscription's monthly fee pro-rated over the range
(`accounts[].subscription_fee_micros`, summed as `subscription_fee_micros`);
an unranged report shares fees from the first recorded turn to now. Below the
tenant level no fee is shared, because one subscription serves every project.
`policy_status` likewise distinguishes an account with no
cap (`none`) from one this deployment could not read (`unknown`). A deployment
recording no consumption answers `503`, not an empty report. Every report
breaks its totals down by host (`hosts`), and `host` narrows any scope to the
turns one host ran; totals count `measured_energy_entries` beside
`electricity_entries`.

Reachable from **every** transport: REST, gRPC
(`quicbaas.orchestrator.v1.OrchestratorService/GetSpendReport`), Unix — the
same gRPC wire over a domain socket — QUIC — the REST surface over HTTP/3 —
MCP (`quicbaas_orchestrator_cost`) and the embedded transport, plus the Go SDK
(`client.Orchestrator().GetSpendReport`), the TypeScript SDK
(`orchestrator.getSpendReport`) and the Wails desktop binding
(`OrchestratorBinding.GetSpendReport`) and the CLI (`quicbaas orchestrator
cost`). Permission `orchestrator:status`.


### `quicbaas.Project` — SDK package `sdk/go/client/project`

`List` is paged, sorted and searched per
[List paging, sorting and search](#list-paging-sorting-and-search).

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 8; MCP tools registered server-side: 5.

MCP tools: `project_create`, `project_delete`, `project_get`, `project_list`, `project_update`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Create` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Delete` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Get` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetByName` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GrantFleetNodeRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `List` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RevokeFleetNodeRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Update` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |

### `quicbaas.Account` — SDK package `sdk/go/client/account`

`List` is paged, sorted and searched per
[List paging, sorting and search](#list-paging-sorting-and-search).

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 9; MCP tools registered server-side: 9.

MCP tools: `quicbaas_agent_account_create`, `quicbaas_agent_account_delete`, `quicbaas_agent_account_get`, `quicbaas_agent_account_list`, `quicbaas_agent_account_revoke`, `quicbaas_agent_account_rotate`, `quicbaas_agent_account_set_default`, `quicbaas_agent_account_update`, `quicbaas_agent_account_usage`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `List` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Usage` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |

## Reference fields are checked on save

A field that names another object the platform controls is checked for
existence by the service layer when it is saved, so every transport refuses a
dangling reference the same way. Stored data is not re-checked on read, and an
update checks only the references it changes.

| Save | Field | Must name |
|---|---|---|
| SDLC bind | `forge.token_secret_ref`, `forge.webhook_secret_ref` | a secret in the tenant |
| SDLC bind | `reviewer.token_secret_ref`, `reviewers[i].token_secret_ref` | a secret in the tenant |
| Project create / update | `git_signing_key_{name,scope,owner}`, `git_ssh_key_{name,scope,owner}` | an SSH key in the KMS; scope defaults to `user`, owner to the tenant |
| Project create / update | `default_workflow` | a workflow registered in the tenant |
| Project create / update | `host_affinity` | a fleet node in the tenant, by agent ID, name or host |
| Project create | `git_credential_secret_ref` | a secret in the tenant |
| Job start | `workflow_name` | a workflow registered in the tenant (a resumed job is not re-checked) |
| Agent register | `tenant_id` | a tenant IAM knows; the system tenant always exists. Nodes without IAM skip this check |

A check fails in one of two ways, and they are never confused:

| Error | Meaning | REST | gRPC | Code |
|---|---|---|---|---|
| `ReferenceNotFoundError` | the store answered and the object is not there | 400 | `InvalidArgument` | `reference_not_found` |
| `ReferenceUnavailableError` | the store could not answer (sealed barrier, no store attached, I/O failure) | 503, retryable | `Unavailable` | `reference_unavailable` |

The message names the field, the kind and the value, e.g.
`forge.token_secret_ref: secret "sdlc/forge/api/token" does not exist`. MCP
returns the same message as the tool error.

### `quicbaas.Agents` — SDK package `sdk/go/client/agents`

The tenant agent catalog: Agents and pools. See [agents.md](agents.md).

| SDK method | REST route | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|
| `ListAgents` | `GET /api/v1/agents` | `ListAgents` | `quicbaas_agents_list` | yes | `agents:read` |
| `GetAgent` | `GET /api/v1/agents/{id}` | `GetAgent` | `quicbaas_agents_get` | yes | `agents:read` |
| `CreateAgent` | `POST /api/v1/agents` | `CreateAgent` | `quicbaas_agents_create` | yes | `agents:write` |
| `UpdateAgent` | `PUT /api/v1/agents/{id}` | `UpdateAgent` | `quicbaas_agents_update` | yes | `agents:write` |
| `DeleteAgent` | `DELETE /api/v1/agents/{id}` | `DeleteAgent` | `quicbaas_agents_delete` | yes | `agents:write` |
| `AgentUsedBy` | `GET /api/v1/agents/{id}/used-by` | `AgentUsedBy` | `quicbaas_agents_used_by` | yes | `agents:read` |
| `ListPools` | `GET /api/v1/agent-pools` | `ListAgentPools` | `quicbaas_agent_pools_list` | yes | `agents:read` |
| `GetPool` | `GET /api/v1/agent-pools/{id}` | `GetAgentPool` | `quicbaas_agent_pools_get` | yes | `agents:read` |
| `CreatePool` | `POST /api/v1/agent-pools` | `CreateAgentPool` | `quicbaas_agent_pools_create` | yes | `agents:write` |
| `UpdatePool` | `PUT /api/v1/agent-pools/{id}` | `UpdateAgentPool` | `quicbaas_agent_pools_update` | yes | `agents:write` |
| `DeletePool` | `DELETE /api/v1/agent-pools/{id}` | `DeleteAgentPool` | `quicbaas_agent_pools_delete` | yes | `agents:write` |
| `PoolUsedBy` | `GET /api/v1/agent-pools/{id}/used-by` | `AgentPoolUsedBy` | `quicbaas_agent_pools_used_by` | yes | `agents:read` |

Refusals map the same way on every transport:

| refusal | REST | gRPC |
|---|---|---|
| invalid request or role mismatch | 400 | InvalidArgument |
| not found | 404 | NotFound |
| already exists | 409 | AlreadyExists |
| in use | 409 | FailedPrecondition |
| catalog unavailable | 503 | Unavailable |
| pool exhausted | 503 | ResourceExhausted |

## The SDLC module

The module's own surface is three verbs on a project's binding, two on that
project's ONBOARDING, three on a pull request's REVIEWER OVERRIDE, one read on
a task's delivery, and one read of the build's forge adapters. Tasks, Jobs and Sessions are the orchestrator's on
every transport; readmitting a blocked task is
`orchestrator:task_readmit`.

### Permissions

| permission | roles | verbs |
|---|---|---|
| `sdlc:read` | admin, operator | read a binding, read a delivery, list the forges, **read a project's onboarding conformance**, read a project's open work, read a pull request's reviewer |
| `sdlc:bind` | admin | attach or replace a binding, **provision (onboard) its repository**, set or clear a pull request's reviewer override |
| `sdlc:unbind` | admin | detach a binding |
| `worker_images:read` | admin, operator — **cross-tenant** | read the worker image refresh's status and last pass |
| `worker_images:refresh` | admin — **cross-tenant** | queue a worker image refresh pass |

The two `worker_images:*` permissions are cross-tenant (qrdb `ScopeCross`):
the refresh rebinds images in EVERY tenant's bindings, so only a system-tenant
identity exercises them. See [the worker image refresh](worker-image.md).

Onboarding sits under `sdlc:bind` and conformance under `sdlc:read`, and the
split is the point rather than a convention: an operator auditing a fleet must
be able to ASK what state a repository is in without being able to change it.

### REST

| method | path | permission |
|---|---|---|
| `GET` | `/api/v1/sdlc/projects/{projectID}/binding` | `sdlc:read` |
| `PUT` | `/api/v1/sdlc/projects/{projectID}/binding` | `sdlc:bind` |
| `DELETE` | `/api/v1/sdlc/projects/{projectID}/binding` | `sdlc:unbind` |
| `POST` | `/api/v1/sdlc/projects/{projectID}/onboarding` | `sdlc:bind` |
| `GET` | `/api/v1/sdlc/projects/{projectID}/conformance` | `sdlc:read` |
| `POST` | `/api/v1/sdlc/projects/{projectID}/adopt` | `orchestrator:task_create` |
| `GET` | `/api/v1/sdlc/projects/{projectID}/open-work` | `sdlc:read` |
| `GET` | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | `sdlc:read` |
| `PUT` | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | `sdlc:bind` |
| `DELETE` | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | `sdlc:bind` |
| `POST` | `/api/v1/sdlc/projects/{projectID}/issues` | `sdlc:issue_create` — 201 |
| `GET` | `/api/v1/sdlc/projects/{projectID}/collaborators` | `sdlc:read` |
| `GET` | `/api/v1/sdlc/tasks/{taskID}/delivery` | `sdlc:read` |
| `GET` | `/api/v1/sdlc/forges` | `sdlc:read` |
| `GET` | `/api/v1/sdlc/worker-image` | `worker_images:read` |
| `POST` | `/api/v1/sdlc/worker-image/refresh` | `worker_images:refresh` — 202; 409 while a pass runs or is queued |
| `POST` | `/api/v1/sdlc/webhook/{projectID}` | public — HMAC over the body |

Tenancy is the caller's own; `?tenant_id=` may only name the bound tenant
(an unbound admin caller must name one). The PUT body is the
`ProjectBinding` JSON; `project_id` may be omitted and must otherwise match
the path. Every `forge:` field is described in [the project binding](sdlc.md#the-project-binding);
`ssh_host_key` is required when the project's repository URL is an ssh one.

```json
{
  "forge": {
    "type": "gitea", "base_url": "https://git.example.com/api/v1",
    "owner": "jeremyhahn", "repo": "go-truststrap",
    "trigger_label": "task:ready",
    "labels": {"in_progress": "factory:in-progress", "in_review": "factory:in-review",
               "merged": "factory:merged", "blocked": "factory:blocked",
               "needs_info": "factory:needs-info", "escalated": "factory:escalated"},
    "token_secret_ref": "sdlc/forge/go-truststrap/token",
    "webhook_secret_ref": "sdlc/webhook/go-truststrap",
    "ssh_host_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA..."
  },
  "strategy": "github-flow",
  "worker": {"pool_id": "coders"},
  "triage": {"agent_id": "openvino-triage"},
  "verification": ["make build", "make test", "make lint"],
  "modules": {"rewrite_prefix": "https://github.com/jeremyhahn/"},
  "review": {"required_approvals": 1, "require_independent_reviewer": true,
             "require_green_checks": true, "reviewers": {"codex": "openai"}},
  "max_review_rounds": 3,
  "ranking": {"default_priority": 0},
  "onboarding": {"protected_branch": "main",
                 "required_status_contexts": ["CI / CI Gate (pull_request)", "quicbaas/review"]}
}
```

The `onboarding` block is optional and every field of it defaults; see
[onboarding](sdlc.md#onboarding-a-repository).
The `modules` block is optional: set `rewrite_prefix` when the project
depends on private Go modules the forge hosts under its owner; see
[private modules](sdlc.md#modulesrewrite_prefix--private-go-modules-resolve-at-the-forge).
`worker` and `triage` are [Agent references](agents.md): `{"agent_id": ...}`
or `{"pool_id": ...}`. The runtime, model, image and reasoning effort live on
the Agent, not the binding. `triage` is optional. When it is absent, the
node's `sdlc.triage_agent` is used, and failing that the worker.

`reviewer` is the project's default reviewer profile and `reviewers` its
alternates. Each alternate needs a `name`, an `agent_id` (an Agent with the
`review` role), and its own `login` and `token_secret_ref`. Seat order is the
failover chain; `counts_toward_merge` is unset (advisory) unless
stated. A bind replaces the whole record, so every client must write back
`reviewers` exactly as it read them. See
[review.md](review.md#reviewer-profiles--a-default-alternates-and-whose-verdict-counts).

```json
"reviewer":  {"agent_id": "codex-reviewer", "login": "codex", "token_secret_ref": "sdlc/reviewer/go-quicbaas/token"},
"reviewers": [{"name": "qwen", "agent_id": "qwen-reviewer",
               "login": "qwen-reviewer", "token_secret_ref": "sdlc/reviewer/go-quicbaas/qwen-token"}]
```

`merge_train` and `clustering` are the two throughput policies. Both are **on
by default**, so both are switched by `disabled`; `max_size` of zero takes the
default (5 changes per train, 4 issues per task). A bind replaces the whole
record, so a client that read `"disabled": true` must write it back. See
[merge-train.md](merge-train.md) and
[triage.md](triage.md#clustering--related-ready-issues-become-one-task).

```json
"merge_train": {"disabled": false, "max_size": 5},
"clustering":  {"disabled": false, "max_size": 4}
```

#### `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer`

Chooses which reviewer profile reviews one pull request. `GET` reads the
selection, `PUT {"profile": "qwen"}` records an operator override, and
`DELETE` clears it. All three answer the selection that now holds:

```json
{"project_id": "go-quicbaas", "pull_request": 1075, "profile": "qwen",
 "overridden": true, "counts_toward_merge": false,
 "status_context": "quicbaas/review/qwen", "updated_at": "2026-09-23T23:00:00Z"}
```

A profile the binding does not carry is a `400`, and so is a project that
binds no reviewer. An unbound project is a `404`.

#### `POST /api/v1/sdlc/projects/{projectID}/issues`

Files an issue on the bound repository through the binding's own forge token
— how the lead splits an issue into follow-ups. Body:

```json
{"title": "Split: the codec half", "body": "…", "labels": ["task:ready"], "parent": 1075}
```

`title` is required (one line, at most 255 characters); `labels` must already
exist on the repository and are refused by name otherwise; `parent` must be an
existing issue and is written as a `Parent: #N` line, which the forge shows on
the parent as a cross-reference (it is not a `Blocked by:` dependency). Answers
`201` with the issue as stored (`number`, `title`, `body`, `state`, `labels`,
`author`, `url`). Every attempt is audited as `sdlc:issue_create`, refusals
included; a node with no audit sink answers `503` rather than file unaudited.
The create is sent once and never retried: an unanswered create is reported,
not repeated.

#### `GET /api/v1/sdlc/projects/{projectID}/collaborators`

The accounts that may rule on the bound repository — its owner first, then
every collaborator, sorted — as `{"users": [{"login": "acme", "full_name": ""}],
"count": 1}`. The binding editor's ruler picker reads it.

#### `POST /api/v1/sdlc/projects/{projectID}/onboarding`

Provisions the bound repository's forge configuration — the webhook that
delivers to this node and the label vocabulary the binding declares — and
answers what it did plus the conformance that follows. Idempotent: a
repository already in the desired state is left alone, and one whose webhook
was deleted by hand gets it back. Existing labels are never renamed,
recoloured or removed.

`PUT .../binding` already does this, so the verb exists for two other cases:
repairing drift a conformance report named, and re-running a fleet loop
without re-sending every binding.

```json
{"project_id": "go-truststrap", "repo": "jeremyhahn/go-truststrap",
 "webhook_url": "https://factory.example.com/api/v1/sdlc/webhook/go-truststrap",
 "webhook_ensured": true, "labels_created": ["factory:in-review"],
 "conformance": {"conformant": true, "checks": [{"name": "webhook", "status": "pass", "findings": 0}]}}
```

#### `GET /api/v1/sdlc/projects/{projectID}/conformance`

Answers "is this project correctly onboarded, and if not, what is wrong".
**Read-only** — repair is the POST above, so no GET mutates a repository.

A non-conformant project is still a `200`: the report IS the answer, and a
status code cannot carry which of several things went wrong. A project that
has no binding is also a report (`binding_invalid`), because a fleet
dashboard asking about twenty-eight projects needs that rendered as a row; a
project that does not exist at all is a `404`.

```json
{"project_id": "go-truststrap", "repo": "jeremyhahn/go-truststrap",
 "conformant": false,
 "webhook_url": "https://factory.example.com/api/v1/sdlc/webhook/go-truststrap",
 "checks": [{"name": "binding", "status": "pass", "findings": 0},
            {"name": "webhook", "status": "fail", "findings": 1},
            {"name": "labels", "status": "pass", "findings": 0},
            {"name": "workflows", "status": "fail", "findings": 1},
            {"name": "protection", "status": "fail", "findings": 1}],
 "findings": [
   {"class": "webhook_missing", "check": "webhook",
    "subject": "https://factory.example.com/api/v1/sdlc/webhook/go-truststrap",
    "detail": "the repository has no delivery to …", "remedy": "onboard the project to provision it"},
   {"class": "stale_workflow", "check": "workflows",
    "subject": ".github/workflows/automerge.yml",
    "detail": "… a second orchestrator competing with quicbaas for the same pull requests",
    "remedy": "delete the file from the repository's default branch"},
   {"class": "protection_context_missing", "check": "protection", "subject": "quicbaas/review",
    "detail": "the protection on main does not require quicbaas/review, so a change can land past it",
    "remedy": "require quicbaas/review on the forge's protection rule for main"}],
 "checked_at": "2026-09-12T12:00:00Z"}
```

Every finding carries a `remedy`, because the repairs differ: a missing
webhook is an `Onboard` call, a stale workflow is a commit, a missing
protection context is a setting only the repository's owner may change. The
classes are listed in [sdlc.md](sdlc.md#what-conformance-checks).

#### `GET /api/v1/sdlc/projects/{projectID}/open-work`

What the factory has in flight on a bound project's forge, read from the
forge through the binding's token: the open pull requests that token's
account (the project's agent) opened, each with its CI state (`success`,
`failure`, `pending`, `none`, by the merge gate's definition of green) and
review state (`approved`, `changes_requested`, `awaiting_review`, from each
reviewer's operative verdict on the current head); and the open issues
carrying a label the module writes for the binding (its display labels, its
trigger label) or `task:blocked`. **Read-only.**

Whole or nothing: a pull request whose checks or reviews cannot be read fails
the request rather than being reported in an unknown state. An unbound
project is a `404`: it has no forge to read.

```json
{"project_id": "go-luks2", "repo": "jeremyhahn/go-luks2", "agent": "agent-go-luks2",
 "pull_requests": [{"number": 210, "title": "…", "head_ref": "issue/209-…", "head_sha": "6a0be839…",
                    "ci_state": "success", "review_state": "approved",
                    "created": "2026-09-22T22:10:00Z", "age_seconds": 259200}],
 "issues": [{"number": 141, "title": "coverage gate", "labels": ["factory:escalated"],
             "created": "2026-09-20T10:00:00Z", "age_seconds": 453600}],
 "read_at": "2026-09-25T20:00:00Z"}
```

#### `GET /api/v1/sdlc/forges`

The forge adapters this build actually carries, so a selector can show an
unavailable platform DISABLED with the reason rather than hide it: a project
may only bind to a forge some adapter serves (`NewForgeBackend` is
fail-closed), and a selector that silently omits `github` says nothing about
why.

```json
{"forges": ["gitea"], "count": 1}
```

BUILD capability, not tenant data, so no tenant is derived and the request
carries no field on any transport. It is still gated on the module running: a
node without it answers `503` / `Unavailable`, which is deliberately distinct
from a `200` listing nothing. Wired on **every** transport (#606) — REST,
gRPC, Unix, QUIC, MCP (`sdlc_forges`) and the embedded transport — plus the Go
SDK (`client.SDLC().ListForges`).

The webhook responds `202 {"accepted":true,"nudged":true}`; every
authentication failure — unknown project, missing secret, bad signature — is
the same `401 unauthenticated`, and a node with the module disabled answers
`503` so the forge retries.

### gRPC / Unix

`quicbaas.sdlc.v1.SDLCService` (`api/proto/sdlc.proto`): `BindProject`,
`UnbindProject`, `GetBinding`, `GetDelivery`, `ListForges`, `Onboard`,
`GetConformance`, `GetOpenWork`, `GetReviewerSelection`, `SetReviewerOverride`,
`ClearReviewerOverride`, `GetWorkerImageStatus`, `RefreshWorkerImage`,
`FileIssue` (`sdlc:issue_create`), `ListCollaborators`, `AdoptIssue`
(`orchestrator:task_create`), each carrying its permission as
`option (qrdb.v1.permission)`. The Unix transport delegates to
gRPC verbatim. A node with the module disabled answers `Unavailable`.

### MCP

Tools `sdlc_bind`, `sdlc_unbind`, `sdlc_binding`, `sdlc_delivery`,
`sdlc_forges`, `sdlc_onboard`, `sdlc_conformance`, `sdlc_open_work`, `sdlc_reviewer`,
`sdlc_reviewer_set`, `sdlc_reviewer_clear`, `sdlc_issue_create`,
`sdlc_collaborators`, `quicbaas_sdlc_worker_image`,
`quicbaas_sdlc_worker_image_refresh`, `sdlc_adopt_issue`, mapped to the same
permissions.

### Go SDK

`sdk/go/client/sdlc`, reached as `client.SDLC()`, on every transport
(`embedded`, `unix`, `grpc`, `quic`, `rest`, `mcp`; each declares
`SDLCCapableTransport` at compile time):

```go
bound, err := client.SDLC().Bind(ctx, &sdlc.BindRequest{TenantID: tenant, ProjectID: projectID, Binding: binding})
binding, err := client.SDLC().Binding(ctx, tenant, projectID)
delivery, err := client.SDLC().Delivery(ctx, tenant, taskID)
err = client.SDLC().Unbind(ctx, tenant, projectID)
forges, err := client.SDLC().ListForges(ctx)
report, err := client.SDLC().Onboard(ctx, tenant, projectID)
conformance, err := client.SDLC().Conformance(ctx, tenant, projectID)
work, err := client.SDLC().OpenWork(ctx, tenant, projectID)
selection, err := client.SDLC().Reviewer(ctx, tenant, projectID, 1075)
selection, err = client.SDLC().SetReviewer(ctx, &sdlc.SetReviewerRequest{TenantID: tenant, ProjectID: projectID, PullRequest: 1075, Profile: "qwen"})
selection, err = client.SDLC().ClearReviewer(ctx, tenant, projectID, 1075)
issue, err := client.SDLC().FileIssue(ctx, &sdlc.FileIssueRequest{TenantID: tenant, ProjectID: projectID, Title: "Follow-up", Labels: []string{"task:ready"}, Parent: 1075})
users, err := client.SDLC().Collaborators(ctx, tenant, projectID)
status, err := client.SDLC().WorkerImageStatus(ctx)   // host verb: no tenant
status, err = client.SDLC().RefreshWorkerImage(ctx)   // queues a pass, returns at once
```

### TypeScript SDK

`client.sdlc.bind(tenantId, projectId, binding)`, `.binding(tenantId,
projectId)`, `.delivery(tenantId, taskId)`, `.unbind(tenantId, projectId)`,
`.onboard(tenantId, projectId)`, `.conformance(tenantId, projectId)`,
`.openWork(tenantId, projectId)`,
`.reviewer(tenantId, projectId, pr)`, `.setReviewer(tenantId, projectId, pr,
profile)`, `.clearReviewer(tenantId, projectId, pr)`, `.fileIssue(tenantId,
projectId, {title, body, labels, parent})`, `.collaborators(tenantId, projectId)`
(`sdk/typescript/src/sdlc.ts`; REST transport).

The worker image verbs are on the orchestrator client, where the desktop app's
Wails binding reaches them: `client.orchestrator.getWorkerImageStatus()` and
`client.orchestrator.refreshWorkerImage()` (REST and Wails; the other browser
transports refuse). The web UI's Fleet page renders them as the *Worker
image* panel.

### CLI

```
quicbaas sdlc bind <project-id> --file binding.yaml
quicbaas sdlc binding <project-id> [--output json|yaml|table]
quicbaas sdlc conformance <project-id>   # reports; exits non-zero when not conformant
quicbaas sdlc onboard <project-id>       # repairs
quicbaas sdlc open-work <project-id>     # the agent's open PRs and the factory-labelled issues
quicbaas sdlc delivery <task-id>
quicbaas sdlc reviewer <project-id> <pr>                   # which profile reviews it
quicbaas sdlc reviewer set <project-id> <pr> <profile>     # operator override
quicbaas sdlc reviewer clear <project-id> <pr>             # back to the default
quicbaas sdlc issue create <project-id> --title T [--body B|--body-file F] [--label L]... [--parent N]
quicbaas sdlc collaborators <project-id>                   # who a ruler may be
quicbaas sdlc unbind <project-id>
quicbaas sdlc worker-image status
quicbaas sdlc worker-image refresh [--wait]
```

`conformance` exits non-zero when the project is not conformant, so it gates a
script the way a test command does — and still prints the report either way,
because an exit code that says "something is wrong" without saying what is the
thing this verb exists to replace.

The binding file is the `ProjectBinding` as YAML or JSON. Tasks, jobs and
sessions: `quicbaas orchestrator …`.

### Task artifacts

Every `Reviewable` and `Complete` publishes two artifacts on the Task so the
generic UI shows the pull request without a module client: `pull_request`
(kind `url`, `ref.path` = the PR URL, metadata `number`, `state`, `branch`)
and `head` (kind `commit`, `ref.sha`, `ref.branch`).

## Attention inbox

The "Needs You" inbox: every blocked task, plus environmental alerts, in one
paged and faceted listing — see
[user.md](user.md#unblocking-the-factory-the-attention-inbox) for the
operator-facing walkthrough. Served by `sdlc.AttentionService`
(`pkg/sdlc/attention_service.go`), which reads through the same task
service and forge credentials every other SDLC verb does.

`GET /api/v1/orchestrator/attention` — natural order `status_changed_at`
(**oldest first**, the opposite default of every other timestamped listing
here: the item that has waited longest is what an operator wants to see
first). `sort`: `id`, `kind`, `project_id`, `subject`, `status_changed_at`.
`q` searches id, subject, project name, issue ref, question, recommendation,
message. Filters: `project_id`, `kind` (`parked_task`, `agent_offline`,
`account_error`, `barrier_sealed`, `project_idle`). Facets: `project_id`,
`kind`.

```json
{
  "items": [{
    "id": "90394657c977f059938a426f7cc785c8",
    "kind": "parked_task",
    "project_id": "cc9c12ee-…", "project_name": "go-quicbaas",
    "subject": "a container-backed spawn cannot mount the workspace",
    "issue_ref": "jeremyhahn/go-quicbaas#981",
    "issue_url": "https://forge.example.com/jeremyhahn/go-quicbaas/issues/981",
    "block_kind": "blocked", "condition": "owner_ruling",
    "question": "…", "recommendation": "…", "truncated": false, "ruling_posted": false,
    "status_changed_at": 1758000000000000000, "attempts": 2,
    "actions": ["approve_recommendation", "answer", "readmit", "close_as_done", "note"]
  }],
  "total": 4, "limit": 25, "offset": 0,
  "facets": { "kind": { "parked_task": 3, "agent_offline": 1 }, "project_id": { "…": 3 } }
}
```

`Question` and `Recommendation` are `sdlc.ExtractAsk`'s reading of the
worker's own words out of `BlockedReason`: it prefers an explicit `## What I
need from you` / `Recommendation` / `Pick one` section and falls back to the
reply's last paragraph. `Truncated` is true when the underlying reply was
capped before it was stored (`AgentTurn.Capped()`), so `Recommendation` may
end mid-sentence. An environmental alert (`agent_offline`, `account_error`,
`barrier_sealed`, `project_idle`) carries `message` and `severity` instead,
and `actions` is always empty — it is observed, not resolved through the task
lifecycle. `project_idle:<project-id>` (severity `warning`) names an
autonomous, unparked project with parked tasks and none pending, claimed or
in progress.

`ruling_posted` is true on a parked task whose issue carries a comment by an
authorized ruler (the repository owner, the binding's `rulers`, or the node's
`sdlc.rulers` for the tenant)
newer than the park — the same test the automatic readmission applies (see
[user.md](user.md#answering-on-the-issue-automatic-readmission)). It is read
for the returned page only, and an unreadable forge leaves it `false` rather
than failing the listing.

`POST /api/v1/orchestrator/attention/{taskID}/resolve` — body `{"action":
"…", "answer": "…"}`, `action` one of `approve_recommendation`, `answer`,
`allow_another_round`, `close_as_done`, `readmit`, `note`. `note` is
accepted on any live task (`pending`, `claimed`, `in_progress`, `blocked`);
every other action only on a `blocked` one. Anything else is refused with
`TaskInvalidStateError` (409) and posts nothing. Posts the ruling
as a comment on the task's forge issue under the bot identity `Owner ruling
(via QuicBaaS)`, THEN moves the task: `note` (which, like `answer`, requires
`answer` text) leaves it where it is; every readmitting action first hands the
issue back to the factory — trigger label on, park labels (`needs_info`,
`escalated`, `blocked`, `task:blocked`) off — then readmits the task;
`close_as_done` takes the park labels off the forge issue (posting the
optional `answer` as its evidence), closes it and completes the task instead,
without readmitting it, through `AgentService.CloseBlockedTask` — the task
store's only `blocked → completed` edge (`CompleteTask` still refuses a
blocked task). `allow_another_round` also raises the task's
review-round budget by one (`Task.ReviewRoundBonus`, bounded by
`orchestration.MaxReviewRoundBonus`) before readmitting; its optional
`answer` scopes the granted round and is posted in the same ruling comment,
ahead of the grant. Returns the
resolved `AttentionItem`. `409` when the task is no longer blocked (another
caller already resolved it); `404` when the task does not exist.

Permissions: `orchestrator:task_list` (list) / `orchestrator:task_readmit`
(resolve) on MCP; the REST route table freezes list at
`orchestrator:status` and resolve at `orchestrator:task_readmit`, matching
the legacy PEP mapper's read/write split for this surface (see
`routes_permissions_generated.go`).

### Transports

Every transport. REST, and QUIC (the REST handlers over HTTP/3); MCP
(`quicbaas_orchestrator_attention_list`,
`quicbaas_orchestrator_attention_resolve`); gRPC as
`quicbaas.sdlc.v1.SDLCService/ListAttention` (`orchestrator:status`) and
`ResolveAttention` (`orchestrator:task_readmit`) — on `SDLCService` because
the SDLC module serves the inbox — and therefore Unix, which delegates to
gRPC; the embedded transport; and the Go and TypeScript SDKs. The gRPC list
carries the same paging fields and envelope, facets included; resolve records
the verified principal as the ruling's actor, never a request field.

### Go SDK

`sdk/go/client/orchestrator`, reached as `client.Orchestrator()`:

```go
page, err := client.Orchestrator().AttentionList(ctx, &orchestrator.AttentionListRequest{TenantID: tenant})
item, err := client.Orchestrator().AttentionResolve(ctx, &orchestrator.AttentionResolveRequest{
    TenantID: tenant, TaskID: taskID, Action: "approve_recommendation",
})
```

### TypeScript SDK

`client.orchestrator.listAttention(tenantID, page?)` /
`.resolveAttention(req)` (`sdk/typescript/src/interfaces.ts`; implemented on
the `rest` and `wails` transports, stubbed elsewhere). The desktop binding
(`OrchestratorBinding.AttentionList`) returns the full paged envelope
(`items`/`total`/`facets`), unlike the older task/job/session bindings,
which return a bare array the TS client re-pages client-side.

## Agent Tools

### quicbaas_orchestrator_discover

Discover agent-enabled projects by scanning a directory tree for `CLAUDE.md` and `.claude/agents/` files.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `root_path` | string | Yes | Root directory path to scan for projects |

**Example request:**
```json
{
  "root_path": "/home/user/sources"
}
```

**Example response:**
```json
{
  "projects": [
    {
      "name": "go-quicbaas",
      "path": "/home/user/sources/go-quicbaas",
      "has_claude_md": true,
      "has_agents_dir": true
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_register

Register a project for agent orchestration with optional backend and model defaults.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Human-readable project name |
| `path` | string | Yes | Absolute filesystem path to the project |
| `dependencies` | string[] | No | List of project IDs this project depends on |
| `default_backend` | string | No | Default backend name for this project |
| `default_model` | string | No | Default model for this project |
| `labels` | object | No | Key-value labels for project categorization |

**Example request:**
```json
{
  "name": "go-quicbaas",
  "path": "/home/user/sources/go-quicbaas",
  "default_backend": "claude",
  "default_model": "claude-sonnet-4-20250514",
  "labels": {"language": "go", "tier": "core"}
}
```

**Example response:**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_list

List all registered fleet node projects with optional pagination.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `page_size` | integer | No | Maximum number of projects to return per page |
| `page_token` | string | No | Pagination token from previous response |

**Example request:**
```json
{
  "page_size": 10
}
```

**Example response:**
```json
{
  "projects": [
    {
      "id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
      "name": "go-quicbaas",
      "path": "/home/user/sources/go-quicbaas"
    }
  ],
  "next_page_token": "",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_spawn

Spawn a persistent, multi-turn ACP agent session on a backend with project
context. Mirrors `POST /api/v1/orchestrator/sessions`. Permission:
`orchestrator:spawn`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `backend` | string | Yes | Backend name to spawn the session on (e.g., `"claude"`, `"codex"`) |
| `project_id` | string | Yes | Registered project ID to associate with the session. This is the UUID minted when the project was created, not its name |
| `model` | string | No | Model to use (falls back to project/backend default) |
| `system_prompt` | string | No | System prompt to configure the agent |
| `work_dir` | string | No | Working directory for the agent process (defaults to the project path) |
| `sandbox_mode` | string | No | `default` \| `required` \| `none`; empty defers to the server SandboxPolicy |
| `host_affinity` | string | No | Pin the session to a daemon by Name or Host |
| `host_affinity_strict` | boolean | No | Fail instead of falling back when the pinned host is offline |
| `gpu_required` | boolean | No | Restrict dispatch to daemons advertising GPU capability |
| `persistent` | boolean | No | Multi-turn session keeping context across prompts (**default `true`**) |
| `account_id` | string | No | Pin the session to a pooled provider account. A bare pin is deliberate: the session is never rotated off it. |
| `account_auto` | boolean | No | Select a pooled account with the named strategy, and authorize rotation across the whole pool |
| `account_strategy` | string | No | `static` \| `least_usage`; empty selects the registry default. Ignored unless `account_auto` is true. |
| `account_allow` | string[] | No | Restrict the session to these pooled account IDs and authorize it to rotate between them |
| `prompt_append` | string | No | Prompt material appended after the session's resolved prompt (`system_prompt`, else the Agent's, else its role's built-in charter). At most 64 KiB; more is `400`. See [agents.md](agents.md#run-time-prompt-material) |
| `mcp_servers` | object[] | No | MCP servers to attach to the session |

Each MCP server object:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Server name |
| `command` | string | Executable command |
| `args` | string[] | Command arguments |
| `url` | string | Remote server URL (alternative to `command`) |
| `env` | object | Environment variables for the server process |

**Example request:**
```json
{
  "tenant_id": "acme",
  "backend": "claude",
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "model": "claude-sonnet-4-20250514",
  "system_prompt": "You are a Go backend engineer.",
  "mcp_servers": [
    {"name": "quicbaas", "command": "quicbaas", "args": ["mcp", "serve"]}
  ]
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "backend": "acp",
  "model": "claude-sonnet-4-20250514",
  "tenant_id": "acme",
  "persistent": true,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

> `persistent` defaults to `true` because a one-shot spawn registers no durable
> session and a follow-up prompt would fail with "session not found". Use
> `quicbaas_orchestrator_inference` for stateless calls.

---

### quicbaas_orchestrator_prompt

Send a prompt to an agent session or backend. Provide `session_id` for existing sessions, or `backend` for stateless prompts.

Permission: `orchestrator:prompt`.

**A session prompt is asynchronous.** MCP has no server-push channel, so the
tool returns only an acknowledgement — it never carries the agent's output.
Read the turn back with `quicbaas_orchestrator_session_history`, paging from the
returned `sequence` until an event of kind `turn_completed` or `turn_failed`
carrying the same `turn_id` appears. See
Asynchronous Agent Turns.

A prompt with no `session_id` is a stateless one-shot: it has no event log to
publish to, so the completed turn is returned inline under `content`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `prompt` | string | Yes | The prompt text to send to the agent |
| `backend` | string | No | Backend name (optional if `session_id` provided) |
| `session_id` | string | No | Existing session ID to route the prompt to |
| `project_id` | string | No | Project ID for context resolution. This is the UUID minted when the project was created, not its name |
| `model` | string | No | Model to use (falls back to project/backend default) |
| `system_prompt` | string | No | System prompt to configure the agent |
| `use_rag` | boolean | No | Whether to use RAG for context enrichment (default: false) |

**Example request (session):**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789",
  "prompt": "Add error handling to the HTTP handler."
}
```

**Example request (stateless):**
```json
{
  "tenant_id": "acme",
  "backend": "ollama",
  "prompt": "Explain the builder pattern in Go.",
  "model": "llama3"
}
```

**Example response (session prompt — acknowledgement):**
```json
{
  "session_id": "sess_xyz789",
  "turn_id": "9f3c1e40-8a21-4c6f-9f0e-1d2b3c4d5e6f",
  "status": "accepted",
  "sequence": 42,
  "backend": "claude",
  "model": "claude-sonnet-4-20250514",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

**Example response (stateless prompt — completed turn):**
```json
{
  "session_id": "",
  "content": [
    {"type": "text", "text": "The builder pattern in Go..."}
  ],
  "model": "llama3",
  "stop_reason": "end_turn",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_session_history

Read a page of a session's durable event transcript: prompts, assistant output,
tool calls, and turn lifecycle events. Permission: `orchestrator:status`.

This is the MCP replacement for a streaming attach. Page from the `sequence`
returned by `quicbaas_orchestrator_prompt` until the turn's terminal event
appears.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session whose transcript to read |
| `from_sequence` | integer | No | Inclusive lower bound on the event sequence (default: start of session) |
| `limit` | integer | No | Maximum events to return (default: 256) |

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "tenant_id": "acme",
  "events": [
    {"sequence": 42, "kind": "turn_started", "turn_started": {"turn_id": "9f3c1e40-..."}},
    {"sequence": 43, "kind": "agent_thought", "thought": {"text": "The handler needs a nil check before..."}},
    {"sequence": 44, "kind": "assistant_chunk", "assistant_chunk": {"text": "I've updated the handler..."}},
    {"sequence": 45, "kind": "turn_completed", "turn_completed": {"turn_id": "9f3c1e40-...", "stop_reason": "end_turn"}}
  ],
  "next_seq": 0
}
```

---

### quicbaas_orchestrator_session_conversation

Read a page of a session's CONVERSATION: who said what, which tools ran, and
what each answer cost. Permission: `orchestrator:status`.

Coarser than `quicbaas_orchestrator_session_history`, which pages the raw event
log. A streaming answer is many events and exactly one turn here, and a page
boundary always falls between turns. Use this to READ a session; use the event
transcript to FOLLOW one asynchronous turn to its terminal event.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session whose conversation to read |
| `cursor` | integer | No | `next_cursor` from a previous page (default: start of the retained conversation) |
| `limit` | integer | No | Maximum turns to return (default: 100, capped at 500) |

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "tenant_id": "acme",
  "turns": [
    {"sequence": 41, "role": "user", "text": "fix the nil check in the handler"},
    {"sequence": 43, "role": "assistant", "text": "I've updated the handler...",
     "model": "claude-sonnet-4-5", "stop_reason": "end_turn",
     "tools": [{"invocation_id": "i1", "name": "Edit", "result_bytes": 214}],
     "usage": {"input_tokens": 1840, "output_tokens": 312}}
  ],
  "next_cursor": 0,
  "oldest_sequence": 1
}
```

`oldest_sequence` above 1 means retention has already dropped earlier turns, so
a client showing "the beginning of the conversation" is showing the beginning
of what remains.

---

### quicbaas_orchestrator_cost

Read one scope of the consumption ledger: the tokens spent under it, the
drill-down to the level below, and the spend policy of every pooled account
that paid.

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `tenant_id` | string | yes | Tenant whose spend to read |
| `project_id` | string | no | Narrow to one project |
| `task_id` | string | no | Narrow to one task (requires `project_id`) |
| `job_id` | string | no | Narrow to one job (requires `task_id`) |
| `session_id` | string | no | Narrow to one session (requires `job_id`) |
| `round` | integer | no | Narrow to one round (requires `session_id`) |

The chain is a PATH: a level supplied without the levels above it is refused.
Supply `-` at a level to open the subtree of entries that have no value there
(a job that belongs to no task).

`equivalent_micros` and `billed_micros` are accompanied by `priced_entries` and
`billed_entries`; when those are zero the figure is UNMEASURED, not free: report
it as unavailable rather than as zero.

### quicbaas_orchestrator_cost_settings_get / _set

Read or replace a tenant's runtime cost settings: the electricity rate and the
per-engine device draws its local inference is costed from. See
[cost-enforcement.md](cost-enforcement.md#local-inference-is-costed-from-electricity).

| Name | Type | Required | Description |
|---|---|---|---|
| `tenant_id` | string | yes | Tenant whose settings to read or replace |
| `electricity_price_per_kwh` | number | no (set) | US dollars per kWh; absent inherits the configuration |
| `engine_power_watts` | object | no (set) | Watts per engine name; 0 switches electricity costing off for that engine |

A malformed value is refused (`CostSettingsArgumentError`) rather than read as
absent.

---

### quicbaas_orchestrator_status

Get the status of an active agent session. Permission: `orchestrator:status`.

MCP is a request/response protocol with no server-initiated streaming channel,
so there is no `attach` tool. This tool reports the *session's* lifecycle state
(active, closed); it cannot tell you whether a particular turn has finished —
use `quicbaas_orchestrator_session_history` for that. Live event streaming stays
on the REST SSE and gRPC server-streaming endpoints.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session ID to query |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789"
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "state": "idle",
  "last_activity": 1710590400,
  "uptime": 3600,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_close

Close and terminate an active agent session. Permission: `orchestrator:close`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session ID to close |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789"
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_rotate_account

Move an agent session onto a different pooled provider account. Mirrors
`POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account`. Permission:
`orchestrator:spawn` — the rotation closes the agent process and reconstitutes
it against a newly selected pooled credential, which is a spawn's authority and
not a close's.

Only a session spawned with `account_auto` or `account_allow` may rotate; a
bare `account_id` pin is honored and answered with a not-rotatable error.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant the session belongs to |
| `session_id` | string | Yes | Session to move |
| `reason` | string | No | Recorded on the closed account segment and in the session transcript |
| `hold_outgoing` | boolean | No | Hold the outgoing account out of the pool until `available_at` |
| `available_at` | string | No | RFC 3339 time the held account rejoins the pool; ignored unless `hold_outgoing` is true, empty applies the server default backoff |

**Example request:**
```json
{
  "tenant_id": "acme",
  "session_id": "sess_xyz789",
  "reason": "seat retired",
  "hold_outgoing": true
}
```

**Example response:**
```json
{
  "session_id": "sess_xyz789",
  "previous_account_id": "acct_a",
  "account_id": "acct_b",
  "strategy": "least_usage",
  "mode": "resume"
}
```

`mode` is `in_place` when the credential was swapped with no process restart,
or `resume` when the agent process was reconstituted and the transcript
replayed.

---

### quicbaas_orchestrator_list_sessions

List one page of a tenant's agent sessions, optionally filtered by backend.
Permission: `orchestrator:list`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | Yes | Tenant whose sessions are listed |
| `backend` | string | No | Filter sessions by backend name |
| `status` | string | No | Filter sessions by lifecycle state |
| `limit` / `offset` / `sort` / `q` | — | No | [List paging, sorting and search](#list-paging-sorting-and-search) |

The result carries `total`, `limit`, `offset` and — except on the last page —
`next_offset` beside `sessions`.

**Example request:**
```json
{
  "tenant_id": "acme",
  "backend": "claude"
}
```

**Example response:**
```json
{
  "sessions": [
    {
      "id": "sess_xyz789",
      "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
      "backend": "acp",
      "model": "claude-sonnet-4-20250514",
      "status": "idle"
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_list_backends

List one page of registered fleet node backends with their capabilities. Accepts
`limit`, `offset`, `sort` and `q` per
[List paging, sorting and search](#list-paging-sorting-and-search).

**Example request:**
```json
{}
```

**Example response:**
```json
{
  "backends": [
    {
      "name": "claude",
      "type": "acp",
      "capabilities": {
        "persistent_sessions": true,
        "tool_use": true,
        "file_access": true,
        "streaming": false,
        "acp": true
      }
    },
    {
      "name": "ollama",
      "type": "ollama",
      "capabilities": {
        "persistent_sessions": false,
        "tool_use": false,
        "file_access": false,
        "streaming": false,
        "acp": false
      }
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_decompose

Use an AI agent to decompose a complex task into discrete work items with dependencies and priorities.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `backend` | string | Yes | Backend to use for task decomposition |
| `task` | string | Yes | Task description to decompose |
| `model` | string | No | Model to use |
| `project_ids` | string[] | No | Target project IDs for context |
| `use_rag` | boolean | No | Whether to use RAG for context enrichment (default: false) |

**Example request:**
```json
{
  "backend": "claude",
  "task": "Implement RBAC with role hierarchy and permission inheritance",
  "project_ids": ["3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62"],
  "use_rag": true
}
```

**Example response:**
```json
{
  "work_items": [
    {
      "title": "Define role and permission data models",
      "description": "Create Role, Permission, and RoleHierarchy types...",
      "priority": 1,
      "dependencies": []
    },
    {
      "title": "Implement role hierarchy resolution",
      "description": "Build a DAG-based resolver for inherited permissions...",
      "priority": 2,
      "dependencies": ["Define role and permission data models"]
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_broadcast

Broadcast a message to a messaging channel for agent coordination.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Messaging channel to broadcast to |
| `message` | string | Yes | Message content to broadcast |
| `metadata` | object | No | Additional metadata to include with the message |

**Example request:**
```json
{
  "channel": "agent-updates",
  "message": "Tests passing on go-quicbaas, ready for integration.",
  "metadata": {"project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62", "status": "complete"}
}
```

**Example response:**
```json
{
  "published": true,
  "channel": "agent-updates",
  "message_id": "msg_def456",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_health

Check health of all active agent sessions. Returns per-session status and summary counts. Takes no parameters.

**Example request:**
```json
{}
```

**Example response:**
```json
{
  "total_sessions": 3,
  "healthy": 2,
  "unhealthy": 0,
  "closed": 1,
  "results": [
    {
      "session_id": "sess_xyz789",
      "backend": "acp",
      "state": "idle",
      "healthy": true
    },
    {
      "session_id": "sess_abc456",
      "backend": "acp",
      "state": "active",
      "healthy": true
    },
    {
      "session_id": "sess_def012",
      "backend": "ollama",
      "state": "closed",
      "healthy": false
    }
  ],
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

Sessions in `active` or `idle` state are considered healthy. Sessions in `error` or `closed` state are flagged as unhealthy.

---

## RAG Index Tools

Build tag: `mcp && rag`

### quicbaas_rag_index_file

Index a single file: read, chunk by language, generate embeddings, and upsert into the specified vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | Yes | Absolute path to the file to index |
| `namespace` | string | Yes | Vector namespace to store embeddings in |
| `model` | string | No | Embedding model to use (uses default if not specified) |

Supported languages for chunking: Go, Python, JavaScript, TypeScript, Rust, Java, C, C++, Ruby, Shell, Markdown, YAML, JSON, TOML, SQL. Unrecognized extensions fall back to `"text"`.

**Example request:**
```json
{
  "file_path": "/home/user/sources/go-quicbaas/pkg/orchestration/backend.go",
  "namespace": "go-quicbaas"
}
```

**Example response:**
```json
{
  "file_path": "/home/user/sources/go-quicbaas/pkg/orchestration/backend.go",
  "chunk_count": 8,
  "namespace": "go-quicbaas",
  "language": "go",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_rag_index_project

Index all matching files in a project directory: scan files, chunk by language, batch embed, and upsert into vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_path` | string | Yes | Absolute path to the project directory to index |
| `namespace` | string | Yes | Vector namespace to store embeddings in |
| `file_pattern` | string | No | Glob pattern for matching files (default: `*.go`) |
| `model` | string | No | Embedding model to use (uses default if not specified) |

**Example request:**
```json
{
  "project_path": "/home/user/sources/go-quicbaas",
  "namespace": "go-quicbaas",
  "file_pattern": "*.go"
}
```

**Example response:**
```json
{
  "project_path": "/home/user/sources/go-quicbaas",
  "file_count": 47,
  "total_chunk_count": 312,
  "namespace": "go-quicbaas",
  "file_pattern": "*.go",
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_rag_index_status

Show indexing status for a vector namespace: vector count, dimension, last indexed timestamp.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `namespace` | string | Yes | Vector namespace to query status for |

**Example request:**
```json
{
  "namespace": "go-quicbaas"
}
```

**Example response:**
```json
{
  "namespace": "go-quicbaas",
  "vector_count": 312,
  "dimension": 1536,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

## Agent Cache Tools

Build tag: `mcp`

These tools store and retrieve expensive analysis results in the KV store for sharing between agent sessions. Keys are scoped by project ID using a composite key (`project_id:key`) hashed via FNV-1a.

### quicbaas_orchestrator_cache_put

Store an analysis result in the agent cache with optional TTL.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | Yes | Project identifier for scoping cached data |
| `key` | string | Yes | Cache key for the analysis result |
| `value` | string | Yes | Analysis result value to cache |
| `ttl_seconds` | integer | No | Time-to-live in seconds (0 means no expiry) |

**Example request:**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "key": "dependency-analysis",
  "value": "{\"packages\": 47, \"external_deps\": 12, \"circular\": false}",
  "ttl_seconds": 3600
}
```

**Example response:**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "key": "dependency-analysis",
  "stored": true,
  "version": 1,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

---

### quicbaas_orchestrator_cache_get

Retrieve a cached analysis result by project and key.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | Yes | Project identifier for scoping cached data |
| `key` | string | Yes | Cache key to retrieve |

**Example request:**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "key": "dependency-analysis"
}
```

**Example response (found):**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "key": "dependency-analysis",
  "value": "{\"packages\": 47, \"external_deps\": 12, \"circular\": false}",
  "found": true,
  "version": 1,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```

**Example response (not found):**
```json
{
  "project_id": "3f6d2a1c-9b4e-4a7d-8c21-5e0f7b9d4a62",
  "key": "dependency-analysis",
  "value": "",
  "found": false,
  "timestamp": "2026-03-16T12:00:00.000000000Z"
}
```


---

## MCP Tools Reference

MCP tool naming conventions and the complete registered inventory of
orchestration tools with their authorization mappings. Request and response
schemas live in [orchestration/mcp-tools.md](api.md).

Registration sites: `pkg/api/mcp/orchestrator_tools.go` (27),
`orchestrator_cache_tools.go` (2), `account_tools.go` (9),
`project_tools.go` (5), `fleet_admin_tools.go` (4) — 47 orchestration
tools in total, all behind the `mcp` build tag.

### Naming Convention

The orchestration surface uses six tool prefixes:

| Prefix | Count | Scope | Description |
|--------|-------|-------|-------------|
| `quicbaas_orchestrator_*` | 22 | Session / project / job management | Agent lifecycle, project discovery, ACP session management, jobs, agent cache |
| `quicbaas_agent_account_*` | 9 | Pooled provider accounts | Credential envelopes, rotation, usage — see [accounts.md](accounts.md) |
| `quicbaas_fleet_*` | 5 | Fleet registry | Register / list / heartbeat the QuicBaaS fleet node daemons themselves |
| `project_*` | 7 | Project CRUD and agent grants | `project_create`/`get`/`list`/`update`/`delete`, `project_grant_fleet_node`, `project_revoke_fleet_node` |
| `quicbaas_task_*` | 2 | Task artifacts | `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts` |
| `agent_*` | 2 | Global agent admin | `quicbaas_fleet_disable`, `quicbaas_fleet_enable` |

The `quicbaas_orchestrator_*` prefix refers to operations on the orchestrator
surface (projects, ACP sessions, jobs, cache). The `quicbaas_fleet_*`
prefix refers to the QuicBaaS agent infrastructure (the binary running on
remote systems).

Every one of these tools resolves to a qrdb-canonical permission in
`pkg/api/mcp/authz_qrdb.go`, but not all to the same family:
`quicbaas_orchestrator_*`, `quicbaas_fleet_*`, `quicbaas_task_*` and
`agent_*` map to `orchestrator:<action>`; `project_*` maps to
`project:<action>`; `quicbaas_agent_account_*` maps to
`credentials:<action>`, because a pooled account is a stored credential
envelope. A tool with no mapping is default-denied and never appears in
`tools/list`.

### quicbaas_orchestrator_* Tools

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
| `quicbaas_orchestrator_list_sessions` | `orchestrator:list` | One page of a tenant's sessions, narrowed by `backend`, `status`, `project_id`, `job_id`, `runner` or `agent_id` |
| `quicbaas_orchestrator_list_backends` | `orchestrator:backend_list` | One page of registered backends and capabilities, paged and counted per [List paging, sorting and search](#list-paging-sorting-and-search) |
| `quicbaas_orchestrator_decompose` | `orchestrator:task_decompose` | Decompose a task into prioritized work items |
| `quicbaas_orchestrator_health` | `orchestrator:health` | Check health of all active sessions |
| `quicbaas_orchestrator_broadcast` | `orchestrator:broadcast` | Publish a message to a coordination channel |
| `quicbaas_orchestrator_inference` | `orchestrator:inference` | Stateless inference against a fleet-resident daemon |
| `quicbaas_orchestrator_start_job` | `orchestrator:job_start` | Start an autonomous, iterating orchestration job; `prompt_append` is appended to every session the job spawns |
| `quicbaas_orchestrator_get_job_status` | `orchestrator:job_read` | Query a job's iteration count and task progress |
| `quicbaas_orchestrator_cancel_job` | `orchestrator:job_cancel` | Cancel a running orchestration job |
| `quicbaas_orchestrator_list_jobs` | `orchestrator:job_list` | One page of the tenant's jobs, newest first; narrowed by `task_id`, `runner`, `project_id`, `agent_id`, `statuses` or `job_ids`, paged and counted per [List paging, sorting and search](#list-paging-sorting-and-search) |
| `quicbaas_orchestrator_task_create` | `orchestrator:task_create` | Create a task, optionally dispatching it at once; carries `project_id`, `source`, `metadata`, `dependencies`, `priority`, `attempt_budget`, `park` and `prompt_append` exactly as the REST body does |
| `quicbaas_orchestrator_cache_put` | `orchestrator:cache_write` | Store analysis result in KV (project-scoped) |
| `quicbaas_orchestrator_cache_get` | `orchestrator:cache_read` | Retrieve cached result by project and key |

### quicbaas_fleet_* Tools

| Tool | Permission | Description |
|------|------------|-------------|
| `quicbaas_fleet_register` | `orchestrator:agent_register` | Register an agent with the fleet |
| `quicbaas_fleet_list` | `orchestrator:agent_list` | One page of a tenant's fleet nodes, narrowed by `status`, `backend` (membership), `host` or `node_type`, paged per [List paging, sorting and search](#list-paging-sorting-and-search) |
| `quicbaas_fleet_status` | `orchestrator:agent_read` | Get one fleet node's status |
| `quicbaas_fleet_remove` | `orchestrator:agent_remove` | Remove an agent from the fleet registry |
| `quicbaas_fleet_heartbeat` | `orchestrator:agent_heartbeat` | Update an agent's last-seen timestamp |

Full request and response schemas for every tool above are in
[orchestration/mcp-tools.md](api.md); the pooled-account
family is documented in [accounts.md](accounts.md).

### Planned: `quicbaas_orch_*` coordination tools

**Not implemented.** No `quicbaas_orch_*` tool is registered anywhere in
`pkg/api/mcp` — the only `quicbaas_orch_*` identifiers that exist in the
codebase are Prometheus metric names in `pkg/metrics/metrics.go` (listed
below). The tools in this section are a design sketch for a namespace that
orchestrated AI agents (Claude Code, Codex, OpenCode) would use to coordinate
with each other. Treat them as planned, not callable.

#### Peer messaging (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox | `to`, `action`, `content`, `channel` (optional), `metadata` (optional) |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages | `since` (optional, unix nanos) |

#### File locking (planned)

| Tool | Description | Parameters |
|------|-------------|------------|
| `quicbaas_orch_file_lock` | Acquire a lock on a file path | `file_path`, `project_id` |
| `quicbaas_orch_file_unlock` | Release a previously acquired lock | `lock_key` |
| `quicbaas_orch_file_lock_status` | Check lock status for a file or list active locks | `file_path` (optional), `project_id` |

### Observability metrics

These are Prometheus metrics, not MCP tools. They are registered in
`pkg/metrics/metrics.go` and are live today.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_orch_peer_messages_total` | Counter | Total peer messages sent |
| `quicbaas_orch_peer_inbox_size` | Gauge | Current inbox size per agent |
| `quicbaas_orch_file_locks_active` | Gauge | Number of active file locks |
| `quicbaas_orch_file_lock_wait_duration_seconds` | Histogram | Time spent waiting for lock release |
| `quicbaas_factory_tokens_total` | Counter | Tokens of every recorded turn, by `tenant`, `project`, `kind`, `provider`, `backend`, `model`, `direction` (bounded labels; see [cost-enforcement.md](cost-enforcement.md#metrics)) |
| `quicbaas_factory_cost_micros_total` | Counter | Cost of priced turns in micro-dollars, same labels with `basis` = `equivalent` or `billed` |
| `quicbaas_factory_spend_deferrals_total` | Counter | Admission deferrals for a spent project or tenant, by `tenant`, `project`, `axis` |
| `quicbaas_orch_job_budget_utilization` | Gauge | Budget utilization percentage per job |
| `quicbaas_orch_agent_restarts_total` | Counter | Total agent restarts |
