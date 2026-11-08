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

package orchestration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestAccountTokenManager_RegisterOAuthRefreshers_CoversOAuthCapableProviders
// is the regression test for the wiring bug this feature closes: before it,
// RegisterRefresher had no production caller and every oauth2 account failed
// its first renewal with AccountRefresherNotConfiguredError. Server bring-up
// makes exactly this call; asserting it here needs no server.
func TestAccountTokenManager_RegisterOAuthRefreshers_CoversOAuthCapableProviders(t *testing.T) {
	mgr, _, _ := setupTokenManager(t)

	registered := mgr.RegisterOAuthRefreshers(NewOAuth2TokenRefresher(OAuth2RefresherConfig{}))

	want := []AccountProvider{AccountProviderClaude, AccountProviderCodex}
	if !reflect.DeepEqual(registered, want) {
		t.Fatalf("RegisterOAuthRefreshers() = %v, want %v", registered, want)
	}
	for _, provider := range want {
		if _, ok := mgr.Refresher(provider); !ok {
			t.Fatalf("no refresher registered for oauth-capable provider %q", provider)
		}
	}
	// opencode is file-based, so it deliberately gets none.
	if _, ok := mgr.Refresher(AccountProviderOpenCode); ok {
		t.Fatalf("a refresher was registered for the file-based opencode provider")
	}
}

// TestAccountTokenManager_RegisterOAuthRefreshers_ConfiguredProvider proves an
// operator-defined provider is covered by the same one-line wiring.
func TestAccountTokenManager_RegisterOAuthRefreshers_ConfiguredProvider(t *testing.T) {
	const custom AccountProvider = "acme-llm"
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      custom,
		TokenEndpoint: "https://id.acme.example/oauth2/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	mgr := NewAccountTokenManager(newTestAccountSecretStore(t), NewInMemoryAccountStore(), 0,
		WithAccountProviders(registry))

	mgr.RegisterOAuthRefreshers(NewOAuth2TokenRefresher(OAuth2RefresherConfig{}))

	if _, ok := mgr.Refresher(custom); !ok {
		t.Fatalf("no refresher registered for configured provider %q", custom)
	}
}

// TestAccountTokenManager_RegisterOAuthRefreshers_NilRefresher proves the
// wiring call refuses to register a nil refresher, which would otherwise turn
// every renewal into a nil-map hit reported as "not configured" from a
// provider that looks configured.
func TestAccountTokenManager_RegisterOAuthRefreshers_NilRefresher(t *testing.T) {
	mgr, _, _ := setupTokenManager(t)

	if registered := mgr.RegisterOAuthRefreshers(nil); registered != nil {
		t.Fatalf("RegisterOAuthRefreshers(nil) = %v, want nil", registered)
	}
	if _, ok := mgr.Refresher(AccountProviderClaude); ok {
		t.Fatalf("a nil refresher was registered for claude")
	}
}

// mintedToken is the access token newTestTokenEndpoint issues on its nth call.
func mintedToken(n int64) string {
	return "minted-token-" + strconv.FormatInt(n, 10)
}

// newTestTokenEndpoint returns an httptest RFC 6749 token endpoint that issues
// a numbered access token per call (mintedToken), plus a counter of the
// requests it saw and the last form values it was presented with.
func newTestTokenEndpoint(t *testing.T) (*httptest.Server, *atomic.Int64, *atomic.Value) {
	t.Helper()
	calls := &atomic.Int64{}
	lastForm := &atomic.Value{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		lastForm.Store(r.Form.Encode())
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  mintedToken(n),
			"refresh_token": "rotated-refresh",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	t.Cleanup(server.Close)
	return server, calls, lastForm
}

// TestAccountResolver_MintsFreshTokenFromProviderDefaults is the end-to-end
// path the whole feature exists for: an oauth2 account that carries only a
// refresh token is handed a freshly minted access token at spawn, projected
// into the environment variable its provider's CLI reads — with the token
// endpoint, client id and env key all coming from the provider registry.
func TestAccountResolver_MintsFreshTokenFromProviderDefaults(t *testing.T) {
	tokenEndpoint, calls, lastForm := newTestTokenEndpoint(t)
	ctx := context.Background()

	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      AccountProviderClaude,
		TokenEndpoint: tokenEndpoint.URL,
		ClientID:      "provider-client",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 0, WithAccountProviders(registry))
	tokens.RegisterOAuthRefreshers(NewOAuth2TokenRefresher(OAuth2RefresherConfig{}))
	resolver := NewAccountResolver(store, tokens, NewAccountStrategyRegistry(nil))

	account := &Account{
		ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
		DisplayName: "alice-work", RefreshPolicy: AccountRefreshOAuth2,
	}
	if createErr := store.Create(ctx, account); createErr != nil {
		t.Fatalf("Create() error = %v", createErr)
	}
	// Deliberately minimal: no endpoint, no client id, no env key, and an
	// access token that already expired.
	if putErr := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:  "stale-token",
		RefreshToken: "operator-supplied-refresh",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); putErr != nil {
		t.Fatalf("Put() error = %v", putErr)
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("token endpoint called %d times, want 1", calls.Load())
	}
	if got := resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != mintedToken(1) {
		t.Fatalf("Env[CLAUDE_CODE_OAUTH_TOKEN] = %q, want the freshly minted token", got)
	}
	form, _ := lastForm.Load().(string)
	if form != "client_id=provider-client&grant_type=refresh_token&refresh_token=operator-supplied-refresh" {
		t.Fatalf("token endpoint saw form %q, want the provider client id and the account's refresh token", form)
	}

	// The rotated pair is persisted, but the provider defaults injected for
	// the call are NOT: changing the endpoint in configuration must keep
	// taking effect instead of being frozen into the account.
	stored, err := secretStore.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.AccessToken != mintedToken(1) || stored.RefreshToken != "rotated-refresh" {
		t.Fatalf("stored credential = %+v, want the rotated pair persisted", stored)
	}
	if stored.TokenEndpoint != "" || stored.ClientID != "" || stored.AccessTokenEnvKey != "" {
		t.Fatalf("stored credential = %+v, want provider defaults left out of the envelope", stored)
	}
	if stored.ExpiresAt.IsZero() {
		t.Fatalf("stored credential has no expiry after a renewal reporting expires_in")
	}
}

