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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// The gRPC server is the point where the proto request becomes
// CreateTenantOptions; a knob dropped here would be invisible to the caller,
// who would get a tenant created on defaults with a 200-shaped response.
func TestIAMServer_CreateTenant_ForwardsEngineProvisioningKnobs(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	ctx := context.Background()

	var captured iam.CreateTenantOptions
	svc.On("CreateTenant", ctx, mock.AnythingOfType("iam.CreateTenantOptions")).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(iam.CreateTenantOptions)
		}).
		Return(&iam.Tenant{ID: "acme", Name: "Acme"}, nil)

	_, err := srv.CreateTenant(ctx, &iampb.CreateTenantRequest{
		Id:            "acme",
		Name:          "Acme",
		IsolationType: string(qrdbsdk.TenantIsolationBarrier),
		Settings: &iampb.TenantSettings{
			MaxUsers:      100,
			NamedNetworks: map[string]string{"office": "10.0.0.0/8"},
		},
		EngineQuotas: &iampb.TenantEngineQuotas{
			MaxDatabases:       4,
			SystemBarrierQuota: -1,
		},
		EngineEntitlements: &iampb.TenantEngineEntitlements{
			AllowedIsolationModes: []string{"prefix", "barrier"},
			AllowDedicatedRaft:    true,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "acme", captured.ID)
	assert.Equal(t, "Acme", captured.Name)
	assert.Equal(t, qrdbsdk.TenantIsolationBarrier, captured.IsolationType)

	require.NotNil(t, captured.EngineQuotas)
	assert.Equal(t, int64(4), captured.EngineQuotas.MaxDatabases)
	assert.Equal(t, int64(-1), captured.EngineQuotas.SystemBarrierQuota)

	require.NotNil(t, captured.EngineEntitlements)
	assert.True(t, captured.EngineEntitlements.AllowDedicatedRaft)

	// Settings still travel; the new fields are additive, not a replacement.
	require.NotNil(t, captured.Settings)
	assert.Equal(t, 100, captured.Settings.MaxUsers)
	assert.Equal(t, []string{"10.0.0.0/8"}, captured.Settings.NamedNetworks["office"])

	svc.AssertExpectations(t)
}

// A pre-existing client that never learned about the knobs must still create
// the tenant on engine defaults, with nil (not empty) quota structures.
func TestIAMServer_CreateTenant_WithoutKnobsKeepsEngineDefaults(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	ctx := context.Background()

	var captured iam.CreateTenantOptions
	svc.On("CreateTenant", ctx, mock.AnythingOfType("iam.CreateTenantOptions")).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(iam.CreateTenantOptions)
		}).
		Return(&iam.Tenant{ID: "acme", Name: "Acme"}, nil)

	_, err := srv.CreateTenant(ctx, &iampb.CreateTenantRequest{Id: "acme", Name: "Acme"})
	require.NoError(t, err)

	assert.Empty(t, string(captured.IsolationType))
	assert.Nil(t, captured.EngineQuotas)
	assert.Nil(t, captured.EngineEntitlements)
	assert.Nil(t, captured.Settings)
}
