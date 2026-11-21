// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestSurfacePolicy_ResolvedAndValidate(t *testing.T) {
	if got := (SurfacePolicy{}).Resolved(); got != DefaultSurfacePolicy {
		t.Fatalf("zero resolved = %+v", got)
	}
	explicit := SurfacePolicy{RetryAttempts: 1, RetryInterval: time.Second, PostTimeout: time.Second}
	if got := explicit.Resolved(); got != explicit {
		t.Fatalf("explicit resolved = %+v", got)
	}
	for name, p := range map[string]SurfacePolicy{
		"attempts": {RetryAttempts: -1},
		"interval": {RetryInterval: -1},
		"post":     {PostTimeout: -1},
		"short":    {PostTimeout: MinPostTimeout / 2},
		"long":     {PostTimeout: MaxPostTimeout + 1},
	} {
		if err := p.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if err := explicit.Validate(); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
}

func TestRenderBlocked_EveryKindReadsRight(t *testing.T) {
	binding := testBinding()
	task := testTaskRecord(25)
	task.Attempts = 2
	task.EnvironmentFaults = 1
	delivery := &Delivery{PullRequest: DeliveryPullRequest{Number: 7, URL: "http://forge.test/pr/7"}}
	for kind, headline := range map[orchestration.TaskBlockKind]string{
		orchestration.TaskBlockKindNeedsInfo: "The agent has a question",
		orchestration.TaskBlockKindEscalated: "This task needs the owner",
		orchestration.TaskBlockKindBlocked:   "This task is blocked",
	} {
		body := renderBlocked(sdlcPrompts, binding, task, 2, orchestration.BlockReason{Kind: kind, Message: "line one\nline two"}, delivery)
		for _, want := range []string{"### " + headline, "> line one\n> line two", "**Attempt:** 1 of 3 (2 dispatches, 1 environment fault, not charged)", "http://forge.test/pr/7", blockedMarker(task.ID, 2, blockedOutcome(kind))} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s lacks %q:\n%s", kind, want, body)
			}
		}
	}
	empty := renderBlocked(sdlcPrompts, nil, nil, 0, orchestration.BlockReason{}, nil)
	if !strings.Contains(empty, "recorded no reason") || strings.Contains(empty, "**Labels:**") {
		t.Fatalf("empty render:\n%s", empty)
	}
	spent := testTaskRecord(1)
	spent.Attempts = 3
	spent.AttemptBudget = 3
	if !strings.Contains(renderBlocked(sdlcPrompts, binding, spent, 3, orchestration.BlockReason{}, nil), "cannot be resumed as it stands") {
		t.Fatal("a spent budget must say so")
	}
}

// TestRenderBlocked_DerivesTheProseFromTheTypedCondition is the surface half
// of the typed condition: the comment a person reads says what would clear
// the block and who clears it, and the machine marker never reaches them.
func TestRenderBlocked_DerivesTheProseFromTheTypedCondition(t *testing.T) {
	binding := testBinding()
	task := testTaskRecord(25)
	condition := UnblockCondition{Kind: UnblockOwnerRuling}
	reason, err := ComposeBlockedReason(condition, "changes requested after 3 review round(s)")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	body := renderBlocked(sdlcPrompts, binding, task, 1, orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindEscalated, Message: reason,
	}, nil)
	for _, want := range []string{
		"**Unblocks when:** " + condition.Describe(),
		"**Cleared by:** the owner",
		"> changes requested after 3 review round(s)",
		"readmit the task",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the comment lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, unblockMarkerOpen) {
		t.Fatalf("the machine marker reached a reader:\n%s", body)
	}
	if strings.Count(body, condition.Describe()) != 1 {
		t.Fatalf("the condition is stated twice:\n%s", body)
	}
}

// TestRenderBlocked_AMachineCheckedConditionAsksForNoReadmission covers the
// remedy that DIFFERS: a condition the sweep decides clears itself, so
// telling a person to readmit the task would send them after a step that is
// already automated. A spent budget overrides it, because then the sweep
// will not readmit either.
func TestRenderBlocked_AMachineCheckedConditionAsksForNoReadmission(t *testing.T) {
	binding := testBinding()
	reason, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe1234"}, "needs green CI")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}

	body := renderBlocked(sdlcPrompts, binding, testTaskRecord(25), 1, orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindBlocked, Message: reason,
	}, nil)
	if !strings.Contains(body, "returns the task to the queue itself") {
		t.Fatalf("a machine-checked condition still asks for a readmission:\n%s", body)
	}
	if strings.Contains(body, "**Cleared by:** the owner") {
		t.Fatalf("a machine-checked condition named a person:\n%s", body)
	}

	spent := testTaskRecord(25)
	spent.Attempts, spent.AttemptBudget = 3, 3
	spentBody := renderBlocked(sdlcPrompts, binding, spent, 1, orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindBlocked, Message: reason,
	}, nil)
	if !strings.Contains(spentBody, "cannot be resumed as it stands") {
		t.Fatalf("a spent budget was promised an automatic return:\n%s", spentBody)
	}
}

