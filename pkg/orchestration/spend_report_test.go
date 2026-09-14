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
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"
)

// spendBase anchors every fixture. Fixed and mid-hour for the same reason
// ledgerBase is: the quota plane truncates to the hour, and a fixture anchored
// at time.Now() would straddle a bucket boundary on some runs and not others.
var spendBase = time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)

// spendEntry builds one acceptable ledger entry, so each test states only the
// dimensions it is about.
func spendEntry(entryID, project, task, job, session string, round int, account string, tokens int64, outcome ConsumptionOutcome, at time.Time) ConsumptionEntry {
	return ConsumptionEntry{
		EntryID:      entryID,
		TenantID:     "t1",
		ProjectID:    project,
		TaskID:       task,
		JobID:        job,
		SessionID:    session,
		Round:        round,
		AccountID:    account,
		Engine:       "acp",
		Model:        "claude-opus-5",
		Outcome:      outcome,
		InputTokens:  tokens,
		OutputTokens: tokens,
		OccurredAt:   at,
	}
}

// seedSpendFixture records a small chain: two projects, two jobs under one
// task, and one job with no task at all — the unattributed subtree the report
// has to label rather than invent a parent for.
func seedSpendFixture(t *testing.T, ledger ConsumptionLedger) {
	t.Helper()
	ctx := context.Background()
	entries := []ConsumptionEntry{
		spendEntry("e1", "p1", "task-a", "job-1", "s1", 0, "acct-1", 10, ConsumptionOutcomeSuccess, spendBase),
		spendEntry("e2", "p1", "task-a", "job-1", "s1", 1, "acct-1", 20, ConsumptionOutcomeSuccess, spendBase.Add(time.Minute)),
		spendEntry("e3", "p1", "task-a", "job-2", "s2", 0, "acct-2", 5, ConsumptionOutcomeFailure, spendBase.Add(2*time.Minute)),
		spendEntry("e4", "p1", "", "job-3", "s3", 0, "acct-1", 1, ConsumptionOutcomeSuccess, spendBase.Add(3*time.Minute)),
		spendEntry("e5", "p2", "task-b", "job-4", "s4", 0, "acct-2", 100, ConsumptionOutcomeSuccess, spendBase.Add(4*time.Minute)),
	}
	for _, entry := range entries {
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("seed %s: %v", entry.EntryID, err)
		}
	}
}

// =============================================================================
// The roll-up, at every level
// =============================================================================

// TestReadSpendReportAtEveryLevel walks the chain down and asserts the totals
// narrow as the scope does. It is the property the drill-down is built on: a
// parent's total is the sum of its children's, at every depth.
func TestReadSpendReportAtEveryLevel(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)
	ctx := context.Background()

	cases := []struct {
		name       string
		req        SpendReportRequest
		wantLevel  SpendLevel
		wantChild  SpendLevel
		wantTokens int64
		wantEntry  int
	}{
		{"tenant", SpendReportRequest{TenantID: "t1"}, SpendLevelTenant, SpendLevelProject, 272, 5},
		{"project", SpendReportRequest{TenantID: "t1", ProjectID: "p1"}, SpendLevelProject, SpendLevelTask, 72, 4},
		{"task", SpendReportRequest{TenantID: "t1", ProjectID: "p1", TaskID: "task-a"}, SpendLevelTask, SpendLevelJob, 70, 3},
		{"job", SpendReportRequest{TenantID: "t1", ProjectID: "p1", TaskID: "task-a", JobID: "job-1"}, SpendLevelJob, SpendLevelSession, 60, 2},
		{
			"session",
			SpendReportRequest{TenantID: "t1", ProjectID: "p1", TaskID: "task-a", JobID: "job-1", SessionID: "s1"},
			SpendLevelSession, SpendLevelRound, 60, 2,
		},
		{
			"round",
			SpendReportRequest{TenantID: "t1", ProjectID: "p1", TaskID: "task-a", JobID: "job-1", SessionID: "s1", Round: 1, RoundSet: true},
			SpendLevelRound, "", 40, 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := ReadSpendReport(ctx, ledger, nil, &tc.req)
			if err != nil {
				t.Fatalf("ReadSpendReport: %v", err)
			}
			if report.Level != tc.wantLevel {
				t.Errorf("Level = %q, want %q", report.Level, tc.wantLevel)
			}
			if report.ChildLevel != tc.wantChild {
				t.Errorf("ChildLevel = %q, want %q", report.ChildLevel, tc.wantChild)
			}
			if report.TotalTokens != tc.wantTokens {
				t.Errorf("TotalTokens = %d, want %d", report.TotalTokens, tc.wantTokens)
			}
			if report.Entries != tc.wantEntry {
				t.Errorf("Entries = %d, want %d", report.Entries, tc.wantEntry)
			}
			if report.InputTokens+report.OutputTokens != report.TotalTokens {
				t.Errorf("input+output = %d, want TotalTokens %d",
					report.InputTokens+report.OutputTokens, report.TotalTokens)
			}
		})
	}
}

