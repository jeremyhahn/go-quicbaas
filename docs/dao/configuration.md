# DAO Configuration Options

This guide covers all configuration options available when creating a GenericDAO instance.

## Overview

DAO instances are configured using functional options passed to the constructor:

```go
userDAO, err := service.CreateDAO[*User](
    svc,
    "users",
    func() *User { return &User{} },
    dao.WithShard(1),
    dao.WithCodec(dao.CodecMsgPack),
    dao.WithTimeout(30*time.Second),
)
```

## Configuration Options

### WithShard

Sets the Raft shard ID for data storage in cluster mode.

```go
dao.WithShard(shardID uint64) Option
```

**Purpose**: In cluster mode, QuicBaaS partitions data across multiple Raft shards. This option specifies which shard stores the entity data.

**When to Use**:
- **Cluster Mode**: Required for multi-node deployments to control data placement
- **Local Mode**: Ignored (single PebbleDB instance)
- **Memory Mode**: Ignored (in-memory storage)

**Default**: Shard 1

**Examples**:

```go
// Store users in shard 1
userDAO, err := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithShard(1),
)

// Store sessions in shard 2 (data partitioning)
sessionDAO, err := service.CreateDAO[*Session](
    svc, "sessions", newSession,
    dao.WithShard(2),
)

// Store analytics in dedicated shard 3
analyticsDAO, err := service.CreateDAO[*Event](
    svc, "events", newEvent,
    dao.WithShard(3),
)
```

**Best Practices**:
- Distribute high-volume entities across multiple shards for load balancing
- Keep related entities in the same shard to minimize cross-shard queries
- Plan shard assignments based on expected growth and access patterns

### WithCodec

Sets the serialization format for entity encoding/decoding.

```go
dao.WithCodec(codecType dao.CodecType) Option
```

**Always Available Codecs**:
- `dao.CodecJSON` - Standard JSON encoding (default, human-readable)
- `dao.CodecYAML` - YAML encoding (human-readable)
- `dao.CodecTOML` - TOML encoding (human-readable, good for config)
- `dao.CodecMsgPack` - MessagePack binary encoding (recommended for performance)
- `dao.CodecBSON` - BSON binary encoding (MongoDB compatible)

**Build-Tag Codecs** (require build flags):
- `dao.CodecCBOR` - CBOR binary encoding (`-tags codec_cbor`)
- `dao.CodecAvro` - Avro binary encoding (`-tags codec_avro`)
- `dao.CodecProtoBuf` - Protocol Buffers (`-tags codec_protobuf`)

Use `dao.SupportedCodecs()` and `dao.IsCodecSupported(codecType)` to discover available codecs at runtime.

**Default**: `CodecJSON`

**Examples**:

```go
// JSON: Human-readable, good for debugging
userDAO, err := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithCodec(dao.CodecJSON),
)

// MsgPack: High performance, compact binary
sessionDAO, err := service.CreateDAO[*Session](
    svc, "sessions", newSession,
    dao.WithCodec(dao.CodecMsgPack),
)

// YAML: Configuration-like entities
configDAO, err := service.CreateDAO[*Config](
    svc, "configs", newConfig,
    dao.WithCodec(dao.CodecYAML),
)
```

### WithTimeout

Sets the default timeout for all DAO operations.

```go
dao.WithTimeout(duration time.Duration) Option
```

**Purpose**: Ensures operations don't hang indefinitely. If the context passed to DAO methods doesn't have a deadline, this timeout is applied.

**Default**: 30 seconds

**Examples**:

```go
// Short timeout for quick lookups
cacheDAO, err := service.CreateDAO[*CacheEntry](
    svc, "cache", newEntry,
    dao.WithTimeout(5*time.Second),
)

// Longer timeout for batch operations
batchDAO, err := service.CreateDAO[*BatchJob](
    svc, "jobs", newJob,
    dao.WithTimeout(5*time.Minute),
)

// Very short timeout for real-time systems
realtimeDAO, err := service.CreateDAO[*Metric](
    svc, "metrics", newMetric,
    dao.WithTimeout(1*time.Second),
)
```

**Interaction with Context**:
```go
// If context already has deadline, it takes precedence
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

// Uses 10 second timeout from context, not DAO's 30 second default
user, err := userDAO.Get(ctx, 123)
```

**Best Practices**:
- Set shorter timeouts for latency-sensitive operations
- Use longer timeouts for batch processing or large scans
- Always check for `context.DeadlineExceeded` errors

