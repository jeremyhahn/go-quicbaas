# Data Services — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/data-services/`.


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

### `quicbaas.KV` — SDK package `sdk/go/client/kv`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=no; embedded routes registered: 6; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | `DELETE /api/v1/entities/{type}/{id}` | yes | yes | MISSING | yes | `kv:delete` |
| `Get` | `GET /api/v1/entities/{type}/{id}` | yes | yes | MISSING | yes | `kv:get` |
| `List` | `GET /api/v1/entities/{type}` | **no** | yes | MISSING | yes | — |
| `Put` | `POST /api/v1/entities/{type}` | **no** | yes | MISSING | yes | — |
| `Watch` | `POST /api/v1/kv/watch` | **no** | yes | MISSING | yes | — |

### `quicbaas.Batch` — SDK package `sdk/go/client/batch`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 3; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | `POST /api/v1/batch/delete` | yes | MISSING | MISSING | yes | `batch:execute` |
| `Execute` | `POST /api/v1/batch/execute` *(default branch)* | **no** | MISSING | MISSING | MISSING | — |
| `Get` | `POST /api/v1/batch/get` | yes | MISSING | MISSING | yes | `batch:execute` |
| `Put` | `POST /api/v1/batch/put` | yes | MISSING | MISSING | yes | `batch:execute` |

### `quicbaas.Transaction` — SDK package `sdk/go/client/transaction`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 1; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Execute` | `POST /api/v1/transaction/execute` | **no** | MISSING | MISSING | yes | — |

### `quicbaas.Query` — SDK package `sdk/go/client/query`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 2; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Execute` | `POST /api/v1/query/execute` *(default branch)* | **no** | MISSING | MISSING | MISSING | — |


## REST API

Base URL: `/v1/databases`

All endpoints require authentication via Bearer token. Database operations
are scoped to the tenant extracted from the JWT unless overridden by an
admin.

### Create Database

```http
POST /v1/databases
```

#### Request Body

```json
{
  "name": "orders",
  "engine": "kv",
  "backend": "raft",
  "etcd": {
    "enabled": true,
    "listen_addr": ":2379",
    "advertise_urls": ["https://node1:2379"]
  },
  "barrier": {
    "enabled": true
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Database name (alphanumeric, hyphens, underscores) |
| `engine` | string | No | Engine type: `kv`, `timeseries`, `vector` (default: `kv`) |
| `backend` | string | No | Backend type: `memory`, `file`, `pebble`, `raft` (default: `raft`) |
| `etcd` | object | No | etcd protocol configuration |
| `barrier` | object | No | Barrier encryption configuration |

#### Response

**201 Created**

```json
{
  "name": "acme_orders",
  "tenant_id": "acme",
  "engine": "kv",
  "backend": "raft",
  "shard_id": 104,
  "purpose": "user",
  "status": "open"
}
```

### List Databases

```http
GET /v1/databases
```

Returns databases for the authenticated tenant.

#### Response

**200 OK**

```json
{
  "databases": [
    {
      "name": "acme_orders",
      "tenant_id": "acme",
      "engine": "kv",
      "backend": "raft",
      "shard_id": 104,
      "purpose": "user",
      "status": "open"
    }
  ]
}
```

### Get Database

```http
GET /v1/databases/{name}
```

#### Response

**200 OK**

```json
{
  "name": "acme_orders",
  "tenant_id": "acme",
  "engine": "kv",
  "backend": "raft",
  "shard_id": 104,
  "purpose": "user",
  "status": "open",
  "created": "2026-03-10T14:30:00Z",
  "updated": "2026-03-10T14:30:00Z"
}
```

### Drop Database

```http
DELETE /v1/databases/{name}
```

#### Response

**200 OK**

```json
{
  "name": "acme_orders",
  "dropped": true
}
```

### Update Protocol

```http
PATCH /v1/databases/{name}/protocol
```

#### Request Body

```json
{
  "mysql": {
    "enabled": true,
    "port": 3306
  },
  "postgresql": {
    "enabled": true,
    "port": 5432
  }
}
```

### Update Etcd

```http
PATCH /v1/databases/{name}/etcd
```

#### Request Body

```json
{
  "enabled": true,
  "listen_addr": ":2379",
  "advertise_urls": ["https://node1:2379"]
}
```

### Update Raft

```http
PATCH /v1/databases/{name}/raft
```

#### Request Body

```json
{
  "election_rtt": 10,
  "heartbeat_rtt": 1
}
```

### Update Barrier

```http
PATCH /v1/databases/{name}/barrier
```

#### Request Body

```json
{
  "enabled": true,
  "key_id": "dek:tenant:acme:db:orders"
}
```

### Update Connection

```http
PATCH /v1/databases/{name}/connection
```

#### Request Body

```json
{
  "max_connections": 200,
  "idle_timeout": "60s"
}
```

---

## REST Endpoints Summary

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/databases` | Create a new database |
| `GET` | `/v1/databases` | List all databases |
| `GET` | `/v1/databases/{name}` | Get database info |
| `DELETE` | `/v1/databases/{name}` | Drop a database |
| `PATCH` | `/v1/databases/{name}/protocol` | Update protocol settings |
| `PATCH` | `/v1/databases/{name}/etcd` | Update etcd protocol |
| `PATCH` | `/v1/databases/{name}/raft` | Update Raft config |
| `PATCH` | `/v1/databases/{name}/barrier` | Update barrier encryption |
| `PATCH` | `/v1/databases/{name}/connection` | Update connection settings |

