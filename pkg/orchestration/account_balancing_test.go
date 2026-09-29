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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Doubles
// =============================================================================

// fakeSessionLookup answers live-session counts from a map, or fails.
type fakeSessionLookup struct {
	live map[string]int
	err  error
}

func (f *fakeSessionLookup) LiveSessions(_ context.Context, _ string, accountIDs []string) (map[string]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]int, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = f.live[id]
	}
	return out, nil
}

// fakeSessionLister returns fixed session records, or fails.
type fakeSessionLister struct {
	records []*SessionRecord
	err     error
}

func (f *fakeSessionLister) ListSessions(context.Context, string) ([]*SessionRecord, error) {
	return f.records, f.err
}

// balancedPool is the burn-in shape: three equivalent Claude accounts, the
// last of them the scope's default.
func balancedPool() []*Account {
	return []*Account{
		{ID: "claude1", TenantID: "t", Provider: AccountProviderClaude, Status: AccountStatusActive},
		{ID: "claude2", TenantID: "t", Provider: AccountProviderClaude, Status: AccountStatusActive},
		{ID: "claude3", TenantID: "t", Provider: AccountProviderClaude, Status: AccountStatusActive, Default: true},
	}
}

func balancedSelect(t *testing.T, s LeastUsageAccountStrategy, pool []*Account) *Account {
	t.Helper()
	got, err := s.Select(context.Background(), pool, AccountSelectionRequest{TenantID: "t", Provider: AccountProviderClaude})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	return got
}

// =============================================================================
// Selection spreads load
// =============================================================================

// TestLeastUsage_SpreadsConcurrentSessionsAcrossThePool is the burn-in
// regression: three sessions started back to back must land on three
// different accounts, not three times on the default.
func TestLeastUsage_SpreadsConcurrentSessionsAcrossThePool(t *testing.T) {
	sessions := &fakeSessionLookup{live: map[string]int{}}
	strategy := LeastUsageAccountStrategy{Usage: &fakeUsageLookup{tokens: map[string]int64{}}, Sessions: sessions}
	pool := balancedPool()

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		chosen := balancedSelect(t, strategy, pool)
		if seen[chosen.ID] {
			t.Fatalf("session %d landed on %s again while another account was idle", i+1, chosen.ID)
		}
		seen[chosen.ID] = true
		sessions.live[chosen.ID]++
	}

	// With all three busy the least busy wins again, and the default breaks
	// the tie among equals.
	if got := balancedSelect(t, strategy, pool); got.ID != "claude3" {
		t.Fatalf("a fourth session chose %s, want the default among equally busy accounts", got.ID)
	}
}

func TestLeastUsage_SpreadsSerialSessionsByRecordedUsage(t *testing.T) {
	usage := &fakeUsageLookup{tokens: map[string]int64{}}
	strategy := LeastUsageAccountStrategy{Usage: usage, Sessions: &fakeSessionLookup{live: map[string]int{}}}
	pool := balancedPool()

	// One session at a time, each charging the account it ran on.
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		chosen := balancedSelect(t, strategy, pool)
		if seen[chosen.ID] {
			t.Fatalf("serial session %d reused %s while another account had recorded nothing", i+1, chosen.ID)
		}
		seen[chosen.ID] = true
		usage.tokens[chosen.ID] += 1000
	}
}

func TestLeastUsage_TheDefaultIsPreferredOnlyAmongEquals(t *testing.T) {
	cases := []struct {
		name   string
		live   map[string]int
		tokens map[string]int64
		used   map[string]time.Time
		want   string
	}{
		{name: "everything equal: the default", want: "claude3"},
		{name: "the default is busier", live: map[string]int{"claude3": 1}, want: "claude1"},
		{name: "the default has used more", tokens: map[string]int64{"claude3": 10}, want: "claude1"},
		{
			name: "equal usage, no default among the idle: least recently used",
			live: map[string]int{"claude3": 1},
			used: map[string]time.Time{"claude1": time.Unix(200, 0), "claude2": time.Unix(100, 0)},
			want: "claude2",
		},
		{
			name: "fewer live sessions outranks lower usage",
			live: map[string]int{"claude1": 1, "claude3": 1}, tokens: map[string]int64{"claude2": 1_000_000},
			want: "claude2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := balancedPool()
			for _, a := range pool {
				a.LastUsedAt = tc.used[a.ID]
			}
			strategy := LeastUsageAccountStrategy{
				Usage:    &fakeUsageLookup{tokens: tc.tokens},
				Sessions: &fakeSessionLookup{live: tc.live},
			}
			if got := balancedSelect(t, strategy, pool); got.ID != tc.want {
				t.Fatalf("chose %s, want %s", got.ID, tc.want)
			}
		})
	}
}

