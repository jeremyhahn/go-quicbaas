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
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Classification
// =============================================================================

// preFirstTurnError builds the error an agent produces when it exits on
// reason before its first model turn: evidence with the reason and a usage
// object that reports zero on every dimension.
func preFirstTurnError(reason string) error {
	return &StreamJSONError{
		Command:             "agent",
		TurnFailureEvidence: TurnFailureEvidence{TerminalReason: reason, Usage: &TokenUsage{}},
	}
}

// TestClassifyEnvironmentFault_RefundsEveryEnvironmentalTerminalReason drives
// the classifier over the reason set orchestration owns and proves each one
// is named, known, and refunded when concluded.
func TestClassifyEnvironmentFault_RefundsEveryEnvironmentalTerminalReason(t *testing.T) {
	reasons := EnvironmentalTerminalReasons()
	if len(reasons) == 0 {
		t.Fatal("the environmental reason set is empty")
	}
	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			fault, ok := classifyEnvironmentFault(preFirstTurnError(reason))
			if !ok {
				t.Fatalf("reason %q was not classified as environmental", reason)
			}
			if fault.Class != reason || !KnownFaultClass(fault.Class) {
				t.Fatalf("class %q does not match reason %q or is unknown", fault.Class, reason)
			}
			if !strings.Contains(fault.Reason, reason) || !fault.RetryNotBefore.IsZero() {
				t.Fatalf("an agent fault names its class and no retry time: %+v", fault)
			}
			if !faultRequeues(fault.Class) {
				t.Fatal("an agent fault requeues; only the resolver's park")
			}
			outcome := faultOutcomeFor(&Task{}, fault)
			if !outcome.refund || !outcome.requeue || !outcome.counts || outcome.bounded {
				t.Fatalf("expected refund+requeue+counts, got %+v", outcome)
			}
		})
	}
}

// TestClassifyEnvironmentFault_ResolverClasses covers every class the package
// itself produces: its name, whether it parks, and the moment it names.
func TestClassifyEnvironmentFault_ResolverClasses(t *testing.T) {
	availableAt := time.Now().Add(10 * time.Minute).UTC()
	retryAt := time.Now().Add(90 * time.Second).UTC()
	for name, tc := range map[string]struct {
		err      error
		class    string
		parks    bool
		notAfter time.Time
		mentions string
	}{
		"cooling down": {
			err:      &AccountsCoolingDownError{TenantID: "acme", Provider: "anthropic", Candidates: 3, AvailableAt: availableAt},
			class:    FaultAccountsCoolingDown,
			notAfter: availableAt,
			mentions: "3 of them",
		},
		"cooling down through the sentinel only": {
			err:      ErrAccountsCoolingDown,
			class:    FaultAccountsCoolingDown,
			mentions: "not charged",
		},
		"no account": {
			err:      &NoAccountAvailableError{TenantID: "acme", ProjectID: "p", Provider: "anthropic"},
			class:    FaultNoProviderAccount,
			parks:    true,
			mentions: "project p",
		},
		"no account through the sentinel only": {
			err:      ErrNoAccountAvailable,
			class:    FaultNoProviderAccount,
			parks:    true,
			mentions: "Register a pooled account",
		},
		"no account in the system scope": {
			err:      &NoAccountAvailableError{Provider: "anthropic"},
			class:    FaultNoProviderAccount,
			parks:    true,
			mentions: SystemTenantID,
		},
		"no resolver": {
			err:      ErrAccountResolverNotConfigured,
			class:    FaultNoAccountResolver,
			parks:    true,
			mentions: "secrets manager",
		},
		"credential missing": {
			err:      ErrAccountCredentialNotFound,
			class:    FaultAccountCredentialMissing,
			parks:    true,
			mentions: "readmit the task",
		},
		"refresher missing": {
			err:      ErrAccountRefresherNotConfigured,
			class:    FaultAccountRefresherMissing,
			parks:    true,
			mentions: "refresher",
		},
		"capacity": {
			err:      &ProjectCapacityExceededError{TenantID: "acme", ProjectID: "p", Live: 2, Limit: 2, RetryAt: retryAt},
			class:    FaultProjectCapacityExceeded,
			notAfter: retryAt,
			mentions: "2 of its 2",
		},
		"capacity through the sentinel only": {
			err:      ErrProjectCapacityExceeded,
			class:    FaultProjectCapacityExceeded,
			mentions: "concurrency budget",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fault, ok := classifyEnvironmentFault(tc.err)
			if !ok {
				t.Fatalf("not classified: %v", tc.err)
			}
			if fault.Class != tc.class {
				t.Fatalf("class = %q, want %q", fault.Class, tc.class)
			}
			if !KnownFaultClass(fault.Class) {
				t.Fatalf("class %q is not known", fault.Class)
			}
			if faultRequeues(fault.Class) == tc.parks {
				t.Fatalf("parks = %v, want %v", !faultRequeues(fault.Class), tc.parks)
			}
			if !fault.RetryNotBefore.Equal(tc.notAfter) {
				t.Fatalf("RetryNotBefore = %v, want %v", fault.RetryNotBefore, tc.notAfter)
			}
			if !strings.Contains(fault.Reason, tc.mentions) {
				t.Fatalf("reason %q does not mention %q", fault.Reason, tc.mentions)
			}
		})
	}
}

