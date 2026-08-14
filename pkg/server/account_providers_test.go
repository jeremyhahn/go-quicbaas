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

package server

import (
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAccountProviderDescriptors_TranslatesConfiguration covers the composition
// root's only job here: mapping configured providers onto domain descriptors.
func TestAccountProviderDescriptors_TranslatesConfiguration(t *testing.T) {
	got := accountProviderDescriptors([]config.AccountProviderConfig{
		{
			Name:          "claude",
			Override:      true,
			TokenEndpoint: "https://auth.example/token",
			ClientID:      "client-abc",
		},
		{
			Name:              "acme-llm",
			DisplayName:       "ACME LLM",
			TokenEndpoint:     "https://id.acme.example/oauth2/token",
			AccessTokenEnvKey: "ACME_LLM_TOKEN",
			Scopes:            []string{"inference"},
		},
	})

	want := []orchestration.ProviderDescriptor{
		{
			Provider:      orchestration.AccountProviderClaude,
			TokenEndpoint: "https://auth.example/token",
			ClientID:      "client-abc",
		},
		{
			Provider:          "acme-llm",
			DisplayName:       "ACME LLM",
			TokenEndpoint:     "https://id.acme.example/oauth2/token",
			AccessTokenEnvKey: "ACME_LLM_TOKEN",
			Scopes:            []string{"inference"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accountProviderDescriptors() = %+v, want %+v", got, want)
	}
}

// TestAccountProviderDescriptors_Empty proves an unconfigured section produces
// no overrides, leaving the built-in registry exactly as shipped.
func TestAccountProviderDescriptors_Empty(t *testing.T) {
	descriptors := accountProviderDescriptors(nil)
	if len(descriptors) != 0 {
		t.Fatalf("accountProviderDescriptors(nil) = %+v, want empty", descriptors)
	}

	registry, err := orchestration.NewAccountProviderRegistry(descriptors...)
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	if !reflect.DeepEqual(registry.Providers(), orchestration.DefaultAccountProviderRegistry().Providers()) {
		t.Fatalf("an empty configuration changed the built-in provider set")
	}
}

// TestAccountProviderDescriptors_FeedRefresherRegistration ties the two halves
// of the server wiring together without standing up a server: a configured
// provider ends up with a refresher registered on the token manager, which is
// what makes an oauth2 account renewable at spawn.
func TestAccountProviderDescriptors_FeedRefresherRegistration(t *testing.T) {
	registry, err := orchestration.NewAccountProviderRegistry(accountProviderDescriptors(
		[]config.AccountProviderConfig{{
			Name:          "acme-llm",
			TokenEndpoint: "https://id.acme.example/oauth2/token",
		}})...)
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}

	tokens := orchestration.NewAccountTokenManager(nil, nil, 0, orchestration.WithAccountProviders(registry))
	registered := tokens.RegisterOAuthRefreshers(
		orchestration.NewOAuth2TokenRefresher(orchestration.OAuth2RefresherConfig{}))

	want := []orchestration.AccountProvider{
		"acme-llm",
		orchestration.AccountProviderClaude,
		orchestration.AccountProviderCodex,
	}
	if !reflect.DeepEqual(registered, want) {
		t.Fatalf("RegisterOAuthRefreshers() = %v, want %v", registered, want)
	}
	for _, provider := range want {
		if _, ok := tokens.Refresher(provider); !ok {
			t.Fatalf("no refresher registered for %q", provider)
		}
	}
}
