# SDLC — User guide

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Binding a project to a forge, following one issue to a merged pull request,
> and unblocking the factory from the attention inbox. Every verb is in
> [cli.md](cli.md); the design is [architecture.md](architecture.md).

## Bind a project and follow one issue

The SDLC module turns a labelled forge issue into a branch, a pull request, a
review and a merge. The design is [architecture.md](architecture.md); this is the order the
steps are done in on a live node.

1. **Register the Project** (once), with its repository, default branch,
   commit identity, signing key, the repositories the worker reads beside it,
   and the number of issues that may run at once. No `default_workflow` — an
   `sdlc` job has no workflow, and `bind` turns autonomy on for you.

   ```
   quicbaas project create --tenant acme --name go-truststrap \
     --git-repo ssh://git@git.example.com/example/go-truststrap.git \
     --git-branch main \
     --git-author-name factory-bot --git-author-email factory-bot@example.com \
     --git-signing --git-signing-key go-truststrap-signing \
     --context-repo name=conventions,url=https://git.example.com/example/conventions,ref=main \
     --workspace-path /var/lib/quicbaas/projects/go-truststrap \
     --max-concurrency 1
   ```

   `--git-signing` is the switch and `--git-signing-key` is the key: naming
   the key alone leaves signing off, `create` accepts it silently, and `bind`
   then refuses the project because its commits would be unsigned.

   `git.example.com` is a placeholder throughout this walkthrough. The forge
   host is the deployment's own and is never hardcoded in this repository.

   `--workspace-path` is not optional in practice. It is the directory the
   attempt's checkout is mounted from, and a project without one is created
   happily and then refused by the runner on its first attempt with `project
   <project-id> has no workspace for its worker's checkout`
   (`runnerRun.workspaceRoot`). Give it an absolute directory under a root
   the daemon admits in `orchestration.sandbox.allowed_mount_roots`.

   The context repositories are cloned **by the worker's container**, beside
   the project (`/workspace/conventions` next to `/workspace/go-truststrap`), not
   by the daemon, with the project's credentials: register each by its
   `https://` URL (cloned with the forge token) or by an `ssh://` URL on the
   project's own ssh host (the one host key the binding pins). `bind` refuses
   an `http://` context repository, which the token is never sent over, and an
   ssh one on any other host, naming the repository. The same check runs again
   every time a worker's workspace is opened, so a context repository added
   with `project update` after the bind, or a host since dropped from
   `sdlc.repo_hosts`, parks the task for an operator with the repository named
   rather than failing inside the worker. `--autonomous` sets the switch by hand for a project that is
   not going to be bound; see
   [Autonomous projects](../orchestration/autonomy-and-dispatch.md).

