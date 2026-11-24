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
	"errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuiltinAccountProviders_ShipNoUnverifiedVendorDetails locks in the
// deliberate decision recorded in BuiltinAccountProviders: QuicBaaS ships the
// two OAuth-capable providers with an EMPTY token endpoint and client id
// because neither vendor documents them. If someone later hardcodes a guessed
// endpoint, this test is the tripwire.
func TestBuiltinAccountProviders_ShipNoUnverifiedVendorDetails(t *testing.T) {
	registry := DefaultAccountProviderRegistry()

	claude, ok := registry.Descriptor(AccountProviderClaude)
	if !ok {
		t.Fatalf("Descriptor(claude) missing from the built-in registry")
	}
	if claude.TokenEndpoint != "" || claude.ClientID != "" {
		t.Fatalf("claude built-in ships endpoint=%q client_id=%q, want both empty (operator-supplied)",
			claude.TokenEndpoint, claude.ClientID)
	}
	if claude.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("claude AccessTokenEnvKey = %q, want CLAUDE_CODE_OAUTH_TOKEN", claude.AccessTokenEnvKey)
	}
	if !claude.OAuthCapable || !claude.BuiltIn {
		t.Fatalf("claude descriptor = %+v, want OAuthCapable and BuiltIn", claude)
	}

	codex, ok := registry.Descriptor(AccountProviderCodex)
	if !ok {
		t.Fatalf("Descriptor(codex) missing from the built-in registry")
	}
	if codex.TokenEndpoint != "" || codex.ClientID != "" || codex.AccessTokenEnvKey != "" {
		t.Fatalf("codex built-in ships endpoint=%q client_id=%q env_key=%q, want all empty (operator-supplied)",
			codex.TokenEndpoint, codex.ClientID, codex.AccessTokenEnvKey)
	}
	if !codex.OAuthCapable {
		t.Fatalf("codex descriptor = %+v, want OAuthCapable", codex)
	}

	opencode, ok := registry.Descriptor(AccountProviderOpenCode)
	if !ok {
		t.Fatalf("Descriptor(opencode) missing from the built-in registry")
	}
	if opencode.OAuthCapable {
		t.Fatalf("opencode descriptor = %+v, want file-based (not OAuthCapable)", opencode)
	}
}

// TestBuiltinAccountProvidersMatchConfig keeps the built-in descriptor set and
// the name list pkg/config validates against in step. pkg/config cannot import
// pkg/orchestration (the dependency runs the other way), so this assertion is
// what prevents the two drifting apart.
func TestBuiltinAccountProvidersMatchConfig(t *testing.T) {
	fromDescriptors := make(map[string]struct{})
	for _, descriptor := range BuiltinAccountProviders() {
		fromDescriptors[string(descriptor.Provider)] = struct{}{}
	}
	fromConfig := make(map[string]struct{})
	for _, name := range config.BuiltinAccountProviderNames() {
		fromConfig[name] = struct{}{}
	}
	if !reflect.DeepEqual(fromDescriptors, fromConfig) {
		t.Fatalf("built-in providers = %v, config.BuiltinAccountProviderNames() = %v: the two lists must match",
			fromDescriptors, fromConfig)
	}
}

