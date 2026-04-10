# File Storage Backend

The File Storage backend provides persistent key-value storage using the filesystem as the backing store. Each key-value pair is stored as a separate file, making it ideal for human-readable data, debugging, and version control scenarios. The implementation follows the same pattern as MemoryStorage for consistency across storage backends.

## Features

- **Human-readable files**: Entities stored in native format (JSON, YAML, etc.)
- **Hierarchical structure**: Entity types become directories, IDs become filenames
- **Multi-codec support**: JSON, YAML, TOML, MessagePack, BSON via `dao.CodecType`
- **Atomic writes**: Uses temp file + rename for crash safety
- **Thread-safe**: Uses RWMutex for concurrent access
- **Snapshots**: Create point-in-time copies for backup

## Use Cases

- Configuration files that can be edited manually
- Debug-friendly storage for development
- Git-friendly data that can be version controlled
- Export/import scenarios
- Human-auditable data stores

## Quick Start

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/storage/file"
    "github.com/jeremyhahn/go-quicbaas/pkg/dao"
)

// Create file store with JSON codec
config := file.DefaultConfig()
config.DataDir = "/data/entities"
config.CodecType = dao.CodecJSON

store, err := file.New(config, log)
if err != nil {
    return err
}
defer store.Close()

// Basic operations (no context)
store.Set([]byte("users/123"), []byte(`{"name":"Alice"}`))
value, _ := store.Get([]byte("users/123"))
store.Delete([]byte("users/123"))

// Range scan
results, _ := store.Scan([]byte("users/"), []byte("users/~"))

// Snapshot and restore
snapshot, _ := store.Snapshot()
newStore.Restore(snapshot)
```

## API Reference

| Method | Signature | Description |
|--------|-----------|-------------|
| `Set` | `Set(key, value []byte) error` | Store a key-value pair |
| `Get` | `Get(key []byte) ([]byte, error)` | Retrieve a value by key |
| `Delete` | `Delete(key []byte) error` | Remove a key-value pair |
| `Scan` | `Scan(start, end []byte) ([][2][]byte, error)` | Range scan returning key-value pairs |
| `Snapshot` | `Snapshot() (map[string][]byte, error)` | Create point-in-time backup |
| `Restore` | `Restore(snapshot map[string][]byte) error` | Restore from backup |
| `Len` | `Len() (int, error)` | Count total keys |
| `Close` | `Close() error` | Close the store |

## File Structure

Keys are mapped directly to filesystem paths:

```
Key: users/123         -> File: {DataDir}/users/123.json
Key: sessions/456      -> File: {DataDir}/sessions/456.yaml
Key: config/db/main    -> File: {DataDir}/config/db/main.toml
```

Example directory structure:

```
/data/entities/
  users/
    1736582400000000000.json
    1736582401000000000.json
  sessions/
    1736582500000000000.json
  config/
    database.yaml
```

## Supported Codecs

| Codec | Extension | Description |
|-------|-----------|-------------|
| `dao.CodecJSON` | `.json` | Human-readable, widely supported |
| `dao.CodecYAML` | `.yaml` | Human-readable, good for config |
| `dao.CodecTOML` | `.toml` | Human-readable, good for config |
| `dao.CodecMsgPack` | `.msgpack` | Binary, compact, fast |
| `dao.CodecBSON` | `.bson` | Binary, MongoDB compatible |
| `dao.CodecProtoBuf` | `.pb` | Binary, Protocol Buffers |

## Codec Matching

The FileStorage codec type must match the DAO codec type. The store uses the codec type only to determine the file extension - the actual encoding/decoding is done by the DAO.

```go
// Correct: Both use CodecJSON
config := file.DefaultConfig()
config.DataDir = "/data"
config.CodecType = dao.CodecJSON
store, _ := file.New(config, log)
dao, _ := dao.New(store, "users", newUser, dao.WithCodec(dao.CodecJSON))

// Incorrect: Mismatched codecs will create files with wrong extension
config.CodecType = dao.CodecYAML  // .yaml files
store, _ := file.New(config, log)
dao, _ := dao.New(store, "users", newUser, dao.WithCodec(dao.CodecJSON))  // JSON content!
```

## Comparison with Other Backends

| Feature | File | Memory | PebbleDB | Raft |
|---------|------|--------|----------|------|
| Persistence | Yes | No | Yes | Yes |
| Human-readable | Yes | N/A | No | No |
| Performance | Low | High | High | Medium |
| Scalability | Low | Medium | High | High |
| Use case | Debug/Config | Testing | Production | Distributed |

## Limitations

- **Performance**: File I/O is slower than in-memory or LSM-tree storage
- **Scalability**: Not suitable for high-volume production workloads
- **Concurrent writes**: File locking may cause contention under heavy load
- **Large files**: Each entity is a separate file; very large entities may be slow

## See Also

- [Configuration](configuration.md) - Detailed configuration options
- [DAO Documentation](../../dao/README.md) - Using DAOs with file storage
- [Codec Documentation](../../codec/README.md) - Codec configuration
