# Codec — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/codec/`.

**No API surface of its own.** There is no `quicbaas.Codec` service: the SDK
client packages under `sdk/go/client/` contain no codec package, and no
transport dispatches a codec service. Serialization is not a capability a caller
invokes; it is the encoding every other capability travels in.

That makes the six-transport matrix inapplicable rather than incomplete, which
is why this file states the absence instead of listing six `MISSING` rows —
`MISSING` means "a Tier 1 capability that should be here is not", and codec is
not a Tier 1 capability.

## Where codec choice is observable through an API

| Surface | How codec appears |
|---|---|
| REST / QUIC | JSON request and response bodies; `Content-Type` on the wire |
| gRPC / Unix | Protocol Buffers, fixed by the generated service definitions |
| MCP | JSON-RPC envelopes; tool arguments and results are JSON |
| embedded | No serialization at all — Go values are passed in-process |

The embedded row is the one that catches people: because nothing is encoded,
a round-trip that silently depends on a codec's behaviour (field ordering, zero
value handling, numeric precision) behaves differently in embedded mode than
over any network transport. `../multiprotocol-api/architecture.md` covers the
transport model.

## Internal surface

`architecture.md` and `engineering.md` document the codec registry and the
`codec:` configuration block — the Go-level API that the storage and transport
layers use. Those are internal seams, not a caller-facing API.
