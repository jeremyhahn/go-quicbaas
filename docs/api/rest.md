# REST API

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

- [gRPC API](../grpc/README.md) - Binary protocol
- [CLI Documentation](../cli/README.md) - Command-line interface
- [Architecture](../architecture/README.md) - System design
- [RAG MCP Tools](../rag/mcp-tools.md) - RAG MCP tool reference
- [Backup and Recovery](../operations/backup-recovery.md) - Backup operations guide
