//go:build iam && !integration

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
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ---------------------------------------------------------------------------
// validateBootstrapCredentials tests
// ---------------------------------------------------------------------------

func TestValidateBootstrapCredentials(t *testing.T) {
	t.Run("insecure dev mode skips all checks", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "admin", true)
		assert.NoError(t, err)
	})

	t.Run("empty password in production rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
		assert.Contains(t, err.Error(), "bootstrap_admin_password is required")
	})

	t.Run("default password admin rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "admin", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password 'password' rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("user", "password", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password 'changeme' rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("user", "changeme", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password same as username rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("myuser", "myuser", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("short password rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "short", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
		assert.Contains(t, err.Error(), "at least")
	})

	t.Run("password at minimum length accepted", func(t *testing.T) {
		longPassword := "abcdefghijkl" // 12 chars = config.MinBootstrapPasswordLength
		require.GreaterOrEqual(t, len(longPassword), config.MinBootstrapPasswordLength)
		err := validateBootstrapCredentials("admin", longPassword, false)
		assert.NoError(t, err)
	})

	t.Run("strong password accepted", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "super-secure-password-2025!", false)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// resolveBootstrapCredentials tests
// ---------------------------------------------------------------------------

func TestServer_ResolveBootstrapCredentials(t *testing.T) {
	t.Run("uses configured credentials", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = "myadmin"
		srv.config.Auth.BootstrapAdminPassword = "mysecret"
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "myadmin", username)
		assert.Equal(t, "mysecret", password)
	})

	t.Run("defaults username to admin when empty", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = "pass"
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "pass", password)
	})

	t.Run("uses dev default password in insecure mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = ""
		srv.config.Auth.InsecureDevMode = true

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "quicbaas-dev-admin", password)
	})

	t.Run("leaves password empty in production mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = ""
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "", password)
	})
}

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
		assert.Nil(t, c.GetAuthManager())
		assert.Nil(t, c.GetInternalAuthManager())
	})
}

// ---------------------------------------------------------------------------
// Phase 11: admin <-> SO certificate SPKI thumbprint binding
// ---------------------------------------------------------------------------

// TestBindAdminToSOCert_PinsThumbprintAndActivatesAdmin walks the full SO-cert
// binding flow that backs path (a) of the first-boot ceremony banner.
// firstBootBootstrap creates a pending admin user; once the operator completes
// the local ceremony, BindAdminToSOCert pins the SO certificate's canonical
// login thumbprint — hex(SHA-256(cert.Raw)) — to that user. A subsequent mTLS /
// PIV handshake presenting the same cert is recognized as the admin.
//
// This is the regression guard for the thumbprint-index collision: the binding
// MUST use the same digest the login lookup computes, and the admin MUST be
// findable via GetUserByPIVThumbprint(sha256(cert.Raw)).
func TestBindAdminToSOCert_PinsThumbprintAndActivatesAdmin(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	// Run the first-boot bootstrap so the pending admin exists.
	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ string) {}
	defer func() { printFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))
	require.NotEmpty(t, srv.bootstrapAdminUserID, "firstBootBootstrap must record bootstrap admin UserID")
	require.Equal(t, "system", srv.bootstrapAdminTenantID)

	// Generate an SO certificate to bind. The helper returns the canonical
	// login thumbprint (sha256(cert.Raw)) — the value every login path uses.
	soCertPEM, loginThumbWant := newTestSOCertWithSPKI(t, "alice")

	// Bind the SO cert.
	require.NoError(t, srv.BindAdminToSOCert(context.Background(), soCertPEM))

	// The admin user stores the cert-DER login thumbprint and is active.
	user, err := c.identityStore.GetUser(context.Background(), srv.bootstrapAdminTenantID, srv.bootstrapAdminUserID)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, loginThumbWant, user.PIVCertThumbprint,
		"BindAdminToSOCert must persist hex(SHA-256(cert.Raw)) — the canonical login lookup key — to PIVCertThumbprint")
	assert.Equal(t, iam.UserStateActive, user.State,
		"binding the SO cert must transition the admin from pending to active")

	// Regression: the SO-bound admin MUST be findable by the login lookup,
	// which keys on sha256(cert.Raw). Before the fix the binding stored the
	// SPKI digest and this lookup failed with "no user found".
	ctx := iam.ContextWithTenant(context.Background(), srv.bootstrapAdminTenantID)
	found, err := c.identityStore.GetUserByPIVThumbprint(ctx, loginThumbWant)
	require.NoError(t, err, "SO-bound admin must be resolvable by the login lookup key")
	require.NotNil(t, found)
	assert.Equal(t, srv.bootstrapAdminUserID, found.ID,
		"GetUserByPIVThumbprint must resolve the SO-bound admin")

	// Regression: the PIV (cert.Raw) and SPKI digests no longer collide on the
	// PIVCertThumbprint column — they are distinct values, and only the login
	// (cert.Raw) digest indexes the user. Looking up by the SPKI digest must NOT
	// find the admin.
	spkiDigest, err := SOCertSPKIThumbprint(soCertPEM)
	require.NoError(t, err)
	require.NotEqual(t, loginThumbWant, spkiDigest,
		"login (cert.Raw) and SPKI digests must differ — they are different algorithms")
	if _, err := c.identityStore.GetUserByPIVThumbprint(ctx, spkiDigest); err == nil {
		t.Fatal("SPKI digest must NOT resolve a user — only the cert.Raw login digest indexes the admin")
	}
}

