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

package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// raftEnv lists the environment variables bound to the Raft address keys.
var raftEnv = []string{
	"QUICBAAS_RAFT_RAFT_ADDRESS",
	"QUICBAAS_RAFT_LISTEN_ADDRESS",
	"QUICBAAS_RAFT_LISTEN_HOST",
}

// raftLayers is one daemon start: the raft section of the config file, the
// environment, and the command-line flags, applied the way runNode does.
type raftLayers struct {
	file  map[string]string
	env   map[string]string
	flags map[string]string
}

// loadRaftLayers runs initConfig over the file and environment layers, then
// applies the flags with applyRaftFlags. The globals it touches are restored
// when the test ends.
func loadRaftLayers(t *testing.T, layers raftLayers, hostname func() (string, error)) error {
	t.Helper()

	origCfg, origLog, origCfgFile := cfg, log, cfgFile
	t.Cleanup(func() {
		cfg, log, cfgFile = origCfg, origLog, origCfgFile
		raftAddress = ""
		viper.Reset()
	})

	for _, name := range raftEnv {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
	for name, value := range layers.env {
		t.Setenv(name, value)
	}

	body := "logging:\n  level: error\nraft:\n"
	for key, value := range layers.file {
		body += "  " + key + ": \"" + value + "\"\n"
	}
	cfgFile = filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(body), 0o600))

	viper.Reset()
	initConfig()

	cmd := newTestCmdWithRaftFlags()
	for name, value := range layers.flags {
		require.NoError(t, cmd.Flags().Set(name, value))
	}
	return applyRaftFlags(cmd, hostname)
}

// failingHostname is a resolver for paths that must never consult it.
func failingHostname() (string, error) {
	return "", &net.DNSError{Err: "no such host", Name: "self"}
}

func TestRaftListenHostPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		layers     raftLayers
		want       string
		wantListen string
	}{
		{
			name:       "defaults layer binds the advertised host",
			want:       "",
			wantListen: "node1:63001",
		},
		{
			name:       "file overrides defaults",
			layers:     raftLayers{file: map[string]string{"listen_host": "10.0.0.1"}},
			want:       "10.0.0.1",
			wantListen: "10.0.0.1:63001",
		},
		{
			name:       "env alone overrides defaults",
			layers:     raftLayers{env: map[string]string{"QUICBAAS_RAFT_LISTEN_HOST": "10.0.0.2"}},
			want:       "10.0.0.2",
			wantListen: "10.0.0.2:63001",
		},
		{
			name: "env overrides file",
			layers: raftLayers{
				file: map[string]string{"listen_host": "10.0.0.1"},
				env:  map[string]string{"QUICBAAS_RAFT_LISTEN_HOST": "10.0.0.2"},
			},
			want:       "10.0.0.2",
			wantListen: "10.0.0.2:63001",
		},
		{
			name: "flag overrides env and file",
			layers: raftLayers{
				file:  map[string]string{"listen_host": "10.0.0.1"},
				env:   map[string]string{"QUICBAAS_RAFT_LISTEN_HOST": "10.0.0.2"},
				flags: map[string]string{"raft-listen-host": "10.0.0.3"},
			},
			want:       "10.0.0.3",
			wantListen: "10.0.0.3:63001",
		},
		{
			name: "flag overrides file without env",
			layers: raftLayers{
				file:  map[string]string{"listen_host": "10.0.0.1"},
				flags: map[string]string{"raft-listen-host": "10.0.0.3"},
			},
			want:       "10.0.0.3",
			wantListen: "10.0.0.3:63001",
		},
		{
			name:       "explicit wildcard flag opts in to every interface",
			layers:     raftLayers{flags: map[string]string{"raft-listen-host": "0.0.0.0"}},
			want:       "0.0.0.0",
			wantListen: "0.0.0.0:63001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.layers.file = withRaftAddress(tt.layers.file, "node1:63001")
			require.NoError(t, loadRaftLayers(t, tt.layers, failingHostname))

			assert.Equal(t, tt.want, cfg.Raft.ListenHost)
			assertDerivedListenAddr(t, tt.wantListen)
		})
	}
}

