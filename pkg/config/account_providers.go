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

package config

import (
	"fmt"
	"net/url"
)

// =============================================================================
// orchestration.account_providers — operator-defined provider defaults for
// pooled agent accounts.
//
// QuicBaaS ships built-in descriptors for the Anthropic (claude), OpenAI
// (codex), and OpenCode providers. Neither OAuth-capable built-in ships a
// token endpoint or client id, because neither vendor documents the OAuth
// application their CLI authenticates with. This section is how an operator
// supplies those values once for a whole provider instead of repeating them on
// every pooled account, and how a provider QuicBaaS does not know about is
// added.
//
// Per-account credential fields always win: a defaulted value here fills in
// only what an account left empty. See pkg/orchestration/account_provider_registry.go
// and docs/orchestration/accounts.md.
//
//	orchestration:
//	  account_providers:
//	    - name: claude               # overrides a built-in
//	      override: true
//	      token_endpoint: https://auth.example.com/v1/oauth/token
//	      client_id: 9d1c...
//	    - name: acme-llm             # brand new provider
//	      display_name: ACME LLM
//	      token_endpoint: https://id.acme.example/oauth2/token
//	      access_token_env_key: ACME_LLM_TOKEN
//	      scopes: [inference]
// =============================================================================

// builtinAccountProviderNames lists the providers QuicBaaS ships descriptors
// for. It exists here only so configuration can be validated without importing
// pkg/orchestration — that package already depends on this one, so the import
// cannot run the other way. pkg/orchestration owns the descriptors themselves
// and TestBuiltinAccountProvidersMatchConfig asserts the two stay in step.
var builtinAccountProviderNames = map[string]struct{}{
	"claude":   {},
	"codex":    {},
	"opencode": {},
}

// IsBuiltinAccountProvider reports whether name is a provider QuicBaaS ships a
// built-in descriptor for.
func IsBuiltinAccountProvider(name string) bool {
	_, ok := builtinAccountProviderNames[name]
	return ok
}

// BuiltinAccountProviderNames returns the built-in provider names, unordered.
func BuiltinAccountProviderNames() []string {
	names := make([]string, 0, len(builtinAccountProviderNames))
	for name := range builtinAccountProviderNames {
		names = append(names, name)
	}
	return names
}

// AccountProviderConfig defines, or overrides, the OAuth defaults for one
// pooled-account provider.
type AccountProviderConfig struct {
	// Name is the provider identifier accounts are created against
	// (orchestration.AccountProvider). Required.
	Name string `mapstructure:"name"`

	// DisplayName is an operator-facing label. Optional.
	DisplayName string `mapstructure:"display_name"`

	// Override must be true when Name matches a built-in provider. Without it
	// a configuration entry that shadows a shipped descriptor is rejected, so
	// a typo'd or copy-pasted name can never silently redirect an existing
	// provider's accounts at a different token endpoint.
	Override bool `mapstructure:"override"`

	// TokenEndpoint is the provider-wide RFC 6749 token endpoint. When set it
	// must be an absolute https URL: the refresh grant presents a long-lived
	// refresh token, which must never traverse a cleartext connection.
	TokenEndpoint string `mapstructure:"token_endpoint"`

	// ClientID is the provider-wide OAuth2 client identifier. Optional.
	ClientID string `mapstructure:"client_id"`

	// Scopes is the provider-wide default scope list. Optional.
	Scopes []string `mapstructure:"scopes"`

	// AccessTokenEnvKey is the environment variable an account's resolved
	// access token is projected into when the account names none itself.
	// Optional.
	AccessTokenEnvKey string `mapstructure:"access_token_env_key"`
}

// validateAccountProviders validates the orchestration.account_providers
// section. It is called from validateOrchestration.
func (v *Validator) validateAccountProviders(providers []AccountProviderConfig) {
	seen := make(map[string]int, len(providers))
	for i, provider := range providers {
		field := fmt.Sprintf("orchestration.account_providers[%d]", i)

		if provider.Name == "" {
			v.addError(field+".name", "must not be empty")
			continue
		}
		if first, duplicate := seen[provider.Name]; duplicate {
			v.addError(field+".name",
				fmt.Sprintf("duplicate provider %q (already defined at index %d)", provider.Name, first))
			continue
		}
		seen[provider.Name] = i

		builtin := IsBuiltinAccountProvider(provider.Name)
		switch {
		case builtin && !provider.Override:
			v.addError(field+".override",
				fmt.Sprintf("%q is a built-in provider: set override: true to change its defaults", provider.Name))
		case !builtin && provider.Override:
			v.addError(field+".override",
				fmt.Sprintf("override is set but %q is not a built-in provider (built-ins: claude, codex, opencode)", provider.Name))
		}

		if provider.TokenEndpoint != "" {
			v.validateAccountProviderEndpoint(field+".token_endpoint", provider.TokenEndpoint)
		}

		// An entry that supplies no default at all changes nothing, which is
		// far more likely to be a mistake (a mistyped key, a half-finished
		// edit) than an intention.
		if !builtin && provider.TokenEndpoint == "" && provider.ClientID == "" &&
			provider.AccessTokenEnvKey == "" && len(provider.Scopes) == 0 {
			v.addError(field,
				fmt.Sprintf("provider %q defines no defaults: set at least one of token_endpoint, client_id, scopes, access_token_env_key", provider.Name))
		}
	}
}

// validateAccountProviderEndpoint enforces that a configured token endpoint is
// an absolute https URL with a host. A relative or http endpoint would either
// never be reachable or would put a refresh token on the wire in cleartext.
func (v *Validator) validateAccountProviderEndpoint(field, endpoint string) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		v.addError(field, fmt.Sprintf("must be a valid URL: %v", err))
		return
	}
	if !parsed.IsAbs() {
		v.addError(field, fmt.Sprintf("must be an absolute URL: %q", endpoint))
		return
	}
	if parsed.Scheme != "https" {
		v.addError(field, fmt.Sprintf("must use https (got %q): a refresh token must never traverse a cleartext connection", parsed.Scheme))
		return
	}
	if parsed.Host == "" {
		v.addError(field, fmt.Sprintf("must include a host: %q", endpoint))
	}
}
