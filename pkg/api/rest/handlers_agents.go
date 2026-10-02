//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// The tenant AI agent catalog
//
//   GET    /api/v1/agents                         agents:read
//   POST   /api/v1/agents                         agents:write
//   GET    /api/v1/agents/{agentID}               agents:read
//   PUT    /api/v1/agents/{agentID}               agents:write
//   DELETE /api/v1/agents/{agentID}               agents:write
//   GET    /api/v1/agents/{agentID}/used-by       agents:read
//   GET    /api/v1/agent-pools                    agents:read
//   POST   /api/v1/agent-pools                    agents:write
//   GET    /api/v1/agent-pools/{poolID}           agents:read
//   PUT    /api/v1/agent-pools/{poolID}           agents:write
//   DELETE /api/v1/agent-pools/{poolID}           agents:write
//   GET    /api/v1/agent-pools/{poolID}/used-by   agents:read
//
// Every route takes ?tenant_id=; without it the caller's own tenant.
// =============================================================================

// agentListResponse is the body of GET /api/v1/agents.
type agentListResponse struct {
	Agents []*orchestration.Agent `json:"agents"`
}

// agentPoolListResponse is the body of GET /api/v1/agent-pools.
type agentPoolListResponse struct {
	Pools []*orchestration.AgentPool `json:"pools"`
}

// agentUsedByResponse is the body of a used-by query.
type agentUsedByResponse struct {
	UsedBy []orchestration.AgentUsage `json:"used_by"`
}

// agentDeletedResponse is the body of a delete.
type agentDeletedResponse struct {
	Deleted bool `json:"deleted"`
}

// agentCatalog resolves the catalog service, answering 503 when the
// deployment wires none.
func (s *Server) agentCatalog(w http.ResponseWriter, r *http.Request) *service.AgentCatalogService {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return nil
	}
	catalog, err := service.AgentCatalogServiceFrom(agentSvc)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return nil
	}
	return catalog
}

// HandleListAgents serves GET /api/v1/agents.
func (s *Server) HandleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	agents, err := catalog.ListAgents(ctx, tenantID)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentListResponse{Agents: agents})
}

// HandleCreateAgent serves POST /api/v1/agents.
func (s *Server) HandleCreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	var agent orchestration.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	created, err := catalog.CreateAgent(ctx, tenantID, &agent)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondCreated(w, r, created)
}

// HandleGetAgent serves GET /api/v1/agents/{agentID}.
func (s *Server) HandleGetAgent(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	agent, err := catalog.GetAgent(ctx, tenantID, chi.URLParam(r, "agentID"))
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agent)
}

// HandleUpdateAgent serves PUT /api/v1/agents/{agentID}.
func (s *Server) HandleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	var agent orchestration.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	updated, err := catalog.UpdateAgent(ctx, tenantID, chi.URLParam(r, "agentID"), &agent)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, updated)
}

// HandleDeleteAgent serves DELETE /api/v1/agents/{agentID}. An Agent anything
// still references is refused with 409, naming every user.
func (s *Server) HandleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	if err := catalog.DeleteAgent(ctx, tenantID, chi.URLParam(r, "agentID")); err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentDeletedResponse{Deleted: true})
}

// HandleAgentUsedBy serves GET /api/v1/agents/{agentID}/used-by.
func (s *Server) HandleAgentUsedBy(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	usages, err := catalog.AgentUsedBy(ctx, tenantID, chi.URLParam(r, "agentID"))
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentUsedByResponse{UsedBy: nonNilUsages(usages)})
}

// HandleListAgentPools serves GET /api/v1/agent-pools.
func (s *Server) HandleListAgentPools(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	pools, err := catalog.ListPools(ctx, tenantID)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentPoolListResponse{Pools: pools})
}

// HandleCreateAgentPool serves POST /api/v1/agent-pools.
func (s *Server) HandleCreateAgentPool(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	var pool orchestration.AgentPool
	if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	created, err := catalog.CreatePool(ctx, tenantID, &pool)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondCreated(w, r, created)
}

// HandleGetAgentPool serves GET /api/v1/agent-pools/{poolID}.
func (s *Server) HandleGetAgentPool(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	pool, err := catalog.GetPool(ctx, tenantID, chi.URLParam(r, "poolID"))
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, pool)
}

// HandleUpdateAgentPool serves PUT /api/v1/agent-pools/{poolID}.
func (s *Server) HandleUpdateAgentPool(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	var pool orchestration.AgentPool
	if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	updated, err := catalog.UpdatePool(ctx, tenantID, chi.URLParam(r, "poolID"), &pool)
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, updated)
}

// HandleDeleteAgentPool serves DELETE /api/v1/agent-pools/{poolID}.
func (s *Server) HandleDeleteAgentPool(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	if err := catalog.DeletePool(ctx, tenantID, chi.URLParam(r, "poolID")); err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentDeletedResponse{Deleted: true})
}

// HandleAgentPoolUsedBy serves GET /api/v1/agent-pools/{poolID}/used-by.
func (s *Server) HandleAgentPoolUsedBy(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	catalog := s.agentCatalog(w, r)
	if catalog == nil {
		return
	}
	usages, err := catalog.PoolUsedBy(ctx, tenantID, chi.URLParam(r, "poolID"))
	if err != nil {
		respondAgentCatalogError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, agentUsedByResponse{UsedBy: nonNilUsages(usages)})
}

// nonNilUsages keeps an empty listing a JSON array rather than null.
func nonNilUsages(usages []orchestration.AgentUsage) []orchestration.AgentUsage {
	if usages == nil {
		return []orchestration.AgentUsage{}
	}
	return usages
}

// agentCatalogResponders answers each kind of catalog refusal.
var agentCatalogResponders = map[orchestration.AgentCatalogFault]func(http.ResponseWriter, *http.Request, string){
	orchestration.AgentCatalogFaultInvalid:     RespondBadRequest,
	orchestration.AgentCatalogFaultNotFound:    RespondNotFound,
	orchestration.AgentCatalogFaultConflict:    RespondConflict,
	orchestration.AgentCatalogFaultInUse:       RespondConflict,
	orchestration.AgentCatalogFaultForbidden:   RespondForbidden,
	orchestration.AgentCatalogFaultUnavailable: RespondServiceUnavailable,
	orchestration.AgentCatalogFaultExhausted:   RespondServiceUnavailable,
}

// respondAgentCatalogError maps a catalog refusal to its status.
func respondAgentCatalogError(w http.ResponseWriter, r *http.Request, err error) {
	if fault, ok := orchestration.ClassifyAgentCatalogVerbError(err); ok {
		agentCatalogResponders[fault](w, r, err.Error())
		return
	}
	RespondInternalError(w, r, err)
}
