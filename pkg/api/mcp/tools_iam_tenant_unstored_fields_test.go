//go:build mcp

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestExecuteIAMTenantUpdate_RefusesUnstoredFields proves the tenant update
// tool refuses description, metadata or status as invalid input naming the
// field, makes no update, and leaves the tenant unchanged (#1518).
func TestExecuteIAMTenantUpdate_RefusesUnstoredFields(t *testing.T) {
	cases := map[string]interface{}{
		"description": "d",
		"metadata":    map[string]interface{}{"k": "v"},
		"status":      "suspended",
	}
	for field, value := range cases {
		t.Run(field, func(t *testing.T) {
			svc, updates := newTenantBackedIAMService(t)
			reg := newIAMTestRegistry(svc)
			ctx := tenantctx.WithTenant(context.Background(), "acme")

			_, err := reg.executeIAMTenantUpdate(ctx, map[string]interface{}{
				"id":           "acme",
				"display_name": "Acme Corp",
				field:          value,
			})

			require.True(t, errors.Is(err, iam.ErrTenantFieldNotStored), "err = %v", err)
			assert.Equal(t, mcptransport.MCPInvalidInput, toolErrorCode(err))
			assert.Contains(t, err.Error(), field)
			assert.Zero(t, *updates)
			got, err := svc.GetTenant(ctx, &service.GetTenantRequest{ID: "acme"})
			require.NoError(t, err)
			assert.Empty(t, got.Tenant.Settings.DisplayName)
		})
	}
}

// TestExecuteIAMTenantUpdate_EmptyMetadataIsNotRefused proves an empty
// metadata object is absent, so the display-name update still applies.
func TestExecuteIAMTenantUpdate_EmptyMetadataIsNotRefused(t *testing.T) {
	svc, _ := newTenantBackedIAMService(t)
	reg := newIAMTestRegistry(svc)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	_, err := reg.executeIAMTenantUpdate(ctx, map[string]interface{}{
		"id":           "acme",
		"display_name": "Acme Corp",
		"metadata":     map[string]interface{}{},
	})

	require.NoError(t, err)
	got, err := svc.GetTenant(ctx, &service.GetTenantRequest{ID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.Tenant.Settings.DisplayName)
}
