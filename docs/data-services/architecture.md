# Data Services — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/data-services/`.

This document provides a deep dive into the GenericDAO architecture, implementation details, and design decisions.

## Overview

The DAO (Data Access Object) layer provides a type-safe abstraction over QuicBaaS's key-value storage, enabling CRUD operations and pagination without requiring direct KVStore interaction.

## Architecture Layers

```
┌─────────────────────────────────────────────────────────┐
│              Application Code                            │
│  (Business Logic, Services, Controllers)                │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│         GenericDAO[E Entity] Interface                   │
│  - Save(E) error                                         │
│  - Get(id) (E, error)                                    │
│  - Delete(E) error                                       │
│  - Count() (int, error)                                  │
│  - Page(PageQuery) (PageResult[E], error)                │
│  - ForEachPage(PageQuery, PagerFunc[E]) error            │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│         genericDAO[E] Implementation                     │
│  - Entity type management                                │
│  - Auto ID generation (UnixNano)                         │
│  - Timeout handling                                      │
│  - Key encoding (<entityType>/<id>)                      │
│  - Pagination logic                                      │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│              Codec Layer                                 │
│  - Marshal(E) ([]byte, error)                            │
│  - Unmarshal([]byte, *E) error                           │
│                                                          │
│  Implementations:                                        │
│  ├─ JSON Codec                                           │
│  ├─ YAML Codec                                           │
│  ├─ TOML Codec                                           │
│  ├─ MsgPack Codec                                        │
│  └─ BSON Codec                                           │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│        interfaces.KVStore Interface                      │
│  - Put(key, value) error                                 │
│  - Get(key) ([]byte, error)                              │
│  - Delete(key) error                                     │
│  - Scan(prefix, callback) error                          │
└──────────────────┬──────────────────────────────────────┘
                   │
       ┌───────────┴───────────┬───────────────┐
       ▼                       ▼               ▼
┌─────────────┐      ┌─────────────┐     ┌─────────────┐
│ MemoryStore │      │ LocalStore  │     │ RaftStore   │
│             │      │  (PebbleDB) │     │  (Cluster)  │
│ (Testing)   │      │ (Single)    │     │ (Multi-Node)│
└─────────────┘      └─────────────┘     └─────────────┘
```

## Key Components

### 1. Entity Interface

The `Entity` interface is the contract that all persistable types must implement:

```go
type Entity interface {
    EntityID() uint64
    SetEntityID(id uint64)
}
```

**Design Decisions**:
- Uses `uint64` for IDs to support high-volume systems (0 to 18.4 quintillion)
- Zero value (0) indicates "not yet persisted"
- Simple interface keeps entity definitions clean

**Auto ID Generation**:
```go
func (d *genericDAO[E]) Save(ctx context.Context, entity E) error {
    if entity.EntityID() == 0 {
        entity.SetEntityID(uint64(time.Now().UnixNano()))
    }
    // ... continue with save
}
```

**Why UnixNano?**
- Monotonically increasing (sorted by creation time)
- Collision-resistant in single-process scenarios
- Simple and fast (no UUID parsing overhead)
- Compatible with sorting algorithms

**Trade-offs**:
- Not suitable for distributed ID generation across processes
- Clock skew could cause ordering issues in multi-node systems
- For distributed systems, consider external ID generators (Snowflake, ULID)

### 2. Key Encoding Scheme

All entities are stored using a hierarchical key format:

```
Format: <entityType>/<id>

Examples:
  users/12345678901234
  sessions/98765432109876
  roles/1001
  tenants/5000
```

**Implementation**:
```go
func (d *genericDAO[E]) makeKey(id uint64) []byte {
    return []byte(fmt.Sprintf("%s/%d", d.entityType, id))
}

func (d *genericDAO[E]) prefixKey() []byte {
    return []byte(d.entityType + "/")
}
```

**Benefits**:
- **Namespace Isolation**: Different entity types can't conflict
- **Efficient Scanning**: Prefix-based iteration for pagination
- **Human Readable**: Easy to debug with raw KV store inspection
- **Lexicographic Ordering**: Natural sort order for pagination

**Storage Layout Example**:
```
roles/1001        → {"id":1001,"name":"Admin",...}
roles/1002        → {"id":1002,"name":"User",...}
sessions/1234567  → {"id":1234567,"token":"abc",...}
sessions/1234568  → {"id":1234568,"token":"xyz",...}
users/5001        → {"id":5001,"username":"alice",...}
users/5002        → {"id":5002,"username":"bob",...}
```

**Scanning Behavior**:
```go
// Scan all users (prefix: "users/")
dao.Scan(ctx, []byte("users/"), func(key, value []byte) error {
    // Receives:
    //   key="users/5001", value={"id":5001,...}
    //   key="users/5002", value={"id":5002,...}
    //   ...
})
```

### 3. Codec Layer

The codec layer handles entity serialization/deserialization:

```go
type Codec[E any] interface {
    Marshal(entity E) ([]byte, error)
    Unmarshal(data []byte, entity *E) error
    Type() CodecType
}
```

**Codec Selection**:
```go
func NewCodecE any (Codec[E], error) {
    var c codec.Codec[E]

    switch ct {
    case CodecJSON:
        c = json.New[E]()
    case CodecMsgPack:
        c = msgpack.New[E]()
    case CodecBSON:
        c = bson.New[E]()
    // ... etc
    }

    return &codecWrapper[E]{
        codec:     c,
        codecType: ct,
    }, nil
}
```

**Codec Wrapper Pattern**:
The `codecWrapper` adapts third-party codecs to the DAO's interface:

```go
type codecWrapper[E any] struct {
    codec     codec.Codec[E]
    codecType CodecType
}

func (w *codecWrapper[E]) Marshal(entity E) ([]byte, error) {
    return w.codec.Marshal(entity)
}

func (w *codecWrapper[E]) Unmarshal(data []byte, entity *E) error {
    return w.codec.Unmarshal(data, entity)
}
```

**Benefits**:
- Decouples DAO from specific codec implementations
- Enables swapping codecs without changing application code
- Allows for custom codec implementations

### 4. KVStore Integration

GenericDAO integrates with `interfaces.KVStore`, which abstracts storage backends:

```go
type KVStore interface {
    Put(ctx context.Context, key, value []byte) error
    Get(ctx context.Context, key []byte) ([]byte, error)
    Delete(ctx context.Context, key []byte) error
    Scan(ctx context.Context, prefix []byte, fn func(key, value []byte) error) error
}
```

**Save Operation Flow**:
```go
func (d *genericDAO[E]) Save(ctx context.Context, entity E) error {
    // 1. Auto-generate ID if needed
    if entity.EntityID() == 0 {
        entity.SetEntityID(uint64(time.Now().UnixNano()))
    }

    // 2. Serialize entity
    data, err := d.codec.Marshal(entity)
    if err != nil {
        return ErrSerializationFailed{EntityType: d.entityType, Cause: err}
    }

    // 3. Apply timeout
    ctx, cancel := d.ensureTimeout(ctx)
    defer cancel()

    // 4. Store in KVStore
    key := d.makeKey(entity.EntityID())
    return d.store.Put(ctx, key, data)
}
```

**Get Operation Flow**:
```go
func (d *genericDAO[E]) Get(ctx context.Context, id uint64) (E, error) {
    var zero E

    ctx, cancel := d.ensureTimeout(ctx)
    defer cancel()

    // 1. Retrieve from KVStore
    key := d.makeKey(id)
    data, err := d.store.Get(ctx, key)
    if err != nil {
        if isNotFoundError(err) {
            return zero, ErrNotFound{EntityType: d.entityType, ID: id}
        }
        return zero, err
    }

    // 2. Deserialize entity
    entity := d.newEntity()
    if err := d.codec.Unmarshal(data, &entity); err != nil {
        return zero, ErrDeserializationFailed{EntityType: d.entityType, Cause: err}
    }

    return entity, nil
}
```

### 5. Pagination Implementation

Pagination uses offset-based iteration over the KVStore:

```go
func (d *genericDAO[E]) Page(ctx context.Context, query PageQuery) (PageResult[E], error) {
    result := PageResult[E]{
        Entities: make([]E, 0, query.PageSize),
        Page:     query.Page,
        PageSize: query.PageSize,
    }

    offset := (query.Page - 1) * query.PageSize
    current := 0
    collected := 0

    err := d.store.Scan(ctx, d.prefixKey(), func(_, value []byte) error {
        // Skip to offset
        if current < offset {
            current++
            return nil
        }

        // Check if page is full
        if collected >= query.PageSize {
            result.HasMore = true
            return ErrStopIteration
        }

        // Deserialize and collect
        entity := d.newEntity()
        if err := d.codec.Unmarshal(value, &entity); err != nil {
            return nil // Skip malformed entities
        }

        result.Entities = append(result.Entities, entity)
        collected++
        current++
        return nil
    })

    return result, err
}
```

**Pagination Characteristics**:
- **Offset-Based**: Simple but O(n) for deep pages
- **Scan Direction**: Always forward (ascending by key)
- **Error Handling**: Skips malformed entities, continues scan
- **Early Termination**: Stops after collecting page + 1 (to detect HasMore)

**ForEachPage Implementation**:
```go
func (d *genericDAO[E]) ForEachPage(ctx context.Context, query PageQuery, fn PagerFunc[E]) error {
    currentPage := query.Page
    if currentPage < 1 {
        currentPage = 1
    }

    for {
        result, err := d.Page(ctx, PageQuery{
            Page:      currentPage,
            PageSize:  query.PageSize,
            SortOrder: query.SortOrder,
        })
        if err != nil {
            return err
        }

        if len(result.Entities) > 0 {
            if err := fn(result.Entities); err != nil {
                if err == ErrStopIteration {
                    return nil
                }
                return err
            }
        }

        if !result.HasMore || len(result.Entities) == 0 {
            break
        }

        currentPage++
    }

    return nil
}
```

### 6. Timeout Management

Timeout handling ensures operations don't hang indefinitely:

```go
func (d *genericDAO[E]) ensureTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
    // If context already has deadline, respect it
    if _, ok := ctx.Deadline(); ok {
        return ctx, func() {}
    }
    // Otherwise, apply DAO's default timeout
    return context.WithTimeout(ctx, d.timeout)
}
```

**Timeout Precedence**:
1. **Context Deadline**: If the incoming context has a deadline, use it
2. **DAO Timeout**: Otherwise, apply the DAO's configured timeout
3. **Default**: If not configured, 30 seconds

**Example**:
```go
// DAO configured with 30s timeout
userDAO, _ := service.CreateDAO*User,
)

// Case 1: No deadline - uses 30s DAO timeout
ctx1 := context.Background()
user1, _ := userDAO.Get(ctx1, 123) // 30s timeout

// Case 2: Context deadline - uses 5s deadline
ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
user2, _ := userDAO.Get(ctx2, 123) // 5s timeout
```

## Factory Pattern

The factory pattern enables clean DAO creation:

```go
type Factory struct {
    kvStoreProvider KVStoreProvider
}

type KVStoreProvider func(shardID uint64) interfaces.KVStore

func NewFactory(provider KVStoreProvider) *Factory {
    return &Factory{kvStoreProvider: provider}
}

func NewDAOE Entity E,
    opts ...Option,
) (GenericDAO[E], error) {
    cfg := ParseOptions(opts...)
    kvStore := factory.kvStoreProvider(cfg.ShardID)
    return New(kvStore, entityType, newEntity, opts...)
}
```

**Service Integration**:
```go
// In service package
type daoFactory struct {
    kvStoreProvider func(shardID uint64) interfaces.KVStore
}

// CreateDAO convenience function
func CreateDAOE dao.Entity E,
    opts ...dao.Option,
) (dao.GenericDAO[E], error) {
    cfg := dao.ParseOptions(opts...)
    df := svc.DAO().(*daoFactory)
    kvStore := df.kvStoreProvider(cfg.ShardID)
    return dao.New(kvStore, entityType, newEntity, opts...)
}
```

## Error Handling

Typed errors provide precise error handling:

```go
// Not found
type ErrNotFound struct {
    EntityType string
    ID         uint64
}

// Serialization failure
type ErrSerializationFailed struct {
    EntityType string
    Cause      error
}

// Deserialization failure
type ErrDeserializationFailed struct {
    EntityType string
    Cause      error
}

// Invalid entity ID
type ErrInvalidEntityID struct {
    EntityType string
    Reason     string
}

// Configuration error
type ErrInvalidConfig struct {
    Field  string
    Reason string
}
```

**Error Detection**:
```go
// Not found check
if dao.IsNotFound(err) {
    // Handle not found
}

// Unwrapping for root cause
var serErr dao.ErrSerializationFailed
if errors.As(err, &serErr) {
    log.Printf("Serialization failed: %v", serErr.Cause)
}
```

## Backend Support

### Memory Mode

```go
// In-memory KVStore for testing
type MemoryKVStore struct {
    data sync.Map
}

func (s *MemoryKVStore) Put(ctx context.Context, key, value []byte) error {
    s.data.Store(string(key), value)
    return nil
}
```

**Use Cases**:
- Unit testing
- Development environments
- Ephemeral sessions

### Local Mode (PebbleDB)

```go
// Direct PebbleDB access
type LocalKVStore struct {
    db *pebble.DB
}

func (s *LocalKVStore) Put(ctx context.Context, key, value []byte) error {
    return s.db.Set(key, value, pebble.Sync)
}
```

**Use Cases**:
- Single-node deployments
- Embedded databases
- High-performance local storage

### Cluster Mode (Raft)

```go
// Raft-replicated storage
type RaftKVStore struct {
    nodeHost *quicraft.NodeHost
    shardID  uint64
}

func (s *RaftKVStore) Put(ctx context.Context, key, value []byte) error {
    // Propose to Raft cluster
    result, err := s.nodeHost.SyncPropose(ctx, s.shardID, key, value)
    return err
}
```

**Use Cases**:
- Multi-node clusters
- High availability
- Strong consistency across nodes

## Thread Safety

GenericDAO instances are safe for concurrent use:

```go
// Single DAO, multiple goroutines
userDAO, _ := service.CreateDAO*User

var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        user := &User{Name: fmt.Sprintf("User%d", id)}
        userDAO.Save(context.Background(), user)
    }(i)
}
wg.Wait()
```

**Thread Safety Guarantees**:
- All DAO operations are safe for concurrent use
- No internal shared mutable state
- KVStore implementations handle their own synchronization
- Codec instances are created per-DAO (not shared)

## Performance Characteristics

### Time Complexity

| Operation      | Time Complexity | Notes                                      |
|----------------|-----------------|---------------------------------------------|
| Save           | O(1)            | Direct key-value put                        |
| Get            | O(1)            | Direct key-value lookup                     |
| Delete         | O(1)            | Direct key-value delete                     |
| Count          | O(n)            | Full scan of entity type                    |
| Page(p)        | O(p × s)        | Offset-based, where s = page size           |
| ForEachPage    | O(n)            | Single scan through all entities            |

### Space Complexity

**Per Entity**:
```
Storage = len(key) + len(serialized_entity)
        = len(entityType) + 1 + len(id_as_string) + len(codec_output)

Example:
  "users/12345" + JSON{"id":12345,...}
  = 12 bytes + 150 bytes (typical user)
  = 162 bytes total
```

**MsgPack vs JSON**:
- JSON: ~30-50% larger than MsgPack
- YAML: ~15-20% larger than JSON
- BSON: ~20-30% smaller than JSON

## Design Trade-offs

### Why Not Cursor-Based Pagination?

**Current**: Offset-based (skip N, take M)
**Alternative**: Cursor-based (start after key X)

