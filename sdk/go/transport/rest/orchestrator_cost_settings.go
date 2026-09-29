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
	"context"
	"net/http"
	neturl "net/url"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// costSettingsPath is the cost-settings route, naming the tenant when the
// request does; the server otherwise answers for the caller's own tenant.
func costSettingsPath(tenantID string) string {
	path := "/api/v1/orchestrator/cost-settings"
	if tenantID != "" {
		path += "?" + neturl.Values{"tenant_id": {tenantID}}.Encode()
	}
	return path
}

// handleGetCostSettings handles GET /api/v1/orchestrator/cost-settings.
func (h *orchestratorHandler) handleGetCostSettings(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetCostSettingsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetCostSettings", "invalid request type")
	}
	var resp orchestrator.CostSettings
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, costSettingsPath(sdkReq.TenantID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSetCostSettings handles PUT /api/v1/orchestrator/cost-settings.
func (h *orchestratorHandler) handleSetCostSettings(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SetCostSettingsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SetCostSettings", "invalid request type")
	}
	body := struct {
		ElectricityPricePerKWh *float64           `json:"electricity_price_per_kwh,omitempty"`
		EnginePowerWatts       map[string]float64 `json:"engine_power_watts,omitempty"`
	}{ElectricityPricePerKWh: sdkReq.ElectricityPricePerKWh, EnginePowerWatts: sdkReq.EnginePowerWatts}
	var resp orchestrator.CostSettings
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, costSettingsPath(sdkReq.TenantID), &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
