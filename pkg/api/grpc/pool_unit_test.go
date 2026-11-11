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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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

	t.Run("WithMaxConnectionAge negative", func(t *testing.T) {
		opts := defaultPoolOptions()
		err := WithMaxConnectionAge(-5 * time.Second)(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "positive")
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

// --- Merged from session_backup_pool_coverage_91_test.go ---

// =============================================================================
// Pool Tests: Stats, Close, String, Get closed pool
// =============================================================================

func TestConnectionPool_Stats(t *testing.T) {
	t.Parallel()

	newConn := func(inUse bool) *pooledConn {
		conn := &pooledConn{created: time.Now()}
		conn.inUse.Store(inUse)
		return conn
	}

	tests := []struct {
		name  string
		conns []*pooledConn
		want  PoolStats
	}{
		{
			name:  "empty pool",
			conns: []*pooledConn{},
			want:  PoolStats{},
		},
		{
			name:  "active and idle connections with counters",
			conns: []*pooledConn{newConn(true), newConn(false), newConn(false)},
			want: PoolStats{
				PoolSize:           3,
				ActiveConns:        1,
				IdleConns:          2,
				TotalRequests:      42,
				RecycledConns:      5,
				FailedConns:        2,
				HealthChecksFailed: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &ConnectionPool{
				conns:   tt.conns,
				metrics: newPoolMetrics("localhost:50051"),
			}
			pool.totalRequests.Store(tt.want.TotalRequests)
			pool.recycledConns.Store(tt.want.RecycledConns)
			pool.failedConns.Store(tt.want.FailedConns)
			pool.healthChecksFailed.Store(tt.want.HealthChecksFailed)

			assert.Equal(t, tt.want, pool.Stats())
		})
	}
}

func TestConnectionPool_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		active []bool
		want   []string
	}{
		{
			name: "empty pool",
			want: []string{"ConnectionPool", "localhost:50051", "size=0"},
		},
		{
			name:   "active and idle connections",
			active: []bool{true, false},
			want: []string{"ConnectionPool", "localhost:50051", "size=2", "active=1",
				"idle=1", "requests=100", "recycled=3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &ConnectionPool{
				address: "localhost:50051",
				conns:   []*pooledConn{},
				metrics: newPoolMetrics("localhost:50051"),
			}
			for _, inUse := range tt.active {
				conn := &pooledConn{created: time.Now()}
				conn.inUse.Store(inUse)
				pool.conns = append(pool.conns, conn)
			}
			if len(tt.active) > 0 {
				pool.totalRequests.Store(100)
				pool.recycledConns.Store(3)
			}

			str := pool.String()
			for _, want := range tt.want {
				assert.Contains(t, str, want)
			}
		})
	}
}

func TestConnectionPool_Close(t *testing.T) {
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

func TestConnectionPool_Get_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		closed  bool
		wantErr string
	}{
		{name: "closed pool", closed: true, wantErr: "pool is closed"},
		{name: "pool with no connections", closed: false, wantErr: "pool has no connections"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &ConnectionPool{
				address: "localhost:50051",
				conns:   make([]*pooledConn, 0),
				metrics: newPoolMetrics("localhost:50051"),
			}
			pool.closed.Store(tt.closed)

			conn, err := pool.Get()
			require.Error(t, err)
			assert.Nil(t, conn)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConnectionPool_NeedsRecycling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		healthy  bool
		age      time.Duration
		requests uint64
		closed   bool
		want     bool
	}{
		{name: "healthy idle connection is kept", healthy: true, requests: 100, want: false},
		{name: "unhealthy connection is recycled", healthy: false, requests: 100, want: true},
		{name: "connection older than max age is recycled", healthy: true, age: 15 * time.Minute, requests: 100, want: true},
		{name: "connection at max requests is recycled", healthy: true, requests: 1000, want: true},
		{name: "shut down connection is recycled", healthy: true, requests: 100, closed: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &ConnectionPool{
				maxAge:      10 * time.Minute,
				maxRequests: 1000,
			}

			// NewClient does not dial, so the connection stays Idle until
			// it is closed, which moves it to Shutdown.
			cc, err := grpclib.NewClient("passthrough:///localhost:1",
				grpclib.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			if tt.closed {
				require.NoError(t, cc.Close())
			} else {
				t.Cleanup(func() { assert.NoError(t, cc.Close()) })
			}

			conn := &pooledConn{
				conn:    cc,
				created: time.Now().Add(-tt.age),
			}
			conn.healthy.Store(tt.healthy)
			conn.requests.Store(tt.requests)

			assert.Equal(t, tt.want, pool.needsRecycling(conn))
		})
	}
}

// =============================================================================
// Pool Options Coverage
// =============================================================================

// =============================================================================
// Backup Client Adapter Coverage
// =============================================================================

func TestNewBackupClientAdapter_Clients(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client *Client
	}{
		{name: "nil client", client: nil},
		{name: "with client", client: &Client{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := NewBackupClientAdapter(tt.client)
			require.NotNil(t, adapter)
			assert.Same(t, tt.client, adapter.client)
		})
	}
}

// =============================================================================
// Pool Metrics Coverage
// =============================================================================

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

		retrieved := PeerCredentialsFromContext(ctx)
		require.NotNil(t, retrieved)
		assert.Equal(t, int32(1234), retrieved.PID)
		assert.Equal(t, uint32(1000), retrieved.UID)
		assert.Equal(t, uint32(1000), retrieved.GID)
	})
}

