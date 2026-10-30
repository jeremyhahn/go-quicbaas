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
	"strconv"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// pagedVectorTransport is a qrdb transport whose VectorClient serves a
// namespace of total vectors through ListVectors with the qrdb wire's cursor
// paging and include_vectors semantics. Every other VectorClient method is
// the nil embedded interface and panics if called.
type pagedVectorTransport struct {
	qrdbtransport.VectorClient
	total   int
	listErr error
	lastReq *qrdbtransport.ListVectorsRequest
}

func (p *pagedVectorTransport) Connect(context.Context) error { return nil }
func (p *pagedVectorTransport) Close() error                  { return nil }
func (p *pagedVectorTransport) Healthy(context.Context) bool  { return true }
func (p *pagedVectorTransport) Request(context.Context, string, any, any) error {
	return nil
}

func (p *pagedVectorTransport) ListVectors(_ context.Context, req *qrdbtransport.ListVectorsRequest) (*qrdbtransport.ListVectorsResponse, error) {
	p.lastReq = req
	if p.listErr != nil {
		return nil, p.listErr
	}
	start := 0
	if req.Cursor != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil {
			return nil, err
		}
		start = n
	}
	end := min(start+req.Limit, p.total)
	resp := &qrdbtransport.ListVectorsResponse{TotalCount: uint64(p.total)}
	for i := start; i < end; i++ {
		vec := &qrdbtransport.VectorData{ID: uint64(i + 1)}
		if req.IncludeVectors {
			vec.Vector = []float32{float32(i), 1, 2}
		}
		resp.Vectors = append(resp.Vectors, vec)
	}
	if end < p.total {
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

func newPagedNodeVectorStorage(t *testing.T, fake *pagedVectorTransport) interfaces.VectorStorage {
	t.Helper()
	client, err := qrdbsdk.NewClient(fake)
	require.NoError(t, err)
	return newNodeVectorStorage(client)
}

// TestNodeVectorStorage_ScanNamespacePage_WalksEveryVector verifies the
// adapter forwards the cursor and include_vectors to ListVectors and returns
// the next cursor, so a walk visits every vector once with its embedding.
func TestNodeVectorStorage_ScanNamespacePage_WalksEveryVector(t *testing.T) {
	const total = 2500
	storage := newPagedNodeVectorStorage(t, &pagedVectorTransport{total: total})

	seen := make(map[uint64]bool, total)
	opts := &interfaces.VectorScanOptions{Limit: 1000, IncludeVectors: true}
	for {
		page, err := storage.ScanNamespacePage(context.Background(), "ns", opts)
		require.NoError(t, err)
		for _, vec := range page.Vectors {
			require.NotEmpty(t, vec.Embedding, "vector %d has no embedding", vec.ID)
			require.False(t, seen[vec.ID], "vector %d returned twice", vec.ID)
			seen[vec.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		opts.Cursor = page.NextCursor
	}
	assert.Len(t, seen, total)
}

// TestNodeVectorStorage_ScanNamespacePage_ForwardsPayloadFlags verifies the
// adapter copies the scan's include flags into the qrdb ListVectors request.
func TestNodeVectorStorage_ScanNamespacePage_ForwardsPayloadFlags(t *testing.T) {
	tests := []struct {
		name string
		opts *interfaces.VectorScanOptions
		want [2]bool
	}{
		{name: "both requested", opts: &interfaces.VectorScanOptions{Limit: 1, IncludeVectors: true, IncludeMetadata: true}, want: [2]bool{true, true}},
		{name: "metadata only", opts: &interfaces.VectorScanOptions{Limit: 1, IncludeMetadata: true}, want: [2]bool{false, true}},
		{name: "nil options fetch both", opts: nil, want: [2]bool{true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &pagedVectorTransport{total: 1}
			storage := newPagedNodeVectorStorage(t, fake)

			_, err := storage.ScanNamespacePage(context.Background(), "ns", tt.opts)
			require.NoError(t, err)
			require.NotNil(t, fake.lastReq)
			assert.Equal(t, tt.want[0], fake.lastReq.IncludeVectors)
			assert.Equal(t, tt.want[1], fake.lastReq.IncludeMetadata)
		})
	}
}

// TestNodeVectorStorage_ScanNamespacePage_ListError verifies a ListVectors
// error comes back from the scan.
func TestNodeVectorStorage_ScanNamespacePage_ListError(t *testing.T) {
	listErr := errors.New("list vectors failed")
	storage := newPagedNodeVectorStorage(t, &pagedVectorTransport{listErr: listErr})

	page, err := storage.ScanNamespacePage(context.Background(), "ns", &interfaces.VectorScanOptions{Limit: 10})
	require.ErrorIs(t, err, listErr)
	assert.Nil(t, page)

	vecs, err := storage.ScanNamespace(context.Background(), "ns", nil)
	require.ErrorIs(t, err, listErr)
	assert.Nil(t, vecs)
}

// TestNewNodeVectorStorage_NilClient verifies that newNodeVectorStorage
// accepts a nil client without panicking and returns a non-nil storage.
// The storage itself will return errors on operations, but construction
// must not panic.
func TestNewNodeVectorStorage_NilClient(t *testing.T) {
	var storage = newNodeVectorStorage(nil)
	require.NotNil(t, storage)
}

// TestNodeVectorStorage_Close verifies that Close is a no-op.
func TestNodeVectorStorage_Close(t *testing.T) {
	storage := newNodeVectorStorage(nil)
	assert.NoError(t, storage.Close())
}

// TestNodeVectorDataToInterface_NilSlice verifies that a nil slice is
// handled gracefully and returns an empty (non-nil) slice.
func TestNodeVectorDataToInterface_NilSlice(t *testing.T) {
	result := nodeVectorDataToInterface(nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestNodeNamespaceInfoToInterface_Nil verifies that a nil NamespaceInfo
// returns a zero-valued VectorNamespaceInfo.
func TestNodeNamespaceInfoToInterface_Nil(t *testing.T) {
	result := nodeNamespaceInfoToInterface(nil)
	assert.Empty(t, result.Name)
}
