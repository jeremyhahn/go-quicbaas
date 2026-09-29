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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Spend is a candidate filter (go-quicbaas#1058, ruling F6)
// =============================================================================

// TestSpendAuthorityCheckTakesNoTrial asserts Check admits a half-open
// breaker without claiming its single trial: a second holder can still win
// it afterwards.
func TestSpendAuthorityCheckTakesNoTrial(t *testing.T) {
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	checker, err := NewSpendAuthority(ledger, kv, "node-a")
	if err != nil {
		t.Fatalf("NewSpendAuthority(node-a): %v", err)
	}
	claimer, err := NewSpendAuthority(ledger, kv, "node-b")
	if err != nil {
		t.Fatalf("NewSpendAuthority(node-b): %v", err)
	}
	policy := AccountSpendPolicy{Breaker: &BreakerPolicy{Window: 4 * time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute}}
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Hour))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-90*time.Minute))
	req := SpendAuthorizeRequest{Account: spendAccount(policy)}

	if err := checker.Check(context.Background(), req, quotaNow); err != nil {
		t.Fatalf("Check on a half-open breaker = %v, want an admission", err)
	}
	if err := claimer.Authorize(context.Background(), req, quotaNow); err != nil {
		t.Fatalf("Authorize after a Check = %v; the Check must not have taken the trial", err)
	}
}

// TestSpendAuthorityCheckRefusesWhatAuthorizeRefuses asserts the two agree on
// a spent window and an open breaker.
func TestSpendAuthorityCheckRefusesWhatAuthorizeRefuses(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	spend(t, ledger, 1200, quotaNow.Add(-time.Hour), "e1")
	err := authority.Check(context.Background(), SpendAuthorizeRequest{
		Account: spendAccount(AccountSpendPolicy{Quota: fiveHourTokens(1000)}),
	}, quotaNow)
	if !stderrors.Is(err, ErrAccountQuotaExhausted) {
		t.Fatalf("Check over the cap = %v, want a quota refusal", err)
	}
	if err := authority.Check(context.Background(), SpendAuthorizeRequest{}, quotaNow); err == nil {
		t.Fatal("Check with no account = nil, want a configuration error")
	}
}

