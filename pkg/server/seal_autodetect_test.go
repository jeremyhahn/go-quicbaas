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
	"context"
	"log/slog"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// fakeTPM2Strategy is a minimal SealingStrategy whose ID is tpm2. It lets the
// auto-detect dispatch be exercised without opening a real TPM.
type fakeTPM2Strategy struct{}

func (fakeTPM2Strategy) ID() qrdbsdk.StrategyID { return xkmssdk.StrategyTPM2 }
func (fakeTPM2Strategy) Available() bool        { return true }
func (fakeTPM2Strategy) HardwareBacked() bool   { return true }
func (fakeTPM2Strategy) Close() error           { return nil }
func (fakeTPM2Strategy) SealRootKey(context.Context, []byte, qrdbsdk.Credentials) (*qrdbsdk.SealedRootKey, error) {
	return &qrdbsdk.SealedRootKey{Strategy: xkmssdk.StrategyTPM2}, nil
}
func (fakeTPM2Strategy) UnsealRootKey(context.Context, *qrdbsdk.SealedRootKey, qrdbsdk.Credentials) ([]byte, error) {
	return nil, nil
}
func (fakeTPM2Strategy) BarrierEncryptor(context.Context, []byte) (qrdbsdk.SymmetricEncrypter, error) {
	return nil, nil
}

