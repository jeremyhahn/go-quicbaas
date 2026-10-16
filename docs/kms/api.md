# KMS — API

> **Status: NORMATIVE** · Last verified: 2026-10-15
> The capability on every transport, with permissions. Feature folder: `docs/kms/`.

The KMS API is go-xkms's and is documented there:
[Key Management — API](../../../go-xkms/docs/key-management/api.md). This page
records only how QuicBaaS's `sdk/go` reaches it on each transport, and with
which permission.

> **Being removed.** `quicbaas.KMS` (`/api/v1/kms`) and `quicbaas.Crypto`
> (`/api/v1/crypto`) below are QuicBaaS's own key store and crypto service, a
> parallel implementation removed on the owner's word ([SPEC](../SPEC.md) row
> `kms`). The matrix records the code as it stands until the go-xkms re-export
> replaces it.

## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.KMS` — SDK package `sdk/go/client/kms`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=no; embedded routes registered: 37; MCP tools registered server-side: 16.

MCP tools: `quicbaas_kms_delete_key`, `quicbaas_kms_delete_rotation_policy`, `quicbaas_kms_generate_key`, `quicbaas_kms_get_key`, `quicbaas_kms_get_metadata`, `quicbaas_kms_get_policy`, `quicbaas_kms_get_public_key`, `quicbaas_kms_get_rotation_policy`, `quicbaas_kms_get_supported_algorithms`, `quicbaas_kms_import_key`, `quicbaas_kms_list_keys`, `quicbaas_kms_revoke_key`, `quicbaas_kms_rotate_key`, `quicbaas_kms_set_policy`, `quicbaas_kms_set_rotation_policy`, `quicbaas_kms_update_metadata`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BackendStatus` | `GET /api/v1/kms/backend/status` | yes | yes | MISSING | yes | `keys:get` |
| `DeleteKey` | `DELETE /api/v1/kms/keys/{name}` | yes | yes | MISSING | yes | `keys:delete` |
| `DeleteKeyRotationPolicy` | `POST /api/v1/kms/deletekeyrotationpolicy` *(default branch)* | **no** | yes | MISSING | yes | — |
| `ExportKey` | `POST /api/v1/kms/keys/{name}/export` | yes | yes | MISSING | yes | `keys:create` |
| `GenerateKey` | `POST /api/v1/kms/keys` | yes | yes | MISSING | yes | `keys:create` |
| `GetKey` | `GET /api/v1/kms/keys/{name}` | yes | yes | MISSING | yes | `keys:get` |
| `GetKeyMetadata` | `GET /api/v1/kms/keys/{name}/metadata` | yes | yes | MISSING | yes | `keys:get` |
| `GetKeyPolicy` | `GET /api/v1/kms/keys/{name}/policy` | yes | yes | MISSING | yes | `keys:get` |
| `GetKeyRotationPolicy` | `POST /api/v1/kms/getkeyrotationpolicy` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GetPublicKey` | `POST /api/v1/kms/getpublickey` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GetSupportedAlgorithms` | `GET /api/v1/kms/algorithms` | yes | yes | MISSING | yes | `keys:list` |
| `ImportKey` | `POST /api/v1/kms/keys/import` | yes | yes | MISSING | yes | `keys:create` |
| `ListKeyVersions` | `GET /api/v1/kms/keys/{name}/versions` | yes | yes | MISSING | yes | `keys:get` |
| `ListKeys` | `GET /api/v1/kms/keys` | yes | yes | MISSING | yes | `keys:get` |
| `RevokeKey` | `POST /api/v1/kms/keys/{name}/revoke` | yes | yes | MISSING | yes | `keys:delete` |
| `RotateKey` | `POST /api/v1/kms/keys/{name}/rotate` | yes | yes | MISSING | yes | `keys:create` |
| `SetKeyPolicy` | `PUT /api/v1/kms/keys/{name}/policy` | yes | yes | MISSING | yes | `keys:create` |
| `SetKeyRotationPolicy` | `POST /api/v1/kms/setkeyrotationpolicy` *(default branch)* | **no** | yes | MISSING | yes | — |
| `UnwrapKey` | `POST /api/v1/kms/keys/{name}/unwrap` | **no** | yes | MISSING | yes | — |
| `UpdateKeyMetadata` | `PUT /api/v1/kms/keys/{name}/metadata` | yes | yes | MISSING | yes | `keys:rotate` |
| `UpdateKeyStatus` | `PUT /api/v1/kms/keys/{name}/status` | yes | yes | MISSING | yes | `keys:create` |

### `quicbaas.Crypto` — SDK package `sdk/go/client/crypto`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=no; embedded routes registered: 13; MCP tools registered server-side: 13.

MCP tools: `quicbaas_crypto_datakey`, `quicbaas_crypto_datakey_wrapped`, `quicbaas_crypto_decrypt`, `quicbaas_crypto_decrypt_batch`, `quicbaas_crypto_encrypt`, `quicbaas_crypto_encrypt_batch`, `quicbaas_crypto_hash`, `quicbaas_crypto_hmac`, `quicbaas_crypto_random`, `quicbaas_crypto_rewrap`, `quicbaas_crypto_sign`, `quicbaas_crypto_verify`, `quicbaas_crypto_verify_hmac`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Decrypt` | `POST /api/v1/crypto/decrypt/{name}` | yes | yes | MISSING | yes | `keys:create` |
| `DecryptBatch` | `POST /api/v1/crypto/decryptbatch` *(default branch)* | **no** | yes | MISSING | yes | — |
| `Encrypt` | `POST /api/v1/crypto/encrypt/{name}` | yes | yes | MISSING | yes | `keys:create` |
| `EncryptBatch` | `POST /api/v1/crypto/encryptbatch` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GenerateDataKey` | `POST /api/v1/crypto/generatedatakey` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GenerateHMAC` | `POST /api/v1/crypto/generatehmac` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GenerateRandom` | `POST /api/v1/crypto/generaterandom` *(default branch)* | **no** | yes | MISSING | yes | — |
| `GenerateWrappedDataKey` | `POST /api/v1/crypto/generatewrappeddatakey` *(default branch)* | **no** | yes | MISSING | yes | — |
| `Hash` | `POST /api/v1/crypto/hash` | yes | yes | MISSING | yes | `keys:create` |
| `Rewrap` | `POST /api/v1/crypto/rewrap` *(default branch)* | **no** | yes | MISSING | yes | — |
| `Sign` | `POST /api/v1/crypto/sign/{name}` | yes | yes | MISSING | yes | `keys:create` |
| `Verify` | `POST /api/v1/crypto/verify/{name}` | yes | yes | MISSING | yes | `keys:create` |
| `VerifyHMAC` | `POST /api/v1/crypto/verifyhmac` *(default branch)* | **no** | yes | MISSING | yes | — |


## Request and response schemas

The key, crypto and backend operations, their fields, algorithms and errors
are go-xkms's and are documented there:
[Key Management — API](../../../go-xkms/docs/key-management/api.md),
[Symmetric Encryption & AEAD — API](../../../go-xkms/docs/symmetric-aead/api.md),
[Signing — API](../../../go-xkms/docs/signing/api.md),
[Crypto Backends — API](../../../go-xkms/docs/crypto-backends/api.md).

The bodies QuicBaaS's own `/api/v1/kms` and `/api/v1/crypto` routes accept
today belong to the implementation being removed; they are defined by
`api/proto` and the SDK request types in `sdk/go/client/kms` and
`sdk/go/client/crypto`. The DEK routes are in
[../data-encryption-keys/api.md](../data-encryption-keys/api.md).

Path parameters URL-encode hierarchical key names
(`tenant-123%2Fprod%2Fdb-key`).