// =============================================================================
// PoolStats Tests
// =============================================================================

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

func TestBuildDialOptions_TLSModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(opts *clientOptions)
		wantErr string
	}{
		{
			name:    "TLS disabled is rejected",
			modify:  func(opts *clientOptions) { opts.tlsEnabled = false },
			wantErr: "TLS configuration is required",
		},
		{
			name: "TLS with keepalive",
			modify: func(opts *clientOptions) {
				opts.tlsEnabled = true
				opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
				opts.keepaliveTime = 30 * time.Second
				opts.keepaliveTimeout = 10 * time.Second
			},
		},
		{
			name: "TLS with pool settings",
			modify: func(opts *clientOptions) {
				opts.tlsEnabled = true
				opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
				opts.usePool = true
				opts.poolMinSize = 10
				opts.poolMaxSize = 50
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := defaultClientOptions()
			tt.modify(opts)

			dialOpts, err := opts.buildDialOptions()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Nil(t, dialOpts)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, dialOpts)
		})
	}
}

// --- Merged from coverage_unit_test.go ---

// =============================================================================
// Pool Tests
// =============================================================================

// =============================================================================
// Vector Metrics Tests
// =============================================================================

func TestGetTenantID_Fallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "tenant from context", ctx: tenantctx.WithTenant(context.Background(), "test-tenant"), want: "test-tenant"},
		{name: "no tenant falls back to default", ctx: context.Background(), want: defaultTenantID},
		{name: "empty tenant falls back to default", ctx: tenantctx.WithTenant(context.Background(), ""), want: defaultTenantID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, getTenantID(tt.ctx))
		})
	}
}

func TestVectorMetricsRecorder(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-100 * time.Millisecond)
	tests := []struct {
		name   string
		record func(r *VectorMetricsRecorder, ctx context.Context, success bool)
	}{
		{name: "RecordUpsert", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordUpsert(ctx, "test-ns", start, success)
		}},
		{name: "RecordBatchUpsert", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordBatchUpsert(ctx, "test-ns", 100, start, success)
		}},
		{name: "RecordGet", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordGet(ctx, "test-ns", start, success)
		}},
		{name: "RecordDelete", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordDelete(ctx, "test-ns", start, success)
		}},
		{name: "RecordBatchDelete", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordBatchDelete(ctx, "test-ns", 50, start, success)
		}},
		{name: "RecordSearch", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordSearch(ctx, "test-ns", "knn", start, success)
		}},
		{name: "RecordNamespaceCreate", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordNamespaceCreate(ctx, "new-ns", start, success)
		}},
		{name: "RecordNamespaceDelete", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordNamespaceDelete(ctx, "old-ns", start, success)
		}},
		{name: "UpdateNamespaceStats", record: func(r *VectorMetricsRecorder, ctx context.Context, _ bool) {
			r.UpdateNamespaceStats(ctx, "test-ns", 1000, 1024*1024)
		}},
		{name: "UpdateQuotaUsage", record: func(r *VectorMetricsRecorder, ctx context.Context, _ bool) {
			r.UpdateQuotaUsage(ctx, "test-ns", "vectors", 0.75)
		}},
		{name: "UpdateTenantNamespaceCount", record: func(r *VectorMetricsRecorder, ctx context.Context, _ bool) {
			r.UpdateTenantNamespaceCount(ctx, 5)
		}},
		{name: "RecordExport", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordExport(ctx, "test-ns", 10000, start, success)
		}},
		{name: "RecordImport", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordImport(ctx, "test-ns", 5000, start, success)
		}},
		{name: "RecordIndexRebuild", record: func(r *VectorMetricsRecorder, ctx context.Context, success bool) {
			r.RecordIndexRebuild(ctx, "test-ns", start, success)
		}},
	}

	recorder := NewVectorMetricsRecorder()
	require.NotNil(t, recorder)
	require.NotNil(t, recorder.metrics)

	contexts := []context.Context{
		context.Background(),
		tenantctx.WithTenant(context.Background(), "tenant-abc"),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The recorder has no return value; each call must record its
			// success and failure outcome, with and without a tenant, without
			// panicking.
			for _, ctx := range contexts {
				assert.NotPanics(t, func() { tt.record(recorder, ctx, true) })
				assert.NotPanics(t, func() { tt.record(recorder, ctx, false) })
			}
		})
	}
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

	// Later calls return the same instance.
	assert.Same(t, metrics, initStreamBatchMetrics())
}

// =============================================================================
// Client State Tests
// =============================================================================

func TestClient_IsConnected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		connected bool
		closed    bool
		want      bool
	}{
		{name: "connected and open", connected: true, closed: false, want: true},
		{name: "connected but closed", connected: true, closed: true, want: false},
		{name: "not connected", connected: false, closed: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{connected: tt.connected, closed: tt.closed}
			assert.Equal(t, tt.want, client.isConnected())
		})
	}
}

func TestClient_Conn_ReturnsNil(t *testing.T) {
	t.Parallel()

	client := &Client{
		conn: nil,
	}

	assert.Nil(t, client.Conn())
}
