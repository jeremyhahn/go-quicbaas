# File Storage Configuration

This document describes the configuration options for the File Storage backend.

## Import Path

```go
import "github.com/quicbaas/quicbaas/pkg/storage/file"
```

## Configuration Struct

```go
type Config struct {
    DataDir         string         // Required: root directory for files
    CodecType       dao.CodecType  // Determines file extension
    FilePermissions os.FileMode    // Permission mode for created files
    DirPermissions  os.FileMode    // Permission mode for created directories
    SyncWrites      bool           // Sync writes to disk for durability
}
```

## Configuration Options

### DataDir (Required)

The root directory where all data files are stored. Entity types become subdirectories, and entity IDs become filenames.

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/var/lib/quicbaas/data"
```

The directory is created automatically if it doesn't exist.

### CodecType

Determines the file extension for stored files. This should match the codec configured on the DAO.

| Codec | Extension |
|-------|-----------|
| `dao.CodecJSON` | `.json` |
| `dao.CodecYAML` | `.yaml` |
| `dao.CodecTOML` | `.toml` |
| `dao.CodecMsgPack` | `.msgpack` |
| `dao.CodecBSON` | `.bson` |
| `dao.CodecProtoBuf` | `.pb` |
| `dao.CodecCBOR` | `.cbor` |

**Default**: `dao.CodecJSON`

### FilePermissions

The Unix permission mode for created files.

**Default**: `0644` (owner read/write, group/others read)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.FilePermissions = 0600 // Owner read/write only
```

### DirPermissions

The Unix permission mode for created directories.

**Default**: `0755` (owner read/write/execute, group/others read/execute)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.DirPermissions = 0700 // Owner only
```

### SyncWrites

When enabled, files are synced to disk (fsync) after each write operation. This provides durability at the cost of performance.

**Default**: `true`

```go
// High-performance mode (less durable)
cfg := file.DefaultConfig()
cfg.DataDir = "/data"
cfg.CodecType = dao.CodecJSON
cfg.SyncWrites = false
```

## Example Configurations

### Development Configuration

```go
cfg := file.DefaultConfig()
cfg.DataDir = "./data"
cfg.CodecType = dao.CodecJSON
cfg.SyncWrites = false // Faster for development
```

### Production Configuration (Config Files)

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/etc/myapp/config"
cfg.CodecType = dao.CodecYAML
cfg.FilePermissions = 0644
cfg.DirPermissions = 0755
cfg.SyncWrites = true
```

### Secure Configuration

```go
cfg := file.DefaultConfig()
cfg.DataDir = "/var/lib/myapp/secrets"
cfg.CodecType = dao.CodecJSON
cfg.FilePermissions = 0600 // Owner read/write only
cfg.DirPermissions = 0700  // Owner only
cfg.SyncWrites = true
```

## Codec-to-Extension Mapping

The `ExtensionForCodec` function maps codec types to file extensions:

```go
ext := file.ExtensionForCodec(dao.CodecJSON)     // ".json"
ext := file.ExtensionForCodec(dao.CodecYAML)     // ".yaml"
ext := file.ExtensionForCodec(dao.CodecTOML)     // ".toml"
ext := file.ExtensionForCodec(dao.CodecMsgPack)  // ".msgpack"
ext := file.ExtensionForCodec(dao.CodecBSON)     // ".bson"
ext := file.ExtensionForCodec(dao.CodecProtoBuf) // ".pb"
ext := file.ExtensionForCodec(dao.CodecCBOR)     // ".cbor"
```

Unknown codec types default to `.dat`.

## Validation

The configuration is validated when creating a new file store:

```go
store, err := file.New(cfg, log)
if err != nil {
    // Handle validation error - uses pkg/errors DragonError
    if errors.IsInvalidInput(err) {
        // Invalid configuration
        fmt.Printf("Configuration error: %v\n", err)
    }
}
```

Required fields:
- `DataDir` must not be empty
- `CodecType` must not be empty

## Performance Considerations

### SyncWrites Trade-offs

| SyncWrites | Durability | Performance |
|------------|------------|-------------|
| `true` | High | Lower (fsync per write) |
| `false` | Lower | Higher (OS buffers writes) |

For development and testing, disable SyncWrites for faster iteration. For production with important data, keep SyncWrites enabled.

### File System Considerations

- **SSD vs HDD**: SSDs handle random writes much better than HDDs
- **File count**: Very large numbers of files (millions) may slow down directory operations
- **File size**: Each entity is a separate file; very large entities may be slow to read/write

## See Also

- [File Storage Overview](README.md)
- [DAO Configuration](../../dao/configuration.md)
- [Codec Configuration](../../codec/configuration.md)