// TestRaftWildcardAdvertisedHost proves a wildcard advertised host is never
// bound by default, and is bound only on an explicit listen opt-in.
func TestRaftWildcardAdvertisedHost(t *testing.T) {
	tests := []struct {
		name       string
		flags      map[string]string
		wantListen string
	}{
		{name: "no listen setting", flags: map[string]string{"raft-address": "0.0.0.0:63001"}},
		{name: "IPv6 wildcard", flags: map[string]string{"raft-address": "[::]:63001"}},
		{
			name:       "explicit listen host",
			flags:      map[string]string{"raft-address": "0.0.0.0:63001", "raft-listen-host": "10.0.0.4"},
			wantListen: "10.0.0.4:63001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, loadRaftLayers(t, raftLayers{flags: tt.flags}, failingHostname))

			got, err := cfg.Raft.ListenAddr()
			if tt.wantListen != "" {
				require.NoError(t, err)
				assert.Equal(t, tt.wantListen, got)
				return
			}
			var herr *config.RaftAdvertisedHostError
			require.True(t, errors.As(err, &herr), "want *config.RaftAdvertisedHostError, got %v", err)
		})
	}
}

// withRaftAddress returns file with raft_address set to addr.
func withRaftAddress(file map[string]string, addr string) map[string]string {
	out := map[string]string{"raft_address": addr}
	for key, value := range file {
		out[key] = value
	}
	return out
}

