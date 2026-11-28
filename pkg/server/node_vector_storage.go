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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
	defer n.snapshots.beginWrite(namespace)()
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
	defer n.snapshots.beginWrite(namespace)()
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
	defer n.snapshots.beginWrite(namespace)()
	_, err := n.client.Vector().DeleteVector(ctx, &qrdbtransport.DeleteVectorRequest{
		Namespace: namespace,
		ID:        id,
	})
	return err
}

// BatchUpsertVectors inserts or updates multiple vectors atomically via the node transport.
func (n *nodeVectorStorage) BatchUpsertVectors(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
	defer n.snapshots.beginWrite(namespace)()
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
	defer n.snapshots.beginWrite(namespace)()
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
	// The first page of a listing (offset 0) re-reads the namespace and
	// stores a fresh snapshot; its later pages slice that snapshot. The
	// snapshot is shared, so it is sliced here and never written to.
	var all []*qrdbtransport.VectorData
	var err error
	if offset <= 0 {
		all, err = n.snapshots.refresh(ctx, n.client.Vector(), req)
	} else {
		all, err = n.snapshots.read(ctx, n.client.Vector(), req)
	}
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
var _ interfaces.VectorScanInvalidator = (*nodeVectorStorage)(nil)

var _ interfaces.VectorWriteTracker = (*nodeVectorStorage)(nil)

// InvalidateScan records a write of namespace that already ended, for a
// caller that wrote the namespace through another client of the node: the
// namespace's write revision changes and its scan snapshots are dropped.
func (n *nodeVectorStorage) InvalidateScan(namespace string) { n.snapshots.beginWrite(namespace)() }

// BeginVectorWrite records a write of namespace made through another
// client of the node, so a full scan that overlaps it is not accepted.
func (n *nodeVectorStorage) BeginVectorWrite(namespace string) func() {
	return n.snapshots.beginWrite(namespace)
}

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
	all, err := listAllVectors(ctx, n.client.Vector(), req, &n.snapshots)
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
// A scan is accepted only when the namespace's write revision, kept in writes,
// is unchanged and no write of the namespace was in flight from the read of
// the first vector it keeps to its last check; otherwise the vectors read so
// far are discarded, so a result never holds a vector that was deleted since
// nor misses one written meanwhile. When the namespace count, or the distinct
// total of a complete pass, differs from the previous pass's, the vectors are
// discarded too. Neither guard sees a write that bypasses this node, such as
// a direct qrdb client deleting one vector and adding another between two
// pages, so a scan is accepted only after every vector it keeps is re-read
// from storage by ID at the version it was listed at, and the namespace count
// is still the same; a vector gone or changed meanwhile discards the vectors
// and uses up a pass. The re-read costs one point read per vector, with no
// vector or metadata payload. With a nil writes only the count checks and the
// re-read apply. If nodeScanMaxPasses passes still fall short it returns errIncompleteVectorScan
// rather than an incomplete set, so a caller such as an index rebuild never
// reports success over vectors it did not read.
//
// A pass requests at most count/nodeScanPageSize + 2 pages, which reaches the
// last page because the count is at least the distinct total: once the cursor
// passes the end of a namespace that shrank between requests, qrdb restarts
// from the first vector with an ever larger cursor, so the pass would
// otherwise never end. The context is checked before every request.
func listAllVectors(ctx context.Context, vc qrdbtransport.VectorClient, req *qrdbtransport.ListVectorsRequest, writes *vectorScanSnapshots) ([]*qrdbtransport.VectorData, error) {
	count, distinct := -1, -1
	var seen map[uint64]struct{}
	var all []*qrdbtransport.VectorData
	reset := func() {
		seen, all = make(map[uint64]struct{}), nil
	}
	// Keep the namespace's write state for the whole scan, so its revision
	// is not forgotten and re-made between two checks.
	defer writes.watch(req.Namespace)()
	// rev is the write revision the vectors in seen were read under, and
	// clean whether no write was in flight then.
	var rev uint64
	clean := false
	for pass := 0; pass < nodeScanMaxPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r, quiet := writes.revision(req.Namespace); seen == nil || r != rev || !quiet {
			// A write began or ended since these vectors were read, or one is
			// still running: start over under the current revision.
			reset()
			count, distinct = -1, -1
			rev, clean = r, quiet
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
			// during the pass: a write that began or ended, or a changed
			// count, sends the scan round again.
			info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
			if err != nil {
				return nil, err
			}
			r, quiet := writes.revision(req.Namespace)
			if int(info.VectorCount) == count && clean && quiet && r == rev {
				// A writer that bypasses this node moves neither the
				// revision nor, with a count-preserving swap, the count:
				// re-read every kept vector at its listed version, then
				// check the count once more.
				ok, err := verifyScannedVectors(ctx, vc, req.Namespace, all)
				if err != nil {
					return nil, err
				}
				if ok {
					info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
					if err != nil {
						return nil, err
					}
					if int(info.VectorCount) == count {
						sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
						return all, nil
					}
				}
				// Storage changed under the kept set: read it again.
				reset()
			}
		}
	}
	return nil, fmt.Errorf("%w: namespace %q: read %d of %d distinct vectors (namespace count %d)", errIncompleteVectorScan, req.Namespace, len(seen), distinct, count)
}

