# Projects — user guide

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Task-oriented walkthroughs for the Project record. Every flag is in
> [cli.md](cli.md), every route in [api.md](api.md), the design in
> [architecture.md](architecture.md). Conflicts with this document are defects.

A project is the tenant-scoped record that ties a repository, a git identity, a
workspace, fleet-node roles and limits together. Tasks and jobs name it by its
id. The examples use the CLI against tenant `acme`; set `PROJECT_ID` to the id
`create` prints.

## Create a project

```bash
quicbaas project create --tenant acme --name backend-service \
  --git-repo https://git.quicbaas.example.com/acme/backend.git --git-branch main \
  --git-author-name agent-backend --git-author-email agent-backend@quicbaas.example.com \
  --git-signing --git-signing-key backend-signing \
  --git-signing-key-scope tenant --git-signing-key-owner acme
```

- The name must be free in the tenant; a taken name is
  `ProjectAlreadyExistsError` (`409`).
- `--git-signing` needs `--git-signing-key`; an enabled policy never falls back
  to unsigned commits. The key must already exist in the KMS.
- The output carries the project id, a UUID. Use the id, not the name, wherever
  a command or API asks for a project id.

Add limits and defaults on create or later with `update`:

```bash
quicbaas project update "$PROJECT_ID" --tenant acme \
  --default-lead-agent pool:leads \
  --spend-window name=weekly,size=168h,usd=250 \
  --max-concurrency 2
```

## Onboard it: bind to a forge

A project starts work on its own only once it is autonomous, and the usual way
to make it autonomous is to bind it to a forge repository through the SDLC
module. Binding requires the project to have a signing policy and a repository
on one of the binding's allowed hosts, and it sets `autonomous` for you. The
binding, onboarding and the issue-to-merge flow are documented in
[Bind a project and follow one issue](../sdlc/user.md#bind-a-project-and-follow-one-issue).

After binding, every task adopted from a forge issue must come from the bound
repository. A task whose source issue names another repository is refused
before anything touches the forge — see
[Why a task is refused](#why-a-task-is-refused).

To run autonomously without a forge binding, set the switch directly:

```bash
quicbaas project update "$PROJECT_ID" --autonomous
```

What autonomy turns on, and how `--max-concurrency` and `--context-repo`
bound it, is in
[autonomy and dispatch](../orchestration/autonomy-and-dispatch.md).

## Grant fleet nodes a role

```bash
quicbaas project grant-role "$PROJECT_ID" --node node-lead-01 --role lead
quicbaas project grant-role "$PROJECT_ID" --node node-build-02 --role worker
```

A project with no grants admits every fleet node as `lead`. **The first grant
closes the project to every node you did not list**, so grant all the nodes
that should work on it in one sitting. `revoke-role` writes `denied`, which
also counts as a grant: revoking on an ungranted project closes it to every
other node too. Roles are explained in
[architecture.md](architecture.md#permission-matrix).

## Park and resume

```bash
quicbaas project park "$PROJECT_ID"
quicbaas project resume "$PROJECT_ID"
```

Parking stops new intake: no issues are adopted, no unstarted task is
dispatched, no triage runs. Work already started — worker turns, pull
requests, reviews, merge trains — finishes. Parking does not change
`autonomous`, and resuming does not turn it on. The full table is in
[architecture.md](architecture.md#parking).

## Rotate the signing key

Create the new key in the KMS, then name it and retire the old one in one
update:

```bash
quicbaas project update "$PROJECT_ID" \
  --git-signing-key backend-signing-2 \
  --git-signing-retire-key backend-signing
```

- The retired key joins the project's trusted signing history, so pull
  requests it signed before the rotation still publish and merge.
- Retire the old key **before** deleting it from the KMS: the server reads its
  public half when it records the retirement.
- `--git-signing-retired-at` (RFC 3339) defaults to now. A future time, or
  naming the current signing key, is refused. Retiring a key twice keeps the
  first retirement.

A job escalated as `untrusted_signer` carries a commit no trusted key signed;
add the signer to the history with the retirement flags, or replace the
commit, then readmit it. Why the history is checked this way is in
[architecture.md](architecture.md#git-identity-and-signing-history).

## Session rotation

`--session-rotation` (`session_rotation` on every wire) decides what a
persistent worker session keeps when the runner replaces it for a reason that
is not the work's:

- the pooled provider account under it rotates its credential;
- the provider refuses the account for a usage limit and the session moves to
  another account;
- its container was released while its pull request was reviewed and the
  review asked for another round.

| Value | The replacement session |
| --- | --- |
| `resume` (default) | continues the same conversation; the old transcript is carried into its first prompt |
| `fresh` | starts a new conversation on the same branch, told only why it is new |

```bash
quicbaas project update "$PROJECT_ID" --session-rotation fresh
```

A project that never set it runs `resume`. Passing `resume` returns a project
to the default. Any other value is refused with `ProjectInvalidError` on field
`session_rotation`. Choose `fresh` for projects whose turns run close to the
context limit, where a replayed conversation would crowd the next turn. The
mechanics are in [SDLC: a rotated account](../sdlc/architecture.md#a-rotated-account-resumes-the-session-or-starts-it-fresh)
and [SDLC: a refused account](../sdlc/architecture.md#a-refused-account-rolls-the-session-onto-another-account).

## Choose a workspace storage tier

```bash
quicbaas project update "$PROJECT_ID" \
  --workspace-tier ephemeral --workspace-tmpfs-mb 4096 --container-memory-mb 8192
```

`persistent` keeps the workspace between runs; `ephemeral` puts it on a tmpfs
of the given size. What each tier costs and guarantees is in
[storage tiers](../software-factory/storage-tiers.md).

## Look up a project by name

```bash
quicbaas project get --name backend-service --tenant acme
```

This works on the CLI's default Unix transport and on gRPC. Over REST and QUIC
it does not resolve the name; see
[engineering.md](engineering.md#known-gaps). `quicbaas project list --search
backend` finds a project by a substring of its name on every transport.

## Why a task is refused

| Error | Cause | Fix |
| --- | --- | --- |
| `InvalidProjectIDError` | a task or job named the project by something other than its UUID, usually its name | pass the id from `create` or `get --name` |
| `TaskSourceRepoMismatchError` | the task's source issue is in a repository the project is not bound to | adopt the issue under the project bound to that repository, or fix the binding |
| `ProjectParkedError` | the project is parked and the task has not started | `quicbaas project resume` |
| `ProjectUnresolvedError` (`403`) | the server could not read the project record | check the project exists in the tenant; the check never fails open |
| `ProjectWorkspaceNotReadyError` | the managed workspace is still cloning or failed | wait for the clone to finish; a failed clone is recorded on the stored record, which no transport returns yet ([known gaps](engineering.md#known-gaps)) |
| refused naming `project_role` | no online fleet node holds at least `worker` on the project | grant a role to an online node |

## Delete a project

Unbind a bound project first. Delete refuses a project with an SDLC binding
and never offboards it for you.

```bash
quicbaas sdlc unbind "$PROJECT_ID"
quicbaas project delete "$PROJECT_ID"
```

Deleting releases the name for reuse and removes a managed workspace.
