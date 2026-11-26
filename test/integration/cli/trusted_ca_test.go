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

//go:build integration

// trusted_ca_test.go drives `quicbaas trusted-ca`, the CLI over the go-xkms CA
// re-exports Security().TrustedCA() and Security().TCGCA(), end to end on
// every enabled transport.

package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/pkg/api/transport"
)

// uniqueTrustedCAName returns a common name no other run or transport uses.
func uniqueTrustedCAName(prefix string) string {
	return fmt.Sprintf("%s-%d.trusted-ca.test", prefix, time.Now().UnixNano())
}

// parsePEMCertificate decodes the first CERTIFICATE block of data.
func parsePEMCertificate(t *testing.T, data []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(data)
	require.NotNil(t, block, "the response should hold a PEM block")
	require.Equal(t, "CERTIFICATE", block.Type)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err, "the PEM certificate should parse")
	return cert
}

// writeTrustedCACSR writes a P-256 PKCS#10 CSR for commonName and returns its
// path.
func writeTrustedCACSR(t *testing.T, commonName string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: commonName},
		DNSNames: []string{commonName},
	}, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "req.csr")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), 0o600))
	return path
}

// writeTrustedCAPublicKey writes a P-256 PKIX public key PEM and returns its
// path.
func writeTrustedCAPublicKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ek.pub.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600))
	return path
}

// TestTrustedCABundleAndCertificate fetches the CA bundle and the CA
// certificate and checks each holds a parseable certificate.
func TestTrustedCABundleAndCertificate(t *testing.T) {
	RunForAllTransports(t, "TrustedCABundleAndCertificate", func(t *testing.T, runner *TestRunner) {
		stdout, _, err := runner.Run(TrustedCABundleCommand())
		require.NoError(t, err, "trusted-ca bundle should succeed")
		var bundle xkmstransport.GetCABundleResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &bundle), "bundle json should parse")
		parsePEMCertificate(t, bundle.BundlePEM)

		stdout, _, err = runner.Run(TrustedCACertificateCommand())
		require.NoError(t, err, "trusted-ca certificate should succeed")
		var caCert xkmstransport.GetCACertificateResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &caCert), "certificate json should parse")
		cert := parsePEMCertificate(t, caCert.CertificatePEM)
		assert.True(t, cert.IsCA, "the CA certificate is a CA")
	})
}

// TestTrustedCASignCSRRevokeRoundTrip signs a CSR, then revokes the leaf and
// watches is-revoked flip from false to true.
func TestTrustedCASignCSRRevokeRoundTrip(t *testing.T) {
	RunForAllTransports(t, "TrustedCASignCSRRevokeRoundTrip", func(t *testing.T, runner *TestRunner) {
		commonName := uniqueTrustedCAName("csr")
		stdout, _, err := runner.Run(TrustedCASignCSRCommand(writeTrustedCACSR(t, commonName)))
		require.NoError(t, err, "trusted-ca sign-csr should succeed")
		var signed xkmstransport.SignCSRResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &signed), "sign-csr json should parse")
		leaf := parsePEMCertificate(t, signed.CertificatePEM)
		assert.Equal(t, commonName, leaf.Subject.CommonName)
		require.NotEmpty(t, signed.SerialNumber, "sign-csr returns the serial number")

		var status xkmstransport.IsRevokedResponse
		stdout, _, err = runner.Run(TrustedCAIsRevokedCommand(signed.SerialNumber))
		require.NoError(t, err, "trusted-ca is-revoked should succeed")
		require.NoError(t, json.Unmarshal([]byte(stdout), &status))
		assert.False(t, status.Revoked, "a fresh leaf is not revoked")

		stdout, _, err = runner.Run(TrustedCARevokeCommand(signed.SerialNumber))
		require.NoError(t, err, "trusted-ca revoke should succeed")
		var revoked xkmstransport.RevokeCertificateResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &revoked))
		assert.True(t, revoked.Success, "the revocation is acknowledged")

		status = xkmstransport.IsRevokedResponse{}
		stdout, _, err = runner.Run(TrustedCAIsRevokedCommand(signed.SerialNumber))
		require.NoError(t, err, "trusted-ca is-revoked should succeed after revoke")
		require.NoError(t, json.Unmarshal([]byte(stdout), &status))
		assert.True(t, status.Revoked, "the leaf is revoked")
	})
}

// TestTrustedCAIssue issues a certificate with a CA-generated key.
func TestTrustedCAIssue(t *testing.T) {
	RunForAllTransports(t, "TrustedCAIssue", func(t *testing.T, runner *TestRunner) {
		commonName := uniqueTrustedCAName("issue")
		stdout, _, err := runner.Run(TrustedCAIssueCommand(commonName))
		require.NoError(t, err, "trusted-ca issue should succeed")
		var issued xkmstransport.IssueCertificateResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &issued), "issue json should parse")
		cert := parsePEMCertificate(t, issued.CertificatePEM)
		assert.Equal(t, commonName, cert.Subject.CommonName)
		assert.NotEmpty(t, issued.SerialNumber)
	})
}

// TestTCGCAIssueEK issues an EK certificate through Security().TCGCA().
//
// The server issues TCG certificates in the system tenant only. Every
// transport in this suite authenticates as the system admin (see
// RunForTransports and TestRunner.Authenticate), so the caller is the system
// tenant and the issuance succeeds.
func TestTCGCAIssueEK(t *testing.T) {
	RunForAllTransports(t, "TCGCAIssueEK", func(t *testing.T, runner *TestRunner) {
		commonName := uniqueTrustedCAName("ek")
		stdout, stderr, err := runner.Run(TCGCAIssueEKCommand(commonName, writeTrustedCAPublicKey(t)))
		require.NoError(t, err, "trusted-ca tcg ek should succeed for the system tenant: %s", stderr)
		var issued xkmstransport.IssueEKCertificateResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &issued), "tcg ek json should parse")
		cert, perr := x509.ParseCertificate(issued.CertificateDER)
		require.NoError(t, perr, "the EK certificate should parse")
		assert.NotNil(t, cert)
		assert.NotEmpty(t, issued.SerialNumber)
	})
}

// TestTrustedCAErrorSuite checks the CLI refuses missing flags and arguments
// before reaching the server.
func TestTrustedCAErrorSuite(t *testing.T) {
	RunForAllTransports(t, "TrustedCAErrorSuite", func(t *testing.T, runner *TestRunner) {
		for _, tc := range []struct {
			cmd  CommandDef
			want string
		}{
			{TrustedCASignCSRMissingCSRCommand(), "--csr is required"},
			{TrustedCAIssueMissingCommonNameCommand(), "--common-name is required"},
			{TrustedCARevokeMissingSerialCommand(), "<serial> argument is required"},
			{TCGCAIssueEKMissingPublicKeyCommand(), "--public-key is required"},
		} {
			t.Run(tc.cmd.Name, func(t *testing.T) {
				stdout, stderr, err := runner.Run(tc.cmd)
				require.Error(t, err, "%s should fail", tc.cmd.Name)
				assert.Contains(t, stdout+stderr, tc.want)
			})
		}
	})
}
