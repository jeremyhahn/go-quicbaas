//go:build !integration && rest && quic && mcp && !raft && !grpc

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//   See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//   Contact licensing@automatethethings.com for commercial licensing options.

package server

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// lifecycle_rest.go: startRESTServer and stopRESTServer
// =============================================================================

func TestLCProto_StartRESTServer_EmbeddedMode(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	assert.NoError(t, srv.startRESTServer())
	assert.Nil(t, srv.restServer)
}

func TestLCProto_StartRESTServer_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = false
	assert.NoError(t, srv.startRESTServer())
	assert.Nil(t, srv.restServer)
}

func TestLCProto_StopRESTServer_NilServer(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.restServer = nil
	assert.NoError(t, srv.stopRESTServer(context.Background()))
}

// =============================================================================
// lifecycle_quic.go: startQUICServer and stopQUICServer
// =============================================================================

func TestLCProto_StartQUICServer_EmbeddedMode(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	assert.NoError(t, srv.startQUICServer())
	assert.Nil(t, srv.quicServer)
}

func TestLCProto_StartQUICServer_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = false
	assert.NoError(t, srv.startQUICServer())
	assert.Nil(t, srv.quicServer)
}

func TestLCProto_StartQUICServer_ZeroPort(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 0
	assert.NoError(t, srv.startQUICServer())
	assert.Nil(t, srv.quicServer)
}

func TestLCProto_StartQUICServer_TLSDisabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 9443
	srv.config.Security.TLSEnabled = false
	assert.NoError(t, srv.startQUICServer())
	assert.Nil(t, srv.quicServer)
}

func TestLCProto_StartQUICServer_NilTLSConfig(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 9443
	srv.config.Security.TLSEnabled = true
	srv.tlsConfig = nil

	err := srv.startQUICServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS")
}

func TestLCProto_StopQUICServer_NilServer(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.quicServer = nil
	assert.NoError(t, srv.stopQUICServer())
}

// =============================================================================
// lifecycle_mcp.go: startMCPServer and stopMCPServer
// =============================================================================

func TestLCProto_StartMCPServer_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.MCP.Enabled = false
	assert.NoError(t, srv.startMCPServer())
	assert.Nil(t, srv.mcpServer)
}

func TestLCProto_StopMCPServer_NilServer(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.mcpServer = nil
	assert.NoError(t, srv.stopMCPServer())
}

// =============================================================================
// lifecycle_rest.go: cloneTLSConfig
// =============================================================================

func TestLCProto_CloneTLSConfig_NilReturnsNil(t *testing.T) {
	t.Parallel()
	result := cloneTLSConfig(nil)
	assert.Nil(t, result)
}

func TestLCProto_CloneTLSConfig_SetsHTTP11(t *testing.T) {
	t.Parallel()
	original := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	}
	cloned := cloneTLSConfig(original)
	assert.NotNil(t, cloned)
	assert.Equal(t, []string{"http/1.1"}, cloned.NextProtos)
	// Original should not be modified
	assert.Equal(t, []string{"h2", "http/1.1"}, original.NextProtos)
}
