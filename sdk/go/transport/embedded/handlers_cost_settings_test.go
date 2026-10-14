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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// costSettingsFakeAgentService exposes a cost settings store.
type costSettingsFakeAgentService struct {
	*fakeAgentService
	store orchestration.CostSettingsStore
}

func (f *costSettingsFakeAgentService) CostSettingsStore() orchestration.CostSettingsStore {
	return f.store
}
func (f *costSettingsFakeAgentService) PriceBook() *orchestration.PriceBook { return nil }

func newCostSettingsTransport(t *testing.T, wired bool) *Transport {
	t.Helper()
	var agent orchestration.AgentService = &fakeAgentService{}
	if wired {
		agent = &costSettingsFakeAgentService{fakeAgentService: &fakeAgentService{}, store: orchestration.NewInMemoryCostSettingsStore()}
	}
	tr, err := New(&orchMock{mockQuicBaaS: &mockQuicBaaS{}, agent: agent, proj: &fakeProjectService{}, admin: &fakeFleetAdminService{}})
	require.NoError(t, err)
	return tr
}

// TestEmbeddedCostSettings sets and reads a tenant's cost settings in process.
func TestEmbeddedCostSettings(t *testing.T) {
	tr := newCostSettingsTransport(t, true)
	ctx := transport.ContextWithTenant(context.Background(), "t1")

	resp, err := tr.Request(ctx, "quicbaas.Orchestrator", "GetCostSettings", &orchestratorSDK.GetCostSettingsRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 0.12, resp.(*orchestratorSDK.CostSettings).Electricity.PricePerKWh)

	rate := 0.2
	resp, err = tr.Request(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestratorSDK.SetCostSettingsRequest{
		TenantID: "t1", ElectricityPricePerKWh: &rate, EnginePowerWatts: map[string]float64{"openvino": 12},
	})
	require.NoError(t, err)
	got, ok := resp.(*orchestratorSDK.CostSettings)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, "tenant_settings", got.Electricity.Source)
	assert.Equal(t, 12.0, got.EngineDraws["openvino"])
	require.NotNil(t, got.Settings.UpdatedAt)
}

func TestEmbeddedCostSettingsRefuses(t *testing.T) {
	ctx := transport.ContextWithTenant(context.Background(), "t1")
	bare := newCostSettingsTransport(t, false)
	_, err := bare.Request(ctx, "quicbaas.Orchestrator", "GetCostSettings", &orchestratorSDK.GetCostSettingsRequest{TenantID: "t1"})
	assert.Error(t, err, "a deployment with no cost settings refuses")
	_, err = bare.Request(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestratorSDK.SetCostSettingsRequest{TenantID: "t1"})
	assert.Error(t, err)

	wired := newCostSettingsTransport(t, true)
	_, err = wired.Request(ctx, "quicbaas.Orchestrator", "GetCostSettings", &orchestratorSDK.SetCostSettingsRequest{})
	assert.Error(t, err, "a request of the wrong type is refused")
	_, err = wired.Request(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestratorSDK.GetCostSettingsRequest{})
	assert.Error(t, err)
	rate := -1.0
	_, err = wired.Request(ctx, "quicbaas.Orchestrator", "SetCostSettings", &orchestratorSDK.SetCostSettingsRequest{TenantID: "t1", ElectricityPricePerKWh: &rate})
	assert.ErrorIs(t, err, orchestration.ErrCostSettingsInvalid)

	// A caller that names no tenant runs as the system tenant, which gets no
	// lift into t1 (#1150).
	_, err = wired.Request(context.Background(), "quicbaas.Orchestrator", "GetCostSettings", &orchestratorSDK.GetCostSettingsRequest{TenantID: "t1"})
	assert.ErrorIs(t, err, orchestration.ErrAccountTenantScope)
}
