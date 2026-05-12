// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestProject builds a minimal valid Project for use in store tests.
func newTestProject(id, tenantID, name string) *Project {
	return &Project{
		ID:       id,
		TenantID: tenantID,
		Name:     name,
		Workspace: ProjectWorkspace{
			AgentID: "agent-1",
			Path:    "/workspace/foo",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// =============================================================================
// Create
// =============================================================================

func TestProjectStore_Create_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "my-project")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_Create_DuplicateName(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p1 := newTestProject("id-1", "tenant-1", "dup")
	if err := store.Create(ctx, p1); err != nil {
		t.Fatalf("unexpected error creating first: %v", err)
	}

	p2 := newTestProject("id-2", "tenant-1", "dup")
	err := store.Create(ctx, p2)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Errorf("expected ErrProjectAlreadyExists, got %T: %v", err, err)
	}
}

func TestProjectStore_Create_NilProject(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Create(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil project, got nil")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %T: %v", err, err)
	}
}

func TestProjectStore_Create_EmptyID(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("", "tenant-1", "name")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_Create_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("id-1", "", "name")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_Create_EmptyName(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("id-1", "tenant-1", "")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// Get
// =============================================================================

func TestProjectStore_Get_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "proj")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "proj" {
		t.Errorf("expected name 'proj', got %q", got.Name)
	}
}

func TestProjectStore_Get_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.Get(context.Background(), "tenant-1", "missing-id")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Get_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.Get(context.Background(), "", "id-1")
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// GetByName
// =============================================================================

func TestProjectStore_GetByName_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "by-name")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.GetByName(ctx, "tenant-1", "by-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", got.ID)
	}
}

func TestProjectStore_GetByName_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.GetByName(context.Background(), "tenant-1", "ghost")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_GetByName_EmptyName(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.GetByName(context.Background(), "tenant-1", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// Update
// =============================================================================

func TestProjectStore_Update_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "old-name")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	p.Name = "new-name"
	if err := store.Update(ctx, p); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, err := store.GetByName(ctx, "tenant-1", "new-name")
	if err != nil {
		t.Fatalf("get by new name failed: %v", err)
	}
	if got.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", got.ID)
	}
}

func TestProjectStore_Update_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("ghost", "tenant-1", "ghost")
	err := store.Update(context.Background(), p)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Update_NilProject(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Update(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil project")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestProjectStore_Delete_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "to-delete")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := store.Delete(ctx, "tenant-1", "id-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := store.Get(ctx, "tenant-1", "id-1")
	if err == nil {
		t.Fatal("expected not-found after delete, got nil")
	}
}

func TestProjectStore_Delete_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Delete(context.Background(), "tenant-1", "ghost")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Delete_EmptyID(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Delete(context.Background(), "tenant-1", "")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// ListByTenant
// =============================================================================

func TestProjectStore_ListByTenant_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	for i, name := range []string{"a", "b", "c"} {
		p := newTestProject("id-"+name, "tenant-1", name)
		_ = i
		if err := store.Create(ctx, p); err != nil {
			t.Fatalf("create %q failed: %v", name, err)
		}
	}
	// Create a project for a different tenant — must not appear.
	other := newTestProject("other-id", "tenant-2", "other")
	if err := store.Create(ctx, other); err != nil {
		t.Fatalf("create other-tenant project failed: %v", err)
	}

	projects, err := store.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
}

func TestProjectStore_ListByTenant_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.ListByTenant(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_ListByTenant_Empty(t *testing.T) {
	store := NewInMemoryProjectStore()
	projects, err := store.ListByTenant(context.Background(), "tenant-nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

// =============================================================================
// Concurrent access (basic smoke test)
// =============================================================================

func TestProjectStore_ConcurrentCreate(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := "id-" + string(rune('a'+n))
			name := "proj-" + string(rune('a'+n))
			errs <- store.Create(ctx, newTestProject(id, "tenant-1", name))
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent create error: %v", err)
		}
	}

	projects, err := store.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(projects) != 10 {
		t.Errorf("expected 10 projects, got %d", len(projects))
	}
}
