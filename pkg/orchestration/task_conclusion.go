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
	"sync"
)

// =============================================================================
// Task conclusions — the moment a task stops, named once
// =============================================================================
//
// A JOB HAS A BLOCKED HOOK; A TASK HAD NOTHING.
//
// The Blocked lifecycle moment is the RUN's: it fires inside the control loop,
// on a live JobRun, and it says why THIS ATTEMPT gave up. The task's own
// conclusion is decided somewhere else entirely — in applyConclusion, after the
// run has returned, and on the reaper's goroutine when the node that held the
// run is gone. A task that spends its last attempt therefore stops without any
// moment firing at all, which is exactly what go-quicbaas#788 measured: task
// 46dac86c concluded `failed / budget_exhausted` and its forge issue kept the
// in-progress label, with no comment, for hours.
//
// So the conclusion is a seam of its own. It is NOT a sixth lifecycle hook: a
// hook belongs to a job, needs a JobContext, and cannot be called for a task
// whose job died with its daemon. This is a task-level observation, handed the
// concluded record, and it fires from the two places a task can stop —
// concludeAttempt (the loop and the reaper) and the block/readmit verbs.
//
// THE VOCABULARY IS CLOSED AND EXHAUSTIVE. Every reason a task stops is named
// here, and a module that surfaces conclusions maps the whole set: pkg/sdlc's
// table is asserted against TaskConclusionReasons(), so a reason added here
// without a rendering fails that test rather than going silently unposted —
// which is the failure this file exists to remove.

// TaskConclusionReason names why a task stopped, in a closed vocabulary.
type TaskConclusionReason string

const (
	// TaskConcludedCompleted is the work accepted: the attempt ended in a
	// state other than failed.
	TaskConcludedCompleted TaskConclusionReason = "completed"

	// TaskConcludedBudgetExhausted is the attempt budget spent. The task is
	// failed, and failed is terminal — a readmit is refused.
	TaskConcludedBudgetExhausted TaskConclusionReason = "budget_exhausted"

	// TaskConcludedAbandoned is a job reaped because its node stopped, with
	// no attempts left to try again. The task is blocked, not failed: nothing
	// diagnosed the condition, so a person may readmit it.
	TaskConcludedAbandoned TaskConclusionReason = "abandoned"

	// TaskConcludedFaultBounded is MaxConsecutiveEnvironmentFaults reached:
	// the free retries stopped being free at the moment they stopped being
	// tried.
	TaskConcludedFaultBounded TaskConclusionReason = "fault_bounded"

	// TaskConcludedFaultHeld is a classified environment fault that does not
	// requeue — a condition an operator must repair.
	TaskConcludedFaultHeld TaskConclusionReason = "fault_held"

	// TaskConcludedParkedNeedsInfo is an explicit BlockTask with kind
	// needs_info: the run stopped on something only a person can settle.
	TaskConcludedParkedNeedsInfo TaskConclusionReason = "parked_needs_info"

	// TaskConcludedParkedBlocked is an explicit BlockTask with kind blocked.
	TaskConcludedParkedBlocked TaskConclusionReason = "parked_blocked"

	// TaskConcludedParkedEscalated is an explicit BlockTask with kind
	// escalated.
	TaskConcludedParkedEscalated TaskConclusionReason = "parked_escalated"

	// TaskConcludedRunnerParked is a park the RUNNER decided from the work
	// itself — a worker that stopped on a question, on a rule it may not
	// break, or on an environment it could not build in (go-quicbaas#787).
	//
	// SEPARATE FROM THE parked_* REASONS BECAUSE THE FORGE IS ALREADY TOLD.
	// Those three are the operator's explicit BlockTask, which writes nothing
	// to the issue itself, so the conclusion seam is the only thing that can.
	// A runner park reached the issue before the job returned: the run's own
	// Blocked moment posted it, with the worker's own text, under the very
	// marker the conclusion seam would use. One outcome, two routes, and the
	// route decides whether anything is left to say — so this reason exists
	// to say "nothing", and a module renders it silent.
	//
	// The task's own BlockKind still records WHICH park it was; this names
	// who has already announced it.
	TaskConcludedRunnerParked TaskConclusionReason = "runner_parked"

	// TaskConcludedReadmitted is a parked task returned to the queue. It is
	// not a stop but the undoing of one, and it is in this vocabulary because
	// it is the write that clears what a stop left behind.
	TaskConcludedReadmitted TaskConclusionReason = "readmitted"

	// TaskConcludedAlreadySatisfied is a task whose work was already done
	// before the worker started: the issue is stale, or an earlier attempt of
	// this very task landed it.
	//
	// IT IS A CONCLUSION, NOT A PARK OR A FAILURE, and that distinction is the
	// whole reason it exists. A worker that reads the tree, finds the change
	// unnecessary and says so with evidence has done its job exactly as the
	// charter demands — but the vocabulary had no word for that outcome, so it
	// was recorded as `parked_escalated` ("the owner must rule") or failed as
	// `produced no changes`. Measured on the burn-in node 2026-09-13: six
	// escalations awaiting an owner ruling, of which four were one stale README
	// issue the workers had each correctly reported as already accurate.
	//
	// Conflating the two costs three ways: an attempt is spent on a task that
	// had nothing to spend it on, the operator queue fills with items needing
	// no decision — which is how a real escalation gets lost — and the stale
	// issue is never reported as stale, so it is adopted again.
	TaskConcludedAlreadySatisfied TaskConclusionReason = "already_satisfied"
)

