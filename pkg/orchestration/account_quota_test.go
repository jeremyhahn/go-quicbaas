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
	"strings"
	"testing"
	"time"
)

// quotaNow is the instant every admission is asked at.
var quotaNow = ledgerBase

// quotaAccount returns an unheld, active account.
func quotaAccount() *Account {
	return &Account{
		ID:       "a1",
		TenantID: "t1",
		Provider: AccountProviderClaude,
		Status:   AccountStatusActive,
	}
}

// fiveHourTokens is a single-window policy metering tokens only.
func fiveHourTokens(max int64) SpendLimits {
	return SpendLimits{Windows: []QuotaWindow{
		{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: max},
	}}
}

// spend records n entries of the given size into the window ending at quotaNow.
func spend(t *testing.T, ledger ConsumptionLedger, tokens int64, at time.Time, id string) {
	t.Helper()
	entry := validEntry()
	entry.EntryID = id
	entry.InputTokens = tokens
	entry.OutputTokens = 0
	entry.EquivalentMicros = tokens // one micro per token keeps the two axes separable
	entry.OccurredAt = at
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// =============================================================================
// Admission
// =============================================================================

func TestQuotaAuthorityAdmitsUnderTheLimit(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	spend(t, ledger, 400, quotaNow.Add(-time.Hour), "e1")

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("400 of 1000 must be admitted, got %s", decision)
	}
	if decision.Reason != QuotaReasonWithinLimits {
		t.Fatalf("reason: got %q, want %q", decision.Reason, QuotaReasonWithinLimits)
	}
}

func TestQuotaAuthorityRefusesASpentWindow(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	oldest := quotaNow.Add(-4 * time.Hour)
	spend(t, ledger, 900, oldest, "e1")
	spend(t, ledger, 200, quotaNow.Add(-time.Hour), "e2")

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("1100 of 1000 must be refused, got %s", decision)
	}
	if decision.Reason != QuotaReasonTokenLimit {
		t.Fatalf("reason: got %q, want %q", decision.Reason, QuotaReasonTokenLimit)
	}
	if decision.Window != "five-hour" {
		t.Fatalf("a refusal must name the limit it hit, got %q", decision.Window)
	}
	if decision.Used != 1100 || decision.Limit != 1000 {
		t.Fatalf("used/limit: got %d/%d, want 1100/1000", decision.Used, decision.Limit)
	}
	// Already over: the remainder is clamped rather than reported negative,
	// which arithmetic downstream would read as headroom in the other
	// direction.
	if decision.Remaining != 0 {
		t.Fatalf("remaining must clamp at zero, got %d", decision.Remaining)
	}
	// A sliding window frees up when its oldest entry ages out.
	if want := oldest.Add(5 * time.Hour); !decision.RetryAt.Equal(want) {
		t.Fatalf("RetryAt: got %s, want %s", decision.RetryAt, want)
	}
}

// TestQuotaAuthorityBoundary pins both sides of the comparison. Usage exactly
// at the limit is admitted and one token past it is refused; a fixture that
// only ever sat far from the boundary would agree with an off-by-one.
func TestQuotaAuthorityBoundary(t *testing.T) {
	tests := []struct {
		name        string
		used        int64
		wantAllowed bool
	}{
		{"just under", 999, true},
		{"exactly at the limit", 1000, true},
		{"one past", 1001, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := NewInMemoryConsumptionLedger()
			spend(t, ledger, tc.used, quotaNow.Add(-time.Hour), "e1")

			decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
				Account: quotaAccount(),
				Policy:  fiveHourTokens(1000),
			}, quotaNow)
			if err != nil {
				t.Fatalf("Admit: %v", err)
			}
			if decision.Allowed != tc.wantAllowed {
				t.Fatalf("used %d against limit 1000: allowed=%v, want %v (%s)",
					tc.used, decision.Allowed, tc.wantAllowed, decision)
			}
		})
	}
}

