# Data Encryption Keys — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/data-encryption-keys/`.


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

### `quicbaas.DEK` — SDK package `sdk/go/client/dek`

Dedicated transport handlers: rest=yes, grpc=no, mcp=no; embedded routes registered: 11; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `ClearDEKSafety` | `DELETE /api/v1/kms/deks/` | **no** | MISSING | MISSING | yes | — |
| `CreateDEK` | dedicated handler (path not statically recoverable) | ? | MISSING | MISSING | yes | — |
| `DeleteDEK` | `DELETE /api/v1/kms/deks/` | **no** | MISSING | MISSING | yes | — |
| `GetDEK` | `GET /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:get` |
| `GetDEKStatus` | `GET /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:get` |
| `GetReencryptionProgress` | `GET /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:get` |
| `ListDEKs` | `GET /api/v1/kms/deks` | **no** | MISSING | MISSING | yes | — |
| `RotateDEK` | `POST /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:create` |
| `SetDEKSafety` | `PUT /api/v1/kms/deks/` | **no** | MISSING | MISSING | yes | — |
| `SetDefaultDEK` | `POST /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:create` |
| `StartReencryption` | `POST /api/v1/kms/deks/` | yes | MISSING | MISSING | yes | `keys:create` |



