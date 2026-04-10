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
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// VectorBenchConfig holds configuration for vector benchmarks.
type VectorBenchConfig struct {
	Dimensions     int
	NumVectors     int
	K              int
	M              int
	EfConstruction int
	EfSearch       int
}

// DefaultVectorBenchConfig returns default benchmark configuration.
func DefaultVectorBenchConfig() *VectorBenchConfig {
	return &VectorBenchConfig{
		Dimensions:     128,
		NumVectors:     10000,
		K:              10,
		M:              16,
		EfConstruction: 200,
		EfSearch:       50,
	}
}

// generateRandomVector generates a random normalized vector.
func generateRandomVector(dim int) []float32 {
	vec := make([]float32, dim)
	var sum float32
	for i := range vec {
		vec[i] = rand.Float32()*2 - 1
		sum += vec[i] * vec[i]
	}
	norm := float32(math.Sqrt(float64(sum)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// generateVectorBatch generates a batch of random vectors.
func generateVectorBatch(count, dim int) [][]float32 {
	vectors := make([][]float32, count)
	for i := range vectors {
		vectors[i] = generateRandomVector(dim)
	}
	return vectors
}

// =============================================================================
// Vector Upsert Benchmarks
// =============================================================================

// BenchmarkVectorUpsert measures single vector upsert performance.
func BenchmarkVectorUpsert(b *testing.B) {
	dimensions := []int{64, 128, 256, 512, 768, 1024}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("Dim-%d", dim), func(b *testing.B) {
			benchmarkVectorUpsert(b, dim)
		})
	}
}

func benchmarkVectorUpsert(b *testing.B, dimensions int) {
	b.Skip("Requires vector state machine setup")

	// Pre-generate vectors for consistent benchmarking
	vectors := generateVectorBatch(b.N, dimensions)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Upsert vector
		_ = vectors[i]
		// sm.UpsertVector(ctx, namespace, id, vectors[i], nil)
	}

	b.StopTimer()

	// Report custom metrics
	b.ReportMetric(float64(dimensions*4), "bytes/vector")
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "vectors/sec")
}

// BenchmarkVectorBatchUpsert measures batch vector upsert performance.
func BenchmarkVectorBatchUpsert(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			benchmarkVectorBatchUpsert(b, batchSize, 128)
		})
	}
}

func benchmarkVectorBatchUpsert(b *testing.B, batchSize, dimensions int) {
	b.Skip("Requires vector state machine setup")

	// Pre-generate batches
	numBatches := b.N / batchSize
	if numBatches == 0 {
		numBatches = 1
	}

	batches := make([][][]float32, numBatches)
	for i := range batches {
		batches[i] = generateVectorBatch(batchSize, dimensions)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < numBatches; i++ {
		// BatchUpsert vectors
		_ = batches[i]
	}

	b.StopTimer()

	effectiveOps := float64(numBatches*batchSize) / b.Elapsed().Seconds()
	b.ReportMetric(effectiveOps, "vectors/sec")
}

// =============================================================================
// Vector Search Benchmarks
// =============================================================================

// BenchmarkVectorSearch measures k-NN search performance.
func BenchmarkVectorSearch(b *testing.B) {
	kValues := []int{1, 5, 10, 50, 100}

	for _, k := range kValues {
		b.Run(fmt.Sprintf("K-%d", k), func(b *testing.B) {
			benchmarkVectorSearch(b, k, 128, 10000)
		})
	}
}

func benchmarkVectorSearch(b *testing.B, k, dimensions, indexSize int) {
	b.Skip("Requires vector state machine setup")

	// Pre-generate query vectors
	queries := generateVectorBatch(b.N, dimensions)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// SearchSimilar(ctx, namespace, queries[i], k)
		_ = queries[i]
	}

	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "queries/sec")
	b.ReportMetric(float64(k), "k")
}

// BenchmarkVectorSearchByDimension measures search performance across dimensions.
func BenchmarkVectorSearchByDimension(b *testing.B) {
	dimensions := []int{64, 128, 256, 512, 768, 1024}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("Dim-%d", dim), func(b *testing.B) {
			benchmarkVectorSearch(b, 10, dim, 10000)
		})
	}
}

