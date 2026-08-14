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

// secret_registry_test.go covers ErrKMSUnavailableError,
// lazyKMSSecretsClient, buildSecretRegistry (nil-node path), GetSecretRegistry,
// and the compile-time interface checks. Boot-order regression (CA runs after
// node init) lives in secret_registry_boot_order_test.go under the raft+local tag.

package server

import (
	"context"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Compile-time interface assertions
// =============================================================================

var _ secrets.SecretsClient = (*lazyKMSSecretsClient)(nil)

// =============================================================================
// Typed test errors
// =============================================================================

// srTestError is a typed error for secret-registry tests.
type srTestError struct{ msg string }

func (e *srTestError) Error() string { return e.msg }

// =============================================================================
// ErrKMSUnavailableError
// =============================================================================

func TestErrKMSUnavailableError_Message(t *testing.T) {
	t.Parallel()
	err := &ErrKMSUnavailableError{}
	assert.Contains(t, err.Error(), "kms")
	assert.Contains(t, err.Error(), "startSecrets")
}

func TestErrKMSUnavailableError_IsDetectable(t *testing.T) {
	t.Parallel()
	var target *ErrKMSUnavailableError
	wrapped := &ErrKMSUnavailableError{}
	assert.ErrorAs(t, wrapped, &target)
}

// =============================================================================
// lazyKMSSecretsClient
// =============================================================================

func TestLazyKMSSecretsClient_NilService_ReturnsUnavailableError(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.quicBaaSService = nil // explicitly nil

	c := &lazyKMSSecretsClient{s: srv}
	_, err := c.GetSecret(context.Background(), "test-secret")
	require.Error(t, err)
	var unavail *ErrKMSUnavailableError
	assert.ErrorAs(t, err, &unavail,
		"a nil quicBaaSService must return ErrKMSUnavailableError")
}

func TestLazyKMSSecretsClient_NilSecretsSubService_ReturnsUnavailableError(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)

	// Wire a service whose Secrets() returns nil to exercise the second guard.
	srv.quicBaaSService = &srNilSecretsQuicBaaS{}

	c := &lazyKMSSecretsClient{s: srv}
	_, err := c.GetSecret(context.Background(), "test-secret")
	require.Error(t, err)
	var unavail *ErrKMSUnavailableError
	assert.ErrorAs(t, err, &unavail,
		"a nil Secrets() sub-service must return ErrKMSUnavailableError")
}

func TestLazyKMSSecretsClient_GetSecret_Success(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	want := []byte("retrieved-secret-value")
	srv.quicBaaSService = &srFixedSecretsQuicBaaS{
		svc: &srFixedSecretsService{data: want},
	}

	c := &lazyKMSSecretsClient{s: srv}
	got, err := c.GetSecret(context.Background(), "my-secret-id")
	require.NoError(t, err)
	assert.Equal(t, want, got, "GetSecret must return the data from the underlying service")
}

func TestLazyKMSSecretsClient_GetSecret_ServiceError_Propagates(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	wantErr := &srTestError{msg: "secret not found in KMS"}
	srv.quicBaaSService = &srFixedSecretsQuicBaaS{
		svc: &srFixedSecretsService{err: wantErr},
	}

	c := &lazyKMSSecretsClient{s: srv}
	_, err := c.GetSecret(context.Background(), "missing-secret")
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr,
		"GetSecret must propagate errors from the underlying SecretsService")
}

// =============================================================================
// buildSecretRegistry — nil-node path
// =============================================================================

func TestBuildSecretRegistry_NilNode_SkipsCredentialResolver(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	assert.Nil(t, srv.qrdbNode, "precondition: qrdbNode must be nil before node init")

	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	// At least the kms/remote-kms adapters must be registered.
	assert.GreaterOrEqual(t, len(reg.Resolvers()), 1,
		"at least one resolver (kms://) must be registered")

	// kms:// must be handled by the lazy adapter.
	assert.True(t, reg.IsSecretURI("kms://my-secret"),
		"kms:// URIs must be recognized even without a live SecretsService")

	// credential:// is NOT recognized without the qrdb node.
	assert.False(t, reg.IsSecretURI("credential://my-cred"),
		"credential:// must not be recognized when the qrdb node is absent")
}

