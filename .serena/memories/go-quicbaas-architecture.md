# go-quicbaas Architecture Deep Dive

## Overview
go-quicbaas is a high-performance, multi-faceted distributed key-value database built on multi-Raft consensus and PebbleDB storage. The architecture emphasizes clean abstraction layers, pluggable backends, and extensible service composition.

## 1. pkg/messaging/ - Notification/Pub-Sub System

### Structure
- **Service Interface** (`messaging.Service`): Core abstraction for push notifications and event streaming
- **Channel Operations**: Create, delete, get, list channels
- **Subscription Model**: Subscribe/unsubscribe to channels with filters
- **Publishing**: Publish messages and delta payloads
- **Watch Integration**: Bridges KV watch events to messaging channels

### Key Types
- `Channel`: Represents a notification channel with metadata (name, codec, subscribers, buffer size, TTL, persistence)
- `Subscription`: Client subscription to a channel
- `Service`: Interface with 13 core methods for channel/subscription/publish operations
- `PushService`: Concrete implementation with:
  - Client management (subscriptions map)
  - Channel storage and dedup
  - Delta computation
  - Message publishing with dedup window
  - Watch bridge integration

### Extension Points
- **Codecs**: Supports JSON, Msgpack, Protobuf via `CodecResolver`
- **Storage**: Pluggable storage backend for persistence
- **Dedup Window**: Configurable duplicate detection
- **Delta Computation**: Field-level change tracking

### Integration Pattern
- Service is composed into main `QuicBaaS` interface
- Accessed via `service.Messaging()` 
- Exposed through REST handlers, gRPC, QUIC, and MCP APIs

---

## 2. pkg/service/ - Unified Service Facade Pattern

### Architecture
```
API Transports (REST, gRPC, QUIC, MCP, WebSocket)
         ↓
    Service Layer (Interface: QuicBaaS)
         ↓
Sub-Services (KV, Batch, Transaction, Query, etc.)
         ↓
Backend Components (Storage, Cache, Auth, etc.)
```

### Main Interface: QuicBaaS
The core abstraction defines 13 sub-service groups:
- **KVService**: Put, Get, Delete, List, Watch operations
- **BatchService**: Batch operations (Put, Get, Delete)
- **TransactionService**: ACID transactions with compare-and-swap
- **QueryService**: Index queries and range scans
- **ClusterService**: Cluster status, rebalancing, node management
- **BackupService**: Backup/restore with snapshots
- **AuthService**: Login, token management, validation
- **RBACService**: User/role management, assignments
- **CacheService**: Cache stats, config, clearing
- **HealthService**: Health, liveness, readiness probes
- **TimeSeriesService**: Aggregations, retention, querying
- **OperationsService**: Data migration, node replacement

### Specialized Services
- `KMSService`: Key management (create, rotate, export, encrypt/decrypt)
- `SecretsService`: Secret storage with encryption
- `CryptoService`: Cryptographic operations
- `VectorService`: Vector/similarity search (HNSW index)
- `EmbeddingService`: ML embeddings via Ollama
- `OllamaService`: LLM integration
- `CertificateService`: Certificate issuance and validation
- `DynamicSecretsService`: Temporary credentials
- `ParameterStoreService`: Configuration storage
- `ObjectStorageService`: S3-compatible object storage
- `ParameterStoreService`: Parameter storage and retrieval

### Concrete Implementation: quicBaaS
Located in `service/quicbaas.go`, orchestrates:
- Sub-services composition
- Cross-cutting concerns (auth, metrics, tracing)
- Backend component delegation
- Messaging integration
- QRDB client provider
- Watch manager

### Dependency Injection Pattern
**ServiceBuilder** uses method chaining:
```go
NewServiceBuilder().
  WithStorage(storage).
  WithCacheManager(cache).
  WithAuthManager(auth).
  WithMessagingService(messaging).
  // ... more components
  Build()
```

All 23 With* methods return builder for chaining. Build() creates the fully initialized QuicBaaS.

---

