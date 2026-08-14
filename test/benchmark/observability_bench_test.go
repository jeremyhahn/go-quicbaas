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

package benchmark

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/tracing"
)

// newBenchAuthConfig returns an enabled AuthConfig backed by an ephemeral
// ECDSA P-256 signing key so benchmarks mint and validate real ES256 tokens.
// The same config validates the tokens it minted.
func newBenchAuthConfig(b *testing.B) *auth.AuthConfig {
	b.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatalf("generate signing key: %v", err)
	}
	cfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "benchmark-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       1000,
	}
	auth.SetAsymmetricSigning(cfg, key)
	return cfg
}

// BenchmarkMetricsCollection benchmarks the overhead of metrics collection.
func BenchmarkMetricsCollection(b *testing.B) {
	m := metrics.InitMetrics()

	b.Run("RequestDuration", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.RecordRequestDuration("put", "success", 0.001)
		}
	})

	b.Run("RequestSize", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.RecordRequestSize("put", 1024)
		}
	})

	b.Run("ConcurrentRequests", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.IncrementConcurrentRequests()
			m.DecrementConcurrentRequests()
		}
	})

	b.Run("TransactionDuration", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.RecordTransactionDuration(0.01)
		}
	})

	b.Run("TransactionConflicts", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.IncrementTransactionConflicts()
		}
	})

	b.Run("RaftProposalLatency", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.RecordRaftProposalLatency("shard-1", 0.005)
		}
	})
}

// BenchmarkMetricsCollectionConcurrent benchmarks concurrent metrics collection.
func BenchmarkMetricsCollectionConcurrent(b *testing.B) {
	m := metrics.InitMetrics()

	b.Run("Parallel-RequestDuration", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				m.RecordRequestDuration("put", "success", 0.001)
			}
		})
	})

	b.Run("Parallel-ConcurrentRequests", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				m.IncrementConcurrentRequests()
				m.DecrementConcurrentRequests()
			}
		})
	})
}

// BenchmarkTracingOverhead benchmarks the overhead of distributed tracing.
func BenchmarkTracingOverhead(b *testing.B) {
	ctx := context.Background()

	// Benchmark with tracing disabled
	b.Run("Disabled", func(b *testing.B) {
		tracer, _ := tracing.NewTracer(&tracing.Config{
			Enabled:        false,
			ServiceName:    "benchmark",
			ServiceVersion: "1.0.0",
		})
		defer tracer.Shutdown(ctx)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spanCtx, end := tracer.TraceRequest(ctx, "test_operation")
			_ = spanCtx
			end()
		}
	})

	// Benchmark with tracing enabled (stdout exporter)
	b.Run("Enabled-Stdout", func(b *testing.B) {
		tracer, _ := tracing.NewTracer(&tracing.Config{
			Enabled:         true,
			ServiceName:     "benchmark",
			ServiceVersion:  "1.0.0",
			ExporterType:    tracing.ExporterStdout,
			SamplingRatio:   1.0,
			Environment:     "benchmark",
			NodeID:          1,
			ShutdownTimeout: 5 * time.Second,
		})
		defer tracer.Shutdown(ctx)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spanCtx, end := tracer.TraceRequest(ctx, "test_operation")
			_ = spanCtx
			end()
		}
	})

	// Benchmark with tracing enabled (no-op exporter)
	b.Run("Enabled-Noop", func(b *testing.B) {
		tracer, _ := tracing.NewTracer(&tracing.Config{
			Enabled:         true,
			ServiceName:     "benchmark",
			ServiceVersion:  "1.0.0",
			ExporterType:    tracing.ExporterNoop,
			SamplingRatio:   1.0,
			Environment:     "benchmark",
			NodeID:          1,
			ShutdownTimeout: 5 * time.Second,
		})
		defer tracer.Shutdown(ctx)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spanCtx, end := tracer.TraceRequest(ctx, "test_operation")
			_ = spanCtx
			end()
		}
	})

	// Benchmark with sampling
	b.Run("Enabled-Sampled-10pct", func(b *testing.B) {
		tracer, _ := tracing.NewTracer(&tracing.Config{
			Enabled:         true,
			ServiceName:     "benchmark",
			ServiceVersion:  "1.0.0",
			ExporterType:    tracing.ExporterNoop,
			SamplingRatio:   0.1,
			Environment:     "benchmark",
			NodeID:          1,
			ShutdownTimeout: 5 * time.Second,
		})
		defer tracer.Shutdown(ctx)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spanCtx, end := tracer.TraceRequest(ctx, "test_operation")
			_ = spanCtx
			end()
		}
	})
}

