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

//go:build !integration && raft && local && quic && rest && mcp && vector && qrdb_vector && timeseries && rag

package server

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeScanVectorClient models the pinned go-qrdb ListVectors: the cursor is an
// offset applied only when it lies inside the namespace, at most 1000 vectors
// come back per page, and NextCursor is offset+limit whenever more remain past
// the page. At call shrinkAt the namespace shrinks to its first shrinkTo IDs.
type fakeScanVectorClient struct {
	qrdbtransport.VectorClient

	ids        []uint64
	staleCount int
	shrinkAt   int
	shrinkTo   int
	calls      int
	// mutate, when set, runs at call mutateAt before the page is served.
	mutateAt int
	mutate   func(f *fakeScanVectorClient)
	// shuffle, when set, serves each page from a fresh order of ids, as
	// qrdb rebuilds its listing from a map on every request.
	shuffle *rand.Rand
}

func (f *fakeScanVectorClient) GetNamespaceInfo(_ context.Context, req *qrdbtransport.GetNamespaceInfoRequest) (*qrdbtransport.GetNamespaceInfoResponse, error) {
	count := len(f.ids)
	if f.staleCount > 0 {
		count = f.staleCount
	}
	return &qrdbtransport.GetNamespaceInfoResponse{Namespace: req.Namespace, VectorCount: uint64(count)}, nil
}

func (f *fakeScanVectorClient) ListVectors(_ context.Context, req *qrdbtransport.ListVectorsRequest) (*qrdbtransport.ListVectorsResponse, error) {
	f.calls++
	if f.calls > 10000 {
		return nil, errors.New("fake: too many ListVectors calls")
	}
	if f.calls == f.shrinkAt {
		f.ids = f.ids[:f.shrinkTo]
	}
	if f.mutate != nil && f.calls == f.mutateAt {
		f.mutate(f)
	}
	limit := int(req.Limit)
	switch {
	case limit <= 0:
		limit = 100
	case limit > 1000:
		limit = 1000
	}
	offset := 0
	if req.Cursor != "" {
		if _, err := fmt.Sscanf(req.Cursor, "%d", &offset); err != nil {
			return nil, err
		}
	}
	ids := f.ids
	if f.shuffle != nil {
		ids = append([]uint64(nil), f.ids...)
		f.shuffle.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	}
	if offset > 0 && offset < len(ids) {
		ids = ids[offset:]
	}
	if len(ids) > limit+1 {
		ids = ids[:limit+1]
	}
	resp := &qrdbtransport.ListVectorsResponse{}
	if len(ids) > limit {
		ids = ids[:limit]
		resp.NextCursor = fmt.Sprintf("%d", offset+limit)
	}
	for _, id := range ids {
		resp.Vectors = append(resp.Vectors, &qrdbtransport.VectorData{ID: id})
	}
	return resp, nil
}

func newFakeScanVectorClient(n int) *fakeScanVectorClient {
	f := &fakeScanVectorClient{}
	for id := 1; id <= n; id++ {
		f.ids = append(f.ids, uint64(id))
	}
	return f
}

// TestListAllVectors_DeleteBetweenCursorRequests deletes 1000 of 2500 vectors
// between the requests at cursor 1000 and 2000: the scan starts over on the
// changed count and returns exactly the vectors that remain.
func TestListAllVectors_DeleteBetweenCursorRequests(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	f.shrinkAt, f.shrinkTo = 3, 1500

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	require.NoError(t, err)
	require.Len(t, all, 1500)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID)
	}
	assert.LessOrEqual(t, f.calls, 6)
}

// TestListAllVectors_ShrunkNamespaceWithStaleCountEnds keeps reporting the old
// count after the namespace shrank: every pass is bounded, so the scan ends
// with errIncompleteVectorScan instead of paging forever.
func TestListAllVectors_ShrunkNamespaceWithStaleCountEnds(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	f.shrinkAt, f.shrinkTo, f.staleCount = 3, 1500, 2500

	_, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errIncompleteVectorScan), "got %v", err)
	assert.LessOrEqual(t, f.calls, nodeScanMaxPasses*(2500/nodeScanPageSize+2))
}

