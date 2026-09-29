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

package grpc

import (
	"context"
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// GetCostSettings returns a tenant's runtime cost settings and the rates in
// force. go-quicbaas#1058.
func (s *OrchestratorServer) GetCostSettings(ctx context.Context, req *pb.GetCostSettingsRequest) (*pb.CostSettingsResponse, error) {
	if req == nil || req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	accessor, ok := s.agentSvc.(orchestration.CostSettingsAccessor)
	if !ok {
		return nil, status.Error(codes.Unavailable, "cost settings not available")
	}
	report, err := service.GetCostSettings(ctx, accessor, req.GetTenantId())
	if err != nil {
		return nil, costSettingsStatusError(err)
	}
	return costSettingsToProto(report), nil
}

// SetCostSettings replaces a tenant's runtime cost settings.
func (s *OrchestratorServer) SetCostSettings(ctx context.Context, req *pb.SetCostSettingsRequest) (*pb.CostSettingsResponse, error) {
	if req == nil || req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	accessor, ok := s.agentSvc.(orchestration.CostSettingsAccessor)
	if !ok {
		return nil, status.Error(codes.Unavailable, "cost settings not available")
	}
	report, err := service.SetCostSettings(ctx, accessor, req.GetTenantId(), orchestration.CostSettings{
		ElectricityPricePerKWh: req.ElectricityPricePerKwh,
		EnginePowerWatts:       req.GetEnginePowerWatts(),
	})
	if err != nil {
		return nil, costSettingsStatusError(err)
	}
	return costSettingsToProto(report), nil
}

// costSettingsStatusError maps a cost-settings failure to a status.
func costSettingsStatusError(err error) error {
	switch {
	case stderrors.Is(err, orchestration.ErrCostSettingsInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case stderrors.Is(err, orchestration.ErrAccountTenantScope):
		return status.Error(codes.PermissionDenied, err.Error())
	case stderrors.Is(err, orchestration.ErrCostSettingsStore):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Errorf(codes.Internal, "cost settings: %v", err)
	}
}

// costSettingsToProto renders a tenant's settings and the rates in force.
func costSettingsToProto(report *orchestration.CostSettingsReport) *pb.CostSettingsResponse {
	out := &pb.CostSettingsResponse{
		TenantId:                        report.Settings.TenantID,
		ElectricityPricePerKwh:          report.Settings.ElectricityPricePerKWh,
		EnginePowerWatts:                report.Settings.EnginePowerWatts,
		EffectiveElectricityPricePerKwh: report.Electricity.PricePerKWh,
		ElectricityPriceSource:          string(report.Electricity.Source),
		EffectiveEnginePowerWatts:       report.EngineDraws,
	}
	if !report.Settings.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(report.Settings.UpdatedAt)
	}
	return out
}
