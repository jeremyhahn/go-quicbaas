# Vector — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/vector/`.


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

### `quicbaas.Vector` — SDK package `sdk/go/client/vector`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 20 (Export and Import not served); MCP tools registered server-side: 23.

MCP tools: `qrdb_vector_batch_delete`, `qrdb_vector_batch_upsert`, `qrdb_vector_create_namespace`, `qrdb_vector_delete`, `qrdb_vector_delete_namespace`, `qrdb_vector_get`, `qrdb_vector_get_namespace_info`, `qrdb_vector_list`, `qrdb_vector_list_namespaces`, `qrdb_vector_rebuild_index`, `qrdb_vector_search_by_id`, `qrdb_vector_search_similar`, `qrdb_vector_update_metadata`, `qrdb_vector_upsert`, `quicbaas_vector_admin_compact_namespace`, `quicbaas_vector_admin_export_vectors`, `quicbaas_vector_admin_get_cluster_stats`, `quicbaas_vector_admin_get_compaction_progress`, `quicbaas_vector_admin_import_vectors`, `quicbaas_vector_get_index_progress`, `quicbaas_vector_get_stats`, `quicbaas_vector_health`, `quicbaas_vector_update_namespace`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BatchDelete` | `DELETE /api/v1/vector/namespaces/{namespace}/vectors/batch` | yes | yes | yes | `quicbaas.Vector/BatchDelete` | `vector:batch_delete` |
| `BatchUpsert` | `POST /api/v1/vector/namespaces/{namespace}/vectors/batch` | yes | yes | yes | `quicbaas.Vector/BatchUpsert` | `vector:batch_upsert` |
| `Compact` | `POST /api/v1/vector/admin/compact` | yes | yes | yes | `quicbaas.Vector/Compact` | `vector:compact` |
| `CreateNamespace` | `POST /api/v1/vector/namespaces` | **no** | yes | yes | `quicbaas.Vector/CreateNamespace` | — |
| `Delete` | `DELETE /api/v1/vector/namespaces/{namespace}/vectors/{id}` | yes | yes | yes | `quicbaas.Vector/Delete` | `vector:delete` |
| `DeleteNamespace` | `DELETE /api/v1/vector/namespaces/{namespace}` | yes | yes | yes | `quicbaas.Vector/DeleteNamespace` | `vector:namespace_delete` |
| `Export` | `POST /api/v1/vector/export` *(default branch)* | **no** | yes | yes | MISSING | — |
| `Get` | `GET /api/v1/vector/namespaces/{namespace}/vectors/{id}` | yes | yes | yes | `quicbaas.Vector/Get` | `vector:get` |
| `GetCompactionProgress` | `GET /api/v1/vector/admin/compact/progress` | yes | yes | yes | `quicbaas.Vector/GetCompactionProgress` | `vector:get_compaction_progress` |
| `GetIndexProgress` | `GET /api/v1/vector/namespaces/{namespace}/index/progress` | yes | yes | yes | `quicbaas.Vector/GetIndexProgress` | `vector:get_rebuild_progress` |
| `GetNamespaceInfo` | `GET /api/v1/vector/namespaces/{namespace}` | yes | yes | yes | `quicbaas.Vector/GetNamespaceInfo` | `vector:namespace_info` |
| `GetStats` | `POST /api/v1/vector/getstats` *(default branch)* | **no** | yes | yes | `quicbaas.Vector/GetStats` | — |
| `HybridSearch` | `POST /api/v1/vector/hybrid-search` | yes | yes | yes | `quicbaas.Vector/HybridSearch` | `vector:hybrid_search` |
| `Import` | `POST /api/v1/vector/import` *(default branch)* | **no** | yes | yes | MISSING | — |
| `List` | `POST /api/v1/vector/list` *(default branch)* | **no** | yes | yes | `quicbaas.Vector/List` | — |
| `ListNamespaces` | `GET /api/v1/vector/namespaces` | **no** | yes | yes | `quicbaas.Vector/ListNamespaces` | — |
| `RebuildIndex` | `POST /api/v1/vector/namespaces/{namespace}/index/rebuild` | yes | yes | yes | `quicbaas.Vector/RebuildIndex` | `vector:rebuild_index` |
| `SearchByID` | `POST /api/v1/vector/namespaces/{namespace}/search/by-id/{id}` | yes | yes | yes | `quicbaas.Vector/SearchByID` | `vector:search_by_id` |
| `SearchByMetadata` | `POST /api/v1/vector/search-by-metadata` | yes | yes | yes | `quicbaas.Vector/SearchByMetadata` | `vector:search_by_metadata` |
| `SearchSimilar` | `POST /api/v1/vector/namespaces/{namespace}/search/similar` | yes | yes | yes | `quicbaas.Vector/SearchSimilar` | `vector:search_similar` |
| `UpdateMetadata` | `PUT /api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata` | yes | yes | yes | `quicbaas.Vector/UpdateMetadata` | `vector:update_metadata` |
| `Upsert` | `POST /api/v1/vector/namespaces/{namespace}/vectors` | **no** | yes | yes | `quicbaas.Vector/Upsert` | — |



