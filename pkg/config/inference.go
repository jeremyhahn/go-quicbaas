// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// InferenceConfig configures the daemon's API-based inference providers
// (Anthropic, OpenAI, xAI, Mistral, ...). The daemon iterates Providers on
// boot and, for each enabled entry, calls inference.Create followed by
// AgentService.RegisterAPIInferenceBackend.
//
// This struct is intentionally vendor-neutral — provider-specific tunables
// live in ProviderConfig.Metadata so adding a provider never requires
// touching this file.
type InferenceConfig struct {
	// Providers is the list of configured provider instances. May be empty,
	// in which case the daemon runs with zero API inference backends.
	Providers []ProviderConfig `mapstructure:"providers" yaml:"providers" json:"providers"`
}

// ProviderConfig configures one API inference provider instance. The same
// provider type (e.g. "anthropic") may appear multiple times under different
// Names to support multiple accounts, regions, or environments.
type ProviderConfig struct {
	// Name is the logical backend name advertised to clients (e.g.
	// "anthropic-prod", "openai-staging"). Required, must be unique across
	// the configuration.
	Name string `mapstructure:"name" yaml:"name" json:"name"`

	// Provider is the provider type and MUST match a factory registered
	// with inference.Default (e.g. "anthropic", "openai", "xai"). Required.
	Provider string `mapstructure:"provider" yaml:"provider" json:"provider"`

	// BaseURL overrides the provider's default endpoint. Optional.
	BaseURL string `mapstructure:"base_url" yaml:"base_url" json:"base_url,omitempty"`

	// APIKeyRef is an indirect reference to the credential in the secrets
	// engine. The factory resolves it at construction time; this struct
	// never contains the raw key.
	APIKeyRef inference.SecretRef `mapstructure:"api_key_ref" yaml:"api_key_ref" json:"api_key_ref"`

	// DefaultModel is used by callers that do not specify a model. Optional.
	DefaultModel string `mapstructure:"default_model" yaml:"default_model" json:"default_model,omitempty"`

	// Models optionally overrides the model catalog reported to clients.
	// When empty, the adapter's native ListModels is used.
	Models []string `mapstructure:"models" yaml:"models" json:"models,omitempty"`

	// Metadata carries provider-specific tunables (e.g. organization ID,
	// region, request timeout). Adapter contracts define accepted keys.
	Metadata map[string]string `mapstructure:"metadata" yaml:"metadata" json:"metadata,omitempty"`

	// Enabled gates whether the daemon constructs and registers this
	// provider on boot. Disabled entries are ignored entirely.
	Enabled bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
}

// DefaultInferenceConfig returns a zero-provider configuration. The daemon
// boots with zero API inference backends until operators populate the
// Providers list and register the corresponding adapters.
func DefaultInferenceConfig() InferenceConfig {
	return InferenceConfig{
		Providers: nil,
	}
}

// Validate checks the configuration for structural errors. It does NOT
// dispatch to the inference registry — that happens at construction time
// in the daemon. Provider-name uniqueness is enforced here so misconfigured
// duplicates fail fast at boot.
func (c *InferenceConfig) Validate() error {
	seen := make(map[string]struct{}, len(c.Providers))
	for i := range c.Providers {
		p := &c.Providers[i]
		if !p.Enabled {
			continue
		}
		if p.Name == "" {
			return &InvalidInferenceConfigError{Index: i, Field: "name", Reason: "must not be empty"}
		}
		if p.Provider == "" {
			return &InvalidInferenceConfigError{Index: i, Field: "provider", Reason: "must not be empty"}
		}
		if _, dup := seen[p.Name]; dup {
			return &InvalidInferenceConfigError{Index: i, Field: "name", Reason: "duplicate provider name: " + p.Name}
		}
		seen[p.Name] = struct{}{}
	}
	return nil
}

// ToInferenceConfig projects a ProviderConfig into the inference.Config
// shape consumed by inference.Registry.Create. Convenience method so
// daemons do not have to copy fields by hand.
func (p *ProviderConfig) ToInferenceConfig() inference.Config {
	return inference.Config{
		Provider:  p.Provider,
		BaseURL:   p.BaseURL,
		APIKeyRef: p.APIKeyRef,
		Models:    p.Models,
		Metadata:  p.Metadata,
	}
}
