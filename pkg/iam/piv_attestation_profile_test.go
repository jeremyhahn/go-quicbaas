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

package iam

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Chaining to the configured anchor is not, by itself, evidence of hardware.
//
// The anchor is operator configuration, and the operator can point it at a CA
// that also issues ordinary client certificates. Nothing about a certificate's
// PATH distinguishes "this key was generated on a card" from "this key was
// generated with openssl and certified by the same CA".
//
// So the verifier cannot decide hardware from the path alone. It has to see the
// artifact only a device can produce: a slot attestation certificate, carrying
// the vendor's attestation profile, ABOUT the key that is authenticating.
// =============================================================================

// mintPIVAttestationCert issues a certificate carrying a PIV attestation profile
// marker — the shape `ykman piv keys attest` produces. It is the artifact the
// verifier must insist on, and the one an organisation's login CA does not emit
// in the course of issuing client certificates.
func mintPIVAttestationCert(
	t *testing.T,
	cn string,
	pub *ecdsa.PublicKey,
	parent *x509.Certificate,
	parentKey *ecdsa.PrivateKey,
) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 11),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{{
			// Firmware version — the marker a YubiKey stamps on every PIV slot
			// attestation. The value is the firmware triple; the verifier reads
			// only its presence, so the encoding here is representative.
			Id:    oidYubicoPIVFirmwareVersion,
			Value: []byte{0x05, 0x04, 0x03},
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	if err != nil {
		t.Fatalf("create attestation cert %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse attestation cert %s: %v", cn, err)
	}
	return cert
}

// TestPIVAttestationVerifier_LoginCertIsNeverItsOwnAttestation is the finding.
//
// The deployment: an operator points iam.piv_login.attestation_ca_file at the
// same CA hierarchy that issues their ordinary client-auth certificates —
// which is exactly what an operator does when they discover that verifying the
// login certificate is the only way to make PIV login report hardware.
//
// The credential: a software key, generated with no card involved, certified by
// that CA like any other client certificate.
//
// It chains to the configured anchor. It must still not be hardware, because
// nothing in it says a card ever existed.
func TestPIVAttestationVerifier_LoginCertIsNeverItsOwnAttestation(t *testing.T) {
	orgKey, orgCA := mintCA(t, "Acme Corp Issuing CA", nil, nil)

	softwareKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate software key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Mallory (software key)", &softwareKey.PublicKey, orgCA, orgKey)

	// The misconfiguration under test: the attestation anchor IS the login CA.
	verifier, err := NewPIVAttestationVerifier(certPEM(orgCA), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(loginCert, nil)
	if err == nil {
		t.Fatal("a software-held login certificate attested itself as hardware")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
	if msg := err.Error(); !strings.Contains(msg, "attestation certificate") {
		t.Errorf("the refusal does not name the missing artifact: %q", msg)
	}
}

// TestPIVAttestationVerifier_SecondCertFromTheSameCAIsNotAttestation closes the
// bypass that a "must not be the login certificate" rule would leave open.
//
// Under the same misconfiguration, the holder submits their CSR twice and gets
// two certificates for one key. The second is a different certificate, it
// chains to the anchor, and it is about the right key — it satisfies every
// structural test except the one that matters: it is an ordinary client
// certificate, and it says nothing about where the key lives.
func TestPIVAttestationVerifier_SecondCertFromTheSameCAIsNotAttestation(t *testing.T) {
	orgKey, orgCA := mintCA(t, "Acme Corp Issuing CA", nil, nil)

	softwareKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate software key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Mallory (software key)", &softwareKey.PublicKey, orgCA, orgKey)
	sameKeyAgain := mintLeafForKey(t, "Mallory (second issuance)", &softwareKey.PublicKey, orgCA, orgKey)

	verifier, err := NewPIVAttestationVerifier(certPEM(orgCA), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(loginCert, []*x509.Certificate{sameKeyAgain})
	if err == nil {
		t.Fatal("a second ordinary certificate for the same key was accepted as an attestation")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
}

// TestPIVAttestationVerifier_AcceptsAProfiledSlotAttestation is the control
// that keeps the fix from being "refuse everything".
//
// The real deployment shape: the login certificate comes from the
// organisation's CA, and the card supplies a slot attestation certificate over
// the same key, carrying the vendor attestation profile and chaining to the
// vendor root.
func TestPIVAttestationVerifier_AcceptsAProfiledSlotAttestation(t *testing.T) {
	vendorKey, vendorRoot := mintCA(t, "Yubico PIV Attestation Root", nil, nil)
	orgKey, orgCA := mintCA(t, "Acme Corp Issuing CA", nil, nil)

	slotKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate slot key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Alice (PIV 9a)", &slotKey.PublicKey, orgCA, orgKey)
	slotAttestation := mintPIVAttestationCert(t, "YubiKey PIV Attestation 9a", &slotKey.PublicKey, vendorRoot, vendorKey)

	verifier, err := NewPIVAttestationVerifier(certPEM(vendorRoot), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(loginCert, []*x509.Certificate{slotAttestation})
	if err != nil {
		t.Fatalf("a genuine slot attestation was refused: %v", err)
	}
	if !result.IsHardware() {
		t.Fatalf("Level = %q, want hardware", result.Level())
	}
}

// TestPIVAttestationVerifier_ProfiledAttestationStillNeedsATrustedAnchor keeps
// the profile marker from becoming the whole test. The marker says "this is
// shaped like an attestation"; only the chain says "a vendor stands behind it".
// Anyone can put an OID in a certificate they signed themselves.
func TestPIVAttestationVerifier_ProfiledAttestationStillNeedsATrustedAnchor(t *testing.T) {
	_, vendorRoot := mintCA(t, "Yubico PIV Attestation Root", nil, nil)
	rogueKey, rogueRoot := mintCA(t, "Impostor Attestation Root", nil, nil)

	slotKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate slot key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Mallory", &slotKey.PublicKey, rogueRoot, rogueKey)
	forged := mintPIVAttestationCert(t, "Totally Real Attestation", &slotKey.PublicKey, rogueRoot, rogueKey)

	verifier, err := NewPIVAttestationVerifier(certPEM(vendorRoot), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	if _, err := verifier.Verify(loginCert, []*x509.Certificate{forged}); err == nil {
		t.Fatal("a self-minted attestation carrying the vendor profile was accepted")
	}
}
