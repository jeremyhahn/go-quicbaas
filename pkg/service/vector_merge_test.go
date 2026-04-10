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

package service

import (
	"math"
	"testing"
)

func TestMergeSearchResults_SingleShard(t *testing.T) {
	shard := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.3},
		{ID: 3, Distance: 0.5},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard}, 5)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != 1 || results[1].ID != 2 || results[2].ID != 3 {
		t.Errorf("wrong order: got IDs %d, %d, %d", results[0].ID, results[1].ID, results[2].ID)
	}
	for i, r := range results {
		if r.Rank != i+1 {
			t.Errorf("result %d: expected rank %d, got %d", i, i+1, r.Rank)
		}
	}
}

func TestMergeSearchResults_SingleShard_KZero(t *testing.T) {
	shard := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
	}
	results := MergeSearchResults([][]*VectorSearchResult{shard}, 0)
	if results != nil {
		t.Fatalf("expected nil for k=0, got %v", results)
	}
}

func TestMergeSearchResults_MultipleShardsNoOverlap(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 3, Distance: 0.5},
	}
	shard2 := []*VectorSearchResult{
		{ID: 2, Distance: 0.2},
		{ID: 4, Distance: 0.4},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2}, 10)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	// Verify sorted by distance ascending.
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("results not sorted: index %d distance %f < index %d distance %f",
				i, results[i].Distance, i-1, results[i-1].Distance)
		}
	}
}

func TestMergeSearchResults_MultipleShardsNoOverlap_NegativeK(t *testing.T) {
	shard := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
	}
	results := MergeSearchResults([][]*VectorSearchResult{shard}, -1)
	if results != nil {
		t.Fatalf("expected nil for k=-1, got %v", results)
	}
}

func TestMergeSearchResults_DuplicateIDs(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
		{ID: 2, Distance: 0.3},
	}
	shard2 := []*VectorSearchResult{
		{ID: 1, Distance: 0.1}, // same ID, closer distance
		{ID: 3, Distance: 0.4},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2}, 10)

	if len(results) != 3 {
		t.Fatalf("expected 3 results after dedup, got %d", len(results))
	}

	// Verify ID=1 kept the closer distance (0.1).
	for _, r := range results {
		if r.ID == 1 {
			if r.Distance != 0.1 {
				t.Errorf("duplicate ID=1 should keep distance 0.1, got %f", r.Distance)
			}
			return
		}
	}
	t.Error("ID=1 not found in results")
}

func TestMergeSearchResults_DuplicateIDs_AllSameID(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
	}
	shard2 := []*VectorSearchResult{
		{ID: 1, Distance: 0.3},
	}
	shard3 := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2, shard3}, 10)

	if len(results) != 1 {
		t.Fatalf("expected 1 result after dedup of identical IDs, got %d", len(results))
	}
	if results[0].Distance != 0.1 {
		t.Errorf("expected distance 0.1, got %f", results[0].Distance)
	}
}

func TestMergeSearchResults_KLessThanTotal(t *testing.T) {
	shard := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.2},
		{ID: 3, Distance: 0.3},
		{ID: 4, Distance: 0.4},
		{ID: 5, Distance: 0.5},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard}, 3)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Should return the 3 closest.
	if results[0].ID != 1 || results[1].ID != 2 || results[2].ID != 3 {
		t.Errorf("wrong top-3: got IDs %d, %d, %d", results[0].ID, results[1].ID, results[2].ID)
	}
}

func TestMergeSearchResults_KLessThanTotal_KEqualsOne(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
		{ID: 2, Distance: 0.2},
	}
	shard2 := []*VectorSearchResult{
		{ID: 3, Distance: 0.1},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2}, 1)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != 3 {
		t.Errorf("expected ID=3 (closest), got ID=%d", results[0].ID)
	}
}

func TestMergeSearchResults_EmptyShards(t *testing.T) {
	shard1 := []*VectorSearchResult{}
	shard2 := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.3},
	}
	shard3 := []*VectorSearchResult{}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2, shard3}, 5)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestMergeSearchResults_EmptyShards_AllNilSlices(t *testing.T) {
	results := MergeSearchResults([][]*VectorSearchResult{nil, nil}, 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil shard slices, got %d", len(results))
	}
}

