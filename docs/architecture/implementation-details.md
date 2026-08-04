# Implementation Details

Detailed technical information about QuicBaaS's internal architecture and implementation.

## Component Details

### API Layer

#### gRPC Server (`pkg/api/grpc`)

High-performance binary protocol access.

Key files: `server.go`, `handlers.go`, `interceptors.go`

Implementation:
```go
type Server struct {
    proto.UnimplementedQuicBaaSServer
    node      *raft.Node
    router    *Router
    logger    *logger.Logger
    metrics   *metrics.Collector
}

// Put implements the Put RPC
func (s *Server) Put(ctx context.Context, req *proto.PutRequest) (*proto.PutResponse, error) {
    // 1. Validate request
    if err := s.validatePutRequest(req); err != nil {
        return nil, status.Error(codes.InvalidArgument, err.Error())
    }

    // 2. Route to appropriate Raft group
    shardID := s.router.Route(req.EntityType, req.Id)

    // 3. Propose to Raft
    result, err := s.node.Propose(ctx, shardID, req)
    if err != nil {
        return nil, status.Error(codes.Unavailable, err.Error())
    }

    // 4. Return response
    return result.(*proto.PutResponse), nil
}
```

Interceptors: authentication (API keys/mTLS), logging, metrics, panic recovery, and timeouts.

#### REST Server (`pkg/api/rest`)

HTTP/JSON access.

Key files: `server.go`, `handlers.go`, `swagger.go`, `middleware.go`

Implementation:
```go
type Server struct {
    node      *raft.Node
    router    *Router
    logger    *logger.Logger
    mux       *http.ServeMux
}

// handlePut handles PUT /api/v1/entities/{type}/{id}
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
    // 1. Parse request
    var req PutRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        s.writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }

    // 2. Convert to gRPC request
    grpcReq := &proto.PutRequest{
        EntityType:  req.EntityType,
        Id:          req.ID,
        Data:        req.Data,
        Consistency: req.Consistency,
    }

    // 3. Delegate to gRPC handler
    grpcResp, err := s.grpcServer.Put(r.Context(), grpcReq)
    if err != nil {
        s.writeGRPCError(w, err)
        return
    }

    // 4. Write JSON response
    s.writeJSON(w, http.StatusOK, grpcResp)
}
```

Middleware: CORS, authentication, rate limiting, gzip compression, and logging.

#### CLI Client (`pkg/cli`)

Command-line interface.

Key files: `client.go`, `formatter.go`, `interfaces.go`

Implementation:
```go
type Client interface {
    Put(ctx context.Context, req *PutRequest) (*PutResponse, error)
    Get(ctx context.Context, req *GetRequest) (*GetResponse, error)
    Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
    List(ctx context.Context, req *ListRequest) (*ListResponse, error)
    Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)
    Close() error
}

type grpcClient struct {
    conn   *grpc.ClientConn
    client proto.QuicBaaSClient
    logger *logger.Logger
}

// Put executes a Put request
func (c *grpcClient) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
    // Convert to proto request
    protoReq := &proto.PutRequest{
        EntityType:  req.EntityType,
        Id:          req.ID,
        Data:        req.Value,
        Consistency: req.Consistency,
    }

    // Call gRPC
    resp, err := c.client.Put(ctx, protoReq)
    if err != nil {
        return nil, err
    }

    // Convert response
    return &PutResponse{
        ID:        resp.Id,
        Version:   resp.Version,
        Timestamp: time.Unix(0, resp.Timestamp),
        LeaderID:  resp.LeaderId,
    }, nil
}
```

### Service Layer

#### Request Routing

Request routing, Raft consensus, and node management are handled by the qrdb database engine, accessed via the qrdb SDK (`github.com/jeremyhahn/go-qrdb/sdk/go`). QuicBaaS's service layer forwards operations to the qrdb SDK, which manages consistent hashing, shard routing, Raft proposals, and read consistency internally.

Key capabilities provided by the qrdb engine:
- **Consistent hashing** for balanced shard distribution
- **Raft proposals** with automatic leader forwarding
- **Local reads** for stale/cached data access
- **Quorum reads** for linearizable consistency
- **Automatic failover** and leader election

### Raft Configuration

Raft parameters are configured in the QuicBaaS YAML configuration and forwarded to the qrdb engine:

```yaml
raft:
  node_id: 1
  raft_address: "10.0.1.1:63001"
  election_rtt: 10
  heartbeat_rtt: 1
  snapshot_entries: 10000
  data_dir: /var/lib/quicbaas/raft
```

### State Machine Layer

State machines are managed by the qrdb engine. See [Sharding](sharding.md) for shard and state machine configuration.

### Storage Layer

See [Storage Engine](storage-engine.md) for detailed implementation.

## Data Structures

### Entity Format

Entities are stored as Protocol Buffer messages:

```protobuf
message Entity {
  uint64 id = 1;
  string entity_type = 2;
  bytes data = 3;
  uint64 version = 4;
  int64 timestamp = 5;
  map<string, string> metadata = 6;
}
```

### Storage Keys

**Primary Key:**
```
{entity_type}:{id} -> Entity
```

**Index Key:**
```
idx:{entity_type}:{index_name}:{value}:{id} -> Entity
```

