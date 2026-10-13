# Orchestration — API

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every capability of the orchestration engine on every transport, with
> permissions: sessions, jobs, tasks, workflows, inference, cost, the
> list-paging contract and the agent MCP tools. Feature folder:
> `docs/orchestration/`. Conflicts with this document are defects.

Projects, pooled accounts, the agent catalog and the SDLC module are in
[../software-factory/api.md](../software-factory/api.md); the fleet registry
and node enrollment are in [../agent-fleet/api.md](../agent-fleet/api.md). The
list-paging contract below is the one contract all of them share.


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
| `GET /api/v1/orchestrator/tasks` | `created_at:desc` | `agent_id`, `attempts`, `created_at`, `id`, `project_id`, `status`, `subject`, `updated_at` | id, subject, status, project id | `statuses` (comma-separated), `project_id`, `agent_id`, `job_id`, `auto_spawn` (`true`/`false`), `source_kind` + `source_ref` (together — see below), `team_name`, `assigned_to`, `created_by`, `status` | `status`, `project_id`, `agent_id` |
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

**`source_kind` + `source_ref` on the tasks listing ask which task holds one
source reservation** — "which task adopted `owner/repo#12`". The answer is
read from the source reservation index, not a scan, so it is one key lookup
whatever the tenant's size. A source is reserved per project, so `project_id`
is the scope the source is looked up in (absent means the unscoped
reservation), not a facet. The two are set together: one without the other,
or a kind no source registered, is a 400 (`InvalidArgument` over gRPC). A
source nobody holds is an empty page, not an error. The other filters still
apply to the holder. Every transport carries the pair — REST and QUIC as query
parameters, gRPC and Unix as `TaskFilter.source_kind` / `source_ref`, the
embedded transport and the Go SDK as `ListTasksRequest.SourceKind` /
`SourceRef`, the TS SDK as `TaskPageParams.source_kind` / `source_ref`. MCP
has no task-listing tool.

**`backend` on the fleet listing is MEMBERSHIP, not equality.** A daemon
started with `--backends acp` (the group) serves claude, codex and opencode from one
record; `?backend=codex` matches it, and the `backend` facet counts it under
each backend it serves.

**Every fleet node record carries `backends`**: each name the node is
matched on, primary backend first, then each capability, each name once.
It is `[]` for a node that serves nothing, never `null`. One renderer,
`orchestration.FleetNode.ServedBackends`, produces it for every transport:
REST and QUIC (`GET /api/v1/fleet`, `GET /api/v1/fleet/{nodeID}`), gRPC and
Unix (`FleetNode.backends`), MCP (`quicbaas_fleet_list`,
`quicbaas_fleet_status`), the embedded transport, the Go SDK
(`orchestrator.FleetNode.Backends`), and the TS SDK (`FleetNode.backends`).

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
`next_offset`) rather than as a first page of an unknown number. MCP pages
`listProjectsPage` and `listAccountsPage` for real (see the table below);
gRPC-Web, QUIC, Unix and Wails refuse those families outright. MCP's
orchestrator `*Page` verbs delegate to their unpaged sibling and report its
answer as a single page; `listTasksPage` refuses, because the server has no
task-list tool. For every other family gRPC-Web, QUIC, Unix and MCP have no
TypeScript transport wired yet: their `*Page` verbs delegate to their own
unpaged sibling, so a caller gets that transport's typed refusal rather than an
invented page. None of them throws "paging not supported".

### TypeScript transports: projects, sdlc, accounts

What each `sdk/typescript` transport does with `client.projects`, `client.sdlc`
and `client.accounts`. A family a transport cannot carry is still an object of
its declared type: every verb rejects with `TransportNotSupportedError`
(`transport`, `family`, `method`, `reason`), never a `null` field.

