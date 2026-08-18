# Data Services — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/data-services/`.

This guide covers all configuration options available when creating a GenericDAO instance.

## Overview

DAO instances are configured using functional options passed to the constructor:

```go
userDAO, err := service.CreateDAO*User *User { return &User{} },
    dao.WithShard(1),
    dao.WithCodec(dao.CodecMsgPack),
    dao.WithTimeout(30*time.Second),
)
```

## Configuration Options

### WithShard

Sets the Raft shard ID for data storage in cluster mode.

```go
dao.WithShard(shardID uint64) Option
```

**Purpose**: In cluster mode, QuicBaaS partitions data across multiple Raft shards. This option specifies which shard stores the entity data.

**When to Use**:
- **Cluster Mode**: Required for multi-node deployments to control data placement
- **Local Mode**: Ignored (single PebbleDB instance)
- **Memory Mode**: Ignored (in-memory storage)

**Default**: Shard 1

**Examples**:

```go
// Store users in shard 1
userDAO, err := service.CreateDAO*User,
)

// Store sessions in shard 2 (data partitioning)
sessionDAO, err := service.CreateDAO*Session,
)

// Store analytics in dedicated shard 3
analyticsDAO, err := service.CreateDAO*Event,
)
```

**Best Practices**:
- Distribute high-volume entities across multiple shards for load balancing
- Keep related entities in the same shard to minimize cross-shard queries
- Plan shard assignments based on expected growth and access patterns

### WithCodec

Sets the serialization format for entity encoding/decoding.

```go
dao.WithCodec(codecType dao.CodecType) Option
```

**Always Available Codecs**:
- `dao.CodecJSON` - Standard JSON encoding (default, human-readable)
- `dao.CodecYAML` - YAML encoding (human-readable)
- `dao.CodecTOML` - TOML encoding (human-readable, good for config)
- `dao.CodecMsgPack` - MessagePack binary encoding (recommended for performance)
- `dao.CodecBSON` - BSON binary encoding (MongoDB compatible)

**Build-Tag Codecs** (require build flags):
- `dao.CodecCBOR` - CBOR binary encoding (`-tags codec_cbor`)
- `dao.CodecProtoBuf` - Protocol Buffers (`-tags codec_protobuf`)

Use `dao.SupportedCodecs()` and `dao.IsCodecSupported(codecType)` to discover available codecs at runtime.

**Default**: `CodecJSON`

**Examples**:

```go
// JSON: Human-readable, good for debugging
userDAO, err := service.CreateDAO*User,
)

// MsgPack: High performance, compact binary
sessionDAO, err := service.CreateDAO*Session,
)

// YAML: Configuration-like entities
configDAO, err := service.CreateDAO*Config,
)
```

### WithTimeout

Sets the default timeout for all DAO operations.

```go
dao.WithTimeout(duration time.Duration) Option
```

**Purpose**: Ensures operations don't hang indefinitely. If the context passed to DAO methods doesn't have a deadline, this timeout is applied.

**Default**: 30 seconds

**Examples**:

```go
// Short timeout for quick lookups
cacheDAO, err := service.CreateDAO*CacheEntry,
)

// Longer timeout for batch operations
batchDAO, err := service.CreateDAO*BatchJob,
)

// Very short timeout for real-time systems
realtimeDAO, err := service.CreateDAO*Metric,
)
```

**Interaction with Context**:
```go
// If context already has deadline, it takes precedence
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

// Uses 10 second timeout from context, not DAO's 30 second default
user, err := userDAO.Get(ctx, 123)
```

**Best Practices**:
- Set shorter timeouts for latency-sensitive operations
- Use longer timeouts for batch processing or large scans
- Always check for `context.DeadlineExceeded` errors

## Codec Performance Comparison

### Benchmark Results

Based on typical entity serialization (struct with 10 fields):

| Codec     | Marshal Time | Unmarshal Time | Size (bytes) | Human Readable |
|-----------|--------------|----------------|--------------|----------------|
| JSON      | 100%         | 100%           | 100%         | Yes            |
| MsgPack   | 40%          | 45%            | 50%          | No             |
| BSON      | 60%          | 65%            | 70%          | No             |
| YAML      | 250%         | 300%           | 120%         | Yes            |
| TOML      | 200%         | 250%           | 115%         | Yes            |

*Relative to JSON (100% = baseline)*

### Codec Selection Guide

**Choose JSON when**:
- Debugging and development
- Need human-readable storage
- Integrating with web APIs
- Storage size is not critical

**Choose MsgPack when**:
- Production high-throughput systems
- Storage efficiency matters
- Low latency is critical
- Don't need human readability

**Choose BSON when**:
- Migrating from MongoDB
- Need binary efficiency but want some structure
- Working with complex nested documents

**Choose YAML when**:
- Storing configuration entities
- Need comments or multi-line strings
- Human editing is required

**Choose TOML when**:
- Configuration-focused entities
- Prefer structured config format
- Need clear nested sections

### Codec Examples

#### JSON - Development & Debugging

```go
type User struct {
    ID       uint64 `json:"id"`
    Name     string `json:"name"`
    Email    string `json:"email"`
    Metadata map[string]interface{} `json:"metadata"`
}

userDAO, _ := service.CreateDAO*User,
)
```

**Stored as**:
```json
{
  "id": 123,
  "name": "Alice",
  "email": "alice@example.com",
  "metadata": {"role": "admin", "verified": true}
}
```

#### MsgPack - Production Performance

```go
type Session struct {
    ID        uint64    `msgpack:"id"`
    UserID    uint64    `msgpack:"uid"`
    Token     string    `msgpack:"tok"`
    ExpiresAt time.Time `msgpack:"exp"`
}

sessionDAO, _ := service.CreateDAO*Session,
)
```

**Stored as**: Compact binary (not human-readable)
- 30-50% smaller than JSON
- 2-3x faster serialization
- Ideal for high-volume operations

#### YAML - Configuration Storage

```go
type AppConfig struct {
    ID          uint64            `yaml:"id"`
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    Settings    map[string]string `yaml:"settings"`
}

configDAO, _ := service.CreateDAO*AppConfig,
)
```

**Stored as**:
```yaml
id: 456
name: Production Config
description: |
  Multi-line description
  for configuration
settings:
  debug: "false"
  timeout: "30s"
```

## Configuration Patterns

### Multi-Environment Setup

```go
func createUserDAO(svc service.QuicBaaS, env string) (dao.GenericDAO[*User], error) {
    var codec dao.CodecType
    var timeout time.Duration

    switch env {
    case "development":
        codec = dao.CodecJSON  // Readable for debugging
        timeout = 60 * time.Second
    case "staging":
        codec = dao.CodecMsgPack
        timeout = 30 * time.Second
    case "production":
        codec = dao.CodecMsgPack  // High performance
        timeout = 10 * time.Second
    default:
        codec = dao.CodecJSON
        timeout = 30 * time.Second
    }

    return service.CreateDAO*User,
        dao.WithTimeout(timeout),
    )
}
```

### Shard Distribution Strategy

```go
// Distribute entities by data characteristics
type DAORegistry struct {
    users     dao.GenericDAO[*User]      // Shard 1: Primary data
    sessions  dao.GenericDAO[*Session]   // Shard 2: Volatile data
    events    dao.GenericDAO[*Event]     // Shard 3: High-volume logs
    analytics dao.GenericDAO[*Metric]    // Shard 4: Time-series data
}

func NewDAORegistry(svc service.QuicBaaS) (*DAORegistry, error) {
    users, err := service.CreateDAO*User,
        dao.WithCodec(dao.CodecMsgPack),
    )
    if err != nil {
        return nil, err
    }

    sessions, err := service.CreateDAO*Session,
        dao.WithCodec(dao.CodecMsgPack),
        dao.WithTimeout(5*time.Second), // Fast timeouts
    )
    if err != nil {
        return nil, err
    }

    events, err := service.CreateDAO*Event,
        dao.WithCodec(dao.CodecMsgPack),
        dao.WithTimeout(60*time.Second), // Longer for batch writes
    )
    if err != nil {
        return nil, err
    }

    return &DAORegistry{
        users:    users,
        sessions: sessions,
        events:   events,
    }, nil
}
```

### Type-Specific Codec Selection

```go
// User entities: Balance readability and performance
userDAO, _ := service.CreateDAO*User,    // Readable for admin tools
    dao.WithTimeout(30*time.Second),
)

// Session tokens: Pure performance
sessionDAO, _ := service.CreateDAO*Session, // Compact & fast
    dao.WithTimeout(5*time.Second),  // Short-lived
)

// Audit logs: Human-readable with metadata
auditDAO, _ := service.CreateDAO*AuditLog,    // Compliance & debugging
    dao.WithTimeout(60*time.Second), // Allow batch operations
)

// System metrics: High-volume time-series
metricDAO, _ := service.CreateDAO*Metric, // Maximum efficiency
    dao.WithTimeout(1*time.Second),  // Real-time constraints
)
```

## Default Configuration

When no options are provided:

```go
userDAO, _ := service.CreateDAO*User
```

Equivalent to:

```go
userDAO, _ := service.CreateDAO*User,
    dao.WithCodec(dao.CodecJSON),
    dao.WithTimeout(30*time.Second),
)
```

## Configuration Validation

Invalid configurations are caught at DAO creation:

```go
// Invalid: nil factory
dao, err := service.CreateDAO*User
// Returns: ErrInvalidConfig{Field: "service", Reason: "cannot be nil"}

// Invalid: empty entity type
dao, err := service.CreateDAO*User
// Returns: ErrInvalidConfig{Field: "entityType", Reason: "cannot be empty"}

// Invalid: nil factory function
dao, err := service.CreateDAO*User
// Returns: ErrInvalidConfig{Field: "newEntity", Reason: "cannot be nil"}

// CBOR codec (requires build tag: -tags codec_cbor)
dao, err := service.CreateDAO*User, // Available with build tag
)
// Works when built with: go build -tags codec_cbor
```

