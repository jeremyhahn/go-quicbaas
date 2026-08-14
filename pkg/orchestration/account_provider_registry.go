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

import "sort"

// =============================================================================
// Provider registry — per-provider OAuth defaults.
//
// A pooled account carries its own AccountCredential (token endpoint, client
// id, scopes, env key). Those per-account values are ALWAYS authoritative: the
// registry only supplies a value for a field the account left empty. This
// keeps two properties the account subsystem depends on:
//
//   - No vendor endpoint is ever hardcoded into the credential path. The
//     refresh grant still reads AccountCredential.TokenEndpoint and nothing
//     else (see OAuth2TokenRefresher); the registry merely fills that field in
//     before the call when the operator configured a provider-wide default.
//
//   - Registry values are never persisted into an account. AccountTokenManager
//     projects them onto a COPY of the stored envelope for the duration of one
//     resolution, so changing a provider default in configuration takes effect
//     on the next spawn instead of being frozen into whatever was written at
//     import time.
//
// Two providers ship built in — Anthropic (AccountProviderClaude) and OpenAI
// (AccountProviderCodex) — and an operator may define additional providers or
// override a built-in's defaults through configuration
// (orchestration.account_providers). See docs/orchestration/accounts.md.
// =============================================================================

// ProviderDescriptor carries the provider-wide OAuth defaults applied to any
// account of that provider which did not specify its own.
type ProviderDescriptor struct {
	// Provider is the AccountProvider these defaults apply to. Required.
	Provider AccountProvider

	// DisplayName is an operator-facing label for the provider.
	DisplayName string

	// TokenEndpoint is the default RFC 6749 token endpoint used by accounts
	// of this provider that did not set AccountCredential.TokenEndpoint.
	//
	// Empty means QuicBaaS ships no default for this provider and every
	// oauth_refresh account must supply its own — see BuiltinAccountProviders
	// for why both built-ins ship empty.
	TokenEndpoint string

	// ClientID is the default OAuth2 client identifier. Empty means none is
	// sent unless the account supplies one.
	ClientID string

	// Scopes is the default scope set recorded on accounts that did not
	// declare their own. It is informational: the refresh_token grant does
	// not re-send scopes.
	Scopes []string

	// AccessTokenEnvKey is the environment variable the resolved access token
	// is projected into for accounts that did not name one themselves (e.g.
	// CLAUDE_CODE_OAUTH_TOKEN for Claude Code). Empty for providers whose CLI
	// reads its credential from a config file rather than the environment.
	AccessTokenEnvKey string

	// OAuthCapable reports whether accounts of this provider may use
	// AccountCredentialModeOAuthRefresh. It drives which providers get a
	// TokenRefresher registered (see AccountTokenManager.RegisterOAuthRefreshers)
	// and is independent of whether a default TokenEndpoint is shipped: a
	// provider can be oauth-capable with every account supplying its own
	// endpoint.
	OAuthCapable bool

	// BuiltIn marks a descriptor QuicBaaS ships. Operator-defined providers
	// are false. Configuration may override a built-in's fields; it never
	// changes this flag.
	BuiltIn bool
}

