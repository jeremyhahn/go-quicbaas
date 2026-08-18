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
  "error": {
    "code": "NOT_FOUND",
    "message": "Entity not found: user/1"
  }
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

All errors follow a consistent format:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message",
    "details": {
      "field": "additional context"
    }
  }
}
```

Common error codes:

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `NOT_FOUND` | 404 | Entity not found |
| `INVALID_INPUT` | 400 | Invalid request parameters |
| `UNAUTHORIZED` | 401 | Authentication required |
| `FORBIDDEN` | 403 | Permission denied |
| `CONFLICT` | 409 | Version conflict (optimistic locking) |
| `UNAVAILABLE` | 503 | Service temporarily unavailable |
| `TIMEOUT` | 504 | Request timeout |
| `INTERNAL` | 500 | Internal server error |

## Rate Limiting

Headers:
```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1696550400
```

When rate limited (HTTP 429):
```json
{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded. Try again in 60 seconds."
  }
}
```

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

gRPC errors use standard status codes:

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

Common error codes:

| Code | Description |
|------|-------------|
| `NOT_FOUND` | Entity not found |
| `INVALID_ARGUMENT` | Invalid request parameters |
| `UNAVAILABLE` | Service temporarily unavailable |
| `DEADLINE_EXCEEDED` | Request timeout |
| `INTERNAL` | Internal server error |
| `UNAUTHENTICATED` | Authentication required |
| `PERMISSION_DENIED` | Permission denied |

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

High-performance binary protocol over QUIC transport for QuicBaaS.

### Overview

The QUIC API provides a high-performance binary protocol built on QUIC transport (RFC 9000). It offers:

- **Low latency**: 0-RTT connection establishment for returning clients
- **Stream multiplexing**: Concurrent operations without head-of-line blocking
- **Connection migration**: Seamless handling of network changes (ideal for mobile clients)
- **Built-in TLS 1.3**: Mandatory encryption with modern security
- **Efficient binary encoding**: JSON payloads with compact wire format

### Default Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Listen Address | `0.0.0.0:4433` | UDP port for QUIC |
| Max Streams | 1000 | Maximum concurrent streams per connection |
| Max Idle Timeout | 30s | Connection idle timeout |
| Keep-Alive Period | 15s | Keep-alive ping interval |
| Handshake Timeout | 5s | TLS handshake timeout |
| Max Payload Size | 16MB | Maximum request/response payload |

### Wire Protocol

#### Request Format

```
[opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M]
```

| Field | Size | Description |
|-------|------|-------------|
| opcode | 1 byte | Operation code (see Operations) |
| tokenLen | 2 bytes | Authentication token length (big-endian) |
| token | N bytes | JWT or API token (max 8KB) |
| payloadLen | 4 bytes | Payload length (big-endian) |
| payload | M bytes | JSON-encoded request data |

#### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| status | 1 byte | Status code (see Status Codes) |
| payloadLen | 4 bytes | Payload length (big-endian) |
| payload | N bytes | JSON-encoded response data |

### Status Codes

| Code | Value | Description |
|------|-------|-------------|
| OK | 0x00 | Success |
| INVALID_REQUEST | 0x01 | Malformed request |
| NOT_FOUND | 0x02 | Entity not found |
| UNAUTHORIZED | 0x03 | Authentication required |
| INTERNAL_ERROR | 0x04 | Server error |
| TIMEOUT | 0x05 | Request timeout |
| ALREADY_EXISTS | 0x06 | Entity already exists |
| PERMISSION_DENIED | 0x07 | Authorization failed |
| RESOURCE_EXHAUSTED | 0x08 | Rate limit exceeded |
| INVALID_ARGUMENT | 0x09 | Invalid parameters |
| UNAVAILABLE | 0x0A | Service unavailable |

### Authentication

Include a JWT token in the request token field. Tokens are obtained via the Login operation.

```go
req := &quic.Request{
    Opcode:  quic.OpGet,
    Token:   "eyJhbGciOiJIUzI1NiIs...",
    Payload: payload,
}
```

Public operations that do not require authentication:
- `OpLogin` (0x60)
- `OpHealth` (0x90)
- `OpLiveness` (0x91)
- `OpReadiness` (0x92)

### Operations

#### KV Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Put | 0x01 | Store key-value pair |
| Get | 0x02 | Retrieve value by key |
| Delete | 0x03 | Remove key-value pair |
| List | 0x04 | List entities by type |

#### Batch Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| BatchPut | 0x10 | Store multiple entities |
| BatchGet | 0x11 | Retrieve multiple entities |
| BatchDelete | 0x12 | Delete multiple entities |

#### Transaction Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Transaction | 0x20 | Atomic compare-and-swap |

#### Query Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Query | 0x30 | Query by index |
| ScanIndex | 0x31 | Scan index range |

#### Cluster Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| ClusterStatus | 0x40 | Get cluster status |
| GetLeader | 0x41 | Get shard leader |
| AddNode | 0x42 | Add node to shard |
| RemoveNode | 0x43 | Remove node from shard |
| CreateShard | 0x44 | Create new shard |
| RebalanceCluster | 0x45 | Initiate rebalancing |
| ListNodes | 0x46 | List cluster nodes |

#### Health Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Health | 0x90 | Full health check |
| Liveness | 0x91 | Kubernetes liveness probe |
| Readiness | 0x92 | Kubernetes readiness probe |

#### Auth Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Login | 0x60 | Authenticate user |
| Logout | 0x61 | Invalidate session |
| RefreshToken | 0x62 | Refresh JWT token |
| ValidateToken | 0x63 | Validate token |

#### KMS Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| KMSGenerateKey | 0xD0 | Generate new key |
| KMSImportKey | 0xD1 | Import external key |
| KMSGetKey | 0xD2 | Get key metadata |
| KMSListKeys | 0xD3 | List all keys |
| KMSRotateKey | 0xD4 | Rotate key version |
| KMSDeleteKey | 0xD5 | Schedule key deletion |
| KMSEncrypt | 0xD6 | Encrypt data |
| KMSDecrypt | 0xD7 | Decrypt data |

#### Secrets Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| SecretsStore | 0xE0 | Store secret |
| SecretsGet | 0xE1 | Retrieve secret |
| SecretsList | 0xE2 | List secrets |
| SecretsDelete | 0xE3 | Delete secret |
| SecretsListVersions | 0xE4 | List secret versions |
| SecretsRollback | 0xE5 | Rollback to version |
| SecretsRotate | 0xE6 | Rotate secret value |

#### Seal Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| SealStatus | 0xC0 | Get seal status |
| SealInit | 0xC1 | Initialize seal |
| Unseal | 0xC2 | Provide unseal key |
| Seal | 0xC3 | Re-seal database |
| SealRekey | 0xC4 | Regenerate key shares |

#### Vector Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| VectorCreateNamespace | 0x16 | Create vector namespace |
| VectorDeleteNamespace | 0x17 | Delete namespace |
| VectorListNamespaces | 0x18 | List namespaces |
| VectorUpsert | 0x1A | Insert/update vector |
| VectorGet | 0x1B | Get vector by ID |
| VectorDelete | 0x1C | Delete vector |
| VectorSearchSimilar | 0x1E | Similarity search |
| VectorHybridSearch | 0x21 | Hybrid search |

### Server Configuration

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"

// TLS configuration (required for QUIC)
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
}

// Server configuration
config := &quic.Config{
    ListenAddr:             "0.0.0.0:4433",
    TLSConfig:              tlsConfig,
    MaxStreams:             1000,
    MaxIdleTimeout:         30 * time.Second,
    MaxIncomingStreams:     1000,
    KeepAlivePeriod:        15 * time.Second,
    HandshakeIdleTimeout:   5 * time.Second,
    MaxReceiveBufferSize:   1 * 1024 * 1024,
    InitialStreamReceiveWindow: 512 * 1024,
    MaxStreamReceiveWindow: 6 * 1024 * 1024,
    EnableMetrics:          true,
}

// Create server with authorization
server, err := quic.NewServer(service, config, logger,
    quic.WithAuthManager(authManager),
    quic.WithAuthorizationService(authzService),
)
if err != nil {
    log.Fatal(err)
}

// Start server
if err := server.Start(); err != nil {
    log.Fatal(err)
}
defer server.Stop()
```

