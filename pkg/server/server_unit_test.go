//go:build !integration && rest

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ---------------------------------------------------------------------------
// HealthChecker tests
// ---------------------------------------------------------------------------

func TestHealthChecker_CheckGRPC_NilServer(t *testing.T) {
	t.Run("unhealthy when grpc server nil", func(t *testing.T) {
		srv := withTempServer(t)
		h := srv.GetHealthChecker()

		health := h.CheckGRPC()
		assert.Equal(t, HealthStatusUnhealthy, health.Status)
		assert.Equal(t, "grpc", health.Name)
		assert.Contains(t, health.Message, "not initialized")
	})
}

func TestHealthChecker_CheckGRPC_NilListener(t *testing.T) {
	t.Run("unhealthy when grpc listener nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.grpcServer = ggrpc.NewServer()
		defer srv.grpcServer.Stop()
		srv.grpcListener = nil

		h := srv.GetHealthChecker()
		health := h.CheckGRPC()
		assert.Equal(t, HealthStatusUnhealthy, health.Status)
		assert.Contains(t, health.Message, "listener")
	})
}

func TestHealthChecker_CheckGRPC_Healthy(t *testing.T) {
	t.Run("healthy when grpc server and listener set", func(t *testing.T) {
		srv := withTempServer(t)
		srv.grpcServer = ggrpc.NewServer()
		defer srv.grpcServer.Stop()
		srv.grpcListener = &dummyListener{}

		h := srv.GetHealthChecker()
		health := h.CheckGRPC()
		assert.Equal(t, HealthStatusHealthy, health.Status)
		assert.Contains(t, health.Message, "listening")
	})
}

func TestHealthChecker_CheckREST_NilServer(t *testing.T) {
	t.Run("unhealthy when rest server nil", func(t *testing.T) {
		srv := withTempServer(t)
		h := srv.GetHealthChecker()

		health := h.CheckREST()
		assert.Equal(t, HealthStatusUnhealthy, health.Status)
		assert.Equal(t, "rest", health.Name)
		assert.Contains(t, health.Message, "not initialized")
	})
}

func TestHealthChecker_GetOverallHealth_AllUnhealthy(t *testing.T) {
	t.Run("returns unhealthy when no servers", func(t *testing.T) {
		srv := withTempServer(t)
		h := srv.GetHealthChecker()

		overall := h.GetOverallHealth()
		require.NotNil(t, overall)
		// Both grpc and rest are unhealthy = 2 unhealthy = overall unhealthy
		assert.Equal(t, HealthStatusUnhealthy, overall.Status)
		assert.Contains(t, overall.Components, "grpc")
		assert.Contains(t, overall.Components, "rest")
		assert.True(t, overall.Uptime >= 0)
	})
}

func TestHealthChecker_GetOverallHealth_Degraded(t *testing.T) {
	t.Run("returns degraded with one unhealthy component", func(t *testing.T) {
		srv := withTempServer(t)
		// Make grpc look healthy by setting both server and listener
		srv.grpcServer = ggrpc.NewServer()
		defer srv.grpcServer.Stop()
		srv.grpcListener = &dummyListener{}

		h := srv.GetHealthChecker()
		overall := h.GetOverallHealth()

		// grpc = healthy, rest = unhealthy => 1 unhealthy = degraded
		assert.Equal(t, HealthStatusDegraded, overall.Status)
	})
}

func TestHealthChecker_IsHealthy(t *testing.T) {
	t.Run("returns false when servers not running", func(t *testing.T) {
		srv := withTempServer(t)
		h := srv.GetHealthChecker()
		assert.False(t, h.IsHealthy())
	})
}

func TestHealthChecker_IsReady(t *testing.T) {
	t.Run("returns false when servers not running", func(t *testing.T) {
		srv := withTempServer(t)
		h := srv.GetHealthChecker()
		assert.False(t, h.IsReady())
	})
}

// ---------------------------------------------------------------------------
// Server accessor tests
// ---------------------------------------------------------------------------

