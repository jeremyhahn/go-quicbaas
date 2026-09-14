# Job runners — one envelope, many shapes

> **Status: NORMATIVE** · Last verified: 2026-09-10
> Supplement to the Software Factory feature folder: the `JobRunner` seam —
> one job envelope, many job shapes. Conflicts with this document are defects.

A job has ONE envelope and MANY shapes. The envelope — the lease, the
heartbeat, the attempt accounting, the reaper, the events, the sessions and
the recovery — is the engine's, and it is identical for every job. The
*shape* of a job — the sequence of states it moves through and what happens
in each — is a **`JobRunner`**'s.

This corrects what this documentation used to say. "A delivery pipeline is a
Workflow" was false: the engine's one loop is a **lead agent decomposing a
description into a DAG of work items executed by workers**, `handleDecompose`
runs unconditionally, and a `canned` or `constrained` workflow is refused at
validation without a `lead` role. A live run turned one Gitea issue into nine
work items in seven waves and the worker never saw the issue text.

A delivery pipeline is a different shape — no lead, no decomposition, one
persistent worker — so it is **a different runner over the same envelope**,
not a second engine and not a Workflow.

## The seam

`pkg/orchestration/job_runner.go`:

```go
const JobRunnerWorkflow = "workflow"

type JobStateSpec struct {
    State         JobState      // as it appears on JobStatus
    Label         string        // how a consumer names it to a person
    SessionBacked bool          // an agent session is doing the work here
    IdleThreshold time.Duration // how long a job may sit here; 0 takes DefaultJobIdleThreshold
}

type JobRunner interface {
    Name() string                                       // JobConfig.Runner selects by this
    States() []JobStateSpec                             // ordered, non-terminal; [0] is the entry state
    Validate(ctx context.Context, cfg *JobConfig) error // before any goroutine or lease exists
    Run(ctx context.Context, job *JobRun) error         // nil completes the job, an error fails it
}

// Optional. A runner without it restarts an interrupted job at States()[0].
type JobResumer interface{ ResumeState(interrupted JobState) JobState }

// Optional. A runner without it takes the envelope's own answers, which are
// correct for every runner whose jobs are attempts at a task.
type JobLeasePolicy interface {
    LeaseSubject(cfg *JobConfig) string // what the lease serializes on; "" takes the envelope's
    OffProjectBudget() bool             // this job does not spend a Quotas.MaxConcurrency slot
}
```

`JobLeasePolicy` declares the two lease facts that belong to the **runner**
and were previously only derivable for task-bound work.

`LeaseSubject` is what the job is mutually exclusive *with*. The envelope
names a task-bound job's lease after the task's source reference, so two
attempts at one issue contend; a job bound to no task falls back to its own
job id, which contends with nothing. That is right for a one-off and wrong
for a runner whose jobs are per-subject singletons — there it means every
duplicate trigger runs a second job that does the same work and writes a
competing answer. The `review` runner answers `review:<projectID>:#<number>`;
the `triage` runner answers `triage:<projectID>`, because a project's triage
pass is about the project and not about one issue — see
[triage.md](triage.md).

`OffProjectBudget` says the job does not spend a worker slot. It is carried
on the record as `JobLease.OffBudget`, because the budget has three readers —
the capacity store's own admission check and the two callers of
`CountLiveJobLeases` — and a node counting another node's lease has only the
record to read. All three answer through `JobLease.CountsAgainstBudget`, so
they cannot disagree: a budget honoured in one place and ignored in another
admits a job that then cannot run.

Both are read from **the runner the envelope is about to execute**, not from
a second lookup by name: one run has one runner, and a lease named after a
different one would be a record about work nobody does.

`JobConfig.Params` is the runner's own configuration for one job — **the
subject it is about**, in the vocabulary of whichever runner drives it. It is
the missing half of this seam: every other field on `JobConfig` describes the
engine's own shape (a description for a lead to decompose, a task to attempt),
so a runner whose job is about something else had nowhere to be told what.
The `sdlc` runner got away with `TaskID` because a delivery *is* an attempt at
a task; a review of a pull request is not.

It is **opaque to the engine**, which never reads a key and never defaults
one. Keys are namespaced by their runner (`review.pull_request`), the runner
validates them in `Validate` before any goroutine or lease exists, and the
map is copied on the way in so a caller cannot mutate a running job's
configuration. It is **persisted and displayed**, so it carries no secret:
secrets stay in the `SecretsService` and travel as references.

