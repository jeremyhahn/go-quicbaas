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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// This file drives the WHOLE gRPC chain — protobuf request, IAMServer, the
// service adapter, the identity store, the in-memory storage — rather than a
// mock, because the defects it covers lived in the translation between those
// layers. The Unix transport is the gRPC transport over a domain socket, so it
// speaks this exact contract.

// newRealIAMGRPCStack builds an IAMServer over a real identity store.
func newRealIAMGRPCStack(t *testing.T) (*IAMServer, *iam.MemoryStorage) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(callerContext("acme"), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
		Tenants:     tenants,
	})
	adapter := NewIAMServiceAdapter(tenants, store, nil, nil, nil, nil)
	return NewIAMServer(adapter, log), mem
}

// TestGRPC_CreateUserCarriesTheRequestedUsername proves the protobuf request
// carries a username distinct from the display name all the way to the store.
//
// It could not before: the message had no username field at all, so this
// transport derived one from the local part of the email and the caller's
// username was unreachable — the same SDK call meant different things depending
// on the wire underneath it.
func TestGRPC_CreateUserCarriesTheRequestedUsername(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId: "acme",
		// The email local part is DELIBERATELY not the handle. This transport
		// derives a handle from the email when none is sent, so an email of
		// "grace@acme.test" would make a dropped username indistinguishable
		// from a carried one.
		Email:     "ghopper@acme.test",
		Username:  "grace",
		Name:      "Grace Hopper",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)
	assert.Equal(t, "grace", created.Username, "the response dropped the username")
	assert.Equal(t, "Grace Hopper", created.Name)

	if _, err := mem.GetUserByUsername(ctx, "acme", "ghopper"); err == nil {
		t.Fatal("the email-derived handle was stored: the requested username was dropped")
	}
	stored, err := mem.GetUserByUsername(ctx, "acme", "grace")
	require.NoError(t, err, "the account must resolve by the username the request asked for")
	assert.Equal(t, "Grace Hopper", stored.Name)

	_, err = mem.GetUserByUsername(ctx, "acme", "Grace Hopper")
	assert.Error(t, err, "the display name must not resolve as a username")
}

// TestGRPC_CreateUserWithoutAUsernameStillDerivesOne pins the behaviour a caller
// that names no username relies on — this transport has always supplied one, and
// making the field carryable must not make it mandatory.
func TestGRPC_CreateUserWithoutAUsernameStillDerivesOne(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "heidi@acme.test",
		Name:      "Heidi",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)
	assert.Equal(t, "heidi", created.Username, "an omitted username must be derived from the email")

	_, err = mem.GetUserByUsername(ctx, "acme", "heidi")
	require.NoError(t, err)
}

// TestGRPC_UpdateUserChangesTheUsername proves the update message carries it too.
func TestGRPC_UpdateUserChangesTheUsername(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada One",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)

	updated, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   created.Id,
		Username: "adalovelace",
		Name:     "Ada Two",
	})
	require.NoError(t, err)
	assert.Equal(t, "adalovelace", updated.Username, "the update dropped the username")
	assert.Equal(t, "Ada Two", updated.Name)

	_, err = mem.GetUserByUsername(ctx, "acme", "adalovelace")
	require.NoError(t, err, "the renamed account must resolve by its new username")
	_, err = mem.GetUserByUsername(ctx, "acme", "ada")
	assert.Error(t, err, "the old username must stop resolving")
}

// TestGRPC_UpdateUserRefusesTheDeletingLifecycle proves this transport cannot
// assert the deletion workflow's terminal state either.
func TestGRPC_UpdateUserRefusesTheDeletingLifecycle(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "ivan@acme.test",
		Username:  "ivan",
		Name:      "Ivan",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)

	_, err = srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId:  "acme",
		UserId:    created.Id,
		Lifecycle: string(iam.LifecycleDeleting),
	})
	require.Error(t, err, "an ordinary update was allowed to claim the deleting state")

	createdID, err := strconv.ParseUint(created.Id, 10, 64)
	require.NoError(t, err)
	after, err := mem.GetUser(ctx, "acme", createdID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, after.Lifecycle,
		"the refused update changed the account state anyway")

	// And a legitimate transition still works, so the guard is specific.
	_, err = srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId:  "acme",
		UserId:    created.Id,
		Lifecycle: string(iam.LifecycleSuspended),
	})
	require.NoError(t, err)
	after, err = mem.GetUser(ctx, "acme", createdID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleSuspended, after.Lifecycle)
}