**Decision**: Offset-based for simplicity
- Simpler API (`Page: 1, 2, 3...` vs. opaque cursors)
- Easier debugging (know exactly which page you're on)
- Works with existing KVStore.Scan interface

**Trade-off**: Deep pagination is slower (must skip many records)

### Why UnixNano IDs?

**Current**: `time.Now().UnixNano()`
**Alternative**: UUID, Snowflake, ULID

**Decision**: UnixNano for performance
- Zero allocation
- Monotonically increasing
- Simple implementation

**Trade-off**: Not suitable for distributed generation

### Why Generic Interface?

**Current**: `GenericDAO[E Entity]`
**Alternative**: Non-generic interface with `interface{}`

**Decision**: Generics for type safety
- Compile-time type checking
- No runtime type assertions
- Better IDE support and autocomplete

**Trade-off**: Requires Go 1.18+

## Implementation Status

### Implemented Features

1. **Secondary Indexes**: Full support via `pkg/storage/index/` with normal and unique index types
2. **Batch Operations**: Storage-level atomic batches via `storage.Batch` interface
3. **Multiple Codecs**: JSON, YAML, TOML, MsgPack, BSON (always available), plus CBOR, Protobuf (with build tags)

### Caching Architecture

The DAO intentionally does **not** include a built-in cache layer. PebbleDB provides transparent block-level caching (128MB-2GB configurable) that makes an additional DAO cache redundant and counterproductive:

- **Block Cache**: Configurable 128MB (default) to 2GB (high-performance)
- **MemTable**: In-memory write buffer before flush
- **Bloom Filters**: 10 bits per key (~1% false positive rate)
- **Cache Metrics**: `cache_hits_total` and `cache_misses_total` via Prometheus

Application-specific caching (e.g., frequently accessed user objects, expensive transformations) should be implemented **above** the DAO layer, not within it.

### Future Enhancements

1. **Cursor-Based Pagination**: For efficient deep pagination
2. **DAO-Level Transactions**: Multi-entity atomic operations (storage batches exist)
3. **Query Builder**: Type-safe filtering and sorting

### Extensibility Points

Custom implementations can:
- Provide alternative ID generation strategies
- Implement custom codecs
- Add application-level caching above the DAO
- Wrap DAOs with middleware (logging, metrics, etc.)

## Next Steps

- Review Configuration Options for tuning
- See Getting Started for practical examples
- Check README for overview and quick start


---

## QuicBaaS DAO (Data Access Object)

The GenericDAO package provides a type-safe, storage-agnostic data access layer for QuicBaaS. It abstracts CRUD operations and pagination over any storage backend (Raft, PebbleDB, Memory), offering a clean, consistent API for persisting and querying entities.

### Overview

GenericDAO is QuicBaaS's unified data access pattern that eliminates boilerplate code while providing:

- **Type Safety**: Full compile-time type checking with Go generics
- **Storage Agnostic**: Works seamlessly with all QuicBaaS backends
- **Pagination**: Built-in cursor-based pagination with `Page()` and `ForEachPage()`
- **Multi-Codec Support**: Choose your serialization format (JSON, YAML, TOML, MsgPack, BSON)
- **Auto ID Generation**: Automatic timestamp-based ID assignment
- **Context Aware**: All operations support context cancellation and timeouts

### Key Features

#### Type-Safe CRUD Operations

```go
type User struct {
    ID    uint64 `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func (u *User) EntityID() uint64      { return u.ID }
func (u *User) SetEntityID(id uint64) { u.ID = id }

// Create DAO
userDAO, err := service.CreateDAO*User *User { return &User{} })

// Save (ID auto-generated if 0)
user := &User{Name: "Alice", Email: "alice@example.com"}
err = userDAO.Save(ctx, user)

// Get by ID
retrieved, err := userDAO.Get(ctx, user.EntityID())

// Delete
err = userDAO.Delete(ctx, user)

// Count
total, err := userDAO.Count(ctx)
```

#### Built-in Pagination

```go
// Retrieve a single page
result, err := userDAO.Page(ctx, dao.PageQuery{
    Page:      1,
    PageSize:  25,
    SortOrder: dao.SortAscending,
})

// Process all pages with callback
err = userDAO.ForEachPage(ctx, dao.NewPageQuery(), func(users []*User) error {
    for _, user := range users {
        fmt.Printf("User: %s <%s>\n", user.Name, user.Email)
    }
    return nil
})
```

#### Multi-Codec Support

```go
// Use MsgPack for high performance
userDAO, err := service.CreateDAO*User *User { return &User{} },
    dao.WithCodec(dao.CodecMsgPack))

// Use JSON for debugging
debugDAO, err := service.CreateDAO*User *User { return &User{} },
    dao.WithCodec(dao.CodecJSON))
```

#### Works with All Backends

The same DAO code works transparently across:

- **Memory Mode**: In-memory storage for testing
- **Local Mode**: Direct PebbleDB access for single-node deployments
- **Cluster Mode**: Raft-replicated storage across multiple nodes

### Quick Start

#### 1. Define Your Entity

Implement the `dao.Entity` interface:

```go
type Session struct {
    ID        uint64    `json:"id"`
    UserID    uint64    `json:"user_id"`
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
}

func (s *Session) EntityID() uint64      { return s.ID }
func (s *Session) SetEntityID(id uint64) { s.ID = id }
```

#### 2. Create a DAO

```go
// Using service.CreateDAO (recommended)
sessionDAO, err := service.CreateDAO*Session *Session { return &Session{} },
    dao.WithShard(1),
    dao.WithCodec(dao.CodecMsgPack),
    dao.WithTimeout(30*time.Second),
)
```

#### 3. Perform CRUD Operations

```go
// Create
session := &Session{
    UserID:    1001,
    Token:     "abc123",
    ExpiresAt: time.Now().Add(24 * time.Hour),
}
err = sessionDAO.Save(ctx, session)
// session.ID now contains auto-generated ID

// Read
found, err := sessionDAO.Get(ctx, session.ID)

// Update (just save again)
found.ExpiresAt = time.Now().Add(48 * time.Hour)
err = sessionDAO.Save(ctx, found)

// Delete
err = sessionDAO.Delete(ctx, found)
```

### Architecture

```
┌─────────────────────────────────────┐
│       Application Layer              │
│   (Your business logic)              │
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│         GenericDAO[E]                │
│  - Type-safe CRUD operations         │
│  - Pagination (Page, ForEachPage)    │
│  - Auto ID generation                │
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│       Codec Layer                    │
│  JSON | YAML | TOML | MsgPack | BSON│
└──────────────┬──────────────────────┘
               │
               v
┌─────────────────────────────────────┐
│      interfaces.KVStore              │
│  - Put, Get, Delete, Scan            │
└──────────────┬──────────────────────┘
               │
       ┌───────┴───────┬───────────┐
       v               v           v
┌──────────┐   ┌──────────┐   ┌──────────┐
│  Memory  │   │ PebbleDB │   │   Raft   │
│ KVStore  │   │ KVStore  │   │ KVStore  │
└──────────┘   └──────────┘   └──────────┘
```

### Key Encoding Scheme

All entities are stored with a consistent key format:

```
<entityType>/<id>
```

Examples:
- `users/12345`
- `sessions/98765`
- `roles/1001`

This scheme enables:
- Efficient prefix scanning for pagination
- Isolation between entity types
- Simple ID-based lookups

### Performance Considerations

#### Codec Selection

| Codec     | Speed       | Size    | Human-Readable | Use Case                    |
|-----------|-------------|---------|----------------|-----------------------------|
| JSON      | Moderate    | Large   | Yes            | Debugging, API responses    |
| YAML      | Slow        | Large   | Yes            | Configuration, documentation|
| TOML      | Slow        | Large   | Yes            | Configuration files         |
| MsgPack   | Very Fast   | Small   | No             | Production, high performance|
| BSON      | Fast        | Medium  | No             | MongoDB compatibility       |

**Recommendation**: Use `CodecMsgPack` for production workloads and `CodecJSON` for development/debugging.

#### Pagination Best Practices

- Use reasonable page sizes (25-100 records)
- Process large datasets with `ForEachPage()` instead of loading all at once
- Consider the total dataset size when setting `PageSize`

### Documentation

- Getting Started Guide - Step-by-step tutorial
- Configuration Options - All configuration options explained
- [Architecture Details](./architecture.md) - Deep dive into design and implementation

### Error Handling

The DAO package uses typed errors for precise error handling:

```go
user, err := userDAO.Get(ctx, 999)
if dao.IsNotFound(err) {
    // Handle not found case
    return nil, fmt.Errorf("user not found")
}
if err != nil {
    // Handle other errors
    return nil, err
}
```

Common error types:
- `ErrNotFound` - Entity doesn't exist
- `ErrInvalidEntityID` - Invalid or zero ID
- `ErrSerializationFailed` - Failed to encode entity
- `ErrDeserializationFailed` - Failed to decode entity
- `ErrInvalidConfig` - Invalid DAO configuration

### Thread Safety

GenericDAO instances are safe for concurrent use. Multiple goroutines can share a single DAO instance and perform operations simultaneously.

### Examples

See the getting-started.md guide for complete working examples including:
- User management system
- Session storage
- Multi-tenant data isolation
- Batch processing with pagination


---

## Storage Engine

QuicBaaS uses [PebbleDB](https://github.com/cockroachdb/pebble) as its embedded storage engine.

### Why PebbleDB?

Pure Go (no CGo), battle-tested in CockroachDB, high performance, actively developed, LSM-tree architecture for write-heavy workloads.

### Architecture

#### LSM-Tree (Log-Structured Merge-Tree)

PebbleDB uses an LSM-tree architecture:

```
MemTable (RAM)
    ↓
Immutable MemTable (RAM)
    ↓
L0 SST files (Disk) ← Unsorted
    ↓
L1 SST files (Disk) ← Sorted, non-overlapping
    ↓
L2 SST files (Disk) ← Sorted, larger files
    ↓
L3-L6 SST files (Disk) ← Increasingly larger
```

Write path: write to WAL, write to MemTable, convert full MemTable to Immutable MemTable, flush to L0 SST file, background compaction merges and sorts SST files.

Read path: check MemTable, Immutable MemTable, L0 SST files (bloom filter first), L1-L6 SST files (binary search), return value or NotFound.

### Configuration

#### Basic Setup

```go
import "github.com/cockroachdb/pebble"

opts := &pebble.Options{
    Cache:        pebble.NewCache(128 * 1024 * 1024), // 128 MB cache
    MaxOpenFiles: 1000,
    WALDir:       "/var/lib/quicbaas/wal",
}

db, err := pebble.Open("/var/lib/quicbaas/data", opts)
if err != nil {
    return err
}
defer db.Close()
```

#### Production Configuration

```go
opts := &pebble.Options{
    // Memory
    Cache:                pebble.NewCache(512 * 1024 * 1024),  // 512 MB
    MemTableSize:         64 * 1024 * 1024,                    // 64 MB
    MemTableStopWritesThreshold: 4,

    // Files
    MaxOpenFiles:         10000,
    MaxConcurrentCompactions: 3,

    // Write-Ahead Log
    WALDir:               "/var/lib/quicbaas/wal",
    WALMinSyncInterval:   100 * time.Millisecond,

    // Compaction
    L0CompactionThreshold: 4,
    L0StopWritesThreshold: 12,
    LBaseMaxBytes:        64 * 1024 * 1024,                    // 64 MB
    MaxBytesForLevelBase: 512 * 1024 * 1024,                   // 512 MB
    MaxBytesForLevelMultiplier: 10,

    // Bloom filters
    Levels: make([]pebble.LevelOptions, 7),
}

// Configure bloom filters for all levels
for i := range opts.Levels {
    opts.Levels[i].FilterPolicy = bloom.FilterPolicy(10)
    opts.Levels[i].Compression = pebble.SnappyCompression
}
```

### Operations

#### Put

```go
func (s *Storage) Put(key, value []byte) error {
    return s.db.Set(key, value, pebble.Sync)
}
```

Options: `pebble.Sync` (durable but slower), `pebble.NoSync` (faster but less durable).

#### Get

```go
func (s *Storage) Get(key []byte) ([]byte, error) {
    value, closer, err := s.db.Get(key)
    if err != nil {
        if err == pebble.ErrNotFound {
            return nil, ErrNotFound
        }
        return nil, err
    }
    defer closer.Close()

    // Copy value before closer
    result := make([]byte, len(value))
    copy(result, value)
    return result, nil
}
```

#### Delete

```go
func (s *Storage) Delete(key []byte) error {
    return s.db.Delete(key, pebble.Sync)
}
```

#### Batch Operations

```go
func (s *Storage) BatchPut(entries map[string][]byte) error {
    batch := s.db.NewBatch()
    defer batch.Close()

    for key, value := range entries {
        if err := batch.Set([]byte(key), value, nil); err != nil {
            return err
        }
    }

    return batch.Commit(pebble.Sync)
}
```

Benefits: atomic (all or nothing), faster than individual writes, single WAL entry.

#### Range Scan

```go
func (s *Storage) Scan(start, end []byte) (Iterator, error) {
    iter := s.db.NewIter(&pebble.IterOptions{
        LowerBound: start,
        UpperBound: end,
    })

    return &PebbleIterator{iter: iter}, nil
}

type PebbleIterator struct {
    iter *pebble.Iterator
}

func (it *PebbleIterator) Next() bool {
    return it.iter.Next()
}

func (it *PebbleIterator) Key() []byte {
    return it.iter.Key()
}

func (it *PebbleIterator) Value() []byte {
    return it.iter.Value()
}

func (it *PebbleIterator) Close() error {
    return it.iter.Close()
}
```

### Key Encoding

QuicBaaS uses a structured key encoding scheme:

#### Primary Keys

```
Format: {entity_type}\x00{id}
Example: user\x00\x00\x00\x00\x00\x00\x00\x01
```

```go
func EncodePrimaryKey(entityType string, id uint64) []byte {
    buf := make([]byte, len(entityType)+1+8)
    n := copy(buf, entityType)
    buf[n] = 0 // Null terminator
    binary.BigEndian.PutUint64(buf[n+1:], id)
    return buf
}

func DecodePrimaryKey(key []byte) (string, uint64, error) {
    idx := bytes.IndexByte(key, 0)
    if idx == -1 {
        return "", 0, errors.New("invalid key format")
    }

    entityType := string(key[:idx])
    id := binary.BigEndian.Uint64(key[idx+1:])
    return entityType, id, nil
}
```

#### Index Keys

```
Format: idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}
Example: idx\x00user\x00email\x00john@example.com\x00\x00\x00\x00\x00\x00\x00\x01
```

```go
func EncodeIndexKey(entityType, indexName string, value []byte, id uint64) []byte {
    parts := [][]byte{
        []byte("idx"),
        []byte(entityType),
        []byte(indexName),
        value,
        make([]byte, 8),
    }

    binary.BigEndian.PutUint64(parts[4], id)

    // Join with null bytes
    return bytes.Join(parts, []byte{0})
}
```

### Indexing

#### Secondary Indexes

Secondary indexes enable fast lookups by non-primary key fields:

```go
type IndexManager struct {
    storage Storage
    indexes map[string]*Index
    mu      sync.RWMutex
}

type Index struct {
    Name       string
    EntityType string
    Field      string
}

// Insert adds an entry to the index
func (im *IndexManager) Insert(entityType, indexName string, value []byte, id uint64) error {
    key := EncodeIndexKey(entityType, indexName, value, id)
    return im.storage.Put(key, nil)  // Value is nil, key contains everything
}

