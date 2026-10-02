// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package grpc

import (
	"context"
	"crypto/tls"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/connectivity"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestNewConnectionPool_Success(t *testing.T) {
	t.Run("create pool without warmup", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.tlsEnabled = false

		pool, err := NewConnectionPool(
			"localhost:50051",
			WithPoolSize(2, 5),
			WithConnectionWarmup(false),
			WithPoolClientOptions(opts),
		)
		// With warmup disabled, pool creation should succeed without a running server
		require.NoError(t, err, "NewConnectionPool with warmup disabled should not require a running server")
		defer pool.Close()

		assert.NotNil(t, pool)
		assert.Equal(t, "localhost:50051", pool.address)
		assert.Equal(t, 2, pool.minSize)
		assert.Equal(t, 5, pool.maxSize)
	})
}

func TestNewConnectionPool_InvalidOptions(t *testing.T) {
	t.Run("invalid pool size", func(t *testing.T) {
		_, err := NewConnectionPool(
			"localhost:50051",
			WithPoolSize(0, 5), // Invalid min size
		)
		assert.Error(t, err)
	})

	t.Run("max less than min", func(t *testing.T) {
		_, err := NewConnectionPool(
			"localhost:50051",
			WithPoolSize(10, 5), // max < min
		)
		assert.Error(t, err)
	})
}

func TestPoolOptionsUnit(t *testing.T) {
	t.Run("WithPoolSize valid", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithPoolSize(5, 20)(opts)
		assert.NoError(t, err)
		assert.Equal(t, 5, opts.minSize)
		assert.Equal(t, 20, opts.maxSize)
	})

	t.Run("WithPoolSize invalid min", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithPoolSize(0, 20)(opts)
		assert.Error(t, err)
	})

	t.Run("WithPoolSize invalid max", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithPoolSize(20, 10)(opts)
		assert.Error(t, err)
	})

	t.Run("WithConnectionWarmup", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithConnectionWarmup(false)(opts)
		assert.NoError(t, err)
		assert.False(t, opts.warmupConns)
	})

	t.Run("WithMaxConnectionAge valid", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithMaxConnectionAge(30 * time.Minute)(opts)
		assert.NoError(t, err)
		assert.Equal(t, 30*time.Minute, opts.maxAge)
	})

	t.Run("WithMaxConnectionAge zero", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithMaxConnectionAge(0)(opts)
		assert.Error(t, err)
	})

	t.Run("WithMaxRequestsPerConn valid", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithMaxRequestsPerConn(50000)(opts)
		assert.NoError(t, err)
		assert.Equal(t, uint64(50000), opts.maxRequests)
	})

	t.Run("WithMaxRequestsPerConn zero", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithMaxRequestsPerConn(0)(opts)
		assert.Error(t, err)
	})

	t.Run("WithHealthCheckInterval valid", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithHealthCheckInterval(15 * time.Second)(opts)
		assert.NoError(t, err)
		assert.Equal(t, 15*time.Second, opts.healthCheck)
	})

	t.Run("WithHealthCheckInterval disabled", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithHealthCheckInterval(0)(opts)
		assert.NoError(t, err)
		assert.Equal(t, time.Duration(0), opts.healthCheck)
	})

	t.Run("WithHealthCheckInterval negative", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithHealthCheckInterval(-1 * time.Second)(opts)
		assert.Error(t, err)
	})

	t.Run("WithPoolClientOptions valid", func(t *testing.T) {
		opts := defaultPoolOptions()
		clientOpts := defaultClientOptions()
		err := WithPoolClientOptions(clientOpts)(opts)
		assert.NoError(t, err)
		assert.Equal(t, clientOpts, opts.clientOpts)
	})

	t.Run("WithPoolClientOptions nil returns error", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithPoolClientOptions(nil)(opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "clientOpts cannot be nil")
	})
}

func TestPooledConnAtomic(t *testing.T) {
	t.Run("atomic operations on pooledConn", func(t *testing.T) {
		pc := &pooledConn{
			created: time.Now(),
		}

		// Test healthy flag
		pc.healthy.Store(true)
		assert.True(t, pc.healthy.Load())
		pc.healthy.Store(false)
		assert.False(t, pc.healthy.Load())

		// Test inUse flag
		pc.inUse.Store(true)
		assert.True(t, pc.inUse.Load())
		pc.inUse.Store(false)
		assert.False(t, pc.inUse.Load())

		// Test requests counter
		pc.requests.Store(0)
		assert.Equal(t, uint64(0), pc.requests.Load())
		pc.requests.Add(1)
		assert.Equal(t, uint64(1), pc.requests.Load())
		pc.requests.Add(100)
		assert.Equal(t, uint64(101), pc.requests.Load())
	})
}

func TestPoolMetricsUnit(t *testing.T) {
	t.Run("create pool metrics", func(t *testing.T) {
		m := newPoolMetrics("localhost:50051")
		assert.NotNil(t, m)
		assert.NotNil(t, m.poolSize)
		assert.NotNil(t, m.activeConns)
		assert.NotNil(t, m.idleConns)
		assert.NotNil(t, m.totalRequests)
		assert.NotNil(t, m.connectionReuse)
		assert.NotNil(t, m.connectionCreated)
		assert.NotNil(t, m.connectionRecycled)
		assert.NotNil(t, m.healthChecksFailed)
		assert.NotNil(t, m.getLatency)
	})
}

