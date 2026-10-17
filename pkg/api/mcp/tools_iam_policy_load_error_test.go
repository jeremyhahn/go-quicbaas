//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// flaggedMCPPolicy is the entry the engine returns for a stored record that
// did not decode: its name and tenant, no rules, and the load error.
func flaggedMCPPolicy() *iam.Policy {
	return &iam.Policy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "p1", Tenant: "t1"},
		LoadError: &iam.StoredPolicyDecodeError{
			TenantID:    "t1",
			Name:        "p1",
			Reason:      `json: unknown field "bogus"`,
			UnknownKeys: []string{"spec.rules[0].bogus"},
		},
	}
}

func healthyMCPPolicy() *iam.Policy {
	return &iam.Policy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "ok", Tenant: "t1"},
		Spec:       iam.PolicySpec{Rules: []iam.Rule{{Name: "allow-read", Effect: iam.EffectAllow}}},
	}
}

// policyLoadErrorOf decodes the load_error of one policy in an MCP result.
func policyLoadErrorOf(t *testing.T, policy interface{}) *iam.PolicyLoadErrorView {
	t.Helper()
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	var view struct {
		LoadError *iam.PolicyLoadErrorView `json:"load_error"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	return view.LoadError
}

func assertMCPFlaggedLoadError(t *testing.T, loadErr *iam.PolicyLoadErrorView) {
	t.Helper()
	require.NotNil(t, loadErr, "a flagged record must carry load_error")
	assert.Equal(t, `json: unknown field "bogus"`, loadErr.Reason)
	assert.Equal(t, []string{"spec.rules[0].bogus"}, loadErr.UnknownKeys)
}

func TestExecuteIAMPolicyGet_LoadError(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	tests := []struct {
		name    string
		policy  *iam.Policy
		flagged bool
	}{
		{name: "flagged record carries load_error", policy: flaggedMCPPolicy(), flagged: true},
		{name: "well-formed policy has none", policy: healthyMCPPolicy()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newIAMTestRegistry(&mockIAMService{
				getPolicyFunc: func(context.Context, *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
					return &service.GetPolicyResponse{Policy: tt.policy}, nil
				},
			})

			result, err := reg.executeIAMTool(ctx, "quicbaas_iam_policy_get",
				map[string]interface{}{"tenant_id": "t1", "name": tt.policy.Metadata.Name})
			require.NoError(t, err)
			resp, ok := result.(map[string]interface{})
			require.True(t, ok, "result = %T", result)
			policy, ok := resp["policy"].(map[string]interface{})
			require.True(t, ok, "policy = %T", resp["policy"])
			assert.Contains(t, policy, "metadata", "the policy document is still returned")

			loadErr := policyLoadErrorOf(t, policy)
			if !tt.flagged {
				assert.Nil(t, loadErr)
				return
			}
			assertMCPFlaggedLoadError(t, loadErr)
		})
	}
}

func TestExecuteIAMPolicyList_LoadError(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	reg := newIAMTestRegistry(&mockIAMService{
		listPoliciesFunc: func(context.Context, *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error) {
			return &service.ListPoliciesResponse{
				Policies:   []*iam.Policy{flaggedMCPPolicy(), healthyMCPPolicy()},
				TotalCount: 2,
			}, nil
		},
	})

	result, err := reg.executeIAMTool(ctx, "quicbaas_iam_policy_list", map[string]interface{}{"tenant_id": "t1"})
	require.NoError(t, err)
	resp, ok := result.(map[string]interface{})
	require.True(t, ok, "result = %T", result)
	assert.EqualValues(t, 2, resp["total_count"])
	policies, ok := resp["policies"].([]interface{})
	require.True(t, ok, "policies = %T", resp["policies"])
	require.Len(t, policies, 2)

	assertMCPFlaggedLoadError(t, policyLoadErrorOf(t, policies[0]))
	assert.Nil(t, policyLoadErrorOf(t, policies[1]), "a well-formed policy has no load_error")
}

func TestExecuteIAMPolicyGet_ServiceErrorPropagates(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	notFound := &iam.PolicyNotFoundTypeError{TenantID: "t1", Name: "missing"}
	reg := newIAMTestRegistry(&mockIAMService{
		getPolicyFunc: func(context.Context, *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
			return nil, notFound
		},
	})

	_, err := reg.executeIAMTool(ctx, "quicbaas_iam_policy_get", map[string]interface{}{"tenant_id": "t1", "name": "missing"})
	assert.ErrorIs(t, err, notFound)
}
