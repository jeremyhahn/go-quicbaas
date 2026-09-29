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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// fakeAgentCatalog is an AgentCatalogService client that records the last
// request and answers from its fields, or with err.
type fakeAgentCatalog struct {
	pb.AgentCatalogServiceClient
	last  interface{}
	agent *pb.Agent
	pool  *pb.AgentPool
	used  []*pb.AgentUsage
	err   error
}

func (f *fakeAgentCatalog) answer(req interface{}) error { f.last = req; return f.err }

func (f *fakeAgentCatalog) ListAgents(_ context.Context, in *pb.ListCatalogAgentsRequest, _ ...grpc.CallOption) (*pb.ListCatalogAgentsResponse, error) {
	return &pb.ListCatalogAgentsResponse{Agents: []*pb.Agent{f.agent}}, f.answer(in)
}
func (f *fakeAgentCatalog) GetAgent(_ context.Context, in *pb.GetCatalogAgentRequest, _ ...grpc.CallOption) (*pb.AgentResponse, error) {
	return &pb.AgentResponse{Agent: f.agent}, f.answer(in)
}
func (f *fakeAgentCatalog) CreateAgent(_ context.Context, in *pb.CreateAgentRequest, _ ...grpc.CallOption) (*pb.AgentResponse, error) {
	return &pb.AgentResponse{Agent: in.GetAgent()}, f.answer(in)
}
func (f *fakeAgentCatalog) UpdateAgent(_ context.Context, in *pb.UpdateAgentRequest, _ ...grpc.CallOption) (*pb.AgentResponse, error) {
	return &pb.AgentResponse{Agent: in.GetAgent()}, f.answer(in)
}
func (f *fakeAgentCatalog) DeleteAgent(_ context.Context, in *pb.DeleteAgentRequest, _ ...grpc.CallOption) (*pb.DeleteAgentResponse, error) {
	return &pb.DeleteAgentResponse{Deleted: true}, f.answer(in)
}
func (f *fakeAgentCatalog) AgentUsedBy(_ context.Context, in *pb.AgentUsedByRequest, _ ...grpc.CallOption) (*pb.AgentUsedByResponse, error) {
	return &pb.AgentUsedByResponse{UsedBy: f.used}, f.answer(in)
}
func (f *fakeAgentCatalog) ListAgentPools(_ context.Context, in *pb.ListAgentPoolsRequest, _ ...grpc.CallOption) (*pb.ListAgentPoolsResponse, error) {
	return &pb.ListAgentPoolsResponse{Pools: []*pb.AgentPool{f.pool}}, f.answer(in)
}
func (f *fakeAgentCatalog) GetAgentPool(_ context.Context, in *pb.GetAgentPoolRequest, _ ...grpc.CallOption) (*pb.AgentPoolResponse, error) {
	return &pb.AgentPoolResponse{Pool: f.pool}, f.answer(in)
}
func (f *fakeAgentCatalog) CreateAgentPool(_ context.Context, in *pb.CreateAgentPoolRequest, _ ...grpc.CallOption) (*pb.AgentPoolResponse, error) {
	return &pb.AgentPoolResponse{Pool: in.GetPool()}, f.answer(in)
}
func (f *fakeAgentCatalog) UpdateAgentPool(_ context.Context, in *pb.UpdateAgentPoolRequest, _ ...grpc.CallOption) (*pb.AgentPoolResponse, error) {
	return &pb.AgentPoolResponse{Pool: in.GetPool()}, f.answer(in)
}
func (f *fakeAgentCatalog) DeleteAgentPool(_ context.Context, in *pb.DeleteAgentPoolRequest, _ ...grpc.CallOption) (*pb.DeleteAgentResponse, error) {
	return &pb.DeleteAgentResponse{Deleted: true}, f.answer(in)
}
func (f *fakeAgentCatalog) AgentPoolUsedBy(_ context.Context, in *pb.AgentPoolUsedByRequest, _ ...grpc.CallOption) (*pb.AgentUsedByResponse, error) {
	return &pb.AgentUsedByResponse{UsedBy: f.used}, f.answer(in)
}

func fullSDKAgent() *agents.Agent {
	return &agents.Agent{
		ID: "codex-reviewer", Name: "Codex reviewer", Description: "reviews", Roles: []string{"review"},
		Backend: "codex", Model: "gpt-5.5", Image: "worker:1", ReasoningEffort: "high",
		Provider:      agents.ProviderEndpoint{Name: "local", BaseURL: "http://llm.test/v1", WireAPI: "responses", RequiresAuth: true},
		Account:       agents.AccountPolicy{Strategy: "least_usage", Accounts: []string{"codex-1"}},
		Prompt:        agents.Prompt{CharterSet: "default", Charter: "reviewer", System: "Be terse."},
		MCPScopes:     []string{"kv:read"},
		Capabilities:  []string{"gpu"},
		ContextTokens: 200000, MaxConcurrent: 2,
		Placement: agents.RolePolicy{Prefer: "review", Reserve: 1, Exclusive: true},
		Spend:     spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 1_000_000, MaxEquivalentMicros: 5_000_000}}},
	}
}

