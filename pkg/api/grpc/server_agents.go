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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// AgentCatalogServer — the tenant AI agent catalog over gRPC
// =============================================================================

// AgentCatalogServer implements pb.AgentCatalogServiceServer over the agent
// service's catalog. The Unix transport delegates to it verbatim.
type AgentCatalogServer struct {
	agents orchestration.AgentService
	logger *logger.Logger
}

var _ pb.AgentCatalogServiceServer = (*AgentCatalogServer)(nil)

// NewAgentCatalogServer serves the catalog of agents.
func NewAgentCatalogServer(agents orchestration.AgentService, log *logger.Logger) *AgentCatalogServer {
	return &AgentCatalogServer{agents: agents, logger: log.WithField("component", "agents-grpc-server")}
}

// catalog resolves the catalog service and the tenant a request acts on.
func (s *AgentCatalogServer) catalog(ctx context.Context, requested string) (*service.AgentCatalogService, string, error) {
	svc, err := service.AgentCatalogServiceFrom(s.agents)
	if err != nil {
		return nil, "", agentCatalogStatusError(err)
	}
	// The caller's own tenant; naming another is refused for every caller
	// before the service is reached.
	tenantID, err := tenantctx.Target(ctx, "grpc.AgentCatalog", requested)
	if err != nil {
		return nil, "", toProtoError(err)
	}
	return svc, tenantID, nil
}

// ListAgents implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) ListAgents(ctx context.Context, req *pb.ListCatalogAgentsRequest) (*pb.ListCatalogAgentsResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	agents, err := svc.ListAgents(ctx, tenantID)
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	out := &pb.ListCatalogAgentsResponse{Agents: make([]*pb.Agent, 0, len(agents))}
	for _, agent := range agents {
		out.Agents = append(out.Agents, AgentToProto(agent))
	}
	return out, nil
}

// GetAgent implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) GetAgent(ctx context.Context, req *pb.GetCatalogAgentRequest) (*pb.AgentResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	agent, err := svc.GetAgent(ctx, tenantID, req.GetAgentId())
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentResponse{Agent: AgentToProto(agent)}, nil
}

// CreateAgent implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) CreateAgent(ctx context.Context, req *pb.CreateAgentRequest) (*pb.AgentResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	agent, err := svc.CreateAgent(ctx, tenantID, AgentFromProto(req.GetAgent()))
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentResponse{Agent: AgentToProto(agent)}, nil
}

// UpdateAgent implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) UpdateAgent(ctx context.Context, req *pb.UpdateAgentRequest) (*pb.AgentResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	agent, err := svc.UpdateAgent(ctx, tenantID, req.GetAgentId(), AgentFromProto(req.GetAgent()))
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentResponse{Agent: AgentToProto(agent)}, nil
}

// DeleteAgent implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) DeleteAgent(ctx context.Context, req *pb.DeleteAgentRequest) (*pb.DeleteAgentResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteAgent(ctx, tenantID, req.GetAgentId()); err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.DeleteAgentResponse{Deleted: true}, nil
}

// AgentUsedBy implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) AgentUsedBy(ctx context.Context, req *pb.AgentUsedByRequest) (*pb.AgentUsedByResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	usages, err := svc.AgentUsedBy(ctx, tenantID, req.GetAgentId())
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentUsedByResponse{UsedBy: agentUsagesToProto(usages)}, nil
}

// ListAgentPools implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) ListAgentPools(ctx context.Context, req *pb.ListAgentPoolsRequest) (*pb.ListAgentPoolsResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	pools, err := svc.ListPools(ctx, tenantID)
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	out := &pb.ListAgentPoolsResponse{Pools: make([]*pb.AgentPool, 0, len(pools))}
	for _, pool := range pools {
		out.Pools = append(out.Pools, AgentPoolToProto(pool))
	}
	return out, nil
}

// GetAgentPool implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) GetAgentPool(ctx context.Context, req *pb.GetAgentPoolRequest) (*pb.AgentPoolResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	pool, err := svc.GetPool(ctx, tenantID, req.GetPoolId())
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentPoolResponse{Pool: AgentPoolToProto(pool)}, nil
}

