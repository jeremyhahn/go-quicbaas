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
	"encoding/json"
	"testing"
	"time"
)

// concludedTask runs one attempt's conclusion through applyConclusion and
// persists the reason exactly as writeConclusion does, so every fixture below
// is a record the engine itself would have written.
func concludedTask(t *testing.T, task *Task, c attemptConclusion) *Task {
	t.Helper()
	task.ConclusionReason = applyConclusion(task, c, time.Now().UTC())
	return task
}

// representationCase is one terminal state and the accounting a reader must
// see for it.
type representationCase struct {
	name       string
	task       func(t *testing.T) *Task
	status     TaskStatus
	conclusion TaskConclusionReason
	class      string
	origin     TaskFaultOrigin
	charged    int
	budget     int
}

// representationCases covers every place a task comes to rest: completed,
// failed on the work, blocked on an environment fault (held and bounded),
// parked for an operator's repair, parked on an owner ruling, and abandoned by
// a node that stopped.
func representationCases() []representationCase {
	return []representationCase{
		{
			name: "completed",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 1},
					attemptConclusion{status: &JobStatus{JobID: "job-1", State: JobStateCompleted}})
			},
			status: TaskStatusCompleted, conclusion: TaskConcludedCompleted,
			origin: "", charged: 1, budget: DefaultAttemptBudget,
		},
		{
			name: "failed on the work",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 3},
					attemptConclusion{status: &JobStatus{
						JobID: "job-3", State: JobStateFailed, FailureClass: JobFailureClassStalled,
						Error: "no progress for 30m",
					}})
			},
			status: TaskStatusFailed, conclusion: TaskConcludedBudgetExhausted,
			class: JobFailureClassStalled, origin: TaskFaultOriginWork, charged: 3, budget: DefaultAttemptBudget,
		},
		{
			name: "held on an environment fault",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 2, AttemptBudget: 5},
					attemptConclusion{
						status: &JobStatus{JobID: "job-2", State: JobStateFailed, FailureClass: FaultNoProviderAccount},
						fault:  EnvironmentFault{Class: FaultNoProviderAccount, Reason: "no account for provider"},
					})
			},
			status: TaskStatusBlocked, conclusion: TaskConcludedFaultHeld,
			class: FaultNoProviderAccount, origin: TaskFaultOriginEnvironment, charged: 1, budget: 5,
		},
		{
			name: "bounded by consecutive environment faults",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{
					ID: "t", Status: TaskStatusInProgress, Attempts: 3, EnvironmentFaults: 2,
					ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1,
				}, attemptConclusion{
					status: &JobStatus{JobID: "job-3", State: JobStateFailed, FailureClass: FaultForgeUnreachable},
					fault:  EnvironmentFault{Class: FaultForgeUnreachable, Reason: "forge down"},
				})
			},
			status: TaskStatusBlocked, conclusion: TaskConcludedFaultBounded,
			class: FaultForgeUnreachable, origin: TaskFaultOriginEnvironment, charged: 1, budget: DefaultAttemptBudget,
		},
		{
			name: "parked for an operator's repair",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 1},
					attemptConclusion{
						status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "workspace missing"},
						park:   &BlockReason{Kind: TaskBlockKindBlocked, Message: "no checkout", FaultClass: FaultWorkspaceUnfit, Surfaced: true},
					})
			},
			status: TaskStatusBlocked, conclusion: TaskConcludedRunnerParked,
			class: FaultWorkspaceUnfit, origin: TaskFaultOriginEnvironment, charged: 0, budget: DefaultAttemptBudget,
		},
		{
			name: "parked on an owner ruling",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 1},
					attemptConclusion{
						status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "needs a ruling"},
						park:   &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "architecture call"},
					})
			},
			status: TaskStatusBlocked, conclusion: TaskConcludedParkedNeedsInfo,
			origin: "", charged: 0, budget: DefaultAttemptBudget,
		},
		{
			name: "abandoned by a node that stopped",
			task: func(t *testing.T) *Task {
				return concludedTask(t, &Task{ID: "t", Status: TaskStatusInProgress, Attempts: 3},
					attemptConclusion{status: &JobStatus{
						JobID: "job-3", State: JobStateFailed, FailureClass: JobFailureClassAbandoned,
						Error: "the node stopped heartbeating",
					}})
			},
			status: TaskStatusBlocked, conclusion: TaskConcludedAbandoned,
			origin: TaskFaultOriginUndiagnosed, charged: 3, budget: DefaultAttemptBudget,
		},
	}
}

// TestTaskRepresentation_EveryTerminalStateCarriesItsAccounting is the
// go-quicbaas#947 contract: each state a task rests in reaches a client with
// its conclusion, fault class, origin and budget, as keys that are present
// even when their value is zero.
func TestTaskRepresentation_EveryTerminalStateCarriesItsAccounting(t *testing.T) {
	t.Parallel()

	for _, tc := range representationCases() {
		t.Run(tc.name, func(t *testing.T) {
			task := tc.task(t)
			if task.Status != tc.status || task.ConclusionReason != tc.conclusion {
				t.Fatalf("fixture rests as %s/%q, want %s/%q; this test proves nothing",
					task.Status, task.ConclusionReason, tc.status, tc.conclusion)
			}

			data, err := json.Marshal(task)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			for _, key := range []string{
				"attempts", "charged_attempts", "effective_budget", "environment_faults",
				"question_attempts", "conclusion_reason", "fault_class", "fault_origin",
				"block_kind", "blocked_reason", "block_fault_class",
			} {
				if _, ok := wire[key]; !ok {
					t.Errorf("key %q is absent from the representation", key)
				}
			}
			if got := wire["conclusion_reason"]; got != string(tc.conclusion) {
				t.Errorf("conclusion_reason = %v, want %q", got, tc.conclusion)
			}
			if got := wire["fault_class"]; got != tc.class {
				t.Errorf("fault_class = %v, want %q", got, tc.class)
			}
			if got := wire["fault_origin"]; got != string(tc.origin) {
				t.Errorf("fault_origin = %v, want %q", got, tc.origin)
			}
			if got := wire["charged_attempts"]; got != float64(tc.charged) {
				t.Errorf("charged_attempts = %v, want %d", got, tc.charged)
			}
			if got := wire["effective_budget"]; got != float64(tc.budget) {
				t.Errorf("effective_budget = %v, want %d", got, tc.budget)
			}
		})
	}
}

