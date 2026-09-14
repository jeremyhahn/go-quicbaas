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

// scopeDefaultPointer reads a scope's default pointer directly, bypassing the
// derivation an Account read would do — the assertions below are about the
// pointer itself, and reading it through Account.Default would only report
// what some account happens to derive from it.
func scopeDefaultPointer(t *testing.T, store AccountStore, tenantID string, provider AccountProvider, projectID string) string {
	t.Helper()
	s, ok := store.(*accountStore)
	if !ok {
		t.Fatalf("store is %T, want *accountStore", store)
	}
	defaultKey, err := accountDefaultKey(tenantID, provider, projectID)
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	holderID, err := s.currentDefaultID(context.Background(), defaultKey)
	if err != nil {
		t.Fatalf("currentDefaultID() error = %v", err)
	}
	return holderID
}

// TestAccountStore_FailedCreateReleasesTheScopeItClaimed is the regression test
// for #249: a create that wins a vacant scope's election and then fails to
// write its record must leave the scope as it found it — vacant.
//
// THE FAILURE IS DURABLE, NOT TRANSIENT, which is why it is worth a test of its
// own. electDefaultLocked only heals a pointer whose holder has NO record
// (defaultHolderMissing), and this pointer names an account whose record exists
// in a DIFFERENT scope — a state the store deliberately refuses to heal,
// because it cannot distinguish it from a move in flight. So nothing the store
// does later recovers the scope; every subsequent create there reports
// Default=false and selection finds no default at all.
func TestAccountStore_FailedCreateReleasesTheScopeItClaimed(t *testing.T) {
	ctx := context.Background()

	// The duplicate ID is the lever: it fails the RECORD write while leaving
	// the election — which is keyed by scope, not by account ID — free to win.
	const dupID = "acct-x"

	t.Run("a lost create does not hold the scope", func(t *testing.T) {
		store := NewInMemoryAccountStore()

		claude := newTestAccount("tenant-a", dupID, AccountProviderClaude)
		if err := store.Create(ctx, claude); err != nil {
			t.Fatalf("Create(claude) error = %v", err)
		}

		// Same ID, different provider: the codex scope is vacant, so the
		// election wins, and the record write then fails on the taken ID.
		codex := newTestAccount("tenant-a", dupID, AccountProviderCodex)
		err := store.Create(ctx, codex)
		if err == nil {
			t.Fatal("Create(codex) succeeded; the duplicate ID must fail the record write")
		}
		var invalid *AccountInvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("Create(codex) error = %v (%T), want *AccountInvalidError", err, err)
		}

		if holder := scopeDefaultPointer(t, store, "tenant-a", AccountProviderCodex, ""); holder != "" {
			t.Fatalf("codex scope default pointer = %q after a FAILED create, want it released", holder)
		}
	})

	t.Run("the next create still gets the scope", func(t *testing.T) {
		store := NewInMemoryAccountStore()

		if err := store.Create(ctx, newTestAccount("tenant-a", dupID, AccountProviderClaude)); err != nil {
			t.Fatalf("Create(claude) error = %v", err)
		}
		if err := store.Create(ctx, newTestAccount("tenant-a", dupID, AccountProviderCodex)); err == nil {
			t.Fatal("Create(codex) succeeded; the duplicate ID must fail the record write")
		}

		// The consequence a caller actually sees: the only account in the scope
		// must be its default.
		good := newTestAccount("tenant-a", "acct-good", AccountProviderCodex)
		if err := store.Create(ctx, good); err != nil {
			t.Fatalf("Create(good) error = %v", err)
		}
		if !good.Default {
			t.Errorf("Create(good).Default = false; the only account in the scope must be its default")
		}

		reread, err := store.Get(ctx, "tenant-a", "acct-good")
		if err != nil {
			t.Fatalf("Get(acct-good) error = %v", err)
		}
		if !reread.Default {
			t.Errorf("Get(acct-good).Default = false; a re-read must agree that it owns the scope")
		}
		if holder := scopeDefaultPointer(t, store, "tenant-a", AccountProviderCodex, ""); holder != "acct-good" {
			t.Errorf("codex scope default pointer = %q, want acct-good", holder)
		}
	})

	// The two subtests above fail on !stored, the path the issue reproduced. This
	// one fails on a STORE ERROR from the same write, because the release is a
	// defer covering every exit and that claim is only worth as much as a second
	// exit proves.
	t.Run("a store error on the record write releases too", func(t *testing.T) {
		kv := newProjectFailingKV(NewMemoryTaskKV(), projectVerbPut, accountKeyspace)
		store := NewAccountStore(kv)

		account := newTestAccount("tenant-a", "acct-doomed", AccountProviderCodex)
		if err := store.Create(ctx, account); err == nil {
			t.Fatal("Create() succeeded; the injected record-write failure must fail it")
		}

		if holder := scopeDefaultPointer(t, store, "tenant-a", AccountProviderCodex, ""); holder != "" {
			t.Fatalf("codex scope default pointer = %q after a failed record write, want it released", holder)
		}
	})

	// A release that cannot run must not swallow the reason the create failed.
	t.Run("a failed release is reported alongside the original error", func(t *testing.T) {
		kv := newProjectFailingKV(NewMemoryTaskKV(), projectVerbDelete, accountDefaultKeyspace)
		store := NewAccountStore(kv)

		if err := store.Create(ctx, newTestAccount("tenant-a", dupID, AccountProviderClaude)); err != nil {
			t.Fatalf("Create(claude) error = %v", err)
		}
		err := store.Create(ctx, newTestAccount("tenant-a", dupID, AccountProviderCodex))
		if err == nil {
			t.Fatal("Create(codex) succeeded; the duplicate ID must fail the record write")
		}

		var invalid *AccountInvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("errors.As(*AccountInvalidError) = false for %v; the caller must still see why its create failed", err)
		}
		var storeErr *AccountStoreError
		if !errors.As(err, &storeErr) {
			t.Errorf("errors.As(*AccountStoreError) = false for %v; a release that could not run must be reported", err)
		}
	})

	t.Run("a create that LOST the election releases nothing", func(t *testing.T) {
		store := NewInMemoryAccountStore()

		if err := store.Create(ctx, newTestAccount("tenant-a", "acct-first", AccountProviderClaude)); err != nil {
			t.Fatalf("Create(first) error = %v", err)
		}
		// Same scope as acct-first, and an ID already taken in it: the election
		// LOSES (the scope is held), and the record write fails. The release
		// must not touch a pointer this call never claimed.
		if err := store.Create(ctx, newTestAccount("tenant-a", "acct-first", AccountProviderClaude)); err == nil {
			t.Fatal("Create(duplicate) succeeded; the duplicate ID must fail the record write")
		}

		if holder := scopeDefaultPointer(t, store, "tenant-a", AccountProviderClaude, ""); holder != "acct-first" {
			t.Fatalf("claude scope default pointer = %q, want acct-first; a losing create must not release the holder's pointer", holder)
		}
	})
}
