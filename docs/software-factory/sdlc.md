# The SDLC module — a labelled issue becomes a merged pull request

> **Status: NORMATIVE** · Last verified: 2026-09-12
> Supplement to the Software Factory feature folder. The engine is
> [architecture.md](architecture.md); this page is the git-delivery module
> that plugs into it. Conflicts with this document are defects.

`pkg/sdlc` is the **SDLC module of the Software Factory**: git-based delivery
automation against a forge (Gitea today; GitHub and GitLab are adapters to
add). It turns a labelled forge issue into an orchestration Task, lets the
engine run each attempt as a Job with one worker Session, publishes the result
as a pull request, waits for an independent review and green checks on the
current head, and merges.

The module owns **no envelope**. Tenant → Project → Job → Task → Session is
`pkg/orchestration`'s model; the durable job lease, the heartbeat, the attempt
budget, the reaper, the stall detector, the recovery and the blocked/readmit
lifecycle are all the engine's. Everything visible — Projects, Tasks, Jobs,
Sessions — is the engine's UI, CLI and API. The module adds two small records
and one REST resource on each.

What it does own is the **shape** of an issue-driven job: the `sdlc`
`JobRunner`, registered into the engine's runner seam, which drives one issue
through one branch with one worker session. A delivery pipeline is not a
Workflow — a Workflow is a lead agent decomposing work into a DAG — and the
seam that makes that distinction real is
[job-runners.md](job-runners.md).

Where the rest of the module is documented:

