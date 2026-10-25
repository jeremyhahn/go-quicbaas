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

//go:build !integration && mcp

package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postMCP sends one JSON-RPC request to the MCP listener with no credential
// and returns the HTTP status, the session id the server assigned, and the
// response body.
func postMCP(t *testing.T, client *http.Client, url, session string, request map[string]any) (int, string, string) {
	t.Helper()
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), string(body)
}

// serverTrustingClient returns an HTTPS client with no client certificate
// that trusts the leaf the server presents on its listeners.
func serverTrustingClient(t *testing.T, srv *Server) *http.Client {
	t.Helper()
	serverTLS := srv.serverTLSConfigRef()
	require.NotNil(t, serverTLS)
	require.NotEmpty(t, serverTLS.Certificates)
	leaf, err := x509.ParseCertificate(serverTLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			ServerName: "localhost",
			MinVersion: tls.VersionTLS13,
		}},
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// TestMCPListener_AnonymousLoginReachesTheLoginTool drives the real
// authorization-enabled MCP listener with no credential. The login tools run
// before the caller has an identity, so the handshake and a login_begin call
// reach the server, while a data tool is still refused 401.
func TestMCPListener_AnonymousLoginReachesTheLoginTool(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Security.TLSEnabled = true
	srv.config.Security.TLSClientAuth = false
	srv.config.Security.RequireAuthz = true
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = getFreePort(t)
	require.NoError(t, srv.initialize())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, srv.Shutdown(ctx))
	})
	client := serverTrustingClient(t, srv)
	url := fmt.Sprintf("https://127.0.0.1:%d/mcp", srv.config.Server.MCP.Port)

	status, session, body := postMCP(t, client, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "anonymous-login-test", "version": "1"},
		},
	})
	require.Equal(t, http.StatusOK, status, "anonymous initialize: %s", body)

	status, _, body = postMCP(t, client, url, session, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "quicbaas_login_begin", "arguments": map[string]any{"type": "mtls"}},
	})
	assert.Equal(t, http.StatusOK, status, "anonymous login_begin: %s", body)

	status, _, body = postMCP(t, client, url, session, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "quicbaas_kms_generate_key", "arguments": map[string]any{"key_id": "k"}},
	})
	assert.Equal(t, http.StatusUnauthorized, status, "anonymous data tool: %s", body)
}
