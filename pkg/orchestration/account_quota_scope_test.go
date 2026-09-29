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

// =============================================================================
// The dollar axis and project/tenant scopes (go-quicbaas#1058, rulings F5)
// =============================================================================

func fiveHourDollars(micros int64) SpendLimits {
	return SpendLimits{Windows: []QuotaWindow{{Name: "five-hour", Size: 5 * time.Hour, MaxEquivalentMicros: micros}}}
}

func TestQuotaAuthorityDollarAxis(t *testing.T) {
	tests := []struct {
		name       string
		spent      int64
		limits     SpendLimits
		wantAllow  bool
		wantReason QuotaReason
		wantAxis   string
	}{
		{"under a dollar cap", 400, fiveHourDollars(1000), true, QuotaReasonWithinLimits, ""},
		{"a spent dollar cap refuses", 1000, fiveHourDollars(1000), false, QuotaReasonCostLimit, QuotaAxisEquivalentMicros},
		{"both axes: the token axis still refuses", 1200, SpendLimits{Windows: []QuotaWindow{
			{Name: "w", Size: 5 * time.Hour, MaxTokens: 1000, MaxEquivalentMicros: 5000},
		}}, false, QuotaReasonTokenLimit, QuotaAxisTokens},
		{"both axes: the dollar axis refuses first when it is the spent one", 3000, SpendLimits{Windows: []QuotaWindow{
			{Name: "w", Size: 5 * time.Hour, MaxTokens: 100000, MaxEquivalentMicros: 2500},
		}}, false, QuotaReasonCostLimit, QuotaAxisEquivalentMicros},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := NewInMemoryConsumptionLedger()
			spend(t, ledger, tc.spent, quotaNow.Add(-time.Hour), "e1")
			decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
				Account: quotaAccount(), Policy: tc.limits,
			}, quotaNow)
			if err != nil {
				t.Fatalf("Admit: %v", err)
			}
			if decision.Allowed != tc.wantAllow || decision.Reason != tc.wantReason || decision.Axis != tc.wantAxis {
				t.Fatalf("decision = %+v, want allowed=%t reason=%s axis=%s", decision, tc.wantAllow, tc.wantReason, tc.wantAxis)
			}
		})
	}
}

// TestQuotaAuthorityDollarAxisIgnoresUnpricedTurns states the rule a dollar cap
// is documented by: it meters priced turns only.
func TestQuotaAuthorityDollarAxisIgnoresUnpricedTurns(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	entry := validEntry()
	entry.InputTokens, entry.EquivalentMicros, entry.PriceRef = 1_000_000, 0, ""
	entry.OccurredAt = quotaNow.Add(-time.Hour)
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("Record: %v", err)
	}
	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(), Policy: fiveHourDollars(1),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("an unpriced turn moved a dollar window: %+v", decision)
	}
}

func TestSpendLimitsValidation(t *testing.T) {
	tests := []struct {
		name   string
		window QuotaWindow
		field  string
	}{
		{"a window metering nothing", QuotaWindow{Name: "w", Size: time.Hour}, "window.max_tokens"},
		{"a negative token cap", QuotaWindow{Name: "w", Size: time.Hour, MaxTokens: -1, MaxEquivalentMicros: 5}, "window.max_tokens"},
		{"a negative dollar cap", QuotaWindow{Name: "w", Size: time.Hour, MaxTokens: 5, MaxEquivalentMicros: -1}, "window.max_equivalent_micros"},
		{"a dollar-only window", QuotaWindow{Name: "w", Size: time.Hour, MaxEquivalentMicros: 5}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := SpendLimits{Windows: []QuotaWindow{tc.window}}.validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("validate = %v, want nil", err)
				}
				return
			}
			var invalid *QuotaInvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("validate = %v, want a QuotaInvalidError on %s", err, tc.field)
			}
		})
	}
}

