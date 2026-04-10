# API Layer Architecture

QuicBaaS provides multiple API protocols for maximum flexibility and performance.

## Overview

The API layer provides clean separation between network protocols and business logic through a service facade pattern. This enables QuicBaaS to support multiple APIs without duplicating business logic.

**Supported APIs:**
- **gRPC** - High-performance binary protocol
- **REST** - HTTP/JSON for web clients
- **QUIC** - Low-latency UDP-based protocol
- **MCP** - Model Context Protocol for AI agents
- **CLI** - Command-line interface

## Architecture

### Service Facade Pattern

```
┌──────────────────────────────────────────────┐
│         API Protocol Layer                    │
│  ┌─────────┐ ┌────────┐ ┌──────┐ ┌────────┐│
│  │  gRPC   │ │  REST  │ │ QUIC │ │  MCP   ││
│  └────┬────┘ └───┬────┘ └──┬───┘ └───┬────┘│
└───────┼──────────┼─────────┼─────────┼─────┘
        │          │         │         │
        └──────────┴─────────┴─────────┘
                     ↓
┌──────────────────────────────────────────────┐
│         Service Facade (pkg/service)          │
│  • KVService                                  │
│  • BatchService                               │
│  • TransactionService                         │
│  • QueryService                               │
│  • ClusterService                             │
│  • VectorService                              │
│  • OllamaService                              │
│  • RAGService                                 │
└──────────────────────────────────────────────┘
                     ↓
┌──────────────────────────────────────────────┐
│         Business Logic Layer                  │
│  State Machines │ Raft │ Storage              │
└──────────────────────────────────────────────┘
```

### Key Benefits

1. **Protocol Independence** - Add new protocols without changing business logic
2. **Code Reuse** - All protocols share the same service implementations
3. **Consistent Behavior** - Same logic ensures consistent results across APIs
4. **Easy Testing** - Test business logic once, reuse across protocols

## API Protocols

### gRPC (`pkg/api/grpc`)

High-performance binary protocol for service-to-service communication.

**Features:**
- Protocol Buffers schema
- Bidirectional streaming
- Connection multiplexing
- Load balancing support
- HTTP/2 transport

**Example:**
```go
// Define service
service QuicBaaS {
  rpc Get(GetRequest) returns (GetResponse);
  rpc Put(PutRequest) returns (PutResponse);
  rpc BatchGet(BatchGetRequest) returns (stream BatchGetResponse);
}

// Client usage
conn, _ := grpc.Dial("localhost:50051")
client := pb.NewQuicBaaSClient(conn)
resp, _ := client.Get(ctx, &pb.GetRequest{Key: "user:123"})
```

**Performance:**
- Throughput: 150,000+ ops/sec
- Latency (P99): <5ms
- Connection overhead: Low (multiplexing)

### REST (`pkg/api/rest`)

HTTP/JSON API for web browsers and standard HTTP clients.

**Features:**
- RESTful resource design
- JSON request/response
- OpenAPI/Swagger documentation
- Content negotiation (JSON/YAML/MessagePack)
- HTTP status codes

**Endpoints:**
```
GET    /api/v1/kv/:key              # Get key
PUT    /api/v1/kv/:key              # Put key
DELETE /api/v1/kv/:key              # Delete key
POST   /api/v1/batch/get            # Batch get
POST   /api/v1/query                # Query with filters
GET    /api/v1/cluster/health       # Cluster health
```

**Example:**
```bash
# Get value
curl http://localhost:8080/api/v1/kv/user:123

# Put value with JSON
curl -X PUT http://localhost:8080/api/v1/kv/user:123 \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice","age":30}'

# Request YAML response
curl -H "Accept: application/yaml" \
  http://localhost:8080/api/v1/kv/user:123
```

**Performance:**
- Throughput: 25,000+ ops/sec
- Latency (P99): <15ms
- Connection overhead: Higher (HTTP/1.1 or HTTP/2)

### QUIC (`pkg/api/quic`)

UDP-based protocol for low-latency communication.

