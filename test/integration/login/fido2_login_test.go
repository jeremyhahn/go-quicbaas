// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

package login

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	webauthnclient "github.com/jeremyhahn/go-xkms/pkg/webauthn/client"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// FIDO2 REGISTRATION -> UNIFIED LOGIN round trip.
//
// This suite pins the fix for the split-store bug: WebAuthn registration used
// to write ONLY the xkms credential side-channel, while the unified login
// ceremony resolves against qrdb's UserAuthFactor store. A passkey registered
// in the browser could therefore never complete a CLI/SDK login — finishing
// with a REAL credential id was byte-identical to finishing with a bogus one
// ("factor not found").
//
// The registration here runs through the canonical xkms-native REST mount
// (/api/v1/iam/{tenant}/webauthn/registration/*) — the single tenant-scoped
// ceremony surface (the legacy /api/v1/tenants mount was deleted). The
// authenticator is go-xkms's headless CTAP2 software adapter, so the ceremony
// is a real cryptographic exchange, not a fixture.
//
// A regression in the write-through fails TestFIDO2_RegisterThenUnifiedLogin at
// the finish step with "factor not found".
// =============================================================================

// webAuthnRegistrationPath is the canonical xkms-native registration mount.
// It routes through pkg/api/rest's MountXKMSWebAuthnRoutes -> the per-tenant
// xkms relying party, whose credential store carries the qrdb factor
// write-through.
func webAuthnRegistrationPath(tenantID, step string) string {
	return "/api/v1/iam/" + tenantID + "/webauthn/registration/" + step
}

// xkmsSessionHeader is the ceremony correlation header the xkms WebAuthn HTTP
// handler emits from begin and consumes on finish.
const xkmsSessionHeader = "X-Session-Id"

// postForSession issues a POST with optional headers and returns the response
// body, the X-Session-Id header, and the HTTP status.
func postForSession(
	ctx context.Context,
	c *restCeremonyClient,
	path string,
	body []byte,
	headers map[string]string,
) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, err
	}
	return respBody, resp.Header.Get(xkmsSessionHeader), resp.StatusCode, nil
}

// registerPasskey drives a full WebAuthn registration ceremony for the given
// subject against the canonical mount and returns the base64url credential id
// the authenticator minted — the value the unified login submits as its
// lookup key.
func registerPasskey(
	ctx context.Context,
	c *restCeremonyClient,
	adapter *webauthnclient.SoftwareAdapter,
	subject string,
) (string, error) {
	beginBody, _ := json.Marshal(map[string]string{"email": subject})
	optionsJSON, sessionID, status, err := postForSession(ctx, c,
		webAuthnRegistrationPath(systemTenantID, "begin"), beginBody, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("registration/begin returned %d: %s", status, string(optionsJSON))
	}
	if sessionID == "" {
		return "", fmt.Errorf("registration/begin returned no %s header", xkmsSessionHeader)
	}

	attestation, err := adapter.MakeCredential(optionsJSON)
	if err != nil {
		return "", fmt.Errorf("software authenticator MakeCredential: %w", err)
	}

	respBody, _, status, err := postForSession(ctx, c,
		webAuthnRegistrationPath(systemTenantID, "finish"), attestation,
		map[string]string{xkmsSessionHeader: sessionID})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("registration/finish returned %d: %s", status, string(respBody))
	}

	return credentialIDFromWebAuthnDocument(attestation)
}

