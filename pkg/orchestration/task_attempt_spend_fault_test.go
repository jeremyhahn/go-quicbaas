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
	stderrors "errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// A spend refusal is a fault the engine can name
// =============================================================================
//
// The gate's refusals reach a person through this table: the class decides
// whether the task waits or parks, the reason is what a forge comment quotes
// verbatim, and RetryNotBefore is how long the task waits before trying the
// same account again. All three are asserted, because getting the class right
// and the wait wrong retries into the same refusal every sweep.

func TestClassifyEnvironmentFaultNamesASpentQuota(t *testing.T) {
	retryAt := time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)
	fault, classified := classifyEnvironmentFault(&AccountQuotaExhaustedError{
		AccountID: "a1", Window: "five-hour", Axis: QuotaAxisTokens,
		Used: 1200, Limit: 1000, RetryAt: retryAt,
	})
	if !classified {
		t.Fatal("a spent quota was not classified; unclassified, it charges the attempt for a job that started no turn")
	}
	if fault.Class != FaultAccountQuotaExhausted {
		t.Errorf("Class = %q, want %q", fault.Class, FaultAccountQuotaExhausted)
	}
	if !fault.RetryNotBefore.Equal(retryAt) {
		t.Errorf("RetryNotBefore = %s, want the window's own %s", fault.RetryNotBefore, retryAt)
	}
	for _, want := range []string{"five-hour", "1200", "1000", "spend cap", "not charged"} {
		if !strings.Contains(fault.Reason, want) {
			t.Errorf("reason %q does not mention %q", fault.Reason, want)
		}
	}
	// The reason is read by a person who did not configure the cap, so it
	// has to say the system is behaving rather than broken.
	if strings.Contains(strings.ToLower(fault.Reason), "error") {
		t.Errorf("reason %q reads as a fault; a working cap is not one", fault.Reason)
	}
}

func TestClassifyEnvironmentFaultQuotaWithNoRetryTimeAsksForAction(t *testing.T) {
	fault, classified := classifyEnvironmentFault(&AccountQuotaExhaustedError{
		AccountID: "a1", Window: "weekly", Axis: QuotaAxisTokens, Used: 10, Limit: 5,
	})
	if !classified {
		t.Fatal("a spent quota with no retry time was not classified")
	}
	if !fault.RetryNotBefore.IsZero() {
		t.Errorf("RetryNotBefore = %s, want zero; nothing will age out so no moment can be named", fault.RetryNotBefore)
	}
	// With no moment to wait for, the reason must tell the reader what to do
	// instead of leaving them waiting for a window that never frees.
	if !strings.Contains(fault.Reason, "raise the account's allowance") {
		t.Errorf("reason %q does not say what to do when nothing will free up", fault.Reason)
	}
}

func TestClassifyEnvironmentFaultNamesATrippedBreaker(t *testing.T) {
	openUntil := time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)
	fault, classified := classifyEnvironmentFault(&AccountBreakerOpenError{
		AccountID: "a1", Failures: 4, OpenUntil: openUntil,
	})
	if !classified {
		t.Fatal("an open breaker was not classified")
	}
	if fault.Class != FaultAccountBreakerTripped {
		t.Errorf("Class = %q, want %q", fault.Class, FaultAccountBreakerTripped)
	}
	if !fault.RetryNotBefore.Equal(openUntil) {
		t.Errorf("RetryNotBefore = %s, want %s", fault.RetryNotBefore, openUntil)
	}
	if !strings.Contains(fault.Reason, "4") || !strings.Contains(fault.Reason, "circuit breaker") {
		t.Errorf("reason %q does not state the failure count and the mechanism", fault.Reason)
	}
}

func TestClassifyEnvironmentFaultNamesAHeldTrial(t *testing.T) {
	fault, classified := classifyEnvironmentFault(&AccountBreakerTrialHeldError{AccountID: "a1", Failures: 2})
	if !classified {
		t.Fatal("a held trial was not classified")
	}
	if fault.Class != FaultAccountBreakerTripped {
		t.Errorf("Class = %q, want %q", fault.Class, FaultAccountBreakerTripped)
	}
	// No moment: the trial ends when somebody else's turn reaches the
	// ledger, and inventing a wait would be inventing a fact.
	if !fault.RetryNotBefore.IsZero() {
		t.Errorf("RetryNotBefore = %s, want zero for a trial nothing can time", fault.RetryNotBefore)
	}
	if !strings.Contains(fault.Reason, "another caller") {
		t.Errorf("reason %q does not distinguish a held trial from an open breaker", fault.Reason)
	}
}

