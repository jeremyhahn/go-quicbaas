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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	webauthnclient "github.com/jeremyhahn/go-xkms/pkg/webauthn/client"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/fido2"

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
// The passkey here is provisioned by the administrator through
// AdminFactors().Enroll, whose FIDO2 enrolment writes the qrdb factor
// through. The anonymous public ceremony at the xkms-native REST mount
// (/api/v1/iam/{tenant}/webauthn/registration/*) refuses an existing account
// by design: the account holder signs in to add a passkey, or an
// administrator provisions one. The round trip asserts that refusal. The
// authenticator is go-xkms's headless CTAP2 software adapter, so the ceremony
// is a real cryptographic exchange, not a fixture. The login hint is the
// admin's email, the subject login key.
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

// enrollAdminPasskey provisions a passkey for the seeded admin the way the
// product does for an existing account: an administrator enrols it through
// AdminFactors().Enroll. It returns the base64url credential id the
// authenticator minted, the value the unified login submits as its lookup key.
func enrollAdminPasskey(
	ctx context.Context,
	t *testing.T,
	c quicbaas.Client,
	adapter *webauthnclient.SoftwareAdapter,
) string {
	t.Helper()

	h := &recordingPasskey{softwarePasskey: &softwarePasskey{adapter: adapter}}
	results, err := c.Security().Auth().AdminFactors().Enroll(ctx, &quicbaas.AdminFactorEnrollRequest{
		Factors:        []string{"fido2"},
		Subject:        adminSubject,
		TenantID:       systemTenantID,
		RelyingPartyID: adminFactorRPID,
		FIDO2Handler:   h,
	})
	require.NoError(t, err, "administrator passkey enrolment must succeed")
	require.Len(t, results, 1, "one enrolment")
	require.Equal(t, qrdbsdk.FactorFIDO2, results[0].Factor, "enrolled factor")
	return h.credentialID
}

// recordingPasskey wraps the software passkey and records the base64url id of
// the credential the authenticator minted, the same encoding
// credentialIDFromWebAuthnDocument reads from an assertion.
type recordingPasskey struct {
	*softwarePasskey
	credentialID string
}

func (p *recordingPasskey) RegisterWebAuthn(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error) {
	att, err := p.softwarePasskey.RegisterWebAuthn(opts)
	if err != nil {
		return nil, err
	}
	p.credentialID = base64.RawURLEncoding.EncodeToString(att.CredentialID)
	return att, nil
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
	ctx context.Context,
	adapter *webauthnclient.SoftwareAdapter,
	begin *qrdbtransport.BeginLoginResponse,
) (lookupKey, assertion []byte, meta map[string]string, err error) {
	if begin == nil || begin.ChallengeMetadata["options"] == "" {
		return nil, nil, nil, fmt.Errorf("begin carries no WebAuthn request options")
	}
	assertion, err = adapter.GetAssertion(ctx, &webauthnclient.CeremonyOptions{
		OptionsJSON: []byte(begin.ChallengeMetadata["options"]),
		Origin:      adminFactorOrigin,
	})
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

// TestFIDO2_RegisterThenUnifiedLogin is the headline round trip. The
// administrator provisions the admin's passkey through AdminFactors().Enroll,
// whose FIDO2 enrolment writes a login-resolvable qrdb auth factor, and a
// unified FIDO2 login then completes with that exact credential, hinted by
// the admin's email. The anonymous public registration ceremony refuses the
// same existing account by design. The issued bearer must encode the admin's
// identity and be accepted by the live /oauth2/userinfo endpoint.
func TestFIDO2_RegisterThenUnifiedLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, cerr, "build login client")
	defer c.Close()

	adapter := webauthnclient.NewSoftwareAdapter()
	credentialID := enrollAdminPasskey(ctx, t, c, adapter)
	require.NotEmpty(t, credentialID, "enrolment must mint a credential id")

	admin := resolveEnrolledAdmin(ctx, t, c, transportREST)
	require.NotEmpty(t, admin.Email, "the enrolled admin must carry an email login key")

	// The anonymous public ceremony never enrols a credential for an existing
	// account: the holder signs in, or an administrator provisions it.
	regClient := newRESTCeremonyClient(t, mtlsTLSConfig(t))
	beginBody, err := json.Marshal(map[string]string{"email": admin.Email})
	require.NoError(t, err, "encode registration begin")
	refusal, _, status, err := postForSession(ctx, regClient,
		webAuthnRegistrationPath(systemTenantID, "begin"), beginBody, nil)
	require.NoError(t, err, "anonymous registration/begin")
	require.Equal(t, http.StatusForbidden, status,
		"the anonymous ceremony must refuse an existing account: %s", string(refusal))
	require.Contains(t, string(refusal), "registration_refused")

	auth := c.Security().Auth()
	begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     admin.Email,
		TenantID: systemTenantID,
	})
	require.NoError(t, berr, "tenant-scoped fido2 begin must succeed")
	require.NotNil(t, begin)

	lookupKey, assertion, meta, aerr := assertWithSoftwareAuthenticator(ctx, adapter, begin)
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
// contract at the wire: repeated administrator enrolment of the same
// authenticator must NOT fail with a duplicate-credential error, and the
// passkey must still complete a login.
func TestFIDO2_ReRegistrationIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, cerr, "build login client")
	defer c.Close()

	adapter := webauthnclient.NewSoftwareAdapter()
	first := enrollAdminPasskey(ctx, t, c, adapter)
	second := enrollAdminPasskey(ctx, t, c, adapter)
	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second, "each enrolment mints a distinct credential")

	// Each enrolment mints a distinct credential; both must be usable.
	admin := resolveEnrolledAdmin(ctx, t, c, transportREST)
	require.NotEmpty(t, admin.Email, "the enrolled admin must carry an email login key")

	auth := c.Security().Auth()
	begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     admin.Email,
		TenantID: systemTenantID,
	})
	require.NoError(t, berr)

	lookupKey, assertion, meta, aerr := assertWithSoftwareAuthenticator(ctx, adapter, begin)
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