// poolResolver builds a resolver over two claude accounts, a1 and a2, each
// carrying its own policy, gated by a ledger-backed spend authority and
// balanced by least usage with a1 reported as the least used.
func poolResolver(t *testing.T, a1, a2 AccountSpendPolicy, strategies *AccountStrategyRegistry) (AccountResolver, ConsumptionLedger, AccountStore) {
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
	if strategies == nil {
		strategies, err = NewBalancedAccountStrategyRegistry(
			&fakeUsageLookup{tokens: map[string]int64{"a1": 0, "a2": 5000}}, &fakeSessionLookup{})
		if err != nil {
			t.Fatalf("NewBalancedAccountStrategyRegistry: %v", err)
		}
	}
	ctx := context.Background()
	for id, policy := range map[string]AccountSpendPolicy{"a1": a1, "a2": a2} {
		if err := store.Create(ctx, &Account{ID: id, TenantID: "t1", Provider: AccountProviderClaude, Spend: policy, Default: id == "a1"}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		if err := secretStore.Put(ctx, "t1", id, &AccountCredential{AccessTokenEnvKey: "K", AccessToken: "tok-" + id}); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	return NewAccountResolver(store, tokens, strategies, WithSpendAuthority(authority)), ledger, store
}

// spendOn records tokens against one account a minute ago.
func spendOn(t *testing.T, ledger ConsumptionLedger, accountID, entryID string, tokens int64) {
	t.Helper()
	entry := validEntry()
	entry.EntryID = entryID
	entry.AccountID = accountID
	entry.InputTokens = tokens
	entry.OutputTokens = 0
	entry.OccurredAt = time.Now().UTC().Add(-time.Minute)
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("record %s: %v", entryID, err)
	}
}

func autoRequest() AccountResolveRequest {
	return AccountResolveRequest{TenantID: "t1", Provider: AccountProviderClaude, Auto: true}
}

// TestAccountResolverLeastUsageFailsOverPastASpentAccount is the issue's
// verification: the least-used account is over its window, so least_usage
// skips it and the pool fails over to its sibling.
func TestAccountResolverLeastUsageFailsOverPastASpentAccount(t *testing.T) {
	resolver, ledger, _ := poolResolver(t, AccountSpendPolicy{Quota: fiveHourTokens(1000)}, AccountSpendPolicy{}, nil)
	spendOn(t, ledger, "a1", "e1", 1500)

	resolved, err := resolver.Resolve(context.Background(), autoRequest())
	if err != nil {
		t.Fatalf("Resolve = %v, want the sibling with headroom", err)
	}
	if resolved.AccountID != "a2" {
		t.Fatalf("resolved %s, want a2: the spent least-used account must be skipped", resolved.AccountID)
	}
}

// TestAccountResolverStaticFailsOverPastASpentDefault asserts the filter runs
// before any strategy: static selection's default is spent, so the one
// account left is chosen.
func TestAccountResolverStaticFailsOverPastASpentDefault(t *testing.T) {
	resolver, ledger, _ := poolResolver(t, AccountSpendPolicy{Quota: fiveHourTokens(1000)}, AccountSpendPolicy{},
		NewAccountStrategyRegistry(nil))
	spendOn(t, ledger, "a1", "e1", 1500)

	resolved, err := resolver.Resolve(context.Background(), autoRequest())
	if err != nil {
		t.Fatalf("Resolve = %v, want a2", err)
	}
	if resolved.AccountID != "a2" {
		t.Fatalf("resolved %s, want a2", resolved.AccountID)
	}
}

// TestAccountResolverSpentPoolIsATypedDeferral asserts a pool whose every
// account is spent says so, names when it frees up, and is classified as a
// deferral that never parks.
func TestAccountResolverSpentPoolIsATypedDeferral(t *testing.T) {
	capped := AccountSpendPolicy{Quota: fiveHourTokens(1000)}
	resolver, ledger, _ := poolResolver(t, capped, capped, nil)
	spendOn(t, ledger, "a1", "e1", 1500)
	spendOn(t, ledger, "a2", "e2", 1500)

	resolved, err := resolver.Resolve(context.Background(), autoRequest())
	if resolved != nil {
		t.Fatal("a spent pool released a credential")
	}
	var spent *AccountsSpentError
	if !stderrors.As(err, &spent) {
		t.Fatalf("Resolve = %v, want *AccountsSpentError", err)
	}
	if !stderrors.Is(err, ErrNoAccountAvailable) {
		t.Error("a spent pool must still read as no account available to every existing caller")
	}
	if spent.Candidates != 2 || spent.AvailableAt.IsZero() {
		t.Fatalf("spent = %+v, want two candidates and a return time", spent)
	}
	if until := time.Until(spent.AvailableAt); until < 4*time.Hour || until > 5*time.Hour {
		t.Fatalf("AvailableAt is %v away, want the five-hour window's oldest turn ageing out", until)
	}

	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAccountsSpent || !fault.RetryNotBefore.Equal(spent.AvailableAt) {
		t.Fatalf("fault = %+v, want %s deferred until %v", fault, FaultAccountsSpent, spent.AvailableAt)
	}
	outcome := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1}, fault)
	if !outcome.requeue || !outcome.refund || !outcome.keepsCount || outcome.bounded {
		t.Fatalf("outcome = %+v, want an uncharged requeue that never reaches the park bound", outcome)
	}
}