func TestSpendFaultReasonsDegradeWithoutTheirTypedError(t *testing.T) {
	// The sentinel can be reached through an error that carries none of the
	// detail — a wrapped or reconstructed one. The reason must still be a
	// whole sentence rather than a template with holes in it.
	quota := quotaExhaustedReason(ErrAccountQuotaExhausted)
	if !strings.Contains(quota, "allowance") || strings.Contains(quota, "  ") {
		t.Errorf("bare quota reason = %q, want a complete sentence", quota)
	}
	if at := quotaExhaustedRetryAt(ErrAccountQuotaExhausted); !at.IsZero() {
		t.Errorf("bare quota retry = %s, want zero", at)
	}
	breaker := breakerTrippedReason(ErrAccountBreakerOpen)
	if !strings.Contains(breaker, "out of service") {
		t.Errorf("bare breaker reason = %q, want a complete sentence", breaker)
	}
	if at := breakerTrippedRetryAt(ErrAccountBreakerTrialHeld); !at.IsZero() {
		t.Errorf("bare breaker retry = %s, want zero", at)
	}
}

func TestSpendFaultsRequeueRatherThanPark(t *testing.T) {
	// Neither is operator-actionable on sight: a window frees up and a
	// breaker retries. Parking on the first occurrence would put a human in
	// front of a condition that resolves itself.
	for _, class := range []string{FaultAccountQuotaExhausted, FaultAccountBreakerTripped} {
		if !faultRequeues(class) {
			t.Errorf("fault %q parks on sight; it resolves without anybody acting", class)
		}
		if !KnownFaultClass(class) {
			t.Errorf("fault %q is not a known class; a caller may refuse it", class)
		}
	}
}

func TestEveryResolverFaultClassHasAMeaning(t *testing.T) {
	// The forge comment renders the meaning beside the class. A class
	// without one shows a bare identifier to somebody waiting on an issue.
	for class := range resolverFaultClasses {
		meaning, known := FaultClassMeaning(class)
		if !known {
			t.Errorf("fault class %q has no meaning; it would render as a bare identifier on a forge issue", class)
			continue
		}
		if strings.TrimSpace(meaning) == "" {
			t.Errorf("fault class %q has an empty meaning", class)
		}
	}
}

func TestFaultClassMeaningRefusesToInventOne(t *testing.T) {
	if meaning, known := FaultClassMeaning("something_this_build_never_heard_of"); known {
		t.Errorf("FaultClassMeaning invented %q for an unknown class", meaning)
	}
	if _, known := FaultClassMeaning(""); known {
		t.Error("FaultClassMeaning claimed to explain the empty class")
	}
}

func TestDispatchReasonRendersAClassifiedRefusal(t *testing.T) {
	reason := dispatchReasonSpawnFailed(&AccountQuotaExhaustedError{
		AccountID: "a1", Window: "five-hour", Axis: QuotaAxisTokens, Used: 1200, Limit: 1000,
	})
	if !strings.Contains(reason, "five-hour") {
		t.Errorf("dispatch reason %q does not name the window that refused", reason)
	}
	if !strings.Contains(reason, "spend cap") {
		t.Errorf("dispatch reason %q shows the error's own sentence rather than the engine's explanation", reason)
	}
}

func TestDispatchReasonKeepsTheErrorItCannotClassify(t *testing.T) {
	// Where the engine has nothing better to say it must not invent a
	// category: hiding an unrecognized fault behind a soothing sentence is
	// how a real failure stops being investigated.
	reason := dispatchReasonSpawnFailed(stderrors.New("container runtime exploded"))
	if !strings.Contains(reason, "container runtime exploded") {
		t.Errorf("dispatch reason %q dropped the unclassified error's own text", reason)
	}
}
