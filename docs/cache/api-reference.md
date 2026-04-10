# Cache API Reference

## Overview

The cache system is primarily accessed through the CLI and internal Go APIs. Direct REST/gRPC endpoints are available through the observability API for cache metrics.

## Go API

### CacheManager

Primary interface for cache operations.

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/cache"
```

#### Constructor

```go
func NewCacheManager(config Config, logger interfaces.Logger) (*CacheManager, error)
```

**Parameters:**
- `config`: Cache configuration (size, TTL, cleanup interval)
- `logger`: Logger interface for debug output

**Example:**
```go
cfg := cache.Config{
    L1Size:          10000,
    L1TTL:           5 * time.Minute,
    CleanupInterval: 1 * time.Minute,
}

manager, err := cache.NewCacheManager(cfg, logger)
if err != nil {
    return err
}
defer manager.Close()
```

#### Get

Retrieve a value from cache.

```go
func (m *CacheManager) Get(ctx context.Context, key string) ([]byte, error)
```

**Parameters:**
- `ctx`: Context for cancellation
- `key`: Cache key

**Returns:**
- `[]byte`: Cached value (copy)
- `error`: `ErrNotFound` if key doesn't exist or is expired

**Example:**
```go
value, err := manager.Get(ctx, "user:123")
if err != nil {
    if errors.Is(err, cache.ErrNotFound) {
        // Cache miss - fetch from storage
    }
    return err
}
```

#### Set

Store a value in cache.

```go
func (m *CacheManager) Set(ctx context.Context, key string, value []byte) error
```

**Parameters:**
- `ctx`: Context for cancellation
- `key`: Cache key
- `value`: Value to cache (copied internally)

**Returns:**
- `error`: Error if set fails

**Example:**
```go
err := manager.Set(ctx, "user:123", userData)
if err != nil {
    return err
}
```

#### SetWithTTL

Store a value with custom TTL.

```go
func (m *CacheManager) SetWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error
```

**Parameters:**
- `ctx`: Context for cancellation
- `key`: Cache key
- `value`: Value to cache
- `ttl`: Time-to-live for this entry

**Example:**
```go
// Cache session with 5-minute TTL
err := manager.SetWithTTL(ctx, "session:abc", sessionData, 5*time.Minute)
```

#### Delete

Remove a value from cache.

```go
func (m *CacheManager) Delete(ctx context.Context, key string) error
```

**Parameters:**
- `ctx`: Context for cancellation
- `key`: Cache key to delete

**Example:**
```go
err := manager.Delete(ctx, "user:123")
```

#### Stats

Get cache statistics.

```go
func (m *CacheManager) Stats() Stats
```

**Returns:**
```go
type Stats struct {
    Enabled    bool
    L1Size     int       // Current entries
    L1Capacity int       // Maximum capacity
    L1Hits     uint64    // Total hits
    L1Misses   uint64    // Total misses
    L1HitRate  float64   // Hit rate (0.0-1.0)
}
```

**Example:**
```go
stats := manager.Stats()
fmt.Printf("Hit rate: %.2f%%\n", stats.L1HitRate * 100)
```

#### Close

Shutdown cache and cleanup goroutine.

```go
func (m *CacheManager) Close() error
```

**Example:**
```go
defer manager.Close()
```

### CacheRegistry

Entity-aware cache management.

#### Constructor

```go
func NewCacheRegistry(config *config.EnhancedCacheConfig, logger interfaces.Logger) (*CacheRegistry, error)
```

**Example:**
```go
registry, err := cache.NewCacheRegistry(&cfg.Phase4.EnhancedCache, logger)
if err != nil {
    return err
}
defer registry.Close()
```

#### GetEntityCache

Get cache manager for specific entity type.

```go
func (r *CacheRegistry) GetEntityCache(entityType string) *EntityCache
```

**Example:**
```go
userCache := registry.GetEntityCache("user")
value, err := userCache.Get(ctx, "user:123")
```

#### GetStats

Get statistics for entity type.

```go
func (r *CacheRegistry) GetStats(entityType string) Stats
```

#### GetAllStats

Get statistics for all entities.

```go
func (r *CacheRegistry) GetAllStats() map[string]Stats
```

#### IsCacheEnabled

Check if caching is enabled for entity.

```go
func (r *CacheRegistry) IsCacheEnabled(entityType string) bool
```

#### UpdateEntityConfig

Update entity configuration at runtime.

```go
func (r *CacheRegistry) UpdateEntityConfig(entityType string, cfg config.EntityCacheConfig) error
```

#### Clear

Clear all cached entries.

```go
func (r *CacheRegistry) Clear()
```

### LRUCache

Low-level LRU cache implementation.

```go
func NewLRUCache(capacity int) *LRUCache
```

#### Methods

```go
func (c *LRUCache) Get(key string) ([]byte, bool)
func (c *LRUCache) Set(key string, value []byte, expiration time.Time)
func (c *LRUCache) Delete(key string) bool
func (c *LRUCache) Len() int
func (c *LRUCache) Clear()
```

## Service Layer Integration

### Service Builder

Inject cache manager into service.

```go
service := quicbaas.NewService(
    quicbaas.WithCacheManager(cacheManager),
    quicbaas.WithStorage(storage),
    quicbaas.WithLogger(logger),
)
```

### Cache-Aware Storage

Wrap storage with caching layer.

```go
type CachedStorage struct {
    storage storage.Storage
    cache   *cache.CacheManager
}

