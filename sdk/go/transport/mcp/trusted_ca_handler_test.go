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

package mcp

import (
	"context"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapToToolName_TrustedCA pins the #1330 MCP re-export of the go-xkms
// CAService: every method names the server's xkms.ca.* tool.
func TestMapToToolName_TrustedCA(t *testing.T) {
	tr := &Transport{}
	for method, want := range trustedCAMethodTools {
		if got := tr.mapToToolName(trustedCAServiceName, method); got != want {
			t.Errorf("mapToToolName(%q) = %q, want %q", method, got, want)
		}
	}
	assert.Len(t, trustedCAMethodTools, 7)
}

// TestTrustedCASignCSR_OverMCP drives Security().TrustedCA().SignCSR's wire
// form end to end: csr_pem travels as the PEM text the xkms.ca.signCSR tool
// takes, and the tool's result decodes into go-xkms's SignCSRResponse.
func TestTrustedCASignCSR_OverMCP(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "xkms.ca.signCSR",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assert.Equal(t, "-----BEGIN CERTIFICATE REQUEST-----", args["csr_pem"])
			assert.Equal(t, "server", args["profile"])
		},
		// The server's go-xkms response, JSON-encoded ([]byte as base64).
		result: map[string]interface{}{"certificate_pem": "TEVBRg==", "serial_number": "0a"},
	})
	defer cleanup()

	resp, err := tr.Request(context.Background(), trustedCAServiceName, "SignCSR",
		&xkmstransport.SignCSRRequest{CSRPEM: []byte("-----BEGIN CERTIFICATE REQUEST-----"), Profile: "server"})
	require.NoError(t, err)
	out, ok := resp.(*xkmstransport.SignCSRResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, "LEAF", string(out.CertificatePEM))
	assert.Equal(t, "0a", out.SerialNumber)
}

// TestTrustedCAIsRevoked_OverMCP checks a second verb's arguments and result.
func TestTrustedCAIsRevoked_OverMCP(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "xkms.ca.isRevoked",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assert.Equal(t, "0a", args["serial_number"])
		},
		result: map[string]interface{}{"revoked": true},
	})
	defer cleanup()

	resp, err := tr.Request(context.Background(), trustedCAServiceName, "IsRevoked",
		&xkmstransport.IsRevokedRequest{SerialNumber: "0a"})
	require.NoError(t, err)
	out, ok := resp.(*xkmstransport.IsRevokedResponse)
	require.True(t, ok, "got %T", resp)
	assert.True(t, out.Revoked)
}

// TestTrustedCATransformRequest_RejectsBadInput keeps the typed contract.
func TestTrustedCATransformRequest_RejectsBadInput(t *testing.T) {
	_, err := trustedCATransformRequest("SignCSR", nil)
	require.Error(t, err)
	_, err = trustedCATransformRequest("SignCSR", &xkmstransport.IsRevokedRequest{})
	require.Error(t, err)
	_, err = trustedCATransformRequest("NoSuchMethod", &xkmstransport.IsRevokedRequest{})
	require.Error(t, err)
	_, err = trustedCATransformResponse("NoSuchMethod", nil)
	require.Error(t, err)
}
