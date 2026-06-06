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

//go:build vector

package server

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// hnswIndexAdapter wraps a go-qrdb HNSWIndex to satisfy the quicbaas
// interfaces.HNSWIndex contract.
type hnswIndexAdapter struct {
	index *qrdbsdk.HNSWIndexImpl
}

// Insert adds a new vector to the index with the given ID.
func (a *hnswIndexAdapter) Insert(id uint64, vector []float32) error {
	return a.index.Insert(id, vector)
}

// Delete removes a vector from the index by ID.
func (a *hnswIndexAdapter) Delete(id uint64) error {
	return a.index.Delete(id)
}

// Search performs k-nearest neighbor search and converts results to the
// quicbaas interface type.
func (a *hnswIndexAdapter) Search(query []float32, k int) ([]*interfaces.HNSWSearchResult, error) {
	results, err := a.index.Search(query, k)
	if err != nil {
		return nil, err
	}
	converted := make([]*interfaces.HNSWSearchResult, len(results))
	for i, r := range results {
		converted[i] = &interfaces.HNSWSearchResult{
			ID:       r.ID,
			Distance: r.Distance,
		}
	}
	return converted, nil
}

// Count returns the number of vectors in the index.
func (a *hnswIndexAdapter) Count() int64 {
	return a.index.Count()
}

// GetStats returns statistics about the index converted to the quicbaas
// interface type.
func (a *hnswIndexAdapter) GetStats() *interfaces.HNSWStats {
	stats := a.index.GetStats()
	if stats == nil {
		return nil
	}
	return &interfaces.HNSWStats{
		NodeCount:      stats.NodeCount,
		MaxLayer:       stats.MaxLayer,
		AvgConnections: stats.AvgConnections,
		Dimensions:     stats.Dimensions,
	}
}

// distanceMetricMap provides O(1) lookup from string metric names to the
// go-qrdb HNSWDistanceMetric enum.
var distanceMetricMap = map[string]qrdbsdk.HNSWDistanceMetric{
	"l2":          qrdbsdk.HNSWL2Distance,
	"L2":          qrdbsdk.HNSWL2Distance,
	"euclidean":   qrdbsdk.HNSWL2Distance,
	"EUCLIDEAN":   qrdbsdk.HNSWL2Distance,
	"cosine":      qrdbsdk.HNSWCosineDistance,
	"COSINE":      qrdbsdk.HNSWCosineDistance,
	"dot_product": qrdbsdk.HNSWDotProductDistance,
	"DOT_PRODUCT": qrdbsdk.HNSWDotProductDistance,
	"manhattan":   qrdbsdk.HNSWManhattanDistance,
	"MANHATTAN":   qrdbsdk.HNSWManhattanDistance,
	"hamming":     qrdbsdk.HNSWHammingDistance,
	"HAMMING":     qrdbsdk.HNSWHammingDistance,
}

// parseDistanceMetric converts a string distance metric to the go-qrdb
// HNSWDistanceMetric type, defaulting to L2 for unknown values.
func parseDistanceMetric(metric string) qrdbsdk.HNSWDistanceMetric {
	if dm, ok := distanceMetricMap[metric]; ok {
		return dm
	}
	return qrdbsdk.HNSWL2Distance
}

// hnswIndexFactory implements interfaces.HNSWIndexFactory by creating go-qrdb
// HNSW indexes wrapped in hnswIndexAdapter.
type hnswIndexFactory struct{}

// CreateIndex creates a new HNSW index with the specified configuration.
func (f *hnswIndexFactory) CreateIndex(config *interfaces.HNSWConfig) (interfaces.HNSWIndex, error) {
	hnswConfig := &qrdbsdk.HNSWIndexConfig{
		M:              config.M,
		MMax:           config.MMax,
		EfConstruction: config.EfConstruction,
		EfSearch:       config.EfSearch,
		DistanceMetric: parseDistanceMetric(config.DistanceMetric),
	}
	index, err := qrdbsdk.NewHNSWIndexImpl(hnswConfig)
	if err != nil {
		return nil, err
	}
	return &hnswIndexAdapter{index: index}, nil
}