// TestAccountProviderRegistry_Effective_AccountFieldsWin is the precedence
// rule: a value the account set for itself is never replaced by a provider
// default.
func TestAccountProviderRegistry_Effective_AccountFieldsWin(t *testing.T) {
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:          AccountProviderClaude,
		TokenEndpoint:     "https://provider.example/token",
		ClientID:          "provider-client",
		AccessTokenEnvKey: "PROVIDER_TOKEN",
		Scopes:            []string{"provider-scope"},
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	cred := &AccountCredential{
		TokenEndpoint:     "https://account.example/token",
		ClientID:          "account-client",
		AccessTokenEnvKey: "ACCOUNT_TOKEN",
		Scopes:            []string{"account-scope"},
	}
	effective := registry.Effective(AccountProviderClaude, cred)

	if effective.TokenEndpoint != "https://account.example/token" {
		t.Fatalf("TokenEndpoint = %q, want the account's own value", effective.TokenEndpoint)
	}
	if effective.ClientID != "account-client" {
		t.Fatalf("ClientID = %q, want the account's own value", effective.ClientID)
	}
	if effective.AccessTokenEnvKey != "ACCOUNT_TOKEN" {
		t.Fatalf("AccessTokenEnvKey = %q, want the account's own value", effective.AccessTokenEnvKey)
	}
	if !reflect.DeepEqual(effective.Scopes, []string{"account-scope"}) {
		t.Fatalf("Scopes = %v, want the account's own value", effective.Scopes)
	}
}

// TestAccountProviderRegistry_Effective_FillsOmittedFields is the other half of
// the precedence rule: what the account left empty comes from the descriptor,
// and the stored envelope itself is never mutated.
func TestAccountProviderRegistry_Effective_FillsOmittedFields(t *testing.T) {
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      AccountProviderClaude,
		TokenEndpoint: "https://provider.example/token",
		ClientID:      "provider-client",
		Scopes:        []string{"inference"},
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	stored := &AccountCredential{AccessToken: "at", RefreshToken: "rt"}
	effective := registry.Effective(AccountProviderClaude, stored)

	if effective.TokenEndpoint != "https://provider.example/token" {
		t.Fatalf("TokenEndpoint = %q, want the provider default", effective.TokenEndpoint)
	}
	if effective.ClientID != "provider-client" {
		t.Fatalf("ClientID = %q, want the provider default", effective.ClientID)
	}
	// The built-in env key survives an override that did not mention it.
	if effective.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("AccessTokenEnvKey = %q, want the built-in default", effective.AccessTokenEnvKey)
	}
	if !reflect.DeepEqual(effective.Scopes, []string{"inference"}) {
		t.Fatalf("Scopes = %v, want the provider default", effective.Scopes)
	}
	if stored.TokenEndpoint != "" || stored.ClientID != "" || stored.AccessTokenEnvKey != "" {
		t.Fatalf("Effective() mutated the stored envelope: %+v", stored)
	}
}

// TestAccountProviderRegistry_Effective_UnknownProvider proves a provider with
// no descriptor contributes nothing rather than erroring or inventing values.
func TestAccountProviderRegistry_Effective_UnknownProvider(t *testing.T) {
	registry := DefaultAccountProviderRegistry()

	if _, ok := registry.Descriptor("nope"); ok {
		t.Fatalf("Descriptor(nope) ok = true, want false")
	}
	cred := &AccountCredential{AccessToken: "at"}
	if got := registry.Effective("nope", cred); got != cred {
		t.Fatalf("Effective(unknown) = %+v, want the credential unchanged", got)
	}
	if got := registry.Effective(AccountProviderClaude, nil); got != nil {
		t.Fatalf("Effective(nil credential) = %+v, want nil", got)
	}
}

// TestNewAccountProviderRegistry_CustomProvider proves an operator-defined
// provider QuicBaaS knows nothing about becomes fully usable, including being
// oauth-capable purely because it declared a token endpoint.
func TestNewAccountProviderRegistry_CustomProvider(t *testing.T) {
	const custom AccountProvider = "acme-llm"
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:          custom,
		DisplayName:       "ACME LLM",
		TokenEndpoint:     "https://id.acme.example/oauth2/token",
		ClientID:          "acme-client",
		AccessTokenEnvKey: "ACME_LLM_TOKEN",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	descriptor, ok := registry.Descriptor(custom)
	if !ok {
		t.Fatalf("Descriptor(%s) missing after configuring it", custom)
	}
	if descriptor.BuiltIn {
		t.Fatalf("custom provider descriptor = %+v, want BuiltIn false", descriptor)
	}
	if !descriptor.OAuthCapable {
		t.Fatalf("custom provider with a token endpoint must be oauth-capable: %+v", descriptor)
	}
	if descriptor.DisplayName != "ACME LLM" {
		t.Fatalf("DisplayName = %q, want ACME LLM", descriptor.DisplayName)
	}

	// The built-ins are still there alongside it.
	if _, builtinOK := registry.Descriptor(AccountProviderClaude); !builtinOK {
		t.Fatalf("configuring a custom provider dropped the built-ins")
	}
}

