# CI-failure classification — deciding who a red check belongs to

> **Status: REFERENCE** · Last verified: 2026-10-11
> Supplement to the [SDLC feature folder](architecture.md): how a red check
> is classified before a review round is spent on it.

A red check is three different events wearing one colour:

1. the change's own test failure,
2. a runner that died holding the job,
3. a host that could not start the job at all.

Answering them the same way is what costs money. Before this existed, the
review gate returned every settled red as decisive, the job went to
`re-delegating`, and a coding agent was paid to author another attempt at a
change whose tests had never run. Measured on 2026-09-12: one CI host had been
provisioned without `CONFIG_FILE=/data/config.yaml`, so `act_runner` ignored
its entire mounted config — no module settings, capacity stuck at 1 — and
separately had no `insecure-registries` entry and no lxcfs installed. Three
lanes spent roughly 1.5M tokens independently re-deriving the symptom, and the
issue they filed named three candidate causes, none of them correct.

Every one of those failures is mechanically identifiable. No model is involved
anywhere on this path.

## The verdicts

`pkg/cifault.Classify` answers, for **one** finished check, one of three
verdicts, and the verdict names the actor:

| Verdict | Who acts | What happens |
|---|---|---|
| `genuine` | the coding agent | decisive failure, findings as feedback, `re-delegating` — what a red check has always done |
| `void` | nobody | the check is re-run automatically, bounded; the job stays in `reviewing` and waits |
| `host_defect` | an operator | the task is parked (`TaskBlockKindEscalated`) and the account is posted to the forge issue |

## The asymmetry that shapes every rule

The two misclassifications are **not** equally expensive.

- Calling a genuine failure `void` auto-retries a broken change. The engine
  pays for CI over and over and the defect never reaches anyone who could fix
  it.
- Calling a void run `genuine` costs one wasted agent attempt — which is what
  happens today anyway.

So **`genuine` is the default and every other verdict has to be earned** by a
signature only infrastructure produces. A log the classifier does not recognise
is the change's own failure. A log it cannot read at all is the change's own
failure. Nothing is void by absence of evidence.

## The signatures

Tried in order; the first match decides.

| # | Signature | Verdict | What it matches |
|---|---|---|---|
| 1 | `registry_unreachable` | `host_defect` | `failed to pull image`, `http response to https client` |
| 2 | `container_create_failed` | `host_defect` | `OCI runtime create failed`, `runc create failed` |
| 3 | `module_resolution` | `host_defect` | `unknown revision` on a module belonging to a declared private owner |
| 4 | `runner_teardown` / `runner_lost` | `void` | a line in the runner-infrastructure table (below), and nothing in the log failing |
| 5 | `status_raced_shutdown` | `void` | the log's own last word is `Job succeeded`, against a conclusion of `failure` |
| 6 | `host_eviction` | `void` | exit 137 (SIGKILL) with the OOM scope observed as `none` or `host` |
| 7 | `runner_died` | `void` | no end-of-job marker anywhere, nothing in the log failing, the runner not known to be reporting |
| — | `cancelled` | `void` | the forge reports the check cancelled; an act from outside the tests |
| — | `unrecognised` | `genuine` | everything else |

**Order is load-bearing.** The host-defect rules come first because a host that
cannot pull the image or create the container never writes an end-of-job marker
either — jobs 77520, 77604 and 77625 of 2026-09-12 all stop mid-preamble — so
rule 7 matches them too, and its verdict is the wrong one: it would re-run them
on the one host that cannot run them. Rule 4 sits before rule 7 so a runner
that said why it went away is named for what it said.

### The runner-infrastructure table

