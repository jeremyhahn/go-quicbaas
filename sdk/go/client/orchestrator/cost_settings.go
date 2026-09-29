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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// A tenant's runtime cost settings (go-quicbaas#1058)
// =============================================================================
//
// Local inference is costed from electricity:
//
//	power_watts × turn seconds / 3.6e6 kWh × electricity_price_per_kwh
//
// These are the two inputs a tenant can set while the node runs. Each beats
// the tenant's configured override, which beats the deployment's; the rate
// defaults to $0.12/kWh.

// GetCostSettingsRequest names the tenant whose cost settings to read.
type GetCostSettingsRequest struct {
	TenantID string `json:"tenant_id"`
}

// SetCostSettingsRequest replaces a tenant's cost settings. A nil rate and an
// engine not named inherit the configuration; an engine named with zero watts
// is not costed from electricity for the tenant.
type SetCostSettingsRequest struct {
	TenantID               string             `json:"tenant_id"`
	ElectricityPricePerKWh *float64           `json:"electricity_price_per_kwh,omitempty"`
	EnginePowerWatts       map[string]float64 `json:"engine_power_watts,omitempty"`
}

// CostSettingValues is what a tenant set at runtime.
type CostSettingValues struct {
	TenantID               string             `json:"tenant_id"`
	ElectricityPricePerKWh *float64           `json:"electricity_price_per_kwh,omitempty"`
	EnginePowerWatts       map[string]float64 `json:"engine_power_watts,omitempty"`
	// UpdatedAt is when the settings were last written; nil for a tenant that
	// never set any.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ElectricityRate is the rate a tenant's turns are costed at and where it
// came from: "tenant_settings", "tenant_config", "deployment_config" or
// "default".
type ElectricityRate struct {
	PricePerKWh float64 `json:"price_per_kwh"`
	Source      string  `json:"source"`
}

// CostSettings is a tenant's runtime settings and the rates in force once
// they are layered over the configuration. Mirrors
// orchestration.CostSettingsReport.
type CostSettings struct {
	Settings    CostSettingValues `json:"settings"`
	Electricity ElectricityRate   `json:"electricity"`
	// EngineDraws is every per-engine draw in force, in watts.
	EngineDraws map[string]float64 `json:"engine_power_watts,omitempty"`
}

// GetCostSettings implements Client.GetCostSettings.
func (c *client) GetCostSettings(ctx context.Context, req *GetCostSettingsRequest) (*CostSettings, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetCostSettings", "request is required")
	}
	return c.costSettings(ctx, "GetCostSettings", req)
}

// SetCostSettings implements Client.SetCostSettings.
func (c *client) SetCostSettings(ctx context.Context, req *SetCostSettingsRequest) (*CostSettings, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.SetCostSettings", "request is required")
	}
	return c.costSettings(ctx, "SetCostSettings", req)
}

// costSettings dispatches one cost-settings verb.
func (c *client) costSettings(ctx context.Context, method string, req interface{}) (*CostSettings, error) {
	op := "orchestrator." + method
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*CostSettings)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}
