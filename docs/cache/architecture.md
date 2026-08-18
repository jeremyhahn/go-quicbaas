# Cache — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the L1/L2 cache. Feature folder: `docs/cache/`.

## Overview

QuicBaaS provides a high-performance L1 in-memory LRU cache with entity-aware configurations. The cache follows a read-aside pattern optimized for O(1) operations.

## Components

```
┌─────────────────────────────────────────────────────────────────┐
│                      Cache Layer                                │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │   CLI       │  │  Service    │  │      Storage Layer      │  │
│  │  Commands   │  │   Layer     │  │      (Consumers)        │  │
│  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘  │
│         │                │                      │               │
│         └────────────────┼──────────────────────┘               │
│                          ▼                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                  Cache Registry                          │   │
│  │   - Per-entity cache instances                           │   │
│  │   - Configuration management                             │   │
│  │   - Global statistics aggregation                        │   │
│  └──────────────────────────┬───────────────────────────────┘   │
│                             ▼                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                  Cache Manager                           │   │
│  │   - TTL management                                       │   │
│  │   - Metrics tracking                                     │   │
│  │   - Cleanup goroutine                                    │   │
│  └──────────────────────────┬───────────────────────────────┘   │
│                             ▼                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   LRU Cache                              │   │
│  │   - Hash map + doubly-linked list                        │   │
│  │   - O(1) get/set/delete                                  │   │
│  │   - Automatic eviction                                   │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Cache Flow

### Read Path

```
Read Request
     │
     ▼
┌─────────────────────┐
│  Check L1 Cache     │
└─────────┬───────────┘
          │
    ┌─────┴─────┐
    │           │
   Hit        Miss
    │           │
    ▼           ▼
┌───────┐   ┌──────────────┐
│ Check │   │ Query        │
│ TTL   │   │ Storage      │
└───┬───┘   └──────┬───────┘
    │              │
┌───┴───┐          ▼
│       │   ┌──────────────┐
Valid  Expired│ Populate   │
│       │     │ Cache      │
│       │     └──────┬─────┘
│       │            │
│       └────────────┤
│                    │
▼                    ▼
Return value     Return value
(hit++)          (miss++)
```

### Write Path

```
Write Request
     │
     ▼
┌───────────────────────┐
│  Write to Storage     │ ◄── Primary write (persistent)
└───────────┬───────────┘
            │
            ▼
┌───────────────────────┐
│  Update L1 Cache      │ ◄── Secondary (read optimization)
└───────────────────────┘
```

**Important**: The cache is read-only in terms of persistence. Cache writes do NOT persist data to storage.

## LRU Algorithm

The LRU cache uses a hash map combined with a doubly-linked list:

```
Hash Map: O(1) key lookup
┌────────────────────────────┐
│  key1 → node1              │
│  key2 → node2              │
│  key3 → node3              │
└────────────────────────────┘
            │
            ▼
Doubly-Linked List: O(1) reordering
┌──────────────────────────────────────────┐
│                                          │
│  HEAD ↔ node1 ↔ node2 ↔ node3 ↔ TAIL     │
│  (MRU)                           (LRU)   │
│                                          │
└──────────────────────────────────────────┘
```

### Operations

| Operation | Complexity | Description |
|-----------|------------|-------------|
| Get | O(1) | Lookup in map, move to head |
| Set | O(1) | Insert at head, evict tail if full |
| Delete | O(1) | Remove from map and list |
| Evict | O(1) | Remove tail node |

### Node Structure

```go
type lruNode struct {
    key        string
    value      []byte
    expiration time.Time
    prev, next *lruNode
}
```

## Entity Cache Registry

The registry manages per-entity cache instances with independent configurations:

```
┌────────────────────────────────────────────┐
│              Cache Registry                │
├────────────────────────────────────────────┤
│                                            │
│  ┌────────────┐  ┌────────────┐            │
│  │ user cache │  │ post cache │  ...       │
│  │ TTL: 10m   │  │ disabled   │            │
│  │ Size: 5000 │  │            │            │
│  └────────────┘  └────────────┘            │
│                                            │
│  ┌─────────────────────────────────────┐   │
│  │          Global Fallback            │   │
│  │  TTL: 5m, Size: 10000               │   │
│  └─────────────────────────────────────┘   │
│                                            │
└────────────────────────────────────────────┘
```

### Entity Isolation

Each entity type has isolated:
- Cache capacity
- TTL settings
- Hit/miss metrics
- Enable/disable state

## TTL Management

### Entry Expiration

```go
type cacheEntry struct {
    value      []byte
    expiration time.Time  // TTL deadline
}

