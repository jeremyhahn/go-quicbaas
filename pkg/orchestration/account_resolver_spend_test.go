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
	stderrors "errors"
	"testing"
	"time"
)

// =============================================================================
// The in-process half of the cap
// =============================================================================
//
// The resolver is what an ACPBackend.Spawn calls to turn "give me an account"
// into an injectable credential. These assert that a metered account over its
// cap never gets that far, and — the half that is easy to leave out — that an
// account under its cap still resolves exactly as it did before the gate
// existed.

// gatedResolver builds a resolver whose spend authority decides from ledger.
func gatedResolver(t *testing.T, policy AccountSpendPolicy) (AccountResolver, ConsumptionLedger, AccountStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)

	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	authority, err := NewSpendAuthority(ledger, kv, spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	resolver := NewAccountResolver(store, tokens, NewAccountStrategyRegistry(nil), WithSpendAuthority(authority))

	ctx := context.Background()
	account := &Account{
		ID: "a1", TenantID: "t1", Provider: AccountProviderClaude, Spend: policy,
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := secretStore.Put(ctx, "t1", "a1", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-1",
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return resolver, ledger, store
}

// gatedRequest pins the seeded account.
func gatedRequest() AccountResolveRequest {
	return AccountResolveRequest{TenantID: "t1", Provider: AccountProviderClaude, AccountID: "a1"}
}

func TestAccountResolverReleasesACredentialUnderTheCap(t *testing.T) {
	resolver, ledger, _ := gatedResolver(t, AccountSpendPolicy{Quota: fiveHourTokens(1000)})
	seedRecentSpend(t, ledger, 100, "e1")

	resolved, err := resolver.Resolve(context.Background(), gatedRequest())
	if err != nil {
		t.Fatalf("Resolve under the cap = %v, want the credential", err)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-1" {
		t.Errorf("Env = %+v, want the account's token; the gate must not alter what an admitted spawn gets", resolved.Env)
	}
}

func TestAccountResolverRefusesACredentialOverTheCap(t *testing.T) {
	resolver, ledger, store := gatedResolver(t, AccountSpendPolicy{Quota: fiveHourTokens(1000)})
	seedRecentSpend(t, ledger, 1500, "e1")

	resolved, err := resolver.Resolve(context.Background(), gatedRequest())
	if !stderrors.Is(err, ErrAccountQuotaExhausted) {
		t.Fatalf("Resolve over the cap = %v, want a quota refusal", err)
	}
	if resolved != nil {
		t.Fatal("Resolve returned credential material alongside its refusal; a refused spawn must hold no secret")
	}

	// THE REFUSAL COMES BEFORE THE CREDENTIAL IS TOUCHED. LastUsedAt is
	// stamped by a successful resolution, so an untouched stamp is the
	// observable proof that no envelope was resolved on behalf of a spawn
	// that was going to be refused anyway.
	account, getErr := store.Get(context.Background(), "t1", "a1")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if !account.LastUsedAt.IsZero() {
		t.Error("the refused account was stamped as used; the cap is checked before the credential is resolved")
	}
}

func TestAccountResolverRefusesAnAccountWhoseBreakerTripped(t *testing.T) {
	resolver, ledger, _ := gatedResolver(t, AccountSpendPolicy{
		Breaker: &BreakerPolicy{Window: 24 * time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute},
	})
	now := time.Now().UTC()
	record(t, ledger, "f1", ConsumptionOutcomeFailure, now.Add(-2*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, now.Add(-time.Minute))

	_, err := resolver.Resolve(context.Background(), gatedRequest())
	if !stderrors.Is(err, ErrAccountBreakerOpen) {
		t.Fatalf("Resolve on a tripped breaker = %v, want ErrAccountBreakerOpen", err)
	}
}

func TestAccountResolverWithoutAnAuthorityIsUnmetered(t *testing.T) {
	// The pre-existing wiring — a deployment with no ledger. It must keep
	// resolving: adding a gate that fails closed when it is absent would
	// take every account offline on upgrade.
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{
		ID: "a1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: fiveHourTokens(1)},
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := secretStore.Put(ctx, "t1", "a1", &AccountCredential{AccessTokenEnvKey: "K", AccessToken: "v"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := resolver.Resolve(ctx, gatedRequest()); err != nil {
		t.Fatalf("Resolve with no authority wired = %v, want the credential", err)
	}
}

// seedRecentSpend records tokens against the fixture account just now, so a
// sliding window anchored on the real clock contains it.
func seedRecentSpend(t *testing.T, ledger ConsumptionLedger, tokens int64, id string) {
	t.Helper()
	entry := validEntry()
	entry.EntryID = id
	entry.TenantID = "t1"
	entry.AccountID = "a1"
	entry.InputTokens = tokens
	entry.OutputTokens = 0
	entry.CostMicros = tokens
	entry.OccurredAt = time.Now().UTC().Add(-time.Minute)
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}