func TestAgentsGRPC_AnAgentSurvivesTheWire(t *testing.T) {
	want := fullSDKAgent()
	assert.Equal(t, want, agentFromProto(agentToProto(want)))
	assert.Nil(t, agentToProto(nil))
	assert.Nil(t, agentFromProto(nil))
	pool := &agents.Pool{ID: "p", Name: "P", Description: "d", Agents: []string{"a", "b"}}
	assert.Equal(t, pool, agentPoolFromProto(agentPoolToProto(pool)))
	assert.Nil(t, agentPoolToProto(nil))
	assert.Nil(t, agentPoolFromProto(nil))

	assert.Nil(t, agentRefToProto(agents.AgentRef{}), "the zero reference is absent on the wire")
	ref := agents.AgentRef{PoolID: "coders"}
	assert.Equal(t, ref, agentRefFromProto(agentRefToProto(ref)))
	assert.Equal(t, agents.AgentRef{}, agentRefFromProto(nil))

	assert.Nil(t, agentAttemptsFromProto(nil))
	assert.Equal(t, []agents.Attempt{{AgentID: "a", Reason: "rate_limited", Detail: "429"}},
		agentAttemptsFromProto([]*pb.AgentAttempt{{AgentId: "a", Reason: "rate_limited", Detail: "429"}}))
}

func TestAgentsGRPC_EveryVerbCarriesItsRequest(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAgentCatalog{
		agent: agentToProto(fullSDKAgent()),
		pool:  &pb.AgentPool{Id: "reviewers", Agents: []string{"codex-reviewer"}},
		used:  []*pb.AgentUsage{{Kind: "binding", Id: "proj-1", Slot: "worker"}},
	}
	cases := []struct {
		method string
		req    interface{}
		check  func(t *testing.T, resp interface{})
	}{
		{"ListAgents", &agents.ListAgentsRequest{TenantID: "acme"}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "acme", fake.last.(*pb.ListCatalogAgentsRequest).GetTenantId())
			assert.Len(t, resp.(*agents.AgentList).Agents, 1)
		}},
		{"GetAgent", &agents.AgentRequest{AgentID: "codex-reviewer"}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "codex-reviewer", fake.last.(*pb.GetCatalogAgentRequest).GetAgentId())
			assert.Equal(t, "gpt-5.5", resp.(*agents.Agent).Model)
		}},
		{"CreateAgent", &agents.SaveAgentRequest{Agent: fullSDKAgent()}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "high", fake.last.(*pb.CreateAgentRequest).GetAgent().GetReasoningEffort())
			assert.Equal(t, "codex-reviewer", resp.(*agents.Agent).ID)
		}},
		{"UpdateAgent", &agents.SaveAgentRequest{AgentID: "codex-reviewer", Agent: fullSDKAgent()}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "codex-reviewer", fake.last.(*pb.UpdateAgentRequest).GetAgentId())
		}},
		{"DeleteAgent", &agents.AgentRequest{AgentID: "codex-reviewer"}, func(t *testing.T, resp interface{}) {
			assert.True(t, resp.(*agents.Deleted).Deleted)
		}},
		{"AgentUsedBy", &agents.AgentRequest{AgentID: "codex-reviewer"}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, agents.Usage{Kind: "binding", ID: "proj-1", Slot: "worker"}, resp.(*agents.UsageList).UsedBy[0])
		}},
		{"ListPools", &agents.ListPoolsRequest{TenantID: "acme"}, func(t *testing.T, resp interface{}) {
			assert.Len(t, resp.(*agents.PoolList).Pools, 1)
		}},
		{"GetPool", &agents.PoolRequest{PoolID: "reviewers"}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "reviewers", fake.last.(*pb.GetAgentPoolRequest).GetPoolId())
			assert.Equal(t, "reviewers", resp.(*agents.Pool).ID)
		}},
		{"CreatePool", &agents.SavePoolRequest{Pool: &agents.Pool{ID: "p", Agents: []string{"a"}}}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, []string{"a"}, resp.(*agents.Pool).Agents)
		}},
		{"UpdatePool", &agents.SavePoolRequest{PoolID: "p", Pool: &agents.Pool{Agents: []string{"b"}}}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "p", fake.last.(*pb.UpdateAgentPoolRequest).GetPoolId())
		}},
		{"DeletePool", &agents.PoolRequest{PoolID: "p"}, func(t *testing.T, resp interface{}) {
			assert.True(t, resp.(*agents.Deleted).Deleted)
		}},
		{"PoolUsedBy", &agents.PoolRequest{PoolID: "p"}, func(t *testing.T, resp interface{}) {
			assert.Equal(t, "p", fake.last.(*pb.AgentPoolUsedByRequest).GetPoolId())
			assert.Len(t, resp.(*agents.UsageList).UsedBy, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			handler, ok := agentsMethodMap[tc.method]
			require.True(t, ok)
			fake.err = nil
			resp, err := handler(ctx, fake, tc.req)
			require.NoError(t, err)
			tc.check(t, resp)

			_, err = handler(ctx, fake, &struct{}{})
			assert.Error(t, err, "a request of the wrong type is refused")

			fake.err = status.Error(codes.FailedPrecondition, "agent codex-reviewer is in use")
			_, err = handler(ctx, fake, tc.req)
			assert.Error(t, err, "a server refusal surfaces")
		})
	}
}

func TestAgentsGRPC_RefusesAnUnknownVerb(t *testing.T) {
	_, err := handleAgentsRequest(context.Background(), nil, "Rename", nil)
	assert.Error(t, err)
	assert.True(t, (&Transport{}).SupportsAgents())
	assert.True(t, isAgentsService(agents.ServiceName))
	assert.False(t, isAgentsService("quicbaas.Fleet"))
}
