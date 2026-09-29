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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// handleOrchestratorGetCostSettings calls the unary
// OrchestratorService.GetCostSettings RPC.
func handleOrchestratorGetCostSettings(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetCostSettingsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetCostSettings", "invalid request type")
	}
	tenantID := sdkReq.TenantID
	if tenantID == "" {
		tenantID = transport.TenantFromContext(ctx)
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).GetCostSettings(ctx, &pb.GetCostSettingsRequest{TenantId: tenantID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetCostSettings")
	}
	return costSettingsFromProto(resp), nil
}

// handleOrchestratorSetCostSettings calls the unary
// OrchestratorService.SetCostSettings RPC.
func handleOrchestratorSetCostSettings(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SetCostSettingsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SetCostSettings", "invalid request type")
	}
	tenantID := sdkReq.TenantID
	if tenantID == "" {
		tenantID = transport.TenantFromContext(ctx)
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).SetCostSettings(ctx, &pb.SetCostSettingsRequest{
		TenantId:               tenantID,
		ElectricityPricePerKwh: sdkReq.ElectricityPricePerKWh,
		EnginePowerWatts:       sdkReq.EnginePowerWatts,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SetCostSettings")
	}
	return costSettingsFromProto(resp), nil
}

// costSettingsFromProto converts a tenant's settings and the rates in force.
func costSettingsFromProto(resp *pb.CostSettingsResponse) *orchestrator.CostSettings {
	out := &orchestrator.CostSettings{
		Settings: orchestrator.CostSettingValues{
			TenantID:               resp.GetTenantId(),
			ElectricityPricePerKWh: resp.ElectricityPricePerKwh,
			EnginePowerWatts:       resp.GetEnginePowerWatts(),
		},
		Electricity: orchestrator.ElectricityRate{
			PricePerKWh: resp.GetEffectiveElectricityPricePerKwh(),
			Source:      resp.GetElectricityPriceSource(),
		},
		EngineDraws: resp.GetEffectiveEnginePowerWatts(),
	}
	if resp.GetUpdatedAt() != nil {
		updated := resp.GetUpdatedAt().AsTime()
		out.Settings.UpdatedAt = &updated
	}
	return out
}
