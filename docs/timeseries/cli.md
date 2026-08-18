# Time Series — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/timeseries/`.

Time series data commands for QuicBaaS.

## Overview

These commands allow you to write, query, and manage time series data. All commands connect to a running QuicBaaS server via gRPC.

Alias: `ts`

## Commands

### write

Write a single time series data point.

```bash
quicbaas ts write
```

#### Flags
- `--metric string`: Metric name (required)
- `--value float64`: Metric value
- `--tags stringToString`: Tags as key=value pairs
- `--timestamp string`: Timestamp (RFC3339 format, defaults to now)

#### Examples

```bash
# Write a CPU usage metric
quicbaas ts write --metric cpu.usage --value 75.5 --tags host=server1,region=us-east-1

# Write with explicit timestamp
quicbaas ts write --metric memory.used --value 4096 --timestamp "2024-01-15T10:00:00Z"
```

### write-batch

Write multiple time series data points atomically.

```bash
quicbaas ts write-batch <json-file>
```

#### Example JSON

```json
[
  {
    "metric": "cpu.usage",
    "value": 75.5,
    "tags": {"host": "server1"},
    "timestamp": "2024-01-15T10:00:00Z"
  },
  {
    "metric": "cpu.usage",
    "value": 82.3,
    "tags": {"host": "server2"},
    "timestamp": "2024-01-15T10:00:00Z"
  }
]
```

#### Examples

```bash
# Write batch from file
quicbaas ts write-batch points.json
```

### query

Query time series data over a time range.

```bash
quicbaas ts query
```

#### Flags
- `--metric string`: Metric name (required)
- `--start-time string`: Start time (RFC3339 format) (required)
- `--end-time string`: End time (RFC3339 format) (required)
- `--aggregation string`: Aggregation function (avg, sum, min, max, count)
- `--interval int32`: Aggregation interval in seconds
- `--group-by stringSlice`: Group by tag keys
- `--tags stringToString`: Filter by tags

#### Examples

```bash
# Query metric over time range
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z"

# Query with aggregation
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --aggregation avg \
  --interval 3600

# Query with tag filtering
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --tags host=server1,region=us-east-1

# Query with grouping
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --aggregation avg \
  --interval 3600 \
  --group-by host,region
```

### delete

Delete time series data for a metric over a time range.

```bash
quicbaas ts delete
```

#### Flags
- `--metric string`: Metric name (required)
- `--start-time string`: Start time (RFC3339 format) (required)
- `--end-time string`: End time (RFC3339 format) (required)
- `--tags stringToString`: Filter by tags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete metric data
quicbaas ts delete --metric cpu.usage \
  --start-time "2024-01-01T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z"

# Delete with tag filtering
quicbaas ts delete --metric cpu.usage \
  --start-time "2024-01-01T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --tags host=server1

# Delete without confirmation
quicbaas ts delete --metric cpu.usage \
  --start-time "2024-01-01T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --yes
```

### get-retention

Get retention policy for a metric.

```bash
quicbaas ts get-retention
```

#### Flags
- `--metric string`: Metric name (required)

#### Examples

```bash
# Get retention policy
quicbaas ts get-retention --metric cpu.usage

# Get retention as JSON
quicbaas ts get-retention --metric cpu.usage --output json
```

### set-retention

Set retention policy for a metric.

```bash
quicbaas ts set-retention
```

#### Flags
- `--metric string`: Metric name (required)
- `--duration-sec int64`: Retention duration in seconds (required)
- `--resolution-sec int64`: Data resolution in seconds (required)

#### Examples

```bash
# Set 30-day retention with 1-minute resolution
quicbaas ts set-retention --metric cpu.usage \
  --duration-sec 2592000 \
  --resolution-sec 60

# Set 90-day retention with 5-minute resolution
quicbaas ts set-retention --metric memory.used \
  --duration-sec 7776000 \
  --resolution-sec 300
```

### list-metrics

List all time series metrics with pagination support.

```bash
quicbaas ts list-metrics
```

#### Flags
- `--limit int`: Maximum number of metrics to return (default: 100)
- `--prefix string`: Filter by metric name prefix
- `--cursor string`: Pagination cursor

#### Examples

```bash
# List all metrics
quicbaas ts list-metrics

