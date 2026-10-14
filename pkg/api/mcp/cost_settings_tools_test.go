//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// costSettingsAgentService exposes a cost settings store.
type costSettingsAgentService struct {
	*mockAgentService
	store orchestration.CostSettingsStore
}

func (s *costSettingsAgentService) CostSettingsStore() orchestration.CostSettingsStore {
	return s.store
}
func (s *costSettingsAgentService) PriceBook() *orchestration.PriceBook { return nil }

func decodeCostSettings(t *testing.T, raw interface{}) orchestration.CostSettingsReport {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var report orchestration.CostSettingsReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return report
}

// TestCostSettingsTools is go-quicbaas#1058 over MCP: the default rate, then
// a set rate and draw read back in force.
func TestCostSettingsTools(t *testing.T) {
	reg := newFleetRegistry(t, &costSettingsAgentService{
		mockAgentService: &mockAgentService{}, store: orchestration.NewInMemoryCostSettingsStore(),
	})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	raw, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_get", map[string]interface{}{"tenant_id": "t1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if report := decodeCostSettings(t, raw); report.Electricity.PricePerKWh != 0.12 {
		t.Fatalf("rate = %+v, want the $0.12 default", report.Electricity)
	}

	raw, err = reg.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_set", map[string]interface{}{
		"tenant_id": "t1", "electricity_price_per_kwh": 0.2,
		"engine_power_watts": map[string]interface{}{"openvino": float64(12)},
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	report := decodeCostSettings(t, raw)
	if report.Electricity.PricePerKWh != 0.2 || report.EngineDraws["openvino"] != 12 {
		t.Fatalf("report = %+v, want $0.20 and openvino 12 W", report)
	}

	// The system tenant gets no cross-tenant lift (#1150).
	system := tenantctx.WithSystem(context.Background())
	if _, err := reg.ExecuteTool(system, "quicbaas_orchestrator_cost_settings_get", map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Fatal("a system-tenant read of t1 must be refused")
	}
}

// TestCostSettingsToolsRefuseMalformedArguments asserts a rate or draw of the
// wrong shape is refused rather than read as absent, which would replace the
// saved rate with the configuration's.
func TestCostSettingsToolsRefuseMalformedArguments(t *testing.T) {
	store := orchestration.NewInMemoryCostSettingsStore()
	reg := newFleetRegistry(t, &costSettingsAgentService{mockAgentService: &mockAgentService{}, store: store})
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	if _, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_set", map[string]interface{}{
		"tenant_id": "t1", "electricity_price_per_kwh": 0.3,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for name, args := range map[string]map[string]interface{}{
		"a string rate":       {"electricity_price_per_kwh": "0.2"},
		"a negative rate":     {"electricity_price_per_kwh": float64(-1)},
		"draws not an object": {"engine_power_watts": "12"},
		"a string draw":       {"engine_power_watts": map[string]interface{}{"ollama": "250"}},
		"a negative draw":     {"engine_power_watts": map[string]interface{}{"ollama": float64(-1)}},
	} {
		t.Run(name, func(t *testing.T) {
			args["tenant_id"] = "t1"
			_, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_set", args)
			var argErr *CostSettingsArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("err = %v, want a CostSettingsArgumentError", err)
			}
		})
	}
	saved, err := store.Get(orchestration.ContextWithTenant(ctx, "t1"), "t1")
	if err != nil || saved.ElectricityPricePerKWh == nil || *saved.ElectricityPricePerKWh != 0.3 {
		t.Fatalf("saved = %+v, %v; want the $0.30 rate intact", saved, err)
	}
	if _, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_get", map[string]interface{}{}); err == nil {
		t.Fatal("a get with no tenant must be refused")
	}
	bare := newFleetRegistry(t, &mockAgentService{})
	if _, err := bare.ExecuteTool(ctx, "quicbaas_orchestrator_cost_settings_get", map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Fatal("a deployment with no cost settings must refuse")
	}
}
