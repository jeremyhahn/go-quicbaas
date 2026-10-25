// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// vecNodeIndex stands in for the qrdb node's vector index: searches,
// rebuilds and statistics all act on the same state, as they do on a node.
type vecNodeIndex struct {
	mu         sync.Mutex
	namespace  string
	generation uint64
	config     map[string]string
	jobID      string
}

func (n *vecNodeIndex) install(vt *vecTransport) {
	vt.searchSimilarFn = func(_ context.Context, req *qrdbtransport.SearchSimilarRequest) (*qrdbtransport.SearchSimilarResponse, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if req.Namespace != n.namespace {
			return nil, &qrdbtransport.NamespaceNotFoundError{Namespace: req.Namespace}
		}
		// The result names the index generation that answered.
		return &qrdbtransport.SearchSimilarResponse{Results: []*qrdbtransport.VectorSearchResult{
			{ID: n.generation, Score: 0.1},
		}}, nil
	}
	vt.rebuildIndexFn = func(_ context.Context, req *qrdbtransport.RebuildIndexRequest) (*qrdbtransport.RebuildIndexResponse, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if req.Namespace != n.namespace {
			return nil, &qrdbtransport.NamespaceNotFoundError{Namespace: req.Namespace}
		}
		n.generation++
		n.config = req.IndexConfig
		return &qrdbtransport.RebuildIndexResponse{
			JobID: n.jobID, Status: "completed", VectorsProcessed: 3, TotalVectors: 3,
			PercentComplete: 100, Timestamp: 42, Success: true,
		}, nil
	}
	vt.getRebuildProgressFn = func(_ context.Context, req *qrdbtransport.GetRebuildProgressRequest) (*qrdbtransport.GetRebuildProgressResponse, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if req.JobID != n.jobID {
			return nil, &qrdbtransport.KeyNotFoundError{Key: req.JobID}
		}
		return &qrdbtransport.GetRebuildProgressResponse{
			JobID: n.jobID, Namespace: n.namespace, Status: "completed",
			VectorsProcessed: 3, TotalVectors: 3, PercentComplete: 100,
		}, nil
	}
	vt.getIndexStatsFn = func(_ context.Context, req *qrdbtransport.GetIndexStatsRequest) (*qrdbtransport.GetIndexStatsResponse, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if req.Namespace != n.namespace {
			return nil, &qrdbtransport.NamespaceNotFoundError{Namespace: req.Namespace}
		}
		return &qrdbtransport.GetIndexStatsResponse{
			Namespace: req.Namespace, IndexType: "HNSW", VectorCount: 3, Dimension: 4,
			IndexSizeBytes: 900, MemoryUsageBytes: 1080, IndexConfig: n.config,
		}, nil
	}
}

func newVecSvcNodeIndex(t *testing.T) (*vectorService, *vecNodeIndex, *vecMockVectorIndex) {
	t.Helper()
	svc, _, vt, localIndex := newVecSvcWithSDK(t, "test-ns", 4)
	// The local index disagrees with the node, so a site that reads it
	// instead of the node is caught.
	localIndex.countVal = 99
	node := &vecNodeIndex{namespace: "test-ns", generation: 1, jobID: "node-job"}
	node.install(vt)
	return svc, node, localIndex
}

func TestVectorRebuildIndex_NodeBackedRebuildsTheIndexSearchUses(t *testing.T) {
	svc, _, localIndex := newVecSvcNodeIndex(t)
	ctx := tenantctx.WithSystem(context.Background())
	factoryCalled := false
	svc.indexFactory = &vecMockVectorIndexFactory{createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
		factoryCalled = true
		return newVecMockVectorIndex(), nil
	}}

	before, err := svc.SearchSimilar(ctx, &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1, 2, 3, 4}, K: 1})
	require.NoError(t, err)
	require.Len(t, before.Results, 1)

	resp, err := svc.RebuildIndex(ctx, &RebuildIndexRequest{Namespace: "test-ns", IndexConfig: map[string]string{"m": "32"}})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "node-job", resp.JobID)
	assert.Equal(t, uint64(3), resp.TotalVectors)

	after, err := svc.SearchSimilar(ctx, &SearchSimilarRequest{Namespace: "test-ns", QueryVector: []float32{1, 2, 3, 4}, K: 1})
	require.NoError(t, err)
	require.Len(t, after.Results, 1)
	assert.NotEqual(t, before.Results[0].ID, after.Results[0].ID, "a search after the rebuild must see the rebuilt index")

	stats, err := svc.GetIndexStats(ctx, &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, "32", stats.IndexConfig["m"], "the new settings must reach the index searches use")

	assert.False(t, factoryCalled, "a node-backed rebuild must not build a local index")
	assert.Same(t, localIndex, svc.indexes["test-ns"])
}

func TestVectorRebuildIndex_NodeBackedErrorIsTyped(t *testing.T) {
	svc, _, _ := newVecSvcNodeIndex(t)
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.RebuildIndex(ctx, &RebuildIndexRequest{Namespace: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *qbErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qbErrors.ErrNotFound, qbErr.Code)
}

func TestVectorGetRebuildProgress_NodeBackedReadsTheNodeJob(t *testing.T) {
	svc, _, _ := newVecSvcNodeIndex(t)
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{JobID: "node-job"})
	require.NoError(t, err)
	assert.Equal(t, "node-job", resp.JobID)
	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, 100.0, resp.PercentComplete)

	_, err = svc.GetRebuildProgress(ctx, &GetRebuildProgressRequest{JobID: "no-such-job"})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.ErrorAs(t, err, &qbErr)
}

func TestVectorGetIndexStats_NodeBackedDescribesTheIndexSearchUses(t *testing.T) {
	svc, _, _ := newVecSvcNodeIndex(t)
	ctx := tenantctx.WithSystem(context.Background())

	stats, err := svc.GetIndexStats(ctx, &GetIndexStatsRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, "test-ns", stats.Namespace)
	assert.Equal(t, uint64(3), stats.VectorCount, "statistics must describe the node index, not the local one")
	assert.Equal(t, uint32(4), stats.Dimension)
	assert.Equal(t, uint64(900), stats.IndexSizeBytes)

	_, err = svc.GetIndexStats(ctx, &GetIndexStatsRequest{Namespace: "missing"})
	require.Error(t, err)
	var qbErr *qbErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qbErrors.ErrNotFound, qbErr.Code)
}

func TestVectorRebuildIndex_LocalIndexStillRebuilds(t *testing.T) {
	svc, storage, _, oldIndex := newVecSvcLocalIndex(t, "test-ns", 4)
	ctx := tenantctx.WithSystem(context.Background())
	storage.scanNamespaceFn = func(_ context.Context, _ string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
		if opts.Offset > 0 {
			return nil, nil
		}
		return []*interfaces.VectorValue{{ID: 1, Embedding: []float32{1, 2, 3, 4}}}, nil
	}
	newIndex := newVecMockVectorIndex()
	svc.indexFactory = newVecMockVectorIndexFactory(newIndex)

	resp, err := svc.RebuildIndex(ctx, &RebuildIndexRequest{Namespace: "test-ns"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.NotSame(t, oldIndex, svc.indexes["test-ns"])
	assert.Same(t, newIndex, svc.indexes["test-ns"])
}
