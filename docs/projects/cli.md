# Projects — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every `quicbaas project` verb and flag, verified against
> `cmd/quicbaas/project.go`. Conflicts with this document are defects.

`quicbaas project` manages the Project record ([architecture.md](architecture.md)).
Every verb uses the SDK `quicbaas.Project` client and runs on every transport
the CLI can select. Projects are tenant-scoped: every verb operates in the
tenant named by the global `--tenant` flag (default `system`).

| Flag (group) | Default | Meaning |
| --- | --- | --- |
| `--output` | `table` | `json`, `yaml` or `table`; persistent on every verb |

## Verbs

| Verb | Arguments | Service call |
| --- | --- | --- |
| `create` | — | `Create` |
| `list` | — | `List` (paged) |
| `get` | `[projectID]` | `Get`, or `GetByName` with `--name` |
| `update` | `<projectID>` | `Update` |
| `delete` | `<projectID>` | `Delete` |
| `park` | `<projectID>` | `Update` with `parked: true` |
| `resume` | `<projectID>` | `Update` with `parked: false` |
| `grant-role` | `<projectID>` | `GrantFleetNodeRole` |
| `revoke-role` | `<projectID>` | `RevokeFleetNodeRole` |

## Shared mutation flags (`create` and `update`)

Registered by `addProjectMutationFlags` (`cmd/quicbaas/project.go:249`).

| Flag | Type | Meaning |
| --- | --- | --- |
| `--description` | string | human-readable description |
| `--workspace-node` | string | fleet node id that owns the workspace |
| `--workspace-path` | string | workspace path on that node's host |
| `--host-affinity` | string | preferred fleet node (agent id, name or host); must exist |
| `--default-lead-agent` | string | `<agent-id>`, `agent:<id>` or `pool:<id>` |
| `--git-repo` | string | remote repository URL |
| `--git-branch` | string | default branch |
| `--default-workflow` | string | tenant workflow a Task auto-spawns into; must be registered |
| `--autonomous` | bool | autonomous development; on `update`, tri-state (see below) |
| `--context-repo` | string, repeatable | `name=<name>,url=<repo-url>,ref=<branch\|tag\|sha>`; all three keys required |
| `--git-author-name`, `--git-author-email` | string | identity recorded on commits |
| `--git-signing` | bool | sign commits with the KMS key; requires `--git-signing-key`; on `update`, tri-state |
| `--git-signing-algorithm` | string | `ED25519` (default when empty) or `ECDSA-P256` |
| `--git-signing-key`, `--git-signing-key-scope`, `--git-signing-key-owner` | string | KMS signing key; scope `user`, `tenant` or `service` |
| `--git-ssh-key`, `--git-ssh-key-scope`, `--git-ssh-key-owner` | string | KMS SSH key for an `ssh://` repository; defaults to the signing key |
| `--spend-window` | string, repeatable | `name=NAME,size=DURATION[,tokens=N][,usd=DOLLARS]`; the set replaces the limits |
| `--clear-spend` | bool | remove every spend window |
| `--gpu-minutes` | int64 | GPU budget; `0` is unlimited |
| `--max-concurrency` | int | concurrent runs; `0` is unlimited |
| `--workspace-tier` | string | `persistent` or `ephemeral` ([storage tiers](../software-factory/storage-tiers.md)) |
| `--workspace-tmpfs-mb` | int | ephemeral tmpfs size in MiB; `0` is unsized |
| `--container-memory-mb` | int | container memory limit in MiB; `0` is the runtime default |
| `--session-rotation` | string | `resume` (default) or `fresh` |
| `--metadata` | key=value, repeatable | free-form metadata |

On `update`, presence decides: a flag you do not pass leaves the field alone.
`--autonomous` and `--git-signing` are tri-state (absent, `=true`, `=false`).
`--gpu-minutes`, `--max-concurrency`, `--workspace-tmpfs-mb` and
`--container-memory-mb` are sent only when passed, so `--gpu-minutes 0` lifts
the cap. String flags apply when non-empty; use the `--clear-*` flags to empty
a field.

## `create`

| Flag | Meaning |
| --- | --- |
| `--name` | project name, unique in the tenant (required) |