// TestClassifyEnvironmentFault_NotEnvironmental proves the charged answers:
// no error, an ordinary error, an agent that reported usage, an agent that
// reported no usage at all, and a non-environmental reason with zero usage.
func TestClassifyEnvironmentFault_NotEnvironmental(t *testing.T) {
	for name, err := range map[string]error{
		"nil":               nil,
		"plain":             errors.New("the work failed"),
		"usage reported":    &StreamJSONError{TurnFailureEvidence: TurnFailureEvidence{TerminalReason: TerminalReasonAPIError, Usage: &TokenUsage{InputTokens: 5}}},
		"no usage at all":   &StreamJSONError{TurnFailureEvidence: TurnFailureEvidence{TerminalReason: TerminalReasonAPIError}},
		"deterministic":     preFirstTurnError(TerminalReasonMaxTurns),
		"unknown reason":    preFirstTurnError("something_new"),
		"lease taken":       &JobLeaseTakenError{JobID: "j"},
		"stalled":           &JobStalledError{JobID: "j"},
		"cancelled context": context.Canceled,
		"refresh failed":    ErrAccountRefreshFailed,
	} {
		t.Run(name, func(t *testing.T) {
			if fault, ok := classifyEnvironmentFault(err); ok {
				t.Fatalf("classified as environmental: %+v", fault)
			}
		})
	}
	if KnownFaultClass("typo") {
		t.Fatal("an unknown class must not be known")
	}
}

// TestClassifyJobFailure names the class of every terminal error kind.
func TestClassifyJobFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		err   error
		class string
		fault bool
	}{
		"nil":        {nil, "", false},
		"plain":      {errors.New("x"), "", false},
		"superseded": {&JobSupersededError{JobID: "j", By: 2}, JobFailureClassSuperseded, false},
		"taken":      {&JobLeaseTakenError{JobID: "j"}, JobFailureClassSuperseded, false},
		"stalled":    {&JobStalledError{JobID: "j"}, JobFailureClassStalled, false},
		"cooldown":   {&AccountsCoolingDownError{AvailableAt: time.Now().Add(time.Minute)}, FaultAccountsCoolingDown, true},
		"agent":      {preFirstTurnError(TerminalReasonAPIError), TerminalReasonAPIError, true},
	} {
		t.Run(name, func(t *testing.T) {
			class, fault := classifyJobFailure(tc.err)
			if class != tc.class {
				t.Fatalf("class = %q, want %q", class, tc.class)
			}
			if (fault.Class != "") != tc.fault {
				t.Fatalf("fault carried = %v, want %v", fault.Class != "", tc.fault)
			}
		})
	}
}

// =============================================================================
// Outcomes
// =============================================================================

// TestFaultOutcomeFor pins the four shapes an outcome takes.
func TestFaultOutcomeFor(t *testing.T) {
	at := time.Now().Add(time.Minute)
	if got := faultOutcomeFor(&Task{}, EnvironmentFault{}); got != (faultOutcome{}) {
		t.Fatalf("no class is the zero outcome, got %+v", got)
	}
	if got := faultOutcomeFor(&Task{}, EnvironmentFault{Class: FaultNoProviderAccount}); got != (faultOutcome{refund: true}) {
		t.Fatalf("a parking class refunds and parks, got %+v", got)
	}
	got := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: 1}, EnvironmentFault{Class: FaultAccountsCoolingDown, RetryNotBefore: at})
	if !got.refund || !got.requeue || !got.counts || got.bounded || !got.notBefore.Equal(at) {
		t.Fatalf("a requeueing class under the bound refunds, requeues, counts and waits, got %+v", got)
	}
	if got := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1}, EnvironmentFault{Class: FaultAccountsCoolingDown}); got != (faultOutcome{bounded: true}) {
		t.Fatalf("the third consecutive fault is bounded, got %+v", got)
	}
	if got := faultOutcomeFor(nil, EnvironmentFault{Class: TerminalReasonAPIError}); !got.requeue {
		t.Fatalf("a nil task is not bounded, got %+v", got)
	}
	want := faultOutcome{refund: true, requeue: true, keepsCount: true}
	if got := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1}, EnvironmentFault{Class: FaultAccountsSaturated}); got != want {
		t.Fatalf("saturation at the bound still requeues uncounted, got %+v", got)
	}
}