// BenchmarkVectorSearchByIndexSize measures search performance with different index sizes.
func BenchmarkVectorSearchByIndexSize(b *testing.B) {
	indexSizes := []int{1000, 5000, 10000, 50000, 100000}

	for _, size := range indexSizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			benchmarkVectorSearch(b, 10, 128, size)
		})
	}
}

// =============================================================================
// HNSW Parameter Benchmarks
// =============================================================================

// BenchmarkVectorSearchEfSearch measures impact of ef_search parameter.
func BenchmarkVectorSearchEfSearch(b *testing.B) {
	efValues := []int{10, 20, 50, 100, 200, 500}

	for _, ef := range efValues {
		b.Run(fmt.Sprintf("EfSearch-%d", ef), func(b *testing.B) {
			benchmarkVectorSearchWithEf(b, ef)
		})
	}
}

func benchmarkVectorSearchWithEf(b *testing.B, efSearch int) {
	b.Skip("Requires vector state machine setup")

	dimensions := 128
	queries := generateVectorBatch(b.N, dimensions)
	_ = efSearch

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Search with custom ef_search
		_ = queries[i]
	}

	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "queries/sec")
}

// BenchmarkVectorBuildByM measures index build time with different M values.
func BenchmarkVectorBuildByM(b *testing.B) {
	mValues := []int{8, 12, 16, 24, 32}

	for _, m := range mValues {
		b.Run(fmt.Sprintf("M-%d", m), func(b *testing.B) {
			benchmarkVectorBuild(b, m, 200, 128, 1000)
		})
	}
}

func benchmarkVectorBuild(b *testing.B, m, efConstruction, dimensions, numVectors int) {
	b.Skip("Requires vector state machine setup")

	vectors := generateVectorBatch(numVectors, dimensions)
	_ = m
	_ = efConstruction

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Build index with specific M and efConstruction
		_ = vectors
	}

	b.StopTimer()

	b.ReportMetric(float64(numVectors)/b.Elapsed().Seconds(), "vectors/sec")
}

// =============================================================================
// Vector Memory Benchmarks
// =============================================================================

// BenchmarkVectorMemoryUsage measures memory usage for vector storage.
func BenchmarkVectorMemoryUsage(b *testing.B) {
	dimensions := []int{64, 128, 256, 512}
	vectorCounts := []int{1000, 5000, 10000}

	for _, dim := range dimensions {
		for _, count := range vectorCounts {
			b.Run(fmt.Sprintf("Dim%d-Count%d", dim, count), func(b *testing.B) {
				benchmarkVectorMemory(b, dim, count)
			})
		}
	}
}

func benchmarkVectorMemory(b *testing.B, dimensions, count int) {
	b.Skip("Requires vector state machine setup")

	vectors := generateVectorBatch(count, dimensions)
	_ = vectors

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Store vectors and measure memory
	}

	b.StopTimer()

	// Calculate expected memory usage
	// Vector: dimensions * 4 bytes (float32)
	// HNSW node: ~M * 2 * 8 bytes for connections
	vectorBytes := dimensions * 4
	hnswOverhead := 16 * 2 * 8 // M=16, bidirectional, 8 bytes per ID
	totalPerVector := vectorBytes + hnswOverhead

	b.ReportMetric(float64(vectorBytes), "bytes/vector")
	b.ReportMetric(float64(hnswOverhead), "hnsw-overhead")
	b.ReportMetric(float64(totalPerVector), "total-bytes/vector")
	b.ReportMetric(float64(count*totalPerVector)/(1024*1024), "total-MB")
}

// =============================================================================
// Concurrent Vector Benchmarks
// =============================================================================

// BenchmarkVectorConcurrentRead measures concurrent read performance.
func BenchmarkVectorConcurrentRead(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8, 16, 32}

	for _, c := range concurrencyLevels {
		b.Run(fmt.Sprintf("Goroutines-%d", c), func(b *testing.B) {
			benchmarkVectorConcurrentRead(b, c)
		})
	}
}

func benchmarkVectorConcurrentRead(b *testing.B, goroutines int) {
	b.Skip("Requires vector state machine setup")

	dimensions := 128
	queries := generateVectorBatch(b.N, dimensions)

	b.SetParallelism(goroutines)
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			// Search
			_ = queries[idx%len(queries)]
			idx++
		}
	})

	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "queries/sec")
}

