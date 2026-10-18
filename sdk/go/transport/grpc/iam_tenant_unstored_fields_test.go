package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestIAMHandler_UpdateTenantRefusesUnstoredFields proves the gRPC handler,
// which the Unix transport shares, refuses description, metadata or status:
// the gRPC wire cannot carry them, so it makes no call rather than drop them
// silently, and the tenant is unchanged (#1518).
func TestIAMHandler_UpdateTenantRefusesUnstoredFields(t *testing.T) {
	status := iam.TenantStatusActive
	cases := map[string]*iam.UpdateTenantRequest{
		"description": {DisplayName: "New", Description: "d"},
		"metadata":    {DisplayName: "New", Metadata: map[string]string{"k": "v"}},
		"status":      {DisplayName: "New", Status: &status},
	}
	for field, req := range cases {
		t.Run(field, func(t *testing.T) {
			old := "Old"
			client := &tenantStoreClient{tenant: &iampb.Tenant{
				Id: "acme", Name: "acme", Settings: &iampb.TenantSettings{DisplayName: &old},
			}}
			h := &iamHandler{}

			_, err := h.handleUpdateTenant(context.Background(), client, map[string]interface{}{
				"tenant_id": "acme",
				"request":   req,
			})

			var refused *iam.TenantFieldNotStoredError
			require.True(t, errors.As(err, &refused), "err = %v", err)
			assert.Equal(t, []string{field}, refused.Fields)
			assert.True(t, errors.Is(err, iam.ErrTenantFieldNotStored))
			assert.Nil(t, client.updated, "no UpdateTenant call may be made")
			got, err := h.handleGetTenant(context.Background(), client, map[string]interface{}{"tenant_id": "acme"})
			require.NoError(t, err)
			assert.Equal(t, "Old", got.(*iam.Tenant).DisplayName)
		})
	}
}
