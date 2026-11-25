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

//go:build integration

package cli

import "testing"

// TestCredentialNotConfiguredAnswer checks the credential probe classifier
// with no cluster and no exec: only each transport's typed not-configured
// answer is accepted, and a 401/403 refusal or a sanitized error never is.
func TestCredentialNotConfiguredAnswer(t *testing.T) {
	tests := []struct {
		name      string
		transport Transport
		stderr    string
		want      bool
	}{
		{"grpc not configured", TransportGRPC, "Error: transport: unsupported capability: grpc: credential service is not configured", true},
		{"unix not configured", TransportUnix, "Error: transport: unsupported capability: grpc: credential service is not configured", true},
		{"rest 404 parsed", TransportREST, "Error: NOT_FOUND: rest.parseErrorResponse: not found", true},
		{"quic 404 page", TransportQUIC, "Error: NOT_FOUND: rest.parseErrorResponse: HTTP 404: 404 page not found", true},
		{"mcp service not configured", TransportMCP, "Error: INTERNAL: mcp.callTool: credential service not configured", true},
		{"mcp tool not found", TransportMCP, "Error: INVALID_INPUT: mcp.callTool: invalid request: tool 'qrdb_credential_list' not found: tool not found", true},

		{"rest 401 bearer missing", TransportREST, "Error: transport: request CredentialSubmit failed (status 401): auth/middleware: all authenticators failed: platform bearer authorization is missing", false},
		{"quic 401 bearer missing", TransportQUIC, "Error: transport: request CredentialSubmit failed (status 401): auth/middleware: all authenticators failed: platform bearer authorization is missing", false},
		{"mcp 401 at initialization", TransportMCP, "Error: transport: connection failed to https://test-node1:8888/mcp: mcp transport: initialization failed: transport error: unauthorized (401)", false},
		{"rest 403 wins over not configured", TransportREST, "Error: FORBIDDEN: rest.parseErrorResponse: credential service is not configured", false},
		{"grpc permission denied wins", TransportGRPC, "PermissionDenied: credential service is not configured", false},
		{"mcp sanitized internal", TransportMCP, "Error: INTERNAL: mcp.callTool: the request could not be completed", false},
		{"rest shape on grpc leg", TransportGRPC, "Error: NOT_FOUND: rest.parseErrorResponse: not found", false},
		{"embedded has no entry", TransportEmbedded, "credential service is not configured", false},
		{"rest empty output", TransportREST, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := credentialNotConfiguredAnswer(tt.transport, "", tt.stderr); got != tt.want {
				t.Errorf("credentialNotConfiguredAnswer(%s, %q) = %v, want %v", tt.transport, tt.stderr, got, tt.want)
			}
		})
	}
}
