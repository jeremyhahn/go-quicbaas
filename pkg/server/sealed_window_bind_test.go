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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestSealedListenAddress_LoopbackDefault asserts the sealed-window listen
// address is built from the sealed bind host — loopback by default — and NOT
// from Server.Host, so a 0.0.0.0 full-boot deployment does not expose the
// pre-unseal ceremony/unseal surface network-wide.
func TestSealedListenAddress_LoopbackDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.SealedBindHost = ""

	assert.Equal(t, "127.0.0.1:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
	assert.Equal(t, "127.0.0.1:9443", sealedListenAddress(cfg, cfg.Server.GRPC.Port))
	assert.Equal(t, "127.0.0.1:10443", sealedListenAddress(cfg, cfg.Server.QUIC.Port))
	assert.Equal(t, "127.0.0.1:11443", sealedListenAddress(cfg, cfg.Server.MCP.Port))
}

// TestSealedListenAddress_ExplicitOptIn asserts an explicit SealedBindHost
// (remote Shamir officer opt-in) is used verbatim for the sealed listeners.
func TestSealedListenAddress_ExplicitOptIn(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.SealedBindHost = "0.0.0.0"

	assert.Equal(t, "0.0.0.0:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
	assert.Equal(t, "0.0.0.0:9443", sealedListenAddress(cfg, cfg.Server.GRPC.Port))
}

// TestSealedListenAddress_IgnoresPerProtocolBindAddress covers the old
// sealed-REST bug: the listener config address was computed from
// ResolveBindHost(REST.BindAddress) while Start() received an address built
// from Server.Host, silently producing two different addresses. The sealed
// address is now computed exactly once from the sealed bind host; the
// per-protocol BindAddress overrides govern only the post-unseal listeners.
func TestSealedListenAddress_IgnoresPerProtocolBindAddress(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.REST.BindAddress = "10.0.0.1"
	cfg.Server.GRPC.BindAddress = "10.0.0.2"
	cfg.Server.QUIC.BindAddress = "10.0.0.3"
	cfg.Server.MCP.BindAddress = "10.0.0.4"

	assert.Equal(t, "127.0.0.1:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
	assert.Equal(t, "127.0.0.1:9443", sealedListenAddress(cfg, cfg.Server.GRPC.Port))
	assert.Equal(t, "127.0.0.1:10443", sealedListenAddress(cfg, cfg.Server.QUIC.Port))
	assert.Equal(t, "127.0.0.1:11443", sealedListenAddress(cfg, cfg.Server.MCP.Port))

	// The full-boot resolution path is untouched: BindAddress still wins there.
	assert.Equal(t, "10.0.0.1", cfg.Server.ResolveBindHost(cfg.Server.REST.BindAddress))
}

// TestSealedListenAddress_IPv6HostJoined asserts IPv6 sealed bind hosts are
// bracketed correctly (net.JoinHostPort semantics).
func TestSealedListenAddress_IPv6HostJoined(t *testing.T) {
	cfg := config.Default()
	cfg.Server.SealedBindHost = "::1"

	assert.Equal(t, "[::1]:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
}