func (e *cacheEntry) IsExpired() bool {
    if e.expiration.IsZero() {
        return false  // No TTL set
    }
    return time.Now().After(e.expiration)
}
```

### Background Cleanup

A dedicated goroutine removes expired entries:

```
┌──────────────────────────────────────────┐
│          Cleanup Goroutine               │
│                                          │
│  ticker := time.NewTicker(interval)      │
│  for range ticker.C {                    │
│      scanAndRemoveExpired()              │
│  }                                       │
│                                          │
└──────────────────────────────────────────┘
```

- Default interval: 1 minute
- Non-blocking cleanup
- Atomic metrics updates

## Metrics System

### Atomic Counters

```go
type Metrics struct {
    l1Hits     atomic.Uint64
    l1Misses   atomic.Uint64
    l1Sets     atomic.Uint64
    l1Deletes  atomic.Uint64
    l1Expired  atomic.Uint64
    l1Evictions atomic.Uint64
}
```

### Hit Rate Calculation

```
Hit Rate = L1 Hits / (L1 Hits + L1 Misses) × 100%
```

### Exported Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `cache_hits_total` | Counter | Successful cache hits |
| `cache_misses_total` | Counter | Cache misses |
| `cache_evictions_total` | Counter | LRU evictions |
| `cache_expired_total` | Counter | TTL expirations |
| `cache_size` | Gauge | Current entry count |
| `cache_capacity` | Gauge | Maximum capacity |

## Thread Safety

### Synchronization Strategy

```go
type CacheManager struct {
    mu      sync.RWMutex   // Protects cache operations
    metrics *Metrics        // Atomic counters
    cache   *LRUCache       // Internal cache
}
```

### Lock Granularity

| Operation | Lock Type |
|-----------|-----------|
| Get | Read lock (RLock) |
| Set | Write lock (Lock) |
| Delete | Write lock (Lock) |
| Stats | No lock (atomic reads) |

### Defensive Copying

Byte slices are copied on entry/exit to prevent external modification:

```go
func (m *Manager) Get(key string) ([]byte, error) {
    value := m.cache.get(key)
    // Return copy to prevent external modification
    result := make([]byte, len(value))
    copy(result, value)
    return result, nil
}
```

## Integration Points

### Storage Layer

```go
type CachedStorage struct {
    storage Storage
    cache   *cache.CacheManager
}

func (s *CachedStorage) Get(key []byte) ([]byte, error) {
    // Try cache first
    if val, err := s.cache.Get(string(key)); err == nil {
        return val, nil
    }

    // Cache miss - query storage
    val, err := s.storage.Get(key)
    if err != nil {
        return nil, err
    }

    // Populate cache
    s.cache.Set(string(key), val)
    return val, nil
}
```

### Vector FSM Integration

The Vector FSM accepts a cache manager for caching search results:

```go
type VectorFSM struct {
    cache cache.CacheManager
}

func (f *VectorFSM) Search(query []float32) ([]Result, error) {
    cacheKey := hashQuery(query)

    if cached, err := f.cache.Get(cacheKey); err == nil {
        return deserialize(cached), nil
    }

    results := f.performSearch(query)
    f.cache.Set(cacheKey, serialize(results))
    return results, nil
}
```

## Memory Management

### Bounded Memory

Cache memory is bounded by:
1. **Max entries**: Configurable per entity
2. **LRU eviction**: Automatic when full
3. **TTL expiration**: Periodic cleanup

### Memory Estimation

```
Per-entry overhead: ~100-200 bytes
- Key: variable (string)
- Value: variable ([]byte copy)
- Node pointers: 16 bytes
- Metadata: ~50 bytes

Total memory ≈ max_size × (avg_key_size + avg_value_size + 200)
```

### GC Considerations

- Small, bounded cache minimizes GC pressure
- Periodic cleanup prevents memory leaks
- Defensive copying ensures no external references

## Design Decisions

### Why LRU?

| Alternative | Pros | Cons | Decision |
|-------------|------|------|----------|
| LRU | O(1), simple, predictable | No frequency tracking | **Chosen** |
| LFU | Frequency-aware | O(log n), complex | Rejected |
| ARC | Adaptive | Patent concerns, complex | Rejected |
| 2Q | Better than LRU | More memory overhead | Future option |

### Why Read-Only?

1. **Consistency**: Write path always goes to storage
2. **Simplicity**: No cache coherence needed
3. **Reliability**: Cache loss doesn't lose data
4. **Performance**: Read optimization without write overhead

### Why Entity-Aware?

1. **Fine-grained control**: Different TTLs for different data
2. **Isolation**: Hot entities don't evict cold ones
3. **Monitoring**: Per-entity hit rates
4. **Flexibility**: Enable/disable per entity

## L2 Cache (Redis)

QuicBaaS supports an optional L2 distributed cache layer using Redis:

```
L1 (Local LRU) → L2 (Redis) → Storage (Pebble)
```

### Configuration

```yaml
cache:
  l2_enabled: true
  l2_ttl: 30m
  l2_redis_addr: "localhost:6379"
  l2_redis_db: 0
  l2_redis_use_secrets: true  # Use internal secrets for password
  l2_redis_secret_path: "cache/redis/password"  # Path in internal secrets
