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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingCostSettingsServer captures the cost settings requests.
type recordingCostSettingsServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu      sync.Mutex
	lastGet *pb.GetCostSettingsRequest
	lastSet *pb.SetCostSettingsRequest
}

func costSettingsResponse(tenantID string, rate *float64) *pb.CostSettingsResponse {
	return &pb.CostSettingsResponse{
		TenantId: tenantID, ElectricityPricePerKwh: rate, EnginePowerWatts: map[string]float64{"openvino": 12},
		UpdatedAt:                       timestamppb.New(time.Unix(1700000000, 0)),
		EffectiveElectricityPricePerKwh: 0.2, ElectricityPriceSource: "tenant_settings",
		EffectiveEnginePowerWatts: map[string]float64{"openvino": 12, "ollama": 250},
	}
}

func (s *recordingCostSettingsServer) GetCostSettings(_ context.Context, req *pb.GetCostSettingsRequest) (*pb.CostSettingsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastGet = req
	return costSettingsResponse(req.GetTenantId(), nil), nil
}

func (s *recordingCostSettingsServer) SetCostSettings(_ context.Context, req *pb.SetCostSettingsRequest) (*pb.CostSettingsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSet = req
	return costSettingsResponse(req.GetTenantId(), req.ElectricityPricePerKwh), nil
}

// TestOrchestratorCostSettings_OverGRPC is go-quicbaas#1058 on the SDK's gRPC
// (and so Unix) transport: both verbs dispatch, the context tenant crosses
// the wire, and the settings and rates come back whole.
func TestOrchestratorCostSettings_OverGRPC(t *testing.T) {
	fake := &recordingCostSettingsServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterOrchestratorServiceServer(s, fake) })
	h := newOrchestratorHandler(srv.dial(t))
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")

	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetCostSettings", &orchestrator.GetCostSettingsRequest{})
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", fake.lastGet.GetTenantId())
	got, ok := resp.(*orchestrator.CostSettings)
	require.Truef(t, ok, "response is %T", resp)
	assert.Nil(t, got.Settings.ElectricityPricePerKWh)
	assert.Equal(t, 0.2, got.Electricity.PricePerKWh)
	assert.Equal(t, "tenant_settings", got.Electricity.Source)
	assert.Equal(t, 250.0, got.EngineDraws["ollama"])
	require.NotNil(t, got.Settings.UpdatedAt)

	rate := 0.2
	resp, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestrator.SetCostSettingsRequest{
		ElectricityPricePerKWh: &rate, EnginePowerWatts: map[string]float64{"openvino": 12},
	})
	require.NoError(t, err)
	assert.Equal(t, 0.2, fake.lastSet.GetElectricityPricePerKwh())
	assert.Equal(t, 12.0, fake.lastSet.GetEnginePowerWatts()["openvino"])
	got = resp.(*orchestrator.CostSettings) //nolint:errcheck // asserted above for the same handler
	require.NotNil(t, got.Settings.ElectricityPricePerKWh)

	_, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "GetCostSettings", &orchestrator.SetCostSettingsRequest{})
	assert.Error(t, err, "a request of the wrong type is refused")
	_, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestrator.GetCostSettingsRequest{})
	assert.Error(t, err, "a request of the wrong type is refused")
}
