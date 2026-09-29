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

// TestExportImportRoundTrip_PreservesTheHandle pins that an export/import cycle
// returns an account answering to the same username it left with.
//
// The export used to write the DISPLAY name into UserImport.Username, with a
// comment claiming the two were the same thing. Once the handle became a field
// of its own that was silent corruption: a round trip re-identified every
// account whose display name differed from its username, and the account could
// no longer be found by the name an administrator types.
func TestExportImportRoundTrip_PreservesTheHandle(t *testing.T) {
	ctx := context.Background()
	source := NewMemoryStorage()
	sourceStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: source, RoleStorage: source,
	})

	created, err := sourceStore.CreateUser(ctx, "acme", CreateUserOptions{
		Email:     "rosa@acme.test",
		Username:  "rosa",
		Name:      "Rosa Luxemburg",
		Lifecycle: LifecycleActive,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	export := &BatchExportResult{TenantID: "acme", Users: []*User{created}}
	req := export.ConvertToImportRequest("target")
	if len(req.Users) != 1 {
		t.Fatalf("export produced %d users, want 1", len(req.Users))
	}
	if req.Users[0].Username != "rosa" {
		t.Fatalf("exported Username = %q, want the handle, not the display name", req.Users[0].Username)
	}
	if req.Users[0].Name != "Rosa Luxemburg" {
		t.Fatalf("exported Name = %q, want the display name", req.Users[0].Name)
	}

	target := NewMemoryStorage()
	targetStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: target, RoleStorage: target,
	})
	bm := NewBatchManager(BatchManagerOptions{IdentityStore: targetStore})
	if _, err := bm.ImportUsers(ctx, "target", req.Users, BatchImportOptions{}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	imported, err := targetStore.GetUserByUsername(ctx, "target", "rosa")
	if err != nil {
		t.Fatalf("the imported account must resolve by the handle it was exported with: %v", err)
	}
	if imported.Name != "Rosa Luxemburg" {
		t.Fatalf("Name = %q, want the display name preserved separately", imported.Name)
	}
}

// TestImportUsers_UpdateExistingAppliesTheHandle covers the branch a re-import
// takes. The update path applied Name and not Username, so re-importing an
// export left the account answering to its old handle while the file said
// otherwise — and the two silently disagreed from then on.
func TestImportUsers_UpdateExistingAppliesTheHandle(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem, RoleStorage: mem,
	})

	if _, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:     "sami@acme.test",
		Username:  "sami",
		Name:      "Sami One",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	bm := NewBatchManager(BatchManagerOptions{IdentityStore: store})
	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
		Email:    "sami@acme.test",
		Username: "samira",
		Name:     "Sami Two",
	}}, BatchImportOptions{UpdateExisting: true}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	if _, err := store.GetUserByUsername(ctx, "acme", "samira"); err != nil {
		t.Fatalf("the re-imported account must resolve by its new handle: %v", err)
	}
	updated, err := store.GetUserByEmail(ctx, "acme", "sami@acme.test")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if updated.Username != "samira" {
		t.Fatalf("Username = %q, want the imported handle", updated.Username)
	}
	if updated.Name != "Sami Two" {
		t.Fatalf("Name = %q, want the imported display name", updated.Name)
	}
}
