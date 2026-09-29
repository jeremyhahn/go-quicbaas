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

import "encoding/json"

// =============================================================================
// The task's representation — the accounting a reader cannot re-derive
// =============================================================================
//
// THE ENGINE KNOWS WHY A TASK STOPPED; A CLIENT SAW `failed, attempts: 3`.
//
// The fault classes, the refunds and the budget are all decided here, and the
// tables that decide them (operatorFaultClasses, resolverFaultClasses, the
// agent's environmental terminal reasons) live in this package. A client that
// had to rebuild "was this the fleet or the work" from Result.Output would be
// classifying prose — the thing this package refuses to do — and a client that
// copied the tables would drift from them the first time a class is added.
// go-quicbaas#947 measured the cost: nine infrastructure failures and two real
// ones read identically, and go-quicbaas#945 went unnoticed for five days.
//
// So the answers are DERIVED ON READ AND PUT ON THE WIRE, never persisted:
// ChargedAttempts and EffectiveAttemptBudget are arithmetic over the record,
// and the fault class and origin are read from what the conclusion wrote.
// marshalTask strips them from the stored bytes, so no record can carry a
// stale copy.

// TaskFaultOrigin says whose failure stopped a task. It is empty for a task
// that did not stop on a failure: one still open, one completed, one parked on
// a question or held by an operator.
type TaskFaultOrigin string

const (
	// TaskFaultOriginEnvironment is a task stopped by a fault class this
	// engine attributes to the environment — the operator's deployment, an
	// account pool, a forge — rather than to the work.
	TaskFaultOriginEnvironment TaskFaultOrigin = "environment"

	// TaskFaultOriginWork is a task that failed on its own merits: its
	// charged attempts were spent on failures no fault class explained.
	TaskFaultOriginWork TaskFaultOrigin = "work"

	// TaskFaultOriginUndiagnosed is a task stopped by an attempt whose node
	// stopped heartbeating. Nothing diagnosed the condition — it may have
	// been a deploy that took the container away, or a turn that killed its
	// own process — so it is named as neither, and applyDeath parks it for a
	// person to readmit rather than failing it.
	TaskFaultOriginUndiagnosed TaskFaultOrigin = "undiagnosed"
)

// String returns the origin's name.
func (o TaskFaultOrigin) String() string { return string(o) }

// taskResultFailureClassKey is the Result.Metadata key applyConclusion records
// the concluded job's failure class under.
const taskResultFailureClassKey = "failure_class"

// FaultClass names the class of the fault behind the task's latest concluded
// attempt: a fault class (see KnownFaultClass) or one of the JobFailureClass
// values. Empty when that attempt failed for no classified reason, or did not
// fail.
//
// A BLOCKED TASK ANSWERS WITH ITS BLOCK'S CLASS ONLY. The block is what the
// task is waiting on, and a block with no class — an operator's hold, a
// question — is waiting on nothing the environment did; reading a requeued
// attempt's older class through it would name a fault the block is not about.
func (t *Task) FaultClass() string {
	if t == nil {
		return ""
	}
	if t.Status == TaskStatusBlocked {
		return t.BlockFaultClass
	}
	if t.Result == nil {
		return ""
	}
	return t.Result.Metadata[taskResultFailureClassKey]
}

// FaultOrigin says whose failure stopped the task. Only a failed or blocked
// task has one; see TaskFaultOrigin.
//
// A CLASS THIS ENGINE DOES NOT KNOW IS NOT READ AS THE ENVIRONMENT, for the
// reason OperatorFaultClass gives: the answer that cannot hide a broken
// change is the one an unknown class takes. On a failed task it reads as the
// work; on a blocked task it reads as no fault at all.
func (t *Task) FaultOrigin() TaskFaultOrigin {
	if t == nil {
		return ""
	}
	switch t.Status {
	case TaskStatusFailed:
		switch class := t.FaultClass(); {
		case KnownFaultClass(class):
			return TaskFaultOriginEnvironment
		case class == JobFailureClassAbandoned:
			return TaskFaultOriginUndiagnosed
		default:
			return TaskFaultOriginWork
		}
	case TaskStatusBlocked:
		switch {
		case KnownFaultClass(t.BlockFaultClass):
			return TaskFaultOriginEnvironment
		case t.ConclusionReason == TaskConcludedAbandoned:
			return TaskFaultOriginUndiagnosed
		default:
			return ""
		}
	default:
		return ""
	}
}

// taskRecord is the Task's stored shape: the same fields with none of the
// methods, so encoding it writes the record and nothing derived from it.
type taskRecord Task

// taskRepresentation is the Task as every transport renders it: the record,
// plus the accounting derived from it.
type taskRepresentation struct {
	taskRecord

	// ChargedAttempts is Task.ChargedAttempts: Attempts less the refunded
	// ones. Always present — zero is an answer.
	ChargedAttempts int `json:"charged_attempts"`

	// EffectiveBudget is Task.EffectiveAttemptBudget: the task's own budget,
	// or the engine default it runs under when it declares none.
	EffectiveBudget int `json:"effective_budget"`

	// FaultClass is Task.FaultClass.
	FaultClass string `json:"fault_class"`

	// FaultOrigin is Task.FaultOrigin.
	FaultOrigin TaskFaultOrigin `json:"fault_origin"`
}

// MarshalJSON renders the task with its derived accounting beside the record.
//
// A VALUE RECEIVER, so a Task and a *Task encode alike. The stored bytes do
// not come through here: marshalTask encodes the taskRecord.
func (t Task) MarshalJSON() ([]byte, error) {
	return json.Marshal(taskRepresentation{
		taskRecord:      taskRecord(t),
		ChargedAttempts: t.ChargedAttempts(),
		EffectiveBudget: t.EffectiveAttemptBudget(),
		FaultClass:      t.FaultClass(),
		FaultOrigin:     t.FaultOrigin(),
	})
}
