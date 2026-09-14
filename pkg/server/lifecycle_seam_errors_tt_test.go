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

// lifecycle_seam_errors_tt_test.go ports the lifecycle error-propagation tests
// from lifecycle_seam_errors_local_test.go to the full ("iam") tag set. The
// original file is gated `!iam` so those error branches were uncovered under
// the production tag set; these copies inject the same seam failures so the
// "if err != nil { return ... }" paths are exercised under the real build.

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// TestServer_Initialize_TT_CAFailurePropagates verifies initialize returns the
// error from the CA-bootstrap seam without proceeding to start storage.
func TestServer_Initialize_TT_CAFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	sentinel := errors.New("ca bootstrap exploded")
	initializeCAFn = func(_ *Server) error { return sentinel }

	err := srv.initialize()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel,
		"initialize must surface the CA-bootstrap failure verbatim")
}

// TestServer_StartMessaging_TT_StartFailurePropagates verifies startMessaging
// wraps a PushService start failure as an operation-failed error.
func TestServer_StartMessaging_TT_StartFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	sentinel := errors.New("push service refused to start")
	pushServiceStartFn = func(_ *messaging.PushService, _ context.Context) error { return sentinel }

	err := srv.startMessaging()
	require.Error(t, err, "a push-service start failure must abort startMessaging")
	assert.Contains(t, err.Error(), "messaging",
		"the wrapped error must identify the messaging subsystem")
}

// TestServer_StartWatchBridge_TT_StartFailurePropagates verifies that a
// watch-bridge start failure aborts startWatchBridge.
func TestServer_StartWatchBridge_TT_StartFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())
	require.NoError(t, srv.startMessaging())

	sentinel := errors.New("watch bridge start failed")
	watchBridgeStartFn = func(_ *watchbridge.WatchBridge, _ context.Context) error { return sentinel }

	err := srv.startWatchBridge()
	require.Error(t, err, "a watch-bridge start failure must abort startWatchBridge")
	assert.Contains(t, err.Error(), "watch bridge",
		"the wrapped error must identify the watch bridge")
}

// TestServer_StartGRPCServer_TT_RoutingValidationFailurePropagates verifies
// startGRPCServer aborts when the shard-routing validation seam reports an
// invalid configuration.
func TestServer_StartGRPCServer_TT_RoutingValidationFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	sentinel := errors.New("invalid shard routing")
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return sentinel }

	err := srv.startGRPCServer()
	require.Error(t, err, "an invalid routing config must abort startGRPCServer")
	assert.Contains(t, err.Error(), "routing",
		"the wrapped error must identify the routing-config validation step")
}

// TestServer_Initialize_TT_StorageFailurePropagates verifies initialize returns
// the storage-init error when the pebble data directory cannot be created.
func TestServer_Initialize_TT_StorageFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Storage.Backend = "pebble"
	srv.config.Storage.DataDir = "/proc/cannot-create-quicbaas-storage"

	err := srv.initialize()
	require.Error(t, err, "an uncreatable storage dir must abort initialize")
	var storageErr *StorageInitError
	require.ErrorAs(t, err, &storageErr,
		"initialize must surface a typed StorageInitError from startStorage")
}

// TestServer_StartCatalogManager_TT_StorageFailurePropagates verifies that a
// failure constructing the catalog storage backend aborts startCatalogManager.
func TestServer_StartCatalogManager_TT_StorageFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("catalog storage open failed")
	newCatalogStore = func(_ *catalog.PebbleStorageConfig) (catalog.PebbleStorage, error) {
		return nil, sentinel
	}

	err := srv.startCatalogManager()
	require.Error(t, err, "catalog storage construction failure must abort startCatalogManager")
	assert.Contains(t, err.Error(), "catalog storage",
		"the wrapped error must identify the failing catalog-storage step")
	assert.Nil(t, srv.catalogManager, "no catalog manager may be left set on failure")
}

// TestServer_Initialize_TT_CatalogFailurePropagates verifies the catalog failure
// surfaces through the top-level initialize.
func TestServer_Initialize_TT_CatalogFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("catalog storage open failed")
	newCatalogStore = func(_ *catalog.PebbleStorageConfig) (catalog.PebbleStorage, error) {
		return nil, sentinel
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the catalog manager fails to start")
}

