# Secrets — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/secrets/`.


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

### `quicbaas.Secrets` — SDK package `sdk/go/client/secrets`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=no; embedded routes registered: 5; MCP tools registered server-side: 12.

MCP tools: `quicbaas_secrets_delete`, `quicbaas_secrets_get`, `quicbaas_secrets_get_prefix`, `quicbaas_secrets_get_rotation_policy`, `quicbaas_secrets_list`, `quicbaas_secrets_rollback`, `quicbaas_secrets_rotate`, `quicbaas_secrets_set_rotation_policy`, `quicbaas_secrets_store`, `quicbaas_secrets_update_status`, `quicbaas_secrets_versions`, `quicbaas_secrets_watch`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Delete` | `DELETE /api/v1/secrets/{path}` | **no** | yes | MISSING | yes | — |
| `Ensure` | `POST /api/v1/secrets/ensure` *(default branch)* | **no** | yes | MISSING | yes | — |
| `Get` | `GET /api/v1/secrets/{path}` | **no** | yes | MISSING | yes | — |
| `GetRotationPolicy` | `POST /api/v1/secrets/getrotationpolicy` *(default branch)* | **no** | yes | MISSING | MISSING | — |
| `GetVersion` | `POST /api/v1/secrets/getversion` *(default branch)* | **no** | yes | MISSING | MISSING | — |
| `List` | `GET /api/v1/secrets` | **no** | yes | MISSING | yes | — |
| `ListVersions` | `POST /api/v1/secrets/listversions` *(default branch)* | **no** | yes | MISSING | MISSING | — |
| `Rollback` | `POST /api/v1/secrets/rollback` *(default branch)* | **no** | yes | MISSING | MISSING | — |
| `Rotate` | `POST /api/v1/secrets/{path}/rotate` | **no** | yes | MISSING | MISSING | — |
| `SetRotationPolicy` | `POST /api/v1/secrets/setrotationpolicy` *(default branch)* | **no** | yes | MISSING | MISSING | — |
| `Store` | `POST /api/v1/secrets/{path}` | **no** | yes | MISSING | yes | — |
| `UpdateStatus` | `POST /api/v1/secrets/updatestatus` *(default branch)* | **no** | yes | MISSING | MISSING | — |

### `quicbaas.DynamicSecrets` — SDK package `sdk/go/client/dynamicsecrets`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 8; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `CreateRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `DeleteRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GenerateCredential` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `GetRole` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListLeases` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `ListRoles` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RenewLease` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |
| `RevokeLease` | dedicated handler (path not statically recoverable) | ? | yes | yes | yes | — |


## REST API

Base URL: `/api/v1/secrets`

### Authentication

All endpoints require authentication via Bearer token:

```http
Authorization: Bearer <token>
```

---

### Store Secret

Create or update a secret at the specified path.

