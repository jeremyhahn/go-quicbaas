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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// attestationTestPKI is a synthetic FIDO attestation hierarchy: a manufacturer
// root and a batch attestation certificate under it, plus an unrelated root an
// attacker controls.
type attestationTestPKI struct {
	rootPEM      []byte
	batchCert    *x509.Certificate
	rogueCert    *x509.Certificate
	intermediate *x509.Certificate
}

// newAttestationTestPKI mints the hierarchy with real signatures.
func newAttestationTestPKI(t *testing.T, aaguid []byte) *attestationTestPKI {
	t.Helper()

	rootKey, rootCert := mintCA(t, "Vendor FIDO Attestation Root", nil, nil)
	interKey, interCert := mintCA(t, "Vendor FIDO Attestation CA 1", rootCert, rootKey)
	batch := mintAttestationCert(t, "Vendor Security Key Batch", interCert, interKey, aaguid)

	// The attacker's own hierarchy: structurally identical, trusted by nobody.
	rogueKey, rogueRoot := mintCA(t, "Impostor Attestation Root", nil, nil)
	rogue := mintAttestationCert(t, "Impostor Batch", rogueRoot, rogueKey, aaguid)

	return &attestationTestPKI{
		rootPEM:      certPEM(rootCert),
		batchCert:    batch,
		rogueCert:    rogue,
		intermediate: interCert,
	}
}

// mintAttestationCert issues a FIDO batch attestation certificate carrying the
// id-fido-gen-ce-aaguid extension, doubly OCTET STRING wrapped as the spec
// requires.
func mintAttestationCert(
	t *testing.T,
	cn string,
	parent *x509.Certificate,
	parentKey *ecdsa.PrivateKey,
	aaguid []byte,
) *x509.Certificate {
	t.Helper()

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if len(aaguid) > 0 {
		encoded, err := asn1.Marshal(aaguid)
		if err != nil {
			t.Fatalf("marshal aaguid extension: %v", err)
		}
		tmpl.ExtraExtensions = []pkix.Extension{{Id: oidFIDOGenCeAAGUID, Value: encoded}}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create attestation cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse attestation cert: %v", err)
	}
	return cert
}

// creationDataWithChain builds parsed credential creation data whose attestation
// statement carries the given certificate chain and authenticator AAGUID.
func creationDataWithChain(aaguid []byte, chain ...*x509.Certificate) *protocol.ParsedCredentialCreationData {
	x5c := make([]interface{}, 0, len(chain))
	for _, cert := range chain {
		x5c = append(x5c, cert.Raw)
	}
	parsed := &protocol.ParsedCredentialCreationData{}
	parsed.Response.AttestationObject.Format = "packed"
	parsed.Response.AttestationObject.AttStatement = map[string]interface{}{"x5c": x5c}
	parsed.Response.AttestationObject.AuthData.AttData.AAGUID = aaguid
	return parsed
}

// -----------------------------------------------------------------------------
// The control: a trust path, not a claimed attestation type
// -----------------------------------------------------------------------------

// TestVerifyChain_AcceptsManufacturerChain is the positive case.
func TestVerifyChain_AcceptsManufacturerChain(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	chain, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID, pki.batchCert, pki.intermediate))
	if err != nil {
		t.Fatalf("a manufacturer chain was refused: %v", err)
	}
	if !chain.Verified() {
		t.Fatal("Verified() = false for a chain that built a path to the root")
	}
	if got, want := chain.AAGUID(), hex.EncodeToString(yubiKey5AAGUID); got != want {
		t.Errorf("AAGUID = %q, want %q", got, want)
	}
}

// TestVerifyChain_RefusesSelfGeneratedAttestationCertificate is the attack this
// whole file exists for.
//
// The rogue statement is structurally perfect: a real signature, a real
// certificate, and the SAME allowlisted AAGUID in both the certificate
// extension and the authenticator data. go-webauthn would report it as
// "basic_full". Only the trust path tells it apart from a real security key.
func TestVerifyChain_RefusesSelfGeneratedAttestationCertificate(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	chain, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID, pki.rogueCert))
	if err == nil {
		t.Fatal("a self-generated attestation certificate was accepted as a manufacturer chain")
	}
	if chain != nil {
		t.Fatalf("a refusal must return no chain, got %+v", chain)
	}
	var chainErr *WebAuthnAttestationChainError
	if !errors.As(err, &chainErr) {
		t.Fatalf("want *WebAuthnAttestationChainError, got %T: %v", err, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "does not chain to a trusted WebAuthn attestation root") {
		t.Errorf("unexpected refusal: %q", msg)
	}
}