// TestAccountResolver_ProjectsAccessTokenEnvKeyForStaticAccount proves the
// registry's env-key default applies to static accounts too — a long-lived
// `claude setup-token` credential imported with nothing but an access token is
// still projected into the variable Claude Code reads.
func TestAccountResolver_ProjectsAccessTokenEnvKeyForStaticAccount(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 0)
	resolver := NewAccountResolver(store, tokens, NewAccountStrategyRegistry(nil))

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshNone}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "long-lived-token"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "long-lived-token" {
		t.Fatalf("Env[CLAUDE_CODE_OAUTH_TOKEN] = %q, want the static token projected", got)
	}
}

// TestAccountTokenManager_Resolve_CanonicalModeSpellingRefreshes proves an
// account stored with the DOMAIN spelling of the oauth mode refreshes exactly
// like one stored with the transport alias. Comparing the mode against a
// single spelling used to make one of the two silently never renew.
func TestAccountTokenManager_Resolve_CanonicalModeSpellingRefreshes(t *testing.T) {
	ctx := context.Background()
	mgr, store, secretStore := setupTokenManager(t)
	refresher := &countingRefresher{}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{
		ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
		RefreshPolicy: AccountCredentialModeOAuthRefresh,
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.AccessToken != "renewed-token" {
		t.Fatalf("AccessToken = %q, want renewed-token", cred.AccessToken)
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresher called %d times, want 1", refresher.calls.Load())
	}
}

// TestAccountTokenManager_Resolve_UnresolvableEndpointFailsLoudly proves an
// oauth account whose endpoint resolves nowhere fails with the refresher's own
// validation error rather than silently handing out the stale token.
func TestAccountTokenManager_Resolve_UnresolvableEndpointFailsLoudly(t *testing.T) {
	ctx := context.Background()
	mgr, store, secretStore := setupTokenManager(t)
	mgr.RegisterOAuthRefreshers(NewOAuth2TokenRefresher(OAuth2RefresherConfig{}))

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := mgr.Resolve(ctx, account); err == nil {
		t.Fatalf("Resolve() error = nil, want a refresh failure for an unresolvable token endpoint")
	}
	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.Status != AccountStatusError {
		t.Fatalf("account.Status = %s, want error", got.Status)
	}
}

// TestApplyRenewal_AdoptsServerReportedScopes proves scopes are taken from the
// token endpoint when it reports a different set, and otherwise the account's
// own (possibly empty) scopes are left alone rather than being replaced with
// the provider defaults.
func TestApplyRenewal_AdoptsServerReportedScopes(t *testing.T) {
	stored := &AccountCredential{RefreshToken: "rt"}
	effective := &AccountCredential{RefreshToken: "rt", Scopes: []string{"default-scope"}}

	unchanged := applyRenewal(stored, effective, &AccountCredential{
		AccessToken: "at", Scopes: []string{"default-scope"},
	})
	if unchanged.Scopes != nil {
		t.Fatalf("Scopes = %v, want the provider default left out of the stored envelope", unchanged.Scopes)
	}

	reported := applyRenewal(stored, effective, &AccountCredential{
		AccessToken: "at", Scopes: []string{"scope-a", "scope-b"},
	})
	if !reflect.DeepEqual(reported.Scopes, []string{"scope-a", "scope-b"}) {
		t.Fatalf("Scopes = %v, want the server-reported set", reported.Scopes)
	}
	if reported.RefreshToken != "rt" {
		t.Fatalf("RefreshToken = %q, want the existing one kept when the server did not rotate it", reported.RefreshToken)
	}
}
