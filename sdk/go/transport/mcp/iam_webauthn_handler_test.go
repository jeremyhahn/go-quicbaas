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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMapWebAuthnToolName pins the SDK-verb-to-MCP-tool mapping. Without these
// entries the credential verbs resolve to no tool at all and the MCP transport
// fails the call, while REST works — the shape of the defect this fixes.
//
// The tool names must match the ones pkg/api/mcp registers; the server-side
// contract test (TestSDKToolNameContract) is what holds the two in step.
func TestMapWebAuthnToolName(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		expected string
	}{
		{"List", "ListWebAuthnCredentials", "quicbaas_iam_webauthn_credentials_list"},
		{"DeleteOne", "DeleteWebAuthnCredential", "quicbaas_iam_webauthn_credential_delete"},
		{"DeleteAll", "DeleteWebAuthnCredentials", "quicbaas_iam_webauthn_credentials_delete_all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, found := mapIAMToolName("quicbaas.IAM.WebAuthn", tt.method)
			assert.True(t, found, "the WebAuthn verb must resolve to an MCP tool")
			assert.Equal(t, tt.expected, toolName)
		})
	}

	t.Run("UnknownMethod", func(t *testing.T) {
		_, found := mapIAMToolName("quicbaas.IAM.WebAuthn", "Frobnicate")
		assert.False(t, found)
	})
}

// TestWebAuthnIsMCPIAMService keeps the credential service inside the IAM
// routing branch, so the transport hands it to the IAM handler rather than
// treating it as an unknown service.
func TestWebAuthnIsMCPIAMService(t *testing.T) {
	assert.True(t, isMCPIAMService("quicbaas.IAM.WebAuthn"))
}
