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

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

// =============================================================================
// go-quicbaas#653, the central's own dispatch client.
//
// The central mints tokens from this application to reach a remote daemon's
// gRPC spawn server, which gates every RPC on agent:runtime:spawn/prompt/status.
// A token carries the grant on its application record, so an application with
// no grant makes the whole fleet undispatchable in the outbound direction --
// the mirror image of the heartbeat refusal.
// =============================================================================

// newDaemonClientAppManager builds an AppManager over the tenant-routed store
// the system tenant's applications actually live in.
func newDaemonClientAppManager(t *testing.T) (*iam.AppManager, iam.AppStorage) {
	t.Helper()
	kv := iamtest.NewTenantRoutingKV()
	kv.BarrierTenant(qrdbsdk.SystemTenantID)
	storage := iam.NewBackendStorage(kv)
	return iam.NewAppManager(storage, nil), storage
}

// TestEnsureOrchestratorDaemonClient_GrantsTheDispatchScopes pins the bootstrap
// path: the application the central creates for itself must hold the scopes a
// daemon's spawn server checks.
func TestEnsureOrchestratorDaemonClient_GrantsTheDispatchScopes(t *testing.T) {
	srv := withTempServer(t)
	mgr, storage := newDaemonClientAppManager(t)

	client, err := srv.ensureOrchestratorDaemonClient(context.Background(), mgr)
	require.NoError(t, err)
	require.ElementsMatch(t, iam.DefaultAgentRuntimeScopes(), client.Scopes)

	app, err := storage.GetApp(context.Background(), client.ClientID)
	require.NoError(t, err)
	require.ElementsMatch(t, iam.DefaultAgentRuntimeScopes(), app.Scopes,
		"the central's dispatch client holds no grant, so its tokens authorize nothing")
}

// TestEnsureOrchestratorDaemonClient_ConvergesAnUngrantedCachedApp covers the
// upgrade path. The credentials cache short-circuits the bootstrap, so an
// application created before grants existed would otherwise keep its cached
// secret and never acquire one.
func TestEnsureOrchestratorDaemonClient_ConvergesAnUngrantedCachedApp(t *testing.T) {
	srv := withTempServer(t)
	mgr, storage := newDaemonClientAppManager(t)

	// Bootstrap, then strip the grant to reproduce a pre-#653 data directory.
	client, err := srv.ensureOrchestratorDaemonClient(context.Background(), mgr)
	require.NoError(t, err)
	app, err := storage.GetApp(context.Background(), client.ClientID)
	require.NoError(t, err)
	app.Scopes = nil
	require.NoError(t, storage.UpdateApp(context.Background(), app))

	again, err := srv.ensureOrchestratorDaemonClient(context.Background(), mgr)
	require.NoError(t, err)
	require.Equal(t, client.ClientID, again.ClientID, "the cached client must be reused, not replaced")
	require.Equal(t, client.ClientSecret, again.ClientSecret, "the cached secret must survive convergence")

	converged, err := storage.GetApp(context.Background(), client.ClientID)
	require.NoError(t, err)
	require.ElementsMatch(t, iam.DefaultAgentRuntimeScopes(), converged.Scopes,
		"a restart left the dispatch client without the grant it needs")
}

// TestEnsureOrchestratorDaemonClient_CachedPathToleratesNoAppManager is the
// error path for convergence: IAM init calls this before the app manager is
// necessarily available, and a cached credential is still usable then. The
// function must return it rather than failing the whole IAM bootstrap.
func TestEnsureOrchestratorDaemonClient_CachedPathToleratesNoAppManager(t *testing.T) {
	srv := withTempServer(t)
	cacheDir := srv.config.Storage.DataDir
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))
	cachePath := filepath.Join(cacheDir, "orchestrator_daemon_client.json")
	require.NoError(t, writeOrchestratorDaemonClient(cachePath, &OrchestratorDaemonClient{
		ClientID:     "app_system_cached",
		ClientSecret: "sk_cached",
		Scopes:       iam.DefaultAgentRuntimeScopes(),
		TenantID:     qrdbsdk.SystemTenantID,
	}))

	got, err := srv.ensureOrchestratorDaemonClient(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "app_system_cached", got.ClientID)
}
