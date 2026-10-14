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

package iam

import (
	"context"
	"errors"
	"testing"
)

// recordingCredentials captures the lifecycle the user record held at the moment
// the credential sweep ran, which is the ordering DeleteUser has to get right.
type recordingCredentials struct {
	store       UserStorage
	tenantID    string
	userID      uint64
	seenAtSweep Lifecycle
	err         error
}

func (r *recordingCredentials) DeleteCredentials(ctx context.Context, tenantID, email string) (int, error) {
	if u, err := r.store.GetUser(ctx, r.tenantID, r.userID); err == nil {
		r.seenAtSweep = u.Lifecycle
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, nil
}

func newDeletionFixture(t *testing.T) (*IdentityStore, *MemoryStorage, *User) {
	t.Helper()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem, RoleStorage: mem,
	})
	ctx := context.Background()
	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email: "doomed@acme.test", Username: "doomed",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return store, mem, user
}

// TestDeleteUser_MarksDeletingBeforeTheCredentialSweep is the race this closes.
//
// `deleting` is terminal — nothing transitions out of it — so writing it FIRST
// means that from that moment the account cannot authenticate and no concurrent
// request can make it able to again. Sweeping credentials while the account is
// still active leaves a window in which a ceremony can enroll against a record
// that is about to vanish.
func TestDeleteUser_MarksDeletingBeforeTheCredentialSweep(t *testing.T) {
	store, mem, user := newDeletionFixture(t)
	rec := &recordingCredentials{store: mem, tenantID: "acme", userID: user.ID}
	store.webauthnCredentials = rec

	if err := store.DeleteUser(context.Background(), "acme", user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if rec.seenAtSweep != LifecycleDeleting {
		t.Fatalf("lifecycle during the credential sweep = %q, want %q — the account was still live while its credentials were being removed",
			rec.seenAtSweep, LifecycleDeleting)
	}
}

// TestDeleteUser_FailedSweepLeavesTheAccountClosed is the other half: if the
// cascade aborts partway, the account must NOT still be able to authenticate.
// Marking after the sweep would leave a failed deletion fully live.
//
// The marker is best effort — a store that cannot accept an update must still
// be able to delete, or a failed create could never be rolled back — so this
// asserts the outcome when the store is healthy and only the sweep fails, which
// is the case the race actually cares about.
func TestDeleteUser_FailedSweepLeavesTheAccountClosed(t *testing.T) {
	store, mem, user := newDeletionFixture(t)
	sweepErr := errors.New("credential store unavailable")
	store.webauthnCredentials = &recordingCredentials{
		store: mem, tenantID: "acme", userID: user.ID, err: sweepErr,
	}

	if err := store.DeleteUser(context.Background(), "acme", user.ID); err == nil {
		t.Fatal("a failed credential sweep must abort the deletion")
	}

	after, err := mem.GetUser(context.Background(), "acme", user.ID)
	if err != nil {
		t.Fatalf("the user must still exist after an aborted deletion: %v", err)
	}
	if after.CanAuthenticate() {
		t.Fatal("an aborted deletion left the account able to authenticate")
	}
	if after.Lifecycle != LifecycleDeleting {
		t.Fatalf("Lifecycle = %q, want %q", after.Lifecycle, LifecycleDeleting)
	}
}

// TestMemoryStorage_EnforcesTheTransitionTable is the guard that stops the
// in-memory store being MORE PERMISSIVE than the real one.
//
// disabled -> suspended is not a legal move; suspended and locked are reachable
// only from active. While this store accepted anything, an import that created a
// record disabled and then "transitioned" it to suspended passed every test and
// was rejected by qrdb in production. The two stores have to answer the same.
func TestMemoryStorage_EnforcesTheTransitionTable(t *testing.T) {
	mem := NewMemoryStorage()
	ctx := context.Background()

	user := &User{TenantID: "acme", Email: "a@acme.test", Name: "A", Lifecycle: LifecycleDisabled}
	if err := mem.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	for _, to := range []Lifecycle{LifecycleSuspended, LifecycleLocked} {
		attempt := *user
		attempt.Lifecycle = to
		if err := mem.UpdateUser(ctx, &attempt); err == nil {
			t.Errorf("disabled -> %s was accepted; qrdb refuses it", to)
		}
	}

	// The legal move is still legal.
	attempt := *user
	attempt.Lifecycle = LifecycleActive
	if err := mem.UpdateUser(ctx, &attempt); err != nil {
		t.Fatalf("disabled -> active must be permitted: %v", err)
	}
}

// TestImportUsers_SuspendedAndLockedLandOnDisabled records what an import can
// honestly assert.
//
// Neither state is creatable, and the transition table reaches both only FROM
// active — so the only route runs through a state in which the account can
// authenticate, which for a record the import explicitly said was suspended is
// exactly the window that must not exist. Disabled is non-authenticating and
// reachable directly, so that is where they land.
func TestImportUsers_SuspendedAndLockedLandOnDisabled(t *testing.T) {
	bm, _, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{
		{Email: "susp@example.com", Username: "susp", Lifecycle: LifecycleSuspended},
		{Email: "lock@example.com", Username: "lock", Lifecycle: LifecycleLocked},
	}, BatchImportOptions{}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	for _, email := range []string{"susp@example.com", "lock@example.com"} {
		user, err := mem.GetUserByEmail(ctx, "acme", email)
		if err != nil {
			t.Fatalf("read back %s: %v", email, err)
		}
		if user.Lifecycle != LifecycleDisabled {
			t.Errorf("%s: Lifecycle = %q, want disabled", email, user.Lifecycle)
		}
		if user.CanAuthenticate() {
			t.Errorf("%s: an account imported as non-authenticating must not authenticate", email)
		}
	}
}

// markFailingStorage fails only the deletion claim, leaving every other verb
// working — the shape that distinguishes "this store cannot mark" from "this
// store is broken".
type markFailingStorage struct {
	UserStorage
	err error
}

func (m *markFailingStorage) MarkUserDeleting(context.Context, string, uint64) error { return m.err }

// TestDeleteUser_FailsClosedWhenTheClaimCannotBeWritten.
//
// The claim is the one step that guarantees the account stops authenticating
// before anything it owns is torn down. Continuing without it would sweep a live
// account's credentials and — if a later step failed — leave that account live,
// intact, and missing its passkey. So a claim that cannot be written aborts the
// deletion instead.
func TestDeleteUser_FailsClosedWhenTheClaimCannotBeWritten(t *testing.T) {
	mem := NewMemoryStorage()
	markErr := errors.New("store unavailable")
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: &markFailingStorage{UserStorage: mem, err: markErr},
		RoleStorage: mem,
	})
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email: "doomed@acme.test", Username: "doomed",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	rec := &recordingCredentials{store: mem, tenantID: "acme", userID: user.ID}
	store.webauthnCredentials = rec

	if err := store.DeleteUser(ctx, "acme", user.ID); err == nil {
		t.Fatal("a deletion whose claim could not be written must abort")
	}
	if rec.seenAtSweep != "" {
		t.Fatal("the credential sweep ran even though the account was never closed")
	}
	if _, err := mem.GetUser(ctx, "acme", user.ID); err != nil {
		t.Fatalf("the user must be untouched after an aborted deletion: %v", err)
	}
}

