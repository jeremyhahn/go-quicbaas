# Parameter Store — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/parameter-store/`.

**No CLI surface.** `quicbaas` registers no `parameters` command group: the root
command's subcommand list in `cmd/quicbaas/main.go:181-246` has no entry for it,
and no `newParameter*Command` constructor exists anywhere in `cmd/quicbaas/`.

The parameter store is reachable through the SDK
(`Client.Storage().ParameterStore()`), the REST routes, and the MCP tools — all
enumerated in [api.md](api.md).

This is a stated absence, not an oversight this document is papering over: a
verb here would also need a `CommandDefinition` in the integration parity suite,
and neither exists. If a CLI surface is wanted, that is a new feature and an
owner decision, not something to be inferred from the API surface.
