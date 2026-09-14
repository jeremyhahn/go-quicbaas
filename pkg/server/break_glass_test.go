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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// writeBootstrapPIVCert issues a self-signed leaf cert, writes it to a temp PEM
// file, and returns the path plus the cert's SHA-256 thumbprint (the identifier
// bindAdminPIVCert binds to the admin and that an mTLS/PIV handshake resolves on
// the break-glass path).
func writeBootstrapPIVCert(t *testing.T) (path, thumbprint string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	certPEM := mustIssueSelfSignedCert(t, key, "break-glass-admin", now.Add(-time.Hour), now.Add(24*time.Hour))

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block, "cert PEM must decode")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	digest := sha256.Sum256(cert.Raw)
	thumbprint = hex.EncodeToString(digest[:])

	path = filepath.Join(t.TempDir(), "break-glass-admin.pem")
	require.NoError(t, os.WriteFile(path, certPEM, 0o600))
	return path, thumbprint
}

// TestBreakGlass_LocalFactorLoginWithFederationUnconfigured proves the
// always-available control-plane login path: a local-factor (PIV/mTLS) admin
// is bootstrapped, cert-bound, activated, and resolvable by its SPKI/cert
// thumbprint with NO federation manager configured. This is the structural
// guarantee that local break-glass never depends on federation: the bootstrap
// path and cert-recognition path never touch iamComponents.federationManager.
func TestBreakGlass_LocalFactorLoginWithFederationUnconfigured(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	certPath, thumbprint := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = certPath

	c := buildTestIAMComponents(t)
	// Federation is intentionally unconfigured — the break-glass path must not
	// require it.
	require.Nil(t, c.federationManager, "harness must start with federation unconfigured")

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c),
		"bootstrap + PIV cert binding must succeed with no federation manager")

	// The admin must be resolvable by its cert thumbprint — this is what an
	// mTLS/PIV handshake does to recognize the break-glass operator. It uses
	// only the identity store (system tenant = shared store), never federation.
	user, err := c.identityStore.GetUserByPIVThumbprint(ctx, thumbprint)
	require.NoError(t, err, "admin must be resolvable by cert thumbprint (break-glass recognition)")
	require.NotNil(t, user)
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle, "bound admin must be active")
	assert.Equal(t, qrdbsdk.SystemTenantID, user.TenantID,
		"break-glass admin must live in the prefix-isolated system tenant")
}

// unreachableFederationManager is a federation manager whose every call fails,
// simulating an external IdP outage (or a totally broken federation subsystem).
// It is used to prove local break-glass NEVER consults federation: if any
// break-glass step routed through federation, these errors would surface.
type unreachableFederationManager struct{}

func (unreachableFederationManager) RegisterProvider(context.Context, *iam.ExternalProvider) error {
	return &idpUnreachableError{}
}

func (unreachableFederationManager) GetProvider(context.Context, string, string) (*iam.ExternalProvider, error) {
	return nil, &idpUnreachableError{}
}

func (unreachableFederationManager) GetProviderByName(context.Context, string, string) (*iam.ExternalProvider, error) {
	return nil, &idpUnreachableError{}
}

func (unreachableFederationManager) UpdateProvider(context.Context, *iam.ExternalProvider) error {
	return &idpUnreachableError{}
}

func (unreachableFederationManager) DeleteProvider(context.Context, string, string) error {
	return &idpUnreachableError{}
}

func (unreachableFederationManager) ListProviders(context.Context, string, iam.ListOptions) ([]*iam.ExternalProvider, error) {
	return nil, &idpUnreachableError{}
}

func (unreachableFederationManager) BeginLogin(context.Context, string, string, string) (*iam.LoginRedirect, error) {
	return nil, &idpUnreachableError{}
}

func (unreachableFederationManager) HandleCallback(context.Context, string, string) (*iam.FederationResult, error) {
	return nil, &idpUnreachableError{}
}

func (unreachableFederationManager) LinkAccount(context.Context, string, string, string, string) error {
	return &idpUnreachableError{}
}

func (unreachableFederationManager) UnlinkAccount(context.Context, string, string, string) error {
	return &idpUnreachableError{}
}

func (unreachableFederationManager) GetUserLinks(context.Context, string, string) ([]*iam.FederatedIdentity, error) {
	return nil, &idpUnreachableError{}
}

// idpUnreachableError simulates an external IdP being down.
type idpUnreachableError struct{}

func (e *idpUnreachableError) Error() string { return "external IdP unreachable (simulated outage)" }

var _ iam.FederationManager = unreachableFederationManager{}

