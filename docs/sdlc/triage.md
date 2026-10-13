# Issue triage — which issues are ready to be worked

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Supplement to the [SDLC feature folder](architecture.md): the front half
> of the factory loop, which decides which issues are ready to be worked and
> marks them. Conflicts with this document are defects.

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
[job-runners.md](../orchestration/job-runners.md) for the seam.

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
| **ready** | applies the binding's `TriggerLabel`, and clears a `needs_info` label an earlier pass left. No comment — the label is the message. Held until the pass has judged every candidate, so related ready issues can share a task; see [clustering](#clustering--related-ready-issues-become-one-task). |
| **needs-info** | comments with exactly what is missing, then applies the binding's `needs_info` label. The comment goes **first**: if only one write lands it must be the one carrying the question. |
| **not-actionable** | comments with the evidence and leaves the issue alone. No label. |

An issue is **ready** when it states a verifiable objective, is work rather
than a tracker (a tracking checklist or epic — child issues, checkboxes over
several changes, or a body that calls itself a tracker — is always
**not-actionable**), is scoped to one pull request, has no unresolved dependency on another open issue, is not a
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

A needs-info verdict must ask at least one question its author can answer,
each on its own line above the verdict:

```
SDLC-TRIAGE-QUESTION: Which of the three `Encode` overloads is affected?
SDLC-TRIAGE: needs-info
```

Every question line is kept, in reply order, up to five, each cut to 300
characters; an empty question line asks nothing. The posted comment lists
them numbered under **Questions for the author**, so the reader sees what to
answer, and the marker lines themselves are left out of the comment.

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

### A reply that is not a verdict is not posted

The table says what a reply *classifies as*; it does not say every reply is
acted on. The runner sets a reply aside — audited as `sdlc:triage_unusable`,
written nowhere on the forge, and judged again on a later pass — when it is:

- `undeclared` or `unreadable`;
- any verdict other than `ready` whose prose, with the marker lines removed,
  is empty — the charter demands evidence, and "the evidence:" followed by
  nothing tells a person nothing;
- a `duplicate` naming the issue itself, or an issue that is not among the
  open issues the pass listed;
- a `duplicate` whose two issues name no file path or backticked identifier
  in common (`ExtractTriageProbes` over both titles and bodies; lexical, no
  model call). go-quicbaas#1082 was once called a duplicate of #894, a DICE
  SDK re-export against scheduler lease coalescing, because #894 was open;
