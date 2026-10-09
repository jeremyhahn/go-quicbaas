# Autonomous control loop

> **Status: REFERENCE** · Last verified: 2026-09-14
> Proposed design, not built.

**Status:** proposed, 2026-09-12. Not built. Per `docs/README.md`, planned-but-unbuilt
work lives here and this file is deleted when the feature ships.

Owner direction, 2026-09-12: the engine should act as the control loop for each
project — watch the issue queue, use the priority system to delegate ready issues
to a coding agent, bound parallelism by project settings when autonomous mode is
on. Each coding agent confirms the issue, implements the change, opens a PR, and
is **notified** when review feedback lands rather than polling the forge. When the
PR is complete the session closes and the PR merges. Goal: agents spend tokens on
coding and judgement only; everything mechanical is traditional Go.

## The loop already exists — twice

This design is mostly a **wiring and refactor** job, not new machinery. Establishing
that first, because the expensive mistake here would be rebuilding what runs today.

| layer | what it is | where |
|---|---|---|
| inner loop | state machine over **one** `JobRun`: initializing → decomposing → scheduling → assigning → executing → validating → reviewing → completing → cleanup. One iteration is one transition. Never sees another job. | `ControlLoop.RunJob`, `pkg/orchestration/control_loop.go:163` |
| outer loop A | forge issues → Tasks. Lists open issues carrying the binding's trigger label, stamps priority, adopts up to capacity in priority order | `TaskSourcePoller.Sweep`, `pkg/orchestration/task_source.go:355`; `IssueSource.Poll`, `pkg/sdlc/issue_source.go` |
| outer loop B | Tasks → Jobs. Sorts by priority, then age, then ID; spawns as many as slots allow. Exists as the queue's "second chance" for what auto-spawn missed | `TaskDispatcher.Sweep`, `pkg/orchestration/task_dispatcher.go:214` |
| maintenance loop | job-lease death detection → stall detection → session reconciliation | `agentServiceImpl.Sweep`, `pkg/orchestration/job_admission.go`; `SweepSessions`, `session_reconcile.go:691` |

Both outer loops walk every tenant, then every project with `Autonomous = true`, on the
same 30s tick (`DefaultOrchestrationSweepInterval`, `pkg/config/config.go:1299`).

Also already built, and **not** to be reimplemented:

- **Priority is consumed, not decorative.** `PriorityInputs.Computed()` is the sort key at
  both admission points (`task_source.go:456`, `task_dispatcher.go:354`).
- **Per-project parallelism.** `Project.Quotas.MaxConcurrency` (`project.go:231`) with
  `Project.Autonomous` (`:62`), enforced by `adoptionCapacity` (`task_source.go:490`)
  against live job leases.
- **Head pinning, enforced twice.** The gate only counts checks and reviews bound to the
  delivery's head (`gate.go:238`, `:264`); `Hooks.Complete` refuses on `HeadMovedError`
  (`hooks.go:519`); and Gitea's adapter sends `head_commit_id` so the **server** refuses a
  merge whose head moved, then reads the PR back to confirm (`forge_gitea.go:1567-1626`).
  This is correct and stays.
- **Webhook ingress.** `POST /api/v1/sdlc/webhook/{projectID}`, HMAC-verified, every auth
  failure collapsing to one 401 (`pkg/sdlc/webhook_http.go`, `routes.go:532`).
- **Session park/resume.** `SessionResumer`, `SessionStateResumable`, transcript replay
  (`orchestrator_resume.go:284-341`). Production code.

## What is actually missing

Nine items. Ordered by token saved per unit of work, which is not the order they were raised in.

### 1. Classify why CI failed, before waking anybody

**There is no classification today.** `evaluateChecks` (`gate.go:236`) buckets only
no-checks / incomplete / not-success, and treats `CheckCancelled` identically to
`CheckFailure`. The single piece of "is this real" logic is a 5-minute settle window
(`DefaultChecksSettleWindow`, `gate.go:456`) — a heuristic against status *flapping*, not a
root-cause classifier. A settled red is returned decisive and the job goes to
`JobStateRedelegating`: **an infrastructure fault makes the coding agent author another
attempt.**

