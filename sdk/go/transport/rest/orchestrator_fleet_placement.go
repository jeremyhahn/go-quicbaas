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

func (h *orchestratorHandler) handleListFleetPlacementConsents(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListFleetPlacementConsentsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListFleetPlacementConsents", "invalid request type")
	}
	path := "/api/v1/fleet/placement-consents"
	query := neturl.Values{}
	if sdkReq.TenantID != "" {
		query.Set("tenant_id", sdkReq.TenantID)
	}
	if sdkReq.State != "" {
		query.Set("state", sdkReq.State)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var resp orchestrator.ListFleetPlacementConsentsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Requests == nil {
		resp.Requests = []*orchestrator.FleetPlacementConsentRequest{}
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleAnswerFleetPlacementConsent(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.AnswerFleetPlacementConsentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.AnswerFleetPlacementConsent", "invalid request type")
	}
	path := "/api/v1/fleet/placement-consents/" + neturl.PathEscape(sdkReq.ID) + "/answer"
	if sdkReq.TenantID != "" {
		path += "?tenant_id=" + neturl.QueryEscape(sdkReq.TenantID)
	}
	body := struct {
		State  string `json:"state"`
		Reason string `json:"reason,omitempty"`
	}{State: sdkReq.State, Reason: sdkReq.Reason}
	var resp orchestrator.FleetPlacementConsentRequest
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