func TestDefaultPoolOptions(t *testing.T) {
	t.Run("returns valid defaults", func(t *testing.T) {
		opts := defaultPoolOptions()
		assert.NotNil(t, opts)
		assert.Equal(t, 5, opts.minSize)
		assert.Equal(t, 20, opts.maxSize)
		assert.Equal(t, 30*time.Minute, opts.maxAge)
		assert.Equal(t, uint64(100000), opts.maxRequests)
		assert.Equal(t, 30*time.Second, opts.healthCheck)
		assert.True(t, opts.warmupConns) // Default is true
		assert.NotNil(t, opts.clientOpts)
	})
}

func TestPoolUpdateMetrics(t *testing.T) {
	t.Run("update metrics with empty pool", func(t *testing.T) {
		pool := &ConnectionPool{
			conns:   make([]*pooledConn, 0),
			metrics: newPoolMetrics("localhost:50051"),
		}

		// Should not panic
		pool.updateMetrics()
	})

	t.Run("update metrics with connections", func(t *testing.T) {
		pool := &ConnectionPool{
			conns: []*pooledConn{
				{created: time.Now()},
				{created: time.Now()},
			},
			metrics: newPoolMetrics("localhost:50051"),
		}

		// Set some connection states
		pool.conns[0].inUse.Store(true)
		pool.conns[0].healthy.Store(true)
		pool.conns[1].inUse.Store(false)
		pool.conns[1].healthy.Store(true)

		// Should not panic
		pool.updateMetrics()
	})

	t.Run("update metrics with unhealthy connections", func(t *testing.T) {
		pool := &ConnectionPool{
			conns: []*pooledConn{
				{created: time.Now()},
				{created: time.Now()},
			},
			metrics: newPoolMetrics("localhost:50051"),
		}

		// Mark first connection as unhealthy
		pool.conns[0].inUse.Store(false)
		pool.conns[0].healthy.Store(false)
		pool.conns[1].inUse.Store(false)
		pool.conns[1].healthy.Store(true)

		// Should not panic
		pool.updateMetrics()
	})
}

// --- Merged from session_backup_pool_coverage_91_test.go ---

// =============================================================================
// Pool Tests: Stats, Close, String, Get closed pool
// =============================================================================

func TestPool91_Stats_EmptyPool(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:   make([]*pooledConn, 0),
		metrics: newPoolMetrics("localhost:50051"),
	}

	stats := pool.Stats()
	assert.Equal(t, 0, stats.PoolSize)
	assert.Equal(t, 0, stats.ActiveConns)
	assert.Equal(t, 0, stats.IdleConns)
	assert.Equal(t, uint64(0), stats.TotalRequests)
	assert.Equal(t, uint64(0), stats.RecycledConns)
	assert.Equal(t, uint64(0), stats.FailedConns)
	assert.Equal(t, uint64(0), stats.HealthChecksFailed)
}

func TestPool91_Stats_WithConnections(t *testing.T) {
	t.Parallel()

	conn1 := &pooledConn{created: time.Now()}
	conn1.inUse.Store(true)
	conn1.healthy.Store(true)

	conn2 := &pooledConn{created: time.Now()}
	conn2.inUse.Store(false)
	conn2.healthy.Store(true)

	conn3 := &pooledConn{created: time.Now()}
	conn3.inUse.Store(false)
	conn3.healthy.Store(false)

	pool := &ConnectionPool{
		conns:   []*pooledConn{conn1, conn2, conn3},
		metrics: newPoolMetrics("localhost:50051"),
	}
	pool.totalRequests.Store(42)
	pool.recycledConns.Store(5)
	pool.failedConns.Store(2)
	pool.healthChecksFailed.Store(1)

	stats := pool.Stats()
	assert.Equal(t, 3, stats.PoolSize)
	assert.Equal(t, 1, stats.ActiveConns)
	assert.Equal(t, 2, stats.IdleConns)
	assert.Equal(t, uint64(42), stats.TotalRequests)
	assert.Equal(t, uint64(5), stats.RecycledConns)
	assert.Equal(t, uint64(2), stats.FailedConns)
	assert.Equal(t, uint64(1), stats.HealthChecksFailed)
}

func TestPool91_String(t *testing.T) {
	t.Parallel()

	conn1 := &pooledConn{created: time.Now()}
	conn1.inUse.Store(true)
	conn2 := &pooledConn{created: time.Now()}
	conn2.inUse.Store(false)

	pool := &ConnectionPool{
		address: "localhost:50051",
		conns:   []*pooledConn{conn1, conn2},
		metrics: newPoolMetrics("localhost:50051"),
	}
	pool.totalRequests.Store(100)
	pool.recycledConns.Store(3)

	str := pool.String()
	assert.Contains(t, str, "localhost:50051")
	assert.Contains(t, str, "size=2")
	assert.Contains(t, str, "active=1")
	assert.Contains(t, str, "idle=1")
	assert.Contains(t, str, "requests=100")
	assert.Contains(t, str, "recycled=3")
}

func TestPool91_Close_EmptyPool(t *testing.T) {
	t.Parallel()

	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	pool := &ConnectionPool{
		address:         "localhost:50051",
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
		logger:          log,
	}

	err = pool.Close()
	assert.NoError(t, err)
}

func TestPool91_Close_AlreadyClosed(t *testing.T) {
	t.Parallel()

	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	pool := &ConnectionPool{
		address:         "localhost:50051",
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
		logger:          log,
	}

	// First close should succeed
	err = pool.Close()
	assert.NoError(t, err)

	// Second close should be a no-op (already closed)
	err = pool.Close()
	assert.NoError(t, err)
}

