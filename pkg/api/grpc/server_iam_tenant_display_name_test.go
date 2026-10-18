package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// TestGRPC_UpdateTenantStoresDisplayNameInSettings proves a
// settings.display_name update reads back as the display name, leaves the
// tenant's Name alone and keeps the other stored settings, MFA included,
// because the absent optional fields are left unchanged (#1518).
func TestGRPC_UpdateTenantStoresDisplayNameInSettings(t *testing.T) {
	srv, _ := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	_, err := srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{
		Id:       "acme",
		Settings: &iampb.TenantSettings{MaxUsers: proto.Int32(42), MfaRequired: proto.Bool(true)},
	})
	require.NoError(t, err)

	_, err = srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{
		Id:       "acme",
		Settings: &iampb.TenantSettings{DisplayName: proto.String("Acme Corp")},
	})
	require.NoError(t, err)

	got, err := srv.GetTenant(ctx, &iampb.GetTenantRequest{Id: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.GetSettings().GetDisplayName())
	assert.Equal(t, "Acme", got.GetName())
	assert.Equal(t, int32(42), got.GetSettings().GetMaxUsers())
	assert.True(t, got.GetSettings().GetMfaRequired())
}

// TestGRPC_UpdateTenantPresentFalseAndZeroAreApplied proves a present
// mfa_required=false turns MFA off and a present max_users=0 is stored,
// while the absent display_name is left unchanged (#1518).
func TestGRPC_UpdateTenantPresentFalseAndZeroAreApplied(t *testing.T) {
	srv, _ := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	_, err := srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{
		Id: "acme",
		Settings: &iampb.TenantSettings{
			MaxUsers:    proto.Int32(42),
			MfaRequired: proto.Bool(true),
			DisplayName: proto.String("Acme Corp"),
		},
	})
	require.NoError(t, err)

	_, err = srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{
		Id:       "acme",
		Settings: &iampb.TenantSettings{MaxUsers: proto.Int32(0), MfaRequired: proto.Bool(false)},
	})
	require.NoError(t, err)

	got, err := srv.GetTenant(ctx, &iampb.GetTenantRequest{Id: "acme"})
	require.NoError(t, err)
	assert.False(t, got.GetSettings().GetMfaRequired())
	assert.Equal(t, int32(0), got.GetSettings().GetMaxUsers())
	assert.Equal(t, "Acme Corp", got.GetSettings().GetDisplayName())
}

// TestGRPC_UpdateTenantDisplayNameOfUnknownTenantIsNotFound proves a
// display-name update of a tenant that does not exist answers NotFound.
func TestGRPC_UpdateTenantDisplayNameOfUnknownTenantIsNotFound(t *testing.T) {
	srv, _ := newRealIAMGRPCStack(t)

	_, err := srv.UpdateTenant(callerContext("missing"), &iampb.UpdateTenantRequest{
		Id:       "missing",
		Settings: &iampb.TenantSettings{DisplayName: proto.String("Nobody")},
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