// Lookup finds all IDs with the given index value
func (im *IndexManager) Lookup(entityType, indexName string, value []byte) ([]uint64, error) {
    prefix := EncodeIndexKey(entityType, indexName, value, 0)[:len(value)+len(indexName)+len(entityType)+3]

    iter, err := im.storage.Scan(prefix, NextKey(prefix))
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    var ids []uint64
    for iter.Next() {
        _, _, _, id, err := DecodeIndexKey(iter.Key())
        if err != nil {
            return nil, err
        }
        ids = append(ids, id)
    }

    return ids, nil
}
```

### Performance Optimization

#### Bloom Filters

Bloom filters reduce disk reads for non-existent keys:

```go
opts.Levels[i].FilterPolicy = bloom.FilterPolicy(10)  // 10 bits per key
```

Trade-off: more bits reduce false positives but increase memory usage. Sweet spot: 10-15 bits per key.

#### Block Cache

Cache frequently accessed blocks in memory:

```go
cache := pebble.NewCache(512 * 1024 * 1024)  // 512 MB
defer cache.Unref()

opts := &pebble.Options{
    Cache: cache,
}
```

Sizing: 10-20% of data size, minimum 128 MB, maximum 50% of available RAM.

#### Compression

Reduce disk space and I/O:

```go
opts.Levels[i].Compression = pebble.SnappyCompression
```

Options: `NoCompression` (fastest), `SnappyCompression` (fast, good ratio), `ZstdCompression` (better compression, slower).

Recommendation: L0-L2 use Snappy (hot data), L3-L6 use Zstd (cold data).

#### Compaction

Tune compaction for workload:

```go
opts := &pebble.Options{
    L0CompactionThreshold:      4,   // Start compaction
    L0StopWritesThreshold:      12,  // Stop writes if too many L0 files
    MaxConcurrentCompactions:   3,   // Parallel compaction threads
    MaxBytesForLevelBase:       512 * 1024 * 1024,  // 512 MB
    MaxBytesForLevelMultiplier: 10,  // Each level 10x larger
}
```

### Monitoring

#### Metrics

```go
metrics := db.Metrics()

fmt.Printf("MemTable size: %d bytes\n", metrics.MemTable.Size)
fmt.Printf("L0 files: %d\n", metrics.Levels[0].NumFiles)
fmt.Printf("Total size: %d bytes\n", metrics.DiskSpaceUsage())
fmt.Printf("Read amplification: %.2f\n", metrics.ReadAmp())
fmt.Printf("Write amplification: %.2f\n", metrics.WriteAmp())
```

Key metrics: Read Amplification (disk reads per logical read), Write Amplification (bytes written to disk per byte written), Space Amplification (disk space used vs logical data size).

#### Compaction Stats

```go
fmt.Printf("Compactions: %d\n", metrics.Compact.Count)
fmt.Printf("Estimated debt: %d bytes\n", metrics.Compact.EstimatedDebt)
fmt.Printf("In progress compactions: %d\n", metrics.Compact.NumInProgress)
```

### Backup and Recovery

#### Snapshot (Checkpoint)

```go
// Create checkpoint
err := db.Checkpoint("/backup/checkpoint")
```

Use cases: backups, replication, testing.

#### Restore

```go
// Close existing DB
db.Close()

// Copy checkpoint to data directory
os.RemoveAll("/var/lib/quicbaas/data")
os.Rename("/backup/checkpoint", "/var/lib/quicbaas/data")

// Reopen
db, err := pebble.Open("/var/lib/quicbaas/data", opts)
```

### Best Practices

DO: use batches for multiple writes, set appropriate cache size, enable bloom filters, monitor metrics regularly, use compression for large values.

DON'T: store very large values (>10 MB), ignore compaction metrics, use very small cache (<128 MB), disable WAL, store many small values in one key.

### Troubleshooting

#### High Write Amplification

Causes: too many L0 files, too frequent compactions, small L0 files.

Solutions: increase MemTableSize, increase L0CompactionThreshold, increase MaxBytesForLevelBase.

#### High Read Amplification

Causes: many levels to check, poor bloom filter tuning, excessive L0 files.

Solutions: increase bloom filter bits, reduce L0 files (increase compaction), increase cache size.

#### Out of Disk Space

Causes: compaction debt, too many snapshots, large WAL files.

Solutions: manual compaction, delete old snapshots, increase MaxConcurrentCompactions.

### References

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning Guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)

---

**Last Updated:** 2025-10-06


---

## Unified Sharding Architecture

This document describes QuicBaaS's unified sharding architecture, which provides a consistent approach to storage partitioning and Raft consensus across all deployment modes.

### Overview

QuicBaaS's unified sharding architecture separates physical storage organization (partitions) from distributed consensus (shards), enabling flexible deployment configurations from single-node development to multi-node production clusters.

The architecture provides:

- **Mode-based deployment**: Choose between standalone, cluster, or devcluster modes
- **Storage partitioning**: Organize data across multiple storage locations
- **Raft sharding**: Distribute consensus across multiple shard groups
- **Pluggable state machines**: Register custom state machines for specialized workloads

### Terminology

| Term | Definition |
|------|------------|
| **Partition** | A physical storage location with its own data directory. Partitions isolate data for performance, capacity, or operational reasons. |
| **Shard** | A Raft consensus group (QuicRaft ShardID). Each shard maintains its own replicated log and state machine. |
| **State Machine** | The application logic that processes Raft log entries. Different shards can use different state machine types. |
| **Mode** | The server deployment mode: standalone, cluster, or devcluster. |

### Benefits

1. **Performance isolation**: Place hot data on fast NVMe storage, cold data on slower drives
2. **Capacity scaling**: Grow storage by adding partitions to new devices
3. **Workload specialization**: Use different state machines for different data types (KV, vector, etc.)
4. **Operational flexibility**: Backup, restore, or migrate partitions independently
5. **Development simplicity**: DevCluster mode provides multi-node testing on a single machine

### Server Modes

QuicBaaS supports three server modes, configured via `server.mode`:

#### Standalone Mode

Single-node deployment without Raft consensus. Ideal for development, testing, or applications that don't require high availability.

```yaml
server:
  mode: standalone
```

**Characteristics:**
- No Raft consensus overhead
- Direct storage access (PebbleDB, memory, or file)
- Single point of failure
- Simplest deployment model

**Use cases:**
- Local development
- CI/CD testing
- Edge deployments where HA is not required
- Embedded library usage

#### Cluster Mode

Multi-node deployment with Raft consensus for distributed high availability. Production-ready for mission-critical workloads.

```yaml
server:
  mode: cluster
```

**Characteristics:**
- Full Raft consensus for strong consistency
- Automatic leader election and failover
- Replicated state across nodes
- Supports multiple shards per cluster

**Use cases:**
- Production deployments
- Distributed databases
- Multi-region replication
- High-availability requirements

#### DevCluster Mode

Local multi-node cluster for development testing. Runs multiple nodes on a single machine with auto-configured ports.

```yaml
server:
  mode: devcluster
```

**Characteristics:**
- Multiple local nodes with Raft consensus
- Auto-incremented ports per node
- Simplified configuration
- Full cluster behavior for testing

**Use cases:**
- Testing cluster features locally
- Integration testing
- CI/CD cluster validation
- Learning Raft behavior

#### Mode Selection Criteria

| Requirement | Recommended Mode |
|-------------|-----------------|
| Development/testing | `standalone` or `devcluster` |
| Production HA | `cluster` |
| Embedded library | `standalone` with `server.embedded: true` |
| CI/CD testing | `standalone` (unit tests) or `devcluster` (integration) |
| Multi-node local testing | `devcluster` |

### Storage Partitions

Partitions provide physical data isolation at the storage layer. Each partition has its own data directory and optional configuration overrides.

#### Partition Configuration

```yaml
storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas/default
      wal_dir: /var/lib/quicbaas/default/wal
      cache_size: 268435456  # 256MB

    users:
      data_dir: /mnt/nvme1/users
      cache_size: 536870912  # 512MB for hot data

    vectors:
      data_dir: /mnt/nvme2/vectors
      cache_size: 1073741824  # 1GB for vector indexes
```

#### Partition Fields

| Field | Type | Description |
|-------|------|-------------|
| `data_dir` | string | Absolute path to partition data directory (required) |
| `wal_dir` | string | Optional separate WAL directory (defaults to `data_dir/wal`) |
| `cache_size` | int64 | Cache size in bytes (overrides global default) |

#### Partition Routing

The partition manager routes operations to partitions based on entity type prefixes. The DAO layer automatically directs entities to their configured partitions.

```go
// Example: Configure entity routing
factoryConfig := &dao.FactoryConfig{
    PartitionedProvider: dao.CreatePartitionedProvider(partitionManager),
    DefaultPartition:    "default",
    EntityRouting: map[string]string{
        "user":   "users",
        "vector": "vectors",
    },
}
```

#### Per-Partition Characteristics

- **Independent directories**: Each partition writes to its own directory tree
- **Separate WAL**: Optional per-partition write-ahead log
- **Cache isolation**: Per-partition cache sizing for workload optimization
- **Snapshot granularity**: Partitions can be snapshotted independently

### Raft Shards (Cluster Mode)

In cluster mode, shards provide distributed consensus. Each shard is a Raft group with its own leader election and replicated state.

#### Shard Configuration

```yaml
shards:
  - id: 1
    partition: primary
    state_machine: fast

  - id: 2
    partition: vectors
    state_machine: vector

  - id: 3
    partition: primary
    state_machine: minimal
```

#### Shard Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint64 | QuicRaft ShardID (must be > 0, unique per cluster) |
| `partition` | string | Name of the partition this shard uses for storage |
| `state_machine` | string | State machine type: `fast`, `vector`, `minimal`, or custom |

#### Shard-to-Partition Mapping

Multiple shards can share a partition, or each shard can have its own partition:

```
Shard 1 (fast)    ----\
                       >---- Partition: primary (/mnt/nvme1/primary)
Shard 3 (minimal) ----/

Shard 2 (vector)  --------- Partition: vectors (/mnt/nvme2/vectors)
```

#### State Machine Registry

QuicBaaS includes a state machine registry with built-in implementations:

| Name | Description | Use Case |
|------|-------------|----------|
| `fast` | High-performance KV operations | Default, general-purpose |
| `vector` | Vector operations with HNSW indexing | Similarity search, embeddings |
| `minimal` | Simple in-memory with optional persistence | Testing, lightweight workloads |

#### Custom State Machine Registration

State machines are managed by the qrdb database engine. Custom state machine registration is performed through the qrdb SDK's admin client. QuicBaaS delegates all Raft consensus and state machine lifecycle management to the qrdb engine.

```yaml
# Use in configuration
shards:
  - id: 4
    partition: custom_data
    state_machine: custom
```

### Configuration Examples

#### Standalone Mode

Minimal configuration for single-node deployment:

```yaml
server:
  mode: standalone
  grpc:
    enabled: true
    port: 9090
  rest:
    enabled: true
    port: 8080

storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas/default
```

#### Cluster Mode

Production multi-node configuration with multiple partitions and shards:

```yaml
server:
  mode: cluster
  grpc:
    enabled: true
    port: 9090
  rest:
    enabled: true
    port: 8080

storage:
  backend: pebble
  partitions:
    primary:
      data_dir: /mnt/nvme1/primary
      cache_size: 536870912  # 512MB
    vectors:
      data_dir: /mnt/nvme2/vectors
      cache_size: 1073741824  # 1GB

shards:
  - id: 1
    partition: primary
    state_machine: fast
  - id: 2
    partition: vectors
    state_machine: vector

raft:
  node_id: 1
  raft_address: "10.0.1.1:63001"
  listen_address: "10.0.1.1:63001"
  join_addresses:
    - "10.0.1.2:63001"
    - "10.0.1.3:63001"
  mutual_tls: true
  election_rtt: 10
  heartbeat_rtt: 1
  snapshot_entries: 10000
```

#### DevCluster Mode

Local multi-node testing configuration:

```yaml
server:
  mode: devcluster
  grpc:
    enabled: true
  rest:
    enabled: true

devcluster:
  node_count: 3
  data_dir: ./dev-data
  base_ports:
    grpc: 9090  # Nodes: 9090, 9091, 9092
    rest: 8080  # Nodes: 8080, 8081, 8082
    quic: 10443
    mcp: 11443
    raft: 63001  # Nodes: 63001, 63002, 63003
  unix_socket_pattern: /tmp/quicbaas-node-{n}.sock
```

DevCluster automatically generates per-node configurations:
- Node 1: gRPC=9090, REST=8080, Raft=63001, Data=./dev-data/node-1
- Node 2: gRPC=9091, REST=8081, Raft=63002, Data=./dev-data/node-2
- Node 3: gRPC=9092, REST=8082, Raft=63003, Data=./dev-data/node-3

### Embedding QuicBaaS

QuicBaaS can be embedded as a library in Go applications, providing direct access to the service layer without network overhead.

#### Basic Embedded Usage

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    "github.com/jeremyhahn/go-quicbaas/pkg/server"
)

func main() {
    // Use embedded configuration (no network listeners)
    cfg := config.EmbeddedConfig()
    cfg.Storage.DataDir = "./myapp-data"
    cfg.Storage.WALDir = "./myapp-data/wal"

    // Create and start server
    srv, err := server.NewServer(cfg)
    if err != nil {
        log.Fatal(err)
    }

    if err := srv.Start(); err != nil {
        log.Fatal(err)
    }
    defer srv.Shutdown(context.Background())

    // Access the service directly
    svc := srv.Service()

    // Use the service
    ctx := context.Background()
    _, err = svc.Put(ctx, &service.PutRequest{
        Key:   []byte("mykey"),
        Value: []byte("myvalue"),
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

#### Embedded with Custom Shard Configuration

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    "github.com/jeremyhahn/go-quicbaas/pkg/server"
)

func main() {
    // Configure with shard assignments
    // State machines are managed by the qrdb engine
    cfg := config.EmbeddedConfig()
    cfg.Server.Mode = config.ServerModeCluster
    cfg.Shards = []*config.ShardConfig{
        {
            ID:           1,
            Partition:    "default",
            StateMachine: "fast",
        },
    }

    // Create and start server
    srv, err := server.NewServer(cfg)
    if err != nil {
        log.Fatal(err)
    }

    if err := srv.Start(); err != nil {
        log.Fatal(err)
    }
    defer srv.Shutdown(context.Background())
}
```

### Configuration Reference

#### Required Server Mode

QuicBaaS requires an explicit `server.mode` setting:

- `standalone` - Single-node operation without Raft consensus
- `cluster` - Multi-node operation with Raft consensus
- `devcluster` - Local multi-node development cluster

**Cluster Configuration Example:**
```yaml
server:
  mode: cluster

storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas

raft:
  node_id: 1
  raft_address: "10.0.1.1:63001"
```

#### Configuration Steps

1. **Set server mode** explicitly via `server.mode`
2. **Choose storage backend** (`memory`, `file`, or `pebble`)
3. **Define partitions** in `storage.partitions` for data isolation
4. **Configure shards** if using multiple state machines (cluster mode)
5. **Set Raft parameters** for cluster/devcluster modes

### API Reference

#### Key Packages

| Package | Description |
|---------|-------------|
| `pkg/server/devcluster` | DevCluster node manager |
| `pkg/dao` | Data access objects |
| `pkg/config` | Configuration structures |
| `github.com/jeremyhahn/go-qrdb/sdk/go` | qrdb SDK for storage, Raft, and state machine operations |

Storage partitions, state machine registries, and Raft consensus are managed by the qrdb database engine, accessed via the qrdb SDK.

