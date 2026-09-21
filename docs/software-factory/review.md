# Code review — QuicBaaS reviews its own pull requests

> **Status: NORMATIVE** · Last verified: 2026-09-13
> Supplement to the Software Factory feature folder: the `review` job runner,
> the forge write surface it needs, and the nine production failures it is
> built not to repeat. Conflicts with this document are defects.

QuicBaaS has always been able to *read* a verdict and *decide* on it —
`pkg/sdlc/gate.go` is the whole merge gate as a pure function. What it could
not do was *write* one. This is that half: a reviewer agent selected from
project configuration, a verdict posted to the forge bound to the head it
judged, and a commit status that makes the verdict enforceable by the
platform's own branch protection.

It replaces a shell workflow that reached its twenty-sixth version. The
sections below are organised around that: each property is stated with the
incident that taught it, because a rule with no incident behind it is a rule
somebody will simplify away.

## The shape

```
forge webhook  →  WebhookIngestor  →  DispatchReview  →  ReviewDispatcher
  (pull_request opened / synchronized,                         │
   status / check_run / workflow_job)         StartJob{Runner: "review"}
                                                               │
                                                        ReviewRunner
   initializing ──► executing ──► completing ──► cleanup
   read the forge   one turn,     submit the     release the
   decide whether   one verdict   verdict, write reviewer
   a review is                    the status,
   needed, spawn                  record the gate
```

Nothing above `ReviewRunner` decides anything about the change. A delivery says
which **project** and which **pull request** — or, for a pipeline delivery,
which **commit** — something happened to, and that is the whole of its
authority: the head, whether the pull request is open, whether it is a draft,
and whether a verdict already stands are all read from the forge inside the
job, at the moment the job runs. The dispatcher's one decision is whether the
head's pipeline has finished green, and it reads that from the forge too.

## When a review starts — after CI is green, never before

`ReviewDispatcher.TriggerReview` (`pkg/sdlc/review_dispatch.go`) refuses on two
things and nothing else: **configuration** — a project that binds no reviewer
will never produce a verdict, so a job for it exists only to fail — and **the
pipeline**.

```
pull_request opened      →  nothing has run yet        →  no review
status / check_run /     →  a check is still running   →  no review
workflow_job (pending)
status / check_run /     →  a check finished red       →  no review
workflow_job (red)
status / check_run /     →  every check finished green →  ONE review
workflow_job (green)
```

**The delivery a gated review starts on is the pipeline's, not the pull
request's.** `EventCheckRunUpdated` — Gitea's `status`, `check_run` and
`workflow_job` hooks, all three — used to be parsed and then discarded, which
left only `opened` and `synchronized`: the two deliveries that arrive *before
any test has run*. This fleet's pipeline takes about fifty minutes, so a
reviewer spawned on `opened` reads a tree whose verdict is an hour away, and
reaches for the build and the tests itself to find out.

**It is event-driven, not a wait.** No session, lease or job is held across the
fifty minutes. Each delivery asks the question again and the answer is read
from the forge; a red head simply gets no review, and the re-run's green
delivery gets one.

**A pipeline event names a COMMIT, not a pull request.** So the dispatcher
resolves the head to its pull request through
`ForgeBackend.ListOpenPullRequests` — whole, because a capped page would answer
"no pull request has this head" for a busy repository and the change whose CI
just passed would never be reviewed. A commit that heads no open pull request
starts nothing, which is what most pushes are.

**Green is defined once.** The gate is `ChecksGreenForReview`
(`pkg/sdlc/gate.go`), which is `evaluateChecks` — the merge gate's own check
half — over the same records. A second definition would eventually dispatch a
reviewer onto a head the merge gate refuses, or withhold a review from a head
it would land.

