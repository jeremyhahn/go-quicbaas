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

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// spendHolder is the host identity every authority under test presents on a
// half-open probe.
const spendHolder = "node-a"

// newSpendFixture returns an authority, the ledger it decides from and the KV
// beneath both, so a test can seed consumption and inspect the probe.
func newSpendFixture(t *testing.T) (SpendAuthority, ConsumptionLedger, interfaces.KVStore) {
	t.Helper()
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	authority, err := NewSpendAuthority(ledger, kv, spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	return authority, ledger, kv
}

// spendAccount returns an active account carrying policy.
func spendAccount(policy AccountSpendPolicy) *Account {
	account := quotaAccount()
	account.Spend = policy
	return account
}

// =============================================================================
// Construction
// =============================================================================

func TestNewSpendAuthorityBuildsFromALedgerAndAStore(t *testing.T) {
	kv := NewMemoryTaskKV()
	authority, err := NewSpendAuthority(NewConsumptionLedger(kv), kv, spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	if authority == nil {
		t.Fatal("NewSpendAuthority returned no authority and no error")
	}
}

func TestNewSpendAuthorityRefusesAnUnusableConfiguration(t *testing.T) {
	kv := NewMemoryTaskKV()
	cases := []struct {
		name   string
		ledger ConsumptionLedger
		kv     interfaces.KVStore
		holder string
		field  string
	}{
		{"no ledger is no authority", nil, kv, spendHolder, "ledger"},
		{"no store cannot arbitrate a probe", NewConsumptionLedger(kv), nil, spendHolder, "kv"},
		{"an anonymous holder arbitrates nothing", NewConsumptionLedger(kv), kv, "", "holder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authority, err := NewSpendAuthority(tc.ledger, tc.kv, tc.holder)
			if err == nil {
				t.Fatal("NewSpendAuthority = nil error; a gate built without what it needs would admit everything")
			}
			if authority != nil {
				t.Error("NewSpendAuthority returned an authority alongside its error")
			}
			var cfg *SpendAuthorityConfigError
			if !stderrors.As(err, &cfg) {
				t.Fatalf("error = %v, want a *SpendAuthorityConfigError", err)
			}
			if cfg.Field != tc.field {
				t.Errorf("Field = %q, want %q", cfg.Field, tc.field)
			}
		})
	}
}

// =============================================================================
// Admission
// =============================================================================

func TestSpendAuthorityAdmitsAnUnmeteredAccount(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	// Consumption far beyond anything a policy would allow. It must not
	// matter: an account nobody configured a limit for has no limit, and
	// inventing one would refuse work the provider would have served.
	spend(t, ledger, 1_000_000, quotaNow.Add(-time.Minute), "e1")

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: spendAccount(AccountSpendPolicy{})}, quotaNow)
	if err != nil {
		t.Fatalf("Authorize on an unmetered account = %v, want nil", err)
	}
}

func TestSpendAuthorityAdmitsUnderTheCap(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	spend(t, ledger, 400, quotaNow.Add(-time.Hour), "e1")

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{
		Account: spendAccount(AccountSpendPolicy{Quota: fiveHourTokens(1000)}),
	}, quotaNow)
	if err != nil {
		t.Fatalf("Authorize under the cap = %v, want nil", err)
	}
}

func TestSpendAuthorityRefusesASpentTokenWindow(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	spend(t, ledger, 1200, quotaNow.Add(-time.Hour), "e1")

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{
		Account: spendAccount(AccountSpendPolicy{Quota: fiveHourTokens(1000)}),
	}, quotaNow)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("Authorize over the cap = %v, want an *AccountQuotaExhaustedError", err)
	}
	if !stderrors.Is(err, ErrAccountQuotaExhausted) {
		t.Error("the refusal does not satisfy errors.Is(ErrAccountQuotaExhausted); every consumer keys on the sentinel")
	}
	if exhausted.Axis != QuotaAxisTokens {
		t.Errorf("Axis = %q, want %q", exhausted.Axis, QuotaAxisTokens)
	}
	if exhausted.Window != "five-hour" {
		t.Errorf("Window = %q, want the window that refused", exhausted.Window)
	}
	if exhausted.Used != 1200 || exhausted.Limit != 1000 {
		t.Errorf("used/limit = %d/%d, want 1200/1000 — a refusal that cannot show its working is indistinguishable from a broken gate",
			exhausted.Used, exhausted.Limit)
	}
	if exhausted.RetryAt.IsZero() {
		t.Error("RetryAt is zero; a sliding window frees up at a stateable moment and the task waits exactly that long")
	}
}

