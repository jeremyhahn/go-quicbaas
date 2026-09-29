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
	"math"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Ranged roll-ups (go-quicbaas#1058, rulings F2 and F4)
// =============================================================================

// TestConsumptionLedgerRangedRollup asserts a range narrows every scope to
// the entries that occurred in [From, To), and that the drill-down rows of the
// two plane-served scopes match what the chain would have attributed.
//
// seedRollupFixture puts e1..e6 in tenant t1 at ledgerBase + 0..5 minutes.
func TestConsumptionLedgerRangedRollup(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)
	from, to := ledgerBase.Add(time.Minute), ledgerBase.Add(5*time.Minute) // e2..e5

	tests := []struct {
		name       string
		scope      ConsumptionScope
		wantTokens int64
		wantChild  map[string]int64
		wantFirst  time.Time
	}{
		{
			name:       "tenant, children are projects",
			scope:      ConsumptionScope{TenantID: "t1"},
			wantTokens: 10 + 30 + 100 + 1000,
			wantChild:  map[string]int64{"p1": 10 + 30 + 100 + 1000},
			wantFirst:  from,
		},
		{
			name:       "project, children are tasks",
			scope:      ConsumptionScope{TenantID: "t1", ProjectID: "p1"},
			wantTokens: 10 + 30 + 100 + 1000,
			wantChild:  map[string]int64{"w1": 10 + 30 + 100, "w2": 1000},
			wantFirst:  from,
		},
		{
			name:       "job, filtered from the chain",
			scope:      ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1"},
			wantTokens: 10 + 30,
			wantChild:  map[string]int64{"s1": 10, "s2": 30},
			wantFirst:  from,
		},
		{
			name:       "task with no job",
			scope:      ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w2"},
			wantTokens: 1000,
			wantChild:  map[string]int64{consumptionUnattributed: 1000},
			wantFirst:  ledgerBase.Add(4 * time.Minute),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := tc.scope
			scope.From, scope.To = from, to
			rollup, err := ledger.Rollup(context.Background(), scope)
			if err != nil {
				t.Fatalf("Rollup: %v", err)
			}
			if rollup.TotalTokens != tc.wantTokens {
				t.Fatalf("TotalTokens = %d, want %d", rollup.TotalTokens, tc.wantTokens)
			}
			if len(rollup.ByChild) != len(tc.wantChild) {
				t.Fatalf("ByChild = %v, want %v", rollup.ByChild, tc.wantChild)
			}
			for child, want := range tc.wantChild {
				if got := rollup.ByChild[child]; got == nil || got.TotalTokens != want {
					t.Fatalf("child %q = %+v, want %d tokens", child, got, want)
				}
			}
			if !rollup.FirstAt.Equal(tc.wantFirst) {
				t.Fatalf("FirstAt = %v, want the first entry in range %v", rollup.FirstAt, tc.wantFirst)
			}
		})
	}
}

// TestConsumptionLedgerRangedRollupReadsTheQuotaPlanes asserts the tenant and
// project scopes cost what their range is worth: one scan per hourly bucket
// the range overlaps, never a scan of the chain.
func TestConsumptionLedgerRangedRollupReadsTheQuotaPlanes(t *testing.T) {
	for _, scope := range []ConsumptionScope{
		{TenantID: "t1"},
		{TenantID: "t1", ProjectID: "p1"},
	} {
		kv := newScanCountingKV()
		ledger := NewConsumptionLedger(kv)
		scope.From, scope.To = ledgerBase.Add(-2*time.Hour), ledgerBase // buckets 08, 09, 10
		if _, err := ledger.Rollup(context.Background(), scope); err != nil {
			t.Fatalf("Rollup(%+v): %v", scope, err)
		}
		if kv.scans != 3 {
			t.Fatalf("scope %+v scanned %d prefixes, want 3: %v", scope, kv.scans, kv.prefixes)
		}
		for _, prefix := range kv.prefixes {
			if strings.HasPrefix(prefix, consumptionChainKeyspace) {
				t.Fatalf("a ranged %+v scanned the chain: %s", scope, prefix)
			}
		}
	}
}

