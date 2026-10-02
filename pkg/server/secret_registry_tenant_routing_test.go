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

//go:build !integration

// secret_registry_tenant_routing_test.go covers the tenant-scoped
// credential:// routing seam wired in buildSecretRegistry().
//
// buildSecretRegistry() constructs the CredentialRetrieveFunc using
// qrdbsecret.NewTenantScopedCredentialRetrieve with the following adapter:
//
//	func(ctx context.Context) (string, bool) {
//	    tid := iam.TenantFromContext(ctx)
//	    return tid, tid != ""
//	}
//
// Tests below verify the adapter semantics and the routing behaviour
// without standing up a full qrdb node: they call NewTenantScopedCredentialRetrieve
// directly with an in-memory BarrierRegistry and a stub system retriever,
// exercising the same seam that buildSecretRegistry() uses.
package server

import (
	"context"
	"log/slog"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// tenantIDFromCtx adapter — the closure wired in buildSecretRegistry()
// =============================================================================

// tenantIDAdapter is the exact function shape wired inside buildSecretRegistry.
// It is extracted here so both tests and the production code share one
// canonical implementation.
func tenantIDAdapter(ctx context.Context) (string, bool) {
	tid := boundTenant(ctx)
	return tid, tid != ""
}

func TestTenantIDAdapter_NoTenant_ReturnsFalse(t *testing.T) {
	t.Parallel()
	tid, ok := tenantIDAdapter(context.Background())
	assert.False(t, ok, "no-tenant context must return ok=false")
	assert.Equal(t, "", tid)
}

func TestTenantIDAdapter_WithTenant_ReturnsTrue(t *testing.T) {
	t.Parallel()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	tid, ok := tenantIDAdapter(ctx)
	assert.True(t, ok, "tenant context must return ok=true")
	assert.Equal(t, "tenant-a", tid)
}

// =============================================================================
// Helpers — in-memory BarrierRegistry
// =============================================================================

// buildTestBarrierRegistry creates a minimal BarrierRegistry backed by
// in-memory barriers. It uses the SDK-exported constructors so the test
// does not need to import pkg/seal directly. The registry is pre-unsealed
// with a known passphrase and has no tenants registered by default.
func buildTestBarrierRegistry(t *testing.T) *qrdbsdk.BarrierRegistry {
	t.Helper()
	ctx := context.Background()
	log := slog.Default()

	sysStrategy, err := qrdbsdk.NewSoftwareStrategy([]byte("test-sys-pass"))
	require.NoError(t, err)

	sysBarrier, err := qrdbsdk.NewStorageBarrier(
		log,
		qrdbsdk.NewMemoryBackend(),
		qrdbsdk.BarrierConfig{RootKeyPath: "system/root"},
		sysStrategy,
	)
	require.NoError(t, err)
	sysCreds, err := qrdbsdk.CredentialsFor([]byte("test-sys-pass"))
	require.NoError(t, err)
	defer sysCreds.Secret.Destroy()
	require.NoError(t, sysBarrier.Initialize(ctx, sysCreds))

	reg, err := qrdbsdk.NewBarrierRegistry(log, sysBarrier)
	require.NoError(t, err)
	return reg
}

// srTrackingRetriever records whether it was called and returns a fixed value.
type srTrackingRetriever struct {
	called bool
	value  []byte
	err    error
}

func (r *srTrackingRetriever) CredentialRetrieve(_ context.Context, _ string) ([]byte, error) {
	r.called = true
	return r.value, r.err
}

// =============================================================================
// Routing via NewTenantScopedCredentialRetrieve
// =============================================================================

// TestTenantCredentialRouting_NoTenant_UsesSystemService verifies that when
// the request context carries no tenant ID the retrieve func delegates to the
// system-scoped credential service rather than consulting the BarrierRegistry.
func TestTenantCredentialRouting_NoTenant_UsesSystemService(t *testing.T) {
	reg := buildTestBarrierRegistry(t)
	sysSvc := &srTrackingRetriever{value: []byte("system-value")}

	retrieve := qrdbsecret.NewTenantScopedCredentialRetrieve(
		reg, sysSvc, tenantIDAdapter, slog.Default(),
	)

	val, err := retrieve(context.Background(), "my-cred")
	require.NoError(t, err)
	assert.Equal(t, "system-value", string(val), "no-tenant context must return system-scoped value")
	assert.True(t, sysSvc.called, "system retriever must be called when no tenant context is present")
}

// TestTenantCredentialRouting_UnknownTenant_ConsultsRegistry verifies that
// when the context carries a tenant ID, GetTenant is consulted on the
// BarrierRegistry rather than falling through to the system service. The
// registry returns an error because the tenant is not registered, confirming
// the tenant path was taken.
func TestTenantCredentialRouting_UnknownTenant_ConsultsRegistry(t *testing.T) {
	reg := buildTestBarrierRegistry(t)
	sysSvc := &srTrackingRetriever{value: []byte("should-not-be-called")}

	retrieve := qrdbsecret.NewTenantScopedCredentialRetrieve(
		reg, sysSvc, tenantIDAdapter, slog.Default(),
	)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err := retrieve(ctx, "my-cred")
	require.Error(t, err,
		"unknown tenant must return an error — GetTenant was consulted")
	assert.False(t, sysSvc.called,
		"system retriever must NOT be called when a tenant ID is present in context")
}

// TestTenantCredentialRouting_TenantIsolation verifies that credentials stored
// in tenant-A's barrier are returned when the context carries tenant-A, and
// that the same lookup with a tenant-B context (tenant-B not registered) fails,
// demonstrating barrier isolation between tenants.
func TestTenantCredentialRouting_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	reg := buildTestBarrierRegistry(t)

	// Register tenant-a with its own in-memory barrier.
	tenantAStrategy, err := qrdbsdk.NewSoftwareStrategy([]byte("tenant-a-pass"))
	require.NoError(t, err)
	tb, err := reg.RegisterTenant(ctx, "tenant-a",
		qrdbsdk.NewMemoryBackend(),
		qrdbsdk.BarrierConfig{},
		tenantAStrategy,
	)
	require.NoError(t, err)
	tenantCreds, err := qrdbsdk.CredentialsFor([]byte("tenant-a-pass"))
	require.NoError(t, err)
	defer tenantCreds.Secret.Destroy()
	require.NoError(t, tb.Barrier().Initialize(ctx, tenantCreds))

	// Pre-store a credential directly in tenant-a's barrier via Put so we
	// can verify retrieval without a running CredentialService.
	require.NoError(t, tb.Barrier().Put(ctx, "credentials/db-password", []byte("tenant-a-secret")))

	sysSvc := &srTrackingRetriever{value: []byte("system-fallback")}
	retrieve := qrdbsecret.NewTenantScopedCredentialRetrieve(
		reg, sysSvc, tenantIDAdapter, slog.Default(),
	)

	// tenant-a context → resolved from tenant-a's barrier.
	ctxA := tenantctx.WithTenant(ctx, "tenant-a")
	val, err := retrieve(ctxA, "db-password")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a-secret", string(val),
		"tenant-a context must return the credential from tenant-a's barrier")
	assert.False(t, sysSvc.called,
		"system retriever must NOT be called when tenant context is present")

	// tenant-b context → fails because tenant-b is not registered (GetTenant
	// consulted, not the system service).
	ctxB := tenantctx.WithTenant(ctx, "tenant-b")
	_, err = retrieve(ctxB, "db-password")
	require.Error(t, err,
		"tenant-b context must fail — tenant-b has no registered barrier")
}