// TestReadSpendReportDrillDownSumsToTheParent pins the invariant the UI's
// drill-down relies on: every child row's tokens add up to the scope's total.
// A breakdown that does not is worse than none, because it looks authoritative.
func TestReadSpendReportDrillDownSumsToTheParent(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil,
		&SpendReportRequest{TenantID: "t1", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	var summed int64
	for _, child := range report.Children {
		summed += child.TotalTokens
	}
	if summed != report.TotalTokens {
		t.Fatalf("children sum to %d, scope total is %d", summed, report.TotalTokens)
	}
}

// TestReadSpendReportLabelsTheUnattributedChild asserts the ledger's no-value
// sentinel is reported as an ABSENCE and never as an entity whose identifier
// is a dash. job-3 in the fixture belongs to no task.
func TestReadSpendReportLabelsTheUnattributedChild(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil,
		&SpendReportRequest{TenantID: "t1", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}

	var unattributed *SpendChild
	for i := range report.Children {
		if report.Children[i].Unattributed {
			unattributed = &report.Children[i]
		}
		if report.Children[i].ID == consumptionUnattributed {
			t.Fatalf("the sentinel leaked into a child id: %+v", report.Children[i])
		}
	}
	if unattributed == nil {
		t.Fatal("the task-less job's spend must be reported as an unattributed row")
	}
	if unattributed.ID != "" {
		t.Errorf("an unattributed row must carry no id, got %q", unattributed.ID)
	}
	if unattributed.TotalTokens != 2 {
		t.Errorf("unattributed tokens = %d, want 2", unattributed.TotalTokens)
	}
	// It sorts last: a residue at the head of a drill-down reads as the
	// largest contributor.
	if !report.Children[len(report.Children)-1].Unattributed {
		t.Error("the unattributed row must sort last")
	}
}

// TestReadSpendReportDrillsIntoTheUnattributedSubtree asserts a row a reader
// can SEE is a row a reader can OPEN. The task-less job's spend is addressed
// by naming SpendUnattributed at the task level, which is unambiguous because
// the write side refuses that id for a real entity.
func TestReadSpendReportDrillsIntoTheUnattributedSubtree(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{
		TenantID: "t1", ProjectID: "p1", TaskID: SpendUnattributed,
	})
	if err != nil {
		t.Fatalf("ReadSpendReport into the unattributed subtree: %v", err)
	}
	if report.Level != SpendLevelTask {
		t.Fatalf("Level = %q, want task", report.Level)
	}
	if report.TotalTokens != 2 || report.Entries != 1 {
		t.Fatalf("tokens=%d entries=%d, want 2 and 1 (only the task-less job)",
			report.TotalTokens, report.Entries)
	}
	if len(report.Children) != 1 || report.Children[0].ID != "job-3" {
		t.Fatalf("children = %+v, want the task-less job", report.Children)
	}
}

// TestRecordRefusesTheUnattributedSentinelAsAnID is the guarantee the scope
// rule above rests on: no real entity may bear the sentinel, so a scope
// naming it can only mean the unattributed subtree. If this ever stops
// holding, drilling into that subtree becomes ambiguous.
func TestRecordRefusesTheUnattributedSentinelAsAnID(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	entry := spendEntry("e1", "p1", SpendUnattributed, "", "", 0, "acct-1", 1, ConsumptionOutcomeSuccess, spendBase)

	err := ledger.Record(context.Background(), entry)
	if err == nil {
		t.Fatal("a task literally named the sentinel must be refused at the write")
	}
	var invalid *ConsumptionInvalidError
	if !stderrors.As(err, &invalid) || invalid.Field != "task_id" {
		t.Fatalf("want a ConsumptionInvalidError naming task_id, got %v", err)
	}
}