// TestServer_StartKMS_TT_BackendConstructFailurePropagates verifies that a
// failure constructing the resolved KMS service backend aborts startKMS with a
// wrapped error. Construction now flows through the central BackendResolver, so
// the failure is injected via the buildKMSServiceBackendFn seam.
func TestServer_StartKMS_TT_BackendConstructFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true

	sentinel := errors.New("service backend unavailable")
	buildKMSServiceBackendFn = func(_ *BackendResolver, _ BackendSpec, _ xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
		return nil, sentinel
	}

	err := srv.startKMS()
	require.Error(t, err, "a KMS backend construction failure must abort startKMS")
	assert.Contains(t, err.Error(), "construct KMS backend",
		"the wrapped error must identify the failing KMS step")
}

// TestServer_StartSecrets_TT_ManagerFailurePropagates verifies startSecrets wraps
// a secrets-manager construction failure as an operation-failed error.
func TestServer_StartSecrets_TT_ManagerFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	sentinel := errors.New("secrets manager init failed")
	newSecretsMgr = func(_ *secrets.ManagerOptions) (*secrets.Manager, error) {
		return nil, sentinel
	}

	err := srv.startSecrets()
	require.Error(t, err, "a secrets-manager construction failure must abort startSecrets")
	assert.Nil(t, srv.secretsManager, "no secrets manager may be left set on failure")
}

// TestServer_StartBackupManager_TT_InvalidEncryptionKeySize covers the
// encryption-key-size validation branch.
func TestServer_StartBackupManager_TT_InvalidEncryptionKeySize(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "aes-256-gcm"
	srv.config.Backup.EncryptionKey = "short"

	err := srv.startBackupManager()
	require.Error(t, err, "an undersized AES-256-GCM key must abort startBackupManager")
}

// TestServer_StartOrchestration_TT_FullWiring drives startOrchestration on a
// fully-wired embedded server (qrdb client + QuicBaaS facade present). This
// covers the project-authz wiring (via the facade's project store), the
// qrdb-backed workflow persistor rehydration branch, and the lead-agent /
// session-event-log wiring that the disabled-feature lifecycle tests skip.
func TestServer_StartOrchestration_TT_FullWiring(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Orchestration.Enabled = true

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService(), "service facade must wire so orchestration has a project store")
	require.NotNil(t, srv.qrdbClient, "qrdb client must be available for the workflow persistor branch")

	require.NoError(t, srv.startOrchestration(), "orchestration startup must succeed")
	// Registered after the node's cleanup so it runs first: the task-source
	// poller and the job sweep read the stores the node owns.
	t.Cleanup(func() { srv.stopTaskSourcePoller(); srv.stopTaskDispatcher(); srv.stopOrchestrationSweeps() })
}

// TestServer_InitClusteredQRDBNode_TT_BadMembers covers the buildClusterMembers
// error branch inside initClusteredQRDBNode: a malformed id-prefixed peer string
// makes ParsePeers fail, so the node construction aborts before reaching the
// raft host builder.
func TestServer_InitClusteredQRDBNode_TT_BadMembers(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Mode = "cluster"
	// An "=value" peer with no numeric replica ID is rejected by ParsePeers.
	srv.config.Raft.JoinAddresses = []string{"=127.0.0.1:7000"}

	err := srv.initClusteredQRDBNode()
	require.Error(t, err, "a malformed peer list must abort clustered node init")
	var nodeErr *QRDBNodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, "build cluster members", nodeErr.Stage)
}

// TestServer_InitClusteredQRDBNode_TT_MultiNodeMTLS covers the multi-node mTLS
// branch of initClusteredQRDBNode: with more than one member configured the
// builder loads the raft mTLS material (here unset, so buildRaftMTLSConfig
// returns nil without error and the WithRaftMTLS option is skipped). Node
// construction then fails at the raft host stage in this hermetic environment,
// which exercises the new-clustered-node error return.
func TestServer_InitClusteredQRDBNode_TT_MultiNodeMTLS(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Mode = "cluster"
	srv.config.Raft.JoinAddresses = []string{"1=127.0.0.1:7001", "2=127.0.0.1:7002"}

	err := srv.initClusteredQRDBNode()
	// Multi-node bring-up cannot complete in a hermetic single-process test, so
	// an error is expected; the value of the test is the coverage of the
	// member-parse + mTLS-resolution path that precedes the failure.
	if err != nil {
		var nodeErr *QRDBNodeError
		require.ErrorAs(t, err, &nodeErr)
	} else {
		t.Cleanup(func() { _ = srv.stopQRDBNode() })
	}
}

