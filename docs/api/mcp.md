# MCP API

Model Context Protocol (MCP) interface for AI agent integration.

> **Breaking Change (v1.1.0):** Token parameters in request body (`_token`, `accessToken`) are no longer supported. Use `Authorization: Bearer <token>` header. See [MCP Authorization](mcp-authorization.md#migration-guide).

## Overview

MCP enables AI assistants like Claude to interact with QuicBaaS using JSON-RPC 2.0 protocol. It exposes database operations as tools and live data as resources.

**Key Features:**
- 46 database operations exposed as MCP tools
- Live data views as MCP resources
- Stdio and HTTP transports
- Native Claude Desktop integration
- Vector similarity search for RAG workflows
- OAuth 2.1 authorization with PKCE

## Authorization

MCP requests require OAuth 2.1 authentication. See [MCP Authorization](mcp-authorization.md) for complete details.

**Quick Start:**
1. Discover authorization server: `GET /.well-known/oauth-authorization-server`
2. Obtain access token via OAuth 2.1 with PKCE (S256 mandatory)
3. Include Bearer token in all requests: `Authorization: Bearer <token>`

**HTTP Transport Example:**
```bash
curl -X POST http://localhost:8888/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiI..." \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

**Public Tools (no auth required):**
- `quicbaas_health` - Health check
- `quicbaas_login` - Authenticate
- `quicbaas_version` - Version info
- `quicbaas_liveness` - Liveness probe

## Architecture

```
AI Assistant (Claude)
       |
       | JSON-RPC 2.0
       |
    MCP Server
       |
       | service.QuicBaaS
       |
    QuicBaaS Core
```

## Transports

### Stdio Transport

For integration with AI assistants:

```bash
# Start QuicBaaS with MCP on stdio
./quicbaas --mcp-transport=stdio
```

The stdio transport communicates via stdin/stdout and is designed for Claude Desktop and similar tools.

### HTTP Transport

For testing and debugging:

```bash
# Start QuicBaaS with MCP on HTTP
./quicbaas --mcp-transport=http --mcp-addr=127.0.0.1:8888

# Test with curl (authenticated request)
curl -X POST http://127.0.0.1:8888/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Test public endpoint (no auth required)
curl -X POST http://127.0.0.1:8888/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"quicbaas_health"}}'
```

## JSON-RPC Protocol

### Request Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_put",
    "arguments": {
      "entityType": "user",
      "id": 123,
      "data": "eyJuYW1lIjoiSm9obiJ9"
    }
  }
}
```

### Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": 123,
    "version": 1,
    "timestamp": 1700000000
  }
}
```

### Error Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32603,
    "message": "Internal error",
    "data": "entity not found"
  }
}
```

## MCP Methods

| Method | Description |
|--------|-------------|
| `initialize` | Initialize MCP connection and exchange capabilities |
| `tools/list` | List all available tools |
| `tools/call` | Execute a specific tool |
| `resources/list` | List all available resources |
| `resources/read` | Read a specific resource |
| `ping` | Health check |

## Available Tools

### KV Operations

- `quicbaas_put` - Store a key-value pair
- `quicbaas_get` - Retrieve a value
- `quicbaas_delete` - Delete a key
- `quicbaas_list` - List entities with pagination

### Batch Operations

- `quicbaas_batch_put` - Atomic multi-put
- `quicbaas_batch_get` - Multi-get operation
- `quicbaas_batch_delete` - Atomic multi-delete

### Transaction Operations

- `quicbaas_transaction` - Atomic compare-and-swap transaction

### Query Operations

- `quicbaas_query` - Query using secondary indexes
- `quicbaas_scan_index` - Full index scan

### Cluster Operations

- `quicbaas_cluster_status` - Get cluster status
- `quicbaas_get_leader` - Get shard leader
- `quicbaas_add_node` - Add node to shard
- `quicbaas_remove_node` - Remove node from shard
- `quicbaas_create_shard` - Create new shard
- `quicbaas_rebalance_cluster` - Rebalance data

### Backup Operations

- `quicbaas_backup_create` - Create backup
- `quicbaas_backup_list` - List backups
- `quicbaas_backup_restore` - Restore from backup
- `quicbaas_backup_delete` - Delete backup
- `quicbaas_backup_verify` - Verify backup integrity

### Authentication and RBAC

- `quicbaas_login` - Authenticate and obtain tokens
- `quicbaas_logout` - Invalidate session
- `quicbaas_user_create` - Create user
- `quicbaas_user_delete` - Delete user
- `quicbaas_user_list` - List users
- `quicbaas_role_create` - Create role
- `quicbaas_role_delete` - Delete role
- `quicbaas_role_list` - List roles
- `quicbaas_role_assign` - Assign role to user
- `quicbaas_role_revoke` - Revoke role from user