func TestSpendAuthorityReportsAProviderHoldInThePoolsOwnVocabulary(t *testing.T) {
	authority, _, _ := newSpendFixture(t)
	account := spendAccount(AccountSpendPolicy{Quota: fiveHourTokens(1000)})
	account.Status = AccountStatusLimited
	account.AvailableAt = quotaNow.Add(time.Hour)

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: account}, quotaNow)
	var limited *AccountLimitedError
	if !stderrors.As(err, &limited) {
		t.Fatalf("Authorize on a held account = %v, want the pool's own *AccountLimitedError; "+
			"a second name for one state is two things to keep in step", err)
	}
	if !limited.AvailableAt.Equal(account.AvailableAt) {
		t.Errorf("AvailableAt = %s, want the account's own %s", limited.AvailableAt, account.AvailableAt)
	}
}

func TestSpendAuthorityCountsTheEstimateBeforeAdmitting(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	spend(t, ledger, 900, quotaNow.Add(-time.Hour), "e1")
	policy := AccountSpendPolicy{Quota: fiveHourTokens(1000)}

	// Already under: the account itself has headroom.
	if err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: spendAccount(policy)}, quotaNow); err != nil {
		t.Fatalf("Authorize with no estimate = %v, want nil", err)
	}
	// A caller that names what it is about to spend is refused BEFORE it
	// runs, which is the whole difference between a cap and a report.
	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{
		Account:         spendAccount(policy),
		EstimatedTokens: 200,
	}, quotaNow)
	if !stderrors.Is(err, ErrAccountQuotaExhausted) {
		t.Fatalf("Authorize with an estimate that crosses the cap = %v, want a quota refusal", err)
	}
}

// =============================================================================
// The breaker half
// =============================================================================

func TestSpendAuthorityRefusesAnOpenBreaker(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	policy := AccountSpendPolicy{Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute}}
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-time.Minute))

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: spendAccount(policy)}, quotaNow)
	var open *AccountBreakerOpenError
	if !stderrors.As(err, &open) {
		t.Fatalf("Authorize on a tripped breaker = %v, want an *AccountBreakerOpenError", err)
	}
	if open.Failures != 2 {
		t.Errorf("Failures = %d, want 2", open.Failures)
	}
	if open.OpenUntil.IsZero() {
		t.Error("OpenUntil is zero; an open breaker becomes half-open at a stateable moment")
	}
}

func TestSpendAuthorityAdmitsTheHalfOpenTrialToExactlyOneHolder(t *testing.T) {
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	first, err := NewSpendAuthority(ledger, kv, "node-a")
	if err != nil {
		t.Fatalf("NewSpendAuthority(node-a): %v", err)
	}
	second, err := NewSpendAuthority(ledger, kv, "node-b")
	if err != nil {
		t.Fatalf("NewSpendAuthority(node-b): %v", err)
	}

	// Two failures an hour ago, a ten-minute cooldown: long elapsed, so the
	// breaker is half-open rather than open.
	policy := AccountSpendPolicy{Breaker: &BreakerPolicy{Window: 4 * time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute}}
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Hour))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-90*time.Minute))

	req := SpendAuthorizeRequest{Account: spendAccount(policy)}
	if err := first.Authorize(context.Background(), req, quotaNow); err != nil {
		t.Fatalf("the first caller on a half-open breaker = %v, want the trial", err)
	}
	err = second.Authorize(context.Background(), req, quotaNow)
	var held *AccountBreakerTrialHeldError
	if !stderrors.As(err, &held) {
		t.Fatalf("the second caller = %v, want an *AccountBreakerTrialHeldError; "+
			"two hosts each believing they hold the single trial is the failure the probe exists to prevent", err)
	}
	// Deliberately NOT the open-breaker error: the account may be healthy and
	// about to be proved so, and reporting a cooldown that is not running
	// would send a reader to wait for a moment that never arrives.
	if stderrors.Is(err, ErrAccountBreakerOpen) {
		t.Error("a held trial reported itself as an open breaker")
	}
}