## Advanced Configuration

### Custom Context Per Operation

Override DAO timeout on a per-call basis:

```go
userDAO, _ := service.CreateDAO*User, // Default
)

// Use shorter timeout for specific call
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

user, err := userDAO.Get(ctx, 123) // Uses 2-second timeout
```

### Configuration Profiles

```go
type DAOProfile struct {
    Shard   uint64
    Codec   dao.CodecType
    Timeout time.Duration
}

var (
    FastProfile = DAOProfile{
        Shard:   1,
        Codec:   dao.CodecMsgPack,
        Timeout: 5 * time.Second,
    }

    DebugProfile = DAOProfile{
        Shard:   1,
        Codec:   dao.CodecJSON,
        Timeout: 60 * time.Second,
    }
)

func createDAOWithProfileE dao.Entity E,
    profile DAOProfile,
) (dao.GenericDAO[E], error) {
    return service.CreateDAOE,
        dao.WithCodec(profile.Codec),
        dao.WithTimeout(profile.Timeout),
    )
}

// Usage
userDAO, _ := createDAOWithProfile(svc, "users", newUser, FastProfile)
```

## Next Steps

- Return to Getting Started for usage examples
- Read [Architecture Details](./architecture.md) for implementation insights
- See README for performance benchmarks


---

## DAO Integration Tests

### Overview

The DAO (Data Access Object) integration tests verify the complete DAO functionality against a real QuicBaaS cluster. These tests ensure that the generic DAO layer works correctly with real Raft consensus, PebbleDB storage, and all serialization codecs.

### Test Location

```
test/integration/dao/
├── dao_test.go         # Main integration tests
├── entities_test.go    # Test entity definitions
├── helpers.go          # Test helper functions
└── main_test.go        # Test setup and teardown
```

### Running the Tests

#### Run All DAO Integration Tests

```bash
make integration-test-dao
```

This command:
1. Starts a 3-node QuicBaaS cluster using Docker Compose
2. Waits for cluster readiness
3. Runs all DAO integration tests
4. Tears down the cluster

#### Run Specific Tests

```bash
# Run a specific test
go test -v -tags integration ./test/integration/dao -run TestDAOBasicSaveGet

# Run tests with timeout
go test -v -tags integration -timeout 10m ./test/integration/dao
```

### Test Coverage

#### 1. Basic CRUD Operations

**Test:** `TestDAOBasicSaveGet`

Tests fundamental Save and Get operations:
- Create an entity with various field types
- Auto-generate ID using UnixNano timestamp
- Save to real Raft storage
- Retrieve and verify data integrity
- Verify all field types (strings, numbers, booleans, slices, maps, timestamps)

**Expected Results:**
- Entity saved successfully with auto-generated ID
- Retrieved entity matches original data
- All field types correctly serialized and deserialized

#### 2. Delete Operations

**Test:** `TestDAODelete`

Tests entity deletion:
- Create and save an entity
- Verify existence
- Delete the entity
- Verify deletion (Get returns not found)

**Expected Results:**
- Delete operation succeeds
- Entity no longer retrievable after deletion

#### 3. Small Pagination

**Test:** `TestDAOPaginationSmall`

Tests pagination with a small dataset (25 entities):
- Create 25 session entities
- Paginate with page size of 10
- Iterate through pages
- Verify page tokens and HasMore flags

**Expected Results:**
- Multiple pages retrieved correctly
- Page sizes respected
- All entities accessible via pagination

#### 4. Large Pagination

**Test:** `TestDAOPaginationLarge`

Tests pagination with 150+ entities:
- Create 150 session entities
- Paginate with page size of 25
- Iterate through all pages
- Count total entities retrieved

**Expected Results:**
- 6+ pages retrieved (150 entities / 25 per page)
- All entities accessible
- No data loss during pagination
- Performance remains acceptable

#### 5. Different Serialization Codecs

**Test:** `TestDAODifferentCodecs`

Tests multiple serialization formats:
- **JSON:** Human-readable, standard format
- **MsgPack:** Binary, compact, fast
- **BSON:** Binary, MongoDB-compatible

For each codec:
- Create config entity with nested data structures
- Marshal with specific codec
- Save to storage
- Retrieve and unmarshal with same codec
- Verify data integrity

**Expected Results:**
- All codecs successfully serialize and deserialize
- No data corruption across codec types
- Complex data structures (maps, nested objects) handled correctly

#### 6. Multi-Shard Operations

**Test:** `TestDAODifferentShards`

Tests DAO operations across multiple Raft shards:
- Create entities on shard 1, 2, and 3
- Verify shard isolation
- Ensure correct shard routing

**Expected Results:**
- Entities saved to correct shards
- Shard-specific retrieval works
- No cross-shard interference

#### 7. Concurrent Operations

**Test:** `TestDAOConcurrentOperations`

Tests thread safety and concurrent access:
- Spawn 10 goroutines
- Each goroutine performs 5 Save operations
- Immediately verify each save with Get
- Total: 50 concurrent operations

**Expected Results:**
- All concurrent saves succeed
- No race conditions
- No data corruption
- All entities retrievable immediately after save

#### 8. Count Accuracy

**Test:** `TestDAOCountAccuracy`

Tests entity counting:
- Create 20 users
- Use List operation to count entities
- Verify count matches created entities

**Expected Results:**
- Count returns accurate number
- No phantom entities
- No missing entities

#### 9. Data Persistence

**Test:** `TestDAODataPersistence`

Tests data durability:
- Create and save an entity
- Retrieve entity 5 times with delays
- Verify data consistency across all retrievals

**Expected Results:**
- Data remains consistent
- No data corruption over time
- All fields maintain integrity

### Test Entities

#### User Entity

Complex entity with diverse field types:

```go
type User struct {
    ID        uint64            // Auto-generated
    Name      string
    Email     string
    Age       int
    Active    bool
    Balance   float64
    Tags      []string
    Metadata  map[string]string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**Use Cases:**
- Testing all common Go field types
- Verifying slice and map serialization
- Testing timestamp handling

#### Config Entity

Entity for testing different serialization formats:

```go
type Config struct {
    ID          uint64
    Name        string
    Environment string
    Settings    map[string]interface{} // Nested data
    Version     string
    Enabled     bool
}
```

**Use Cases:**
- Testing codec compatibility
- Verifying nested data structures
- Testing YAML/TOML tags

#### Product Entity

Entity for concurrent operations:

```go
type Product struct {
    ID          uint64
    SKU         string
    Name        string
    Description string
    Price       float64
    Stock       int
    Category    string
}
```

**Use Cases:**
- Concurrent write testing
- Performance benchmarking
- Real-world entity modeling

#### Session Entity

Entity for pagination tests:

```go
type Session struct {
    ID        uint64
    UserID    uint64
    Token     string
    IPAddress string
    UserAgent string
    ExpiresAt time.Time
    CreatedAt time.Time
}
```

**Use Cases:**
- Large-scale pagination
- Time-based queries
- Session management scenarios

### Test Environment

#### Docker Compose Setup

The tests use the integration test Docker Compose environment:

- **test-node1:** Primary Raft node (ID: 1)
- **test-node2:** Secondary Raft node (ID: 2)
- **test-node3:** Tertiary Raft node (ID: 3)

#### Network Configuration

- **gRPC Endpoints:**
  - Node 1: `test-node1:50051`
  - Node 2: `test-node2:50051`
  - Node 3: `test-node3:50051`

- **REST Endpoints:**
  - Node 1: `http://test-node1:8080`
  - Node 2: `http://test-node2:8080`
  - Node 3: `http://test-node3:8080`

#### Environment Variables

Tests read configuration from environment:

```bash
QUICBAAS_NODE1_GRPC=test-node1:50051
QUICBAAS_NODE2_GRPC=test-node2:50051
QUICBAAS_NODE3_GRPC=test-node3:50051
QUICBAAS_TEST_MODE=integration
```

### Test Architecture

#### TestMain Setup

The `TestMain` function in `main_test.go`:

1. **Setup Phase:**
   - Connect to QuicBaaS cluster via gRPC
   - Create service client
   - Wait for cluster readiness

2. **Execution Phase:**
   - Run all tests (`m.Run()`)

3. **Cleanup Phase:**
   - Close gRPC connections
   - Release resources

#### Helper Functions

##### `createTestKVClient()`

Creates a KV service client for direct gRPC operations.

##### `marshalJSON(v interface{}) ([]byte, error)`

Helper for JSON serialization in tests.

##### `unmarshalJSON(data []byte, v interface{}) error`

Helper for JSON deserialization in tests.

### Best Practices

#### 1. Entity Type Naming

Use unique entity types per test to avoid conflicts:

```go
entityType := fmt.Sprintf("users_test_%d", time.Now().UnixNano())
```

#### 2. ID Generation

Ensure unique IDs across concurrent tests:

```go
entity.SetEntityID(uint64(time.Now().UnixNano() + int64(i*1000)))
```

#### 3. Cleanup

Tests don't explicitly delete entities; each test uses unique entity types.

#### 4. Timeouts

Always use contexts with timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

#### 5. Error Handling

Use testify assertions for clear failure messages:

```go
require.NoError(t, err, "Save operation should succeed")
assert.Equal(t, expected, actual, "Values should match")
```

### Performance Expectations

#### Small Operations (< 10 entities)
- Save: < 100ms per entity
- Get: < 50ms per entity
- Delete: < 100ms per entity

#### Large Operations (100+ entities)
- Batch save: < 5 seconds for 100 entities
- Pagination: < 500ms per page (25 entities)

