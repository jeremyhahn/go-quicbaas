# The merge train — approved changes verified together, landed together

> **Status: REFERENCE** · Last verified: 2026-10-11
> Supplement to the [SDLC feature folder](architecture.md): the merge
> train that verifies approved changes together and lands them together.

A project whose target branch refuses an out-of-date pull request (Gitea's
`block_on_outdated_branch`) pays for every merge twice. Each merge makes every
other open pull request stale; each of those merges the target in, gets a new
head, and runs its whole pipeline again — about an hour a time. Merging `n`
approved changes one at a time costs `n` more pipelines after they were
already green.

The merge train lands them together. Approved pull requests are queued; a
train takes up to `max_size` of them, CI runs **once** on the stack, the
project's reviewer reviews the stack as a whole, and the stack lands through
one merge.

The train is QuicBaaS's own (`pkg/sdlc/merge_train*.go`), not a forge
feature. Gitea has no merge queue, GitHub's is GitHub-only and GitLab's merge
trains are a paid tier. Everything the train does is branches, pull requests,
check runs, commit statuses and a merge call — verbs every forge adapter
already has — so it behaves the same on every platform.
`ForgeCapabilities.NativeMergeQueue` records which platforms could queue on
their own (false for Gitea, and for GitHub, whose REST API has no merge-queue
route — its queue is driven only through GraphQL and branch rulesets; true
for GitLab, whose merge-train route exists on the tiers that carry it);
nothing reads it yet.

## When a project is trained

`merge_train` on the binding. **On by default**, which is why its switch is
`disabled` rather than `enabled`: a binding written before trains existed is
trained.

