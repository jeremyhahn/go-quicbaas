# Software Factory — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every verb and flag of the Software Factory command groups, with runnable
> examples: `quicbaas agents`, `quicbaas account`, `quicbaas project`,
> `quicbaas sdlc` and the planned `quicbaas prompts`. The orchestration
> engine's groups are in [../orchestration/cli.md](../orchestration/cli.md). Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

Output formats, global flags, paging and the `quicbaas orchestrator` and
`quicbaas fleet` groups are in
[../orchestration/cli.md](../orchestration/cli.md); the same flags apply here.

## Agents

`quicbaas agents list|get|create -f|update <id> -f|delete|used-by` and
`quicbaas agents pools list|get|create -f|update <id> -f|delete|used-by`
manage the tenant's [Agents and pools](agents.md). Definitions are YAML or
JSON. Deleting an Agent or pool that something still references is refused,
and the refusal lists every user.

## quicbaas project

Manage agentic **projects** — the unit that binds a workspace, a git
repository, per-agent permissions, and resource quotas together. The design
is [projects.md](projects.md).

Projects are tenant-scoped. Every subcommand operates inside the tenant named
by the global `--tenant` flag (default: `system`). The command group goes
through the SDK `Project()` client, so it is the CLI counterpart of the REST
routes under `/api/v1/projects` and of the project views in the desktop/web
GUI.

| Command | Description |
|---------|-------------|
| `create` | Create a project (`--name` required) |
| `list` | List every project in the tenant |
| `get [projectID]` | Show one project by ID, or by name with `--name` |
| `update <projectID>` | Update a project (only the flags you pass are applied) |
| `delete <projectID>` | Delete a project |
| `park <projectID>` | Close the project's intake; work in flight finishes ([projects.md](projects.md#parking-a-project)) |
| `resume <projectID>` | Reopen a parked project's intake |
| `grant-role <projectID>` | Set an agent's role in the project |
| `revoke-role <projectID>` | Remove an agent's access (sets role `denied`) |

`--output json|yaml|table` is available on the whole group (default `table`).

