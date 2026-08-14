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
	"time"
)

func setupResolver(t *testing.T) (AccountResolver, AccountStore, *AccountSecretStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(nil)
	resolver := NewAccountResolver(store, tokens, strategies)
	return resolver, store, secretStore
}

func TestAccountResolver_PinnedAccountID(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-1",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" || resolved.Strategy != "pinned" {
		t.Fatalf("Resolve() = %+v, want account=acct-1 strategy=pinned", resolved)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-1" {
		t.Fatalf("Resolve().Env = %+v, want CLAUDE_CODE_OAUTH_TOKEN=tok-1", resolved.Env)
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt not recorded after Resolve()")
	}
}

func TestAccountResolver_PinnedAccount_WrongTenant_NotFound(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-b", Provider: AccountProviderClaude, AccountID: "acct-1"})
	if err == nil {
		t.Fatalf("Resolve() across tenants succeeded, want AccountNotFoundError")
	}
}

func TestAccountResolver_PinnedAccount_ProviderMismatch(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderCodex}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1"})
	var mismatch *AccountProviderMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountProviderMismatchError", err, err)
	}
}

func TestAccountResolver_PinnedAccount_Revoked(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusRevoked}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1"})
	var revoked *AccountRevokedError
	if !errors.As(err, &revoked) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRevokedError", err, err)
	}
}

func TestAccountResolver_Auto_UsesStaticStrategy(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	a := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	b := &Account{ID: "acct-2", TenantID: "tenant-a", Provider: AccountProviderClaude, Default: true}
	for _, acc := range []*Account{a, b} {
		if err := store.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s) error = %v", acc.ID, err)
		}
		if err := secretStore.Put(ctx, "tenant-a", acc.ID, &AccountCredential{AccessToken: "tok-" + acc.ID}); err != nil {
			t.Fatalf("Put(%s) error = %v", acc.ID, err)
		}
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-2" {
		t.Fatalf("Resolve() picked %s, want acct-2 (the Default)", resolved.AccountID)
	}
	if resolved.Strategy != StaticAccountStrategyName {
		t.Fatalf("Resolve().Strategy = %s, want %s", resolved.Strategy, StaticAccountStrategyName)
	}
}

func TestAccountResolver_Auto_ExcludesRevokedAndErrored(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	revoked := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusRevoked}
	errored := &Account{ID: "acct-2", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusError}
	active := &Account{ID: "acct-3", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusActive}
	for _, acc := range []*Account{revoked, errored, active} {
		if err := store.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s) error = %v", acc.ID, err)
		}
		_ = secretStore.Put(ctx, "tenant-a", acc.ID, &AccountCredential{AccessToken: "tok"})
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-3" {
		t.Fatalf("Resolve() picked %s, want acct-3 (only active candidate)", resolved.AccountID)
	}
}

func TestAccountResolver_Auto_NoCandidates(t *testing.T) {
	resolver, _, _ := setupResolver(t)
	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	var notAvailable *NoAccountAvailableError
	if !errors.As(err, &notAvailable) {
		t.Fatalf("Resolve() error = %v (%T), want *NoAccountAvailableError", err, err)
	}
}

func TestAccountResolver_NeitherPinnedNorAuto_Errors(t *testing.T) {
	resolver, _, _ := setupResolver(t)
	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude})
	if err == nil {
		t.Fatalf("Resolve() error = nil, want an error when neither AccountID nor Auto is set")
	}
}

func TestAccountResolver_Auto_ProjectScopedFallsBackToTenantWide(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	tenantWide := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, tenantWide); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "tok"})

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-x", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" {
		t.Fatalf("Resolve() = %s, want acct-1 (tenant-wide fallback)", resolved.AccountID)
	}
}