// TestApplyConclusion_RepeatedSaturationIsNeverChargedOrParked is the
// regression for a busy pool: contention past the three-strike bound must
// keep requeueing uncharged, and must neither advance nor clear the count
// another fault class left behind.
func TestApplyConclusion_RepeatedSaturationIsNeverChargedOrParked(t *testing.T) {
	task := &Task{ID: "t", TenantID: "acme", ConsecutiveEnvironmentFaults: 1}
	fault := EnvironmentFault{Class: FaultAccountsSaturated, Reason: "saturated"}

	for round := 1; round <= 3*MaxConsecutiveEnvironmentFaults; round++ {
		task.Status = TaskStatusClaimed
		task.Attempts++
		reason := applyConclusion(task, attemptConclusion{
			status: failedStatus("job", FaultAccountsSaturated, "saturated"),
			fault:  fault,
		}, time.Now().UTC())

		if reason != "" || task.Status != TaskStatusPending {
			t.Fatalf("round %d: conclusion %q status %s, want a plain requeue", round, reason, task.Status)
		}
		if charged := task.ChargedAttempts(); charged != 0 {
			t.Fatalf("round %d: charged attempts = %d, want 0", round, charged)
		}
		if task.ConsecutiveEnvironmentFaults != 1 {
			t.Fatalf("round %d: consecutive = %d, want the prior 1 untouched", round, task.ConsecutiveEnvironmentFaults)
		}
	}
}

// TestClampRetryDeferral pins both directions of the cap.
func TestClampRetryDeferral(t *testing.T) {
	now := time.Now().UTC()
	if got := clampRetryDeferral(time.Time{}, now); !got.IsZero() {
		t.Fatal("zero stays zero")
	}
	if got := clampRetryDeferral(now.Add(-time.Second), now); !got.IsZero() {
		t.Fatal("a past moment is no wait")
	}
	if got := clampRetryDeferral(now.Add(time.Hour), now); !got.Equal(now.Add(time.Hour)) {
		t.Fatal("a moment inside the cap is kept")
	}
	if got := clampRetryDeferral(now.Add(48*time.Hour), now); !got.Equal(now.Add(MaxRetryDeferral)) {
		t.Fatalf("a moment past the cap is clamped to it, got %v", got)
	}
}

// TestBoundedFaultReason pins the reason names the count and the last detail.
func TestBoundedFaultReason(t *testing.T) {
	reason := boundedFaultReason(&Task{ConsecutiveEnvironmentFaults: 2}, EnvironmentFault{Class: TerminalReasonAPIError, Reason: "the agent exited"})
	if !strings.HasPrefix(reason, "3 consecutive environmental failures — api_error") || !strings.Contains(reason, "The last one: the agent exited") {
		t.Fatalf("unexpected reason: %s", reason)
	}
	short := boundedFaultReason(&Task{}, EnvironmentFault{Class: TerminalReasonAPIError})
	if !strings.HasPrefix(short, "1 consecutive") || strings.Contains(short, "The last one") {
		t.Fatalf("unexpected reason: %s", short)
	}
}

// TestApplyConclusion_PersistsTheReasonADeathIsToldBy pins the contract every
// later reader depends on, pkg/sdlc's reconciliation sweep included.
//
// applyDeath rests a reaped job whose budget is spent in the SAME shape an
// explicit engine escalation leaves -- blocked/escalated, no fault class -- so
// the resting record cannot distinguish them. The conclusion reason is what
// does, and it is only usable by a later reader because it is PERSISTED. A
// reader re-deriving it from the status would announce the escalation's
// diagnosis over a death nobody diagnosed.
func TestApplyConclusion_PersistsTheReasonADeathIsToldBy(t *testing.T) {
	task := &Task{ID: "task-1", Status: TaskStatusInProgress, Attempts: 3, AttemptBudget: 3}
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{
			JobID: "job-1", State: JobStateFailed,
			FailureClass: JobFailureClassAbandoned, Error: "the node stopped heartbeating",
		},
	}, time.Now())

	if reason != TaskConcludedAbandoned {
		t.Fatalf("reason = %q, want %q", reason, TaskConcludedAbandoned)
	}
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindEscalated {
		t.Fatalf("a death rests as %s/%s, want blocked/escalated -- the escalation's shape", task.Status, task.BlockKind)
	}

	// THE SHAPE IS AMBIGUOUS AND THE REASON IS NOT. An explicit escalation
	// reaches the same status and kind, so only the reason tells them apart.
	if got, _ := ConclusionOfBlockKind(task.BlockKind); got == reason {
		t.Fatalf("the block kind maps to %q, the same as the death's reason; this test proves nothing", got)
	}
}

// TestTask_Budget pins the task's own arithmetic.
func TestTask_Budget(t *testing.T) {
	task := &Task{Attempts: 4, EnvironmentFaults: 2}
	if task.ChargedAttempts() != 2 || task.EffectiveAttemptBudget() != DefaultAttemptBudget || task.BudgetExhausted() {
		t.Fatalf("2 charged of the default 3 is not exhausted: %+v", task)
	}
	task.Attempts = 5
	if !task.BudgetExhausted() {
		t.Fatal("3 charged of 3 is exhausted")
	}
	task.AttemptBudget = 10
	if task.BudgetExhausted() || task.EffectiveAttemptBudget() != 10 {
		t.Fatal("a declared budget replaces the default")
	}
	if (&Task{EnvironmentFaults: 3}).ChargedAttempts() != 0 {
		t.Fatal("charged attempts never go negative")
	}
	now := time.Now().UnixNano()
	if (&Task{RetryNotBefore: now + 1}).Deferred(now) != true || (&Task{RetryNotBefore: now}).Deferred(now) {
		t.Fatal("deferred is strictly after now")
	}
	if (&Task{}).AttemptHistory() != nil {
		t.Fatal("no history is nil")
	}
	withHistory := &Task{Metadata: map[string]string{attemptHistoryKey: "a,b"}}
	if got := withHistory.AttemptHistory(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("history = %v", got)
	}
}

// =============================================================================
// concludeAttempt
// =============================================================================

// newConcludingService is a service over an in-memory task store and a
// process lease store, ready to conclude attempts.
func newConcludingService(t *testing.T) (*agentServiceImpl, TaskStore) {
	t.Helper()
	svc := newTestService(t)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	return svc, store
}

// seedSpawnedTask creates a task that has a job out for it.
func seedSpawnedTask(t *testing.T, store TaskStore, task *Task) *Task {
	t.Helper()
	if task.ID == "" {
		task.ID = uniqueTaskID("attempt")
	}
	task.TenantID = "acme"
	task.Subject = "work"
	if task.Status == "" {
		task.Status = TaskStatusClaimed
	}
	if task.SpawnedJobID == "" {
		task.SpawnedJobID = "job-1"
	}
	if task.Attempts == 0 {
		task.Attempts = 1
	}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func failedStatus(jobID, class, detail string) *JobStatus {
	return &JobStatus{JobID: jobID, TenantID: "acme", State: JobStateFailed, FailureClass: class, Error: detail}
}

func readTask(t *testing.T, store TaskStore, id string) *Task {
	t.Helper()
	task, err := store.GetTask(context.Background(), "acme", id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return task
}

// TestConcludeAttempt_Completed proves a completed job completes the task and
// records the history.
func TestConcludeAttempt_Completed(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{ConsecutiveEnvironmentFaults: 2, RetryNotBefore: 5, BlockedReason: "old"})
	status := &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateCompleted}

	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{status: status}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusCompleted || task.CompletedAt == 0 {
		t.Fatalf("expected completed, got %+v", task)
	}
	if task.SpawnedJobID != "" || task.AttemptHistory()[0] != "job-1" {
		t.Fatalf("history not recorded: spawned=%q history=%v", task.SpawnedJobID, task.AttemptHistory())
	}
	if task.ConsecutiveEnvironmentFaults != 0 || task.RetryNotBefore != 0 || task.BlockedReason != "" {
		t.Fatalf("completion clears the retry state: %+v", task)
	}
	if task.Result == nil || task.Result.Metadata["job_id"] != "job-1" || task.Result.Metadata["job_status"] != "completed" {
		t.Fatalf("result must name the job: %+v", task.Result)
	}
	if task.Attempts != 1 || task.EnvironmentFaults != 0 {
		t.Fatal("completion charges the attempt it was")
	}

	// A second conclusion about the same job is refused, not applied twice.
	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{status: status})
	var concluded *AttemptConcludedError
	if !errors.As(err, &concluded) || !errors.Is(err, ErrAttemptConcluded) {
		t.Fatalf("expected AttemptConcludedError, got %v", err)
	}
	if concluded.Status != TaskStatusCompleted || concluded.JobID != "job-1" {
		t.Fatalf("refusal names the wrong state: %+v", concluded)
	}
}

// TestConcludeAttempt_EnvironmentFaultRefundsAndRequeues proves the refund:
// the attempt is not charged, the task waits until the cooldown lifts, and
// the consecutive count advances.
func TestConcludeAttempt_EnvironmentFaultRefundsAndRequeues(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{AssignedTo: "agent-a", Attempts: 2})
	availableAt := time.Now().Add(10 * time.Minute).UTC()
	fault := EnvironmentFault{Class: FaultAccountsCoolingDown, Reason: "cooling", RetryNotBefore: availableAt}

	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", FaultAccountsCoolingDown, "no account free"),
		fault:  fault,
	})
	if err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusPending || task.AssignedTo != "" {
		t.Fatalf("expected a requeue, got %+v", task)
	}
	if task.Attempts != 2 || task.EnvironmentFaults != 1 || task.ChargedAttempts() != 1 {
		t.Fatalf("the attempt must be refunded: attempts=%d faults=%d", task.Attempts, task.EnvironmentFaults)
	}
	if task.ConsecutiveEnvironmentFaults != 1 {
		t.Fatalf("consecutive = %d, want 1", task.ConsecutiveEnvironmentFaults)
	}
	if task.RetryNotBefore != availableAt.UnixNano() {
		t.Fatalf("RetryNotBefore = %d, want %d", task.RetryNotBefore, availableAt.UnixNano())
	}
	if task.Result.Metadata["failure_class"] != FaultAccountsCoolingDown || task.Result.Output != "no account free" {
		t.Fatalf("result must carry the class and detail: %+v", task.Result)
	}
}

// TestConcludeAttempt_CooldownIsClampedToADay proves a hold beyond the cap is
// not honoured past it.
func TestConcludeAttempt_CooldownIsClampedToADay(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{})
	before := time.Now().UTC()
	fault := EnvironmentFault{Class: FaultAccountsCoolingDown, RetryNotBefore: before.Add(72 * time.Hour)}
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", FaultAccountsCoolingDown, ""), fault: fault,
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	got := time.Unix(0, task.RetryNotBefore)
	if got.Before(before.Add(MaxRetryDeferral)) || got.After(time.Now().Add(MaxRetryDeferral)) {
		t.Fatalf("RetryNotBefore %v is not clamped to now+%s", got, MaxRetryDeferral)
	}
}

// TestConcludeAttempt_ThreeConsecutiveFaultsBlockAndCharge proves the bound:
// the third consecutive fault parks the task and is charged.
func TestConcludeAttempt_ThreeConsecutiveFaultsBlockAndCharge(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 3, EnvironmentFaults: 2, ConsecutiveEnvironmentFaults: 2})
	fault := EnvironmentFault{Class: TerminalReasonAPIError, Reason: "the agent exited on api_error"}
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", TerminalReasonAPIError, "exited"), fault: fault,
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindBlocked {
		t.Fatalf("expected blocked, got %+v", task)
	}
	if task.EnvironmentFaults != 2 || task.ChargedAttempts() != 1 {
		t.Fatalf("the bounded fault is charged: faults=%d charged=%d", task.EnvironmentFaults, task.ChargedAttempts())
	}
	if !strings.HasPrefix(task.BlockedReason, "3 consecutive environmental failures") {
		t.Fatalf("reason: %s", task.BlockedReason)
	}
	if task.ConsecutiveEnvironmentFaults != 0 {
		t.Fatal("the run length resets once the bound is hit")
	}
}