func TestServer_GetEffectiveMode(t *testing.T) {
	t.Run("returns standalone when mode empty", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Server.Mode = ""

		assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())
	})

	t.Run("returns configured mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Server.Mode = "custom"

		assert.Equal(t, "custom", srv.GetEffectiveMode())
	})
}

func TestServer_Accessors(t *testing.T) {
	srv := withTempServer(t)

	t.Run("GetConfig returns config", func(t *testing.T) {
		assert.NotNil(t, srv.GetConfig())
	})

	t.Run("GetRESTServer returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetRESTServer())
	})

	t.Run("GetGRPCServer returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetGRPCServer())
	})

	t.Run("GetCatalogManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetCatalogManager())
	})

	t.Run("GetBackendManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetBackendManager())
	})

	t.Run("GetWorkerManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetWorkerManager())
	})

	t.Run("GetDevClusterManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetDevClusterManager())
	})

	t.Run("GetRAGSystem returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetRAGSystem())
	})

	t.Run("GetEmbeddingManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetEmbeddingManager())
	})

	t.Run("GetWatchBridge returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetWatchBridge())
	})

	t.Run("GetBackupManager returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetBackupManager())
	})

	t.Run("GetQRDBClient returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetQRDBClient())
	})

	t.Run("Service returns nil before start", func(t *testing.T) {
		assert.Nil(t, srv.Service())
	})

	t.Run("Uptime is positive", func(t *testing.T) {
		assert.True(t, srv.Uptime() > 0)
	})

	t.Run("IsStarted returns false before start", func(t *testing.T) {
		assert.False(t, srv.IsStarted())
	})

	t.Run("IsClosed returns false before close", func(t *testing.T) {
		assert.False(t, srv.IsClosed())
	})
}

// ---------------------------------------------------------------------------
// TransportFactory tests
// ---------------------------------------------------------------------------

func TestTransportFactory_IsEmbedded(t *testing.T) {
	t.Run("nil config is embedded", func(t *testing.T) {
		f := newTransportFactory(nil, nil, nil)
		assert.True(t, f.isEmbedded())
	})

	t.Run("embedded transport config", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "embedded"}
		f := newTransportFactory(cfg, nil, nil)
		assert.True(t, f.isEmbedded())
	})

	t.Run("grpc transport is not embedded", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "grpc"}
		f := newTransportFactory(cfg, nil, nil)
		assert.False(t, f.isEmbedded())
	})
}

func TestTransportFactory_TransportType(t *testing.T) {
	t.Run("nil config returns embedded", func(t *testing.T) {
		f := newTransportFactory(nil, nil, nil)
		assert.Equal(t, TransportEmbedded, f.transportType())
	})

	t.Run("empty transport returns embedded", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: ""}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportEmbedded, f.transportType())
	})

	t.Run("grpc transport", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "grpc"}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportGRPC, f.transportType())
	})

	t.Run("rest transport", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "rest"}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportREST, f.transportType())
	})

	t.Run("quic transport", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "quic"}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportQUIC, f.transportType())
	})

	t.Run("mcp transport", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "mcp"}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportMCP, f.transportType())
	})

	t.Run("unix transport", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "unix"}
		f := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportUnix, f.transportType())
	})
}

func TestTransportFactory_ValidateNetworkConfig(t *testing.T) {
	t.Run("unix transport missing socket", func(t *testing.T) {
		f := newTransportFactory(&config.QRDBConfig{}, nil, nil)
		err := f.validateNetworkConfig(TransportUnix)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnixTransportMissingSocket))
	})

	t.Run("unix transport with socket", func(t *testing.T) {
		cfg := &config.QRDBConfig{UnixSocketPath: "/tmp/test.sock"}
		f := newTransportFactory(cfg, nil, nil)
		err := f.validateNetworkConfig(TransportUnix)
		assert.NoError(t, err)
	})

	t.Run("unix transport nil config", func(t *testing.T) {
		f := newTransportFactory(nil, nil, nil)
		err := f.validateNetworkConfig(TransportUnix)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnixTransportMissingSocket))
	})

	t.Run("grpc transport missing address", func(t *testing.T) {
		f := newTransportFactory(&config.QRDBConfig{}, nil, nil)
		err := f.validateNetworkConfig(TransportGRPC)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNetworkTransportMissingAddress))
	})

	t.Run("grpc transport with address", func(t *testing.T) {
		cfg := &config.QRDBConfig{Address: "localhost:9090"}
		f := newTransportFactory(cfg, nil, nil)
		err := f.validateNetworkConfig(TransportGRPC)
		assert.NoError(t, err)
	})

	t.Run("rest transport missing address nil config", func(t *testing.T) {
		f := newTransportFactory(nil, nil, nil)
		err := f.validateNetworkConfig(TransportREST)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNetworkTransportMissingAddress))
	})
}

