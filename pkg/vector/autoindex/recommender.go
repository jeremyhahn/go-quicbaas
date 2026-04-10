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

package autoindex

import "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

// Dataset size thresholds for index selection.
const (
	thresholdSmall     int64 = 10_000
	thresholdMedium    int64 = 1_000_000
	thresholdLarge     int64 = 10_000_000
	thresholdGPUMin    int64 = 100_000
	thresholdGPULarge  int64 = 10_000_000
	highRecallTarget         = 0.99
	lowLatencyTargetMs       = 1.0
)

// Memory estimation constants (bytes per vector element).
const (
	// flatBytesPerDim: float32 = 4 bytes per dimension.
	flatBytesPerDim int64 = 4
	// hnswOverheadFactor: HNSW stores graph edges, roughly 1.5x raw vectors.
	hnswOverheadFactor float64 = 1.5
	// ivfFlatOverheadFactor: IVF adds cluster metadata, roughly 1.1x raw vectors.
	ivfFlatOverheadFactor float64 = 1.1
	// ivfPQCompressionRatio: PQ compresses to roughly 1/4 of original size.
	ivfPQCompressionRatio float64 = 0.25
	// gpuOverheadFactor: GPU indexes store additional acceleration structures.
	gpuOverheadFactor float64 = 1.3
)

// Quantization recommendation strings.
const (
	QuantNone    = "none"
	QuantSQ8     = "sq8"
	QuantPQ      = "pq"
	QuantFloat16 = "float16"
)

// DatasetProfile describes the characteristics of a vector dataset for index recommendation.
type DatasetProfile struct {
	// VectorCount is the total number of vectors.
	VectorCount int64
	// Dimensions is the vector dimensionality.
	Dimensions int
	// QueryPatterns describes typical query behavior.
	QueryPatterns QueryProfile
	// GPUAvailable indicates whether GPU acceleration is available.
	GPUAvailable bool
	// MemoryBudgetBytes is the maximum memory budget for the index (0 = unlimited).
	MemoryBudgetBytes int64
}

// QueryProfile describes typical query patterns.
type QueryProfile struct {
	// AvgQPS is the average queries per second.
	AvgQPS float64
	// RecallTarget is the minimum acceptable recall (0.0-1.0).
	RecallTarget float64
	// LatencyTargetMs is the maximum acceptable latency in milliseconds.
	LatencyTargetMs float64
}

// Recommendation describes the recommended index configuration.
type Recommendation struct {
	// IndexType is the recommended index type.
	IndexType interfaces.IndexType
	// Quantization is the recommended quantization type.
	Quantization string
	// ScalarIndexes are recommended scalar indexes for metadata fields.
	ScalarIndexes []ScalarIndexRecommendation
	// Reason explains why this configuration was recommended.
	Reason string
	// EstimatedMemoryBytes is the estimated memory usage.
	EstimatedMemoryBytes int64
}

// ScalarIndexRecommendation describes a recommended scalar index.
type ScalarIndexRecommendation struct {
	// Field is the metadata field name.
	Field string
	// IndexType is the recommended scalar index type.
	IndexType string
	// Reason explains why this index type was recommended.
	Reason string
}

// Recommender analyzes dataset characteristics and recommends optimal index configuration.
type Recommender struct {
	gpuAvailable bool
}

// NewRecommender creates a new Recommender with the given GPU availability.
func NewRecommender(gpuAvailable bool) *Recommender {
	return &Recommender{
		gpuAvailable: gpuAvailable,
	}
}

