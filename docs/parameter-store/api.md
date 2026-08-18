# Parameter Store — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/parameter-store/`.

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

### `quicbaas.ParameterStore` — SDK package `sdk/go/client/parameterstore`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 0; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `DeleteParameter` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `DeleteParameters` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `GetParameter` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `GetParameters` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `ListParameters` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `SearchByTags` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `SetParameter` | MISSING | ? | MISSING | MISSING | MISSING | — |
| `SetParameters` | MISSING | ? | MISSING | MISSING | MISSING | — |


Complete API documentation for the Parameter Store service.

## REST API

Base URL: `/api/v1/parameters`

### Get Parameter

Retrieve a single parameter by path.

```
GET /api/v1/parameters/{path}
```

**Path Parameters:**
- `path` - Hierarchical parameter path (e.g., `app/database/host`)

**Query Parameters:**
- `with_decryption` (boolean, default: `true`) - Decrypt encrypted values

**Response:**
```json
{
  "parameter": {
    "path": "/app/database/host",
    "value": "ZGIuZXhhbXBsZS5jb20=",
    "type": "string",
    "description": "Database hostname",
    "tags": {
      "env": "prod",
      "service": "database"
    },
    "is_encrypted": false,
    "expires_at": null,
    "audit_enabled": false,
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T10:30:00Z",
    "created_by": "user-123",
    "updated_by": "user-123"
  },
  "timestamp": "2024-01-15T12:00:00Z"
}
```

**Status Codes:**
- `200 OK` - Success
- `404 Not Found` - Parameter not found
- `410 Gone` - Parameter expired
- `401 Unauthorized` - Missing/invalid token
- `403 Forbidden` - Access denied

---

### Set Parameter

Create or update a parameter.

```
PUT /api/v1/parameters/{path}
```

**Path Parameters:**
- `path` - Hierarchical parameter path

**Request Body:**
```json
{
  "value": "ZGIuZXhhbXBsZS5jb20=",
  "type": "string",
  "description": "Database hostname",
  "kms_key_id": "default",
  "tags": {
    "env": "prod",
    "service": "database"
  },
  "ttl_seconds": 3600,
  "audit_enabled": true,
  "overwrite": true
}
```

**Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | bytes (base64) | Yes | Parameter value |
| `type` | string | No | `string`, `number`, `json`, `binary` (default: `string`) |
| `description` | string | No | Human-readable description |
| `kms_key_id` | string | No | KMS key for encryption |
| `tags` | object | No | Key-value tags |
| `ttl_seconds` | int64 | No | Time-to-live (0 = no expiration) |
| `audit_enabled` | boolean | No | Enable audit logging |
| `overwrite` | boolean | No | Allow update if exists (default: `true`) |

**Response:**
```json
{
  "path": "/app/database/host",
  "created": true,
  "timestamp": "2024-01-15T12:00:00Z"
}
```

**Status Codes:**
- `200 OK` - Updated existing parameter
- `201 Created` - Created new parameter
- `400 Bad Request` - Invalid request
- `409 Conflict` - Parameter exists and overwrite=false

---

### Delete Parameter

Delete a parameter.

```
DELETE /api/v1/parameters/{path}
```

**Response:**
```json
{
  "path": "/app/database/host",
  "deleted": true,
  "timestamp": "2024-01-15T12:00:00Z"
}
```

**Status Codes:**
- `200 OK` - Deleted successfully
- `404 Not Found` - Parameter not found

---

### List Parameters

List parameters by prefix.