## 3. pkg/server/ - Multi-Protocol Orchestrator

### Server Structure
Central `Server` type manages lifecycle and initialization of all components:

**Core Components**:
- Raft Node (multi-Raft consensus)
- Storage backends (PebbleDB, file, in-memory)
- KMS Manager
- Secrets Manager
- Vector Service
- Embedding Manager
- Cache Manager
- Backup Manager
- Watch Bridge

**Protocol Servers** (conditional compilation):
- **gRPC** (always enabled): `grpcAPI`, `grpcServer`, `grpcListener`
- **REST** (WITH_REST): `restServer`
- **QUIC** (WITH_QUIC): `quicServer`
- **MCP** (WITH_MCP): `mcpServer`
- **WebSocket**: Integrated in REST
- **Unix Domain Sockets**: UNIX socket gRPC and REST endpoints

### Lifecycle Management
Located in `lifecycle.go` and conditional compilation files:

**Startup Order**:
1. Load config
2. Initialize storage backends
3. Setup Raft consensus
4. Start KMS system
5. Start secrets manager
6. Create service facade
7. Initialize protocol servers (REST, QUIC, MCP, gRPC)
8. Start health check system
9. Register with discovery (if enabled)

**Shutdown Order** (reverse):
1. Signal shutdown
2. Gracefully stop REST server
3. Gracefully stop QUIC server
4. Stop gRPC with grace period
5. Stop discovery
6. Close service
7. Close storage
8. Cleanup resources

### Conditional Compilation (Build Tags)
**Feature Gates** (Makefile-driven):
```makefile
WITH_REST=1        # REST/HTTP API (includes WebSocket)
WITH_QUIC=1        # QUIC protocol
WITH_MCP=1         # Model Context Protocol
WITH_IAM=1         # Advanced authorization
WITH_TIMESERIES=1  # Time series database
WITH_VECTOR=0      # Vector/embedding search (disabled by default)
WITH_RAG=0         # Retrieval-Augmented Generation (requires VECTOR)
WITH_MQTT=1        # MQTT over WebSocket
WITH_AMQP=1        # AMQP over WebSocket
WITH_STOMP=1       # STOMP over WebSocket
```

**Build Tag Implementation**:
- Files have `//go:build rest` directive (Go 1.16+)
- Multiple lifecycle_*.go files: one per feature
- E.g., `lifecycle_rest.go` contains `startRESTServer()`, compiled only with `-tags rest`
- Stub files provide no-op implementations when feature disabled
- Example: `lifecycle_rest_stub.go` returns nil for startRESTServer() when REST not enabled

### Seam Injection Pattern (Testing)
Package-level functions injected for testability:
```go
var (
  newREST = restapi.NewServer
  newGRPCAPI = grpcapi.NewServer
  listenFunc = net.Listen
  // ... etc for each component
)
```

---

## 4. pkg/api/ - Multi-Transport Protocol Handlers

### Transport Layers
Located in subdirectories:

#### REST API (`pkg/api/rest/`)
- `Server`: HTTP/2 capable server
- Handlers for all operations (KV, KMS, Secrets, etc.)
- Router setup with chi framework
- Middleware: auth, authorization, metrics, tracing
- WebSocket upgrade handler
- Response serialization with codec support
- TLS configuration (HTTP/2 requires specific setup)

#### gRPC API (`pkg/api/grpc/`)
- Protobuf service definition
- Unary interceptors for cross-cutting concerns
- Stream handlers for batch operations
- Connection pooling and session management
- Type converters from service types to protobuf
- Specialized handlers for each service domain
- Peercredentials for Unix socket auth

#### QUIC API (`pkg/api/quic/`)
- Binary protocol over QUIC streams
- Opcode-based dispatch
- Handler map pattern (not switch statements)
- Supports all service operations
- Connection management
- Message encoding/decoding

#### MCP API (`pkg/api/mcp/`)
- Model Context Protocol
- Tool-based interface for LLM integration
- Resources for file access
- Authorization layer
- Tool registry

