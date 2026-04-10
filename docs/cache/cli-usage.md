# Cache CLI Usage

## Command Overview

```bash
quicbaas cache <command> [options]
```

| Command | Description |
|---------|-------------|
| `stats` | Show cache statistics |
| `clear` | Clear cache entries |
| `config` | Show cache configuration |
| `enable` | Enable caching for an entity |
| `disable` | Disable caching for an entity |

## Show Statistics

Display cache performance metrics.

### Usage

```bash
quicbaas cache stats [entity-type] [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | | `table` | Output format (json, yaml, table) |
| `--details` | | `false` | Show detailed breakdown |

### Examples

```bash
# Show all cache statistics
quicbaas cache stats

# Show statistics for specific entity
quicbaas cache stats user

# Show stats as JSON
quicbaas cache stats --output json

# Show detailed breakdown
quicbaas cache stats user --details
```

### Output Example

```
Entity Type: user
Enabled:     true
L1 Size:     1,234 / 5,000
L1 Hits:     45,678
L1 Misses:   2,345
Hit Rate:    95.12%
Evictions:   123
```

### JSON Output

```json
{
  "entity_type": "user",
  "enabled": true,
  "stats": {
    "l1_size": 1234,
    "l1_capacity": 5000,
    "l1_hits": 45678,
    "l1_misses": 2345,
    "l1_hit_rate": 0.9512
  }
}
```

### All Entities Output

```bash
quicbaas cache stats
```

```
ENTITY      ENABLED   SIZE     CAPACITY   HIT RATE   HITS      MISSES
user        true      1,234    5,000      95.12%     45,678    2,345
post        true      567      2,000      88.50%     12,345    1,609
comment     false     -        -          -          -         -
session     true      2,100    3,000      92.30%     98,765    8,234
```

## Clear Cache

Remove cached entries.

### Usage

```bash
quicbaas cache clear [entity-type] [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--yes` | `-y` | `false` | Skip confirmation prompt |
| `--output` | | `table` | Output format (json, yaml, table) |

### Examples

```bash
# Clear all cache entries (with confirmation)
quicbaas cache clear

# Clear cache for specific entity
quicbaas cache clear user

# Clear without confirmation
quicbaas cache clear --yes

# Clear specific entity without confirmation
quicbaas cache clear user --yes
```

### Output Example

```
Are you sure you want to clear the cache for entity type 'user'? (y/N): y
Cache cleared successfully
Entries cleared: 1,234
Duration: 2ms
```

### JSON Output

```bash
quicbaas cache clear user --yes --output json
```

```json
{
  "entity_type": "user",
  "entries_cleared": 1234,
  "duration_ms": 2,
  "timestamp": "2024-01-15T10:30:00Z",
  "success": true
}
```

## Show Configuration

Display current cache settings.

### Usage

```bash
quicbaas cache config [entity-type] [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | | `table` | Output format (json, yaml, table) |

### Examples

```bash
# Show all cache configuration
quicbaas cache config

# Show configuration for specific entity
quicbaas cache config user

# Show config as JSON
quicbaas cache config --output json

# Show config as YAML
quicbaas cache config --output yaml
```

### Output Example

```bash
quicbaas cache config
```

```
Global Configuration:
  Enabled:          true
  Default TTL:      5m
  Max Size:         10,000
  Cleanup Interval: 1m

Entity Configurations:
ENTITY      ENABLED   TTL     MAX SIZE
user        true      10m     5,000
post        false     -       -
comment     true      2m      1,000
session     true      5m      3,000
```

### Entity Configuration Output

```bash
quicbaas cache config user
```

```
Entity Type: user
Enabled:     true
TTL:         10m
Max Size:    5,000
```

### JSON Output

```json
{
  "global_enabled": true,
  "default_ttl": "5m",
  "max_size": 10000,
  "cleanup_interval": "1m",
  "entity_configs": {
    "user": {
      "enabled": true,
      "ttl": "10m",
      "max_size": 5000
    },
    "post": {
      "enabled": false
    }
  }
}
```

## Enable Caching

Dynamically enable caching for an entity type.

### Usage

```bash
quicbaas cache enable <entity-type> [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | | `table` | Output format (json, yaml, table) |

### Examples

```bash
# Enable caching for user entity
quicbaas cache enable user

# Enable caching for post entity
quicbaas cache enable post
```

### Output Example

```
Caching enabled for entity type: user
Using default TTL: 5m
Using proportional capacity allocation
```

### JSON Output

```json
{
  "entity_type": "user",
  "enabled": true,
  "timestamp": "2024-01-15T10:30:00Z",
  "success": true,
  "message": "Caching enabled for entity type: user"
}
```

### Error Cases

```bash
# Already enabled
quicbaas cache enable user
Error: caching is already enabled for entity type: user
```

## Disable Caching

Dynamically disable caching for an entity type.

### Usage

```bash
quicbaas cache disable <entity-type> [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | | `table` | Output format (json, yaml, table) |

### Examples

```bash
# Disable caching for user entity
quicbaas cache disable user

# Disable caching for post entity
quicbaas cache disable post
```

### Output Example

```
Caching disabled for entity type: user
Cached entries cleared: 1,234
```

**Note**: Disabling cache automatically clears all cached entries for that entity.

### JSON Output

```json
{
  "entity_type": "user",
  "enabled": false,
  "timestamp": "2024-01-15T10:30:00Z",
  "success": true,
  "message": "Caching disabled for entity type: user"
}
```

## Common Options

Global options available for all cache commands:

| Option | Description |
|--------|-------------|
| `--server` | QuicBaaS server address |
| `--token` | Authentication token |
| `--config` | Configuration file path |

## Practical Examples

### Monitor Cache Health

```bash
#!/bin/bash
# Cache health check script

# Check hit rate
HIT_RATE=$(quicbaas cache stats --output json | jq '.stats.l1_hit_rate')

if (( $(echo "$HIT_RATE < 0.5" | bc -l) )); then
    echo "WARNING: Low cache hit rate: $HIT_RATE"
    echo "Consider increasing cache size or TTL"
fi
```

### Clear Stale Data

```bash
# Clear cache for entities that need fresh data
quicbaas cache clear user --yes
quicbaas cache clear session --yes

echo "Cache cleared for user and session entities"
```

### Export Stats to Monitoring

```bash
# Export cache stats for Prometheus
quicbaas cache stats --output json | jq -r '
  .entity_stats | to_entries[] |
  "quicbaas_cache_hits_total{entity=\"\(.key)\"} \(.value.stats.l1_hits)\n" +
  "quicbaas_cache_misses_total{entity=\"\(.key)\"} \(.value.stats.l1_misses)\n" +
  "quicbaas_cache_hit_rate{entity=\"\(.key)\"} \(.value.stats.l1_hit_rate)"
'
```

### Dynamic Cache Management

```bash
#!/bin/bash
# Enable caching for high-traffic entities during peak hours

PEAK_ENTITIES="user session product"
OFF_PEAK_ENTITIES="analytics audit_log"

if [[ "$1" == "peak" ]]; then
    for entity in $PEAK_ENTITIES; do
        quicbaas cache enable "$entity" 2>/dev/null || true
    done
    echo "Peak mode: Enabled caching for high-traffic entities"
elif [[ "$1" == "off-peak" ]]; then
    for entity in $OFF_PEAK_ENTITIES; do
        quicbaas cache enable "$entity" 2>/dev/null || true
    done
    echo "Off-peak mode: Enabled caching for analytics entities"
fi
```

### Troubleshooting Script

```bash
#!/bin/bash
# Cache troubleshooting script

echo "=== Cache Statistics ==="
quicbaas cache stats

echo ""
echo "=== Cache Configuration ==="
quicbaas cache config

echo ""
echo "=== Hit Rate Analysis ==="
quicbaas cache stats --output json | jq -r '
  .entity_stats | to_entries[] |
  select(.value.stats.l1_hit_rate < 0.8) |
  "LOW HIT RATE: \(.key) - \(.value.stats.l1_hit_rate * 100 | floor)%"
'

echo ""
echo "=== Capacity Check ==="
quicbaas cache stats --output json | jq -r '
  .entity_stats | to_entries[] |
  select(.value.stats.l1_size > .value.stats.l1_capacity * 0.9) |
  "NEAR CAPACITY: \(.key) - \(.value.stats.l1_size)/\(.value.stats.l1_capacity)"
'
```