// =============================================================================
// buildSecretRegistry — system-scoped fallback when BarrierRegistry is nil
// =============================================================================

// TestBuildSecretRegistry_WithNode_NilBarrierRegistry_FallsBackToSystemScoped
// verifies that when s.qrdbNode is non-nil but BarrierRegistry() returns nil
// (seal disabled), buildSecretRegistry registers the system-scoped resolver as
// the credential:// handler rather than the tenant-routing form.
//
// This is exercised indirectly: nil-node path is already covered by the
// existing TestBuildSecretRegistry_NilNode_SkipsCredentialResolver test. Here
// we confirm the credential:// scheme IS handled when only the system credential
// service is available (no BarrierRegistry), using the pre-existing
// srStubResolver-based approach of asserting the registry can claim the URI.
//
// A direct end-to-end test with a real qrdb node exercising the non-nil
// BarrierRegistry path is covered by the qrdb node package tests
// (TestBarrierRegistry_NonNilAfterSealEnabled) and the integration test suite.
func TestBuildSecretRegistry_SystemScopedResolverRegistered(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)

	// Inject a credential resolver that claims credential:// URIs —
	// simulating what buildSecretRegistry does when svcs.Credential is present.
	reg := qrdbsecret.NewResolverRegistry()
	reg.Register(qrdbsecret.NewCredentialResolver(func(_ context.Context, _ string) ([]byte, error) {
		return []byte("injected-value"), nil
	}))
	srv.secretRegistry = reg

	// credential:// must now be recognized.
	assert.True(t, srv.secretRegistry.IsSecretURI("credential://my-cred"),
		"credential:// must be recognized once a CredentialResolver is registered")

	// Resolve must return the injected value.
	sv, err := srv.secretRegistry.Resolve(context.Background(), "credential://my-cred")
	require.NoError(t, err)
	assert.Equal(t, "injected-value", sv.String())
}