### Operations Management

- `quicbaas_migrate_data` - Migrate data between shards
- `quicbaas_migration_progress` - Get migration progress
- `quicbaas_replace_node` - Replace failed node
- `quicbaas_upgrade_node` - Perform rolling upgrade

### Ollama Operations

- `quicbaas_ollama_health` - Check Ollama connectivity
- `quicbaas_ollama_model_list` - List available Ollama models
- `quicbaas_ollama_model_info` - Get model details
- `quicbaas_ollama_embed` - Generate embedding for text
- `quicbaas_ollama_embed_batch` - Generate embeddings for multiple texts

### RAG Operations

- `quicbaas_rag_query` - All-in-one: embed → search → build context
- `quicbaas_rag_build_context` - Build context from user-provided results

See [RAG MCP Tools Reference](../rag/mcp-tools.md) for detailed RAG tool documentation.

## Available Resources

| URI | Description |
|-----|-------------|
| `quicbaas://cluster/status` | Current cluster status |
| `quicbaas://cluster/nodes` | All cluster nodes |
| `quicbaas://cluster/shards` | All shards |
| `quicbaas://metrics/summary` | Performance metrics |
| `quicbaas://cache/stats` | Cache statistics |
| `quicbaas://health` | Health status |
| `quicbaas://backups` | Available backups |
| `quicbaas://users` | User list |
| `quicbaas://roles` | Role list |
| `quicbaas://entities/{type}` | Entities by type |
| `quicbaas://entities/{type}/{id}` | Specific entity |

## Examples

### Initialize Connection

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize"
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "serverInfo": {
      "name": "QuicBaaS MCP Server",
      "version": "1.0.0"
    },
    "capabilities": {
      "tools": {},
      "resources": {}
    }
  }
}
```

### Store Data

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_put",
    "arguments": {
      "entityType": "user",
      "id": 100,
      "data": "eyJuYW1lIjoiSm9obiBEb2UiLCJlbWFpbCI6ImpvaG5AZXhhbXBsZS5jb20ifQ=="
    }
  }
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "id": 100,
    "version": 1,
    "timestamp": 1700000000,
    "leaderID": 1
  }
}
```

### Retrieve Data

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_get",
    "arguments": {
      "entityType": "user",
      "id": 100
    }
  }
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "id": 100,
    "data": "eyJuYW1lIjoiSm9obiBEb2UiLCJlbWFpbCI6ImpvaG5AZXhhbXBsZS5jb20ifQ==",
    "version": 1,
    "timestamp": 1700000000,
    "found": true
  }
}
```

### Read Cluster Status Resource

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "resources/read",
  "params": {
    "uri": "quicbaas://cluster/status"
  }
}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "contents": [
      {
        "uri": "quicbaas://cluster/status",
        "mimeType": "application/json",
        "text": "{\"clusterID\":\"quicbaas-cluster-1\",\"nodes\":[...]}"
      }
    ]
  }
}
```

### Batch Operations

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_batch_put",
    "arguments": {
      "operations": [
        {
          "entityType": "user",
          "id": 101,
          "data": "eyJuYW1lIjoiQWxpY2UifQ=="
        },
        {
          "entityType": "user",
          "id": 102,
          "data": "eyJuYW1lIjoiQm9iIn0="
        }
      ]
    }
  }
}
```

### Transaction

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_transaction",
    "arguments": {
      "compare": [
        {
          "entityType": "counter",
          "id": 1,
          "target": 0,
          "operator": 1,
          "version": 5
        }
      ],
      "success": [
        {
          "opType": 1,
          "entityType": "counter",
          "id": 1,
          "data": "eyJ2YWx1ZSI6IDZ9"
        }
      ],
      "failure": []
    }
  }
}
```

## AI Assistant Integration

### Claude Integration

Claude can interact with QuicBaaS through MCP:

**User:** "Create a new user with ID 100"

**Claude's Process:**
1. Encodes user data as base64 JSON
2. Calls `quicbaas_put` tool
3. Returns confirmation

**User:** "What's the cluster status?"

**Claude's Process:**
1. Reads `quicbaas://cluster/status` resource
2. Parses JSON response
3. Formats for human readability

**Claude's Response:** "The cluster has 3 nodes (all healthy), 1 shard with leader on node 1, and total data size of 1.2 GB."

### Example Workflows

**Storing user data:**
```
User: Store a user named Alice with email alice@example.com
Claude: [Calls quicbaas_put]
Claude: I've stored the user data successfully.
```

**Querying by index:**
```
User: Find the user with email john@example.com
Claude: [Calls quicbaas_query with email index]
Claude: Found user John Doe (ID: 100).
```