**A project that does not set `RequireGreenChecks` is dispatched exactly as it
always was**, and reads nothing from the forge to find that out: its policy says
checks do not decide whether a change may land, so they do not decide whether it
may be reviewed. Which also means **a pipeline delivery is nothing to it** — the
check-run delivery exists for the gate, and acting on it without one would turn
every job of every run into a review job. The converse is an operator's
misconfiguration and is left visible: a project that requires green checks and
publishes none has no head this will ever dispatch on, exactly as it has no
head the merge gate will ever clear.

**Duplicate green deliveries do not mean duplicate reviews.** One pipeline
finishing produces several deliveries (a `status`, a `check_run`, a
`workflow_job` per job), and each is evaluated. What keeps them to one verdict
is unchanged and deliberate: the job holds the pull request's lease, and its
first step re-asserts a standing verdict rather than re-deriving one
(`skipDecision`). Starting a job that turns out not to be needed is cheap; it
decides against the forge before a model is spent.

## The floor — a green pull request is reviewed with no delivery at all

Everything above is the **delivery** path. It is not the only one, and until
go-quicbaas#933 it was: `DispatchReview` had exactly one caller, the webhook
ingestor, so a node whose delivery path was broken or never provisioned
reviewed nothing and said nothing. Worse, **repairing the hook did not recover
the backlog** — the pipeline event for an already-green head fired in the past,
a new hook receives no replay, and no operator verb could ask for a review, so
the only way to advance an accumulated queue was to force CI to run again on
each pull request purely to manufacture a fresh event. Measured on the burn-in
node, 2026-09-13: `jeremyhahn/go-truststrap` had zero webhooks, and two
finished pull requests sat with all six checks green and `mergeable: true`,
permanently unmergeable.

`ReviewSweeper` (`pkg/sdlc/review_sweep.go`) is the floor under that path. Per
bound project it lists the open pull requests and, for each, reads the head's
commit statuses **once**:

| the head carries | the pass |
|---|---|
| a status under the reviewer's context, in any state | passes over it — a review has already been given, re-asserted, declined or is in flight |
| no such status, and every other check finished green | asks `ReviewTrigger.TriggerReview` for it |
| no such status, and a check pending or red | passes over it; the next pass asks again |

**It is a second trigger for an existing port, not new review logic.** Green is
`ChecksGreenForReview`, the same function the delivery path and the merge gate
call. Whether a review is warranted is the trigger's decision, unchanged. The
pass only finds the pull requests nobody was told about.

**The absent status is the filter because the forge already holds the answer.**
The review job publishes under its context on every ending it has — a verdict,
a re-asserted standing verdict, a draft it declined to approve, a head it ran
out of rounds for — bound to the head it is about, visible to a person, and
surviving any restart of this node. A record of this module's own would be a
weaker copy that can disagree with it.

**Double-starting is the engine's to prevent.** A delivery and a pass that race
name the same pull request, so both jobs take the same lease subject
(`ReviewLeaseSubject`) and exactly one of them acquires it — the same rule that
already keeps a forge's duplicate deliveries to one verdict.

**What it costs.** One `GET /pulls` per bound project per pass, plus one
`GET /commits/{sha}/statuses` per open pull request. A pull request that is
actually a candidate costs two more calls, because the dispatcher re-reads the
pull request and its head before starting a job — paid only where a whole model
run is about to be spent. Nothing else: no listing is repeated and no judged
head is read twice.

**Where it runs.** On the SDLC backstop goroutine beside the triage pass
(`pkg/server/lifecycle_sdlc_backstop.go`), on its own ticker — one pass at boot,
which is the point, because the deliveries a node missed are exactly the ones
that arrived while it was down.

## Discovery modes — `poll`, `webhook`, `both`

A binding's `discovery` selects which of the two paths a project runs. Empty
takes `both`.

| mode | behaviour | when |
|---|---|---|
| `both` (default) | a delivery acts immediately; the pass reconciles whatever it missed | production |
| `poll` | the pass only; a delivery that arrives anyway is ignored for review dispatch | a forge that cannot reach this node — air-gapped, NAT, no inbound route — or one with no webhook support |
| `webhook` | the delivery only; the pass makes no forge call for this project | a trusted, observed delivery path where latency matters most |

