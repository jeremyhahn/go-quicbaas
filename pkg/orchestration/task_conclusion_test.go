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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// The vocabulary
// =============================================================================

// TestTaskConclusionReasons_IsClosedAndSorted proves the enumerating half and
// the membership half of the vocabulary agree, and that the order is stable
// for the module that covers the set.
func TestTaskConclusionReasons_IsClosedAndSorted(t *testing.T) {
	reasons := TaskConclusionReasons()
	if len(reasons) != len(taskConclusionReasons) {
		t.Fatalf("the list holds %d of %d reasons", len(reasons), len(taskConclusionReasons))
	}
	if !sort.SliceIsSorted(reasons, func(i, j int) bool { return reasons[i] < reasons[j] }) {
		t.Fatalf("the list is not sorted: %v", reasons)
	}
	for _, reason := range reasons {
		if !reason.Valid() {
			t.Fatalf("listed reason %q is not a member", reason)
		}
		if reason.String() != string(reason) {
			t.Fatalf("reason %q renders as %q", reason, reason.String())
		}
	}
	for _, named := range []TaskConclusionReason{
		TaskConcludedCompleted, TaskConcludedBudgetExhausted, TaskConcludedAbandoned,
		TaskConcludedFaultBounded, TaskConcludedFaultHeld, TaskConcludedParkedNeedsInfo,
		TaskConcludedParkedBlocked, TaskConcludedParkedEscalated, TaskConcludedReadmitted,
	} {
		if !named.Valid() {
			t.Fatalf("constant %q is not in the set", named)
		}
	}
}

// TestTaskConclusionReason_RefusesWhatItDoesNotName is the negative: a reason
// from nowhere is not a member, and neither is the empty string.
func TestTaskConclusionReason_RefusesWhatItDoesNotName(t *testing.T) {
	for _, reason := range []TaskConclusionReason{"", "budget_spent", "COMPLETED"} {
		if reason.Valid() {
			t.Fatalf("reason %q must not be a member", reason)
		}
	}
}

// TestConclusionOfBlockKind_CoversTheClosedEnum proves an explicit park names
// its conclusion from the same vocabulary as the attempt accounting.
func TestConclusionOfBlockKind_CoversTheClosedEnum(t *testing.T) {
	for kind := range taskBlockKinds {
		reason, ok := ConclusionOfBlockKind(kind)
		if !ok {
			t.Fatalf("block kind %q concludes nothing", kind)
		}
		if !reason.Valid() {
			t.Fatalf("block kind %q concludes %q, which is not a member", kind, reason)
		}
	}
	if reason, ok := ConclusionOfBlockKind(TaskBlockKind("wedged")); ok {
		t.Fatalf("an unknown block kind must conclude nothing, got %q", reason)
	}
}

// =============================================================================
// The conclusion
// =============================================================================

func taskConclusionFixture() TaskConclusion {
	return TaskConclusion{
		TenantID: "acme",
		Reason:   TaskConcludedBudgetExhausted,
		Task:     &Task{ID: "task-1", TenantID: "acme"},
		JobID:    "job-3",
	}
}

// TestTaskConclusion_ValidateAcceptsAWholeConclusion is the positive.
func TestTaskConclusion_ValidateAcceptsAWholeConclusion(t *testing.T) {
	if err := taskConclusionFixture().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestTaskConclusion_ValidateRefusesWhatAnObserverCannotRead is the negative:
// each field an observer needs, missing in turn.
func TestTaskConclusion_ValidateRefusesWhatAnObserverCannotRead(t *testing.T) {
	cases := map[string]func(*TaskConclusion){
		"tenant_id": func(c *TaskConclusion) { c.TenantID = "" },
		"task":      func(c *TaskConclusion) { c.Task = nil },
		"task.id":   func(c *TaskConclusion) { c.Task = &Task{} },
		"reason":    func(c *TaskConclusion) { c.Reason = "made_up" },
	}
	for field, break_ := range cases {
		conclusion := taskConclusionFixture()
		break_(&conclusion)
		err := conclusion.Validate()
		var invalid *TaskConclusionInvalidError
		if !errors.As(err, &invalid) || !errors.Is(err, ErrTaskConclusionInvalid) {
			t.Fatalf("%s: expected TaskConclusionInvalidError, got %v", field, err)
		}
		if invalid.Field != field {
			t.Fatalf("expected field %q, got %q", field, invalid.Field)
		}
		if !strings.Contains(invalid.Error(), field) {
			t.Fatalf("error text does not name the field: %q", invalid.Error())
		}
	}
}

// =============================================================================
// The registry
// =============================================================================

// recordingObserver records every conclusion it is told about and can be made
// to fail.
type recordingObserver struct {
	mu   sync.Mutex
	seen []TaskConclusion
	err  error
}

func (o *recordingObserver) TaskConcluded(_ context.Context, conclusion TaskConclusion) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, conclusion)
	return o.err
}