// TestReadSpendReportUnpadsTheRoundSegment asserts a round drill-down reads as
// a number and not as the ledger's zero-padded key segment.
func TestReadSpendReportUnpadsTheRoundSegment(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{
		TenantID: "t1", ProjectID: "p1", TaskID: "task-a", JobID: "job-1", SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	got := make([]string, 0, len(report.Children))
	for _, child := range report.Children {
		if child.Level != SpendLevelRound {
			t.Errorf("child level = %q, want round", child.Level)
		}
		got = append(got, child.ID)
	}
	if len(got) != 2 || got[0] != "0" || got[1] != "1" {
		t.Fatalf("round children = %v, want [0 1]", got)
	}
}

// TestReadSpendReportPartitionsOutcomes asserts a failed turn is counted and
// is distinguishable — it spent tokens too, which is why it is in the ledger.
func TestReadSpendReportPartitionsOutcomes(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.Successes != 4 || report.Failures != 1 {
		t.Fatalf("successes=%d failures=%d, want 4 and 1", report.Successes, report.Failures)
	}
	if report.Successes+report.Failures != report.Entries {
		t.Fatalf("outcomes must partition entries: %d+%d != %d",
			report.Successes, report.Failures, report.Entries)
	}
}

// TestReadSpendReportSplitsTheModelKey asserts the composite engine/model
// roll-up key is recovered into its two parts.
func TestReadSpendReportSplitsTheModelKey(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	entry := spendEntry("e1", "p1", "", "", "", 0, "acct-1", 3, ConsumptionOutcomeSuccess, spendBase)
	entry.Engine = "openrouter"
	entry.Model = "claude-opus-5"
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("Models = %+v, want one row", report.Models)
	}
	if report.Models[0].Engine != "openrouter" || report.Models[0].Model != "claude-opus-5" {
		t.Fatalf("model row = %+v, want engine openrouter and model claude-opus-5", report.Models[0])
	}
}

// TestReadSpendReportModelSplitIsUnambiguous states WHY cutting at the first
// separator is exact: the ledger refuses a separator inside either half at the
// write, so no engine or model name can span the cut. If that guard is ever
// relaxed, this test fails and spendModels has to be revisited.
func TestReadSpendReportModelSplitIsUnambiguous(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	entry := spendEntry("e1", "p1", "", "", "", 0, "acct-1", 3, ConsumptionOutcomeSuccess, spendBase)
	entry.Model = "anthropic/claude-opus-5"

	err := ledger.Record(context.Background(), entry)
	if err == nil {
		t.Fatal("a model containing the key separator must be refused at the write")
	}
	var invalid *ConsumptionInvalidError
	if !stderrors.As(err, &invalid) || invalid.Field != "model" {
		t.Fatalf("want a ConsumptionInvalidError naming model, got %v", err)
	}
}

// TestReadSpendReportKeepsAnUnsplittableModelKeyWhole asserts a breakdown key
// with no separator — which this package never writes — is reported as it was
// read rather than guessed apart.
func TestReadSpendReportKeepsAnUnsplittableModelKeyWhole(t *testing.T) {
	models := spendModels(map[string]int64{"bare": 7})
	if len(models) != 1 {
		t.Fatalf("spendModels = %+v, want one row", models)
	}
	if models[0].Engine != "" || models[0].Model != "bare" {
		t.Fatalf("row = %+v, want the key kept whole as the model", models[0])
	}
}

// =============================================================================
// The honesty rules
// =============================================================================

// TestReadSpendReportCostIsUnmeasured is the guard go-quicbaas#791 asks for:
// nothing prices a turn, so a production roll-up's cost is UNMEASURED and a
// reader must not render it. It fails the day a price source lands, which is
// the reminder to revisit every renderer.
func TestReadSpendReportCostIsUnmeasured(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.CostMeasured {
		t.Fatal("no production writer prices a turn; CostMeasured must be false")
	}
	if report.CostMicros != 0 {
		t.Fatalf("CostMicros = %d, want 0 (unmeasured)", report.CostMicros)
	}
}

