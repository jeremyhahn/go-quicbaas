// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// mcpServerPolicyJSON is a server-format policy as the quicbaas_iam_policy
// tools return it; loadError is appended verbatim when non-empty.
func mcpServerPolicyJSON(name, loadError string) string {
	policy := `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
		`"metadata":{"name":"` + name + `","tenant":"t1"},"spec":{}`
	if loadError != "" {
		policy += `,"load_error":` + loadError
	}
	return policy + `}`
}

const mcpLoadErrorJSON = `{"reason":"json: unknown field \"bogus\"","unknown_keys":["spec.rules[0].bogus"]}`

// decodeSDKPolicy decodes a mapped policy into the SDK type, as the
// transport does for a caller.
func decodeSDKPolicy(t *testing.T, mapped interface{}) iam.Policy {
	t.Helper()
	raw, err := json.Marshal(mapped)
	require.NoError(t, err)
	var policy iam.Policy
	require.NoError(t, json.Unmarshal(raw, &policy))
	return policy
}

func decodeServerMap(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	return m
}

func assertSDKFlagged(t *testing.T, policy iam.Policy) {
	t.Helper()
	require.NotNil(t, policy.LoadError, "a flagged policy must carry load_error")
	assert.Equal(t, `json: unknown field "bogus"`, policy.LoadError.Reason)
	assert.Equal(t, []string{"spec.rules[0].bogus"}, policy.LoadError.UnknownKeys)
}

func TestMapMCPServerPolicyToSDK_LoadError(t *testing.T) {
	tests := []struct {
		name      string
		loadError string
	}{
		{name: "flagged policy carries load_error", loadError: mcpLoadErrorJSON},
		{name: "healthy policy has none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := decodeServerMap(t, `{"policy":`+mcpServerPolicyJSON("p1", tt.loadError)+`}`)

			policy := decodeSDKPolicy(t, mapMCPServerPolicyToSDK(resp))

			assert.Equal(t, "p1", policy.Name)
			assert.Equal(t, "t1", policy.TenantID)
			if tt.loadError == "" {
				assert.Nil(t, policy.LoadError)
				return
			}
			assertSDKFlagged(t, policy)
		})
	}
}

func TestMapMCPServerPolicyToSDK_NonServerShapePassesThrough(t *testing.T) {
	resp := map[string]interface{}{"name": "p1", "load_error": "ignored"}

	assert.Equal(t, resp, mapMCPServerPolicyToSDK(resp), "an SDK-shaped response is returned unchanged")
}

func TestMapMCPServerPolicyListToSDK_LoadError(t *testing.T) {
	resp := decodeServerMap(t, `{"policies":[`+
		mcpServerPolicyJSON("flagged", mcpLoadErrorJSON)+`,`+
		mcpServerPolicyJSON("healthy", "")+`],"total_count":2}`)

	raw, err := json.Marshal(mapMCPServerPolicyListToSDK(resp))
	require.NoError(t, err)
	var list iam.ListPoliciesResponse
	require.NoError(t, json.Unmarshal(raw, &list))

	require.Len(t, list.Policies, 2)
	assert.Equal(t, "flagged", list.Policies[0].Name)
	assertSDKFlagged(t, list.Policies[0])
	assert.Equal(t, "healthy", list.Policies[1].Name)
	assert.Nil(t, list.Policies[1].LoadError)
}

func TestMapMCPServerPolicyListToSDK_MalformedListPassesThrough(t *testing.T) {
	resp := map[string]interface{}{"policies": "not-a-list"}

	assert.Equal(t, resp, mapMCPServerPolicyListToSDK(resp))
}