// TestRenderBlocked_AConditionWithNoDetailStatesItself guards the case a
// caller composes a condition and writes nothing else: the comment must not
// fall back to "recorded no reason" when a condition was in fact recorded.
func TestRenderBlocked_AConditionWithNoDetailStatesItself(t *testing.T) {
	condition := UnblockCondition{Kind: UnblockOperatorRepair}
	reason, err := ComposeBlockedReason(condition, "")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	body := renderBlocked(sdlcPrompts, testBinding(), testTaskRecord(25), 1, orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindBlocked, Message: reason,
	}, nil)
	if strings.Contains(body, "recorded no reason") {
		t.Fatalf("a recorded condition rendered as no reason at all:\n%s", body)
	}
	if !strings.Contains(body, "> "+condition.Describe()) || !strings.Contains(body, "**Cleared by:** an operator") {
		t.Fatalf("the condition is not stated:\n%s", body)
	}
}

func TestLabelTransition(t *testing.T) {
	b := testBinding()
	if got := labelTransition(b, LabelBlocked); got != "`task:ready` → `factory:blocked`" {
		t.Fatalf("both = %q", got)
	}
	if got := labelTransition(b, "unknown"); got != "`task:ready` removed" {
		t.Fatalf("no display = %q", got)
	}
	b.Forge.TriggerLabel = ""
	if got := labelTransition(b, LabelBlocked); got != "`factory:blocked` applied" {
		t.Fatalf("no trigger = %q", got)
	}
	b.Forge.Labels = nil
	if got := labelTransition(b, LabelBlocked); got != "" {
		t.Fatalf("nothing = %q", got)
	}
	if blockedLabelKey(orchestration.TaskBlockKindNeedsInfo) != LabelNeedsInfo || blockedLabelKey(orchestration.TaskBlockKindEscalated) != LabelEscalated || blockedLabelKey("x") != LabelBlocked {
		t.Fatal("blockedLabelKey")
	}
}

