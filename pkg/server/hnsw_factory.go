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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// hnswIndexAdapter wraps a go-qrdb HNSWIndex to satisfy the quicbaas
// interfaces.HNSWIndex contract.
type hnswIndexAdapter struct {
	index *xkmssdk.QRDBHNSWIndexImpl
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
// go-qrdb DistanceMetric enum.
var distanceMetricMap = map[string]xkmssdk.QRDBHNSWDistanceMetric{
	"l2":          xkmssdk.QRDBHNSWL2Distance,
	"L2":          xkmssdk.QRDBHNSWL2Distance,
	"euclidean":   xkmssdk.QRDBHNSWL2Distance,
	"EUCLIDEAN":   xkmssdk.QRDBHNSWL2Distance,
	"cosine":      xkmssdk.QRDBHNSWCosineDistance,
	"COSINE":      xkmssdk.QRDBHNSWCosineDistance,
	"dot_product": xkmssdk.QRDBHNSWDotProductDistance,
	"DOT_PRODUCT": xkmssdk.QRDBHNSWDotProductDistance,
	"manhattan":   xkmssdk.QRDBHNSWManhattanDistance,
	"MANHATTAN":   xkmssdk.QRDBHNSWManhattanDistance,
	"hamming":     xkmssdk.QRDBHNSWHammingDistance,
	"HAMMING":     xkmssdk.QRDBHNSWHammingDistance,
}

// parseDistanceMetric converts a string distance metric to the go-qrdb
// DistanceMetric type, defaulting to L2 for unknown values.
func parseDistanceMetric(metric string) xkmssdk.QRDBHNSWDistanceMetric {
	if dm, ok := distanceMetricMap[metric]; ok {
		return dm
	}
	return xkmssdk.QRDBHNSWL2Distance
}

// hnswIndexFactory implements interfaces.HNSWIndexFactory by creating go-qrdb
// HNSW indexes wrapped in hnswIndexAdapter.
type hnswIndexFactory struct{}

// CreateIndex creates a new HNSW index with the specified configuration.
func (f *hnswIndexFactory) CreateIndex(config *interfaces.HNSWConfig) (interfaces.HNSWIndex, error) {
	hnswConfig := &xkmssdk.QRDBHNSWIndexConfig{
		M:              config.M,
		MMax:           config.MMax,
		EfConstruction: config.EfConstruction,
		EfSearch:       config.EfSearch,
		DistanceMetric: parseDistanceMetric(config.DistanceMetric),
	}
	index, err := xkmssdk.QRDBNewHNSWIndexImpl(hnswConfig)
	if err != nil {
		return nil, err
	}
	return &hnswIndexAdapter{index: index}, nil
}
