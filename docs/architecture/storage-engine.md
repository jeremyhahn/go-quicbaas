# Storage Engine

QuicBaaS uses [PebbleDB](https://github.com/cockroachdb/pebble) as its embedded storage engine.

## Why PebbleDB?

Pure Go (no CGo), battle-tested in CockroachDB, high performance, actively developed, LSM-tree architecture for write-heavy workloads.

## Architecture

### LSM-Tree (Log-Structured Merge-Tree)

PebbleDB uses an LSM-tree architecture:

```
MemTable (RAM)
    ↓
Immutable MemTable (RAM)
    ↓
L0 SST files (Disk) ← Unsorted
    ↓
L1 SST files (Disk) ← Sorted, non-overlapping
    ↓
L2 SST files (Disk) ← Sorted, larger files
    ↓
L3-L6 SST files (Disk) ← Increasingly larger
```

Write path: write to WAL, write to MemTable, convert full MemTable to Immutable MemTable, flush to L0 SST file, background compaction merges and sorts SST files.

Read path: check MemTable, Immutable MemTable, L0 SST files (bloom filter first), L1-L6 SST files (binary search), return value or NotFound.

## Configuration

### Basic Setup

```go
import "github.com/cockroachdb/pebble"

opts := &pebble.Options{
    Cache:        pebble.NewCache(128 * 1024 * 1024), // 128 MB cache
    MaxOpenFiles: 1000,
    WALDir:       "/var/lib/quicbaas/wal",
}

db, err := pebble.Open("/var/lib/quicbaas/data", opts)
if err != nil {
    return err
}
defer db.Close()
```

### Production Configuration

```go
opts := &pebble.Options{
    // Memory
    Cache:                pebble.NewCache(512 * 1024 * 1024),  // 512 MB
    MemTableSize:         64 * 1024 * 1024,                    // 64 MB
    MemTableStopWritesThreshold: 4,

    // Files
    MaxOpenFiles:         10000,
    MaxConcurrentCompactions: 3,

    // Write-Ahead Log
    WALDir:               "/var/lib/quicbaas/wal",
    WALMinSyncInterval:   100 * time.Millisecond,

    // Compaction
    L0CompactionThreshold: 4,
    L0StopWritesThreshold: 12,
    LBaseMaxBytes:        64 * 1024 * 1024,                    // 64 MB
    MaxBytesForLevelBase: 512 * 1024 * 1024,                   // 512 MB
    MaxBytesForLevelMultiplier: 10,

    // Bloom filters
    Levels: make([]pebble.LevelOptions, 7),
}

// Configure bloom filters for all levels
for i := range opts.Levels {
    opts.Levels[i].FilterPolicy = bloom.FilterPolicy(10)
    opts.Levels[i].Compression = pebble.SnappyCompression
}
```

## Operations

### Put

```go
func (s *Storage) Put(key, value []byte) error {
    return s.db.Set(key, value, pebble.Sync)
}
```

Options: `pebble.Sync` (durable but slower), `pebble.NoSync` (faster but less durable).

### Get

```go
func (s *Storage) Get(key []byte) ([]byte, error) {
    value, closer, err := s.db.Get(key)
    if err != nil {
        if err == pebble.ErrNotFound {
            return nil, ErrNotFound
        }
        return nil, err
    }
    defer closer.Close()

    // Copy value before closer
    result := make([]byte, len(value))
    copy(result, value)
    return result, nil
}
```

### Delete

```go
func (s *Storage) Delete(key []byte) error {
    return s.db.Delete(key, pebble.Sync)
}
```

### Batch Operations

```go
func (s *Storage) BatchPut(entries map[string][]byte) error {
    batch := s.db.NewBatch()
    defer batch.Close()

    for key, value := range entries {
        if err := batch.Set([]byte(key), value, nil); err != nil {
            return err
        }
    }

    return batch.Commit(pebble.Sync)
}
```

Benefits: atomic (all or nothing), faster than individual writes, single WAL entry.

### Range Scan

```go
func (s *Storage) Scan(start, end []byte) (Iterator, error) {
    iter := s.db.NewIter(&pebble.IterOptions{
        LowerBound: start,
        UpperBound: end,
    })

    return &PebbleIterator{iter: iter}, nil
}

type PebbleIterator struct {
    iter *pebble.Iterator
}

func (it *PebbleIterator) Next() bool {
    return it.iter.Next()
}

func (it *PebbleIterator) Key() []byte {
    return it.iter.Key()
}

func (it *PebbleIterator) Value() []byte {
    return it.iter.Value()
}

func (it *PebbleIterator) Close() error {
    return it.iter.Close()
}
```

## Key Encoding

QuicBaaS uses a structured key encoding scheme:

### Primary Keys

```
Format: {entity_type}\x00{id}
Example: user\x00\x00\x00\x00\x00\x00\x00\x01
```

```go
func EncodePrimaryKey(entityType string, id uint64) []byte {
    buf := make([]byte, len(entityType)+1+8)
    n := copy(buf, entityType)
    buf[n] = 0 // Null terminator
    binary.BigEndian.PutUint64(buf[n+1:], id)
    return buf
}

func DecodePrimaryKey(key []byte) (string, uint64, error) {
    idx := bytes.IndexByte(key, 0)
    if idx == -1 {
        return "", 0, errors.New("invalid key format")
    }

    entityType := string(key[:idx])
    id := binary.BigEndian.Uint64(key[idx+1:])
    return entityType, id, nil
}
```

### Index Keys

```
Format: idx\x00{entity_type}\x00{index_name}\x00{value}\x00{id}
Example: idx\x00user\x00email\x00john@example.com\x00\x00\x00\x00\x00\x00\x00\x01
```

```go
func EncodeIndexKey(entityType, indexName string, value []byte, id uint64) []byte {
    parts := [][]byte{
        []byte("idx"),
        []byte(entityType),
        []byte(indexName),
        value,
        make([]byte, 8),
    }

    binary.BigEndian.PutUint64(parts[4], id)

    // Join with null bytes
    return bytes.Join(parts, []byte{0})
}
```

## Indexing

### Secondary Indexes

Secondary indexes enable fast lookups by non-primary key fields:

```go
type IndexManager struct {
    storage Storage
    indexes map[string]*Index
    mu      sync.RWMutex
}

type Index struct {
    Name       string
    EntityType string
    Field      string
}

// Insert adds an entry to the index
func (im *IndexManager) Insert(entityType, indexName string, value []byte, id uint64) error {
    key := EncodeIndexKey(entityType, indexName, value, id)
    return im.storage.Put(key, nil)  // Value is nil, key contains everything
}

// Lookup finds all IDs with the given index value
func (im *IndexManager) Lookup(entityType, indexName string, value []byte) ([]uint64, error) {
    prefix := EncodeIndexKey(entityType, indexName, value, 0)[:len(value)+len(indexName)+len(entityType)+3]

    iter, err := im.storage.Scan(prefix, NextKey(prefix))
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    var ids []uint64
    for iter.Next() {
        _, _, _, id, err := DecodeIndexKey(iter.Key())
        if err != nil {
            return nil, err
        }
        ids = append(ids, id)
    }

    return ids, nil
}
```

## Performance Optimization

### Bloom Filters

Bloom filters reduce disk reads for non-existent keys:

```go
opts.Levels[i].FilterPolicy = bloom.FilterPolicy(10)  // 10 bits per key
```

Trade-off: more bits reduce false positives but increase memory usage. Sweet spot: 10-15 bits per key.

### Block Cache

Cache frequently accessed blocks in memory:

```go
cache := pebble.NewCache(512 * 1024 * 1024)  // 512 MB
defer cache.Unref()

opts := &pebble.Options{
    Cache: cache,
}
```

Sizing: 10-20% of data size, minimum 128 MB, maximum 50% of available RAM.

### Compression

Reduce disk space and I/O:

```go
opts.Levels[i].Compression = pebble.SnappyCompression
```

Options: `NoCompression` (fastest), `SnappyCompression` (fast, good ratio), `ZstdCompression` (better compression, slower).

Recommendation: L0-L2 use Snappy (hot data), L3-L6 use Zstd (cold data).

### Compaction

Tune compaction for workload:

```go
opts := &pebble.Options{
    L0CompactionThreshold:      4,   // Start compaction
    L0StopWritesThreshold:      12,  // Stop writes if too many L0 files
    MaxConcurrentCompactions:   3,   // Parallel compaction threads
    MaxBytesForLevelBase:       512 * 1024 * 1024,  // 512 MB
    MaxBytesForLevelMultiplier: 10,  // Each level 10x larger
}
```

## Monitoring

### Metrics

```go
metrics := db.Metrics()

fmt.Printf("MemTable size: %d bytes\n", metrics.MemTable.Size)
fmt.Printf("L0 files: %d\n", metrics.Levels[0].NumFiles)
fmt.Printf("Total size: %d bytes\n", metrics.DiskSpaceUsage())
fmt.Printf("Read amplification: %.2f\n", metrics.ReadAmp())
fmt.Printf("Write amplification: %.2f\n", metrics.WriteAmp())
```

Key metrics: Read Amplification (disk reads per logical read), Write Amplification (bytes written to disk per byte written), Space Amplification (disk space used vs logical data size).

### Compaction Stats

```go
fmt.Printf("Compactions: %d\n", metrics.Compact.Count)
fmt.Printf("Estimated debt: %d bytes\n", metrics.Compact.EstimatedDebt)
fmt.Printf("In progress compactions: %d\n", metrics.Compact.NumInProgress)
```

## Backup and Recovery

### Snapshot (Checkpoint)

```go
// Create checkpoint
err := db.Checkpoint("/backup/checkpoint")
```

Use cases: backups, replication, testing.

### Restore

```go
// Close existing DB
db.Close()

// Copy checkpoint to data directory
os.RemoveAll("/var/lib/quicbaas/data")
os.Rename("/backup/checkpoint", "/var/lib/quicbaas/data")

// Reopen
db, err := pebble.Open("/var/lib/quicbaas/data", opts)
```

## Best Practices

DO: use batches for multiple writes, set appropriate cache size, enable bloom filters, monitor metrics regularly, use compression for large values.

DON'T: store very large values (>10 MB), ignore compaction metrics, use very small cache (<128 MB), disable WAL, store many small values in one key.

## Troubleshooting

### High Write Amplification

Causes: too many L0 files, too frequent compactions, small L0 files.

Solutions: increase MemTableSize, increase L0CompactionThreshold, increase MaxBytesForLevelBase.

### High Read Amplification

Causes: many levels to check, poor bloom filter tuning, excessive L0 files.

Solutions: increase bloom filter bits, reduce L0 files (increase compaction), increase cache size.

### Out of Disk Space

Causes: compaction debt, too many snapshots, large WAL files.

Solutions: manual compaction, delete old snapshots, increase MaxConcurrentCompactions.

## References

- [PebbleDB Documentation](https://github.com/cockroachdb/pebble)
- [LSM-tree Paper](https://www.cs.umb.edu/~poneil/lsmtree.pdf)
- [RocksDB Tuning Guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide)

---

**Last Updated:** 2025-10-06