// withStubbedAutoStrategy swaps tpm2AutoSealStrategy for the duration of a test
// so the OPT-IN auto path can be exercised deterministically without touching a
// TPM. usable=true returns a fake tpm2 strategy; usable=false returns nil
// (no usable TPM -> software fallback).
func withStubbedAutoStrategy(t *testing.T, usable bool) {
	t.Helper()
	prev := tpm2AutoSealStrategy
	tpm2AutoSealStrategy = func(*config.Config, *slog.Logger) (qrdbsdk.SealingStrategy, *xkmssdk.TPM2Backend) {
		if usable {
			return fakeTPM2Strategy{}, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { tpm2AutoSealStrategy = prev })
}

// TestResolveEffectiveSealStrategy_StringMapping verifies the pure string
// normalizer: auto/empty/passphrase -> passphrase; every explicit value
// unchanged. It must NEVER open a TPM (no seam involved).
func TestResolveEffectiveSealStrategy_StringMapping(t *testing.T) {
	cases := []struct {
		in   string
		want qrdbsdk.StrategyID
	}{
		{"", qrdbsdk.StrategyPassphrase},
		{string(config.BackendAuto), qrdbsdk.StrategyPassphrase},
		{string(qrdbsdk.StrategyPassphrase), qrdbsdk.StrategyPassphrase},
		{string(qrdbsdk.StrategyNone), qrdbsdk.StrategyNone},
		{string(qrdbsdk.StrategyShamir), qrdbsdk.StrategyShamir},
		{string(xkmssdk.StrategyTPM2), xkmssdk.StrategyTPM2},
		{string(xkmssdk.StrategyAWSKMS), xkmssdk.StrategyAWSKMS},
	}
	for _, tc := range cases {
		t.Run("strategy="+tc.in, func(t *testing.T) {
			cfg := config.TestConfig()
			cfg.Seal.Strategy = tc.in
			if got := resolveEffectiveSealStrategy(cfg, nil); got != tc.want {
				t.Fatalf("resolveEffectiveSealStrategy(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolveEffectiveSealStrategy_NilConfig verifies the nil-config guard
// returns the software passphrase default.
func TestResolveEffectiveSealStrategy_NilConfig(t *testing.T) {
	if got := resolveEffectiveSealStrategy(nil, nil); got != qrdbsdk.StrategyPassphrase {
		t.Fatalf("nil config: got %q, want passphrase", got)
	}
}

// TestBuildHardwareSealStrategies_AutoSelectsTPM2WhenUsable verifies the OPT-IN
// auto path dispatches the TPM2 strategy when a usable TPM is present.
func TestBuildHardwareSealStrategies_AutoSelectsTPM2WhenUsable(t *testing.T) {
	withStubbedAutoStrategy(t, true)
	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(config.BackendAuto)

	strategies, err := buildHardwareSealStrategies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(strategies) != 1 {
		t.Fatalf("auto + usable TPM: want 1 strategy, got %d", len(strategies))
	}
	if strategies[0].ID() != xkmssdk.StrategyTPM2 {
		t.Fatalf("auto + usable TPM: strategy.ID()=%q, want tpm2", strategies[0].ID())
	}
}

// TestBuildHardwareSealStrategies_AutoFallsBackToSoftware verifies that the auto
// path returns (nil, nil) — the software-passthrough signal — when no usable TPM
// is present, so the caller builds the passphrase strategy. This is the
// conservative fallback that guarantees boot never depends on hardware.
func TestBuildHardwareSealStrategies_AutoFallsBackToSoftware(t *testing.T) {
	withStubbedAutoStrategy(t, false)
	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(config.BackendAuto)

	strategies, err := buildHardwareSealStrategies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategies != nil {
		t.Fatalf("auto + no usable TPM must yield nil (software) strategies, got %d", len(strategies))
	}
}

// TestBuildHardwareSealStrategies_AutoNeverProvisionStub asserts the auto path
// goes through the tpm2AutoSealStrategy seam (never the provisioning factory).
// A stub that records invocation proves the explicit-tpm2 factory is not reached.
func TestBuildHardwareSealStrategies_AutoNeverProvisionStub(t *testing.T) {
	called := false
	prev := tpm2AutoSealStrategy
	tpm2AutoSealStrategy = func(*config.Config, *slog.Logger) (qrdbsdk.SealingStrategy, *xkmssdk.TPM2Backend) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { tpm2AutoSealStrategy = prev })

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(config.BackendAuto)
	if _, err := buildHardwareSealStrategies(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("auto path did not route through the non-provisioning seam")
	}
}

// TestResolveBarrierStrategy_AutoFallsBackToPassphrase verifies the data-plane
// init-marker resolver maps "auto" to the software passphrase default (the
// hardware decision is made separately, once, in buildHardwareSealStrategies).
func TestResolveBarrierStrategy_AutoFallsBackToPassphrase(t *testing.T) {
	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(config.BackendAuto)
	cfg.Storage.DataDir = t.TempDir() // fresh dir, no init marker

	strategy, _, _, err := resolveBarrierStrategy(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != qrdbsdk.StrategyPassphrase {
		t.Fatalf("auto: strategy=%q, want passphrase", strategy)
	}
}

// TestBuildTPM2AutoSealStrategy_UnreachableSimulatorIsNotUsable exercises the
// REAL single-open seam against a closed TCP port (no TPM listening).
// NewTPM2Backend fails fast with a connection error, so the seam must return nil
// (software fallback) WITHOUT provisioning and without stalling.
func TestBuildTPM2AutoSealStrategy_UnreachableSimulatorIsNotUsable(t *testing.T) {
	cfg := config.TestConfig()
	cfg.Seal.TPM2.UseSimulator = true
	cfg.Seal.TPM2.SimulatorHost = "127.0.0.1"
	cfg.Seal.TPM2.SimulatorPort = 1 // reserved/unused port: connection refused
	cfg.Seal.TPM2.KeyDir = t.TempDir()

	if got, backend := buildTPM2AutoSealStrategy(cfg, slog.Default()); got != nil || backend != nil {
		t.Fatalf("unreachable simulator must yield (nil, nil), got strategy=%v backend=%v", got, backend)
	}
}

// TestBuildTPM2AutoSealStrategy_NilConfig verifies the nil-config guard.
func TestBuildTPM2AutoSealStrategy_NilConfig(t *testing.T) {
	if got, backend := buildTPM2AutoSealStrategy(nil, slog.Default()); got != nil || backend != nil {
		t.Fatalf("nil config must yield (nil, nil), got strategy=%v backend=%v", got, backend)
	}
}
