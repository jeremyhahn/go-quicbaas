// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).
// See LICENSE file in the root directory for details.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// addressCommands is a fresh root command carrying the real --address flag
// and the real txn command carrying --rest, both bound to a fresh viper
// loaded from a temporary config file.
type addressCommands struct {
	v    *viper.Viper
	root *cobra.Command
	txn  *cobra.Command
}

// newAddressCommands builds the command pair. It restores the package
// address variables when the test ends, because registering the flags
// resets them to their defaults.
func newAddressCommands(t *testing.T, configYAML string) addressCommands {
	t.Helper()
	savedAddress, savedREST := address, restAddress
	t.Cleanup(func() {
		address, restAddress = savedAddress, savedREST
	})
	for _, b := range serverAddressBindings {
		// Setenv registers the restore; Unsetenv clears the variable, since
		// a set-but-empty address variable is itself refused.
		t.Setenv(b.env, "")
		require.NoError(t, os.Unsetenv(b.env))
	}

	root := &cobra.Command{Use: "quicbaas"}
	addServerAddressFlag(root.PersistentFlags())
	txn := newTransactionCommand()
	root.AddCommand(txn)

	v := viper.New()
	require.NoError(t, bindServerAddressFlags(v, root.PersistentFlags(), txn.PersistentFlags()))
	if configYAML != "" {
		path := filepath.Join(t.TempDir(), "quicbaas.yaml")
		require.NoError(t, os.WriteFile(path, []byte(configYAML), 0o600))
		v.SetConfigFile(path)
		require.NoError(t, v.ReadInConfig())
	}
	return addressCommands{v: v, root: root, txn: txn}
}

// resolve resolves the addresses the way initConfig does for the real root.
func (c addressCommands) resolve() error {
	return resolveServerAddresses(c.v, c.txn.Flags())
}

func TestResolveServerAddresses_Precedence(t *testing.T) {
	const (
		fileAddress = "file.address.test:9090"
		envAddress  = "env.address.test:9090"
		flagAddress = "flag.address.test:9090"
		fileREST    = "https://file.rest.test:8443"
		envREST     = "https://env.rest.test:8443"
		flagREST    = "https://flag.rest.test:8443"
	)
	fileYAML := "client:\n  address: " + fileAddress + "\n  transaction_rest: " + fileREST + "\n"

	tests := []struct {
		name        string
		configYAML  string
		env         bool
		flags       []string
		wantAddress string
		wantREST    string
	}{
		{
			name:        "defaults",
			wantAddress: config.DefaultUnixSocket,
			wantREST:    defaultTransactionRESTAddress,
		},
		{
			name:        "config file overrides default",
			configYAML:  fileYAML,
			wantAddress: fileAddress,
			wantREST:    fileREST,
		},
		{
			name:        "env without a file",
			env:         true,
			wantAddress: envAddress,
			wantREST:    envREST,
		},
		{
			name:        "env beats file",
			configYAML:  fileYAML,
			env:         true,
			wantAddress: envAddress,
			wantREST:    envREST,
		},
		{
			name:        "flag beats env and file",
			configYAML:  fileYAML,
			env:         true,
			flags:       []string{"--address", flagAddress, "--rest", flagREST},
			wantAddress: flagAddress,
			wantREST:    flagREST,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := newAddressCommands(t, tt.configYAML)
			if tt.env {
				t.Setenv("QUICBAAS_CLIENT_ADDRESS", envAddress)
				t.Setenv("QUICBAAS_CLIENT_TRANSACTION_REST", envREST)
			}
			require.NoError(t, cmds.txn.ParseFlags(tt.flags))

			require.NoError(t, cmds.resolve())
			assert.Equal(t, tt.wantAddress, address)
			assert.Equal(t, tt.wantREST, restAddress)
		})
	}
}

func TestResolveServerAddresses_AcceptedForms(t *testing.T) {
	tests := []struct {
		name    string
		address string
		rest    string
	}{
		{name: "unix socket path", address: "/run/quicbaas/quicbaas.sock", rest: "localhost:8443"},
		{name: "relative socket file", address: "quicbaas.sock", rest: "http://localhost:8080"},
		{name: "IPv6 host and port", address: "[::1]:9090", rest: "[::1]:8443"},
		{name: "URL", address: "https://quicbaas.test:11443", rest: "https://quicbaas.test:8443/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := newAddressCommands(t, "")
			require.NoError(t, cmds.txn.ParseFlags([]string{"--address", tt.address, "--rest", tt.rest}))

			require.NoError(t, cmds.resolve())
			assert.Equal(t, tt.address, address)
			assert.Equal(t, tt.rest, restAddress)
		})
	}
}

