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
	"crypto/ecdsa"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trustPIVAttestationCA points the handler's PIV attestation verifier at ca.
//
// It must be called BEFORE the handler serves its first PIV login: the verifier
// is built once per server (sync.Once), because the trust anchors cannot change
// without a restart.
func trustPIVAttestationCA(t *testing.T, handler *IAMHandler, ca *x509.Certificate) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "piv-attestation-ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	handler.server.config.PIVClientCert.AttestationCAFile = path
}

// oidYubicoPIVFirmwareVersion is 1.3.6.1.4.1.41482.3.3, the firmware-version
// extension a YubiKey stamps on every PIV slot attestation certificate. It
// mirrors the profile marker pkg/iam recognises; the OID is a vendor-defined
// external constant, so the fixture spells it out rather than reaching for an
// unexported value in another package.
var oidYubicoPIVFirmwareVersion = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 41482, 3, 3}

// pivSlotAttestationFor mints the certificate a card produces for the slot key
// behind leaf: the SAME public key, carrying the PIV attestation profile, and
// issued by the VENDOR attestation CA.
//
// It is a separate artifact from the login credential, which is the point. The
// login certificate cannot be its own attestation — an operator whose
// attestation anchor also issues client certificates would otherwise have every
// software key report hardware.
func pivSlotAttestationFor(
	t *testing.T,
	leaf *x509.Certificate,
	vendorCA *x509.Certificate,
	vendorKey *ecdsa.PrivateKey,
) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(4242),
		Subject:      pkix.Name{CommonName: "YubiKey PIV Attestation 9a"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{{
			Id:    oidYubicoPIVFirmwareVersion,
			Value: []byte{0x05, 0x04, 0x03},
		}},
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, tmpl, vendorCA, leaf.PublicKey, vendorKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// pivRequestWithChain builds a PIV login request whose TLS peer chain is the
// login certificate followed by the material the card supplied alongside it.
func pivRequestWithChain(certs ...*x509.Certificate) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: certs}
	return req
}

// allowUnattestedPIV opts the handler into accepting a certificate whose
// hardware provenance was not proven.
func allowUnattestedPIV(handler *IAMHandler) {
	handler.server.config.PIVClientCert.AllowUnattested = true
}

// unverifiedJWTClaims decodes a JWT payload WITHOUT verifying its signature.
// These tests assert what the handler put in the token, and the signature is
// pkg/iam's contract, covered by its own tests. Nothing outside a test may
// read a token this way.
func unverifiedJWTClaims(t *testing.T, token string) map[string]interface{} {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "not a JWT: %q", token)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// TestPIVLogin_RefusesWhenNoAttestationCAConfigured is the "unconfigured
// deployment refuses rather than silently accepting anything" criterion at the
// transport boundary.
//
// The certificate here is otherwise perfect: it chains to the configured PIV
// CA, its SPIFFE SAN resolves the tenant, and its thumbprint matches a
// registered binding. Before this control the endpoint answered 200 with a
// token claiming acr=hardware-attested and attest_level=hardware — for a
// deployment that had configured nothing capable of establishing either.
func TestPIVLogin_RefusesWhenNoAttestationCAConfigured(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	// Deliberately no trustPIVAttestationCA call.
	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusUnauthorized, w.Code, "body: %s", w.Body.String())
	// The setting the operator has to fill in is named in the AUDIT record,
	// not in the response. This assertion used to read the body: the endpoint
	// is reachable by anyone who can complete a handshake, so naming a
	// configuration setting there told an unauthenticated caller how the
	// deployment is set up. The operator-facing requirement is unchanged, only
	// the channel that carries it.
	assert.Contains(t, auditDetail(audit), "attestation_ca_file",
		"the audit record must name the setting the operator has to fill in")
	assert.NotContains(t, w.Body.String(), "attestation_ca_file",
		"the response must not name server configuration")
}

// TestPIVLogin_RefusesSelfSignedCertificate is the negative verification
// criterion at the transport boundary: a self-signed certificate is not
// evidence of hardware, however well-formed it is.
func TestPIVLogin_RefusesSelfSignedCertificate(t *testing.T) {
	ca, _ := pivLoginTestCA(t, "PIV Root CA")

	// The credential the attacker presents is its own issuer. It still carries
	// a valid SPIFFE SAN for the tenant, so nothing else about the request is
	// wrong — only the provenance of the key.
	selfSigned, selfKey := pivLoginTestCA(t, "Impostor Root")
	leaf := pivSpiffeLeaf(t, selfSigned, selfKey, "tenant-piv")

	// The impostor even supplies an attestation certificate of the right
	// shape, over the right key — signed by their own root. Only the anchor
	// tells the two apart.
	forged := pivSlotAttestationFor(t, leaf, selfSigned, selfKey)

	handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()
	// The deployment trusts the REAL attestation CA, not the impostor's.
	trustPIVAttestationCA(t, handler, ca)

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithChain(leaf, forged))

	require.Equal(t, http.StatusUnauthorized, w.Code, "body: %s", w.Body.String())
	// The refusal REASON is recorded server-side; the wire says only that
	// attestation failed. See the disclosure tests for why.
	assert.Contains(t, auditDetail(audit), "trusted PIV attestation CA")
	assert.Equal(t, ErrPIVCertIssuerRejected, pivErrorCode(t, w.Body.String()),
		"the error code must survive so a client can still branch on it")
}

// TestPIVLogin_AllowUnattestedIssuesASoftwareToken proves the explicit opt-in
// is a HONEST downgrade rather than a silent one: the login succeeds, but the
// token records attest_level=software and does NOT carry the hardware-attested
// ACR, so a policy requiring hardware still refuses it.
func TestPIVLogin_AllowUnattestedIssuesASoftwareToken(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()
	allowUnattestedPIV(handler)

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp["id_token"])

	claims := unverifiedJWTClaims(t, resp["id_token"].(string))
	assert.Equal(t, "software", claims["attest_level"],
		"an unattested login must record what it actually proved")
	assert.NotEqual(t, "urn:quicbaas:auth:hardware-attested", claims["acr"],
		"an unattested login must not carry the hardware-attested ACR")

	// The opt-out admits an ordinary software-held TLS key: one factor, and no
	// card. A token claiming otherwise lets a relying party authorize this —
	// the weakest login this endpoint issues — under an MFA policy.
	assert.Equal(t, false, claims["mfa_verified"],
		"an unattested login must not be marked MFA-verified")
	assert.Equal(t, []interface{}{"mtls"}, claims["amr"],
		"an unattested login proved mutual TLS, not a smartcard")

	// The same answer has to reach the bearer that authorizes, and survive the
	// renewal that rebuilds it from the session frozen at login.
	access := unverifiedJWTClaims(t, resp["access_token"].(string))
	assert.Equal(t, false, access["mfa_verified"],
		"the bearer of an unattested login must not be marked MFA-verified")
}

// TestPIVLogin_AttestedCertificateIssuesAHardwareToken is the positive
// criterion: a certificate chaining to the trusted attestation CA is accepted
// and its token says hardware.
func TestPIVLogin_AttestedCertificateIssuesAHardwareToken(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	vendorCA, vendorKey := pivLoginTestCA(t, "Yubico PIV Attestation Root")
	attestation := pivSlotAttestationFor(t, leaf, vendorCA, vendorKey)
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()
	trustPIVAttestationCA(t, handler, vendorCA)

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithChain(leaf, attestation))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	claims := unverifiedJWTClaims(t, resp["id_token"].(string))
	assert.Equal(t, "hardware", claims["attest_level"])
	assert.Equal(t, "urn:quicbaas:auth:hardware-attested", claims["acr"])

	// The positive control for the assurance claims: a verified card is the
	// second factor, and the amr names the credential that proved it.
	assert.Equal(t, true, claims["mfa_verified"])
	assert.Equal(t, []interface{}{"smartcard", "mfa"}, claims["amr"])
}

// TestPIVLoginAudit_RecordsTheProvenLevel proves the audit trail can tell a
// verified smartcard login from one an operator opted into accepting
// unattested. The record used to carry only attest_method ("smartcard"), which
// names the credential kind and says nothing about what was established.
func TestPIVLoginAudit_RecordsTheProvenLevel(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	vendorCA, vendorKey := pivLoginTestCA(t, "Yubico PIV Attestation Root")
	attestation := pivSlotAttestationFor(t, leaf, vendorCA, vendorKey)

	t.Run("attested", func(t *testing.T) {
		handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
		defer cleanup()
		trustPIVAttestationCA(t, handler, vendorCA)

		w := httptest.NewRecorder()
		handler.PIVLogin(w, pivRequestWithChain(leaf, attestation))
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		require.NotEmpty(t, audit.events)
		last := audit.events[len(audit.events)-1]
		require.Equal(t, "auth.login.piv.success", last.Action)
		assert.Equal(t, "hardware", last.Details["attest_level"])
	})

	t.Run("explicitly unattested", func(t *testing.T) {
		handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
		defer cleanup()
		allowUnattestedPIV(handler)

		w := httptest.NewRecorder()
		handler.PIVLogin(w, pivRequestWithSpiffe(leaf))
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		require.NotEmpty(t, audit.events)
		last := audit.events[len(audit.events)-1]
		require.Equal(t, "auth.login.piv.success", last.Action)
		assert.Equal(t, "software", last.Details["attest_level"],
			"an unattested login must be distinguishable in the audit trail")
	})
}

// TestPIVLogin_AllowUnattestedDoesNotMaskAConfigError is the difference between
// "we decided not to verify" and "we meant to and got it wrong".
//
// allow_unattested covers the first. It must NOT cover the second: an
// attestation_ca_file that is named but unreadable would otherwise turn a typo
// into a fleet-wide loss of hardware assurance, silently — exactly the class of
// downgrade this work removes.
func TestPIVLogin_AllowUnattestedDoesNotMaskAConfigError(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	allowUnattestedPIV(handler)
	// A path the operator DID configure, which cannot be read.
	handler.server.config.PIVClientCert.AttestationCAFile =
		filepath.Join(t.TempDir(), "does-not-exist.pem")

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusUnauthorized, w.Code,
		"a broken attestation_ca_file was silently downgraded: %s", w.Body.String())
}
