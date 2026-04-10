# quicbaas cache

Cache management commands for QuicBaaS.

## Overview

The cache system provides L1 (in-memory LRU) caching with entity-specific configurations. These commands allow you to monitor cache performance, clear cache data, and manage per-entity caching settings.

## Commands

### stats

Show cache statistics including hits, misses, size, and hit rates.

```bash
quicbaas cache stats [entity-type]
```

#### Flags
- `--details`: Show detailed statistics breakdown
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Show all cache statistics
quicbaas cache stats

# Show statistics for a specific entity
quicbaas cache stats user

# Show stats as JSON
quicbaas cache stats --output json

# Show stats with detailed breakdown
quicbaas cache stats user --details
```

### clear

Clear all cache entries or entries for a specific entity type.

```bash
quicbaas cache clear [entity-type]
```

#### Flags
- `-y, --yes`: Skip confirmation prompt
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Clear all cache entries (with confirmation)
quicbaas cache clear

# Clear cache for a specific entity type
quicbaas cache clear user

# Clear without confirmation
quicbaas cache clear --yes

# Clear specific entity without confirmation
quicbaas cache clear user --yes
```

### config

Show current cache configuration settings.

```bash
quicbaas cache config [entity-type]
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Show all cache configuration
quicbaas cache config

# Show configuration for a specific entity
quicbaas cache config user

# Show config as JSON
quicbaas cache config --output json
```

### enable

Enable caching for a specific entity type.

```bash
quicbaas cache enable <entity-type>
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Enable caching for user entity
quicbaas cache enable user

# Enable caching for post entity
quicbaas cache enable post
```

### disable

Disable caching for a specific entity type.

```bash
quicbaas cache disable <entity-type>
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Disable caching for user entity
quicbaas cache disable user

# Disable caching for post entity
quicbaas cache disable post
```

## Notes

- Cache is automatically warmed on first access
- Disabling cache for an entity clears all cached entries for that entity
- Cache statistics are updated in real-time
- TTL and size settings are configurable per entity
- Hit rate is calculated as: (hits / (hits + misses)) * 100
