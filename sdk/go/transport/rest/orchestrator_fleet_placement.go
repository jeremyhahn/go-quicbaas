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

// fleetPlacementPath is /api/v1/fleet/placements[/{name}][suffix] with the
// tenant as a query parameter when one is named.
func fleetPlacementPath(tenantID, name, suffix string) string {
	path := "/api/v1/fleet/placements"
	if name != "" {
		path += "/" + neturl.PathEscape(name) + suffix
	}
	if tenantID != "" {
		path += "?tenant_id=" + neturl.QueryEscape(tenantID)
	}
	return path
}

func (h *orchestratorHandler) handleDeclareFleetPlacementSpec(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DeclareFleetPlacementSpecRequest)
	if !ok || sdkReq.Spec == nil {
		return nil, errors.NewInvalidInput("orchestrator.DeclareFleetPlacementSpec", "invalid request type")
	}
	var resp orchestrator.FleetPlacementSpec
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, fleetPlacementPath(sdkReq.TenantID, sdkReq.Spec.Name, ""), sdkReq.Spec, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleGetFleetPlacementSpec(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.FleetPlacementSpecRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetFleetPlacementSpec", "invalid request type")
	}
	var resp orchestrator.FleetPlacementSpec
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, fleetPlacementPath(sdkReq.TenantID, sdkReq.Name, ""), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleListFleetPlacementSpecs(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListFleetPlacementSpecsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListFleetPlacementSpecs", "invalid request type")
	}
	var resp orchestrator.ListFleetPlacementSpecsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, fleetPlacementPath(sdkReq.TenantID, "", ""), nil, &resp); err != nil {
		return nil, err
	}
	if resp.Specs == nil {
		resp.Specs = []*orchestrator.FleetPlacementSpec{}
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleWithdrawFleetPlacementSpec(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.FleetPlacementSpecRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.WithdrawFleetPlacementSpec", "invalid request type")
	}
	var resp orchestrator.WithdrawFleetPlacementSpecResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, fleetPlacementPath(sdkReq.TenantID, sdkReq.Name, ""), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleGetFleetPlacementDrift(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.FleetPlacementSpecRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetFleetPlacementDrift", "invalid request type")
	}
	var resp orchestrator.FleetPlacementDrift
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, fleetPlacementPath(sdkReq.TenantID, sdkReq.Name, "/drift"), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