// TestConsumptionLedgerRangedRollupRefusesAnOpenOrInvertedRange asserts a
// range is both ends or neither, and forward.
func TestConsumptionLedgerRangedRollupRefusesAnOpenOrInvertedRange(t *testing.T) {
	ledger, _ := newLedgerFixture()
	for name, scope := range map[string]ConsumptionScope{
		"from only": {TenantID: "t1", From: ledgerBase},
		"to only":   {TenantID: "t1", To: ledgerBase},
		"inverted":  {TenantID: "t1", From: ledgerBase, To: ledgerBase.Add(-time.Minute)},
		"empty":     {TenantID: "t1", From: ledgerBase, To: ledgerBase},
	} {
		rollup, err := ledger.Rollup(context.Background(), scope)
		var invalid *ConsumptionInvalidError
		if rollup != nil || !stderrors.As(err, &invalid) || invalid.Field != "range" {
			t.Fatalf("%s: rollup=%v err=%v, want a range refusal", name, rollup, err)
		}
	}
}

// TestConsumptionLedgerRangedRollupRefusesAFailedScan asserts a plane that
// cannot be read yields no roll-up, never a partial one.
func TestConsumptionLedgerRangedRollupRefusesAFailedScan(t *testing.T) {
	boom := stderrors.New("scan failed")
	ledger := NewConsumptionLedger(&failingScanKV{err: boom})
	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{
		TenantID: "t1", From: ledgerBase, To: ledgerBase.Add(time.Hour),
	})
	if rollup != nil || !stderrors.Is(err, ErrConsumptionRead) || !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("rollup=%v err=%v, want no roll-up and the scan's failure", rollup, err)
	}
}

// failingScanKV fails every scan.
type failingScanKV struct {
	err error
}

func (k *failingScanKV) Put(context.Context, string, []byte) error      { return k.err }
func (k *failingScanKV) Get(context.Context, string) ([]byte, error)    { return nil, k.err }
func (k *failingScanKV) Delete(context.Context, string) error           { return k.err }
func (k *failingScanKV) List(context.Context, string) ([]string, error) { return nil, k.err }
func (k *failingScanKV) Exists(context.Context, string) (bool, error)   { return false, k.err }
func (k *failingScanKV) Scan(context.Context, string, func(string, []byte) error) error {
	return k.err
}

// =============================================================================
// Subscription fee share (ruling F4)
// =============================================================================

func TestAccountBillingFeeShare(t *testing.T) {
	month := 2629746 * time.Second
	subscription := AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: 200_000_000}
	tests := []struct {
		name      string
		billing   AccountBilling
		span      time.Duration
		want      int64
		wantKnown bool
	}{
		{"a whole month is the whole fee", subscription, month, 200_000_000, true},
		{"a day is a day's share", subscription, 24 * time.Hour, 6_570_976, true},
		{"a year is twelve fees", subscription, 12 * month, 2_400_000_000, true},
		{"an empty range shares nothing", subscription, 0, 0, true},
		{"an inverted range shares nothing", subscription, -time.Hour, 0, true},
		{"an api account has no fee", AccountBilling{Mode: BillingAPI}, month, 0, false},
		{"an unconfigured fee is absent", AccountBilling{Mode: BillingSubscription}, month, 0, false},
		{"no billing is absent", AccountBilling{}, month, 0, false},
		{"an absurd product saturates", AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: math.MaxInt64}, math.MaxInt64, math.MaxInt64, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, known := tc.billing.FeeShare(ledgerBase, ledgerBase.Add(tc.span))
			if got != tc.want || known != tc.wantKnown {
				t.Fatalf("FeeShare = (%d, %v), want (%d, %v)", got, known, tc.want, tc.wantKnown)
			}
		})
	}
}

// =============================================================================
// The ranged report and its fee shares
// =============================================================================

