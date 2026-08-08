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
	"encoding/json"
	"net/http"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// FIDO2 login-ceremony TENANT SCOPE.
//
// WebAuthn is relying-party scoped: the RP id/origin belong to a tenant, so the
// server's tenant-aware WebAuthn verifier (pkg/server.tenantWebAuthnVerifier)
// resolves the per-tenant xkms WebAuthn service and FAILS CLOSED when the
// BeginLogin request carries no tenant — it never falls back to a default
// tenant, which would break tenant isolation.
//
// A client that omits the tenant therefore gets a bare 401 from
// /api/v1/auth/login/begin BEFORE any authenticator is touched. That was a real
// shipped bug in `quicbaas auth login --method fido2` (the CLI never sent the
// tenant), so these tests pin BOTH halves of the contract at the wire:
//
//	no tenant   -> 401, no challenge issued (fail closed)
//	with tenant -> 200 + a real WebAuthn challenge the client can drive
// =============================================================================

// beginBody POSTs an arbitrary begin-login body so a test can omit fields the
// typed helper always sends (restCeremonyClient.begin always scopes to the
// system tenant). Returns the decoded response for 200s and the raw status
// otherwise.
func (c *restCeremonyClient) beginBody(ctx context.Context, body map[string]string) (*wireBeginLoginResponse, int, error) {
	raw, _ := json.Marshal(body)
	respBody, status, err := c.post(ctx, "/api/v1/auth/login/begin", raw)
	if err != nil {
		return nil, status, err
	}
	if status != http.StatusOK {
		return nil, status, nil
	}
	var out wireBeginLoginResponse
	if jerr := json.Unmarshal(respBody, &out); jerr != nil {
		return nil, status, jerr
	}
	return &out, status, nil
}

// TestUnifiedLogin_FIDO2WithoutTenantIsRejected proves the server refuses to
// open a WebAuthn login ceremony that carries no tenant scope. This is the
// server-side half of the CLI bug: an unscoped begin can only ever 401, so a
// client MUST send the tenant.
func TestUnifiedLogin_FIDO2WithoutTenantIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client := newRESTCeremonyClient(t, noCertTLSConfig(t))
	resp, status, err := client.beginBody(ctx, map[string]string{
		"type": string(qrdbtransport.FactorTypeFIDO2),
	})
	require.NoError(t, err, "begin-login transport failure")
	require.Equal(t, http.StatusUnauthorized, status,
		"an unscoped fido2 begin-login must fail closed (the WebAuthn RP is per-tenant)")
	require.Nil(t, resp, "no challenge may be issued without a tenant scope")
}

// TestUnifiedLogin_FIDO2WithTenantIssuesChallenge proves the tenant-scoped
// begin returns a REAL WebAuthn challenge: the assertion options blob plus the
// relying-party session id, under the exact metadata keys the client reads
// (cmd/quicbaas/canonical_auth_login.go canonicalFIDO2OptionsKey /
// canonicalFIDO2SessionIDKey). Without these the CLI cannot drive a CTAP2
// authenticator, so this asserts the ceremony is usable end to end and not
// merely non-erroring.
//
// The hint names no enrolled user on purpose: the ceremony then issues a
// discoverable-credential challenge, which keeps the assertion independent of
// which users the surrounding suite happens to have created.
func TestUnifiedLogin_FIDO2WithTenantIssuesChallenge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client := newRESTCeremonyClient(t, noCertTLSConfig(t))
	resp, status, err := client.beginBody(ctx, map[string]string{
		"type":      string(qrdbtransport.FactorTypeFIDO2),
		"tenant_id": systemTenantID,
	})
	require.NoError(t, err, "begin-login transport failure")
	require.Equal(t, http.StatusOK, status, "tenant-scoped fido2 begin-login must be accepted")
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.SessionID, "login session id")
	require.NotEmpty(t, resp.Nonce, "WebAuthn challenge bytes")

	options := resp.ChallengeMetadata["options"]
	require.NotEmpty(t, options,
		"begin must carry the WebAuthn request options the client feeds to the authenticator")
	require.NotEmpty(t, resp.ChallengeMetadata["session_id"],
		"begin must carry the relying-party session id the client echoes on finish")

	// The options must be the real navigator.credentials.get() document — a
	// client that cannot parse an rpId out of it cannot drive an authenticator.
	var parsed struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal([]byte(options), &parsed), "WebAuthn options must be valid JSON")
	require.NotEmpty(t, parsed.PublicKey.Challenge, "options must carry a challenge")
	require.NotEmpty(t, parsed.PublicKey.RPID, "options must carry the tenant's relying-party id")
}

// TestUnifiedLogin_FIDO2SDKSendsTenant proves the SDK path the CLI uses carries
// the tenant all the way to the server: BeginLogin over each network transport
// with TenantID set must be accepted, while the same call without it must be
// rejected. This is the transport-parity guard — the tenant field has to
// survive REST JSON, gRPC protobuf, QUIC frames, Unix (gRPC) and MCP tool args.
func TestUnifiedLogin_FIDO2SDKSendsTenant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, tr := range allLoginTransports() {
		tr := tr
		t.Run(string(tr), func(t *testing.T) {
			c, cerr := newLoginClient(t, tr, mtlsTLSConfig(t))
			require.NoError(t, cerr, "[%s] build login client", tr)
			defer c.Close()

			auth := c.Security().Auth()

			begin, berr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
				Type:     qrdbtransport.FactorTypeFIDO2,
				TenantID: systemTenantID,
			})
			require.NoError(t, berr, "[%s] tenant-scoped fido2 begin must succeed", tr)
			require.NotNil(t, begin)
			require.NotEmpty(t, begin.ChallengeMetadata["options"],
				"[%s] tenant must reach the per-tenant relying party", tr)

			_, uerr := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
				Type: qrdbtransport.FactorTypeFIDO2,
			})
			require.Error(t, uerr, "[%s] unscoped fido2 begin must fail closed", tr)
		})
	}
}