#### Concurrent Operations
- 50 concurrent saves: < 3 seconds total
- No race conditions
- No goroutine leaks

### Troubleshooting

#### Test Timeouts

**Symptom:** Tests timeout after 30 seconds

**Solutions:**
- Increase timeout: `go test -timeout 10m`
- Check cluster health
- Verify network connectivity

#### Connection Refused

**Symptom:** `connection refused` errors

**Solutions:**
- Ensure Docker Compose is running
- Check node health: `docker compose ps`
- Verify environment variables

#### Data Not Found After Save

**Symptom:** Entity not found immediately after save

**Solutions:**
- Check Raft consensus delay
- Add small delay after save
- Verify shard ID consistency

#### Codec Errors

**Symptom:** Serialization/deserialization failures

**Solutions:**
- Verify struct tags match codec
- Check for unsupported types
- Ensure nested structures are exported

### Continuous Integration

The DAO integration tests run in CI/CD pipelines:

```bash
# GitHub Actions
make integration-test-dao

# Local development
make integration-test-dao
```

#### CI Requirements

- Docker and Docker Compose installed
- Sufficient memory (4GB+)
- Network access for gRPC

### Implemented Test Coverage

#### Available Tests

1. **Index Queries:**
   - Secondary index support via `pkg/storage/index/`
   - Range queries with `ScanIndex()`

2. **Batch Operations:**
   - Storage-level atomic batches via `storage.Batch`
   - Multi-entity batch tests

3. **Advanced Codecs:**
   - CBOR support (`-tags codec_cbor`)
   - Protocol Buffers (`-tags codec_protobuf`)

#### Future Enhancements

1. **DAO-Level Transactions:**
   - Multi-entity atomic saves
   - Rollback on failures

2. **Streaming:**
   - Large entity streaming

3. **Performance Benchmarks:**
   - Throughput testing
   - Latency percentiles
   - Memory profiling

### Related Documentation

- DAO API Reference
- DAO Usage Guide
- Integration Test Framework
- [Docker Compose Configuration](../../test/integration/docker-compose.integration.yml)


---

## QuicBaaS Transactions

Transaction support for atomic operations with strong consistency guarantees.

### Overview

QuicBaaS provides multiple transaction mechanisms to handle different consistency and atomicity requirements:

| Type | Scope | Use Case |
|------|-------|----------|
| DAO Transactions | Single entity type | Application-level CRUD |
| Distributed Transactions | Cross-shard | Multi-partition atomicity |
| Optimistic Locking | Per-entity | Conflict detection |

### Documentation

#### DAO Transactions

Entity-aware transactions with read-your-own-writes semantics.

- DAO Transactions - Full documentation

```go
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)

tx.Commit(ctx)
```

#### Distributed Transactions (2PC)

Two-Phase Commit for atomic operations across multiple Raft shards.

- Distributed Transactions - Full documentation

```go
ops := map[uint64][]coordinator.TxnOp{
    shard1: {/* operations */},
    shard2: {/* operations */},
}

txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

#### Optimistic Locking

Version-based concurrency control for conflict detection.

- Optimistic Locking - Full documentation

```go
err := tx.SaveWithCondition(entity, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    expectedVersion,
})
```

### Choosing the Right Transaction Type

| Scenario | Recommended |
|----------|-------------|
| Single entity CRUD | DAO Transaction |
| Multiple related entities (same type) | DAO Transaction with batch |
| Cross-shard operations | 2PC Distributed Transaction |
| Concurrent updates | Optimistic Locking |
| High-contention writes | Optimistic Locking with retry |

### Related Documentation

- [Batch Operations](architecture.md)
- [Conditional Batch Operations](engineering.md)
- [MVCC Architecture](architecture.md)


---

## DAO-Level Transactions

Entity-aware transaction API providing atomic operations, read-your-own-writes semantics, and optimistic locking for QuicBaaS's Data Access Object layer.

### Overview

The DAO transaction system extends GenericDAO with transaction capabilities, enabling atomic multi-operation commits across entities of the same type. It provides read-your-own-writes semantics within transaction boundaries and supports conditional saves for optimistic concurrency control.

### Key Features

- **Atomic Commits** - All queued operations succeed or all fail
- **Read-Your-Own-Writes** - Immediate visibility of uncommitted changes within transaction
- **Optimistic Locking** - Version-based conditional saves for conflict detection
- **Auto ID Generation** - Automatic timestamp-based IDs for new entities
- **Safe Rollback** - Rollback is always safe to call, even after commit

### Quick Start

```go
// Create a TransactionalDAO
userDAO, err := dao.NewTransactional*User *User { return &User{} },
)

// Begin transaction
tx, err := userDAO.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback() // Safe to call even after commit

// Queue operations
tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)

// Commit atomically
return tx.Commit(ctx)
```

### API Reference

#### TransactionalDAO Interface

```go
type TransactionalDAO[E Entity] interface {
    GenericDAO[E]

    // BeginTx starts a new transaction.
    BeginTx(ctx context.Context) (DAOTransaction[E], error)
}
```

#### DAOTransaction Interface

```go
type DAOTransaction[E Entity] interface {
    // Save queues an entity for insert/update.
    Save(entity E) error

    // Delete queues an entity for deletion.
    Delete(entity E) error

    // Get retrieves an entity within transaction snapshot.
    // Returns entities from write cache first (read-your-own-writes).
    Get(id uint64) (E, error)

    // SaveWithCondition saves only if condition is met.
    SaveWithCondition(entity E, cond Condition) error

    // Commit atomically applies all queued operations.
    Commit(ctx context.Context) error

    // Rollback discards all queued operations.
    Rollback() error

    // IsClosed returns true if transaction has been committed or rolled back.
    IsClosed() bool
}
```

### Read-Your-Own-Writes

The transaction maintains a write cache that provides immediate visibility of uncommitted changes:

```go
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

// Create new user
user := &User{Name: "Alice", Email: "alice@example.com"}
tx.Save(user) // ID auto-generated

// Immediately visible within transaction (before commit)
retrieved, _ := tx.Get(user.EntityID())
fmt.Println(retrieved.Name) // "Alice"

// Deleted entities return NotFoundError
tx.Delete(user)
_, err := tx.Get(user.EntityID())
// err is NotFoundError

tx.Commit(ctx)
```

### Optimistic Locking

Use `SaveWithCondition` for optimistic concurrency control:

```go
// Read entity with version
user, _ := userDAO.Get(ctx, 123)
currentVersion := user.Version

// Modify locally
user.Name = "Updated Name"
user.Version++

// Save only if version hasn't changed
tx, _ := userDAO.BeginTx(ctx)
defer tx.Rollback()

err := tx.SaveWithCondition(user, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    currentVersion,
})
if dao.IsConditionFailed(err) {
    // Another process modified the entity
    return handleConflict()
}

return tx.Commit(ctx)
```

#### Condition Types

| Target | Description |
|--------|-------------|
| `ConditionVersion` | Check entity's version field |
| `ConditionCreate` | Check entity's creation timestamp |
| `ConditionMod` | Check entity's modification timestamp |
| `ConditionValue` | Check entity's serialized value |

#### Condition Operators

| Operator | Description |
|----------|-------------|
| `OpEqual` | Equality check (==) |
| `OpNotEqual` | Inequality check (!=) |
| `OpGreater` | Greater than (>) |
| `OpLess` | Less than (<) |
| `OpGreaterEqual` | Greater or equal (>=) |
| `OpLessEqual` | Less or equal (<=) |

### Helper Functions

#### WithTx - Automatic Transaction Management

```go
err := dao.WithTx(ctx, userDAO, func(tx dao.DAOTransaction[*User]) error {
    tx.Save(user1)
    tx.Save(user2)
    return nil // Commit on success
})
// Automatically commits or rolls back based on return value
```

#### BatchSave - Atomic Batch Insert

```go
users := []*User{user1, user2, user3}
err := dao.BatchSave(ctx, userDAO, users)
```

#### BatchDelete - Atomic Batch Deletion

```go
users := []*User{user1, user2, user3}
err := dao.BatchDelete(ctx, userDAO, users)
```

#### SaveWithOptimisticLock - Version Check Shortcut

```go
err := dao.SaveWithOptimisticLock(ctx, userDAO, user, expectedVersion)
if dao.IsConditionFailed(err) {
    // Handle concurrent modification
}
```

### Entity Interfaces

#### Versioned Interface

For entities that track version numbers:

```go
type Versioned interface {
    GetVersion() int64
    SetVersion(version int64)
}

// Embed VersionedEntity for automatic implementation
type User struct {
    dao.VersionedEntity
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
}
```

#### Timestamped Interface

For entities that track creation/modification times:

```go
type Timestamped interface {
    GetCreatedAt() time.Time
    GetUpdatedAt() time.Time
}

// Embed TimestampedEntity for automatic implementation
type Session struct {
    dao.TimestampedEntity
    ID     uint64 `json:"id"`
    UserID uint64 `json:"user_id"`
}
```

### Error Handling

The DAO transaction package uses typed errors:

```go
// Check for specific errors
if dao.IsConditionFailed(err) {
    // Optimistic lock failed - concurrent modification
}

if dao.IsNotFound(err) {
    // Entity doesn't exist
}

if dao.IsTransactionClosed(err) {
    // Transaction already committed or rolled back
}
```

#### Error Types

| Error | Description |
|-------|-------------|
| `TransactionClosedError` | Operation attempted on closed transaction |
| `ConditionFailedError` | Precondition not satisfied |
| `InvalidEntityError` | Entity is nil or invalid |
| `InvalidEntityIDError` | Entity ID is invalid (e.g., 0 for delete) |
| `InvalidConditionError` | Condition is malformed |
| `TransactionCommitError` | Commit failed |

### Use Cases

#### User Registration with Profile

```go
err := dao.WithTx(ctx, userDAO, func(tx dao.DAOTransaction[*User]) error {
    user := &User{Name: "Alice", Email: "alice@example.com"}
    if err := tx.Save(user); err != nil {
        return err
    }

    profile := &Profile{UserID: user.EntityID(), Bio: "New user"}
    return profileTx.Save(profile)
})
```

#### Inventory Update with Stock Check

```go
tx, _ := inventoryDAO.BeginTx(ctx)
defer tx.Rollback()