### quicbaas project create

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name <name>` | string | — | Project name, unique within the tenant (**required**) |
| `--description <text>` | string | `""` | Human-readable description |
| `--workspace-node <id>` | string | `""` | ID of the fleet node that owns the workspace |
| `--workspace-path <path>` | string | `""` | Filesystem path to the workspace on the agent host |
| `--host-affinity <host>` | string | `""` | Pin project operations to a preferred agent host |
| `--default-lead-agent <id>` | string | `""` | Default lead agent ID for workflow runs |
| `--git-repo <url>` | string | `""` | Remote git repository URL |
| `--git-branch <branch>` | string | `""` | Default git branch (e.g. `main`) |
| `--default-workflow <name>` | string | `""` | Tenant-scoped workflow a Task auto-spawns into (must be registered) |
| `--autonomous` | bool | `false` | Turn autonomous development on: the poller adopts work and the dispatcher starts jobs for this project |
| `--context-repo <spec>` | string (repeatable) | — | Repository the worker clones beside the checkout, as `name=<name>,url=<repo-url>,ref=<branch\|tag\|sha>` |
| `--git-author-name <name>` | string | `""` | Author name recorded on commits made from the workspace |
| `--git-author-email <email>` | string | `""` | Author email recorded on commits made from the workspace |
| `--git-signing` | bool | `false` | Sign commits with the KMS-held signing key (requires `--git-signing-key`) |
| `--git-signing-algorithm <alg>` | string | `ED25519` | `ED25519` or `ECDSA-P256` (FIPS) |
| `--git-signing-key <name>` | string | `""` | KMS signing key name |
| `--git-signing-key-scope <scope>` | string | `""` | KMS signing key scope: `user`, `tenant`, `service` |
| `--git-signing-key-owner <owner>` | string | `""` | KMS signing key owner |
| `--git-ssh-key <name>` | string | `""` | KMS SSH key an `ssh://` repository is reached with (defaults to the signing key) |
| `--git-ssh-key-scope <scope>` | string | `""` | KMS SSH key scope: `user`, `tenant`, `service` |
| `--git-ssh-key-owner <owner>` | string | `""` | KMS SSH key owner |
| `--spend-window name=N,size=D[,tokens=T][,usd=X]` | string (repeatable) | — | A spend window over every turn charged to the project; the set replaces the limits (go-quicbaas#1058) |
| `--clear-spend` | bool | `false` | Remove every spend window |
| `--gpu-minutes <n>` | int | `0` | Maximum GPU compute budget in minutes (0 = unlimited) |
| `--max-concurrency <n>` | int | `0` | Maximum concurrent workflow runs (0 = unlimited) |
| `--workspace-tier <tier>` | string | `""` | Storage tier of the sandboxed checkout: `persistent` or `ephemeral` ([storage-tiers.md](storage-tiers.md)); empty keeps the host bind mount |
| `--workspace-tmpfs-mb <n>` | int | `0` | Size of the ephemeral tier's tmpfs checkout in MiB (0 = unsized; ephemeral tier only) |
| `--container-memory-mb <n>` | int | `0` | Container memory limit in MiB (0 = the runtime's default) |
| `--session-rotation <policy>` | string | `""` | What a persistent worker session does when its pooled provider account rotates: `resume` (continue the same conversation; the default) or `fresh` (start a new conversation on the same branch). On update, not passing it leaves the policy unchanged. See [projects.md](projects.md#session-rotation) |
| `--metadata k=v` | key=value (repeatable) | — | Caller-defined annotations |

```bash
quicbaas project create --name web-api \
  --description "Public API service" \
  --git-repo https://git.example.com/acme/web-api.git --git-branch main \
  --spend-window name=daily,size=24h,tokens=1000000
```

`--git-signing` is the switch and `--git-signing-key` is the key: naming the
key alone leaves signing off, `create` accepts it silently, and
`quicbaas sdlc bind` then refuses the project because its commits would be
unsigned.

### quicbaas project update

Accepts every `create` flag plus:

| Flag | Type | Description |
|------|------|-------------|
| `--name <name>` | string | New project name |
| `--clear-description` | bool | Remove the description (an empty `--description` leaves it unchanged) |
| `--git-sha <sha>` | string | Record the latest observed commit SHA |
| `--clear-default-workflow` | bool | Remove the default workflow so Tasks are never auto-spawned |
| `--clear-context-repos` | bool | Remove every context repository from the project |

Fields you do not pass keep their current value. `--git-signing` is
tri-state: not passing it leaves signing unchanged. The quota and storage-size
flags are presence-based: not passing one leaves it unchanged, and passing `0`
lifts the cap or limit.

```bash
quicbaas project update proj_abc123 --gpu-minutes 120 --spend-window name=monthly,size=720h,usd=500
quicbaas project update proj_abc123 --clear-description
quicbaas project update proj_abc123 --git-signing=false --clear-default-workflow
quicbaas project update proj_abc123 --workspace-tier ephemeral \
  --workspace-tmpfs-mb 4096 --container-memory-mb 8192
```

### quicbaas project get

```bash
quicbaas project get proj_abc123
quicbaas project get --name web-api --output yaml
```

Passing neither a project ID nor `--name` is an error — the command will not
issue a request it cannot scope.

### Agent roles

| Role | Meaning |
|------|---------|
| `lead` | Full control over the project and its workflows |
| `worker` | Can execute tasks, cannot change project configuration |
| `observer` | Read-only |
| `denied` | Explicitly prohibited (equivalent to `revoke-role`) |

```bash
quicbaas project grant-role proj_abc123 --agent agent_abc --role lead
quicbaas project revoke-role proj_abc123 --agent agent_abc
```

An unrecognized `--role` is rejected locally before any request is sent.

### Transport support

`project` routes through the SDK like every other command group. Dedicated
Project handlers exist on **REST** (`sdk/go/transport/rest/project_handler.go`),
**gRPC** (`sdk/go/transport/grpc/project_handler.go`), **MCP**
(`sdk/go/transport/mcp/project_handler.go`) and the **embedded** transport
(eight `quicbaas.Project/*` routes in
`sdk/go/transport/embedded/transport.go:349-356`); **Unix** delegates to gRPC
and **QUIC** carries the REST surface over HTTP/3.

*Correction (2026-09-10): the absorbed page claimed the Project service was
"implemented only on the REST and embedded transports" and that "gRPC, QUIC,
Unix and MCP have no Project service handler yet". All six now have one; the
transport matrix in [api.md](api.md) agrees.*

```bash
quicbaas --address https://quicbaas.example.com:8443 project list
quicbaas project list --search web --sort name:desc --limit 50
```

`project list` takes the [paging flags](../orchestration/cli.md#paging-sorting-and-search).

## quicbaas account

Pooled provider accounts — one provider login an agent session can be spawned
against instead of the host's shared identity. The full model, the selection
strategies and the credential path are in [accounts.md](accounts.md).

| Command | Description |
|---------|-------------|
| `create` | Create an account over a tenant secret (`--credential-secret-ref`, `--credential-secret-file`) or an inline credential (`--credential-file`); `--provider`, `--display-name` |
| `list` | List the tenant's accounts, filterable by `--provider`; takes the [paging flags](../orchestration/cli.md#paging-sorting-and-search) |
| `get <accountID>` | Show one account (identifiers only; never the credential) |
| `update <accountID>` | Change the display name, the project scope (`--project` / `--clear-project`), the refresh policy, the billing (`--billing`, `--monthly-fee`) or the spend policy (below) |
| `set-default <accountID>` | Make this the provider's default account |
| `rotate <accountID>` | Replace the stored credential |
| `usage <accountID>` | Token and cost attribution for the account |
| `revoke <accountID>` | Revoke the account with `--reason` |
| `delete <accountID>` | Delete the account |

Credentials are read from a file or from stdin (`--credential-file -`), never
from a flag value: a command-line secret lands in shell history and the
process table.

```bash
quicbaas account create --provider claude --display-name alice-work \
  --refresh-policy oauth2 --credential-file -
quicbaas account list --provider claude --output json
```

An account's **spend policy** (go-quicbaas#781) is set on `create` and
`update` with the shared `--spend-window name=N,size=D[,tokens=T][,usd=X]`
(repeatable; the set replaces the windows) and `--clear-spend`, plus the
failure breaker `--breaker-failures N --breaker-window D --breaker-cooldown D`
(all three together) and `--clear-breaker`. On `update` the flags change only
what they name: new windows keep the breaker and a new breaker keeps the
windows.

```bash
quicbaas account update acct_abc123 --spend-window name=five-hour,size=5h,tokens=2000000
quicbaas account update acct_abc123 --breaker-failures 3 --breaker-window 1h --breaker-cooldown 30m
```

## quicbaas sdlc

Bind a forge repository and its review policy to an orchestration project, so
that issues labelled with the binding's trigger label become tasks the
Software Factory branches, implements, reviews and merges
(`cmd/quicbaas/sdlc.go`). Tasks, jobs and sessions are
`quicbaas orchestrator`; readmitting a blocked task is
`quicbaas orchestrator task readmit`.

Bindings are tenant-scoped: every command operates within the tenant named by
the global `--tenant` flag.

| Command | Arguments | Description |
|---------|-----------|-------------|
| `bind <project-id>` | exactly 1 | Attach a forge binding to a project from a file |
| `unbind <project-id>` | exactly 1 | Detach a project's forge binding |
| `binding <project-id>` | exactly 1 | Show a project's forge binding |
| `delivery <task-id>` | exactly 1 | Show what a task's attempt delivered |
| `adopt <project-id> <issue>` | exactly 2 | Hand an issue back to the factory, optionally resuming its open pull request |

| Flag | Scope | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--output` | group | string | `table` | `json`, `yaml` or `table` |
| `--file`, `-f` | `bind` | string | — | The project binding as YAML or JSON (**required** for `bind`) |
| `--pull-request` | `adopt` | int | — | An open pull request the task resumes, on its own head branch |
| `--ruling` | `adopt` | string | — | Appended to the task's contract and posted on the issue |

Omitting `--file` on `bind` fails locally with `BindingFileRequiredError`
before any request is sent; an unreadable or unparseable file fails with
`BindingFileError` naming the path.

```bash
quicbaas sdlc bind go-truststrap --file go-truststrap-binding.yaml
quicbaas sdlc binding go-truststrap --output json
quicbaas sdlc delivery 1788567351228350294
quicbaas sdlc reviewer go-truststrap 42
quicbaas sdlc reviewer set go-truststrap 42 qwen
quicbaas sdlc reviewer clear go-truststrap 42
quicbaas sdlc issue create go-truststrap --title "Split: codec half" --label task:ready --parent 41
quicbaas sdlc collaborators go-truststrap
quicbaas sdlc adopt go-luks2 141 --pull-request 194 --ruling "Land #194 as it stands."
quicbaas sdlc unbind go-truststrap
```

`issue create` files an issue on the bound repository through the binding's
token (`sdlc:issue_create`, audited). `--title` is required; `--body` and
`--body-file` are exclusive; `--label` repeats and each label must exist on
the repository; `--parent` links the issue it was split out of. Bad flags fail
locally with `IssueTitleRequiredError`, `IssueBodyConflictError`,
`IssueParentError` or `IssueBodyFileError` before any request is sent.
`collaborators` lists the repository's owner and collaborators — the accounts a
binding's `rulers` may name.

`adopt` is for an issue no live task holds — its task completed, or triage
holds it — and is documented in
[user.md](user.md#adopting-an-issue-no-task-holds). An issue argument that is
not a positive integer fails with `IssueNumberError` before any request is sent.

`reviewer` shows which reviewer profile reviews a pull request, whether an
operator chose it, whether its verdict counts toward merge, and the status
context it publishes under. `set` records an operator override naming one of
the binding's profiles, and `clear` returns the pull request to the project
default. A pull request number that is not a positive integer fails with
`PullRequestNumberError` before any request is sent. See
[review.md](review.md#the-per-pull-request-override).

The `table` rendering of a binding is one line per decision an operator
checks — forge, trigger label, strategy, worker (`backend/model in image`),
verification commands, review policy, the default reviewer and each
alternate profile with its merge role, review rounds, whether a webhook secret
is configured, and the pinned SSH host key rendered as its algorithm and
comment only. `delivery` prints the branch, the base, the head SHA, the pull
request, then one line per review (reviewer, verdict, head, round) and one
per check.

The binding file's fields are documented in
[sdlc.md](sdlc.md#the-project-binding); the wire shape is in
[api.md](api.md#the-sdlc-module).

## quicbaas prompts

**Not implemented.** The planned command group for editing the prompt sets the
worker, the reviewer and the lead are given — `list`, `get`, `effective`,
`diff`, `history`, `put`, `rollback`, `reset` — is specified in
[prompt-sets.md](prompt-sets.md). The prompts themselves are now markdown
embedded in the binary rather than Go constants, but there is no store behind
them yet, so editing one is editing a file and rebuilding — nothing to
configure from the command line.
