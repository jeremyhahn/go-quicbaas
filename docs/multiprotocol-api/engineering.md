# Multiprotocol API — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/multiprotocol-api/`.

The service facade layer provides a unified interface for all database operations across all API protocols (gRPC, REST, QUIC, MCP, CLI), abstracting business logic from protocol details.

## Architecture

QuicBaaS delegates core database primitives (KV, MVCC, Batch, Transaction, Lease, Watch, TimeSeries, Index, Vector) to [go-qrdb](https://github.com/jeremyhahn/go-qrdb) via the qrdb SDK embedded transport. QuicBaaS adds higher-level concerns: multi-tenancy, IAM, KMS, secrets, backup, cache, object storage, and application-specific services.

```
┌─────────────────────────────────────────────────────────┐
│                     API Transports                      │
│  gRPC │ REST │ QUIC │ MCP │ CLI                        │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│              QuicBaaSService (Facade)                   │
│  • Authentication/Authorization                         │
│  • Metrics Collection                                   │
│  • Logging & Tracing                                    │
│  • Error Translation                                    │
└────────────────────┬────────────────────────────────────┘
                     │
    ┌────────────────┼────────────────┐
    │                │                │
┌───▼────┐  ┌───────▼───┐  ┌────────▼──────┐
│ KV     │  │  Cluster  │  │    Backup     │
│Service │  │  Service  │  │    Service    │
└───┬────┘  └─────┬─────┘  └────────┬──────┘
    │             │                 │
┌───▼─────────────▼─────────────────▼────────┐
│        qrdb SDK (Embedded Transport)       │
│  KV │ MVCC │ Batch │ Txn │ Lease │ Watch  │
│  TimeSeries │ Index │ Vector │ Admin      │
└────────────────────┬───────────────────────┘
                     │
┌────────────────────▼───────────────────────┐
│     qrdb FastStateMachine + Raft           │
│  PebbleDB │ HNSW │ Protocol Dispatch      │
└────────────────────────────────────────────┘
```

### qrdb SDK Delegation

Core data-plane operations are delegated through a per-shard `ShardedClient` that creates embedded qrdb SDK clients:

```go
// Per-shard qrdb client (zero-overhead in-process transport)
client, err := svc.qrdbForShard(shardID)
if err != nil { return err }

// KV delegation
err = client.KV().Put(ctx, key, value)

// TimeSeries delegation
err = client.TimeSeries().TSWritePoint(ctx, point)

// Vector delegation (with recursion prevention)
resp, err = client.Vector().UpsertVector(ctx, req)
```

The embedded transport calls qrdb services directly in-process with zero serialization overhead.

## Service Interface

The `QuicBaaSService` interface composes multiple sub-service interfaces:

```go
type QuicBaaSService interface {
    KVService              // Basic key-value operations
    BatchService           // Atomic batch operations
    TransactionService     // Compare-and-swap transactions
    QueryService           // Secondary index queries
    ClusterService         // Cluster management
    BackupService          // Backup and restore
    AuthService            // Authentication
    RBACService            // User and role management
    CacheService           // Cache management
    HealthService          // Health checks
    TimeSeriesService      // Time-series aggregation
    OperationsService      // Migration and rebalancing
    VectorService          // Vector similarity search

    Close() error          // Graceful shutdown
}
```

## Sub-Services

### KVService (4 operations)

Basic key-value operations:

- **Put** - Store key-value pair with consistency guarantees
- **Get** - Retrieve value with configurable consistency
- **Delete** - Remove key-value pair
- **List** - Stream all entities of a type with pagination

### BatchService (3 operations)

Batch operations for improved performance:

- **BatchPut** - Atomic multi-put operation
- **BatchGet** - Multi-get operation (independent requests)
- **BatchDelete** - Atomic multi-delete operation

### TransactionService (1 operation)

ACID transaction support:

- **Transaction** - Atomic compare-and-swap with conditional operations

### QueryService (2 operations)

Secondary index queries:

- **Query** - Search entities using secondary indexes (streaming)
- **ScanIndex** - Full index scan (streaming)

### ClusterService (6 operations)

Cluster management and topology:

- **ClusterStatus** - Get current cluster state
- **GetLeader** - Get leader for specific shard
- **AddNode** - Add node to Raft shard
- **RemoveNode** - Remove node from Raft shard
- **CreateShard** - Create new Raft shard
- **RebalanceCluster** - Redistribute data across shards

### BackupService (5 operations)

Backup and restore functionality:

- **CreateBackup** - Create consistent backup
- **ListBackups** - List available backups
- **RestoreBackup** - Restore from backup
- **DeleteBackup** - Remove backup
- **VerifyBackup** - Check backup integrity

### VectorService (5 operations)

Vector similarity search:

- **UpsertVector** - Insert or update vector
- **SearchVectors** - Find similar vectors
- **DeleteVector** - Remove vector
- **ListNamespaces** - List vector namespaces
- **GetVectorMetadata** - Retrieve vector metadata

## Cross-Cutting Concerns

### Authentication Flow

```
1. Client sends request with JWT token in metadata
2. Service extracts token from context
3. AuthService.ValidateToken() called
4. Token signature verified, expiration checked
5. User info and roles extracted
6. Permissions checked against required permissions
7. If authorized, add user info to context
8. Call actual service method
9. Return response (or auth error)
```

### Metrics Collection

All operations emit Prometheus metrics:

```
# Request count by method and status
quicbaas_service_requests_total{method="Get", status="ok"}

# Request latency histogram
quicbaas_service_request_duration_seconds{method="Get", quantile="0.99"}

# Active requests
quicbaas_service_active_requests{method="Get"}

# Error rate by type
quicbaas_service_errors_total{method="Get", error="not_found"}
```

### Error Translation

Consistent error handling across all APIs:

```go
type ServiceError struct {
    Code       ErrorCode
    Op         string
    Message    string
    Underlying error
    Retryable  bool
    HTTPStatus int        // For REST API
    GRPCStatus codes.Code // For gRPC API
}

// Error mapping
var errorMapping = map[ErrorCode]struct {
    HTTPStatus int
    GRPCStatus codes.Code
}{
    ErrInvalidInput:        {400, codes.InvalidArgument},
    ErrNotFound:            {404, codes.NotFound},
    ErrAlreadyExists:       {409, codes.AlreadyExists},
    ErrUnauthorized:        {401, codes.Unauthenticated},
    ErrForbidden:           {403, codes.PermissionDenied},
    ErrTimeout:             {408, codes.DeadlineExceeded},
    ErrLeaderNotAvailable:  {503, codes.Unavailable},
    ErrInternal:            {500, codes.Internal},
}
```

## Benefits

### Protocol Independence

Add new protocols without changing business logic:

```go
// gRPC integration
type ServerV2 struct {
    service QuicBaaSService
}

func (s *ServerV2) Put(ctx context.Context, req *pb.PutRequest) (*pb.PutResponse, error) {
    serviceReq := protoToServicePutRequest(req)
    serviceResp, err := s.service.Put(ctx, serviceReq)
    if err != nil {
        return nil, serviceErrorToGRPC(err)
    }
    return serviceToProtoPutResponse(serviceResp), nil
}

// REST integration
type HandlerV2 struct {
    service QuicBaaSService
}

func (h *HandlerV2) HandlePut(w http.ResponseWriter, r *http.Request) {
    var serviceReq PutRequest
    json.NewDecoder(r.Body).Decode(&serviceReq)

    serviceResp, err := h.service.Put(r.Context(), &serviceReq)
    if err != nil {
        serviceErrorToHTTP(w, err)
        return
    }

    json.NewEncoder(w).Encode(serviceResp)
}
```

### Code Reuse

All protocols share the same service implementations, ensuring consistent behavior.

### Easy Testing

Mock individual sub-services for unit testing:

```go
func TestKVService_Put(t *testing.T) {
    mockRaftNode := &MockRaftNode{}
    mockRaftNode.On("ProposeWithMode", mock.Anything).Return(nil)

    kvService := &kvServiceImpl{
        raftNode: mockRaftNode,
        logger:   logger.New(),
    }

    resp, err := kvService.Put(ctx, &PutRequest{...})
    assert.NoError(t, err)
    mockRaftNode.AssertExpectations(t)
}
```

### Observability

Unified metrics, logging, and tracing across all transports:

- Request rates and latencies per method
- Error rates by type and method
- Authentication success/failure rates
- Cache hit rates
- Backend operation metrics

## Implementation

### Service Builder Pattern

```go
type ServiceBuilder struct {
    config        *ServiceConfig
    raftNode      RaftNodeAPI
    storage       StorageAPI
    indexManager  IndexManagerAPI
    cacheManager  CacheManagerAPI
    authManager   AuthManagerAPI
    backupManager BackupManagerAPI
}

func NewServiceBuilder() *ServiceBuilder {
    return &ServiceBuilder{}
}

func (b *ServiceBuilder) WithRaftNode(node RaftNodeAPI) *ServiceBuilder {
    b.raftNode = node
    return b
}

func (b *ServiceBuilder) Build() (QuicBaaSService, error) {
    // Validate required components
    if b.raftNode == nil {
        return nil, errors.New("raftNode is required")
    }

    // Create service implementation
    return &serviceImpl{
        raftNode:     b.raftNode,
        storage:      b.storage,
        indexManager: b.indexManager,
        // ... initialize sub-services
    }, nil
}
```

### Context-Based Metadata

Use `context.Context` for timeouts, cancellation, and metadata propagation:

```go
func (s *kvServiceImpl) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
    // Extract user info from context
    userInfo := UserInfoFromContext(ctx)

    // Check timeout
    if deadline, ok := ctx.Deadline(); ok {
        if time.Until(deadline) < 0 {
            return nil, ErrTimeout
        }
    }

    // Perform operation
    result, err := s.raftNode.ProposeWithMode(ctx, ...)
    return result, err
}
```

### Streaming with Channels

Stream large result sets efficiently:

```go
func (s *kvServiceImpl) List(ctx context.Context, req *ListRequest, ch chan<- *ListResponse) error {
    defer close(ch)

    iterator := s.storage.NewIterator(...)
    defer iterator.Close()

    for iterator.Next() {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case ch <- &ListResponse{Data: iterator.Value()}:
        }
    }

    return nil
}
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Interface composition | Type safety, testability, extensibility |
| Context-based metadata | Standard Go pattern, works with tracing |
| Channel-based streaming | Backpressure, memory efficiency, cancellation |
| Builder pattern | Flexible configuration, validation |
| Separate sub-services | Separation of concerns, maintainability |
| Auth middleware | Consistent enforcement across all operations |
| Service-level metrics | Unified observability across all transports |
| Error translation layer | Consistent error handling and status codes |

## Testing Strategy

### Unit Tests

Mock backend components for each sub-service:

```go
func TestClusterService_AddNode(t *testing.T) {
    mockRaft := &MockRaftNode{}
    mockRaft.On("AddNode", uint64(1), uint64(2), "addr").Return(nil)

    service := &clusterServiceImpl{raftNode: mockRaft}
    resp, err := service.AddNode(ctx, &AddNodeRequest{...})

    assert.NoError(t, err)
    mockRaft.AssertExpectations(t)
}
```

### Integration Tests

Use real backend components:

```go
func TestServiceIntegration(t *testing.T) {
    raftNode := node.NewNode(raftConfig)
    storage := pebble.NewStorage(storageConfig)

    service, _ := NewServiceBuilder().
        WithRaftNode(raftNode).
        WithStorage(storage).
        Build()

    putResp, _ := service.Put(ctx, &PutRequest{...})
    getResp, _ := service.Get(ctx, &GetRequest{...})

    assert.Equal(t, putResp.Data, getResp.Data)
}
```

### End-to-End Tests

Test through API transports:

```go
func TestE2E_gRPC(t *testing.T) {
    server := startTestServer(t)
    defer server.Stop()

    client := pb.NewQuicBaaSClient(conn)
    putResp, _ := client.Put(ctx, &pb.PutRequest{...})
    getResp, _ := client.Get(ctx, &pb.GetRequest{...})

    assert.Equal(t, putResp.Data, getResp.Data)
}
```

## Performance Considerations

- **Caching** - Service layer caches frequently accessed data
- **Batching** - Batch operations reduce Raft overhead by 5-10x
- **Streaming** - Prevents memory issues with large result sets
- **Connection Pooling** - Backend connections reused across requests
- **Metrics Overhead** - Metrics add <1ms per request
- **Auth Overhead** - JWT validation adds ~0.5ms per request

## References

### Implementation
- **Service Interface** - `pkg/service/service.go`
- **Service Facade** - `pkg/service/quicbaas.go`
- **Vector Service** - `pkg/service/vector.go`
- **qrdb Client Provider** - `pkg/server/sharded_client.go`
- **Storage Adapter** - `pkg/storage/qrdb/adapter.go`
- **TimeSeries Adapter** - `pkg/storage/qrdb/timeseries_adapter.go`
- **Vector Adapter** - `pkg/server/vector_adapter.go`

### API Integration
- **gRPC Server** - `pkg/api/grpc/server.go`
- **REST Server** - `pkg/api/rest/server.go`
- **QUIC client transport** - `sdk/go/transport/quic/transport.go` (no server; see [architecture.md](architecture.md#quic-sdk-client-only))
- **MCP Server** - `pkg/api/mcp/server.go`

---

**Last Updated:** 2026-02-26
**Version:** 0.4.0


---

## Binary Protocol

### Overview

QuicBaaS uses a high-performance binary protocol for internal state machine operations, achieving 8.7x faster encoding/decoding compared to JSON with 77% fewer allocations. The system maintains a hybrid architecture that accepts both JSON and binary from external clients while always using binary encoding internally.

### Protocol Format

#### Single Operations

**PUT Operation**:
```
[0x01][keyLen:uint16][key bytes][valueLen:uint32][value bytes]
```

**DELETE Operation**:
```
[0x02][keyLen:uint16][key bytes]
```

#### Batch Operations

**BATCH Format**:
```
[0x03][op1][op2]...[opN]
```

Each operation within a batch is a complete PUT or DELETE operation including its opcode.

#### Format Example

PUT operation for key="user:123", value="John Doe":
```
01 00 08 75 73 65 72 3A 31 32 33 00 00 00 08 4A 6F 68 6E 20 44 6F 65
^  ^---- ^---------------------- ^---------- ^-----------------------
|  len   key                     len         value
op
```

### Architecture

```
┌─────────────────────────────────────────────────────┐
│                  External Clients                    │
│         (Can use JSON or Binary protocol)            │
└─────────────────────────────────────────────────────┘
                          │
                ┌─────────▼──────────┐
                │  Protocol Header   │
                │ x-quicbaas-protocol│
                │   (json|binary)    │
                └─────────┬──────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│              gRPC Server (Conversion Layer)        │
│  • Accepts JSON or Binary from clients             │
│  • ALWAYS converts to Binary for FastStateMachine  │
│  • Key format: "entityType:id"                     │
└─────────────────────────┬─────────────────────────┘
                          │
                ┌─────────▼──────────┐
                │  Binary Protocol   │
                │ [op][keyLen][key]  │
                │ [valueLen][value]  │
                └─────────┬──────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│           Raft Consensus (QuicRaft)              │
│  • WAL durability                                  │
│  • Leader election                                 │
│  • Log replication                                 │
└─────────────────────────┬─────────────────────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│         FastStateMachine (Binary Only)             │
│  • 256-way cache sharding                          │
│  • Zero-copy string operations                     │
│  • Async PebbleDB writes                           │
│  • Binary protocol parser                          │
└─────────────────────────┬─────────────────────────┘
                          │
┌─────────────────────────▼─────────────────────────┐
│              PebbleDB Storage                      │
│  • LSM-tree storage engine                         │
│  • Durable persistence                             │
└────────────────────────────────────────────────────┘
```

### Implementation

#### Encoding Functions

Location: `pkg/api/grpc/optimized_encoding.go`

```go
// Core encoding functions
encodePutBinary(key, value []byte) []byte
encodeDeleteBinary(key []byte) []byte
encodeBatchBinary(operations [][]byte) []byte
```

#### Batch Encoding

The batch encoding format matches FastStateMachine's expectations:

```go
func encodeBatchBinary(operations [][]byte) []byte {
    // Calculate total size: 1 byte for batch opcode + all operations
    totalSize := 1 // 1 byte for opBatch (0x03)
    for _, op := range operations {
        totalSize += len(op)
    }

    buf := make([]byte, totalSize)
    buf[0] = 0x03 // opBatch

    // Write each operation (keeping their opcodes: 0x01 or 0x02)
    offset := 1
    for _, op := range operations {
        copy(buf[offset:], op)
        offset += len(op)
    }

    return buf
}
```

#### State Machine Integration

The FastStateMachine (managed by the qrdb engine) handles all binary operations:

```go
// Binary operations supported
case opPut:    return f.handlePut(data[1:])
case opDelete: return f.handleDelete(data[1:])
case opBatch:  return f.handleBatch(data[1:])
```

#### Server Integration

Location: `pkg/api/grpc/server.go`

Operations using binary protocol:
- Put (lines 260-270)
- Delete (lines 447-455)
- BatchPut (lines 936-958)
- BatchDelete (lines 1126-1171)
- Transaction PUT (executeProtoPutOp, lines 1429-1448)
- Transaction DELETE (executeProtoDeleteOp, lines 1450-1469)

### Performance Results

#### Benchmark Configuration

- Duration: 30 seconds
- Workers: 50 concurrent workers
- Value Size: 100 bytes per operation
- Protocol: Binary
- Write Mode: WAL (Write-Ahead Log)
- Connection Pool: 20 connections

#### Results

| Metric | Value |
|--------|-------|
| Total Operations | 475,972 |
| Successful Operations | 475,922 (99.99%) |
| Throughput | 15,865 ops/sec |
| Average Latency | 3.15 ms |
| P50 Latency | 2.52 ms |
| P95 Latency | 6.81 ms |
| P99 Latency | 11.64 ms |
| Failures | 50 (0.01%) |

#### Performance Improvements

| Implementation | Throughput | Improvement |
|----------------|-----------|-------------|
| MinimalStateMachine (JSON) | ~280 ops/sec | Baseline |
| FastStateMachine (Binary) | 15,865 ops/sec | 56.6x faster |

### Key Optimizations

#### Binary Protocol Benefits

- 77% fewer allocations vs JSON
- 8.7x faster encoding/decoding
- 65% smaller message size

#### FastStateMachine Features

1. Cache Sharding
   - 256 independent cache shards
   - Reduces lock contention by 256x
   - Parallel access across shards

2. Zero-Copy Operations
   - Uses `unsafe.Pointer` for []byte to string conversion
   - Eliminates memory allocations
   - Reduces GC pressure

3. Async PebbleDB Writes
   - Non-blocking state machine updates
   - Raft WAL provides durability guarantee
   - Higher throughput, lower latency

### Stability and Reliability

Test Results:
- Zero crashes during sustained load testing
- No panics with binary protocol
- 99.99% success rate
- Stable throughput maintained throughout tests
- Zero data loss - all successful operations persisted

### Production Readiness

Completed Features:
- Binary protocol implementation across all operations
- FastStateMachine integration complete
- Hybrid protocol architecture (supports both JSON and binary externally)
- Backwards compatibility maintained
- 15,865 ops/sec throughput achieved
- Sub-12ms P99 latency
- 99.99% success rate
- No crashes during testing

Status: Production-ready for single-node deployments. Multi-node cluster testing recommended before production use.

### Technical Notes

#### Critical Fix: Batch Format Alignment

Initial implementation used incorrect batch format:
```go
// INCORRECT:
[numOps:4bytes][op1][op2]...[opN]
```

FastStateMachine expected:
```go
// CORRECT:
[0x03][0x01][keyLen][key][valueLen][value][0x02][keyLen][key]...
```

The `encodeBatchBinary()` function was updated to match FastStateMachine's expectations by including operation opcodes within the batch payload.

#### Protocol Negotiation

The system supports protocol negotiation via the `x-quicbaas-protocol` header:
- Clients can request `json` or `binary` protocol
- Server always converts to binary for FastStateMachine
- Responses can be optimized based on client's preferred protocol
- Default is JSON for backwards compatibility

### References

Implementation files:
- `pkg/api/grpc/optimized_encoding.go` - Binary encoding functions
- `pkg/api/grpc/server.go` - Server-side protocol handling
- FastStateMachine binary parser is managed by the qrdb engine
- `pkg/server/lifecycle.go` - Server initialization


---

## Build Tags Reference

QuicBaaS uses Go build tags for modular compilation. This allows you to customize the binary size, attack surface, and feature set for your deployment.

### Quick Reference

| Tag | Default | Description |
|-----|---------|-------------|
| `raft` | ON | Distributed consensus and clustering |
| `timeseries` | ON | Time series metrics storage (paired with qrdb's `qrdb_ts` engine tag) |
| `vector` | ON | Vector database (HNSW indexing; paired with qrdb's `qrdb_vector` engine tag) |
| `rag` | ON | Retrieval Augmented Generation (requires vector; paired with `qrdb_vector`) |
| `rest` | ON | REST/HTTP API server |
| `quic` | ON | QUIC protocol support |
| `mcp` | ON | Model Context Protocol (AI agent tools) |
| `unix` | OFF | Unix socket server |
| `otel` | OFF | OpenTelemetry tracing |
| `l2_redis` | OFF | Redis L2 cache backend |
| `fips` | OFF | FIPS 140-3 compliant cryptography |

#### KMS Backend Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `awskms` | OFF | AWS KMS integration |
| `gcpkms` | OFF | GCP Cloud KMS integration |
| `azurekv` | OFF | Azure Key Vault integration |
| `vault` | OFF | HashiCorp Vault integration |
| `pkcs11` | OFF | PKCS#11 HSM support (requires CGO) |
| `quantum` | OFF | Quantum-safe cryptography (ML-DSA, ML-KEM) |

#### Codec Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `codec_json` | ON | JSON codec support |
| `codec_yaml` | ON | YAML codec support |
| `codec_toml` | ON | TOML codec support |
| `codec_msgpack` | ON | MessagePack codec support |
| `codec_bson` | ON | BSON codec support |
| `codec_cbor` | ON | CBOR codec support |
| `codec_protobuf` | ON | Protobuf codec support |

#### Object Storage Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `awss3` | OFF | AWS S3 object storage |
| `gcpstorage` | OFF | GCP Cloud Storage |
| `azureblob` | OFF | Azure Blob Storage |
| `minio` | OFF | MinIO object storage |

#### Test Tags

| Tag | Description |
|-----|-------------|
| `integration` | Integration tests (run in Docker) |
| `benchmark` | Performance benchmark tests |

### IAM Is Always Compiled

QuicBaaS is the central identity broker for the stack. The IAM subsystem
(ABAC, multi-tenant identity, OIDC/OAuth2, federation, GDPR, audit) is always
compiled into every binary; there is no `iam` build tag and a no-IAM artifact
is not supported. The IAM CLI surface (`quicbaas iam tenant|user|group|role|
app|policy|mfa|session|pep|audit|admin|gdpr|provider|identity`) is always
available.

### Excluded Commands by Tag

#### Without `timeseries` tag (~7 commands excluded)

- `quicbaas timeseries write` - Write single data point
- `quicbaas timeseries write-batch` - Write batch data points
- `quicbaas timeseries query` - Query time series data
- `quicbaas timeseries delete` - Delete time series data
- `quicbaas timeseries get-retention` - Get retention policy
- `quicbaas timeseries set-retention` - Set retention policy
- `quicbaas timeseries list-metrics` - List available metrics

### Build Examples

#### Using Makefile (Recommended)

```bash
# Full-featured build (default)
make build

# Build without specific features
make build WITH_TIMESERIES=0       # Exclude time series
make build WITH_VECTOR=0 WITH_RAG=0  # Exclude vector/RAG

# Pre-configured build profiles
make build-minimal   # Core + gRPC only
make build-embedded  # Single-node, gRPC only
make build-standard  # Cluster, no AI features
make build-cluster   # All features, no cloud KMS
make build-enterprise  # All features + cloud KMS
make build-full      # Everything including HSM, quantum

# FIPS 140-3 compliant build
make build-fips
```

#### Using go build directly

```bash
# Full-featured build (IAM is always compiled)
go build -tags "raft timeseries vector rag rest quic mcp codec_json" ./cmd/quicbaas

# Minimal core (KV + gRPC only; IAM still compiled)
go build ./cmd/quicbaas

# Without time series
go build -tags "raft vector rag rest quic mcp codec_json" ./cmd/quicbaas

# With cloud KMS backends
go build -tags "raft vector rag rest quic mcp awskms gcpkms azurekv" ./cmd/quicbaas

# With FIPS mode
GOFIPS140=latest go build -tags "fips raft rest quic mcp codec_json" ./cmd/quicbaas
```

### Build Profiles Matrix

| Profile | raft | ts | vector | rag | rest | quic | mcp | Cloud KMS |
|---------|------|----|--------|-----|------|------|-----|-----------|
| `minimal` | - | - | - | - | - | - | - | - |
| `embedded` | - | - | - | - | - | - | - | - |
| `standard` | Y | Y | - | - | Y | Y | - | - |
| `cluster` | Y | Y | Y | Y | Y | Y | Y | - |
| `enterprise` | Y | Y | Y | Y | Y | Y | Y | Y |
| `full` | Y | Y | Y | Y | Y | Y | Y | Y + HSM |

Legend: Y = included, - = excluded, ts = timeseries. IAM is always compiled.

### Makefile Variables

Control features via environment or make arguments:

| Variable | Default | Description |
|----------|---------|-------------|
| `WITH_RAFT` | 1 | Include Raft clustering |
| `WITH_TIMESERIES` | 1 | Include time series |
| `WITH_VECTOR` | 1 | Include vector database |
| `WITH_RAG` | 1 | Include RAG (requires VECTOR) |
| `WITH_REST` | 1 | Include REST API |
| `WITH_QUIC` | 1 | Include QUIC protocol |
| `WITH_MCP` | 1 | Include MCP protocol |
| `WITH_L2_REDIS` | 0 | Include Redis L2 cache |
| `WITH_AWS` | 0 | Include AWS KMS/S3 |
| `WITH_GCP` | 0 | Include GCP KMS/GCS |
| `WITH_AZURE` | 0 | Include Azure KV/Blob |
| `WITH_VAULT` | 0 | Include HashiCorp Vault |
| `WITH_PKCS11` | 0 | Include PKCS#11 (CGO) |
| `WITH_QUANTUM` | 0 | Include quantum-safe crypto |

#### Codec Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WITH_CODEC_JSON` | 1 | JSON codec |
| `WITH_CODEC_YAML` | 1 | YAML codec |
| `WITH_CODEC_TOML` | 1 | TOML codec |
| `WITH_CODEC_MSGPACK` | 1 | MessagePack codec |
| `WITH_CODEC_BSON` | 1 | BSON codec |
| `WITH_CODEC_CBOR` | 1 | CBOR codec |
| `WITH_CODEC_PROTOBUF` | 1 | Protobuf codec |

### File Organization

Build tags use conditional compilation with stub files:

```
cmd/quicbaas/
  timeseries.go           # //go:build timeseries
  timeseries_stub.go      # //go:build !timeseries
  tracing.go              # //go:build otel
  tracing_no_otel.go      # //go:build !otel
```

Stub files provide placeholder commands that inform users an optional feature
is disabled. IAM has no stub: it is always compiled, so `quicbaas iam ...` is
always present.

### Dependencies

Some build tags have dependencies:

| Tag | Requires |
|-----|----------|
| `rag` | `vector` |
| `pkcs11` | CGO_ENABLED=1 |
| `awskms` | AWS credentials |
| `gcpkms` | GCP credentials |
| `azurekv` | Azure credentials |

The Makefile validates dependencies at build time:

```makefile
ifeq ($(WITH_RAG),1)
  ifneq ($(WITH_VECTOR),1)
    $(error WITH_RAG=1 requires WITH_VECTOR=1)
  endif
endif
```

### Binary Size Impact

Approximate binary size by profile (Linux amd64):

| Profile | Size | Reduction |
|---------|------|-----------|
| full | ~95 MB | baseline |
| enterprise | ~85 MB | ~10% |
| cluster | ~75 MB | ~20% |
| standard | ~55 MB | ~40% |
| embedded | ~45 MB | ~50% |
| minimal | ~35 MB | ~60% |

Use `-ldflags="-s -w"` for additional ~20% reduction by stripping debug info:

```bash
go build -ldflags="-s -w" -tags "raft rest" ./cmd/quicbaas
```

### Testing with Build Tags

Run tests for specific features:

```bash
# Run IAM tests only (no special tag needed; IAM is always compiled)
go test ./pkg/iam/... ./cmd/quicbaas/... -run IAM

# Run timeseries tests
go test -tags=timeseries ./cmd/quicbaas/... -run TimeSeries

# Run integration tests (requires Docker)
make integration-test

# Run benchmarks
make bench-cmd
```