// BenchmarkTracingNestedSpans benchmarks nested span creation.
func BenchmarkTracingNestedSpans(b *testing.B) {
	ctx := context.Background()

	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:         true,
		ServiceName:     "benchmark",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterNoop,
		SamplingRatio:   1.0,
		Environment:     "benchmark",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	defer tracer.Shutdown(ctx)

	depths := []int{1, 3, 5, 10}

	for _, depth := range depths {
		b.Run(fmt.Sprintf("Depth-%d", depth), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				createNestedSpans(ctx, tracer, depth)
			}
		})
	}
}

func createNestedSpans(ctx context.Context, tracer *tracing.Tracer, depth int) {
	if depth == 0 {
		return
	}
	ctx, span := tracer.StartSpan(ctx, fmt.Sprintf("span-%d", depth))
	defer span.End()
	createNestedSpans(ctx, tracer, depth-1)
}

// BenchmarkCachePerformance benchmarks cache operations.
func BenchmarkCachePerformance(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error", // Reduce logging overhead in benchmark
		Format: "json",
	})

	cacheManager, _ := cache.NewManager(cache.Config{
		L1Size:          10000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, newLoggerAdapter(log))
	defer cacheManager.Close()

	b.Run("Set", func(b *testing.B) {
		value := make([]byte, 1024)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			cacheManager.Set(ctx, key, value)
		}
	})

	// Pre-populate cache for Get benchmark
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := make([]byte, 1024)
		cacheManager.Set(ctx, key, value)
	}

	b.Run("Get-Hit", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%1000)
			cacheManager.Get(ctx, key)
		}
	})

	b.Run("Get-Miss", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("missing-key-%d", i)
			cacheManager.Get(ctx, key)
		}
	})

	b.Run("Delete", func(b *testing.B) {
		// Pre-populate
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("delete-key-%d", i)
			cacheManager.Set(ctx, key, []byte("value"))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("delete-key-%d", i)
			cacheManager.Delete(ctx, key)
		}
	})
}

// BenchmarkCacheConcurrent benchmarks concurrent cache access.
func BenchmarkCacheConcurrent(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	cacheManager, _ := cache.NewManager(cache.Config{
		L1Size:          10000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, newLoggerAdapter(log))
	defer cacheManager.Close()

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := make([]byte, 1024)
		cacheManager.Set(ctx, key, value)
	}

	b.Run("Get-ReadHeavy", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key-%d", i%1000)
				cacheManager.Get(ctx, key)
				i++
			}
		})
	})

	b.Run("Set-WriteHeavy", func(b *testing.B) {
		value := make([]byte, 1024)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("write-key-%d", i)
				cacheManager.Set(ctx, key, value)
				i++
			}
		})
	})

	b.Run("Mixed-ReadWrite", func(b *testing.B) {
		value := make([]byte, 1024)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 < 8 { // 80% reads
					key := fmt.Sprintf("key-%d", i%1000)
					cacheManager.Get(ctx, key)
				} else { // 20% writes
					key := fmt.Sprintf("write-key-%d", i)
					cacheManager.Set(ctx, key, value)
				}
				i++
			}
		})
	})
}

// BenchmarkCacheValueSizes benchmarks cache with different value sizes.
func BenchmarkCacheValueSizes(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			cacheManager, _ := cache.NewManager(cache.Config{
				L1Size:          1000,
				L1TTL:           5 * time.Minute,
				CleanupInterval: 1 * time.Minute,
			}, newLoggerAdapter(log))
			defer cacheManager.Close()

			value := make([]byte, size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				cacheManager.Set(ctx, key, value)
			}

			b.ReportMetric(float64(size*b.N), "bytes_cached")
		})
	}
}

