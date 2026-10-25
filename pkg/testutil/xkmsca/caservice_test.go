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

// The test CA service is itself tested, because a helper that lies is worse than
// no helper: every assertion written against it inherits the lie. These tests
// establish that SignCSR really signs, that IssueCertificate really withholds
// the private key, and that each fault-injection knob does what its name says.

package xkmsca

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// newCSR builds a PKCS#10 CSR for a freshly generated key and returns both.
func newCSR(t *testing.T, commonName string) ([]byte, crypto.Signer) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader) // local-keygen-ok: a test fixture's throwaway requester key; this file is a test.
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: commonName},
		DNSNames: []string{commonName},
	}, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), key
}

// parseLeaf decodes a certificate the service returned.
func parseLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("response certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse response certificate: %v", err)
	}
	return cert
}

// SignCSR signs the CSR it is handed with the real CA: the leaf carries the
// CSR's own public key and verifies against the authority.
func TestCAServiceSignsTheSuppliedCSR(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Sign CA")
	csrPEM, key := newCSR(t, "leaf.example.test")

	resp, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{
		CSRPEM:       csrPEM,
		Profile:      "client",
		ValidityDays: 7,
		TenantID:     "automatethethings",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if svc.SignCSRCalls() != 1 {
		t.Errorf("SignCSRCalls = %d, want 1", svc.SignCSRCalls())
	}
	if svc.LastSignCSR() == nil || svc.LastSignCSR().TenantID != "automatethethings" {
		t.Error("the request was not recorded for inspection")
	}
	if len(resp.ChainPEM) == 0 {
		t.Error("no chain returned")
	}
	if resp.SerialNumber == "" {
		t.Error("no serial returned")
	}

	leaf := parseLeaf(t, resp.CertificatePEM)
	if leaf.Subject.CommonName != "leaf.example.test" {
		t.Errorf("CommonName = %q", leaf.Subject.CommonName)
	}
	if err := svc.VerifyAsClientCert(leaf); err != nil {
		t.Fatalf("signed leaf does not verify against the CA: %v", err)
	}

	got, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		t.Fatalf("marshal CSR key: %v", err)
	}
	want, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("the leaf does not carry the CSR's public key; the CA is not really signing")
	}
}

// A malformed CSR is refused by the real CA, so a caller's error path is
// exercised by a real refusal rather than a configured one.
func TestCAServiceRefusesAMalformedCSR(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Malformed CA")
	if _, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{
		TenantID: "fixtures",
		CSRPEM:   []byte("not a csr"),
	}); err == nil {
		t.Fatal("SignCSR accepted a malformed CSR")
	}
}

// SignCSRErr stands for "the CA refused" without making the rest of the type a
// fake.
func TestCAServiceSignCSRErr(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Refusal CA")
	sentinel := errors.New("ca down")
	svc.SetSignCSRErr(sentinel)
	csrPEM, _ := newCSR(t, "leaf.example.test")

	if _, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the configured sentinel", err)
	}
}

// A served CAService answers on transport handler goroutines while the test
// goroutine sets faults and reads records. Under -race this fails if any fault
// or record field is reached without the lock (go-quicbaas#1422).
func TestCAServiceIsSafeAcrossGoroutines(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Concurrency CA")
	sentinel := errors.New("ca down")
	csrPEM, _ := newCSR(t, "leaf.example.test")
	const callers = 8

	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM})
			if err != nil && !errors.Is(err, sentinel) {
				t.Errorf("SignCSR error = %v, want nil or the configured sentinel", err)
			}
			if _, err := svc.GetCABundle(context.Background(), &xkmssdk.GetCABundleRequest{}); err != nil && !errors.Is(err, sentinel) {
				t.Errorf("GetCABundle error = %v, want nil or the configured sentinel", err)
			}
		}()
	}
	svc.SetSignCSRErr(sentinel)
	svc.SetSuppressChain(true)
	svc.SetCorruptCertificatePEM(true)
	svc.SetBundleErr(sentinel)
	svc.SetNilBundleResponse(true)
	if req := svc.LastSignCSR(); req != nil && len(req.CSRPEM) == 0 {
		t.Error("LastSignCSR recorded a request without its CSR")
	}
	wg.Wait()

	if got := svc.SignCSRCalls(); got != callers {
		t.Errorf("SignCSRCalls = %d, want %d", got, callers)
	}
	if svc.LastSignCSR() == nil {
		t.Error("LastSignCSR = nil after concurrent calls, want the last request")
	}
}

