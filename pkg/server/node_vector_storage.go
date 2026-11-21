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
	"strconv"
	"sync"
	"time"

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
	// snapshots lets the offset pages of one listing share one read of the
	// namespace instead of reading it once per page.
	snapshots vectorScanSnapshots
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
	n.snapshots.drop(namespace)
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
	n.snapshots.drop(namespace)
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
	n.snapshots.drop(namespace)
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
	n.snapshots.drop(namespace)
	return err
}

// BatchDeleteVectors deletes multiple vectors by IDs via the node transport.
func (n *nodeVectorStorage) BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	_, err := n.client.Vector().BatchDeleteVectors(ctx, &qrdbtransport.BatchDeleteVectorsRequest{
		Namespace: namespace,
		IDs:       ids,
	})
	n.snapshots.drop(namespace)
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
	// The pages of one listing share one snapshot of the namespace; the
	// snapshot is shared, so it is sliced here and never written to.
	all, err := n.snapshots.read(ctx, n.client.Vector(), req)
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

var _ interfaces.VectorFullScanner = (*nodeVectorStorage)(nil)

// ScanAllVectors returns every vector of a namespace once each, ordered by ID,
// from one listAllVectors read. It includes embeddings and metadata unless
// opts says otherwise; opts' paging fields are ignored.
func (n *nodeVectorStorage) ScanAllVectors(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	req := &qrdbtransport.ListVectorsRequest{
		Namespace:       namespace,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}
	if opts != nil {
		req.IncludeVectors = opts.IncludeVectors
		req.IncludeMetadata = opts.IncludeMetadata
	}
	all, err := listAllVectors(ctx, n.client.Vector(), req)
	if err != nil {
		return nil, err
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
// listAllVectors therefore repeats whole cursor passes, deduplicating by ID,
// until it holds every distinct vector of the namespace.
//
// The namespace count qrdb reports counts every stored version of a vector,
// so it is not the number of distinct IDs and never ends a scan. That number
// comes from the last page of a complete pass instead: qrdb deduplicates the
// namespace by ID on each request and returns no next cursor only for the
// page that reaches its end, so that page's offset plus its length is the
// distinct total. A namespace that fits one page is read in one request.
//
// When the namespace count, or the distinct total of a complete pass, differs
// from the previous pass's, the namespace changed: the vectors read so far are
// discarded, so a result never holds a vector that was deleted since. If
// nodeScanMaxPasses passes still fall short it returns errIncompleteVectorScan
// rather than an incomplete set, so a caller such as an index rebuild never
// reports success over vectors it did not read.
//
// A pass requests at most count/nodeScanPageSize + 2 pages, which reaches the
// last page because the count is at least the distinct total: once the cursor
// passes the end of a namespace that shrank between requests, qrdb restarts
// from the first vector with an ever larger cursor, so the pass would
// otherwise never end. The context is checked before every request.
func listAllVectors(ctx context.Context, vc qrdbtransport.VectorClient, req *qrdbtransport.ListVectorsRequest) ([]*qrdbtransport.VectorData, error) {
	count, distinct := -1, -1
	var seen map[uint64]struct{}
	var all []*qrdbtransport.VectorData
	reset := func() {
		seen, all = make(map[uint64]struct{}), nil
	}
	for pass := 0; pass < nodeScanMaxPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
		if err != nil {
			return nil, err
		}
		if c := int(info.VectorCount); c != count {
			count, distinct = c, -1
			reset()
		}
		page := *req
		page.Limit = nodeScanPageSize
		page.Cursor = ""
		passDistinct := -1
		for pages := count/nodeScanPageSize + 2; pages > 0; pages-- {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			resp, err := vc.ListVectors(ctx, &page)
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
			if resp.NextCursor == "" {
				if offset, ok := scanCursorOffset(page.Cursor); ok {
					passDistinct = offset + len(resp.Vectors)
				}
				break
			}
			// A later pass meets pages it has fully seen, so only an empty
			// page (not one with nothing new) ends the pass early.
			if resp.NextCursor == page.Cursor || len(resp.Vectors) == 0 {
				break
			}
			page.Cursor = resp.NextCursor
		}
		if passDistinct >= 0 {
			if distinct >= 0 && passDistinct != distinct {
				reset()
			}
			distinct = passDistinct
		}
		if distinct >= 0 && len(seen) == distinct {
			// The distinct total holds only if the namespace did not change
			// during the pass; a changed count sends the scan round again.
			info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
			if err != nil {
				return nil, err
			}
			if int(info.VectorCount) == count {
				sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
				return all, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: namespace %q: read %d of %d distinct vectors (namespace count %d)", errIncompleteVectorScan, req.Namespace, len(seen), distinct, count)
}

// nodeScanSnapshotTTL bounds how long the offset pages of one listing reuse
// one read of the namespace, and nodeScanSnapshotMax how many such reads the
// node keeps.
const (
	nodeScanSnapshotTTL = 30 * time.Second
	nodeScanSnapshotMax = 4
)

// vectorSnapshotKey names one snapshot: a namespace read with or without
// embeddings and metadata.
type vectorSnapshotKey struct {
	namespace         string
	vectors, metadata bool
}

// vectorSnapshot is one full, ID-ordered read of a namespace, with the
// namespace count read before it and the time it was taken.
type vectorSnapshot struct {
	count uint64
	at    time.Time
	all   []*qrdbtransport.VectorData
}

// vectorScanSnapshots keeps the last few full reads of namespaces, so the
// offset pages of one listing read the namespace once instead of once per
// page. A snapshot holds while it is younger than nodeScanSnapshotTTL and
// the namespace count is unchanged; every write through the node drops the
// namespace's snapshots. The zero value is ready to use.
type vectorScanSnapshots struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[vectorSnapshotKey]*vectorSnapshot
	// gen counts drops, so a read that a write overlapped does not store
	// what it read.
	gen uint64
}

func (s *vectorScanSnapshots) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// read returns the whole namespace of req ordered by ID, from a snapshot
// when one still holds and from listAllVectors otherwise. The returned slice
// is shared and must not be modified.
func (s *vectorScanSnapshots) read(ctx context.Context, vc qrdbtransport.VectorClient, req *qrdbtransport.ListVectorsRequest) ([]*qrdbtransport.VectorData, error) {
	key := vectorSnapshotKey{namespace: req.Namespace, vectors: req.IncludeVectors, metadata: req.IncludeMetadata}
	info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
	if err != nil {
		return nil, err
	}
	count := info.VectorCount

	s.mu.Lock()
	if e, ok := s.entries[key]; ok && e.count == count && s.clock().Sub(e.at) < nodeScanSnapshotTTL {
		all := e.all
		s.mu.Unlock()
		return all, nil
	}
	gen := s.gen
	s.mu.Unlock()

	all, err := listAllVectors(ctx, vc, req)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return all, nil
	}
	if s.entries == nil {
		s.entries = make(map[vectorSnapshotKey]*vectorSnapshot)
	}
	s.entries[key] = &vectorSnapshot{count: count, at: s.clock(), all: all}
	for len(s.entries) > nodeScanSnapshotMax {
		var oldest vectorSnapshotKey
		var oldestAt time.Time
		first := true
		for k, e := range s.entries {
			if first || e.at.Before(oldestAt) {
				oldest, oldestAt, first = k, e.at, false
			}
		}
		delete(s.entries, oldest)
	}
	return all, nil
}

// drop forgets every snapshot of namespace.
func (s *vectorScanSnapshots) drop(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	for k := range s.entries {
		if k.namespace == namespace {
			delete(s.entries, k)
		}
	}
}

// scanCursorOffset reads a qrdb ListVectors cursor as the offset it encodes;
// the empty cursor is offset 0.
func scanCursorOffset(cursor string) (int, bool) {
	if cursor == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, false
	}
	return offset, true
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
