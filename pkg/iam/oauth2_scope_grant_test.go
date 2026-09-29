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
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// =============================================================================
// go-quicbaas#653 — a service principal's authority is the grant on its
// application record, never the scope it asked for.
//
// The heartbeat route requires orchestrator:agent_heartbeat. An enrolled agent
// held no permission at all: nothing bound one to the principal, and the token
// endpoint copied the client's own request into scp. These tests pin both
// halves: a granted app receives its grant, and an ungranted one receives
// nothing no matter what it asks for.
// =============================================================================

// grantedAppOptions builds the register options for a client_credentials app
// carrying an explicit server-side grant.
func grantedAppOptions(name string, scopes []string) RegisterAppOptions {
	return RegisterAppOptions{
		Name:          name,
		GrantTypes:    []string{GrantTypeClientCredentials},
		ResponseTypes: []string{},
		Scopes:        scopes,
	}
}

// TestClientCredentialsGrant_IssuesTheApplicationGrant proves the happy path:
// an app whose record carries orchestrator:agent_heartbeat gets a token whose
// scope claim carries it, so the qrdb RBAC authorizer unions it in and the
// heartbeat is authorized.
func TestClientCredentialsGrant_IssuesTheApplicationGrant(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, storage := newTenantRoutedOIDCProvider(t, tenantID)

	granted := []string{"orchestrator:agent_heartbeat", "orchestrator:hook"}
	app, err := mgr.RegisterApp(context.Background(), tenantID,
		grantedAppOptions("granted-agent", granted))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	stored, err := storage.GetApp(ContextWithTenant(context.Background(), tenantID), app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(stored.Scopes) != len(granted) {
		t.Fatalf("stored grant = %v, want %v", stored.Scopes, granted)
	}

	tokenCtx := ContextWithTenant(context.Background(), tenantID)
	resp, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
		Scope:        []string{"orchestrator:agent_heartbeat"},
	})
	if err != nil {
		t.Fatalf("client_credentials grant: %v", err)
	}
	if resp.Scope != "orchestrator:agent_heartbeat" {
		t.Fatalf("issued scope = %q, want %q", resp.Scope, "orchestrator:agent_heartbeat")
	}

	// No requested scope means the whole grant, per RFC 6749 §3.3.
	whole, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	})
	if err != nil {
		t.Fatalf("client_credentials grant without a requested scope: %v", err)
	}
	if whole.Scope != "orchestrator:agent_heartbeat orchestrator:hook" {
		t.Fatalf("issued scope = %q, want the whole grant", whole.Scope)
	}
}

// TestClientCredentialsGrant_RefusesAnUngrantedPrincipal is the load-bearing
// half: an application with NO grant asks for the heartbeat permission and is
// refused. Before #653 the token endpoint copied the request into the token,
// so any client_credentials client could mint itself any permission it named.
func TestClientCredentialsGrant_RefusesAnUngrantedPrincipal(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	app, err := mgr.RegisterApp(context.Background(), tenantID,
		grantedAppOptions("ungranted-app", nil))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	tokenCtx := ContextWithTenant(context.Background(), tenantID)
	resp, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
		Scope:        []string{"orchestrator:agent_heartbeat"},
	})
	if err == nil {
		t.Fatalf("an ungranted client minted scope %q", resp.Scope)
	}
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
		t.Fatalf("refusal = %v, want an %s OAuth error", err, ErrOAuthInvalidScope)
	}

	// The same client asking for nothing gets a token that authorizes nothing.
	empty, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	})
	if err != nil {
		t.Fatalf("client_credentials grant without a requested scope: %v", err)
	}
	if empty.Scope != "" {
		t.Fatalf("an ungranted client received scope %q", empty.Scope)
	}
}

// TestClientCredentialsGrant_RefusesScopeOutsideTheGrant proves the grant
// bounds the request: an app granted the heartbeat cannot escalate to a
// task-creating orchestrator principal by naming a broader scope.
func TestClientCredentialsGrant_RefusesScopeOutsideTheGrant(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	app, err := mgr.RegisterApp(context.Background(), tenantID,
		grantedAppOptions("narrow-agent", []string{"orchestrator:agent_heartbeat"}))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	tokenCtx := ContextWithTenant(context.Background(), tenantID)
	resp, err := provider.Token(tokenCtx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
		Scope:        []string{"orchestrator:agent_heartbeat", "orchestrator:task_create"},
	})
	if err == nil {
		t.Fatalf("a narrowly granted client minted scope %q", resp.Scope)
	}
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
		t.Fatalf("refusal = %v, want an %s OAuth error", err, ErrOAuthInvalidScope)
	}
}

