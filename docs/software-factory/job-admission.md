# Agent Orchestration — Job Admission, Attempt Accounting, and Reaping

> **Status: NORMATIVE** · Last verified: 2026-09-10
> Supplement to the Software Factory feature folder: the durable job lease,
> attempt accounting, the reaper and the stall detector.
> Conflicts with this document are defects.

How a Job is durably owned by one node, how a Job's outcome is charged to
its Task, and how a Job whose node died or whose agent vanished is
concluded. All of it is generic and lives in `pkg/orchestration`.

## The lease

Every Job a service runs holds a **JobLease** before its first session is
spawned. The lease is a record at

```
__sys:orch/joblease/{tenant}/{project}/{sha256(workflow)}/{sha256(ref)}
```

where `ref` is the Task's source reference, the Task ID when the Task has
no source, or the Job ID for a Job bound to no Task, and `workflow` is the
workflow a workflow job runs — or, for a job with no workflow, **the name of
the runner driving it** (`sdlc`, say). `workflow` and `ref` are hashed
because they are not the engine's to shape; `tenant` and `project` are
validated key segments. An unscoped Job uses `-` as its project segment.

```go
type JobLease struct {
    TenantID, ProjectID, Workflow, Ref, TaskID, JobID string
    Generation  int    // the attempt ordinal; 1 for a Job with no Task
    Node        string // the daemon running the loop
    AcquiredAt, HeartbeatAt int64
    Released    bool   // release is a FIELD, never a delete
    ReleasedAt  int64
}
```

**Admission.** A contender takes a held lease only when the holder
released, the holder's generation is lower, or the holder's heartbeat is
older than the TTL. A Job re-acquiring its own lease at its own generation
is admitted (that is resumption after a restart). Anything else is declined
with `JobLeaseTakenError{HeldBy, HeldByJobID, Generation}`.

**Stores.** `NewTransactionJobLeaseStore(tx, kv, ttl)` decides leases on
go-qrdb's transaction plane through `cas.Writer.Handover` — one proposal,
no read-then-write window — and is the store for every cluster-like mode.
`NewProcessJobLeaseStore(ttl)` is the complete answer for a standalone
boot, where the only contention is between goroutines. The server picks by
the same predicate the task-claim arbiter uses.

**Capacity.** `NewProjectCapacityJobLeaseStore(store, projects, ttl)` wraps
either store and refuses an acquire that would put a project over
`Project.Quotas.MaxConcurrency` with `ProjectCapacityExceededError{Live,
Limit, RetryAt}`. Live means unreleased and heartbeating within the TTL.

**Off the budget.** A lease with `OffBudget` set is outside
`MaxConcurrency` in both directions: admitted while the budget is full, and
not filling it. A runner declares it through `JobLeasePolicy.OffProjectBudget`
— the `review` runner does, so a project at `max_concurrency: 1` can still
review its own pull requests instead of queueing the review behind the worker
whose change it has to read.

The flag is on the **record**, not in the admitting node's memory, because
the budget has three readers: the capacity store's own admission check, the
task dispatcher, and the task-source poller. All three answer through
`JobLease.CountsAgainstBudget(now, ttl)` — the two schedulers by way of
`CountLiveJobLeases`, which is a *budget* count rather than a job count — so
they cannot disagree. A budget honoured in one place and ignored in another
admits a job that then cannot run.

**Subject.** A lease is named after the work it holds, not the job holding
it: the task's source reference for task-bound work, and otherwise whatever
`JobLeasePolicy.LeaseSubject` answers. Without a policy a job bound to no
task is leased by its own id and contends with nothing, which is right for a
one-off and wrong for a runner whose jobs are per-subject singletons. See
`docs/software-factory/job-runners.md`.

**Heartbeat.** The **envelope** heartbeats on every state transition
(`JobRun.SetState`) and on a ticker at TTL/3, started when the lease is
acquired and stopped by `JobRun.execute` on every exit path — not by the
runner, which cannot forget to. A heartbeat that finds another Job holding
the lease aborts the run with `JobSupersededError{By, Node}`.
`JobStatus.Node` and `JobStatus.HeartbeatAt` carry the last proof of life.

**Acquire before Prepare.** The lease is taken in `execute`, before the
runner's first state and therefore before the `Prepare` moment. It used to
be taken inside the loop's `initializing` handler, after Prepare had already
run: a deliberate change, and the safer order — no side effect happens
outside the lease.

**Release.** A Job bound to no Task releases on the run's exit. A Job
bound to a Task keeps its lease until `concludeAttempt` has written the
Task, then releases — releasing first would leave a crash window in which
the Task is open, the lease is released, and the reaper (which acts only on
unreleased leases) could never repair it. `CancelJob` releases
synchronously.

