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
	"errors"
	"testing"
)

func TestParseUserID_RoundTripsTheCanonicalRendering(t *testing.T) {
	for _, id := range []uint64{1, 42, 1001, 1 << 62} {
		external := FormatUserID(id)
		got, err := ParseUserID(external)
		if err != nil {
			t.Fatalf("ParseUserID(%q): %v", external, err)
		}
		if got != id {
			t.Fatalf("round trip: got %d, want %d", got, id)
		}
	}
	if got := FormatUserID(0); got != "" {
		t.Fatalf("FormatUserID(0) = %q, want empty — zero is the store's invalid sentinel", got)
	}
}

// TestParseUserID_RefusesNonCanonicalSpellings is the finding this guards: a
// leading-zero form parses to the same integer, so accepting it would give one
// user two spellings. Callers verify a user exists by id and then persist the
// string they were handed, so two spellings become two keys for one account.
func TestParseUserID_RefusesNonCanonicalSpellings(t *testing.T) {
	for _, in := range []string{
		"",
		"0",
		"00042",
		"+42",
		"-1",
		"42 ",
		" 42",
		"0x2a",
		"not-a-number",
		"9f1c2c4e-0000-4000-8000-000000000000", // the UUID shape the idmap correlated
		"18446744073709551616",                 // one past uint64
	} {
		if _, err := ParseUserID(in); !errors.Is(err, ErrInvalidUserID) {
			t.Errorf("ParseUserID(%q) err = %v, want ErrInvalidUserID", in, err)
		}
	}
}

func TestUserQRDBID(t *testing.T) {
	u := &User{ID: "1001"}
	got, err := u.QRDBID()
	if err != nil {
		t.Fatalf("QRDBID: %v", err)
	}
	if got != 1001 {
		t.Fatalf("QRDBID = %d, want 1001", got)
	}

	var nilUser *User
	if _, err := nilUser.QRDBID(); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("err = %v, want ErrInvalidUserID for a nil user", err)
	}
	if _, err := (&User{ID: "nope"}).QRDBID(); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("err = %v, want ErrInvalidUserID for a malformed id", err)
	}
}

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
	seeded := &User{ID: "1", TenantID: "acme", Email: "seeded@acme.test", Name: "Seeded"}
	if err := mem.CreateUser(ctx, seeded); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A subsequent mint must not collide with it.
	fresh := &User{TenantID: "acme", Email: "fresh@acme.test", Name: "Fresh"}
	if err := mem.CreateUser(ctx, fresh); err != nil {
		t.Fatalf("a create after a seeded id must not collide: %v", err)
	}
	if fresh.ID == seeded.ID {
		t.Fatalf("minted %q, the id the seeded record already holds", fresh.ID)
	}

	// And a much higher seed moves it further still.
	high := &User{ID: "5000", TenantID: "acme", Email: "high@acme.test", Name: "High"}
	if err := mem.CreateUser(ctx, high); err != nil {
		t.Fatalf("seed high: %v", err)
	}
	next := &User{TenantID: "acme", Email: "next@acme.test", Name: "Next"}
	if err := mem.CreateUser(ctx, next); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := ParseUserID(next.ID)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", next.ID, err)
	}
	if got <= 5000 {
		t.Fatalf("minted %d, want an id past the highest seeded one", got)
	}
}
