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
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// costSettingsAccessorOf resolves the in-process cost settings.
func costSettingsAccessorOf(svc service.QuicBaaS, method string) (orchestration.CostSettingsAccessor, error) {
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", method)
	}
	accessor, ok := agentSvc.(orchestration.CostSettingsAccessor)
	if !ok {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", method)
	}
	return accessor, nil
}

// handleOrchestratorGetCostSettings answers GetCostSettings in process.
// service.GetCostSettings scopes the storage namespace.
func handleOrchestratorGetCostSettings(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.GetCostSettingsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "GetCostSettings",
			"*orchestrator.GetCostSettingsRequest", reflect.TypeOf(req).String())
	}
	accessor, err := costSettingsAccessorOf(svc, "GetCostSettings")
	if err != nil {
		return nil, err
	}
	report, err := service.GetCostSettings(ctx, accessor, r.TenantID)
	if err != nil {
		return nil, err
	}
	return embeddedCostSettings(report), nil
}

// handleOrchestratorSetCostSettings answers SetCostSettings in process.
func handleOrchestratorSetCostSettings(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.SetCostSettingsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "SetCostSettings",
			"*orchestrator.SetCostSettingsRequest", reflect.TypeOf(req).String())
	}
	accessor, err := costSettingsAccessorOf(svc, "SetCostSettings")
	if err != nil {
		return nil, err
	}
	report, err := service.SetCostSettings(ctx, accessor, r.TenantID, orchestration.CostSettings{
		ElectricityPricePerKWh: r.ElectricityPricePerKWh,
		EnginePowerWatts:       r.EnginePowerWatts,
	})
	if err != nil {
		return nil, err
	}
	return embeddedCostSettings(report), nil
}

// embeddedCostSettings converts a tenant's settings and the rates in force.
func embeddedCostSettings(report *orchestration.CostSettingsReport) *orchestratorSDK.CostSettings {
	out := &orchestratorSDK.CostSettings{
		Settings: orchestratorSDK.CostSettingValues{
			TenantID:               report.Settings.TenantID,
			ElectricityPricePerKWh: report.Settings.ElectricityPricePerKWh,
			EnginePowerWatts:       report.Settings.EnginePowerWatts,
		},
		Electricity: orchestratorSDK.ElectricityRate{
			PricePerKWh: report.Electricity.PricePerKWh,
			Source:      string(report.Electricity.Source),
		},
		EngineDraws: report.EngineDraws,
	}
	if !report.Settings.UpdatedAt.IsZero() {
		updated := report.Settings.UpdatedAt
		out.Settings.UpdatedAt = &updated
	}
	return out
}
