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
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// An operator fault is not an attempt at the work
// =============================================================================
//
// go-quicbaas#908, measured: task 729e87c6 was dispatched twice, once against an
// empty worktree and once against a workspace missing a repository nobody had
// mounted. Both were parked correctly and both were CHARGED, so a task with a
// budget of three had one attempt left for the first real difficulty after the
// repair — and the log said "charged as a failure of the work" on the line after
// the one that said an operator had to fix it.
//
// THE HARD DIRECTION IS THE OTHER ONE, and it is tested hardest below: a
// genuine failure read as infrastructure would be refunded, requeued, refunded
// again, and retried for ever. So every branch that does NOT refund is asserted
// as explicitly as the one that does.

// operatorFaultPark is the park a runner makes when it proved the factory, not
// the work, was at fault.
func operatorFaultPark(class string) *BlockReason {
	return &BlockReason{
		Kind:       TaskBlockKindBlocked,
		Message:    "a context repository this project declared is not there",
		FaultClass: class,
		Surfaced:   true,
	}
}

// TestApplyConclusion_AnOperatorFaultParkRefundsTheAttempt: the accounting half
// of #908. The park still stops the task; what changes is that the attempt it
// spent comes back.
func TestApplyConclusion_AnOperatorFaultParkRefundsTheAttempt(t *testing.T) {
	t.Parallel()

	for _, class := range []string{FaultWorkspaceUnfit, FaultWorkerEnvironment} {
		t.Run(class, func(t *testing.T) {
			task := parkedTask()
			if charged := task.ChargedAttempts(); charged != 1 {
				t.Fatalf("the fixture starts at %d charged attempts; this test proves nothing", charged)
			}

			reason := applyConclusion(task, attemptConclusion{
				status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
				park:   operatorFaultPark(class),
			}, time.Now().UTC())

			if reason != TaskConcludedRunnerParked {
				t.Fatalf("conclusion = %q, want runner_parked", reason)
			}
			if task.Status != TaskStatusBlocked {
				t.Fatalf("status = %s, want blocked — the park still stops the task", task.Status)
			}
			// THE ATTEMPT CAME BACK: the job was dispatched and charged nothing.
			if got := task.ChargedAttempts(); got != 0 {
				t.Fatalf("charged attempts = %d, want 0 — the operator's fault was charged to the work", got)
			}
			if task.EnvironmentFaults != 1 {
				t.Fatalf("environment faults = %d, want the refund recorded", task.EnvironmentFaults)
			}
			// THE CLASS IS ON THE BLOCK, which is what lets a re-label after
			// the repair release the task source and what names the cause on
			// the issue.
			if task.BlockFaultClass != class {
				t.Fatalf("block fault class = %q, want %q", task.BlockFaultClass, class)
			}
			if !taskSourceReleasable(task.Status, task.BlockFaultClass) {
				t.Fatal("an operator-fault hold does not release its source, so a re-label cannot readmit it")
			}
			// It is not a REQUEUE either: nothing retries until a person acts.
			if task.RetryNotBefore != 0 || task.ConsecutiveEnvironmentFaults != 0 {
				t.Fatalf("the park was turned into a timed retry: %d / %d",
					task.RetryNotBefore, task.ConsecutiveEnvironmentFaults)
			}
		})
	}
}

// TestApplyConclusion_AParkAboutTheWorkIsStillCharged is the direction that
// must not regress: a park recording a judgement about the work spends its
// attempt.
//
// NEEDS_INFO WAS REMOVED FROM THIS SET (owner decision, 2026-09-14), reversing
// the rule this test previously stated. The original reasoning — "a worker's
// question is a judgement about the work" — does not survive one observation:
// a needs_info park NEVER RETRIES. It is terminal until a person acts, so the
// attempt budget bounds nothing there, and the only effect of charging it is
// that when the answer finally arrives the readmission meets a task with no
// allowance left and needs a second manual step. Measured on the burn-in
// fleet: three go-luks2 tasks at seven charged attempts against a budget of
// three, spent entirely on unanswered questions.
//
// Escalated and blocked stay charged and are still covered here.
func TestApplyConclusion_AParkAboutTheWorkIsStillCharged(t *testing.T) {
	t.Parallel()

	for _, kind := range []TaskBlockKind{TaskBlockKindEscalated, TaskBlockKindBlocked} {
		t.Run(string(kind), func(t *testing.T) {
			task := parkedTask()
			charged := task.ChargedAttempts()

			applyConclusion(task, attemptConclusion{
				status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
				park:   &BlockReason{Kind: kind, Message: "which store should hold the index?"},
			}, time.Now().UTC())

			if got := task.ChargedAttempts(); got != charged {
				t.Fatalf("charged attempts = %d, want %d unchanged — an unattributed park is charged as it always was",
					got, charged)
			}
			if task.EnvironmentFaults != 0 {
				t.Fatalf("environment faults = %d, want a park about the work refunded nothing", task.EnvironmentFaults)
			}
			if task.QuestionAttempts != 0 {
				t.Fatalf("question attempts = %d, want a park about the work refunded nothing", task.QuestionAttempts)
			}
			if task.BlockFaultClass != "" {
				t.Fatalf("block fault class = %q, want none for a park about the work", task.BlockFaultClass)
			}
			// AND ITS SOURCE STAYS HELD: a re-label must not supersede a
			// question nobody has answered.
			if taskSourceReleasable(task.Status, task.BlockFaultClass) {
				t.Fatal("a park about the work released its source, so re-labelling would skip the question")
			}
		})
	}
}

