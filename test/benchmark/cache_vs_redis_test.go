// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build benchmark
// +build benchmark

package benchmark

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// Benchmark configuration constants.
const (
	cacheBenchNumKeys              = 10000
	cacheBenchValueSize            = 1024 // 1KB values
	cacheBenchSmallValueSize       = 64   // 64B values
	cacheBenchLargeValueSize       = 8192 // 8KB values
	cacheBenchNumGoroutines        = 100
	cacheBenchOperationsPerRoutine = 100
	cacheBenchBatchSize            = 100
	cacheBenchDefaultTTL           = 5 * time.Minute
)

// cacheBackend defines the interface for cache implementations
type cacheBackend interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Close() error
	Name() string
}

// =============================================================================
// Backend Implementations.
// =============================================================================

// quicBaaSCache wraps QuicBaaS SDK client for standalone/cluster modes
type quicBaaSCache struct {
	client    quicbaas.Client
	readMode  string
	writeMode string // "sync", "async", or "batch"
	mode      string // "Standalone" or "Cluster"
}

func (d *quicBaaSCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// Always use linearizable consistency for writes to ensure data is committed
	// The readMode only affects Get operations
	_, err := d.client.KV().Put(ctx, &service.PutRequest{
		EntityType:  "kv",
		ID:          hash.FNV1a(key),
		Data:        value,
		Consistency: service.ConsistencyLevelLinearizable,
	})
	return err
}

func (d *quicBaaSCache) Get(ctx context.Context, key string) ([]byte, error) {
	consistency := service.ConsistencyLevelEventual
	if d.readMode == "consistent" {
		consistency = service.ConsistencyLevelLinearizable
	}

	resp, err := d.client.KV().Get(ctx, &service.GetRequest{
		EntityType:  "kv",
		ID:          hash.FNV1a(key),
		Consistency: consistency,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Found {
		return nil, ErrKeyNotFound
	}
	return resp.Data, nil
}

func (d *quicBaaSCache) Delete(ctx context.Context, key string) error {
	// Always use linearizable consistency for deletes to ensure data is removed
	// The readMode only affects Get operations
	_, err := d.client.KV().Delete(ctx, &service.DeleteRequest{
		EntityType:  "kv",
		ID:          hash.FNV1a(key),
		Consistency: service.ConsistencyLevelLinearizable,
	})
	return err
}

func (d *quicBaaSCache) Close() error {
	return d.client.Close()
}

func (d *quicBaaSCache) Name() string {
	return fmt.Sprintf("QuicBaaS-%s-%s-%s", d.mode, d.readMode, d.writeMode)
}

// redisCache wraps Redis client
type redisCache struct {
	client redis.Cmdable
	name   string
}

func (r *redisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *redisCache) Get(ctx context.Context, key string) ([]byte, error) {
	return r.client.Get(ctx, key).Bytes()
}

func (r *redisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *redisCache) Close() error {
	if closer, ok := r.client.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (r *redisCache) Name() string {
	return r.name
}

// =============================================================================
// Setup Functions.
// =============================================================================

// setupQuicBaaSStandalone creates a QuicBaaS standalone SDK backend
func setupQuicBaaSStandalone(b *testing.B, readMode, writeMode string) cacheBackend {
	b.Helper()

	// Note: writeMode is currently ignored because the SDK doesn't expose it yet
	// In a full implementation, this would be passed to the server configuration

	// Create SDK client connecting to standalone instance
	client, err := quicbaas.NewClient(
		"localhost:50051",
		quicbaas.WithTLSConfig(benchTLSConfig()),
		quicbaas.WithTimeout(5*time.Second),
	)
	if err != nil {
		b.Skipf("QuicBaaS server not available: %v", err)
	}

	// Test connection with health check
	ctx := context.Background()
	if _, err := client.Health().Readiness(ctx); err != nil {
		client.Close()
		b.Skipf("QuicBaaS server not responding: %v", err)
	}

	b.Cleanup(func() {
		client.Close()
	})

	return &quicBaaSCache{
		client:    client,
		readMode:  readMode,
		writeMode: writeMode,
		mode:      "Standalone",
	}
}

// setupQuicBaaSCluster creates a QuicBaaS cluster SDK backend (requires Docker)
func setupQuicBaaSCluster(b *testing.B, readMode, writeMode string) cacheBackend {
	b.Helper()

	// Note: writeMode is currently ignored because the SDK doesn't expose it yet
	// In a full implementation, this would be passed to the server configuration

	// Create SDK client connecting to cluster node 1
	// The cluster has 3 nodes (localhost:50051, 50052, 50053) but we can connect to any one
	client, err := quicbaas.NewClient(
		"localhost:50051",
		quicbaas.WithTLSConfig(benchTLSConfig()),
		quicbaas.WithTimeout(5*time.Second),
	)
	if err != nil {
		b.Skipf("QuicBaaS cluster not available: %v", err)
	}

	// Test connection with health check
	ctx := context.Background()
	if _, err := client.Health().Readiness(ctx); err != nil {
		client.Close()
		b.Skipf("QuicBaaS cluster not responding: %v", err)
	}

	b.Cleanup(func() {
		client.Close()
	})

	return &quicBaaSCache{
		client:    client,
		readMode:  readMode,
		writeMode: writeMode,
		mode:      "Cluster",
	}
}

// setupRedisCache creates a Redis client for single-node benchmarking
func setupRedisCache(b *testing.B) cacheBackend {
	b.Helper()

	// Connect to standalone Redis instance on default port
	client := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     20,
		MinIdleConns: 10,
		MaxRetries:   3,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		b.Skipf("Redis standalone not available: %v", err)
	}

	// Clear existing data
	if err := client.FlushDB(ctx).Err(); err != nil {
		b.Logf("Warning: Could not flush Redis: %v", err)
	}

	b.Cleanup(func() {
		client.FlushDB(ctx)
		client.Close()
	})

	return &redisCache{client: client, name: "Redis"}
}

// setupRedisClusterCache creates a Redis Cluster client for distributed benchmarking
func setupRedisClusterCache(b *testing.B) cacheBackend {
	b.Helper()

	// Use cluster-aware client that handles MOVED redirects automatically
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:          []string{"localhost:7001", "localhost:7002", "localhost:7003"},
		Password:       "",
		PoolSize:       20,
		MinIdleConns:   10,
		MaxRetries:     3,
		DialTimeout:    2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		RouteByLatency: true, // Route to closest node for reads
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		b.Skipf("Redis Cluster not available: %v", err)
	}

	// Clear existing data from all nodes
	if err := client.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
		return shard.FlushDB(ctx).Err()
	}); err != nil {
		b.Logf("Warning: Could not flush Redis Cluster: %v", err)
	}

	b.Cleanup(func() {
		client.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
			return shard.FlushDB(ctx).Err()
		})
		client.Close()
	})

	return &redisCache{client: client, name: "Redis-Cluster"}
}