#### WebSocket (`pkg/api/websocket/`)
- Base transport layer
- Subprotocol negotiation
- Session management
- Message multiplexing

**Subprotocol Handlers** (pluggable):
- **Echo**: Simple echo (always present, testing)
- **PubSub**: Generic pub/sub (always present)
- **MQTT** (WITH_MQTT): 3.1.1 and 5.0 support
- **AMQP** (WITH_AMQP): 0-9-1 support
- **STOMP** (WITH_STOMP): 1.2 support

### Subprotocol Registration Pattern
Located in `websocket/server.go`:

```go
type SubprotocolHandler interface {
  Name() string
  HandleConnection(session *Session) error
}

type Server struct {
  subprotocols map[string]SubprotocolHandler // Registry
  mu sync.RWMutex
}

func (s *Server) RegisterSubprotocol(handler SubprotocolHandler) {
  s.mu.Lock()
  s.subprotocols[handler.Name()] = handler
  s.mu.Unlock()
}

func (s *Server) selectSubprotocol(requested []string) string {
  // First match in requested list gets selected
}
```

**Key Design**:
- Subprotocols registered at startup
- Client requests via Sec-WebSocket-Protocol header
- Server selects first matching subprotocol
- Fall back to default "quicbaas" if none requested
- Each handler manages its own state/protocol

---

## 5. Configuration System (pkg/config/)

### Main Config Structure
Located in `config.go`:

```go
type Config struct {
  Server ServerConfig
  Raft RaftConfig
  Storage StorageConfig
  Security SecurityConfig
  Auth AuthConfig
  // ... 20+ other sections
}
```

**Server Config Sections**:
- `Mode`: Standalone, Cluster, or DevCluster
- `GRPC`, `REST`, `QUIC`, `MCP`, `Unix`: Server-specific configs
- `Shards`: Shard configuration
- `EntityShardPins`: Entity → shard mapping

**Feature Configs**:
- `Messaging`: Channel buffer, dedup window, watch bridge
- `Vector`: Index type, dimension, enabled flag
- `KMS`: Backend selection, key rotation
- `Backup`: Storage backend, compression, encryption
- `Ollama`: Model selection, embedding config

### Configuration Loading
`loader.go`: 
- YAML-based configuration
- Environment variable override support
- Validation of all fields
- Default value injection
- Deep merging of nested configs

### Watch Bridge Config
Special config for bridging KV watch events to messaging:
```go
type WatchBridgeConfig struct {
  Enabled bool
  LeaderOnly bool
  BufferSize int
  DefaultChannelPrefix string
  AutoCreateChannels bool
  Mappings []WatchBridgeMappingConfig
}
```

---

## 6. Plugin/Extension Model

### Backend Manager (Pluggable Storage)
Located in `catalog/backend_manager.go`:

```go
type BackendManager interface {
  GetBackend(name string) (Backend, error)
  ListBackends() []BackendConfig
  RegisterBackend(name string, cfg *BackendConfig, storage Backend) error
  UnregisterBackend(name string) error
  SelectBackend(criteria *SelectionCriteria) (Backend, error)
  CheckHealth(name string) (*BackendHealth, error)
}
```

**Backend Registry**:
- Runtime registration of storage backends
- Health checking
- Tier-based selection (local, regional, cloud)
- Failover and balancing

### Service Registration Pattern
All services follow interface-based composition:

1. **Interface Definition**: `SomethingService interface { Methods }`
2. **Concrete Impl**: `somethingService struct { fields }`
3. **Builder Methods**: `WithSomethingService(svc SomethingService) ServiceOption`
4. **Composition**: Service facade holds all service types
5. **Accessor**: `service.Something()` returns interface

### Example: VectorService
```go
type VectorService interface {
  Index(ctx context.Context, id string, vector []float32) error
  Search(ctx context.Context, vector []float32, k int) ([]Result, error)
  // ... more methods
}

type ServiceBuilder {
  vectorService VectorService
  // ...
}

func (b *ServiceBuilder) WithVectorService(svc VectorService) *ServiceBuilder {
  b.vectorService = svc
  return b
}

// In QuicBaaS implementation
func (qb *quicBaaS) Vector() VectorService {
  return qb.vectorService
}
```

