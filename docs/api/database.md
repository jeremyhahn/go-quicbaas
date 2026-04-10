# Database API Reference

Complete API documentation for QuicBaaS database management across REST,
gRPC, QUIC, and MCP interfaces.

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

- [Database CLI Reference](../cli/database.md)
- [Multi-Tenant Architecture](../architecture/multi-tenant.md)
- [Per-Database Encryption](../security/per-database-encryption.md)
