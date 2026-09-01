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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// The service wrapper is the last hop before the tenant manager, so a knob
// dropped here would defeat every transport at once.
func TestIAMServiceWrapper_CreateTenant_ForwardsEngineProvisioningKnobs(t *testing.T) {
	var captured iam.CreateTenantOptions
	mockTM := &mockTenantManager{
		createFn: func(_ context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
			captured = opts
			return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
		},
	}
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{tenantMgr: mockTM}}

	quotas := &qrdbsdk.Quotas{MaxDatabases: 4, SystemBarrierQuota: -1}
	entitlements := &qrdbsdk.Entitlements{AllowDedicatedRaft: true}

	_, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{
		ID:                 "acme",
		Name:               "Acme",
		IsolationType:      qrdbsdk.TenantIsolationBarrier,
		EngineQuotas:       quotas,
		EngineEntitlements: entitlements,
	})
	require.NoError(t, err)

	assert.Equal(t, qrdbsdk.TenantIsolationBarrier, captured.IsolationType)
	assert.Same(t, quotas, captured.EngineQuotas)
	assert.Same(t, entitlements, captured.EngineEntitlements)
}

func TestIAMServiceWrapper_CreateTenant_OmittedKnobsStayNil(t *testing.T) {
	var captured iam.CreateTenantOptions
	mockTM := &mockTenantManager{
		createFn: func(_ context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
			captured = opts
			return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
		},
	}
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{tenantMgr: mockTM}}

	_, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{Name: "Acme"})
	require.NoError(t, err)

	assert.Empty(t, string(captured.IsolationType))
	assert.Nil(t, captured.EngineQuotas)
	assert.Nil(t, captured.EngineEntitlements)
}
