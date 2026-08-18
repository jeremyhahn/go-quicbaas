# Agentic Projects — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/agentic-projects/`.



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

### `quicbaas.Project` — SDK package `sdk/go/client/project`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 8; MCP tools registered server-side: 5.

MCP tools: `project_create`, `project_delete`, `project_get`, `project_list`, `project_update`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Create` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Delete` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Get` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetByName` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GrantAgentRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `List` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RevokeAgentRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Update` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |

### `quicbaas.Account` — SDK package `sdk/go/client/account`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 9; MCP tools registered server-side: 9.

MCP tools: `quicbaas_agent_account_create`, `quicbaas_agent_account_delete`, `quicbaas_agent_account_get`, `quicbaas_agent_account_list`, `quicbaas_agent_account_revoke`, `quicbaas_agent_account_rotate`, `quicbaas_agent_account_set_default`, `quicbaas_agent_account_update`, `quicbaas_agent_account_usage`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `List` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `Usage` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |


