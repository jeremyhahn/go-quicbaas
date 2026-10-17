//go:build mcp

package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// policyEngineComponents serves one in-memory policy engine; the IAM
// policy verbs read nothing else from IAMComponents.
type policyEngineComponents struct {
	service.IAMComponents
	engine *iam.PolicyEngine
}

func (c *policyEngineComponents) GetPolicyEngine() *iam.PolicyEngine { return c.engine }

// unusedRaftNode satisfies the builder's raft node requirement; the IAM
// policy verbs never reach it.
type unusedRaftNode struct {
	interfaces.RaftNode
}

// TestExecuteIAMPolicyCreate_DuplicateIsAlreadyExists pins, through the
// real service IAM wrapper, that creating a policy whose name is taken
// answers the MCP already-exists tool error.
func TestExecuteIAMPolicyCreate_DuplicateIsAlreadyExists(t *testing.T) {
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&unusedRaftNode{}).
		Build()
	require.NoError(t, err)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewMemoryStorage()})
	svc.SetIAMProvider(&policyEngineComponents{engine: engine})

	accessor, ok := svc.(iamServiceAccessor)
	require.True(t, ok, "the built service must expose IAM()")
	reg := newIAMTestRegistry(accessor.IAM())
	ctx := tenantctx.WithTenant(t.Context(), "t1")
	args := map[string]interface{}{
		"tenant_id": "t1",
		"policy":    map[string]interface{}{"name": "p1", "spec_yaml": sdkReadDocsSpec},
	}

	_, err = reg.executeIAMPolicyCreate(ctx, args)
	require.NoError(t, err, "a fresh create must succeed")

	_, err = reg.executeIAMPolicyCreate(ctx, args)
	require.Error(t, err)
	result := ToolErrorResult(nil, "quicbaas_iam_policy_create", err)
	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPAlreadyExists}, result.StructuredContent)
}
