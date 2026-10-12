package iam

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const cascadeTenant = "tenant1"

// cascadeFaultUserStorage fails chosen calls of a DeleteRole cascade so a
// test can stop it partway and then retry with the fault cleared.
type cascadeFaultUserStorage struct {
	*mockUserStorage
	failListPastFirstPage bool
	failUpdateID          string
}

func (f *cascadeFaultUserStorage) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	if f.failListPastFirstPage && opts.Offset > 0 {
		return nil, &dberrors.MessageError{Message: "simulated list failure"}
	}
	return f.mockUserStorage.ListUsers(ctx, tenantID, opts)
}

func (f *cascadeFaultUserStorage) UpdateUser(ctx context.Context, user *User) error {
	if f.failUpdateID != "" && user.ID == f.failUpdateID {
		return &dberrors.MessageError{Message: "simulated update failure"}
	}
	return f.mockUserStorage.UpdateUser(ctx, user)
}

// newCascadeFixture builds a store over a fault-injecting user double, seeds
// n users, and creates the role under test. It returns the users in the
// order the store lists them.
func newCascadeFixture(t *testing.T, n int) (*IdentityStore, *cascadeFaultUserStorage, *Role, []*User) {
	t.Helper()
	users := &cascadeFaultUserStorage{mockUserStorage: newMockUserStorage()}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: users,
		RoleStorage: newMockRoleStorage(),
	})
	ctx := context.Background()

	role, err := store.CreateRole(ctx, cascadeTenant, CreateRoleOptions{Name: "Cascade Role"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	for i := range n {
		err := users.CreateUser(ctx, &User{
			TenantID: cascadeTenant,
			Email:    fmt.Sprintf("user%d@example.com", i),
			Username: fmt.Sprintf("user%d", i),
		})
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
	}
	return store, users, role, listAllUsers(t, users)
}

func listAllUsers(t *testing.T, users UserStorage) []*User {
	t.Helper()
	all, err := users.ListUsers(context.Background(), cascadeTenant, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	return all
}

// grantRole gives each user roleID directly through the storage.
func grantRole(t *testing.T, users UserStorage, holders []*User, roleID string) {
	t.Helper()
	for _, u := range holders {
		u.Roles = append(u.Roles, roleID)
		if err := users.UpdateUser(context.Background(), u); err != nil {
			t.Fatalf("UpdateUser %s: %v", u.ID, err)
		}
	}
}

// holderIDs returns the ids of every listed user that carries roleID.
func holderIDs(t *testing.T, users UserStorage, roleID string) []string {
	t.Helper()
	var ids []string
	for _, u := range listAllUsers(t, users) {
		if slices.Contains(u.Roles, roleID) {
			ids = append(ids, u.ID)
		}
	}
	return ids
}

func userIDs(users []*User) []string {
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	return ids
}

func assertRoleExists(t *testing.T, store *IdentityStore, roleID string, want bool) {
	t.Helper()
	_, err := store.GetRole(context.Background(), cascadeTenant, roleID)
	if got := err == nil; got != want {
		t.Fatalf("role exists = %v, want %v (err %v)", got, want, err)
	}
}

func TestDeleteRole_StripsHoldersAcrossPages(t *testing.T) {
	// More holders than the old single 1000-user page, and several cascade
	// pages, plus a few users holding only another role.
	const holders = 4*deleteRoleCascadePage + 1
	store, users, role, all := newCascadeFixture(t, holders+3)
	ctx := context.Background()

	other, err := store.CreateRole(ctx, cascadeTenant, CreateRoleOptions{Name: "Other Role"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	grantRole(t, users, all[:holders], role.ID)
	grantRole(t, users, all[holders:], other.ID)

	if err := store.DeleteRole(ctx, cascadeTenant, role.ID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	if ids := holderIDs(t, users, role.ID); len(ids) != 0 {
		t.Errorf("%d users still hold the deleted role, first %s", len(ids), ids[0])
	}
	if got, want := holderIDs(t, users, other.ID), userIDs(all[holders:]); !slices.Equal(got, want) {
		t.Errorf("non-holders changed: hold other role = %v, want %v", got, want)
	}
	assertRoleExists(t, store, role.ID, false)
}

func TestDeleteRole_StripsHoldersStraddlingPageBoundary(t *testing.T) {
	store, users, role, all := newCascadeFixture(t, 2*deleteRoleCascadePage+1)
	boundary := all[deleteRoleCascadePage-1 : deleteRoleCascadePage+1]
	grantRole(t, users, boundary, role.ID)

	if err := store.DeleteRole(context.Background(), cascadeTenant, role.ID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	if ids := holderIDs(t, users, role.ID); len(ids) != 0 {
		t.Errorf("users still hold the deleted role: %v", ids)
	}
	assertRoleExists(t, store, role.ID, false)
}

func TestDeleteRole_ExactPageMultipleTerminates(t *testing.T) {
	store, users, role, all := newCascadeFixture(t, 2*deleteRoleCascadePage)
	grantRole(t, users, all, role.ID)

	if err := store.DeleteRole(context.Background(), cascadeTenant, role.ID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	if ids := holderIDs(t, users, role.ID); len(ids) != 0 {
		t.Errorf("%d users still hold the deleted role", len(ids))
	}
	assertRoleExists(t, store, role.ID, false)
}

func TestDeleteRole_ListFailureMidCascadeKeepsRoleForRetry(t *testing.T) {
	store, users, role, all := newCascadeFixture(t, 2*deleteRoleCascadePage+1)
	grantRole(t, users, all, role.ID)
	users.failListPastFirstPage = true
	ctx := context.Background()

	err := store.DeleteRole(ctx, cascadeTenant, role.ID)
	if err == nil {
		t.Fatal("DeleteRole succeeded, want list failure")
	}
	if !strings.Contains(err.Error(), "offset") {
		t.Errorf("error %q does not name the failing page offset", err)
	}
	assertRoleExists(t, store, role.ID, true)
	if got, want := holderIDs(t, users, role.ID), userIDs(all[deleteRoleCascadePage:]); !slices.Equal(got, want) {
		t.Errorf("after failure, holders = %d users, want only the %d past the first page", len(got), len(want))
	}

	users.failListPastFirstPage = false
	if err := store.DeleteRole(ctx, cascadeTenant, role.ID); err != nil {
		t.Fatalf("retry DeleteRole: %v", err)
	}
	if ids := holderIDs(t, users, role.ID); len(ids) != 0 {
		t.Errorf("after retry, %d users still hold the deleted role", len(ids))
	}
	assertRoleExists(t, store, role.ID, false)
}

func TestDeleteRole_UpdateFailureOnSecondPageKeepsRoleForRetry(t *testing.T) {
	store, users, role, all := newCascadeFixture(t, 2*deleteRoleCascadePage+1)
	grantRole(t, users, all, role.ID)
	failing := all[deleteRoleCascadePage]
	users.failUpdateID = failing.ID
	ctx := context.Background()

	err := store.DeleteRole(ctx, cascadeTenant, role.ID)
	if err == nil {
		t.Fatal("DeleteRole succeeded, want update failure")
	}
	if !strings.Contains(err.Error(), failing.ID) {
		t.Errorf("error %q does not name the failing user %s", err, failing.ID)
	}
	assertRoleExists(t, store, role.ID, true)
	if got, want := holderIDs(t, users, role.ID), userIDs(all[deleteRoleCascadePage:]); !slices.Equal(got, want) {
		t.Errorf("after failure, holders = %d users, want only the %d from the failing user on", len(got), len(want))
	}

	users.failUpdateID = ""
	if err := store.DeleteRole(ctx, cascadeTenant, role.ID); err != nil {
		t.Fatalf("retry DeleteRole: %v", err)
	}
	if ids := holderIDs(t, users, role.ID); len(ids) != 0 {
		t.Errorf("after retry, %d users still hold the deleted role", len(ids))
	}
	assertRoleExists(t, store, role.ID, false)
}

func TestMemoryStorage_ListUsers_StableOffsetPages(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	const n = 11
	for i := range n {
		err := storage.CreateUser(ctx, &User{
			TenantID: cascadeTenant,
			Email:    fmt.Sprintf("user%d@example.com", i),
			Username: fmt.Sprintf("user%d", i),
		})
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
	}

	first := userIDs(listAllUsers(t, storage))
	if len(first) != n {
		t.Fatalf("listed %d users, want %d", len(first), n)
	}
	if !slices.IsSorted(first) {
		t.Errorf("listing is not ID-sorted: %v", first)
	}
	if again := userIDs(listAllUsers(t, storage)); !slices.Equal(first, again) {
		t.Errorf("listing order changed between calls: %v then %v", first, again)
	}

	const limit = 4
	var paged []string
	for offset := 0; offset < n; offset += limit {
		page, err := storage.ListUsers(ctx, cascadeTenant, ListOptions{Limit: limit, Offset: offset})
		if err != nil {
			t.Fatalf("ListUsers offset %d: %v", offset, err)
		}
		paged = append(paged, userIDs(page)...)
	}
	if !slices.Equal(paged, first) {
		t.Errorf("offset pages = %v, want each user exactly once in order %v", paged, first)
	}
}

func TestMemoryStorage_ListUsers_OffsetPastEnd(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	if err := storage.CreateUser(ctx, &User{
		TenantID: cascadeTenant,
		Email:    "only@example.com",
		Username: "only",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	page, err := storage.ListUsers(ctx, cascadeTenant, ListOptions{Limit: 10, Offset: 5})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(page) != 0 {
		t.Errorf("offset past end listed %d users, want 0", len(page))
	}
}
