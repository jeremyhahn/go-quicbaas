### Changed

- **Each vector REST route and MCP tool now requires the action go-qrdb
  declares for that operation** (`qrdb.v1.permission` on its `Vector*`
  RPCs), so all transports agree with the gRPC table. A role granted only
  the old action loses access to these routes and tools. Parity tests on
  REST, MCP and gRPC fail when a vector route, tool or RPC has no pinned
  action. See `docs/authorization/engineering.md#vector-actions-reference`.

  REST:

  | Route | Was | Now |
  |-------|-----|-----|
  | `DELETE /api/v1/vector/namespaces/{namespace}/vectors/{id}` | `vector:namespace_delete` | `vector:delete` |
  | `GET /api/v1/vector/namespaces/{namespace}/vectors/{id}` | `vector:namespace_list` | `vector:get` |
  | `GET /api/v1/vector/namespaces/{namespace}` | `vector:namespace_list` | `vector:namespace_info` |
  | `PUT /api/v1/vector/namespaces/{namespace}` | `vector:upsert` | `vector:namespace_create` |
  | `PUT /api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata` | `vector:upsert` | `vector:update_metadata` |
  | `GET /api/v1/vector/admin/compact/progress` | `vector:get` | `vector:get_compaction_progress` |
  | `POST /api/v1/vector/admin/export/{namespace}` | `vector:upsert` | `vector:list` |
  | `POST /api/v1/vector/admin/import/{namespace}` | `vector:upsert` | `vector:batch_upsert` |

  MCP:

  | Tool | Was | Now |
  |------|-----|-----|
  | `qrdb_vector_delete_namespace` | `vector:delete` | `vector:namespace_delete` |
  | `qrdb_vector_list_namespaces` | `vector:list` | `vector:namespace_list` |
  | `qrdb_vector_get_namespace_info` | `vector:get` | `vector:namespace_info` |
  | `qrdb_vector_search_by_id` | `vector:search_similar` | `vector:search_by_id` |

  gRPC is unchanged. Vector health (`GET /api/v1/vector/health` and
  `VectorDB/Health`) still requires `vector:namespace_list`, because
  go-qrdb declares no health action.
