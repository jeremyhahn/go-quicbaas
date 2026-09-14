//go:build !integration

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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ---------------------------------------------------------------------------
// iamComponents getter tests
// ---------------------------------------------------------------------------

func TestIAMComponents_GettersReturnNil(t *testing.T) {
	t.Run("all getters return nil when not set", func(t *testing.T) {
		c := &iamComponents{}

		assert.Nil(t, c.GetTenantManager())
		assert.Nil(t, c.GetIdentityStore())
		assert.Nil(t, c.GetPolicyEngine())
		assert.Nil(t, c.GetAppManager())
		assert.Nil(t, c.GetOIDCProvider())
		assert.Nil(t, c.GetMFAManager())
		assert.Nil(t, c.GetWebAuthnManager())
		assert.Nil(t, c.GetAuditLogger())
		assert.Nil(t, c.GetAdminManager())
		assert.Nil(t, c.GetGDPRManager())
		assert.Nil(t, c.GetSessionStorage())
		assert.Nil(t, c.GetSessionManager())
	})
}

// ---------------------------------------------------------------------------
// Test fixtures (local to this test file; see test_helpers_test.go for
// shared helpers like withTempServer / buildTestIAMComponents).
// ---------------------------------------------------------------------------

// newTestSOCertWithSPKI generates a self-signed client certificate and returns
// its PEM bytes alongside the CANONICAL mTLS identifier — qrdb's SPKI
// thumbprint (base64url SHA-256 over SubjectPublicKeyInfo). The thumbprint is
// produced via the same parseCertLoginPEM + qrdbsdk.ComputeSPKIThumbprint pair
// the production binding paths use, so the tests assert the real code path.
func newTestSOCertWithSPKI(t *testing.T, commonName string) ([]byte, string) {
	t.Helper()
	priv := mustGenerateECKey(t)
	certPEM := mustIssueSelfSignedCert(t, priv, commonName, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cert, err := parseCertLoginPEM(certPEM)
	if err != nil {
		t.Fatalf("compute login thumbprint: %v", err)
	}
	return certPEM, qrdbsdk.ComputeSPKIThumbprint(cert)
}

// mustGenerateECKey returns a fresh P-256 ECDSA private key.
func mustGenerateECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	return priv
}

// mustIssueSelfSignedCert issues a self-signed certificate with the
// supplied private key, common name, and validity window. Returned as
// PEM bytes.
func mustIssueSelfSignedCert(t *testing.T, priv *ecdsa.PrivateKey, commonName string, notBefore, notAfter time.Time) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// ---------------------------------------------------------------------------
// reconcileAdminRole tests
//
// Stage 1.2 of the authz unification adopts qrdb's canonical admin role as the
// single source of truth: the broker no longer SEEDS its own admin role, it
// RECONCILES against qrdb's deterministic admin role (created by qrdb's
// first-boot in the system tenant). reconcileAdminRole therefore only verifies
// the canonical role is present and surfaces ErrAdminRoleMissing when it is
// absent — it never creates a parallel broker admin.
// ---------------------------------------------------------------------------

func TestReconcileAdminRole_PresentRoleOK(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// Simulate qrdb's first-boot having seeded the canonical admin role under
	// its deterministic ID in the system tenant.
	_, err := c.identityStore.CreateRole(ctx, AdminTenantID, iam.CreateRoleOptions{
		ID:          AdminRoleID,
		Name:        "admin",
		Permissions: []string{"*:*"},
	})
	require.NoError(t, err)

	require.NoError(t, reconcileAdminRole(ctx, c.identityStore),
		"reconcile must succeed once the canonical admin role exists")
}

func TestReconcileAdminRole_MissingRoleReturnsErr(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// A fresh store with no canonical admin role (first-boot not yet run /
	// storage wiped) must surface the typed missing-role error rather than
	// silently passing.
	err := reconcileAdminRole(ctx, c.identityStore)
	require.ErrorIs(t, err, ErrAdminRoleMissing)
}

func TestReconcileAdminRole_NilIdentityStoreIsNoOp(t *testing.T) {
	// Defensive: passing a nil identity store should not panic and should
	// return nil so the caller's bootstrap flow continues unaffected.
	require.NoError(t, reconcileAdminRole(context.Background(), nil))
}

// writeTestCAPEM generates a self-signed ECDSA CA certificate and writes it as
// PEM to a file under the test's temp directory, returning the path.
func writeTestCAPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PIV Activation Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return path
}

func TestLoadPIVActivationRoots_ValidBundle(t *testing.T) {
	path := writeTestCAPEM(t)

	pool, err := loadPIVActivationRoots(path)
	require.NoError(t, err)
	assert.NotNil(t, pool)
}

func TestLoadPIVActivationRoots_MissingFile(t *testing.T) {
	pool, err := loadPIVActivationRoots(filepath.Join(t.TempDir(), "absent.pem"))
	require.Error(t, err)
	assert.Nil(t, pool)

	var rootsErr *PIVActivationRootsError
	require.ErrorAs(t, err, &rootsErr)
	assert.Equal(t, "read failed", rootsErr.Reason)
	assert.Error(t, rootsErr.Unwrap())
}

func TestLoadPIVActivationRoots_NoCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a pem bundle"), 0o600))

	pool, err := loadPIVActivationRoots(path)
	require.Error(t, err)
	assert.Nil(t, pool)

	var rootsErr *PIVActivationRootsError
	require.ErrorAs(t, err, &rootsErr)
	assert.Equal(t, "no certificates parsed", rootsErr.Reason)
	assert.NoError(t, rootsErr.Unwrap())
}

func TestIAMComponents_GetPIVActivator_NilWhenUnset(t *testing.T) {
	c := &iamComponents{}
	assert.Nil(t, c.GetPIVActivator())
}

func TestIAMComponents_GetPIVActivator_ReturnsConfigured(t *testing.T) {
	activator := &iam.PIVActivator{}
	c := &iamComponents{pivActivator: activator}
	assert.Same(t, activator, c.GetPIVActivator())
}