// THE reason this package exists: IssueCertificate generates inside the
// keystore, so it returns a certificate and NO private key. A helper that
// invented one here would reproduce the go-quicbaas#910 defect in every test
// written against it.
func TestCAServiceIssueCertificateWithholdsThePrivateKey(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Issue CA")

	resp, err := svc.IssueCertificate(context.Background(), &xkmssdk.IssueCertificateRequest{
		TenantID:     "fixtures",
		CommonName:   "issued.example.test",
		Organization: "Automate The Things",
		ValidityDays: 7,
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if len(resp.CertificatePEM) == 0 {
		t.Fatal("no certificate returned")
	}
	if len(resp.PrivateKeyPEM) != 0 {
		t.Fatalf("PrivateKeyPEM carries %d bytes; the keystore must not export", len(resp.PrivateKeyPEM))
	}
	if parseLeaf(t, resp.CertificatePEM).Subject.CommonName != "issued.example.test" {
		t.Error("the issued certificate does not carry the requested CN")
	}
}

// IssueCertificate propagates a real CA refusal — here, an empty common name.
func TestCAServiceIssueCertificateRefusesAnEmptySubject(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Issue Refusal CA")
	if _, err := svc.IssueCertificate(context.Background(), &xkmssdk.IssueCertificateRequest{
		TenantID:     "fixtures",
		ValidityDays: 7,
	}); err == nil {
		t.Fatal("IssueCertificate accepted an empty common name")
	}
}

// The trust-material accessors return the CA's own certificate, and the
// fault-injection knobs on them behave as documented.
func TestCAServiceTrustMaterial(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Bundle CA")

	bundle, err := svc.GetCABundle(context.Background(), &xkmssdk.GetCABundleRequest{})
	if err != nil {
		t.Fatalf("GetCABundle: %v", err)
	}
	if len(bundle.BundlePEM) == 0 {
		t.Fatal("empty bundle")
	}

	caCert, err := svc.GetCACertificate(context.Background(), &xkmssdk.GetCACertificateRequest{})
	if err != nil {
		t.Fatalf("GetCACertificate: %v", err)
	}
	if string(caCert.CertificatePEM) != string(bundle.BundlePEM) {
		t.Error("the CA certificate and the bundle disagree")
	}
	if svc.TrustedCA() == nil {
		t.Error("TrustedCA() returned nil")
	}
}

// Each bundle fault produces the shape it claims: an error, a nil response, and
// a signing response with no chain.
func TestCAServiceBundleFaults(t *testing.T) {
	t.Run("BundleErr", func(t *testing.T) {
		svc := NewCAService(t, "QuicBaaS CAService BundleErr CA")
		sentinel := errors.New("no bundle")
		svc.SetBundleErr(sentinel)
		if _, err := svc.GetCABundle(context.Background(), &xkmssdk.GetCABundleRequest{}); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the configured sentinel", err)
		}
	})

	t.Run("NilBundleResponse", func(t *testing.T) {
		svc := NewCAService(t, "QuicBaaS CAService NilBundle CA")
		svc.SetNilBundleResponse(true)
		resp, err := svc.GetCABundle(context.Background(), &xkmssdk.GetCABundleRequest{})
		if err != nil {
			t.Fatalf("GetCABundle: %v", err)
		}
		if resp != nil {
			t.Fatal("response is not nil; the knob did not take effect")
		}
	})

	t.Run("SuppressChain", func(t *testing.T) {
		svc := NewCAService(t, "QuicBaaS CAService SuppressChain CA")
		svc.SetSuppressChain(true)
		csrPEM, _ := newCSR(t, "leaf.example.test")
		resp, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM})
		if err != nil {
			t.Fatalf("SignCSR: %v", err)
		}
		if len(resp.ChainPEM) != 0 {
			t.Fatal("a chain was returned; the knob did not take effect")
		}
		if len(resp.CertificatePEM) == 0 {
			t.Fatal("suppressing the chain must not suppress the leaf")
		}
	})
}

