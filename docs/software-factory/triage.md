# Issue triage — which issues are ready to be worked

> **Status: NORMATIVE** · Last verified: 2026-09-12
> The front half of the factory loop: the capability that decides which
> issues are ready to be worked, and marks them. Conflicts with this document
> are defects.

The `sdlc` runner turns a **triggered** issue into a merged pull request.
Triage decides which issues get triggered. Before it, an issue entered the
factory only when a person applied the trigger label by hand.

## Why it exists

Two failures from live runs, neither of them hypothetical.

**Duplicates nothing detected.** On `go-truststrap`, six open issues described
the same stale README — #25, #52, #53, #54, #55, #56, four with byte-identical
titles — every one filed by the factory's own acceptance runs. A worker
eventually picked one up, proved with file-and-line evidence that the work was
already on `main`, and parked. The other five were queued to waste a full
worker run each.

**Owner rulings found too late.** Of three issues handed to the factory, two
came back needing an owner ruling before any code could be written — discovered
only after a worker had spent a run reading the repository.

Both are cheap reads. A worker run plus a review round is not.

## The shape: a third JobRunner

Triage is a **judgement**, so it needs a session, a prompt, a budget, a
recorded verdict and an audit trail. That is exactly the list the engine's job
envelope provides, so triage is a third `JobRunner` registered beside
`workflow` and `sdlc` — not a sweep, not a cron, not a shell script. See
[job-runners.md](job-runners.md) for the seam.

| | |
|---|---|
| Name | `triage` (`sdlc.TriageRunnerName`) |
| Source | `pkg/sdlc/triage_runner.go` |
| Registered by | `sdlcTriageForwarder` in `pkg/server/lifecycle_sdlc.go` |
| Scope | one job per **project** (never per task) |
| States | `initializing → executing → cleanup` |
| Sessions | exactly one (`sdlc.TriageSessionCount`), for however many issues it judges |

`Validate` refuses a job with no `ProjectID`, and refuses one that names a
`TaskID`: a triage job is not an attempt at a task, it decides which issues
become tasks at all.

## The three verdicts

The only state triage changes is **labels**.

| Verdict | What it does |
|---|---|
| **ready** | applies the binding's `TriggerLabel`, and clears a `needs_info` label an earlier pass left. No comment — the label is the message. |
| **needs-info** | comments with exactly what is missing, then applies the binding's `needs_info` label. The comment goes **first**: if only one write lands it must be the one carrying the question. |
| **not-actionable** | comments with the evidence and leaves the issue alone. No label. |

An issue is **ready** when it states a verifiable objective, is scoped to one
pull request, has no unresolved dependency on another open issue, is not a
duplicate, is not already done, and the binding's verification commands apply
to it.

### Triage never closes an issue

A label is reversible and carries no social weight; closing somebody's issue
does. The guarantee is **structural as well as stated**: `ForgeBackend` has no
close verb at all, so there is no API for triage to call. The charter forbids
even proposing one, and `TestTriageRunner_NeverClosesAnIssue` asserts the
property directly so it fails loudly the day somebody adds one and wires it in.

## The verdict is read from structure, never from prose

The same rule, and the same shape, as the worker stop declaration in
`pkg/sdlc/turn.go`: **the factory does not grep prose for a decision that costs
something.** The agent ends its reply with a marker line and a code from a
closed set, and nothing else is read.

```
SDLC-TRIAGE: ready | needs-info | not-actionable | duplicate
```

A duplicate must also name the issue it duplicates, on its own line above the
verdict:

```
SDLC-TRIAGE-DUPLICATE: #25
SDLC-TRIAGE: duplicate
```

`ParseTriageDeclaration` finds the **last** declaration of each marker rather
than assuming the final line, and tolerates the decoration a model puts around
it (quoting, emphasis, code fences) — so a reply that quotes the marker while
thinking aloud and then declares for real is read on its real declaration.

### The safe direction is "not ready"

Every way of failing to understand the agent lands on a disposition that leaves
the issue for a person:

| Reply | Outcome | Disposition |
|---|---|---|
| `SDLC-TRIAGE: ready` | `ready` | **ready** |
| `SDLC-TRIAGE: needs-info` | `needs_info` | needs-info |
| `SDLC-TRIAGE: not-actionable` | `not_actionable` | not-actionable |
| duplicate **with** a reference | `duplicate` | not-actionable |
| duplicate with **no** reference | `unreadable` | not-actionable |
| a code outside the vocabulary | `unreadable` | not-actionable |
| no declaration at all | `undeclared` | not-actionable |