// Recommend returns the optimal index configuration for the given dataset profile.
//
// Selection rules (inspired by Milvus autoindex heuristics):
//   - <10K vectors: FLAT (brute-force is fast enough, 100% recall)
//   - <1M vectors: HNSW (best recall/speed balance for medium datasets)
//   - <10M vectors: IVF_FLAT (handles scale with good recall)
//   - >10M vectors: IVF_PQ (compression needed for memory)
//   - If GPU available and >100K vectors: GPU_CAGRA (fastest for large datasets)
//   - If GPU available and >10M vectors: GPU_IVF_PQ (GPU + compression)
//   - Memory budget constraints may override to compressed types
//   - High recall target (>0.99): HNSW or FLAT
//   - Low latency target (<1ms): FLAT (if small) or GPU
func (r *Recommender) Recommend(profile *DatasetProfile) *Recommendation {
	if profile == nil || profile.VectorCount <= 0 {
		return &Recommendation{
			IndexType:            interfaces.IndexTypeFlat,
			Quantization:         QuantNone,
			Reason:               "empty or zero-vector dataset defaults to FLAT index",
			EstimatedMemoryBytes: 0,
		}
	}

	indexType := r.selectIndexType(profile)
	quantization := r.RecommendQuantization(profile)
	estimatedMem := r.EstimateMemory(indexType, profile.VectorCount, profile.Dimensions)

	// If memory budget is set and exceeded, downgrade to a compressed type.
	if profile.MemoryBudgetBytes > 0 && estimatedMem > profile.MemoryBudgetBytes {
		indexType, quantization, estimatedMem = r.applyMemoryConstraint(profile)
	}

	return &Recommendation{
		IndexType:            indexType,
		Quantization:         quantization,
		Reason:               r.buildReason(indexType, profile),
		EstimatedMemoryBytes: estimatedMem,
	}
}

// selectIndexType chooses the best index type based on dataset size, GPU, and query patterns.
func (r *Recommender) selectIndexType(profile *DatasetProfile) interfaces.IndexType {
	count := profile.VectorCount
	query := profile.QueryPatterns
	gpuAvail := r.gpuAvailable || profile.GPUAvailable

	// High recall requirement constrains the choice.
	if query.RecallTarget > highRecallTarget {
		if count < thresholdSmall {
			return interfaces.IndexTypeFlat
		}
		return interfaces.IndexTypeHNSW
	}

	// Low latency for small datasets.
	if query.LatencyTargetMs > 0 && query.LatencyTargetMs < lowLatencyTargetMs && count < thresholdSmall {
		return interfaces.IndexTypeFlat
	}

	// GPU acceleration paths.
	if gpuAvail {
		if count >= thresholdGPULarge {
			return interfaces.IndexTypeGPUIVFPQ
		}
		if count >= thresholdGPUMin {
			return interfaces.IndexTypeGPUCAGRA
		}
	}

	// CPU-based selection by dataset size.
	if count < thresholdSmall {
		return interfaces.IndexTypeFlat
	}
	if count < thresholdMedium {
		return interfaces.IndexTypeHNSW
	}
	if count < thresholdLarge {
		return interfaces.IndexTypeIVFFlat
	}
	return interfaces.IndexTypeIVFPQ
}

// RecommendQuantization suggests quantization based on dataset size and memory budget.
func (r *Recommender) RecommendQuantization(profile *DatasetProfile) string {
	if profile == nil || profile.VectorCount <= 0 {
		return QuantNone
	}

	rawSize := rawMemoryEstimate(profile.VectorCount, profile.Dimensions)

	// If memory budget is set and raw size exceeds it, suggest compression.
	if profile.MemoryBudgetBytes > 0 {
		if rawSize > profile.MemoryBudgetBytes*4 {
			return QuantPQ // Strongest compression.
		}
		if rawSize > profile.MemoryBudgetBytes*2 {
			return QuantSQ8 // Moderate compression.
		}
		if rawSize > profile.MemoryBudgetBytes {
			return QuantFloat16 // Mild compression.
		}
	}

	// Size-based defaults without explicit budget.
	if profile.VectorCount >= thresholdLarge {
		return QuantPQ
	}
	if profile.VectorCount >= thresholdMedium {
		return QuantSQ8
	}
	return QuantNone
}

