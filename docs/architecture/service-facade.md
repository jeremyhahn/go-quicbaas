# Service Facade

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
- **QUIC Server** - `pkg/api/quic/server.go`
- **MCP Server** - `pkg/api/mcp/server.go`

---

**Last Updated:** 2026-02-26
**Version:** 0.4.0
