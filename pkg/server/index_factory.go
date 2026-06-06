//go:build vector

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

package server

import (
	"strconv"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// IndexCreatorFunc creates a VectorIndex from configuration.
type IndexCreatorFunc func(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error)

// indexCreators maps index types to their creator functions for O(1) dispatch.
var indexCreators = map[interfaces.IndexType]IndexCreatorFunc{
	interfaces.IndexTypeFlat:    createFlatIndex,
	interfaces.IndexTypeHNSW:    createHNSWIndex,
	interfaces.IndexTypeIVFFlat: createIVFFlatIndex,
	interfaces.IndexTypeIVFPQ:   createIVFPQIndex,
}

// vectorIndexFactory implements interfaces.VectorIndexFactory using map-based dispatch.
type vectorIndexFactory struct{}

// NewVectorIndexFactory creates a new VectorIndexFactory that supports all index types.
func NewVectorIndexFactory() interfaces.VectorIndexFactory {
	return &vectorIndexFactory{}
}

// CreateIndex creates a new VectorIndex based on the configuration's IndexType.
func (f *vectorIndexFactory) CreateIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	creator, ok := indexCreators[config.IndexType]
	if !ok {
		return nil, api.NewUnsupportedIndexTypeError(
			"vectorIndexFactory.CreateIndex",
			config.IndexType.String(),
		)
	}
	return creator(config)
}

// =============================================================================
// HNSW Index Adapter
// =============================================================================

// hnswVectorIndexAdapter wraps the existing hnswIndexAdapter to satisfy
// the VectorIndex interface, providing backward compatibility.
type hnswVectorIndexAdapter struct {
	inner *hnswIndexAdapter
}

// Insert adds a vector with the given ID.
func (a *hnswVectorIndexAdapter) Insert(id uint64, vector []float32) error {
	return a.inner.Insert(id, vector)
}

// Delete removes a vector by ID.
func (a *hnswVectorIndexAdapter) Delete(id uint64) error {
	return a.inner.Delete(id)
}

// Search performs k-nearest neighbor search. SearchParams.EfSearch is ignored
// at the adapter level since efSearch is set at index construction time.
func (a *hnswVectorIndexAdapter) Search(query []float32, k int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
	results, err := a.inner.Search(query, k)
	if err != nil {
		return nil, err
	}
	converted := make([]*interfaces.SearchResult, len(results))
	for i, r := range results {
		converted[i] = &interfaces.SearchResult{
			ID:       r.ID,
			Distance: r.Distance,
		}
	}
	return converted, nil
}

// Count returns the number of indexed vectors.
func (a *hnswVectorIndexAdapter) Count() int64 {
	return a.inner.Count()
}

// GetStats returns index statistics.
func (a *hnswVectorIndexAdapter) GetStats() *interfaces.IndexStats {
	stats := a.inner.GetStats()
	if stats == nil {
		return &interfaces.IndexStats{IndexType: interfaces.IndexTypeHNSW}
	}
	return &interfaces.IndexStats{
		NodeCount:      stats.NodeCount,
		MaxLayer:       stats.MaxLayer,
		AvgConnections: stats.AvgConnections,
		Dimensions:     stats.Dimensions,
		IndexType:      interfaces.IndexTypeHNSW,
		IsTrained:      true,
	}
}

// IndexType returns IndexTypeHNSW.
func (a *hnswVectorIndexAdapter) IndexType() interfaces.IndexType {
	return interfaces.IndexTypeHNSW
}

// Train is a no-op for HNSW indexes; they do not require training.
func (a *hnswVectorIndexAdapter) Train(_ [][]float32) error {
	return nil
}

// IsTrained returns true; HNSW indexes are always ready for use.
func (a *hnswVectorIndexAdapter) IsTrained() bool {
	return true
}

// Close releases index resources. HNSW indexes do not hold external resources.
func (a *hnswVectorIndexAdapter) Close() error {
	return nil
}

// createHNSWIndex creates an HNSW index wrapped in a VectorIndex adapter.
func createHNSWIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
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
	return &hnswVectorIndexAdapter{
		inner: &hnswIndexAdapter{index: index},
	}, nil
}