// BuiltinAccountProviders returns the descriptors QuicBaaS ships.
//
// Both OAuth-capable built-ins deliberately ship an EMPTY TokenEndpoint and
// ClientID. Neither Anthropic nor OpenAI documents the OAuth application their
// CLI authenticates with as a supported API, and inventing a value here would
// bake an unverified vendor endpoint into every deployment. The operator
// supplies it — per account (AccountCredential.TokenEndpoint) or provider-wide
// (orchestration.account_providers) — and an oauth_refresh account that
// resolves to neither is rejected at write time with
// AccountProviderEndpointUnresolvedError rather than failing later at spawn.
func BuiltinAccountProviders() []ProviderDescriptor {
	return []ProviderDescriptor{
		{
			Provider:    AccountProviderClaude,
			DisplayName: "Anthropic Claude Code",
			// CLAUDE_CODE_OAUTH_TOKEN is the environment variable Claude Code
			// reads its OAuth token from (`claude setup-token`).
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
			OAuthCapable:      true,
			BuiltIn:           true,
		},
		{
			Provider:    AccountProviderCodex,
			DisplayName: "OpenAI Codex",
			// No default env key: the codex CLI reads its OAuth material from
			// its own auth.json rather than from an access-token environment
			// variable, so there is no honest equivalent of
			// CLAUDE_CODE_OAUTH_TOKEN to ship. An operator pooling API keys
			// sets access_token_env_key (or Env) on the account explicitly.
			OAuthCapable: true,
			BuiltIn:      true,
		},
		{
			Provider:    AccountProviderOpenCode,
			DisplayName: "OpenCode",
			// File-based: the credential is materialized as
			// .config/opencode/auth.json (AccountCredential.Files), so there
			// is no token endpoint and no environment projection.
			OAuthCapable: false,
			BuiltIn:      true,
		},
	}
}

// AccountProviderRegistry resolves the effective ProviderDescriptor for an
// AccountProvider.
//
// It is built once at start-up and never mutated afterwards, so concurrent
// readers need no synchronization at all — a lock-free read of an immutable
// map rather than a mutex on the spawn hot path.
type AccountProviderRegistry struct {
	descriptors map[AccountProvider]ProviderDescriptor
}

// DefaultAccountProviderRegistry returns a registry holding only the built-in
// descriptors. It is the registry used when a deployment configures no
// provider overrides.
func DefaultAccountProviderRegistry() *AccountProviderRegistry {
	registry, err := NewAccountProviderRegistry()
	if err != nil {
		// Unreachable: the built-in set is a compile-time constant with
		// distinct, non-empty provider names, which is the only failure mode
		// NewAccountProviderRegistry has with no overrides.
		panic("orchestration: built-in account providers are invalid: " + err.Error())
	}
	return registry
}

// NewAccountProviderRegistry returns a registry of the built-in descriptors
// with overrides applied on top, field by field: an override supplies only the
// fields it sets and inherits the rest from the built-in (or from the zero
// descriptor, for a provider QuicBaaS does not ship).
//
// An override naming a provider twice is rejected with
// AccountProviderDuplicateError — silently keeping the last one would make the
// effective endpoint depend on configuration file ordering.
func NewAccountProviderRegistry(overrides ...ProviderDescriptor) (*AccountProviderRegistry, error) {
	descriptors := make(map[AccountProvider]ProviderDescriptor, len(BuiltinAccountProviders())+len(overrides))
	for _, builtin := range BuiltinAccountProviders() {
		descriptors[builtin.Provider] = builtin
	}

	seen := make(map[AccountProvider]struct{}, len(overrides))
	for _, override := range overrides {
		if override.Provider == "" {
			return nil, &AccountInvalidError{Field: "provider", Message: "provider name must not be empty"}
		}
		if _, duplicate := seen[override.Provider]; duplicate {
			return nil, &AccountProviderDuplicateError{Provider: override.Provider}
		}
		seen[override.Provider] = struct{}{}
		descriptors[override.Provider] = descriptors[override.Provider].merge(override)
	}
	return &AccountProviderRegistry{descriptors: descriptors}, nil
}

// merge returns d with every field override actually sets applied over it.
// An empty field on override leaves d's value untouched, which is what makes
// "override only the token endpoint of a built-in" work without having to
// restate the provider's env key.
func (d ProviderDescriptor) merge(override ProviderDescriptor) ProviderDescriptor {
	merged := d
	merged.Provider = override.Provider
	if override.DisplayName != "" {
		merged.DisplayName = override.DisplayName
	}
	if override.TokenEndpoint != "" {
		merged.TokenEndpoint = override.TokenEndpoint
	}
	if override.ClientID != "" {
		merged.ClientID = override.ClientID
	}
	if len(override.Scopes) > 0 {
		merged.Scopes = override.Scopes
	}
	if override.AccessTokenEnvKey != "" {
		merged.AccessTokenEnvKey = override.AccessTokenEnvKey
	}
	// A provider is oauth-capable if it already was, if the override declares
	// it, or if the override supplies a token endpoint — configuring an
	// endpoint for a provider is an unambiguous statement that its accounts
	// can run the refresh grant.
	merged.OAuthCapable = d.OAuthCapable || override.OAuthCapable || override.TokenEndpoint != ""
	return merged
}

