# Software Factory — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Implementation contract for the Software Factory modules: exported surface,
> invariants, errors, configuration, persistence, tests and observability —
> the SDLC module, projects, pooled accounts and prompt sets. Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

The orchestration engine's contract (backends, sessions, the attach event log,
jobs and amendments, fleet, workflows, inference) is in
[../orchestration/engineering.md](../orchestration/engineering.md). This
document covers the modules built on it.

## The SDLC module

The engine's own contract is everything above. The `sdlc` module adds one
service, two records and one closed set of typed errors on top of it; the
design and the lifecycle are [sdlc.md](sdlc.md).

### Exported surface

| Symbol | Package | What it is |
|---|---|---|
| `sdlc.ProjectBinding` | `pkg/sdlc/binding.go:329` | The project's autonomous-development configuration: `Forge`, `Strategy`, `Worker`, `Verification`, `Review`, `MaxReviewRounds`, `Ranking` |
| `sdlc.ForgeConfig` | `pkg/sdlc/binding.go:78` | The `forge:` half — type, API root, owner/repo, trigger label, label map, secret refs, pinned `SSHHostKey` |
| `sdlc.ReviewPolicy` / `sdlc.WorkerConfig` / `sdlc.RankingPolicy` | `pkg/sdlc/binding.go:204,268,299` | Review gate, worker backend/model/image, dependency ranking |
| `sdlc.Delivery` | `pkg/sdlc/delivery.go:155` | What one attempt delivered: branch, base, head SHA, `DeliveryPullRequest`, `[]ReviewRecord`, `[]CheckRecord` |
| `sdlc.IssueSource` | `pkg/sdlc/issue_source.go:59` | The `forge_issue` `TaskSource`; `NewIssueSource(bindings, forges, pageLimit)` |
| `sdlc.Hooks` | `pkg/sdlc/hooks.go:98` | The lifecycle moments; `NewHooks(HooksConfig)` |
| `sdlc.SurfacePolicy` | `pkg/sdlc/blocked.go:36` | Retry attempts, interval and post timeout for surfacing a park to the issue |
| `sdlc.UnblockCondition` / `sdlc.UnblockKind` | `pkg/sdlc/readmission.go:122,74` | The typed vocabulary a park is expressed in, and what a sweep can decide for itself |
| `sdlc.DependencyAnalyzer` / `sdlc.DependencyManifest` | `pkg/sdlc/analyzer.go:99,66` | Blast-radius ranking inputs; `NewStaticManifestSource` for a declared graph |
| `sdlc.WebhookRouteIndex` | `pkg/sdlc/webhook_index.go:57` | The system-plane index that routes an unauthenticated webhook to a tenant |
| `service.SDLCService` | `pkg/service/sdlc_service.go` | `Bind`, `Unbind`, `Binding`, `GetBinding`, `ListBindings`, `Delivery`, `NudgeProject`, `ForgeFor`, `ForgeToken`, `WebhookSecret` |

### Invariants

- **`Bind` turns the project autonomous** (`enableAutonomy`,
  `pkg/service/sdlc_service.go:172`) and registers no workflow: an `sdlc` job
  has no workflow, and `default_workflow` belongs to the lead-driven
  `workflow` runner.
- **The binding stores secret *references*, never secret values.** `Bind`
  proves each reference resolves in the tenant's secret store
  (`checkSecrets`, `:247`) before the record is written.
- **A binding is refused for a project the pipeline could not deliver**
  (`checkProject`, `:230`): no signing policy, or a repository host outside
  `sdlc.repo_hosts`.
- **Tenancy is the caller's own.** The tenant bound to the request by the
  authenticator wins; a `tenant_id` query naming another tenant is refused
  rather than honoured (`sdlcTenant`, `pkg/api/rest/handlers_sdlc.go:55`).

### Error semantics and the status mapping

`respondSDLCError` (`pkg/api/rest/handlers_sdlc.go:80`) and its gRPC twin
`convertSDLCError` (`pkg/api/grpc/server_sdlc.go:182`) are the two mappings;
Unix inherits the gRPC one verbatim and QUIC the REST one.

| Error | HTTP | gRPC |
|---|---|---|
| `sdlc.ErrNotFound`, `orchestration.ErrProjectNotFound`, `service.ProjectNotFoundError` | `404` | `NotFound` |
| `sdlc.ErrAlreadyExists`, `sdlc.ErrVersionConflict`, `sdlc.ErrWebhookRouteConflict` | `409` | `AlreadyExists` |
| `sdlc.ErrInvalidInput`, `sdlc.ForgeNotRegisteredError`, `sdlc.StrategyNotRegisteredError`, `orchestration.ErrWorkflowValidation` | `400` | `InvalidArgument` |
| `rest.ErrSDLCTenantMismatch` — `tenant_id` is not the caller's own | `403` | `PermissionDenied` |
| `rest.ErrSDLCServiceUnavailable` — the module is not running on this node (`sdlc.ErrUnavailable` at the seams) | `503` | `Unavailable` |
| anything else | `500` | `Internal` |