Registry verbs: `RegisterJobRunner`, `LookupJobRunner`, `JobRunners`,
`KnownJobStates`, `ResolveJobRunnerName`, `ValidateJobRunnerName`.
Registration is append-only and refuses a nil runner, an empty name, a runner
declaring no state (`JobRunnerRegistrationError`) and a duplicate name
(`JobRunnerAlreadyRegisteredError`); a job naming a runner nothing registered
is refused with `UnknownJobRunnerError` before the job exists.

**Registration order matters.** `Server.Start` runs
`registerSDLCModule()` → `startOrchestration()` → `startSDLC()`, in that
order: every runner is in the registry before the engine reads it, and before
`recoverOrchestrationJobs` resumes a job whose runner would otherwise be
unknown. Registration needs no runtime — `RunnerStates()` is a package
function as well as a method precisely so a host can declare a runner's
states before there is anything to drive them with — and the runtime is
attached afterwards, through an atomic pointer the registered forwarder
reads.

The order is also what lets `startOrchestration`'s `SetIdleThresholds`
validate an operator's threshold against the states the registered runners
declare: `JobIdleThresholdsFrom` reads the registry, and by then every runner
is in it.

## The envelope

`pkg/orchestration/job_run.go` — `*JobRun` is the engine's handle to one
running job, and the only thing a runner is given.

Read-only: `JobID`, `TenantID`, `Config`, `Task` (loaded when
`Config.TaskID` is set — the SDLC worker reads `Task.Description` straight
off it), `Project`, `Workflow` (workflow runner only), `StartState`,
`Resumed`, `Agents`, `Logger`, `CostTracker`.

| Verb | What it does |
|---|---|
| `SetState` | the single choke point: heartbeat, snapshot and persist, emit `state_change`, fire the state-keyed lifecycle moment (a hook error is `FailureClassHook`) |
| `Emit`, `Audit` | job events and the audit trail |
| `Spawn`, `Prompt`, `Close`, `CloseAll`, `Sessions` | sessions, with `ApplyWorkerSpawnEnvironment` (`AgentConfigDir`, `AccountAuto`, job/project defaults) applied and each session tracked |
| `RunStage` | run one lifecycle moment directly: the operator's stage timeout, `JobStatus.stage` and the stage event, without the state→moment table |
| `Decorate` | the runner adds its own recoverable state to every snapshot (the SDLC runner adds its worker session; `ControlLoop` adds its session ids) |
| `Validators`, `Guard`, `Policy`, `Memory`, `Context`, `WorkDir`/`SetWorkDir` | what the runner's steps are judged and bounded by |
| `SetContainer`/`Container` | the containerized worker session the job's work happens in, and the path the CONTAINER calls its checkout. A runner that spawns one records it once; a validator judging the work then runs its command there — `JobContext.Container.Exec`, which pins that directory — instead of on the daemon. The SDLC runner's `container_command` verification is the caller |
| `Iteration`, `SetIteration`, `NextIteration` | the review round, carried across a resume |
| `UseHooks` | a runner may name its hook set — refused after the first `SetState` |
| `Escalate`, `Abort`, `Lease`, `Bound`, `Finished`, `Fault` | escalation, supersession, and what the accounting reads |

`pkg/orchestration/job_envelope.go` — **`launchJob` is the ONE start path.**
A job started by a caller and a job resumed after a restart differ in exactly
two values, the job ID and the state it enters, so they are the same call with
those two as arguments rather than two paths that drift:

1. look up the runner, mint the job ID here (the old 2-second poll for the
   loop to mint its own is gone);
2. load the Project; for the `workflow` runner only, resolve the Workflow and
   its hooks from `WorkflowSettings.Hooks` — an unregistered runner, an
   unknown workflow and an unknown hook set are all refused here, before any
   goroutine;
3. build the `JobRun` with leases, node, TTL, persistence, audit and cost
   tracking; `StartState` = `States()[0]` or `resumeStateFor`. `Policy()` and
   `Validators()` are resolved lazily on first use rather than wired here;
4. store the running job **before** the goroutine starts, then
   `run.execute(ctx, runner)`.

