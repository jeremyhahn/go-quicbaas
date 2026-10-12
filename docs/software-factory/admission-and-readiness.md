# Admission and readiness — four inputs to start, five predicates to be ready

> **Status: NORMATIVE** · Last verified: 2026-09-24
> Supplement to the Software Factory feature folder: what bounds how much work
> runs at once, and what an issue must satisfy before it becomes a Task.
> Conflicts with this document are defects.

Two gates stand between a forge issue and a running agent:

- **readiness** (`pkg/sdlc/readiness.go`) decides whether an issue may become a
  Task at all;
- **admission** (`pkg/orchestration/admission.go`) decides how many of a
  project's Tasks may hold a job right now.

Before 2026-09-12 readiness was one predicate and admission was one input. Both
failures were observed on the burn-in fleet the same day.

## Admission

### The four inputs

Concurrency used to be bounded **per project only**. Ten autonomous projects
each permitted three concurrent jobs will run thirty of them on a fleet with
eight worker slots, and every per-project check will have been satisfied.
Admission therefore reads four inputs and decides once:

| Input | Where it comes from | Zero / absent means |
|---|---|---|
| the project's own quota | `Project.Quotas.MaxConcurrency` | unlimited for that project |
| fleet worker capacity | the daemons' reported slots, and/or `orchestration.fleet.max_concurrent_jobs` | the axis is left out — **not** "no slots" |
| provider usage headroom | `Account.Selectable` + `QuotaAuthority.Admit` over the project's pooled accounts | the axis is left out (nothing is pooled) |
| the project's and tenant's own spend limits | `Project.Spend` + the tenant's `TenantSpendPolicy`, judged by `QuotaAuthority.AdmitScope` over the project and tenant quota planes | unmetered (no limits set) |

The project quota is a **ceiling**: no redistribution below ever lifts a project
above what its operator set.

### The project quota bounds worker-bearing states only

The owner ruled on go-quicbaas#978 (2026-09-22) that `Project.Quotas.MaxConcurrency`
bounds **running workers**. It does not bound open jobs. For the sdlc runner the states that spend a slot are
`initializing`, `executing`, `validating`, `re-delegating` and `completing`.
`reviewing`, `cleanup` and `escalating` hold no slot. A job in `reviewing` is
waiting on a CI pipeline and a reviewer, often for 30 to 90 minutes, with no worker
turn running. Before this ruling it held its slot for that whole wait. On the burn-in fleet two
such jobs held both of a project's slots, and the next ready task sat
`pending` while no worker ran anywhere.

| Piece | What it does |
|---|---|
| `JobStateSpec.OffProjectBudget` | the runner declares which of its states hold no worker (`sdlc.RunnerStates`) |
| `JobRun.SetState` → `moveBudget` | on every transition between the two kinds, moves the job's lease off or back onto the budget (`JobLeaseStore.Rebudget`) |
| `projectCapacityJobLeaseStore.Rebudget` | admits a move **back onto** the budget against the slots other jobs hold. A move off the budget is never refused |
| `JobLease.AwaitingSlot` | marks a refused return as queued demand |

**A round waits and does not fail.** Suppose a review or a red pipeline asks for another round and the
project is full. The job stays in `reviewing` and its lease is marked
`AwaitingSlot`. The move is retried on the heartbeat cadence until a slot
frees, and then the job proceeds to `re-delegating`. The wait ends only when the run
ends: a cancellation, a supersession, or the stall detector, whose `reviewing`
bound is unchanged. A refusal for any other reason is returned, because
entering a worker state with no decided admission is the oversubscription the
quota exists to prevent.

**A queued round is ahead of new work.** `CountsAgainstBudget`, the one
predicate the capacity store, the dispatcher, the task-source poller and the
plan all read, counts a lease that holds a slot **and** a lease awaiting one.
Without that, a busy project's dispatcher refills every slot the moment it
frees and the round waits for ever. Queued rounds do not count against **each
other**: a returning job is compared with `HoldsSlot`, the slots actually
held, so the first waiter to ask after a slot frees takes it.

**The lease itself never moves.** It is held from the first state to the
conclusion. One job per task, the reaper and the heartbeat are unchanged. A
job resumed into `reviewing` takes its lease off the budget from the start, so
a project that filled while the daemon was down does not refuse it.

### A job waiting in reviewing holds no worker on any axis

