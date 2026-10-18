package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// tenantStoreClient keeps one tenant and applies an update's
// settings.display_name to it, as the server does.
type tenantStoreClient struct {
	iampb.IAMServiceClient
	tenant  *iampb.Tenant
	updated *iampb.UpdateTenantRequest
}

func (c *tenantStoreClient) UpdateTenant(_ context.Context, in *iampb.UpdateTenantRequest, _ ...grpclib.CallOption) (*iampb.Tenant, error) {
	c.updated = in
	if c.tenant == nil || in.GetId() != c.tenant.GetId() {
		return nil, status.Error(codes.NotFound, "tenant not found")
	}
	if name := in.GetSettings().DisplayName; name != nil {
		c.tenant.Settings.DisplayName = name
	}
	return c.tenant, nil
}

func (c *tenantStoreClient) GetTenant(_ context.Context, in *iampb.GetTenantRequest, _ ...grpclib.CallOption) (*iampb.Tenant, error) {
	if c.tenant == nil || in.GetId() != c.tenant.GetId() {
		return nil, status.Error(codes.NotFound, "tenant not found")
	}
	return c.tenant, nil
}

// TestIAMHandler_UpdateTenantSendsDisplayNameInSettings proves the SDK sends
// DisplayName as settings.display_name, not as the tenant's name, and reads
// it back from settings.display_name (#1518).
func TestIAMHandler_UpdateTenantSendsDisplayNameInSettings(t *testing.T) {
	client := &tenantStoreClient{tenant: &iampb.Tenant{
		Id: "acme", Name: "acme", Settings: &iampb.TenantSettings{MaxUsers: proto.Int32(42), MfaRequired: proto.Bool(true)},
	}}
	h := &iamHandler{}

	_, err := h.handleUpdateTenant(context.Background(), client, map[string]interface{}{
		"tenant_id": "acme",
		"request":   &iam.UpdateTenantRequest{DisplayName: "Acme Corp"},
	})
	require.NoError(t, err)
	assert.Empty(t, client.updated.GetName())
	assert.Equal(t, "Acme Corp", client.updated.GetSettings().GetDisplayName())
	// A display-name update must not send the other optional settings, or
	// the server would apply them (#1518).
	assert.Nil(t, client.updated.GetSettings().MfaRequired)
	assert.Nil(t, client.updated.GetSettings().MaxUsers)

	got, err := h.handleGetTenant(context.Background(), client, map[string]interface{}{"tenant_id": "acme"})
	require.NoError(t, err)
	tenant := got.(*iam.Tenant)
	assert.Equal(t, "Acme Corp", tenant.DisplayName)
	assert.Equal(t, "acme", tenant.Name)
}

// TestIAMHandler_UpdateTenantDisplayNameOfUnknownTenantFails proves a
// display-name update of a tenant that does not exist returns an error.
func TestIAMHandler_UpdateTenantDisplayNameOfUnknownTenantFails(t *testing.T) {
	h := &iamHandler{}

	_, err := h.handleUpdateTenant(context.Background(), &tenantStoreClient{}, map[string]interface{}{
		"tenant_id": "missing",
		"request":   &iam.UpdateTenantRequest{DisplayName: "Nobody"},
	})

	require.Error(t, err)
}
