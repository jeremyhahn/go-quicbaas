package grpc

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// countingUserStorage counts the user record writes that reach storage. A role
// replacement made of a read followed by RevokeRole and AssignRole writes the
// record once per call; the single-write path writes it exactly once.
type countingUserStorage struct {
	iam.UserStorage
	mu     sync.Mutex
	writes int
}

func (c *countingUserStorage) UpdateUser(ctx context.Context, user *iam.User) error {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return c.UserStorage.UpdateUser(ctx, user)
}

func (c *countingUserStorage) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func (c *countingUserStorage) reset() {
	c.mu.Lock()
	c.writes = 0
	c.mu.Unlock()
}

// newCountingIAMGRPCStack builds the real gRPC stack over memory storage with
// the roles viewer, admin and auditor and one user, ada, holding [viewer].
func newCountingIAMGRPCStack(t *testing.T) (*IAMServer, *iam.MemoryStorage, *countingUserStorage, uint64) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	ctx := updateUserAuthzContext("acme")

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	counting := &countingUserStorage{UserStorage: mem}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: counting,
		RoleStorage: mem,
		Tenants:     tenants,
	})
	for _, id := range []string{"viewer", "admin", "auditor"} {
		_, err := store.CreateRole(ctx, "acme", iam.CreateRoleOptions{
			ID: id, Name: id, Permissions: []string{"users:read"},
		})
		require.NoError(t, err)
	}

	srv := NewIAMServer(NewIAMServiceAdapter(tenants, store, nil, nil, nil, nil), log)
	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada One",
		Lifecycle: string(iam.LifecycleActive),
		Roles:     []string{"viewer"},
	})
	require.NoError(t, err)
	// Grant roles:assign so what is under test is the replacement itself.
	srv.SetRoleAuthorizer(allowAllRoleAuthorizer{})

	userID, err := strconv.ParseUint(created.Id, 10, 64)
	require.NoError(t, err)
	counting.reset()
	return srv, mem, counting, userID
}

// TestGRPC_UpdateUserReplacesRolesExactly pins that a replacement of [viewer]
// with [viewer, admin] returns and stores exactly [viewer, admin].
func TestGRPC_UpdateUserReplacesRolesExactly(t *testing.T) {
	srv, mem, _, userID := newCountingIAMGRPCStack(t)
	ctx := updateUserAuthzContext("acme")

	got, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   strconv.FormatUint(userID, 10),
		Roles:    []string{"viewer", "admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer", "admin"}, got.Roles, "the response must carry the persisted roles")

	stored, err := mem.GetUser(ctx, "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer", "admin"}, stored.Roles)
}

// TestGRPC_UpdateUserNonexistentRoleLeavesRolesAndWritesNothing pins that a
// request naming a nonexistent role fails before any write.
func TestGRPC_UpdateUserNonexistentRoleLeavesRolesAndWritesNothing(t *testing.T) {
	srv, mem, counting, userID := newCountingIAMGRPCStack(t)
	ctx := updateUserAuthzContext("acme")

	_, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   strconv.FormatUint(userID, 10),
		Roles:    []string{"admin", "no-such-role"},
	})
	require.Error(t, err)
	assert.Zero(t, counting.writeCount(), "a refused replacement must not write the user")

	stored, err := mem.GetUser(ctx, "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles)
}

// TestGRPC_UpdateUserWithoutRolesLeavesRoles pins that an update naming no
// roles leaves the stored roles as they are.
func TestGRPC_UpdateUserWithoutRolesLeavesRoles(t *testing.T) {
	srv, mem, _, userID := newCountingIAMGRPCStack(t)
	ctx := updateUserAuthzContext("acme")

	got, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   strconv.FormatUint(userID, 10),
		Name:     "Ada Two",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, got.Roles)

	stored, err := mem.GetUser(ctx, "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer"}, stored.Roles)
	assert.Equal(t, "Ada Two", stored.Name)
}

// TestGRPC_UpdateUserReplacesRolesInOneWrite pins that a role replacement is
// a single write of the user record, not a read followed by RevokeRole and
// AssignRole calls (each of which writes the record again). Separate calls let
// two concurrent replacements interleave and leave a union neither requested.
func TestGRPC_UpdateUserReplacesRolesInOneWrite(t *testing.T) {
	srv, mem, counting, userID := newCountingIAMGRPCStack(t)
	ctx := updateUserAuthzContext("acme")

	// Swap viewer for auditor: the old path revoked one role and assigned
	// another, three writes in all.
	_, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   strconv.FormatUint(userID, 10),
		Name:     "Ada Two",
		Roles:    []string{"auditor"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, counting.writeCount(), "a role replacement must be one write, with no RevokeRole or AssignRole")

	stored, err := mem.GetUser(ctx, "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, []string{"auditor"}, stored.Roles)
	assert.Equal(t, "Ada Two", stored.Name)
}
