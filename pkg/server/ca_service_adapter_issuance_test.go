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

// ca_service_adapter_issuance_test.go is the acceptance test for
// go-quicbaas#910 on the `ca issue` path: after routing key creation through the
// single audited generator in pkg/xkms, every certificate the adapter issues
// must still VERIFY AGAINST THE CA'S OWN CHAIN, and the private key handed back
// must still be the one in that certificate.
//
// It runs against a real go-xkms CA, not a mock. A mock would happily return a
// certificate and a key that have nothing to do with each other, which is
// exactly the defect class this file exists to catch.

package server

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// Every algorithm the adapter advertises produces a leaf that chains to the CA
// that signed it, carries the requested subject and SANs, and comes back with a
// private key whose public half is the leaf's.
//
// RSA-3072 and RSA-4096 are exercised by
// TestCAServiceAdapter_IssueCertificate_LargeRSA so the common path stays fast.
func TestCAServiceAdapter_IssueCertificate_VerifiesAgainstTheCAChain(t *testing.T) {
	for _, algorithm := range []string{"", "ecdsa", "p256", "ecdsa-p384", "rsa", "ed25519"} {
		t.Run(algorithm, func(t *testing.T) {
			ca := xkmsca.New(t, "QuicBaaS #910 Issue Root CA "+algorithm)
			adapter, err := newCAServiceAdapter(ca)
			if err != nil {
				t.Fatalf("newCAServiceAdapter: %v", err)
			}

			resp, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
				CommonName:   "leaf-" + algorithm + ".example.test",
				Organization: "Automate The Things",
				SANs:         []string{"DNS:leaf.example.test", "IP:127.0.0.1"},
				ValidityDays: 30,
				Algorithm:    algorithm,
			})
			if err != nil {
				t.Fatalf("IssueCertificate(%q): %v", algorithm, err)
			}

			leaf := parseIssuedLeaf(t, resp.CertificatePEM)

			// THE acceptance assertion: the CA that signed it accepts it.
			if _, err := ca.Verify(leaf); err != nil {
				t.Fatalf("issued leaf does not verify against the CA chain: %v", err)
			}

			// The key the caller was handed must open that certificate. A
			// certificate paired with the wrong key is worse than no
			// certificate: it fails at the first handshake, far from here.
			assertKeyMatchesCertificate(t, resp.PrivateKeyPEM, leaf)

			if leaf.Subject.CommonName != "leaf-"+algorithm+".example.test" {
				t.Errorf("CommonName = %q", leaf.Subject.CommonName)
			}
			if len(leaf.DNSNames) == 0 || leaf.DNSNames[0] != "leaf.example.test" {
				t.Errorf("DNSNames = %v, want the requested DNS SAN", leaf.DNSNames)
			}
			if len(leaf.IPAddresses) == 0 {
				t.Error("IP SAN missing from the issued leaf")
			}
			if resp.SerialNumber == "" {
				t.Error("response carries no serial number")
			}
			if len(resp.ChainPEM) == 0 {
				t.Error("response carries no CA chain")
			}
		})
	}
}

// The larger RSA moduli issue and verify too, in their own test so the table
// above does not pay for them.
func TestCAServiceAdapter_IssueCertificate_LargeRSA(t *testing.T) {
	for _, algorithm := range []string{"rsa-3072", "rsa4096"} {
		t.Run(algorithm, func(t *testing.T) {
			ca := xkmsca.New(t, "QuicBaaS #910 RSA Root CA "+algorithm)
			adapter, err := newCAServiceAdapter(ca)
			if err != nil {
				t.Fatalf("newCAServiceAdapter: %v", err)
			}

			resp, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
				CommonName:   "rsa-leaf.example.test",
				ValidityDays: 30,
				Algorithm:    algorithm,
			})
			if err != nil {
				t.Fatalf("IssueCertificate(%q): %v", algorithm, err)
			}
			leaf := parseIssuedLeaf(t, resp.CertificatePEM)
			if _, err := ca.Verify(leaf); err != nil {
				t.Fatalf("issued leaf does not verify against the CA chain: %v", err)
			}
			assertKeyMatchesCertificate(t, resp.PrivateKeyPEM, leaf)
			if leaf.PublicKeyAlgorithm != x509.RSA {
				t.Fatalf("public key algorithm = %v, want RSA", leaf.PublicKeyAlgorithm)
			}
		})
	}
}