Measured cost, 2026-09-12: one CI host had been provisioned without
`CONFIG_FILE=/data/config.yaml`, so `act_runner` ignored its entire mounted config — no
`GOPRIVATE`/`GOPROXY`, capacity stuck at 1, no container resource limits — and separately had
no `insecure-registries` and no lxcfs installed. Three lanes spent roughly 1.5M tokens
independently re-deriving the symptom, and the issue they filed named three candidate causes,
none of them correct. Every one of those failures is mechanically identifiable:

| signature | verdict |
|---|---|
| no terminal marker in the log (`Job succeeded` / `Job failed` / `Cleaning up container`) and the runner stopped reporting | runner died mid-job; the result is void |
| `failed to pull image`, `HTTP response to HTTPS client` | registry or host misconfiguration |
| `OCI runtime create failed` / `runc create failed` | host defect: missing mount, cgroup, ulimit |
| `unknown revision` on a `GOPRIVATE` module | module resolution env missing on that host |
| `Job succeeded` in the log but conclusion `failure` | status write raced the runner's shutdown |
| exit 137 with no OOM in the job's own cgroup | evicted by the host |

Verdict drives the action: **void** re-runs automatically and does not touch the agent;
**host defect** escalates to an operator and quarantines the runner; only a genuine test
failure reaches the agent. Nothing here needs a model.

This is self-contained, needs no new storage model, touches `gate.go` plus a new classifier
package, and would have prevented most of one day's spend on its own. **Build it first.**

### 2. Wake the waiting agent on an event — the events already arrive and are discarded

Gitea's adapter already parses `pull_request_review` and `pull_request_review_comment` into
`EventReviewSubmitted`, and `status` / `check_run` / `workflow_job` into
`EventCheckRunUpdated` (`forge_gitea.go:2284-2314`). `DispatchReview` then **drops exactly
those two kinds**: `reviewDispatchKinds` covers only PR opened/updated, so both return
`(false, nil)` at `review_dispatch.go:132`. `WebhookIngestor.Ingest` otherwise only calls
`NudgeProject`, which by its own doc comment (`webhook.go:22`, `ports.go:63`) sweeps the
issue→Task poller and "has no connection to any running job".

So the notification path is a wiring gap. What it needs:

- Route `EventReviewSubmitted` and `EventCheckRunUpdated` to the **job waiting on that PR**,
  keyed on `(projectID, pullRequestNumber)`.
- Idempotency on `(PR, head SHA, review id)` so a redelivery cannot wake an agent twice.
- Webhook **provisioning** — `EnsureWebhook` is implemented per adapter
  (`forge_gitea.go:2335`) but is called from no non-test code. PR #881 is adding the
  provisioning path and the `public_url` config surface; this design consumes that and must
  not duplicate it.

**Polling is kept, and the two are configurable** (owner direction, 2026-09-12). The webhook
does not replace the poll; both are first-class discovery modes selected per project:

| mode | behaviour | when |
|---|---|---|
| `poll` | today's timer only. No ingress required | forge cannot reach this node — air-gapped, NAT, no inbound route; local development; a forge with no webhook support |
| `webhook` | event-driven only | a trusted, observable delivery path where latency matters most |
| `both` (default) | event wakes immediately; the sweep continues at a longer interval and reconciles anything the event path missed | production |

`both` is the default because webhook delivery is at-least-once **and droppable**: a design
that deletes the poll trades a token cost for a liveness bug, where a single lost delivery
strands a job until a human notices. In `both`, the sweep interval can be relaxed well past
30s precisely because it is no longer the primary path — which is itself a saving.

The modes must be genuinely independent, not a flag that shortcuts to the same code: `poll`
must work with no ingress configured at all, and `webhook` must not silently degrade to
polling. Config lives beside the existing per-project binding, and the effective mode is
reported on the project's status surface so an operator can see which path is live.

