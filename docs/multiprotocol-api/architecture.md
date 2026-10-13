# Multiprotocol API — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/multiprotocol-api/`.

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

### QUIC (SDK client only)

**No component in this repository serves HTTP/3 today.** There is no
`pkg/api/quic` package, no QUIC server, no `--quic-port` on `quicbaasd` and no
`http3.Server` anywhere under `pkg/`.

QUIC exists only as the SDK client transport `sdk/go/transport/quic`. It embeds
`*rest.Transport` and swaps only the `http.Client` for quic-go's
`http3.Transport` (`sdk/go/transport/quic/transport.go`), so it speaks the REST
dialect over HTTP/3 and has no wire protocol of its own. Until something serves
HTTP/3, that client has nothing to dial. Client usage:
[api.md § QUIC API](api.md#quic-api).

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
# Typed data operations (there is no kv or batch verb)
quicbaas data put user 123 '{"name":"Alice"}' --json
quicbaas data get user 123
quicbaas data delete user 123 --yes

# Vector operations
quicbaas vector search similar docs --vector 0.12,0.34,0.56 --k 10
quicbaas vector upsert docs doc-123 --vector 0.12,0.34,0.56 --metadata doc_id=123
quicbaas vector batch-upsert docs --file vectors.json

# Cluster management
quicbaas cluster status
quicbaas node add node-4 203.0.113.10:50051
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

The REST API rate-limits each source IP by default: 1000 requests per second,
with a burst of the same size. Over the limit, a request gets 429.

```yaml
server:
  rest:
    rate_limit: 1000   # omitted or 0 = default; -1 disables
```

The limiter keys on the client address that `server.trusted_proxies`
resolves. The OAuth2 client-authentication endpoints keep a stricter limiter
of their own. See
[Rate Limiting Middleware](../security-hardening/api.md#rate-limiting-middleware)
for the full behavior and how the default was measured.

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
| Mobile apps | REST | QUIC has no server today (see [QUIC](#quic-sdk-client-only)) |
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
- **QUIC client transport** - `sdk/go/transport/quic/transport.go` (no server)
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


---

## Multi-Protocol Parity

QuicBaaS exposes the same operation surface over five transports.
Every CLI command, every SDK call, every GUI action goes through the
SDK's transport abstraction — if a feature works on one transport it
works on all of them. The integration tests under
`test/integration/cli/` enforce this with the `RunForAllTransports`
table-driven harness.

### Transports

| Transport | Default port | Wire | Auth | Streaming |
|-----------|--------------|------|------|-----------|
| REST      | `:18080` (TLS) | HTTP/1.1 + JSON | OAuth2 / mTLS | NDJSON over chunked |
| gRPC      | `:19090` (TLS, `h2` ALPN) | Protocol Buffers | OAuth2 / mTLS | bidi streams |
| QUIC      | `:10443` (TLS 1.3) | QUIC streams | OAuth2 / mTLS | native streams |
| MCP       | `:11443` (TLS) | JSON-RPC over WSS | OAuth2 / mTLS | server push |
| Unix      | `/tmp/quicbaas.sock` | gRPC over UDS | filesystem perms (no TLS) | bidi streams |

The `embedded` transport is also available — it's used by the Wails
desktop binary, which links the SDK and the daemon together so calls
are direct Go function calls (no IPC, no serialisation).

### Selecting a transport

Auto-detection from `--address`:

| Address | Transport |
|---------|-----------|
| `https://host:port` | REST |
| `host:port` (no scheme) | gRPC over TLS |
| `quic://host:port` | QUIC |
| `mcp://host:port` | MCP |
| `/abs/path` (starts with `/`) | Unix socket |

Override with `--transport <name>`. The CLI always uses the SDK
abstraction internally — there is no transport-specific CLI code.

### Coverage matrix

The SDK exposes the same `Client` interface for every transport
(`sdk/typescript/src/interfaces.ts` for the TS SDK, `sdk/go` for Go).
Per-method coverage is enforced by the table-driven integration tests:

```go
func TestOrchestratorInference(t *testing.T) {
    RunForAllTransports(t, "Inference", func(t *testing.T, runner *TestRunner) {
        // ... same test body executes over REST, gRPC, QUIC, MCP, Unix.
    })
}
```

Tests live in `test/integration/cli/*_test.go`. CI fails if any
transport returns `notImpl` or a `method-not-supported` error for a
method that any other transport implements.

### Adding a new method

When you add a method to the orchestration / data / IAM / KMS / etc.
service:

1. Add it to the domain interface in the relevant `pkg/<service>` package.
2. Add the proto RPC if it's gRPC-bound.
3. Implement the server side once per transport: REST handler,
   gRPC handler, QUIC handler (usually shares with gRPC), MCP tool,
   Unix-socket gRPC (shares with gRPC).
4. Implement the SDK client side once per transport. The TypeScript
   SDK's per-transport stubs (`sdk/typescript/src/{rest,grpc_web,quic,mcp,unix}.ts`)
   must all have a real implementation — `notImpl` calls fail the
   integration tests.
5. Add a `RunForAllTransports` test that exercises the method.

DRY rule: the orchestration service's domain method is the canonical
implementation. Every transport handler delegates to it — no business
logic in transport handlers.

### TLS / mTLS

REST, gRPC, QUIC, and MCP all run over TLS by default. Server certs
are issued by the daemon's embedded CA at startup; clients can
auto-discover the CA chain at `--data-dir/certs/ca-chain.crt`. mTLS
is engaged whenever the client presents a cert (`--tls-cert` /
`--tls-key`).

The Unix transport runs without TLS — the wire is a local socket and
filesystem permissions guard access.

### See also

- REST API
- gRPC API
- QUIC
- [MCP](../software-factory/api.md)
- [Fleet quick start](../agent-fleet/user.md#quick-start) — same `inference` command on every transport.


---

## QuicBaaS API Documentation

QuicBaaS provides multiple API protocols for diverse client requirements. **All APIs share a common SDK abstraction**, so every CLI command, SDK call, and GUI action works identically over every transport. See multiprotocol.md for the parity contract and the integration tests that enforce it.

### Available APIs

| Protocol | Use Case | Transport | Performance | Documentation |
|----------|----------|-----------|-------------|---------------|
| gRPC | Service-to-service communication | Unix Socket/TCP, Binary | 150K+ ops/sec, <5ms P99 | grpc.md |
| QUIC | Mobile clients, high-latency networks | UDP/TLS 1.3, Binary | 100K+ ops/sec, <3ms P99 | quic.md |
| REST | Web applications, HTTP clients | HTTP/1.1-2, JSON | 25K+ ops/sec, <15ms P99 | rest.md |
| MCP | AI agents, LLM integration | Stdio/HTTP, JSON-RPC | Context-aware operations | mcp.md |
| Unix | Local CLI / desktop GUI | gRPC over UDS, no TLS | gRPC-equivalent, lowest latency | (uses gRPC docs) |
| Embedded | Wails desktop app | direct Go calls | no IPC overhead | (Wails-only) |

> **Multi-protocol parity is mandatory.** A method that works on REST must work on gRPC, QUIC, Unix, and MCP. The integration suite at `test/integration/cli/` exercises every CLI command via every transport — adding a feature means implementing each transport's handler in the same PR. See multiprotocol.md.

### Default Transport: Unix Socket

QuicBaaS uses Unix domain sockets as the default transport for gRPC communication:

| Environment | Socket Path |
|-------------|-------------|
| Development | `/tmp/quicbaas.sock` |
| Production | `/var/run/quicbaas/quicbaas.sock` |

Unix sockets are enabled by default. TCP and REST are optional transports that must be explicitly enabled.

### When to Use Each API

#### gRPC (Unix Socket or TCP)

Choose gRPC when you need:
- Maximum throughput and minimal latency
- Strong typing with Protocol Buffers
- Bidirectional streaming
- Multi-language client support
- Service mesh integration

**Unix Socket (Default):** Lowest latency for local connections, no port management, enhanced security through filesystem permissions.

**TCP:** Required for remote connections, service mesh integration, or when clients don't support Unix sockets.

Best for microservices, internal APIs, and high-performance systems.

#### QUIC

Choose QUIC when you need:
- Mobile client connectivity with connection migration
- Low latency on high-latency networks (0-RTT resumption)
- Concurrent operations without head-of-line blocking
- Built-in TLS 1.3 encryption
- Binary protocol efficiency

Best for mobile applications, edge deployments, and unreliable network conditions.

#### REST

Choose REST when you need:
- Universal HTTP client compatibility
- Browser-based applications
- Simple debugging with curl
- OpenAPI/Swagger documentation
- Standard HTTP tooling

Best for public APIs, web applications, and third-party integrations.

#### MCP

Choose MCP when you need:
- AI agent integration (Claude, ChatGPT)
- Tool-based LLM interactions
- Context-aware database operations
- RAG workflows with vector search
- Semantic operations

Best for AI-powered applications, chatbots, and intelligent systems.

### Common Features

#### Authentication

All APIs support the same authentication methods:

**API Keys:**
```bash
# REST
curl -H "X-API-Key: sk_live_abc123" http://localhost:8080/api/v1/health

# gRPC (via metadata)
md := metadata.New(map[string]string{"x-api-key": "sk_live_abc123"})
```

**Mutual TLS:**
```bash
curl --cert client.crt --key client.key --cacert ca.crt https://localhost:8443/api/v1/health
```

#### Consistency Levels

All write operations support tunable consistency:
- `local` - Write to local node only (low latency)
- `quorum` - Write to majority of replicas (strong consistency)

#### Error Codes

Consistent error codes across all protocols:

| Code | HTTP | gRPC | Meaning |
|------|------|------|---------|
| NOT_FOUND | 404 | NOT_FOUND | Entity does not exist |
| INVALID_ARGUMENT | 400 | INVALID_ARGUMENT | Invalid request parameters |
| PERMISSION_DENIED | 403 | PERMISSION_DENIED | Authorization failed |
| UNAVAILABLE | 503 | UNAVAILABLE | Service temporarily unavailable |
| INTERNAL | 500 | INTERNAL | Internal server error |

### Architecture

All API protocols share a common service facade (`service.QuicBaaS` interface):

```
┌──────────────────────────────────────────┐
│          API Protocol Layer              │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐    │
│  │ gRPC │ │ QUIC │ │ REST │ │ MCP  │    │
│  └───┬──┘ └───┬──┘ └───┬──┘ └───┬──┘    │
└──────┼────────┼────────┼────────┼────────┘
       └────────┴────────┴────────┘
                │
      ┌─────────────────────┐
      │  service.QuicBaaS   │
      │  • KV Operations    │
      │  • Transactions     │
      │  • Vector Search    │
      │  • Cluster Mgmt     │
      │  • Time-Series      │
      │  • Auth/RBAC        │
      └─────────────────────┘
```

This architecture provides:
- Protocol independence - add new APIs without changing business logic
- Code reuse - shared implementations across all protocols
- Consistent behavior - same operations produce identical results
- Easy testing - test once, deploy everywhere

### See Also

- gRPC API - High-performance binary protocol
- QUIC API - Mobile and high-latency network protocol
- REST API - HTTP/JSON interface
- MCP API - AI agent integration
- CLI Guide - Command-line interface
- [Architecture](architecture.md) - System design


---

## QuicBaaS Architecture

QuicBaaS is a high-performance, distributed key-value database with native vector search capabilities, built on multi-Raft consensus and PebbleDB storage. It provides strong consistency, horizontal scalability, field-level encryption, and multi-protocol API access.

### Core Architecture

```
+-------------------------------------------------------------------------+
|                          Client Layer                                    |
|     gRPC Client | REST Client | QUIC Client | MCP Client | CLI          |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                       API Protocol Layer                                 |
|     gRPC Server | REST Server | QUIC Server | MCP Server | CLI          |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                        Service Facade Layer                              |
|   KVService | VectorService | TransactionService | ClusterService       |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                      Business Logic Layer                                |
|                                                                          |
|  +-------------------+  +--------------------+  +--------------------+   |
|  |   Router          |  |  Multi-Raft        |  |  State Machines    |   |
|  |  - Sharding       |->|  - Shard 1         |->|  - KV FSM          |   |
|  |  - Load Balance   |  |  - Shard 2         |  |  - Vector FSM      |   |
|  |  - Consistency    |  |  - Shard N         |  |  - TS FSM          |   |
|  +-------------------+  +--------------------+  +--------------------+   |
|                                                        |                 |
|                                                        v                 |
|  +-------------------------------------------------------------------+   |
|  |              Raft Consensus (QuicRaft)                          |   |
|  |  - Leader Election  - Log Replication  - Snapshots                |   |
|  +-------------------------------------------------------------------+   |
+--------------------------------------+--------------------------------------+
                                       |
+--------------------------------------+--------------------------------------+
|                         Storage Layer                                    |
|                                                                          |
|  +------------------+  +------------------+  +------------------+         |
|  |   PebbleDB       |  |  HNSW Index      |  |  Index Manager   |         |
|  |  - LSM-tree      |  |  - Vector ANN    |  |  - Secondary     |         |
|  |  - WAL           |  |  - Namespaces    |  |  - Metadata      |         |
|  |  - Snapshots     |  |  - Persistence   |  |  - Time-series   |         |
|  +------------------+  +------------------+  +------------------+         |
+--------------------------------------------------------------------------+
```

### Core Components

#### API Layer

Multiple protocol support for maximum flexibility:

- **gRPC** - High-performance binary protocol (150K+ ops/sec, <5ms P99)
- **REST** - HTTP/JSON for web clients (25K+ ops/sec, <15ms P99)
- **QUIC** - Low-latency UDP protocol (100K+ ops/sec, <3ms P99)
- **MCP** - Model Context Protocol for AI agents
- **CLI** - Command-line interface with tab completion

All protocols share business logic through a service facade pattern, ensuring consistent behavior and code reuse. See [api.md](api.md)

#### Service Facade

Abstracts business logic from protocol details, providing a single point of access for all database operations:

- Request routing and shard selection
- Consistency level enforcement
- Transaction coordination
- Cluster state management
- Security and authorization
- Metrics collection and logging

See service-facade.md

#### Consensus Layer

Multi-Raft architecture using QuicRaft for horizontal scaling:

- Multiple independent Raft groups per cluster
- Deterministic key-to-shard routing
- Strong consistency with linearizable reads
- Fault tolerance (survives (n-1)/2 failures)
- Automatic leader election and failover
- Configurable write modes (sync, WAL, batched)

See raft-consensus.md, write-modes.md

#### State Machines

Generic, type-safe state machines implementing business logic:

- **KV State Machine** - Key-value operations with indexing
- **Vector State Machine** - Vector search with HNSW indexing
- **Time-Series State Machine** - Temporal data management
- ACID guarantees via Raft consensus
- Snapshot support for recovery
- Concurrent and on-disk variants

See state-machines.md

#### Storage Layer

Embedded LSM-tree storage with PebbleDB:

- High write throughput with WAL
- Efficient range scans
- Block cache and bloom filters
- Compression (Snappy/Zstd)
- Secondary indexes for fast lookups

See storage-engine.md

#### Vector Database

Native vector similarity search for AI/ML workloads:

- HNSW algorithm for approximate nearest neighbor search
- Multiple distance metrics (L2, Cosine, Dot Product)
- Namespace isolation for multi-tenancy
- Hybrid search with metadata filtering
- RAG workflow optimization

See vector.md

#### Security

Defense-in-depth security architecture:

- TLS 1.3 for data in transit
- Field-level encryption for data at rest
- Integrated Key Management System (KMS)
- FIPS 140-3 compliance mode
- API key and mTLS authentication
- Audit logging

See security.md

### Key Design Decisions

1. **Multi-Raft Design** - Horizontal scalability through independent Raft groups, avoiding single-leader bottleneck
2. **Generic State Machines** - Type-safe, reusable state machines using Go generics
3. **Service Facade Pattern** - Protocol-independent business logic for consistent behavior across APIs
4. **Dual-Codec System** - MessagePack for storage, flexible wire formats for APIs (3x faster, 62% fewer allocations)
5. **Embedded Storage** - PebbleDB for pure-Go, production-grade LSM-tree storage
6. **HNSW Vector Index** - Approximate nearest neighbor search with 95%+ recall and <10ms P99 latency
7. **Field-Level Encryption** - Granular data protection with automatic key rotation

### Performance Characteristics

| Operation | Throughput (ops/sec) | Latency (P99) | Notes |
|-----------|---------------------|---------------|-------|
| KV Put (gRPC) | 150,000+ | <5ms | Single shard, batched mode |
| KV Get (local read) | 200,000+ | <1ms | Stale allowed |
| KV Get (quorum read) | 100,000+ | <5ms | Linearizable |
| Vector Search (k=10) | 5,000+ | <50ms | 10M vectors, HNSW |
| Batch Operations | 10x single | - | Amortized overhead |

### Scalability

- **Nodes**: Linear scaling up to 10+ nodes
- **Shards**: 1-100+ Raft groups per cluster
- **Vectors**: 1B+ vectors across shards
- **Data Size**: Petabyte-scale with proper sharding

### Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Consensus | [QuicRaft](https://github.com/jeremyhahn/go-quicraft) | Multi-group Raft |
| Storage | [PebbleDB](https://github.com/cockroachdb/pebble) | LSM-tree database |
| RPC | [gRPC](https://grpc.io/) | High-performance RPC |
| CLI | [Cobra](https://github.com/spf13/cobra) | Command framework |
| Config | [Viper](https://github.com/spf13/viper) | Configuration |
| Logging | [log/slog](https://pkg.go.dev/log/slog) | Structured logging (stdlib) |
| Metrics | [Prometheus](https://prometheus.io/) | Monitoring |

### Documentation Index

#### Core Architecture
- [API Layer](api.md) - Multi-protocol API design
- Service Facade - Business logic abstraction
- Raft Consensus - Multi-Raft implementation
- State Machines - Business logic layer
- Storage Engine - PebbleDB integration
- Write Modes - Durability guarantees and performance tradeoffs
- Composition - How quicbaasd composes qrdb + xkms via `With*` injection
- Embedded Mode - In-process qrdb routing (QRDBEmbeddedServicers + WithEmbeddedQRDB)
- Barrier - StorageBarrier design (Software + Shamir + None)

#### Features
- Vector Database - Vector search architecture
- Security - Encryption and authentication
- Multi-Tenant Architecture - Tenant isolation and resource management
- [Multi-Tenant Service Isolation](../multi-tenancy/engineering.md) - Security policies for tenant isolation
- Transactions - ACID transaction support
- Batch Operations - Bulk operation optimization
- Indexing - Secondary index design

#### Implementation Details
- Binary Protocol - Wire protocol optimization
- Implementation Details - Internal design notes

### References

- [Raft Consensus Paper](https://raft.github.io/raft.pdf) - Consensus algorithm
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf) - Storage structure
- [HNSW Paper](https://arxiv.org/abs/1603.09320) - Vector indexing
- [QuicRaft Documentation](https://github.com/jeremyhahn/go-quicraft)
- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)

---

**Last Updated:** 2026-02-03
