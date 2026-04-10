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

// Package service provides the unified service facade for QuicBaaS.
// This file implements multi-way merge for distributed k-NN search results.
package service

import (
	"container/heap"
	"sort"
)

// MergeSearchResults merges multiple per-shard result sets into a single
// globally-sorted top-K result set. Uses a max-heap for efficient O(n log k)
// merge. Deduplicates by vector ID, keeping the result with the smallest
// distance.
func MergeSearchResults(shardResults [][]*VectorSearchResult, k int) []*VectorSearchResult {
	if k <= 0 {
		return nil
	}

	// Flatten and deduplicate in one pass.
	deduped := deduplicateShardResults(shardResults)

	// Use a max-heap to maintain top-K smallest distances.
	// The max-heap lets us efficiently evict the worst (largest distance)
	// element when the heap exceeds k.
	h := &resultHeap{}
	heap.Init(h)

	for _, r := range deduped {
		if h.Len() < k {
			heap.Push(h, r)
		} else if r.Distance < (*h)[0].Distance {
			// Replace the worst element in the heap.
			(*h)[0] = r
			heap.Fix(h, 0)
		}
	}

	// Extract results from the heap and sort by distance ascending.
	results := make([]*VectorSearchResult, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		popped, ok := heap.Pop(h).(*VectorSearchResult)
		if !ok {
			continue
		}
		results[i] = popped
	}

	// Assign ranks (1-based).
	for i := range results {
		results[i].Rank = i + 1
	}

	return results
}

// DeduplicateResults removes duplicate vector IDs from a flat result slice,
// keeping the result with the smallest distance for each ID.
func DeduplicateResults(results []*VectorSearchResult) []*VectorSearchResult {
	if len(results) == 0 {
		return nil
	}

	seen := make(map[uint64]*VectorSearchResult, len(results))
	for _, r := range results {
		if existing, ok := seen[r.ID]; !ok || r.Distance < existing.Distance {
			seen[r.ID] = r
		}
	}

	out := make([]*VectorSearchResult, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}

	// Sort by distance ascending for deterministic output.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Distance < out[j].Distance
	})

	return out
}

// deduplicateShardResults flattens and deduplicates results across shards,
// keeping the result with the smallest distance for each vector ID.
func deduplicateShardResults(shardResults [][]*VectorSearchResult) []*VectorSearchResult {
	// Estimate total capacity.
	total := 0
	for _, shard := range shardResults {
		total += len(shard)
	}
	if total == 0 {
		return nil
	}

	seen := make(map[uint64]*VectorSearchResult, total)
	for _, shard := range shardResults {
		for _, r := range shard {
			if existing, ok := seen[r.ID]; !ok || r.Distance < existing.Distance {
				seen[r.ID] = r
			}
		}
	}

	out := make([]*VectorSearchResult, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}

	return out
}

// resultHeap is a max-heap of search results used for top-K selection.
// We use a max-heap so we can efficiently discard the worst result (largest
// distance) when the heap exceeds k.
type resultHeap []*VectorSearchResult

// Len returns the number of elements in the heap.
func (h resultHeap) Len() int { return len(h) }

// Less reports whether element i should sort before element j.
// Max-heap: larger distance has higher priority (to be popped first).
func (h resultHeap) Less(i, j int) bool { return h[i].Distance > h[j].Distance }

// Swap swaps the elements with indexes i and j.
func (h resultHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push adds an element to the heap.
func (h *resultHeap) Push(x interface{}) {
	if r, ok := x.(*VectorSearchResult); ok {
		*h = append(*h, r)
	}
}

// Pop removes and returns the maximum element from the heap.
func (h *resultHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[:n-1]
	return item
}
