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
	"errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// GET / PUT /api/v1/orchestrator/spend-policy (go-quicbaas#1058, ruling F5)
// =============================================================================

// SetTenantSpendPolicyRESTRequest is the body of PUT
// /api/v1/orchestrator/spend-policy: the tenant's limits, replacing the
// stored ones. No windows makes the tenant unmetered.
type SetTenantSpendPolicyRESTRequest struct {
	Limits orchestration.SpendLimits `json:"limits"`
}

// HandleGetTenantSpendPolicy serves GET /api/v1/orchestrator/spend-policy
// [?tenant_id=]: the tenant's own limits and its usage against each window.
// Gated on orchestrator:status, like the spend report.
func (s *Server) HandleGetTenantSpendPolicy(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID := resolveOrchestratorTenant(r)
	accessor := s.tenantSpendAccessor(w, r)
	if accessor == nil {
		return
	}
	report, err := service.GetTenantSpendPolicy(ctx, accessor, tenantID)
	if err != nil {
		respondTenantSpendError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// HandleSetTenantSpendPolicy serves PUT /api/v1/orchestrator/spend-policy
// [?tenant_id=]: it replaces the tenant's limits and answers with the stored
// policy and its usage. Gated on orchestrator:spend_policy.
func (s *Server) HandleSetTenantSpendPolicy(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID := resolveOrchestratorTenant(r)
	var body SetTenantSpendPolicyRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	accessor := s.tenantSpendAccessor(w, r)
	if accessor == nil {
		return
	}
	report, err := service.SetTenantSpendPolicy(ctx, accessor, tenantID, body.Limits)
	if err != nil {
		respondTenantSpendError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// tenantSpendAccessor resolves the agent service's tenant spend-policy store,
// answering 503 when the deployment wires none.
func (s *Server) tenantSpendAccessor(w http.ResponseWriter, r *http.Request) service.TenantSpendPolicySource {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return nil
	}
	accessor, ok := agentSvc.(service.TenantSpendPolicySource)
	if !ok {
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": "tenant spend policy not available"})
		return nil
	}
	return accessor
}

// respondTenantSpendError maps a tenant spend-policy failure to a status.
func respondTenantSpendError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, orchestration.ErrQuotaInvalid), errors.Is(err, orchestration.ErrTenantSpendPolicyInvalid):
		RespondBadRequest(w, r, err.Error())
	case errors.Is(err, orchestration.ErrAccountTenantScope):
		RespondJSON(w, r, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, orchestration.ErrTenantSpendPolicyStore):
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		RespondJSON(w, r, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
