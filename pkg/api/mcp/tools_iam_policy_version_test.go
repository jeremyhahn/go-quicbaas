//go:build mcp

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// policyVersionArgs returns policy tool arguments for policy p1, with
// version set at the top level and inside the policy object as given. A nil
// version leaves the key out.
func policyVersionArgs(topLevel, nested interface{}) map[string]interface{} {
	policy := map[string]interface{}{
		"apiVersion": "iam.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata":   map[string]interface{}{"name": "p1", "tenant": "t1"},
	}
	if nested != nil {
		policy["version"] = nested
	}
	args := map[string]interface{}{"tenant_id": "t1", "name": "p1", "policy": policy}
	if topLevel != nil {
		args["version"] = topLevel
	}
	return args
}

// policyVersionService returns an IAM service double that counts the
// create and update calls that reach it.
func policyVersionService(calls *int) *mockIAMService {
	return &mockIAMService{
		createPolicyFunc: func(_ context.Context, req *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
			*calls++
			return &service.CreatePolicyResponse{Policy: req.Policy}, nil
		},
		updatePolicyFunc: func(_ context.Context, req *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
			*calls++
			return &service.UpdatePolicyResponse{Policy: req.Policy}, nil
		},
	}
}

func TestExecuteIAMPolicy_NonZeroVersionRefused(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	cases := map[string]map[string]interface{}{
		"top level":     policyVersionArgs(float64(3), nil),
		"inside policy": policyVersionArgs(nil, float64(3)),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			reg := newIAMTestRegistry(policyVersionService(&calls))

			for tool, execute := range map[string]func(context.Context, map[string]interface{}) (interface{}, error){
				"create": reg.executeIAMPolicyCreate,
				"update": reg.executeIAMPolicyUpdate,
			} {
				_, err := execute(ctx, args)
				var versionErr *iam.PolicyVersionUnsupportedError
				require.True(t, errors.As(err, &versionErr), "%s: got %v", tool, err)
				assert.Equal(t, "3", versionErr.Version, tool)
			}
			assert.Zero(t, calls, "a refused version must not reach the service")
		})
	}
}

func TestExecuteIAMPolicy_ZeroOrAbsentVersionPasses(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	cases := map[string]map[string]interface{}{
		"absent":             policyVersionArgs(nil, nil),
		"zero at top level":  policyVersionArgs(float64(0), nil),
		"zero inside policy": policyVersionArgs(nil, float64(0)),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			reg := newIAMTestRegistry(policyVersionService(&calls))

			_, err := reg.executeIAMPolicyCreate(ctx, args)
			require.NoError(t, err)
			_, err = reg.executeIAMPolicyUpdate(ctx, args)
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
		})
	}
}
