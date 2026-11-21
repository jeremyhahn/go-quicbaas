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

// mcpServerPolicyWith is a server-format policy whose spec and extra
// top-level fields are given as raw JSON.
func mcpServerPolicyWith(name, spec, extra string) string {
	return `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
		`"metadata":{"name":"` + name + `","tenant":"t1"},"spec":` + spec + extra + `}`
}

func TestMapMCPServerPolicyToSDK_Enabled(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		want   bool
	}{
		{
			name:   "production create response without enabled",
			policy: mcpServerPolicyWith("p1", `{"rules":[]}`, ""),
			want:   true,
		},
		{name: "explicit spec enabled false", policy: mcpServerPolicyWith("p1", `{"enabled":false}`, ""), want: false},
		{name: "explicit spec enabled true", policy: mcpServerPolicyWith("p1", `{"enabled":true}`, ""), want: true},
		{name: "explicit response enabled false", policy: mcpServerPolicyWith("p1", `{}`, `,"enabled":false`), want: false},
		{
			name:   "spec_yaml with spec.enabled false",
			policy: mcpServerPolicyWith("p1", `{}`, `,"spec_yaml":"spec:\n  enabled: false\n"`),
			want:   false,
		},
		{
			name:   "spec_yaml without spec.enabled",
			policy: mcpServerPolicyWith("p1", `{}`, `,"spec_yaml":"spec:\n  rules: []\n"`),
			want:   true,
		},
		{
			name:   "malformed spec_yaml",
			policy: mcpServerPolicyWith("p1", `{}`, `,"spec_yaml":"spec: [unclosed"`),
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := decodeServerMap(t, `{"policy":`+tt.policy+`}`)

			policy := decodeSDKPolicy(t, mapMCPServerPolicyToSDK(resp))

			assert.Equal(t, "p1", policy.Name)
			assert.Equal(t, tt.want, policy.Enabled)
		})
	}
}

func TestMapMCPServerPolicyListToSDK_Enabled(t *testing.T) {
	resp := decodeServerMap(t, `{"policies":[`+
		mcpServerPolicyWith("default", `{"rules":[]}`, "")+`,`+
		mcpServerPolicyWith("disabled", `{"enabled":false}`, "")+`,`+
		mcpServerPolicyWith("yaml-disabled", `{}`, `,"spec_yaml":"spec:\n  enabled: false\n"`)+
		`],"total_count":3}`)

	raw, err := json.Marshal(mapMCPServerPolicyListToSDK(resp))
	require.NoError(t, err)
	var list iam.ListPoliciesResponse
	require.NoError(t, json.Unmarshal(raw, &list))

	require.Len(t, list.Policies, 3)
	assert.True(t, list.Policies[0].Enabled, "a policy without enabled is enabled")
	assert.False(t, list.Policies[1].Enabled, "an explicit enabled false stays false")
	assert.False(t, list.Policies[2].Enabled, "spec_yaml spec.enabled false disables")
}