func (s *CachedStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
    // Try cache first
    if val, err := s.cache.Get(ctx, string(key)); err == nil {
        return val, nil
    }

    // Cache miss
    val, err := s.storage.Get(key)
    if err != nil {
        return nil, err
    }

    // Populate cache
    s.cache.Set(ctx, string(key), val)
    return val, nil
}

func (s *CachedStorage) Put(ctx context.Context, key, value []byte) error {
    // Write to storage first
    if err := s.storage.Put(key, value); err != nil {
        return err
    }

    // Update cache
    s.cache.Set(ctx, string(key), value)
    return nil
}

func (s *CachedStorage) Delete(ctx context.Context, key []byte) error {
    // Invalidate cache first
    s.cache.Delete(ctx, string(key))

    // Delete from storage
    return s.storage.Delete(key)
}
```

## Metrics API

### Observability Endpoint

```http
GET /api/v1/metrics/cache
Authorization: Bearer {token}
```

**Response:**
```json
{
  "global": {
    "enabled": true,
    "total_entries": 5234,
    "total_capacity": 10000
  },
  "entities": {
    "user": {
      "enabled": true,
      "size": 1234,
      "capacity": 5000,
      "hits": 45678,
      "misses": 2345,
      "hit_rate": 0.9512,
      "evictions": 123
    }
  }
}
```

### Prometheus Metrics

Exposed at `/metrics`:

```prometheus
# HELP quicbaas_cache_hits_total Total cache hits
# TYPE quicbaas_cache_hits_total counter
quicbaas_cache_hits_total{entity="user"} 45678
quicbaas_cache_hits_total{entity="post"} 12345

# HELP quicbaas_cache_misses_total Total cache misses
# TYPE quicbaas_cache_misses_total counter
quicbaas_cache_misses_total{entity="user"} 2345
quicbaas_cache_misses_total{entity="post"} 1609

# HELP quicbaas_cache_hit_rate Cache hit rate
# TYPE quicbaas_cache_hit_rate gauge
quicbaas_cache_hit_rate{entity="user"} 0.9512
quicbaas_cache_hit_rate{entity="post"} 0.8850

# HELP quicbaas_cache_size Current cache size
# TYPE quicbaas_cache_size gauge
quicbaas_cache_size{entity="user"} 1234
quicbaas_cache_size{entity="post"} 567

# HELP quicbaas_cache_capacity Maximum cache capacity
# TYPE quicbaas_cache_capacity gauge
quicbaas_cache_capacity{entity="user"} 5000
quicbaas_cache_capacity{entity="post"} 2000

# HELP quicbaas_cache_evictions_total Total evictions
# TYPE quicbaas_cache_evictions_total counter
quicbaas_cache_evictions_total{entity="user"} 123
quicbaas_cache_evictions_total{entity="post"} 45
```

## Error Types

```go
package cache

var (
    // ErrNotFound indicates key doesn't exist in cache
    ErrNotFound = errors.New("cache: key not found")

    // ErrExpired indicates entry has expired
    ErrExpired = errors.New("cache: entry expired")

    // ErrClosed indicates cache manager is closed
    ErrClosed = errors.New("cache: manager closed")

    // ErrDisabled indicates caching is disabled
    ErrDisabled = errors.New("cache: caching disabled for entity")

    // ErrInvalidConfig indicates invalid configuration
    ErrInvalidConfig = errors.New("cache: invalid configuration")
)
```

## Thread Safety

All cache operations are thread-safe:

| Operation | Thread Safety | Lock Type |
|-----------|---------------|-----------|
| Get | Safe | RLock |
| Set | Safe | Lock |
| Delete | Safe | Lock |
| Stats | Safe | Atomic reads |
| Clear | Safe | Lock |
| Close | Safe | Lock |

## Usage Patterns

### Cache-Aside Pattern

```go
func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    cacheKey := "user:" + id

    // Try cache
    if data, err := s.cache.Get(ctx, cacheKey); err == nil {
        return deserializeUser(data)
    }

    // Cache miss - fetch from storage
    user, err := s.storage.GetUser(id)
    if err != nil {
        return nil, err
    }

    // Populate cache
    data, _ := serializeUser(user)
    s.cache.Set(ctx, cacheKey, data)

    return user, nil
}
```

### Write-Invalidate Pattern

```go
func (s *UserService) UpdateUser(ctx context.Context, user *User) error {
    // Invalidate cache first
    s.cache.Delete(ctx, "user:"+user.ID)

    // Write to storage
    return s.storage.UpdateUser(user)
}
```

### Write-Through Pattern

```go
func (s *UserService) UpdateUser(ctx context.Context, user *User) error {
    // Write to storage
    if err := s.storage.UpdateUser(user); err != nil {
        return err
    }

    // Update cache
    data, _ := serializeUser(user)
    s.cache.Set(ctx, "user:"+user.ID, data)

    return nil
}
```
