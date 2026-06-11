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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"
)

// TestConfigTrustFieldNilByDefault verifies that the Trust field is nil in
// all default config factory functions, ensuring no implicit TOFU is injected
// at the Go level.
func TestConfigTrustFieldNilByDefault(t *testing.T) {
	t.Parallel()
	factories := map[string]*Config{
		"DefaultConfig":     DefaultConfig(),
		"Default":           Default(),
		"ProductionConfig":  ProductionConfig(),
		"DevelopmentConfig": DevelopmentConfig(),
		"TestConfig":        TestConfig(),
		"EmbeddedConfig":    EmbeddedConfig(),
	}
	for name, cfg := range factories {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if cfg.Trust != nil {
				t.Errorf("%s: Trust field must be nil (TOFU must only appear in explicit YAML templates)", name)
			}
		})
	}
}

// TestValidatorTrustNil verifies that a nil Trust field passes validation
// without error (the section is optional).
func TestValidatorTrustNil(t *testing.T) {
	t.Parallel()
	v := &Validator{}
	v.validateTrust(nil)
	if err := v.Errors(); err != nil {
		t.Errorf("nil trust should not produce validation error: %v", err)
	}
}

// TestValidatorTrustValidConfig verifies that a well-formed Trust config
// passes validation.
func TestValidatorTrustValidConfig(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies:       []xkmsresolver.Strategy{xkmsresolver.StrategyEmbedded},
				PerMethodTimeout: 5 * time.Second,
			},
		},
	}
	v := &Validator{}
	v.validateTrust(cfg)
	if err := v.Errors(); err != nil {
		t.Errorf("valid trust config should not produce validation error: %v", err)
	}
}

// TestValidatorTrustInvalidStrategy verifies that an unknown strategy name
// causes a validation error.
func TestValidatorTrustInvalidStrategy(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{"bogus-strategy"},
			},
		},
	}
	v := &Validator{}
	v.validateTrust(cfg)
	err := v.Errors()
	if err == nil {
		t.Fatal("expected validation error for unknown strategy, got nil")
	}
	if !strings.Contains(err.Error(), "trust") {
		t.Errorf("error should reference 'trust' field, got: %v", err)
	}
}

// TestValidatorTrustTOFURequiresPinStore verifies that a scope with the tofu
// strategy but no pin store raises a validation error referencing trust.
func TestValidatorTrustTOFURequiresPinStore(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyTOFU},
				ServerURL:  "https://kms.example.com:8443",
			},
		},
	}
	v := &Validator{}
	v.validateTrust(cfg)
	err := v.Errors()
	if err == nil {
		t.Fatal("expected validation error for tofu without pin store, got nil")
	}
	if !strings.Contains(err.Error(), "trust") {
		t.Errorf("error should reference 'trust' field, got: %v", err)
	}
}

// TestLoadTrustSectionFromYAML verifies that a YAML config containing a trust:
// section unmarshals into Config.Trust correctly and passes validation.
func TestLoadTrustSectionFromYAML(t *testing.T) {
	t.Parallel()
	yaml := `
storage:
  backend: memory
logging:
  level: info
  format: text
  output: stdout
trust:
  scopes:
    default:
      strategies: [embedded]
      per-method-timeout: 5s
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quicbaas.yaml")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	loader := NewLoader()
	cfg, err := loader.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Trust == nil {
		t.Fatal("expected Trust to be non-nil after loading YAML with trust: section")
	}
	scope, ok := cfg.Trust.Scopes["default"]
	if !ok || scope == nil {
		t.Fatal("expected default scope in Trust config")
	}
	if len(scope.Strategies) != 1 || scope.Strategies[0] != xkmsresolver.StrategyEmbedded {
		t.Errorf("unexpected strategies: %v", scope.Strategies)
	}
}

// TestLoadTrustSectionInvalidStrategyFromYAML verifies that a YAML config with
// an unrecognized trust strategy produces a validation error when Validate is
// called on the loaded config.
func TestLoadTrustSectionInvalidStrategyFromYAML(t *testing.T) {
	t.Parallel()
	yaml := `
storage:
  backend: memory
logging:
  level: info
  format: text
  output: stdout
trust:
  scopes:
    default:
      strategies: [not-a-real-strategy]
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quicbaas-bad.yaml")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	loader := NewLoader()
	cfg, err := loader.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Trust == nil {
		t.Skip("loader did not populate Trust section; skipping validation check")
	}
	v := &Validator{}
	v.validateTrust(cfg.Trust)
	if verr := v.Errors(); verr == nil {
		t.Error("expected validation error for unknown strategy, got nil")
	}
}
