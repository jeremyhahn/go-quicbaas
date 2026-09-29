# Autonomous Projects — the switch, the dispatcher, and the worker's workspace

> **Status: NORMATIVE** · Last verified: 2026-09-10
> Supplement to the Software Factory feature folder: the autonomy switch, the
> task dispatcher, and the workspace spec a worker container clones from.
> Conflicts with this document are defects.

How a project runs work on its own: what turns autonomy on, how the engine
decides which pending Task starts next, and what the daemon hands a worker
container so the container — never the daemon — can clone the repositories it
needs. All of it is generic and lives in `pkg/orchestration`.

## The switch

```go
type Project struct {
    // ...
    Autonomous   bool          // the engine adopts and dispatches for this project
    ContextRepos []ContextRepo // repositories the worker clones beside the project
    Quotas       ProjectQuotas // MaxConcurrency is the one concurrency number
}
```

`Project.Autonomous` is the only thing that turns autonomous development on.
It gates two loops:

- the **task-source poller** (`task_source.go`), which asks every registered
  `TaskSource` what work a project has and turns each new answer into a Task;
- the **task dispatcher** (`task_dispatcher.go`), which starts jobs for the
  Tasks that are waiting.

A **parked** project (`Project.Parked`) keeps both loops running for the work it
has already started and refuses everything new; see
[Parking a project](projects.md#parking-a-project).

`Project.DefaultWorkflow` no longer gates anything. It keeps its original
meaning — the workflow a Task under this project is auto-spawned into — for
projects whose work runs the `workflow` runner, and it is irrelevant to a
project whose work runs a module runner such as `sdlc`. The SDLC module's
`Bind` sets `Autonomous = true`; it does not require a default workflow.

## A project id is a UUID

`Project.ID` is the UUID `CreateProject` mints; `Project.Name` is free text the
operator chose. They are not interchangeable, and passing the name where the id
belongs used to be accepted silently (#613): the record was scoped to a project
that does not exist, the project's listing did not show it, and the concurrency
quota below did not count it, because both filter on the id.

`CreateTask` and `StartJob` both refuse a `project_id` that is set but is not a
UUID, with a typed `InvalidProjectIDError` naming the value. They are the two
ways in — a task the engine will dispatch later, and a job started directly —
and a job's lease pins the project segment of its key from whatever the job
carried, so a name accepted here would key a lease under a project nobody
registered (#613, #667). An **empty** `project_id` is still accepted and means
unscoped, which is a real state — not every task or job belongs to a project.

Work items are scoped by the engine, not by the agent that produced them. A
lead's decomposition and a reviewer's follow-up items come back as free-form
text, so `ControlLoop.scopeWorkItems` binds every item to the project the JOB
runs under, overwriting any project the agent named itself. Each item's Task is
then created with that project, so a decomposed job's work is visible to the
project's listing and counted by its quota.

`Quotas.MaxConcurrency` bounds ONE project, and zero means unlimited. It is
enforced in four places, deliberately — and the fleet's own capacity is enforced
in a fifth:

| Where | What it does |
|---|---|
| `projectCapacityJobLeaseStore.Acquire` (`job_lease.go`) | refuses the lease — the actual boundary |
| `fleetCapacityJobLeaseStore.Acquire` (`job_lease.go`) | refuses the lease over the **deployment's** worker capacity, whatever any one project's quota permits |
| `TaskDispatcher.sweepProject` | starts only what admission left room for |
| `TaskSourcePoller.sweepProject` | adopts only what admission left room for, less the queue |
| `agentServiceImpl.checkProjectConcurrency` (`orchestrator.go`) | refuses an `AutoSpawn` dispatch at `MaxConcurrency − inflight`, before the claim |

The two lease checks are the boundary; the other three narrow the window the
lease store already states it has, and keep the queue from filling with work
nothing will start for hours.

**A per-project number is not the only bound it needs to be.** It cannot stop N
projects each inside their own quota from oversubscribing the fleet — ten
projects at three jobs each will run thirty on a fleet with eight worker slots —
and it says nothing about whether the provider pool can serve another session.
Both sweeps therefore ask an **admission controller** that considers four inputs
at once — the project quota, the fleet's worker capacity, provider usage
headroom, and the project's and tenant's own spend limits — and divides the fleet's free slots by max-min fairness, so the
aggregate bound cannot let one busy project starve every other. The fleet row
above is that bound's last-ditch check at the lease, reading the same capacity
the controller reads. See
[admission-and-readiness.md](admission-and-readiness.md).

The `checkProjectConcurrency` row closes a bypass (go-quicbaas#898). A manually triggered
`CreateTask(..., AutoSpawn: true)` goes straight to `autoSpawnTask` and reached
neither sweep, so a caller in a loop could hand a project capped at two twenty
concurrent jobs — and the sweeps had nothing left to reclaim, because the jobs
were already leased and running. The check runs **before** `ClaimTaskSpawn`,
because the claim charges the task an attempt and a task refused for capacity
has not been attempted. It computes capacity exactly as the dispatcher does —
the quota less the project's live leases — since it is answering the
dispatcher's question at the dispatcher's moment: may one more job start now.
The poller's arithmetic subtracts the queued tasks as well, and rightly so,
because it is deciding whether to *create* work; a task that already exists
must not count against itself.

The refusal is `ProjectCapacityExceededError`, the same type the lease store
raises, so a task refused at admission gets the same retryable
`project_capacity_exceeded` fault class and the same retry-at. The task is
still created and still pending, with the refusal as its dispatch reason, so
the dispatcher's next sweep starts it as soon as a slot frees. Where no quota
can be *decided* — no project store, an unscoped task, no lease store, an
unknown project, no cap — the dispatch is admitted, because each of those is a
deployment in which no quota was ever configured; a project store that
**errors** is returned instead, since dispatching on a guess about a limit is
how a fleet gets oversubscribed.

## The dispatcher

```go
NewTaskDispatcher(TaskDispatcherConfig{
    Tenants, Projects, Tasks, Leases, LeaseTTL, Spawner, Admission, Logger,
})
```

`Run(ctx, interval)` sweeps at the poller's cadence beside the poller;
`Sweep(ctx)` visits every tenant's autonomous projects once; `SweepProject`
does one project now.

A project that is **not autonomous** dispatches nothing, parked or not. The
sweep still reads its pending queue so the skip is not silent: every
auto-spawned, unbound pending task gets the `dispatch_reason` "project autonomy
is off: nothing dispatches until it is switched on". Like every refusal, the
reason is written once and warned once. It is cleared when a later sweep
dispatches the task. A parked project drains only while autonomy is on (see
[Parking a project](projects.md#parking-a-project)).

Per autonomous project, one sweep:

1. lists the project's **pending** and **in_progress** Tasks — the queue it
   dispatches from, plus the running ones, because the sweep is also the only
   thing that frees a Task bound to a job that no longer exists and a Task
   whose attempt is under way reads `in_progress`;
2. counts **inflight** = the project's live job leases;
3. computes `slots` from the sweep's admission plan — the project quota, the
   fleet's free capacity divided fairly, and the provider pool's headroom — or
   from `MaxConcurrency − inflight` alone when no controller is wired, unbounded
   when the quota is zero
   ([admission-and-readiness.md](admission-and-readiness.md));
4. builds the **candidates** — `AutoSpawn && SpawnedJobID == "" &&
   Status == pending && !Deferred(now) && !BudgetExhausted()` — ordered by
   `Priority` descending, then `CreatedAt` ascending, then ID;
5. starts the first `slots` of them through `TaskSpawner.SpawnTask`.

`TaskSpawner` is the seam:

```go
type TaskSpawner interface {
    SpawnTask(ctx context.Context, task *Task) (jobID string, err error)
}
```

The agent service satisfies it. Both the runner (`Task.Runner`) and the
routing hints (`Task.Routing{Backend, HostAffinity, GpuRequired}`) are read
off the **record**, so a Task dispatched an hour later runs under the runner
and routes under the constraints it would have at creation. They used to be
folded into `Task.Metadata` and never read back; see
[job-runners.md](job-runners.md).

What a refusal means depends on the runner: the `workflow` runner needs a
matching online fleet node and the project's `default_workflow`, while a
module runner such as `sdlc` needs neither — it spawns its own sessions and
the job's lease is the capacity check.

### One pending Task, one job

Two spawners look at one pending Task routinely: auto-spawn runs at creation
while the dispatcher is mid-sweep over a list it read a moment earlier, and a
push sweep can arrive between them. Each decides "no job yet" from the snapshot
it holds, so a spawner that started the job first and recorded it second gave
the other one a whole job's worth of window to decide the same thing — two jobs
for one Task, two attempts charged, and a backoff long enough that a
restart-and-resume never got its retry (#670).

The **spawn claim** closes it. `ClaimTaskSpawn` (`task_dispatcher.go`) mints the
job's identity, writes it onto `Task.SpawnedJobID` under the task store's
version check, and charges the attempt in the same write; only the spawner that
wins that claim starts a job. A spawner whose claim finds a job already there is
not a failure — it reports the winner's job and starts nothing, which is what
makes `SpawnTask` idempotent per Task.

`Task.SpawnedJobID` already meant "the Task's open attempt" — it is what
`recordAttemptHistory` clears when an attempt concludes — so the claim is not
new state, only that state written a moment earlier. A claim whose job then
fails to start is given back and the attempt refunded (`ReleaseTaskSpawnClaim`),
which returns the Task to the queue it was claimed out of. A claim held by a
process that died between the claim and the run leaves a Task naming a job with
no live lease, which the sweep's own repair frees
([the stranded binding](#a-task-bound-to-a-job-that-is-gone)).

### The claim takes the Task off the queue

The claim is also where the Task's **status** moves, `pending -> in_progress`,
with the job as the claimant:

| | `pending -> claimed -> in_progress` | `pending -> in_progress` |
|---|---|---|
| **who claims** | an agent, through `ClaimTask` | the engine, through `ClaimTaskSpawn` |
| **why that many steps** | `claimed` means "taken, not started yet": the agent claims, then starts | there is no such gap — the claim and the dispatch are one write |

Both edges go through the one transition table (`taskTransitions`), so the
engine's claim is audited exactly like an agent's. **The engine dispatches out
of the queue and nowhere else**: a Task that is parked, already concluded, or
claimed by an agent in the window between the sweep's listing and the claim is
refused with `TaskInvalidStateError` and charged nothing, which leaves one
claimant rather than two.

Before that edge existed the engine claimed nothing, and a Task stayed `pending`
for the whole life of the job it had dispatched. Measured on the burn-in node
(go-truststrap#14, 2026-09-21): a Task adopted at 02:13:49 and merged at
02:28:06 read `pending` for those fourteen minutes and went straight to
`completed`. `Task.Status` is what the API, `orchestrator task get`, the task
list and the label reconciler's claim sweep all read, so a busy hour showed
every running delivery as queued, the reconciler reported
`LabelReconcileUndecidable` three times per delivery on evidence that was simply
wrong ("the task is pending, which is queued rather than handed over"), and
anything reasoning from `StatusChangedAt` saw a fourteen-minute-old *pending*
Task — which is also the signature of a stranded binding, so a healthy run and a
real stall were indistinguishable by the record (go-quicbaas#966).

A conclusion moves the Task on from `in_progress` exactly as it did before
(`completed` / `failed` / `blocked`), and a requeue after a reaped attempt
returns it to `pending`.

### A Task bound to a job that is gone

The claim is written **before** the job starts, so a process that dies in
between leaves a Task naming a job that never ran. The **reaper cannot repair
it**: `JobReaper.Sweep` lists LEASES and examines the unreleased ones, so a job
whose lease was never acquired — or whose lease was correctly released — has
nothing for the reaper to find, and nothing looked at the Task again. Measured
on the burn-in node: `pending=4 inflight=0 slots=0 dispatched=0`, with not one
refusal line naming any of them.

So the sweep repairs it (`repairStrandedBindings`), which is why it lists the
running Tasks as well as the queued ones:

- **the grace period is the lease TTL** — the same clock the reaper judges
  staleness by. A binding younger than it is left alone whatever the lease store
  says, because a Task legitimately names a job whose lease does not exist yet;
- a binding older than that whose job holds **no live lease** has its attempt
  closed into `Metadata["attempt_history"]` and the Task requeued, so it is
  dispatchable again **in the same sweep**;
- a **lease store that cannot be read** repairs nothing: treating an unreadable
  store as "no live leases" would free every bound Task in the fleet at once.

The job lease is still the capacity boundary; it was never the deduplicator, and
by the time it refuses a second job the attempt is already spent.

### Why a Task is still pending

Every Task the dispatcher looks at and declines carries the answer on the
record:

```go
type Task struct {
    // ...
    DispatchReason string // why this task is not running
}
```

| Reason | Meaning |
|---|---|
| `project at capacity: N of M concurrent jobs are running` | the quota is spent |
| `deferred until <RFC3339>` | `RetryNotBefore` is in the future (an environment fault's hold) |
| `attempt budget exhausted: N of M attempts charged` | the Task is out of attempts |
| `spawn refused: <the engine's own explanation>` | the spawner would not start it |

A refusal the engine can NAME is rendered as that name rather than as the
error's own sentence: `classifyEnvironmentFault` already turns a spent spend
cap, an account cooldown or a full project into prose written for somebody who
was not watching, so a spend refusal reads

> spawn refused: the pooled provider account serving this job has spent its
> tokens allowance for the "five-hour" window (1200 of 1000). Nothing is
> broken — this is the spend cap configured on the account. …

Everything the classifier does not recognize keeps the error's own text:
inventing a category for an unrecognized fault is how a real failure stops
being investigated. See [cost-enforcement.md](cost-enforcement.md).

The reason is written through the task store's compare-and-swap and **only
when it changed**, with one bounded retry when another writer wins the race. A
WARN is logged on the same condition, so a project sitting at capacity for an
hour writes nothing and logs nothing after the first sweep. A successful
dispatch clears the reason.

A Task with `AutoSpawn == false`, one that already has a `SpawnedJobID`, and one
that is live without one (an agent claimed it) is neither dispatched nor refused
— the dispatcher has no opinion about it.

### Nudges

`Server.NudgeProject(ctx, tenant, project)` sweeps the **poller first, then the
dispatcher**. A verified webhook delivery says work appeared; polling first
turns that one call into a running job, where dispatching first would start
whatever was already queued and leave the new issue for the next tick.

## The worker's workspace — cloned by the container, never by the daemon

**The daemon does no clone, copy or worktree.** A daemon that cloned would own
a filesystem that has to be reaped, kept in step with the record, and reasoned
about after a crash. A container that clones owns nothing that outlives it.

It is also what keeps parallel workers apart: each has its own HEAD, index and
stash, so no worker can read another's branch as its own base or pop a stash
onto someone else's checkout. The charter adds the habit that catches the rest:
a fact read from a file outside the worker's own diff is confirmed from a ref
(`git show origin/<base>:<path>`, `git branch --contains <commit>`), not the
working copy (go-quicbaas#870).

What the daemon hands the container at spawn is a **spec**, decided on the
daemon and written — by the host that executes the session — into a fresh
per-spawn directory beside the session's workspace, bind-mounted read-only.
The spawn **names** its workspace rather than giving a host path, because the
host that runs it may be a fleet node on another machine
([sandbox](sandbox.md#agent-local-workspaces), go-quicbaas#981).

```go
spec, err := NewWorkspaceSpec(project, WorkspaceBranchPlan{
    Name: "issue/601-dispatcher-autonomy", Base: "main", Create: true,
})
files, err := NewWorkspaceSpecFiles(spec, forgeToken) // decided on the daemon
spawnReq.Workspace = &SessionWorkspace{TenantID: t, ProjectID: p, Key: "task-a1"}
ApplyWorkspaceSpec(spawnReq, files, signingAgentSocket)
// The executing host: WorkspaceManager.ProvisionSession(spawnReq.Workspace)
// makes <projects_root>/<t>/<p>/workspace/task-a1 and writes the spec beside it.
```

The **signing socket goes somewhere else**, and for a different reason. A
unix socket address is a fixed-size kernel field (`sun_path`, 108 bytes), and
a managed workspace spends 88 bytes on the tenant and project before a socket
name is appended, so a socket under the workspace does not fit on any
realistic projects root. It is bound under `orchestration.sockets_root`
instead — short, dedicated, created `0700` at startup, measured at startup
against the kernel's limit, and admitted by the mount policy **by
construction** rather than by an operator allowlist entry. That grant is the
narrowest one that works: the socket file itself, one directory below the
root, read-only, and actually a socket. See
[SDLC configuration](sdlc.md#configuration).

### The container workspace contract

**Nothing in the image makes the checkout.** The spawn clears the image's
entrypoint and runs the agent binary as the container's command
(`spawnContainerJSONRPC`), so an entrypoint that cloned would never run. The
`sdlc` runner's `initializing` step drives the clone instead, as git commands
exec'd into the container through `ContainerRuntime.Exec`
(`pkg/sdlc/checkout.go`) — from the same spec written at the path below, which
stays the one description of the checkout both sides act on.

| Path in the container | What it is |
|---|---|
| `/run/quicbaas/workspace` | the spec directory, mounted **read-only** |
| `/run/quicbaas/workspace/workspace.json` | the spec; `$QUICBAAS_WORKSPACE_SPEC` names this path |
| `/run/quicbaas/workspace/forge-token` | the forge token, mode `0400`; present only when the spec's `forge_token_file` names it |
| `/run/quicbaas/ssh-agent.sock` | the ephemeral, KMS-backed SSH signing agent, mounted **read-only**; `$SSH_AUTH_SOCK` names this path. The agent is started per attempt with its socket directory at `<orchestration.sockets_root>/<12 hex characters>` (`AgentOptions.SocketDir`, go-xkms `sdk/go/client/ssh`); the directory and the socket are removed when the attempt ends. The derived sockets root is per-daemon, not per-user — see [the signing socket’s root](sdlc.md#the-signing-sockets-root) |
| `/workspace/<project name>` | the project checkout the container makes |
| `/workspace/<context repo name>` | each context repository, beside it |

`workspace.json`:

```json
{
  "project": {
    "name": "go-truststrap",
    "repo_url": "https://forge.example/org/go-truststrap",
    "ref": "main"
  },
  "context_repos": [
    { "name": "handbook", "repo_url": "https://forge.example/org/handbook", "ref": "main" }
  ],
  "branch": { "name": "issue/601-dispatcher-autonomy", "base": "main", "create": true },
  "checkout_root": "/workspace",
  "forge_token_file": "forge-token"
}
```

**What the checkout does**, in order, every command inside the container:

1. Clone `project.repo_url` into `<checkout_root>/<project.name>` — skipped
   when the directory is already a repository, which is how a resumed attempt
   adopts the checkout its predecessor's container made.
2. Write `project.git.author` into that checkout, so a git the WORKER runs
   commits under the project's identity rather than the image's.
3. Fetch `branch.base`, and `branch.name` too when `branch.create` is false;
   then put `branch.name` on the checkout — cut from its base when it is new,
   continued from its remote when a prior attempt pushed it, so a second
   attempt keeps the pull request and its reviews.
4. Clone every entry of `context_repos` into `<checkout_root>/<name>` at its
   `ref`. They sit **beside** the project, never inside it, so nothing the
   worker reads can be committed by accident.

Authentication is the same credential-helper shape the publish steps use: the
helper names `forge_token_file` and reads it at the moment git asks, so the
value reaches no argument vector and no environment. Commits are signed through
`$SSH_AUTH_SOCK`; the signing key itself never enters the container, so a
compromised worker can ask for signatures while its container lives and holds
nothing afterwards.

Every command and its output is recorded on the job under the
`sdlc:workspace_checkout` audit event — a clone that could not reach the forge
is the most common way an attempt dies, and that record is where a person reads
why.

A session whose container has stopped or been removed is not a failed git
command. If the runtime refuses an exec because the container has stopped, or
cannot find the container, no git ran. The checkout and the pre-flight report
that as `SessionContainerGoneError`, which names the session, the container and
the runtime's own message. The evidence line starts with `!` and says the
container was gone, in place of the `$ git …` line for the command that never
ran. `CheckoutCommandError` is only for a git that ran and exited non-zero
(go-quicbaas#932). Both are workspace faults, so the task is parked and no
attempt is charged.

**What the image must carry**, since the runner runs these in it: `git`,
`ssh-keygen` (git's `gpg.format=ssh` signing invokes it), and whatever the
binding's verification commands need — for a Go project, `make`, the Go
toolchain and the `golangci-lint` / `gosec` / `govulncheck` that `make lint`,
`make check` and `make security` are made of.
`deployments/agents/software-factory-worker/` is the one this repository builds;
see [the worker image](sandbox.md#the-worker-image) and
[the worker image](sdlc.md#the-worker-image).

**No credential value ever reaches argv or the environment.** The two
environment variables the spawn sets — `QUICBAAS_WORKSPACE_SPEC` and
`SSH_AUTH_SOCK` — carry paths, not secrets.

**What is live today, plainly.** All five steps happen in the container, and
all five are live (#608, #627): the daemon creates the empty directory the
checkout is bind-mounted through, and clones, commits, signs and pushes nothing
itself. Every one of the module's git commands — the clone included — is an
argument vector the runner runs through `Exec` in the worker's own container,
and the signing agent's socket is mounted, so `git commit -S` reaches a key the
container never holds.

**Verification is the container's too** (#623). The binding's `verification`
commands run through the module's `container_command` validator: the
operator's `ExecutionGuard` admits a command on the daemon, where the
operator's configuration is, and the admitted command then runs inside the
worker's container, in the checkout that container made — the same `Exec`
seam the delivery's git uses. A project whose build needs the worker image's
toolchain verifies in the environment the work was done in, and a job with no
container is refused rather than verified on the daemon.

### Context repositories

```go
type ContextRepo struct {
    Name    string // one path segment: the directory it is cloned into
    RepoURL string
    Ref     string // branch, tag or commit
}
```

`ValidateContextRepos` refuses an empty name, `.`/`..`, a name containing a
path separator or NUL, an empty `repo_url` or `ref`, and two repositories
claiming one directory. `NewWorkspaceSpec` additionally refuses a context
repository that would occupy the project's own directory.

For go-truststrap the list is one entry — the company handbook:

```
context_repos:
  - name: handbook
    repo_url: https://forge.example/jeremyhahn/handbook
    ref: main
```

which the worker reads at `/workspace/handbook` while the project is at
`/workspace/go-truststrap`, exactly the `../handbook/...` layout the charter
already assumes.

**Declare every repository the work needs, not just the first one.** The list
is a list: a project whose `CLAUDE.md` names two repositories beside the
checkout — the handbook *and* the company repository — declares both, and both
are cloned. A repository the issues reference and the project does not declare
is a repository the worker cannot reach, and the only thing it can do about that
is spend a session proving it (go-quicbaas#908). Declaring it is what turns that
into a refusal the pre-flight makes for nothing.

### The pre-flight — the workspace is proved before anything is prompted

After the clone and **before the first prompt**, the runner proves the checkout
is the one the spec described (`pkg/sdlc/preflight.go`). Per repository — the
project and every declared context repository — three facts, each a git probe
whose exit status is the answer:

1. the directory is a git repository (`rev-parse --git-dir`);
2. its `origin` is the remote the spec named (`remote get-url origin`);
3. `HEAD` resolves to a commit (`rev-parse HEAD`).

And for the project checkout, that `HEAD` is on the planned branch
(`rev-parse --abbrev-ref HEAD`). The pinned-commit case is the cloner's
(`verifyPinnedHead`) and is not repeated.

It **writes nothing** — no clone, no fetch, no checkout — and every probe it ran
is recorded on the job under `sdlc:workspace_preflight`, passing or failing, so
nobody has to run them again by hand.

It is not redundant with the clone. The clone is idempotent by design, because a
resumed attempt is bind-mounted through the directory its predecessor cloned
into; every skip it takes is a fact it did not establish. Three faults reach a
worker otherwise:

| Fault | Why the clone misses it |
|---|---|
| an empty checkout | nothing failed; there was nothing to fail |
| a directory holding a different repository | `isRepository` answers yes, so the clone is skipped |
| a clone killed part-way | the directory is a repository and holds no object |

A refusal **names the repository** (`WorkspaceUnfitError`), parks the task with
`workspace_unfit` — refunded, held for an operator, released by a re-label after
the repair — and posts the cause to the issue. See
[Job admission](job-admission.md#operator-faults--a-factory-that-could-not-present-the-work).

The reviewer's checkout is pre-flighted the same way. There is no park there — a
review is not an attempt at a task — but a reviewer is never prompted on a tree
nobody intended, because a verdict about one is worse than no verdict: it is
submitted to the forge under a commit status.

### Running a command inside the container

`ContainerRuntime.Exec(ctx, containerID, *ExecSpec) (*ExecResult, error)` runs
one command in a container that is already running. It exists for the steps the
SYSTEM owns rather than the agent — a delivery's commit and push happen once,
with the identity the project configured, whatever the worker said that turn —
and it is the only channel into a live session that is not the agent's own
stdio pipe.

The engine reaches it through `agentServiceImpl.ExecSession`, which resolves
the container from the session's own record: `Session.ContainerID` is reported
by the backend that launched it (the ACP backend sets it from the interactive
process) and copied onto the live record, the durable `SessionRecord` and
`SpawnSessionResponse`, so it survives a resume. `JobRun.Exec` is the verb a
runner uses, beside `Spawn` and `Prompt`.

A non-zero exit is REPORTED on `ExecResult`, not raised: a caller frequently
wants it (`git rev-parse --verify` answers by exit status). An error means the
command could not be run at all. A session with no container is refused with
`*SessionNotContainerizedError` — never run on the daemon instead, which is the
whole thing the container exists to prevent.

`ExecSession` is deliberately NOT on the `AgentService` interface and no
transport exposes it: a remote caller able to name a session and a command
would have arbitrary code execution inside another tenant's sandbox. It is an
optional capability a caller type-asserts for (`SessionExecutor`), the same
shape `AttestingContainerRuntime` already uses in this package.

### The container a REMOTE daemon created

A session dispatched to a registered fleet node daemon is sandboxed by THAT daemon:
its runtime creates the container, from its own image map, under its own mount
policy. So the container's identity exists only on the worker host, and the
spawn response is the one place it can come home from.

`SpawnSessionResponse.container_id` carries it. The daemon fills it from the
session its backend returned (`pkg/fleet` `GRPCSpawnServer.Spawn`) and the
control plane reads it into the `Session` it builds
(`RemoteACPBackend.Spawn`), from where the orchestrator copies it onto the live
record, the durable record and its own `SpawnSessionResponse` — the same three
places a local spawn writes it to. While either end skipped the field, a
containerized remote worker was indistinguishable from a host process and the
SDLC runner refused every one of them as "runs no container".

The id belongs to the daemon's host. It addresses a container under the
container runtime the AGENT owns, which is not the control plane's whenever the
two are on different hosts — and `ExecSession` above uses the control plane's
own runtime unconditionally. Routing a command to the daemon that can run it
needs an exec seam the `FleetRuntime` service does not have yet; the session
record already names the host (`SessionRecord.HostAffinity`) for it to route
on. Until then, treat the id as an identifier the control plane RECORDS rather
than one it acts on.

### The backend a REMOTE daemon serves

A daemon registers ONE primary backend and advertises the whole set it serves.
The heartbeat reports the set (`FleetNodeObservation.Backends`), reconciliation
writes the first as the registration's `Backend` and all of them into
`Capabilities`, and `FleetNodeServesBackend` — the one predicate every backend
filter in the stack shares — admits the daemon for any of them. So one daemon
answers for claude, codex and opencode, and the scheduler is right to pick it
for all three.

The name on the WIRE is what the daemon runs. Every `FleetRuntime` request
carries a backend field, and the daemon looks its runtime's backend up by that
name — so the control plane's choice of what to put there IS the choice of
model family. `RemoteACPBackend` therefore serves exactly ONE backend name,
given at construction (`RemoteACPBackendDeps.Backend`) and stamped on Spawn,
Prompt, Infer and ListModels alike, and the `Session` it returns reports that
same field. The resolver's pool consequently caches per `(daemon host,
requested backend)`: the host is the dialable target, the backend is the name
on the wire, and both vary independently. Invalidation stays per host — a
daemon replaced at the same address drops every backend cached for it.

A registration asked for a backend it does not serve is REFUSED, with the
resolver's own `NoMatchingFleetNodeError` (constraint `backend`), stashed on the
request so the API answers `no_matching_agent` rather than falling through to
an in-process backend.

Before this, the pool cached one backend per host and the instance named the
registration's PRIMARY on every RPC. A session resolved for codex was served
by claude: the process ran, the session record said codex, and nothing
compared the two. The review module's `require_independent_reviewer` is the
reason this is a correctness property rather than a label — it refuses an
approval from the model family that wrote the code, and it checks the
CONFIGURED reviewer, so the gate was satisfiable in configuration while being
violated in fact (go-quicbaas#937).


### What this replaced

The daemon-side worktree and its "companion" copy are gone: `sdlc.companions`
and `sdlc.worktree_root` are refused by name at load, the `.factory/`
companion directory inside every worktree is gone, and `GitProvisioner`
no longer adds a worktree on the SDLC path. Pre-v1, there is no migration:
name the repositories under the project's `context_repos` instead.

The daemon-side commit and push are gone with them: `sdlc.Workspace` and
`service.SDLCWorkspace` are deleted, and the module's git is an argument
vector run through `Exec` in the worker's own container.