// TestConcludeAttempt_ParkingFaultBlocksAndRefunds proves a resolver fault
// parks on sight, with the attempt given back.
func TestConcludeAttempt_ParkingFaultBlocksAndRefunds(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{ConsecutiveEnvironmentFaults: 1})
	fault := EnvironmentFault{Class: FaultNoProviderAccount, Reason: "no account for anthropic"}
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", FaultNoProviderAccount, ""), fault: fault,
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked || task.BlockedReason != "no account for anthropic" || task.BlockKind != TaskBlockKindBlocked {
		t.Fatalf("expected a block with the fault's reason, got %+v", task)
	}
	if task.EnvironmentFaults != 1 || task.ConsecutiveEnvironmentFaults != 0 {
		t.Fatalf("a park refunds and does not count toward the bound: %+v", task)
	}
}

// TestConcludeAttempt_GenuineFailureChargesAndRequeues proves a failure of
// the work spends an attempt and goes back to the queue while the budget
// lasts.
func TestConcludeAttempt_GenuineFailureChargesAndRequeues(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{AssignedTo: "agent-a", ConsecutiveEnvironmentFaults: 2, RetryNotBefore: time.Now().Add(time.Hour).UnixNano()})
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", "", "review rejected"),
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusPending || task.AssignedTo != "" {
		t.Fatalf("expected a requeue, got %+v", task)
	}
	if task.EnvironmentFaults != 0 || task.ChargedAttempts() != 1 {
		t.Fatal("a genuine failure is charged")
	}
	if task.ConsecutiveEnvironmentFaults != 0 || task.RetryNotBefore != 0 {
		t.Fatal("a charged attempt resets the fault run and the wait")
	}
	if task.Result.Output != "review rejected" || task.Result.Metadata["failure_class"] != "" {
		t.Fatalf("result: %+v", task.Result)
	}
}

// TestConcludeAttempt_BudgetExhaustedFailsWithReason proves the last charged
// attempt retires the task.
func TestConcludeAttempt_BudgetExhaustedFailsWithReason(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 4, EnvironmentFaults: 1, AttemptBudget: 3, SpawnedJobID: "job-4"})
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-4", JobFailureClassStalled, "stalled"),
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusFailed || task.CompletedAt == 0 {
		t.Fatalf("expected failed, got %+v", task)
	}
	if task.Result.Metadata["reason"] != budgetExhaustedReason || task.Result.Metadata["failure_class"] != JobFailureClassStalled {
		t.Fatalf("result must say why: %+v", task.Result)
	}
}

// TestConcludeAttempt_DeathRequeuesThenBlocks proves a reaped job is charged,
// requeued while the budget lasts, and blocked for an operator when it does
// not.
func TestConcludeAttempt_DeathRequeuesThenBlocks(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1, ConsecutiveEnvironmentFaults: 1})
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", JobFailureClassAbandoned, "node gone"),
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusPending || task.ConsecutiveEnvironmentFaults != 0 || task.EnvironmentFaults != 0 {
		t.Fatalf("a death with budget left requeues, charged: %+v", task)
	}

	spent := seedSpawnedTask(t, store, &Task{Attempts: 3, SpawnedJobID: "job-3"})
	if err := svc.concludeAttempt(context.Background(), "acme", spent.ID, attemptConclusion{
		status: failedStatus("job-3", JobFailureClassAbandoned, "node gone"),
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task = readTask(t, store, spent.ID)
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindEscalated {
		t.Fatalf("a death past the budget blocks for an operator, got %+v", task)
	}
	if !strings.Contains(task.BlockedReason, "node gone") || !strings.Contains(task.BlockedReason, "budget is spent") {
		t.Fatalf("reason: %s", task.BlockedReason)
	}
}

// TestConcludeAttempt_SupersededWritesNothing proves a job that lost its
// lease leaves the task to whoever holds it.
func TestConcludeAttempt_SupersededWritesNothing(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{})
	before := readTask(t, store, seeded.ID)
	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", JobFailureClassSuperseded, "superseded"),
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	after := readTask(t, store, seeded.ID)
	if after.Version != before.Version || after.Status != before.Status || after.SpawnedJobID != "job-1" {
		t.Fatalf("a superseded job must not touch the task: before=%+v after=%+v", before, after)
	}
}

// TestConcludeAttempt_RefusesAJobTheTaskDoesNotName proves the guard: a
// report from a job other than the open one, or about a task whose attempt
// is not open, is refused and the record left alone.
func TestConcludeAttempt_RefusesAJobTheTaskDoesNotName(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{})
	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-other", "", "late"),
	})
	var concluded *AttemptConcludedError
	if !errors.As(err, &concluded) || concluded.SpawnedJobID != "job-1" {
		t.Fatalf("expected AttemptConcludedError naming job-1, got %v", err)
	}
	blocked := seedSpawnedTask(t, store, &Task{Status: TaskStatusBlocked})
	if err := svc.concludeAttempt(context.Background(), "acme", blocked.ID, attemptConclusion{
		status: failedStatus("job-1", "", "late"),
	}); !errors.Is(err, ErrAttemptConcluded) {
		t.Fatalf("a blocked task's attempt is not open, got %v", err)
	}
	if !taskAttemptOpen(TaskStatusPending) || !taskAttemptOpen(TaskStatusInProgress) || taskAttemptOpen(TaskStatusCancelled) {
		t.Fatal("open statuses are pending, claimed and in_progress")
	}
}