// TestReadSpendReportCostIsMeasuredWhenPriced asserts the flag is DERIVED and
// not hard-coded: an entry that carries a price reports a measured total, so
// the price source can land without every reader changing.
func TestReadSpendReportCostIsMeasuredWhenPriced(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	entry := spendEntry("e1", "p1", "", "", "", 0, "acct-1", 3, ConsumptionOutcomeSuccess, spendBase)
	entry.CostMicros = 4200
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if !report.CostMeasured || report.CostMicros != 4200 {
		t.Fatalf("measured=%t cost=%d, want true and 4200", report.CostMeasured, report.CostMicros)
	}
}

// TestReadSpendReportPolicyIsUnknownWithoutAnAccountStore asserts an unwired
// deployment says "unknown" rather than reporting every account as unmetered.
func TestReadSpendReportPolicyIsUnknownWithoutAnAccountStore(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if len(report.Accounts) != 2 {
		t.Fatalf("Accounts = %+v, want two", report.Accounts)
	}
	for _, account := range report.Accounts {
		if account.PolicyStatus != SpendPolicyUnknown {
			t.Errorf("%s: PolicyStatus = %q, want unknown", account.AccountID, account.PolicyStatus)
		}
		if account.Windows != nil || account.Breaker != nil {
			t.Errorf("%s: an unknown policy must carry no allowance detail", account.AccountID)
		}
	}
}

// TestReadSpendReportPolicyIsNoneForAnUnmeteredAccount asserts the honest "no
// spend policy": the account was READ and meters nothing, which is a different
// fact from "could not look" and must not render as a budget of zero.
func TestReadSpendReportPolicyIsNoneForAnUnmeteredAccount(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude, DisplayName: "alice-work",
	})
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 7, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(ctx, ledger, accounts, &SpendReportRequest{TenantID: "t1", Now: spendBase.Add(time.Minute)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if len(report.Accounts) != 1 {
		t.Fatalf("Accounts = %+v, want one", report.Accounts)
	}
	account := report.Accounts[0]
	if account.PolicyStatus != SpendPolicyNone {
		t.Fatalf("PolicyStatus = %q, want none", account.PolicyStatus)
	}
	if account.DisplayName != "alice-work" || account.Provider != string(AccountProviderClaude) {
		t.Fatalf("a readable account must carry its label: %+v", account)
	}
	if len(account.Windows) != 0 {
		t.Fatalf("an unmetered account has no windows, got %+v", account.Windows)
	}
}

// TestReadSpendReportDeletedAccountKeepsItsSpend asserts the ledger outlives
// the accounts it names: spend by an account that no longer exists is still
// reported, with its allowance marked unknown, rather than failing the scope.
func TestReadSpendReportDeletedAccountKeepsItsSpend(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "gone", 9, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(ctx, ledger, accounts, &SpendReportRequest{TenantID: "t1", Now: spendBase})
	if err != nil {
		t.Fatalf("a deleted account must not fail the report: %v", err)
	}
	if len(report.Accounts) != 1 || report.Accounts[0].TotalTokens != 18 {
		t.Fatalf("Accounts = %+v, want the spend kept", report.Accounts)
	}
	if report.Accounts[0].PolicyStatus != SpendPolicyUnknown {
		t.Fatalf("PolicyStatus = %q, want unknown", report.Accounts[0].PolicyStatus)
	}
}

// =============================================================================
// The metered account: windows and breaker
// =============================================================================

// TestReadSpendReportReportsQuotaWindows asserts a metered account's window
// reports the SAME usage the enforcement path would read, so a dashboard and a
// refusal cannot disagree about one account.
func TestReadSpendReportReportsQuotaWindows(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
		}}},
	})
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 60, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	now := spendBase.Add(time.Minute)
	report, err := ReadSpendReport(ctx, ledger, accounts, &SpendReportRequest{TenantID: "t1", Now: now})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	account := report.Accounts[0]
	if account.PolicyStatus != SpendPolicyMetered {
		t.Fatalf("PolicyStatus = %q, want metered", account.PolicyStatus)
	}
	if len(account.Windows) != 1 {
		t.Fatalf("Windows = %+v, want one", account.Windows)
	}
	window := account.Windows[0]
	if window.Name != "five-hour" || window.MaxTokens != 1000 {
		t.Fatalf("window = %+v, want the account's own allowance", window)
	}
	if window.UsedTokens != 120 || window.RemainingTokens != 880 {
		t.Fatalf("used=%d remaining=%d, want 120 and 880", window.UsedTokens, window.RemainingTokens)
	}
	if window.Exhausted {
		t.Error("a window with headroom must not report exhausted")
	}
	if want := spendBase.Add(5 * time.Hour); !window.FreesAt.Equal(want) {
		t.Errorf("FreesAt = %v, want the oldest entry plus the window (%v)", window.FreesAt, want)
	}
}