## Attempt accounting

A Job bound to a Task concludes the Task **synchronously**, on the
goroutine that ran the loop, immediately after `Run` returns
(`agentServiceImpl.concludeAttempt`). The Task is written **once** through
the store's compare-and-swap on `Task.Version`; a conflict re-reads and
re-derives, so a concurrent block, readmit, or artifact write survives.

The Task accepts a conclusion only from the Job it names in
`SpawnedJobID`, and only while its attempt is open (`pending`, `claimed`,
`in_progress`). Every conclusion moves `SpawnedJobID` into
`Metadata["attempt_history"]` (`Task.AttemptHistory()`), so a second
report about the same Job is refused with `AttemptConcludedError`.

`JobStatus.FailureClass` carries **why** a Job failed as a class, never
prose:

| Class | Source | Accounting |
|---|---|---|
| an environmental terminal reason (`api_error`, …) | `PreFirstTurnFault` | refunded, requeued (bounded) |
| `accounts_cooling_down` | `AccountsCoolingDownError` | refunded, requeued at `AvailableAt` (clamped to 24h) |
| `project_capacity_exceeded` | `ProjectCapacityExceededError` | refunded, requeued at `RetryAt` |
| `no_provider_account`, `no_account_resolver`, `account_credential_missing`, `account_refresher_missing` | resolver errors | refunded, **blocked** for a human |
| `superseded` | `JobSupersededError`, `JobLeaseTakenError` | nothing written; the lease holder owns the Task |
| `stalled` | stall detector | charged |
| `abandoned` | reaper | charged; requeued, or blocked (`escalated`) when the budget is spent |
| `""` | any other failure | charged; requeued, or `failed` with `Result.Metadata["reason"]="budget_exhausted"` |

### Operator faults — a factory that could not present the work

The classes above are read off a typed error on the **dispatch** path, before a
runner exists. There is a second family, arriving by a different door: a
judgement a runner makes **while it holds the job**, carried on
`BlockReason.FaultClass` (`JobRun.Park(kind, reason, faultClass)`).

| Class | Proved by | Accounting |
|---|---|---|
| `workspace_unfit` | the project names no workspace path; the daemon could not make the mount point; the checkout is absent, empty, holding another repository, or missing a declared context repository; any git command of the checkout failed | refunded, **blocked** for an operator |
| `worker_environment` | the worker declared `SDLC-STOP: environment`, declared a stop code this binary does not know, or answered with no text on every turn of the attempt | refunded, **blocked** for an operator |

The park still stops the Task — nothing retries it until a person acts — and
the class adds three things:

- the attempt is **refunded** (`Task.EnvironmentFaults++`), so an operator's
  mistake cannot exhaust a budget that belongs to the work;
- the class is written onto the block (`Task.BlockFaultClass`), so
  `taskSourceReleasable` lets a re-label readmit the Task after the repair, and
  the issue comment names the cause (`blockedCause`);
- the daemon's log says `attributed to the factory, not charged to the work`
  rather than the contradictory `charged as a failure of the work` it used to
  print on the line after classifying the same job as an operator's to repair
  (`logAttribution`).

**An unrecognised class is charged.** `OperatorFaultClass` answers false for
anything outside the two above, and `JobRun.Park` drops such a class while
keeping the park. Reading a genuine failure as infrastructure would refund
every attempt of a broken change and retry it for ever; the fail-closed
direction is to charge it and let the budget bound the retries.

Everything else a runner parks for — a worker's question, an owner's decision,
a spent review budget, a prompt that repeated — carries **no** class and is
charged exactly as before.

### A held subject is refused at StartJob

A runner whose jobs are per-subject singletons — one declaring a
`JobLeasePolicy`, today triage and review — acquires its lease inside the
run. Starting a second job on a subject already in flight therefore used to
succeed, create the job record, and fail it minutes later with `job lease:
held by another job`: a failed job in the queue for what was a duplicate. The
triage backstop produced one every fifteen minutes for as long as a project's
pass outran its interval.

`StartJob` now reads the subject's lease before any record exists and applies
the store's own admission rule (`jobLeaseAdmits`: a released or stale holder
admits, a later generation supersedes, the same job re-acquires) to a
contender of the request's generation. A contender the store would refuse is
refused here, synchronously, as `ErrJobLeaseTaken` naming the holder — the
answer the triage scheduler's "already running" branch was written for. A
runner that declares no subject is not asked.

