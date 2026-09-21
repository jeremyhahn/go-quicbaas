# Changelog

All notable changes to QuicBaaS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.10.0-alpha] - 2026-09-21

MINOR: consumers must build with Go 1.27.0 — both modules, the images and CI
now declare it, and the Software Factory worker image ships it. The rest is
the factory learning to run on a small local model: triage measures the
repository facts itself, sets aside replies that are not verdicts, bounds how
often it re-asks, and says on its audit line what each turn actually did.
Reviewed changes stop deadlocking on comment verdicts, the unblock sweep runs,
and a merge re-judges the backlog it may have unblocked.

### Added

- **`--codex-reasoning-effort`**, defaulting to `medium`. The codex backend
  previously ran at whatever the installed CLI defaulted to, which was stated
  nowhere. Code review is the one role whose output is a GATE: a shallower pass
  does not fail visibly, it approves what it should have caught. The depth a
  review reasons at is now a deployment decision an operator can read and
  change.

- **`deployments/inference/vllm-cuda/`**, the compose file for serving the
  triage model on a CUDA card. It carries the two flags whose absence is
  invisible until a real job runs: without `--enable-auto-tool-choice` and
  `--tool-call-parser`, vLLM serves plain completions perfectly well and
  answers every agentic turn with a 400. The parser must also match the
  model's chat template — Qwen3.8 emits XML-form tool calls, so `qwen3_xml`
  rather than `hermes`, and a parser that disagrees yields zero tool calls
  rather than an error.

- **Triage measures the repository facts itself.** The file paths and
  backticked identifiers an issue names are probed with `git ls-files` and
  `git grep` in the session's checkout before the model is prompted, and the
  answers go into the prompt as a "Repository facts" section measured at the
  head being judged. A frontier model would have run those commands; the NPU
  model never did — `tool_uses=0` on every triage turn of 2026-09-21 while it
  wrote "I have examined the code" — so its already-done verdicts were
  ungrounded by construction. The deterministic half of the check is now done
  in Go for every rung; the judgement stays with the model.

### Changed

- **Go 1.27.0 across the module, the images and CI.** Go 1.27.0 has been the
  portfolio standard since 2026-09-01 and this repository was still declaring
  1.26.7 everywhere — including the Software Factory worker image, which ships
  ONE toolchain under `GOTOOLCHAIN=local`. A factory project that had already
  moved to 1.27 (go-luks2, PR #202) could therefore not be built, tested or
  reviewed by the factory at all. Both `go.mod` files, the Makefile's
  `GO_VERSION`, every Dockerfile base, the integration `go.work` stubs and the
  four workflow pins now say 1.27.0, and golangci-lint moves to v2.13.2, the
  first release that lints a 1.27 module and the version the fleet's ci-runner
  image bakes.

- **An OpenVINO turn now says what it did.** Every command the tool loop ran
  is reported as a `tool_use` block ahead of the answer, and an answer forced
  on the final, tool-less round carries the stop reason `tool_budget_spent`
  instead of `end_turn`. The triage audit line records both. Before, a turn
  that ran twelve commands and then gave a 27-byte answer was
  indistinguishable from one that answered from the prompt alone, which is
  why the NPU's give-ups on go-luks2 could not be told from its judgements.

- **The reviewer charter no longer tells the reviewer to withhold a verdict.**
  It defined `comment` as "deciding it needs something you do not have" and
  closed with "if you cannot reach a verdict, say why and declare `comment`".
  A thorough reviewer can nearly always name something it could not confirm,
  so that made `comment` the default outcome — and a comment satisfies no
  gate, so the change waits forever and every later round is blocked too.
  Observed three times on clean changes: on go-luks2 #196 the reviewer read
  all 21 files and found no defect, and on #200 it traced every caller with
  Serena, found "no actionable code defects", and declared `comment` because a
  changelog fragment referenced a companion issue in another repository that
  it could not open.

  `comment` is now reserved for a change that could not be judged at all — an
  explicit draft, or a diff the reviewer could not read. An unverifiable
  detail is recorded as a caveat inside an approval, or, when the change
  *asserts* the thing that cannot be confirmed, as an actionable
  `request-changes` naming what to remove or prove. The rigour is unchanged:
  the reviewer is not told to approve anything it thinks is wrong, and the
  gate still requires an independent approval over green checks.

- **A spent tool-call budget now asks the model to conclude instead of failing
  the turn.** The agentic loop withdraws its tools for one final round and says
  the budget is gone, so the turn ends in the judgement a human triager makes
  when their time box expires. Measured cause: a triage turn read the CI
  workflow, `go.mod`, the upstream module versions and the implementation —
  every call on point — and then returned nothing because the budget ended
  mid-investigation. A model that is asked outright for a conclusion and still
  produces nothing is reported exactly as before; no partial text is promoted
  to an answer.

- **Triage is told it has not read the other issues.** The duplicate
  candidates reach the prompt as number and title only, and a small model
  filled the gap by inventing what the other issue "already addresses" —
  go-luks2#141 (a coverage gate) was declared a duplicate of #195 (a
  filesystem probe) twice running, on the strength of both mentioning CI.
  The charter and the candidate list now say the bodies are unread, that a
  shared area is not the same work, that a prerequisite is not a duplicate
  of what it precedes, and that when the titles alone do not settle it the
  issue is judged on its own.

- **Triage stops asking after two unusable replies at one head.** A reply set
  aside leaves no mark on the issue, so the issue is a candidate again on
  every pass — and a model that fails an issue the same way each time was
  asked every fifteen minutes for as long as the head stood. The runner now
  counts set-asides per issue and default-branch head, skips the issue after
  two (audited as `sdlc:triage_skipped`), and asks again only when the head
  moves or somebody comments, both of which change what the model is shown.

### Fixed

- **A parked issue was parked for ever, so every project ran out of work.** A
  triage verdict is an answer about a *tree* — "blocked on another change",
  "the prerequisite has not landed", "not actionable yet" — and the only thing
  that could reopen one was a person commenting on the issue. Nothing about the
  repository changing reopened anything, so a backlog the factory parked stayed
  parked however much the code moved underneath it. Measured: 23 of 23 open
  issues across two bound projects sat `factory:escalated` / `needs-info` /
  `blocked`, every verdict predating the day's fixes, while triage woke every
  fifteen minutes, found nothing, and slept.

  Every verdict now records the commit it was formed against, and a parked
  issue is re-judged when the project's default branch has moved since — which
  is exactly the event that makes "blocked on another change" wrong. A verdict
  carrying no commit is re-examinable, so a backlog parked by an older build
  gets one fresh reading.

- **A triage job recorded neither its backend nor its model**, so the queue
  showed rows that could not tell an operator whether a sweep was about to
  spend a local GPU turn or a frontier-model one. Both are now set when the
  job is started.

- **A merge no longer re-asks a question a person owes.** `needs-info` names a
  specific thing only a person can supply, so movement in the repository says
  nothing about it; reopening those on every merge spent a model turn per
  parked issue to reach the same verdict again, which on a parked backlog is
  most of the fleet's triage cost. Only `not-actionable` and escalated
  verdicts — the ones that are answers about a tree — are reopened by a merge.

- **A verdict now replaces the previous disposition instead of stacking on
  it.** Each disposition only ever added its label, so an issue judged
  needs-info after being escalated carried both: two contradictory answers,
  with no way for an operator to tell which was current. Six of eleven open
  go-luks2 issues were in that state after a single re-triage pass.

- **A commit status was treated as proof that a head had been reviewed.** The
  review floor decided "has this head been judged" from the presence of a
  status under the reviewer's context. A status is only a *projection* of a
  verdict, and the two disagree whenever a review declines to judge — the
  reviewer publishes a status, records no verdict, and the head then reads as
  reviewed forever while the gate correctly still wants a review for it.
  Nothing could ever dispatch one. Three finished, green changes on go-luks2
  were stranded exactly that way.

  The floor now asks the forge for the verdict, using the same
  `forgeReviewVerdicts` map the gate reduces reviews with, so "judged" means
  the same thing in both places. The two questions are separated: a *pending*
  status means a reviewer is at work and a second must not be started; whether
  the head was *judged* is answered by an approval or a request for changes
  bound to that head. A comment is neither.

  It costs one review listing per open pull request, paid before the answer is
  known. That is a deliberate trade: one forge read against a change that can
  never merge.

- **A review that declined to judge deadlocked its pull request permanently.**
  A `comment` verdict published a `pending` commit status, on the sound
  reasoning that it decides nothing and so must not go green. But pending is
  the shape of a review still *running*, and the review floor deliberately
  skips any head already carrying a status under the reviewer's context so that
  an in-flight review is never given a duplicate reviewer. A finished review
  that left pending behind therefore blocked the merge gate **and** the
  backstop that exists to recover the gate: nothing could re-review that head,
  ever. Measured on go-luks2 #202 — the round-2 commit answered the round-1
  finding, CI went green, and it then waited indefinitely on a review no code
  path could dispatch.

  The fix is in the floor, not the mapping. `pending` is the only status that
  means "no verdict yet", which is exactly what a comment leaves behind — it
  binds onto no review record, so the gate correctly still wants a review and
  the floor must still be able to dispatch one. The floor now treats a
  `pending` reviewer status older than the job stall bound as abandoned rather
  than in flight, sharing the engine's own "the agent is presumed gone" answer
  rather than being a second, independently drifting number. An undated pending
  status is never stale, so a forge that reports no timestamp does not cause a
  re-dispatch on every pass. `error` is mapped alongside `comment` for the same
  reason: a review that produced no verdict is owed another attempt.

- **The reviewer's own commit status decided whether the head was reviewable.**
  Both dispatch paths ask "is this head green enough to review" by evaluating
  every status on the head — including the one the reviewer itself publishes
  there. That is circular: it asks a review to be green before it has run.
  While the floor short-circuited on status presence the question never
  arose, so the fault was latent; making a stale pending status reviewable
  exposed it immediately, and mapping a comment to `failure` would have
  re-created the same deadlock in a different colour. The two dispatch sites
  now exclude the reviewer's context, which is what the sweep's own
  documentation already promised ("every *other* check"). The merge gate still
  counts it, because there a failed review is exactly what should hold a
  change back.

- **The reviewer was told to verify acceptance criteria and never given them.**
  Its charter says to check the change against the issue it closes; the review
  prompt carried the pull request's title, body and file list but not the
  issue, and the reviewer's container holds no forge credential — so every
  attempt to fetch one came back 404. The effect was not a worse review but
  **no verdict at all**: on go-luks2 #196 the reviewer read all 21 changed
  files, found no defect, and still declined to approve, because it could not
  confirm the change closed the issue. A change with nothing wrong with it
  could not merge.

  The issue and its comments are now read on the control plane, where the
  forge token works, and rendered into the prompt. The comments matter as much
  as the body: an owner's ruling on a question the worker raised lives in a
  comment, and a reviewer judging against the body alone contradicts decisions
  already made. Both are bounded so a long discussion cannot crowd out the
  diff, and the bound keeps the newest comments, because a ruling supersedes
  the question it answers.

  When the issue cannot be read the prompt says so, names why, and tells the
  reviewer to judge the code anyway and mark conformance unverified — a
  missing input must not become a refused review.

- **Serena could not read Go in the worker image, so every review lost symbol
  and reference lookup.** solidlsp composes its platform id as
  `<os>-<libc>-<arch>` whenever the libc is not glibc, and reads the libc from
  Python's `platform.libc_ver()` — which only ever detects glibc. On this musl
  image it returns `""`, producing `linux--x64`, which is not a member of the
  `PlatformId` enum and raises. `linux-musl-x64` is a member; nothing was
  missing but the detection.

  It broke Go even though nothing about Go asks for a platform: resolving any
  language server walks every `LanguageServerId` and *imports* every
  language-server module to compare classes, and the TypeScript module
  evaluates the platform id at import time. So starting the **Go** server
  imported the TypeScript module and died there. The reviewer then reported it
  could not complete the review and returned a verdict that decides nothing.

- **The image gate probed a handshake, not a lookup**, which is why it stayed
  green throughout. It started serena against a project root holding no code,
  so no language server was ever resolved — and resolving one was exactly
  where the failure lay. The gate now builds a Go module and resolves a symbol
  in it.

- **A routine session lookup logged up to six warnings.** The spawn server
  finds which backend owns a session by asking each one in turn until one
  answers, so every backend that does not hold it reports a miss — six of them
  on a daemon serving seven backends, for a lookup that then succeeds. At WARN
  that filled `agent.out` with `acp_backend: status session miss` for every
  healthy openvino and vllm turn, which is how a real fault becomes invisible.
  The diagnostic is unchanged and now emitted at debug; a miss is only
  interesting once every backend has missed, and the server already reports
  that as `NotFound`.

- **A re-triage now says why it is one.** An issue triage has already judged
  is judged again only because somebody spoke after that verdict — and what
  they said is the answer to it: the fact needs-info asked for, or the ruling
  an escalation waited on. The discussion was rendered flat, so a small model
  read the old verdict's "blocked on an owner ruling" over the ruling posted
  directly beneath it; measured on go-luks2#12 on the NPU (2026-09-21), which
  re-asked for a ruling the thread already carried. The rendered discussion now
  opens by naming triage's last verdict, marks every comment after it as the
  answer, and the charter says a ruling given there is settled. (Landed in
  `8f1ce591`, whose message wrongly describes it as a gofmt change.)

- **A triage reply the binary cannot read is no longer posted as a verdict.**
  An undeclared or unreadable reply, a verdict carrying no evidence, and a
  duplicate that names the issue itself or an issue the pass never listed are
  now set aside: audited as `sdlc:triage_unusable`, written nowhere on the
  forge, and judged again on the next pass. Before, each was posted as
  "triage declared a verdict this binary cannot act on" with the escalated
  label — and that comment then became triage's last word, so the issue was
  frozen until a person spoke. Measured on go-luks2 on the NPU (2026-09-21):
  one pass posted a self-referencing duplicate with a misspelt marker, two
  27-byte not-actionables with no evidence, and a duplicate of an unrelated
  issue. Those are a small model running out of rounds, not findings about
  the work.

- **The unblock sweep was never run.** It was written, documented and tested —
  a pass that re-examines every blocked task, checks the condition it parked
  on, and returns the ones whose condition has cleared through the engine's
  readmission verb — and nothing anywhere constructed it. So a task that
  blocked on a checkable condition stayed blocked however completely that
  condition cleared, and the only thing that ever returned one to the queue was
  a person. A fleet parks tasks faster than a person clears them, which is how
  every project ends up with nothing runnable: eight blocked tasks stood across
  two projects, the oldest from 09-09.

  It had no tenant-wide entry point either, which is the mechanical reason it
  could not be mounted beside the other backstops. It now has one, shaped like
  the review floor's, and runs at boot and on a ten-minute ticker — slower than
  the review floor because the latency it bounds is cheaper: a finished change
  waiting to merge costs more than a task whose blocker cleared a few minutes
  ago.

  It is wired to the engine's **service**, not its store. Only the service
  announces a readmission, and that announcement is what takes
  `factory:blocked` back off the issue — a sweep wired to the store would
  return the task to the queue while the issue still said it was blocked.
  `NewUnblockSweeper` refuses the store by name for exactly this reason.

- **The worker image's Go toolchain was unreachable to every command the
  factory ran.** The agent execs tools as `sh -lc`, and a login shell sources
  `/etc/profile`, whose first line on the base image unconditionally overwrites
  `PATH` — discarding the `ENV PATH` that put `/usr/local/go/bin` on it.
  Measured on the image: `sh -c "command -v go"` resolved it, `sh -lc` did not.
  A `/etc/profile.d` entry now restores it, asserted at build time.

  The symptom was nothing like the cause. No missing-toolchain error was
  reported anywhere: Serena's Go language server simply failed to start, the
  reviewer lost symbol and reference lookup and returned a `comment` verdict
  saying it could not complete the review, and because a comment is neither an
  approval nor a request for changes the gate stayed at
  `insufficient_approvals`. The delivery job then waited in `reviewing` for a
  verdict that could never change and the stall detector ended it thirty
  minutes later, logging `job stalled` — which points at the agent. Five jobs
  died that way.

- **The image gate probed a shell the factory never uses**, which is why it
  stayed green throughout. Its checks ran under `sh -euc`, not a login shell,
  so they asserted a `PATH` production never saw and certified a toolchain that
  no real invocation could reach. Every probe now runs `sh -lec`.

## [0.9.2-alpha] - 2026-09-16

PATCH: a dependency migration and a second inference backend. No consumer's
code has to change to take either.

THE REASON TO TAKE THIS ONE IS go-xkms v0.13.0-alpha, which carries the xKey
unlock-convergence fix that downstream projects are waiting on.

### Added

- **A `vllm` backend, registered alongside `ollama` rather than instead of it.**
  Both stay available, so moving a project between runtimes is an edit to its
  binding — and moving back is the same edit.

  vLLM is preferred for throughput because ollama does not batch: measured on
  one fleet at a flat 21 triage turns/min at concurrency 1, 4, 8 and 16 alike,
  with time-to-first-token degrading linearly to 45 s, while vLLM reached 268.7
  turns/min at concurrency 16 with a 1.3 s TTFT. Most of that gap is the
  cross-request prefix cache: with project-stable content first, vLLM gained
  4.5× at concurrency 16 against ollama's 1.06×.

  The three OpenAI-compatible agent endpoints — OVMS, ollama and vLLM — are now
  registered by one loop over their data rather than three near-identical
  blocks. They differ only in route and address.

  Unquantised weights are deliberately NOT recommended: a 4-bit 27B scored
  16/16 on the triage cases at both 6k and 20k tokens, which BF16 cannot beat,
  and BF16 costs an entire 96 GB card.

### Changed

- **go-xkms bumped to v0.13.0-alpha**, which unblocks consumers waiting on the
  xKey unlock-convergence fix.

  It is not a mechanical bump. v0.13 deleted the SDK's re-export of the storage
  layer's database surface — key-value stores, engines, watches, leases,
  indexes, time series — on the grounds that go-xkms does not ship that layer
  and must not expose it. The types are now imported from go-qrdb's own SDK
  directly, which is what this project's SDK-boundary rule said to do anyway.

  `BackendTypeThreshold` is gone and deliberately not replaced by a local
  constant: upstream removed it because the threshold backend did not do
  threshold cryptography, and re-adding the name here would restore exactly
  that misrepresentation. A configuration naming it is now refused as an
  unknown backend. `BackendTypePhone` is added, matching upstream.

## [0.9.1-alpha] - 2026-09-16

PATCH rather than MINOR: the capabilities this adds are internal seams — a
context limit, a role policy, a second backend shape — that no consumer's code
has to change to take.

WHAT THIS SPAN IS ABOUT IS THROUGHPUT. The factory now runs 30+ repositories
against a fleet of unlike accelerators, and every piece here answers the same
question: how do you keep scarce hardware busy without letting one role starve
another, one endpoint's limits surprise you, or one prompt be prefilled twice.

### Added

- **A portable context limit for served models.** Every model has a prompt
  limit and almost no server will tell you what it is: measured against one
  OpenAI-compatible server, the model endpoints return `id`, `object`,
  `created` and `owned_by`, and the metadata endpoint answers 500. Others
  expose *something* — a `max_model_len`, an `n_ctx`, a `max_input_length` —
  but they do not agree on where or what it is called, so a client that reads
  any one of them works against one server.

  `ContextLimit` establishes the limit by a ladder instead, every rung
  optional: **declared** (`AgentProfile.ContextTokens`, the only rung that can
  be right before the first request), **learned** (a refusal narrows it, which
  costs nothing until something overruns), and **unbounded** (send it and let
  the server decide). The learned bound wins when it is tighter, because the
  server is the authority on its own configuration.

  Deliberately absent: model geometry, KV-cache arithmetic and `config.json`
  parsing — layer and head counts are a transformer-family convention, not a
  universal one — and per-vendor metadata probes, which would each be a guess
  about one server wearing the costume of a general mechanism.


- **A runtime-tunable role policy for scarce accelerators.** A pool answers
  "which profile", which is only half the question: it cannot decide who goes
  FIRST when one place is worth more to one kind of work than another. On a
  fleet with one large accelerator and several small ones, that is the question
  that matters — the large card is where review belongs, and triage would
  happily fill it and starve the thing it was bought for.

  `AgentProfile.Roles` states who a profile is for and on what terms it helps
  everybody else: `prefer` names the role it is held for, `reserve` keeps slots
  empty for that role's *latency*, and `exclusive` refuses everyone else
  outright. Selection is `ProfileResolver.ResolveForRole`, and the two tests are
  separate: capacity is arithmetic about what is running, the policy is a
  statement about what the profile is FOR, and a profile with a free slot can
  still decline so the work spills to the next profile in the pool.

  **Idle capacity is shared, not reserved.** A profile preferred by review does
  not sit empty while triage queues — it yields whatever review is not asking
  for, measured as queued jobs rather than running ones. Reserving
  unconditionally would trade one starvation for another. The policy rides on
  the stored profile record, so retuning the split is an edit rather than a
  redeploy, which matters because the right split depends on a queue that
  changes hourly.

  An unreadable queue admits the work: refusing on a failed read would stop the
  fleet on a storage blip, and the concurrency cap still bounds what starts. A
  profile with no policy behaves exactly as before.

### Changed

- **The triage prompt puts project-stable content first, so a sweep prefills it
  once.** A triage turn is ~94% prefill — 5,600-7,500 tokens in, 24-44 out;
  measured on an 8B, 28.7 s of prefill against 1.9 s of decode — so the only
  optimisation that matters is not prefilling the same tokens twice, and every
  serving runtime in use (llama.cpp, ollama, vLLM, OVMS) caches a prompt PREFIX.

  The previous order defeated that completely: `Issue: #<n>` sat on the third
  line, so two prompts from the same sweep diverged after about forty tokens,
  while the genuinely stable sections — the open-issue list, the verification
  commands, the closing policy — sat at the END where a cache can never reach
  them. They now come first and the issue comes last.

  It is also the better order for the answer, which is why it is not a trade:
  the decisive evidence lands nearest the question, where attention is
  strongest.

  The open-issue list now includes the issue being triaged, because excluding
  it made the list differ per issue. The prompt says so explicitly — a list
  that silently contained the subject would invite the one duplicate verdict
  that is always wrong.

### Fixed

- **A prompt that exceeds the model's context is reported as itself.**
  `PromptTooLongError` names the size sent and the ceiling in force, unwraps to
  its own sentinel so a caller can tell it from a transient fault, and is
  raised locally — without a round trip — once the ceiling is known. It
  previously surfaced as an opaque server error wrapped in whatever the agent
  said next; on one fleet every triage turn failed that way for a day before
  anyone read the raw response body.

## [0.9.0-alpha] - 2026-09-16

MINOR rather than PATCH because this span adds a capability — agent profiles
and pools, a working `openvino` backend, and a node-level triage agent — rather
than only correcting behaviour.

THE THEME IS A LOOP THAT COMPLETES. The previous release could triage, deliver
and review, and in practice it stopped: a not-actionable verdict left an issue
unlabelled and invisible, a readmitted task resumed into a review nobody could
satisfy, and the local-model backend could not spawn a session at all. Each of
those is fixed here with the measurement that found it.

### Added

- **Agent profiles and pools.** `AgentProfile` names one combination of
  *runtime* (claude / codex / opencode / openvino), *model* and *provider
  endpoint* — three independent facts that were previously fused into a single
  backend name. `AgentPool` is an ordered preference list of profiles, and
  `ProfileResolver` picks the first with capacity, so "prefer the local model,
  spill to the metered one" is data rather than a hardcoded fallback. Load
  comes from the same live-lease count the concurrency cap enforces, so
  placement and admission cannot disagree about what "busy" means.


- **A project may name its own triage agent** (`binding.triage`), falling back
  to `worker` when unset. Triage reads and judges and writes no code, so it can
  run on a cheaper or locally hosted model without changing who writes the
  code. The binding table says plainly when it is falling back.

- **An `openvino` agent backend, and a container that can drive an Intel NPU.**
  `pkg/orchestration/openvino_backend.go` runs a real agent loop against an
  OpenVINO Model Server — completion, tool calls executed in the session's
  container, results fed back — offering exactly one tool, `run_command`.
  `deployments/inference/openvino-npu/Dockerfile` adds the Intel NPU user-mode
  driver to `openvino/model_server`, which ships the NPU plugin but no
  level-zero and therefore cannot enumerate the device at all. Measured on the
  burn-in fleet with `Qwen3-8B-int4-cw-ov`: 98% NPU utilisation with the GPU at
  0%, so reading work stops competing with reviews for the card. The agent
  daemon registers it from `--openvino-url` / `--openvino-model`; unset leaves
  it unregistered rather than pointing at a guess.

  It is a backend rather than another provider config on the ACP exec path
  because that was tried first and cannot work: OVMS answers *"Only function
  tools are supported"* to codex, which advertises its shell and patch tools as
  custom types and offers no switch to change them.

- **Triage can close an issue whose work is already on the default branch,
  when the project allows it.** `ProjectBinding.TriageMayClose` is off by
  default and the charter still tells the agent not to ask unless the project
  has enabled it — a label is reversible and carries no social weight, and
  closing somebody's issue does. Closing requires BOTH the binding's consent
  and an explicit `SDLC-TRIAGE-CLOSE: already-done` marker on the verdict, so
  enabling the feature cannot retroactively close what an earlier pass judged,
  and a not-actionable that named no reason is never closed. Only that one
  reason closes: a model inventing its own justification cannot act on it. The
  evidence comment is posted first, so a crash between the two leaves an open
  issue carrying the reasoning rather than a closed one saying nothing.

- **A node-level default triage agent**, `sdlc.triage_backend` /
  `sdlc.triage_model`. Triage re-reads every open issue on every bound project
  on every sweep and writes no code, so it is both the highest-volume job the
  factory runs and the one least in need of a frontier model. Its only fallback
  was the project's WORKER, so a fleet that wrote its code with a frontier model
  also re-read its entire backlog with one. The fallback is now three rungs,
  most specific first: the binding's own `triage`, the node's default, the
  project's worker. A rung is taken only if it names a BACKEND — a model with no
  backend is not a selection, and treating it as one spawns the wrong runtime
  carrying the right model name. Both keys ship EMPTY, which keeps the previous
  behaviour exactly for an install with no locally served model.

### Fixed

- **A triaged issue always leaves in a state somebody can find.** A
  `not_actionable` verdict commented and stopped, leaving the issue open with no
  factory label at all — and because the newest comment was then triage's own,
  the candidate rule skipped it on every later pass. The issue was invisible to
  the factory and unmarked for a person: nobody was waiting on it and nothing
  would look at it again. Measured on a burn-in project: five issues in exactly
  that state, zero issues carrying the trigger label anywhere in the fleet, and
  34 hours during which the backstop ran every 15 minutes, found no candidate
  and reported a clean pass every time.

  Not-actionable now escalates when it does not close, and `escalated` joins
  `needs-info` as a label triage re-reads once somebody replies. Both verdicts
  are REQUESTS — one for a missing fact, one for a ruling — and the answer
  arrives as a comment. A closed issue is not also escalated.

- **A re-adopted issue no longer races a rival branch against its own open pull
  request.** A delivery is keyed by task and a task is terminal, so an issue
  whose first task ended is adopted again as a new task with a fresh delivery
  that knows nothing of the pull request the old one opened. The branch name is
  planned from the issue, so it is the same name; the remote branch still
  carries the old commits; and the new attempt cut its branch from the base
  instead. Every push was then refused — `! [rejected] (non-fast-forward)` —
  and each refusal was charged to the *work*, so the task spent its whole
  attempt budget failing at something the work had nothing to do with. That
  made re-labelling an orphaned issue not merely useless but harmful. A
  delivery holding no pull request now adopts one already open on its branch,
  takes that pull request's head as its own, and goes to review instead of
  working again.

- **A spawn whose credential directory did not mount is refused instead of
  started blind.** A pooled account's files must be keyed under
  `CredentialDirSpec.Rel` — `materializeAccountFiles` writes each relative path
  verbatim and the backend mounts `<account-home>/<Rel>`. Files stored at the
  root produced no mount, yet the backend still set the CLI's `HomeEnv`,
  pointing it at a container path nothing had mounted. codex answered
  `CODEX_HOME points to "/home/agent/.codex", but that path does not exist`,
  produced no turn, and the runner reported only `(9882 bytes of stderr
  withheld)`. Every review on the burn-in fleet failed that way for hours while
  the account read `active` and the credential resolved.
- **Agent-daemon backends log again.** `ACPBackend.SetLogger` was never called
  in `agent serve`, so every backend's logger was nil and its diagnostics — the
  evidence withheld from the error string — were discarded. That is why the
  fault above took hours to see rather than minutes.


- **A task no longer abandons the pull request it opened.** A delivery is keyed
  by task, so it outlives the attempt that created it — but every attempt ran
  the worker regardless. When an attempt ended for a reason that was not the
  work's fault *after* its pull request was opened, the next attempt was handed
  a workspace with that branch checked out; the worker read the tree, found the
  change already present and reported `satisfied`, exactly as its contract
  says; and the conclusion seam completed the task as `already_satisfied` on
  the premise that "the work was already present, so there is nothing to
  merge". That premise holds for a stale issue and is false here — the pull
  request was still open, and completing the task left it with nobody to merge
  it, ever. Three of the burn-in fleet's nine open pull requests were stranded
  that way, two carrying review verdicts nobody would act on and one approved
  and gate-cleared. An attempt whose task already has an open, unmerged pull
  request now resumes it — straight to the review state, worker not re-run —
  and the forge is asked rather than the delivery record, so a pull request
  merged or closed by a person since is not reviewed again.

- **The `openvino` backend can actually run a session.** Five faults stood
  between naming the backend and a working turn, each visible only once the one
  before it was fixed:

  - The registration was deleted alongside the model-named backend it replaced,
    while `--openvino-url` and `--openvino-model` kept populating the config —
    flags accepted, backend constructed from nothing.
  - It was handed no container runtime. It is not a CLI: it drives a remote
    model over HTTP and execs its tool INSIDE a container of its own, so it
    holds the runtime directly rather than being given one at spawn. Without it
    every sandboxed spawn was refused, which on a required-policy daemon is
    every spawn.
  - `Spawn` returned a session with no `ContainerID`, so the control plane saw
    an uncontainerized session and refused it while the container ran on the
    host. Empty stays a real answer for an unsandboxed session.
  - The keep-alive lost its arguments: the runtime maps only `Entrypoint[0]`
    onto `--entrypoint`, so a bare `tail` read EOF, exited, and auto-remove took
    the container away — about a second after the spawn reported an id.
  - The spawn's environment did not travel, so the container had no agent socket
    and no pinned host key and cloned nothing.

  `HasContainerRuntime` makes the wiring provable: a backend missing its runtime
  is indistinguishable from a working one until the first spawn.

- **A served model needs a usable prompt length.** OVMS generates a default
  graph when the model directory has none, and that default takes the NPU
  plugin's 1024-token limit — smaller than the triage charter alone, so every
  turn failed before an issue was attached. `deployments/inference/openvino-npu/`
  now carries the graph, with the measured result that the key is
  `MAX_PROMPT_LEN` (unprefixed) in `plugin_config`: the field spelling is
  refused at load, and the `NPUW_LLM_`-prefixed spelling loads and changes
  nothing. 20,009 prompt tokens accepted where 1,109 previously failed.

- **A task readmitted onto a refused head gets a worker, not a second review.**
  The runner's "a round was requested" flag lives for one job. A task readmitted
  after its job ended arrived with the flag clear and a pull request whose
  CURRENT head a reviewer had already refused, so the run resumed into review —
  asking for the identical verdict on the identical tree with no worker running
  to address it. It surfaced as "there is no worker session to prompt", one
  attempt at a time, until the budget was gone, leaving the pull request open,
  refused and unattended. Reproduced by readmitting a real task: three attempts
  spent in 91 seconds.

  `Delivery.HeadRefusal` is the durable form of the flag — it reads the newest
  review BINDING to the current head, so a second reviewer's later approval
  still wins — and the question is asked AFTER the pull request is adopted,
  because a re-adopted issue learns its head and its reviews during that
  adoption.

- **The reviewer's objection survives the job that received it.** Round findings
  came from the job's memory and this run's feedback, and a readmitted task has
  neither. The worker was handed the bare issue contract, read a branch that
  already held the work, correctly answered that the implementation was already
  there, and was charged a failure for producing no changes — while the
  reviewer's objection reached nobody. The delivery is now the third and durable
  source, and the round is numbered from the refusal.

### Removed