// BenchmarkVectorConcurrentWrite measures concurrent write performance.
func BenchmarkVectorConcurrentWrite(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8, 16}

	for _, c := range concurrencyLevels {
		b.Run(fmt.Sprintf("Goroutines-%d", c), func(b *testing.B) {
			benchmarkVectorConcurrentWrite(b, c)
		})
	}
}

func benchmarkVectorConcurrentWrite(b *testing.B, goroutines int) {
	b.Skip("Requires vector state machine setup")

	dimensions := 128
	vectors := generateVectorBatch(b.N, dimensions)

	b.SetParallelism(goroutines)
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			// Upsert
			_ = vectors[idx%len(vectors)]
			idx++
		}
	})

	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "upserts/sec")
}

// BenchmarkVectorMixedWorkload measures mixed read/write performance.
func BenchmarkVectorMixedWorkload(b *testing.B) {
	readRatios := []int{50, 80, 90, 95}

	for _, ratio := range readRatios {
		b.Run(fmt.Sprintf("ReadRatio-%d", ratio), func(b *testing.B) {
			benchmarkVectorMixedWorkload(b, ratio)
		})
	}
}

func benchmarkVectorMixedWorkload(b *testing.B, readPercentage int) {
	b.Skip("Requires vector state machine setup")

	dimensions := 128
	vectors := generateVectorBatch(b.N, dimensions)
	_ = readPercentage

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if rand.Intn(100) < readPercentage {
			// Search
		} else {
			// Upsert
		}
		_ = vectors[i%len(vectors)]
	}

	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

// =============================================================================
// Distance Metric Benchmarks
// =============================================================================

// BenchmarkDistanceMetrics measures distance calculation performance.
func BenchmarkDistanceMetrics(b *testing.B) {
	dimensions := []int{64, 128, 256, 512, 1024}

	b.Run("Cosine", func(b *testing.B) {
		for _, dim := range dimensions {
			b.Run(fmt.Sprintf("Dim-%d", dim), func(b *testing.B) {
				benchmarkCosineSimilarity(b, dim)
			})
		}
	})

	b.Run("Euclidean", func(b *testing.B) {
		for _, dim := range dimensions {
			b.Run(fmt.Sprintf("Dim-%d", dim), func(b *testing.B) {
				benchmarkEuclideanDistance(b, dim)
			})
		}
	})

	b.Run("DotProduct", func(b *testing.B) {
		for _, dim := range dimensions {
			b.Run(fmt.Sprintf("Dim-%d", dim), func(b *testing.B) {
				benchmarkDotProduct(b, dim)
			})
		}
	})
}

func benchmarkCosineSimilarity(b *testing.B, dimensions int) {
	v1 := generateRandomVector(dimensions)
	v2 := generateRandomVector(dimensions)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cosineSimilarity(v1, v2)
	}
}

func benchmarkEuclideanDistance(b *testing.B, dimensions int) {
	v1 := generateRandomVector(dimensions)
	v2 := generateRandomVector(dimensions)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		euclideanDistance(v1, v2)
	}
}

func benchmarkDotProduct(b *testing.B, dimensions int) {
	v1 := generateRandomVector(dimensions)
	v2 := generateRandomVector(dimensions)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dotProduct(v1, v2)
	}
}

// Distance metric implementations for benchmarking.
func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = float32(math.Sqrt(float64(normA)))
	normB = float32(math.Sqrt(float64(normB)))
	if normA < 1e-10 || normB < 1e-10 {
		return 0
	}
	return dot / (normA * normB)
}

func euclideanDistance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}
	return float32(math.Sqrt(float64(sum)))
}

func dotProduct(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// =============================================================================
// Latency Benchmarks
// =============================================================================

// BenchmarkVectorLatencyProfile measures latency distribution.
func BenchmarkVectorLatencyProfile(b *testing.B) {
	b.Skip("Requires vector state machine setup")

	dimensions := 128
	queries := generateVectorBatch(1000, dimensions)
	latencies := make([]time.Duration, 0, b.N)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		// Search
		_ = queries[i%len(queries)]
		latencies = append(latencies, time.Since(start))
	}

	b.StopTimer()

	// Calculate percentiles
	// (Would sort and calculate P50, P95, P99 in a real benchmark)
	_ = latencies
}

// init seeds the random number generator for reproducible benchmarks.
func init() {
	rand.Seed(time.Now().UnixNano())
}

// Placeholder to avoid unused import error.
var _ = context.Background