// CorruptCertificatePEM yields a PEM envelope that decodes but does not parse,
// which is what a caller's parse-failure path needs.
func TestCAServiceCorruptCertificatePEM(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Corrupt CA")
	svc.SetCorruptCertificatePEM(true)
	csrPEM, _ := newCSR(t, "leaf.example.test")

	resp, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	block, _ := pem.Decode(resp.CertificatePEM)
	if block == nil {
		t.Fatal("the corrupt response is not even PEM; it must decode and fail to parse")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err == nil {
		t.Fatal("the corrupt response parsed as a certificate")
	}
}

// Revocation round-trips through the real CA, and the CRL it generates is real
// PEM.
func TestCAServiceRevocation(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Revoke CA")
	csrPEM, _ := newCSR(t, "revoked.example.test")

	resp, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM, Profile: "client"})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	before, err := svc.IsRevoked(context.Background(), &xkmssdk.IsRevokedRequest{SerialNumber: resp.SerialNumber})
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if before.Revoked {
		t.Fatal("a freshly issued certificate is reported revoked")
	}

	revoked, err := svc.RevokeCertificate(context.Background(), &xkmssdk.RevokeCertificateRequest{
		SerialNumber: resp.SerialNumber,
		Reason:       1,
	})
	if err != nil {
		t.Fatalf("RevokeCertificate: %v", err)
	}
	if !revoked.Success {
		t.Fatal("RevokeCertificate reported failure")
	}

	after, err := svc.IsRevoked(context.Background(), &xkmssdk.IsRevokedRequest{SerialNumber: resp.SerialNumber})
	if err != nil {
		t.Fatalf("IsRevoked after revocation: %v", err)
	}
	if !after.Revoked {
		t.Fatal("the certificate is not reported revoked after revocation")
	}

	crl, err := svc.GenerateCRL(context.Background(), &xkmssdk.GenerateCRLRequest{})
	if err != nil {
		t.Fatalf("GenerateCRL: %v", err)
	}
	if len(crl.CRLPEM) == 0 {
		t.Fatal("empty CRL")
	}
}

// A serial the transport cannot parse is answered, not errored: that is what a
// served CA does with an unknown serial, and a test asserting an error here
// would be asserting something no deployment produces.
func TestCAServiceRejectsAMalformedSerial(t *testing.T) {
	svc := NewCAService(t, "QuicBaaS CAService Serial CA")

	revoked, err := svc.RevokeCertificate(context.Background(), &xkmssdk.RevokeCertificateRequest{
		SerialNumber: "not-hex-zz",
	})
	if err != nil {
		t.Fatalf("RevokeCertificate: %v", err)
	}
	if revoked.Success {
		t.Error("a malformed serial reported a successful revocation")
	}

	status, err := svc.IsRevoked(context.Background(), &xkmssdk.IsRevokedRequest{SerialNumber: "not-hex-zz"})
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if status.Revoked {
		t.Error("a malformed serial reported as revoked")
	}
}

// VerifyAsClientCert must refuse a certificate from a different authority, or it
// is not verifying anything.
func TestCAServiceVerifyAsClientCertRejectsAForeignLeaf(t *testing.T) {
	mine := NewCAService(t, "QuicBaaS CAService Verify Mine CA")
	theirs := NewCAService(t, "QuicBaaS CAService Verify Theirs CA")
	csrPEM, _ := newCSR(t, "leaf.example.test")

	resp, err := theirs.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM, Profile: "client"})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if err := mine.VerifyAsClientCert(parseLeaf(t, resp.CertificatePEM)); err == nil {
		t.Fatal("a leaf from another authority verified")
	}
}