func TestResolveServerAddresses_MalformedValues(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		envKey     string
		envValue   string
	}{
		{name: "address port out of range", envKey: "QUICBAAS_CLIENT_ADDRESS", envValue: "quicbaas.test:99999"},
		{name: "address without a port", envKey: "QUICBAAS_CLIENT_ADDRESS", envValue: "quicbaas.test"},
		{name: "address with whitespace", envKey: "QUICBAAS_CLIENT_ADDRESS", envValue: "quicbaas.test :9090"},
		{name: "address URL without a host", envKey: "QUICBAAS_CLIENT_ADDRESS", envValue: "https://"},
		{name: "address from config file", configYAML: "client:\n  address: quicbaas.test:port\n"},
		{name: "rest empty in config file", configYAML: "client:\n  transaction_rest: \"\"\n"},
		{name: "rest unix socket", envKey: "QUICBAAS_CLIENT_TRANSACTION_REST", envValue: "/run/quicbaas.sock"},
		{name: "rest non-HTTP scheme", envKey: "QUICBAAS_CLIENT_TRANSACTION_REST", envValue: "ftp://quicbaas.test:21"},
		{name: "rest port zero", envKey: "QUICBAAS_CLIENT_TRANSACTION_REST", envValue: "quicbaas.test:0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := newAddressCommands(t, tt.configYAML)
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envValue)
			}

			err := cmds.resolve()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrMalformedServerAddress)
			assert.True(t, errors.Is(err, errors.ErrConfiguration), "got %v", err)
		})
	}
}

func TestResolveServerAddresses_EmptyValueNamesSource(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		envKey     string
		flags      []string
		wantSource string
	}{
		{name: "address flag", flags: []string{"--address", ""}, wantSource: "client.address from --address"},
		{name: "address env", envKey: "QUICBAAS_CLIENT_ADDRESS", wantSource: "client.address from QUICBAAS_CLIENT_ADDRESS"},
		{name: "address config file", configYAML: "client:\n  address: \"\"\n", wantSource: "client.address from config file"},
		{name: "rest flag", flags: []string{"--rest", ""}, wantSource: "client.transaction_rest from --rest"},
		{name: "rest env", envKey: "QUICBAAS_CLIENT_TRANSACTION_REST", wantSource: "client.transaction_rest from QUICBAAS_CLIENT_TRANSACTION_REST"},
		{name: "rest config file", configYAML: "client:\n  transaction_rest: \"\"\n", wantSource: "client.transaction_rest from config file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := newAddressCommands(t, tt.configYAML)
			if tt.envKey != "" {
				t.Setenv(tt.envKey, "")
			}
			require.NoError(t, cmds.txn.ParseFlags(tt.flags))

			err := cmds.resolve()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrMalformedServerAddress)
			assert.Contains(t, err.Error(), tt.wantSource)
			assert.Contains(t, err.Error(), errAddressEmpty.Error())
		})
	}
}

func TestResolveServerAddresses_FlagOverridesEmptyEnv(t *testing.T) {
	const flagAddress = "flag.address.test:9090"
	cmds := newAddressCommands(t, "")
	t.Setenv("QUICBAAS_CLIENT_ADDRESS", "")
	require.NoError(t, cmds.txn.ParseFlags([]string{"--address", flagAddress}))

	require.NoError(t, cmds.resolve())
	assert.Equal(t, flagAddress, address)
}

func TestResolveServerAddresses_CommandWithoutRESTFlag(t *testing.T) {
	cmds := newAddressCommands(t, "")
	require.NoError(t, cmds.root.ParseFlags([]string{"--address", ""}))

	err := resolveServerAddresses(cmds.v, cmds.root.Flags())
	require.ErrorIs(t, err, ErrMalformedServerAddress)
	assert.Contains(t, err.Error(), "client.address from --address")

	cmds = newAddressCommands(t, "")
	require.NoError(t, resolveServerAddresses(cmds.v, cmds.root.Flags()))
	assert.Equal(t, defaultTransactionRESTAddress, restAddress)
}

func TestRootCommand_BindsServerAddressFlags(t *testing.T) {
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("address"))
	txn, _, err := rootCmd.Find([]string{"txn"})
	require.NoError(t, err)
	assert.NotNil(t, txn.PersistentFlags().Lookup("rest"))
	assert.NotNil(t, txn.PersistentPreRunE, "txn must run the root initialisation that resolves --rest")
}
