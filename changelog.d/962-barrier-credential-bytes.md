### Changed

- **Barrier credentials are bytes on every go-quicbaas wire
  (go-quicbaas#962).** The `secret` of `BarrierInitializeRequest`,
  `BarrierUnsealRequest` and `BarrierShamirInitializeRequest`, and the
  `passphrase` of `BarrierEnableEncryptionRequest`,
  `TenantBarrierInitRequest` and `TenantBarrierEnableEncryptionRequest`, are
  now protobuf `bytes`. The frames are byte-identical to the old `string`
  frames, but a credential no longer has to be valid UTF-8. The generated Go
  fields change from `string` to `[]byte`, so gRPC clients compiled against
  `api/proto` must update their call sites. Shamir share fields stay
  `string`.
- **REST break: barrier credentials in a JSON body must be base64.** This
  covers `secret` on `/api/v1/barrier/initialize`, `/unseal` and
  `/shamir/initialize`; `passphrase` on barrier enable-encryption and tenant
  barrier init; and `key` on tenant barrier unseal. A raw string that is not
  well-formed base64 now gets `400` with `BarrierCredentialEncodingError`.
  The response does not echo the rejected text, and the request body is
  wiped after it is decoded.
- **TypeScript SDK: barrier credentials are `Uint8Array`.**
  `BarrierInitRequest.secret`, `BarrierUnsealRequest.secret`,
  `BarrierShamirInitRequest.secret`, `TenantBarrierInitRequest.passphrase`
  and the `key` argument of `tenantBarrierUnseal` now take raw bytes, and the
  REST client base64-encodes them. Callers that passed pre-encoded strings
  now pass the bytes.
- MCP is unchanged on the wire. A credential is still a JSON string, and
  the server converts it to bytes in one function and wipes the copy after
  use.
