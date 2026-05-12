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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

func TestDefaultInferenceConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultInferenceConfig()
	if len(cfg.Providers) != 0 {
		t.Errorf("default Providers length = %d, want 0", len(cfg.Providers))
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config must validate cleanly, got %v", err)
	}
}

func TestInferenceConfig_Validate_OK(t *testing.T) {
	t.Parallel()

	cfg := InferenceConfig{
		Providers: []ProviderConfig{
			{
				Name:     "anthropic-prod",
				Provider: "anthropic",
				Enabled:  true,
				APIKeyRef: inference.SecretRef{
					Engine: "secrets", Path: "tenants/x/inference/anthropic", Key: "api_key",
				},
			},
			{
				Name:     "anthropic-staging-disabled",
				Provider: "anthropic",
				Enabled:  false, // disabled — not validated
			},
			{
				Name:     "openai-prod",
				Provider: "openai",
				Enabled:  true,
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected clean validation, got %v", err)
	}
}

func TestInferenceConfig_Validate_EmptyName(t *testing.T) {
	t.Parallel()

	cfg := InferenceConfig{
		Providers: []ProviderConfig{
			{Name: "", Provider: "anthropic", Enabled: true},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	var typed *InvalidInferenceConfigError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *InvalidInferenceConfigError, got %T", err)
	}
	if typed.Field != "name" {
		t.Errorf("Field = %q, want %q", typed.Field, "name")
	}
	if !errors.Is(err, ErrInvalidInferenceConfig) {
		t.Error("errors.Is should match ErrInvalidInferenceConfig")
	}
}

func TestInferenceConfig_Validate_EmptyProvider(t *testing.T) {
	t.Parallel()

	cfg := InferenceConfig{
		Providers: []ProviderConfig{
			{Name: "x", Provider: "", Enabled: true},
		},
	}
	err := cfg.Validate()
	var typed *InvalidInferenceConfigError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *InvalidInferenceConfigError, got %T: %v", err, err)
	}
	if typed.Field != "provider" {
		t.Errorf("Field = %q, want %q", typed.Field, "provider")
	}
}

func TestInferenceConfig_Validate_DuplicateName(t *testing.T) {
	t.Parallel()

	cfg := InferenceConfig{
		Providers: []ProviderConfig{
			{Name: "dup", Provider: "anthropic", Enabled: true},
			{Name: "dup", Provider: "openai", Enabled: true},
		},
	}
	err := cfg.Validate()
	var typed *InvalidInferenceConfigError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *InvalidInferenceConfigError, got %T: %v", err, err)
	}
	if typed.Index != 1 {
		t.Errorf("Index = %d, want 1", typed.Index)
	}
}

func TestInferenceConfig_Validate_DisabledEntriesAreIgnored(t *testing.T) {
	t.Parallel()

	// Disabled entries with otherwise-invalid fields must NOT fail validation
	// — operators frequently leave half-filled disabled rows around while
	// rotating credentials.
	cfg := InferenceConfig{
		Providers: []ProviderConfig{
			{Name: "", Provider: "", Enabled: false},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled entries should not validate, got %v", err)
	}
}

func TestProviderConfig_ToInferenceConfig(t *testing.T) {
	t.Parallel()

	pc := ProviderConfig{
		Name:     "anthropic-prod",
		Provider: "anthropic",
		BaseURL:  "https://api.anthropic.com",
		APIKeyRef: inference.SecretRef{
			Engine: "secrets",
			Path:   "tenants/x/inference/anthropic",
			Key:    "api_key",
		},
		Models:   []string{"claude-3-5-sonnet"},
		Metadata: map[string]string{"organization": "acme"},
	}
	got := pc.ToInferenceConfig()

	if got.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", got.Provider, "anthropic")
	}
	if got.BaseURL != "https://api.anthropic.com" {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, "https://api.anthropic.com")
	}
	if got.APIKeyRef.Path != "tenants/x/inference/anthropic" {
		t.Errorf("APIKeyRef.Path = %q", got.APIKeyRef.Path)
	}
	if len(got.Models) != 1 || got.Models[0] != "claude-3-5-sonnet" {
		t.Errorf("Models = %v, want [claude-3-5-sonnet]", got.Models)
	}
	if got.Metadata["organization"] != "acme" {
		t.Errorf("Metadata[organization] = %q, want %q", got.Metadata["organization"], "acme")
	}
}
