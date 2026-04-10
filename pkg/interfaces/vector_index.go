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

package interfaces

// IndexType represents the type of vector index.
type IndexType int

const (
	// IndexTypeFlat is a brute-force flat index providing 100% recall.
	IndexTypeFlat IndexType = iota
	// IndexTypeHNSW is a Hierarchical Navigable Small World graph index.
	IndexTypeHNSW
	// IndexTypeIVFFlat is an Inverted File index with flat quantization.
	IndexTypeIVFFlat
	// IndexTypeIVFPQ is an Inverted File index with product quantization.
	IndexTypeIVFPQ
	// IndexTypeGPUCAGRA is a GPU-accelerated CAGRA graph-based index.
	IndexTypeGPUCAGRA
	// IndexTypeGPUIVFFlat is a GPU-accelerated IVF-Flat index.
	IndexTypeGPUIVFFlat
	// IndexTypeGPUIVFPQ is a GPU-accelerated IVF-PQ index.
	IndexTypeGPUIVFPQ
	// IndexTypeGPUBruteForce is a GPU-accelerated brute-force index.
	IndexTypeGPUBruteForce
)

// indexTypeNames provides O(1) lookup from IndexType to string representation.
var indexTypeNames = map[IndexType]string{
	IndexTypeFlat:          "flat",
	IndexTypeHNSW:          "hnsw",
	IndexTypeIVFFlat:       "ivf_flat",
	IndexTypeIVFPQ:         "ivf_pq",
	IndexTypeGPUCAGRA:      "gpu_cagra",
	IndexTypeGPUIVFFlat:    "gpu_ivf_flat",
	IndexTypeGPUIVFPQ:      "gpu_ivf_pq",
	IndexTypeGPUBruteForce: "gpu_brute_force",
}

// indexTypeFromString provides O(1) lookup from string to IndexType.
var indexTypeFromString = map[string]IndexType{
	"flat":            IndexTypeFlat,
	"FLAT":            IndexTypeFlat,
	"hnsw":            IndexTypeHNSW,
	"HNSW":            IndexTypeHNSW,
	"ivf_flat":        IndexTypeIVFFlat,
	"IVF_FLAT":        IndexTypeIVFFlat,
	"ivf_pq":          IndexTypeIVFPQ,
	"IVF_PQ":          IndexTypeIVFPQ,
	"gpu_cagra":       IndexTypeGPUCAGRA,
	"GPU_CAGRA":       IndexTypeGPUCAGRA,
	"gpu_ivf_flat":    IndexTypeGPUIVFFlat,
	"GPU_IVF_FLAT":    IndexTypeGPUIVFFlat,
	"gpu_ivf_pq":      IndexTypeGPUIVFPQ,
	"GPU_IVF_PQ":      IndexTypeGPUIVFPQ,
	"gpu_brute_force": IndexTypeGPUBruteForce,
	"GPU_BRUTE_FORCE": IndexTypeGPUBruteForce,
}

// String returns the string representation of the IndexType.
func (t IndexType) String() string {
	if name, ok := indexTypeNames[t]; ok {
		return name
	}
	return "unknown"
}

// ParseIndexType converts a string to an IndexType.
// Returns IndexTypeHNSW for unknown values as a safe default.
func ParseIndexType(s string) IndexType {
	if t, ok := indexTypeFromString[s]; ok {
		return t
	}
	return IndexTypeHNSW
}

// IsGPUIndexType returns true if the index type requires GPU acceleration.
func IsGPUIndexType(t IndexType) bool {
	return t >= IndexTypeGPUCAGRA && t <= IndexTypeGPUBruteForce
}

// VectorIndex defines a generalized interface for vector indexes.
// All index backends (HNSW, FLAT, IVF) must satisfy this interface.
type VectorIndex interface {
	// Insert adds a vector with the given ID.
	Insert(id uint64, vector []float32) error

	// Delete removes a vector by ID.
	Delete(id uint64) error

	// Search performs k-nearest neighbor search.
	Search(query []float32, k int, params *SearchParams) ([]*SearchResult, error)

	// Count returns the number of indexed vectors.
	Count() int64

	// GetStats returns index statistics.
	GetStats() *IndexStats

	// IndexType returns the type of this index.
	IndexType() IndexType

	// Train trains the index (for IVF variants). No-op for HNSW/FLAT.
	Train(vectors [][]float32) error

	// IsTrained returns whether the index has been trained.
	IsTrained() bool

	// Close releases index resources.
	Close() error
}

// SearchParams holds index-type-specific search parameters.
type SearchParams struct {
	// EfSearch is HNSW search depth (only for HNSW).
	EfSearch int

	// Nprobe is the number of clusters to search (only for IVF).
	Nprobe int
}

// SearchResult represents a single vector search result.
type SearchResult struct {
	// ID is the unique identifier of the matched vector.
	ID uint64

	// Distance is the computed distance between the query and this result.
	Distance float32
}

// IndexStats contains statistics about an index.
type IndexStats struct {
	// NodeCount is the total number of vectors in the index.
	NodeCount int64

	// MaxLayer is the highest layer in the HNSW graph (HNSW-specific).
	MaxLayer int

	// AvgConnections is the average number of connections per node (HNSW-specific).
	AvgConnections float64

	// Dimensions is the dimensionality of vectors in the index.
	Dimensions int

	// IndexType is the type of this index.
	IndexType IndexType

	// IsTrained indicates whether the index has been trained (IVF-specific).
	IsTrained bool

	// MemoryBytes is an estimate of memory usage in bytes.
	MemoryBytes int64
}

// VectorIndexFactory creates VectorIndex instances based on configuration.
type VectorIndexFactory interface {
	// CreateIndex creates a new VectorIndex with the specified configuration.
	CreateIndex(config *VectorIndexConfig) (VectorIndex, error)
}

// VectorIndexConfig holds configuration for creating any index type.
type VectorIndexConfig struct {
	// IndexType specifies which index backend to create.
	IndexType IndexType

	// DistanceMetric specifies the distance function to use.
	// Common values: "l2", "cosine", "dot_product"
	DistanceMetric string

	// Dimensions is the dimensionality of vectors.
	Dimensions int

	// HNSW-specific parameters.

	// M is the maximum number of bidirectional connections per layer.
	M int

	// MMax is the maximum number of connections for layer 0.
	MMax int

	// EfConstruction is the size of the dynamic candidate list during construction.
	EfConstruction int

	// EfSearch is the size of the dynamic candidate list during search.
	EfSearch int

	// IVF-specific parameters.

	// Nlist is the number of clusters for IVF indexes.
	Nlist int

	// Nprobe is the default number of clusters to search for IVF indexes.
	Nprobe int

	// Common parameters.

	// MaxElements is the maximum number of vectors the index can hold.
	MaxElements int
}

// DefaultVectorIndexConfig returns a default VectorIndex configuration
// that creates an HNSW index with standard parameters.
func DefaultVectorIndexConfig() *VectorIndexConfig {
	return &VectorIndexConfig{
		IndexType:      IndexTypeHNSW,
		DistanceMetric: "l2",
		M:              16,
		MMax:           32,
		EfConstruction: 200,
		EfSearch:       50,
		MaxElements:    1000000,
	}
}
