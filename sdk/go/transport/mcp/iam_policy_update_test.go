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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// serverPolicyUpdateKeys are the SDK-format keys the server tool
// quicbaas_iam_policy_update accepts inside "policy"; any other key is
// refused there as untranslated.
var serverPolicyUpdateKeys = map[string]bool{
	"name":        true,
	"description": true,
	"labels":      true,
	"priority":    true,
	"spec_yaml":   true,
	"version":     true,
	"enabled":     true,
	"effect":      true,
}

// policyUpdateWire mirrors the request the iam policy client sends for
// Update.
type policyUpdateWire struct {
	TenantID   string                   `json:"tenant_id"`
	PolicyName string                   `json:"policy_name"`
	Request    *iam.UpdatePolicyRequest `json:"request"`
}

// TestIAMHandler_TransformRequest_PolicyUpdateShape checks that a policy
// update leaves the SDK in the shape the server tool reads:
// {tenant_id, name, policy: {...}} with spec_yaml as raw YAML.
func TestIAMHandler_TransformRequest_PolicyUpdateShape(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)
	handler := newIAMHandler(tr)

	description := "updated"
	priority := 7
	spec := "apiVersion: quicbaas.io/v1\nkind: Policy\n"

	tests := []struct {
		name       string
		req        *iam.UpdatePolicyRequest
		wantPolicy map[string]interface{}
	}{
		{
			name: "metadata only",
			req: &iam.UpdatePolicyRequest{
				Description: &description,
				Priority:    &priority,
				Labels:      map[string]string{"team": "a"},
			},
			wantPolicy: map[string]interface{}{
				"description": description,
				"priority":    float64(priority),
				"labels":      map[string]interface{}{"team": "a"},
			},
		},
		{
			name: "carrying a spec",
			req: &iam.UpdatePolicyRequest{
				Description: &description,
				SpecYAML:    []byte(spec),
			},
			wantPolicy: map[string]interface{}{
				"description": description,
				"spec_yaml":   spec,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := &policyUpdateWire{TenantID: "tenant-a", PolicyName: "p1", Request: tt.req}
			args, err := handler.transformRequest("quicbaas.IAM.Policy", "Update", wire)
			require.NoError(t, err)

			assert.Equal(t, "tenant-a", args["tenant_id"])
			assert.Equal(t, "p1", args["name"])
			assert.NotContains(t, args, "policy_name")
			assert.Len(t, args, 3, "only tenant_id, name and policy reach the tool: %v", args)

			policy, ok := args["policy"].(map[string]interface{})
			require.True(t, ok, "policy must be an object, got %T", args["policy"])
			assert.Equal(t, tt.wantPolicy, policy)
			for key := range policy {
				assert.True(t, serverPolicyUpdateKeys[key], "server tool refuses policy key %q", key)
			}
		})
	}
}

// TestIAMHandler_TransformRequest_PolicyUpdateRefusesBeforeRegroup checks
// that the structured-field refusal still sees the update fields, which the
// regroup moves under "policy".
func TestIAMHandler_TransformRequest_PolicyUpdateRefusesBeforeRegroup(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)
	handler := newIAMHandler(tr)

	effect := iam.EffectAllow
	wire := &policyUpdateWire{TenantID: "tenant-a", PolicyName: "p1", Request: &iam.UpdatePolicyRequest{Effect: &effect}}
	args, err := handler.transformRequest("quicbaas.IAM.Policy", "Update", wire)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "effect")
	assert.Nil(t, args)
}

func TestRenamePolicyName(t *testing.T) {
	args := map[string]interface{}{"policy_name": "p1"}
	renamePolicyName(args)
	assert.Equal(t, map[string]interface{}{"name": "p1"}, args)

	// An explicit name wins; policy_name is still dropped.
	args = map[string]interface{}{"policy_name": "p1", "name": "p2"}
	renamePolicyName(args)
	assert.Equal(t, map[string]interface{}{"name": "p2"}, args)

	// No policy_name leaves the arguments alone.
	args = map[string]interface{}{"x": 1}
	renamePolicyName(args)
	assert.Equal(t, map[string]interface{}{"x": 1}, args)
}