Three **consecutive** environmental faults (`MaxConsecutiveEnvironmentFaults`)
block the Task and charge the attempt. `Task.ChargedAttempts()` is
`Attempts - EnvironmentFaults`; `Task.BudgetExhausted()` compares it to
`AttemptBudget` (default `DefaultAttemptBudget` = 3); `Task.Deferred(now)`
reads `RetryNotBefore`.

## The attempt history a Task can be read back through

`Task.AttemptHistory()` names every Job that ran the Task. Reading one of
those Jobs back is what makes a terminal Task legible, and it needs two
things to agree: the narrowing the caller asks for, and the store the record
was filed in.

**The narrowing is the SERVER'S.** `ListJobs(ctx, tenantID, *JobFilter)`
applies `JobFilter.TaskID`, and every transport passes the caller's
`task_id` into it — REST `GET /api/v1/orchestrator/jobs?task_id=…`, gRPC
`OrchestratorListJobsRequest.task_id`, MCP
`quicbaas_orchestrator_list_jobs{task_id}`, the embedded transport, the Go
SDK `ListJobsRequest.TaskID`, the TypeScript SDK `listJobs(tenantID, taskID)`
and `quicbaas orchestrator job list --task`. A filter the client applies to
whatever the tenant listing happened to return cannot tell "this task has no
attempts" from "this task's attempts were not in the answer".

A job matches on `JobStatus.TaskID`, falling back to `Config.TaskID` for a
record persisted before the binding was promoted onto the status. A nil or
empty filter lists every Job the tenant owns: *no narrowing asked for* is
never *narrow to nothing*.

**The record scope is UNTENANTED, by construction.** A job record is
addressed by its key — `job:{tenant}:{id}`, and a listing is bounded by
`job:{tenant}:` so no tenant can observe another's — not by the tenant on the
operation's context. The store underneath the orchestrator routes by that
context (`pkg/server/tenant_kvstore.go`), so all four job-record paths are
detached from the caller's tenant:

| Path | Scope | Why it cannot carry a tenant |
|---|---|---|
| `persistJobStatus` | `jobRecordOp` | runs on the job goroutine, after the request that started it returned |
| `loadJobStatus` | `jobRecordOp` | serves `GetJobStatus`, and must answer for a record written by any of the others |
| `ListJobs`' merge scan | `jobRecordScope` | must see the same records the other three do |
| `recoverJobs` | `jobRecordScope` | enumerates **every** node's interrupted jobs at boot, before any request exists |

`RecordJobDeath` is the reason this is not merely convenient: the reaper
writes a death record for a tenant whose barrier may by then be sealed, and a
subsystem whose entire purpose is to record what happened cannot fail closed
on a seal.

**The two scopes differ in exactly one thing: whether they impose a
deadline.** Getting that wrong recreates the defect this section is about
through a different door, so it is stated rather than implied.

- `jobRecordOp` — ONE record, bounded by `jobRecordTimeout` (5 s). A single
  `Get` or `Put` that has not answered in five seconds is a stuck store, not
  a large one. Both callers build it from nothing: neither has a caller
  context to inherit.
- `jobRecordScope(ctx)` — the two SCANS. It drops `ctx`'s values and adds **no
  deadline of its own**; a caller with one keeps it, a caller without one
  stays unbounded. A fallback ceiling here would make a deadline-less REST
  listing inherit it as the only bound in the chain, and a tenant with
  thousands of concluded records — or a store whose prefix scan is simply
  slower — would have its attempt history truncated with the scan error only
  logged. That is the #822 symptom again.

The **boot budget is therefore the caller's, unchanged**: `recoverJobs` runs
under the `Server.ShutdownTimeout`-bounded context `recoverOrchestrationJobs`
passes it, and that context — not the scan scope — is what `resumeJob` and
`launchJob`'s synchronous legs (the project lookup in `loadJobProject`) run
under.

Cancellation IS forwarded in both scopes, so a listing stops when the request
it serves is abandoned. A record written with no tenant at all files under
the system tenant (`jobPersistenceKey` normalises the segment the way
`StorageTenant` does), where a listing can name it.

Before #822 only three of the four agreed. `ListJobs` scanned on the request
context while the writer wrote on a bare one, so on a barrier-isolated tenant
every concluded job was filed in one backend and looked for in another: the
tenant listing returned only the jobs still in memory, while `GET
/orchestrator/jobs/{id}` answered for a record two days older from the same
store.

## Reaper and stall detector

Both run from the server on the heartbeat cadence (TTL/3) via
`agentServiceImpl.Sweep`, and both are no-ops when nothing is bound.