- a `needs-info` that asks no `SDLC-TRIAGE-QUESTION:`. A summary of the
  issue under "What is missing:" gives its author nothing to answer, and it
  parked well-scoped issues (go-quicbaas#826, #851).

These are a model failing — out of rounds, or lost the marker vocabulary —
not findings about the work. Posting them used to make each one triage's last
word on the issue, which froze re-triage until a person spoke. Measured on
go-luks2 on the NPU (2026-09-21): one pass posted a misspelt self-duplicate, two
27-byte not-actionables with no evidence, and a duplicate of an unrelated issue.

**The retry is bounded.** A set-aside reply leaves the issue a candidate, so a
model that fails it the same way every pass would be asked every fifteen
minutes. The runner counts set-asides per issue and default-branch head,
skips the issue after two (`sdlc:triage_skipped`), and asks again only when the
head moves or somebody comments — the two things that change what the model is
shown. The count is per node process; a restart asks again.

What the overlap test cannot catch is a duplicate that names a real open
issue sharing a path or identifier for a wrong reason. The charter tells the model it holds the other issues by
number and title only and that a shared area is not the same work; if that
class keeps recurring on a small model, the answer is a larger one, not more
parsing.

## The two checks that earn their keep

**Already done.** The agent is given a read-only checkout of the project's
default branch and is told to read it before anything else. An issue whose work
is already present is `not-actionable` with file-and-line evidence — the
six-duplicate case above, caught for the cost of a read.

The deterministic half of that check is done by the **runner**, not the model.
Before each issue is prompted, `ExtractTriageProbes` reads the file paths and
backticked identifiers the issue names (bounded: ten paths, eight identifiers),
`MeasureTriageFacts` asks git in the session's container whether each path is
tracked (`git ls-files`) and where each identifier appears (`git grep`, three
matches), and the answers are rendered as a **Repository facts** section of the
prompt, measured at the head being judged. The charter tells the agent they are
measured, not remembered, and to cite them.

Why: a frontier model runs those commands itself; a small one does not.
Measured on the NPU (2026-09-21), qwen3-8b called the tool on a short prompt
and never once on the real ~7k-token triage prompt — `tool_uses=0` on every
turn — while writing "I have examined the code". Its verdicts were ungrounded
by construction, and no amount of asking changes that; measuring in Go does.
The facts stop at facts (this file exists, this symbol appears here); whether
the work is *done* remains the judgement. A measurement that fails is audited
(`sdlc:triage_facts_failed`) and the prompt goes out without the section.

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

**A held issue is never judged, and never written to.** An issue carrying the
portfolio hold `task:blocked` (handbook `labels.md` §3), or the binding's own
`blocked` or `escalated` label, is passed over before its thread is read, and
the skip is audited as `sdlc:triage_skipped reason=held label=<label>`. Only the party
named on the issue clears a hold, and a model's verdict is not that party
(go-quicbaas#1058: a `task:blocked` issue was judged ready, relabelled
`task:ready`, adopted, and charged an attempt the worker rightly refused). The
verdict writer re-reads the issue before its first write and refuses the whole
verdict — comment and label — when a hold has appeared since the issue was
listed, or when the re-read fails. The task source refuses a held issue too,
with readiness verdict `held`, even when it also carries the trigger.

`escalated` is a hold, not a question. It names a person as the next step, so
a new comment or a moved head does not return the issue to triage; the person
does, by removing the label. It used to be exempt like `needs_info`, which is
how a verdict could promote an issue a person had been asked to rule on.

The attempt charged on #1058 (task `b7a8eb05`) stands on the ledger. The
factory has no path to refund an attempt after it has been charged; refunds
happen only when the conclusion is applied. This fix also does not reach into
the running factory's task store. The charge cannot recur: #1058 is held, so
the task source no longer adopts it, and the task's attempt budget is never
spent on it again. If an operator wants the ledger corrected, the task record
is the place to do it, and the cause to cite is a triage fault
(go-quicbaas#1060).

Labels are not the whole rule, because **two of the three verdicts apply no
label**. not-actionable and duplicate comment and leave the issue alone by
design, so a label-only rule would see a bare open issue on the next pass,
spend a model turn on it, and post the same verdict again — one turn and one
duplicate comment per issue per sweep, for ever, on every repository in the
fleet. That is the pathology this runner exists to remove.

The record of a verdict is therefore triage's own **comment**, tagged
`quicbaas-sdlc:triage`, and `spokenToSinceTriage` asks one question of the
thread: **who spoke last?**

The tag is a hidden trailer that also records what the verdict was formed
from, so mis-triage can be counted per model and prompt:

```
<!-- quicbaas-sdlc:triage head=<sha> verdict=<outcome> model=<model> prompt=<revision> -->
```

`model` is the model the triage session reported at spawn (`unknown` when it
reported none); `prompt` is `TriagePromptRevision`, a hash prefix of the
charter and `TriagePrompt`'s text that `TestTriagePromptRevisionPinned` pins,
so editing the prompt without bumping it fails the tests. Each reader cuts
its own field out by key, so a comment posted before `model` and `prompt`
existed still reads.

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

**A merge reopens a verdict; an identical verdict is not posted again.** A
verdict records the head it was formed against, and a merge makes an answer
about the old tree stale, so the issue is judged again. When that judgement
reaches the **same verdict** — the same outcome and the same headline, which
names the duplicate target when there is one — nothing is posted: the agent's
prose around it differs every time and is not news. go-quicbaas#1013 collected
sixteen identical "duplicate of #981" comments before this, one per merge. A
different verdict, or a different duplicate target, is posted as before.

The cost of the narrowing is a body-only edit with no comment, which is not
noticed until somebody comments. That is the right way round: a missed
re-triage costs one human nudge, and the alternative costs a model turn and a
duplicate comment on every issue in the fleet every sweep.

The thread is read **once** per issue. The candidate filter needs it and so
does the prompt, so the comments the filter read are the comments the prompt
renders — two reads would be two forge calls per issue and two views of one
thread a concurrent comment can make disagree.

## Clustering — related ready issues become one task

Every pull request pays a pipeline, a review and a merge, and each one merged
makes the next stale. A backlog of small issues against one package paid all
three once per issue. So triage batches at planning time: the issues a pass
finds **ready** are admitted together, and related ones become ONE task, one
branch and one pull request that closes every one of them
(`pkg/sdlc/triage_cluster.go`, `triage_cluster_apply.go`).

### How related is decided — no model turn

Triage already measures, in its own checkout, the files an issue names and
the files its backticked identifiers are defined in (the repository facts).
Those files and the directories that hold them are the issue's **footprint**
(`TriageFootprint`): the part of the tree its change will most likely touch.
Two ready issues whose footprints share a file or a package are related. The
repository root is not a package — every top-level file sits in it — though
two issues naming the same top-level file still share that file. An issue
whose text names no code has no footprint and stays alone.

`PlanClusters` takes the ready issues in number order; each joins the first
group it overlaps that still has room, or starts its own. The lowest-numbered
issue of a group is therefore its **anchor**, every node plans the same groups
from the same input, and no chain of overlaps grows a group past `max_size` —
the issue that would starts the next group.

### What a cluster is on the forge

| Issue | Gets |
| --- | --- |
| the anchor | a comment naming its members and the shared code, then the trigger label, **last** |
| each member | a comment naming the anchor, `needs_info` cleared, the `clustered` display label (when the binding maps one) — **never** the trigger |

The order is the recovery. The cluster **record** (`IssueCluster`, filed under
every issue it covers) is written first, so the moment the trigger lands —
which is when the issue source may adopt the anchor — the anchor's contract
already carries its members. The trigger goes last, so a pass that dies half
way leaves an anchor that is not yet triggered, which the next pass re-judges
and re-clusters. A cluster whose record cannot be written admits nothing of
itself; an unrelated issue is admitted regardless.

### What a cluster is to the rest of the factory

- **The issue source** adopts only the anchor. Its task's description is the
  anchor's contract, then every OPEN member's contract verbatim
  (`issue_contract_cluster`), and its metadata carries
  `cluster_issues: "12,15"`. A member closed since triage is left out. A member
  somebody triggered by hand is not adopted on its own — its work is the
  anchor's task.
- **The delivery's** pull request body, commit message and merge message close
  every issue in the cluster; the merge train carries all of them; and when it
  lands each member is labelled with the anchor's ending.
- **Triage** does not judge a member while its anchor is open. A member still
  open after its anchor closed was left behind — the change was abandoned, or
  the anchor closed by hand — so the cluster is dissolved and the member is
  judged on its own.
- **Members never carry a lifecycle label.** A member claiming `in_progress`
  with no task of its own would read to the label reconciliation sweep as a
  lost hand-over.

### Configuration

`clustering` on the binding, **on by default**:

```json
"clustering": {"disabled": false, "max_size": 4}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `disabled` | `false` | every ready issue becomes its own task |
| `max_size` | `0` → 4 | issues per task, the anchor included, at most 10 |

Four, because a cluster is still one change a reviewer must hold in their head
and one change a red pipeline sends back whole.

Audit: `sdlc:triage_clustered` (anchor, members, shared keys) and
`sdlc:triage_cluster_dissolved`.

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
(`pkg/server/lifecycle_sdlc_backstop.go`, `docs/sdlc/review.md`,
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
the issue, outcome, disposition, duplicate reference, reply size, the number
of tool calls the turn made and its stop reason, so an operator can say what
triage concluded without re-reading the forge. The last two are what tell a
judgement from a give-up: an OpenVINO turn reports each command it ran as a
`tool_use` block, and an answer forced on its final, tool-less round carries
`stop_reason=tool_budget_spent` rather than `end_turn`.

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
[prompt-sets.md](../software-factory/prompt-sets.md) and [../design/prompt-sets.md](../design/prompt-sets.md).

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
| `sdlc.review_sweep_interval` | `5m` | how often the REVIEW floor visits every bound project — the same goroutine, its own ticker (see `docs/sdlc/review.md`) |
| `sdlc.unblock_sweep_interval` | `10m` | how often the UNBLOCK sweep re-examines every blocked task — the same goroutine, its own ticker (see `docs/software-factory/admission-and-readiness.md`) |
| `sdlc.train_sweep_interval` | `5m` | how often the MERGE TRAIN backstop visits every trained project — the same goroutine, its own ticker (see `docs/sdlc/merge-train.md`) |
| `sdlc.issue_page_limit` | `50` | also bounds one triage job's issue listing |
| `sdlc.checkout_root` | `/workspace` | where the read-only checkout is mounted in the container |

Triage reads the trigger label, the `needs_info` and `clustered` display
labels, the worker backend/model/image, the verification commands and the
`clustering` policy from the project's existing `ProjectBinding`.

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