Wrongly withholding the trigger label costs a human glance. Wrongly applying it
costs a worker run, a review round, and an issue the factory churns on. The
table prices them accordingly, and
`TestClassifyTriage_OnlyReadyIsEverReady` asserts the property over the whole
table rather than case by case.

## The two checks that earn their keep

**Already done.** The agent is given a read-only checkout of the project's
default branch and is told to read it before anything else. An issue whose work
is already present is `not-actionable` with file-and-line evidence — the
six-duplicate case above, caught for the cost of a read.

**Duplicate.** The prompt carries every **other open** issue in the project by
number and title. The issue under triage is excluded from its own candidate
list (an issue offered to itself is the one duplicate verdict that is always
wrong) and closed issues are excluded (work already closed is not work this
issue duplicates). Both filters live in `writeTriageCandidates`, so a reader of
the rendered prompt can see they held.

## Which issues are candidates

An open issue is judged when it carries **none** of the module's owned labels —
neither the trigger nor any progress label. An issue already moving has had a
position taken on it, and re-triaging would fight the runner that owns it.
`needs_info` is not one of those: that verdict is a **question**, and the point
of asking is that somebody answers.

Labels are not the whole rule, because **two of the three verdicts apply no
label**. not-actionable and duplicate comment and leave the issue alone by
design, so a label-only rule would see a bare open issue on the next pass,
spend a model turn on it, and post the same verdict again — one turn and one
duplicate comment per issue per sweep, for ever, on every repository in the
fleet. That is the pathology this runner exists to remove.

The record of a verdict is therefore triage's own **comment**, tagged
`quicbaas-sdlc:triage`, and `spokenToSinceTriage` asks one question of the
thread: **who spoke last?**

- **nobody has commented** → judged. Triage has never spoken, so there is
  nothing to wait on.
- **triage's own comment is newest** → left alone. The verdict stands and the
  periodic pass costs nothing.
- **somebody else's comment is newest** → judged again. A new comment is new
  information, whether it answers a needs-info question or disputes a
  not-actionable verdict — and the answer is noticed without a person
  re-labelling by hand.

It is deliberately **not** a comparison against the issue's `Updated`
timestamp. needs-info writes a comment and then a **label**, which advances
`Updated` past the comment triage had just posted: a timestamp rule reads
triage's own second write as the reporter's answer and re-asks the question on
the very next pass. Who spoke last cannot be moved by triage's own labelling.

The cost of the narrowing is a body-only edit with no comment, which is not
noticed until somebody comments. That is the right way round: a missed
re-triage costs one human nudge, and the alternative costs a model turn and a
duplicate comment on every issue in the fleet every sweep.

The thread is read **once** per issue. The candidate filter needs it and so
does the prompt, so the comments the filter read are the comments the prompt
renders — two reads would be two forge calls per issue and two views of one
thread a concurrent comment can make disagree.

## Bounding: one per project, and off the worker budget

Triage is frequent, small and latency-sensitive, and it must never queue behind
a code review. Both halves are enforced by the **lease**, through an optional
runner seam added for this:

```go
// pkg/orchestration/job_runner.go
type JobLeasePolicy interface {
    LeaseSubject(cfg *JobConfig) string  // what the job is mutually exclusive with
    OffProjectBudget() bool              // does it spend Quotas.MaxConcurrency
}
```

**One per project.** `TriageLeaseSubject(projectID)` is `"triage:<projectID>"`,
and every triage job of a project names it. Two jobs naming one subject
contend, and `jobLeaseAdmits` refuses the loser with `ErrJobLeaseTaken`. The
bound therefore survives a restart and holds across nodes — a scheduler-side
"is one running" flag would be a second, weaker answer that disagrees with the
first the moment either happens. Without the seam a job bound to no task falls
back to its own job id, which serializes with nothing.

**Off the worker budget.** `Quotas.MaxConcurrency` bounds the **workers** a
project runs; triage reads the forge and writes a label, and is not that work.
`JobLease.OffBudget` is set from the policy when the lease is written, and is
honoured in both directions at all three places that read it:

