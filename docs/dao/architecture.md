# DAO Architecture

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
func NewCodec[E any](ct CodecType) (Codec[E], error) {
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
userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithTimeout(30*time.Second),
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

func NewDAO[E Entity](
    factory *Factory,
    entityType string,
    newEntity func() E,
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
func CreateDAO[E dao.Entity](
    svc QuicBaaS,
    entityType string,
    newEntity func() E,
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
userDAO, _ := service.CreateDAO[*User](svc, "users", newUser)

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
3. **Multiple Codecs**: JSON, YAML, TOML, MsgPack, BSON (always available), plus CBOR, Avro, Protobuf (with build tags)

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

- Review [Configuration Options](./configuration.md) for tuning
- See [Getting Started](./getting-started.md) for practical examples
- Check [README](./README.md) for overview and quick start