// credentialIDFromWebAuthnDocument recovers the base64url credential id from a
// navigator.credentials.create() / .get() document. rawId is the canonical
// field; id is the fallback. This is the SAME derivation the CLI performs
// (cmd/quicbaas canonicalFIDO2CredentialID), so the key the test submits is the
// key a real client would submit.
func credentialIDFromWebAuthnDocument(doc []byte) (string, error) {
	var parsed struct {
		ID    string `json:"id"`
		RawID string `json:"rawId"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return "", fmt.Errorf("decode webauthn document: %w", err)
	}
	if parsed.RawID != "" {
		return parsed.RawID, nil
	}
	if parsed.ID != "" {
		return parsed.ID, nil
	}
	return "", fmt.Errorf("webauthn document carries no credential id")
}

// assertWithSoftwareAuthenticator runs the client half of a unified FIDO2 login:
// it drives the software authenticator with the server-issued assertion options
// and assembles the (lookup key, assertion, metadata) triple under the exact
// keys the server's WebAuthn verifier reads.
func assertWithSoftwareAuthenticator(
	adapter *webauthnclient.SoftwareAdapter,
	begin *qrdbtransport.BeginLoginResponse,
) (lookupKey, assertion []byte, meta map[string]string, err error) {
	if begin == nil || begin.ChallengeMetadata["options"] == "" {
		return nil, nil, nil, fmt.Errorf("begin carries no WebAuthn request options")
	}
	assertion, err = adapter.GetAssertion([]byte(begin.ChallengeMetadata["options"]))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("software authenticator GetAssertion: %w", err)
	}
	credentialID, cerr := credentialIDFromWebAuthnDocument(assertion)
	if cerr != nil {
		return nil, nil, nil, cerr
	}
	meta = map[string]string{
		"session_id":    begin.ChallengeMetadata["session_id"],
		"credential_id": credentialID,
	}
	return []byte(credentialID), assertion, meta, nil
}

// TestFIDO2_RegisterThenUnifiedLogin is the headline round trip. It registers a
// passkey through the XKMSWebAuthnManager mount and then completes a unified
// FIDO2 login with that exact credential, proving the registration wrote a
// login-resolvable qrdb auth factor. The issued bearer must encode the
// registrant's identity and be accepted by the live /oauth2/userinfo endpoint.
func TestFIDO2_RegisterThenUnifiedLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adapter := webauthnclient.NewSoftwareAdapter()
	regClient := newRESTCeremonyClient(t, mtlsTLSConfig(t))

	credentialID, err := registerPasskey(ctx, regClient, adapter, adminSubject)
	require.NoError(t, err, "WebAuthn registration through the manager mount must succeed")
	require.NotEmpty(t, credentialID, "registration must mint a credential id")

	c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, cerr, "build login client")
	defer c.Close()

	auth := c.Security().Auth()
	begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     adminSubject,
		TenantID: systemTenantID,
	})
	require.NoError(t, berr, "tenant-scoped fido2 begin must succeed")
	require.NotNil(t, begin)

	lookupKey, assertion, meta, aerr := assertWithSoftwareAuthenticator(adapter, begin)
	require.NoError(t, aerr, "the registered authenticator must produce an assertion")
	require.Equal(t, credentialID, string(lookupKey),
		"the assertion must be produced by the credential the registration minted")

	finish, ferr := auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID:      begin.SessionID,
		Type:           qrdbtransport.FactorTypeFIDO2,
		LookupKey:      lookupKey,
		Signature:      assertion,
		MethodMetadata: meta,
	})
	require.NoError(t, ferr,
		"a passkey registered through the WebAuthn ceremony must be resolvable by the unified login "+
			"(a 'factor not found' here means the registration write-through regressed)")

	require.NotEmpty(t, finish.Bearer, "login must mint a bearer")
	require.NotNil(t, finish.Identity, "login must resolve an identity")
	require.Equal(t, adminSubject, finish.Identity.Subject,
		"the resolved subject must be the registrant")
	require.Equal(t, systemTenantID, finish.Identity.TenantID,
		"the resolved tenant must be the relying party's tenant")

	jwtSub, jwtTenant, jerr := jwtSubject(finish.Bearer)
	require.NoError(t, jerr, "bearer must be a decodable JWT")
	require.Equal(t, finish.Identity.Subject, jwtSub, "JWT sub must match the resolved subject")
	if jwtTenant != "" {
		require.Equal(t, finish.Identity.TenantID, jwtTenant, "JWT tenant claim must match")
	}

	require.NoError(t, validateBearerOverREST(ctx, finish.Bearer),
		"the issued bearer must validate against /oauth2/userinfo")
}

// TestFIDO2_UnknownCredentialFailsClosed is the negative control that gives the
// round-trip test its meaning. Before the write-through fix, a REAL registered
// credential and a fabricated one produced byte-identical 401s. Finishing with
// a credential id no ceremony ever minted must STILL fail closed — while
// TestFIDO2_RegisterThenUnifiedLogin succeeds with a real one.
func TestFIDO2_UnknownCredentialFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, cerr, "build login client")
	defer c.Close()

	auth := c.Security().Auth()
	begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		TenantID: systemTenantID,
	})
	require.NoError(t, berr, "tenant-scoped fido2 begin must succeed")

	bogus := make([]byte, 32)
	_, rerr := rand.Read(bogus)
	require.NoError(t, rerr)
	bogusCredentialID := base64.RawURLEncoding.EncodeToString(bogus)

	_, ferr := auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID: begin.SessionID,
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte(bogusCredentialID),
		Signature: []byte(`{"id":"x","rawId":"x","type":"public-key","response":{}}`),
		MethodMetadata: map[string]string{
			"session_id":    begin.ChallengeMetadata["session_id"],
			"credential_id": bogusCredentialID,
		},
	})
	require.Error(t, ferr, "an unregistered credential id must never mint a bearer")
}

// TestFIDO2_ReRegistrationIsIdempotent proves the write-through's idempotency
// contract at the wire: re-running the registration ceremony for an already
// enrolled authenticator must NOT error out with a duplicate-credential
// failure, and the resulting passkey must still complete a login.
func TestFIDO2_ReRegistrationIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adapter := webauthnclient.NewSoftwareAdapter()
	regClient := newRESTCeremonyClient(t, mtlsTLSConfig(t))

	first, err := registerPasskey(ctx, regClient, adapter, adminSubject)
	require.NoError(t, err, "first registration must succeed")

	second, err := registerPasskey(ctx, regClient, adapter, adminSubject)
	require.NoError(t, err,
		"re-running the registration ceremony must not fail with a duplicate-credential error")
	require.NotEmpty(t, second)

	// Each ceremony mints a distinct credential; both must be usable.
	c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, cerr, "build login client")
	defer c.Close()

	auth := c.Security().Auth()
	begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     adminSubject,
		TenantID: systemTenantID,
	})
	require.NoError(t, berr)

	lookupKey, assertion, meta, aerr := assertWithSoftwareAuthenticator(adapter, begin)
	require.NoError(t, aerr)
	require.Contains(t, []string{first, second}, string(lookupKey),
		"the assertion must come from one of the registered credentials")

	finish, ferr := auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID:      begin.SessionID,
		Type:           qrdbtransport.FactorTypeFIDO2,
		LookupKey:      lookupKey,
		Signature:      assertion,
		MethodMetadata: meta,
	})
	require.NoError(t, ferr, "both registered credentials must be login-resolvable")
	require.NotEmpty(t, finish.Bearer)
}
