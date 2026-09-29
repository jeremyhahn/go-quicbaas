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

// iam_init_admin_delegation_test.go covers go-quicbaas#910 on the first-boot
// admin path: quicbaas must not build the platform admin's keypair and CSR
// itself. It leaves deps.AdminCSRPEM empty, qrdb therefore selects Path A, and
// go-xkms's CAAdapter.GenerateAdminCert does the work.
//
// The forked version worked, which is why it survived: it produced a usable
// credential. What it did not do was compose — it was a second implementation of
// a flow go-xkms owns, reached through a seam this code already populated, and a
// second implementation is a second thing to keep correct.

package server

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// With no operator CSR configured, quicbaas supplies NO CSR: the seam is left to
// qrdb, which takes Path A. Both cert seams are still populated, because Path A
// needs deps.CA and an operator who later configures a CSR needs deps.Signer.
func TestFirstBootDependencies_LeavesPathASelectionToQRDB(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c := buildTestIAMComponents(t)
	deps, users, err := srv.firstBootDependencies(c)
	require.NoError(t, err)
	require.NotNil(t, deps)
	require.NotNil(t, users)

	assert.Empty(t, deps.AdminCSRPEM,
		"quicbaas must not generate the admin CSR: an empty AdminCSRPEM is what selects qrdb Path A")
	assert.NotNil(t, deps.CA, "Path A needs the CertProvider seam")
	assert.NotNil(t, deps.Signer, "Path B needs the CertSigner seam")
}

// With an operator CSR configured, the CSR is forwarded verbatim and Path B is
// selected. The negative half of the test above: leaving AdminCSRPEM empty must
// be a decision, not the only thing this function can do.
func TestFirstBootDependencies_ForwardsTheOperatorCSR(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	csrPath := writeTestAdminCSR(t, t.TempDir())
	srv.config.Auth.BootstrapAdminCSRPath = csrPath

	want, err := os.ReadFile(csrPath)
	require.NoError(t, err)

	c := buildTestIAMComponents(t)
	deps, _, err := srv.firstBootDependencies(c)
	require.NoError(t, err)
	require.Equal(t, want, deps.AdminCSRPEM,
		"the operator's CSR must reach qrdb unmodified")
}

// The acceptance test: the credential Path A produces is usable. The certificate
// and the key written to the admin bundle are two halves of ONE keypair, the
// certificate chains to the platform CA, and go-qrdb can derive the system
// tenant from its SPIFFE SAN.
//
// Pairing the two halves is the assertion the pre-#910 tests did not make. They
// checked that a key file existed, which a delegation that returned somebody
// else's key would also satisfy.
func TestFirstBootBootstrap_DelegatedAdminKeyMatchesItsCertificate(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))

	require.NotEmpty(t, banner.CertPath, "Path A must write the issued certificate")
	require.NotEmpty(t, banner.KeyPath, "Path A must write the key go-xkms generated")

	certPEM, err := os.ReadFile(banner.CertPath)
	require.NoError(t, err)
	keyPEM, err := os.ReadFile(banner.KeyPath)
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block, "admin certificate must be valid PEM")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	require.NoError(t, err, "the delegated admin key must be decodable")
	signer, ok := key.(crypto.Signer)
	require.True(t, ok, "the delegated admin key must be a crypto.Signer, got %T", key)

	gotPub, err := x509.MarshalPKIXPublicKey(signer.Public())
	require.NoError(t, err)
	wantPub, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	require.NoError(t, err)
	require.Equal(t, wantPub, gotPub,
		"the admin key written to the bundle is not the key in the admin certificate")

	// The certificate must still authenticate into the system tenant, which is
	// what makes it usable for `quicbaas auth login --method mtls`.
	require.NotEmpty(t, cert.URIs, "the admin certificate must carry a SPIFFE URI SAN")
	identity, err := qrdbsdk.TenantFromCert(cert, "")
	require.NoError(t, err, "go-qrdb must be able to derive a tenant from the admin certificate")
	require.Equal(t, qrdbsdk.SystemTenantID, identity.TenantID)
}

// The key material must exist nowhere but the 0600 bundle file: the operator
// collects it from there, and a world-readable copy would make the mode on the
// file decorative.
func TestFirstBootBootstrap_DelegatedAdminKeyIsOwnerOnly(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c := buildTestIAMComponents(t)

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))
	require.NotEmpty(t, banner.KeyPath)

	info, err := os.Stat(banner.KeyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"the admin private key must be owner-read/write only")
}
