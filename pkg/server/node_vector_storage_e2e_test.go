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
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newEmbeddedVectorClient builds an in-memory standalone qrdb node and returns
// its SDK client. The node owns the vector engine in-process, so every adapter
// call routes directly to the engine without network round-trips. The node is
// closed on test cleanup.
func newEmbeddedVectorClient(t *testing.T) *qrdbsdk.Client {
	t.Helper()
	srv := newEmbeddedVectorServer(t)
	client := srv.qrdbNode.Client()
	require.NotNil(t, client, "embedded node must expose a non-nil SDK client")
	return client
}

// newEmbeddedVectorServer starts a standalone server with an embedded qrdb
// node on memory storage and stops the node when the test ends.
func newEmbeddedVectorServer(t *testing.T) *Server {
	t.Helper()
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.startStorage())
	require.NoError(t, srv.startIndexes())
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// TestNodeVectorStorage_E2E exercises the full nodeVectorStorage adapter path
// against an in-memory embedded qrdb node: namespace lifecycle, single + batch
// vector mutations, scans, metadata search, stats, and compaction. Each call
// routes through the SDK transport and the engine, validating both the request
// conversion and the response conversion code in node_vector_storage.go.
func TestNodeVectorStorage_E2E(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns = "docs"

	// CreateNamespace with metadata.
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           ns,
		Dimension:      3,
		DistanceMetric: "cosine",
		IndexType:      "flat",
		Metadata:       map[string]string{"owner": "qa"},
	}))

	// ListNamespaces should contain the new namespace.
	nss, err := vs.ListNamespaces(ctx)
	require.NoError(t, err)
	found := false
	for i := range nss {
		if nss[i].Name == ns {
			found = true
			assert.Equal(t, uint32(3), nss[i].Dimension)
		}
	}
	assert.True(t, found, "created namespace must appear in ListNamespaces")

	// GetNamespaceInfo returns the namespace metadata.
	info, err := vs.GetNamespaceInfo(ctx, ns)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, ns, info.Name)
	assert.Equal(t, uint32(3), info.Dimension)

	// UpsertVector + GetVector round-trip.
	require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{
		ID:        1,
		Embedding: []float32{0.1, 0.2, 0.3},
		Metadata:  map[string]string{"lang": "en"},
	}))
	got, err := vs.GetVector(ctx, ns, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint64(1), got.ID)
	require.Len(t, got.Embedding, 3)
	assert.Equal(t, "en", got.Metadata["lang"])

	// BatchUpsertVectors inserts multiple vectors.
	require.NoError(t, vs.BatchUpsertVectors(ctx, ns, []*interfaces.VectorValue{
		{ID: 2, Embedding: []float32{0.4, 0.5, 0.6}, Metadata: map[string]string{"lang": "fr"}},
		{ID: 3, Embedding: []float32{0.7, 0.8, 0.9}, Metadata: map[string]string{"lang": "en"}},
	}))

	// ScanNamespace returns every vector in the namespace.
	scanned, err := vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{
		Limit:           10,
		IncludeVectors:  true,
		IncludeMetadata: true,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(scanned), 3)

	// ScanNamespace with nil options uses defaults (exercises the nil branch).
	scannedDefault, err := vs.ScanNamespace(ctx, ns, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(scannedDefault), 3)

	// ScanByMultipleFilters narrows by metadata.
	filtered, err := vs.ScanByMultipleFilters(ctx, ns,
		[]interfaces.VectorMetadataFilter{{Field: "lang", Value: "en"}},
		&interfaces.VectorScanOptions{Limit: 10})
	require.NoError(t, err)
	for _, v := range filtered {
		assert.Equal(t, "en", v.Metadata["lang"])
	}

	// ScanByMultipleFilters with nil options exercises the nil branch.
	_, err = vs.ScanByMultipleFilters(ctx, ns,
		[]interfaces.VectorMetadataFilter{{Field: "lang", Value: "fr"}}, nil)
	require.NoError(t, err)

	// GetStats reports the vector count.
	stats, err := vs.GetStats(ctx, ns)
	require.NoError(t, err)
	require.NotNil(t, stats)
	// VectorCount is an unsigned counter; presence (non-nil stats) is the
	// meaningful assertion here.
	_ = stats.VectorCount

	// DeleteVector removes a single vector.
	require.NoError(t, vs.DeleteVector(ctx, ns, 1))

	// BatchDeleteVectors removes multiple vectors.
	require.NoError(t, vs.BatchDeleteVectors(ctx, ns, []uint64{2, 3}))

	// Close is a no-op.
	require.NoError(t, vs.Close())

	// DeleteNamespace removes the namespace.
	require.NoError(t, vs.DeleteNamespace(ctx, ns))
}