item, _ := tx.Get(itemID)
if item.Stock < requiredQuantity {
    return ErrInsufficientStock
}

// Conditional update - only if stock hasn't changed
item.Stock -= requiredQuantity
err := tx.SaveWithCondition(item, dao.Condition{
    Target:   dao.ConditionVersion,
    Operator: dao.OpEqual,
    Value:    item.Version,
})
if err != nil {
    return err
}

return tx.Commit(ctx)
```

### Thread Safety

- TransactionalDAO instances are safe for concurrent use
- Individual DAOTransaction instances should be used by a single goroutine
- Multiple transactions can run concurrently against the same DAO

### Limitations

- Transactions are scoped to a single entity type
- Cross-shard transactions require the 2PC coordinator (see Distributed Transactions)
- No automatic retry on condition failures

### Related Documentation

- [GenericDAO](architecture.md) - Base DAO functionality
- Distributed Transactions - Cross-shard 2PC
- Optimistic Locking - Detailed concurrency patterns


---

## Distributed Transactions (2PC)

Two-Phase Commit (2PC) coordination for atomic distributed transactions across multiple Raft shards with strong consistency guarantees.

### Overview

The 2PC Coordinator enables atomic commit or abort of transactions spanning multiple partitions (shards). It ensures that either all participating shards commit the transaction or all abort, maintaining data consistency across the distributed system.

### Architecture

```
                        ┌─────────────────────┐
                        │   2PC Coordinator   │
                        │  - Begin            │
                        │  - Prepare          │
                        │  - Commit/Abort     │
                        │  - Recovery         │
                        └─────────┬───────────┘
                                  │
         ┌────────────────────────┼────────────────────────┐
         │                        │                        │
         v                        v                        v
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Participant 1  │    │  Participant 2  │    │  Participant N  │
│    (Shard 1)    │    │    (Shard 2)    │    │    (Shard N)    │
│                 │    │                 │    │                 │
│  - Prepare      │    │  - Prepare      │    │  - Prepare      │
│  - Commit       │    │  - Commit       │    │  - Commit       │
│  - Abort        │    │  - Abort        │    │  - Abort        │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Transaction States

```
   ┌──────────────┐
   │  INITIATED   │
   └──────┬───────┘
          │ Begin
          v
   ┌──────────────┐     Prepare Failed
   │  PREPARING   │─────────────────────┐
   └──────┬───────┘                     │
          │ All Prepared                │
          v                             v
   ┌──────────────┐              ┌──────────────┐
   │   PREPARED   │              │   ABORTING   │
   └──────┬───────┘              └──────┬───────┘
          │ Commit Decision             │
          v                             v
   ┌──────────────┐              ┌──────────────┐
   │  COMMITTING  │              │   ABORTED    │
   └──────┬───────┘              └──────────────┘
          │
          v
   ┌──────────────┐
   │  COMMITTED   │
   └──────────────┘
```

### Quick Start

```go
// Create coordinator
coordinator := coordinator.NewCoordinator(nodeHost, store, logger)

// Register participants for each shard
for shardID, participant := range participants {
    coordinator.RegisterParticipant(shardID, participant)
}

// Define operations per shard
ops := map[uint64][]coordinator.TxnOp{
    1: { // Shard 1 operations
        {Type: coordinator.TxnOpPut, Key: []byte("user:1"), Value: userBytes},
    },
    2: { // Shard 2 operations
        {Type: coordinator.TxnOpPut, Key: []byte("order:1"), Value: orderBytes},
    },
}

// Begin transaction
txnID, err := coordinator.Begin(ctx, ops, nil)
if err != nil {
    return err
}

// Execute 2PC
if err := coordinator.Prepare(ctx, txnID); err != nil {
    coordinator.Abort(ctx, txnID)
    return err
}

if err := coordinator.Commit(ctx, txnID); err != nil {
    return err
}
```

### API Reference

#### Coordinator

```go
// NewCoordinator creates a new 2PC coordinator.
func NewCoordinator(
    nodeHost *quicraft.NodeHost,
    store storage.Storage,
    log *logger.Logger,
) *Coordinator

// Begin starts a new distributed transaction.
func (c *Coordinator) Begin(
    ctx context.Context,
    ops map[uint64][]TxnOp,
    conditions []BatchCondition,
) (TransactionID, error)

// Prepare executes the prepare phase.
func (c *Coordinator) Prepare(ctx context.Context, txnID TransactionID) error

// Commit executes the commit phase.
func (c *Coordinator) Commit(ctx context.Context, txnID TransactionID) error

// Abort aborts a transaction.
func (c *Coordinator) Abort(ctx context.Context, txnID TransactionID) error

// GetStatus returns the current transaction status.
func (c *Coordinator) GetStatus(txnID TransactionID) (*DistributedTransaction, error)

// RegisterParticipant registers a participant for a shard.
func (c *Coordinator) RegisterParticipant(shardID uint64, p *Participant)
```

#### Transaction Operations

```go
type TxnOp struct {
    Type  TxnOpType  // TxnOpPut, TxnOpDelete, TxnOpGet
    Key   []byte
    Value []byte     // Only for TxnOpPut
}
```

#### Batch Conditions

Apply preconditions that must be satisfied before commit:

```go
type BatchCondition struct {
    Target   CompareTarget  // Version, CreateRevision, ModRevision, Value
    Op       CompareOp      // Equal, NotEqual, Less, Greater
    Key      []byte
    Value    []byte         // For value comparisons
    Revision int64          // For revision comparisons
    Version  int64          // For version comparisons
}
```

### Participant Implementation

Each shard requires a Participant that handles the 2PC protocol:

```go
type Participant struct {
    shardID     uint64
    store       storage.Storage
    preparedTxn sync.Map // Stores prepared transactions
}

// Prepare validates and locks resources.
func (p *Participant) Prepare(
    ctx context.Context,
    txnID TransactionID,
    ops []TxnOp,
    conditions []BatchCondition,
) error

// Commit applies the prepared operations.
func (p *Participant) Commit(ctx context.Context, txnID TransactionID) error

// Abort releases locks and discards prepared operations.
func (p *Participant) Abort(ctx context.Context, txnID TransactionID) error
```

### Recovery

The coordinator persists transaction state for crash recovery:

```go
// On startup, recover pending transactions
recovery := coordinator.NewRecovery(store, logger)
pendingTxns, err := recovery.RecoverPendingTransactions()

for _, txn := range pendingTxns {
    switch txn.State() {
    case TxnStateCommitting:
        // Transaction was decided to commit - complete it
        coordinator.Commit(ctx, txn.ID)
    case TxnStatePreparing, TxnStatePrepared:
        // Uncertain state - abort to be safe
        coordinator.Abort(ctx, txn.ID)
    }
}
```

#### Recovery States

| State | Recovery Action |
|-------|-----------------|
| Initiated | Abort |
| Preparing | Abort |
| Prepared | Abort (timeout expired) or retry prepare |
| Committing | Complete commit |
| Committed | Clean up state |
| Aborting | Complete abort |
| Aborted | Clean up state |

### Timeout Configuration

```go
const (
    DefaultPrepareTimeout     = 10 * time.Second
    DefaultCommitTimeout      = 30 * time.Second
    DefaultAbortTimeout       = 10 * time.Second
    DefaultTransactionTimeout = 60 * time.Second
    DefaultRecoveryInterval   = 5 * time.Second
)
```

### Use Cases

#### Cross-Shard Transfer

```go
// Transfer funds between accounts on different shards
ops := map[uint64][]coordinator.TxnOp{
    1: { // Debit from account A
        {Type: TxnOpPut, Key: []byte("account:A"), Value: debitedBalance},
    },
    2: { // Credit to account B
        {Type: TxnOpPut, Key: []byte("account:B"), Value: creditedBalance},
    },
}

// Add condition: only if account A has sufficient balance
conditions := []BatchCondition{
    {
        Target: CompareValue,
        Op:     CompareGreater,
        Key:    []byte("account:A:balance"),
        Value:  minimumBalance,
    },
}

txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

#### Multi-Entity Creation

```go
// Create user and associated data across shards
ops := map[uint64][]coordinator.TxnOp{
    userShard: {
        {Type: TxnOpPut, Key: userKey, Value: userData},
    },
    profileShard: {
        {Type: TxnOpPut, Key: profileKey, Value: profileData},
    },
    settingsShard: {
        {Type: TxnOpPut, Key: settingsKey, Value: settingsData},
    },
}

txnID, _ := coordinator.Begin(ctx, ops, nil)
if err := coordinator.Prepare(ctx, txnID); err != nil {
    coordinator.Abort(ctx, txnID)
    return err
}
coordinator.Commit(ctx, txnID)
```

### Metrics

The coordinator exports Prometheus metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_coordinator_transactions_total` | Counter | Total transactions by status |
| `quicbaas_coordinator_prepare_latency_seconds` | Histogram | Prepare phase latency |
| `quicbaas_coordinator_commit_latency_seconds` | Histogram | Commit phase latency |
| `quicbaas_coordinator_abort_latency_seconds` | Histogram | Abort phase latency |
| `quicbaas_coordinator_active_transactions` | Gauge | Currently active transactions |
| `quicbaas_coordinator_prepare_failures_total` | Counter | Prepare failures by reason |
| `quicbaas_coordinator_commit_failures_total` | Counter | Commit failures by reason |
| `quicbaas_coordinator_condition_failures_total` | Counter | Condition check failures |
| `quicbaas_coordinator_recovered_transactions_total` | Counter | Recovered transactions |

