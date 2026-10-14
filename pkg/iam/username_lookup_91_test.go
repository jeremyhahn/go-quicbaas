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
	"time"
)

// TestIdentityStore_GetUserByUsername covers the username-resolution seam used by
// the RBAC role-binding service: a successful tenant-scoped resolve, a propagated
// not-found from storage, and the two input-validation guards (empty tenant and
// empty username) that must fail before storage is consulted.
func TestIdentityStore_GetUserByUsername(t *testing.T) {
	ctx := context.Background()
	store := newMockUserStorage()
	if err := store.CreateUser(ctx, &User{
		ID:       1,
		TenantID: "t1",
		Username: "alice",
		Name:     "Alice",
		Email:    "alice@example.com",
		Created:  time.Now(),
		Updated:  time.Now(),
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	id := NewIdentityStore(IdentityStoreOptions{UserStorage: store})

	// Success: tenant-scoped username resolves to the seeded user.
	got, err := id.GetUserByUsername(ctx, "t1", "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("resolved user = %d, want 1", got.ID)
	}

	// Not found: a username that exists in no tenant propagates ErrUserNotFound.
	if _, err := id.GetUserByUsername(ctx, "t1", "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing username: expected ErrUserNotFound, got %v", err)
	}

	// Validation: an empty tenant ID fails closed before storage is consulted.
	if _, err := id.GetUserByUsername(ctx, "", "alice"); err == nil {
		t.Fatal("empty tenant ID must be rejected")
	} else {
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != "tenant_id" {
			t.Fatalf("expected tenant_id ValidationError, got %v", err)
		}
	}

	// Validation: an empty username fails closed with a typed IAM error.
	if _, err := id.GetUserByUsername(ctx, "t1", ""); err == nil {
		t.Fatal("empty username must be rejected")
	}
}

// The persistent backend username-index test was removed in phase C1-F:
// BackendStorage no longer persists users or the username index (qrdb owns the
// user record). The IdentityStore seam test above and the MemoryStorage test
// below still cover username resolution; the qrdbadapter suite covers the
// qrdb-backed index.

// TestMemoryStorage_GetUserByUsername covers the in-memory scan resolver: a hit on
// the tenant-scoped name, a miss returning ErrUserNotFound, and a tenant mismatch
// (right username, wrong tenant) that must not cross the tenant boundary.
func TestMemoryStorage_GetUserByUsername(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Tenant One"}); err != nil {
		t.Fatalf("CreateTenant t1: %v", err)
	}
	if err := storage.CreateTenant(ctx, &Tenant{ID: "t2", Name: "Tenant Two"}); err != nil {
		t.Fatalf("CreateTenant t2: %v", err)
	}

	if err := storage.CreateUser(ctx, &User{ID: 1, TenantID: "t1", Username: "erin", Name: "Erin", Email: "erin@example.com"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Hit: the user resolves within its own tenant.
	got, err := storage.GetUserByUsername(ctx, "t1", "erin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("resolved user = %d, want 1", got.ID)
	}

	// Miss: an unknown username returns ErrUserNotFound.
	if _, err := storage.GetUserByUsername(ctx, "t1", "frank"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing username: expected ErrUserNotFound, got %v", err)
	}

	// Tenant mismatch: the right username under the wrong tenant must not resolve.
	if _, err := storage.GetUserByUsername(ctx, "t2", "erin"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("cross-tenant lookup must fail closed, got %v", err)
	}
}

// TestCreateUser_KeepsTheHandleAndTheDisplayNameApart pins the fix for a field
// that was required, validated, and then thrown away.
//
// CreateUser demands a Username (3-64 of [A-Za-z0-9_-], so it can carry a
// uniqueness constraint) and then stored the free-form display Name over it.
// The projection writes that field into qrdb's tenant-unique Username, so:
//
//   - a lookup by the handle the caller chose missed the record it had just
//     created — and that lookup is how RBAC assigns a role and how a
//     username-subject ceremony resolves its principal; and
//   - two people who share a display name were pointed at one uniqueness
//     constraint, which neither of them asked to share.
//
// Both follow from one cause, so the test asserts both: the handle resolves,
// and a shared display name does not make two accounts collide.
func TestCreateUser_KeepsTheHandleAndTheDisplayNameApart(t *testing.T) {
	ctx := context.Background()
	store := newTestIdentityStore()

	alice, err := store.CreateUser(ctx, "t1", CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
		Name:     "Alice Smith",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if alice.Username != "alice" {
		t.Fatalf("Username = %q, want the handle the caller chose", alice.Username)
	}
	if alice.Name != "Alice Smith" {
		t.Fatalf("Name = %q, want the display name preserved", alice.Name)
	}

	// The handle resolves. This is the lookup RBAC assign/revoke runs.
	got, err := store.GetUserByUsername(ctx, "t1", "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername(alice): %v", err)
	}
	if got.ID != alice.ID {
		t.Fatalf("resolved %d, want %d", got.ID, alice.ID)
	}

	// The display name is not a handle and resolves nobody: it carries no
	// uniqueness constraint, so treating it as one is what produced the
	// collision below.
	if _, err := store.GetUserByUsername(ctx, "t1", "Alice Smith"); err == nil {
		t.Fatal("a display name resolved as a handle")
	}

	// A second account with the SAME display name and its own handle is a
	// normal thing to create, and each keeps its own identity.
	other, err := store.CreateUser(ctx, "t1", CreateUserOptions{
		Email:    "alice.smith@example.com",
		Username: "asmith",
		Name:     "Alice Smith",
	})
	if err != nil {
		t.Fatalf("two users may share a display name: %v", err)
	}
	if other.ID == alice.ID {
		t.Fatal("the second account collapsed onto the first")
	}
	resolved, err := store.GetUserByUsername(ctx, "t1", "asmith")
	if err != nil {
		t.Fatalf("GetUserByUsername(asmith): %v", err)
	}
	if resolved.ID != other.ID {
		t.Fatalf("resolved %d, want the second account %d", resolved.ID, other.ID)
	}
}

// TestUpdateUser_RenamingTheDisplayNameLeavesTheHandleAlone is the update-path
// half. The two fields used to be one write with a precedence rule — Name wins,
// else Username is written INTO Name — so changing a user's display name
// silently changed the handle every role assignment and ceremony subject
// resolves through. A rename is not a re-identification.
func TestUpdateUser_RenamingTheDisplayNameLeavesTheHandleAlone(t *testing.T) {
	ctx := context.Background()
	store := newTestIdentityStore()

	created, err := store.CreateUser(ctx, "t1", CreateUserOptions{
		Email:    "bob@example.com",
		Username: "bob",
		Name:     "Bob",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newName := "Robert Tables"
	updated, err := store.UpdateUser(ctx, "t1", created.ID, UpdateUserOptions{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("Name = %q, want the new display name", updated.Name)
	}
	if updated.Username != "bob" {
		t.Fatalf("Username = %q, want the handle untouched by a rename", updated.Username)
	}
	if _, err := store.GetUserByUsername(ctx, "t1", "bob"); err != nil {
		t.Fatalf("the handle stopped resolving after a display-name change: %v", err)
	}
}