// Helper to create test data.
func generateCacheTestData(size int) []byte {
	return generateTestData(size)
}

// =============================================================================
// Backend List for Benchmarks.
// =============================================================================

// getAllBackends returns all configured backend setups
func getAllBackends() []struct {
	name  string
	setup func(*testing.B) cacheBackend
} {
	return []struct {
		name  string
		setup func(*testing.B) cacheBackend
	}{
		// Redis baseline (standalone for Standalone tests)
		{"Redis", setupRedisCache},

		// Redis Cluster (for cluster comparison tests)
		{"Redis-Cluster", setupRedisClusterCache},

		// QuicBaaS Standalone (6 modes)
		{"QuicBaaS-Standalone-Stale-Sync", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "stale", "sync")
		}},
		{"QuicBaaS-Standalone-Stale-Async", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "stale", "async")
		}},
		{"QuicBaaS-Standalone-Stale-Batch", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "stale", "batch")
		}},
		{"QuicBaaS-Standalone-Consistent-Sync", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "consistent", "sync")
		}},
		{"QuicBaaS-Standalone-Consistent-Async", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "consistent", "async")
		}},
		{"QuicBaaS-Standalone-Consistent-Batch", func(b *testing.B) cacheBackend {
			return setupQuicBaaSStandalone(b, "consistent", "batch")
		}},

		// QuicBaaS Cluster (6 modes) - all skip for now pending Docker setup
		{"QuicBaaS-Cluster-Stale-Sync", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "stale", "sync")
		}},
		{"QuicBaaS-Cluster-Stale-Async", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "stale", "async")
		}},
		{"QuicBaaS-Cluster-Stale-Batch", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "stale", "batch")
		}},
		{"QuicBaaS-Cluster-Consistent-Sync", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "consistent", "sync")
		}},
		{"QuicBaaS-Cluster-Consistent-Async", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "consistent", "async")
		}},
		{"QuicBaaS-Cluster-Consistent-Batch", func(b *testing.B) cacheBackend {
			return setupQuicBaaSCluster(b, "consistent", "batch")
		}},
	}
}

