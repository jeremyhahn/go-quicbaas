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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// costSettingsAgentService exposes a cost settings store.
type costSettingsAgentService struct {
	grpcTestAgentService
	store orchestration.CostSettingsStore
}

func (m *costSettingsAgentService) CostSettingsStore() orchestration.CostSettingsStore {
	return m.store
}
func (m *costSettingsAgentService) PriceBook() *orchestration.PriceBook { return nil }

// TestCostSettingsOverGRPC is go-quicbaas#1058: the default rate, then a set
// rate and draw read back in force.
func TestCostSettingsOverGRPC(t *testing.T) {
	srv := NewOrchestratorServer(&costSettingsAgentService{store: orchestration.NewInMemoryCostSettingsStore()},
		createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	got, err := srv.GetCostSettings(ctx, &pb.GetCostSettingsRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 0.12, got.GetEffectiveElectricityPricePerKwh())
	assert.Equal(t, "default", got.GetElectricityPriceSource())
	assert.Nil(t, got.ElectricityPricePerKwh, "nothing set is unset, not zero")

	set, err := srv.SetCostSettings(ctx, &pb.SetCostSettingsRequest{
		TenantId: "t1", ElectricityPricePerKwh: proto.Float64(0.2), EnginePowerWatts: map[string]float64{"openvino": 12},
	})
	require.NoError(t, err)
	assert.Equal(t, 0.2, set.GetElectricityPricePerKwh())
	assert.Equal(t, "tenant_settings", set.GetElectricityPriceSource())
	assert.Equal(t, 12.0, set.GetEffectiveEnginePowerWatts()["openvino"])
	assert.NotNil(t, set.GetUpdatedAt())
}

func TestCostSettingsOverGRPCRefuses(t *testing.T) {
	wired := NewOrchestratorServer(&costSettingsAgentService{store: orchestration.NewInMemoryCostSettingsStore()},
		createOrchestratorGRPCTestLogger())
	bare := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"get with no tenant", func() error { _, err := wired.GetCostSettings(ctx, &pb.GetCostSettingsRequest{}); return err }, codes.InvalidArgument},
		{"set with no tenant", func() error { _, err := wired.SetCostSettings(ctx, &pb.SetCostSettingsRequest{}); return err }, codes.InvalidArgument},
		{"a negative rate", func() error {
			_, err := wired.SetCostSettings(ctx, &pb.SetCostSettingsRequest{TenantId: "t1", ElectricityPricePerKwh: proto.Float64(-1)})
			return err
		}, codes.InvalidArgument},
		{"an unwired get", func() error {
			_, err := bare.GetCostSettings(ctx, &pb.GetCostSettingsRequest{TenantId: "t1"})
			return err
		}, codes.Unavailable},
		{"an unwired set", func() error {
			_, err := bare.SetCostSettings(ctx, &pb.SetCostSettingsRequest{TenantId: "t1"})
			return err
		}, codes.Unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, status.Code(tc.call()))
		})
	}
}