```json
"merge_train": {"disabled": false, "max_size": 5}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `disabled` | `false` | merge approved pull requests one at a time instead |
| `max_size` | `0` → 5 | changes per train, at most 25 |

**A project with no reviewer profile that counts toward merge is not
trained**, whatever the policy says (`ProjectBinding.TrainEnabled`,
`TrainOffReason`). The train's pull request lands only on a genuine verdict of
that reviewer on the train's own head (see [review](#review)); a project
reviewed by people, or only by advisory profiles, has nobody to ask, so its
changes merge one at a time as before. The reason is recorded as
`sdlc:train_off` — on the delivery that merged alone, and on a train job that
formed nothing — and `quicbaas sdlc binding` shows it.

The train's reviewer is `ProjectBinding.TrainReviewer`: the first profile, in
binding order, whose verdict counts toward merge. The default profile comes
first, so on an ordinary binding it is the same reviewer every other pull
request gets.

A node whose module was built without a train refuses a trained delivery with
`TrainUnavailableError` rather than merging it on its own: the binding says
how the project lands its changes, and a node does not overrule it.

## The delivery's side

The delivery runner changes in one place. When the review gate approves a
head (`review` in `runner.go`), a trained project **queues** it instead of
going to `completing`, and the job waits in `reviewing` — off the project's
worker budget, with its worker still there — for the train's answer:

| The train says | The delivery does |
| --- | --- |
| `landed` | `completing`: `Complete` records the landing, closes the change's own pull request with a comment naming the merge commit and the train pull request, deletes the change's source branch when its branch plan says it goes once landed (a closed pull request never triggers the forge's delete-on-merge, and a leftover branch refuses the next task's push for the same issue), moves the issue to `merged`, and deletes the entry |
| ejected `checks_failed` | a worker round with the train's red (`train_ejected` prompt); the fixed head is reviewed again and rejoins the queue |
| ejected `review_refused` | a worker round with the train reviewer's findings (`train_review_refused` prompt); only a train that carried this change alone ejects it this way |
| ejected `conflict` | a merge round, exactly as a conflicted merge is (`merge_conflict` prompt) |
| ejected `head_moved`, `unreviewed` | back to `reviewing`: the gate judges the pull request as it is now |
| ejected `closed` | the pull request is read once: merged → `completing` as landed; closed unmerged → the attempt ends `superseded`; open again → back to `reviewing` |
| ejected `unsigned` | parked `escalated` for a person: the change carries a commit no trusted key signed, and neither the gate nor another train can change that (see [Rotating the signing key](../projects/user.md#rotate-the-signing-key)) |

The queue entry (`TrainEntry`, one per task) is the module's record of the
train's decision; the delivery polls it. The one forge read in the wait is for
an entry no train carries (queued, or ejected for any reason): a pull request
merged or closed outside the train leaves the queue **for good** — merged goes
to `completing`, closed unmerged ends the attempt `superseded` — and is never
queued again. An approval of a pull request the gate found already merged is
not queued at all. Before this, a merged car was ejected `closed`, passed the
gate, was queued again and ejected again, one train every 30 seconds (X15:
go-luks2 PR #210, trains #79 to #152). Queueing is
idempotent on the head: a resumed job re-approving the same head keeps its
place and never pulls it out of a train already testing it.

If the project's train is turned off while a change waits in the queue, the
delivery withdraws it and merges on its own. A change already riding is left
to its train.

## The train's side

A fourth JobRunner, `merge_train` (`merge_train_runner.go`), one job per
project at a time by lease (`merge_train:<project>`), off the project budget.

| State | Label | What happens |
| --- | --- | --- |
| `initializing` | Forming train | resume the train in flight, or form the next: the queue in order (first queued first, ties by pull request number), same target as its head, up to `max_size` |
| `executing` | Building train | a container builds `queue/<n>` and pushes it (see below); the container is closed before the wait |
| `validating` | Testing and reviewing train | a pull request `queue/<n>` → target is opened so the target's own CI runs on it; the pipeline is read until it settles; then the verified prefix's pull request is reviewed (see [review](#review)) |
| `completing` | Landing train | the culprit (if any) is ejected, the verified prefix lands, the rest are requeued |
| `cleanup` | Cleanup | release everything; form the next train if anything is queued |

**Why `validating` and not `reviewing`.** A state's bounds and session-backing
are merged across every runner that declares it. The delivery runner declares
`reviewing` session-backed, so a train waiting there with no session would be
read by the stall detector as a job whose agent is gone and ended. No runner
declares `validating` session-backed: a train past its bound is reported and
left running. And no train bound is wider than the delivery runner's for a
shared state, because the merge takes the longest.

### Building

In the train's own container, through the same forge-access seam every runner
holds, at the delivery signing scope — each merge is a commit, signed by the
project's key exactly as a worker's is:

1. Check out the target on a new local branch `queue/<n>`; its head is the
   train's `base_sha`.
2. For each car, read its pull request first: closed or moved since it was
   queued is an ejection before any pipeline is spent. Then fetch its branch,
   check the fetched head is the approved one, and `git merge --no-ff` it.
   A conflict is **aborted, never resolved** — resolving it would put a line
   in the train nobody approved — and the change is ejected naming the files.
   Before merging, the commits the car would bring are checked against the
   project's trusted signing keys; a car carrying one no trusted key signed is
   ejected `unsigned`, naming the commit, and the rest of the train rides on.
   Checked only at the push, one such commit abandoned the whole train and the
   next train failed identically, for ever.
3. Push `queue/<n>` through the one publication path (every outgoing commit's
   signature verified, a plain push, never a force). The ledger is re-read
   immediately before: a train the ledger no longer names is not pushed.

A merge, never a rebase or a squash: each approved head stays an ancestor of
the train, so what CI tests is exactly "the target plus these changes".

### Testing and bisection

Each prefix under test is a **probe**: the full train on `queue/<n>`, a
shorter prefix on `queue/<n>-<k>` cut by the forge at that prefix's commit.
Each gets a pull request, because a pull request is what the target's CI runs
on. Green and red are the gate's own definitions (`ChecksGreenForReview`,
`evaluateChecksFailure`), with every reviewer's own status set aside, and a
red is read only once it has settled.

A settled red is triaged as the gate triages one (`TriageChecksFailure`):

- **void** (a runner died, a job was cancelled) is re-run, bounded by
  `DefaultChecksRerunBudget`; the train waits for the new answer.
- **host defect** stops the train: its changes go back to the queue, nobody
  is ejected, and the job fails with `CIHostDefectError` for an operator.
- **genuine** is the change's own red, and feeds the planner — unless it is
  environmental (below).

**Environmental reds** (`merge_train_environment.go`). A check that stalls on
a loaded runner reads as genuine to the classifier. A genuine red is instead
judged the environment's when, for every failed check, both hold:

- the check passed on each car's own head in its pull request CI;
- the train's run of it timed out, was cancelled, or ran more than
  `EnvironmentalSlowdown` (3) times as long as that passing run. A Gitea run's
  length is measured from its pending status to its final one.

An environmental red re-runs the failed checks on the same commit (the same
cars) **once per train**: no ejection, no review round, audited as
`sdlc:train_environment_retry`. A second red ejects as any red does, and the
ejection's detail names the suspicion. A forge that cannot re-run ejects at
once and says so; one that refuses the re-run it advertises is a host defect.

The planner (`MergeTrain.observe`) bisects over prefixes. A green prefix
raises `good`, a red one lowers `bad`, and while they are more than one apart
the midpoint is tested. When `bad == good + 1`, car `bad` is the first change
whose addition turned the train red. A fully green train needs one probe;
finding one culprit among five needs at most three more.

### Review

A green pipeline is not a review. Each change in the train was approved at its
own head, but what lands is their union on the current target, which no
reviewer has read — and handbook `sdlc.md` §5 asks for an active APPROVE on
the head that lands. So once the planner has decided what lands, the train
moves to phase `reviewing` (still the `validating` state) and:

1. Ejects the culprit, if there is one, straight away: CI has decided about
   it, and its worker should not wait on a verdict about other changes.
2. Asks the train reviewer for a verdict on the verified prefix's pull request
   (`queue/<n>`, or `queue/<n>-<k>` after a bisection) by starting the
   ordinary review job, with that profile, through the same start path the
   dispatcher uses. The request is recorded on the probe
   (`review_requested_at`, `review_job_id`, `review_asks`) only once the
   engine accepts it. A start refused because a review of that pull request
   already holds its lease is not a failure: the next pass asks again.
3. Every `PollInterval`, reads the recorded review job's state and then the
   reviewer's standing verdict on exactly the prefix's head, through
   `standingVerdict` — the review runner's own definition of a head already
   judged. A verdict on any other head is not the train's.
4. **Asks again when the review ended with no verdict.** A job that has
   reached a terminal state (or that the engine no longer knows) while no
   verdict stands on the head ended without deciding; the job state is read
   before the verdict, and a review posts its verdict before its job ends, so
   this is never a verdict still in flight. The train asks again through the
   same start path, recording the new job and audit event
   `sdlc:train_review_reasked`, up to `MaxTrainReviewAsks` (3) review jobs
   per head. A job whose state cannot be read fails the train job; the train
   stays on the ledger, still waiting.
5. **Holds while the reviewer has no usable account.** Before it starts a
   review job, the train asks the dispatcher's own account check. When every
   account the reviewer's Agent could use is errored, revoked or over budget,
   no job is started and no ask is counted. The train audits
   `sdlc:train_review_held` and posts a pending review status carrying the
   reason on the head, once per reason. Nobody is ejected, and the train asks
   again on its next pass (see [review.md](review.md)).

The review job itself is the one every pull request gets: the reviewer's own
container, identity and token; the verdict and the required status
(`quicbaas/review`) posted by the review runner. The only difference is the
prompt: in place of the issue, the reviewer is told it is merge train `#n`,
which changes it carries at which approved heads and which issues they close,
that each was already approved on its own head, and that what is left to judge
is how they combine (`train_review` prompt). **The train never writes a review
or a review status of its own.**