func TestBuildTransportOptions(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		opts := buildTransportOptions(nil)
		assert.Nil(t, opts)
	})

	t.Run("unix transport uses socket path", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:      config.QRDBTransportUnix,
			UnixSocketPath: "/tmp/qrdb.sock",
		}
		opts := buildTransportOptions(cfg)
		assert.NotNil(t, opts)
		assert.Len(t, opts, 1)
	})

	t.Run("network transport uses address", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport: "grpc",
			Address:   "localhost:9090",
		}
		opts := buildTransportOptions(cfg)
		assert.NotNil(t, opts)
		assert.Len(t, opts, 1)
	})

	t.Run("tls options added when enabled", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:   "grpc",
			Address:     "localhost:9090",
			TLSEnabled:  true,
			TLSCertFile: "/tmp/cert.pem",
			TLSKeyFile:  "/tmp/key.pem",
			TLSCAFile:   "/tmp/ca.pem",
		}
		opts := buildTransportOptions(cfg)
		assert.Len(t, opts, 2) // address + TLS
	})

	t.Run("empty address produces no address option", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport: "grpc",
			Address:   "",
		}
		opts := buildTransportOptions(cfg)
		assert.Nil(t, opts)
	})

	t.Run("unix transport empty socket produces no options", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:      config.QRDBTransportUnix,
			UnixSocketPath: "",
		}
		opts := buildTransportOptions(cfg)
		assert.Nil(t, opts)
	})
}

// ---------------------------------------------------------------------------
// ShardedClient tests
// ---------------------------------------------------------------------------

func TestShardedClient_Close(t *testing.T) {
	t.Run("close is idempotent", func(t *testing.T) {
		sc := NewShardedClient(nil)
		err := sc.Close()
		assert.NoError(t, err)

		// Second close returns nil
		err = sc.Close()
		assert.NoError(t, err)
	})
}

func TestShardedClient_IsEmbedded(t *testing.T) {
	t.Run("nil config is embedded", func(t *testing.T) {
		sc := NewShardedClient(nil)
		assert.True(t, sc.IsEmbedded())
	})

	t.Run("grpc config is not embedded", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: "grpc", Address: "localhost:9090"}
		sc := NewShardedClient(nil, WithTransportConfig(cfg))
		assert.False(t, sc.IsEmbedded())
	})
}

func TestShardedClient_ForShard_EmbeddedReturnsError(t *testing.T) {
	t.Run("bare embedded transport returns error for ForShard", func(t *testing.T) {
		// Phase E removed the legacy in-memory baasTransport path. A bare
		// ShardedClient (no node-backed client, no transport address) can no
		// longer materialize a per-shard client: ForShard surfaces the
		// "address is required for embedded qrdb transport" error from the
		// transport factory. Node-backed clients short-circuit this via nodeClient.
		sc := NewShardedClient(nil)
		t.Cleanup(func() { assert.NoError(t, sc.Close()) })
		client, err := sc.ForShard(1)
		require.Error(t, err)
		assert.Nil(t, client)
	})

	t.Run("embedded transport returns closed error after close", func(t *testing.T) {
		// After Close, ForShard returns ShardedClientClosedError.
		sc := NewShardedClient(nil)
		require.NoError(t, sc.Close())
		_, err := sc.ForShard(1)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrShardedClientClosed))
	})
}

// ---------------------------------------------------------------------------
// TestableApp tests
// ---------------------------------------------------------------------------