---

## 7. Build System & Feature Flags

### Makefile Organization
Top-level `Makefile` drives the build:

**Feature Variables**:
```makefile
WITH_REST=1        # Default: enabled
WITH_QUIC=1        # Default: enabled
WITH_MCP=1         # Default: enabled
WITH_VECTOR=0      # Default: disabled (requires HNSW)
WITH_RAG=0         # Default: disabled (requires VECTOR)
WITH_MQTT=1        # Default: enabled
WITH_AMQP=1        # Default: enabled
WITH_STOMP=1       # Default: enabled
```

**Build Tag Accumulation**:
```makefile
BUILD_TAGS := hashicorpmetrics local
ifeq ($(WITH_REST),1)
  BUILD_TAGS += rest
endif
# ... for each feature
BUILD_TAGS_ARG := -tags '$(strip $(BUILD_TAGS))'
```

**Build Invocation**:
```bash
make build WITH_VECTOR=1 WITH_RAG=1
# Results in: -tags 'rest quic mcp iam timeseries vector rag ...'
```

### Per-Feature Compilation
Each feature has two files:

1. **Implementation File**: `lifecycle_feature.go` with `//go:build feature`
   - Contains actual initialization code
   - Example: `startRESTServer()`, `startQuicServer()`

2. **Stub File**: `lifecycle_feature_stub.go` without build tag
   - Called when feature not enabled
   - Returns nil/noop
   - Example: stub returns `nil` from `startRESTServer()`

**Pattern**:
```go
// lifecycle_rest.go
//go:build rest
func (s *Server) startRESTServer() error {
  // Real implementation
}

// lifecycle_rest_stub.go (no build tag)
// Falls back to generic startRESTServer
func (s *Server) startRESTServer() error {
  return nil // or provided default
}
```

---

## 8. Dependency Injection & Inversion of Control

### Seam Injection (Testing)
All external dependencies injected as package-level functions:

```go
var (
  listenFunc = net.Listen
  newGRPCAPI = grpcapi.NewServer
  newREST = restapi.NewServer
  // ... per component
)
```

Tests override these to inject mocks/stubs:
```go
func testSomething(t *testing.T) {
  oldNewREST := newREST
  defer func() { newREST = oldNewREST }()
  newREST = func(...) *restapi.Server {
    return &mockRESTServer{}
  }
  // ... test code
}
```

### Interface-Based Design
Every component exposed as interface:
- `interfaces.Storage`
- `interfaces.CacheManager`
- `interfaces.AuthManager`
- `interfaces.RaftNode`
- `interfaces.Logger`

Allows:
- Implementation swapping
- Mock creation
- Testing in isolation
- Loose coupling

### Service Builder Pattern
Central place for composition:

```go
type ServiceBuilder struct {
  // Holds all components
  raftNode interfaces.RaftNode
  storage interfaces.Storage
  cacheManager interfaces.CacheManager
  // ... 20+ components
}

// Methods return builder for chaining
func (b *ServiceBuilder) WithStorage(s interfaces.Storage) *ServiceBuilder {
  b.storage = s
  return b
}

// Final assembly
func (b *ServiceBuilder) Build() (*quicBaaS, error) {
  // Validate all required components present
  // Create sub-services
  // Compose into main facade
  // Return fully initialized service
}
```

---

## 9. Extension Points for Signal Server

### Adding Signal Protocol Support

#### Option 1: New Service via ServiceBuilder
Create `SignalService` interface in `service/`:
```go
type SignalService interface {
  Register(ctx context.Context, deviceID string) (*SignalDevice, error)
  CreateSession(ctx context.Context, deviceID string) (*Session, error)
  // ... Signal-specific operations
}
```

Add to QuicBaaS interface and ServiceBuilder.

