# Codec — CLI

> **Status: NORMATIVE** · Last verified: 2026-11-13
> Feature folder: `docs/codec/`.

**No CLI surface.** `quicbaas` registers no `codec` command group — there is no
entry in the root command list (`cmd/quicbaas/main.go:181-246`) and no
`newCodec*Command` constructor in `cmd/quicbaas/`.

Serialization is not an operation an operator performs; it is how every other
operation travels. The codec in force is a property of the transport and the
configuration, not of a command.

Where codec choice does surface at the command line, it is as an **output
format** flag on other command groups, which is a different thing — it selects
how the CLI renders a result, not how the system encodes a record:

```bash
quicbaas data get users user1 --output json
quicbaas iam user list --output yaml
```

There is no `codec:` configuration block either: the codec is go-qrdb's
(`architecture.md`).
