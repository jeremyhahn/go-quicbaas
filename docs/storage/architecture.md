# QuicBaaS Storage Architecture

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          QuicBaaS Node                               │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                      API Layer                                  │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │ │
│  │  │  gRPC    │  │   REST   │  │   QUIC   │  │   MCP    │       │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                     Service Layer                               │ │
│  │  (Business logic, validation, coordination)                     │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                  Raft State Machine                             │ │
│  │  (Consensus, replication, state transitions)                    │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                   Storage Layer                                 │ │
│  │                                                                  │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │ │
│  │  │   MVCC   │  │  Index   │  │ Encrypt  │  │  Watch   │       │ │
│  │  │  Layer   │  │ Manager  │  │  Layer   │  │  System  │       │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │ │
│  │       ↓              ↓              ↓              ↓            │ │
│  │  ┌──────────────────────────────────────────────────────┐      │ │
│  │  │            PebbleDB Adapter                           │      │ │
│  │  │  - Batching, Snapshots, Iterators                    │      │ │
│  │  └──────────────────────────────────────────────────────┘      │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                              ↓                                       │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                      PebbleDB                                   │ │
│  │                                                                  │ │
│  │  ┌─────────────────┐  ┌─────────────────┐                      │ │
│  │  │  MemTable (RAM) │  │  Block Cache    │                      │ │
│  │  └─────────────────┘  └─────────────────┘                      │ │
│  │           ↓                     ↑                                │ │
│  │  ┌─────────────────────────────────────────┐                   │ │
│  │  │           LSM-tree (Disk)                │                   │ │
│  │  │  L0: [SST] [SST] [SST]                  │                   │ │
│  │  │  L1: [SST] [SST]                        │                   │ │
│  │  │  L2: [SST] [SST] [SST]                  │                   │ │
│  │  │  L3-L6: [...more levels...]             │                   │ │
│  │  └─────────────────────────────────────────┘                   │ │
│  │           ↓                                                      │ │
│  │  ┌─────────────────┐                                           │ │
│  │  │  WAL (Disk)     │                                           │ │
│  │  └─────────────────┘                                           │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

## LSM-Tree Architecture

### Write Path

```
1. Client Write
       ↓
2. Write to WAL (durability)
       ↓
3. Write to MemTable (RAM)
       ↓
4. Return to client
       ↓
5. [Background] MemTable full → Freeze
       ↓
6. [Background] Flush to L0 SST (disk)
       ↓
7. [Background] Compact L0 → L1 (sorted, merged)
       ↓
8. [Background] Compact L1 → L2 → ... → L6
```

**Flow Diagram**:

```
Write(key, value)
    |
    ├─> WAL.Append(entry) ──────────────┐
    |                                    |
    └─> MemTable.Set(key, value)        |
              |                          |
              | (when full)              |
              ↓                          |
        Freeze MemTable ───────────────> Disk Flush
              |                          |
        New MemTable                     ↓
              |                     L0 SST File
              |                          |
              |                          | (background)
              |                          ↓
              |                     Compaction
              |                     L0 → L1 → L2...
              |
              ↓
        Return Success
```

### Read Path

```
1. Check MemTable (active)
       ↓ (miss)
2. Check Immutable MemTable (frozen)
       ↓ (miss)
3. Check L0 SSTs (bloom filter → block cache → disk)
       ↓ (miss)
4. Check L1 SST (binary search → bloom filter → block cache → disk)
       ↓ (miss)
5. Check L2-L6 SSTs (increasing size)
       ↓ (miss)
6. Return NotFound
```

**Flow Diagram**:

```
Get(key)
    |
    ├─> MemTable.Get(key) ──────────> Found? → Return
    |        |
    |        | (miss)
    |        ↓
    ├─> ImmutableMemTable.Get(key) ──> Found? → Return
    |        |
    |        | (miss)
    |        ↓
    └─> For each L0 SST:
            |
            ├─> BloomFilter.MayContain(key)
            |       |
            |       | (probably yes)
            |       ↓
            ├─> BlockCache.Get(key) ──> Found? → Return
            |       |
            |       | (miss)
            |       ↓
            └─> DiskRead(block) ──────> Found? → Return
                    |
                    | (miss in all L0)
                    ↓
            For each L1-L6 level:
                    ↓
            BinarySearch(sstable) ──────> Found? → Return
                    |
                    | (miss in all levels)
                    ↓
            Return NotFound
```

