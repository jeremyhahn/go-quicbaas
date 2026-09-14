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
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pivTestChain is a synthetic stand-in for a vendor PIV attestation hierarchy:
// a root ("attestation CA"), a device intermediate (the card's slot-F9
// certificate) and a leaf (the slot attestation certificate).
type pivTestChain struct {
	root         *x509.Certificate
	intermediate *x509.Certificate
	leaf         *x509.Certificate

	// attestation is the slot attestation certificate the card produces for
	// the leaf's key: same SubjectPublicKeyInfo, issued by the device
	// intermediate, carrying the PIV attestation profile. It is a separate
	// artifact from the credential, which is what makes it evidence.
	attestation *x509.Certificate

	rootPEM []byte
}

// newPIVTestChain mints root -> intermediate -> leaf with real signatures, so
// the verifier is exercised against genuine path building rather than a stub.
func newPIVTestChain(t *testing.T) *pivTestChain {
	t.Helper()

	rootKey, rootCert := mintCA(t, "Test PIV Attestation Root", nil, nil)
	interKey, interCert := mintCA(t, "Test Device Attestation F9", rootCert, rootKey)
	leafKey, leafCert := mintLeaf(t, "PIV Slot 9a", interCert, interKey)
	attestation := mintPIVAttestationCert(t, "PIV Slot 9a Attestation",
		&leafKey.PublicKey, interCert, interKey)

	return &pivTestChain{
		root:         rootCert,
		intermediate: interCert,
		leaf:         leafCert,
		attestation:  attestation,
		rootPEM:      certPEM(rootCert),
	}
}

// mintCA issues a CA certificate, self-signed when parent is nil.
func mintCA(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent, parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatalf("create CA %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA %s: %v", cn, err)
	}
	return key, cert
}

// mintLeaf issues an end-entity certificate under parent.
func mintLeaf(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent, parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatalf("create leaf %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf %s: %v", cn, err)
	}
	return key, cert
}

// newSelfSignedLeaf mints the credential an attacker presents: a well-formed
// certificate that vouches only for itself.
func newSelfSignedLeaf(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	_, cert := mintLeaf(t, cn, nil, nil)
	return cert
}

func certPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// -----------------------------------------------------------------------------
// The verification criteria from #381
// -----------------------------------------------------------------------------

