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

package certfixture

import (
	"context"
	"crypto/x509"
	"errors"
	"path/filepath"
	"testing"
)

// trustDomain is the SPIFFE trust domain these tests issue under.
const trustDomain = "certfixture.test"

// openTestAuthority opens a fixture CA under the test's temp directory.
func openTestAuthority(t *testing.T) *Authority {
	t.Helper()
	a, err := Open(context.Background(), Config{
		Dir:               t.TempDir(),
		SPIFFETrustDomain: trustDomain,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return a
}

// trustPool returns a pool holding the authority's own certificate.
func trustPool(t *testing.T, a *Authority) *x509.CertPool {
	t.Helper()
	caPEM, err := a.CACertificatePEM()
	if err != nil {
		t.Fatalf("CACertificatePEM: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("the authority's own certificate is not usable as a trust anchor")
	}
	return pool
}

// =============================================================================
// Open
// =============================================================================

// Open creates the CA on the first call and its own certificate is a usable
// trust anchor.
func TestOpen_CreatesAUsableAuthority(t *testing.T) {
	a := openTestAuthority(t)
	if a.TrustDomain() != trustDomain {
		t.Errorf("TrustDomain() = %q, want %q", a.TrustDomain(), trustDomain)
	}
	trustPool(t, a)
}

// Open without a directory is refused. The fixture CA is deliberately
// persistent: the cluster's material is issued by one process and extended by
// the suites that run against it, so an in-memory CA would leave every later
// caller unable to issue anything that chains to the cluster's trust anchor.
func TestOpen_RejectsAnEmptyDir(t *testing.T) {
	a, err := Open(context.Background(), Config{})
	if !errors.Is(err, ErrNoDir) {
		t.Fatalf("Open error = %v, want ErrNoDir", err)
	}
	if a != nil {
		t.Error("Open returned an authority alongside the error")
	}
}

// A second Open over the same directory ADOPTS the existing root, so a leaf the
// suite issues at runtime verifies against the ca.crt the nodes were started
// with. The whole runtime-credential path depends on this.
func TestOpen_SecondProcessIssuesUnderTheSameRoot(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Dir: dir, SPIFFETrustDomain: trustDomain}

	first, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	pool := trustPool(t, first)

	second, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	issued, err := second.IssueClient(context.Background(), ClientFixture{
		CommonName: "runtime@certfixture.test",
		TenantID:   "acme",
	})
	if err != nil {
		t.Fatalf("IssueClient from the second handle: %v", err)
	}
	if _, err := issued.Certificate.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("a leaf issued by the second handle does not chain to the first root: %v", err)
	}
}

// The keystore lives in a named subdirectory of the fixture directory, so a
// harness can mount or clean it without knowing the layout.
func TestKeystoreDir(t *testing.T) {
	if got, want := KeystoreDir("/etc/quicbaas/certs"), filepath.Join("/etc/quicbaas/certs", "ca-keystore"); got != want {
		t.Errorf("KeystoreDir = %q, want %q", got, want)
	}
	if got, want := KeystoreDir(""), "ca-keystore"; got != want {
		t.Errorf("KeystoreDir(\"\") = %q, want %q", got, want)
	}
}

// =============================================================================
// IssueServer
// =============================================================================

// A node's serving credential carries every declared name, both server and
// client extended key usage, and a private key its holder can present it with.
//
// Both EKUs matter: a cluster node SERVES TLS and DIALS its peers over mTLS with
// the same certificate, so a server-only leaf breaks Raft.
func TestIssueServer_CarriesTheDeclaredNamesAndBothEKUs(t *testing.T) {
	a := openTestAuthority(t)
	pool := trustPool(t, a)

	issued, err := a.IssueServer(context.Background(), ServerFixture{
		File:        "node1",
		CommonName:  "test-node1",
		DNSNames:    []string{"localhost", "test-node1", "quicbaas-node1"},
		IPAddresses: []string{"127.0.0.1", "::1"},
	})
	if err != nil {
		t.Fatalf("IssueServer: %v", err)
	}
	if len(issued.PrivateKeyPEM) == 0 || issued.Signer == nil {
		t.Fatal("IssueServer returned no usable private key")
	}

	cert := issued.Certificate
	if cert.Subject.CommonName != "test-node1" {
		t.Errorf("CN = %q, want test-node1", cert.Subject.CommonName)
	}
	for _, want := range []string{"localhost", "test-node1", "quicbaas-node1"} {
		if err := cert.VerifyHostname(want); err != nil {
			t.Errorf("certificate is not valid for %q: %v", want, err)
		}
	}
	if len(cert.IPAddresses) != 2 {
		t.Errorf("IP SANs = %v, want two", cert.IPAddresses)
	}

	for _, eku := range []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth} {
		if _, err := cert.Verify(x509.VerifyOptions{
			Roots:     pool,
			DNSName:   "test-node1",
			KeyUsages: []x509.ExtKeyUsage{eku},
		}); err != nil {
			t.Errorf("serving leaf does not verify for EKU %v: %v", eku, err)
		}
	}
	if len(cert.URIs) != 0 {
		t.Errorf("URI SANs = %v; a node identity carries no tenant SAN", cert.URIs)
	}
}

// A fixture with no common name is refused: the CA addresses everything by
// subject, so an empty CN produces a certificate nothing can look up.
func TestIssueServer_RejectsAnEmptyCommonName(t *testing.T) {
	a := openTestAuthority(t)
	if _, err := a.IssueServer(context.Background(), ServerFixture{File: "node1"}); !errors.Is(err, ErrNoCommonName) {
		t.Fatalf("error = %v, want ErrNoCommonName", err)
	}
}