### Best Practices

1. **Keep Transactions Short** - Minimize the time between prepare and commit
2. **Handle Failures** - Always have a fallback plan for abort scenarios
3. **Use Conditions** - Leverage batch conditions for optimistic locking
4. **Monitor Metrics** - Track transaction latencies and failure rates
5. **Test Recovery** - Verify recovery works correctly for your scenarios

### Limitations

- Performance overhead compared to single-shard operations
- Blocking protocol - participants hold locks during 2PC
- Coordinator is a single point of coordination (but not failure with persistence)
- Network partitions can cause transaction timeouts

### Related Documentation

- DAO Transactions - Single-entity transactions
- Optimistic Locking - Version-based concurrency
- [Conditional Batch Operations](engineering.md) - Batch conditions
- [Raft Consensus](../cluster/architecture.md) - Underlying consensus


---

## Optimistic Locking

Version-based concurrency control for conflict detection and resolution in QuicBaaS transactions.

### Overview

Optimistic locking allows concurrent access to data while detecting conflicts at commit time. Instead of acquiring locks upfront, the system tracks version information and rejects updates when concurrent modifications are detected.

### Concepts

#### Version-Based Concurrency

Each entity maintains a version number that increments on modification:

```
Time T1: Read user (version=5)
Time T2: Another process modifies user (version=6)
Time T3: Save with condition version==5 -> FAILS (actual=6)
```

#### Compare-And-Swap Semantics

Operations succeed only if the current state matches expected conditions:

```go
// Only save if version hasn't changed
tx.SaveWithCondition(entity, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    expectedVersion,
})
```

### Implementation Patterns

#### Basic Optimistic Lock

```go
// Read current state
user, err := userDAO.Get(ctx, userID)
if err != nil {
    return err
}
originalVersion := user.Version

// Modify locally
user.Name = "Updated Name"
user.Version++

// Save with version check
err = dao.SaveWithOptimisticLock(ctx, userDAO, user, originalVersion)
if dao.IsConditionFailed(err) {
    return ErrConcurrentModification
}
```

#### Retry Pattern

```go
const maxRetries = 3

func updateUserWithRetry(ctx context.Context, userID uint64, updateFn func(*User)) error {
    for attempt := 0; attempt < maxRetries; attempt++ {
        // Read current state
        user, err := userDAO.Get(ctx, userID)
        if err != nil {
            return err
        }
        originalVersion := user.Version

        // Apply update
        updateFn(user)
        user.Version++

        // Attempt save
        err = dao.SaveWithOptimisticLock(ctx, userDAO, user, originalVersion)
        if err == nil {
            return nil // Success
        }

        if !dao.IsConditionFailed(err) {
            return err // Non-retryable error
        }

        // Conflict detected - retry
        log.Printf("Retry %d: concurrent modification detected", attempt+1)
    }

    return ErrMaxRetriesExceeded
}
```

#### Conditional Save Patterns

##### Must Not Exist (Create Only)

```go
// Only create if entity doesn't exist
tx.SaveWithCondition(newUser, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    int64(0), // Version 0 means non-existent
})
```

##### Must Exist (Update Only)

```go
// Only update if entity exists
tx.SaveWithCondition(user, Condition{
    Target:   ConditionVersion,
    Operator: OpGreater,
    Value:    int64(0), // Version > 0 means exists
})
```

##### Timestamp-Based Condition

```go
// Only update if not modified since lastReadTime
tx.SaveWithCondition(user, Condition{
    Target:   ConditionMod,
    Operator: OpLessEqual,
    Value:    lastReadTime.UnixNano(),
})
```

### Entity Setup

#### Using VersionedEntity Helper

```go
type User struct {
    dao.VersionedEntity // Embeds Version field
    ID    uint64        `json:"id"`
    Name  string        `json:"name"`
    Email string        `json:"email"`
}

func (u *User) EntityID() uint64      { return u.ID }
func (u *User) SetEntityID(id uint64) { u.ID = id }

// VersionedEntity provides:
// - GetVersion() int64
// - SetVersion(int64)
```

#### Using TimestampedEntity Helper

```go
type Session struct {
    dao.TimestampedEntity // Embeds CreatedAt, UpdatedAt
    ID     uint64         `json:"id"`
    UserID uint64         `json:"user_id"`
    Token  string         `json:"token"`
}

// TimestampedEntity provides:
// - GetCreatedAt() time.Time
// - GetUpdatedAt() time.Time
```

#### Custom Implementation

```go
type Account struct {
    ID       uint64 `json:"id"`
    Balance  int64  `json:"balance"`
    Version  int64  `json:"version"`
    Modified int64  `json:"modified"` // Unix nano timestamp
}

func (a *Account) GetVersion() int64 { return a.Version }
func (a *Account) SetVersion(v int64) { a.Version = v }
```

### Condition Types Reference

| Target | Interface Required | Description |
|--------|-------------------|-------------|
| `ConditionVersion` | `Versioned` | Entity's version counter |
| `ConditionCreate` | `Timestamped` | Entity's creation time |
| `ConditionMod` | `Timestamped` | Entity's last modification time |
| `ConditionValue` | None | Entity's serialized value |

### Error Handling

```go
err := tx.SaveWithCondition(entity, condition)

switch {
case err == nil:
    // Success
case dao.IsConditionFailed(err):
    // Concurrent modification - handle conflict
    cfErr := err.(dao.ConditionFailedError)
    log.Printf("Condition failed: %s", cfErr.Reason)
case dao.IsInvalidCondition(err):
    // Malformed condition
    icErr := err.(dao.InvalidConditionError)
    log.Printf("Invalid condition: %s", icErr.Reason)
default:
    // Other error
    return err
}
```

### Conflict Resolution Strategies

#### Last-Write-Wins

Simple but may lose updates:

```go
// Ignore conflicts, always save
err := userDAO.Save(ctx, user)
```

#### Merge Strategy

Combine concurrent changes:

```go
func mergeUsers(base, current, updated *User) *User {
    merged := &User{ID: base.ID, Version: current.Version + 1}

    // Merge each field
    if updated.Name != base.Name {
        merged.Name = updated.Name
    } else {
        merged.Name = current.Name
    }

    // ... merge other fields

    return merged
}
```

#### Application-Level Resolution

Let the user decide:

```go
if dao.IsConditionFailed(err) {
    current, _ := userDAO.Get(ctx, user.ID)
    return NewConflictError(user, current)
}
```

### Performance Considerations

#### Advantages

- No lock contention on reads
- High throughput for read-heavy workloads
- No deadlock possibility
- Simple implementation

#### Trade-offs

- Retries consume resources under high contention
- Read-modify-write cycle can be expensive
- May require application-level conflict resolution

#### Recommendations

1. **Short Transaction Windows** - Minimize time between read and write
2. **Batch Updates** - Group related changes to reduce retry probability
3. **Exponential Backoff** - Add jitter to retry delays
4. **Monitor Conflict Rate** - High rates indicate design issues

### Use Cases

#### Counter Increment

```go
func incrementCounter(ctx context.Context, counterID uint64) error {
    for attempt := 0; attempt < 3; attempt++ {
        counter, _ := counterDAO.Get(ctx, counterID)
        originalVersion := counter.Version

        counter.Value++
        counter.Version++

        err := dao.SaveWithOptimisticLock(ctx, counterDAO, counter, originalVersion)
        if err == nil {
            return nil
        }
        if !dao.IsConditionFailed(err) {
            return err
        }
        // Retry on conflict
    }
    return ErrTooManyRetries
}
```

#### Inventory Reservation

```go
func reserveInventory(ctx context.Context, itemID uint64, quantity int) error {
    item, _ := inventoryDAO.Get(ctx, itemID)

    if item.Available < quantity {
        return ErrInsufficientStock
    }

    originalVersion := item.Version
    item.Available -= quantity
    item.Reserved += quantity
    item.Version++

    return dao.SaveWithOptimisticLock(ctx, inventoryDAO, item, originalVersion)
}
```

#### Document Edit

```go
func saveDocument(ctx context.Context, doc *Document, baseVersion int64) error {
    err := dao.SaveWithOptimisticLock(ctx, docDAO, doc, baseVersion)
    if dao.IsConditionFailed(err) {
        // Load current version for conflict resolution UI
        current, _ := docDAO.Get(ctx, doc.ID)
        return NewEditConflictError(doc, current)
    }
    return err
}
```

### Related Documentation

- DAO Transactions - Transaction API
- Distributed Transactions - Cross-shard transactions
- [Conditional Batch Operations](engineering.md) - Batch conditions


---

## QuicBaaS Batch Operations

Atomic batch operations for efficient multi-key writes with strong consistency.

### Overview

QuicBaaS provides multiple batch operation modes for different throughput and consistency requirements:

| Mode | Best For | Features |
|------|----------|----------|
| Atomic Batch | Related entities | All-or-nothing semantics |
| Conditional Batch | Precondition checks | if-match, if-none-match, version checks |
| Streaming Batch | Large datasets | Progress tracking, resume capability |

### Documentation

#### Atomic Batch Operations

Basic atomic multi-key operations with automatic rollback.

- [Atomic Batch Operations](architecture.md) - Full documentation

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userData},
        {EntityType: "profile", Id: 123, Data: profileData},
    },
})
```

#### Conditional Batch Operations

Precondition-based batch operations with version checks and existence requirements.

- Conditional Batch Operations - Full documentation

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: requests,
    Conditions: []*pb.BatchCondition{
        {EntityType: "user", Id: 123, MustNotExist: true},
        {EntityType: "order", Id: 456, ExpectedVersion: 5},
    },
})
```