```
GET /api/v1/parameters
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `prefix` | string | `/` | Path prefix filter |
| `recursive` | boolean | `true` | Include nested paths |
| `with_values` | boolean | `false` | Include parameter values |
| `with_decryption` | boolean | `false` | Decrypt encrypted values |
| `limit` | int | `100` | Max results (1-1000) |
| `cursor` | string | | Pagination cursor |

**Response:**
```json
{
  "parameters": [
    {
      "path": "/app/database/host",
      "type": "string",
      "is_encrypted": false
    },
    {
      "path": "/app/database/port",
      "type": "number",
      "is_encrypted": false
    }
  ],
  "next_cursor": "eyJvZmZzZXQiOjEwMH0=",
  "has_more": true,
  "total_count": 150,
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

### Batch Get

Retrieve multiple parameters.

```
POST /api/v1/parameters/batch/get
```

**Request Body:**
```json
{
  "paths": [
    "/app/database/host",
    "/app/database/port",
    "/app/database/name"
  ],
  "with_decryption": true
}
```

**Response:**
```json
{
  "parameters": [
    {
      "path": "/app/database/host",
      "value": "ZGIuZXhhbXBsZS5jb20=",
      "type": "string"
    },
    {
      "path": "/app/database/port",
      "value": "NTQzMg==",
      "type": "number"
    }
  ],
  "errors": {
    "/app/database/name": "parameter not found"
  },
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

### Batch Set

Create or update multiple parameters atomically.

```
POST /api/v1/parameters/batch/set
```

**Request Body:**
```json
{
  "parameters": [
    {
      "path": "/app/database/host",
      "value": "ZGIuZXhhbXBsZS5jb20=",
      "type": "string",
      "tags": {"env": "prod"}
    },
    {
      "path": "/app/database/port",
      "value": "NTQzMg==",
      "type": "number",
      "tags": {"env": "prod"}
    }
  ]
}
```

**Response:**
```json
{
  "results": [
    {
      "path": "/app/database/host",
      "created": true,
      "success": true
    },
    {
      "path": "/app/database/port",
      "created": false,
      "success": true
    }
  ],
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

### Batch Delete

Delete multiple parameters.

```
POST /api/v1/parameters/batch/delete
```

**Request Body:**
```json
{
  "paths": [
    "/app/database/host",
    "/app/database/port"
  ]
}
```

**Response:**
```json
{
  "results": [
    {
      "path": "/app/database/host",
      "deleted": true
    },
    {
      "path": "/app/database/port",
      "deleted": true
    }
  ],
  "deleted_count": 2,
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

### Search by Tags

Find parameters matching tag criteria.

```
POST /api/v1/parameters/search
```

**Request Body:**
```json
{
  "tags": {
    "env": "prod",
    "service": "database"
  },
  "prefix": "/app/",
  "with_values": false,
  "with_decryption": false,
  "limit": 100,
  "cursor": ""
}
```

**Response:**
```json
{
  "parameters": [
    {
      "path": "/app/database/host",
      "type": "string",
      "tags": {"env": "prod", "service": "database"}
    },
    {
      "path": "/app/database/port",
      "type": "number",
      "tags": {"env": "prod", "service": "database"}
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "timestamp": "2024-01-15T12:00:00Z"
}
```

---

## gRPC API

Service definition: `ParameterStoreService`

### Service Methods

```protobuf
service ParameterStoreService {
  rpc GetParameter(GetParameterRequest) returns (GetParameterResponse);
  rpc SetParameter(SetParameterRequest) returns (SetParameterResponse);
  rpc DeleteParameter(DeleteParameterRequest) returns (DeleteParameterResponse);
  rpc GetParameters(GetParametersRequest) returns (GetParametersResponse);
  rpc SetParameters(SetParametersRequest) returns (SetParametersResponse);
  rpc DeleteParameters(DeleteParametersRequest) returns (DeleteParametersResponse);
  rpc ListParameters(ListParametersRequest) returns (ListParametersResponse);
  rpc SearchByTags(SearchByTagsRequest) returns (SearchByTagsResponse);
}
```

### Message Types

```protobuf
enum ParameterType {
  PARAMETER_TYPE_UNSPECIFIED = 0;
  PARAMETER_TYPE_STRING = 1;
  PARAMETER_TYPE_NUMBER = 2;
  PARAMETER_TYPE_JSON = 3;
  PARAMETER_TYPE_BINARY = 4;
}

message Parameter {
  string path = 1;
  bytes value = 2;
  ParameterType type = 3;
  string description = 4;
  map<string, string> tags = 5;
  bool is_encrypted = 6;
  string kms_key_id = 7;
  google.protobuf.Timestamp expires_at = 8;
  bool audit_enabled = 9;
  google.protobuf.Timestamp created_at = 10;
  google.protobuf.Timestamp updated_at = 11;
  string created_by = 12;
  string updated_by = 13;
}
```

---

## MCP Tools

Model Context Protocol tools for AI agent integration.

### quicbaas_parameter_get

Get a single parameter.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Parameter path"},
    "with_decryption": {"type": "boolean", "default": true}
  },
  "required": ["path"]
}
```

### quicbaas_parameter_set

Create or update a parameter.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "value": {"type": "string"},
    "type": {"type": "string", "enum": ["string", "number", "json", "binary"]},
    "description": {"type": "string"},
    "kms_key_id": {"type": "string"},
    "tags": {"type": "object"},
    "ttl_seconds": {"type": "integer"},
    "audit_enabled": {"type": "boolean"}
  },
  "required": ["path", "value"]
}
```

### quicbaas_parameter_delete

Delete a parameter.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "path": {"type": "string"}
  },
  "required": ["path"]
}
```

### quicbaas_parameter_list

List parameters by prefix.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "prefix": {"type": "string"},
    "recursive": {"type": "boolean", "default": true},
    "limit": {"type": "integer", "default": 100}
  }
}
```

### quicbaas_parameter_search

Search parameters by tags.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "tags": {"type": "object"},
    "prefix": {"type": "string"},
    "limit": {"type": "integer", "default": 100}
  },
  "required": ["tags"]
}
```

---

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `PARAMETER_NOT_FOUND` | 404 | Parameter does not exist |
| `PARAMETER_EXISTS` | 409 | Parameter already exists |
| `INVALID_PATH` | 400 | Invalid path format |
| `PARAMETER_EXPIRED` | 410 | Parameter has expired |
| `ENCRYPTION_ERROR` | 500 | KMS operation failed |
| `VALIDATION_ERROR` | 400 | Request validation failed |
| `UNAUTHORIZED` | 401 | Authentication required |
| `FORBIDDEN` | 403 | Access denied |

## See Also

- [README](README.md) - Overview and quick start
- [Architecture](architecture.md) - Internal design
- [CLI Usage](cli-usage.md) - Command-line interface
