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
// The policy is judged at the write
// =============================================================================
//
// By the time the gate reads a malformed policy the choice is between
// refusing an account the operator believes is capped and admitting one they
// believe is metered, and both are wrong. So the store refuses it, where the
// operator is still in the room.

func TestAccountStoreRoundTripsASpendPolicy(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()
	policy := AccountSpendPolicy{
		Quota: SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
			{Name: "weekly", Size: 7 * 24 * time.Hour, MaxTokens: 500_000},
		}},
		Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 10 * time.Minute},
	}

	if err := store.Create(ctx, &Account{
		ID: "a1", TenantID: "t1", Provider: AccountProviderClaude, Spend: policy,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, "t1", "a1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Spend.Quota.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 — a cap that does not survive a round trip is not a cap", len(got.Spend.Quota.Windows))
	}
	if got.Spend.Quota.Windows[0].MaxTokens != 1000 || got.Spend.Quota.Windows[1].MaxTokens != 500_000 {
		t.Errorf("windows = %+v, want the allowances as written", got.Spend.Quota.Windows)
	}
	if got.Spend.Breaker == nil || got.Spend.Breaker.FailureThreshold != 3 {
		t.Errorf("breaker = %+v, want the threshold as written", got.Spend.Breaker)
	}
}

func TestAccountStoreRefusesAMalformedSpendPolicyOnCreate(t *testing.T) {
	store := NewInMemoryAccountStore()
	err := store.Create(context.Background(), &Account{
		ID: "a1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour},
		}}},
	})
	if err == nil {
		t.Fatal("Create accepted a window metering neither axis; stored, it reads as a limit and silently is not one")
	}
	if !stderrors.Is(err, ErrQuotaInvalid) {
		t.Errorf("error = %v, want the quota authority's own refusal", err)
	}
}

func TestAccountStoreRefusesAMalformedSpendPolicyOnUpdate(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "a1", TenantID: "t1", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The update path needs its own guard: a policy added to an existing
	// account walks past a check written only where a create enters.
	account, err := store.Get(ctx, "t1", "a1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	account.Spend.Breaker = &BreakerPolicy{Window: time.Hour, FailureThreshold: 0, Cooldown: time.Minute}
	if updateErr := store.Update(ctx, account); updateErr == nil {
		t.Fatal("Update accepted a zero failure threshold; it trips an account that has never failed")
	} else if !stderrors.Is(updateErr, ErrBreakerInvalid) {
		t.Errorf("error = %v, want the breaker's own refusal", updateErr)
	}

	// And the stored record is untouched, so a refused write leaves no half
	// of the policy behind.
	stored, err := store.Get(ctx, "t1", "a1")
	if err != nil {
		t.Fatalf("Get after refusal: %v", err)
	}
	if stored.Spend.Breaker != nil {
		t.Error("the refused breaker policy was stored anyway")
	}
}