func (o *recordingObserver) conclusions() []TaskConclusion {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]TaskConclusion(nil), o.seen...)
}

// TestTaskConclusionRegistry_NotifiesEveryObserver is the positive: both
// observers hear about the conclusion, in name order.
func TestTaskConclusionRegistry_NotifiesEveryObserver(t *testing.T) {
	registry := NewTaskConclusionRegistry()
	first, second := &recordingObserver{}, &recordingObserver{}
	if err := registry.Register("a-first", first); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register("b-second", second); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Notify(context.Background(), taskConclusionFixture()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	for name, observer := range map[string]*recordingObserver{"a-first": first, "b-second": second} {
		seen := observer.conclusions()
		if len(seen) != 1 || seen[0].Reason != TaskConcludedBudgetExhausted || seen[0].JobID != "job-3" {
			t.Fatalf("%s heard %+v", name, seen)
		}
	}
	if got := registry.names(); len(got) != 2 || got[0] != "a-first" || got[1] != "b-second" {
		t.Fatalf("observers are not in a stable order: %v", got)
	}
}

// TestTaskConclusionRegistry_RefusesABadRegistration is the negative for the
// three ways a registration is refused.
func TestTaskConclusionRegistry_RefusesABadRegistration(t *testing.T) {
	registry := NewTaskConclusionRegistry()
	observer := &recordingObserver{}

	var refused *TaskConclusionRegistrationError
	if err := registry.Register("", observer); !errors.As(err, &refused) || !errors.Is(err, ErrTaskConclusionRegistration) {
		t.Fatalf("an unnamed observer must be refused, got %v", err)
	}
	if err := registry.Register("named", nil); !errors.As(err, &refused) || refused.Name != "named" {
		t.Fatalf("a nil observer must be refused, got %v", err)
	}
	if err := registry.Register("named", observer); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var taken *TaskConclusionAlreadyRegisteredError
	err := registry.Register("named", observer)
	if !errors.As(err, &taken) || !errors.Is(err, ErrTaskConclusionAlreadyRegistered) || taken.Name != "named" {
		t.Fatalf("a taken name must be refused, got %v", err)
	}
	if !strings.Contains(taken.Error(), "named") || !strings.Contains(refused.Error(), "named") {
		t.Fatalf("refusals must name the observer: %q %q", taken.Error(), refused.Error())
	}
}

// TestTaskConclusionRegistry_OneFailureDoesNotSilenceTheRest proves the
// reports are independent: a forge that will not answer must not stop the
// next observer from hearing about the conclusion.
func TestTaskConclusionRegistry_OneFailureDoesNotSilenceTheRest(t *testing.T) {
	registry := NewTaskConclusionRegistry()
	broken := &recordingObserver{err: errors.New("forge down")}
	working := &recordingObserver{}
	if err := registry.Register("a-broken", broken); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register("b-working", working); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := registry.Notify(context.Background(), taskConclusionFixture())
	var notSurfaced *TaskConclusionNotSurfacedError
	if !errors.As(err, &notSurfaced) || !errors.Is(err, ErrTaskConclusionNotSurfaced) {
		t.Fatalf("expected TaskConclusionNotSurfacedError, got %v", err)
	}
	if notSurfaced.Observer != "a-broken" || notSurfaced.TaskID != "task-1" {
		t.Fatalf("the failure must name who failed about what: %+v", notSurfaced)
	}
	if !strings.Contains(notSurfaced.Error(), "forge down") {
		t.Fatalf("the failure must carry its cause: %q", notSurfaced.Error())
	}
	if len(working.conclusions()) != 1 {
		t.Fatal("the working observer was silenced by the broken one")
	}

	bare := &TaskConclusionNotSurfacedError{Observer: "x", TaskID: "y", Reason: TaskConcludedReadmitted}
	if unwrapped := bare.Unwrap(); len(unwrapped) != 1 || !errors.Is(bare, ErrTaskConclusionNotSurfaced) {
		t.Fatalf("a causeless failure still unwraps to the sentinel: %v", unwrapped)
	}
}

// TestTaskConclusionRegistry_RefusesAnUnusableConclusion proves the guard runs
// before any observer is called.
func TestTaskConclusionRegistry_RefusesAnUnusableConclusion(t *testing.T) {
	registry := NewTaskConclusionRegistry()
	observer := &recordingObserver{}
	if err := registry.Register("only", observer); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := registry.Notify(context.Background(), TaskConclusion{TenantID: "acme", Reason: TaskConcludedCompleted})
	if !errors.Is(err, ErrTaskConclusionInvalid) {
		t.Fatalf("expected the invalid sentinel, got %v", err)
	}
	if len(observer.conclusions()) != 0 {
		t.Fatal("an unusable conclusion must reach no observer")
	}
}

// TestRegisterTaskConclusionObserver_UsesTheProcessRegistry proves the
// package-level door and the registry behind it are the same one, and that it
// is append-only like the engine's other seams.
func TestRegisterTaskConclusionObserver_UsesTheProcessRegistry(t *testing.T) {
	observer := installTestConclusionObserver(t)
	if err := RegisterTaskConclusionObserver(testConclusionObserverName, &recordingObserver{}); !errors.Is(err, ErrTaskConclusionAlreadyRegistered) {
		t.Fatalf("the process registry must be append-only, got %v", err)
	}
	if err := NotifyTaskConcluded(context.Background(), taskConclusionFixture()); err != nil {
		t.Fatalf("NotifyTaskConcluded: %v", err)
	}
	if seen := observer.conclusions(); len(seen) != 1 || seen[0].Task.ID != "task-1" {
		t.Fatalf("the process registry did not carry the conclusion: %+v", seen)
	}
}

// =============================================================================
// The wiring: a conclusion the store wrote reaches the observers
// =============================================================================

// testConclusionObserverName is the one name this package's tests register
// under. The process registry is append-only, so registration happens once
// and the SINK is what a test installs and removes.
const testConclusionObserverName = "orchestration-test"

// testConclusionSink is the process-registered observer whose current
// recipient a test swaps in and out.
type testConclusionSink struct {
	mu   sync.Mutex
	into *recordingObserver
}

func (s *testConclusionSink) TaskConcluded(ctx context.Context, conclusion TaskConclusion) error {
	s.mu.Lock()
	into := s.into
	s.mu.Unlock()
	if into == nil {
		return nil
	}
	return into.TaskConcluded(ctx, conclusion)
}

func (s *testConclusionSink) attach(into *recordingObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.into = into
}

var (
	testConclusionSinkOnce sync.Once
	errTestConclusionSink  error
	theTestConclusionSink  = &testConclusionSink{}
)

// installTestConclusionObserver points the process-registered sink at a fresh
// recorder for the duration of one test.
func installTestConclusionObserver(t *testing.T) *recordingObserver {
	t.Helper()
	testConclusionSinkOnce.Do(func() {
		errTestConclusionSink = RegisterTaskConclusionObserver(testConclusionObserverName, theTestConclusionSink)
	})
	if errTestConclusionSink != nil {
		t.Fatalf("registering the test observer: %v", errTestConclusionSink)
	}
	observer := &recordingObserver{}
	theTestConclusionSink.attach(observer)
	t.Cleanup(func() { theTestConclusionSink.attach(nil) })
	return observer
}

// TestConcludeAttempt_ASpentBudgetIsAnnounced is go-quicbaas#788's unit: the
// third charged attempt of a three-attempt task fails, the task is failed with
// budget_exhausted, and an observer is told so — where before nothing was.
func TestConcludeAttempt_ASpentBudgetIsAnnounced(t *testing.T) {
	observer := installTestConclusionObserver(t)
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 3, AttemptBudget: 3, SpawnedJobID: "job-3"})

	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-3", "", "the worker changed nothing"),
	})
	if err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusFailed || task.Result.Metadata["reason"] != budgetExhaustedReason {
		t.Fatalf("expected a budget-exhausted failure, got %+v", task)
	}

	seen := observer.conclusions()
	if len(seen) != 1 {
		t.Fatalf("expected exactly one announcement, got %d", len(seen))
	}
	if seen[0].Reason != TaskConcludedBudgetExhausted || seen[0].JobID != "job-3" || seen[0].TenantID != "acme" {
		t.Fatalf("the announcement is wrong: %+v", seen[0])
	}
	if seen[0].Task.Status != TaskStatusFailed || seen[0].Task.Result.Output != "the worker changed nothing" {
		t.Fatalf("the announcement must carry the concluded record: %+v", seen[0].Task)
	}
}