// BenchmarkAuthTokenValidation benchmarks JWT token validation.
func BenchmarkAuthTokenValidation(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	authManager, _ := auth.NewInMemoryAuthManager(newBenchAuthConfig(b), log)

	// Create test user and token
	benchUser, _ := authManager.CreateUser("benchuser", []string{"admin"})
	token, _ := authManager.IssueTokenForUser(ctx, benchUser)

	b.Run("Authorize-Valid", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			authManager.Authorize(ctx, token.AccessToken, "resource", "read")
		}
	})

	b.Run("Authorize-Invalid", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			authManager.Authorize(ctx, "invalid-token", "resource", "read")
		}
	})
}

// BenchmarkAuthConcurrent benchmarks concurrent authentication operations.
func BenchmarkAuthConcurrent(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	authManager, _ := auth.NewInMemoryAuthManager(newBenchAuthConfig(b), log)

	// Create multiple users
	tokens := make([]*auth.Token, 100)
	for i := 0; i < 100; i++ {
		username := fmt.Sprintf("user%d", i)
		concUser, _ := authManager.CreateUser(username, []string{"user"})
		token, _ := authManager.IssueTokenForUser(ctx, concUser)
		tokens[i] = token
	}

	b.Run("Parallel-Authorize", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				token := tokens[i%100]
				authManager.Authorize(ctx, token.AccessToken, "resource", "read")
				i++
			}
		})
	})
}

// BenchmarkExtendedFullStack benchmarks full Phase 4 stack.
func BenchmarkExtendedFullStack(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	// Initialize all Phase 4 components
	m := metrics.InitMetrics()

	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:         true,
		ServiceName:     "benchmark",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterNoop,
		SamplingRatio:   0.1,
		Environment:     "benchmark",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	defer tracer.Shutdown(ctx)

	authManager, _ := auth.NewInMemoryAuthManager(newBenchAuthConfig(b), log)

	cacheManager, _ := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, newLoggerAdapter(log))
	defer cacheManager.Close()

	// Create test user
	fullStackUser, _ := authManager.CreateUser("benchuser", []string{"admin"})
	token, _ := authManager.IssueTokenForUser(ctx, fullStackUser)

	b.Run("WithAllFeatures", func(b *testing.B) {
		value := make([]byte, 1024)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Start tracing
			ctx, endSpan := tracer.TraceRequest(ctx, "full_stack_operation")

			// Record metrics
			m.IncrementConcurrentRequests()
			start := time.Now()

			// Authorize
			authManager.Authorize(ctx, token.AccessToken, "resource", "write")

			// Cache operation
			key := fmt.Sprintf("key-%d", i)
			cacheManager.Set(ctx, key, value)

			// Record metrics
			duration := time.Since(start).Seconds()
			m.RecordRequestDuration("put", "success", duration)
			m.RecordRequestSize("put", float64(len(value)))
			m.DecrementConcurrentRequests()

			// End tracing
			endSpan()
		}
	})

	b.Run("WithoutFeatures-Baseline", func(b *testing.B) {
		value := make([]byte, 1024)
		cache := make(map[string][]byte)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			cache[key] = value
		}
	})
}

