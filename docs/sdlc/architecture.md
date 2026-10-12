# SDLC — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The SDLC module of the Software Factory: a labelled issue becomes a merged
> pull request. The engine it plugs into is
> [../software-factory/architecture.md](../software-factory/architecture.md).
> Conflicts with this document are defects.

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
[job-runners.md](../orchestration/job-runners.md).

Where the rest of the module is documented:

| Subject | Where |
|---|---|
| Every verb and flag of `quicbaas sdlc` | [cli.md](cli.md) |
| REST, gRPC, MCP, Go and TypeScript SDK, permissions | [api.md](api.md) |
| Exported surface, typed errors, the project binding, configuration | [engineering.md](engineering.md) |
| Binding a project and following one issue end to end | [user.md](user.md) |
| The review gate, reviewer profiles and the per-PR override | [review.md](review.md) |
| Triage, clustering and readiness of incoming issues | [triage.md](triage.md) |
| The merge train | [merge-train.md](merge-train.md) |
| How a red check is classified before a round is spent | [ci-failure-classification.md](ci-failure-classification.md) |
| The prompts the worker and the reviewer are given | [prompt-sets.md](../software-factory/prompt-sets.md) |
| The agents and pools a binding's `worker` and `triage` name | [../software-factory/agents.md](../software-factory/agents.md) |
| The engine's Project → Task → Job → Session model | [../orchestration/](../orchestration/) |

## Design rationale and rejected alternatives

Each decision below is argued in full in the section it links to; this table
is the index of what was chosen and what was turned down.

