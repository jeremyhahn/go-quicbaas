package grpc

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// allowAllRoleAuthorizer grants every permission, for UpdateUser tests that
// name roles but test something other than the roles:assign check.
type allowAllRoleAuthorizer struct{}

func (allowAllRoleAuthorizer) Authorize(context.Context, string, string, string, string, string) (*platformtoken.AuthorizedContext, error) {
	return &platformtoken.AuthorizedContext{}, nil
}

const updateUserAuthzToken = "update-user-token"

func updateUserAuthzContext(tenantID string) context.Context {
	md := metadata.New(map[string]string{"authorization": "Bearer " + updateUserAuthzToken})
	return metadata.NewIncomingContext(callerContext(tenantID), md)
}

// seedUpdateUserAuthz creates the roles viewer and admin and a user holding
// viewer in tenant acme, returning the user's id.
func seedUpdateUserAuthz(t *testing.T, srv *IAMServer, mem *iam.MemoryStorage) (string, uint64) {
	t.Helper()
	ctx := callerContext("acme")
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	for _, id := range []string{"viewer", "admin"} {
		_, err := store.CreateRole(ctx, "acme", iam.CreateRoleOptions{
			ID: id, Name: id, Permissions: []string{"users:read"},
		})
		require.NoError(t, err)
	}
	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada One",
		Lifecycle: string(iam.LifecycleActive),
		Roles:     []string{"viewer"},
	})
	require.NoError(t, err)
	userID, err := strconv.ParseUint(created.Id, 10, 64)
	require.NoError(t, err)
	return created.Id, userID
}

func TestGRPC_UpdateUserRolesWithoutRolesAssignIsDenied(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, granted: map[string]bool{"users:update": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Two", Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, []string{"roles:assign"}, authz.asked)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "a denied update changed the user's roles")
	assert.Equal(t, "Ada One", stored.Name, "a denied update wrote the profile")
}

func TestGRPC_UpdateUserRolesWithRolesAssignSucceeds(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, granted: map[string]bool{"users:update": true, "roles:assign": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"roles:assign"}, authz.asked)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, stored.Roles)
}

func TestGRPC_UpdateUserRolesWithoutAuthorizerIsRefused(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Roles: []string{"admin"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "an update with no authorizer changed the user's roles")
}

func TestGRPC_UpdateUserWithoutRolesSkipsRolesAssign(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	id, userID := seedUpdateUserAuthz(t, srv, mem)
	authz := &grantSetAuthorizer{token: updateUserAuthzToken, granted: map[string]bool{"users:update": true}}
	srv.SetRoleAuthorizer(authz)

	_, err := srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Two",
	})
	require.NoError(t, err)
	assert.Empty(t, authz.asked, "an update naming no roles asked for a permission")

	// With no authorizer at all, an update naming no roles is unchanged too.
	srv.SetRoleAuthorizer(nil)
	_, err = srv.UpdateUser(updateUserAuthzContext("acme"), &iampb.UpdateUserRequest{
		TenantId: "acme", UserId: id, Name: "Ada Three",
	})
	require.NoError(t, err)

	stored, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, "Ada Three", stored.Name)
	assert.Equal(t, []string{"viewer"}, stored.Roles)
}