// CreateAgentPool implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) CreateAgentPool(ctx context.Context, req *pb.CreateAgentPoolRequest) (*pb.AgentPoolResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	pool, err := svc.CreatePool(ctx, tenantID, AgentPoolFromProto(req.GetPool()))
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentPoolResponse{Pool: AgentPoolToProto(pool)}, nil
}

// UpdateAgentPool implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) UpdateAgentPool(ctx context.Context, req *pb.UpdateAgentPoolRequest) (*pb.AgentPoolResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	pool, err := svc.UpdatePool(ctx, tenantID, req.GetPoolId(), AgentPoolFromProto(req.GetPool()))
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentPoolResponse{Pool: AgentPoolToProto(pool)}, nil
}

// DeleteAgentPool implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) DeleteAgentPool(ctx context.Context, req *pb.DeleteAgentPoolRequest) (*pb.DeleteAgentResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := svc.DeletePool(ctx, tenantID, req.GetPoolId()); err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.DeleteAgentResponse{Deleted: true}, nil
}

// AgentPoolUsedBy implements pb.AgentCatalogServiceServer.
func (s *AgentCatalogServer) AgentPoolUsedBy(ctx context.Context, req *pb.AgentPoolUsedByRequest) (*pb.AgentUsedByResponse, error) {
	svc, tenantID, err := s.catalog(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	usages, err := svc.PoolUsedBy(ctx, tenantID, req.GetPoolId())
	if err != nil {
		return nil, agentCatalogStatusError(err)
	}
	return &pb.AgentUsedByResponse{UsedBy: agentUsagesToProto(usages)}, nil
}

// agentCatalogCodes maps each kind of catalog refusal to its status code.
var agentCatalogCodes = map[orchestration.AgentCatalogFault]codes.Code{
	orchestration.AgentCatalogFaultInvalid:     codes.InvalidArgument,
	orchestration.AgentCatalogFaultNotFound:    codes.NotFound,
	orchestration.AgentCatalogFaultConflict:    codes.AlreadyExists,
	orchestration.AgentCatalogFaultInUse:       codes.FailedPrecondition,
	orchestration.AgentCatalogFaultForbidden:   codes.PermissionDenied,
	orchestration.AgentCatalogFaultUnavailable: codes.Unavailable,
	orchestration.AgentCatalogFaultExhausted:   codes.ResourceExhausted,
}

// isAgentCatalogError reports whether err is one of the catalog's own
// refusals, so a verb outside this service can answer it the same way.
func isAgentCatalogError(err error) bool {
	_, ok := orchestration.ClassifyAgentCatalogError(err)
	return ok
}

// agentCatalogStatusError maps a catalog refusal to its gRPC status.
func agentCatalogStatusError(err error) error {
	if fault, ok := orchestration.ClassifyAgentCatalogVerbError(err); ok {
		return status.Error(agentCatalogCodes[fault], err.Error())
	}
	return status.Errorf(codes.Internal, "agent catalog: %v", err)
}

// =============================================================================
// Conversions
// =============================================================================

// AgentRefToProto converts a reference; the zero reference is nil.
func AgentRefToProto(ref orchestration.AgentRef) *pb.AgentRef {
	if ref.IsZero() {
		return nil
	}
	return &pb.AgentRef{AgentId: ref.AgentID, PoolId: ref.PoolID}
}

// AgentRefFromProto converts a reference; nil is the zero reference.
func AgentRefFromProto(ref *pb.AgentRef) orchestration.AgentRef {
	return orchestration.AgentRef{AgentID: ref.GetAgentId(), PoolID: ref.GetPoolId()}
}

// AgentToProto converts an Agent.
func AgentToProto(a *orchestration.Agent) *pb.Agent {
	if a == nil {
		return nil
	}
	roles := make([]string, 0, len(a.Roles))
	for _, role := range a.Roles {
		roles = append(roles, string(role))
	}
	return &pb.Agent{
		Id: a.ID, Name: a.Name, Description: a.Description, Roles: roles,
		Backend: a.Backend, Model: a.Model, Image: a.Image, ReasoningEffort: a.ReasoningEffort,
		Provider: &pb.ProviderEndpoint{
			Name: a.Provider.Name, BaseUrl: a.Provider.BaseURL, WireApi: a.Provider.WireAPI, RequiresAuth: a.Provider.RequiresAuth,
		},
		Account:       &pb.AgentAccountPolicy{Strategy: a.Account.Strategy, Accounts: a.Account.Accounts},
		Prompt:        &pb.AgentPrompt{CharterSet: a.Prompt.CharterSet, Charter: a.Prompt.Charter, System: a.Prompt.System},
		McpScopes:     a.MCPScopes,
		Capabilities:  a.Capabilities,
		ContextTokens: int32(a.ContextTokens),
		MaxConcurrent: int32(a.MaxConcurrent),
		Placement: &pb.RolePolicy{
			Prefer: string(a.Placement.Prefer), Reserve: int32(a.Placement.Reserve), Exclusive: a.Placement.Exclusive,
		},
		Spend:         spendLimitsToProto(a.Spend),
		CreatedAtUnix: unixOrZero(a.CreatedAt),
		UpdatedAtUnix: unixOrZero(a.UpdatedAt),
	}
}

// AgentFromProto converts an Agent; timestamps are the store's, not the
// caller's, so they are not read.
func AgentFromProto(a *pb.Agent) *orchestration.Agent {
	if a == nil {
		return nil
	}
	roles := make([]orchestration.AgentRole, 0, len(a.GetRoles()))
	for _, role := range a.GetRoles() {
		roles = append(roles, orchestration.AgentRole(role))
	}
	return &orchestration.Agent{
		ID: a.GetId(), Name: a.GetName(), Description: a.GetDescription(), Roles: roles,
		Backend: a.GetBackend(), Model: a.GetModel(), Image: a.GetImage(), ReasoningEffort: a.GetReasoningEffort(),
		Provider: orchestration.ProviderEndpoint{
			Name: a.GetProvider().GetName(), BaseURL: a.GetProvider().GetBaseUrl(),
			WireAPI: a.GetProvider().GetWireApi(), RequiresAuth: a.GetProvider().GetRequiresAuth(),
		},
		Account: orchestration.AgentAccountPolicy{Strategy: a.GetAccount().GetStrategy(), Accounts: a.GetAccount().GetAccounts()},
		Prompt: orchestration.AgentPrompt{
			CharterSet: a.GetPrompt().GetCharterSet(), Charter: a.GetPrompt().GetCharter(), System: a.GetPrompt().GetSystem(),
		},
		MCPScopes:     a.GetMcpScopes(),
		Capabilities:  a.GetCapabilities(),
		ContextTokens: int(a.GetContextTokens()),
		MaxConcurrent: int(a.GetMaxConcurrent()),
		Placement: orchestration.RolePolicy{
			Prefer: orchestration.AgentRole(a.GetPlacement().GetPrefer()), Reserve: int(a.GetPlacement().GetReserve()),
			Exclusive: a.GetPlacement().GetExclusive(),
		},
		Spend: spendLimitsFromProto(a.GetSpend()),
	}
}

// AgentPoolToProto converts a pool.
func AgentPoolToProto(p *orchestration.AgentPool) *pb.AgentPool {
	if p == nil {
		return nil
	}
	return &pb.AgentPool{
		Id: p.ID, Name: p.Name, Description: p.Description, Agents: p.Agents,
		CreatedAtUnix: unixOrZero(p.CreatedAt), UpdatedAtUnix: unixOrZero(p.UpdatedAt),
	}
}

// AgentPoolFromProto converts a pool.
func AgentPoolFromProto(p *pb.AgentPool) *orchestration.AgentPool {
	if p == nil {
		return nil
	}
	return &orchestration.AgentPool{ID: p.GetId(), Name: p.GetName(), Description: p.GetDescription(), Agents: p.GetAgents()}
}

func agentUsagesToProto(usages []orchestration.AgentUsage) []*pb.AgentUsage {
	out := make([]*pb.AgentUsage, 0, len(usages))
	for _, usage := range usages {
		out = append(out, &pb.AgentUsage{Kind: usage.Kind, Id: usage.ID, Slot: usage.Slot})
	}
	return out
}

// agentAttemptsToProto converts the Agents a pooled spawn passed over.
func agentAttemptsToProto(attempts []orchestration.AgentAttempt) []*pb.AgentAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]*pb.AgentAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, &pb.AgentAttempt{AgentId: attempt.AgentID, Reason: attempt.Reason, Detail: attempt.Detail})
	}
	return out
}