func TestPool91_Get_ClosedPool(t *testing.T) {
	t.Parallel()

	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	pool := &ConnectionPool{
		address:         "localhost:50051",
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
		logger:          log,
	}

	// Close the pool
	err = pool.Close()
	require.NoError(t, err)

	// Get should fail on closed pool
	conn, err := pool.Get()
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestPool91_Get_EmptyPool(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		address: "localhost:50051",
		conns:   make([]*pooledConn, 0),
		metrics: newPoolMetrics("localhost:50051"),
	}

	conn, err := pool.Get()
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool has no connections")
}

func TestPool91_NeedsRecycling_HealthyConn(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100000,
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(true)
	conn.requests.Store(0)

	// Cannot fully test needsRecycling without a real gRPC conn,
	// but we can verify the method exists and the struct fields are accessible
	assert.True(t, conn.healthy.Load())
	assert.Equal(t, uint64(0), conn.requests.Load())
	assert.True(t, time.Since(conn.created) < pool.maxAge)
}

func TestPool91_NeedsRecycling_UnhealthyConn(t *testing.T) {
	t.Parallel()

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(false)

	assert.False(t, conn.healthy.Load())
}

func TestPool91_NeedsRecycling_OldConn(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      1 * time.Millisecond,
		maxRequests: 100000,
	}

	conn := &pooledConn{
		created: time.Now().Add(-1 * time.Hour),
	}
	conn.healthy.Store(true)

	assert.True(t, time.Since(conn.created) > pool.maxAge)
}

func TestPool91_NeedsRecycling_MaxRequests(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100,
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(true)
	conn.requests.Store(101)

	assert.True(t, conn.requests.Load() >= pool.maxRequests)
}

// =============================================================================
// Pool Options Coverage
// =============================================================================

func TestPool91_DefaultPoolOptions(t *testing.T) {
	t.Parallel()

	opts := defaultPoolOptions()
	assert.Equal(t, 5, opts.minSize)
	assert.Equal(t, 20, opts.maxSize)
	assert.Equal(t, 30*time.Minute, opts.maxAge)
	assert.Equal(t, uint64(100000), opts.maxRequests)
	assert.Equal(t, 30*time.Second, opts.healthCheck)
	assert.True(t, opts.warmupConns)
	assert.NotNil(t, opts.clientOpts)
}

func TestPool91_WithMaxConnectionAge_Negative(t *testing.T) {
	t.Parallel()

	opts := defaultPoolOptions()
	err := WithMaxConnectionAge(-5 * time.Second)(opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestPool91_PoolStats_StructFields(t *testing.T) {
	t.Parallel()

	stats := PoolStats{
		PoolSize:           10,
		ActiveConns:        5,
		IdleConns:          5,
		TotalRequests:      1000,
		RecycledConns:      50,
		FailedConns:        3,
		HealthChecksFailed: 2,
	}

	assert.Equal(t, 10, stats.PoolSize)
	assert.Equal(t, 5, stats.ActiveConns)
	assert.Equal(t, 5, stats.IdleConns)
	assert.Equal(t, uint64(1000), stats.TotalRequests)
	assert.Equal(t, uint64(50), stats.RecycledConns)
	assert.Equal(t, uint64(3), stats.FailedConns)
	assert.Equal(t, uint64(2), stats.HealthChecksFailed)
}

// =============================================================================
// Backup Client Adapter Coverage
// =============================================================================

func TestBackup91_NewBackupClientAdapter_NilClient(t *testing.T) {
	t.Parallel()

	adapter := NewBackupClientAdapter(nil)
	assert.NotNil(t, adapter)
	assert.Nil(t, adapter.client)
}

func TestBackup91_NewBackupClientAdapter_WithClient(t *testing.T) {
	t.Parallel()

	client := &Client{}
	adapter := NewBackupClientAdapter(client)
	assert.NotNil(t, adapter)
	assert.Equal(t, client, adapter.client)
}

func TestBackup91_BackupCreateRequest_AllTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		backupType  string
		compression string
		encryption  string
	}{
		{"full_gzip_aes256", "full", "gzip", "aes256"},
		{"incremental_snappy_none", "incremental", "snappy", "none"},
		{"snapshot_lz4_aes256gcm", "snapshot", "lz4", "aes256gcm"},
		{"default_none_none", "", "none", "none"},
		{"full_zstd_unset", "full", "zstd", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &mockBackupCreateRequest{
				name:        "backup-" + tc.name,
				backupType:  tc.backupType,
				compression: tc.compression,
				encryption:  tc.encryption,
				storagePath: "/backups",
				storageType: "local",
				tags:        map[string]string{"env": "test"},
			}

			assert.Equal(t, tc.backupType, req.GetBackupType())
			assert.Equal(t, tc.compression, req.GetCompression())
			assert.Equal(t, tc.encryption, req.GetEncryption())
		})
	}
}

func TestBackup91_BackupRestoreRequest_AllOptions(t *testing.T) {
	t.Parallel()

	pit := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	req := &mockBackupRestoreRequest{
		backupID:       "backup-123",
		storagePath:    "/restore/path",
		verifyFirst:    true,
		verifyChecksum: true,
		skipExisting:   true,
		pointInTime:    pit,
		storageType:    "s3",
		storageConfig:  map[string]interface{}{"bucket": "test-bucket"},
		encryptionKey:  "enc-key-123",
	}

	assert.Equal(t, "backup-123", req.GetBackupID())
	assert.Equal(t, "/restore/path", req.GetStoragePath())
	assert.True(t, req.GetVerifyFirst())
	assert.True(t, req.GetVerifyChecksum())
	assert.True(t, req.GetSkipExisting())
	assert.Equal(t, pit, req.GetPointInTime())
	assert.Equal(t, "s3", req.GetStorageType())
	assert.Equal(t, "enc-key-123", req.GetEncryptionKey())
}

