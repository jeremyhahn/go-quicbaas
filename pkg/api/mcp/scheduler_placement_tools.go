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
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The scheduler's workload placement tools. Their arguments are decoded
// straight into the typed requests, because a placement request carries
// nested maps (resources, labels) and arrays of objects (constraints,
// affinity terms) that per-field scalar getters would flatten or drop.

// placementArgsError reports tool arguments that do not decode.
type placementArgsError struct {
	tool  string
	cause error
}

// Error implements error.
func (e *placementArgsError) Error() string {
	return "mcp: invalid arguments for " + e.tool + ": " + e.cause.Error()
}

// Unwrap returns the decode error.
func (e *placementArgsError) Unwrap() error { return e.cause }

// decodePlacementArgs decodes tool arguments into a typed request.
func decodePlacementArgs(tool string, args map[string]interface{}, into any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return &placementArgsError{tool: tool, cause: err}
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return &placementArgsError{tool: tool, cause: err}
	}
	return nil
}

// placementNodeArgs are the register_node tool's arguments.
type placementNodeArgs struct {
	NodeID     string            `json:"node_id"`
	Capacity   map[string]int64  `json:"capacity"`
	Labels     map[string]string `json:"labels"`
	TTLSeconds int64             `json:"ttl_seconds"`
}

// placementToolExecutor serves one placement tool.
type placementToolExecutor func(ctx context.Context, svc service.PlacementService, tool string, args map[string]interface{}) (interface{}, error)

// placementExecutors dispatches on the tool name in constant time.
var placementExecutors = map[string]placementToolExecutor{
	"quicbaas_scheduler_register_node": func(ctx context.Context, svc service.PlacementService, tool string, args map[string]interface{}) (interface{}, error) {
		var a placementNodeArgs
		if err := decodePlacementArgs(tool, args, &a); err != nil {
			return nil, err
		}
		return svc.RegisterNode(ctx, &service.RegisterPlacementNodeRequest{
			Facts:      placement.NodeFacts{NodeID: a.NodeID, Capacity: a.Capacity, Labels: a.Labels},
			TTLSeconds: a.TTLSeconds,
		})
	},
	"quicbaas_scheduler_renew_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		id := getString(args, "node_id")
		if err := svc.RenewNode(ctx, id); err != nil {
			return nil, err
		}
		return map[string]interface{}{"node_id": id, "renewed": true}, nil
	},
	"quicbaas_scheduler_deregister_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		id := getString(args, "node_id")
		if err := svc.DeregisterNode(ctx, id); err != nil {
			return nil, err
		}
		return map[string]interface{}{"node_id": id, "deregistered": true}, nil
	},
	"quicbaas_scheduler_get_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		return svc.GetNode(ctx, getString(args, "node_id"))
	},
	"quicbaas_scheduler_list_nodes": func(ctx context.Context, svc service.PlacementService, _ string, _ map[string]interface{}) (interface{}, error) {
		nodes, err := svc.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"nodes": nodes}, nil
	},
	"quicbaas_scheduler_cordon_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		return svc.CordonNode(ctx, getString(args, "node_id"))
	},
	"quicbaas_scheduler_uncordon_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		return svc.UncordonNode(ctx, getString(args, "node_id"))
	},
	"quicbaas_scheduler_drain_node": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		id := getString(args, "node_id")
		moved, err := svc.DrainNode(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"node_id": id, "placements": moved}, nil
	},
	"quicbaas_scheduler_place": func(ctx context.Context, svc service.PlacementService, tool string, args map[string]interface{}) (interface{}, error) {
		var req placement.PlacementRequest
		if err := decodePlacementArgs(tool, args, &req); err != nil {
			return nil, err
		}
		return svc.Place(ctx, &req)
	},
	"quicbaas_scheduler_release": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		id := getString(args, "workload_id")
		if err := svc.Release(ctx, id); err != nil {
			return nil, err
		}
		return map[string]interface{}{"workload_id": id, "released": true}, nil
	},
	"quicbaas_scheduler_get_placement": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		return svc.GetPlacement(ctx, getString(args, "workload_id"))
	},
	"quicbaas_scheduler_list_placements": func(ctx context.Context, svc service.PlacementService, _ string, args map[string]interface{}) (interface{}, error) {
		out, err := svc.ListPlacements(ctx, &placement.PlacementFilter{NodeID: getString(args, "node_id"), Group: getString(args, "group")})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"placements": out}, nil
	},
}

