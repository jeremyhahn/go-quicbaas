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

package xkms_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof the device-trust aliases are the go-xkms types, not
// lookalikes: a value of the alias must be assignable to the upstream type.
var (
	_ xkmssdk.PeerDevIDPolicy       = xkms.PeerDevIDPolicy{}
	_ xkmssdk.PeerTLSEvidence       = xkms.PeerTLSEvidence{}
	_ *xkmssdk.PeerDevIDError       = (*xkms.PeerDevIDError)(nil)
	_ *xkmssdk.PeerDevIDPolicyError = (*xkms.PeerDevIDPolicyError)(nil)
)

// TestPeerDevID_FunctionsHaveUpstreamSignatures pins each re-exported
// function to the signature go-xkms publishes, so a drift upstream fails
// here rather than in a consumer.
func TestPeerDevID_FunctionsHaveUpstreamSignatures(t *testing.T) {
	var (
		verifyIDevID func(context.Context, *x509.Certificate, xkmssdk.PeerDevIDPolicy) error           = xkms.PeerVerifyIDevID
		verifyIAK    func(context.Context, *x509.Certificate, xkmssdk.PeerDevIDPolicy) error           = xkms.PeerVerifyIAK
		isIDevID     func(*x509.Certificate) bool                                                      = xkms.PeerIsIDevID
		isIAK        func(*x509.Certificate) bool                                                      = xkms.PeerIsIAK
		evidence     func(*tls.ConnectionState, xkmssdk.PeerEvidence) (xkmssdk.PeerTLSEvidence, error) = xkms.PeerEvidenceFromTLS
		qualifying   func(*tls.ConnectionState, []byte) ([]byte, error)                                = xkms.PeerQualifyingDataFromTLS
	)
	if verifyIDevID == nil || verifyIAK == nil || isIDevID == nil || isIAK == nil || evidence == nil || qualifying == nil {
		t.Fatal("a re-exported device-trust function is nil")
	}
}

// selfSignedCert returns a throwaway self-signed ECDSA P-256 certificate with
// no TCG extensions: a plain leaf, not a device credential.
func selfSignedCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// TestPeerDevID_PlainCertIsNotADeviceCredential proves the predicates are the
// real ones: a certificate without the TCG otherNames and capability
// extensions has neither the IDevID nor the IAK shape.
func TestPeerDevID_PlainCertIsNotADeviceCredential(t *testing.T) {
	cert := selfSignedCert(t)
	if xkms.PeerIsIDevID(cert) {
		t.Fatal("PeerIsIDevID accepted a plain self-signed certificate")
	}
	if xkms.PeerIsIAK(cert) {
		t.Fatal("PeerIsIAK accepted a plain self-signed certificate")
	}
	if xkms.PeerIsIDevID(cert) != xkmssdk.PeerIsIDevID(cert) || xkms.PeerIsIAK(cert) != xkmssdk.PeerIsIAK(cert) {
		t.Fatal("device-credential predicates diverged from upstream")
	}
}

// TestPeerDevID_EmptyPolicyIsAPolicyError proves a policy with no anchors is
// reported as a misconfigured verifier, not as a rejected device.
func TestPeerDevID_EmptyPolicyIsAPolicyError(t *testing.T) {
	cert := selfSignedCert(t)
	for name, verify := range map[string]func(context.Context, *x509.Certificate, xkms.PeerDevIDPolicy) error{
		"PeerVerifyIDevID": xkms.PeerVerifyIDevID,
		"PeerVerifyIAK":    xkms.PeerVerifyIAK,
	} {
		err := verify(context.Background(), cert, xkms.PeerDevIDPolicy{})
		var policyErr *xkms.PeerDevIDPolicyError
		if !errors.As(err, &policyErr) {
			t.Fatalf("%s with an empty policy: got %T (%v), want *PeerDevIDPolicyError", name, err, err)
		}
		var devErr *xkms.PeerDevIDError
		if errors.As(err, &devErr) {
			t.Fatalf("%s with an empty policy reported a device rejection: %v", name, err)
		}
	}
}

// TestPeerDevID_FromTLSFailsClosedWithoutExporter proves both TLS helpers
// refuse a connection that cannot export keying material instead of falling
// back to an unbound nonce.
func TestPeerDevID_FromTLSFailsClosedWithoutExporter(t *testing.T) {
	var bindErr *xkms.PeerChannelBindingError

	data, err := xkms.PeerQualifyingDataFromTLS(&tls.ConnectionState{}, []byte("nonce"))
	if !errors.As(err, &bindErr) {
		t.Fatalf("PeerQualifyingDataFromTLS: got %T (%v), want *PeerChannelBindingError", err, err)
	}
	if data != nil {
		t.Fatalf("PeerQualifyingDataFromTLS returned qualifying data on refusal: %x", data)
	}

	bindErr = nil
	if _, err := xkms.PeerEvidenceFromTLS(&tls.ConnectionState{}, xkms.PeerEvidence{}); !errors.As(err, &bindErr) {
		t.Fatalf("PeerEvidenceFromTLS: got %T (%v), want *PeerChannelBindingError", err, err)
	}
}