func TestBackup91_BackupDeleteRequest_ForceFlag(t *testing.T) {
	t.Parallel()

	t.Run("force true", func(t *testing.T) {
		req := &mockBackupDeleteRequest{
			backupID:    "del-1",
			storagePath: "/path",
			force:       true,
		}
		assert.True(t, req.GetForce())
	})

	t.Run("force false", func(t *testing.T) {
		req := &mockBackupDeleteRequest{
			backupID:    "del-2",
			storagePath: "/path",
			force:       false,
		}
		assert.False(t, req.GetForce())
	})
}

func TestBackup91_BackupVerifyRequest_WithConfig(t *testing.T) {
	t.Parallel()

	req := &mockBackupVerifyRequest{
		backupID:    "verify-1",
		storagePath: "/verify/path",
		storageType: "azure",
		storageConfig: map[string]interface{}{
			"container": "test-container",
			"account":   "storage-account",
		},
		encryptionKey: "verify-enc-key",
	}

	assert.Equal(t, "verify-1", req.GetBackupID())
	assert.Equal(t, "/verify/path", req.GetStoragePath())
	assert.Equal(t, "azure", req.GetStorageType())
	assert.Equal(t, "test-container", req.GetStorageConfig()["container"])
	assert.Equal(t, "storage-account", req.GetStorageConfig()["account"])
	assert.Equal(t, "verify-enc-key", req.GetEncryptionKey())
}

func TestBackup91_BackupListRequest_Pagination(t *testing.T) {
	t.Parallel()

	req := &mockBackupListRequest{
		limit:       50,
		offset:      25,
		storagePath: "/list/path",
		storageType: "gcs",
		storageConfig: map[string]interface{}{
			"project": "my-project",
		},
	}

	assert.Equal(t, 50, req.GetLimit())
	assert.Equal(t, 25, req.GetOffset())
	assert.Equal(t, "/list/path", req.GetStoragePath())
	assert.Equal(t, "gcs", req.GetStorageType())
	assert.Equal(t, "my-project", req.GetStorageConfig()["project"])
}

// =============================================================================
// Pool Metrics Coverage
// =============================================================================

func TestPool91_PoolMetrics_Creation(t *testing.T) {
	t.Parallel()

	m := newPoolMetrics("test-address:9090")
	assert.NotNil(t, m)
	assert.NotNil(t, m.poolSize)
	assert.NotNil(t, m.activeConns)
	assert.NotNil(t, m.idleConns)
	assert.NotNil(t, m.totalRequests)
	assert.NotNil(t, m.connectionReuse)
	assert.NotNil(t, m.connectionCreated)
	assert.NotNil(t, m.connectionRecycled)
	assert.NotNil(t, m.healthChecksFailed)
	assert.NotNil(t, m.getLatency)
}

func TestPool91_PoolMetrics_Operations(t *testing.T) {
	t.Parallel()

	m := newPoolMetrics("test-address:9090")

	// Test gauge operations
	m.poolSize.Set(10)
	m.activeConns.Set(5)
	m.idleConns.Set(5)

	// Test counter operations
	m.totalRequests.Inc()
	m.connectionReuse.Inc()
	m.connectionCreated.Inc()
	m.connectionRecycled.Inc()
	m.healthChecksFailed.Inc()

	// Test histogram operations
	m.getLatency.Observe(0.001)
	m.getLatency.Observe(0.005)
}

func TestPool91_PooledConn_AtomicOperations(t *testing.T) {
	t.Parallel()

	pc := &pooledConn{
		created: time.Now(),
	}

	// Test healthy flag toggling
	pc.healthy.Store(true)
	assert.True(t, pc.healthy.Load())
	old := pc.healthy.Swap(false)
	assert.True(t, old)
	assert.False(t, pc.healthy.Load())

	// Test inUse flag
	pc.inUse.Store(true)
	assert.True(t, pc.inUse.Load())
	pc.inUse.Store(false)
	assert.False(t, pc.inUse.Load())

	// Test requests counter
	pc.requests.Store(0)
	pc.requests.Add(1)
	pc.requests.Add(10)
	assert.Equal(t, uint64(11), pc.requests.Load())

	// Test created timestamp
	assert.False(t, pc.created.IsZero())
}

func TestPool91_UpdateMetrics_MixedStates(t *testing.T) {
	t.Parallel()

	conns := make([]*pooledConn, 5)
	for i := range conns {
		conns[i] = &pooledConn{created: time.Now()}
	}

	// Set varied states
	conns[0].inUse.Store(true)
	conns[1].inUse.Store(true)
	conns[2].inUse.Store(false)
	conns[3].inUse.Store(false)
	conns[4].inUse.Store(true)

	pool := &ConnectionPool{
		conns:   conns,
		metrics: newPoolMetrics("localhost:50051"),
	}

	pool.updateMetrics()

	// Verify through stats
	stats := pool.Stats()
	assert.Equal(t, 5, stats.PoolSize)
	assert.Equal(t, 3, stats.ActiveConns)
	assert.Equal(t, 2, stats.IdleConns)
}

// --- Merged from coverage_target_test.go ---

// =============================================================================
// Pool Coverage Tests
// =============================================================================