// =============================================================================
// The session cap
// =============================================================================

func TestLeastUsage_SkipsAnAccountAtItsCap(t *testing.T) {
	pool := balancedPool()
	pool[0].MaxConcurrentSessions = 1 // claude1: least used, but full
	strategy := LeastUsageAccountStrategy{
		Usage:    &fakeUsageLookup{tokens: map[string]int64{"claude2": 50, "claude3": 90}},
		Sessions: &fakeSessionLookup{live: map[string]int{"claude1": 1, "claude2": 1, "claude3": 1}},
	}
	if got := balancedSelect(t, strategy, pool); got.ID != "claude2" {
		t.Fatalf("chose %s, want claude2 — claude1 is at its cap", got.ID)
	}
}

func TestLeastUsage_AnUnmeteredAccountIsNeverCapped(t *testing.T) {
	pool := balancedPool()[:1]
	strategy := LeastUsageAccountStrategy{
		Usage:    &fakeUsageLookup{},
		Sessions: &fakeSessionLookup{live: map[string]int{"claude1": 40}},
	}
	if got := balancedSelect(t, strategy, pool); got.ID != "claude1" {
		t.Fatalf("chose %s, want claude1", got.ID)
	}
}

func TestLeastUsage_AFullPoolIsSaturatedNotEmpty(t *testing.T) {
	pool := balancedPool()
	for _, a := range pool {
		a.MaxConcurrentSessions = 2
	}
	strategy := LeastUsageAccountStrategy{
		Usage:    &fakeUsageLookup{},
		Sessions: &fakeSessionLookup{live: map[string]int{"claude1": 2, "claude2": 2, "claude3": 3}},
	}
	_, err := strategy.Select(context.Background(), pool, AccountSelectionRequest{TenantID: "t", Provider: AccountProviderClaude})
	var saturated *AccountsSaturatedError
	if !errors.As(err, &saturated) {
		t.Fatalf("err = %v, want AccountsSaturatedError", err)
	}
	if saturated.Candidates != 3 {
		t.Errorf("Candidates = %d, want 3", saturated.Candidates)
	}
	if !strings.Contains(saturated.Occupancy, "claude3 3/2") {
		t.Errorf("Occupancy = %q, want it to name each account's load", saturated.Occupancy)
	}
	if !errors.Is(err, ErrAccountsSaturated) || !errors.Is(err, ErrNoAccountAvailable) {
		t.Errorf("errors.Is must match both sentinels: %v", err)
	}
	if errors.Is(err, ErrAccountsCoolingDown) {
		t.Errorf("a saturated pool is not a cooling one: %v", err)
	}
}

func TestLeastUsage_AnUnreadableSessionCountFailsClosed(t *testing.T) {
	boom := errors.New("store down")
	strategy := LeastUsageAccountStrategy{Usage: &fakeUsageLookup{}, Sessions: &fakeSessionLookup{err: boom}}
	got, err := strategy.Select(context.Background(), balancedPool(), AccountSelectionRequest{TenantID: "t"})
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("Select = (%v, %v), want the lookup failure and no account", got, err)
	}
}

func TestLeastUsage_NoSessionLookupRanksOnUsageAlone(t *testing.T) {
	strategy := LeastUsageAccountStrategy{Usage: &fakeUsageLookup{tokens: map[string]int64{"claude1": 5, "claude3": 5}}}
	if got := balancedSelect(t, strategy, balancedPool()); got.ID != "claude2" {
		t.Fatalf("chose %s, want claude2 (fewest tokens)", got.ID)
	}
}

// =============================================================================
// The registry
// =============================================================================