func TestPostWithRetry(t *testing.T) {
	forge := newFakeForge()
	forge.putIssue(1, "x")
	policy := SurfacePolicy{RetryAttempts: 3, RetryInterval: 1, PostTimeout: time.Second}
	forge.postFailures = 2
	comment, err := postWithRetry(context.Background(), forge, testRepoRef, 1, "hello", policy)
	if err != nil || comment == nil || comment.ID == 0 {
		t.Fatalf("retry did not land: %v", err)
	}
	forge.postFailures = 3
	_, err = postWithRetry(context.Background(), forge, testRepoRef, 1, "hello", policy)
	var undelivered *SurfacingUndeliveredError
	if !errors.As(err, &undelivered) || undelivered.Attempts != 3 || undelivered.IssueNumber != 1 || !errors.Is(err, ErrBackend) {
		t.Fatalf("exhausted: %v", err)
	}
	// An accepted post with no id is not proof of delivery.
	forge.postFailures = 0
	forge.nextCommentID = 0
	forge.commentErr = nil
	unproven := &fakeForge{issues: map[int]*ForgeIssue{}, comments: map[int][]ForgeComment{}}
	if _, err := postWithRetry(context.Background(), unproven, testRepoRef, 1, "hello", SurfacePolicy{RetryAttempts: 1, RetryInterval: 1, PostTimeout: time.Second}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("unproven post accepted: %v", err)
	}
	// The wait between attempts ends with the context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	forge.postFailures = 1
	if _, err := postWithRetry(ctx, forge, testRepoRef, 1, "hello", SurfacePolicy{RetryAttempts: 2, RetryInterval: time.Hour, PostTimeout: time.Second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

// TestRenderBlocked_NamesTheFaultThatCausedThePark is what makes a spend cap
// legible on the issue somebody is waiting on.
//
// Without it every park reads "the task waits on an external condition",
// which is equally true of a spent spend cap, a saturated account pool and a
// full project — and the three want three different responses from the
// reader.
func TestRenderBlocked_NamesTheFaultThatCausedThePark(t *testing.T) {
	binding := testBinding()
	task := testTaskRecord(25)
	task.BlockFaultClass = orchestration.FaultAccountQuotaExhausted

	body := renderBlocked(sdlcPrompts, binding, task, 1, orchestration.BlockReason{
		Kind:    orchestration.TaskBlockKindBlocked,
		Message: "the account has spent its allowance",
	}, nil)

	if !strings.Contains(body, "**Cause:** `"+orchestration.FaultAccountQuotaExhausted+"`") {
		t.Fatalf("the comment does not name the fault class:\n%s", body)
	}
	meaning, known := orchestration.FaultClassMeaning(orchestration.FaultAccountQuotaExhausted)
	if !known {
		t.Fatal("the engine has no meaning for its own fault class")
	}
	if !strings.Contains(body, meaning) {
		t.Fatalf("the comment shows a bare identifier rather than the engine's own explanation:\n%s", body)
	}
}

// TestRenderBlocked_OmitsACauseItCannotName is the other half: a park with no
// classified fault, and one whose class this build does not recognize.
func TestRenderBlocked_OmitsACauseItCannotName(t *testing.T) {
	binding := testBinding()

	unclassified := testTaskRecord(26)
	body := renderBlocked(sdlcPrompts, binding, unclassified, 1, orchestration.BlockReason{Kind: orchestration.TaskBlockKindEscalated}, nil)
	if strings.Contains(body, "**Cause:**") {
		t.Fatalf("a park with no classified fault invented a cause:\n%s", body)
	}

	// An agent's own terminal reason reaches the record as a class this
	// package has no sentence for. It is shown as itself rather than
	// explained wrongly.
	foreign := testTaskRecord(27)
	foreign.BlockFaultClass = "agent_said_something_new"
	body = renderBlocked(sdlcPrompts, binding, foreign, 1, orchestration.BlockReason{Kind: orchestration.TaskBlockKindBlocked}, nil)
	if !strings.Contains(body, "**Cause:** `agent_said_something_new`") {
		t.Fatalf("an unrecognized class was dropped rather than shown:\n%s", body)
	}
	if strings.Contains(body, "agent_said_something_new` — ") {
		t.Fatalf("an unrecognized class was given an invented meaning:\n%s", body)
	}
}

// TestRenderOutcome_TheCauseRendersOnEveryTerminalOutcome checks the cause
// line against the rows it is NOT reached through by renderBlocked.
//
// #790 made one renderer serve every ending, so a fault class now has to read
// correctly on an exhausted budget and on an abandoned job as well as on a
// park — and those two are the endings a spend refusal most plausibly reaches,
// because a task that keeps being refused a credential runs out of attempts or
// has its node reaped while waiting.
func TestRenderOutcome_TheCauseRendersOnEveryTerminalOutcome(t *testing.T) {
	for _, kind := range []OutcomeKind{OutcomeExhausted, OutcomeAbandoned, OutcomeEscalated, OutcomeNeedsInfo} {
		t.Run(string(kind), func(t *testing.T) {
			task := testTaskRecord(60)
			task.BlockFaultClass = orchestration.FaultAccountBreakerTripped
			body := renderOutcome(outcomeReport{
				binding: testBinding(), task: task, attempt: 1, kind: kind,
				message: "the account keeps failing",
			})
			meaning, known := orchestration.FaultClassMeaning(orchestration.FaultAccountBreakerTripped)
			if !known {
				t.Fatal("the engine has no meaning for its own fault class")
			}
			if !strings.Contains(body, "**Cause:** `"+orchestration.FaultAccountBreakerTripped+"` — "+meaning) {
				t.Fatalf("the %s comment does not name the fault that caused it:\n%s", kind, body)
			}
		})
	}
}

// TestRenderOutcome_AReadmissionNamesNoCause is the negative half, and it is
// pinned rather than trusted.
//
// A readmission is not a stop, so there is nothing to explain — and the field
// backing the cause is cleared on the way back to the queue (requeue and the
// readmit path both zero Task.BlockFaultClass), so this holds for free TODAY.
// "For free today" is exactly the kind of property that stops holding when
// somebody keeps the class around for history, which is why it is asserted
// here instead of left to the reader of two other files.
func TestRenderOutcome_AReadmissionNamesNoCause(t *testing.T) {
	task := testTaskRecord(61)
	body := renderOutcome(outcomeReport{
		binding: testBinding(), task: task, attempt: 1, kind: OutcomeReadmitted,
	})
	if strings.Contains(body, "**Cause:**") {
		t.Fatalf("a readmission explained a stop that is over:\n%s", body)
	}

	// And the contrast: were the class still set, the renderer WOULD say so.
	// Without this the test above passes just as well against a renderer that
	// never emits a cause at all.
	task.BlockFaultClass = orchestration.FaultAccountQuotaExhausted
	body = renderOutcome(outcomeReport{
		binding: testBinding(), task: task, attempt: 1, kind: OutcomeReadmitted,
	})
	if !strings.Contains(body, "**Cause:**") {
		t.Fatalf("the readmission case proves nothing: the renderer emits no cause even when one is set:\n%s", body)
	}
}

// TestRenderBlocked_CauseAndConditionAreIndependent is the composition guard
// for the two mechanisms that landed together (#758's fault classes and
// #759's typed unblock conditions).
//
// They answer different questions from different records — what STOPPED the
// task (Task.BlockFaultClass) and what would RETURN it to the queue (the
// condition in the park reason) — so a park may carry either alone or both,
// and neither may swallow the other. Written as one test over the four
// combinations because the defect worth catching is exactly a branch that
// renders one only when the other is absent.
func TestRenderBlocked_CauseAndConditionAreIndependent(t *testing.T) {
	binding := testBinding()
	condition := UnblockCondition{Kind: UnblockOperatorRepair}
	conditioned, err := ComposeBlockedReason(condition, "the runner cannot reach its forge")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}

	cases := []struct {
		name       string
		faultClass string
		message    string
		wantCause  bool
		wantCond   bool
	}{
		{
			name:       "both: an environment fault parked with a typed condition",
			faultClass: orchestration.FaultAccountQuotaExhausted,
			message:    conditioned,
			wantCause:  true,
			wantCond:   true,
		},
		{
			// The ordinary spend-cap park: three consecutive environment
			// faults hit the bound, and nothing composed a condition.
			name:       "cause only: the consecutive-fault bound",
			faultClass: orchestration.FaultAccountQuotaExhausted,
			message:    "3 consecutive environmental failures",
			wantCause:  true,
			wantCond:   false,
		},
		{
			name:      "condition only: an owner ruling, which no fault caused",
			message:   conditioned,
			wantCause: false,
			wantCond:  true,
		},
		{
			name:      "neither: prose, as it always was",
			message:   "the agent asked a question",
			wantCause: false,
			wantCond:  false,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := testTaskRecord(40 + i)
			task.BlockFaultClass = tc.faultClass
			body := renderBlocked(sdlcPrompts, binding, task, 1, orchestration.BlockReason{
				Kind: orchestration.TaskBlockKindBlocked, Message: tc.message,
			}, nil)

			if got := strings.Contains(body, "**Cause:**"); got != tc.wantCause {
				t.Errorf("cause rendered = %t, want %t:\n%s", got, tc.wantCause, body)
			}
			if got := strings.Contains(body, "**Unblocks when:**"); got != tc.wantCond {
				t.Errorf("condition rendered = %t, want %t:\n%s", got, tc.wantCond, body)
			}
			if tc.wantCause && tc.wantCond {
				// The order is the causal chain a reader follows: what
				// stopped it, then what would let it go.
				if strings.Index(body, "**Cause:**") > strings.Index(body, "**Unblocks when:**") {
					t.Errorf("the cause is rendered after the condition it caused:\n%s", body)
				}
			}
		})
	}
}

// TestAttemptLine_NamesQuestionsAndFaultsApart is the operator-facing half of
// the refund change.
//
// THIS TEXT GOES ON THE ISSUE, so calling a question an environment fault would
// send whoever reads it to check a deployment that is perfectly fine. Both
// kinds are refunded; only one of them means something is broken.
func TestAttemptLine_NamesQuestionsAndFaultsApart(t *testing.T) {
	both := attemptLine(&orchestration.Task{
		Attempts: 9, EnvironmentFaults: 2, QuestionAttempts: 5, AttemptBudget: 3,
	})
	for _, want := range []string{"2 environment faults", "5 questions", "9 dispatches", "not charged"} {
		if !strings.Contains(both, want) {
			t.Fatalf("attempt line %q is missing %q", both, want)
		}
	}
	if !strings.HasPrefix(both, "2 of 3") {
		t.Fatalf("the charged count leads the line: %q", both)
	}

	// Questions alone must not borrow the environment-fault wording.
	only := attemptLine(&orchestration.Task{Attempts: 4, QuestionAttempts: 4, AttemptBudget: 3})
	if strings.Contains(only, "environment") {
		t.Fatalf("a task that only asked questions must not mention environment faults: %q", only)
	}
	if !strings.Contains(only, "4 questions") {
		t.Fatalf("the question refund must be named: %q", only)
	}
}

// TestAttemptLine_OmitsTheParentheticalWhenNothingIsRefunded keeps the common
// case terse: a task that simply failed twice says so and no more.
func TestAttemptLine_OmitsTheParentheticalWhenNothingIsRefunded(t *testing.T) {
	line := attemptLine(&orchestration.Task{Attempts: 2, AttemptBudget: 3})
	if strings.Contains(line, "(") {
		t.Fatalf("no refunds means no parenthetical: %q", line)
	}
	if line != "2 of 3" {
		t.Fatalf("attempt line = %q, want \"2 of 3\"", line)
	}
}