func TestRaftListenAddressExplicitHonoured(t *testing.T) {
	// 127.0.0.1:63000 is also the defaults layer's advertised address; an
	// operator who sets it as the bind address must get exactly that (#185).
	const explicit = "127.0.0.1:63000"
	tests := []struct {
		name   string
		layers raftLayers
		want   string
	}{
		{
			name:   "file",
			layers: raftLayers{file: map[string]string{"listen_address": explicit}},
			want:   explicit,
		},
		{
			name:   "env",
			layers: raftLayers{env: map[string]string{"QUICBAAS_RAFT_LISTEN_ADDRESS": explicit}},
			want:   explicit,
		},
		{
			name:   "flag",
			layers: raftLayers{flags: map[string]string{"raft-listen-address": explicit}},
			want:   explicit,
		},
		{
			name: "env overrides file",
			layers: raftLayers{
				file: map[string]string{"listen_address": "10.0.0.1:63000"},
				env:  map[string]string{"QUICBAAS_RAFT_LISTEN_ADDRESS": explicit},
			},
			want: explicit,
		},
		{
			name: "flag overrides env",
			layers: raftLayers{
				env:   map[string]string{"QUICBAAS_RAFT_LISTEN_ADDRESS": "10.0.0.1:63000"},
				flags: map[string]string{"raft-listen-address": explicit},
			},
			want: explicit,
		},
		{
			name: "explicit address beats listen host",
			layers: raftLayers{
				file:  map[string]string{"listen_address": explicit},
				flags: map[string]string{"raft-listen-host": "10.0.0.3"},
			},
			want: explicit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.layers.file = withRaftAddress(tt.layers.file, "node1:63001")
			require.NoError(t, loadRaftLayers(t, tt.layers, failingHostname))

			got, err := cfg.Raft.ListenAddr()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRaftPortFlag(t *testing.T) {
	tests := []struct {
		name       string
		layers     raftLayers
		wantRaft   string
		wantListen string
	}{
		{
			name:       "nothing configured advertises the hostname",
			layers:     raftLayers{flags: map[string]string{"raft-port": "64100"}},
			wantRaft:   "node-d:64100",
			wantListen: "node-d:64100",
		},
		{
			name: "file advertised host is kept",
			layers: raftLayers{
				file:  map[string]string{"raft_address": "peer.example:63001"},
				flags: map[string]string{"raft-port": "64100"},
			},
			wantRaft:   "peer.example:64100",
			wantListen: "peer.example:64100",
		},
		{
			name: "env advertised host is kept",
			layers: raftLayers{
				env:   map[string]string{"QUICBAAS_RAFT_RAFT_ADDRESS": "quicbaas-node1:63001"},
				flags: map[string]string{"raft-port": "64100"},
			},
			wantRaft:   "quicbaas-node1:64100",
			wantListen: "quicbaas-node1:64100",
		},
		{
			name: "configured listen host is kept",
			layers: raftLayers{
				file:  map[string]string{"listen_host": "10.0.0.7"},
				flags: map[string]string{"raft-port": "64100"},
			},
			wantRaft:   "node-d:64100",
			wantListen: "10.0.0.7:64100",
		},
		{
			name: "configured listen address keeps its host",
			layers: raftLayers{
				file:  map[string]string{"listen_address": "10.0.0.8:63000"},
				flags: map[string]string{"raft-port": "64100"},
			},
			wantRaft:   "node-d:64100",
			wantListen: "10.0.0.8:64100",
		},
		{
			name: "listen address flag is not rewritten",
			layers: raftLayers{
				file:  map[string]string{"listen_address": "10.0.0.8:63000"},
				flags: map[string]string{"raft-port": "64100", "raft-listen-address": "10.0.0.9:65000"},
			},
			wantRaft:   "node-d:64100",
			wantListen: "10.0.0.9:65000",
		},
		{
			name: "raft address flag wins over raft port",
			layers: raftLayers{
				flags: map[string]string{"raft-port": "64100", "raft-address": "10.0.0.1:63500"},
			},
			wantRaft:   "10.0.0.1:63500",
			wantListen: "10.0.0.1:63500",
		},
		{
			name:       "no raft flags leave the documented defaults",
			wantRaft:   config.DefaultRaftAddress,
			wantListen: config.DefaultRaftAddress,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, loadRaftLayers(t, tt.layers, stubHostname("node-d")))

			assert.Equal(t, tt.wantRaft, cfg.Raft.RaftAddress)
			got, err := cfg.Raft.ListenAddr()
			require.NoError(t, err)
			assert.Equal(t, tt.wantListen, got)
		})
	}
}

// TestRaftComposeShape pins docker-compose.cluster.yml: only the advertised
// address is set, and the node listens on the host it advertises.
func TestRaftComposeShape(t *testing.T) {
	layers := raftLayers{env: map[string]string{"QUICBAAS_RAFT_RAFT_ADDRESS": "quicbaas-node1:63001"}}
	require.NoError(t, loadRaftLayers(t, layers, failingHostname))

	assert.Equal(t, "quicbaas-node1:63001", cfg.Raft.RaftAddress)
	assertDerivedListenAddr(t, "quicbaas-node1:63001")
}

func TestRaftHostnameError(t *testing.T) {
	lookupErr := &net.DNSError{Err: "no such host", Name: "self"}
	tests := []struct {
		name      string
		hostname  func() (string, error)
		wantCause error
	}{
		{
			name:      "lookup failure",
			hostname:  func() (string, error) { return "", lookupErr },
			wantCause: lookupErr,
		},
		{
			name:     "empty hostname",
			hostname: stubHostname(""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layers := raftLayers{flags: map[string]string{"raft-port": "64100"}}
			err := loadRaftLayers(t, layers, tt.hostname)

			var herr *RaftHostnameError
			require.True(t, errors.As(err, &herr), "want *RaftHostnameError, got %T", err)
			assert.Equal(t, tt.wantCause, herr.Cause)
			assert.Contains(t, err.Error(), "--raft-address")
			assert.Equal(t, config.DefaultRaftAddress, cfg.Raft.RaftAddress, "no placeholder may be advertised")
		})
	}
}

func TestRaftAddressFlagError(t *testing.T) {
	tests := []struct {
		name        string
		file        map[string]string
		wantKey     string
		wantAddress string
	}{
		{
			name:        "advertised address without a port",
			file:        map[string]string{"raft_address": "peer.example"},
			wantKey:     "raft.raft_address",
			wantAddress: "peer.example",
		},
		{
			name:        "listen address without a port",
			file:        map[string]string{"listen_address": "10.0.0.8"},
			wantKey:     "raft.listen_address",
			wantAddress: "10.0.0.8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layers := raftLayers{file: tt.file, flags: map[string]string{"raft-port": "64100"}}
			err := loadRaftLayers(t, layers, stubHostname("node-d"))

			var ferr *RaftAddressFlagError
			require.True(t, errors.As(err, &ferr), "want *RaftAddressFlagError, got %T", err)
			assert.Equal(t, tt.wantKey, ferr.Key)
			assert.Equal(t, tt.wantAddress, ferr.Address)
			assert.NotNil(t, ferr.Cause)
		})
	}
}
