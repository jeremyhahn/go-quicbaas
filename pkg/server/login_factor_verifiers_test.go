//go:build iam

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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-xkms/pkg/auth_factors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ---------------------------------------------------------------------------
// Type() — the verifiers must report the correct factor types so the qrdb
// node keys them under the right slot.
// ---------------------------------------------------------------------------

func TestTenantWebAuthnVerifier_Type(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	assert.Equal(t, qrdbtransport.FactorTypeFIDO2, v.Type())
}

func TestLateBoundPIVVerifier_Type(t *testing.T) {
	v := newLateBoundPIVVerifier()
	assert.Equal(t, qrdbtransport.FactorTypePIV, v.Type())
}

// ---------------------------------------------------------------------------
// Fail-closed: unbound dependencies must never produce a successful or empty
// verification.
// ---------------------------------------------------------------------------

func TestLateBoundPIVVerifier_VerifyUnboundFailsClosed(t *testing.T) {
	v := newLateBoundPIVVerifier()
	err := v.Verify(context.Background(), &qrdbauth.AuthFactor{}, qrdbtransport.VerifyChallenge{})
	require.Error(t, err, "unbound PIV verifier must fail closed")
	var want *UnboundPIVVerifierError
	assert.True(t, errors.As(err, &want), "expected UnboundPIVVerifierError, got %T", err)
}

func TestTenantWebAuthnVerifier_BeginChallengeUnboundFactoryFailsClosed(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	_, _, _, err := v.BeginChallenge(context.Background(), "tenant-a", qrdbtransport.FactorTypeFIDO2, "")
	require.Error(t, err, "unbound factory must fail closed")
	var want *UnboundWebAuthnFactoryError
	assert.True(t, errors.As(err, &want), "expected UnboundWebAuthnFactoryError, got %T", err)
}

func TestTenantWebAuthnVerifier_VerifyTenantUnboundFactoryFailsClosed(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	err := v.VerifyTenant(context.Background(), "tenant-a", &qrdbauth.AuthFactor{}, qrdbtransport.VerifyChallenge{})
	require.Error(t, err, "unbound factory must fail closed")
	var want *UnboundWebAuthnFactoryError
	assert.True(t, errors.As(err, &want), "expected UnboundWebAuthnFactoryError, got %T", err)
}

// ---------------------------------------------------------------------------
// Empty-tenant rejection: WebAuthn proofs MUST be scoped to a tenant.
// ---------------------------------------------------------------------------

func TestTenantWebAuthnVerifier_BeginChallengeEmptyTenantRejected(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	// Even when the factory is bound, an empty tenant must be rejected before
	// any per-tenant resolution happens.
	v.setFactory(buildLiveWebAuthnFactory(t))
	_, _, _, err := v.BeginChallenge(context.Background(), "", qrdbtransport.FactorTypeFIDO2, "")
	require.Error(t, err)
	var want *EmptyLoginTenantError
	require.True(t, errors.As(err, &want), "expected EmptyLoginTenantError, got %T", err)
	assert.Equal(t, "BeginChallenge", want.Op)
}

func TestTenantWebAuthnVerifier_VerifyTenantEmptyTenantRejected(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	v.setFactory(buildLiveWebAuthnFactory(t))
	err := v.VerifyTenant(context.Background(), "", &qrdbauth.AuthFactor{}, qrdbtransport.VerifyChallenge{})
	require.Error(t, err)
	var want *EmptyLoginTenantError
	require.True(t, errors.As(err, &want), "expected EmptyLoginTenantError, got %T", err)
	assert.Equal(t, "VerifyTenant", want.Op)
}

// ---------------------------------------------------------------------------
// Non-tenant Verify must refuse: WebAuthn requires a tenant RP.
// ---------------------------------------------------------------------------

func TestTenantWebAuthnVerifier_NonTenantVerifyRefused(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	v.setFactory(buildLiveWebAuthnFactory(t))
	err := v.Verify(context.Background(), &qrdbauth.AuthFactor{}, qrdbtransport.VerifyChallenge{})
	require.Error(t, err, "non-tenant Verify must refuse")
	var want *NonTenantWebAuthnVerifyError
	assert.True(t, errors.As(err, &want), "expected NonTenantWebAuthnVerifyError, got %T", err)
}

// ---------------------------------------------------------------------------
// Tenant isolation: once bound, BeginChallenge/VerifyTenant resolve the
// per-tenant service, and two distinct tenants resolve to DISTINCT services.
// ---------------------------------------------------------------------------

