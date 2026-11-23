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

package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func providerAccountsRegistry(t *testing.T) *orchestration.AccountProviderRegistry {
	t.Helper()
	registry, err := orchestration.NewAccountProviderRegistry()
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	return registry
}

// tenantProviderAccounts returns tenantID's accounts of each frontier
// provider, keyed by provider.
func tenantProviderAccounts(t *testing.T, store orchestration.AccountStore, tenantID string) map[orchestration.AccountProvider][]string {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	got := map[orchestration.AccountProvider][]string{}
	for _, provider := range []orchestration.AccountProvider{orchestration.AccountProviderClaude, orchestration.AccountProviderCodex} {
		accounts, err := store.ListByTenant(ctx, tenantID, provider, "")
		if err != nil {
			t.Fatalf("ListByTenant(%s, %s) error = %v", tenantID, provider, err)
		}
		for _, account := range accounts {
			got[provider] = append(got[provider], account.ID)
		}
	}
	return got
}

func assertSeededDefaults(t *testing.T, store orchestration.AccountStore, tenantID string) {
	t.Helper()
	got := tenantProviderAccounts(t, store, tenantID)
	claude, codex := got[orchestration.AccountProviderClaude], got[orchestration.AccountProviderCodex]
	if len(claude) != 1 || claude[0] != "default-claude" || len(codex) != 1 || codex[0] != "default-codex" {
		t.Fatalf("tenant %s accounts = %v, want exactly default-claude and default-codex", tenantID, got)
	}
}

func TestSeedDefaultProviderAccounts_SystemTenantAtBoot(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
}

func TestSeedDefaultProviderAccounts_ReachesATenantUnsealedLater(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	s.seedDefaultProviderAccounts(context.Background(), store, providerAccountsRegistry(t))
	if got := tenantProviderAccounts(t, store, "acme"); len(got) != 0 {
		t.Fatalf("a tenant the boot pass does not reach has accounts %v", got)
	}

	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("unseal: %v", err)
	}
	assertSeededDefaults(t, store, "acme")
}

func TestSeedDefaultProviderAccounts_SecondBootCreatesNothing(t *testing.T) {
	s := agentsTestServer(t, nil)
	store := orchestration.NewInMemoryAccountStore()
	registry := providerAccountsRegistry(t)
	s.seedDefaultProviderAccounts(context.Background(), store, registry)
	s.seedDefaultProviderAccounts(context.Background(), store, registry)
	assertSeededDefaults(t, store, orchestration.SystemTenantID)
}
