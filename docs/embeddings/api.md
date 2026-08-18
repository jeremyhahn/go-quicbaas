# Embeddings — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/embeddings/`.

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

### `quicbaas.Embedding` — SDK package `sdk/go/client/embedding`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 0; MCP tools registered server-side: 7.

MCP tools: `quicbaas_embedding_batch`, `quicbaas_embedding_cache_clear`, `quicbaas_embedding_cache_stats`, `quicbaas_embedding_generate`, `quicbaas_embedding_get_provider`, `quicbaas_embedding_list_providers`, `quicbaas_embedding_set_default`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BatchGenerateEmbeddings` | `POST /api/v1/embedding/batch` | yes | yes | yes | MISSING | `embedding:embed` |
| `ClearCache` | `DELETE /api/v1/embedding/cache` | yes | yes | yes | MISSING | `embedding:embed` |
| `GenerateEmbedding` | `POST /api/v1/embedding/generate` | yes | yes | yes | MISSING | `embedding:embed` |
| `GetCacheStats` | `GET /api/v1/embedding/cache/stats` | yes | yes | yes | MISSING | `embedding:list_models` |
| `GetProviderInfo` | `GET /api/v1/embedding/providers/{provider}` | yes | yes | yes | MISSING | `embedding:list_models` |
| `ListProviders` | `GET /api/v1/embedding/providers` | yes | yes | yes | MISSING | `embedding:list_models` |
| `SetDefaultProvider` | `PUT /api/v1/embedding/providers/default` | yes | yes | yes | MISSING | `embedding:list_models` |

