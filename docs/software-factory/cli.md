# Software Factory — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-15
> The Software Factory command groups: `quicbaas agents` and
> `quicbaas prompts`. `quicbaas project`, `quicbaas account` and
> `quicbaas sdlc` are in [../projects/cli.md](../projects/cli.md),
> [../accounts/cli.md](../accounts/cli.md) and [../sdlc/cli.md](../sdlc/cli.md);
> the orchestration engine's groups are in
> [../orchestration/cli.md](../orchestration/cli.md). Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

Output formats, global flags, paging and the `quicbaas orchestrator` group are
in [../orchestration/cli.md](../orchestration/cli.md); the same flags apply
here. The `quicbaas fleet` group is in
[../agent-fleet/cli.md](../agent-fleet/cli.md).

## Agents

`quicbaas agents list|get|create -f|update <id> -f|delete|used-by` and
`quicbaas agents pools list|get|create -f|update <id> -f|delete|used-by`
manage the tenant's [Agents and pools](agents.md). Definitions are YAML or
JSON. Deleting an Agent or pool that something still references is refused,
and the refusal lists every user.

## quicbaas project

Every `quicbaas project` verb and flag is in
[projects/cli.md](../projects/cli.md).

## quicbaas account

Pooled provider accounts. Every verb and flag, including the billing and spend
flags, is in [../accounts/cli.md](../accounts/cli.md#quicbaas-account).

## quicbaas sdlc

Every verb and flag of `quicbaas sdlc` is in
[../sdlc/cli.md](../sdlc/cli.md).

## quicbaas prompts

Reads and edits the prompt sets the worker, the reviewer and the lead are
given. The model — scopes, records, revisions, the stop marker — is in
[prompt-sets.md](prompt-sets.md). Every verb goes through the Go SDK's `Prompts()`
client, so it works on every transport.

| Verb | Does |
|---|---|
| `list <set>` | Lists the set's prompts at the scope: overridden or inherited, version, effective origin |
| `get <set> <name>` | Shows the record overridden at the scope; `--version N` shows an earlier revision |
| `effective <set> <name>` | Shows the body a job at the scope runs with, and the chain it resolved through |
| `diff <set> <name>` | Compares the scope's body with its parent scope's |
| `history <set> <name>` | Lists the record's revisions, newest first |
| `put <set> <name> -f FILE\|-` | Overrides the prompt at the tenant or project scope |
| `rollback <set> <name> --version N` | Writes a new revision carrying revision N's body |
| `reset <set> <name>` | Makes the scope follow its parent again |

| Flag | Applies to | Meaning |
|---|---|---|
| `--tenant` | all | The global flag; the tenant the prompt belongs to |
| `--project` | all | The project, for the project scope |
| `--scope` | all | `system`, `tenant` or `project`; inferred from `--project` when empty |
| `--output` | all | `table` (default), `json` or `yaml` |
| `--raw` | `get`, `effective` | Prints only the body |
| `--version` | `get`, `rollback` | The revision to show or restore |
| `-f`, `--file` | `put` | The new body; `-` reads stdin |
| `--note` | `put`, `rollback`, `reset` | Why the change was made; kept in the history |
| `--expected-version` | `put`, `rollback`, `reset` | The version last read; the write is refused if the record has moved on. `rollback` and `reset` need it ≥ 1 |
| `--yes` | `rollback`, `reset` | Skips the confirmation prompt |

`rollback` and `reset` ask for confirmation on stderr unless `--yes` is given;
any answer but `y` or `yes` cancels without a write.
