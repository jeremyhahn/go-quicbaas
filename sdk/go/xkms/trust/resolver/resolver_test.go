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

package resolver_test

import (
	"context"
	"errors"
	"testing"

	xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"
)

// compile-time alias assertion: the re-exported Resolver interface must be
// assignable from a concrete implementation returned by New.
var _ xkmsresolver.Resolver = (xkmsresolver.Resolver)(nil)

// TestStrategyConstants verifies that the re-exported strategy constants carry
// the expected string values. Any rename upstream becomes a compile error here
// because the constants are aliases; this test guards the human-readable wire
// values used in YAML.
func TestStrategyConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		got      xkmsresolver.Strategy
		expected string
	}{
		{"dane", xkmsresolver.StrategyDANE, "dane"},
		{"noise", xkmsresolver.StrategyNoise, "noise"},
		{"spki", xkmsresolver.StrategySPKI, "spki"},
		{"embedded", xkmsresolver.StrategyEmbedded, "embedded"},
		{"ca-file", xkmsresolver.StrategyCAFile, "ca-file"},
		{"provider-ca", xkmsresolver.StrategyProviderCA, "provider-ca"},
		{"tofu", xkmsresolver.StrategyTOFU, "tofu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if string(tc.got) != tc.expected {
				t.Errorf("strategy %s: got %q, want %q", tc.name, tc.got, tc.expected)
			}
		})
	}
}

// TestNewWithNilConfig verifies that New returns ErrNilConfig when a nil config
// is passed.
func TestNewWithNilConfig(t *testing.T) {
	t.Parallel()
	_, err := xkmsresolver.New(nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	var nilErr xkmsresolver.ErrNilConfig
	if !errors.As(err, &nilErr) {
		t.Errorf("expected ErrNilConfig, got %T: %v", err, err)
	}
}

// TestNewWithEmptyStrategies verifies that New returns ErrInvalidConfig when a
// scope is configured with an empty strategy list.
func TestNewWithEmptyStrategies(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{},
			},
		},
	}
	_, err := xkmsresolver.New(cfg)
	if err == nil {
		t.Fatal("expected error for empty strategies, got nil")
	}
	var invalidErr *xkmsresolver.ErrInvalidConfig
	if !errors.As(err, &invalidErr) {
		t.Errorf("expected *ErrInvalidConfig, got %T: %v", err, err)
	}
}

// TestDefaultConfig verifies that DefaultConfig returns a non-nil config with
// the standard chain (dane, noise, spki) for the default scope and that TOFU
// is explicitly absent from the Go-level default.
func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := xkmsresolver.DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	scope, ok := cfg.Scopes["default"]
	if !ok || scope == nil {
		t.Fatal("DefaultConfig has no 'default' scope")
	}
	want := []xkmsresolver.Strategy{
		xkmsresolver.StrategyDANE,
		xkmsresolver.StrategyNoise,
		xkmsresolver.StrategySPKI,
	}
	if len(scope.Strategies) != len(want) {
		t.Fatalf("default strategies len = %d, want %d", len(scope.Strategies), len(want))
	}
	for i, s := range scope.Strategies {
		if s != want[i] {
			t.Errorf("strategies[%d] = %q, want %q", i, s, want[i])
		}
	}
	// TOFU must not be in the Go-level default.
	if scope.HasStrategy(xkmsresolver.StrategyTOFU) {
		t.Error("TOFU must not appear in Go-level DefaultConfig (only in explicit YAML templates)")
	}
}

// TestNewWithEmbeddedStrategy verifies that a resolver with only the embedded
// strategy can be constructed and that TLSConfig resolves successfully using
// the OS trust store (no network call required).
func TestNewWithEmbeddedStrategy(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyEmbedded},
			},
		},
	}
	r, err := xkmsresolver.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tlsCfg, err := r.TLSConfig(context.Background(), "default", "")
	if err != nil {
		t.Fatalf("TLSConfig with embedded strategy: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("TLSConfig returned nil")
	}
}

// TestTOFURequiresPinStore verifies that constructing a resolver with the tofu
// strategy but without WithTOFUPinStore returns ErrTOFUPinStoreRequired.
func TestTOFURequiresPinStore(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyTOFU},
				ServerURL:  "https://kms.example.com:8443",
			},
		},
	}
	_, err := xkmsresolver.New(cfg)
	if err == nil {
		t.Fatal("expected ErrTOFUPinStoreRequired, got nil")
	}
	var tofuErr xkmsresolver.ErrTOFUPinStoreRequired
	if !errors.As(err, &tofuErr) {
		t.Errorf("expected ErrTOFUPinStoreRequired, got %T: %v", err, err)
	}
}

// TestScopeNotConfigured verifies that resolving an unknown scope returns
// ErrScopeNotConfigured.
func TestScopeNotConfigured(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyEmbedded},
			},
		},
	}
	r, err := xkmsresolver.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.TLSConfig(context.Background(), "nonexistent", "")
	if err == nil {
		t.Fatal("expected ErrScopeNotConfigured, got nil")
	}
	var notCfgErr *xkmsresolver.ErrScopeNotConfigured
	if !errors.As(err, &notCfgErr) {
		t.Errorf("expected *ErrScopeNotConfigured, got %T: %v", err, err)
	}
}

// TestInstallToSystemTrustEmptyBundle verifies that InstallToSystemTrust
// returns ErrCABundleEmpty for an empty PEM.
func TestInstallToSystemTrustEmptyBundle(t *testing.T) {
	t.Parallel()
	err := xkmsresolver.InstallToSystemTrust(nil, "/tmp/test-ca.pem")
	if err == nil {
		t.Fatal("expected error for empty bundle, got nil")
	}
	var emptyErr xkmsresolver.ErrCABundleEmpty
	if !errors.As(err, &emptyErr) {
		t.Errorf("expected ErrCABundleEmpty, got %T: %v", err, err)
	}
}

// TestWithLoggerOption verifies that passing WithLogger does not panic and
// that a resolver constructed with it behaves correctly.
func TestWithLoggerOption(t *testing.T) {
	t.Parallel()
	cfg := &xkmsresolver.Config{
		Scopes: map[string]*xkmsresolver.ScopeConfig{
			"default": {
				Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyEmbedded},
			},
		},
	}
	r, err := xkmsresolver.New(cfg, xkmsresolver.WithLogger(nil))
	if err != nil {
		t.Fatalf("New with nil logger: %v", err)
	}
	if r == nil {
		t.Fatal("resolver is nil")
	}
}
