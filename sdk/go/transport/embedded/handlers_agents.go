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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// SupportsAgents declares the Agents service on the embedded transport.
func (t *Transport) SupportsAgents() bool { return true }

var _ agents.CapableTransport = (*Transport)(nil)

// agentsService is the transport service name the Agents verbs route under.
const agentsService = agents.ServiceName

// agentCatalogOf resolves the catalog service of the facade's agent service.
func agentCatalogOf(svc service.QuicBaaS, method string) (*service.AgentCatalogService, error) {
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError(agentsService, method)
	}
	return service.AgentCatalogServiceFrom(agentSvc)
}

// agentsRequest asserts the request type of one verb.
func agentsRequest[T any](req interface{}, method string) (T, error) {
	r, ok := req.(T)
	if !ok {
		var zero T
		return zero, NewInvalidRequestTypeError(agentsService, method, typeNameOf(zero), typeNameOf(req))
	}
	return r, nil
}

func handleAgentsList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.ListAgentsRequest](req, "ListAgents")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "ListAgents")
	if err != nil {
		return nil, err
	}
	list, err := catalog.ListAgents(ctx, r.TenantID)
	if err != nil {
		return nil, err
	}
	out := &agents.AgentList{Agents: make([]*agents.Agent, 0, len(list))}
	for _, agent := range list {
		out.Agents = append(out.Agents, agentFromDomain(agent))
	}
	return out, nil
}

func handleAgentsGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "GetAgent")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "GetAgent")
	if err != nil {
		return nil, err
	}
	agent, err := catalog.GetAgent(ctx, r.TenantID, r.AgentID)
	if err != nil {
		return nil, err
	}
	return agentFromDomain(agent), nil
}

func handleAgentsCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SaveAgentRequest](req, "CreateAgent")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "CreateAgent")
	if err != nil {
		return nil, err
	}
	agent, err := catalog.CreateAgent(ctx, r.TenantID, agentToDomain(r.Agent))
	if err != nil {
		return nil, err
	}
	return agentFromDomain(agent), nil
}

func handleAgentsUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SaveAgentRequest](req, "UpdateAgent")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "UpdateAgent")
	if err != nil {
		return nil, err
	}
	agent, err := catalog.UpdateAgent(ctx, r.TenantID, r.AgentID, agentToDomain(r.Agent))
	if err != nil {
		return nil, err
	}
	return agentFromDomain(agent), nil
}

func handleAgentsDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "DeleteAgent")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "DeleteAgent")
	if err != nil {
		return nil, err
	}
	if err := catalog.DeleteAgent(ctx, r.TenantID, r.AgentID); err != nil {
		return nil, err
	}
	return &agents.Deleted{Deleted: true}, nil
}

func handleAgentsUsedBy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.AgentRequest](req, "AgentUsedBy")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "AgentUsedBy")
	if err != nil {
		return nil, err
	}
	usages, err := catalog.AgentUsedBy(ctx, r.TenantID, r.AgentID)
	if err != nil {
		return nil, err
	}
	return &agents.UsageList{UsedBy: agentUsagesFromDomain(usages)}, nil
}

func handleAgentPoolsList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.ListPoolsRequest](req, "ListPools")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "ListPools")
	if err != nil {
		return nil, err
	}
	pools, err := catalog.ListPools(ctx, r.TenantID)
	if err != nil {
		return nil, err
	}
	out := &agents.PoolList{Pools: make([]*agents.Pool, 0, len(pools))}
	for _, pool := range pools {
		out.Pools = append(out.Pools, agentPoolFromDomain(pool))
	}
	return out, nil
}

func handleAgentPoolsGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "GetPool")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "GetPool")
	if err != nil {
		return nil, err
	}
	pool, err := catalog.GetPool(ctx, r.TenantID, r.PoolID)
	if err != nil {
		return nil, err
	}
	return agentPoolFromDomain(pool), nil
}

func handleAgentPoolsCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SavePoolRequest](req, "CreatePool")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "CreatePool")
	if err != nil {
		return nil, err
	}
	pool, err := catalog.CreatePool(ctx, r.TenantID, agentPoolToDomain(r.Pool))
	if err != nil {
		return nil, err
	}
	return agentPoolFromDomain(pool), nil
}

func handleAgentPoolsUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.SavePoolRequest](req, "UpdatePool")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "UpdatePool")
	if err != nil {
		return nil, err
	}
	pool, err := catalog.UpdatePool(ctx, r.TenantID, r.PoolID, agentPoolToDomain(r.Pool))
	if err != nil {
		return nil, err
	}
	return agentPoolFromDomain(pool), nil
}

func handleAgentPoolsDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "DeletePool")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "DeletePool")
	if err != nil {
		return nil, err
	}
	if err := catalog.DeletePool(ctx, r.TenantID, r.PoolID); err != nil {
		return nil, err
	}
	return &agents.Deleted{Deleted: true}, nil
}

