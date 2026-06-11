//go:build raft && rest && quic && mcp && vector && !integration

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

// start_mcp_credential_test.go covers the credential-service wiring branch of
// startMCPServer: when the bootstrap ceremony services expose a credential
// service, startMCPServer wires it into the MCP server so the qrdb_credential_*
// tools serve real responses.

package server

import (
	"context"
	"log/slog"
	"testing"
	"time"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"

	"github.com/stretchr/testify/require"
)

// TestStartMCPServer_WiresCredentialService drives the
// ceremonyServices.Credential != nil branch of startMCPServer.
func TestStartMCPServer_WiresCredentialService(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = 0

	// Wire bootstrap ceremony services so the credential-wiring branch runs.
	bundle, err := buildBootstrapCeremonyServices(slog.Default(), t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, bundle.Credential)
	srv.ceremonyServices = bundle

	seamMu.Lock()
	mcpStopFn = func(*mcpapi.Server) error { return nil }
	seamMu.Unlock()

	require.NoError(t, srv.startMCPServer())
	require.NotNil(t, srv.mcpServer)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