// seedFeeFixture registers a used subscription, an idle subscription created
// mid-range, and a metered api account, and records turns on the first and
// the last.
func seedFeeFixture(t *testing.T) (ConsumptionLedger, AccountStore) {
	t.Helper()
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	mustCreateSpendAccount(t, accounts, &Account{ID: "sub-used", TenantID: "t1", Provider: AccountProviderClaude,
		Billing:   AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: 200_000_000},
		CreatedAt: spendBase.Add(-720 * time.Hour)})
	// Registered mid-way through the ranges the tests read.
	mustCreateSpendAccount(t, accounts, &Account{ID: "sub-idle", TenantID: "t1", Provider: AccountProviderClaude,
		Billing:   AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: 100_000_000},
		CreatedAt: spendBase.Add(6 * time.Hour)})
	mustCreateSpendAccount(t, accounts, &Account{ID: "api", TenantID: "t1", Provider: AccountProviderClaude,
		Billing: AccountBilling{Mode: BillingAPI}, CreatedAt: spendBase.Add(-720 * time.Hour)})
	ctx := context.Background()
	for _, entry := range []ConsumptionEntry{
		spendEntry("e1", "p1", "", "", "", 0, "sub-used", 10, ConsumptionOutcomeSuccess, spendBase),
		spendEntry("e2", "p1", "", "", "", 0, "api", 10, ConsumptionOutcomeSuccess, spendBase.Add(time.Minute)),
	} {
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("Record %s: %v", entry.EntryID, err)
		}
	}
	return ledger, accounts
}

// TestReadSpendReportSharesSubscriptionFeesOverItsRange asserts the tenant
// report pro-rates every fee-carrying subscription over its range — the idle
// one included, from its registration — and totals the shares.
func TestReadSpendReportSharesSubscriptionFeesOverItsRange(t *testing.T) {
	ledger, accounts := seedFeeFixture(t)
	from := spendBase.Add(-time.Hour)
	to := from.Add(24 * time.Hour)

	report, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger, Accounts: accounts},
		&SpendReportRequest{TenantID: "t1", From: from, To: to, Now: to.Add(time.Hour)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.From == nil || !report.From.Equal(from) || report.To == nil || !report.To.Equal(to) {
		t.Fatalf("range echoed as %v..%v, want %v..%v", report.From, report.To, from, to)
	}
	rows := make(map[string]SpendAccount, len(report.Accounts))
	for _, row := range report.Accounts {
		rows[row.AccountID] = row
	}
	used, idle, api := rows["sub-used"], rows["sub-idle"], rows["api"]
	usedShare, _ := used.Billing.FeeShare(from, to)
	if used.SubscriptionFeeMicros == nil || *used.SubscriptionFeeMicros != usedShare {
		t.Fatalf("used subscription share = %v, want %d", used.SubscriptionFeeMicros, usedShare)
	}
	if idle.AccountID == "" || idle.Entries != 0 {
		t.Fatalf("the idle subscription must be reported with no consumption: %+v", idle)
	}
	idleShare, _ := idle.Billing.FeeShare(spendBase.Add(6*time.Hour), to)
	idleWhole, _ := idle.Billing.FeeShare(from, to)
	if idleShare >= idleWhole {
		t.Fatalf("the idle share %d is not clamped to its registration (whole range %d)", idleShare, idleWhole)
	}
	if idle.SubscriptionFeeMicros == nil || *idle.SubscriptionFeeMicros != idleShare {
		t.Fatalf("idle subscription share = %v, want %d", idle.SubscriptionFeeMicros, idleShare)
	}
	if api.SubscriptionFeeMicros != nil {
		t.Fatalf("an api account has no fee share, got %d", *api.SubscriptionFeeMicros)
	}
	if report.SubscriptionFeeMicros == nil || *report.SubscriptionFeeMicros != usedShare+idleShare {
		t.Fatalf("report fee total = %v, want %d", report.SubscriptionFeeMicros, usedShare+idleShare)
	}
	if report.Entries != 2 {
		t.Fatalf("Entries = %d, want both turns in range", report.Entries)
	}
}