// The adapter's published error for an algorithm it cannot honour survives the
// move of key creation into pkg/xkms. Callers match this sentinel; swapping it
// for the pkg/xkms typed error would be an undeclared API change.
func TestCAServiceAdapter_IssueCertificate_UnsupportedAlgorithm(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Unsupported Root CA")
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	for _, algorithm := range []string{"dilithium", "rsa1024", "ecdsa-p521"} {
		t.Run(algorithm, func(t *testing.T) {
			resp, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
				CommonName: "leaf.example.test",
				Algorithm:  algorithm,
			})
			if err == nil {
				t.Fatalf("expected an error for algorithm %q", algorithm)
			}
			if resp != nil {
				t.Fatal("no response should be returned on failure")
			}
			if !errors.Is(err, ErrCAServiceAdapterUnsupportedAlgorithm) {
				t.Fatalf("error should be ErrCAServiceAdapterUnsupportedAlgorithm, got %v", err)
			}
			// The pkg/xkms sentinel must NOT leak to the caller in its place.
			if errors.Is(err, dbxkms.ErrUnsupportedKeyAlgorithm) {
				t.Fatal("the pkg/xkms sentinel leaked through the adapter's error contract")
			}
		})
	}
}

// A nil request is refused before any key is created, so a malformed call costs
// no entropy and leaves nothing in the CA's stores.
func TestCAServiceAdapter_IssueCertificate_NilRequest(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Nil Request Root CA")
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	resp, err := adapter.IssueCertificate(context.Background(), nil)
	if !errors.Is(err, ErrCAServiceAdapterNilRequest) {
		t.Fatalf("error = %v, want ErrCAServiceAdapterNilRequest", err)
	}
	if resp != nil {
		t.Fatal("no response should be returned for a nil request")
	}
}

// Re-issuing for a Common Name that already holds a certificate must keep
// working. The obvious "delegate everything" fix — calling
// TrustedCA.IssueCertificate instead of signing a CSR — cannot do this: it
// refuses with ErrCertificateAlreadyExists, which would break the embedded
// agent's stable per-host identity on its second boot. Pinned so that fix is
// not attempted without noticing.
func TestCAServiceAdapter_IssueCertificate_IsRepeatableForOneCommonName(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Reissue Root CA")
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	issue := func() *xkmstransport.IssueCertificateResponse {
		t.Helper()
		resp, issueErr := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
			CommonName:   "stable-agent.example.test",
			ValidityDays: 30,
		})
		if issueErr != nil {
			t.Fatalf("IssueCertificate: %v", issueErr)
		}
		return resp
	}

	first := issue()
	second := issue()

	firstLeaf := parseIssuedLeaf(t, first.CertificatePEM)
	secondLeaf := parseIssuedLeaf(t, second.CertificatePEM)
	if _, err := ca.Verify(secondLeaf); err != nil {
		t.Fatalf("re-issued leaf does not verify against the CA chain: %v", err)
	}
	if firstLeaf.SerialNumber.Cmp(secondLeaf.SerialNumber) == 0 {
		t.Fatal("re-issuance reused the serial number")
	}
	assertKeyMatchesCertificate(t, second.PrivateKeyPEM, secondLeaf)
}

// =============================================================================
// helpers
// =============================================================================

// parseIssuedLeaf decodes the leaf certificate out of an issuance response.
func parseIssuedLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("issued certificate PEM has no block")
	}
	if block.Type != "CERTIFICATE" {
		t.Fatalf("issued PEM block type = %q, want CERTIFICATE", block.Type)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	return leaf
}

// assertKeyMatchesCertificate proves the returned private key is the leaf's. It
// decodes the key through the go-xkms decoder that is the counterpart of the
// encoder the adapter used, then compares public halves.
func assertKeyMatchesCertificate(t *testing.T, keyPEM []byte, leaf *x509.Certificate) {
	t.Helper()
	if len(keyPEM) == 0 {
		t.Fatal("no private key returned: the `ca issue` contract is to hand the requester both halves")
	}
	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	if err != nil {
		t.Fatalf("decode returned private key: %v", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		t.Fatalf("returned key %T is not a crypto.Signer", key)
	}
	got, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		t.Fatalf("marshal returned public key: %v", err)
	}
	want, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		t.Fatalf("marshal certificate public key: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("the returned private key does not match the issued certificate")
	}
}