#### DevCluster Manager

```go
// pkg/server/devcluster/manager.go

// Manager manages local multi-node clusters
type Manager struct {
    // ...
}

// NodeInstance represents a local cluster node
type NodeInstance struct {
    NodeID     uint64
    GRPCPort   int
    RESTPort   int
    RaftPort   int
    DataDir    string
    Config     *config.Config
}

// Nodes returns all node instances
func (m *Manager) Nodes() []*NodeInstance
```


---

## QuicBaaS Shard ID Allocation

QuicBaaS uses Raft consensus for distributed operations. Each subsystem that requires consensus operates on its own Raft shard. This document defines the shard ID allocation strategy and reserved ranges.

### Shard Ranges

The BaaS multi-tenant architecture uses a simplified shard allocation scheme
managed by the central registry in `pkg/shards/`:

| Range | Constant | Purpose | Description |
|-------|----------|---------|-------------|
| 1 | `PlatformShardID` | Platform | Platform database (tenant registry, global config) |
| 2-99 | `ReservedMax = 100` | Reserved | Future platform services |
| 100+ | `TenantShardMin = 100` | Tenant Databases | Dynamically allocated tenant shards |

### Platform Shard (ID 1)

The platform shard hosts the platform database which stores:

- Tenant registry records (`platform:tenant:{tenantID}`)
- Shard allocation state
- Global platform configuration

Access to the platform shard requires the `db:admin` role, enforced by
`GuardPlatformShard` at gRPC and REST transport layers.

### Tenant Shards (100+)

Each tenant database (system and user) receives its own shard ID, allocated
monotonically by the `ShardAllocator` in `pkg/database/shard_allocator.go`.

Example allocation:

| Shard ID | Database | Purpose |
|----------|----------|---------|
| 1 | platform | Platform database |
| 101 | acme_system | Tenant "acme" system database |
| 102 | acme_orders | Tenant "acme" user database |
| 103 | acme_logs | Tenant "acme" user database |
| 104 | globex_system | Tenant "globex" system database |
| 105 | globex_app | Tenant "globex" user database |

### Shard Classification

```go
const (
    PlatformShardID uint64 = 1
    ReservedMax     uint64 = 100
    TenantShardMin  uint64 = 100
)

IsPlatformShard(id) bool  // id == 1
IsReservedShard(id) bool  // id > 1 && id < 100
IsTenantShard(id)   bool  // id >= 100
```

### Access Control

Two enforcement points prevent unauthorized access to the platform shard:

1. **gRPC interceptor** (`pkg/api/grpc/interceptor_shard.go`): Checks `GuardPlatformShard` on the shard ID from request metadata.
2. **REST middleware** (`pkg/api/rest/middleware_shard.go`): Same guard on the `X-Shard-ID` header.

```go
func GuardPlatformShard(id uint64, hasAdminRole bool) error
```

Returns `PlatformShardAccessDeniedError` when `id == PlatformShardID` and the caller lacks admin role.

### State Machine Types

Each shard uses a specific state machine type optimized for its workload:

| Type | Description | Used By |
|------|-------------|---------|
| `fast` | High-performance KV operations with binary protocol | Platform, tenant system databases |
| `vector` | Vector operations with HNSW index support | Vector engine databases |
| `timeseries` | Time series operations with msgpack encoding | TimeSeries engine databases |
| `rag` | RAG operations with document/chunk management | RAG engine databases |

### Best Practices

1. **Never use shard IDs 2-99 for tenant databases** -- These are reserved for future platform services.

2. **Use the ShardAllocator for tenant shards** -- Do not manually assign shard IDs; let the allocator assign them monotonically.

3. **Use consistent shard IDs across all nodes** -- All nodes in a cluster must use the same shard ID configuration.

4. **Monitor shard health** -- Each shard has its own Raft group with leader election and replication.

### API Reference

Shard ID allocation and cluster management are handled by the qrdb database engine, accessed via the qrdb SDK (`github.com/jeremyhahn/go-qrdb/sdk/go`). QuicBaaS configures shard IDs through the `pkg/shards/` registry, and the qrdb engine manages shard lifecycle, Raft consensus groups, and state machine assignments.

### Cross-References

- Multi-Tenant BaaS Architecture
- [Shard Consolidation Design](../design/shard-consolidation.md)


---

## Selective Indexing & Vector Indexing

### Selective Field Indexing

QuicBaaS supports selective field indexing on KV records. Only fields marked with `index:"true"` struct tags are indexed, conserving resources by avoiding unnecessary index creation.

### Why Selective Indexing?

Indexing every field has costs: storage overhead, write performance impact, and memory usage. Selective indexing balances query performance with resource efficiency.

### How to Use Selective Indexing

#### 1. Define Your Entity Models

Create struct types with `index:"true"` tags on fields you want to query:

```go
package models

type User struct {
    // ID is always stored, but you control if it's indexed
    ID uint64 `json:"id"`

    // Indexed fields (marked with index:"true")
    Email     string `json:"email" index:"true"`
    Username  string `json:"username" index:"true"`
    Status    string `json:"status" index:"true"`
    CreatedAt int64  `json:"created_at" index:"true"`

    // NOT indexed - saves resources
    FullName          string `json:"full_name"`
    Bio               string `json:"bio"`
    PasswordHash      string `json:"password_hash"`
    ProfilePictureURL string `json:"profile_picture_url"`
}
```

#### 2. Register Entity Types

Before storing entities, register their types with the QuicBaaS service layer. Entity type registration and indexing are managed by the qrdb database engine via the qrdb SDK.

```go
import (
    "reflect"
    "github.com/jeremyhahn/go-quicbaas/examples/models"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func main() {
    // Register entity types via the service layer
    svc := service.New(/* ... */)

    if err := svc.RegisterEntityType("user", reflect.TypeOf(models.User{})); err != nil {
        panic(fmt.Sprintf("Failed to register User model: %v", err))
    }
}
```

#### 3. Query Indexed Fields

Once registered, you can query entities by their indexed fields:

```go
// Query users by status
results, err := client.Query(ctx, &pb.QueryRequest{
    EntityType: "user",
    IndexName:  "status",
    Value:      []byte("active"),
})

// Query users by email
results, err := client.Query(ctx, &pb.QueryRequest{
    EntityType: "user",
    IndexName:  "email",
    Value:      []byte("user@example.com"),
})
```

### Complete Example

```go
package main

import (
    "fmt"
    "reflect"

    "github.com/jeremyhahn/go-quicbaas/examples/models"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func main() {
    // Create service (entity types are registered via the service layer;
    // indexing is handled by the qrdb engine)
    svc := service.New(/* ... */)

    // Register entity types with their models
    if err := svc.RegisterEntityType("user", reflect.TypeOf(models.User{})); err != nil {
        panic(fmt.Sprintf("Failed to register User model: %v", err))
    }

    if err := svc.RegisterEntityType("product", reflect.TypeOf(models.Product{})); err != nil {
        panic(fmt.Sprintf("Failed to register Product model: %v", err))
    }

    if err := svc.RegisterEntityType("order", reflect.TypeOf(models.Order{})); err != nil {
        panic(fmt.Sprintf("Failed to register Order model: %v", err))
    }

    fmt.Println("Entity types registered successfully!")
    fmt.Println("\nIndexed fields:")
    fmt.Println("  User: Email, Username, Status, CreatedAt")
    fmt.Println("  Product: SKU, Category, Status, Price, StockQuantity")
    fmt.Println("  Order: CustomerID, Status, OrderDate, TotalAmount")
    fmt.Println("\nNon-indexed fields will NOT use extra storage or processing.")
}
```

### Best Practices

DO index: fields used in queries (status, category, email), foreign keys, date/time fields, enum-like fields.

DON'T index: large text fields, binary data, sensitive data not queried, fields with unique values per entity, nested objects or arrays.

#### Example: Product Model

```go
type Product struct {
    ID uint64 `json:"id"`

    // Index these - commonly queried
    SKU           string  `json:"sku" index:"true"`
    Category      string  `json:"category" index:"true"`
    Status        string  `json:"status" index:"true"`
    Price         float64 `json:"price" index:"true"`
    StockQuantity int     `json:"stock_quantity" index:"true"`

    // Don't index these - not queried or too large
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Metadata    map[string]interface{} `json:"metadata"`
}
```

### Backward Compatibility

For unregistered entity types, QuicBaaS falls back to indexing all JSON fields automatically. This ensures existing code continues to work without changes.

```go
// Without registration - all fields are indexed (legacy behavior)
client.Put(ctx, &pb.PutRequest{
    EntityType: "unregistered_type",
    Data:       jsonData,
})

// With registration - only tagged fields are indexed
minimalSM.RegisterEntityType("user", reflect.TypeOf(models.User{}))
client.Put(ctx, &pb.PutRequest{
    EntityType: "user",
    Data:       jsonData,
})
```

### Performance Considerations

Memory usage: each indexed field creates an in-memory map, size grows with unique values × entities.

Write performance: each indexed field requires one map insertion per write, non-indexed fields have zero overhead.

Query performance: indexed queries are O(1) lookup + O(n) iteration, non-indexed queries require full scan (not supported).

### Running the Example

```bash
# Build and run the selective indexing example
cd examples/selective_indexing
go run main.go

# Output:
# ✓ Entity types registered successfully!
#
# Indexed fields:
#   User: Email, Username, Status, CreatedAt
#   Product: SKU, Category, Status, Price, StockQuantity
#   Order: CustomerID, Status, OrderDate, TotalAmount
#
# Non-indexed fields will NOT use extra storage or processing.
```

### Implementation Details (Field Indexing)

The selective field indexing system uses Go's reflection to parse struct tags at registration time. `RegisterEntityType()` scans struct tags, `IndexEntityWithType()` only indexes registered fields, unregistered types index all fields for backward compatibility.

### Vector Indexing (Persistent HNSW via qrdb)

**Implemented (2026-05-31):** RAG vector storage has been migrated from the in-process in-memory brute-force index to go-qrdb's persistent, barrier-encrypted HNSW vector primitive, accessed over the qrdb SDK.

#### Architecture

**Locked Architecture:** qrdb owns the vector primitive; quicbaas owns RAG orchestration.

```
Server (lifecycle_rag.go)
  └── rag.NewSystem(cfg, rag.WithVectorIndexFactory(factory))
        └── factory → indexing.NewVectorIndexFromConfig(..., WithVectorClient(client))
              └── qrdbVectorIndex (pkg/rag/indexing/vector_qrdb.go)
                    └── transport.VectorClient (via ShardedClient.ForShard(vectorShardID))
                          └── baasTransport → PebbleVectorStorage (barrier-encrypted)
```

#### Components

**`pkg/rag/indexing/vector_qrdb.go`** — `qrdbVectorIndex` implements `rag.VectorIndex`:
- Delegates every op to `transport.VectorClient` (CreateNamespace, UpsertVector, SearchSimilar, DeleteVector, GetNamespaceInfo).
- `Train()`/`IsTrained()` are no-ops: the qrdb HNSW graph builds lazily as vectors are inserted.
- `Save()`/`Load()` are no-ops: qrdb persists to PebbleDB automatically.
- RAG string IDs (e.g. `ns:abc123:0`) are mapped to qrdb `uint64` IDs via FNV-1a 64-bit hash and recovered via the `rag_id` metadata field on search results.

**`pkg/rag/indexing/vector_adapter.go`** — `NewVectorIndexFromConfig` functional options:
- `WithVectorClient(client)` — injects the qrdb VectorClient.
- `WithTenantID(tenantID)` — prefixes the physical namespace: `{tenantID}:{namespace}`.
- `WithDistanceMetric(metric)` — overrides the distance metric.
- Index type `"qrdb"` is now registered. `"auto"` and `"hnsw"` resolve to qrdb when a VectorClient is present; fall back to in-memory otherwise.

**`pkg/rag/factory.go`** — `rag.NewSystem` now accepts `rag.WithVectorIndexFactory(factory)` to inject the qrdb-backed factory without creating an import cycle between `pkg/rag` and `pkg/rag/indexing`.

**`pkg/server/lifecycle_rag.go`** — `buildRAGSystemOptions()` wires the factory at server startup:
- Resolves the vector shard client via `ShardedClient.ForShard(vectorShardID)`.
- Derives the physical namespace from `config.RAG.IndexPath` (or defaults to `"rag"`).
- System-level RAG uses the system/single-tenant context (empty tenant ID).

#### Tenant Isolation