// TestNodeVectorStorage_GetVector_NotFound verifies the error path when reading
// a vector that does not exist surfaces a non-nil error and nil value.
func TestNodeVectorStorage_GetVector_NotFound(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           "empty",
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))

	got, err := vs.GetVector(ctx, "empty", 999)
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_GetNamespaceInfo_Missing verifies the error path when a
// namespace does not exist.
func TestNodeVectorStorage_GetNamespaceInfo_Missing(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)

	info, err := vs.GetNamespaceInfo(context.Background(), "does-not-exist")
	assert.Error(t, err)
	assert.Nil(t, info)
}

// TestNodeVectorDataToInterface_SkipsNil verifies that nil entries inside the
// input slice are skipped while real entries are converted, and metadata is
// stringified.
func TestNodeVectorDataToInterface_SkipsNil(t *testing.T) {
	in := []*qrdbtransport.VectorData{
		nil,
		{ID: 7, Vector: []float32{1, 2}, Metadata: map[string]interface{}{"n": 42}},
	}
	result := nodeVectorDataToInterface(in)
	require.Len(t, result, 1, "nil entries must be skipped")
	assert.Equal(t, uint64(7), result[0].ID)
	assert.Equal(t, "42", result[0].Metadata["n"], "metadata values are stringified")
}

// TestServer_Shutdown_VectorStorageBranch covers the vector-storage teardown
// branch of Server.Shutdown: a non-nil vectorStorage is closed during shutdown.
// The node-backed storage's Close is a no-op, so the success path of that
// branch runs.
func TestServer_Shutdown_VectorStorageBranch(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initialize())
	// Inject a node-backed vector storage so the Shutdown vector-storage branch
	// (which is otherwise skipped because the qrdb node owns vector storage)
	// executes its Close path.
	srv.vectorStorage = newNodeVectorStorage(srv.qrdbNode.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestNodeVectorStorage_GetStats_Missing covers the GetStats error branch:
// requesting stats for a namespace that does not exist surfaces the transport
// not-found error.
func TestNodeVectorStorage_GetStats_Missing(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)

	stats, err := vs.GetStats(context.Background(), "does-not-exist")
	assert.Error(t, err)
	assert.Nil(t, stats)
}

// TestNodeVectorStorage_ScanNamespace_Missing covers the ScanNamespace error
// branch: scanning a namespace that does not exist surfaces the transport
// not-found error rather than an empty result.
func TestNodeVectorStorage_ScanNamespace_Missing(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)

	got, err := vs.ScanNamespace(context.Background(), "does-not-exist", nil)
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_ScanNamespace_Offset proves the scan honours Offset
// (#1868): qrdb pages by cursor, so the storage skips the first Offset
// vectors itself, and a rebuild that pages by Offset reads every vector once.
func TestNodeVectorStorage_ScanNamespace_Offset(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           "paged",
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for id := uint64(1); id <= 5; id++ {
		require.NoError(t, vs.UpsertVector(ctx, "paged", &interfaces.VectorValue{
			ID:        id,
			Embedding: []float32{float32(id), 1},
		}))
	}

	all, err := vs.ScanNamespace(ctx, "paged", &interfaces.VectorScanOptions{Limit: 10, IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, all, 5)
	for i, v := range all {
		assert.Equal(t, uint64(i+1), v.ID, "an offset scan is ordered by ID")
	}

	ids := func(vs []*interfaces.VectorValue) []uint64 {
		out := make([]uint64, len(vs))
		for i, v := range vs {
			out[i] = v.ID
		}
		return out
	}

	page, err := vs.ScanNamespace(ctx, "paged", &interfaces.VectorScanOptions{Limit: 2, Offset: 2, IncludeVectors: true})
	require.NoError(t, err)
	assert.Equal(t, ids(all[2:4]), ids(page))
	for _, v := range page {
		assert.Len(t, v.Embedding, 2)
	}

	tail, err := vs.ScanNamespace(ctx, "paged", &interfaces.VectorScanOptions{Limit: 10, Offset: 4})
	require.NoError(t, err)
	assert.Equal(t, ids(all[4:]), ids(tail))

	past, err := vs.ScanNamespace(ctx, "paged", &interfaces.VectorScanOptions{Limit: 10, Offset: 5})
	require.NoError(t, err)
	assert.Empty(t, past)

	// Paging the way performIndexRebuild does visits each vector once.
	seen := map[uint64]int{}
	for offset := 0; ; {
		got, err := vs.ScanNamespace(ctx, "paged", &interfaces.VectorScanOptions{Limit: 2, Offset: offset, IncludeVectors: true})
		require.NoError(t, err)
		for _, v := range got {
			seen[v.ID]++
		}
		offset += len(got)
		if len(got) < 2 {
			break
		}
	}
	assert.Len(t, seen, 5)
	for id, n := range seen {
		assert.Equal(t, 1, n, "vector %d", id)
	}
}

// TestNodeVectorStorage_ScanNamespace_OffsetSeesOwnWrite pages a namespace
// by offset, writes through the node, and pages it again: the next scan sees
// a new vector and the new embedding of a re-upserted one, even though the
// re-upsert leaves the namespace count unchanged.
func TestNodeVectorStorage_ScanNamespace_OffsetSeesOwnWrite(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns = "paged-write"
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           ns,
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for id := uint64(1); id <= 5; id++ {
		require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{
			ID:        id,
			Embedding: []float32{float32(id), 1},
		}))
	}

	page, err := vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Limit: 2, Offset: 4, IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, uint64(5), page[0].ID)

	require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{ID: 5, Embedding: []float32{5, 2}}))
	page, err = vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Limit: 2, Offset: 4, IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Len(t, page[0].Embedding, 2)
	assert.Equal(t, float32(2), page[0].Embedding[1], "the re-upserted vector has its new embedding")

	require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{ID: 6, Embedding: []float32{6, 1}}))
	page, err = vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Limit: 2, Offset: 4, IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, uint64(5), page[0].ID)
	assert.Equal(t, uint64(6), page[1].ID, "the new vector is in the next scan")
}

