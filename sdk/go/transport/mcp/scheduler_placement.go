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
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The placement methods of Async().Scheduler() over MCP. Their SDK types
// carry the same snake_case JSON tags the quicbaas_scheduler_* placement
// tools take and return, so a request is its own JSON and a result decodes
// straight into the SDK type. RegisterNode is the one exception: its tool
// takes the node facts flat (node_id, capacity, labels, ttl_seconds).

// placementResults maps each placement method to the SDK type its tool
// result decodes into.
var placementResults = map[string]func() interface{}{
	"RegisterNode":   func() interface{} { return &scheduler.NodeInfo{} },
	"RenewNode":      func() interface{} { return &scheduler.RenewNodeResponse{} },
	"DeregisterNode": func() interface{} { return &scheduler.DeregisterNodeResponse{} },
	"GetNode":        func() interface{} { return &scheduler.NodeInfo{} },
	"CordonNode":     func() interface{} { return &scheduler.NodeInfo{} },
	"UncordonNode":   func() interface{} { return &scheduler.NodeInfo{} },
	"ListNodes":      func() interface{} { return &scheduler.ListNodesResponse{} },
	"DrainNode":      func() interface{} { return &scheduler.DrainNodeResponse{} },
	"Place":          func() interface{} { return &scheduler.PlaceResult{} },
	"Release":        func() interface{} { return &scheduler.ReleaseResponse{} },
	"GetPlacement":   func() interface{} { return &scheduler.Placement{} },
	"ListPlacements": func() interface{} { return &scheduler.ListPlacementsResponse{} },
}

// transformPlacementRequest renders a placement request as tool arguments.
func transformPlacementRequest(method string, req interface{}) (map[string]interface{}, error) {
	if reg, ok := req.(*scheduler.RegisterNodeRequest); ok {
		if reg == nil {
			return nil, errors.NewInvalidInput("mcp.scheduler.RegisterNode", "request is required")
		}
		return map[string]interface{}{
			"node_id":     reg.Facts.NodeID,
			"capacity":    reg.Facts.Capacity,
			"labels":      reg.Facts.Labels,
			"ttl_seconds": reg.TTLSeconds,
		}, nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.scheduler."+method, "failed to encode request")
	}
	args := map[string]interface{}{}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.scheduler."+method, "request is not a JSON object")
	}
	return args, nil
}

// decodePlacementResult decodes a tool result into the SDK type.
func decodePlacementResult(into interface{}, result interface{}) (interface{}, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.scheduler", "failed to re-encode tool result")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.scheduler", "tool result does not match the SDK type")
	}
	return into, nil
}