// TestReadSpendReportFeeRangeDefaultsToTheLedgersSpan asserts an unranged
// report shares fees from the first recorded turn to now, and a report ranged
// only by From closes at now.
func TestReadSpendReportFeeRangeDefaultsToTheLedgersSpan(t *testing.T) {
	ledger, _ := seedFeeFixture(t)
	accounts := NewInMemoryAccountStore()
	billing := AccountBilling{Mode: BillingSubscription, MonthlyFeeMicros: 200_000_000}
	// Registered long before the first turn, so its creation does not clamp.
	mustCreateSpendAccount(t, accounts, &Account{ID: "sub-used", TenantID: "t1",
		Provider: AccountProviderClaude, Billing: billing, CreatedAt: spendBase.Add(-720 * time.Hour)})
	now := spendBase.Add(48 * time.Hour)
	start := spendBase
	unranged, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger, Accounts: accounts},
		&SpendReportRequest{TenantID: "t1", Now: now})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	want, _ := billing.FeeShare(start, now)
	if unranged.From != nil || unranged.SubscriptionFeeMicros == nil || *unranged.SubscriptionFeeMicros != want {
		t.Fatalf("unranged: from=%v fee=%v, want no range and %d", unranged.From, unranged.SubscriptionFeeMicros, want)
	}

	fromOnly := spendBase.Add(-time.Hour)
	ranged, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger, Accounts: accounts},
		&SpendReportRequest{TenantID: "t1", From: fromOnly, Now: now})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if ranged.To == nil || !ranged.To.Equal(now) {
		t.Fatalf("a report ranged by From alone must close at now, got %v", ranged.To)
	}
}

// TestReadSpendReportSharesNoFeeBelowTheTenant asserts a project report never
// carries a subscription's fee: one plan serves every project.
func TestReadSpendReportSharesNoFeeBelowTheTenant(t *testing.T) {
	ledger, accounts := seedFeeFixture(t)
	report, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger, Accounts: accounts},
		&SpendReportRequest{TenantID: "t1", ProjectID: "p1", Now: spendBase.Add(time.Hour)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.SubscriptionFeeMicros != nil {
		t.Fatalf("a project report carries a fee total %d", *report.SubscriptionFeeMicros)
	}
	for _, row := range report.Accounts {
		if row.SubscriptionFeeMicros != nil || row.AccountID == "sub-idle" {
			t.Fatalf("a project report carries a fee row: %+v", row)
		}
	}
}

// TestReadSpendReportFailsWhenTheFeeListingFails asserts an account listing
// that cannot be read fails the tenant report rather than reporting no fees.
func TestReadSpendReportFailsWhenTheFeeListingFails(t *testing.T) {
	ledger, _ := seedFeeFixture(t)
	boom := stderrors.New("listing failed")
	report, err := ReadSpendReport(context.Background(),
		SpendSources{Ledger: ledger, Accounts: &faultyAccountStore{err: &AccountNotFoundError{}, listErr: boom}},
		&SpendReportRequest{TenantID: "t1", Now: spendBase.Add(time.Hour)})
	if report != nil || !stderrors.Is(err, boom) {
		t.Fatalf("report=%v err=%v, want no report and the listing's failure", report, err)
	}
}

// TestReadSpendReportRefusesAnOpenEndedRangeAtTheStart asserts To alone is
// refused rather than read as the whole history.
func TestReadSpendReportRefusesAnOpenEndedRangeAtTheStart(t *testing.T) {
	ledger, _ := seedFeeFixture(t)
	report, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger},
		&SpendReportRequest{TenantID: "t1", To: spendBase})
	var invalid *ConsumptionInvalidError
	if report != nil || !stderrors.As(err, &invalid) {
		t.Fatalf("report=%v err=%v, want a range refusal", report, err)
	}
}
