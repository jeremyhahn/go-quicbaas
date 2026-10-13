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
	"errors"
	"testing"
)

// TestDefaultsLayer_OneOllamaEndpoint pins that every place the defaults
// layer states an Ollama endpoint states the same one, so the default has a
// single owner and cannot drift between ollama.base_url, the RAG embedding
// default and the embedded fleet node.
func TestDefaultsLayer_OneOllamaEndpoint(t *testing.T) {
	owner := DefaultEmbeddingAPIURL("ollama")
	if owner == "" {
		t.Fatal("the defaults layer states no Ollama endpoint")
	}
	stated := map[string]string{
		"DefaultConfig().Ollama.BaseURL":                        DefaultConfig().Ollama.BaseURL,
		"DefaultConfig().Server.EmbeddedFleetNode.OllamaURL":    DefaultConfig().Server.EmbeddedFleetNode.OllamaURL,
		"DefaultServerConfig().EmbeddedFleetNode.OllamaURL":     DefaultServerConfig().EmbeddedFleetNode.OllamaURL,
		"DefaultOllamaConfig().BaseURL":                         DefaultOllamaConfig().BaseURL,
		"DefaultRAGConfig().Embedding.APIURL (provider ollama)": DefaultRAGConfig().Embedding.APIURL,
	}
	for where, got := range stated {
		if got != owner {
			t.Errorf("%s = %q, want the defaults-layer Ollama endpoint %q", where, got, owner)
		}
	}
}

func TestDefaultEmbeddingAPIURL_EveryBuiltInProviderHasOne(t *testing.T) {
	for _, provider := range []string{"ollama", "openai", "voyage", "lmstudio"} {
		if DefaultEmbeddingAPIURL(provider) == "" {
			t.Errorf("provider %q has no defaults-layer endpoint", provider)
		}
	}
	for _, provider := range []string{"", "npu", "custom"} {
		if got := DefaultEmbeddingAPIURL(provider); got != "" {
			t.Errorf("provider %q has defaults-layer endpoint %q, want none", provider, got)
		}
	}
}

func TestEmbeddingConfig_ResolveAPIURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  EmbeddingConfig
		want string
	}{
		{
			name: "configured endpoint wins over the default",
			cfg:  EmbeddingConfig{Provider: "ollama", APIURL: "http://gpu.example.test:11434"},
			want: "http://gpu.example.test:11434",
		},
		{
			name: "configured endpoint is trimmed",
			cfg:  EmbeddingConfig{Provider: "lmstudio", APIURL: "  http://lm.example.test:1234 "},
			want: "http://lm.example.test:1234",
		},
		{
			name: "empty endpoint takes the provider default",
			cfg:  EmbeddingConfig{Provider: "voyage"},
			want: DefaultEmbeddingAPIURL("voyage"),
		},
		{
			name: "blank endpoint takes the provider default",
			cfg:  EmbeddingConfig{Provider: "openai", APIURL: "   "},
			want: DefaultEmbeddingAPIURL("openai"),
		},
		{
			name: "a custom provider with an endpoint resolves",
			cfg:  EmbeddingConfig{Provider: "npu", APIURL: "http://npu.example.test:8000"},
			want: "http://npu.example.test:8000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolveAPIURL()
			if err != nil {
				t.Fatalf("ResolveAPIURL() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveAPIURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbeddingConfig_ResolveAPIURL_NoEndpointIsTypedError(t *testing.T) {
	for _, provider := range []string{"npu", "custom", ""} {
		cfg := EmbeddingConfig{Provider: provider, APIURL: " "}
		got, err := cfg.ResolveAPIURL()
		if got != "" {
			t.Errorf("provider %q resolved to %q, want nothing", provider, got)
		}
		if !errors.Is(err, ErrUpstreamEndpointMissing) {
			t.Fatalf("provider %q: error = %v, want ErrUpstreamEndpointMissing", provider, err)
		}
		var missing *UpstreamEndpointMissingError
		if !errors.As(err, &missing) {
			t.Fatalf("provider %q: error %T is not *UpstreamEndpointMissingError", provider, err)
		}
		if missing.Key != "rag.embedding.api_url" || missing.Provider != provider {
			t.Errorf("error names key %q provider %q, want rag.embedding.api_url and %q", missing.Key, missing.Provider, provider)
		}
	}
}