| Subject | Where |
|---|---|
| Every verb and flag of `quicbaas sdlc` | [cli.md](cli.md#quicbaas-sdlc) |
| REST, gRPC, MCP, Go and TypeScript SDK, permissions | [api.md](api.md#the-sdlc-module) |
| Exported surface, typed errors and their status mapping | [engineering.md](engineering.md#the-sdlc-module) |
| Binding a project and following one issue end to end | [user.md](user.md#bind-a-project-and-follow-one-issue) |
| The prompts the worker and the reviewer are given | [prompt-sets.md](prompt-sets.md) |


## The split

| `pkg/orchestration` (generic, built once) | `pkg/sdlc` (git, the module) |
|---|---|
| durable Job lease, heartbeat, supersession | forge adapters and SDLC strategies (`forge.go`, `forge_gitea.go`, `strategy.go`) |
| attempt budget, environment-fault refund, `RetryNotBefore` | issue source (`issue_source.go`), webhooks (`webhook*.go`) |
| Task `blocked` / `needs_info` + readmit | signed commit and push, run INSIDE the worker's container (`publish.go` through `ContainerRuntime.Exec`) |
| Task↔Job binding, autonomy, the task dispatcher | pull request, head-SHA review gate, checks, merge (`gate.go`, `forge_review.go`, `hooks.go`) |
| dead-job reaper, stall detection | park reason posted to the issue (`blocked.go`) |
| consumption ledger, per-account spend cap and circuit breaker | — |
| `Task.Source` provenance, `Priority` | forge-token custody and rotation (`token_*.go`) |
| `TaskSource`, validator registry, `JobRunner` registry, `JobLifecycleHooks` | the `sdlc` runner (`runner.go`), dependency ranking, readmission vocabulary, the worker charter |
| tenant-scoped workflows, job events, Job↔Session links | its two records: `ProjectBinding`, `Delivery` |

The module runs no spend accounting of its own. Every turn it prompts goes
through `JobRun.Prompt`, which attaches the job and task the turn belongs to,
and the engine records it against both the advisory counters and the
consumption ledger the spend cap is decided from. A worker whose account is
over its cap is refused its credential before its session is spawned, and the
refusal comes back as an environment fault the module's own `blocked.go`
renders onto the issue. See
[cost-enforcement.md](cost-enforcement.md).

Nothing in the left column names git. Nothing in the right column has a
loop, a lease, a reaper, a work item or a project record. `pkg/orchestration`
never imports `pkg/sdlc` — `pkg/sdlc/import_graph_test.go` walks the
engine's import graph and refuses the module in it.

## The seams

The module registers one implementation into each of the engine's seams
from `pkg/server/lifecycle_sdlc.go` at boot. The registries are
process-global and append-only, so what is registered is a thin forwarder
that reads the live runtime through an atomic pointer; a stopped module
answers every seam with `sdlc.ErrUnavailable` — except `container_command`,
which needs nothing from the runtime (it runs the criterion through the job's
own container) and is registered directly. The engine resumes the jobs
a node owned when it last stopped only after every module has registered
(`Server.recoverOrchestrationJobs`, after `startSDLC`): a resumed job runs
its `prepare` step at once, and a step whose module has not started would
fail the attempt.

REGISTRATION HAPPENS BEFORE THE ENGINE STARTS, the runtime after it
(`Server.Start` runs `registerSDLCModule` → `startOrchestration` →
`startSDLC`). Registering first is what lets `startOrchestration` read a
complete runner registry and what lets `recoverOrchestrationJobs` resume a
job whose runner would otherwise be unknown. `registerSDLCModule` needs no
runtime, so it runs first; `startSDLC` attaches what the runner drives with.
(`SetIdleThresholds` does not yet validate against the registry — see
[job-runners.md](job-runners.md) and issue #621.)

1. **`TaskSource` — `forge_issue`** (`issue_source.go`). The engine's
   task-source poller (`orchestration.sweep_interval`) asks the source, per
   **autonomous** project (`Project.Autonomous`, which `Bind` sets), what work
   it holds, and adopts at most the project's remaining capacity per sweep.
   The answer is every
   OPEN issue of the bound repository carrying the binding's
   `trigger_label` — compared exactly, case-sensitively — as a
   `TaskSourceSpec{Source{Kind: forge_issue, Ref: owner/repo#n, URL},
   Subject, Description (the issue and its discussion), Priority (blast
   radius × 10 + the binding's default), Runner: "sdlc", Metadata}`. Dedupe is not the
   source's: the task store's guarded source index refuses a second adoption
   of one `(kind, ref)`, so the same spec on every sweep creates one Task. A
   verified webhook delivery nudges the poller for that project
   (`TaskSourcePoller.SweepProject`); it is the same path, sooner.

2. **Validator — `forge_review`** (`forge_review.go`, `gate.go`). The
   reviewing-phase criterion the `sdlc` runner names. It reads the forge's
   reviews and check runs on the pull request's CURRENT head, records them
   on the `Delivery` with one guarded write per round, and answers through
   the pure gate `EvaluateReviewGate`: approvals on the head from reviewers
   whose model family differs from the worker's, no request-changes, every
   check finished green. A decision only time can change (approvals not yet
   given, checks running) is waited for on the workflow's `poll_interval`,
   and an `sdlc` job has no workflow, so it is always
   `DefaultReviewPollInterval` (30s); the job sits in `reviewing` until a
   person or a pipeline decides. A refusal — changes requested, a red that
   stayed red past the settle window — fails the criterion with the findings,
   and the runner re-delegates to the worker (`re-delegating → executing`)
   with the findings in job memory. The binding's `max_review_rounds` rounds
   exhausted → `escalating` → the Blocked hook.

   **A settled red is classified before anyone is woken.** `pkg/cifault` reads
   each failed check's log and answers `genuine`, `void` (a runner died, a
   status write raced shutdown — re-run it, bounded, and wake nobody) or
   `host_defect` (registry, container runtime, module resolution — park for an
   operator). Only `genuine` re-delegates. See
   [ci-failure-classification.md](ci-failure-classification.md).
   **The round bound terminates, and that is the point of it.** `nextRound`
   answers `escalating` on every call once the bound is crossed, so a run can
   never re-enter review after the park — it parks and escalates exactly once,
   consults the review gate exactly `max_review_rounds + 1` times, and ends
   `failed`. An unbounded review cycle is the most expensive failure mode an
   autonomous loop has, so the termination is asserted directly (worker turns
   and forge review reads counted) rather than inferred from the label.

3. **Validator — `container_command`** (`verify.go`). The validating-phase
   criterion the `sdlc` runner names, one per entry of the binding's
   `verification` list. A binding's verification commands are a tenant
   repository's own build and tests, so they run WHERE THE WORK IS: the
   operator's `ExecutionGuard` admits (or refuses) each command on the
   daemon — the allowlist is daemon configuration and stays there — and an
   admitted command is then executed inside the worker's container through
   `JobContext.Container`, in the checkout the container's own entrypoint
   made. It is the seam `publish.go` uses for the delivery's git
   (`ContainerRuntime.Exec` reached through the session), narrowed to one
   directory so a criterion cannot name a daemon path. The exit status is
   the verdict; both output streams are the evidence the failing round sends
   back to the worker. A job with no containerized session is refused
   (`VerificationNotContainerizedError`) rather than verified on the daemon
   — there is no second path.

4. **`JobRunner` — `sdlc`** (`runner.go`). The shape of an issue-driven job.
   An issue is not a DAG: there is nothing to decompose, because the issue IS
   the unit of work, and nothing for a lead to paraphrase, because the issue
   text is the contract. So the runner declares no `decomposing`,
   `scheduling` or `assigning` state, spawns exactly ONE persistent worker
   session, and prompts it with `Task.Description` — the issue contract —
   VERBATIM.

   | state | what happens |
   |---|---|
   | `initializing` | `Prepare` plans the branch, writes the `Delivery` and labels the issue; the ONE worker session is spawned in the binding's `worker.image` with the workspace spec and the forge token mounted read-only. The runner then clones the project and its `context_repos` INSIDE that container, through the runtime's exec seam (`checkout.go`), and puts the planned branch on the checkout; the daemon makes only the directory the checkout is mounted through. A **pre-flight** then proves that checkout (`preflight.go`) before anything is prompted: an unfit workspace parks the task for an operator at zero token cost — see [Autonomous projects](autonomy-and-dispatch.md#the-pre-flight--the-workspace-is-proved-before-anything-is-prompted). |
   | `executing` | the worker is prompted with the issue contract verbatim, plus the previous round's findings from job memory and, when the last attempt changed nothing, that attempt's turn. The answer is recorded on the job as an `sdlc:agent_turn` event whatever it was, and what the run does next is decided by whether the checkout moved — see [the worker's turn](#the-workers-turn). |
   | `validating` | reached ONLY when the turn changed something. The binding's `verification` commands, in order, through the module's `container_command` validator: the operator's `ExecutionGuard` admits each command on the DAEMON, and the command then runs INSIDE the worker's container, in the checkout its entrypoint made (`ContainerRuntime.Exec`, the same seam the delivery's git uses). Nothing verifies on the daemon; a job with no container is refused. A failure goes back to `executing` with the command's output — both streams — and counts a round. |
   | `reviewing` | `Reviewable` publishes; then the `forge_review` gate. `request_changes` → `re-delegating`; a plan needing no pull request (the trunk-based carve-out) goes straight to `completing`. |
   | `re-delegating` | round++ up to `max_review_rounds`, then `escalating`. |
   | `completing` | `Complete` merges, bound to the reviewed head; the strategy's `PostMerge` steps run through the forge adapter (gitflow's hotfix back-merge). A pull request that [conflicts with its base](#a-merge-conflict-is-a-rebase-round) (`MergeConflictError`) goes back to `executing` as a rebase round; it counts a round, and it is not a failure of the work. |
   | `cleanup` / `escalating` | the session is closed and the container goes; on a non-completed exit the branch is published FIRST so nothing the worker did is lost, and `Blocked` posts the reason to the issue. |
   | ANY OTHER EXIT | a refusal after the spawn, a checkout or verification that would not run, a cancellation or a panic runs the SAME teardown, deferred (`abandonWorker` → `teardownWorker`). It publishes, closes and discards exactly as a planned exit does, and the close is issued on a context the run's own cancellation cannot reach — bounded by the discard stage timeout. Without it a refused attempt left its container running and its daemon session record behind. |

   **`executing` and `reviewing` are declared session-backed**
   (`JobStateSpec.SessionBacked`), and nothing else is. The declaration says
   which states the stall detector may CONCLUDE about: a job past its bound
   in one of them with no addressable session has lost the worker it is
   waiting on and is ended; a job in any other state is only reported. The
   one worker session outlives more states than that — it is spawned in
   `initializing` and closed in `cleanup`, in `escalating`, or on the
   deferred exit path any other ending takes — so "the run happens to hold a
   session here" is not the test.

   `reviewing` is declared because the wait in it never touches the worker:
   the container commits, signs and pushes, and the daemon then polls the
   forge for a verdict that may never arrive, so a worker that died after the
   push is noticed by nothing else — and a review that cannot take another
   round (`re-delegating → executing`, on the SAME session) is a job to end.
   `validating` is NOT declared even though its commands run in the
   container: the state's own work IS the exec, so a worker that has gone
   fails the pipeline rather than leaving the run waiting. `initializing`,
   `re-delegating`, `completing` and `cleanup` are the daemon's own work and
   each finishes correctly whether or not a worker is still there.

   The engine's `workflow` runner declares `reviewing` session-backed too and
   `mergeJobStateSpec` takes the disjunction, so the effective value is
   unchanged — but a declaration that is only true because another runner
   made it would go silently false the day that runner changed
   (go-quicbaas#668).

   **Recovery is answered per state** (`JobResumer`, `runnerResumeStates`),
   and what decides each answer is what is already true on the forge — a
   resumed run holds no worker session, because the container died with the
   daemon.

   | interrupted in | resumes at | why |
   |---|---|---|
   | `initializing`, `executing`, `validating`, `re-delegating` | `initializing` | nothing reached the forge and the worker's edits went with its container; `Prepare` continues the branch an earlier attempt pushed, and the round and its findings come back from the persisted iteration and job memory |
   | `reviewing` | `reviewing`, or `initializing` when the `Delivery` records no head | a recorded head is the push that already happened, so the resumed run does the daemon's own half — refresh the pull request, ask the gate — rather than re-cloning and re-executing. No head means the push never happened |
   | `completing` | `completing` | `Complete` is bound to the approved head and idempotent |
   | `cleanup` | `cleanup` | the merge and the strategy's post-merge steps are done by the time the state is persisted; what remains needs no session |
   | `escalating` | `escalating` | the escalation FINISHES rather than re-runs |

   `escalating` is issue #626. It used to fall into a catch-all and restart
   the whole pipeline — a new worker session, a new prompt, a refreshed pull
   request, a re-labelled issue — on work parked precisely so a person would
   look at it. It does **not** map to `failed` either, the way the engine's
   `workflow` runner can: `resumeJob` never launches a run for a terminal
   answer, and this escalation still owes the issue its park reason. So it
   resumes at `escalating`, where `escalate` restores the reason from the
   audit trail (`sdlc:escalated`, recorded by the run that decided the park —
   `JobStatus` carries no reason of its own), posts `Blocked` only if the
   issue does not already carry this attempt's park marker, tears the worker
   down and returns `failed`.

   A gate that asks for another round on a resumed run needs a worker it does
   not hold; `executing` sends it to `initializing` for one rather than
   failing on a session that died with the daemon.

   The runner calls the module's lifecycle moments DIRECTLY through
   `job.RunStage(ctx, JobStagePrepare, …)` rather than through the engine's
   state→moment table: it gets the operator's stage timeout, `JobStatus.stage`
   and the stage event, and it can run `Reviewable` again on each review
   round, which the table cannot express. There is no registered `sdlc`
   `JobLifecycleHooks` set.

5. **The lifecycle moments** (`hooks.go`). They are the FORGE half and only
   that: the module holds no git port at all. The git half is `publish.go`,
   an argument vector the runner runs in the worker's container through
   `orchestration.ContainerRuntime.Exec` — see [the publish](#the-publish).

   | moment | what the module does |
   |---|---|
   | `Prepare` (initializing) | the attempt's branch is planned (`issue/<n>-<slug>` from the binding's strategy, based on `Project.Git.DefaultBranch`), the `Delivery` records it, the issue is labelled `in_progress`. A later attempt continues a branch an earlier one pushed. **Nothing is materialised on the daemon's filesystem** and the returned work directory is empty: the worker's container clones the project and the project's `context_repos` itself — see [Autonomous projects](autonomy-and-dispatch.md). |
   | `Reviewable` (reviewing) | the pull request opened (or refreshed on a later round) against the head the CONTAINER already published, recorded on the `Delivery` and published as Task artifacts (`url` `pull_request`, `commit` `head`), issue labelled `in_review`. No head recorded → `NoPublishedHeadError`; the container publishes before the forge is written. |
   | `Complete` (completing) | merge as the binding's strategy plans, **bound to the head the review approved**: the forge refuses a merge whose head moved (`HeadMovedError`), retried only while the forge says not-ready (`merge_attempts`), issue labelled `merged`. When the retries are spent, one more read of the pull request decides between `MergeNotReadyExhaustedError` and `MergeConflictError`; see [a merge conflict is a rebase round](#a-merge-conflict-is-a-rebase-round). A merge the forge never answered is settled by reading the pull request back — see [Forge transport](#forge-transport-deadlines-retries-and-unanswered-writes). |
   | `Discard` (cleanup, detached context) | no filesystem work — the container that made the checkout is gone with it; the `Delivery` stays as the record of the attempt. The runner publishes the branch before the container ends on every non-completed exit. |
   | `Blocked` (escalating) | the park reason posted to the issue (secrets redacted, bounded), derived from the park's [typed unblock condition](#the-block-lifecycle) plus the attempt accounting and what resumes it, retried per `surfacing`; issue labelled `blocked` / `needs_info` / `escalated`, **and the delivery's open pull request labelled with the same one**. |

   The pull-request label is not decoration. The branch is published on every
   non-completed exit, so a park leaves an OPEN pull request behind; unlabelled,
   it reads exactly like a change waiting for review, and the next reader either
   waits for a verdict nobody will ask for or reviews and merges a change the
   engine already refused. It is ADD-ONLY: a pull request never carried the
   module's trigger label and may carry a person's own, so there is nothing of
   the module's to move and a removal would destroy somebody's annotation.
   `AddIssueLabels` is a no-op on a label already present, which is what makes
   a resumed escalation's re-run idempotent. A merged or closed pull request is
   left alone — the label is a statement about an open change.

   A hook error fails the job with `FailureClass: hook`; the engine's attempt
   accounting decides what happens to the Task.

   #### A merge conflict is a rebase round

   Gitea (measured on 1.27.1) does not say whether a pull request conflicts
   with its base or is still being checked. The merge endpoint answers
   `405 "Please try again later"` for every status except mergeable
   (`checking`, `conflict`, `error`), and the API's `mergeable` field is
   false for all of them. Time is what tells them apart: a check settles in
   seconds, and a conflict never does. So `Hooks.merge` keeps its bounded
   not-ready retry (`merge_attempts`, doubling from 2s, about a minute by
   default). Once the retries are spent, it reads the pull request one more
   time:

   | final read | answer |
   |---|---|
   | open, on the approved head, `mergeable: false` | `MergeConflictError` (`ErrMergeConflict`): the forge's settled verdict |
   | `mergeable: true` but still refused, or closed | `MergeNotReadyExhaustedError`, as before |
   | head moved | `HeadMovedError` |
   | merged meanwhile | success; `Complete` records it |

   The runner treats a `MergeConflictError` as a **round of the work** on the
   same branch and pull request. It is not a hook failure and it is not an
   environment fault. The `merge_conflict` prompt (the base, the approved
   head, rebase or merge the latest base, keep both sides' intent, re-run the
   build and tests) goes into job memory the way review findings do, and
   `nextRound` spends a round from `max_review_rounds`. A rebased branch no
   longer descends from the head it published, so a plain push would be
   refused as non-fast-forward. From the rebase round on, every publication
   of the attempt's branch goes through the existing `Force` path as
   `--force-with-lease=<branch>:<published head>`, on every workspace kind:
   the end-of-turn push on an ephemeral workspace and the review push on a
   persistent one. The lease is pinned to the head the delivery records, not
   to the tracking ref. The worker's own `git fetch` refreshes the tracking
   ref, so an implicit lease would accept a remote that moved in the
   meantime. It is never an unleased force. The new head then goes through
   validation and the review gate again, because an approval only covers the
   head it was given. A run that holds no worker (for example, a
   re-dispatched job that went straight from its open pull request to the
   gate) goes through `initializing` to get one. A pull request that
   conflicts again after every rebase uses up the budget and escalates to a
   person, like a review that never converges. Each conflict is recorded on
   the job as an `sdlc:merge_conflict` event. Measured on go-truststrap#64
   (2026-09-22) and fixed in go-quicbaas#1007.

6. **`TaskConclusionObserver` — the task's own ending** (`outcome.go`). The
   moments above belong to a JOB. A TASK's conclusion is decided after the run
   has returned (`orchestration.applyConclusion`) and, when the node that held
   the run is gone, on the reaper's goroutine — so a task that spends its
   attempt budget stops with no moment firing at all, and before
   go-quicbaas#788 wrote nothing to the forge. The observer is the seam for
   that: it is handed the concluded `*Task` and a reason from
   `orchestration.TaskConclusionReasons()`, and posts one comment, moves the
   issue's label and records an `outcome_comment` artifact on the Task. It may
   NOT change the outcome — the task is already durable — so a failure to
   report is logged and nothing else. Every conclusion reason has a row in one
   table, asserted against the engine's vocabulary; see
   [Outcomes](#outcomes-every-way-a-task-stops-and-what-reaches-the-issue).

### The block lifecycle

A park says **what would unblock it, in a form something can check.** Prose
has one failure mode and it is total: nothing can evaluate it, so nothing
re-examines the task, and the condition clears without the task moving.

`pkg/sdlc/readmission.go` is the vocabulary. A condition is a value — a kind
from a closed set, a subject, a target — encoded as a marker inside
`Task.BlockedReason` (`… unblock[kind=…;subject=…;target=…]`). It rides the
reason field rather than a record of its own because the engine writes a
task's status and its reason in ONE update; a separate record would
reintroduce the window whose symptom is a task blocked with no stated
condition.

| kind | subject / target | decided by |
|---|---|---|
| `pipeline_green` | subject: head SHA | a check — the forge's checks on that commit, evaluated by the same `evaluateChecks` the merge gate uses, so "green" cannot mean something weaker here |
| `forge_issue_state` | subject: `owner/repo#n`, target: `open` \| `closed` | a check — `GetIssue`, in this repository or another on the same forge |
| `owner_ruling` | — | **the owner.** Never machine-satisfiable: the convention reserves these calls for a person, so clearing it automatically would answer exactly the class of question the factory is forbidden to answer |
| `operator_repair` | — | **an operator** — whoever runs the fleet, not whoever decides the architecture |

Every kind is decided by EXACTLY ONE of a check or a named person. A kind
with neither is refused at `Validate`, because a permissive default would
read downstream as "nothing to check", which is indistinguishable from a
condition that has been met.

`UnblockSweeper.SweepProject` (`pkg/sdlc/unblock_sweep.go`) re-examines every
blocked task in a project and readmits the ones whose condition holds,
through the engine's own readmission verb — `orchestration.AgentService`,
never the task store underneath it. Only the service's verb ANNOUNCES the
readmission, and that announcement (`TaskConcludedReadmitted`, whose outcome
row carries no label) is what takes `factory:blocked` back off the issue; a
sweep wired to the store makes the same transition in silence and strands the
label, so the construction refuses the store by name (go-quicbaas#842). It is
**fail-closed in every direction that is not a proven satisfaction** — one verdict per task, and
only one of them moves anything:

| verdict | what it means | the task |
|---|---|---|
| `released` | a check ran and answered yes | readmitted |
| `waiting` | a check ran and answered no | stays blocked |
| `awaiting_human` | the condition names a person; the finding says who | stays blocked |
| `undecidable` | the check could not run — forge outage, a subject naming nothing | stays blocked |
| `awaiting_diagnosis` | no condition was recorded; the evidence is quoted | stays blocked |
| `unreadable` | a condition is recorded and cannot be used | stays blocked |
| `budget_exhausted` | the condition holds and the attempt budget does not | stays blocked |

`awaiting_diagnosis` is a **recognised class, not a defect report.** The
engine blocks a reaped death precisely BECAUSE nothing diagnosed it, so such
a task genuinely has no checkable condition; attaching a kind would fabricate
a diagnosis nobody made. What changed is that the task is now re-examined and
reported on every pass instead of sitting where nothing looks.

The sweeper is **callable, not yet scheduled**: nothing in the daemon ticks
it today. Where its cadence comes from is go-quicbaas#780.

### The publish

The container commits, signs and pushes; the daemon opens the pull request.
Neither half can do the other's.

`pkg/sdlc/publish.go` builds the argument vectors and runs each one through
`JobRun.Exec` into the worker's session container, in the directory the
CONTAINER calls its checkout (`/workspace/<project name>`, recorded on the
`Delivery`):

| step | command |
|---|---|
| is there anything uncommitted | `git status --porcelain` |
| stage and commit | `git add --all`, then `git -c user.name=… -c user.email=… -c gpg.format=ssh -c user.signingkey=key::<public half> -c commit.gpgsign=true commit -m …` |
| is there anything to publish | `git rev-list --count origin/<base>..HEAD` |
| publish | `git [-c credential.helper=…] push origin <branch>[:<target>]` |
| what landed | `git rev-parse HEAD` |

Three properties the argument vectors carry deliberately:

- **Signing is stated, never inherited.** A worker image whose global git
  config sets `commit.gpgsign` would otherwise decide for the project. With no
  signing agent the commit says `commit.gpgsign=false` explicitly.
- **The key never travels.** `user.signingkey=key::<line>` is the PUBLIC half
  by value; the private half stays in the KMS and the container reaches it
  only through the agent socket at `$SSH_AUTH_SOCK`
  (`sdlc.SigningAgent` → `service.SDLCSigningAgent`, one ephemeral session per
  attempt, its directory created and removed by the session). The delivery
  runner asks for `SigningScopeDelivery`, which serves **two** keys: the commit
  signing key and the **access key** the push and the clone authenticate with
  (`ProjectGit.AccessKey`, frequently the same key). Readers — the reviewer and
  triage — ask for `SigningScopeCheckout` and get the access key alone; see
  [The forge-access seam](#the-forge-access-seam). The socket is
  bound under `orchestration.sockets_root` — not the project workspace, which
  a 107-byte unix socket path does not fit under — measured at startup and
  admitted by the mount policy by construction; see
  [The signing socket’s root](#the-signing-sockets-root).
- **The forge token never travels either.** For an `https://` remote the push
  carries a credential helper that READS the mounted token file when git asks;
  for `ssh://` there is no helper at all, because the agent socket already
  authenticates it. No credential value reaches argv, the environment or a log
  line. `PublishCommandError` and `CheckoutCommandError` keep a failed
  command's output VERBATIM on a field, and their message carries a **scrubbed,
  bounded tail** of it: the last 4 KiB, through the same `redactSecrets` a
  published reason goes through. The rule that an error string reaches every
  log and every wrapped chain is kept; what is not kept is the blindness — the
  message used to say only how many bytes it was withholding, and
  go-quicbaas#751 is what that cost (`git clone … exited 128 (102 bytes of
  output withheld)` withheld `sh: ssh: not found`, the entire diagnosis).

### The forge-access seam

`forgeAccess` (`pkg/sdlc/workspace.go`) is the **one** way any of the module's
three runners gets a container to the forge. A runner states the project, the
binding's forge, the branch plan, the host mount and whether its container signs
commits; the seam resolves the token, starts the ssh agent when one is needed,
**checks the credentials against the spec**, materializes the workspace and puts
every mount and every path on the spawn request.

| runner | scope | what the agent serves |
| --- | --- | --- |
| delivery | `SigningScopeDelivery` | commit signing key **and** access key; lives as long as the container, because the worker pushes at the end of every turn |
| reviewer | `SigningScopeCheckout` | access key only; stopped as soon as the checkout is made |
| triage | `SigningScopeCheckout` | access key only; stopped as soon as the checkout is made |

An https-only checkout starts no agent at all — the token authenticates it.

**A credential the spawn does not hold is refused before the container starts.**
`orchestration.RequireCheckoutCredentials` reads the required credential off each
repository's transport (ssh → agent socket, https → token file, anything else →
none) and answers `CheckoutCredentialMissingError` naming the repository, the
remote and what is missing. That is the earliest-decidable workspace fault there
is, and go-quicbaas#929 is what not deciding it cost: triage held a token and no
agent while the project's remote was ssh, and spent one container per sweep —
every fifteen minutes, indefinitely — learning that inside `git clone`.

It reaches the runner as `ErrWorkspaceUnfit`, so a delivery attempt **parks** for
an operator with the fault attributed to the factory (`workspace_unfit`) rather
than failing: a missing credential is a configuration, and failing would spend
the task's whole attempt budget on the identical refusal.

The seam exists because three runners assembling their own spawns is how that
divergence happened in the first place. A test asserts that every call which
acquires or mounts a forge credential appears in that one file, so a fourth
runner gets the seam or gets nothing.

Nothing changed and nothing ahead of base → `NoChangesError`. A worker session
that carries no container is refused at the SPAWN
(`WorkerNotContainerizedError`), before a turn is spent on work nothing could
publish.

6. **No canonical Workflow.** The owner's ruling (plan of 2026-09-06): the
   SDLC pipeline is not a Workflow on the engine's control loop, whose one
   shape is lead-driven decomposition — a live run turned one issue into
   nine work items. The `JobRunner` seam and the `sdlc` runner above are the
   answer, and they have landed. The module registers **no workflow at all**,
   `Bind` neither reads nor sets `Project.DefaultWorkflow`, and no `sdlc`
   `JobLifecycleHooks` set is registered — the runner calls the moments
   itself. The engine's side of the seam is
   [job-runners.md](job-runners.md).

## The worker's turn

Every prompt handed to the worker and every answer it gave is recorded on the
job as an `sdlc:agent_turn` event. The event carries the attempt, the review
round, how long the turn took, a census of the content blocks the backend
published (tool use included, where the backend publishes it), the stop
reason, the token accounting, **the classification** (`outcome=`) when the
turn changed nothing, and **the reply itself** — truncated at
`TurnReplyLimit` (4096 bytes) with the cut marked, never dropped.

**The cap applies where the reply is WRITTEN, never before it is
classified.** The stop declaration sits at the end of the reply, so capping
at read time would cut it off any reply longer than the cap — and the turn
would then be read as a worker claiming work it never did, charged, and
re-prompted, on exactly the workers with most to say. `ReadAgentTurn` parses
the declaration from the whole text into `StopCode`/`StopDeclared`, the
classification reads those fields rather than the text, and `Capped()` is
what the event and the durable record carry. `NewAgentTurn` is the only way
a turn's reply is set, so no caller can build one whose declaration nothing
read. The turn is
classified before its event is written, so one event answers both "what did
the worker do" and "what did the factory conclude"; a turn that never got as
far as being classified — a prompt that errored, a tree that could not be
read — is still recorded, without an outcome it did not earn. It is
written on the failure path too: a turn whose text is discarded leaves nobody
able to say whether the worker asked a question, cited a rule it may not
break, or died (go-quicbaas#787).

### A turn that changed nothing is a verdict, not a failure

When a turn ends with the checkout untouched, the binding's verification
commands are **not run**, and the runner says so with an
`sdlc:verification_skipped` event. There is nothing to build, test or review.

"Untouched" is measured PER TURN, against a baseline the runner reads before
the worker is prompted: nothing uncommitted AND a `HEAD` that has not moved
off that baseline. It is deliberately not "the branch is ahead of its base".
Round one commits and pushes, so from round two onward the branch is
permanently ahead — and a re-spawned container for a resumed attempt checks
out the published head — so reading the branch's position would classify
every later turn as productive. The follow-up worker that cannot proceed and
correctly declares a stop would have its declaration dropped, and the stale
head it never touched would be sent to verification and review: the same
silent loop, one turn later. `AheadOfBase` keeps the two jobs it is right
for — whether there is anything to publish, and whether an ending attempt
has commits worth pushing before its container goes. A checkout whose `HEAD`
cannot be read ends the turn before it is dispatched, because there is no
honest classification to make without a baseline.

What the turn WAS is then classified, **from structure and never from prose**.
The charter requires a worker that stops without finishing to end its reply
with a declaration on its own last line:

```
SDLC-STOP: <code>
```

| code | outcome | block kind | unblock condition | what happens |
|---|---|---|---|---|
| `question` | `question` | `needs_info` | `owner_ruling` | the worker's own text is the issue comment |
| `owner-decision` | `owner_ruling` | `needs_info` | `owner_ruling` | the worker cited a rule or reached a call the owner alone makes |
| `environment` | `environment` | `blocked` | `operator_repair` | the workspace or tooling made the work impossible |
| a code this binary does not know | `unreadable` | `blocked` | `operator_repair` | the worker stated it stopped and only the reason is unreadable, so nothing guesses |
| no declaration, no text at all | `silent` | `escalated` | `operator_repair` | the turn is taken ONCE more with the silence stated, then escalated naming it |
| no declaration, no tool use, back inside `NonAnswerTurnBound` | `non_answer` | — | — | the prompt is sent once more with one line saying the reply missed; see below |
| no declaration, text present | `claimed_work` | — | — | `NoChangesError`: the worker reported work on a tree it did not touch. The one no-change turn that is still a failure of the work |

**A reply that never read the prompt is not a verdict on anything.** A
persistent worker session can hold a turn's worth of its own — a background
task armed two turns earlier, a notification that expired while it worked —
and answer THAT when the next prompt arrives. The turn is recognised from the
two numbers its `sdlc:agent_turn` event already carries and nothing else: it
declared no stop, used no tool, touched no file, the backend reported
`end_turn`, and it came back inside `NonAnswerTurnBound` (5s, no working turn
is under it; an unmeasured turn does not qualify). Measured on
go-quicbaas#62 (2026-09-22): a review round answered in 451ms with one text
block and no tool use, charged as a failure of the work — the task lost its
job, its pull-request adoption state, a review round and an attempt to it.
Such a turn is now RE-PROMPTED ONCE — the same prompt, rebuilt with its
contract and the round's findings intact, plus one line saying the previous
reply did not address it — and the re-prompt is audited as
`sdlc:non_answer_reprompted`. That event is the only trace of it, because the
re-prompt is neither a new round nor a new attempt and the `Delivery` is not
written: a turn that judged nothing tells the next attempt nothing.
`MaxNonAnswerTurns` is one, so a second consecutive non-answer is a session
not reading prompts at all and takes the `claimed_work` path above; any turn
that was something else ends the streak.

The park reason is composed with `ComposeBlockedReason`, so it carries the
[typed unblock condition](#the-block-lifecycle) the `UnblockSweeper`
reads as well as the sentence a person reads. Neither kind used here is
machine-checkable, and that is deliberate: a worker's stop is settled by a
person by construction, and a factory that could clear it automatically
would be answering the class of question the SDLC convention forbids it to
answer. The worker's reply is carried as the condition's DETAIL, never
appended after the marker — the marker is found with `LastIndex`, so text
after it would let a worker declare the condition its own task is parked
under; an `unblock[` sequence in a reply is defused with a space and the
text is kept.

The park reaches the task through `JobRun.Park`, which the engine's attempt
accounting honours ahead of the reaper's death, ahead of the fault classes
and ahead of the budget (`applyConclusion`), concluding the task as
`runner_parked`.

**It is recorded the moment the judgement is made, before the `escalating`
state is persisted.** `JobRun.SetState` snapshots and persists a state before
the handler for it runs, so a park recorded inside the escalating handler
leaves a durable record saying "escalating" with no park on it — and a crash
in that window hands the reaper a record it reads as an undiagnosed death.
`runnerRun.park` therefore calls `JobRun.Park` itself and only then returns
the state, and `JobRun.restore` hydrates it back from the persisted status
when a daemon resumes — `snapshot()` rebuilds `JobStatus.Park` from the run
every time, so a resumed run that did not restore it would publish a status
with none and erase the record the recovery exists to preserve — and **every** park goes through it, the exhausted review
included. That ending (`nextRound` past `max_review_rounds`) is the common
one in production, and it used to write its own audit event and escalation
reason and return the state directly, which is exactly the shape the window
exploits.

**It is on the persisted `JobStatus`, not only on the live run**, and the
order above is why. A daemon that dies after writing the failed status but
before applying the conclusion leaves the lease reaper to rebuild it from the
record alone — and the reaper stamps everything it concludes `abandoned`
(`abandonedJobStatus`). A park held in memory would be gone, the task would
read as an undiagnosed node failure, and it would be requeued: #787's loop
surviving a restart. `JobStatus.Park` carries the judgement across the crash,
`RecordJobDeath` reads it off that record, and `applyConclusion` tests it
before the death — a diagnosis made before the node died is not un-made by
the node dying. That conclusion
is [`OutcomeSilent`](#why-runner_parked-is-silent-when-the-parked_-three-are-not):
the `Blocked` moment has already posted this park, with the worker's own text,
under the marker the conclusion seam would use, so a second comment would be
two comments claiming to be one. Without it a parked job reads to the accounting as an
ordinary failed attempt and the next one is dispatched — which is how three
attempts at one issue ran the same prompt and spent a whole budget on an
unanswered question. The attempt that ran is still charged: a turn that
reached the model made a judgement about the work, which is what the budget
counts. What changed is that the task PARKS instead of being re-dispatched,
so the rest of the budget is spent on the attempt that follows the answer.

### An identical prompt is never sent twice

A fresh attempt renders the same issue contract as the last one, so a
no-change turn is carried forward on `Delivery.LastTurn` and rendered into
the next attempt's prompt: what the previous worker said, that it changed no
file, and how its stop was classified. That is genuinely new input — evidence
the previous worker could not have had.

Behind it is a guard: the prompt's SHA-256 digest is recorded with the turn,
and a run that composes a prompt whose digest matches one that already
produced no changes refuses to dispatch it and parks with that as the stated
reason. The note is the mechanism; the digest is what makes "the same prompt"
a decidable question rather than a judgement.

## The two records

Both reference orchestration ids and never duplicate a Project or a Task.
They live in the tenant's own store through the qrdb DAO (`store.go`), keyed
by a digest of the id they reference, so a read is one key and the tenant
appears in no key.

- **`ProjectBinding{ProjectID; Forge{Type, BaseURL, Owner, Repo, Labels,
  TriggerLabel, TokenSecretRef, WebhookSecretRef, SSHHostKey}; Strategy;
  Worker{Backend, Model}; Verification []string;
  Review{RequiredApprovals, RequireIndependentReviewer, RequireGreenChecks,
  ChecksSettleWindow, Reviewers}; MaxReviewRounds; Ranking}`** — one per
  bound Project: its autonomous-development configuration. No name,
  workspace, repository URL, author or signing policy: those are `Project`
  fields
  (`Project.Git.RepoURL`, `.DefaultBranch`, `.Author`, `.Signing`,
  `Project.DefaultWorkflow`). `Labels` maps the module's keys
  (`in_progress`, `in_review`, `merged`, `blocked`, `needs_info`,
  `escalated`) to forge labels; labels are DISPLAY OUTPUT, never a lock.

- **`Delivery{TaskID, ProjectID, Attempt, Branch, Base, Target,
  RequiresPullRequest, DeleteAfterMerge, HeadSHA, WorkDir,
  PullRequest{Number, URL, State, Title, Base}, Reviews
  []ReviewRecord{Reviewer, ModelFamily, Verdict, HeadSHA, ExternalID, Round,
  Findings, SubmittedAt}, Checks []CheckRecord{Name, HeadSHA, Status,
  ForgeRunID, FinishedAt}, SupersededRuns []SupersessionRecord, Version}`** —
  one per Task, rewritten by each
  attempt's `Prepare` (carrying over the pull request when the branch
  continues) and updated under `Version` afterwards. `Version` is the only
  concurrency token: a write that finds it moved is refused
  (`VersionConflictError`) and the writer re-reads.

### Superseded CI runs

`Checks` holds the LATEST head only — it is replaced whole on every head move
— so before `SupersededRuns` existed the previous head's runs were simply
dropped, and the only record of a run still burning a runner was the run
itself, on the forge, indistinguishable from a live one.

Head pinning already stops the engine ACTING on a displaced result: the gate
counts only checks and reviews bound to the delivery's head, and `Complete`
refuses on `HeadMovedError`. A displaced run is therefore waste, not
incorrectness — but it is still VISIBLE, and on Gitea **re-running it cancels
the run on the real head**. That cost two lanes a full CI cycle each on
2026-09-12, because nothing said the run was obsolete.

So every displacement is recorded, with what the engine was able to do about
it (`supersession.go`):

| disposition | meaning |
|---|---|
| `finished` | the run had already completed. It holds nothing, and is recorded anyway — a completed run on a stale head is exactly the one a reader finds, believes and re-runs. |
| `cancelled` | the forge stopped it and its capacity is back. |
| `orphaned` | it is still in flight and nothing will stop it: the forge publishes no cancel route (Gitea), the run carries no addressable id, or the cancel was refused. |

- **Two producers, one helper.** `runnerRun.publishWork` records at the push,
  which is where the engine's OWN rounds displace CI — round two's commits move
  the head while round one's pipeline is very likely still running.
  `ReviewValidator.round` records the other case, a head that moved on the
  forge ahead of the delivery's. Both call `supersedeChecks` and fold through
  `Delivery.RecordSupersession`, which deduplicates by
  `(displaced head, displacing head, context, run id)`, so a displacement seen
  by both is recorded once and the version-conflict retry converges.
- **The cancel is attempted, never assumed.** This is what
  `ForgeCapabilities.CancelCheckRuns` was declared for: a platform with a
  cancel route gets one, and Gitea's `false` produces an orphan record rather
  than a claimed cancellation. A cancel that FAILS is recorded as an orphan
  too — it is never returned, because a review round must not fail on a
  courtesy call the forge refused.
- **The guard applies to the engine.** `disposeRun` refuses to cancel a run id
  `Delivery.SupersededRun` already knows as displaced. A forge that numbers
  runs per repository rather than per commit hands the same id back on a later
  head, and cancelling it again is the 2026-09-12 failure committed by the
  engine on every round instead of by a person once.
- **Bounded and surfaced.** The list keeps the newest `MaxSupersededRuns` (32),
  because it rides on every guarded write of the `Delivery`. Orphans — and only
  orphans — are named on the park comment (`Orphaned CI runs (n, do not
  re-run …)`), since a finished or cancelled displacement is bookkeeping for
  whoever goes looking while an orphan is a runner nobody freed.
- **On every transport.** `superseded_runs` is on the SDK's `sdlc.Delivery` and
  on `pb.Delivery`, so `quicbaas sdlc delivery <task-id>` and
  `client.SDLC().Delivery(...)` carry it over REST, gRPC, QUIC, MCP, Unix and
  embedded alike. REST serialises the domain record directly, so a gRPC mapper
  that dropped the field would leave two transports disagreeing about whether a
  displaced run exists — and the caller on the wrong one re-runs it.

One system-plane record routes webhooks: `__sys:sdlc/webhookroute/<project>
→ tenant`, written by a guarded create at bind time, because a delivery
carries no tenant and the URL must not publish the tenancy layout.

## The SDLC strategy

An SDLC is ONE decision, not two. A project names a `Strategy` and that
strategy owns the whole shape of a change: the branch and where it is cut
from, whether it is reviewed, how it lands, and what follows it.

```go
type Strategy interface {
    Name() string
    PlanBranch(IssueRef) (BranchPlan, error)      // branch, base, target, PR?, delete?
    PlanMerge(PullRequestRef) (MergeSpec, error)  // method, commit title/message
    PostMerge(MergedRef) ([]Step, error)          // back-merges and other follow-ups
}
```

Splitting branching from merging — which is what this replaced — let a
project select gitflow branching with a squash merge, a combination no SDLC
describes and one that discards the merge commit gitflow's history depends
on.

**Every `BranchPlan` field is honoured.** `Base` and `Target` may differ.
An empty `Base` or `Target` means *the project's default branch*: the
project supplies the default, the strategy reads it through
`BranchPlan.Resolve`, and nothing overwrites a ref a strategy did name.
`RequiresPullRequest` selects the reviewed path or the direct-push carve-out;
`DeleteAfterMerge` is the one branch-lifecycle decision, made when the branch
is cut and copied into `MergeSpec.ExpectSourceDeleted` by `Complete`. All
four are recorded on the `Delivery` at `Prepare`, so the merge reads the
decision made when the branch was cut rather than re-deriving it from labels
that may have moved since.

| Strategy | Branch | Base → Target | Lands as | After |
|---|---|---|---|---|
| `github-flow` | `issue/<n>-<slug>` | default → default | squash, PR required | — |
| `trunk-based` | `issue/<n>-<slug>` | default → default | squash, PR required **except** the carved-out change classes (`docs`, `ci`), which push straight to the trunk | — |
| `gitflow` (feature) | `feature/<n>-<slug>` | `develop` → `develop` | merge commit | — |
| `gitflow` (`hotfix` label) | `hotfix/<n>-<slug>` | default → default | merge commit | back-merge the release line into `develop` |

`github-flow` is the company's own (handbook
`software/conventions/sdlc.md` §2), and `trunk-based` adds the direct-push
carve-out that section permits. Label matching ignores case and surrounding
space: a change class is a label people type on a forge, and refusing `Docs`
because the strategy was configured with `docs` would send a docs change
through a review nobody asked for. `NewTrunkBased(labels...)` builds a
trunk-based strategy with a different carve-out for a build that wants one —
registered under its own name, never mutating the one other projects already
selected.

Gitflow's **release branches** — cutting `release/<version>` off `develop`,
tagging the release merge and the double back-merge behind it — are not
implemented and are deliberately not registered as half a strategy
(issue #597).

**Registration is fail-closed.** Only complete strategies are registered; a
name nothing registered is a typed `StrategyNotRegisteredError` naming what
IS available, refused at `Bind` before any branch exists. A project silently
run under a strategy it did not choose pushes to the wrong ref and lands
history the wrong way, and neither is recoverable by retrying.

## Bind-time refusals

`service.SDLCService.Bind` refuses, with a typed reason, a project the
pipeline could not deliver: the Project must exist; `Project.Git.RepoURL`
must be on a host in `sdlc.repo_hosts` (or `allow_local_repos`);
`Project.Git.Signing` must be enabled and name a key — the module signs
every commit it pushes and never degrades to unsigned;
the token secret must resolve, and the webhook secret when named; and the
binding must name a registered strategy and a registered forge adapter.
Every refusal leaves no record behind.

Those checks all run BEFORE anything is written. The one refusal that does
leave a record is a **provisioning** failure: the route index and the binding
are stored before the forge is told to deliver, so a forge that cannot be
written to leaves a project bound but not driven. That is the recoverable
half of the two — `conformance` names exactly what is missing and `onboard`
puts it right — where the other order would leave a repository delivering to
a node that has no record of it. See
[Onboarding a repository](#onboarding-a-repository).

A successful `Bind` **turns the project autonomous**: it sets
`Project.Autonomous = true` and persists it, because a project bound to a
forge and a strategy and left un-adopting is a configuration that does
nothing. It neither reads nor sets `Project.DefaultWorkflow` — that field is
the lead-driven `workflow` runner's, and an `sdlc` job has no workflow.
`Unbind` leaves `Autonomous` where it is: turning it off is the operator's
call, on the project.

## The lifecycle of one issue

```
label task:ready ─► poller/webhook ─► Task{Source: forge_issue owner/repo#n, Runner: sdlc, AutoSpawn}
                                        │ dispatcher → Job{TaskID, Attempt: 1}, runner sdlc
   initializing ── Prepare ────────────► branch issue/n-slug planned, Delivery, label in_progress
                                        │ ONE worker Session, sandboxed; its container clones
                                        │ the project + context_repos and checks out the branch
   executing ──────────────────────────► the issue contract, VERBATIM (charter as system prompt)
   validating ── container_command ────► make build / make test / make lint IN the container's checkout
                 └─ failed ────────────► executing with the output, and the round counts
   reviewing ── container: add/commit -S/push ─► signed commit and push, in the container
             ── Reviewable ────────────► PR #m, head + artifacts recorded, label in_review
             ── forge_review ──────────► waits for approvals + green checks on the head
                 ├─ request_changes ───► re-delegating → executing (round 2 …)
                 ├─ rounds exhausted ──► escalating
                 └─ approved + green ──► completing ── Complete ─► merge (head-guarded), label merged
                                                    ├─ conflicts with base ─► executing: rebase round (round counts)
                                                    └─ strategy PostMerge steps (gitflow back-merge)
   escalating ── Blocked ──────────────► comment on the issue, label per kind; Task blocked
   cleanup ── Discard ─────────────────► nothing on disk to reclaim; the Delivery records the attempt

   the job is over ── the ENGINE concludes the Task ── TaskConcluded ─┐
     budget spent → failed          ─► comment `exhausted`,  label escalated  (no readmit)
     node died, no attempts left    ─► comment `abandoned`,  label escalated
     environment fault held/bounded ─► comment `blocked`,    label blocked
     attempts left                  ─► requeued; nothing concluded, nothing posted
   readmit ─────────────────────────► comment `readmitted`, every owned label removed
```

A turn that changed no file never reaches `validating` at all; it is
classified and routed straight from `executing`:

```
   executing ── the turn changed nothing ─► classify the reply (turn.go)
                 ├─ SDLC-STOP: question / owner-decision ─► escalating, Task blocked needs_info
                 ├─ SDLC-STOP: environment / unknown code ─► escalating, Task blocked
                 ├─ no reply at all ────► executing once more, then escalating
                 └─ no declaration ─────► NoChangesError: a failure of the work, charged
```

Readmitting a blocked task is the engine's `orchestrator:task_readmit`;
the next attempt continues the branch and reads the discussion. A park that
carries a machine-checked unblock condition needs no readmission — see
[the block lifecycle](#the-block-lifecycle). Every ending and the readmission
are posted to the issue through one table — see [Outcomes](#outcomes-every-way-a-task-stops-and-what-reaches-the-issue).

## The project binding

A **binding** attaches an orchestration Project to a forge repository and is
that project's autonomous-development configuration: which repository it
delivers to, which SDLC it follows, which agent writes the code, what verifies
the work, and how its pull requests are judged. It is `sdlc.ProjectBinding`
(`pkg/sdlc/binding.go`), stored per tenant, and it is the only record the
module owns besides the delivery.

- The full wire shape, and every transport it travels on, is in
  [api.md](api.md#the-sdlc-module).
- Binding a live project step by step is in
  [user.md](user.md#bind-a-project-and-follow-one-issue).

This page documents the **forge half** of the record — `forge:` — and, in
detail, the one field of it that decides whether a worker can check out at
all over ssh.

### `forge:` fields

| Field | Required | What it is |
| --- | --- | --- |
| `type` | yes | The adapter: `gitea`, `github` or `gitlab`. Only an adapter this build carries is accepted; `GET /api/v1/sdlc/forges` lists them. |
| `base_url` | yes | The forge's **API root**, version segment included (`/api/v1` for Gitea) — not the site origin. |
| `owner` / `repo` | yes | The repository on the forge. |
| `trigger_label` | yes | The label an OPEN issue must carry before the module adopts it as a Task. Compared exactly, case-sensitively. |
| `labels` | no | The module's label keys mapped to the forge labels that display them. Display output only. |
| `token_secret_ref` | yes | Names the tenant secret holding the forge API token. The value never enters this record. |
| `webhook_secret_ref` | no | Names the tenant secret holding the webhook HMAC key. Empty means the project is polled only. |
| `ssh_host_key` | for an ssh checkout | The forge's own SSH **host public key**, pinned. See below. |

The binding also carries an `onboarding:` block, outside `forge:`, declaring
what provisioning writes to the forge and what conformance checks — see
[Onboarding a repository](#onboarding-a-repository).

The token and webhook secrets are **references**. The values stay in the
tenant's secret store; the binding carries their names.

### `ssh_host_key` — the forge is pinned, never learned

A project whose `git.repo_url` is an `ssh://` (or `git@host:path`) remote is
cloned inside the worker container by a real OpenSSH client, and ssh will not
talk to a host it cannot recognise. The worker image ships **no**
`known_hosts` and the container's `$HOME` is fresh per attempt, so there is
nothing for ssh to recognise the forge from unless the operator says what the
forge is.

`ssh_host_key` is that statement. It holds **one OpenSSH public-key line** —
exactly what `ssh-keyscan` prints after the host field:

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA…
```

Get it, once, from the forge host itself:

```
ssh-keyscan -t ed25519 -p 2222 <forge-host> | awk '{print $2, $3}'
```

Paste the two fields (`<type> <base64>`, a trailing comment is allowed) into
the binding. Do **not** paste the whole `ssh-keyscan` line: the host pattern
in front of the key is derived from the project's repository URL, and a line
that still carries one is refused on bind with `forge.ssh_host_key`.

#### What the daemon does with it

Per attempt, before any container starts
(`pkg/orchestration/workspace_spec.go`):

1. The host pattern is derived from the project's repository URL — bare
   hostname on port 22, `[host]:port` otherwise; both the `ssh://` and the
   `git@host:path` spellings are recognised.
2. The pinned key is re-rendered from its parsed form and written as a
   single-entry `known_hosts` beside the workspace spec, mode `0400`, in the
   directory the container mounts **read-only** at
   `/run/quicbaas/workspace`.
3. The spawn carries

   ```
   GIT_SSH_COMMAND=ssh -o UserKnownHostsFile=/run/quicbaas/workspace/known_hosts -o StrictHostKeyChecking=yes
   ```

   so the worker's `git clone`, `git fetch` and `git push` verify the forge
   against that file and nothing else.

**`StrictHostKeyChecking=yes` is not a tunable and `accept-new` is never
used.** A worker holds a live socket to a KMS-backed signing agent for as long
as its attempt runs; trust-on-first-use would hand that socket to whatever
machine answered at the forge's address.

#### Refusals

| Situation | What happens |
| --- | --- |
| `ssh_host_key` is not one OpenSSH public-key line | Refused **on bind**, `forge.ssh_host_key`, so a typo surfaces while the operator is looking at it. |
| A whole `known_hosts` line, or several lines | Refused on bind; the message names the host pattern to drop. |
| Repository URL is `ssh://…` and no key is pinned | The attempt is refused **before the worker spawns**, with `orchestration.SSHHostKeyError` naming `forge.ssh_host_key`. No container is started. |
| Repository URL is `https://…` | No key is needed; the forge token authenticates the clone. A key pinned anyway is harmless and ignored. |
| The forge presents a key other than the pinned one | The clone fails inside the container with ssh's own `Host key verification failed`, and the attempt fails with the git output as evidence on the job. |

A **context repository on a different ssh host** is not covered: a binding
pins one forge, so only the project's own host gets an entry and a clone from
elsewhere fails host-key verification. Put context repositories on the forge
the project is bound to, or reach them over https.

#### The worker image's half

None of the above helps if the container has no `ssh` binary — git has no
built-in ssh transport and execs one. The worker image
(`deployments/agents/software-factory-worker/Dockerfile`) installs
`openssh-client` alongside the `openssh-keygen` that `gpg.format=ssh` signing
needs, and asserts both at BUILD time as the uid the spawn runs as:

```
RUN set -eux; git --version; ssh -V
```

`make test-worker-image` re-checks the same tools on the built image. No host
key and no forge address is ever baked into the image.

### `modules.rewrite_prefix` — private Go modules resolve at the forge

A Go module is named by its import path (`github.com/<owner>/<repo>`); a
private one is fetched by git from wherever the code lives — here, the forge.
`go build` in a fresh container asks the public proxy for that path, is told
it does not exist, and the verification fails before one line of the change
compiles. Measured on go-quicbaas#53 (2026-09-21): three review rounds of an
identical dependency-resolution failure, then an escalation, for work that
was never at fault. The first two factory projects never hit it because
neither depends on a private module.

`modules.rewrite_prefix` names the import-path prefix whose modules the forge
hosts under the project's own owner, as an https URL with a trailing slash:

```json
"modules": {"rewrite_prefix": "https://github.com/jeremyhahn/"}
```

From it the runner puts four variables in the **worker's** container
environment, and nothing anywhere else:

| Variable | Value | Why |
| --- | --- | --- |
| `GOPRIVATE` | `github.com/jeremyhahn/*` | the proxy and the checksum database are bypassed for the owner's modules |
| `GIT_CONFIG_COUNT` | `1` | git reads the rewrite below without a config file being written |
| `GIT_CONFIG_KEY_0` | `url.ssh://git@forge:2222/jeremyhahn/.insteadOf` | the rewrite target: the project's own `git.repo_url` with the repository dropped — same scheme, user, host, port and owner |
| `GIT_CONFIG_VALUE_0` | `https://github.com/jeremyhahn/` | the prefix being rewritten |

This is the mechanism the handbook's dependency-resolution convention
prescribes for containers, and nothing secret travels in it: the rewrite is
to the same ssh remote the project itself is cloned from, so the access key
already in the container's ssh-agent and the host key already pinned in its
`known_hosts` reach the sibling modules exactly as they reach the project.
`GOFLAGS`, `GONOSUMDB` and `GOINSECURE` are never set.

A prefix that is not `https://<host>/<owner>/` is refused at bind time. A
project whose `git.repo_url` carries no owner segment to derive the target
from is refused at spawn, before a worker is charged. An empty policy injects
nothing, so a project with no private modules gets the image's Go untouched.

## A satisfied verdict is answered by the base, not the branch

A worker that changes nothing and reports the work already present is
believed — but the question is whether the **base** has the work. On a
resumed attempt the checkout is the attempt's own branch, carrying what an
earlier attempt wrote and pushed and never got to publish (its verification
failed, its session died, the daemon restarted). Until 2026-09-21 that
verdict completed the task as `already_satisfied` with the code unmerged on
a branch no pull request named (go-quicbaas#51). Now, when the attempt's
branch is ahead of its base and no pull request is open, the run records
`sdlc:satisfied_on_branch` and sends the branch through verification and
publication like any other turn's work. Only a branch with nothing the base
lacks concludes the task.

## A full context rolls the session, not the attempt

A worker's session is a Claude Code process in print mode, and in print mode
the CLI cannot compact its conversation. A delivery turn of twenty minutes
reads and runs enough to fill the model's context, and the next prompt — the
round's verification findings, a few hundred bytes — is refused after minutes
with no text and no usage. The CLI reports it as `blocking_limit`, which its
own dispatch files, with `prompt_too_long` and `rapid_refill_breaker`, under
`context_limit`. Measured twice on the burn-in fleet on 2026-09-21
(go-quicbaas #51 and #965): both had passed build and test, both were failed
as attempts, and both redispatches began the same twenty-minute turn again in
a session that would fill the same way.

**The branch is the memory; the conversation is not.** When a prompt fails
with one of those three reasons, the runner keeps the attempt and replaces
the session:

1. whatever the dead session's checkout holds is committed and pushed, and
   the delivery records that head, so the next container checks it out
   rather than cutting the branch again from its base;
2. the session is closed and its container released;
3. a fresh worker is spawned on the same branch (the `initializing` state,
   as for a resumed run); the round, its findings and the job's memory are
   untouched, because they live on the run and the job;
4. the fresh session's first prompt carries the `session_rolled_note`
   template, so it reads the branch before it writes and keeps its own
   context small.

The audit event is `sdlc:session_rolled` with `class=`, `round=`, `session=`
and `elapsed=`. Every verification command also leaves `sdlc:verification`
(`command=`, `passed=`, `output_bytes=`, first line of stderr) on the job,
whether or not the pipeline went on. The most common way a round-1 prompt used to overflow was the
verification feedback itself: the whole `make test` output went into the
prompt. It is now the lines that name the failure (`--- FAIL`, `FAIL`,
`panic:`, a compiler's `# pkg` header and `file.go:line:col:` diagnostics,
`make: ***` — up to `VerificationSalientLines`, gathered from the whole
output) followed by the output's last 16 KiB (`VerificationEvidenceBytes`,
whole lines, omission stated); the full output stays on the job's
verification record. **Once per round**: a fresh session that fills again on the
same prompt has a prompt the work cannot take, and the run fails with the
CLI's reason, charged like `prompt_too_long`. Nothing in this group refunds
an attempt — see [accounts.md](accounts.md) for the refunded set, which is
now `api_error` and `turn_setup_failed` only.

## Onboarding a repository

Binding a project does not only record a policy — it **onboards** the
repository: it provisions, on the forge, everything QuicBaaS needs to drive
it. Nothing is committed to the repository.

**That constraint is the whole design.** The system this replaces shipped its
orchestration engine as a workflow file copied into every repository, so a fix
reached whichever repositories somebody remembered to copy it to and silently
missed the rest. Onboarding here writes to the forge's own configuration from
one binding record, so changing how orchestration works never means editing
twenty-eight repositories. Onboarding is a **data** operation.

### What binding provisions

| | | |
| --- | --- | --- |
| **Webhook** | a delivery to `<sdlc.webhook.public_url>/api/v1/sdlc/webhook/<projectID>`, subscribed to `issues`, `pull_request` and `status` | skipped when the binding names no `webhook_secret_ref` — that project is **poll-only by declaration**, not degraded |
| **Labels** | every label the binding's `labels` map declares, plus the `trigger_label` | created only when missing |

The webhook secret comes from the secrets engine, never a literal, and never
travels on the `WebhookSpec` — a spec is logged, stored and compared, and a
secret in it would leak through all three.

**Labels are never destroyed.** `EnsureLabels` has no delete path and no
rename path, and an existing label is left exactly as it is — colour and
description included. Labels carry history: every past pull request and every
saved search refers to them by name, so a provisioning pass that renamed one
would rewrite a repository's record of itself to make a config file tidier.

**Unbinding removes the webhook and keeps the labels.** The asymmetry is
deliberate: a delivery left behind is a live wrong behaviour — it goes to a
node that no longer drives the project — where a label left behind is only a
stale display. A webhook that cannot be removed **refuses the unbind** rather
than deleting the binding first, because once the route index has forgotten
the project nothing can identify the delivery's owner and no later unbind
could find it. A repository that is GONE from the forge is not that case: it
holds no delivery (a 404 on the hook listing), so unbinding it succeeds.

**A forge that is merely DOWN answers 5xx, and that still refuses the unbind.**
That is deliberate — a 5xx is "ask again", not "there is nothing there" — but it
is an operational constraint worth knowing before you meet it: a project cannot
be unbound for the duration of a forge outage. The refusal is retryable and
leaves the binding, the route and the delivery all intact, so the recovery is to
retry once the forge answers. There is no force-unbind, because accepting a
stray delivery nobody can later identify is a decision that needs an owner
rather than a flag.

### Idempotent, and that is the feature

Running onboarding again creates nothing and changes nothing; running it on a
project whose webhook somebody deleted by hand puts the webhook back. That is
what makes onboarding twenty-eight repositories a loop nobody has to keep
notes about.

`POST /api/v1/sdlc/projects/{id}/onboarding` — `quicbaas sdlc onboard <id>` —
re-runs provisioning against the stored binding and returns the conformance
that follows. It is also the **repair** for a conformance finding.

### Conformance: what is wrong, not merely that something is

`GET /api/v1/sdlc/projects/{id}/conformance` — `quicbaas sdlc conformance
<id>` — answers "is this project correctly onboarded, and if not, what is
wrong". **It reports and never repairs.** An operator asking what state a
repository is in must be able to ask without changing it, which is why the
repair is a separate verb on a separate route under a separate permission.

The CLI exits non-zero when the project is not conformant, so it gates a
script — and still prints the report, because an exit code that says
"something is wrong" without saying what is precisely the failure this verb
exists to end. The company already owned `conformance.sh`,
`review-workflow-drift.sh` and `check-workflow-outputs.py`: three well-built
instruments, none of them wired to anything. A check nobody runs is
indistinguishable from a check that does not exist, which is why this is a
queryable surface rather than a script somebody remembers.

#### What conformance checks

| check | class | means |
| --- | --- | --- |
| `binding` | `binding_invalid` | the stored binding does not validate, or the project has none. Reported ALONE — the repository, the vocabulary and the token are all in question, so the consequences would bury the cause |
| `webhook` | `webhook_missing` | no delivery to this node |
| | `webhook_inactive` | a delivery the forge has switched off. Separate because it LOOKS present in the settings list and delivers nothing |
| | `webhook_misdirected` | a delivery for this project aimed at another node — what a restored backup or a migrated deployment leaves behind |
| | `webhook_events_missing` | subscribed to less than the engine needs. Nothing looks broken; the deliveries that would have prompted a reconcile never come |
| `labels` | `label_missing` | a declared label the repository does not define. **The silent failure this whole surface was built for**: the forge refuses the label write, the ending never displays, and nothing says the repository was never set up |
| `workflows` | `stale_workflow` | an orchestration workflow still in the repository — a second orchestrator racing QuicBaaS for the same pull requests |
| `protection` | `protection_missing` | the branch is unprotected |
| | `protection_context_missing` | the branch does not require a status context QuicBaaS writes, so a change can land past the module's own verdict |
| any | `check_unsupported` | the PLATFORM cannot answer the check. Not a failure — the honest alternative to reporting a finding for a setting that does not exist on that forge |
| any | `check_failed` | the check could not be run. **Counts as non-conformant**: a pass whose reads failed knows nothing, and reporting that as conformant is how a fleet dashboard turns an outage into a clean bill of health |

Every finding names its class, its subject, what was observed and its
**remedy**, because the repairs differ: a missing webhook is an `Onboard`
call, a stale workflow is a commit, a missing protection context is a setting
only the repository's owner may change. Onboarding deliberately does **not**
create branch protection — who may merge is the owner's policy, and
provisioning it behind their back would be the module widening its own
authority.

### `onboarding:` fields

Every field defaults, so a binding that omits the block still onboards
correctly.

| Field | Default | What it is |
| --- | --- | --- |
| `webhook_events` | `issues`, `pull_request`, `status` | the forge events the delivery subscribes to. A forge may EXPAND one into its family; the adapter's read-back accounts for that |
| `protected_branch` | `main` | the branch whose protection conformance reads |
| `required_status_contexts` | `["quicbaas/review"]` | the contexts that branch's protection must require. A repository whose CI publishes under its own context name adds it here |
| `forbidden_paths` | the four orchestration workflows deleted at the 2026-09-12 cutover | repository paths that must not exist |
| `forbidden_paths_ref` | the default branch | the ref the forbidden-path check reads |
| `label_colors` | a per-key palette | the fill a label is CREATED with. An existing label's colour is never changed |

### The forge seam

`ForgeBackend` embeds `ForgeOnboarding` (`pkg/sdlc/forge_onboarding.go`):
`Labels`, `EnsureLabels`, `Webhooks`, `DeleteWebhook`, `RepoFiles`,
`BranchProtection`. Every one is stated in terms Gitea, GitHub and GitLab all
have — a repository has labels, webhooks, a file tree and protected branches —
and the one real platform difference is declared as data on
`ForgeCapabilities.BranchProtectionContexts`, never discovered with a switch
on `Type()`.

`RepoFiles` lists a directory; **which** paths count as stale orchestration is
QuicBaaS's policy and lives on the binding. A `ListWorkflows` method would
have pushed that policy into every adapter and made a GitLab port invent an
equivalent of a GitHub Actions directory.

### Forge transport: deadlines, retries, and unanswered writes

Every forge call is bounded by a deadline chosen by what the call costs the
forge, and a call the forge does not answer — no response inside the
deadline, a dropped connection, or a gateway's 502/503/504 — is handled by
what repeating it would do (go-quicbaas#1034). Configured under
`sdlc.forge` (see [Configuration](#configuration)); zero takes the default.

One 30-second bound for everything was wrong both ways. On 2026-09-22 a
Gitea host under CI load missed it on a triage listing, a review read and a
squash merge, and answered in ~70 ms moments later — and the merge had
**landed**: the job was recorded failed on a change that was on `main`. A
create-PR the same evening timed out after Gitea had opened the pull request,
and the retry's create would have been refused as a duplicate.

| call | deadline | unanswered |
|---|---|---|
| any read (GET) | `read_timeout` | retried up to `read_attempts`, backing off from `retry_backoff`; then `ForgeRetriesExhaustedError` |
| `MergePullRequest` | `heavy_write_timeout` | the PR is re-read: merged with a merge commit is **success**; open and mergeable is not-ready, so the bounded `merge_attempts` retry decides (its next attempt re-reads first and stops at "already merged"); anything else is `ForgeWriteUnconfirmedError` |
| `CreatePullRequest` | `heavy_write_timeout` | an open PR for the same head → base **from this repository** (never a fork's same-named branch) is **adopted** — looked for before the first create, after an unanswered one, and on Gitea's 409 duplicate refusal. A create is re-sent only after a lookup has just shown none exists, within `read_attempts` |
| labels (set, add, remove), close issue, delete branch, commit status, update/delete webhook | `write_timeout` | replayed like a read: each is idempotent on the forge and verified by the method's own read-back |
| comment, create branch, submit review, rerun CI, create webhook, create label, mint/revoke token | `write_timeout` | **never repeated** — a second one is a second comment, verdict, pipeline, hook or credential; `ForgeWriteUnconfirmedError` |

The table is `giteaWritePolicies` in `pkg/sdlc/forge_gitea_transport.go`; a
write missing from it takes the last row, and a test fails the build if any
write in the adapter is unclassified. Only silence is retried: a status the
forge itself returned (404, 409, 422, Gitea's own 500) is final.

**What is charged.** Every one of these errors keeps the unanswered call in
its chain, and that call unwraps to `orchestration.ErrForgeUnreachable`. A job
that ends on one is classified `forge_unreachable`: an environment fault,
refunded and requeued, bounded by the three-in-a-row rule like every other
requeueing class. A forge that ANSWERED — refused a merge, reported a moved
head — said something about the work, and that is still charged. A caller
that cancelled its own context is not the forge's fault either, and is
neither retried nor attributed to it.

## Configuration

The module is configured under `sdlc:`. Everything generic about a job —
its attempt budget, lease, stage timeouts, idle thresholds, the poller's
cadence — is the engine's and lives under `orchestration:`.

```yaml
sdlc:
  enabled: true
  # Forge hosts a bound project's repository may live on (hostname or
  # host:port). Empty refuses every repository.
  repo_hosts: ["git.example.com"]   # the deployment's own forge host
  allow_local_repos: false          # file:// and bare paths; test rigs only
  # DECLARED AND UNREAD (issue #625). What the sdlc runner runs is the
  # binding's own `verification` list, and what admits a command is the
  # engine's ExecutionGuard allowlist below. This key is validated at load
  # and then read by nothing; set orchestration.verification_commands.
  verification_commands: ["make build", "make test", "make lint"]
  merge_attempts: 6                 # retries while the forge says not-ready; still not mergeable after = conflict
  issue_page_limit: 50              # issues per project sweep
  # Where the worker's container finds its checkouts: the project at
  # <checkout_root>/<project name> and each context repository beside it.
  # It is the path INSIDE the container — the daemon clones nothing — and it
  # is what the runner's clone commands name. Name it only if the worker image
  # lays its filesystem out differently.
  checkout_root: /workspace
  surfacing:                        # the Blocked hook's post to the issue
    retry_attempts: 5
    retry_interval: 2m
    post_timeout: 30s
  label_reconcile:                  # the repair sweep; see "Reconciliation"
    interval: 5m                    # how often the forge is re-read
    forge_interval: 200ms           # minimum spacing between its forge calls
                                    # env: QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL
                                    #      QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL
                                    # the module's own scalars are bound too, incl.
                                    # QUICBAAS_SDLC_ENABLED — an env-only
                                    # deployment must be able to switch it on.
                                    # verification_commands and repo_hosts are
                                    # file-only: viper will not decode an env
                                    # list into []string through Unmarshal.
  webhook:
    max_body: 1048576               # bytes; the signature check buffers the body
    # THIS NODE's externally reachable ORIGIN: scheme, host, optional path.
    # REQUIRED when the module is enabled and deliberately not defaulted:
    # onboarding provisions forge webhooks pointing at
    # <public_url>/api/v1/sdlc/webhook/<projectID>, and a guessed origin would
    # provision every managed repository to deliver into the void and report
    # each one provisioned.
    #
    # A QUERY, A FRAGMENT AND USERINFO ARE REFUSED AT LOAD. The route above is
    # APPENDED to this value, so "https://factory.example.com?x=1" yields a
    # delivery URL whose path is "/" with the route buried in a query string —
    # and nothing notices: the hook is provisioned, read back, compared to the
    # same malformed string, and reported conformant while every delivery
    # misses the endpoint. A PATH is allowed, for a node behind a reverse
    # proxy: "https://factory.example.com/quicbaas" appends correctly.
    # env: QUICBAAS_SDLC_WEBHOOK_PUBLIC_URL
    public_url: https://factory.example.com
  forge:                            # every forge call; see "Forge transport"
    read_timeout: 30s               # one attempt at a read
    write_timeout: 1m               # a light write: label, comment, status
    heavy_write_timeout: 5m         # merging or opening a pull request
    read_attempts: 3                # attempts at an unanswered read or a
                                    # write proven safe to repeat
    retry_backoff: 1s               # first wait between attempts; doubles
                                    # env: QUICBAAS_SDLC_FORGE_* (e.g.
                                    #      QUICBAAS_SDLC_FORGE_HEAVY_WRITE_TIMEOUT)

orchestration:
  sweep_interval: 30s               # task-source poller cadence
  reap_lease_ttl: 90s               # dead-job detection
  # The ExecutionGuard allowlist: the operator decides, on the DAEMON, which
  # commands a criterion may run. An admitted sdlc verification command then
  # runs INSIDE the worker's container, in its checkout — never on the daemon
  # — so it needs the worker image's toolchain, not the daemon host's.
  verification_commands: ["make build", "make test", "make lint", "make security"]
  stage_timeouts: {prepare: 10m, reviewable: 10m, complete: 10m, cleanup: 2m, blocked: 1m}
  idle_thresholds: {reviewing: 24h}
  # Where the daemon binds each attempt's KMS-backed signing-agent socket.
  # Empty derives $XDG_RUNTIME_DIR/quicbaas/<instance>, or <storage.data_dir>/s
  # when there is no runtime directory. <instance> is per-DAEMON, so two
  # daemons of one user do not share a root. See "The signing socket's root".
  sockets_root: /run/user/1000/quicbaas/3f0a1c9d24b7
```

### The signing socket's root

The worker's commit is signed inside its container against an ephemeral
agent the daemon holds; what is mounted in is a unix socket, and the key
never leaves the KMS. **That socket is bound under
`orchestration.sockets_root`, not under the project's workspace.**

A unix socket address is a fixed-size kernel field — `sun_path` is 108
bytes, so 107 is the longest path that can be bound — and a managed
workspace is `<projects_root>/<tenant_id>/<project_id>/workspace`, which
spends 88 bytes on the tenant and project alone. Under the default projects
root that came to 148 bytes on a real node, so the workspace placement
failed on every realistic deployment.

| | |
|---|---|
| key | `orchestration.sockets_root` (absolute path; refused otherwise) |
| default | `$XDG_RUNTIME_DIR/quicbaas/<instance>`, else `<storage.data_dir>/s` |
| `<instance>` | 12 hexadecimal characters, the leading bytes of the SHA-256 of the **absolute** `storage.data_dir` |
| longest usable root | 83 bytes — the root, plus a 12-character session directory, plus `/agent.sock` |
| created | at startup, mode `0700`, by the daemon |
| per attempt | `<sockets_root>/<12 hex characters>/agent.sock`, created `0700` by the agent and removed when the attempt ends |

Three properties are load-bearing:

- **It is checked at startup, not at the first delivery.** The arithmetic is
  fixed, so a root past the budget can never work. The daemon refuses to
  start with a `SigningSocketRootError` naming the root, the longest path it
  yields and the limit — instead of starting, looking configured, and
  failing every job at `initializing` with a listen error that names nothing.
  A root that cannot hold a socket safely — world-writable (which is what
  the OS temporary directory is), a symlink, not a directory — is refused
  the same way, with a `SigningSocketRootUnusableError`.
- **The mount policy admits it by construction.** There is no allowlist
  entry to remember: `orchestration.sandbox.allowed_mount_roots` is for the
  operator's own directories. The grant is the narrowest one that works —
  the socket file itself, one directory below the root, read-only, and
  actually a socket on disk. The root, the session directory, and anything
  beside or below the socket stay denied.
- **It is swept at startup.** An attempt that ends releases its agent, but a
  daemon *killed* between the agent's bind and its release cannot, and the
  root outlives the daemon — so without a sweep every hard kill costs the
  node one dead socket for ever. Preparing the root removes the session
  directories left behind and the daemon logs the count at INFO
  (`sdlc: sockets root <root> swept: <n> orphaned signing agent session(s)
  removed`). A sweep that cannot be completed — the root refusing to be
  enumerated, or an orphan refusing to be removed — is a
  `SigningSocketSweepError` and a startup failure, for the same reason an
  unusable root is.

  What is swept is only what the daemon mints: a directory named exactly 12
  hexadecimal characters, holding nothing but `agent.sock`, and that socket
  answering nobody. **Liveness is decided by the socket, not by the clock** —
  an agent's listener dies with the process that held it, so a socket that
  refuses a connection belongs to no running attempt. The simpler invariant
  ("the daemon has only just started, so nothing here can be live") holds for
  one daemon per root; the derived root is now one daemon's, but an operator
  who points two daemons at one explicit `sockets_root` gets the probe rather
  than a wrong assumption. Anything else under the root — a foreign name, a
  file, a symlink, a directory with other contents in it, a directory the
  daemon may not read — is left exactly where it is, and a root the refusals
  above reject is never swept at all.

#### The derived root is per-daemon, not per-user

`$XDG_RUNTIME_DIR` is a **per-user** directory. A root derived from it alone
is reached by every `quicbaasd` the user runs, so two daemons with different
data directories bound their session sockets under one root — and one
daemon's startup sweep could reach the other's live sessions. The derived
root therefore carries a per-daemon segment:

```
$XDG_RUNTIME_DIR/quicbaas/<instance>/<12 hex characters>/agent.sock
         per-user       per-daemon        per-attempt
```

- **`<instance>` is derived from the absolute `storage.data_dir`** — the one
  setting two instances on one host cannot share. There is no node identity
  to borrow: `raft.node_id` defaults to `1` for every standalone daemon, and
  the node name the orchestrator uses is deliberately stable across restarts
  and shared by every daemon on a host. It is the leading 12 hexadecimal
  characters of a SHA-256 digest, truncated so the segment stays fixed-width
  and inside the socket budget; it names a directory in the user's own
  private runtime directory and authorises nothing.
- **The path is resolved first**, so two daemons started from different
  working directories with the same relative `data_dir` are still different
  daemons. A working directory that cannot be read is a startup failure
  (`config.SocketsRootIdentityError`), not a shared root.
- **A daemon with no `storage.data_dir` derives no root at all.** It has
  nothing to be identified by, and the runner refuses the empty root by name
  rather than letting it share the user's.
- **The data-directory derivation takes no segment.** `<storage.data_dir>/s`
  is already inside the directory that distinguishes the daemons, and a
  segment there would spend socket budget for nothing.
- **An explicit `orchestration.sockets_root` is used exactly as written.** It
  names one deployment already; appending a segment would silently move the
  directory the operator provisioned.

### Removed keys

The following `sdlc:` keys are refused **by name** at load
(`config.RefuseRemovedSDLCKeys`, `RemovedSDLCKeyError`), from the file and
from the environment. Pre-v1: there is no migration path; rename by hand.

| removed | where it went |
|---|---|
| `sdlc.attempt_budget` | the engine's per-task budget (`Task.AttemptBudget`, default `orchestration.DefaultAttemptBudget`) |
| `sdlc.reap_lease_ttl` | `orchestration.reap_lease_ttl` |
| `sdlc.stage_timeouts` | `orchestration.stage_timeouts` |
| `sdlc.idle_thresholds` | `orchestration.idle_thresholds` |
| `sdlc.dispatch_cap` | the project's `quotas.max_concurrency` (live job leases per project) |
| `sdlc.run_timeout` | `orchestration.idle_thresholds` |
| `sdlc.sweep_interval` | `orchestration.sweep_interval` |
| `sdlc.mirror_lease_ttl` | nothing; adoption is the task store's guarded source index |
| `sdlc.queue_capacity`, `sdlc.delivery_memory` | nothing; a verified webhook nudges the poller, re-polling is idempotent |
| `sdlc.max_webhook_body` | `sdlc.webhook.max_body` |
| `sdlc.stage_event_flush_timeout` | nothing; job events are the engine's own stream |
| `sdlc.agent_config_dir` | `orchestration.agent_config_dir` — the ONE knob; the engine puts it on every session it spawns (`JobLifecyclePolicy.AgentConfigDir`) |
| `sdlc.worktree_root`, `sdlc.companions` | nothing; the daemon does no clone, copy or worktree. The worker's container clones the project and the project's `context_repos` itself — see [Autonomous projects](autonomy-and-dispatch.md) |

### The worker image

A binding's `worker.image` names the container image the job's one worker
session runs in. Naming one requires the sandbox for that project's workers.

This repository builds one:

```
make build-worker-image      # quicbaas-software-factory-worker:<VERSION>
make test-worker-image       # builds it, then proves it can sign a commit and lint a module
```

```yaml
# the binding
worker:
  backend: claude
  model: claude-opus-5
  image: quicbaas-software-factory-worker:0.5.1-alpha
```

The image (`deployments/agents/software-factory-worker/Dockerfile`) carries the
claude-code agent, **git**, **ssh-keygen** — git's `gpg.format=ssh` signing
invokes it, so a commit cannot be signed without it — **make**, a C toolchain
and the **Go toolchain** the projects pin, and no secret of any kind. The forge
token arrives as a read-only file under `/run/quicbaas/workspace`; the signing
key never arrives, only a socket to an agent that holds it.

**It carries the verification toolchain too**, because the binding's
verification commands run inside this container: **golangci-lint**, **gosec**
and **govulncheck**, which is what a Go project's `make lint`, `make check`
(fmt vet lint gosec vuln) and `make security` are made of. Without them
`make lint` exits non-zero with *"golangci-lint is required but not installed"*
on a clean tree, so every review round fails and every Go job escalates
(go-quicbaas#772). They are installed at `$(go env GOPATH)/bin` — where a Go
Makefile actually looks for the linter, which is not the same as the `PATH` —
and at the versions this repository pins:

```
make build-worker-image  # passes GOLANGCI_VERSION / GOSEC_VERSION / GOVULNCHECK_VERSION
```

Those three Makefile variables are the repository's **only** copy of the pins:
`make install-tools` installs them, CI installs them by reading them
(`make print-GOLANGCI_VERSION`), and the image bakes them. A worker linting with
a version CI does not use produces a review verdict the merge then contradicts.
`make test-worker-image` asserts the tools resolve where a Makefile looks, that
`golangci-lint` reports the pinned version, and that it lints a vendored module
clean and reports a finding on a bad one — with the container on `--network none`.

**Trivy is deliberately absent.** It is the fourth member of `make security`,
but it downloads a vulnerability database on every run: in a worker sandbox
whose network policy is the operator's to decide, it would fail for the
environment rather than for the diff. A binding whose verification needs it must
name an image that carries it.

**It is built for the uid that will run it.** The spawn runs a sandboxed
session as the daemon's own uid:gid, and musl resolves users from `/etc/passwd`
alone — ssh-keygen calls `getpwuid` at start and exits without an entry, so an
image built for someone else's uid cannot sign anything. `make
build-worker-image` passes the building user's ids; building for a daemon that
runs as another user means overriding them:

```
make build-worker-image WORKER_IMAGE_UID=997 WORKER_IMAGE_GID=997
```

The image has **no meaningful entrypoint**, and does not need one: the spawn
clears it and runs the agent binary as the container's command, and the clone
is driven by the runner through the runtime's exec seam
(`pkg/sdlc/checkout.go`) rather than by anything in the image.

### The project side

The repository URL, default branch, commit author, commit signing key,
autonomy, the concurrency bound and the repositories the worker clones beside
the project (`context_repos`) are `Project` fields, set with
`quicbaas project create` / `update` (`--git-repo`, `--git-branch`,
`--git-signing` + `--git-signing-key`, `--autonomous`,
`--context-repo`, `--clear-context-repos`, `--max-concurrency`). Bind refuses a project without a signing policy or with a repository
outside `repo_hosts`. It **turns the project autonomous** (`autonomous:
true`), and it neither reads nor sets `default_workflow` — that field belongs
to the lead-driven `workflow` runner, and an `sdlc` job has no workflow.

`context_repos` is what replaced the removed `companions`: the daemon puts
the list in the workspace spec it hands the container, and the runner clones
each entry beside the project checkout, inside that container. The spec's
shape and the checkout's obligations are in
[Autonomous projects](autonomy-and-dispatch.md).

## Outcomes: every way a task stops, and what reaches the issue

A Task the module adopted from a forge issue stops for one of ten reasons.
Each one writes to the issue the same way — one comment, one label move, one
artifact on the Task — through one table and one renderer.

Before go-quicbaas#788 only the run's own escalation did. A task that spent
its attempt budget concluded `failed / budget_exhausted` and wrote nothing at
all: `go-truststrap#26` kept `factory:in-progress` and had no comment two hours
after the engine had given up on it, which from the owner's side is
indistinguishable from work in progress.

### The seam

The lifecycle **moments** (`Prepare`, `Reviewable`, `Complete`, `Discard`,
`Blocked`) belong to a *job*: they run inside the control loop, on a live
`JobContext`. A task's conclusion is decided elsewhere — in
`orchestration.applyConclusion`, after the run has returned, and on the
reaper's goroutine when the node that held the run is gone. So the conclusion
is a **seam of its own**, not a sixth moment:

| | moments | conclusion observer |
|---|---|---|
| registered as | called directly by the `sdlc` runner | `orchestration.RegisterTaskConclusionObserver` |
| receives | `*JobContext` | `orchestration.TaskConclusion` (tenant, reason, the concluded `*Task`, the job) |
| fires from | the control loop | `concludeAttempt` (loop **and** reaper), `BlockTask`, `ReadmitTask` |
| may change the outcome | yes — a hook error fails the job | **no** — the task is already durable |

`pkg/server/lifecycle_sdlc.go` registers `sdlcConclusionForwarder` under the
module's name, exactly as it registers the source, the validators and the
runner; a stopped module answers `sdlc.ErrUnavailable` rather than acting on
a dead store.

The announcement runs on a context detached from the caller's and bounded by
`orchestration.taskConclusionSurfaceTimeout` (2 minutes). Detached because the
conclusion is already written and must still reach the person watching while
the run that produced it is being torn down; bounded because a forge that will
not answer must not hold a job goroutine or a reaper sweep open.

### The table

`orchestration.TaskConclusionReasons()` is the closed vocabulary;
`pkg/sdlc/outcome.go`'s `conclusionOutcomes` maps every member to what the
module writes. `TestConclusionOutcomes_CoverEveryReason` fails if a reason has
no row — a conclusion the module cannot render is the defect itself.

| conclusion | when | comment | label swap | readmit |
|---|---|---|---|---|
| `completed` | the attempt ended in any state but failed | none — `Complete` already merged and relabelled | already `merged` | n/a |
| `budget_exhausted` | the last charged attempt failed | `exhausted` — *This task has spent its attempt budget* | `in_progress` → `escalated` | **no** |
| `abandoned` | the node died and no attempts remain | `abandoned` — *This task lost the node that was running it* | `in_progress` → `escalated` | yes |
| `fault_bounded` | `MaxConsecutiveEnvironmentFaults` reached | `blocked` — *This task is blocked* | `in_progress` → `blocked` | yes |
| `fault_held` | a classified fault that does not requeue | `blocked` | `in_progress` → `blocked` | yes |
| `parked_needs_info` | `BlockTask` kind `needs_info` | `needs_info` — *The agent has a question* | `in_progress` → `needs_info` | yes |
| `parked_blocked` | `BlockTask` kind `blocked` | `blocked` | `in_progress` → `blocked` | yes |
| `parked_escalated` | `BlockTask` kind `escalated` | `escalated` — *This task needs the owner* | `in_progress` → `escalated` | yes |
| `runner_parked` | the RUN parked the task itself | none — the `Blocked` moment already posted it | already moved by that moment | yes |
| `readmitted` | `orchestrator:task_readmit` | `readmitted` — *This task has been readmitted* | every owned label removed | n/a |

A failed attempt that leaves budget behind concludes **nothing**: the task is
requeued and the next attempt starts. One comment per failed attempt would bury
the issue, and nothing has ended.

### Why `runner_parked` is silent when the `parked_*` three are not

Both park the task; they differ in **who has already told the issue**.

The three `parked_*` reasons are the operator's explicit `BlockTask`, a verb
that writes nothing to the forge — so this table is the only thing that can,
and it does.

`runner_parked` is the `sdlc` runner deciding from the work itself that
another attempt would ask the same unanswered question: a worker that stopped
on a question, on a rule it may not break, or on an environment it could not
build in (go-quicbaas#787). That park reached the issue **before the job
returned** — `runnerRun.surfacePark` posted it through the `Blocked` moment,
carrying the worker's own text and its typed unblock condition, under
`blockedMarker(task, attempt, blockedOutcome(kind))`. That is the very marker
`renderOutcome` would write. A row here would put two comments with one marker
on the issue, each claiming to be the park.

The `Blocked` moment keeps it for a second reason the conclusion seam cannot
serve: it is the only path that can ask the issue whether an interrupted post
already landed, which is what makes an escalation survive a daemon restart
exactly once (`runnerRun.parkPosted`).

**And the silence is conditional on the post having landed.** `BlockReason`
carries a `Surfaced` flag, set only AFTER `surfacePark` returns — and
published and persisted at that moment rather than at whatever snapshot
happens next, because a death in that gap would make the reaper read a park
that WAS posted as unposted and post it again. The published status carries
a *copy* of the park for the same reason `JobStatus` is immutable once
published: `Status()` hands it to concurrent readers, so the run must not be
able to write through it. `parkConclusion` reads the flag, and
`parkConclusion` reads it: a park the run announced concludes
`runner_parked` (silent, because the comment is already there), while a park
the run could NOT announce concludes as the plain `parked_<kind>` its block
kind names — one of the three rows above — so the conclusion seam writes the
comment instead.

That closes what used to be the sharp edge here: if the forge could not be
reached within `surfacing.retry_attempts`, the run failed with
`SurfacingUndeliveredError`, the task blocked anyway, and the issue said
nothing at all — silence in exactly the case where the runner's own
announcement is what broke. Now the conclusion posts it.

One deliberate trade remains: `Hooks.Blocked` posts the comment and then
moves the label, so a run whose comment landed and whose LABEL move failed is
recorded unsurfaced and the conclusion posts a second comment. A duplicate is
recoverable by a reader; silence is not.

#### Why `escalated` and not a new `exhausted` label

The comment kinds and the label keys are different vocabularies. A binding's
`labels:` map is operator configuration, and `ForgeConfig.Label` answers a key
it does not hold with the empty string — so a new `exhausted` key would leave
every binding already in the field applying **no** label at all, which is a
worse version of the bug. `escalated` already means "the engine gave up on this
task"; the comment says which way.

#### Why `budget_exhausted` cannot be readmitted

`taskTransitions` has no edge out of `failed`. A task the budget exhausted is
failed, so `orchestrator:task_readmit` is refused however much budget is added
afterwards, and the comment says so rather than offering a command that will
not work. The route back is the runbook's: fix what the evidence names and
re-apply the trigger label — the spent task no longer reserves the issue, so
the next sweep adopts a fresh one and records the spent one in
`metadata.predecessor_task`.

### What the comment carries

Every outcome comment is `renderOutcome`, one function, one shape:

```
### This task has spent its attempt budget

**Outcome:** `exhausted` — every attempt the task was allowed has been charged
and failed, so the Software Factory has stopped working on it.

> sdlc: task 46dac86c… produced no changes in /workspace/go-truststrap

**Attempt:** 3 of 3
**Task:** `46dac86cfe9834dc84b96fb8e4d18871`
**Attempt history:** `1d680be2`, `68782d66`, `08628ce5`
**Labels:** `task:ready` → `factory:escalated`

**This task is over and cannot be readmitted.** …

<!-- quicbaas-sdlc:blocked task=46dac86c… attempt=3 kind=exhausted -->
```

- The **evidence** is the task's own record: `BlockedReason` when the task is
  parked, otherwise `Result.Output` — which is where the runner records the
  failure that ended the attempt, and where a worker's final reply is carried
  when the attempt kept one (go-quicbaas#787). It is rendered through
  `publicOutput`, so credential shapes are redacted and the window is capped;
  see `public_reason.go`.
- The **attempt history** is `Task.AttemptHistory()`, the jobs concluded
  against the task, oldest first.
- A park whose reason carries a **typed unblock condition** (go-quicbaas#784)
  adds `**Unblocks when:**` and `**Cleared by:**` lines, and the remedy says
  "nothing here" when a sweep can decide the condition itself. The split is
  `blockedCondition`, one function, so the machine marker never reaches a
  reader whichever path renders the comment. A spent budget outranks it: no
  sweep can return a task with no attempts left, so the budget line wins.
- The **marker** names the task, the attempt and the kind together, so a task
  parked twice carries two comments a reader and a resumed run can tell apart.

### What is recorded on the task

One artifact, named `outcome_comment`, the way `Reviewable` records
`pull_request` and `head`:

```json
{ "kind": "url", "name": "outcome_comment",
  "ref": { "path": "https://forge/owner/repo/issues/26" },
  "metadata": { "outcome": "exhausted", "issue": "owner/repo#26", "comment_id": "913" } }
```

A task whose provenance carries no issue URL records nothing rather than an
artifact pointing nowhere — the same rule `Hooks.publish` follows for a
carve-out change with no pull request.

### Idempotence

Nothing is deduplicated at this seam and nothing needs to be. A task concludes
once under the store's compare-and-swap on `Task.Version`, and the block and
readmit verbs are refused by the status transition table on a second call.

The one path that really can run twice is a **resumed escalation**: a daemon
that died between entering `escalating` and `Blocked` completing resumes there,
and `runnerRun.parkPosted` asks the issue whether this attempt's marker is
already on it before posting again. That check stays where it is.

A run that escalates on its last attempt therefore posts **two** comments — the
run's `escalated` and the task's `exhausted`. They are two different facts (this
attempt gave up; the task is over and cannot come back) with two different
markers, and the second is the one that says what happens next.

### Reconciliation: the forge is re-read, not assumed to have been written

The conclusion seam makes every **new** ending reach the forge. It cannot fix an
ending that already did not, and on the burn-in node two holes were measured
behind it — seven `go-truststrap` issues (#25, #26, #52, #53, #54, #55, #56)
carrying `factory:in-progress` for tasks that had been terminal for **days**:

- **A conclusion whose forge write failed is never revisited.**
  `TaskConcluded` answers `SurfacingUndeliveredError` when the forge refuses
  every retry, and the engine reads an observer error as a report that did not
  land — correctly, because the task is already durable. Nothing tries again.
- **A conclusion that predates the seam was never posted at all.** Every task
  that stopped before go-quicbaas#788 shipped left its issue claiming live work.

There was also no liveness check from the forge's side: a node that died between
concluding a task and posting its outcome left the label stuck with nobody
watching. From the forge, an operator could not tell a live run from a corpse.

`pkg/sdlc/label_reconcile.go` closes both. It runs **at boot and on a ticker**
(`sdlc.label_reconcile.interval`, default 5m) with no operator verb — the state
it repairs is by definition state nobody noticed, and the operator is the person
the stale label is lying to. It is started at the tail of `startSDLC` and stopped
before the module detaches, so a pass never runs against a dead store.

#### What one pass does

It asks the forge which issues carry a **claiming** label — the binding's
`in_progress` and `in_review`, the only two that assert the factory is working
right now — and compares that claim with the engine's own task records:

| claim | engine record | verdict | action |
|---|---|---|---|
| claiming label | task claimed / in_progress | `consistent` | nothing |
| claiming label | task is **pending** | `undecidable` | nothing; logged |
| claiming label | task is terminal | `surfaced` | post the outcome, swap the label |
| claiming label | outcome already posted | `relabelled` | swap the label only |
| claiming label | no task at all | `surfaced` | post `abandoned`, swap the label |
| claiming label | task completed | `relabelled` | move to `merged`, post nothing |
| claiming label | the **worker** parked it | `relabelled` | restore the park's label, post nothing |
| no lifecycle label | task claimed / in_progress | `restored` | re-apply `in_progress`, post nothing |
| a park's label | task readmitted, **still queued** | `relabelled` | clear the label, post nothing |
| — | forge refused | `deferred` | nothing; retried next sweep |
| — | record unreadable | `deferred` | nothing; retried next sweep |
| — | status the table names no ending for | `undecidable` | nothing; logged |

**Neither answer that writes is decided from the pass's opening snapshot.** The pass lists
the tasks and *then* lists the forge, and it spends a paced forge round trip on
every issue before the one it is settling — so between the snapshot and the
decision there are seconds in which an ordinary adoption happens: the poller
persists a task and the run it dispatches labels the issue in progress. Writing
from the snapshot would announce an ending on a *live* run and move its issue to
a terminal label, which is strictly worse than the stale label the sweep exists
to correct, because a lie a sweep **wrote** is one an operator has no reason to
doubt. So the record is read **again**, after the claim that provoked the
question and before anything is written.

This applies to the terminal row as much as to the orphan row. A terminal task
releases its source reservation, so re-labelling the issue adopts a *fresh* task
while the spent one stays on the record; a pass holding only the spent task would
post its ending over a run that is live right now. The re-read is indexed by the
same live-supersedes rule, so the newly adopted task wins exactly as it would
have had the snapshot been late enough to see it. That is a different answer rather
than a narrower window: the engine persists a task before anything labels its
issue, so a claim observed at *T* implies its task was durable before *T*, and a
record read strictly after *T* sees it. (Listing the forge first and the tasks
second would close only the one interleaving, and would buy a false "lost
hand-over" on every task adopted during the window.) A store that will not answer
is not evidence that no record exists: the issue defers untouched.

**A death is announced as `abandoned`, not as an escalation.** `applyDeath`
parks a reaped job whose attempt budget is spent as `blocked`/`escalated` — the
same resting shape an explicit engine escalation leaves — but concludes it
`abandoned`, because a death carries no diagnosis and "the engine gave up on this
task" is a statement nobody made. The conclusion is handed to the observers as it
happens and never persisted, so a sweep arriving days later tells the two apart
by the one thing that survives: the job failure class `applyConclusion` records
on the task's result (`Task.FailureClass()`). Kind **and** class are both
required — a task parked some other way whose earlier attempt happened to be
reaped still carries that class, and neither requeue nor readmit clears it.

**A queued task does not make a claim true.** `pending` is live but not
handed over, which is why the converse direction refuses to *apply* the
`in_progress` label to one — and reading an existing claim over one as
`consistent` was the same statement made backwards. It is reachable both ways
round: a reaped attempt with budget left is requeued to `pending` while its issue
still carries the label its previous attempt applied, and a re-labelled issue
whose stale claim was never cleared is adopted as a fresh `pending` task that
auto-spawn may refuse for as long as no agent can take it. The sweep still writes
nothing — the task is about to start, and moving the issue back onto the trigger
label would un-claim a run the engine is dispatching — but it reports the issue
as `undecidable` rather than certifying it healthy, so it appears in the drift
report an operator reads.

**Only the claiming labels are listed.** An issue resting on `escalated` or
`merged` with no task is the *documented correct* end state of a task that spent
its budget and released its source reservation. Listing the terminal labels too
would have the sweep post `abandoned` over every issue the factory ever finished
with.

**A readmission is a conclusion, and it needs its own direction.** `ReadmitTask`
persists `readmitted` on the record and then surfaces it; the readmitted row's
label key is *empty*, so the park label comes off and the next attempt's
`Prepare` labels the issue afresh. If that forge write fails, the record says the
task is queued again while the issue still says `factory:blocked` — and neither
other direction sees it, because the claim direction lists only `in_progress` and
`in_review`, and the hand-over direction skips pending tasks. So the sweep also
walks tasks whose persisted reason is `readmitted` and clears the label their
readmission could not, announcing nothing. It re-reads the record first: a task
readmitted and then parked *again* mid-pass has a new park whose label is correct.

**Only while the task is still queued**, and the asymmetry matters. `ReadmitTask`
writes the reason and `ClaimTask` does *not* clear it, so a task readmitted and
then claimed still reads `readmitted` — the reason describes the last thing that
concluded the task, which is what a job page wants it for. Keying the repair on
the reason alone fires on live work: the hand-over direction restores
`factory:in-progress` for a running attempt whose issue lost its label, and the
readmission direction would then strip it straight back off, leaving active work
unlabelled. So the *status* decides whether the repair applies and the *reason*
decides which repair it is, which also makes the two label-writing directions
select on disjoint statuses — `{claimed, in_progress}` against `{pending}` — by
construction rather than by luck.

**Silence is two endings, not one.** The outcome table maps both `completed` and
`runner_parked` to `OutcomeSilent`, because in both cases the moment that
concluded the task already wrote to the forge — and that is all they have in
common. `completed` means the pull request merged; `runner_parked` means the
worker stopped and posted its own park comment, so the task is **blocked** and
waiting on a person. Settling the silence on `merged` would tell the reader of a
blocked issue that their work had landed, which is worse than the stale claim it
replaced: a stale `in_progress` is visibly unfinished, while `merged` closes the
question. So the reason decides, and a runner park is restored to the label its
own `BlockKind` names, through the same `blockedLabelKey` the Blocked moment used.

**The conclusion reason is read, not re-derived.** `Task.ConclusionReason` is
persisted by the write that concluded the task and cleared by a requeue or a
readmit. That matters because two different endings rest in the *same* shape — an
engine escalation and a reaped job whose budget is spent are both
`blocked`/`escalated` — so a reader guessing from the resting status announces the
escalation's diagnosis over a death nobody diagnosed. An empty reason is not an
ending: it is what a requeue leaves, and the sweep reports `undecidable` rather
than inventing one.

**The converse direction re-establishes its premise too.** Restoration writes,
so it asks the same question before writing: the task is re-read and must still
be handed over, and the issue is re-read and must still lack a claiming label.
A task that concluded while the pass was in flight has already had its ending
posted and its issue moved onto a terminal label by `TaskConcluded`; restoring
from the snapshot would strip that label off and re-assert live work for a task
that has stopped, undoing a correct ending. Either read failing defers the issue
untouched.

**The converse is not an outcome.** A task the engine is still running whose
issue carries no lifecycle label is a lost hand-over — the `Prepare` moment's
label move failed, or the node died between claiming the task and making it.
Nothing ended, so nothing is announced; the label is re-applied and the finding
logged. A **pending** task is deliberately excluded: it is queued, not started,
and its issue correctly still carries the trigger.

#### It writes nothing of its own

The comment is `renderOutcome` reading the same table above, and the post is
`Hooks.TaskConcluded` — the whole of go-quicbaas#788's path, reached from a
sweep instead of from the engine. A reconciled ending is byte-for-byte the
ending the engine would have written itself, which is the only way the two
cannot drift. The orphan case (no task record) is the one exception: it cannot
call `TaskConcluded`, which requires the concluded record, so it renders and
posts the same `abandoned` row directly.

Because the conclusion reason is **not stored** on the task — it is computed by
`applyConclusion` and handed to the observers as it happens — the sweep reads it
back from the resting record: `completed` → `completed`, `failed` **with the
budget spent** → `budget_exhausted`, `blocked` → through orchestration's own
`ConclusionOfBlockKind`. A failed task whose budget is *not* spent was retired by
some other hand, so the sweep reports `undecidable` rather than announcing an
attempt budget that is not what failed it.

#### Idempotence and pacing

Before posting, the issue's discussion is read and searched for **this** task's,
**this** attempt's, **this** outcome's `blockedMarker` — the same identity
`runnerRun.parkPosted` checks. A sweep running every five minutes against a
forge whose write failed once must not leave a comment per pass.

**That is idempotence within one node, and only within one node.** The sweep
starts on every SDLC-enabled server and its boot pass runs immediately, so two
nodes can read the same discussion, both find the marker absent, and both post.
Read-then-write against a forge that offers no conditional create cannot be made
atomic from the daemon.

What that costs is a **duplicate, not a wrong comment**: both are the same
rendering of the same ending, the label lands in the same place from either
writer, and once either comment exists the marker is there, so every later sweep
on every node reads it and posts nothing. The duplication does not compound.

It is **deliberately left open**. Closing it needs a durable one-writer
reservation taken *before* the post, and a reservation without a lease is worse
than the duplicate it prevents — a node that dies between reserving and posting
holds the claim for ever and the outcome is never announced at all, which is
silent and permanent where a duplicate is cosmetic and self-limiting. The
reservation therefore needs an explicit TTL, which is durable state with its own
lifecycle. Specified in go-quicbaas#840. Not reachable on a single-node
deployment.

Forge calls are paced (`sdlc.label_reconcile.forge_interval`, default 200ms) by
a lock-free interval pacer. The bound is on the **burst**: a converged project
makes two listings and stops, while the first pass after this ships may find a
whole project drifted, and a forge that starts refusing under that load turns
one repair pass into a rate-limit ban for the node.

#### Failure is a finding, never a crash

A forge that refuses one issue produces a typed `LabelReconcileDeferredError`
on the finding, leaves the issue exactly as it was, and the next sweep tries
again. A project that cannot be swept at all defers as one finding and the pass
continues to the next project. Only one fault ends the whole pass —
`LabelReconcileUnsweptError`, the tenants could not be enumerated — because a
sweep that does not know what it was meant to visit must not look like a clean
run.

## Naming

"SDLC" is the company's own term (`conventions/sdlc.md`) and names what the
module does. The engine is the **Software Factory** on user-facing surfaces
and `pkg/orchestration` in code.

The earlier working name for the area is retired, and `make lint` enforces
that: `scripts/verify-naming.sh` fails when the retired word appears in **any
tracked file**, case-insensitively and with no allowlist. (The English verb
*fabricate* and its inflections are a different word and are left alone.) The
script's second rule is the `Seal*` deprecation alias surface — see
[../barrier/engineering.md](../barrier/engineering.md). It fails closed when
it cannot establish a git work tree, and it runs before golangci-lint so a
regression reports in milliseconds rather than after the long linter.

## The worker's contract is re-read on every attempt after the first

The task's `Description` is the issue contract rendered at adoption (title,
body, discussion). The first attempt is handed exactly that, rendered moments
before dispatch. Every later attempt — a readmission after a park, a resumed
run — renders the contract again from the forge at each turn
(`runnerRun.freshContract`), because the comment that unparked the task is
what the worker most needs to read: the answer to a needs-info, the operator
repair, the owner's ruling. A forge that will not answer leaves the adopted
contract in place and audits `sdlc:contract_stale`, so a worker that later
claims it was not told can be checked against the trail.