func TestBuildSecretRegistry_NilNode_LiteralFallthrough(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	// A plain string that is not a known URI resolves as literal.
	sv, err := reg.Resolve(context.Background(), "plain-value")
	require.NoError(t, err)
	assert.Equal(t, "plain-value", sv.String())
	assert.Equal(t, qrdbsecret.StrategyLiteral, sv.Strategy)
}

// TestBuildSecretRegistry_PlaceholderValuesResolveVerbatim guards the resolver
// SPI contract that dispatch happens by URI scheme only. Values that merely
// look like placeholders carry no scheme, so they must round-trip untouched
// under StrategyLiteral instead of being swapped for generated entropy — the
// regression that replaced a 6-digit TPM PIN with 43 base64 characters and
// blew the 32-byte TPM2B_AUTH limit.
func TestBuildSecretRegistry_PlaceholderValuesResolveVerbatim(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	reg := srv.buildSecretRegistry()
	require.NotNil(t, reg)

	for _, raw := range []string{"123456", "auto", "changeme", "CHANGEME", "placeholder"} {
		assert.False(t, reg.IsSecretURI(raw),
			"%q carries no scheme and must not be treated as a secret reference", raw)

		sv, err := reg.Resolve(context.Background(), raw)
		require.NoError(t, err)
		assert.Equal(t, raw, sv.String(),
			"%q must resolve verbatim through the literal fallback", raw)
		assert.Equal(t, qrdbsecret.StrategyLiteral, sv.Strategy)
	}
}

// =============================================================================
// GetSecretRegistry
// =============================================================================

func TestGetSecretRegistry_InitiallyNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	assert.Nil(t, srv.GetSecretRegistry(),
		"GetSecretRegistry must return nil before initializeCA runs")
}

func TestGetSecretRegistry_ReturnsStoredRegistry(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	reg := qrdbsecret.NewResolverRegistry()
	srv.secretRegistry = reg
	assert.Same(t, reg, srv.GetSecretRegistry(),
		"GetSecretRegistry must return the stored registry instance")
}

// =============================================================================
// CA password resolution via initializeCA()
// =============================================================================

