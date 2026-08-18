# Cache — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to enabling, sizing and observing the cache.

High-performance in-memory LRU caching with entity-aware configuration.

## Overview

QuicBaaS provides an L1 in-memory cache optimized for read-heavy workloads. The cache uses LRU eviction, supports per-entity TTLs, and provides O(1) operations for all cache access patterns.

## Key Features

| Feature | Description |
|---------|-------------|
| **Two-Tier Caching** | L1 in-memory LRU + optional L2 Redis distributed cache |
| **LRU Eviction** | Automatic eviction of least recently used entries |
| **TTL Expiration** | Time-to-live based expiration with background cleanup |
| **Entity-Specific Config** | Per-entity cache settings (TTL, size, enable/disable) |
| **Write Policies** | WriteThroughSync, WriteThroughAsync, WriteBack |
| **Cache Warming** | Pre-populate cache from storage on startup |
| **Secure Credentials** | Redis password stored in encrypted internal secrets |
| **Metrics Tracking** | Real-time hit/miss rates and performance monitoring |
| **Thread-Safe** | Concurrent access with mutex protection |

## Quick Start

### Enable Cache

```yaml
# quicbaas.yaml
cache:
  enabled: true
  default_ttl: 5m
  max_size: 10000

  entities:
    user:
      enabled: true
      ttl: 10m
      max_size: 5000
```

### View Statistics

```bash
quicbaas cache stats
```

```
ENTITY      ENABLED   SIZE     CAPACITY   HIT RATE   HITS
user        true      1,234    5,000      95.12%     45,678
post        true      567      2,000      88.50%     12,345
```

### Clear Cache

```bash
quicbaas cache clear user --yes
```

### Enable/Disable Per Entity

```bash
# Enable caching for user entity
quicbaas cache enable user

# Disable caching for post entity
quicbaas cache disable post
```

## Architecture

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │
       ▼
┌─────────────────────────┐
│    Cache Registry       │  ◄── Per-entity configuration
└──────┬──────────────────┘
       │
       ▼
┌─────────────────────────┐
│    Cache Manager        │  ◄── TTL, metrics, cleanup
└──────┬──────────────────┘
       │
       ▼
┌─────────────────────────┐
│      LRU Cache          │  ◄── O(1) operations
└─────────────────────────┘
```

### Cache Flow

```
Read Request → Check Cache → Hit? → Return (hit++)
                    │
                   Miss
                    │
                    ▼
              Query Storage → Populate Cache → Return (miss++)
```

## Performance

| Operation | Complexity | Latency |
|-----------|------------|---------|
| Get | O(1) | <1μs |
| Set | O(1) | <1μs |
| Delete | O(1) | <1μs |
| Eviction | O(1) | <1μs |

**Throughput**: 1M+ ops/sec per cache instance

## Building with Redis L2 Cache

Redis L2 cache support is an optional build feature. By default, QuicBaaS builds without Redis dependencies to minimize binary size and external requirements.

### Enable Redis Support

```bash
# Build with Redis L2 cache support
make build WITH_L2_REDIS=1

# Build FIPS-compliant version with Redis
make build-fips WITH_L2_REDIS=1

# Run tests with Redis support
make test-cache WITH_L2_REDIS=1
```

When building without Redis support (`WITH_L2_REDIS=0` or omitted), stub implementations are provided that return `ErrNotImplemented` errors if Redis L2 features are accessed.

### Docker Build with Redis

```bash
docker build --build-arg WITH_L2_REDIS=1 -t quicbaas:redis .
```

## Configuration Example

```yaml
cache:
  enabled: true
  default_ttl: 5m
  max_size: 10000
  cleanup_interval: 1m
  write_through: true

  # L2 Redis Cache (optional)
  l2_enabled: true
  l2_ttl: 30m
  l2_redis_addr: "localhost:6379"
  l2_redis_db: 0
  l2_redis_use_secrets: true  # Use internal secrets for password (recommended)
  l2_redis_secret_path: "cache/redis/password"

  # Cache warming (optional)
  warming:
    enabled: true
    prefixes:
      - "user:"
      - "config:"

  entities:
    # Static data: longer TTL
    config:
      enabled: true
      ttl: 1h

    # User profiles: moderate TTL
    user:
      enabled: true
      ttl: 10m
      max_size: 5000

    # Real-time data: disable
    stock_price:
      enabled: false
```

## Metrics

| Metric | Description |
|--------|-------------|
| `l1_hits` | L1 cache hits |
| `l1_misses` | L1 cache misses |
| `l1_evictions` | LRU evictions |
| `l1_expired` | TTL expirations |
| `l1_size` | Current L1 entries |
| `l1_capacity` | Maximum L1 capacity |
| `l2_hits` | L2 cache hits |
| `l2_misses` | L2 cache misses |
| `l2_errors` | L2 connection/operation errors |
| `l2_sets` | L2 write operations |
| `writeback_success` | WriteBack L2 writes on eviction |
| `writeback_failures` | Failed WriteBack L2 writes |

### Hit Rate Formula

```
l1_hit_rate = l1_hits / (l1_hits + l1_misses) × 100%
l2_hit_rate = l2_hits / (l2_hits + l2_misses) × 100%
```

**Target**: >80% L1 hit rate, >60% L2 hit rate for optimal performance

## Best Practices

1. **Size cache based on working set** - Cover ~80% of frequently accessed data
2. **Set appropriate TTLs** - Match data volatility (1m-1h typical range)
3. **Monitor hit rates** - Low hit rate indicates undersized cache or poor access patterns
4. **Disable for large objects** - Cache memory overhead is per-entry
5. **Remember read-only semantics** - Always write to storage first

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Cache internals, LRU algorithm, data flow |
| [Configuration](engineering.md) | Complete configuration reference |
| [CLI Usage](cli.md) | Command-line interface guide |
| [API Reference](api.md) | Go API and metrics endpoints |

## See Also

- [Storage Engine](../data-services/architecture.md)
- Performance Tuning
- CLI Reference
