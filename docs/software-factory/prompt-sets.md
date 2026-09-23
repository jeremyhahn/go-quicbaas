# Prompt sets

> **Status: NORMATIVE for the extraction; REFERENCE for the store** ·
> Last verified: 2026-09-13
> Every instruction the Software Factory sends an agent is now markdown
> embedded in the binary, declared, validated and rendered by
> `pkg/orchestration/prompts` — that part is built and is described here as
> built. The tenant-scoped store, the transports, the GUI and the per-role
> charters are not: they are go-quicbaas#804 to #807, specified in
> [../design/prompt-sets.md](../design/prompt-sets.md).

## What exists

Two **sets** of human-editable markdown, each owned by the package that sends
it, over one generic prompt package.

| set | owner | shipped content | sent to |
|---|---|---|---|
| `workflow` | `pkg/orchestration` | `pkg/orchestration/promptdefaults/workflow/*.md` | the lead, worker, reviewer and validator of a generic agent workflow |
| `sdlc` | `pkg/sdlc` | `pkg/sdlc/promptdefaults/sdlc/*.md` | the worker driving one forge issue, and the person watching that issue |

**The embedded files are the system tenant's set.** That is what the
`promptdefaults/` directory name says and what each directory's `README.md`
repeats: they are compiled in, they are what #804 will write into the system
tenant's records, and they are what every new tenant is seeded from and
inherits until it overrides something. Nothing reads a file at runtime.

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
registration and (from #804) at save. The rules:

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

Typed errors, all with sentinels for `errors.Is`: `InvalidSpecError`,
`SetAlreadyRegisteredError`, `UnknownSetError`, `UnknownPromptError`,
`BlankPromptError`, `UnknownPlaceholderError`, `MissingVariableError`,
`UnknownVariableError`, `MissingMarkerError`, `DefaultsUnreadableError`.

`Set.MustRender` panics. It is for assemblers on a path with no error to
return, and the failure it panics on — an assembler naming a variable the
prompt does not declare, or withholding a required one — is a defect in this
binary that the golden tests catch before it ships, not a state a running node
can reach. **#804 must revisit this**: once content comes from a tenant's
store rather than from the binary, the resolver validates on save and at
resolution, and a render failure becomes a launch refusal rather than a panic.

**One prompt sits outside this machinery, and must be folded into it.** The
triage charter is human-editable markdown embedded with its own `go:embed` —
`pkg/sdlc/prompts/triage.md`, served by `sdlc.TriageCharter()` — rather than a
registered spec, because it was written while this work was in flight.
`pkg/sdlc` therefore has two prompt mechanisms where it should have one, and
**that duplication is tracked as #883**: the charter moves into the `sdlc` set
and the private `embed.FS` is deleted. It was deliberately built as one charter
behind one accessor so the migration is a rename rather than a rewrite. See
[triage.md](triage.md).

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

The owner's standing principle: *"we want as much offloaded to quicbaas as
possible so that we dont need to waste tokens having agents do basic work that
can be automated with quicbaas using traditional programming."*

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

Two rulings of 2026-09-10 bear on the same principle and have a natural home
in the `sdlc` set, to be authored separately: an agent may work cross-project
**only when it is blocked**, and it **owns the issue until it is merged** so it
can resume after unblocking; and **all work happens in a dedicated branch and
worktree**, always. A blocked-on-dependency prompt is a member of this set;
creating the branch and the worktree, and filing the cross-project issue, are
rows 8 and 15 of the table above — deterministic work the factory should do.

## Not built yet

The store, inheritance, history, rollback, the `prompts:read|write|history`
permission family, resolution at job launch, the job's recorded version refs,
the six transports, the CLI, the GUI and the per-role charters are
go-quicbaas#804 to #807. Until they land:

- [cli.md](cli.md#quicbaas-prompts) — `quicbaas prompts` does not exist.
- [api.md](api.md) — there are no prompt routes, RPCs or MCP tools.
- [user.md](user.md#tune-the-prompts) — nothing to tune from a running node;
  editing a prompt is editing a file and rebuilding.