**Checking cluster health:**
```
User: Is the cluster healthy?
Claude: [Reads quicbaas://cluster/status]
Claude: Yes, all 3 nodes are healthy and operational.
```

## Error Codes

### Standard JSON-RPC Errors

| Code | Name | Description |
|------|------|-------------|
| -32700 | Parse Error | Invalid JSON |
| -32600 | Invalid Request | Invalid JSON-RPC format |
| -32601 | Method Not Found | Unknown method |
| -32602 | Invalid Params | Invalid parameters |
| -32603 | Internal Error | Server error |

### Authorization Errors

| Code | Name | HTTP Status | Description |
|------|------|-------------|-------------|
| -32001 | AuthenticationRequired | 401 | No valid token provided |
| -32002 | InvalidToken | 401 | Token is malformed or expired |
| -32003 | PermissionDenied | 403 | Insufficient permissions |

Authorization errors include `WWW-Authenticate` header per RFC 6750.

## Configuration

### Go Server

```go
package main

import (
    "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
    "github.com/jeremyhahn/go-quicbaas/pkg/logger"
    "time"
)

func main() {
    log, _ := logger.New(logger.LogConfig{
        Level:  "info",
        Format: "json",
        Output: "stderr", // stderr for stdio transport
    })

    svc := initializeQuicBaaS(log)

    config := &mcp.Config{
        ServerName:            "QuicBaaS MCP Server",
        ServerVersion:         "1.0.0",
        Transport:             "stdio",
        RequestTimeout:        30 * time.Second,
        MaxConcurrentRequests: 100,
    }

    server := mcp.NewServer(svc, config, log)
    server.Start()
}
```

### Python Client

```python
import json
import requests

MCP_URL = "http://localhost:8888/mcp"

def mcp_call(method, params=None):
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": method,
    }
    if params:
        payload["params"] = params

    response = requests.post(MCP_URL, json=payload)
    return response.json()

# Store data
result = mcp_call("tools/call", {
    "name": "quicbaas_put",
    "arguments": {
        "entityType": "user",
        "id": 100,
        "data": "eyJuYW1lIjoiSm9obiJ9"
    }
})
print(result)
```

## Security

### Authentication

MCP uses OAuth 2.1 with mandatory PKCE. See [MCP Authorization](mcp-authorization.md) for complete flow.

**OAuth 2.1 Flow:**
1. Discover AS metadata: `GET /.well-known/oauth-authorization-server`
2. Authorization request with PKCE (S256)
3. Token exchange with code verifier
4. Include Bearer token in requests

**Discovery Endpoints:**
- `/.well-known/oauth-authorization-server` - OAuth AS Metadata (RFC 8414)
- `/.well-known/oauth-protected-resource` - Protected Resource Metadata (RFC 9728)
- `/.well-known/openid-configuration` - OIDC Discovery
- `/.well-known/jwks.json` - JSON Web Key Set

**Dynamic Client Registration (RFC 7591):**
```bash
POST /oauth2/register
Content-Type: application/json

{
  "redirect_uris": ["https://client.example.com/callback"],
  "client_name": "My MCP Client",
  "grant_types": ["authorization_code"]
}
```

### Authorization

Operations are subject to RBAC/ABAC:
- Read operations require `read` permission
- Write operations require `write` permission
- Admin operations require `admin` role
- Token audience must match MCP resource URI

### External IdP Federation

QuicBaaS can delegate authentication to external providers (Okta, Auth0, etc.):
- MCP server inherits IAM federation configuration
- External tokens validated via JWKS
- Claims mapped to QuicBaaS permissions

### TLS

All OAuth/MCP endpoints require HTTPS in production. HTTP allowed only for localhost development.

## Performance

### Benchmarks

- Stdio Transport: ~0.5ms latency per request
- HTTP Transport: ~1-2ms latency per request
- Concurrent Requests: 100+ concurrent requests

### Optimizations

- Connection pooling (HTTP transport)
- Request pipelining (stdio transport)
- Streaming responses for large datasets
- Efficient JSON marshaling
- Context-based timeouts

## Testing

### Ping Test

```bash
curl -X POST http://localhost:8888/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"ping"}'
```

### Health Check

```bash
curl http://localhost:8888/health
```

Response:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "mcp": "2024-11-05"
}
```

## See Also

- [MCP Authorization](mcp-authorization.md) - OAuth 2.1 authentication
- [MCP IAM Integration](../iam/mcp-integration.md) - IdP federation, role mapping
- [gRPC API](grpc.md) - Binary protocol
- [REST API](rest.md) - HTTP/JSON interface
- [IAM Configuration](../iam/configuration.md) - Identity management
- [Architecture](../architecture/README.md) - System design
- [Model Context Protocol Specification](https://modelcontextprotocol.io/)
- [MCP Authorization Spec](https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization)
