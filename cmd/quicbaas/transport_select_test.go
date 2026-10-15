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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectTransport(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		explicit  bool
		address   string
		want      string
	}{
		// Implicit: the address chooses.
		{name: "implicit https URL", requested: defaultTransport, address: "https://localhost:8443", want: "rest"},
		{name: "implicit http URL", requested: defaultTransport, address: "http://localhost:8080", want: "rest"},
		{name: "implicit absolute socket path", requested: defaultTransport, address: "/run/quicbaas/quicbaas.sock", want: "unix"},
		{name: "implicit relative socket file", requested: defaultTransport, address: "quicbaas.sock", want: "unix"},
		{name: "implicit host and port", requested: defaultTransport, address: "localhost:9090", want: defaultTransport},
		{name: "implicit empty request with https URL", requested: "", address: "https://localhost:8443", want: "rest"},
		// Explicit: the flag wins.
		{name: "explicit unix with https URL", requested: "unix", explicit: true, address: "https://localhost:8443", want: "unix"},
		{name: "explicit grpc with https URL", requested: "grpc", explicit: true, address: "https://localhost:8443", want: "grpc"},
		{name: "explicit grpc with host and port", requested: "grpc", explicit: true, address: "localhost:9090", want: "grpc"},
		{name: "explicit grpc with socket file", requested: "grpc", explicit: true, address: "/run/quicbaas/quicbaas.sock", want: "unix"},
		{name: "explicit grpc with path lacking .sock", requested: "grpc", explicit: true, address: "/run/quicbaas/socket", want: "grpc"},
		{name: "explicit rest with socket file", requested: "rest", explicit: true, address: "/run/quicbaas/quicbaas.sock", want: "rest"},
		{name: "explicit quic with http URL", requested: "quic", explicit: true, address: "http://localhost:8080", want: "quic"},
		{name: "explicit mcp with host and port", requested: "mcp", explicit: true, address: "localhost:8090", want: "mcp"},
		{name: "explicit unknown is passed through", requested: "carrier-pigeon", explicit: true, address: "https://localhost:8443", want: "carrier-pigeon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, detectTransport(tt.requested, tt.explicit, tt.address))
		})
	}
}

func TestTransportExplicit(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("transport")
	require.NotNil(t, flag)
	savedTransport, savedChanged := transport, flag.Changed
	defer func() { transport, flag.Changed = savedTransport, savedChanged }()

	tests := []struct {
		name      string
		transport string
		changed   bool
		want      bool
	}{
		{name: "flag default", transport: defaultTransport, want: false},
		{name: "empty", transport: "", want: false},
		{name: "flag set to the default value", transport: defaultTransport, changed: true, want: true},
		{name: "set by a command", transport: "mcp", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, flag.Changed = tt.transport, tt.changed
			assert.Equal(t, tt.want, transportExplicit())
		})
	}
}

// TestBuildSDKClientOptions_DefaultTransportFollowsURL pins the defect lane
// 185k found: with --transport unset, an http(s) address was used as a Unix
// socket path.
func TestBuildSDKClientOptions_DefaultTransportFollowsURL(t *testing.T) {
	savedTransport, savedAddress := transport, address
	defer func() { transport, address = savedTransport, savedAddress }()

	transport = defaultTransport
	address = "https://localhost:8443"

	_, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.Equal(t, "rest", transport)
}

func TestCreateSchedulerClient_SelectsTransportFromAddress(t *testing.T) {
	savedTransport, savedAddress, savedBuilders := transport, address, schedulerClientBuilders
	defer func() {
		transport, address, schedulerClientBuilders = savedTransport, savedAddress, savedBuilders
	}()

	var built string
	schedulerClientBuilders = map[string]func() (SchedulerClient, error){}
	for name := range savedBuilders {
		schedulerClientBuilders[name] = func() (SchedulerClient, error) {
			built = name
			return nil, nil
		}
	}

	tests := []struct {
		name      string
		transport string
		address   string
		want      string
	}{
		{name: "default with https URL", transport: defaultTransport, address: "https://localhost:8443", want: "rest"},
		{name: "default with socket path", transport: defaultTransport, address: "/run/quicbaas/quicbaas.sock", want: "unix"},
		{name: "grpc with socket path", transport: "grpc", address: "/run/quicbaas/quicbaas.sock", want: "unix"},
		{name: "grpc with host and port", transport: "grpc", address: "localhost:9090", want: "grpc"},
		{name: "quic with https URL", transport: "quic", address: "https://localhost:8443", want: "quic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, address, built = tt.transport, tt.address, ""
			_, err := createSchedulerClient()
			require.NoError(t, err)
			assert.Equal(t, tt.want, built)
		})
	}
}

func TestCreateSchedulerClient_UnsupportedTransportIsTyped(t *testing.T) {
	savedTransport := transport
	defer func() { transport = savedTransport }()

	for _, name := range []string{"unsupported_transport_xyz", "embedded"} {
		t.Run(name, func(t *testing.T) {
			transport = name

			client, err := createSchedulerClient()
			assert.Nil(t, client)
			var unsupportedErr *UnsupportedTransportError
			require.ErrorAs(t, err, &unsupportedErr)
			assert.Equal(t, name, unsupportedErr.Transport)
			assert.NotContains(t, err.Error(), "embedded)", "the scheduler must not advertise transports it cannot dial")
		})
	}
}

// TestRootHelp_NamesTheDefaultSocket keeps the root help in step with the
// --address default, which is the socket in the user's home directory.
func TestRootHelp_NamesTheDefaultSocket(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	flag := rootCmd.PersistentFlags().Lookup("address")
	require.NotNil(t, flag)
	assert.Equal(t, filepath.Join(home, "quicbaas.sock"), flag.DefValue)
	assert.Contains(t, rootCmd.Long, "Default connection: $HOME/quicbaas.sock")
}