#### Option 2: New Protocol Handler
Create QUIC subprotocol handler (like MQTT/AMQP):
```go
type SignalQUICHandler struct {
  service *quicserver.Server
  logger interfaces.Logger
}

func (h *SignalQUICHandler) Name() string {
  return "signal"
}

func (h *SignalQUICHandler) HandlePacket(opcode byte, payload []byte) ([]byte, error) {
  // Dispatch based on opcode
}
```

Register in server initialization.

#### Option 3: WebSocket Subprotocol
Implement `SubprotocolHandler` for Signal protocol:
```go
type SignalWSHandler struct {
  hub websocket.PubSubHub
}

func (h *SignalWSHandler) Name() string {
  return "signal"
}

func (h *SignalWSHandler) HandleConnection(session *websocket.Session) error {
  // Handle Signal WebSocket protocol
}
```

Register via `wsServer.RegisterSubprotocol(handler)`.

#### Option 4: REST Endpoints
Add Signal routes to REST handler:
- `POST /v1/signal/register` → SignalService.Register()
- `POST /v1/signal/session` → SignalService.CreateSession()
- etc.

#### Option 5: gRPC Service
Add Signal gRPC service:
- Define `.proto` file
- Generate code
- Implement gRPC server
- Register with gRPC multiplexer

### Key Integration Points
1. **Service Layer**: Implement SignalService, add to QuicBaaS
2. **Config**: Add SignalConfig section to Config struct
3. **Build Tags**: Add `with_signal` tag for optional compilation
4. **Lifecycle**: Add `startSignal()` in lifecycle
5. **Protocol Handler**: Expose via REST/gRPC/QUIC/WebSocket as needed
6. **Testing**: Use seam injection to mock dependencies

### Storage Integration
Signal entities stored in KV via:
```go
kvService.Put(ctx, "signal:device:"+deviceID, data)
kvService.Get(ctx, "signal:device:"+deviceID)
```

Or in specialized namespace with routing:
```go
// Dedicate shard to Signal
config.Server.EntityShardPins["signal:device"] = 4
```

---

## Key Design Principles

1. **Clean Architecture**: Clear separation between API transports, business logic (service layer), and storage
2. **Interface-Driven**: Everything exposed as interface for testability and extensibility
3. **Lock-Free Preferred**: Use atomic operations, channels, and message passing
4. **Service Facade Pattern**: Single point of entry (QuicBaaS interface) for all operations
5. **Feature Gates**: Compile-time feature selection via build tags, no runtime overhead
6. **Handler Maps**: Use map-based dispatch (O(1)) instead of switch statements
7. **Typed Errors**: All errors are typed, never fmt.Errorf
8. **Pluggable Everything**: Backends, services, protocols, storage - all pluggable
9. **Cross-Cutting Concerns**: Applied uniformly at service/transport boundaries
10. **Builder Pattern**: Use method chaining for complex initialization

---

## Directory Structure Summary

```
pkg/
├── service/           # Service facade + sub-services
├── api/
│   ├── rest/         # HTTP API handlers
│   ├── grpc/         # gRPC service implementation
│   ├── quic/         # QUIC protocol handler
│   ├── mcp/          # Model Context Protocol
│   ├── websocket/    # WebSocket base + session mgmt
│   └── websocket/subprotocol/  # MQTT, AMQP, STOMP, PubSub
├── messaging/        # Pub/sub and push notifications
├── server/           # Main server orchestrator + lifecycle
├── config/           # Configuration loading and validation
├── catalog/          # Backend manager, object catalog
├── storage/          # KV store interfaces
├── cache/            # Caching layer
├── kms/              # Key management
├── secrets/          # Secrets storage
├── crypto/           # Cryptographic operations
├── seal/             # Seal/unseal mechanism
├── watch/            # KV watch manager
├── watchbridge/      # Bridge watch events to messaging
├── interfaces/       # All service interfaces
├── logger/           # Logging
├── routing/          # Shard routing
└── errors/           # Error types
```

This architecture enables Signal protocol to be added as either a service (Option 1), a new transport protocol (Options 2-5), or both, with minimal changes to existing code.