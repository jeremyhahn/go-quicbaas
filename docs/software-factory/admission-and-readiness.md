# Admission and readiness — three inputs to start, four predicates to be ready

> **Status: NORMATIVE** · Last verified: 2026-09-12
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

### The three inputs

Concurrency used to be bounded **per project only**. Ten autonomous projects
each permitted three concurrent jobs will run thirty of them on a fleet with
eight worker slots, and every per-project check will have been satisfied.
Admission therefore reads three inputs and decides once:

| Input | Where it comes from | Zero / absent means |
|---|---|---|
| the project's own quota | `Project.Quotas.MaxConcurrency` | unlimited for that project |
| fleet worker capacity | the daemons' reported slots, and/or `orchestration.fleet.max_concurrent_jobs` | the axis is left out — **not** "no slots" |
| provider usage headroom | `Account.Selectable` + `QuotaAuthority.Admit` over the project's pooled accounts | the axis is left out (nothing is pooled) |

The project quota is a **ceiling**: no redistribution below ever lifts a project
above what its operator set.

The provider axis is **subscription usage, never a price.** The owner's ruling is
subscription-based accounts only, so no turn has a cost, `ConsumptionEntry.CostMicros`
has no production writer, and a money axis summing zeroes would admit forever
while looking like a cap. What is read is the sliding token window and the
`AvailableAt` cooldown a provider refusal wrote — the same two authorities the
spawn path consults, asked one step earlier. A project whose whole pool is spent
is **deferred**, with a `RetryAt`, rather than spawning an agent whose first turn
the provider will refuse.

### Fleet capacity: two sources, one answer

`pkg/orchestration/fleet_capacity.go` reduces two honest sources to one:

- **the fleet itself** — every daemon reports `max_concurrent_jobs` on every
  heartbeat (`pkg/agent` `Config.MaxConcurrentJobs` → `AgentObservation` →
  `AgentRegistration.MaxConcurrentJobs`), and the capacity is the sum over the
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
| `AdmissionController.Plan` | reads all three inputs once per sweep and divides the fleet; both sweeps consult the one plan |
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
# the agent daemon, per host
max_concurrent_jobs: 4       # 0 = this host reports no count (default)
```

## Readiness

Eligibility used to be one test: the issue is open and carries the binding's
trigger label. `ReadinessEvaluator.Evaluate` now applies four predicates in a
fixed order — cheapest first, so an issue refused for a reason readable from its
own text costs no forge round trip:

| Predicate | Verdict when it fails |
|---|---|
| open, and carrying the trigger label | skip |
| acceptance criteria present (opt-in) | refuse |
| not already owned by a live session | skip |
| declared dependencies satisfied | park, or refuse |

`ReadinessPolicy` lives on `ProjectBinding.Readiness`; the zero value is the
shipped default. Dependency satisfaction and live-session ownership are **facts**
and are on. Acceptance criteria are a **judgement** about an issue's text, so that
predicate is opt-in: a project whose issues do not use the heading convention
would otherwise have every issue refused, which is worse than an under-specified
issue.

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

## See also

- [autonomy-and-dispatch.md](autonomy-and-dispatch.md) — the autonomy switch and the two sweeps
- [job-admission.md](job-admission.md) — job leases, stall detection, supersession
- [cost-enforcement.md](cost-enforcement.md) — the consumption ledger and account quotas
- [sdlc.md](sdlc.md) — the delivery runner and the park the pre-flight uses
