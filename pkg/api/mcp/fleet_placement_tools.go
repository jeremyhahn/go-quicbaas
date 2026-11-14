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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Fleet placement specs over MCP (SPEC row declarative-placement,
// go-quicbaas#1681). The service enforces the tenant (empty is the caller's)
// and validates the spec; these tools only decode arguments.

func (r *ToolRegistry) registerFleetPlacementTools() {
	named := func(extra map[string]interface{}) map[string]interface{} {
		props := map[string]interface{}{
			"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant; empty is the caller's"},
			"name":      map[string]interface{}{"type": "string", "description": "Placement spec name"},
		}
		for k, v := range extra {
			props[k] = v
		}
		return map[string]interface{}{"type": "object", "properties": props, "required": []string{"name"}}
	}
	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_placement_declare",
		Description: "Create or replace a fleet placement spec: where a role's work may run across the fleet's hosts. generation is the one read, or 0 to create.",
		InputSchema: named(map[string]interface{}{
			"spec": map[string]interface{}{"type": "object", "description": "role, host_count or hosts, profile, required_capabilities, actuate, generation"},
		}),
	})
	r.addTool(ToolDefinition{Name: "quicbaas_fleet_placement_get", Description: "Get a fleet placement spec.", InputSchema: named(nil)})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_placement_list",
		Description: "List a tenant's fleet placement specs.",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{
			"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant; empty is the caller's"},
		}},
	})
	r.addTool(ToolDefinition{Name: "quicbaas_fleet_placement_withdraw", Description: "Withdraw a fleet placement spec.", InputSchema: named(nil)})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_placement_drift",
		Description: "Report a placement spec's role work against the hosts it admits. Read-only: drift is never acted on.",
		InputSchema: named(nil),
	})
}

func isFleetPlacementTool(name string) bool {
	switch name {
	case "quicbaas_fleet_placement_declare", "quicbaas_fleet_placement_get", "quicbaas_fleet_placement_list",
		"quicbaas_fleet_placement_withdraw", "quicbaas_fleet_placement_drift":
		return true
	}
	return false
}

func (r *ToolRegistry) executeFleetPlacementTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	provider, ok := r.service.(service.FleetPlacementProvider)
	if !ok || provider.FleetPlacement() == nil {
		return nil, api.NewServiceUnavailableError("fleet-placement")
	}
	svc := provider.FleetPlacement()
	tenant, spec := getString(args, "tenant_id"), getString(args, "name")
	switch name {
	case "quicbaas_fleet_placement_declare":
		in, err := fleetPlacementSpecArg(args)
		if err != nil {
			return nil, err
		}
		return svc.DeclarePlacementSpec(ctx, tenant, in)
	case "quicbaas_fleet_placement_get":
		return svc.GetPlacementSpec(ctx, tenant, spec)
	case "quicbaas_fleet_placement_list":
		specs, err := svc.ListPlacementSpecs(ctx, tenant)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"specs": specs, "count": len(specs)}, nil
	case "quicbaas_fleet_placement_withdraw":
		if err := svc.WithdrawPlacementSpec(ctx, tenant, spec); err != nil {
			return nil, err
		}
		return map[string]interface{}{"name": spec, "withdrawn": true}, nil
	case "quicbaas_fleet_placement_drift":
		return svc.GetPlacementDrift(ctx, tenant, spec)
	}
	return nil, api.NewUnknownToolError(name)
}

// fleetPlacementSpecArg decodes the spec argument with the REST body's JSON
// names; the name argument, when given, names the spec.
func fleetPlacementSpecArg(args map[string]interface{}) (*orchestration.PlacementSpec, error) {
	spec := &orchestration.PlacementSpec{}
	if raw, ok := args["spec"]; ok && raw != nil {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, api.NewInvalidFormatError("spec", err)
		}
		if err := json.Unmarshal(data, spec); err != nil {
			return nil, api.NewInvalidFormatError("spec", err)
		}
	}
	if name := getString(args, "name"); name != "" {
		spec.Name = name
	}
	return spec, nil
}
