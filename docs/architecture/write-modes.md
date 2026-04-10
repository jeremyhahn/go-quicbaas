# Write Modes

QuicBaaS offers three write modes with different trade-offs between latency, throughput, and durability.

## Overview

Write modes determine how QuicBaaS handles writes and when it acknowledges them. They impact latency, throughput, durability, and consistency.

## Write Mode Comparison

| Mode | Durability | Latency | Throughput | Data Loss Risk | Use Case |
|------|------------|---------|------------|----------------|----------|
| **Sync** | Strong (Quorum) | 5-10ms | 500-1K/sec | None | Production, financial transactions |
| **WAL Durable** | Medium (Local) | 100-500μs | 10K-50K/sec | Minimal (last 10-100ms) | High-throughput production, logs |
| **Batched** | Strong (Quorum) | 500μs-5ms | 100K+/sec | None | Bulk ingestion, high-volume workloads |

## Sync Mode (Default)

Every write is immediately proposed to Raft and waits for quorum acknowledgment.

**Characteristics:**
- Strongest durability (data committed to quorum before acknowledgment)
- Immediate consistency across cluster
- Highest latency (5-10ms per write)
- Lowest throughput (500-1,000 ops/sec per node)
- Zero data loss on crash or power failure
- Standard behavior for distributed consensus

**Use Cases:**
- Financial transactions
- Critical configuration updates
- Any data that must not be lost
- Production databases requiring strong consistency

**Configuration:**
```yaml
raft:
  write_mode: "sync"
```

## WAL Durable Mode

Writes are acknowledged after WAL write, before Raft consensus.

**Characteristics:**
- Very low latency (100-500μs)
- High throughput (10K-50K ops/sec per node)
- Data durable on local node only
- Risk of data loss if node fails before replication (last 10-100ms of writes)
- Eventual consistency (millisecond-scale lag)

**Use Cases:**
- High-throughput logging systems
- Metrics collection
- Cache warming
- Analytics pipelines
- Applications that can tolerate minimal data loss

**Configuration:**
```yaml
raft:
  write_mode: "wal"
```

## Batched Mode

Writes are aggregated into batches and submitted to Raft as a single operation.

**Characteristics:**
- Excellent throughput (100K+ ops/sec with large batches)
- 100x latency improvement over sync mode
- Full Raft durability (quorum acknowledgment)
- Slight latency variance due to batching delay (configurable)
- Configurable batch size and flush interval
- Zero data loss (batched operations are replicated)

**Use Cases:**
- High-volume workloads
- Bulk data ingestion
- Time-series data collection
- Event logging
- Applications tolerating small batching delays (1-10ms)

**Configuration:**
```yaml
raft:
  write_mode: "batched"
  batch:
    max_size: 100
    flush_interval: "5ms"
```

## Decision Tree

```
Can you tolerate ANY data loss?
├─ NO → High volume?
│       ├─ NO → Sync
│       └─ YES → Batched
└─ YES → WAL Durable
```

## Configuration

### Via Config File

```yaml
raft:
  write_mode: "batched"  # sync, wal, or batched
  batch:
    max_size: 100
    flush_interval: "5ms"
```

### Via Environment Variables

```bash
export QUICBAAS_WRITE_MODE=batched
export QUICBAAS_BATCH_SIZE=100
export QUICBAAS_BATCH_FLUSH_INTERVAL=5ms
```

### Via CLI Flags

```bash
quicbaas server \
  --write-mode=batched \
  --batch-size=100 \
  --batch-flush-interval=5ms
```

## API Usage

### REST API

```bash
# Specify write mode per request
curl -X PUT http://localhost:8080/api/v1/kv/mykey \
  -H "Content-Type: application/json" \
  -H "X-Write-Mode: batched" \
  -d '{"value": "myvalue"}'
```

### gRPC API

```go
import (
    "context"
    pb "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc/proto"
)

client := pb.NewKVServiceClient(conn)

// Set with specific write mode
resp, err := client.Set(context.Background(), &pb.SetRequest{
    Key:       "mykey",
    Value:     []byte("myvalue"),
    WriteMode: "batched",
})
```

### Go Client Library

Write modes are configured via the qrdb SDK write options. QuicBaaS's client library passes write mode preferences through to the qrdb engine.

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc/client"
)

// Create client with default write mode
cli, err := client.New(client.Config{
    Addresses: []string{"localhost:9000"},
    WriteMode: "batched",
})

// Override write mode for specific operation
err = cli.SetWithMode(ctx, "key", []byte("value"), "sync")
```

## Data Loss Scenarios

### Sync Mode
```
Time: t0 -> t1 -> t2 -> CRASH
Data: Write -> fsync() -> Replicate -> ACK -> Safe

Power failure at CRASH: Zero data loss
```

### WAL Mode
```
Time: t0 -> t1 -> t2 -> t3 -> CRASH (before replication)
Data: Write -> WAL -> ACK -> Write -> Lost

Power failure at CRASH: Lose writes since last replication (~10-100ms)
```

### Batched Mode
```
Time: t0 -> t1 -> t2 -> t3 -> t4 -> CRASH (during batch)
Data: Write -> Batch -> Write -> Flush -> Replicate -> ACK -> Safe

