# Codec — Architecture

> **Status: NORMATIVE** · Last verified: 2026-11-13
> Design, component boundaries and data flow. Feature folder: `docs/codec/`.

**The codec is go-qrdb's.** go-quicbaas embeds a go-qrdb node, and the node
encodes what it persists with its own codec package (go-qrdb `pkg/codec`).
go-quicbaas neither selects nor configures that codec: go-qrdb's `sdk/go`
exports no codec, and go-quicbaas does not reach past `sdk/go` into the node.

## Data flow

```
client ──(transport encoding)──▶ go-quicbaas API ──(Go values)──▶ embedded go-qrdb node
                                                                  └─ go-qrdb codec ─▶ storage
```

- **Transport encoding** is fixed by each transport, not by a codec setting:
  JSON bodies on REST and QUIC, Protocol Buffers on gRPC and the Unix socket,
  JSON-RPC on MCP, and no encoding at all in embedded mode (`api.md`).
- **Storage encoding** is go-qrdb's concern from the moment a Go value enters
  `sdk/go`.

## What go-quicbaas does not have

- No codec service, SDK client or CLI command (`api.md`, `cli.md`).
- No `codec:` configuration block: `config.Config` has no `Codec` field and
  `sdk/go` exports no `CodecConfig`.
- No codec negotiation on REST: no `Accept`-driven codec selection, no
  `?codec=` parameter, no `X-QuicBaaS-Codec` header.

The guard tests `TestConfig_NoCodecBlock` (`pkg/config`),
`TestSDK_NoCodecSurface` (`sdk/go`) and `TestREST_NoCodecSurface`
(`pkg/api/rest`) fail if any of these comes back.

## The parallel package

`pkg/codec` is go-quicbaas's own codec package: a JSON, YAML, MessagePack and
Protobuf manager written before go-qrdb carried its codec. Nothing imports it
(`TestCodec_NoImporters`). It is a parallel implementation, kept only until the
owner rules it removed (SPEC row `codec`, #1647).

The messaging channel codec (`MessagingCodec*`, a channel's `codec` field) is
a messaging setting that labels message payloads. It is not this feature;
see `../messaging/`.