func TestMergeSearchResults_AllEmpty(t *testing.T) {
	results := MergeSearchResults([][]*VectorSearchResult{{}, {}, {}}, 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestMergeSearchResults_AllEmpty_NilInput(t *testing.T) {
	results := MergeSearchResults(nil, 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil input, got %d", len(results))
	}
}

func TestMergeSearchResults_SortOrder(t *testing.T) {
	// Results deliberately out of order across shards.
	shard1 := []*VectorSearchResult{
		{ID: 10, Distance: 0.9},
		{ID: 11, Distance: 0.1},
	}
	shard2 := []*VectorSearchResult{
		{ID: 20, Distance: 0.5},
		{ID: 21, Distance: 0.05},
	}
	shard3 := []*VectorSearchResult{
		{ID: 30, Distance: 0.3},
		{ID: 31, Distance: 0.7},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2, shard3}, 10)

	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("results not sorted at index %d: %f < %f",
				i, results[i].Distance, results[i-1].Distance)
		}
	}

	// Verify ranks are sequential.
	for i, r := range results {
		if r.Rank != i+1 {
			t.Errorf("result %d: expected rank %d, got %d", i, i+1, r.Rank)
		}
	}
}

func TestMergeSearchResults_SortOrder_EqualDistances(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
	}
	shard2 := []*VectorSearchResult{
		{ID: 2, Distance: 0.5},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2}, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Both should have distance 0.5.
	for _, r := range results {
		if r.Distance != 0.5 {
			t.Errorf("expected distance 0.5, got %f", r.Distance)
		}
	}
}

func TestMergeSearchResults_LargeScale(t *testing.T) {
	const numShards = 10
	const perShard = 1000
	const k = 50

	shardResults := make([][]*VectorSearchResult, numShards)
	for s := 0; s < numShards; s++ {
		results := make([]*VectorSearchResult, perShard)
		for i := 0; i < perShard; i++ {
			results[i] = &VectorSearchResult{
				ID:       uint64(s*perShard + i),
				Distance: float32(s*perShard+i) * 0.001,
			}
		}
		shardResults[s] = results
	}

	results := MergeSearchResults(shardResults, k)

	if len(results) != k {
		t.Fatalf("expected %d results, got %d", k, len(results))
	}

	// Verify sorted.
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("not sorted at index %d: %f < %f",
				i, results[i].Distance, results[i-1].Distance)
		}
	}

	// The top-50 should have the smallest distances (IDs 0-49).
	maxDist := results[k-1].Distance
	if maxDist > float32(k)*0.001+0.0001 {
		t.Errorf("top-%d max distance %f exceeds expected bound", k, maxDist)
	}
}

func TestMergeSearchResults_LargeScale_KGreaterThanTotal(t *testing.T) {
	shard := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.2},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard}, 100)

	if len(results) != 2 {
		t.Fatalf("expected 2 results (all available), got %d", len(results))
	}
}

func TestDeduplicateResults(t *testing.T) {
	results := []*VectorSearchResult{
		{ID: 1, Distance: 0.5},
		{ID: 2, Distance: 0.3},
		{ID: 1, Distance: 0.1}, // duplicate, closer
		{ID: 3, Distance: 0.4},
		{ID: 2, Distance: 0.8}, // duplicate, farther
	}

	deduped := DeduplicateResults(results)

	if len(deduped) != 3 {
		t.Fatalf("expected 3 results, got %d", len(deduped))
	}

	// Build a map for easy lookup.
	byID := make(map[uint64]*VectorSearchResult)
	for _, r := range deduped {
		byID[r.ID] = r
	}

	if byID[1].Distance != 0.1 {
		t.Errorf("ID=1 should have distance 0.1, got %f", byID[1].Distance)
	}
	if byID[2].Distance != 0.3 {
		t.Errorf("ID=2 should have distance 0.3, got %f", byID[2].Distance)
	}
	if byID[3].Distance != 0.4 {
		t.Errorf("ID=3 should have distance 0.4, got %f", byID[3].Distance)
	}
}

func TestDeduplicateResults_Empty(t *testing.T) {
	deduped := DeduplicateResults(nil)
	if deduped != nil {
		t.Fatalf("expected nil for empty input, got %v", deduped)
	}

	deduped = DeduplicateResults([]*VectorSearchResult{})
	if deduped != nil {
		t.Fatalf("expected nil for empty slice, got %v", deduped)
	}
}

func TestDeduplicateResults_NoDuplicates(t *testing.T) {
	results := []*VectorSearchResult{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.2},
		{ID: 3, Distance: 0.3},
	}

	deduped := DeduplicateResults(results)

	if len(deduped) != 3 {
		t.Fatalf("expected 3 results, got %d", len(deduped))
	}
}