#### Streaming Batch Operations

gRPC streaming for large dataset operations with progress tracking.

- Streaming Batch Operations - Full documentation

```go
stream, _ := client.StreamBatchPut(ctx)

stream.Send(header)
for _, chunk := range chunks {
    stream.Send(chunk)
    progress, _ := stream.Recv()
}
stream.Send(complete)
```

### Choosing the Right Batch Mode

| Scenario | Recommended |
|----------|-------------|
| Small batches (< 100 ops) | Atomic Batch |
| Create-only operations | Conditional Batch (MustNotExist) |
| Update with conflict detection | Conditional Batch (ExpectedVersion) |
| Large imports (> 1000 ops) | Streaming Batch |
| Resume after failure | Streaming Batch |

### Performance Comparison

| Mode | Latency | Throughput | Memory |
|------|---------|------------|--------|
| Single Put | 5ms | 200 ops/sec | Low |
| Atomic Batch (10) | 6ms | 1,600 ops/sec | Low |
| Streaming Batch | Variable | 10,000+ ops/sec | Moderate |

### Related Documentation

- [Distributed Transactions](engineering.md)
- [DAO Transactions](engineering.md)
- [Transaction Architecture](architecture.md)


---

## Conditional Batch Operations

Precondition-based batch operations with if-match, if-none-match, and version check semantics for atomic multi-key updates.

### Overview

Conditional batch operations extend the basic batch API with preconditions that must be satisfied before operations execute. This enables atomic check-and-set patterns across multiple keys while maintaining the atomicity guarantees of batch operations.

### Key Features

- **MustExist** - Operations fail if key doesn't exist
- **MustNotExist** - Operations fail if key already exists
- **ExpectedVersion** - Operations fail if version doesn't match
- **Atomic Evaluation** - All conditions checked before any operation executes
- **Detailed Results** - Per-key success/failure information

### Quick Start

```go
// Create condition evaluator
reader := batch.NewStorageKeyValueReader(store)
evaluator := batch.NewConditionEvaluator(reader)

// Define conditions
conditions := []*proto.BatchCondition{
    {
        EntityType:      "user",
        Id:              123,
        MustExist:       true, // User must exist
    },
    {
        EntityType:      "order",
        Id:              456,
        MustNotExist:    true, // Order must not exist (create only)
    },
    {
        EntityType:      "account",
        Id:              789,
        ExpectedVersion: 5, // Account must be at version 5
    },
}

// Evaluate conditions
result, err := evaluator.Evaluate(ctx, conditions)
if err != nil {
    return err
}

if !result.Succeeded {
    log.Printf("Conditions failed: %v", result.FailedKeys)
    return ErrPreconditionFailed
}

// Proceed with batch operation
```

### API Reference

#### ConditionEvaluator

```go
// NewConditionEvaluator creates a new evaluator with the given storage reader.
func NewConditionEvaluator(reader KeyValueReader) *ConditionEvaluator

// Evaluate checks all conditions against current storage state.
func (e *ConditionEvaluator) Evaluate(
    ctx context.Context,
    conditions []*proto.BatchCondition,
) (*EvaluationResult, error)
```

#### BatchCondition (Protocol Buffer)

```protobuf
message BatchCondition {
    string entity_type = 1;
    uint64 id = 2;
    bool must_exist = 3;
    bool must_not_exist = 4;
    uint64 expected_version = 5;
}
```

#### EvaluationResult

```go
type EvaluationResult struct {
    Succeeded    bool              // True if all conditions passed
    FailedKeys   []string          // Keys that failed conditions
    FailedReason string            // First failure reason
    Results      []ConditionResult // Per-condition results
}

type ConditionResult struct {
    Key           string
    EntityType    string
    ID            uint64
    Passed        bool
    Reason        string
    ActualVersion uint64
}
```

### Condition Types

#### MustExist

Requires the key to exist before the operation:

```go
// Only update existing users
condition := &proto.BatchCondition{
    EntityType: "user",
    Id:         123,
    MustExist:  true,
}
```

Use cases:
- Update operations that should fail on missing entities
- Reference validation before creating dependent records
- Ensuring consistency in multi-step workflows

#### MustNotExist

Requires the key to not exist:

```go
// Only create new orders (prevent duplicates)
condition := &proto.BatchCondition{
    EntityType:   "order",
    Id:           456,
    MustNotExist: true,
}
```

Use cases:
- Create-only operations (idempotent inserts)
- Preventing duplicate entries
- Reserving unique identifiers

#### ExpectedVersion

Requires a specific version number:

```go
// Only update if at expected version (optimistic locking)
condition := &proto.BatchCondition{
    EntityType:      "account",
    Id:              789,
    ExpectedVersion: 5,
}
```

Use cases:
- Optimistic concurrency control
- Detecting concurrent modifications
- Ensuring read-modify-write consistency

### Integration with Batch Operations

#### gRPC API

```protobuf
message BatchPutRequest {
    repeated PutRequest requests = 1;
    ConsistencyLevel consistency = 2;
    uint64 shard_id = 3;
    repeated BatchCondition conditions = 4; // NEW: Conditions
}
```

#### Server-Side Processing

```go
func (s *Server) BatchPut(ctx context.Context, req *proto.BatchPutRequest) (*proto.BatchPutResponse, error) {
    // Evaluate conditions first
    if len(req.Conditions) > 0 {
        reader := batch.NewStorageKeyValueReader(s.storage)
        evaluator := batch.NewConditionEvaluator(reader)

        result, err := evaluator.Evaluate(ctx, req.Conditions)
        if err != nil {
            return nil, err
        }

        if !result.Succeeded {
            return &proto.BatchPutResponse{
                Success:      false,
                FailedKeys:   result.FailedKeys,
                ErrorMessage: result.FailedReason,
            }, nil
        }
    }

    // Proceed with batch put
    return s.executeBatchPut(ctx, req)
}
```

### Use Cases

#### User Registration with Email Uniqueness

```go
conditions := []*proto.BatchCondition{
    {
        EntityType:   "user",
        Id:           newUserID,
        MustNotExist: true, // User ID must be unique
    },
    {
        EntityType:   "email_index",
        Id:           emailHash,
        MustNotExist: true, // Email must be unique
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    if containsKey(result.FailedKeys, "email_index") {
        return ErrEmailAlreadyExists
    }
    return ErrUserIDExists
}

// Create user and email index atomically
batchPut(ctx, user, emailIndex)
```

#### Inventory Transfer

```go
conditions := []*proto.BatchCondition{
    {
        EntityType:      "inventory",
        Id:              sourceWarehouseID,
        ExpectedVersion: sourceVersion, // Prevent concurrent modifications
    },
    {
        EntityType: "inventory",
        Id:         destWarehouseID,
        MustExist:  true, // Destination must exist
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    return ErrTransferPreconditionFailed
}

// Transfer inventory atomically
batchPut(ctx, updatedSource, updatedDest)
```

#### Order Creation with Customer Validation

```go
conditions := []*proto.BatchCondition{
    {
        EntityType: "customer",
        Id:         customerID,
        MustExist:  true, // Customer must exist
    },
    {
        EntityType:   "order",
        Id:           orderID,
        MustNotExist: true, // Prevent duplicate orders
    },
}

result, _ := evaluator.Evaluate(ctx, conditions)
if !result.Succeeded {
    for _, r := range result.Results {
        if !r.Passed && r.EntityType == "customer" {
            return ErrCustomerNotFound
        }
    }
    return ErrOrderAlreadyExists
}

// Create order
batchPut(ctx, order, orderItems)
```

### Error Handling

```go
result, err := evaluator.Evaluate(ctx, conditions)

// Check for system errors
if err != nil {
    if errors.IsCanceled(err) {
        return ErrOperationCanceled
    }
    return err
}

// Check condition failures
if !result.Succeeded {
    for _, r := range result.Results {
        if !r.Passed {
            log.Printf("Condition failed for %s:%d - %s",
                r.EntityType, r.ID, r.Reason)
        }
    }
    return &PreconditionFailedError{
        FailedKeys: result.FailedKeys,
        Reason:     result.FailedReason,
    }
}
```

### Performance Considerations

#### Condition Handler Dispatch

Conditions use O(1) map-based dispatch for efficient evaluation:

```go
// Internal implementation
conditionHandlers map[conditionType]conditionHandler{
    conditionTypeMustExist:     handleMustExist,
    conditionTypeMustNotExist:  handleMustNotExist,
    conditionTypeVersionCheck:  handleVersionCheck,
}
```

#### Batch Evaluation

All conditions are evaluated in a single pass before any operations execute, minimizing storage reads.

#### Context Cancellation

Evaluation respects context cancellation for long-running checks:

```go
for _, cond := range conditions {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // Evaluate condition
    }
}
```

### Best Practices

1. **Order Conditions** - Put most likely to fail conditions first
2. **Minimize Conditions** - Only include necessary preconditions
3. **Use Specific Errors** - Map failed keys to meaningful error messages
4. **Handle Timeouts** - Set appropriate context timeouts for evaluation
5. **Monitor Failures** - Track condition failure rates for capacity planning

### Related Documentation

- [Atomic Batch Operations](architecture.md) - Basic batch operations
- Streaming Batch Operations - Large dataset batching
- [Optimistic Locking](engineering.md) - Version-based concurrency
- [Distributed Transactions](engineering.md) - Cross-shard transactions


---

## Streaming Batch Operations

gRPC streaming API for high-throughput batch operations on large datasets with progress tracking and resume capability.

### Overview

Streaming batch operations enable efficient processing of large datasets that exceed practical limits for single-request batches. The system supports chunked uploads, real-time progress tracking, validation feedback, and session resume after interruption.