// TestPIVAttestationVerifier_AcceptsChainToTrustedCA is the positive
// verification criterion: a slot certificate chaining to a trusted attestation
// CA is accepted and reports hardware.
func TestPIVAttestationVerifier_AcceptsChainToTrustedCA(t *testing.T) {
	chain := newPIVTestChain(t)

	verifier, err := NewPIVAttestationVerifier(chain.rootPEM, nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(chain.leaf, []*x509.Certificate{chain.attestation, chain.intermediate})
	if err != nil {
		t.Fatalf("a slot attestation chaining to the trusted attestation CA was refused: %v", err)
	}
	if !result.IsHardware() {
		t.Fatalf("Level = %q, want hardware", result.Level())
	}
	if result.Method() != AttestMethodPIV {
		t.Errorf("Method = %q, want %q", result.Method(), AttestMethodPIV)
	}
	// PIV credentials have no AAGUID; the field must stay empty rather than
	// carrying a placeholder a policy could match on.
	if result.AAGUID() != "" {
		t.Errorf("AAGUID = %q, want empty for PIV", result.AAGUID())
	}
}

// TestPIVAttestationVerifier_RefusesSelfSigned is the negative verification
// criterion, and the one that matters most: a self-signed slot certificate is
// not evidence of hardware, and the PIV login handler used to accept exactly
// this and mint attest_level=hardware for it.
func TestPIVAttestationVerifier_RefusesSelfSigned(t *testing.T) {
	chain := newPIVTestChain(t)
	verifier, err := NewPIVAttestationVerifier(chain.rootPEM, nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(newSelfSignedLeaf(t, "Impostor"), nil)
	if err == nil {
		t.Fatal("a self-signed certificate was accepted as evidence of hardware")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}

	var unverified *PIVAttestationUnverifiedError
	if !errors.As(err, &unverified) {
		t.Fatalf("want *PIVAttestationUnverifiedError, got %T: %v", err, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "self-signed") {
		t.Errorf("refusal does not name self-signing as the problem: %q", msg)
	}
}

// TestPIVAttestationVerifier_RefusesForeignChain covers the certificate that is
// properly issued — just not by anything this deployment trusts.
func TestPIVAttestationVerifier_RefusesForeignChain(t *testing.T) {
	ours := newPIVTestChain(t)
	theirs := newPIVTestChain(t)

	verifier, err := NewPIVAttestationVerifier(ours.rootPEM, nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	_, err = verifier.Verify(theirs.leaf, []*x509.Certificate{theirs.attestation, theirs.intermediate})
	if err == nil {
		t.Fatal("a certificate from an untrusted attestation CA was accepted")
	}
	var chainErr *PIVAttestationChainError
	if !errors.As(err, &chainErr) {
		t.Fatalf("want *PIVAttestationChainError, got %T: %v", err, err)
	}
	if chainErr.Subject == "" || chainErr.Issuer == "" {
		t.Errorf("the refusal must name the certificate it rejected: %+v", chainErr)
	}
}

// TestPIVAttestationVerifier_UnconfiguredDenies is the "unconfigured deployment
// refuses rather than silently accepting anything" criterion, on the PIV side.
func TestPIVAttestationVerifier_UnconfiguredDenies(t *testing.T) {
	chain := newPIVTestChain(t)

	var verifier *PIVAttestationVerifier
	if verifier.Configured() {
		t.Fatal("a nil verifier reported itself configured")
	}
	if verifier.RootCount() != 0 {
		t.Errorf("nil RootCount = %d, want 0", verifier.RootCount())
	}

	result, err := verifier.Verify(chain.leaf, []*x509.Certificate{chain.intermediate})
	if err == nil {
		t.Fatal("an unconfigured verifier accepted a certificate")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
	if msg := err.Error(); !strings.Contains(msg, "iam.piv_login.attestation_ca_file") {
		t.Errorf("refusal does not name the missing setting: %q", msg)
	}
}

// TestPIVAttestationVerifier_NilCertificate covers the empty-input error path.
func TestPIVAttestationVerifier_NilCertificate(t *testing.T) {
	chain := newPIVTestChain(t)
	verifier, err := NewPIVAttestationVerifier(chain.rootPEM, nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}
	if _, err := verifier.Verify(nil, nil); err == nil {
		t.Fatal("Verify(nil) succeeded")
	}
}

// TestPIVAttestationVerifier_DeviceIntermediatesAreNotAnchors is the trust
// boundary: material the card supplies can complete a path to an operator's
// root, but supplying its OWN root must not make the chain trusted.
func TestPIVAttestationVerifier_DeviceIntermediatesAreNotAnchors(t *testing.T) {
	ours := newPIVTestChain(t)
	theirs := newPIVTestChain(t)

	verifier, err := NewPIVAttestationVerifier(ours.rootPEM, nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	// The attacker presents their whole hierarchy, root included, as chain
	// material. It must not be promoted to a trust anchor.
	_, err = verifier.Verify(theirs.leaf,
		[]*x509.Certificate{theirs.attestation, theirs.intermediate, theirs.root})
	if err == nil {
		t.Fatal("a caller-supplied root was treated as a trust anchor")
	}
}

// -----------------------------------------------------------------------------
// Construction
// -----------------------------------------------------------------------------

// TestNewPIVAttestationVerifier_WithIntermediates checks a configured
// intermediate bundle completes a chain without the card supplying one.
func TestNewPIVAttestationVerifier_WithIntermediates(t *testing.T) {
	chain := newPIVTestChain(t)

	verifier, err := NewPIVAttestationVerifier(chain.rootPEM, certPEM(chain.intermediate))
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}
	if _, err := verifier.Verify(chain.leaf, []*x509.Certificate{chain.attestation}); err != nil {
		t.Fatalf("a configured intermediate did not complete the chain: %v", err)
	}
}

// TestNewPIVAttestationVerifier_RejectsEmptyBundle refuses an anchor set that
// silently ended up empty — the state that would verify nothing while looking
// configured.
func TestNewPIVAttestationVerifier_RejectsEmptyBundle(t *testing.T) {
	for _, tc := range []struct {
		name string
		pem  []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"no certificate blocks", []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")},
		{"not pem at all", []byte("hello")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPIVAttestationVerifier(tc.pem, nil); err == nil {
				t.Fatal("an empty attestation CA bundle was accepted")
			}
		})
	}
}

// TestNewPIVAttestationVerifier_RejectsMalformedCertificate proves a corrupt
// CERTIFICATE block is an error rather than a quietly smaller trust set.
func TestNewPIVAttestationVerifier_RejectsMalformedCertificate(t *testing.T) {
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")})

	_, err := NewPIVAttestationVerifier(bad, nil)
	if err == nil {
		t.Fatal("a malformed CERTIFICATE block was accepted")
	}
	var cfgErr *PIVAttestationConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *PIVAttestationConfigError, got %T: %v", err, err)
	}
	if cfgErr.Unwrap() == nil {
		t.Error("the config error does not carry the parse failure")
	}
}

// TestNewPIVAttestationVerifierFromFiles covers the operator-facing loader,
// including both missing-file paths and the no-path-configured refusal.
func TestNewPIVAttestationVerifierFromFiles(t *testing.T) {
	chain := newPIVTestChain(t)
	dir := t.TempDir()

	rootPath := filepath.Join(dir, "attestation-ca.pem")
	if err := os.WriteFile(rootPath, chain.rootPEM, 0o600); err != nil {
		t.Fatalf("write root: %v", err)
	}
	interPath := filepath.Join(dir, "intermediates.pem")
	if err := os.WriteFile(interPath, certPEM(chain.intermediate), 0o600); err != nil {
		t.Fatalf("write intermediates: %v", err)
	}

	verifier, err := NewPIVAttestationVerifierFromFiles(rootPath, interPath)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifierFromFiles: %v", err)
	}
	if !verifier.Configured() || verifier.RootCount() != 1 {
		t.Fatalf("Configured=%v RootCount=%d, want true/1", verifier.Configured(), verifier.RootCount())
	}
	if _, err := verifier.Verify(chain.leaf, []*x509.Certificate{chain.attestation}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// Error paths.
	if _, err := NewPIVAttestationVerifierFromFiles("", ""); err == nil {
		t.Error("an empty attestation CA path was accepted")
	}
	if _, err := NewPIVAttestationVerifierFromFiles(filepath.Join(dir, "absent.pem"), ""); err == nil {
		t.Error("a missing attestation CA file was accepted")
	}
	if _, err := NewPIVAttestationVerifierFromFiles(rootPath, filepath.Join(dir, "absent.pem")); err == nil {
		t.Error("a missing intermediates file was accepted")
	}
}

// TestUnattestedPIVCredential documents the explicit opt-out: the result is
// representable, says "software", and is never mistaken for hardware.
func TestUnattestedPIVCredential(t *testing.T) {
	result := UnattestedPIVCredential()

	if result.IsHardware() {
		t.Fatal("the unattested PIV result reported hardware")
	}
	if result.Level() != AttestLevelSoftware {
		t.Errorf("Level = %q, want %q", result.Level(), AttestLevelSoftware)
	}
	if result.Method() != AttestMethodPIV {
		t.Errorf("Method = %q, want %q", result.Method(), AttestMethodPIV)
	}
	if result.Format() != "unverified" {
		t.Errorf("Format = %q, want %q", result.Format(), "unverified")
	}
}

// TestPIVAttestationErrors_NilReceivers checks every typed error renders
// without panicking on a nil receiver.
func TestPIVAttestationErrors_NilReceivers(t *testing.T) {
	var cfgErr *PIVAttestationConfigError
	var unverified *PIVAttestationUnverifiedError
	var chainErr *PIVAttestationChainError

	for name, msg := range map[string]string{
		"config":     cfgErr.Error(),
		"unverified": unverified.Error(),
		"chain":      chainErr.Error(),
	} {
		if msg == "" {
			t.Errorf("nil %s error produced an empty message", name)
		}
	}
	if cfgErr.Unwrap() != nil {
		t.Error("nil config error unwrapped to non-nil")
	}
	if chainErr.Unwrap() != nil {
		t.Error("nil chain error unwrapped to non-nil")
	}
	if (&PIVAttestationUnverifiedError{}).Error() == "" {
		t.Error("empty unverified error produced an empty message")
	}
}

// TestIsSelfSigned distinguishes a genuine self-signature from a certificate
// that merely carries a matching issuer DN — the name comparison alone would be
// forgeable.
func TestIsSelfSigned(t *testing.T) {
	chain := newPIVTestChain(t)

	if !isSelfSigned(chain.root) {
		t.Error("the self-signed root was not recognised")
	}
	if isSelfSigned(chain.leaf) {
		t.Error("a CA-issued leaf was reported as self-signed")
	}
	if isSelfSigned(nil) {
		t.Error("isSelfSigned(nil) = true")
	}
}

// TestPIVAttestationErrors_Messages checks each typed error names the
// certificate or configuration at fault, and unwraps to its cause.
func TestPIVAttestationErrors_Messages(t *testing.T) {
	cause := errors.New("underlying")

	cfgErr := &PIVAttestationConfigError{Reason: "bundle unreadable", Cause: cause}
	if !strings.Contains(cfgErr.Error(), "bundle unreadable") || !strings.Contains(cfgErr.Error(), "underlying") {
		t.Errorf("config message: %s", cfgErr.Error())
	}
	if !errors.Is(cfgErr, cause) {
		t.Error("config error does not unwrap to its cause")
	}
	if strings.Contains((&PIVAttestationConfigError{Reason: "bare"}).Error(), "underlying") {
		t.Error("a causeless config error appended a cause")
	}

	chainErr := &PIVAttestationChainError{Subject: "CN=Slot", Issuer: "CN=Impostor", Cause: cause}
	if !strings.Contains(chainErr.Error(), "CN=Slot") || !strings.Contains(chainErr.Error(), "CN=Impostor") {
		t.Errorf("chain message: %s", chainErr.Error())
	}
	if !errors.Is(chainErr, cause) {
		t.Error("chain error does not unwrap to its cause")
	}
	if strings.Contains((&PIVAttestationChainError{Subject: "a", Issuer: "b"}).Error(), "underlying") {
		t.Error("a causeless chain error appended a cause")
	}
}

// -----------------------------------------------------------------------------
// The attestation must vouch for the key that is authenticating
// -----------------------------------------------------------------------------

// mintLeafForKey issues a certificate over an EXISTING public key, so a test
// can produce two certificates that describe the same slot key — which is what
// a PIV slot attestation certificate and a CA-issued login certificate are.
func mintLeafForKey(
	t *testing.T,
	cn string,
	pub *ecdsa.PublicKey,
	parent *x509.Certificate,
	parentKey *ecdsa.PrivateKey,
) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 7),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	if err != nil {
		t.Fatalf("create cert %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert %s: %v", cn, err)
	}
	return cert
}

// TestPIVAttestationVerifier_AcceptsSeparateSlotAttestation is the real-world
// deployment shape: the login certificate is issued by the ORGANISATION's CA
// from a CSR, and the card supplies a separate slot attestation certificate —
// over the same slot key — that chains to the vendor attestation root.
//
// Verifying only the login certificate would refuse this, which is what would
// push an operator into pointing attestation_ca_file at their own login CA and
// hollowing the control out.
func TestPIVAttestationVerifier_AcceptsSeparateSlotAttestation(t *testing.T) {
	vendorKey, vendorRoot := mintCA(t, "Yubico PIV Attestation Root", nil, nil)
	orgKey, orgCA := mintCA(t, "Acme Corp Issuing CA", nil, nil)

	// One slot key, two certificates about it.
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

// TestPIVAttestationVerifier_RefusesAttestationForADifferentKey is the binding
// that makes the check mean something.
//
// The client presents a REAL, vendor-signed slot attestation — just one that
// attests a different key than the one authenticating. Without the
// SubjectPublicKeyInfo binding, anyone holding any genuine YubiKey attestation
// could use it to launder a software key into a hardware-attested login.
func TestPIVAttestationVerifier_RefusesAttestationForADifferentKey(t *testing.T) {
	vendorKey, vendorRoot := mintCA(t, "Yubico PIV Attestation Root", nil, nil)
	orgKey, orgCA := mintCA(t, "Acme Corp Issuing CA", nil, nil)

	// The key actually authenticating — software-held, as far as anyone knows.
	softwareKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate software key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Mallory (software)", &softwareKey.PublicKey, orgCA, orgKey)

	// A genuine attestation, borrowed from some other card and some other key.
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	borrowed := mintPIVAttestationCert(t, "YubiKey PIV Attestation 9a", &otherKey.PublicKey, vendorRoot, vendorKey)

	verifier, err := NewPIVAttestationVerifier(certPEM(vendorRoot), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	result, err := verifier.Verify(loginCert, []*x509.Certificate{borrowed})
	if err == nil {
		t.Fatal("an attestation for a DIFFERENT key was accepted as evidence for this one")
	}
	if result != nil {
		t.Fatalf("a refusal must return no result, got %+v", result)
	}
	if msg := err.Error(); !strings.Contains(msg, "attests a DIFFERENT key") {
		t.Errorf("the refusal does not name the missing binding: %q", msg)
	}
}

// TestPIVAttestationVerifier_RefusesUntrustedAttestationForTheRightKey closes
// the other half: the certificate is about the right key, but no trust anchor
// vouches for it.
func TestPIVAttestationVerifier_RefusesUntrustedAttestationForTheRightKey(t *testing.T) {
	_, vendorRoot := mintCA(t, "Yubico PIV Attestation Root", nil, nil)
	rogueKey, rogueRoot := mintCA(t, "Impostor Attestation Root", nil, nil)

	slotKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate slot key: %v", err)
	}
	loginCert := mintLeafForKey(t, "Mallory", &slotKey.PublicKey, rogueRoot, rogueKey)
	selfMadeAttestation := mintPIVAttestationCert(t, "Totally Real Attestation", &slotKey.PublicKey, rogueRoot, rogueKey)

	verifier, err := NewPIVAttestationVerifier(certPEM(vendorRoot), nil)
	if err != nil {
		t.Fatalf("NewPIVAttestationVerifier: %v", err)
	}

	if _, err := verifier.Verify(loginCert, []*x509.Certificate{selfMadeAttestation}); err == nil {
		t.Fatal("a self-minted attestation for the right key was accepted")
	}
}
