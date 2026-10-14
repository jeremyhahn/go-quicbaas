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
	"errors"
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

	if err := parseSpecialEnvVars(); err != nil {
		t.Fatalf("parseSpecialEnvVars: %v", err)
	}

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

// TestParseSpecialEnvVars_ValidValuesApply verifies each variable the daemon
// parses itself applies a well-formed value to the config.
func TestParseSpecialEnvVars_ValidValuesApply(t *testing.T) {
	tests := []struct {
		name  string
		value string
		check func(c *config.Config) bool
	}{
		{"QUICBAAS_SERVER_WRITE_TIMEOUT", "45s", func(c *config.Config) bool { return c.Server.WriteTimeout == 45*time.Second }},
		{"QUICBAAS_SERVER_READ_TIMEOUT", "2m", func(c *config.Config) bool { return c.Server.ReadTimeout == 2*time.Minute }},
		{"QUICBAAS_SERVER_IDLE_TIMEOUT", "90s", func(c *config.Config) bool { return c.Server.IdleTimeout == 90*time.Second }},
		{"QUICBAAS_SERVER_SHARDS", "7, 9", func(c *config.Config) bool {
			return len(c.Server.Shards) == 2 && c.Server.Shards[0] == 7 && c.Server.Shards[1] == 9
		}},
		{"QUICBAAS_SERVER_ENTITY_SHARD_PINS", "user:7", func(c *config.Config) bool {
			return len(c.Server.EntityShardPins) == 1 && c.Server.EntityShardPins["user"] == 7
		}},
		{"QUICBAAS_ENABLE_SCHEDULER", "ON", func(c *config.Config) bool { return c.Scheduler.Enabled }},
		{"QUICBAAS_TIMESERIES_ENABLED", "yes", func(c *config.Config) bool { return c.TimeSeries.Enabled }},
		{"QUICBAAS_VECTOR_ENABLED", "1", func(c *config.Config) bool { return c.Vector.Enabled }},
		{"QUICBAAS_BACKUP_ENABLED", "true", func(c *config.Config) bool { return c.Backup.Enabled }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origCfg := cfg
			defer func() { cfg = origCfg }()
			cfg = config.Default()

			t.Setenv(tt.name, tt.value)
			if err := parseSpecialEnvVars(); err != nil {
				t.Fatalf("parseSpecialEnvVars: %v", err)
			}
			if !tt.check(cfg) {
				t.Errorf("%s=%q was not applied", tt.name, tt.value)
			}
		})
	}
}

// TestParseSpecialEnvVars_MalformedValueFails verifies a malformed value
// returns an InvalidEnvValueError naming the variable and its value instead
// of being dropped.
func TestParseSpecialEnvVars_MalformedValueFails(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"QUICBAAS_SERVER_WRITE_TIMEOUT", "not-a-duration"},
		{"QUICBAAS_SERVER_READ_TIMEOUT", "30"},
		{"QUICBAAS_SERVER_IDLE_TIMEOUT", "forever"},
		{"QUICBAAS_SERVER_SHARDS", "1,two"},
		{"QUICBAAS_SERVER_SHARDS", "-1"},
		{"QUICBAAS_SERVER_ENTITY_SHARD_PINS", "user"},
		{"QUICBAAS_SERVER_ENTITY_SHARD_PINS", "user:x"},
		{"QUICBAAS_ENABLE_SCHEDULER", "enabled"},
		{"QUICBAAS_TIMESERIES_ENABLED", "y"},
		{"QUICBAAS_VECTOR_ENABLED", "2"},
		{"QUICBAAS_BACKUP_ENABLED", "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			origCfg := cfg
			defer func() { cfg = origCfg }()
			cfg = config.Default()

			t.Setenv(tt.name, tt.value)
			err := parseSpecialEnvVars()
			if !errors.Is(err, config.ErrInvalidEnvValue) {
				t.Fatalf("parseSpecialEnvVars() error = %v, want ErrInvalidEnvValue", err)
			}
			var envErr *config.InvalidEnvValueError
			if !errors.As(err, &envErr) {
				t.Fatalf("error %v is not an *InvalidEnvValueError", err)
			}
			if envErr.Name != tt.name || envErr.Value != tt.value {
				t.Errorf("error names %s=%q, want %s=%q", envErr.Name, envErr.Value, tt.name, tt.value)
			}
		})
	}
}

// TestParseSpecialEnvVars_RaftJoinAddresses covers the comma-split branch
// for QUICBAAS_RAFT_JOIN_ADDRESSES, including empty-entry trimming.
func TestParseSpecialEnvVars_RaftJoinAddresses(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()

	t.Setenv("QUICBAAS_RAFT_JOIN_ADDRESSES", " 1=host-a:7000 , , 2=host-b:7000 ")
	if err := parseSpecialEnvVars(); err != nil {
		t.Fatalf("parseSpecialEnvVars: %v", err)
	}

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
	if err := parseSpecialEnvVars(); err != nil {
		t.Fatalf("parseSpecialEnvVars: %v", err)
	}

	if len(cfg.Raft.JoinAddresses) != len(before) {
		t.Errorf("JoinAddresses unexpectedly changed to %v", cfg.Raft.JoinAddresses)
	}
}

// TestParseSpecialEnvVars_RaftMemberIdentities covers the comma-split of
// QUICBAAS_RAFT_MEMBER_IDENTITIES, including empty-entry trimming.
func TestParseSpecialEnvVars_RaftMemberIdentities(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()

	t.Setenv("QUICBAAS_RAFT_MEMBER_IDENTITIES", " 2=spki:aa , , 3=spki:bb ")
	if err := parseSpecialEnvVars(); err != nil {
		t.Fatalf("parseSpecialEnvVars: %v", err)
	}

	got := cfg.Raft.MemberIdentities
	if len(got) != 2 || got[0] != "2=spki:aa" || got[1] != "3=spki:bb" {
		t.Errorf("MemberIdentities = %v, want trimmed pair", got)
	}
}

// TestParseSpecialEnvVars_RaftMemberIdentitiesAllBlank verifies an env value
// of only separators leaves the configured identities untouched.
func TestParseSpecialEnvVars_RaftMemberIdentitiesAllBlank(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = config.Default()
	cfg.Raft.MemberIdentities = []string{"2=spki:aa"}

	t.Setenv("QUICBAAS_RAFT_MEMBER_IDENTITIES", " , , ")
	if err := parseSpecialEnvVars(); err != nil {
		t.Fatalf("parseSpecialEnvVars: %v", err)
	}

	if len(cfg.Raft.MemberIdentities) != 1 {
		t.Errorf("MemberIdentities unexpectedly changed to %v", cfg.Raft.MemberIdentities)
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
