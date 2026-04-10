# State Machines

State machines implement the business logic layer between Raft consensus and storage, handling CRUD operations, ID generation, field encryption, and index management.

## Overview

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

## Generic Implementation

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

## State Machine Types

QuicRaft supports three state machine types:

### 1. Regular State Machine (RSM)

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

### 2. Concurrent State Machine

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

### 3. On-Disk State Machine

Minimizes memory usage:

```go
func NewOnDiskStateMachine(shardID, nodeID uint64) sm.OnDiskStateMachine {
    return &OnDiskStateMachine[Entity]{
        GenericStateMachine: NewGenericStateMachine[Entity](),
    }
}
```

Characteristics: lower memory footprint, all state on disk, slightly lower performance. QuicRaft manages sessions internally, so no manual session management is needed.

## Operations

### Put Operation

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

### Get Operation

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

### Delete Operation

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

### Query Operation

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

## Update Processing

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

## Snapshot Management

### Save Snapshot

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

### Recover from Snapshot

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

## ID Generation

### Hash-Based IDs

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

### Timestamp-Based IDs

```go
func (g *IDGenerator) NextTimestamp() uint64 {
    // Unix nanoseconds as ID
    return uint64(time.Now().UnixNano())
}
```

## Field Encryption

### Encryption Tags

```go
type User struct {
    ID       uint64 `json:"id" quicbaas:"id"`
    Name     string `json:"name" quicbaas:"index"`
    Email    string `json:"email" quicbaas:"index"`
    Password string `json:"password" quicbaas:"encrypt:aes256"`
    SSN      string `json:"ssn" quicbaas:"encrypt:aes128"`
}
```

### Encryptor Implementation

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

## Performance Optimization

### Batching

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

### Caching

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

## Best Practices

DO: use appropriate state machine type, implement proper error handling, use batches for bulk operations, monitor snapshot size and frequency, implement caching for hot data.

DON'T: hold locks during I/O, store very large entities (>1 MB), ignore Update() errors, skip snapshot testing, forget to close resources.

## Monitoring

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

## References

- [QuicRaft State Machine Guide](https://github.com/jeremyhahn/go-quicraft) - Library documentation
- [Go Generics Tutorial](https://go.dev/doc/tutorial/generics)

---

**Last Updated:** 2026-02-23