```http
POST /api/v1/secrets/{path}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path (e.g., `database/prod`) |

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | No | Tenant ID for tenant-specific DEK |
| `key_id` | string | No | Custom key identifier |
| `algorithm` | string | No | Encryption algorithm |

#### Request Body

```json
{
  "data": "base64-encoded-secret-data",
  "description": "Optional description",
  "metadata": {
    "key": "value"
  },
  "tenant_id": "tenant-123",
  "key_id": "custom-key-id",
  "algorithm": "AES-256-GCM"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `data` | string | Yes | Base64-encoded secret data |
| `description` | string | No | Human-readable description |
| `metadata` | object | No | Key-value metadata |
| `tenant_id` | string | No | Tenant identifier |
| `key_id` | string | No | Custom encryption key ID |
| `algorithm` | string | No | Encryption algorithm |

#### Response

**201 Created**

```json
{
  "secret_id": "database/prod",
  "version": 1,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `VALIDATION_ERROR` | Missing path or empty data |
| 500 | `INTERNAL_ERROR` | Service error |

---

### Get Secret

Retrieve a secret's decrypted data.

```http
GET /api/v1/secrets/{path}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `version` | uint64 | No | latest | Specific version number |

#### Response

**200 OK**

```json
{
  "secret_id": "database/prod",
  "version": 2,
  "data": "decrypted-secret-data",
  "metadata": {
    "env": "production"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "timestamp": "2024-01-15T10:35:00Z",
  "algorithm": "AES-256-GCM",
  "key_identifier": "dek:global"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `secret_id` | string | Secret identifier/path |
| `version` | uint64 | Version number |
| `data` | string | Decrypted secret data |
| `metadata` | object | Version metadata |
| `created_at` | string | Creation timestamp (RFC3339) |
| `timestamp` | string | Response timestamp |
| `algorithm` | string | Encryption algorithm used |
| `key_identifier` | string | DEKID or KeyID used |

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `VALIDATION_ERROR` | Missing path |
| 404 | `NOT_FOUND` | Secret not found |
| 500 | `INTERNAL_ERROR` | Service error |

---

### List Secrets

List all accessible secrets.

```http
GET /api/v1/secrets
```

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `prefix` | string | No | - | Filter by path prefix |
| `limit` | int | No | 100 | Max results (1-1000) |
| `cursor` | string | No | - | Pagination cursor |

#### Response

**200 OK**

```json
{
  "secrets": [
    {
      "secret_id": "database/prod",
      "name": "database/prod",
      "description": "Production database credentials",
      "current_version": 3,
      "created_at": "2024-01-10T10:00:00Z",
      "updated_at": "2024-01-15T10:30:00Z",
      "tags": {
        "environment": "production"
      },
      "owner": "service-account-1"
    }
  ],
  "next_cursor": "cursor-456",
  "has_more": true,
  "total_count": 42,
  "timestamp": "2024-01-15T10:40:00Z"
}
```

---

### Get Secrets by Prefix

Bulk retrieve all secrets matching a path prefix.

```http
GET /api/v1/secrets/prefix/{prefix}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `prefix` | string | Path prefix to match (e.g., `database/`) |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `include_data` | bool | No | true | Include secret data |
| `limit` | int | No | 100 | Max results (1-1000) |
| `cursor` | string | No | - | Pagination cursor |

#### Response

**200 OK**

```json
{
  "secrets": {
    "database/prod": {
      "data": {
        "password": "c2VjcmV0MTIz",
        "username": "YWRtaW4="
      },
      "metadata": {
        "owner": "devops"
      },
      "version": 3
    },
    "database/staging": {
      "data": {
        "password": "c3RhZ2luZzEyMw=="
      },
      "metadata": {},
      "version": 1
    }
  },
  "next_cursor": "",
  "has_more": false,
  "total_count": 2,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `secrets` | object | Map of path to secret data |
| `next_cursor` | string | Pagination cursor for next page |
| `has_more` | bool | Whether more results exist |
| `total_count` | int | Total matching secrets |
| `timestamp` | string | Response timestamp (RFC3339) |

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `VALIDATION_ERROR` | Missing prefix |
| 500 | `INTERNAL_ERROR` | Service error |

---

### Watch Secrets by Prefix

Stream real-time secret changes for a prefix. Server-Sent Events (SSE) endpoint.

```http
GET /api/v1/secrets/watch/{prefix}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `prefix` | string | Path prefix to watch (e.g., `database/`) |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `start_version` | int64 | No | 0 | Start from this version |

#### Response

**200 OK** (streaming, `text/event-stream`)

Each event is a JSON object:

```json
{
  "path": "database/prod",
  "change_type": "UPDATED",
  "data": {
    "password": "bmV3c2VjcmV0"
  },
  "metadata": {
    "updated_by": "rotation-svc"
  },
  "version": 4,
  "timestamp": "2024-01-15T11:20:15Z"
}
```

#### Change Types

| Type | Description |
|------|-------------|
| `CREATED` | New secret stored |
| `UPDATED` | Existing secret modified |
| `DELETED` | Secret deleted |

#### Example with curl

```bash
curl -N http://localhost:8080/api/v1/secrets/watch/database/
```

---

### Delete Secret

Soft-delete a secret (recoverable by default).

```http
DELETE /api/v1/secrets/{path}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `destroy` | bool | No | false | Permanent deletion |
| `permanent` | bool | No | false | Alias for destroy |

#### Response

**200 OK**

```json
{
  "secret_id": "database/prod",
  "deleted": true,
  "timestamp": "2024-01-15T10:45:00Z"
}
```

---

### Destroy Secret

Permanently destroy a secret and all versions.

```http
POST /api/v1/secrets/{path}/destroy
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Response

**200 OK**

```json
{
  "secret_id": "database/prod",
  "deleted": true,
  "timestamp": "2024-01-15T10:50:00Z"
}
```

---

### List Secret Versions

List all versions of a secret.

```http
GET /api/v1/secrets/{path}/versions
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `limit` | int | No | 100 | Max results (1-1000) |
| `cursor` | string | No | - | Pagination cursor |

#### Response

**200 OK**

```json
{
  "versions": [
    {
      "secret_id": "database/prod",
      "version": 3,
      "metadata": {
        "rotation_reason": "scheduled"
      },
      "created_at": "2024-01-15T10:35:00Z",
      "created_by": "rotation-service",
      "deleted_at": null,
      "expires_at": "2025-01-15T10:35:00Z"
    },
    {
      "secret_id": "database/prod",
      "version": 2,
      "metadata": {},
      "created_at": "2024-01-12T14:00:00Z",
      "created_by": "user@example.com",
      "deleted_at": null,
      "expires_at": null
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "timestamp": "2024-01-15T10:55:00Z"
}
```

---

### Rollback Secret

Restore a previous version as the current version.

```http
POST /api/v1/secrets/{path}/rollback
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Request Body

```json
{
  "version": 2
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | uint64 | Yes | Target version to restore |

#### Response

**200 OK**

```json
{
  "secret_id": "database/prod",
  "new_version": 4,
  "timestamp": "2024-01-15T11:00:00Z"
}
```

---

### Rotate Secret

Create a new version with updated data.

```http
POST /api/v1/secrets/{path}/rotate
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | string | Secret path |

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tenant_id` | string | No | Tenant ID (inherits if not set) |
| `key_id` | string | No | Key ID (inherits if not set) |
| `algorithm` | string | No | Algorithm (inherits if not set) |

#### Request Body

```json
{
  "data": "new-base64-encoded-secret-data",
  "metadata": {
    "rotation_reason": "scheduled"
  },
  "tenant_id": "tenant-123",
  "key_id": "new-key-id",
  "algorithm": "AES-256-GCM"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `data` | string | Yes | New secret data (base64) |
| `metadata` | object | No | Version metadata |
| `tenant_id` | string | No | Tenant identifier |
| `key_id` | string | No | Custom key ID |
| `algorithm` | string | No | Encryption algorithm |

#### Response

**200 OK**

```json
{
  "secret_id": "database/prod",
  "old_version": 3,
  "new_version": 4,
  "timestamp": "2024-01-15T11:05:00Z"
}
```

---

### Get Secrets by Prefix

Retrieve all secrets matching a path prefix for bulk operations.

```http
GET /api/v1/secrets-prefix/{prefix}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `prefix` | string | Path prefix to match (e.g., `apps/myapp/`) |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `include_data` | boolean | No | true | Include decrypted secret data |
| `limit` | integer | No | 100 | Maximum secrets to return (1-1000) |
| `cursor` | string | No | | Pagination cursor |

#### Response

**200 OK**

```json
{
  "secrets": {
    "apps/myapp/database": {
      "password": "secret123"
    },
    "apps/myapp/cache": {
      "password": "cache456"
    }
  },
  "metadata": {
    "apps/myapp/database": {
      "env": "production"
    }
  },
  "total_count": 2,
  "has_more": false,
  "next_cursor": "",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `secrets` | object | Map of path to secret data |
| `metadata` | object | Map of path to metadata |
| `total_count` | integer | Total matching secrets |
| `has_more` | boolean | More results available |
| `next_cursor` | string | Cursor for next page |
| `timestamp` | string | Response timestamp |

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `VALIDATION_ERROR` | Missing prefix |
| 500 | `INTERNAL_ERROR` | Service error |

---

### Watch Secrets (SSE)

Stream real-time secret changes via Server-Sent Events.

```http
GET /api/v1/secrets-watch/{prefix}
```

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `prefix` | string | Path prefix to watch (e.g., `apps/myapp/`) |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `start_version` | integer | No | 0 | Start watching from version |

#### Response Headers

```http
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

#### Events

**Connected**
```
event: connected
data: {"status":"connected"}
```

**Change Event**
```
event: change
data: {"path":"apps/myapp/db","change_type":"updated","version":5,"timestamp":"2024-01-15T10:30:00Z"}
```

**Close Event**
```
event: close
data: {"status":"closed"}
```

#### Change Types

| Type | Description |
|------|-------------|
| `created` | New secret created |
| `updated` | Secret value updated |
| `deleted` | Secret deleted |

#### Example (curl)

```bash
curl -N -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/secrets-watch/apps/myapp/"
```

---

## gRPC API

Service: `quicbaas.secrets.v1.SecretsService`

### Service Definition

```protobuf
service SecretsService {
  rpc StoreSecret(StoreSecretRequest) returns (StoreSecretResponse);
  rpc GetSecret(GetSecretRequest) returns (GetSecretResponse);
  rpc ListSecrets(ListSecretsRequest) returns (ListSecretsResponse);
  rpc GetSecretsByPrefix(GetSecretsByPrefixRequest) returns (GetSecretsByPrefixResponse);
  rpc WatchSecretsByPrefix(WatchSecretsRequest) returns (stream SecretChange);
  rpc DeleteSecret(DeleteSecretRequest) returns (DeleteSecretResponse);
  rpc ListSecretVersions(ListSecretVersionsRequest) returns (ListSecretVersionsResponse);
  rpc RollbackSecret(RollbackSecretRequest) returns (RollbackSecretResponse);
  rpc RotateSecret(RotateSecretRequest) returns (RotateSecretResponse);
  rpc UpdateSecretStatus(UpdateSecretStatusRequest) returns (UpdateSecretStatusResponse);
}
```

### StoreSecret

```protobuf
message StoreSecretRequest {
  string secret_id = 1;           // Optional for new secrets
  string name = 2;                // Required: secret name/path
  string description = 3;         // Optional description
  bytes data = 4;                 // Required: secret data
  map<string, string> metadata = 5;
  map<string, string> tags = 6;
  google.protobuf.Timestamp expires_at = 7;
  string tenant_id = 8;           // Optional: tenant ID
  string key_id = 9;              // Optional: custom key ID
  string algorithm = 10;          // Optional: encryption algorithm
}

message StoreSecretResponse {
  string secret_id = 1;
  uint64 version = 2;
  google.protobuf.Timestamp timestamp = 3;
}
```

### GetSecret

```protobuf
message GetSecretRequest {
  string secret_id = 1;           // Required: secret path
  uint64 version = 2;             // Optional: 0 = latest
}

message GetSecretResponse {
  string secret_id = 1;
  uint64 version = 2;
  bytes data = 3;                 // Decrypted secret data
  map<string, string> metadata = 4;
  google.protobuf.Timestamp created_at = 5;
  google.protobuf.Timestamp timestamp = 6;
  string algorithm = 7;           // Encryption algorithm used
  string key_identifier = 8;      // DEKID or KeyID used
}
```

### ListSecrets

```protobuf
message ListSecretsRequest {
  int32 limit = 1;                // Max 1000, default 100
  string cursor = 2;              // Pagination cursor
  map<string, string> filter = 3; // Filter by tags, prefix, etc.
}

message ListSecretsResponse {
  repeated Secret secrets = 1;
  string next_cursor = 2;
  bool has_more = 3;
  int32 total_count = 4;
  google.protobuf.Timestamp timestamp = 5;
}

message Secret {
  string secret_id = 1;
  string name = 2;
  string description = 3;
  uint64 current_version = 4;
  google.protobuf.Timestamp created_at = 5;
  google.protobuf.Timestamp updated_at = 6;
  SecretRotationPolicy rotation_policy = 7;
  map<string, string> tags = 8;
  string owner = 9;
}
```

### GetSecretsByPrefix

Bulk retrieve secrets matching a path prefix.

```protobuf
message GetSecretsByPrefixRequest {
  string prefix = 1;              // Required: path prefix to match
  bool include_data = 2;          // Optional: include secret data (default: true)
  int32 limit = 3;                // Optional: max results (default: 100)
  string cursor = 4;              // Optional: pagination cursor
}

message GetSecretsByPrefixResponse {
  map<string, SecretData> secrets = 1;  // Map of path to secret data
  string next_cursor = 2;
  bool has_more = 3;
  int32 total_count = 4;
  google.protobuf.Timestamp timestamp = 5;
}

message SecretData {
  map<string, bytes> data = 1;          // Secret key-value data
  map<string, string> metadata = 2;     // Secret metadata
  int64 version = 3;                    // Current version
}
```

### WatchSecretsByPrefix

Stream real-time changes for secrets matching a prefix. Returns a server-side stream.

```protobuf
message WatchSecretsRequest {
  string prefix = 1;              // Required: path prefix to watch
  int64 start_version = 2;        // Optional: start from this version
}

message SecretChange {
  string path = 1;                // Secret path
  SecretChangeType change_type = 2;
  map<string, bytes> data = 3;    // Updated data (empty on delete)
  map<string, string> metadata = 4;
  int64 version = 5;
  google.protobuf.Timestamp timestamp = 6;
}

enum SecretChangeType {
  SECRET_CHANGE_TYPE_UNSPECIFIED = 0;
  SECRET_CHANGE_TYPE_CREATED = 1;
  SECRET_CHANGE_TYPE_UPDATED = 2;
  SECRET_CHANGE_TYPE_DELETED = 3;
}
```

**Usage Example (Go):**

```go
stream, err := client.WatchSecretsByPrefix(ctx, &pb.WatchSecretsRequest{
    Prefix: "database/",
})
if err != nil {
    return err
}

for {
    change, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    fmt.Printf("Change: %s %s version=%d\n",
        change.ChangeType, change.Path, change.Version)
}
```

### DeleteSecret

```protobuf
message DeleteSecretRequest {
  string secret_id = 1;           // Required
  bool permanently_delete = 2;    // Default: false (soft delete)
}

message DeleteSecretResponse {
  string secret_id = 1;
  bool deleted = 2;
  google.protobuf.Timestamp timestamp = 3;
}
```

### ListSecretVersions

```protobuf
message ListSecretVersionsRequest {
  string secret_id = 1;           // Required
  int32 limit = 2;                // Max 1000, default 100
  string cursor = 3;
}

message ListSecretVersionsResponse {
  repeated SecretVersion versions = 1;
  string next_cursor = 2;
  bool has_more = 3;
  google.protobuf.Timestamp timestamp = 4;
}

message SecretVersion {
  string secret_id = 1;
  uint64 version = 2;
  bytes data = 3;                 // Optional, not returned by list
  map<string, string> metadata = 4;
  google.protobuf.Timestamp created_at = 5;
  string created_by = 6;
  google.protobuf.Timestamp deleted_at = 7;  // Null if not deleted
  google.protobuf.Timestamp expires_at = 8;
}
```

### RollbackSecret

```protobuf
message RollbackSecretRequest {
  string secret_id = 1;           // Required
  uint64 target_version = 2;      // Required: version to restore
}

message RollbackSecretResponse {
  string secret_id = 1;
  uint64 new_version = 2;         // New version created by rollback
  google.protobuf.Timestamp timestamp = 3;
}
```

### RotateSecret

```protobuf
message RotateSecretRequest {
  string secret_id = 1;           // Required
  bytes new_data = 2;             // Required: new secret data
  map<string, string> metadata = 3;
  string tenant_id = 4;           // Optional: inherits if not set
  string key_id = 5;              // Optional: inherits if not set
  string algorithm = 6;           // Optional: inherits if not set
}

message RotateSecretResponse {
  string secret_id = 1;
  uint64 old_version = 2;
  uint64 new_version = 3;
  google.protobuf.Timestamp timestamp = 4;
}
```

### UpdateSecretStatus

```protobuf
message UpdateSecretStatusRequest {
  string secret_id = 1;           // Required
  uint64 version = 2;             // Optional: 0 = current
  SecretStatus status = 3;        // Required: enabled/disabled
  bool all_versions = 4;          // Optional: apply to all versions
}

message UpdateSecretStatusResponse {
  string secret_id = 1;
  SecretStatus status = 2;
  int32 versions_updated = 3;
  google.protobuf.Timestamp timestamp = 4;
}

enum SecretStatus {
  SECRET_STATUS_UNSPECIFIED = 0;
  SECRET_STATUS_ENABLED = 1;
  SECRET_STATUS_DISABLED = 2;
}
```

---

## MCP Tools

Model Context Protocol tools for AI agent integration.

### quicbaas_secrets_store

Store a secret with automatic encryption.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (optional for new secrets)"
    },
    "name": {
      "type": "string",
      "description": "Human-readable secret name (required)"
    },
    "description": {
      "type": "string",
      "description": "Secret description"
    },
    "data": {
      "type": "string",
      "description": "Base64-encoded secret data (required)"
    },
    "metadata": {
      "type": "object",
      "description": "Version-specific metadata"
    },
    "tags": {
      "type": "object",
      "description": "User-defined tags"
    },
    "expires_at": {
      "type": "string",
      "description": "ISO 8601 expiration timestamp"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID for tenant-specific DEK"
    },
    "key_id": {
      "type": "string",
      "description": "Custom key identifier (DEKID or KeyID)"
    },
    "algorithm": {
      "type": "string",
      "description": "Encryption algorithm (AES-256-GCM, AES-128-GCM, ChaCha20-Poly1305)"
    }
  },
  "required": ["name", "data"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "version": 1,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

### quicbaas_secrets_get

Retrieve a secret's decrypted data.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "version": {
      "type": "integer",
      "description": "Specific version (optional, defaults to latest)"
    }
  },
  "required": ["secret_id"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "version": 2,
  "data": "base64-encoded-decrypted-data",
  "metadata": {},
  "created_at": "2024-01-15T10:30:00Z",
  "timestamp": "2024-01-15T10:35:00Z",
  "algorithm": "AES-256-GCM",
  "key_identifier": "dek:global"
}
```

---

### quicbaas_secrets_list

List all accessible secrets.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "limit": {
      "type": "integer",
      "description": "Maximum results (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor"
    },
    "filter": {
      "type": "object",
      "description": "Filter by tags, prefix, etc."
    }
  }
}
```

**Output:**

```json
{
  "secrets": [
    {
      "secret_id": "database/prod",
      "name": "database/prod",
      "current_version": 3,
      "created_at": "2024-01-10T10:00:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "total_count": 1
}
```

---

### quicbaas_secrets_get_prefix

Bulk retrieve secrets matching a path prefix.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string",
      "description": "Path prefix to match (required)"
    },
    "include_data": {
      "type": "boolean",
      "description": "Include secret data in response (default: true)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum results (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor"
    }
  },
  "required": ["prefix"]
}
```

**Output:**

```json
{
  "secrets": {
    "database/prod": {
      "data": {
        "password": "c2VjcmV0MTIz",
        "username": "YWRtaW4="
      },
      "metadata": {
        "owner": "devops"
      },
      "version": 3
    }
  },
  "total_count": 1,
  "has_more": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

### quicbaas_secrets_watch

Watch for real-time secret changes. Returns streaming updates.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string",
      "description": "Path prefix to watch (required)"
    },
    "start_version": {
      "type": "integer",
      "description": "Start watching from this version (optional)"
    },
    "timeout_seconds": {
      "type": "integer",
      "description": "Watch timeout in seconds (optional, 0 = indefinite)"
    }
  },
  "required": ["prefix"]
}
```

**Output (streaming):**

Each change event:

```json
{
  "path": "database/prod",
  "change_type": "UPDATED",
  "data": {
    "password": "bmV3c2VjcmV0"
  },
  "metadata": {},
  "version": 4,
  "timestamp": "2024-01-15T11:20:15Z"
}
```

---

### quicbaas_secrets_delete

Delete a secret (soft delete by default).

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "permanently_delete": {
      "type": "boolean",
      "description": "Permanent deletion (default: false)"
    }
  },
  "required": ["secret_id"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "deleted": true,
  "timestamp": "2024-01-15T10:45:00Z"
}
```

---

### quicbaas_secrets_versions

List all versions of a secret.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum results (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor"
    }
  },
  "required": ["secret_id"]
}
```

**Output:**

```json
{
  "versions": [
    {
      "version": 3,
      "created_at": "2024-01-15T10:35:00Z",
      "created_by": "rotation-service",
      "deleted_at": null,
      "expires_at": null
    }
  ],
  "has_more": false
}
```

---

### quicbaas_secrets_rollback

Rollback to a previous version.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "target_version": {
      "type": "integer",
      "description": "Version to rollback to (required)"
    }
  },
  "required": ["secret_id", "target_version"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "new_version": 4,
  "timestamp": "2024-01-15T11:00:00Z"
}
```

---

### quicbaas_secrets_rotate

Rotate a secret to a new value.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "new_data": {
      "type": "string",
      "description": "Base64-encoded new secret data (required)"
    },
    "metadata": {
      "type": "object",
      "description": "Version metadata"
    },
    "tenant_id": {
      "type": "string",
      "description": "Tenant ID (inherits if not set)"
    },
    "key_id": {
      "type": "string",
      "description": "Custom key ID (inherits if not set)"
    },
    "algorithm": {
      "type": "string",
      "description": "Encryption algorithm (inherits if not set)"
    }
  },
  "required": ["secret_id", "new_data"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "old_version": 3,
  "new_version": 4,
  "timestamp": "2024-01-15T11:05:00Z"
}
```

---

### quicbaas_secrets_set_rotation_policy

Configure automatic rotation for a secret.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    },
    "policy": {
      "type": "object",
      "description": "Rotation policy configuration (required)",
      "properties": {
        "enabled": {
          "type": "boolean",
          "description": "Enable automatic rotation"
        },
        "interval": {
          "type": "string",
          "description": "Rotation interval (e.g., '24h', '7d', '30d')"
        },
        "retain_versions": {
          "type": "integer",
          "description": "Number of old versions to retain"
        },
        "rotation_lambda": {
          "type": "string",
          "description": "Function ARN for custom rotation logic"
        }
      }
    }
  },
  "required": ["secret_id", "policy"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "policy": {
    "enabled": true,
    "interval": "720h",
    "next_rotation": "2024-02-15T10:30:00Z",
    "retain_versions": 5,
    "rotation_lambda": ""
  },
  "timestamp": "2024-01-15T11:10:00Z"
}
```

---

### quicbaas_secrets_get_rotation_policy

Retrieve the current rotation policy.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "secret_id": {
      "type": "string",
      "description": "Secret identifier (required)"
    }
  },
  "required": ["secret_id"]
}
```

**Output:**

```json
{
  "secret_id": "database/prod",
  "policy": {
    "enabled": true,
    "interval": "720h",
    "next_rotation": "2024-02-15T10:30:00Z",
    "retain_versions": 5,
    "rotation_lambda": ""
  },
  "timestamp": "2024-01-15T11:15:00Z"
}
```

---

### quicbaas_secrets_get_prefix

Retrieve all secrets matching a path prefix for bulk operations.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string",
      "description": "Path prefix to match (e.g., 'apps/myapp/') (required)"
    },
    "include_data": {
      "type": "boolean",
      "description": "Include decrypted secret data (default: true)"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum secrets to return (default: 100)"
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor for next page"
    }
  },
  "required": ["prefix"]
}
```

**Output:**

```json
{
  "secrets": {
    "apps/myapp/database": {"password": "secret123"},
    "apps/myapp/cache": {"password": "cache456"}
  },
  "metadata": {
    "apps/myapp/database": {"env": "production"}
  },
  "total_count": 2,
  "has_more": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

### quicbaas_secrets_watch

Watch for real-time changes to secrets under a prefix.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string",
      "description": "Path prefix to watch (e.g., 'apps/myapp/') (required)"
    },
    "start_version": {
      "type": "integer",
      "description": "Start watching from this version (default: 0 = current)"
    },
    "timeout_seconds": {
      "type": "integer",
      "description": "Maximum time to wait for changes (default: 30, 0 = indefinite)"
    }
  },
  "required": ["prefix"]
}
```

**Output:**

```json
{
  "changes": [
    {
      "path": "apps/myapp/db",
      "change_type": "updated",
      "version": 5,
      "timestamp": "2024-01-15T10:30:00Z"
    }
  ],
  "count": 1
}
```

**Change Types:**
- `created` - New secret created
- `updated` - Secret value updated
- `deleted` - Secret deleted

---

## Encryption Options

### Supported Algorithms

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| `AES-128-GCM` | 128-bit | Fast, NIST approved |
| `AES-192-GCM` | 192-bit | Higher security margin |
| `AES-256-GCM` | 256-bit | Default, maximum security |
| `ChaCha20-Poly1305` | 256-bit | Constant-time, no AES-NI needed |

### Key Identifiers

**DEKID Format** (managed by QuicBaaS):
- `dek:global` - Global secrets DEK
- `dek:tenant:{id}` - Tenant-specific DEK

**KeyID Format** (go-xkms):
- `my-key` - Simple key name
- `software::AES-256-GCM:my-key` - Full format
- `pkcs11:::my-hsm-key` - HSM-backed key

### Inheritance on Rotation

When rotating a secret, encryption options are inherited from the previous version unless explicitly specified:

| Parameter | Behavior |
|-----------|----------|
| `tenant_id` | Inherits if not specified |
| `key_id` | Inherits if not specified |
| `algorithm` | Inherits if not specified |

---

## Error Codes

### HTTP Status Codes

| Status | Description |
|--------|-------------|
| 200 | Success |
| 201 | Created |
| 400 | Bad Request (validation error) |
| 403 | Forbidden (permission denied) |
| 404 | Not Found |
| 408 | Request Timeout |
| 409 | Conflict (already exists) |
| 410 | Gone (soft-deleted) |
| 500 | Internal Server Error |
| 503 | Service Unavailable |

### Error Response Format

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable description",
    "field": "field_name"
  }
}
```

### Error Codes

| Code | HTTP | Description |
|------|------|-------------|
| `VALIDATION_ERROR` | 400 | Invalid request parameters |
| `NOT_FOUND` | 404 | Secret or version not found |
| `CONFLICT` | 409 | Secret already exists |
| `FORBIDDEN` | 403 | Permission denied |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `SERVICE_UNAVAILABLE` | 503 | Leader not available |
| `TIMEOUT` | 408 | Operation timeout |

---

## See Also

- [CLI Usage](cli.md) - Command-line interface guide
- Configuration - Server configuration options
- [Architecture](architecture.md) - Encryption and storage design