`both` is the default because webhook delivery is at-least-once **and
droppable**: deleting the pass trades a token cost for a liveness bug, where one
lost delivery strands a finished change until a person notices. The modes are
independent rather than a flag in front of one code path — `poll` works with no
ingress configured at all, and `webhook` does not silently degrade to polling —
and a mode this module does not implement is refused when the project is bound,
not discovered later as a project that looks configured and finds nothing.

A `poll`-only project still has its issue poller nudged by a verified delivery:
the mode selects how **reviews** are discovered, not whether a delivery is worth
anything.

## What the reviewer is told — CI already passed, review only

The reviewer charter is `pkg/sdlc/promptdefaults/sdlc/reviewer_charter.md`
(markdown since #880, a Go constant before #927). It states plainly that
build, lint, unit, integration, race and security have already run against this
exact commit and passed, and that the reviewer's job is to read the diff — not
to run the build, the tests, the linters or the scanners, and not to install a
toolchain.

That is now true by construction rather than by hope: the dispatcher above
starts no review until the head is green. Both sentences are **declared
markers** on the prompt specification (`reviewerCharterMarkers`,
`pkg/sdlc/prompt_set.go`), so an edited charter that drops either is refused at
registration — the same mechanism that stops the worker charter dropping a stop
code. `ReviewerCIGreenMarker` and `ReviewerNoRerunMarker` name the literals.

Both halves are required. A reviewer told only that CI passed still reaches for
`make test`, because running the tests is what a careful reviewer does when
nothing forbids it.

## Who reviews — project configuration, never a label

`ProjectBinding.Reviewer` (`pkg/sdlc/review_config.go`):

```go
type ReviewerConfig struct {
    Backend        string // ACP backend; EMPTY means this project is not self-reviewed
    Model          string
    Image          string // the reviewer reads the change in a real checkout
    Login          string // the forge account the reviewer's token authenticates as
    TokenSecretRef string // the REVIEWER's own forge token, not the worker's
    StatusContext  string // default "quicbaas/review"
}
```

**The reviewer is decided once, on the binding.** Routing by label made every
question about the reviewer a question about the state of a mutable,
forgeable, eventually-consistent display field: who may add the label, what
happens when two are present, what happens when it is removed mid-review,
what happens when the payload renders an empty set as `null` rather than as
an empty array. None of those has a good answer and each cost an incident.

`ForgeEvent` carries **no label field at all**, so label routing is
unexpressible rather than merely discouraged.
`TestForgeEvent_CarriesNoLabelsAtAll` is the tripwire.

**An unconfigured reviewer is a configuration, not a gap.** A project reviewed
by people binds no reviewer and the runner is never dispatched for it. A
*partly* configured one is refused at bind time: a backend with no login
posts verdicts nobody can attribute, and a backend with no token of its own
posts them as the author.

**The reviewer is a different principal from the worker.** `TokenSecretRef`
is deliberately separate from `ForgeConfig.TokenSecretRef`. A verdict posted
with the author's own token is a change approving itself — the forge records
the author as the reviewer and the independence rule has nothing to tell
apart. `ReviewerConfig.ForgeConfigFor` is the whole identity switch: the
project's repository, reached with the reviewer's token.

**The reviewer signs nothing.** It asks the forge-access seam for
`SigningScopeCheckout`, so the agent behind its checkout serves the project's
**access key** — what ssh authenticates a clone with, and without which the
reviewer could not read the change at all — and **no commit signing key**. The
agent is stopped the moment the checkout is made, before the reviewer is
prompted, so for the whole time it is reading an untrusted change the socket it
holds answers nothing. An https-only project gets no agent at all.

Before go-quicbaas#929 the reviewer was handed no agent under any circumstances,
which read as the stronger posture and in fact meant it could not clone an ssh
remote: `git` offers no client key and the forge answers `Permission denied
(publickey)`. The two keys a project's git identity holds are now named
separately rather than bundled into one socket.

## The forge write surface

Three methods were added to `ForgeBackend`, designed to be SCM-portable —
Gitea today, GitHub and GitLab as follow-ups. Call sites branch on
`Capabilities()`, never on `Type()`.

### `SubmitReview`

```go
SubmitReview(ctx, repo RepoRef, number int, submission ReviewSubmission) (*ForgeReview, error)

type ReviewSubmission struct {
    Verdict ReviewVerdict // approve | request_changes | comment — the MODULE's vocabulary
    Body    string        // required for every verdict
    HeadSHA string        // required; the tree the verdict judges
}
```

Portable because:

- **`Verdict` is `ReviewVerdict`, never a platform's wire string.** Gitea and
  GitHub both spell approval `APPROVED`; GitLab has no review object at all
  and expresses the same three outcomes as an approval, an unapproval plus a
  note, and a note. A submission carrying `"APPROVED"` would have encoded one
  platform's vocabulary into every caller. Each adapter owns one table
  (`giteaReviewStates`) and nothing above it changes.
- **`HeadSHA` is required.** Gitea and GitHub bind it natively (`commit_id`);
  an adapter for a platform that does not must record it in the body so
  `ListReviews` can bind the verdict back. Making it optional would let a
  verdict be posted the gate could never attribute to a head.
- **`Body` is required for every verdict.** See failure 6.

`VerdictError` is deliberately **not** submittable: a review that could not be
*produced* is the module's own record of its own failure, and posting it
would publish an internal fault as a reviewer's opinion.

### `SetCommitStatus`

```go
SetCommitStatus(ctx, repo RepoRef, sha string, status CommitStatus) error

type CommitStatus struct {
    Context     string      // the check's name — what branch protection requires by
    Status      CheckStatus // the SAME vocabulary ForgeCheckRun is read back into
    Description string
    TargetURL   string
}
```

Portable because `Status` is `CheckStatus` rather than any platform's spelling
of it: Gitea writes `pending`/`success`/`failure`/`error`, GitHub adds a
`queued`/`in_progress`/`completed` split with a separate conclusion, GitLab
writes `pending`/`running`/`success`/`failed`/`canceled`. Each adapter maps
one way and reads back the other; a caller never learns which platform it is
on. `Context` is passed through unchanged — a platform-specific prefix
invented in an adapter would mean the same logical check had a different
required name per forge.

A state the platform cannot express is an **error**, not a nearest match.
Gitea has no word for "cancelled": mapping it onto failure would report a run
nobody finished as a change that failed, and onto pending would leave a
required check waiting for ever.

**A status is written against a SHA, not against a pull request**, because
that is what the verdict is about and because the two disagree the moment a
commit is pushed.

### `ListPullRequestFiles`

```go
ListPullRequestFiles(ctx, repo RepoRef, number int) ([]ForgeChangedFile, error)
```

Paginated, whole-listing-or-error. It is the reviewer's **map** of the change
— the checkout is the territory — and the reason it must be complete is
failure 5.

### Capabilities

| Flag | Gitea | What turns on it |
|---|---|---|
| `SubmitReviews` | true | whether this deployment can review its own pull requests at all. False refuses the runner up front rather than per pull request |
| `CommitStatuses` | true | whether a verdict is *enforceable* by the platform. False still reviews — the module's own gate is unaffected — but nothing outside the module honours the verdict |

## How a verdict is read

From **structure, never from prose** — the same rule `turn.go` applies to a
worker's stop declaration. A verdict decides whether a change lands, so
grepping a paragraph for "looks good" would let a sentence in a quoted code
block merge a change nobody approved.

The reviewer ends its reply with a declaration on the last line:

```
SDLC-REVIEW: approve | request-changes | comment
```

Everything above it is the findings. The declaration is taken from the **last**
line that carries one, scanning upwards, so a reviewer quoting the charter
does not have the quote read as its answer. A reply with no declaration, an
unknown code, or a declaration with nothing above it is **not a verdict**: it
is recorded as a review that could not be produced, the check goes red, and
the job fails so the engine's own retry drives another attempt.

## The nine failures

Twenty-six versions of `opencode-review.yml` are a specification written in
incident reports. Each is a Go test that fails without its fix; the mapping is
below, and the tests are named for the *property* rather than for the version
that broke it.

| # | Property | Where it is enforced | Test |
|---|---|---|---|
| 1 | A draft may not be approved | `skipDecision` skips a draft; `submit` refuses an approval on one even past the skip; `EvaluateReviewGate` blocks `GateBlockedPullRequestDraft` before any verdict is read | `TestReviewRunner_ADraftIsNotApproved`, `…ADraftApprovalIsRefusedEvenPastTheSkip`, `TestEvaluateReviewGate_ADraftNeverClears` |
| 2 | A COMMENT must not dedup a head for ever | `standingVerdict` counts only decisions, through the same `forgeReviewVerdicts` table the gate reads | `TestReviewRunner_AnEarlierCommentDoesNotDedupTheHead`, `TestStandingVerdict_ACommentIsNotADecision` |
| 3 | No labels must not crash the reviewer | nothing routes on labels; `ForgeEvent` has no label field | `TestParseWebhook_APullRequestWithNoLabelsParses`, `TestForgeEvent_CarriesNoLabelsAtAll` |
| 4 | A relabel must not overwrite a standing rejection | a second trigger on an unchanged head **re-asserts** the standing verdict's status rather than re-deriving a verdict | `TestReviewRunner_ASecondTriggerReassertsAStandingRejection` |
| 5 | The changed-file list must paginate | `ListPullRequestFiles` and `ListReviews` page until a short page; a forge that never ends is a fault, never a prefix | `TestListPullRequestFiles_PagesUntilAShortPage`, `TestListReviews_PagesUntilAShortPage`, `TestReviewRunner_TheWholeChangedFileListReachesThePrompt` |
| 6 | An empty verdict is not a verdict | `ParseReviewVerdict` refuses empty findings (a lone code fence included); `ReviewSubmission.Validate` refuses an empty body for every verdict | `TestParseReviewVerdict_RefusesEverythingThatIsNotAVerdict`, `TestReviewRunner_AnApprovalWithNoFindingsIsNotPosted` |
| 7 | A rejection must make the gate red | the status is derived from the verdict **this run produced**, in the same step that produced it (`reviewStatuses`); nothing green is published on the way | `TestReviewRunner_ARejectionIsRedOnTheRunThatFilesIt` |
| 8 | A rejection is not order-dependent | a forge COMMENT produces no verdict record at all, and `verdictSeverity` ranks a comment above an approval and below a refusal so a same-tick tie fails closed | `TestEvaluateReviewGate_ALaterCommentDoesNotRetractARejection`, `TestLatestVerdictsForHead_ACommentTieDoesNotClearAnApproval` |
| 9 | A stale verdict must not gate a new head | `ReviewRecord.AppliesTo` binds a verdict to a SHA; `standingVerdict` matches on the head, so a new commit is reviewed again | `TestEvaluateReviewGate_EveryRefusalInOrder/stale head ignored`, `TestReviewRunner_AVerdictOnAnEarlierHeadDoesNotCountForThisOne` |

Failures 8 and 9 were already enforced by `gate.go` before this change; the
gate's `stale head ignored` case covers 9 and the `forgeReviewVerdicts` table
covers 8. What was added for them is the explicit end-to-end statement of each
property and, for 8, the `VerdictComment` severity — a rule that held only
because a value was unknown stops holding the moment it becomes known.

## `quicbaas/review` — the required check

`DefaultReviewStatusContext` is the literal string **`quicbaas/review`**, and
branch protection on `main` requires it across the fleet. Two properties
follow, and both are pinned by tests.

**The name is project configuration, not a literal at the call site.**
`ReviewerConfig.StatusContext` overrides it per project and the runner reads
`reviewer.Context()`; onboarding a repository that wants a different check
name never means editing Go.
`TestReviewRunner_TheStatusContextIsProjectConfigurationNotALiteral` fails if
the call site is hardcoded, and
`TestDefaultReviewStatusContext_IsTheNameBranchProtectionRequires` fails if
the default is changed silently — the one edit that would leave every
protected branch waiting for a check nothing writes.

**The verdict and the status come from one decision.** This is the single
property the whole change exists to guarantee. The shell workflow computed
its exit code and its review verdict *separately*, from state read at
different moments, so the two could differ — and on the run that filed
`REQUEST_CHANGES` the check reported green and auto-merge landed rejected
code (#853, #868). Here `submit()` posts the verdict and derives the status
from **that same `submission.Verdict`**, in the same step, through
`reviewStatuses`:

| Verdict | `quicbaas/review` |
|---|---|
| `approve` | `success` |
| `request_changes` | `failure` |
| `comment` | `pending` — it decided nothing, so the check is not satisfied |
| a review that could not be produced | `failure` |
| a verdict with no mapping | `failure` — failing closed, so a verdict added without a colour blocks until somebody gives it one |

`TestReviewRunner_TheVerdictAndTheStatusCannotDisagree` is written so the two
cannot be satisfied independently: the expected status is looked up from the
verdict the **forge actually received**, never from what the test intended to
produce, so a runner that posted one verdict and published another's colour
fails it whichever way round the mistake was made. Reverting `submit()` to
compute the status from pre-run state — the shell bug, faithfully — produces
`posted request_changes but published success`.

The dedup path is covered separately by
`TestReviewRunner_AReassertedVerdictAgreesWithItToo`, because it publishes a
status *without* posting a verdict and is the one place the two could drift
apart unobserved. It re-asserts the standing verdict's own colour from the
same table.

`quicbaas/review` is a valid Gitea status context: contexts are free-form
strings and Gitea Actions itself writes ones with slashes and spaces
(`CI / CI Gate (pull_request)`). `ListCheckRuns` carries the context straight
through as `ForgeCheckRun.Name` with no parsing, so the name round-trips
unchanged.

## Ordering, and what happens when a step fails

**The review goes first and the status second.** A verdict recorded with no
status leaves a check pending: the change waits, and the next run re-asserts
the standing verdict's status and clears it. A status written with no verdict
behind it would be the module's own word for a review that does not exist, and
a green one would merge a change nobody reviewed.

**The pull request is re-read before anything is written.** A review takes
time and the pull request does not hold still: the author can push, close it,
or mark it a draft between the read that started the review and the write that
ends it. The **reviewed head is not updated** — the verdict is about the tree
the reviewer actually read — so a verdict on a superseded head is posted
honestly and the gate discards it as stale.

**Every exit releases the session**, including the exits a run does not plan:
a forge that stopped answering, a cancellation, a panic.

## One pull request, one reviewer, one tree

Three rules keep a verdict attached to the change it is actually about. Each
is a defect that shipped in the first cut of this runner.

### The checkout is pinned to the captured head

`initialize` captures `pr.HeadSHA`, and the workspace plan the reviewer's
container clones from carries that **commit**, not just the branch name
(`WorkspaceBranchPlan.Commit`). The cloner fetches the branch — that is how
the object arrives — and then resets onto the commit, reads `HEAD` back, and
**refuses** if it is anything else (`CheckoutHeadMismatchError`).

Without the pin, an author pushing between the read and the clone moves the
branch: the reviewer reads the new commit while the verdict and the commit
status still name the old one, and a judgement is attached to a tree nobody
opened. A force-push that drops the captured commit fails the checkout
outright, which is the right answer — the tree the run means to judge no
longer exists, and reviewing whatever replaced it is the failure being
prevented.

### One review job per pull request at a time

The review runner declares an `orchestration.JobLeasePolicy`. Its
`LeaseSubject` is `review:<projectID>:#<number>`, so every review job of one
pull request contends for one lease and the envelope declines the losers
before a reviewer is spawned.

A forge redelivers, and an opened pull request that is immediately labelled or
retargeted produces several deliveries within a second. The standing-verdict
skip cannot deduplicate them, because when either job looks, neither has
published anything. Both would spend a model run and both would write a commit
status under one login — and the later write wins, so a green check can land
over a rejection that was being filed while it ran.

The subject is the **pull request**, not the head: the head is not knowable
when the job is started, and serializing on the pull request is strictly
stronger anyway.

### A head that moves under the review is still reviewed

Because the holder is the only job that may review the pull request while it
runs, stopping at the head it started on would leave a mid-review push with no
verdict at all. So `publish` compares the head it judged against the pull
request it re-read, and goes round again on the new head — a fresh reviewer on
a fresh checkout — up to `MaxReviewRounds`.

On exhausting the budget the job publishes a **pending** status on the head it
did not get to, saying the change kept moving, and ends. That releases the
lease, so the next delivery starts a fresh job; and a required check that is
pending is one a person can see, where an absent one reads on some platforms
as nothing to wait for.

### A review does not spend the project's worker budget

`OffProjectBudget` is true, so a review lease is marked `OffBudget` and is
outside `Quotas.MaxConcurrency` in both directions: admitted while the budget
is full, and not filling it.

A project at `max_concurrency: 1` is the common case. If a review counted, a
pull request would sit unreviewed while the next worker held the only slot —
and that worker's own pull request would then queue behind the worker after
it. The queue never drains, and it fails worse the busier the project is.

## What the runner does not do

It does not merge. The gate is **evaluated and recorded** — through the same
`EvaluateReviewGate` the delivery runner's merge step reads — so a person
looking at the review job can see whether the pull request is now clear and
what is holding it if not. Re-deciding the merge in a second place is exactly
the duplication that let the shell automation merge over its own rejection.

## Configuring a project

```yaml
reviewer:
  backend: codex
  model: gpt-5-codex
  image: ghcr.io/automatethethings/software-factory-reviewer:latest
  login: agent-reviewer
  token_secret_ref: forge/agent-reviewer/token
  status_context: quicbaas/review     # optional
  reasoning_effort: medium            # optional; codex's model_reasoning_effort: minimal|low|medium|high|xhigh
discovery: both                       # poll | webhook | both (default)
review:
  required_approvals: 1
  require_independent_reviewer: true  # the reviewer's model family must differ from the worker's
  require_green_checks: true
  reviewers:
    agent-reviewer: openai            # the family behind the login, for the independence rule
```

With `require_independent_reviewer`, a reviewer drawn from the worker's own
model family is refused **before a model is spent**: the gate would discard
such an approval anyway, and running it would leave a green commit status
beside a blocked gate, which reads to a person as the module disagreeing with
itself.

The rule is only as good as the routing beneath it: it names the family of the
CONFIGURED reviewer, so the backend the reviewer's session actually runs has to
be the one it was routed on. See
[autonomy-and-dispatch.md](autonomy-and-dispatch.md#the-backend-a-remote-daemon-serves)
— a remote daemon serves one backend per `RemoteACPBackend`, and a spawn for a
backend its daemon does not serve is refused rather than served by the
daemon's primary.

Require `quicbaas/review` in the repository's branch protection to make the
verdict binding on every route, including the routes the module does not own.

Node-level, in `sdlc:`:

| key | default | what it paces |
|---|---|---|
| `review_sweep_interval` | `5m` | how often the review floor visits every bound project |
| `triage_sweep_interval` | `15m` | how often the triage backstop visits every bound project |
| `unblock_sweep_interval` | `10m` | how often the unblock sweep re-examines every blocked task and returns the ones whose condition has cleared |

The three share one goroutine and keep separate intervals: a triage pass starts
a job per bound project, a review pass makes forge reads and starts work only
for a pull request that is genuinely unjudged, and an unblock pass reads the
blocked tasks and readmits only those whose typed condition a forge check can
decide (see [admission-and-readiness.md](admission-and-readiness.md)).
