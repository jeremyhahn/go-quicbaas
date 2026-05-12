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
// iamComponents.GetAuthManager with non-nil value
// ---------------------------------------------------------------------------

func TestIAMComponents_GetAuthManager_NonNil(t *testing.T) {
	t.Run("returns non-nil when set", func(t *testing.T) {
		adapter := &inMemoryAuthManagerAdapter{manager: nil}
		c := &iamComponents{
			authManager: adapter,
		}
		assert.NotNil(t, c.GetAuthManager())
	})
}

func TestIAMComponents_GetInternalAuthManager_NonNil(t *testing.T) {
	t.Run("returns non-nil adapter when auth manager set", func(t *testing.T) {
		adapter := &inMemoryAuthManagerAdapter{manager: nil}
		c := &iamComponents{
			authManager: adapter,
		}
		// GetInternalAuthManager creates an interfacesAuthManagerAdapter
		// but requires a non-nil manager field. Since we pass nil, this
		// will work but the adapter wraps nil.
		result := c.GetInternalAuthManager()
		assert.NotNil(t, result)
	})
}

// ---------------------------------------------------------------------------
// Phase 11: admin <-> SO certificate SPKI thumbprint binding
// ---------------------------------------------------------------------------

// TestBindAdminToSOCert_PinsThumbprintAndActivatesAdmin walks the full
// SO-cert binding flow that backs path (a) of the first-boot ceremony
// banner. firstBootBootstrap creates a pending admin user; once the
// operator completes the local ceremony, BindAdminToSOCert pins the SO
// certificate's SPKI thumbprint to that user. A subsequent mTLS
// handshake presenting the same cert (or any cert with the same public
// key) is recognized as the admin.
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
	require.Equal(t, "default", srv.bootstrapAdminTenantID)

	// Generate an SO certificate to bind.
	soCertPEM, soCertSPKIWant := newTestSOCertWithSPKI(t, "alice")

	// Bind the SO cert.
	require.NoError(t, srv.BindAdminToSOCert(context.Background(), soCertPEM))

	// Verify the admin user now stores the SHA-256 SPKI thumbprint and
	// is in the active state.
	user, err := c.identityStore.GetUser(context.Background(), srv.bootstrapAdminTenantID, srv.bootstrapAdminUserID)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, soCertSPKIWant, user.PIVCertThumbprint,
		"BindAdminToSOCert must persist the SHA-256 SPKI thumbprint to the canonical PIVCertThumbprint field")
	assert.Equal(t, iam.UserStateActive, user.State,
		"binding the SO cert must transition the admin from pending to active")
}

// TestBindAdminToSOCert_RebindingSameKeyIsIdempotent ensures binding the
// same SO public key (potentially a renewed certificate) yields the same
// thumbprint. This is the SPKI-binding invariant that survives cert
// renewal as long as the public key remains stable.
func TestBindAdminToSOCert_RebindingSameKeyIsIdempotent(t *testing.T) {
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
	cert1 := mustIssueSelfSignedCert(t, priv, "alice", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cert2 := mustIssueSelfSignedCert(t, priv, "alice", time.Now().Add(-time.Hour), time.Now().Add(2*time.Hour))

	require.NoError(t, srv.BindAdminToSOCert(context.Background(), cert1))
	user1, err := c.identityStore.GetUser(context.Background(), "default", srv.bootstrapAdminUserID)
	require.NoError(t, err)
	first := user1.PIVCertThumbprint

	require.NoError(t, srv.BindAdminToSOCert(context.Background(), cert2))
	user2, err := c.identityStore.GetUser(context.Background(), "default", srv.bootstrapAdminUserID)
	require.NoError(t, err)
	assert.Equal(t, first, user2.PIVCertThumbprint,
		"renewing the SO cert with the same public key must yield the same SPKI thumbprint")
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

// TestBindAdminToSOCert_RejectsMalformedPEM verifies the binding path
// surfaces SOCertSPKIThumbprint errors as typed FirstBootBootstrapError.
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

// newTestSOCertWithSPKI generates a self-signed SO certificate and
// returns its PEM bytes alongside the expected base64url-encoded SHA-256
// SPKI thumbprint. The thumbprint is computed via SOCertSPKIThumbprint
// so the test asserts the same code path the production binding uses.
func newTestSOCertWithSPKI(t *testing.T, commonName string) ([]byte, string) {
	t.Helper()
	priv := mustGenerateECKey(t)
	certPEM := mustIssueSelfSignedCert(t, priv, commonName, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	thumbprint, err := SOCertSPKIThumbprint(certPEM)
	if err != nil {
		t.Fatalf("compute SPKI thumbprint: %v", err)
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
// ensureAdminRole tests
//
// Regression for the IAM Roles tab showing "No roles defined" while the
// Users tab shows the admin role. Without a corresponding Role row in the
// IdentityStore the two surfaces drift; ensureAdminRole keeps them in
// sync.
// ---------------------------------------------------------------------------

func TestEnsureAdminRole_CreatesAdminRoleInDefaultTenant(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// Pre-condition: the role store starts empty for the default tenant.
	rolesBefore, err := c.identityStore.ListRoles(ctx, "default", iam.ListOptions{Limit: 100})
	require.NoError(t, err)
	require.Empty(t, rolesBefore, "fresh tenant must start with no roles")

	require.NoError(t, ensureAdminRole(ctx, c.identityStore, "default"))

	role, err := c.identityStore.GetRole(ctx, "default", AdminRoleID)
	require.NoError(t, err, "admin role must exist after ensureAdminRole")
	require.NotNil(t, role)
	assert.Equal(t, AdminRoleID, role.ID)
	assert.Equal(t, "admin", role.Name)
	assert.Contains(t, role.Permissions, "*",
		"admin role must carry the wildcard permission so policy enforcement treats it as full access")

	// Post-condition: the Roles tab API surface (ListRoles) returns the
	// admin role so the IAM UI no longer reports an empty roster while the
	// Users tab shows the role label.
	rolesAfter, err := c.identityStore.ListRoles(ctx, "default", iam.ListOptions{Limit: 100})
	require.NoError(t, err)
	require.Len(t, rolesAfter, 1)
	assert.Equal(t, AdminRoleID, rolesAfter[0].ID)
}

func TestEnsureAdminRole_IsIdempotent(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// First call seeds the role.
	require.NoError(t, ensureAdminRole(ctx, c.identityStore, "default"))

	// Second call must be a no-op: no error, no duplicate row.
	require.NoError(t, ensureAdminRole(ctx, c.identityStore, "default"))

	roles, err := c.identityStore.ListRoles(ctx, "default", iam.ListOptions{Limit: 100})
	require.NoError(t, err)
	require.Len(t, roles, 1, "ensureAdminRole must not create duplicate rows on repeated invocations")
}

func TestEnsureAdminRole_NilIdentityStoreIsNoOp(t *testing.T) {
	// Defensive: passing a nil identity store should not panic and should
	// return nil so the caller's bootstrap flow continues unaffected.
	require.NoError(t, ensureAdminRole(context.Background(), nil, "default"))
}

func TestEnsureAdminRole_RejectsInvalidTenant(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// An empty tenant ID must surface an error rather than silently
	// creating a role under the zero-value tenant.
	err := ensureAdminRole(ctx, c.identityStore, "")
	require.Error(t, err)
}