func TestTenantWebAuthnVerifier_TenantIsolation(t *testing.T) {
	factory := buildLiveWebAuthnFactory(t)

	// Two distinct tenants must resolve to two distinct per-tenant services.
	svcA, err := factory.ServiceFor(context.Background(), "tenant-a")
	require.NoError(t, err)
	require.NotNil(t, svcA)

	svcB, err := factory.ServiceFor(context.Background(), "tenant-b")
	require.NoError(t, err)
	require.NotNil(t, svcB)

	assert.NotSame(t, svcA, svcB, "each tenant must get its own RP service (tenant isolation)")

	// And the same tenant resolves to the cached service (no cross-tenant
	// bleed, deterministic resolution).
	svcA2, err := factory.ServiceFor(context.Background(), "tenant-a")
	require.NoError(t, err)
	assert.Same(t, svcA, svcA2, "same tenant must resolve to the same cached service")

	// The bound verifier resolves the per-tenant service: BeginChallenge for a
	// tenant drives that tenant's RP ceremony (discoverable-credential flow,
	// empty hint) and returns a non-empty challenge + session metadata.
	v := newTenantWebAuthnVerifier()
	v.setFactory(factory)
	challenge, meta, _, err := v.BeginChallenge(context.Background(), "tenant-a", qrdbtransport.FactorTypeFIDO2, "")
	require.NoError(t, err, "bound verifier must open a tenant-scoped ceremony")
	assert.NotEmpty(t, challenge, "ceremony must produce a challenge")
	require.NotNil(t, meta)
	assert.NotEmpty(t, meta["session_id"], "ceremony must produce a session id")
	assert.NotEmpty(t, meta["options"], "ceremony must produce request options")
}

// ---------------------------------------------------------------------------
// PIV: a bound verifier delegates to the real xkms PIVHardwareFactorVerifier.
// ---------------------------------------------------------------------------

func TestLateBoundPIVVerifier_DelegatesToXKMSVerifier(t *testing.T) {
	root, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)

	inner, err := auth_factors.NewPIVHardwareFactorVerifier(pool, nil)
	require.NoError(t, err)

	v := newLateBoundPIVVerifier()
	v.setVerifier(inner)

	// The bound verifier delegates the actual crypto to xkms. Drive a path the
	// xkms verifier owns: a revoked factor must be rejected (proving the call
	// reached the inner verifier rather than short-circuiting). The shell does
	// no revocation logic of its own.
	revokedAt := time.Now().UTC()
	factor := &qrdbauth.AuthFactor{
		ID:        "f1",
		Type:      qrdbauth.AuthFactorPIV,
		RevokedAt: &revokedAt,
	}
	err = v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
	require.Error(t, err, "revoked factor must be rejected by the delegated xkms verifier")

	var revoked *qrdbsdk.FactorRevokedError
	assert.True(t, errors.As(err, &revoked), "expected xkms FactorRevokedError (proves delegation), got %T", err)
}

func TestLateBoundPIVVerifier_DelegatesMissingFieldRejection(t *testing.T) {
	root, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)

	inner, err := auth_factors.NewPIVHardwareFactorVerifier(pool, nil)
	require.NoError(t, err)

	v := newLateBoundPIVVerifier()
	v.setVerifier(inner)

	// Active PIV factor but no nonce/signature: the xkms verifier owns the
	// missing-field rejection. A nil/empty error here would mean the shell
	// bypassed the inner verifier.
	factor := &qrdbauth.AuthFactor{ID: "f2", Type: qrdbauth.AuthFactorPIV}
	err = v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
	require.Error(t, err, "missing challenge fields must be rejected by the delegated xkms verifier")
}

// ---------------------------------------------------------------------------
// buildLoginFactorVerifiers / bind wiring on the Server.
// ---------------------------------------------------------------------------

func TestBuildLoginFactorVerifiers_PopulatesBothShells(t *testing.T) {
	s := &Server{}
	s.buildLoginFactorVerifiers()
	require.Len(t, s.loginFactorVerifiers, 2)

	var sawPIV, sawWebAuthn bool
	for _, v := range s.loginFactorVerifiers {
		switch v.(type) {
		case *lateBoundPIVVerifier:
			sawPIV = true
		case *tenantWebAuthnVerifier:
			sawWebAuthn = true
		}
	}
	assert.True(t, sawPIV, "PIV verifier shell must be present")
	assert.True(t, sawWebAuthn, "WebAuthn verifier shell must be present")
}

func TestBuildLoginFactorVerifiers_Idempotent(t *testing.T) {
	s := &Server{}
	s.buildLoginFactorVerifiers()
	first := s.loginFactorVerifiers
	s.buildLoginFactorVerifiers()
	assert.Equal(t, len(first), len(s.loginFactorVerifiers))
	// Same backing slice — second call is a no-op.
	require.Len(t, s.loginFactorVerifiers, 2)
	assert.Same(t, &first[0], &s.loginFactorVerifiers[0])
}

