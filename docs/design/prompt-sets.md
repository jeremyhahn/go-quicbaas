# Prompt sets inside one Software Factory: consolidated docs, consolidated packages, tenant-owned editable prompts

> **Status: REFERENCE** · Last verified: 2026-10-12 · Design for planned work; deleted when the feature folder ships
>
> **PR1 (go-quicbaas#803) has landed**: the extraction, the embedded markdown,
> the spec registry, the validator, the renderer and the golden equivalence
> tests are built and are described as built in
> [../software-factory/prompt-sets.md](../software-factory/prompt-sets.md).
> Two deviations from the sketch below, recorded there: the shipped markdown
> lives at `pkg/<owner>/promptdefaults/<set>/*.md` rather than inside the
> generic package's own directory, so ownership and the system-tenant origin
> are visible in the tree; and the package move (`pkg/sdlc` →
> `pkg/orchestration/sdlc`) had not happened, so the `sdlc` set is owned from
> `pkg/sdlc` and moves with it.
>
> **PR2 (go-quicbaas#804) has landed** too: the store, inheritance, seeding,
> boot reconcile, resolution at launch, job version refs and the permission
> family are described as built in the same document. The transports, CLI,
> GUI and role charters are still unbuilt and are go-quicbaas#805 to #807.
> Deviations from the signatures below: the seeder is `SeedTenant` plus a
> per-tenant `Reconcile` that the server calls system tenant first, and
> `Change` carries a `Stamp{Author, Note, ExpectedVersion}`.

The approved design for making the Software Factory's prompts editable data
instead of Go constants, and for putting the whole feature — engine, SDLC
module, prompt sets — behind one docs folder and one package tree. It amends
the Software Factory plan of 2026-09-05 (revised 2026-09-06). Planned-but-
unbuilt work lives in `docs/design/` per `docs/README.md`; this document is
deleted when the feature ships and
[../software-factory/prompt-sets.md](../software-factory/prompt-sets.md)
becomes its as-built description.

## Context

Every instruction QuicBaaS sends to an agent is Go code: the 128-line `WorkerCharter` constant in `pkg/sdlc/contract.go:34`, the issue/findings/CI-red/blocked renderers beside it, and the workflow runner's string builders in `pkg/orchestration` (`buildWorkerPrompt` control_loop.go:1909, `buildReviewPrompt` :1877 plus a hardcoded reviewer system prompt :855, `buildDecompositionPrompt` orchestrator.go:3775 plus a planner system prompt duplicated at :2422/:2457, `buildValidationPrompt` validators.go:365, `JobMemory.Serialize`, the resume priming/handshake literals). Nothing documents them. The charter's comment says constants were chosen so an operator cannot change instructions without a code review.

Owner direction (2026-09-10): prompts are human-editable markdown kept with the code (embed + recompile acceptable), better still editable at runtime from the GUI; stored per tenant, seeded from the system tenant at tenant creation; **per project**, because an SDLC set differs from a generic workflow set that strings a fleet of agents together. Scoping answers: inheritance **system → tenant → project**; editing needs **history + rollback, validation on save, reset to parent, diff against parent**; editing is gated by a **`prompts` permission family any role can hold**, no hardcoded admin. Further rulings after the first draft: **the docs work covers the whole Software Factory / agentic-workflow feature with prompt sets inside it — one folder, not another top-level directory; and packages are organised the same way: `pkg/orchestration/{sdlc,agent,prompts}`, moved now.**

The review control the constant protected moves into the product: every save is a versioned, audited, permissioned write validated against the markers the engine parses, and every job records the exact prompt versions it ran with.

## Part A — one feature, one docs folder, one package tree

### Docs: `docs/software-factory/` — SHIPPED
Landed by PR-0; the folder described below now exists. Before it, the feature
was spread over three now-deleted folders: `agent-orchestration` (10 files), `sdlc` (7, non-conformant: no cli/engineering/user, no banners) and `agentic-projects` (5) — 15.4k lines. Target, following the handbook feature-docs standard (five canonical files; supplements allowed, as the engine folder already did):

```
docs/software-factory/
  architecture.md   engine + projects + accounts + SDLC module + prompt sets: boundaries, data flow, trust, rejected alternatives, scope fence
  engineering.md    exported surface, invariants, errors, configuration, persistence, tests, observability — all sub-areas
  cli.md            quicbaas orchestrator | agent | projects | accounts | sdlc | prompts — every verb and flag
  api.md            one row per capability across the six transports with the scp permission (orchestrator, projects, accounts, sdlc, prompts)
  user.md           task-ordered: register a project, bind SDLC, hand over an issue, watch the job, tune the prompts, manage accounts, run a workflow
  sdlc.md           supplement: binding, strategies, lifecycle of one issue, outcomes (absorbs sdlc/{architecture,binding,configuration,runbook,api,README})
  prompt-sets.md    supplement: the two sets, scopes, follow_parent, markers, editing, job version refs
  projects.md, accounts.md, job-runners.md, autonomy-and-dispatch.md, job-admission.md, sandbox.md, cost-enforcement.md   (existing supplements moved; the projects folder merged into projects.md/accounts.md). `cost-enforcement.md` does not exist on any merged branch and was not moved.
```
Rules: every file carries the status banner + `Last verified`; content is **moved and absorbed, not rewritten**, corrections noted in place; the three old folders are deleted and every inbound link repointed in the same change (`docs/README.md` rows 58-60 → one row; `README.md:478-479`; `docs/desktop-gui/*` 20 links; `docs/multiprotocol-api/architecture.md`, `docs/ssh/cli.md`, `docs/embeddings/user.md`, `docs/barrier/engineering.md`; `changelog.d/*` fragments stay as history); `docs/README.md` records the migration in its migration-state section. Publication rules: no internal hosts/paths.

### Packages: `pkg/orchestration/{sdlc,agent,prompts}`
- `git mv pkg/sdlc pkg/orchestration/sdlc`, `git mv pkg/fleet pkg/orchestration/agent` (keeps `agent/deploy`); import-path rewrite in the 9 non-test + 26 test importers (`pkg/api/{grpc,mcp,rest}`, `pkg/server`, `pkg/service`, `cmd/quicbaas`, `sdk/go/transport/embedded`) via `gofmt -r`/sed + `goimports`; package names stay `sdlc` and `agent`. Cycle check done: `pkg/orchestration` imports neither; `agent` imports `orchestration` (child → parent is fine); `sdlc` does not import `agent`.
- Build/CI references to update: `Makefile:1389` (`TestLive_` path), `:2635` (`INTEGRATION_VET_ROT`), `:3606-3628` (`test-agent`/`bench-agent`/`coverage-agent` → `./pkg/orchestration/agent/...`), `.github/workflows/ci.yml:469-479` (race pass path), the `pkg/sdlc/` comment in `scripts/verifynaming/main.go`; `CLAUDE.md` prose that names `pkg/sdlc`/`pkg/fleet`; changelog fragments untouched (history).
- New generic package `pkg/orchestration/prompts` (never imports its parent — the parent imports it; `sdlc` imports it too). `pkg/signalbridge` was not named in the ruling and stays.
- Sequencing: the move lands **after PRs 785 and 789 merge** (both touch `pkg/sdlc` and `pkg/orchestration`) and **before PR1**, as its own behaviour-free PR, so no in-flight lane rebases across a rename.

## Part B — prompt sets

### The model
```
embedded markdown (in the binary) ──seed/reconcile──► system tenant set (v1, v2… authored "build <commit>")
                                                          │ copied at tenant creation (follow_parent=true)
                                                          ▼
                                                    tenant set ──override──► project set (per project)
Resolution at job launch: project → tenant → system → embedded; a record with follow_parent=true is skipped,
so unedited copies track the parent without per-build copying. The job records []VersionRef{set,name,scope,version,hash}.
```
Two sets, each owned by the package that consumes it (both import the generic `pkg/orchestration/prompts`; `orchestration` never imports `sdlc`):

| set `sdlc` (pkg/orchestration/sdlc/prompts/*.md) | set `workflow` (pkg/orchestration/prompts/defaults/*.md … owned by the engine) |
|---|---|
| worker_charter, issue_contract, round_findings, verification_failed, review_findings, merge_conflict, changes_requested, ci_red, blocked_comment, outcome_comment | worker_prompt, worker_workspace, reviewer_prompt, reviewer_system, validation_summary, capabilities_context, memory_{header,worked,failed,constraints,feedback}, decomposition_prompt, decomposition_backends, planner_system, validation_prompt, resume_priming, handshake, charter_{lead,worker,reviewer,validator} (PR4) |

### Decisions (one-line reason each)
1. **Generic package `pkg/orchestration/prompts`**: spec registry, validator, renderer, record/store, resolver, seeder. Owners register a `SetSpec` from `init()`; markers such as `SDLC-STOP:` are declared by sdlc from its own `turn.go` constants, so the validator learns them by registration.
2. **Templating = `${name}` substitution against a per-prompt declared variable list.** `text/template` is unused in the repo and would let user-edited markdown carry `{{if}}/{{range}}` that cannot be validated; loops, plurals, fenced logs, redaction, renumbered comments and every JSON parser stay in Go as thin assemblers over the set.
3. **One validator, two call sites**: at registration (embedded default; failure panics in `init` like `RegisterJobRunner`) and at `Store.Save`. Rules: non-blank; every `${x}` declared; every required variable present; every marker literal present. The typed error names the marker/placeholder.
4. **One record per (tenant, project|"", set, name)** with `Content`, `ContentHash`, `Version`, `FollowParent`, bounded inline `Revisions` (`MaxRevisions = 20`). Concurrency is decided by the store, not by a read before the write: a create is `PutIfAbsent` and an edit is `CompareAndSwap` against the exact bytes read, both from `qrdbsdk.AtomicKVStore`; a lost race is a typed `VersionConflictError`, and a store without those verbs is refused (`ErrGuardedWriteUnsupported`). The DAO's `SaveWithCondition` is a read followed by a write and is not used. Rollback and reset are new revisions; history is append-only. System scope = the tenant-wide record under `qrdbsdk.SystemTenantID`; only the boot reconciler writes it (transports refuse system-scope writes).
5. **Resolution once per job in `launchJob`** (`pkg/orchestration/job_envelope.go:69-140`, beside runner/project/workflow/hooks) via an optional runner interface `PromptSetProvider{PromptSet() SetKind}`; the `ResolvedSet` rides `JobRunConfig.Prompts` → `JobRun.Prompts` → `JobContext.Prompts`; `JobStatus.Prompts []VersionRef` is persisted and audited (`prompts:resolved`). Resolve failure refuses the launch. Nil store (no qrdb) resolves to embedded.
6. **`AgentRoleSpec.SystemPrompt` retired in PR4**: read at one site (control_loop.go:479); an unversioned, unvalidated, unpermissioned back door around the store. Role charters come from the workflow set; the loader refuses `system_prompt` with a typed error naming the store.
7. **Seeding = tenant-create step 10** (`pkg/iam/tenant.go:298-315` after `SeedBuiltInRoles`, same rollback chain, idempotent). **Boot reconcile** (`pkg/server/prompt_reconcile.go`, mirrors `role_reconcile.go`): rewrite the system set when the embedded hash changed (revision authored `build <version.GitCommit>`), seed-if-absent for existing tenants, never touch a record with `follow_parent=false`.
8. **Permissions** `prompts:read` (list/get/effective/diff), `prompts:write` (put/rollback/reset), `prompts:history` (revision log, identifiers only). Tenant-scoped (`ScopeSelf`, never `ScopeCross`). Built-in placement: read/history → Admin, Operator (history also Auditor); write → Admin, Operator; all `dutyOutsideSecurityOfficer`; no wildcard; any custom role may hold them. Fleet node daemons do not need them: resolution happens on the control plane and the rendered text travels in `SpawnSessionRequest`.

### Package layout (after the move)
```
pkg/orchestration/prompts/{doc,spec,validate,render,hash,record,store,resolver,seed,errors}.go (+ tests)
pkg/orchestration/prompt_set.go + pkg/orchestration/prompts/defaults/*.md   (SetWorkflow; go:embed; init RegisterSet)
pkg/orchestration/sdlc/prompt_set.go + pkg/orchestration/sdlc/prompts/*.md  (SetSDLC; markers from turn.go constants)
pkg/service/prompt_service.go, pkg/server/lifecycle_prompt.go, pkg/server/prompt_reconcile.go
pkg/api/rest/handlers_prompt.go, api/proto/prompt.proto + pkg/api/grpc/server_prompt.go, pkg/api/mcp/prompt_tools.go
sdk/go/client/prompt/client.go, sdk/go/errors/prompt_transport.go, sdk/go/transport/{rest,grpc,mcp,embedded}/prompt_handler.go
cmd/quicbaas/prompts.go; sdk/typescript/src/prompts.ts; frontend {views/orchestrator/PromptsView.svelte, components/orchestrator/Prompt{Editor,HistoryPanel,DiffPanel}.svelte, lib/prompts.ts}
```
Key signatures: `RegisterSet(SetSpec) error` (atomic.Pointer map swap), `Validate(spec, content) error`, `Render(spec, content, Vars) (string, error)`, `Hash(content) string` ("sha256:…"), `Store{Get, List, Save(ref, Change{Content, Author, Note, ExpectedVersion}), Rollback, Reset, Delete, History}` over `qrdbsdk.TenantStoreResolver` using the `entityDAO` idiom from `pkg/sdlc/store.go:60-78` (entity ID = FNV-64a of `projectScope/set/name` with the `_tenant` sentinel from `account_store.go:40`; collisions refused by field comparison), `Resolver.Resolve(ctx, tenantID, projectID, kind) (*ResolvedSet, error)`, `ResolvedSet.{Render, Get, Refs}` (immutable), `Seeder.{SeedTenant, ReconcileSystem, ReconcileTenants}`. Typed errors only: `MissingMarkerError{Set,Prompt,Marker}`, `UnknownPlaceholderError`, `MissingVariableError`, `NotFoundError`, `VersionConflictError`, `UnknownRevisionError`, `ResolveError`, `SeedError`, `BackendError`.

### Verbs (every transport; scope addressed by `tenant_id` + `scope=tenant|project|system` + `project_id`; `system` read-only)
| verb | REST | gRPC (`quicbaas.prompt.v1.PromptService`) | MCP | SDK `Prompts()` / CLI `quicbaas prompts` | perm |
|---|---|---|---|---|---|
| List | `GET /api/v1/prompts/{set}` | ListPrompts | prompt_list | List / `list <set>` | read |
| Get (`?version=N`) | `GET /api/v1/prompts/{set}/{name}` | GetPrompt | prompt_get | Get / `get` | read |
| Effective (+chain) | `GET …/{name}/effective` | GetEffectivePrompt | prompt_effective | Effective / `effective` | read |
| Diff vs parent | `GET …/{name}/diff` | DiffPrompt | prompt_diff | Diff / `diff` | read |
| History | `GET …/{name}/history` | GetPromptHistory | prompt_history | History / `history` | history |
| Put (CAS on `expected_version`, note required) | `PUT …/{name}` | PutPrompt | prompt_put | Put / `put -f FILE --note` | write |
| Rollback | `POST …/{name}/rollback` | RollbackPrompt | prompt_rollback | Rollback / `rollback --version N` | write |
| Reset to parent | `DELETE …/{name}` | ResetPrompt | prompt_reset | Reset / `reset` | write |

Status mapping: not found → 404/NotFound; version conflict → 409/Aborted; missing marker, unknown set/name, system-scope write → 400/InvalidArgument (body names the marker); unmounted → 503/Unavailable. `Get` is 404 when nothing is overridden at that scope; `Effective` always answers with `origin` and the chain. QUIC = REST over HTTP/3 (parity test only); Unix = gRPC over UDS (delegation test only). TypeScript SDK implements REST (the GUI's transport) and null-stubs the other five as projects/sdlc/accounts do; one issue covers the family on non-REST TS transports.

### Injection call sites (behaviour-preserving cut)
sdlc: `runner.go:704` charter → `Render("worker_charter")`; `:816/:874/:931` findings framing; `issue_source.go:133` `IssueContract(set, spec)` (resolver into `NewIssueSource`, resolved once per project per poll); `gate.go:289,477`; `blocked.go`/`outcome.go` from `JobContext.Prompts`; `Runner.PromptSet()` → `SetSDLC`.
workflow: `control_loop.go:652`, `:840-858`, `:296/:301`, `orchestrator.go:2413-2457` (two literals → one prompt), `validators.go:384-395`, `session_resume.go:244`, `acp_backend.go:1177`; PR4: `:479/:1626/:1691` role charters.

### GUI
Standalone page `#/prompts?set=&project=` under SOFTWARE FACTORY (model `AccountsView.svelte`; router/navigation/MainApp + `page-header-consistency` registration). Toolbar: scope picker (tenant | project), set tabs. List: name, badge `overridden here` / `inherited from tenant` / `inherited from system`, effective version + hash8. Editor: monospace textarea (no new dependency), legend chips for placeholders and required markers with a red chip per missing marker (client pre-check; server authoritative), required note, save with CAS (409 banner names who changed it; 400 names the marker). Inherited state shows the effective body read-only with "Override here". Panels: history (view vN, roll back with note), diff against parent, effective preview with the chain. 403 → empty state naming the permission, write verbs hidden; 503 → "prompt store is not running on this node". Job detail gains a "Prompts" group (`set/name · scope · vN · hash8`, linking to the page).

## Tests (no skip guards, no -short, typed errors, ≥90%)
- `prompts`: registry, validator, renderer, hash, store on `qrdbsdk.NewMemoryKVStore` (create v1, stale CAS → conflict, history bound, rollback/reset semantics, collision refusal, sealed store → BackendError), resolver (layer order; follow_parent skipped; system tenant's own chain; nil store → embedded; store error → ResolveError), seeder (copy with follow_parent; idempotent; mid-failure deletes own writes; system v1 → v2 on hash change; tenant edits untouched; seed-if-absent).
- **Golden equivalence in PR1**: `testdata/prompts/*.golden` generated from today's builders; the charter and every renderer byte-equal across their variable combinations. A spec test proves the sdlc markers come from `turn.go` constants.
- Orchestration: `launchJob` refuses on resolve error; runner without `PromptSetProvider` gets nil; `JobStatus.Prompts` persisted; resume re-resolves; `prompts:resolved` audit. IAM: tenant create seeds after roles, rolls back on seeder failure. Server: reconcile ordering and targets.
- Surface: service (scope rules, project ownership, system read-only), REST (statuses, 503, tenant mismatch 403, frozen goldens + `baseRouteTableSize` bump + `TestRESTPermissions_EveryMappingIsRegistered`), gRPC (`TestGRPCPermissions_*`, `TestPromptServiceDescIsGuarded`), MCP (`TestMCPPermissions_EveryToolMappedOrPublic`, zero `sdkCallsWithoutMCPTool` entries), permission guards, SDK `refuseUnwired`, six-row transport matrix, four parity + quic + unix tests, CLI shape/format tests, TS path tests, frontend unit + e2e.
- Move PR: `make test` full suite + `make ci` targets green with zero behaviour change; `go vet` on every tag set; the `go:build` tag coverage check.
- Docs PR: every identifier grepped, every path/line reference opened, every link resolved (documentation.md §3); `make lint` (verify-naming) clean.
- **Burn-in (go-truststrap)**: `quicbaas prompts effective sdlc worker_charter --project <id>` → origin system; override in the GUI with a sentinel line → v1 `overridden here`; hand over an issue; the job's `prompts[]` shows `sdlc/worker_charter scope=project v1 hash=<h>` equal to the record's hash; the worker's system prompt carries the sentinel (session record / `--system-prompt` in the sandbox log); audit `resource=prompt action=update` identifiers only; dropping the `SDLC-STOP:` clause is refused naming it; reset → next job records `scope=system`; restart with a changed embedded file → system v2, project edit untouched.

## PR slices (each through a lane, WIP → review → auto-merge; docs + changelog fragment in the same PR)
0. **PR-0 docs consolidation** (behaviour-free, first): `docs/software-factory/` built from the three folders per Part A, sdlc's missing cli/engineering/user content written into the canonical files, banners everywhere, links repointed, old folders deleted, `docs/README.md` row + migration note, `docs/design/prompt-sets.md` (this plan).
1. **PR-move** (behaviour-free, after 785 and 789 merge): `pkg/sdlc` → `pkg/orchestration/sdlc`, `pkg/fleet` → `pkg/orchestration/agent`, importers/Makefile/CI/CLAUDE.md updated, docs paths in `docs/software-factory/` updated; full suite green.
2. **PR1 extraction + embed + goldens + validator** (no store, no behaviour change): `pkg/orchestration/prompts` spec/validate/render/hash/errors; both `prompt_set.go` + markdown; every builder/const replaced by an assembler over `prompts.Embedded(kind)`; goldens; `prompt-sets.md` supplement first cut. Depends on #789 (markers) and #790 (`outcome_comment`).
3. **PR2 store, seeding, reconcile, resolver, injection, job refs, permissions**.
4. **PR3a service + REST + gRPC + MCP + Go SDK (all transports, matrix, parity) + CLI + docs (api/cli/engineering rows).**
5. **PR3b TypeScript SDK + GUI + job-detail refs + frontend tests + e2e + rebuilt dist (both copies) + user.md.**
6. **PR4 role charters + `AgentRoleSpec.SystemPrompt` retired + loader refusal + docs.**

Out of scope, filed as issues: signalbridge `Intent.Goal` comment/code discrepancy; non-REST TypeScript transports for the Software Factory family; a markdown-capable editor component; moving `pkg/signalbridge` (not in the ruling).

## Verification recipe
`GOFLAGS=-count=1 make test test-standard test-lean test-minimal test-sdk-go`, `make vet lint`, `-race` on prompts, orchestration, sdlc, server, service, api/*; `make test-frontend build-frontend test-e2e`; CI green on every job; then the burn-in steps above on the go-truststrap node after a rebuild (daemon + worker image).