func handleAgentPoolsUsedBy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := agentsRequest[*agents.PoolRequest](req, "PoolUsedBy")
	if err != nil {
		return nil, err
	}
	catalog, err := agentCatalogOf(svc, "PoolUsedBy")
	if err != nil {
		return nil, err
	}
	usages, err := catalog.PoolUsedBy(ctx, r.TenantID, r.PoolID)
	if err != nil {
		return nil, err
	}
	return &agents.UsageList{UsedBy: agentUsagesFromDomain(usages)}, nil
}

// =============================================================================
// Conversions
// =============================================================================

func agentRefToDomain(ref agents.AgentRef) orchestration.AgentRef {
	return orchestration.AgentRef{AgentID: ref.AgentID, PoolID: ref.PoolID}
}

func agentRefFromDomain(ref orchestration.AgentRef) agents.AgentRef {
	return agents.AgentRef{AgentID: ref.AgentID, PoolID: ref.PoolID}
}

func agentToDomain(a *agents.Agent) *orchestration.Agent {
	if a == nil {
		return nil
	}
	roles := make([]orchestration.AgentRole, 0, len(a.Roles))
	for _, role := range a.Roles {
		roles = append(roles, orchestration.AgentRole(role))
	}
	return &orchestration.Agent{
		ID: a.ID, Name: a.Name, Description: a.Description, Roles: roles,
		Backend: a.Backend, Model: a.Model, Image: a.Image, ReasoningEffort: a.ReasoningEffort,
		Provider: orchestration.ProviderEndpoint{
			Name: a.Provider.Name, BaseURL: a.Provider.BaseURL, WireAPI: a.Provider.WireAPI, RequiresAuth: a.Provider.RequiresAuth,
		},
		Account:       orchestration.AgentAccountPolicy{Strategy: a.Account.Strategy, Accounts: a.Account.Accounts},
		Prompt:        orchestration.AgentPrompt{CharterSet: a.Prompt.CharterSet, Charter: a.Prompt.Charter, System: a.Prompt.System},
		MCPScopes:     a.MCPScopes,
		Capabilities:  a.Capabilities,
		ContextTokens: a.ContextTokens,
		MaxConcurrent: a.MaxConcurrent,
		Placement: orchestration.RolePolicy{
			Prefer: orchestration.AgentRole(a.Placement.Prefer), Reserve: a.Placement.Reserve, Exclusive: a.Placement.Exclusive,
		},
		Spend: spendLimitsToOrchestration(a.Spend),
	}
}

func agentFromDomain(a *orchestration.Agent) *agents.Agent {
	if a == nil {
		return nil
	}
	roles := make([]string, 0, len(a.Roles))
	for _, role := range a.Roles {
		roles = append(roles, string(role))
	}
	return &agents.Agent{
		ID: a.ID, Name: a.Name, Description: a.Description, Roles: roles,
		Backend: a.Backend, Model: a.Model, Image: a.Image, ReasoningEffort: a.ReasoningEffort,
		Provider: agents.ProviderEndpoint{
			Name: a.Provider.Name, BaseURL: a.Provider.BaseURL, WireAPI: a.Provider.WireAPI, RequiresAuth: a.Provider.RequiresAuth,
		},
		Account:       agents.AccountPolicy{Strategy: a.Account.Strategy, Accounts: a.Account.Accounts},
		Prompt:        agents.Prompt{CharterSet: a.Prompt.CharterSet, Charter: a.Prompt.Charter, System: a.Prompt.System},
		MCPScopes:     a.MCPScopes,
		Capabilities:  a.Capabilities,
		ContextTokens: a.ContextTokens,
		MaxConcurrent: a.MaxConcurrent,
		Placement: agents.RolePolicy{
			Prefer: string(a.Placement.Prefer), Reserve: a.Placement.Reserve, Exclusive: a.Placement.Exclusive,
		},
		Spend:     spendLimitsFromOrchestration(a.Spend),
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

func agentPoolToDomain(p *agents.Pool) *orchestration.AgentPool {
	if p == nil {
		return nil
	}
	return &orchestration.AgentPool{ID: p.ID, Name: p.Name, Description: p.Description, Agents: p.Agents}
}

func agentPoolFromDomain(p *orchestration.AgentPool) *agents.Pool {
	if p == nil {
		return nil
	}
	return &agents.Pool{
		ID: p.ID, Name: p.Name, Description: p.Description, Agents: p.Agents,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func agentUsagesFromDomain(usages []orchestration.AgentUsage) []agents.Usage {
	out := make([]agents.Usage, 0, len(usages))
	for _, usage := range usages {
		out = append(out, agents.Usage{Kind: usage.Kind, ID: usage.ID, Slot: usage.Slot})
	}
	return out
}

func agentAttemptsFromDomain(attempts []orchestration.AgentAttempt) []agents.Attempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]agents.Attempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, agents.Attempt{AgentID: attempt.AgentID, Reason: attempt.Reason, Detail: attempt.Detail})
	}
	return out
}