// TestInitializeCA_RootKeyPassword_Literal_ReachesCA verifies that when
// RootKeyPassword is a plain literal (not a URI), initializeCA() resolves it
// without error and continues to the CA construction step (newCAFn is reached).
//
// newCAFn is stubbed to return a typed sentinel so initializeCA() aborts after
// the resolution block without any real filesystem access. An error containing
// "resolve CA root key password" would indicate the resolution logic failed.
func TestInitializeCA_RootKeyPassword_Literal_ReachesCA(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.CA.RootKeyPassword = "my-literal-password"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Pre-inject a registry so buildSecretRegistry() is NOT called inside
	// initializeCA() — the pre-set empty registry uses the literal fallback.
	srv.secretRegistry = qrdbsecret.NewResolverRegistry()

	seamMu.Lock()
	origNewCAFn := newCAFn
	newCAFn = func(_ *xkmssdk.TrustedCAParams) (xkmssdk.TrustedCA, error) {
		return nil, &srCAPasswordSentinelError{}
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origNewCAFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	// Any error except "resolve CA root key password" is acceptable here:
	// it means resolution succeeded and execution advanced to newCAFn.
	if err != nil {
		assert.NotContains(t, err.Error(), "resolve CA root key password",
			"literal RootKeyPassword must not cause a resolution failure: %v", err)
	}
}

// TestInitializeCA_RootKeyPassword_EmptySkipsResolution verifies that an empty
// RootKeyPassword does not trigger any resolution logic.
func TestInitializeCA_RootKeyPassword_EmptySkipsResolution(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.CA.RootKeyPassword = "" // empty → resolution block is skipped entirely

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.secretRegistry = qrdbsecret.NewResolverRegistry()

	seamMu.Lock()
	origNewCAFn := newCAFn
	newCAFn = func(_ *xkmssdk.TrustedCAParams) (xkmssdk.TrustedCA, error) {
		return nil, &srCAPasswordSentinelError{}
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origNewCAFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	if err != nil {
		assert.NotContains(t, err.Error(), "resolve CA root key password",
			"empty RootKeyPassword must not trigger resolution: %v", err)
	}
}

// TestInitializeCA_RootKeyPassword_ResolutionFailure_Propagates verifies that
// when the configured credential resolver returns an error, initializeCA wraps
// it as "resolve CA root key password".
func TestInitializeCA_RootKeyPassword_ResolutionFailure_Propagates(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.CA.RootKeyPassword = "credential://missing-cred"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Register a credential resolver that claims and fails the URI so it is
	// NOT treated as a literal and resolution returns a hard error.
	reg := qrdbsecret.NewResolverRegistry()
	reg.Register(qrdbsecret.NewCredentialResolver(func(_ context.Context, _ string) ([]byte, error) {
		return nil, &srTestError{msg: "credential not found"}
	}))
	srv.secretRegistry = reg

	err = srv.initializeCA()
	require.Error(t, err,
		"initializeCA must return an error when RootKeyPassword resolution fails")
	assert.Contains(t, err.Error(), "resolve CA root key password")
}

// srCAPasswordSentinelError is the typed sentinel for CA-password tests.
type srCAPasswordSentinelError struct{}

func (e *srCAPasswordSentinelError) Error() string { return "sr-test: CA construction sentinel" }

// =============================================================================
// Stub helpers
// =============================================================================

// srStubResolver is a minimal secrets.SecretResolver for tests.
// secrets.SecretValue is an alias for qrdbsecret.SecretValue; construct values
// via secrets.NewSecretValue rather than struct literals.
type srStubResolver struct {
	scheme     string
	canResolve bool
	sv         *secrets.SecretValue
	err        error
}

func (r *srStubResolver) Scheme() string           { return r.scheme }
func (r *srStubResolver) CanResolve(_ string) bool { return r.canResolve }
func (r *srStubResolver) Resolve(_ context.Context, _ string) (*secrets.SecretValue, error) {
	return r.sv, r.err
}

var _ secrets.SecretResolver = (*srStubResolver)(nil)

// srNilSecretsQuicBaaS is a minimal service.QuicBaaS whose Secrets() returns nil.
// All other methods delegate to the embedded nil interface and would panic if called —
// lazyKMSSecretsClient only calls Secrets() so that is safe here.
type srNilSecretsQuicBaaS struct {
	service.QuicBaaS // nil embed — only Secrets() is exercised
}

func (n *srNilSecretsQuicBaaS) Secrets() service.SecretsService { return nil }

// srFixedSecretsService is a minimal service.SecretsService stub that returns a
// fixed payload (or error) from GetSecret. All other interface methods delegate to
// the embedded nil interface; lazyKMSSecretsClient only calls GetSecret.
type srFixedSecretsService struct {
	service.SecretsService // nil embed — only GetSecret is exercised
	data                   []byte
	err                    error
}

func (s *srFixedSecretsService) GetSecret(_ context.Context, _ *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &service.GetSecretResponse{Data: s.data}, nil
}

// srFixedSecretsQuicBaaS is a minimal service.QuicBaaS that returns a fixed
// SecretsService from Secrets(). All other interface methods delegate to the
// embedded nil interface; lazyKMSSecretsClient only calls Secrets().
type srFixedSecretsQuicBaaS struct {
	service.QuicBaaS // nil embed — only Secrets() is exercised
	svc              service.SecretsService
}

func (n *srFixedSecretsQuicBaaS) Secrets() service.SecretsService { return n.svc }
