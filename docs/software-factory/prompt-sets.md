# Prompt sets

> **Status: NORMATIVE** · Last verified: 2026-10-15
> Every instruction the Software Factory sends an agent is markdown embedded in
> the binary, declared, validated and rendered by `pkg/orchestration/prompts`,
> and kept as tenant-scoped, versioned records that each job resolves once at
> launch (go-quicbaas#804). Both are built and described here as built. The
> transports, the CLI, the GUI and the per-role charters are not: they are
> go-quicbaas#805 to #807, specified in
> [The editing surface](#the-editing-surface-not-built-yet).

## What exists

Two **sets** of human-editable markdown, each owned by the package that sends
it, over one generic prompt package.

| set | owner | shipped content | sent to |
|---|---|---|---|
| `workflow` | `pkg/orchestration` | `pkg/orchestration/promptdefaults/workflow/*.md` | the lead, worker, reviewer and validator of a generic agent workflow |
| `sdlc` | `pkg/sdlc` | `pkg/sdlc/promptdefaults/sdlc/*.md` | the worker driving one forge issue, and the person watching that issue |

**The embedded files are the system tenant's set.** That is what the
`promptdefaults/` directory name says and what each directory's `README.md`
repeats: they are compiled in, the boot reconcile writes them into the system
tenant's records, and they are what every new tenant is seeded from and
inherits until it overrides something. Nothing reads a file at runtime. How
the records are scoped, seeded, reconciled and resolved is
[Scopes and resolution](#scopes-and-resolution).

```
pkg/orchestration/prompts/                      the generic package: spec, registry, validator, renderer, hash
pkg/orchestration/prompt_set.go                 declares set "workflow"; //go:embed promptdefaults/workflow
pkg/orchestration/promptdefaults/README.md      the editing rules
pkg/orchestration/promptdefaults/workflow/*.md  28 prompts — the system tenant's defaults
pkg/sdlc/prompt_set.go                          declares set "sdlc"; //go:embed promptdefaults/sdlc
pkg/sdlc/promptdefaults/README.md               the editing rules
pkg/sdlc/promptdefaults/sdlc/*.md               28 prompts — the system tenant's defaults
```

The generic package never imports its parent, and `pkg/orchestration` never
imports `pkg/sdlc`; both owners import `prompts`.

## The contract

### Templating is substitution, not a language

A body may carry `${name}` holes and nothing else. A hole must be declared by
the prompt's spec; a hole declared `Required: true` must stay in the body; an
optional hole with no value renders as nothing, which is how a clause the
engine has nothing to say for disappears. There are no conditionals and no
loops.

`text/template` was refused deliberately: it is unused in this repository and
would let human-edited markdown carry `{{if}}` and `{{range}}` that no
validator can judge. An unvalidatable prompt is the failure the whole store
exists to prevent.

A substituted value is never rescanned, so text an agent wrote cannot open a
hole of its own.

### Validation happens twice, on four rules

`prompts.Validate(kind, spec, content)` is the only validator, and it runs at
registration, at `Store.Save`, and again when a stored body is resolved for a
job. The rules:

1. the body is not blank;
2. every `${x}` in it is declared;
3. every required variable appears;
4. every declared marker literal is present.

Each refusal is typed and names the thing: `UnknownPlaceholderError` names the
placeholder, `MissingMarkerError` names the marker **and what reads it**.

A shipped default that fails its own specification panics in `init()`, the way
an unregisterable job runner does. A binary that would send agents
instructions the engine cannot act on refuses to start.

### Markers come from the parser's own constants

`pkg/sdlc/prompt_set.go`'s `workerCharterMarkers()` builds the charter's
markers from `TurnStopMarker` and **every key of `stopCodeOutcomes`** — the
table `ClassifyTurn` dispatches on. A stop code added to that vocabulary is a
code the charter must teach, and a charter that does not name it fails
registration. The validator therefore learns what `SDLC-STOP:` is by
registration rather than by having it retyped.

`reviewerCharterMarkers()` does the same for `sdlc/reviewer_charter`, over
`ReviewVerdictMarker` and **every key of `reviewCodeVerdicts`** — the closed
set `ParseReviewVerdict` reads. It adds two markers nothing parses, and that is
deliberate (#927): `ReviewerCIGreenMarker` and `ReviewerNoRerunMarker` are the
sentences that tell the reviewer its head's pipeline has already passed and
that it must not re-run it. The dispatcher now guarantees the first is true —
no review starts on a head that is not green — so a charter that stopped
saying so would leave an agent spending a model, and fourteen minutes of wall
clock, reproducing an answer it was handed. A marker is for anything an edit
must not silently drop, not only for what a parser reads.

Both halves are required: an agent told only that CI passed still reaches for
`make test`, because running the tests is what a careful reviewer does when
nothing forbids it.

That is what replaces the old control. The charter used to be a Go constant
whose comment said the point was that an operator could not change what an
agent is told without a code review; `TestWorkerCharterMarkers_ComeFromTheParsersOwnConstants`
and `TestWorkerCharter_RefusesABodyThatDroppedAStopCode` are the same
guarantee, enforced rather than assumed.

Markers are declared on the machine-read workflow prompts too: `reviewer_system`
must keep `approved` and `new_work_items`, `planner_system` must keep `JSON`,
`validation_prompt` must keep `passed`, `resume_priming` must keep `READY`.

### Files

A default file ends with **exactly one** trailing newline, and that newline is
not part of the prompt: `prompts.Defaults` removes it. Whitespace between
assembled pieces belongs to the Go assembler, never to the file, so a prompt
stays something an editor can save without changing what an agent is told. A
body that must end on a blank line ends its file with two newlines. Tests in
both owning packages hold every file to this and refuse a file no
specification declares.

## Exported surface

| identifier | what it is |
|---|---|
| `SetKind` | names one set |
| `Variable{Name, Description, Required}` | one `${name}` hole |
| `Marker{Literal, Reason}` | a literal the engine parses, and what reads it |
| `PromptSpec{Name, Description, Default, Variables, Markers}` | one prompt's declaration |
| `SetSpec{Kind, Description, Prompts}` | a whole set; `Prompt(name)`, `Names()` |
| `Registry`, `NewRegistry` | a registry; the process one is package level |
| `RegisterSet`, `MustRegisterSet` | register; `Must` panics, for `init()` |
| `LookupSet`, `Embedded`, `Sets` | read the process registry |
| `Set` | an immutable registered set: `Kind`, `Spec`, `Names`, `Body`, `Render`, `MustRender` |
| `Validate(kind, spec, content)` | the four rules |
| `Render(kind, spec, content, Vars)` | `${name}` substitution |
| `Hash(content)` | `sha256:…`, what a job will record |
| `Defaults`, `MustDefaults`, `UnusedDefaults` | read embedded markdown; name dead files |
| `FileExtension`, `HashPrefix` | `.md`, `sha256:` |
| `Ref{TenantID, ProjectID, Set, Name}`, `Record`, `Revision`, `MaxRevisions` | one stored prompt, its bounded inline history |
| `Store`, `NewStore`, `WithAuditor`, `WithRegistry` | `Get`, `List`, `History`, `Save`, `Rollback`, `Reset`, `Delete` over a tenant store resolver |
| `Change{Stamp, Content, FollowParent}`, `Stamp{Author, Note, ExpectedVersion}` | one write and who made it |
| `Auditor`, `AuditEvent`, `AuditResource`, `AuditAction*` | the write audit: identifiers only, never a body |
| `Scope` (`project`, `tenant`, `system`, `embedded`) | which layer answered a resolution |
| `Resolver`, `NewResolver`, `ResolvedSet`, `VersionRef` | resolve a set once; render from it; name what was rendered |
| `Renderer`, `RendererOr` | what an assembler renders from; a resolved set or a fallback |
| `Seeder`, `NewSeeder`, `SeedReport`, `SeedAuthor` | seed a tenant; reconcile at boot |

Typed errors, all with sentinels for `errors.Is`: `InvalidSpecError`,
`SetAlreadyRegisteredError`, `UnknownSetError`, `UnknownPromptError`,
`BlankPromptError`, `UnknownPlaceholderError`, `MissingVariableError`,
`UnknownVariableError`, `MissingMarkerError`, `DefaultsUnreadableError`, and
for the store `InvalidInputError`, `NotFoundError`, `VersionConflictError`,
`UnknownRevisionError`, `CollisionError`, `BackendError`, `ResolveError`,
`SeedError`.

`Set.MustRender` panics. It is for assemblers on a path with no error to
return, and the failure it panics on — an assembler naming a variable the
prompt does not declare, or withholding a required one — is a defect in this
binary that the golden tests catch before it ships, not a state a running node
can reach. A tenant-supplied body does not change that: the store refuses an
invalid body at save and the resolver refuses it again at launch, so a job
only ever renders bodies that passed the same four rules, and a bad body is a
launch refusal rather than a panic.

**One prompt sits outside this machinery, and must be folded into it.** The
triage charter is human-editable markdown embedded with its own `go:embed` —
`pkg/sdlc/prompts/triage.md`, served by `sdlc.TriageCharter()` — rather than a
registered spec, because it was written while this work was in flight.
`pkg/sdlc` therefore has two prompt mechanisms where it should have one, and
**that duplication is tracked as #883**: the charter moves into the `sdlc` set
and the private `embed.FS` is deleted. It was deliberately built as one charter
behind one accessor so the migration is a rename rather than a rewrite. See
[triage.md](../sdlc/triage.md).

## Scopes and resolution

### Four layers

```
embedded markdown ──boot reconcile──► system tenant record (authored "build <commit>")
                                          │ copied at tenant creation, follow_parent=true
                                          ▼
                                    tenant record ──edit──► project record (per project)
```

| scope | record | written by |
|---|---|---|
| `project` | `Ref{tenant, project, set, name}` | an edit for one project |
| `tenant` | `Ref{tenant, "", set, name}` | the seeder, then the tenant's edits |
| `system` | `Ref{qrdbsdk.SystemTenantID, "", set, name}` | the boot reconcile only |
| `embedded` | none — the build's file | the build |

A job resolves each prompt **project → tenant → system → embedded**, taking the
first record that does not follow its parent. The tenant step is skipped when
the job's tenant is the system tenant, whose tenant-wide record *is* the
system record.

### follow_parent

`follow_parent=true` marks an unedited copy: resolution reads through it to
the layer above. Every seeded copy carries it, so a tenant tracks the system
set — and through it each new build — without per-build copying. An edit or a
rollback clears it; `Store.Reset` sets it again as a new revision that keeps
the body. Every write is a revision; history is append-only apart from the
`MaxRevisions` (20) bound.

### Seeding

Creating a tenant copies the system set into it, one record per prompt with
`follow_parent=true`, authored `seed`. It is the step after the built-in roles
in `pkg/iam`'s tenant creation (`DefaultTenantManager.SetPromptSeeder`), it is
idempotent — a prompt the tenant already holds is left alone — and a failure
deletes every record it wrote, so a rolled-back tenant leaves nothing behind.
A copy starts from the system record's body, or the embedded default before
the system set was ever written.

### Boot reconcile

`pkg/server/prompt_reconcile.go` runs once at boot, beside the role reconcile:

1. **The system tenant first**, onto this build's defaults. A missing record
   is created; a record that follows the build and whose hash differs from
   the embedded file gets a new revision authored `build <commit>`. A record
   that does not follow is never touched.
2. **Every other tenant**, by seeding the prompts it lacks — a tenant that
   predates the store, or a prompt a newer build added. An edited record is
   never touched.

One log line is written per record written. A tenant that fails — a sealed
barrier, typically — does not stop the others; failures are returned joined
and the next boot retries them. With no embedded qrdb node there is no store:
nothing is reconciled and every job resolves to the embedded defaults.

### Resolution at launch

`launchJob` resolves a runner's set **once**, beside its project and
workflow, when the runner implements `PromptSetProvider` (`PromptSet()
SetKind`): the workflow runner answers `workflow`, the SDLC and review runners
`sdlc`. The `ResolvedSet` rides `JobRunConfig.Prompts` → `JobRun.Prompts` →
`JobContext.Prompts` and the job renders from it for its whole life, however
the store is edited meanwhile. A resumed job resolves afresh.

- **A set that cannot be resolved refuses the launch** with the
  `*prompts.ResolveError` naming the prompt. That includes a stored body that
  no longer validates: a job never runs on instructions nobody can name.
- **`JobStatus.Prompts`** (`prompts` in JSON) persists one `VersionRef{Set,
  Name, Scope, ScopeID, Version, Hash}` per prompt.
- **The `prompts:resolved` job audit event** names the set and every prompt a
  layer other than the build answered, as `name=scope:id@vN` — identifiers
  only, never a body.

The SDLC issue source renders contracts before any job exists, so it resolves
the project's `sdlc` set once per poll (`IssueSource.SetPromptResolver`) and
refuses the poll when it cannot.

### What still renders the build's defaults

Some prompts are rendered where no job or tenant is in scope, so they read the
embedded set directly: the ACP handshake; session-resume priming; the
process-wide role charters registered with `RegisterRoleCharter`;
`JobMemory.Serialize`; workflow prompts rendered inside an SDLC job (which
resolved `sdlc`, not `workflow`); outcome comments posted from the
conclusion seam and the orphaned-label comment, which have no job; and the
adoption contract `IssueAdopter` posts. The per-role charters move into the
store with go-quicbaas#807.

### Persistence and audit

One record per `(tenant, project | "", set, name)`, stored in the owning
tenant's store through `qrdbsdk.TenantStoreResolver` — so a sealed tenant
barrier seals its prompts with everything else. The entity id is derived from
the ref; a collision is refused by comparing fields (`CollisionError`). Every
write is a compare-and-swap on `Version` (`ExpectedVersion` 0 creates);
a stale version is `VersionConflictError`. Each write is audited as resource
`prompt`, action `create`, `update`, `rollback`, `reset` or `delete`, on the
same trail as the orchestration stores, identifiers only.

### Permissions

| permission | built-in roles | covers |
|---|---|---|
| `prompts:read` | `admin`, `operator` | list, get, the effective body and its chain, diff |
| `prompts:write` | `admin`, `operator` | put, rollback, reset |
| `prompts:history` | `admin`, `operator`, `auditor` | the revision log, identifiers only |

All three are tenant-scoped (`ScopeSelf`) and outside the Security Officer's
duties; any custom role may hold them. No verb writes the system set — only
the boot reconcile does. The verbs themselves arrive with go-quicbaas#805;
the permissions are registered now so roles can be granted ahead of them.

## What stays in Go

The design's rule is that loops, plurals, fenced logs, redaction, renumbered
comments and every JSON assembly stay in Go as thin assemblers. Applied, the
line is: **a prompt is text a reader would recognise as a message; anything
that only exists to splice a value into one is the assembler's.** Concretely,
these deliberately did not become prompts:

| what | where | why |
|---|---|---|
| `outcomeSpecs`' per-kind `headline` and `meaning` | `pkg/sdlc/outcome.go` | row data of the closed conclusion vocabulary, beside that row's label key and resumability — the same reason `turnVerdicts[…].Headline` stays there |
| the outcome comment's field labels (`**Cause:**`, `**Attempt:**`, `**Labels:**`, …) | `pkg/sdlc/outcome.go` | labels over values; the comment's structure, not its message |
| `renderFailures`' `### name` blocks | `pkg/sdlc/runner.go` | a loop over criteria with no prose in it |
| `commentAttribution` (`— @author, timestamp`) | `pkg/sdlc/contract.go` | identifier and timestamp formatting |
| `(finished <RFC3339>)`, `, classified as …`'s comma, `(none)` | `pkg/sdlc` | sub-clause punctuation and a placeholder token |
| `PASSED`/`FAILED`, `required check`/`required checks` | `pkg/orchestration`, `pkg/sdlc` | vocabulary and plural agreement the engine owes the reader |
| `backendCapabilityNames`, the transcript delimiters | `pkg/orchestration` | capability vocabulary and structural fences |
| redaction of a check's log excerpt | `pkg/sdlc/gate.go` | a scrub a prompt could be edited to skip is not a scrub |

## Verification

Byte equality is the acceptance test. `pkg/orchestration/testdata/prompts/*.golden`
and `pkg/sdlc/testdata/prompts/*.golden` were written by the builders **before**
a word moved, across the variable combinations each of them branches on — with
and without a workspace, with and without an issue body, a discussion whose
blank comments still renumber, one failed check against several, a redacted
log, a park with no evidence, each remedy in its precedence order. After the
extraction every one of them is byte-identical. No agent is told anything
different.

```
go test ./pkg/orchestration/prompts/                              # 100% of statements
go test ./pkg/orchestration/ -run 'Prompt'                        # workflow set + goldens
go test ./pkg/sdlc/ -run 'Prompt|WorkerCharter'                   # sdlc set + goldens + marker coupling
```

Regenerating a golden is `-update-prompt-golden`, and doing so is the explicit
admission that an edit changed what an agent reads.

## Offloadable work: what we are still paying a model to do

The rule: work that traditional programming can do is done by QuicBaaS, not
paid for in agent tokens.

Extracting the prompts made every one of these readable in one place for the
first time. **Nothing here was changed by the extraction** — the catalogue is
the output, not an action — and each row is a candidate to move from tokens
into Go.

| # | the prompt asks an agent to | where | what code could do instead |
|---|---|---|---|
| 1 | "Do NOT just describe code… Actually create the files on disk" | `workflow/worker_prompt` | the engine already measures whether the tree moved (`workerTouchedTree`, `ClassifyTurn`). The plea is paid for on every prompt; the measurement is what actually decides. Shrink the plea to a sentence and let the classification report it. |
| 2 | "All files MUST be written using absolute paths under this directory" | `workflow/worker_workspace` | the sandbox already bind-mounts the workspace; a working directory and a mount policy enforce this without asking. |
| 3 | "Verify your work compiles/runs by using the Bash tool" | `workflow/worker_prompt` | the binding's verification commands run in the same container immediately afterwards. The work is paid for twice. |
| 4 | "Respond with JSON: {…}", "ONLY valid JSON", "Do not wrap the JSON in prose or code fences" | `workflow/{reviewer_prompt,reviewer_system,planner_system,validation_prompt,decomposition_prompt}` | every backend with structured output or tool calling takes a schema. Keep the prose only for backends that have neither, and stop paying for a tolerant JSON parser's existence. |
| 5 | "Assign each work item to the most appropriate backend" + the whole backend/capability listing | `workflow/{decomposition_backends,decomposition_backend,capabilities_context,capabilities_agent}` | placement is scheduling: the engine already matches capabilities, accounts, GPU affinity and host affinity better than a model can. Ask only for `required_capabilities` and place the work in Go. |
| 6 | "reply with READY and nothing else" | `workflow/resume_priming` | a whole turn spent on an acknowledgement. The spawn's init record already proves the process is alive. |
| 7 | "Initialize session. Respond with OK." | `workflow/handshake` | a paid round trip whose only purpose is to make a CLI emit its `init` record. A CLI flag, or a protocol that emits init without input, removes it. |
| 8 | "Your branch is already checked out in the workspace directory you were given. Work there. Never commit to the trunk and never force-push." | `sdlc/worker_charter` | the checkout, the branch and the worktree are already the factory's (`NewWorkspaceSpecFiles`, `WorkspaceManager.ProvisionSession`); trunk protection and force-push refusal are a refspec and a forge setting, not a request. |
| 9 | "Verify locally. The project's own build and test commands must pass, and gofmt must be clean on everything you touched." | `sdlc/worker_charter` | the runner runs the binding's verification commands and records them. `gofmt -l` on the diff is three lines of Go; asking a model to remember it costs tokens every attempt and fails silently. |
| 10 | "Review your own diff against the conventions before you finish" | `sdlc/worker_charter` | the conventions that are mechanical — typed errors, no `time.Sleep`, no `_` discards, gofmt — are already lint rules. Run them and hand back findings instead of asking for self-review. |
| 11 | "End every working turn with a short summary of what changed and why" | `sdlc/worker_charter` | the diff, the command results and the turn record already say what changed. The *why* is the only part a model is needed for. |
| 12 | "The Software Factory handles what comes after… You do not poll for reviews and you do not merge." | `sdlc/worker_charter` | a paragraph that exists solely to stop an agent doing work already automated. It is evidence the automation is right; the cost is the tokens spent saying so every attempt. Tool-level denial is cheaper than instruction. |
| 13 | "push to the same branch" | `sdlc/ci_red` | the publisher pushes. Telling the worker to push invites a second, unsigned path to the forge. |
| 14 | "(no findings were written; ask the reviewer)" | `sdlc/changes_requested_no_findings` | the forge's inline review comments are an API call. Fetch them rather than asking an agent to ask a person. |
| 15 | the four human remedies on an outcome comment | `sdlc/outcome_remedy_*` | `outcome_remedy_self_clearing` already exists because the sweep can check that condition itself. Every condition made machine-checkable moves a row from "ask a person" to "the factory did it". |

Two rules bear on the same principle and belong in the `sdlc` set: an agent may work cross-project
**only when it is blocked**, and it **owns the issue until it is merged** so it
can resume after unblocking; and **all work happens in a dedicated branch and
worktree**, always. A blocked-on-dependency prompt is a member of this set;
creating the branch and the worktree, and filing the cross-project issue, are
rows 8 and 15 of the table above — deterministic work the factory should do.

## The editing surface

Every transport serves the prompt verbs — REST, gRPC (and Unix), QUIC, MCP
and the embedded in-process transport — through the Go SDK's `Prompts()`
client, and `quicbaas prompts` edits them; see [api.md](api.md#prompt-sets)
and [cli.md](cli.md#quicbaas-prompts). Still to come: the TypeScript client
and the GUI (#806), and the per-role charters (#807). Until then no GUI screen
edits a record.

### Verbs

Every verb is served on every transport. A request addresses its layer by
`tenant_id`, `scope=tenant|project|system` and `project_id`; `system` is
read-only.

| verb | REST | gRPC (`quicbaas.prompt.v1.PromptService`) | MCP | SDK `Prompts()` / CLI `quicbaas prompts` | permission |
|---|---|---|---|---|---|
| List | `GET /api/v1/prompts/{set}` | `ListPrompts` | `prompt_list` | `List` / `list <set>` | `prompts:read` |
| Get (`?version=N`) | `GET /api/v1/prompts/{set}/{name}` | `GetPrompt` | `prompt_get` | `Get` / `get` | `prompts:read` |
| Effective, with its chain | `GET …/{name}/effective` | `GetEffectivePrompt` | `prompt_effective` | `Effective` / `effective` | `prompts:read` |
| Diff against the parent | `GET …/{name}/diff` | `DiffPrompt` | `prompt_diff` | `Diff` / `diff` | `prompts:read` |
| History | `GET …/{name}/history` | `GetPromptHistory` | `prompt_history` | `History` / `history` | `prompts:history` |
| Put (compare-and-swap on `expected_version`, note required) | `PUT …/{name}` | `PutPrompt` | `prompt_put` | `Put` / `put -f FILE --note` | `prompts:write` |
| Rollback | `POST …/{name}/rollback` | `RollbackPrompt` | `prompt_rollback` | `Rollback` / `rollback --version N` | `prompts:write` |
| Reset to the parent | `DELETE …/{name}` | `ResetPrompt` | `prompt_reset` | `Reset` / `reset` | `prompts:write` |

Status mapping: not found is `404` / `NotFound`; a version conflict is `409` /
`Aborted`; a missing marker, an unknown set or name, or a system-scope write is
`400` / `InvalidArgument`, with the marker named in the body; a node with no
store is `503` / `Unavailable`. `Get` is `404` when nothing is overridden at
that scope; `Effective` always answers with its `origin` and the chain. QUIC
serves the REST routes over HTTP/3 and Unix serves gRPC over a domain socket.

### GUI (#806, not built yet)

A standalone page under SOFTWARE FACTORY, `#/prompts?set=&project=`:

- a scope picker (tenant or project) and one tab per set;
- a list showing each prompt's name, whether it is overridden here or
  inherited from the tenant or the system, and its effective version and hash;
- an editor that shows the placeholders and required markers, flags a missing
  marker before saving (the server stays authoritative), requires a note and
  saves with compare-and-swap, naming who changed the record on a `409` and the
  marker on a `400`;
- an inherited prompt shown read-only, with "Override here";
- panels for the history (view a version, roll back with a note), the diff
  against the parent and the effective body with its chain;
- a `403` shows an empty state naming the permission and hides the write verbs;
  a `503` says the prompt store is not running on this node.

A job's detail view lists the prompts it ran with, as `set/name · scope · vN ·
hash`, each linking to the page.

### Role charters (#807, not built yet)

The per-role charters of a workflow come from the `workflow` set
(`charter_lead`, `charter_worker`, `charter_reviewer`, `charter_validator`).
`AgentRoleSpec.SystemPrompt` is removed: it is an unversioned, unvalidated,
unpermissioned way around the store, and the workflow loader refuses a
`system_prompt` key with a typed error naming the store.
