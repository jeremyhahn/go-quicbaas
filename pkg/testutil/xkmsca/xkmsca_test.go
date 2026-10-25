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

package xkmsca

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// New returns a CA that is up and can sign: a builder that returned something
// less would make every test that depends on it assert against a broken CA.
func TestNewReturnsAnInitializedSigningCA(t *testing.T) {
	ca := New(t, "xkmsca Test Root CA")

	if !ca.IsInitialized() {
		t.Fatal("New returned an uninitialized CA")
	}
	caCert, err := ca.CACertificate()
	if err != nil {
		t.Fatalf("CACertificate: %v", err)
	}
	if caCert.Subject.CommonName != "xkmsca Test Root CA" {
		t.Fatalf("CA CommonName = %q, want the name it was given", caCert.Subject.CommonName)
	}
	if !caCert.IsCA {
		t.Fatal("the CA certificate is not marked as a CA")
	}
	bundle, err := ca.CABundle()
	if err != nil {
		t.Fatalf("CABundle: %v", err)
	}
	if len(bundle) == 0 {
		t.Fatal("CABundle is empty")
	}

	issued, err := ca.IssueCertificate(context.Background(), &xkmssdk.TrustedCACertificateRequest{
		TenantID: "fixtures",
		Subject:  xkmssdk.TrustedCASubject{CommonName: "leaf.example.test"},
		Valid:    30,
	})
	if err != nil {
		t.Fatalf("IssueCertificate against a New CA: %v", err)
	}
	if len(issued.CertificatePEM) == 0 {
		t.Fatal("the CA issued an empty certificate")
	}
}

// Two CAs built by New are independent trust roots. A shared one would let a
// certificate from one test verify in another and hide a chain defect.
func TestNewBuildsIndependentRoots(t *testing.T) {
	first, err := New(t, "xkmsca Root A").CACertificate()
	if err != nil {
		t.Fatalf("first CACertificate: %v", err)
	}
	second, err := New(t, "xkmsca Root B").CACertificate()
	if err != nil {
		t.Fatalf("second CACertificate: %v", err)
	}
	if string(first.Raw) == string(second.Raw) {
		t.Fatal("two CAs share one root certificate")
	}
}

// NewUninitialized stands for "the CA is not up": it must refuse issuance rather
// than quietly working, because tests use it as the honest failure condition.
func TestNewUninitializedRefusesIssuance(t *testing.T) {
	ca := NewUninitialized(t, "xkmsca Cold Root CA")

	if ca.IsInitialized() {
		t.Fatal("NewUninitialized returned an initialized CA")
	}
	if _, err := ca.IssueCertificate(context.Background(), &xkmssdk.TrustedCACertificateRequest{
		TenantID: "fixtures",
		Subject:  xkmssdk.TrustedCASubject{CommonName: "leaf.example.test"},
		Valid:    30,
	}); err == nil {
		t.Fatal("an uninitialized CA issued a certificate")
	}
	if _, err := ca.SignCSR(context.Background(), []byte("not a csr"), nil); err == nil {
		t.Fatal("an uninitialized CA signed a CSR")
	}
}

// The same CA becomes usable once initialized, which is what New does. This pins
// that NewUninitialized differs from New in exactly one respect.
func TestNewUninitializedBecomesUsableAfterInit(t *testing.T) {
	ca := NewUninitialized(t, "xkmsca Warming Root CA")
	if err := ca.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !ca.IsInitialized() {
		t.Fatal("the CA is still uninitialized after Init")
	}
	if _, err := ca.IssueCertificate(context.Background(), &xkmssdk.TrustedCACertificateRequest{
		TenantID: "fixtures",
		Subject:  xkmssdk.TrustedCASubject{CommonName: "leaf.example.test"},
		Valid:    30,
	}); err != nil {
		t.Fatalf("IssueCertificate after Init: %v", err)
	}
}

// The documented reason this package exists: go-xkms NEVER returns an
// exportable private key from IssueCertificate, on any backend, so a test that
// mocks one is asserting against behaviour no deployment has.
//
// go-quicbaas#910 turned on this fact. It is pinned here rather than left as
// prose, so if go-xkms ever gains an exportable-key path the claim in
// pkg/xkms/keygen.go and the issuers built on it get revisited deliberately.
func TestIssueCertificateNeverExportsThePrivateKey(t *testing.T) {
	ca := New(t, "xkmsca Custody Root CA")

	issued, err := ca.IssueCertificate(context.Background(), &xkmssdk.TrustedCACertificateRequest{
		TenantID: "fixtures",
		Subject:  xkmssdk.TrustedCASubject{CommonName: "custody.example.test"},
		Valid:    30,
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if len(issued.PrivateKeyPEM) != 0 {
		t.Fatalf("go-xkms returned %d bytes of private key PEM; the custody rule "+
			"pkg/xkms/keygen.go documents no longer holds", len(issued.PrivateKeyPEM))
	}
	if issued.PrivateKey != nil {
		t.Fatalf("go-xkms returned a private key object (%T); the custody rule "+
			"pkg/xkms/keygen.go documents no longer holds", issued.PrivateKey)
	}
}
