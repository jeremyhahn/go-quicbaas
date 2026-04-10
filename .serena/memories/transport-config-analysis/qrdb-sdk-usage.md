# QRDB SDK Transport Configuration Analysis

## Current State Summary

The go-quicbaas project uses a **hardcoded embedded transport** for qrdb SDK client creation with no configurable transport switching mechanism. All qrdb SDK clients use the embedded transport exclusively.

---

## 1. Current QRDB SDK Usage

### Client Creation Pattern
- **File**: `pkg/server/sharded_client.go` (lines 40-105)
- **Type**: `ShardedClient` - manages per-shard qrdb SDK clients
- **Architecture**: Lazy per-shard client creation using `sync.Map`
- **Transport**: HARDCODED to embedded transport only

**Creation Code** (`pkg/server/sharded_client.go:89-97`):
```go
t, err := embedded.New(kvSvc, adminSvc, opts...)
if err != nil {
    return nil, err
}

client, err := qrdbsdk.NewClient(t)
if err != nil {
    return nil, err
}
```

### Transport Options Used
- **Only one**: `embedded.New()` from `github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded`
- NO configuration for other transports (gRPC, REST, QUIC, MCP)
- Hardcoded in per-shard client initialization

### Service Injection Point
- **File**: `pkg/server/lifecycle_qrdb.go` (lines 22-37)
- **Function**: `Server.createQRDBClient(host qrdbservice.Host)`
- **Build tag**: `//go:build raft` (only compiled in raft builds)
- **Stub**: `pkg/server/lifecycle_qrdb_stub.go` (returns nil for non-raft builds)
- **Vector Service Integration**: Vector service wrapper injected via `WithVectorServicer()` option

### Client Assignment
- **File**: `pkg/server/lifecycle.go` (lines 1239-1247)
- **Context**: Inside `startService()` after all storage/services initialized
- **Backend Check**: Backend must satisfy `qrdbservice.Host` interface (RaftNode, LocalBackend, MemoryBackend)
- **Service Builder**: Client passed via `builder.WithQRDBClient(s.qrdbClient)` (line 1365)

---

## 2. Viper/Config Structure

### Current Configuration
**File**: `pkg/config/config.go`

**Main Config Struct** (lines 48-71):
```go
type Config struct {
    Server        ServerConfig        // Contains protocol configs
    DevCluster    DevClusterConfig
    Raft          RaftConfig
    Storage       StorageConfig
    Security      SecurityConfig
    Auth          AuthConfig
    Backup        BackupConfig
    ObjectStorage ObjectStorageConfig
    Logging       LoggingConfig
    Discovery     DiscoveryConfig
    Cache         EnhancedCacheConfig
    Metrics       MetricsConfig
    Codec         CodecConfig
    KMS           KMSConfig
    Secrets       SecretsConfig
    Vector        VectorConfig
    Ollama        OllamaConfig
    RAG           RAGConfig
    Scheduler     SchedulerConfig
    Messaging     MessagingConfig
    Seal          SealConfig
    CA            CAConfig
}
```

**ServerConfig Struct** (lines 324+):
```go
type ServerConfig struct {
    Mode              string                  // "standalone", "cluster", "devcluster"
    Host              string
    ExposeShardHeader bool
    Embedded          bool                    // Embedded mode flag
    
    // Protocol-specific configurations
    GRPC ServerGRPCConfig
    REST ServerRESTConfig
    QUIC ServerQUICConfig
    MCP  ServerMCPConfig
    Unix ServerUnixConfig
}
```

**CRITICAL**: NO `Transport` field exists in config for QRDB SDK transport selection.

### Configuration Loading
- **Viper-based**: `mapstructure` tags for YAML/env var loading
- **No qrdb transport config**: The qrdb SDK transport is not configurable
- **Embedded mode exists**: But only controls network listeners, not internal SDK transport

---

## 3. Server Lifecycle Wiring

### Initialization Order (in `Server.initialize()` - lines 267-447)
1. **Storage initialization** (line 290)
2. **Vector/TimeSeries storage** (lines 299-309, pre-Raft)
3. **State machine registry** (line 319, cluster mode only)
4. **Raft Node startup** (lines 326, 340)
5. **Index initialization** (line 349)
6. **KMS/Secrets** (lines 354-361)
7. **Vector/TimeSeries services** (lines 364-371)
8. **Service creation** (line 394) - **QRDB client created here**
9. **API servers** (lines 405-419)

### QRDB Client Creation (in `Server.startService()` - lines 1236-1247)
```
1. Get backend (RaftNode/LocalBackend/MemoryBackend)
2. Check if backend implements qrdbservice.Host
3. Call s.createQRDBClient(host) 
4. Assign to s.qrdbClient
5. Pass to service builder via WithQRDBClient()
```

**File Organization**:
- Raft build: `lifecycle_qrdb.go` (actual implementation)
- Non-raft build: `lifecycle_qrdb_stub.go` (returns nil)

---

## 4. QRDB SDK Transport Support

### Current Implementation (Embedded-Only)
The qrdb SDK currently uses **only the embedded transport** in quicbaas:
- Import: `"github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"`
- Per-shard execution: Creates service wrappers + embedded transport per shard
- No external transport configuration needed or available

### Where Transport is Used
1. **Vector Service**: Uses SDK client via embedded transport
   - File: `pkg/service/vector.go`
   - Pattern: `client.Vector().CreateNamespace(ctx, req)`

2. **Vector Write Delegation**:
   - Writes delegated via SDK to ensure Raft consensus
   - Falls back to direct `proposeToRaft` if SDK unavailable

3. **Service Layer**:
   - File: `pkg/service/quicbaas.go`
   - Type: `QRDBClientProvider` interface (lines 118-123)
   - Used by sub-services needing type-safe KV operations

---

## What Needs to Change for Configurable Transport Switching

### 1. Configuration Addition
- Add `Transport` field to ServerConfig or new QRDBConfig
- Options: "embedded" (default), "grpc", "rest", "quic", "mcp"
- Load from YAML/env via Viper

### 2. Transport Factory Pattern
- Create transport factory in `pkg/server/`
- Factory reads config and instantiates appropriate transport
- Support signature: `NewTransport(cfg TransportConfig) (Transport, error)`

### 3. ShardedClient Modification
- Accept `Transport` as dependency injection
- Use factory transport instead of hardcoded `embedded.New()`
- Per-shard client creation: `client, err := qrdbsdk.NewClient(t)`

### 4. Lifecycle Wiring
- Move transport instantiation to early lifecycle (before services)
- Store transport config in Server struct
- Pass transport through builder pattern
- Handle transport-specific authentication/TLS

### 5. Service Integration
- Vector service needs transport-aware client lookup
- Other delegating services (DAO factory) need same treatment
- Graceful fallback for network-based transports

### 6. Testing Implications
- Test all transport variants
- Mock transport implementations for unit tests
- E2E tests with real network transports

---

## Key Files to Modify

1. **pkg/config/config.go** - Add transport config
2. **pkg/server/sharded_client.go** - Accept transport dependency
3. **pkg/server/lifecycle_qrdb.go** - Create transport from config
4. **pkg/server/server.go** - Store transport config
5. **pkg/service/quicbaas.go** - May need transport awareness in builder

---

## Build Tags Consideration

- `//go:build raft` controls ShardedClient compilation
- Transport switching requires Raft (Host interface)
- Stub files maintain non-raft compatibility
