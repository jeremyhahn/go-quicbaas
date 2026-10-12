# SDLC — API

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every transport the SDLC module and its attention inbox are served on:
> REST, gRPC/Unix, MCP, QUIC, the embedded transport and the Go and
> TypeScript SDKs. The CLI is [cli.md](cli.md); the design is
> [architecture.md](architecture.md). Conflicts with this document are defects.

## The SDLC module

The module's own surface is grouped by what it acts on: a project's binding
(read, bind, unbind), its onboarding (provision, conformance), its forge
(open work, file an issue, list collaborators, adopt an issue), a pull
request's reviewer override (read, set, clear), a task's delivery, the
build's forge adapters, the host's worker image (status, refresh) and the
webhook. Tasks, Jobs and Sessions are the orchestrator's on every transport;
readmitting a blocked task is `orchestrator:task_readmit`. The REST table
below is the authoritative list.

### Permissions

| permission | roles | verbs |
|---|---|---|
| `sdlc:read` | admin, operator | read a binding, read a delivery, list the forges, **read a project's onboarding conformance**, read a project's open work, read a pull request's reviewer |
| `sdlc:bind` | admin | attach or replace a binding, **provision (onboard) its repository**, set or clear a pull request's reviewer override |
| `sdlc:unbind` | admin | detach a binding |
| `sdlc:issue_create` | admin, operator | file an issue on the bound repository; every filing is audited |
| `orchestrator:task_create` | the engine's | adopt an issue no task holds — it creates a task, so it is task creation's permission |
| `worker_images:read` | admin, operator — **cross-tenant** | read the worker image refresh's status and last pass |
| `worker_images:refresh` | admin — **cross-tenant** | queue a worker image refresh pass |

The two `worker_images:*` permissions are cross-tenant (qrdb `ScopeCross`):
the refresh rebinds images in EVERY tenant's bindings, so only a system-tenant
identity exercises them. See [the worker image refresh](../software-factory/worker-image.md).

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
the path. Every `forge:` field is described in [the project binding](engineering.md#the-project-binding);
`ssh_host_key` is required when the project's repository URL is an ssh one.

```json
{
  "forge": {
    "type": "gitea", "base_url": "https://git.example.com/api/v1",
    "owner": "example", "repo": "go-truststrap",
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
  "modules": {"rewrite_prefix": "https://github.com/example/"},
  "review": {"required_approvals": 1, "require_independent_reviewer": true,
             "require_green_checks": true, "reviewers": {"codex": "openai"}},
  "max_review_rounds": 3,
  "ranking": {"default_priority": 0},
  "onboarding": {"protected_branch": "main",
                 "required_status_contexts": ["CI / CI Gate (pull_request)", "quicbaas/review"]}
}
```

The `onboarding` block is optional and every field of it defaults; see
[onboarding](architecture.md#onboarding-a-repository).
The `modules` block is optional: set `rewrite_prefix` when the project
depends on private Go modules the forge hosts under its owner; see
[private modules](engineering.md#modulesrewrite_prefix--private-go-modules-resolve-at-the-forge).
`worker` and `triage` are [Agent references](../software-factory/agents.md): `{"agent_id": ...}`
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

#### `POST /api/v1/sdlc/projects/{projectID}/adopt`

Hands an issue no task holds back to the factory: one whose task completed
while its pull request is still open, or one triage holds with no task at
all. The attention inbox and `task_readmit` act only on a *blocked* task;
this is the path for everything else. Body:

```json
{"issue": 209, "pull_request": 210, "ruling": "finish the coverage half only"}
```

`issue` is required. `pull_request`, when set, must be open and is the pull
request the task resumes: its head branch becomes the delivery's branch, so
the attempt does not cut a rival one. `ruling`, when set, is appended to the
task's contract and posted on the issue. Answers `201`:

```json
{"task_id": "…", "issue": 209, "pull_request": 210,
 "branch": "issue/209-…", "superseded": "…"}
```

`superseded` names the finished task that held the issue before, if any. A
closed issue, a pull request that is not open, or one with no head branch is
`409` (`ErrAdoptionRefused`); an issue a live task already holds is `409`
(`ErrTaskSourceAlreadyAdopted`). When the task was created but a forge write
after it did not land, the answer is `502` (`ErrAdoptionIncomplete`) naming
the task, which runs regardless.

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
{"project_id": "go-truststrap", "repo": "example/go-truststrap",
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
{"project_id": "go-truststrap", "repo": "example/go-truststrap",
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
classes are listed in [architecture.md](architecture.md#what-conformance-checks).

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
{"project_id": "go-luks2", "repo": "example/go-luks2", "agent": "agent-go-luks2",
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
adoption, err := client.SDLC().AdoptIssue(ctx, &sdlc.AdoptIssueRequest{TenantID: tenant, ProjectID: projectID, Issue: 209, PullRequest: 210})
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
projectId, {title, body, labels, parent})`, `.collaborators(tenantId, projectId)`,
`.adoptIssue(tenantId, projectId, {issue, pull_request, ruling})`, `.forges()`
(`sdk/typescript/src/sdlc.ts`; REST and MCP transports — see
[TypeScript transports](../orchestration/api.md#typescript-transports-projects-sdlc-accounts)).

The worker image verbs are on the orchestrator client, where the desktop app's
Wails binding reaches them: `client.orchestrator.getWorkerImageStatus()` and
`client.orchestrator.refreshWorkerImage()` (REST and Wails; the other browser
transports refuse). The web UI's Fleet page renders them as the *Worker
image* panel.

### CLI

`quicbaas sdlc` — every verb and flag is in [cli.md](cli.md).

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
    "issue_ref": "example/go-quicbaas#981",
    "issue_url": "https://forge.example.com/example/go-quicbaas/issues/981",
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