### 3. Park the session while it waits. The primitive exists

Today `ReviewValidator.Validate` is a blocking `for {}` with a 30s timer
(`forge_review.go:90-112`) **inside the job's own goroutine**, and the worker container is
never torn down while it runs — `teardownWorker` is only reached at cleanup or unplanned exit
(`runner.go:1554`, `:1616`). `JobStateReviewing` is declared `SessionBacked: true` with a
30-minute idle threshold (`job_runner.go:368`), so the stall detector *expects* a live, idle,
context-holding session sitting there for hours.

Item 2 alone does not fix this: waking on an event removes the poll but not the residency.
The saving requires the session to be **gone** while waiting. `SessionStateResumable` and
transcript replay already do exactly this, but are wired only to daemon-restart recovery
(`backend.go:290`). Wire the review wait to the same machinery: on entering the wait, park;
on the event from item 2, resume from the persisted transcript.

This is where the bulk of the token saving in the owner's proposal actually lives.

### 4. Admission needs three inputs, not one

Concurrency is bounded per project only. N autonomous projects each at their quota will
oversubscribe the fleet — observed 2026-09-12. Admission must consider, in one place:

1. the project's `Quotas.MaxConcurrency` (exists),
2. **fleet runner capacity** (absent),
3. the **cost budget** from the consumption ledger (the spend surface added under #835).

### 5. Close the quota bypass — this is a bug, not a gap

A manually triggered `CreateTask(..., AutoSpawn: true)` goes straight through `autoSpawnTask`
(`orchestrator.go:3019`) and is **never checked against `Quotas.MaxConcurrency`**; the quota
is enforced only inside the two sweep paths. Fix independently of everything else here.

### 6. Readiness is one predicate

Eligibility today is: issue open, and carries the binding's trigger label. Nothing else. The
owner's cross-project ruling ("work in another repo only when blocked, and own that issue to
merge") has nothing to attach to. Readiness should also require: dependencies satisfied, not
already owned by a live session, acceptance criteria present. Blocked-on-dependency needs a
parked state that resumes on the upstream merge — and "merged" is not "released", so an
artifact-published signal is needed too.

### 7. Storage tiers: ephemeral by default, persistent where it must be

Owner direction: default to tmpfs to cut SSD wear and speed execution, keep persistence for
long-running agents that must resume. Two constraints make a naive version worse than today:

- **tmpfs counts against the container's memory cgroup.** Job containers run `--memory=12g`.
  A tmpfs workspace plus a Go build and test binaries will OOM inside that budget; tmpfs needs
  an explicit size and the limit must cover both.
- **An ephemeral module/build cache inverts the goal** — a cold `go mod download` per agent
  costs more wall-clock than the writes it saves.

| tier | contents | rationale |
|---|---|---|
| tmpfs | workspace, `/tmp`, test scratch, **all secret material** | the actual churn; zero wear; gone at teardown |
| shared persistent | `GOMODCACHE`, npm, Docker layers, or the existing module proxy | warm and read-mostly across agents |
| per-session persistent | session transcript, and the git branch | what must survive a restart |

**Consequence:** with an ephemeral workspace, pushing the branch after every tree-touching
turn stops being hygiene and becomes correctness — otherwise a restart silently loses work.
That is deterministic, so the engine does it, not the agent.

### 8. Secrets and keys through the platform's own engines

Ephemeral containers still need a forge token, a signing identity, and a CA bundle. Today's
pattern is the counterexample: a live git credential readable from `docker inspect` on a CI
host, and one shared static runner registration token across the fleet. **Environment
variables are not a secrets mechanism** — visible in `docker inspect`,
`/proc/<pid>/environ`, and they leak into logs.

Use `Security().KMS()`, `Security().Secrets()` and `Security().Credentials()`:

- **Never hand over a key that can be used remotely instead.** The commit-signing identity
  must not enter the container. The agent asks the engine to sign the commit object over the
  existing Unix transport; the key stays in KMS and a signature comes back. A compromised
  agent container can then only request signatures — each one audited and attributable —
  rather than exfiltrate the identity. Same for any TLS client identity.
- **For material that must be a file** (git credential helper, CA bundle): mint from
  `Credentials()` into the **tmpfs** tier from item 7. Never on SSD, gone at teardown.
- **Scope and lifetime follow the job:** minted per `(tenant, project, job)`, TTL matched to
  the job lease, revoked on session close. This makes the session reaper a security control
  rather than only hygiene, and retires the shared-static-token pattern.
- **Cross-project access becomes policy, not a permission edit.** Rather than a standing
  write grant on another repository, the loop mints a scoped expiring credential for a
  specific unblocking task — the owner's "only when blocked, and own it to merge" ruling
  expressed as a capability. Motivating case: on 2026-09-12 a verified one-line fix for
  another project's red `main` could not be pushed because the agent identity holds no write
  there, which is the correct boundary and the wrong remedy.

### 9. The engine owns the merge, and the loop must terminate

`Hooks.Complete` (`hooks.go:498`) already merges with the head checks described above, so
ownership is right today and should stay: the agent signals ready, the engine evaluates and
merges. Two things to add:

- **Round exhaustion.** `MaxReviewRounds` exists on the binding (default 3). What happens
  when it is spent is undefined: it must label the PR, park the issue, and escalate — never
  loop.
- **Superseded runs.** `ForgeBackend.CancelCheckRun` is declared (`forge.go:656`) but Gitea
  returns `ErrForgeUnsupported` (`forge_gitea.go:2160`) and nothing calls it; the
  `SupersessionRecord` named in surrounding comments **does not exist in code**. Head pinning
  already prevents acting on a stale result, so this is waste, not incorrectness — but a
  superseded run must at least be recorded, because a re-run of a stale run cancels the run on
  the real head. That happened to two lanes on 2026-09-12.

## Phasing

Each phase is independently shippable and independently useful.

| phase | content | depends on |
|---|---|---|
| 1 | CI-failure classifier (item 1) + quota bypass (item 5) | nothing |
| 2 | event routing to the waiting job, idempotency, and the `poll`/`webhook`/`both` mode switch (item 2) | #881's webhook provisioning |
| 3 | park/resume on review wait (item 3) | phase 2 |
| 4 | three-input admission (item 4) + readiness (item 6) | #835's spend surface |
| 5 | storage tiers (item 7) + secrets via KMS/Credentials (item 8) | nothing; can run parallel to 2–4 |
| 6 | round exhaustion + supersession record (item 9) | nothing |

Phases 1, 5 and 6 have no dependency on the others and can proceed in parallel.

## Explicitly out of scope

- Replacing `ControlLoop`. It is the inner loop and is correct.
- Reimplementing priority, per-project quota, head pinning, or the session reconciler.
- Merging the two outer sweepers into one. They serve different stages and the second exists
  deliberately to retry what the first missed; unifying them is a separate question.

## Verification

Per `CLAUDE.md`: typed errors, no skip guards, no `-short`, >=90% coverage, table-driven with
an error case per function. Additionally:

- The classifier gets a fixture corpus of **real** job logs from 2026-09-12 — one per
  signature in item 1 — asserting verdict and that a genuine test failure is never
  misclassified as void. Misclassifying a real failure as infrastructure would auto-retry a
  broken change forever, so that direction is the one to over-test.
- Event routing: a redelivered webhook wakes a job exactly once; a dropped webhook is still
  caught by the reconciler; an event for a superseded head does not wake anything.
- Discovery modes: `poll` completes a full issue-to-merge cycle with **no ingress configured**;
  `webhook` does not silently fall back to polling; `both` does not double-dispatch when an
  event and a sweep observe the same transition.
- Park/resume: a parked session holds no process, and resumes with its transcript intact.
- Secrets: no secret appears in `docker inspect`, the container environment, or any log; a
  signing request is audited; a credential is unusable after session close.
