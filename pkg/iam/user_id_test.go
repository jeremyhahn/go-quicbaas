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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestLoginName_IsTheUsername pins the rule both UserStorage implementations
// resolve GetUserByUsername through. Sharing it is what stops the in-memory
// store answering a question production answers differently.
//
// It is the USERNAME, and neither of the two fields it sits between. Not the
// email: a username is what an administrator types to assign a role, and
// collapsing it onto the email would make every such lookup miss — the
// email/username collision is closed by resolveSubject's disjoint subject
// shapes, not by making the two indexes hold one string. And not the display
// name, which is what it used to return: a display name carries no uniqueness
// constraint and is not the handle the caller chose, so the lookup missed for
// anyone whose display name was not also their username and two people sharing
// a display name collided in qrdb on a constraint neither had asked to share.
func TestLoginName_IsTheUsername(t *testing.T) {
	u := &User{Email: "alice@acme.test", Username: "alice", Name: "Alice Display"}
	if got := u.LoginName(); got != "alice" {
		t.Errorf("LoginName = %q, want the username, not the email or the display name", got)
	}
	if got := (&User{Username: "admin"}).LoginName(); got != "admin" {
		t.Errorf("LoginName = %q, want the username", got)
	}
	// A display name alone names nobody in the username index: the handle is
	// its own field precisely so this cannot resolve.
	if got := (&User{Name: "Alice Display"}).LoginName(); got != "" {
		t.Errorf("LoginName = %q, want empty — a display name is not a handle", got)
	}
	var nilUser *User
	if got := nilUser.LoginName(); got != "" {
		t.Errorf("LoginName = %q, want empty for a nil user", got)
	}
}

// TestCanAuthenticate_OnlyActivePasses pins the gate, including the case that
// used to be a fail-open: an ABSENT lifecycle denies.
//
// It briefly did the opposite, mirroring qrdb's fallback for records written
// before the field existed. That justification was backward compatibility, which
// the pre-release policy removes — and a fail-open on an authentication gate
// needs a much better reason than a population we do not carry.
func TestCanAuthenticate_OnlyActivePasses(t *testing.T) {
	if (&User{}).CanAuthenticate() {
		t.Error("a record with no lifecycle must not authenticate")
	}
	if !(&User{Lifecycle: LifecycleActive}).CanAuthenticate() {
		t.Error("active must authenticate")
	}
	for _, l := range []Lifecycle{LifecyclePending, LifecycleSuspended, LifecycleLocked, LifecycleDisabled, LifecycleDeleting} {
		if (&User{Lifecycle: l}).CanAuthenticate() {
			t.Errorf("%q must not authenticate — active is the only state that does", l)
		}
	}
	// A state written by a newer node fails closed.
	if (&User{Lifecycle: Lifecycle("something-newer")}).CanAuthenticate() {
		t.Error("an unrecognized state must fail closed")
	}
	var nilUser *User
	if nilUser.CanAuthenticate() {
		t.Error("a nil user must not authenticate")
	}
}

// TestMemoryStorage_SeededIDAdvancesTheAllocator.
//
// A restored record keeps its id, so the allocator has to move past it. Without
// that, seeding id "1" and then creating a user with no id mints "1" again and
// the create fails with ErrAlreadyExists — a collision the caller cannot see the
// cause of, and one the real qrdb allocator would never produce.
func TestMemoryStorage_SeededIDAdvancesTheAllocator(t *testing.T) {
	mem := NewMemoryStorage()
	ctx := t.Context()

	// Restore a record that already carries a low id.
	seeded := &User{ID: 1, TenantID: "acme", Email: "seeded@acme.test", Name: "Seeded"}
	if err := mem.CreateUser(ctx, seeded); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A subsequent mint must not collide with it.
	fresh := &User{TenantID: "acme", Email: "fresh@acme.test", Name: "Fresh"}
	if err := mem.CreateUser(ctx, fresh); err != nil {
		t.Fatalf("a create after a seeded id must not collide: %v", err)
	}
	if fresh.ID == seeded.ID {
		t.Fatalf("minted %d, the id the seeded record already holds", fresh.ID)
	}

	// And a much higher seed moves it further still.
	high := &User{ID: 5000, TenantID: "acme", Email: "high@acme.test", Name: "High"}
	if err := mem.CreateUser(ctx, high); err != nil {
		t.Fatalf("seed high: %v", err)
	}
	next := &User{TenantID: "acme", Email: "next@acme.test", Name: "Next"}
	if err := mem.CreateUser(ctx, next); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if next.ID <= 5000 {
		t.Fatalf("minted %d, want an id past the highest seeded one", next.ID)
	}
}