func TestNewBalancedAccountStrategyRegistry_DefaultsToBalancing(t *testing.T) {
	registry, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, &fakeSessionLookup{})
	if err != nil {
		t.Fatalf("NewBalancedAccountStrategyRegistry: %v", err)
	}
	got, ok := registry.Get("")
	if !ok || got.Name() != LeastUsageAccountStrategyName {
		t.Fatalf("default strategy = %v, want least_usage", got)
	}
	if _, ok := registry.Get(StaticAccountStrategyName); !ok {
		t.Fatal("static must stay registered for a caller that names it")
	}
}

func TestNewBalancedAccountStrategyRegistry_RequiresBothLookups(t *testing.T) {
	if _, err := NewBalancedAccountStrategyRegistry(nil, &fakeSessionLookup{}); !errors.Is(err, ErrAccountInvalid) {
		t.Errorf("nil usage: err = %v, want ErrAccountInvalid", err)
	}
	if _, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, nil); !errors.Is(err, ErrAccountInvalid) {
		t.Errorf("nil sessions: err = %v, want ErrAccountInvalid", err)
	}
}

// TestAccountResolver_BalancedDefaultIsNotStickyOnTheDefaultAccount runs the
// whole resolver, as the server wires it, over three real accounts.
func TestAccountResolver_BalancedDefaultIsNotStickyOnTheDefaultAccount(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	for _, id := range []string{"claude3", "claude1", "claude2"} {
		seedPoolAccount(t, store, secrets, &Account{ID: id, TenantID: "tenant-a", Provider: AccountProviderClaude})
	}
	sessions := &fakeSessionLookup{live: map[string]int{}}
	registry, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, sessions)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), registry)

	chosen := map[string]bool{}
	for i := 0; i < 3; i++ {
		resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
			TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		})
		if err != nil {
			t.Fatalf("Resolve %d: %v", i+1, err)
		}
		if resolved.Strategy != LeastUsageAccountStrategyName {
			t.Errorf("Strategy = %q, want least_usage", resolved.Strategy)
		}
		chosen[resolved.AccountID] = true
		sessions.live[resolved.AccountID]++
	}
	if len(chosen) != 3 {
		t.Fatalf("three sessions used %v; every account in the pool should have been selected", chosen)
	}
}

// =============================================================================
// Recovery once a hold lapses
// =============================================================================

func TestAccountResolver_ALimitedAccountRejoinsBalancingWhenItsHoldLapses(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	seedPoolAccount(t, store, secrets, &Account{ID: "claude1", TenantID: "tenant-a", Provider: AccountProviderClaude})
	seedPoolAccount(t, store, secrets, &Account{ID: "claude2", TenantID: "tenant-a", Provider: AccountProviderClaude})
	registry, err := NewBalancedAccountStrategyRegistry(
		&fakeUsageLookup{tokens: map[string]int64{"claude2": 500}}, &fakeSessionLookup{})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), registry)
	resolve := func() string {
		t.Helper()
		resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
			TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		return resolved.AccountID
	}

	// claude1 is the least used, but the provider refused it until later.
	holdAccountUntil(t, store, "tenant-a", "claude1", time.Now().UTC().Add(time.Hour))
	if got := resolve(); got != "claude2" {
		t.Fatalf("while held: chose %s, want claude2", got)
	}

	// The hold's moment has passed: the account is balanced on again.
	holdAccountUntil(t, store, "tenant-a", "claude1", time.Now().UTC().Add(-time.Second))
	if got := resolve(); got != "claude1" {
		t.Fatalf("after the hold lapsed: chose %s, want claude1 back in the pool", got)
	}
}

// =============================================================================
// Expiry buffer
// =============================================================================

