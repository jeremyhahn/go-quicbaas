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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TenantSpendPolicySource is the agent service that holds the tenant
// spend-policy store and the ledger a tenant's usage is measured from.
type TenantSpendPolicySource interface {
	orchestration.TenantSpendPolicyAccessor

	// ConsumptionLedger returns the ledger usage is read from, or nil when
	// none is wired (the report then carries the limits alone).
	ConsumptionLedger() orchestration.ConsumptionLedger
}

// GetTenantSpendPolicy reads tenantID's own limits and its usage against each
// window.
//
// The caller must be of tenantID or of the system tenant: a tenant-A caller
// holding spend_policy:write may not read or replace tenant B's limits, and
// the check is made against the caller's own identity BEFORE the context is
// switched, after which every scope check passes (targetTenantContext;
// go-quicbaas#1058 review). The read then runs in tenantID's own storage
// namespace. Storage routes by the IAM tenant first, so scoping the
// orchestration key alone put a system admin's change to tenant T's limits
// in the admin's namespace, where T's admission never reads it; both keys
// are scoped.
func GetTenantSpendPolicy(ctx context.Context, source TenantSpendPolicySource, tenantID string) (*orchestration.TenantSpendPolicyReport, error) {
	if tenantID == "" {
		return nil, &orchestration.TenantSpendPolicyInvalidError{Field: "tenant_id", Message: "is required"}
	}
	ctx, err := targetTenantContext(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return orchestration.ReadTenantSpendPolicy(ctx, source.TenantSpendPolicies(), source.ConsumptionLedger(), tenantID, time.Time{})
}

// SetTenantSpendPolicy replaces tenantID's limits and returns the stored
// policy with its usage. Limits that meter nothing make the tenant unmetered.
// The same authorization and namespace rule as GetTenantSpendPolicy applies.
func SetTenantSpendPolicy(ctx context.Context, source TenantSpendPolicySource, tenantID string, limits orchestration.SpendLimits) (*orchestration.TenantSpendPolicyReport, error) {
	if tenantID == "" {
		return nil, &orchestration.TenantSpendPolicyInvalidError{Field: "tenant_id", Message: "is required"}
	}
	ctx, err := targetTenantContext(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := orchestration.WriteTenantSpendPolicy(ctx, source.TenantSpendPolicies(), tenantID, limits); err != nil {
		return nil, err
	}
	return orchestration.ReadTenantSpendPolicy(ctx, source.TenantSpendPolicies(), source.ConsumptionLedger(), tenantID, time.Time{})
}