Tenant isolation is encoded into the qrdb **namespace name** at construction time:
- Physical namespace: `{tenantID}:{logicalNamespace}` (or just `logicalNamespace` for system tenant).
- The `baasTransport.VectorClient` uses the namespace string as the KV key prefix, providing complete storage isolation between tenants without relying on the `tenant.FromContext(ctx)` context key (which uses a different context slot than quicbaas's `iam.tenantContextKey{}`).
- The system-level RAG operates under the system tenant. Per-tenant RAG requires a separate `rag.System` per tenant, constructed with `WithRAGTenantID(tenantID)` propagated through the server's per-tenant initialization flow.

#### In-Memory Fallback

The in-process in-memory brute-force index (`pkg/rag/vector_index.go`) is retained as an explicit `index_type: memory` option for development, testing, and offline scenarios where the qrdb vector service is not available. It is no longer the default when a VectorClient is present.

#### Properties

- **Durability:** Vectors survive daemon restarts (stored in PebbleDB).
- **Barrier encryption:** Vectors are encrypted at the DEK layer by the qrdb storage barrier.
- **Tenant isolation:** Per-tenant namespace prefixes prevent cross-tenant data access.
- **HNSW index:** Approximate nearest-neighbor search with configurable `EfSearch` (mapped from the `nprobe` parameter).


---

## State Machines

State machines implement the business logic layer between Raft consensus and storage, handling CRUD operations, ID generation, field encryption, and index management.

### Overview

State machines implement QuicRaft's state machine interface:

```go
type StateMachine interface {
    // Update applies a batch of log entries (receives context and results slice)
    Update(ctx context.Context, entries []Entry, results []Entry) error

    // Lookup performs a read-only query (receives context)
    Lookup(ctx context.Context, query interface{}) (interface{}, error)

    // SaveSnapshot saves the current state
    SaveSnapshot(io.Writer, sm.SnapshotFileCollection, <-chan struct{}) error

    // RecoverFromSnapshot restores from a snapshot
    RecoverFromSnapshot(io.Reader, []sm.SnapshotFile, <-chan struct{}) error

    // Close releases resources
    Close() error
}
```

### Generic Implementation

QuicBaaS uses Go generics for type-safe, reusable state machines:

```go
type GenericStateMachine[E KeyValueEntity] struct {
    storage   storage.Storage
    idGen     *IDGenerator
    encryptor *Encryptor
    indexer   *IndexManager
    logger    *logger.Logger
    mu        sync.RWMutex
}

type KeyValueEntity interface {
    GetID() uint64
    SetID(uint64)
    GetEntityType() string
}
```

### State Machine Types

QuicRaft supports three state machine types:

#### 1. Regular State Machine (RSM)

Default type:

```go
func NewStateMachine(shardID, nodeID uint64) sm.StateMachine {
    return &GenericStateMachine[Entity]{
        storage:   storage.NewPebbleStorage(dataDir),
        idGen:     NewIDGenerator(),
        encryptor: NewEncryptor(key),
        indexer:   NewIndexManager(storage),
    }
}
```

Characteristics: sequential log application, snapshot support, recovery support, highest consistency.

#### 2. Concurrent State Machine

Allows concurrent Update() calls:

```go
func NewConcurrentStateMachine(shardID, nodeID uint64) sm.ConcurrentStateMachine {
    return &ConcurrentStateMachine[Entity]{
        GenericStateMachine: NewGenericStateMachine[Entity](),
        workers:             runtime.NumCPU(),
        queue:              make(chan *Entry, 1000),
    }
}
```

Characteristics: parallel Update() execution, higher throughput, requires careful locking, same consistency guarantees.

#### 3. On-Disk State Machine

Minimizes memory usage:

```go
func NewOnDiskStateMachine(shardID, nodeID uint64) sm.OnDiskStateMachine {
    return &OnDiskStateMachine[Entity]{
        GenericStateMachine: NewGenericStateMachine[Entity](),
    }
}
```

Characteristics: lower memory footprint, all state on disk, slightly lower performance. QuicRaft manages sessions internally, so no manual session management is needed.

### Operations

#### Put Operation

```go
func (sm *GenericStateMachine[E]) Put(ctx context.Context, entity E) error {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    // 1. Generate ID if not set
    if entity.GetID() == 0 {
        id := sm.idGen.Next()
        entity.SetID(id)
    }

    // 2. Encrypt sensitive fields
    if err := sm.encryptor.Encrypt(entity); err != nil {
        return errors.Wrap(err, "failed to encrypt entity")
    }

    // 3. Serialize entity
    data, err := json.Marshal(entity)
    if err != nil {
        return errors.Wrap(err, "failed to marshal entity")
    }

    // 4. Generate storage key
    key := EncodeKey(entity.GetEntityType(), entity.GetID())

    // 5. Write to storage
    if err := sm.storage.Put(key, data); err != nil {
        return errors.Wrap(err, "failed to store entity")
    }

    // 6. Update indexes
    if err := sm.indexer.UpdateIndexes(entity); err != nil {
        // Rollback
        sm.storage.Delete(key)
        return errors.Wrap(err, "failed to update indexes")
    }

    return nil
}
```

#### Get Operation

```go
func (sm *GenericStateMachine[E]) Get(ctx context.Context, entityType string, id uint64) (E, error) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    // 1. Generate storage key
    key := EncodeKey(entityType, id)

    // 2. Read from storage
    data, err := sm.storage.Get(key)
    if err != nil {
        if errors.Is(err, storage.ErrNotFound) {
            var zero E
            return zero, ErrNotFound
        }
        return zero, errors.Wrap(err, "failed to read entity")
    }

    // 3. Deserialize entity
    var entity E
    if err := json.Unmarshal(data, &entity); err != nil {
        return zero, errors.Wrap(err, "failed to unmarshal entity")
    }

    // 4. Decrypt sensitive fields
    if err := sm.encryptor.Decrypt(&entity); err != nil {
        return zero, errors.Wrap(err, "failed to decrypt entity")
    }

    return entity, nil
}
```

#### Delete Operation

```go
func (sm *GenericStateMachine[E]) Delete(ctx context.Context, entityType string, id uint64) error {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    // 1. Get entity for index cleanup
    entity, err := sm.Get(ctx, entityType, id)
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            return nil  // Idempotent delete
        }
        return err
    }

    // 2. Remove from indexes
    if err := sm.indexer.RemoveIndexes(entity); err != nil {
        return errors.Wrap(err, "failed to remove indexes")
    }

    // 3. Delete from storage
    key := EncodeKey(entityType, id)
    if err := sm.storage.Delete(key); err != nil {
        return errors.Wrap(err, "failed to delete entity")
    }

    return nil
}
```

#### Query Operation

```go
func (sm *GenericStateMachine[E]) Query(ctx context.Context, indexName string, value []byte) ([]E, error) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    // 1. Lookup IDs from index
    ids, err := sm.indexer.Lookup(indexName, value)
    if err != nil {
        return nil, errors.Wrap(err, "failed to lookup index")
    }

    // 2. Fetch entities
    entities := make([]E, 0, len(ids))
    for _, id := range ids {
        entity, err := sm.Get(ctx, entityType, id)
        if err != nil {
            if errors.Is(err, ErrNotFound) {
                continue  // Entity deleted, skip
            }
            return nil, err
        }
        entities = append(entities, entity)
    }

    return entities, nil
}
```

### Update Processing

The Update() method processes Raft log entries. The QuicRaft interface passes context, entries, and a results slice:

```go
func (sm *GenericStateMachine[E]) Update(ctx context.Context, entries []sm.Entry, results []sm.Entry) error {
    for i, entry := range entries {
        // Deserialize command
        var cmd Command
        if err := json.Unmarshal(entry.Cmd, &cmd); err != nil {
            results[i] = sm.Entry{Result: wrapError(err)}
            continue
        }

        // Execute command
        var result interface{}
        var err error

        switch cmd.Type {
        case CommandPut:
            err = sm.Put(ctx, cmd.Entity)
            result = &PutResult{ID: cmd.Entity.GetID()}

        case CommandDelete:
            err = sm.Delete(ctx, cmd.EntityType, cmd.ID)
            result = &DeleteResult{Deleted: err == nil}

        default:
            err = fmt.Errorf("unknown command type: %v", cmd.Type)
        }

        // Serialize result
        if err != nil {
            results[i] = sm.Entry{Result: wrapError(err)}
        } else {
            data, _ := json.Marshal(result)
            results[i] = sm.Entry{Result: data}
        }
    }

    return nil
}
```

### Snapshot Management

#### Save Snapshot

```go
func (sm *GenericStateMachine[E]) SaveSnapshot(w io.Writer, fc sm.SnapshotFileCollection, stop <-chan struct{}) error {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    // 1. Create snapshot writer
    sw := NewSnapshotWriter(w)

    // 2. Write metadata
    metadata := SnapshotMetadata{
        Version:   1,
        Timestamp: time.Now().UnixNano(),
        EntityCount: sm.getEntityCount(),
    }
    if err := sw.WriteMetadata(metadata); err != nil {
        return err
    }

    // 3. Iterate all entities
    iter, err := sm.storage.Scan(nil, nil)
    if err != nil {
        return err
    }
    defer iter.Close()

    for iter.Next() {
        select {
        case <-stop:
            return sm.ErrSnapshotStopped
        default:
        }

        // Write key-value pair
        if err := sw.WriteKV(iter.Key(), iter.Value()); err != nil {
            return err
        }
    }

    return sw.Close()
}
```

#### Recover from Snapshot

```go
func (sm *GenericStateMachine[E]) RecoverFromSnapshot(r io.Reader, files []sm.SnapshotFile, stop <-chan struct{}) error {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    // 1. Clear existing state
    if err := sm.storage.Clear(); err != nil {
        return err
    }

    // 2. Create snapshot reader
    sr := NewSnapshotReader(r)

    // 3. Read metadata
    metadata, err := sr.ReadMetadata()
    if err != nil {
        return err
    }

    // 4. Read all key-value pairs
    for {
        select {
        case <-stop:
            return sm.ErrSnapshotStopped
        default:
        }

        key, value, err := sr.ReadKV()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }

        // Restore to storage
        if err := sm.storage.Put(key, value); err != nil {
            return err
        }
    }

    sm.logger.Info().
        Int64("version", metadata.Version).
        Int64("entity_count", metadata.EntityCount).
        Msg("snapshot recovered")

    return nil
}
```

### ID Generation

#### Hash-Based IDs

```go
type IDGenerator struct {
    counter  uint64
    mu       sync.Mutex
    hasher   hash.Hash64
}

func (g *IDGenerator) Next() uint64 {
    g.mu.Lock()
    defer g.mu.Unlock()

    g.counter++
    return g.counter
}
```

#### Timestamp-Based IDs

```go
func (g *IDGenerator) NextTimestamp() uint64 {
    // Unix nanoseconds as ID
    return uint64(time.Now().UnixNano())
}
```

### Field Encryption

#### Encryption Tags

```go
type User struct {
    ID       uint64 `json:"id" quicbaas:"id"`
    Name     string `json:"name" quicbaas:"index"`
    Email    string `json:"email" quicbaas:"index"`
    Password string `json:"password" quicbaas:"encrypt:aes256"`
    SSN      string `json:"ssn" quicbaas:"encrypt:aes128"`
}
```

#### Encryptor Implementation

```go
type Encryptor struct {
    key256 []byte  // 32 bytes for AES-256
    key128 []byte  // 16 bytes for AES-128
}

func (e *Encryptor) Encrypt(entity interface{}) error {
    v := reflect.ValueOf(entity).Elem()
    t := v.Type()

    for i := 0; i < v.NumField(); i++ {
        field := v.Field(i)
        tag := t.Field(i).Tag.Get("quicbaas")

        if strings.HasPrefix(tag, "encrypt:") {
            algo := strings.TrimPrefix(tag, "encrypt:")
            plaintext := field.String()

            var ciphertext string
            var err error

            switch algo {
            case "aes128":
                ciphertext, err = e.encryptAES128(plaintext)
            case "aes256":
                ciphertext, err = e.encryptAES256(plaintext)
            default:
                return fmt.Errorf("unknown encryption algorithm: %s", algo)
            }

            if err != nil {
                return err
            }

            field.SetString(ciphertext)
        }
    }

    return nil
}
```

### Performance Optimization

#### Batching

```go
// Process multiple updates in single transaction
func (sm *GenericStateMachine[E]) BatchPut(entities []E) error {
    batch := sm.storage.NewBatch()
    defer batch.Close()

    for _, entity := range entities {
        // Process entity
        // Add to batch
        batch.Put(key, value)
    }

    return batch.Commit()
}
```

#### Caching

```go
type CachedStateMachine[E] struct {
    *GenericStateMachine[E]
    cache *lru.Cache
}

func (sm *CachedStateMachine[E]) Get(ctx context.Context, entityType string, id uint64) (E, error) {
    // Check cache first
    key := fmt.Sprintf("%s:%d", entityType, id)
    if v, ok := sm.cache.Get(key); ok {
        return v.(E), nil
    }

    // Cache miss, fetch from storage
    entity, err := sm.GenericStateMachine.Get(ctx, entityType, id)
    if err != nil {
        return entity, err
    }

    // Update cache
    sm.cache.Add(key, entity)
    return entity, nil
}
```

### Best Practices

DO: use appropriate state machine type, implement proper error handling, use batches for bulk operations, monitor snapshot size and frequency, implement caching for hot data.

DON'T: hold locks during I/O, store very large entities (>1 MB), ignore Update() errors, skip snapshot testing, forget to close resources.

### Monitoring

```go
type Metrics struct {
    UpdatesTotal      uint64
    UpdatesFailedTotal uint64
    UpdateDuration    time.Duration
    LookupsTotal      uint64
    SnapshotsTotal    uint64
    SnapshotDuration  time.Duration
}
```

### References

- [QuicRaft State Machine Guide](https://github.com/jeremyhahn/go-quicraft) - Library documentation
- [Go Generics Tutorial](https://go.dev/doc/tutorial/generics)

---

**Last Updated:** 2026-02-23


---

## Write Modes

QuicBaaS offers three write modes with different trade-offs between latency, throughput, and durability.

### Overview

Write modes determine how QuicBaaS handles writes and when it acknowledges them. They impact latency, throughput, durability, and consistency.

### Write Mode Comparison

| Mode | Durability | Latency | Throughput | Data Loss Risk | Use Case |
|------|------------|---------|------------|----------------|----------|
| **Sync** | Strong (Quorum) | 5-10ms | 500-1K/sec | None | Production, financial transactions |
| **WAL Durable** | Medium (Local) | 100-500μs | 10K-50K/sec | Minimal (last 10-100ms) | High-throughput production, logs |
| **Batched** | Strong (Quorum) | 500μs-5ms | 100K+/sec | None | Bulk ingestion, high-volume workloads |

### Sync Mode (Default)

Every write is immediately proposed to Raft and waits for quorum acknowledgment.

**Characteristics:**
- Strongest durability (data committed to quorum before acknowledgment)
- Immediate consistency across cluster
- Highest latency (5-10ms per write)
- Lowest throughput (500-1,000 ops/sec per node)
- Zero data loss on crash or power failure
- Standard behavior for distributed consensus

**Use Cases:**
- Financial transactions
- Critical configuration updates
- Any data that must not be lost
- Production databases requiring strong consistency

**Configuration:**
```yaml
raft:
  write_mode: "sync"
```

### WAL Durable Mode

Writes are acknowledged after WAL write, before Raft consensus.

**Characteristics:**
- Very low latency (100-500μs)
- High throughput (10K-50K ops/sec per node)
- Data durable on local node only
- Risk of data loss if node fails before replication (last 10-100ms of writes)
- Eventual consistency (millisecond-scale lag)

**Use Cases:**
- High-throughput logging systems
- Metrics collection
- Cache warming
- Analytics pipelines
- Applications that can tolerate minimal data loss

**Configuration:**
```yaml
raft:
  write_mode: "wal"
```

### Batched Mode

Writes are aggregated into batches and submitted to Raft as a single operation.

**Characteristics:**
- Excellent throughput (100K+ ops/sec with large batches)
- 100x latency improvement over sync mode
- Full Raft durability (quorum acknowledgment)
- Slight latency variance due to batching delay (configurable)
- Configurable batch size and flush interval
- Zero data loss (batched operations are replicated)

**Use Cases:**
- High-volume workloads
- Bulk data ingestion
- Time-series data collection
- Event logging
- Applications tolerating small batching delays (1-10ms)

**Configuration:**
```yaml
raft:
  write_mode: "batched"
  batch:
    max_size: 100
    flush_interval: "5ms"
```

### Decision Tree

```
Can you tolerate ANY data loss?
├─ NO → High volume?
│       ├─ NO → Sync
│       └─ YES → Batched
└─ YES → WAL Durable
```

### Configuration

#### Via Config File

```yaml
raft:
  write_mode: "batched"  # sync, wal, or batched
  batch:
    max_size: 100
    flush_interval: "5ms"
```

#### Via Environment Variables

```bash
export QUICBAAS_WRITE_MODE=batched
export QUICBAAS_BATCH_SIZE=100
export QUICBAAS_BATCH_FLUSH_INTERVAL=5ms
```

#### Via CLI Flags

```bash
quicbaas server \
  --write-mode=batched \
  --batch-size=100 \
  --batch-flush-interval=5ms
```

### API Usage

#### REST API

```bash
# Specify write mode per request
curl -X PUT http://localhost:8080/api/v1/kv/mykey \
  -H "Content-Type: application/json" \
  -H "X-Write-Mode: batched" \
  -d '{"value": "myvalue"}'
```

#### gRPC API

```go
import (
    "context"
    pb "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc/proto"
)

client := pb.NewKVServiceClient(conn)

// Set with specific write mode
resp, err := client.Set(context.Background(), &pb.SetRequest{
    Key:       "mykey",
    Value:     []byte("myvalue"),
    WriteMode: "batched",
})
```

#### Go Client Library

Write modes are configured via the qrdb SDK write options. QuicBaaS's client library passes write mode preferences through to the qrdb engine.

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc/client"
)

// Create client with default write mode
cli, err := client.New(client.Config{
    Addresses: []string{"localhost:9000"},
    WriteMode: "batched",
})

// Override write mode for specific operation
err = cli.SetWithMode(ctx, "key", []byte("value"), "sync")
```

### Data Loss Scenarios

#### Sync Mode
```
Time: t0 -> t1 -> t2 -> CRASH
Data: Write -> fsync() -> Replicate -> ACK -> Safe

Power failure at CRASH: Zero data loss
```

#### WAL Mode
```
Time: t0 -> t1 -> t2 -> t3 -> CRASH (before replication)
Data: Write -> WAL -> ACK -> Write -> Lost

Power failure at CRASH: Lose writes since last replication (~10-100ms)
```

#### Batched Mode
```
Time: t0 -> t1 -> t2 -> t3 -> t4 -> CRASH (during batch)
Data: Write -> Batch -> Write -> Flush -> Replicate -> ACK -> Safe

Power failure at CRASH: Only in-flight batch lost (flushed batches are safe)
```

### Metrics

QuicBaaS exports Prometheus metrics for monitoring write performance:

#### Batch Aggregator Metrics

```
# Total batches flushed
quicbaas_batch_batches_total{shard="1",status="success"} 1250

# Operations per batch
quicbaas_batch_size_bucket{shard="1",le="50"} 800
quicbaas_batch_size_bucket{shard="1",le="100"} 1200

# Flush duration
quicbaas_batch_flush_duration_seconds_bucket{shard="1",le="0.005"} 1180

# Queue depth
quicbaas_batch_queue_depth 15

# Flush reasons
quicbaas_batch_flush_reason_total{reason="size"} 950
quicbaas_batch_flush_reason_total{reason="time"} 280
quicbaas_batch_flush_reason_total{reason="close"} 20
```

#### Write Mode Distribution

```
quicbaas_raft_writes_total{mode="sync"} 1000
quicbaas_raft_writes_total{mode="wal_durable"} 50000
quicbaas_raft_writes_total{mode="batched"} 100000
```

### Best Practices

#### Batched Mode Tuning

**Batch Size:**
- Start with 100 operations
- Increase for higher throughput (up to 1000)
- Decrease if latency spikes

**Flush Interval:**
- Default: 5ms
- Lower for latency-sensitive apps (1-2ms)
- Higher for maximum throughput (10-50ms)

Example configurations:

```go
// High throughput
BatchConfig{
    MaxBatchSize:  500,
    FlushInterval: 10 * time.Millisecond,
}

// Low latency
BatchConfig{
    MaxBatchSize:  50,
    FlushInterval: 1 * time.Millisecond,
}
```

#### Mixed Workload Patterns

Use different write modes for different data types:

```go
// Critical user data - use sync
err := cli.SetWithMode(ctx, "user:"+id, userData, "sync")

// Access logs - use batched
err = cli.SetWithMode(ctx, "log:"+ts, logData, "batched")

// Metrics - use WAL durable
err = cli.SetWithMode(ctx, "metric:"+name, metricData, "wal")
```

#### Monitoring Recommendations

**Critical Alerts:**
- Batch queue depth > 80% capacity
- Batch flush errors > 0
- WAL sync errors > 0

**Warning Alerts:**
- Batch size consistently below 10
- Flush duration p99 > 100ms
- Queue depth > 50% capacity

### Troubleshooting

#### High Latency in Batched Mode

**Symptoms:** p99 latency > 100ms

**Causes:**
- Batch size too large (> 1000)
- Flush interval too high (> 50ms)
- Raft proposal backlog

**Solutions:**
```go
// Reduce batch size
BatchConfig{MaxBatchSize: 100}

// Reduce flush interval
BatchConfig{FlushInterval: 5 * time.Millisecond}

// Monitor Raft metrics
quicbaas_raft_proposal_duration_seconds_bucket
```

#### Low Throughput in WAL Durable Mode

**Symptoms:** < 5K ops/sec

**Causes:**
- Slow disk I/O
- CPU saturation
- Network latency

**Solutions:**
- Use NVMe SSD for WAL storage
- Tune OS disk I/O scheduler
- Monitor `quicbaas_wal_sync_duration_seconds`

#### Batch Queue Filling Up

**Symptoms:** `quicbaas_batch_queue_depth` approaching max

**Causes:**
- Write rate exceeds Raft capacity
- Slow Raft proposals
- Network issues

**Solutions:**
```go
// Increase queue size
const DefaultQueueSize = 10000  // was 1000

// Increase batch size to drain faster
BatchConfig{MaxBatchSize: 500}

// Scale cluster (add more nodes)
```

### Performance Summary

| Mode | Performance | Durability |
|------|-------------|------------|
| Sync | 500-1,000 writes/sec | Durable (fsync) |
| WAL Durable | 10K-50K writes/sec | Minimal loss risk |
| Batched | 100,000+ writes/sec | Durable (fsync) |

QuicBaaS's batched mode provides exceptional throughput with full durability guarantees.

### FAQ

**Q: Can I change write mode at runtime?**
A: Yes, on a per-request basis via API. The server's default write mode requires a restart to change.

**Q: Does write mode affect read performance?**
A: Indirectly. WAL Durable may cause slightly stale reads until replication catches up (milliseconds).

**Q: What happens if the batch aggregator crashes?**
A: In-flight operations will fail and return errors. Clients should retry. Data already flushed to Raft is safe.

**Q: Can I use different write modes for different keys?**
A: Yes. Specify the write mode per request using API headers or parameters.

**Q: Which mode is recommended for production?**
A: For most production workloads, **Batched mode** offers the best balance of throughput, latency, and durability. Use Sync for critical transactions requiring immediate consistency, and WAL Durable only if you can tolerate minimal data loss.

### References

- [Raft Consensus Algorithm](https://raft.github.io/)
- [Write-Ahead Logging](https://en.wikipedia.org/wiki/Write-ahead_logging)
- QuicBaaS Architecture

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0


---

## Atomic Batch Operations

### Overview

QuicBaaS supports atomic batch operations for multiple PUT and DELETE operations. All operations succeed or all fail, preventing orphaned or inconsistent data.

### Why Batch Operations?

Without batch operations, related entities can end up in inconsistent states:

```go
// WITHOUT batch operations - dangerous!
client.Put(ctx, &pb.PutRequest{
    EntityType: "user",
    Id:         123,
    Data:       userJSON,
})

// If this fails, we have a user without an order!
client.Put(ctx, &pb.PutRequest{
    EntityType: "order",
    Id:         456,
    Data:       orderJSON,
})
```

#### Solution: Atomic Batches

```go
// WITH batch operations - safe!
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userJSON},
        {EntityType: "order", Id: 456, Data: orderJSON},
    },
})
// Both succeed or both fail - no orphaned data!
```

### How It Works

Atomicity guarantee: all operations wrapped into one Raft proposal, state machine applies all operations together, automatic rollback if any operation fails.

Operations are grouped per shard; each shard group commits as its own atomic proposal. Batches whose encoded size exceeds the Raft entry limit (8 MB) are split into size-bounded sub-batches (~4 MB each); atomicity then holds per sub-batch, and operation order is preserved within each shard.

#### Architecture

```
Client Request
    |