// =============================================================================
// FLAT Index Adapter
// =============================================================================

// flatIndexAdapter wraps a go-qrdb flat.Index to satisfy VectorIndex.
type flatIndexAdapter struct {
	index *qrdbsdk.FlatIndex
}

// Insert adds a vector with the given ID.
func (a *flatIndexAdapter) Insert(id uint64, vector []float32) error {
	return a.index.Insert(id, vector)
}

// Delete removes a vector by ID.
func (a *flatIndexAdapter) Delete(id uint64) error {
	return a.index.Delete(id)
}

// Search performs brute-force k-nearest neighbor search.
func (a *flatIndexAdapter) Search(query []float32, k int, _ *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
	results, err := a.index.Search(query, k)
	if err != nil {
		return nil, err
	}
	converted := make([]*interfaces.SearchResult, len(results))
	for i, r := range results {
		converted[i] = &interfaces.SearchResult{
			ID:       r.ID,
			Distance: r.Distance,
		}
	}
	return converted, nil
}

// Count returns the number of indexed vectors.
func (a *flatIndexAdapter) Count() int64 {
	return a.index.Count()
}

// GetStats returns index statistics.
func (a *flatIndexAdapter) GetStats() *interfaces.IndexStats {
	return &interfaces.IndexStats{
		NodeCount:  a.index.Count(),
		Dimensions: a.index.Dimensions(),
		IndexType:  interfaces.IndexTypeFlat,
		IsTrained:  true,
	}
}

// IndexType returns IndexTypeFlat.
func (a *flatIndexAdapter) IndexType() interfaces.IndexType {
	return interfaces.IndexTypeFlat
}

// Train is a no-op for FLAT indexes; they do not require training.
func (a *flatIndexAdapter) Train(_ [][]float32) error {
	return nil
}

// IsTrained returns true; FLAT indexes are always ready for use.
func (a *flatIndexAdapter) IsTrained() bool {
	return true
}

// Close releases index resources.
func (a *flatIndexAdapter) Close() error {
	return a.index.Close()
}

// createFlatIndex creates a flat brute-force index wrapped in a VectorIndex adapter.
func createFlatIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	distMetric := parseDistanceMetric(config.DistanceMetric)
	index := qrdbsdk.NewFlatIndex(distMetric)
	return &flatIndexAdapter{index: index}, nil
}

// =============================================================================
// IVF Index Adapters
// =============================================================================

// ivfDistanceMetricMap maps string distance metrics to IVF DistanceMetric constants.
var ivfDistanceMetricMap = map[string]qrdbsdk.IVFDistanceMetric{
	"l2":          qrdbsdk.IVFL2Distance,
	"L2":          qrdbsdk.IVFL2Distance,
	"euclidean":   qrdbsdk.IVFL2Distance,
	"EUCLIDEAN":   qrdbsdk.IVFL2Distance,
	"cosine":      qrdbsdk.IVFCosineDistance,
	"COSINE":      qrdbsdk.IVFCosineDistance,
	"dot_product": qrdbsdk.IVFDotProductDistance,
	"DOT_PRODUCT": qrdbsdk.IVFDotProductDistance,
}

// parseIVFDistanceMetric converts a string metric to an IVF DistanceMetric.
func parseIVFDistanceMetric(metric string) qrdbsdk.IVFDistanceMetric {
	if dm, ok := ivfDistanceMetricMap[metric]; ok {
		return dm
	}
	return qrdbsdk.IVFL2Distance
}

// ivfFlatAdapter wraps a go-qrdb IVF index to satisfy VectorIndex.
// IVF indexes use string IDs internally, so this adapter converts
// uint64 IDs to strings and vice versa.
type ivfFlatAdapter struct {
	index      qrdbsdk.IVFIndex
	dimensions int
}

// Insert adds a vector with the given ID.
func (a *ivfFlatAdapter) Insert(id uint64, vector []float32) error {
	return a.index.Add(strconv.FormatUint(id, 10), vector, nil)
}