**Reaper** (`JobReaper`) scans every unreleased lease. A lease whose
heartbeat is older than the TTL and whose Job is not running in this
process is a death: the persisted `JobStatus` is marked `failed` /
`abandoned`, the Task is concluded through `concludeAttempt`, and the
lease is released. A late heartbeat on a Job running here is `run_live`
and untouched. A dead lease with no Task, or whose Task has already moved
past the Job, is `orphaned` and released.

**Stall detector** (`JobStallDetector`) examines this node's running,
Task-bound loops against per-state bounds (`orchestration.idle_thresholds`,
defaults in `DefaultJobIdleThresholds`). A Job past its bound in a
session-backed state (`executing`, `reviewing`) is asked whether any of
its `SessionIDs` is still addressable via `SessionStatus`; if none is, the
loop is aborted with `JobStalledError` and concluded as a charged failure.
A Job past its bound in any other state, or with an addressable session,
is reported (once per threshold period) and left running. A session whose
state cannot be read concludes nothing.

**The session is asked for under the Job's own tenant.** The sweep runs on
the daemon's maintenance ticker and carries no principal, so its context
names no tenant; the session service resolves tenancy from the context and
from nowhere else. The detector therefore binds `JobStatus.TenantID` onto
the probe context with `ContextWithTenant`, the same way `RecordJobDeath`
does. A run whose status names no tenant is left untenanted and its probe
is refused, which is `session_unknown` — the detector never invents a
tenant to get an answer with.

**Liveness of a containerized session is the container's.** A container
session has no supervisor — `docker run -i --rm` belongs to the container
runtime, so there is no `*exec.Cmd` for `procReaper` to `Wait` on — and its
death reaches the daemon only as EOF on a stdio nothing reads between
prompts. `ACPBackend.Status` therefore asks the runtime
(`ContainerRuntime.IsRunning`) whenever the session records a container:
not running, or `ContainerNotFoundError` because `--rm` removed it, is
`closed`. Any other runtime failure is returned as an error, so the
detector reports `session_unknown` rather than ending live work on a
`docker inspect` that could not run. Across the wire, a daemon answering
`codes.NotFound` is translated back into `SessionNotFoundError` by
`RemoteACPBackend.Status`, which is what lets the detector tell "the worker
is gone" from "the worker is unreachable".

## Recovery

`recoverJobs` resumes only persisted Jobs whose `Node` is this node's name
(`quicbaasd-node-<raft id>`, else `quicbaasd-<hostname>`); another node's
Job is that node's to resume, or the reaper's to conclude.

**The runner decides where a resumed Job restarts.** `resumeJob` reads the
runner name off the persisted `JobStatus`, looks it up, and calls the same
`launchJob` a fresh start uses — the two differ only in the Job ID and the
entry state. `resumeStateFor(runner, interrupted)` asks the runner's
optional `JobResumer` and otherwise restarts at the runner's first state.

| Runner | Interrupted in | Resumes at |
|---|---|---|
| `workflow` | `initializing`, `decomposing` | `initializing` |
| `workflow` | `scheduling` … `reviewing`, `re-delegating` | `decomposing` — sessions are dead after a restart; work items and memory are preserved |
| `workflow` | `escalating` | `failed` |
| `workflow` | `completing`, `cleanup` | `completing` |
| `sdlc` | `initializing`, `executing`, `validating`, `re-delegating` | `initializing` — nothing reached the forge and the container is gone; `Prepare` continues the branch the attempt already pushed |
| `sdlc` | `reviewing` | `reviewing` when the `Delivery` records a head, `initializing` when it does not |
| `sdlc` | `completing` | `completing` — `Complete` is head-bound and idempotent |
| `sdlc` | `cleanup` | `cleanup` — the merge and the post-merge steps are done; teardown needs no session |
| `sdlc` | `escalating` | `escalating` — the escalation FINISHES: `Blocked` if it was not already posted, teardown, then `failed` |

**The `sdlc` table is per state, and none of it is a catch-all.** A state the
runner does not declare — a record written by a runner that has since changed
shape — restarts at `initializing`, which is safe for anything that published
nothing.

`reviewing` is decided in two places because `JobResumer` cannot see the
record: `ResumeState` answers `reviewing`, and the reviewing step reads the
`Delivery`. A recorded head is the push that already happened, so the resumed
run does the daemon's own half — open or refresh the pull request, ask the
`forge_review` gate for the verdict — instead of re-cloning and re-executing
work the forge already has. No head means the push never happened and the
worker's edits went with the container, so the step returns `initializing`. A
gate that then asks for another round needs a worker the resumed run does not
hold, and `executing` sends it to `initializing` for one.

