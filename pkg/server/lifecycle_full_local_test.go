//go:build local && !raft && !rest && !quic && !mcp && !rag && !iam && !integration

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

// lifecycle_full_local_test.go drives a full Server.initialize() + Shutdown()
// cycle under the default "local" transport build, where REST/QUIC/MCP/RAG are
// compiled as no-op stubs and gRPC is the only real listener. This exercises
// the end-to-end composition path (storage -> service facade -> databases ->
// gRPC -> shutdown) that the per-function unit tests only touch in isolation.
//
// The test binds gRPC to a loopback port; no external network or host state is
// touched. Shutdown is driven via context with a generous deadline; goroutine
// teardown is coordinated by Shutdown's own error collection, never sleeps.

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newTestGRPCServer returns a bare gRPC server for listener-wiring tests.
func newTestGRPCServer(t *testing.T) *ggrpc.Server {
	t.Helper()
	return ggrpc.NewServer()
}

// shortUnixSocketPath returns a per-test unix socket path under /tmp that stays
// within the platform's sockaddr_un length limit (t.TempDir paths can exceed
// it). The directory is removed on test completion.
func shortUnixSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "qb")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "g.sock")
}

// TestServer_InitializeAndShutdown_Local_EmbeddedMode drives the embedded-mode
// initialize path. gRPC and the other transports short-circuit on the embedded
// guard, but storage, the service facade, the database subsystem, and the full
// reverse-order Shutdown all run. This is the broadest hermetic lifecycle
// exercise available without binding a network listener.
func TestServer_InitializeAndShutdown_Local_EmbeddedMode(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initialize(), "embedded initialize must succeed")
	require.NotNil(t, srv.Service(), "service facade must be wired after initialize")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")

	// Shutdown must be idempotent: a second call returns without error.
	assert.NoError(t, srv.Shutdown(ctx), "second Shutdown must be a no-op")
}

// TestServer_InitializeAndShutdown_Local_NetworkGRPC drives the non-embedded
// initialize path so the real startGRPCServer body runs: it constructs the
// gRPC server, registers the QuicBaaS/qrdb/barrier/ceremony services, binds a
// loopback listener, and serves in the background. Shutdown then stops the
// listener and tears down every subsystem in reverse order.
func TestServer_InitializeAndShutdown_Local_NetworkGRPC(t *testing.T) {
	srv := withTempServer(t)
	// Non-embedded so the gRPC listener body executes. The free port was
	// allocated by createTestConfig; reuse it for the bind.
	srv.config.Server.Embedded = false

	require.NoError(t, srv.initialize(), "network initialize must succeed")
	require.NotNil(t, srv.grpcServer, "gRPC server must be constructed in network mode")
	require.NotNil(t, srv.grpcListener, "gRPC listener must be bound in network mode")

	// Health must reflect the live gRPC listener.
	grpcHealth := srv.GetHealthChecker().CheckGRPC()
	assert.Equal(t, HealthStatusHealthy, grpcHealth.Status,
		"gRPC must report healthy once the listener is bound")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")
}

// TestServer_InitializeAndShutdown_Local_WithObjectStorageAndKMS drives an
// initialize where object storage and KMS are enabled, so the catalog manager,
// backend manager, background workers, and KMS subsystem all come up. The
// subsequent Shutdown then exercises the reverse-order teardown of every one of
// those managers (stopWorkerManager / stopBackendManager / stopCatalogManager /
// KMS), which the disabled-feature lifecycle test leaves on their nil-guard
// branches.
func TestServer_InitializeAndShutdown_Local_WithObjectStorageAndKMS(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	// Object storage enabled with no explicit backends auto-creates an
	// in-memory default backend, bringing up the catalog/backend/worker stack.
	srv.config.ObjectStorage.Enabled = true

	// KMS enabled uses the software backend with file-backed key storage under
	// the test data dir (TLS is disabled in createTestConfig).
	srv.config.KMS.Enabled = true

	// Backup enabled brings up the backup manager backed by a local filesystem
	// store under the test data dir, so its start and stop paths run too.
	srv.config.Backup.Enabled = true

	require.NoError(t, srv.initialize(), "feature-rich initialize must succeed")
	require.NotNil(t, srv.catalogManager, "catalog manager must be started")
	require.NotNil(t, srv.backendManager, "backend manager must be started")
	require.NotNil(t, srv.workerManager, "worker manager must be started")
	require.NotNil(t, srv.backupManager, "backup manager must be started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown of the full stack must succeed")

	// The managers must be torn down (set to nil) by the stop* helpers.
	assert.Nil(t, srv.catalogManager, "catalog manager must be released after Shutdown")
	assert.Nil(t, srv.backendManager, "backend manager must be released after Shutdown")
	assert.Nil(t, srv.workerManager, "worker manager must be released after Shutdown")
}

// TestServer_InitializeAndShutdown_Local_WithMessagingAndOrchestration drives an
// initialize where the messaging subsystem, its watch bridge, and the agent
// orchestration service are all enabled. These subsystems are wired into the
// QuicBaaS facade during initialize and torn down during Shutdown, exercising
// startMessaging, startWatchBridge, startOrchestration, and their corresponding
// shutdown branches that the feature-disabled lifecycle tests skip.
func TestServer_InitializeAndShutdown_Local_WithMessagingAndOrchestration(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	srv.config.Messaging.Enabled = true
	srv.config.Messaging.WatchBridge.Enabled = true
	srv.config.Orchestration.Enabled = true

	// Vector and TimeSeries are compiled as no-op stubs under the default
	// build, but enabling them still drives the corresponding Enabled-gated
	// branches in initialize (and the matching shutdown checks) which are
	// otherwise skipped.
	srv.config.Vector.Enabled = true
	srv.config.TimeSeries.Enabled = true

	require.NoError(t, srv.initialize(), "messaging+orchestration initialize must succeed")
	require.NotNil(t, srv.messagingService, "messaging service must be started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")
}

