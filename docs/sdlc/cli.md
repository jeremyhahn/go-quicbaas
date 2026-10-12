# SDLC — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every verb and flag of `quicbaas sdlc` (`cmd/quicbaas/sdlc*.go`). The wire
> surface behind each verb is [api.md](api.md). Conflicts with this document
> are defects.

## quicbaas sdlc

Bind a forge repository and its review policy to an orchestration project, so
that issues labelled with the binding's trigger label become tasks the
Software Factory branches, implements, reviews and merges. The project itself
(repository URL, commit signing, context repositories) is
`quicbaas project`; tasks, jobs and sessions are `quicbaas orchestrator`, and
readmitting a blocked task is `quicbaas orchestrator task readmit`.

Bindings are tenant-scoped: every command operates within the tenant named by
the global `--tenant` flag, except the two `worker-image` verbs, which act on
the host and take no tenant.

Binding a project also **onboards** its repository: the webhook that delivers
to this node and the label vocabulary the binding declares are provisioned on
the forge, idempotently, with nothing committed to the repository.
`conformance` reports whether a repository is still in that state; `onboard`
repairs it.

| Command | Arguments | Description |
|---------|-----------|-------------|
| `bind <project-id>` | exactly 1 | Attach a forge binding to a project from a file, and onboard its repository |
| `unbind <project-id>` | exactly 1 | Detach a project's forge binding |
| `binding <project-id>` | exactly 1 | Show a project's forge binding |
| `delivery <task-id>` | exactly 1 | Show what a task's attempt delivered |
| `onboard <project-id>` | exactly 1 | Provision the bound repository's webhook and labels on its forge |
| `conformance <project-id>` | exactly 1 | Report whether a project is correctly onboarded, and what is wrong. Read-only |
| `open-work <project-id>` | exactly 1 | List a bound project's open factory pull requests and labelled issues |
| `reviewer <project-id> <pull-request>` | exactly 2 | Show which reviewer profile reviews a pull request |
| `reviewer set <project-id> <pull-request> <profile>` | exactly 3 | Record an operator override naming one of the binding's profiles |
| `reviewer clear <project-id> <pull-request>` | exactly 2 | Return the pull request to the project default |
| `issue create <project-id>` | exactly 1 | File an issue on the bound repository through the binding's token |
| `collaborators <project-id>` | exactly 1 | List the repository's owner and collaborators — who a ruler may be |
| `adopt <project-id> <issue>` | exactly 2 | Hand an issue back to the factory, optionally resuming its open pull request |
| `worker-image status` | none | Show the worker image refresh's status and last pass |
| `worker-image refresh` | none | Queue a worker image refresh pass |

| Flag | Scope | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--output` | group | string | `table` | `json`, `yaml` or `table` |
| `--file`, `-f` | `bind` | string | — | The project binding as YAML or JSON (**required** for `bind`) |
| `--title` | `issue create` | string | — | Issue title, one line (**required**) |
| `--body` | `issue create` | string | — | Issue body |
| `--body-file` | `issue create` | string | — | Read the issue body from a file; exclusive with `--body` |
| `--label` | `issue create` | string, repeatable | — | A label to apply; each must exist on the repository |
| `--parent` | `issue create` | int | — | The issue this one was split out of |
| `--pull-request` | `adopt` | int | — | An open pull request the task resumes, on its own head branch |
| `--ruling` | `adopt` | string | — | Appended to the task's contract and posted on the issue |
| `--wait` | `worker-image refresh` | bool | `false` | Follow the pass until it finishes |
| `--wait-timeout` | `worker-image refresh` | duration | `1h` | How long `--wait` follows the pass |

```bash
quicbaas sdlc bind go-truststrap --file go-truststrap-binding.yaml
quicbaas sdlc binding go-truststrap --output json
quicbaas sdlc conformance go-truststrap
quicbaas sdlc onboard go-truststrap
quicbaas sdlc open-work go-truststrap
quicbaas sdlc delivery 1788567351228350294
quicbaas sdlc reviewer go-truststrap 42
quicbaas sdlc reviewer set go-truststrap 42 qwen
quicbaas sdlc reviewer clear go-truststrap 42
quicbaas sdlc issue create go-truststrap --title "Split: codec half" --label task:ready --parent 41
quicbaas sdlc collaborators go-truststrap
quicbaas sdlc adopt go-luks2 141 --pull-request 194 --ruling "Land #194 as it stands."
quicbaas sdlc unbind go-truststrap
quicbaas sdlc worker-image status
quicbaas sdlc worker-image refresh --wait
```

## Errors raised before any request is sent

| Error | Raised by |
|---|---|
| `BindingFileRequiredError` | `bind` without `--file` |
| `BindingFileError` | `bind` with an unreadable or unparseable file; names the path |
| `IssueTitleRequiredError`, `IssueBodyConflictError`, `IssueParentError`, `IssueBodyFileError` | `issue create` with no title, both `--body` and `--body-file`, a bad parent, or an unreadable body file |
| `IssueNumberError` | `adopt` with an issue that is not a positive integer |
| `PullRequestNumberError` | `reviewer` with a pull request that is not a positive integer |

Two more are raised after the answer arrives: `SDLCNotConformantError` makes
`conformance` exit non-zero when the project is not conformant, and
`WorkerImageWaitTimeoutError` ends `worker-image refresh --wait` when the pass
outlives `--wait-timeout`.

## Verb notes

`conformance` exits non-zero when the project is not conformant, so it gates a
script the way a test command does — and it still prints the report either
way, because an exit code that says "something is wrong" without saying what
is the thing this verb exists to replace. The classes it reports are in
[architecture.md](architecture.md#what-conformance-checks).

`issue create` is audited as `sdlc:issue_create`, refusals included, and is
sent once: an unanswered create is reported, not repeated. `collaborators`
lists the accounts a binding's `rulers` may name.

`adopt` is for an issue no live task holds — its task completed, or triage
holds it — and is walked through in
[user.md](user.md#adopting-an-issue-no-task-holds).

`reviewer` shows which reviewer profile reviews a pull request, whether an
operator chose it, whether its verdict counts toward merge, and the status
context it publishes under. See
[review.md](review.md#the-per-pull-request-override).

The `worker-image` verbs act on the host's worker image refresh and need the
cross-tenant `worker_images:*` permissions; see
[the worker image refresh](../agent-fleet/worker-image.md).

The `table` rendering of a binding is one line per decision an operator
checks: project, forge, trigger label, strategy, worker, worker prompt append
(its size, not its text), verification commands, review policy, triage, triage
prompt append, whether triage may close, the default reviewer and each
alternate profile, review rounds, merge train, issue clustering, discovery,
rulers, whether a webhook secret is configured, and the pinned SSH host key
rendered as its algorithm and comment only. `delivery` prints the branch, the
base, the head SHA, the pull request, then one line per review (reviewer,
verdict, head, round) and one per check.

The binding file's fields are documented in
[engineering.md](engineering.md#the-project-binding); the wire shape is in
[api.md](api.md#the-sdlc-module).