func TestTestableApp(t *testing.T) {
	srv := withTempServer(t)
	app := NewTestableApp(srv)

	t.Run("GetServer returns server", func(t *testing.T) {
		assert.Equal(t, srv, app.GetServer())
	})

	t.Run("WaitForLeader returns nil", func(t *testing.T) {
		assert.NoError(t, app.WaitForLeader(1))
	})

	t.Run("GetLeaderID returns zero", func(t *testing.T) {
		id, err := app.GetLeaderID(1)
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id)
	})

	t.Run("IsLeader returns false", func(t *testing.T) {
		assert.False(t, app.IsLeader(1))
	})

	t.Run("GetNodeHostInfo returns nil", func(t *testing.T) {
		assert.Nil(t, app.GetNodeHostInfo())
	})

	t.Run("WaitForShards returns nil", func(t *testing.T) {
		assert.NoError(t, app.WaitForShards([]uint64{1, 2, 3}))
	})
}

// ---------------------------------------------------------------------------
// Additional error type tests
// ---------------------------------------------------------------------------

func TestQRDBClientNotInitializedError(t *testing.T) {
	t.Run("error message", func(t *testing.T) {
		err := &QRDBClientNotInitializedError{}
		assert.Equal(t, "GetKVStore called before qrdb SDK client initialized", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &QRDBClientNotInitializedError{}
		assert.True(t, errors.Is(err, ErrQRDBClientNotInitialized))
	})
}

func TestShardClientError(t *testing.T) {
	t.Run("error message includes cause", func(t *testing.T) {
		cause := &ServerMessageError{Message: "transport down"}
		err := &ShardClientError{ShardID: 42, Cause: cause}
		assert.Contains(t, err.Error(), "transport down")
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		cause := &ServerMessageError{Message: "x"}
		err := &ShardClientError{ShardID: 1, Cause: cause}
		assert.True(t, errors.Is(err, ErrShardClientFailed))
	})
}

// ---------------------------------------------------------------------------
// kvShards tests
// ---------------------------------------------------------------------------

func TestKvShards(t *testing.T) {
	t.Run("empty shards returns default", func(t *testing.T) {
		result := kvShards(nil)
		assert.Equal(t, []uint64{1}, result)
	})

	t.Run("empty shards slice returns default", func(t *testing.T) {
		result := kvShards([]uint64{})
		assert.Equal(t, []uint64{1}, result)
	})

	t.Run("excludes specified shards", func(t *testing.T) {
		result := kvShards([]uint64{1, 2, 3, 4}, 2, 4)
		assert.Equal(t, []uint64{1, 3}, result)
	})

	t.Run("excludes all shards returns default", func(t *testing.T) {
		result := kvShards([]uint64{1, 2}, 1, 2)
		assert.Equal(t, []uint64{1}, result)
	})

	t.Run("exclude zero shards are ignored", func(t *testing.T) {
		result := kvShards([]uint64{1, 2, 3}, 0)
		assert.Equal(t, []uint64{1, 2, 3}, result)
	})

	t.Run("no exclusions returns all shards", func(t *testing.T) {
		result := kvShards([]uint64{5, 10, 15})
		assert.Equal(t, []uint64{5, 10, 15}, result)
	})
}

func TestValidateRouting(t *testing.T) {
	t.Run("always returns nil (stub)", func(t *testing.T) {
		err := validateRouting([]uint64{1, 2, 3}, map[string]uint64{"entity": 1})
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Lifecycle noop/stub method tests
// ---------------------------------------------------------------------------

func TestServer_LifecycleNoopMethods(t *testing.T) {
	srv := withTempServer(t)

	t.Run("startStorage returns nil", func(t *testing.T) {
		assert.NoError(t, srv.startStorage())
	})

	t.Run("startIndexes returns nil", func(t *testing.T) {
		assert.NoError(t, srv.startIndexes())
	})

	t.Run("stopBootstrap returns nil", func(t *testing.T) {
		assert.NoError(t, srv.stopBootstrap())
	})

	t.Run("stopDevCluster returns nil", func(t *testing.T) {
		assert.NoError(t, srv.stopDevCluster(context.Background()))
	})

	t.Run("initTimeSeriesStorage returns nil", func(t *testing.T) {
		assert.NoError(t, srv.initTimeSeriesStorage())
	})

	t.Run("startTimeSeries returns nil", func(t *testing.T) {
		assert.NoError(t, srv.startTimeSeries())
	})

	t.Run("stopTimeSeries returns nil", func(t *testing.T) {
		assert.NoError(t, srv.stopTimeSeries())
	})

	t.Run("initVectorStorage returns nil", func(t *testing.T) {
		assert.NoError(t, srv.initVectorStorage())
	})

	t.Run("startVector returns nil", func(t *testing.T) {
		assert.NoError(t, srv.startVector())
	})

	t.Run("startEmbedding returns nil", func(t *testing.T) {
		assert.NoError(t, srv.startEmbedding())
	})

	t.Run("stopEmbedding returns nil", func(t *testing.T) {
		assert.NoError(t, srv.stopEmbedding())
	})
}

// ---------------------------------------------------------------------------
// Server.Shutdown with components tests
// ---------------------------------------------------------------------------

func TestServer_ShutdownWithShardedClient(t *testing.T) {
	t.Run("shutdown with sharded client", func(t *testing.T) {
		srv := withTempServer(t)
		srv.qrdbClient = NewShardedClient(nil)

		ctx := context.Background()
		err := srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})
}

func TestServer_ShutdownIdempotent(t *testing.T) {
	t.Run("shutdown when already closed is idempotent", func(t *testing.T) {
		srv := withTempServer(t)

		ctx := context.Background()
		err := srv.Shutdown(ctx)
		assert.NoError(t, err)

		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Server.GetKVStore panic tests
// ---------------------------------------------------------------------------

func TestServer_GetKVStore_PanicsWhenClientNil(t *testing.T) {
	t.Run("panics when qrdb client not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Panics(t, func() {
			srv.GetKVStore(1)
		})
	})
}

func TestServer_GetKVStore_PanicsOnShardError(t *testing.T) {
	t.Run("panics when ForShard fails", func(t *testing.T) {
		srv := withTempServer(t)
		sc := NewShardedClient(nil)
		require.NoError(t, sc.Close())
		srv.qrdbClient = sc

		assert.Panics(t, func() {
			srv.GetKVStore(1)
		})
	})
}

// ---------------------------------------------------------------------------
// Server state concurrency tests
// ---------------------------------------------------------------------------

func TestServer_ConcurrentAccessors(t *testing.T) {
	t.Run("concurrent reads are safe", func(t *testing.T) {
		srv := withTempServer(t)
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srv.IsStarted()
				srv.IsClosed()
				srv.Uptime()
				srv.GetConfig()
				srv.GetEffectiveMode()
				srv.GetRESTServer()
				srv.GetGRPCServer()
				srv.GetCatalogManager()
				srv.GetBackendManager()
				srv.GetWorkerManager()
				srv.GetDevClusterManager()
				srv.GetRAGSystem()
				srv.GetEmbeddingManager()
				srv.GetWatchBridge()
				srv.GetBackupManager()
				srv.GetQRDBClient()
				srv.Service()
				srv.GetHealthChecker()
			}()
		}
		wg.Wait()
	})
}

// ---------------------------------------------------------------------------
// Transport type constants tests
// ---------------------------------------------------------------------------

func TestTransportTypeConstants(t *testing.T) {
	assert.Equal(t, TransportType("embedded"), TransportEmbedded)
	assert.Equal(t, TransportType("grpc"), TransportGRPC)
	assert.Equal(t, TransportType("rest"), TransportREST)
	assert.Equal(t, TransportType("quic"), TransportQUIC)
	assert.Equal(t, TransportType("mcp"), TransportMCP)
	assert.Equal(t, TransportType("unix"), TransportUnix)
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// cloneTLSConfig tests
// ---------------------------------------------------------------------------

func TestCloneTLSConfig(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		result := cloneTLSConfig(nil)
		assert.Nil(t, result)
	})

	t.Run("clones config with http/1.1 proto", func(t *testing.T) {
		original := &tls.Config{
			ServerName: "test.example.com",
			NextProtos: []string{"h2", "http/1.1"},
		}

		cloned := cloneTLSConfig(original)
		require.NotNil(t, cloned)
		assert.Equal(t, "test.example.com", cloned.ServerName)
		assert.Equal(t, []string{"http/1.1"}, cloned.NextProtos)
		// Original should be unmodified
		assert.Equal(t, []string{"h2", "http/1.1"}, original.NextProtos)
	})
}

// ---------------------------------------------------------------------------
// fileExists tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// setupSignalHandlers test
// ---------------------------------------------------------------------------

func TestSetupSignalHandlers(t *testing.T) {
	t.Run("configures handlers without error", func(t *testing.T) {
		log := mustCreateLogger()
		setupSignalHandlers(func() {}, log)
	})
}

// ---------------------------------------------------------------------------
// NewServer edge cases
// ---------------------------------------------------------------------------

func TestNewServer_EdgeCases(t *testing.T) {
	t.Run("creates health checker", func(t *testing.T) {
		srv := withTempServer(t)
		require.NotNil(t, srv.GetHealthChecker())
		assert.Equal(t, srv, srv.GetHealthChecker().server)
	})

	t.Run("shutdown channel is open initially", func(t *testing.T) {
		srv := withTempServer(t)
		select {
		case <-srv.shutdownChan:
			t.Fatal("shutdown channel should not be closed initially")
		default:
			// expected
		}
	})
}

// ---------------------------------------------------------------------------
// createMinTestCfg test
// ---------------------------------------------------------------------------

func TestCreateMinTestCfg(t *testing.T) {
	t.Run("creates config with expected defaults", func(t *testing.T) {
		cfg := createMinTestCfg("/tmp/test")
		assert.Equal(t, "127.0.0.1", cfg.Server.Host)
		assert.True(t, cfg.Server.Embedded)
		assert.Equal(t, uint64(1), cfg.Raft.NodeID)
		assert.False(t, cfg.Security.TLSEnabled)
		assert.False(t, cfg.Discovery.Enabled)
		assert.Equal(t, "/tmp/test/data", cfg.Storage.DataDir)
		assert.Equal(t, "/tmp/test/wal", cfg.Storage.WALDir)
	})
}

// ---------------------------------------------------------------------------
// Lifecycle stubs for KMS/Secrets when disabled
// ---------------------------------------------------------------------------

func TestServer_StartKMS_Disabled(t *testing.T) {
	t.Run("returns nil when KMS disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.KMS.Enabled = false
		assert.NoError(t, srv.startKMS())
	})
}