// taskConclusionReasons is the closed set, as a membership test.
var taskConclusionReasons = map[TaskConclusionReason]struct{}{
	TaskConcludedCompleted:        {},
	TaskConcludedBudgetExhausted:  {},
	TaskConcludedAbandoned:        {},
	TaskConcludedFaultBounded:     {},
	TaskConcludedFaultHeld:        {},
	TaskConcludedParkedNeedsInfo:  {},
	TaskConcludedParkedBlocked:    {},
	TaskConcludedParkedEscalated:  {},
	TaskConcludedRunnerParked:     {},
	TaskConcludedReadmitted:       {},
	TaskConcludedAlreadySatisfied: {},
}

// Valid reports whether the reason is a member of the closed vocabulary.
func (r TaskConclusionReason) Valid() bool {
	_, ok := taskConclusionReasons[r]
	return ok
}

// String returns the reason's wire name.
func (r TaskConclusionReason) String() string { return string(r) }

// TaskConclusionReasons lists the whole vocabulary, sorted so the order is
// stable.
//
// EXPORTED SO A SURFACING MODULE COVERS THE SET RATHER THAN A COPY OF IT.
// pkg/sdlc renders one comment per reason and asserts its table against this
// list; a reason added here and nowhere else fails that test.
func TaskConclusionReasons() []TaskConclusionReason {
	out := make([]TaskConclusionReason, 0, len(taskConclusionReasons))
	for reason := range taskConclusionReasons {
		out = append(out, reason)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// blockKindConclusions maps an explicit park's kind to its conclusion reason,
// so the block verb names the outcome from the same closed vocabulary as the
// attempt accounting rather than from a second one.
var blockKindConclusions = map[TaskBlockKind]TaskConclusionReason{
	TaskBlockKindNeedsInfo: TaskConcludedParkedNeedsInfo,
	TaskBlockKindBlocked:   TaskConcludedParkedBlocked,
	TaskBlockKindEscalated: TaskConcludedParkedEscalated,
}

// ConclusionOfBlockKind returns the conclusion reason an explicit park of this
// kind concludes with, and whether the kind is one the enum names.
func ConclusionOfBlockKind(kind TaskBlockKind) (TaskConclusionReason, bool) {
	reason, ok := blockKindConclusions[kind]
	return reason, ok
}

// TaskConclusion is one task's conclusion, as an observer receives it.
type TaskConclusion struct {
	// TenantID owns the task.
	TenantID string

	// Reason names the conclusion. Always a member of the closed vocabulary.
	Reason TaskConclusionReason

	// Task is the record AS CONCLUDED — the status, the block, the result and
	// the attempt accounting an observer renders are all read off it.
	Task *Task

	// JobID is the job whose outcome concluded the task, empty for a
	// conclusion no job produced (an explicit park, a readmit).
	JobID string
}

// Validate refuses a conclusion an observer could not act on.
func (c TaskConclusion) Validate() error {
	switch {
	case c.TenantID == "":
		return &TaskConclusionInvalidError{Field: "tenant_id", Message: "a conclusion names the tenant that owns the task"}
	case c.Task == nil:
		return &TaskConclusionInvalidError{Field: "task", Message: "a conclusion carries the concluded record; an observer has nothing to read without it"}
	case c.Task.ID == "":
		return &TaskConclusionInvalidError{Field: "task.id", Message: "the concluded record must name its task"}
	case !c.Reason.Valid():
		return &TaskConclusionInvalidError{Field: "reason", Message: "reason " + string(c.Reason) + " is outside the closed vocabulary"}
	}
	return nil
}

// TaskConclusionObserver is told, once, that a task stopped.
//
// AN OBSERVER MAY NOT CHANGE THE OUTCOME. The task is already written and
// durable by the time this is called; an error here is a report that did not
// reach whoever was watching, never a reason to re-decide the conclusion.
type TaskConclusionObserver interface {
	TaskConcluded(ctx context.Context, conclusion TaskConclusion) error
}

// TaskConclusionRegistry holds observers by name. Registration is
// append-only, matching the engine's other seam registries.
type TaskConclusionRegistry struct {
	mu     sync.RWMutex
	byName map[string]TaskConclusionObserver
}

// NewTaskConclusionRegistry creates an empty registry.
func NewTaskConclusionRegistry() *TaskConclusionRegistry {
	return &TaskConclusionRegistry{byName: make(map[string]TaskConclusionObserver)}
}

// Register adds an observer under name.
func (r *TaskConclusionRegistry) Register(name string, observer TaskConclusionObserver) error {
	if name == "" {
		return &TaskConclusionRegistrationError{Message: "name must not be empty"}
	}
	if observer == nil {
		return &TaskConclusionRegistrationError{Name: name, Message: "observer must not be nil"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return &TaskConclusionAlreadyRegisteredError{Name: name}
	}
	r.byName[name] = observer
	return nil
}

// names lists the registered observers in a stable order.
func (r *TaskConclusionRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Notify hands the conclusion to every registered observer, in name order.
//
// EVERY OBSERVER IS CALLED even when an earlier one failed: they are
// independent reports, and one forge being unreachable must not silence the
// rest. The failures are joined so a caller can log them all.
func (r *TaskConclusionRegistry) Notify(ctx context.Context, conclusion TaskConclusion) error {
	if err := conclusion.Validate(); err != nil {
		return err
	}
	var failures []error
	for _, name := range r.names() {
		r.mu.RLock()
		observer := r.byName[name]
		r.mu.RUnlock()
		if observer == nil {
			continue
		}
		if err := observer.TaskConcluded(ctx, conclusion); err != nil {
			failures = append(failures, &TaskConclusionNotSurfacedError{
				Observer: name, TaskID: conclusion.Task.ID, Reason: conclusion.Reason, Cause: err,
			})
		}
	}
	return errors.Join(failures...)
}

// taskConclusionObservers is the engine's registry: the one a module
// registers into and the one every conclusion is announced on.
var taskConclusionObservers = NewTaskConclusionRegistry()

// RegisterTaskConclusionObserver registers an observer in the engine's
// registry under name.
func RegisterTaskConclusionObserver(name string, observer TaskConclusionObserver) error {
	return taskConclusionObservers.Register(name, observer)
}

// NotifyTaskConcluded announces a conclusion to every registered observer.
func NotifyTaskConcluded(ctx context.Context, conclusion TaskConclusion) error {
	return taskConclusionObservers.Notify(ctx, conclusion)
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrTaskConclusionInvalid indicates a conclusion an observer could not
	// act on.
	ErrTaskConclusionInvalid = errors.New("task conclusion: invalid")

	// ErrTaskConclusionRegistration indicates an observer the registry
	// refused.
	ErrTaskConclusionRegistration = errors.New("task conclusion: registration refused")

	// ErrTaskConclusionAlreadyRegistered indicates an observer name that is
	// taken.
	ErrTaskConclusionAlreadyRegistered = errors.New("task conclusion: already registered")

	// ErrTaskConclusionNotSurfaced indicates an observer that did not report
	// the conclusion.
	ErrTaskConclusionNotSurfaced = errors.New("task conclusion: not surfaced")
)

// TaskConclusionInvalidError reports a conclusion that names no task, no
// tenant, or a reason outside the closed vocabulary.
type TaskConclusionInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TaskConclusionInvalidError) Error() string {
	return ErrTaskConclusionInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskConclusionInvalidError) Unwrap() error { return ErrTaskConclusionInvalid }

// TaskConclusionRegistrationError reports a registration the registry refused
// for a reason other than a duplicate name.
type TaskConclusionRegistrationError struct {
	Name    string
	Message string
}

// Error implements the error interface.
func (e *TaskConclusionRegistrationError) Error() string {
	if e.Name == "" {
		return ErrTaskConclusionRegistration.Error() + ": " + e.Message
	}
	return ErrTaskConclusionRegistration.Error() + ": " + e.Name + ": " + e.Message
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskConclusionRegistrationError) Unwrap() error { return ErrTaskConclusionRegistration }

// TaskConclusionAlreadyRegisteredError reports a second registration under a
// name that is already taken.
type TaskConclusionAlreadyRegisteredError struct {
	Name string
}

// Error implements the error interface.
func (e *TaskConclusionAlreadyRegisteredError) Error() string {
	return ErrTaskConclusionAlreadyRegistered.Error() + ": " + e.Name
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskConclusionAlreadyRegisteredError) Unwrap() error {
	return ErrTaskConclusionAlreadyRegistered
}

// TaskConclusionNotSurfacedError reports an observer that failed to report a
// conclusion. The task's own record is unaffected.
type TaskConclusionNotSurfacedError struct {
	Observer string
	TaskID   string
	Reason   TaskConclusionReason
	Cause    error
}

// Error implements the error interface.
func (e *TaskConclusionNotSurfacedError) Error() string {
	msg := ErrTaskConclusionNotSurfaced.Error() + ": observer=" + e.Observer +
		" task=" + e.TaskID + " reason=" + string(e.Reason)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *TaskConclusionNotSurfacedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrTaskConclusionNotSurfaced}
	}
	return []error{ErrTaskConclusionNotSurfaced, e.Cause}
}
