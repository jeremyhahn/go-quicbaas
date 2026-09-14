//go:build rest

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

package rest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// The ADMIT side of the WebAuthn registration route.
//
// Every other assertion on this branch proves the attestation gate REFUSES
// something — an unverified chain, a self-signed certificate, a rogue batch
// cert. A gate that refused EVERYTHING would satisfy all of them and be
// completely broken, and the failure mode is silent: real security keys stop
// enrolling and the logs say "attestation trust path not verified", which reads
// like the operator's trust store is wrong rather than like a bug.
//
// These tests are the other half. They drive a genuine manufacturer-chained
// attestation through the live dispatch path and assert it is ADMITTED.
// =============================================================================

// oidFIDOGenCeAAGUIDForTest is id-fido-gen-ce-aaguid, the extension a batch
// attestation certificate carries to name the authenticator model it vouches
// for. It is spelled out here rather than borrowed because the production
// constant is unexported in pkg/iam — a test that shares the constant with the
// code under test cannot catch the code using the wrong OID.
var oidFIDOGenCeAAGUIDForTest = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 45724, 1, 1, 4}

// webauthnTestAAGUID is the authenticator model these tests enroll.
var webauthnTestAAGUID = []byte{
	0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
	0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8,
}

// mintAttestationRoot issues a self-signed CA standing in for a manufacturer
// attestation root.
func mintAttestationRoot(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "Test FIDO Attestation Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	return cert, key
}

// mintBatchAttestationCert issues the per-model batch attestation certificate a
// real authenticator ships, carrying the AAGUID extension doubly OCTET STRING
// wrapped as the FIDO spec requires.
func mintBatchAttestationCert(
	t *testing.T,
	parent *x509.Certificate,
	parentKey *ecdsa.PrivateKey,
	aaguid []byte,
) *x509.Certificate {
	t.Helper()
	encoded, err := asn1.Marshal(aaguid)
	if err != nil {
		t.Fatalf("marshal aaguid extension: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:    big.NewInt(time.Now().UnixNano() + 1),
		Subject:         pkix.Name{CommonName: "Test Authenticator Batch Attestation"},
		NotBefore:       time.Now().Add(-time.Hour),
		NotAfter:        time.Now().Add(24 * time.Hour),
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{{Id: oidFIDOGenCeAAGUIDForTest, Value: encoded}},
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate batch key: %v", err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create batch cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse batch cert: %v", err)
	}
	return cert
}

