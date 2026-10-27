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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapMCPServerPolicyToSDK_ReportsStoredEnabled covers issue #1929: an
// absent spec.enabled means enabled, an explicit value is reported as
// stored, and a flagged entry, never applied, reports disabled.
func TestMapMCPServerPolicyToSDK_ReportsStoredEnabled(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{name: "spec.enabled absent", json: `{"policy":` + mcpServerPolicyJSON("p", "") + `}`, want: true},
		{
			name: "spec.enabled false",
			json: `{"policy":{"apiVersion":"v1","metadata":{"name":"p"},"spec":{"enabled":false}}}`,
			want: false,
		},
		{
			name: "spec.enabled true",
			json: `{"apiVersion":"v1","metadata":{"name":"p"},"spec":{"enabled":true}}`,
			want: true,
		},
		{name: "load error", json: mcpServerPolicyJSON("p", mcpLoadErrorJSON), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(tt.json), &resp))
			policy := decodeSDKPolicy(t, mapMCPServerPolicyToSDK(resp))
			assert.Equal(t, tt.want, policy.Enabled)
		})
	}
}