**Features:**
- 0-RTT connection establishment
- Built-in encryption (TLS 1.3)
- Connection migration
- Improved congestion control
- Multiplexed streams

**Use Cases:**
- Mobile clients with network handoffs
- Geo-distributed deployments
- Latency-sensitive applications
- Environments with packet loss

**Performance:**
- Throughput: 100,000+ ops/sec
- Latency (P99): <3ms (0-RTT)
- Connection overhead: Very low

### MCP (`pkg/api/mcp`)

Model Context Protocol for AI agents and LLM integrations.

**Features:**
- Tool-based interface for AI agents
- Structured prompts and responses
- Context-aware operations
- Vector similarity search integration
- RAG (Retrieval-Augmented Generation) support

**Tools:**
- `get_value` - Retrieve key-value data
- `put_value` - Store key-value data
- `query_data` - Query with filters
- `search_similar` - Vector similarity search
- `batch_operations` - Bulk operations

**Example:**
```python
# Claude Desktop integration
from mcp import Client

client = Client("quicbaas")
result = client.call_tool("search_similar", {
    "query_vector": embedding,
    "namespace": "docs",
    "k": 5
})
```

### CLI (`cmd/quicbaas`)

Command-line interface built with Cobra.

**Features:**
- Interactive and scripting modes
- Tab completion
- Configuration via flags, env vars, or config files
- Colored output
- Progress indicators

**Commands:**
```bash
# Key-value operations
quicbaas kv get user:123
quicbaas kv put user:123 --value '{"name":"Alice"}'
quicbaas kv delete user:123

# Batch operations
quicbaas batch get --keys-file keys.txt
quicbaas batch put --data-file data.json

# Vector operations
quicbaas vector search --query-file query.bin --k 10
quicbaas vector upsert --vector-file vec.bin --metadata '{"doc_id":"123"}'

# Cluster management
quicbaas cluster status
quicbaas cluster add-node --address 192.168.1.10:50051
```

## Dual-Codec System

The API layer integrates with QuicBaaS's dual-codec system for optimal performance.

### Codec Architecture

**Storage Codec (MessagePack):**
- Fixed binary format for all disk operations
- 30-50% smaller than JSON
- Type-preserving
- Consistent across cluster

**Wire Codec (Flexible):**
- Per-request format selection
- Supports: JSON, YAML, MessagePack, Protobuf
- Content negotiation
- Auto-detection from struct tags

### Content Negotiation

**REST API:**
```bash
# Request JSON response (default)
curl http://localhost:8080/api/v1/kv/user:123

# Request YAML response
curl -H "Accept: application/yaml" \
  http://localhost:8080/api/v1/kv/user:123

# Request MessagePack response
curl -H "Accept: application/msgpack" \
  http://localhost:8080/api/v1/kv/user:123
```

**gRPC:**
```go
// Set codec preference in metadata
md := metadata.New(map[string]string{
    "x-quicbaas-codec": "yaml",
})
ctx := metadata.NewOutgoingContext(context.Background(), md)
```

### Performance Impact

Using pre-encoded responses eliminates double-encoding:

- **Traditional:** Storage (decode) → Re-encode for client
- **Optimized:** Storage → Pre-encoded response (zero-copy)

**Results:**
- 3x faster response latency
- 62% fewer allocations
- 2.5x higher REST API throughput

## Authentication & Authorization

All APIs support consistent authentication mechanisms.

### API Keys

Token-based authentication for programmatic access:

```bash
# REST API
curl -H "Authorization: Bearer sk_live_abc123" \
  http://localhost:8080/api/v1/kv/user:123

# gRPC metadata
md := metadata.New(map[string]string{
    "authorization": "Bearer sk_live_abc123",
})
```

### mTLS

Certificate-based authentication for service-to-service:

```go
// Load client certificate
cert, _ := tls.LoadX509KeyPair("client.crt", "client.key")
creds := credentials.NewTLS(&tls.Config{
    Certificates: []tls.Certificate{cert},
})

// Connect with mTLS
conn, _ := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(creds))
```

## Rate Limiting

Protect APIs from abuse and ensure fair resource allocation.