Power failure at CRASH: Only in-flight batch lost (flushed batches are safe)
```

## Metrics

QuicBaaS exports Prometheus metrics for monitoring write performance:

### Batch Aggregator Metrics

```
# Total batches flushed
quicbaas_batch_batches_total{shard="1",status="success"} 1250

# Operations per batch
quicbaas_batch_size_bucket{shard="1",le="50"} 800
quicbaas_batch_size_bucket{shard="1",le="100"} 1200

# Flush duration
quicbaas_batch_flush_duration_seconds_bucket{shard="1",le="0.005"} 1180

# Queue depth
quicbaas_batch_queue_depth 15

# Flush reasons
quicbaas_batch_flush_reason_total{reason="size"} 950
quicbaas_batch_flush_reason_total{reason="time"} 280
quicbaas_batch_flush_reason_total{reason="close"} 20
```

### Write Mode Distribution

```
quicbaas_raft_writes_total{mode="sync"} 1000
quicbaas_raft_writes_total{mode="wal_durable"} 50000
quicbaas_raft_writes_total{mode="batched"} 100000
```

## Best Practices

### Batched Mode Tuning

**Batch Size:**
- Start with 100 operations
- Increase for higher throughput (up to 1000)
- Decrease if latency spikes

**Flush Interval:**
- Default: 5ms
- Lower for latency-sensitive apps (1-2ms)
- Higher for maximum throughput (10-50ms)

Example configurations:

```go
// High throughput
BatchConfig{
    MaxBatchSize:  500,
    FlushInterval: 10 * time.Millisecond,
}

// Low latency
BatchConfig{
    MaxBatchSize:  50,
    FlushInterval: 1 * time.Millisecond,
}
```

### Mixed Workload Patterns

Use different write modes for different data types:

```go
// Critical user data - use sync
err := cli.SetWithMode(ctx, "user:"+id, userData, "sync")

// Access logs - use batched
err = cli.SetWithMode(ctx, "log:"+ts, logData, "batched")

// Metrics - use WAL durable
err = cli.SetWithMode(ctx, "metric:"+name, metricData, "wal")
```

### Monitoring Recommendations

**Critical Alerts:**
- Batch queue depth > 80% capacity
- Batch flush errors > 0
- WAL sync errors > 0

**Warning Alerts:**
- Batch size consistently below 10
- Flush duration p99 > 100ms
- Queue depth > 50% capacity

## Troubleshooting

### High Latency in Batched Mode

**Symptoms:** p99 latency > 100ms

**Causes:**
- Batch size too large (> 1000)
- Flush interval too high (> 50ms)
- Raft proposal backlog

**Solutions:**
```go
// Reduce batch size
BatchConfig{MaxBatchSize: 100}

// Reduce flush interval
BatchConfig{FlushInterval: 5 * time.Millisecond}

// Monitor Raft metrics
quicbaas_raft_proposal_duration_seconds_bucket
```

### Low Throughput in WAL Durable Mode

**Symptoms:** < 5K ops/sec

**Causes:**
- Slow disk I/O
- CPU saturation
- Network latency

**Solutions:**
- Use NVMe SSD for WAL storage
- Tune OS disk I/O scheduler
- Monitor `quicbaas_wal_sync_duration_seconds`

### Batch Queue Filling Up

**Symptoms:** `quicbaas_batch_queue_depth` approaching max

**Causes:**
- Write rate exceeds Raft capacity
- Slow Raft proposals
- Network issues

**Solutions:**
```go
// Increase queue size
const DefaultQueueSize = 10000  // was 1000

// Increase batch size to drain faster
BatchConfig{MaxBatchSize: 500}

// Scale cluster (add more nodes)
```

## Performance Summary

| Mode | Performance | Durability |
|------|-------------|------------|
| Sync | 500-1,000 writes/sec | Durable (fsync) |
| WAL Durable | 10K-50K writes/sec | Minimal loss risk |
| Batched | 100,000+ writes/sec | Durable (fsync) |

QuicBaaS's batched mode provides exceptional throughput with full durability guarantees.

## FAQ

**Q: Can I change write mode at runtime?**
A: Yes, on a per-request basis via API. The server's default write mode requires a restart to change.

**Q: Does write mode affect read performance?**
A: Indirectly. WAL Durable may cause slightly stale reads until replication catches up (milliseconds).

**Q: What happens if the batch aggregator crashes?**
A: In-flight operations will fail and return errors. Clients should retry. Data already flushed to Raft is safe.

**Q: Can I use different write modes for different keys?**
A: Yes. Specify the write mode per request using API headers or parameters.

**Q: Which mode is recommended for production?**
A: For most production workloads, **Batched mode** offers the best balance of throughput, latency, and durability. Use Sync for critical transactions requiring immediate consistency, and WAL Durable only if you can tolerate minimal data loss.

## References

- [Raft Consensus Algorithm](https://raft.github.io/)
- [Write-Ahead Logging](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [QuicBaaS Architecture](README.md)

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0
