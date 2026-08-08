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

// Package main: regression coverage for the TENANT SCOPE of the canonical
// "auth login" ceremony.
//
// The login ceremony is relying-party scoped: the server's tenant-aware
// WebAuthn verifier (pkg/server.tenantWebAuthnVerifier) resolves the per-tenant
// xkms WebAuthn service and FAILS CLOSED with EmptyLoginTenantError when the
// BeginLogin request carries no tenant. A CLI that omits the tenant therefore
// gets a bare 401 from /api/v1/auth/login/begin before the local authenticator
// is ever touched — which is exactly the bug these tests pin.
package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// fakeLoginAuthService is an AuthService that records the unified login
// ceremony requests the CLI submits. Only BeginLogin / FinishLogin are
// implemented; every other accessor is inherited from the embedded (nil)
// interface and panics if used, so a test that strays off the login path
// fails loudly instead of silently passing.
type fakeLoginAuthService struct {
	quicbaas.AuthService

	begin  qrdbtransport.BeginLoginRequest
	finish qrdbtransport.FinishLoginRequest

	beginCalls  int
	finishCalls int

	beginResp  *qrdbtransport.BeginLoginResponse
	beginErr   error
	finishResp *qrdbtransport.FinishLoginResponse
	finishErr  error
}

// BeginLogin records the request and returns the canned response.
func (f *fakeLoginAuthService) BeginLogin(_ context.Context, req qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	f.beginCalls++
	f.begin = req
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.beginResp, nil
}

// FinishLogin records the request and returns the canned response.
func (f *fakeLoginAuthService) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*quicbaas.FinishLoginResponse, error) {
	f.finishCalls++
	f.finish = req
	if f.finishErr != nil {
		return nil, f.finishErr
	}
	if f.finishResp == nil {
		return nil, nil
	}
	return &quicbaas.FinishLoginResponse{FinishLoginResponse: f.finishResp}, nil
}

// newFakeLoginClient wires the login fake through the SDK accessor chain the
// CLI walks (Client -> Security() -> Auth()).
func newFakeLoginClient(auth quicbaas.AuthService) quicbaas.Client {
	return &fakeSecurityClient{security: &fakeSecurityService{auth: auth}}
}

// webAuthnOptionsJSON is a minimal but structurally valid WebAuthn assertion
// options document — the shape BeginChallenge returns under the "options"
// challenge-metadata key.
const webAuthnOptionsJSON = `{"publicKey":{"challenge":"Y2hhbGxlbmdl","rpId":"localhost",` +
	`"allowCredentials":[{"type":"public-key","id":"Y3JlZC1pZA"}],"userVerification":"preferred"}}`