// rereadFailingStorage fails GetUser only AFTER the deletion claim, which is the
// window the post-claim re-read exists to cover.
type rereadFailingStorage struct {
	UserStorage
	claimed bool
	err     error
}

func (r *rereadFailingStorage) MarkUserDeleting(ctx context.Context, tenantID string, userID uint64) error {
	r.claimed = true
	return r.UserStorage.MarkUserDeleting(ctx, tenantID, userID)
}

func (r *rereadFailingStorage) GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error) {
	if r.claimed {
		return nil, r.err
	}
	return r.UserStorage.GetUser(ctx, tenantID, userID)
}

// TestDeleteUser_AbortsWhenThePostClaimRereadFails.
//
// The pre-claim snapshot can be stale: a concurrent update may commit a new
// email, binding or membership between the read and the claim. The re-read is
// what makes the sweep complete — so falling back to the stale copy when it
// fails would sweep the record as it looked BEFORE the claim and then delete it,
// orphaning exactly what the re-read was added to catch, and making the step
// decorative.
//
// Aborting is safe and resumable: the account is already terminal, so nothing
// can make it authenticate again, and a retry starts from a fresh snapshot.
func TestDeleteUser_AbortsWhenThePostClaimRereadFails(t *testing.T) {
	mem := NewMemoryStorage()
	rereadErr := errors.New("store unavailable")
	backing := &rereadFailingStorage{UserStorage: mem, err: rereadErr}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: backing, RoleStorage: mem,
	})
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email: "doomed@acme.test", Username: "doomed",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	rec := &recordingCredentials{store: mem, tenantID: "acme", userID: user.ID}
	store.webauthnCredentials = rec

	if err := store.DeleteUser(ctx, "acme", user.ID); err == nil {
		t.Fatal("a deletion whose post-claim re-read failed must abort rather than sweep a stale snapshot")
	}
	if rec.seenAtSweep != "" {
		t.Fatal("the credential sweep ran against a snapshot that could not be confirmed")
	}

	// The account is left terminal, so the retry it invites is safe.
	after, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("the user must still exist after an aborted deletion: %v", err)
	}
	if after.Lifecycle != LifecycleDeleting || after.CanAuthenticate() {
		t.Fatalf("Lifecycle = %q, want the account left closed for retry", after.Lifecycle)
	}
}

