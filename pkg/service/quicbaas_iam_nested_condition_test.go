package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func conditionHappyPolicy(when iam.Condition) *iam.Policy {
	p := happyPolicy("t1", "p1")
	p.Spec.Rules[0].When = []iam.Condition{when}
	return p
}

func TestIAMServiceWrapper_CreatePolicy_NestedCondition(t *testing.T) {
	ctx := context.Background()
	provider, pe := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}

	_, err := w.CreatePolicy(ctx, &CreatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Or: []iam.Condition{{Expression: "x == 1"}}}),
	})
	var nested *iam.NestedConditionError
	require.True(t, stderrors.As(err, &nested), "expected NestedConditionError, got %v", err)
	_, getErr := pe.GetPolicy(ctx, "t1", "p1")
	assert.Error(t, getErr, "refused policy was stored")

	_, err = w.CreatePolicy(ctx, &CreatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Expression: "x == 1"}),
	})
	require.NoError(t, err)
	stored, err := pe.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "x == 1", stored.Spec.Rules[0].When[0].Expression)
}

func TestIAMServiceWrapper_UpdatePolicy_NestedCondition(t *testing.T) {
	ctx := context.Background()
	provider, pe := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	_, err := w.CreatePolicy(ctx, &CreatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Expression: "x == 1"}),
	})
	require.NoError(t, err)

	_, err = w.UpdatePolicy(ctx, &UpdatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Not: &iam.Condition{Expression: "x == 2"}}),
	})
	var nested *iam.NestedConditionError
	require.True(t, stderrors.As(err, &nested), "expected NestedConditionError, got %v", err)
	stored, err := pe.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Nil(t, stored.Spec.Rules[0].When[0].Not, "refused update changed the stored policy")

	_, err = w.UpdatePolicy(ctx, &UpdatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Expression: "x == 2"}),
	})
	require.NoError(t, err)
	stored, err = pe.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	assert.Equal(t, "x == 2", stored.Spec.Rules[0].When[0].Expression)
}