### Client Usage

#### Direct Client

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"

// Create client with TLS
client, err := quic.NewClient("localhost:4433",
    quic.WithTLSConfig(tlsConfig),
    quic.WithTimeout(10*time.Second),
    quic.WithMaxIdleTimeout(30*time.Second),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Put operation
putResp, err := client.Put(ctx, &service.PutRequest{
    EntityType:  "user",
    Id:          1,
    Data:        []byte(`{"name":"John","email":"john@example.com"}`),
    Consistency: "quorum",
})

// Get operation
getResp, err := client.Get(ctx, &service.GetRequest{
    EntityType: "user",
    Id:         1,
})

// KMS operations
keyResp, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
    Name:      "my-key",
    Algorithm: "AES256",
    Purpose:   "ENCRYPT_DECRYPT",
})

// Secrets operations
secretResp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
    Path:  "app/database/password",
    Value: []byte("secret-password"),
})
```

#### SDK Transport

```go
import (
    "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
    quictransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

// Create QUIC transport
cfg := &transport.Config{
    Address:   "localhost:4433",
    TLSConfig: tlsConfig,
    Timeout:   10 * time.Second,
}

t, err := quictransport.New(cfg,
    quictransport.WithHandshakeTimeout(10*time.Second),
    quictransport.WithMaxIdleTimeout(30*time.Second),
    quictransport.WithKeepAlivePeriod(15*time.Second),
    quictransport.WithAuthToken("eyJhbGciOiJIUzI1NiIs..."),
    quictransport.WithEnable0RTT(true),
)
if err != nil {
    log.Fatal(err)
}
defer t.Close()

// Send request
resp, err := t.Request(ctx, "QuicBaaS", "Get", &GetRequest{
    EntityType: "user",
    Id:         1,
})
```

### TLS Configuration

QUIC requires TLS 1.3. Configure certificates for production:

```go
// Load server certificate
cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
if err != nil {
    log.Fatal(err)
}

// Server TLS config
serverTLS := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
    ClientAuth:   tls.RequireAndVerifyClientCert, // mTLS
    ClientCAs:    caCertPool,
}