**Approved** → `completing`.

**Changes requested** → the train is abandoned:

- Every change it carried gets a comment on its own pull request with the
  reviewer's findings and a link to the train's pull request, saying its own
  approval still stands.
- Each goes back to the queue **in its place, keeping its approval**, bounded
  to a train half the size (`TrainEntry.max_train`, `⌈n/2⌉`). A queue whose
  head carries a bound forms a train no longer than it, so a refused
  combination is judged again in smaller parts, each on a verdict of its own.
  A new head replaces the entry, and the bound with it.
- **A train that carried one change** is the exception: the findings can be
  about nothing else, so that change is ejected `review_refused` to its worker
  with them, as a red pipeline ejects its culprit. The train never guesses a
  culprit out of a larger train's findings.
- Cars after the culprit go back to the queue unbounded; they were never part
  of the reviewed combination.

**No verdict after every ask** → the train is abandoned without ejecting or
bounding anyone, since nothing was found wrong: every change it carried gets a
comment on its own pull request saying the train's review never produced a
verdict, and goes back to the queue in its place with its approval standing
(`sdlc:train_review_exhausted`).

A binding that loses its counting reviewer while a train waits abandons the
train; its changes return to the queue and, the project now untrained, merge
one at a time.

### Landing

The verified and approved prefix `good` lands; car `bad` (already ejected at
review) is ejected with the red of the smallest red prefix as its findings;
every car after it goes back to the queue, keeping its place — none of them
was ever verified without the culprit. A train whose first change is the
culprit verifies nothing, so it is not reviewed and lands nothing.

Before landing, the train re-reads what the landing rests on:

- **The target is still `base_sha`.** If it moved, nothing is ejected — it is
  nobody's fault — and the train is abandoned; the next one is built on the
  target as it now is.