func TestBindLoginFactorVerifiers_BindsWebAuthnFactory(t *testing.T) {
	s := newTestServer(t)
	s.buildLoginFactorVerifiers()

	factory := buildLiveWebAuthnFactory(t)
	mgr, err := iam.NewXKMSWebAuthnManager(factory)
	require.NoError(t, err)

	s.bindLoginFactorVerifiers(&iamComponents{webauthnManager: mgr})

	// The WebAuthn shell must now resolve the per-tenant service rather than
	// failing closed on an unbound factory.
	for _, v := range s.loginFactorVerifiers {
		if wv, ok := v.(*tenantWebAuthnVerifier); ok {
			_, _, _, beginErr := wv.BeginChallenge(context.Background(), "tenant-a", qrdbtransport.FactorTypeFIDO2, "")
			assert.NoError(t, beginErr, "factory must be bound after bindLoginFactorVerifiers")
			return
		}
	}
	t.Fatal("WebAuthn verifier shell not found")
}

func TestBindLoginFactorVerifiers_NoWebAuthnManagerLeavesUnbound(t *testing.T) {
	s := newTestServer(t)
	s.buildLoginFactorVerifiers()

	// No webauthn manager: the WebAuthn shell must remain unbound and fail
	// closed (never silently succeed).
	s.bindLoginFactorVerifiers(&iamComponents{})

	for _, v := range s.loginFactorVerifiers {
		if wv, ok := v.(*tenantWebAuthnVerifier); ok {
			_, _, _, beginErr := wv.BeginChallenge(context.Background(), "tenant-a", qrdbtransport.FactorTypeFIDO2, "")
			require.Error(t, beginErr)
			var want *UnboundWebAuthnFactoryError
			assert.True(t, errors.As(beginErr, &want), "expected fail-closed unbound factory error, got %T", beginErr)
			return
		}
	}
	t.Fatal("WebAuthn verifier shell not found")
}

// ---------------------------------------------------------------------------
// Error message smoke tests.
// ---------------------------------------------------------------------------

func TestLoginFactorVerifierErrors_Messages(t *testing.T) {
	assert.Contains(t, (&EmptyLoginTenantError{Op: "VerifyTenant"}).Error(), "VerifyTenant")
	assert.NotEmpty(t, (&EmptyLoginTenantError{}).Error())
	assert.NotEmpty(t, (&UnboundWebAuthnFactoryError{}).Error())
	assert.NotEmpty(t, (&NonTenantWebAuthnVerifyError{}).Error())
	assert.NotEmpty(t, (&UnboundPIVVerifierError{}).Error())
}

// ---------------------------------------------------------------------------
// Test helpers (real dependencies, no mocks).
// ---------------------------------------------------------------------------

// newTestServer builds a minimal Server with a real logger for tests that
// exercise the bind path (which logs when a dependency is absent).
func newTestServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return &Server{logger: log}
}

// buildLiveWebAuthnFactory constructs a real WebAuthnServiceFactory backed by a
// live in-memory qrdb standalone node, so ServiceFor() exercises the real
// per-tenant store-adapter construction path (real tenant isolation, no mock).
func buildLiveWebAuthnFactory(t *testing.T) *iam.WebAuthnServiceFactory {
	t.Helper()

	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	client := node.Client()
	require.NotNil(t, client, "memory node must expose a live SDK client")

	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "localhost",
			RPDisplayName: "QuicBaaS Test",
			RPOrigins:     []string{"https://localhost"},
			Timeout:       60 * time.Second,
		},
		QRDB: client,
	})
	require.NoError(t, err)
	return factory
}

// newTestPIVRoot builds a self-signed CA certificate suitable for use as a PIV
// trust root in tests. It returns the parsed certificate and its private key.
func newTestPIVRoot(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "QuicBaaS Test PIV Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	// Sanity: the cert is PEM-encodable (mirrors how roots arrive from the CA).
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NotEmpty(t, pemBlock)

	return cert, key
}

// TestTenantWebAuthnVerifier_VerifyTenantReachesVerifier covers the delegation
// line of VerifyTenant: with a bound factory and a non-empty tenant, the
// per-tenant verifier resolves and VerifyTenant delegates to verifier.Verify
// (which then rejects the empty challenge — the meaningful coverage is that the
// delegation path executed rather than failing earlier).
func TestTenantWebAuthnVerifier_VerifyTenantReachesVerifier(t *testing.T) {
	v := newTenantWebAuthnVerifier()
	v.setFactory(buildLiveWebAuthnFactory(t))

	err := v.VerifyTenant(context.Background(), "tenant-a", &qrdbauth.AuthFactor{}, qrdbtransport.VerifyChallenge{})
	// The empty factor/challenge causes verifier.Verify to reject; the point is
	// that resolution succeeded and the delegation line ran (no Unbound/Empty
	// tenant error).
	require.Error(t, err)
	var unbound *UnboundWebAuthnFactoryError
	require.False(t, errors.As(err, &unbound), "must have reached the per-tenant verifier, not the unbound guard")
	var empty *EmptyLoginTenantError
	require.False(t, errors.As(err, &empty), "a non-empty tenant must not hit the empty-tenant guard")
}
