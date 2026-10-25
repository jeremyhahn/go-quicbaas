// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"fmt"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// nodeVectorStorage implements interfaces.VectorStorage by delegating
// all operations through the qrdb SDK node client's embedded transport.
// When the qrdb node is running in-process every call routes directly
// to the node's PebbleDB-backed vector engine without network round-trips.
// The node owns the underlying storage lifecycle; Close is a no-op here.
type nodeVectorStorage struct {
	vec qrdbtransport.VectorClient
}

// newNodeVectorStorage creates a nodeVectorStorage backed by the given
// qrdb vector client. The server passes a client bound to the node's
// service principal (qrdbservice.Vector); the caller must ensure it remains
// valid for the lifetime of the returned storage.
func newNodeVectorStorage(vec qrdbtransport.VectorClient) interfaces.VectorStorage {
	return &nodeVectorStorage{vec: vec}
}

// CreateNamespace creates a new vector namespace via the node transport.
func (n *nodeVectorStorage) CreateNamespace(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
	meta := make(map[string]interface{}, len(info.Metadata))
	for k, v := range info.Metadata {
		meta[k] = v
	}
	_, err := n.vec.CreateNamespace(ctx, &qrdbtransport.CreateNamespaceRequest{
		Namespace:      info.Name,
		Dimension:      info.Dimension,
		DistanceMetric: info.DistanceMetric,
		IndexType:      info.IndexType,
		IndexConfig:    info.IndexConfig,
		Metadata:       meta,
	})
	return err
}

// DeleteNamespace removes a namespace and all its vectors via the node transport.
func (n *nodeVectorStorage) DeleteNamespace(ctx context.Context, namespace string) error {
	_, err := n.vec.DeleteNamespace(ctx, &qrdbtransport.DeleteNamespaceRequest{
		Namespace: namespace,
	})
	return err
}

// ListNamespaces returns all vector namespaces via the node transport.
func (n *nodeVectorStorage) ListNamespaces(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
	resp, err := n.vec.ListNamespaces(ctx, &qrdbtransport.ListNamespacesRequest{})
	if err != nil {
		return nil, err
	}
	result := make([]interfaces.VectorNamespaceInfo, len(resp.Namespaces))
	for i, ns := range resp.Namespaces {
		result[i] = nodeNamespaceInfoToInterface(ns)
	}
	return result, nil
}

// GetNamespaceInfo retrieves metadata about a namespace via the node transport.
func (n *nodeVectorStorage) GetNamespaceInfo(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
	resp, err := n.vec.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}
	// Convert response metadata map[string]interface{} → map[string]string.
	meta := make(map[string]string, len(resp.Metadata))
	for k, v := range resp.Metadata {
		meta[k] = fmt.Sprintf("%v", v)
	}
	return &interfaces.VectorNamespaceInfo{
		Name:           resp.Namespace,
		Dimension:      resp.Dimension,
		VectorCount:    resp.VectorCount,
		DistanceMetric: resp.DistanceMetric,
		IndexType:      resp.IndexType,
		IndexConfig:    resp.IndexConfig,
		Metadata:       meta,
		CreatedAt:      resp.CreatedAt,
		UpdatedAt:      resp.UpdatedAt,
	}, nil
}

// UpsertVector inserts or updates a single vector via the node transport.
func (n *nodeVectorStorage) UpsertVector(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
	meta := make(map[string]interface{}, len(vec.Metadata))
	for k, v := range vec.Metadata {
		meta[k] = v
	}
	_, err := n.vec.UpsertVector(ctx, &qrdbtransport.UpsertVectorRequest{
		Namespace: namespace,
		ID:        vec.ID,
		Vector:    vec.Embedding,
		Metadata:  meta,
	})
	return err
}

// GetVector retrieves a vector by namespace and ID via the node transport.
func (n *nodeVectorStorage) GetVector(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
	resp, err := n.vec.GetVector(ctx, &qrdbtransport.GetVectorRequest{
		Namespace:       namespace,
		ID:              id,
		IncludeVector:   true,
		IncludeMetadata: true,
	})
	if err != nil {
		return nil, err
	}
	meta := make(map[string]string, len(resp.Metadata))
	for k, v := range resp.Metadata {
		meta[k] = fmt.Sprintf("%v", v)
	}
	return &interfaces.VectorValue{
		ID:        resp.ID,
		Embedding: resp.Vector,
		Metadata:  meta,
		Version:   resp.Version,
		CreatedAt: resp.CreatedAt,
		UpdatedAt: resp.UpdatedAt,
	}, nil
}

// DeleteVector removes a single vector by namespace and ID via the node transport.
func (n *nodeVectorStorage) DeleteVector(ctx context.Context, namespace string, id uint64) error {
	_, err := n.vec.DeleteVector(ctx, &qrdbtransport.DeleteVectorRequest{
		Namespace: namespace,
		ID:        id,
	})
	return err
}