// TestTaskRepresentation_ValueAndPointerEncodeAlike: a Task held by value in
// a slice or a struct must not lose the accounting a *Task carries.
func TestTaskRepresentation_ValueAndPointerEncodeAlike(t *testing.T) {
	t.Parallel()

	task := Task{ID: "t", Status: TaskStatusFailed, Attempts: 2, AttemptBudget: 2}
	byPointer, err := json.Marshal(&task)
	if err != nil {
		t.Fatalf("marshal pointer: %v", err)
	}
	byValue, err := json.Marshal([]Task{task})
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if string(byValue) != "["+string(byPointer)+"]" {
		t.Fatalf("value encoding %s differs from pointer encoding %s", byValue, byPointer)
	}
}

// TestMarshalTask_StoresNoDerivedAccounting: the stored record must not carry
// a copy of what is derived on read, or a record would disagree with itself
// the moment its counters move.
func TestMarshalTask_StoresNoDerivedAccounting(t *testing.T) {
	t.Parallel()

	task := &Task{
		ID: "t", Status: TaskStatusFailed, Attempts: 3,
		Result: &TaskResult{Metadata: map[string]string{taskResultFailureClassKey: JobFailureClassStalled}},
	}
	data, err := marshalTask(task)
	if err != nil {
		t.Fatalf("marshalTask: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"charged_attempts", "effective_budget", "fault_class", "fault_origin"} {
		if _, ok := stored[key]; ok {
			t.Errorf("the stored record carries derived key %q", key)
		}
	}

	// And the representation round-trips back into the same record: the
	// derived keys are read-only and a decoder ignores them.
	wire, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Task
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("decode representation: %v", err)
	}
	if decoded.Attempts != 3 || decoded.FaultClass() != JobFailureClassStalled {
		t.Fatalf("decoded record = %+v, want the original", decoded)
	}
}

// TestTaskFaultOrigin_UnknownClassIsNeverTheEnvironment pins the fail-closed
// direction OperatorFaultClass takes: a class nothing produces is the work's
// on a failed task and no fault at all on a blocked one.
func TestTaskFaultOrigin_UnknownClassIsNeverTheEnvironment(t *testing.T) {
	t.Parallel()

	failed := &Task{Status: TaskStatusFailed, Result: &TaskResult{Metadata: map[string]string{taskResultFailureClassKey: "made_up"}}}
	if got := failed.FaultOrigin(); got != TaskFaultOriginWork {
		t.Errorf("failed with an unknown class: origin = %q, want work", got)
	}
	blocked := &Task{Status: TaskStatusBlocked, BlockKind: TaskBlockKindBlocked, BlockFaultClass: "made_up"}
	if got := blocked.FaultOrigin(); got != "" {
		t.Errorf("blocked with an unknown class: origin = %q, want none", got)
	}
}

// TestTaskFaultClass_BlockedReadsOnlyItsBlock: an operator's hold placed on a
// task whose previous attempt was requeued after a fault is not about that
// fault, and must not be reported as though it were.
func TestTaskFaultClass_BlockedReadsOnlyItsBlock(t *testing.T) {
	t.Parallel()

	task := &Task{
		Status: TaskStatusBlocked, BlockKind: TaskBlockKindBlocked, BlockedReason: "operator hold",
		Result: &TaskResult{Metadata: map[string]string{taskResultFailureClassKey: FaultForgeUnreachable}},
	}
	if got := task.FaultClass(); got != "" {
		t.Errorf("fault class = %q, want none: the hold carries no class", got)
	}
	if got := task.FaultOrigin(); got != "" {
		t.Errorf("fault origin = %q, want none", got)
	}
}

// TestTaskFaultAccounting_NilAndOpenTasks: nothing to report is reported as
// nothing, never a panic.
func TestTaskFaultAccounting_NilAndOpenTasks(t *testing.T) {
	t.Parallel()

	var nilTask *Task
	if nilTask.FaultClass() != "" || nilTask.FaultOrigin() != "" {
		t.Error("a nil task reports a fault")
	}
	pending := &Task{
		Status: TaskStatusPending,
		Result: &TaskResult{Metadata: map[string]string{taskResultFailureClassKey: FaultForgeUnreachable}},
	}
	if got := pending.FaultClass(); got != FaultForgeUnreachable {
		t.Errorf("a requeued task's fault class = %q, want the last attempt's", got)
	}
	if got := pending.FaultOrigin(); got != "" {
		t.Errorf("an open task's origin = %q, want none: it has not stopped", got)
	}
	if got := (&Task{Status: TaskStatusCompleted}).FaultClass(); got != "" {
		t.Errorf("a completed task with no result reports class %q", got)
	}
}