// Descriptor returns the effective descriptor for provider. The second result
// is false for a provider neither built in nor configured.
func (r *AccountProviderRegistry) Descriptor(provider AccountProvider) (ProviderDescriptor, bool) {
	if r == nil {
		return ProviderDescriptor{}, false
	}
	descriptor, ok := r.descriptors[provider]
	return descriptor, ok
}

// Providers returns every known provider, sorted by name for stable output.
func (r *AccountProviderRegistry) Providers() []AccountProvider {
	if r == nil {
		return nil
	}
	providers := make([]AccountProvider, 0, len(r.descriptors))
	for provider := range r.descriptors {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	return providers
}

// OAuthCapableProviders returns every provider whose accounts may use
// AccountCredentialModeOAuthRefresh, sorted by name.
func (r *AccountProviderRegistry) OAuthCapableProviders() []AccountProvider {
	if r == nil {
		return nil
	}
	providers := make([]AccountProvider, 0, len(r.descriptors))
	for provider, descriptor := range r.descriptors {
		if descriptor.OAuthCapable {
			providers = append(providers, provider)
		}
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	return providers
}

// Effective returns a copy of cred with every provider-default field the
// account left empty filled in. The stored envelope is never modified, and the
// returned copy is never persisted (see AccountTokenManager.renew), so a
// default can be changed in configuration without rewriting any account.
//
// A nil cred, or a provider with no descriptor, yields cred unchanged.
func (r *AccountProviderRegistry) Effective(provider AccountProvider, cred *AccountCredential) *AccountCredential {
	if cred == nil {
		return nil
	}
	descriptor, ok := r.Descriptor(provider)
	if !ok {
		return cred
	}

	effective := *cred
	if effective.TokenEndpoint == "" {
		effective.TokenEndpoint = descriptor.TokenEndpoint
	}
	if effective.ClientID == "" {
		effective.ClientID = descriptor.ClientID
	}
	if len(effective.Scopes) == 0 {
		effective.Scopes = descriptor.Scopes
	}
	if effective.AccessTokenEnvKey == "" {
		effective.AccessTokenEnvKey = descriptor.AccessTokenEnvKey
	}
	return &effective
}

// ValidateCredential enforces, at WRITE time, every rule that would otherwise
// only surface when a spawn tried to use the account:
//
//   - mode must be a recognized AccountCredentialMode (either spelling).
//   - oauth_refresh requires a refresh token, and requires that the account's
//     own TokenEndpoint or its provider's default resolves to one. Without an
//     endpoint the account is unusable the moment its access token expires.
//   - static_token forbids a refresh token: an account holding a renewal
//     credential nobody will ever redeem is a mode mistake, not a spare field.
func (r *AccountProviderRegistry) ValidateCredential(provider AccountProvider, mode AccountCredentialMode, cred *AccountCredential) error {
	if cred == nil {
		return &AccountInvalidError{Field: "credential", Message: "must not be nil"}
	}
	canonical, ok := mode.Canonical()
	if !ok {
		return &AccountCredentialModeInvalidError{Mode: string(mode)}
	}

	if canonical == AccountCredentialModeStaticToken {
		if cred.RefreshToken != "" {
			return &AccountStaticTokenRefreshableError{Provider: provider}
		}
		return nil
	}

	if cred.RefreshToken == "" {
		return &AccountRefreshTokenRequiredError{Provider: provider}
	}
	if r.Effective(provider, cred).TokenEndpoint == "" {
		return &AccountProviderEndpointUnresolvedError{Provider: provider}
	}
	return nil
}