// TestReadSpendReportClampsAnOverspentWindow asserts an over-spend reports no
// remaining allowance rather than a negative one, which a reader would
// otherwise be free to treat as headroom in the other direction.
func TestReadSpendReportClampsAnOverspentWindow(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
			{Name: "tight", Size: time.Hour, MaxTokens: 10},
		}}},
	})
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 50, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(ctx, ledger, accounts,
		&SpendReportRequest{TenantID: "t1", Now: spendBase.Add(time.Minute)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	window := report.Accounts[0].Windows[0]
	if window.RemainingTokens != 0 {
		t.Fatalf("RemainingTokens = %d, want 0", window.RemainingTokens)
	}
	if !window.Exhausted {
		t.Fatal("a spent window must report exhausted")
	}
}

// TestReadSpendReportExhaustedIsAboutTheAllowanceNotTheRequest pins the one
// boundary where the report and the quota gate give different answers, and
// the reason they do.
//
// At used == max the window has NO ALLOWANCE LEFT, so the report says
// exhausted with zero remaining. The gate still ADMITS a spawn there, because
// a spawn passes an estimate of zero and the refusal is
// used + estimated > max. Both are correct about different questions, and
// asserting them side by side is what stops a future reader "fixing" one into
// the other. See SpendWindowStatus.Exhausted.
func TestReadSpendReportExhaustedIsAboutTheAllowanceNotTheRequest(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	window := QuotaWindow{Name: "tight", Size: time.Hour, MaxTokens: 20}
	account := &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{window}}},
	}
	mustCreateSpendAccount(t, accounts, account)
	// Exactly the allowance: 10 in + 10 out.
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 10, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	now := spendBase.Add(time.Minute)

	report, err := ReadSpendReport(ctx, ledger, accounts, &SpendReportRequest{TenantID: "t1", Now: now})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	status := report.Accounts[0].Windows[0]
	if status.UsedTokens != 20 || status.RemainingTokens != 0 || !status.Exhausted {
		t.Fatalf("window = %+v, want used 20, remaining 0, exhausted", status)
	}

	// The gate, asked the spawn's question, admits: max > max is false.
	decision, err := NewQuotaAuthority(ledger).Admit(ctx, QuotaAdmitRequest{
		Account: account, Policy: account.Spend.Quota, EstimatedTokens: 0,
	}, now)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("a zero-estimate spawn at used == max must still be admitted; got %+v", decision)
	}

	// A turn that estimates even one token is refused, which is what makes
	// the report's "exhausted" the honest word for the allowance.
	next, err := NewQuotaAuthority(ledger).Admit(ctx, QuotaAdmitRequest{
		Account: account, Policy: account.Spend.Quota, EstimatedTokens: 1,
	}, now)
	if err != nil {
		t.Fatalf("Admit(1): %v", err)
	}
	if next.Allowed {
		t.Fatalf("a one-token turn at used == max must be refused; got %+v", next)
	}
}

// TestReadSpendReportRefusesAnUnreadableAccountStore asserts a store FAULT
// fails the report rather than rendering as "nobody configured a cap".
//
// This is the ledger's own rule applied to the other store: a failed read is
// never a number. Collapsing it to SpendPolicyUnknown meant that while the
// account store was down, every account read "spend policy unknown" beside
// healthy token totals — a fault presented as a fact.
func TestReadSpendReportRefusesAnUnreadableAccountStore(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 5, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	boom := stderrors.New("account store unavailable")

	report, err := ReadSpendReport(ctx, ledger, &faultyAccountStore{err: boom},
		&SpendReportRequest{TenantID: "t1", Now: spendBase})
	if report != nil {
		t.Fatalf("an unreadable account store must return no report, got %+v", report)
	}
	if !stderrors.Is(err, boom) {
		t.Fatalf("err = %v, want it to carry the store's own failure", err)
	}
	var reportErr *SpendReportError
	if !stderrors.As(err, &reportErr) {
		t.Fatalf("want SpendReportError, got %v", err)
	}
}

