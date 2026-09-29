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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// bootstrapAdminEmail is the deterministic address the qrdb first-boot adapter
// synthesizes for the platform admin in the system tenant.
const bootstrapAdminEmail = "admin@" + qrdbsdk.SystemTenantID + ".qrdb.local"

// ---------------------------------------------------------------------------
// writeFirstBootBanner
// ---------------------------------------------------------------------------

func TestPrintFirstBootBanner_GenerateAndDistribute(t *testing.T) {
	var buf bytes.Buffer
	writeFirstBootBanner(&buf, adminEnrollmentBanner{
		CertPath:   "/var/lib/quicbaas/admin/admin.crt",
		KeyPath:    "/var/lib/quicbaas/admin/admin.key",
		Thumbprint: "Zm9vYmFy",
	})

	output := buf.String()
	assert.Contains(t, output, "/var/lib/quicbaas/admin/admin.crt",
		"banner must tell the operator where the admin certificate is")
	assert.Contains(t, output, "/var/lib/quicbaas/admin/admin.key",
		"banner must tell the operator where the admin private key is")
	assert.Contains(t, output, "Zm9vYmFy", "banner must show the bound SPKI thumbprint")
	assert.NotContains(t, output, "/invite/",
		"the invite activation URL was deleted; the banner must never print one")
}

func TestPrintFirstBootBanner_CSRSignPathOmitsKey(t *testing.T) {
	var buf bytes.Buffer
	writeFirstBootBanner(&buf, adminEnrollmentBanner{
		CertPath:   "/var/lib/quicbaas/admin/admin.crt",
		Thumbprint: "Zm9vYmFy",
	})

	output := buf.String()
	assert.Contains(t, output, "never reached this host",
		"the CSR-sign banner must state that the private key stayed with the admin")
	assert.NotContains(t, output, "admin.key",
		"no key file exists on the CSR-sign path, so none may be advertised")
}

func TestPrintFirstBootBanner_NoCertificateIssued(t *testing.T) {
	// A CA-less deployment still prints a banner; it must say plainly that no
	// certificate was issued rather than pointing at a file that is not there.
	var buf bytes.Buffer
	writeFirstBootBanner(&buf, adminEnrollmentBanner{})

	output := buf.String()
	require.NotEmpty(t, output)
	assert.Contains(t, output, "NO admin certificate")
	assert.NotContains(t, output, "admin.crt")
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: admin-mTLS enrollment (the point of first-boot)
// ---------------------------------------------------------------------------

// TestFirstBootBootstrap_BindsCanonicalSPKIThumbprintAndMintsNoInvite is the
// authoritative regression test for the admin-mTLS bootstrap.
//
// Before the seams were wired, firstBootBootstrap passed only
// Tenants/Roles/Users/Logger to qrdb's FirstBootInitialize, so qrdb's step 4
// (cert issuance + SPKI binding) never ran: the admin ended first-boot with NO
// certificate and NO cert binding, and quicbaas instead minted an invite token
// and printed an /ui/#/invite/<token> activation URL — the mechanism §08
// deleted. This test fails on that code in three independent ways: no
// certificate is written, the admin carries no SPKI CertBinding, and an invite
// is created.
func TestFirstBootBootstrap_BindsCanonicalSPKIThumbprintAndMintsNoInvite(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false
	// The platform CA is up before IAM init in the real boot order
	// (initializeCA runs from Server.initialize before startService).
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c, _ := buildTestIAMComponentsWithFactors(t)
	invites := &recordingInviteStore{InviteStore: c.inviteStore}
	c.inviteStore = invites

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	// 1. A certificate was issued and persisted for the operator to collect.
	certPath := filepath.Join(srv.config.Storage.DataDir, adminBundleDirName, adminCertFileName)
	require.Equal(t, certPath, banner.CertPath,
		"the banner must point at the certificate that was written")
	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err, "first-boot must write the issued admin certificate")

	keyPath := filepath.Join(srv.config.Storage.DataDir, adminBundleDirName, adminKeyFileName)
	require.Equal(t, keyPath, banner.KeyPath,
		"the generate-and-distribute path must also write the admin key")
	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.NotEmpty(t, keyPEM)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block, "admin certificate must be valid PEM")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// 2. The admin user carries the CANONICAL binding: qrdb's SPKI thumbprint,
	//    not a hex digest over the certificate DER.
	want := qrdbsdk.ComputeSPKIThumbprint(cert)
	require.NotEmpty(t, want)
	assert.Equal(t, want, banner.Thumbprint,
		"first-boot must report the canonical SPKI thumbprint it bound")

	admin, err := c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.NoError(t, err, "first-boot must have created the platform admin")
	require.NotEmpty(t, admin.CertBindings,
		"the admin must end first-boot with an mTLS cert binding")

	var bound *qrdbsdk.CertBinding
	for i := range admin.CertBindings {
		if admin.CertBindings[i].Kind == qrdbsdk.CertBindingSPKIThumbprint {
			bound = &admin.CertBindings[i]
			break
		}
	}
	require.NotNil(t, bound,
		"the admin binding must use the canonical SPKI-thumbprint kind")
	assert.Equal(t, want, bound.Thumbprint,
		"the bound thumbprint must be qrdbsdk.ComputeSPKIThumbprint of the issued cert")
	assert.Equal(t, want, admin.SPKIThumbprint())

	// 3. No invite token was minted, stored, or stamped on the user.
	assert.Zero(t, invites.creates,
		"first-boot must not create an invite record; admin enrollment is cert-based")
	// There is no invite state on the user record to check any more: the invite
	// store owns invitations, and asserting the store minted none (above) is the
	// whole of the claim.
}

