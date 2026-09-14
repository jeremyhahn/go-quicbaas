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
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// What may become a trust anchor.
//
// parseCertPool is the single place every attestation trust bundle is built —
// the WebAuthn roots, the PIV attestation CA, and the PIV intermediates. It
// used to add whatever CERTIFICATE block it found, which let an END-ENTITY
// certificate become a root. That is a fail-open in the path this work exists
// to make fail-closed: point iam.piv_login.attestation_ca_file at a card's own
// slot-attestation certificate and the verification builds a one-element path
// from that certificate to itself, reporting hardware with no manufacturer CA
// anywhere in the deployment.
// =============================================================================

// TestParseCertPool_RefusesAnEndEntityCertificateAsAnAnchor is the criterion:
// the offending bundle is refused at LOAD time, with the subject named, rather
// than silently producing a pool that trusts a leaf.
func TestParseCertPool_RefusesAnEndEntityCertificateAsAnAnchor(t *testing.T) {
	pivChain := newPIVTestChain(t)
	webauthnPKI := newAttestationTestPKI(t, yubiKey5AAGUID)

	for _, tc := range []struct {
		name string
		load func() error
	}{
		{
			// The scenario in operator terms: attestation_ca_file points at the
			// card's own attestation certificate instead of the vendor CA.
			name: "PIV attestation CA bundle holding a slot attestation certificate",
			load: func() error {
				_, err := NewPIVAttestationVerifier(certPEM(pivChain.attestation), nil)
				return err
			},
		},
		{
			name: "PIV attestation CA bundle holding a login certificate",
			load: func() error {
				_, err := NewPIVAttestationVerifier(certPEM(pivChain.leaf), nil)
				return err
			},
		},
		{
			// The intermediates pool is built by the same function and carries
			// the same exposure: an intermediate that is not a CA cannot
			// legitimately complete a path.
			name: "PIV intermediate bundle holding an end-entity certificate",
			load: func() error {
				_, err := NewPIVAttestationVerifier(pivChain.rootPEM, certPEM(pivChain.leaf))
				return err
			},
		},
		{
			name: "WebAuthn attestation roots holding a batch attestation certificate",
			load: func() error {
				_, err := NewWebAuthnAttestationRoots(certPEM(webauthnPKI.batchCert))
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.load()
			if err == nil {
				t.Fatal("an end-entity certificate was accepted as an attestation trust anchor")
			}
			var notCA *AttestationAnchorNotCAError
			if !errors.As(err, &notCA) {
				t.Fatalf("want *AttestationAnchorNotCAError, got %T: %v", err, err)
			}
			if notCA.Subject == "" {
				t.Error("the refusal does not name the offending certificate")
			}
		})
	}
}

// TestParseCertPool_StillAcceptsGenuineCAAnchors is the positive half, and it
// is the half that matters most: a check that refused everything would satisfy
// the test above while breaking every real deployment. Both trust paths must
// still LOAD and still VERIFY a genuine chain.
func TestParseCertPool_StillAcceptsGenuineCAAnchors(t *testing.T) {
	t.Run("PIV verifies a real slot attestation", func(t *testing.T) {
		chain := newPIVTestChain(t)
		verifier, err := NewPIVAttestationVerifier(chain.rootPEM, certPEM(chain.intermediate))
		if err != nil {
			t.Fatalf("a genuine CA bundle was refused: %v", err)
		}
		if !verifier.Configured() {
			t.Fatal("Configured() = false after loading a genuine CA bundle")
		}
		result, err := verifier.Verify(chain.leaf, []*x509.Certificate{chain.attestation, chain.intermediate})
		if err != nil {
			t.Fatalf("a genuine attestation chain was refused: %v", err)
		}
		if !result.IsHardware() {
			t.Errorf("proven level = %q, want hardware", result.Level())
		}
	})

	t.Run("WebAuthn verifies a real manufacturer chain", func(t *testing.T) {
		pki := newAttestationTestPKI(t, yubiKey5AAGUID)
		roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
		if err != nil {
			t.Fatalf("a genuine CA bundle was refused: %v", err)
		}
		verified, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID, pki.batchCert, pki.intermediate))
		if err != nil {
			t.Fatalf("a genuine manufacturer chain was refused: %v", err)
		}
		if !verified.Verified() {
			t.Fatal("Verified() = false for a chain that built a path to the root")
		}
	})
}

// TestAttestationTrustBundle_NamesTheOffendingFile covers the operator's side
// of the failure. A bundle that cannot serve as anchors is a mistyped path far
// more often than it is a compromise, and an error that does not say WHICH file
// sends the operator hunting a verification failure reported nowhere.
func TestAttestationTrustBundle_NamesTheOffendingFile(t *testing.T) {
	chain := newPIVTestChain(t)
	dir := t.TempDir()

	pivPath := filepath.Join(dir, "wrong-piv-bundle.pem")
	if err := os.WriteFile(pivPath, certPEM(chain.attestation), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	_, err := NewPIVAttestationVerifierFromFiles(pivPath, "")
	if err == nil {
		t.Fatal("a non-CA PIV bundle loaded from file was accepted")
	}
	if !strings.Contains(err.Error(), pivPath) {
		t.Errorf("the refusal does not name the file the operator configured: %v", err)
	}
	var notCA *AttestationAnchorNotCAError
	if !errors.As(err, &notCA) {
		t.Errorf("the specific reason was lost in wrapping: %T: %v", err, err)
	}

	webauthnPath := filepath.Join(dir, "wrong-webauthn-bundle.pem")
	if err := os.WriteFile(webauthnPath, certPEM(chain.leaf), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	_, err = NewWebAuthnAttestationRootsFromFile(webauthnPath)
	if err == nil {
		t.Fatal("a non-CA WebAuthn root bundle loaded from file was accepted")
	}
	if !strings.Contains(err.Error(), webauthnPath) {
		t.Errorf("the refusal does not name the file the operator configured: %v", err)
	}
	if !errors.As(err, &notCA) {
		t.Errorf("the specific reason was lost in wrapping: %T: %v", err, err)
	}
}
