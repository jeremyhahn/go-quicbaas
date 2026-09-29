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
	"strings"
)

// =============================================================================
// Model providers a provider-routed CLI reaches
// =============================================================================
//
// opencode names every model as <provider>/<model> and routes the turn by the
// prefix: "anthropic/claude-…" goes to Anthropic with an Anthropic credential,
// "ollama/review-q8:latest" goes to an ollama server and needs no credential
// at all. So for that CLI the PROVIDER, not the backend, decides two things:
// which models a node can offer, and whether a spawn needs a pooled account.

// ModelProvider is the provider prefix of a provider-routed model reference.
type ModelProvider string

// ModelProviderOllama is a model an ollama server serves.
const ModelProviderOllama ModelProvider = "ollama"

// localModelProviders are the providers whose models run on a server the
// fleet operates, reached with no provider account.
var localModelProviders = map[ModelProvider]bool{
	ModelProviderOllama: true,
}

// providerRoutedBackends are the backends that route a turn by its model's
// provider prefix. For any other backend a "provider/" prefix is just part of
// a model name and says nothing about credentials.
var providerRoutedBackends = map[string]bool{
	"opencode": true,
}

// ModelProviderOf returns the provider a model reference names, or empty when
// it names none.
func ModelProviderOf(model string) ModelProvider {
	provider, _, found := strings.Cut(strings.TrimSpace(model), "/")
	if !found {
		return ""
	}
	return ModelProvider(strings.ToLower(strings.TrimSpace(provider)))
}

// Local reports whether the provider's models run on a server the fleet
// operates, so a spawn on one needs no provider account.
func (p ModelProvider) Local() bool { return localModelProviders[p] }

// Ref names model on this provider: "<provider>/<model>". A model already
// carrying this provider's prefix is returned as it is; any other name —
// including one with slashes of its own, which ollama allows — is prefixed.
func (p ModelProvider) Ref(model string) string {
	name := strings.TrimSpace(model)
	prefix := string(p) + "/"
	if strings.HasPrefix(strings.ToLower(name), prefix) {
		return prefix + name[len(prefix):]
	}
	return prefix + name
}

// SpawnNeedsNoAccount reports whether a spawn of backend on model runs on a
// local provider and so needs no pooled provider account.
func SpawnNeedsNoAccount(backend, model string) bool {
	return providerRoutedBackends[backend] && ModelProviderOf(model).Local()
}

// ACPModelProvider is a model server a provider-routed CLI on this node can
// reach: its models join the backend's catalog as <ID>/<model>, which is what
// the node reports through the backend models endpoint.
type ACPModelProvider struct {
	// ID is the provider prefix the CLI names the server's models by.
	ID ModelProvider

	// Models lists what the server serves now.
	Models ModelLister
}

// ModelProviderUnavailableError reports a provider whose model listing could
// not be read, so the backend cannot say which models it offers.
type ModelProviderUnavailableError struct {
	Backend  string
	Provider ModelProvider
	Cause    error
}

// Error implements the error interface.
func (e *ModelProviderUnavailableError) Error() string {
	msg := "orchestration: backend " + e.Backend + " cannot list the models of provider " + string(e.Provider)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the cause.
func (e *ModelProviderUnavailableError) Unwrap() error { return e.Cause }

// providerModels lists every provider's models as <ID>/<model>.
func providerModels(ctx context.Context, backend string, providers []ACPModelProvider) ([]string, error) {
	var models []string
	for _, provider := range providers {
		if provider.Models == nil {
			continue
		}
		served, err := provider.Models.ListModels(ctx)
		if err != nil {
			return nil, &ModelProviderUnavailableError{Backend: backend, Provider: provider.ID, Cause: err}
		}
		for _, name := range served {
			if name = strings.TrimSpace(name); name != "" {
				models = append(models, string(provider.ID)+"/"+name)
			}
		}
	}
	return models, nil
}
