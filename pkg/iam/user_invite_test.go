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

package iam_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

// newInviteTestStore returns an identity store over memory storage, with
// tenant "acme" created, wrapping its user storage with wrap if non-nil.
func newInviteTestStore(t *testing.T, wrap func(iam.UserStorage) iam.UserStorage) (*iam.IdentityStore, *iam.MemoryStorage) {
	t.Helper()
	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err := tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	var users iam.UserStorage = mem
	if wrap != nil {
		users = wrap(mem)
	}
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: users,
		RoleStorage: mem,
		Tenants:     tenants,
	}), mem
}

// failingCreateInviteStore refuses to issue invites.
type failingCreateInviteStore struct {
	iam.InviteStore
	err error
}

func (f *failingCreateInviteStore) Create(context.Context, string, string, time.Duration) (string, *iam.InviteRecord, error) {
	return "", nil, f.err
}

// recordingInviteStore remembers the last token it minted.
type recordingInviteStore struct {
	iam.InviteStore
	last string
}

func (r *recordingInviteStore) Create(ctx context.Context, tenantID, userID string, ttl time.Duration) (string, *iam.InviteRecord, error) {
	token, rec, err := r.InviteStore.Create(ctx, tenantID, userID, ttl)
	r.last = token
	return token, rec, err
}

// failingUpdateStorage persists creates but fails every update.
type failingUpdateStorage struct {
	iam.UserStorage
	err error
}

func (f *failingUpdateStorage) UpdateUser(context.Context, *iam.User) error { return f.err }

// undeletableStorage fails updates and refuses to delete, so a rollback
// cannot complete.
type undeletableStorage struct {
	failingUpdateStorage
	markErr error
}

func (u *undeletableStorage) MarkUserDeleting(context.Context, string, string) error {
	return u.markErr
}

func TestCreateInvitedUser_PendingUserGetsRedeemableInvite(t *testing.T) {
	ctx := context.Background()
	store, _ := newInviteTestStore(t, nil)
	invites := iamtest.NewMemoryInviteStore()

	user, token, record, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "bob@acme.com", Username: "bob"}, invites, 2*time.Hour)
	require.NoError(t, err)

	assert.Equal(t, iam.LifecyclePending, user.Lifecycle)
	require.NotEmpty(t, token)
	require.NotNil(t, record)
	assert.Equal(t, user.ID, record.UserID)
	assert.WithinDuration(t, time.Now().Add(2*time.Hour), record.ExpiresAt, time.Minute)

	redeemed, err := invites.Redeem(ctx, "acme", token)
	require.NoError(t, err)
	assert.Equal(t, user.ID, redeemed.UserID)
}

func TestCreateInvitedUser_NonPendingUserGetsNoInvite(t *testing.T) {
	ctx := context.Background()
	store, _ := newInviteTestStore(t, nil)

	// No invite store at all: a user that is not pending must not need one.
	user, token, record, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "ann@acme.com", Username: "ann", Lifecycle: iam.LifecycleActive}, nil, 0)
	require.NoError(t, err)

	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)
	assert.Empty(t, token)
	assert.Nil(t, record)
}

func TestCreateInvitedUser_NoInviteStoreRefusesBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store, mem := newInviteTestStore(t, nil)

	_, _, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "cy@acme.com", Username: "cy"}, nil, 0)
	require.ErrorIs(t, err, iam.ErrInviteStoreUnavailable)

	_, getErr := mem.GetUserByEmail(ctx, "acme", "cy@acme.com")
	assert.Error(t, getErr, "a pending user was written with no invite to activate it")
}

func TestCreateInvitedUser_InviteFailureRollsBackUser(t *testing.T) {
	ctx := context.Background()
	store, mem := newInviteTestStore(t, nil)
	cause := errors.New("invite store unreachable")

	_, token, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "dee@acme.com", Username: "dee"}, &failingCreateInviteStore{err: cause}, 0)
	require.ErrorIs(t, err, cause)
	assert.Empty(t, token)

	_, getErr := mem.GetUserByEmail(ctx, "acme", "dee@acme.com")
	assert.Error(t, getErr, "the user survived a create whose invite could not be issued")
}

func TestCreateInvitedUser_AttachFailureSpendsTokenAndRollsBackUser(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("user store unavailable")
	store, mem := newInviteTestStore(t, func(u iam.UserStorage) iam.UserStorage {
		return &failingUpdateStorage{UserStorage: u, err: cause}
	})
	invites := &recordingInviteStore{InviteStore: iamtest.NewMemoryInviteStore()}

	_, _, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "eve@acme.com", Username: "eve"}, invites, 0)
	require.ErrorIs(t, err, cause)

	_, getErr := mem.GetUserByEmail(ctx, "acme", "eve@acme.com")
	assert.Error(t, getErr, "the user survived a create whose invite could not be attached")

	require.NotEmpty(t, invites.last, "the fixture never minted an invite")
	_, redeemErr := invites.Redeem(ctx, "acme", invites.last)
	assert.Error(t, redeemErr, "the minted invite outlived the rolled-back user")
}

