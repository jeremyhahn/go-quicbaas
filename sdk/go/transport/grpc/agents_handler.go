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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsAgents declares the Agents service on this transport, and so on the
// Unix transport that delegates to it.
func (t *Transport) SupportsAgents() bool { return true }

var _ agents.CapableTransport = (*Transport)(nil)

// isAgentsService reports whether the service name is the Agents service.
func isAgentsService(svc string) bool { return svc == agents.ServiceName }

type agentsHandlerFunc func(ctx context.Context, client pb.AgentCatalogServiceClient, req interface{}) (interface{}, error)

// agentsMethodMap dispatches Agents methods.
var agentsMethodMap = map[string]agentsHandlerFunc{
	"ListAgents":  handleAgentsList,
	"GetAgent":    handleAgentsGet,
	"CreateAgent": handleAgentsCreate,
	"UpdateAgent": handleAgentsUpdate,
	"DeleteAgent": handleAgentsDelete,
	"AgentUsedBy": handleAgentsUsedBy,
	"ListPools":   handleAgentPoolsList,
	"GetPool":     handleAgentPoolsGet,
	"CreatePool":  handleAgentPoolsCreate,
	"UpdatePool":  handleAgentPoolsUpdate,
	"DeletePool":  handleAgentPoolsDelete,
	"PoolUsedBy":  handleAgentPoolsUsedBy,
}

// handleAgentsRequest dispatches one Agents verb over conn.
func handleAgentsRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := agentsMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.agents."+method, "unknown agents method")
	}
	return handler(ctx, pb.NewAgentCatalogServiceClient(conn), req)
}

// agentsRequest asserts one verb's request type.
func agentsRequest[T any](req interface{}, method string) (T, error) {
	r, ok := req.(T)
	if !ok {
		var zero T
		return zero, errors.NewInvalidInput("grpc.agents."+method, "invalid request type")
	}
	return r, nil
}

func handleAgentsList(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.ListAgentsRequest](req, "ListAgents")
	if err != nil {
		return nil, err
	}
	resp, err := c.ListAgents(ctx, &pb.ListCatalogAgentsRequest{TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.ListAgents")
	}
	out := &agents.AgentList{Agents: make([]*agents.Agent, 0, len(resp.GetAgents()))}
	for _, agent := range resp.GetAgents() {
		out.Agents = append(out.Agents, agentFromProto(agent))
	}
	return out, nil
}

func handleAgentsGet(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "GetAgent")
	if err != nil {
		return nil, err
	}
	resp, err := c.GetAgent(ctx, &pb.GetCatalogAgentRequest{TenantId: r.TenantID, AgentId: r.AgentID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.GetAgent")
	}
	return agentFromProto(resp.GetAgent()), nil
}

func handleAgentsCreate(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SaveAgentRequest](req, "CreateAgent")
	if err != nil {
		return nil, err
	}
	const op = "grpc.agents.CreateAgent"
	agent, err := agentToProto(op, r.Agent)
	if err != nil {
		return nil, err
	}
	resp, err := c.CreateAgent(ctx, &pb.CreateAgentRequest{TenantId: r.TenantID, Agent: agent})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return agentFromProto(resp.GetAgent()), nil
}

func handleAgentsUpdate(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SaveAgentRequest](req, "UpdateAgent")
	if err != nil {
		return nil, err
	}
	const op = "grpc.agents.UpdateAgent"
	agent, err := agentToProto(op, r.Agent)
	if err != nil {
		return nil, err
	}
	resp, err := c.UpdateAgent(ctx, &pb.UpdateAgentRequest{TenantId: r.TenantID, AgentId: r.AgentID, Agent: agent})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return agentFromProto(resp.GetAgent()), nil
}

func handleAgentsDelete(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "DeleteAgent")
	if err != nil {
		return nil, err
	}
	resp, err := c.DeleteAgent(ctx, &pb.DeleteAgentRequest{TenantId: r.TenantID, AgentId: r.AgentID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.DeleteAgent")
	}
	return &agents.Deleted{Deleted: resp.GetDeleted()}, nil
}