// scanIDs returns the ids of one ScanNamespace page.
func scanIDs(t *testing.T, vs interfaces.VectorStorage, ns string, offset, limit int) []uint64 {
	t.Helper()
	page, err := vs.ScanNamespace(context.Background(), ns, &interfaces.VectorScanOptions{Offset: offset, Limit: limit, IncludeVectors: true})
	require.NoError(t, err)
	ids := make([]uint64, 0, len(page))
	for _, v := range page {
		ids = append(ids, v.ID)
	}
	return ids
}

// A write by another client of the node never reaches the adapter's drop,
// and a delete plus an insert keeps the count. A new listing's first page
// re-reads the namespace, so it sees the write anyway.
func TestNodeVectorStorage_ScanNamespace_FreshListingSeesOtherClientWrite(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns = "paged-other"
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           ns,
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for id := uint64(1); id <= 5; id++ {
		require.NoError(t, vs.UpsertVector(ctx, ns, &interfaces.VectorValue{ID: id, Embedding: []float32{float32(id), 1}}))
	}
	assert.Equal(t, []uint64{1, 2, 3}, scanIDs(t, vs, ns, 0, 3))
	assert.Equal(t, []uint64{4, 5}, scanIDs(t, vs, ns, 3, 3))

	_, err := client.Vector().DeleteVector(ctx, &qrdbtransport.DeleteVectorRequest{Namespace: ns, ID: 3})
	require.NoError(t, err)
	_, err = client.Vector().UpsertVector(ctx, &qrdbtransport.UpsertVectorRequest{Namespace: ns, ID: 9, Vector: []float32{9, 1}})
	require.NoError(t, err)

	assert.Equal(t, []uint64{1, 2, 4, 5, 9}, scanIDs(t, vs, ns, 0, 10))
}

// oneClientProvider hands the vector service the embedded node's client.
type oneClientProvider struct{ c *qrdbsdk.Client }

func (p oneClientProvider) ForShard(uint64) (*qrdbsdk.Client, error) { return p.c, nil }
func (p oneClientProvider) Close() error                             { return nil }