// TestAccountResolverAnswersWithTheEarlierReturn asserts a pool that is part
// spent and part cooling down is reported as whichever returns first.
func TestAccountResolverAnswersWithTheEarlierReturn(t *testing.T) {
	resolver, ledger, store := poolResolver(t, AccountSpendPolicy{Quota: fiveHourTokens(1000)}, AccountSpendPolicy{}, nil)
	spendOn(t, ledger, "a1", "e1", 1500)
	ctx := context.Background()
	held, err := store.Get(ctx, "t1", "a2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	held.Status = AccountStatusLimited
	held.AvailableAt = time.Now().UTC().Add(10 * time.Minute)
	if err := store.Update(ctx, held); err != nil {
		t.Fatalf("Update: %v", err)
	}

	_, err = resolver.Resolve(ctx, autoRequest())
	if !stderrors.Is(err, ErrAccountsCoolingDown) {
		t.Fatalf("Resolve = %v, want the cooldown, which returns in ten minutes rather than hours", err)
	}
}

// TestAccountResolverFailsClosedWhenSpendCannotBeRead asserts a ledger that
// cannot be read is an error, never a spent pool and never headroom.
func TestAccountResolverFailsClosedWhenSpendCannotBeRead(t *testing.T) {
	boom := stderrors.New("ledger unreadable")
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	authority, err := NewSpendAuthority(NewConsumptionLedger(&failingScanKV{err: boom}), NewMemoryTaskKV(), spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	if err := store.Create(context.Background(), &Account{ID: "a1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: fiveHourTokens(1000)}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	resolver := NewAccountResolver(store, NewAccountTokenManager(secretStore, store, time.Minute),
		NewAccountStrategyRegistry(nil), WithSpendAuthority(authority))

	_, err = resolver.Resolve(context.Background(), autoRequest())
	if err == nil || stderrors.Is(err, ErrNoAccountAvailable) {
		t.Fatalf("Resolve = %v, want the read failure, not a pool verdict", err)
	}
}

// raceSpend admits every candidate at Check and refuses the named accounts
// at Authorize, as a window crossed or a trial lost between the two reads.
type raceSpend struct {
	refuse map[string]error
}

func (r *raceSpend) Check(context.Context, SpendAuthorizeRequest, time.Time) error { return nil }

func (r *raceSpend) Authorize(_ context.Context, req SpendAuthorizeRequest, _ time.Time) error {
	return r.refuse[req.Account.ID]
}

// TestAccountResolverReselectsWhenTheGateRefusesTheSelection asserts a spend
// refusal of the selected account fails over to the next candidate, and that
// a pool refused whole at the gate is still a spent pool, not an empty one.
func TestAccountResolverReselectsWhenTheGateRefusesTheSelection(t *testing.T) {
	build := func(t *testing.T, gate SpendAuthority) AccountResolver {
		t.Helper()
		store := NewInMemoryAccountStore()
		secretStore := newTestAccountSecretStore(t)
		ctx := context.Background()
		for _, id := range []string{"a1", "a2"} {
			if err := store.Create(ctx, &Account{ID: id, TenantID: "t1", Provider: AccountProviderClaude}); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
			if err := secretStore.Put(ctx, "t1", id, &AccountCredential{AccessTokenEnvKey: "K", AccessToken: id}); err != nil {
				t.Fatalf("Put %s: %v", id, err)
			}
		}
		strategies, err := NewBalancedAccountStrategyRegistry(
			&fakeUsageLookup{tokens: map[string]int64{"a1": 0, "a2": 5000}}, &fakeSessionLookup{})
		if err != nil {
			t.Fatalf("strategies: %v", err)
		}
		return NewAccountResolver(store, NewAccountTokenManager(secretStore, store, time.Minute), strategies, WithSpendAuthority(gate))
	}

	t.Run("fails over", func(t *testing.T) {
		resolver := build(t, &raceSpend{refuse: map[string]error{
			"a1": &AccountBreakerTrialHeldError{AccountID: "a1", Failures: 3},
		}})
		resolved, err := resolver.Resolve(context.Background(), autoRequest())
		if err != nil || resolved.AccountID != "a2" {
			t.Fatalf("Resolve = (%v, %v), want a2", resolved, err)
		}
	})

	t.Run("every account refused", func(t *testing.T) {
		freesAt := time.Now().UTC().Add(2 * time.Hour)
		resolver := build(t, &raceSpend{refuse: map[string]error{
			"a1": &AccountBreakerTrialHeldError{AccountID: "a1"},
			"a2": &AccountQuotaExhaustedError{AccountID: "a2", RetryAt: freesAt},
		}})
		_, err := resolver.Resolve(context.Background(), autoRequest())
		var spent *AccountsSpentError
		if !stderrors.As(err, &spent) || spent.Candidates != 2 {
			t.Fatalf("Resolve = %v, want a spent pool of two", err)
		}
		// The held trial frees up within the probe's TTL, well before the
		// window; the deferral names the earlier.
		if !spent.AvailableAt.Before(freesAt) {
			t.Fatalf("AvailableAt = %v, want the trial's release before %v", spent.AvailableAt, freesAt)
		}
	})

	t.Run("a pinned account keeps its refusal", func(t *testing.T) {
		resolver := build(t, &raceSpend{refuse: map[string]error{
			"a1": &AccountQuotaExhaustedError{AccountID: "a1"},
		}})
		_, err := resolver.Resolve(context.Background(), AccountResolveRequest{TenantID: "t1", Provider: AccountProviderClaude, AccountID: "a1"})
		if !stderrors.Is(err, ErrAccountQuotaExhausted) {
			t.Fatalf("Resolve(pinned) = %v, want the account's own refusal", err)
		}
	})
}

// TestAccountsSpentCrossesTheSpawnWire asserts a remote daemon's spent pool
// reaches the control plane as the typed deferral with its return time.
func TestAccountsSpentCrossesTheSpawnWire(t *testing.T) {
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	local := &AccountsSpentError{TenantID: "t1", Candidates: 3, AvailableAt: at}
	if class := SpawnFaultClass(local); class != spawnFaultSpent {
		t.Fatalf("SpawnFaultClass = %q, want %q", class, spawnFaultSpent)
	}
	rebuilt := wrapRemoteSpawnFault("Spawn", AttachSpawnFaultEvidence(status.Error(codes.Unavailable, "pool spent"), local))
	var spent *AccountsSpentError
	if !stderrors.As(rebuilt, &spent) || spent.Candidates != 3 || !spent.AvailableAt.Equal(at) {
		t.Fatalf("rebuilt = %v, want the spent pool with its candidates and return time", rebuilt)
	}
	if exhaustion, ok := exhaustionFromResolveError(AccountResolveRequest{TenantID: "t1"}, local); !ok || exhaustion.Reason != AccountPoolSpent {
		t.Fatalf("exhaustion = %+v, want reason %q", exhaustion, AccountPoolSpent)
	}
}
