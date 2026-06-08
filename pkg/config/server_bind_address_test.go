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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveBindHostFallsBackToServerHost verifies that an empty per-protocol
// override resolves to the shared Server.Host, preserving current behavior.
func TestResolveBindHostFallsBackToServerHost(t *testing.T) {
	sc := &ServerConfig{Host: "0.0.0.0"}
	assert.Equal(t, "0.0.0.0", sc.ResolveBindHost(""))
}

// TestResolveBindHostOverride verifies that a non-empty per-protocol override
// wins over Server.Host.
func TestResolveBindHostOverride(t *testing.T) {
	sc := &ServerConfig{Host: "0.0.0.0"}
	assert.Equal(t, "127.0.0.1", sc.ResolveBindHost("127.0.0.1"))
}

// TestResolveBindHostPerProtocol verifies that each protocol's BindAddress is
// resolved independently: a private-ingress override on one listener does not
// affect the others, which keep Server.Host.
func TestResolveBindHostPerProtocol(t *testing.T) {
	sc := &ServerConfig{
		Host: "0.0.0.0",
		REST: ServerRESTConfig{BindAddress: "10.0.0.1"},
		GRPC: ServerGRPCConfig{BindAddress: "10.0.0.2"},
		QUIC: ServerQUICConfig{}, // unset -> Server.Host
		MCP:  ServerMCPConfig{BindAddress: "127.0.0.1"},
		Unix: ServerUnixConfig{},
	}

	assert.Equal(t, "10.0.0.1", sc.ResolveBindHost(sc.REST.BindAddress))
	assert.Equal(t, "10.0.0.2", sc.ResolveBindHost(sc.GRPC.BindAddress))
	assert.Equal(t, "0.0.0.0", sc.ResolveBindHost(sc.QUIC.BindAddress))
	assert.Equal(t, "127.0.0.1", sc.ResolveBindHost(sc.MCP.BindAddress))
	assert.Equal(t, "0.0.0.0", sc.ResolveBindHost(sc.Unix.BindAddress))
}

// TestRequireMTLSDefaultFalse verifies the strict-mTLS posture flag defaults to
// false (opportunistic mTLS, current behavior).
func TestRequireMTLSDefaultFalse(t *testing.T) {
	sc := &SecurityConfig{}
	assert.False(t, sc.RequireMTLS)
}

// TestRequireAuthzSecureDefault verifies the default-deny authorization gate is
// the secure default: a config produced by Default()/DefaultConfig() has the gate
// ON, so the REST surface denies token-less requests to protected paths.
func TestRequireAuthzSecureDefault(t *testing.T) {
	assert.True(t, Default().Security.RequireAuthz)
	assert.True(t, DefaultConfig().Security.RequireAuthz)
	assert.True(t, DefaultSecurityConfig().RequireAuthz)
}

// TestRequireAuthzDisabledInDevAndEmbedded verifies the dev/embedded presets opt
// out of the gate so local anonymous data-plane access keeps working.
func TestRequireAuthzDisabledInDevAndEmbedded(t *testing.T) {
	assert.False(t, DevelopmentConfig().Security.RequireAuthz)
	assert.False(t, EmbeddedConfig().Security.RequireAuthz)
}

// TestRequireAuthzSettable verifies the flag can be toggled off.
func TestRequireAuthzSettable(t *testing.T) {
	sc := &SecurityConfig{RequireAuthz: true}
	assert.True(t, sc.RequireAuthz)
	sc.RequireAuthz = false
	assert.False(t, sc.RequireAuthz)
}