// TestQuotaAuthorityCountsTheEstimate proves the turn about to run is weighed
// before it runs. Usage alone is under the limit in both cases; only the
// estimate separates them.
func TestQuotaAuthorityCountsTheEstimate(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	spend(t, ledger, 900, quotaNow.Add(-time.Hour), "e1")
	authority := NewQuotaAuthority(ledger)

	withoutEstimate, err := authority.Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !withoutEstimate.Allowed {
		t.Fatalf("900 of 1000 with no estimate must be admitted, got %s", withoutEstimate)
	}

	withEstimate, err := authority.Admit(context.Background(), QuotaAdmitRequest{
		Account:         quotaAccount(),
		Policy:          fiveHourTokens(1000),
		EstimatedTokens: 200,
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if withEstimate.Allowed {
		t.Fatalf("900 spent plus a 200-token turn exceeds 1000 and must be refused, got %s", withEstimate)
	}
	// The report is what was SPENT, not what was projected: an operator
	// reading "used 1100" against a ledger holding 900 could not reconcile it.
	if withEstimate.Used != 900 {
		t.Fatalf("Used must report actual spend, got %d, want 900", withEstimate.Used)
	}
}

func TestQuotaAuthorityWindowIsSliding(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	// Spent six hours ago: outside a five-hour window and inside a weekly one.
	spend(t, ledger, 5000, quotaNow.Add(-6*time.Hour), "old")
	authority := NewQuotaAuthority(ledger)

	fiveHour, err := authority.Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !fiveHour.Allowed {
		t.Fatalf("spend older than the window must have aged out, got %s", fiveHour)
	}

	weekly, err := authority.Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy: SpendLimits{Windows: []QuotaWindow{
			{Name: "weekly", Size: 7 * 24 * time.Hour, MaxTokens: 1000},
		}},
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if weekly.Allowed {
		t.Fatalf("the same spend is inside a weekly window and must refuse, got %s", weekly)
	}
}

func TestQuotaAuthorityReturnsTheFirstRefusingWindow(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	spend(t, ledger, 5000, quotaNow.Add(-time.Hour), "e1")

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy: SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
			{Name: "weekly", Size: 7 * 24 * time.Hour, MaxTokens: 2000},
		}},
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Window != "five-hour" {
		t.Fatalf("windows are evaluated in order; got %q, want %q", decision.Window, "five-hour")
	}
}

func TestQuotaAuthorityChecksEveryWindow(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()
	// Under the five-hour cap, over the weekly one: only the SECOND window can
	// produce this refusal, so a loop that stopped after the first would admit.
	spend(t, ledger, 900, quotaNow.Add(-time.Hour), "recent")
	spend(t, ledger, 900, quotaNow.Add(-48*time.Hour), "older")

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy: SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000},
			{Name: "weekly", Size: 7 * 24 * time.Hour, MaxTokens: 1500},
		}},
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("1800 over a weekly cap of 1500 must refuse, got %s", decision)
	}
	if decision.Window != "weekly" {
		t.Fatalf("the refusing window is the weekly one, got %q", decision.Window)
	}
}

// =============================================================================
// The provider's hold outranks arithmetic
// =============================================================================

func TestQuotaAuthorityHonorsTheProviderHold(t *testing.T) {
	ledger := &countingLedger{ConsumptionLedger: NewInMemoryConsumptionLedger()}
	held := quotaAccount()
	held.Status = AccountStatusLimited
	held.AvailableAt = quotaNow.Add(time.Hour)

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: held,
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("an account the provider is holding must be refused, got %s", decision)
	}
	if decision.Reason != QuotaReasonProviderHold {
		t.Fatalf("reason: got %q, want %q", decision.Reason, QuotaReasonProviderHold)
	}
	if !decision.RetryAt.Equal(held.AvailableAt) {
		t.Fatalf("RetryAt must be the account's own AvailableAt, got %s", decision.RetryAt)
	}
	// Our arithmetic cannot overrule the provider, so it is not even consulted.
	if ledger.windowReads != 0 {
		t.Fatalf("a held account must not be re-litigated against the ledger: %d reads", ledger.windowReads)
	}
}

func TestQuotaAuthorityProceedsPastALapsedHold(t *testing.T) {
	// The permitting direction of the hold check: a limited account whose hold
	// has lapsed is judged on consumption like any other. A hold test written
	// only against a live hold would pass with a check that refused every
	// limited account forever.
	ledger := &countingLedger{ConsumptionLedger: NewInMemoryConsumptionLedger()}
	lapsed := quotaAccount()
	lapsed.Status = AccountStatusLimited
	lapsed.AvailableAt = quotaNow.Add(-time.Minute)

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: lapsed,
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("a lapsed hold must not keep refusing, got %s", decision)
	}
	if ledger.windowReads == 0 {
		t.Fatal("a lapsed hold must fall through to the consumption check")
	}
}

