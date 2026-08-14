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

// coverage_extra_test.go targets the remaining uncovered error/branch paths
// of the daemon entrypoint helpers (env parsing, officer/CA loaders, marker
// writers, seal override). Every test runs in t.TempDir(), never binds a
// port, and never starts a long-running daemon.

package main

import (
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// parseSpecialEnvVars: duration timeouts + raft join addresses
// =============================================================================

// TestParseSpecialEnvVars_DurationTimeouts covers the three server timeout
// branches that parse a duration string into cfg.Server.*Timeout.
func TestParseSpecialEnvVars_DurationTimeouts(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()

	t.Setenv("QUICBAAS_SERVER_WRITE_TIMEOUT", "45s")
	t.Setenv("QUICBAAS_SERVER_READ_TIMEOUT", "30s")
	t.Setenv("QUICBAAS_SERVER_IDLE_TIMEOUT", "120s")

	parseSpecialEnvVars()

	if cfg.Server.WriteTimeout != 45*time.Second {
		t.Errorf("WriteTimeout = %v, want 45s", cfg.Server.WriteTimeout)
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", cfg.Server.ReadTimeout)
	}
	if cfg.Server.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", cfg.Server.IdleTimeout)
	}
}

// TestParseSpecialEnvVars_InvalidDurationIsIgnored verifies an unparseable
// duration leaves the existing config value untouched (the err != nil branch
// of time.ParseDuration takes no action).
func TestParseSpecialEnvVars_InvalidDurationIsIgnored(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()
	before := cfg.Server.WriteTimeout

	t.Setenv("QUICBAAS_SERVER_WRITE_TIMEOUT", "not-a-duration")
	parseSpecialEnvVars()

	if cfg.Server.WriteTimeout != before {
		t.Errorf("WriteTimeout changed to %v on invalid input, want %v", cfg.Server.WriteTimeout, before)
	}
}

// TestParseSpecialEnvVars_RaftJoinAddresses covers the comma-split branch
// for QUICBAAS_RAFT_JOIN_ADDRESSES, including empty-entry trimming.
func TestParseSpecialEnvVars_RaftJoinAddresses(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()

	t.Setenv("QUICBAAS_RAFT_JOIN_ADDRESSES", " 1=host-a:7000 , , 2=host-b:7000 ")
	parseSpecialEnvVars()

	got := cfg.Raft.JoinAddresses
	if len(got) != 2 {
		t.Fatalf("JoinAddresses = %v, want 2 entries", got)
	}
	if got[0] != "1=host-a:7000" || got[1] != "2=host-b:7000" {
		t.Errorf("JoinAddresses = %v, want trimmed pair", got)
	}
}

// TestParseSpecialEnvVars_RaftJoinAddressesAllBlank verifies an env value of
// only separators yields no addresses (len(addrs) == 0 branch is skipped).
func TestParseSpecialEnvVars_RaftJoinAddressesAllBlank(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()
	before := cfg.Raft.JoinAddresses

	t.Setenv("QUICBAAS_RAFT_JOIN_ADDRESSES", " , , ")
	parseSpecialEnvVars()

	if len(cfg.Raft.JoinAddresses) != len(before) {
		t.Errorf("JoinAddresses unexpectedly changed to %v", cfg.Raft.JoinAddresses)
	}
}

// =============================================================================
// applySealOverride: both viper.IsSet branches
// =============================================================================

// TestApplySealOverride_SetFlipsExplicit covers the branch where viper reports
// seal.strategy as configured, flipping StrategyExplicit on.
func TestApplySealOverride_SetFlipsExplicit(t *testing.T) {
	origCfg := cfg
	defer func() {
		cfg = origCfg
		viper.Reset()
	}()
	cfg = config.Default()
	cfg.Seal.StrategyExplicit = false

	viper.Reset()
	viper.Set("seal.strategy", "passphrase")
	applySealOverride()

	if !cfg.Seal.StrategyExplicit {
		t.Fatal("expected StrategyExplicit=true when seal.strategy is set")
	}
}

// TestApplySealOverride_UnsetLeavesImplicit covers the branch where viper has
// no seal.strategy, leaving StrategyExplicit unchanged.
func TestApplySealOverride_UnsetLeavesImplicit(t *testing.T) {
	origCfg := cfg
	defer func() {
		cfg = origCfg
		viper.Reset()
	}()
	cfg = config.Default()
	cfg.Seal.StrategyExplicit = false

	viper.Reset()
	applySealOverride()

	if cfg.Seal.StrategyExplicit {
		t.Fatal("expected StrategyExplicit to stay false when seal.strategy is unset")
	}
}

// =============================================================================
// officer loaders: valid PEM block but undecodable DER body
// =============================================================================

// =============================================================================
// loadTrustedCAs: directory path rejected
// =============================================================================

// writeBadDERPEM writes a PEM block of the given type whose body is not valid
// DER for that type, so the parser inside the loader fails after PEM decode.
func writeBadDERPEM(t *testing.T, path, blockType string) {
	t.Helper()
	out := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: []byte{0x01, 0x02, 0x03, 0x04}})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write bad DER PEM: %v", err)
	}
}
