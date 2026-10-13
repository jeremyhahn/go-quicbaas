//go:build mcp && !integration

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

// Shared seam-management helpers for the pkg/server lifecycle tests.
//
// These live here, and not in lifecycle_fulltags_test.go where they were
// written, because their callers do not agree on a tag set: 20 call them from
// files gated on the full expression (which also requires local, timeseries
// and rag), and two -- lifecycle_seams_test.go and
// register_iam_service_errors_test.go -- from a narrower one. A helper file
// must be no broader than its narrowest caller, or that caller cannot build.
//
// The tag is derived from the symbols below, not copied from a caller. Every
// seam saveAndRestoreSeams snapshots is declared in the untagged lifecycle.go,
// except newREST/restShutdownFn -- declared on BOTH sides of rest, in
// lifecycle_rest.go and lifecycle_rest_stub.go -- and newMCP, whose !mcp
// counterpart lifecycle_mcp_stub.go does not declare it. newMCP is therefore
// the whole tag requirement, and all 22 callers already require mcp.

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// saveAndRestoreSeams snapshots all seam injection variables and restores
// them via t.Cleanup, preventing inter-test pollution.
func saveAndRestoreSeams(t *testing.T) {
	t.Helper()

	seamMu.Lock()

	// Base seams from lifecycle.go
	origListenFunc := listenFunc
	origNewGRPCAPI := newGRPCAPI
	origGrpcStopTimeout := grpcStopTimeout
	origGrpcGracefulStop := grpcGracefulStop
	origGrpcForceStop := grpcForceStop
	origNewSecretsMgr := newSecretsMgr
	origNewCatalogStore := newCatalogStore
	origNewCatalogMgr := newCatalogMgr
	origNewBackendMgr := newBackendMgr
	origNewReplicationWorkerFn := newReplicationWorkerFn
	origNewArchiveWorkerFn := newArchiveWorkerFn
	origNewVerifyWorkerFn := newVerifyWorkerFn
	origNewRetentionWorkerFn := newRetentionWorkerFn
	origCloseBackupMgrFn := closeBackupMgrFn
	origNewKMSKeyStorage := newKMSKeyStorage
	origBuildKMSServiceBackendFn := buildKMSServiceBackendFn
	origInitXKMS := initXKMS
	origRegisterXKMSBackendFn := registerXKMSBackendFn
	origNewKMSBackendFactory := newKMSBackendFactory
	origNewKMSServiceFn := newKMSServiceFn
	origNewCacheManagerFn := newCacheManagerFn
	origInitSealManagerFn := initSealManagerFn
	origNewWatchBridgeFn := newWatchBridgeFn
	origInitIAMComponentsFn := initIAMComponentsFn
	origNewKMSDEKProviderFn := newKMSDEKProviderFn
	origNewEncryptionManagerFn := newEncryptionManagerFn
	origNewReencryptWorkerSeamFn := newReencryptWorkerSeamFn
	origValidateRoutingFn := validateRoutingFn
	origInitFromXKMSFn := initFromXKMSFn
	origWorkerMgrRegisterFn := workerMgrRegisterFn
	origWorkerMgrStartAllFn := workerMgrStartAllFn
	origPushServiceStartFn := pushServiceStartFn
	origWatchBridgeStartFn := watchBridgeStartFn
	origNewCAFn := newCAFn
	origNewBarrierManagerFn := newBarrierManagerFn
	origInitializeCAFn := initializeCAFn

	// REST seams from lifecycle_rest.go
	origNewREST := newREST
	origRESTShutdownFn := restShutdownFn

	// MCP seams from lifecycle_mcp.go
	origNewMCP := newMCP

	seamMu.Unlock()

	t.Cleanup(func() {
		seamMu.Lock()
		defer seamMu.Unlock()

		// Restore base seams
		listenFunc = origListenFunc
		newGRPCAPI = origNewGRPCAPI
		grpcStopTimeout = origGrpcStopTimeout
		grpcGracefulStop = origGrpcGracefulStop
		grpcForceStop = origGrpcForceStop
		newSecretsMgr = origNewSecretsMgr
		newCatalogStore = origNewCatalogStore
		newCatalogMgr = origNewCatalogMgr
		newBackendMgr = origNewBackendMgr
		newReplicationWorkerFn = origNewReplicationWorkerFn
		newArchiveWorkerFn = origNewArchiveWorkerFn
		newVerifyWorkerFn = origNewVerifyWorkerFn
		newRetentionWorkerFn = origNewRetentionWorkerFn
		closeBackupMgrFn = origCloseBackupMgrFn
		newKMSKeyStorage = origNewKMSKeyStorage
		buildKMSServiceBackendFn = origBuildKMSServiceBackendFn
		initXKMS = origInitXKMS
		registerXKMSBackendFn = origRegisterXKMSBackendFn
		newKMSBackendFactory = origNewKMSBackendFactory
		newKMSServiceFn = origNewKMSServiceFn
		newCacheManagerFn = origNewCacheManagerFn
		initSealManagerFn = origInitSealManagerFn
		newWatchBridgeFn = origNewWatchBridgeFn
		initIAMComponentsFn = origInitIAMComponentsFn
		newKMSDEKProviderFn = origNewKMSDEKProviderFn
		newEncryptionManagerFn = origNewEncryptionManagerFn
		newReencryptWorkerSeamFn = origNewReencryptWorkerSeamFn
		validateRoutingFn = origValidateRoutingFn
		initFromXKMSFn = origInitFromXKMSFn
		workerMgrRegisterFn = origWorkerMgrRegisterFn
		workerMgrStartAllFn = origWorkerMgrStartAllFn
		pushServiceStartFn = origPushServiceStartFn
		watchBridgeStartFn = origWatchBridgeStartFn
		newCAFn = origNewCAFn
		newBarrierManagerFn = origNewBarrierManagerFn
		initializeCAFn = origInitializeCAFn

		// Restore REST seams
		newREST = origNewREST
		restShutdownFn = origRESTShutdownFn

		// Restore MCP seams
		newMCP = origNewMCP
	})
}

// createFTServerWithService creates a test server with a mock QuicBaaS
// service assigned, suitable for tests that need a fully wired server
// without running the full initialization lifecycle.
func createFTServerWithService(t *testing.T) *Server {
	t.Helper()
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.quicBaaSService = testutil.NewMockQuicBaaS()
	return srv
}
