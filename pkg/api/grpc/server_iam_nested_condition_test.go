package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

const (
	nestedConditionSpecYAML = `rules:
  - name: r1
    effect: allow
    actions: [read]
    resources: ["*"]
    when:
      - and:
          - expression: "subject.dept == 'eng'"
`
	flatConditionSpecYAML = `rules:
  - name: r1
    effect: allow
    actions: [read]
    resources: ["*"]
    when:
      - expression: "subject.dept == 'eng'"
`
)

func newNestedConditionIAMServer(t *testing.T) (*IAMServer, *IAMServiceAdapter, *iam.MemoryStorage) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	storage := iam.NewMemoryStorage()
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: storage})
	adapter := NewIAMServiceAdapter(nil, nil, engine, nil, nil, nil)
	return NewIAMServer(adapter, log), adapter, storage
}

func storedGRPCPolicy(storage *iam.MemoryStorage) *iam.Policy {
	p, err := storage.GetPolicy(context.Background(), "t1", "p1")
	if err != nil {
		return nil
	}
	return p
}

func TestIAMServer_CreatePolicy_NestedConditionRefused(t *testing.T) {
	srv, _, storage := newNestedConditionIAMServer(t)

	_, err := srv.CreatePolicy(callerContext("t1"), &iampb.CreatePolicyRequest{
		TenantId: "t1", Name: "p1", SpecYaml: []byte(nestedConditionSpecYAML),
	})

	assert.Equal(t, codes.InvalidArgument, status.Code(err), "err: %v", err)
	assert.Nil(t, storedGRPCPolicy(storage), "refused policy was stored")
}

func TestIAMServer_CreatePolicy_FlatConditionStored(t *testing.T) {
	srv, _, storage := newNestedConditionIAMServer(t)

	_, err := srv.CreatePolicy(callerContext("t1"), &iampb.CreatePolicyRequest{
		TenantId: "t1", Name: "p1", SpecYaml: []byte(flatConditionSpecYAML),
	})

	require.NoError(t, err)
	stored := storedGRPCPolicy(storage)
	require.NotNil(t, stored)
	assert.Equal(t, "subject.dept == 'eng'", stored.Spec.Rules[0].When[0].Expression)
}

func TestIAMServiceAdapter_UpdatePolicy_NestedCondition(t *testing.T) {
	srv, adapter, storage := newNestedConditionIAMServer(t)
	ctx := callerContext("t1")
	_, err := srv.CreatePolicy(ctx, &iampb.CreatePolicyRequest{
		TenantId: "t1", Name: "p1", SpecYaml: []byte(flatConditionSpecYAML),
	})
	require.NoError(t, err)

	update := func(when iam.Condition) *iam.Policy {
		return &iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata:   iam.PolicyMetadata{Name: "p1", Tenant: "t1"},
			Spec: iam.PolicySpec{Rules: []iam.Rule{{
				Name: "r1", Effect: iam.EffectAllow, Actions: []string{"read"}, Resources: []string{"*"},
				When: []iam.Condition{when},
			}}},
		}
	}
	_, err = adapter.UpdatePolicy(ctx, update(iam.Condition{Not: &iam.Condition{Expression: "x == 1"}}))
	assert.Equal(t, codes.InvalidArgument, status.Code(convertIAMError(err)), "err: %v", err)
	stored := storedGRPCPolicy(storage)
	require.NotNil(t, stored)
	assert.Nil(t, stored.Spec.Rules[0].When[0].Not, "refused update changed the stored policy")

	_, err = adapter.UpdatePolicy(ctx, update(iam.Condition{Expression: "subject.dept == 'ops'"}))
	require.NoError(t, err)
	assert.Equal(t, "subject.dept == 'ops'", storedGRPCPolicy(storage).Spec.Rules[0].When[0].Expression)
}