// TestNewAccountProviderRegistry_OverrideBuiltinFieldByField proves an override
// replaces only the fields it sets.
func TestNewAccountProviderRegistry_OverrideBuiltinFieldByField(t *testing.T) {
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      AccountProviderClaude,
		TokenEndpoint: "https://auth.example/v1/oauth/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	claude, _ := registry.Descriptor(AccountProviderClaude)
	if claude.TokenEndpoint != "https://auth.example/v1/oauth/token" {
		t.Fatalf("TokenEndpoint = %q, want the override", claude.TokenEndpoint)
	}
	if claude.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("AccessTokenEnvKey = %q, want the built-in value preserved", claude.AccessTokenEnvKey)
	}
	if claude.DisplayName != "Anthropic Claude Code" {
		t.Fatalf("DisplayName = %q, want the built-in value preserved", claude.DisplayName)
	}
	if !claude.BuiltIn {
		t.Fatalf("overriding a built-in must not clear the BuiltIn flag: %+v", claude)
	}
}

// TestNewAccountProviderRegistry_OverrideMakesFileProviderOAuthCapable proves
// configuring an endpoint for the otherwise file-based opencode provider opts
// its accounts into the refresh grant.
func TestNewAccountProviderRegistry_OverrideMakesFileProviderOAuthCapable(t *testing.T) {
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      AccountProviderOpenCode,
		TokenEndpoint: "https://id.example/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	descriptor, _ := registry.Descriptor(AccountProviderOpenCode)
	if !descriptor.OAuthCapable {
		t.Fatalf("descriptor = %+v, want OAuthCapable once an endpoint is configured", descriptor)
	}
}

// TestNewAccountProviderRegistry_RejectsBadOverrides covers the two ways a
// registry cannot be built.
func TestNewAccountProviderRegistry_RejectsBadOverrides(t *testing.T) {
	t.Run("duplicate provider", func(t *testing.T) {
		_, err := NewAccountProviderRegistry(
			ProviderDescriptor{Provider: AccountProviderClaude, TokenEndpoint: "https://a.example/token"},
			ProviderDescriptor{Provider: AccountProviderClaude, TokenEndpoint: "https://b.example/token"},
		)
		var duplicate *AccountProviderDuplicateError
		if !errors.As(err, &duplicate) {
			t.Fatalf("error = %v (%T), want *AccountProviderDuplicateError", err, err)
		}
		if !errors.Is(err, ErrAccountProviderDuplicate) {
			t.Fatalf("errors.Is(err, ErrAccountProviderDuplicate) = false")
		}
	})

	t.Run("empty provider name", func(t *testing.T) {
		_, err := NewAccountProviderRegistry(ProviderDescriptor{TokenEndpoint: "https://a.example/token"})
		var invalid *AccountInvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("error = %v (%T), want *AccountInvalidError", err, err)
		}
	})
}

// TestAccountProviderRegistry_Providers covers both listings.
func TestAccountProviderRegistry_Providers(t *testing.T) {
	registry, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      "acme-llm",
		TokenEndpoint: "https://id.acme.example/oauth2/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	want := []AccountProvider{"acme-llm", AccountProviderClaude, AccountProviderCodex, AccountProviderOpenCode}
	if got := registry.Providers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Providers() = %v, want %v", got, want)
	}
	wantOAuth := []AccountProvider{"acme-llm", AccountProviderClaude, AccountProviderCodex}
	if got := registry.OAuthCapableProviders(); !reflect.DeepEqual(got, wantOAuth) {
		t.Fatalf("OAuthCapableProviders() = %v, want %v", got, wantOAuth)
	}
}