| Reader | Behaviour |
|---|---|
| `projectCapacityJobLeaseStore.admit` | an off-budget lease is admitted without reading the quota; an off-budget holder is not counted against a worker |
| `CountLiveJobLeases` (task dispatcher) | skips off-budget leases |
| `CountLiveJobLeases` (task-source poller) | skips off-budget leases |

All three must agree: a budget honoured in one and ignored in another admits a
job that then cannot run, or refuses one the store was ready to take. The flag
is on the **record** rather than re-derived from the runner at read time,
because the dispatcher and the poller see leases, not runners.

A project at `max_concurrency: 1` therefore triages while its one worker runs.
`TestProjectCapacity_AdmitsAnOffBudgetLeaseAtTheLimit` is that requirement in
one test.

## What wakes it

**The forge webhook**, for the deliveries that can change a verdict:

| Event | Wakes triage |
|---|---|
| `issue_opened` | yes |
| `issue_edited` | yes |
| `issue_labeled` | yes |
| `issue_closed` | no — nothing to conclude, nobody waiting |
| pull-request, review, check events | no — the delivery half of the loop |

`EventIssueLabeled` is new: the Gitea adapter previously mapped `label_updated`
and `label_cleared` to `EventUnsupported`, so a person clearing `needs_info`
woke nothing at all.

A verified delivery nudges **both** the task-source poller and the triage
scheduler. The two answer different questions — the poller asks "has a
*triggered* issue appeared", triage asks "is any *untriaged* issue now ready" —
and they fail independently. A triage nudge that fails does **not** fail the
delivery: re-verifying and re-nudging the poller to reach a pass the backstop
will make anyway is a poor trade.

**The periodic backstop** (`TriageScheduler.Sweep`, default 15 minutes,
`sdlc.triage_sweep_interval`) visits every bound project. It catches the
deliveries that never arrived — an unprovisioned hook, a forge outage, a daemon
restarting through the delivery. It runs **at boot as well as on the tick**,
because the deliveries a node missed are exactly the ones that arrived while it
was down.

It shares one goroutine with the **review floor** and the **unblock sweep**,
the backstops for the back half of the loop
(`pkg/server/lifecycle_sdlc_backstop.go`, `docs/software-factory/review.md`,
`docs/software-factory/admission-and-readiness.md`); each keeps its own
ticker, because a triage pass starts a job per bound project where a review
pass only reads the forge and an unblock pass only re-examines parked tasks.

It is cheap by construction rather than by rate limiting: a triage job for a
project with nothing to judge lists its issues, finds no candidate, and
finishes **without spawning a session**. A pass over a quiet fleet costs one
forge listing per project.

## Failure handling

**One issue's failure is not the sweep's.** A forge write that fails, a
discussion that cannot be read, a prompt that cannot be rendered — each is
audited against its issue (`sdlc:triage_failed`) and the run moves on. The
sweep is the only path the other issues have, and losing all of them to one bad
record is the failure mode triage exists to remove, not to reproduce.

**The sweep's one fatal failure** is not being able to enumerate the tenants:
it then does not know what it was meant to visit, and reporting a clean pass
would be a lie. A tenant whose bindings cannot be listed, or a project whose
job the engine refuses, is a deferred failure drained by `SweepFailures` and
logged once.

Every verdict is written to the job's audit trail as `sdlc:triage_verdict` with
the issue, outcome, disposition, duplicate reference and reply size, so an
operator can say what triage concluded without re-reading the forge.

## Where the prompt lives

`pkg/sdlc/prompts/triage.md`, embedded with `go:embed` and served by
`TriageCharter()`.

**It is editable markdown, not a Go constant.** A triage agent's judgement is
tuned by editing prose, and prose in a string literal is prose nobody edits: it
has to be re-escaped, it cannot be diffed as text, and every wording change
reads as a code change in review. `TestTriageCharter_IsNotAGoConstant` reads
the embedded file and compares, so the two cannot drift.

**This is a temporary home, and the follow-up is named.** The owner's design
calls for a generic prompt-set package with `system → tenant → project`
inheritance, so a project can tune its own definition of "ready" —
[prompt-sets.md](prompt-sets.md) and [../design/prompt-sets.md](../design/prompt-sets.md).

