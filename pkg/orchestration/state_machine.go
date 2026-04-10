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

package orchestration

import "errors"

// =============================================================================
// Member Status (Fleet-Level State)
// =============================================================================

// MemberStatus represents the fleet-level lifecycle state of an agent.
type MemberStatus string

const (
	// MemberStatusReady indicates the agent is registered and available for work.
	MemberStatusReady MemberStatus = "ready"

	// MemberStatusBusy indicates the agent is currently executing a task.
	MemberStatusBusy MemberStatus = "busy"

	// MemberStatusShutdownRequested indicates a graceful shutdown has been requested.
	MemberStatusShutdownRequested MemberStatus = "shutdown_requested"

	// MemberStatusShutdown indicates the agent has completed shutdown.
	MemberStatusShutdown MemberStatus = "shutdown"

	// MemberStatusError indicates the agent is in an error state.
	MemberStatusError MemberStatus = "error"
)

// allowedMemberTransitions defines the valid state transitions for MemberStatus.
// Key: current state, Value: set of valid next states for O(1) lookup.
var allowedMemberTransitions = map[MemberStatus]map[MemberStatus]struct{}{
	MemberStatusReady: {
		MemberStatusBusy:              {},
		MemberStatusShutdownRequested: {},
		MemberStatusError:             {},
	},
	MemberStatusBusy: {
		MemberStatusReady:             {},
		MemberStatusShutdownRequested: {},
		MemberStatusError:             {},
	},
	MemberStatusShutdownRequested: {
		MemberStatusShutdown: {},
		MemberStatusError:    {},
	},
	MemberStatusShutdown: {},
	MemberStatusError: {
		MemberStatusReady: {},
	},
}

// =============================================================================
// Execution Status (Task-Level State)
// =============================================================================

// ExecutionStatus represents the task-level execution state of an agent.
type ExecutionStatus string

const (
	// ExecStatusIdle indicates the agent has no active task.
	ExecStatusIdle ExecutionStatus = "idle"

	// ExecStatusAwaitingInput indicates the agent is waiting for input.
	ExecStatusAwaitingInput ExecutionStatus = "awaiting_input"

	// ExecStatusProcessing indicates the agent is processing a task.
	ExecStatusProcessing ExecutionStatus = "processing"

	// ExecStatusCallingTool indicates the agent is invoking a tool.
	ExecStatusCallingTool ExecutionStatus = "calling_tool"

	// ExecStatusGenerating indicates the agent is generating output.
	ExecStatusGenerating ExecutionStatus = "generating"

	// ExecStatusPaused indicates the agent execution is paused.
	ExecStatusPaused ExecutionStatus = "paused"

	// ExecStatusCancelling indicates the agent is cancelling execution.
	ExecStatusCancelling ExecutionStatus = "cancelling"

	// ExecStatusCancelled indicates the agent execution was cancelled.
	ExecStatusCancelled ExecutionStatus = "cancelled"

	// ExecStatusCompleted indicates the agent execution completed successfully.
	ExecStatusCompleted ExecutionStatus = "completed"

	// ExecStatusError indicates the agent execution encountered an error.
	ExecStatusError ExecutionStatus = "error"
)

// allowedExecTransitions defines the valid state transitions for ExecutionStatus.
// Key: current state, Value: set of valid next states for O(1) lookup.
var allowedExecTransitions = map[ExecutionStatus]map[ExecutionStatus]struct{}{
	ExecStatusIdle: {
		ExecStatusAwaitingInput: {},
		ExecStatusProcessing:    {},
		ExecStatusError:         {},
	},
	ExecStatusAwaitingInput: {
		ExecStatusProcessing: {},
		ExecStatusCancelling: {},
		ExecStatusError:      {},
	},
	ExecStatusProcessing: {
		ExecStatusCallingTool: {},
		ExecStatusGenerating:  {},
		ExecStatusPaused:      {},
		ExecStatusCancelling:  {},
		ExecStatusCompleted:   {},
		ExecStatusError:       {},
	},
	ExecStatusCallingTool: {
		ExecStatusProcessing: {},
		ExecStatusCancelling: {},
		ExecStatusError:      {},
	},
	ExecStatusGenerating: {
		ExecStatusProcessing: {},
		ExecStatusCompleted:  {},
		ExecStatusCancelling: {},
		ExecStatusError:      {},
	},
	ExecStatusPaused: {
		ExecStatusProcessing: {},
		ExecStatusCancelling: {},
		ExecStatusError:      {},
	},
	ExecStatusCancelling: {
		ExecStatusCancelled: {},
		ExecStatusError:     {},
	},
	ExecStatusCancelled: {
		ExecStatusIdle: {},
	},
	ExecStatusCompleted: {
		ExecStatusIdle: {},
	},
	ExecStatusError: {
		ExecStatusIdle: {},
	},
}

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrInvalidMemberTransition indicates an invalid MemberStatus state transition.
	ErrInvalidMemberTransition = errors.New("state: invalid member transition")

	// ErrInvalidExecTransition indicates an invalid ExecutionStatus state transition.
	ErrInvalidExecTransition = errors.New("state: invalid execution transition")
)

// =============================================================================
// Typed Errors
// =============================================================================

// MemberTransitionError indicates an invalid MemberStatus state transition.
type MemberTransitionError struct {
	From MemberStatus
	To   MemberStatus
}

// Error implements the error interface.
func (e *MemberTransitionError) Error() string {
	return ErrInvalidMemberTransition.Error() + ": " + string(e.From) + " -> " + string(e.To)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MemberTransitionError) Unwrap() error {
	return ErrInvalidMemberTransition
}

// ExecTransitionError indicates an invalid ExecutionStatus state transition.
type ExecTransitionError struct {
	From ExecutionStatus
	To   ExecutionStatus
}

// Error implements the error interface.
func (e *ExecTransitionError) Error() string {
	return ErrInvalidExecTransition.Error() + ": " + string(e.From) + " -> " + string(e.To)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ExecTransitionError) Unwrap() error {
	return ErrInvalidExecTransition
}

// =============================================================================
// Transition Options
// =============================================================================

// TransitionOption controls transition validation behavior.
type TransitionOption struct {
	// Guard skips the transition if already in the target state (idempotent).
	Guard bool

	// Force bypasses validation entirely (post-crash recovery only).
	Force bool
}

// =============================================================================
// Transition Validators
// =============================================================================

// ValidateMemberTransition checks if a MemberStatus transition is valid.
// When Guard is true, transitioning to the current state returns nil.
// When Force is true, all transitions are allowed.
func ValidateMemberTransition(from, to MemberStatus, opts TransitionOption) error {
	if opts.Force {
		return nil
	}
	if opts.Guard && from == to {
		return nil
	}
	targets, ok := allowedMemberTransitions[from]
	if !ok {
		return &MemberTransitionError{From: from, To: to}
	}
	if _, ok := targets[to]; !ok {
		return &MemberTransitionError{From: from, To: to}
	}
	return nil
}

// ValidateExecTransition checks if an ExecutionStatus transition is valid.
// When Guard is true, transitioning to the current state returns nil.
// When Force is true, all transitions are allowed.
func ValidateExecTransition(from, to ExecutionStatus, opts TransitionOption) error {
	if opts.Force {
		return nil
	}
	if opts.Guard && from == to {
		return nil
	}
	targets, ok := allowedExecTransitions[from]
	if !ok {
		return &ExecTransitionError{From: from, To: to}
	}
	if _, ok := targets[to]; !ok {
		return &ExecTransitionError{From: from, To: to}
	}
	return nil
}
