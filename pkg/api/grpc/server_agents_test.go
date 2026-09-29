// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// catalogAgentService is an agent service holding a real agent directory.
type catalogAgentService struct {
	grpcTestAgentService
	directory *orchestration.AgentDirectory
}

func (m *catalogAgentService) AgentDirectory() *orchestration.AgentDirectory { return m.directory }

// anyModels serves every backend with no curated model list.
type anyModels struct{}

func (anyModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

func newAgentCatalogServer(t *testing.T) *AgentCatalogServer {
	t.Helper()
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: anyModels{},
	})
	require.NoError(t, err)
	return NewAgentCatalogServer(&catalogAgentService{directory: dir}, createOrchestratorGRPCTestLogger())
}

func TestAgentCatalogOverGRPC_Lifecycle(t *testing.T) {
	srv := newAgentCatalogServer(t)
	ctx := context.Background()
	agent := &pb.Agent{
		Id: "codex-reviewer", Name: "Codex reviewer", Roles: []string{"review"}, Backend: "codex", Model: "gpt-5.5",
		ReasoningEffort: "medium", Image: "worker:1", MaxConcurrent: 2, ContextTokens: 32000,
		Provider:  &pb.ProviderEndpoint{Name: "ovms", BaseUrl: "http://gpu:9000/v1", WireApi: "chat"},
		Account:   &pb.AgentAccountPolicy{Strategy: "least_usage"},
		Prompt:    &pb.AgentPrompt{System: "review carefully"},
		Placement: &pb.RolePolicy{Prefer: "review", Reserve: 1},
		McpScopes: []string{"orchestrator:read"}, Capabilities: []string{"tool_use"},
		Spend: &pb.SpendLimits{Windows: []*pb.SpendLimitWindow{{Name: "daily", SizeNanos: int64(24 * time.Hour), MaxTokens: 1_000_000}}},
	}
	created, err := srv.CreateAgent(ctx, &pb.CreateAgentRequest{TenantId: "acme", Agent: agent})
	require.NoError(t, err)
	assert.Equal(t, "gpt-5.5", created.GetAgent().GetModel())
	assert.NotZero(t, created.GetAgent().GetCreatedAtUnix())

	// Every field survives the wire both ways.
	back := AgentFromProto(AgentToProto(AgentFromProto(agent)))
	assert.Equal(t, AgentFromProto(agent), back)

	list, err := srv.ListAgents(ctx, &pb.ListCatalogAgentsRequest{TenantId: "acme"})
	require.NoError(t, err)
	require.Len(t, list.GetAgents(), 1)

	agent.Name = "Reviewer"
	updated, err := srv.UpdateAgent(ctx, &pb.UpdateAgentRequest{TenantId: "acme", AgentId: "codex-reviewer", Agent: agent})
	require.NoError(t, err)
	assert.Equal(t, "Reviewer", updated.GetAgent().GetName())

	got, err := srv.GetAgent(ctx, &pb.GetCatalogAgentRequest{TenantId: "acme", AgentId: "codex-reviewer"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), got.GetAgent().GetMaxConcurrent())

	pool, err := srv.CreateAgentPool(ctx, &pb.CreateAgentPoolRequest{TenantId: "acme",
		Pool: &pb.AgentPool{Id: "reviewers", Name: "Reviewers", Agents: []string{"codex-reviewer"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"codex-reviewer"}, pool.GetPool().GetAgents())

	used, err := srv.AgentUsedBy(ctx, &pb.AgentUsedByRequest{TenantId: "acme", AgentId: "codex-reviewer"})
	require.NoError(t, err)
	require.Len(t, used.GetUsedBy(), 1)

	_, err = srv.DeleteAgent(ctx, &pb.DeleteAgentRequest{TenantId: "acme", AgentId: "codex-reviewer"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "%v", err)

	_, err = srv.ListAgentPools(ctx, &pb.ListAgentPoolsRequest{TenantId: "acme"})
	require.NoError(t, err)
	_, err = srv.GetAgentPool(ctx, &pb.GetAgentPoolRequest{TenantId: "acme", PoolId: "reviewers"})
	require.NoError(t, err)
	_, err = srv.UpdateAgentPool(ctx, &pb.UpdateAgentPoolRequest{TenantId: "acme", PoolId: "reviewers",
		Pool: &pb.AgentPool{Name: "Reviewers", Agents: []string{"codex-reviewer"}}})
	require.NoError(t, err)
	poolUsed, err := srv.AgentPoolUsedBy(ctx, &pb.AgentPoolUsedByRequest{TenantId: "acme", PoolId: "reviewers"})
	require.NoError(t, err)
	assert.Empty(t, poolUsed.GetUsedBy())
	deleted, err := srv.DeleteAgentPool(ctx, &pb.DeleteAgentPoolRequest{TenantId: "acme", PoolId: "reviewers"})
	require.NoError(t, err)
	assert.True(t, deleted.GetDeleted())
	_, err = srv.DeleteAgent(ctx, &pb.DeleteAgentRequest{TenantId: "acme", AgentId: "codex-reviewer"})
	require.NoError(t, err)
}

func TestAgentCatalogOverGRPC_RefusalsMapToCodes(t *testing.T) {
	srv := newAgentCatalogServer(t)
	ctx := context.Background()
	_, err := srv.GetAgent(ctx, &pb.GetCatalogAgentRequest{TenantId: "acme", AgentId: "ghost"})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = srv.CreateAgent(ctx, &pb.CreateAgentRequest{TenantId: "acme", Agent: &pb.Agent{Id: "x", Name: "X", Roles: []string{"coding"}, Backend: "claude"}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "an agent with no model")
	_, err = srv.ListAgents(ctx, &pb.ListCatalogAgentsRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "no tenant on the request or the call")
	// A caller's own tenant comes from the call when the request names none.
	_, err = srv.ListAgents(iam.ContextWithTenant(ctx, "acme"), &pb.ListCatalogAgentsRequest{})
	assert.NoError(t, err)

	bare := NewAgentCatalogServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	_, err = bare.ListAgents(ctx, &pb.ListCatalogAgentsRequest{TenantId: "acme"})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestAgentCatalogStatusError_FallsBackToInternal(t *testing.T) {
	assert.Equal(t, codes.Internal, status.Code(agentCatalogStatusError(errors.New("boom"))))
	assert.Equal(t, codes.ResourceExhausted,
		status.Code(convertOrchestratorError(&orchestration.AgentPoolExhaustedError{Ref: orchestration.PoolRefTo("p")})))
	assert.Equal(t, codes.NotFound, status.Code(convertSDLCError(&orchestration.UnknownAgentError{ID: "a"})))
	assert.Equal(t, codes.NotFound, status.Code(convertProjectError(&orchestration.UnknownAgentError{ID: "a"})))
}

func TestAgentRefConversions(t *testing.T) {
	assert.Nil(t, AgentRefToProto(orchestration.AgentRef{}))
	assert.True(t, AgentRefFromProto(nil).IsZero())
	ref := orchestration.PoolRefTo("leads")
	assert.Equal(t, ref, AgentRefFromProto(AgentRefToProto(ref)))
	assert.Nil(t, AgentToProto(nil))
	assert.Nil(t, AgentFromProto(nil))
	assert.Nil(t, AgentPoolToProto(nil))
	assert.Nil(t, AgentPoolFromProto(nil))
	assert.Nil(t, agentAttemptsToProto(nil))
	attempts := agentAttemptsToProto([]orchestration.AgentAttempt{{AgentID: "a", Reason: "rate_limited"}})
	require.Len(t, attempts, 1)
	assert.Equal(t, "rate_limited", attempts[0].GetReason())
	pool := &orchestration.AgentPool{ID: "p", Name: "P", Agents: []string{"a"}, CreatedAt: time.Unix(10, 0)}
	assert.Equal(t, int64(10), AgentPoolToProto(pool).GetCreatedAtUnix())
}
