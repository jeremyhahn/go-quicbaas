//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// newEnginePolicyService returns an IAM service double whose policy writes go
// through a real in-memory PolicyEngine, so validation and storage are real.
func newEnginePolicyService() (*mockIAMService, iam.PolicyStorage) {
	storage := iam.NewMemoryStorage()
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: storage})
	return &mockIAMService{
		createPolicyFunc: func(ctx context.Context, req *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
			// Mirror the service: the request's name is checked against
			// the document's own (iam.CheckPolicyName).
			if err := iam.CheckPolicyName(req.Policy, req.Name); err != nil {
				return nil, err
			}
			if err := engine.CreatePolicy(ctx, req.TenantID, req.Policy); err != nil {
				return nil, err
			}
			return &service.CreatePolicyResponse{Policy: req.Policy}, nil
		},
		updatePolicyFunc: func(ctx context.Context, req *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
			if err := engine.UpdatePolicy(ctx, req.TenantID, req.Policy); err != nil {
				return nil, err
			}
			return &service.UpdatePolicyResponse{Policy: req.Policy}, nil
		},
	}, storage
}

// nestedConditionPolicyArg builds the MCP "policy" argument for a policy whose
// single rule carries the given condition.
func nestedConditionPolicyArg(t *testing.T, when iam.Condition) map[string]interface{} {
	t.Helper()
	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "p1", Tenant: "t1", Priority: 100},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Authenticated: true},
			Rules: []iam.Rule{{
				Name:      "allow-read",
				Effect:    iam.EffectAllow,
				Priority:  100,
				Actions:   []string{"read"},
				Resources: []string{"/test/*"},
				When:      []iam.Condition{when},
			}},
		},
	}
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	var arg map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &arg))
	return arg
}

func requireNestedConditionToolError(t *testing.T, err error) {
	t.Helper()
	var nested *iam.NestedConditionError
	require.True(t, stderrors.As(err, &nested), "expected NestedConditionError, got %v", err)
	assert.Equal(t, mcptransport.MCPInvalidInput, toolErrorCode(err))
}

func TestExecuteIAMPolicyCreate_NestedCondition(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	iamSvc, storage := newEnginePolicyService()
	reg := newIAMTestRegistry(iamSvc)

	_, err := reg.executeIAMPolicyCreate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    nestedConditionPolicyArg(t, iam.Condition{Or: []iam.Condition{{Expression: "x == 1"}}}),
	})
	requireNestedConditionToolError(t, err)
	_, getErr := storage.GetPolicy(ctx, "t1", "p1")
	assert.Error(t, getErr, "refused policy was stored")

	_, err = reg.executeIAMPolicyCreate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    nestedConditionPolicyArg(t, iam.Condition{Expression: "x == 1"}),
	})
	require.NoError(t, err)
	stored, err := storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "x == 1", stored.Spec.Rules[0].When[0].Expression)
}

func TestExecuteIAMPolicyUpdate_NestedCondition(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	iamSvc, storage := newEnginePolicyService()
	reg := newIAMTestRegistry(iamSvc)

	_, err := reg.executeIAMPolicyCreate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    nestedConditionPolicyArg(t, iam.Condition{Expression: "x == 1"}),
	})
	require.NoError(t, err)

	_, err = reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"name":      "p1",
		"policy":    nestedConditionPolicyArg(t, iam.Condition{Not: &iam.Condition{Expression: "x == 2"}}),
	})
	requireNestedConditionToolError(t, err)
	stored, err := storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "x == 1", stored.Spec.Rules[0].When[0].Expression)
	assert.Nil(t, stored.Spec.Rules[0].When[0].Not, "refused update changed the stored policy")

	_, err = reg.executeIAMPolicyUpdate(ctx, map[string]interface{}{
		"tenant_id": "t1",
		"name":      "p1",
		"policy":    nestedConditionPolicyArg(t, iam.Condition{Expression: "x == 2"}),
	})
	require.NoError(t, err)
	stored, err = storage.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "x == 2", stored.Spec.Rules[0].When[0].Expression)
}