**Fail-closed paths.** `NewForgeBackend` refuses a forge type this build
carries no adapter for rather than degrading; a webhook whose project,
secret or signature does not check answers the same `401 unauthenticated`
for all three, so the response distinguishes nothing an attacker could use;
a verification criterion with no containerized session is refused with
`VerificationNotContainerizedError` rather than run on the daemon; an
`ssh://` repository with no pinned host key is refused before the worker
spawns (`orchestration.SSHHostKeyError`).

### Configuration and persistence

The `sdlc:` section, what moved to `orchestration:`, and the keys the loader
now refuses **by name** (`config.RefuseRemovedSDLCKeys`,
`RemovedSDLCKeyError`) are in [sdlc.md](sdlc.md#configuration). The module
persists two tenant-scoped records — the binding and the delivery — plus one
system-plane webhook route index; everything else a reader sees is the
engine's Project, Task, Job and Session.

## Projects and pooled accounts

The Project record, its quota and permission matrix, and its service
invariants are [projects.md](projects.md). The pooled-provider-account model,
its two-record split, selection strategies, limit holds, rotation and cost
attribution are [accounts.md](accounts.md).

## Prompt sets

Every instruction the Software Factory sends an agent is markdown embedded in
the binary, not a Go constant. `pkg/orchestration/prompts` is the generic
package — `SetKind`, `PromptSpec`, `SetSpec`, a registry swapped as a whole
under `atomic.Pointer`, a validator, a `${name}` renderer and a content hash.
Two sets register from `init()`: `workflow`, owned by `pkg/orchestration` from
`promptdefaults/workflow/*.md`, and `sdlc`, owned by `pkg/sdlc` from
`promptdefaults/sdlc/*.md`. **The embedded files are the system tenant's
defaults.**

The invariants:

- **A body carries `${name}` holes and nothing else.** Every hole is declared;
  a required hole must stay; an optional one with no value renders as nothing.
  No conditionals, no loops — `text/template` in human-edited markdown cannot
  be validated. Loops, plurals, fenced logs, redaction and JSON assembly stay
  in Go as thin assemblers.
- **One validator, two call sites.** `Validate` refuses a blank body, an
  undeclared `${x}`, a missing required variable and a missing marker, each
  with a typed error that names the thing. It runs at registration — where a
  failure panics in `init()` like an unregisterable job runner — at
  `Store.Save`, and when a stored body is resolved for a job.
- **Markers come from the parser's constants.** The charter's markers are
  built from `TurnStopMarker` and every key of `stopCodeOutcomes`, so a stop
  code the runner dispatches on is a code the charter must teach. This is the
  enforced form of the review control the old constant asserted.
- **`Set.MustRender` panics**, and only assemblers with no error to return use
  it: the content is embedded and was validated against its own spec, so a
  failure is a defect the golden tests catch. Tenant-supplied content does not
  reach it unvalidated: the store refuses it at save and the resolver at
  launch.
- **Resolved once per job.** `launchJob` resolves a `PromptSetProvider`
  runner's set project → tenant → system → embedded, skipping records with
  `follow_parent=true`; the job renders from that `ResolvedSet` for its whole
  life and a resume resolves afresh. A failure refuses the launch with
  `prompts.ResolveError`. `JobStatus.Prompts` records a `VersionRef` per
  prompt and the `prompts:resolved` job audit event names every non-embedded
  answer. The SDLC issue source resolves once per poll and refuses the poll
  on failure.

**Persistence.** One `prompts.Record` per `(tenant, project | "", set, name)`
in the owning tenant's store via `qrdbsdk.TenantStoreResolver`; the system set
is the tenant-wide record under `qrdbsdk.SystemTenantID`. History is inline
and bounded by `MaxRevisions` (20). Every write is a compare-and-swap on
`Version`; a stale one is `VersionConflictError`. Writes are audited as
resource `prompt` (`create`, `update`, `rollback`, `reset`, `delete`) on the
orchestration store trail, identifiers only. Tenant creation seeds a copy of
each prompt with `follow_parent=true` and deletes its writes on rollback; the
boot reconcile (`pkg/server/prompt_reconcile.go`) moves the system set onto
the build's defaults first, then seeds what each tenant lacks, never touching
an edited record. Without an embedded qrdb node there is no store and every
job renders the embedded defaults.

**Permissions.** `prompts:read` and `prompts:write` are `admin` and
`operator`; `prompts:history` adds `auditor`. All are `ScopeSelf` and outside
the Security Officer's duties (`pkg/permission`). The system set is written
only by the boot reconcile.

Byte equality is the acceptance test: `testdata/prompts/*.golden` in both
packages were written by the previous builders and every assembler still
reproduces them exactly. The transports and SDK (go-quicbaas#805), the GUI
(#806) and the per-role charters (#807) are not built. Full description —
scopes, `follow_parent`, seeding, reconcile, and the catalogue of
instructions that ought to become code: [prompt-sets.md](prompt-sets.md).