```

### Secure Credential Management

Redis credentials should be stored in QuicBaaS's internal secrets store rather than plain text:

```go
// Store Redis password securely
internalSecrets.Store(ctx, secrets.InternalSecretRedisPassword, []byte("redis-password"))

// The cache system retrieves it automatically when l2_redis_use_secrets: true
```

### Cache Flow with L2

```
Read Request
     │
     ▼
┌─────────────────────┐
│  Check L1 Cache     │ ◄── In-memory, O(1)
└─────────┬───────────┘
          │
    ┌─────┴─────┐
    │           │
   Hit        Miss
    │           │
    ▼           ▼
┌───────┐   ┌──────────────┐
│Return │   │ Check L2     │ ◄── Redis, network call
└───────┘   │ (Redis)      │
            └──────┬───────┘
                   │
             ┌─────┴─────┐
             │           │
            Hit        Miss
             │           │
             ▼           ▼
      ┌──────────┐   ┌──────────────┐
      │Warm L1   │   │ Query        │
      │Return    │   │ Storage      │
      └──────────┘   └──────────────┘
```

## Write Policies

QuicBaaS supports three write policies for L2 cache:

### WriteThroughSync (Default)

Writes to L2 synchronously after L1 write. Provides strongest consistency but higher latency.

```go
// Set() blocks until L2 write completes
cache.Set(ctx, key, value) // Returns after L1 + L2 writes
```

### WriteThroughAsync

Writes to L2 asynchronously (fire-and-forget). Lower latency but weaker consistency.

```go
// Set() returns immediately after L1 write
cache.Set(ctx, key, value) // L2 write happens in background goroutine
```

### WriteBack

Writes only to L1. L2 writes occur when entries are evicted from L1.

```
┌──────────────────────────────────────────────────────────────┐
│                    WriteBack Flow                            │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  Set(key, value)                                             │
│       │                                                      │
│       ▼                                                      │
│  ┌─────────────────┐                                         │
│  │ Write to L1     │ ◄── Immediate, O(1)                     │
│  └─────────────────┘                                         │
│       │                                                      │
│       ▼                                                      │
│    Return (fast)                                             │
│                                                              │
│  [Later, when L1 is full...]                                 │
│                                                              │
│  ┌─────────────────┐    ┌─────────────────┐                  │
│  │ L1 Eviction     │───▶│ Async L2 Write  │                  │
│  └─────────────────┘    └─────────────────┘                  │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

**Use Cases:**
- Low write latency requirements
- High write volume workloads
- When eventual L2 consistency is acceptable

## Cache Warming

Pre-populate L1 cache from storage on startup or on-demand.

### Configuration

```yaml
cache:
  warming:
    enabled: true
    prefixes:
      - "user:"
      - "config:"
    batch_size: 100
    rate_limit_per_second: 1000
    max_keys: 10000
```

### Programmatic Warming

```go
warmer := cache.NewWarmer(cacheManager, storage, config)

// Warm a single prefix
status, err := warmer.WarmPrefix(ctx, "user:")

// Warm multiple prefixes
status, err := warmer.WarmPrefixes(ctx, []string{"user:", "config:"})

// Check warming status
status := warmer.GetStatus()
fmt.Printf("State: %s, Keys warmed: %d\n", status.State, status.KeysWarmed)
```

### CLI Warming

```bash
# Warm all configured prefixes
quicbaas cache warm

# Warm specific prefix
quicbaas cache warm --prefix "user:"

# Warm with rate limiting
quicbaas cache warm --prefix "user:" --rate 500
```

## Internal Secrets for Cache Credentials

QuicBaaS provides secure credential management for cache components using its internal secrets store.

### Storing Cache Credentials

```go
// Store Redis password in internal secrets
store.Store(ctx, secrets.InternalSecretRedisPassword, []byte("redis-password"))

// Store Redis TLS certificate
store.Store(ctx, secrets.InternalSecretRedisTLSCert, certBytes)
store.Store(ctx, secrets.InternalSecretRedisTLSKey, keyBytes)
```

### Available Secret Paths

| Constant | Path | Description |
|----------|------|-------------|
| `InternalSecretRedisPassword` | `cache/redis/password` | Redis authentication password |
| `InternalSecretRedisUsername` | `cache/redis/username` | Redis username (ACL) |
| `InternalSecretRedisTLSCert` | `cache/redis/tls/cert` | TLS certificate |
| `InternalSecretRedisTLSKey` | `cache/redis/tls/key` | TLS private key |

### Security Benefits

1. **Encrypted at Rest**: All secrets encrypted with global DEK (AES-256-GCM)
2. **No Plain Text**: Credentials never stored in config files
3. **Audit Trail**: All secret access is logged
4. **Key Rotation**: DEK rotation automatically re-encrypts secrets
5. **Cluster-Wide**: Secrets replicated across cluster nodes
