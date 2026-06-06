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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

package server

import (
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestRAGNamespace covers the namespace-derivation branches: a path with a
// trailing component, a bare value with no slash, and the empty-path default.
func TestRAGNamespace(t *testing.T) {
	cases := []struct {
		name      string
		indexPath string
		want      string
	}{
		{"path with component", "/var/lib/rag_index", "rag-rag_index"},
		{"bare value no slash", "myindex", "rag-myindex"},
		{"empty path", "", "rag"},
		{"trailing slash empty tail", "/data/", "rag-data/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.RAGConfig{IndexPath: tc.indexPath}
			assert.Equal(t, tc.want, ragNamespace(cfg))
		})
	}
}

// TestRAGMetricToQRDB covers each recognised distance metric plus the
// cosine fallback for an unknown value.
func TestRAGMetricToQRDB(t *testing.T) {
	cases := map[string]string{
		"cosine":  qrdbtransport.DistanceCosine,
		"l2":      qrdbtransport.DistanceEuclidean,
		"ip":      qrdbtransport.DistanceDotProduct,
		"dot":     qrdbtransport.DistanceDotProduct,
		"unknown": qrdbtransport.DistanceCosine,
		"":        qrdbtransport.DistanceCosine,
	}
	for metric, want := range cases {
		t.Run("metric="+metric, func(t *testing.T) {
			assert.Equal(t, want, ragMetricToQRDB(metric))
		})
	}
}

// TestRAGVectorBackend covers both branches of the backend descriptor: a server
// with a qrdb client reports the persistent backend; one without reports memory.
func TestRAGVectorBackend(t *testing.T) {
	withClient := newEmbeddedRAGServer(t)
	assert.Equal(t, "qrdb-hnsw", withClient.ragVectorBackend(),
		"a server with a qrdb client uses the persistent HNSW backend")

	noClient := withTempServer(t)
	assert.Equal(t, "memory", noClient.ragVectorBackend(),
		"a server without a qrdb client falls back to the in-memory backend")
}

// TestBuildRAGSystemOptions covers the populated path of buildRAGSystemOptions:
// with a live qrdb client it resolves the vector shard, derives the namespace,
// and returns a qrdb-backed vector-index factory option. The nil-client path is
// covered separately.
func TestBuildRAGSystemOptions(t *testing.T) {
	srv := newEmbeddedRAGServer(t)
	opts := srv.buildRAGSystemOptions()
	require.NotNil(t, opts, "a server with a qrdb client must produce vector-index options")
	assert.Len(t, opts, 1)

	noClient := withTempServer(t)
	assert.Nil(t, noClient.buildRAGSystemOptions(),
		"a server without a qrdb client must produce no options")
}

// TestStartRAG_ValidConfig drives startRAG with a valid RAG config and a live
// qrdb client. The embedding provider is unreachable in the hermetic test, so
// rag.NewSystem degrades gracefully (no ragSystem set), but buildRAGSystemOptions
// and its namespace/metric/factory helpers run before that point.
func TestStartRAG_ValidConfig(t *testing.T) {
	srv := newEmbeddedRAGServer(t)
	srv.config.RAG = config.DefaultRAGConfig()
	srv.config.RAG.Enabled = true

	require.NoError(t, srv.startRAG(), "startRAG degrades gracefully when the embedding provider is down")
}

// TestStartRAG_InvalidConfig drives the validation-failure branch: an enabled
// RAG config with mismatched embedding/vector dimensions fails Validate and
// startRAG returns nil (graceful, RAG disabled) without constructing a system.
func TestStartRAG_InvalidConfig(t *testing.T) {
	srv := newEmbeddedRAGServer(t)
	srv.config.RAG = config.DefaultRAGConfig()
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Dimensions = 384
	srv.config.RAG.Vector.Dimensions = 768 // mismatch -> Validate fails

	require.NoError(t, srv.startRAG(), "an invalid RAG config must be a graceful no-op")
	assert.Nil(t, srv.ragSystem, "no RAG system may be constructed for an invalid config")
}

// newEmbeddedRAGServer builds an in-memory standalone server with a live qrdb
// client (via startService) so the RAG vector-shard resolution path is
// exercised. The node is closed on cleanup.
func newEmbeddedRAGServer(t *testing.T) *Server {
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
	require.NoError(t, srv.startService())
	require.NotNil(t, srv.qrdbClient, "startService must wire the qrdb client")
	return srv
}

// TestBuildRAGSystemOptions_ForShardError covers the ForShard-error branch of
// buildRAGSystemOptions: when the sharded client is closed, resolving the vector
// shard fails and the function falls back to an in-memory index (nil options).
func TestBuildRAGSystemOptions_ForShardError(t *testing.T) {
	srv := newEmbeddedRAGServer(t)
	// Closing the sharded client makes ForShard fail for every shard.
	require.NoError(t, srv.qrdbClient.Close())

	opts := srv.buildRAGSystemOptions()
	assert.Nil(t, opts, "a failing ForShard must fall back to the in-memory index (nil options)")
}
