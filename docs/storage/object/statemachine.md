# ObjectStore State Machine

A hybrid Raft state machine that automatically offloads large values to object storage while keeping metadata in PebbleDB.

## Overview

The ObjectStore state machine optimizes storage by:

- **Small values** (< threshold): Stored inline in PebbleDB for fast access
- **Large values** (>= threshold): Offloaded to object storage (S3/GCS/Azure)
- **Metadata**: Always in PebbleDB, replicated via Raft consensus

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                ObjectStoreStateMachine                          │
│            (implements IOnDiskStateMachine)                     │
└─────────────────────────────────────────────────────────────────┘
                 │                           │
      value < threshold              value >= threshold
                 │                           │
                 ▼                           ▼
    ┌─────────────────────┐      ┌─────────────────────┐
    │   PebbleDB          │      │  Object Storage     │
    │   (Inline Data)     │      │  (S3/GCS/Azure)     │
    │   + Metadata        │      │                     │
    └─────────────────────┘      └─────────────────────┘
```

## Multi-Cloud Replication

Each node writes large values to its own object storage backend. Example 3-node cluster:

```
   Node A               Node B               Node C
   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │ PebbleDB     │    │ PebbleDB     │    │ PebbleDB     │
   │ (metadata)   │    │ (metadata)   │    │ (metadata)   │
   └──────────────┘    └──────────────┘    └──────────────┘
          │                   │                   │
          ▼                   ▼                   ▼
   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │     S3       │    │     GCS      │    │    Azure     │
   │   (AWS)      │    │   (GCP)      │    │   (Azure)    │
   └──────────────┘    └──────────────┘    └──────────────┘
```

**Benefits:**
- Multi-cloud redundancy (no single cloud dependency)
- Each node reads from its own store (low latency)
- Independent failure domains

## Configuration

```yaml
raft:
  statemachines:
    - shard_id: 1
      type: objstore
      config:
        value_threshold: 1048576  # 1 MB
        metadata_path: /var/lib/quicbaas/raft/shard-1
        async_writes: true
        max_pending_writes: 100
        compression: zstd
        objstore:
          type: s3
          config:
            bucket: my-quicbaas-bucket
            region: us-east-1
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `value_threshold` | int64 | 1048576 | Size in bytes to trigger offload |
| `metadata_path` | string | required | PebbleDB path for metadata |
| `async_writes` | bool | true | Async object storage writes |
| `max_pending_writes` | int | 100 | Max concurrent async operations |
| `compression` | string | "none" | Compression: none, zstd, gzip |

### Object Storage Configuration

See [backends.md](backends.md) for backend-specific options.

## Value Metadata

Stored in PebbleDB for every key:

```go
type ValueMetadata struct {
    Key             []byte            // Original key
    Size            int64             // Original value size
    IsOffloaded     bool              // True if in object storage
    ObjectRef       string            // Object storage key (local)
    Checksum        []byte            // SHA-256 for integrity
    Data            []byte            // Inline data (if not offloaded)
    Version         uint64            // MVCC version
    CreatedAt       int64
    UpdatedAt       int64
    FetchStatus     ObjectFetchStatus // Available, Pending, or Fetching
    SourceObjectRef string            // Source peer's object ref (for fetching)
}

type ObjectFetchStatus uint8

const (
    ObjectStatusAvailable ObjectFetchStatus = 0 // Locally available
    ObjectStatusPending   ObjectFetchStatus = 1 // Needs fetch from peer
    ObjectStatusFetching  ObjectFetchStatus = 2 // Fetch in progress
)
```

**FetchStatus Transitions:**
- `Available` → Normal state, object is locally accessible
- `Pending` → After snapshot recovery, object needs fetching from peer
- `Fetching` → Fetch worker is actively retrieving the object

## Operations

### Update Flow

```
1. Decode command (binary protocol)
2. If PUT with value >= threshold:
   - Compute SHA-256 checksum
   - Compress if configured
   - Upload to object storage (async or sync)
   - Store metadata with ObjectRef in PebbleDB
3. If PUT with value < threshold:
   - Store metadata with inline Data in PebbleDB
4. If DELETE:
   - Delete from PebbleDB
   - Delete from object storage if offloaded
```

### Lookup Flow

```
1. Load metadata from PebbleDB
2. If inline data, return directly
3. If offloaded and FetchStatus is Pending or Fetching:
   - Schedule high-priority fetch from peers (if Pending)
   - Return ObjectPendingError (client should retry)
4. If offloaded and FetchStatus is Available:
   - Fetch from local object storage
   - Decompress if needed
   - Verify checksum
   - Return value
```

**Pending Object Behavior:** When a read encounters a pending object, the state machine returns `ObjectPendingError` and triggers an async fetch. The client should retry after a brief delay.

## Snapshots

Snapshots contain **metadata only** - large object data is excluded for optimal performance. Recovering nodes fetch objects lazily from peers using the async ObjectFetcher.

**Format:**
```
Header: [Version:1B][Revision:8B][Timestamp:8B][NumKeys:8B]
For each key:
  [MetaLen:4B][ValueMetadata (msgpack)]
```