// TestConcludeAttempt_ARequeueAnnouncesNothing is the negative: an attempt
// that ends without concluding the task is not an outcome anyone should hear
// about, and a comment per failed attempt would bury the issue.
func TestConcludeAttempt_ARequeueAnnouncesNothing(t *testing.T) {
	observer := installTestConclusionObserver(t)
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1, AttemptBudget: 3, SpawnedJobID: "job-1"})

	err := svc.concludeAttempt(context.Background(), "acme", seeded.ID, attemptConclusion{
		status: failedStatus("job-1", "", "round one failed"),
	})
	if err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}
	if task := readTask(t, store, seeded.ID); task.Status != TaskStatusPending {
		t.Fatalf("expected a requeue, got %s", task.Status)
	}
	if seen := observer.conclusions(); len(seen) != 0 {
		t.Fatalf("a requeue concludes nothing, got %+v", seen)
	}
}

// TestConcludeAttempt_EveryTerminalBranchIsNamed walks applyConclusion's
// branches and proves each names a member of the vocabulary — the enumeration
// go-quicbaas#788 asks for, asserted rather than described.
func TestConcludeAttempt_EveryTerminalBranchIsNamed(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name string
		task *Task
		c    attemptConclusion
		want TaskConclusionReason
	}{
		{
			name: "completed",
			task: &Task{ID: "t", Attempts: 1, AttemptBudget: 3},
			c:    attemptConclusion{status: &JobStatus{JobID: "j", State: JobStateCompleted}},
			want: TaskConcludedCompleted,
		},
		{
			name: "budget exhausted",
			task: &Task{ID: "t", Attempts: 3, AttemptBudget: 3},
			c:    attemptConclusion{status: failedStatus("j", "", "no changes")},
			want: TaskConcludedBudgetExhausted,
		},
		{
			name: "abandoned with the budget spent",
			task: &Task{ID: "t", Attempts: 3, AttemptBudget: 3},
			c:    attemptConclusion{status: failedStatus("j", JobFailureClassAbandoned, "node gone")},
			want: TaskConcludedAbandoned,
		},
		{
			name: "abandoned with attempts left",
			task: &Task{ID: "t", Attempts: 1, AttemptBudget: 3},
			c:    attemptConclusion{status: failedStatus("j", JobFailureClassAbandoned, "node gone")},
			want: "",
		},
		{
			name: "three environment faults in a row",
			task: &Task{ID: "t", Attempts: 1, AttemptBudget: 9, ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1},
			c: attemptConclusion{
				status: failedStatus("j", FaultAccountsCoolingDown, "cooling"),
				fault:  EnvironmentFault{Class: FaultAccountsCoolingDown, Reason: "cooling"},
			},
			want: TaskConcludedFaultBounded,
		},
		{
			name: "an environment fault that does not requeue",
			task: &Task{ID: "t", Attempts: 1, AttemptBudget: 9},
			c: attemptConclusion{
				status: failedStatus("j", FaultNoProviderAccount, "no account"),
				fault:  EnvironmentFault{Class: FaultNoProviderAccount, Reason: "no account"},
			},
			want: TaskConcludedFaultHeld,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyConclusion(tc.task, tc.c, now)
			if got != tc.want {
				t.Fatalf("concluded %q, want %q (task now %+v)", got, tc.want, tc.task)
			}
			if got != "" && !got.Valid() {
				t.Fatalf("conclusion %q is outside the vocabulary", got)
			}
		})
	}
}