- **The `qwen` backend.** It was the codex runtime driving a qwen model at an
  ollama endpoint, registered under the model's name with the endpoint baked
  into its argv — so one codex registration could reach exactly one provider
  and a second provider needed a second "backend". It is now expressible as
  profiles over the runtimes that already exist. `QwenProviderName` and
  `QwenProviderConfigArgs` are superseded by `AgentProfile.SpawnArgs`.

## [0.8.0-alpha] - 2026-09-14

MINOR rather than PATCH because this span adds a capability rather than
repairing one: the **Software Factory** — an issue-driven delivery loop that
adopts a labelled forge issue as a task, runs a sandboxed agent against a real
checkout, opens a pull request, waits for CI, obtains an independent peer
review and merges on a cleared gate. Pre-1.0 that is also where breakage
lives (release-versioning.md §1), and this span carries some: `pkg/sdlc` and
the orchestration seams changed shape, `AgentRoleSpec` lost its ad-hoc system
prompt, and the SDK's `TaskArtifact` was corrected to the artifact the server
actually sends — a field-level break for any caller that read `Ref` as a
string.

Proven end to end on the burn-in fleet the day this was cut: a worker took an
issue to a pull request, CI went green, an independent reviewer approved it
with a substantive read of the diff, the gate cleared on one independent
approval, and the change merged thirteen seconds later — with the
changes-requested path exercised too, a worker re-engaging on a rejected head
and the reviewer judging the new one.

### Added

- **`Project.DefaultWorkflow`** — the tenant-scoped workflow a Task under the
  project auto-spawns into (empty = never). A set-but-unknown name is refused
  on create and update by consulting the same registry the orchestrator
  serves from.
- **`Project.Git.Author`, `Project.Git.Signing`, `Project.Git.SSHKey`** — the
  commit identity, signing policy and access key of a project's git
  workspace, on the workspace record where they belong. `Signing` is the one
  `orchestration.CommitSigning` type (the `service.SigningPolicy` it replaces
  is gone); `GitProvisioner.CommitForProject` records a commit under the
  project's identity and policy, and `ProjectGit.AccessKey()` falls back to
  the signing key when no separate SSH key is named. All three ride every
  transport (REST, gRPC, MCP, embedded, and the Unix/QUIC delegations), the Go
  and TypeScript SDKs, and `quicbaas project create|update`.

- **`TaskSource` + poller** — `RegisterTaskSource(kind, source)`; the server
  sweeps every registered source against every project with a
  `DefaultWorkflow` on `orchestration.sweep_interval` and creates
  auto-spawning tasks. Re-polling is idempotent through the task store's
  guarded source index; a `TaskSourceUnavailableError` is reported once per
  outage.
- **`JobLifecycleHooks`** — five moments (`Prepare`, `Reviewable`,
  `Complete`, `Discard`, `Blocked`), registered with `RegisterJobHooks` and
  selected per workflow by `WorkflowSettings.Hooks`. `Prepare` returns the
  directory every session is spawned into; `Discard` runs on a detached
  context bounded by `orchestration.stage_timeouts.cleanup`. A hook error
  fails the job with `failure_class: hook`. `JobStatus.stage` and a `stage`
  `JobEvent` report each moment on every transport (REST, gRPC, MCP,
  embedded, Unix/QUIC, Go and TypeScript SDKs, CLI).
- **`ReviewCriterion.phase`** — `validating` (default) or `reviewing`;
  reviewing-phase criteria, when present, are the review.
- **Bound-task mode** — a job started as an attempt at a Task
  (`JobConfig.TaskID`) binds its work items to that Task, re-delegates
  straight back to `executing` on a request for changes, and escalates
  through the `Blocked` hook when `max_iterations` is reached.
- **`orchestration.stage_timeouts`** (`prepare`, `reviewable`, `complete`,
  `cleanup`, `blocked`) and **`orchestration.sweep_interval`** configuration.
- **`worker_spawn.go`** — the spawn environment every session the control
  loop spawns (worker, lead, reviewer) passes through: `acceptEdits` grant,
  `orchestration.agent_config_dir` as `CLAUDE_CONFIG_DIR`, and `AccountAuto`
  on pooled-account backends, so a spawn never runs on the daemon host's
  ambient identity (#577).

- **`POST /api/v1/tenants/{tenant_id}/roles/reconcile`** — rewrites the
  tenant's built-in roles from the running build's definitions: go-qrdb
  reconciles `BuiltInRoles()` **plus `OwnerRole()`**, so `owner` is
  re-permissioned with the other six. Wired on
  REST, gRPC, QUIC, MCP, Unix, the embedded transport, the Go SDK, the
  TypeScript SDK, the CLI (`quicbaas iam role reconcile`) and the UI; no
  transport answers "not supported". It maps to each transport's own
  role-create permission — `tenants:create` on REST (and therefore QUIC),
  `roles:create` on gRPC (and therefore Unix) and MCP — rather than minting a
  `roles:reconcile` verb, which would be absent from every role seeded before
  it existed, leaving nobody holding the grant needed to repair exactly that
  absence.
- **A startup sweep.** `Server.reconcileTenantBuiltInRoles` runs at the tail
  of IAM initialization, after module permission registration (built-in roles
  resolve through the process-global permission registry at CALL time), over
  every tenant plus the system tenant. One log line per tenant per CHANGED
  role; a converged tenant logs nothing and writes nothing. Non-fatal.
- **`BuiltInRoleReconciler`**, deliberately NOT part of `RoleStorage`: the
  built-in definitions are go-qrdb's, so only a qrdb-backed storage can
  honour the contract and any other is refused with
  `ErrRoleReconcileUnsupported` rather than reconciled against a set this
  repo invented.
- **`retired_permissions` is surfaced** on role list and get — it was being
  dropped in `transportRoleToIAM` — through both SDKs, `quicbaas iam role
  get`, and the UI roles table as warning badges beside a Reconcile Roles
  action.

- **`JobRunner` — the seam that lets a module own a Job's state sequence
  while the engine keeps the envelope.** `pkg/orchestration/job_runner.go`
  declares `JobRunner{Name, States, Validate, Run}`, the optional
  `JobResumer{ResumeState}`, `JobStateSpec{State, Label, SessionBacked,
  IdleThreshold}` and a registry (`RegisterJobRunner` / `LookupJobRunner` /
  `JobRunners` / `KnownJobStates`) with typed refusals
  (`JobRunnerRegistrationError`, `JobRunnerAlreadyRegisteredError`,
  `UnknownJobRunnerError`). `ControlLoop` — a lead agent decomposing a
  description into a DAG of work items — is registered as the `workflow`
  runner; it is one shape of job, not the shape of every job. A runner
  declares its states so the stall detector's thresholds and the UI's
  stepper can read them instead of keeping their own copy.
- **`Runner` on `JobConfig`, `JobStatus` and `StartJobRequest`** across
  proto, REST, gRPC, MCP, the embedded transport, the Go SDK client, the
  TypeScript SDK and `quicbaas orchestrator run --runner`. Empty means the
  `workflow` runner, so every existing caller is unchanged.

- **`trunk-based`** — github-flow plus the direct-push carve-out the handbook
  permits (`software/conventions/sdlc.md` §2) for documentation and CI
  configuration. Carved-out changes still work on their own signed branch;
  what the carve-out removes is the pull request and its review round.
  `NewTrunkBased(labels...)` builds one with a different carve-out.
- **`gitflow`** — `feature/<n>-<slug>` off `develop` landing there as a merge
  commit; a `hotfix`-labelled issue cuts `hotfix/<n>-<slug>` from the release
  line and back-merges it into `develop` through `PostMerge`. Release
  branches (cutting, tagging, the double back-merge) are out of scope and
  filed as #597 rather than registered as half a strategy.

- **`Runner` on the Task.** `Task.Runner` records the `JobRunner` that drives
  an attempt at the task, resolved when the task is created and never empty
  afterwards, so a task requeued after a failed attempt is re-dispatched by
  the runner that ran it. It is set from `CreateTaskRequest.Runner` and
  `TaskSourceSpec.Runner` — a module supplying work its own runner drives
  says so on the spec — and carried across proto, REST, gRPC, MCP, the
  embedded transport, the Go SDK client and the TypeScript SDK. Empty
  resolves to the engine's own `workflow` runner, so every existing caller
  is unchanged; a name nothing registered is refused with
  `UnknownJobRunnerError` before a task exists.
- **`Task.Routing{Backend, HostAffinity, GpuRequired}`** — the placement
  hints a dispatch reads, now typed and persisted on the record rather than
  only living on the request that created it, so a requeue dispatches under
  the same constraints as the first attempt. `CreateTaskRequest` gains
  `host_affinity` on the proto, gRPC, MCP and Go SDK surfaces, which
  previously carried only `backend` and `gpu_required`.
- **`Task.DispatchReason`** — why the last dispatch produced no job, written
  to the record through the store's compare-and-swap when a spawn is
  refused and cleared when one succeeds. A pending task now says what it is
  waiting for instead of the reason living only in an error the caller
  discarded.
- **Auto-spawn under a module's runner needs no fleet agent.** A task whose
  runner is a module's is dispatched with no agent registered, no backend
  and no project workflow: the runner spawns its own sessions and the job's
  lease is the capacity check. The lead-driven `workflow` runner is
  unchanged — it still needs a matching online agent (backend → gpu →
  host_affinity) and the project's default workflow, and still refuses
  without either.

- **`Project.Autonomous` — the one switch that turns autonomous development
  on.** It gates the task-source poller and the new task dispatcher; the
  SDLC module's `Bind` sets it. `quicbaas project create|update
  --autonomous` and the field are on every transport and both SDKs.
- **`Project.ContextRepos` — the repositories the worker clones beside the
  project.** `ContextRepo{Name, RepoURL, Ref}`, validated by
  `ValidateContextRepos` (one path segment, no `.`/`..`, no separator or
  NUL, no two repositories claiming one directory, none occupying the
  project's own directory), set with `quicbaas project create|update
  --context-repo name=handbook,repo_url=…,ref=main`. The daemon puts the
  list in the workspace spec it hands the worker's container; the container
  clones each entry beside the project checkout, so `../handbook/...`
  resolves for the worker exactly as it does on a host.
- **The task dispatcher** (`pkg/orchestration/task_dispatcher.go`): per
  autonomous project it lists pending Tasks, counts live job leases, and
  starts `MaxConcurrency − inflight` of the eligible ones through
  `TaskSpawner`, ordered by `Priority` descending then `CreatedAt` then ID.
  A Task it declines carries the answer on the record — **`Task.DispatchReason`**
  ("project at capacity", "deferred until …", "attempt budget exhausted",
  "spawn refused: …"), written only when it changed and cleared on a
  successful dispatch. `Server.NudgeProject` sweeps the poller first, then
  the dispatcher, so a verified webhook delivery turns into a running job in
  one call.
- **The poller's adoption cap**: `TaskSourcePoller.sweepProject` adopts at
  most `MaxConcurrency − inflight − pendingUndispatched` specs, so a
  project's queue stops filling with work nothing will start for hours. The
  job lease store remains the actual boundary.
- `docs/agent-orchestration/autonomy-and-dispatch.md` documents the switch,
  the dispatcher, the workspace spec and the container entrypoint contract.

- **`GET /api/v1/orchestrator/runners`** — every registered `JobRunner` and
  the ordered, non-terminal states it declares, with each state's label and
  whether an agent session backs it. The runner registry landed without an
  endpoint; this is it. Registry data, not tenant data, so no tenant is
  derived — but still gated on the orchestrator service being present,
  because a node that runs no orchestrator drives no jobs, and all three
  transports gate on the same condition. It reads `orchestrator:status`, the
  permission reading the job already requires, rather than minting a new
  action in a resource family go-qrdb owns. Reachable on REST, the embedded
  transport, MCP (`quicbaas_orchestrator_list_runners`), the Go SDK, the
  TypeScript SDK and the Wails desktop binding. **gRPC, QUIC and Unix are not
  wired** — they need a new RPC and regenerated stubs; tracked as issue #606.
- **`GET /api/v1/sdlc/forges`** — which forge adapters this build carries, so
  a caller can offer an SCM selection that is true of the node it is talking
  to. REST and the TypeScript REST client only; the Go SDK, the embedded
  transport, MCP, the Wails binding and the CLI do not carry it (also #606).
- **The Projects dialogs edit the whole record.** Create and edit share one
  field component so they cannot drift apart, and now carry the git commit
  author, the signing policy (switch, algorithm, KMS key name/scope/owner),
  the ssh access-key reference, `default_workflow`, and an **Autonomous
  development** section: the `autonomous` switch, `quotas.max_concurrency`,
  and `context_repos[]` rows. The field mapping and the presence-based update
  diff are a unit-testable module rather than DOM logic, and validation
  mirrors the server's own fail-closed rules so a refusal is shown before the
  round trip.
- **The SDLC binding is edited in place**, in the project's own edit dialog —
  SCM, strategy, trigger label, worker backend/model, verification commands,
  review policy and max review rounds. The dialog reads the module's three
  answers apart the way the handler writes them: `503` = the module is not
  running on this node (section absent, no capability probe made), `404` =
  running and unbound, `200` = bound. An SCM whose adapter this build does
  not carry is shown **disabled with the reason**, never hidden.

- **The `sdlc` JobRunner** (`pkg/sdlc/runner.go`) — the SDLC module now owns
  the SHAPE of an issue-driven job, over the engine's envelope. States:
  `initializing → executing → validating → reviewing → (re-delegating →
  executing | completing) → cleanup → completed`, with `escalating → failed`.
  There is **no `decomposing`, `scheduling` or `assigning` state**: an issue
  is not a DAG, so nothing decomposes it, nothing assigns it to a fleet, and
  the job spawns **exactly one** persistent worker session — prompted with
  `Task.Description`, the issue contract, **verbatim**. The loop this replaces
  handed the worker an LLM's paraphrase framed "Execute the following task".
- The runner implements `orchestration.JobResumer`: a job interrupted in
  `completing` resumes there, because `Complete` is bound to the reviewed head
  and idempotent; everything earlier restarts at `initializing`, where
  `Prepare` continues the branch the attempt already pushed.
- **`sdlc.worker.image`** on the project binding (all transports, both SDKs,
  the CLI): the container image the worker session runs in. Naming one
  requires the sandbox — the image's entrypoint is what clones the project and
  its `context_repos` from the workspace spec the runner mounts.
- **`sdlc.checkout_root`** (default `/workspace`): the path INSIDE the
  container the workspace spec names, for a worker image that lays its
  filesystem out differently.
- `orchestration.MaterializeWorkspaceSpecIn` writes the spec directory under a
  caller-named parent. The mount policy is deny-by-default and the OS
  temporary directory is admitted by nothing, so the runner writes the spec
  beside the attempt's checkout, under the operator's projects root.
- `orchestration.NewJobRun`, `JobRun.SetWorkDir`, `JobRun.SetIteration` and
  `JobRun.NextIteration`: the minimum a runner living in another package needs
  to be built, positioned and driven without booting a daemon.

- **A Task can be deleted, on every transport.** `DeleteTask` existed only on
  the store (`pkg/orchestration/task_store.go`), reachable from no service, no
  route, no SDK and no CLI, so `DELETE /api/v1/orchestrator/tasks/{id}`
  answered `405 method not allowed`. The consequence was not cosmetic: a task
  holds its source's reservation, so 26 dead tasks left over from a live run
  kept `jeremyhahn/go-truststrap#25` permanently unadoptable — every poller
  sweep answered `ErrTaskSourceAlreadyAdopted` and no re-run was possible.

  `AgentService.DeleteTask(ctx, tenant, task)` now wraps the store verb with
  the one guard the store cannot make: a task a job holds a **live** job lease
  on is refused with a typed `TaskLeasedError` naming the job and node
  (409 over REST/QUIC, `FailedPrecondition` over gRPC/Unix). A released or
  stale lease is no obstacle. The store's existing cascade does the rest —
  record, claim mark, artifact payloads and the **source reservation**, so the
  issue the task was adopted from can be adopted again.

  Reachable from REST, QUIC, gRPC, Unix, MCP
  (`quicbaas_orchestrator_task_delete`), the embedded transport, the Go SDK
  (`Orchestrator().DeleteTask`), the TypeScript SDK
  (`orchestrator.deleteTask`) and the CLI
  (`quicbaas orchestrator task delete <taskID>`).

  **Job bookkeeping is not rewritten.** A job record's `task_id` states what
  the job was an attempt at, which deleting the task does not change; erasing
  it would falsify the account of work that ran, and nothing dispatches from a
  concluded job's `task_id`. The live-lease guard is what keeps a *running*
  job's task from being deleted out from under it.

  Authorized as `orchestrator:task_create` — the task-record write verb, of
  which a delete is the inverse. go-qrdb owns the `orchestrator:*` vocabulary
  and carries no separate delete action for the family; inventing one here
  would fork the single-PDP vocabulary.

- **A Software Factory worker image**, `deployments/agents/software-factory-worker/`,
  built by `make build-worker-image` as
  `quicbaas-software-factory-worker:<VERSION>` — the value a project binding's
  `worker.image` names. It carries the claude-code agent, git, ssh-keygen (git's
  `gpg.format=ssh` signing invokes it), make, a C toolchain and the Go toolchain
  the projects pin, and no secret of any kind. Its `/etc/passwd` carries the uid
  the ACP spawn runs it as, because musl resolves users from that file alone and
  ssh-keygen refuses to start without an entry — so an image without one could
  not sign a single commit. `make test-worker-image` builds it and proves it can
  produce and verify a signed commit as that uid.

- **A containerized CLI session — `spawnContainerCLI`.** A CLI-mode backend can
  now honour `required` instead of only refusing it. It speaks exactly the
  protocol `spawnCLI` speaks — Claude Code's
  `--print --input-format stream-json --output-format stream-json` — to the
  agent CLI running as the **command of a container** rather than as a child of
  the daemon. The argument vector comes from the same `cliArgs` and the opening
  exchange from the same `openStreamJSON`, so a session cannot behave
  differently because it was sandboxed. The provider credential travels as
  container ENV through the runtime's private 0600 `--env-file` (never a
  mounted file, never argv); the workspace is bind-mounted at `/workspace` from
  a path the mount policy authorized; the network is the daemon's configured
  `--sandbox-network`; the container runs as the daemon's uid; and the session
  carries the `ContainerID` the sdlc runner needs to run the checkout,
  verification and publish inside the sandbox. Persistent and one-shot requests
  both containerize — sandboxing is a property of the policy, not of the
  caller's session-persistence preference — and a sandboxed CLI session resumes
  into a fresh container.

  Both sandboxed paths now build their spec through one `sandboxSpec`, so a
  change to the isolation cannot reach one protocol and miss the other. A
  containerized agent that stops answering is reported as
  `ContainerStreamClosedError`, which names the container and image and
  deliberately carries no exit code: the `docker run` process belongs to the
  runtime, and a fabricated `0` would read as success.

  Documented in `docs/agent-orchestration/sandbox.md`.

- `forge.ssh_host_key` travels on every surface the binding's other forge
  fields do: REST, gRPC (`ForgeConfig.ssh_host_key`, field 9), MCP, the Go SDK,
  the TypeScript SDK, `quicbaas sdlc binding` and the Projects binding dialog.
- [docs/sdlc/binding.md](../docs/sdlc/binding.md) — the binding's `forge:`
  fields and the ssh checkout contract in full.

- **A consumption ledger is now the single authority for what a pooled
  provider account has spent, and two credential-release paths refuse an
  account that is over its cap.** Before this the orchestration cost tracker
  REPORTED a token budget (ok / warning at 80% / exceeded) and nothing acted on
  the report: a live paid credential was materialized to a worker with no spend
  cap at all, so a runaway or compromised worker could spend without limit and
  the first evidence was the invoice.

  `pkg/orchestration/account_ledger.go` is an append-only record written under
  the store's guarded create, keyed by a turn identity minted when the turn is
  PLANNED. It is not a counter, deliberately: the counters are
  read-modify-write, so two hosts recording turns for one account both read the
  same total, both write their own sum, and one turn's tokens vanish. That is
  tolerable for an advisory budget and is not tolerable for the number a limit
  decision is made against.

  Every entry is written to two keyspaces over one record — an attribution
  plane keyed by `tenant/project/task/job/session/round`, so a roll-up at any
  level is a prefix scan, and a quota plane keyed by account and hour bucket,
  so a window read costs what the WINDOW is worth rather than what the
  account's history is worth. The quota plane is written first, so a partial
  write leaves spend visible to the LIMIT and missing from the INVOICE and
  never the reverse: over-refusing is recoverable, silent overspend is not.

- **`AccountSpendPolicy` on the account record: sliding quota windows and a
  circuit breaker.** An allowance belongs to the thing that is billed, and a
  provider meters a login, which is what an `Account` is. Windows are sliding
  (`[now-Size, now)`) because that is how the plans this models actually meter;
  a tumbling window would hand back a full allowance the instant a boundary
  passed, which is the burst the provider is metering against.

  The breaker is DERIVED from the ledger's failure entries rather than kept, so
  there is no second record for two hosts to contest. "Tripped" means exactly:
  at least `FailureThreshold` failures in the window AND no success since the
  most recent failure. The one thing that cannot be derived — which host gets
  the half-open trial — is a guarded create released by a guarded delete.

  The policy is validated at the WRITE. By the time the gate reads a malformed
  one, the choice is between refusing an account the operator believes is
  capped and admitting one they believe is metered, and both are wrong.

- **`SpendAuthority` is asked by both release paths**: `accountResolver.Resolve`
  for an in-process spawn and `AccountService.MaterializeAccountCredential` for
  a remote agent daemon. Both ask BEFORE the credential envelope is resolved,
  so a refused release never renews an `oauth_refresh` account — a provider
  round trip and a rotation of live secret material — on behalf of work that is
  about to be refused anyway.

  It fails closed and never with a number: a path that cannot read the ledger
  returns an error and no admission, because a gate that admitted when it could
  not see would open exactly when the store is in trouble. A read failure is
  never reported as a refusal either — an operator told "the cap is spent"
  would raise a limit that was never reached.

- **Two new environment fault classes** — `account_quota_exhausted` and
  `account_breaker_tripped` — so a refusal is charged to nobody's attempt
  budget and waits exactly as long as the condition lasts. Neither parks on
  first sight: a window frees up and a breaker retries, so putting a human in
  front of either would be putting them in front of a condition that resolves
  itself. `Task.DispatchReason` now carries the engine's own explanation for
  any refusal it can classify rather than the error's sentence, and the forge
  comment renders a `**Cause:**` line from `FaultClassMeaning`, so a reader
  sees "the account's configured spend cap for the window is spent" instead of
  the generic "the task waits on an external condition".

- **`GET /api/v1/orchestrator/sessions/{id}/history` — a session's
  conversation, paged.** The events endpoint serves the raw log (every
  fragment, thought, presence change and file operation) in the shape a live
  attacher needs. This serves what an operator opened the page to read: who
  said what, which tools ran, and what each answer cost, with a streaming
  answer folded into ONE assistant turn and every page boundary falling between
  turns. It is a projection of the same event log, not a second store.

  Wired on every transport as `GetSessionConversation` — REST, gRPC (a unary
  `OrchestratorService` RPC rather than a stream read to its end, so a caller
  can resume from a cursor), QUIC (via REST), Unix (via gRPC), MCP
  (`quicbaas_orchestrator_session_conversation`), the in-process embedded
  transport, the Go SDK and the TypeScript SDK. Permission `orchestrator:status`,
  the existing family.

  It is named for what it returns rather than for the route it is reached on:
  the SDK already has a `GetSessionHistory` that pages the raw EVENT log, and
  two verbs called history returning two different shapes would be worse than
  one verb whose name does not match its URL.

- **Token usage on the transcript.** A turn's cost was recorded only in the
  cost ledger, keyed by turn and account, so answering "what did this reply
  cost" meant joining two stores. A non-streaming turn now carries it on the
  aggregate assistant chunk and a streaming turn on the `turn_completed` event
  that closes it — the first moment the count exists.

- **A documented cap on persisted turn text, and a retention bound per
  session.** Text fields are clipped to `MaxSessionEventTextBytes` (64 KiB) on
  a rune boundary, with the dropped byte count recorded on the payload and a
  visible marker in the stored text; tool arguments over
  `MaxSessionEventArgsBytes` (16 KiB) are replaced with a marker object,
  because structured input cannot be clipped mid-value and stay parseable.
  Retention keeps the last `DefaultSessionRetentionTurns` (200) turns per
  session, configurable deployment-wide and per tenant via
  `orchestration.session_history`; a tenant set to zero keeps everything.
  Retention prunes WHOLE TURNS — half a turn would show an answer with no
  question above it — and deleting a session now deletes its transcript with
  it.

- **A transcript that cannot be fetched says so.** The web UI's history fetch
  swallowed every error, so an unreachable conversation and an empty one
  rendered as the same blank timeline. That is why this fault survived as long
  as it did; the session page now distinguishes the two.

- **`GET /api/v1/orchestrator/cost` — one scope of the consumption ledger,
  rolled up.** The ledger has recorded every worker turn since #785, and
  nothing could read it: `/orchestrator/cost`, `/orchestrator/usage` and
  `/projects/{id}/cost` were all unregistered, so the tokens an account was
  actually charged reached no operator on any transport.

  `orchestration.ReadSpendReport` is a projection over
  `ConsumptionLedger.Rollup` plus the account's own `AccountSpendPolicy` — the
  same two records the credential-release gate judges, so a dashboard and a
  refusal cannot disagree about one account. It returns the scope's tokens in
  and out, the turn outcomes, the drill-down to the level below, the accounts
  that paid with their live quota windows and breaker verdict, and the
  engine/model breakdown.

  ONE VERB FOR EVERY LEVEL, because the ledger's attribution chain is a PATH —
  `tenant/project/task/job/session/round` — and a project's spend and a job's
  spend are the same prefix scan at two depths. Four routes over one scan would
  be four places for a drill-down's arithmetic to stop agreeing with itself.

  Wired on every transport as `GetSpendReport`: REST, gRPC (a unary
  `OrchestratorService` RPC, projected field by field), QUIC (via REST), Unix
  (via gRPC), MCP (`quicbaas_orchestrator_cost`), the in-process embedded
  transport, the Go SDK, the TypeScript SDK and the Wails desktop binding.
  Permission `orchestrator:status` — the existing orchestrator read family, not
  a new one.

- **A scope may address the unattributed subtree.** A job that belongs to no
  task is filed under the ledger's no-value sentinel, and a roll-up has always
  reported it as its own row. It could not be OPENED: the sentinel was refused
  as a scope value along with entity ids. It is now accepted for a READ, which
  is unambiguous precisely because the write side still refuses it as an entity
  id — no real project, task, job or session can bear it. Exported as
  `orchestration.SpendUnattributed`, `orchestrator.SpendUnattributed` (Go SDK)
  and `SPEND_UNATTRIBUTED` (TypeScript SDK). Without it a reportable subtree
  was not addressable, and a job with no task had spend no scope could name.

- **The job record now says who ran the attempt, which attempt it was, and
  what it left behind.** `JobStatus` carries a provenance block
  (`pkg/orchestration/job_provenance.go`), on the existing job shapes across
  REST, gRPC and MCP — no new routes:

  - `agent_id` — the **fleet agent** the work was dispatched to, captured at
    spawn from the resolved backend's registration and recorded by
    `JobRun.Spawn`. It is not `node`: that is the daemon whose control loop
    drove the job, and the two differ whenever the work runs anywhere but the
    control plane, which is the normal case.
  - `stages[]` — every lifecycle moment entered, with the instant it was
    entered at. The stage events already said this as prose in an audit line;
    a duration is arithmetic over this list. A repeated moment is recorded
    again, because a job that reviewed, sent work back and reviewed again
    entered `reviewable` twice.
  - `verification[]` — the last validator pipeline's results. Runners now
    verify through `JobRun.Verify`, which runs the pipeline **and** records
    what it said, rather than each runner calling `RunPipeline` and keeping
    the answer to itself.
  - `attempts[]`, `attempt_budget`, `terminal_reason`, `readmit_allowed`,
    `delivery` — the TASK's facts, laid over a copy of the status when it is
    read. Derived rather than persisted: a copy taken when the job last wrote
    its status would be stale for every attempt after it.

  `delivery` is read from the Task **artifacts** a runner published, under
  names `pkg/orchestration` owns (`pull_request`, `head`, `reviews`), so the
  engine never imports the module that produced them and a generic job page
  works for a runner it has never heard of. `pkg/sdlc` now publishes its
  review rounds alongside the pull request and head it already published.