// TestUpdateUser_AuthMethodsPresenceDistinguishesClearFromSilence.
//
// UpdateUserOptions.AuthMethods is a pointer so that "replace with nothing" and
// "do not mention it" are different requests. A length check collapses them, and
// the collapse is one-directional: the clear is the one that disappears.
func TestUpdateUser_AuthMethodsPresenceDistinguishesClearFromSilence(t *testing.T) {
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem, RoleStorage: mem,
	})
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email: "restricted@acme.test", Username: "restricted", AuthMethods: []string{"piv"},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Saying nothing leaves the restriction alone.
	name := "Renamed"
	got, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Name: &name})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(got.AuthMethods) != 1 || got.AuthMethods[0] != "piv" {
		t.Fatalf("AuthMethods = %v, want the restriction untouched by an unrelated update", got.AuthMethods)
	}

	// An explicit empty set clears it.
	empty := []string{}
	got, err = store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{AuthMethods: &empty})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(got.AuthMethods) != 0 {
		t.Fatalf("AuthMethods = %v, want the restriction cleared", got.AuthMethods)
	}
}

// TestUpdateUser_AssignableLifecycleStates is the enumeration behind the guard:
// every state the public update surface can name, and whether a caller may
// assert it DIRECTLY.
//
//	pending    yes — "created, not yet activated". Reversible, owns no cascade.
//	active     yes — the ordinary enable. Reversible, owns no cascade.
//	suspended  yes — an administrative hold. Reversible, owns no cascade.
//	locked     yes — an automated lockout. Reversible, owns no cascade.
//	disabled   yes — an administrative deactivation. Reversible, owns no cascade.
//	deleting   NO  — terminal, and it OWNS a cascade. DeleteUser claims the
//	               record with it and then removes the credentials, group
//	               memberships and sessions the account holds. Asserted through
//	               an update, the claim happens and the cascade does not: the
//	               account can never authenticate again, can never be repaired
//	               (nothing transitions out of a terminal state), and everything
//	               it owns stays live.
//
// The five safe states share the property that makes them safe — each is a
// statement about the account alone, and each can be undone by naming another
// one. `deleting` is neither.
func TestUpdateUser_AssignableLifecycleStates(t *testing.T) {
	ctx := context.Background()

	// `assignable` is about NAMING the state, which is what the guard decides.
	// Whether the move is legal FROM WHERE THE RECORD SITS is a separate check
	// that stays qrdb's: active -> pending, for one, is nameable here and
	// refused by the transition table, because an activated account cannot
	// become one that was never activated.
	for _, tc := range []struct {
		state      Lifecycle
		assignable bool
	}{
		{LifecyclePending, true},
		{LifecycleActive, true},
		{LifecycleSuspended, true},
		{LifecycleLocked, true},
		{LifecycleDisabled, true},
		{LifecycleDeleting, false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			store := newTestIdentityStore()
			created, err := store.CreateUser(ctx, "t1", CreateUserOptions{
				Email:     "state@example.com",
				Username:  "stateuser",
				Name:      "State User",
				Lifecycle: LifecycleActive,
			})
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}

			state := tc.state
			updated, err := store.UpdateUser(ctx, "t1", created.ID, UpdateUserOptions{Lifecycle: &state})
			if tc.assignable {
				if err != nil {
					t.Fatalf("UpdateUser(%s): %v", tc.state, err)
				}
				if updated.Lifecycle != tc.state {
					t.Fatalf("Lifecycle = %q, want %q", updated.Lifecycle, tc.state)
				}
				return
			}

			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("UpdateUser(%s) error = %v, want a field validation error", tc.state, err)
			}

			// Refused means UNCHANGED, not "refused after writing": a record
			// left terminal would be exactly the damage the guard prevents.
			stored, gErr := store.GetUser(ctx, "t1", created.ID)
			if gErr != nil {
				t.Fatalf("GetUser: %v", gErr)
			}
			if stored.Lifecycle != LifecycleActive {
				t.Fatalf("stored Lifecycle = %q, want the account untouched", stored.Lifecycle)
			}
			if !stored.CanAuthenticate() {
				t.Fatal("the refused update left the account unable to authenticate")
			}
		})
	}
}

// TestDeleteUser_StillClaimsTheRecordItself proves reserving `deleting` costs
// the deletion workflow nothing: it claims the record through MarkUserDeleting,
// a lifecycle-only storage op that exists separately precisely because a
// terminal record refuses a full-record write. If the guard had been put where
// the workflow passes, this would fail.
func TestDeleteUser_StillClaimsTheRecordItself(t *testing.T) {
	ctx := context.Background()
	store := newTestIdentityStore()

	created, err := store.CreateUser(ctx, "t1", CreateUserOptions{
		Email:     "gone@example.com",
		Username:  "gone",
		Name:      "Gone User",
		Lifecycle: LifecycleActive,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.DeleteUser(ctx, "t1", created.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUser(ctx, "t1", created.ID); err == nil {
		t.Fatal("the user survived its own deletion")
	}
}
