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
	"strings"
	"testing"
)

// validateProviders runs only the account-provider section of the validator
// and returns the messages it produced.
func validateProviders(t *testing.T, providers []AccountProviderConfig) []string {
	t.Helper()
	v := NewValidator()
	v.validateAccountProviders(providers)
	err := v.Errors()
	if err == nil {
		return nil
	}
	return strings.Split(err.Error(), "\n")
}

// requireNoProviderErrors fails when the validator rejected a section it
// should have accepted.
func requireNoProviderErrors(t *testing.T, providers []AccountProviderConfig) {
	t.Helper()
	if messages := validateProviders(t, providers); len(messages) != 0 {
		t.Fatalf("validateAccountProviders() = %v, want no errors", messages)
	}
}

// requireProviderError fails unless some validator message mentions want.
func requireProviderError(t *testing.T, providers []AccountProviderConfig, want string) {
	t.Helper()
	messages := validateProviders(t, providers)
	for _, message := range messages {
		if strings.Contains(message, want) {
			return
		}
	}
	t.Fatalf("validateAccountProviders() = %v, want an error containing %q", messages, want)
}

func TestValidateAccountProviders_Accepts(t *testing.T) {
	t.Run("empty section", func(t *testing.T) {
		requireNoProviderErrors(t, nil)
	})

	t.Run("built-in override", func(t *testing.T) {
		requireNoProviderErrors(t, []AccountProviderConfig{{
			Name:          "claude",
			Override:      true,
			TokenEndpoint: "https://auth.example.com/v1/oauth/token",
			ClientID:      "client-abc",
		}})
	})

	t.Run("custom provider", func(t *testing.T) {
		requireNoProviderErrors(t, []AccountProviderConfig{{
			Name:              "acme-llm",
			DisplayName:       "ACME LLM",
			TokenEndpoint:     "https://id.acme.example/oauth2/token",
			AccessTokenEnvKey: "ACME_LLM_TOKEN",
			Scopes:            []string{"inference"},
		}})
	})

	t.Run("custom provider with only an env key", func(t *testing.T) {
		requireNoProviderErrors(t, []AccountProviderConfig{{
			Name:              "acme-llm",
			AccessTokenEnvKey: "ACME_LLM_TOKEN",
		}})
	})
}

func TestValidateAccountProviders_Rejects(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{TokenEndpoint: "https://id.example/token"}},
			"account_providers[0].name")
	})

	t.Run("duplicate name", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{
			{Name: "acme-llm", TokenEndpoint: "https://a.example/token"},
			{Name: "acme-llm", TokenEndpoint: "https://b.example/token"},
		}, "duplicate provider")
	})

	t.Run("shadows a built-in without override", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "claude",
			TokenEndpoint: "https://evil.example/token",
		}}, "set override: true")
	})

	t.Run("override of a provider that is not built in", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "acme-llm",
			Override:      true,
			TokenEndpoint: "https://id.acme.example/token",
		}}, "not a built-in provider")
	})

	t.Run("relative token endpoint", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "acme-llm",
			TokenEndpoint: "/oauth2/token",
		}}, "must be an absolute URL")
	})

	t.Run("http token endpoint", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "acme-llm",
			TokenEndpoint: "http://id.acme.example/token",
		}}, "must use https")
	})

	t.Run("token endpoint with no host", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "acme-llm",
			TokenEndpoint: "https:///token",
		}}, "must include a host")
	})

	t.Run("unparseable token endpoint", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{
			Name:          "acme-llm",
			TokenEndpoint: "https://%zz",
		}}, "must be a valid URL")
	})

	t.Run("custom provider defining nothing", func(t *testing.T) {
		requireProviderError(t, []AccountProviderConfig{{Name: "acme-llm"}}, "defines no defaults")
	})
}

// TestValidateAccountProviders_ReachedThroughValidate proves the section is
// actually reached by the top-level orchestration validation rather than only
// being callable directly.
func TestValidateAccountProviders_ReachedThroughValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Orchestration.AccountProviders = []AccountProviderConfig{{
		Name:          "claude",
		TokenEndpoint: "http://insecure.example/token",
	}}

	v := NewValidator()
	v.validateOrchestration(&cfg.Orchestration)
	err := v.Errors()
	if err == nil {
		t.Fatalf("validateOrchestration() error = nil, want the account_providers rejections")
	}
	for _, want := range []string{"set override: true", "must use https"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validateOrchestration() error = %v, want it to contain %q", err, want)
		}
	}
}

func TestIsBuiltinAccountProvider(t *testing.T) {
	for _, name := range BuiltinAccountProviderNames() {
		if !IsBuiltinAccountProvider(name) {
			t.Fatalf("IsBuiltinAccountProvider(%q) = false, want true", name)
		}
	}
	if IsBuiltinAccountProvider("acme-llm") {
		t.Fatalf("IsBuiltinAccountProvider(acme-llm) = true, want false")
	}
	if len(BuiltinAccountProviderNames()) != 3 {
		t.Fatalf("BuiltinAccountProviderNames() = %v, want three entries", BuiltinAccountProviderNames())
	}
}
