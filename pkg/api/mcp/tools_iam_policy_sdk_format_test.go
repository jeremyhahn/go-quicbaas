//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const sdkReadDocsSpec = `rules:
  - name: "read-docs"
    actions: ["read"]
    resources: ["documents"]
    effect: "allow"`

// newStoredPolicyService returns an engine-backed IAM double holding policy
// p1 in tenant t1, created through the MCP create tool in SDK format.
func newStoredPolicyService(t *testing.T) (*ToolRegistry, iam.PolicyStorage, context.Context) {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	iamSvc, storage := newEnginePolicyService()
	iamSvc.getPolicyFunc = func(ctx context.Context, req *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
		policy, err := storage.GetPolicy(ctx, req.TenantID, req.Name)
		if err != nil {
			return nil, err
		}
		return &service.GetPolicyResponse{Policy: policy}, nil
	}
	reg := newIAMTestRegistry(iamSvc)
	_, err := reg.executeIAMPolicyCreate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    map[string]interface{}{"name": "p1", "description": "old", "spec_yaml": sdkReadDocsSpec},
	})
	require.NoError(t, err)
	return reg, storage, ctx
}

func TestExecuteIAMPolicyCreate_SDKFormatRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy map[string]interface{}
	}{
		{"no_spec_yaml", map[string]interface{}{"name": "p2"}},
		{"untranslated_rules", map[string]interface{}{"name": "p2", "spec_yaml": sdkReadDocsSpec, "rules": []interface{}{}}},
		{"enabled_false", map[string]interface{}{"name": "p2", "spec_yaml": sdkReadDocsSpec, "enabled": false}},
		{"effect_allow", map[string]interface{}{"name": "p2", "spec_yaml": sdkReadDocsSpec, "effect": float64(1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, storage, ctx := newStoredPolicyService(t)
			_, err := reg.executeIAMPolicyCreate(ctx, map[string]interface{}{"tenant_id": "t1", "policy": tc.policy})
			assert.True(t, errors.IsInvalidInput(err), "error = %v, want INVALID_INPUT", err)
			_, getErr := storage.GetPolicy(ctx, "t1", "p2")
			assert.Error(t, getErr, "refused create stored a policy")
		})
	}
}

func TestExecuteIAMPolicyCreate_SDKFormatStoresRules(t *testing.T) {
	_, storage, ctx := newStoredPolicyService(t)
	stored, err := storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, "old", stored.Metadata.Description)
}

func TestExecuteIAMPolicyUpdate_SDKFormatRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy map[string]interface{}
	}{
		{"untranslated_targets", map[string]interface{}{"description": "new", "targets": []interface{}{}}},
		{"enabled_false", map[string]interface{}{"enabled": false}},
		{"effect_allow", map[string]interface{}{"effect": "allow"}},
		{"invalid_spec_yaml", map[string]interface{}{"spec_yaml": "rules: [unclosed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, storage, ctx := newStoredPolicyService(t)
			_, err := reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{"tenant_id": "t1", "name": "p1", "policy": tc.policy})
			assert.True(t, errors.IsInvalidInput(err), "error = %v, want INVALID_INPUT", err)
			stored, getErr := storage.GetPolicy(ctx, "t1", "p1")
			require.NoError(t, getErr)
			assert.Equal(t, "old", stored.Metadata.Description, "refused update changed the stored policy")
			assert.Len(t, stored.Spec.Rules, 1, "refused update changed the stored rules")
		})
	}
}

func TestExecuteIAMPolicyUpdate_SDKFormatPatchesMetadataOnly(t *testing.T) {
	reg, storage, ctx := newStoredPolicyService(t)
	_, err := reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"name":      "p1",
		"policy":    map[string]interface{}{"description": "new", "priority": float64(9), "labels": map[string]interface{}{"team": "a"}},
	})
	require.NoError(t, err)
	stored, err := storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "new", stored.Metadata.Description)
	assert.Equal(t, 9, stored.Metadata.Priority)
	assert.Equal(t, map[string]string{"team": "a"}, stored.Metadata.Labels)
	require.Len(t, stored.Spec.Rules, 1, "a metadata update blanked the rules")
	assert.Equal(t, []string{"read"}, stored.Spec.Rules[0].Actions)
}

func TestExecuteIAMPolicyUpdate_SDKFormatReplacesSpec(t *testing.T) {
	reg, storage, ctx := newStoredPolicyService(t)
	_, err := reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"name":      "p1",
		"policy": map[string]interface{}{"spec_yaml": `rules:
  - name: "write-docs"
    actions: ["write"]
    resources: ["documents"]
    effect: "allow"`},
	})
	require.NoError(t, err)
	stored, err := storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", stored.Metadata.Name)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, []string{"write"}, stored.Spec.Rules[0].Actions)
}

func TestExecuteIAMPolicyUpdate_SDKFormatMissingPolicy(t *testing.T) {
	reg, _, ctx := newStoredPolicyService(t)
	_, err := reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"name":      "absent",
		"policy":    map[string]interface{}{"description": "new"},
	})
	assert.Error(t, err)
}