**Time-Series Key:**
```
ts:{entity_type}:{timestamp}:{id} -> Entity
```

**Metadata Key:**
```
meta:{entity_type}:{id} -> Metadata
```

## Concurrency Model

### Raft Concurrency

Raft provides linearizability through:
1. Single leader per shard
2. Sequential log application
3. Quorum consensus

### Storage Concurrency

PebbleDB provides:
- Snapshot isolation for reads
- MVCC (Multi-Version Concurrency Control)
- Lock-free reads
- Optimistic concurrency control

### Application Concurrency

QuicBaaS uses:
- Read-Write mutexes for shared state
- Atomic operations for counters
- Channel-based synchronization
- Context-based cancellation

## Memory Management

### Buffer Pooling

```go
var (
    smallBufferPool = sync.Pool{
        New: func() interface{} {
            return make([]byte, 0, 4096)
        },
    }

    largeBufferPool = sync.Pool{
        New: func() interface{} {
            return make([]byte, 0, 65536)
        },
    }
)
```

### Connection Pooling

```go
type ConnectionPool struct {
    pool   chan *grpc.ClientConn
    size   int
    target string
}

func (p *ConnectionPool) Get() (*grpc.ClientConn, error) {
    select {
    case conn := <-p.pool:
        return conn, nil
    default:
        return grpc.Dial(p.target, grpc.WithInsecure())
    }
}

func (p *ConnectionPool) Put(conn *grpc.ClientConn) {
    select {
    case p.pool <- conn:
    default:
        conn.Close()
    }
}
```

## Error Handling

### Error Types

```go
type ErrorCode int

const (
    ErrNotFound ErrorCode = iota
    ErrConflict
    ErrTimeout
    ErrUnavailable
    ErrInternal
    ErrInvalidInput
    ErrConfiguration
)

// QRDBError is go-qrdb's canonical structured error, re-exported as QBError in pkg/errors.
type QRDBError struct {
    Code    ErrorCode
    Op      string
    Err     error
    Context map[string]interface{}
}

func (e *QRDBError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("%s: %v", e.Op, e.Err)
    }
    return e.Op
}

func (e *QRDBError) Unwrap() error {
    return e.Err
}
```

### Error Wrapping

```go
func Wrapf(err error, code ErrorCode, op string, format string, args ...interface{}) error {
    return &QRDBError{
        Code:    code,
        Op:      op,
        Err:     err,
        Context: map[string]interface{}{
            "message": fmt.Sprintf(format, args...),
        },
    }
}
```

## Logging Strategy

### Structured Logging

```go
logger.Info().
    Str("op", "Put").
    Str("entity_type", "user").
    Uint64("id", id).
    Dur("duration", duration).
    Msg("entity stored")
```

### Log Levels

- **DEBUG:** Detailed diagnostic information
- **INFO:** General operational information
- **WARN:** Warning messages (non-critical)
- **ERROR:** Error messages (operation failed)
- **FATAL:** Fatal errors (process exits)

### Log Context

Each log entry includes:
- Timestamp (ISO 8601)
- Log level
- Operation name
- Entity type and ID
- Duration (for operations)
- Error details (if applicable)

## Metrics Collection

### Prometheus Metrics

```go
var (
    requestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "quicbaas_requests_total",
            Help: "Total number of requests",
        },
        []string{"method", "status"},
    )

    requestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "quicbaas_request_duration_seconds",
            Help:    "Request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method"},
    )

    activeConnections = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "quicbaas_active_connections",
            Help: "Number of active connections",
        },
    )
)
```

### Custom Metrics

- **Raft Proposals:** Number of proposals submitted
- **Raft Commits:** Number of proposals committed
- **Storage Size:** Total storage size per shard
- **Cache Hit Rate:** Index cache hit percentage
- **Snapshot Duration:** Time to create/restore snapshots

## Configuration Management

### Configuration Loading

```go
type Config struct {
    Server   ServerConfig
    Raft     RaftConfig
    Storage  StorageConfig
    Security SecurityConfig
    Logging  LoggingConfig
}

func LoadConfig(path string) (*Config, error) {
    v := viper.New()
    v.SetConfigFile(path)
    v.SetEnvPrefix("QUICBAAS")
    v.AutomaticEnv()

    if err := v.ReadInConfig(); err != nil {
        return nil, err
    }

    var config Config
    if err := v.Unmarshal(&config); err != nil {
        return nil, err
    }

    return &config, nil
}
```

### Configuration Validation

```go
func (c *Config) Validate() error {
    if c.Server.Port < 1 || c.Server.Port > 65535 {
        return fmt.Errorf("invalid port: %d", c.Server.Port)
    }

    if c.Raft.NodeID == 0 {
        return fmt.Errorf("node ID cannot be zero")
    }

    if c.Raft.ElectionRTT < c.Raft.HeartbeatRTT {
        return fmt.Errorf("election RTT must be >= heartbeat RTT")
    }

    return nil
}
```

## Next Steps

- [Raft Consensus Details](raft-consensus.md)
- [Storage Engine Implementation](storage-engine.md)
- [State Machine Lifecycle](state-machines.md)

---

**Last Updated:** 2026-02-23
