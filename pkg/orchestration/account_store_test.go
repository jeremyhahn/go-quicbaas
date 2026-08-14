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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

func newTestAccount(tenantID, id string, provider AccountProvider) *Account {
	return &Account{
		ID:       id,
		TenantID: tenantID,
		Provider: provider,
	}
}

func TestAccountStore_CreateGetRoundTrip(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.DisplayName = "alice"

	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.DisplayName != "alice" || got.Provider != AccountProviderClaude {
		t.Fatalf("Get() = %+v, want display_name=alice provider=claude", got)
	}
	if got.Status != AccountStatusActive {
		t.Fatalf("Get().Status = %s, want active (default)", got.Status)
	}
}

// TestAccountStore_TenantIsolation_ImpossibleByConstruction proves tenant B
// can never observe tenant A's account through Get or ListByTenant, even
// when both tenants use the same account ID.
func TestAccountStore_TenantIsolation_ImpossibleByConstruction(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	a := newTestAccount("tenant-a", "shared-id", AccountProviderClaude)
	b := newTestAccount("tenant-b", "shared-id", AccountProviderCodex)

	if err := store.Create(ctx, a); err != nil {
		t.Fatalf("Create(a) error = %v", err)
	}
	if err := store.Create(ctx, b); err != nil {
		t.Fatalf("Create(b) error = %v", err)
	}

	gotA, err := store.Get(ctx, "tenant-a", "shared-id")
	if err != nil {
		t.Fatalf("Get(tenant-a) error = %v", err)
	}
	if gotA.Provider != AccountProviderClaude {
		t.Fatalf("Get(tenant-a).Provider = %s, want claude (tenant-b leaked in)", gotA.Provider)
	}

	gotB, err := store.Get(ctx, "tenant-b", "shared-id")
	if err != nil {
		t.Fatalf("Get(tenant-b) error = %v", err)
	}
	if gotB.Provider != AccountProviderCodex {
		t.Fatalf("Get(tenant-b).Provider = %s, want codex (tenant-a leaked in)", gotB.Provider)
	}

	listA, err := store.ListByTenant(ctx, "tenant-a", "", "")
	if err != nil {
		t.Fatalf("ListByTenant(tenant-a) error = %v", err)
	}
	if len(listA) != 1 || listA[0].TenantID != "tenant-a" {
		t.Fatalf("ListByTenant(tenant-a) = %+v, want exactly tenant-a's own record", listA)
	}

	// A tenant cannot even ask for another tenant's account by supplying its
	// ID under its own tenant scope and expecting a cross-tenant read: the
	// key layout makes tenant-b's "shared-id" a physically different key
	// from tenant-a's, so there is no code path that could return it.
	if _, err := store.Get(ctx, "tenant-a", "does-not-exist-for-a"); err == nil {
		t.Fatalf("Get() for a nonexistent ID under tenant-a unexpectedly succeeded")
	}
}

func TestAccountStore_Get_NotFound(t *testing.T) {
	store := NewInMemoryAccountStore()
	_, err := store.Get(context.Background(), "tenant-a", "missing")
	var notFound *AccountNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Get() error = %v (%T), want *AccountNotFoundError", err, err)
	}
}

func TestAccountStore_SetDefault_ClearsPreviousDefault(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	a := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	a.Default = true
	b := newTestAccount("tenant-a", "acct-2", AccountProviderClaude)

	if err := store.Create(ctx, a); err != nil {
		t.Fatalf("Create(a) error = %v", err)
	}
	if err := store.Create(ctx, b); err != nil {
		t.Fatalf("Create(b) error = %v", err)
	}

	if err := store.SetDefault(ctx, "tenant-a", "acct-2"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}

	gotA, _ := store.Get(ctx, "tenant-a", "acct-1")
	gotB, _ := store.Get(ctx, "tenant-a", "acct-2")
	if gotA.Default {
		t.Fatalf("acct-1.Default = true, want false after acct-2 became default")
	}
	if !gotB.Default {
		t.Fatalf("acct-2.Default = false, want true")
	}
}

func TestAccountStore_Delete_RemovesRecord(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, "tenant-a", "acct-1"); err == nil {
		t.Fatalf("Get() after Delete() succeeded, want AccountNotFoundError")
	}
}

func TestAccountStore_Delete_NotFound(t *testing.T) {
	store := NewInMemoryAccountStore()
	if err := store.Delete(context.Background(), "tenant-a", "missing"); err == nil {
		t.Fatalf("Delete() of a nonexistent account succeeded, want AccountNotFoundError")
	}
}

func TestAccountStore_ListByTenant_FiltersProviderAndProject(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	claude1 := newTestAccount("tenant-a", "claude-1", AccountProviderClaude)
	claude2 := newTestAccount("tenant-a", "claude-2", AccountProviderClaude)
	claude2.ProjectID = "proj-1"
	codex1 := newTestAccount("tenant-a", "codex-1", AccountProviderCodex)

	for _, a := range []*Account{claude1, claude2, codex1} {
		if err := store.Create(ctx, a); err != nil {
			t.Fatalf("Create(%s) error = %v", a.ID, err)
		}
	}

	claudeOnly, err := store.ListByTenant(ctx, "tenant-a", AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("ListByTenant(claude) error = %v", err)
	}
	if len(claudeOnly) != 2 {
		t.Fatalf("ListByTenant(claude) len = %d, want 2", len(claudeOnly))
	}

	proj1Only, err := store.ListByTenant(ctx, "tenant-a", "", "proj-1")
	if err != nil {
		t.Fatalf("ListByTenant(proj-1) error = %v", err)
	}
	if len(proj1Only) != 1 || proj1Only[0].ID != "claude-2" {
		t.Fatalf("ListByTenant(proj-1) = %+v, want exactly claude-2", proj1Only)
	}
}

func TestAccountStore_Touch_UpdatesBookkeeping(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{MarkUsed: true}); err != nil {
		t.Fatalf("Touch(MarkUsed) error = %v", err)
	}
	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt is zero after Touch(MarkUsed)")
	}

	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		SetStatusValid: true,
		SetStatus:      AccountStatusError,
		SetLastError:   "refresh failed",
	}); err != nil {
		t.Fatalf("Touch(error) error = %v", err)
	}
	got, _ = store.Get(ctx, "tenant-a", "acct-1")
	if got.Status != AccountStatusError || got.LastError != "refresh failed" {
		t.Fatalf("Touch(error) result = %+v, want status=error last_error=refresh failed", got)
	}

	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		SetStatusValid: true,
		SetStatus:      AccountStatusActive,
		ClearLastError: true,
	}); err != nil {
		t.Fatalf("Touch(recover) error = %v", err)
	}
	got, _ = store.Get(ctx, "tenant-a", "acct-1")
	if got.Status != AccountStatusActive || got.LastError != "" {
		t.Fatalf("Touch(recover) result = %+v, want status=active last_error=\"\"", got)
	}
}