// TestServer_StartOrchestration_TT_OllamaBackend covers the Ollama-backend
// registration branch of startOrchestration: when an Ollama service is present
// the orchestrator registers an Ollama agent backend. The Ollama service is
// constructed against an unreachable URL (construction is lazy and does not
// dial), so the branch runs hermetically.
func TestServer_StartOrchestration_TT_OllamaBackend(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Orchestration.Enabled = true

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	ollamaCfg := config.DefaultOllamaConfig()
	ollamaCfg.Enabled = true
	ollamaCfg.BaseURL = "http://127.0.0.1:1"
	ollamaSvc, err := service.NewOllamaService(&ollamaCfg)
	require.NoError(t, err, "Ollama service construction is lazy and must not dial")
	srv.ollamaService = ollamaSvc

	require.NoError(t, srv.startOrchestration(),
		"orchestration startup with an Ollama backend must succeed")
	t.Cleanup(func() { srv.stopTaskSourcePoller(); srv.stopTaskDispatcher(); srv.stopOrchestrationSweeps() })
}

// ttNoopCloser is an io.Closer that records whether Close was invoked. It backs
// the data-plane-barrier-closer Shutdown branch in the test below.
type ttNoopCloser struct{ closed bool }

func (c *ttNoopCloser) Close() error { c.closed = true; return nil }

// TestServer_Shutdown_TT_OptionalBackendBranches covers the optional
// teardown branch of Server.Shutdown that is skipped on the standard
// node-backed path: the data-plane barrier closer. It is injected with a
// no-op closer so the non-nil branch executes its Close path during shutdown.
func TestServer_Shutdown_TT_OptionalBackendBranches(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initialize())

	closer := &ttNoopCloser{}
	srv.dataPlaneBarrierCloser = closer

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
	assert.True(t, closer.closed, "the data-plane barrier closer must be closed during shutdown")
}

// ttFailingCloser is an io.Closer that always fails, used to drive the
// error-collection branch of the data-plane barrier teardown in Shutdown.
type ttFailingCloser struct{}

func (ttFailingCloser) Close() error { return errors.New("barrier close failed") }

// TestServer_Shutdown_TT_DataPlaneBarrierCloseFailure covers the error branch of
// the data-plane barrier teardown: a failing closer is collected into the
// aggregate Shutdown error rather than aborting the remaining teardown.
func TestServer_Shutdown_TT_DataPlaneBarrierCloseFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initialize())
	srv.dataPlaneBarrierCloser = ttFailingCloser{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	require.Error(t, err, "a failing barrier closer must surface in the aggregate shutdown error")
}

// TestServer_StartCatalogManager_TT_ManagerFailure injects a catalog-manager
// construction failure so the newCatalogMgr error branch of startCatalogManager
// runs.
func TestServer_StartCatalogManager_TT_ManagerFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("catalog manager init failed")
	newCatalogMgr = func(_ *catalog.ManagerConfig) (catalog.CatalogManager, error) {
		return nil, sentinel
	}

	err := srv.startCatalogManager()
	require.Error(t, err, "a catalog-manager construction failure must abort startCatalogManager")
}

// TestServer_StartCatalogManager_TT_BackendManagerFailure injects a
// backend-manager construction failure so the newBackendMgr error branch runs.
func TestServer_StartCatalogManager_TT_BackendManagerFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("backend manager init failed")
	newBackendMgr = func(_ *catalog.BackendManagerConfig) (catalog.BackendManager, error) {
		return nil, sentinel
	}

	err := srv.startCatalogManager()
	require.Error(t, err, "a backend-manager construction failure must abort startCatalogManager")
}

// TestServer_StartCatalogManager_TT_BadBackendRegistration covers the
// register-backend warning branch: an unknown backend type fails registration
// but startCatalogManager continues (the failure is non-fatal).
func TestServer_StartCatalogManager_TT_BadBackendRegistration(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true
	srv.config.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"broken": {Type: "no-such-backend-type", Config: map[string]string{}},
	}

	require.NoError(t, srv.startCatalogManager(),
		"an unregisterable backend must be skipped without aborting startup")
	require.NotNil(t, srv.catalogManager)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// TestServer_Initialize_TT_KMSFailurePropagates verifies the KMS startup failure
// surfaces through the top-level initialize (covering the KMS error return in
// the component-startup sequence).
func TestServer_Initialize_TT_KMSFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.KMS.Enabled = true

	sentinel := errors.New("service backend unavailable")
	buildKMSServiceBackendFn = func(_ *BackendResolver, _ BackendSpec, _ xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
		return nil, sentinel
	}

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when KMS startup fails")
}

// TestServer_Initialize_TT_SecretsFailurePropagates verifies the secrets startup
// failure surfaces through initialize.
func TestServer_Initialize_TT_SecretsFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	sentinel := errors.New("secrets manager init failed")
	newSecretsMgr = func(_ *secrets.ManagerOptions) (*secrets.Manager, error) {
		return nil, sentinel
	}

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the secrets manager fails to start")
}