// TestVerifyChain_RefusesAAGUIDSubstitution proves the certificate's model
// binding is enforced: a genuine manufacturer certificate cannot be paired with
// authenticator data naming a different (allowlisted) model.
func TestVerifyChain_RefusesAAGUIDSubstitution(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	other := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}

	_, err = roots.VerifyChain(creationDataWithChain(other, pki.batchCert, pki.intermediate))
	if err == nil {
		t.Fatal("a certificate attesting one model was accepted for another")
	}
	if msg := err.Error(); !strings.Contains(msg, "names authenticator model") {
		t.Errorf("the refusal does not name the mismatch: %q", msg)
	}
}

// TestVerifyChain_UnconfiguredRootsDeny is the "unconfigured deployment denies"
// criterion for the WebAuthn trust store.
func TestVerifyChain_UnconfiguredRootsDeny(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)

	var roots *WebAuthnAttestationRoots
	if roots.Configured() {
		t.Fatal("a nil trust store reported itself configured")
	}
	if roots.Count() != 0 {
		t.Errorf("nil Count() = %d, want 0", roots.Count())
	}

	_, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID, pki.batchCert, pki.intermediate))
	if err == nil {
		t.Fatal("an unconfigured trust store verified a chain")
	}
	if msg := err.Error(); !strings.Contains(msg, "iam.webauthn.attestation_roots_file") {
		t.Errorf("the refusal must name the missing setting: %q", msg)
	}
}

// TestVerifyChain_RefusesStatementWithNoChain covers self attestation reaching
// the verifier: there is no certificate, so there is nothing to trust.
func TestVerifyChain_RefusesStatementWithNoChain(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	if _, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID)); err == nil {
		t.Fatal("a statement with no certificate chain was accepted")
	}
	if _, err := roots.VerifyChain(nil); err == nil {
		t.Fatal("VerifyChain(nil) succeeded")
	}
}