| Transport | projects | sdlc | accounts | How / why not |
|---|---|---|---|---|
| REST | yes | yes | yes | `/api/v1/projects`, `/api/v1/sdlc`, `/api/v1/agent-accounts`. |
| QUIC | no | no | no | The server's QUIC listener speaks qrdb's frame protocol, not REST over HTTP/3, and Node's `fetch` does not negotiate HTTP/3. Whether and how a QUIC client listener is offered to SDK callers is an open owner decision. |
| MCP | yes, with refusals | yes | yes, with one refusal | `project_*`, `sdlc_*` and `quicbaas_agent_account_*` tools on one Streamable HTTP session (`mcp_rpc.ts`). `credential.expires_at` (RFC 3339) reaches create and rotation. Refused because the tool does not read the argument: `createProject` with non-empty `metadata`, `updateProject` with any `metadata` (`{}` would mean "clear"), `updateProject` with `clear_description`, `listProjectsPage` with `autonomous`, `listAccountsPage` with `status`. |
| gRPC-Web | no | no | no | The server serves native gRPC only (no gRPC-Web endpoint), and the SDK has no gRPC-Web client library. Needs an architecture decision. |
| Unix | no | no | no | The Unix transport is gRPC over `AF_UNIX`; the TypeScript SDK has no Node gRPC client or generated stubs. Needs an architecture decision. |
| Wails | no | no | no | The desktop app exposes no Wails binding for these families. Use the REST transport against the embedded server. |

On Unix the same refusal covers every other family too: each
`client.orchestrator` verb (jobs, tasks, sessions, fleet, workflows,
attention, decompose, backends, runners, worker image, cost settings, spend
policy, spend report, inference, health), each `client.auth` verb, and
`connection.connect`/`disconnect` reject with `TransportNotSupportedError`
(family `orchestrator`, `auth` or `connection`) for the reason in the row
above. The server does serve these services on its Unix socket; the gap is
client-side. `deployFleetNode` keeps its `DeployNotSupportedError`.

Prompts have no surface on any wire (server, Go SDK or TypeScript interface),
so no transport carries them.

### TypeScript QUIC transport: the other families

The QUIC client sends nothing. Every verb of its other families rejects with
`QuicNotImplementedError`, a `TransportNotSupportedError` subclass carrying
`transport: 'quic'`, the family, the method and one of two reasons:

| Family | Reason | Gap |
|---|---|---|
| `orchestrator` (jobs, tasks, sessions, fleet, workflows, attention, decompose, backends, runners, models, health, inference, worker image) and cost (cost settings, spend policy, spend report) | The server's QUIC listener serves no orchestrator method. | Server: quicbaas's API module does not implement go-qrdb's `RegisterQUIC` hook (go-quicbaas#819), so no QUIC route exists. |
| `auth`, `barrier`, `ceremony`, `bootstrap`, `connection` (`connect`, `disconnect`) | The listener speaks qrdb's frame protocol, which the SDK does not implement. | SDK: go-qrdb serves `barrier.*` and `ceremony.*` on that protocol; a TypeScript client for it waits on the same owner decision as projects, sdlc and accounts. |

`deployFleetNode` keeps its own `DeployNotSupportedError` on every non-REST
transport. A `*Page` listing refuses with its unpaged sibling's method name.

### TypeScript MCP transport: orchestrator

`McpClient.orchestrator` calls the server's `quicbaas_orchestrator_*`,
`quicbaas_fleet_*` and `quicbaas_sdlc_worker_image*` tools on the same session
as the families above. A verb the server has no tool for, or an argument the
tool does not read, rejects with `TransportNotSupportedError`; a tool failure
is `McpError`.

| Area | Wired verbs | Refused (server has no MCP tool) | Refused arguments |
|---|---|---|---|
| Jobs | `listJobs`, `getJobStatus`, `startJob`, `cancelJob` | `subscribeJobEvents` (no streaming tool) | `startJob`: `workflow_name`, `workflow_file`, `poll_interval`. `tenant_id` is not sent: the tool runs under the caller's tenant. |
| Fleet | `listFleetNodes`, `getFleetNode`, `registerFleetNode`, `removeFleetNode`, `disableFleetNode`, `enableFleetNode` | `listBackendModels`; `deployFleetNode` is `DeployNotSupportedError` | `listFleetNodes`: `model`. `registerFleetNode`: `model`, `metadata`. |
| Sessions | `listSessions`, `spawnSession`, `promptSession`, `closeSession`, `rotateSessionAccount`, `getSessionHistory`, `getSessionConversation` | `subscribeSession` (no streaming tool) | `spawnSession`: `env`, `sandboxed`. The session tools require `tenant_id`, so pass it on `promptSession`, `closeSession` and `rotateSessionAccount`. `promptSession` answers the asynchronous turn acknowledgement, as REST's 202 does. |
| Spend and cost | `getSpendReport`, `getTenantSpendPolicy`, `setTenantSpendPolicy`, `getCostSettings`, `setCostSettings` | — | — |
| Backends, runners, health | `listBackends`, `listRunners` (every page), `healthCheck` | — | — |
| Inference | `inference` | `inferenceStream` (no streaming tool) | `host_affinity_strict` |
| Workflows | `listWorkflows` | `createWorkflow`, `getWorkflow`, `updateWorkflow`, `deleteWorkflow` | — |
| Tasks and attention | `createTask`, `readmitTask`, `deleteTask`, `listAttention`, `resolveAttention` | `listTasks`, `getTask`, `claimTask`, `completeTask`, `decomposeTask` (the decompose tool splits free text, not a stored task) | `createTask`: `host_affinity_strict`. |
| Worker image | `getWorkerImageStatus`, `refreshWorkerImage` | — | — |

