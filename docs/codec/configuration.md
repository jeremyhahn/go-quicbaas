# Codec Configuration

Configuration guide for QuicBaaS's dual-codec system covering setup, tuning, and optimization strategies.

## Basic Configuration

### Default Configuration

```yaml
codec:
  storage_codec: "msgpack"
  default_wire_codec: "json"
  auto_detect: true
  dual_codec_mode: true
  use_optimized_codecs: true
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| storage_codec | string | msgpack | Format for disk storage |
| default_wire_codec | string | json | Default API response format |
| auto_detect | bool | true | Auto-detect codec from struct tags |
| dual_codec_mode | bool | true | Enable dual-codec optimization |
| use_optimized_codecs | bool | true | Enable buffer pooling |

## Environment Profiles

### Development Profile

Human-readable formats for debugging:

```yaml
codec:
  storage_codec: "json"
  default_wire_codec: "json"
  auto_detect: false
  dual_codec_mode: false
```

**Use When**:
- Local development
- Debugging storage issues
- Manual data inspection

### Production Profile

Optimized for performance:

```yaml
codec:
  storage_codec: "msgpack"
  default_wire_codec: "json"
  auto_detect: true
  dual_codec_mode: true
  use_optimized_codecs: true
```

**Use When**:
- Production deployments
- Web APIs
- External client access

### High-Throughput Profile

Minimize overhead:

```yaml
codec:
  storage_codec: "msgpack"
  default_wire_codec: "msgpack"
  auto_detect: true
  dual_codec_mode: true
  use_optimized_codecs: true
```

**Use When**:
- Metrics collection
- Time-series data
- Service-to-service communication
- No transcoding needed

### Microservices Profile

Schema enforcement:

```yaml
codec:
  storage_codec: "protobuf"
  default_wire_codec: "json"
  auto_detect: true
  dual_codec_mode: true
  use_optimized_codecs: true
```

**Use When**:
- gRPC services
- Cross-language systems
- Schema versioning required
- Type safety critical

## Environment Variable Overrides

```bash
# Override storage codec
export QUICBAAS_CODEC_STORAGE_CODEC=msgpack

# Override wire codec default
export QUICBAAS_CODEC_DEFAULT_WIRE_CODEC=json

# Enable/disable features
export QUICBAAS_CODEC_DUAL_CODEC_MODE=true
export QUICBAAS_CODEC_AUTO_DETECT=true
export QUICBAAS_CODEC_USE_OPTIMIZED_CODECS=true
```

## Programmatic Configuration

### Go Configuration

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/codec"

func setupCodecManager() (*codec.CodecManager, error) {
    cfg := &codec.Config{
        StorageCodec:       "msgpack",
        DefaultWireCodec:   "json",
        AutoDetect:         true,
        DualCodecMode:      true,
        UseOptimizedCodecs: true,
    }

    return codec.NewCodecManager(cfg)
}
```

### Using Default Config

```go
// Use sensible defaults
manager, err := codec.NewCodecManager(codec.DefaultConfig())
```

## Entity-Level Configuration

### Explicit Codec Tags

```go
// JSON for web API
type User struct {
    ID   uint64 `json:"id" msgpack:"id" quicbaas:"codec=json"`
    Name string `json:"name" msgpack:"name"`
}

// YAML for configuration
type Config struct {
    Setting string `yaml:"setting" json:"setting" quicbaas:"codec=yaml"`
    Value   int    `yaml:"value" json:"value"`
}

// MessagePack for metrics
type Metric struct {
    Timestamp int64   `msgpack:"ts" quicbaas:"codec=msgpack"`
    Value     float64 `msgpack:"val"`
}
```

### Auto-Detection

When `auto_detect: true`, codecs are automatically selected:

```go
// Auto-detected as JSON (has json tags)
type User struct {
    ID   uint64 `json:"id"`
    Name string `json:"name"`
}

// Auto-detected as YAML (has yaml tags)
type Config struct {
    Setting string `yaml:"setting"`
    Value   int    `yaml:"value"`
}
```

**Detection Priority**:
1. Protobuf tags
2. JSON tags
3. MessagePack tags
4. YAML tags
5. Configuration default

## Tuning Guide

### Storage Codec Selection

**MessagePack** (Recommended):
- 30-50% smaller than JSON
- Fast encoding/decoding
- Type-preserving
- Good compression

**Protobuf**:
- Schema enforcement
- Versioning support
- Cross-language
- Requires .proto files

**JSON** (Development only):
- Human-readable
- Larger storage size
- Slower encoding

**Avoid YAML for storage**: 25% larger, slower

### Wire Codec Selection

Match wire codec to client requirements:

| Client Type | Recommended Codec | Reason |
|-------------|------------------|--------|
| Web browsers | JSON | Universal support |
| Mobile apps | JSON | Compatibility |
| Internal services | MessagePack | Performance |
| gRPC services | Protobuf | Type safety |
| Configuration tools | YAML | Readability |

### Buffer Pooling

Enable `use_optimized_codecs: true` for:

**Benefits**:
- 18-27% fewer allocations
- 12-28% faster encoding
- Lower GC pressure
- Better throughput

**Methods**:
```go
// Pooled encoding (single operation)
data, cleanup, err := manager.EncodeForStoragePooled(entity)
if err != nil {
    return err
}
defer cleanup()

// Buffer reuse (batch operations)
buf := make([]byte, 0, 4096)
for _, entity := range entities {
    buf, err = manager.EncodeForStorageTo(buf[:0], entity)
    if err != nil {
        return err
    }
    process(buf)
}
```

## Performance Tuning

### Minimize Transcoding

Use same codec for storage and wire when possible:

```yaml
# No transcoding - maximum performance
codec:
  storage_codec: "msgpack"
  default_wire_codec: "msgpack"
```

### Enable Auto-Detection

Automatic per-entity optimization:

```yaml
codec:
  auto_detect: true
```

### Use Optimized Codecs

Always enabled in production:

```yaml
codec:
  use_optimized_codecs: true
```

## Migration Guide

### From Single-Codec to Dual-Codec

**Step 1**: Update configuration

```yaml
codec:
  storage_codec: "msgpack"
  default_wire_codec: "json"
  dual_codec_mode: true
  auto_detect: true
```

**Step 2**: Add codec tags

```go
type User struct {
    ID   uint64 `json:"id" msgpack:"id" quicbaas:"codec=json"`
    Name string `json:"name" msgpack:"name"`
}
```

**Step 3**: Update API handlers

```go
// Before
entity, err := sm.Get(id)
jsonData, _ := json.Marshal(entity)
w.Write(jsonData)

// After
_, wireData, codec, err := sm.GetWithCodec(id, "json")
w.Header().Set("Content-Type", contentType(codec))
w.Write(wireData)
```

**Step 4**: Update clients

```bash
# REST clients
curl -H "Accept: application/json" http://localhost:8080/api/v1/user/123

# gRPC clients
md := metadata.New(map[string]string{"x-quicbaas-codec": "json"})
ctx := metadata.NewOutgoingContext(ctx, md)
```

### Backward Compatibility

Dual-codec is 100% backward compatible:
- Existing `Get()` methods continue to work
- No breaking API changes
- Gradual migration supported
- Existing storage readable

## Troubleshooting

### Wrong Codec Being Used

**Symptoms**: Client receives unexpected format

**Check**:
1. Struct tag: `quicbaas:"codec=X"`
2. Auto-detection enabled: `auto_detect: true`
3. Client headers: `Accept: application/json`
4. Selection priority

**Fix**:
```go
// Explicit tag
type Entity struct {
    Data string `json:"data" quicbaas:"codec=json"`
}

// Verify client request
curl -H "Accept: application/json" http://localhost:8080/api/v1/entity/123
```

### High Allocation Rates

**Symptoms**: Memory usage higher than expected

**Check**:
1. Optimized codecs enabled: `use_optimized_codecs: true`
2. Buffer cleanup called: `defer cleanup()`
3. No buffer leaks

**Fix**:
```go
// Use pooled methods
data, cleanup, err := manager.EncodeForStoragePooled(entity)
if err != nil {
    return err
}
defer cleanup()  // Critical
```

### Performance Not Improving

**Symptoms**: Latency same as before

**Check**:
1. Dual-codec enabled: `dual_codec_mode: true`
2. Using `GetWithCodec()` not `Get()`
3. Pre-encoded data used directly

**Fix**:
```go
// Use optimized path
_, wireData, codec, err := sm.GetWithCodec(id, preferred)
w.Header().Set("Content-Type", contentType(codec))
w.Write(wireData)  // Pre-encoded
```

### Codec Not Found

**Symptoms**: Error "codec not registered: X"

**Check**:
1. Supported codecs: json, yaml, msgpack, protobuf, bson
2. Configuration references valid codec
3. Registry initialized

**Fix**:
```yaml
codec:
  storage_codec: "msgpack"      # Valid
  default_wire_codec: "json"    # Valid
```

## Best Practices

### Production Checklist

- Use MessagePack for storage
- Enable optimized codecs
- Use `GetWithCodec()` in API handlers
- Implement content negotiation
- Monitor codec metrics
- Always call `cleanup()` with `defer`
- Match wire codec to client needs

### Configuration Review

```yaml
# Production-ready configuration
codec:
  storage_codec: "msgpack"       # Efficient storage
  default_wire_codec: "json"     # Web-friendly
  auto_detect: true              # Per-entity optimization
  dual_codec_mode: true          # Full optimization
  use_optimized_codecs: true     # Buffer pooling
```

### Monitoring

Track codec usage in production:

```go
metrics := codecManager.GetMetrics()
for codec, stats := range metrics.CodecUsage {
    log.Printf("Codec %s: %d ops, %d bytes",
        codec, stats.Count, stats.Bytes)
}
```

## Version

- Status: Production Ready
- Version: 1.0.0
- Last Updated: 2025-12-19