- **`Task.ConclusionReason` is persisted.** `applyConclusion` decided why a
  task stopped and told the observers (#790); the reason is now on the record
  too, and cleared by a requeue. A reader hours later has no conclusion event
  to consult, and re-deriving the reason from the resting status cannot tell
  an `abandoned` attempt from an operator's hold — the two differ only in what
  produced them.

- **`docs/design/prompt-sets.md`** — the approved design for making the
  Software Factory's prompts editable, versioned, per-tenant and per-project
  data instead of Go constants, cross-linked from
  `docs/design/orchestration-remaining-work.md`. It is deleted when the
  feature ships and `docs/software-factory/prompt-sets.md` becomes its
  as-built description.

- **QuicBaaS can review its own pull requests.** The merge gate
  (`pkg/sdlc/gate.go`) could already read a verdict and decide on it; it could
  not write one. `ForgeBackend` gains `SubmitReview`, `SetCommitStatus` and
  `ListPullRequestFiles`, all implemented for Gitea and declared through
  `Capabilities()` (`SubmitReviews`, `CommitStatuses`) so call sites branch on
  what a platform can do rather than on which platform it is. The signatures
  carry the module's own vocabulary — `ReviewVerdict`, `CheckStatus` — and
  never a forge's wire strings, so a GitHub or GitLab adapter writes one
  mapping table and nothing above it changes.

  A `review` job runner is registered beside `workflow` and `sdlc`, dispatched
  from the forge webhook on pull-request events. It selects the reviewer from
  **project configuration** (`ProjectBinding.Reviewer`) and never by parsing
  labels out of a payload; spawns one containerised reviewer session with a
  checkout of the head and **no signing agent**; reads the verdict from a
  structured declaration rather than from prose; posts it bound to the head it
  judged; writes the commit status that makes it enforceable by branch
  protection; and records `EvaluateReviewGate`'s answer rather than deciding
  the merge a second time. The reviewer acts through its **own** forge token,
  so a change cannot approve itself by configuration accident.

  Nine production failures of the shell automation this replaces are now Go
  tests that fail without their fix: a draft may not be approved; a COMMENT
  must not dedup a head for ever; an unlabelled pull request must not crash
  the reviewer; a relabel must not overwrite a standing rejection; the
  changed-file list must paginate; an empty verdict is not a verdict; a
  rejection must make the gate red on the run that files it; a rejection is
  not order-dependent; a stale verdict must not gate a new head.

- **Binding a project now onboards its repository, and a conformance verb says
  when it has drifted.** QuicBaaS is the sole orchestrator: on 2026-09-12 every
  orchestration workflow was deleted from eight repositories, and the
  architectural requirement behind that is absolute — nothing the factory needs
  may be a file committed to the repository it drives. The system this replaces
  failed exactly there, shipping a 75KB review engine copied into every repo, so
  fixes reached one repository and silently missed eighteen.

  Three holes stood between "a repository exists" and "QuicBaaS is driving it".
  `ForgeBackend.EnsureWebhook` was implemented, read-back-verified, and **called
  by nothing** — webhooks were configured by hand. Labels were hand-created per
  repository, and a missing one is a *silent* orchestration failure: the label
  write is refused, the ending never displays, and nobody is told the repository
  was never set up. And nothing answered "is this project correctly onboarded?".

  `pkg/sdlc/onboarding.go` closes all three. `Bind` now provisions the
  repository's webhook and the label vocabulary the binding declares — from the
  binding record, writing to the forge's own configuration and committing
  nothing — and `Unbind` removes the delivery. Provisioning is idempotent, so
  onboarding twenty-eight repositories is a loop nobody has to keep notes about,
  and re-running it repairs a webhook somebody deleted by hand. Labels are
  **never** renamed, recoloured or removed: they carry the history of every pull
  request that ever wore one.

  Two new verbs carry it across every transport. `Onboard` provisions and is the
  explicit repair; `Conformance` reports and changes nothing, naming each failure
  distinctly with its subject and its fix — `webhook_missing`,
  `webhook_inactive`, `webhook_misdirected`, `webhook_events_missing`,
  `label_missing`, `stale_workflow`, `protection_missing`,
  `protection_context_missing`, plus `check_unsupported` for a platform that
  cannot answer and `check_failed` for a read that did not. A pass whose reads
  failed is non-conformant, because reporting an outage as a clean bill of health
  is how a fleet dashboard lies. `quicbaas sdlc conformance <project>` exits
  non-zero when the project is not conformant, so it can gate a script — which
  is the point: the company already owned `conformance.sh`,
  `review-workflow-drift.sh` and `check-workflow-outputs.py`, three well-built
  instruments wired to nothing, and a check nobody runs is indistinguishable from
  a check that does not exist.

  `ForgeBackend` gains a `ForgeOnboarding` half — `Labels`, `EnsureLabels`,
  `Webhooks`, `DeleteWebhook`, `RepoFiles`, `BranchProtection` — stated in terms
  every target platform has, with `ForgeCapabilities.BranchProtectionContexts`
  carrying the one real difference. Which paths count as stale orchestration is
  QuicBaaS's policy on the binding, not a concept an adapter has to invent.

- **Every hand-over of a pooled provider credential takes a delivery lease,
  and the session reaper withdraws it.** The subscription OAuth credential has to
  exist inside the agent container — the CLI dials the provider itself and a
  request proxy was ruled out — but it no longer has to be DURABLE. Each
  hand-over now takes a grant scoped to `(tenant, project, job, account)`, with a
  one-hour default TTL (`orchestration.account_delivery_ttl`), audited on
  issuance, and revoked when the session that asked for it ends.

  It composes the account pool rather than replacing any of it: selection,
  status, cooldown, the usage-limit classifier, the sliding-window quota, the
  breaker, the OAuth refresher and rotation are untouched. It composes
  `pkg/lease.LeaseManager` rather than leasing separately, so a delivery grant
  sits in the same keyspace as every other lease, is listed by the same
  `ListLeases` and revoked by the same `RevokeLease`.

  **The lease governs delivery, not the credential's lifetime.**
  `AccountCredential.ExpiresAt` and the RFC 6749 refresh grant remain
  authoritative for how long the token works. A lapsing grant does not shorten a
  token the refresher would have kept alive; a refreshed token does not extend a
  grant. What a revoked grant stops is the NEXT delivery. Grants are therefore
  renewable with no renewal cap — the bound is the session's conclusion, and
  capping renewals would cut a long job off at an arbitrary multiple of the TTL.

- **`MaterializeAccountCredential` is the enforcement point, and the daemon
  presents its grant so there is something to enforce on.** The verb now takes
  `job_id` and `lease_id`. An absent lease is a first delivery and is governed by
  the checks that already existed; a presented lease is a REFRESH of a copy the
  caller holds, and a revoked, expired or wrongly-scoped one is refused from the
  lease store whatever the caller believes. `pkg/agent`'s read-through cache
  records the grant each copy was released under and presents it on every
  refresh — without that, every refresh would look like a first delivery and
  revocation would be unenforceable.

  A daemon leaves `job_id` EMPTY, deliberately: its credential cache is keyed by
  account, so one release serves every job on that host and a grant naming one of
  them would be false — and a later refresh validated against it would then be
  refused its own copy. An in-process resolution names its job and gets a
  job-scoped grant.

  The limit is stated deliberately: revoking one delivery stops the host holding
  that copy from replacing it. It does not disable the account — that is
  `AccountStatusRevoked`'s job.

- **The daemon reports the grant back, which is what makes a REMOTE session's
  credential revocable at all.** The control plane issues the grant when it
  releases the credential, but the release happens inside the daemon's account
  resolution, so the control plane cannot tell which session the grant belongs to
  — only the daemon can. `AccountCredentialLeaser` is therefore split: the
  one-verb `AccountDeliveryGrantor` is what a resolution needs, and it has two
  honest implementations — the control plane's, which CREATES the grant in the
  lease store, and `pkg/agent`'s, which REPORTS the one the control plane already
  issued. A daemon holds no lease store, so requiring it to implement `Validate`
  and `Revoke` would have forced a stub.

  The grant now travels `ResolvedAccountCredential.LeaseID` ->
  `Session.CredentialLeaseID` -> `SpawnSessionResponse.credential_lease_id` ->
  `SessionRecord.CredentialLeaseIDs`. Before this the remote grant stopped at the
  daemon's cache: no session record named it, so no conclusion withdrew it, and a
  remote session's provider credential was leased and then un-revocable — which
  is worse than not leasing it, because the grant claimed a governance it did not
  have and merely aged out on its TTL.

- **A SHARED grant is withdrawn by its last holder, not by the first session to
  end.** A daemon's grant is per account on that host, so two concurrent sessions
  there are served under one grant. Withdrawing it on the first conclusion would
  refuse the other session's next refresh and break a job nobody ended. So
  `concludeBackend` asks the durable session records who else is still being
  served under each grant (`credentialLeasesHeldElsewhere`) and withdraws only
  what nothing live still holds. A `closed` or `error` record is not a holder: it
  has no process to refresh with, and a resume re-resolves and takes its own
  grant. A store that cannot answer the scan FAILS the conclusion rather than
  revoking on the assumption nobody else holds it — the conclusion is retryable,
  a broken running session is not.

  "Revoked on session close" therefore reads, precisely: revoked once no live
  session is still being served under it.

- **`SessionRecord.CredentialLeaseIDs`, on the DURABLE record.** Every route a
  session can end by now withdraws its grants, including the routes with nothing
  left in memory — the node restarted, the job failed, the daemon left the fleet
  — because `session_reconcile.go` concludes those from the store alone.
  `concludeBackend`'s revocation failure is RETURNED rather than logged: a
  session whose grant could not be withdrawn is not concluded, so the next pass
  retries the whole teardown. Marking it closed is the one outcome nothing can
  repair, because a closed record is never revisited. Revocation is idempotent,
  so the retry converges. A rotation withdraws the grant it supersedes and
  records the replacement in the same step; a spawn that resolved an account and
  then failed to dispatch withdraws its own grant, which is the only revocation
  the backend owns.

- **No credential material reaches a lease record, a log line or an audit
  event.** A grant's metadata is identifiers only — kind, tenant, account,
  provider, project, job, agent — and the audit trail names which GRANT a login
  was released under, never the login. Pinned by
  `TestAccountCredentialLeaser_NoSecretInLeaseOrAudit` and
  `TestMaterialize_AuditNamesTheGrantAndNeverTheCredential`.

- **`make check-local-keygen`** — `scripts/localkeygen` fails the build on any
  `ecdsa`/`rsa`/`ed25519.GenerateKey`, `x509.CreateCertificate` or
  `x509.CreateCertificateRequest` in non-test `pkg/`, `cmd/` or `sdk/` code. It
  is a prerequisite of `make test` and the four tag-variant test targets.

  The gate is worth as much as the refactor: six local `GenerateKey` calls
  accumulated across three packages with nothing in the build objecting, and
  without this the next one lands the same way. Resolution is syntactic (no build
  tags, no module loading, runs on a tree that does not compile), and a site with
  no alternative waives itself in place with `// local-keygen-ok: <reason>` —
  an empty reason is not a waiver. `pkg/signal`'s two Signal-protocol X3DH key
  generations carry the only waivers outside the generator; whether that material
  belongs under go-xkms custody is left as an open question rather than decided
  here.

- **`pkg/testutil/xkmsca`** — a real, in-memory go-xkms CA for tests, so a
  package that needs one stops mocking it. Two mocked CAs have now hidden
  production defects in this stack; a mock cannot detect either, and this costs
  nothing to build. It offers the CA directly (`New`, `NewUninitialized`), the
  same CA behind the transport interface (`NewCAService`, for code that consumes
  `xkmstransport.CAService`), and faults on individual methods
  (`WithoutCABundle`, `SignCSRErr`, `SuppressChain`) so an error path is
  exercised by a real CA refusing one thing rather than by a fake agreeing to
  everything.

- `ForgeBackend.ListOpenPullRequests` resolves a commit to the open pull requests
  it heads, because a pipeline delivery — a commit status, a check run, a workflow
  job — names a commit and not a pull request on every forge the module speaks to.
  Whole and paged: a capped page would answer "no pull request has this head" for
  a busy repository, and the change whose CI just passed would never be reviewed.

- **`ReviewSweeper` — the polling floor under review dispatch**
  (`pkg/sdlc/review_sweep.go`). Per bound project it lists the open pull
  requests and reads each head's commit statuses **once**: a head carrying no
  status under the reviewer's own context, whose other checks have all finished
  green, is handed to the existing `ReviewTrigger.TriggerReview`. It is a second
  trigger for an existing port and not new review logic — green is
  `ChecksGreenForReview`, the same function the delivery path and the merge gate
  call, and whether a review is warranted stays the trigger's decision.

  The absent status is the filter because the forge already holds the answer:
  the review job publishes under its context on every ending it has — a verdict,
  a re-asserted standing verdict, a draft it declined to approve, a head it ran
  out of rounds for — bound to the head it is about and surviving any restart of
  this node. Double-starting stays the engine's to prevent: a delivery and a
  pass that race name the same pull request, so both jobs take the same lease
  subject (`ReviewLeaseSubject`) and exactly one acquires it.

  Cost per pass per bound project: one `GET /pulls`, plus one
  `GET /commits/{sha}/statuses` per open pull request. A genuine candidate costs
  two more calls, because the dispatcher re-reads the pull request and its head
  before starting a job — paid only where a whole model run is about to be
  spent.

- **`ProjectBinding.Discovery` — `poll` | `webhook` | `both`, default `both`**
  (`pkg/sdlc/discovery.go`), the discovery mode from
  `docs/design/autonomous-control-loop.md`. `webhook` makes the pass skip the
  project without a single forge call; `poll` ignores the review half of a
  delivery while still nudging the issue poller; `both` runs the two and cannot
  double-start. The modes are independent rather than a flag in front of one
  code path, and a mode this module does not implement is refused when the
  project is bound rather than discovered later as a project that looks
  configured and finds nothing.

- **`sdlc.review_sweep_interval`** (default 5m). The floor runs on the SDLC
  backstop goroutine beside the triage pass — no third goroutine — on its own
  ticker, with one pass at boot: the deliveries a node missed are exactly the
  ones that arrived while it was down. The two intervals stay separate because a
  triage pass starts a job per bound project where a review pass only reads the
  forge.

- **The Software Factory image now carries both agent CLIs, so it can host a
  reviewer as well as a worker.** `ReviewPolicy.RequireIndependentReviewer`
  refuses an approval from the model family that wrote the code, so a project
  with a `claude` worker needs a reviewer from another family — and
  `backendFamilies` maps `codex` to OpenAI. The image installed
  `@anthropic-ai/claude-code` only, so no image in the fleet could host the
  reviewer the policy demands. It now installs `@openai/codex` alongside it and
  asserts both versions in the same layer.

  **One image for both roles, deliberately.** What the reviewer needs *from the
  image* is what the worker needs — git, a real checkout, the non-root agent
  user, the passwordless `sudo` grant and the workspace contract. A second
  Dockerfile would duplicate every one of those and then drift.
  `ReviewerConfig.Image` stays a separate field so a project may differ; the
  default is that it does not. `deployments/agents/codex` remains the
  standalone single-purpose image and is not the factory's reviewer.

  `make test-worker-image` now asserts `codex` is present, so the image cannot
  regress into hosting only one role.

- **`pkg/labstrings`, the release gate.** It scans every file in the git index
  and fails on a fleet address, a fleet hostname, or a path under the owner's
  home directory or the lab's NFS mount. It runs as a unit test
  — `TestTheTrackedTreeIsPubliclyReleasable` — so `make test` runs it with no
  Makefile target to remember and no target list it can fall off, and resolution
  is textual so it covers Markdown, YAML, Dockerfiles, shell, Python and minified
  JavaScript as readily as Go, on a tree that does not compile.

  A line whose lab value *is* the point waives itself in place with a
  `lab-string-ok: <reason>` comment — the same mechanism `scripts/localkeygen`
  and `scripts/codedsentinel` use, and an empty reason is not a waiver. Whole
  trees, and files that cannot carry a comment, are covered by an explicit
  `DefaultAllowList` where every entry names the classes it accepts and why; an
  entry with no reason fails the gate rather than widening it.

  The scan reads 30 MB of tracked content in about a second because each pattern
  carries a lowercase literal needle that any match must contain, and a
  case-folded substring search settles "there is nothing here" two orders of
  magnitude faster than the patterns themselves. A needle that its own pattern
  does not imply would silently blind the gate, so one test pins that invariant
  against a sample of the values the audit found.

  `docs/release/public-releasability.md` documents the rule, the waiver, the
  allow-list, and how to add a fleet value to the rule set.

- **`orchestration.backend_concurrency` bounds how many jobs run at once on
  each agent backend.** A project quota bounds one project and the fleet quota
  bounds worker slots; neither can express "this GPU serves two things at
  once", which is the only limit a locally hosted model has. It matters most
  for reviews, which are off the project budget by construction so a pull
  request never queues behind the work it unblocks — with the consequence that
  a project's every green, unjudged pull request is reviewed in the same pass.
  Measured on the burn-in fleet: five such reviews landed on one 27B model at
  once, the GPU pinned at 99%, a two-second prompt took 115 seconds, and no
  review finished in 37 minutes. A backend that is absent or mapped to zero is
  unlimited, so a deployment that configures nothing is unchanged.

- **A peer review can run on a locally served Qwen, as an alternative to codex.**
  A new `qwen` agent backend drives the codex binary against an
  OpenAI-compatible endpoint instead of OpenAI, configured entirely on argv, so
  it needs no config file and — unlike codex-on-OpenAI — no credential at all.
  `ModelFamilyQwen` lets the independence rule classify it: the served models
  are tagged `review-bf16` / `review-q8` / `review-q4_K_M`, which carry no
  "qwen" for a prefix rule to find, so the backend name is what identifies them.
  Configure the endpoint with `quicbaas agent serve --ollama-url`.

- **`sdlc.label_reconcile` configuration.** `interval` (default `5m`) is the
  sweep's cadence and `forge_interval` (default `200ms`) the minimum spacing
  between its forge calls. Zero takes the module's default for both; a negative
  value is refused at load. Both are reachable from the environment as
  `QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL` and
  `QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL`, bound explicitly from
  `SDLCLabelReconcileKeys` on both viper instances that load the daemon —
  `AutomaticEnv` alone does not reach `Unmarshal` for a key absent from the
  config file, so an unbound knob is readable from a file and never from the
  environment. A reflection drift gate fails the suite if a field is ever added
  without a bound key, which is the check that was missing.

- **`sdlc.enabled` and the module's other scalar keys are bound too**
  (`SDLCKeys` / `BindSDLCEnv`, on both viper instances). The module is opt-in, so
  an unbound `enabled` meant an environment-only deployment could set the sweep's
  cadence, have it decoded, and still boot with SDLC off — a worse failure than a
  dropped knob, because every variable that *did* arrive looks like it worked.
  `verification_commands` and `repo_hosts` stay file-only and say so: viper does
  not decode an environment list into a `[]string` reliably through `Unmarshal`,
  and binding them would advertise a reachability that does not hold. A drift gate
  walks the struct and skips nested structs and slices **by type**, so neither
  exemption can quietly swallow a scalar. There is no operator verb: the SDLC module exposes
  none for its other sweeps, and the state this one repairs is by definition
  state nobody noticed.

### Changed

- **Accepting unparseable stdout as a turn now emits a warning naming the
  command, the session and the byte count.** The fallback's *outcome* is
  unchanged and deliberately so: whether the one-shot path may accept
  unparseable stdout as a turn at all is an open behaviour question, and a
  backend whose CLI does not honour `--output-format json` works today because
  of that branch. What was wrong is that taking it left no trace — a result
  truncated by a killed process, or one carrying a number outside `int64`
  (which fails the whole unmarshal), arrived as an ordinary `end_turn` whose
  "answer" was the broken bytes.

  The stdout bytes are **counted, not logged**: stdout is model output from a
  process holding credential material in its environment, and this package does
  not render such buffers into log lines.

- **`TokenUsage.Malformed` reports that a record was repaired on the way in,
  and `recordTurnUsage` logs a warning when it was.** Clamping alone is not
  enough: a record whose every dimension was negative clamps to four zeroes and
  is dropped from the ledger exactly as a turn that genuinely cost nothing is.
  Without a mark the two are the same event to every downstream reader, which
  is the property that made the original defect invisible rather than merely
  wrong.

  Malformedness propagates through `Add`, because the accumulated per-message
  total is what gets billed when a turn's terminal result carries no aggregate.

  A repaired turn is **not** failed. The turn has already produced its answer,
  and usage recording is best-effort by construction — failing it over
  bookkeeping would discard work on account of the record describing it.
  Whether a malformed record should instead fail the turn outright remains open
  and is the owner's call.

- **`TurnFailureEvidence.usageProvesNoTurn` now also requires that the record
  was not repaired.** It proved "no request reached the model" by testing every
  dimension for exactly zero — a test that rejected `{"input_tokens":-5}`
  precisely because `-5` is not `0`. Clamping at the decode boundary takes that
  proof away, since the record now arrives as four zeroes and would read as
  evidence. Without this the repair would manufacture the evidence, and turns
  would be refunded their attempt on exactly the accounting that was refused as
  untrustworthy.

- **`PushService.ListChannels`, `GetChannel` and `Publish` return
  `ErrHubOverloaded` (`ErrResourceExhausted`) when the hub's request queue is
  full**, on paths that previously succeeded. This is separate from the stopped-hub
  fix above and affects a **live, healthy hub under load**: the two calls share
  one refusal path, so a full queue previously produced the same empty list and
  the same not-found as a stopped loop.

  **A caller polling through a transient queue-full will now see failures where
  it saw empty listings.** If you are diagnosing a new burst of
  resource-exhausted errors from the messaging surface under load, this change
  is the cause. Retry on `ErrHubOverloaded`; it is a transient backpressure
  signal, not an assertion about what exists. Treating an empty list as
  authoritative during load was the defect being removed — the answer was
  always wrong, it was merely silent.

  No signatures changed: the REST, gRPC, MCP and unix-socket handlers, and both
  SDK transports, already propagate whatever error they are handed.

- **BREAKING (pre-v1, no migration): workflow definitions are tenant-scoped.**
  The KV key moved from `_orchestration/workflows/<name>` to
  `_orchestration/workflows/<tenant>/<name>`, `WorkflowLoader` takes the
  tenant on every verb (`Register`, `Update`, `Delete`, `Get`, `List`,
  `ListDefinitions`, `LoadFromFile`, `LoadFromDir`, `Rehydrate`), and
  `WorkflowDefinition` carries `tenant_id`. Two tenants may now register the
  same workflow name without colliding; the REST workflow handlers derive the
  tenant exactly as every other orchestrator handler does (query, then token
  claim, then system). Definitions stored under the old key are not read:
  there is one format, and a workflow registered before this change must be
  registered again under its tenant.

- **BREAKING (pre-v1, no migration): `orchestration.TaskSource` is now
  `TaskSourceRef`, and `TaskSourceKind` is an open set.** The provenance
  struct on a Task (`Kind`, `Ref`, `URL`; JSON `source`, unchanged on the
  wire) is renamed so `TaskSource` can name the seam a module supplies work
  through. The engine keeps only the kinds it emits itself (`scheduler`,
  `operator`, `job`); every other kind is declared by the `TaskSource` that
  registers it. The module-specific kind constant the engine previously
  enumerated is gone: a task carrying it is refused until a source declares
  it.
- **`orchestration.Validator` is the artifact-scoped shape; the registry
  holds `ValidatorFunc`.** `ValidatorRegistry.Register` takes a
  `ValidatorFunc(ctx, *JobContext, ReviewCriterion)`, returns a typed error
  on a duplicate kind, and `RunPipeline` takes the `*JobContext`. An
  artifact-scoped `Validator` registers through `ArtifactValidator`. The
  engine's registry carries `agent`, `command` and `policy` at init; a
  workflow criterion naming an unregistered kind is refused at validation.
- **The `command` validator applies the operator allowlist
  `orchestration.verification_commands`** (exact match, zero value refuses
  everything) and runs in the job's prepared directory.

- **BREAKING (pre-v1, no migration): `pkg/sdlc` is the SDLC module of the
  Software Factory and owns no engine.** Its second control loop, lease
  system, reaper, work item, project record and stage machinery
  (`loop.go`, `dispatch*.go`, `coordination*.go`, `supersede*.go`,
  `session_*.go`, `mirror_lease.go`, `mirror_queue.go`, `reap.go`,
  `stall*.go`, `worker*.go`, `types.go`, `domain_store.go`, …) are deleted.
  The module plugs into the engine's four seams: the `forge_issue`
  `TaskSource`, the `forge_review` validator (reviewing phase), the `sdlc`
  `JobLifecycleHooks` on `service.GitProvisioner`. A labelled issue is an
  orchestration Task, each attempt a Job, the worker a Session, and the
  pull request is opened, reviewed and merged by the hooks — all visible
  through Projects / Tasks / Jobs / Sessions. **No canonical `sdlc`
  Workflow:** the owner ruled (2026-09-06) that the pipeline is not a
  Workflow on the control loop, whose one shape is lead-driven
  decomposition; the `JobRunner` seam and the `sdlc` runner follow (Phases
  B/C). Bind registers no workflow and does not set
  `Project.DefaultWorkflow`.
- **Two referencing records replace `SDLCProject` / `WorkItem` /
  `PullRequest` / `Review` / `PipelineRun`:** `ProjectBinding` (per
  Project: forge, review policy, publication, ranking) and `Delivery` (per
  Task: branch, head, pull request, reviews, checks, version). The live
  tenant's old records are deleted and the project re-bound; there is no
  migration.
- **Surface:** REST `GET|PUT|DELETE /api/v1/sdlc/projects/{projectID}/binding`,
  `GET /api/v1/sdlc/tasks/{taskID}/delivery`, `POST /api/v1/sdlc/webhook/{projectID}`
  (HMAC, public); the `/api/v1/sdlc/projects[/{id}[/items[/{itemID}]]]`
  registration and work-item surface is gone. Permission `sdlc:*` shrinks
  to `sdlc:read|bind|unbind`. The service is on every transport
  (`quicbaas.sdlc.v1.SDLCService` on gRPC/Unix, `sdlc_*` MCP tools,
  embedded, REST/QUIC), in the Go SDK (`client.SDLC()`:
  `Bind|Unbind|Binding|Delivery`), the TypeScript SDK (`client.sdlc`) and
  the CLI (`quicbaas sdlc bind|unbind|binding|delivery`).
- **Config split:** `sdlc:` keeps `enabled, repo_hosts, allow_local_repos,
  verification_commands, merge_attempts, surfacing, webhook.max_body,
  issue_page_limit`. The generic knobs
  (`attempt_budget, reap_lease_ttl, stage_timeouts, idle_thresholds,
  dispatch_cap, run_timeout, sweep_interval, agent_config_dir` — the last
  is `orchestration.agent_config_dir`, the one knob the engine puts on
  every session it spawns, and the
  removed `mirror_lease_ttl, queue_capacity, delivery_memory,
  max_webhook_body, stage_event_flush_timeout`) are refused at load by
  name with where each went (`config.RemovedSDLCKeyError`). `companions`
  and `worktree_root` are refused the same way (go-quicbaas#601): the
  daemon materialises no workspace, so neither has a replacement key.
- **Engine additions the module needed:** `AgentRoleSpec.SystemPrompt`
  (a role's standing instruction set, passed to every session of the
  role) and `TaskSourcePoller.SweepProject` (a per-project sweep, the
  module's webhook nudge). `pkg/orchestration` never imports `pkg/sdlc`;
  `pkg/sdlc/import_graph_test.go` pins it.
- **Restart ordering fix:** interrupted jobs are resumed after every
  module has registered its hooks (`Server.recoverOrchestrationJobs`, a
  lifecycle step after `startSDLC`) instead of inside `startOrchestration`.
  A job resumed before the module started ran its `prepare` hook against
  "the module is not running on this node" and failed; the daemon e2e
  `TestSoftwareFactory_KilledWorkerIsResumedOnRestart` pins the fix.
  Re-dispatch of a task the reaper requeued is go-quicbaas#570.
- The module's single-process gate (`ErrSDLCClusterUnsupported`) is gone:
  the module is cluster-capable because the engine is. The module's docs
  move to `docs/sdlc/`.

- Pins: `go-qrdb` / `go-qrdb/sdk/go` `v0.16.2-alpha → v0.16.3-alpha` (the
  release adding the one write path that may rewrite a built-in role) and
  `go-xkms` / `go-xkms/sdk/go` `v0.9.2-alpha → v0.9.5-alpha`, in both
  modules. No `replace` directive is added or left behind.

- **`docs/iam/engineering.md` documents the WebAuthn credential
  administration surface** it previously described as deleted: the three
  verbs, their transports and CLI, the base64url `{credential_id}` contract,
  and the three id outcomes (malformed, truncated, unknown).

- **One name for the area: the Software Factory, and one for the git module:
  the SDLC module.** An older working name for the area survived in 77 places
  across 34 tracked files — doc comments, prose, the orchestrator proto and its
  generated Go, test names and strings, a CI script and two workflow comments.
  Every occurrence is reworded. The one test fixture that changed: the
  tenant-crossing test's user is now `factory-ui@`.

- **`scripts/verify-naming.sh` refuses the retired word in any tracked file**,
  and `make lint` runs it before golangci-lint, so CI's Lint job gates on it.
  There is no allowlist: every tracked file is in scope.

- **The job envelope is `*JobRun`, not `ControlLoop`.**
  `pkg/orchestration/job_run.go`, `job_run_lease.go` (renamed from
  `control_loop_lease.go`) and `job_envelope.go` own everything true of
  every job whatever its shape: the identity, the lease and its heartbeat,
  the status snapshot and its persistence, the event stream, the audit
  trail, the five lifecycle moments, the sessions, the abort path and the
  terminal accounting. `SetState` is the single choke point — heartbeat,
  snapshot and persist, announce, then run the moment the state owns.
  `ControlLoop.RunJob` is `RunFromState`'s dispatch loop and only that;
  `ControlLoop` does not implement `JobRunner`.
- **`launchJob` is the one start path**, fresh or resumed. It mints the job
  ID, loads the Task and Project, resolves a workflow job's definition and
  hook set, registers the job with a status *before* its goroutine exists,
  and runs `execute` — which owns the lease, the heartbeat, the panic
  boundary (`FailureClassPanic`), the terminal status a runner did not
  write, and `Discard` on every exit. `StartJob` and `resumeJob` are both
  this call. `StartJob`'s two-second poll for the loop to publish its job ID
  is deleted.
- **The job lease is acquired BEFORE the `Prepare` hook** (previously
  `Prepare` ran first and the lease was taken inside `initializing`).
  Prepare is the first moment with a side effect outside the daemon — it
  labels an issue — so the capacity gate now runs before anything has
  changed.
- `JobLease.Workflow` is the job's workflow name, or its **runner** when the
  job names no workflow. A lease key for a job with no workflow name is no
  longer degenerate.
- The stall detector examines `RunningJobs() []*JobRun` (`JobRunSource`)
  rather than `RunningLoops() []*ControlLoop`.

- **BREAKING (pre-v1, no migration): one `Strategy` per SDLC replaces the
  `BranchingStrategy` + `MergeStrategy` pair.** A project names one SDLC and
  that strategy owns the branch, its base and target, whether the change is
  reviewed, how it lands and what follows: `Strategy{Name, PlanBranch,
  PlanMerge, PostMerge}`. The old split let a project select gitflow
  branching with a squash merge — a combination no SDLC describes, and one
  that discards the merge commit gitflow's history depends on. On the wire
  and in every SDK, `forge.branching_strategy` and `forge.merge_strategy`
  become `strategy`.
- **Every `BranchPlan` field is honoured.** `Base` and `Target` may differ,
  and the project's default branch is now a DEFAULT the strategy reads
  (`BranchPlan.Resolve`) rather than an override applied behind it — the
  `hooks.go` override that overwrote both refs is deleted. It made every
  plan single-ref and put gitflow out of reach entirely.
  `RequiresPullRequest` and `DeleteAfterMerge` were declared and never read;
  both are now recorded on the `Delivery` at `Prepare` and read by
  `Reviewable` and `Complete`. Branch lifecycle is decided once, on the
  plan, and copied into `MergeSpec.ExpectSourceDeleted`.
- **`ProjectBinding` is the project's autonomous-development
  configuration**, gaining `Strategy`, `Worker{Backend, Model}`,
  `Verification []string` and `MaxReviewRounds` — the settings that lived on
  the deleted canonical Workflow and are SDLC settings, not workflow ones.
  Validation is fail-closed with typed errors on every field.
- `Delivery` gains `Target`, `RequiresPullRequest` and `DeleteAfterMerge`;
  `Base` now means the ref the branch was cut from, and `Target` the ref it
  lands on.
- `sdlc.Workspace.Push` takes a `PushSpec`, whose `RemoteBranch` lets a
  change exempted from review land on the trunk directly. `GitPushRequest`
  gains the matching `RemoteBranch`; the push is still never forced.
- `ForgeIssue` gains `Milestone`, parsed by the Gitea adapter, so a strategy
  can route on it.
- `StrategyNotRegisteredError` loses `Kind` and gains `Available`;
  `ForgeNotRegisteredError` gains `Available`. Both refusals now name what
  the build DOES carry, rather than only what failed.

- **Recovery asks the runner where to restart.** `resumeStateFor(runner,
  interrupted)` consults `JobResumer` and otherwise restarts at the runner's
  first state; `resolveResumeState` loses its `bound` parameter and the
  unexported `boundJobResumer` seam B1 carried is gone. `resumeJob` resolves
  the runner from the persisted status and hands it to `launchJob`.
- `JobConfig.TaskID` keeps its accounting meaning for a workflow job started
  as an attempt at a Task — lease subject and generation, the synchronous
  `concludeAttempt`, the reaper's conclusion, stall membership and
  `JobContext.TaskID`/`Attempt`. Only the work-item mapping went.

- **BREAKING (pre-v1, no migration): `Project.Autonomous` gates the
  task-source poller.** A project that relied on a non-empty
  `DefaultWorkflow` alone to have its work adopted stops adopting; set
  `Autonomous` on it. `DefaultWorkflow` keeps only its original meaning —
  the workflow a Task under the project is auto-spawned into — and gates
  nothing.
- **BREAKING (pre-v1, no migration): `sdlc.companions` and
  `sdlc.worktree_root` are removed and refused by name at load**
  (`config.RemovedSDLCKeys` → `RemovedSDLCKeyError`), from the file and from
  the environment. Name the repositories under the project's
  `context_repos` instead.
- **BREAKING (pre-v1, no migration): the daemon no longer creates worktrees
  or copies companions.** The worker's container clones the project and its
  `context_repos` itself, so nothing the daemon makes has to be reaped, kept
  in step with the record, or reasoned about after a crash. `Prepare` now
  plans the attempt's branch, writes the `Delivery` and moves the issue
  label, returning no work directory; `Discard` does no filesystem work.
  `GitProvisioner.AddWorktree`, `RemoveWorktree`, `ListWorktrees`,
  `DeleteLocalBranch`, `DiscardedRefPrefix` and the `.factory/` companion
  directory are deleted, as is `sdlc.ValidateWorktreePathSegment`.

- **The job stepper follows the runner.** `JobStateMachine.svelte` built its
  steps from a hardcoded copy of the engine's thirteen job states. It now
  builds them from the states declared by the runner named on
  `status.runner` (absent → `workflow`) plus `completed`/`failed`, so an
  `sdlc` job renders its own eight states plus those two — ten steps — instead
  of a lead-driven DAG's thirteen. A runner this node does not register is
  said so in place rather than rendered as invented states.

- **BREAKING (pre-v1, no migration): there is no registered `sdlc`
  `JobLifecycleHooks` set.** `RegisterJobHooks("sdlc", …)` and
  `sdlc.HooksName` are deleted. The runner calls the module's moments directly
  through `job.RunStage(ctx, JobStagePrepare, …)`, so it gets the operator's
  stage timeout, `JobStatus.stage` and the stage event without the
  state→moment table — and can run `Reviewable` again on each review round,
  which the table cannot express.
- `TaskSourceSpec.Runner` is `sdlc` on every task the `forge_issue` source
  adopts, so a requeued task is re-dispatched by the runner that first ran it.
- **The SDLC module's seams are registered BEFORE `startOrchestration`** and
  its runtime after it. The stall detector's idle thresholds are validated
  against the states every registered runner declares; a runner registered
  afterwards would declare states the operator could not configure.
- The review gate's independence rule reads the author's model family from the
  **binding's worker** (`binding.Worker.Backend/Model`). It read it from the
  canonical `sdlc` workflow's worker role, and that workflow is gone.

- **`MountPolicy` admits the sockets root by construction.** There is no
  allowlist entry to remember: `orchestration.sandbox.allowed_mount_roots`
  stays the operator's own surface. The grant is the narrowest one that works
  — the socket file itself, one directory below the root, actually a socket on
  disk, and read-only — so the root, the session directory, and anything
  beside or below the socket stay denied, which is also what stops a caller
  mounting the root to enumerate the sessions inside it. The signing socket's
  container mount is now read-only to match; a unix socket is exempt from a
  read-only superblock's write check, so `connect()` is unaffected.

- **`recordTurnUsage` writes the counters and the ledger from one site.** The
  counters report and the ledger decides, and feeding both from one call is
  what keeps them from disagreeing about a turn: a turn is in both or in
  neither. A turn that drew on no pooled account is reported and not ledgered —
  the quota plane is keyed by account because an allowance belongs to one, and
  filing such a turn under a placeholder would make a fictional account the
  tenant's largest spender.

- **A turn whose backend reported no usage is recorded as an OCCURRENCE, not
  as a quantity.** The counters skip it, because they are the figure
  `LeastUsageAccountStrategy` balances a pool on and a zero there makes the
  account that ran it look perpetually least-used. The LEDGER records it,
  because the breaker reads occurrences: a success is what says an account is
  working again, and dropping the unmeasured ones left an account that could
  trip and could never be talked out of it by the very turn that proved
  recovery. The backends this bites are the ones that publish no token counts
  at all — the ACP JSON-RPC dialect and `codex exec` — for which EVERY success
  is unmeasured. Caught in peer review of this PR;
  `TestUnmeasuredSuccessUntripsTheBreaker` fails without the fix.

- **A FAILED turn is now recorded too.** Its tokens were spent if the backend
  reported any, and its occurrence is what the breaker is derived from: a
  ledger of successes says every account is healthy however hard it is failing.
  It is recorded even when the backend reported nothing, because a provider
  that refuses instantly costs nothing and is exactly the failure worth
  tripping on.

- **`WithTurnAttribution` now requires a job and no longer requires an agent,
  and carries the task.** The advisory counters' job dimension is
  per-agent-within-job and still refuses a job with no agent; the ledger's
  chain has no agent level at all, and a runner that prompts one session
  directly (`pkg/sdlc`) has a job and no agent. Requiring the agent dropped
  that whole population from the record the cap is decided from.
  `JobRun.Prompt` attaches the attribution at the envelope every runner already
  prompts through, so no runner can forget; a caller that attached a richer one
  keeps it.

- **A turn identity is minted in `planPromptTurn` rather than in the
  asynchronous path.** The ack a client correlates on and the ledger entry the
  turn is charged to are now the same value, and an id minted at the moment of
  recording would be new on every attempt — so a retried write would be a
  second charge rather than a converged one.

- **One feature, one docs folder.** `docs/agent-orchestration/` (10 files),
  `docs/sdlc/` (7) and `docs/agentic-projects/` (5) described one product
  area across three top-level folders. They are now
  [`docs/software-factory/`](../docs/software-factory/): five canonical files
  covering the orchestration engine, agentic projects, pooled provider
  accounts, the SDLC module and prompt sets, plus the supplements
  `sdlc.md`, `projects.md`, `accounts.md`, `prompt-sets.md`,
  `job-runners.md`, `autonomy-and-dispatch.md`, `job-admission.md`,
  `sandbox.md` and `cost-enforcement.md`.

  `docs/sdlc/` had no `cli.md`, `engineering.md` or `user.md`. Those sections
  are written from the code, not from memory: the `quicbaas sdlc` verb and
  flag tables from `cmd/quicbaas/sdlc.go`, the exported surface from
  `pkg/sdlc` and `pkg/service/sdlc_service.go`, and both error mappings from
  `pkg/api/rest/handlers_sdlc.go:80` and `pkg/api/grpc/server_sdlc.go:182`.

  Corrections made while verifying the moved text against the code:
  the Project service is served on all six transports, not "REST and embedded
  only"; `quicbaas project create`/`update` carry sixteen flags the tables
  omitted; the project CLI takes a positional project ID, not `--id`, and the
  flag is `--default-lead-agent`, not `--lead-agent`; five cross-references
  pointed at `docs/orchestration/*.md` and `docs/security/audit.md`, removed
  by the 2026-08-19 migration; a version conflict maps to gRPC
  `AlreadyExists`, not `Aborted`. Each correction is noted where it was made.

  Every file in the folder now carries a status banner and a `Last verified`
  stamp — `autonomy-and-dispatch.md`, `job-admission.md`, `job-runners.md`
  and `sandbox.md` had none — and the absorbed pages' deployment host, ports
  and agent principal names are replaced with published placeholders
  (feature-docs.md §10).

  Every inbound link is repointed: `README.md`, `docs/README.md` (one row in
  place of three, both migration counts recounted, a migration entry naming
  what was absorbed), `docs/desktop-gui/*`, `docs/ollama/architecture.md`,
  `docs/ssh/*`, `docs/scheduler/user.md`, `docs/multiprotocol-api/architecture.md`,
  `docs/embeddings/user.md` and `docs/barrier/engineering.md`. Fragments in
  `changelog.d/` are history and are left as written.

- **Every instruction the Software Factory sends an agent is now markdown a
  person can edit, not a Go constant.** The 149-line `WorkerCharter`, the issue
  contract, the round findings, the verification and review framing, the
  CI-red message, the park reasons and every outcome comment; and on the
  engine's side the worker, reviewer, planner, decomposition and validation
  prompts, the job memory's serialization, the resume priming and the spawn
  handshake. 56 prompts across two sets, embedded in the binary.

  `pkg/orchestration/prompts` is the generic package: `SetKind`, `PromptSpec`,
  `SetSpec`, a registry swapped as a whole under `atomic.Pointer`, a
  validator, a `${name}` renderer and a content hash. The `workflow` set is
  owned by `pkg/orchestration` from `promptdefaults/workflow/*.md`; the `sdlc`
  set by `pkg/sdlc` from `promptdefaults/sdlc/*.md`. **Those files are the
  system tenant's defaults** — what go-quicbaas#804 will seed into the system
  tenant and copy to each new tenant — and the tree says so.

  **Templating is `${name}` substitution against a declared variable list, not
  a template language.** `text/template` would let human-edited markdown carry
  `{{if}}` and `{{range}}` that no validator can judge, and an unvalidatable
  prompt is the failure the whole store exists to prevent. Loops, plurals,
  fenced logs, secret redaction, renumbered comments and every JSON assembly
  stay in Go as thin assemblers. A substituted value is never rescanned, so
  text an agent wrote cannot open a hole of its own.

  **The review control the charter's constant asserted is now enforced rather
  than assumed.** One validator runs at registration and (from #804) at save,
  on four rules: the body is not blank, every `${x}` is declared, every
  required variable is present, and every marker literal the engine parses is
  still there. Each refusal is typed and names the thing — `MissingMarkerError`
  names the marker *and what reads it*. A shipped default that fails its own
  specification panics in `init()` the way an unregisterable job runner does.
  The charter's markers are built from `TurnStopMarker` and every key of
  `stopCodeOutcomes`, so a stop code `ClassifyTurn` dispatches on is a code the
  charter must teach, and one it stopped teaching will not build.

  **Nothing an agent reads changed.** `testdata/prompts/*.golden` in both
  packages were written by the previous builders before a word moved, across
  the variable combinations each branches on — with and without a workspace,
  an issue with no body, a discussion whose blank comments still renumber, one
  failed check against several, a redacted log, a park with no evidence, each
  outcome remedy in its precedence order. Every one is byte-identical
  afterwards.

  The tenant store, inheritance, history, rollback, the
  `prompts:read|write|history` permission family, resolution at job launch,
  the six transports, the CLI and the GUI are go-quicbaas#804 to #807. Editing
  a prompt today is editing a file and rebuilding.
  `docs/software-factory/prompt-sets.md` describes what is built, what
  deliberately stayed in Go and why, and carries a catalogue of the
  instructions we are still paying a model to follow that ordinary code could
  do instead.

- **`JobConfig` and `StartJobRequest` carry `Params map[string]string`.** It
  is the missing half of the `JobRunner` seam: every other field describes the
  engine's own shape, so a runner whose job is about something else had
  nowhere to be told what. Opaque to the engine, namespaced by runner,
  validated by the runner before any goroutine or lease exists, copied on the
  way in, and — because it is persisted and displayed — never a credential
  channel.

  The commit status is published under **`quicbaas/review`** by default —
  the context branch protection requires fleet-wide — and the name is
  `ReviewerConfig.StatusContext` on the binding rather than a literal at the
  call site, so onboarding a repository that wants a different check name
  never means editing Go.

- **`sdlc.webhook.public_url` is required when the SDLC module is enabled.**
  Onboarding provisions a forge webhook pointing at
  `<public_url>/api/v1/sdlc/webhook/<projectID>`, and "the webhook points at
  *this node*" is a property of the node rather than of any project. It is
  refused at load rather than defaulted: a guessed origin would provision every
  managed repository to deliver into the void and report each one provisioned,
  with the failure silent on both sides — the forge records a delivery it made,
  and the node never sees one. A drift gate pins the path prefix against the
  route the router actually mounts.

- `JobRun.Park` takes the operator fault class as a third argument, so every
  park states whose fault it was rather than defaulting silently to the work's.

- **QuicBaaS no longer generates keys or certificates in six places; it
  generates them in one, and that one is gated by the build.** Owner ruling:
  *"we have certificate and key generation code in quicbaas, that shouldn't ever
  happen. We should be using the xkms SDK for keys and certificates."*

  Three non-test sites created private keys locally — `pkg/server`'s CA service
  adapter (a six-algorithm `ecdsa`/`rsa`/`ed25519.GenerateKey` switch), the
  first-boot admin bootstrap, and `quicbaas agent deploy` — while the file
  beside the first one documented go-xkms as the canonical issuer. Every
  cryptographic step now belongs to go-xkms: `CreateCSRWithKey` builds the CSR,
  `TrustedCA.SignCSR` signs it (the stack's single `x509.CreateCertificate`
  call), `EncodePrivateKeyPEM` encodes the key. `x509.CreateCertificateRequest`
  is gone from non-test code entirely, and with it the hand-rolled
  `x509.MarshalECPrivateKey` / `x509.MarshalPKCS8PrivateKey` / PEM-block
  assembly these three sites used to deliver a key. (`cmd/quicbaas/kms.go` still
  calls `MarshalPKCS8PrivateKey`, and correctly: it re-encodes a key an operator
  *imported*, which creates nothing.)

  The first-boot admin path is a straight deletion. quicbaas used to build the
  admin's keypair and CSR itself and feed them to qrdb as the CSR-sign path
  (Path B). It now leaves `AdminCSRPEM` empty, qrdb selects Path A, and
  go-xkms's own `CAAdapter.GenerateAdminCert` does the work through a seam
  quicbaas already populated. The credential is unchanged apart from gaining
  `OU=system` on the subject; the SPIFFE SAN, the SPKI binding and the
  `<data-dir>/admin/` bundle are identical.

  What could NOT be delegated, and why: go-xkms cannot return a private key on
  any backend. Its keystore wraps every generated key in a non-exportable opaque
  handle, so `TrustedCA.IssueCertificate` returns `PrivateKeyPEM` as `nil`
  always — now pinned against a real software CA in `pkg/testutil/xkmsca`. That
  is the custody rule rather than a gap: go-xkms applies the same rule in its own
  `pkg/init/firstboot_adapter.go`, generating the administrator's key outside the
  CA's keystore because *"it is the administrator's personal credential, not
  platform key material"*. So a credential the REQUESTER holds is created at the
  point of custody through the single audited generator
  `pkg/xkms.GenerateRequesterKey`, and the CA only ever sees a CSR. See
  `docs/certificate-authority/key-custody.md`.

  Honest note on entropy: the generator passes the go-xkms FIPS RNG resolver,
  but since Go 1.26 `ecdsa.GenerateKey` and `rsa.GenerateKey` **ignore** the
  reader they are given and use Go's own FIPS 140-3 DRBG. Only Ed25519 still
  consumes it. Both facts are asserted, so neither the code comment nor the
  documentation can drift from the toolchain, and the stale example in
  `pkg/xkms/rng.go` that advertised `ecdsa.GenerateKey(elliptic.P256(),
  xkms.Reader())` is corrected. The value of the single generator is the audit
  surface and the gate, not the entropy.

- `ReviewTrigger.TriggerReview` takes a `ReviewRequest` carrying the tenant, the
  project, and **either** a pull request number or a head commit, replacing the
  four positional arguments. A port that took only a number was a port a green
  build could not reach.
- `ReviewerCharter` is a function reading
  `promptdefaults/sdlc/reviewer_charter.md` rather than a Go constant, joining
  every other prompt in the set (#880).
- `NewReviewDispatcher` requires a `ForgeResolver`: whether a head is green, and
  which pull request a commit heads, are read from the forge.

- `DispatchReview` takes the `*ProjectBinding` the ingestor already holds rather
  than a project id, so the delivery half's whole decision — including the
  discovery mode — is in one function beside the dispatch table. No extra read.
- `pkg/server/lifecycle_sdlc_triage.go` becomes `lifecycle_sdlc_backstop.go`:
  one loop driving both missed-delivery backstops, each with its own cadence and
  its own log prefix.
- `pkg/testutil/giteafake` models pull requests and commit statuses — paginated
  and state-filtered listings, `GET /pulls/{n}`, and an appending status history
  — so the floor is tested end to end against the real Gitea adapter over a
  repository with **no webhook at all**.

- **The repository's gates are Go.** Seven of the eight tracked shell scripts
  under `scripts/` — 1,137 lines carrying real gate logic, six of them wired
  into `make` — are now Go commands under `scripts/<name>/main.go`, the idiom
  `scripts/localkeygen` already established. `verify-naming.sh` →
  `verifynaming`, `verify-api-shapes.sh` → `verifyapishapes`,
  `verify-cli-flags.sh` → `verifycliflags`, `verify-clean-docker.sh` →
  `verifycleandocker`, `verify-integration-teardown.sh` → `verifyteardown`,
  `docker-image-inventory.sh` → `dockerinventory`. Every gate reports the same
  0 / 1 / 2 contract — clean, a violation, could not run — with typed errors,
  and every one of them now has tests: one proving it passes on compliant
  input and one proving it FAILS on the violation it exists to catch. Several
  had no test at all.

  Two pieces are shared rather than copied. `scripts/internal/dockerinv` is the
  image-identity derivation, which `dockerinventory` prints and
  `verifycleandocker` links — so the inventory and the gate that checks cleanup
  against it cannot disagree about what this repository builds.
  `scripts/internal/gate` holds the exit contract, the typed errors, the one
  way to ask make for a variable, and the fake Docker daemon the two
  recipe-behaviour gates run their cases against. **That fake is the gate
  binary itself**, symlinked onto PATH as `docker` and dispatched on `argv[0]`:
  a Go gate that wrote a bash stub to make itself work would have moved the
  shell rather than removed it.

- **`make verify` is 20 seconds instead of 55.** `verify-cli-flags` built
  qrdbctl, extracted its flag set into `QRDB_FLAGS`, and never read the
  variable again — a minute of build time spent on a comparison nobody made.
  It is gone; xkmsctl and quicbaas are what the gate actually compares.

- **The integration suite's certificates now come from QuicBaaS's own CA.**
  `test/integration/gen-certs` was a 444-line parallel certificate authority:
  it built its own ECDSA root and its own leaves with raw `crypto/ecdsa` and
  `crypto/x509`, hardcoded five node hostnames, three name aliases per node and
  two loopback addresses inside the function that signed the certificate, and
  declared its own SPIFFE trust domain. Every node, every mTLS peer and the
  bootstrap admin in the integration cluster presented material that **no part
  of the product issues** — so any divergence between that lookalike and the
  real issuance path (extensions, SPIFFE URI shape, key usage, chain
  construction) was invisible to every test in the suite. A 5 MB unstripped ELF
  build of it was also tracked at the repository root, from `47f0b37b`, not
  gitignored.

  The fixtures are now issued by `pkg/server.NewCAService` — the CA adapter the
  daemon serves certificate operations through on every transport. Inside it,
  go-xkms does all of the cryptography: the requester key comes from the single
  audited generator (`pkg/xkms.GenerateRequesterKey`), the CSR from go-xkms
  `CreateCSRWithKey`, and the signature from the go-xkms CA's `SignCSR`, which
  is the stack's one `x509.CreateCertificate` call. The cluster topology — the
  identities and the names they are reached by — is a JSON declaration
  (`test/integration/certfixture/topology.json`), not a constant in a signing
  function.

  Four more helpers that minted their own material were folded onto the same
  path: the tenant-admin credential in `test/integration/helpers`, the software
  PIV credential in `test/integration/login`, the PIV/mTLS factor credentials in
  `test/integration/factors`, and the ceremony officer CSR in
  `test/integration/init`. The first two used to read the fixture CA's PRIVATE
  KEY off the mounted volume and sign with it; both now open the same CA the
  cluster was started with and ask it to issue. No `ca.key` is written any more,
  because a go-xkms keystore key is a non-exportable handle and the CA's
  material lives in the keystore where it belongs.

- **`make check-local-keygen` now covers `test/`.** The gate scanned `pkg`,
  `cmd` and `sdk` only, excluding the test tree outright on the grounds that a
  fixture minting a throwaway self-signed certificate is not a key-custody
  decision. That is true of a three-line inline fixture and was not true of what
  grew in the blind spot. The blanket tree exclusion is gone; `_test.go` files
  remain excluded uniformly across every tree, `-include-tests` still reports
  them for an audit, and an exemption is now an explicit `-skip` path rather
  than a tree nobody looks at. The suite proves the gate fails on a new bespoke
  generator under `test/` and passes the product's issuance path.

- UI: the Software Factory section's landing page is called **Dashboard** in the sidebar and on the page itself. The section heading already names the product; an item repeating it read as a second product name rather than as the page it opens (owner ruling 2026-09-11). The other two landing pages keep their distinct names, Overview and Cluster.

- **Asking the owner a question no longer spends an attempt.** A `needs_info`
  park is terminal until a person acts — it never retries on its own — so the
  attempt budget bounded nothing there; all charging it achieved was that when
  the answer finally arrived, readmission met a task with no allowance left.
  Measured on the burn-in fleet: three tasks at seven charged attempts against
  a budget of three, spent entirely on unanswered questions. This reverses the
  rule stated in `TestApplyConclusion_AParkAboutTheWorkIsStillCharged`; parks
  of kind `escalated` and `blocked` are unchanged and still charged.
- Refunded questions are counted in a new `question_attempts` field, separate
  from `environment_faults` and carried across every transport. The blocked
  comment names them apart, because an environment fault means the factory is
  broken while a question means the worker did exactly the right thing.

### Removed

- **`.github/workflows/task-worker.yml`** — the Gitea-side stand-in that
  claimed a `task:ready` issue and drove the SDLC from a runner shell. The
  Software Factory replaces it; nothing in the repository referenced it.

- **`PublicationPolicy` and `ProjectBinding.Publication`**, with every
  consumer across REST, gRPC/Unix, MCP, the embedded transport, the Go SDK
  and the TypeScript SDK. Nothing ever read it.

- **Bound-task mode.** A Job that was an attempt at a Task used to map its
  DAG work items one-for-one onto that Task instead of creating its own:
  `boundResults`, `bindWaveToTask`, `boundTaskResults`, the synthesized
  review tasks, the re-delegation that returned straight to executing, the
  memory-in-the-worker-prompt feedback channel, `resolveBoundResumeState`
  and `BoundTaskSessionError` are all deleted. It was the workaround that
  let one lead-driven loop pretend to be a delivery pipeline; a module with
  its own `JobRunner` is the real answer. **A workflow job attempting a Task
  now creates per-item Tasks like any other lead-driven job.**
- The routing hints are no longer copied into `Task.Metadata` under
  `host_affinity` / `gpu_required` / `backend`. They were written there
  because there was no schema field for them and nothing ever read them
  back; `Task.Routing` is the field. A task's metadata is now exactly what
  its creator supplied.

- **`orchestration.AgentSecurityContext` — a struct that read as a security
  control and was not one.** It carried an access token, an expiry, a correlation
  ID and a list of "IAM policy IDs created for this agent's session", with a
  `Validate` and an `IsTokenExpired`, and had **zero non-test references**:
  nothing populated it, and no authorization check consulted it. It has already
  misled one analysis pass into believing spawned agents were governed by it.
  Its sentinel and error type go with it.

  `AgentIdentity` stays, because it is real: `pkg/agent` `ProductionEnroller`
  populates it from the daemon's registration and token exchange, and the
  daemon's own spawn server authorizes the central's inbound dispatch against its
  `Scopes`. Its doc comment no longer claims provisioning via
  `OrchestratorAuthService.CreateServiceAccount` (which has no non-test callers)
  and now states what it actually is in the control loop: a cost/audit
  attribution record, not an authorization control.

- **`scripts/e2e-remote-agent.sh` and the `e2e-remote-agent` target.** 176
  lines of SSH-and-Docker orchestration hard-wired to one developer's lab:
  a hard-coded host address, a lab bridge interface, and an unrelated project's
  workspace, plus
  OIDC credentials that must already exist on the remote. No CI workflow ran
  it, no documentation mentioned it, and `make integration-test-agents` covers
  the same ground in Docker. Porting it would have produced a Go program nobody
  runs.

  Its removal takes `$(DOCKER_IMAGE)-claude-code-agent:latest` out of
  `DOCKER_LOCAL_IMAGES` with it: that script's `docker build -t` was the sole
  producer of the tag, and `deployments/agents/docker-compose.yml`'s
  `claude-code-agent` service carries a `build:` with no explicit `image:`, so
  Compose names its image after the project directory rather than that. A tag
  no producer names is a tag cleanup must not reach — incident #3.

- The tracked `gen-certs` binary at the repository root
  (`git rm --cached`, now gitignored) and the `test/integration/gen-certs`
  package it was built from.

### Fixed

- **`TestWatcher_Debouncing` asserted a scheduler outcome, not the debouncing
  decision.** It wrote one file five times at 10 ms intervals, drained the
  watcher's event channel for five wall-clock seconds, and asserted the drained
  count was `<= 2`. That count is a function of how many `BatchInterval` ticks
  the writes happened to straddle, so on a loaded runner three or four events
  were a correct result being reported as a failure. Measured on the trunk
  binary at `-count=100` under 32 spinning load processes: **4 failures in 100**
  (`"3" is not <= "2"` ×3, `"4" is not <= "2"` ×1), matching the three CI
  reports on #511, #513 and #520 — none of whose diffs touch `pkg/rag/watcher`.

  The test now removes the periodic flush from the picture entirely
  (`BatchInterval` is set past any plausible runtime, so the ticker never
  fires), confirms each of the five writes has actually reached the debouncer
  before making the next one, then flushes explicitly and asserts the five
  buffered writes emerge as **exactly one** event carrying the newest write.
  Nothing is measured against elapsed time, so there is no count left for the
  scheduler to change.

  Confirming each write before the next is what gives the assertion its teeth:
  it establishes that several distinct file system events really were buffered,
  so a single flushed event can only be coalescing and not the writes having
  gone unnoticed. With the path-keyed coalescing removed from `debouncer.Add`,
  the test fails on that assertion with 6, 9 and 10 events across three runs.

  Watching-behaviour is unchanged; this is a test-only fix.

- **A one-shot CLI that exits successfully having written nothing is no longer
  reported as a completed turn.** An empty stdout buffer fails
  `json.Unmarshal`, so it reached the raw-stdout fallback and produced a turn
  whose single content block held the empty string — byte-for-byte what a CLI
  reporting `{"result":""}` produces. A dead process and an empty answer were
  the same `PromptResponse`, and no caller could tell them apart. Such a run
  now fails with `ErrOneShotNoOutput`.

  The distinction is the one this package already draws for token counts: an
  absence is not a measurement of zero. A result object saying the answer was
  empty is a claim the CLI made; a silent exit is the absence of any claim.
  Whitespace-only output is treated as nothing, so the fail-open cannot return
  one byte away.

  Nothing shipped differently before this: `ParseFinishReport` already rejected
  such a turn downstream. What changes is the diagnosis an operator reads —
  the CLI that produced nothing, rather than the finish schema.

- **A negative token count from an agent CLI no longer sums a turn to zero and
  bills it as nothing.** `TokenUsage`'s dimensions are `int64` decoded straight
  from a subprocess's JSON, and a negative literal decodes without complaint.
  `{"input_tokens":1,"output_tokens":-1}` summed to zero, read as "the CLI
  reported nothing", and the turn was discarded before it reached the cost
  ledger. An account whose turns keep summing to zero looks perpetually
  least-used, so `LeastUsageAccountStrategy` keeps selecting it.

  Negative dimensions are now clamped to zero **where the value enters** —
  `TokenUsage.UnmarshalJSON` for CLI output and the protobuf rebuild for a
  remote daemon's evidence — so one bad dimension can no longer cancel a good
  one, and no reader downstream is handed a value it was not written for.
  `Total`, `Empty` and `Add` are unchanged.

- **A failed account create no longer keeps the scope default pointer it
  claimed.** `accountStore.create` claims a vacant scope's default pointer
  before it writes the account record, and every failure between the two
  returned without giving the pointer back — leaving the scope naming an
  account that was never created. The pointer is now released, guarded by
  `CompareAndDelete` on this call's own account ID, on every exit before the
  record write succeeds.

  **The residue was durable, not transient.** The election heals only a pointer
  whose holder has no record, and the create that fails most often is a
  duplicate ID — whose record exists, in another scope. That is the one state
  the store deliberately refuses to heal, because it cannot be told apart from
  a move in flight. So a scope whose first create failed was left permanently
  without a default: `StaticAccountStrategy` and every unpinned spawn found no
  default account there, and every later create in that scope reported
  `Default=false`.

  Winning an election and claiming a pointer are now separate answers.
  `electDefaultLocked` reports both: a pointer that already named this account
  was written by an earlier call, so a create that fails must leave it exactly
  as it found it. Only a pointer this call wrote is released.

  The write-then-claim ordering is unchanged — reordering would close the same
  window but gives up the "pointer written BEFORE the record" property the
  store is built on, and that decision is not this fix's to make. The sibling
  case where a create with `Default: true` destroys the scope's previous
  default is tracked separately.

- **`OIDCProvider.UserInfo`'s call-site comment no longer describes a username
  fallback the endpoint does not have.** The fallback was deleted in #390
  (`596e6cab`); the doc comment on `userInfoSubject` was updated and the one at
  the call site was not. It went on claiming that a token minted before the
  `uid` claim "falls back to the subject string", and that the fallback was safe
  because it "applies the SAME disjoint-shape rule the session resolver does".
  Neither clause was true.

  Documentation only — no behaviour change. It is recorded as a fix rather than
  a chore because a stale comment describing *behaviour* costs a reader some
  confusion, while one describing a **guard that does not exist** costs them
  their conclusion: anyone auditing `/oauth2/userinfo`, or re-adding a
  resolution fallback, would have believed a disjoint-shape rule already covered
  them.

  The argument now lives on `userInfoSubject` alone and the call site points at
  it. Stating it twice is what allowed one copy to rot.

- **`TokenLiveness`'s doc block no longer describes the deleted subject-guessing
  path as the live behaviour for a token carrying neither claim.** Its
  "neither claim" bullet still routed the reader to "the legacy dual-lookup
  below, with its documented rename residual", while `principalLive` denies such
  a token outright — and while the closing paragraph of that same block already
  said inferring service-principal status from a failed lookup **was a live
  authorization bypass** and had been removed. One comment block asserted both.

  This is the worst of the three, because it sits in the doc block of the
  security decision function and names the lenient path as still running. A
  reader who trusted it and restored that behaviour would have reopened the
  bypass its own final paragraph describes. Re-pointed at `principalLive`, which
  states the rule and why nothing needs the old path back.

- **Three more comments describing the deleted subject-guessing path as live,
  and misstating its consequence in the reassuring direction.** `oidc.go`'s
  refresh-session freeze, `oidc_tokens.go`'s hardware-login refresh mint, and
  the doc and failure message on
  `TestOAuthRefreshTokenRedeemedAtTheSessionEndpointKeepsTheImmutableHandle` all
  said a token minted without the immutable handle would "land on the legacy
  subject rule" — a lenient path that would drain as old tokens aged out.

  It does not. `principalLive` DENIES a token carrying neither `uid` nor a
  service marker, so such a renewal is dead at its next liveness check. Freezing
  `UserID` is therefore load-bearing rather than belt-and-braces, and anyone
  tidying it on the strength of the old wording would have caused silent session
  death instead of a soft landing. The hardware-login copy was the worst: it
  spelled out the full #276 bypass in the present tense, reading as a live known
  hole.

  The past-tense account on `TestHardwareSessionDoesNotOutliveADeletedUser` is
  deliberately left alone — a comment describing what the code once did wrong is
  not stale, and it is the reason that test exists.

- **`PushService` no longer reports an unavailable hub as an empty or missing
  channel.** `NotificationHub.GetChannel` and `ListChannels` carry no error, so
  a hub whose loop is gone answered not-found and an empty list — the same
  answers a healthy hub gives for a name nobody created and for having no
  channels. `PushService` surfaced both through APIs that do carry an error, so
  `ListChannels` returned `([], nil)` for a dead hub, `GetChannel` answered from
  its metadata cache with a nil error, and `Publish` continued on that cache.
  The hub keeps its exported signatures; `PushService` now uses internal
  error-returning queries and reports `ErrHubStopped` (`ErrUnavailable`)
  instead of converting "the hub did not answer" into an answer.

  This state does not require a shutdown. `PushService.Start` runs the hub's
  loop on a context derived from the caller's, so cancelling that context ends
  the loop while `ps.running` stays set, and `PushService.Stop` holds the same
  flag set for up to five seconds while it waits for the loop to leave.

- **The one test guarding #448's role-sentinel distinction could not fail for
  the reason it named.** `TestQRDBRoleStorage_CreateRole` asserted
  `errors.Is(err, ErrRoleAlreadyExists)` under the comment "name collision maps
  to the broker's ErrRoleAlreadyExists sentinel". `QBError.Is` compares the
  **code** alone, so that assertion is equally true for a group conflict, a user
  conflict, or a duplicate role ID — every sentinel in the package carrying
  `ErrAlreadyExists` matches every other.

  Measured: with a name collision mapped to `ErrGroupAlreadyExists` the old
  assertion still passed. It now asserts the error's `Op` (which separates role
  from group and user) and its condition (which separates a duplicate NAME from
  a duplicate ID, since those share both code and `Op`).

  This does **not** make the sentinels matchable — that is a broader change to
  how every consumer tests for "already exists", tracked on #449 — but a test
  standing in for the guarantee must not be blind to it in the meantime.

- **A `429` inside a credential, a byte offset or a token count no longer
  classifies a non-limit failure as a provider rate limit.**
  `matchHTTPStatusLimit` tested `strings.Contains(lowered, "429")` — an
  unanchored substring — plus any context word anywhere in the same buffer, and
  it ran on the **raw** stderr tail rather than the redacted evidence. Agent
  stderr routinely carries the injected credential, so an API key whose body
  contained `429`, next to an ordinary word like "limit", was enough.

  The status must now stand alone as a number. Measured on the unanchored
  version, `4294967296` (2^32) and `4291` both matched; they no longer do.

  **This is not a mislabel.** A provider-limit classification selects a
  different failure response — the turn is backed off, retried, or the account
  rotated — so a misclassified failure was hidden behind a plausible-looking
  limit instead of being surfaced as the fault it was.

  The boundary is written as explicit alphanumeric delimiters rather than `\b`,
  because `\b` treats `-` as a boundary and provider keys are hyphen-segmented:
  `sk-ant-429-xyz` would have satisfied `\b429\b`. Every separator a status code
  legitimately appears beside — space, colon, comma, quote, `=`, `/`, newline,
  parenthesis — still qualifies.

  The context-word requirement is unchanged and still required: the two
  narrowings answer different questions, and neither subsumes the other. A file
  path ending `:429:` still satisfies both, so this closes the credential,
  byte-offset and token-count cases rather than the whole class the context
  words were written for.

- **The module no longer consumes `task:ready` on an issue it will never act
  on.** The mirror claimed the trigger label on EVERY parked item, including
  parks that can never return to the queue — a terminal state, or a spent
  attempt budget. On those, `readmissible` refuses for a reason no later pass
  can change and `armHandBack` has nothing to arm, so the strip repeated on
  every sweep: an operator applied the label, the next poll removed it, and
  nothing else happened. Measured twice on the burn-in node 2026-09-04, on
  `go-truststrap#25` — label applied 13:32:15, gone 13:32:26, item `exhausted`,
  no readmission, no work item, no worktree, no session.

  Ownership of the trigger is now decided by one predicate, `triggerReclaimable`,
  which the arming shares: the module may take the label off only where a
  hand-back on it could ever be taken up. An unresumable park keeps the label,
  which is the `#198` display wart `ValidateTriggerLabel` already accepts for
  exactly that state — a label that reads wrong is recoverable by looking, and a
  consumed hand-back is not.

- **Every trigger decision now reaches the log, naming the issue and the work
  item.** `MirrorReport` carries `TriggerEvent`s for each removal and for each
  trigger deliberately held, and `MirrorReconciler.RunPoll` delivers its report
  to a new `onReport` hook. The polling floor is the only path on a node with no
  webhook route and it discarded that report entirely, so the most consequential
  forge action the module takes was invisible. Removals log at INFO; a held
  trigger is a standing condition and degrades through the existing
  first-or-changed suppression, keyed by work item so two stranded issues do not
  silence each other. The held message names the condition and stops there:
  there is no verb that raises `AttemptBudget`, so telling an operator to raise
  it would be advice the module cannot take.

- **An adoption that cannot be made durable leaves the label alone.** The claim
  path's ordering — create the work item, then remove the trigger — is now
  enforced by a postcondition: `createFromIssue` returns a typed
  `WorkItemNotDurableError` if the store reports success without assigning an
  id, because a record `FindWorkItemByIssue` cannot answer for is not a claim,
  and stripping the label on the strength of it spends the hand-over on nothing.

- **A `429` between hyphens or underscores is no longer read as an HTTP 429.**

  `httpStatusLimitPattern`'s doc comment says it is written as explicit
  delimiters rather than `\b` because `\b` treats `-` as a boundary and
  provider keys are hyphen-segmented — `sk-ant-429-xyz` would satisfy
  `\b429\b`. The character class it actually used, `[^a-z0-9]`, treats `-` as a
  boundary in exactly the same way, so the comment stated an intent the pattern
  did not have (#530).

  The class is now `[^a-z0-9_-]` on both sides. `-` and `_` are identifier
  characters here, not separators: they segment provider keys, request ids,
  trace ids and the snake_case field names in a JSON error body. Every
  separator a status code legitimately appears beside — space, colon, comma,
  quote, `=`, `/`, `.`, newline, parenthesis — still qualifies.

  The context-word requirement is unchanged.

- **A tenant's built-in roles no longer strand on the grant they were seeded
  with.** `admin`, `operator`, `so`, `user`, `auditor` and `custodian` were
  copied from the build's definitions when the tenant was created and never
  touched again, while every ordinary write to a built-in role is refused
  (`SystemRoleImmutableError` from go-qrdb, `ErrSystemRoleImmutable` here). A
  build that renamed or added a permission therefore left every existing
  tenant on the old grant **with no remedy at any privilege level** —
  measured on the burn-in node, where a tenant still carried
  `sdlc:register/update/deregister`, held none of the live `sdlc:` verbs, and
  got a 403 with nothing in the log saying why.

- **A truncated WebAuthn credential id no longer reads as a missing
  credential.** `DELETE .../users/{user_id}/webauthn/credentials/{credential_id}`
  answered `404 no credential <id> is registered` for any id it did not hold,
  including a *fragment* of one the account does hold. A base64url credential
  id has no fixed length, so any prefix whose length is a multiple of four
  decodes cleanly into bytes that match nothing — a copy/paste that dropped
  the tail of a 43-character id was indistinguishable from "that credential is
  already gone", and the only remaining recovery was the bulk delete, which
  takes every working passkey with it. The delete now recognises the case and
  answers `400` (`InvalidArgument` on gRPC) with the new typed
  `iam.WebAuthnCredentialTruncatedIDError`, naming the full id so the next
  command is the correct one. (#574)

- **A published `JobStatus` is now immutable, and sub-job lineage lives on
  the run.** `appendChildJobID` appended to the *published* status's
  `ChildJobIDs` slice in place, from the goroutine that spawned the child,
  while status polls read the same value — and the envelope's snapshot now
  reads it too. `JobRun` owns `ParentJobID` / `ChildJobIDs` / `SpawnedBy` /
  `SpawnedAt` under its own lock; `AddChildJob` and `RecordParent` record
  them and republish a COPY of the status by compare-and-swap, so a
  concurrent state change is retried against rather than lost, and every
  snapshot carries the lineage forward. Caught by the CI race detector.

- The daemon e2e drives the runner again (`cmd/quicbaasd/sdlc_loop_e2e_test.go`,
  `-tags integration`): a labelled issue becomes a Task, the Task becomes an
  `sdlc` Job, and the job reaches a merged pull request through the real store,
  the real git provisioner and the real review gate — asserting exactly one
  worker session, no decomposition, and the issue body in the worker's prompt
  verbatim. Also: a review round on the same session, the trunk-based
  direct-push carve-out, gitflow's hotfix back-merge, and a killed node whose
  successor resumes the same branch and pull request. The scripted backend
  plays the container entrypoint, cloning from the workspace spec the daemon
  mounted — so the test proves the daemon clones nothing.

- **A decomposed job's tasks are created under the job's project, and a
  `project_id` that is not a project id is refused.**

  Two defects on the same identifier (#613).

  `ControlLoop.handleAssign` built its `CreateTaskRequest` with no `ProjectID`
  at all, and `WorkItem.ProjectID` — a field that has always existed — was
  never set by any production path. So every task the engine decomposed a job
  into was created **unscoped**: invisible to the project's task listing,
  uncounted by the project's `MaxConcurrency` quota, and leased under the
  unscoped key segment. The lead is given the project only as decomposition
  *context*, so nothing in its answer was bound to it.

  `ControlLoop.scopeWorkItems` now binds every decomposed item to the project
  the job runs under, for the lead's items and the reviewer's follow-up items
  alike, and `handleAssign` passes it through to the task. The job's project
  **overwrites** whatever the item carries, including a project the agent named
  itself: an agent must not be able to move work into another project by
  mentioning it. A job with no project leaves its items unscoped, because that
  is a real state.

  `CreateTask` copied `req.ProjectID` onto the record unexamined. A project's
  ID is the UUID `CreateProject` mints and its NAME is free text, and nothing
  downstream re-checks which one it was given — a listing filters on the id and
  a quota counts leases under the id's subtree, so both quietly find nothing.
  `CreateTask` now refuses a `project_id` that is set but is not a UUID, with a
  typed `InvalidProjectIDError` naming the value and the field. An empty
  `project_id` is still accepted and still means unscoped.

- **The stall detector reads the runners' declared states instead of keeping
  its own copies of them.**

  `JobStateSpec` has carried `IdleThreshold` and `SessionBacked` since runners
  were introduced, and its doc comment said the stall detector read them. It
  did not. The detector kept three hand-maintained tables —
  `DefaultJobIdleThresholds`, `sessionBackedJobStates` and `WatchedJobStates()`
  — and `KnownJobStates()`, the registry view written for exactly this, had no
  callers anywhere in the repository. A runner declaring a state outside those
  tables, or a bound on one, would have been silently unwatched (#621).

  The engine's bounds now live on `workflowRunner.States()`, where the runner
  that waits is the one that says how long the wait may be, and
  `DefaultJobIdleThresholds()`, `WatchedJobStates()` and the detector's
  session-backed set are all derived from `KnownJobStates()`.

  A state two runners share has one envelope-wide answer, because the detector
  reads `JobStatus.State`. `mergeJobStateSpec` folds the declarations:
  `SessionBacked` is a disjunction, so a state any runner drives with a live
  session stays session-backed and a stalled job there is still ended;
  `IdleThreshold` takes the longest declared bound, because a shorter one would
  judge another runner's job late by a clock that is not its; `Label` takes the
  first non-empty in registry order. A state no runner bounds takes the new
  `DefaultJobIdleThreshold`.

  **No effective threshold changes.** `sdlc`'s eight states keep the bound and
  the session-backing they had, asserted against the pre-change tables verbatim
  by a test in `pkg/server`, which is the only place both runners are
  registered.

  `config.OrchestrationWatchedJobStates` remains a restatement, because
  `pkg/config` cannot read `WatchedJobStates()`: `pkg/orchestration` imports
  `pkg/interfaces` which imports `pkg/config`, so the reverse edge is a
  production import cycle, not merely a test one. Its comment said the latter;
  it now says the former and names the two seams that would close it. The drift
  test that holds the two in step is unchanged.

- **The worker's checkout is actually made now.** The container entrypoint
  contract said the image's entrypoint clones the project and its context
  repositories from the mounted workspace spec — but the ACP spawn clears the
  image entrypoint and runs the agent binary as the container's command
  (`spawnContainerJSONRPC`), so nothing ever ran it. Every worker came up with
  an empty `/workspace`, and the charter's "Your branch is already checked out"
  was false on any real image. The daemon e2e passed only because its scripted
  backend played the entrypoint itself.

  The `sdlc` runner's `initializing` step now drives the clone INSIDE the
  container through `ContainerRuntime.Exec` (`pkg/sdlc/checkout.go`), from the
  same `WorkspaceSpec` it materialized at `/run/quicbaas/workspace/workspace.json`:
  the project into `<checkout_root>/<project name>`, each context repository
  beside it, the planned branch cut from its base or fetched and continued when
  a prior attempt pushed it, and the project's commit author written into the
  checkout. The daemon still touches no filesystem but the empty directory it
  bind-mounts. Authentication is the credential-helper shape `publish.go`
  already uses — the helper reads the forge token out of the mounted file when
  git asks, so no credential value reaches argv, the environment or a log line.
  The step is idempotent, so a resumed attempt adopts the checkout its
  predecessor's container made rather than failing on a non-empty directory.
  Failures are `CheckoutCommandError` (output carried on the field, never in
  the error string), and every command with what it said is recorded on the job
  under the `sdlc:workspace_checkout` audit event.

- **The sdlc signing agent's socket now fits in the kernel's `sun_path`
  field.** It was bound under `Project.Workspace.Path` —
  `<orchestration.projects_root>/<tenant_id>/<project_id>/workspace` — which
  measured 148 bytes on the burn-in node against the 107-byte limit
  `MaxSigningSocketPath` enforces. The budget was `len(projects_root) + 88`, so
  every projects root longer than 19 characters failed, and it failed at
  `initializing` on every delivery. #624's e2e passed only because its temp
  root was short.

  The socket moves to a short, dedicated per-session root:
  `<orchestration.sockets_root>/<12 hex characters>/agent.sock`. The new key
  defaults to `$XDG_RUNTIME_DIR/quicbaas`, or `<storage.data_dir>/s` when
  there is no runtime directory, and it is independent of the projects root —
  a 200-byte projects root no longer moves it at all.

- **A sockets root no socket could be bound under refuses the daemon at
  startup.** The arithmetic is fixed — the session directory and the socket's
  name are constant widths — so `EnsureSigningSocketRoot` measures the
  effective root when the module is built and returns a
  `SigningSocketRootError` naming the root, the longest path it yields and the
  limit. A root that cannot hold a socket safely (world-writable, a symlink,
  not a directory) is refused the same way with a
  `SigningSocketRootUnusableError`. The go-xkms caveats are unchanged: the
  session's own directory must not already exist, and the OS temporary
  directory is still not a candidate parent — it is world-writable, which is
  now a startup refusal rather than a bind-time one.

- **A daemon killed mid-attempt no longer leaks its signing-agent session
  directory into the sockets root.** An attempt that *ends* releases its agent
  — the runner's teardown stops the session on a context detached from the
  attempt's, so a cancelled or failed attempt still unlinks its socket — but a
  process killed between the agent's bind and its release cannot. The sockets
  root outlives the daemon (`$XDG_RUNTIME_DIR/quicbaas`, or
  `<storage.data_dir>/s`), so on a node that kills jobs those 0700 directories
  and their dead sockets accumulated without bound. Nothing was exposed: the
  agent goroutine died with the process, the names are random, and a session
  never adopts an existing directory.

  `EnsureSigningSocketRoot` now sweeps the root when it prepares it, which is
  the one moment the daemon knows the root and has nothing of its own bound
  under it. The removals are reported to the host as a `SigningSocketSweep`
  and logged at INFO with the count; a sweep that cannot be completed — the
  root refusing to be enumerated, or an identified orphan refusing to be
  removed — is a new `SigningSocketSweepError` and a startup failure, for the
  same reason an unusable root is.

  **Liveness is decided by the socket, not by the clock.** An agent's listener
  dies with the process that held it, so a socket that refuses a connection
  belongs to no running attempt. The sweep asks rather than assuming "this
  daemon has only just started, so nothing here can be live": that invariant
  holds for one daemon per root, and the default root is reached by every
  daemon of the same user, so a second one starting must not unlink the
  first's live sockets.

  It removes only what the runner mints — a directory named exactly
  `SigningSocketDirNameLen` hexadecimal characters, holding nothing but
  `agent.sock`, and that socket answering nobody (an empty one is a process
  killed between the agent's mkdir and its bind). A foreign name, a file, a
  symlink, a directory with other contents in it and a directory the daemon
  may not read are all left where they are, and the existing refusals — a
  symlink root, a root writable by group or other, a root past the `sun_path`
  budget — are all evaluated *before* the sweep, so a refused root is left
  exactly as it was found. The socket path budget arithmetic from #628 is
  unchanged.

- **The tenant secret vault is authorized the same way on every transport.**
  PR #679 (#534) moved the eight REST `/api/v1/secrets` routes off go-qrdb's
  `credentials:*` family — which guards qrdb's named-credential store, has no
  tenant namespacing, and is therefore registered `ScopeCross`, system tenant
  only — and onto the `ScopeTenantOnly` `secrets:{read,write,delete,list}`
  family qrdb declares for exactly this consumer. The gRPC and MCP tables were
  left naming `credentials:*` (#682), so the same principal storing the same
  secret was admitted over REST and denied over gRPC, the Unix socket and MCP:
  an authorization outcome that depended on which wire the call took.

  All fourteen `quicbaas.kms.v1.SecretsManagement` RPCs and all thirteen
  `quicbaas_secrets_*` MCP tools now carry the REST family, verb for verb —
  `list` enumerates names, `read` returns secret material or its metadata,
  `write` is every mutation, `delete` is removal. The
  `(qrdb.v1.permission)` annotations in `api/proto/kms.proto`, which qrdb's
  unified-server interceptor enforces, were regenerated from the table by
  `go run ./scripts/protoannotate`. QUIC carries the REST routes over HTTP/3
  and the Unix socket carries gRPC, so neither needed a table of its own.

  Each plane pins itself in its own package, and the gRPC and MCP suites drive
  go-qrdb's real RBAC authorizer to prove the three outcomes the family exists
  to produce: a tenant is admitted in its own vault, refused when it names
  another tenant, and the system tenant is denied. That last one is qrdb's
  deliberate `ScopeTenantOnly` posture; restoring a private system vault needs
  a new upstream scope and is tracked in #689. There is no `credentials:*`
  fallback for system callers — it would reopen the cross-tenant reach #534
  closed.

- **A worker can check out over ssh at all.** The Software Factory worker image
  installed `openssh-keygen` — for `gpg.format=ssh` commit signing — but not
  `openssh-client`, and git has no built-in ssh transport: it execs `ssh`. So
  the first containerized worker runs turned every
  `git clone --origin origin ssh://git@…` into `sh: ssh: not found` and exit
  128 (proven live on the burn-in, #749). The image now installs
  `openssh-client`, and asserts `git --version` and `ssh -V` at BUILD time as
  the uid the spawn runs as, so a missing tool fails the image rather than a
  job in a container nobody can see into.

- **The Software Factory worker can run `make lint` now.** The worker image
  carried git, ssh, make and the Go toolchain but no linter, so a binding whose
  verification commands are a Go project's own — which is all of them — hit
  `make lint` exiting 2 with *"golangci-lint is required but not installed"* on
  a tree that was perfectly clean. `make build` and `make test` passed in the
  same container, so every review round failed on the verification step and
  every Go job escalated to a human.

  `deployments/agents/software-factory-worker/Dockerfile` now installs
  **golangci-lint**, **gosec** and **govulncheck** in the toolchain stage with
  `go install` — the way the CI runner installs the same three — and copies
  them into the runtime stage. They go to `$(go env GOPATH)/bin`, not merely
  onto the `PATH`, because a Go Makefile resolves the linter as
  `$(shell go env GOPATH)/bin/golangci-lint` and `test -x`es it; a binary
  reachable only through `PATH` would have left the target still reporting the
  tool as missing. The image declares `GOPATH` rather than deriving it from a
  `$HOME` the spawn does not promise. gosec and govulncheck are there because
  `make check` (fmt vet lint gosec vuln) and `make security` name them; trivy
  is deliberately not, because it downloads a vulnerability database on every
  run and a sandbox's network policy is the operator's to decide.

  **The versions have one home.** `GOLANGCI_VERSION`, `GOSEC_VERSION` and
  `GOVULNCHECK_VERSION` in the Makefile are now the repository's only copy:
  `make install-tools` installs them, `make build-worker-image` passes them to
  the image as build arguments, and CI reads them (`make print-GOLANGCI_VERSION`)
  instead of repeating them in `ci.yml`. `install-gosec` and
  `install-govulncheck` no longer install `@latest`, which contradicted the
  pins CI already applied. A worker that lints with a version CI does not use
  produces a review verdict the merge then contradicts.

  The image's build-time smoke check — the one that already ran `git --version`
  and `ssh -V` as the uid the spawn runs as — now also runs the three tools and
  asserts the linter at the `GOPATH` path. `make test-worker-image` goes
  further on the built image: it checks the reported version against the pin,
  then lints a vendored module clean **and** requires a finding on a module with
  an unused function, so a linter that silently no-ops fails the target. Both
  run with the container on `--network none`.

- **The job stall detector could not read a single session, so it concluded
  nothing.** `JobStallDetector.addressable` probed `SessionStatus` on the
  sweep's own context. That context comes from the daemon's maintenance
  ticker and carries no principal, while the session service resolves
  tenancy from the context and from nowhere else — so every session-backed
  check answered `orchestration: tenant ID required` and every verdict was
  `session_unknown`. On the burn-in node this repeated every two minutes for
  the whole of a job's `reviewing` wait: the detector was inert in
  production, and no worker was ever concluded gone. `examine` already held
  `JobStatus.TenantID`; it now passes it down and the probe is made under
  the job's own tenant via `ContextWithTenant`, the same way `RecordJobDeath`
  binds one. A run whose status names no tenant is still refused and still
  reports `session_unknown` — the resolution is the job's answer, not a
  default. (`#776`)

- **A containerized session reported `active` after its container died,
  holding the job's lease and its project's only slot forever.** A container
  session has no supervisor: `docker run -i --rm` belongs to the container
  runtime, so there is no `*exec.Cmd` for `procReaper` to `Wait` on, and the
  container's death reaches the daemon only as EOF on a stdio nothing reads
  between prompts. `ACPBackend.Status` inferred liveness from the stream-json
  mode alone and answered `active` indefinitely; the burn-in node sat at
  `pending=1 inflight=1 slots=0` for nine minutes with a dead worker holding
  the slot. `Status` now asks the runtime — `ContainerRuntime.IsRunning` —
  whenever the session records a container. Not running, or a
  `ContainerNotFoundError` because `--rm` already removed it, is `closed`.
  Any other runtime failure is returned as an error rather than guessed at,
  because the stall detector turns a state into a conclusion and a guess in
  the not-live direction ends work that is still running. The session's own
  record still outranks the probe: a concluded or supervised-exited session
  is `closed` without the runtime being asked. (`#774`)

- **A remote daemon's "I no longer hold that session" was indistinguishable
  from "I could not be reached".** The daemon maps its own
  `SessionNotFoundError` onto `codes.NotFound`, and
  `RemoteACPBackend.Status` wrapped every non-OK status in
  `RemoteBackendRPCError`. The stall detector type-asserts for
  `*SessionNotFoundError` to tell a vanished session from an unreadable one,
  so a dead remote worker read as permanently unknown and its job was never
  ended. `Status` now translates `codes.NotFound` back into the package's own
  typed error; every other code stays an RPC error, so an unavailable daemon
  is still never mistaken for a dead agent. (`#774`)

- **A worker turn that changed nothing was charged as a failure of the work,
  and its reply was thrown away.** Measured on the burn-in node: three
  attempts at `go-truststrap#26` (jobs `1d680be2`, `68782d66`, `08628ce5`)
  each ran the identical charter prompt, each returned after 3–12 minutes,
  each then ran `make build / make test / make lint` on an UNCHANGED tree,
  and each failed with `sdlc: task 46dac86c… produced no changes in
  /workspace/go-truststrap`. Every one was charged against the attempt
  budget and the next dispatched with the same prompt, so the task ended
  `failed`, `reason: budget_exhausted`. The job records held four events
  apiece and `memory: {}` — **nothing recorded what the worker said**, so
  nobody could tell whether it had asked a question, cited a rule it may not
  break, or died.

  Three things now happen instead.

  **Every turn is an event.** `sdlc:agent_turn` carries the attempt, the
  review round, the elapsed time, a census of the content blocks the backend
  published (tool use included where the backend publishes it), the stop
  reason, the token accounting, the classification (`outcome=`) for a turn
  that changed nothing, and the reply itself — truncated at
  `TurnReplyLimit` (4096 bytes) with the cut marked, never dropped. The turn
  is classified before its own event is written, so one event answers both
  what the worker did and what the factory concluded — and the cap applies
  only where the reply is written, because the declaration sits at the END
  of a reply and capping first would read a long, careful stop as a worker
  claiming work it never did. It is written on the
  failure path too, so a turn whose prompt errored still leaves an account —
  without an outcome it never earned. On a `NoChangesError` the reply is on the error, which
  is what the task result carries.

  **A turn that changed nothing is classified rather than charged.** The
  charter now requires a worker that stops without finishing to end its
  reply with `SDLC-STOP: <code>` on its own last line, and the runner reads
  that declaration and nothing else — no prose is grepped for a decision
  that costs a task its budget. `question` and `owner-decision` park the
  task `needs_info` with the worker's own text as the issue comment;
  `environment`, and any code this binary does not know, park it `blocked`;
  an empty reply is retried ONCE with the silence stated and then escalated
  naming it. Only a reply that declared no stop — a worker reporting work on
  a tree it did not touch — is still `NoChangesError` and still a failure of
  the work.

  **"Changed nothing" is a question about THIS turn**, so it is answered
  against a baseline commit the runner reads before the worker is prompted:
  a clean tree and a `HEAD` still on that baseline. Asking instead whether
  the branch is ahead of its base — which it permanently is once round one
  has pushed, and which a re-spawned container for a resumed attempt checks
  out — would answer "productive" for every turn after the first, dropping
  the follow-up worker's declaration and sending the stale head it never
  touched to verification and review. That is the same silent loop for the
  commonest shape of all: a task whose pull request already exists.
  `AheadOfBase` still decides what may be published and what an ending
  attempt has worth pushing; it no longer decides what one turn did.

  Each park carries a typed unblock condition (#759) — `owner_ruling` for a
  question or a cited rule, `operator_repair` for a broken environment or a
  worker that said nothing — composed with `ComposeBlockedReason`, so the
  sweep reports who is being waited on instead of passing the task over in
  silence. The worker's reply is the condition's DETAIL and never follows the
  marker: the marker is read with `LastIndex`, so text after it would let a
  worker declare the condition its own task is parked under.

  The park reaches the task through the new `JobRun.Park`, which
  `applyConclusion` honours ahead of the reaper's death, ahead of the fault
  classes and ahead of the budget; without that seam a parked job read to the accounting as an
  ordinary failed attempt and the next one was dispatched. The park rides the
  persisted `JobStatus` rather than only the live run, because a daemon that
  dies between writing the failed status and applying the conclusion leaves
  the lease reaper to rebuild it from that record — stamping it `abandoned`
  and requeueing a task a person was already waiting on, which is the same
  loop surviving a restart. It is recorded at the moment the judgement is
  made rather than in the escalating handler, because `SetState` persists a
  state before that handler runs; and it carries a `Surfaced` flag set only
  after the comment lands, so a park whose surfacing FAILED concludes as
  `parked_<kind>` and the conclusion seam posts it, instead of taking the
  silent path in the one case where nothing else will speak. Every park runs
  through the one helper — the exhausted review (`max_review_rounds`), which
  is the common ending in production, included — and marking one surfaced
  publishes and persists a fresh status rather than mutating the one
  concurrent readers hold; `JobRun.restore` hydrates it back on resume, so a
  restart recovers the judgement instead of erasing it with its first
  snapshot. It concludes the
  task as `runner_parked` — a conclusion reason of its own, rendered
  `OutcomeSilent`, because unlike the operator's explicit `BlockTask` this
  park has already reached the issue: the run's `Blocked` moment posted it
  under the very marker the conclusion seam would use, so a row there would
  put two comments with one marker on the issue. The attempt that
  ran is still charged — a turn that reached the model made a judgement about
  the work, which is what the budget counts — but the task now PARKS instead
  of being re-dispatched, so the rest of the budget is spent on the attempt
  that follows the answer rather than on re-asking the question.

  **The identical prompt is never sent twice.** A no-change turn is carried
  on `Delivery.LastTurn` across attempts and rendered into the next
  attempt's prompt — what the previous worker said, that it changed no file,
  how its stop was classified — which is evidence that worker could not have
  had. Behind it, the prompt's SHA-256 digest is recorded with the turn and a
  run that composes a prompt whose digest matches one that already produced
  no changes refuses to dispatch it and parks with that as the stated reason.

  And the verification commands no longer run on a tree the worker did not
  touch: `executing` routes straight to the classification, recording an
  `sdlc:verification_skipped` event that names the outcome.

- **A task that spent its attempt budget concluded in silence.** Task
  `46dac86c…` (go-truststrap#26) charged all three of its attempts by 04:36 and
  concluded `failed / budget_exhausted`; two hours later the forge issue still
  carried `factory:in-progress`, had no comment, and had had its trigger label
  consumed — from the owner's side, indistinguishable from work in progress.

  The cause is a seam that did not exist. The `Blocked` lifecycle moment
  belongs to a JOB: it runs inside the control loop on a live `JobContext`,
  which is why the escalation on go-truststrap#57 posted and relabelled
  correctly. A TASK's conclusion is decided somewhere else entirely — in
  `orchestration.applyConclusion`, after the run has returned, and on the
  reaper's goroutine when the node that held the run is gone. Every ending
  reached that way (the budget spent, three consecutive environment faults, a
  fault an operator must repair, a reaped job with no attempts left, an
  operator's own `BlockTask`, and the `ReadmitTask` that undoes one) wrote
  nothing to the forge at all.

  `orchestration` now names its conclusions in a closed vocabulary
  (`TaskConclusionReason`, nine members) and announces each one, exactly once,
  to registered `TaskConclusionObserver`s — from `concludeAttempt` (the loop
  and the reaper both) and from the block and readmit verbs. The announcement
  runs on a context detached from the caller's and bounded at two minutes: the
  task is already durable, so it must still reach the issue while the run that
  produced it is torn down, and a forge that will not answer must not hold a
  job goroutine or a reaper sweep open. An observer may not change the
  outcome; a report that fails is logged and nothing else.

  `pkg/sdlc` renders every member from ONE table (`conclusionOutcomes` →
  `outcomeSpecs`), which the `Blocked` moment now reads too, so the escalation
  comment and the exhaustion comment are the same renderer with different rows.
  `TestConclusionOutcomes_CoverEveryReason` fails if the engine names a reason
  this table does not, and `Hooks.TaskConcluded` refuses such a conclusion with
  a typed `UnknownConclusionError` rather than posting nothing — the silence
  is refused at both ends.

  A spent budget now posts `### This task has spent its attempt budget` with
  the attempt accounting, the attempt history and the last attempt's error
  (which is where go-quicbaas#787 puts the worker's final reply), moves the
  issue from `in_progress` to the binding's `escalated` label, and records the
  write as an `outcome_comment` artifact on the Task. A readmit posts
  `### This task has been readmitted` and clears every label the module owns.

  Two judgements are worth stating. The exhausted comment takes the
  **`escalated`** label rather than a new `exhausted` key: `labels:` is
  operator configuration and `ForgeConfig.Label` answers an unknown key with
  the empty string, so a new key would have left every binding already in the
  field applying no label at all. And it says the task **cannot be
  readmitted** — `taskTransitions` has no edge out of `failed`, so offering
  `task readmit` there would be offering a command that is refused; the route
  back is the runbook's re-apply-the-trigger-label.

  A failed attempt that leaves budget behind still concludes nothing and posts
  nothing: the task is requeued and the next attempt starts.

  The one renderer composes with go-quicbaas#784: a park reason carrying a
  typed unblock condition still renders `**Unblocks when:**` and
  `**Cleared by:**`, and still says "nothing here" when a sweep can decide the
  condition — with a spent budget outranking it, because no sweep can return a
  task with no attempts left.

  See [docs/sdlc/outcomes.md](../docs/sdlc/outcomes.md).

- **37 of 44 tenant sessions were `active` with nothing behind them.** On the
  burn-in node `GET /orchestrator/sessions` listed 44 sessions, 37 of them
  `active` with a last activity between hours and six days old and no process,
  container or job behind any of them. An operator could not tell which of the
  37 was real, and the list reported `created_at: 0` for all 44.

  The cause is that a session RECORD is durable and a session PROCESS is not,
  and exactly one route closed the gap: a job that reached its discard hook
  asked `CloseSession` to release what it held. A job that failed, was aborted
  mid-stage or panicked never got there; a job whose node died was concluded by
  the reaper, which knew nothing about sessions; and a session that was live
  when the daemon restarted had its record SKIPPED by
  `LoadPersistedSessions` — deliberately, against a later boot that might
  register its backend — which is the hope that produced the 37.

  Session reconciliation now closes all four routes onto the ONE terminal
  transition `session_conclusion.go` already owns, and every closed record
  carries a typed `SessionRecord.ConclusionReason` written atomically with the
  terminal state (`AgentStore.ConcludeSession`, which refuses to overwrite a
  first ending):

  - **Startup** — `LoadPersistedSessions` asks LOCALITY FIRST. A local
    record's process died with the previous daemon, so one whose backend is no
    longer registered, or that is not persistent, is concluded `node_restart`;
    a persistent one is still restored `resumable`. A REMOTE record names a
    process on another host that this restart did not touch and that is
    reached through the remote resolver, not the local backend table — so it
    is concluded only on positive evidence that its daemon has left (the
    resolver cannot reach the backend and the fleet registry has no
    registration for its pin), and then as `agent_gone`. At boot that registry
    question is presence rather than heartbeat recency: every registration
    predates the restart, so a live peer necessarily looks silent until it
    checks in, and concluding on that would close every remote worker's
    session on every restart.
  - **Every terminal job path** — `finishJob` and the reaper's
    `RecordJobDeath` conclude `job_concluded` for every session still bound to
    the job. The binding is read from the durable records rather than from the
    `JobRun`, so it survives a run that never untracked what it spawned and a
    job whose run object never existed in this process.
  - **Agent unregister** — `RemoveAgent` concludes `agent_gone` for every
    session whose `HostAffinity` names the departing registration.
  - **The maintenance sweep** — `Sweep` now runs `SweepSessions` after the
    reaper and the stall detector, on the same ticker.

  Both the boot recovery and the sweep now enumerate through `TenantLister`,
  the seam the task dispatcher and the task-source poller already use, and
  visit each record under a context scoped to its owner. The durable
  orchestration KV routes every read to the tenant on the CONTEXT, and both
  passes run on contexts that carry none — so the cross-tenant scan they used
  saw the system tenant's records and nothing else, which is why no tenant
  session had ever been restored or reconciled on a routed node. A tenant that
  cannot be listed (a sealed barrier) is logged and skipped rather than ending
  the walk.

  The sweep needs TWO signals and never acts on the clock alone: quiet for
  longer than the longest job idle bound AND either a terminal job or a daemon
  that is unregistered or has not heartbeated within
  `orchestration.heartbeat_timeout` — a setting that was configurable and unread
  until now. Both corroborating reads are fail-closed, so an unreadable job
  record or registry closes nothing.

  Reconciliation never becomes a second owner of an ending. A record this
  process holds live is concluded through the existing compare-and-swap as a
  nudge; a record it does not hold live goes through `concludeAbsentSession` —
  a per-session lock, a re-read under it, and a store write that keeps the
  first attribution. Each conclusion is also recorded on the job: a
  `session_concluded` audit event, and the session dropped from
  `JobStatus.SessionIDs`, so a job never claims to hold a closed session.

  **A conclusion is all of it or none.** The session record is the commit
  point and is written LAST — after the log's terminal transition and after
  the job's tidy-up — so `status == closed` means "complete" by construction.
  The order was the other way round, and the failure was unrepairable: a
  record written closed over a `concludeLog` that then failed was skipped by
  every later pass, because a pass reads the record, sees closed and returns.
  The job-side cleanup moved inside the transition for the same reason —
  after the commit point it was the step no retry could reach — and with it,
  an operator's close and a supervisor's exit observer now owe the job the
  same tidy-up a sweep does. Every step before the commit is idempotent, so a
  resuming pass repeats work rather than duplicating it: one terminal event,
  one audit line, however many attempts it took.

  **Only a genuine absence is vacuously complete.** Because the closed record
  is believed, every step ahead of the commit has to tell "there is nothing to
  do" from "I could not find out". The job tidy-up collapsed every
  `loadJobStatus` failure into the first, so a transient read error or a
  corrupt record let the conclusion walk past it and close the session — and
  the invariant then guaranteed no later pass would revisit the pair, leaving
  the job pointing at a closed session forever. `jobStatusAbsent` now admits
  only a KV not-found as absence; read, unmarshal and unavailable errors are
  returned. The same line is drawn where the session's job is resolved: a
  store that cannot answer is an error, not "no job".

  `CloseSession` can now close a stale record. Previously the only sessions
  that NEEDED closing — a durable record saying `active`, no live entry, no
  backend that would admit to it — were the only ones the close verb refused
  with "not found"; it now concludes them as `operator`, and closing an
  already-concluded session stays an idempotent success.

- **The session list accepted a `status` filter and ignored it.** The SDK has
  exposed `ListSessionsRequest.Status` and the REST transport has sent
  `?status=` since they gained the field, but the REST handler read only
  `backend` and the domain request had nowhere to put it. Invisible while the
  service effectively listed live sessions; once this change merged durable
  records into the listing, a caller asking for ACTIVE sessions was handed
  CLOSED ones back — on the one surface where the leak is read. `Status` is
  now on the domain request, on the proto (field 3) and parsed by REST, gRPC,
  MCP and the embedded transport, and it filters BOTH the live and the durable
  branch. It runs after the durable `closed` verdict, so it never answers for
  a state the listing does not report, and an unrecognised value is refused
  with `ErrSessionStateUnknown` rather than silently matching nothing.

- **The session list reported `created_at: 0`.** `sessionStore` now stamps
  `CreatedAt` at write time when the caller supplies none — backends disagree
  about whether they report a spawn time at all — and `ListSessions` falls back
  to the durable value when a live entry carries none, so no session that
  exists can be listed as created at the epoch. On gRPC, `SessionInfo`'s times
  were built with `time.Unix(nanos, 0)`, reading a nanosecond field as seconds
  and putting the instant tens of billions of years out, past what a protobuf
  timestamp can carry; they now go through `unixNanoTimestamp`, which also
  sends an unset instant as an absent timestamp rather than as 1970.

  Every orchestrator SESSION response goes through the helper, not only the
  list entries: spawn, prompt (both the synchronous turn and the async ack),
  status (including `last_activity`), close and the list envelope all stamped
  `UnixNano` and all converted as seconds. The task and agent converters in
  the same file have the same defect on their own surfaces and are left to
  go-quicbaas#818.

- **`TestSweep_ConcludesAJobWhoseWorkerSessionIsGone` asserted a lease release
  it had not waited for.** A bound job's slot is freed by `concludeAttempt`
  inside `finishJob`, which runs after `JobRun.execute`'s last deferred call
  sets `run.finished` — so reading the lease the instant `awaitFinished`
  returns reads ahead of the release. It passed only because nothing competed
  for the scheduler: adding a single unrelated store read to `Sweep` makes it
  fail one run in ten, reproduced on an untouched base branch. The assertion
  is unchanged and now waits for it (`awaitLeaseReleased`, the same
  timer-and-ticker idiom as `awaitFinished`).

  See [docs/software-factory/engineering.md](../docs/software-factory/engineering.md).

- **An untenanted turn wrote its transcript where nothing could read it, so
  every session the UI opened showed only `system joined`.** Every durable
  orchestration record is keyed by `StorageTenant(ctx)`, which substitutes the
  system tenant for a caller carrying none — an in-process job runner, an
  embedded consumer, a test. The session event log's producers stamped
  `TenantFromContext(ctx)` instead, which for those callers is the empty
  string, and the KV log REFUSES an event with no tenant. The refusal went to a
  log line and the turn carried on, so the session record existed under
  `system` and its conversation existed nowhere: the prompt the SDLC runner
  sent, the worker's reply, its tool use and its summary were all dropped, and
  the only event an operator saw was the `operator_joined` their own attach had
  just caused.

  The tenant is now resolved on `AppendNextSessionEvent` — the documented
  single write path, so a future producer inherits the agreement instead of
  having to remember it — and inside both log implementations' key builders, so
  the key a transcript is stored under always matches the key it is read back
  from. An event that names a tenant explicitly is untouched: a cross-tenant
  append is still a caller bug the log rejects.

- **Cost Tracking showed the workflow runner's iteration budget instead of
  spend.** `#/cost` rendered a gauge reading "0.0% Iterations", "0 iterations
  across 3 jobs" and a per-job breakdown of `0/10` for three sdlc attempts.
  Those are `JobStatus.iteration` and `JobConfig.max_iterations` — the
  *workflow* runner's fields. The sdlc runner does not iterate, so they are
  structurally zero for it, and they were never a cost in the first place.
  Meanwhile the real spend was in the ledger and reached no one.

  The page now reads the ledger: the tenant's roll-up with a drill-down along
  the chain, tokens in and out, the accounts that paid and each account's live
  quota window. The job detail **Cost** tab reads that job's ledger entries —
  it previously looked for `token_usage` in job memory, which no production
  writer sets, so it said "No cost data available for this job" for every job
  that had ever run. The iteration gauge renders only for `runner=workflow`
  (`isWorkflowJob`, the job page's own rule from #796); for any other runner
  the panel says so in place, naming the runner.

- **The job page's Cost tab is offered for every runner again.** #796 hid it
  for the sdlc runner, correctly at the time: the tab then held nothing but
  the workflow runner's iteration gauge and the job-memory token panel above.
  Once it reads the ledger the reason evaporates — every runner burns provider
  tokens — and keeping it hidden left an sdlc job's real spend unreachable on
  its own page, which is exactly what #794 measured. Only the GAUGE is
  runner-gated now; the **DAG** tab remains the workflow runner's.

- **Three absences the UI used to render as zeroes.** The rules live in
  `cmd/quicbaas/frontend/src/lib/cost.ts` so they are tested rather than
  re-derived in a template:
  - **cost** is a dash and "no price sheet — cost unmeasured", never `0.00`.
    Nothing prices a turn (go-quicbaas#791), so `cost_micros` is UNMEASURED and
    the server says so in a sibling `cost_measured` flag a renderer must
    consult. The flag is DERIVED from the data, so the price source can land
    without every reader changing.
  - **an account with no cap** reads "No spend policy", which is a fact, and
    an account the server could not read reads "Spend policy unknown", which
    is a different one. Collapsing them would let an unwired deployment report
    every account as uncapped.
  - **a deployment recording no consumption** answers `503` / `Unavailable`
    and the page says so, rather than rendering an empty report: "nothing is
    being recorded" and "nothing has been spent" are opposite facts and only
    one of them is reassuring.
  - **a moment that never happened is omitted, not sent as the year 1.** Every
    optional instant on the report — `first_at`, `last_at`, a window's
    `frees_at` and the breaker's timestamps — is a pointer, because
    `omitempty` does NOT omit a zero `time.Time`: for a struct field
    `encoding/json` treats nothing as empty. A non-pointer field always
    serialises, so an absent moment would have crossed as
    `0001-01-01T00:00:00Z` on REST, on MCP, and on the desktop binding (which
    re-marshals the SDK type straight to the frontend) — a real date a
    renderer draws as January of year 1, which is this feature's own rule
    broken by the encoder rather than by the arithmetic. The gRPC projection
    already got this right; the JSON transports did not. Guarded by tests that
    read the encoded JSON, because the struct cannot tell the two encodings
    apart.
  - **an account store that cannot answer** fails the report, while an account
    that is merely GONE does not. The ledger is append-only and outlives the
    accounts it names, so a deleted account keeps its spend with its allowance
    marked unknown; a KV fault, by contrast, would otherwise have made every
    account read "spend policy unknown" beside healthy token totals drawn from
    the separate ledger store — a fault rendered as a fact, and inconsistent
    with the ledger's own rule that a subtree it cannot scan fails the call.

- **The job page was the workflow runner's page, for every runner.** An
  `sdlc` job — one attempt at one issue, in one container, with no lead agent
  and no DAG — rendered `Workflow —`, `Agent —`, an iteration gauge reading
  `0/10` and `Tasks 0%`, and rendered none of what it does have. Attempt 3 of
  go-truststrap#26 said nothing about being attempt 3, about which container
  did the work, about the run ending with `produced no changes`, or about
  where the branch was; a merged run linked neither its pull request nor its
  review rounds.

  The page is now built **by runner**. An sdlc job gets the attempt and its
  budget, the agent (linked into the fleet), the session, a stage timeline
  with per-moment durations, the delivery with its pull request and every
  review round, the verification output, the typed outcome, and the worker's
  last turn when the daemon recorded one. The iteration gauge, task
  percentage, DAG tab and cost tab are shown only for `runner=workflow`. A
  record naming no runner is a workflow job, which is the engine's own reading
  of an empty `JobStatus.Runner`.

  The panel decisions are pure functions in
  `cmd/quicbaas/frontend/src/lib/jobDetail.ts`, so they are tested without
  mounting anything and the component stays a rendering of them.

  The worker's turn is split before it is shown. `pkg/sdlc` records each turn
  as `job.Audit("sdlc:agent_turn", turn.Describe())` (#787), and `Describe`
  writes a header of `key=value` counts, a blank line, then the reply. The
  page splits at the FIRST blank line — the reply has blank lines of its own
  — and renders the counts as metrics beside the title rather than inside the
  box labelled as the worker's words: `tokens_in=1843` is the daemon's
  measurement, not something the agent said. A turn whose reply is `(the
  worker answered with no text)` is named as **silent**, which is the
  condition three attempts at go-truststrap#26 died of while the page said
  nothing at all. The producer is read opportunistically: no event, no panel.
  `TERMINAL_REASONS` covers the whole closed conclusion vocabulary as a map,
  asserted against `TaskConclusionReasons()`, so a reason added on the server
  and forgotten in the UI fails a test instead of rendering a raw wire string.

- **Retry was permanently disabled and keyed off the wrong thing.** The button
  appeared for `state === 'failed'` and was hard-coded `disabled` with "not
  yet available in this version". It is now **Readmit**, it returns the TASK
  to the queue, and it is enabled exactly when the server says the task may be
  readmitted: `abandoned` parks and may be, `budget_exhausted` fails and is
  terminal, and both are "the attempts ran out", so nothing in the UI can
  derive it. `readmitTask` is wired across the TypeScript SDK's transports.

- **The detail page was populated from the job LIST, which carries no
  provenance.** `ListJobs` returns the raw persisted statuses; only
  `GetJobStatus` applies the task enrichment. Opening a job from the list — or
  by direct route — therefore rendered no attempts, no delivery, no review
  rounds, no terminal reason, no verification and a dead Readmit button: every
  panel this change adds, on the most common path into it. The page now paints
  the list row as a placeholder and replaces it with the record read by id.

- **Readmit threw in desktop mode.** The TypeScript Wails transport called
  `OrchestratorBinding.ReadmitTask`, which did not exist. The Go binding is
  added, taking the SDK request struct like every other task verb on it.

- **A finished job reported its last stage as "(still open)".** The timeline
  marked the final span open merely because nothing followed it — but nothing
  follows it precisely because the job stopped there. Only a job that has not
  reached a terminal state can still be inside a moment.

- **The delivery shown was the task's latest, not the attempt's own.** Task
  artifacts are stored once per name and replaced on resubmission, while a
  retried task keeps one record; published under a flat name, attempt 3's
  pull request overwrote attempt 1's and attempt 1's page then showed work it
  never did. Delivery artifacts are now scoped to the attempt that published
  them (`pull_request@a3`), so every attempt's delivery survives and each job
  reads its own. The per-task page groups them by base name and shows all.

- **The review gate bypassed the verification recorder.** The sdlc runner
  still called `Validators().RunPipeline` directly for the forge review, and
  only `JobRun.Verify` records results — so a review refusal, and the
  redelegation it causes, rendered an empty verification list even though a
  validator had run and answered. Every runner path goes through `Verify`.

- **The desktop binding dropped every new field.** `OrchestratorBinding`'s
  `GetJobStatus` hand-built a ten-key map, so `runner`, `task_id`,
  `agent_id`, `stages`, `attempts`, `attempt_budget`, `terminal_reason`,
  `readmit_allowed`, `delivery` and `verification` never reached the desktop
  UI — which reloads through `GetJobStatus` for precisely those. It returns
  the SDK status now; a projection is a second wire format that nothing
  tests. The Go SDK's `JobStatus` and its gRPC and embedded converters carry
  the fields too.

- **Long review rounds were silently dropped.** The task store offloads an
  inline artifact past its size limit to a `kv_key` and clears the content,
  so the job page decoded the record and found nothing — showing every short
  review and losing every long one, which are the findings worth reading. The
  reviews artifact is now dereferenced through the store before decoding, and
  an unresolved reference is a hard error rather than an absence.

- **`ReadmitTask` was unusable over MCP.** It was registered as a tool but
  missing from the transport's result-transform table, so a SUCCESSFUL
  readmit fell through as the raw tool map and the client refused it as an
  invalid response type. `BlockTask` had the identical gap and is fixed with
  it; a test now asserts every task-lifecycle tool has a decoder, which is
  what found the second one.

- **The project dialog promised a clear it cannot perform.** The server's
  update is presence-based and offers explicit clear flags only for the
  description, the default workflow and the context repositories, so emptying
  any other box sends nothing and the stored value stays. The SSH access-key
  help text nonetheless invited leaving it empty to fall back to the signing
  key — true on create, false on edit — so an operator could clear a stale
  key, save without error, and keep using the old one. The edit dialog now
  states the rule once, at the top, and the SSH help text tells the truth per
  mode. Adding `clear_git_*` end to end is an API change across every
  transport and the store, which is not this change's to make; what is this
  change's is to stop the UI promising it.

- **Saving any project edit in the browser failed with 400.** The TypeScript
  `updateProject` sent `PUT /api/v1/projects/:id` with no `tenant_id` query
  parameter, and the handler derives the tenant from the query and refuses
  before it reads the body — so the SPA's edit dialog could not save at all,
  which also meant the #797 hydration fix could not be exercised. The verb now
  carries the tenant on the URL like its siblings, with a path test.

- **A capped project could not be set to unlimited.** A quota of 0 means
  unlimited on the project record, in the dialog and in the docs, but the
  update applied a quota only when it was non-zero — so an operator could lift
  a cap, watch the save succeed, and stay capped. The three quotas are now
  presence-based end to end (proto `optional`, pointers through the REST
  request, service, both SDKs and the CLI's flag-changed check), so omitting
  one leaves it alone and sending `0` lifts the cap.

- **No page ever linked a delivered pull request.** The frontend filtered task
  artifacts on `kind === 'pull_request'`, a kind the engine's own artifact
  validator refuses — the runner publishes `{kind: 'url', name:
  'pull_request'}` — so the filter matched nothing that could exist. It now
  matches on the published NAME, and the job and task fixtures carry the bytes
  the runner actually writes.

  Closes #796.

- **The project edit dialog opened with every Git setting blank.** Editing an
  existing project showed no repository URL, no default branch, no commit
  author and no signing key, on every project that had them. The form hydrated
  from `project.git?.repo_url` — and `project.git?.author?.name`,
  `project.git?.signing?.key?.scope`, and the rest — a nested member the API
  has never produced: `pkg/api/rest.ProjectRESTResponse` is flat
  (`git_repo_url`, `git_author_name`, `git_signing_key_scope`, …) and is what
  every transport serializes. The write path was already correct, which is why
  nothing failed loudly: a save just carried the operator's re-typing.

  The TypeScript SDK's `Project` type declared the nested mirror, so the form
  was type-correct against a shape that does not exist. **One shape, and it is
  the one on the wire**: `Project` is now flat and matches
  `ProjectRESTResponse` field for field. The nested-only helper types
  (`ProjectGit`, `ProjectWorkspace`, `GitAuthor`, `KMSKeyRef`,
  `CommitSigning`, `ProjectPermissions`, `ProjectQuotas`) are deleted rather
  than left as a second vocabulary for the same record.

  The same nesting was wrong for the other members and is fixed with it: the
  workspace path (project list row, edit dialog, spawn dialog) read
  `workspace.path`, the quotas read `quotas.token_budget`, and the access
  matrix read `permissions.agent_roles`, all against members the API does not
  send.

  `projectForm.test.ts` now hydrates from a verbatim `GET
  /api/v1/projects/{id}` body and asserts every git control is populated, plus
  a round trip proving that loading and saving an untouched project sends
  nothing but the two identifiers. Hydrating from a hand-written object is
  what let this live: the fixture obligingly had the nested member the server
  never sends.

  Closes #797.

- CI: the peer-review workflow no longer dies before checkout on a pull request that carries no labels. Gitea renders the label list as JSON `null`, and the reviewer-resolution step iterated it with `jq '.[]'`; the list is now normalised to `[]` once at the top of the step (#812; closes the duplicates #786 and #798).

- **A task page said "no agent has matched this task yet" about a task that
  had dispatched three times, run three jobs and ended `failed`.** Measured on
  the burn-in node at `#/tasks/8bf2fb5f…`: the header counted `Charged /
  budget 3` and `Dispatched 3`, and every line under it was false —
  `JOBS (0)`, `PULL REQUESTS (0)`. Three separate defects produced one page.

- **`GET /api/v1/orchestrator/jobs?task_id=<id>` accepted the narrowing and
  ignored it.** The handler read only `tenant_id`, and
  `AgentService.ListJobs` had no filter to pass it to. Measured: asking for
  task `8bf2fb5f…`'s jobs returned seven jobs belonging to tasks `13d36256…`
  and `f1155264…`. Any client that trusted the query got another task's
  attempts and no way to tell.

  `ListJobs` now takes a `*JobFilter`, matching `ListTasks`'s shape, and
  applies `TaskID` against the job's own binding (falling back to the
  launching config for a record persisted before the binding moved onto the
  status). Wired on every transport — REST `?task_id=`, gRPC
  `OrchestratorListJobsRequest.task_id`, MCP
  `quicbaas_orchestrator_list_jobs{task_id}`, Unix via gRPC, QUIC via REST,
  the in-process embedded transport, the Go SDK `ListJobsRequest.TaskID`, the
  TypeScript SDK `listJobs(tenantID, taskID)`, the Wails binding and
  `quicbaas orchestrator job list --task`. A nil or empty filter still lists
  every job: *no narrowing asked for* is not *narrow to nothing*.

- **Concluded jobs were durable and unlistable: the writer filed them where
  the listing did not look.** A job record's tenant lives in its KEY
  (`job:{tenant}:{id}`), but the store beneath the orchestrator routes by the
  tenant on the operation's CONTEXT. `persistJobStatus`, `loadJobStatus` and
  the restart scan all ran on a bare context; `ListJobs` scanned on the
  REQUEST's. On a barrier-isolated tenant those are two different backends, so
  the tenant listing returned only the jobs still in memory — seven, all
  started after the daemon's current boot — while `GET
  /orchestrator/jobs/{id}` happily answered for a record two days older from
  the same store. Every task older than the process rendered its attempt
  history as `JOBS (0)`, permanently. Same class as #820.

  All four job-record paths are now deliberately untenanted, documented where
  they are declared and in
  [job-admission.md](../docs/software-factory/job-admission.md#the-attempt-history-a-task-can-be-read-back-through).
  It has to be untenanted rather than tenanted: the restart scan enumerates
  every node's interrupted jobs before any request exists, and the reaper's
  death record is written for a tenant whose barrier may by then be sealed —
  a subsystem whose purpose is to record what happened cannot fail closed on a
  seal. Isolation is the key prefix, which a listing is still bounded by. A
  record written with no tenant now files under the system tenant, where a
  listing can name it, instead of at an empty key segment.

  **The four share the tenant detachment but NOT a timeout**, which is why
  there are two helpers rather than one. `jobRecordOp` bounds a single record
  read or write at 5 s, which those two operations have always carried;
  `jobRecordScope` bounds the two SCANS by their caller and nothing else, as
  they have always been. A fallback ceiling on the scans would make a
  deadline-less REST listing inherit it as the only bound in the chain, and a
  tenant with thousands of concluded records — or a store whose prefix scan is
  simply slower — would have its attempt history truncated with the scan error
  only logged: the same symptom, through a different door. The boot budget is
  therefore still `Server.ShutdownTimeout` end to end, including the
  synchronous project lookup a resumed job performs. Cancellation is forwarded
  in both, so a listing stops when the request it serves is abandoned.

- **`GET /orchestrator/jobs` and `GET /orchestrator/jobs/{id}` read the tenant
  straight off the query string.** They were the last two orchestrator routes
  that did not go through `resolveOrchestratorTenant`, so a caller that
  omitted `?tenant_id=` was refused with "tenant ID required" instead of being
  served its own tenant, and the system-tenant fallback every sibling route
  applies did not apply to them. Both now derive the tenant the same way as
  the rest of the surface: explicit `?tenant_id=` (the documented
  control-plane override), then the authenticated token's `tenant_id` claim,
  then the system tenant.

  What guards cross-tenant reach is unchanged and is NOT the derivation: the
  query parameter still wins by design, and a caller naming another tenant is
  stopped by the route's `orchestrator:*` permission and by the orchestrator's
  own `validateTenant` / `TenantGuard.ValidateTenantAccess`. A deployment that
  installs no guard has no cross-tenant check on these routes, exactly as it
  has none on every other orchestrator route.

- **The task page's empty panels asserted things that were not true.**
  "Dispatch requested; no agent has matched this task yet" is a claim about a
  task that has never dispatched, and it was rendered for every empty Jobs
  panel. The panel now distinguishes the facts an operator needs apart:
  nothing has run, something ran and its records could not be read, the read
  is still in flight, and a settled task that never dispatched — the second of
  which names the attempt count and says plainly that it is a gap in what the
  page can read, not a statement about the work. The pull-request panel
  likewise separates "no attempt has run", "ended `failed` before publishing
  (3 attempts ran)", "completed without publishing" and "not published yet".

  The page also reads its attempts from the server, narrowed to its own task
  (`loadTaskJobs`), instead of sieving the tenant-wide job list — which is
  what made a task's history depend on which jobs the list happened to hold.

- **`ListReviews` was unpaginated.** It returned Gitea's first page and
  treated it as the whole verdict history. The gate reduces reviews to each
  reviewer's verdict on the *current* head, and the current head's verdicts
  are the newest — the far end of the listing — so a long-running pull request
  silently lost exactly the reviews that decide the merge: the approval that
  would clear the gate went missing and the change waited on a verdict already
  given. It now pages until a short page, and a forge that never sends one is
  reported as a fault rather than returned as a prefix.

- **A draft pull request could clear the merge gate.** `EvaluateReviewGate`
  had no notion of a draft, so a fully approved, green draft was reported
  mergeable — an approval overruling the one person who had said the work was
  unfinished. It is now refused with `GateBlockedPullRequestDraft`, decided
  before any verdict is read so the answer cannot depend on who reviewed
  first, and reported as pending rather than as a refusal because marking a
  pull request ready is what ordinarily happens next.

- **`sdlc.webhook.public_url` now refuses a query string, a fragment and
  userinfo.** The webhook route is appended to that value textually, so
  `https://factory.example.com?x=1` — which parses, carries a scheme and
  carries a host, and passed every check — produced
  `https://factory.example.com?x=1/api/v1/sdlc/webhook/<project>`: a delivery
  URL whose path is `/` with the route buried in a query string. A fragment
  swallowed the route the same way, and userinfo put a credential in a URL the
  forge stores and displays.

  None of it failed loudly, which is what made it worth fixing before a fleet
  rollout rather than after: the hook was provisioned, `EnsureWebhook` read it
  back and compared the same malformed string and agreed, and conformance
  reported the repository healthy while every delivery missed the endpoint —
  on every repository at once, since the origin is a property of the node. The
  refusal is now at load and again in `NewOnboarder`, so an embedder that never
  went through the loader gets the same answer.

  A **path is still allowed**, for a node mounted behind a reverse proxy:
  `https://factory.example.com/quicbaas` appends correctly. The rule is stated
  once per side — `config.ValidateSDLCWebhookPublicURL` and
  `sdlc.NormalizeWebhookPublicURL`, because `pkg/config` cannot import
  `pkg/sdlc` — and a drift test walks one table through both, since the looser
  side silently winning is exactly the failure mode above.

- **A verdict that applies no label is no longer re-reached on every sweep.**
  Candidate selection read only labels, and two of the three verdicts apply
  none — not-actionable and duplicate comment and leave the issue alone by
  design. The next periodic pass therefore saw a bare open issue, spent a model
  turn on it and posted the same verdict again, every sweep, for ever, on every
  repository in the fleet. That is the exact pathology the runner exists to
  remove: six issues once described one stale README with nothing detecting the
  duplication.

  The record of a verdict is triage's own tagged comment, so the rule is now
  **who spoke last**: nobody → judged, triage → left alone, anybody else →
  judged again. It is deliberately not a comparison against the issue's
  `Updated` timestamp, because needs-info writes a comment and then a label and
  so advances `Updated` past its own question — a timestamp rule reads triage's
  second write as the reporter's answer. The thread is now read once per issue
  and shared with the prompt, which is also one fewer forge call per issue.

- **The repository the charter promises is now actually cloned.** The charter
  tells the agent it has "a read-only checkout of the project's repository at
  its default branch", and the first question it is asked to answer is whether
  the change is already on that branch. Nothing made that checkout: the daemon
  does not, and the spawn clears the image's entrypoint, so the agent received
  an empty directory and every already-done and duplicate judgement was made
  from issue text alone. The runner now drives the clone inside the container
  through the same seam the delivery and review runners use, with the git
  output on the job as evidence.

- **An unsandboxed triage session is refused.** Anybody who can open an issue
  writes the body triage is about to read, so a spawn that returns no container
  id is refused before the first prompt rather than reading untrusted text on
  the daemon. The reviewer already made this refusal about a candidate change.

- **The session is closed with a live context.** Every unplanned exit —
  shutdown, supersession, stall abort — reaches the deferred teardown with the
  context already cancelled, and the close inherited it: the request failed at
  once and left the agent session and its container running with nothing left
  holding the id. It now uses `context.WithoutCancel`, as the delivery and
  review teardowns do. The package's agent double now honours the context on
  close, so the difference is observable in a test instead of silently fine.

- **A per-issue forge read failure is recorded rather than swallowed.** An
  issue whose discussion could not be listed is dropped from the sweep, which
  is right, but it was dropped silently — a project whose forge refuses
  listings looked triaged and was not. It now reaches the audit trail and the
  job's event stream as `sdlc:triage_failed`, naming the issue.

- **The triage session is published on job snapshots.** `executing` is declared
  `SessionBacked`, but the runner held the session only privately, so the stall
  detector saw a session-backed state with nothing addressable in it and would
  abort a healthy container instead of addressing it.

- **A spawned agent's per-session certificate was minted but never enrolled, so
  it carried no permissions.** `AgentMCPPresetResolver.Resolve` already minted a
  fresh ephemeral mTLS keypair per spawned session (`CN=agent-<sessionID>`,
  `O=<tenant>`, `OU=agents`, one-day validity) and delivered cert/key/CA to the
  agent by environment variable. Nothing enrolled it. go-qrdb resolves an mTLS
  principal's roles by looking the certificate's Subject CommonName up in the
  user store (`pkg/auth/middleware` `finalizeIdentity`) and proceeds "with base
  identity (no enrichment)" — **empty roles** — when the lookup misses; the MCP
  surface is default-deny. So every spawned agent completed its handshake and was
  then refused every tool except the five public ones. The resolver's own doc
  comment claimed `pkg/api/mcp/authz_agent_cert.go` recognised the certificate;
  that file never existed.

  Resolve now enrolls the certificate as a **session-scoped qrdb principal** and
  the cleanup the ACP backend fires on session Close **revokes** it. The
  enrollment reuses the mechanism registration already proved for agent daemons
  (`IdentityStore.EnrollAgentCertIdentity`, go-quicbaas#663) rather than adding a
  second one: that function was generalised over the role it converges, and
  `EnrollAgentSessionIdentity` / `RevokeAgentSessionIdentity` are the session half
  of the same seam. Neither the role nor the grant is caller-supplied — a
  session's authority is a property of the build, not of whoever spawned it.

  The principal holds `iam.AgentSessionGrant()`: `orchestrator:inference`,
  `kv:get`, `kv:put`, `kv:list` — the floor `orchestration.AgentMCPScopes()` had
  only declared, now load-bearing and pinned against it. It holds nothing else,
  and the absences are asserted rather than assumed: no `kv:delete`, no secrets,
  KMS, IAM, barrier, seal, ceremony, cluster or tenant verb, and no orchestrator
  control-plane verb beyond inference. A session's role is disjoint from the
  daemon's, so neither enrollment is a route to the other's authority.

  Revocation **deletes** the record rather than disabling it. qrdb never consults
  the certificate binding on this path, so the record's absence is what actually
  revokes; a disabled record would also deny but would linger one-per-session for
  the life of the tenant, and re-enrollment refuses to converge an out-of-service
  record, which would turn a CommonName collision into an unspawnable session.
  It is idempotent, and it refuses to delete anything that is not one of this
  build's session principals — without that guard a session whose CN collided
  with a human account or an agent daemon would delete that account on close.

- **An agent certificate carried no tenant binding, so even an enrolled principal
  would have been unreachable.** go-qrdb derives an mTLS identity's tenant from
  the certificate's tenant-scoped SPIFFE SAN and falls back to the first Subject
  OU when there is none. Every agent certificate carries `OU=agents`, so a
  certificate issued without the SAN authenticates into a phantom tenant
  literally named "agents" — where no enrollment in the real tenant can ever be
  found. `XKMSAgentCertIssuer` never stamped the SAN.

  `AgentCertificateRequest` now carries `SPIFFETrustDomain`, and the issuer
  forwards it to the CA, which owns the single synthesis site. The session
  resolver requires it (`ErrMCPPresetResolverNoTrustDomain`) and enrolls into the
  tenant it reads back **off the minted certificate**, refusing to proceed unless
  that is the tenant the spawn named. There is deliberately no synthesized
  default: first boot already refuses to issue the admin certificate without
  `iam.spiffe_trust_domain` (`ErrNoSPIFFETrustDomain`) for the stated reason that
  a default would bake a fake pinned security identifier into every certificate.
  A deployment without one now gets no MCP preset and a log line saying why,
  instead of a credential that authenticates and holds nothing.

- **`Security().DynamicSecrets()` was nil in every deployment, and now is not.**
  `pkg/service` declared `dynamicSecretsService`, `DynamicSecrets()` returned it,
  and nothing ever assigned it — `NewDynamicSecretsHandler` and
  `lease.NewManager` had no non-test callers at all, so all eight verbs reported
  the capability as unsupported on all five transports. `Server.startDynamicSecrets`
  now composes it and attaches it to the facade, after the service container and
  before orchestration.

  The blocker was storage, and it was the wrong storage. `pkg/lease` and the
  dynamic-secrets handler depended on `pkg/storage.Storage` — a LOCAL engine
  interface (batches, snapshots, bidirectional iterators) that a BaaS-mode server
  never instantiates, which is what the old "no local pebble storage" comment in
  `lifecycle.go` was recording. Both now consume `interfaces.KVStore`, the one
  contract every durable quicbaas component already persists through, satisfied
  in production by the embedded qrdb node: a lease is durable, replicated,
  barrier-encrypted at rest and tenant-routed, and revocation survives a restart.
  A 250-line hand-written `storage.Storage` mock and three more fault doubles are
  deleted in favour of `kvstore.NewMemory()` plus one fault injector.

- **`RevokeLease` now reaches the thing that was leased.** It marked the lease
  and stopped: the database user, cloud role or whatever else the generator had
  created stayed live and usable forever, and
  `credentials.CredentialGenerator.Revoke` — the SPI verb that exists precisely
  to undo a `Generate` — had NO caller anywhere in the product. A lease whose
  revocation does not reach the thing it leases is not a control.

  The generator is asked to destroy the credential FIRST, and the lease is marked
  only after it succeeds. The order is what makes a failure repairable: a revoked
  lease is terminal and nothing revisits it, so marking it first and then failing
  to drop the login would leave live material behind a record saying it is gone.
  Now a generator failure leaves the lease ACTIVE, the caller sees the refusal,
  and the retry converges. The teardown is driven off the lease's own metadata
  (`credential_id`, `generator`), so the lease stays the single handle a caller
  needs and no second index is introduced. A lease with no credential metadata (a
  delivery grant), a record already gone, and a generator whose plugin has been
  dropped are all tolerated; the generator refusing is not.

- **A revoked lease is now a tombstone rather than a deletion, so
  `LeaseStateRevoked` is reachable in production.** Revocation deleted the
  record, which made a revoked lease indistinguishable from one that never
  existed and left nothing to audit. `RevokeLease` marks `RevokedAt` and
  persists; the new `Manager.Validate` refuses with `LeaseRevokedError`,
  `LeaseExpiredError` or `LeaseNotFoundError` as the case is. Expiry is answered
  from the record's own `ExpiresAt`, never from the sweep — a lease that lapsed a
  millisecond ago is refused before any pass has visited it. `ReapExpired`
  converts lapsed leases into tombstones and purges tombstones past
  `TombstoneRetention` (24h), and is exported and synchronous so the sweep is
  testable as a function rather than as a race against a ticker.

  `StartExpirationLoop` also now honours `ManagerConfig.ExpirationCheckInterval`,
  which the loop had been ignoring in favour of the package default.

- **Ten autonomous projects each permitted three concurrent jobs ran thirty of
  them on a fleet with eight worker slots.** Concurrency was bounded per project
  only — `Project.Quotas.MaxConcurrency`, checked independently by the
  task-source poller and the task dispatcher, neither of which knew anything
  outside its own project. Observed on the burn-in fleet, 2026-09-12. Admission
  is now decided in ONE place (`pkg/orchestration/admission.go`) from three
  inputs: the project's own quota (unchanged, and still a hard ceiling), the
  **fleet's worker capacity**, and the **provider pool's usage headroom**. Both
  sweeps consult one plan per sweep instead of each doing its own arithmetic.

  The fleet's capacity is measured from the fleet: every daemon reports
  `max_concurrent_jobs` on its heartbeat and the capacity is the sum over the
  daemons alive at that instant, so it shrinks when a host is drained and grows
  when one is added with nothing to keep in step by hand. An operator may also
  declare a ceiling below what the hardware could serve
  (`orchestration.fleet.max_concurrent_jobs`); when both answer, the tighter one
  binds. **An unmeasured value is not zero**: a fleet nobody could measure — an
  empty registry, or one daemon short of a report — leaves the axis out entirely
  rather than reading as "no slots", because that reading refuses every job in
  the deployment while wearing the costume of a safety bound.

  The provider axis is **subscription usage, never a price.** No turn has a cost
  and `ConsumptionEntry.CostMicros` has no production writer, so a money axis
  summing zeroes would admit forever while looking like a cap. What admission
  reads are the two authorities the spawn path already consults — the account's
  `Selectable` lifecycle/cooldown state and the quota authority's sliding token
  window — asked one step earlier. A project whose whole pool is spent is
  **deferred** with a `RetryAt` rather than spawning an agent whose first turn
  the provider will refuse.

  The fleet's free slots are divided by **max-min fairness with a least-served
  residual**, because a fleet-wide cap handed out first-come would let one busy
  project hold every slot indefinitely — strictly worse than the per-project
  behaviour it replaces. The guarantee: shares never sum above the free slots and
  never exceed a project's own unused quota; a project asking for no more than an
  equal share gets everything it asked for; and every asking project is served
  within ⌈P/F⌉ sweeps of P projects sharing F free slots.

  Both lease decorators now exist, mirroring each other at the one point every
  job passes through: `projectCapacityJobLeaseStore` refuses over a project's
  quota, `fleetCapacityJobLeaseStore` over the fleet's capacity, with a typed
  `FleetCapacityExceededError` naming the source — because a project refusal and
  a fleet refusal send an operator to different numbers.

- **An issue was eligible on one test, and the cheapest failures were discovered
  with tokens.** `IssueSource.Poll` asked only whether an issue was open and
  carried the trigger label. So an issue could be adopted while a live session
  already owned it, an issue with no acceptance criteria was handed to an agent
  that had to guess what "done" meant, and an issue blocked on another
  repository's work had nothing to attach that fact to. Readiness is now four
  predicates evaluated cheapest-first (`pkg/sdlc/readiness.go`): open and
  triggered, acceptance criteria present (opt-in), not already owned by a live
  session anywhere in the fleet, and declared dependencies satisfied.

  An issue declares its dependencies in its own text, in the body or a comment:
  `Blocked by: owner/repo#123` and `Requires release: owner/repo@v0.9.5-alpha`.
  Two directives, because **merged is not released**: a fix can be merged and its
  issue closed while every consumer that pins a version still resolves the old
  code, since module versions resolve from tags. The release dependency is
  checked by asking the forge whether the tag exists — exactly the consumer's own
  question — through the new `ForgeBackend.GetTag` and a new
  `UnblockReleaseTag` condition kind.

  A blocked issue **parks and resumes** rather than holding a session, because an
  upstream wait is unbounded. It is adopted as a Task created already blocked in
  ONE write (`TaskSourceSpec.Park` → `CreateTaskRequest.Park`) — create-then-block
  leaves a window exactly long enough for a sweep to start the job the park exists
  to prevent — and it resumes through the machinery that already existed: a typed
  `UnblockCondition` re-examined by `UnblockSweeper` on every pass. It holds no
  session, no container and no lease while it waits, and the issue says why.

  The dependency walk carries three bounds. A **cycle** (A blocks B blocks A) is
  detected and the issue **refused** for a person to fix, because an undetected
  cycle is an infinite spawn loop — the most expensive failure available here. A
  **depth bound** (default 8) is refused rather than truncated, since a silently
  truncated walk reports "satisfied" for a chain it never finished reading. A
  **node bound** stops one malformed graph spending a sweep's whole budget.

- **`feature/software-factory` did not compile.** `triage_runner.go` (#879)
  constructs a `cloner` with `exec:` and `taskID:` set in the composite literal,
  and #914 moved both fields into an embedded `gitSteps` — which Go forbids
  setting through promotion. Each change was green on its own branch and the
  break appeared only in the merge. Fixed here because this branch's merge is
  where it surfaced.

- **A workspace fault cost a whole agent session and was then charged to the
  work.** Measured on the burn-in node, 2026-09-11: go-truststrap#24 asked a
  worker to adopt a file from a repository nobody had declared, so it read the
  issue, the repository, its `Makefile` and the one context repository that was
  there, ran five filesystem probes to establish that the file was absent, and
  escalated. Its behaviour was correct. Two things around it were not.

  **The workspace is now proved before anything is prompted**
  (`pkg/sdlc/preflight.go`). After the clone and before the first turn, the
  runner probes — inside the container, in Go, at zero token cost — that the
  project checkout and every declared context repository is a git repository,
  is the repository the workspace spec named, and holds a commit; and that the
  project checkout is on the planned branch. A refusal **names the repository**
  an operator has to fix, every probe it ran is recorded on the job under
  `sdlc:workspace_preflight`, and it writes nothing itself. It catches three
  faults the clone's own idempotence cannot: an empty checkout (the fault the
  attempt *before* the escalation died on), a directory a dead predecessor left
  holding a different repository, and a clone killed part-way. The reviewer's
  checkout is proved the same way, so no verdict is ever stated about a tree
  nobody intended.

  **An operator fault is no longer charged as a failure of the work.**
  `BlockReason.FaultClass` carries the runner's own attribution, in two classes
  the engine owns: `workspace_unfit` (no workspace path, a mount point the
  daemon could not make, a pre-flight refusal, or any failed git command of the
  checkout — all of which happen before a token is spent) and
  `worker_environment` (the worker declared `SDLC-STOP: environment`, declared a
  stop code this binary does not know, or answered with no text on every turn).
  A park carrying one is refunded (`Task.EnvironmentFaults++`), names its cause
  on the issue, and releases its task source so a re-label readmits it after the
  repair. The log line that used to say `charged as a failure of the work` on
  the line *after* classifying the same job as an operator's to repair now says
  which of the two it was.

  **A genuine failure is still charged**, and that direction is tested hardest:
  `OperatorFaultClass` recognises exactly those two classes, `JobRun.Park` drops
  any other while keeping the park, and a park about the work's own content — a
  question, an owner's decision, a spent review budget, a worker that reported
  work it did not do — spends its attempt exactly as before. Refunding a broken
  change would retry it for ever.

- **The embedded agent and every per-session agent MCP certificate could not be
  issued at all.** `service.XKMSAgentCertIssuer` called
  `TrustedCA.IssueCertificate` and then required
  `IssuedCertificate.PrivateKeyPEM`, a field no go-xkms backend has ever
  populated. Against a real CA it failed on every invocation with *"ca did not
  return exportable private key"* — silently, because its only callers log a
  warning and continue. The embedded agent therefore ran without an mTLS
  identity, and `AgentMCPPresetResolver` could mint no session credential, which
  is the path go-quicbaas#911 built agent session identity on.

  `service.SDKAgentCertIssuer` — the transport-agnostic sibling on the same
  interface — carried the identical defect and is fixed the same way. It had no
  non-test caller, so nothing was broken in the field, but it was a trap: the
  next caller to reach for it would have got "ca did not return exportable
  private key" from every transport, and its certificate could not have carried
  the `OU` the agent register handler requires, because
  `IssueCertificateRequest` has no field for one. It now generates at the point
  of custody and calls `CAService.SignCSR`, so all three agent issuers take the
  same path.

  Nothing caught it because the tests covering that code asserted against a stub
  CA that returned a private key it had invented — the exact failure mode
  go-xkms's `pkg/acme/challenges/enroll-01/ca_compliance.go` records from a
  shipped bug. The issuer now generates at the point of custody and signs a CSR,
  and the tests assert against a real go-xkms CA on the issued certificate:
  it verifies against the bundle shipped with it for `clientAuth`, the returned
  key is the certificate's, go-qrdb derives the intended tenant from its SPIFFE
  SAN, and two sessions never share a key.

- **A worker spent fourteen minutes and a whole attempt producing prose, because
  it was not permitted to run a command.** Measured on the burn-in node,
  2026-09-13: session `570cff37-42e7-442d-b079-39d0f4623b50` ended
  `tool_uses=0 blocks=text:6` — six blocks of text, not one tool call, the tree
  untouched. The container was not locked down at the Docker level
  (`ReadonlyRootfs=false`, no dropped capabilities); the binding constraint was
  the **tool grant**. Every Claude Code spawn carried
  `--permission-mode acceptEdits`, which grants the file-editing tools and
  withholds Bash, so a containerized worker could edit files and run nothing: no
  build, no test, no `git diff` — and in a non-interactive `--print` session
  there is nobody to approve the command it would have had to ask for.

  **A sandboxed spawn now takes full control of its container.**
  `WorkerSpawnArgs(backend, sandboxed)` returns
  `--dangerously-skip-permissions` for a request whose own `SandboxMode` is
  `Required`, and the rationed host grant for everything else — including a mode
  policy has not resolved yet, because the narrower grant is the safe reading of
  an answer that is not known. The operator's `ExecutionGuard` allowlist remains
  the authority for what runs on the daemon host. A role that must stay narrow
  states its own `--permission-mode` and keeps it, which is the opt-out **triage**
  takes: it reads a repository at its default branch and writes nothing.

  **The image gives that uid passwordless `sudo`** (`ALL ALL=(ALL:ALL) NOPASSWD:
  ALL`), so a task whose toolchain the image does not carry is one `sudo apk add`
  away. `sudo` rather than `USER root`, for two measured reasons: Claude Code
  refuses `--dangerously-skip-permissions` at uid 0 (*"cannot be used with
  root/sudo privileges for security reasons"*), so a root container takes back the
  grant this change exists to give; and anything root writes through a bind mount
  is root-owned on the host, which the daemon can then neither clean up nor
  re-clone into. The ephemeral workspace tier moves the checkout to a tmpfs, and
  the two remaining bind mounts — the workspace spec staging directory and the
  signing agent's socket — are read-only, so no bind mount can receive an agent's
  write except the persistent-tier workspace, which stays daemon-owned.

  **The `getpwuid` trap is proved rather than documented.** `git` invokes
  `ssh-keygen` for every `gpg.format=ssh` signature, `ssh-keygen` calls
  `getpwuid`, and without an `/etc/passwd` entry for the running uid it exits
  `"No user exists for uid"` — so every signed commit fails, after the work is
  done and before it is pushed. The image build now makes a real signed commit as
  the uid the spawn runs as and verifies it back, and `make test-worker-image`
  asserts the same plus `sudo -n apk add`, an arbitrary privileged command, and
  that a bind-mounted write stays owned by the spawning uid.

- **A reviewer was spawned before any test had run.** `DispatchReview` fired on
  `pull_request` opened and synchronized — the two deliveries that arrive before
  CI starts — and **discarded** `EventCheckRunUpdated`, the one that says the
  pipeline finished. This fleet's pipeline takes about fifty minutes, so a review
  was spent on a tree CI might still reject, and the reviewer was invited to run
  the suite itself to find out.

  A review now starts only once the head's required checks are **green**, decided
  by `ChecksGreenForReview` — which *is* the merge gate's own `evaluateChecks`, so
  "green enough to review" and "green enough to merge" cannot drift apart. It is
  event-driven rather than a wait: nothing is held open across the fifty minutes,
  each delivery asks again, and a red head simply gets no review until the
  re-run's green delivery arrives — so a red-then-green head produces exactly one
  review. A project that does not set `RequireGreenChecks` is dispatched exactly
  as before and reads nothing from the forge to establish it — and a pipeline
  delivery is nothing to such a project, because the check-run delivery exists
  for the gate.

- **The reviewer was not told CI had already passed, so it re-ran it.** The
  reviewer charter now states plainly that build, lint, unit, integration, race
  and security have already run against this exact commit and passed, and that
  the reviewer must not run the build, the tests, the linters or the scanners,
  install a toolchain, or execute the change — its job is the diff: design,
  correctness, security, and whether the change matches the issue it claims to
  close. Both sentences are declared **markers** on the prompt specification, so
  an edited charter that drops either is refused at registration, the same way
  the worker charter cannot stop naming a stop code.

- **Triage could not authenticate its clone, so the capability had never once
  run.** Observed on the burn-in node, 2026-09-13: five of seven triage sweeps
  spawned a container, ran `git clone ssh://git@…/go-truststrap.git`, collected
  `Permission denied (publickey)` and concluded — one wasted job every fifteen
  minutes, on every project with triage enabled, since #879 merged.

  The cause was two runners obtaining forge credentials through different
  seams. The delivery runner mounts an ssh agent socket because it needs one to
  **sign**, and ssh authentication for its clone came along as a side effect.
  Triage and the reviewer sign nothing, so they were deliberately handed no
  socket — and an ssh remote is authenticated by nothing else. Each runner was
  internally consistent; the defect existed only in the difference between them,
  which is why no test could see it.

  **There is now exactly one way a container reaches the forge**
  (`pkg/sdlc/workspace.go`). A runner states what it needs — the project, the
  binding's forge, the branch, and whether its container also signs commits —
  and the seam resolves the token, starts the agent, checks the credentials
  against the spec and puts every mount and path on the spawn request. The
  delivery runner, the reviewer and triage all call it, and a test asserts that
  every call which acquires or mounts a forge credential appears in that one
  file.

  **The two keys a project's git identity holds are now stated separately.**
  `ProjectGit.AccessKey` has always known that an ssh remote is authenticated
  with an access key that may differ from the commit signing key; the agent did
  not, and served both or neither. `sdlc.SigningScope` names the difference:
  `SigningScopeDelivery` serves both (a worker commits, signs and pushes),
  `SigningScopeCheckout` serves the **access key alone**, so a reader can clone
  and can produce no commit signature. A reader's agent is then stopped the
  moment its checkout is made — before the model is prompted at all — so the
  socket its container holds for the rest of its life answers nothing. An
  https-only reader is handed no agent whatsoever, as before. A project that
  wants its readers to hold no signing key even for that window configures a
  distinct `git.ssh_key`.

  **A credential the spawn does not hold is refused before the container**
  (`orchestration.RequireCheckoutCredentials`). The transport decides the
  credential and the spec names the transport, so "this spawn could not have
  cloned" is decidable on the daemon. The refusal names the repository, the
  remote and what was missing. This is the #914 pre-flight applied one step
  earlier: a triage job should not reach `git clone` to learn it has no key.
  It is `ErrWorkspaceUnfit`, so a delivery attempt **parks for an operator**
  attributed to the factory rather than spending the task's whole attempt budget
  on a configuration that would refuse identically every time.

- **Triage now proves its checkout before it spends a turn**, with the same
  pre-flight the delivery runner and the reviewer run (#914). It had none: a
  mount a dead predecessor left holding a different repository is adopted by the
  clone's own idempotence, and triage would have judged the whole issue queue
  against it.

- **The reviewer had the same credential gap** and is fixed by the same change.
  It had not surfaced on the node only because no in-daemon review had yet
  cloned an ssh remote.

- **Review dispatch was webhook-only, so a project whose delivery was broken
  never reviewed anything.** `DispatchReview` had exactly one non-test caller —
  the webhook ingestor — while its own comment promised "the poller remains the
  floor". There was no floor: the task-source poller adopts *issues*, and
  nothing swept pull requests. A node whose hook was missing, misconfigured or
  never provisioned therefore reviewed nothing, silently, and **repairing the
  hook did not recover the backlog**: the pipeline event for an already-green
  head fired in the past, a newly provisioned hook receives no replay, and no
  operator verb could ask for a review, so the only way to advance an
  accumulated queue was to force CI to run again on each pull request purely to
  manufacture a fresh event. Measured on the burn-in node (2026-09-13):
  `jeremyhahn/go-truststrap` had zero webhooks, and pull requests #64 and #65
  sat with all six checks green and `mergeable: true`, permanently unmergeable
  because branch protection requires `quicbaas/review`.

- **A daemon's non-primary backends were unreachable: the request was silently
  served by its primary.** Measured on the burn-in node, 2026-09-13. The fleet
  daemon registers with `backend: claude` and serves `["claude","codex",
  "opencode"]`; a review job with a `codex` reviewer bound spawned
  `session … backend=codex`, then logged `remote_acp_backend: Prompt
  backend=claude` and ran claude, which failed with
  `terminal_reason=api_error`. The session record said codex throughout and
  nothing compared the two.

  Two facts combined. `remoteBackendPool.backendFor` cached ONE backend per
  daemon, keyed on `reg.Host` — the axis that actually varies, the requested
  backend, was not in the key — and `RemoteACPBackend` then stamped
  `registration.Backend`, the daemon's PRIMARY, on every RPC it made. So
  resolution correctly found a daemon that serves codex and handed back the
  instance that asks for claude.

  **A `RemoteACPBackend` now serves exactly one backend name**, given at
  construction as `RemoteACPBackendDeps.Backend` and read from one field by
  Spawn, Prompt, Infer and ListModels — and by the `Session` Spawn returns, so
  "the session's recorded backend equals the backend named on its RPCs" holds by
  construction rather than by a check. The name is bound at construction rather
  than passed per call because `AgentBackend`'s own requests do not carry one:
  `SpawnRequest` and `PromptRequest` have no backend field and `Close` / `Status`
  send none on the wire, so a per-call name would have had to widen three
  request types and the interface, and would have left an empty value to fall
  back from — which is the defect. The resolver's pool caches per `(daemon host,
  requested backend)` accordingly; invalidation stays per HOST, so a daemon
  replaced at the same address drops every backend cached for it.

  **A spawn for a backend the selected registration does not serve is now
  refused**, with the resolver's own `NoMatchingAgentError` (constraint
  `backend`) rather than a new error class, stashed on the request so the API
  answers `no_matching_agent` instead of falling through to an in-process
  backend. The old code could not express the refusal: it never compared the
  requested backend against the registration.

  **`Infer` is the one request that carries its own backend name**, so it is the
  one place the invariant is checked rather than only stated: a request naming a
  backend other than the instance's is refused with
  `RemoteBackendMismatchError` before any RPC runs.

  This is a correctness property, not a label. `ReviewPolicy.
  RequireIndependentReviewer` refuses an approval from the model family that
  wrote the code, and it checks the CONFIGURED reviewer — so the check passed on
  codex/openai, claude ran, and a gate whose entire purpose is independence was
  satisfiable in configuration while violated in fact. On this node it failed
  loudly only because claude returned an API error; a successful turn would have
  been recorded as an independent openai review of Anthropic-written code.

- **`verify-api-shapes` had never compared anything.** It defaulted its two
  go-qrdb inputs to an absolute NFS path on one developer's machine and, when
  that path was absent, printed "missing source files; skipping" and exited
  **0** — which is
  every host that is not that one lab machine, including CI. go-qrdb's SDK is
  a module this repository requires, so the gate now resolves it with
  `go list -m -f '{{.Dir}}'` and there is nothing left to default. On its first
  real run it found three canonical Go types with no TypeScript counterpart;
  the product decision for each is go-quicbaas#946, and the three names sit on
  the gate's documented exemption list with that issue as their reason, so the
  drift is tracked rather than silenced. Deleting them is part of closing #946.

- **`verify-cli-flags` passed when it could not build its reference.** Both
  `go build`s discarded their errors, and a binary that would not compile
  yielded an empty flag set and a "skipping" exit 0 — so a compile error in the
  canonical CLI read as agreement. Both binaries are required, and a build
  failure is a typed error carrying the compiler's own output.

- **A gate could not tell "a violation" from "could not run".** All three
  outcomes were exit 0 or exit 1 with the reason in prose. Every converted gate
  reports 0 clean, 1 a finding, 2 the gate could not run, and a missing
  required input is a typed error naming the input and what it was needed for.

- **Rule 2 of the naming gate reads its candidates after the walk, not inside
  it.** Reading a path a `WalkDir` callback was handed is a symlink TOCTOU
  (gosec G122): the entry the walk classified and the file the read opens need
  not be the same thing. `golangci-lint` excludes G122 and `make gosec` does
  not, so the shape passed one gate and failed the other. Collecting and then
  reading is also what `scripts/internal/dockerinv` does, so the tree has one
  answer to this rather than two.

- **`verifycleandocker` no longer indexes an inventory it does not have.**
  Deriving the behaviour cases' tags from a repository whose inventory failed
  left the tag list empty and the gate indexed it anyway. Found by the test
  written for the failing path.

- **The repository is publicly releasable again, and a gate keeps it that way.**
  Everything tracked here is published — a module zip, a release tarball, two
  container images, and a UI built from this tree and embedded in the binary —
  and an audit (go-quicbaas#944) found a pile of accidental commits plus lab
  values scattered through help text, godoc and documentation. None of it was
  load-bearing and nothing prevented any of it.

  Seven artefacts were deleted and added to `.gitignore`: a **symlink at the repo
  root pointing at `/mnt/<lab-host>/go-quicbaas/`** — dangling in every clone,
  every release tarball and the Docker build context — a stale
  `SESSION_RESUME.md` full of absolute paths, a per-machine Claude permission
  allowlist, two FUSE orphans from the lab's NFS mount, and opencode's SQLite
  WAL/SHM pair.

  Lab values were removed from the files a public reader actually reads: the
  seven forge issue links in `docs/barrier/cli.md` now point at
  `github.com/jeremyhahn/...`; `.github/workflows/release.yml` resolves its
  registry as `${{ vars.REGISTRY || 'ghcr.io' }}`, so the private forge sets a
  repository variable and no forge address is written down in the tree (the old
  ternary published it four times and pointed every non-github fork at an
  unreachable host); the shipped UI's commit-author placeholder is `agent`
  instead of the owner's private Gitea principal, with **both** tracked bundles
  rebuilt so the embedded UI stops carrying it; `--help` text and exported godoc
  across `cmd/quicbaas` and `pkg/agent.Config.ServerAddr` name
  `quicbaas.example.com:8443` and `agent-01` rather than fleet hosts; and the
  copy-pasteable endpoints in `docs/embeddings/npu.md` name `npu-host.internal`.
  Benchmark prose naming the machine a measurement was taken on is provenance and
  stays, waived in place.

  The captured runner logs in `pkg/cifault/testdata` and the `pkg/sdlc` log
  fixtures published the private registry on 13 lines. Those logs are the right
  way to build a log classifier, so the host was neutralised rather than the
  fixtures discarded: `pkg/cifault/classify.go` keys on generic signatures
  (`failed to pull image`, `http response to https client`), so every assertion
  still holds.

- **The ollama endpoint was hardcoded to loopback**, which is correct only for a
  daemon running on the GPU host itself; every other node in a fleet had no way
  to reach a remotely served model. It is now `--ollama-url` / `ollama_base_url`,
  defaulting to the previous value.

- **A bound project's reviewer could not be configured, seen, or kept.**
  `ProjectBinding.Reviewer` and `.Discovery` existed in the domain type and in
  the REST body but had no representation in `sdlc.proto` or the Go SDK type, so
  only a hand-written REST call could set a reviewer; `quicbaas sdlc binding`
  reported none whatever was stored; and because a bind REPLACES the whole
  record, binding through gRPC, MCP or the CLI silently erased the reviewer a
  project already had. A project with no reviewer is skipped by both dispatch
  paths — `TriggerReview` and `ReviewSweeper.sweepBinding` each return
  `(false, nil)` on `!Reviewer.Configured()` — with nothing written to a log, a
  task or the forge, so the only symptom was finished, green pull requests
  sitting unreviewed. Both fields now cross every transport, and the binding
  table names an unconfigured reviewer in words.
- **A reviewer's forge token is now resolved at bind time.** It was checked only
  when a verdict was published — after the pull request was green, a review job
  had started and a model run had been spent reading the change, with the
  attempt charged to the work. The forge token was always checked at bind; the
  reviewer token was missed.

- **A forge issue no longer claims live work for a task that stopped days ago.**
  go-quicbaas#788's conclusion seam made every *new* ending reach the forge, and
  left two holes behind it. A conclusion whose forge write failed answers
  `SurfacingUndeliveredError` and is never revisited — correctly, because the
  task is already durable, so nothing tries again. A conclusion that predates
  the seam was never posted at all. There was also no liveness check from the
  forge's side: a node that died between concluding a task and posting its
  outcome left the label stuck with nobody watching. Measured on the burn-in
  node as seven `go-truststrap` issues (#25, #26, #52, #53, #54, #55, #56)
  carrying `factory:in-progress` for terminal tasks, cleared by hand.

  `pkg/sdlc/label_reconcile.go` adds a reconciliation sweep that runs at boot
  and on a ticker. It asks the forge which issues carry a *claiming* label —
  the binding's `in_progress` and `in_review`, the only two that assert the
  factory is working right now — and settles each against the engine's own task
  record: a terminal task gets the outcome it never got and the label moves, an
  issue whose task record is gone gets `abandoned`, and a task the engine is
  still running whose issue carries no lifecycle label has its `in_progress`
  label re-applied without any outcome being announced, because nothing ended.

  The comment is `renderOutcome` and the post is `Hooks.TaskConcluded` — the
  whole of #788's path, reached from a sweep rather than from the engine — so a
  reconciled ending is byte-for-byte the ending the engine would have written.
  Before posting, the issue's discussion is searched for this task's, this
  attempt's, this outcome's marker, the same identity `runnerRun.parkPosted`
  checks, so a sweep against a forge whose write failed once does not leave a
  comment per pass.

  **That marker check is idempotence within one node.** The sweep runs on every
  SDLC-enabled server, so two nodes can both find the marker absent and both
  post. The cost is a duplicate comment, not a wrong one: both render the same
  ending, the label lands in the same place, and once either exists no later
  sweep on any node posts again. Closing it needs a durable one-writer
  reservation with an explicit TTL — without the lease, a node dying between
  reserving and posting would strand the outcome unannounced for ever, which is
  worse than the duplicate — so it is specified separately in go-quicbaas#840
  and is not reachable on a single-node deployment.

  Only the claiming labels are listed. An issue resting on `escalated` or
  `merged` with no task is the documented correct end state of a task that spent
  its budget and released its source reservation; listing the terminal labels
  too would have the sweep post `abandoned` over every issue the factory ever
  finished with.

  A forge refusal is a typed, logged, per-issue `deferred` finding that leaves
  the issue untouched and is retried on the next sweep; a project that cannot be
  swept defers as one finding and the pass continues. Only a tenant enumeration
  that fails ends a pass, as `LabelReconcileUnsweptError`. Forge calls are paced
  by a lock-free interval pacer so the first pass over a wholly drifted project
  cannot earn the node a rate-limit ban.

  A failed task whose attempt budget is *not* spent is reported `undecidable`
  rather than announced: `applyConclusion` fails a task for one reason only, so
  such a task was retired by some other hand and "this task has spent its
  attempt budget" would be a false statement on somebody's issue.

  **The sweep never writes on the strength of its opening snapshot.** It lists the
  tasks and then lists the forge, and spends a paced forge round trip on every
  issue before the one it is settling, so an ordinary adoption — the poller
  persists a task, the run it dispatches labels the issue in progress — can land
  in between. Announcing an ending on that live run and moving its issue to a
  terminal label would be strictly worse than the stale label the sweep exists to
  correct. The record is therefore read **again** before BOTH answers that write
  — the terminal task's outcome as well as the orphan's `abandoned` — after the
  claim that provoked the question and before anything is written. The terminal
  half matters because a spent task releases its source reservation, so
  re-labelling the issue adopts a fresh task while the spent one stays on the
  record, and a pass holding only the spent one would post its ending over a live
  run; the re-read is indexed by the same live-supersedes rule, so the fresh task
  wins. because the engine persists a task
  before anything labels its issue, a claim observed at *T* implies its task was
  durable before *T*, so the re-read is a different answer rather than a narrower
  window. A task store that will not answer defers the issue untouched as
  `LabelReconcileRecordError`.

  **A death is announced as `abandoned`, not as an escalation.** `applyDeath`
  parks a reaped job whose attempt budget is spent as `blocked`/`escalated` —
  the same resting shape an explicit engine escalation leaves — but concludes it
  `abandoned`, because a death carries no diagnosis. Reading the block kind alone
  posted the escalation's diagnosis over precisely the node-died-before-surfacing
  case this sweep exists to repair. The conclusion is never persisted, so the
  sweep tells the two apart by the one thing that survives: the job failure class
  `applyConclusion` records on the task's result, read through the new
  `orchestration.Task.FailureClass()` accessor. Both conditions are required —
  a task parked some other way whose earlier attempt happened to be reaped still
  carries that class, and neither requeue nor readmit clears it.

  **A readmission whose label never came off is repaired too.** `ReadmitTask`
  persists `readmitted` and then surfaces it, and that outcome's empty label key
  is what takes the park label off the issue. When the forge write fails, the
  record says the task is queued again while the issue still says
  `factory:blocked` — and neither of the other two directions can see it, since
  the claim direction lists only the claiming labels and the hand-over direction
  skips pending tasks. A third direction walks tasks whose persisted reason is
  `readmitted` **and is still queued**, and clears the label, announcing nothing;
  it re-reads the record first, so a task parked again mid-pass keeps its new
  park's label. The status gate is load-bearing: `ClaimTask` does not clear the
  reason, so keying on the reason alone would have fired on live work — the
  hand-over direction restoring a running attempt's label and this one stripping
  it back off in the same pass. The two label-writing directions now select on
  disjoint statuses by construction.

  **A park the worker surfaced is not announced as merged.** The outcome table
  maps both `completed` and `runner_parked` to the silent row, because in both
  cases the moment that concluded the task already wrote to the forge — but
  `completed` means the pull request merged while `runner_parked` means the worker
  stopped and the task is blocked waiting on a person. Settling the silence on
  `merged` told the reader of a blocked issue that their work had landed, which is
  worse than the stale claim it replaced. The reason now decides, and a runner
  park is restored to the label its own `BlockKind` names.

  **The conclusion reason is read from the record rather than re-derived from
  it.** `Task.ConclusionReason` is persisted by the write that concluded the task,
  so the sweep reads it. The earlier derivation from the resting status could not
  tell an engine escalation from a reaped job whose budget was spent — both rest
  `blocked`/`escalated` — and would have announced the escalation's diagnosis
  over a death nobody diagnosed. An empty reason is not an ending and reports
  `undecidable`.

  **The restoration direction re-establishes its premise too.** Re-applying the
  in-progress label is a write, so it asks the same question the claim direction
  asks: the task is re-read and must still be handed over, and the issue is
  re-read and must still lack a claiming label. A task that concluded while the
  pass was in flight has already had its ending posted and its issue moved onto a
  terminal label by `TaskConcluded` — restoring from the snapshot would strip
  that label off and re-assert live work for a task that has stopped, undoing a
  correct ending and leaving the forge lying until a later sweep repaired the
  repair. Either read failing defers the issue untouched.

  **A `pending` task does not make a claim true.** Pending is live but not handed
  over — the converse direction refuses to apply `in_progress` to one for exactly
  that reason — so a claiming label over a queued task is reported `undecidable`
  and appears in the drift report, rather than being certified `consistent` and
  filtered out of it. Nothing is written: the task is about to start, and moving
  the issue back onto the trigger label would un-claim a run being dispatched.

- **A half-applied label move no longer strands the issue.** `moveLabelsOn`
  removed the module's other owned labels before adding the desired one, so a
  remove that landed and an add that failed left the issue carrying no lifecycle
  label at all — invisible to *both* directions of the reconciliation sweep,
  since it is not in the claiming listing and a terminal task is not a lost
  hand-over. The display drift became permanent at the moment the repair failed.
  The add now goes first: if it fails nothing has been removed and the issue is
  still findable, and if a later remove fails the issue is merely over-labelled,
  still claiming, and converged by the next sweep. Over-labelled is recoverable;
  unlabelled is not.

- **The daemon refuses removed `sdlc.*` keys too.** `pkg/config`'s `Loader`
  refused them and the documentation said a configuration carrying one fails to
  load, but `quicbaasd` unmarshals the global viper directly and never called
  `RefuseRemovedSDLCKeys` — so the promise was kept by the library path and
  broken by the production one, and an operator who renamed nothing booted a
  node that ignored the setting while believing it was enforced.

- **`quicbaas orchestrator task get` could not read any task that had ever run.**
  The Go SDK's `TaskArtifact` disagreed with the server in five of its seven
  fields: `ref` is an object on the wire and was declared a string, `mime_type`
  was spelled `content_type`, `content` was spelled `data`, and `kind` and
  `metadata` had nowhere to go. Since the SDLC module writes an
  `outcome_comment` artifact on every issue it touches, every real task failed
  to decode with `cannot unmarshal object into Go struct field
  TaskArtifact.output_artifacts.ref of type string`. The type now matches what
  the server sends.
- **The embedded transport answered with a smaller artifact than REST served.**
  Three separate copies of the conversion each flattened `Ref` to its `Path` and
  dropped `Kind` and `Metadata`. They are now one converter, covered by a parity
  test.

- **A forge that refuses the offboarding no longer strands a binding.** The
  previous fix waved through only a missing secret, which turned out to be half
  the failure: restoring the deleted secret moved the refusal from `404` to
  `403 user should be an owner or a collaborator with admin write` — the agent
  is not a repo admin, so it cannot delete the webhook — and the binding was
  still undeletable while the triage backstop failed a job against it on every
  pass. `Unbind` now removes the local record whenever the forge *answered*
  (any 4xx bar 408/429), since repeating that call can never change the answer.
  A 5xx or transport failure still refuses, so a cleanup a retry would complete
  is not silently skipped.

- **A binding whose forge token secret was deleted could never be unbound.**
  `Unbind` offboards the forge first, offboarding needs the binding's forge
  token, and a deleted secret made that permanently impossible — so `DELETE
  .../binding` answered 500 `secret has been deleted`, `retryable: false`, on
  every attempt. Because the triage backstop starts a job for every *bound*
  project, an orphaned binding failed a job on every pass for ever with no verb
  able to stop it. Offboarding now proceeds past a forge cleanup that can never
  succeed, logging what was left on the forge; a transient forge failure still
  refuses, so a cleanup that a retry would complete is not silently skipped.

### Security

- **`--sandbox-policy required` is no longer silently bypassed for a CLI-mode
  agent.** `ACPBackend.dispatchSpawn` asked whether the backend was in CLI mode
  BEFORE it asked what isolation the request demanded, so the two CLI branches
  returned before the sandbox switch was ever reached. The daemon builds its
  `claude` backend with `CLIMode: true` and the sdlc worker spawns persistent,
  required sessions — so on a node started with `--sandbox-policy required` the
  agent ran as a direct child of the daemon, on the daemon's uid, with the
  daemon host's whole filesystem and network namespace and the tenant's live
  provider credential in its environment, and the session it returned carried no
  container id. Every guarantee the container was there to provide — the mount
  policy, network isolation, the trust tier, `--user`, `--rm` — was inert, and
  nothing said so (proven live on the burn-in, #742).

  The sandbox mode is now resolved FIRST, on both the spawn and the resume path,
  and `required` has exactly one shape: a container, or a typed
  `SandboxModeViolationError`. It never degrades to a host exec.

- **The forge's SSH host key is pinned on the binding, never learned.** Nothing
  provisioned a `known_hosts` for the worker container, so once `ssh` existed
  the clone would have failed on host-key verification instead — and the one-line
  fix for that, `StrictHostKeyChecking=accept-new`, would have made every
  checkout a trust-on-first-use for a worker that holds a live socket to a
  KMS-backed signing agent.

  A binding now carries `forge.ssh_host_key`: one OpenSSH public-key line, what
  `ssh-keyscan` prints after the host field, validated with
  `golang.org/x/crypto/ssh` when the project is bound. Per attempt the daemon
  derives the host pattern from the project's repository URL, re-renders the
  pinned key into a single-entry `known_hosts` under the read-only workspace
  spec mount (`/run/quicbaas/workspace`), and puts
  `GIT_SSH_COMMAND=ssh -o UserKnownHostsFile=… -o StrictHostKeyChecking=yes`
  on the worker's spawn. `accept-new` is not reachable from any configuration,
  and no host key or forge address is baked into the image or the source.

  An `ssh://` project whose binding pinned nothing is refused BEFORE the worker
  spawns, with `orchestration.SSHHostKeyError` naming `forge.ssh_host_key`. An
  `https://` project is unaffected.

### Notes

**Unchanged, deliberately**

- **`DeleteTask`'s lease guard still lists the tenant's whole lease subtree**
  rather than one project's. The commit that widened it cited #613; the reason
  that survives the fix is a stronger one. A lease's project segment is pinned
  when the job acquires it, while a task's `ProjectID` is a field `UpdateTask`
  rewrites, so a task moved between projects mid-flight leaves its live lease
  under the FORMER project's subtree. A narrowed listing would find nothing
  there and delete the record of a task a job is still running — the one
  outcome the guard exists to prevent. Now covered by a regression test.

**Removed before shipping — the cost axis**

- **wp12's second metered axis (integer micros) was re-homed and then taken
  out.** Nothing in this tree prices a turn: `recordConsumption` is the single
  production writer of ledger entries and sets no `CostMicros`, and
  `TokenUsage` carries no cost field, so every entry would carry zero. A
  `MaxCostMicros` window would have summed zero and admitted every turn
  forever — an operator setting a "daily spend" cap would have got something
  documented as a limit that silently was not one, which is precisely the
  failure this whole layer exists to remove, on the money axis. A knob that
  looks like protection and is not is worse than no knob.

  `ConsumptionEntry.CostMicros` is kept as the slot a price lands in, and its
  zero now means UNMEASURED rather than free, stated at the field and at the
  roll-up so nothing renders it as a measured figure.
  `TestConsumptionCostIsUnmeasuredAndNotMetered` fails the day something
  prices a turn, which is the only order in which the price source, the cost
  axis and the reported column may land. Filed as #791.

  Found in peer review of this PR, before merge.

**Not in this change**

- **An account with no policy is unmetered**, and the zero value admits.
  Inventing a default token allowance would be inventing a fact about someone's
  plan, and a wrong one refuses work the provider would have served. What a
  deployment's shipped default should be is an operator decision.
- **The policy is not yet settable over a transport.** It round-trips through
  `AccountStore` and is enforced wherever it is set; the operator-facing
  create/update surface across REST, gRPC, MCP, QUIC, Unix, the SDK and the CLI
  is filed separately, alongside the cost drill-down.
- **Bounded failover** (the fourth wp12 piece) is not re-homed. The current
  tree already ships session account rotation, which does the same job with
  different bounds, and reconciling the two changes shipped behaviour.

**Review follow-ups**

- **A report on screen belongs to the breadcrumb above it.** Both Cost views
  kept the previous scope's figures rendered while the next read was in
  flight: the Cost page's breadcrumb changed on the click and the job page
  stays mounted across a navigation from one job to the next, so a read slower
  than the eye printed the parent's totals as the child's and job A's tokens as
  job B's. The state is cleared when the read starts, and the reply guard now
  includes the TENANT as well as the scope — switching tenant leaves the drill
  chain untouched, so a scope-only check accepted the previous tenant's report.
  A superseded read also no longer clears `loading`, which had left the
  successor in flight under "No spend report loaded."
- **A quota window shorter than a second survives being displayed.** The policy
  validator accepts any positive `time.Duration` and the wire carries
  nanoseconds for exactly that reason, but rounding to whole seconds before
  choosing a unit rendered a 100ms window as `0s` — a real limit that reads as
  no limit — and 1.5s as `2s`.

Closes #794

**Notes**

- **`scripts/check-reciprocal-pins.sh` stays shell, deliberately.** It declares
  itself a vendored copy of the canonical script in the `automatethethings`
  repo, and it is one: byte-identical apart from its vendoring header.
  Converting it here would fork it, and the value of this particular gate is
  that every repository in the portfolio enforces the identical rule — a
  rewrite differing even slightly in tag-namespace detection or ignore-file
  glob semantics would make this repo's answer differ from go-xkms's, silently,
  which is what the file's own header warns against. The canonical has to move
  first; that is filed as handbook#51, and closing it closes the last item of
  go-quicbaas#942. The exception is stated in the script's header rather than
  left to be rediscovered.

## [0.7.0-alpha] - 2026-09-06

MINOR rather than PATCH because the span since `0.6.0-alpha` carries breaking
dependency moves, and pre-1.0 that is where breakage lives
(release-versioning.md §1). go-qrdb and its SDK cross seven minors
(`0.9.0-alpha` → `0.16.0-alpha`), go-xkms, its SDK and xkey cross two
(`0.7.7-alpha` → `0.9.4-alpha`), go-quicraft one (`0.8.0-alpha` →
`0.9.0-alpha`) and go-truststrap two (`0.2.0-alpha` → `0.4.0-alpha`). Those
moves reach consumers through the type-alias re-exports in `sdk/go`, and
go-xkms `v0.9.2-alpha` raises the required Go toolchain to 1.26.7.

### Added

- **`sdk/go/xkms/ca` re-exports the TCG certificate-policy OIDs** as aliases of
  the go-xkms values: `OIDTCGKpAIKCertificate`,
  `OIDTCGVerifiedTPM{Residency,Fixed,Restricted}` and the `x509.OID` forms
  `TCGVerifiedTPM{Residency,Fixed,Restricted}Policy`. A relying party must
  check `tcg-cap-verifiedTPMRestricted` on an IAK/LAK certificate before
  trusting an attestation, and the platform may reach the arc only through
  this SDK. (#533)

- **`sdk/go/xkms/ca` re-exports the IAK certificate surface** from go-xkms#371:
  `CAIAKCertificateReader` (`IAKCertificate(ctx, model, serial)`) and
  `CAIAKCommonName` (`"iak-<model>-<serial>"`), the name the CA now issues and
  stores the Initial Attestation Key under so the device's IDevID record no
  longer overwrites it. (#533)

- **`sdk/go/xkms` re-exports the attested-peer verification primitive** from
  go-xkms#273: `PeerAttestationVerifier` / `NewPeerAttestationVerifier` /
  `WithPeerVerifierClock`, `PeerEvidence`, `PeerPolicy`, `PeerVerdict`,
  `PeerChecks`, `PeerClockInfo`, `PeerFailureClass` with
  `PeerFailure{None,Absence,Cryptographic}`, the typed
  `Peer{Policy,Context,ChannelBinding}Error`s, and `PeerChannelBinding` /
  `PeerQualifyingData` with `PeerChannelBindingExporterLabel` /
  `PeerChannelBindingLength`. Pure aliases; the platform's remote-attestation
  path is the consumer. Not aliased: `peer.FailureClassError`, which go-xkms's
  own SDK does not yet export (go-xkms#391); it follows in a later alpha. (#533)

### Changed

- First-party dependencies moved to the current tags in both modules: go-xkms,
  its SDK and xkey to `v0.9.2-alpha` (carries the IDevID signing fix,
  go-xkms#274/#276). Minimal version selection carries go-qrdb and its SDK to
  `v0.16.0-alpha` and go-quicraft to `v0.9.0-alpha`. go-xkms `v0.9.2-alpha`
  requires Go 1.26.7, so the `go` directive, `GO_VERSION` and every
  `golang:1.26.x-alpine` build image move with it. (#536)

- go-xkms, its SDK and xkey moved on to `v0.9.4-alpha` in both modules — the
  tag that carries the `Restricted` OID (go-xkms#359), the IAK certstore fix
  (go-xkms#383) and the attested-peer primitive (go-xkms#385). Minimal version
  selection carries go-truststrap to `v0.4.0-alpha` with it. (#533)

- **`GitProvisioner` bounds the SSH agent teardown after a clone.** go-xkms's
  `ConnectSession.SyncAndStop` now takes a context and reports any signing
  operation still running when it elapsed; the clone teardown gives it a
  5-second bound of its own and logs abandoned operations instead of hanging
  on a hardware key awaiting touch. (#536)

- **A seal-disabled node's IAM records now live under the system tenant's
  confined keyspace.** go-qrdb v0.10.0 (#41) made every node expose a
  `StoreResolver`, so IAM writes route through `"{tenant}/"` whether or not
  the seal subsystem is on; the new `Server.IAMKVStore()` is the accessor
  for reading them from outside the IAM components. Pre-release policy: wipe
  and re-bootstrap dev deployments. (#536)

- **A known-hosts pin must carry a well-formed fingerprint.** go-xkms v0.9.x
  validates `SHA256:<unpadded base64 of 32 bytes>` at the store, so the REST
  and CLI paths now reject a malformed fingerprint with a 400 / typed error
  instead of pinning something no host key can ever match. (#536)

- **The broker's email reaches qrdb as the canonical login key.** go-qrdb
  v0.15.0 (#191) refuses a user with no email login key unless the caller
  declares `WithoutEmailLoginKey`; `QRDBUserStorage.CreateUser` now forwards
  the email (and the display name) and declares a certificate-only identity
  when there is none, and a duplicate email maps to `iam.ErrEmailAlreadyExists`.
  First boot was failing at admin-user creation without this. (#536)

- **`RoleAdapter` resolves built-in roles by name for first-boot re-runs.**
  go-qrdb's firstboot refuses to derive a system role's identifier when the
  roles already exist and nothing can resolve them (`UnresolvableSystemRoleError`);
  the adapter now implements `RoleNameResolver` and is wired as
  `FirstBootDependencies.RoleResolver`, so the retry after a partial first boot
  binds the admin grant to the persisted role ID. (#536)

- **Self-service registration is forwarded to both WebAuthn relying parties.**
  go-xkms v0.9.2-alpha reads an unset `AllowPublicRegistration` as closed,
  which would have silently switched off `iam.self_service_registration.enabled`
  on every node; the operator's decision now reaches quicbaas's own per-tenant
  relying party and the composed go-xkms module's, and a test pins that the
  two agree (port of the feature/software-factory fix in #516). (#536)

## [0.4.0-alpha] - 2026-08-20

MINOR rather than PATCH because the span since `0.3.0-alpha` carries breaking
change, and pre-1.0 that is where breakage lives (release-versioning.md §1).
Two things break in it: service-account client secrets are now derived with
PBKDF2-HMAC-SHA256, so secrets issued before this release no longer verify;
and the dependency bump below moves go-qrdb across two minors, go-quicraft
across two, and go-signal and go-libsignal across one each, which reaches
consumers through the type-alias re-exports in `sdk/go`.

### Changed

- First-party dependencies moved to the current tags in both modules: go-qrdb
  and its SDK to `v0.9.0-alpha`, go-xkms, its SDK and xkey to `v0.7.7-alpha`,
  go-signal to `v0.2.0-alpha`, go-libsignal to `v0.2.0-alpha`, and go-objstore
  to `v0.3.0-alpha`. Minimal version selection carries go-quicraft to
  `v0.8.0-alpha` and go-truststrap to `v0.2.0-alpha`.

- **`signalbridge` reads the Kyber pre-key's public half through
  `SerializedPublicKey`.** go-libsignal `v0.2.0-alpha` moved the KEM secret
  key into guarded memory and withdrew the `KyberPreKey.KeyPair()` accessor
  that handed out the whole pair; the public half is now reached without
  touching the secret. The bytes published in the pre-key bundle are
  unchanged.

### Security

- **Service-account client secrets are derived with PBKDF2-HMAC-SHA256.**
  Secrets issued before this release no longer verify; re-issue them. Local
  deployments are wiped and re-bootstrapped, which is the pre-release upgrade
  path (release-versioning.md §1).

## [0.3.0-alpha] - 2026-08-19

MINOR rather than PATCH because the route de-collision below is breaking, and
pre-1.0 that is where breakage lives (release-versioning.md §1).

### Fixed

- **`StdioTransport.Send` no longer discards a response that races the
  transport close.** `Send` selected on the response channel and the
  transport's `done` channel together. `readLoop` delivers a response before
  it observes EOF and closes `done`, so an agent that answers and then
  immediately exits left both cases ready — and `select` chooses uniformly
  among ready cases. Roughly half the time the delivered response was thrown
  away and the call returned `ErrSessionClosed`. This affected any short-lived
  or one-shot ACP agent, not only tests; it surfaced as an intermittent
  `TestACPBackend_JSONRPC_Spawn_InitializeError` failure.

### Security

- **The form-encoded OAuth2 endpoints now bound their request bodies.**
  `/oauth2/token`, `/oauth2/token-mtls`, `/oauth2/revoke` and
  `/oauth2/introspect` are `RouteAuthPublic` and are registered outside the
  `BodyLimitMiddleware` group, so `ParseForm` would buffer an unbounded body
  from an unauthenticated caller. All four are capped at 1 MB via the new
  exported `MaxOAuth2FormBytes`.

### Changed

- First-party dependencies moved to the current tags: go-qrdb and its SDK to
  `v0.6.0-alpha`, go-xkms, its SDK and xkey to `v0.7.5-alpha`, go-quicraft to
  `v0.6.2-alpha`, go-truststrap to `v0.1.4-alpha`, go-frost to `v0.1.8-alpha`,
  go-luks2 to `v0.1.7-alpha`.

### Build

- `make vuln` distinguishes a govulncheck killed by a signal (exit >128, in
  practice the job container's memory cap) from a real vulnerability finding,
  instead of reporting both as "found reachable vulnerabilities". Still
  fail-closed in both cases.

### Changed — BREAKING: unified-node route de-collision (P3.3)

- **`/api/v1/auth/{refresh,logout,validate,login/piv}` dropped from the
  module surface.** On a composed node (the qrdb-embedded node's own
  `RegisterHTTP`, `pkg/apimodule/module_http.go`) go-xkms's ServiceModule
  owns the identical `/api/v1/auth/*` session-login plane; quicbaas's own
  registrations at those four paths would route-collide. All four are the SAME token/session family — they resolve
  through the same `authService.sessionAuthority` -> `iam.OIDCProvider`
  (`pkg/service/quicbaas.go`) that PIV login mints from — so they were
  removed together. The legacy in-process listener (standalone, non-composed
  deployments) is unaffected for `refresh`/`logout`/`validate`: they remain
  reachable at their original paths there.
- **`POST /api/v1/auth/login/piv` moved to `POST /api/v1/iam/login/piv`.**
  quicbaas's OIDC-session PIV login capability is kept (it is functionally
  distinct from the qrdb `LoginCeremony`-backed
  `/api/v1/auth/login/{begin,finish}`, which is unaffected), but renamed
  under the IAM namespace to clear the path collision with xkms's own PIV
  login endpoint, following the same tenant-namespaced-mount convention as
  `/api/v1/iam/{tenant_id}/webauthn/*`. Both the module surface and the
  legacy listener serve the new path only — the old path is gone on both.
  SDK callers (TypeScript `rest.ts` `loginPIV()`, integration tests) were
  repointed.
- **`/api/v1/barrier/*` and `/api/v1/init/*` excluded from the module
  surface** (`pkg/api/rest.ModuleRoutes()`); the legacy listener keeps
  serving them for the standalone sealed-window boot path.

### Changed — BREAKING: ceremony claim-share is now a Begin/Complete handshake

- **Single-shot `CeremonyClaimShare` deleted** (tracks go-qrdb's regenerated
  qrdbv1 protos + SDK). The officer Shamir-share claim is now a two-step
  challenge-response handshake, mirroring qrdb's officer-share wire contract
  verbatim on every transport:
  - **SDK** (`Security().Ceremony()`, the re-exported qrdb `CeremonyClient`):
    `CeremonyClaimShare(*ClaimShareRequest)` →
    `CeremonyBeginClaimShare(*BeginClaimShareRequest)` (returns
    `nonce_hex` + `expires_at`) then
    `CeremonyCompleteClaimShare(*CompleteClaimShareRequest{Username,
    NonceHex, Signature})` (returns `share_index` + `share_json`, released
    exactly once). The caller signs the nonce with the private key of their
    enrolled officer certificate; the server derives the caller thumbprint
    from the mTLS peer chain — it never travels on the wire.
  - **REST**: `POST /api/v1/init/claim-share` and the qrdb wire mirror
    `POST /v1/ceremony/claim-share` are replaced by
    `/api/v1/init/officer-share/{begin,complete}` and
    `/v1/ceremony/officer-share/{begin,complete}`.
  - **gRPC** (`quicbaas.ceremony.v1.Ceremony`): `CeremonyClaimShare` RPC
    replaced by `CeremonyBeginClaimShare` / `CeremonyCompleteClaimShare`.
  - **Native QUIC**: `SubOpCeremonyClaimShare` (0x05) is now
    `SubOpCeremonyBeginClaimShare` (0x05) plus
    `SubOpCeremonyCompleteClaimShare` (0x09).
  - **qrdb-QUIC bridge**: `ceremony.ClaimShare` replaced by
    `ceremony.BeginClaimShare` / `ceremony.CompleteClaimShare`.
  - **MCP**: tool `qrdb_ceremony_claim_share` replaced by
    `qrdb_ceremony_begin_claim_share` / `qrdb_ceremony_complete_claim_share`
    (signature and released share travel base64-encoded).
  - **CLI**: `quicbaas init claim-share` keeps its flags but now drives the
    full handshake internally, signing the challenge nonce with `--tls-key`.
  - Service facade (`service.CeremonyServicer`), sealed-window allowlists
    (new `ceremony-officer-share-begin/complete` verbs mirroring qrdb's
    canonical 34-verb table), and gRPC PEP mappings
    (`ceremony:claim_share_begin` / `ceremony:claim_share_complete`)
    migrated accordingly. There is no compatibility shim — the stack is
    pre-v1 with exactly one wire format.

### Added

- **Barrier enable-encryption (live on all transports).** System (`POST /api/v1/barrier/enable-encryption`) and per-tenant (`POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption`) routes enable migration from cleartext (`none`) to encrypted barriers across REST, gRPC, QUIC, MCP, Unix, and embedded transports. Driven by the embedded qrdb node (wired as `EncryptionMigrator`), not the qrdb `BarrierClient` transport, providing direct cluster coordination and fail-safe recovery. Supports passphrase, Shamir, TPM2, PKCS#11, AWS KMS, GCP KMS, Azure Key Vault, and Vault strategies. Request fields: `strategy`, `passphrase`, `shamir_threshold`, `shamir_total`, `mechanism`, `availability`.
- **Tenant quota and entitlement governance.** All qrdb `Quotas` fields (MaxDatabases, MaxRaftGroups, MaxReplicasPerGroup, MaxTotalReplicas, MaxCollections, MaxKeys, MaxStorageBytes, MaxDatabaseSizeBytes, MaxUsers, MaxRoles, MaxCertificates, MaxBackups, MaxVectorDims, MaxTSMetrics, MaxDedicatedWALGroups, SystemBarrierQuota) and `Entitlements` fields (AllowedIsolationModes, AllowedBarrierModes, AllowDedicatedRaft, AllowVolumeStorage, AllowSystemManagedBarrier, AllowDedicatedWAL) are settable at tenant create-time via the provisioner and via dedicated update endpoints on REST/gRPC. Quota enforcement blocks operations that exceed limits; entitlement checks reject feature use (e.g., enable-encryption to a disallowed barrier mode) with 403.
- **Expanded tenant isolation modes.** Quicbaas config now allows `keyspace`, `barrier`, `volume`, and `raft` isolation modes (matching qrdb's full set), enabling per-tenant enable-encryption. Raft mode requires clustered quicbaas deployment.
- **Entitlement-denied error mapping.** `ErrSharedBarrierImmutable` / `EntitlementError` → 403 Forbidden; `ErrMigrationInProgress` / `ErrMigrationSourceNotNone` → 409 Conflict; invalid mechanism/availability → 400 Bad Request; `ErrMigrationProviderUnavailable` → 501 Not Implemented. Consistent across all transports.
- **Barrier in embedded mode.** Full barrier surface (status, init, unseal, seal, rekey, recovery, enable-encryption) now wired in embedded mode via the embedded qrdb node, eliminating the prior "NilServiceError" limitation. CLI commands like `quicbaas barrier status --transport embedded` and the init ceremony operate without a network listener.

### Changed — `seal` → `barrier` rename (cross-project)

- **Encryption-at-rest unified with go-qrdb / go-xkms.** The barrier
  subsystem now wraps every Pebble-backed data-plane store via the
  encrypting `vfs.FS` from `go-qrdb/pkg/seal/encfs`; sealed-window
  middleware enforces a canonical allowlist of `/v1/health` +
  `/v1/barrier/*` + `/v1/init/*` + `/v1/bootstrap/*` while the barrier
  is sealed. See `docs/architecture/barrier.md`.
- **REST surface renamed** `/api/v1/seal/*` → `/api/v1/barrier/*` (16
  endpoints) plus 7 new `/api/v1/init/*` and 4 new `/api/v1/bootstrap/*`
  endpoints. Legacy `/seal/*` paths retained for one release with
  `Deprecation:` and `Sunset:` headers. Migration table at
  `docs/migrations/seal-to-barrier.md`.
- **CLI surface split**: `quicbaas seal …` → `quicbaas barrier …` /
  `quicbaas init …` / `quicbaas bootstrap …`, mirroring `xkmsctl`
  verb-for-verb. `quicbaas seal …` retained for one release as a
  deprecation alias that warns to stderr.
- **TypeScript SDK** ships `BarrierClient`, `CeremonyClient`,
  `BootstrapClient` interfaces with REST + stub grpc-web/quic/mcp
  transports. `SealClient` retained as a deprecation alias that
  `console.warn`s on construction.
- **`defaultSealManager` deleted** in favour of the SDK-backed
  `pkg/barrier/manager.go` adapter. `pkg/interfaces/managers.go`
  exposes both `SealManager` (alias) and `BarrierManager` for one
  release.
- **`cmd/quicbaasd/init.go`** added — daemon-side init command mirroring
  `xkmsd init` (`--barrier-strategy`, `--shares`, `--threshold`,
  `--so name:KIND:path`, `--trusted-ca`).
- **Frontend** `BarrierView.svelte`, `CeremonyView.svelte`, and
  `BootstrapView.svelte` replace `SealView.svelte`. Sidebar label
  "Seal" → "Barrier" with "Ceremony"/"Bootstrap" sub-items under
  SECURITY.
- **`make verify-naming`, `make verify-cli-flags`, `make verify-api-shapes`**
  added — CI watchdogs that catch operator/SDK drift across QuicBaaS,
  go-qrdb, and go-xkms.

### Breaking

- **No in-place upgrade from pre-encfs data directories.** A new
  format-version sentinel at `<data-dir>/seal/__sys/barrier_format_version`
  is checked at startup; daemons refuse to start on a non-empty data
  dir without the sentinel. Operators must export and re-import. See
  `docs/migrations/seal-to-barrier.md`.

### Changed

- **Agent identity is now mTLS-based.** The one-shot enrollment-token
  bootstrap was removed. `quicbaas agent deploy` issues an X.509 client
  certificate for each deployed daemon via the server-embedded CA and
  uploads it to `$DATA_DIR/agent.crt`, `$DATA_DIR/agent.key`, and
  `$DATA_DIR/server-ca.crt`. The daemon presents the cert on the TLS
  handshake when calling `POST /api/v1/agents/register`; the server
  returns OAuth2 client credentials the daemon uses for heartbeat and
  event streams thereafter.
- **Renamed `--central` → `--server` everywhere.** The central
  orchestrator terminology on the agent wire protocol, CLI, GUI, and
  SDK DTOs is now uniformly "server". `agent.Config.CentralAddr` is
  now `agent.Config.ServerAddr`, `deploy.Spec.Central` is now
  `deploy.Spec.Server`, and the Svelte deploy dialog input is now
  labelled "Server URL".
- **`agent serve` requires `--agent-cert`, `--agent-key`, `--server-ca`.**
  The `--enrollment-token` and `--device-cert` flags have been removed.

### Removed

- `iam role grant agent-runtime` CLI command.
- `iam enrollment-token issue` CLI command.
- `POST /api/v1/agents/enroll` REST endpoint.
- `POST /api/v1/agents/enrollment-tokens` REST endpoint.
- `iam.AgentEnrollmentService` and `iam.MemoryEnrollmentTokenStore`.
- `sdk/go/client/iam.IssueEnrollmentToken` and `IssueEnrollmentTokenRequest/Response`.
- The `enrollment_token` field on the `EnrollRequest` proto message.

## [1.0.0] - 2026-02-03

### First Public Release

QuicBaaS is a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus and PebbleDB storage. Designed for speed, strong consistency, horizontal scalability, security, and operational simplicity.

### Core Features

#### Storage & Data
- **Multi-Raft Consensus** - Distributed consensus using QuicRaft with configurable write modes (sync, WAL, batched)
- **PebbleDB Storage Engine** - High-performance LSM-tree storage with MVCC support
- **Key-Value Operations** - Full CRUD with transactions, batch operations, and prefix queries
- **Vector Database** - HNSW and IVF indexing for similarity search
- **Time-Series Database** - Metrics storage with retention policies and downsampling
- **Object Storage** - Multi-backend support (S3, GCS, Azure, MinIO) with tiering

#### Security
- **IAM/ABAC** - Identity and access management with attribute-based access control
- **KMS Integration** - Support for AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault, PKCS#11
- **Secrets Engine** - Versioned secrets with rotation and watch API
- **Encryption** - At-rest and in-transit encryption with FIPS 140-3 compliance option
- **Seal/Unseal** - Vault-style initialization with Shamir secret sharing
- **mTLS** - Mutual TLS for all inter-node communication

#### APIs & Protocols
- **gRPC** - High-performance RPC with streaming support
- **REST/HTTP** - RESTful API with SSE for real-time updates
- **QUIC** - Low-latency UDP-based transport
- **WebSocket** - Real-time communication with MQTT, AMQP, STOMP subprotocols
- **MCP** - Model Context Protocol for AI/LLM integration (270+ tools)
- **Unix Socket** - Local IPC for embedded use cases

#### AI & Machine Learning
- **RAG Pipeline** - Retrieval-Augmented Generation with chunking, embedding, and ranking
- **Embedding Providers** - Support for OpenAI, Ollama, and custom providers
- **Vector Search** - Semantic similarity search with configurable distance metrics

#### Messaging & Real-Time
- **Push Notifications** - Channel-based pub/sub with delta publishing
- **Watch System** - Real-time change notifications with delta computation
- **Message Deduplication** - Content-based and ID-based deduplication

#### Operations
- **Distributed Scheduler** - Cron-based job scheduling across the cluster
- **Backup & Recovery** - Point-in-time recovery with multiple storage backends
- **Cache Layer** - L1 in-memory cache with optional L2 Redis
- **Cluster Management** - Dynamic node addition/removal, rebalancing

#### SDKs
- **Go SDK** - Full-featured client with all transports
- **JavaScript SDK** - Browser and Node.js support with 100% test coverage

#### CLI
- **308 Commands** - Comprehensive CLI across 24 categories
- **Multiple Transports** - gRPC, REST, QUIC, Unix socket, embedded

### Build Profiles
- **Minimal** - Core KV + gRPC only
- **Embedded** - Single-node, no clustering
- **Standard** - Cluster mode, no AI features
- **Cluster** - All features, no cloud KMS
- **Enterprise** - All features + cloud KMS
- **Full** - Everything including HSM and quantum-safe crypto

### Documentation
- Comprehensive documentation in `/docs/`
- API references for all protocols
- Production deployment checklist
- Security hardening guides

### Testing
- 88%+ unit test coverage
- Integration test suite with Docker
- Benchmark suite for performance validation

---

## [0.1.0-alpha] - 2025-01-01

### Initial Development Release
- Core key-value storage
- Basic Raft consensus
- gRPC API
- Foundation for future development