// TestQuotaAuthorityAdmitScope judges a project and a tenant from their own
// quota planes: an unpooled local turn counts against both, and another
// project's spend counts against the tenant only.
func TestQuotaAuthorityAdmitScope(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()
	local := validEntry()
	local.EntryID, local.AccountID, local.ProjectID = "local", "", "p1"
	local.InputTokens, local.OutputTokens = 600, 0
	local.OccurredAt = quotaNow.Add(-time.Hour)
	other := validEntry()
	other.EntryID, other.ProjectID = "other", "p2"
	other.InputTokens, other.OutputTokens = 600, 0
	other.OccurredAt = quotaNow.Add(-time.Hour)
	for _, e := range []ConsumptionEntry{local, other} {
		if err := ledger.Record(ctx, e); err != nil {
			t.Fatalf("Record %s: %v", e.EntryID, err)
		}
	}
	authority := NewQuotaAuthority(ledger)

	project, err := authority.AdmitScope(ctx, ScopeAdmitRequest{TenantID: "t1", ProjectID: "p1", Limits: fiveHourTokens(1000)}, quotaNow)
	if err != nil {
		t.Fatalf("AdmitScope(project): %v", err)
	}
	if !project.Allowed {
		t.Fatalf("p1 spent 600 of 1000 and was refused: %+v", project)
	}
	tenant, err := authority.AdmitScope(ctx, ScopeAdmitRequest{TenantID: "t1", Limits: fiveHourTokens(1000)}, quotaNow)
	if err != nil {
		t.Fatalf("AdmitScope(tenant): %v", err)
	}
	if tenant.Allowed || tenant.Used != 1200 || tenant.Reason != QuotaReasonTokenLimit {
		t.Fatalf("tenant spent 1200 of 1000 and got %+v", tenant)
	}
	none, err := authority.AdmitScope(ctx, ScopeAdmitRequest{TenantID: "t1"}, quotaNow)
	if err != nil || !none.Allowed {
		t.Fatalf("no limits = %+v, %v; want admitted", none, err)
	}
}

func TestQuotaAuthorityAdmitScopeRefusesBadInput(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	tests := []struct {
		name      string
		authority QuotaAuthority
		req       ScopeAdmitRequest
		field     string
	}{
		{"no ledger", NewQuotaAuthority(nil), ScopeAdmitRequest{TenantID: "t1"}, "ledger"},
		{"no tenant", NewQuotaAuthority(ledger), ScopeAdmitRequest{}, "tenant_id"},
		{"a negative estimate", NewQuotaAuthority(ledger), ScopeAdmitRequest{TenantID: "t1", EstimatedTokens: -1}, "estimated_tokens"},
		{"a malformed window", NewQuotaAuthority(ledger), ScopeAdmitRequest{TenantID: "t1",
			Limits: SpendLimits{Windows: []QuotaWindow{{Name: "w", Size: time.Hour}}}}, "window.max_tokens"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := tc.authority.AdmitScope(context.Background(), tc.req, quotaNow)
			var invalid *QuotaInvalidError
			if decision != nil || !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("AdmitScope = %+v, %v; want no decision and a QuotaInvalidError on %s", decision, err, tc.field)
			}
		})
	}
}

// TestSpendAuthorityRefusesASpentDollarWindow asserts the gate turns a spent
// dollar window into the same typed refusal as a token one, naming the axis.
func TestSpendAuthorityRefusesASpentDollarWindow(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	spend(t, ledger, 5000, time.Now().UTC().Add(-time.Minute), "e1")
	account := quotaAccount()
	account.Spend = AccountSpendPolicy{Quota: fiveHourDollars(5000)}

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: account}, time.Now().UTC())
	var exhausted *AccountQuotaExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Axis != QuotaAxisEquivalentMicros || exhausted.Used != 5000 {
		t.Fatalf("Authorize = %v, want an AccountQuotaExhaustedError on the dollar axis", err)
	}
}

// TestSpendWindowStatusReportsBothAxes asserts the report reads a dollar
// window the way the gate does: exhausted when either metered axis is spent.
func TestSpendWindowStatusReportsBothAxes(t *testing.T) {
	read := func(time.Time, time.Time) (*ConsumptionRollup, error) {
		rollup := newConsumptionRollup(ConsumptionScope{TenantID: "t1"})
		rollup.TotalTokens, rollup.EquivalentMicros = 100, 9_000
		return rollup, nil
	}
	status, err := spendWindowStatus(QuotaWindow{Name: "day", Size: 24 * time.Hour, MaxTokens: 1000, MaxEquivalentMicros: 9_000}, quotaNow, read)
	if err != nil {
		t.Fatalf("spendWindowStatus: %v", err)
	}
	if status.RemainingTokens != 900 || status.UsedEquivalentMicros != 9_000 || status.RemainingEquivalentMicros != 0 || !status.Exhausted {
		t.Fatalf("status = %+v, want tokens left but the dollars spent, and exhausted", status)
	}

	dollarsOnly, err := spendWindowStatus(QuotaWindow{Name: "day", Size: 24 * time.Hour, MaxEquivalentMicros: 20_000}, quotaNow, read)
	if err != nil {
		t.Fatalf("spendWindowStatus: %v", err)
	}
	if dollarsOnly.Exhausted || dollarsOnly.RemainingTokens != 0 || dollarsOnly.RemainingEquivalentMicros != 11_000 {
		t.Fatalf("status = %+v, want an unexhausted dollar window with no token axis", dollarsOnly)
	}
}

func TestSpendWindowStatusFailsWithTheLedger(t *testing.T) {
	boom := errors.New("ledger down")
	_, err := spendWindowStatus(QuotaWindow{Name: "day", Size: time.Hour, MaxTokens: 1}, quotaNow,
		func(time.Time, time.Time) (*ConsumptionRollup, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the ledger's failure", err)
	}
}