// TestApplyConclusion_AParkCarryingAClassNothingProducesIsCharged is the
// fail-closed direction stated as a test. A class this binary does not
// recognise — a typo, a record from a newer build — must be read as a failure of
// the work, because the alternative refunds every attempt of a broken change.
func TestApplyConclusion_AParkCarryingAClassNothingProducesIsCharged(t *testing.T) {
	t.Parallel()

	for _, class := range []string{
		"workspace-unfit",        // a plausible typo
		FaultNoProviderAccount,   // a real class, but not an operator fault
		JobFailureClassAbandoned, // a failure class, not a fault class
		"",                       // the ordinary park
	} {
		t.Run("class="+class, func(t *testing.T) {
			task := parkedTask()
			charged := task.ChargedAttempts()

			applyConclusion(task, attemptConclusion{
				status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
				park:   operatorFaultPark(class),
			}, time.Now().UTC())

			if got := task.ChargedAttempts(); got != charged {
				t.Fatalf("charged attempts = %d, want %d — %q refunded an attempt it should not",
					got, charged, class)
			}
			if task.EnvironmentFaults != 0 {
				t.Fatalf("%q was refunded as an operator fault", class)
			}
		})
	}
}

// TestApplyConclusion_AnOperatorFaultParkCannotExhaustABudget: the whole point
// of the refund. A task whose every remaining attempt meets the same unmounted
// repository must not end `failed` on a budget nothing spent.
func TestApplyConclusion_AnOperatorFaultParkCannotExhaustABudget(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	task.AttemptBudget = 3
	for attempt := 1; attempt <= 5; attempt++ {
		task.Attempts = attempt
		task.Status = TaskStatusInProgress
		task.SpawnedJobID = "job-" + itoa(attempt)
		applyConclusion(task, attemptConclusion{
			status: &JobStatus{JobID: "job-" + itoa(attempt), State: JobStateFailed, Error: "escalated"},
			park:   operatorFaultPark(FaultWorkspaceUnfit),
		}, time.Now().UTC())

		if task.BudgetExhausted() {
			t.Fatalf("attempt %d exhausted the budget on an operator's fault", attempt)
		}
		if task.Status != TaskStatusBlocked {
			t.Fatalf("attempt %d rests as %s, want blocked", attempt, task.Status)
		}
	}
	// AND THE BUDGET IS INTACT for the work itself: the first genuine failure
	// after the repair is the first charged attempt.
	task.Attempts = 6
	task.Status = TaskStatusInProgress
	task.SpawnedJobID = "job-6"
	applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-6", State: JobStateFailed, Error: "the tests fail"},
	}, time.Now().UTC())
	if charged := task.ChargedAttempts(); charged != 1 {
		t.Fatalf("charged attempts = %d, want 1: five operator faults spent nothing", charged)
	}
}

// TestApplyConclusion_AnOperatorFaultParkConsumesNoReviewRound: the park
// records the rounds the run actually ran, and a run that never reached a
// worker ran none.
func TestApplyConclusion_AnOperatorFaultParkConsumesNoReviewRound(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	park := operatorFaultPark(FaultWorkspaceUnfit)
	park.Iterations = 0
	applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
		park:   park,
	}, time.Now().UTC())

	if task.Result == nil {
		t.Fatal("the conclusion recorded no result")
	}
	// The conclusion records the job and its status; it records no failure
	// class, because the job did not fail on the environment's dispatch path.
	if _, charged := task.Result.Metadata["failure_class"]; charged {
		t.Fatalf("the result named a failure class: %+v", task.Result.Metadata)
	}
}

// =============================================================================
// The vocabulary
// =============================================================================