### Compaction

```
Background Process:

L0 Files: [f1] [f2] [f3] [f4]  (unsorted, overlapping)
              ↓
    Compact when count > L0CompactionThreshold
              ↓
L1 Files: [f5────────] [f6────────]  (sorted, non-overlapping)
              ↓
    Compact when size > MaxBytesForLevelBase
              ↓
L2 Files: [f7──────────────] [f8──────────────]
              ↓
    Continue down to L6...
```

**Compaction Strategies**:

1. **L0 → L1 Compaction** (most expensive)
   - Merge all overlapping L0 files
   - Sort and deduplicate keys
   - Apply tombstones (deletes)
   - Create non-overlapping L1 files

2. **Ln → Ln+1 Compaction** (cheaper)
   - Select files that overlap
   - Merge and compact
   - Each level 10x larger than previous

3. **Tombstone Cleanup**
   - Remove deleted keys during compaction
   - Configurable retention period
   - Garbage collection of old versions

## Key Encoding

### Encoding Principles

1. **Hierarchical prefixes** group related data
2. **Null byte separators** enable prefix scans
3. **Big-endian integers** ensure proper sorting
4. **Common prefixes** optimize compression

### Entity Key Format

```
{prefix}\x00{namespace}\x00{entity_type}\x00{id}\x00{version}

Example:
kv\x00default\x00user\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x05
└─┬──┘└───┬───┘└─┬──┘└──────────┬─────────┘└──────────┬─────────┘
  |       |      |               |                     |
Prefix  NS  EntityType      ID (uint64)         Version (uint64)
```

### Index Key Format

```
idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}\x00{version}

Example (email index):
idx\x00user\x00email\x00alice@example.com\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x05
```

### Metadata Key Format

```
meta\x00{entity_type}\x00{id}\x00{field}

Example:
meta\x00user\x00\x00\x00\x00\x00\x00\x00\x01\x00created_at
```

### Sequence Key Format

```
seq\x00{entity_type}

Example:
seq\x00user → uint64 (next available ID)
```

## Storage Layers

### MVCC Layer

Multi-Version Concurrency Control provides:

**Version Management**:
```go
// Each write creates new version
key := EncodeKey("user", 1)
version := GetNextVersion()  // Monotonically increasing

storage.Set(key + version, value)
```

**Snapshot Isolation**:
```go
// Read at specific version
snapshot := storage.NewSnapshot()
defer snapshot.Close()

value := snapshot.Get(key)  // Consistent point-in-time view
```

**Garbage Collection**:
```go
// During compaction, remove old versions
if key.version < snapshot.minVersion {
    // Safe to delete
    compaction.Remove(key)
}
```

### Index Manager

**Primary Index** (entity by ID):
```
{entity_type}\x00{id} → full entity data
```

**Secondary Indexes** (query by field):
```
idx\x00{entity_type}\x00{field}\x00{value}\x00{id} → empty
```

**Index Maintenance**:
```go
func IndexEntity(entity Entity) error {
    batch := storage.NewBatch()

    // Store entity
    key := EncodePrimaryKey(entity.Type, entity.ID)
    batch.Set(key, entity.Data)

    // Create indexes
    for field, value := range entity.IndexedFields {
        idxKey := EncodeIndexKey(entity.Type, field, value, entity.ID)
        batch.Set(idxKey, nil)  // Existence index
    }

    return batch.Commit()
}
```

**Index Query**:
```go
func QueryByIndex(entityType, field, value string) []Entity {
    prefix := EncodeIndexPrefix(entityType, field, value)

    iter := storage.NewIterator(&IteratorOptions{
        Prefix: prefix,
    })
    defer iter.Close()

    var results []Entity
    for iter.Next() {
        id := ExtractIDFromIndexKey(iter.Key())
        entity := GetEntity(entityType, id)
        results = append(results, entity)
    }

    return results
}
```

### Encryption Layer

**Encryption at Rest**:
```
Plaintext → AES-256-GCM → Ciphertext + Tag
                ↓
         Store in PebbleDB
```

**Key Hierarchy**:
```
Master Key (KMS/HSM)
    ↓
Cluster DEK (Data Encryption Key)
    ↓
Per-Entity Encryption (optional)
```

