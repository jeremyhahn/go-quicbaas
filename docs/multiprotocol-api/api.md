# Multiprotocol API — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/multiprotocol-api/`.

HTTP/JSON interface for database operations.

## Base URL

```
http://localhost:8080/api/v1
```

For HTTPS:
```
https://localhost:8443/api/v1
```

## Authentication

### API Key

```bash
curl -H "X-API-Key: your-api-key-here" \
  http://localhost:8080/api/v1/health
```

### Mutual TLS

```bash
curl --cert client.crt \
     --key client.key \
     --cacert ca.crt \
     https://localhost:8443/api/v1/health
```

### Token Introspection

`Security().Auth().IntrospectToken` (RFC 7662) is served on every transport
and returns the same `service.TokenIntrospection`, including `AttestLevel`
and `AttestAAGUID`:

| Transport | Endpoint | Caller requirement |
|-----------|----------|--------------------|
| REST, QUIC | `POST /oauth2/introspect` (form field `token`) | mTLS or client credentials; otherwise `401 invalid_client` |
| gRPC, Unix | `QuicBaaS/IntrospectToken` | authenticated, `user_authfactors:login` |
| MCP | tool `quicbaas_introspecttoken` (`token`) | authenticated, `user_authfactors:login` |
| Embedded | in-process | the host process |

