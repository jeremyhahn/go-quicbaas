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

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// mockCostSettingsRegistry holds each mock client's last cost-settings set
// request, so tests can assert what the command sent.
var mockCostSettingsRegistry = map[*mockOrchestratorClient]*orchestratorSDK.SetCostSettingsRequest{}

func (m *mockOrchestratorClient) GetCostSettings(_ context.Context, req *orchestratorSDK.GetCostSettingsRequest) (*orchestratorSDK.CostSettings, error) {
	return &orchestratorSDK.CostSettings{
		Settings:    orchestratorSDK.CostSettingValues{TenantID: req.TenantID},
		Electricity: orchestratorSDK.ElectricityRate{PricePerKWh: 0.12, Source: "default"},
	}, nil
}

func (m *mockOrchestratorClient) SetCostSettings(_ context.Context, req *orchestratorSDK.SetCostSettingsRequest) (*orchestratorSDK.CostSettings, error) {
	mockCostSettingsRegistry[m] = req
	rate := 0.12
	if req.ElectricityPricePerKWh != nil {
		rate = *req.ElectricityPricePerKWh
	}
	return &orchestratorSDK.CostSettings{
		Settings:    orchestratorSDK.CostSettingValues{TenantID: req.TenantID, ElectricityPricePerKWh: req.ElectricityPricePerKWh, EnginePowerWatts: req.EnginePowerWatts},
		Electricity: orchestratorSDK.ElectricityRate{PricePerKWh: rate, Source: "tenant_settings"},
		EngineDraws: req.EnginePowerWatts,
	}, nil
}

// runCostSettingsSet runs cost-settings set with args against mock.
func runCostSettingsSet(t *testing.T, mock *mockOrchestratorClient, args ...string) error {
	t.Helper()
	setMockOrchestratorFactory(mock)
	t.Cleanup(func() { delete(mockCostSettingsRegistry, mock) })
	cmd := newOrchCostSettingsCommand()
	cmd.SetArgs(append([]string{"set"}, args...))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.Execute()
}

func TestOrchCostSettingsSetSendsTheFlags(t *testing.T) {
	mock := &mockOrchestratorClient{}
	require.NoError(t, runCostSettingsSet(t, mock,
		"--electricity-price", "0.15", "--engine-watts", "openvino=12", "--engine-watts", "ollama=250"))
	sent := mockCostSettingsRegistry[mock]
	require.NotNil(t, sent)
	require.NotNil(t, sent.ElectricityPricePerKWh)
	assert.Equal(t, 0.15, *sent.ElectricityPricePerKWh)
	assert.Equal(t, map[string]float64{"openvino": 12, "ollama": 250}, sent.EnginePowerWatts)

	clearMock := &mockOrchestratorClient{}
	require.NoError(t, runCostSettingsSet(t, clearMock, "--clear"))
	cleared := mockCostSettingsRegistry[clearMock]
	require.NotNil(t, cleared)
	assert.Nil(t, cleared.ElectricityPricePerKWh)
	assert.Empty(t, cleared.EnginePowerWatts)
}

func TestOrchCostSettingsSetRefusesBadFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"nothing to set", nil},
		{"a negative price", []string{"--electricity-price", "-1"}},
		{"a price that is not a number", []string{"--electricity-price", "cheap"}},
		{"an engine with no watts", []string{"--engine-watts", "ollama"}},
		{"an unnamed engine", []string{"--engine-watts", "=5"}},
		{"infinite watts", []string{"--engine-watts", "ollama=Inf"}},
		{"clear with a setting", []string{"--clear", "--electricity-price", "0.1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockOrchestratorClient{}
			err := runCostSettingsSet(t, mock, tc.args...)
			require.Error(t, err)
			assert.Nil(t, mockCostSettingsRegistry[mock], "nothing may be sent")
		})
	}
	var flagErr *CostSettingsFlagError
	assert.True(t, errors.As(&CostSettingsFlagError{Flag: "x"}, &flagErr))
	assert.Contains(t, (&CostSettingsFlagsRequiredError{}).Error(), "--electricity-price")
}

func TestFormatCostSettingsTable(t *testing.T) {
	updated := time.Date(2026, 9, 23, 6, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	require.NoError(t, formatCostSettings(&orchestratorSDK.CostSettings{
		Settings: orchestratorSDK.CostSettingValues{
			TenantID: "acme", EnginePowerWatts: map[string]float64{"openvino": 12}, UpdatedAt: &updated,
		},
		Electricity: orchestratorSDK.ElectricityRate{PricePerKWh: 0.12, Source: "default"},
		EngineDraws: map[string]float64{"openvino": 12, "ollama": 250},
	}, "table", &out))
	text := out.String()
	assert.Contains(t, text, "$0.12/kWh (default)")
	assert.Regexp(t, `ollama\s+250\s+false`, text)
	assert.Regexp(t, `openvino\s+12\s+true`, text)
	assert.True(t, strings.Index(text, "ollama") < strings.Index(text, "openvino"), "engines are sorted")

	out.Reset()
	require.NoError(t, formatCostSettings(&orchestratorSDK.CostSettings{}, "json", &out))
	assert.Contains(t, out.String(), `"electricity"`)
	assert.Error(t, formatCostSettings(&orchestratorSDK.CostSettings{}, "xml", &out))
}
