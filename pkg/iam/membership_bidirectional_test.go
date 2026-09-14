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
	"testing"
)

// membershipFixture builds an identity store over the real in-memory storage
// with a user and two groups. Both sides of a membership live in that store, so
// a double that maintained only one would decide these tests.
func membershipFixture(t *testing.T) (*IdentityStore, *User, *Group, *Group) {
	t.Helper()
	ctx := context.Background()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem, GroupStorage: mem, RoleStorage: mem,
	})

	engineering, err := store.CreateGroup(ctx, "acme", CreateGroupOptions{Name: "engineering"})
	if err != nil {
		t.Fatalf("CreateGroup engineering: %v", err)
	}
	support, err := store.CreateGroup(ctx, "acme", CreateGroupOptions{Name: "support"})
	if err != nil {
		t.Fatalf("CreateGroup support: %v", err)
	}
	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:     "tess@acme.test",
		Username:  "tess",
		Name:      "Tess",
		Lifecycle: LifecycleActive,
		Groups:    []string{engineering.ID},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return store, user, engineering, support
}

// assertMembershipAgrees fails unless both halves of the relationship say the
// same thing: the user lists the group and the group lists the user, or neither
// does. Checking one half is how the defect this covers went unnoticed.
func assertMembershipAgrees(t *testing.T, store *IdentityStore, userID, groupID string, want bool) {
	t.Helper()
	ctx := context.Background()

	user, err := store.GetUser(ctx, "acme", userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	group, err := store.GetGroup(ctx, "acme", groupID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}

	userSide := contains(user.Groups, groupID)
	groupSide := contains(group.Members, userID)
	if userSide != groupSide {
		t.Fatalf("the two halves disagree for user %q / group %q: user.Groups=%v group.Members=%v",
			userID, groupID, user.Groups, group.Members)
	}
	if userSide != want {
		t.Fatalf("membership of %q in %q = %v, want %v", userID, groupID, userSide, want)
	}
}

// TestUpdateUser_GroupsChangeBothSides is the regression for a generic update
// that wrote one half of a bidirectional relationship.
//
// UpdateUser assigned user.Groups straight from the request and never touched
// the groups' Members, so an update that moved an account between groups
// reported success while GetGroup still listed it under the old one and never
// under the new one. Membership is an authorization input, so the half that
// vanished is a grant the system believes it made.
func TestUpdateUser_GroupsChangeBothSides(t *testing.T) {
	store, user, engineering, support := membershipFixture(t)
	ctx := context.Background()

	assertMembershipAgrees(t, store, user.ID, engineering.ID, true)
	assertMembershipAgrees(t, store, user.ID, support.ID, false)

	groups := []string{support.ID}
	if _, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Groups: &groups}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	assertMembershipAgrees(t, store, user.ID, support.ID, true)
	assertMembershipAgrees(t, store, user.ID, engineering.ID, false)
}

// TestUpdateUser_EmptyGroupsRemovesEveryMembership pins the meaning of the
// explicit empty list, which is the destructive end of the same field.
func TestUpdateUser_EmptyGroupsRemovesEveryMembership(t *testing.T) {
	store, user, engineering, _ := membershipFixture(t)
	ctx := context.Background()

	groups := []string{}
	if _, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Groups: &groups}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	assertMembershipAgrees(t, store, user.ID, engineering.ID, false)
}

// TestUpdateUser_NilGroupsLeavesMembershipAlone pins the other end: a request
// that does not mention groups must not touch them. Without the distinction an
// update of an unrelated field would silently empty the account's memberships.
func TestUpdateUser_NilGroupsLeavesMembershipAlone(t *testing.T) {
	store, user, engineering, _ := membershipFixture(t)
	ctx := context.Background()

	name := "Tess Renamed"
	if _, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Name: &name}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	assertMembershipAgrees(t, store, user.ID, engineering.ID, true)
}

