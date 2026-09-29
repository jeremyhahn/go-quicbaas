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

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// GetCostSettings reads tenantID's runtime cost settings — its electricity
// rate and engine draws — and the rates in force once they are layered over
// the configuration.
//
// The caller must be of tenantID or of the system tenant
// (targetTenantContext), and the read then runs in tenantID's own storage
// namespace: storage routes by the IAM tenant first, so both tenant keys are
// scoped.
func GetCostSettings(ctx context.Context, source orchestration.CostSettingsAccessor, tenantID string) (*orchestration.CostSettingsReport, error) {
	if tenantID == "" {
		return nil, &orchestration.CostSettingsInvalidError{Field: "tenant_id", Message: "is required"}
	}
	ctx, err := targetTenantContext(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return orchestration.ReadCostSettings(ctx, source.CostSettingsStore(), source.PriceBook(), tenantID)
}

// SetCostSettings replaces tenantID's runtime cost settings and returns the
// rates they leave in force. Settings that override nothing restore the
// configuration. The same authorization and namespace rule as
// GetCostSettings applies.
func SetCostSettings(ctx context.Context, source orchestration.CostSettingsAccessor, tenantID string, settings orchestration.CostSettings) (*orchestration.CostSettingsReport, error) {
	if tenantID == "" {
		return nil, &orchestration.CostSettingsInvalidError{Field: "tenant_id", Message: "is required"}
	}
	ctx, err := targetTenantContext(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	settings.TenantID = tenantID
	return orchestration.WriteCostSettings(ctx, source.CostSettingsStore(), source.PriceBook(), &settings)
}