func handleAgentsUsedBy(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "AgentUsedBy")
	if err != nil {
		return nil, err
	}
	resp, err := c.AgentUsedBy(ctx, &pb.AgentUsedByRequest{TenantId: r.TenantID, AgentId: r.AgentID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.AgentUsedBy")
	}
	return &agents.UsageList{UsedBy: agentUsagesFromProto(resp.GetUsedBy())}, nil
}

func handleAgentPoolsList(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.ListPoolsRequest](req, "ListPools")
	if err != nil {
		return nil, err
	}
	resp, err := c.ListAgentPools(ctx, &pb.ListAgentPoolsRequest{TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.ListPools")
	}
	out := &agents.PoolList{Pools: make([]*agents.Pool, 0, len(resp.GetPools()))}
	for _, pool := range resp.GetPools() {
		out.Pools = append(out.Pools, agentPoolFromProto(pool))
	}
	return out, nil
}

func handleAgentPoolsGet(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "GetPool")
	if err != nil {
		return nil, err
	}
	resp, err := c.GetAgentPool(ctx, &pb.GetAgentPoolRequest{TenantId: r.TenantID, PoolId: r.PoolID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.GetPool")
	}
	return agentPoolFromProto(resp.GetPool()), nil
}

func handleAgentPoolsCreate(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SavePoolRequest](req, "CreatePool")
	if err != nil {
		return nil, err
	}
	resp, err := c.CreateAgentPool(ctx, &pb.CreateAgentPoolRequest{TenantId: r.TenantID, Pool: agentPoolToProto(r.Pool)})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.CreatePool")
	}
	return agentPoolFromProto(resp.GetPool()), nil
}

func handleAgentPoolsUpdate(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SavePoolRequest](req, "UpdatePool")
	if err != nil {
		return nil, err
	}
	resp, err := c.UpdateAgentPool(ctx, &pb.UpdateAgentPoolRequest{TenantId: r.TenantID, PoolId: r.PoolID, Pool: agentPoolToProto(r.Pool)})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.UpdatePool")
	}
	return agentPoolFromProto(resp.GetPool()), nil
}

func handleAgentPoolsDelete(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "DeletePool")
	if err != nil {
		return nil, err
	}
	resp, err := c.DeleteAgentPool(ctx, &pb.DeleteAgentPoolRequest{TenantId: r.TenantID, PoolId: r.PoolID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.DeletePool")
	}
	return &agents.Deleted{Deleted: resp.GetDeleted()}, nil
}

func handleAgentPoolsUsedBy(ctx context.Context, c pb.AgentCatalogServiceClient, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "PoolUsedBy")
	if err != nil {
		return nil, err
	}
	resp, err := c.AgentPoolUsedBy(ctx, &pb.AgentPoolUsedByRequest{TenantId: r.TenantID, PoolId: r.PoolID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.agents.PoolUsedBy")
	}
	return &agents.UsageList{UsedBy: agentUsagesFromProto(resp.GetUsedBy())}, nil
}

// =============================================================================
// Conversions
// =============================================================================

// agentRefToProto converts a reference; the zero reference is nil.
func agentRefToProto(ref agents.AgentRef) *pb.AgentRef {
	if ref.IsZero() {
		return nil
	}
	return &pb.AgentRef{AgentId: ref.AgentID, PoolId: ref.PoolID}
}

// agentRefFromProto converts a reference; nil is the zero reference.
func agentRefFromProto(ref *pb.AgentRef) agents.AgentRef {
	return agents.AgentRef{AgentID: ref.GetAgentId(), PoolID: ref.GetPoolId()}
}