A runner writes its own errors in the same `##[error]` annotation a failed step
uses. On 2026-09-22 (go-luks2 PR #203, go-quicbaas#1011) `CI / Unit Tests` went
red after every step and post-step had passed, in the runner's own teardown:

```
Timed out waiting for the daemon to remove container 895cf614c74b…, its volumes and network may be left behind
##[error]Error while stop job container: Error response from daemon: remove GITEA-ACTIONS-TASK-73374-…: volume is in use
Job failed
```

The `##[error]` vetoed every void rule, so the red reached the worker, who spent
a round proving a docs-only diff was fine before the task escalated to the
owner.

The runner's own lines now live in ONE table, `runnerInfraSignatures` in
`pkg/cifault/infra.go` — a map from a lower-cased needle to its signature and
any co-needle that must share the line. It is read two ways: rule 4 reads it
for the signature, and `failureLine` reads it to refuse those lines as the
change's failure. A new signature is a new row, never a new conditional.

| Needle | Also on the line | Signature |
|---|---|---|
| `error while stop job container` | | `runner_teardown` |
| `timed out waiting for the daemon to remove container` | | `runner_teardown` |
| `volume is in use` | `error response from daemon: remove` | `runner_teardown` |
| `the runner has received a shutdown signal` | | `runner_lost` |
| `lost communication with the server` | | `runner_lost` |

**A failed step stays genuine.** Rule 4 fires only when nothing else in the log
failed, and `Process completed with exit code N` (N ≠ 0) is failure evidence
with or without its `##[error]` prefix — the step's answer is the change's,
whatever the runner did afterwards. A check the runner *cancelled* is already
`void` by its conclusion (`cancelled`, below).

### Two guards carry most of the safety

**End-of-job markers are matched whole-line**, and the *success* marker only
within the last twelve lines. A test that prints `Job succeeded` in the middle
of a suite it then fails must not void the run that failed it.

**`vetoVoid` sits after every rule.** No void verdict stands over a log
containing the change's own failure — a `--- FAIL:`, a bare `FAIL`, a `panic:`,
a `fatal error:`, `make: *** … Error`, a non-zero `Process completed with exit
code N`, or a `##[error]` / `::error` annotation that is not one of the
runner's own lines (the table above). It was added because the corpus test found the hole: a genuine test
failure on a host that *also* reported an out-of-cgroup SIGKILL classified as
an eviction. It deliberately does **not** read the runner's `Job failed` line,
which a runner writes for any non-zero exit including a SIGKILL and which
therefore says nothing about why.

### `module_resolution` needs the project

`unknown revision v0.2.0-alpha` on a **public** module is usually a real
mistake in the change — a go.mod pinned to a tag nobody pushed — and belongs to
its author. It is a host defect only for a module no host can resolve without
settings an operator provisions. `cifault.Input.PrivateModuleOwners` names
those owners, and `pkg/sdlc` supplies the project's own forge owner. Declare no
owners and the rule never fires.

### `host_eviction` needs the OOM scope

Exit 137 is SIGKILL and says only that something outside the process ended it.
Killed for memory inside its own cgroup, that is the change's appetite and the
change's failure (`OOMJob`); killed with no out-of-memory event of its own
(`OOMNone`, `OOMHost`), the host wanted the machine back. With the scope
**unknown** the rule does not fire, because a bare 137 is exactly as likely to
be a change that allocates without bound. Nothing wires a runner-cgroup source
yet, so in practice exit 137 reaches the agent — the safe answer.

## Testable without a forge

`Classify` is pure over `(conclusion, log text, runner state)`. It talks to no
forge, reads no clock and holds no state, so `pkg/cifault/testdata` is the whole
harness: nine **real** Gitea Actions job logs from 2026-09-12, trimmed to the
tail the classifier reads and otherwise unedited. Provenance for each is
recorded in the test table, including the two "genuine" fixtures that are the
same log trimmed to two different tail lengths — because the tail length is
what decides whether the failure evidence is inside the window, and both must
answer `genuine`.

`TestClassify_GenuineFailureIsNeverVoid` crosses every genuine fixture with
every runner state a caller could plausibly supply. That direction is
over-tested relative to the rest on purpose.

## Wiring: what the gate does with the verdict

`pkg/sdlc` is the seam. `TriageChecksFailure` turns the module's records into
the classifier's inputs and keeps the answers beside the checks they are about;
`ChecksTriage.Aggregate` reduces them to the one action the round takes.

**`genuine` outranks everything.** One real test failure among a dozen
infrastructure faults is still a real test failure and must reach the agent.
**`host_defect` outranks `void`**, because a host that cannot start a container
will not start one by being asked again.

`ReviewValidator.checksFailed` then:

- **genuine** — decisive, `ChecksFailure.Findings` as feedback. The worker is
  additionally told which reds are *not* its own and not to chase them; only
  the genuine ones are named and quoted.
- **void** — not decisive. `rerunVoid` asks the forge, records the ask, and the
  job waits.
- **host_defect** — a `*CIHostDefectError`, which `runnerRun.review` turns into
  a park with `TaskBlockKindEscalated`. The park machinery posts the account —
  per-check verdict, signature and redacted evidence — to the forge issue.

### The settle window stays

`DefaultChecksSettleWindow` (5 min) is not a root-cause rule and the classifier
does not replace it. It is what stops a red being **read** while a rerun is in
flight, and reading one early would classify a half-written log.

### Cancelled checks now settle

`evaluateChecks` already blocks the gate on a cancelled check, but
`evaluateChecksFailure` used to collect only outright failures — so a head whose
only red was a cancellation never settled and the job sat in `reviewing` until
the stall detector noticed. It now settles, and classifies `void`.

## The re-run budget

"Void" means *ask the machine again*, and that is only safe while the asking
stops: a permanently broken host answers void every time.

- `DefaultChecksRerunBudget` = **3** asks per `(head, check)`.
- Every ask is recorded on `Delivery.CIReruns` — on the **record**, not in the
  validator, because a budget lost to a daemon restart is an unbounded loop with
  extra steps. The write is guarded and merges append-only: a count applied
  twice escalates a healthy fleet and one dropped bounds nothing.
- Every ask is also written to the job's audit trail as an **`sdlc:ci_rerun`**
  event — `check=… head=… run=… signature=… attempt=N/3`, plus `refused=…`
  when the forge said no. The runner hands the review gate its audit sink
  through the context (`withCIRerunAudit`), because the gate is one validator
  shared by every job and blocks until a decision, so the re-run would
  otherwise be invisible on the job until the round ended.
- Three paths escalate rather than loop: a spent budget (the account says how
  many times the fleet was asked), a forge with no rerun route, and a rerun the
  forge refuses (which still spends budget).

**Whether a re-run is pending is decided on TIME, not on the run id.** A forge
is free to restart a run in place and report the result under the same
identifier, so "have we already re-run this id" would answer yes forever and
the job would hang waiting for an answer it already had. An ask newer than the
recorded finish is pending; one older is the ask that finish answers.

## The forge seam

| Member | Gitea | Why it is data |
|---|---|---|
| `ForgeCapabilities.RerunCheckRuns` | `true` | a platform that cannot re-run must make the engine **escalate**, not wait for a rerun nobody will trigger |
| `ForgeBackend.RerunCheckRun(ctx, repo, run)` | `POST …/actions/runs/{run}/rerun-failed-jobs` | restarts only the failed jobs; the green ones are answers about the same head |

The adapter resolves the run through `giteaJobFromStatusURL` **and** the run's
own job listing (`actionsJob`, shared with `CheckRunLogs`). That proof matters
more here than for the log route because this is a **write**: a commit status is
publishable by anything holding status-write, so a publisher able to name any
run could burn the fleet's capacity from a field it controls. Read-back accepts
a bumped `run_attempt` **or** a job that is no longer `completed`; neither alone
is safe, because a job that fails again within the round trip is "completed"
once more.

## Two log budgets

| Constant | Bytes | What for |
|---|---|---|
| `DefaultChecksClassifyBytes` | 64 KiB | what is **read** per failed check and handed to the classifier |
| `DefaultChecksLogBytes` | 4 KiB | what is **quoted** to the worker |

Sixteen times apart because they answer different questions. On this fleet the
decisive line sits behind the post-checkout teardown the runner appends to every
job — roughly forty lines of git-config noise: `unknown revision` is 42 lines
from the end of job 77532's log, a bare `FAIL` 40 lines from the end of the race
detector's. A 4 KiB tail reaches neither, and a classifier that cannot see the
decisive line answers `genuine` for everything — safe, but the whole feature
off.

The read also covers up to `maxChecksClassifyContexts` (12) failed checks, not
the 3 that are quoted: a broken host fails every job of a pipeline at once, and
a check whose log was never read classifies as `genuine`, so one unread check
among six would send the whole outage to a coding agent.

## Where the code is

| Concern | File |
|---|---|
| The classifier, pure | `pkg/cifault/classify.go`, `verdict.go`, `log.go` |
| Runner-infrastructure signatures | `pkg/cifault/infra.go` (`runnerInfraSignatures`) |
| Typed refusals | `pkg/cifault/errors.go` |
| Real-log corpus | `pkg/cifault/testdata/*.log` |
| The seam and the budget constant | `pkg/sdlc/checks_triage.go` |
| Acting on the verdict | `pkg/sdlc/forge_review.go` (`checksFailed`, `rerunVoid`) |
| Findings, settle window, log budgets | `pkg/sdlc/gate.go` |
| Re-run bookkeeping | `pkg/sdlc/delivery.go` (`CIReruns`, `RerunPending`) |
| Park instead of re-delegate | `pkg/sdlc/runner.go` (`runnerRun.review`) |
| Forge rerun | `pkg/sdlc/forge.go`, `forge_gitea.go` |

## Not built yet

- **Runner quarantine.** A `host_defect` tells an operator; it does not take
  the offending runner out of the fleet. There is no runner-fleet API at this
  seam.
- **A runner-state source.** `cifault.RunnerState` is read — `Reporting` blocks
  the `runner_died` rule, `OOM` decides `host_eviction` — but nothing supplies
  it yet, so both fields arrive unknown and both rules behave conservatively.