// TestListAllVectors_StopsOnCancel returns the context's error before any
// request when the context is already cancelled.
func TestListAllVectors_StopsOnCancel(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := listAllVectors(ctx, f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	assert.Equal(t, 0, f.calls)
}

// TestNodeVectorStorage_ScanAllVectors reads a namespace larger than one qrdb
// page in one call: every vector once, ordered by ID, with its embedding.
func TestNodeVectorStorage_ScanAllVectors(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns, total = "full-scan", 2500
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           ns,
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for start := uint64(1); start <= total; start += 500 {
		batch := make([]*interfaces.VectorValue, 0, 500)
		for id := start; id < start+500 && id <= total; id++ {
			batch = append(batch, &interfaces.VectorValue{
				ID:        id,
				Embedding: []float32{float32(id), 1},
			})
		}
		require.NoError(t, vs.BatchUpsertVectors(ctx, ns, batch))
	}

	full, ok := vs.(interfaces.VectorFullScanner)
	require.True(t, ok, "the node vector storage is a full scanner")
	all, err := full.ScanAllVectors(ctx, ns, &interfaces.VectorScanOptions{IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, all, total)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID, "a full scan is ordered by ID")
		require.Len(t, v.Embedding, 2, "vector %d", v.ID)
	}
}

// TestNodeVectorStorage_ScanAllVectors_Reupserted re-upserts some vectors of a
// namespace larger than one qrdb page: a full scan and an offset scan return
// every vector once, with its latest embedding. The pinned qrdb service writes
// every upsert at version 1, so the namespace count cannot exceed the distinct
// IDs here; TestListAllVectors_StoredVersions* cover a count that also counts
// stored versions.
func TestNodeVectorStorage_ScanAllVectors_Reupserted(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns, total, updated = "versioned", 1500, 40
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           ns,
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for start := uint64(1); start <= total; start += 500 {
		batch := make([]*interfaces.VectorValue, 0, 500)
		for id := start; id < start+500 && id <= total; id++ {
			batch = append(batch, &interfaces.VectorValue{
				ID:        id,
				Embedding: []float32{float32(id), 1},
			})
		}
		require.NoError(t, vs.BatchUpsertVectors(ctx, ns, batch))
	}
	for id := uint64(1); id <= updated; id++ {
		require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{
			ID:        id,
			Embedding: []float32{float32(id), 2},
		}))
	}

	full, ok := vs.(interfaces.VectorFullScanner)
	require.True(t, ok, "the node vector storage is a full scanner")
	all, err := full.ScanAllVectors(ctx, ns, &interfaces.VectorScanOptions{IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, all, total)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID, "a full scan is ordered by ID")
		require.Len(t, v.Embedding, 2, "vector %d", v.ID)
		want := float32(1)
		if v.ID <= updated {
			want = 2
		}
		require.Equal(t, want, v.Embedding[1], "vector %d has its latest embedding", v.ID)
	}

	page, err := vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Offset: total - 10, Limit: 20})
	require.NoError(t, err)
	require.Len(t, page, 10)
	for i, v := range page {
		require.Equal(t, uint64(total-10+i+1), v.ID)
	}
}

// TestListAllVectors_StoredVersionsCount ends a scan on the distinct vectors a
// complete cursor pass reports, not on a namespace count that also counts
// stored versions: a count above the distinct total neither fails the scan nor
// costs more than one pass.
func TestListAllVectors_StoredVersionsCount(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	f.staleCount = 2600

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	require.NoError(t, err)
	require.Len(t, all, 2500)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID)
	}
	assert.Equal(t, 3, f.calls, "one cursor pass of three pages")
}

// TestListAllVectors_StoredVersionsShuffledPages serves every page from a
// fresh order of the namespace, the way qrdb does, with a namespace count that
// counts stored versions: the scan repeats passes until it holds every
// distinct vector, and returns each once.
func TestListAllVectors_StoredVersionsShuffledPages(t *testing.T) {
	f := newFakeScanVectorClient(3000)
	f.staleCount = 3500
	f.shuffle = rand.New(rand.NewSource(1670))

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	require.NoError(t, err)
	require.Len(t, all, 3000)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID)
	}
}

// TestListAllVectors_SmallNamespaceOneRequest reads a namespace that fits one
// qrdb page with a single ListVectors request, whatever the namespace count.
func TestListAllVectors_SmallNamespaceOneRequest(t *testing.T) {
	f := newFakeScanVectorClient(700)
	f.staleCount = 900

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, nil)
	require.NoError(t, err)
	require.Len(t, all, 700)
	assert.Equal(t, 1, f.calls)
}

// scanSnapshotPages reads a namespace as ListVectors in pkg/service does:
// offset pages of size until a short page.
func scanSnapshotPages(t *testing.T, s *vectorScanSnapshots, f *fakeScanVectorClient, ns string, size int) []uint64 {
	t.Helper()
	req := &qrdbtransport.ListVectorsRequest{Namespace: ns}
	var ids []uint64
	for offset := 0; ; offset += size {
		all, err := s.read(context.Background(), f, req)
		require.NoError(t, err)
		if offset >= len(all) {
			return ids
		}
		end := offset + size
		if end > len(all) {
			end = len(all)
		}
		for _, v := range all[offset:end] {
			ids = append(ids, v.ID)
		}
	}
}