func listIDs(t *testing.T, svc service.VectorService, req *service.ListVectorsRequest) ([]uint64, string) {
	t.Helper()
	resp, err := svc.ListVectors(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	ids := make([]uint64, 0, len(resp.Vectors))
	for _, v := range resp.Vectors {
		ids = append(ids, v.ID)
	}
	return ids, resp.NextCursor
}

// The vector service writes through the SDK client, not the adapter; its
// writes must still reach the next page of a listing and a new listing.
func TestVectorService_ListVectorsAfterServiceWrites(t *testing.T) {
	srv := newEmbeddedVectorServer(t)
	storage := newNodeVectorStorage(srv.qrdbNode.Client())
	svc, err := service.NewVectorService(storage, logger.NewAdapter(srv.logger),
		service.WithVectorIndexFactory(NewVectorIndexFactory()),
		service.WithVectorSDKProvider(oneClientProvider{srv.qrdbNode.Client()}))
	require.NoError(t, err)
	ctx := tenantctx.WithSystem(context.Background())

	const ns = "lv"
	_, err = svc.CreateNamespace(ctx, &service.CreateNamespaceRequest{Namespace: ns, Dimension: 2, DistanceMetric: "cosine", IndexType: "flat"})
	require.NoError(t, err)
	for id := uint64(1); id <= 5; id++ {
		_, err = svc.UpsertVector(ctx, &service.UpsertVectorRequest{Namespace: ns, ID: id, Vector: []float32{float32(id), 1}})
		require.NoError(t, err)
	}

	ids, next := listIDs(t, svc, &service.ListVectorsRequest{Namespace: ns, Limit: 2})
	assert.Equal(t, []uint64{1, 2}, ids)
	require.Equal(t, "2", next)

	_, err = svc.DeleteVector(ctx, &service.DeleteVectorRequest{Namespace: ns, ID: 3})
	require.NoError(t, err)
	_, err = svc.UpsertVector(ctx, &service.UpsertVectorRequest{Namespace: ns, ID: 9, Vector: []float32{9, 1}})
	require.NoError(t, err)

	ids, _ = listIDs(t, svc, &service.ListVectorsRequest{Namespace: ns, Limit: 2, Cursor: "2"})
	assert.Equal(t, []uint64{4, 5}, ids, "the next page must not come from the stale snapshot")
	ids, _ = listIDs(t, svc, &service.ListVectorsRequest{Namespace: ns, Limit: 10})
	assert.Equal(t, []uint64{1, 2, 4, 5, 9}, ids)
}

// TestNodeVectorStorage_ScanNamespace_OffsetPastOnePage pages a namespace
// larger than one qrdb page (1,000 vectors) the way performIndexRebuild does:
// every vector is visited exactly once, and one large page is ordered by ID.
func TestNodeVectorStorage_ScanNamespace_OffsetPastOnePage(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	const ns, total = "paged-big", 2500
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

	info, err := vs.GetNamespaceInfo(ctx, ns)
	require.NoError(t, err)
	require.Equal(t, int64(total), int64(info.VectorCount), "the full scan relies on VectorCount")

	const pageSize = 1000
	seen := map[uint64]int{}
	for offset := 0; ; {
		got, err := vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Limit: pageSize, Offset: offset, IncludeVectors: true})
		require.NoError(t, err)
		for _, v := range got {
			seen[v.ID]++
		}
		offset += len(got)
		if len(got) < pageSize {
			break
		}
	}
	assert.Len(t, seen, total)
	for id, n := range seen {
		assert.Equal(t, 1, n, "vector %d", id)
	}

	all, err := vs.ScanNamespace(ctx, ns, &interfaces.VectorScanOptions{Limit: 3000, Offset: 0})
	require.NoError(t, err)
	require.Len(t, all, total)
	for i, v := range all {
		require.Equal(t, uint64(i+1), v.ID, "an offset scan is ordered by ID")
	}
}

// TestNodeVectorStorage_Compact_WaitsForCompletion checks Compact returns
// only once qrdb has finished compacting the namespace.
func TestNodeVectorStorage_Compact_WaitsForCompletion(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	ctx := context.Background()

	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           "compact",
		Dimension:      2,
		DistanceMetric: "cosine",
		IndexType:      "flat",
	}))
	for id := uint64(1); id <= 5; id++ {
		require.NoError(t, vs.UpsertVector(ctx, "compact", &interfaces.VectorValue{
			ID:        id,
			Embedding: []float32{float32(id), 1},
		}))
	}

	require.NoError(t, vs.Compact(ctx, "compact"))

	progress, err := client.Vector().GetCompactionProgress(ctx, &qrdbtransport.GetCompactionProgressRequest{Namespace: "compact"})
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.False(t, progress.InProgress, "compaction is finished when Compact returns")
	assert.NotNil(t, progress.CompletedAt)
	assert.Empty(t, progress.Error)
}

// TestNodeVectorStorage_ListNamespaces_ClientClosed covers the ListNamespaces
// error branch: a closed transport makes the underlying Vector().ListNamespaces
// call fail, and the adapter surfaces the error (nil result).
func TestNodeVectorStorage_ListNamespaces_ClientClosed(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	require.NoError(t, client.Close())

	got, err := vs.ListNamespaces(context.Background())
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_ScanByMultipleFilters_ClientClosed covers the
// ScanByMultipleFilters error branch via a closed transport.
func TestNodeVectorStorage_ScanByMultipleFilters_ClientClosed(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	require.NoError(t, client.Close())

	got, err := vs.ScanByMultipleFilters(context.Background(), "ns", nil, nil)
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_Compact_ClientClosed covers the Compact request-side
// path through the adapter: the request conversion and transport dispatch run
// against a closed transport so the underlying CompactNamespace call surfaces
// the error before any compaction work runs against a torn-down engine.
func TestNodeVectorStorage_Compact_ClientClosed(t *testing.T) {
	client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(client)
	require.NoError(t, client.Close())

	assert.Error(t, vs.Compact(context.Background(), "ns"))
}
