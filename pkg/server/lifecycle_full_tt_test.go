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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

// lifecycle_full_tt_test.go ports the broad initialize()+Shutdown() lifecycle
// tests from lifecycle_full_local_test.go to the full ("iam") tag set. The
// original file is gated `!iam`/`!rest`/`!quic`/`!mcp`, so under the production
// tag set the feature-rich startup + reverse-order Shutdown teardown branches
// (catalog/backend/worker/backup/messaging/watch-bridge/orchestration/
// embedded-agent/network-gRPC/unix-socket) were never exercised. These copies
// run the same composition path under the real build.

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newTTGRPCServer returns a bare gRPC server for listener-wiring tests.
func newTTGRPCServer(t *testing.T) *ggrpc.Server {
	t.Helper()
	return ggrpc.NewServer()
}

// shortTTUnixSocketPath returns a per-test unix socket path under /tmp that
// stays within the platform's sockaddr_un length limit.
func shortTTUnixSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "qb")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "g.sock")
}

// TestServer_InitializeAndShutdown_TT_NetworkGRPC drives a non-embedded
// initialize so the real gRPC listener body executes and binds, then a graceful
// Shutdown.
func TestServer_InitializeAndShutdown_TT_NetworkGRPC(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false

	require.NoError(t, srv.initialize(), "network initialize must succeed")
	require.NotNil(t, srv.grpcServer, "gRPC server must be constructed in network mode")
	require.NotNil(t, srv.grpcListener, "gRPC listener must be bound in network mode")

	grpcHealth := srv.GetHealthChecker().CheckGRPC()
	assert.Equal(t, HealthStatusHealthy, grpcHealth.Status,
		"gRPC must report healthy once the listener is bound")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")
}

// TestServer_InitializeAndShutdown_TT_WithObjectStorageAndKMS brings up the
// catalog/backend/worker stack plus KMS and backup, then exercises the
// reverse-order teardown of every one of those managers in Shutdown.
func TestServer_InitializeAndShutdown_TT_WithObjectStorageAndKMS(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	srv.config.ObjectStorage.Enabled = true
	srv.config.KMS.Enabled = true
	srv.config.Backup.Enabled = true

	require.NoError(t, srv.initialize(), "feature-rich initialize must succeed")
	require.NotNil(t, srv.catalogManager, "catalog manager must be started")
	require.NotNil(t, srv.backendManager, "backend manager must be started")
	require.NotNil(t, srv.workerManager, "worker manager must be started")
	require.NotNil(t, srv.backupManager, "backup manager must be started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown of the full stack must succeed")

	assert.Nil(t, srv.catalogManager, "catalog manager must be released after Shutdown")
	assert.Nil(t, srv.backendManager, "backend manager must be released after Shutdown")
	assert.Nil(t, srv.workerManager, "worker manager must be released after Shutdown")
}

// TestServer_InitializeAndShutdown_TT_WithMessagingAndOrchestration brings up the
// messaging subsystem, its watch bridge, and the agent orchestration service,
// then tears them all down via Shutdown.
func TestServer_InitializeAndShutdown_TT_WithMessagingAndOrchestration(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	srv.config.Messaging.Enabled = true
	srv.config.Messaging.WatchBridge.Enabled = true
	srv.config.Orchestration.Enabled = true
	srv.config.Vector.Enabled = true
	srv.config.TimeSeries.Enabled = true

	require.NoError(t, srv.initialize(), "messaging+orchestration initialize must succeed")
	require.NotNil(t, srv.messagingService, "messaging service must be started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")
}

