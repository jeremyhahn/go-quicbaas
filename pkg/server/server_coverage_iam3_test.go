//go:build !integration

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

// server_coverage_iam3_test.go targets the high-leverage iam_init.go
// initializeIAMComponents function with a real in-memory qrdb client so
// the bulk of the IAM bootstrap can be exercised by unit tests. Other boost
// tests in this directory are gated behind raft+local tags and therefore do
// not run under a minimal build.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// iam_init.go — initializeIAMComponents
//
// With a freshly-created ShardedClient (embedded memory transport) bound to
// s.qrdbClient and the bootstrap PIV path empty, initializeIAMComponents
// exercises: backendStorage construction, OIDC provider with ephemeral RSA
// key, MFA manager, admin manager, GDPR manager, federation manager,
// orchestrator daemon-client bootstrap, ensureAdminRole, and the
// firstBootBootstrap path. The function must return a non-nil components
// bundle and a nil error.
// =============================================================================

func TestServer_InitializeIAMComponents_NilQRDBClientShortCircuits(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.qrdbClient, "withTempServer leaves qrdbClient nil")

	got, err := srv.initializeIAMComponents()
	require.NoError(t, err)
	assert.Nil(t, got, "with nil qrdb client initializeIAMComponents returns (nil, nil)")
}

func TestServer_InitializeIAMComponents_WithEmbeddedQRDB(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	// Wire the node-backed ShardedClient exactly as startService does, so
	// GetKVStore resolves to the real engine client (the qrdb node owns storage).
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)

	got, err := srv.initializeIAMComponents()
	require.NoError(t, err)
	require.NotNil(t, got, "initializeIAMComponents must return components when qrdbClient is wired")

	// Spot-check the major sub-managers — they are constructed unconditionally
	// when the qrdb client is available.
	assert.NotNil(t, got.GetTenantManager())
	assert.NotNil(t, got.GetIdentityStore())
	assert.NotNil(t, got.GetPolicyEngine())
	assert.NotNil(t, got.GetAppManager())
	assert.NotNil(t, got.GetAdminManager())
	assert.NotNil(t, got.GetAppStorage())
	assert.NotNil(t, got.GetSessionStorage())
	assert.NotNil(t, got.GetAuditLogger())
	assert.NotNil(t, got.GetGDPRManager())
}

// TestServer_InitializeIAMComponents_DoesNotCreateRetiredDefaultTenant is the
// regression guard for the tenant-fallback migration. qrdb permanently retired
// the "default" tenant id, and qrdb's first-boot auto-provisions the "system"
// tenant. The quicbaas IAM init path must therefore:
//   - NEVER create (or even probe-then-create) the retired "default" tenant.
//   - Provision the orchestrator daemon client under qrdbsdk.SystemTenantID.
func TestServer_InitializeIAMComponents_DoesNotCreateRetiredDefaultTenant(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)

	got, err := srv.initializeIAMComponents()
	require.NoError(t, err)
	require.NotNil(t, got)

	// The retired "default" tenant must NOT exist — IAM init no longer attempts
	// to create it. (qrdb would reject a public create anyway; the point is that
	// quicbaas does not try.)
	_, defErr := got.GetTenantManager().GetTenant(context.Background(), "default")
	require.Error(t, defErr, "the retired \"default\" tenant must not be created during IAM init")

	// The orchestrator daemon client must be bootstrapped under the system
	// tenant.
	require.NotNil(t, srv.orchestratorDaemonClient, "orchestrator daemon client must be bootstrapped")
	assert.Equal(t, qrdbsdk.SystemTenantID, srv.orchestratorDaemonClient.TenantID,
		"orchestrator daemon client must live under the system tenant")
}

// TestServer_InitializeIAMComponents_ClientWithoutNodeFailsClosed exercises the
// fail-closed invariant added in phase C1-F: user persistence is owned by the
// embedded qrdb node, so a qrdb client wired WITHOUT a node (an inconsistent
// state with nowhere to store users) must abort IAM init with a typed
// QRDBUserStorageRequiredError rather than silently routing user storage to a
// local (empty) store. The KVStore is still resolvable via the node-backed
// client (so the pre-check IAM wiring builds), but s.qrdbNode is nil at the
// point of the user-storage decision.
func TestServer_InitializeIAMComponents_ClientWithoutNodeFailsClosed(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	// Wire the node-backed ShardedClient so the IAM KVStore resolves, then drop
	// the node reference to simulate the client-without-node inconsistency.
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)
	srv.qrdbNode = nil

	got, err := srv.initializeIAMComponents()
	assert.Nil(t, got, "no components must be returned when failing closed")
	require.Error(t, err, "client-without-node must fail closed")

	var typed *QRDBUserStorageRequiredError
	require.True(t, errors.As(err, &typed), "expected QRDBUserStorageRequiredError, got %v", err)
	assert.True(t, errors.Is(err, ErrQRDBUserStorageRequired), "must unwrap to the sentinel")
}
