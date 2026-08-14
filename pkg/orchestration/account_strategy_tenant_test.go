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
	"testing"
	"time"
)

// =============================================================================
// LeastUsageAccountStrategy over the real, KV-backed ledger.
//
// The strategy-level tests in account_strategy_test.go use a tenant-blind fake
// lookup, so they cannot catch a ledger that leaks usage across tenants. These
// drive the production CostTracker instead: the same store, the same keys, two
// tenants naming the SAME account IDs.
// =============================================================================

func TestLeastUsageAccountStrategy_RealLedger_PicksLowestUsage(t *testing.T) {
	tracker := NewCostTracker(newCostTestKVStore(), &costTestLogger{})
	ctx := context.Background()

	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-a", Tokens: 50_000}); err != nil {
		t.Fatalf("RecordTurnUsage: %v", err)
	}
	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-b", Tokens: 10}); err != nil {
		t.Fatalf("RecordTurnUsage: %v", err)
	}

	strategy := LeastUsageAccountStrategy{Usage: tracker}
	got, err := strategy.Select(ctx, []*Account{{ID: "acct-a"}, {ID: "acct-b"}},
		AccountSelectionRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID != "acct-b" {
		t.Fatalf("Select() = %s, want acct-b (10 tokens vs 50000)", got.ID)
	}
}

// TestLeastUsageAccountStrategy_TenantIsolation is the isolation guarantee: one
// tenant's spend must be invisible to another tenant's selection even when both
// pools use identical account identifiers and share one physical store. A
// ledger keyed on account ID alone would hand tenant-b the "wrong" account here
// and, worse, disclose tenant-a's activity through the choice.
func TestLeastUsageAccountStrategy_TenantIsolation(t *testing.T) {
	tracker := NewCostTracker(newCostTestKVStore(), &costTestLogger{})
	ctx := context.Background()
	candidates := []*Account{{ID: "acct-a"}, {ID: "acct-b"}}

	// tenant-a has hammered acct-a. tenant-b has recorded nothing at all.
	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-a", Tokens: 1_000_000}); err != nil {
		t.Fatalf("RecordTurnUsage: %v", err)
	}

	strategy := LeastUsageAccountStrategy{Usage: tracker}

	forTenantA, err := strategy.Select(ctx, candidates, AccountSelectionRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("Select(tenant-a) error = %v", err)
	}
	if forTenantA.ID != "acct-b" {
		t.Fatalf("Select(tenant-a) = %s, want acct-b — its own acct-a is saturated", forTenantA.ID)
	}

	forTenantB, err := strategy.Select(ctx, candidates, AccountSelectionRequest{TenantID: "tenant-b"})
	if err != nil {
		t.Fatalf("Select(tenant-b) error = %v", err)
	}
	if forTenantB.ID != "acct-a" {
		t.Fatalf("Select(tenant-b) = %s, want acct-a (both at zero, stable tie-break by ID); tenant-a's usage must not steer tenant-b's selection", forTenantB.ID)
	}

	// And once tenant-b spends on acct-a, only tenant-b's own figure moves it.
	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-b", AccountID: "acct-a", Tokens: 5}); err != nil {
		t.Fatalf("RecordTurnUsage: %v", err)
	}
	forTenantB, err = strategy.Select(ctx, candidates, AccountSelectionRequest{TenantID: "tenant-b"})
	if err != nil {
		t.Fatalf("Select(tenant-b) error = %v", err)
	}
	if forTenantB.ID != "acct-b" {
		t.Fatalf("Select(tenant-b) = %s, want acct-b after tenant-b's own 5 tokens on acct-a", forTenantB.ID)
	}
}

// TestLeastUsageAccountStrategy_NoCandidates_Errors is the invalid-input case:
// an empty pool is refused rather than silently resolved to nothing.
func TestLeastUsageAccountStrategy_NoCandidates_Errors(t *testing.T) {
	strategy := LeastUsageAccountStrategy{Usage: NewCostTracker(newCostTestKVStore(), &costTestLogger{})}
	if _, err := strategy.Select(context.Background(), nil, AccountSelectionRequest{TenantID: "tenant-a"}); err == nil {
		t.Fatal("Select() with no candidates returned nil error, want NoAccountAvailableError")
	}
}

// =============================================================================
// End to end through AccountResolver: the seam a spawn actually calls.
// =============================================================================

// TestAccountResolver_LeastUsage_SelectsAcrossTenantsIndependently drives the
// whole path a spawn takes — registry lookup, candidate filtering, strategy,
// credential materialization — for two tenants whose pools share account IDs.
func TestAccountResolver_LeastUsage_SelectsAcrossTenantsIndependently(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	tracker := NewCostTracker(newCostTestKVStore(), &costTestLogger{})

	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		for _, id := range []string{"acct-a", "acct-b"} {
			if err := store.Create(ctx, &Account{ID: id, TenantID: tenant, Provider: AccountProviderClaude}); err != nil {
				t.Fatalf("Create(%s/%s): %v", tenant, id, err)
			}
			if err := secrets.Put(ctx, tenant, id, &AccountCredential{
				AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
				AccessToken:       "tok-" + tenant + "-" + id,
			}); err != nil {
				t.Fatalf("Put(%s/%s): %v", tenant, id, err)
			}
		}
	}

	// tenant-a saturates acct-a; tenant-b is untouched.
	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "tenant-a", AccountID: "acct-a", Tokens: 900_000}); err != nil {
		t.Fatalf("RecordTurnUsage: %v", err)
	}

	resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), NewAccountStrategyRegistry(tracker))

	resolvedA, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true, StrategyName: LeastUsageAccountStrategyName,
	})
	if err != nil {
		t.Fatalf("Resolve(tenant-a): %v", err)
	}
	if resolvedA.AccountID != "acct-b" {
		t.Fatalf("Resolve(tenant-a) = %s, want acct-b", resolvedA.AccountID)
	}
	if got := resolvedA.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "tok-tenant-a-acct-b" {
		t.Fatalf("resolved credential = %q, want tenant-a's own acct-b token", got)
	}

	resolvedB, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-b", Provider: AccountProviderClaude, Auto: true, StrategyName: LeastUsageAccountStrategyName,
	})
	if err != nil {
		t.Fatalf("Resolve(tenant-b): %v", err)
	}
	if resolvedB.AccountID != "acct-a" {
		t.Fatalf("Resolve(tenant-b) = %s, want acct-a — tenant-a's spend must not steer tenant-b", resolvedB.AccountID)
	}
	if got := resolvedB.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "tok-tenant-b-acct-a" {
		t.Fatalf("resolved credential = %q, want tenant-b's own acct-a token", got)
	}
}

// TestAccountResolver_UnregisteredStrategy_FailsClosed is the invalid-input
// case: naming a strategy that is not registered must fail rather than fall
// back to another policy, which would silently pick a different account than
// the operator asked for.
func TestAccountResolver_UnregisteredStrategy_FailsClosed(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)

	if err := store.Create(ctx, &Account{ID: "acct-a", TenantID: "tenant-a", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A registry built without a usage lookup does NOT register least_usage.
	resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), NewAccountStrategyRegistry(nil))

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true, StrategyName: LeastUsageAccountStrategyName,
	})
	if err == nil {
		t.Fatal("Resolve() with an unregistered strategy succeeded; it must fail closed rather than substitute another policy")
	}
}
