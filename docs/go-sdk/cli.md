# Go SDK — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/go-sdk/`.

**No CLI surface of its own** — and the relationship runs the other way round.
`quicbaas` is a *consumer* of the SDK, not a wrapper with its own transport
code: `cmd/quicbaas/factories_sdk.go` builds an SDK client and every command
group issues its calls through it.

That inversion is the useful thing to know here:

- **Every root persistent flag is an SDK client setting.** `--transport`,
  `--address`, `--tls`, `--tls-ca`, `--tls-cert`, `--tls-key`, `--timeout`,
  `--tenant` (`cmd/quicbaas/main.go:160-172`) map onto `transport.Config` and
  the SDK's functional options. Reading `engineering.md` tells you what a flag
  does more precisely than the flag's own help text.
- **`--transport embedded` runs the stack in-process.** No server, no socket.
  Same SDK, different `transport.Transport` implementation.
- **A CLI verb that fails with a transport error is an SDK-level failure.** The
  retry counts, the backoff and the error classification you see at the command
  line are `sdk/go/transport/*`'s, not the command's.

The full flag reference lives in
[`../multiprotocol-api/cli.md`](../multiprotocol-api/cli.md), because those
flags are the multiprotocol surface's, and duplicating them here would create
the second copy that drifts.

For per-feature verbs, see each feature's own `cli.md`.