func TestAccount_AcceptsNewSession(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		account Account
		want    bool
	}{
		{"no expiry recorded", Account{Status: AccountStatusActive, RefreshPolicy: AccountCredentialModeStaticToken}, true},
		{"static token well before expiry", Account{Status: AccountStatusActive, RefreshPolicy: AccountCredentialModeStaticToken,
			CredentialExpiresAt: now.Add(time.Hour)}, true},
		{"static token inside the buffer", Account{Status: AccountStatusActive, RefreshPolicy: AccountCredentialModeStaticToken,
			CredentialExpiresAt: now.Add(90 * time.Second)}, false},
		{"static token already expired", Account{Status: AccountStatusActive, RefreshPolicy: AccountCredentialModeStaticToken,
			CredentialExpiresAt: now.Add(-time.Minute)}, false},
		{"oauth account inside the buffer is renewed at resolve", Account{Status: AccountStatusActive,
			RefreshPolicy: AccountCredentialModeOAuthRefresh, CredentialExpiresAt: now.Add(30 * time.Second)}, true},
		{"a held account", Account{Status: AccountStatusLimited, AvailableAt: now.Add(time.Hour)}, false},
		{"a revoked account", Account{Status: AccountStatusRevoked}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.account.AcceptsNewSession(now); got != tc.want {
				t.Fatalf("AcceptsNewSession = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAccountResolver_ExpiryBufferAppliesToNewSessionsOnly(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	expiring := &Account{
		ID: "acct-expiring", TenantID: "tenant-a", Provider: AccountProviderClaude,
		RefreshPolicy: AccountCredentialModeStaticToken, CredentialExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	seedPoolAccount(t, store, secrets, expiring)
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-fresh", TenantID: "tenant-a", Provider: AccountProviderClaude})

	ctx := context.Background()
	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	if err != nil {
		t.Fatalf("Resolve(auto): %v", err)
	}
	if resolved.AccountID != "acct-fresh" {
		t.Fatalf("a new session got %s, want acct-fresh — the other expires inside the buffer", resolved.AccountID)
	}

	// A pinned NEW session is held to the buffer as well: naming the account
	// does not make a lapsing credential safe to start on.
	_, err = resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", AccountID: "acct-expiring"})
	var expiringErr *AccountCredentialExpiringError
	if !errors.As(err, &expiringErr) || expiringErr.AccountID != "acct-expiring" {
		t.Fatalf("Resolve(pinned new) err = %v, want AccountCredentialExpiringError for acct-expiring", err)
	}

	// Only a resume, which reconstitutes a session already on the account,
	// is exempt.
	resumed, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", AccountID: "acct-expiring", ReplacesSessionID: "sess-1",
	})
	if err != nil || resumed.AccountID != "acct-expiring" {
		t.Fatalf("Resolve(resume) = (%v, %v), want acct-expiring", resumed, err)
	}
}

// =============================================================================
// The cap is validated at the write
// =============================================================================

func TestAccountStore_RejectsANegativeSessionCap(t *testing.T) {
	store := NewInMemoryAccountStore()
	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.MaxConcurrentSessions = -1
	if err := store.Create(context.Background(), account); !errors.Is(err, ErrAccountInvalid) {
		t.Fatalf("Create: err = %v, want ErrAccountInvalid", err)
	}

	account.MaxConcurrentSessions = 2
	if err := store.Create(context.Background(), account); err != nil {
		t.Fatalf("Create with a cap of 2: %v", err)
	}
	account.MaxConcurrentSessions = -3
	if err := store.Update(context.Background(), account); !errors.Is(err, ErrAccountInvalid) {
		t.Fatalf("Update: err = %v, want ErrAccountInvalid", err)
	}
	got, err := store.Get(context.Background(), "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MaxConcurrentSessions != 2 {
		t.Fatalf("MaxConcurrentSessions = %d, want the stored 2", got.MaxConcurrentSessions)
	}
}

// =============================================================================
// The session counter
// =============================================================================

func TestAccountSessionCounter_CountsOnlyLiveSessionsOfTheAskedAccounts(t *testing.T) {
	lister := &fakeSessionLister{records: []*SessionRecord{
		{SessionID: "s1", AccountID: "claude1", Status: SessionStateActive},
		{SessionID: "s2", AccountID: "claude1", Status: SessionStateIdle},
		{SessionID: "s3", AccountID: "claude1", Status: SessionStateClosed},
		{SessionID: "s4", AccountID: "claude1", Status: SessionStateError},
		{SessionID: "s5", AccountID: "claude1", Status: SessionStateResumable},
		{SessionID: "s6", AccountID: "claude2", Status: SessionStateActive},
		{SessionID: "s7", AccountID: "other", Status: SessionStateActive},
		{SessionID: "s8", Status: SessionStateActive},
		nil,
	}}
	counter, err := NewAccountSessionCounter(lister)
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	live, err := counter.LiveSessions(context.Background(), "tenant-a", []string{"claude1", "claude2", "claude3"})
	if err != nil {
		t.Fatalf("LiveSessions: %v", err)
	}
	want := map[string]int{"claude1": 2, "claude2": 1}
	for id, n := range want {
		if live[id] != n {
			t.Errorf("live[%s] = %d, want %d", id, live[id], n)
		}
	}
	if _, counted := live["other"]; counted {
		t.Error("an account nobody asked about must not be counted")
	}
	if live["claude3"] != 0 {
		t.Errorf("live[claude3] = %d, want 0", live["claude3"])
	}
}

func TestAccountSessionCounter_ReportsAnUnreadableStore(t *testing.T) {
	boom := errors.New("kv down")
	counter, err := NewAccountSessionCounter(&fakeSessionLister{err: boom})
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	_, err = counter.LiveSessions(context.Background(), "tenant-a", []string{"claude1"})
	var countErr *AccountSessionCountError
	if !errors.As(err, &countErr) || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want AccountSessionCountError wrapping the store failure", err)
	}
	if !strings.Contains(err.Error(), "tenant-a") {
		t.Errorf("error %q should name the tenant", err)
	}
}