// Client TLS config
clientTLS := &tls.Config{
    RootCAs:      caCertPool,
    Certificates: []tls.Certificate{clientCert}, // mTLS
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
    ServerName:   "quicbaas.example.com",
}
```

For development/testing only:

```go
client, err := quic.NewClient("localhost:4433",
    quic.WithInsecure(),
)
```

### Error Handling

```go
resp, err := client.Get(ctx, req)
if err != nil {
    var qbErr *errors.QBError
    if errors.As(err, &qbErr) {
        switch qbErr.Code {
        case errors.ErrNotFound:
            // Handle not found
        case errors.ErrUnauthorized:
            // Handle auth error
        case errors.ErrTimeout:
            // Handle timeout
        default:
            // Handle other errors
        }
    }
}
```

### Performance Tuning

#### Connection Settings

```go
config := &quic.Config{
    // Increase for high-throughput workloads
    MaxStreams:             2000,
    MaxIncomingStreams:     2000,

    // Increase buffer sizes for large payloads
    MaxReceiveBufferSize:   4 * 1024 * 1024,  // 4MB
    InitialStreamReceiveWindow: 1 * 1024 * 1024, // 1MB
    MaxStreamReceiveWindow: 16 * 1024 * 1024,    // 16MB

    // Adjust timeouts for network conditions
    MaxIdleTimeout:         60 * time.Second,
    KeepAlivePeriod:        20 * time.Second,
}
```

#### 0-RTT Connection Resumption

Enable 0-RTT for reduced latency on reconnection:

```go
t, err := quictransport.New(cfg,
    quictransport.WithEnable0RTT(true),
)
```

Note: 0-RTT data may be replayed by an attacker. Only use for idempotent operations.

### Metrics

The server exposes metrics via `GetMetrics()`:

```go
metrics := server.GetMetrics()
// {
//   "active_connections": 150,
//   "active_streams": 1200,
//   "total_requests": 5000000,
//   "total_errors": 1234,
//   "running": true
// }
```

### See Also

- gRPC API - High-performance binary protocol (alternative)
- REST API - HTTP/JSON interface
- [Architecture](architecture.md) - System design