gRPC BatchPut
    |
Single Raft Proposal (atomic)
    |
State Machine Update
    |-- Operation 1 (success)
    |-- Operation 2 (success)
    +-- Operation 3 (FAIL!) -> Rollback all
```

### API Reference

#### BatchPut - Atomic Multi-Insert

Stores multiple entities atomically.

**Request:**
```protobuf
message BatchPutRequest {
  repeated PutRequest requests = 1;
  ConsistencyLevel consistency = 2;
  uint64 shard_id = 3;
  repeated BatchCondition conditions = 4;  // Optional preconditions
}
```

**Response:**
```protobuf
message BatchPutResponse {
  repeated PutResponse responses = 1;
  int64 timestamp = 2;
}
```

**Example:**
```go
resp, err := client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {
            EntityType: "user",
            Id:         123,
            Data:       []byte(`{"name":"Alice","email":"alice@example.com"}`),
        },
        {
            EntityType: "profile",
            Id:         123,
            Data:       []byte(`{"bio":"Software Engineer","avatar":"avatar.png"}`),
        },
    },
    Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
})
```

#### BatchDelete - Atomic Multi-Delete

Deletes multiple entities atomically.

**Request:**
```protobuf
message BatchDeleteRequest {
  repeated DeleteRequest requests = 1;
  ConsistencyLevel consistency = 2;
  uint64 shard_id = 3;
}
```

**Response:**
```protobuf
message BatchDeleteResponse {
  repeated DeleteResponse responses = 1;
  int64 timestamp = 2;
}
```

**Example:**
```go
resp, err := client.BatchDelete(ctx, &pb.BatchDeleteRequest{
    Requests: []*pb.DeleteRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "profile", Id: 123},
        {EntityType: "settings", Id: 123},
    },
})
// All related entities deleted atomically
```

#### BatchGet - Multi-Retrieve (Non-Atomic)

Retrieves multiple entities in a single request. Note: not atomic, convenience method for efficient bulk reads.

**Request:**
```protobuf
message BatchGetRequest {
  repeated GetRequest requests = 1;
}
```

**Response:**
```protobuf
message BatchGetResponse {
  repeated GetResponse responses = 1;
}
```

**Example:**
```go
resp, err := client.BatchGet(ctx, &pb.BatchGetRequest{
    Requests: []*pb.GetRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "user", Id: 456},
        {EntityType: "user", Id: 789},
    },
})
```

### Use Cases

#### 1. User Registration with Profile

Create user and profile together:

```go
userJSON := `{"email":"user@example.com","username":"user123"}`
profileJSON := `{"bio":"New user","preferences":{}}`

client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: []byte(userJSON)},
        {EntityType: "profile", Id: 123, Data: []byte(profileJSON)},
    },
})
```

#### 2. Order with Line Items

Create order and all line items atomically:

```go
orderJSON := `{"customer_id":456,"total":99.99,"status":"pending"}`
item1JSON := `{"product_id":789,"quantity":2,"price":29.99}`
item2JSON := `{"product_id":790,"quantity":1,"price":39.99}`

client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "order", Id: 1001, Data: []byte(orderJSON)},
        {EntityType: "order_item", Id: 2001, Data: []byte(item1JSON)},
        {EntityType: "order_item", Id: 2002, Data: []byte(item2JSON)},
    },
})
```

#### 3. Account Deletion with Cleanup

Delete user and all related data:

```go
client.BatchDelete(ctx, &pb.BatchDeleteRequest{
    Requests: []*pb.DeleteRequest{
        {EntityType: "user", Id: 123},
        {EntityType: "profile", Id: 123},
        {EntityType: "settings", Id: 123},
        {EntityType: "preferences", Id: 123},
    },
})
```

### Best Practices

DO:
- Use batches for related entities (User + Profile, Order + Items)
- Keep batches small (2-10 operations)
- Use same shard (all entities must be on same shard)
- Handle errors (check individual responses)

DON'T:
- Batch unrelated entities
- Create huge batches (>100 operations)
- Batch cross-shard operations
- Assume BatchGet is atomic

### Error Handling

#### Batch Failure Behavior

If any operation fails: all previous operations rolled back, subsequent operations not executed, entire batch returns error.

Example:
```go
resp, err := client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: validJSON},    // Would succeed alone
        {EntityType: "user", Id: 456, Data: invalidJSON},  // FAILS HERE
        {EntityType: "user", Id: 789, Data: validJSON},    // Never executed
    },
})
// Result: err != nil, NO entities created (rollback)
```

#### Checking Individual Responses

```go
resp, err := client.BatchPut(ctx, req)
if err != nil {
    log.Errorf("Batch failed: %v", err)
    return
}

for i, r := range resp.Responses {
    if !r.Created {
        log.Warnf("Operation %d failed: %s", i, r.Message)
    }
}
```

### Performance Considerations

Advantages: single network round-trip, single Raft consensus, atomic guarantee with no extra cost.

Limitations: same-shard only, rollback overhead on failures, large batches consume more memory.

#### Benchmarks

| Operation | Latency | Throughput |
|-----------|---------|------------|
| Single Put | 5ms | 200 ops/sec |
| Batch Put (10 items) | 6ms | 1,600 ops/sec |
| Single Delete | 4ms | 250 ops/sec |
| Batch Delete (10 items) | 5ms | 2,000 ops/sec |

*8x-10x improvement for batch operations*

### Implementation Details

#### State Machine

The batch operation is implemented in the qrdb engine's state machine:

1. **Backup Phase**: Save current values of all keys being modified
2. **Apply Phase**: Execute each operation sequentially
3. **Success**: Commit all changes
4. **Failure**: Restore all keys from backup (rollback)

```go
operation: "batch"
operations: [
    {operation: "put", entity_type: "user", id: 123, data: "..."},
    {operation: "put", entity_type: "profile", id: 123, data: "..."},
]
```

#### Rollback Strategy

**Data Rollback:** Complete - all key-value changes are reversed

**Index Rollback:** Best-effort - indexes may be inconsistent after failed batch (noted in code comment)

For production systems requiring perfect index consistency after rollback, additional index change tracking would be needed.

### Advanced Features

The batch system has been extended with additional capabilities:

#### Conditional Batch Operations

Add preconditions that must be satisfied before operations execute:

```go
client.BatchPut(ctx, &pb.BatchPutRequest{
    Requests: []*pb.PutRequest{
        {EntityType: "user", Id: 123, Data: userData},
    },
    Conditions: []*pb.BatchCondition{
        {
            EntityType:   "user",
            Id:           123,
            MustNotExist: true, // Only create if doesn't exist
        },
    },
})
```

See [Conditional Batch Operations](engineering.md) for complete documentation.

#### Streaming Batch Operations

For large datasets that exceed practical single-request limits:

```go
stream, _ := client.StreamBatchPut(ctx)

// Send header
stream.Send(&proto.StreamBatchPutRequest{...})

// Stream chunks
for _, chunk := range chunks {
    stream.Send(chunk)
    progress, _ := stream.Recv()
    log.Printf("Progress: %d/%d", progress.OperationsReceived, total)
}

// Complete
stream.Send(completeRequest)
result, _ := stream.Recv()
```

See [Streaming Batch Operations](engineering.md) for complete documentation.

#### Cross-Shard Transactions

For atomic operations across multiple shards, use the 2PC coordinator:

```go
ops := map[uint64][]coordinator.TxnOp{
    shard1: {/* shard 1 operations */},
    shard2: {/* shard 2 operations */},
}

txnID, _ := coordinator.Begin(ctx, ops, nil)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

See [Distributed Transactions](engineering.md) for complete documentation.

### Examples

See `examples/batch_operations/` for complete working examples.

### Related Documentation

- [Conditional Batch Operations](engineering.md) - Precondition-based batches
- [Streaming Batch Operations](engineering.md) - Large dataset streaming
- [Distributed Transactions](engineering.md) - Cross-shard 2PC
- [DAO Transactions](engineering.md) - Entity-level transactions
- Raft Consensus - Underlying consensus mechanism


---

## Transaction Architecture

### Overview

Multi-Version Concurrency Control (MVCC) transaction system for QuicBaaS. Enables transactional and non-transactional operations with strong consistency guarantees.

### Architecture Layers

```
+-------------------------------------------------------------+
|  API Layer (gRPC, REST, CLI)                                |
|  - Txn(), Put(), Get(), Delete()                            |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  Transaction Coordinator                                    |
|  - Transaction lifecycle management                         |
|  - Compare-and-swap validation                              |
|  - Success/failure operation execution                      |
|  - Conflict detection                                       |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  MVCC Storage Layer                                         |
|  - Versioned key-value storage                              |
|  - Revision tracking (global counter)                       |
|  - Version history per key                                  |
|  - Garbage collection                                       |
+----------------------------+--------------------------------+
                             |
+----------------------------v--------------------------------+
|  PebbleDB Storage Engine                                    |
|  - Atomic batch operations                                  |
|  - Persistent storage                                       |
|  - Snapshots and iterators                                  |
+-------------------------------------------------------------+
```

### Key Concepts

#### 1. Revision Numbers

