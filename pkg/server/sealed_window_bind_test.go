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

// TestSealedListenAddress_IgnoresServerHost covers the sealed window's own
// bind host: a wildcard server.host for the post-unseal listeners does not
// widen the sealed listeners, which stay on the sealed bind host (loopback
// by default).
func TestSealedListenAddress_IgnoresServerHost(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Host = "0.0.0.0"

	assert.Equal(t, "127.0.0.1:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
	assert.Equal(t, "127.0.0.1:9443", sealedListenAddress(cfg, cfg.Server.GRPC.Port))
	assert.Equal(t, "127.0.0.1:11443", sealedListenAddress(cfg, cfg.Server.MCP.Port))
}

// TestSealedListenAddress_IPv6HostJoined asserts IPv6 sealed bind hosts are
// bracketed correctly (net.JoinHostPort semantics).
func TestSealedListenAddress_IPv6HostJoined(t *testing.T) {
	cfg := config.Default()
	cfg.Server.SealedBindHost = "::1"

	assert.Equal(t, "[::1]:8443", sealedListenAddress(cfg, cfg.Server.REST.Port))
}
