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

//go:build desktop

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorBinding_CostSettings asserts the desktop binding forwards
// both verbs and returns the SDK type.
func TestOrchestratorBinding_CostSettings(t *testing.T) {
	mock := &mockOrchestratorClient{}
	binding := newTestOrchestratorBinding(t, mock)
	t.Cleanup(func() { delete(mockCostSettingsRegistry, mock) })

	got, err := binding.GetCostSettings(&orchestratorSDK.GetCostSettingsRequest{TenantID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, 0.12, got.Electricity.PricePerKWh)

	rate := 0.2
	set, err := binding.SetCostSettings(&orchestratorSDK.SetCostSettingsRequest{TenantID: "acme", ElectricityPricePerKWh: &rate})
	require.NoError(t, err)
	assert.Equal(t, 0.2, set.Electricity.PricePerKWh)
	require.NotNil(t, mockCostSettingsRegistry[mock])

	_, err = binding.GetCostSettings(nil)
	assert.Error(t, err)
	_, err = binding.SetCostSettings(nil)
	assert.Error(t, err)
}
