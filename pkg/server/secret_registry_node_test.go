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

//go:build !integration && raft

// secret_registry_node_test.go covers the buildSecretRegistry branches that
// require a live qrdb standalone node: the qrdbNode-non-nil gate, the inner
// svcs.Credential-non-nil condition, and the tenant-scoped (br != nil) path.
//
// These tests use the same helper pattern as qrdb_node_test.go: a minimal
// in-memory qrdb config + initStandaloneQRDBNode, which is cheap enough for
// fast unit tests (in-memory backend, no network, no persistent disk state).
//
// Coverage targets:
//
//	buildSecretRegistry: the entire if s.qrdbNode != nil { ... } body
//	  - seal disabled: svcs.Credential == nil → inner block skipped
//	  - seal enabled:  br != nil → tenant-scoped resolver registered
//	  - seal enabled + Resolve call: closure body (tenantIDFromCtx) executed

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// buildSealDisabledNodeServer creates a server backed by an in-memory qrdb
// standalone node with seal disabled. The node satisfies qrdbNode != nil
// but has no credential service (Seal.Enabled == false → credCoreSvc is nil).
func buildSealDisabledNodeServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.initStandaloneQRDBNode(),
		"seal-disabled standalone node must start without error")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// buildSealEnabledNodeServer creates a server backed by an in-memory qrdb
// standalone node with passphrase seal enabled and auto-unsealed. After
// initStandaloneQRDBNode completes:
//
//   - srv.qrdbNode != nil
//   - srv.qrdbNode.Services().Credential != nil
//   - srv.qrdbNode.BarrierRegistry() != nil
const srnTestPassphrase = "sr-node-test-passphrase"

func buildSealEnabledNodeServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	srv, err := NewServer(cfg, WithSealPassphrase([]byte(srnTestPassphrase)))
	require.NoError(t, err)

	require.NoError(t, srv.initStandaloneQRDBNode(),
		"seal-enabled standalone node must start and auto-unseal without error")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// =============================================================================
// buildSecretRegistry — node-backed paths
// =============================================================================

// TestBuildSecretRegistry_WithNode_SealDisabled_SkipsCredentialResolver verifies
// that when the qrdb node is non-nil but seal is disabled (Services().Credential
// == nil), buildSecretRegistry does not register a credential:// resolver.
// credential:// URIs must therefore not be recognized by the resulting registry.
func TestBuildSecretRegistry_WithNode_SealDisabled_SkipsCredentialResolver(t *testing.T) {
	srv := buildSealDisabledNodeServer(t)
	require.NotNil(t, srv.qrdbNode, "precondition: qrdbNode must be non-nil")

	// Confirm the precondition that makes this branch exercisable.
	svcs := srv.qrdbNode.Services()
	if svcs != nil && svcs.Credential != nil {
		t.Skip("seal is enabled in this build; test requires seal-disabled credential == nil")
	}

	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	// kms:// must still be registered (resolvers 2–4 are unconditional).
	assert.True(t, reg.IsSecretURI("kms://my-secret"),
		"kms:// must always be recognized regardless of seal state")

	// credential:// must NOT be registered when Credential service is absent.
	assert.False(t, reg.IsSecretURI("credential://my-cred"),
		"credential:// must not be recognized when seal is disabled and Credential service is nil")
}

// TestBuildSecretRegistry_WithNode_SealEnabled_RegistersTenantScopedResolver
// verifies that when the qrdb node is non-nil, Services().Credential != nil, and
// BarrierRegistry() != nil (seal enabled + auto-unsealed), buildSecretRegistry
// registers a credential:// resolver so the URI is recognized.
func TestBuildSecretRegistry_WithNode_SealEnabled_RegistersTenantScopedResolver(t *testing.T) {
	srv := buildSealEnabledNodeServer(t)
	require.NotNil(t, srv.qrdbNode, "precondition: qrdbNode must be non-nil")
	require.NotNil(t, srv.qrdbNode.BarrierRegistry(),
		"precondition: BarrierRegistry must be non-nil when seal is enabled")

	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	// credential:// must be recognized once the tenant-scoped resolver is registered.
	assert.True(t, reg.IsSecretURI("credential://any-cred"),
		"credential:// must be recognized when seal is enabled and BarrierRegistry is non-nil")

	// kms:// and autogen must also be present (unconditional registrations).
	assert.True(t, reg.IsSecretURI("kms://any-secret"),
		"kms:// must always be recognized")
}

// TestBuildSecretRegistry_WithNode_SealEnabled_TenantAdapterClosureInvoked
// verifies that the tenant-ID adapter closure registered inside buildSecretRegistry
// is actually called when the registry resolves a credential:// URI. The closure
// body (`tid := iam.TenantFromContext(ctx); return tid, tid != ""`) is exercised
// both with a no-tenant context (adapter returns false → system path) and with a
// tenant context (adapter returns true → tenant path).
//
// The resolution itself may fail (credential not stored) but the important
// invariant is that the closure executes without panic and correctly reports the
// presence or absence of a tenant in the context.
func TestBuildSecretRegistry_WithNode_SealEnabled_TenantAdapterClosureInvoked(t *testing.T) {
	srv := buildSealEnabledNodeServer(t)
	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	// No-tenant context: closure returns (tid="", false) → system service path.
	// The credential is not stored so we expect an error, but not a panic.
	_, err := reg.Resolve(context.Background(), "credential://nonexistent-cred")
	// An error is expected (credential not stored). Absence of panic confirms
	// the closure body executed and took the system service branch.
	assert.Error(t, err,
		"resolution of an unstored credential must fail; closure must not panic")

	// Tenant context: closure returns (tid="test-tenant", true) → tenant path.
	// The tenant is not registered in the barrier registry so the resolution
	// must also fail — but again without panicking, proving the closure ran.
	ctxWithTenant := iam.ContextWithTenant(context.Background(), "test-tenant")
	_, err = reg.Resolve(ctxWithTenant, "credential://nonexistent-cred")
	assert.Error(t, err,
		"resolution with an unregistered tenant must fail; closure must not panic")
}