**Encrypted Key Format**:
```
{prefix}_enc\x00{namespace}\x00{id}\x00{version}
  → {encrypted_value, nonce, tag, metadata}
```

### Watch System

**Event Generation**:
```go
// On every write
func Set(key, value []byte) error {
    err := storage.Set(key, value)
    if err != nil {
        return err
    }

    // Notify watchers
    watchEvent := &WatchEvent{
        Type:  EventPut,
        Key:   key,
        Value: value,
    }

    watchSystem.Notify(watchEvent)
    return nil
}
```

**Watch Stream**:
```
Client                        Server
  |                             |
  |─── Watch(prefix) ──────────>|
  |                             |
  |<─── WatchID ────────────────|
  |                             |
  |                    [Write happens]
  |                             |
  |<─── Event(PUT, key, value)─|
  |<─── Event(DELETE, key) ────|
  |                             |
```

## Data Flow Examples

### Write Operation (Synchronous)

```
1. Client: Put(key="user:1", value="{name: 'Alice'}")
           ↓
2. Service Layer: Validate + Authorize
           ↓
3. Raft Proposal: Propose(entry)
           ↓
4. Raft Consensus: Replicate to majority
           ↓
5. State Machine Apply:
           ├─> MVCC: Allocate version
           ├─> Indexes: Update secondary indexes
           ├─> Encryption: Encrypt value (if enabled)
           └─> Storage: Write to PebbleDB
                  ├─> WAL: Append entry
                  ├─> MemTable: Set(key, value)
                  └─> Return success
           ↓
6. Raft Commit: Mark entry as committed
           ↓
7. Return to client: Success
           ↓
8. [Async] Watch System: Notify watchers
```

### Range Scan Operation

```
1. Client: Scan(prefix="user:")
           ↓
2. Service Layer: Authorize prefix access
           ↓
3. Storage: NewIterator(prefix)
           ↓
4. PebbleDB:
           ├─> Check MemTable (active)
           ├─> Check Immutable MemTable
           ├─> Create merged iterator (all levels)
           └─> Return iterator
           ↓
5. Iterate:
    For each key:
           ├─> Decrypt (if encrypted)
           ├─> Check MVCC version
           ├─> Filter by visibility
           └─> Return to client
           ↓
6. Client receives stream of key-value pairs
```

### Background Compaction

```
Trigger: L0 file count > L0CompactionThreshold
         ↓
1. Select files to compact (all overlapping L0 + L1 files)
         ↓
2. Create merge iterator
         ↓
3. For each unique key:
    ├─> Apply tombstones (delete markers)
    ├─> Keep latest version (discard old)
    ├─> Compress value
    └─> Write to new L1 SST file
         ↓
4. Atomically replace old files with new
         ↓
5. Delete old SST files
         ↓
6. Update manifest
         ↓
7. Release disk space
```

## Performance Characteristics

### Latency Analysis

**Write Latency Breakdown**:
```
Total: ~2ms (sync mode)

Components:
  Validation:        0.01ms  (0.5%)
  Raft proposal:     0.10ms  (5%)
  Network RTT:       0.50ms  (25%)
  WAL append:        0.05ms  (2.5%)
  MemTable write:    0.01ms  (0.5%)
  fsync:             1.30ms  (65%)
  Callback:          0.03ms  (1.5%)
```

**Read Latency Breakdown**:
```
Hot path (cache hit): ~0.5ms

Components:
  Lookup:            0.01ms  (2%)
  MemTable check:    0.10ms  (20%)
  Cache lookup:      0.30ms  (60%)
  Deserialize:       0.05ms  (10%)
  Authorization:     0.04ms  (8%)

Cold path (disk read): ~3ms

Components:
  Lookup:            0.01ms  (0.3%)
  MemTable check:    0.10ms  (3.3%)
  Bloom filter:      0.05ms  (1.7%)
  Cache miss:        0.10ms  (3.3%)
  Disk read:         2.50ms  (83.3%)
  Deserialize:       0.05ms  (1.7%)
  Authorization:     0.04ms  (1.3%)
  Cache update:      0.15ms  (5%)
```

### Throughput Analysis