// TestFirstBootBootstrap_IssuedCertIsLoginResolvable asserts the certificate
// first-boot issued is enrolled as an mtls auth factor. Without it the
// certificate authenticates the TLS handshake but `auth login --method mtls`
// fails FinishLogin with "factor not found".
func TestFirstBootBootstrap_IssuedCertIsLoginResolvable(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c, factors := buildTestIAMComponentsWithFactors(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	certPEM, err := os.ReadFile(banner.CertPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// This is the exact lookup go-qrdb's LoginResolver performs.
	factor, err := factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS,
		iam.MTLSSPKICredentialKey(cert))
	require.NoError(t, err,
		"the issued admin certificate must be enrolled as an mtls login factor")
	require.NotNil(t, factor)
}

// TestFirstBootBootstrap_CSRSignPathKeepsAdminKeyOffHost covers admin-mTLS
// Path B: with auth.bootstrap_admin_csr_path set, the CA signs the operator's
// CSR and this host never holds the admin private key.
func TestFirstBootBootstrap_CSRSignPathKeepsAdminKeyOffHost(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.Auth.BootstrapAdminCSRPath = writeTestAdminCSR(t, t.TempDir())

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	assert.NotEmpty(t, banner.CertPath, "the signed certificate must be written")
	assert.Empty(t, banner.KeyPath,
		"the CSR-sign path must not produce a private key on this host")
	_, err := os.Stat(filepath.Join(srv.config.Storage.DataDir, adminBundleDirName, adminKeyFileName))
	assert.True(t, os.IsNotExist(err), "no admin key file may exist on the CSR-sign path")

	admin, err := c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.NoError(t, err)
	assert.NotEmpty(t, admin.SPKIThumbprint(),
		"the CSR-signed certificate must still be SPKI-bound to the admin")
}

// TestFirstBootBootstrap_EmptyAdminCSRFailsClosed covers the error path for the
// CSR-sign configuration: an empty CSR file must abort rather than silently
// falling through to generate-and-distribute, which would leave a private key
// on a host the operator deliberately kept it off.
func TestFirstBootBootstrap_EmptyAdminCSRFailsClosed(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	empty := filepath.Join(t.TempDir(), "admin.csr")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	srv.config.Auth.BootstrapAdminCSRPath = empty

	c := buildTestIAMComponents(t)

	err := srv.firstBootBootstrap(context.Background(), c)
	require.Error(t, err)
	// FirstBootBootstrapError.Unwrap yields the sentinel, so reach the cause
	// through the typed error rather than errors.Is.
	var fb *FirstBootBootstrapError
	require.ErrorAs(t, err, &fb)
	assert.ErrorIs(t, fb.Cause, ErrEmptyAdminCSR)
}

// TestFirstBootBootstrap_MissingAdminCSRFailsClosed covers the other CSR
// misconfiguration: a path that does not exist.
func TestFirstBootBootstrap_MissingAdminCSRFailsClosed(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.Auth.BootstrapAdminCSRPath = filepath.Join(t.TempDir(), "absent.csr")

	c := buildTestIAMComponents(t)

	err := srv.firstBootBootstrap(context.Background(), c)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFirstBootBootstrap)
	assert.Contains(t, err.Error(), "read admin CSR")
}

