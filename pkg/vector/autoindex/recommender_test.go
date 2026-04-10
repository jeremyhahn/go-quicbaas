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

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestRecommender_SmallDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 5_000,
		Dimensions:  128,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for small dataset, got %s", rec.IndexType)
	}
	if rec.Quantization != QuantNone {
		t.Errorf("expected no quantization for small dataset, got %s", rec.Quantization)
	}
	if rec.EstimatedMemoryBytes <= 0 {
		t.Error("expected positive memory estimate")
	}
	if rec.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestRecommender_SmallDatasetError(t *testing.T) {
	r := NewRecommender(false)
	rec := r.Recommend(nil)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for nil profile, got %s", rec.IndexType)
	}
	if rec.EstimatedMemoryBytes != 0 {
		t.Errorf("expected zero memory for nil profile, got %d", rec.EstimatedMemoryBytes)
	}
}

func TestRecommender_MediumDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 500_000,
		Dimensions:  256,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeHNSW {
		t.Errorf("expected HNSW for medium dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_MediumDatasetError(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: -1,
		Dimensions:  256,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for negative vector count, got %s", rec.IndexType)
	}
	if rec.EstimatedMemoryBytes != 0 {
		t.Errorf("expected zero memory for negative count, got %d", rec.EstimatedMemoryBytes)
	}
}

func TestRecommender_LargeDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 5_000_000,
		Dimensions:  512,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeIVFFlat {
		t.Errorf("expected IVF_FLAT for large dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_LargeDatasetError(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 5_000_000,
		Dimensions:  0,
	}
	rec := r.Recommend(profile)
	// With zero dimensions, memory estimation should be zero but type still selected.
	if rec.EstimatedMemoryBytes != 0 {
		t.Errorf("expected zero memory for zero dimensions, got %d", rec.EstimatedMemoryBytes)
	}
}

func TestRecommender_VeryLargeDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 50_000_000,
		Dimensions:  768,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeIVFPQ {
		t.Errorf("expected IVF_PQ for very large dataset, got %s", rec.IndexType)
	}
	if rec.Quantization != QuantPQ {
		t.Errorf("expected PQ quantization for very large dataset, got %s", rec.Quantization)
	}
}

func TestRecommender_VeryLargeDatasetError(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 50_000_000,
		Dimensions:  -1,
	}
	rec := r.Recommend(profile)
	if rec.EstimatedMemoryBytes != 0 {
		t.Errorf("expected zero memory for negative dimensions, got %d", rec.EstimatedMemoryBytes)
	}
}