// TestResolveGrantedScopes covers the resolver directly, including the
// wildcard and protocol-scope rules the token endpoint depends on.
func TestResolveGrantedScopes(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		requested []string
		want      []string
	}{
		{
			name:      "no request yields the whole grant",
			granted:   []string{"orchestrator:agent_heartbeat", "orchestrator:hook"},
			requested: nil,
			want:      []string{"orchestrator:agent_heartbeat", "orchestrator:hook"},
		},
		{
			name:      "a request narrows the grant",
			granted:   []string{"orchestrator:agent_heartbeat", "orchestrator:hook"},
			requested: []string{"orchestrator:hook"},
			want:      []string{"orchestrator:hook"},
		},
		{
			name:      "no grant yields no permissions",
			granted:   nil,
			requested: nil,
			want:      []string{},
		},
		{
			name:      "a resource wildcard covers a concrete permission",
			granted:   []string{"orchestrator:*"},
			requested: []string{"orchestrator:hook"},
			want:      []string{"orchestrator:hook"},
		},
		{
			name:      "a full wildcard covers anything",
			granted:   []string{"*:*"},
			requested: []string{"keys:get"},
			want:      []string{"keys:get"},
		},
		{
			name:      "oidc protocol scopes need no grant",
			granted:   nil,
			requested: []string{"openid", "profile", "offline_access"},
			want:      []string{"openid", "profile", "offline_access"},
		},
		{
			name:      "protocol and permission scopes resolve together",
			granted:   []string{"mcp:tools"},
			requested: []string{"openid", "mcp:tools"},
			want:      []string{"openid", "mcp:tools"},
		},
		{
			name:      "duplicates and blanks are dropped",
			granted:   []string{"mcp:tools"},
			requested: []string{"mcp:tools", " ", "mcp:tools"},
			want:      []string{"mcp:tools"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveGrantedScopes("client-a", tc.granted, tc.requested)
			if err != nil {
				t.Fatalf("ResolveGrantedScopes: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestResolveGrantedScopes_RefusesUngranted covers the error path: every way a
// client can name a permission it does not hold.
func TestResolveGrantedScopes_RefusesUngranted(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		requested []string
		wantScope string
	}{
		{
			name:      "no grant at all",
			granted:   nil,
			requested: []string{"orchestrator:agent_heartbeat"},
			wantScope: "orchestrator:agent_heartbeat",
		},
		{
			name:      "outside a narrow grant",
			granted:   []string{"orchestrator:agent_heartbeat"},
			requested: []string{"orchestrator:agent_heartbeat", "keys:get"},
			wantScope: "keys:get",
		},
		{
			name:      "a wildcard the client invented for itself",
			granted:   []string{"orchestrator:agent_heartbeat"},
			requested: []string{"orchestrator:*"},
			wantScope: "orchestrator:*",
		},
		{
			name:      "a wildcard grant does not cross resources",
			granted:   []string{"orchestrator:*"},
			requested: []string{"keys:get"},
			wantScope: "keys:get",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveGrantedScopes("client-a", tc.granted, tc.requested)
			if err == nil {
				t.Fatalf("ResolveGrantedScopes admitted %v", got)
			}
			var notGranted *ScopeNotGrantedError
			if !errors.As(err, &notGranted) {
				t.Fatalf("error = %T (%v), want *ScopeNotGrantedError", err, err)
			}
			if notGranted.Scope != tc.wantScope {
				t.Fatalf("refused scope %q, want %q", notGranted.Scope, tc.wantScope)
			}
			if notGranted.ClientID != "client-a" {
				t.Fatalf("refusal names client %q", notGranted.ClientID)
			}
		})
	}
}

// TestPermissionScopes separates the authority claim from the protocol scopes:
// only the former belongs in scp, which qrdb's RBAC authorizer unions in.
func TestPermissionScopes(t *testing.T) {
	got := PermissionScopes([]string{"openid", "orchestrator:hook", "profile", "kv:get"})
	want := []string{"orchestrator:hook", "kv:get"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if len(PermissionScopes(nil)) != 0 {
		t.Fatal("PermissionScopes(nil) must be empty")
	}
	if len(PermissionScopes([]string{"openid"})) != 0 {
		t.Fatal("an OIDC protocol scope must not reach the scp claim")
	}
}

// TestAppManager_GrantScopes covers the grant-convergence verb and its error
// path on an application that does not exist.
func TestAppManager_GrantScopes(t *testing.T) {
	const tenantID = "automatethethings"
	_, mgr, storage := newTenantRoutedOIDCProvider(t, tenantID)
	ctx := ContextWithTenant(context.Background(), tenantID)

	app, err := mgr.RegisterApp(context.Background(), tenantID, grantedAppOptions("converge-me", nil))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	grant := FleetRuntimeGrant()
	if _, err := mgr.GrantScopes(ctx, tenantID, app.ClientID, grant); err != nil {
		t.Fatalf("GrantScopes: %v", err)
	}
	stored, err := storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(stored.Scopes) != len(grant) {
		t.Fatalf("stored grant = %v, want %v", stored.Scopes, grant)
	}

	// Idempotent: a second convergence onto the same set is a no-op.
	if _, err := mgr.GrantScopes(ctx, tenantID, app.ClientID, grant); err != nil {
		t.Fatalf("GrantScopes (repeat): %v", err)
	}

	// The credential-bearing fields never leave the manager.
	granted, err := mgr.GrantScopes(ctx, tenantID, app.ClientID, grant)
	if err != nil {
		t.Fatalf("GrantScopes: %v", err)
	}
	if granted.ClientSecret != "" || granted.RegistrationTokenHash != "" {
		t.Fatal("GrantScopes leaked a credential hash")
	}

	if _, err := mgr.GrantScopes(ctx, tenantID, "app_"+tenantID+"_nonexistent", grant); err == nil {
		t.Fatal("GrantScopes converged an application that does not exist")
	}
}

// TestAgentRuntimeGrant_IsTheAdvertisedLeastPrivilegeSet pins the grant's
// shape: the two control-plane permissions a daemon exercises outbound, the
// three pooled-account permissions it resolves a provider credential with, the
// three service-surface permissions its own spawn server checks inbound, and
// nothing else.
func TestAgentRuntimeGrant_IsTheAdvertisedLeastPrivilegeSet(t *testing.T) {
	grant := FleetRuntimeGrant()
	want := map[string]struct{}{
		"agent:runtime:spawn":        {},
		"agent:runtime:prompt":       {},
		"agent:runtime:status":       {},
		FleetScopeControlHeartbeat:   {},
		FleetScopeControlHook:        {},
		FleetScopeAccountMaterialize: {},
		FleetScopeAccountRead:        {},
		FleetScopeAccountList:        {},
	}
	if len(grant) != len(want) {
		t.Fatalf("grant = %v, want exactly %d permissions", grant, len(want))
	}
	for _, p := range grant {
		if _, ok := want[p]; !ok {
			t.Fatalf("grant contains unexpected permission %q", p)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("grant is missing %v", want)
	}

	// A caller must not be able to widen the shared grant in place.
	FleetRuntimeGrant()[0] = "*:*"
	if FleetRuntimeGrant()[0] == "*:*" {
		t.Fatal("FleetRuntimeGrant returns aliased backing storage a caller can widen")
	}
}

// TestTokenMTLS_IssuesTheApplicationGrant covers the certificate-authenticated
// half of the grant. It is the endpoint a current daemon prefers, so a grant
// that only reached /oauth2/token would leave every mTLS agent refused.
func TestTokenMTLS_IssuesTheApplicationGrant(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	grant := FleetRuntimeGrant()
	app, err := mgr.RegisterApp(context.Background(), tenantID, grantedAppOptions("mtls-agent", grant))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	tokenCtx := ContextWithTenant(context.Background(), tenantID)
	resp, err := provider.TokenMTLS(tokenCtx, &MTLSTokenRequest{
		GrantType:          GrantTypeClientCredentials,
		ClientID:           app.ClientID,
		CertThumbprintS256: "dGVzdC10aHVtYnByaW50",
		AttestMethod:       AttestMethodManual,
	})
	if err != nil {
		t.Fatalf("TokenMTLS: %v", err)
	}
	for _, want := range grant {
		if !strings.Contains(resp.Scope, want) {
			t.Fatalf("issued scope %q omits %q", resp.Scope, want)
		}
	}

	claims := decodeTokenClaims(t, resp.AccessToken)
	scp, ok := claims["scp"].([]interface{})
	if !ok {
		t.Fatalf("the mTLS service token carries no scp claim: %v", claims["scp"])
	}
	got := make([]string, 0, len(scp))
	for _, v := range scp {
		got = append(got, v.(string))
	}
	for _, want := range grant {
		if !slices.Contains(got, want) {
			t.Fatalf("scp = %v, omits %q", got, want)
		}
	}
}

// TestTokenMTLS_RefusesAnUngrantedPrincipal is the mTLS negative: a valid
// certificate proves WHICH principal is asking, never WHAT it may do.
func TestTokenMTLS_RefusesAnUngrantedPrincipal(t *testing.T) {
	const tenantID = "automatethethings"
	provider, mgr, _ := newTenantRoutedOIDCProvider(t, tenantID)

	app, err := mgr.RegisterApp(context.Background(), tenantID, grantedAppOptions("mtls-ungranted", nil))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	tokenCtx := ContextWithTenant(context.Background(), tenantID)
	resp, err := provider.TokenMTLS(tokenCtx, &MTLSTokenRequest{
		GrantType:          GrantTypeClientCredentials,
		ClientID:           app.ClientID,
		Scope:              "orchestrator:agent_heartbeat",
		CertThumbprintS256: "dGVzdC10aHVtYnByaW50",
		AttestMethod:       AttestMethodManual,
	})
	if err == nil {
		t.Fatalf("an ungranted mTLS client minted scope %q", resp.Scope)
	}
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
		t.Fatalf("refusal = %v, want an %s OAuth error", err, ErrOAuthInvalidScope)
	}
}

// decodeTokenClaims returns the JWT payload as a claim map. The token was just
// minted by the provider under test, so verification is not the point here.
func decodeTokenClaims(t *testing.T, token string) map[string]interface{} {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return claims
}