func TestAccountSessionCounter_AnEmptyQuestionReadsNothing(t *testing.T) {
	counter, err := NewAccountSessionCounter(&fakeSessionLister{err: errors.New("must not be read")})
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	live, err := counter.LiveSessions(context.Background(), "tenant-a", nil)
	if err != nil || len(live) != 0 {
		t.Fatalf("LiveSessions(nil) = (%v, %v), want an empty count and no read", live, err)
	}
}

func TestNewAccountSessionCounter_RequiresALister(t *testing.T) {
	if _, err := NewAccountSessionCounter(nil); !errors.Is(err, ErrAccountInvalid) {
		t.Fatalf("err = %v, want ErrAccountInvalid", err)
	}
}

// TestAccountSessionCounter_ReadsTheRealSessionStore proves the counter over
// the production AgentStore, not only over a double of it.
func TestAccountSessionCounter_ReadsTheRealSessionStore(t *testing.T) {
	store := NewAgentStore(kvstore.NewMemory())
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	for _, r := range []*SessionRecord{
		{SessionID: "s1", TenantID: "tenant-a", Backend: "claude", AccountID: "claude1", Status: SessionStateActive},
		{SessionID: "s2", TenantID: "tenant-a", Backend: "claude", AccountID: "claude1", Status: SessionStateClosed},
	} {
		if err := store.PutSession(ctx, r); err != nil {
			t.Fatalf("PutSession(%s): %v", r.SessionID, err)
		}
	}
	counter, err := NewAccountSessionCounter(store)
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	live, err := counter.LiveSessions(context.Background(), "tenant-a", []string{"claude1"})
	if err != nil {
		t.Fatalf("LiveSessions: %v", err)
	}
	if live["claude1"] != 1 {
		t.Fatalf("live[claude1] = %d, want 1 (one active, one closed)", live["claude1"])
	}
}

func TestAccountCredentialExpiringError_NamesTheAccountAndUnwraps(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	err := error(&AccountCredentialExpiringError{AccountID: "acct-1", ExpiresAt: at})
	if !errors.Is(err, ErrAccountCredentialExpiring) {
		t.Fatalf("errors.Is(%v, ErrAccountCredentialExpiring) = false", err)
	}
	if errors.Is(err, ErrNoAccountAvailable) {
		t.Fatal("a pinned refusal is not an empty pool")
	}
	want := ErrAccountCredentialExpiring.Error() + ": id=acct-1 expires_at=2026-09-23T12:00:00Z"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

// TestAccountStore_RejectsACapTheWireCannotCarry proves the stored bound
// matches the gRPC int32 field: a larger cap would read back as a different
// number, or as zero, which means unmetered.
func TestAccountStore_RejectsACapTheWireCannotCarry(t *testing.T) {
	store := NewInMemoryAccountStore()
	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.MaxConcurrentSessions = MaxAccountSessionCap + 1
	if err := store.Create(context.Background(), account); !errors.Is(err, ErrAccountInvalid) {
		t.Fatalf("Create: err = %v, want ErrAccountInvalid", err)
	}
	account.MaxConcurrentSessions = MaxAccountSessionCap
	if err := store.Create(context.Background(), account); err != nil {
		t.Fatalf("Create at the bound: %v", err)
	}
}