// TestFirstBootBootstrap_NoCAStillCreatesAdmin covers the CA-less deployment:
// tenant, roles, admin and the admin assignment are still created; only the
// certificate is absent.
func TestFirstBootBootstrap_NoCAStillCreatesAdmin(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.xkmsCA)

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	assert.Empty(t, banner.CertPath, "no CA means no certificate is written")
	_, err := c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.NoError(t, err, "the admin user must exist even without a CA")

	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Len(t, assignments, 1)
}

// TestFirstBootBootstrap_NoDataDirRefusesToWriteAdminBundle covers the
// fail-closed guard: with storage.data_dir unset, filepath.Join would produce
// the relative path "admin" and drop the admin PRIVATE KEY into the process
// CWD (the source tree in development). First-boot must refuse instead.
func TestFirstBootBootstrap_NoDataDirRefusesToWriteAdminBundle(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.Storage.DataDir = ""

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	err := srv.firstBootBootstrap(context.Background(), c)
	require.Error(t, err)
	var fb *FirstBootBootstrapError
	require.ErrorAs(t, err, &fb)
	assert.ErrorIs(t, fb.Cause, ErrNoDataDirForAdminBundle)

	// Nothing may have been written relative to the working directory.
	_, statErr := os.Stat(filepath.Join(adminBundleDirName, adminKeyFileName))
	assert.True(t, os.IsNotExist(statErr),
		"no admin key may be written into the working directory")
}

// TestFirstBootBootstrap_AdminCertCarriesSystemTenantSPIFFESAN is the
// regression test for the "no headless path from init to an authenticated CLI"
// defect: the init-issued admin certificate was CN=admin with NO SANs at all,
// so REST's tenant resolution found no SPIFFE SAN, fell through to home-realm
// discovery, found no routing entry on a fresh node, and failed closed with
// 401 "login tenant could not be resolved". The operator could not use the very
// credential first-boot had just handed them.
//
// The assertion is end-to-end on the resolution contract, not just the SAN
// bytes: it feeds the issued certificate to the same iam.TenantFromPeerCert
// core that deriveLoginTenant delegates to, with NO home-realm resolver — i.e.
// exactly a freshly initialized node — and requires the system tenant back.
func TestFirstBootBootstrap_AdminCertCarriesSystemTenantSPIFFESAN(t *testing.T) {
	const trustDomain = "quicbaas.example.test"

	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = trustDomain

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	certPEM, err := os.ReadFile(banner.CertPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// The certificate must carry a SPIFFE URI SAN in the configured trust domain.
	require.NotEmpty(t, cert.URIs,
		"the init-issued admin certificate must carry a SPIFFE URI SAN; without one no tenant can be resolved at login")
	var spiffeURI string
	for _, u := range cert.URIs {
		if u.Scheme == "spiffe" {
			spiffeURI = u.String()
			break
		}
	}
	require.NotEmpty(t, spiffeURI, "expected a spiffe:// URI SAN, got %v", cert.URIs)
	assert.Contains(t, spiffeURI, trustDomain,
		"the SAN must be scoped to the configured trust domain")
	assert.Contains(t, spiffeURI, qrdbsdk.SystemTenantID,
		"the admin lives in the system tenant, so the SAN must name it")

	// The contract that actually matters: resolution with NO home-realm
	// resolver — a freshly initialized node — must yield the system tenant.
	tenantID, err := iam.TenantFromPeerCert(ctx, cert, trustDomain, nil, iam.RealmHints{})
	require.NoError(t, err,
		"a fresh node with no HRD entries must still resolve the login tenant from the cert SAN")
	assert.Equal(t, qrdbsdk.SystemTenantID, tenantID)
}

// TestFirstBootBootstrap_AdminCertSANPinsTrustDomain covers the negative half of
// the pin: a certificate issued under one trust domain must NOT resolve against
// a different configured trust domain.
func TestFirstBootBootstrap_AdminCertSANPinsTrustDomain(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = "issued.example.test"

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	ctx := context.Background()
	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	certPEM, err := os.ReadFile(banner.CertPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	_, err = iam.TenantFromPeerCert(ctx, cert, "someone.else.test", nil, iam.RealmHints{})
	require.Error(t, err, "a foreign trust domain must fail closed, never resolve")
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: fresh storage
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_FreshStorage_CreatesAdminAssignment(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c := buildTestIAMComponents(t)

	var stderrCapture bytes.Buffer
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, b adminEnrollmentBanner) {
		writeFirstBootBanner(&stderrCapture, b)
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))

	output := stderrCapture.String()
	assert.Contains(t, output, "FIRST-BOOT COMPLETE")
	assert.NotContains(t, output, "/invite/",
		"the invite activation URL must never be printed again")

	assignments, listErr := c.adminManager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Len(t, assignments, 1, "exactly one admin assignment must be created on first boot")
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: second boot skips — for a REAL administrator only
// ---------------------------------------------------------------------------

// TestFirstBootBootstrap_ExistingAdmin_Skipped is the second-boot no-op, and it
// is the direction that matters most: a bootstrap that re-runs on an
// administered node would refuse the boot of a healthy deployment.
//
// It seeds the shape a completed first-boot leaves behind — a system-scoped
// super_admin grant naming a user who EXISTS and may authenticate — because that
// is what the skip predicate reads. It used to seed a grant naming
// "existing-admin-id", a user that was never created, and require the skip
// anyway; that is the orphaned grant #57 is about, and the test asserting it is
// now TestFirstBootBootstrap_OrphanedGrantIsNotAnAdministrator below.
func TestFirstBootBootstrap_ExistingAdmin_Skipped(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)
	ctx := context.Background()

	admin, err := c.identityStore.CreateUser(ctx, qrdbsdk.SystemTenantID, iam.CreateUserOptions{
		Email:     bootstrapAdminEmail,
		Username:  "admin",
		Lifecycle: iam.LifecycleActive,
	})
	require.NoError(t, err)

	// Pre-seed the admin assignment a completed prior boot would have written.
	_, err = c.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    admin.ID,
		AdminRole: iam.AdminRoleSuperAdmin,
		GrantedBy: "system:first-boot",
	})
	require.NoError(t, err)

	var bannerCalled bool
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		bannerCalled = true
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(ctx, c),
		"a node that already has an administrator must boot, not re-bootstrap")

	assert.False(t, bannerCalled, "banner must NOT be printed when an admin already exists")

	// Still only one assignment.
	assignments, listErr := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Len(t, assignments, 1, "no new assignment must be created on second boot")
}

