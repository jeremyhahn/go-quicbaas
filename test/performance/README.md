# QuicBaaS Performance Testing Suite

This directory contains comprehensive Go-based performance tests and benchmarks for QuicBaaS.

## Directory Structure

```
test/performance/
├── cmd/                # Standalone benchmark tools
│   └── pipelined/      # Pipelined load testing tool
├── internal/           # Shared utilities
│   └── stats.go        # Statistics tracking and reporting
└── README.md           # This file
```

## Running Performance Tests

### Prerequisites

1. **QuicBaaS server running:**
   ```bash
   make run
   ```

### Run All QuicBaaS Performance Tests

```bash
make perf-test
```

### Run Specific Test Suites

**QuicBaaS throughput tests:**
```bash
go test -bench=. -benchtime=30s ./test/performance/...
```

**Run with detailed metrics:**
```bash
go test -bench=. -benchtime=30s -benchmem ./test/performance/...
```

### Run Individual Benchmarks

**Write throughput with 100 workers, WAL mode:**
```bash
go test -bench=BenchmarkPutThroughput/100workers_100b_wal -benchtime=30s ./test/performance/...
```

**Batch writes:**
```bash
go test -bench=BenchmarkBatchPutThroughput -benchtime=30s ./test/performance/...
```

**Mixed workload (50% reads, 50% writes):**
```bash
go test -bench=BenchmarkMixedWorkload/50read_50write -benchtime=30s ./test/performance/...
```

## Benchmark Parameters

### Write Modes

QuicBaaS supports multiple write modes for different performance/durability trade-offs:

- **`sync`**: Wait for quorum commit (~10-30ms) - Highest durability
- **`wal`**: Wait for local WAL write (~1-2ms) - Balanced performance/durability
- **`batched`**: Batched async writes - Highest throughput

### Test Configurations

Performance tests vary across multiple dimensions:

- **Workers**: 1, 10, 100, 1000
- **Value sizes**: 100B, 1KB, 10KB
- **Write modes**: sync, wal, batched
- **Connection pools**: 1, 10, 100 connections
- **Read/write ratios**: 50/50, 90/10, 10/90

## Interpreting Results

### Metrics Reported

- **req/sec**: Throughput (requests per second)
- **avg_ms**: Average latency in milliseconds
- **p50_ms**: 50th percentile latency (median)
- **p95_ms**: 95th percentile latency
- **p99_ms**: 99th percentile latency

### Example Output

```
BenchmarkPutThroughput/100workers_100b_wal-8   	    5000	      2.15 ms/op	 46511 req/sec	    1.85 p99_ms
BenchmarkGetThroughput/100workers_linearizable-8    10000	      0.95 ms/op	105263 req/sec	    1.20 p99_ms
```

### Performance Targets

QuicBaaS aims for:

- **Write throughput (WAL mode)**: >50,000 req/s (100 workers, 100B values)
- **Read throughput (linearizable)**: >100,000 req/s (100 workers)
- **Write latency (p99, WAL)**: <5ms
- **Read latency (p99, linearizable)**: <2ms

## Standalone Load Testing Tools

### Pipelined Load Test

High-throughput load testing with pipelined requests:

```bash
cd test/performance/cmd/pipelined
go run main.go \
  --address localhost:50051 \
  --duration 60s \
  --workers 100 \
  --pipeline 100 \
  --write-mode wal
```

## Best Practices

1. **Warm-up**: Run benchmarks twice, discard first run to warm up connections
2. **Consistent load**: Use `-benchtime` to ensure consistent test duration
3. **Isolation**: Run benchmarks on idle systems for accurate measurements
4. **Multiple runs**: Run each benchmark 3-5 times and average results
5. **Monitoring**: Check Prometheus metrics during benchmarks for system health

## Troubleshooting

### Server Not Available

```
Error: QuicBaaS server not available
```

**Solution**: Start QuicBaaS server:
```bash
make run
```

### Connection Pool Exhaustion

```
Error: too many open connections
```

**Solution**: Reduce worker count or connection pool size in test parameters.

### High Latency

If latencies are much higher than expected:

1. Check server CPU/memory usage
2. Verify no other processes competing for resources
3. Check network latency (use localhost for benchmarks)
4. Review Prometheus metrics for bottlenecks

## Contributing

When adding new performance tests:

1. Use consistent naming: `Benchmark<Operation><Variant>`
2. Report all standard metrics (throughput, latency percentiles)
3. Include test configurations as sub-benchmarks
4. Document expected performance ranges
5. Add to this README

## See Also

- [Performance Metrics Documentation](../../docs/performance/)
- [Performance Quick Reference](../../docs/performance/quick-reference.md)
- [Monitoring Guide](../../docs/operations/monitoring.md)
- [Write Modes Documentation](../../docs/architecture/write-modes.md)