An inactive, expired, revoked or unparseable token returns only
`Active=false`. A missing token is `invalid_input` (`400 invalid_request` on
REST). See [OAuth2/OIDC engineering](../oauth2-oidc/engineering.md#every-transport).

## OpenAPI Documentation

Interactive docs: `http://localhost:8080/swagger/`

Specification: `http://localhost:8080/swagger/openapi.yaml`

## Core Operations

### PUT

`PUT /api/v1/entities/{type}/{id}`
```bash
curl -X PUT http://localhost:8080/api/v1/entities/user/1 \
  -H "Content-Type: application/json" \
  -d '{
    "data": "eyJuYW1lIjoiSm9obiIsImVtYWlsIjoiam9obkBleGFtcGxlLmNvbSJ9",
    "consistency": "quorum"
  }'
```

Request body:
```json
{
  "data": "base64-encoded-entity-data",
  "consistency": "local|quorum",
  "ttl_seconds": 3600,
  "metadata": {
    "source": "api",
    "version": "1.0"
  }
}
```

Response:
```json
{
  "id": 1,
  "version": 1,
  "timestamp": 1696550400000000000,
  "leader_id": 1
}
```

### GET

`GET /api/v1/entities/{type}/{id}`
```bash
curl http://localhost:8080/api/v1/entities/user/1?consistency=quorum
```

Query parameters:
- `consistency` - `local` (default) or `quorum`
- `include_metadata` - `true` or `false` (default)

Response:
```json
{
  "id": 1,
  "data": "eyJuYW1lIjoiSm9obiIsImVtYWlsIjoiam9obkBleGFtcGxlLmNvbSJ9",
  "version": 1,
  "timestamp": 1696550400000000000,
  "found": true,
  "metadata": {
    "source": "api",
    "version": "1.0"
  }
}
```

Not found (404):
```json
{
  "error": "resource not found: entity.get: not_found: user/1",
  "timestamp": "2026-10-12T12:00:00Z",
  "request_id": "…",
  "retryable": false
}
```

### DELETE

`DELETE /api/v1/entities/{type}/{id}`
```bash
curl -X DELETE http://localhost:8080/api/v1/entities/user/1?consistency=quorum
```

Query parameters:
- `consistency` - `local` (default) or `quorum`
- `version` - Optional version for optimistic locking

Response:
```json
{
  "deleted": true,
  "timestamp": 1696550400000000000
}
```

### LIST

`GET /api/v1/entities/{type}`
```bash
curl "http://localhost:8080/api/v1/entities/user?page_size=100&consistency=local"
```

Query parameters:
- `page_size` - Number of results per page (default: 100, max: 1000)
- `page_token` - Continuation token from previous response
- `consistency` - `local` (default) or `quorum`
- `include_metadata` - `true` or `false` (default)

Response:
```json
{
  "entities": [
    {
      "id": 1,
      "entity_type": "user",
      "data": "eyJuYW1lIjoiSm9obiJ9",
      "version": 1,
      "timestamp": 1696550400000000000
    },
    {
      "id": 2,
      "entity_type": "user",
      "data": "eyJuYW1lIjoiSmFuZSJ9",
      "version": 1,
      "timestamp": 1696550500000000000
    }
  ],
  "next_page_token": "eyJsYXN0X2lkIjoyfQ==",
  "total_count": 150
}
```

### LIST ENTITY TYPES

`GET /api/v1/entity-types`
```bash
curl "http://localhost:8080/api/v1/entity-types?limit=100"
```

Discover all unique entity types stored in the database. In multi-tenant mode, only returns entity types within the tenant's keyspace.

Query parameters:
- `limit` - Number of results per page (default: 100, max: 1000)
- `cursor` - Continuation token from previous response

Response:
```json
{
  "entity_types": ["Organization", "User", "Zone"],
  "next_page_token": "",
  "total_count": 3
}
```

### QUERY

`POST /api/v1/entities/{type}/query`
```bash
curl -X POST http://localhost:8080/api/v1/entities/user/query \
  -H "Content-Type: application/json" \
  -d '{
    "index_name": "email",
    "value": "am9obkBleGFtcGxlLmNvbQ==",
    "page_size": 50,
    "consistency": "local"
  }'
```

Request body:
```json
{
  "index_name": "email",
  "value": "base64-encoded-search-value",
  "range_start": "base64-encoded-start",
  "range_end": "base64-encoded-end",
  "page_size": 100,
  "page_token": "continuation-token",
  "consistency": "local|quorum",
  "include_metadata": false
}
```

Response:
```json
{
  "entities": [
    {
      "id": 1,
      "entity_type": "user",
      "data": "eyJuYW1lIjoiSm9obiIsImVtYWlsIjoiam9obkBleGFtcGxlLmNvbSJ9",
      "version": 1,
      "timestamp": 1696550400000000000
    }
  ],
  "next_page_token": "",
  "total_count": 1
}
```

## Batch Operations

### Batch Put

`POST /api/v1/entities/{type}/batch`

Store multiple entities atomically.

```bash
curl -X POST http://localhost:8080/api/v1/entities/user/batch \
  -H "Content-Type: application/json" \
  -d '{
    "operations": [
      {
        "id": 1,
        "data": "eyJuYW1lIjoiSm9obiJ9",
        "consistency": "quorum"
      },
      {
        "id": 2,
        "data": "eyJuYW1lIjoiSmFuZSJ9",
        "consistency": "quorum"
      }
    ]
  }'
```

Response:
```json
{
  "responses": [
    {
      "id": 1,
      "version": 1,
      "timestamp": 1696550400000000000,
      "leader_id": 1
    },
    {
      "id": 2,
      "version": 1,
      "timestamp": 1696550400000000000,
      "leader_id": 1
    }
  ],
  "timestamp": 1696550400000000000
}
```

### Batch Delete

`DELETE /api/v1/entities/{type}/batch`

Delete multiple entities atomically.

```bash
curl -X DELETE http://localhost:8080/api/v1/entities/user/batch \
  -H "Content-Type: application/json" \
  -d '{
    "ids": [1, 2, 3],
    "consistency": "quorum"
  }'
```

Response:
```json
{
  "responses": [
    {
      "deleted": true,
      "timestamp": 1696550400000000000
    },
    {
      "deleted": true,
      "timestamp": 1696550400000000000
    },
    {
      "deleted": false,
      "timestamp": 1696550400000000000
    }
  ],
  "timestamp": 1696550400000000000
}
```

## Transaction Operations

### Transaction

`POST /api/v1/transaction`

Execute atomic compare-and-swap transaction with ACID guarantees.

```bash
curl -X POST http://localhost:8080/api/v1/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "compare": [
      {
        "entity_type": "user",
        "id": 1,
        "target": "version",
        "operator": "equal",
        "version": 5
      }
    ],
    "success": [
      {
        "op_type": "put",
        "entity_type": "user",
        "id": 1,
        "data": "eyJuYW1lIjoiSm9obiBVcGRhdGVkIn0="
      }
    ],
    "failure": [
      {
        "op_type": "get",
        "entity_type": "user",
        "id": 1
      }
    ],
    "consistency": "quorum"
  }'
```

Request body:
```json
{
  "compare": [
    {
      "entity_type": "user",
      "id": 1,
      "target": "version|value|exists|create|mod",
      "operator": "equal|not_equal|greater|less|greater_equal|less_equal",
      "version": 5,
      "value": "base64-encoded-value",
      "timestamp": 1696550400000000000
    }
  ],
  "success": [
    {
      "op_type": "put|get|delete",
      "entity_type": "user",
      "id": 1,
      "data": "base64-encoded-data",
      "metadata": {
        "key": "value"
      }
    }
  ],
  "failure": [
    {
      "op_type": "put|get|delete",
      "entity_type": "user",
      "id": 1,
      "data": "base64-encoded-data"
    }
  ],
  "consistency": "local|quorum",
  "timeout_ms": 30000
}
```

Response (success):
```json
{
  "succeeded": true,
  "responses": [
    {
      "op_type": "put",
      "entity_type": "user",
      "id": 1,
      "version": 6,
      "timestamp": 1696550400000000000,
      "success": true
    }
  ],
  "timestamp": 1696550400000000000,
  "message": "Transaction committed successfully",
  "revision": 1234
}
```

Response (failure):
```json
{
  "succeeded": false,
  "responses": [
    {
      "op_type": "get",
      "entity_type": "user",
      "id": 1,
      "data": "eyJuYW1lIjoiSm9obiJ9",
      "version": 7,
      "timestamp": 1696550400000000000,
      "success": true
    }
  ],
  "timestamp": 1696550400000000000,
  "message": "Version mismatch: expected 5, got 7",
  "revision": 1234
}
```

Common patterns:

Optimistic locking:
```bash
# Compare version and update if it matches
curl -X POST http://localhost:8080/api/v1/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "compare": [{"entity_type": "user", "id": 1, "target": "version", "operator": "equal", "version": 5}],
    "success": [{"op_type": "put", "entity_type": "user", "id": 1, "data": "..."}],
    "consistency": "quorum"
  }'
```

Conditional create (only if not exists):
```bash
curl -X POST http://localhost:8080/api/v1/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "compare": [{"entity_type": "user", "id": 1, "target": "exists", "operator": "equal", "version": 0}],
    "success": [{"op_type": "put", "entity_type": "user", "id": 1, "data": "..."}],
    "consistency": "quorum"
  }'
```

Multi-operation atomic update:
```bash
curl -X POST http://localhost:8080/api/v1/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "compare": [
      {"entity_type": "account", "id": 1, "target": "version", "operator": "equal", "version": 10},
      {"entity_type": "account", "id": 2, "target": "version", "operator": "equal", "version": 5}
    ],
    "success": [
      {"op_type": "put", "entity_type": "account", "id": 1, "data": "eyJiYWxhbmNlIjo5MDB9"},
      {"op_type": "put", "entity_type": "account", "id": 2, "data": "eyJiYWxhbmNlIjoxMTAwfQ=="}
    ],
    "consistency": "quorum"
  }'
```

## Backup Operations

Manage database backups through the REST API.

### Create Backup

`POST /api/v1/backups`

Create a new backup of the database.

```bash
curl -X POST http://localhost:8080/api/v1/backups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "prod-backup",
    "description": "Weekly production backup",
    "compression": "zstd",
    "encryption": "aes-256-gcm",
    "include_shards": [1, 2, 3]
  }'
```

Request body:
```json
{
  "name": "backup-name",
  "description": "optional description",
  "compression": "none|gzip|zstd",
  "encryption": "none|aes-256-gcm",
  "include_shards": [1, 2, 3]
}
```

Response (201 Created):
```json
{
  "backup_id": "backup-prod-backup-1729123456",
  "timestamp": 1729123456000000000,
  "size": 104857600,
  "status": "completed"
}
```

### List Backups

`GET /api/v1/backups`

List all available backups.

```bash
curl "http://localhost:8080/api/v1/backups?limit=10&offset=0"
```

Query parameters:
- `limit` - Maximum number of backups to return (default: 100)
- `offset` - Number of backups to skip for pagination

Response:
```json
{
  "backups": [
    {
      "backup_id": "backup-prod-backup-1729123456",
      "name": "prod-backup",
      "description": "Weekly production backup",
      "size": 104857600,
      "timestamp": 1729123456000000000,
      "status": "completed",
      "shards": [1, 2, 3]
    }
  ],
  "total_count": 1
}
```

### Delete Backup

`DELETE /api/v1/backups/{id}`

Delete a backup from storage.

```bash
curl -X DELETE http://localhost:8080/api/v1/backups/backup-prod-backup-1729123456
```

Response:
```json
{
  "success": true
}
```

### Restore Backup

`POST /api/v1/backups/{id}/restore`

Restore data from a backup.

```bash
curl -X POST http://localhost:8080/api/v1/backups/backup-prod-backup-1729123456/restore \
  -H "Content-Type: application/json" \
  -d '{
    "target_shards": [1, 2, 3]
  }'
```

Request body:
```json
{
  "backup_id": "backup-prod-backup-1729123456",
  "target_shards": [1, 2, 3]
}
```

Response:
```json
{
  "job_id": "restore-abc123",
  "status": "in_progress",
  "progress": 0.0
}
```

### Verify Backup

`POST /api/v1/backups/{id}/verify`

Verify the integrity of a backup. Checks metadata validity, shard snapshot presence, SHA-256 checksums, and data structure integrity.

```bash
curl -X POST http://localhost:8080/api/v1/backups/backup-prod-backup-1729123456/verify
```

Response:
```json
{
  "valid": true,
  "checksum_match": true,
  "issues": []
}
```

Response (integrity failure):
```json
{
  "valid": false,
  "checksum_match": false,
  "issues": [
    "Shard 2 checksum mismatch: expected sha256:abc123, got sha256:def456"
  ]
}
```

### Get Backup Status

`GET /api/v1/backups/{id}/status`

Get the current status of a backup, including integrity verification results.

```bash
curl http://localhost:8080/api/v1/backups/backup-prod-backup-1729123456/status
```

Response:
```json
{
  "backup_id": "backup-prod-backup-1729123456",
  "valid": true,
  "checksum_match": true,
  "issues": []
}
```

## Cluster Operations

### Cluster Status

`GET /api/v1/cluster/status`
```bash
curl http://localhost:8080/api/v1/cluster/status?include_metrics=true
```

Response:
```json
{
  "cluster_id": "quicbaas-cluster-1",
  "nodes": [
    {
      "node_id": 1,
      "address": "localhost:8080",
      "raft_address": "localhost:9090",
      "is_leader": true,
      "health_status": "HEALTHY",
      "version": "0.1.0-alpha",
      "uptime_seconds": 3600,
      "metrics": {
        "requests_per_second": 1250.5,
        "avg_latency_ms": 1.2,
        "storage_size_mb": 1024.5
      }
    }
  ],
  "shards": [
    {
      "shard_id": 1,
      "leader_id": 1,
      "replicas": [1, 2, 3],
      "entity_types": ["user", "session"],
      "status": "HEALTHY"
    }
  ],
  "timestamp": 1696550400000000000
}
```

### Health Check

`GET /api/v1/health`

```bash
curl http://localhost:8080/api/v1/health
```

Response:
```json
{
  "status": "HEALTHY",
  "node_id": 1,
  "version": "0.1.0-alpha",
  "uptime_seconds": 3600,
  "details": {
    "storage": {
      "status": "HEALTHY",
      "message": "Storage engine operational",
      "last_check": 1696550400000000000
    },
    "raft": {
      "status": "HEALTHY",
      "message": "Raft consensus operational",
      "last_check": 1696550400000000000
    }
  }
}
```

## Error Handling

REST returns a flat JSON object; the QUIC transport carries the same REST
requests over HTTP/3 and gets the same body. There is no nested envelope:

```json
{
  "error": "unavailable: the request could not be completed",
  "code": "unavailable",
  "timestamp": "2026-10-12T12:00:00Z",
  "request_id": "…",
  "retryable": true
}
```

| Field | Type | Present |
|-------|------|---------|
| `error` | string | always; human-readable message |
| `code` | string | when the error carries a code (see below) |
| `details` | object | when the error carries context |
| `timestamp` | RFC 3339 time | always |
| `request_id` | string | when the request has an ID |
| `retryable` | bool | always |

Error codes are lower-case strings from `pkg/errors`. The HTTP status and
`retryable` value come from the code:

| Code | HTTP Status | Retryable | Description |
|------|-------------|-----------|-------------|
| `invalid_input` | 400 | no | Invalid request parameters |
| `validation` | 400 | no | Request failed validation |
| `unauthorized` | 401 | no | Authentication required |
| `forbidden` | 403 | no | Permission denied |
| `not_allowed` | 403 | no | Operation not allowed for this caller |
| `cross_tenant_access` | 403 | no | Request names a tenant that is not the caller's |
| `not_found` | 404 | no | Entity not found |
| `timeout` | 408 | yes | Request timed out |
| `deadline_exceeded` | 408 | yes | Request deadline passed |
| `conflict` | 409 | no | Version conflict (optimistic locking) |
| `already_exists` | 409 | no | Entity already exists |
| `rate_limited` | 429 | yes | Rate limit exceeded; see [Rate Limiting](#rate-limiting) |
| `internal` | 500 | no | Internal server error |
| `unavailable` | 503 | yes | Service temporarily unavailable |
| `leader_not_available` | 503 | yes | Raft leader election in progress |
| `cluster_not_ready` | 503 | yes | Cluster still forming |
| `circuit_open` | 503 | yes | Downstream circuit breaker open |

The entity, batch, query, cluster and scheduler routes answer the
client-error classes (400, 401, 403, 404, 408, 409) without a `code` field and
with `retryable: false`; branch on the HTTP status for those. Server-side
classes (500, 503) and `rate_limited` always carry `code` and `retryable`.
Server-side messages are replaced by a fixed sentence; the cause is logged.

gRPC and the Unix socket (gRPC over a local socket) return a gRPC status:
the code maps to the nearest status (`not_found` to `NOT_FOUND`,
`rate_limited` to `RESOURCE_EXHAUSTED`, ...; the full map is in the
[gRPC status table](#grpc-status-codes)) and the message reads
`op: code: message`, for example
`entity.get: not_found: user/1`. MCP returns a JSON-RPC 2.0 error; see the
[MCP API](../mcp-server/api.md#standard-json-rpc-errors).

## Rate Limiting

Each source IP may send 1000 requests per second by default, with a burst of
the same size. The budget covers every REST route, including the module
routes the unified API server serves. Set `server.rest.rate_limit` to change
the limit, or to `-1` to turn it off. Over the limit, the server returns HTTP
429 with `Retry-After: 1` and the code `rate_limited`. Core and module routes
return the same body. No `X-RateLimit-*` headers are sent.

Over gRPC, a `rate_limited` error arrives as `RESOURCE_EXHAUSTED` with
`rate_limited` in the status message. gRPC carries no retry-after hint. The Go
SDK reports it as the retryable `rate_limited` code on both transports.

```json
{
  "error": "rate limit exceeded: 1000 requests per 1s",
  "code": "rate_limited",
  "timestamp": "2026-10-12T12:00:00Z",
  "request_id": "…",
  "retryable": true
}
```

See [Rate Limiting Middleware](../security-hardening/api.md#rate-limiting-middleware).

## Examples

### CRUD Operations

```bash
# 1. Create a user
curl -X PUT http://localhost:8080/api/v1/entities/user/1 \
  -H "Content-Type: application/json" \
  -d '{
    "data": "eyJuYW1lIjoiSm9obiIsImVtYWlsIjoiam9obkBleGFtcGxlLmNvbSJ9",
    "consistency": "quorum"
  }'

# 2. Read the user
curl http://localhost:8080/api/v1/entities/user/1?consistency=quorum

# 3. Update the user (same as create)
curl -X PUT http://localhost:8080/api/v1/entities/user/1 \
  -H "Content-Type: application/json" \
  -d '{
    "data": "eyJuYW1lIjoiSm9obiBEb2UiLCJlbWFpbCI6ImpvaG5AZXhhbXBsZS5jb20ifQ==",
    "consistency": "quorum"
  }'

# 4. Delete the user
curl -X DELETE http://localhost:8080/api/v1/entities/user/1?consistency=quorum
```

### Query by Index

```bash
# Query users by email
curl -X POST http://localhost:8080/api/v1/entities/user/query \
  -H "Content-Type: application/json" \
  -d '{
    "index_name": "email",
    "value": "am9obkBleGFtcGxlLmNvbQ==",
    "consistency": "local"
  }'
```

## Client Libraries

### JavaScript/TypeScript

```typescript
import { QuicBaaSClient } from '@quicbaas/client';

const client = new QuicBaaSClient({
  baseURL: 'http://localhost:8080/api/v1',
  apiKey: 'your-api-key'
});

// Put operation
await client.put('user', 1, {
  name: 'John',
  email: 'john@example.com'
});

// Get operation
const user = await client.get('user', 1);
console.log(user);
```

#### 64-bit integers in REST responses (`@quicbaas/sdk`)

The daemon writes Go `uint64`/`int64` values as bare JSON numbers, and a
JavaScript `number` holds integers exactly only up to 2^53. The TypeScript
SDK's REST client parses every response body with
`parseJSONPreservingLargeIntegers` (`sdk/typescript/src/json.ts`), which
applies one rule by member name, with no per-endpoint table:

| Key | Integer past 2^53 arrives as | Why |
|-----|------------------------------|-----|
| `id`, `ids`, `*_id`, `*_ids`, `*Id`, `*Ids`, `*ID`, `*IDs` (and elements of an array under such a key) | its exact decimal **string** | An identity: a rounded id names a record the server never issued. |
| Any other key (`created_at`, `timestamp`, `size`, `sequence`, …) | a `number`, as `JSON.parse` gives | A quantity: a `time.UnixNano` timestamp loses under a microsecond and still names the right millisecond. |
| No key (a bare top-level value or top-level array) | its exact decimal string | Nothing to judge it by, so it is kept exact. |

An integer at or below 2^53 is a `number` under every key. Fields that can
carry either form are typed `number | string` — `RAGDocument.id`
(`RAGDocumentID`, a content hash) and `PlacementNodeInfo.lease_id` (a
registry lease). Pass such a value back to the SDK unchanged; never
`Number()` it. Raft `shard_id`, `node_id` and `leader_id` stay `number`:
they are operator-configured replica ids, far below 2^53.

### Python

```python
from quicbaas import Client

client = Client(
    base_url='http://localhost:8080/api/v1',
    api_key='your-api-key'
)

# Put operation
client.put('user', 1, {
    'name': 'John',
    'email': 'john@example.com'
})

# Get operation
user = client.get('user', 1)
print(user)
```

## Ollama Operations

QuicBaaS integrates with Ollama for embedding generation. These endpoints require Ollama to be configured and running.

### Ollama Health

`GET /api/v1/ollama/health`

Check Ollama connectivity and status.

```bash
curl http://localhost:8080/api/v1/ollama/health
```

Response:
```json
{
  "available": true,
  "version": "0.1.40",
  "base_url": "http://localhost:11434",
  "last_checked": "2025-01-10T12:00:00Z"
}
```

### List Models

`GET /api/v1/ollama/models`

List available Ollama models.

```bash
curl http://localhost:8080/api/v1/ollama/models
```

Response:
```json
{
  "models": [
    {
      "name": "nomic-embed-text",
      "size": 274302464,
      "digest": "0a109f422b47",
      "modified_at": "2025-01-10T10:00:00Z"
    },
    {
      "name": "llama3.2",
      "size": 2048000000,
      "digest": "abc123def456",
      "modified_at": "2025-01-09T15:30:00Z"
    }
  ],
  "count": 2
}
```

### Get Model Info

`GET /api/v1/ollama/models/{model}`

Get detailed information about a specific model.

```bash
curl http://localhost:8080/api/v1/ollama/models/nomic-embed-text
```

Response:
```json
{
  "model": "nomic-embed-text",
  "details": {
    "license": "Apache 2.0",
    "format": "gguf",
    "family": "nomic",
    "parameter_size": "137M",
    "quantization_level": "Q4_0"
  }
}
```

### Generate Embedding

`POST /api/v1/ollama/embed`

Generate an embedding for a single text using the default model.

```bash
curl -X POST http://localhost:8080/api/v1/ollama/embed \
  -H "Content-Type: application/json" \
  -d '{
    "text": "What is QuicBaaS?"
  }'
```

Response:
```json
{
  "embedding": [0.1234, -0.5678, 0.9012, ...],
  "model": "nomic-embed-text",
  "dimensions": 768,
  "processing_time": "45ms"
}
```

### Generate Embedding with Model

`POST /api/v1/ollama/embed/{model}`

Generate an embedding using a specific model.

```bash
curl -X POST http://localhost:8080/api/v1/ollama/embed/mxbai-embed-large \
  -H "Content-Type: application/json" \
  -d '{
    "text": "What is QuicBaaS?"
  }'
```

### Batch Embed

`POST /api/v1/ollama/embed/batch`

Generate embeddings for multiple texts.

```bash
curl -X POST http://localhost:8080/api/v1/ollama/embed/batch \
  -H "Content-Type: application/json" \
  -d '{
    "texts": [
      "First document",
      "Second document",
      "Third document"
    ],
    "model": "nomic-embed-text"
  }'
```

Response:
```json
{
  "embeddings": [
    {"values": [0.1, -0.2, ...]},
    {"values": [0.3, -0.4, ...]},
    {"values": [0.5, -0.6, ...]}
  ],
  "model": "nomic-embed-text",
  "dimensions": 768,
  "count": 3,
  "processing_time": "120ms"
}
```

## RAG Operations

Retrieval-Augmented Generation context building operations.

### RAG Query

`POST /api/v1/rag/query`

Perform an all-in-one RAG query: generate embedding, search vectors, and build context.

```bash
curl -X POST http://localhost:8080/api/v1/rag/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How does error handling work?",
    "namespace": "codebase",
    "top_k": 10,
    "max_tokens": 4000,
    "min_score": 0.7,
    "template": "markdown",
    "include_citations": true
  }'
```

Request body:
```json
{
  "query": "Search query text",
  "namespace": "vector-namespace",
  "top_k": 10,
  "max_tokens": 4000,
  "min_score": 0.7,
  "template": "markdown|json|xml|plain",
  "include_citations": true,
  "model": "embedding-model-name"
}
```

Response:
```json
{
  "context": "## Relevant Code\n\n[1] src/errors/handler.go:23-45\n```go\ntype ErrorHandler struct {...}\n```\n...",
  "tokens": 3200,
  "truncated": false,
  "result_count": 8,
  "citations": [
    {
      "index": 1,
      "file_path": "src/errors/handler.go",
      "start_line": 23,
      "end_line": 45,
      "score": 0.94
    },
    {
      "index": 2,
      "file_path": "src/errors/types.go",
      "start_line": 10,
      "end_line": 25,
      "score": 0.89
    }
  ]
}
```

### Build Context

`POST /api/v1/rag/context`

Build LLM context from user-provided search results.

```bash
curl -X POST http://localhost:8080/api/v1/rag/context \
  -H "Content-Type: application/json" \
  -d '{
    "results": [
      {
        "content": "func main() {\n    fmt.Println(\"Hello\")\n}",
        "score": 0.92,
        "metadata": {
          "file_path": "main.go",
          "start_line": "1",
          "end_line": "10"
        }
      },
      {
        "content": "type Config struct {\n    Name string\n}",
        "score": 0.85,
        "metadata": {
          "file_path": "config.go",
          "start_line": "5",
          "end_line": "8"
        }
      }
    ],
    "max_tokens": 2000,
    "template": "markdown",
    "include_citations": true,
    "truncation_strategy": "smart"
  }'
```

Request body:
```json
{
  "results": [
    {
      "content": "text content",
      "score": 0.95,
      "metadata": {
        "file_path": "path/to/file",
        "start_line": "10",
        "end_line": "20"
      }
    }
  ],
  "max_tokens": 4000,
  "template": "markdown|json|xml|plain",
  "include_citations": true,
  "truncation_strategy": "smart|hard|none"
}
```

Response:
```json
{
  "context": "## Context\n\n[1] main.go:1-10\n```go\nfunc main() {...}\n```\n\n[2] config.go:5-8\n```go\ntype Config struct {...}\n```",
  "tokens": 150,
  "truncated": false,
  "result_count": 2,
  "citations": [
    {
      "index": 1,
      "file_path": "main.go",
      "start_line": 1,
      "end_line": 10,
      "score": 0.92
    },
    {
      "index": 2,
      "file_path": "config.go",
      "start_line": 5,
      "end_line": 8,
      "score": 0.85
    }
  ]
}
```

## See Also

- gRPC API - Binary protocol
- CLI Documentation - Command-line interface
- [Architecture](architecture.md) - System design
- [RAG MCP Tools](../rag/api.md) - RAG MCP tool reference
- [Backup and Recovery](../backup-restore/architecture.md) - Backup operations guide


---

## gRPC API

High-performance binary protocol for database operations.

### Protocol Definition

```
api/proto/quicbaas.proto
```

### Service Overview

```protobuf
service QuicBaaS {
  // CRUD Operations
  rpc Put(PutRequest) returns (PutResponse);
  rpc Get(GetRequest) returns (GetResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  rpc List(ListRequest) returns (stream ListResponse);

  // Index Operations
  rpc Query(QueryRequest) returns (stream QueryResponse);
  rpc ScanIndex(ScanRequest) returns (stream ScanResponse);

  // Cluster Management
  rpc ClusterStatus(ClusterStatusRequest) returns (ClusterStatusResponse);
  rpc GetLeader(GetLeaderRequest) returns (GetLeaderResponse);

  // Time-Series Operations
  rpc Aggregate(AggregateRequest) returns (AggregateResponse);
  rpc TimeSeriesQuery(TimeSeriesQueryRequest) returns (stream TimeSeriesQueryResponse);

  // Batch Operations
  rpc BatchPut(BatchPutRequest) returns (BatchPutResponse);
  rpc BatchGet(BatchGetRequest) returns (BatchGetResponse);
  rpc BatchDelete(BatchDeleteRequest) returns (BatchDeleteResponse);

  // Transaction Operations
  rpc Transaction(TransactionRequest) returns (TransactionResponse);

  // Health and Diagnostics
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

### Server Connection

#### Unix Socket (Default)

Unix domain sockets are the default and recommended transport for local communication:

```
unix:///tmp/quicbaas.sock          (development)
unix:///var/run/quicbaas/quicbaas.sock  (production)
```

Unix sockets provide:
- Lower latency than TCP (no network stack overhead)
- No port management required
- Enhanced security through filesystem permissions
- Automatic detection from bare paths ending in `.sock`
- **Peer credentials authentication** via SO_PEERCRED (kernel-verified UID/GID/PID)

##### Peer Credentials Authentication

QuicBaaS extracts peer credentials from Unix socket connections using the Linux `SO_PEERCRED` socket option. These credentials are kernel-verified and cannot be spoofed by the connecting process.

**Types:**

| Type | Description |
|------|-------------|
| `PeerCredentials` | Contains PID, UID, and GID of the connecting process |
| `PeerCredentialsAuthInfo` | Implements gRPC `credentials.AuthInfo` |
| `UnixTransportCredentials` | Implements gRPC `credentials.TransportCredentials` |

**Verifiers:**

| Verifier | Description |
|----------|-------------|
| `AllowAllVerifier` | Allows all credentials (testing/open environments) |
| `RootOnlyVerifier` | Only allows root (UID 0) |
| `UIDVerifier` | Allows specific UIDs (with root bypass) |
| `GIDVerifier` | Allows specific GIDs (with root bypass) |
| `ChainVerifier` | Chains multiple verifiers (all must pass) |

**Server-side usage:**

```go
import grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"

// Create transport credentials
creds := grpcapi.NewUnixTransportCredentials()

// Create a UID-based verifier (allow UIDs 1000 and 1001)
verifier := grpcapi.NewUIDVerifier(1000, 1001)

// Create interceptors
unaryInterceptor := grpcapi.UnaryPeerCredentialsInterceptor(verifier, logger)
streamInterceptor := grpcapi.StreamPeerCredentialsInterceptor(verifier, logger)

// Create gRPC server with credentials
server := grpc.NewServer(
    grpc.Creds(creds),
    grpc.UnaryInterceptor(unaryInterceptor),
    grpc.StreamInterceptor(streamInterceptor),
)
```

**Extracting credentials in handlers:**

```go
func (s *Server) SomeMethod(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    creds := grpcapi.PeerCredentialsFromContext(ctx)
    if creds != nil {
        log.Printf("Request from PID %d, UID %d, GID %d", creds.PID, creds.UID, creds.GID)
        if creds.IsRoot() {
            // Handle root user
        }
    }
    // ...
}
```

**Platform support:**
- **Linux**: Full SO_PEERCRED support
- **macOS/Windows**: Stub implementation (returns error for credential extraction)

#### TCP

TCP listeners are optional and must be explicitly enabled:

```
localhost:50051    (insecure)
localhost:50052    (TLS)
```

Enable TCP with `--enable-tcp` flag or `enable_tcp: true` in configuration.

### Authentication

#### Unix Socket Connection (Recommended)

```go
// Using bare socket path (auto-detected)
conn, err := grpc.Dial("unix:///tmp/quicbaas.sock", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

The client automatically detects unix sockets from:
- `unix://` prefix: `unix:///tmp/quicbaas.sock`
- `unix-abstract://` prefix: `unix-abstract://quicbaas`
- Bare paths ending in `.sock`: `/tmp/quicbaas.sock`

#### Insecure TCP Connection (Development)

```go
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

#### TLS Connection (Production)

```go
creds, err := credentials.NewClientTLSFromFile("ca.crt", "")
if err != nil {
    log.Fatal(err)
}

conn, err := grpc.Dial("localhost:50052",
    grpc.WithTransportCredentials(creds),
)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

#### Mutual TLS (mTLS)

```go
cert, err := tls.LoadX509KeyPair("client.crt", "client.key")
if err != nil {
    log.Fatal(err)
}

caCert, err := os.ReadFile("ca.crt")
if err != nil {
    log.Fatal(err)
}

caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
}

creds := credentials.NewTLS(tlsConfig)
conn, err := grpc.Dial("localhost:50052",
    grpc.WithTransportCredentials(creds),
)
```

### API Reference

#### Put Operation

```go
req := &proto.PutRequest{
    EntityType:  "user",
    Id:          1,
    Data:        []byte(`{"name":"John","email":"john@example.com"}`),
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    Metadata: map[string]string{
        "source": "api",
        "version": "1.0",
    },
}

resp, err := client.Put(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Stored with ID: %d, Version: %d\n", resp.Id, resp.Version)
```

**Request Fields:**
- `entity_type` (string) - Entity category (e.g., "user", "sensor")
- `id` (uint64) - Unique identifier (0 for auto-generated)
- `data` (bytes) - Serialized entity data
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `ttl_seconds` (uint64) - Time-to-live (0 = no expiration)
- `metadata` (map<string,string>) - Optional key-value metadata
- `shard_id` (uint64) - Target shard (0 = auto-assign)

**Response Fields:**
- `id` (uint64) - Assigned or provided identifier
- `version` (uint64) - Entity version number
- `timestamp` (int64) - Server timestamp (Unix nanoseconds)
- `leader_id` (uint64) - Node that handled the write

#### Get Operation

```go
req := &proto.GetRequest{
    EntityType:      "user",
    Id:              1,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: true,
}

resp, err := client.Get(ctx, req)
if err != nil {
    log.Fatal(err)
}

if !resp.Found {
    log.Println("Entity not found")
    return
}

fmt.Printf("Data: %s\n", string(resp.Data))
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `id` (uint64) - Unique identifier
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = auto-detect)
- `include_metadata` (bool) - Return metadata with response

**Response Fields:**
- `id` (uint64) - Unique identifier
- `data` (bytes) - Serialized entity data
- `version` (uint64) - Current entity version
- `timestamp` (int64) - Last modified timestamp (Unix nanoseconds)
- `metadata` (map<string,string>) - Optional metadata
- `found` (bool) - Whether the key exists

#### Delete Operation

```go
req := &proto.DeleteRequest{
    EntityType:  "user",
    Id:          1,
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    Version:     1, // Optional: for optimistic locking
}

resp, err := client.Delete(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Deleted: %v\n", resp.Deleted)
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `id` (uint64) - Unique identifier to delete
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = auto-detect)
- `version` (uint64) - Optional version for optimistic locking (0 = no check)

**Response Fields:**
- `deleted` (bool) - Whether the key existed and was deleted
- `timestamp` (int64) - Deletion timestamp (Unix nanoseconds)

#### List Operation

```go
req := &proto.ListRequest{
    EntityType:      "user",
    PageSize:        100,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: false,
}

stream, err := client.List(ctx, req)
if err != nil {
    log.Fatal(err)
}

for {
    resp, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }

    for _, entity := range resp.Entities {
        fmt.Printf("ID: %d, Data: %s\n", entity.Id, string(entity.Data))
    }

    if resp.NextPageToken == "" {
        break
    }
}
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `page_size` (uint32) - Results per page
- `page_token` (string) - Continuation token from previous response
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = all shards)
- `include_metadata` (bool) - Return metadata with entities

**Response Fields:**
- `entities` ([]Entity) - Batch of results
- `next_page_token` (string) - Token for next page (empty if last)
- `total_count` (uint64) - Total number of entities (if available)

#### ListEntityTypes Operation

Discover all unique entity types stored in the database. In multi-tenant mode, only returns entity types within the tenant's keyspace.

```go
req := &proto.ListEntityTypesRequest{
    PageSize:  100,
    PageToken: "",
}

resp, err := client.ListEntityTypes(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, entityType := range resp.EntityTypes {
    fmt.Printf("Entity Type: %s\n", entityType)
}

if resp.NextPageToken != "" {
    // Fetch more results with resp.NextPageToken
}
```

**Request Fields:**
- `page_size` (uint32) - Maximum results per page (default: 100, max: 1000)
- `page_token` (string) - Continuation token from previous response
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `timeout_ms` (int64) - Operation timeout in milliseconds

**Response Fields:**
- `entity_types` ([]string) - List of unique entity type names
- `next_page_token` (string) - Token for next page (empty if last)
- `total_count` (uint64) - Total number of entity types (if available)

#### Query Operation

```go
req := &proto.QueryRequest{
    EntityType:      "user",
    IndexName:       "email",
    Value:           []byte("john@example.com"),
    PageSize:        50,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: false,
}

stream, err := client.Query(ctx, req)
if err != nil {
    log.Fatal(err)
}

for {
    resp, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }

    for _, entity := range resp.Entities {
        fmt.Printf("Found: ID=%d, Data=%s\n", entity.Id, string(entity.Data))
    }
}
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `index_name` (string) - Name of the index to query
- `value` (bytes) - Exact value to match (for equality queries)
- `range_start` (bytes) - Inclusive start for range queries
- `range_end` (bytes) - Inclusive end for range queries
- `page_size` (uint32) - Number of results
- `page_token` (string) - Continuation token
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `include_metadata` (bool) - Return metadata

**Response Fields:**
- `entities` ([]Entity) - Matching entities
- `next_page_token` (string) - Pagination token
- `total_count` (uint64) - Total matches (if available)

#### Cluster Status

```go
req := &proto.ClusterStatusRequest{
    IncludeMetrics: true,
}

resp, err := client.ClusterStatus(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Cluster ID: %s\n", resp.ClusterId)
for _, node := range resp.Nodes {
    fmt.Printf("Node %d: %s (Leader: %v, Health: %s)\n",
        node.NodeId, node.Address, node.IsLeader, node.HealthStatus)
}
```

**Response Fields:**
- `cluster_id` (string) - Unique cluster identifier
- `nodes` ([]NodeInfo) - Information about all nodes
- `shards` ([]ShardInfo) - Information about all shards
- `timestamp` (int64) - Status collection time (Unix nanoseconds)

#### Batch Put

```go
req := &proto.BatchPutRequest{
    Requests: []*proto.PutRequest{
        {
            EntityType: "user",
            Id:         1,
            Data:       []byte(`{"name":"John"}`),
        },
        {
            EntityType: "user",
            Id:         2,
            Data:       []byte(`{"name":"Jane"}`),
        },
    },
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
}

resp, err := client.BatchPut(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, r := range resp.Responses {
    fmt.Printf("Stored ID %d, Version %d\n", r.Id, r.Version)
}
```

#### Health Check

```go
req := &proto.HealthRequest{
    IncludeDetails: true,
}

resp, err := client.Health(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Status: %s, Uptime: %d seconds\n",
    resp.Status, resp.UptimeSeconds)
```

#### Transaction

Atomic compare-and-swap operations with ACID guarantees.

```go
// Example: Optimistic locking update
req := &proto.TransactionRequest{
    // Compare conditions: all must succeed
    Compare: []*proto.CompareCondition{
        {
            EntityType: "user",
            Id:         1,
            Target:     proto.CompareTarget_COMPARE_TARGET_VERSION,
            Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
            Version:    5, // Expected current version
        },
    },
    // Success operations: execute if all comparisons succeed
    Success: []*proto.TransactionOp{
        {
            OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
            EntityType: "user",
            Id:         1,
            Data:       []byte(`{"name":"John Updated","email":"john@example.com"}`),
        },
    },
    // Failure operations: execute if any comparison fails
    Failure: []*proto.TransactionOp{
        {
            OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_GET,
            EntityType: "user",
            Id:         1,
        },
    },
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
}

resp, err := client.Transaction(ctx, req)
if err != nil {
    log.Fatal(err)
}

if resp.Succeeded {
    fmt.Println("Update successful")
} else {
    fmt.Println("Version conflict detected")
    // Check failure operations results
    for _, opResp := range resp.Responses {
        if opResp.OpType == proto.TransactionOpType_TRANSACTION_OP_TYPE_GET {
            fmt.Printf("Current version: %d\n", opResp.Version)
        }
    }
}
```

**Request Fields:**
- `compare` ([]CompareCondition) - Predicates to evaluate (all must succeed)
- `success` ([]TransactionOp) - Operations to execute if all comparisons succeed
- `failure` ([]TransactionOp) - Operations to execute if any comparison fails
- `consistency` (ConsistencyLevel) - Transaction consistency level
- `shard_id` (uint64) - Target shard (must be same for all operations)
- `timeout_ms` (int64) - Maximum time to wait (default: 30000ms)

**Compare Targets:**
- `VERSION` - Compare entity version number
- `VALUE` - Compare entity data value
- `EXISTS` - Check if entity exists
- `CREATE` - Check entity creation timestamp
- `MOD` - Check entity modification timestamp

**Compare Operators:**
- `EQUAL` (==)
- `NOT_EQUAL` (!=)
- `GREATER` (>)
- `LESS` (<)
- `GREATER_EQUAL` (>=)
- `LESS_EQUAL` (<=)

**Transaction Operations:**
- `PUT` - Create or update entity
- `GET` - Retrieve entity
- `DELETE` - Delete entity

**Response Fields:**
- `succeeded` (bool) - Whether all comparisons succeeded
- `responses` ([]TransactionOpResponse) - Results of executed operations
- `timestamp` (int64) - Transaction completion time
- `message` (string) - Additional information
- `revision` (uint64) - MVCC revision

**Common Patterns:**

Conditional Put (only if exists):
```go
Compare: []*proto.CompareCondition{
    {
        EntityType: "counter",
        Id:         1,
        Target:     proto.CompareTarget_COMPARE_TARGET_EXISTS,
        Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
        Version:    1, // 1 = exists, 0 = doesn't exist
    },
}
```

Conditional Delete (only if value matches):
```go
Compare: []*proto.CompareCondition{
    {
        EntityType: "session",
        Id:         123,
        Target:     proto.CompareTarget_COMPARE_TARGET_VALUE,
        Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
        Value:      []byte(`{"status":"expired"}`),
    },
}
```

Multi-operation transaction:
```go
Success: []*proto.TransactionOp{
    {
        OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
        EntityType: "account",
        Id:         1,
        Data:       []byte(`{"balance":900}`),
    },
    {
        OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
        EntityType: "account",
        Id:         2,
        Data:       []byte(`{"balance":1100}`),
    },
}
```

### Complete Example

```go
package main

import (
    "context"
    "log"
    "time"

    "google.golang.org/grpc"
    proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func main() {
    // Connect via Unix socket (recommended for local connections)
    conn, err := grpc.Dial("unix:///tmp/quicbaas.sock", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    client := proto.NewQuicBaaSClient(conn)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    // Put operation
    putReq := &proto.PutRequest{
        EntityType:  "user",
        Id:          1,
        Data:        []byte(`{"name":"John","email":"john@example.com"}`),
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    }

    putResp, err := client.Put(ctx, putReq)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Put: ID=%d, Version=%d", putResp.Id, putResp.Version)

    // Get operation
    getReq := &proto.GetRequest{
        EntityType:  "user",
        Id:          1,
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    }

    getResp, err := client.Get(ctx, getReq)
    if err != nil {
        log.Fatal(err)
    }

    if getResp.Found {
        log.Printf("Get: %s", string(getResp.Data))
    }

    // Delete operation
    deleteReq := &proto.DeleteRequest{
        EntityType:  "user",
        Id:          1,
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    }

    deleteResp, err := client.Delete(ctx, deleteReq)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Delete: %v", deleteResp.Deleted)
}
```

### Error Handling

gRPC errors use the standard gRPC status codes. An independent client
(`connectrpc.com/connect` in gRPC mode) reads OK, Unauthenticated,
PermissionDenied and Unimplemented from the server, and an OK trailer at the end
of a server stream (`pkg/api/grpc/interop_connect_test.go`); other codes are so
far exercised by the Go client only:

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

resp, err := client.Get(ctx, req)
if err != nil {
    st, ok := status.FromError(err)
    if !ok {
        log.Fatal("unknown error")
    }

    switch st.Code() {
    case codes.NotFound:
        log.Println("Entity not found")
    case codes.Unavailable:
        log.Println("Service unavailable")
    case codes.DeadlineExceeded:
        log.Println("Request timeout")
    default:
        log.Printf("Error: %v", st.Message())
    }
}
```

#### gRPC Status Codes

Every `pkg/errors` code maps to one gRPC status. The status message carries
the lower-case code as `op: code: message` (see [Error Handling](#error-handling)),
so codes that share a status, such as `rate_limited` and `resource_exhausted`,
stay distinguishable. The IAM, MFA, PEP and federation services answer
`conflict` with `FAILED_PRECONDITION`. An error that is not a `pkg/errors`
error is `INTERNAL`.

| Status | `pkg/errors` codes |
|--------|--------------------|
| `UNKNOWN` | `unknown` |
| `INVALID_ARGUMENT` | `invalid_input`, `validation`, `out_of_range`, `missing_tenant_context`, `missing_user_context`, `missing_justification` |
| `UNAUTHENTICATED` | `unauthorized` |
| `PERMISSION_DENIED` | `forbidden`, `not_allowed`, `cross_tenant_access`, `system_keyspace_violation` |
| `NOT_FOUND` | `not_found` |
| `ALREADY_EXISTS` | `already_exists`, `conflict` |
| `FAILED_PRECONDITION` | `precondition_failed` |
| `ABORTED` | `aborted` |
| `CANCELLED` | `canceled` |
| `DEADLINE_EXCEEDED` | `timeout`, `deadline_exceeded` |
| `RESOURCE_EXHAUSTED` | `rate_limited`, `resource_exhausted` |
| `UNAVAILABLE` | `unavailable`, `leader_not_available`, `cluster_not_ready`, `circuit_open` |
| `UNIMPLEMENTED` | `unimplemented`, `not_implemented` |
| `DATA_LOSS` | `data_loss` |
| `INTERNAL` | `internal`, `storage`, `raft`, `quorum_lost`, `split_brain`, `replication`, `snapshot`, `configuration`, `encryption`, `decryption` |

Clients read the code back from the status. The root gRPC client
(`pkg/api/grpc`) returns the `pkg/errors` code, reading `rate_limited` from a
`RESOURCE_EXHAUSTED` status message. The Go SDK has fewer codes: it reads
`OUT_OF_RANGE` as `invalid_input` and `DATA_LOSS` as `internal`.

### Performance

#### Connection Pooling

```go
conn, err := grpc.Dial("localhost:50051",
    grpc.WithInsecure(),
    grpc.WithDefaultCallOptions(
        grpc.MaxCallRecvMsgSize(10*1024*1024),
        grpc.MaxCallSendMsgSize(10*1024*1024),
    ),
)
```

#### Compression

```go
import "google.golang.org/grpc/encoding/gzip"

resp, err := client.Put(ctx, req,
    grpc.UseCompressor(gzip.Name),
)
```

#### Streaming

```go
stream, err := client.List(ctx, &proto.ListRequest{
    EntityType: "user",
    PageSize:   1000,
})
```

#### Keepalive

```go
conn, err := grpc.Dial("localhost:50051",
    grpc.WithInsecure(),
    grpc.WithKeepaliveParams(keepalive.ClientParameters{
        Time:                10 * time.Second,
        Timeout:             3 * time.Second,
        PermitWithoutStream: true,
    }),
)
```

### Ollama Service

gRPC service for Ollama embedding generation and model management.

#### Protocol Definition

```
api/proto/ollama.proto
```

#### Service Overview

```protobuf
service OllamaService {
  // Health returns the health status of the Ollama connection.
  rpc Health(OllamaHealthRequest) returns (OllamaHealthResponse);

  // ListModels returns the list of available Ollama models.
  rpc ListModels(OllamaListModelsRequest) returns (OllamaListModelsResponse);

  // GetModelInfo returns detailed information about a specific model.
  rpc GetModelInfo(OllamaGetModelInfoRequest) returns (OllamaGetModelInfoResponse);

  // Embed generates an embedding for a single text using the default model.
  rpc Embed(OllamaEmbedRequest) returns (OllamaEmbedResponse);

  // EmbedWithModel generates an embedding for a single text using a specific model.
  rpc EmbedWithModel(OllamaEmbedWithModelRequest) returns (OllamaEmbedResponse);

  // EmbedBatch generates embeddings for multiple texts.
  rpc EmbedBatch(OllamaEmbedBatchRequest) returns (OllamaEmbedBatchResponse);
}
```

#### Health Check

```go
req := &proto.OllamaHealthRequest{
    FullCheck: true,
}

resp, err := client.Health(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Available: %v, Version: %s\n", resp.Available, resp.Version)
```

#### List Models

```go
req := &proto.OllamaListModelsRequest{
    NamePrefix: "nomic", // Optional filter
}

resp, err := client.ListModels(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, model := range resp.Models {
    fmt.Printf("Model: %s, Size: %d bytes\n", model.Name, model.Size)
}
```

#### Generate Embedding

```go
req := &proto.OllamaEmbedRequest{
    Text: "What is QuicBaaS?",
}

resp, err := client.Embed(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Model: %s, Dimensions: %d\n", resp.Model, resp.Dimensions)
fmt.Printf("Embedding: %v\n", resp.Embedding[:5]) // First 5 values
```

#### Batch Embedding

```go
req := &proto.OllamaEmbedBatchRequest{
    Texts: []string{
        "First document",
        "Second document",
        "Third document",
    },
    Model: "nomic-embed-text", // Optional
}

resp, err := client.EmbedBatch(ctx, req)
if err != nil {
    log.Fatal(err)
}

for i, emb := range resp.Embeddings {
    fmt.Printf("Document %d: %d dimensions\n", i, len(emb.Values))
}
```

### RAG Service

gRPC service for RAG context building operations.

#### Protocol Definition

```
api/proto/rag.proto
```

#### Service Overview

```protobuf
service RAGService {
  // Query performs an all-in-one RAG query: embed, search, and build context.
  rpc Query(RAGQueryRequest) returns (RAGQueryResponse);

  // BuildContext assembles context from user-provided search results.
  rpc BuildContext(RAGBuildContextRequest) returns (RAGBuildContextResponse);
}
```

#### RAG Query

```go
req := &proto.RAGQueryRequest{
    Query:           "How does error handling work?",
    Namespace:       "codebase",
    TopK:            10,
    MaxTokens:       4000,
    MinScore:        0.7,
    Template:        proto.RAG_TEMPLATE_MARKDOWN,
    IncludeCitations: true,
}

resp, err := client.Query(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Context (%d tokens):\n%s\n", resp.Tokens, resp.Context)
fmt.Printf("Results: %d, Truncated: %v\n", resp.ResultCount, resp.Truncated)

for _, citation := range resp.Citations {
    fmt.Printf("[%d] %s:%d-%d (score: %.2f)\n",
        citation.Index, citation.FilePath,
        citation.StartLine, citation.EndLine, citation.Score)
}
```

#### Build Context

```go
req := &proto.RAGBuildContextRequest{
    Results: []*proto.RAGResultInput{
        {
            Content: "func main() {\n    fmt.Println(\"Hello\")\n}",
            Score:   0.92,
            Metadata: map[string]string{
                "file_path":  "main.go",
                "start_line": "1",
                "end_line":   "10",
            },
        },
        {
            Content: "type Config struct {\n    Name string\n}",
            Score:   0.85,
            Metadata: map[string]string{
                "file_path":  "config.go",
                "start_line": "5",
                "end_line":   "8",
            },
        },
    },
    MaxTokens:          2000,
    Template:           proto.RAG_TEMPLATE_MARKDOWN,
    IncludeCitations:   true,
    TruncationStrategy: proto.RAG_TRUNCATION_SMART,
}

resp, err := client.BuildContext(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Context:\n%s\n", resp.Context)
fmt.Printf("Tokens: %d, Results: %d\n", resp.Tokens, resp.ResultCount)
```

#### Template Options

| Template | Description |
|----------|-------------|
| `RAG_TEMPLATE_MARKDOWN` | Markdown-formatted context with code blocks |
| `RAG_TEMPLATE_JSON` | JSON-structured context |
| `RAG_TEMPLATE_XML` | XML-formatted context |
| `RAG_TEMPLATE_PLAIN` | Plain text without formatting |

#### Truncation Strategies

| Strategy | Description |
|----------|-------------|
| `RAG_TRUNCATION_SMART` | Preserve complete chunks, drop lowest-scored |
| `RAG_TRUNCATION_HARD` | Hard cut at token limit |
| `RAG_TRUNCATION_NONE` | No truncation, may exceed limit |

### See Also

- CLI Documentation - Command-line interface
- REST API - HTTP/JSON interface
- [Architecture](architecture.md) - System design
- [RAG MCP Tools](../rag/api.md) - RAG MCP tool reference


---

## QUIC API

> **No component in this repository serves HTTP/3 today.** There is no QUIC
> server, no `pkg/api/quic` package, no `--quic-port` on `quicbaasd` and no
> `http3.Server` anywhere under `pkg/`. See the caveat in
> [architecture.md](architecture.md#quic-sdk-client-only).

QUIC exists only on the client side, as the SDK transport
`sdk/go/transport/quic`. It is not a separate protocol: it has no binary wire
format, no opcodes and no status-code table of its own. It speaks the REST
dialect documented above over HTTP/3.

### SDK transport

`quictransport.New` builds an `*http.Client` whose `RoundTripper` is quic-go's
`http3.Transport` and hands it to the REST transport via
`rest.WithHTTPClient` (`sdk/go/transport/quic/transport.go`). Every request is
therefore built, sent and decoded exactly as the REST transport does it: the
same endpoint mapping, the same JSON bodies, the same error decoding.

```go
import (
    "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
    quictransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

cfg := &transport.Config{
    Address:   "https://quicbaas.example.com:8443",
    TLSConfig: tlsConfig, // optional; see TLS below
    Timeout:   10 * time.Second,
    AuthToken: "eyJhbGciOiJIUzI1NiIs...", // optional
}

t, err := quictransport.New(cfg,
    quictransport.WithAuthToken("eyJhbGciOiJIUzI1NiIs..."), // overrides cfg.AuthToken
)
if err != nil {
    log.Fatal(err)
}
defer t.Close() // closes the HTTP/3 connections, then the REST transport

resp, err := t.Request(ctx, service, method, req)
```

`WithAuthToken` is the transport's only option. Every other setting (timeout,
TLS, retries, auth token) travels on `transport.Config` and is honoured exactly
as the REST transport honours it. `quictransport.Factory` implements
`transport.Factory` for callers that select a transport by name.

### TLS

QUIC mandates TLS 1.3 (RFC 9001 §4.2). The transport clones a caller-supplied
`cfg.TLSConfig`, raises its `MinVersion` to TLS 1.3 when lower, and sets
`NextProtos` to `h3` when empty. Client certificates for mTLS, including
hardware-backed `crypto.Signer` certificates, are preserved. With no
`cfg.TLSConfig` it uses TLS 1.3 with ALPN `h3` and the system roots.

### See Also

- REST API - the dialect this transport carries
- [Architecture](architecture.md) - System design and the HTTP/3 caveat
