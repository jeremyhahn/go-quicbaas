# Cache — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: configuration, invariants, errors, tests.

## Configuration File

```yaml
# quicbaas.yaml
cache:
  # Master switch for cache system
  enabled: true

  # Default time-to-live for cache entries
  # Format: duration (e.g., 5m, 1h, 30s)
  # 0 = no expiration
  default_ttl: 5m

  # Maximum entries across all entity types
  max_size: 10000

  # Frequency of expired entry cleanup
  cleanup_interval: 1m

  # Write-through behavior for L2 cache
  write_through: true

  # L2 cache settings (requires l2_redis build tag)
  l2_enabled: false
  l2_ttl: 30m
  l2_redis_addr: "localhost:6379"
  l2_redis_password: ""
  l2_redis_db: 0

  # Per-entity configuration
  entities:
    user:
      enabled: true
      ttl: 10m
      max_size: 5000
    post:
      enabled: false
    comment:
      enabled: true
      ttl: 2m
      max_size: 1000
```

## Global Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `enabled` | bool | `true` | Enable/disable entire cache system |
| `default_ttl` | duration | `5m` | Default entry expiration time |
| `max_size` | int | `10000` | Maximum entries (shared across entities) |
| `cleanup_interval` | duration | `1m` | Background cleanup frequency |
| `write_through` | bool | `true` | Write-through to L2 cache (requires `l2_redis` build tag) |

## Entity Configuration

Configure caching behavior per entity type:

```yaml
cache:
  entities:
    # Entity name as key
    user:
      # Enable/disable caching for this entity
      enabled: true

      # TTL for this entity (overrides default_ttl)
      ttl: 10m

      # Max entries for this entity (0 = proportional allocation)
      max_size: 5000

      # Write-through behavior for this entity
      write_through: true
```

### Entity Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `enabled` | bool | `true` | Enable caching for this entity |
| `ttl` | duration | `default_ttl` | Entity-specific TTL |
| `max_size` | int | proportional | Maximum entries for this entity |
| `write_through` | bool | `true` | Write-through to L2 (future) |

### Proportional Allocation

When `max_size` is 0 or not specified, capacity is allocated proportionally:

```
entity_capacity = global_max_size / number_of_enabled_entities
```

Example:
- `max_size: 10000`
- 4 enabled entities
- Each entity gets ~2500 entries

## L2 Cache Settings (Redis Backend)

```yaml
cache:
  # Enable L2 (Redis) cache tier
  l2_enabled: false

  # TTL for L2 entries
  l2_ttl: 30m

  # Redis connection settings
  l2_redis_addr: "localhost:6379"
  l2_redis_password: ""
  l2_redis_db: 0
```

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `l2_enabled` | bool | `false` | Enable L2 cache tier |
| `l2_ttl` | duration | `30m` | L2 entry expiration |
| `l2_redis_addr` | string | `localhost:6379` | Redis server address |
| `l2_redis_password` | string | `""` | Redis password |
| `l2_redis_db` | int | `0` | Redis database number |

## Environment Variables

**The cache subsystem has no environment-variable overrides.** Configuration
comes from the `cache:` block of the config file only.

`pkg/config/loader.go` reads exactly one explicit list of variables in
`applyEnvironmentOverrides`, and no cache key is in it. `Loader.Load` does call
`viper.AutomaticEnv()` with the `QUICBAAS` prefix, but the config struct is
populated by `Unmarshal`, and viper's automatic binding does not reach
`Unmarshal` targets for keys that were never registered with `SetDefault` or
`BindEnv` — and this package registers none. A `QUICBAAS_CACHE_MAX_SIZE` in the
environment is therefore silently ignored.

Earlier revisions of this file listed `QUICBAAS_CACHE_ENABLED`,
`QUICBAAS_CACHE_DEFAULT_TTL`, `QUICBAAS_CACHE_MAX_SIZE`,
`QUICBAAS_CACHE_CLEANUP_INTERVAL`, `QUICBAAS_CACHE_L2_ENABLED` and
`QUICBAAS_CACHE_L2_REDIS_ADDR` as working overrides. None of them are read
anywhere in the tree. The list is recorded here so an operator who set them and
saw no effect learns why, rather than concluding the cache is broken.