// Delete removes a vector by ID.
func (a *ivfFlatAdapter) Delete(id uint64) error {
	return a.index.Remove(strconv.FormatUint(id, 10))
}

// Search performs approximate nearest neighbor search using IVF.
func (a *ivfFlatAdapter) Search(query []float32, k int, params *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
	nprobe := 10 // default
	if params != nil && params.Nprobe > 0 {
		nprobe = params.Nprobe
	}

	results, err := a.index.Search(query, k, nprobe)
	if err != nil {
		return nil, err
	}

	converted := make([]*interfaces.SearchResult, len(results))
	for i, r := range results {
		parsedID, parseErr := strconv.ParseUint(r.ID, 10, 64)
		if parseErr != nil {
			continue
		}
		converted[i] = &interfaces.SearchResult{
			ID:       parsedID,
			Distance: r.Distance,
		}
	}

	return converted, nil
}

// Count returns the number of indexed vectors.
func (a *ivfFlatAdapter) Count() int64 {
	return int64(a.index.Size())
}

// GetStats returns index statistics.
func (a *ivfFlatAdapter) GetStats() *interfaces.IndexStats {
	return &interfaces.IndexStats{
		NodeCount:  int64(a.index.Size()),
		Dimensions: a.dimensions,
		IndexType:  interfaces.IndexTypeIVFFlat,
		IsTrained:  a.index.IsTrained(),
	}
}

// IndexType returns IndexTypeIVFFlat.
func (a *ivfFlatAdapter) IndexType() interfaces.IndexType {
	return interfaces.IndexTypeIVFFlat
}

// Train trains the IVF index on the provided vectors.
func (a *ivfFlatAdapter) Train(vectors [][]float32) error {
	return a.index.Train(vectors)
}

// IsTrained returns whether the IVF index has been trained.
func (a *ivfFlatAdapter) IsTrained() bool {
	return a.index.IsTrained()
}

// Close releases index resources. IVF indexes do not hold external resources.
func (a *ivfFlatAdapter) Close() error {
	return nil
}

// createIVFFlatIndex creates an IVF-Flat index wrapped in a VectorIndex adapter.
func createIVFFlatIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	nlist := config.Nlist
	if nlist <= 0 {
		nlist = 256
	}
	nprobe := config.Nprobe
	if nprobe <= 0 {
		nprobe = 10
	}
	dims := config.Dimensions
	if dims <= 0 {
		dims = 128 // reasonable default
	}

	ivfConfig := &qrdbsdk.IVFConfig{
		NumCentroids:   nlist,
		Dimensions:     dims,
		MaxIterations:  25,
		Convergence:    0.0001,
		InitMethod:     "kmeans++",
		DefaultNProbe:  nprobe,
		DistanceMetric: parseIVFDistanceMetric(config.DistanceMetric),
	}

	index, err := qrdbsdk.NewIVFIndex(ivfConfig)
	if err != nil {
		return nil, err
	}

	return &ivfFlatAdapter{
		index:      index,
		dimensions: dims,
	}, nil
}

// createIVFPQIndex creates an IVF-PQ index wrapped in a VectorIndex adapter.
// IVF-PQ uses the same IVF index implementation with quantization enabled.
func createIVFPQIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	nlist := config.Nlist
	if nlist <= 0 {
		nlist = 256
	}
	nprobe := config.Nprobe
	if nprobe <= 0 {
		nprobe = 10
	}
	dims := config.Dimensions
	if dims <= 0 {
		dims = 128
	}

	ivfConfig := &qrdbsdk.IVFConfig{
		NumCentroids:    nlist,
		Dimensions:      dims,
		MaxIterations:   25,
		Convergence:     0.0001,
		InitMethod:      "kmeans++",
		DefaultNProbe:   nprobe,
		UseQuantization: true,
		NumSubvectors:   8,
		NumBits:         8,
		DistanceMetric:  parseIVFDistanceMetric(config.DistanceMetric),
	}

	index, err := qrdbsdk.NewIVFIndex(ivfConfig)
	if err != nil {
		return nil, err
	}

	return &ivfFlatAdapter{
		index:      index,
		dimensions: dims,
	}, nil
}