// TestOperatorFaultClass_KnowsExactlyTheClassesThatRefund: the predicate that
// decides a refund answers true for the two classes a runner can prove and for
// nothing else.
func TestOperatorFaultClass_KnowsExactlyTheClassesThatRefund(t *testing.T) {
	t.Parallel()

	for _, class := range []string{FaultWorkspaceUnfit, FaultWorkerEnvironment, FaultConfigError} {
		if !OperatorFaultClass(class) {
			t.Fatalf("%q is not recognised as an operator fault", class)
		}
		if meaning, known := FaultClassMeaning(class); !known || meaning == "" {
			t.Fatalf("%q has no meaning; it would render as a bare identifier on a forge issue", class)
		}
	}
	for class := range resolverFaultClasses {
		if OperatorFaultClass(class) {
			t.Fatalf("%q is a dispatch-path fault and must not be read as an operator fault", class)
		}
	}
	for _, class := range []string{"", "workspace_unfit ", "WORKSPACE_UNFIT", "operator", JobFailureClassStalled} {
		if OperatorFaultClass(class) {
			t.Fatalf("%q was read as an operator fault", class)
		}
	}
}

// TestKnownFaultClass_IncludesTheOperatorFaults: KnownFaultClass is what
// decides whether a blocked task's source releases on a re-label, so a class
// the accounting produces and it does not know would leave the task held for
// ever after the operator repaired the thing it named.
func TestKnownFaultClass_IncludesTheOperatorFaults(t *testing.T) {
	t.Parallel()

	for class := range operatorFaultClasses {
		if !KnownFaultClass(class) {
			t.Fatalf("%q is produced by a park and unknown to KnownFaultClass", class)
		}
	}
	if KnownFaultClass("not_a_class") {
		t.Fatal("an invented class is known")
	}
}

// TestFaultClassMeaning_EveryOperatorFaultIsExplained: the meanings are what a
// person reads on the issue, so a class with none renders as a bare identifier
// beside "Cause:".
func TestFaultClassMeaning_EveryOperatorFaultIsExplained(t *testing.T) {
	t.Parallel()

	for class := range operatorFaultClasses {
		meaning, known := FaultClassMeaning(class)
		if !known || meaning == "" {
			t.Fatalf("%q has no meaning to render", class)
		}
	}
}

// TestJobRun_ParkCarriesTheAttributionAndDropsAnInventedOne: the park is the
// only channel the attribution travels on, and the class is what decides a
// refund — so a caller must not be able to refund an attempt by naming a class
// nothing produces, and must not be able to LOSE a park by mistyping one.
func TestJobRun_ParkCarriesTheAttributionAndDropsAnInventedOne(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	run.Park(TaskBlockKindBlocked, "the handbook is not there", FaultWorkspaceUnfit)
	parked := run.Parked()
	if parked == nil || parked.FaultClass != FaultWorkspaceUnfit {
		t.Fatalf("parked = %+v, want the workspace fault class", parked)
	}

	run.Park(TaskBlockKindBlocked, "the handbook is not there", "invented_class")
	parked = run.Parked()
	if parked == nil {
		t.Fatal("an invented class lost the park entirely")
	}
	if parked.FaultClass != "" {
		t.Fatalf("fault class = %q, want the invented class dropped", parked.FaultClass)
	}
	if parked.Message != "the handbook is not there" || parked.Kind != TaskBlockKindBlocked {
		t.Fatalf("parked = %+v, want the park itself kept", parked)
	}
}

// TestJobRun_ParkStillRefusesAnUnknownKind: the kind's own guard is unchanged by
// the attribution — a kind nothing can render still parks nothing.
func TestJobRun_ParkStillRefusesAnUnknownKind(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	run.Park(TaskBlockKind("invented"), "a reason", FaultWorkspaceUnfit)
	if parked := run.Parked(); parked != nil {
		t.Fatalf("parked = %+v, want nothing for a kind nothing can render", parked)
	}
}

// =============================================================================
// What the log says
// =============================================================================