# List with prefix filter
quicbaas ts list-metrics --prefix cpu.

# List with pagination
quicbaas ts list-metrics --limit 50 --cursor <token>
```

## Aggregation Functions

- **avg**: Average value over interval
- **sum**: Sum of values over interval
- **min**: Minimum value over interval
- **max**: Maximum value over interval
- **count**: Count of data points over interval

## Retention Policies

Retention policies control:
- **Duration**: How long to keep data (in seconds)
- **Resolution**: Data point granularity (in seconds)

Old data is automatically deleted based on retention policy.

## Raft Consensus Integration

All write operations go through Raft consensus for strong consistency:

| Operation | Consensus | Description |
|-----------|-----------|-------------|
| `write` | Yes | Single point write replicated to cluster |
| `write-batch` | Yes | Atomic batch write replicated to cluster |
| `delete` | Yes | Delete operation replicated to cluster |
| `set-retention` | Yes | Retention policy replicated to cluster |
| `query` | No | Direct read from local storage |
| `get-retention` | No | Direct read from local storage |
| `list-metrics` | No | Direct read from local storage |

### Consistency Guarantees

- Writes are linearizable (strongly consistent)
- Reads are sequentially consistent
- Operations are durable after acknowledgment
- Leader election provides automatic failover

## Shard Routing

Time series data is distributed across shards using FNV-64a hashing:

```
Metric Name --> FNV-64a Hash --> Shard ID
```

### Routing Behavior

- Same metric always routes to the same shard
- All data points for a metric are co-located
- Enables efficient queries without cross-shard coordination
- Default: 16 shards (configurable, must be power of 2)

### Batch Write Routing

Batch writes are automatically grouped by shard:
1. Points are partitioned by metric hash
2. Each shard receives its subset atomically
3. All shard writes must succeed (or all fail)

## Error Types and Troubleshooting

### Common Errors

| Error | Description | Resolution |
|-------|-------------|------------|
| `TimeSeriesNotFoundError` | Metric does not exist | Verify metric name spelling |
| `TimeSeriesInvalidTimestampError` | Invalid timestamp format | Use RFC3339 format |
| `TimeSeriesInvalidMetricError` | Invalid metric name | Check naming conventions |
| `TimeSeriesInvalidTagsError` | Invalid tag format | Use key=value format |
| `TimeSeriesBatchTooLargeError` | Batch exceeds size limit | Split into smaller batches |
| `TimeSeriesQueryTimeoutError` | Query took too long | Narrow time range or add filters |
| `TimeSeriesShardUnavailableError` | Shard is offline | Wait for cluster recovery |
| `TimeSeriesRetentionViolationError` | Data exceeds retention | Adjust retention policy |

### Retryable Errors

The following errors are automatically retryable:
- `TimeSeriesQueryTimeoutError` - Transient timeout
- `TimeSeriesShardUnavailableError` - Temporary shard unavailability

### Troubleshooting

**Write failures:**
```bash
# Check cluster status
quicbaas cluster status

# Verify shard health
quicbaas node status
```

**Query timeouts:**
```bash
# Narrow time range
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T01:00:00Z"

# Add tag filters to reduce scan
quicbaas ts query --metric cpu.usage \
  --start-time "2024-01-15T00:00:00Z" \
  --end-time "2024-01-15T23:59:59Z" \
  --tags host=server1
```

**Shard unavailable:**
```bash
# Wait for leader election (typically < 10 seconds)
# Check node logs for election status
```

## Notes

- Timestamps must be in RFC3339 format (e.g., 2024-01-15T10:00:00Z)
- Tags provide dimensional filtering and grouping
- Aggregation requires interval parameter
- Retention policies are per-metric
- Batch writes are atomic (all succeed or all fail)
- Query results are ordered by timestamp

## See Also

- [Time Series Architecture](../timeseries/architecture.md)
- [Retention Policies](engineering.md)
- [Raft Consensus](../cluster/architecture.md)