// =============================================================================
// Single Key Operation Benchmarks.
// =============================================================================

// BenchmarkCache_SingleSet benchmarks single Set operations.
func BenchmarkCache_SingleSet(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Set failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCache_SingleGet benchmarks single Get operations.
func BenchmarkCache_SingleGet(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i%numKeys)
				if _, err := cache.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCache_SingleDelete benchmarks single Delete operations.
func BenchmarkCache_SingleDelete(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Delete(ctx, key); err != nil {
					b.Fatalf("Delete failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCache_SingleSetGet benchmarks Set followed by Get.
func BenchmarkCache_SingleSetGet(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Set failed: %v", err)
				}
				if _, err := cache.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// Batch Operation Benchmarks.
// =============================================================================

// BenchmarkCache_BatchSet benchmarks batch Set operations.
func BenchmarkCache_BatchSet(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < cacheBenchBatchSize; j++ {
					key := fmt.Sprintf("key-%d-%d", i, j)
					if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
						b.Fatalf("Set failed: %v", err)
					}
				}
			}
			b.StopTimer()

			// Report ops/sec
			totalOps := b.N * cacheBenchBatchSize
			opsPerSec := float64(totalOps) / b.Elapsed().Seconds()
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// BenchmarkCache_BatchGet benchmarks batch Get operations.
func BenchmarkCache_BatchGet(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			for i := 0; i < cacheBenchBatchSize; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < cacheBenchBatchSize; j++ {
					key := fmt.Sprintf("key-%d", j)
					if _, err := cache.Get(ctx, key); err != nil {
						b.Fatalf("Get failed: %v", err)
					}
				}
			}
			b.StopTimer()

			// Report ops/sec
			totalOps := b.N * cacheBenchBatchSize
			opsPerSec := float64(totalOps) / b.Elapsed().Seconds()
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// BenchmarkCache_BatchMixed benchmarks mixed operations.
func BenchmarkCache_BatchMixed(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate some keys
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < cacheBenchBatchSize; j++ {
					key := fmt.Sprintf("key-%d", j)

					// 50% reads, 30% writes, 20% deletes
					switch j % 10 {
					case 0, 1, 2, 3, 4: // 50% reads
						cache.Get(ctx, key)
					case 5, 6, 7: // 30% writes
						cache.Set(ctx, key, value, cacheBenchDefaultTTL)
					case 8, 9: // 20% deletes
						cache.Delete(ctx, key)
					}
				}
			}
		})
	}
}

// =============================================================================
// Concurrent Operation Benchmarks.
// =============================================================================

// BenchmarkCache_ConcurrentSets benchmarks concurrent Set operations.
func BenchmarkCache_ConcurrentSets(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i)
					if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
						b.Errorf("Set failed: %v", err)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkCache_ConcurrentGets benchmarks concurrent Get operations.
func BenchmarkCache_ConcurrentGets(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i%numKeys)
					if _, err := cache.Get(ctx, key); err != nil {
						b.Errorf("Get failed: %v", err)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkCache_ConcurrentMixed benchmarks concurrent mixed operations.
func BenchmarkCache_ConcurrentMixed(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name, func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate some keys
			for i := 0; i < 1000; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i%1000)

					// 70% reads, 30% writes (realistic workload)
					if i%10 < 7 {
						cache.Get(ctx, key)
					} else {
						cache.Set(ctx, key, value, cacheBenchDefaultTTL)
					}
					i++
				}
			})
		})
	}
}

// =============================================================================
// Value Size Variation Benchmarks.
// =============================================================================

// BenchmarkCache_ValueSizes benchmarks different value sizes.
func BenchmarkCache_ValueSizes(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"64B", cacheBenchSmallValueSize},
		{"1KB", cacheBenchValueSize},
		{"8KB", cacheBenchLargeValueSize},
	}

	backends := getAllBackends()

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			for _, backend := range backends {
				b.Run(backend.name, func(b *testing.B) {
					cache := backend.setup(b)
					defer cache.Close()

					ctx := context.Background()
					value := generateCacheTestData(size.size)

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						key := fmt.Sprintf("key-%d", i)
						if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
							b.Fatalf("Set failed: %v", err)
						}
						if _, err := cache.Get(ctx, key); err != nil {
							b.Fatalf("Get failed: %v", err)
						}
					}
					b.StopTimer()

					// Report throughput
					totalBytes := int64(b.N) * int64(size.size) * 2 // *2 for set+get
					mbPerSec := float64(totalBytes) / b.Elapsed().Seconds() / 1024 / 1024
					b.ReportMetric(mbPerSec, "MB/sec")
				})
			}
		})
	}
}

