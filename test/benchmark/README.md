# QuicBaaS Performance Benchmark Suite

Comprehensive production-grade benchmarks for validating QuicBaaS performance targets.

## Overview

This benchmark suite provides systematic performance testing for:
- Single-key operations (latency percentiles)
- Throughput testing (ops/sec)
- Batch operations
- Transaction performance
- Cache effectiveness (QuicBaaS vs Redis)
- Concurrent operations
- Memory and CPU profiling

## Performance Targets

### Latency (Single-Key Operations)
- **Read p99**: <10ms
- **Write p99**: <25ms
- **Delete p99**: <15ms

### Throughput
- **Sequential reads**: 10,000+ ops/sec
- **Sequential writes**: 5,000+ ops/sec
- **Mixed workload** (70% read, 30% write): 8,000+ ops/sec

## Quick Start

### Run All Benchmarks
```bash
make bench
```

### Run Specific Benchmark
```bash
go test -bench=BenchmarkReadLatency -benchmem -benchtime=10s ./test/benchmark/
```

### Run with CPU Profiling
```bash
go test -bench=. -benchmem -cpuprofile=cpu.prof ./test/benchmark/
go tool pprof cpu.prof
```

### Run with Memory Profiling
```bash
go test -bench=. -benchmem -memprofile=mem.prof ./test/benchmark/
go tool pprof mem.prof
```

## Benchmark Files

### bench_single_key_test.go
Tests single-key operation latency:
- `BenchmarkReadLatency` - Read operation latency (p50, p95, p99)
- `BenchmarkWriteLatency` - Write operation latency (p50, p95, p99)
- `BenchmarkDeleteLatency` - Delete operation latency

### bench_throughput_test.go
Tests throughput limits:
- `BenchmarkSequentialReads` - Maximum read throughput
- `BenchmarkSequentialWrites` - Maximum write throughput
- `BenchmarkMixedWorkload` - 70% read, 30% write workload

### bench_batch_test.go
Tests batch operation performance:
- `BenchmarkBatchWrite` - Batch write throughput
- `BenchmarkBatchRead` - Batch read throughput
- `BenchmarkOptimalBatchSize` - Finds optimal batch size

### bench_transactions_test.go
Tests transaction performance:
- `BenchmarkTransactionLatency` - Transaction latency
- `BenchmarkTransactionConflicts` - Conflict handling performance
- `BenchmarkTransactionThroughput` - Throughput with transactions

### bench_cache_test.go
Tests cache performance (QuicBaaS internal cache):
- `BenchmarkCacheHitRate` - Cache hit rate measurement
- `BenchmarkCacheLatencyImpact` - Cache impact on latency
- `BenchmarkCacheEviction` - Cache eviction performance

### bench_concurrent_test.go
Tests concurrent operation scaling:
- `BenchmarkConcurrentReads` - Multi-client read scalability
- `BenchmarkConcurrentWrites` - Multi-client write scalability
- `BenchmarkScalability` - 1, 10, 100, 1000 concurrent clients

## Cache Benchmarks

QuicBaaS L2 distributed cache vs Redis performance comparison.

### Overview

The cache benchmark suite compares QuicBaaS's L2 distributed cache (built on Raft consensus) against Redis, the industry-standard in-memory data store. This helps quantify performance trade-offs and guide architecture decisions.

**Key Comparisons:**
- **QuicBaaS Stale Reads**: Local cache reads (low latency, eventual consistency)
- **QuicBaaS Consistent Reads**: Raft-coordinated reads (strong consistency)
- **Redis Single Instance**: Maximum performance (single point of failure)
- **Redis Cluster**: Horizontal scalability (eventual consistency)

### Running Cache Benchmarks

```bash
# All cache benchmarks
go test -bench=BenchmarkCache -benchmem -benchtime=10s

# QuicBaaS stale reads only
go test -bench=BenchmarkCacheQuicBaaS_Stale -benchmem -benchtime=10s

# QuicBaaS consistent reads only
go test -bench=BenchmarkCacheQuicBaaS_Consistent -benchmem -benchtime=10s

# Redis benchmarks only
go test -bench=BenchmarkCacheRedis -benchmem -benchtime=10s

# Concurrent workloads
go test -bench=BenchmarkCacheConcurrent -benchmem -benchtime=10s

# Batch operations
go test -bench=BenchmarkCacheBatch -benchmem -benchtime=10s

# Latency percentiles
go test -bench=BenchmarkCacheLatency -benchmem -benchtime=10s

# Hit rate analysis
go test -bench=BenchmarkCacheHitRate -benchmem -benchtime=10s
```

### Prerequisites

**QuicBaaS Cluster:**
```bash
# Start 3-node cluster for distributed cache testing
./quicbaas --node-id=1 --cluster-members=node1:localhost:9000,node2:localhost:9001,node3:localhost:9002 \
  --data-dir=/tmp/quicbaas-bench/node1 --port=9000 &

./quicbaas --node-id=2 --cluster-members=node1:localhost:9000,node2:localhost:9001,node3:localhost:9002 \
  --data-dir=/tmp/quicbaas-bench/node2 --port=9001 &

./quicbaas --node-id=3 --cluster-members=node1:localhost:9000,node2:localhost:9001,node3:localhost:9002 \
  --data-dir=/tmp/quicbaas-bench/node3 --port=9002 &

# Wait for cluster formation
sleep 5

# Verify cluster health
./quicbaas status --address=localhost:9000
```

**Redis Instance:**
```bash
# Using Docker (recommended for benchmarking)
docker run -d --name redis-bench -p 6379:6379 redis:7-alpine

# Or install locally (Ubuntu/Debian)
sudo apt-get install redis-server

# Verify Redis is running
redis-cli ping
# Should return: PONG

# Configure Redis for benchmarking
redis-cli CONFIG SET maxmemory 2gb
redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

**Redis Cluster (Optional):**
```bash
# For cluster comparison
docker-compose -f test/benchmark/redis-cluster.yml up -d