// TestBindAdminToSOCert_RebindingSameCertIsIdempotent ensures binding the SAME
// SO certificate twice yields the same login thumbprint and leaves the admin
// resolvable. The login lookup keys on the full certificate DER (cert.Raw), so
// — unlike an SPKI binding — a renewed certificate with a fresh serial produces
// a NEW thumbprint; idempotence here is per-certificate, not per-public-key.
func TestBindAdminToSOCert_RebindingSameCertIsIdempotent(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ string) {}
	defer func() { printFirstBootBannerFn = origPrint }()
	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))

	priv := mustGenerateECKey(t)
	cert := mustIssueSelfSignedCert(t, priv, "alice", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	require.NoError(t, srv.BindAdminToSOCert(context.Background(), cert))
	user1, err := c.identityStore.GetUser(context.Background(), "system", srv.bootstrapAdminUserID)
	require.NoError(t, err)
	first := user1.PIVCertThumbprint

	require.NoError(t, srv.BindAdminToSOCert(context.Background(), cert))
	user2, err := c.identityStore.GetUser(context.Background(), "system", srv.bootstrapAdminUserID)
	require.NoError(t, err)
	assert.Equal(t, first, user2.PIVCertThumbprint,
		"re-binding the same SO certificate must yield the same login thumbprint")

	want, err := soCertLoginThumbprint(cert)
	require.NoError(t, err)
	assert.Equal(t, want, first,
		"stored thumbprint must be hex(SHA-256(cert.Raw))")
}

// TestBindAdminToSOCert_RejectsMissingBootstrapAdmin ensures the binding
// path returns a FirstBootBootstrapError wrapping
// ErrBootstrapAdminMissing when called before firstBootBootstrap has
// populated the bootstrap admin. The outer error wraps the canonical
// ErrFirstBootBootstrap sentinel for errors.Is traversal; the cause
// preserves the underlying ErrBootstrapAdminMissing for callers that
// inspect the typed FirstBootBootstrapError.
func TestBindAdminToSOCert_RejectsMissingBootstrapAdmin(t *testing.T) {
	srv := withTempServer(t)
	// Intentionally do NOT call firstBootBootstrap.
	srv.iamComponents = nil

	soCertPEM, _ := newTestSOCertWithSPKI(t, "alice")
	err := srv.BindAdminToSOCert(context.Background(), soCertPEM)
	if err == nil {
		t.Fatal("expected FirstBootBootstrapError when iamComponents is nil")
	}
	if !errors.Is(err, ErrFirstBootBootstrap) {
		t.Errorf("got %v, want errors.Is(ErrFirstBootBootstrap)", err)
	}
	var typedErr *FirstBootBootstrapError
	if !errors.As(err, &typedErr) {
		t.Fatalf("got %T, want *FirstBootBootstrapError", err)
	}
	if !errors.Is(typedErr.Cause, ErrBootstrapAdminMissing) {
		t.Errorf("typedErr.Cause = %v, want errors.Is(ErrBootstrapAdminMissing)", typedErr.Cause)
	}
}

// TestBindAdminToSOCert_RejectsMalformedPEM verifies the binding path surfaces
// soCertLoginThumbprint errors as a typed FirstBootBootstrapError.
func TestBindAdminToSOCert_RejectsMalformedPEM(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ string) {}
	defer func() { printFirstBootBannerFn = origPrint }()
	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))

	if err := srv.BindAdminToSOCert(context.Background(), []byte("not a pem block")); err == nil {
		t.Fatal("expected error for malformed PEM input")
	}
}

// ---------------------------------------------------------------------------
// Test fixtures (local to this test file; see test_helpers_test.go for
// shared helpers like withTempServer / buildTestIAMComponents).
// ---------------------------------------------------------------------------

// newTestSOCertWithSPKI generates a self-signed SO certificate and returns its
// PEM bytes alongside the expected canonical login thumbprint —
// hex(SHA-256(cert.Raw)) — which is exactly what BindAdminToSOCert pins to the
// PIVCertThumbprint index and what every login path computes. The thumbprint is
// produced via the same soCertLoginThumbprint helper the production binding
// uses, so the tests assert the real code path. (Despite the historical name,
// the returned digest is the cert-DER login thumbprint, NOT the SPKI digest —
// the SPKI digest collided with the PIV index and was unreachable by any login
// lookup; see BindAdminToSOCert.)
func newTestSOCertWithSPKI(t *testing.T, commonName string) ([]byte, string) {
	t.Helper()
	priv := mustGenerateECKey(t)
	certPEM := mustIssueSelfSignedCert(t, priv, commonName, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	thumbprint, err := soCertLoginThumbprint(certPEM)
	if err != nil {
		t.Fatalf("compute login thumbprint: %v", err)
	}
	return certPEM, thumbprint
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

// TestBindAdminToSOCert_RejectsNilIdentityStore covers the second guard of
// BindAdminToSOCert: when iamComponents is present but its identityStore is nil,
// the bind must still fail closed with a typed FirstBootBootstrapError wrapping
// ErrBootstrapAdminMissing (rather than dereferencing the nil store).
func TestBindAdminToSOCert_RejectsNilIdentityStore(t *testing.T) {
	srv := withTempServer(t)
	// Present iamComponents, but with a nil identityStore.
	srv.iamComponents = &iamComponents{}

	soCertPEM, _ := newTestSOCertWithSPKI(t, "alice")
	err := srv.BindAdminToSOCert(context.Background(), soCertPEM)
	if err == nil {
		t.Fatal("expected error when identityStore is nil")
	}
	var typedErr *FirstBootBootstrapError
	if !errors.As(err, &typedErr) {
		t.Fatalf("got %T, want *FirstBootBootstrapError", err)
	}
	if !errors.Is(typedErr.Cause, ErrBootstrapAdminMissing) {
		t.Errorf("typedErr.Cause = %v, want errors.Is(ErrBootstrapAdminMissing)", typedErr.Cause)
	}
}
