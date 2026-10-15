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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRaftConfigListenAddr(t *testing.T) {
	tests := []struct {
		name string
		cfg  RaftConfig
		want string
	}{
		{
			name: "default binds the advertised host and port",
			cfg:  RaftConfig{RaftAddress: "node1:63001"},
			want: "node1:63001",
		},
		{
			name: "default brackets an IPv6 advertised host",
			cfg:  RaftConfig{RaftAddress: "[fd00::5]:63001"},
			want: "[fd00::5]:63001",
		},
		{
			// A value equal to the defaults layer's own address is still an
			// explicit choice and must not be replaced (#185 item 1).
			name: "explicit listen address equal to the default address is honoured",
			cfg:  RaftConfig{ListenAddress: DefaultRaftAddress, RaftAddress: "node1:63001"},
			want: DefaultRaftAddress,
		},
		{
			name: "explicit listen address wins over listen host",
			cfg:  RaftConfig{ListenAddress: "10.0.0.5:7000", ListenHost: "192.168.1.1", RaftAddress: "node1:63001"},
			want: "10.0.0.5:7000",
		},
		{
			name: "NAT keeps a listen address that differs from the advertised address",
			cfg:  RaftConfig{ListenAddress: "0.0.0.0:7000", RaftAddress: "public.example:63001"},
			want: "0.0.0.0:7000",
		},
		{
			name: "derived from listen host and the advertised port",
			cfg:  RaftConfig{ListenHost: "10.0.0.5", RaftAddress: "node1:63001"},
			want: "10.0.0.5:63001",
		},
		{
			name: "explicit wildcard listen host opts in to every interface",
			cfg:  RaftConfig{ListenHost: "0.0.0.0", RaftAddress: "node1:63001"},
			want: "0.0.0.0:63001",
		},
		{
			name: "IPv6 listen host is bracketed",
			cfg:  RaftConfig{ListenHost: "::1", RaftAddress: "node1:63001"},
			want: "[::1]:63001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ListenAddr()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRaftConfigListenAddr_Underivable(t *testing.T) {
	tests := []struct {
		name         string
		cfg          RaftConfig
		wantWildcard bool
	}{
		{name: "advertised address without a port", cfg: RaftConfig{ListenHost: "10.0.0.5", RaftAddress: "node1"}},
		{name: "advertised address with an empty port", cfg: RaftConfig{RaftAddress: "node1:"}},
		{name: "empty advertised address", cfg: RaftConfig{}},
		{name: "empty advertised host", cfg: RaftConfig{RaftAddress: ":63001"}, wantWildcard: true},
		{name: "IPv4 wildcard advertised host", cfg: RaftConfig{RaftAddress: "0.0.0.0:63001"}, wantWildcard: true},
		{name: "IPv6 wildcard advertised host", cfg: RaftConfig{RaftAddress: "[::]:63001"}, wantWildcard: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ListenAddr()
			assert.Empty(t, got)

			var lerr *RaftListenAddressError
			require.True(t, errors.As(err, &lerr), "want *RaftListenAddressError, got %T", err)
			assert.Equal(t, tt.cfg.RaftAddress, lerr.RaftAddress)
			require.Error(t, lerr.Cause)
			assert.Contains(t, err.Error(), "raft.listen_address")

			var herr *RaftAdvertisedHostError
			assert.Equal(t, tt.wantWildcard, errors.As(err, &herr))
		})
	}
}

// TestRaftAddressKeys_FileThenEnv walks the Loader for every Raft address key:
// the file sets it, and the environment overrides the file.
func TestRaftAddressKeys_FileThenEnv(t *testing.T) {
	tests := []struct {
		key     string
		env     string
		fileVal string
		envVal  string
		get     func(*Config) string
	}{
		{"raft_address", "QUICBAAS_RAFT_RAFT_ADDRESS", "file-node:63001", "env-node:63002", func(c *Config) string { return c.Raft.RaftAddress }},
		{"listen_address", "QUICBAAS_RAFT_LISTEN_ADDRESS", "127.0.0.1:63000", "10.0.0.9:63000", func(c *Config) string { return c.Raft.ListenAddress }},
		{"listen_host", "QUICBAAS_RAFT_LISTEN_HOST", "10.0.0.1", "10.0.0.2", func(c *Config) string { return c.Raft.ListenHost }},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			body := "raft:\n  " + tt.key + ": \"" + tt.fileVal + "\"\n"
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			cfg, err := NewLoader().Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.fileVal, tt.get(cfg), "file layer")

			t.Setenv(tt.env, tt.envVal)
			cfg, err = NewLoader().Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.envVal, tt.get(cfg), "environment must beat the file")
		})
	}
}

// TestRaftAddressKeys_EnvOnly proves a key no file names still reaches the
// struct from the environment alone — the shape the cluster compose uses.
func TestRaftAddressKeys_EnvOnly(t *testing.T) {
	t.Setenv("QUICBAAS_RAFT_LISTEN_HOST", "10.0.0.3")

	cfg, err := NewLoader().Load("")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.3", cfg.Raft.ListenHost)
}

// TestRaftAddressDefaults pins the defaults layer: neither listen setting is
// set, so the node binds the address it advertises and never a wildcard.
func TestRaftAddressDefaults(t *testing.T) {
	for name, rc := range map[string]RaftConfig{
		"DefaultConfig":     DefaultConfig().Raft,
		"DefaultRaftConfig": DefaultRaftConfig(),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, rc.ListenAddress, "listen_address must default to derived")
			assert.Empty(t, rc.ListenHost, "listen_host must default to the advertised host")
			got, err := rc.ListenAddr()
			require.NoError(t, err)
			assert.Equal(t, DefaultRaftAddress, got)
		})
	}
}

// TestProductionConfigAdvertisesNoWildcard verifies the production preset
// carries the Raft port but no host: it never advertises a wildcard peers
// cannot dial, and leaves the bind to derive from the operator's host.
func TestProductionConfigAdvertisesNoWildcard(t *testing.T) {
	rc := ProductionConfig().Raft
	assert.Empty(t, rc.ListenAddress)
	assert.Empty(t, rc.ListenHost)

	host, port, err := net.SplitHostPort(rc.RaftAddress)
	require.NoError(t, err)
	assert.Empty(t, host, "the advertised host is the operator's to set")
	assert.Equal(t, strconv.Itoa(DefaultRaftPort), port)
}

// TestProductionConfigRequiresAdvertisedHost verifies that a production
// config with no advertised host fails validation with the typed error, and
// that the message names the key and flag the operator must set.
func TestProductionConfigRequiresAdvertisedHost(t *testing.T) {
	cfg := ProductionConfig()
	v := NewValidator()
	v.validateRaft(&cfg.Raft, &cfg.Server)
	err := v.Errors()

	var herr *RaftAdvertisedHostError
	require.True(t, errors.As(err, &herr), "want *RaftAdvertisedHostError, got %v", err)
	assert.Empty(t, herr.Host)
	assert.Contains(t, err.Error(), "raft.raft_address")
	assert.Contains(t, err.Error(), "--raft-address")
}

// TestProductionConfigWithAdvertisedHost verifies that once the operator
// supplies the advertised host, the production Raft section validates and
// the transport binds that host.
func TestProductionConfigWithAdvertisedHost(t *testing.T) {
	cfg := ProductionConfig()
	cfg.Raft.RaftAddress = "node1:63000"
	v := NewValidator()
	v.validateRaft(&cfg.Raft, &cfg.Server)
	require.NoError(t, v.Errors())

	got, err := cfg.Raft.ListenAddr()
	require.NoError(t, err)
	assert.Equal(t, "node1:63000", got)
}