func TestCreateInvitedUser_RollbackSurvivesCancellation(t *testing.T) {
	store, mem := newInviteTestStore(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The invite store cancels the caller's context as it fails, the case where
	// a rollback bound to that context would be cancelled with it.
	invites := &cancellingInviteStore{cancel: cancel, err: errors.New("deadline")}
	_, _, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "fay@acme.com", Username: "fay"}, invites, 0)
	require.Error(t, err)
	require.Error(t, ctx.Err())

	_, getErr := mem.GetUserByEmail(context.Background(), "acme", "fay@acme.com")
	assert.Error(t, getErr, "the rollback was cancelled along with the request")
}

// cancellingInviteStore cancels the caller's context, then fails.
type cancellingInviteStore struct {
	iam.InviteStore
	cancel context.CancelFunc
	err    error
}

func (c *cancellingInviteStore) Create(context.Context, string, string, time.Duration) (string, *iam.InviteRecord, error) {
	c.cancel()
	return "", nil, c.err
}

func TestCreateInvitedUser_FailedRollbackIsTyped(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("user store unavailable")
	store, _ := newInviteTestStore(t, func(u iam.UserStorage) iam.UserStorage {
		return &undeletableStorage{
			failingUpdateStorage: failingUpdateStorage{UserStorage: u, err: cause},
			markErr:              errors.New("mark refused"),
		}
	})

	_, _, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "gus@acme.com", Username: "gus"}, iamtest.NewMemoryInviteStore(), 0)

	var rollbackErr *iam.UserRollbackError
	require.ErrorAs(t, err, &rollbackErr)
	assert.NotEmpty(t, rollbackErr.UserID)
	assert.ErrorIs(t, err, cause, "the create's own failure must stay visible")
}

// userReaderFunc adapts a function to UserReader.
type userReaderFunc func(ctx context.Context, tenantID, userID string) (*iam.User, error)

func (f userReaderFunc) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	return f(ctx, tenantID, userID)
}

func TestRereadUserAfterFailedWrite_DetachesFromCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := userReaderFunc(func(ctx context.Context, tenantID, userID string) (*iam.User, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("re-read context has no deadline, want it bounded")
		}
		return &iam.User{ID: userID, TenantID: tenantID, Lifecycle: iam.LifecycleActive}, nil
	})

	user, err := iam.RereadUserAfterFailedWrite(ctx, reader, "t1", "u1")
	if err != nil {
		t.Fatalf("RereadUserAfterFailedWrite = %v, want the read to outlive the canceled request", err)
	}
	if user.ID != "u1" || user.Lifecycle != iam.LifecycleActive {
		t.Fatalf("user = %+v, want the persisted user", user)
	}
}

func TestRereadUserAfterFailedWrite_ReturnsReadError(t *testing.T) {
	readErr := errors.New("read failed")
	reader := userReaderFunc(func(context.Context, string, string) (*iam.User, error) {
		return nil, readErr
	})

	if _, err := iam.RereadUserAfterFailedWrite(context.Background(), reader, "t1", "u1"); !errors.Is(err, readErr) {
		t.Fatalf("RereadUserAfterFailedWrite = %v, want the read error", err)
	}
}

func TestCreateInvitedUser_InvalidInputWritesNothing(t *testing.T) {
	ctx := context.Background()
	store, _ := newInviteTestStore(t, nil)
	invites := &recordingInviteStore{InviteStore: iamtest.NewMemoryInviteStore()}

	_, _, _, err := store.CreateInvitedUser(ctx, "acme",
		iam.CreateUserOptions{Email: "not-an-email"}, invites, 0)
	require.Error(t, err)
	assert.Empty(t, invites.last, "an invite was minted for a rejected create")
}

func TestCreateUserOptions_EffectiveLifecycle(t *testing.T) {
	disabled, enabled := false, true
	tests := []struct {
		name string
		opts iam.CreateUserOptions
		want iam.Lifecycle
	}{
		{"default is pending", iam.CreateUserOptions{}, iam.LifecyclePending},
		{"enabled stays pending", iam.CreateUserOptions{Enabled: &enabled}, iam.LifecyclePending},
		{"enabled false disables", iam.CreateUserOptions{Enabled: &disabled}, iam.LifecycleDisabled},
		{"explicit lifecycle wins", iam.CreateUserOptions{Enabled: &disabled, Lifecycle: iam.LifecyclePending}, iam.LifecyclePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.opts.EffectiveLifecycle())
		})
	}
}

func TestClampInviteTTL(t *testing.T) {
	assert.Equal(t, iam.DefaultInviteTTL, iam.ClampInviteTTL(0))
	assert.Equal(t, iam.DefaultInviteTTL, iam.ClampInviteTTL(-time.Hour))
	assert.Equal(t, 3*time.Hour, iam.ClampInviteTTL(3*time.Hour))
	assert.Equal(t, iam.MaxInviteTTL, iam.ClampInviteTTL(iam.MaxInviteTTL+time.Hour))
}

func TestInviteURL(t *testing.T) {
	assert.Equal(t, "https://id.example.com/ui/#/invite/acme/tok",
		iam.InviteURL("https://id.example.com/", "acme", "tok"))
	// Path separators in either segment must not change the route.
	assert.Equal(t, "https://id.example.com/ui/#/invite/a%2Fb/t%2Fk",
		iam.InviteURL("https://id.example.com", "a/b", "t/k"))
}
