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
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

	all, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"})
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

	_, err := listAllVectors(context.Background(), f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"})
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

	_, err := listAllVectors(ctx, f, &qrdbtransport.ListVectorsRequest{Namespace: "ns"})
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