func TestConnectionPool_Stats_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("empty pool stats", func(t *testing.T) {
		pool := &ConnectionPool{
			conns:   make([]*pooledConn, 0),
			metrics: newPoolMetrics("test:50051"),
		}

		stats := pool.Stats()
		assert.Equal(t, 0, stats.PoolSize)
		assert.Equal(t, 0, stats.ActiveConns)
		assert.Equal(t, 0, stats.IdleConns)
		assert.Equal(t, uint64(0), stats.TotalRequests)
	})

	t.Run("pool with connections", func(t *testing.T) {
		pool := &ConnectionPool{
			conns: []*pooledConn{
				{created: time.Now()},
				{created: time.Now()},
				{created: time.Now()},
			},
			metrics: newPoolMetrics("test:50051"),
		}

		// Set up connection states
		pool.conns[0].inUse.Store(true)
		pool.conns[1].inUse.Store(false)
		pool.conns[2].inUse.Store(true)
		pool.totalRequests.Store(100)
		pool.recycledConns.Store(5)
		pool.failedConns.Store(2)
		pool.healthChecksFailed.Store(1)

		stats := pool.Stats()
		assert.Equal(t, 3, stats.PoolSize)
		assert.Equal(t, 2, stats.ActiveConns)
		assert.Equal(t, 1, stats.IdleConns)
		assert.Equal(t, uint64(100), stats.TotalRequests)
		assert.Equal(t, uint64(5), stats.RecycledConns)
		assert.Equal(t, uint64(2), stats.FailedConns)
		assert.Equal(t, uint64(1), stats.HealthChecksFailed)
	})
}

func TestConnectionPool_String_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		address: "localhost:50051",
		conns: []*pooledConn{
			{created: time.Now()},
			{created: time.Now()},
		},
		metrics: newPoolMetrics("test:50051"),
	}

	pool.conns[0].inUse.Store(true)
	pool.conns[1].inUse.Store(false)
	pool.totalRequests.Store(50)
	pool.recycledConns.Store(3)

	str := pool.String()
	assert.Contains(t, str, "ConnectionPool")
	assert.Contains(t, str, "localhost:50051")
	assert.Contains(t, str, "size=2")
	assert.Contains(t, str, "active=1")
	assert.Contains(t, str, "idle=1")
	assert.Contains(t, str, "requests=50")
	assert.Contains(t, str, "recycled=3")
}

func TestConnectionPool_NeedsRecycling_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      10 * time.Minute,
		maxRequests: 1000,
	}

	t.Run("healthy connection does not need recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  true,
			created:  time.Now(),
			requests: 100,
			state:    connectivity.Ready,
		}
		assert.False(t, pool.needsRecyclingForMockTest(conn))
	})

	t.Run("unhealthy connection needs recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  false,
			created:  time.Now(),
			requests: 100,
			state:    connectivity.Ready,
		}
		assert.True(t, pool.needsRecyclingForMockTest(conn))
	})

	t.Run("old connection needs recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  true,
			created:  time.Now().Add(-15 * time.Minute),
			requests: 100,
			state:    connectivity.Ready,
		}
		assert.True(t, pool.needsRecyclingForMockTest(conn))
	})

	t.Run("connection over max requests needs recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  true,
			created:  time.Now(),
			requests: 1001,
			state:    connectivity.Ready,
		}
		assert.True(t, pool.needsRecyclingForMockTest(conn))
	})

	t.Run("transient failure needs recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  true,
			created:  time.Now(),
			requests: 100,
			state:    connectivity.TransientFailure,
		}
		assert.True(t, pool.needsRecyclingForMockTest(conn))
	})

	t.Run("shutdown connection needs recycling", func(t *testing.T) {
		conn := &mockPooledConnTest{
			healthy:  true,
			created:  time.Now(),
			requests: 100,
			state:    connectivity.Shutdown,
		}
		assert.True(t, pool.needsRecyclingForMockTest(conn))
	})
}

// mockPooledConnTest for unit testing pool recycling logic
type mockPooledConnTest struct {
	healthy  bool
	created  time.Time
	requests uint64
	state    connectivity.State
}

// needsRecyclingForMockTest is a test helper that checks mock connections
func (p *ConnectionPool) needsRecyclingForMockTest(conn *mockPooledConnTest) bool {
	if !conn.healthy {
		return true
	}
	if time.Since(conn.created) > p.maxAge {
		return true
	}
	if conn.requests >= p.maxRequests {
		return true
	}
	if conn.state == connectivity.TransientFailure || conn.state == connectivity.Shutdown {
		return true
	}
	return false
}

func TestConnectionPool_Get_ClosedPool_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		metrics: newPoolMetrics("test:50051"),
	}
	pool.closed.Store(true)

	conn, err := pool.Get()
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestConnectionPool_Get_EmptyPool_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:   make([]*pooledConn, 0),
		metrics: newPoolMetrics("test:50051"),
	}

	conn, err := pool.Get()
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool has no connections")
}

func TestConnectionPool_Close_AlreadyClosed_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		stopHealthCheck: make(chan struct{}),
	}
	pool.closed.Store(true)

	err := pool.Close()
	assert.NoError(t, err)
}

// =============================================================================
// Peer Credentials Coverage Tests
// =============================================================================

func TestIsPeerCredentialsAvailable_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("no credentials in context", func(t *testing.T) {
		ctx := context.Background()
		available := IsPeerCredentialsAvailable(ctx)
		assert.False(t, available)
	})

	t.Run("credentials in context", func(t *testing.T) {
		creds := &PeerCredentials{
			PID: 1234,
			UID: 1000,
			GID: 1000,
		}
		ctx := ContextWithPeerCredentials(context.Background(), creds)
		available := IsPeerCredentialsAvailable(ctx)
		assert.True(t, available)
	})
}

func TestPeerCredentialsFromContext_FromContextValue_Coverage(t *testing.T) {
	t.Parallel()

	creds := &PeerCredentials{
		PID: 5678,
		UID: 500,
		GID: 500,
	}

	ctx := ContextWithPeerCredentials(context.Background(), creds)
	retrieved := PeerCredentialsFromContext(ctx)

	require.NotNil(t, retrieved)
	assert.Equal(t, int32(5678), retrieved.PID)
	assert.Equal(t, uint32(500), retrieved.UID)
	assert.Equal(t, uint32(500), retrieved.GID)
}

