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

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// go-quicbaas#290: a registration that finishes while its account is being
// deleted must not leave a credential behind.
//
// The interleavings are driven deterministically: the account is marked
// deleting either before Save runs, or from inside the inner credential write,
// which is the window between Save's first read of the account and the write.
// =============================================================================

// hookedCredentialStore runs afterSave once the inner store has persisted a
// credential, standing in for a deletion that commits mid-registration.
type hookedCredentialStore struct {
	xkmswebauthn.CredentialStore
	afterSave func()
}

func (s *hookedCredentialStore) Save(ctx context.Context, cred *xkmswebauthn.Credential) error {
	if err := s.CredentialStore.Save(ctx, cred); err != nil {
		return err
	}
	if s.afterSave != nil {
		s.afterSave()
	}
	return nil
}

// unreadableUserStore fails every lookup with err while err is set, and
// otherwise answers from the embedded store.
type unreadableUserStore struct {
	xkmswebauthn.UserStore
	err error
}

func (s *unreadableUserStore) GetByID(ctx context.Context, userID []byte) (xkmswebauthn.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.UserStore.GetByID(ctx, userID)
}

// seedUserInState stores the account for id in the given lifecycle state.
func seedUserInState(t *testing.T, users xkmswebauthn.UserStore, id uint64, state Lifecycle) {
	t.Helper()
	user := xkmswebauthn.NewDefaultUser(testUserHandle(id), "alice@example.com", "alice's yubikey")
	user.SetLifecycle(state)
	if err := users.Save(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// newDeletingFixture builds the write-through over a hooked in-memory
// credential store and a factor client that can revoke what it enrolled.
func newDeletingFixture(t *testing.T, users xkmswebauthn.UserStore) (*webAuthnFactorWriteThrough, *hookedCredentialStore, *reapableFactorClient) {
	t.Helper()
	factors := &reapableFactorClient{}
	creds := &hookedCredentialStore{CredentialStore: newTestWebAuthnCredentialStore(t)}
	wt, err := newWebAuthnFactorWriteThrough(creds, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	return wt, creds, factors
}

// assertNothingPersisted fails unless neither the credential nor a live factor
// for it remains.
func assertNothingPersisted(t *testing.T, creds xkmswebauthn.CredentialStore, factors *reapableFactorClient, credID []byte) {
	t.Helper()
	if _, err := creds.GetByCredentialID(context.Background(), credID); !errors.Is(err, xkmswebauthn.ErrCredentialNotFound) {
		t.Errorf("credential store still holds the credential (lookup err = %v), want not found", err)
	}
	if len(factors.stored) != 0 {
		t.Errorf("factor store holds %d factors, want none", len(factors.stored))
	}
}

func TestWriteThroughSave_RefusesAccountAlreadyDeleting(t *testing.T) {
	users := newTestWebAuthnUserStore(t)
	seedUserInState(t, users, 42, LifecycleDeleting)
	wt, creds, factors := newDeletingFixture(t, users)

	credID := []byte{1, 2, 3, 4}
	err := wt.Save(context.Background(), testCredential(42, credID))

	var refused *WebAuthnRegistrantDeletingError
	if !errors.As(err, &refused) {
		t.Fatalf("Save err = %v, want *WebAuthnRegistrantDeletingError", err)
	}
	if refused.UserID != 42 || refused.Compensated {
		t.Errorf("refusal = %+v, want UserID 42 and nothing to compensate", refused)
	}
	if len(factors.added) != 0 {
		t.Errorf("a refused registration must not reach the factor store, got %d enrollments", len(factors.added))
	}
	assertNothingPersisted(t, creds, factors, credID)
}

func TestWriteThroughSave_RollsBackWhenDeletionStartsMidRegistration(t *testing.T) {
	users := newTestWebAuthnUserStore(t)
	seedUserInState(t, users, 42, LifecycleActive)
	wt, creds, factors := newDeletingFixture(t, users)
	creds.afterSave = func() { seedUserInState(t, users, 42, LifecycleDeleting) }

	credID := []byte{5, 6, 7, 8}
	err := wt.Save(context.Background(), testCredential(42, credID))

	var refused *WebAuthnRegistrantDeletingError
	if !errors.As(err, &refused) {
		t.Fatalf("Save err = %v, want *WebAuthnRegistrantDeletingError", err)
	}
	if !refused.Compensated {
		t.Errorf("refusal after the write must report the rollback: %+v", refused)
	}
	assertNothingPersisted(t, creds, factors, credID)
}

func TestWriteThroughSave_RollsBackWhenAccountRemovedMidRegistration(t *testing.T) {
	users := newTestWebAuthnUserStore(t)
	seedUserInState(t, users, 42, LifecycleActive)
	wt, creds, factors := newDeletingFixture(t, users)
	creds.afterSave = func() {
		if err := users.Delete(context.Background(), testUserHandle(42)); err != nil {
			t.Errorf("delete user: %v", err)
		}
	}

	credID := []byte{9, 10, 11, 12}
	err := wt.Save(context.Background(), testCredential(42, credID))

	var refused *WebAuthnRegistrantDeletingError
	if !errors.As(err, &refused) || !refused.Compensated {
		t.Fatalf("Save err = %v, want a compensated *WebAuthnRegistrantDeletingError", err)
	}
	assertNothingPersisted(t, creds, factors, credID)
}

func TestWriteThroughSave_PendingAccountStillRegisters(t *testing.T) {
	// Invite activation enrolls the first passkey of a PENDING account; only
	// deletion is refused here.
	users := newTestWebAuthnUserStore(t)
	seedUserInState(t, users, 42, LifecyclePending)
	wt, creds, factors := newDeletingFixture(t, users)

	credID := []byte{13, 14, 15, 16}
	if err := wt.Save(context.Background(), testCredential(42, credID)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := creds.GetByCredentialID(context.Background(), credID); err != nil {
		t.Errorf("credential not persisted: %v", err)
	}
	if len(factors.stored) != 1 {
		t.Errorf("factor store holds %d factors, want 1", len(factors.stored))
	}
}

func TestWriteThroughSave_UnreadableAccountFailsClosed(t *testing.T) {
	readErr := errors.New("user store unavailable")
	users := &unreadableUserStore{UserStore: newTestWebAuthnUserStore(t), err: readErr}
	wt, creds, factors := newDeletingFixture(t, users)

	credID := []byte{17, 18, 19, 20}
	err := wt.Save(context.Background(), testCredential(42, credID))

	var lookup *WebAuthnRegistrantLookupError
	if !errors.As(err, &lookup) || !errors.Is(err, readErr) {
		t.Fatalf("Save err = %v, want *WebAuthnRegistrantLookupError wrapping the read failure", err)
	}
	if lookup.Compensated {
		t.Errorf("nothing was written, so nothing can have been compensated: %+v", lookup)
	}
	assertNothingPersisted(t, creds, factors, credID)
}

func TestWriteThroughSave_UnreadableAccountAfterWriteRollsBackNewCredential(t *testing.T) {
	readErr := errors.New("user store unavailable")
	inner := newTestWebAuthnUserStore(t)
	seedUserInState(t, inner, 42, LifecycleActive)
	users := &unreadableUserStore{UserStore: inner}
	wt, creds, factors := newDeletingFixture(t, users)
	creds.afterSave = func() { users.err = readErr }

	credID := []byte{21, 22, 23, 24}
	err := wt.Save(context.Background(), testCredential(42, credID))

	var lookup *WebAuthnRegistrantLookupError
	if !errors.As(err, &lookup) || !lookup.Compensated {
		t.Fatalf("Save err = %v, want a compensated *WebAuthnRegistrantLookupError", err)
	}
	assertNothingPersisted(t, creds, factors, credID)
}

func TestWebAuthnRegistrantErrors_Messages(t *testing.T) {
	deleting := (&WebAuthnRegistrantDeletingError{CredentialID: "abc", UserID: 7, Compensated: true}).Error()
	if deleting != "iam.webauthn.registration: refused, the account is being deleted (user 7) (credential abc); the credential was rolled back" {
		t.Errorf("deleting message = %q", deleting)
	}
	lookup := &WebAuthnRegistrantLookupError{CredentialID: "abc", Cause: errors.New("boom")}
	if got := lookup.Error(); got != "iam.webauthn.registration: could not read the registrant's account, so the registration was refused (credential abc): boom" {
		t.Errorf("lookup message = %q", got)
	}
	var nilLookup *WebAuthnRegistrantLookupError
	if nilLookup.Unwrap() != nil || nilLookup.Error() == "" {
		t.Error("a nil *WebAuthnRegistrantLookupError must still describe itself and unwrap to nil")
	}
}
