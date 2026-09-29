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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// A tenant's own spend limits (go-quicbaas#1058)
// =============================================================================

// GetTenantSpendPolicyRequest names the tenant whose limits to read.
type GetTenantSpendPolicyRequest struct {
	TenantID string `json:"tenant_id"`
}

// SetTenantSpendPolicyRequest replaces a tenant's limits. Limits with no
// windows make the tenant unmetered.
type SetTenantSpendPolicyRequest struct {
	TenantID string       `json:"tenant_id"`
	Limits   spend.Limits `json:"limits"`
}

// TenantSpendPolicy is a tenant's own limits and its live usage against each
// window, over every turn charged to the tenant. Mirrors
// orchestration.TenantSpendPolicyReport.
type TenantSpendPolicy struct {
	TenantID string       `json:"tenant_id"`
	Limits   spend.Limits `json:"limits,omitzero"`
	// UpdatedAt is when the limits were last written; nil for a tenant that
	// never set any.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// Windows is each window with the tenant's usage in it.
	Windows []SpendWindowStatus `json:"windows,omitempty"`
}

// GetTenantSpendPolicy implements Client.GetTenantSpendPolicy.
func (c *client) GetTenantSpendPolicy(ctx context.Context, req *GetTenantSpendPolicyRequest) (*TenantSpendPolicy, error) {
	return c.tenantSpendPolicy(ctx, "GetTenantSpendPolicy", req)
}

// SetTenantSpendPolicy implements Client.SetTenantSpendPolicy.
func (c *client) SetTenantSpendPolicy(ctx context.Context, req *SetTenantSpendPolicyRequest) (*TenantSpendPolicy, error) {
	return c.tenantSpendPolicy(ctx, "SetTenantSpendPolicy", req)
}

// tenantSpendPolicy dispatches one tenant spend-policy verb.
func (c *client) tenantSpendPolicy(ctx context.Context, method string, req interface{}) (*TenantSpendPolicy, error) {
	op := "orchestrator." + method
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	switch r := req.(type) {
	case *GetTenantSpendPolicyRequest:
		if r == nil {
			return nil, errors.NewInvalidInput(op, "request is required")
		}
	case *SetTenantSpendPolicyRequest:
		if r == nil {
			return nil, errors.NewInvalidInput(op, "request is required")
		}
	}
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*TenantSpendPolicy)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}