Global revision: monotonically increasing counter for all modifications, incremented on every Put/Delete/Transaction, used for point-in-time reads and watches.

#### 2. Key Versioning

Each key maintains: CreateRevision (first created), ModRevision (last modification), Version (modification count), Value (actual data).

#### 3. Key Encoding

Keys are encoded with revisions to support MVCC:

```
Format: <user_key>/<revision>

Example: "mykey/123" for user key "mykey" at revision 123

Special metadata keys:
- "mvcc/rev" -> current global revision
- "mvcc/meta/<key>" -> {CreateRevision, ModRevision, Version}
```

#### 4. Transaction Semantics

Transactions use a compare-and-swap model: compare phase (check conditions), execute phase (apply success or failure operations atomically), commit (all or nothing).

### Data Structures

#### MVCCStorage Interface

```go
type MVCCStorage interface {
    // Core MVCC operations
    Put(ctx context.Context, key, value []byte) (*KeyValue, int64, error)
    Get(ctx context.Context, key []byte, rev int64) (*KeyValue, error)
    Delete(ctx context.Context, key []byte) (*KeyValue, int64, error)

    // Range operations
    Range(ctx context.Context, start, end []byte, rev int64, limit int64) ([]*KeyValue, error)

    // Transaction support
    BeginTxn(ctx context.Context) (Transaction, error)

    // Revision management
    CurrentRevision() int64
    Compact(ctx context.Context, rev int64) error
}

type KeyValue struct {
    Key            []byte
    Value          []byte
    CreateRevision int64
    ModRevision    int64
    Version        int64
    Lease          int64  // For lease support (future)
}

type Transaction interface {
    // Add operations to transaction
    Put(key, value []byte) error
    Delete(key []byte) error
    Get(key []byte) (*KeyValue, error)

    // Compare operations for conditional execution
    AddCompare(cmp Compare) error

    // Commit or rollback
    Commit() error
    Rollback() error
}

type Compare struct {
    Key      []byte
    Target   CompareTarget  // VERSION, VALUE, MOD, CREATE
    Result   CompareResult  // EQUAL, NOT_EQUAL, GREATER, LESS
    Value    interface{}    // For VALUE target
    Revision int64          // For VERSION/MOD/CREATE targets
}
```

#### Storage Layout

PebbleDB stores three types of entries:

1. **Data entries**: `data/<key>/<revision>` -> `<value>`
2. **Metadata entries**: `meta/<key>` -> `{CreateRevision, ModRevision, Version}`
3. **System entries**: `sys/revision` -> `<current_revision>`

### Transaction Flow

#### Non-Transactional Operations

Simple operations (Put, Get, Delete) bypass the transaction coordinator:

```
1. Put("key", "value")
   |-- Increment global revision (123 -> 124)
   |-- Write: data/key/124 -> "value"
   |-- Update: meta/key -> {Create: 120, Mod: 124, Ver: 5}
   +-- Update: sys/revision -> 124
```

#### Transactional Operations

Complex operations use the transaction coordinator:

```
1. Txn.Compare(key="foo", mod_revision=120)
   |-- Read: meta/foo -> {Create: 110, Mod: 120, Ver: 3}
   +-- Compare: 120 == 120 (match)

2. Txn.Success: Put("foo", "new_value")
   |-- Increment revision (124 -> 125)
   |-- Batch:
   |   |-- data/foo/125 -> "new_value"
   |   |-- meta/foo -> {Create: 110, Mod: 125, Ver: 4}
   |   +-- sys/revision -> 125
   +-- Commit batch atomically
```

### Performance Considerations

#### Optimization Strategies

Read path: use PebbleDB snapshots for consistent reads. Write path: batch all transaction operations. Metadata: cache frequently accessed metadata. Compaction: background GC of old revisions.

#### Expected Performance

Non-transactional operations: no overhead. Single-key transactions: ~10-20% overhead. Multi-key transactions: ~20-30% overhead.

### Garbage Collection

Old revisions cleaned up via: compaction (remove old revisions), retention policy (keep last N revisions per key), background GC (periodic cleanup).

### Consistency Guarantees

Snapshot isolation: transactions see consistent snapshot. Serializable: compare operations detect conflicts.

Conflict detection via ModRevision checks: if ModRevision changed between read and commit, abort transaction (optimistic concurrency control).

### Transaction Types

QuicBaaS provides multiple transaction mechanisms for different use cases:

#### DAO-Level Transactions

Entity-aware transactions for single-entity-type operations:

```go
tx, _ := userDAO.BeginTx(ctx)
tx.Save(user1)
tx.Save(user2)
tx.Delete(user3)
tx.Commit(ctx)
```

See [DAO Transactions](engineering.md) for details.

#### Distributed Transactions (2PC)

Cross-shard atomic transactions using Two-Phase Commit:

```go
txnID, _ := coordinator.Begin(ctx, ops, conditions)
coordinator.Prepare(ctx, txnID)
coordinator.Commit(ctx, txnID)
```

See [Distributed Transactions](engineering.md) for details.

#### Optimistic Locking

Version-based concurrency control:

```go
tx.SaveWithCondition(entity, Condition{
    Target:   ConditionVersion,
    Operator: OpEqual,
    Value:    expectedVersion,
})
```

See [Optimistic Locking](engineering.md) for details.

### Related Documentation

- [DAO Transactions](engineering.md) - Entity-level transactions
- [Distributed Transactions](engineering.md) - 2PC coordination
- [Optimistic Locking](engineering.md) - Version-based concurrency
- [Conditional Batch Operations](engineering.md) - Batch preconditions
- Batch Operations - Atomic batch operations

### References

- [CockroachDB MVCC](https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer.html)
- [Google Percolator](https://research.google/pubs/pub36726/)


---

## QuicBaaS Storage Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          QuicBaaS Node                               │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                      API Layer                                  │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │ │
│  │  │  gRPC    │  │   REST   │  │   QUIC   │  │   MCP    │       │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                     Service Layer                               │ │
│  │  (Business logic, validation, coordination)                     │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                  Raft State Machine                             │ │
│  │  (Consensus, replication, state transitions)                    │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                   Storage Layer                                 │ │
│  │                                                                  │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │ │
│  │  │   MVCC   │  │  Index   │  │ Encrypt  │  │  Watch   │       │ │
│  │  │  Layer   │  │ Manager  │  │  Layer   │  │  System  │       │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │ │
│  │       ↓              ↓              ↓              ↓            │ │
│  │  ┌──────────────────────────────────────────────────────┐      │ │
│  │  │            PebbleDB Adapter                           │      │ │
│  │  │  - Batching, Snapshots, Iterators                    │      │ │
│  │  └──────────────────────────────────────────────────────┘      │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                      PebbleDB                                   │ │
│  │                                                                  │ │
│  │  ┌─────────────────┐  ┌─────────────────┐                      │ │
│  │  │  MemTable (RAM) │  │  Block Cache    │                      │ │
│  │  └─────────────────┘  └─────────────────┘                      │ │
│  │           ↓                     ↑                                │ │
│  │  ┌─────────────────────────────────────────┐                   │ │
│  │  │           LSM-tree (Disk)                │                   │ │
│  │  │  L0: [SST] [SST] [SST]                  │                   │ │
│  │  │  L1: [SST] [SST]                        │                   │ │
│  │  │  L2: [SST] [SST] [SST]                  │                   │ │
│  │  │  L3-L6: [...more levels...]             │                   │ │
│  │  └─────────────────────────────────────────┘                   │ │
│  │           ↓                                                      │ │
│  │  ┌─────────────────┐                                           │ │
│  │  │  WAL (Disk)     │                                           │ │
│  │  └─────────────────┘                                           │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### LSM-Tree Architecture

#### Write Path

```
1. Client Write
       ↓
2. Write to WAL (durability)
       ↓
3. Write to MemTable (RAM)
       ↓
4. Return to client
       ↓
5. [Background] MemTable full → Freeze
       ↓
6. [Background] Flush to L0 SST (disk)
       ↓
7. [Background] Compact L0 → L1 (sorted, merged)
       ↓
8. [Background] Compact L1 → L2 → ... → L6
```

**Flow Diagram**:

```
Write(key, value)
    |
    ├─> WAL.Append(entry) ──────────────┐
    |                                    |
    └─> MemTable.Set(key, value)        |
              |                          |
              | (when full)              |
              ↓                          |
        Freeze MemTable ───────────────> Disk Flush
              |                          |
        New MemTable                     ↓
              |                     L0 SST File
              |                          |
              |                          | (background)
              |                          ↓
              |                     Compaction
              |                     L0 → L1 → L2...
              |
              ↓
        Return Success
```

#### Read Path

```
1. Check MemTable (active)
       ↓ (miss)
2. Check Immutable MemTable (frozen)
       ↓ (miss)
3. Check L0 SSTs (bloom filter → block cache → disk)
       ↓ (miss)
4. Check L1 SST (binary search → bloom filter → block cache → disk)
       ↓ (miss)
5. Check L2-L6 SSTs (increasing size)
       ↓ (miss)
6. Return NotFound
```

**Flow Diagram**:

```
Get(key)
    |
    ├─> MemTable.Get(key) ──────────> Found? → Return
    |        |
    |        | (miss)
    |        ↓
    ├─> ImmutableMemTable.Get(key) ──> Found? → Return
    |        |
    |        | (miss)
    |        ↓
    └─> For each L0 SST:
            |
            ├─> BloomFilter.MayContain(key)
            |       |
            |       | (probably yes)
            |       ↓
            ├─> BlockCache.Get(key) ──> Found? → Return
            |       |
            |       | (miss)
            |       ↓
            └─> DiskRead(block) ──────> Found? → Return
                    |
                    | (miss in all L0)
                    ↓
            For each L1-L6 level:
                    ↓
            BinarySearch(sstable) ──────> Found? → Return
                    |
                    | (miss in all levels)
                    ↓
            Return NotFound
```

#### Compaction

```
Background Process:

L0 Files: [f1] [f2] [f3] [f4]  (unsorted, overlapping)
              ↓
    Compact when count > L0CompactionThreshold
              ↓
L1 Files: [f5────────] [f6────────]  (sorted, non-overlapping)
              ↓
    Compact when size > MaxBytesForLevelBase
              ↓
L2 Files: [f7──────────────] [f8──────────────]
              ↓
    Continue down to L6...
```

**Compaction Strategies**:

1. **L0 → L1 Compaction** (most expensive)
   - Merge all overlapping L0 files
   - Sort and deduplicate keys
   - Apply tombstones (deletes)
   - Create non-overlapping L1 files

2. **Ln → Ln+1 Compaction** (cheaper)
   - Select files that overlap
   - Merge and compact
   - Each level 10x larger than previous

3. **Tombstone Cleanup**
   - Remove deleted keys during compaction
   - Configurable retention period
   - Garbage collection of old versions

### Key Encoding

#### Encoding Principles

1. **Hierarchical prefixes** group related data
2. **Null byte separators** enable prefix scans
3. **Big-endian integers** ensure proper sorting
4. **Common prefixes** optimize compression

#### Entity Key Format

```
{prefix}\x00{namespace}\x00{entity_type}\x00{id}\x00{version}

Example:
kv\x00default\x00user\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x05
└─┬──┘└───┬───┘└─┬──┘└──────────┬─────────┘└──────────┬─────────┘
  |       |      |               |                     |
Prefix  NS  EntityType      ID (uint64)         Version (uint64)
```

#### Index Key Format

```
idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}\x00{version}

Example (email index):
idx\x00user\x00email\x00alice@example.com\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x05
```

#### Metadata Key Format

```
meta\x00{entity_type}\x00{id}\x00{field}

Example:
meta\x00user\x00\x00\x00\x00\x00\x00\x00\x01\x00created_at
```

#### Sequence Key Format

```
seq\x00{entity_type}

Example:
seq\x00user → uint64 (next available ID)
```

### Storage Layers

#### MVCC Layer

Multi-Version Concurrency Control provides:

**Version Management**:
```go
// Each write creates new version
key := EncodeKey("user", 1)
version := GetNextVersion()  // Monotonically increasing

storage.Set(key + version, value)
```

**Snapshot Isolation**:
```go
// Read at specific version
snapshot := storage.NewSnapshot()
defer snapshot.Close()

value := snapshot.Get(key)  // Consistent point-in-time view
```

**Garbage Collection**:
```go
// During compaction, remove old versions
if key.version < snapshot.minVersion {
    // Safe to delete
    compaction.Remove(key)
}
```

#### Index Manager

**Primary Index** (entity by ID):
```
{entity_type}\x00{id} → full entity data
```

**Secondary Indexes** (query by field):
```
idx\x00{entity_type}\x00{field}\x00{value}\x00{id} → empty
```

**Index Maintenance**:
```go
func IndexEntity(entity Entity) error {
    batch := storage.NewBatch()

    // Store entity
    key := EncodePrimaryKey(entity.Type, entity.ID)
    batch.Set(key, entity.Data)

    // Create indexes
    for field, value := range entity.IndexedFields {
        idxKey := EncodeIndexKey(entity.Type, field, value, entity.ID)
        batch.Set(idxKey, nil)  // Existence index
    }

    return batch.Commit()
}
```

**Index Query**:
```go
func QueryByIndex(entityType, field, value string) []Entity {
    prefix := EncodeIndexPrefix(entityType, field, value)

    iter := storage.NewIterator(&IteratorOptions{
        Prefix: prefix,
    })
    defer iter.Close()

    var results []Entity
    for iter.Next() {
        id := ExtractIDFromIndexKey(iter.Key())
        entity := GetEntity(entityType, id)
        results = append(results, entity)
    }

    return results
}
```

#### Encryption Layer

**Encryption at Rest**:
```
Plaintext → AES-256-GCM → Ciphertext + Tag
                ↓
         Store in PebbleDB
```

**Key Hierarchy**:
```
Master Key (KMS/HSM)
    ↓
Cluster DEK (Data Encryption Key)
    ↓
Per-Entity Encryption (optional)
```

**Encrypted Key Format**:
```
{prefix}_enc\x00{namespace}\x00{id}\x00{version}
  → {encrypted_value, nonce, tag, metadata}
```

#### Watch System

**Event Generation**:
```go
// On every write
func Set(key, value []byte) error {
    err := storage.Set(key, value)
    if err != nil {
        return err
    }

    // Notify watchers
    watchEvent := &WatchEvent{
        Type:  EventPut,
        Key:   key,
        Value: value,
    }

    watchSystem.Notify(watchEvent)
    return nil
}
```

**Watch Stream**:
```
Client                        Server
  |                             |
  |─── Watch(prefix) ──────────>|
  |                             |
  |<─── WatchID ────────────────|
  |                             |
  |                    [Write happens]
  |                             |
  |<─── Event(PUT, key, value)─|
  |<─── Event(DELETE, key) ────|
  |                             |
```

### Data Flow Examples

#### Write Operation (Synchronous)

```
1. Client: Put(key="user:1", value="{name: 'Alice'}")
           ↓
2. Service Layer: Validate + Authorize
           ↓
3. Raft Proposal: Propose(entry)
           ↓
4. Raft Consensus: Replicate to majority
           ↓
5. State Machine Apply:
           ├─> MVCC: Allocate version
           ├─> Indexes: Update secondary indexes
           ├─> Encryption: Encrypt value (if enabled)
           └─> Storage: Write to PebbleDB
                  ├─> WAL: Append entry
                  ├─> MemTable: Set(key, value)
                  └─> Return success
           ↓
6. Raft Commit: Mark entry as committed
           ↓
7. Return to client: Success
           ↓
8. [Async] Watch System: Notify watchers
```

#### Range Scan Operation

```
1. Client: Scan(prefix="user:")
           ↓
2. Service Layer: Authorize prefix access
           ↓
3. Storage: NewIterator(prefix)
           ↓
4. PebbleDB:
           ├─> Check MemTable (active)
           ├─> Check Immutable MemTable
           ├─> Create merged iterator (all levels)
           └─> Return iterator
           ↓
5. Iterate:
    For each key:
           ├─> Decrypt (if encrypted)
           ├─> Check MVCC version
           ├─> Filter by visibility
           └─> Return to client
           ↓
6. Client receives stream of key-value pairs
```

#### Background Compaction

```
Trigger: L0 file count > L0CompactionThreshold
         ↓
1. Select files to compact (all overlapping L0 + L1 files)
         ↓
2. Create merge iterator
         ↓
3. For each unique key:
    ├─> Apply tombstones (delete markers)
    ├─> Keep latest version (discard old)
    ├─> Compress value
    └─> Write to new L1 SST file
         ↓
4. Atomically replace old files with new
         ↓
5. Delete old SST files
         ↓
6. Update manifest
         ↓
7. Release disk space
```

### Performance Characteristics

#### Latency Analysis

**Write Latency Breakdown**:
```
Total: ~2ms (sync mode)

Components:
  Validation:        0.01ms  (0.5%)
  Raft proposal:     0.10ms  (5%)
  Network RTT:       0.50ms  (25%)
  WAL append:        0.05ms  (2.5%)
  MemTable write:    0.01ms  (0.5%)
  fsync:             1.30ms  (65%)
  Callback:          0.03ms  (1.5%)
```

**Read Latency Breakdown**:
```
Hot path (cache hit): ~0.5ms

Components:
  Lookup:            0.01ms  (2%)
  MemTable check:    0.10ms  (20%)
  Cache lookup:      0.30ms  (60%)
  Deserialize:       0.05ms  (10%)
  Authorization:     0.04ms  (8%)

Cold path (disk read): ~3ms

Components:
  Lookup:            0.01ms  (0.3%)
  MemTable check:    0.10ms  (3.3%)
  Bloom filter:      0.05ms  (1.7%)
  Cache miss:        0.10ms  (3.3%)
  Disk read:         2.50ms  (83.3%)
  Deserialize:       0.05ms  (1.7%)
  Authorization:     0.04ms  (1.3%)
  Cache update:      0.15ms  (5%)
```

#### Throughput Analysis

**Single-threaded Throughput**:
```
Sync writes:   500-1K ops/sec   (limited by fsync)
Async writes:  10-20K ops/sec   (limited by CPU)
Batch writes:  50-100K keys/sec (amortized)
Point reads:   2-5K ops/sec     (limited by disk)
Cached reads:  50-100K ops/sec  (limited by CPU)
```

**Multi-threaded Throughput** (32 cores):
```
Sync writes:   10-15K ops/sec   (parallel fsyncs)
Async writes:  200-500K ops/sec (parallel processing)
Point reads:   50-100K ops/sec  (cache hits)
Range scans:   1-5K scans/sec   (I/O bound)
```

#### Resource Usage

**Memory**:
```
Fixed overhead:    50-100 MB  (runtime, connections)
Block cache:       configurable (128 MB - 64 GB)
MemTables:         2-4 × MemTableSize (128-512 MB)
Bloom filters:     10 bits/key (~1.25 MB per 1M keys)
Index blocks:      ~1% of data size
Total (typical):   500 MB - 10 GB
```

**Disk I/O**:
```
Write amplification:  10-30x (LSM-tree characteristic)
Space amplification:  1.5-2.5x (during compaction)
Read amplification:   5-20x (levels checked)

Mitigation:
  - Larger MemTables reduce write amp
  - Better compression reduces space amp
  - Bloom filters reduce read amp
```

**CPU**:
```
Foreground (requests): 20-40% (normal load)
Background (compaction): 30-50% (write-heavy)
Idle: 1-5%

CPU breakdown:
  Encoding/decoding:    25%
  Compression:          20%
  Compaction:           30%
  Network I/O:          15%
  Business logic:       10%
```

### Configuration Recommendations

#### Development

```go
config := &pebble.Config{
    DataDir:                  "./data",
    CacheSize:                128 << 20,  // 128 MB
    MemTableSize:             64 << 20,   // 64 MB
    MaxOpenFiles:             500,
    MaxConcurrentCompactions: 1,
    SyncWrites:               true,
    Compression:              CompressionSnappy,
}
```

#### Production (Read-Heavy)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                20 << 30,   // 20 GB
    MemTableSize:             128 << 20,  // 128 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 2,
    SyncWrites:               true,
    Compression:              CompressionZstd,
    L0CompactionThreshold:    4,
    L0StopWritesThreshold:    12,
}
```

#### Production (Write-Heavy)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                10 << 30,   // 10 GB
    MemTableSize:             256 << 20,  // 256 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 4,
    SyncWrites:               false,
    WALMinSyncInterval:       100 * time.Millisecond,
    Compression:              CompressionSnappy,
    L0CompactionThreshold:    8,
    L0StopWritesThreshold:    20,
}
```

