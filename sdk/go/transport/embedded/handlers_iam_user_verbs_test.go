// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package embedded

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// newUserClient returns the real SDK IAM user client over an embedded
// transport backed by the given service, plus the store behind it.
func newUserClient(t *testing.T) (iamsdk.UserClient, *iam.IdentityStore) {
	t.Helper()
	svc, store := newIAMTestService()
	tr, err := New(svc)
	require.NoError(t, err)
	return iamsdk.NewUserClient(tr), store
}

// newUnconfiguredUserClient returns the SDK user client over an embedded
// transport whose service exposes no identity store.
func newUnconfiguredUserClient(t *testing.T) iamsdk.UserClient {
	t.Helper()
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	return iamsdk.NewUserClient(tr)
}

// createTestUser stores a user in the given lifecycle and returns its ID in
// the string form the SDK client sends.
func createTestUser(t *testing.T, store *iam.IdentityStore, state iam.Lifecycle) string {
	t.Helper()
	u, err := store.CreateUser(context.Background(), "t-1", iam.CreateUserOptions{
		Email:     "ada@acme.test",
		Username:  "ada",
		Name:      "Ada",
		Lifecycle: state,
	})
	require.NoError(t, err)
	return strconv.FormatUint(u.ID, 10)
}

func lifecycleOf(t *testing.T, store *iam.IdentityStore, userID string) iam.Lifecycle {
	t.Helper()
	id, err := strconv.ParseUint(userID, 10, 64)
	require.NoError(t, err)
	u, err := store.GetUser(context.Background(), "t-1", id)
	require.NoError(t, err)
	return u.Lifecycle
}

func TestEmbeddedIAMUser_VerbsRegistered(t *testing.T) {
	for _, method := range []string{"GetByEmail", "FindByUsername", "AssignRole", "RevokeRole", "Approve", "Reject"} {
		_, ok := iamUnaryHandlers["quicbaas.IAM.User/"+method]
		assert.True(t, ok, "quicbaas.IAM.User/%s must be routed", method)
	}
}

func TestEmbeddedIAMUser_GetByEmail(t *testing.T) {
	client, store := newUserClient(t)
	userID := createTestUser(t, store, iam.LifecycleActive)

	got, err := client.GetByEmail(context.Background(), "t-1", "ada@acme.test")
	require.NoError(t, err)
	assert.Equal(t, userID, got.ID)
	assert.Equal(t, "ada@acme.test", got.Email)

	_, err = client.GetByEmail(context.Background(), "t-1", "nobody@acme.test")
	assert.Error(t, err, "an unknown email must not resolve")
}

func TestEmbeddedIAMUser_FindByUsername(t *testing.T) {
	client, store := newUserClient(t)
	userID := createTestUser(t, store, iam.LifecycleActive)

	got, err := client.FindByUsername(context.Background(), "t-1", "ada")
	require.NoError(t, err)
	assert.Equal(t, userID, got.ID)
	assert.Equal(t, "ada", got.Username)

	_, err = client.FindByUsername(context.Background(), "t-1", "nobody")
	assert.Error(t, err, "an unknown username must not resolve")
}

func TestEmbeddedIAMUser_AssignAndRevokeRole(t *testing.T) {
	client, store := newUserClient(t)
	ctx := context.Background()
	userID := createTestUser(t, store, iam.LifecycleActive)
	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: "auditor"})
	require.NoError(t, err)
	id, err := strconv.ParseUint(userID, 10, 64)
	require.NoError(t, err)

	require.NoError(t, client.AssignRole(ctx, "t-1", userID, role.ID))
	roles, err := store.GetUserRoles(ctx, "t-1", id)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, role.ID, roles[0].ID)

	require.NoError(t, client.RevokeRole(ctx, "t-1", userID, role.ID))
	roles, err = store.GetUserRoles(ctx, "t-1", id)
	require.NoError(t, err)
	assert.Empty(t, roles)
}

func TestEmbeddedIAMUser_AssignRoleErrors(t *testing.T) {
	client, store := newUserClient(t)
	role, err := store.CreateRole(context.Background(), "t-1", iam.CreateRoleOptions{Name: "auditor"})
	require.NoError(t, err)

	assert.Error(t, client.AssignRole(context.Background(), "t-1", "999", role.ID),
		"a missing user must not take a role")
	assert.Error(t, client.RevokeRole(context.Background(), "t-1", "not-a-number", role.ID),
		"a malformed user ID must be refused")
}

func TestEmbeddedIAMUser_ApproveAndReject(t *testing.T) {
	client, store := newUserClient(t)
	ctx := context.Background()

	userID := createTestUser(t, store, iam.LifecyclePending)
	require.NoError(t, client.Approve(ctx, "t-1", userID))
	assert.Equal(t, iam.LifecycleActive, lifecycleOf(t, store, userID))

	other, err := store.CreateUser(ctx, "t-1", iam.CreateUserOptions{
		Email:     "bob@acme.test",
		Username:  "bob",
		Name:      "Bob",
		Lifecycle: iam.LifecyclePending,
	})
	require.NoError(t, err)
	otherID := strconv.FormatUint(other.ID, 10)
	require.NoError(t, client.Reject(ctx, "t-1", otherID, "unknown applicant"))
	assert.Equal(t, iam.LifecycleDisabled, lifecycleOf(t, store, otherID))
}

func TestEmbeddedIAMUser_ApproveRejectMissingUser(t *testing.T) {
	client, _ := newUserClient(t)

	assert.Error(t, client.Approve(context.Background(), "t-1", "999"))
	assert.Error(t, client.Reject(context.Background(), "t-1", "999", ""))
}

func TestEmbeddedIAMUser_NotConfigured(t *testing.T) {
	client := newUnconfiguredUserClient(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"GetByEmail": func() error {
			_, err := client.GetByEmail(ctx, "t-1", "ada@acme.test")
			return err
		},
		"FindByUsername": func() error {
			_, err := client.FindByUsername(ctx, "t-1", "ada")
			return err
		},
		"AssignRole": func() error { return client.AssignRole(ctx, "t-1", "1", "r") },
		"RevokeRole": func() error { return client.RevokeRole(ctx, "t-1", "1", "r") },
		"Approve":    func() error { return client.Approve(ctx, "t-1", "1") },
		"Reject":     func() error { return client.Reject(ctx, "t-1", "1", "") },
	}
	for method, call := range calls {
		var notConfigured *ServiceNotConfiguredError
		assert.True(t, errors.As(call(), &notConfigured), "%s must report the identity store as not configured", method)
	}
}

func TestEmbeddedIAMUser_WrongRequestType(t *testing.T) {
	svc, _ := newIAMTestService()
	handlers := map[string]func(context.Context, service.QuicBaaS, interface{}) (interface{}, error){
		"GetByEmail":     handleIAMUserGetByEmail,
		"FindByUsername": handleIAMUserFindByUsername,
		"AssignRole":     handleIAMUserAssignRole,
		"RevokeRole":     handleIAMUserRevokeRole,
		"Approve":        handleIAMUserApprove,
		"Reject":         handleIAMUserReject,
	}
	for method, handle := range handlers {
		_, err := handle(context.Background(), svc, "not a request")
		var invalid *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalid), "%s must refuse a request without its fields, got %v", method, err)
	}
}