// TestFirstBootBootstrap_OrphanedGrantIsNotAnAdministrator is the same setup the
// old skip test used — a grant naming a user who does not exist — with the
// opposite expectation.
//
// The grant is ACTIVE and counted by len(), so the old rule skipped bootstrap and
// the node served with nobody able to administer it. First-boot must RE-ENTER
// instead, which is observable as the banner being printed and the administrator
// actually existing afterwards.
func TestFirstBootBootstrap_OrphanedGrantIsNotAnAdministrator(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)
	ctx := context.Background()

	_, err := c.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "existing-admin-id", // never created: the grant outlived its user
		AdminRole: iam.AdminRoleSuperAdmin,
		GrantedBy: "system:first-boot",
	})
	require.NoError(t, err)

	var bannerCalled bool
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		bannerCalled = true
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(ctx, c))

	assert.True(t, bannerCalled,
		"first-boot SKIPPED on a grant that administers nothing. The node then "+
			"serves with no administrator and nothing says so — the #57 fail-open")

	admin, err := c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.NoError(t, err, "the re-entered bootstrap must create the platform admin")
	require.True(t, admin.CanAuthenticate())

	assignments, listErr := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		AdminRole:  iam.AdminRoleSuperAdmin,
	})
	require.NoError(t, listErr)
	administered, adminErr := hasSystemAdministrator(ctx, c, assignments)
	require.NoError(t, adminErr)
	assert.True(t, administered,
		"the re-entry must leave a grant the NEXT boot recognises as an administrator")
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: password-auth deployments never reach the cert path
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_SkippedWhenPasswordAuthEnabled(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = true
	srv.config.Auth.InsecureDevMode = false
	srv.config.Auth.BootstrapAdminPassword = "Super$ecureP@ss1"

	c := buildTestIAMComponents(t)

	var bannerCalled bool
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		bannerCalled = true
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	// Simulate the gate that wraps firstBootBootstrap in initializeIAMComponents.
	if !srv.config.Auth.AllowPasswordAuth && !srv.config.Auth.InsecureDevMode {
		err := srv.firstBootBootstrap(context.Background(), c)
		require.NoError(t, err)
	}

	assert.False(t, bannerCalled, "no banner when password auth is enabled")

	assignments, listErr := c.adminManager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Empty(t, assignments, "no admin assignment when password auth flow owns bootstrap")
}

// ---------------------------------------------------------------------------
// FirstBootBootstrapError typed error
// ---------------------------------------------------------------------------