// =============================================================================
// Fail closed, and never with a number
// =============================================================================

func TestQuotaAuthorityRefusesToGuessWhenTheLedgerFails(t *testing.T) {
	ledger := &countingLedger{
		ConsumptionLedger: NewInMemoryConsumptionLedger(),
		windowErr:         stderrors.New("qrdb unavailable"),
	}

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)

	// The failure mode this whole file exists to prevent: an unreadable ledger
	// reported as an account with no spend and full headroom.
	if decision != nil {
		t.Fatalf("an unreadable ledger must produce NO decision, got %s", decision)
	}
	if err == nil {
		t.Fatal("an unreadable ledger must be an error, never an admission")
	}
	if !strings.Contains(err.Error(), "qrdb unavailable") {
		t.Fatalf("the cause must survive to the caller, got: %v", err)
	}
}

func TestQuotaAuthorityRefusesMalformedRequests(t *testing.T) {
	ledger := NewInMemoryConsumptionLedger()

	tests := []struct {
		name  string
		req   QuotaAdmitRequest
		field string
	}{
		{"nil account", QuotaAdmitRequest{Policy: fiveHourTokens(10)}, "account"},
		{"negative token estimate", QuotaAdmitRequest{
			Account: quotaAccount(), Policy: fiveHourTokens(10), EstimatedTokens: -1,
		}, "estimated_tokens"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), tc.req, quotaNow)
			if decision != nil {
				t.Fatalf("a question this authority cannot answer must yield no decision, got %s", decision)
			}
			var invalid *QuotaInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want QuotaInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}

	if _, err := NewQuotaAuthority(nil).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(), Policy: fiveHourTokens(10),
	}, quotaNow); err == nil {
		t.Fatal("an authority with no ledger must refuse rather than admit")
	}
}

// =============================================================================
// Policy validation, both directions
// =============================================================================

func TestQuotaPolicyValidation(t *testing.T) {
	refused := []struct {
		name   string
		policy SpendLimits
		field  string
		// names is the window name the message must carry. Empty for the
		// unnamed-window case, which is refused precisely because there is no
		// name to report.
		names string
	}{
		{"unnamed window", SpendLimits{Windows: []QuotaWindow{
			{Size: time.Hour, MaxTokens: 1},
		}}, "window.name", ""},
		{"duplicate name", SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: time.Hour, MaxTokens: 1},
			{Name: "five-hour", Size: 2 * time.Hour, MaxTokens: 2},
		}}, "window.name", "five-hour"},
		{"zero size", SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: 0, MaxTokens: 1},
		}}, "window.size", "five-hour"},
		{"negative size", SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: -time.Hour, MaxTokens: 1},
		}}, "window.size", "five-hour"},
		{"negative token limit", SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: time.Hour, MaxTokens: -1},
		}}, "window.max_tokens", "five-hour"},
		// A window with no allowance reads as a limit and is not one.
		{"meters nothing", SpendLimits{Windows: []QuotaWindow{
			{Name: "five-hour", Size: time.Hour},
		}}, "window.max_tokens", "five-hour"},
	}

	ledger := NewInMemoryConsumptionLedger()
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
				Account: quotaAccount(), Policy: tc.policy,
			}, quotaNow)
			if decision != nil {
				t.Fatalf("a malformed policy must yield no decision, got %s", decision)
			}
			var invalid *QuotaInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want QuotaInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
			if tc.names != "" && !strings.Contains(invalid.Message, tc.names) {
				t.Fatalf("refusal must name the window %q, got %q", tc.names, invalid.Message)
			}
		})
	}

	// The permitting direction. Each of these sits on the other side of a rule
	// above, so a validation tightened into refusing everything fails here.
	accepted := []struct {
		name   string
		policy SpendLimits
	}{
		{"a token allowance", fiveHourTokens(1000)},
		{"two distinctly named windows", SpendLimits{Windows: []QuotaWindow{
			{Name: "a", Size: time.Hour, MaxTokens: 1000},
			{Name: "b", Size: 2 * time.Hour, MaxTokens: 2000},
		}}},
		// No windows at all: nothing is metered, and admitting is the honest
		// answer for an account nobody has configured a limit for. Refusing
		// would take the whole pool down the moment a policy went missing.
		{"no windows", SpendLimits{}},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
				Account: quotaAccount(), Policy: tc.policy,
			}, quotaNow)
			if err != nil {
				t.Fatalf("policy must be accepted, got: %v", err)
			}
			if !decision.Allowed {
				t.Fatalf("an unspent account must be admitted, got %s", decision)
			}
		})
	}
}