func agentToProto(op string, a *agents.Agent) (*pb.Agent, error) {
	if a == nil {
		return nil, nil
	}
	contextTokens, err := int32ToProto(op, "context_tokens", a.ContextTokens)
	if err != nil {
		return nil, err
	}
	maxConcurrent, err := int32ToProto(op, "max_concurrent", a.MaxConcurrent)
	if err != nil {
		return nil, err
	}
	reserve, err := int32ToProto(op, "placement.reserve", a.Placement.Reserve)
	if err != nil {
		return nil, err
	}
	return &pb.Agent{
		Id: a.ID, Name: a.Name, Description: a.Description, Roles: a.Roles,
		Backend: a.Backend, Model: a.Model, Image: a.Image, ReasoningEffort: a.ReasoningEffort,
		Provider: &pb.ProviderEndpoint{
			Name: a.Provider.Name, BaseUrl: a.Provider.BaseURL, WireApi: a.Provider.WireAPI, RequiresAuth: a.Provider.RequiresAuth,
		},
		Account:       &pb.AgentAccountPolicy{Strategy: a.Account.Strategy, Accounts: a.Account.Accounts},
		Prompt:        &pb.AgentPrompt{CharterSet: a.Prompt.CharterSet, Charter: a.Prompt.Charter, System: a.Prompt.System},
		McpScopes:     a.MCPScopes,
		Capabilities:  a.Capabilities,
		ContextTokens: contextTokens,
		MaxConcurrent: maxConcurrent,
		Placement: &pb.RolePolicy{
			Prefer: a.Placement.Prefer, Reserve: reserve,
			Exclusive: a.Placement.Exclusive,
		},
		Spend: spendLimitsToProto(a.Spend),
	}, nil
}

func agentFromProto(a *pb.Agent) *agents.Agent {
	if a == nil {
		return nil
	}
	return &agents.Agent{
		ID: a.GetId(), Name: a.GetName(), Description: a.GetDescription(), Roles: a.GetRoles(),
		Backend: a.GetBackend(), Model: a.GetModel(), Image: a.GetImage(), ReasoningEffort: a.GetReasoningEffort(),
		Provider: agents.ProviderEndpoint{
			Name: a.GetProvider().GetName(), BaseURL: a.GetProvider().GetBaseUrl(),
			WireAPI: a.GetProvider().GetWireApi(), RequiresAuth: a.GetProvider().GetRequiresAuth(),
		},
		Account: agents.AccountPolicy{Strategy: a.GetAccount().GetStrategy(), Accounts: a.GetAccount().GetAccounts()},
		Prompt: agents.Prompt{
			CharterSet: a.GetPrompt().GetCharterSet(), Charter: a.GetPrompt().GetCharter(), System: a.GetPrompt().GetSystem(),
		},
		MCPScopes:     a.GetMcpScopes(),
		Capabilities:  a.GetCapabilities(),
		ContextTokens: int(a.GetContextTokens()),
		MaxConcurrent: int(a.GetMaxConcurrent()),
		Placement: agents.RolePolicy{
			Prefer: a.GetPlacement().GetPrefer(), Reserve: int(a.GetPlacement().GetReserve()), Exclusive: a.GetPlacement().GetExclusive(),
		},
		Spend:     spendLimitsFromProto(a.GetSpend()),
		CreatedAt: timeFromUnix(a.GetCreatedAtUnix()),
		UpdatedAt: timeFromUnix(a.GetUpdatedAtUnix()),
	}
}

func agentPoolToProto(p *agents.Pool) *pb.AgentPool {
	if p == nil {
		return nil
	}
	return &pb.AgentPool{Id: p.ID, Name: p.Name, Description: p.Description, Agents: p.Agents}
}

func agentPoolFromProto(p *pb.AgentPool) *agents.Pool {
	if p == nil {
		return nil
	}
	return &agents.Pool{
		ID: p.GetId(), Name: p.GetName(), Description: p.GetDescription(), Agents: p.GetAgents(),
		CreatedAt: timeFromUnix(p.GetCreatedAtUnix()), UpdatedAt: timeFromUnix(p.GetUpdatedAtUnix()),
	}
}

func agentUsagesFromProto(usages []*pb.AgentUsage) []agents.Usage {
	out := make([]agents.Usage, 0, len(usages))
	for _, usage := range usages {
		out = append(out, agents.Usage{Kind: usage.GetKind(), ID: usage.GetId(), Slot: usage.GetSlot()})
	}
	return out
}

func agentAttemptsFromProto(attempts []*pb.AgentAttempt) []agents.Attempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]agents.Attempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, agents.Attempt{AgentID: attempt.GetAgentId(), Reason: attempt.GetReason(), Detail: attempt.GetDetail()})
	}
	return out
}