// TestConcludeAttempt_RefusesInvalidInput proves the typed refusals.
func TestConcludeAttempt_RefusesInvalidInput(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{})
	ctx := context.Background()
	if err := svc.concludeAttempt(ctx, "acme", seeded.ID, attemptConclusion{}); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("no status: %v", err)
	}
	if err := svc.concludeAttempt(ctx, "acme", "", attemptConclusion{status: failedStatus("j", "", "")}); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("no task: %v", err)
	}
	if err := svc.concludeAttempt(ctx, "acme", "missing", attemptConclusion{status: failedStatus("j", "", "")}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	bare := newTestService(t)
	if err := bare.concludeAttempt(ctx, "acme", seeded.ID, attemptConclusion{status: failedStatus("j", "", "")}); !errors.Is(err, ErrTaskStoreRequired) {
		t.Fatalf("no store: %v", err)
	}
}

// conflictingTaskStore makes the first UpdateTask lose: it writes a
// concurrent change to the record first, so the caller's version is stale.
type conflictingTaskStore struct {
	TaskStore
	updates atomic.Int64
}

func (s *conflictingTaskStore) UpdateTask(ctx context.Context, task *Task) error {
	if s.updates.Add(1) == 1 {
		fresh, err := s.TaskStore.GetTask(ctx, task.TenantID, task.ID)
		if err != nil {
			return err
		}
		if fresh.Metadata == nil {
			fresh.Metadata = map[string]string{}
		}
		fresh.Metadata["concurrent"] = "yes"
		fresh.Priority = 7
		if err := s.TaskStore.UpdateTask(ctx, fresh); err != nil {
			return err
		}
	}
	return s.TaskStore.UpdateTask(ctx, task)
}

// TestConcludeAttempt_WritesOnceAndRetriesAConflict proves a lost CAS is
// re-derived from the fresh record — the concurrent change survives and the
// conclusion still lands exactly once.
func TestConcludeAttempt_WritesOnceAndRetriesAConflict(t *testing.T) {
	svc := newTestService(t)
	inner := NewInMemoryTaskStore()
	store := &conflictingTaskStore{TaskStore: inner}
	svc.SetTaskStore(store)
	seeded := seedSpawnedTask(t, inner, &Task{})

	if err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateCompleted},
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	if store.updates.Load() != 2 {
		t.Fatalf("expected the conflict and one retry, got %d updates", store.updates.Load())
	}
	task := readTask(t, inner, seeded.ID)
	if task.Metadata["concurrent"] != "yes" || task.Priority != 7 {
		t.Fatalf("the concurrent change was overwritten: %+v", task)
	}
	if task.Status != TaskStatusCompleted || len(task.AttemptHistory()) != 1 {
		t.Fatalf("the conclusion did not land once: %+v", task)
	}
}

// alwaysConflictingTaskStore never lets an update through.
type alwaysConflictingTaskStore struct {
	TaskStore
}

func (s *alwaysConflictingTaskStore) UpdateTask(_ context.Context, task *Task) error {
	return &TaskVersionConflictError{TaskID: task.ID, ExpectedVersion: task.Version, ActualVersion: task.Version + 1}
}

// TestConcludeAttempt_GivesUpAfterBoundedConflicts proves the retry is
// bounded and the exhaustion is typed.
func TestConcludeAttempt_GivesUpAfterBoundedConflicts(t *testing.T) {
	svc := newTestService(t)
	inner := NewInMemoryTaskStore()
	svc.SetTaskStore(&alwaysConflictingTaskStore{TaskStore: inner})
	seeded := seedSpawnedTask(t, inner, &Task{})
	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateCompleted},
	})
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("expected ErrTaskVersionConflict after the bound, got %v", err)
	}
}