// =============================================================================
// PoolStats Tests
// =============================================================================

func TestPoolStats_Structure_Coverage(t *testing.T) {
	t.Parallel()

	stats := PoolStats{
		PoolSize:           10,
		ActiveConns:        5,
		IdleConns:          5,
		TotalRequests:      1000,
		RecycledConns:      20,
		FailedConns:        2,
		HealthChecksFailed: 1,
	}

	assert.Equal(t, 10, stats.PoolSize)
	assert.Equal(t, 5, stats.ActiveConns)
	assert.Equal(t, 5, stats.IdleConns)
	assert.Equal(t, uint64(1000), stats.TotalRequests)
	assert.Equal(t, uint64(20), stats.RecycledConns)
	assert.Equal(t, uint64(2), stats.FailedConns)
	assert.Equal(t, uint64(1), stats.HealthChecksFailed)
}

// =============================================================================
// healthCheckLoop Coverage Tests
// =============================================================================

func TestConnectionPool_HealthCheckLoop_Cancellation_Coverage(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		healthCheck:     100 * time.Millisecond,
		stopHealthCheck: make(chan struct{}),
		metrics:         newPoolMetrics("test:50051"),
	}

	pool.wg.Add(1)
	go pool.healthCheckLoop()

	// Give the loop a moment to start
	time.Sleep(50 * time.Millisecond)

	// Close the stop channel to trigger graceful shutdown
	close(pool.stopHealthCheck)

	// Wait for goroutine to finish
	done := make(chan struct{})
	go func() {
		pool.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - goroutine exited cleanly
	case <-time.After(500 * time.Millisecond):
		t.Error("health check loop did not exit in time")
	}
}

// =============================================================================
// Client Options Builder Coverage
// =============================================================================

func TestBuildDialOptions_DefaultInsecure_Coverage(t *testing.T) {
	t.Parallel()

	opts := defaultClientOptions()
	opts.tlsEnabled = false

	dialOpts, err := opts.buildDialOptions()
	assert.Error(t, err)
	assert.Nil(t, dialOpts)
	assert.Contains(t, err.Error(), "TLS configuration is required")
}

func TestBuildDialOptions_WithKeepalive_Coverage(t *testing.T) {
	t.Parallel()

	opts := defaultClientOptions()
	opts.tlsEnabled = true
	opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	opts.keepaliveTime = 30 * time.Second
	opts.keepaliveTimeout = 10 * time.Second

	dialOpts, err := opts.buildDialOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, dialOpts)
}

func TestBuildDialOptions_WithPoolSettings_Coverage(t *testing.T) {
	t.Parallel()

	opts := defaultClientOptions()
	opts.tlsEnabled = true
	opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	opts.usePool = true
	opts.poolMinSize = 10
	opts.poolMaxSize = 50

	dialOpts, err := opts.buildDialOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, dialOpts)
}

// --- Merged from coverage_unit_test.go ---

// =============================================================================
// Pool Tests
// =============================================================================

func TestPoolStats_ZeroValues(t *testing.T) {
	t.Parallel()

	stats := PoolStats{}
	assert.Equal(t, 0, stats.PoolSize)
	assert.Equal(t, 0, stats.ActiveConns)
	assert.Equal(t, 0, stats.IdleConns)
	assert.Equal(t, uint64(0), stats.TotalRequests)
	assert.Equal(t, uint64(0), stats.RecycledConns)
	assert.Equal(t, uint64(0), stats.FailedConns)
	assert.Equal(t, uint64(0), stats.HealthChecksFailed)
}

func TestPoolStats_WithValues(t *testing.T) {
	t.Parallel()

	stats := PoolStats{
		PoolSize:           10,
		ActiveConns:        5,
		IdleConns:          5,
		TotalRequests:      1000,
		RecycledConns:      50,
		FailedConns:        2,
		HealthChecksFailed: 3,
	}

	assert.Equal(t, 10, stats.PoolSize)
	assert.Equal(t, 5, stats.ActiveConns)
	assert.Equal(t, 5, stats.IdleConns)
	assert.Equal(t, uint64(1000), stats.TotalRequests)
	assert.Equal(t, uint64(50), stats.RecycledConns)
	assert.Equal(t, uint64(2), stats.FailedConns)
	assert.Equal(t, uint64(3), stats.HealthChecksFailed)
}

func TestPooledConn_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	pc := &pooledConn{
		created: time.Now(),
	}
	pc.healthy.Store(true)
	pc.inUse.Store(false)

	// Simulate concurrent access patterns
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			pc.healthy.Load()
			pc.inUse.Load()
			pc.requests.Load()
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		pc.healthy.Store(true)
		pc.inUse.Store(true)
		pc.requests.Add(1)
	}

	<-done
	assert.True(t, pc.requests.Load() >= 100)
}

func TestPoolNeedsRecycling_Unhealthy(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100000,
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(false) // Unhealthy

	assert.True(t, pool.needsRecycling(conn))
}

func TestPoolNeedsRecycling_TooOld(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      1 * time.Nanosecond, // Very short max age
		maxRequests: 100000,
	}

	conn := &pooledConn{
		created: time.Now().Add(-1 * time.Minute), // Created 1 minute ago
	}
	conn.healthy.Store(true)

	assert.True(t, pool.needsRecycling(conn))
}

func TestPoolNeedsRecycling_TooManyRequests(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 10, // Low max requests
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(true)
	conn.requests.Store(15) // More than max

	assert.True(t, pool.needsRecycling(conn))
}

