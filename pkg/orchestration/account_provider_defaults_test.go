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
	"sync"
	"testing"
	"time"
)

func seedTestRegistry(t *testing.T) *AccountProviderRegistry {
	t.Helper()
	registry, err := NewAccountProviderRegistry()
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	return registry
}

func seedTestContext() context.Context {
	return ContextWithTenant(context.Background(), "tenant-a")
}

func TestSeedDefaultProviderAccounts_CreatesOnePerFrontierProvider(t *testing.T) {
	ctx := seedTestContext()
	store := NewInMemoryAccountStore()
	created, err := SeedDefaultProviderAccounts(ctx, store, seedTestRegistry(t), "tenant-a")
	if err != nil {
		t.Fatalf("SeedDefaultProviderAccounts() error = %v", err)
	}
	if len(created) != 2 || created[0].ID != "default-claude" || created[1].ID != "default-codex" {
		t.Fatalf("created = %v, want default-claude and default-codex", accountIDs(created))
	}
	for _, id := range []string{"default-claude", "default-codex"} {
		got, getErr := store.Get(ctx, "tenant-a", id)
		if getErr != nil {
			t.Fatalf("Get(%s) error = %v", id, getErr)
		}
		if got.Status != AccountStatusAwaitingCredential || !got.Default || got.ProjectID != "" {
			t.Fatalf("%s = status %q default %v project %q, want awaiting_credential, default, tenant-wide",
				id, got.Status, got.Default, got.ProjectID)
		}
		if got.RefreshPolicy != AccountCredentialModeStaticToken {
			t.Fatalf("%s RefreshPolicy = %q, want static_token", id, got.RefreshPolicy)
		}
	}
	opencode, err := store.ListByTenant(ctx, "tenant-a", AccountProviderOpenCode, "")
	if err != nil {
		t.Fatalf("ListByTenant(opencode) error = %v", err)
	}
	if len(opencode) != 0 {
		t.Fatalf("opencode accounts = %v, want none", accountIDs(opencode))
	}
}

func TestSeedDefaultProviderAccounts_IsIdempotent(t *testing.T) {
	ctx := seedTestContext()
	store := NewInMemoryAccountStore()
	registry := seedTestRegistry(t)
	if _, err := SeedDefaultProviderAccounts(ctx, store, registry, "tenant-a"); err != nil {
		t.Fatalf("first seed error = %v", err)
	}
	created, err := SeedDefaultProviderAccounts(ctx, store, registry, "tenant-a")
	if err != nil {
		t.Fatalf("second seed error = %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("second seed created %v, want nothing", accountIDs(created))
	}
	assertOneAccountPerFrontierProvider(t, store, "tenant-a")
}

func TestSeedDefaultProviderAccounts_SkipsAProviderThatHasAnAccount(t *testing.T) {
	ctx := seedTestContext()
	store := NewInMemoryAccountStore()
	if err := store.Create(ctx, &Account{
		ID: "mine", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusActive,
	}); err != nil {
		t.Fatalf("Create(mine) error = %v", err)
	}
	created, err := SeedDefaultProviderAccounts(ctx, store, seedTestRegistry(t), "tenant-a")
	if err != nil {
		t.Fatalf("SeedDefaultProviderAccounts() error = %v", err)
	}
	if len(created) != 1 || created[0].ID != "default-codex" {
		t.Fatalf("created = %v, want only default-codex", accountIDs(created))
	}
	mine, err := store.Get(ctx, "tenant-a", "mine")
	if err != nil {
		t.Fatalf("Get(mine) error = %v", err)
	}
	if !mine.Default {
		t.Fatal("mine lost the claude default to the seeder")
	}
}

func TestSeedDefaultProviderAccounts_NeverSelected(t *testing.T) {
	resolver, store, _ := setupResolver(t)
	ctx := seedTestContext()
	if _, err := SeedDefaultProviderAccounts(ctx, store, seedTestRegistry(t), "tenant-a"); err != nil {
		t.Fatalf("SeedDefaultProviderAccounts() error = %v", err)
	}
	seeded, err := store.Get(ctx, "tenant-a", "default-claude")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if seeded.Selectable(time.Now()) {
		t.Fatal("a seeded account awaiting its credential is selectable")
	}
	if _, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude,
	}); err == nil {
		t.Fatal("Resolve() with no pin succeeded on a pool of only an awaiting account")
	}
	_, err = resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "default-claude",
	})
	if !errors.Is(err, ErrAccountAwaitingCredential) {
		t.Fatalf("pinned Resolve() error = %v, want ErrAccountAwaitingCredential", err)
	}
}

func TestSeedDefaultProviderAccounts_RejectsBadInput(t *testing.T) {
	ctx := seedTestContext()
	registry := seedTestRegistry(t)
	store := NewInMemoryAccountStore()
	cases := map[string]struct {
		store     AccountStore
		providers *AccountProviderRegistry
		tenantID  string
	}{
		"nil store":    {nil, registry, "tenant-a"},
		"nil registry": {store, nil, "tenant-a"},
		"empty tenant": {store, registry, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := SeedDefaultProviderAccounts(ctx, tc.store, tc.providers, tc.tenantID)
			var invalid *AccountInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v (%T), want *AccountInvalidError", err, err)
			}
		})
	}
}

func TestSeedDefaultProviderAccounts_ConcurrentSeedersCreateOnce(t *testing.T) {
	ctx := seedTestContext()
	store := NewInMemoryAccountStore()
	registry := seedTestRegistry(t)
	const seeders = 8
	errs := make([]error, seeders)
	var wg sync.WaitGroup
	for i := range seeders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = SeedDefaultProviderAccounts(ctx, store, registry, "tenant-a")
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("seeder %d error = %v", i, err)
		}
	}
	assertOneAccountPerFrontierProvider(t, store, "tenant-a")
}

func assertOneAccountPerFrontierProvider(t *testing.T, store AccountStore, tenantID string) {
	t.Helper()
	ctx := ContextWithTenant(context.Background(), tenantID)
	for _, provider := range []AccountProvider{AccountProviderClaude, AccountProviderCodex} {
		accounts, err := store.ListByTenant(ctx, tenantID, provider, "")
		if err != nil {
			t.Fatalf("ListByTenant(%s) error = %v", provider, err)
		}
		if len(accounts) != 1 {
			t.Fatalf("%s accounts = %v, want exactly one", provider, accountIDs(accounts))
		}
	}
}

func accountIDs(accounts []*Account) []string {
	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	return ids
}
