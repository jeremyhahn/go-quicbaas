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
	"math"
	"strings"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Cost settings MCP tools (go-quicbaas#1058)
// =============================================================================

// registerCostSettingsTools registers the tools that read and replace a
// tenant's runtime cost settings: the electricity rate and engine draws its
// local inference is costed from.
func (r *ToolRegistry) registerCostSettingsTools() {
	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_cost_settings_get",
		Description: "Read a tenant's runtime cost settings — the electricity rate ($/kWh) and per-engine device draws (watts) " +
			"local inference is costed from — and the rates in force once they are layered over the configuration.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant whose settings to read. Required."},
			},
			"required": []string{"tenant_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_cost_settings_set",
		Description: "Replace a tenant's runtime cost settings. A local turn costs power_watts × seconds / 3.6e6 kWh × " +
			"electricity_price_per_kwh. An absent rate and an engine not named inherit the configuration; an engine named " +
			"with 0 watts is not costed from electricity for the tenant.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant whose settings to replace. Required."},
				"electricity_price_per_kwh": map[string]interface{}{
					"type": "number", "minimum": 0, "description": "US dollars per kilowatt-hour",
				},
				"engine_power_watts": map[string]interface{}{
					"type":                 "object",
					"description":          "Device draw while serving, in watts, keyed by engine name (\"openvino\", \"ollama\")",
					"additionalProperties": map[string]interface{}{"type": "number", "minimum": 0},
				},
			},
			"required": []string{"tenant_id"},
		},
	})
}

// costSettingsToolHandlers maps each cost settings tool to its handler.
var costSettingsToolHandlers = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"quicbaas_orchestrator_cost_settings_get": (*ToolRegistry).executeCostSettingsGet,
	"quicbaas_orchestrator_cost_settings_set": (*ToolRegistry).executeCostSettingsSet,
}

// isCostSettingsTool reports whether name is a cost settings tool.
func isCostSettingsTool(name string) bool {
	_, ok := costSettingsToolHandlers[name]
	return ok
}

// executeCostSettingsTool dispatches a cost settings tool call.
func (r *ToolRegistry) executeCostSettingsTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handler, ok := costSettingsToolHandlers[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return handler(r, ctx, args)
}

// costSettingsSource resolves the tenant and the agent service's cost
// settings.
func (r *ToolRegistry) costSettingsSource(args map[string]interface{}) (string, orchestration.CostSettingsAccessor, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return "", nil, api.NewRequiredFieldError("tenant_id")
	}
	agentSvc, err := r.getAgentService()
	if err != nil {
		return "", nil, err
	}
	accessor, ok := agentSvc.(orchestration.CostSettingsAccessor)
	if !ok {
		return "", nil, api.NewServiceUnavailableError("cost settings")
	}
	return tenantID, accessor, nil
}

func (r *ToolRegistry) executeCostSettingsGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tenantID, accessor, err := r.costSettingsSource(args)
	if err != nil {
		return nil, err
	}
	report, err := service.GetCostSettings(ctx, accessor, tenantID)
	if err != nil {
		return nil, err
	}
	return marshalResult(report)
}

func (r *ToolRegistry) executeCostSettingsSet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tenantID, accessor, err := r.costSettingsSource(args)
	if err != nil {
		return nil, err
	}
	settings, err := costSettingsFromArgs(args)
	if err != nil {
		return nil, err
	}
	report, err := service.SetCostSettings(ctx, accessor, tenantID, settings)
	if err != nil {
		return nil, err
	}
	return marshalResult(report)
}

// CostSettingsArgumentError is a cost settings argument of the wrong shape.
// It is refused rather than read as absent, because an absent rate inherits
// the configuration and would silently replace the tenant's saved one.
type CostSettingsArgumentError struct {
	// Field is the offending argument.
	Field string
}

// Error implements the error interface.
func (e *CostSettingsArgumentError) Error() string {
	return "mcp: " + e.Field + " must be a finite, non-negative number"
}

// costSettingsFromArgs reads the settings arguments. A null or absent rate
// is unset; anything else must be a finite, non-negative number, and so must
// every engine's draw.
func costSettingsFromArgs(args map[string]interface{}) (orchestration.CostSettings, error) {
	var settings orchestration.CostSettings
	if raw, present := args["electricity_price_per_kwh"]; present && raw != nil {
		rate, ok := finiteNumber(raw)
		if !ok {
			return settings, &CostSettingsArgumentError{Field: "electricity_price_per_kwh"}
		}
		settings.ElectricityPricePerKWh = &rate
	}
	if raw, present := args["engine_power_watts"]; present && raw != nil {
		engines, ok := raw.(map[string]interface{})
		if !ok {
			return settings, &CostSettingsArgumentError{Field: "engine_power_watts"}
		}
		settings.EnginePowerWatts = make(map[string]float64, len(engines))
		for engine, value := range engines {
			watts, ok := finiteNumber(value)
			if !ok || strings.TrimSpace(engine) == "" {
				return settings, &CostSettingsArgumentError{Field: "engine_power_watts." + engine}
			}
			settings.EnginePowerWatts[engine] = watts
		}
	}
	return settings, nil
}

// finiteNumber reads a JSON number that is finite and non-negative.
func finiteNumber(v interface{}) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, false
	}
	return f, true
}