// TestVectorScanSnapshots_PagesReadNamespaceOnce lists 2500 vectors as 25
// offset pages of 100: every ID comes back once, and the namespace is read
// once. One read is ceil(2500/nodeScanPageSize) = 3 ListVectors calls, which
// is all the fake counts.
func TestVectorScanSnapshots_PagesReadNamespaceOnce(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	var s vectorScanSnapshots

	ids := scanSnapshotPages(t, &s, f, "ns", 100)
	require.Len(t, ids, 2500)
	for i, id := range ids {
		require.Equal(t, uint64(i+1), id)
	}
	assert.Equal(t, 3, f.calls)
}

// TestVectorScanSnapshots_CountChangeRereads shrinks the namespace after the
// first read: the changed count reads it again. 3 calls for 2500 vectors,
// then 2 for 1500, makes 5.
func TestVectorScanSnapshots_CountChangeRereads(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	var s vectorScanSnapshots
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}

	all, err := s.read(context.Background(), f, req)
	require.NoError(t, err)
	require.Len(t, all, 2500)

	f.ids = f.ids[:1500]
	all, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	require.Len(t, all, 1500)
	assert.Equal(t, 5, f.calls)
}

// TestVectorScanSnapshots_ExpiresAfterTTL reads the namespace again once the
// snapshot is older than nodeScanSnapshotTTL: 3 calls per read of 2500.
func TestVectorScanSnapshots_ExpiresAfterTTL(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	now := time.Unix(1670, 0)
	s := vectorScanSnapshots{now: func() time.Time { return now }}
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}

	_, err := s.read(context.Background(), f, req)
	require.NoError(t, err)
	now = now.Add(nodeScanSnapshotTTL - time.Second)
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	require.Equal(t, 3, f.calls, "a snapshot younger than the TTL holds")

	now = now.Add(2 * time.Second)
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	assert.Equal(t, 6, f.calls, "an expired snapshot reads the namespace again")
}

// TestVectorScanSnapshots_DropRereads reads the namespace again after drop,
// even though its count is unchanged: 3 calls per read of 2500.
func TestVectorScanSnapshots_DropRereads(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	var s vectorScanSnapshots
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}

	_, err := s.read(context.Background(), f, req)
	require.NoError(t, err)
	s.drop("other")
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	require.Equal(t, 3, f.calls, "dropping another namespace keeps this snapshot")

	s.drop("ns")
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	assert.Equal(t, 6, f.calls)
}

// TestListAllVectors_CountPreservingWriteBetweenPages is the review's case:
// between the first and second page ID 1 is deleted and 1501 inserted, so
// both counts hold, yet the pass kept 1 and missed 1001.
func TestListAllVectors_CountPreservingWriteBetweenPages(t *testing.T) {
	f := newFakeScanVectorClient(1500)
	var s vectorScanSnapshots
	f.mutateAt = 2
	f.mutate = func(f *fakeScanVectorClient) {
		done := s.beginWrite("ns")
		f.ids = append(append([]uint64(nil), f.ids[1:]...), 1501)
		done()
	}

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, &s)
	require.NoError(t, err)
	require.Len(t, all, 1500)
	for i, v := range all {
		require.Equal(t, uint64(i+2), v.ID, "index %d", i)
	}
}

func TestListAllVectors_WriteInFlightIsNotAccepted(t *testing.T) {
	f := newFakeScanVectorClient(1500)
	var s vectorScanSnapshots
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}

	done := s.beginWrite("ns")
	_, err := listAllVectors(context.Background(), f, req, &s)
	require.ErrorIs(t, err, errIncompleteVectorScan)

	done()
	all, err := listAllVectors(context.Background(), f, req, &s)
	require.NoError(t, err)
	require.Len(t, all, 1500)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID, "index %d", i)
	}
}