#### Production (Low-Latency)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                15 << 30,   // 15 GB (large cache)
    MemTableSize:             128 << 20,  // 128 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 2,          // Limit CPU usage
    SyncWrites:               false,      // Async for speed
    WALMinSyncInterval:       50 * time.Millisecond,
    Compression:              CompressionSnappy,  // Fast compression
    L0CompactionThreshold:    6,
}
```

### Monitoring and Metrics

#### Key Performance Indicators

```
Operational Health:
  ├─ Cache hit rate > 80%
  ├─ Write latency p99 < 10ms
  ├─ Read latency p99 < 5ms
  ├─ L0 file count < 12
  └─ Compaction lag < 100 MB

Resource Utilization:
  ├─ Memory usage < 80% of cache
  ├─ Disk usage < 80% of capacity
  ├─ CPU usage < 70% avg
  └─ Disk I/O < 80% saturation

Error Rates:
  ├─ Storage errors < 0.01%
  ├─ Timeout errors < 0.1%
  └─ Compaction failures = 0
```

#### Alerting Thresholds

```yaml
Critical:
  - disk_usage > 90%
  - l0_files > 15
  - compaction_failures > 0
  - write_latency_p99 > 50ms
  - cache_hit_rate < 50%

Warning:
  - disk_usage > 75%
  - l0_files > 10
  - write_latency_p99 > 20ms
  - cache_hit_rate < 70%
  - memory_usage > 80%
```

### References

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Design](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)
- [CockroachDB Storage](https://www.cockroachlabs.com/docs/stable/architecture/storage-layer.html)


---

## QuicBaaS Storage Layer

High-performance embedded storage built on PebbleDB with LSM-tree architecture.

### Overview

QuicBaaS uses [PebbleDB](https://github.com/cockroachdb/pebble) as its embedded storage engine, providing:

- Pure Go implementation (no CGo dependencies)
- LSM-tree architecture optimized for write-heavy workloads
- Battle-tested in CockroachDB production environments
- Low-latency point lookups with bloom filters
- Efficient range scans with sorted keys
- ACID guarantees via write-ahead logging

### Key Features

| Feature | Description |
|---------|-------------|
| **LSM-tree Architecture** | Write-optimized storage with background compaction |
| **Embedded Storage** | No external dependencies, single binary deployment |
| **MVCC Support** | Multi-version concurrency control for consistent reads |
| **Write Modes** | Configurable durability vs performance trade-offs |
| **Compression** | Snappy and Zstd compression to reduce disk usage |
| **Bloom Filters** | Fast negative lookups reduce disk I/O |
| **Block Cache** | In-memory cache for frequently accessed data |

### Quick Reference

#### Performance Characteristics

```
Operation              Latency (p50)    Throughput
Point Lookup (hot)     < 1ms            50K ops/sec
Point Lookup (cold)    2-5ms            10K ops/sec
Sequential Write       0.5ms            20K ops/sec
Batch Write (1K)       50ms             20K entries/sec
Range Scan (1K keys)   10-50ms          -
```

#### Storage Overhead

```
Per Key-Value Pair:
  Key size:            variable (typically 20-100 bytes)
  Value size:          variable
  Index overhead:      ~10% (bloom filters, indexes)
  Space amplification: 1.5-2.5x (during compaction)
  Write amplification: 10-30x (typical LSM-tree)
```

#### Default Configuration

```go
config := &pebble.Config{
    DataDir:                  "./data",
    CacheSize:                128 << 20,  // 128 MB
    MaxOpenFiles:             1000,
    MaxConcurrentCompactions: 1,
    MemTableSize:             64 << 20,   // 64 MB
    SyncWrites:               true,
    Compression:              CompressionSnappy,
}
```

### Key Schema

QuicBaaS uses a hierarchical key encoding scheme with prefix-based isolation:

#### General Format

```
{prefix}\x00{namespace}\x00{identifier}\x00{version}
```

#### Key Prefixes

```
KV Data:        kv\x00{namespace}\x00{key}\x00{version}
Indexes:        idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}
Metadata:       meta\x00{entity_type}\x00{id}
Sequences:      seq\x00{entity_type}
IAM:            __iam:tenant:{tenant_id}:...
Vector:         v\x00{namespace}\x00{id}\x00{version}
Timeseries:     ts\x00{metric}\x00{timestamp}
```

#### Key Encoding Rules

1. **Null byte separator** (`\x00`) enables prefix scans
2. **Big-endian integers** ensure proper lexicographic ordering
3. **Common prefixes** group related data for efficient range queries
4. **Version suffix** supports MVCC and compaction

### Storage Operations

#### Basic Operations

```go
// Get a value
value, err := storage.Get(ctx, key)

// Set a value (sync mode)
err := storage.Set(ctx, key, value)

// Delete a key
err := storage.Delete(ctx, key)

// Check existence
exists, err := storage.Exists(ctx, key)
```

#### Batch Operations

```go
// Create a batch
batch := storage.NewBatch()

// Add operations
batch.Set(key1, value1)
batch.Set(key2, value2)
batch.Delete(key3)

// Commit atomically
err := batch.Commit()
```

#### Range Scans

```go
// Scan with prefix
iter := storage.NewIterator(&IteratorOptions{
    Prefix: []byte("users:"),
})
defer iter.Close()

for iter.Next() {
    key := iter.Key()
    value := iter.Value()
    // Process key-value pair
}
```

#### Snapshots

```go
// Create snapshot for consistent reads
snapshot := storage.NewSnapshot()
defer snapshot.Close()

// Read from snapshot
value, err := snapshot.Get(ctx, key)
```

### Write Modes

QuicBaaS supports multiple write modes with different durability guarantees:

#### Sync Mode (Default)

```go
config.SyncWrites = true
```

- Writes flushed to disk before returning
- Full durability, survives crashes
- Latency: 1-2ms per write
- Throughput: 500-1K writes/sec

#### Async Mode (Performance)

```go
config.SyncWrites = false
config.WALMinSyncInterval = 100 * time.Millisecond
```

- Writes buffered in memory
- Periodic fsync every 100ms
- Latency: < 0.5ms per write
- Throughput: 10-20K writes/sec
- Risk: Up to 100ms of writes lost on crash

#### Batch Mode (High Throughput)

```go
batch := storage.NewBatch()
for i := 0; i < 10000; i++ {
    batch.Set(key, value)
}
batch.Commit()
```

- Multiple writes committed atomically
- Amortized cost across batch
- Latency: 50-100ms per batch
- Throughput: 50-100K entries/sec

### Tuning Guide

#### Memory Allocation

```go
// Development
CacheSize:    128 << 20  // 128 MB
MemTableSize: 64 << 20   // 64 MB

// Production (64GB RAM)
CacheSize:    10 << 30   // 10 GB
MemTableSize: 256 << 20  // 256 MB
```

#### Write-Heavy Workloads

```go
config.MemTableSize = 256 << 20           // Larger buffer
config.MaxConcurrentCompactions = 4       // More compaction threads
config.L0CompactionThreshold = 8          // Delay compaction
config.WALMinSyncInterval = 100 * time.Millisecond
```

#### Read-Heavy Workloads

```go
config.CacheSize = 20 << 30               // Larger cache
config.Compression = CompressionZstd      // Better compression
config.MaxConcurrentReads = 500           // More concurrent readers
```

#### Low-Latency Requirements

```go
config.CacheSize = 10 << 30               // Large cache
config.SyncWrites = false                 // Async writes
config.MaxConcurrentCompactions = 2       // Limit CPU usage
```

### Monitoring

#### Key Metrics

```
Storage Metrics:
  quicbaas_storage_get_operations_total
  quicbaas_storage_set_operations_total
  quicbaas_storage_get_duration_seconds
  quicbaas_storage_set_duration_seconds
  quicbaas_storage_bytes_read_total
  quicbaas_storage_bytes_written_total
  quicbaas_storage_cache_hits_total
  quicbaas_storage_cache_misses_total
  quicbaas_storage_compactions_total
  quicbaas_storage_flushes_total
```

#### Health Indicators

```
Cache hit rate:        > 80% (healthy)
Write amplification:   10-30x (typical)
Space amplification:   1.5-2.5x (normal)
L0 file count:         < 12 (stable)
Compaction backlog:    < 100MB (manageable)
```

### Troubleshooting

#### High Write Latency

**Symptoms**: Writes taking > 10ms consistently

**Causes**:
- Too many L0 files (compaction falling behind)
- Small memtable size (frequent flushes)
- Sync writes on slow disk

**Solutions**:
```go
config.MemTableSize = 256 << 20
config.MaxConcurrentCompactions = 4
config.L0CompactionThreshold = 8
config.WALMinSyncInterval = 100 * time.Millisecond
```

#### High Read Latency

**Symptoms**: Reads taking > 5ms consistently

**Causes**:
- Low cache hit rate (cache too small)
- Too many L0 files (read amplification)
- Missing bloom filters

**Solutions**:
```go
config.CacheSize = 10 << 30
config.L0CompactionThreshold = 4
// Ensure bloom filters enabled (default)
```

#### Out of Disk Space

**Symptoms**: Disk usage growing rapidly

**Causes**:
- Compaction falling behind
- Old snapshots not released
- Too many tombstones

**Solutions**:
```bash
# Manual compaction
quicbaas server compact --force

# Check snapshots
quicbaas server stats

# Increase compaction resources
config.MaxConcurrentCompactions = 4
```

#### Memory Pressure

**Symptoms**: OOM errors or high memory usage

**Causes**:
- Cache too large
- Too many memtables
- Too many open snapshots

**Solutions**:
```go
config.CacheSize = 2 << 30  // Reduce cache
config.MemTableStopWritesThreshold = 2
config.MaxOpenFiles = 500
```

### Best Practices

#### DO:

- Use batches for multiple writes
- Set appropriate cache size (10-20% of data)
- Enable bloom filters for point lookups
- Monitor compaction metrics
- Use compression for large values
- Close iterators and snapshots promptly
- Use prefix scans instead of full scans

#### DON'T:

- Store very large values (> 10 MB)
- Ignore compaction warnings
- Use very small cache (< 128 MB)
- Disable WAL in production
- Keep snapshots open indefinitely
- Use async mode without understanding risks
- Perform full key scans frequently

### Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Detailed storage architecture and design |
| Object Storage | External object storage backends (S3, GCS, Azure, MinIO) |
| Vector Storage | Vector database storage schema |
| Performance Analysis | Vector performance tuning |

### External Resources

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning Guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)
- [CockroachDB Storage Layer](https://www.cockroachlabs.com/docs/stable/architecture/storage-layer.html)