`execute` loads the Task (inside `acquireJobLease` — a task that cannot be
read fails the run, not the admission) and acquires the lease **before
`Prepare`**: it used to run Prepare first, and taking the lease first means
the capacity gate is enforced before anything outside the daemon has changed.
It then starts the heartbeat, sets the start state and calls `runner.Run`.

On the way out, in this order: recover a panic as `FailureClassPanic`
(`JobPanicError`), write `completed`/`failed` if the runner did not, fire
`Discard`, stop the heartbeat, release the lease (unbound jobs only — a bound
job's lease is held until `concludeAttempt` has written the Task), mark the
run finished.

## Recovery

`resumeJob` reads the runner name off the persisted `JobStatus`, looks it up
and calls `launchJob` with the existing job ID. `resumeStateFor(runner,
interrupted)` asks `JobResumer` and otherwise restarts at `States()[0]`.

**A terminal answer is not resumed, it is recorded.** `resumeJob` checks
`IsTerminalJobState` before it launches anything: a runner that answers
`failed` or `completed` gets the state persisted and no run. That is fine for
a runner whose terminal states owe nothing to anyone, and wrong for one whose
last state still has an obligation to discharge — the `sdlc` runner's
`escalating` owes the person watching the issue the reason the job stopped.
Such a runner answers with the state itself and lets the step conclude the
job, which is what `RunnerResumeState`'s `escalating → escalating` does.

**A resumed run holds no session.** Every state a runner resumes at must
therefore be one it can drive without the sessions the dead daemon spawned,
or must send the run to a state that spawns fresh ones. Two shapes appear:

- **finishable without a worker** — a forge merge, a teardown, a park
  comment. Resume there; the work is the daemon's own half and idempotent.
- **needs a worker** — a prompt, a verification command in the checkout, a
  commit. Resume at the runner's first state, which spawns one.

A state whose answer depends on the durable record rather than on the state
name — `sdlc`'s `reviewing`, which is finishable without a worker only when
the `Delivery` records the head the container pushed — cannot be decided in
`ResumeState`, whose only input is the interrupted state. It answers with the
state, and the step reads the record and diverts. See the resume table in
[job-admission.md](job-admission.md).

## Runner on the Task

`JobConfig.Runner`, `JobStatus.Runner` and `StartJobRequest.Runner` name the
runner; empty resolves to `workflow`, so every pre-existing caller is
unchanged. `TaskSourceSpec.Runner`, `CreateTaskRequest.Runner` and
**`Task.Runner`** carry it to the record, because a task requeued after a
failed attempt must be re-dispatched by the runner that first ran it. The
placement hints live beside it on `Task.Routing{Backend, HostAffinity,
GpuRequired}` — on the record, not in `Task.Metadata`, where they used to be
folded and never read back — so a requeue dispatches under the same
constraints as the first attempt. `Task.DispatchReason` says why a pending
task is still pending; see
[Autonomous projects](autonomy-and-dispatch.md).

**What it takes to dispatch depends on the runner.** The `workflow` runner
assigns work items to fleet agents and runs the project's declared workflow,
so `autoSpawnTask` refuses without a matching online agent (backend → gpu →
host affinity) and without `Project.DefaultWorkflow`. A module's runner spawns
its own sessions and owns its own state sequence, so it needs **neither**: its
job's lease is the whole capacity check.

## Bound-task mode is gone

A job that was an attempt at a Task used to map its DAG work items onto that
Task instead of creating its own. That was the workaround that let one
lead-driven loop pretend to be a delivery pipeline, and it is deleted. A
workflow job attempting a Task now creates per-item Tasks like any other
lead-driven job. `JobConfig.TaskID` keeps its **accounting** meaning — lease
subject and generation, the synchronous `concludeAttempt`, the reaper's
conclusion, stall membership, and `JobContext.TaskID`/`Attempt`.

## The registered runners

| Runner | Shape | States |
|---|---|---|
| `workflow` (`ControlLoop`, registered by the engine's `init`) | a lead agent decomposes a description into a DAG of work items executed by workers | the engine's eleven non-terminal states, unchanged (thirteen with `completed`/`failed`) |
| `sdlc` (`pkg/sdlc/runner.go`, registered by the module) | one issue, one branch, one worker; no lead, no decomposition | `initializing → executing → validating → reviewing → (re-delegating → executing \| completing) → cleanup`, plus `escalating`. Its recovery answers per state (`runnerResumeStates`); `escalating` resumes at `escalating` so the park is delivered before the job fails |
| `triage` (`pkg/sdlc/triage_runner.go`, registered by the module) | the front half of the loop: which issues are ready to be worked, and marks them. Per **project**, not per task; one session for however many issues it judges | `initializing → executing → cleanup`. See [triage.md](triage.md) |
| `review` (`pkg/sdlc/review_runner.go`, registered by the module) | one pull request, one head, one verdict; a reviewer that writes no code | `initializing → executing → completing → cleanup`. Every interrupted review resumes at `initializing`: the forge is the record, and the first step asks it whether this head already carries this reviewer's verdict |

`ControlLoop` does **not** itself implement `JobRunner` — its
`Run(ctx, *JobConfig)` would collide — so `workflowRunner{}` is the stateless
adapter: `Validate` requires a `Description`, `ResumeState` is
`resolveResumeState`, and `Run` builds a `ControlLoop` and calls `RunJob`.
`RunJob` is `RunFromState`'s dispatch loop and only that. The `Run`,
`RunFromState`, `SetJobLeasing` and `SetHooks` methods that remain on
`ControlLoop` are **test wrappers** that build a standalone `JobRun` and call
`execute`; they exist so the pre-existing suite passes unchanged and are
scheduled for removal.

## Reading the runners from a consumer

```
GET /api/v1/orchestrator/runners        permission: orchestrator:status
{"runners":[{"name":"sdlc","states":[{"state":"initializing","label":"Initializing","session_backed":false}, …]},
            {"name":"workflow","states":[…]}],
 "count":2}
```

This is **registry data, not tenant data** — the runners a daemon can drive
are the same for every caller, so no tenant is derived — but it is still gated
on the orchestrator service being present, because a node that runs no
orchestrator drives no jobs. It reads `orchestrator:status`, the permission
reading the job already requires, rather than minting a new action.

Wired on **every** transport (#606): REST, gRPC
(`quicbaas.orchestrator.v1.OrchestratorService/ListRunners`), Unix — the same
gRPC wire over a domain socket — QUIC — the REST surface over HTTP/3 — MCP
(`quicbaas_orchestrator_list_runners`) and the embedded transport, plus the Go
SDK (`client.Orchestrator().ListRunners`), the TypeScript SDK and the Wails
desktop binding. The gRPC request carries no tenant, for the same reason the
REST route derives none.

`JobStateMachine.svelte` builds its stepper from the states declared by the
runner named on `status.runner` (absent → `workflow`) plus
`completed`/`failed`, instead of the hardcoded copy it used to keep.

The stall detector reads the registry too (#621). `DefaultJobIdleThresholds()`
and the detector's session-backed set are derived from `KnownJobStates()`, so
`JobStateSpec.IdleThreshold` and `.SessionBacked` are what the detector acts
on and the engine's bounds live on `workflowRunner.States()` — the runner that
waits is the one that says how long the wait may be. `WatchedJobStates()`,
which a configuration validator reads, is the same derived set.

**A state two runners share has one envelope-wide answer**, because the
detector reads `JobStatus.State`. `mergeJobStateSpec` folds the declarations:
`SessionBacked` is a disjunction (a state any runner drives with a live session
is session-backed, so a stalled job there is still ended), `IdleThreshold`
takes the longest declared bound (a shorter one would judge another runner's
job late by a clock that is not its), and `Label` takes the first non-empty in
registry order. A state no runner bounds takes `DefaultJobIdleThreshold`.

**The disjunction is a safety net, not a substitute for declaring.** A runner
that leaves `SessionBacked` off a state it drives with a live session gets the
right effective answer only for as long as some other runner declares the same
state, and loses it silently the day that runner changes. `sdlc` held its
worker through `reviewing` while declaring the state not session-backed for
exactly that reason (go-quicbaas#668); each runner's own `States()` is now the
thing its tests assert on.

**One copy remains, and it is not the engine's.**
`config.OrchestrationWatchedJobStates` restates the vocabulary for the early
refusal in `ValidateOrchestration`, because `pkg/config` cannot read
`WatchedJobStates()`: `pkg/orchestration` imports `pkg/interfaces` which
imports `pkg/config`, so the reverse edge is a production import cycle. The
two are held in step by `TestJobIdleThresholdsFrom_RefusesAnUnknownState`,
which fails on drift. Closing it needs a seam — either the state vocabulary
moves to a leaf package both import, or the config-side check is dropped in
favour of `JobIdleThresholdsFrom`, which already refuses an unknown name at
wiring time.

## The job page is built by runner

A job record is read by one page, and until go-quicbaas#796 that page was the
workflow runner's for every runner. An `sdlc` job — one attempt at one issue,
in one container, with no lead and no DAG — rendered `Workflow —`, `Agent —`,
`Iteration Progress 0/10` and `Tasks 0%`, and rendered none of the things it
does have: which attempt it was, which daemon ran it, its stage timeline, its
pull request, its review rounds, or the typed reason it stopped.

### What the record carries

`JobStatus` grew a provenance block (`pkg/orchestration/job_provenance.go`).
The fields come from three different places and the difference is the design:

| Field | Source | Written |
|---|---|---|
| `agent_id` | the fleet agent the spawn resolved to (`RemoteACPBackend.ResolvedAgentID`) | by `JobRun.Spawn`, first spawn wins |
| `stages[]` | `{stage, entered_at}` per lifecycle moment | by `JobRun.markStage`, a repeated moment recorded again |
| `verification[]` | the last validator pipeline's results | by `JobRun.Verify` |
| `attempts[]`, `attempt_budget` | the task's attempt history and budget | derived on read |
| `terminal_reason` | `Task.ConclusionReason`, the closed vocabulary | derived on read |
| `readmit_allowed` | the task is parked (not failed) | derived on read |
| `delivery` | the task artifacts the runner published | derived on read |

**The first three persist; the last four do not.** They are the TASK's facts,
laid over a copy of the status by `withTaskProvenance` when `GetJobStatus`
reads it, because a copy taken when the job last wrote its status would be
stale for every attempt after it. The published status is immutable — it is
read by every poll and by the next snapshot — so the enrichment always copies.

**`agent_id` is not `node`.** `node` is the daemon whose control loop drove
the job; `agent_id` is the fleet agent its worker session was dispatched to.
They differ whenever the work runs anywhere but the control plane, which is
the normal case, and the page links `agent_id` into the fleet.

### Delivery is read from artifacts, never from the module

`pkg/orchestration` does not import `pkg/sdlc`. The git side of an attempt
reaches the job page because the module PUBLISHES it as Task artifacts under
names the engine owns:

| Artifact | Kind | Carries |
|---|---|---|
| `ArtifactNamePullRequest` (`pull_request`) | `url` | `Ref.Path` the URL; metadata `number`, `state`, `branch` |
| `ArtifactNameHead` (`head`) | `commit` | `Ref.SHA`, `Ref.Branch` |
| `ArtifactNameReviews` (`reviews`) | `inline` | a JSON array of `JobReviewRound` |

**Every name is scoped to the attempt** — `pull_request@a3`,
`ArtifactNameForAttempt`. `SubmitTaskArtifacts` indexes a task's artifacts by
name and REPLACES a resubmission in place, while a retried task keeps the same
record and merely increments `Attempts`. Published flat, attempt 3's pull
request therefore overwrote attempt 1's, and attempt 1's job page then showed
attempt 3's delivery as its own — the exact opposite of what a per-attempt
page is for. Scoped, every attempt's delivery survives and each job reads
only its own.

`JobDeliveryFromArtifacts(artifacts, attempt)` takes the attempt as part of
the question rather than filtering afterwards, and returns nil when THAT
attempt published nothing — so an attempt that died early shows its own
emptiness instead of borrowing its neighbour's work. An artifact that is
PRESENT and undecodable is an error, not a silent omission: review rounds that
exist and are not shown is the failure the issue is about.

The task page, which is per-task, groups them by base name and shows them all.

**An offloaded artifact is resolved before it is decoded.**
`SubmitTaskArtifacts` rewrites an inline artifact past the store's inline
limit to a `kv_key` and clears `Content`, so the rounds of a long review reach
the record as a reference to bytes elsewhere. Read straight off the record,
that showed every SHORT review and silently dropped every long one — the
findings most worth reading. `withTaskProvenance` dereferences the reviews
artifact through the task store first, and `JobDeliveryFromArtifacts` refuses
an unresolved `kv_key` rather than treating it as absent, so the omission can
never be silent again.

### Every runner verifies through `JobRun.Verify`

`Verify` is the only path that records what a validator said. A runner that
calls `Validators().RunPipeline` itself runs the gate and throws the answer
away, which leaves the verification panel empty on exactly the runs a reader
needs it for — a review-gate refusal and the redelegation that follows.

The names are constants in `pkg/orchestration`, referenced by `pkg/sdlc`'s
`Hooks.publish`, so a rename is a compile error rather than a page that
quietly stops linking pull requests. (It quietly stopped once: the frontend
filtered artifacts on `kind === 'pull_request'`, a kind the engine's own
artifact validator refuses, so a merged run linked nothing.)

### `Task.ConclusionReason` is persisted

`applyConclusion` decides why a task stopped and hands the reason to the
registered observers (#790). It is now also WRITTEN to the record, and cleared
by a requeue. A reader hours later has no conclusion event to consult, and
re-deriving the reason from the resting status cannot tell an `abandoned`
attempt from an operator's hold — the two differ only in what produced them.
The block and readmit verbs write it from the same vocabulary.

### What the page renders

`cmd/quicbaas/frontend/src/lib/jobDetail.ts` holds the panel decisions as pure
functions; `JobDetailView.svelte` is their rendering.

| Panel | `runner=workflow` | `runner=sdlc` |
|---|---|---|
| Summary | workflow, lead backend, lead model | attempt `N of M`, budget, agent (linked to the fleet), daemon |
| Iteration gauge, task progress, DAG tab, cost tab | yes | no |
| Stage timeline with per-moment durations | no | yes |
| Delivery: branch, head, pull request, review rounds | no | yes |
| Verification results | no | yes |
| Outcome badge from `terminal_reason` | when the task concluded | when the task concluded |
| Worker's last turn (`sdlc:agent_turn`) | no | when the daemon recorded one |

A record that names **no** runner is a workflow job — the engine's own reading
of an empty `JobStatus.Runner`.

**The worker's turn is split before it is shown.** `pkg/sdlc` records each
turn as `job.Audit("sdlc:agent_turn", turn.Describe())` (go-quicbaas#787), and
`Describe` writes a header of `key=value` counts, a blank line, then the
reply. The page splits at the FIRST blank line — the reply has blank lines of
its own — and renders the counts as metrics beside the panel title rather than
inside the box labelled as the worker's words: `tokens_in=1843` is the
daemon's measurement, not something the agent said. A turn whose reply is
`(the worker answered with no text)` is rendered as **silent**, which is the
condition three attempts at go-truststrap#26 died of while the page said
nothing at all.

The producer is read opportunistically. A daemon that emits no such event has
no panel, and a body carrying no header at all is treated as all reply rather
than discarded — losing the worker's words because an expected header was
missing is the worse failure.

**Retry is `readmit_allowed`, not `state === 'failed'`.** The action returns
the TASK to the queue, and whether that is possible is the server's answer:
`abandoned` parks and may be readmitted, `budget_exhausted` fails and is
terminal, and both are "the attempts ran out". The UI never re-derives it.

`TERMINAL_REASONS` renders the whole closed vocabulary as a map, and
`jobDetailLib.test.ts` asserts its key set against
`TaskConclusionReasons()`, so a reason added on the server and forgotten in
the UI fails a test instead of rendering a raw wire string.

### The page reads GetJobStatus, not the list

`ListJobs` returns the raw persisted statuses. Only `GetJobStatus` runs
`withTaskProvenance`, so **only that path carries the attempt history, the
budget, the terminal reason, the readmit state and the delivery**. A detail
page populated from the list row renders every one of those panels empty.

The UI therefore paints from the list row as a placeholder — it is what makes
the page appear instantly, and on a cold load at `#/jobs/:id` it may not exist
at all — and then replaces it with the record read through `GetJobStatus`
(`loadSelectedJob`). The route effect depends on the route, never on the
selection it writes, or it would re-run on its own write forever.

A consumer that lists jobs and renders a detail from the row is making the
same mistake; read the job by id.

### Transports

The provenance block is on the existing job shapes — no new routes. REST
serializes `JobStatus` directly; gRPC adds fields 22–29 to
`OrchestratorGetJobStatusResponse` (`OrchestratorJobStage`,
`OrchestratorJobAttempt`, `OrchestratorJobDelivery`,
`OrchestratorJobReviewRound`, `OrchestratorJobVerification`); MCP's
`quicbaas_orchestrator_get_job_status` adds the same keys, letting the domain
types serialize themselves rather than hand-rolling a second wire format.