**Single-threaded Throughput**:
```
Sync writes:   500-1K ops/sec   (limited by fsync)
Async writes:  10-20K ops/sec   (limited by CPU)
Batch writes:  50-100K keys/sec (amortized)
Point reads:   2-5K ops/sec     (limited by disk)
Cached reads:  50-100K ops/sec  (limited by CPU)
```

**Multi-threaded Throughput** (32 cores):
```
Sync writes:   10-15K ops/sec   (parallel fsyncs)
Async writes:  200-500K ops/sec (parallel processing)
Point reads:   50-100K ops/sec  (cache hits)
Range scans:   1-5K scans/sec   (I/O bound)
```

### Resource Usage

**Memory**:
```
Fixed overhead:    50-100 MB  (runtime, connections)
Block cache:       configurable (128 MB - 64 GB)
MemTables:         2-4 × MemTableSize (128-512 MB)
Bloom filters:     10 bits/key (~1.25 MB per 1M keys)
Index blocks:      ~1% of data size
Total (typical):   500 MB - 10 GB
```

**Disk I/O**:
```
Write amplification:  10-30x (LSM-tree characteristic)
Space amplification:  1.5-2.5x (during compaction)
Read amplification:   5-20x (levels checked)

Mitigation:
  - Larger MemTables reduce write amp
  - Better compression reduces space amp
  - Bloom filters reduce read amp
```

**CPU**:
```
Foreground (requests): 20-40% (normal load)
Background (compaction): 30-50% (write-heavy)
Idle: 1-5%

CPU breakdown:
  Encoding/decoding:    25%
  Compression:          20%
  Compaction:           30%
  Network I/O:          15%
  Business logic:       10%
```

## Configuration Recommendations

### Development

```go
config := &pebble.Config{
    DataDir:                  "./data",
    CacheSize:                128 << 20,  // 128 MB
    MemTableSize:             64 << 20,   // 64 MB
    MaxOpenFiles:             500,
    MaxConcurrentCompactions: 1,
    SyncWrites:               true,
    Compression:              CompressionSnappy,
}
```

### Production (Read-Heavy)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                20 << 30,   // 20 GB
    MemTableSize:             128 << 20,  // 128 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 2,
    SyncWrites:               true,
    Compression:              CompressionZstd,
    L0CompactionThreshold:    4,
    L0StopWritesThreshold:    12,
}
```

### Production (Write-Heavy)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                10 << 30,   // 10 GB
    MemTableSize:             256 << 20,  // 256 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 4,
    SyncWrites:               false,
    WALMinSyncInterval:       100 * time.Millisecond,
    Compression:              CompressionSnappy,
    L0CompactionThreshold:    8,
    L0StopWritesThreshold:    20,
}
```

### Production (Low-Latency)

```go
config := &pebble.Config{
    DataDir:                  "/var/lib/quicbaas/data",
    CacheSize:                15 << 30,   // 15 GB (large cache)
    MemTableSize:             128 << 20,  // 128 MB
    MaxOpenFiles:             10000,
    MaxConcurrentCompactions: 2,          // Limit CPU usage
    SyncWrites:               false,      // Async for speed
    WALMinSyncInterval:       50 * time.Millisecond,
    Compression:              CompressionSnappy,  // Fast compression
    L0CompactionThreshold:    6,
}
```

## Monitoring and Metrics

### Key Performance Indicators

```
Operational Health:
  ├─ Cache hit rate > 80%
  ├─ Write latency p99 < 10ms
  ├─ Read latency p99 < 5ms
  ├─ L0 file count < 12
  └─ Compaction lag < 100 MB

Resource Utilization:
  ├─ Memory usage < 80% of cache
  ├─ Disk usage < 80% of capacity
  ├─ CPU usage < 70% avg
  └─ Disk I/O < 80% saturation

Error Rates:
  ├─ Storage errors < 0.01%
  ├─ Timeout errors < 0.1%
  └─ Compaction failures = 0
```

### Alerting Thresholds

```yaml
Critical:
  - disk_usage > 90%
  - l0_files > 15
  - compaction_failures > 0
  - write_latency_p99 > 50ms
  - cache_hit_rate < 50%

Warning:
  - disk_usage > 75%
  - l0_files > 10
  - write_latency_p99 > 20ms
  - cache_hit_rate < 70%
  - memory_usage > 80%
```

## References

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Design](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)
- [CockroachDB Storage](https://www.cockroachlabs.com/docs/stable/architecture/storage-layer.html)