// hardwareRegistrationBody produces the wire body a browser POSTs to
// /registration/finish for an authenticator whose batch attestation certificate
// chains to root: a WebAuthn PublicKeyCredential JSON whose packed attestation
// statement carries the x5c chain.
//
// The ceremony response comes from a virtual authenticator, so the structure —
// CBOR attestation object, authenticator data, COSE public key — is the real
// shape rather than a hand-rolled approximation. Only the x5c chain is injected,
// because the virtual authenticator self-attests and a manufacturer chain is
// exactly what this test needs to exercise.
func hardwareRegistrationBody(t *testing.T, chain ...*x509.Certificate) []byte {
	t.Helper()

	rp := virtualwebauthn.RelyingParty{
		Name:   "Example",
		ID:     "example.com",
		Origin: "https://example.com",
	}
	auth := virtualwebauthn.NewAuthenticator()
	copy(auth.Aaguid[:], webauthnTestAAGUID)
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	raw := virtualwebauthn.CreateAttestationResponse(rp, auth, cred, virtualwebauthn.AttestationOptions{
		Challenge:        []byte("attestation-test-challenge"),
		RelyingPartyID:   rp.ID,
		RelyingPartyName: rp.Name,
		UserID:           "user-1",
		UserName:         "admin@example.com",
		UserDisplayName:  "Admin",
	})

	// Decode far enough to reach the attestation statement, add the chain, and
	// put the credential back together exactly as it arrived.
	var credential map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		t.Fatalf("unmarshal virtual credential: %v", err)
	}
	response, ok := credential["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("virtual credential has no response object: %s", raw)
	}
	encodedAttestation, ok := response["attestationObject"].(string)
	if !ok {
		t.Fatalf("virtual credential has no attestationObject: %s", raw)
	}
	attestationCBOR, err := base64.RawURLEncoding.DecodeString(encodedAttestation)
	if err != nil {
		t.Fatalf("decode attestation object: %v", err)
	}

	// Decoded into the same shape go-webauthn uses, so the statement map is
	// keyed by string rather than by the empty interface a generic CBOR decode
	// produces.
	var attestation struct {
		AuthData []byte         `json:"authData"`
		Format   string         `json:"fmt"`
		AttStmt  map[string]any `json:"attStmt"`
	}
	if err := webauthncbor.Unmarshal(attestationCBOR, &attestation); err != nil {
		t.Fatalf("cbor-decode attestation object: %v", err)
	}
	if attestation.AttStmt == nil {
		t.Fatal("attestation object carries no attStmt")
	}
	x5c := make([]interface{}, 0, len(chain))
	for _, cert := range chain {
		x5c = append(x5c, cert.Raw)
	}
	attestation.AttStmt["x5c"] = x5c

	rebuilt, err := webauthncbor.Marshal(attestation)
	if err != nil {
		t.Fatalf("cbor-encode attestation object: %v", err)
	}
	response["attestationObject"] = base64.RawURLEncoding.EncodeToString(rebuilt)
	credential["response"] = response

	body, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	return body
}

// attestationTestFactory builds a WebAuthn factory that trusts root as a
// manufacturer attestation anchor.
func attestationTestFactory(t *testing.T, root *x509.Certificate) *iam.WebAuthnServiceFactory {
	t.Helper()
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	roots, err := iam.NewWebAuthnAttestationRoots(rootPEM)
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}
	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient(),
		AttestationRecorder: func() qrdbsdk.AttestationRecorder { return testutil.NewMemoryUserAuthFactors() },
		AttestationRoots:    roots,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	return factory
}

