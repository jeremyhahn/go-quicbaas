# Signal Bridge — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/signal-bridge/`.

## The command group exists only behind a build tag

`registerSignalBridgeCommand(rootCmd)` is called unconditionally at
`cmd/quicbaas/main.go:246`, but it is a **no-op stub** unless the binary was
built with the `quicbaas_signal` build tag
(`cmd/quicbaas/signalbridge_register_stub.go`). A stock `quicbaas` binary has no
signal bridge verbs at all, and `--help` will not list them.

That is the first thing to check when a documented verb "does not exist": it is
not missing, it was not compiled in.

```bash
# Build with the bridge.
go build -tags quicbaas_signal ./cmd/quicbaas

# Confirm.
quicbaas --help | grep -i signal
```

## Why it is tagged

The bridge imports `go-signal` packages directly. Keeping it behind a tag means
a deployment that does not want a Signal dependency does not carry one — and it
keeps the coupling visible rather than ambient. `architecture.md` covers the
import boundary and why it is unusual; `engineering.md` covers configuration.

## Verbs

The registered surface is defined in `cmd/quicbaas/signalbridge.go` and
`cmd/quicbaas/signalbridge_register.go`, both of which are compiled only under
the tag. Because the verb set is build-gated, it is not reproduced here as a
fixed list: run `quicbaas signalbridge --help` against a tagged binary, which is
the only authoritative answer for the binary you actually have.

This is a deliberate departure from the convention that `cli.md` enumerates
every verb. Enumerating a set that a default build does not have would be worse
than pointing at the binary — the reader's most likely question is "why is this
not there", and the answer is above.

## Related

- [`../messaging/cli.md`](../messaging/cli.md) — the native messaging surface,
  which is not build-gated.
- [`architecture.md`](architecture.md) — what the bridge connects and where the
  boundary sits.