// newFIDO2BeginResponse builds the BeginLogin response a tenant-scoped
// WebAuthn challenge produces.
func newFIDO2BeginResponse() *qrdbtransport.BeginLoginResponse {
	return &qrdbtransport.BeginLoginResponse{
		SessionID: "login-session-1",
		Nonce:     []byte("challenge"),
		ChallengeMetadata: map[string]string{
			canonicalFIDO2OptionsKey:   webAuthnOptionsJSON,
			canonicalFIDO2SessionIDKey: "rp-session-1",
		},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
}

// newFinishResponse builds a successful FinishLogin response.
func newFinishResponse() *qrdbtransport.FinishLoginResponse {
	return &qrdbtransport.FinishLoginResponse{
		Bearer:    "bearer-token",
		ExpiresAt: time.Now().Add(time.Hour),
		FactorID:  "factor-1",
		Identity: &qrdbtransport.Identity{
			Subject:  "admin@system.qrdb.local",
			TenantID: "system",
		},
	}
}

// stubFIDO2Assertion swaps the CTAP2 device seam for one that returns a fixed
// assertion carrying the given credential id.
func stubFIDO2Assertion(t *testing.T, credentialID string) {
	t.Helper()
	assertion, err := json.Marshal(map[string]any{
		"id":       credentialID,
		"rawId":    credentialID,
		"type":     "public-key",
		"response": map[string]any{},
	})
	require.NoError(t, err)
	withCanonicalFIDO2AssertFunc(t, func(context.Context, []byte, string, string) ([]byte, error) {
		return assertion, nil
	})
}

// =============================================================================
// canonicalLoginTenant
// =============================================================================

// TestCanonicalLoginTenant_Explicit returns the supplied tenant unchanged.
func TestCanonicalLoginTenant_Explicit(t *testing.T) {
	got, err := canonicalLoginTenant("acme-corp")
	require.NoError(t, err)
	assert.Equal(t, "acme-corp", got)
}

// TestCanonicalLoginTenant_Empty fails closed rather than starting an
// unscoped ceremony the server would reject with a bare 401.
func TestCanonicalLoginTenant_Empty(t *testing.T) {
	_, err := canonicalLoginTenant("")
	require.Error(t, err)
	var want *MissingLoginTenantError
	require.ErrorAs(t, err, &want)
}

// =============================================================================
// runCanonicalAuthLoginV2 — tenant propagation
// =============================================================================

// TestRunCanonicalAuthLoginV2_FIDO2_SendsTenant is the REGRESSION test for the
// bare-UNAUTHORIZED bug: "auth login --method fido2" must scope BeginLogin to
// the tenant, because the server's WebAuthn relying party is per-tenant.
func TestRunCanonicalAuthLoginV2_FIDO2_SendsTenant(t *testing.T) {
	auth := &fakeLoginAuthService{
		beginResp:  newFIDO2BeginResponse(),
		finishResp: newFinishResponse(),
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)
	stubFIDO2Assertion(t, "Y3JlZC1pZA")

	err := runCanonicalAuthLoginV2(context.Background(), &canonicalAuthLoginOptions{
		method:   "fido2",
		username: "admin@system.qrdb.local",
		tenantID: "system",
	})
	require.NoError(t, err)

	require.Equal(t, 1, auth.beginCalls)
	assert.Equal(t, "system", auth.begin.TenantID,
		"BeginLogin must carry the tenant: the WebAuthn relying party is per-tenant")
	assert.Equal(t, qrdbtransport.FactorTypeFIDO2, auth.begin.Type)
	assert.Equal(t, "admin@system.qrdb.local", auth.begin.Hint)

	require.Equal(t, 1, auth.finishCalls)
	assert.Equal(t, "login-session-1", auth.finish.SessionID)
	assert.Equal(t, []byte("Y3JlZC1pZA"), auth.finish.LookupKey)
	assert.Equal(t, "rp-session-1", auth.finish.MethodMetadata[canonicalFIDO2SessionIDKey])
}

// TestRunCanonicalAuthLoginV2_MTLSAndPIVSendTenant proves the tenant scope is
// not FIDO2-specific: every method carries it so the login session the server
// opens is bound to the right tenant for FinishLogin verification.
func TestRunCanonicalAuthLoginV2_MTLSSendsTenant(t *testing.T) {
	auth := &fakeLoginAuthService{
		beginResp:  &qrdbtransport.BeginLoginResponse{SessionID: "s1", Nonce: []byte("nonce")},
		finishResp: newFinishResponse(),
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)

	certPath := filepath.Join(t.TempDir(), "admin.pem")
	writePEMCertToPath(t, certPath)
	err := runCanonicalAuthLoginV2(context.Background(), &canonicalAuthLoginOptions{
		method:         "mtls",
		clientCertPath: certPath,
		tenantID:       "acme-corp",
	})
	require.NoError(t, err)
	require.Equal(t, 1, auth.beginCalls)
	assert.Equal(t, "acme-corp", auth.begin.TenantID)
}

// TestRunCanonicalAuthLoginV2_MissingTenant fails BEFORE any network call so
// the operator gets an actionable message instead of a bare UNAUTHORIZED.
func TestRunCanonicalAuthLoginV2_MissingTenant(t *testing.T) {
	auth := &fakeLoginAuthService{beginResp: newFIDO2BeginResponse()}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)

	err := runCanonicalAuthLoginV2(context.Background(), &canonicalAuthLoginOptions{
		method:   "fido2",
		username: "admin@system.qrdb.local",
	})
	require.Error(t, err)
	var want *MissingLoginTenantError
	require.ErrorAs(t, err, &want)
	assert.Zero(t, auth.beginCalls, "no ceremony may start without a tenant scope")
}

// TestNewCanonicalAuthLoginCmdV2_InheritsGlobalTenant proves the wiring the
// operator actually exercises: the global --tenant flag (default: the qrdb
// system tenant) is what scopes the ceremony. No per-command tenant flag
// exists, so a regression here silently reintroduces the unscoped login.
func TestNewCanonicalAuthLoginCmdV2_InheritsGlobalTenant(t *testing.T) {
	auth := &fakeLoginAuthService{
		beginResp:  newFIDO2BeginResponse(),
		finishResp: newFinishResponse(),
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)
	stubFIDO2Assertion(t, "Y3JlZC1pZA")

	prev := globalTenantID
	globalTenantID = "tenant-from-global-flag"
	t.Cleanup(func() { globalTenantID = prev })

	cmd := newCanonicalAuthLoginCmdV2()
	cmd.SetArgs([]string{"--method", "fido2", "--username", "u@example.com"})
	require.NoError(t, cmd.Execute())

	require.Equal(t, 1, auth.beginCalls)
	assert.Equal(t, "tenant-from-global-flag", auth.begin.TenantID)
}