When this landed, nothing under `pkg/orchestration` or `pkg/sdlc` implemented
it. **#803 (scope 1 of 4) landed it in parallel, the same day**, bringing
`pkg/orchestration/prompts` plus a per-owner set at
`pkg/sdlc/promptdefaults/sdlc` with a spec registry, validator, renderer and
golden tests.

So this charter **must move into that set** rather than remain a second way of
embedding a prompt in one package — tracked as **#883**, blocked on #803
merging. It is deliberately not registered there yet: the two changes were in
flight simultaneously, and coupling a working capability to an unmerged
four-part refactor would block it on churn it does not depend on. The shape
here was chosen to make that migration a rename — one charter, one accessor,
no inheritance mechanism of its own to unpick.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `sdlc.triage_sweep_interval` | `15m` | how often the backstop visits every bound project |
| `sdlc.review_sweep_interval` | `5m` | how often the REVIEW floor visits every bound project — the same goroutine, its own ticker (see `docs/software-factory/review.md`) |
| `sdlc.unblock_sweep_interval` | `10m` | how often the UNBLOCK sweep re-examines every blocked task — the same goroutine, its own ticker (see `docs/software-factory/admission-and-readiness.md`) |
| `sdlc.issue_page_limit` | `50` | also bounds one triage job's issue listing |
| `sdlc.checkout_root` | `/workspace` | where the read-only checkout is mounted in the container |

Triage reads the trigger label, the `needs_info` display label, the worker
backend/model/image and the verification commands from the project's existing
`ProjectBinding`. It adds no configuration of its own to the binding.

## How the checkout authenticates

Triage clones whatever transport the project's `git.repo_url` names, so it needs
whatever that transport is authenticated with: an https remote takes the forge
token from the mounted token file, and an **ssh remote takes a client key and
nothing else**. It gets both through `forgeAccess` (`pkg/sdlc/workspace.go`), the
one seam the delivery runner and the reviewer also use.

Triage asks for `SigningScopeCheckout`. The agent serves the project's **access
key** — `ProjectGit.AccessKey`, which is `git.ssh_key` or the signing key
standing in for it — and **no commit signing key**, so nothing in the container
can produce a commit signature under the project's provenance identity. The
agent is then **stopped as soon as the checkout is made**, before the first
prompt: nothing triage does afterwards reaches the forge over git (the verdicts
are forge API calls the daemon makes), so the socket the container holds for the
rest of its life answers nothing. A project whose access key *is* its signing
key narrows that window rather than removing it; configure a distinct
`git.ssh_key` to remove it.

An https-only project gets **no agent at all**, which is the strongest posture
available and the one this path had before — it was also the reason an ssh
project could not be triaged: every sweep spawned a container, cloned, collected
`Permission denied (publickey)` and concluded (go-quicbaas#929).

A spawn that holds no credential the remote would accept is **refused on the
daemon**, before any container starts, naming the repository and what is
missing (`orchestration.RequireCheckoutCredentials`).

## What triage does not get

- **No commit signing key**, per the scope above.
- **The runner drives the clone.** The daemon makes no checkout and the image's
  entrypoint is cleared at spawn, so the runner execs the clone inside the
  container through the same seam the delivery and review runners use. Without
  it the charter's "a read-only checkout ... Read it." would be a promise
  nothing kept, and the already-done check — the first question triage is asked
  — would be a guess. The checkout is then **proved** by the same pre-flight
  the delivery runner and the reviewer run, so a mount holding another
  repository is a named refusal rather than a queue judged against the wrong
  tree.
- **The session must be containerized.** A spawn that returns no container id
  is refused before the first prompt: anybody who can open an issue writes the
  body triage is about to read, so it is untrusted input and never read on the
  daemon. Same refusal the reviewer makes about a candidate change.
- **No branch.** The checkout is the default branch, read-only.
- **No task.** A triage job is bound to no task and spends no attempt budget.

## Testing

```
make test-triage      # verdict, prompt, runner, scheduler, wiring, lease bounding
make test-sdlc        # the whole module
make coverage-triage  # per-file coverage of the triage sources
```

Each verdict is driven **end to end** through the real declaration parser and
the real verdict table — the doubles stand in for the agent transport and the
forge API, never for the judgement being tested. The already-done case, the
duplicate case, "triage never closes an issue" and "a project at its worker
concurrency limit can still triage" each have a test of their own.
