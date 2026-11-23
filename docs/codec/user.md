# Codec — User Guide

> **Status: NORMATIVE** · Last verified: 2026-11-13
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/codec/`.

There is nothing to set up. go-quicbaas stores data through its embedded
go-qrdb node, and the node's codec encodes it. There is no codec to choose,
configure or call.

## Choosing how a response is encoded

Pick the transport, not a codec:

| You want | Use |
|---|---|
| JSON over HTTP | REST or QUIC |
| Protocol Buffers | gRPC, or the Unix socket |
| JSON-RPC for an agent | MCP |
| No encoding at all | the embedded transport (in-process Go values) |

`api.md` lists what each transport puts on the wire.

## Rendering CLI output

`--output json` or `--output yaml` on a command selects how the CLI prints a
result (`cli.md`). It does not change how anything is stored.

## Messaging payloads

A messaging channel carries a `codec` label for its payloads (`json`,
`msgpack`, `protobuf`). That is a messaging setting; see `../messaging/`.

## Upgrading from a configuration with a `codec:` block

The `codec:` block (`default_format`, `enable_json`, `enable_msgpack`,
`enable_binary`) configured nothing and is gone. Delete it from your
configuration; go-quicbaas ignores the key.