// TestBreakGlass_LocalFactorLoginWithIdPUnreachable proves the same local
// break-glass path succeeds even when a federation manager IS configured but
// the external IdP is down (every federation call errors). Because the
// bootstrap + cert-recognition path never invokes the federation manager, the
// simulated outage is invisible to control-plane login.
func TestBreakGlass_LocalFactorLoginWithIdPUnreachable(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	certPath, thumbprint := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = certPath

	c := buildTestIAMComponents(t)
	// A federation manager is present but the IdP is down — every call fails.
	c.federationManager = unreachableFederationManager{}

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c),
		"bootstrap must succeed despite a down external IdP")

	user, err := c.identityStore.GetUserByPIVThumbprint(ctx, thumbprint)
	require.NoError(t, err,
		"admin must be resolvable by cert thumbprint even with the IdP down")
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)

	// Sanity: the configured federation manager really IS broken, proving the
	// break-glass success above did not accidentally route through a healthy
	// federation path.
	_, fedErr := c.federationManager.HandleCallback(ctx, "state", "code")
	assert.Error(t, fedErr, "the simulated IdP must be unreachable")
}

// TestBreakGlass_CertLoginBeforeBarrierUnseal proves the bootstrap-ordering /
// availability invariant (c): the bootstrap admin lives in the prefix-isolated
// system tenant (shared store, NOT a per-tenant barrier), so cert/local login
// resolution works before any tenant barrier is unsealed. The harness has no
// barrier at all, and admin resolution still succeeds — exactly the
// pre-unseal control-plane condition.
func TestBreakGlass_CertLoginBeforeBarrierUnseal(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	certPath, thumbprint := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = certPath

	c := buildTestIAMComponents(t)
	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	// The admin record is in the system tenant's shared store. No barrier has
	// been initialized or unsealed in this harness, yet the cert-binding lookup
	// resolves — this is the pre-unseal availability the system tenant provides.
	user, err := c.identityStore.GetUserByPIVThumbprint(ctx, thumbprint)
	require.NoError(t, err, "system-tenant admin must resolve before any barrier unseal")
	assert.Equal(t, qrdbsdk.SystemTenantID, user.TenantID)

	// And the canonical email lookup (used by the bootstrap path) is also
	// available in the shared store pre-unseal.
	byEmail, err := c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, "admin@"+qrdbsdk.SystemTenantID+".qrdb.local")
	require.NoError(t, err, "system-tenant admin email lookup must work pre-unseal")
	assert.Equal(t, user.ID, byEmail.ID)
}

// TestOperatorSSORegistrar_Accessor proves the iamComponents accessor wires the
// operator-SSO registrar to the real federation + admin managers, and that the
// registrar it returns honours the bootstrap-ordering invariant (no admin yet
// -> registration is refused). This is the additive operator-SSO config path.
func TestOperatorSSORegistrar_Accessor(t *testing.T) {
	c := buildTestIAMComponents(t)
	c.federationManager = unreachableFederationManager{}

	registrar, err := c.OperatorSSORegistrar()
	require.NoError(t, err)
	require.NotNil(t, registrar)

	// No admin assignment exists yet, so registration must fail closed with the
	// bootstrap-ordering error — proving the registrar is bound to the real
	// admin manager gate.
	provider := &iam.ExternalProvider{
		Name: "okta-operators", Type: iam.ProviderTypeOIDC,
		ClientID: "c", IssuerURL: "https://idp.local",
	}
	err = registrar.RegisterOperatorProvider(context.Background(), provider)
	require.ErrorIs(t, err, iam.ErrBootstrapNotReady)
}

// TestOperatorSSORegistrar_Accessor_FederationUnavailable proves the accessor
// errors when federation is not configured.
func TestOperatorSSORegistrar_Accessor_FederationUnavailable(t *testing.T) {
	c := buildTestIAMComponents(t)
	c.federationManager = nil

	_, err := c.OperatorSSORegistrar()
	require.Error(t, err)
}

// TestOperatorSSORegistrar_Accessor_AdminManagerUnavailable covers the
// admin-manager-nil guard of OperatorSSORegistrar: with federation present but
// the admin manager unset, the accessor must fail with a typed
// IAMComponentInitError rather than returning a half-wired registrar.
func TestOperatorSSORegistrar_Accessor_AdminManagerUnavailable(t *testing.T) {
	c := buildTestIAMComponents(t)
	c.federationManager = unreachableFederationManager{}
	c.adminManager = nil

	_, err := c.OperatorSSORegistrar()
	require.Error(t, err)
	var initErr *IAMComponentInitError
	require.ErrorAs(t, err, &initErr)
}
