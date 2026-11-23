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

package qrdbadapter

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// staleReadStorage runs afterGet once, right after the identity store has read
// the user, so a test can change the stored record between that read and the
// write that follows it: the window a concurrent revoke lands in.
type staleReadStorage struct {
	*QRDBUserStorage
	afterGet func()
}

func (s *staleReadStorage) GetUser(ctx context.Context, tenantID string, userID uint64) (*iam.User, error) {
	u, err := s.QRDBUserStorage.GetUser(ctx, tenantID, userID)
	if s.afterGet != nil {
		f := s.afterGet
		s.afterGet = nil
		f()
	}
	return u, err
}

func TestIdentityStore_UpdateUser_DoesNotRestoreAConcurrentRevoke(t *testing.T) {
	ctx := context.Background()
	fake := newFakeUserServicer()
	storage := newTestUserStorage(t, fake)

	user := newTestUser()
	if len(user.Roles) == 0 {
		user.Roles = []string{"role-admin"}
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	id := user.ID

	wrapped := &staleReadStorage{QRDBUserStorage: storage}
	wrapped.afterGet = func() {
		stored := fake.users[id]
		stored.RoleIDs = nil
		stored.Lifecycle = qrdbsdk.LifecycleDisabled
	}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: wrapped,
		RoleStorage: iam.NewMemoryStorage(),
	})

	newName := "Alice Renamed"
	if _, err := store.UpdateUser(ctx, testTenant, id, iam.UpdateUserOptions{Name: &newName}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got := fake.users[id]
	if len(got.RoleIDs) != 0 {
		t.Errorf("RoleIDs = %v, want the concurrently revoked roles to stay revoked", got.RoleIDs)
	}
	if got.Lifecycle != qrdbsdk.LifecycleDisabled {
		t.Errorf("Lifecycle = %q, want %q to survive the rename", got.Lifecycle, qrdbsdk.LifecycleDisabled)
	}
	if got.Name != newName {
		t.Errorf("Name = %q, want %q", got.Name, newName)
	}
	req := fake.lastUpdate
	if req == nil {
		t.Fatal("no UpdateUser request reached the store")
	}
	if req.RoleIDs != nil {
		t.Errorf("request RoleIDs = %v, want nil", req.RoleIDs)
	}
	if req.Lifecycle != "" {
		t.Errorf("request Lifecycle = %q, want empty", req.Lifecycle)
	}
	if req.CertBindings != nil {
		t.Errorf("request CertBindings = %v, want nil", req.CertBindings)
	}
}

func TestQRDBUserStorage_UpdateUserFields_SendsOnlyNamedFields(t *testing.T) {
	ctx := context.Background()
	fake := newFakeUserServicer()
	storage := newTestUserStorage(t, fake)

	user := newTestUser()
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	user.Roles = []string{"role-admin", "role-viewer"}

	if err := storage.UpdateUserFields(ctx, user, iam.UserFieldRoles); err != nil {
		t.Fatalf("UpdateUserFields: %v", err)
	}

	req := fake.lastUpdate
	if req == nil {
		t.Fatal("no UpdateUser request reached the store")
	}
	if len(req.RoleIDs) != 2 {
		t.Errorf("request RoleIDs = %v, want the two named roles", req.RoleIDs)
	}
	if req.Username != "" || req.Email != "" || req.Name != "" || req.Lifecycle != "" {
		t.Errorf("request carries unnamed fields: username=%q email=%q name=%q lifecycle=%q",
			req.Username, req.Email, req.Name, req.Lifecycle)
	}
	if req.Attributes != nil || req.AuthMethods != nil || req.CertBindings != nil {
		t.Errorf("request carries unnamed collections: attributes=%v authMethods=%v certBindings=%v",
			req.Attributes, req.AuthMethods, req.CertBindings)
	}
	if req.LastAuth != nil {
		t.Errorf("request LastAuth = %v, want nil", req.LastAuth)
	}
}

// An explicit empty roles list is a change that removes every role, so the
// field-scoped write must send it as an empty, non-nil list rather than as
// "roles unchanged".
func TestIdentityStore_UpdateUser_EmptyRolesClearsThem(t *testing.T) {
	ctx := context.Background()
	fake := newFakeUserServicer()
	storage := newTestUserStorage(t, fake)

	user := newTestUser()
	if len(user.Roles) == 0 {
		user.Roles = []string{"role-admin"}
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: iam.NewMemoryStorage(),
	})

	empty := []string{}
	if _, err := store.UpdateUser(ctx, testTenant, user.ID, iam.UpdateUserOptions{Roles: &empty}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	req := fake.lastUpdate
	if req == nil {
		t.Fatal("no UpdateUser request reached the store")
	}
	if req.RoleIDs == nil || len(req.RoleIDs) != 0 {
		t.Errorf("request RoleIDs = %#v, want an empty non-nil list", req.RoleIDs)
	}
	if got := fake.users[user.ID].RoleIDs; len(got) != 0 {
		t.Errorf("stored RoleIDs = %v, want none after clearing", got)
	}
}

// An update that names no field writes nothing, so it cannot write back a
// stale copy.
func TestIdentityStore_UpdateUser_NoChangeWritesNothing(t *testing.T) {
	ctx := context.Background()
	fake := newFakeUserServicer()
	storage := newTestUserStorage(t, fake)

	user := newTestUser()
	if len(user.Roles) == 0 {
		user.Roles = []string{"role-admin"}
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	created := append([]string(nil), fake.users[user.ID].RoleIDs...)
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: iam.NewMemoryStorage(),
	})

	fake.lastUpdate = nil
	if _, err := store.UpdateUser(ctx, testTenant, user.ID, iam.UpdateUserOptions{}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if fake.lastUpdate != nil {
		t.Errorf("an update naming no field sent %+v to the store", fake.lastUpdate)
	}
	got := fake.users[user.ID].RoleIDs
	if len(got) != len(created) {
		t.Fatalf("stored RoleIDs = %v, want %v unchanged", got, created)
	}
	for i := range created {
		if got[i] != created[i] {
			t.Errorf("stored RoleIDs = %v, want %v unchanged", got, created)
			break
		}
	}
}