2. **Store the forge token and webhook secret** in the tenant's secrets
   (`quicbaas secrets put <path>`), then **bind**:

   ```
   quicbaas sdlc bind <project-id> --tenant acme --file go-truststrap-binding.yaml
   ```

   ```yaml
   # go-truststrap-binding.yaml
   forge:
     type: gitea                                # gitea | github | gitlab
     base_url: https://git.example.com/api/v1   # the API ROOT, version segment included
     owner: example
     repo: go-truststrap
     trigger_label: task:ready
     labels: {in_progress: factory:in-progress, in_review: factory:in-review,
              merged: factory:merged, blocked: factory:blocked,
              needs_info: factory:needs-info, escalated: factory:escalated}
     token_secret_ref: sdlc/forge/go-truststrap/token
     webhook_secret_ref: sdlc/webhook/go-truststrap
     ssh_host_key: ssh-ed25519 AAAAC3Nza...
                                # the forge's OWN host key, from
                                # `ssh-keyscan -t ed25519 -p <port> <host>`
                                # with the leading host field dropped.
                                # Required when git.repo_url is ssh://
   strategy: github-flow               # github-flow | trunk-based | gitflow
   worker:
     backend: claude
     model: claude-opus-5-5
     image: quicbaas-software-factory-worker:0.11.0-alpha
                                # `make build-worker-image`; the image the
                                # runner clones, verifies and publishes in
   verification: [make build, make test, make lint]
   review:
     required_approvals: 1
     require_independent_reviewer: true
     require_green_checks: true
     reviewers: {codex: openai}
   max_review_rounds: 3
   ```

   Bind refuses a project whose repository host or signing policy the
   pipeline could not deliver, and a binding naming a strategy or forge this
   build does not carry — each refusal names what is available. Only `gitea`
   has an adapter today (GitHub #617, GitLab #618). It registers no workflow;
   the one thing it sets on the project is `autonomous: true`.

   **Bind also onboards the repository.** It provisions, on the forge, the
   webhook that delivers to this node and the labels the binding declares —
   nothing is committed to the repository, so changing how orchestration
   works never means editing every repository it drives. It is idempotent,
   and it never renames, recolours or removes a label that already exists.
   The node's `sdlc.webhook.public_url` is what the webhook points at.

   Check it, and repair it:

   ```
   quicbaas sdlc conformance <project-id> --tenant acme   # reports; exits non-zero when not conformant
   quicbaas sdlc onboard <project-id> --tenant acme       # repairs what provisioning can
   ```

   `conformance` names each problem distinctly — a missing, disabled or
   misdirected webhook; a label the repository does not define; an
   orchestration workflow still committed to the repository; a protected
   branch that does not require the status contexts QuicBaaS writes — with
   the fix for each. It changes nothing. See
   [architecture.md](architecture.md#onboarding-a-repository).

   `ssh_host_key` is what the worker's ssh verifies the forge against. A
   project cloned over `ssh://` with no key pinned is refused before its
   first container starts; there is no accept-new. See
   [engineering.md](engineering.md#ssh_host_key--the-forge-is-pinned-never-learned).

   `base_url` is the forge's **API root**, version segment included
   (`/api/v1` for Gitea) — not the site origin. The origin alone satisfies
   `ForgeConfig.Validate`, which only asks that the field name something, and
   then 404s every call the adapter makes.

   The six display labels in `labels:`, and the trigger label, must **already
   exist on the repository**. `GiteaBackend.resolveLabelIDs` resolves names
   through the repository's own label vocabulary and creates nothing, so a
   binding naming a label the repository does not define binds cleanly and
   then fails at the first label move with `repository <owner>/<repo> defines
   no label named <name>`. Create the vocabulary on the repository before
   binding.

3. **Point the forge's webhook** at `POST /api/v1/sdlc/webhook/<project-id>`
   with the same secret (optional; the poller sweeps on
   `orchestration.sweep_interval` regardless).

4. **Label an issue** with the binding's trigger label (`task:ready`).

5. **Follow it** through the engine's surfaces:

   ```
   quicbaas orchestrator task list --project <project-id>      # Task with source forge_issue owner/repo#n, runner sdlc, spawned_job_id
   quicbaas orchestrator job status <job-id>                    # initializing → executing → validating → reviewing → completing
   quicbaas orchestrator sessions                               # THE worker session — an sdlc job has exactly one
   quicbaas sdlc delivery <task-id>                             # branch, head, pull request, reviews, checks
   quicbaas sdlc conformance <project-id>                       # is the forge still configured for us to drive it?
   ```

   The issue shows the display labels move (`in_progress` → `in_review` →
   `merged`); the pull request and head appear as the Task's artifacts.

   A second labelled issue past `max_concurrency` is adopted as a Task and
   stays `pending` with `dispatch_reason` saying `project at capacity: 1 of 1
   concurrent jobs are running`. It dispatches on its own when the first job
   ends — no operator action.

6. **When the task stops** — for any reason at all — the issue gets one
   comment naming the outcome and the evidence, and its label moves off
   `in_progress`. The comment's `**Outcome:**` line says which ending it was:
   `escalated` (review did not converge), `needs_info`, `blocked` (an
   environment fault, or a condition the task waits on), `abandoned` (the node
   died with no attempts left) or `exhausted` (the budget is spent). The full
   table, including which endings a readmit can undo, is in
   [architecture.md](architecture.md#outcomes-every-way-a-task-stops-and-what-reaches-the-issue).

   A park also says **Unblocks when** and **Cleared by**, both derived from
   the park's typed unblock condition — see
   [the block lifecycle](architecture.md#the-block-lifecycle).

   - **Cleared by the owner / an operator** — answer on the issue, then
     `quicbaas orchestrator task readmit <task-id>`; the readmission is
     commented on the issue too, the label is cleared, and the next attempt
     continues the branch and reads the discussion.
   - **Cleared by the Software Factory's own sweep** — nothing to do. The
     condition (`pipeline_green`, `forge_issue_state`) is checked on every
     sweep and the task returns to the queue on its own. A task whose
     attempt budget is spent is the exception: the comment says so, and it
     needs a fresh issue or a raised budget.

   An issue still carrying `in_progress` with no comment means the daemon
   could not reach the forge — look for `concluded … but was not surfaced` in
   the daemon log. A park the RUN decided is the one conclusion that writes
   nothing through that seam — the `Blocked` moment already posted it with the
   worker's own text — so for those look for `the park reason did not reach
   issue` instead.

   **When the worker changed nothing**, the job record says why. Every turn
   is on the job as an `sdlc:agent_turn` event carrying the reply itself:

   ```
   quicbaas orchestrator job status <job-id>    # sdlc:agent_turn — attempt, elapsed, tool_uses, outcome, the reply
   ```

   The `outcome=` field on that event, and on the `sdlc:verification_skipped`
   event beside it, is the whole diagnosis:

   | outcome | what it means | what to do |
   |---|---|---|
   | `question` / `owner_ruling` | the worker declared a stop; the task is blocked `needs_info` under an `owner_ruling` condition and its question is the issue comment | answer on the issue, then `task readmit` |
   | `environment` | the worker could not work in the container it was given; the task is blocked under an `operator_repair` condition | fix the workspace, image or network, then `task readmit` |
   | `unreadable` | the worker declared a stop with a code this build does not know | read the reply on the event; it is a charter/binary version mismatch |
   | `silent` | the worker answered with no text, twice | look at the backend and the container: nothing recorded a reason because nothing was said |
   | `claimed_work` | the worker reported edits on a tree it did not touch. **The only one still treated as a failure of the work** | read the reply; usually the worker worked outside the checkout |

   Neither condition a worker's stop produces is machine-checkable, so the
   comment always says **Cleared by the owner** or **Cleared by an
   operator** — the sweep reports these tasks rather than releasing them,
   and the readmission is yours to make once you have answered.

   Every one of these spends the attempt it ran — a turn that reached the
   model made a judgement about the work, which is what the budget counts
   (`orchestration/task_attempt.go`). What changed is that only
   `claimed_work` puts the task back in the queue: the rest PARK it, so the
   remaining budget is spent on the attempt that follows the answer instead
   of on re-running the question.

   If the task is out of budget and the fix is elsewhere (a dispatcher bug,
   a broken account), fix that and re-apply `task:ready` to the issue: a task
   that spent its budget is terminal, so its source reservation no longer
   binds and the next sweep adopts a fresh task, recording the spent one in
   `metadata.predecessor_task`. Nothing has to be deleted.

   If the answer is that the task should not have been adopted at all,
   `quicbaas orchestrator task delete <task-id>` removes it and releases the
   issue's source reservation, so a later sweep can adopt the same issue
   again. It is refused while a job holds a live lease on the task — cancel
   the job first.

7. **If the daemon dies mid-attempt**: a standalone node owns its job leases
   in-process, so the restarted node resumes its own interrupted job at the
   state its runner names (`JobResumer`, or the runner's first state), and
   the module's `Prepare`, idempotent for one attempt, re-plans the same
   branch and rewrites the same `Delivery`. In cluster modes the dead node's
   lease is foreign to its peers: the reaper
   (`orchestration.reap_lease_ttl`) concludes the job on the next sweep and
   requeues the task under its budget, and the next attempt's `Prepare`
   continues the pushed branch. There is nothing on the daemon's disk to
   reclaim either way — the dead attempt's container took its checkout with
   it. A requeued task is re-dispatched by the task dispatcher on its next
   sweep, with no operator action.

## Unblocking the factory: the attention inbox

Everything above this line — the readmit table, the outcome table, the
per-condition "Cleared by" column — is how an operator unblocks ONE task
they already know about, from the issue or the CLI. The attention inbox is
the other half: it finds every task the factory has parked and every fault
in the factory's own operation, in one place, without anyone having to know
which project or which issue to look at first.

```bash
# Every parked task and environmental alert the tenant carries, paged and
# faceted by project and kind.
curl -s "https://<node>/api/v1/orchestrator/attention?tenant_id=<tenant>" \
  --cert agent.crt --key agent.key --cacert ca-chain.crt | jq .

# Apply a ruling: the comment is posted to the forge issue first, then the
# task moves.
curl -s -X POST \
  "https://<node>/api/v1/orchestrator/attention/<task-id>/resolve?tenant_id=<tenant>" \
  -d '{"action": "approve_recommendation"}' \
  --cert agent.crt --key agent.key --cacert ca-chain.crt
```

`action` is one of:

| action | what it does |
|---|---|
| `approve_recommendation` | posts the worker's own recommendation as the ruling, verbatim, and readmits the task |
| `answer` | posts the operator's own free text (`"answer": "…"`) as the ruling and readmits the task |
| `allow_another_round` | grants one more review round beyond the project's `max_review_rounds` for this task only, then readmits it; optional `"answer": "…"` scopes the round and is posted in the same ruling comment |
| `close_as_done` | takes the park labels off the forge issue, closes it and completes the task **without** readmitting it — no further work is needed. An optional `answer` is posted as the evidence |
| `readmit` | posts a minimal comment and readmits the task, for a condition already satisfied outside the forge |
| `note` | posts the operator's free text (`"answer": "…"`) and leaves the task where it is — a partial ruling, context, or a decision recorded on a parked project. It is the one action that also reaches a **live** task (pending, claimed or in progress): a scope change for queued or running work lands on the issue, and the worker re-reads the issue's discussion at its next dispatch |

Every resolution is ordered comment-then-move: the ruling reaches the forge
issue before the task's state changes, so an issue never reads `blocked`
after the factory has already acted on it.

Every action that readmits also **hands the issue back to the factory**
before the task moves: the trigger label goes on and every park label comes
off — the binding's `needs_info`, `escalated` and `blocked` labels and the
portfolio hold `task:blocked`. The operator giving the ruling is the person
those labels were waiting for. The readmitted task's first turn re-reads the
issue, so the worker sees the ruling.

### Every waiting issue has a parked task

The inbox is built from parked tasks, so every issue that waits on a person
has one. The issue source offers each open, untriggered issue carrying a park
label as a task **adopted parked** (`TaskSourceSpec.Park`): kind
`needs_info`, `escalated` or `blocked` after its label, condition
`owner_ruling`, and triage's last verdict as the question. The source
reservation keeps this idempotent — an issue whose task is still live adopts
nothing, and one whose task ended gets a fresh parked task — so the interval
sweep covers both a new triage verdict and every issue labelled before this
existed. A parked adoption spends no adoption slot: it runs nothing until
someone rules.

`close_as_done` is the one path from `blocked` straight to `completed`
(`CloseBlockedTask`); a worker's own completion report for a task that has
since been parked is still refused.

### Answering on the issue: automatic readmission

You do not have to use the inbox or the API to unblock a task parked for you.
A task parked `needs_info` or `escalated` (or on an `owner_ruling`
condition) is **readmitted automatically when an authorized ruler comments on
its issue** after the park:

- **Who rules:** the repository's owner account; the binding's `rulers`
  (picked in the project's binding editor from the repository's
  collaborators, each validated at bind time as the owner or a collaborator);
  and the node's `sdlc.rulers` — `sdlc.rulers.tenants.<tenant>` when the
  tenant has an entry, else `sdlc.rulers.default`, else `agent-company`
  (`sdlc.DefaultRulers`). A binding can only add rulers; a tenant entry
  replaces the node default for that tenant. Project agents, workers, reviewers and the task's
  own bot do not — nor does any comment carrying the factory's
  `<!-- quicbaas-sdlc:… -->` marker, whichever account posted it.
- **When:** at once, when the project's webhook delivers the
  `issue_comment` event (the `issues` hook family already includes it); the
  unblock sweep reads the same comments on every pass
  (`DefaultUnblockSweepInterval`), so a missed delivery costs at most one
  sweep.
- **In the UI:** Projects → Edit → *Issue-driven delivery (SDLC)* has a
  *Rulers* picker listing the repository's collaborators (the owner always
  rules and is shown fixed). A saved ruler who has since lost access is
  flagged, because the next save would be refused. The same panel's *File
  issue* action files a follow-up on the bound repository — title, body,
  "hand to the factory" (the trigger label) and a parent issue — through the
  binding's token, audited like the CLI and API verbs.
- **Audit and idempotency:** each readmission records an `owner_ruling:<comment-id>`
  URL artifact on the task naming the ruler, the comment and the issue. One
  comment readmits a task once: repeated comments, a repeated delivery, or the
  webhook and the sweep racing each other all yield a single readmission, and
  a task that parks again later waits for a new ruling.

In the inbox a parked task whose issue already carries such a comment is
flagged **Ruling posted**; every parked task shows its recorded question, a
link to its issue, and a **Readmit** button.

The inbox always loads the complete set, every page of it; the project and
kind dropdowns narrow the cards on screen only, so the sidebar badge, the
dashboard card and the alert banner keep counting everything.

The listing also reports environmental alerts — facts about the factory
itself, not about any one task's work — as separate item kinds:
`agent_offline` (a fleet node the engine has not heard from),
`account_error` (a pooled provider credential whose last refresh failed),
`barrier_sealed` (your tenant's own storage barrier, initialized and sealed
— a tenant with no per-tenant barrier runs under the system barrier and is
never reported; the system barrier itself would take the whole API down
before this listing could render anyway), and `project_idle` (an autonomous, unparked project with
parked work and nothing pending or in progress: the factory is idle there
until someone rules). These have no `resolve`
action: the fix is outside the factory (repair or remove the agent, rotate
the credential, unseal the barrier, rule on the project's parked items), so
the item exists to be seen, not clicked.

The desktop and browser UI surfaces this as **Needs You** in the sidebar,
with a count badge; an environmental alert also renders as a dismissible
banner at the top of every page until it clears. See
[api.md](api.md#attention-inbox) for the full request/response shapes and
the MCP tool names (`quicbaas_orchestrator_attention_list`,
`quicbaas_orchestrator_attention_resolve`).

### Adopting an issue no task holds

The inbox and `task readmit` act on a **blocked** task. An issue that no live
task holds — its task completed while its pull request was still open, or
triage holds it with no task at all — is handed back with **adopt**:

```bash
quicbaas sdlc adopt go-luks2 141 --pull-request 194 --ruling "Land #194 as it stands."
```

Adoption creates the `forge_issue` task for the issue (superseding a finished
task that held it; refused with 409 while a live one still does — readmit that
one instead), takes every hold off the issue (needs-info, escalated, blocked,
the portfolio hold), puts the trigger label on, and posts the ruling. The
ruling is also appended to the task's contract.

**Name the pull request by number** to resume it: the task records the pull
request's head branch and base (`pull_request_branch`, `pull_request_target`
metadata) and its delivery works on that branch, never on one the strategy
would cut from the issue title — a rival branch the old commits can never be
pushed over. Without `--pull-request` the strategy plans the branch as usual.

The same verb is `POST /api/v1/sdlc/projects/{projectID}/adopt`
(`orchestrator:task_create`), `SDLCService/AdoptIssue` on gRPC and Unix, the
MCP tool `sdlc_adopt_issue`, `sdlc.Client.AdoptIssue` in the Go SDK and
`sdlc.adoptIssue` in the TypeScript SDK. A forge write that fails after the
task exists answers 502 naming the task, which runs regardless.
