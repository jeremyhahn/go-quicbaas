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

package service

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// CompactionState represents the current state of a compaction operation.
type CompactionState int32

const (
	// CompactionPlanning is the initial state when a compaction is being planned.
	CompactionPlanning CompactionState = iota
	// CompactionExecuting indicates the compaction is actively processing.
	CompactionExecuting
	// CompactionCompleted indicates the compaction finished successfully.
	CompactionCompleted
	// CompactionFailed indicates the compaction encountered an error.
	CompactionFailed
	// CompactionTimeout indicates the compaction exceeded its deadline.
	CompactionTimeout
	// CompactionCanceled indicates the compaction was explicitly canceled.
	CompactionCanceled
)

// compactionStateNames provides O(1) lookup from CompactionState to string.
var compactionStateNames = map[CompactionState]string{
	CompactionPlanning:  "planning",
	CompactionExecuting: "executing",
	CompactionCompleted: "completed",
	CompactionFailed:    "failed",
	CompactionTimeout:   "timeout",
	CompactionCanceled:  "canceled",
}

// String returns the string representation of the CompactionState.
func (s CompactionState) String() string {
	if name, ok := compactionStateNames[s]; ok {
		return name
	}
	return "unknown(" + strconv.Itoa(int(s)) + ")"
}

// validTransitions defines the allowed state transitions for compaction.
// Terminal states (completed, failed, timeout, canceled) have no valid transitions.
var validTransitions = map[CompactionState][]CompactionState{
	CompactionPlanning:  {CompactionExecuting, CompactionCanceled, CompactionFailed},
	CompactionExecuting: {CompactionCompleted, CompactionFailed, CompactionTimeout, CompactionCanceled},
	CompactionCompleted: {},
	CompactionFailed:    {},
	CompactionTimeout:   {},
	CompactionCanceled:  {},
}

// InvalidTransitionError indicates an invalid compaction state transition was attempted.
type InvalidTransitionError struct {
	From CompactionState
	To   CompactionState
}

// Error implements the error interface.
func (e *InvalidTransitionError) Error() string {
	return "invalid compaction state transition from " + e.From.String() + " to " + e.To.String()
}

// UpstreamCompactionError reports a compaction qrdb accepted and then failed
// in the background.
type UpstreamCompactionError struct {
	Namespace   string
	OperationID string
	Reason      string
}

func (e *UpstreamCompactionError) Error() string {
	return "compaction " + e.OperationID + " of namespace " + e.Namespace + " failed: " + e.Reason
}

// CompactionProgressError reports that qrdb refused or failed a progress
// read for an async compaction, for example when the principal lacks
// vector:get_compaction_progress. The job stays in progress: the
// compaction itself may still be running.
type CompactionProgressError struct {
	Namespace   string
	OperationID string
	Err         error
}

func (e *CompactionProgressError) Error() string {
	return "progress of compaction " + e.OperationID + " of namespace " + e.Namespace + " unavailable: " + e.Err.Error()
}

func (e *CompactionProgressError) Unwrap() error { return e.Err }

// CompactionPlan describes what a compaction will do.
type CompactionPlan struct {
	// Namespace is the target namespace for compaction.
	Namespace string
	// EstimatedBytes is the estimated total bytes to process.
	EstimatedBytes int64
	// SegmentsToCompact is the number of source segments.
	SegmentsToCompact int
	// TargetSegments is the desired number of output segments.
	TargetSegments int
}

// CompactionProgress tracks compaction execution progress.
type CompactionProgress struct {
	// BytesCompacted is the number of bytes processed so far.
	BytesCompacted int64
	// TotalBytes is the total bytes to process.
	TotalBytes int64
	// SegmentsRead is the number of source segments read.
	SegmentsRead int
	// SegmentsWritten is the number of output segments written.
	SegmentsWritten int
}