// TestConcludeAttempt_ReleasesTheLeaseAfterTheWrite proves the order: the
// lease is released once the task is written, and released even when the
// task refused the conclusion.
func TestConcludeAttempt_ReleasesTheLeaseAfterTheWrite(t *testing.T) {
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{})
	ctx := context.Background()
	lease := &JobLease{TenantID: "acme", Ref: seeded.ID, TaskID: seeded.ID, JobID: "job-1", Generation: 1, Node: "node-a"}
	if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := svc.concludeAttempt(ctx, "acme", seeded.ID, attemptConclusion{
		status: &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateCompleted}, lease: lease,
	}); err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	got, err := svc.jobLeases.Get(ctx, "acme", "", "", seeded.ID)
	if err != nil {
		t.Fatalf("get lease: %v", err)
	}
	if !got.Released {
		t.Fatal("the lease is released after the task is written")
	}

	// A refused conclusion still releases: the holder is finished either way.
	stale := &JobLease{TenantID: "acme", Ref: "stale-ref", TaskID: seeded.ID, JobID: "job-stale", Generation: 1}
	if err := svc.jobLeases.Acquire(ctx, stale); err != nil {
		t.Fatalf("acquire stale: %v", err)
	}
	err = svc.concludeAttempt(ctx, "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-stale", "", ""), lease: stale,
	})
	if !errors.Is(err, ErrAttemptConcluded) {
		t.Fatalf("expected the refusal, got %v", err)
	}
	got, err = svc.jobLeases.Get(ctx, "acme", "", "", "stale-ref")
	if err != nil || !got.Released {
		t.Fatalf("a refused conclusion still releases its lease: %+v %v", got, err)
	}
}

// TestParkAwaitsAnAnswer_OnlyNeedsInfo pins the predicate that decides which
// parks are refunded for waiting on a person.
//
// THE OTHER TWO KINDS ARE EXCLUDED ON PURPOSE. TaskBlockKindBlocked waits on an
// external CONDITION that can clear by itself, and carries a fault class when
// the factory caused it; TaskBlockKindEscalated is reached through paths where
// the work itself did fail. Widening this would quietly start refunding those.
func TestParkAwaitsAnAnswer_OnlyNeedsInfo(t *testing.T) {
	if !ParkAwaitsAnAnswer(TaskBlockKindNeedsInfo) {
		t.Fatal("needs_info is the kind whose whole meaning is that a person was asked")
	}
	for _, kind := range []TaskBlockKind{TaskBlockKindBlocked, TaskBlockKindEscalated, TaskBlockKind(""), TaskBlockKind("mystery")} {
		if ParkAwaitsAnAnswer(kind) {
			t.Fatalf("%q must not be refunded as a question", kind)
		}
	}
}

// TestChargedAttempts_RefundsQuestionsSeparately is the accounting property the
// whole change exists for.
//
// Measured on the burn-in fleet (2026-09-14): three go-luks2 tasks sat at SEVEN
// charged attempts against a default budget of three, having spent them asking
// the owner rulings nobody had answered yet. A needs_info park never retries on
// its own, so the budget bounds nothing there — all charging it achieved was
// that answering the question would meet a task with no allowance left.
func TestChargedAttempts_RefundsQuestionsSeparately(t *testing.T) {
	task := &Task{Attempts: 9, EnvironmentFaults: 2, QuestionAttempts: 5}
	if got := task.ChargedAttempts(); got != 2 {
		t.Fatalf("ChargedAttempts = %d, want 2 (9 dispatches − 2 environment − 5 questions)", got)
	}

	// The two refunds stay distinguishable: one says the factory broke, the
	// other says the worker did the right thing and is waiting.
	if task.EnvironmentFaults == 0 || task.QuestionAttempts == 0 {
		t.Fatal("the counters must remain separate, not folded together")
	}

	// A task that only ever asked questions has spent none of its budget.
	asking := &Task{Attempts: 4, QuestionAttempts: 4}
	if got := asking.ChargedAttempts(); got != 0 {
		t.Fatalf("a task that only asked questions charged %d, want 0", got)
	}
	if asking.BudgetExhausted() {
		t.Fatal("asking the owner questions must never exhaust the attempt budget")
	}
}

// TestChargedAttempts_NeverNegative guards the clamp against a record written
// by a future binary, or counters that disagree after a partial write.
func TestChargedAttempts_NeverNegative(t *testing.T) {
	task := &Task{Attempts: 1, EnvironmentFaults: 3, QuestionAttempts: 4}
	if got := task.ChargedAttempts(); got != 0 {
		t.Fatalf("ChargedAttempts = %d, want 0 rather than a negative budget", got)
	}
}
