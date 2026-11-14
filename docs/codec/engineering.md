# Codec — Engineering

> **Status: NORMATIVE** · Last verified: 2026-11-13
> Implementation notes and constraints. Feature folder: `docs/codec/`.

## Rules

1. **Use go-qrdb's codec through the node.** Persist Go values through
   `sdk/go`; the embedded go-qrdb node encodes them. Do not encode a record
   yourself before handing it to the node, and do not import go-qrdb's
   `pkg/codec`.
2. **Add no codec surface.** No `codec:` configuration block, no `CodecConfig`
   type in `pkg/config` or `sdk/go`, no codec handler or codec negotiation on
   any transport, no `sdk/go/client/codec`. The guard tests below fail if one
   appears.
3. **Do not import `pkg/codec`.** It is a parallel implementation awaiting the
   owner's word on its removal; a new importer would turn it into a
   dependency.
4. **Transport encodings are the transport's.** A REST handler responds with
   `RespondJSON`; a gRPC handler returns its generated message. Neither picks
   a codec.

## Guard tests

| Test | Package | Fails when |
|---|---|---|
| `TestConfig_NoCodecBlock` | `pkg/config` | `Config` has a `Codec` field or a `codec` key, or a `Codec*` type is declared |
| `TestSDK_NoCodecSurface` | `sdk/go` | the SDK exports a `Codec*` identifier or ships `client/codec` |
| `TestREST_NoCodecSurface` | `pkg/api/rest` | a codec handler or codec negotiation helper is declared |
| `TestCodec_NoImporters` | `pkg/codec` | a non-test file outside `pkg/codec` imports it |

## Not this feature

- Messaging channel codecs (`MessagingCodecJSON`, `MessagingCodecMsgpack`,
  `MessagingCodecProtobuf`): a messaging field, see `../messaging/`.
- `pkg/storage/file`'s use of `github.com/jeremyhahn/go-codec` for file-backed
  object storage: the storage feature's own encoding.
- CLI `--output json|yaml`: rendering of a result, see `cli.md`.