// TestBlockAndReadmit_AreAnnounced proves the two verbs an operator drives
// reach the observers too: a park is a conclusion and a readmit undoes one.
func TestBlockAndReadmit_AreAnnounced(t *testing.T) {
	observer := installTestConclusionObserver(t)
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusPending, Attempts: 1, AttemptBudget: 3})
	ctx := ContextWithTenant(context.Background(), "acme")

	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindNeedsInfo, "which convention wins?"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if err := svc.ReadmitTask(ctx, "acme", seeded.ID); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}

	seen := observer.conclusions()
	if len(seen) != 2 {
		t.Fatalf("expected a park and a readmission, got %+v", seen)
	}
	if seen[0].Reason != TaskConcludedParkedNeedsInfo || seen[0].Task.BlockedReason != "which convention wins?" {
		t.Fatalf("the park was announced wrong: %+v", seen[0])
	}
	if seen[1].Reason != TaskConcludedReadmitted || seen[1].Task.Status != TaskStatusPending {
		t.Fatalf("the readmission was announced wrong: %+v", seen[1])
	}
	if seen[0].JobID != "" || seen[1].JobID != "" {
		t.Fatal("a verb-driven conclusion names no job")
	}
}

// TestBlockTask_ARefusedParkAnnouncesNothing is the negative: a verb that
// failed has concluded nothing, so nobody is told.
func TestBlockTask_ARefusedParkAnnouncesNothing(t *testing.T) {
	observer := installTestConclusionObserver(t)
	svc, store := newConcludingService(t)
	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusPending})
	ctx := ContextWithTenant(context.Background(), "acme")

	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKind("wedged"), "because"); err == nil {
		t.Fatal("a block kind outside the enum must be refused")
	}
	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, ""); err == nil {
		t.Fatal("a park with no reason must be refused")
	}
	if seen := observer.conclusions(); len(seen) != 0 {
		t.Fatalf("a refused verb announces nothing, got %+v", seen)
	}
}
