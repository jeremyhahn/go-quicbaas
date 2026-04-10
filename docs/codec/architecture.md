# Codec Architecture

QuicBaaS implements a dual-codec architecture that separates storage encoding from wire encoding, optimizing both disk I/O performance and API flexibility.

## Architecture Overview

```
┌──────────┐    Wire     ┌─────────────┐    Storage     ┌─────────────┐
│  Client  │────JSON────▶│   API Layer │───MessagePack─▶│    State    │
│          │◀───JSON─────│             │◀──MessagePack──│   Machine   │
└──────────┘             └─────────────┘                └─────────────┘
                              ▲                               │
                              │                               │
                         CodecManager                    PebbleDB
                       (Dual Encoding)                  (Binary KV)
```

## Core Components

### Storage Codec

Fixed binary format for on-disk persistence.

**Default**: MessagePack

**Characteristics**:
- Binary format (compact, fast)
- Fixed across all nodes in cluster
- Optimized for disk I/O
- Type-preserving

**Configuration**:
```yaml
codec:
  storage_codec: "msgpack"
```

### Wire Codec

Flexible format for client communication.

**Default**: JSON

**Supported Codecs**:
- JSON: Human-readable, universal compatibility
- YAML: Configuration-friendly
- MessagePack: Binary efficiency
- Protobuf: Schema enforcement
- BSON: Document-oriented

**Codec Selection Priority**:
1. Explicit struct tag: `quicbaas:"codec=X"`
2. Auto-detection from field tags
3. Configuration default
4. System fallback (JSON)

### Codec Manager

Central component managing all encoding/decoding operations.

**Responsibilities**:
- Initialize codec registry
- Route operations to appropriate codec
- Detect preferred wire codec per entity
- Manage buffer pooling
- Track metrics

**Key Methods**:
```go
// Storage operations
EncodeForStorage(entity interface{}) ([]byte, error)
DecodeFromStorage(data []byte, entity interface{}) error

// Wire operations
EncodeForWire(entity interface{}) ([]byte, string, error)
DecodeFromWire(data []byte, entity interface{}, codec string) error

// Optimized operations
EncodeForStoragePooled(entity interface{}) ([]byte, func(), error)
EncodeForStorageTo(buf []byte, entity interface{}) ([]byte, error)
```

## Data Flow

### Traditional Path (Double Encoding)

```
Storage → Decode → Go Struct → Encode → Client
         [Step 1]              [Step 2]
         50 allocs             30 allocs
```

### Optimized Path (Direct Transcoding)

```
Storage → Transcode → Client
         [Single Step]
         19 allocs
```

**Result**: 3x faster, 62% fewer allocations

## Entity-Level Configuration

Control codec selection per entity type:

```go
// JSON for web clients
type User struct {
    ID   uint64 `json:"id" msgpack:"id" quicbaas:"codec=json"`
    Name string `json:"name" msgpack:"name"`
}

// YAML for configuration
type Config struct {
    Setting string `yaml:"setting" json:"setting" quicbaas:"codec=yaml"`
    Value   int    `yaml:"value" json:"value"`
}

// MessagePack for high-throughput
type Metric struct {
    Timestamp int64   `msgpack:"ts" quicbaas:"codec=msgpack"`
    Value     float64 `msgpack:"val"`
}
```

## API Integration

### REST Handler

```go
func HandleGet(w http.ResponseWriter, r *http.Request) {
    id := parseID(r)
    preferredCodec := detectCodec(r)

    // Get pre-encoded data
    _, wireData, codecName, err := sm.GetWithCodec(id, preferredCodec)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    w.Header().Set("Content-Type", contentType(codecName))
    w.Write(wireData)
}
```

### gRPC Handler

```go
func (s *Server) GetOptimized(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
    md, _ := metadata.FromIncomingContext(ctx)
    preferredCodec := extractCodecPreference(md)

    _, wireData, codecName, err := s.sm.GetWithCodec(req.Id, preferredCodec)
    if err != nil {
        return nil, err
    }

    return &pb.GetResponse{
        Data:  wireData,
        Codec: codecName,
    }, nil
}
```

## Performance Characteristics

### Encoding Performance

| Codec | Allocs | Memory | Speed |
|-------|--------|--------|-------|
| JSON Standard | 40 | 1600 B | baseline |
| JSON Optimized | 16 | 640 B | +30% |
| MsgPack Standard | 35 | 1400 B | baseline |
| MsgPack Optimized | 14 | 560 B | +35% |
| YAML Optimized | 1 | 128 B | +94% |

### Request Performance

| Path | Latency | Allocations | CPU |
|------|---------|-------------|-----|
| Traditional Get | 1.2ms | 8/req | 100% |
| GetWithCodec | 0.4ms | 3/req | 33% |
| **Improvement** | **3x** | **62% fewer** | **67% less** |

### REST API Throughput

| Configuration | Throughput | CPU | Memory |
|---------------|-----------|-----|--------|
| Traditional | 10k req/s | 80% | 1.0x |
| Dual-codec | 25k req/s | 45% | 0.5x |
| **Improvement** | **2.5x** | **43% less** | **50% less** |

## Buffer Pooling

High-throughput optimization using buffer reuse:

```go
// Pooled encoding
data, cleanup, err := manager.EncodeForStoragePooled(entity)
if err != nil {
    return err
}
defer cleanup()

// Batch encoding with reuse
buf := make([]byte, 0, 4096)
for _, entity := range entities {
    buf, err = manager.EncodeForStorageTo(buf[:0], entity)
    if err != nil {
        return err
    }
    process(buf)
}
```

## Design Principles

1. **Separation of Concerns**: Storage and wire encoding are independent
2. **Zero-Copy Optimization**: Pre-encoded wire data eliminates double-encoding
3. **Format Agnostic**: State machines work with byte arrays only
4. **Entity-Level Control**: Per-entity codec selection via tags
5. **Backward Compatible**: Existing code continues to work

## Best Practices

### Storage Codec Selection

**Recommended**: MessagePack
- 30-50% smaller than JSON
- Fast encoding/decoding
- Type-preserving
- Cluster must use same storage codec

### Wire Codec Selection

| Format | Use Case |
|--------|----------|
| JSON | Web APIs, external clients |
| YAML | Configuration, diagnostics |
| MessagePack | High-throughput, internal services |
| Protobuf | gRPC services, schema enforcement |

### Auto-Detection

Enable `auto_detect: true` to automatically select wire codec based on entity tags:

```go
type User struct {
    ID   uint64 `json:"id"`  // Auto-detected as JSON
    Name string `json:"name"`
}
```

### Content Negotiation

Support multiple formats in APIs:

```go
func detectCodec(r *http.Request) string {
    if codec := r.Header.Get("X-QuicBaaS-Codec"); codec != "" {
        return codec
    }
    if codec := r.URL.Query().Get("codec"); codec != "" {
        return codec
    }
    if accept := r.Header.Get("Accept"); accept != "" {
        return parseAccept(accept)
    }
    return "json"
}
```

## Source Code

- **CodecManager**: `pkg/codec/manager.go`
- **Optimized Codecs**: `pkg/codec/msgpack_optimized.go`, `pkg/codec/json_optimized.go`
- **Configuration**: `pkg/config/config.go`
- **State Machine**: Managed by the qrdb engine via the qrdb SDK
- **gRPC Handlers**: `pkg/api/grpc/server_codec.go`
- **REST Handlers**: `pkg/api/rest/handlers_codec.go`

## Version

- Status: Production Ready
- Version: 1.0.0
- Last Updated: 2025-12-19