// TestServer_InitializeAndShutdown_Local_FullStackNetworkGRPC drives the
// broadest hermetic lifecycle: a non-embedded server with object storage, KMS,
// backup, messaging + watch bridge, and orchestration all enabled. Because the
// server is non-embedded, the real gRPC listener body runs and registers every
// service, and the subsequent Shutdown walks the full reverse-order teardown
// with each subsystem present (catalog/backend/worker/backup/messaging/
// watch-bridge/raft/qrdb-client/gRPC), exercising the non-nil branch of each
// Shutdown stage in a single run.
func TestServer_InitializeAndShutdown_Local_FullStackNetworkGRPC(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	// A Unix socket alongside the TCP listener exercises the Unix-socket
	// branch of startGRPCServer in addition to the TCP path. A short /tmp path
	// is used because unix socket paths are capped near 104 bytes.
	srv.config.Server.Unix.Socket = shortUnixSocketPath(t)

	srv.config.ObjectStorage.Enabled = true
	srv.config.KMS.Enabled = true
	srv.config.Backup.Enabled = true
	srv.config.Messaging.Enabled = true
	srv.config.Messaging.WatchBridge.Enabled = true
	srv.config.Orchestration.Enabled = true

	// The embedded agent is started after orchestration; enabling it covers
	// both the startEmbeddedAgent call inside initialize and the embedded-agent
	// teardown branch inside Shutdown.
	srv.config.Server.EmbeddedAgent.Enabled = true
	srv.config.Server.EmbeddedAgent.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedAgent.DataDir = t.TempDir()

	require.NoError(t, srv.initialize(), "full-stack network initialize must succeed")
	require.NotNil(t, srv.embeddedAgentState, "embedded agent must be started")
	require.NotNil(t, srv.grpcServer, "gRPC server must be constructed in network mode")
	require.NotNil(t, srv.grpcListener, "gRPC listener must be bound")
	require.NotNil(t, srv.catalogManager)
	require.NotNil(t, srv.backupManager)
	require.NotNil(t, srv.messagingService)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "full-stack shutdown must succeed")
}

// TestServer_StartUnixSocketListener_Local_HappyPath verifies the Unix-socket
// gRPC listener is created, permissioned, and recorded on the server. The
// socket lives under t.TempDir so nothing outside the test sandbox is touched.
func TestServer_StartUnixSocketListener_Local_HappyPath(t *testing.T) {
	srv := withTempServer(t)
	socketPath := filepath.Join(t.TempDir(), "quicbaas.sock")
	srv.config.Server.Unix.Socket = socketPath

	grpcServer := newTestGRPCServer(t)
	require.NoError(t, srv.startUnixSocketListener(grpcServer),
		"unix socket listener must start cleanly")
	require.NotNil(t, srv.grpcUnixListener, "the unix listener must be recorded on the server")

	// Recreating over an existing socket file must also succeed: the method
	// removes the stale socket before binding.
	grpcServer2 := newTestGRPCServer(t)
	require.NoError(t, srv.startUnixSocketListener(grpcServer2),
		"binding over an existing socket file must succeed after removal")

	grpcServer.Stop()
	grpcServer2.Stop()
}

// TestServer_StartUnixSocketListener_Local_MkdirFailure verifies the typed
// error path when the socket directory cannot be created (an unwritable parent).
func TestServer_StartUnixSocketListener_Local_MkdirFailure(t *testing.T) {
	srv := withTempServer(t)
	// /proc/<x>/... cannot be created, so MkdirAll on the socket dir fails.
	srv.config.Server.Unix.Socket = "/proc/quicbaas-no-such/sub/quicbaas.sock"

	grpcServer := newTestGRPCServer(t)
	t.Cleanup(grpcServer.Stop)

	err := srv.startUnixSocketListener(grpcServer)
	require.Error(t, err, "an uncreatable socket directory must produce an error")
}

// TestServer_InitializeAndShutdown_Local_ClusterMode drives initialize with the
// server configured for cluster mode. This covers the cluster branch of the
// raft-node initialization switch (a standalone raft node is still used as the
// local shard transport until full cluster Raft is wired). Embedded mode keeps
// the run hermetic.
func TestServer_InitializeAndShutdown_Local_ClusterMode(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeCluster

	require.NoError(t, srv.initialize(), "cluster-mode initialize must succeed")
	require.NotNil(t, srv.raftNode, "a local raft node must be initialized in cluster mode")
	assert.Equal(t, config.ServerModeCluster, srv.GetEffectiveMode())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_StartGRPCServer_Local_NoListenersConfigured covers the guard that
// rejects a gRPC start when neither a Unix socket nor the TCP listener is
// configured. The service facade is wired first so the method reaches the
// listener-selection stage.
func TestServer_StartGRPCServer_Local_NoListenersConfigured(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.Unix.Socket = ""
	srv.config.Server.GRPC.Enabled = false

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	err := srv.startGRPCServer()
	require.Error(t, err, "startGRPCServer must fail when no listeners are configured")
}
