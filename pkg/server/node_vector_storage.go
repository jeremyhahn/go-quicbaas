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
	"errors"
	"fmt"
	"sort"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// nodeVectorStorage implements interfaces.VectorStorage by delegating
// all operations through the qrdb SDK node client's embedded transport.
// When the qrdb node is running in-process every call routes directly
// to the node's PebbleDB-backed vector engine without network round-trips.
// The node owns the underlying storage lifecycle; Close is a no-op here.
type nodeVectorStorage struct {
	client *qrdbsdk.Client
}

// newNodeVectorStorage creates a nodeVectorStorage backed by the given
// qrdb SDK client. The caller must ensure the client remains valid for
// the lifetime of the returned storage.
func newNodeVectorStorage(client *qrdbsdk.Client) interfaces.VectorStorage {
	return &nodeVectorStorage{client: client}
}

// CreateNamespace creates a new vector namespace via the node transport.
func (n *nodeVectorStorage) CreateNamespace(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
	meta := make(map[string]interface{}, len(info.Metadata))
	for k, v := range info.Metadata {
		meta[k] = v
	}
	_, err := n.client.Vector().CreateNamespace(ctx, &qrdbtransport.CreateNamespaceRequest{
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
	_, err := n.client.Vector().DeleteNamespace(ctx, &qrdbtransport.DeleteNamespaceRequest{
		Namespace: namespace,
	})
	return err
}

// ListNamespaces returns all vector namespaces via the node transport.
func (n *nodeVectorStorage) ListNamespaces(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
	resp, err := n.client.Vector().ListNamespaces(ctx, &qrdbtransport.ListNamespacesRequest{})
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
	resp, err := n.client.Vector().GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{
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
	_, err := n.client.Vector().UpsertVector(ctx, &qrdbtransport.UpsertVectorRequest{
		Namespace: namespace,
		ID:        vec.ID,
		Vector:    vec.Embedding,
		Metadata:  meta,
	})
	return err
}

// GetVector retrieves a vector by namespace and ID via the node transport.
func (n *nodeVectorStorage) GetVector(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
	resp, err := n.client.Vector().GetVector(ctx, &qrdbtransport.GetVectorRequest{
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
	_, err := n.client.Vector().DeleteVector(ctx, &qrdbtransport.DeleteVectorRequest{
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
	_, err := n.client.Vector().BatchUpsert(ctx, &qrdbtransport.BatchUpsertRequest{
		Namespace: namespace,
		Vectors:   tvecs,
	})
	return err
}

// BatchDeleteVectors deletes multiple vectors by IDs via the node transport.
func (n *nodeVectorStorage) BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	_, err := n.client.Vector().BatchDeleteVectors(ctx, &qrdbtransport.BatchDeleteVectorsRequest{
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
	offset, limit := 0, 0
	if opts != nil {
		req.Limit = opts.Limit
		req.Cursor = opts.Cursor
		req.IncludeVectors = opts.IncludeVectors
		req.IncludeMetadata = opts.IncludeMetadata
		offset, limit = opts.Offset, opts.Limit
	}
	if req.Cursor != "" || (offset <= 0 && limit <= 0) {
		resp, err := n.client.Vector().ListVectors(ctx, req)
		if err != nil {
			return nil, err
		}
		return nodeVectorDataToInterface(resp.Vectors), nil
	}

	// qrdb pages by cursor only, and the order of one page is not stable
	// from call to call. Offset paging (#1868) needs a stable order, so
	// read the whole namespace, order it by ID, and cut the page from that.
	all, err := n.listAllVectors(ctx, req)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(all) {
		return []*interfaces.VectorValue{}, nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return nodeVectorDataToInterface(all), nil
}

// nodeScanPageSize is the qrdb page size listAllVectors reads with.
const nodeScanPageSize = 1000

// nodeScanMaxPasses bounds how many whole cursor passes listAllVectors makes
// over a namespace before it gives up on reading every vector.
const nodeScanMaxPasses = 32

// errIncompleteVectorScan reports that listAllVectors could not read every
// vector the namespace holds within nodeScanMaxPasses passes.
var errIncompleteVectorScan = errors.New("vector scan did not return every vector in the namespace")

// listAllVectors returns every vector of a namespace once each, ordered by ID.
//
// qrdb reads its cursor as an offset into a slice it rebuilds from a map on
// every call, so the order differs from page to page: one cursor pass over a
// namespace larger than one page can return a vector twice and skip another.
// listAllVectors therefore reads the namespace's vector count first and repeats
// whole cursor passes, deduplicating by ID, until it has seen that many vectors.
// If nodeScanMaxPasses passes still fall short it returns
// errIncompleteVectorScan rather than an incomplete set, so a caller such as an
// index rebuild never reports success over vectors it did not read.
func (n *nodeVectorStorage) listAllVectors(ctx context.Context, req *qrdbtransport.ListVectorsRequest) ([]*qrdbtransport.VectorData, error) {
	info, err := n.client.Vector().GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{
		Namespace: req.Namespace,
	})
	if err != nil {
		return nil, err
	}
	want := int(info.VectorCount)

	seen := make(map[uint64]struct{})
	var all []*qrdbtransport.VectorData
	for pass := 0; pass < nodeScanMaxPasses && (pass == 0 || len(seen) < want); pass++ {
		page := *req
		page.Limit = nodeScanPageSize
		page.Cursor = ""
		for {
			resp, err := n.client.Vector().ListVectors(ctx, &page)
			if err != nil {
				return nil, err
			}
			for _, v := range resp.Vectors {
				if v == nil {
					continue
				}
				if _, dup := seen[v.ID]; dup {
					continue
				}
				seen[v.ID] = struct{}{}
				all = append(all, v)
			}
			// A later pass meets pages it has fully seen, so only an empty
			// page (not one with nothing new) ends the pass early.
			if resp.NextCursor == "" || resp.NextCursor == page.Cursor || len(resp.Vectors) == 0 {
				break
			}
			page.Cursor = resp.NextCursor
		}
	}
	if len(seen) < want {
		return nil, fmt.Errorf("%w: namespace %q: read %d of %d vectors", errIncompleteVectorScan, req.Namespace, len(seen), want)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
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
	resp, err := n.client.Vector().SearchByMetadata(ctx, req)
	if err != nil {
		return nil, err
	}
	return nodeVectorDataToInterface(resp.Results), nil
}

// GetStats returns storage statistics for a namespace via the node transport.
func (n *nodeVectorStorage) GetStats(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error) {
	resp, err := n.client.Vector().GetIndexStats(ctx, &qrdbtransport.GetIndexStatsRequest{
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

// Compact compacts a namespace via the node transport and returns once storage
// compaction has finished. It asks qrdb for a synchronous compaction: the
// service already runs Compact in its own background job, which marks the job
// completed when Compact returns, so a storage failure must surface here.
func (n *nodeVectorStorage) Compact(ctx context.Context, namespace string) error {
	resp, err := n.client.Vector().CompactNamespace(ctx, &qrdbtransport.CompactNamespaceRequest{
		Namespace: namespace,
		Async:     false,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("compact namespace %q: no response from storage", namespace)
	}
	if !resp.Success {
		return fmt.Errorf("compact namespace %q: storage reported the compaction did not succeed", namespace)
	}
	return nil
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