// TestMemoryStorage_UserCRUDByUint64 drives a record through its whole life by
// an id above 2^53, the range a float64-based decoder would corrupt.
func TestMemoryStorage_UserCRUDByUint64(t *testing.T) {
	mem := NewMemoryStorage()
	ctx := t.Context()
	const id uint64 = 1<<53 + 1

	if err := mem.CreateUser(ctx, &User{ID: id, TenantID: "acme", Email: "big@acme.test"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := mem.GetUser(ctx, "acme", id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.ID != id {
		t.Fatalf("GetUser returned id %d, want %d", got.ID, id)
	}

	got.Name = "Renamed"
	if err := mem.UpdateUser(ctx, got); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, err := mem.GetUser(ctx, "acme", id)
	if err != nil {
		t.Fatalf("GetUser after update: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("Name = %q, want the update", updated.Name)
	}

	if err := mem.MarkUserDeleting(ctx, "acme", id); err != nil {
		t.Fatalf("MarkUserDeleting: %v", err)
	}
	if err := mem.DeleteUser(ctx, "acme", id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := mem.GetUser(ctx, "acme", id); !IsUserNotFound(err) {
		t.Fatalf("GetUser after delete = %v, want ErrUserNotFound", err)
	}
}

// TestMemoryStorage_MissingUserIsNotFound pins the typed not-found answer for
// an id the store does not hold, including one held only by another tenant.
func TestMemoryStorage_MissingUserIsNotFound(t *testing.T) {
	mem := NewMemoryStorage()
	ctx := t.Context()
	other := &User{TenantID: "other", Email: "o@other.test"}
	if err := mem.CreateUser(ctx, other); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	for _, id := range []uint64{other.ID, 42} {
		if _, err := mem.GetUser(ctx, "acme", id); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("GetUser(%d) = %v, want ErrUserNotFound", id, err)
		}
		if err := mem.MarkUserDeleting(ctx, "acme", id); !dberrors.IsNotFound(err) {
			t.Errorf("MarkUserDeleting(%d) = %v, want not-found", id, err)
		}
		if err := mem.DeleteUser(ctx, "acme", id); !dberrors.IsNotFound(err) {
			t.Errorf("DeleteUser(%d) = %v, want not-found", id, err)
		}
		if err := mem.UpdateUser(ctx, &User{ID: id, TenantID: "acme"}); !dberrors.IsNotFound(err) {
			t.Errorf("UpdateUser(%d) = %v, want not-found", id, err)
		}
	}
}

// TestMemoryStorage_RefusesTheZeroID proves the "not created yet" id is refused
// rather than looked up.
func TestMemoryStorage_RefusesTheZeroID(t *testing.T) {
	mem := NewMemoryStorage()
	ctx := t.Context()

	if _, err := mem.GetUser(ctx, "acme", 0); !errors.Is(err, ErrInvalidUserID) {
		t.Errorf("GetUser(0) = %v, want ErrInvalidUserID", err)
	}
	if err := mem.MarkUserDeleting(ctx, "acme", 0); !errors.Is(err, ErrInvalidUserID) {
		t.Errorf("MarkUserDeleting(0) = %v, want ErrInvalidUserID", err)
	}
	if err := mem.DeleteUser(ctx, "acme", 0); !errors.Is(err, ErrInvalidUserID) {
		t.Errorf("DeleteUser(0) = %v, want ErrInvalidUserID", err)
	}
}

// TestUser_JSONCarriesTheIDAsADecimalString pins the wire form: an id above
// 2^53 survives a round trip, and is encoded as a string so a JavaScript
// client does not round it.
func TestUser_JSONCarriesTheIDAsADecimalString(t *testing.T) {
	const id uint64 = 1<<53 + 1
	data, err := json.Marshal(&User{ID: id, TenantID: "acme"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"id":"9007199254740993"`) {
		t.Fatalf("encoded user %s, want the id as a decimal string", data)
	}

	var back User
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.ID != id {
		t.Errorf("round-tripped id = %d, want %d", back.ID, id)
	}
}

func TestUser_JSONRefusesANonDecimalID(t *testing.T) {
	for _, body := range []string{
		`{"id":"user-1"}`,
		`{"id":"18446744073709551616"}`,
		`{"id":9007199254740993}`,
	} {
		var u User
		if err := json.Unmarshal([]byte(body), &u); err == nil {
			t.Errorf("Unmarshal(%s) accepted id %d, want an error", body, u.ID)
		}
	}
}