// CompactionTask represents a tracked compaction operation with state machine transitions.
type CompactionTask struct {
	id          string
	namespace   string
	state       atomic.Int32
	plan        *CompactionPlan
	startedAt   time.Time
	completedAt atomic.Pointer[time.Time]
	err         atomic.Pointer[error]
	cancelCh    chan struct{}
	cancelOnce  sync.Once

	// mu protects progress which has multiple fields updated together.
	mu       sync.RWMutex
	progress CompactionProgress
}

// NewCompactionTask creates a new CompactionTask in the Planning state.
func NewCompactionTask(id, namespace string) *CompactionTask {
	t := &CompactionTask{
		id:        id,
		namespace: namespace,
		startedAt: time.Now(),
		cancelCh:  make(chan struct{}),
	}
	t.state.Store(int32(CompactionPlanning))
	return t
}

// Transition attempts to move to the next state. Returns an error if the transition is invalid.
func (t *CompactionTask) Transition(newState CompactionState) error {
	for {
		current := CompactionState(t.state.Load())
		allowed, ok := validTransitions[current]
		if !ok {
			return &InvalidTransitionError{From: current, To: newState}
		}

		valid := false
		for _, s := range allowed {
			if s == newState {
				valid = true
				break
			}
		}
		if !valid {
			return &InvalidTransitionError{From: current, To: newState}
		}

		if t.state.CompareAndSwap(int32(current), int32(newState)) {
			// Record completion time for terminal states.
			if newState == CompactionCompleted || newState == CompactionFailed ||
				newState == CompactionTimeout || newState == CompactionCanceled {
				now := time.Now()
				t.completedAt.Store(&now)
			}
			return nil
		}
		// CAS failed, retry with updated current state.
	}
}

// State returns the current compaction state.
func (t *CompactionTask) State() CompactionState {
	return CompactionState(t.state.Load())
}

// ID returns the task identifier.
func (t *CompactionTask) ID() string {
	return t.id
}

// Namespace returns the task namespace.
func (t *CompactionTask) Namespace() string {
	return t.namespace
}

// SetPlan sets the compaction plan.
func (t *CompactionTask) SetPlan(plan *CompactionPlan) {
	t.mu.Lock()
	t.plan = plan
	t.mu.Unlock()
}

// Plan returns the compaction plan.
func (t *CompactionTask) Plan() *CompactionPlan {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.plan
}

// UpdateProgress updates the compaction progress atomically.
func (t *CompactionTask) UpdateProgress(bytesCompacted, totalBytes int64) {
	t.mu.Lock()
	t.progress.BytesCompacted = bytesCompacted
	t.progress.TotalBytes = totalBytes
	t.mu.Unlock()
}

// UpdateSegmentProgress updates segment read/write counts.
func (t *CompactionTask) UpdateSegmentProgress(segmentsRead, segmentsWritten int) {
	t.mu.Lock()
	t.progress.SegmentsRead = segmentsRead
	t.progress.SegmentsWritten = segmentsWritten
	t.mu.Unlock()
}

// Cancel signals the compaction to stop. Safe to call multiple times.
func (t *CompactionTask) Cancel() {
	t.cancelOnce.Do(func() {
		close(t.cancelCh)
	})
}

// IsCanceled returns whether cancellation was requested.
func (t *CompactionTask) IsCanceled() bool {
	select {
	case <-t.cancelCh:
		return true
	default:
		return false
	}
}

// CancelCh returns the cancellation channel for use in select statements.
func (t *CompactionTask) CancelCh() <-chan struct{} {
	return t.cancelCh
}

// GetProgress returns a snapshot of the current progress.
func (t *CompactionTask) GetProgress() CompactionProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.progress
}

// StartedAt returns the time the task was created.
func (t *CompactionTask) StartedAt() time.Time {
	return t.startedAt
}

// CompletedAt returns the completion time, or nil if still running.
func (t *CompactionTask) CompletedAt() *time.Time {
	return t.completedAt.Load()
}

// SetError stores an error on the task.
func (t *CompactionTask) SetError(err error) {
	t.err.Store(&err)
}

// GetError returns the stored error, or nil.
func (t *CompactionTask) GetError() error {
	ptr := t.err.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}