- **Every change in the prefix** is still open, on its approved head, and its
  head still carries the reviewer's `success` status. One that is not is
  ejected (`head_moved`, `closed`, `unreviewed`) and the train abandoned.

Then:

1. **Merge** the probe's pull request through the forge's ordinary merge, by
   the project's own strategy (`PlanMerge` — squash for github-flow), bound
   to the tested and reviewed head. The commit message lists every change and
   closes every issue. The target's branch protection — up to date, required
   contexts, the reviewer's approval, signing — is therefore enforced on
   exactly the commit that lands.
2. **Record** every car as landed with the merge commit and the train pull
   request. Each delivery then closes its own pull request (see above).
3. Close the other probes' pull requests and delete the train's branches.
   A forge that refuses this clean-up is audited, never fatal: the decision is
   already on the entries.

### Why this landing, and not another

Both alternatives keep branch protection honest only by running CI more than
once:

- **Update each pull request to the train state and merge them in order.**
  Each pull request's head would become a train commit that no pipeline ran
  on, and the target requires a green pipeline on the head that merges — so
  every one would need its own run.
- **Fast-forward the target to the train head.** A protected target refuses a
  direct push from the factory, and a push bypasses the checks the forge
  enforces on a merge.

Landing the train's own pull request runs CI once and lets the forge enforce
every rule on the commit that actually lands. Each change's pull request is
closed rather than marked merged because "mark merged" is not portable (GitHub
has no such call; Gitea's needs a per-repository setting), and the module's
own record — the entry, then the delivery — is what says the change landed.

## Restart

Every decision is on the **ledger** (`TrainLedger`, one per project, written
under a version guard the store decides with `PutIfAbsent` /
`CompareAndSwap`, so two train passes holding the same version cannot both
write) before it is acted on:

- interrupted in `validating`: the same probe, the same pull request; nothing
  is rebuilt and no second pull request is opened.
- interrupted while `reviewing`: the wait resumes on the same pull request and
  head. A review the train already asked for (`review_job_id`) is not asked
  for again while it runs; its verdict is read from the forge when it
  arrives. A re-ask is on the ledger before anything waits on it, so a
  restart during one waits on that review and counts it toward the bound. A
  refusal interrupted half way is finished, not repeated: every comment is
  posted once by its marker, and every entry is written only while it still
  rides the train.
- interrupted in `completing`: the same landing decision; a train the forge had
  already merged is recorded, not merged again.
- interrupted mid-build: the half-built train is abandoned (its container died
  with the node) and its changes form the next train under a new number — a
  train branch name is never pushed twice.
- an entry left `riding` a train that is not in flight is returned to the
  queue when the next train forms.

## What wakes a train

A queued change nudges `TrainScheduler.NudgeTrain`, which starts one train
job; a train already running is the lease's refusal and is ignored, because
the running train forms the next one when it ends. **Formation is rate-limited
per project**: the scheduler starts at most one train job per project per
`DefaultTrainFormationInterval` (1 minute); a nudge inside it starts nothing,
and a failed start does not hold the interval. A delivery whose entry is still
queued nudges again on every poll of its wait, so a nudge the limit swallowed
is retried a poll later rather than at the backstop. A backstop sweep
(`sdlc.train_sweep_interval`, default `5m`) starts a job for every trained
project with a change queued or a train in flight — which also resumes a train
whose job died.

## What review does with a train

A pull request from the project's own `queue/` branch is reviewed **only while
its train is waiting on that verdict**: the train ledger's active train is in
phase `reviewing` and its verified prefix is this pull request at this head.
Anything else — a prefix still being bisected, the red full train, a train
that has ended — is skipped with no status written, because it will never
land. And it is reviewed only by the train reviewer's profile; a job started
under another (an operator's override, an advisory default) skips it.

Whichever path starts the job — the train's own request, the check-run
delivery that reported the pipeline green, or the review sweep — the ledger
decides. On an ordinary binding the sweep therefore also stands under the
train's wait as a floor. A fork's branch of the same name is reviewed as any
change is.

## Audit events

| Event | Recorded by | Details |
| --- | --- | --- |
| `sdlc:train_queued` | delivery | pull request, head, target, state |
| `sdlc:train_landed` | delivery | train, merge commit, train pull request |
| `sdlc:train_ejected` | delivery | train, reason, head, detail |
| `sdlc:train_merged_outside` | delivery | pull request, entry state, head |
| `sdlc:train_closed_outside` | delivery | pull request, entry state, train, head |
| `sdlc:train_formed` | train | train, target, cars |
| `sdlc:train_build` | train | every git command the build ran |
| `sdlc:train_built` | train | base, head, cars |
| `sdlc:train_probe` | train | prefix, branch, pull request, head |
| `sdlc:train_verdict` | train | prefix, head, verdict, good, bad |
| `sdlc:train_ejected_change` | train | pull request, head, reason |
| `sdlc:train_landed_train` | train | train pull request, merge commit, cars |
| `sdlc:train_abandoned` | train | reason |
| `sdlc:train_review_requested` | train | train, pull request, head, reviewer profile, job, ask |
| `sdlc:train_review_reasked` | train | train, pull request, head, ask of the bound, previous job and its state |
| `sdlc:train_review_exhausted` | train | train, pull request, head, asks, last job, cars |
| `sdlc:train_refused` | train | train, pull request, head, reviewer, cars |
| `sdlc:train_off` | delivery, train | why the project is not trained |
| `sdlc:train_cleanup_failed` | train | the probe pull request or branch the forge would not clear |

## Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| binding `merge_train.disabled` | `false` | see [above](#when-a-project-is-trained) |
| binding `merge_train.max_size` | `5` | changes per train, 1–25 |
| `sdlc.train_sweep_interval` | `5m` | the backstop's cadence |
| `sdlc.merge_attempts` | `6` | the landing merge's not-ready retries, shared with the delivery's merge |

The two binding fields travel on every transport (REST, QUIC, Unix, gRPC, MCP,
embedded) and in both SDKs; see [api.md](api.md#the-sdlc-module).

## Display labels

`train_ejected` is a label key a binding may map (`forge.labels`); an ejected
change's pull request gets it beside the comment. A binding that maps none
keeps the comment as the record.

## Testing

`go test -tags codec_json ./pkg/sdlc/ -run 'Train|MergeTrain'`:

- `merge_train_test.go` — the policy, the planner against every culprit
  position of trains up to eight long (and the probe count bound), the text,
  the store's guards.
- `merge_train_runner_test.go` — the real runner, planner, store and gate
  definitions against the fake forge, with the git half and the review job
  stood in for: green, bounded, red and bisected (only the landing prefix
  reviewed), void rerun, environmental retry (once, then ejected with the
  suspicion named), host defect, build-time conflict and moved head,
  target moved, approval lost, refused merge; the review requested once,
  awaited, bound to the head, a lease already held, a start refused; re-asked
  after a job that ended with no verdict or vanished, a job that cannot be
  read, the bound exhausting into an abandonment that ejects no one, and a
  restart during a re-ask;
  a refused train re-forming smaller and landing its changes one by one, a
  refused single change ejected with the findings, a refusal resumed half way;
  a reviewer lost mid-train, a project with no counting reviewer; and
  restarts in `validating`, while reviewing, in `completing` and mid-build.
- `merge_train_gitea_test.go` — the review runner on a train's pull request:
  reviewed with the train prompt when its train waits, skipped when it does
  not, on another head, or under another profile; an unreadable ledger fails
  the review.
- `merge_train_build_test.go` — the git half's argument vectors: a `--no-ff`
  merge of the approved head, a conflict aborted and named, a plain push.
- `merge_train_delivery_test.go` — the delivery runner end to end: queued and
  closed where it landed, ejected and fixed and landed, refused by a train's
  reviewer and fixed and landed, merged alone with the reason recorded,
  withdrawn, refused on a node with no train.
- `merge_train_scheduler_test.go`.

## The live GitHub train e2e

`cmd/quicbaasd/sdlc_train_github_e2e_test.go` (build tag `integration`) runs a real node bound with forge type `github` to a scratch repository, and proves that a train of approved pull requests — one of them a pull request the daemon did not open, brought in with `adopt` — lands through one `queue/<n>` pull request as a squash with its branches deleted, and that a car which breaks the queue CI is ejected by bisection while the rest land.
It reads `GITHUB_ITEST_TOKEN`, `GITHUB_ITEST_REVIEWER_TOKEN`, `GITHUB_ITEST_OWNER`, `GITHUB_ITEST_REPO` and the optional `GITHUB_ITEST_BASE_URL` through `pkg/testutil/githubitest`, which cuts a per-run base branch and commits `testdata/queue-ci.yml` to it as `.github/workflows/queue-ci.yml`.
That workflow's one job, `ci-gate`, runs only for `queue/*` heads and fails when the tree holds a file named `BREAK-CI`, which is how the red test breaks one car.
With any required variable unset every test fails, naming each missing variable; none skips.
