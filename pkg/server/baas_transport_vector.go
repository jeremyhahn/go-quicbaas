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
	"container/heap"
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Key prefixes for vector data stored in the KV store.
const (
	vecNSPrefix   = "vec:ns:"
	vecDataPrefix = "vec:data:"
)

// vecNamespaceMeta is the JSON-serializable representation of a vector
// namespace stored in the KV store.
type vecNamespaceMeta struct {
	Namespace      string                 `json:"namespace"`
	Dimension      uint32                 `json:"dimension"`
	DistanceMetric string                 `json:"distance_metric"`
	IndexType      string                 `json:"index_type"`
	IndexConfig    map[string]string      `json:"index_config,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      int64                  `json:"created_at"`
	UpdatedAt      int64                  `json:"updated_at"`
}

// vecEncodedVector is the JSON-serializable representation of a vector
// stored in the KV store.
type vecEncodedVector struct {
	ID        uint64                 `json:"id"`
	Vector    []float32              `json:"vector"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Version   uint64                 `json:"version"`
	CreatedAt int64                  `json:"created_at"`
	UpdatedAt int64                  `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Vector error types
// ---------------------------------------------------------------------------

// VectorNamespaceExistsError indicates the namespace already exists.
type VectorNamespaceExistsError struct {
	Namespace string
}

// Error implements the error interface.
func (e *VectorNamespaceExistsError) Error() string {
	return "vector namespace already exists: " + e.Namespace
}

// VectorNamespaceNotFoundError indicates the namespace was not found.
type VectorNamespaceNotFoundError struct {
	Namespace string
}

// Error implements the error interface.
func (e *VectorNamespaceNotFoundError) Error() string {
	return "vector namespace not found: " + e.Namespace
}

// VectorNotFoundError indicates the vector was not found.
type VectorNotFoundError struct {
	Namespace string
	ID        uint64
}

// Error implements the error interface.
func (e *VectorNotFoundError) Error() string {
	return "vector not found: " + e.Namespace + "/" + strconv.FormatUint(e.ID, 10)
}

// VectorDimensionMismatchError indicates the vector dimension does not match
// the namespace dimension.
type VectorDimensionMismatchError struct {
	Expected uint32
	Got      uint32
}

// Error implements the error interface.
func (e *VectorDimensionMismatchError) Error() string {
	return "vector dimension mismatch: expected " +
		strconv.FormatUint(uint64(e.Expected), 10) +
		" got " + strconv.FormatUint(uint64(e.Got), 10)
}

// VectorNilRequestError indicates a nil request was provided.
type VectorNilRequestError struct {
	Operation string
}

// Error implements the error interface.
func (e *VectorNilRequestError) Error() string {
	return "nil request for vector operation: " + e.Operation
}

// ---------------------------------------------------------------------------
// Distance computation
// ---------------------------------------------------------------------------

// distanceFunc computes the distance between two vectors.
type distanceFunc func(a, b []float32) float32

// distanceFuncs maps distance metric names to their computation functions.
var distanceFuncs = map[string]distanceFunc{
	transport.DistanceCosine:     vecCosineDistance,
	transport.DistanceEuclidean:  vecEuclideanDistance,
	transport.DistanceDotProduct: vecDotProductDistance,
}

// vecCosineDistance computes cosine distance (1 - cosine similarity).
func vecCosineDistance(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 1.0
	}
	return float32(1.0 - dot/denom)
}

// vecEuclideanDistance computes L2 (Euclidean) distance.
func vecEuclideanDistance(a, b []float32) float32 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return float32(math.Sqrt(sum))
}

// vecDotProductDistance computes negative dot product distance
// (lower is more similar).
func vecDotProductDistance(a, b []float32) float32 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return float32(-dot)
}

// ---------------------------------------------------------------------------
// Min-heap for top-K search results
// ---------------------------------------------------------------------------

// vecSearchHeap is a max-heap used for tracking the top-K nearest neighbors.
// We use a max-heap so we can efficiently evict the farthest candidate
// when we find a closer one.
type vecSearchHeap []vecSearchEntry

type vecSearchEntry struct {
	id       uint64
	distance float32
	vector   []float32
	metadata map[string]interface{}
}

func (h vecSearchHeap) Len() int           { return len(h) }
func (h vecSearchHeap) Less(i, j int) bool { return h[i].distance > h[j].distance } // max-heap
func (h vecSearchHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *vecSearchHeap) Push(x interface{}) {
	entry, ok := x.(vecSearchEntry)
	if !ok {
		return
	}
	*h = append(*h, entry)
}

func (h *vecSearchHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// ---------------------------------------------------------------------------
// transport.VectorClient - implementations with vector storage delegation
// ---------------------------------------------------------------------------

// CreateNamespace creates a new vector namespace. When vectorStorage is
// available, the namespace is created in the real PebbleVectorStorage.
// Otherwise, it falls back to the KV store.
func (t *baasTransport) CreateNamespace(ctx context.Context, req *transport.CreateNamespaceRequest) (*transport.CreateNamespaceResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "CreateNamespace"}
	}

	if t.vectorStorage != nil {
		return t.createNamespaceViaStorage(ctx, req)
	}
	return t.createNamespaceViaKV(ctx, req)
}

// createNamespaceViaStorage delegates namespace creation to the real
// vector storage backend.
func (t *baasTransport) createNamespaceViaStorage(ctx context.Context, req *transport.CreateNamespaceRequest) (*transport.CreateNamespaceResponse, error) {
	now := time.Now().UnixNano()

	// Convert transport metadata (map[string]interface{}) to storage metadata (map[string]string).
	var storageMeta map[string]string
	if len(req.Metadata) > 0 {
		storageMeta = vecMetadataToStringMap(req.Metadata)
	}

	info := &interfaces.VectorNamespaceInfo{
		Name:           req.Namespace,
		Dimension:      req.Dimension,
		DistanceMetric: req.DistanceMetric,
		IndexType:      req.IndexType,
		IndexConfig:    req.IndexConfig,
		Metadata:       storageMeta,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := t.vectorStorage.CreateNamespace(ctx, info); err != nil {
		return nil, err
	}

	return &transport.CreateNamespaceResponse{
		Namespace: req.Namespace,
		Dimension: req.Dimension,
		Timestamp: now,
		Success:   true,
	}, nil
}

// createNamespaceViaKV creates a namespace in the KV store (fallback).
func (t *baasTransport) createNamespaceViaKV(ctx context.Context, req *transport.CreateNamespaceRequest) (*transport.CreateNamespaceResponse, error) {
	key := vecNSPrefix + req.Namespace
	exists, err := t.store.Exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &VectorNamespaceExistsError{Namespace: req.Namespace}
	}

	now := time.Now().UnixNano()
	meta := vecNamespaceMeta{
		Namespace:      req.Namespace,
		Dimension:      req.Dimension,
		DistanceMetric: req.DistanceMetric,
		IndexType:      req.IndexType,
		IndexConfig:    req.IndexConfig,
		Metadata:       req.Metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	data, err := json.Marshal(&meta)
	if err != nil {
		return nil, err
	}

	if err := t.store.Put(ctx, key, data); err != nil {
		return nil, err
	}

	return &transport.CreateNamespaceResponse{
		Namespace: req.Namespace,
		Dimension: req.Dimension,
		Timestamp: now,
		Success:   true,
	}, nil
}

// DeleteNamespace deletes a namespace and all its vectors. When vectorStorage
// is available, deletion is delegated to the real storage backend.
func (t *baasTransport) DeleteNamespace(ctx context.Context, req *transport.DeleteNamespaceRequest) (*transport.DeleteNamespaceResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "DeleteNamespace"}
	}

	if t.vectorStorage != nil {
		return t.deleteNamespaceViaStorage(ctx, req)
	}
	return t.deleteNamespaceViaKV(ctx, req)
}

// deleteNamespaceViaStorage delegates namespace deletion to the real
// vector storage backend.
func (t *baasTransport) deleteNamespaceViaStorage(ctx context.Context, req *transport.DeleteNamespaceRequest) (*transport.DeleteNamespaceResponse, error) {
	// Get vector count before deletion for the response.
	stats, statsErr := t.vectorStorage.GetStats(ctx, req.Namespace)
	var vectorCount uint64
	if statsErr == nil && stats != nil {
		vectorCount = stats.VectorCount
	}

	if err := t.vectorStorage.DeleteNamespace(ctx, req.Namespace); err != nil {
		return nil, err
	}

	return &transport.DeleteNamespaceResponse{
		Success:        true,
		VectorsDeleted: vectorCount,
		Timestamp:      time.Now().UnixNano(),
	}, nil
}

// deleteNamespaceViaKV deletes a namespace from the KV store (fallback).
func (t *baasTransport) deleteNamespaceViaKV(ctx context.Context, req *transport.DeleteNamespaceRequest) (*transport.DeleteNamespaceResponse, error) {
	nsKey := vecNSPrefix + req.Namespace
	exists, err := t.store.Exists(ctx, nsKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &VectorNamespaceNotFoundError{Namespace: req.Namespace}
	}

	// Delete all vectors in the namespace.
	dataPrefix := vecDataPrefix + req.Namespace + ":"
	var keysToDelete []string
	if err := t.store.Scan(ctx, dataPrefix, func(key string, _ []byte) error {
		keysToDelete = append(keysToDelete, key)
		return nil
	}); err != nil {
		return nil, err
	}

	for _, key := range keysToDelete {
		if err := t.store.Delete(ctx, key); err != nil {
			return nil, err
		}
	}

	// Delete the namespace metadata.
	if err := t.store.Delete(ctx, nsKey); err != nil {
		return nil, err
	}

	return &transport.DeleteNamespaceResponse{
		Success:        true,
		VectorsDeleted: uint64(len(keysToDelete)),
		Timestamp:      time.Now().UnixNano(),
	}, nil
}

// ListNamespaces lists vector namespaces matching the given prefix.
// When vectorStorage is available, it delegates to the real storage backend.
func (t *baasTransport) ListNamespaces(ctx context.Context, req *transport.ListNamespacesRequest) (*transport.ListNamespacesResponse, error) {
	if req == nil {
		return &transport.ListNamespacesResponse{}, nil
	}

	if t.vectorStorage != nil {
		return t.listNamespacesViaStorage(ctx, req)
	}
	return t.listNamespacesViaKV(ctx, req)
}

// listNamespacesViaStorage delegates namespace listing to the real
// vector storage backend.
func (t *baasTransport) listNamespacesViaStorage(ctx context.Context, req *transport.ListNamespacesRequest) (*transport.ListNamespacesResponse, error) {
	infos, err := t.vectorStorage.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	// Apply prefix filter.
	var namespaces []*transport.NamespaceInfo
	for i := range infos {
		if req.Prefix != "" && !strings.HasPrefix(infos[i].Name, req.Prefix) {
			continue
		}
		namespaces = append(namespaces, &transport.NamespaceInfo{
			Namespace:      infos[i].Name,
			Dimension:      infos[i].Dimension,
			DistanceMetric: infos[i].DistanceMetric,
			IndexType:      infos[i].IndexType,
			IndexConfig:    infos[i].IndexConfig,
			CreatedAt:      infos[i].CreatedAt,
			UpdatedAt:      infos[i].UpdatedAt,
		})
	}

	totalCount := len(namespaces)

	// Apply offset.
	if req.Offset > 0 && req.Offset < len(namespaces) {
		namespaces = namespaces[req.Offset:]
	} else if req.Offset >= len(namespaces) {
		namespaces = nil
	}

	// Apply limit.
	if req.Limit > 0 && req.Limit < len(namespaces) {
		namespaces = namespaces[:req.Limit]
	}

	return &transport.ListNamespacesResponse{
		Namespaces: namespaces,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// listNamespacesViaKV lists namespaces from the KV store (fallback).
func (t *baasTransport) listNamespacesViaKV(ctx context.Context, req *transport.ListNamespacesRequest) (*transport.ListNamespacesResponse, error) {
	prefix := vecNSPrefix
	if req.Prefix != "" {
		prefix = vecNSPrefix + req.Prefix
	}

	var namespaces []*transport.NamespaceInfo
	if err := t.store.Scan(ctx, prefix, func(_ string, value []byte) error {
		var meta vecNamespaceMeta
		if err := json.Unmarshal(value, &meta); err != nil {
			return nil // skip corrupt entries
		}
		namespaces = append(namespaces, &transport.NamespaceInfo{
			Namespace:      meta.Namespace,
			Dimension:      meta.Dimension,
			DistanceMetric: meta.DistanceMetric,
			IndexType:      meta.IndexType,
			IndexConfig:    meta.IndexConfig,
			CreatedAt:      meta.CreatedAt,
			UpdatedAt:      meta.UpdatedAt,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	totalCount := len(namespaces)

	// Apply offset.
	if req.Offset > 0 && req.Offset < len(namespaces) {
		namespaces = namespaces[req.Offset:]
	} else if req.Offset >= len(namespaces) {
		namespaces = nil
	}

	// Apply limit.
	if req.Limit > 0 && req.Limit < len(namespaces) {
		namespaces = namespaces[:req.Limit]
	}

	return &transport.ListNamespacesResponse{
		Namespaces: namespaces,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// GetNamespaceInfo retrieves metadata for a vector namespace. When
// vectorStorage is available, it delegates to the real storage backend.
func (t *baasTransport) GetNamespaceInfo(ctx context.Context, req *transport.GetNamespaceInfoRequest) (*transport.GetNamespaceInfoResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "GetNamespaceInfo"}
	}

	if t.vectorStorage != nil {
		return t.getNamespaceInfoViaStorage(ctx, req)
	}
	return t.getNamespaceInfoViaKV(ctx, req)
}

// getNamespaceInfoViaStorage delegates namespace info retrieval to the
// real vector storage backend.
func (t *baasTransport) getNamespaceInfoViaStorage(ctx context.Context, req *transport.GetNamespaceInfoRequest) (*transport.GetNamespaceInfoResponse, error) {
	info, err := t.vectorStorage.GetNamespaceInfo(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	// Convert storage metadata (map[string]string) to transport metadata (map[string]interface{}).
	var meta map[string]interface{}
	if len(info.Metadata) > 0 {
		meta = make(map[string]interface{}, len(info.Metadata))
		for k, v := range info.Metadata {
			meta[k] = v
		}
	}

	return &transport.GetNamespaceInfoResponse{
		Namespace:      info.Name,
		Dimension:      info.Dimension,
		VectorCount:    info.VectorCount,
		DistanceMetric: info.DistanceMetric,
		IndexType:      info.IndexType,
		IndexConfig:    info.IndexConfig,
		Metadata:       meta,
		CreatedAt:      info.CreatedAt,
		UpdatedAt:      info.UpdatedAt,
	}, nil
}

// getNamespaceInfoViaKV retrieves namespace info from the KV store (fallback).
func (t *baasTransport) getNamespaceInfoViaKV(ctx context.Context, req *transport.GetNamespaceInfoRequest) (*transport.GetNamespaceInfoResponse, error) {
	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	// Count vectors in the namespace.
	vectorCount, err := t.vecCountVectors(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	return &transport.GetNamespaceInfoResponse{
		Namespace:      meta.Namespace,
		Dimension:      meta.Dimension,
		VectorCount:    vectorCount,
		DistanceMetric: meta.DistanceMetric,
		IndexType:      meta.IndexType,
		IndexConfig:    meta.IndexConfig,
		Metadata:       meta.Metadata,
		CreatedAt:      meta.CreatedAt,
		UpdatedAt:      meta.UpdatedAt,
	}, nil
}

// UpsertVector inserts or updates a vector. When vectorStorage is
// available, the vector is written to the real storage backend.
func (t *baasTransport) UpsertVector(ctx context.Context, req *transport.UpsertVectorRequest) (*transport.UpsertVectorResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "UpsertVector"}
	}

	if t.vectorStorage != nil {
		return t.upsertVectorViaStorage(ctx, req)
	}
	return t.upsertVectorViaKV(ctx, req)
}

// upsertVectorViaStorage delegates vector upsert to the real vector
// storage backend.
func (t *baasTransport) upsertVectorViaStorage(ctx context.Context, req *transport.UpsertVectorRequest) (*transport.UpsertVectorResponse, error) {
	now := time.Now().UnixNano()

	// Convert transport metadata (map[string]interface{}) to storage metadata (map[string]string).
	var storageMeta map[string]string
	if len(req.Metadata) > 0 {
		storageMeta = vecMetadataToStringMap(req.Metadata)
	}

	// Check for existing vector to determine version.
	var version uint64 = 1
	createdAt := now
	existing, getErr := t.vectorStorage.GetVector(ctx, req.Namespace, req.ID)
	if getErr == nil && existing != nil {
		version = existing.Version + 1
		createdAt = existing.CreatedAt
	}

	vec := &interfaces.VectorValue{
		ID:        req.ID,
		Embedding: req.Vector,
		Metadata:  storageMeta,
		Version:   version,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}

	if err := t.vectorStorage.UpsertVector(ctx, req.Namespace, vec); err != nil {
		return nil, err
	}

	return &transport.UpsertVectorResponse{
		ID:        req.ID,
		Version:   version,
		Timestamp: now,
		Success:   true,
	}, nil
}

// upsertVectorViaKV writes a vector to the KV store (fallback).
func (t *baasTransport) upsertVectorViaKV(ctx context.Context, req *transport.UpsertVectorRequest) (*transport.UpsertVectorResponse, error) {
	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	if uint32(len(req.Vector)) != meta.Dimension {
		return nil, &VectorDimensionMismatchError{
			Expected: meta.Dimension,
			Got:      uint32(len(req.Vector)),
		}
	}

	now := time.Now().UnixNano()
	key := vecBuildDataKey(req.Namespace, req.ID)

	// Check if vector already exists for version tracking.
	var version uint64 = 1
	createdAt := now
	if existing, getErr := t.store.Get(ctx, key); getErr == nil {
		var prev vecEncodedVector
		if jsonErr := json.Unmarshal(existing, &prev); jsonErr == nil {
			version = prev.Version + 1
			createdAt = prev.CreatedAt
		}
	}

	encoded := vecEncodedVector{
		ID:        req.ID,
		Vector:    req.Vector,
		Metadata:  req.Metadata,
		Version:   version,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}

	data, err := json.Marshal(&encoded)
	if err != nil {
		return nil, err
	}

	if err := t.store.Put(ctx, key, data); err != nil {
		return nil, err
	}

	return &transport.UpsertVectorResponse{
		ID:        req.ID,
		Version:   version,
		Timestamp: now,
		Success:   true,
	}, nil
}

// GetVector retrieves a single vector. When vectorStorage is available,
// it reads from the real storage backend.
func (t *baasTransport) GetVector(ctx context.Context, req *transport.GetVectorRequest) (*transport.GetVectorResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "GetVector"}
	}

	if t.vectorStorage != nil {
		return t.getVectorViaStorage(ctx, req)
	}
	return t.getVectorViaKV(ctx, req)
}

// getVectorViaStorage delegates vector retrieval to the real vector
// storage backend.
func (t *baasTransport) getVectorViaStorage(ctx context.Context, req *transport.GetVectorRequest) (*transport.GetVectorResponse, error) {
	vec, err := t.vectorStorage.GetVector(ctx, req.Namespace, req.ID)
	if err != nil || vec == nil {
		return &transport.GetVectorResponse{
			ID:    req.ID,
			Found: false,
		}, nil
	}

	resp := &transport.GetVectorResponse{
		ID:        vec.ID,
		Version:   vec.Version,
		CreatedAt: vec.CreatedAt,
		UpdatedAt: vec.UpdatedAt,
		Found:     true,
	}

	if req.IncludeVector {
		resp.Vector = vec.Embedding
	}
	if req.IncludeMetadata {
		resp.Metadata = vecStringMapToMetadata(vec.Metadata)
	}

	return resp, nil
}

// getVectorViaKV retrieves a vector from the KV store (fallback).
func (t *baasTransport) getVectorViaKV(ctx context.Context, req *transport.GetVectorRequest) (*transport.GetVectorResponse, error) {
	key := vecBuildDataKey(req.Namespace, req.ID)
	data, err := t.store.Get(ctx, key)
	if err != nil {
		return &transport.GetVectorResponse{
			ID:    req.ID,
			Found: false,
		}, nil
	}

	var encoded vecEncodedVector
	if err := json.Unmarshal(data, &encoded); err != nil {
		return &transport.GetVectorResponse{
			ID:    req.ID,
			Found: false,
		}, nil
	}

	resp := &transport.GetVectorResponse{
		ID:        encoded.ID,
		Version:   encoded.Version,
		CreatedAt: encoded.CreatedAt,
		UpdatedAt: encoded.UpdatedAt,
		Found:     true,
	}

	if req.IncludeVector {
		resp.Vector = encoded.Vector
	}
	if req.IncludeMetadata {
		resp.Metadata = encoded.Metadata
	}

	return resp, nil
}

// DeleteVector deletes a single vector. When vectorStorage is available,
// it deletes from the real storage backend.
func (t *baasTransport) DeleteVector(ctx context.Context, req *transport.DeleteVectorRequest) (*transport.DeleteVectorResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "DeleteVector"}
	}

	if t.vectorStorage != nil {
		return t.deleteVectorViaStorage(ctx, req)
	}
	return t.deleteVectorViaKV(ctx, req)
}

// deleteVectorViaStorage delegates vector deletion to the real vector
// storage backend.
func (t *baasTransport) deleteVectorViaStorage(ctx context.Context, req *transport.DeleteVectorRequest) (*transport.DeleteVectorResponse, error) {
	if err := t.vectorStorage.DeleteVector(ctx, req.Namespace, req.ID); err != nil {
		return &transport.DeleteVectorResponse{
			Success:   false,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	return &transport.DeleteVectorResponse{
		Success:   true,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// deleteVectorViaKV deletes a vector from the KV store (fallback).
func (t *baasTransport) deleteVectorViaKV(ctx context.Context, req *transport.DeleteVectorRequest) (*transport.DeleteVectorResponse, error) {
	key := vecBuildDataKey(req.Namespace, req.ID)
	if err := t.store.Delete(ctx, key); err != nil {
		return &transport.DeleteVectorResponse{
			Success:   false,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	return &transport.DeleteVectorResponse{
		Success:   true,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// BatchUpsert inserts or updates multiple vectors. When vectorStorage is
// available, it delegates to the real storage backend.
func (t *baasTransport) BatchUpsert(ctx context.Context, req *transport.BatchUpsertRequest) (*transport.BatchUpsertResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "BatchUpsert"}
	}

	if t.vectorStorage != nil {
		return t.batchUpsertViaStorage(ctx, req)
	}
	return t.batchUpsertViaKV(ctx, req)
}

// batchUpsertViaStorage delegates batch upsert to the real vector
// storage backend.
func (t *baasTransport) batchUpsertViaStorage(ctx context.Context, req *transport.BatchUpsertRequest) (*transport.BatchUpsertResponse, error) {
	now := time.Now().UnixNano()
	vecs := make([]*interfaces.VectorValue, 0, len(req.Vectors))
	ids := make([]uint64, 0, len(req.Vectors))
	versions := make([]uint64, 0, len(req.Vectors))

	for _, v := range req.Vectors {
		var version uint64 = 1
		createdAt := now

		// Check for existing vector to determine version.
		existing, getErr := t.vectorStorage.GetVector(ctx, req.Namespace, v.ID)
		if getErr == nil && existing != nil {
			version = existing.Version + 1
			createdAt = existing.CreatedAt
		}

		var storageMeta map[string]string
		if len(v.Metadata) > 0 {
			storageMeta = vecMetadataToStringMap(v.Metadata)
		}

		vecs = append(vecs, &interfaces.VectorValue{
			ID:        v.ID,
			Embedding: v.Vector,
			Metadata:  storageMeta,
			Version:   version,
			CreatedAt: createdAt,
			UpdatedAt: now,
		})
		ids = append(ids, v.ID)
		versions = append(versions, version)
	}

	if err := t.vectorStorage.BatchUpsertVectors(ctx, req.Namespace, vecs); err != nil {
		return nil, err
	}

	return &transport.BatchUpsertResponse{
		IDs:           ids,
		Versions:      versions,
		Timestamp:     time.Now().UnixNano(),
		Success:       true,
		InsertedCount: len(req.Vectors),
	}, nil
}

// batchUpsertViaKV writes multiple vectors to the KV store (fallback).
func (t *baasTransport) batchUpsertViaKV(ctx context.Context, req *transport.BatchUpsertRequest) (*transport.BatchUpsertResponse, error) {
	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(req.Vectors))
	versions := make([]uint64, 0, len(req.Vectors))

	for _, vec := range req.Vectors {
		if uint32(len(vec.Vector)) != meta.Dimension {
			return nil, &VectorDimensionMismatchError{
				Expected: meta.Dimension,
				Got:      uint32(len(vec.Vector)),
			}
		}

		resp, upsertErr := t.upsertVectorViaKV(ctx, &transport.UpsertVectorRequest{
			Namespace: req.Namespace,
			ID:        vec.ID,
			Vector:    vec.Vector,
			Metadata:  vec.Metadata,
		})
		if upsertErr != nil {
			return nil, upsertErr
		}

		ids = append(ids, resp.ID)
		versions = append(versions, resp.Version)
	}

	return &transport.BatchUpsertResponse{
		IDs:           ids,
		Versions:      versions,
		Timestamp:     time.Now().UnixNano(),
		Success:       true,
		InsertedCount: len(req.Vectors),
	}, nil
}

// BatchDeleteVectors deletes multiple vectors. When vectorStorage is
// available, it delegates to the real storage backend.
func (t *baasTransport) BatchDeleteVectors(ctx context.Context, req *transport.BatchDeleteVectorsRequest) (*transport.BatchDeleteVectorsResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "BatchDeleteVectors"}
	}

	if t.vectorStorage != nil {
		return t.batchDeleteVectorsViaStorage(ctx, req)
	}
	return t.batchDeleteVectorsViaKV(ctx, req)
}

// batchDeleteVectorsViaStorage delegates batch deletion to the real
// vector storage backend.
func (t *baasTransport) batchDeleteVectorsViaStorage(ctx context.Context, req *transport.BatchDeleteVectorsRequest) (*transport.BatchDeleteVectorsResponse, error) {
	if err := t.vectorStorage.BatchDeleteVectors(ctx, req.Namespace, req.IDs); err != nil {
		return nil, err
	}

	return &transport.BatchDeleteVectorsResponse{
		DeletedCount: len(req.IDs),
		Timestamp:    time.Now().UnixNano(),
		Success:      true,
	}, nil
}

// batchDeleteVectorsViaKV deletes multiple vectors from the KV store (fallback).
func (t *baasTransport) batchDeleteVectorsViaKV(ctx context.Context, req *transport.BatchDeleteVectorsRequest) (*transport.BatchDeleteVectorsResponse, error) {
	var deletedCount int
	var failedIDs []uint64

	for _, id := range req.IDs {
		key := vecBuildDataKey(req.Namespace, id)
		if err := t.store.Delete(ctx, key); err != nil {
			failedIDs = append(failedIDs, id)
			continue
		}
		deletedCount++
	}

	return &transport.BatchDeleteVectorsResponse{
		DeletedCount: deletedCount,
		FailedIDs:    failedIDs,
		Timestamp:    time.Now().UnixNano(),
		Success:      len(failedIDs) == 0,
	}, nil
}

// ListVectors returns a paginated list of vectors in a namespace. When
// vectorStorage is available, it reads from the real storage backend.
func (t *baasTransport) ListVectors(ctx context.Context, req *transport.ListVectorsRequest) (*transport.ListVectorsResponse, error) {
	if req == nil {
		return &transport.ListVectorsResponse{}, nil
	}

	if t.vectorStorage != nil {
		return t.listVectorsViaStorage(ctx, req)
	}
	return t.listVectorsViaKV(ctx, req)
}

// listVectorsViaStorage delegates vector listing to the real vector
// storage backend.
func (t *baasTransport) listVectorsViaStorage(ctx context.Context, req *transport.ListVectorsRequest) (*transport.ListVectorsResponse, error) {
	opts := &interfaces.VectorScanOptions{
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
	}
	if req.Limit > 0 {
		opts.Limit = req.Limit
	}

	vecs, err := t.vectorStorage.ScanNamespace(ctx, req.Namespace, opts)
	if err != nil {
		return nil, err
	}

	vectors := make([]*transport.VectorData, 0, len(vecs))
	for _, vec := range vecs {
		vd := &transport.VectorData{
			ID:        vec.ID,
			Version:   vec.Version,
			CreatedAt: vec.CreatedAt,
			UpdatedAt: vec.UpdatedAt,
		}
		if req.IncludeVectors {
			vd.Vector = vec.Embedding
		}
		if req.IncludeMetadata {
			vd.Metadata = vecStringMapToMetadata(vec.Metadata)
		}
		vectors = append(vectors, vd)
	}

	totalCount := uint64(len(vectors))

	// Compute next cursor.
	var nextCursor string
	if req.Limit > 0 && req.Limit < len(vectors) {
		lastID := vectors[req.Limit-1].ID
		nextCursor = vecBuildDataKey(req.Namespace, lastID)
		vectors = vectors[:req.Limit]
	}

	return &transport.ListVectorsResponse{
		Vectors:    vectors,
		NextCursor: nextCursor,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// listVectorsViaKV lists vectors from the KV store (fallback).
func (t *baasTransport) listVectorsViaKV(ctx context.Context, req *transport.ListVectorsRequest) (*transport.ListVectorsResponse, error) {
	dataPrefix := vecDataPrefix + req.Namespace + ":"
	var vectors []*transport.VectorData
	var pastCursor bool

	// When no cursor is set, start from the beginning.
	if req.Cursor == "" {
		pastCursor = true
	}

	if err := t.store.Scan(ctx, dataPrefix, func(key string, value []byte) error {
		// Handle cursor-based pagination.
		if !pastCursor {
			if key == req.Cursor {
				pastCursor = true
			}
			return nil
		}

		var encoded vecEncodedVector
		if err := json.Unmarshal(value, &encoded); err != nil {
			return nil // skip corrupt entries
		}

		vd := &transport.VectorData{
			ID:        encoded.ID,
			Version:   encoded.Version,
			CreatedAt: encoded.CreatedAt,
			UpdatedAt: encoded.UpdatedAt,
		}

		if req.IncludeVectors {
			vd.Vector = encoded.Vector
		}
		if req.IncludeMetadata {
			vd.Metadata = encoded.Metadata
		}

		vectors = append(vectors, vd)
		return nil
	}); err != nil {
		return nil, err
	}

	totalCount := uint64(len(vectors))

	// Apply limit and compute next cursor.
	var nextCursor string
	if req.Limit > 0 && req.Limit < len(vectors) {
		lastID := vectors[req.Limit-1].ID
		nextCursor = vecBuildDataKey(req.Namespace, lastID)
		vectors = vectors[:req.Limit]
	}

	return &transport.ListVectorsResponse{
		Vectors:    vectors,
		NextCursor: nextCursor,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// UpdateMetadata updates the metadata of an existing vector. When
// vectorStorage is available, it reads from and writes to the real
// storage backend.
func (t *baasTransport) UpdateMetadata(ctx context.Context, req *transport.UpdateVectorMetadataRequest) (*transport.UpdateVectorMetadataResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "UpdateMetadata"}
	}

	if t.vectorStorage != nil {
		return t.updateMetadataViaStorage(ctx, req)
	}
	return t.updateMetadataViaKV(ctx, req)
}

// updateMetadataViaStorage delegates metadata update to the real vector
// storage backend.
func (t *baasTransport) updateMetadataViaStorage(ctx context.Context, req *transport.UpdateVectorMetadataRequest) (*transport.UpdateVectorMetadataResponse, error) {
	vec, err := t.vectorStorage.GetVector(ctx, req.Namespace, req.ID)
	if err != nil || vec == nil {
		return nil, &VectorNotFoundError{Namespace: req.Namespace, ID: req.ID}
	}

	now := time.Now().UnixNano()
	vec.Version++
	vec.UpdatedAt = now

	// Convert request metadata to string map.
	newMeta := vecMetadataToStringMap(req.Metadata)

	if req.MergeMetadata && vec.Metadata != nil {
		for k, v := range newMeta {
			vec.Metadata[k] = v
		}
	} else {
		vec.Metadata = newMeta
	}

	// Delete specified keys.
	for _, delKey := range req.DeleteKeys {
		delete(vec.Metadata, delKey)
	}

	if err := t.vectorStorage.UpsertVector(ctx, req.Namespace, vec); err != nil {
		return nil, err
	}

	return &transport.UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   vec.Version,
		Timestamp: now,
		Success:   true,
	}, nil
}

// updateMetadataViaKV updates vector metadata in the KV store (fallback).
func (t *baasTransport) updateMetadataViaKV(ctx context.Context, req *transport.UpdateVectorMetadataRequest) (*transport.UpdateVectorMetadataResponse, error) {
	key := vecBuildDataKey(req.Namespace, req.ID)
	data, err := t.store.Get(ctx, key)
	if err != nil {
		return nil, &VectorNotFoundError{Namespace: req.Namespace, ID: req.ID}
	}

	var encoded vecEncodedVector
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil, &VectorNotFoundError{Namespace: req.Namespace, ID: req.ID}
	}

	now := time.Now().UnixNano()
	encoded.Version++
	encoded.UpdatedAt = now

	if req.MergeMetadata && encoded.Metadata != nil {
		// Merge new metadata into existing.
		for k, v := range req.Metadata {
			encoded.Metadata[k] = v
		}
	} else {
		encoded.Metadata = req.Metadata
	}

	// Delete specified keys.
	for _, delKey := range req.DeleteKeys {
		delete(encoded.Metadata, delKey)
	}

	updated, err := json.Marshal(&encoded)
	if err != nil {
		return nil, err
	}

	if err := t.store.Put(ctx, key, updated); err != nil {
		return nil, err
	}

	return &transport.UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   encoded.Version,
		Timestamp: now,
		Success:   true,
	}, nil
}

// SearchSimilar performs a brute-force k-NN search across all vectors in
// the namespace using the configured distance metric. Search operations
// always use the KV fallback since the real vector storage handles search
// through the HNSW index layer above.
func (t *baasTransport) SearchSimilar(ctx context.Context, req *transport.SearchSimilarRequest) (*transport.SearchSimilarResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "SearchSimilar"}
	}

	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	distFn := vecResolveDistanceFunc(meta.DistanceMetric)
	startTime := time.Now()

	k := req.K
	if k <= 0 {
		k = 10
	}

	h := &vecSearchHeap{}
	heap.Init(h)

	dataPrefix := vecDataPrefix + req.Namespace + ":"
	if err := t.store.Scan(ctx, dataPrefix, func(_ string, value []byte) error {
		var encoded vecEncodedVector
		if err := json.Unmarshal(value, &encoded); err != nil {
			return nil
		}

		// Apply metadata filter.
		if len(req.MetadataFilter) > 0 && !vecMetadataMatches(encoded.Metadata, req.MetadataFilter) {
			return nil
		}

		dist := distFn(req.QueryVector, encoded.Vector)

		// Apply minimum score filter.
		if req.MinScore > 0 && vecDistanceToScore(dist) < req.MinScore {
			return nil
		}

		entry := vecSearchEntry{
			id:       encoded.ID,
			distance: dist,
			vector:   encoded.Vector,
			metadata: encoded.Metadata,
		}

		if h.Len() < k {
			heap.Push(h, entry)
		} else if dist < (*h)[0].distance {
			(*h)[0] = entry
			heap.Fix(h, 0)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	results := vecHeapToResults(h, req.IncludeVectors, req.IncludeMetadata)
	duration := time.Since(startTime).Nanoseconds()

	return &transport.SearchSimilarResponse{
		Results:       results,
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: duration,
	}, nil
}

// SearchByMetadata searches for vectors matching the given metadata filters.
func (t *baasTransport) SearchByMetadata(ctx context.Context, req *transport.SearchByMetadataRequest) (*transport.SearchByMetadataResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "SearchByMetadata"}
	}

	dataPrefix := vecDataPrefix + req.Namespace + ":"
	var results []*transport.VectorData

	useOR := strings.EqualFold(req.FilterOperator, "or")

	if err := t.store.Scan(ctx, dataPrefix, func(_ string, value []byte) error {
		var encoded vecEncodedVector
		if err := json.Unmarshal(value, &encoded); err != nil {
			return nil
		}

		var matches bool
		if useOR {
			matches = vecMetadataMatchesAny(encoded.Metadata, req.Filters)
		} else {
			matches = vecMetadataMatches(encoded.Metadata, req.Filters)
		}

		if !matches {
			return nil
		}

		vd := &transport.VectorData{
			ID:        encoded.ID,
			Version:   encoded.Version,
			CreatedAt: encoded.CreatedAt,
			UpdatedAt: encoded.UpdatedAt,
		}
		if req.IncludeVectors {
			vd.Vector = encoded.Vector
		}
		if req.IncludeMetadata {
			vd.Metadata = encoded.Metadata
		}

		results = append(results, vd)
		return nil
	}); err != nil {
		return nil, err
	}

	totalCount := uint64(len(results))

	// Apply offset.
	if req.Offset > 0 && req.Offset < len(results) {
		results = results[req.Offset:]
	} else if req.Offset >= len(results) {
		results = nil
	}

	// Apply limit.
	if req.Limit > 0 && req.Limit < len(results) {
		results = results[:req.Limit]
	}

	return &transport.SearchByMetadataResponse{
		Results:    results,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// HybridSearch performs a combined vector similarity and metadata search.
func (t *baasTransport) HybridSearch(ctx context.Context, req *transport.HybridSearchRequest) (*transport.HybridSearchResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "HybridSearch"}
	}

	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	distFn := vecResolveDistanceFunc(meta.DistanceMetric)
	startTime := time.Now()

	k := req.K
	if k <= 0 {
		k = 10
	}

	useOR := strings.EqualFold(req.FilterOperator, "or")

	h := &vecSearchHeap{}
	heap.Init(h)

	var totalCandidates int
	var totalFiltered int

	dataPrefix := vecDataPrefix + req.Namespace + ":"
	if err := t.store.Scan(ctx, dataPrefix, func(_ string, value []byte) error {
		var encoded vecEncodedVector
		if err := json.Unmarshal(value, &encoded); err != nil {
			return nil
		}

		totalCandidates++

		// Apply metadata filter.
		if len(req.MetadataFilter) > 0 {
			var matches bool
			if useOR {
				matches = vecMetadataMatchesAny(encoded.Metadata, req.MetadataFilter)
			} else {
				matches = vecMetadataMatches(encoded.Metadata, req.MetadataFilter)
			}
			if !matches {
				totalFiltered++
				return nil
			}
		}

		dist := distFn(req.QueryVector, encoded.Vector)

		if req.MinScore > 0 && vecDistanceToScore(dist) < req.MinScore {
			return nil
		}

		entry := vecSearchEntry{
			id:       encoded.ID,
			distance: dist,
			vector:   encoded.Vector,
			metadata: encoded.Metadata,
		}

		if h.Len() < k {
			heap.Push(h, entry)
		} else if dist < (*h)[0].distance {
			(*h)[0] = entry
			heap.Fix(h, 0)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	results := vecHeapToResults(h, req.IncludeVectors, req.IncludeMetadata)
	duration := time.Since(startTime).Nanoseconds()

	return &transport.HybridSearchResponse{
		Results:         results,
		TotalCandidates: totalCandidates,
		TotalFiltered:   totalFiltered,
		Timestamp:       time.Now().UnixNano(),
		QueryDuration:   duration,
	}, nil
}

// SearchByID searches for vectors similar to an existing vector by its ID.
func (t *baasTransport) SearchByID(ctx context.Context, req *transport.SearchByIDRequest) (*transport.SearchByIDResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "SearchByID"}
	}

	// Retrieve the source vector.
	key := vecBuildDataKey(req.Namespace, req.ID)
	data, err := t.store.Get(ctx, key)
	if err != nil {
		return nil, &VectorNotFoundError{Namespace: req.Namespace, ID: req.ID}
	}

	var source vecEncodedVector
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, &VectorNotFoundError{Namespace: req.Namespace, ID: req.ID}
	}

	// Perform similarity search using the source vector.
	k := req.K
	if req.ExcludeSelf {
		k++ // fetch one extra to account for self-exclusion
	}

	searchResp, err := t.SearchSimilar(ctx, &transport.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     source.Vector,
		K:               k,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  req.MetadataFilter,
	})
	if err != nil {
		return nil, err
	}

	results := searchResp.Results

	// Exclude self from results if requested.
	if req.ExcludeSelf {
		filtered := make([]*transport.VectorSearchResult, 0, len(results))
		for _, r := range results {
			if r.ID != req.ID {
				filtered = append(filtered, r)
			}
		}
		results = filtered
		// Trim back to original K.
		if req.K > 0 && req.K < len(results) {
			results = results[:req.K]
		}
	}

	return &transport.SearchByIDResponse{
		SourceID:      req.ID,
		Results:       results,
		Timestamp:     searchResp.Timestamp,
		QueryDuration: searchResp.QueryDuration,
	}, nil
}

// RebuildIndex records an index rebuild request. In the KV-backed transport
// layer, this is a no-op since the brute-force scan does not maintain an
// index structure. The request is acknowledged for API compatibility.
func (t *baasTransport) RebuildIndex(_ context.Context, req *transport.RebuildIndexRequest) (*transport.RebuildIndexResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "RebuildIndex"}
	}

	now := time.Now().UnixNano()
	return &transport.RebuildIndexResponse{
		JobID:           "kv-rebuild-" + req.Namespace,
		Status:          "completed",
		PercentComplete: 100.0,
		Timestamp:       now,
		Success:         true,
	}, nil
}

// GetRebuildProgress returns rebuild progress. In the KV-backed transport,
// rebuilds complete immediately since there is no index structure to rebuild.
func (t *baasTransport) GetRebuildProgress(_ context.Context, req *transport.GetRebuildProgressRequest) (*transport.GetRebuildProgressResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "GetRebuildProgress"}
	}

	now := time.Now().UnixNano()
	return &transport.GetRebuildProgressResponse{
		JobID:           req.JobID,
		Namespace:       req.Namespace,
		Status:          "completed",
		PercentComplete: 100.0,
		StartedAt:       now,
		CompletedAt:     now,
	}, nil
}

// GetIndexStats returns statistics for a namespace's vector index. When
// vectorStorage is available, it reads stats from the real storage backend.
func (t *baasTransport) GetIndexStats(ctx context.Context, req *transport.GetIndexStatsRequest) (*transport.GetIndexStatsResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "GetIndexStats"}
	}

	if t.vectorStorage != nil {
		return t.getIndexStatsViaStorage(ctx, req)
	}
	return t.getIndexStatsViaKV(ctx, req)
}

// getIndexStatsViaStorage delegates index stats retrieval to the real
// vector storage backend.
func (t *baasTransport) getIndexStatsViaStorage(ctx context.Context, req *transport.GetIndexStatsRequest) (*transport.GetIndexStatsResponse, error) {
	info, err := t.vectorStorage.GetNamespaceInfo(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	return &transport.GetIndexStatsResponse{
		Namespace:   info.Name,
		IndexType:   info.IndexType,
		VectorCount: info.VectorCount,
		Dimension:   info.Dimension,
		IndexConfig: info.IndexConfig,
		Timestamp:   time.Now().UnixNano(),
	}, nil
}

// getIndexStatsViaKV retrieves index stats from the KV store (fallback).
func (t *baasTransport) getIndexStatsViaKV(ctx context.Context, req *transport.GetIndexStatsRequest) (*transport.GetIndexStatsResponse, error) {
	meta, err := t.vecGetNamespace(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	vectorCount, err := t.vecCountVectors(ctx, req.Namespace)
	if err != nil {
		return nil, err
	}

	return &transport.GetIndexStatsResponse{
		Namespace:   meta.Namespace,
		IndexType:   meta.IndexType,
		VectorCount: vectorCount,
		Dimension:   meta.Dimension,
		IndexConfig: meta.IndexConfig,
		Timestamp:   time.Now().UnixNano(),
	}, nil
}

// CompactNamespace triggers compaction for a namespace. When vectorStorage
// is available, it delegates to the real storage backend.
func (t *baasTransport) CompactNamespace(ctx context.Context, req *transport.CompactNamespaceRequest) (*transport.CompactNamespaceResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "CompactNamespace"}
	}

	if t.vectorStorage != nil {
		if err := t.vectorStorage.Compact(ctx, req.Namespace); err != nil {
			return nil, err
		}
	}

	now := time.Now().UnixNano()
	return &transport.CompactNamespaceResponse{
		OperationID: "kv-compact-" + req.Namespace,
		StartedAt:   now,
		CompletedAt: &now,
		Success:     true,
	}, nil
}

// GetCompactionProgress returns compaction progress. In the KV-backed
// transport, compaction completes immediately.
func (t *baasTransport) GetCompactionProgress(_ context.Context, req *transport.GetCompactionProgressRequest) (*transport.GetCompactionProgressResponse, error) {
	if req == nil {
		return nil, &VectorNilRequestError{Operation: "GetCompactionProgress"}
	}

	now := time.Now().UnixNano()
	return &transport.GetCompactionProgressResponse{
		OperationID:     req.OperationID,
		Namespace:       req.Namespace,
		PercentComplete: 100.0,
		InProgress:      false,
		StartedAt:       now,
		CompletedAt:     &now,
	}, nil
}

// ---------------------------------------------------------------------------
// Metadata conversion helpers
// ---------------------------------------------------------------------------

// vecMetadataToStringMap converts a map[string]interface{} to map[string]string
// for storage in the VectorStorage backend which uses string-typed metadata.
func vecMetadataToStringMap(meta map[string]interface{}) map[string]string {
	if meta == nil {
		return nil
	}
	result := make(map[string]string, len(meta))
	for k, v := range meta {
		switch val := v.(type) {
		case string:
			result[k] = val
		default:
			result[k] = strings.TrimRight(strings.TrimRight(
				strconv.FormatFloat(vecToFloat64Safe(v), 'f', -1, 64), "0"), ".")
			// Fall back to JSON for complex types.
			if result[k] == "0" && val != nil {
				if b, err := json.Marshal(val); err == nil {
					result[k] = string(b)
				}
			}
		}
	}
	return result
}

// vecStringMapToMetadata converts a map[string]string to map[string]interface{}
// for use in transport responses.
func vecStringMapToMetadata(meta map[string]string) map[string]interface{} {
	if meta == nil {
		return nil
	}
	result := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		result[k] = v
	}
	return result
}

// vecToFloat64Safe attempts to convert an interface value to float64,
// returning 0 if conversion is not possible.
func vecToFloat64Safe(v interface{}) float64 {
	f, _ := vecToFloat64(v)
	return f
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// vecBuildDataKey constructs a KV key for a vector data entry.
func vecBuildDataKey(namespace string, id uint64) string {
	return vecDataPrefix + namespace + ":" + strconv.FormatUint(id, 10)
}

// vecGetNamespace retrieves and decodes namespace metadata from the KV store.
func (t *baasTransport) vecGetNamespace(ctx context.Context, namespace string) (*vecNamespaceMeta, error) {
	key := vecNSPrefix + namespace
	data, err := t.store.Get(ctx, key)
	if err != nil {
		return nil, &VectorNamespaceNotFoundError{Namespace: namespace}
	}

	var meta vecNamespaceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, &VectorNamespaceNotFoundError{Namespace: namespace}
	}

	return &meta, nil
}

// vecCountVectors counts the number of vectors in a namespace by scanning
// the data prefix.
func (t *baasTransport) vecCountVectors(ctx context.Context, namespace string) (uint64, error) {
	dataPrefix := vecDataPrefix + namespace + ":"
	var count uint64
	if err := t.store.Scan(ctx, dataPrefix, func(_ string, _ []byte) error {
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

// vecResolveDistanceFunc returns the distance function for the given metric
// name, defaulting to cosine distance.
func vecResolveDistanceFunc(metric string) distanceFunc {
	if fn, ok := distanceFuncs[metric]; ok {
		return fn
	}
	return vecCosineDistance
}

// vecMetadataMatches returns true if all filter key-value pairs match the
// vector metadata (AND semantics).
func vecMetadataMatches(metadata, filters map[string]interface{}) bool {
	for k, filterVal := range filters {
		metaVal, ok := metadata[k]
		if !ok {
			return false
		}
		if !vecValuesEqual(metaVal, filterVal) {
			return false
		}
	}
	return true
}

// vecMetadataMatchesAny returns true if any filter key-value pair matches
// the vector metadata (OR semantics).
func vecMetadataMatchesAny(metadata, filters map[string]interface{}) bool {
	for k, filterVal := range filters {
		metaVal, ok := metadata[k]
		if !ok {
			continue
		}
		if vecValuesEqual(metaVal, filterVal) {
			return true
		}
	}
	return false
}

// vecValuesEqual compares two interface{} values for equality using string
// representation as a fallback for JSON-deserialized types.
func vecValuesEqual(a, b interface{}) bool {
	// Fast path for direct equality.
	if a == b {
		return true
	}

	// Handle numeric comparisons across JSON types. JSON numbers
	// unmarshal as float64, so compare numerically when possible.
	aNum, aOk := vecToFloat64(a)
	bNum, bOk := vecToFloat64(b)
	if aOk && bOk {
		return aNum == bNum
	}

	// Fall back to string comparison for mixed types.
	aStr, aStrOk := a.(string)
	bStr, bStrOk := b.(string)
	if aStrOk && bStrOk {
		return aStr == bStr
	}

	return false
}

// vecToFloat64 attempts to convert an interface value to float64.
func vecToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// vecDistanceToScore converts a distance value to a similarity score in [0, 1].
func vecDistanceToScore(distance float32) float32 {
	if distance <= 0 {
		return 1.0
	}
	return 1.0 / (1.0 + distance)
}

// vecHeapToResults converts a max-heap of search entries to a sorted slice
// of VectorSearchResult, ordered by ascending distance (nearest first).
func vecHeapToResults(h *vecSearchHeap, includeVectors, includeMetadata bool) []*transport.VectorSearchResult {
	results := make([]*transport.VectorSearchResult, 0, h.Len())
	for h.Len() > 0 {
		popped := heap.Pop(h)
		entry, ok := popped.(vecSearchEntry)
		if !ok {
			continue
		}
		r := &transport.VectorSearchResult{
			ID:       entry.id,
			Score:    vecDistanceToScore(entry.distance),
			Distance: entry.distance,
		}
		if includeVectors {
			r.Vector = entry.vector
		}
		if includeMetadata {
			r.Metadata = entry.metadata
		}
		results = append(results, r)
	}

	// Sort by ascending distance (nearest first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})

	// Assign ranks.
	for i := range results {
		results[i].Rank = i + 1
	}

	return results
}