// TestLogAttribution_SaysWhoseFaultItWas: the burn-in's log printed
// "charged as a failure of the work" on the line AFTER the one saying an
// operator had to repair it (go-quicbaas#908). The line is what a person greps
// when the accounting looks wrong, so a line that contradicts the accounting
// sends the reader after a bug in the wrong half.
func TestLogAttribution_SaysWhoseFaultItWas(t *testing.T) {
	t.Parallel()

	loopErr := errors.New("sdlc runner: refused: task 729e87c6 escalated")
	cases := []struct {
		name       string
		status     *JobStatus
		concludeOk bool
		loopErr    error
		want       string
		absent     string
	}{
		{
			name: "an operator fault is not charged to the work",
			status: &JobStatus{
				JobID: "job-1", State: JobStateFailed,
				Park: &BlockReason{Kind: TaskBlockKindBlocked, FaultClass: FaultWorkspaceUnfit},
			},
			loopErr: loopErr,
			want:    "attributed to the factory",
			absent:  "charged as a failure of the work",
		},
		{
			name: "a park about the work is charged",
			status: &JobStatus{
				JobID: "job-1", State: JobStateFailed,
				Park: &BlockReason{Kind: TaskBlockKindNeedsInfo},
			},
			loopErr: loopErr,
			want:    "charged as a failure of the work",
			absent:  "attributed to the factory",
		},
		{
			name:    "a plain failure is charged",
			status:  &JobStatus{JobID: "job-1", State: JobStateFailed},
			loopErr: loopErr,
			want:    "charged as a failure of the work",
			absent:  "attributed to the factory",
		},
		{
			name: "a park carrying an invented class is charged",
			status: &JobStatus{
				JobID: "job-1", State: JobStateFailed,
				Park: &BlockReason{Kind: TaskBlockKindBlocked, FaultClass: "invented_class"},
			},
			loopErr: loopErr,
			want:    "charged as a failure of the work",
			absent:  "attributed to the factory",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := newRecordingLogger()
			svc := &agentServiceImpl{logger: logger}
			svc.logAttribution(tc.status, tc.loopErr, nil)
			joined := logger.atLevel("DEBUG")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("log = %q, want it to say %q", joined, tc.want)
			}
			if strings.Contains(joined, tc.absent) {
				t.Fatalf("log = %q, want it NOT to say %q", joined, tc.absent)
			}
		})
	}
}

// TestLogAttribution_SaysNothingWhenThereIsNothingToAttribute: a job that
// succeeded, a job whose failure class already names the cause, and a
// conclusion that was not applied at all each leave the line unwritten.
func TestLogAttribution_SaysNothingWhenThereIsNothingToAttribute(t *testing.T) {
	t.Parallel()

	loopErr := errors.New("it failed")
	cases := []struct {
		name       string
		status     *JobStatus
		loopErr    error
		concludeEr error
	}{
		{name: "no loop error", status: &JobStatus{JobID: "job-1"}, loopErr: nil},
		{
			name:    "the failure class already named it",
			status:  &JobStatus{JobID: "job-1", FailureClass: JobFailureClassStalled},
			loopErr: loopErr,
		},
		{
			name:       "the conclusion was not applied",
			status:     &JobStatus{JobID: "job-1"},
			loopErr:    loopErr,
			concludeEr: errors.New("the task moved under it"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := newRecordingLogger()
			svc := &agentServiceImpl{logger: logger}
			svc.logAttribution(tc.status, tc.loopErr, tc.concludeEr)
			if lines := logger.all(); lines != "" {
				t.Fatalf("log = %q, want nothing attributed", lines)
			}
		})
	}
}

// TestApplyConclusion_AQuestionIsRefundedNotCharged is the counterpart: the one
// park kind that is waiting on a person costs no budget.
//
// IT IS NOT AN ENVIRONMENT FAULT, and the separate counter is the point. The
// blocked comment tells the operator "N of them environment faults, not
// charged", so folding a question in there would report a worker that did
// exactly the right thing as a broken deployment.
func TestApplyConclusion_AQuestionIsRefundedNotCharged(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	before := task.ChargedAttempts()

	applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "question"},
		park:   &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store should hold the index?"},
	}, time.Now().UTC())

	// The dispatch that ended in a question is GIVEN BACK, so the charged
	// count falls by one: it was counted at dispatch, before anyone knew the
	// turn would end by asking rather than by judging the work.
	if got := task.ChargedAttempts(); got != before-1 {
		t.Fatalf("charged attempts = %d, want %d — the attempt spent asking is refunded", got, before-1)
	}
	if task.QuestionAttempts != 1 {
		t.Fatalf("question attempts = %d, want 1", task.QuestionAttempts)
	}
	if task.EnvironmentFaults != 0 {
		t.Fatalf("environment faults = %d, want 0 — a question is not a broken deployment", task.EnvironmentFaults)
	}
	// AND ITS SOURCE STAYS HELD, exactly as before: a re-label must not
	// supersede a question nobody has answered.
	if taskSourceReleasable(task.Status, task.BlockFaultClass) {
		t.Fatal("a question released its source, so re-labelling would skip it")
	}
}