```bash
quicbaas project create --tenant acme --name backend-service \
  --git-repo https://git.quicbaas.example.com/acme/backend.git --git-branch main \
  --git-author-name agent-backend --git-author-email agent-backend@quicbaas.example.com \
  --git-signing --git-signing-key backend-signing \
  --git-signing-key-scope tenant --git-signing-key-owner acme \
  --default-lead-agent pool:leads \
  --spend-window name=weekly,size=168h,usd=250 \
  --max-concurrency 2 --session-rotation fresh
```

The output carries the minted project id (a UUID). Use it, not the name,
wherever a command or API takes a project id.

## `list`

Paging flags (`cmd/quicbaas/list_page.go`):

| Flag | Default | Meaning |
| --- | --- | --- |
| `--limit` | `0` | page size; `0` takes the server default of 25, capped at 100 |
| `--offset` | `0` | records to skip |
| `--sort` | — | `<field>` or `<field>:desc`; an unknown field is refused |
| `--search` | — | case-insensitive substring over the listing's text fields |
| `--all` | `false` | fetch every page from `--offset` to the end |

```bash
quicbaas project list --tenant acme
quicbaas project list --search web --sort name:desc --limit 50
quicbaas project list --all --output json
```

## `get`

| Flag | Meaning |
| --- | --- |
| `--name` | look the project up by name instead of id |

Passing neither an id nor `--name` fails with `ProjectSelectorRequiredError`.

```bash
quicbaas project get 3f2b6c1e-8d4a-4b7e-9a10-2c5d7e8f9a01
quicbaas project get --name backend-service --output yaml
```

`--name` resolves through `GetByName`, which every transport serves.

## `update`

Takes every shared mutation flag plus:

| Flag | Meaning |
| --- | --- |
| `--name` | rename the project (the new name must be free in the tenant) |
| `--clear-description` | remove the description |
| `--git-sha` | record the latest observed commit SHA |
| `--clear-default-workflow` | remove the default workflow |
| `--clear-context-repos` | remove every context repository |
| `--git-signing-retire-key` | KMS key the project signed with before its current one; added to the trusted signing history |
| `--git-signing-retire-key-scope` | its scope (`user`, `tenant`, `service`) |
| `--git-signing-retire-key-owner` | its owner; defaults to the project's tenant |
| `--git-signing-retired-at` | when it stopped being the signing key, RFC 3339; defaults to now |

```bash
# Turn signing off and drop the default workflow.
quicbaas project update "$PROJECT_ID" --git-signing=false --clear-default-workflow

# Lift the concurrency cap.
quicbaas project update "$PROJECT_ID" --max-concurrency 0

# Rotate the signing key, keeping the old one trusted for earlier commits.
# --git-signing-retired-at defaults to now; a future time is refused.
quicbaas project update "$PROJECT_ID" \
  --git-signing-key backend-signing-2 \
  --git-signing-retire-key backend-signing
```

Retire a key **before** deleting it from the KMS: the server reads its public
half from the KMS when it records the retirement. Naming the current signing
key as the retired one is refused; retiring a key twice keeps the first
retirement.

## `park` and `resume`

No flags. Park closes the project's intake; work already started finishes.
Resume reopens it. Neither changes `--autonomous`.

```bash
quicbaas project park "$PROJECT_ID"
quicbaas project resume "$PROJECT_ID"
```

## `grant-role` and `revoke-role`

| Verb | Flag | Meaning |
| --- | --- | --- |
| `grant-role` | `--node` | fleet node id (required) |
| `grant-role` | `--role` | `denied`, `lead`, `observer` or `worker` (required); anything else is `InvalidProjectRoleError` |
| `revoke-role` | `--node` | fleet node id (required) |

Revoke writes `denied`. The first entry on a project with an empty matrix
closes it to every node not listed; see
[architecture.md](architecture.md#permission-matrix).

```bash
quicbaas project grant-role "$PROJECT_ID" --node node-lead-01 --role lead
quicbaas project grant-role "$PROJECT_ID" --node node-build-02 --role worker
quicbaas project revoke-role "$PROJECT_ID" --node node-build-02
```

## `delete`

No flags. A managed workspace is removed with the project. A project with
an SDLC binding is refused; run `quicbaas sdlc unbind <project-id>` first.

```bash
quicbaas project delete "$PROJECT_ID"
```
