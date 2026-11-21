//go:build raft

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ---------------------------------------------------------------------------
// WithStorageConfig option
// ---------------------------------------------------------------------------

func TestWithStorageConfig(t *testing.T) {
	t.Run("sets storage configuration on sharded client", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Backend: config.StorageBackendMemory,
			DataDir: "/tmp/test-data",
		}
		sc := NewShardedClient(nil, WithStorageConfig(cfg))
		defer sc.Close()
		assert.Equal(t, cfg, sc.storageCfg)
	})

	t.Run("nil storage config is accepted", func(t *testing.T) {
		sc := NewShardedClient(nil, WithStorageConfig(nil))
		defer sc.Close()
		assert.Nil(t, sc.storageCfg)
	})
}

// ---------------------------------------------------------------------------
// WithVectorStorage option
// ---------------------------------------------------------------------------

func TestWithVectorStorage(t *testing.T) {
	t.Run("sets vector shard ID", func(t *testing.T) {
		sc := NewShardedClient(nil, WithVectorStorage(nil, 42))
		defer sc.Close()
		// vectorStorage field removed in Phase E (node owns vector storage).
		// Only the shard ID is tracked for config routing.
		assert.Equal(t, uint64(42), sc.vectorShardID)
	})
}

// ---------------------------------------------------------------------------
// WithTimeSeriesStorage option
// ---------------------------------------------------------------------------

func TestWithTimeSeriesStorage(t *testing.T) {
	t.Run("sets timeseries shard ID", func(t *testing.T) {
		sc := NewShardedClient(nil, WithTimeSeriesStorage(nil, 7))
		defer sc.Close()
		// timeSeriesStorage field removed in Phase E (node owns TS storage).
		// Only the shard ID is tracked for config routing.
		assert.Equal(t, uint64(7), sc.timeSeriesShardID)
	})
}

// ---------------------------------------------------------------------------
// WithDatabaseService option
// ---------------------------------------------------------------------------

func TestWithDatabaseService(t *testing.T) {
	t.Run("no-op option accepted without error", func(t *testing.T) {
		// WithDatabaseService is a no-op retained for call-site compatibility.
		// The dbService field was removed in Phase E; the qrdb node owns DB
		// management for both modes.
		sc := NewShardedClient(nil, WithDatabaseService(nil))
		defer sc.Close()
		require.NotNil(t, sc)
	})
}

// ---------------------------------------------------------------------------
// Multiple options combined
// ---------------------------------------------------------------------------

func TestShardedClient_MultipleOptions(t *testing.T) {
	t.Run("applies all options", func(t *testing.T) {
		transportCfg := &config.QRDBConfig{Transport: config.QRDBTransportEmbedded}
		storageCfg := &config.StorageConfig{Backend: config.StorageBackendMemory}

		sc := NewShardedClient(nil,
			WithTransportConfig(transportCfg),
			WithStorageConfig(storageCfg),
			WithVectorStorage(nil, 10),
			WithTimeSeriesStorage(nil, 20),
			WithDatabaseService(nil),
		)
		defer sc.Close()

		assert.Equal(t, transportCfg, sc.transportCfg)
		assert.Equal(t, storageCfg, sc.storageCfg)
		assert.Equal(t, uint64(10), sc.vectorShardID)
		assert.Equal(t, uint64(20), sc.timeSeriesShardID)
		assert.True(t, sc.IsEmbedded())
	})
}