// BenchmarkExtendedConcurrent benchmarks Phase 4 with concurrent workload.
func BenchmarkExtendedConcurrent(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	// Initialize components
	m := metrics.InitMetrics()

	tracer, _ := tracing.NewTracer(&tracing.Config{
		Enabled:         true,
		ServiceName:     "benchmark",
		ServiceVersion:  "1.0.0",
		ExporterType:    tracing.ExporterNoop,
		SamplingRatio:   0.1,
		Environment:     "benchmark",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	defer tracer.Shutdown(ctx)

	authManager, _ := auth.NewInMemoryAuthManager(newBenchAuthConfig(b), log)

	cacheManager, _ := cache.NewManager(cache.Config{
		L1Size:          10000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, newLoggerAdapter(log))
	defer cacheManager.Close()

	// Create test users
	tokens := make([]*auth.Token, 10)
	for i := 0; i < 10; i++ {
		username := fmt.Sprintf("user%d", i)
		parallelUser, _ := authManager.CreateUser(username, []string{"user"})
		token, _ := authManager.IssueTokenForUser(ctx, parallelUser)
		tokens[i] = token
	}

	b.Run("Parallel-FullStack", func(b *testing.B) {
		value := make([]byte, 1024)

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				ctx, endSpan := tracer.TraceRequest(ctx, "concurrent_operation")

				m.IncrementConcurrentRequests()
				start := time.Now()

				token := tokens[i%10]
				authManager.Authorize(ctx, token.AccessToken, "resource", "write")

				key := fmt.Sprintf("key-%d", i)
				cacheManager.Set(ctx, key, value)

				duration := time.Since(start).Seconds()
				m.RecordRequestDuration("put", "success", duration)
				m.DecrementConcurrentRequests()

				endSpan()
				i++
			}
		})
	})
}

// BenchmarkComparison compares performance with and without extended features.
func BenchmarkComparison(b *testing.B) {
	ctx := context.Background()
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})

	tests := []struct {
		name          string
		enableMetrics bool
		enableTracing bool
		enableAuth    bool
		enableCache   bool
	}{
		{"Baseline-NoFeatures", false, false, false, false},
		{"Metrics-Only", true, false, false, false},
		{"Tracing-Only", false, true, false, false},
		{"Auth-Only", false, false, true, false},
		{"Cache-Only", false, false, false, true},
		{"Metrics+Tracing", true, true, false, false},
		{"Metrics+Auth", true, false, true, false},
		{"All-Features", true, true, true, true},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			var m *metrics.Metrics
			var tracer *tracing.Tracer
			var authManager *auth.AuthManager
			var cacheManager *cache.CacheManager
			var token *auth.Token

			if tt.enableMetrics {
				metrics.Reset()
				m = metrics.InitMetrics()
			}

			if tt.enableTracing {
				tracer, _ = tracing.NewTracer(&tracing.Config{
					Enabled:         true,
					ServiceName:     "benchmark",
					ServiceVersion:  "1.0.0",
					ExporterType:    tracing.ExporterNoop,
					SamplingRatio:   0.1,
					Environment:     "benchmark",
					NodeID:          1,
					ShutdownTimeout: 5 * time.Second,
				})
				defer tracer.Shutdown(ctx)
			}

			if tt.enableAuth {
				authManager, _ = auth.NewInMemoryAuthManager(newBenchAuthConfig(b), log)
				compUser, _ := authManager.CreateUser("benchuser", []string{"admin"})
				token, _ = authManager.IssueTokenForUser(ctx, compUser)
			}

			if tt.enableCache {
				cacheManager, _ = cache.NewManager(cache.Config{
					L1Size:          1000,
					L1TTL:           5 * time.Minute,
					CleanupInterval: 1 * time.Minute,
				}, newLoggerAdapter(log))
				defer cacheManager.Close()
			}

			value := make([]byte, 1024)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				operationCtx := ctx

				if tt.enableTracing && tracer != nil {
					var endSpan func()
					operationCtx, endSpan = tracer.TraceRequest(operationCtx, "operation")
					defer endSpan()
				}

				if tt.enableMetrics && m != nil {
					m.IncrementConcurrentRequests()
					start := time.Now()
					defer func() {
						duration := time.Since(start).Seconds()
						m.RecordRequestDuration("put", "success", duration)
						m.DecrementConcurrentRequests()
					}()
				}

				if tt.enableAuth && authManager != nil && token != nil {
					authManager.Authorize(operationCtx, token.AccessToken, "resource", "write")
				}

				if tt.enableCache && cacheManager != nil {
					key := fmt.Sprintf("key-%d", i)
					cacheManager.Set(operationCtx, key, value)
				} else {
					// Baseline: simple map operation
					_ = value
				}
			}
		})
	}
}