`escalating` resumes at **`escalating`, not `failed`**, which is where the
`sdlc` runner and the `workflow` runner part company. `resumeJob` never
launches a run for a terminal answer — it persists the state and returns — so
mapping to `failed` would conclude a job that still owes a person the reason
it stopped. The `sdlc` escalation has that debt: `Blocked` posts the park
reason to the issue and moves its label, and a daemon that died between
entering `escalating` and `Blocked` completing left the issue with nothing on
it. Resuming at `escalating` finishes exactly that, and the state still
returns `failed`.

It posts **exactly once**. The park comment carries a marker naming the task,
the attempt and the block kind (`<!-- quicbaas-sdlc:blocked task=… attempt=…
kind=escalated -->`), and a resumed run reads the issue's comments before it
posts. The attempt is part of the identity on purpose: a task readmitted after
a park is a new attempt that must be able to park again. A fresh run skips the
read — it cannot have posted anything yet. The park reason itself is restored
from the audit trail, where the run that decided the escalation records it as
`sdlc:escalated`; `JobStatus` carries no escalation reason of its own. Issue
#626.

The separate bound-Job resume table this section used to describe
(`resolveBoundResumeState`) is deleted with bound-task mode.

A runner declaring a state the stall detector's own maps do not know is
silently unwatched, and `JobIdleThresholdsFrom` refuses an operator's
threshold for it: the detector has not moved onto `KnownJobStates()` yet.
See [job-runners.md](job-runners.md) and issue #621.

## Shutdown drain

Daemon shutdown drains the running control loops before it closes the
stores they write to. `Server.Shutdown` runs the drain after the last thing
that starts a Job (the SDLC module, the task-source poller, the dispatcher,
the sweep) and before the first thing that closes a store.

`agentServiceImpl.Shutdown(ctx)`:

1. marks the service draining — a Job launched from here on is refused with
   `ServiceShuttingDownError`, including a running loop's child Job, because
   the transports are still accepting requests at this point;
2. waits for every launched Job goroutine, then for every asynchronous turn,
   within one bounded window (`DefaultShutdownDrainTimeout`, 10s, or the
   caller's deadline);
3. returns `DrainTimeoutError` naming what it left running when the window
   closes first.

**The drain does not cancel the loops it waits for.** A cancelled loop
returns `context.Canceled`, which the envelope records as a FAILED Job:
work that was merely interrupted would be charged as work that failed and
would spend its task's attempt budget. A loop that does not fit the window
keeps its non-terminal record and is resumed by `recoverJobs` on the next
start — the same disposition it has when the daemon is killed, and the one
the resume table above expects.

A store that closes under a loop the drain could not finish is survivable:
the envelope's exit path recognizes a closed store and reports
`StoreClosedError` instead of letting the engine's panic reach the top of
the Job goroutine.

## Configuration

```yaml
orchestration:
  reap_lease_ttl: 90s          # heartbeat age at which a lease is stale; 0 = default
  idle_thresholds:             # per job state; 0 = default, negative = do not watch
    executing: 3h
    reviewing: 30m
```

An `idle_thresholds` key that is not a job state is refused at config
validation and again at wiring.

## Errors

All typed, all with sentinels: `JobLeaseError` (`ErrJobLeaseInvalidInput`,
`ErrJobLeaseUnreadable`, `ErrJobLeaseSubjectMismatch`, `ErrJobLeaseBackend`),
`JobLeaseTakenError`, `JobSupersededError`, `JobLeaseNotFoundError`,
`ProjectCapacityExceededError`, `FleetCapacityExceededError`
(`ErrFleetCapacityExceeded`), `AttemptConcludedError`, `JobStalledError`,
`JobIdleThresholdError`, `DrainTimeoutError` (`ErrDrainTimeout`),
`ServiceShuttingDownError` (`ErrServiceShuttingDown`), `StoreClosedError`
(`ErrStoreClosed`).

## Source files

- `pkg/orchestration/job_lease.go` — record, key, stores, the project and fleet capacity checks, errors
- `pkg/orchestration/fleet_capacity.go` — how many worker slots the deployment has; see [admission-and-readiness.md](admission-and-readiness.md)
- `pkg/orchestration/job_run_lease.go` — the loop's acquire, heartbeat, abort, release
- `pkg/orchestration/task_attempt.go` — classification, outcomes, `concludeAttempt`
- `pkg/orchestration/job_reaper.go`, `job_stall.go` — the sweep
- `pkg/orchestration/job_admission.go` — the service's wiring, finish path, `Sweep`
- `pkg/orchestration/shutdown.go` — the drain, its phases and its bound
- `pkg/server/lifecycle_orchestration.go` — node name, store selection, sweep goroutine, drain step
