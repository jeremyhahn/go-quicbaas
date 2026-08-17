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
		MCP:  ServerMCPConfig{BindAddress: "127.0.0.1"},
		Unix: ServerUnixConfig{},
	}

	assert.Equal(t, "10.0.0.1", sc.ResolveBindHost(sc.REST.BindAddress))
	assert.Equal(t, "10.0.0.2", sc.ResolveBindHost(sc.GRPC.BindAddress))
	assert.Equal(t, "127.0.0.1", sc.ResolveBindHost(sc.MCP.BindAddress))
	assert.Equal(t, "0.0.0.0", sc.ResolveBindHost(sc.Unix.BindAddress))
}

// TestResolveSealedBindHostDefaultsToLoopback verifies that an unset
// SealedBindHost resolves to loopback even when Server.Host binds all
// interfaces — the sealed-window surface must never inherit Host.
func TestResolveSealedBindHostDefaultsToLoopback(t *testing.T) {
	sc := &ServerConfig{Host: "0.0.0.0"}
	assert.Equal(t, "127.0.0.1", sc.ResolveSealedBindHost())
}

// TestResolveSealedBindHostExplicitValueWins verifies that an explicit
// SealedBindHost (remote-officer opt-in) is returned verbatim.
func TestResolveSealedBindHostExplicitValueWins(t *testing.T) {
	sc := &ServerConfig{Host: "127.0.0.1", SealedBindHost: "0.0.0.0"}
	assert.Equal(t, "0.0.0.0", sc.ResolveSealedBindHost())
}

// TestResolveSealedBindHostEmptyFallsBack verifies the empty-string fallback
// on a zero-value config resolves to the loopback default.
func TestResolveSealedBindHostEmptyFallsBack(t *testing.T) {
	sc := &ServerConfig{}
	assert.Equal(t, DefaultSealedBindHost, sc.ResolveSealedBindHost())
}

// TestSealedBindHostDefaultConfigs verifies every config constructor seeds
// SealedBindHost with the loopback default, including the production preset
// that binds Server.Host to 0.0.0.0.
func TestSealedBindHostDefaultConfigs(t *testing.T) {
	assert.Equal(t, DefaultSealedBindHost, Default().Server.SealedBindHost)
	assert.Equal(t, DefaultSealedBindHost, DefaultConfig().Server.SealedBindHost)
	assert.Equal(t, DefaultSealedBindHost, DefaultServerConfig().SealedBindHost)
	assert.Equal(t, DefaultSealedBindHost, TestConfig().Server.SealedBindHost)

	prod := ProductionConfig()
	assert.Equal(t, "0.0.0.0", prod.Server.Host)
	assert.Equal(t, DefaultSealedBindHost, prod.Server.ResolveSealedBindHost(),
		"production preset must keep the sealed window on loopback")
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

// TestRequireAuthzDisabledInDev verifies the dev preset opts out of the gate
// so local anonymous data-plane access keeps working during development.
func TestRequireAuthzDisabledInDev(t *testing.T) {
	assert.False(t, DevelopmentConfig().Security.RequireAuthz)
}

// TestEmbeddedConfigSecureDefaults verifies that EmbeddedConfig uses secure
// defaults: RequireAuthz=true, AllowAnonymous=false, and a non-empty system
// principal. All network transports are disabled so RequireAuthz has no
// network-surface effect, but the flag documents intent and the RBAC posture
// ensures service-layer guards enforce identity checks.
func TestEmbeddedConfigSecureDefaults(t *testing.T) {
	cfg := EmbeddedConfig()
	assert.True(t, cfg.Security.RequireAuthz, "EmbeddedConfig must keep RequireAuthz=true (secure default)")
	assert.False(t, cfg.Security.Authorization.RBAC.AllowAnonymous, "EmbeddedConfig must not allow anonymous access")
	assert.NotEmpty(t, cfg.Security.EmbeddedSystemPrincipal, "EmbeddedConfig must name a system principal")
	assert.Equal(t, EmbeddedSystemPrincipalDefault, cfg.Security.EmbeddedSystemPrincipal)
}

// TestRequireAuthzSettable verifies the flag can be toggled off.
func TestRequireAuthzSettable(t *testing.T) {
	sc := &SecurityConfig{RequireAuthz: true}
	assert.True(t, sc.RequireAuthz)
	sc.RequireAuthz = false
	assert.False(t, sc.RequireAuthz)
}