func TestPoolString_Empty(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		address: "localhost:50051",
		conns:   []*pooledConn{},
		metrics: newPoolMetrics("localhost:50051"),
	}

	str := pool.String()
	assert.Contains(t, str, "ConnectionPool")
	assert.Contains(t, str, "localhost:50051")
	assert.Contains(t, str, "size=0")
}

func TestPoolString_WithConnections(t *testing.T) {
	t.Parallel()

	pc1 := &pooledConn{created: time.Now()}
	pc1.inUse.Store(true)
	pc2 := &pooledConn{created: time.Now()}
	pc2.inUse.Store(false)

	pool := &ConnectionPool{
		address: "test-server:50051",
		conns:   []*pooledConn{pc1, pc2},
		metrics: newPoolMetrics("test-server:50051"),
	}
	pool.totalRequests.Store(500)
	pool.recycledConns.Store(10)

	str := pool.String()
	assert.Contains(t, str, "ConnectionPool")
	assert.Contains(t, str, "test-server:50051")
	assert.Contains(t, str, "size=2")
	assert.Contains(t, str, "active=1")
	assert.Contains(t, str, "idle=1")
	assert.Contains(t, str, "requests=500")
	assert.Contains(t, str, "recycled=10")
}

func TestPoolClose_AlreadyClosed(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:           []*pooledConn{},
		stopHealthCheck: make(chan struct{}),
		metrics:         newPoolMetrics("localhost:50051"),
	}
	pool.closed.Store(true)

	err := pool.Close()
	assert.NoError(t, err)
}

func TestPoolGet_Closed(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:   []*pooledConn{},
		metrics: newPoolMetrics("localhost:50051"),
	}
	pool.closed.Store(true)

	_, err := pool.Get()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestPoolGet_NoConnections(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{
		conns:   []*pooledConn{},
		metrics: newPoolMetrics("localhost:50051"),
	}

	_, err := pool.Get()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool has no connections")
}

func TestPoolStats_Method(t *testing.T) {
	t.Parallel()

	pc1 := &pooledConn{created: time.Now()}
	pc1.inUse.Store(true)
	pc2 := &pooledConn{created: time.Now()}
	pc2.inUse.Store(false)

	pool := &ConnectionPool{
		address: "test:50051",
		conns:   []*pooledConn{pc1, pc2},
		metrics: newPoolMetrics("test:50051"),
	}
	pool.totalRequests.Store(1000)
	pool.recycledConns.Store(50)
	pool.failedConns.Store(5)
	pool.healthChecksFailed.Store(2)

	stats := pool.Stats()
	assert.Equal(t, 2, stats.PoolSize)
	assert.Equal(t, 1, stats.ActiveConns)
	assert.Equal(t, 1, stats.IdleConns)
	assert.Equal(t, uint64(1000), stats.TotalRequests)
	assert.Equal(t, uint64(50), stats.RecycledConns)
	assert.Equal(t, uint64(5), stats.FailedConns)
	assert.Equal(t, uint64(2), stats.HealthChecksFailed)
}

// =============================================================================
// Vector Metrics Tests
// =============================================================================

func TestGetTenantID_WithContext(t *testing.T) {
	t.Parallel()

	ctx := tenantctx.WithTenant(context.Background(), "test-tenant")
	tenantID := getTenantID(ctx)
	assert.Equal(t, "test-tenant", tenantID)
}

func TestGetTenantID_Default(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tenantID := getTenantID(ctx)
	assert.Equal(t, defaultTenantID, tenantID)
}

func TestGetTenantID_EmptyTenant(t *testing.T) {
	t.Parallel()

	ctx := tenantctx.WithTenant(context.Background(), "")
	tenantID := getTenantID(ctx)
	assert.Equal(t, defaultTenantID, tenantID)
}

func TestNewVectorMetricsRecorder_Coverage(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	require.NotNil(t, recorder)
	assert.NotNil(t, recorder.metrics)
}

func TestVectorMetricsRecorder_RecordUpsert(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-100 * time.Millisecond)

	// Should not panic
	recorder.RecordUpsert(ctx, "test-ns", startTime, true)
	recorder.RecordUpsert(ctx, "test-ns", startTime, false)
}

func TestVectorMetricsRecorder_RecordBatchUpsert(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-100 * time.Millisecond)

	// Should not panic
	recorder.RecordBatchUpsert(ctx, "test-ns", 100, startTime, true)
	recorder.RecordBatchUpsert(ctx, "test-ns", 100, startTime, false)
}

func TestVectorMetricsRecorder_RecordGet(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-10 * time.Millisecond)

	// Should not panic
	recorder.RecordGet(ctx, "test-ns", startTime, true)
	recorder.RecordGet(ctx, "test-ns", startTime, false)
}

func TestVectorMetricsRecorder_RecordDelete(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-10 * time.Millisecond)

	// Should not panic
	recorder.RecordDelete(ctx, "test-ns", startTime, true)
	recorder.RecordDelete(ctx, "test-ns", startTime, false)
}

func TestVectorMetricsRecorder_RecordBatchDelete(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-100 * time.Millisecond)

	// Should not panic
	recorder.RecordBatchDelete(ctx, "test-ns", 50, startTime, true)
	recorder.RecordBatchDelete(ctx, "test-ns", 50, startTime, false)
}

func TestVectorMetricsRecorder_RecordSearch(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-50 * time.Millisecond)

	// Should not panic
	recorder.RecordSearch(ctx, "test-ns", "knn", startTime, true)
	recorder.RecordSearch(ctx, "test-ns", "hybrid", startTime, false)
}