// registrationFinishRequest builds the request the dispatcher sees for
// /registration/finish, with the session in the header where xkms reads it.
func registrationFinishRequest(body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/iam/acme/webauthn/registration/finish", bytes.NewReader(body))
	req.Header.Set("X-Session-Id", "ceremony-session-1")
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestVerifyDispatchedAttestation_AdmitsAManufacturerChainedCredential is the
// positive control this route did not have.
//
// It drives a real hardware-shaped registration through the dispatch path and
// asserts the verified chain reaches the assurance gate — that a security key
// whose batch certificate chains to a configured manufacturer root is ENROLLED
// under the strict hardware policy, not merely "not crashed".
//
// The assertion goes through the production gate rather than reading the
// context directly: what matters is not that some value was stored, but that
// the gate this route feeds resolves the registration to hardware and admits
// it. A test that inspected the context would still pass if the gate stopped
// reading it.
func TestVerifyDispatchedAttestation_AdmitsAManufacturerChainedCredential(t *testing.T) {
	root, rootKey := mintAttestationRoot(t)
	batch := mintBatchAttestationCert(t, root, rootKey, webauthnTestAAGUID)
	factory := attestationTestFactory(t, root)
	body := hardwareRegistrationBody(t, batch)

	ctx, replayed, err := verifyDispatchedAttestation(factory, registrationFinishRequest(body), "/registration/finish")
	if err != nil {
		t.Fatalf("a well-formed registration was rejected outright: %v", err)
	}
	if !bytes.Equal(replayed, body) {
		t.Fatal("the body handed on to xkms is not the body that arrived")
	}

	// The strict policy: hardware required, this model allowlisted, and the
	// registrant treated as an administrator (nil resolver), which is the
	// harshest combination this gate applies.
	gate := iam.NewWebAuthnAttestationGate(
		iam.WebAuthnAssuranceHardware,
		iam.NewAAGUIDAllowlist([]string{hex.EncodeToString(webauthnTestAAGUID)}, iam.AAGUIDPolicyStrict),
		nil,
	)
	result, err := gate.Evaluate(ctx, "acme", "admin@example.com", "basic_full", webauthnTestAAGUID)
	if err != nil {
		t.Fatalf("a manufacturer-chained security key was refused enrollment: %v", err)
	}
	if got := result.Level(); got != iam.AttestLevelHardware {
		t.Errorf("attestation level = %q, want %q", got, iam.AttestLevelHardware)
	}
	if got, want := result.AAGUID(), hex.EncodeToString(webauthnTestAAGUID); got != want {
		t.Errorf("AAGUID = %q, want %q", got, want)
	}
}

// TestVerifyDispatchedAttestation_RequiresRawCredentialJSON pins the wire
// contract this route depends on.
//
// The SDK's REST client sends the credential JSON as the body and the ceremony
// session in the X-Session-Id header (sdk/go/transport/rest/security_handler.go
// unwraps its FinishRegistrationRequest before the call, and xkms's handler
// reads the header). The transport envelope
// {"session_id":…,"attestation_json":…} is the shape used by the gRPC / MCP /
// QUIC surfaces, which reach the attestation check through
// pkg/service/fido2_service.go instead.
//
// If a client ever posted the envelope HERE, the credential would not parse and
// no chain would be attached — a hardware key would silently enroll as
// software, or be refused under the strict policy. That is a wire-contract
// break rather than a policy decision, so it is pinned: this test fails the day
// the two shapes are confused.
func TestVerifyDispatchedAttestation_RequiresRawCredentialJSON(t *testing.T) {
	root, rootKey := mintAttestationRoot(t)
	batch := mintBatchAttestationCert(t, root, rootKey, webauthnTestAAGUID)
	factory := attestationTestFactory(t, root)

	envelope, err := json.Marshal(map[string]string{
		"session_id":       "ceremony-session-1",
		"attestation_json": string(hardwareRegistrationBody(t, batch)),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	ctx, replayed, err := verifyDispatchedAttestation(factory, registrationFinishRequest(envelope), "/registration/finish")
	if err != nil {
		t.Fatalf("an unparseable body must be left to xkms, not rejected here: %v", err)
	}
	if !bytes.Equal(replayed, envelope) {
		t.Fatal("the body handed on to xkms is not the body that arrived")
	}

	gate := iam.NewWebAuthnAttestationGate(
		iam.WebAuthnAssuranceHardware,
		iam.NewAAGUIDAllowlist([]string{hex.EncodeToString(webauthnTestAAGUID)}, iam.AAGUIDPolicyStrict),
		nil,
	)
	if _, err := gate.Evaluate(ctx, "acme", "admin@example.com", "basic_full", webauthnTestAAGUID); err == nil {
		t.Fatal("an envelope body produced a hardware enrollment; the route's wire contract has changed " +
			"and verifyDispatchedAttestation must be taught the new shape")
	}
}

// TestVerifyDispatchedAttestation_LeavesOtherRoutesUntouched confirms the
// verification is scoped to the one ceremony step that carries an attestation.
func TestVerifyDispatchedAttestation_LeavesOtherRoutesUntouched(t *testing.T) {
	root, _ := mintAttestationRoot(t)
	factory := attestationTestFactory(t, root)

	for _, path := range []string{"/registration/begin", "/login/finish", "/registration/status"} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/iam/acme/webauthn"+path, bytes.NewReader([]byte(`{"email":"admin@example.com"}`)))
		ctx, replayed, err := verifyDispatchedAttestation(factory, req, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if replayed != nil {
			t.Errorf("%s: body was consumed on a route that carries no attestation", path)
		}
		if ctx == nil {
			t.Errorf("%s: no context returned", path)
		}
	}
}