// TestReadSpendReportKeepsSpendForAnAccountThatIsMerelyGone is the other half
// of the rule above: a NOT-FOUND is a permanent, benign fact about an
// append-only ledger outliving its accounts, and must not fail the report.
func TestReadSpendReportKeepsSpendForAnAccountThatIsMerelyGone(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "gone", 5, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	notFound := &AccountNotFoundError{TenantID: "t1", AccountID: "gone"}

	report, err := ReadSpendReport(ctx, ledger, &faultyAccountStore{err: notFound},
		&SpendReportRequest{TenantID: "t1", Now: spendBase})
	if err != nil {
		t.Fatalf("a deleted account must not fail the report: %v", err)
	}
	if len(report.Accounts) != 1 || report.Accounts[0].PolicyStatus != SpendPolicyUnknown {
		t.Fatalf("accounts = %+v, want the spend kept with an unknown allowance", report.Accounts)
	}
	if report.Accounts[0].TotalTokens != 10 {
		t.Fatalf("TotalTokens = %d, want 10", report.Accounts[0].TotalTokens)
	}
}

// TestReadSpendReportReportsTheBreakerVerdict asserts a tripped account is
// reported as open, derived from the ledger's own failure entries.
func TestReadSpendReportReportsTheBreakerVerdict(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Breaker: &BreakerPolicy{
			Window: time.Hour, FailureThreshold: 2, Cooldown: 30 * time.Minute,
		}},
	})
	for i, id := range []string{"f1", "f2"} {
		entry := spendEntry(id, "p1", "", "", "", 0, "acct-1", 1, ConsumptionOutcomeFailure,
			spendBase.Add(time.Duration(i)*time.Minute))
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("Record %s: %v", id, err)
		}
	}

	report, err := ReadSpendReport(ctx, ledger, accounts,
		&SpendReportRequest{TenantID: "t1", Now: spendBase.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	account := report.Accounts[0]
	if account.Breaker == nil {
		t.Fatal("an account with a breaker rule must report its verdict")
	}
	if account.Breaker.State != BreakerOpen {
		t.Fatalf("breaker state = %q, want open", account.Breaker.State)
	}
	if account.Breaker.Failures != 2 {
		t.Fatalf("Failures = %d, want 2", account.Breaker.Failures)
	}
}

// TestReadSpendReportOmitsTheBreakerWhenNoRuleIsSet asserts the absence of a
// breaker rule is an absence, not a closed breaker — the same distinction the
// policy status draws.
func TestReadSpendReportOmitsTheBreakerWhenNoRuleIsSet(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
		}}},
	})
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 1, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(ctx, ledger, accounts, &SpendReportRequest{TenantID: "t1", Now: spendBase})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.Accounts[0].Breaker != nil {
		t.Fatalf("no breaker rule must report no verdict, got %+v", report.Accounts[0].Breaker)
	}
}

// =============================================================================
// The wire: an instant that never happened must be ABSENT
// =============================================================================

// TestSpendReportOmitsInstantsThatNeverHappened is the regression guard for a
// defect `omitempty` invites and does not prevent.
//
// For a STRUCT field, encoding/json treats nothing as empty — only absent
// pointers, maps, slices, empty strings, false and zero numbers are omitted.
// So a `time.Time` tagged omitempty is ALWAYS serialised, and a zero one
// crosses as "0001-01-01T00:00:00Z": a real date, which a renderer draws as
// January of year 1. That is the same invented figure this package refuses
// everywhere else, arriving through the encoder rather than the arithmetic,
// and it reaches a browser on both the REST and the desktop paths.
//
// The fields are therefore pointers. This test reads the JSON rather than the
// struct, because the struct cannot tell the two encodings apart.
func TestSpendReportOmitsInstantsThatNeverHappened(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()

	// An empty scope: nothing was spent, so there is no first and no last.
	report, err := ReadSpendReport(context.Background(), ledger, nil,
		&SpendReportRequest{TenantID: "t1", Now: spendBase})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, field := range []string{"first_at", "last_at"} {
		if strings.Contains(string(encoded), field) {
			t.Errorf("an empty scope must omit %q entirely; got %s", field, encoded)
		}
	}
	if strings.Contains(string(encoded), "0001-01-01") {
		t.Fatalf("the year 1 reached the wire: %s", encoded)
	}
}