// TestAccountProviderRegistry_ValidateCredential covers every write-time rule,
// happy path and rejection alike.
func TestAccountProviderRegistry_ValidateCredential(t *testing.T) {
	configured, err := NewAccountProviderRegistry(ProviderDescriptor{
		Provider:      AccountProviderClaude,
		TokenEndpoint: "https://auth.example/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	builtinOnly := DefaultAccountProviderRegistry()

	tests := []struct {
		name     string
		registry *AccountProviderRegistry
		provider AccountProvider
		mode     AccountCredentialMode
		cred     *AccountCredential
		wantErr  error
	}{
		{
			name:     "oauth account with its own endpoint",
			registry: builtinOnly,
			provider: AccountProviderClaude,
			mode:     AccountRefreshOAuth2,
			cred:     &AccountCredential{RefreshToken: "rt", TokenEndpoint: "https://account.example/token"},
		},
		{
			name:     "oauth account relying on the provider default",
			registry: configured,
			provider: AccountProviderClaude,
			mode:     AccountCredentialModeOAuthRefresh,
			cred:     &AccountCredential{RefreshToken: "rt"},
		},
		{
			name:     "static account with no refresh token",
			registry: builtinOnly,
			provider: AccountProviderCodex,
			mode:     AccountRefreshNone,
			cred:     &AccountCredential{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}},
		},
		{
			name:     "oauth account with no resolvable endpoint",
			registry: builtinOnly,
			provider: AccountProviderClaude,
			mode:     AccountRefreshOAuth2,
			cred:     &AccountCredential{RefreshToken: "rt"},
			wantErr:  ErrAccountProviderEndpointUnresolved,
		},
		{
			name:     "oauth account with no refresh token",
			registry: configured,
			provider: AccountProviderClaude,
			mode:     AccountRefreshOAuth2,
			cred:     &AccountCredential{AccessToken: "at"},
			wantErr:  ErrAccountRefreshTokenRequired,
		},
		{
			name:     "static account carrying a refresh token",
			registry: builtinOnly,
			provider: AccountProviderClaude,
			mode:     AccountRefreshNone,
			cred:     &AccountCredential{AccessToken: "at", RefreshToken: "rt"},
			wantErr:  ErrAccountStaticTokenRefreshable,
		},
		{
			name:     "static account with an empty credential",
			registry: builtinOnly,
			provider: AccountProviderCodex,
			mode:     AccountRefreshNone,
			cred:     &AccountCredential{AccessTokenEnvKey: "CODEX_API_KEY", Scopes: []string{"s"}},
			wantErr:  ErrAccountCredentialEmpty,
		},
		{
			name:     "static account carrying only files",
			registry: builtinOnly,
			provider: AccountProviderCodex,
			mode:     AccountRefreshNone,
			cred:     &AccountCredential{Files: map[string][]byte{".codex/auth.json": []byte(`{}`)}},
		},
		{
			name:     "static account carrying only env",
			registry: builtinOnly,
			provider: AccountProviderCodex,
			mode:     AccountRefreshNone,
			cred:     &AccountCredential{Env: map[string]string{"CODEX_API_KEY": "k"}},
		},
		{
			name:     "unrecognized mode",
			registry: builtinOnly,
			provider: AccountProviderClaude,
			mode:     "sometimes",
			cred:     &AccountCredential{AccessToken: "at"},
			wantErr:  ErrAccountCredentialModeInvalid,
		},
		{
			name:     "nil credential",
			registry: builtinOnly,
			provider: AccountProviderClaude,
			mode:     AccountRefreshNone,
			cred:     nil,
			wantErr:  ErrAccountInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.registry.ValidateCredential(tt.provider, tt.mode, tt.cred)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateCredential() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateCredential() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