// verifyScannedVectors re-reads every vector of a finished scan from storage,
// one point read each with no vector or metadata payload. It reports false
// on the first vector that is gone, cannot be read, or is no longer at the
// version it was listed at, so the caller scans again; it fails only when
// ctx ends.
func verifyScannedVectors(ctx context.Context, vc qrdbtransport.VectorClient, ns string, all []*qrdbtransport.VectorData) (bool, error) {
	for _, v := range all {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		resp, err := vc.GetVector(ctx, &qrdbtransport.GetVectorRequest{Namespace: ns, ID: v.ID})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, ctxErr
			}
			return false, nil
		}
		if resp == nil || resp.ID != v.ID || resp.Version != v.Version {
			return false, nil
		}
	}
	return true, nil
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
	// tenant is the tenant bound in the reading ctx: go-qrdb scopes each
	// call by it, so one namespace name can hold different tenants' vectors.
	tenant            string
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
	// writes is the write state of each namespace; an entry lives while a
	// write of it is in flight or a scan watches it. Revisions come from
	// seq, so they never repeat even when an entry is removed and re-made.
	writes map[string]*namespaceWrites
	// seq is the last revision handed out; it only grows.
	seq uint64
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
	return s.load(ctx, vc, req, true)
}

// refresh always re-reads the namespace and replaces its snapshot; a
// listing calls it for its first page, so no listing starts from a
// snapshot older than that page.
func (s *vectorScanSnapshots) refresh(ctx context.Context, vc qrdbtransport.VectorClient, req *qrdbtransport.ListVectorsRequest) ([]*qrdbtransport.VectorData, error) {
	return s.load(ctx, vc, req, false)
}

// load reads the namespace snapshot for req. With reuse it serves a cached
// snapshot whose count still matches and whose age is within the TTL;
// without it, it always re-reads and stores the result.
func (s *vectorScanSnapshots) load(ctx context.Context, vc qrdbtransport.VectorClient, req *qrdbtransport.ListVectorsRequest, reuse bool) ([]*qrdbtransport.VectorData, error) {
	tenant, _ := tenantctx.Lookup(ctx)
	key := vectorSnapshotKey{tenant: tenant, namespace: req.Namespace, vectors: req.IncludeVectors, metadata: req.IncludeMetadata}
	info, err := vc.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{Namespace: req.Namespace})
	if err != nil {
		return nil, err
	}
	count := info.VectorCount

	s.mu.Lock()
	if e, ok := s.entries[key]; reuse && ok && e.count == count && s.clock().Sub(e.at) < nodeScanSnapshotTTL {
		all := e.all
		s.mu.Unlock()
		return all, nil
	}
	gen := s.gen
	s.mu.Unlock()

	all, err := listAllVectors(ctx, vc, req, s)
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

// namespaceWrites is the write state of one namespace that a full scan
// checks: rev changes when a write begins and when it ends, inflight counts
// the writes begun and not yet ended, and scans the scans watching it.
type namespaceWrites struct {
	rev      uint64
	inflight int
	scans    int
}

// beginWrite records that a write of namespace starts. The returned func
// records its end and drops the namespace's snapshots; it acts once.
func (s *vectorScanSnapshots) beginWrite(namespace string) func() {
	s.mu.Lock()
	w := s.writesOf(namespace)
	s.seq++
	w.rev = s.seq
	w.inflight++
	s.gen++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.seq++
			w.rev = s.seq
			w.inflight--
			s.reclaimLocked(namespace, w)
			s.mu.Unlock()
			s.drop(namespace)
		})
	}
}

// watch keeps namespace's write state for a scan until the returned func
// runs, so a write during the scan always moves the revision the scan
// read under. A nil s tracks nothing and returns a no-op func.
func (s *vectorScanSnapshots) watch(namespace string) func() {
	if s == nil {
		return func() {}
	}
	s.mu.Lock()
	w := s.writes[namespace]
	if w == nil {
		w = s.writesOf(namespace)
		w.rev = s.seq
	}
	w.scans++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			w.scans--
			s.reclaimLocked(namespace, w)
			s.mu.Unlock()
		})
	}
}

// reclaimLocked forgets namespace's write state once no write is in flight
// and no scan watches it. s.mu is held.
func (s *vectorScanSnapshots) reclaimLocked(namespace string, w *namespaceWrites) {
	if w.inflight == 0 && w.scans == 0 && s.writes[namespace] == w {
		delete(s.writes, namespace)
	}
}

// writesOf returns namespace's write state, creating it. s.mu is held.
func (s *vectorScanSnapshots) writesOf(namespace string) *namespaceWrites {
	if s.writes == nil {
		s.writes = make(map[string]*namespaceWrites)
	}
	w := s.writes[namespace]
	if w == nil {
		w = &namespaceWrites{}
		s.writes[namespace] = w
	}
	return w
}

// revision reports namespace's write revision and whether no write of it
// is in flight. A nil s tracks nothing and always reports (0, true).
func (s *vectorScanSnapshots) revision(namespace string) (uint64, bool) {
	if s == nil {
		return 0, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.writes[namespace]
	if w == nil {
		return 0, true
	}
	return w.rev, w.inflight == 0
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