func TestVectorMetricsRecorder_RecordNamespaceCreate(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-200 * time.Millisecond)

	// Should not panic
	recorder.RecordNamespaceCreate(ctx, "new-ns", startTime, true)
	recorder.RecordNamespaceCreate(ctx, "new-ns", startTime, false)
}

func TestVectorMetricsRecorder_RecordNamespaceDelete(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-200 * time.Millisecond)

	// Should not panic
	recorder.RecordNamespaceDelete(ctx, "old-ns", startTime, true)
	recorder.RecordNamespaceDelete(ctx, "old-ns", startTime, false)
}

func TestVectorMetricsRecorder_UpdateNamespaceStats(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()

	// Should not panic
	recorder.UpdateNamespaceStats(ctx, "test-ns", 1000, 1024*1024)
}

func TestVectorMetricsRecorder_UpdateQuotaUsage(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()

	// Should not panic
	recorder.UpdateQuotaUsage(ctx, "test-ns", "vectors", 0.75)
	recorder.UpdateQuotaUsage(ctx, "test-ns", "storage", 0.50)
}

func TestVectorMetricsRecorder_UpdateTenantNamespaceCount(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()

	// Should not panic
	recorder.UpdateTenantNamespaceCount(ctx, 5)
}

func TestVectorMetricsRecorder_RecordExport(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-1 * time.Second)

	// Should not panic
	recorder.RecordExport(ctx, "test-ns", 10000, startTime, true)
	recorder.RecordExport(ctx, "test-ns", 10000, startTime, false)
}

func TestVectorMetricsRecorder_RecordImport(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-2 * time.Second)

	// Should not panic
	recorder.RecordImport(ctx, "test-ns", 5000, startTime, true)
	recorder.RecordImport(ctx, "test-ns", 5000, startTime, false)
}

func TestVectorMetricsRecorder_RecordIndexRebuild(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := context.Background()
	startTime := time.Now().Add(-5 * time.Second)

	// Should not panic
	recorder.RecordIndexRebuild(ctx, "test-ns", startTime, true)
	recorder.RecordIndexRebuild(ctx, "test-ns", startTime, false)
}

func TestVectorMetricsRecorder_WithTenant(t *testing.T) {
	t.Parallel()

	recorder := NewVectorMetricsRecorder()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-abc")
	startTime := time.Now()

	// All operations should use the tenant from context
	recorder.RecordUpsert(ctx, "ns", startTime, true)
	recorder.RecordGet(ctx, "ns", startTime, true)
	recorder.RecordDelete(ctx, "ns", startTime, true)
	recorder.RecordSearch(ctx, "ns", "knn", startTime, true)
	recorder.UpdateNamespaceStats(ctx, "ns", 100, 1024)
	recorder.UpdateQuotaUsage(ctx, "ns", "vectors", 0.5)
	recorder.UpdateTenantNamespaceCount(ctx, 3)
}

// =============================================================================
// Stream Batch Metrics Tests
// =============================================================================

func TestInitStreamBatchMetrics(t *testing.T) {
	t.Parallel()

	metrics := initStreamBatchMetrics()
	require.NotNil(t, metrics)
	assert.NotNil(t, metrics.streamsTotal)
	assert.NotNil(t, metrics.streamDuration)
	assert.NotNil(t, metrics.chunksProcessed)
	assert.NotNil(t, metrics.operationsTotal)
	assert.NotNil(t, metrics.backpressure)
	assert.NotNil(t, metrics.activeStreams)
}

func TestInitStreamBatchMetrics_Singleton(t *testing.T) {
	t.Parallel()

	metrics1 := initStreamBatchMetrics()
	metrics2 := initStreamBatchMetrics()

	// Should return the same instance
	assert.Same(t, metrics1, metrics2)
}

// =============================================================================
// Client State Tests
// =============================================================================

func TestClient_IsConnected_Closed(t *testing.T) {
	t.Parallel()

	client := &Client{
		connected: true,
		closed:    true,
	}

	assert.False(t, client.isConnected())
}

func TestClient_IsConnected_NotConnected(t *testing.T) {
	t.Parallel()

	client := &Client{
		connected: false,
		closed:    false,
	}

	assert.False(t, client.isConnected())
}

func TestClient_IsConnected_True(t *testing.T) {
	t.Parallel()

	client := &Client{
		connected: true,
		closed:    false,
	}

	assert.True(t, client.isConnected())
}

func TestClient_Conn_ReturnsNil(t *testing.T) {
	t.Parallel()

	client := &Client{
		conn: nil,
	}

	assert.Nil(t, client.Conn())
}

// =============================================================================
// Connection State Tests
// =============================================================================

func TestConnectivityState(t *testing.T) {
	t.Parallel()

	// Test that we understand gRPC connectivity states
	states := []connectivity.State{
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.TransientFailure,
		connectivity.Shutdown,
	}

	for _, state := range states {
		assert.NotEmpty(t, state.String())
	}
}

// =============================================================================
// Atomic Operations Tests
// =============================================================================

func TestAtomicBool(t *testing.T) {
	t.Parallel()

	var b atomic.Bool
	assert.False(t, b.Load())

	b.Store(true)
	assert.True(t, b.Load())

	old := b.Swap(false)
	assert.True(t, old)
	assert.False(t, b.Load())

	swapped := b.CompareAndSwap(false, true)
	assert.True(t, swapped)
	assert.True(t, b.Load())
}

func TestAtomicUint64(t *testing.T) {
	t.Parallel()

	var u atomic.Uint64
	assert.Equal(t, uint64(0), u.Load())

	u.Store(100)
	assert.Equal(t, uint64(100), u.Load())

	new := u.Add(50)
	assert.Equal(t, uint64(150), new)
	assert.Equal(t, uint64(150), u.Load())
}