// EstimateMemory estimates memory usage in bytes for a given index type and dataset.
func (r *Recommender) EstimateMemory(indexType interfaces.IndexType, vectorCount int64, dimensions int) int64 {
	if vectorCount <= 0 || dimensions <= 0 {
		return 0
	}

	raw := rawMemoryEstimate(vectorCount, dimensions)

	// estimators provides O(1) dispatch for per-index memory estimation.
	type estimatorFunc func(raw int64) int64
	estimators := map[interfaces.IndexType]estimatorFunc{
		interfaces.IndexTypeFlat:          func(raw int64) int64 { return raw },
		interfaces.IndexTypeHNSW:          func(raw int64) int64 { return int64(float64(raw) * hnswOverheadFactor) },
		interfaces.IndexTypeIVFFlat:       func(raw int64) int64 { return int64(float64(raw) * ivfFlatOverheadFactor) },
		interfaces.IndexTypeIVFPQ:         func(raw int64) int64 { return int64(float64(raw) * ivfPQCompressionRatio) },
		interfaces.IndexTypeGPUCAGRA:      func(raw int64) int64 { return int64(float64(raw) * gpuOverheadFactor) },
		interfaces.IndexTypeGPUIVFFlat:    func(raw int64) int64 { return int64(float64(raw) * ivfFlatOverheadFactor * gpuOverheadFactor) },
		interfaces.IndexTypeGPUIVFPQ:      func(raw int64) int64 { return int64(float64(raw) * ivfPQCompressionRatio * gpuOverheadFactor) },
		interfaces.IndexTypeGPUBruteForce: func(raw int64) int64 { return int64(float64(raw) * gpuOverheadFactor) },
	}

	if fn, ok := estimators[indexType]; ok {
		return fn(raw)
	}
	return raw
}

// applyMemoryConstraint downgrades index type and quantization to fit within budget.
func (r *Recommender) applyMemoryConstraint(profile *DatasetProfile) (interfaces.IndexType, string, int64) {
	// Try IVF_PQ with PQ quantization as the most compressed option.
	ivfpqMem := r.EstimateMemory(interfaces.IndexTypeIVFPQ, profile.VectorCount, profile.Dimensions)
	if profile.MemoryBudgetBytes == 0 || ivfpqMem <= profile.MemoryBudgetBytes {
		return interfaces.IndexTypeIVFPQ, QuantPQ, ivfpqMem
	}

	// If even IVF_PQ exceeds budget, still recommend it with PQ as the best effort.
	return interfaces.IndexTypeIVFPQ, QuantPQ, ivfpqMem
}

// buildReason generates a human-readable reason for the recommendation.
func (r *Recommender) buildReason(indexType interfaces.IndexType, profile *DatasetProfile) string {
	// reasons provides O(1) lookup for recommendation explanations.
	reasons := map[interfaces.IndexType]string{
		interfaces.IndexTypeFlat:          "brute-force search provides 100% recall for small datasets",
		interfaces.IndexTypeHNSW:          "HNSW provides optimal recall/speed balance for medium datasets",
		interfaces.IndexTypeIVFFlat:       "IVF-Flat handles large-scale datasets with good recall",
		interfaces.IndexTypeIVFPQ:         "IVF-PQ provides compression for very large datasets",
		interfaces.IndexTypeGPUCAGRA:      "GPU CAGRA provides fastest search for large datasets with GPU available",
		interfaces.IndexTypeGPUIVFFlat:    "GPU IVF-Flat accelerates large-scale search with GPU",
		interfaces.IndexTypeGPUIVFPQ:      "GPU IVF-PQ provides GPU-accelerated compressed search for very large datasets",
		interfaces.IndexTypeGPUBruteForce: "GPU brute-force provides exact search with GPU acceleration",
	}

	reason, ok := reasons[indexType]
	if !ok {
		reason = "default recommendation"
	}

	if profile.MemoryBudgetBytes > 0 {
		reason += " (memory-constrained)"
	}
	return reason
}

// rawMemoryEstimate calculates the raw memory for storing vectors without overhead.
func rawMemoryEstimate(vectorCount int64, dimensions int) int64 {
	return vectorCount * int64(dimensions) * flatBytesPerDim
}