// =============================================================================
// Reporting
// =============================================================================

func TestQuotaDecisionRetryAtIsAbsentWhenNothingWillAgeOut(t *testing.T) {
	// Nothing has been spent; the estimate alone exceeds the limit. There is
	// no oldest entry to age out, so no retry time can honestly be named —
	// and a zero here means "there is no such moment", not "retry at the
	// epoch".
	ledger := NewInMemoryConsumptionLedger()

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account:         quotaAccount(),
		Policy:          fiveHourTokens(100),
		EstimatedTokens: 500,
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("a turn larger than the whole window must be refused, got %s", decision)
	}
	if !decision.RetryAt.IsZero() {
		t.Fatalf("nothing will age out, so no retry time may be invented; got %s", decision.RetryAt)
	}
	if decision.Remaining != 100 {
		t.Fatalf("an unspent window has its whole allowance remaining, got %d", decision.Remaining)
	}
}

func TestQuotaDecisionRetryAtIsAbsentWhenTheOldestAgesOutNow(t *testing.T) {
	// The oldest entry sits exactly on the window's lower edge, so it ages out
	// at this instant: oldest+Size == now. A retry time equal to now is not a
	// future moment, and reporting it would have callers sleep until a
	// deadline that has already passed.
	ledger := NewInMemoryConsumptionLedger()
	spend(t, ledger, 1500, quotaNow.Add(-5*time.Hour), "edge")

	decision, err := NewQuotaAuthority(ledger).Admit(context.Background(), QuotaAdmitRequest{
		Account: quotaAccount(),
		Policy:  fiveHourTokens(1000),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("1500 of 1000 must be refused, got %s", decision)
	}
	if !decision.RetryAt.IsZero() {
		t.Fatalf("a retry time that is not in the future must be omitted, got %s", decision.RetryAt)
	}
}

func TestQuotaDecisionString(t *testing.T) {
	var absent *QuotaDecision
	if !strings.Contains(absent.String(), "no decision") {
		t.Fatalf("a nil decision must say so rather than panic, got %q", absent.String())
	}

	refusal := &QuotaDecision{
		Allowed: false, Reason: QuotaReasonTokenLimit, Window: "five-hour",
		Used: 1100, Limit: 1000,
	}
	rendered := refusal.String()
	for _, want := range []string{"token_limit", "five-hour", "1100", "1000"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered decision %q must carry %q", rendered, want)
		}
	}
}

func TestQuotaInvalidErrorUnwraps(t *testing.T) {
	err := &QuotaInvalidError{Field: "window.size", Message: "must be positive"}
	if !stderrors.Is(err, ErrQuotaInvalid) {
		t.Fatal("must unwrap to ErrQuotaInvalid")
	}
	// Distinct from the ledger's own vocabulary: a caller switching on
	// sentinels must not confuse a malformed policy with an unreadable ledger.
	if stderrors.Is(err, ErrConsumptionInvalid) {
		t.Fatal("quota and consumption sentinels must stay distinct")
	}
	if !strings.Contains(err.Error(), "window.size") {
		t.Fatalf("message must name the field, got %q", err.Error())
	}
}

// =============================================================================
// Fakes
// =============================================================================

// countingLedger records how often the quota plane was read and can fail it.
type countingLedger struct {
	ConsumptionLedger

	windowReads int
	windowErr   error
}

func (l *countingLedger) WindowUsage(ctx context.Context, tenantID, accountID string, from, to time.Time) (*ConsumptionRollup, error) {
	l.windowReads++
	if l.windowErr != nil {
		return nil, l.windowErr
	}
	return l.ConsumptionLedger.WindowUsage(ctx, tenantID, accountID, from, to)
}