// =============================================================================
// Latency Percentile Benchmarks.
// =============================================================================

// BenchmarkCache_LatencyPercentiles benchmarks latency distribution.
func BenchmarkCache_LatencyPercentiles(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name+"-Get", func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			// Track latencies
			tracker := NewLatencyTracker()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i%numKeys)
				start := time.Now()
				if _, err := cache.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}
				tracker.Record(time.Since(start))
			}
			b.StopTimer()

			// Report percentiles
			stats := tracker.GetStats()
			reportLatencyStats(b, stats)
		})

		b.Run(backend.name+"-Set", func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Track latencies
			tracker := NewLatencyTracker()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				start := time.Now()
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Set failed: %v", err)
				}
				tracker.Record(time.Since(start))
			}
			b.StopTimer()

			// Report percentiles
			stats := tracker.GetStats()
			reportLatencyStats(b, stats)
		})
	}
}

// =============================================================================
// Throughput Benchmarks.
// =============================================================================

// BenchmarkCache_Throughput benchmarks maximum throughput.
func BenchmarkCache_Throughput(b *testing.B) {
	backends := getAllBackends()

	for _, backend := range backends {
		b.Run(backend.name+"-ReadOnly", func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			// Measure throughput
			tracker := NewThroughputTracker()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i%numKeys)
					_, err := cache.Get(ctx, key)
					tracker.RecordOp(cacheBenchValueSize, err == nil)
					i++
				}
			})
			b.StopTimer()

			// Report throughput
			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})

		b.Run(backend.name+"-WriteOnly", func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Measure throughput
			tracker := NewThroughputTracker()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i)
					err := cache.Set(ctx, key, value, cacheBenchDefaultTTL)
					tracker.RecordOp(cacheBenchValueSize, err == nil)
					i++
				}
			})
			b.StopTimer()

			// Report throughput
			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})

		b.Run(backend.name+"-Mixed", func(b *testing.B) {
			cache := backend.setup(b)
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate some keys
			for i := 0; i < 1000; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			// Measure throughput
			tracker := NewThroughputTracker()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i%1000)

					var err error
					// 80% reads, 20% writes
					if i%10 < 8 {
						_, err = cache.Get(ctx, key)
					} else {
						err = cache.Set(ctx, key, value, cacheBenchDefaultTTL)
					}
					tracker.RecordOp(cacheBenchValueSize, err == nil)
					i++
				}
			})
			b.StopTimer()

			// Report throughput
			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})
	}
}

// =============================================================================
// Read Mode Comparison (QuicBaaS only).
// =============================================================================

// BenchmarkCache_ReadModeComparison compares stale vs consistent reads.
func BenchmarkCache_ReadModeComparison(b *testing.B) {
	readModes := []string{"stale", "consistent"}

	for _, mode := range readModes {
		b.Run(mode, func(b *testing.B) {
			cache := setupQuicBaaSStandalone(b, mode, "sync")
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			// Track latencies
			tracker := NewLatencyTracker()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i%numKeys)
				start := time.Now()
				if _, err := cache.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}
				tracker.Record(time.Since(start))
			}
			b.StopTimer()

			// Report statistics
			stats := tracker.GetStats()
			reportLatencyStats(b, stats)

			// Report target comparison
			if mode == "stale" {
				assertPerformanceTarget(b, stats.P99, 500*time.Microsecond, "stale-p99")
			} else {
				assertPerformanceTarget(b, stats.P99, 5*time.Millisecond, "consistent-p99")
			}
		})
	}
}

// BenchmarkCache_ReadModeThroughput compares throughput between read modes.
func BenchmarkCache_ReadModeThroughput(b *testing.B) {
	readModes := []string{"stale", "consistent"}

	for _, mode := range readModes {
		b.Run(mode, func(b *testing.B) {
			cache := setupQuicBaaSStandalone(b, mode, "sync")
			defer cache.Close()

			ctx := context.Background()
			value := generateCacheTestData(cacheBenchValueSize)

			// Pre-populate cache
			numKeys := 1000
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				if err := cache.Set(ctx, key, value, cacheBenchDefaultTTL); err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
			}

			// Measure throughput
			tracker := NewThroughputTracker()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key-%d", i%numKeys)
					_, err := cache.Get(ctx, key)
					tracker.RecordOp(cacheBenchValueSize, err == nil)
					i++
				}
			})
			b.StopTimer()

			// Report throughput
			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})
	}
}
