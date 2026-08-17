//go:build !integration && rest && quic && mcp

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