// BatchUpsertVectors inserts or updates multiple vectors atomically via the node transport.
func (n *nodeVectorStorage) BatchUpsertVectors(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
	tvecs := make([]*qrdbtransport.VectorData, len(vecs))
	for i, v := range vecs {
		meta := make(map[string]interface{}, len(v.Metadata))
		for k, mv := range v.Metadata {
			meta[k] = mv
		}
		tvecs[i] = &qrdbtransport.VectorData{
			ID:        v.ID,
			Vector:    v.Embedding,
			Metadata:  meta,
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			UpdatedAt: v.UpdatedAt,
		}
	}
	_, err := n.vec.BatchUpsert(ctx, &qrdbtransport.BatchUpsertRequest{
		Namespace: namespace,
		Vectors:   tvecs,
	})
	return err
}

// BatchDeleteVectors deletes multiple vectors by IDs via the node transport.
func (n *nodeVectorStorage) BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	_, err := n.vec.BatchDeleteVectors(ctx, &qrdbtransport.BatchDeleteVectorsRequest{
		Namespace: namespace,
		IDs:       ids,
	})
	return err
}

// ScanNamespace lists vectors in a namespace with optional pagination via the node transport.
func (n *nodeVectorStorage) ScanNamespace(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	req := &qrdbtransport.ListVectorsRequest{
		Namespace:       namespace,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}
	if opts != nil {
		req.Limit = opts.Limit
		req.Cursor = opts.Cursor
		req.IncludeVectors = opts.IncludeVectors
		req.IncludeMetadata = opts.IncludeMetadata
	}
	resp, err := n.vec.ListVectors(ctx, req)
	if err != nil {
		return nil, err
	}
	return nodeVectorDataToInterface(resp.Vectors), nil
}

// ScanByMultipleFilters scans vectors matching multiple metadata filters via the node transport.
// Filters are converted to a combined metadata-filter map and dispatched as SearchByMetadata.
func (n *nodeVectorStorage) ScanByMultipleFilters(ctx context.Context, namespace string, filters []interfaces.VectorMetadataFilter, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	combined := make(map[string]interface{}, len(filters))
	for _, f := range filters {
		combined[f.Field] = f.Value
	}
	req := &qrdbtransport.SearchByMetadataRequest{
		Namespace:       namespace,
		Filters:         combined,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}
	if opts != nil {
		req.Limit = opts.Limit
		req.Offset = opts.Offset
	}
	resp, err := n.vec.SearchByMetadata(ctx, req)
	if err != nil {
		return nil, err
	}
	return nodeVectorDataToInterface(resp.Results), nil
}

// GetStats returns storage statistics for a namespace via the node transport.
func (n *nodeVectorStorage) GetStats(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error) {
	resp, err := n.vec.GetIndexStats(ctx, &qrdbtransport.GetIndexStatsRequest{
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}
	return &interfaces.VectorStorageStats{
		VectorCount:      resp.VectorCount,
		SizeBytes:        resp.IndexSizeBytes,
		IndexSizeBytes:   resp.IndexSizeBytes,
		MemoryUsageBytes: resp.MemoryUsageBytes,
	}, nil
}

// Compact triggers compaction for a namespace via the node transport.
func (n *nodeVectorStorage) Compact(ctx context.Context, namespace string) error {
	_, err := n.vec.CompactNamespace(ctx, &qrdbtransport.CompactNamespaceRequest{
		Namespace: namespace,
		Async:     true,
	})
	return err
}

// Close is a no-op: the node owns the underlying vector storage lifecycle.
func (n *nodeVectorStorage) Close() error { return nil }

// nodeNamespaceInfoToInterface converts a qrdb transport NamespaceInfo to
// a quicbaas interfaces.VectorNamespaceInfo.
func nodeNamespaceInfoToInterface(ns *qrdbtransport.NamespaceInfo) interfaces.VectorNamespaceInfo {
	if ns == nil {
		return interfaces.VectorNamespaceInfo{}
	}
	return interfaces.VectorNamespaceInfo{
		Name:           ns.Namespace,
		Dimension:      ns.Dimension,
		VectorCount:    ns.VectorCount,
		DistanceMetric: ns.DistanceMetric,
		IndexType:      ns.IndexType,
		IndexConfig:    ns.IndexConfig,
		Metadata:       ns.Metadata,
		CreatedAt:      ns.CreatedAt,
		UpdatedAt:      ns.UpdatedAt,
	}
}

// nodeVectorDataToInterface converts a slice of qrdb transport VectorData
// to quicbaas interfaces.VectorValue slices.
func nodeVectorDataToInterface(vecs []*qrdbtransport.VectorData) []*interfaces.VectorValue {
	result := make([]*interfaces.VectorValue, 0, len(vecs))
	for _, v := range vecs {
		if v == nil {
			continue
		}
		meta := make(map[string]string, len(v.Metadata))
		for k, mv := range v.Metadata {
			meta[k] = fmt.Sprintf("%v", mv)
		}
		result = append(result, &interfaces.VectorValue{
			ID:        v.ID,
			Embedding: v.Vector,
			Metadata:  meta,
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			UpdatedAt: v.UpdatedAt,
		})
	}
	return result
}