// isPlacementTool reports whether name is a placement tool.
func isPlacementTool(name string) bool {
	_, ok := placementExecutors[name]
	return ok
}

// executePlacementTool runs a placement tool.
func (r *ToolRegistry) executePlacementTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.service.Placement()
	if svc == nil {
		return nil, api.NewServiceUnavailableError("scheduler placement")
	}
	executor, ok := placementExecutors[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return executor(ctx, svc, name, args)
}

// nodeIDSchema is the input schema of a tool keyed by a node ID.
func nodeIDSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{"type": "string", "description": description},
		},
		"required": []string{"node_id"},
	}
}

// workloadIDSchema is the input schema of a tool keyed by a workload ID.
func workloadIDSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workload_id": map[string]interface{}{"type": "string", "description": description},
		},
		"required": []string{"workload_id"},
	}
}

// int64MapSchema and stringMapSchema describe resource and label maps.
var (
	int64MapSchema  = map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "integer"}}
	stringMapSchema = map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}
	termSchema      = map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"group":    map[string]interface{}{"type": "string"},
				"required": map[string]interface{}{"type": "boolean"},
				"weight":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100},
			},
			"required": []string{"group"},
		},
	}
)

// registerPlacementTools registers the placement tools.
func (r *ToolRegistry) registerPlacementTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_register_node",
		Description: "Register or refresh a node's caller-supplied facts (abstract resource capacities and opaque labels) under a lease; the node heartbeats with renew_node within ttl_seconds or drops out of placement",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"node_id":     map[string]interface{}{"type": "string", "description": "Node identifier"},
				"capacity":    int64MapSchema,
				"labels":      stringMapSchema,
				"ttl_seconds": map[string]interface{}{"type": "integer", "description": "Lease time-to-live in seconds"},
			},
			"required": []string{"node_id", "ttl_seconds"},
		},
	})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_renew_node", Description: "Heartbeat a node's placement lease", InputSchema: nodeIDSchema("Node to renew")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_deregister_node", Description: "Deregister a node: revoke its lease and clear its cordon/drain status; placements on it are reported orphaned until released", InputSchema: nodeIDSchema("Node to deregister")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_get_node", Description: "Get a live node's facts, status, lease and allocation", InputSchema: nodeIDSchema("Node to read")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_list_nodes", Description: "List every live node", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_cordon_node", Description: "Cordon a node: exclude it from new placements", InputSchema: nodeIDSchema("Node to cordon")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_uncordon_node", Description: "Return a node to service, clearing cordon and drain", InputSchema: nodeIDSchema("Node to uncordon")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_drain_node", Description: "Drain a node: cordon it, mark it draining and return the placements the caller must move", InputSchema: nodeIDSchema("Node to drain")})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_place",
		Description: "Place a workload: filter live nodes by label constraints, free capacity, affinity, anti-affinity and spread, score by bin-packing or spreading, and record the decision. Idempotent per workload_id",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workload_id": map[string]interface{}{"type": "string", "description": "Workload identifier"},
				"group":       map[string]interface{}{"type": "string", "description": "Workload group for affinity and spread"},
				"labels":      stringMapSchema,
				"resources":   int64MapSchema,
				"constraints": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"key":      map[string]interface{}{"type": "string"},
							"operator": map[string]interface{}{"type": "string", "enum": []string{"In", "NotIn", "Exists", "DoesNotExist"}},
							"values":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						},
						"required": []string{"key", "operator"},
					},
				},
				"affinity":      termSchema,
				"anti_affinity": termSchema,
				"spread": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"topology_key": map[string]interface{}{"type": "string"},
						"max_skew":     map[string]interface{}{"type": "integer", "minimum": 1},
						"required":     map[string]interface{}{"type": "boolean"},
					},
					"required": []string{"topology_key", "max_skew"},
				},
				"strategy": map[string]interface{}{"type": "string", "enum": []string{"binpack", "spread"}},
			},
			"required": []string{"workload_id"},
		},
	})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_release", Description: "Release a workload's placement, returning its resources to the node", InputSchema: workloadIDSchema("Workload to release")})
	r.addTool(ToolDefinition{Name: "quicbaas_scheduler_get_placement", Description: "Get a workload's placement; orphaned is true when its node's lease has expired", InputSchema: workloadIDSchema("Workload to read")})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_list_placements",
		Description: "List placements, optionally filtered by node or group",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"node_id": map[string]interface{}{"type": "string"},
				"group":   map[string]interface{}{"type": "string"},
			},
		},
	})
}