// TestServer_InitializeAndShutdown_TT_FullStackNetworkGRPC drives the broadest
// hermetic lifecycle: a non-embedded server with object storage, KMS, backup,
// messaging + watch bridge, orchestration, and the embedded agent all enabled.
// The real gRPC listener body runs (TCP + unix socket) and the subsequent
// Shutdown walks the full reverse-order teardown with each subsystem present.
func TestServer_InitializeAndShutdown_TT_FullStackNetworkGRPC(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.Unix.Socket = shortTTUnixSocketPath(t)

	srv.config.ObjectStorage.Enabled = true
	srv.config.KMS.Enabled = true
	srv.config.Backup.Enabled = true
	srv.config.Messaging.Enabled = true
	srv.config.Messaging.WatchBridge.Enabled = true
	srv.config.Orchestration.Enabled = true

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

// TestServer_StartUnixSocketListener_TT_HappyPath verifies the Unix-socket gRPC
// listener is created, permissioned, and recorded on the server, including the
// stale-socket-removal-and-rebind path.
func TestServer_StartUnixSocketListener_TT_HappyPath(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	socketPath := filepath.Join(t.TempDir(), "quicbaas.sock")
	srv.config.Server.Unix.Socket = socketPath

	grpcServer := newTTGRPCServer(t)
	require.NoError(t, srv.startUnixSocketListener(grpcServer),
		"unix socket listener must start cleanly")
	require.NotNil(t, srv.grpcUnixListener, "the unix listener must be recorded on the server")

	grpcServer2 := newTTGRPCServer(t)
	require.NoError(t, srv.startUnixSocketListener(grpcServer2),
		"binding over an existing socket file must succeed after removal")

	grpcServer.Stop()
	grpcServer2.Stop()
}

// TestServer_StartUnixSocketListener_TT_MkdirFailure verifies the typed error
// path when the socket directory cannot be created.
func TestServer_StartUnixSocketListener_TT_MkdirFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Unix.Socket = "/proc/quicbaas-no-such/sub/quicbaas.sock"

	grpcServer := newTTGRPCServer(t)
	t.Cleanup(grpcServer.Stop)

	err := srv.startUnixSocketListener(grpcServer)
	require.Error(t, err, "an uncreatable socket directory must produce an error")
}

