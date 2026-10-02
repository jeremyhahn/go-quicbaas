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
// GET / PUT /api/v1/orchestrator/cost-settings (go-quicbaas#1058)
// =============================================================================

// SetCostSettingsRESTRequest is the body of PUT
// /api/v1/orchestrator/cost-settings: the tenant's electricity rate and
// engine draws, replacing the stored ones. Absent fields inherit the
// configuration.
type SetCostSettingsRESTRequest struct {
	ElectricityPricePerKWh *float64           `json:"electricity_price_per_kwh,omitempty"`
	EnginePowerWatts       map[string]float64 `json:"engine_power_watts,omitempty"`
}

// HandleGetCostSettings serves GET /api/v1/orchestrator/cost-settings
// [?tenant_id=]: the tenant's runtime cost settings and the rates in force.
// Gated on orchestrator:status, like the spend report.
func (s *Server) HandleGetCostSettings(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	accessor := s.costSettingsAccessor(w, r)
	if accessor == nil {
		return
	}
	report, err := service.GetCostSettings(ctx, accessor, tenantID)
	if err != nil {
		respondCostSettingsError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// HandleSetCostSettings serves PUT /api/v1/orchestrator/cost-settings
// [?tenant_id=]: it replaces the tenant's settings and answers with the rates
// they leave in force. Gated on cost_settings:write.
func (s *Server) HandleSetCostSettings(w http.ResponseWriter, r *http.Request) {
	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	var body SetCostSettingsRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	accessor := s.costSettingsAccessor(w, r)
	if accessor == nil {
		return
	}
	report, err := service.SetCostSettings(ctx, accessor, tenantID, orchestration.CostSettings{
		ElectricityPricePerKWh: body.ElectricityPricePerKWh,
		EnginePowerWatts:       body.EnginePowerWatts,
	})
	if err != nil {
		respondCostSettingsError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// costSettingsAccessor resolves the agent service's cost settings, answering
// 503 when the deployment wires none.
func (s *Server) costSettingsAccessor(w http.ResponseWriter, r *http.Request) orchestration.CostSettingsAccessor {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return nil
	}
	accessor, ok := agentSvc.(orchestration.CostSettingsAccessor)
	if !ok {
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": "cost settings not available"})
		return nil
	}
	return accessor
}

// respondCostSettingsError maps a cost-settings failure to a status.
func respondCostSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, orchestration.ErrCostSettingsInvalid):
		RespondBadRequest(w, r, err.Error())
	case errors.Is(err, orchestration.ErrAccountTenantScope):
		RespondJSON(w, r, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, orchestration.ErrCostSettingsStore):
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		RespondJSON(w, r, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