**Recovery:**
1. Restore metadata to PebbleDB
2. For each offloaded value:
   - Mark `FetchStatus = Pending`
   - Store `SourceObjectRef` (original peer's object ref)
   - Clear local `ObjectRef` (no local copy yet)
3. Schedule bulk fetch for all pending objects

**Benefits:**
- Dramatically smaller snapshot sizes
- Faster snapshot transfer between nodes
- Non-blocking recovery (reads can proceed, pending objects return error)
- Parallel object fetching from multiple peers

## Lazy Object Fetching

The `ObjectFetcher` asynchronously retrieves pending objects from peers without blocking Raft or state machine operations.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     ObjectFetcher                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ High Priority│  │Normal Priority│  │  Fetch Workers  │   │
│  │    Queue     │  │    Queue      │  │  (N concurrent) │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               ObjectTransferService (gRPC)                  │
│                                                             │
│   Peer A ◄────────► Peer B ◄────────► Peer C                │
└─────────────────────────────────────────────────────────────┘
```

### Priority Queues

- **High Priority**: On-demand fetches triggered by read operations
- **Normal Priority**: Background fetches scheduled after recovery

### Fetch Flow

```
1. Worker selects request (high priority first)
2. Get peer list from discovery
3. For each peer until success:
   - Connect via gRPC
   - Call FetchObject(shardID, objectRef, checksum)
   - Verify checksum if provided
4. Write to local object store
5. Update metadata: FetchStatus=Available, ObjectRef=localRef
6. Clear SourceObjectRef
```

### Configuration

```yaml
raft:
  statemachines:
    - shard_id: 1
      type: objstore
      config:
        fetcher:
          max_concurrent: 4      # Parallel fetch workers
          max_retries: 5         # Retries per object
          retry_base_delay: 1s   # Exponential backoff base
          fetch_timeout: 30s     # Per-fetch timeout
          queue_size: 10000      # Max pending requests
```

### Deduplication

The fetcher deduplicates requests using an `inFlight` map. Multiple reads for the same pending object result in a single fetch operation.

### Retry Logic

Failed fetches retry with exponential backoff:
- Attempt 1: immediate
- Attempt 2: 1s delay
- Attempt 3: 2s delay
- Attempt 4: 4s delay
- Attempt 5: 8s delay (max)

After max retries, the object is marked permanently failed.

## Object Transfer Service

gRPC service for peer-to-peer object transfer:

```protobuf
service ObjectTransferService {
    rpc FetchObject(FetchObjectRequest) returns (FetchObjectResponse);
}

message FetchObjectRequest {
    uint64 shard_id = 1;
    string object_ref = 2;
    bytes checksum = 3;
}

message FetchObjectResponse {
    bytes data = 1;
    bytes checksum = 2;
    bool found = 3;
    string error_message = 4;
}
```

## Threshold Strategies

### Small Threshold (100 KB)

```yaml
value_threshold: 102400
```

- Most values offloaded
- Smaller PebbleDB
- More object storage operations
- Best for: Media-heavy workloads

### Default (1 MB)

```yaml
value_threshold: 1048576
```

- Balanced approach
- Common KV data stays local
- Large blobs offloaded
- Best for: Mixed workloads

### Large Threshold (10 MB)

```yaml
value_threshold: 10485760
```

- Only very large values offloaded
- Larger PebbleDB
- Fewer object storage operations
- Best for: Predominantly small values

## Compression

| Algorithm | Speed | Ratio | Use Case |
|-----------|-------|-------|----------|
| `none` | Fastest | 1:1 | Pre-compressed data (images, videos) |
| `zstd` | Fast | Best | General purpose (recommended) |
| `gzip` | Medium | Good | Maximum compatibility |

## Async vs Sync Writes

### Async Writes (Recommended)

```yaml
async_writes: true
```

- Better write latency
- Raft log provides durability
- Object storage writes happen after Raft commit
- May have brief window where value is in Raft log but not object storage

### Sync Writes

```yaml
async_writes: false
```

- Stricter consistency
- Higher write latency
- Value confirmed in object storage before Raft commit returns

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `objstore_put_ops_total` | Counter | Total PUT operations |
| `objstore_get_ops_total` | Counter | Total GET operations |
| `objstore_offloaded_writes_total` | Counter | Values written to object storage |
| `objstore_inline_writes_total` | Counter | Values stored inline |
| `objstore_offloaded_bytes_total` | Counter | Bytes written to object storage |
| `objstore_put_latency_seconds` | Histogram | PUT operation duration |
| `objstore_offload_latency_seconds` | Histogram | Object storage upload duration |
| `objstore_pending_writes` | Gauge | Current pending async writes |
| `objstore_fetcher_fetched_total` | Counter | Objects fetched from peers |
| `objstore_fetcher_failed_total` | Counter | Failed fetch attempts |
| `objstore_fetcher_pending` | Gauge | Objects pending fetch |

## Error Handling

| Error | Description | Recovery |
|-------|-------------|----------|
| `ErrObjectNotFound` | Object missing from storage | Re-upload from snapshot |
| `ErrChecksumMismatch` | Data integrity failure | Fetch from another node |
| `ErrAsyncWriteFailed` | Background write failed | Retry with backoff |
| `ErrObjectPending` | Object being fetched from peer | Retry read after delay |
| `ErrFetchFailed` | Peer fetch failed | Auto-retry with backoff |