### Key Features

- **Chunked Streaming** - Process large datasets in manageable chunks
- **Progress Tracking** - Real-time feedback on operation status
- **Resume Capability** - Continue interrupted sessions from last checkpoint
- **Backpressure Handling** - Automatic flow control for overload prevention
- **Condition Validation** - Per-chunk validation with deferred commit checks
- **Prometheus Metrics** - Comprehensive observability

### Architecture

```
Client                              Server
  │                                   │
  │─────── StreamBatchHeader ────────>│  Create session
  │<──────── SessionID ───────────────│
  │                                   │
  │─────── StreamBatchChunk[0] ──────>│  Process chunk
  │<──────── Progress ────────────────│
  │                                   │
  │─────── StreamBatchChunk[1] ──────>│  Process chunk
  │<──────── Progress ────────────────│
  │                                   │
  │           ... N chunks ...         │
  │                                   │
  │─────── StreamBatchComplete ──────>│  Commit/Abort
  │<──────── StreamBatchResult ───────│
```

### Protocol Messages

#### StreamBatchHeader

Initiates a streaming batch session:

```protobuf
message StreamBatchHeader {
    uint64 shard_id = 1;
    int64 total_operations = 2;       // Expected total operations
    ConsistencyLevel consistency = 3;
    repeated BatchCondition conditions = 4;
}
```

#### StreamBatchChunk

Contains a batch of operations:

```protobuf
message StreamBatchChunk {
    int32 chunk_index = 1;            // For ordering/resume
    repeated PutRequest operations = 2;
}
```

#### StreamBatchProgress

Real-time progress feedback:

```protobuf
message StreamBatchProgress {
    int64 operations_received = 1;
    int64 operations_validated = 2;
    int32 chunk_index = 3;
    repeated string validation_errors = 4;
}
```

#### StreamBatchComplete

Signals end of stream:

```protobuf
message StreamBatchComplete {
    bool commit = 1;                  // true=commit, false=abort
}
```

#### StreamBatchResult

Final result after commit/abort:

```protobuf
message StreamBatchResult {
    bool succeeded = 1;
    int64 operations_committed = 2;
    repeated string failed_keys = 3;
    string error = 4;
    int64 timestamp = 5;
}
```

### Quick Start

#### Client-Side

```go
// Create streaming client
stream, err := client.StreamBatchPut(ctx)
if err != nil {
    return err
}

// Send header
header := &proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Header{
        Header: &proto.StreamBatchHeader{
            ShardId:         1,
            TotalOperations: int64(len(allOperations)),
            Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
        },
    },
}
stream.Send(header)

// Send chunks
chunkSize := 100
for i := 0; i < len(allOperations); i += chunkSize {
    end := min(i+chunkSize, len(allOperations))
    chunk := &proto.StreamBatchPutRequest{
        Request: &proto.StreamBatchPutRequest_Chunk{
            Chunk: &proto.StreamBatchChunk{
                ChunkIndex: int32(i / chunkSize),
                Operations: allOperations[i:end],
            },
        },
    }
    stream.Send(chunk)

    // Receive progress
    progress, _ := stream.Recv()
    log.Printf("Progress: %d/%d", progress.OperationsReceived, len(allOperations))
}

// Complete the stream
complete := &proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Complete{
        Complete: &proto.StreamBatchComplete{Commit: true},
    },
}
stream.Send(complete)

// Get final result
result, _ := stream.Recv()
log.Printf("Committed: %d operations", result.OperationsCommitted)
```

#### Server-Side Processing

```go
processor := batch.NewStreamingBatchProcessor(aggregator, logger)

func (s *Server) StreamBatchPut(stream proto.QuicBaaS_StreamBatchPutServer) error {
    var session *streamingSession
    var sessionID string

    for {
        req, err := stream.Recv()
        if err == io.EOF {
            break
        }

        switch r := req.Request.(type) {
        case *proto.StreamBatchPutRequest_Header:
            sessionID = generateSessionID()
            session, err = processor.NewSession(ctx, sessionID, r.Header)

        case *proto.StreamBatchPutRequest_Chunk:
            progress, err := processor.AddChunk(ctx, sessionID, r.Chunk)
            stream.Send(progress)

        case *proto.StreamBatchPutRequest_Complete:
            result, err := processor.Complete(ctx, sessionID, r.Complete)
            stream.Send(result)
            return nil
        }
    }
    return nil
}
```

### Session Management

#### Creating Sessions

```go
session, err := processor.NewSession(ctx, sessionID, header)
if err != nil {
    // Handle duplicate session, invalid header, etc.
}
```

#### Session Lifecycle

```
NewSession -> AddChunk* -> Complete(commit=true)  -> COMMITTED
                        -> Complete(commit=false) -> ABORTED
                        -> Abort(reason)          -> ABORTED
                        -> Timeout                -> ABORTED
```

#### Resume Capability

Sessions track the last successfully received chunk index:

```go
// Get last chunk index for resume
lastChunk, _ := processor.GetLastChunkIndex(sessionID)

// Client resumes from lastChunk + 1
for i := lastChunk + 1; i < totalChunks; i++ {
    stream.Send(chunks[i])
}
```

### Validation

#### Per-Chunk Validation

Basic validation occurs immediately:

- Entity type required
- Data payload required
- Conditions recorded for commit-time check

#### Commit-Time Validation

Full condition evaluation at commit:

- MustExist/MustNotExist checks
- Version comparisons
- Cross-reference validation

#### Handling Validation Errors

```go
result, _ := processor.Complete(ctx, sessionID, complete)
if !result.Succeeded {
    for _, key := range result.FailedKeys {
        log.Printf("Failed: %s", key)
    }
    // Validation errors returned in FailedKeys
}
```

### Backpressure Handling

The processor implements backpressure to prevent memory exhaustion:

```go
// Progress channel has limited buffer
progressChan: make(chan *pb.StreamBatchProgress, 100)

// Non-blocking send with backpressure metric
select {
case session.progressChan <- progress:
    // Sent successfully
default:
    metrics.backpressureEvents.Inc()
    // Channel full, client should slow down
}
```

### Configuration

Default values:

```go
const (
    DefaultStreamTimeout       = 5 * time.Minute
    DefaultMaxChunksPerSession = 10000
    DefaultProgressInterval    = 100 // Progress every N operations
)
```

### Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_streaming_batch_sessions_total` | Counter | Sessions by status |
| `quicbaas_streaming_batch_chunks_received_total` | Counter | Chunks by shard |
| `quicbaas_streaming_batch_operations_total` | Counter | Operations by shard/status |
| `quicbaas_streaming_batch_session_duration_seconds` | Histogram | Session duration |
| `quicbaas_streaming_batch_chunk_size` | Histogram | Operations per chunk |
| `quicbaas_streaming_batch_validation_errors_total` | Counter | Validation failures |
| `quicbaas_streaming_batch_active_sessions` | Gauge | Active session count |
| `quicbaas_streaming_batch_backpressure_events_total` | Counter | Backpressure events |

### Use Cases

#### Bulk Data Import

```go
// Import 1 million records in chunks
records := loadRecordsFromFile("data.csv")
chunkSize := 1000

stream, _ := client.StreamBatchPut(ctx)

// Send header
stream.Send(&proto.StreamBatchPutRequest{
    Request: &proto.StreamBatchPutRequest_Header{
        Header: &proto.StreamBatchHeader{
            ShardId:         1,
            TotalOperations: int64(len(records)),
        },
    },
})

// Stream chunks
for i := 0; i < len(records); i += chunkSize {
    chunk := buildChunk(records[i:min(i+chunkSize, len(records))], i/chunkSize)
    stream.Send(chunk)

    // Handle progress
    progress, _ := stream.Recv()
    printProgress(progress)
}

// Commit
stream.Send(completeRequest(true))
result, _ := stream.Recv()
```

#### ETL Pipeline

```go
// Transform and load with validation
transformer := NewDataTransformer(schema)
validator := NewDataValidator(rules)

stream, _ := client.StreamBatchPut(ctx)
stream.Send(headerRequest)

chunkIndex := 0
for batch := range dataSource.Batches() {
    // Transform
    transformed := transformer.Transform(batch)

    // Pre-validate
    if errors := validator.Validate(transformed); len(errors) > 0 {
        log.Printf("Validation errors in chunk %d", chunkIndex)
        continue
    }

    // Stream
    stream.Send(buildChunk(transformed, chunkIndex))
    chunkIndex++
}

stream.Send(completeRequest(true))
```

#### Incremental Backup Restore

```go
// Restore with resume capability
lastProcessed := getCheckpoint(sessionID)

stream, _ := client.StreamBatchPut(ctx)
stream.Send(headerRequest)

for i, record := range backupRecords {
    if i <= lastProcessed {
        continue // Skip already processed
    }

    stream.Send(buildChunk([]*Record{record}, i))

    progress, _ := stream.Recv()
    saveCheckpoint(sessionID, progress.ChunkIndex)
}

stream.Send(completeRequest(true))
```

### Error Handling

#### Session Errors

```go
session, err := processor.NewSession(ctx, sessionID, header)
switch err.(type) {
case *StreamingBatchError:
    // System error (closed, invalid input)
case *StreamingSessionNotFoundError:
    // Session doesn't exist
}
```

#### Chunk Errors

```go
progress, err := processor.AddChunk(ctx, sessionID, chunk)
switch err.(type) {
case *StreamingSessionAbortedError:
    // Session was aborted
case *StreamingChunkOutOfOrderError:
    // Resume from correct position
}
```

#### Commit Errors

```go
result, err := processor.Complete(ctx, sessionID, complete)
if !result.Succeeded {
    // Check FailedKeys and Error for details
}
```

### Best Practices

