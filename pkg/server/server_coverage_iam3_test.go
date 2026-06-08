//go:build iam && !integration

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
// the bulk of the IAM bootstrap can be exercised by unit tests. The other
// boost tests in this directory are gated behind raft+local+iam tags and
// therefore never run under the canonical `go test -tags iam` invocation.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