### Configuration

```yaml
security:
  rate_limit:
    enabled: true
    requests_per_second: 1000
    burst: 2000
    by_api_key: true
```

### Implementation

```go
// Per-API-key rate limiter
limiter := rate.NewLimiter(1000, 2000)
if !limiter.Allow() {
    return ErrRateLimitExceeded
}
```

## Error Handling

Consistent error responses across all APIs.

### Error Types

```go
type ErrorCode int32

const (
    OK                  ErrorCode = 0
    NotFound            ErrorCode = 404
    InvalidArgument     ErrorCode = 400
    PermissionDenied    ErrorCode = 403
    Internal            ErrorCode = 500
    Unavailable         ErrorCode = 503
)
```

### REST Responses

```json
{
  "error": {
    "code": 404,
    "message": "key not found",
    "details": {
      "key": "user:123",
      "shard_id": 1
    }
  }
}
```

### gRPC Status

```go
st := status.New(codes.NotFound, "key not found")
st, _ = st.WithDetails(&pb.ErrorInfo{
    Key: "user:123",
    ShardId: 1,
})
return st.Err()
```

## Metrics & Monitoring

All APIs expose Prometheus metrics for monitoring.

### API Metrics

```
# Request count by API and status
quicbaas_api_requests_total{api="grpc", method="Get", status="ok"}
quicbaas_api_requests_total{api="rest", method="Put", status="error"}

# Request latency histogram
quicbaas_api_request_duration_seconds{api="grpc", method="Get", quantile="0.99"}

# Active connections
quicbaas_api_active_connections{api="grpc"}
```

### Example Queries

```promql
# Request rate by API
rate(quicbaas_api_requests_total[5m])

# P99 latency by method
histogram_quantile(0.99,
  rate(quicbaas_api_request_duration_seconds_bucket[5m])
)

# Error rate
rate(quicbaas_api_requests_total{status="error"}[5m])
```

## Best Practices

### Choosing an API

| Use Case | Recommended API | Reason |
|----------|----------------|--------|
| Service-to-service | gRPC | High performance, strong typing |
| Web applications | REST | Universal compatibility |
| Mobile apps | QUIC | Low latency, connection migration |
| AI agents | MCP | Native tool interface |
| DevOps | CLI | Human-friendly, scriptable |

### Performance Optimization

1. **Use batching** - Reduce round trips with batch operations
2. **Enable compression** - gRPC compression for large payloads
3. **Connection pooling** - Reuse connections across requests
4. **Request appropriate format** - Use MessagePack for binary efficiency
5. **Leverage caching** - Use TTL headers and ETags (REST)

### Security

1. **Always use TLS** - Encrypt all network communication
2. **Validate inputs** - Check request parameters at API layer
3. **Rate limit aggressively** - Protect from abuse
4. **Audit log** - Track all operations for compliance
5. **Principle of least privilege** - Grant minimal required permissions

## References

### Implementation
- **gRPC Server** - `pkg/api/grpc/server.go`
- **REST Server** - `pkg/api/rest/server.go`
- **QUIC Server** - `pkg/api/quic/server.go`
- **MCP Server** - `pkg/api/mcp/server.go`
- **CLI** - `cmd/quicbaas/main.go`

### Service Facade
- **Service Interface** - `pkg/service/service.go`
- **KV Service** - `pkg/service/kv_service.go`
- **Vector Service** - `pkg/service/vector_service.go`
- **Ollama Service** - `pkg/service/ollama_service.go`
- **RAG Service** - `pkg/rag/context/builder.go`

### Documentation
- **Service Facade Architecture** - `SERVICE_FACADE_ARCHITECTURE.md`
- **Dual-Codec System** - `../features/DUAL_CODEC_SYSTEM.md`
- **REST API Reference** - `../api/rest.md`
- **gRPC API Reference** - `../api/grpc.md`
- **MCP API Reference** - `../api/mcp.md`
- **RAG MCP Tools** - `../rag/mcp-tools.md`

---

**Last Updated:** 2026-01-12
**Version:** 0.4.0
