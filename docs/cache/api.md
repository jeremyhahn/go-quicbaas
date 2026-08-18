# Cache — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The cache capability on every transport, plus the in-process Go API.

## Overview

The cache system is primarily accessed through the CLI and internal Go APIs. Direct REST/gRPC endpoints are available through the observability API for cache metrics.

## Transport matrix

One row per capability, showing its form on each of the six transports
(the six named in the portfolio API conventions). The transport model itself — why REST and
QUIC always carry identical rows, and why Unix and gRPC do — is in
[../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md).

SDK entry point: `Client.ClusterMgmt().Cache()`, returning
`sdk/go/client/cache.Client` (aliased as `quicbaas.CacheClient` in
`sdk/go/quicbaas.go:757`). Every method dispatches through
`transport.Request(ctx, "quicbaas.Cache", "<Method>", req)`.

| Capability | SDK method | REST (server) | gRPC | QUIC | MCP | Unix | Embedded |
|---|---|---|---|---|---|---|---|
| Read statistics | `GetCacheStats` | `GET /api/v1/cache/stats` | MISSING | as REST over HTTP/3 | MISSING | MISSING | `quicbaas.Cache/GetStats` |
| Clear entries | `ClearCache` | `POST /api/v1/cache/clear` | MISSING | as REST over HTTP/3 | MISSING | MISSING | `quicbaas.Cache/Clear` |
| Read configuration | `GetCacheConfig` | `GET /api/v1/cache/config` | MISSING | as REST over HTTP/3 | MISSING | MISSING | `quicbaas.Cache/GetConfig` |
| Update configuration | `UpdateCacheConfig` | `PUT /api/v1/cache/config` | MISSING | as REST over HTTP/3 | MISSING | MISSING | `quicbaas.Cache/UpdateConfig` |
| Invalidate | — | `POST /api/v1/cache/invalidate` mapped, **no server route** | MISSING | — | MISSING | MISSING | MISSING |
| Warm | — (CLI-local) | `POST /api/v1/cache/warm` mapped, **no server route** | MISSING | — | MISSING | MISSING | MISSING |

Cache is a **Tier 1** capability in the ownership registry, so all six
transports are required and the `MISSING` cells are real gaps, not scope
boundaries. They are written as rows rather than omitted so the gap is
countable: `sdk/go/transport/grpc/`, `sdk/go/transport/mcp/` and
`sdk/go/transport/unix/` contain no cache handler of any kind. The Unix
transport is gRPC over `AF_UNIX` — `sdk/go/transport/unix/transport.go:114`
delegates every `Request` to the embedded gRPC transport — so it inherits
gRPC's absence exactly, and closing the gRPC gap closes the Unix one with it.

**No `docs/13` gap number has been assigned to these rows.** They are recorded
here as the first written statement of the gap; assigning the identifier is the
registry owner's call, not this document's.

### Required permissions

From `pkg/api/rest/routes_permissions_generated.go`:

| Route | Auth | Resource | Action |
|---|---|---|---|
| `GET /api/v1/cache/stats` | `RouteAuthPermission` | `admin` | `status` |
| `GET /api/v1/cache/config` | `RouteAuthPermission` | `admin` | `status` |
| `POST /api/v1/cache/clear` | `RouteAuthPermission` | `admin` | `capabilities` |
| `PUT /api/v1/cache/config` | `RouteAuthPermission` | `admin` | `capabilities` |

### DEFECT — the REST/QUIC client method names do not reach the server

`sdk/go/client/cache/client.go` sends the method names `GetCacheStats`,
`ClearCache`, `GetCacheConfig` and `UpdateCacheConfig`. The REST transport's
route table, `mapCacheMethod` in `sdk/go/transport/rest/transport.go:1148`,
recognises the *short* names — `GetStats`, `Clear`, `GetConfig`,
`UpdateConfig`, `Invalidate`, `Warm` — and nothing else. Cache has no dedicated
REST handler in `Transport.Request`, so every call falls through to the default
branch and is issued as, for example, `POST /api/v1/cache/getcachestats`. No
such route is registered in `pkg/api/rest/routes.go:314-317`, so the call 404s.

The embedded transport registers the same short names
(`sdk/go/transport/embedded/transport.go:91-94`) and has the same mismatch.

The *server* routes in the table above are correct and reachable with `curl`;
it is the SDK path to them that is broken. This is recorded rather than fixed
because the fix — renaming client methods or widening the route map — is a
behavior change, and behavior changes go through the SDLC, not a docs commit.


## Go API

### CacheManager

Primary interface for cache operations.

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/cache"
```

#### Constructor

```go
func NewManager(config Config, log interfaces.Logger) (*CacheManager, error)
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

manager, err := cache.NewManager(cfg, logger)
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

#### SetWithTTL — DOES NOT EXIST

`CacheManager` has no `SetWithTTL`. TTL is a property of the
manager (`Config.L1TTL` / `Config.L2TTL`), not of an individual entry: `Set`
applies the configured TTL to every key it writes. Per-entity TTL is reached
through `CacheRegistry`, which builds one `CacheManager` per entity type from
`config.EntityCacheConfig.TTL`. Documented here rather than deleted because
previous revisions of this file advertised the method and callers were written
against it.

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