---

## gRPC RPCs

Service: `quicbaas.database.v1.DatabaseService`

| RPC | Request | Response | Description |
|-----|---------|----------|-------------|
| `DatabaseCreate` | `DatabaseCreateRequest` | `DatabaseCreateResponse` | Create a database |
| `DatabaseGet` | `DatabaseGetRequest` | `DatabaseGetResponse` | Get database info |
| `DatabaseList` | `DatabaseListRequest` | `DatabaseListResponse` | List databases |
| `DatabaseDrop` | `DatabaseDropRequest` | `DatabaseDropResponse` | Drop a database |
| `DatabaseUpdateProtocol` | `DatabaseUpdateProtocolRequest` | `DatabaseUpdateProtocolResponse` | Update protocol |
| `DatabaseUpdateEtcd` | `DatabaseUpdateEtcdRequest` | `DatabaseUpdateEtcdResponse` | Update etcd |
| `DatabaseUpdateRaft` | `DatabaseUpdateRaftRequest` | `DatabaseUpdateRaftResponse` | Update Raft |
| `DatabaseUpdateBarrier` | `DatabaseUpdateBarrierRequest` | `DatabaseUpdateBarrierResponse` | Update barrier |
| `DatabaseUpdateConnection` | `DatabaseUpdateConnectionRequest` | `DatabaseUpdateConnectionResponse` | Update connection |

---

## QUIC Methods

| Method | Description |
|--------|-------------|
| `db.Create` | Create a new database |
| `db.Get` | Get database metadata |
| `db.List` | List all databases |
| `db.Drop` | Drop a database |
| `db.UpdateProtocol` | Update protocol settings |
| `db.UpdateEtcd` | Update etcd protocol |
| `db.UpdateRaft` | Update Raft config |
| `db.UpdateBarrier` | Update barrier encryption |
| `db.UpdateConnection` | Update connection settings |

---

## MCP Tools

| Tool | Description |
|------|-------------|
| `qrdb_database_create` | Create a new database |
| `qrdb_database_list` | List all databases |
| `qrdb_database_get` | Get database info |
| `qrdb_database_drop` | Drop a database |
| `qrdb_database_update_protocol` | Update protocol config |
| `qrdb_database_update_etcd` | Update etcd protocol |
| `qrdb_database_update_raft` | Update Raft config |
| `qrdb_database_update_barrier` | Update barrier encryption |
| `qrdb_database_update_connection` | Update connection settings |

### MCP Tool Example: qrdb_database_create

**Input:**

```json
{
  "name": "orders",
  "engine": "kv",
  "backend": "raft",
  "barrier_enabled": true
}
```

**Output:**

```json
{
  "name": "acme_orders",
  "tenant_id": "acme",
  "engine": "kv",
  "backend": "raft",
  "shard_id": 104,
  "status": "open"
}
```

---

## Error Codes

| HTTP Status | gRPC Code | Description |
|-------------|-----------|-------------|
| 400 | `INVALID_ARGUMENT` | Invalid database name, engine, or backend |
| 403 | `PERMISSION_DENIED` | Platform shard access denied or tenant mismatch |
| 404 | `NOT_FOUND` | Database not found |
| 409 | `ALREADY_EXISTS` | Database already exists |
| 500 | `INTERNAL` | Internal server error |

### Error Response Format

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "database 'acme_orders' not found"
  }
}
```

---

## Cross-References

- [Database CLI Reference](cli.md)
- [Multi-Tenant Architecture](../multi-tenancy/architecture.md)
- Per-Database Encryption
