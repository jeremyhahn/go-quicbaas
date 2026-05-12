//go:build raft && rest && grpc && quic && mcp && vector && rag && timeseries

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestVectorLifecycle tests vector service initialization paths.
func TestVectorLifecycle(t *testing.T) {
	t.Run("DisabledSkips", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })
		cfg.Vector.Enabled = false

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.startVector()
		require.NoError(t, err)
		assert.Nil(t, srv.vectorService)
	})

	t.Run("EnabledWithValidConfig", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })
		cfg.Vector.Enabled = true
		cfg.Vector.StoragePath = t.TempDir()
		cfg.Vector.ShardID = 200
		cfg.Vector.HNSW = config.DefaultVectorConfig().HNSW

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.startVector()
		require.NoError(t, err)
		assert.NotNil(t, srv.vectorStorage)
		assert.NotNil(t, srv.vectorService)

		// Cleanup
		if srv.vectorStorage != nil {
			srv.vectorStorage.Close()
		}
	})

	t.Run("EnabledWithPreInitializedStorage", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })
		cfg.Vector.Enabled = true
		cfg.Vector.StoragePath = t.TempDir()
		cfg.Vector.ShardID = 200
		cfg.Vector.HNSW = config.DefaultVectorConfig().HNSW

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Pre-initialize vector storage
		err = srv.initVectorStorage()
		require.NoError(t, err)
		require.NotNil(t, srv.vectorStorage)

		// Now start vector service (should reuse existing storage)
		err = srv.startVector()
		require.NoError(t, err)
		assert.NotNil(t, srv.vectorService)

		// Cleanup
		if srv.vectorStorage != nil {
			srv.vectorStorage.Close()
		}
	})
}

// TestRAGLifecycle tests RAG system initialization paths.
func TestRAGLifecycle(t *testing.T) {
	t.Run("DisabledSkips", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })
		cfg.RAG.Enabled = false

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.startRAG()
		require.NoError(t, err)
		assert.Nil(t, srv.ragSystem)
	})

	t.Run("StopRAGNilSystem", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		srv.ragSystem = nil

		err = srv.stopRAG()
		require.NoError(t, err)
	})
}

// TestTimeSeriesLifecycle tests time series service initialization paths.
func TestTimeSeriesLifecycle(t *testing.T) {
	t.Run("NilStorageSkips", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		srv.timeSeriesStorage = nil

		err = srv.startTimeSeries()
		require.NoError(t, err)
	})

	t.Run("StopTimeSeriesNilStorage", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		srv.timeSeriesStorage = nil

		err = srv.stopTimeSeries()
		require.NoError(t, err)
	})

	t.Run("InitAndStop", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Initialize time series storage (uses config.Storage.DataDir)
		err = srv.initTimeSeriesStorage()
		require.NoError(t, err)
		require.NotNil(t, srv.timeSeriesStorage)

		// Start time series service
		err = srv.startTimeSeries()
		require.NoError(t, err)

		// Stop time series (cleanup)
		err = srv.stopTimeSeries()
		require.NoError(t, err)
		assert.Nil(t, srv.timeSeriesStorage)
	})
}

// TestInitVectorStorage tests vector storage initialization.
func TestInitVectorStorage(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })
		cfg.Vector.Enabled = true
		cfg.Vector.StoragePath = t.TempDir()
		cfg.Vector.ShardID = 200
		cfg.Vector.HNSW = config.DefaultVectorConfig().HNSW

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.initVectorStorage()
		require.NoError(t, err)
		assert.NotNil(t, srv.vectorStorage)

		// Cleanup
		if srv.vectorStorage != nil {
			srv.vectorStorage.Close()
		}
	})

	// EnvVarFastPath_BackfillsDefaults locks in the bug fix for the documented
	// QUICBAAS_VECTOR_ENABLED=true UX path. Before the fix, enabling vector
	// without supplying an HNSW block crashed the daemon with
	// "configuration: invalid vector configuration: hnsw configuration is required".
	// This test mirrors the env-var path: Enabled=true, no HNSW, no ShardID,
	// no StoragePath. initVectorStorage must backfill defaults from the
	// daemon's data directory and start cleanly. If anyone removes the
	// ApplyDefaults call this test will fail, restoring the original UX bug.
	t.Run("EnvVarFastPath_BackfillsDefaults", func(t *testing.T) {
		cfg := createTestConfig(t)
		t.Cleanup(func() { cleanupTestData(t, cfg) })

		// Simulate exactly what QUICBAAS_VECTOR_ENABLED=true produces: only
		// the Enabled flag is set; everything else is zero.
		cfg.Vector = config.VectorConfig{Enabled: true}

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.initVectorStorage()
		require.NoError(t, err, "initVectorStorage must succeed on the env-var fast path; HNSW defaults must be applied")
		require.NotNil(t, srv.vectorStorage)

		// Verify the canonical defaults landed exactly where we expect them.
		require.NotNil(t, srv.config.Vector.HNSW)
		assert.Equal(t, 16, srv.config.Vector.HNSW.M)
		assert.Equal(t, 200, srv.config.Vector.HNSW.EfConstruction)
		assert.Equal(t, 10000, srv.config.Vector.HNSW.MaxElements)
		assert.Equal(t, 1.0, srv.config.Vector.HNSW.Ml)
		assert.Equal(t, uint64(200), srv.config.Vector.ShardID)
		assert.NotEmpty(t, srv.config.Vector.StoragePath, "StoragePath must be derived from Storage.DataDir")

		// Cleanup
		if srv.vectorStorage != nil {
			srv.vectorStorage.Close()
		}
	})
}
