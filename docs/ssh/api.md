# SSH — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/ssh/`.

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

### `quicbaas.SSH` — SDK package `sdk/go/client/ssh`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 14; MCP tools registered server-side: 14.

MCP tools: `quicbaas_ssh_authorized_add`, `quicbaas_ssh_authorized_emit`, `quicbaas_ssh_authorized_list`, `quicbaas_ssh_authorized_remove`, `quicbaas_ssh_key_delete`, `quicbaas_ssh_key_export`, `quicbaas_ssh_key_generate`, `quicbaas_ssh_key_get`, `quicbaas_ssh_key_import`, `quicbaas_ssh_key_list`, `quicbaas_ssh_key_sign`, `quicbaas_ssh_known_host_add`, `quicbaas_ssh_known_host_list`, `quicbaas_ssh_known_host_remove`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `AddAuthorizedKey` | `POST /api/v1/ssh/authorized/keys` | yes | yes | yes | yes | `keys:create` |
| `AddKnownHost` | `POST /api/v1/ssh/known-hosts` | yes | yes | yes | yes | `keys:create` |
| `DeleteKey` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `EmitAuthorizedKey` | `POST /api/v1/ssh/authorized` | yes | yes | yes | yes | `keys:get` |
| `ExportPublicKey` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GenerateKey` | `POST /api/v1/ssh/keys` | yes | yes | yes | yes | `keys:create` |
| `GetKey` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ImportKey` | `POST /api/v1/ssh/keys/import` | yes | yes | yes | yes | `keys:import` |
| `ListAuthorizedKeys` | `GET /api/v1/ssh/authorized/keys` | yes | yes | yes | yes | `keys:list` |
| `ListKeys` | `GET /api/v1/ssh/keys` | yes | yes | yes | yes | `keys:list` |
| `ListKnownHosts` | `GET /api/v1/ssh/known-hosts` | yes | yes | yes | yes | `keys:list` |
| `RemoveAuthorizedKey` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RemoveKnownHost` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |

