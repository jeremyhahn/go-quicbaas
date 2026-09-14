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
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// The FIDO2 login proof wire format, pinned on BOTH sides in one test.
// =============================================================================
//
// Three independent pieces of code have to agree on the shape of a WebAuthn
// login proof and none of them can see the others:
//
//	producer  sdk/typescript/src/rest.ts webAuthnFinishLogin (the SPA)
//	          cmd/quicbaas canonical_auth_login.go proveCanonicalFIDO2 (the CLI)
//	decoder   ceremonyFinishLoginRequest (this package)
//	consumer  go-xkms pkg/auth_factors/webauthn.go WebAuthnFactorVerifier.Verify,
//	          reached through pkg/server tenantWebAuthnVerifier
//
// go-quicbaas#484 was diagnosed twice on the theory that they DISAGREED — once
// on the lookup-key encoding, once on the whole payload — and both readings
// were wrong. The cost of being wrong twice was two rounds of black-box
// probing against a live authenticator, because nothing in the tree stated the
// contract in one place where a disagreement would fail loudly.
//
// This test is that place. It builds the byte-exact body the TypeScript SDK
// emits (mirroring sdk/typescript/test/webauthn_login.test.ts, which pins the
// same three fields from the client side), drives it through the real handler,
// and asserts the decoded request is what the verifier reads — including
// parsing the assertion with the SAME go-webauthn call the verifier makes.
//
// The lookup-key half is pinned from the storage side too:
// sdk/go/factor_enroll_login_roundtrip_test.go
// TestFactorEnrollment_FIDO2CredentialIDIsLookupKey stores a fido2 factor and
// resolves it with []byte(credentialID) — the base64url STRING as bytes —
// which is the same value this test asserts arrives on the wire, and the same
// value pkg/iam webAuthnCredentialLookupKey writes at enrollment. Three
// independent tests, one encoding.

// webAuthnCredentialID is a realistic 32-byte credential id in the base64url
// form an authenticator's rawId carries.
const webAuthnCredentialID = "QBWMLoIzncNDpMLLKDQrpkWIXd_ZYaRHP7wpWhFjA3s"

// spaAssertionJSON is the assertion document the browser produces, in the
// shape cmd/quicbaas/frontend/src/lib/webauthn.ts encodeAssertionResponse
// emits: base64url string members under the go-webauthn camelCase keys.
const spaAssertionJSON = `{` +
	`"id":"` + webAuthnCredentialID + `",` +
	`"rawId":"` + webAuthnCredentialID + `",` +
	`"type":"public-key",` +
	`"response":{` +
	`"clientDataJSON":"eyJ0eXBlIjoid2ViYXV0aG4uZ2V0IiwiY2hhbGxlbmdlIjoibmV3ZGEtRkh3cnlLMEpOczB1TjNHMlhXa01vVWJtOWJsX1VWOUNjaFNYUSIsIm9yaWdpbiI6Imh0dHBzOi8vbG9jYWxob3N0OjE4NDQzIiwiY3Jvc3NPcmlnaW4iOmZhbHNlfQ",` +
	`"authenticatorData":"SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAACg",` +
	`"signature":"MEQCIHsR7SpVMfbt8zeZKXxKNHK2Oq6UirYSilcAkUTh2rpGAiAyNvQsfFtq7KMsHb3LxKZSwc-meHQ-1vI0E5bxi6UcQg"` +
	`},` +
	`"clientExtensionResults":{},` +
	`"authenticatorAttachment":"cross-platform"}`

// spaFinishLoginBody returns the request body byte-for-byte as the TypeScript
// SDK's webAuthnFinishLogin builds it: Go []byte fields carry STANDARD base64,
// and both the lookup key and the assertion are UTF-8 text inside it.
func spaFinishLoginBody(sessionID, rpSessionID string) []byte {
	body := map[string]interface{}{
		"session_id": sessionID,
		"type":       "fido2",
		"lookup_key": base64.StdEncoding.EncodeToString([]byte(webAuthnCredentialID)),
		"signature":  base64.StdEncoding.EncodeToString([]byte(spaAssertionJSON)),
		"method_metadata": map[string]string{
			"session_id":    rpSessionID,
			"credential_id": webAuthnCredentialID,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		panic("test fixture: " + err.Error())
	}
	return encoded
}

// TestCeremonyFinishLogin_FIDO2_SPAPayloadMatchesVerifierContract is the
// round trip: the SPA's body in, the verifier's inputs out.
func TestCeremonyFinishLogin_FIDO2_SPAPayloadMatchesVerifierContract(t *testing.T) {
	fake := &ceremonyFakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "b1"},
	}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish",
		bytes.NewReader(spaFinishLoginBody("ceremony-1", "rp-1")))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, "the SPA payload must reach the login client")
	require.True(t, fake.finishInvoked, "the ceremony must forward the proof")
	got := fake.finishReq

	assert.Equal(t, "ceremony-1", got.SessionID)
	assert.Equal(t, qrdbtransport.FactorTypeFIDO2, got.Type)

	// 1. The lookup key is the RAW BYTES OF THE BASE64URL CREDENTIAL ID —
	//    byte-identical to what pkg/iam webAuthnCredentialLookupKey stores as
	//    the factor's Credential at enrollment, which is what qrdb's login
	//    resolver indexes on. Not the decoded credential bytes, and not
	//    base64 of the string: those were the two wrong theories.
	assert.Equal(t, []byte(webAuthnCredentialID), got.LookupKey,
		"lookup key must be the base64url credential-id string as bytes")

	// 2. The credential id metadata is the same string UNENCODED, because
	//    go-xkms's verifier compares []byte(metadata["credential_id"])
	//    against the stored factor Credential directly.
	assert.Equal(t, webAuthnCredentialID, got.MethodMetadata["credential_id"])

	// 3. The relying-party session id rides in metadata under "session_id",
	//    the key the verifier reads to fetch the ceremony session. It is a
	//    DIFFERENT value from the ceremony session id above.
	assert.Equal(t, "rp-1", got.MethodMetadata["session_id"])

	// 4. The proof is the assertion document itself, carried in Signature.
	//    The verifier reads it there when metadata["assertion"] is absent and
	//    parses it with exactly this call — so parsing it here is the
	//    contract, not a paraphrase of it.
	parsed, err := protocol.ParseCredentialRequestResponseBytes(got.Signature)
	require.NoError(t, err, "the verifier must be able to parse the forwarded proof")
	assert.Equal(t, webAuthnCredentialID, parsed.ID,
		"the parsed assertion must name the same credential as the lookup key")
	assert.NotEmpty(t, parsed.Response.AuthenticatorData.RPIDHash)
	assert.NotEmpty(t, parsed.Response.Signature)
}

// TestCeremonyFinishLogin_FIDO2_NeverForwardsAKeyItWasNotGiven asserts the
// handler adds nothing to the proof and drops nothing from it.
//
// The FIDO2 path is documented as "forward unchanged" (pkg/service
// login_service.go finishCeremony) — the mTLS overwrite must not leak onto it,
// and no server-side field may be injected into the caller's metadata. This is
// the guard for that claim: it fails if a future change starts populating the
// method metadata on the way through.
func TestCeremonyFinishLogin_FIDO2_NeverForwardsAKeyItWasNotGiven(t *testing.T) {
	fake := &ceremonyFakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "b1"},
	}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish",
		bytes.NewReader(spaFinishLoginBody("ceremony-2", "rp-2")))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, map[string]string{
		"session_id":    "rp-2",
		"credential_id": webAuthnCredentialID,
	}, fake.finishReq.MethodMetadata,
		"the fido2 proof must be forwarded with exactly the caller's metadata")
	assert.Empty(t, fake.finishReq.AltLookupKeys,
		"only the mtls path derives alternate lookup keys")
}

// TestCeremonyFinishLogin_FIDO2_MalformedProofIsRejectedUniformly asserts that
// a body whose signature is not a parseable assertion still produces the
// uniform 401 and never a distinguishing error.
//
// The failure has to stay indistinguishable to the CLIENT (it is the anti
// enumeration property RespondLoginRejected exists for) while being fully
// described on the server — which is what the rejection record added for
// go-quicbaas#484 provides.
func TestCeremonyFinishLogin_FIDO2_MalformedProofIsRejectedUniformly(t *testing.T) {
	fake := &ceremonyFakeLoginClient{
		finishErr: &qrdbtransport.FactorVerificationError{
			FactorID: "factor-1",
			Reason:   "parse webauthn assertion: unexpected end of JSON input",
		},
	}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body := map[string]interface{}{
		"session_id":      "ceremony-3",
		"type":            "fido2",
		"lookup_key":      base64.StdEncoding.EncodeToString([]byte(webAuthnCredentialID)),
		"signature":       base64.StdEncoding.EncodeToString([]byte("not-an-assertion")),
		"method_metadata": map[string]string{"session_id": "rp-3", "credential_id": webAuthnCredentialID},
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(encoded))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NotContains(t, w.Body.String(), "factor-1",
		"the response must not disclose which factor was resolved")
	assert.NotContains(t, w.Body.String(), "parse webauthn assertion",
		"the response must not disclose the verification reason")
}