// TestVerifyChain_RefusesMalformedX5C covers the malformed-input paths.
func TestVerifyChain_RefusesMalformedX5C(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	for name, statement := range map[string]map[string]interface{}{
		"x5c is not an array":      {"x5c": "not-an-array"},
		"entry is not bytes":       {"x5c": []interface{}{"not-der"}},
		"entry is not certificate": {"x5c": []interface{}{[]byte("garbage")}},
	} {
		t.Run(name, func(t *testing.T) {
			parsed := &protocol.ParsedCredentialCreationData{}
			parsed.Response.AttestationObject.AttStatement = statement
			if _, err := roots.VerifyChain(parsed); err == nil {
				t.Fatal("a malformed x5c was accepted")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Construction
// -----------------------------------------------------------------------------

// TestNewWebAuthnAttestationRoots_RejectsEmptyBundle refuses a trust store that
// silently ended up empty.
func TestNewWebAuthnAttestationRoots_RejectsEmptyBundle(t *testing.T) {
	for name, bundle := range map[string][]byte{
		"nil":            nil,
		"no cert blocks": []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"),
		"not pem":        []byte("hello"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewWebAuthnAttestationRoots(bundle); err == nil {
				t.Fatal("an empty attestation root bundle was accepted")
			}
		})
	}

	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")})
	err := func() error { _, e := NewWebAuthnAttestationRoots(bad); return e }()
	if err == nil {
		t.Fatal("a malformed CERTIFICATE block was accepted")
	}
	var rootsErr *WebAuthnAttestationRootsError
	if !errors.As(err, &rootsErr) {
		t.Fatalf("want *WebAuthnAttestationRootsError, got %T", err)
	}
	if rootsErr.Unwrap() == nil {
		t.Error("the roots error does not carry the parse failure")
	}
}

// TestNewWebAuthnAttestationRootsFromFile covers the loader, including that an
// EMPTY path is not an error — it means "no trust store", which refuses at
// ceremony time rather than failing startup and taking WebAuthn down entirely.
func TestNewWebAuthnAttestationRootsFromFile(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	dir := t.TempDir()

	path := filepath.Join(dir, "attestation-roots.pem")
	if err := os.WriteFile(path, pki.rootPEM, 0o600); err != nil {
		t.Fatalf("write roots: %v", err)
	}

	roots, err := NewWebAuthnAttestationRootsFromFile(path)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRootsFromFile: %v", err)
	}
	if !roots.Configured() || roots.Count() != 1 {
		t.Fatalf("Configured=%v Count=%d, want true/1", roots.Configured(), roots.Count())
	}

	empty, err := NewWebAuthnAttestationRootsFromFile("")
	if err != nil {
		t.Fatalf("an empty path must not be an error: %v", err)
	}
	if empty != nil {
		t.Fatalf("an empty path must yield no trust store, got %+v", empty)
	}

	if _, err := NewWebAuthnAttestationRootsFromFile(filepath.Join(dir, "absent.pem")); err == nil {
		t.Fatal("a missing attestation roots file was accepted")
	}
}

// -----------------------------------------------------------------------------
// Context hand-off
// -----------------------------------------------------------------------------

// TestAttestationChainContext round-trips the verified path and, critically,
// reports "not verified" for a context that never carried one.
func TestAttestationChainContext(t *testing.T) {
	chain := &WebAuthnAttestationChain{verified: true, aaguid: "cb69481e"}

	ctx := contextWithAttestationChain(context.Background(), chain)
	got := attestationChainFromContext(ctx)
	if !got.Verified() || got.AAGUID() != "cb69481e" {
		t.Fatalf("round-trip lost the chain: %+v", got)
	}

	// A bare context carries nothing, and reading it must not panic.
	if attestationChainFromContext(context.Background()).Verified() {
		t.Error("a bare context reported a verified chain")
	}
	//nolint:staticcheck // deliberately exercising the nil-context guard
	if attestationChainFromContext(nil).Verified() {
		t.Error("a nil context reported a verified chain")
	}

	// Storing nil leaves the context untouched rather than recording a
	// not-verified marker that a later read could mistake for evidence.
	if contextWithAttestationChain(context.Background(), nil) == nil {
		t.Error("contextWithAttestationChain(nil) returned a nil context")
	}
}

// TestWebAuthnAttestationChain_NilAccessors keeps the reader safe.
func TestWebAuthnAttestationChain_NilAccessors(t *testing.T) {
	var chain *WebAuthnAttestationChain
	if chain.Verified() {
		t.Error("nil Verified() = true")
	}
	if chain.AAGUID() != "" {
		t.Error("nil AAGUID() is not empty")
	}
}

// TestAttestationChainErrors_Messages checks the operator-facing text and nil
// receivers.
func TestAttestationChainErrors_Messages(t *testing.T) {
	unverified := (&AttestationChainUnverifiedError{AAGUID: "cb69481e"}).Error()
	for _, want := range []string{"cb69481e", "generated by anyone", "attestation_roots_file"} {
		if !strings.Contains(unverified, want) {
			t.Errorf("unverified message missing %q: %s", want, unverified)
		}
	}
	var nilUnverified *AttestationChainUnverifiedError
	if nilUnverified.Error() == "" {
		t.Error("nil AttestationChainUnverifiedError produced an empty message")
	}

	mismatch := (&AttestationChainMismatchError{VerifiedAAGUID: "aaaa", CredentialAAGUID: "bbbb"}).Error()
	if !strings.Contains(mismatch, "aaaa") || !strings.Contains(mismatch, "bbbb") {
		t.Errorf("mismatch message: %s", mismatch)
	}
	var nilMismatch *AttestationChainMismatchError
	if nilMismatch.Error() == "" {
		t.Error("nil AttestationChainMismatchError produced an empty message")
	}

	var nilRoots *WebAuthnAttestationRootsError
	if nilRoots.Error() == "" {
		t.Error("nil WebAuthnAttestationRootsError produced an empty message")
	}
	if nilRoots.Unwrap() != nil {
		t.Error("nil roots error unwrapped to non-nil")
	}
	var nilChain *WebAuthnAttestationChainError
	if nilChain.Error() == "" {
		t.Error("nil WebAuthnAttestationChainError produced an empty message")
	}
	if nilChain.Unwrap() != nil {
		t.Error("nil chain error unwrapped to non-nil")
	}
	if (&WebAuthnAttestationChainError{}).Error() == "" {
		t.Error("empty chain error produced an empty message")
	}
}

// TestCertificateAAGUID covers the extension reader, including a certificate
// that carries none.
func TestCertificateAAGUID(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)

	got, ok := certificateAAGUID(pki.batchCert)
	if !ok {
		t.Fatal("the AAGUID extension was not found on a certificate that carries one")
	}
	if hex.EncodeToString(got) != hex.EncodeToString(yubiKey5AAGUID) {
		t.Errorf("AAGUID = %x, want %x", got, yubiKey5AAGUID)
	}

	noExt := newAttestationTestPKI(t, nil)
	if _, ok := certificateAAGUID(noExt.batchCert); ok {
		t.Error("an AAGUID was reported for a certificate that carries no extension")
	}
}

// TestVerifyChain_CertificateWithoutAAGUIDExtension is accepted: the extension
// is REQUIRED only when the root signs for multiple models, so its absence is
// not a defect and must not refuse a valid authenticator.
func TestVerifyChain_CertificateWithoutAAGUIDExtension(t *testing.T) {
	pki := newAttestationTestPKI(t, nil)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}

	chain, err := roots.VerifyChain(creationDataWithChain(yubiKey5AAGUID, pki.batchCert, pki.intermediate))
	if err != nil {
		t.Fatalf("a certificate without the AAGUID extension was refused: %v", err)
	}
	if got, want := chain.AAGUID(), hex.EncodeToString(yubiKey5AAGUID); got != want {
		t.Errorf("AAGUID = %q, want the authenticator-reported %q", got, want)
	}
}

// -----------------------------------------------------------------------------
// A failed verification must not leave a proof behind
// -----------------------------------------------------------------------------

// TestVerifyAttestationChain_ClearsAProofOnFailure is the injection guard.
//
// The context hand-off is what carries a verified trust path from a ceremony
// entry point to the gate, so a proof that SURVIVES a failed verification is a
// bypass: a caller able to arrange for one to be present — an embedded,
// in-process consumer, or a reused context from an earlier ceremony — could
// submit a registration whose chain does not verify and still be treated as
// hardware.
func TestVerifyAttestationChain_ClearsAProofOnFailure(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}
	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
		AttestationRoots:    roots,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}

	// A context that already carries a proof, as a hostile or stale caller
	// would arrange.
	planted := contextWithAttestationChain(context.Background(),
		&WebAuthnAttestationChain{verified: true, aaguid: hex.EncodeToString(yubiKey5AAGUID)})
	if !attestationChainFromContext(planted).Verified() {
		t.Fatal("test setup: the planted proof is not present")
	}

	// A registration whose chain does NOT verify against the configured roots.
	got, verifyErr := factory.VerifyAttestationChain(planted, creationDataWithChain(yubiKey5AAGUID, pki.rogueCert))
	if verifyErr == nil {
		t.Fatal("an untrusted chain verified")
	}
	if attestationChainFromContext(got).Verified() {
		t.Fatal("a planted proof survived a failed verification")
	}
}

