package grpc

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestGRPC_UpdateUserWithANonexistentRoleKeepsTheOldRole pins that UpdateUser
// validates every requested role before it writes anything. The role sync
// revokes roles absent from the request before it assigns the new ones, so a
// replacement that does not exist used to strip the user's access and only
// then fail.
func TestGRPC_UpdateUserWithANonexistentRoleKeepsTheOldRole(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	_, err := store.CreateRole(ctx, "acme", iam.CreateRoleOptions{
		ID: "viewer", Name: "viewer", Permissions: []string{"users:read"},
	})
	require.NoError(t, err)

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada One",
		Lifecycle: string(iam.LifecycleActive),
		Roles:     []string{"viewer"},
	})
	require.NoError(t, err)

	_, err = srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   created.Id,
		Name:     "Ada Two",
		Roles:    []string{"no-such-role"},
	})
	require.Error(t, err, "a nonexistent replacement role must be refused")

	userID, err := strconv.ParseUint(created.Id, 10, 64)
	require.NoError(t, err)
	stored, err := mem.GetUser(ctx, "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles, "the refused update revoked the user's existing role")
	assert.Equal(t, "Ada One", stored.Name, "the refused update wrote the profile")
}