# Verify cluster
redis-cli -c -p 7000 cluster info
```

### Benchmark Scenarios

1. **Single Key Operations**: Individual Get/Set/Delete latency (P50, P95, P99)
2. **Batch Operations**: Pipelined operations for improved throughput
3. **Concurrent Workloads**: Multi-client performance (1, 10, 100, 1000 clients)
4. **Read Mode Comparison**: Stale (fast) vs Consistent (strong consistency)
5. **Latency Percentiles**: Tail latency analysis (P99, P999)
6. **Throughput Tests**: Maximum sustainable ops/sec
7. **Cache Hit Rate**: Effectiveness under different access patterns
8. **TTL Expiration**: Time-to-live accuracy and overhead

### Expected Performance

**QuicBaaS L2 Cache (Stale Reads):**
- Reads: 30,000-60,000 ops/sec, P99 < 1ms
- Comparable to Redis single-instance for read-heavy workloads
- Eventual consistency (data may be slightly stale)

**QuicBaaS L2 Cache (Consistent Reads):**
- Reads: 5,000-15,000 ops/sec, P99 < 25ms
- Strong consistency via Raft consensus
- Unique capability: linearizable reads

**Redis (Single Instance):**
- Reads: 50,000-100,000+ ops/sec, P99 < 1ms
- Writes: 20,000-50,000 ops/sec, P99 < 2ms
- Best raw performance

**Redis Cluster:**
- Scales linearly with shards
- 100,000-500,000+ ops/sec aggregate
- Eventual consistency

### Documentation

See [CACHE_BENCHMARK.md](CACHE_BENCHMARK.md) for:
- Detailed setup instructions
- Comprehensive benchmark descriptions
- Performance analysis and interpretation
- When to use QuicBaaS vs Redis
- Configuration tuning guide

See [CACHE_RESULTS.md](CACHE_RESULTS.md) for:
- Results template with example data
- Performance comparison tables
- Decision matrix for cache selection
- Recommendations and best practices

### Quick Comparison

| Metric | QuicBaaS Stale | QuicBaaS Consistent | Redis Single | Redis Cluster |
|--------|----------------|---------------------|--------------|---------------|
| Read Latency (P99) | ~1ms | ~25ms | ~1ms | ~5ms |
| Write Latency (P99) | ~10ms | ~30ms | ~2ms | ~10ms |
| Consistency | Eventually | Strongly | Eventually | Eventually |
| Availability | High (Raft) | High (Raft) | Single point | High |
| Throughput (reads) | 30K-60K | 5K-15K | 50K-100K | 100K-500K |
| Throughput (writes) | 5K-10K | 5K-10K | 20K-50K | 30K-100K |

**When to use QuicBaaS L2 Cache:**
- Strong consistency required
- Integrated with QuicBaaS storage
- High availability via Raft
- Read-mostly workloads (stale reads)

**When to use Redis:**
- Maximum throughput critical
- Sub-millisecond latency required
- Rich data structures needed
- Proven, mature ecosystem

## Comparing Results

### Generate Baseline
```bash
go test -bench=. -benchmem ./test/benchmark/ > baseline.txt
```

### Run Comparison
```bash
go test -bench=. -benchmem ./test/benchmark/ > new.txt
benchcmp baseline.txt new.txt
```

Or use benchstat:
```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat baseline.txt new.txt
```

## CI/CD Integration

### GitHub Actions
```yaml
- name: Run Benchmarks
  run: |
    go test -bench=. -benchmem -benchtime=5s ./test/benchmark/ | tee bench.txt

- name: Compare with Baseline
  run: |
    benchstat baseline.txt bench.txt
```

### Performance Regression Detection
The benchmark suite can detect performance regressions by comparing against baseline results.

## Interpreting Results

### Benchmark Output Format
```
BenchmarkReadLatency-8    50000    25000 ns/op    1024 B/op    10 allocs/op
```

- `BenchmarkReadLatency-8` - Test name with GOMAXPROCS
- `50000` - Number of iterations
- `25000 ns/op` - Average time per operation (nanoseconds)
- `1024 B/op` - Bytes allocated per operation
- `10 allocs/op` - Allocations per operation

### Custom Metrics
Many benchmarks report additional metrics:
- `p50-latency` - 50th percentile latency
- `p95-latency` - 95th percentile latency
- `p99-latency` - 99th percentile latency
- `ops/sec` - Operations per second
- `hit-rate` - Cache hit rate percentage

## Best Practices

1. **Run on dedicated hardware** - Minimize interference from other processes
2. **Run multiple times** - Results can vary, average multiple runs
3. **Use appropriate benchtime** - Longer runs provide more stable results
4. **Profile before optimizing** - Use CPU/memory profiles to identify bottlenecks
5. **Compare apples to apples** - Use same hardware and configuration for comparisons
6. **Monitor system resources** - CPU, memory, disk I/O during benchmarks

## Troubleshooting

### Benchmarks timeout
Increase timeout:
```bash
go test -bench=. -timeout=30m ./test/benchmark/
```

### High variance in results
Run longer:
```bash
go test -bench=. -benchtime=30s ./test/benchmark/
```

### Out of memory
Reduce batch sizes or use smaller datasets.

## Contributing

When adding new benchmarks:
1. Follow the existing naming convention: `Benchmark<Feature><Test>`
2. Use `b.ResetTimer()` to exclude setup time
3. Report custom metrics with `b.ReportMetric()`
4. Clean up resources in `defer` statements
5. Document expected performance targets

## License

MIT License - See LICENSE file for details