func TestSpendAuthorityTrialIsReentrantForItsHolder(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	policy := AccountSpendPolicy{Breaker: &BreakerPolicy{Window: 4 * time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute}}
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Hour))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-90*time.Minute))

	req := SpendAuthorizeRequest{Account: spendAccount(policy)}
	if err := authority.Authorize(context.Background(), req, quotaNow); err != nil {
		t.Fatalf("first authorize = %v, want the trial", err)
	}
	// A host that asks again while holding its own probe must not be told it
	// lost a claim it still has: a resolver resolves twice for one job.
	if err := authority.Authorize(context.Background(), req, quotaNow); err != nil {
		t.Fatalf("second authorize by the same holder = %v, want nil", err)
	}
}

func TestSpendAuthorityAdmitsAClosedBreaker(t *testing.T) {
	authority, ledger, _ := newSpendFixture(t)
	policy := AccountSpendPolicy{Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute}}
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Minute))
	record(t, ledger, "s1", ConsumptionOutcomeSuccess, quotaNow.Add(-time.Minute))

	if err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: spendAccount(policy)}, quotaNow); err != nil {
		t.Fatalf("Authorize below the threshold = %v, want nil", err)
	}
}

// =============================================================================
// Failing closed
// =============================================================================

func TestSpendAuthorityRefusesToDecideWhenTheLedgerCannotBeRead(t *testing.T) {
	kv := newChainFaultKV()
	kv.failChain = false
	kv.scanErr = stderrors.New("store unavailable")
	authority, err := NewSpendAuthority(NewConsumptionLedger(kv), kv, spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}

	authErr := authority.Authorize(context.Background(), SpendAuthorizeRequest{
		Account: spendAccount(AccountSpendPolicy{Quota: fiveHourTokens(1000)}),
	}, quotaNow)
	if authErr == nil {
		t.Fatal("Authorize = nil on an unreadable ledger; a gate that admits when it cannot see " +
			"opens exactly when the store is in trouble, and the first evidence is the invoice")
	}
	if !stderrors.Is(authErr, ErrConsumptionRead) {
		t.Errorf("error = %v, want the ledger's own read failure rather than a spend verdict", authErr)
	}
	// A read failure is NOT a refusal: a caller that treated it as one would
	// report a cap that is not spent, and an operator would raise a limit
	// that was never reached.
	if stderrors.Is(authErr, ErrAccountQuotaExhausted) {
		t.Error("an unreadable ledger was reported as a spent quota")
	}
}

func TestSpendAuthorityRefusesAQuestionWithNoAccount(t *testing.T) {
	authority, _, _ := newSpendFixture(t)
	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{}, quotaNow)
	var cfg *SpendAuthorityConfigError
	if !stderrors.As(err, &cfg) {
		t.Fatalf("Authorize with no account = %v, want a *SpendAuthorityConfigError", err)
	}
	if cfg.Field != "account" {
		t.Errorf("Field = %q, want \"account\"", cfg.Field)
	}
}

func TestSpendAuthorityRefusesAMalformedPolicy(t *testing.T) {
	authority, _, _ := newSpendFixture(t)
	// A window metering neither axis reaches the gate only when it was
	// stored around the account store's validation — a hand-built record, a
	// downgrade. Refusing here too means the gate is not relying on somebody
	// else having checked.
	account := spendAccount(AccountSpendPolicy{Quota: SpendLimits{Windows: []QuotaWindow{
		{Name: "broken", Size: time.Hour},
	}}})

	err := authority.Authorize(context.Background(), SpendAuthorizeRequest{Account: account}, quotaNow)
	if !stderrors.Is(err, ErrQuotaInvalid) {
		t.Fatalf("Authorize with a malformed policy = %v, want a quota validation error and NO admission", err)
	}
}

// =============================================================================
// Error surfaces
// =============================================================================

