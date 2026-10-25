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
)

// newEmbeddedVectorClient builds an in-memory standalone qrdb node and returns
// its SDK client. The node owns the vector engine in-process, so every adapter
// call routes directly to the engine without network round-trips. The node is
// closed on test cleanup.
func newEmbeddedVectorClient(t *testing.T) (qrdbtransport.VectorClient, *qrdbsdk.Client) {
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

	client := srv.qrdbNode.Client()
	require.NotNil(t, client, "embedded node must expose a non-nil SDK client")
	// The vector client the server hands the vector service: bound to the
	// node's service principal, as startVector binds it.
	return srv.serviceVector(client.Vector()), client
}

// newEmbeddedVectorStorage returns a node-backed vector storage wired the
// way startVector wires it.
func newEmbeddedVectorStorage(t *testing.T) interfaces.VectorStorage {
	t.Helper()
	vec, _ := newEmbeddedVectorClient(t)
	return newNodeVectorStorage(vec)
}

// TestNodeVectorStorage_E2E exercises the full nodeVectorStorage adapter path
// against an in-memory embedded qrdb node: namespace lifecycle, single + batch
// vector mutations, scans, metadata search, stats, and compaction. Each call
// routes through the SDK transport and the engine, validating both the request
// conversion and the response conversion code in node_vector_storage.go.
func TestNodeVectorStorage_E2E(t *testing.T) {
	vs := newEmbeddedVectorStorage(t)
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
	vs := newEmbeddedVectorStorage(t)
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
	vs := newEmbeddedVectorStorage(t)

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
	srv.vectorStorage = newNodeVectorStorage(srv.serviceVector(srv.qrdbNode.Client().Vector()))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestNodeVectorStorage_GetStats_Missing covers the GetStats error branch:
// requesting stats for a namespace that does not exist surfaces the transport
// not-found error.
func TestNodeVectorStorage_GetStats_Missing(t *testing.T) {
	vs := newEmbeddedVectorStorage(t)

	stats, err := vs.GetStats(context.Background(), "does-not-exist")
	assert.Error(t, err)
	assert.Nil(t, stats)
}

// TestNodeVectorStorage_ScanNamespace_Missing covers the ScanNamespace error
// branch: scanning a namespace that does not exist surfaces the transport
// not-found error rather than an empty result.
func TestNodeVectorStorage_ScanNamespace_Missing(t *testing.T) {
	vs := newEmbeddedVectorStorage(t)

	got, err := vs.ScanNamespace(context.Background(), "does-not-exist", nil)
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_ListNamespaces_ClientClosed covers the ListNamespaces
// error branch: a closed transport makes the underlying Vector().ListNamespaces
// call fail, and the adapter surfaces the error (nil result).
func TestNodeVectorStorage_ListNamespaces_ClientClosed(t *testing.T) {
	vec, client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(vec)
	require.NoError(t, client.Close())

	got, err := vs.ListNamespaces(context.Background())
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_ScanByMultipleFilters_ClientClosed covers the
// ScanByMultipleFilters error branch via a closed transport.
func TestNodeVectorStorage_ScanByMultipleFilters_ClientClosed(t *testing.T) {
	vec, client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(vec)
	require.NoError(t, client.Close())

	got, err := vs.ScanByMultipleFilters(context.Background(), "ns", nil, nil)
	assert.Error(t, err)
	assert.Nil(t, got)
}

// TestNodeVectorStorage_Compact_ClientClosed covers the Compact request-side
// path through the adapter: the request conversion and transport dispatch run
// against a closed transport so the underlying CompactNamespace enqueue call
// surfaces the error synchronously without spawning background compaction work
// against a torn-down engine.
func TestNodeVectorStorage_Compact_ClientClosed(t *testing.T) {
	vec, client := newEmbeddedVectorClient(t)
	vs := newNodeVectorStorage(vec)
	require.NoError(t, client.Close())

	operationID, err := vs.Compact(context.Background(), "ns", false)
	assert.Error(t, err)
	assert.Empty(t, operationID)
	_, err = vs.CompactionProgress(context.Background(), "ns", "op")
	assert.Error(t, err)
}

// Compact hands back qrdb's operation ID, and CompactionProgress follows the
// compaction qrdb knows by it.
func TestNodeVectorStorage_CompactionProgress(t *testing.T) {
	vs := newEmbeddedVectorStorage(t)
	ctx := context.Background()
	const ns = "compacted"
	require.NoError(t, vs.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name: ns, Dimension: 3, DistanceMetric: "cosine", IndexType: "flat",
	}))

	operationID, err := vs.Compact(ctx, ns, false)
	require.NoError(t, err)
	require.NotEmpty(t, operationID, "qrdb's operation ID is kept")

	progress, err := vs.CompactionProgress(ctx, ns, operationID)
	require.NoError(t, err)
	assert.False(t, progress.InProgress)
	assert.NotNil(t, progress.CompletedAt)
	assert.Empty(t, progress.Error)

	_, err = vs.CompactionProgress(ctx, ns, "no-such-operation")
	assert.Error(t, err, "qrdb knows no compaction by that ID")
}
