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
		ID:       "u1",
		TenantID: "t1",
		Name:     "alice",
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
	if got.ID != "u1" {
		t.Fatalf("resolved user = %q, want u1", got.ID)
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

	if err := storage.CreateUser(ctx, &User{ID: "u1", TenantID: "t1", Name: "erin", Email: "erin@example.com"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Hit: the user resolves within its own tenant.
	got, err := storage.GetUserByUsername(ctx, "t1", "erin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != "u1" {
		t.Fatalf("resolved user = %q, want u1", got.ID)
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