// Over an uninitialized CA every certificate operation fails, and the trust
// material is simply absent. This is the "the CA is not up" condition a caller
// must survive, and it is produced by a real CA refusing rather than by a knob.
func TestCAServiceOverAnUninitializedCA(t *testing.T) {
	svc := NewCAServiceOver(t, NewUninitialized(t, "QuicBaaS CAService Down CA"))
	csrPEM, _ := newCSR(t, "leaf.example.test")

	if _, err := svc.SignCSR(context.Background(), &xkmssdk.SignCSRRequest{TenantID: "fixtures", CSRPEM: csrPEM}); err == nil {
		t.Error("SignCSR succeeded against a CA that is not initialized")
	}
	if _, err := svc.IssueCertificate(context.Background(), &xkmssdk.IssueCertificateRequest{
		TenantID:   "fixtures",
		CommonName: "leaf.example.test", ValidityDays: 1,
	}); err == nil {
		t.Error("IssueCertificate succeeded against a CA that is not initialized")
	}
	if _, err := svc.GenerateCRL(context.Background(), &xkmssdk.GenerateCRLRequest{}); err == nil {
		t.Error("GenerateCRL succeeded against a CA that is not initialized")
	}

	// No CA certificate exists yet, so the bundle is empty rather than wrong. A
	// caller must treat that as "no trust anchor" and refuse.
	bundle, err := svc.GetCABundle(context.Background(), &xkmssdk.GetCABundleRequest{})
	if err != nil {
		t.Fatalf("GetCABundle: %v", err)
	}
	if len(bundle.BundlePEM) != 0 {
		t.Errorf("bundle has %d bytes; an uninitialized CA has no certificate", len(bundle.BundlePEM))
	}

	// Revocation cannot be attempted either, and the serial is well-formed here
	// so the failure is the CA's rather than the parser's.
	if _, err := svc.RevokeCertificate(context.Background(), &xkmssdk.RevokeCertificateRequest{
		SerialNumber: "2a", Reason: 1,
	}); err == nil {
		t.Error("RevokeCertificate succeeded against a CA that is not initialized")
	}
	if _, err := svc.IsRevoked(context.Background(), &xkmssdk.IsRevokedRequest{SerialNumber: "2a"}); err == nil {
		t.Error("IsRevoked succeeded against a CA that is not initialized")
	}
}

// WithoutCABundle faults only the bundle call: everything else still works, so a
// caller's fallback is exercised against a CA that is otherwise real.
func TestWithoutCABundle(t *testing.T) {
	real := New(t, "QuicBaaS WithoutCABundle CA")
	sentinel := errors.New("no bundle")
	faulted := WithoutCABundle(real, sentinel)

	if _, err := faulted.CABundle(); !errors.Is(err, sentinel) {
		t.Fatalf("CABundle error = %v, want the configured sentinel", err)
	}

	// The CA's own certificate is still available — that is what makes it a
	// usable fallback anchor — and signing still works.
	caCert, err := faulted.CACertificate()
	if err != nil || caCert == nil {
		t.Fatalf("CACertificate: %v", err)
	}
	csrPEM, _ := newCSR(t, "leaf.example.test")
	if _, err := faulted.SignCSR(context.Background(), csrPEM, &xkmssdk.TrustedCASignOptions{TenantID: "fixtures", Profile: "client"}); err != nil {
		t.Fatalf("SignCSR through the faulted CA: %v", err)
	}
}

// The unfaulted CA does produce a bundle, so the test above is faulting
// something that otherwise works rather than papering over a missing feature.
func TestWithoutCABundleFaultsSomethingThatWorks(t *testing.T) {
	bundle, err := New(t, "QuicBaaS Bundle Baseline CA").CABundle()
	if err != nil {
		t.Fatalf("CABundle on a healthy CA: %v", err)
	}
	if len(bundle) == 0 {
		t.Fatal("a healthy CA returned an empty bundle")
	}
}