// A malformed IP SAN is reported rather than dropped. Silently losing an address
// SAN fails at TLS handshake time, a long way from the typo that caused it.
func TestIssueServer_RejectsAMalformedIPSAN(t *testing.T) {
	a := openTestAuthority(t)
	_, err := a.IssueServer(context.Background(), ServerFixture{
		File:        "node1",
		CommonName:  "test-node1",
		IPAddresses: []string{"127.0.0.300"},
	})
	var invalid *InvalidIPError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want *InvalidIPError", err)
	}
	if invalid.Address != "127.0.0.300" {
		t.Errorf("InvalidIPError.Address = %q, want 127.0.0.300", invalid.Address)
	}
}

// =============================================================================
// IssueClient
// =============================================================================

// A tenant-scoped client credential carries ClientAuth only, the email SAN, and
// the tenant-scoped SPIFFE URI SAN that login derives the tenant from.
func TestIssueClient_CarriesTheTenantSPIFFESAN(t *testing.T) {
	a := openTestAuthority(t)
	pool := trustPool(t, a)

	issued, err := a.IssueClient(context.Background(), ClientFixture{
		File:       "admin",
		CommonName: "admin@certfixture.test",
		Email:      "admin@certfixture.test",
		TenantID:   "system",
	})
	if err != nil {
		t.Fatalf("IssueClient: %v", err)
	}

	cert := issued.Certificate
	want := "spiffe://" + trustDomain + "/tenant/system/user/admin@certfixture.test"
	var uris []string
	for _, u := range cert.URIs {
		uris = append(uris, u.String())
	}
	if len(uris) != 1 || uris[0] != want {
		t.Fatalf("URI SANs = %v, want [%s]", uris, want)
	}
	if len(cert.EmailAddresses) != 1 || cert.EmailAddresses[0] != "admin@certfixture.test" {
		t.Errorf("email SANs = %v, want [admin@certfixture.test]", cert.EmailAddresses)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("EKU = %v, want ClientAuth only", cert.ExtKeyUsage)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("client leaf does not chain to the authority: %v", err)
	}
	if issued.Signer == nil {
		t.Error("IssueClient returned no signer; a PIV challenge could not be answered")
	}
}

// A tenant-scoped credential from an authority with no trust domain is refused.
// It would carry no tenant SAN, and login derives the tenant from that SAN, so
// issuing one would hand back a credential that cannot log in.
func TestIssueClient_RejectsATenantWithoutATrustDomain(t *testing.T) {
	a, err := Open(context.Background(), Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = a.IssueClient(context.Background(), ClientFixture{
		CommonName: "admin@certfixture.test",
		TenantID:   "system",
	})
	if !errors.Is(err, ErrNoTrustDomain) {
		t.Fatalf("error = %v, want ErrNoTrustDomain", err)
	}
}

// A client fixture with no common name is refused.
func TestIssueClient_RejectsAnEmptyCommonName(t *testing.T) {
	a := openTestAuthority(t)
	if _, err := a.IssueClient(context.Background(), ClientFixture{File: "admin"}); !errors.Is(err, ErrNoCommonName) {
		t.Fatalf("error = %v, want ErrNoCommonName", err)
	}
}

// =============================================================================
// decoding
// =============================================================================

// A certificate PEM the CA could not have produced is reported, not guessed at.
func TestDecodeCertificate_RejectsBadInput(t *testing.T) {
	if _, err := decodeCertificate(nil); err == nil {
		t.Error("decodeCertificate(nil) succeeded")
	}
	if _, err := decodeCertificate([]byte("not pem")); err == nil {
		t.Error("decodeCertificate(garbage) succeeded")
	}
	notACert := []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n")
	var decodeErr *DecodeError
	if _, err := decodeCertificate(notACert); !errors.As(err, &decodeErr) {
		t.Errorf("error = %v, want *DecodeError", err)
	}
}

// The private key decoder is go-xkms's own, the counterpart of the encoder the
// adapter used. Asserting that pairing here is the point: go-xkms labels an
// ECDSA key's PEM block "EC PRIVATE KEY" while the DER inside is PKCS#8, so the
// obvious-looking crypto/x509.ParseECPrivateKey fails on real material — which
// is exactly the trap the old fixture helpers were in.
func TestDecodeSigner_AcceptsWhatTheAdapterEncodesAndRejectsGarbage(t *testing.T) {
	a := openTestAuthority(t)
	issued, err := a.IssueClient(context.Background(), ClientFixture{
		CommonName: "signer@certfixture.test",
		TenantID:   "system",
	})
	if err != nil {
		t.Fatalf("IssueClient: %v", err)
	}
	signer, err := decodeSigner(issued.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("decodeSigner over the adapter's own output: %v", err)
	}
	if signer.Public() == nil {
		t.Error("decoded signer has no public key")
	}

	if _, err := decodeSigner(nil); err == nil {
		t.Error("decodeSigner(nil) succeeded")
	}
	if _, err := decodeSigner([]byte("-----BEGIN EC PRIVATE KEY-----\nZm9v\n-----END EC PRIVATE KEY-----\n")); err == nil {
		t.Error("decodeSigner(garbage) succeeded")
	}
}

// validity applies the default only when nothing was asked for.
func TestValidity(t *testing.T) {
	if got := validity(0); got != DefaultValidityDays {
		t.Errorf("validity(0) = %d, want %d", got, DefaultValidityDays)
	}
	if got := validity(-3); got != DefaultValidityDays {
		t.Errorf("validity(-3) = %d, want %d", got, DefaultValidityDays)
	}
	if got := validity(30); got != 30 {
		t.Errorf("validity(30) = %d, want 30", got)
	}
}
