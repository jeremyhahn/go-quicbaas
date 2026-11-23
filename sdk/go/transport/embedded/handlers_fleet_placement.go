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
	"encoding/json"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// Fleet placement specs (go-quicbaas#1681) in process. The SDK types carry
// the server types' JSON names field for field, so the conversion is a JSON
// round trip: a field added on one side and not the other fails the
// round-trip test instead of being dropped silently.

func fleetPlacementOf(svc service.QuicBaaS, method string) (service.FleetPlacementService, error) {
	provider, ok := svc.(service.FleetPlacementProvider)
	if !ok || provider.FleetPlacement() == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", method)
	}
	return provider.FleetPlacement(), nil
}

// convertFleetPlacement copies in to a new Out through their shared JSON form.
func convertFleetPlacement[Out any](in any) (*Out, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("fleet placement: encode %T: %w", in, err)
	}
	out := new(Out)
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("fleet placement: decode into %T: %w", out, err)
	}
	return out, nil
}

func fleetPlacementSpecRequest(req interface{}, method string) (*orchestratorSDK.FleetPlacementSpecRequest, error) {
	r, ok := req.(*orchestratorSDK.FleetPlacementSpecRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", method,
			"*orchestrator.FleetPlacementSpecRequest", typeNameOf(req))
	}
	return r, nil
}

func handleOrchestratorDeclareFleetPlacementSpec(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "DeclareFleetPlacementSpec"
	r, ok := req.(*orchestratorSDK.DeclareFleetPlacementSpecRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", method,
			"*orchestrator.DeclareFleetPlacementSpecRequest", typeNameOf(req))
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	var spec *orchestration.PlacementSpec
	if r.Spec != nil {
		if spec, err = convertFleetPlacement[orchestration.PlacementSpec](r.Spec); err != nil {
			return nil, err
		}
	}
	stored, err := fp.DeclarePlacementSpec(ctx, r.TenantID, spec)
	if err != nil {
		return nil, err
	}
	return convertFleetPlacement[orchestratorSDK.FleetPlacementSpec](stored)
}

func handleOrchestratorGetFleetPlacementSpec(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "GetFleetPlacementSpec"
	r, err := fleetPlacementSpecRequest(req, method)
	if err != nil {
		return nil, err
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	spec, err := fp.GetPlacementSpec(ctx, r.TenantID, r.Name)
	if err != nil {
		return nil, err
	}
	return convertFleetPlacement[orchestratorSDK.FleetPlacementSpec](spec)
}

func handleOrchestratorListFleetPlacementSpecs(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "ListFleetPlacementSpecs"
	r, ok := req.(*orchestratorSDK.ListFleetPlacementSpecsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", method,
			"*orchestrator.ListFleetPlacementSpecsRequest", typeNameOf(req))
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	specs, err := fp.ListPlacementSpecs(ctx, r.TenantID)
	if err != nil {
		return nil, err
	}
	out := &orchestratorSDK.ListFleetPlacementSpecsResponse{Specs: make([]*orchestratorSDK.FleetPlacementSpec, 0, len(specs))}
	for _, spec := range specs {
		converted, err := convertFleetPlacement[orchestratorSDK.FleetPlacementSpec](spec)
		if err != nil {
			return nil, err
		}
		out.Specs = append(out.Specs, converted)
	}
	out.Count = len(out.Specs)
	return out, nil
}

func handleOrchestratorWithdrawFleetPlacementSpec(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "WithdrawFleetPlacementSpec"
	r, err := fleetPlacementSpecRequest(req, method)
	if err != nil {
		return nil, err
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	if err := fp.WithdrawPlacementSpec(ctx, r.TenantID, r.Name); err != nil {
		return nil, err
	}
	return &orchestratorSDK.WithdrawFleetPlacementSpecResponse{Name: r.Name, Withdrawn: true}, nil
}

func handleOrchestratorGetFleetPlacementDrift(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "GetFleetPlacementDrift"
	r, err := fleetPlacementSpecRequest(req, method)
	if err != nil {
		return nil, err
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	report, err := fp.GetPlacementDrift(ctx, r.TenantID, r.Name)
	if err != nil {
		return nil, err
	}
	return convertFleetPlacement[orchestratorSDK.FleetPlacementDrift](report)
}

func handleOrchestratorListFleetPlacementConsents(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "ListFleetPlacementConsents"
	r, ok := req.(*orchestratorSDK.ListFleetPlacementConsentsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", method,
			"*orchestrator.ListFleetPlacementConsentsRequest", typeNameOf(req))
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	requests, err := fp.ListConsentRequests(ctx, r.TenantID, orchestration.ConsentState(r.State))
	if err != nil {
		return nil, err
	}
	converted, err := convertFleetPlacement[[]*orchestratorSDK.FleetPlacementConsentRequest](requests)
	if err != nil {
		return nil, err
	}
	out := *converted
	if out == nil {
		out = []*orchestratorSDK.FleetPlacementConsentRequest{}
	}
	return &orchestratorSDK.ListFleetPlacementConsentsResponse{Requests: out, Count: len(out)}, nil
}

func handleOrchestratorAnswerFleetPlacementConsent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const method = "AnswerFleetPlacementConsent"
	r, ok := req.(*orchestratorSDK.AnswerFleetPlacementConsentRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", method,
			"*orchestrator.AnswerFleetPlacementConsentRequest", typeNameOf(req))
	}
	fp, err := fleetPlacementOf(svc, method)
	if err != nil {
		return nil, err
	}
	answered, err := fp.AnswerConsent(ctx, r.TenantID, r.ID, orchestration.ConsentAnswer{
		State: orchestration.ConsentState(r.State), Reason: r.Reason,
	})
	if err != nil {
		return nil, err
	}
	return convertFleetPlacement[orchestratorSDK.FleetPlacementConsentRequest](answered)
}