func TestFirstBootBootstrapError_IsAndMessage(t *testing.T) {
	cause := errors.New("storage failure")
	err := &FirstBootBootstrapError{Op: "create bootstrap admin user", Cause: cause}

	assert.True(t, errors.Is(err, ErrFirstBootBootstrap),
		"errors.Is must match ErrFirstBootBootstrap sentinel")
	assert.Contains(t, err.Error(), "first-boot bootstrap")
	assert.Contains(t, err.Error(), "create bootstrap admin user")
	assert.Contains(t, err.Error(), "storage failure")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// buildTestIAMComponents constructs a minimal iamComponents for unit testing
// the bootstrap logic, without starting a real server.
func buildTestIAMComponents(t *testing.T) *iamComponents {
	t.Helper()
	c, _ := buildTestIAMComponentsWithFactors(t)
	return c
}

// buildTestIAMComponentsWithFactors builds the components AND returns the
// in-memory qrdb factor store wired into them, so a certificate-binding test can
// assert the mtls factor the unified login ceremony will resolve.
func buildTestIAMComponentsWithFactors(t *testing.T) (*iamComponents, *testutil.MemoryUserAuthFactors) {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	return buildTestIAMComponentsOver(t, memStorage, memStorage, memStorage)
}

// buildTestIAMComponentsOver is buildTestIAMComponentsWithFactors with the user
// and role persistence supplied by the caller, so a fixture can put a fault
// injector or a call recorder between qrdb's first-boot and the store while
// everything else stays the ordinary in-memory wiring. memStorage backs the
// tenants and groups.
func buildTestIAMComponentsOver(
	t *testing.T,
	memStorage *iam.MemoryStorage,
	userStorage iam.UserStorage,
	roleStorage iam.RoleStorage,
) (*iamComponents, *testutil.MemoryUserAuthFactors) {
	t.Helper()

	tenantManager := iam.NewDefaultTenantManager(memStorage)

	ctx := context.Background()
	if _, err := tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "default",
		Name: "Default Tenant",
	}); err != nil {
		t.Fatalf("buildTestIAMComponents: create default tenant: %v", err)
	}
	// The bootstrap admin lives in the reserved "system" tenant (qrdb's
	// SystemTenantID), so provision it too.
	if _, err := tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   qrdbsdk.SystemTenantID,
		Name: "System Tenant",
	}); err != nil {
		t.Fatalf("buildTestIAMComponents: create system tenant: %v", err)
	}

	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
		Tenants:     tenantManager,
	})

	auditLogger, err := iam.NewAuditLogger()
	require.NoError(t, err)

	adminManager, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
		AuditLogger:          auditLogger,
	})
	require.NoError(t, err)

	// Certificate bindings must ALSO enroll an mtls login factor, so every
	// component built for a bind test carries the same two collaborators the
	// production wiring supplies: the qrdb factor store and the broker-id ->
	// qrdb-id correlation. The auto-assigning resolver models production
	// faithfully — every user created through the qrdb-backed storage has an
	// IDMap entry, a property the in-memory IAM storage cannot provide.
	certFactors := testutil.NewMemoryUserAuthFactors()
	certLoginFactors, err := iam.NewCertLoginFactorEnroller(certFactors)
	require.NoError(t, err)

	return &iamComponents{
		tenantManager:    tenantManager,
		identityStore:    identityStore,
		adminManager:     adminManager,
		inviteStore:      iamtest.NewMemoryInviteStore(),
		certLoginFactors: certLoginFactors,
	}, certFactors
}

// captureFirstBootBanner redirects the banner seam into dst and returns the
// restore func.
func captureFirstBootBanner(t *testing.T, dst *adminEnrollmentBanner) func() {
	t.Helper()
	orig := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, b adminEnrollmentBanner) {
		*dst = b
	}
	return func() { writeFirstBootBannerFn = orig }
}

// recordingInviteStore counts Create calls so a test can assert that the
// deleted invite path is not silently resurrected.
type recordingInviteStore struct {
	iam.InviteStore
	creates int
}

// Create records the call and delegates to the wrapped store.
func (s *recordingInviteStore) Create(
	ctx context.Context,
	tenantID, userID string,
	ttl time.Duration,
) (string, *iam.InviteRecord, error) {
	s.creates++
	return s.InviteStore.Create(ctx, tenantID, userID, ttl)
}

// writeTestAdminCSR generates an admin-held keypair, writes the corresponding
// CSR under dir, and returns its path. The key stays in the test — mirroring
// the operator whose key never reaches the daemon.
func writeTestAdminCSR(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	path := filepath.Join(dir, "admin.csr")
	require.NoError(t, os.WriteFile(path, csrPEM, 0o600))
	return path
}