// TestUpdateUser_RefusesUnknownRoleAndGroupReferences covers the validation the
// generic path skipped. AssignRole checks the role exists; writing the slice
// through the update did not, so an account could end up holding a role id that
// names nothing — a grant that evaluates to nothing now and starts granting if
// something later claims that id.
func TestUpdateUser_RefusesUnknownRoleAndGroupReferences(t *testing.T) {
	store, user, engineering, _ := membershipFixture(t)
	ctx := context.Background()

	roles := []string{"role-that-does-not-exist"}
	if _, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Roles: &roles}); err == nil {
		t.Fatal("an update naming an unknown role must be refused")
	}

	groups := []string{"group-that-does-not-exist"}
	if _, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{Groups: &groups}); err == nil {
		t.Fatal("an update naming an unknown group must be refused")
	}

	// Neither refusal may have moved the account.
	assertMembershipAgrees(t, store, user.ID, engineering.ID, true)
	after, err := store.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(after.Roles) != 0 {
		t.Fatalf("Roles = %v, want unchanged after a refused update", after.Roles)
	}
}

// TestUpdateGroup_MembersChangeBothSides is the same defect driven from the
// group end: replacing Members left each affected account's own Groups list
// untouched, so the two reads disagreed and the update reported success.
func TestUpdateGroup_MembersChangeBothSides(t *testing.T) {
	store, user, engineering, support := membershipFixture(t)
	ctx := context.Background()

	// Add the account to support by writing the GROUP's half.
	members := []string{user.ID}
	if _, err := store.UpdateGroup(ctx, "acme", support.ID, UpdateGroupOptions{Members: &members}); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	assertMembershipAgrees(t, store, user.ID, support.ID, true)

	// And remove it from engineering the same way.
	empty := []string{}
	if _, err := store.UpdateGroup(ctx, "acme", engineering.ID, UpdateGroupOptions{Members: &empty}); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	assertMembershipAgrees(t, store, user.ID, engineering.ID, false)
}

// TestUpdateGroup_RefusesAMemberThatIsNotAUser covers the group-side validation.
// A membership naming a user that does not exist is a grant to nobody, written
// where authorization looks.
func TestUpdateGroup_RefusesAMemberThatIsNotAUser(t *testing.T) {
	store, user, engineering, _ := membershipFixture(t)
	ctx := context.Background()

	members := []string{"404"}
	if _, err := store.UpdateGroup(ctx, "acme", engineering.ID, UpdateGroupOptions{Members: &members}); err == nil {
		t.Fatal("a membership naming an unknown user must be refused")
	}
	assertMembershipAgrees(t, store, user.ID, engineering.ID, true)
}

// TestUpdateGroup_NilMembersLeavesMembershipAlone pins that renaming a group
// does not empty it.
func TestUpdateGroup_NilMembersLeavesMembershipAlone(t *testing.T) {
	store, user, engineering, _ := membershipFixture(t)
	ctx := context.Background()

	name := "engineering-renamed"
	if _, err := store.UpdateGroup(ctx, "acme", engineering.ID, UpdateGroupOptions{Name: &name}); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	assertMembershipAgrees(t, store, user.ID, engineering.ID, true)
}

// TestDiffGroupMembership covers the delta directly, including the repeated ids
// a caller can send.
func TestDiffGroupMembership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		current   []string
		desired   []string
		wantJoin  []string
		wantLeave []string
	}{
		{"no change", []string{"a", "b"}, []string{"a", "b"}, nil, nil},
		{"join one", []string{"a"}, []string{"a", "b"}, []string{"b"}, nil},
		{"leave one", []string{"a", "b"}, []string{"a"}, nil, []string{"b"}},
		{"swap", []string{"a"}, []string{"b"}, []string{"b"}, []string{"a"}},
		{"clear", []string{"a", "b"}, nil, nil, []string{"a", "b"}},
		{"from nothing", nil, []string{"a"}, []string{"a"}, nil},
		{"repeated desired id yields one join", []string{}, []string{"a", "a"}, []string{"a"}, nil},
		{"repeated current id yields one leave", []string{"a", "a"}, []string{}, nil, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			join, leave := diffGroupMembership(tc.current, tc.desired)
			if !equalStrings(join, tc.wantJoin) {
				t.Fatalf("join = %v, want %v", join, tc.wantJoin)
			}
			if !equalStrings(leave, tc.wantLeave) {
				t.Fatalf("leave = %v, want %v", leave, tc.wantLeave)
			}
		})
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