func TestVectorScanSnapshots_BeginWriteRevision(t *testing.T) {
	var s vectorScanSnapshots
	release := s.watch("ns")
	r0, quiet := s.revision("ns")
	require.Equal(t, uint64(0), r0)
	require.True(t, quiet)

	done := s.beginWrite("ns")
	r1, quiet := s.revision("ns")
	require.False(t, quiet)
	require.NotEqual(t, r0, r1)

	s.beginWrite("other")()
	r, _ := s.revision("ns")
	require.Equal(t, r1, r, "a write of another namespace leaves this revision")

	done()
	r2, quiet := s.revision("ns")
	require.True(t, quiet)
	require.NotEqual(t, r0, r2)
	require.NotEqual(t, r1, r2)

	done()
	r, quiet = s.revision("ns")
	require.True(t, quiet)
	require.Equal(t, r2, r, "a second done changes nothing")
	release()
	require.Empty(t, s.writes, "released write state is reclaimed")

	f := newFakeScanVectorClient(2500)
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}
	_, err := s.read(context.Background(), f, req)
	require.NoError(t, err)
	s.beginWrite("ns")()
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	assert.Equal(t, 6, f.calls, "the ended write dropped the snapshot")
}

// A key's snapshot is the reading tenant's: a system caller's namespace
// "tenant/acme/docs" and tenant acme's "docs" share a name, not vectors.
func TestVectorScanSnapshots_TenantsDoNotShareSnapshots(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	var s vectorScanSnapshots
	req := &qrdbtransport.ListVectorsRequest{Namespace: "tenant/acme/docs"}

	_, err := s.refresh(tenantctx.WithSystem(context.Background()), f, req)
	require.NoError(t, err)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	_, err = s.read(acme, f, req)
	require.NoError(t, err)
	require.Equal(t, 6, f.calls, "another tenant's snapshot is not served")
	_, err = s.read(acme, f, req)
	require.NoError(t, err)
	assert.Equal(t, 6, f.calls, "the tenant's own snapshot is reused")
}

// Writes of namespaces that do not exist (a failed delete) leave no state
// behind once they end.
func TestVectorScanSnapshots_WriteStateReclaimed(t *testing.T) {
	var s vectorScanSnapshots
	for i := 0; i < 10000; i++ {
		s.beginWrite(fmt.Sprintf("missing-%d", i))()
	}
	require.Empty(t, s.writes)

	done := s.beginWrite("ns")
	s.beginWrite("other")()
	require.Len(t, s.writes, 1, "a write in flight keeps its state")
	done()
	assert.Empty(t, s.writes)
}

func TestListAllVectors_WriteStateReclaimedAfterScan(t *testing.T) {
	f := newFakeScanVectorClient(1500)
	var s vectorScanSnapshots
	f.mutateAt = 2
	f.mutate = func(f *fakeScanVectorClient) {
		done := s.beginWrite("ns")
		f.ids = append(append([]uint64(nil), f.ids[1:]...), 1501)
		done()
	}

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"}, &s)
	require.NoError(t, err)
	require.Len(t, all, 1500)
	for i, v := range all {
		require.Equal(t, uint64(i+2), v.ID, "index %d", i)
	}
	assert.Empty(t, s.writes, "the scan releases the namespace's write state")
}

// TestVectorScanSnapshots_Bounded reads more namespaces than the cache keeps:
// it never holds more than nodeScanSnapshotMax snapshots.
func TestVectorScanSnapshots_Bounded(t *testing.T) {
	f := newFakeScanVectorClient(700)
	now := time.Unix(1670, 0)
	s := vectorScanSnapshots{now: func() time.Time { now = now.Add(time.Millisecond); return now }}

	for i := 0; i < 3*nodeScanSnapshotMax; i++ {
		_, err := s.read(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: fmt.Sprintf("ns-%d", i)})
		require.NoError(t, err)
		require.LessOrEqual(t, len(s.entries), nodeScanSnapshotMax)
	}
	last := vectorSnapshotKey{namespace: fmt.Sprintf("ns-%d", 3*nodeScanSnapshotMax-1)}
	assert.Contains(t, s.entries, last, "the newest snapshot is kept")
}

// refresh always re-reads and stores what it read, so a later read hits it.
// A read of 2500 ids costs 3 ListVectors pages: read 3, refresh 3 more (6),
// and the read after the refresh serves its snapshot (still 6).
func TestVectorScanSnapshots_RefreshRereads(t *testing.T) {
	f := newFakeScanVectorClient(2500)
	var s vectorScanSnapshots
	req := &qrdbtransport.ListVectorsRequest{Namespace: "ns"}

	_, err := s.read(context.Background(), f, req)
	require.NoError(t, err)
	require.Equal(t, 3, f.calls)
	all, err := s.refresh(context.Background(), f, req)
	require.NoError(t, err)
	require.Len(t, all, 2500)
	require.Equal(t, 6, f.calls, "refresh re-reads a cached namespace")
	_, err = s.read(context.Background(), f, req)
	require.NoError(t, err)
	assert.Equal(t, 6, f.calls, "refresh stored its snapshot")
}
