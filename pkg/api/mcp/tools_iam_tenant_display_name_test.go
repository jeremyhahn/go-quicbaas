//go:build mcp

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newTenantBackedIAMService routes tenant get/update to a real in-memory
// tenant manager holding the tenant "acme", the way the service layer does.
func newTenantBackedIAMService(t *testing.T) (*mockIAMService, *int) {
	t.Helper()
	tenants := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	_, err := tenants.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:       "acme",
		Name:     "Acme",
		Settings: &iam.TenantSettings{MaxUsers: 42, MFARequired: true},
	})
	require.NoError(t, err)

	updates := 0
	svc := &mockIAMService{
		getTenantFunc: func(ctx context.Context, req *service.GetTenantRequest) (*service.GetTenantResponse, error) {
			tenant, getErr := tenants.GetTenant(ctx, req.ID)
			if getErr != nil {
				return nil, getErr
			}
			return &service.GetTenantResponse{Tenant: tenant, Timestamp: time.Now()}, nil
		},
		updateTenantFunc: func(ctx context.Context, req *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error) {
			updates++
			tenant, updateErr := tenants.UpdateTenant(ctx, req.ID, iam.UpdateTenantOptions{
				Name:     req.Name,
				Settings: req.Settings,
				Quotas:   req.Quotas,
			})
			if updateErr != nil {
				return nil, updateErr
			}
			return &service.UpdateTenantResponse{Tenant: tenant, Timestamp: time.Now()}, nil
		},
	}
	return svc, &updates
}

func TestExecuteIAMTenantUpdate_DisplayNameOnlyKeepsNameAndSettings(t *testing.T) {
	svc, _ := newTenantBackedIAMService(t)
	reg := newIAMTestRegistry(svc)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	_, err := reg.executeIAMTenantUpdate(ctx, map[string]interface{}{
		"id":           "acme",
		"display_name": "Acme Corp",
	})
	require.NoError(t, err)

	got, err := svc.GetTenant(ctx, &service.GetTenantRequest{ID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.Tenant.Settings.DisplayName)
	assert.Equal(t, "Acme", got.Tenant.Name)
	assert.Equal(t, 42, got.Tenant.Settings.MaxUsers)
	assert.True(t, got.Tenant.Settings.MFARequired)
}

func TestExecuteIAMTenantUpdate_DisplayNameOfUnknownTenantIsNotFound(t *testing.T) {
	svc, updates := newTenantBackedIAMService(t)
	reg := newIAMTestRegistry(svc)
	ctx := tenantctx.WithTenant(context.Background(), "ghost")

	_, wantErr := svc.GetTenant(ctx, &service.GetTenantRequest{ID: "ghost"})
	require.Error(t, wantErr)

	_, err := reg.executeIAMTenantUpdate(ctx, map[string]interface{}{
		"id":           "ghost",
		"display_name": "Ghost Corp",
	})
	require.Error(t, err)
	assert.Equal(t, wantErr.Error(), err.Error())
	assert.Zero(t, *updates, "UpdateTenant must not run when the tenant is missing")
}