The owner ruled on go-quicbaas#938 (2026-09-24) that a job in `reviewing` closes
its worker container, frees its slot, and stops counting against fleet
capacity (go-quicbaas#1036).

| Piece | What it does |
|---|---|
| `sdlc` `releaseWorker` | once the branch is pushed, closes the worker's session and container on entry to `reviewing`. The workspace stays: it is keyed per attempt |
| `JobLease.Dormant` | set with `OffBudget` whenever the job is in a state its runner declares off the budget. It means that no container and no model session are running |
| `JobLease.ClaimsWorker` | what the fleet and backend checks, and the admission plan's fleet count, count against new work: a worker held, or a dormant job queued for one |
| `JobLease.HoldsWorker` | what a waking job is counted against, the counterpart of `HoldsSlot` |
| fleet and backend `Rebudget` | admit a dormant job **waking**. Going dormant is never refused |

A review round that asks for changes goes through `re-delegating` and wakes the
lease. Waking is admitted on all three axes, and a refusal on any of them makes
the round wait (`AwaitingSlot`). It does not fail. Then `executing` finds no
session and returns through `initializing`. That path spawns a new
container on the same branch and workspace, the same path a replaced session
takes. The new session follows the project's `session_rotation`: on `resume`
(the default) its first prompt carries `worker_resumed_note` with the released
session's conversation, read before the release closed it; on `fresh` it
carries `worker_released_note`.
`Hooks.Prepare` keeps the attempt's `Delivery` whole on such a respawn.

The fleet axis counts a review or triage job, which is off the project budget
for its whole life but runs a container while it does (owner ruling,
2026-09-25). The admission plan's fleet count (`countFleetInflight`) and the
fleet check both read `ClaimsWorker`, so the plan never hands out a slot the
check then refuses. Only a dormant job is off it. The backend axis counts both
kinds for the same reason. A job
resumed into `reviewing` after a restart acquires its lease dormant.

Because the sdlc worker holds no session in `reviewing`, the stall detector
reads each job's **own** runner declaration (`runnerSessionBackedIn`) rather
than the merged one. The workflow runner's `reviewing` stays session-backed.
The sdlc runner's is not, so a slow review is reported and never ended.

The provider axis reads each pooled account's own sliding windows (tokens and
list-price dollars, see
[../accounts/architecture.md](../accounts/architecture.md#spend-limits)) and the `AvailableAt`
cooldown a provider refusal wrote — the same two authorities the spawn path
consults, asked one step earlier. A project whose whole pool is spent is
**deferred**, with a `RetryAt`, rather than spawning an agent whose first turn
the provider will refuse.

The spend axis (`admission_spend.go`, go-quicbaas#1058 ruling F6) reads the
project's own `Spend` windows and its tenant's. A spent one **defers** the
project with axis `project_spend` or `tenant_spend`, the window's usage in the
reason, and `RetryAt` when the window's oldest turn ages out. The tenant is
asked first, because its limits stop every project it owns. The deferral is on
the audit trail (`orchestration_project:spend_deferred`, resource
`{project}/{axis}/{token_limit|cost_limit}/{window}`) when it starts or changes,
not on every sweep. A limit that cannot be read fails the plan, never admits.
A deployment that declares no fleet capacity and pools no accounts still plans
when this axis is wired.

### Fleet capacity: two sources, one answer

`pkg/orchestration/fleet_capacity.go` reduces two honest sources to one:

- **the fleet itself** — every daemon reports `max_concurrent_jobs` on every
  heartbeat (`pkg/fleet` `Config.MaxConcurrentJobs` → `FleetNodeObservation` →
  `FleetNode.MaxConcurrentJobs`), and the capacity is the sum over the
  daemons alive at that instant. It shrinks when a host is drained and grows when
  one is added, with nothing to keep in step by hand;
- **the operator** — `orchestration.fleet.max_concurrent_jobs`, a ceiling below
  what the hardware could serve.

When both answer, the **tighter one binds**. The rule lives in
`NewFleetCapacity` and nowhere else.

**An unmeasured value is not zero.** A fleet nobody could measure reads as "this
axis has nothing to say", never as "there are no slots" — the second reading
refuses every job in the deployment while wearing the costume of a safety bound.
Concretely, the fleet sum is reported as known only when **every** live daemon
reported a count: one daemon short makes the sum a lower bound on reality, and
enforcing a lower bound refuses work the fleet would have served. An empty
registry is likewise undeclared, not zero.

A daemon stops contributing when it is disabled, reported offline or errored, has
never heartbeated, or has been silent for longer than
`orchestration.heartbeat_timeout` (default 90s).

The count is a **declaration, not a local limiter**: the daemon does not refuse a
spawn past its own number — admission does. One authority per bound.

### The fairness property

A fleet-wide cap handed out first-come would let one busy project hold every slot
indefinitely, which is strictly worse than the per-project behaviour it replaces.
The division is **max-min fair with a least-served residual**, and it guarantees:

1. **Safety.** The shares handed out sum to at most the free fleet slots, and no
   project's share exceeds its own unused quota. So the jobs running after a
   sweep are at most the fleet capacity, however many projects are each inside
   their own quota.
2. **Max-min fairness.** A project asking for no more than an equal share of the
   free slots receives everything it asked for. Capacity nobody wants is
   redistributed to those that do, and no project's share is reduced to satisfy
   another asking for more than its equal share.
3. **No starvation.** When free slots are fewer than the projects asking, the
   indivisible residual goes to the projects with the fewest live jobs,
   least-recently-started first. Taking a slot makes a project the most recently
   started, putting it behind every other claimant next sweep — so every asking
   project is served within **⌈P/F⌉ sweeps** of P projects sharing F free slots.
   A busy project holds the whole fleet only while nobody else is asking, and
   yields on the first sweep after somebody does.

### Where it is enforced

| Where | What it does |
|---|---|
| `AdmissionController.Plan` | reads all four inputs once per sweep and divides the fleet; both sweeps consult the one plan |
| `TaskSourcePoller.sweepProject` | adopts at most `AdoptionSlots()` new specs, in priority order |
| `TaskDispatcher.sweepProject` | starts at most `DispatchSlots()` queued Tasks, in priority order |
| `projectCapacityJobLeaseStore.Acquire` | refuses a lease over the **project's** quota |
| `fleetCapacityJobLeaseStore.Acquire` | refuses a lease over the **fleet's** capacity |

The two lease decorators are the last-ditch checks at the one point every job
passes through: they catch what no sweep sees — a job started directly, an
auto-spawned task, a second node planning from its own reading. They read the
same `FleetCapacityReader` the plan reads, so the sweep and the lease cannot bound
the fleet by different numbers.

Refusals are typed and name the axis, because they send an operator to different
numbers: `ProjectCapacityExceededError` means raise a project quota,
`FleetCapacityExceededError` means add a worker or raise the configured ceiling.

**Fail closed, and never with a number.** Every read failure returns an error and
no plan; a sweep that could not compute a plan ends rather than degrading to the
project axis. A plan reporting "unbounded" because it could not count leases would
be the oversubscription this exists to prevent, wearing the costume of a
successful measurement. The one exception is stated above: an *undeclared*
capacity is an answer and admits, while a capacity read that *failed* is not.

### Configuration

```yaml
orchestration:
  fleet:
    max_concurrent_jobs: 8   # 0 = leave the operator's ceiling out (default)
  heartbeat_timeout: 90s     # a daemon silent longer than this stops counting
```

```yaml
# the fleet node daemon, per host
max_concurrent_jobs: 4       # 0 = this host reports no count (default)
```

## Readiness

Eligibility used to be one test: the issue is open and carries the binding's
trigger label. `ReadinessEvaluator.Evaluate` now applies five predicates in a
fixed order — cheapest first, so an issue refused for a reason readable from its
own text costs no forge round trip:

| Predicate | Verdict when it fails |
|---|---|
| open, and carrying the trigger label | skip |
| acceptance criteria present (opt-in) | refuse |
| not already owned by a live session | skip |
| not already closed by a pull request | skip |
| declared dependencies satisfied | park, or refuse |

`ReadinessPolicy` lives on `ProjectBinding.Readiness`; the zero value is the
shipped default. Dependency satisfaction and live-session ownership are **facts**
and are on. Acceptance criteria are a **judgement** about an issue's text, so that
predicate is opt-in: a project whose issues do not use the heading convention
would otherwise have every issue refused, which is worse than an under-specified
issue.

### Issues a pull request already closes

An issue that a pull request says it closes is not adopted (go-quicbaas#1054).
On 2026-09-22 pull request #1049 said `Closes #1048`, #1048 was adopted anyway,
and its worker opened #1052, a duplicate of the change already under review.

Two sources establish the claim:

- **the pull request's text.** The source lists the repository's open pull
  requests once per sweep and reads the closing keywords in each one's title and
  body: `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`,
  `resolves`, `resolved`, followed by `#<n>`, `owner/repo#<n>`, or the issue's
  URL. Only the bound repository counts. A URL must also be on the bound forge:
  the repository's web address is read from the forge's own issue URLs, so
  `https://github.com/acme/api/issues/25` never claims Gitea's `acme/api#25`
  (`pkg/sdlc/closing_refs.go`).
- **the forge's own link.** A forge implementing `IssueLinkReader` is asked
  which pull requests it links to the issue as closing it. Gitea answers from
  the issue timeline: a `pull_ref` entry whose `ref_action` is `closes`. The read
  is per issue, so it runs only for an issue that nothing else has refused or
  claimed (`pkg/sdlc/forge_links.go`, `forge_gitea_links.go`).

A skipped issue is recorded once on the platform audit trail as
`sdlc:adoption_skipped`, naming the issue and the pull request.

Only an **open** pull request holds its issue. Once the pull request is no
longer open, the issue is offered again on the next sweep. That holds whether
it closed without merging (nobody is delivering it) or merged. A merged change
is history: an issue still open and still labelled after its change merged has
been reopened or re-triggered because that fix was incomplete or reverted. The
forge's link and the pull request's text both outlive the merge, and holding
the issue for them would mean it is never adopted again. A listing or a link
read the forge cannot answer adopts nothing from that project on that sweep
(`pkg/sdlc/issue_source_claims.go`).

### Declared dependencies

An issue declares what it waits for in its own text, in the body or in a comment,
line-leading and case-insensitive:

```
Blocked by: jeremyhahn/go-qrdb#123, #456
Requires release: jeremyhahn/go-qrdb@v0.9.5-alpha
```

**Merged is not released**, and that is why there are two directives rather than
one. `Blocked by #123` is satisfied when #123 closes — right for work in the same
repository, and wrong for a consumer that pins a version: module versions resolve
from **tags**, so the fix can be merged and the issue closed while every build
still selects the old code. `Requires release` is checked by asking the forge
whether the tag exists (`ForgeBackend.GetTag`), which is exactly the consumer's
own question. An issue needing both states both. Folding them into one directive
would mean guessing which the author meant, and the cheap guess — closed is
enough — is the one that unblocks work too early.

### Parking, not waiting

An upstream wait is **unbounded**: nobody can say when another repository's issue
will close. A readiness gate that held a session while waiting would hold it for
days. So a dependency-blocked issue is adopted **as a parked Task** — created
blocked in one write (`TaskSourceSpec.Park` → `CreateTaskRequest.Park`), holding
no session, no container and no lease — and resumes through the machinery that
already exists: a typed `UnblockCondition`, re-examined by `UnblockSweeper` on
every pass of the unblock sweep — at boot and every
`sdlc.unblock_sweep_interval` (default `10m`), on the same backstop goroutine
as the triage backstop and the review floor — and readmitted through the
engine's readmission verb when it clears, so the issue's `factory:blocked`
label comes off with it. The `AutoSpawn` bit survives the park deliberately: it
is what makes readmission dispatch the Task.

A park whose reason carries **no** `unblock[kind=…]` condition — an operator
repair, an owner ruling, a needs-info question — is never readmitted by the
sweep, and a condition the sweep cannot decide (an owner ruling) is left where
it is. Those return to the queue only through a person's `readmit`; the sweep
keeps the checkable ones from needing one.

One write, not two. Create-then-block leaves the Task pending and auto-spawning
for exactly long enough for a sweep to start the job the park exists to prevent.

This is the same park `#914`'s workspace pre-flight uses for an unfit workspace,
through the same `ComposeBlockedReason` vocabulary. Composition, not a second
mechanism.

### Cycles and depth

A-blocks-B-blocks-A undetected is an infinite spawn loop — the most expensive
failure available here. The dependency walk therefore carries **three bounds**:

- a **visited set**, so a cycle is detected and the issue is **refused**
  (`TaskBlockKindNeedsInfo` — a person must edit an issue; nothing external will
  ever clear it);
- a **depth bound** (`ReadinessPolicy.MaxDependencyDepth`, default 8), refused
  rather than truncated: a silently truncated walk reports "satisfied" for a
  chain it never finished reading;
- a **node bound**, so one malformed graph cannot spend a sweep's whole budget.

A diamond is not a cycle and is walked once per node.

## Verification

The properties proved, with the tests that prove them:

| Property | Test |
|---|---|
| N projects each within their quota cannot exceed the fleet in aggregate | `TestPlan_ProjectsWithinTheirOwnQuotaCannotExceedFleetCapacity`, `TestFleetCapacity_ProjectsInsideTheirOwnQuotaCannotExceedTheFleet` |
| the division is max-min fair and starvation-free | `TestPlan_FairnessIsMaxMin`, `TestPlan_EveryDemandingProjectIsServedWithinCeilOfProjectsOverSlots`, `TestFairShares_NeverOversubscribesForAnyShape` |
| admission defers rather than spawning with no usage headroom | `TestPlan_DefersWhenNoAccountHasUsageHeadroom`, `TestTaskDispatcher_DefersAProjectWhoseProviderPoolIsSpent` |
| an unmeasured fleet is not a fleet with no slots | `TestAgentFleetCapacity_ADaemonThatReportedNothingLeavesTheAxisUndeclared`, `TestAgentFleetCapacity_AnEmptyRegistryIsUndeclaredNotZero` |
| an unreadable input fails closed | `TestPlan_FailsClosedOnEveryUnreadableInput`, `TestFleetCapacity_AnUnreadableCapacityRefuses` |
| a dependency cycle is refused, not looped | `TestReadiness_ADependencyCycleIsRefusedNotLooped`, `TestReadiness_ALongerCycleIsAlsoRefused` |
| a blocked task parks without holding a session, and resumes | `TestTaskSourcePoller_ASpecCarryingAParkAdoptsBlockedAndSpawnsNothing`, `TestSweepProject_ReleasesAPublishedTag` |
| merged-but-unreleased does not satisfy a version pin | `TestReadiness_MergedIsNotReleased`, `TestSweepProject_AnAbsentTagIsWaitingNotUndecidable` |
| an issue owned by a live session is not adopted twice | `TestReadiness_AnIssueOwnedByALiveSessionIsNotAdoptedTwice` |
| an issue an open pull request closes is skipped, audited once, and offered again when the pull request closes unmerged | `TestIssueSource_AnIssueAnOpenPullRequestClosesIsNotAdopted`, `TestIssueSource_AnIssueTheForgeLinksToAnOpenPullRequestIsNotAdopted`, `TestIssueSource_AReopenedIssueWhosePullRequestMergedIsAdopted`, `TestIssueSource_AReopenedIssueTheForgeLinksToAMergedPullRequestIsAdopted`, `TestClosingIssueNumbers_AURLCountsOnlyOnTheBoundForge` |
| a job in reviewing frees its slot, and its round waits for one | `TestJobRun_AReviewingJobFreesItsSlotAndItsRoundWaitsForOne`, `TestProjectCapacity_AJobOffTheBudgetAdmitsASecondTask` |
| a job in reviewing is off the fleet and backend axes, and its round waits for a worker | `TestFleetCapacity_NDormantJobsAtCapacityNStillAdmitNewWork`, `TestBackendCapacity_ADormantJobHoldsNoBackend`, `TestJobRun_AReviewingJobIsDormantAndItsRoundWaitsForTheFleet` |
| the worker's container is closed for the review and respawned for a round | `TestRunner_ReviewingHoldsNoWorkerAndIsNotDeclaredSessionBacked`, `TestRunner_ChangesRequestedRunsAnotherRound`, `TestRunner_AWorkerWhoseWorkDidNotReachTheForgeIsNotReleased` |
| queued rounds do not block each other, and a waiting round ends with its run | `TestProjectCapacity_QueuedRoundsDoNotBlockEachOther`, `TestJobRun_AWaitingRoundEndsWithItsRun` |
| only the worker-bearing sdlc states spend the budget | `TestRunner_OnlyTheWorkerBearingStatesSpendTheProjectBudget` |

## See also

- [autonomy-and-dispatch.md](../orchestration/autonomy-and-dispatch.md) — the autonomy switch and the two sweeps
- [../accounts/architecture.md](../accounts/architecture.md#job-admission) — job leases, stall detection, supersession
- [../accounts/architecture.md](../accounts/architecture.md#the-consumption-ledger) — the consumption ledger and account quotas
- [../sdlc/architecture.md](../sdlc/architecture.md) — the delivery runner and the park the pre-flight uses