1. **Chunk Size** - Use 100-1000 operations per chunk for optimal throughput
2. **Progress Handling** - Process progress responses to detect issues early
3. **Resume Support** - Persist chunk index for crash recovery
4. **Timeout Handling** - Set appropriate context timeouts for long operations
5. **Validation** - Pre-validate data client-side when possible
6. **Backpressure** - Slow down if receiving backpressure signals

### Limitations

- Single shard per session (use 2PC for cross-shard)
- Memory usage scales with pending operations
- Session timeout aborts incomplete streams

### Related Documentation

- [Atomic Batch Operations](architecture.md) - Basic batch API
- Conditional Batch Operations - Batch conditions
- gRPC API Reference - Full gRPC documentation


---

## File Storage Backend

The File Storage backend provides persistent key-value storage using the filesystem as the backing store. Each key-value pair is stored as a separate file, making it ideal for human-readable data, debugging, and version control scenarios. The implementation follows the same pattern as MemoryStorage for consistency across storage backends.

### Features

- **Human-readable files**: Entities stored in native format (JSON, YAML, etc.)
- **Hierarchical structure**: Entity types become directories, IDs become filenames
- **Multi-codec support**: JSON, YAML, TOML, MessagePack, BSON via `dao.CodecType`
- **Atomic writes**: Uses temp file + rename for crash safety
- **Thread-safe**: Uses RWMutex for concurrent access
- **Snapshots**: Create point-in-time copies for backup

### Use Cases

- Configuration files that can be edited manually
- Debug-friendly storage for development
- Git-friendly data that can be version controlled
- Export/import scenarios
- Human-auditable data stores

### Quick Start

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/storage/file"
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

// Create file store with JSON codec
config := file.DefaultConfig()
config.DataDir = "/data/entities"
config.CodecType = dao.CodecJSON

store, err := file.New(config, log)
if err != nil {
    return err
}
defer store.Close()

// Basic operations (no context)
store.Set([]byte("users/123"), []byte(`{"name":"Alice"}`))
value, _ := store.Get([]byte("users/123"))
store.Delete([]byte("users/123"))

// Range scan
results, _ := store.Scan([]byte("users/"), []byte("users/~"))

// Snapshot and restore
snapshot, _ := store.Snapshot()
newStore.Restore(snapshot)
```

### API Reference

| Method | Signature | Description |
|--------|-----------|-------------|
| `Set` | `Set(key, value []byte) error` | Store a key-value pair |
| `Get` | `Get(key []byte) ([]byte, error)` | Retrieve a value by key |
| `Delete` | `Delete(key []byte) error` | Remove a key-value pair |
| `Scan` | `Scan(start, end []byte) ([][2][]byte, error)` | Range scan returning key-value pairs |
| `Snapshot` | `Snapshot() (map[string][]byte, error)` | Create point-in-time backup |
| `Restore` | `Restore(snapshot map[string][]byte) error` | Restore from backup |
| `Len` | `Len() (int, error)` | Count total keys |
| `Close` | `Close() error` | Close the store |

### File Structure

Keys are mapped directly to filesystem paths:

```
Key: users/123         -> File: {DataDir}/users/123.json
Key: sessions/456      -> File: {DataDir}/sessions/456.yaml
Key: config/db/main    -> File: {DataDir}/config/db/main.toml
```

Example directory structure:

```
/data/entities/
  users/
    1736582400000000000.json
    1736582401000000000.json
  sessions/
    1736582500000000000.json
  config/
    database.yaml
```

### Supported Codecs

| Codec | Extension | Description |
|-------|-----------|-------------|
| `dao.CodecJSON` | `.json` | Human-readable, widely supported |
| `dao.CodecYAML` | `.yaml` | Human-readable, good for config |
| `dao.CodecTOML` | `.toml` | Human-readable, good for config |
| `dao.CodecMsgPack` | `.msgpack` | Binary, compact, fast |
| `dao.CodecBSON` | `.bson` | Binary, MongoDB compatible |
| `dao.CodecProtoBuf` | `.pb` | Binary, Protocol Buffers |

### Codec Matching

The FileStorage codec type must match the DAO codec type. The store uses the codec type only to determine the file extension - the actual encoding/decoding is done by the DAO.

```go
// Correct: Both use CodecJSON
config := file.DefaultConfig()
config.DataDir = "/data"
config.CodecType = dao.CodecJSON
store, _ := file.New(config, log)
dao, _ := dao.New(store, "users", newUser, dao.WithCodec(dao.CodecJSON))

// Incorrect: Mismatched codecs will create files with wrong extension
config.CodecType = dao.CodecYAML  // .yaml files
store, _ := file.New(config, log)
dao, _ := dao.New(store, "users", newUser, dao.WithCodec(dao.CodecJSON))  // JSON content!
```

### Comparison with Other Backends

| Feature | File | Memory | PebbleDB | Raft |
|---------|------|--------|----------|------|
| Persistence | Yes | No | Yes | Yes |
| Human-readable | Yes | N/A | No | No |
| Performance | Low | High | High | Medium |
| Scalability | Low | Medium | High | High |
| Use case | Debug/Config | Testing | Production | Distributed |

### Limitations

- **Performance**: File I/O is slower than in-memory or LSM-tree storage
- **Scalability**: Not suitable for high-volume production workloads
- **Concurrent writes**: File locking may cause contention under heavy load
- **Large files**: Each entity is a separate file; very large entities may be slow

### See Also

- Configuration - Detailed configuration options
- DAO Documentation - Using DAOs with file storage
- Codec Documentation - Codec configuration


---

## File Storage Configuration

This document describes the configuration options for the File Storage backend.

### Import Path

```go
import "github.com/quicbaas/quicbaas/pkg/storage/file"
```

### Configuration Struct

```go
type Config struct {
    DataDir         string         // Required: root directory for files
    CodecType       dao.CodecType  // Determines file extension
    FilePermissions os.FileMode    // Permission mode for created files
    DirPermissions  os.FileMode    // Permission mode for created directories
    SyncWrites      bool           // Sync writes to disk for durability
}
```

### Configuration Options

#### DataDir (Required)

The root directory where all data files are stored. Entity types become subdirectories, and entity IDs become filenames.

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/var/lib/quicbaas/data"
```

The directory is created automatically if it doesn't exist.

#### CodecType

Determines the file extension for stored files. This should match the codec configured on the DAO.

| Codec | Extension |
|-------|-----------|
| `dao.CodecJSON` | `.json` |
| `dao.CodecYAML` | `.yaml` |
| `dao.CodecTOML` | `.toml` |
| `dao.CodecMsgPack` | `.msgpack` |
| `dao.CodecBSON` | `.bson` |
| `dao.CodecProtoBuf` | `.pb` |
| `dao.CodecCBOR` | `.cbor` |

**Default**: `dao.CodecJSON`

#### FilePermissions

The Unix permission mode for created files.

**Default**: `0644` (owner read/write, group/others read)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.FilePermissions = 0600 // Owner read/write only
```

#### DirPermissions

The Unix permission mode for created directories.

**Default**: `0755` (owner read/write/execute, group/others read/execute)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.DirPermissions = 0700 // Owner only
```

#### SyncWrites

When enabled, files are synced to disk (fsync) after each write operation. This provides durability at the cost of performance.

**Default**: `true`

```go
// High-performance mode (less durable)
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.SyncWrites = false
```

### Example Configurations

#### Development Configuration

```go
cfg := file.DefaultConfig()
cfg.DataDir = "./data"
cfg.CodecType = dao.CodecJSON
cfg.SyncWrites = false // Faster for development
```

#### Production Configuration (Config Files)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/etc/myapp/config"
cfg.CodecType = dao.CodecYAML
cfg.FilePermissions = 0644
cfg.DirPermissions = 0755
cfg.SyncWrites = true
```

#### Secure Configuration

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/var/lib/myapp/secrets"
cfg.CodecType = dao.CodecJSON
cfg.FilePermissions = 0600 // Owner read/write only
cfg.DirPermissions = 0700  // Owner only
cfg.SyncWrites = true
```

### Codec-to-Extension Mapping

The `ExtensionForCodec` function maps codec types to file extensions:

```go
ext := file.ExtensionForCodec(dao.CodecJSON)     // ".json"
ext := file.ExtensionForCodec(dao.CodecYAML)     // ".yaml"
ext := file.ExtensionForCodec(dao.CodecTOML)     // ".toml"
ext := file.ExtensionForCodec(dao.CodecMsgPack)  // ".msgpack"
ext := file.ExtensionForCodec(dao.CodecBSON)     // ".bson"
ext := file.ExtensionForCodec(dao.CodecProtoBuf) // ".pb"
ext := file.ExtensionForCodec(dao.CodecCBOR)     // ".cbor"
```

Unknown codec types default to `.dat`.

### Validation

The configuration is validated when creating a new file store:

```go
store, err := file.New(cfg, log)
if err != nil {
    // Handle validation error - uses pkg/errors QRDBError (aliased QBError)
    if errors.IsInvalidInput(err) {
        // Invalid configuration
        fmt.Printf("Configuration error: %v\n", err)
    }
}
```

Required fields:
- `DataDir` must not be empty
- `CodecType` must not be empty

### Performance Considerations

#### SyncWrites Trade-offs

| SyncWrites | Durability | Performance |
|------------|------------|-------------|
| `true` | High | Lower (fsync per write) |
| `false` | Lower | Higher (OS buffers writes) |

For development and testing, disable SyncWrites for faster iteration. For production with important data, keep SyncWrites enabled.

#### File System Considerations

- **SSD vs HDD**: SSDs handle random writes much better than HDDs
- **File count**: Very large numbers of files (millions) may slow down directory operations
- **File size**: Each entity is a separate file; very large entities may be slow to read/write

### See Also

- File Storage Overview
- DAO Configuration
- Codec Configuration