// TestVerifyAttestationChain_ReplacesAProofOnSuccess confirms the success path
// records THIS verification rather than leaving an earlier one in place.
func TestVerifyAttestationChain_ReplacesAProofOnSuccess(t *testing.T) {
	pki := newAttestationTestPKI(t, yubiKey5AAGUID)
	roots, err := NewWebAuthnAttestationRoots(pki.rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}
	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
		AttestationRoots:    roots,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}

	stale := contextWithAttestationChain(context.Background(),
		&WebAuthnAttestationChain{verified: true, aaguid: "deadbeef"})

	got, err := factory.VerifyAttestationChain(stale, creationDataWithChain(yubiKey5AAGUID, pki.batchCert, pki.intermediate))
	if err != nil {
		t.Fatalf("a manufacturer chain was refused: %v", err)
	}
	if aaguid := attestationChainFromContext(got).AAGUID(); aaguid != hex.EncodeToString(yubiKey5AAGUID) {
		t.Errorf("the recorded proof is %q, want this ceremony's %q", aaguid, hex.EncodeToString(yubiKey5AAGUID))
	}
}

// TestClearAttestationChain removes a proof and is a no-op on a clean context.
func TestClearAttestationChain(t *testing.T) {
	planted := contextWithAttestationChain(context.Background(),
		&WebAuthnAttestationChain{verified: true, aaguid: "cb69481e"})
	if attestationChainFromContext(ClearAttestationChain(planted)).Verified() {
		t.Fatal("ClearAttestationChain left the proof in place")
	}

	clean := context.Background()
	if ClearAttestationChain(clean) != clean {
		t.Error("clearing a context with no proof should return it unchanged")
	}
}
