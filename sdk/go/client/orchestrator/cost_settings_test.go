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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostSettings_HappyPath(t *testing.T) {
	want := &CostSettings{Electricity: ElectricityRate{PricePerKWh: 0.12, Source: "default"}}
	got, err := NewClient(okTransport(t, "GetCostSettings", want)).GetCostSettings(context.Background(), &GetCostSettingsRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 0.12, got.Electricity.PricePerKWh)

	rate := 0.2
	got, err = NewClient(okTransport(t, "SetCostSettings", want)).SetCostSettings(context.Background(),
		&SetCostSettingsRequest{TenantID: "t1", ElectricityPricePerKWh: &rate})
	require.NoError(t, err)
	assert.Equal(t, "default", got.Electricity.Source)
}

func TestCostSettings_Validation(t *testing.T) {
	c := NewClient(okTransport(t, "GetCostSettings", &CostSettings{}))
	_, err := c.GetCostSettings(context.Background(), nil)
	assert.Error(t, err, "a nil request is refused")
	_, err = c.SetCostSettings(context.Background(), nil)
	assert.Error(t, err, "a nil request is refused")
	//nolint:staticcheck // a nil context is the case under test
	_, err = c.GetCostSettings(nil, &GetCostSettingsRequest{TenantID: "t1"})
	assert.Error(t, err, "a nil context is refused")

	wrong := NewClient(okTransport(t, "GetCostSettings", &GetSpendReportResponse{}))
	_, err = wrong.GetCostSettings(context.Background(), &GetCostSettingsRequest{TenantID: "t1"})
	assert.Error(t, err, "a response of the wrong type is refused")
}
