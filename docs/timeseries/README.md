# Time Series

QuicBaaS provides native time series data storage with Raft consensus, multi-shard routing, and automatic retention enforcement.

## Features

- **Raft Consensus**: All writes are replicated across the cluster for strong consistency
- **Multi-Shard Routing**: FNV-64a hash-based routing distributes metrics across shards
- **Retention Policies**: Automatic expiration and deletion of old data
- **Aggregation**: Built-in aggregation functions (avg, sum, min, max, count)
- **Tag-Based Filtering**: Dimensional filtering and grouping by tags

## Quick Start

### Write Data

```bash
# Write a single data point
quicbaas ts write --metric cpu.usage --value 75.5 --tags host=server1,region=us-east-1

# Write batch from JSON file
quicbaas ts write-batch points.json
```

### Query Data

```bash
# Query with time range
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z"

# Query with aggregation
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --aggregation avg \
  --interval 3600
```

### Configure Retention

```bash
# Set 30-day retention with 1-minute resolution
quicbaas ts set-retention --metric cpu.usage \
  --duration-sec 2592000 \
  --resolution-sec 60
```

## Documentation

- [Architecture](architecture.md) - Raft consensus, shard routing, data flow
- [Retention](retention.md) - Retention policies and enforcement
- [CLI Reference](../cli/timeseries.md) - Complete CLI documentation

## Key Concepts

### Metrics

A metric is a named time series (e.g., `cpu.usage`, `memory.used`). Each metric can have multiple data points with timestamps and tags.

### Tags

Tags are key-value pairs that provide dimensions for filtering and grouping (e.g., `host=server1`, `region=us-east`).

### Data Points

A data point consists of:
- Metric name
- Timestamp (RFC3339 format)
- Value (float64)
- Tags (optional)

### Retention Policies

Retention policies control:
- **Duration**: How long to keep data (in seconds)
- **Resolution**: Data point granularity for downsampling

## Consistency Guarantees

All write operations (WritePoint, WriteBatch, DeleteSeries, SetRetention) go through Raft consensus, ensuring:

- Strong consistency across all replicas
- Durability after commit
- Ordered operations
- Automatic failover