func TestSpendErrorMessagesNameTheirSubject(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		err      error
		sentinel error
		contains []string
	}{
		{
			name: "a spent window names the window, the axis and the figures",
			err: &AccountQuotaExhaustedError{
				AccountID: "a1", Window: "five-hour", Axis: QuotaAxisTokens,
				Used: 1200, Limit: 1000, RetryAt: at,
			},
			sentinel: ErrAccountQuotaExhausted,
			contains: []string{"a1", "five-hour", "tokens", "1200", "1000", "2026-08-22T12:00:00Z"},
		},
		{
			name:     "a spent window with nothing to age out states no moment",
			err:      &AccountQuotaExhaustedError{AccountID: "a1", Window: "weekly", Axis: QuotaAxisTokens, Limit: 10},
			sentinel: ErrAccountQuotaExhausted,
			contains: []string{"a1", "weekly", "tokens"},
		},
		{
			name:     "an open breaker names the failures and when it is retried",
			err:      &AccountBreakerOpenError{AccountID: "a2", Failures: 5, OpenUntil: at},
			sentinel: ErrAccountBreakerOpen,
			contains: []string{"a2", "5", "2026-08-22T12:00:00Z"},
		},
		{
			name:     "a held trial names the account and the failures",
			err:      &AccountBreakerTrialHeldError{AccountID: "a3", Failures: 2},
			sentinel: ErrAccountBreakerTrialHeld,
			contains: []string{"a3", "2", "another caller"},
		},
		{
			name:     "an unrecognized verdict names it rather than guessing",
			err:      &AccountSpendUnknownVerdictError{AccountID: "a4", Reason: "invented"},
			sentinel: ErrAccountSpendUnknownVerdict,
			contains: []string{"a4", "invented"},
		},
		{
			name:     "a config fault names the field",
			err:      &SpendAuthorityConfigError{Field: "ledger"},
			sentinel: ErrSpendAuthorityConfig,
			contains: []string{"ledger", "required"},
		},
		{
			name:     "a config fault with a message states it",
			err:      &SpendAuthorityConfigError{Field: "holder", Message: "must name this host"},
			sentinel: ErrSpendAuthorityConfig,
			contains: []string{"holder", "must name this host"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !stderrors.Is(tc.err, tc.sentinel) {
				t.Errorf("errors.Is(%v) = false, want true", tc.sentinel)
			}
			msg := tc.err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not contain %q", msg, want)
				}
			}
		})
	}
}

func TestQuotaRefusalsCoverEveryRefusingReason(t *testing.T) {
	// The gate refuses anything it cannot explain, so a reason added to the
	// quota authority and forgotten here fails closed rather than silently
	// admitting. This asserts the table is nonetheless complete, because
	// "fails closed" would otherwise park a healthy account and look like an
	// outage.
	refusing := []QuotaReason{QuotaReasonProviderHold, QuotaReasonTokenLimit}
	for _, reason := range refusing {
		if _, ok := quotaRefusalsByReason[reason]; !ok {
			t.Errorf("quota reason %q has no refusal; the gate would refuse it as unexplainable", reason)
		}
	}
	if _, ok := quotaRefusalsByReason[QuotaReasonWithinLimits]; ok {
		t.Error("the admitting reason has a refusal mapped to it")
	}
}

// TestConsumptionIsStampedFromThePriceBook asserts the recording path prices a
// turn from the price book in force — cache tokens at their own rates — and
// leaves it unpriced, never guessed, when the model is not in the table.
func TestConsumptionIsStampedFromThePriceBook(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	book, err := NewPriceBook(config.PricingConfig{
		Version: "v1",
		Models: []config.ModelPriceConfig{{
			Model: "m", Provider: "anthropic",
			InputPerMTok: 5, OutputPerMTok: 25, CacheReadPerMTok: 0.5, CacheWritePerMTok: 6.25,
		}},
	})
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	svc.SetPriceBook(book)

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadInputTokens: 2_000_000, CacheCreationInputTokens: 10_000})
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-2", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "not-in-table",
	}, &TokenUsage{InputTokens: 10, OutputTokens: 5})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	// 1M×$5 + 0.1M×$25 + 2M×$0.50 + 0.01M×$6.25 = $5 + $2.5 + $1 + $0.0625.
	if rollup.EquivalentMicros != 8_562_500 {
		t.Fatalf("EquivalentMicros = %d, want 8562500", rollup.EquivalentMicros)
	}
	if rollup.PricedEntries != 1 || rollup.UnpricedEntries != 1 {
		t.Fatalf("priced=%d unpriced=%d, want 1/1", rollup.PricedEntries, rollup.UnpricedEntries)
	}
	if rollup.CacheReadTokens != 2_000_000 || rollup.CacheWriteTokens != 10_000 {
		t.Fatalf("cache read=%d write=%d, want them kept apart from input",
			rollup.CacheReadTokens, rollup.CacheWriteTokens)
	}
}
