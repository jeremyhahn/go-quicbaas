# QuicBaaS Storage Layer

High-performance embedded storage built on PebbleDB with LSM-tree architecture.

## Overview

QuicBaaS uses [PebbleDB](https://github.com/cockroachdb/pebble) as its embedded storage engine, providing:

- Pure Go implementation (no CGo dependencies)
- LSM-tree architecture optimized for write-heavy workloads
- Battle-tested in CockroachDB production environments
- Low-latency point lookups with bloom filters
- Efficient range scans with sorted keys
- ACID guarantees via write-ahead logging

## Key Features

| Feature | Description |
|---------|-------------|
| **LSM-tree Architecture** | Write-optimized storage with background compaction |
| **Embedded Storage** | No external dependencies, single binary deployment |
| **MVCC Support** | Multi-version concurrency control for consistent reads |
| **Write Modes** | Configurable durability vs performance trade-offs |
| **Compression** | Snappy and Zstd compression to reduce disk usage |
| **Bloom Filters** | Fast negative lookups reduce disk I/O |
| **Block Cache** | In-memory cache for frequently accessed data |

## Quick Reference

### Performance Characteristics

```
Operation              Latency (p50)    Throughput
Point Lookup (hot)     < 1ms            50K ops/sec
Point Lookup (cold)    2-5ms            10K ops/sec
Sequential Write       0.5ms            20K ops/sec
Batch Write (1K)       50ms             20K entries/sec
Range Scan (1K keys)   10-50ms          -
```

### Storage Overhead

```
Per Key-Value Pair:
  Key size:            variable (typically 20-100 bytes)
  Value size:          variable
  Index overhead:      ~10% (bloom filters, indexes)
  Space amplification: 1.5-2.5x (during compaction)
  Write amplification: 10-30x (typical LSM-tree)
```

### Default Configuration

```go
config := &pebble.Config{
    DataDir:                  "./data",
    CacheSize:                128 << 20,  // 128 MB
    MaxOpenFiles:             1000,
    MaxConcurrentCompactions: 1,
    MemTableSize:             64 << 20,   // 64 MB
    SyncWrites:               true,
    Compression:              CompressionSnappy,
}
```

## Key Schema

QuicBaaS uses a hierarchical key encoding scheme with prefix-based isolation:

### General Format

```
{prefix}\x00{namespace}\x00{identifier}\x00{version}
```

### Key Prefixes

```
KV Data:        kv\x00{namespace}\x00{key}\x00{version}
Indexes:        idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}
Metadata:       meta\x00{entity_type}\x00{id}
Sequences:      seq\x00{entity_type}
IAM:            __iam:tenant:{tenant_id}:...
Vector:         v\x00{namespace}\x00{id}\x00{version}
Timeseries:     ts\x00{metric}\x00{timestamp}
```

### Key Encoding Rules

1. **Null byte separator** (`\x00`) enables prefix scans
2. **Big-endian integers** ensure proper lexicographic ordering
3. **Common prefixes** group related data for efficient range queries
4. **Version suffix** supports MVCC and compaction

## Storage Operations

### Basic Operations

```go
// Get a value
value, err := storage.Get(ctx, key)

// Set a value (sync mode)
err := storage.Set(ctx, key, value)

// Delete a key
err := storage.Delete(ctx, key)

// Check existence
exists, err := storage.Exists(ctx, key)
```

### Batch Operations

```go
// Create a batch
batch := storage.NewBatch()

// Add operations
batch.Set(key1, value1)
batch.Set(key2, value2)
batch.Delete(key3)

// Commit atomically
err := batch.Commit()
```

### Range Scans

```go
// Scan with prefix
iter := storage.NewIterator(&IteratorOptions{
    Prefix: []byte("users:"),
})
defer iter.Close()

for iter.Next() {
    key := iter.Key()
    value := iter.Value()
    // Process key-value pair
}
```

### Snapshots

```go
// Create snapshot for consistent reads
snapshot := storage.NewSnapshot()
defer snapshot.Close()

// Read from snapshot
value, err := snapshot.Get(ctx, key)
```

## Write Modes

QuicBaaS supports multiple write modes with different durability guarantees:

### Sync Mode (Default)

```go
config.SyncWrites = true
```

- Writes flushed to disk before returning
- Full durability, survives crashes
- Latency: 1-2ms per write
- Throughput: 500-1K writes/sec

### Async Mode (Performance)

```go
config.SyncWrites = false
config.WALMinSyncInterval = 100 * time.Millisecond
```

- Writes buffered in memory
- Periodic fsync every 100ms
- Latency: < 0.5ms per write
- Throughput: 10-20K writes/sec
- Risk: Up to 100ms of writes lost on crash

### Batch Mode (High Throughput)

```go
batch := storage.NewBatch()
for i := 0; i < 10000; i++ {
    batch.Set(key, value)
}
batch.Commit()
```

- Multiple writes committed atomically
- Amortized cost across batch
- Latency: 50-100ms per batch
- Throughput: 50-100K entries/sec

## Tuning Guide

### Memory Allocation

```go
// Development
CacheSize:    128 << 20  // 128 MB
MemTableSize: 64 << 20   // 64 MB

// Production (64GB RAM)
CacheSize:    10 << 30   // 10 GB
MemTableSize: 256 << 20  // 256 MB
```

### Write-Heavy Workloads

```go
config.MemTableSize = 256 << 20           // Larger buffer
config.MaxConcurrentCompactions = 4       // More compaction threads
config.L0CompactionThreshold = 8          // Delay compaction
config.WALMinSyncInterval = 100 * time.Millisecond
```

### Read-Heavy Workloads

```go
config.CacheSize = 20 << 30               // Larger cache
config.Compression = CompressionZstd      // Better compression
config.MaxConcurrentReads = 500           // More concurrent readers
```

### Low-Latency Requirements

```go
config.CacheSize = 10 << 30               // Large cache
config.SyncWrites = false                 // Async writes
config.MaxConcurrentCompactions = 2       // Limit CPU usage
```

## Monitoring

### Key Metrics

```
Storage Metrics:
  quicbaas_storage_get_operations_total
  quicbaas_storage_set_operations_total
  quicbaas_storage_get_duration_seconds
  quicbaas_storage_set_duration_seconds
  quicbaas_storage_bytes_read_total
  quicbaas_storage_bytes_written_total
  quicbaas_storage_cache_hits_total
  quicbaas_storage_cache_misses_total
  quicbaas_storage_compactions_total
  quicbaas_storage_flushes_total
```

### Health Indicators

```
Cache hit rate:        > 80% (healthy)
Write amplification:   10-30x (typical)
Space amplification:   1.5-2.5x (normal)
L0 file count:         < 12 (stable)
Compaction backlog:    < 100MB (manageable)
```

## Troubleshooting

### High Write Latency

**Symptoms**: Writes taking > 10ms consistently

**Causes**:
- Too many L0 files (compaction falling behind)
- Small memtable size (frequent flushes)
- Sync writes on slow disk

**Solutions**:
```go
config.MemTableSize = 256 << 20
config.MaxConcurrentCompactions = 4
config.L0CompactionThreshold = 8
config.WALMinSyncInterval = 100 * time.Millisecond
```

### High Read Latency

**Symptoms**: Reads taking > 5ms consistently

**Causes**:
- Low cache hit rate (cache too small)
- Too many L0 files (read amplification)
- Missing bloom filters

**Solutions**:
```go
config.CacheSize = 10 << 30
config.L0CompactionThreshold = 4
// Ensure bloom filters enabled (default)
```

### Out of Disk Space

**Symptoms**: Disk usage growing rapidly

**Causes**:
- Compaction falling behind
- Old snapshots not released
- Too many tombstones

**Solutions**:
```bash
# Manual compaction
quicbaas server compact --force

# Check snapshots
quicbaas server stats

# Increase compaction resources
config.MaxConcurrentCompactions = 4
```

### Memory Pressure

**Symptoms**: OOM errors or high memory usage

**Causes**:
- Cache too large
- Too many memtables
- Too many open snapshots

**Solutions**:
```go
config.CacheSize = 2 << 30  // Reduce cache
config.MemTableStopWritesThreshold = 2
config.MaxOpenFiles = 500
```

## Best Practices

### DO:

- Use batches for multiple writes
- Set appropriate cache size (10-20% of data)
- Enable bloom filters for point lookups
- Monitor compaction metrics
- Use compression for large values
- Close iterators and snapshots promptly
- Use prefix scans instead of full scans

### DON'T:

- Store very large values (> 10 MB)
- Ignore compaction warnings
- Use very small cache (< 128 MB)
- Disable WAL in production
- Keep snapshots open indefinitely
- Use async mode without understanding risks
- Perform full key scans frequently

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Detailed storage architecture and design |
| [Object Storage](object-storage.md) | External object storage backends (S3, GCS, Azure, MinIO) |
| [Vector Storage](VECTOR_STORAGE_SCHEMA.md) | Vector database storage schema |
| [Performance Analysis](VECTOR_PERFORMANCE_ANALYSIS.md) | Vector performance tuning |

## External Resources

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning Guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)
- [CockroachDB Storage Layer](https://www.cockroachlabs.com/docs/stable/architecture/storage-layer.html)