// TestSpendWindowOmitsFreesAtForAnEmptyWindow asserts the same rule for the
// window's own instant: a window holding nothing has nothing to age out, and
// says so by absence rather than by the epoch.
func TestSpendWindowOmitsFreesAtForAnEmptyWindow(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	accounts := NewInMemoryAccountStore()
	ctx := context.Background()
	mustCreateSpendAccount(t, accounts, &Account{
		ID: "acct-1", TenantID: "t1", Provider: AccountProviderClaude,
		Spend: AccountSpendPolicy{
			Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
				{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
			}},
			// A breaker that has never tripped: its last failure never
			// happened either.
			Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute},
		},
	})
	// Spend recorded far enough in the past that the five-hour window is
	// empty, so the account appears with a window that holds nothing.
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "acct-1", 1, ConsumptionOutcomeSuccess, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	report, err := ReadSpendReport(ctx, ledger, accounts,
		&SpendReportRequest{TenantID: "t1", Now: spendBase.Add(10 * time.Hour)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	window := report.Accounts[0].Windows[0]
	if window.UsedTokens != 0 {
		t.Fatalf("the window should have aged out; used = %d", window.UsedTokens)
	}
	if window.FreesAt != nil {
		t.Fatalf("an empty window has nothing to age out; FreesAt = %v", window.FreesAt)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "0001-01-01") {
		t.Fatalf("the year 1 reached the wire: %s", encoded)
	}
	for _, field := range []string{"frees_at", "last_failure_at"} {
		if strings.Contains(string(encoded), field) {
			t.Errorf("a moment that never happened must be omitted, not zero: %q in %s", field, encoded)
		}
	}
}

// TestSpendReportKeepsInstantsThatDidHappen is the permitting half: a real
// moment is still reported, so the omission above is about absence and not
// about dropping the field.
func TestSpendReportKeepsInstantsThatDidHappen(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	seedSpendFixture(t, ledger)

	report, err := ReadSpendReport(context.Background(), ledger, nil,
		&SpendReportRequest{TenantID: "t1", Now: spendBase.Add(time.Hour)})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if report.FirstAt == nil || report.LastAt == nil {
		t.Fatalf("a scope with spend must bound it: first=%v last=%v", report.FirstAt, report.LastAt)
	}
	if !report.FirstAt.Equal(spendBase) {
		t.Errorf("FirstAt = %v, want %v", report.FirstAt, spendBase)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, field := range []string{"first_at", "last_at"} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("a real moment must be reported; %q missing from %s", field, encoded)
		}
	}
}

// =============================================================================
// Refusals
// =============================================================================

// TestReadSpendReportRefusalPaths covers every way the read must fail rather
// than answer, because each of them would otherwise render as an account that
// has spent nothing.
func TestReadSpendReportRefusalPaths(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()

	cases := []struct {
		name    string
		ledger  ConsumptionLedger
		req     *SpendReportRequest
		wantErr error
	}{
		{"nil request", ledger, nil, ErrSpendRequestRequired},
		{"no ledger wired", nil, &SpendReportRequest{TenantID: "t1"}, ErrSpendLedgerRequired},
		{"no tenant", ledger, &SpendReportRequest{}, ErrConsumptionInvalid},
		{"skipped level", ledger, &SpendReportRequest{TenantID: "t1", TaskID: "task-a"}, ErrConsumptionInvalid},
		{"negative round", ledger, &SpendReportRequest{
			TenantID: "t1", ProjectID: "p", TaskID: "t", JobID: "j", SessionID: "s", Round: -1, RoundSet: true,
		}, ErrConsumptionInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := ReadSpendReport(ctx, tc.ledger, nil, tc.req)
			if report != nil {
				t.Fatalf("a refused read must return no report, got %+v", report)
			}
			var reportErr *SpendReportError
			if !stderrors.As(err, &reportErr) {
				t.Fatalf("want SpendReportError, got %v", err)
			}
			if !stderrors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want it to unwrap to %v", err, tc.wantErr)
			}
		})
	}
}