// TestServer_StartGRPCServer_TT_NoListenersConfigured covers the guard that
// rejects a gRPC start when neither a Unix socket nor the TCP listener is
// configured.
func TestServer_StartGRPCServer_TT_NoListenersConfigured(t *testing.T) {
	saveAndRestoreSeams(t)
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

// TestServer_InitializeAndShutdown_TT_AllTransports enables the REST and
// MCP transports alongside gRPC in non-embedded mode so each transport's start
// branch (and the matching Shutdown teardown branch) executes. Free loopback
// ports are allocated per-transport so nothing outside the sandbox is touched.
func TestServer_InitializeAndShutdown_TT_AllTransports(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false

	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = getFreePort(t)

	require.NoError(t, srv.initialize(), "all-transports initialize must succeed")
	require.NotNil(t, srv.restServer, "REST server must be started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown of all transports must succeed")
}

// TestServer_InitializeAndShutdown_TT_TLSEnabled drives initialize with TLS
// enabled so the internal certificate-authority bootstrap (initializeCA) runs:
// it builds the file-backed key/cert stores, the PKCS#8 + xkms backends, the
// cert store, and the TrustedCA hierarchy, then issues the server's own TLS
// certificate. TLS is disabled in the default test config, leaving this whole
// path uncovered; enabling it (in a temp data dir) keeps the run hermetic.
func TestServer_InitializeAndShutdown_TT_TLSEnabled(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Security.TLSEnabled = true

	require.NoError(t, srv.initialize(), "TLS-enabled initialize must bootstrap the CA")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_InitializeAndShutdown_TT_TLSNetworkAllTransports drives a
// non-embedded, TLS-enabled server with every transport enabled. This covers
// the TLS branches of the REST/MCP/gRPC start paths (TLS config cloning,
// listener TLS wrapping) plus the CA bootstrap, all in one hermetic run.
func TestServer_InitializeAndShutdown_TT_TLSNetworkAllTransports(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Security.TLSEnabled = true

	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = getFreePort(t)

	require.NoError(t, srv.initialize(), "TLS network initialize must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_InitializeCA_TT_P384Curve covers the P-384 curve branch of the
// ECDSA key configuration in initializeCA.
func TestServer_InitializeCA_TT_P384Curve(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Security.TLSEnabled = true
	srv.config.CA.RootKeyCurve = "P-384"

	require.NoError(t, srv.initialize(), "P-384 CA bootstrap must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_Shutdown_TT_StopFailuresCollected drives Shutdown with every
// transport's stop seam (and the backup-manager close seam) injected to fail.
// Shutdown must collect each error, continue tearing down the remaining
// subsystems, and return an aggregate ShutdownError. This covers the
// error-append branches of the REST/MCP/backup teardown stages that the
// happy-path Shutdown leaves uncovered.
func TestServer_Shutdown_TT_StopFailuresCollected(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Backup.Enabled = true
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)

	require.NoError(t, srv.initialize(), "full-transport initialize must succeed")
	require.NotNil(t, srv.restServer)
	require.NotNil(t, srv.backupManager)

	// Inject stop failures into the transport and backup teardown seams.
	sentinel := errors.New("stop refused")
	restShutdownFn = func(_ *restapi.Server, _ context.Context) error { return sentinel }
	closeBackupMgrFn = func(_ *backup.BackupManager) error { return sentinel }

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	require.Error(t, err, "Shutdown must surface an aggregate error when teardown seams fail")
}

// TestServer_Initialize_TT_RESTConstructorFailure injects a REST-server
// construction failure so the startRESTServer error return — and the
// corresponding error propagation inside initialize — both run.
func TestServer_Initialize_TT_RESTConstructorFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)

	sentinel := errors.New("rest construction refused")
	newREST = func(_ service.QuicBaaS, _ *restapi.Config, _ *logger.Logger) (*restapi.Server, error) {
		return nil, sentinel
	}

	err := srv.initialize()
	require.Error(t, err, "a REST construction failure must abort initialize")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// TestServer_InitializeCA_TT_KeyStorageFailure covers the key-storage
// construction error branch of initializeCA: an uncreatable keys directory
// (under /proc) makes NewFileStorageBackend fail, aborting CA bootstrap with a
// wrapped operation-failed error.
func TestServer_InitializeCA_TT_KeyStorageFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Security.TLSEnabled = true
	// A keys directory under /proc cannot be created.
	srv.config.CA.KeysDir = "/proc/quicbaas-no-such/keys"

	err := srv.initialize()
	require.Error(t, err, "an uncreatable CA keys directory must abort initialize")
}

// TestServer_StartEmbeddedAgent_TT_NoRegistry covers the no-registry guard:
// starting the embedded agent without an agent registry returns the typed
// ErrEmbeddedAgentNoRegistry.
func TestServer_StartEmbeddedAgent_TT_NoRegistry(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.agentRegistry = nil

	err := srv.startEmbeddedAgent()
	require.Error(t, err, "starting the embedded agent without a registry must fail")
}

// TestServer_StartEmbeddedAgent_TT_AlreadyStarted covers the already-started
// guard: a second start with a populated embeddedAgentState returns the typed
// already-started error.
func TestServer_StartEmbeddedAgent_TT_AlreadyStarted(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.EmbeddedAgent.Enabled = true
	srv.config.Server.EmbeddedAgent.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedAgent.DataDir = t.TempDir()

	require.NoError(t, srv.initialize())
	require.NotNil(t, srv.embeddedAgentState, "the first start must establish the agent state")

	err := srv.startEmbeddedAgent()
	require.Error(t, err, "a second embedded-agent start must be rejected")
	var alreadyErr *EmbeddedAgentAlreadyStartedError
	require.ErrorAs(t, err, &alreadyErr)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// TestServer_StartEmbeddedAgent_TT_WithTLSIdentity covers the CA-backed mTLS
// identity issuance branch of startEmbeddedAgent: with TLS enabled the local CA
// issues the agent's mTLS certificate during startup.
func TestServer_StartEmbeddedAgent_TT_WithTLSIdentity(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Security.TLSEnabled = true
	srv.config.Server.EmbeddedAgent.Enabled = true
	srv.config.Server.EmbeddedAgent.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedAgent.DataDir = t.TempDir()

	require.NoError(t, srv.initialize(), "TLS-enabled embedded-agent start must succeed")
	require.NotNil(t, srv.embeddedAgentState)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_StartUnixSocketListener_TT_RemoveExistingFails covers the
// remove-existing-socket error branch: when the socket path is a NON-empty
// directory, os.Remove fails with a non-ENOENT error (ENOTEMPTY) and
// startUnixSocketListener aborts with a typed operation-failed error.
func TestServer_StartUnixSocketListener_TT_RemoveExistingFails(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	// socketPath is a directory containing a file, so os.Remove(socketPath)
	// fails with ENOTEMPTY (neither nil nor IsNotExist).
	base := t.TempDir()
	socketPath := filepath.Join(base, "quicbaas.sock")
	require.NoError(t, os.Mkdir(socketPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(socketPath, "occupant"), []byte("x"), 0o600))
	srv.config.Server.Unix.Socket = socketPath

	grpcServer := newTTGRPCServer(t)
	t.Cleanup(grpcServer.Stop)

	err := srv.startUnixSocketListener(grpcServer)
	require.Error(t, err, "removing a non-empty directory at the socket path must fail")
}
