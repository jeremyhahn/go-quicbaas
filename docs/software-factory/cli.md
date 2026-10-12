# Software Factory — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The Software Factory command groups: `quicbaas agents` and the planned
> `quicbaas prompts`. `quicbaas project`, `quicbaas account` and
> `quicbaas sdlc` are in [../projects/cli.md](../projects/cli.md),
> [../accounts/cli.md](../accounts/cli.md) and [../sdlc/cli.md](../sdlc/cli.md);
> the orchestration engine's groups are in
> [../orchestration/cli.md](../orchestration/cli.md). Feature folder: `docs/software-factory/`.
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

Every `quicbaas project` verb and flag is in
[projects/cli.md](../projects/cli.md).

## quicbaas account

Pooled provider accounts. Every verb and flag, including the billing and spend
flags, is in [../accounts/cli.md](../accounts/cli.md#quicbaas-account).

## quicbaas sdlc

Every verb and flag of `quicbaas sdlc` is in
[../sdlc/cli.md](../sdlc/cli.md).

## quicbaas prompts

**Not implemented.** The planned command group for editing the prompt sets the
worker, the reviewer and the lead are given — `list`, `get`, `effective`,
`diff`, `history`, `put`, `rollback`, `reset` — is specified in
[prompt-sets.md](prompt-sets.md). The prompts themselves are now markdown
embedded in the binary rather than Go constants, but there is no store behind
them yet, so editing one is editing a file and rebuilding — nothing to
configure from the command line.
