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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The handle is tenant-unique in the qrdb-backed store, which refuses a
// duplicate with UsernameExistsError. These tests hold the in-memory store to
// the same rule.
//
// The stakes are higher than "the two stores differ". GetUserByUsername scans
// the user map and returns the FIRST match, and Go randomises map iteration, so
// two accounts sharing a handle make that lookup resolve NONDETERMINISTICALLY —
// a role assignment or a ceremony subject could reach either account, and a
// second call could reach the other one.

// TestMemoryStorage_CreateUser_RefusesADuplicateHandle pins the create rule.
func TestMemoryStorage_CreateUser_RefusesADuplicateHandle(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	first := &User{TenantID: "acme", Username: "alice", Name: "Alice One", Email: "alice1@acme.test"}
	if err := mem.CreateUser(ctx, first); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	second := &User{TenantID: "acme", Username: "alice", Name: "Alice Two", Email: "alice2@acme.test"}
	if err := mem.CreateUser(ctx, second); !isErrorCode(err, dberrors.ErrAlreadyExists) {
		t.Fatalf("a duplicate handle must be refused as a conflict, got %v", err)
	}

	// The refused create must not have been written: the lookup has to stay
	// deterministic, which is the whole point of the rule.
	for i := 0; i < 20; i++ {
		got, err := mem.GetUserByUsername(ctx, "acme", "alice")
		if err != nil {
			t.Fatalf("GetUserByUsername: %v", err)
		}
		if got.ID != first.ID {
			t.Fatalf("lookup resolved to %d, want the only holder %d — a duplicate was stored",
				got.ID, first.ID)
		}
	}
}

// TestMemoryStorage_CreateUser_HandleIsUniquePerTenant pins the scope: the same
// handle in another tenant is a different person and must be allowed.
func TestMemoryStorage_CreateUser_HandleIsUniquePerTenant(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	if err := mem.CreateUser(ctx, &User{
		TenantID: "acme", Username: "alice", Email: "alice@acme.test",
	}); err != nil {
		t.Fatalf("CreateUser acme: %v", err)
	}
	if err := mem.CreateUser(ctx, &User{
		TenantID: "other", Username: "alice", Email: "alice@other.test",
	}); err != nil {
		t.Fatalf("the same handle in another tenant must be allowed: %v", err)
	}
}

// TestMemoryStorage_CreateUser_AllowsRecordsWithNoHandle pins that the check
// does not invent a constraint the model does not have: a record carrying no
// handle is not addressable by username, so two of them cannot collide.
func TestMemoryStorage_CreateUser_AllowsRecordsWithNoHandle(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	if err := mem.CreateUser(ctx, &User{TenantID: "acme", Email: "one@acme.test"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := mem.CreateUser(ctx, &User{TenantID: "acme", Email: "two@acme.test"}); err != nil {
		t.Fatalf("a second record with no handle must be allowed: %v", err)
	}
}

// TestMemoryStorage_UpdateUser_RefusesTakingAnotherAccountsHandle pins the
// update rule, which is the half a create-only check would leave open: two
// accounts created legally could still be made to collide afterwards.
func TestMemoryStorage_UpdateUser_RefusesTakingAnotherAccountsHandle(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	first := &User{TenantID: "acme", Username: "alice", Email: "alice@acme.test"}
	if err := mem.CreateUser(ctx, first); err != nil {
		t.Fatalf("CreateUser first: %v", err)
	}
	second := &User{TenantID: "acme", Username: "bob", Email: "bob@acme.test"}
	if err := mem.CreateUser(ctx, second); err != nil {
		t.Fatalf("CreateUser second: %v", err)
	}

	second.Username = "alice"
	if err := mem.UpdateUser(ctx, second); !isErrorCode(err, dberrors.ErrAlreadyExists) {
		t.Fatalf("taking another account's handle must be refused, got %v", err)
	}

	stored, err := mem.GetUser(ctx, "acme", second.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if stored.Username != "bob" {
		t.Fatalf("Username = %q, want it unchanged after a refused update", stored.Username)
	}
	got, err := mem.GetUserByUsername(ctx, "acme", "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("lookup resolved to %d, want %d", got.ID, first.ID)
	}
}

// TestMemoryStorage_UpdateUser_KeepingItsOwnHandleIsNotACollision pins that the
// check compares against OTHER records. A record re-written with the handle it
// already holds — which every update that does not touch the handle does — must
// not collide with itself.
func TestMemoryStorage_UpdateUser_KeepingItsOwnHandleIsNotACollision(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	user := &User{TenantID: "acme", Username: "alice", Name: "Alice", Email: "alice@acme.test"}
	if err := mem.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user.Name = "Alice Renamed"
	if err := mem.UpdateUser(ctx, user); err != nil {
		t.Fatalf("an update that keeps the handle must be accepted: %v", err)
	}

	// And an update that re-asserts the same handle explicitly is equally fine.
	user.Username = "alice"
	if err := mem.UpdateUser(ctx, user); err != nil {
		t.Fatalf("re-asserting the same handle must be accepted: %v", err)
	}
}