// TestReadSpendReportRefusesAnUnreadableSubtree asserts a scan failure fails
// the whole report. A partial roll-up presented as a total is a healthy-looking
// account whose first contradiction is the provider's invoice.
func TestReadSpendReportRefusesAnUnreadableSubtree(t *testing.T) {
	kv := newChainFaultKV()
	kv.scanErr = stderrors.New("store unavailable")
	ledger := NewConsumptionLedger(kv)

	report, err := ReadSpendReport(context.Background(), ledger, nil, &SpendReportRequest{TenantID: "t1"})
	if report != nil {
		t.Fatalf("a failed scan must return no report, got %+v", report)
	}
	var readErr *ConsumptionReadError
	if !stderrors.As(err, &readErr) {
		t.Fatalf("want ConsumptionReadError, got %v", err)
	}
}

// TestSpendReportErrorMessage pins the refusal's wording: it names the tenant
// and the level, so an operator reading a log knows which read failed.
func TestSpendReportErrorMessage(t *testing.T) {
	withCause := &SpendReportError{TenantID: "t1", Level: SpendLevelJob, Cause: stderrors.New("boom")}
	if got, want := withCause.Error(), "orchestration spend report: tenant=t1 level=job: boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !stderrors.Is(withCause, withCause.Cause) {
		t.Fatal("Unwrap must expose the cause")
	}
	bare := &SpendReportError{TenantID: "t1"}
	if got, want := bare.Error(), "orchestration spend report: tenant=t1"; got != want {
		t.Fatalf("Error() with no cause = %q, want %q", got, want)
	}
	if bare.Unwrap() != nil {
		t.Fatal("a refusal with no cause must unwrap to nil")
	}
}

// =============================================================================
// EvaluateBreaker — the exported derivation
// =============================================================================

// TestEvaluateBreakerDerivesFromTheLedgerAlone asserts the exported helper
// needs no KV store: the state is a function of the ledger, and only the
// half-open probe is kept.
func TestEvaluateBreakerDerivesFromTheLedgerAlone(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()
	if err := ledger.Record(ctx, spendEntry("f1", "p1", "", "", "", 0, "acct-1", 1, ConsumptionOutcomeFailure, spendBase)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	policy := BreakerPolicy{Window: time.Hour, FailureThreshold: 1, Cooldown: 10 * time.Minute}

	verdict, err := EvaluateBreaker(ctx, ledger, "t1", "acct-1", policy, spendBase.Add(time.Minute))
	if err != nil {
		t.Fatalf("EvaluateBreaker: %v", err)
	}
	if verdict.State != BreakerOpen {
		t.Fatalf("state = %q, want open", verdict.State)
	}
}

// TestEvaluateBreakerRefusesBadInput asserts the helper refuses rather than
// reporting a healthy account it could not establish.
func TestEvaluateBreakerRefusesBadInput(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()

	if _, err := EvaluateBreaker(ctx, nil, "t1", "a1",
		BreakerPolicy{Window: time.Hour, FailureThreshold: 1, Cooldown: time.Minute}, spendBase); err == nil {
		t.Error("a nil ledger must be refused")
	}
	if _, err := EvaluateBreaker(ctx, ledger, "t1", "a1", BreakerPolicy{}, spendBase); err == nil {
		t.Error("a zero policy must be refused: it would trip an account that has never failed")
	}
}

// mustCreateSpendAccount stores an account or fails the test.
func mustCreateSpendAccount(t *testing.T, store AccountStore, account *Account) {
	t.Helper()
	if err := store.Create(context.Background(), account); err != nil {
		t.Fatalf("create account %s: %v", account.ID, err)
	}
}

// faultyAccountStore is an AccountStore whose Get always fails with err. Only
// Get is exercised by the spend report; the rest panic so a future caller
// that starts using them cannot get a silent zero value.
type faultyAccountStore struct {
	AccountStore
	err error
}

func (s *faultyAccountStore) Get(context.Context, string, string) (*Account, error) {
	return nil, s.err
}