| Decision | Rejected alternative | Why |
|---|---|---|
| The module is a set of registrations into the engine's seams; it owns no loop, lease, budget or reaper ([the split](#the-split)) | An SDLC-specific scheduler beside the engine | Two leases and two reapers for one job disagree; `pkg/orchestration` never imports `pkg/sdlc`, and a test walks the import graph to keep it so |
| An issue is driven by the `sdlc` `JobRunner` ([the seams](#the-seams)) | A canonical SDLC Workflow on the lead-driven control loop | A Workflow's one shape is decomposition: a live run turned one issue into nine work items |
| The container commits, signs and pushes; the daemon opens the pull request ([the publish](#the-publish)) | Pushing from the daemon, or handing the container the key or the token | The private key stays in the KMS behind a per-attempt agent socket, and the token is read by a credential helper, so no secret reaches argv, the environment or a log |
| An unsigned outgoing commit is refused ([the publish](#the-publish)) | Re-signing the worker's commits before the push | Re-signing rewrites the branch (go-quicbaas#1048) |
| The forge's SSH host key is pinned on the binding ([engineering.md](engineering.md#ssh_host_key--the-forge-is-pinned-never-learned)) | Trust on first use (`accept-new`) | A worker holds a live signing socket; TOFU would hand it to whatever answered at the forge's address |
| One seam gets every runner's container to the forge ([the forge-access seam](#the-forge-access-seam)) | Each runner assembling its own spawn | That divergence is how triage spent a container per sweep learning inside `git clone` that it held no agent (go-quicbaas#929) |
| Verification judges the pull request merged with its base ([verification](#verification-judges-the-merge-result-not-the-branch-tree)) | Judging the branch tree as pushed | A branch behind a base that already fixed a test fails here and passes on the forge, and the worker spends a round on a defect `main` fixed (go-quicbaas#965) |
| A `Strategy` owns branching, merging and follow-ups as one decision ([the SDLC strategy](#the-sdlc-strategy)) | Choosing branching and merging separately | It permits combinations no SDLC describes, such as gitflow branches with squash merges |
| Strategy and forge registration are fail-closed ([the SDLC strategy](#the-sdlc-strategy)) | Falling back to a default | A project run under a strategy it did not choose pushes to the wrong ref, and retrying cannot undo it |
| The reviewer is decided on the binding ([review.md](review.md#who-reviews--project-configuration-never-a-label)) | Routing review by forge label | A label is mutable, forgeable display state, and every question about it cost an incident |
| The binding names agents and pools (`orchestration.AgentRef`) for `worker` and `triage` ([../software-factory/agents.md](../software-factory/agents.md)) | Backend, model and image fields on the binding itself | One fleet record serves every project; the removed keys are refused, not ignored ([engineering.md](engineering.md#configuration)) |
| The binding stores secret references ([engineering.md](engineering.md#invariants)) | Secret values on the record | The values stay in the tenant's secret store, and `Bind` proves each reference resolves before writing |
| Onboarding is idempotent provisioning plus a read-only conformance report ([onboarding](#onboarding-a-repository)) | Committing configuration into the repository | Nothing lands in the repository, and running `onboard` again is the repair |

The engine's Project → Task → Job → Session model, its leases and its
recovery are documented in [../orchestration/](../orchestration/) and
[../software-factory/architecture.md](../software-factory/architecture.md).

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
[../accounts/architecture.md](../accounts/architecture.md#spend-limits).

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
[job-runners.md](../orchestration/job-runners.md) and issue #621.)

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
   person or a pipeline decides. A round whose forge READ fails (the pull
   request, its reviews or its checks) is polled again on the next tick, up
   to `reviewReadFailureBound` (10) failed rounds in a row, so a forge blip
   never ends a long wait. Only a forge that keeps failing ends it, with the
   forge's own error, attributed as before. A refusal — changes requested, a red that
   stayed red past the settle window — fails the criterion with the findings,
   and the runner re-delegates to the worker (`re-delegating → executing`)
   with the findings in job memory. The binding's `max_review_rounds` rounds
   exhausted → `escalating` → the Blocked hook.

   **A settled red is classified before anyone is woken.** `pkg/cifault` reads
   each failed check's log and answers `genuine`, `void` (a runner died, a
   status write raced shutdown, the runner failed tearing down a job whose
   steps all passed — re-run it through the forge, at most
   `DefaultChecksRerunBudget` (3) times per check per head, record an
   `sdlc:ci_rerun` audit event for each, and wake nobody) or
   `host_defect` (registry, container runtime, module resolution — park for an
   operator). Only `genuine` re-delegates, and a step that ended `Process
   completed with exit code N` is always `genuine`. The runner's own error
   lines (job-container teardown, runner lost) are one table in
   `pkg/cifault/infra.go`. See
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
   — there is no second path. What it runs ON is the tree the forge's checks
   will judge, not always the branch as pushed — see
   [Verification judges the merge result](#verification-judges-the-merge-result-not-the-branch-tree).

4. **`JobRunner` — `sdlc`** (`runner.go`). The shape of an issue-driven job.
   An issue is not a DAG: there is nothing to decompose, because the issue IS
   the unit of work, and nothing for a lead to paraphrase, because the issue
   text is the contract. So the runner declares no `decomposing`,
   `scheduling` or `assigning` state, spawns exactly ONE persistent worker
   session, and prompts it with `Task.Description` — the issue contract —
   VERBATIM.

   | state | what happens |
   |---|---|
   | `initializing` | `Prepare` plans the branch, writes the `Delivery` and labels the issue; the ONE worker session is spawned in the worker Agent's `image` with the workspace spec mounted read-only. The runner delivers the forge token to the container's tmpfs credential tier ([the forge credential](../orchestration/autonomy-and-dispatch.md#the-forge-credential)), then clones the project and its `context_repos` INSIDE that container, through the runtime's exec seam (`checkout.go`), and puts the planned branch on the checkout; the host that runs the session — this daemon or a fleet node — makes only the directory the checkout is mounted through, under its own projects root ([sandbox](../orchestration/sandbox.md#agent-local-workspaces)). A **pre-flight** then proves that checkout (`preflight.go`) before anything is prompted: an unfit workspace parks the task for an operator at zero token cost — see [Autonomous projects](../orchestration/autonomy-and-dispatch.md#the-pre-flight--the-workspace-is-proved-before-anything-is-prompted). |
   | `executing` | the worker is prompted with the issue contract verbatim, plus the previous round's findings from job memory and, when the last attempt changed nothing, that attempt's turn. The answer is recorded on the job as an `sdlc:agent_turn` event whatever it was, and what the run does next is decided by whether the checkout moved — see [the worker's turn](#the-workers-turn). |
   | `validating` | reached ONLY when the turn changed something. The binding's `verification` commands, in order, through the module's `container_command` validator: the operator's `ExecutionGuard` admits each command on the DAEMON, and the command then runs INSIDE the worker's container, in the checkout its entrypoint made (`ContainerRuntime.Exec`, the same seam the delivery's git uses). Nothing verifies on the daemon; a job with no container is refused. A failure goes back to `executing` with the command's output — both streams — and counts a round. A branch BEHIND its base is verified on the merge result instead, and the round says so first (below). |
   | `reviewing` | the container pushes, then the worker's session and container are CLOSED for the wait (`releaseWorker`, go-quicbaas#938; audited `sdlc:worker_released`). On a project whose `session_rotation` is `resume` (the default) the session's conversation is read first, and the audit records `replayed_turns=` (or `resume_unavailable=`). `Reviewable` publishes; then the `forge_review` gate. `request_changes` → `re-delegating`; a plan needing no pull request (the trunk-based carve-out) goes straight to `completing`. |
   | `re-delegating` | round++ up to `max_review_rounds`, then `escalating`. The round goes back through `initializing`, which spawns a new container on the same branch and workspace. Its first prompt carries `worker_resumed_note` with the released session's conversation under `session_rotation` `resume` (the default), or `worker_released_note` under `fresh`. |
   | `completing` | `Complete` merges, bound to the reviewed head; the strategy's `PostMerge` steps run through the forge adapter (gitflow's hotfix back-merge). A pull request that [conflicts with its base](#a-merge-conflict-is-a-merge-round) (`MergeConflictError`) goes back to `executing` as a merge round (the base is merged into the branch; nothing is rebased or force-pushed); it counts a round, and it is not a failure of the work. |
   | `cleanup` / `escalating` | the session is closed and the container goes; on a non-completed exit the branch is published FIRST so nothing the worker did is lost, and `Blocked` posts the reason to the issue. |
   | ANY OTHER EXIT | a refusal after the spawn, a checkout or verification that would not run, a cancellation or a panic runs the SAME teardown, deferred (`abandonWorker` → `teardownWorker`). It publishes, closes and discards exactly as a planned exit does, and the close is issued on a context the run's own cancellation cannot reach — bounded by the discard stage timeout. Without it a refused attempt left its container running and its daemon session record behind. |

   **Only `executing` is declared session-backed**
   (`JobStateSpec.SessionBacked`). The declaration says
   which states the stall detector may CONCLUDE about: a job past its bound
   in one of them with no addressable session has lost the worker it is
   waiting on and is ended; a job in any other state is only reported. The
   worker session outlives more states than that — it is spawned in
   `initializing` and closed on entry to `reviewing`, in `cleanup`, in
   `escalating`, or on the deferred exit path any other ending takes — so
   "the run happens to hold a session here" is not the test.

   `reviewing` is NOT declared: the run holds no worker there. A job waiting
   on a verdict runs no worker turn, so it holds no container, no project
   slot and no fleet or backend slot (its lease is `Dormant`, see
   [admission-and-readiness.md](../software-factory/admission-and-readiness.md)). A verdict that
   never arrives is reported, and no worker exists whose death could strand
   it.
   `validating` is NOT declared even though its commands run in the
   container: the state's own work IS the exec, so a worker that has gone
   fails the pipeline rather than leaving the run waiting. `initializing`,
   `re-delegating`, `completing` and `cleanup` are the daemon's own work and
   each finishes correctly whether or not a worker is still there.

   The engine's `workflow` runner declares `reviewing` session-backed and
   `mergeJobStateSpec` takes the disjunction, so the stall detector reads each
   job's OWN runner's declaration (`runnerSessionBackedIn`) and falls back to
   the merge only for a runner nobody registered.

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
   | `Prepare` (initializing) | the attempt's branch is planned (`issue/<n>-<slug>` from the binding's strategy, based on `Project.Git.DefaultBranch`), the `Delivery` records it, the issue is labelled `in_progress`. A later attempt continues a branch an earlier one pushed. **Nothing is materialised on the daemon's filesystem** and the returned work directory is empty: the worker's container clones the project and the project's `context_repos` itself — see [Autonomous projects](../orchestration/autonomy-and-dispatch.md). |
   | `Reviewable` (reviewing) | the pull request opened (or refreshed on a later round) against the head the CONTAINER already published, recorded on the `Delivery` and published as Task artifacts (`url` `pull_request`, `commit` `head`), issue labelled `in_review`. A readmitted task's attempt that refreshes an open pull request takes off the park labels an earlier `Blocked` put on it; labels a person added stay. No head recorded → `NoPublishedHeadError`; the container publishes before the forge is written. |
   | `Complete` (completing) | merge as the binding's strategy plans, **bound to the head the review approved**: the forge refuses a merge whose head moved (`HeadMovedError`), retried only while the forge says not-ready (`merge_attempts`), issue labelled `merged`. When the retries are spent, one more read of the pull request decides between `MergeNotReadyExhaustedError` and `MergeConflictError`; see [a merge conflict is a merge round](#a-merge-conflict-is-a-merge-round). A merge the forge never answered is settled by reading the pull request back — see [Forge transport](#forge-transport-deadlines-retries-and-unanswered-writes). |
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

   #### A merge conflict is a merge round

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
   environment fault. The `merge_conflict` prompt goes into job memory the
   way review findings do, and `nextRound` spends a round from
   `max_review_rounds`. It names the base and the approved head and tells the
   worker to `git fetch origin <base>` and `git merge origin/<base>`, to
   resolve each conflict keeping both sides' intent (never `-X ours`,
   `-X theirs`, `checkout --ours` or `checkout --theirs`), to commit the
   merge and to re-run the build and tests.

   **The branch is merged, never rewritten.** The owner's ruling (2026-09-04,
   handbook `software/conventions/version-control.md` §5b) is absolute: no
   force push of any kind, leased or not, and no rebase, reset or amend of a
   pushed commit. The worker charter says so too. Merging the base keeps the
   published head as an ancestor of the new one, so every publication of the
   attempt's branch stays a plain fast-forward push, on every workspace kind:
   the end-of-turn push on an ephemeral workspace and the review push on a
   persistent one. The publisher has no force path at all. A push that git
   refuses as non-fast-forward means a published commit was rewritten, and
   it surfaces as a `PublishCommandError`; nothing forces past it. The
   runner prepares nothing on the branch itself: the worker runs the merge.

   The merged head then goes through validation and the review gate again,
   because an approval only covers the head it was given. A run that holds no
   worker (for example, a re-dispatched job that went straight from its open
   pull request to the gate) goes through `initializing` to get one. A pull
   request that conflicts again after every merge uses up the budget and
   escalates to a person, like a review that never converges. Each conflict
   is recorded on the job as an `sdlc:merge_conflict` event. Measured on
   go-truststrap#64 (2026-09-22), fixed in go-quicbaas#1007, and made a merge
   round in go-quicbaas#1051.

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
| stage and commit | `git add --all`; `git diff --cached --quiet` (exit 1 = staged, 0 = nothing, anything else refused); then `git -c user.name=… -c user.email=… -c gpg.format=ssh -c user.signingkey=key::<public half> -c commit.gpgsign=true commit -m …` only when something is staged |
| is there anything to publish | `git rev-list --count origin/<base>..HEAD` |
| is every outgoing commit signed by the attempt's key | `git -c gpg.ssh.allowedSignersFile=/run/quicbaas/workspace/allowed_signers log --format='%H %G? %GF' origin/<base>..refs/heads/<branch>`, run inside `Push` over the exact ref being pushed (not `HEAD`, which a worker can point at a signed ancestor), so the teardown publish is checked too; a commit passes only with status `G` **and** a signer fingerprint equal to the attempt's SSH key fingerprint (a trusted OpenPGP signature reports `G` too), otherwise `UnsignedCommitError` and nothing is pushed |
| publish | `git [-c credential.helper=…] push origin <branch>[:<target>]` |
| what landed | `git rev-parse refs/heads/<branch>` |

**The worker's own commits are signed and checked.** When the attempt
signs, the checkout writes `gpg.format=ssh`, `user.signingkey=key::<public
half>`, `commit.gpgsign=true` and `gpg.ssh.allowedSignersFile` into the
repository, so a worker that commits on its own signs with the attempt's key.
The daemon writes `allowed_signers` beside the spec, admitting exactly that
key. Before any push the publisher checks every outgoing commit against it, so
a commit the worker made unsigned, or with another key, is refused
(`ErrUnsignedCommit`) rather than published. It is never re-signed, because
re-signing rewrites the branch (go-quicbaas#1048).

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
  [The signing socket’s root](engineering.md#the-signing-sockets-root). A session placed on
  a fleet node on another host is served the same agent over the agent's
  FleetRuntime link rather than a mount — the key and the socket stay here,
  `StopSigning` still closes the window, and every relayed signature is
  audited ([sandbox.md](../orchestration/sandbox.md#agent-local-workspaces), go-quicbaas#1013).
- **The forge token never travels either.** For an `https://` remote the push
  carries a credential helper that READS the token file on the session's tmpfs
  credential tier when git asks (go-quicbaas#899);
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
binding's forge, the branch plan, the workspace key and whether its container
signs commits; the seam resolves the token, starts the ssh agent when one is
needed, **checks the credentials against the spec**, and puts the named
workspace, its decided spec and every container path on the spawn request. The
host that runs the session provisions the workspace and writes the spec — see
[sandbox.md § Agent-local workspaces](../orchestration/sandbox.md#agent-local-workspaces).

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
   [job-runners.md](../orchestration/job-runners.md).

## The worker's turn

Every prompt handed to the worker and every answer it gave is recorded on the
job as an `sdlc:agent_turn` event. The event carries the attempt, the review
round, how long the turn took, a census of the content blocks the backend
published (tool use included, where the backend publishes it), the stop
reason, the token accounting, **the classification** (`outcome=`) when the
turn changed nothing, and **the reply itself** — truncated at
`TurnReplyLimit` (4096 bytes) with the cut marked, never dropped.

**Tool use is counted from the backend's census, not from `Content`.** Most
backends put only the assistant's text in `PromptResponse.Content`, so
counting blocks alone reads a turn that ran tools as one that ran none
(go-quicbaas#799). `PromptResponse.ToolUse` (a `ToolUseCensus`, beside
`Usage`) carries the count, and `ReadAgentTurn` records it as the
`tool_use` block count. Like `Usage`, nil and zero differ: a zero census is
a backend reporting that the turn used no tool, and is recorded as
`tool_use:0`; a nil census means the backend does not report tool use, and
no `tool_use` entry is recorded.

| Backend path | Census |
|---|---|
| ACP persistent stream-json | counts the assistant messages' `tool_use` blocks |
| ACP JSON-RPC | counts `tool_call` session updates (`tool_call_update` is progress, not a call) |
| OpenVINO | counts the tool calls it ran; also adds a `tool_use` block per call to `Content` |
| ACP one-shot and exec, codex exec, Ollama, API inference, remote ACP | nil: their output carries no tool record this package parses |

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

## Verification judges the merge result, not the branch tree

Verification and the merge gate used to judge **different trees**. Verification
ran the binding's commands on the branch exactly as the container held it; the
forge's "Merge Result" job runs its checks on the pull request **merged with
its base**. A branch one commit behind a base that had already fixed an
environment-sensitive test therefore failed here and passed there, and the
round of feedback the worker got was about a defect `main` had already fixed.
Measured on go-quicbaas#965 (2026-09-22): six minutes and one of the attempt's
turns spent merging `origin/main` to make the same commands pass — and the
same thing had cost #966 a round earlier that evening.

So before the binding's commands run, the runner **refreshes the base and
counts the branch against it** — `git fetch origin <base>`, then
`git rev-list --count HEAD..<the commit that fetch landed on>`, in the
container, like every other git this module runs (`merge_result.go`).

The refresh is not optional. The container fetched the base when the attempt
was *initialized*, before the worker was prompted; a base that moves during
the turn — which is exactly what happened to #965 and #966, `main` fixing the
environment-sensitive test mid-attempt — is invisible to that ref, and a count
against it answers "level" for a branch that is not. Everything downstream
names the **commit** the fetch landed on rather than the ref, so a base that
moves again between the count and the merge cannot have verification judge a
tree it never counted.

| what it finds | what happens |
|---|---|
| **level with its base** (the overwhelmingly common case) | nothing. No worktree, no merge, no cleanup; verification runs exactly as it did before, one fetch and one count the poorer. |
| **behind by N**, checkout clean | a throwaway worktree is added beside the checkout (`.merge-result-<project>`), `origin/<base>` is merged into it **`--no-commit --no-ff`**, and the binding's commands run there. The worktree is removed the moment verification ends. |
| **behind by N**, merge conflicts | `MergeResultConflictError`, naming the conflicting paths. It is a REFUSAL, not a verification failure: a branch that will not merge says nothing about the work, and reporting it as a failed build would send the worker looking for a defect that is not there. Only a merge git left unmerged paths for is reported this way, and the paths are read back with `git diff --name-only --diff-filter=U`. |
| **behind by N**, merge fails for any other reason | `PublishCommandError` naming the merge — a worktree that is not there, a git configuration the image refuses, a full disk. Nothing about such a branch needs reconciling, so calling it a conflict would send the worker after conflicting paths that do not exist. If the unmerged-paths probe is what failed, its own failure travels with the merge's rather than being read as "no conflicts". |
| **behind by N**, checkout holds uncommitted work | the working tree is snapshotted into a commit **object** — a **copy of the worker's own index** beside the checkout (`GIT_INDEX_FILE`), `git add --all` staged into it, then `write-tree` and `commit-tree` parented on `HEAD` — and the worktree is made from that snapshot. The copy is what makes the snapshot the tree the delivery would commit: `git add --all` into an index that starts EMPTY skips every file the repository tracks despite an ignore rule (seven here, the vector-store markers), which would record them as deleted and have verification judge a tree missing files the branch never touched. What the snapshot tree actually holds is measured against real git by `make integration-test-merge-result`, which CI runs on every push and pull request as the `Merge-Result Snapshot` job; the unit suite holds the command plumbing. The merge result is therefore the worker's actual work merged with the base, uncommitted changes and new files included. No ref moves, the worker's own index and working tree are untouched, and the snapshot is never signed: it is scaffolding nobody will pull, review or verify. |

**The pushed branch is untouched and the worker still owns git.** Nothing here
commits, amends, rebases or pushes: the merge exists only inside a worktree
that is thrown away, and the merge gate — not this — is what decides whether
the merge lands.

**The throwaway worktree is removed on a context detached from the stage's**,
bounded by the same discard timeout every other detached cleanup in the runner
takes. A verification that was cancelled or timed out is exactly when the
removal matters: the checkout outlives the job on a persistent workspace tier,
so a worktree left behind is the next attempt's problem. "There is no such
worktree" is told from "the removal failed" by asking git what worktrees it
holds (`git worktree list --porcelain`) rather than by reading an exit status,
and a removal that failed is reported — on the job as `sdlc:verification`, and
to the caller as a typed `PublishCommandError`. Nothing here is swallowed.

Verifying the checkout itself when it is dirty was the earlier behaviour, and
it left #982 open where it bites hardest. `PushTurn` commits and pushes only on
the **ephemeral** tier, where the container's tmpfs is the sole copy of the
work; off it the checkout stays dirty between turns by design. So "dirty,
therefore judge the branch as committed" was not a corner case on a persistent
workspace — it was every attempt, each one still paying the wasted round for a
failure the base had already fixed.

Both throwaway artefacts — the worktree and the snapshot index — are removed
together when verification ends, and both answers are kept: the one that could
be removed is not abandoned because the other could not. The index is removed
with `rm -f`, the one undo here git cannot do, since the file belongs to no
repository; a path that was never there exits zero, so an ordinary cleanup is
never mistaken for a failure.

**The "behind by N" line leads the round's evidence**, in front of the failure
output and inside the existing bound (salient lines + the last 16 KiB). A
worker that reads a test failure first starts by fixing the test; a worker that
reads *"Your branch is 1 commit behind `main`; rebase or merge before fixing
anything below"* first knows to look at the base before it writes anything.
Which tree was judged is on the job as `sdlc:verification` with `behind=`,
`base=` and `judged=merge_result|branch_tree`, so a verification that ran
somewhere other than the branch as pushed is never silent.

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
  (`VersionConflictError`) and the writer re-reads. The store decides the
  guard, not the caller: a create lands only through `PutIfAbsent` and an
  edit only through `CompareAndSwap` over the exact bytes the writer read, so
  of N writers holding the same `Version` exactly one wins. `PutDelivery`
  re-reads and retries when it loses, so every replacement gets its own
  version. A tenant store that cannot perform atomic writes is refused
  (`ErrGuardedWriteUnsupported` inside a `BackendError`) rather than guarded
  with a read and a plain put. Bindings, train entries and train ledgers are
  written the same way. Deletes are guarded too: deleting a binding, train
  entry, reviewer override or issue cluster goes through `CompareAndDelete`
  over the exact bytes the delete read and checked. A record written after
  that read is kept and the delete is refused (`VersionConflictError`), so a
  delete never erases a write it did not see. A record another delete
  already removed counts as deleted. Reviewer overrides and issue clusters
  are blind upserts: each put replaces the whole record (an override only
  carries its `CreatedAt` forward), so a racing put has no stale read to
  merge. An override and the binding it depends on are two keys, and the
  store guards one key at a time, so the read decides instead: a binding's
  `Epoch` is drawn at random when the project is bound and kept across
  updates, an override records the epoch it was set under, and
  `SelectReviewer` ignores an override whose epoch is not the binding's. An
  override put that passed its binding check before an unbind and wrote after
  it therefore never reaches the project's next binding.

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
the token secret must resolve, and the webhook secret when named; the
binding must name a registered strategy and a registered forge adapter; and
every `verification` command must be on the operator allowlist
`orchestration.verification_commands` — refused with
`VerificationCommandRefusedError` naming the index and the command (400 over
REST, `InvalidArgument` over gRPC). Every refusal leaves no record behind.
`Onboard` makes the same allowlist check on the stored binding, so a binding
written before the allowlist narrowed is refused there too.

The allowlist is checked a third time **before a worker is spawned**: a task
whose binding carries a command the node refuses is parked `blocked` with the
operator fault class `config_error` (condition `operator_repair`), so the
attempt is refunded and no worker turn is paid for. The execution guard's own
refusal at validation — an allowlist narrowed while the job ran — parks the
same way instead of failing the attempt. Before this, go-blockstore's binding
named `make fmt-check vet golangci-lint guards`, was accepted, and was refused
after twelve paid worker turns, each charged as an attempt (X14).

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
                 ├─ approved + green, trained project ─► queued; waits in reviewing for the merge train
                 │     ├─ landed ──────► completing ── Complete ─► own PR closed naming the landing, label merged
                 │     ├─ ejected red ─► re-delegating with the train's red (round counts)
                 │     └─ ejected conflict ─► executing: merge round
                 └─ approved + green ──► completing ── Complete ─► merge (head-guarded), label merged
                                                    ├─ conflicts with base ─► executing: merge round, base merged in (round counts)
                                                    └─ strategy PostMerge steps (gitflow back-merge)
   escalating ── Blocked ──────────────► comment on the issue, label per kind; Task blocked
   cleanup ── Discard ─────────────────► nothing on disk to reclaim; the Delivery records the attempt

   the job is over ── the ENGINE concludes the Task ── TaskConcluded ─┐
     budget spent → failed          ─► comment `exhausted`,  label escalated  (no readmit)
     node died, no attempts left    ─► comment `abandoned`,  label escalated
     environment fault held/bounded ─► comment `blocked`,    label blocked
     attempts left                  ─► requeued; nothing concluded, nothing posted
   readmit ─────────────────────────► comment `readmitted`, park label → trigger (queued)
```

A trained project's approved changes land through the merge train, CI
running once per batch rather than once per change; see
[merge-train.md](merge-train.md). An issue triage clustered with related ones
is delivered by its anchor's task, whose pull request closes every issue in the
cluster; see [triage.md](triage.md#clustering--related-ready-issues-become-one-task).

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

## Issue priority: blast radius, shifted by `priority:*` labels

An adopted issue's task priority is the project's computed priority (its
blast radius × 10, plus the binding's `ranking.default_priority`, clamped to
0–999), shifted by the most urgent `priority:*` label the issue carries:

| label | shift |
|---|---|
| `priority:critical` | +600 |
| `priority:high` | +300 |
| none, or `priority:normal` | 0 — exactly the computed priority |
| `priority:low` | −300 |

The shifts outrank blast radius on purpose, so project-management priority set
in the forge actually orders the work: a `priority:critical` issue in a project
nothing depends on dispatches before an unlabelled issue in a heavily
depended-on one. Labelled issues may leave the 0–999 band so that `low` sorts
below every unlabelled issue and `critical` above every one. Label matching
ignores case; an issue carrying two tiers is ranked by the more urgent.

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
(`command=`, `passed=`, `exit_code=` for a command that ran and failed,
`output_bytes=`) on the job, whether or not the pipeline went on. A passing command's record adds its first line; a failed
one's adds, on the lines after the header, its failing lines — the lines
naming a failure (`--- FAIL`, `FAIL\t`, `panic:`, `error:`, `Error `,
`make: *** `) from ahead of the tail, then the last
`VerificationSummaryTailLines` lines of each stream, each line cut at
`VerificationSummaryLineBytes` — because a first line can be harmless noise
(go-luks2#205). When verification failing spends the round budget, the
escalation carries the same lines under a head naming the command and its
exit status (`` `make lint` exited 2: ``), sized to the 1200 bytes the posted
comment keeps, failing lines first. The output is redacted whole before any
of it is selected or cut. The most common way a round-1 prompt used to overflow was the
verification feedback itself: the whole `make test` output went into the
prompt. It is now the lines that name the failure (`--- FAIL`, `FAIL`,
`panic:`, a compiler's `# pkg` header and `file.go:line:col:` diagnostics,
`make: ***` — up to `VerificationSalientLines`, gathered from the whole
output) followed by the output's last 16 KiB (`VerificationEvidenceBytes`,
whole lines, omission stated); the full output stays on the job's
verification record. **Once per round**: a fresh session that fills again on the
same prompt has a prompt the work cannot take, and the run fails with the
CLI's reason, charged like `prompt_too_long`. Nothing in this group refunds
an attempt — see [../accounts/architecture.md](../accounts/architecture.md#attempt-accounting) for the refunded set, which is
now `api_error` and `turn_setup_failed` only.

## A refused account rolls the session onto another account

The same mechanics answer a turn the provider refused because the worker's
pooled account hit its subscription limit (go-quicbaas#1046). The
orchestration layer has already held that account until the provider's stated
reset. The runner then pushes, closes, and spawns a fresh session on the same
branch in the same round, and the fresh spawn selects a different account.
The attempt is not charged. This roll is checked **before** the context
rollover, because a refused account makes every other reading of the failure
beside the point.

What the new session keeps follows the project's `session_rotation`, exactly
as for a rotated credential below: the conversation is read from the refused
session **before** it is closed, through the same carry-over.

| `session_rotation` | The new session | Audit event |
| --- | --- | --- |
| `resume` (default; also an unset project) | continues the conversation; its first prompt carries the `account_resumed_note` template with the bounded transcript | `sdlc:session_resumed` with `replayed_turns=` and `omitted_turns=` |
| `fresh` | starts a new conversation, told only why by the `account_rolled_note` template | `sdlc:session_rolled` |

Both events carry `class=account_limited`, `account=`, `available_at=`,
`round=` and `session=`. A resume whose conversation cannot be read starts
fresh with `resume_unavailable=`, and any other read failure fails the turn,
as described below.

Each account is rolled off at most once per run. If a fresh session lands on
an account that was already refused, the error is returned, classified
`account_limited`, refunded and requeued. When no account in the pool is
free, the fresh spawn fails with the pool's cooldown, and the task waits,
uncharged, until the first hold lifts. See
[../accounts/architecture.md](../accounts/architecture.md#rotating-a-session-onto-another-account).

## A rotated account resumes the session, or starts it fresh

A session materializes its pooled provider credential once, at spawn: the
access token into the container's process environment, the provider's config
files into a per-session directory. Neither is re-read. When the pool replaces
that credential — the host refreshes the login (on the burn-in fleet roughly
every eight hours), an `oauth_refresh` renewal lands, or the tenant secret a
referenced account reads is rewritten — the copy the session holds is no
longer what the provider honours. Its next prompt dies in about two and a half
seconds as an `api_error`, which the runner refunds and re-dispatches: the
round's work is done over in a fresh job. Measured on go-quicbaas #965, #980
and #982 (2026-09-22), each losing a review round to a refresh.

**The pool is asked before the turn is spent.** Before every prompt the runner
asks the orchestrator whether the credential this session materialized is
still the one its account holds (`SessionAccountFreshness`). The comparison is
between two **credential generations**, the copy the session recorded when its
material was produced and the copy the account's authority holds now (the
envelope, or a referenced account's secret version), so a rotation that lands
while a container is still starting is visible. A remote worker is checked the
same way: the generation travels on the released credential to the fleet host
and back on the spawn response. See
[../accounts/architecture.md](../accounts/architecture.md#credential-generations). A session
that carries no generation — one spawned before stamping existed — falls back
to the account's `CredentialRotatedAt` against the start of its current
account segment.

**A left-behind session is replaced; the project decides what the replacement
keeps.** The only way onto the current credential is a new container, so the
first three steps are the full-context rollover's: push what the session
committed, close it, spawn again on the same branch, in the same workspace and
the same round. The attempt is not charged and the round does not advance.
What the new session is handed is the project's `session_rotation` setting
(see [session rotation](../projects/user.md#session-rotation)):

| `session_rotation` | The new session | Audit event |
| --- | --- | --- |
| `resume` (default; also an unset project) | continues the old session's conversation: its transcript is read from the durable session log **before** the old session is closed and carried into the first prompt with the `session_resumed_account_note` template | `sdlc:session_resumed` with `replayed_turns=` and `omitted_turns=` |
| `fresh` | starts a new conversation, told only why it is new by the `session_rolled_account_note` template, and reads the branch | `sdlc:session_rolled` |

Both events carry `reason=account_rotated`, `account=`, `round=`, `session=`
and the evidence the verdict rested on: `session_generation=` /
`account_generation=`, or `rotated_at=` when timestamps decided it.

The carried conversation is the same bounded transcript a restored session is
primed with — the prompts and the agent's answers, oldest turns dropped first
to fit the replay budget, with the number dropped stated — so a resume never
hands the worker a silently truncated history. Tool calls are not replayed;
the branch holds their effects. A session replaced before it was ever prompted
has no conversation and is told only why it is in a new container.

**A resume that cannot read the conversation starts fresh.** When the agent
service keeps no session log, or the log will not answer
(`SessionTranscriptUnavailableError`), the session is replaced fresh rather
than prompted with a credential the provider will refuse, and the
`sdlc:session_rolled` record says so with `resume_unavailable=`. Any other
failure to read the conversation — the session is unknown to the service —
fails the turn, because a fresh session does not answer it.

**Once per round, counted apart from the other rollovers.** The replacement
holds the material the pool holds now, so a second report inside one round is
the pool flapping rather than the session being behind; the run prompts rather
than spawning containers without end.

**Fail-soft in one direction only.** Every case where the question cannot be
answered — a deployment running no pooled accounts, an agent service that is
not a freshness reader, an account or secret that cannot be read, a session
record with neither a generation nor a datable pair of timestamps — answers
"not stale" and the turn proceeds exactly as it did before.

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
`sdlc.forge` (see [Configuration](engineering.md#configuration)); zero takes the default.

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
| `readmitted` | `orchestrator:task_readmit` | `readmitted` — *This task has been readmitted* | park label → trigger (the display state of queued work, until the next attempt starts) | n/a |
| `superseded` | the attempt's pull request was closed without merging, seen by the review gate or on resume | `superseded` — *This task's pull request was closed without merging* | every owned label removed | **no** — the task is completed; re-adding the trigger label adopts a new task |

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
| a park's label | task readmitted, **still queued** | `relabelled` | move it to the trigger, post nothing |
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
walks tasks whose persisted reason is `readmitted` and moves the park label their
readmission could not onto the trigger, announcing nothing. It re-reads the record first: a task
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
that: `scripts/verifynaming` fails when the retired word appears in **any
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