`createTask` builds its `Task` from the request and the tool's reply. The tool
returns no server-stamped times or `version`, so those fields are `0`. A task
the tool saved but could not dispatch is reported as `McpError`, matching REST.
`startJob` and `healthCheck` convert the tool's RFC 3339 instant to Unix
nanoseconds; an instant that does not parse is `0`.

A 200 whose body is not a listing — no records under the endpoint's key — is a
typed `MalformedPageError` rather than an empty page, because a table rendering
"0 of 0" for a failed read tells the operator something untrue.


## Transports

QUIC is not a separate surface: `sdk/go/transport/quic` embeds
`*rest.Transport` and swaps only the HTTP client for an HTTP/3 one, so every
QUIC call is its REST call (the caveat in
[../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md)
applies: no component in this repository serves HTTP/3 today). The Unix
transport is gRPC over `AF_UNIX`. MCP routes an SDK method to a tool by the
map in `sdk/go/transport/mcp/orchestrator_handler.go`, and the embedded
transport to the `service/method` keys in
`sdk/go/transport/embedded/transport.go`.

### `quicbaas.Orchestrator` — SDK package `sdk/go/client/orchestrator`

`ListJobs`, `ListTasks`, `ListSessions`, `ListFleetNodes`, `ListBackends`,
`ListRunners`, `ListWorkflows` and `ListTaskArtifacts` are paged, sorted and
searched per [List paging, sorting and search](#list-paging-sorting-and-search).

**REST routes.** The table is the daemon's route table
(`pkg/api/rest/routes_permissions_generated.go`); QUIC carries it over HTTP/3
and the Unix transport reaches the same handlers over gRPC on `AF_UNIX`.
Every route is tenant-scoped by the caller's credential.

| Route | Permission | What it does |
|---|---|---|
| `POST /api/v1/orchestrator/discover` | `orchestrator:discover` | Scan a directory tree for agent-enabled projects |
| `POST /api/v1/orchestrator/inference` | `orchestrator:inference` | Stateless inference against a fleet-resident daemon |
| `GET /api/v1/orchestrator/backends` | `orchestrator:backend_list` | Registered backends and their capabilities |
| `GET /api/v1/orchestrator/health` | `orchestrator:health` | Orchestrator and session health |
| `GET /api/v1/orchestrator/runners` | `orchestrator:status` | Registered job runners and their states |
| `POST /api/v1/orchestrator/sessions` | `orchestrator:spawn` | Spawn a session |
| `GET /api/v1/orchestrator/sessions` | `orchestrator:list` | List sessions |
| `GET /api/v1/orchestrator/sessions/{sessionID}` | `orchestrator:status` | One session's status |
| `DELETE /api/v1/orchestrator/sessions/{sessionID}` | `orchestrator:close` | Close a session |
| `POST /api/v1/orchestrator/sessions/{sessionID}/prompt` | `orchestrator:prompt` | Accept an asynchronous turn |
| `POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account` | `orchestrator:spawn` | Move the session onto another pooled account |
| `GET /api/v1/orchestrator/sessions/{sessionID}/events` | `orchestrator:status` | Server-sent event stream of the session (attach) |
| `GET /api/v1/orchestrator/sessions/{sessionID}/history` | `orchestrator:status` | The session's conversation, read from its event log |
| `POST /api/v1/orchestrator/jobs` | `orchestrator:job_start` | Start a job |
| `GET /api/v1/orchestrator/jobs` | `orchestrator:status` | List jobs |
| `GET /api/v1/orchestrator/jobs/{jobID}` | `orchestrator:status` | One job's status |
| `GET /api/v1/orchestrator/jobs/{jobID}/events` | `orchestrator:status` | Job event stream |
| `POST /api/v1/orchestrator/jobs/{jobID}/children` | `orchestrator:job_start` | Spawn a child job |
| `DELETE /api/v1/orchestrator/jobs/{jobID}` | `orchestrator:close` | Cancel a job |
| `POST /api/v1/orchestrator/tasks` | `orchestrator:task_create` | Create a task |
| `GET /api/v1/orchestrator/tasks`, `GET .../tasks/{taskID}` | `orchestrator:status` | List / read tasks |
| `DELETE /api/v1/orchestrator/tasks/{taskID}` | `orchestrator:task_create` | Delete a task |
| `POST /api/v1/orchestrator/tasks/decompose` | `orchestrator:task_decompose` | Decompose a task into work items |
| `POST .../tasks/{taskID}/claim` | `orchestrator:task_claim` | Claim a task |
| `POST .../tasks/{taskID}/block` | `orchestrator:task_block` | Block a task |
| `POST .../tasks/{taskID}/complete` | `orchestrator:task_complete` | Complete a task |
| `POST .../tasks/{taskID}/readmit` | `orchestrator:task_readmit` | Return a task to the queue |
| `POST`, `GET .../tasks/{taskID}/artifacts`; `GET .../artifacts/{name}` | `orchestrator:artifact_submit` / `artifact_read` | Typed task artifacts |
| `POST`, `GET /api/v1/orchestrator/workflows`; `GET`, `PUT`, `DELETE .../workflows/{name}` | `orchestrator:workflow_write` / `workflow_read` | Workflow definitions |
| `POST /api/v1/orchestrator/hooks` | `orchestrator:hook` | Agent lifecycle hook receiver |
| `GET /api/v1/orchestrator/cost` | `orchestrator:status` | Consumption ledger rollup (below) |
| `GET`, `PUT /api/v1/orchestrator/spend-policy` | `orchestrator:status` / `spend_policy:write` | The tenant's spend limits |
| `GET`, `PUT /api/v1/orchestrator/cost-settings` | `orchestrator:status` / `cost_settings:write` | Local-inference costing |

Not every route is in this table: `GET /api/v1/orchestrator/attention` and
`POST .../attention/{taskID}/resolve` are the attention inbox, documented with
the SDLC module in [../software-factory/api.md](../software-factory/api.md);
the `/api/v1/fleet` routes are in [../agent-fleet/api.md](../agent-fleet/api.md#rest).

**Streaming.** `SubscribeSessionEvents` is server-streaming on gRPC and a
server-sent-event stream on REST (`sdk/go/transport/rest/orchestrator_stream.go`);
`GetSessionHistory` over gRPC is that stream read to its end with `follow=false`
(`sdk/go/transport/grpc/orchestrator_handler.go`).

**MCP tools** (`pkg/api/mcp/orchestrator_tools.go`, `fleet_admin_tools.go`,
`cost_settings_tools.go`, `orchestrator_cache_tools.go`):
`quicbaas_orchestrator_{discover,register,list,spawn,prompt,session_history,session_conversation,status,close,rotate_account,list_sessions,list_backends,list_runners,decompose,health,broadcast,inference,start_job,get_job_status,cancel_job,list_jobs,list_workflows,task_create,task_block,task_readmit,task_delete,cost,spend_policy_get,spend_policy_set,cost_settings_get,cost_settings_set,cache_put,cache_get}`,
`quicbaas_task_{get,list}_artifact(s)`.
The attention tools are `quicbaas_orchestrator_attention_{list,resolve}`; the
`quicbaas_fleet_*` tools are in [../agent-fleet/api.md](../agent-fleet/api.md#mcp).

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
[The SDLC module](../software-factory/api.md#the-sdlc-module).

The seam itself is documented in [job-runners.md](job-runners.md).

#### `GET /api/v1/orchestrator/cost` — the consumption ledger, rolled up

The spend report: query parameters, the response, the two money figures and
the transports are in [../accounts/api.md](../accounts/api.md#spend-report).
Permission `orchestrator:status`.


The project, pooled-account and agent-catalog matrices, the reference-field
checks and the SDLC and attention-inbox routes are in
[../software-factory/api.md](../software-factory/api.md).

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
| `keep` | boolean | No | Keep a session bound to no job past the idle bound that reconciliation otherwise concludes it at. Persisted on the session record. Changes nothing for a session bound to a job, which still ends with its job. Default `false` |
| `gpu_required` | boolean | No | Restrict dispatch to daemons advertising GPU capability |
| `persistent` | boolean | No | Multi-turn session keeping context across prompts (**default `true`**) |
| `account_id` | string | No | Pin the session to a pooled provider account. A bare pin is deliberate: the session is never rotated off it. |
| `account_auto` | boolean | No | Select a pooled account with the named strategy, and authorize rotation across the whole pool |
| `account_strategy` | string | No | `static` \| `least_usage`; empty selects the registry default. Ignored unless `account_auto` is true. |
| `account_allow` | string[] | No | Restrict the session to these pooled account IDs and authorize it to rotate between them |
| `prompt_append` | string | No | Prompt material appended after the session's resolved prompt (`system_prompt`, else the Agent's, else its role's built-in charter). At most 64 KiB; more is `400`. See [agents.md](../software-factory/agents.md#run-time-prompt-material) |
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

Read one scope of the consumption ledger. Arguments and the response are in
[../accounts/api.md](../accounts/api.md#spend-report).

### quicbaas_orchestrator_cost_settings_get / _set

Read or replace a tenant's electricity rate and per-engine device draws.
Arguments and errors are in
[../accounts/api.md](../accounts/api.md#cost-settings).

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

Move an agent session onto a different pooled provider account. Permission:
`orchestrator:spawn`. Arguments and the response are in
[../accounts/api.md](../accounts/api.md#rotating-a-sessions-account).

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

MCP tool naming conventions and the registered orchestration tools with
their authorization mappings. Request and response schemas are the tools'
input structs in the registration files below.

Registration sites: `pkg/api/mcp/orchestrator_tools.go`,
`orchestrator_cache_tools.go`, `account_tools.go`, `project_tools.go` and
`fleet_admin_tools.go`, all behind the `mcp` build tag. Totals are not
stated here because they drift; list the registry for the current set.

### Naming Convention

The orchestration surface uses six tool prefixes:

| Prefix | Scope | Description |
|--------|-------|-------------|
| `quicbaas_orchestrator_*` | Session / project / job management | Agent lifecycle, project discovery, ACP session management, jobs, agent cache |
| `quicbaas_agent_account_*` | Pooled provider accounts | Credential envelopes, rotation, usage — see [../accounts/api.md](../accounts/api.md) |
| `quicbaas_fleet_*` | Fleet registry | Register / list / heartbeat the QuicBaaS fleet node daemons themselves |
| `project_*` | Project CRUD and agent grants | `project_create`/`get`/`list`/`update`/`delete`, `project_grant_fleet_node`, `project_revoke_fleet_node` |
| `quicbaas_task_*` | Task artifacts | `quicbaas_task_get_artifact`, `quicbaas_task_list_artifacts` |
| `agent_*` | Global agent admin | `quicbaas_fleet_disable`, `quicbaas_fleet_enable` |

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
| `quicbaas_orchestrator_spawn` | `orchestrator:spawn` | Create a persistent session on a backend; `keep` marks a session bound to no job as kept past the idle bound |
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

The `quicbaas_fleet_*` tools are in
[../agent-fleet/api.md](../agent-fleet/api.md#mcp). The pooled-account family is documented in
[../accounts/api.md](../accounts/api.md).

### Observability metrics

These are Prometheus metrics, not MCP tools. They are registered in
`pkg/metrics/metrics.go` and are live today.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_orch_peer_messages_total` | Counter | Total peer messages sent |
| `quicbaas_orch_peer_inbox_size` | Gauge | Current inbox size per agent |
| `quicbaas_orch_file_locks_active` | Gauge | Number of active file locks |
| `quicbaas_orch_file_lock_wait_duration_seconds` | Histogram | Time spent waiting for lock release |
| `quicbaas_factory_tokens_total` | Counter | Tokens of every recorded turn, by `tenant`, `project`, `kind`, `provider`, `backend`, `model`, `direction` (bounded labels; see [../accounts/engineering.md](../accounts/engineering.md#metrics)) |
| `quicbaas_factory_cost_micros_total` | Counter | Cost of priced turns in micro-dollars, same labels with `basis` = `equivalent` or `billed` |
| `quicbaas_factory_spend_deferrals_total` | Counter | Admission deferrals for a spent project or tenant, by `tenant`, `project`, `axis` |
| `quicbaas_orch_job_budget_utilization` | Gauge | Budget utilization percentage per job |
| `quicbaas_orch_agent_restarts_total` | Counter | Total agent restarts |
