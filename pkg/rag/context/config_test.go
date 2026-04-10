//go:build rag

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

package context

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.DefaultMaxTokens != 8000 {
		t.Errorf("DefaultMaxTokens = %d, want 8000", config.DefaultMaxTokens)
	}

	if config.DefaultTemplate != TemplateMarkdown {
		t.Errorf("DefaultTemplate = %s, want %s", config.DefaultTemplate, TemplateMarkdown)
	}

	if !config.IncludeMetadata {
		t.Error("IncludeMetadata should be true by default")
	}

	if !config.IncludeCitations {
		t.Error("IncludeCitations should be true by default")
	}

	if config.MaxResultsPerChunk != 10 {
		t.Errorf("MaxResultsPerChunk = %d, want 10", config.MaxResultsPerChunk)
	}

	if config.TruncationStrategy != TruncationSmart {
		t.Errorf("TruncationStrategy = %s, want %s", config.TruncationStrategy, TruncationSmart)
	}

	if config.TokenMultiplier != 1.3 {
		t.Errorf("TokenMultiplier = %f, want 1.3", config.TokenMultiplier)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expectOK bool
	}{
		{
			name:     "valid config",
			config:   DefaultConfig(),
			expectOK: true,
		},
		{
			name: "zero max tokens",
			config: Config{
				DefaultMaxTokens: 0,
			},
			expectOK: true, // Should fix to default
		},
		{
			name: "negative max tokens",
			config: Config{
				DefaultMaxTokens: -100,
			},
			expectOK: true, // Should fix to default
		},
		{
			name: "zero max results",
			config: Config{
				MaxResultsPerChunk: 0,
			},
			expectOK: true, // Should fix to default
		},
		{
			name: "zero multiplier",
			config: Config{
				TokenMultiplier: 0,
			},
			expectOK: true, // Should fix to default
		},
		{
			name: "empty template",
			config: Config{
				DefaultTemplate: "",
			},
			expectOK: true, // Should fix to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectOK && err != nil {
				t.Errorf("Validate() returned error: %v", err)
			}

			// Check that invalid values were fixed
			if tt.config.DefaultMaxTokens <= 0 {
				if tt.config.DefaultMaxTokens != 8000 {
					t.Error("DefaultMaxTokens was not fixed")
				}
			}

			if tt.config.MaxResultsPerChunk <= 0 {
				if tt.config.MaxResultsPerChunk != 10 {
					t.Error("MaxResultsPerChunk was not fixed")
				}
			}

			if tt.config.TokenMultiplier <= 0 {
				if tt.config.TokenMultiplier != 1.3 {
					t.Error("TokenMultiplier was not fixed")
				}
			}

			if tt.config.DefaultTemplate == "" {
				if tt.config.DefaultTemplate != TemplateMarkdown {
					t.Error("DefaultTemplate was not fixed")
				}
			}
		})
	}
}

func TestConfig_WithMaxTokens(t *testing.T) {
	config := DefaultConfig().WithMaxTokens(5000)

	if config.DefaultMaxTokens != 5000 {
		t.Errorf("WithMaxTokens() = %d, want 5000", config.DefaultMaxTokens)
	}
}

func TestConfig_WithTemplate(t *testing.T) {
	config := DefaultConfig().WithTemplate(TemplateJSON)

	if config.DefaultTemplate != TemplateJSON {
		t.Errorf("WithTemplate() = %s, want %s", config.DefaultTemplate, TemplateJSON)
	}
}

func TestConfig_WithMetadata(t *testing.T) {
	config := DefaultConfig().WithMetadata(false)

	if config.IncludeMetadata {
		t.Error("WithMetadata(false) did not set IncludeMetadata to false")
	}

	config = config.WithMetadata(true)
	if !config.IncludeMetadata {
		t.Error("WithMetadata(true) did not set IncludeMetadata to true")
	}
}

func TestConfig_WithCitations(t *testing.T) {
	config := DefaultConfig().WithCitations(false)

	if config.IncludeCitations {
		t.Error("WithCitations(false) did not set IncludeCitations to false")
	}

	config = config.WithCitations(true)
	if !config.IncludeCitations {
		t.Error("WithCitations(true) did not set IncludeCitations to true")
	}
}

func TestConfig_WithTruncationStrategy(t *testing.T) {
	tests := []TruncationStrategy{
		TruncationSmart,
		TruncationHard,
		TruncationNone,
	}

	for _, strategy := range tests {
		t.Run(string(strategy), func(t *testing.T) {
			config := DefaultConfig().WithTruncationStrategy(strategy)

			if config.TruncationStrategy != strategy {
				t.Errorf("WithTruncationStrategy() = %s, want %s",
					config.TruncationStrategy, strategy)
			}
		})
	}
}

func TestConfig_Chaining(t *testing.T) {
	// Test method chaining
	config := DefaultConfig().
		WithMaxTokens(10000).
		WithTemplate(TemplateJSON).
		WithMetadata(false).
		WithCitations(false).
		WithTruncationStrategy(TruncationHard)

	if config.DefaultMaxTokens != 10000 {
		t.Errorf("MaxTokens = %d, want 10000", config.DefaultMaxTokens)
	}

	if config.DefaultTemplate != TemplateJSON {
		t.Errorf("Template = %s, want JSON", config.DefaultTemplate)
	}

	if config.IncludeMetadata {
		t.Error("IncludeMetadata should be false")
	}

	if config.IncludeCitations {
		t.Error("IncludeCitations should be false")
	}

	if config.TruncationStrategy != TruncationHard {
		t.Errorf("TruncationStrategy = %s, want hard", config.TruncationStrategy)
	}
}

func TestConfig_ValidatePreservesValidValues(t *testing.T) {
	config := Config{
		DefaultMaxTokens:   5000,
		DefaultTemplate:    TemplateXML,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    2.0,
		TruncationStrategy: TruncationNone,
	}

	err := config.Validate()
	if err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}

	// All valid values should be preserved
	if config.DefaultMaxTokens != 5000 {
		t.Error("Valid DefaultMaxTokens was changed")
	}

	if config.DefaultTemplate != TemplateXML {
		t.Error("Valid DefaultTemplate was changed")
	}

	if config.MaxResultsPerChunk != 20 {
		t.Error("Valid MaxResultsPerChunk was changed")
	}

	if config.TokenMultiplier != 2.0 {
		t.Error("Valid TokenMultiplier was changed")
	}

	if config.TruncationStrategy != TruncationNone {
		t.Error("Valid TruncationStrategy was changed")
	}
}

func TestConfig_AllTemplates(t *testing.T) {
	templates := []Template{
		TemplateMarkdown,
		TemplateJSON,
		TemplateXML,
		TemplatePlain,
	}

	for _, tmpl := range templates {
		t.Run(string(tmpl), func(t *testing.T) {
			config := DefaultConfig().WithTemplate(tmpl)
			if config.DefaultTemplate != tmpl {
				t.Errorf("Template not set correctly: got %s, want %s",
					config.DefaultTemplate, tmpl)
			}
		})
	}
}

func TestConfig_AllStrategies(t *testing.T) {
	strategies := []TruncationStrategy{
		TruncationSmart,
		TruncationHard,
		TruncationNone,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			config := DefaultConfig().WithTruncationStrategy(strategy)
			if config.TruncationStrategy != strategy {
				t.Errorf("Strategy not set correctly: got %s, want %s",
					config.TruncationStrategy, strategy)
			}
		})
	}
}