func TestRecommender_GPUAvailableLarge(t *testing.T) {
	r := NewRecommender(true)
	profile := &DatasetProfile{
		VectorCount: 500_000,
		Dimensions:  256,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeGPUCAGRA {
		t.Errorf("expected GPU_CAGRA for GPU + large dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_GPUAvailableLargeViaProfile(t *testing.T) {
	// Test GPU availability through profile field rather than constructor.
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount:  500_000,
		Dimensions:   256,
		GPUAvailable: true,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeGPUCAGRA {
		t.Errorf("expected GPU_CAGRA when profile.GPUAvailable=true, got %s", rec.IndexType)
	}
}

func TestRecommender_GPUAvailableVeryLarge(t *testing.T) {
	r := NewRecommender(true)
	profile := &DatasetProfile{
		VectorCount: 50_000_000,
		Dimensions:  768,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeGPUIVFPQ {
		t.Errorf("expected GPU_IVF_PQ for GPU + very large dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_GPUAvailableVeryLargeError(t *testing.T) {
	r := NewRecommender(true)
	profile := &DatasetProfile{
		VectorCount: 0,
		Dimensions:  768,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for zero vectors even with GPU, got %s", rec.IndexType)
	}
}

func TestRecommender_HighRecallTarget(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 500_000,
		Dimensions:  256,
		QueryPatterns: QueryProfile{
			RecallTarget: 0.999,
		},
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeHNSW {
		t.Errorf("expected HNSW for high recall target, got %s", rec.IndexType)
	}
}

func TestRecommender_HighRecallSmallDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 5_000,
		Dimensions:  128,
		QueryPatterns: QueryProfile{
			RecallTarget: 0.999,
		},
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for high recall + small dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_MemoryConstrained(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount:       5_000_000,
		Dimensions:        512,
		MemoryBudgetBytes: 1_000_000, // Very tight budget.
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeIVFPQ {
		t.Errorf("expected IVF_PQ for memory-constrained dataset, got %s", rec.IndexType)
	}
	if rec.Quantization != QuantPQ {
		t.Errorf("expected PQ quantization for memory-constrained dataset, got %s", rec.Quantization)
	}
}

func TestRecommender_MemoryConstrainedAdequate(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount:       5_000,
		Dimensions:        128,
		MemoryBudgetBytes: 100_000_000, // Very generous budget.
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for small dataset with generous budget, got %s", rec.IndexType)
	}
}

func TestRecommender_ZeroVectors(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 0,
		Dimensions:  128,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for zero vectors, got %s", rec.IndexType)
	}
	if rec.EstimatedMemoryBytes != 0 {
		t.Errorf("expected zero memory for zero vectors, got %d", rec.EstimatedMemoryBytes)
	}
}

func TestRecommender_ZeroVectorsError(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 0,
		Dimensions:  0,
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for zero everything, got %s", rec.IndexType)
	}
	if rec.Reason == "" {
		t.Error("expected non-empty reason for zero vectors")
	}
}

func TestRecommender_RecommendQuantization(t *testing.T) {
	r := NewRecommender(false)

	tests := []struct {
		name     string
		profile  *DatasetProfile
		expected string
	}{
		{
			name:     "nil profile",
			profile:  nil,
			expected: QuantNone,
		},
		{
			name: "small dataset no budget",
			profile: &DatasetProfile{
				VectorCount: 1_000,
				Dimensions:  128,
			},
			expected: QuantNone,
		},
		{
			name: "medium dataset no budget",
			profile: &DatasetProfile{
				VectorCount: 5_000_000,
				Dimensions:  256,
			},
			expected: QuantSQ8,
		},
		{
			name: "large dataset no budget",
			profile: &DatasetProfile{
				VectorCount: 50_000_000,
				Dimensions:  768,
			},
			expected: QuantPQ,
		},
		{
			name: "tight budget forces PQ",
			profile: &DatasetProfile{
				VectorCount:       1_000_000,
				Dimensions:        256,
				MemoryBudgetBytes: 10_000, // Very tight.
			},
			expected: QuantPQ,
		},
		{
			name: "moderate budget forces SQ8",
			profile: &DatasetProfile{
				VectorCount:       100_000,
				Dimensions:        128,
				MemoryBudgetBytes: 20_000_000, // Raw = ~51.2MB, budget*2 = 40MB < raw.
			},
			expected: QuantSQ8,
		},
		{
			name: "slightly tight budget forces float16",
			profile: &DatasetProfile{
				VectorCount:       100_000,
				Dimensions:        128,
				MemoryBudgetBytes: 45_000_000, // Raw = ~51.2MB, budget < raw.
			},
			expected: QuantFloat16,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := r.RecommendQuantization(tc.profile)
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestRecommender_RecommendQuantizationError(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: -10,
		Dimensions:  128,
	}
	result := r.RecommendQuantization(profile)
	if result != QuantNone {
		t.Errorf("expected none for negative vector count, got %s", result)
	}
}

func TestRecommender_EstimateMemory(t *testing.T) {
	r := NewRecommender(false)

	tests := []struct {
		name         string
		indexType    interfaces.IndexType
		vectorCount  int64
		dimensions   int
		wantPositive bool
	}{
		{"flat", interfaces.IndexTypeFlat, 10_000, 128, true},
		{"hnsw", interfaces.IndexTypeHNSW, 10_000, 128, true},
		{"ivf_flat", interfaces.IndexTypeIVFFlat, 10_000, 128, true},
		{"ivf_pq", interfaces.IndexTypeIVFPQ, 10_000, 128, true},
		{"gpu_cagra", interfaces.IndexTypeGPUCAGRA, 10_000, 128, true},
		{"gpu_ivf_flat", interfaces.IndexTypeGPUIVFFlat, 10_000, 128, true},
		{"gpu_ivf_pq", interfaces.IndexTypeGPUIVFPQ, 10_000, 128, true},
		{"gpu_brute_force", interfaces.IndexTypeGPUBruteForce, 10_000, 128, true},
		{"zero_vectors", interfaces.IndexTypeFlat, 0, 128, false},
		{"zero_dims", interfaces.IndexTypeFlat, 10_000, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := r.EstimateMemory(tc.indexType, tc.vectorCount, tc.dimensions)
			if tc.wantPositive && mem <= 0 {
				t.Errorf("expected positive memory estimate, got %d", mem)
			}
			if !tc.wantPositive && mem != 0 {
				t.Errorf("expected zero memory estimate, got %d", mem)
			}
		})
	}

	// Verify relative ordering: HNSW > FLAT > IVF_PQ (for same data).
	flatMem := r.EstimateMemory(interfaces.IndexTypeFlat, 100_000, 256)
	hnswMem := r.EstimateMemory(interfaces.IndexTypeHNSW, 100_000, 256)
	ivfpqMem := r.EstimateMemory(interfaces.IndexTypeIVFPQ, 100_000, 256)

	if hnswMem <= flatMem {
		t.Errorf("expected HNSW memory (%d) > FLAT memory (%d)", hnswMem, flatMem)
	}
	if ivfpqMem >= flatMem {
		t.Errorf("expected IVF_PQ memory (%d) < FLAT memory (%d)", ivfpqMem, flatMem)
	}
}

func TestRecommender_EstimateMemoryError(t *testing.T) {
	r := NewRecommender(false)
	mem := r.EstimateMemory(interfaces.IndexTypeFlat, -100, 128)
	if mem != 0 {
		t.Errorf("expected zero memory for negative count, got %d", mem)
	}
	mem = r.EstimateMemory(interfaces.IndexTypeFlat, 100, -1)
	if mem != 0 {
		t.Errorf("expected zero memory for negative dimensions, got %d", mem)
	}
}

func TestRecommender_LowLatencySmallDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 5_000,
		Dimensions:  128,
		QueryPatterns: QueryProfile{
			LatencyTargetMs: 0.5,
		},
	}
	rec := r.Recommend(profile)
	if rec.IndexType != interfaces.IndexTypeFlat {
		t.Errorf("expected FLAT for low latency + small dataset, got %s", rec.IndexType)
	}
}

func TestRecommender_LowLatencyLargeDataset(t *testing.T) {
	r := NewRecommender(false)
	profile := &DatasetProfile{
		VectorCount: 500_000,
		Dimensions:  256,
		QueryPatterns: QueryProfile{
			LatencyTargetMs: 0.5,
		},
	}
	rec := r.Recommend(profile)
	// Large dataset with low latency but no GPU should still use HNSW.
	if rec.IndexType != interfaces.IndexTypeHNSW {
		t.Errorf("expected HNSW for low latency + large dataset without GPU, got %s", rec.IndexType)
	}
}

func TestInvalidProfileError(t *testing.T) {
	err := &InvalidProfileError{
		Field:  "VectorCount",
		Reason: "must be positive",
	}
	expected := "invalid dataset profile: field VectorCount: must be positive"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestInvalidProfileError_DifferentFields(t *testing.T) {
	err := &InvalidProfileError{
		Field:  "Dimensions",
		Reason: "must be non-zero",
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}