## Codec Performance Comparison

### Benchmark Results

Based on typical entity serialization (struct with 10 fields):

| Codec     | Marshal Time | Unmarshal Time | Size (bytes) | Human Readable |
|-----------|--------------|----------------|--------------|----------------|
| JSON      | 100%         | 100%           | 100%         | Yes            |
| MsgPack   | 40%          | 45%            | 50%          | No             |
| BSON      | 60%          | 65%            | 70%          | No             |
| YAML      | 250%         | 300%           | 120%         | Yes            |
| TOML      | 200%         | 250%           | 115%         | Yes            |

*Relative to JSON (100% = baseline)*

### Codec Selection Guide

**Choose JSON when**:
- Debugging and development
- Need human-readable storage
- Integrating with web APIs
- Storage size is not critical

**Choose MsgPack when**:
- Production high-throughput systems
- Storage efficiency matters
- Low latency is critical
- Don't need human readability

**Choose BSON when**:
- Migrating from MongoDB
- Need binary efficiency but want some structure
- Working with complex nested documents

**Choose YAML when**:
- Storing configuration entities
- Need comments or multi-line strings
- Human editing is required

**Choose TOML when**:
- Configuration-focused entities
- Prefer structured config format
- Need clear nested sections

### Codec Examples

#### JSON - Development & Debugging

```go
type User struct {
    ID       uint64 `json:"id"`
    Name     string `json:"name"`
    Email    string `json:"email"`
    Metadata map[string]interface{} `json:"metadata"`
}

userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithCodec(dao.CodecJSON),
)
```

**Stored as**:
```json
{
  "id": 123,
  "name": "Alice",
  "email": "alice@example.com",
  "metadata": {"role": "admin", "verified": true}
}
```

#### MsgPack - Production Performance

```go
type Session struct {
    ID        uint64    `msgpack:"id"`
    UserID    uint64    `msgpack:"uid"`
    Token     string    `msgpack:"tok"`
    ExpiresAt time.Time `msgpack:"exp"`
}

sessionDAO, _ := service.CreateDAO[*Session](
    svc, "sessions", newSession,
    dao.WithCodec(dao.CodecMsgPack),
)
```

**Stored as**: Compact binary (not human-readable)
- 30-50% smaller than JSON
- 2-3x faster serialization
- Ideal for high-volume operations

#### YAML - Configuration Storage

```go
type AppConfig struct {
    ID          uint64            `yaml:"id"`
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    Settings    map[string]string `yaml:"settings"`
}

configDAO, _ := service.CreateDAO[*AppConfig](
    svc, "configs", newConfig,
    dao.WithCodec(dao.CodecYAML),
)
```

**Stored as**:
```yaml
id: 456
name: Production Config
description: |
  Multi-line description
  for configuration
settings:
  debug: "false"
  timeout: "30s"
```

## Configuration Patterns

### Multi-Environment Setup

```go
func createUserDAO(svc service.QuicBaaS, env string) (dao.GenericDAO[*User], error) {
    var codec dao.CodecType
    var timeout time.Duration

    switch env {
    case "development":
        codec = dao.CodecJSON  // Readable for debugging
        timeout = 60 * time.Second
    case "staging":
        codec = dao.CodecMsgPack
        timeout = 30 * time.Second
    case "production":
        codec = dao.CodecMsgPack  // High performance
        timeout = 10 * time.Second
    default:
        codec = dao.CodecJSON
        timeout = 30 * time.Second
    }

    return service.CreateDAO[*User](
        svc, "users", newUser,
        dao.WithCodec(codec),
        dao.WithTimeout(timeout),
    )
}
```

### Shard Distribution Strategy

```go
// Distribute entities by data characteristics
type DAORegistry struct {
    users     dao.GenericDAO[*User]      // Shard 1: Primary data
    sessions  dao.GenericDAO[*Session]   // Shard 2: Volatile data
    events    dao.GenericDAO[*Event]     // Shard 3: High-volume logs
    analytics dao.GenericDAO[*Metric]    // Shard 4: Time-series data
}

func NewDAORegistry(svc service.QuicBaaS) (*DAORegistry, error) {
    users, err := service.CreateDAO[*User](
        svc, "users", newUser,
        dao.WithShard(1),
        dao.WithCodec(dao.CodecMsgPack),
    )
    if err != nil {
        return nil, err
    }

    sessions, err := service.CreateDAO[*Session](
        svc, "sessions", newSession,
        dao.WithShard(2),
        dao.WithCodec(dao.CodecMsgPack),
        dao.WithTimeout(5*time.Second), // Fast timeouts
    )
    if err != nil {
        return nil, err
    }

    events, err := service.CreateDAO[*Event](
        svc, "events", newEvent,
        dao.WithShard(3),
        dao.WithCodec(dao.CodecMsgPack),
        dao.WithTimeout(60*time.Second), // Longer for batch writes
    )
    if err != nil {
        return nil, err
    }

    return &DAORegistry{
        users:    users,
        sessions: sessions,
        events:   events,
    }, nil
}
```