func TestServer_StartSecrets_Disabled(t *testing.T) {
	t.Run("returns nil when Secrets disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Secrets.Enabled = false
		assert.NoError(t, srv.startSecrets())
	})
}

// ---------------------------------------------------------------------------
// Server.GetBarrierManager and GetCertificateAuthority tests
// ---------------------------------------------------------------------------

func TestServer_GetSealManager(t *testing.T) {
	t.Run("returns nil before start", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetBarrierManager())
	})
}

func TestServer_GetCertificateAuthority(t *testing.T) {
	t.Run("returns nil before start", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetCertificateAuthority())
	})
}

// ---------------------------------------------------------------------------
// StartCatalogManager disabled test
// ---------------------------------------------------------------------------

func TestServer_StartCatalogManager_Disabled(t *testing.T) {
	t.Run("returns nil when object storage not configured", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.ObjectStorage.Enabled = false
		srv.config.ObjectStorage.Backends = nil
		assert.NoError(t, srv.startCatalogManager())
	})
}

// ---------------------------------------------------------------------------
// StartBackupManager disabled test
// ---------------------------------------------------------------------------

func TestServer_StartBackupManager_Disabled(t *testing.T) {
	t.Run("returns nil when backup not enabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Backup.Enabled = false
		assert.NoError(t, srv.startBackupManager())
	})
}
