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
	"fmt"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// deleteFailUserStorage wraps a MemoryStorage but fails DeleteUser for a
// specific user ID, exercising DeleteTenant's fail-closed user-deprovision
// path. ListUsers/CreateUser delegate to the embedded store so a realistic
// multi-user population can be set up.
type deleteFailUserStorage struct {
	*MemoryStorage
	failUserID string
	failErr    error
}

func (s *deleteFailUserStorage) DeleteUser(ctx context.Context, tenantID, userID string) error {
	if userID == s.failUserID {
		return s.failErr
	}
	return s.MemoryStorage.DeleteUser(ctx, tenantID, userID)
}

// seedTenantUsers creates a tenant in the given storage plus n users in it,
// returning the created user IDs.
func seedTenantUsers(t *testing.T, store UserStorage, tenantID string, n int) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("user-%03d", i)
		u := &User{
			ID:       id,
			TenantID: tenantID,
			Name:     id,
			Email:    fmt.Sprintf("%s@%s.local", id, tenantID),
		}
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser(%s) failed: %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestDeleteTenant_DeprovisionsUsers proves DeleteTenant drains every user in
// the tenant through the wired user store, including across multiple ListUsers
// pages (the population exceeds the internal page size).
func TestDeleteTenant_DeprovisionsUsers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	// Manager uses MemoryStorage for tenant metadata AND as the routed user
	// store, so DeleteTenant's deprovision step deletes the seeded users.
	mgr := NewDefaultTenantManager(store)
	mgr.SetUserStorage(store)

	const tenantID = "deprovision-test"
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: tenantID, Name: "Deprovision Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Seed more users than a single ListUsers page (page size is 256) so the
	// pagination loop in DeleteTenant is exercised across multiple pages.
	const userCount = 600
	seedTenantUsers(t, store, tenantID, userCount)

	// Also seed a user in a DIFFERENT tenant to prove cross-tenant isolation:
	// it must survive the delete.
	otherID := "other-tenant"
	otherUsers := seedTenantUsers(t, store, otherID, 3)

	if err := mgr.DeleteTenant(ctx, tenantID); err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	// The deleted tenant must have zero users left.
	remaining, err := store.ListUsers(ctx, tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers after delete failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected all %d users deprovisioned, %d remain", userCount, len(remaining))
	}

	// The other tenant's users must be untouched.
	for _, id := range otherUsers {
		if _, err := store.GetUser(ctx, otherID, id); err != nil {
			t.Fatalf("cross-tenant user %s was wrongly removed: %v", id, err)
		}
	}
}

// TestDeleteTenant_UserDeprovisionFailureAborts proves a DeleteUser failure
// during deprovision aborts the tenant delete with a wrapped storage error,
// and that the tenant metadata is NOT removed (fail-closed).
func TestDeleteTenant_UserDeprovisionFailureAborts(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	sentinel := errors.New("simulated delete failure")
	store := &deleteFailUserStorage{
		MemoryStorage: mem,
		failUserID:    "user-001",
		failErr:       sentinel,
	}

	mgr := NewDefaultTenantManager(mem)
	mgr.SetUserStorage(store)

	const tenantID = "abort-test"
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: tenantID, Name: "Abort Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}
	seedTenantUsers(t, store, tenantID, 3)

	err := mgr.DeleteTenant(ctx, tenantID)
	if err == nil {
		t.Fatal("expected DeleteTenant to fail when a user deprovision fails")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
	var qb *dberrors.QBError
	if !errors.As(err, &qb) {
		t.Fatalf("expected a wrapped QBError, got %T: %v", err, err)
	}
	if qb.Op != "iam.DeleteTenant" {
		t.Fatalf("expected op iam.DeleteTenant, got %q", qb.Op)
	}

	// Fail-closed: the tenant must still exist because the cascade aborted
	// before storage.DeleteTenant.
	if _, err := mgr.GetTenant(ctx, tenantID); err != nil {
		t.Fatalf("tenant should still exist after aborted delete, got %v", err)
	}
}

// TestSetUserStorage_Accessor proves the setter/accessor round-trips the wired
// user store.
func TestSetUserStorage_Accessor(t *testing.T) {
	store := NewMemoryStorage()
	mgr := NewDefaultTenantManager(store)

	if mgr.UserStorage() != nil {
		t.Fatal("expected nil user storage before wiring")
	}
	mgr.SetUserStorage(store)
	if mgr.UserStorage() == nil {
		t.Fatal("expected non-nil user storage after wiring")
	}
}

// TestDeleteTenant_NilUserStorage proves DeleteTenant still works when no user
// store is wired (the deprovision step is a no-op), preserving legacy behavior.
func TestDeleteTenant_NilUserStorage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	mgr := NewDefaultTenantManager(store)
	// Deliberately do NOT call SetUserStorage.

	const tenantID = "no-userstore"
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: tenantID, Name: "No UserStore"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	if err := mgr.DeleteTenant(ctx, tenantID); err != nil {
		t.Fatalf("DeleteTenant with nil user storage failed: %v", err)
	}

	if _, err := mgr.GetTenant(ctx, tenantID); !dberrors.IsNotFound(err) {
		t.Fatalf("expected tenant to be deleted, got %v", err)
	}
}