## Runtime Configuration

### View Configuration

```bash
# Show all cache configuration
quicbaas cache config

# Show specific entity configuration
quicbaas cache config user

# Output as JSON
quicbaas cache config --output json
```

### Dynamic Updates

```bash
# Enable caching for an entity
quicbaas cache enable user

# Disable caching for an entity
quicbaas cache disable post
```

**Note**: Dynamic updates are session-scoped. For persistent changes, update the configuration file.

## Configuration Examples

### High-Performance Workload

Maximize cache capacity for read-heavy workloads:

```yaml
cache:
  enabled: true
  default_ttl: 15m           # Longer TTL for stable data
  max_size: 50000            # Large cache capacity
  cleanup_interval: 5m       # Less frequent cleanup

  entities:
    user:
      enabled: true
      ttl: 30m               # Users rarely change
      max_size: 20000
    session:
      enabled: true
      ttl: 5m                # Sessions are volatile
      max_size: 10000
```

### Memory-Constrained Environment

Minimize memory usage:

```yaml
cache:
  enabled: true
  default_ttl: 2m            # Short TTL
  max_size: 1000             # Small cache
  cleanup_interval: 30s      # Frequent cleanup

  entities:
    # Only cache critical entities
    user:
      enabled: true
      max_size: 500
    post:
      enabled: false         # Disable for large entities
    document:
      enabled: false         # Disable for large entities
```

### Real-Time Data

Disable caching for frequently changing data:

```yaml
cache:
  enabled: true
  default_ttl: 30s           # Very short default

  entities:
    stock_price:
      enabled: false         # Real-time data
    user_location:
      enabled: false         # Frequently updated
    static_config:
      enabled: true
      ttl: 1h                # Rarely changes
```

### Per-Tenant Isolation

Different TTLs per data type:

```yaml
cache:
  enabled: true
  default_ttl: 5m

  entities:
    # Tenant configuration (stable)
    tenant_config:
      enabled: true
      ttl: 1h
      max_size: 100

    # User data (moderate churn)
    user:
      enabled: true
      ttl: 10m
      max_size: 5000

    # Audit logs (read-heavy)
    audit_log:
      enabled: true
      ttl: 30m
      max_size: 10000

    # Real-time metrics
    metrics:
      enabled: false
```

## Validation Rules

The configuration is validated on startup:

| Rule | Error |
|------|-------|
| `max_size` must be > 0 | `invalid max_size: must be positive` |
| `default_ttl` must be >= 0 | `invalid default_ttl: must be non-negative` |
| `cleanup_interval` must be > 0 | `invalid cleanup_interval: must be positive` |
| Entity `max_size` must be >= 0 | `invalid entity max_size` |
| Entity `ttl` must be >= 0 | `invalid entity ttl` |

## Best Practices

### 1. Size Based on Working Set

```yaml
# Analyze access patterns
# Cache size should cover ~80% of frequently accessed data
max_size: 10000  # Adjust based on monitoring
```

### 2. TTL Based on Data Volatility

| Data Type | Recommended TTL |
|-----------|-----------------|
| Static config | 1h - 24h |
| User profiles | 10m - 30m |
| Session data | 1m - 5m |
| Real-time data | Disable caching |

### 3. Monitor and Adjust

```bash
# Check hit rate (target: >80%)
quicbaas cache stats | grep "Hit Rate"

# If hit rate is low:
# - Increase max_size
# - Increase TTL
# - Check access patterns
```

### 4. Entity-Specific Tuning

```yaml
entities:
  # High-frequency, stable data
  user:
    ttl: 15m
    max_size: 10000

  # Low-frequency, large data
  document:
    enabled: false  # Don't cache large objects
```
