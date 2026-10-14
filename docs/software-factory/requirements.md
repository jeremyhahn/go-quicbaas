# Software Factory — Requirements

> **Status: NORMATIVE** · Last verified: 2026-10-14 against the handbook's
> factory documents at `main` 5d88fd4
> The requirements contract of the `software-factory` row of
> [../SPEC.md](../SPEC.md). Conflicts with this document are defects.

## Purpose and scope

The company's software is built today by a manual factory. Coordinators,
departments, the Company Agent and lane agents run it by hand with a set of
host scripts. This document is the contract for automating it. It lists every
step the manual factory performs and classes each one as deterministic or
agent. Each deterministic step becomes a requirement on go-quicbaas code. Each
agent step becomes a requirement on the task-worker contract.

Scope: every step from an open issue to a merged, released and cleaned-up
change, for every project bound to the factory. The forge and the CI runner
stay outside the factory, which reaches them only through their APIs. The
owner's decisions stay with the owner; the factory only holds them in a queue
and applies the answers.

The pieces these requirements land on are described in
[architecture.md](architecture.md).

## Efficiency

| ID | Requirement |
|---|---|
| SF-EFF-01 | The factory runs continuously. No step depends on a person or an agent session being attached. |
| SF-EFF-02 | The factory idles at near-zero token cost. With no event that needs judgement, it makes no model call. |
| SF-EFF-03 | The factory wakes an agent only for a step classed `agent` in the inventory below. Every other step is go-quicbaas code. |
| SF-EFF-04 | The factory spends hardware before tokens. Measurement, polling, scheduling, rule checks and git mechanics run as deterministic code on CPU, never as a model turn. |
| SF-EFF-05 | The factory gives every agent wake the smallest context that decides its question: the records the wake names, snapshots rather than transcripts, and log excerpts within a budget ([SF-A-02](#task-worker-contract)). |

## Layering

| ID | Requirement |
|---|---|
| SF-LAY-01 | The factory is a go-quicbaas service on the single embedded qrdb node, with nothing of its own beside it. |
| SF-LAY-02 | The factory keeps all of its state in qrdb, through the qrdb SDK: queues, claims, lane records, placements, reservations, account usage, trains, the owner queue and reports. It has no separate store, no state files and no lock files. |
| SF-LAY-03 | The factory installs nothing on a host: no watcher, timer, cron job or script. Its schedules are timers inside the service. Work on a build host runs in a container that the fleet node daemon starts ([../agent-fleet/architecture.md](../agent-fleet/architecture.md)). |
| SF-LAY-04 | Verification and git run inside the worker container, never on the daemon ([architecture.md](architecture.md#the-scope-fence)). |

## Step inventory

The manual factory as it runs today, one row per step. **Performed today by**
names the role that does the step now: coordinator, department, Company Agent,
lane agent, owner, or script. A script is named by its role, with the manual
script's name in parentheses. **Class** says what the step becomes in the
factory.

### Intake & triage

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-IT-01 | List the open issues, which are the inbound queue, that no lane or factory label has claimed | coordinator | every status loop | No | deterministic |
| SF-IT-02 | Check issue hygiene: each open issue is labelled, milestoned and `task:ready`, or it is closed | coordinator | every status loop | No | deterministic |
| SF-IT-03 | Decide whether an issue is actionable: it has an objective and a verification, fits one lane, and traces to a SPEC row or to owner words relayed by the Company Agent | coordinator | per candidate issue | Yes | agent |
| SF-IT-04 | Order the ready queue: release-blocker and release-candidate issues first, then by priority and milestone. An unmilestoned `priority:low` issue waits while a stuck PR or a due milestone has work | coordinator | every status loop | No | deterministic |
| SF-IT-05 | Claim the issue with `task:in-progress` (the WIP guard). Add `code-review` when it touches a security-review path | coordinator | per lane | No | deterministic |
| SF-IT-06 | Split an issue that is too large for one lane (more than about 300 turns) into slices, and file them | coordinator | occasional | Yes | agent |
| SF-IT-07 | Write the lane brief from the template: Objective, Context, Scope, Done when, Route line, SPEC row | coordinator | per lane | No, once triage records scope and done-when | deterministic |

### Lane lifecycle

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-LN-01 | Choose the route (backend, model, effort) from the model-usage table, using the issue's labels and size | coordinator | per lane | No | deterministic |
| SF-LN-02 | Launch the lane: one container, an isolated shared clone, one agent session and one per-lane code-intelligence server. A lane never pushes | script: the lane launcher (lane-run) | per lane | No | deterministic |
| SF-LN-03 | Refill a slot within five minutes of a lane ending while ready issues exist, and keep at least three issues in flight | coordinator | per lane end | No | deterministic |
| SF-LN-04 | Classify each lane as running, done, limited or stalled (stalled means no log event for 15 minutes and no background task). Record the end reason: idle grace of 20 s, the 6-hour cap, a limit, or a crash | script: the lane status reader (lane-status, lane-summary) | every health check | No | deterministic |
| SF-LN-05 | Kill and resume a lane that has been stuck for 10 minutes | coordinator | on stall | No | deterministic |
| SF-LN-06 | Resume a crashed lane: fetch its branch, then relaunch it with the resume line | coordinator, script: the lane fetcher (lane-fetch) | on crash | No | deterministic |
| SF-LN-07 | Read a failing lane's ledger and decide whether to resume it, re-slice it or abandon it | coordinator | on repeated failure | Yes | agent |
| SF-LN-08 | Compact the session at its threshold (coordinator 70 %, lane 80 %), writing the ledger first | coordinator, lane agent | per session | No for the trigger | deterministic |
| SF-LN-09 | Write the code for the slice, run its tests and make signed commits | lane agent | per lane | Yes | agent |
| SF-LN-10 | Clean up a merged lane: remove the container and clone (lane-clean), prune the host, and clean the project when it is finished | coordinator, script: the lane cleaner (lane-clean) | per merge | No | deterministic |

### Placement & capacity

The gates and values below are what the manual factory runs today (handbook
`local-software-factory.md` §9 and §10). The owner's rulings on the account
pool and the caps are open with the Company Agent. The factory holds every cap
and threshold as configuration in qrdb, so a ruling changes a value, not code.

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-PL-01 | Measure each build host: busy CPU from `/proc/stat` (running lanes and CI jobs are inside it), memory and CPU pressure, available and total memory, the lanes running and starting from every project, and the 1- and 15-minute load | script: the lane placer (lane-place) | per placement | No | deterministic |
| SF-PL-02 | Pick the host. Committed CPU is the larger of busy CPU and 2 cores per running lane, plus the lanes still starting and the holds. Headroom is 80 % of the cores minus committed. A host is eligible when its headroom covers the lane's CPUs (default 6); its available memory, less this lane's and the starting lanes' memory (default 14 GiB each), stays at or above 10 % of total; memory pressure (full, 10 s) is at most 5 % and CPU pressure at most 15 %; its running and starting lanes are below its lane cap; and its device and project gates admit the lane. The lane placer does not gate on load average or swap-in; the coordinator's slot queue adds per-host 1- and 15-minute load caps on department order. Policy: lowest committed share of cores (the default, host order breaking ties), a fixed order, or most headroom. Verifies are placed the same way | script: the lane placer (lane-place); coordinator: the slot queue for the load caps | per placement | No | deterministic |
| SF-PL-03 | Serialize placement per host. Hold a slot for 180 s and keep a reservation for 300 s | script: the lane placer (lane-place), the account ledger (tokenstate) | per placement | No | deterministic |
| SF-PL-04 | Queue a lane that no host can take, and retry it on the next measurement | coordinator, script: the slot queue | on refusal | No | deterministic |
| SF-PL-05 | Refuse a host that is not prepared, that has a full container disk, or that an operator has excluded | department, Company Agent | on incident | No for detection | deterministic |
| SF-PL-06 | Prepare a new host with the module cache and repository mirror (node-prep) | owner approves, department runs | per new host | No once approved | deterministic |
| SF-PL-07 | Trim the shared build cache to three quarters of its ceiling when a lane or verify ends, sparing entries used in the last 120 minutes | script: the cache trimmer | per lane end | No | deterministic |

### Verification & gates

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-VG-01 | Fetch the finished lane's branch from the worker | script: the lane fetcher (lane-fetch) | per lane end | No | deterministic |
| SF-VG-02 | Check provenance: signatures against the key the forge publishes, the agent identity as author, no tool-attribution trailers, and a clean clone | script: the lane verifier (lane-verify) | per lane | No | deterministic |
| SF-VG-03 | Check that the branch merges cleanly into main (merge-tree) | script: the lane verifier (lane-verify) | per lane | No | deterministic |
| SF-VG-04 | Run the gates in the toolchain image on a build host: lint, vet across build tags, build, race tests, coverage as CI measures it, coded sentinels and the feature integration targets. Keep one log per gate | script: the lane verifier (lane-verify) | per lane | No | deterministic |
| SF-VG-05 | Detect a diff that touches a security-review path, and require the `code-review` label on it | script: the lane verifier (lane-verify) | per lane | No | deterministic |
| SF-VG-06 | Diagnose a failed gate and write the fix | coordinator, lane agent | per failure | Yes | agent |

### Trains & PRs

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-TR-01 | Group verified lanes into a train of 3 to 6 lanes by conflict surface, dependency and review need. Fewer lanes only for a release-blocker or a fix lane | coordinator | per train | No: conflict surface and dependencies can be computed | deterministic |
| SF-TR-02 | Enforce the train cap: at most 3 open trains per project. Verified work waits in its branch | coordinator | per train | No | deterministic |
| SF-TR-03 | Build the train: a merge-tree precheck (any conflict refuses the whole train), signed no-fast-forward merges, and a backup ref | script: the train builder (train-build) | per train | No | deterministic |
| SF-TR-04 | Land a lane that conflicts with its train or with main, using a fix lane on the train | coordinator, lane agent | per conflict | Yes | agent |
| SF-TR-05 | Open the train PR. Its body lists the Closes lines, the docs files changed and the open questions. Label it `task:ready` and inherit `code-review`. Keep it draft until it is labelled. Refuse without a backup ref, and never force-push | script: the train PR opener (train-pr) | per train | No | deterministic |
| SF-TR-06 | Arm native auto-merge on qualifying PRs. Hold `code-review` PRs until they are approved | script: the merge armer (arm-merge) | every status loop | No | deterministic |
| SF-TR-07 | Update a behind or conflicting branch by merging main into it, never by rebase | coordinator | on conflict | No while the merge is clean | deterministic |

### Review

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-RV-01 | Request a review once its preconditions hold: the `code-review` label, the base merged in, the `task:in-progress` claim, and CI green | script: the review requester (review-request) | per PR head | No | deterministic |
| SF-RV-02 | Review the diff and return APPROVED or REQUEST_CHANGES | script: the review requester runs the review engine | per PR head | Yes | agent |
| SF-RV-03 | Track every REQUEST_CHANGES. It is answered within 2 hours; unanswered at 24 hours it is a defect | coordinator | per verdict | No | deterministic |
| SF-RV-04 | Judge each finding, then accept it as a fix or answer it with a written rebuttal | coordinator | per verdict | Yes | agent |
| SF-RV-05 | Re-request review of an approval that a later push made stale | coordinator | per push | No | deterministic |
| SF-RV-06 | Escalate a review deadlock to the owner after 3 rounds | coordinator | on deadlock | No | deterministic |
| SF-RV-07 | Refuse self-approval: the author identity never approves its own PR | coordinator | per PR | No | deterministic |

### CI

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-CI-01 | Wait for the required check on every train head | coordinator | every health check | No | deterministic |
| SF-CI-02 | Tell a queue from a failure. A long pending run is queued behind batch runs, so do not re-push, re-run or cancel it | coordinator | per pending run | No | deterministic |
| SF-CI-03 | Classify a red run. A cancelled run is not a code failure. Re-run only failed jobs, and only with a known infrastructure cause | coordinator | per red run | No for the known signatures | deterministic |
| SF-CI-04 | Diagnose a code failure and answer the red PR within 1 hour, with a fix commit or a written cause | coordinator, lane agent | per red run | Yes | agent |
| SF-CI-05 | Dispatch the batch suites on main after each merge (scheduled workflows are off) | coordinator | per merge | No | deterministic |
| SF-CI-06 | File one issue per real batch finding, deduplicated against the open issues | coordinator | per finding | No once classified | deterministic |

### Merge & release

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-MR-01 | Check the merge preconditions: the PR is open; the required check is green on the head; on `code-review` PRs the approval is current; no REQUEST_CHANGES stands; and the forge accepts the branch | coordinator | per merge pass | No | deterministic |
| SF-MR-02 | Merge by hand, one PR per pass, by squash, pinned to the head commit, after a dry run | coordinator | per merge pass | No | deterministic |
| SF-MR-03 | Fast-forward the local main after each merge | coordinator | per merge | No | deterministic |
| SF-MR-04 | Order merges by their re-verification cost | coordinator | per merge pass | No | deterministic |
| SF-MR-05 | Run the stranded-work detector (the merge commit tree equals the branch tip tree), then delete the branch | coordinator | per merge | No | deterministic |
| SF-MR-06 | Confirm that the closed issues closed, and release the claim | coordinator | per merge | No | deterministic |
| SF-MR-07 | Tag a release on a green main on the forge, then run the post-tag consumer check | coordinator | per release | No for the mechanics | deterministic |

### Accounts & tokens

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-AC-01 | Record each account's 5-hour and 7-day usage and reset times | script: the account ledger (tokenstate) | per launch and per limit event | No | deterministic |
| SF-AC-02 | Select the account for a launch from the pool of five Claude accounts; the Codex login is outside the pool and this rule. Eligible: not excluded, not held by a limit, 5-hour usage at most 70 %, 7-day usage at most 90 %. Room: fewer than 5 lanes on the account across every project, and remaining weekly headroom of 5 % per lane including the new one. Accounts with room rank first; with none, an eligible account without room still takes the lane. Rank: lowest 5-hour usage, then earliest weekly reset (a never-used account first, an unknown reset last), then fewest live lanes, least recorded usage, and name. None eligible: no launch, and report the earliest time an account comes back | script: the account ledger (tokenstate) | per launch | No | deterministic |
| SF-AC-03 | Class each lane light, moderate or heavy by turns, cost and peak context (weights 0.3, 0.5 and 1). Start at most 1 heavy lane per account per 30 minutes, and mix weights on one account | coordinator | per launch | No | deterministic |
| SF-AC-04 | Classify a limit hit, in this order: a rejected rate-limit event, then the error category, then a 429, then the text. Mark the lane limited | script: the lane status reader | per limit event | No | deterministic |
| SF-AC-05 | Rotate a limited lane onto another eligible account, or wait for the earliest reset and report it | coordinator | per limit event | No | deterministic |

### Health & monitoring

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-HM-01 | Run the health check: lanes, hosts, runners, CI on every train, and the fast-forward of main | coordinator | every 10 minutes | No | deterministic |
| SF-HM-02 | Run the coordinator tick | coordinator | every 30 minutes | No | deterministic |
| SF-HM-03 | Fleet gather: collect the lane, host, train, PR and account state of every project into one view | department | every status loop | No | deterministic |
| SF-HM-04 | Follow the lane transcripts into a rotating log, with size rotation, a keep count, an age limit and a free-space floor | script: the fleet streamer (fleet-stream) | continuous | No | deterministic |
| SF-HM-05 | Detect a stuck PR: none landing this hour, red with no answer, or a review overdue | coordinator, department | every status loop | No | deterministic |
| SF-HM-06 | Detect a failing host (full disk, memory exhaustion, or a host that stops answering) and stop placing on it | department, Company Agent | on incident | No | deterministic |

### Messaging & reporting

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-MS-01 | Run the status loop: wake, gather, decide, report | coordinator | every 55 minutes | No for the loop itself | deterministic |
| SF-MS-02 | Write the status line: the lane count taken from live containers, trains as N/3, open PRs, the PR landing this hour and its blocker, and the open questions | coordinator | every status loop | No | deterministic |
| SF-MS-03 | Roll the status lines up into the department's report files | coordinator | every status loop | No | deterministic |
| SF-MS-04 | Deliver an order to a session by typing it into the prompt and reading it back from the transcript (coord-run send, dept-run send) | Company Agent, department | per order | No | deterministic |
| SF-MS-05 | Interpret an order and change the plan. The newest order wins, except over the credential, history, architecture and spend rules | coordinator | per order | Yes | agent |
| SF-MS-06 | Write the owner-facing digest of the fleet's state | Company Agent | per reporting cycle | Yes | agent |

### Docs & agreement

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-DA-01 | Run the SPEC/ROADMAP agreement check. The set of features in the SPEC Features table equals the set in the ROADMAP. Each ROADMAP line has the form `- **f** — outcome — link — status`. Each line sits in the section its SPEC status maps to. A mismatch is fixed in the open docs PR that finds it or in one follow-up docs PR; no issue is filed for it | department | every PM cycle | No | deterministic |
| SF-DA-02 | Report every PR that changes SPEC, ROADMAP or `docs/` to the Company Agent, at open and at merge, with its number, head and the rows or files changed | coordinator | per docs PR | No | deterministic |
| SF-DA-03 | Check that each lane traces to a SPEC row or to relayed owner words, and that its brief names the row | coordinator, department | per lane | No | deterministic |
| SF-DA-04 | Write and update the feature documentation for a slice | lane agent | per feature slice | Yes | agent |
| SF-DA-05 | Check publication rules: no internal host names, addresses or home paths in a feature folder, and a status banner on every feature doc | coordinator | per docs PR | No | deterministic |
| SF-DA-06 | Check a seam: a cross-project feature is a row with the same name in both SPECs before any issue about it is filed | coordinator, Company Agent | per seam | No | deterministic |

### Owner queue

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-OQ-01 | Write an owner question: its options, and the reversible choice taken while it waits | coordinator | per decision | Yes | agent |
| SF-OQ-02 | Keep the owner queue: number each question, relay it through the Company Agent, track it until answered, and keep working on the reversible choice | coordinator, Company Agent | per decision | No | deterministic |
| SF-OQ-03 | Apply an owner answer that picks one of the listed options | coordinator | per answer | No | deterministic |

### Retention

| ID | Step | Performed today by | Frequency | Judgement needed | Class |
|---|---|---|---|---|---|
| SF-RT-01 | Retain the lane's transcript before lane-clean | coordinator | per lane clean | No | deterministic |
| SF-RT-02 | Push the backup refs and verify them before a branch is deleted | script: the train builder (train-build), coordinator | per train | No | deterministic |
| SF-RT-03 | Rotate and age out the fleet logs and per-gate logs by size, count, age and free space | script: the fleet streamer (fleet-stream) | continuous | No | deterministic |
| SF-RT-04 | Prune ended containers, clones and volumes from each host | script: the lane cleaner (lane-clean) | per lane clean | No | deterministic |
| SF-RT-05 | Keep each lane's ledger and end reason with its record | lane agent, coordinator | per lane | No | deterministic |

## Deterministic requirements

Each deterministic row is a requirement on go-quicbaas code. **Designed in**
links the existing design where there is one.

| ID | Requirement | Covers | Designed in |
|---|---|---|---|
| SF-D-01 | The factory reads the open issues of every bound project from the forge, by webhook or poll, and records each one in qrdb with its labels, milestone, priority and claim. | SF-IT-01 | [triage.md](../sdlc/triage.md#which-issues-are-candidates) |
| SF-D-02 | The factory reports every open issue that is not labelled, milestoned and `task:ready`. It never closes an issue itself. | SF-IT-02 | [triage.md](../sdlc/triage.md#triage-never-closes-an-issue) |
| SF-D-03 | The factory orders the ready queue by rule: release-blocker and release-candidate issues first, then priority label and milestone. An unmilestoned `priority:low` issue waits while a stuck PR or a due milestone has work. | SF-IT-04 | [../sdlc/architecture.md](../sdlc/architecture.md) |
| SF-D-04 | The factory claims an issue with `task:in-progress` before any worker starts, and skips an issue that is already claimed. | SF-IT-05 | [autonomy-and-dispatch.md](../orchestration/autonomy-and-dispatch.md#the-claim-takes-the-task-off-the-queue) |
| SF-D-05 | The factory renders the lane brief from the issue and the triage verdict, with no model turn: Objective, Context, Scope, Done when, Route and SPEC row. | SF-IT-07, SF-DA-03 | [prompt-sets.md](prompt-sets.md) |
| SF-D-06 | The factory refuses to brief an issue that names neither a SPEC row nor relayed owner words, and puts the issue on the owner queue. | SF-DA-03, SF-OQ-02 | — |
| SF-D-07 | The factory picks a lane's backend, model and effort from a routing table keyed on the issue's labels and size class. | SF-LN-01 | [agents.md](agents.md) |
| SF-D-08 | The factory launches a lane as one container holding an isolated clone, one agent session and one per-lane code-intelligence server. The lane's container never pushes. | SF-LN-02 | [autonomy-and-dispatch.md](../orchestration/autonomy-and-dispatch.md) |
| SF-D-09 | The factory starts the next ready issue within five minutes of a lane ending, while ready issues and capacity exist, and keeps at least three issues in flight. | SF-LN-03 | [admission-and-readiness.md](admission-and-readiness.md#admission) |
| SF-D-10 | The factory classifies each lane as running, done, limited or stalled from its event log, and records the end reason: idle grace, session cap, limit or crash. | SF-LN-04 | [../accounts/architecture.md](../accounts/architecture.md#the-reaper-and-the-stall-detector) |
| SF-D-11 | The factory restarts a stalled lane, and resumes a crashed lane from its fetched branch and ledger. It does neither by waking an agent. | SF-LN-05, SF-LN-06 | [../accounts/architecture.md](../accounts/architecture.md#recovery) |
| SF-D-12 | The factory rolls a session at its context threshold once the ledger is written, and resumes the work from the ledger. | SF-LN-08 | [../sdlc/architecture.md](../sdlc/architecture.md#a-full-context-rolls-the-session-not-the-attempt) |
| SF-D-13 | The factory deletes a lane's branch, container, clone and volumes only after three things: the stranded-work detector passes, the backup refs are verified, and the lane's records are retained. | SF-LN-10, SF-MR-05, SF-RT-02, SF-RT-04 | — |
| SF-D-14 | Before each placement, the factory measures every build host: busy CPU with running lanes and CI jobs inside it, memory and CPU pressure, available and total memory, the lanes running and starting from every project, container disk use, and the 1- and 15-minute load. | SF-PL-01 | [admission-and-readiness.md](admission-and-readiness.md#fleet-capacity-two-sources-one-answer) |
| SF-D-15 | The factory places a lane or a verify only on a host that passes the gates of SF-PL-02, plus any per-host load caps configured, and that is prepared, under its disk ceiling and not excluded. It chooses among eligible hosts by the configured policy: lowest committed share of cores by default, or a fixed order, or most headroom. | SF-PL-02, SF-PL-05 | [admission-and-readiness.md](admission-and-readiness.md#fleet-capacity-two-sources-one-answer) |
| SF-D-16 | The factory serializes placement per host, so two concurrent decisions never both take the last capacity on one host. A placed lane counts against the host's lane cap, committed CPU and memory from the moment it is placed until its own load shows in the host's measurements. The factory holds a placed slot until the lane reports in, and expires the reservation after its lease. | SF-PL-03 | [../accounts/architecture.md](../accounts/architecture.md#reservations) |
| SF-D-17 | The factory parks a lane that no host can take, and readmits it when a host measurement changes. | SF-PL-04 | [admission-and-readiness.md](admission-and-readiness.md#parking-not-waiting) |
| SF-D-18 | The factory prepares a new host only after the owner's approval is recorded, and only by starting a container through the fleet node daemon. | SF-PL-06 | [../agent-fleet/architecture.md](../agent-fleet/architecture.md) |
| SF-D-19 | When a lane or verify ends, the factory trims the project's shared build cache to three quarters of its ceiling, sparing entries used in the last 120 minutes. | SF-PL-07 | [storage-tiers.md](storage-tiers.md) |
| SF-D-20 | The factory verifies a finished lane in the toolchain image on a build host. The checks are signatures against the key the forge publishes, the agent identity as author, no tool-attribution trailers, a clean merge into main, and the gates: lint, vet across build tags, build, race tests, coverage as CI measures it, coded sentinels and the feature integration targets. It keeps one log per gate. | SF-VG-01, SF-VG-02, SF-VG-03, SF-VG-04 | [../sdlc/architecture.md](../sdlc/architecture.md#verification-judges-the-merge-result-not-the-branch-tree) |
| SF-D-21 | The factory puts the `code-review` label on a change that touches a security-review path or tags a release, and on no other change. | SF-IT-05, SF-VG-05 | [../sdlc/review.md](../sdlc/review.md) |
| SF-D-22 | The factory groups verified lanes into trains of 3 to 6 by conflict surface (pairwise merge-tree), declared dependencies and review need. A train has fewer lanes only for a release-blocker or a fix lane. | SF-TR-01 | [../sdlc/merge-train.md](../sdlc/merge-train.md#building) |
| SF-D-23 | The factory keeps at most 3 open trains per project. Verified work waits in its branch. | SF-TR-02 | [../sdlc/merge-train.md](../sdlc/merge-train.md#what-wakes-a-train) |
| SF-D-24 | The factory builds a train with a merge-tree precheck that refuses the whole train on any conflict, then signed no-fast-forward merges. It pushes and verifies a backup ref before it opens the PR. | SF-TR-03, SF-RT-02 | [../sdlc/merge-train.md](../sdlc/merge-train.md#building) |
| SF-D-25 | The factory opens the train PR with the Closes lines, the docs files changed and the open questions in its body. It labels the PR `task:ready`, inherits `code-review` from its issues, keeps it draft until it is labelled, and never force-pushes. | SF-TR-05 | [../sdlc/merge-train.md](../sdlc/merge-train.md) |
| SF-D-26 | On every pass, the factory arms native auto-merge on qualifying PRs and holds `code-review` PRs until they are approved. | SF-TR-06 | [../sdlc/merge-train.md](../sdlc/merge-train.md#landing) |
| SF-D-27 | The factory updates a behind or conflicting branch by merging main into it, never by rebase. It wakes an agent only when that merge conflicts. | SF-TR-07 | [../sdlc/merge-train.md](../sdlc/merge-train.md#restart) |
| SF-D-28 | The factory requests a review when the preconditions hold: the label, the base merged in, the claim held and CI green on the head. It re-requests review after any push that makes an approval stale. It never lets the author identity approve. | SF-RV-01, SF-RV-05, SF-RV-07 | [../sdlc/review.md](../sdlc/review.md) |
| SF-D-29 | The factory tracks every REQUEST_CHANGES. It wakes the review-response worker at once, marks the PR as a defect when it is unanswered at 24 hours, and puts the PR on the owner queue after 3 rounds. | SF-RV-03, SF-RV-06 | [../sdlc/review.md](../sdlc/review.md) |
| SF-D-30 | The factory follows the required check on every train head. It treats a long pending run as a queue (no re-push, re-run or cancel) and a cancelled run as no verdict. | SF-CI-01, SF-CI-02 | [ci-failure-classification.md](../sdlc/ci-failure-classification.md#the-verdicts) |
| SF-D-31 | The factory classifies a red run by signature. It re-runs only the failed jobs of a known infrastructure failure, within the re-run budget. For a code failure it wakes a fix worker in time to answer within 1 hour. | SF-CI-03 | [ci-failure-classification.md](../sdlc/ci-failure-classification.md#the-re-run-budget) |
| SF-D-32 | The factory dispatches the batch suites on main after each merge. It files one issue per distinct real finding, deduplicated against the open issues by test and signature. | SF-CI-05, SF-CI-06 | — |
| SF-D-33 | The factory merges a PR only when it is open, its required check is green on the head, its approval is current on a `code-review` PR, and no REQUEST_CHANGES stands. It merges one PR per pass, by squash, pinned to the head, after a dry run, in order of re-verification cost. | SF-MR-01, SF-MR-02, SF-MR-04 | [../sdlc/merge-train.md](../sdlc/merge-train.md#landing) |
| SF-D-34 | After each merge, the factory fast-forwards its mirror of main, confirms the closed issues and releases the claim. | SF-MR-03, SF-MR-06 | [../sdlc/architecture.md](../sdlc/architecture.md#outcomes-every-way-a-task-stops-and-what-reaches-the-issue) |
| SF-D-35 | The factory tags a release on a green main when the release gate is met, then runs the post-tag consumer check. | SF-MR-07 | — |
| SF-D-36 | The factory keeps each account's 5-hour and 7-day usage and reset times in qrdb. | SF-AC-01 | [../accounts/architecture.md](../accounts/architecture.md#the-consumption-ledger) |
| SF-D-37 | The factory selects the account for a launch by the eligibility, room and ranking rules and the lane weights of SF-AC-02 and SF-AC-03. | SF-AC-02, SF-AC-03 | [../accounts/architecture.md](../accounts/architecture.md#selection) |
| SF-D-38 | The factory classifies a limit hit in the order of SF-AC-04 and marks the lane limited. It then rotates the lane onto an eligible account, or holds it until the earliest reset and reports that time. | SF-AC-04, SF-AC-05 | [../accounts/architecture.md](../accounts/architecture.md#rotating-a-session-onto-another-account), [../sdlc/architecture.md](../sdlc/architecture.md#a-refused-account-rolls-the-session-onto-another-account) |
| SF-D-39 | The factory runs the health pass every 10 minutes as code: lanes, hosts, runners, CI on every train, and main. The timed status loop and the coordinator tick become events on the same pass. A pass wakes an agent only when a rule fires that names an agent step. | SF-HM-01, SF-HM-02, SF-MS-01 | — |
| SF-D-40 | The factory keeps one fleet snapshot per project in qrdb (lanes, hosts, trains, PRs, accounts). Every report and every agent wake reads the snapshot, never a transcript. | SF-HM-03 | — |
| SF-D-41 | The factory streams lane events into a log with size rotation, a keep count, an age limit and a free-space floor. | SF-HM-04, SF-RT-03 | — |
| SF-D-42 | The factory detects a stuck PR and a failing host. It reports the PR, and stops placing on the host until the host passes the next measurement. | SF-HM-05, SF-HM-06 | [../agent-fleet/architecture.md](../agent-fleet/architecture.md#liveness-heartbeat-and-recovery) |
| SF-D-43 | On every pass the factory writes the status line from the snapshot (lanes from live containers, trains as N/3, open PRs, the PR landing this hour and its blocker, open questions) and appends it to the project's report in qrdb. | SF-MS-02, SF-MS-03 | — |
| SF-D-44 | The factory delivers an order as a typed message on the recipient's queue in qrdb and records when it is read. It never types into a terminal. | SF-MS-04 | — |
| SF-D-45 | The factory runs the SPEC/ROADMAP agreement check of SF-DA-01 on every change to either file, and refuses a change that breaks agreement. It files no issue for a mismatch. | SF-DA-01 | — |
| SF-D-46 | The factory reports every PR that changes SPEC, ROADMAP or `docs/` to the Company Agent, at open and at merge, with its number, head and the rows or files changed. | SF-DA-02 | — |
| SF-D-47 | The factory refuses a PR that adds an internal host name, address or home path to a feature folder, or a feature doc without its status banner. | SF-DA-05 | — |
| SF-D-48 | The factory refuses to file an issue about a cross-project feature until a row with the same name exists in both projects' SPECs. | SF-DA-06 | — |
| SF-D-49 | The factory keeps the owner queue in qrdb. Each question is numbered and holds its options, the reversible choice in force, its relay path and its answer. The factory keeps working on the reversible choice. When an answer picks a listed option, it applies the answer with no model turn. | SF-OQ-02, SF-OQ-03 | — |
| SF-D-50 | Before a lane is cleaned, the factory retains the lane's transcript, ledger, end reason and gate logs with the lane record. It ages them out by policy. | SF-RT-01, SF-RT-03, SF-RT-05 | [storage-tiers.md](storage-tiers.md) |

### Scripts these requirements replace

The handbook's SDLC automation reference (`software/docs/20`) lists the
factory scripts with no go-quicbaas replacement mapped yet. These rows map
them.

| Script | Replaced by |
|---|---|
| `lane-place` | SF-D-14 to SF-D-17 |
| `lanelog.py` `select_account` | SF-D-36 to SF-D-38 |
| `node-prep` | SF-D-18 |
| `spec-roadmap-check.py` | SF-D-45 |

## Task-worker contract

Each agent row is a requirement on the task worker the factory wakes. The rules
below hold for every wake.

| ID | Requirement | Designed in |
|---|---|---|
| SF-A-01 | Every wake is a task with a typed input record and a typed output record. The factory reads the result from structure, never from prose. An output that is not a valid record is asked for once more, and after that the task is parked. | [triage.md](../sdlc/triage.md#the-verdict-is-read-from-structure-never-from-prose), [review.md](../sdlc/review.md#a-reply-that-stated-no-verdict-is-asked-once-more-for-one) |
| SF-A-02 | A wake's input is the smallest context that decides its question: the records its row names, the fleet snapshot rather than transcripts, and log excerpts within the log budget. The worker's tools are cut down to what its output needs. | [ci-failure-classification.md](../sdlc/ci-failure-classification.md#two-log-budgets) |
| SF-A-03 | Every wake runs under its role's spend window and its turn cap. A wake that reaches either ends with its outcome recorded and is not re-run automatically. | [agents.md](agents.md#spend-limits), [../sdlc/architecture.md](../sdlc/architecture.md#this-task-has-spent-its-attempt-budget) |
| SF-A-04 | No timer wakes an agent. Every wake is recorded with the event that triggered it. | [triage.md](../sdlc/triage.md#what-wakes-it) |
| SF-A-05 | No agent's instructions ask it to do deterministic work. The factory does that work, and the prompt does not mention it. | [prompt-sets.md](prompt-sets.md#offloadable-work-what-we-are-still-paying-a-model-to-do) |

| ID | Worker | Woken when | Input context | Output | Budget or cap | Covers |
|---|---|---|---|---|---|---|
| SF-A-06 | Triage | A candidate issue is new, or has changed since its last verdict | The issue's title, body, labels and milestone; the SPEC row it names; the IDs of its cluster | `ready`, `not ready` with the missing fields, or `split` with child slices (objective, scope, done when, SPEC row) | Triage bounding: one per project, off the worker budget ([triage.md](../sdlc/triage.md#bounding-one-per-project-and-off-the-worker-budget)) | SF-IT-03, SF-IT-06 |
| SF-A-07 | Coding | A ready, claimed issue has a host and an account | The rendered brief, the checked-out branch, and the ledger on a resume | Signed commits on the branch, the ledger and the turn outcome. No push and no PR | One slice per session; more than about 300 turns means split; the 6-hour session cap; the role's spend window | SF-LN-09, SF-DA-04 |
| SF-A-08 | Fix | A failed gate, or a CI code-failure verdict | The failing gate's or job's log excerpt within the log budget, the diff and the brief | Fix commits on the same branch or train, or a written cause | The role's spend window. A red PR is answered within 1 hour | SF-VG-06, SF-CI-04 |
| SF-A-09 | Review | The review preconditions hold on a new head | The diff against base, the changed-file list, the issue's done-when, and the conventions that no lint rule checks | APPROVED, or REQUEST_CHANGES with inline findings | One review job per PR at a time; the reviewer seat's spend window; the independence rule ([agents.md](agents.md#failover)) | SF-RV-02 |
| SF-A-10 | Review response | A REQUEST_CHANGES verdict | The findings, the lines they cite and the brief | For each finding: accept (it becomes a fix task) or a rebuttal | 3 rounds; answered within 2 hours | SF-RV-04 |
| SF-A-11 | Conflict resolution | Merging main into a branch, or a lane into a train, conflicts | The conflicting hunks with both sides, and both lanes' briefs | A merge commit that resolves the conflict, or a re-slice verdict | The role's spend window | SF-TR-04 |
| SF-A-12 | Lane recovery | The restart and resume rules of SF-D-11 are used up for a lane | The lane's ledger, its end reasons and its gate results | `resume`, `re-slice` or `abandon`, with a reason | The role's spend window | SF-LN-07 |
| SF-A-13 | Order intake | An order arrives that is not a structured command | The order text, the rules it may not override, and the snapshot | A structured change (configuration, queue or hold) or an owner question | The role's spend window | SF-MS-05 |
| SF-A-14 | Owner question | A rule puts a decision on the owner queue | The triggering record, the rule and the known options | The question text, the options and the reversible choice | The role's spend window | SF-OQ-01 |
| SF-A-15 | Owner digest | The reporting cycle the Company Agent sets | The change in the snapshot since the last digest, and the open owner questions | The digest text | The role's spend window | SF-MS-06 |

## Token cost

The token cost of each step comes from the Software Department's step
inventory, which it builds from its lane ledgers. When the department supplies
that inventory, the cost becomes a column of the step inventory. This document
states no cost figures until then.