func TestDeduplicateResults_SortedOutput(t *testing.T) {
	results := []*VectorSearchResult{
		{ID: 3, Distance: 0.9},
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.5},
	}

	deduped := DeduplicateResults(results)

	for i := 1; i < len(deduped); i++ {
		if deduped[i].Distance < deduped[i-1].Distance {
			t.Errorf("not sorted at index %d: %f < %f",
				i, deduped[i].Distance, deduped[i-1].Distance)
		}
	}
}

// TestResultHeap_HeapProperty verifies the max-heap invariant.
func TestResultHeap_HeapProperty(t *testing.T) {
	h := &resultHeap{
		{ID: 1, Distance: 0.1},
		{ID: 2, Distance: 0.5},
		{ID: 3, Distance: 0.3},
	}

	if h.Len() != 3 {
		t.Fatalf("expected len 3, got %d", h.Len())
	}

	// After Init, the max (largest distance) should be at the root.
	h.Swap(0, 2)
	if (*h)[0].ID != 3 || (*h)[2].ID != 1 {
		t.Error("swap did not work correctly")
	}
}

// TestResultHeap_LessMaxHeap verifies Less implements max-heap ordering.
func TestResultHeap_LessMaxHeap(t *testing.T) {
	h := resultHeap{
		{ID: 1, Distance: 0.8},
		{ID: 2, Distance: 0.2},
	}

	// In max-heap, larger distance is "less" (higher priority).
	if !h.Less(0, 1) {
		t.Error("expected h[0] (dist=0.8) to be Less than h[1] (dist=0.2) in max-heap")
	}
	if h.Less(1, 0) {
		t.Error("expected h[1] (dist=0.2) NOT to be Less than h[0] (dist=0.8) in max-heap")
	}
}

func TestMergeSearchResults_PreservesMetadata(t *testing.T) {
	shard := []*VectorSearchResult{
		{
			ID:       1,
			Distance: 0.1,
			Score:    0.9,
			Vector:   []float32{1.0, 2.0},
			Metadata: map[string]interface{}{"key": "value"},
		},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard}, 5)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Score != 0.9 {
		t.Errorf("score not preserved: got %f", r.Score)
	}
	if len(r.Vector) != 2 || r.Vector[0] != 1.0 {
		t.Errorf("vector not preserved: got %v", r.Vector)
	}
	if r.Metadata["key"] != "value" {
		t.Errorf("metadata not preserved: got %v", r.Metadata)
	}
}

func TestMergeSearchResults_InfiniteDistance(t *testing.T) {
	shard1 := []*VectorSearchResult{
		{ID: 1, Distance: float32(math.Inf(1))},
	}
	shard2 := []*VectorSearchResult{
		{ID: 2, Distance: 0.1},
	}

	results := MergeSearchResults([][]*VectorSearchResult{shard1, shard2}, 1)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != 2 {
		t.Errorf("expected ID=2 (finite distance), got ID=%d", results[0].ID)
	}
}

func BenchmarkMergeSearchResults_10Shards_100Each(b *testing.B) {
	shardResults := buildBenchmarkShards(10, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MergeSearchResults(shardResults, 50)
	}
}

func BenchmarkMergeSearchResults_10Shards_1000Each(b *testing.B) {
	shardResults := buildBenchmarkShards(10, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MergeSearchResults(shardResults, 100)
	}
}

func BenchmarkMergeSearchResults_50Shards_1000Each(b *testing.B) {
	shardResults := buildBenchmarkShards(50, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MergeSearchResults(shardResults, 100)
	}
}

func BenchmarkDeduplicateResults_10000(b *testing.B) {
	results := make([]*VectorSearchResult, 10000)
	for i := range results {
		results[i] = &VectorSearchResult{
			ID:       uint64(i % 5000), // 50% duplicates
			Distance: float32(i) * 0.0001,
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeduplicateResults(results)
	}
}

func buildBenchmarkShards(numShards, perShard int) [][]*VectorSearchResult {
	shardResults := make([][]*VectorSearchResult, numShards)
	for s := 0; s < numShards; s++ {
		results := make([]*VectorSearchResult, perShard)
		for i := 0; i < perShard; i++ {
			results[i] = &VectorSearchResult{
				ID:       uint64(s*perShard + i),
				Distance: float32(s*perShard+i) * 0.001,
			}
		}
		shardResults[s] = results
	}
	return shardResults
}