### Type-Specific Codec Selection

```go
// User entities: Balance readability and performance
userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithCodec(dao.CodecJSON),    // Readable for admin tools
    dao.WithTimeout(30*time.Second),
)

// Session tokens: Pure performance
sessionDAO, _ := service.CreateDAO[*Session](
    svc, "sessions", newSession,
    dao.WithCodec(dao.CodecMsgPack), // Compact & fast
    dao.WithTimeout(5*time.Second),  // Short-lived
)

// Audit logs: Human-readable with metadata
auditDAO, _ := service.CreateDAO[*AuditLog](
    svc, "audit", newAudit,
    dao.WithCodec(dao.CodecJSON),    // Compliance & debugging
    dao.WithTimeout(60*time.Second), // Allow batch operations
)

// System metrics: High-volume time-series
metricDAO, _ := service.CreateDAO[*Metric](
    svc, "metrics", newMetric,
    dao.WithCodec(dao.CodecMsgPack), // Maximum efficiency
    dao.WithTimeout(1*time.Second),  // Real-time constraints
)
```

## Default Configuration

When no options are provided:

```go
userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    // Defaults applied:
    // - ShardID: 1
    // - Codec: CodecJSON
    // - Timeout: 30 seconds
)
```

Equivalent to:

```go
userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithShard(1),
    dao.WithCodec(dao.CodecJSON),
    dao.WithTimeout(30*time.Second),
)
```

## Configuration Validation

Invalid configurations are caught at DAO creation:

```go
// Invalid: nil factory
dao, err := service.CreateDAO[*User](nil, "users", newUser)
// Returns: ErrInvalidConfig{Field: "service", Reason: "cannot be nil"}

// Invalid: empty entity type
dao, err := service.CreateDAO[*User](svc, "", newUser)
// Returns: ErrInvalidConfig{Field: "entityType", Reason: "cannot be empty"}

// Invalid: nil factory function
dao, err := service.CreateDAO[*User](svc, "users", nil)
// Returns: ErrInvalidConfig{Field: "newEntity", Reason: "cannot be nil"}

// CBOR codec (requires build tag: -tags codec_cbor)
dao, err := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithCodec(dao.CodecCBOR), // Available with build tag
)
// Works when built with: go build -tags codec_cbor
```

## Advanced Configuration

### Custom Context Per Operation

Override DAO timeout on a per-call basis:

```go
userDAO, _ := service.CreateDAO[*User](
    svc, "users", newUser,
    dao.WithTimeout(30*time.Second), // Default
)

// Use shorter timeout for specific call
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

user, err := userDAO.Get(ctx, 123) // Uses 2-second timeout
```

### Configuration Profiles

```go
type DAOProfile struct {
    Shard   uint64
    Codec   dao.CodecType
    Timeout time.Duration
}

var (
    FastProfile = DAOProfile{
        Shard:   1,
        Codec:   dao.CodecMsgPack,
        Timeout: 5 * time.Second,
    }

    DebugProfile = DAOProfile{
        Shard:   1,
        Codec:   dao.CodecJSON,
        Timeout: 60 * time.Second,
    }
)

func createDAOWithProfile[E dao.Entity](
    svc service.QuicBaaS,
    entityType string,
    newEntity func() E,
    profile DAOProfile,
) (dao.GenericDAO[E], error) {
    return service.CreateDAO[E](
        svc,
        entityType,
        newEntity,
        dao.WithShard(profile.Shard),
        dao.WithCodec(profile.Codec),
        dao.WithTimeout(profile.Timeout),
    )
}

// Usage
userDAO, _ := createDAOWithProfile(svc, "users", newUser, FastProfile)
```

## Next Steps

- Return to [Getting Started](./getting-started.md) for usage examples
- Read [Architecture Details](./architecture.md) for implementation insights
- See [README](./README.md) for performance benchmarks
