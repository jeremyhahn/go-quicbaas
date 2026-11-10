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

import (
	"errors"
	"testing"
)

// =============================================================================
// ValidateMemberTransition - Valid Transitions
// =============================================================================

func TestValidateMemberTransition_AllValidPaths(t *testing.T) {
	tests := []struct {
		name string
		from MemberStatus
		to   MemberStatus
	}{
		{"ready->busy", MemberStatusReady, MemberStatusBusy},
		{"ready->shutdown_requested", MemberStatusReady, MemberStatusShutdownRequested},
		{"ready->error", MemberStatusReady, MemberStatusError},
		{"busy->ready", MemberStatusBusy, MemberStatusReady},
		{"busy->shutdown_requested", MemberStatusBusy, MemberStatusShutdownRequested},
		{"busy->error", MemberStatusBusy, MemberStatusError},
		{"shutdown_requested->shutdown", MemberStatusShutdownRequested, MemberStatusShutdown},
		{"shutdown_requested->error", MemberStatusShutdownRequested, MemberStatusError},
		{"error->ready", MemberStatusError, MemberStatusReady},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMemberTransition(tc.from, tc.to, TransitionOption{})
			if err != nil {
				t.Errorf("expected valid transition %s->%s, got: %v", tc.from, tc.to, err)
			}
		})
	}
}

// =============================================================================
// ValidateMemberTransition - Invalid Transitions, Guard and Force Options
// =============================================================================

func TestValidateMemberTransition_InvalidPairsGuardAndForce(t *testing.T) {
	tests := []struct {
		name    string
		from    MemberStatus
		to      MemberStatus
		opts    TransitionOption
		wantErr bool
	}{
		{"shutdown->ready is invalid", MemberStatusShutdown, MemberStatusReady, TransitionOption{}, true},
		{"ready->shutdown must go through shutdown_requested", MemberStatusReady, MemberStatusShutdown, TransitionOption{}, true},
		{"shutdown->busy is invalid", MemberStatusShutdown, MemberStatusBusy, TransitionOption{}, true},
		{"unknown from state is invalid", "unknown", MemberStatusReady, TransitionOption{}, true},
		{"guard allows same state", MemberStatusReady, MemberStatusReady, TransitionOption{Guard: true}, false},
		{"guard allows shutdown->shutdown", MemberStatusShutdown, MemberStatusShutdown, TransitionOption{Guard: true}, false},
		{"guard does not bypass an invalid pair", MemberStatusShutdown, MemberStatusBusy, TransitionOption{Guard: true}, true},
		{"force bypasses an invalid pair", MemberStatusShutdown, MemberStatusBusy, TransitionOption{Force: true}, false},
		{"force allows unknown states", "unknown", "also_unknown", TransitionOption{Force: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMemberTransition(tc.from, tc.to, tc.opts)
			if !tc.wantErr {
				if err != nil {
					t.Errorf("expected %s->%s to be allowed, got: %v", tc.from, tc.to, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidMemberTransition) {
				t.Fatalf("expected ErrInvalidMemberTransition, got: %v", err)
			}
			var mtErr *MemberTransitionError
			if !errors.As(err, &mtErr) {
				t.Fatalf("expected *MemberTransitionError, got %T", err)
			}
			if mtErr.From != tc.from || mtErr.To != tc.to {
				t.Errorf("expected %s->%s in error, got %s->%s", tc.from, tc.to, mtErr.From, mtErr.To)
			}
		})
	}
}

// =============================================================================
// ValidateExecTransition - Valid Transitions
// =============================================================================

func TestValidateExecTransition_AllValidPaths(t *testing.T) {
	tests := []struct {
		name string
		from ExecutionStatus
		to   ExecutionStatus
	}{
		{"idle->awaiting_input", ExecStatusIdle, ExecStatusAwaitingInput},
		{"idle->processing", ExecStatusIdle, ExecStatusProcessing},
		{"idle->error", ExecStatusIdle, ExecStatusError},
		{"awaiting_input->processing", ExecStatusAwaitingInput, ExecStatusProcessing},
		{"awaiting_input->cancelling", ExecStatusAwaitingInput, ExecStatusCancelling},
		{"awaiting_input->error", ExecStatusAwaitingInput, ExecStatusError},
		{"processing->calling_tool", ExecStatusProcessing, ExecStatusCallingTool},
		{"processing->generating", ExecStatusProcessing, ExecStatusGenerating},
		{"processing->paused", ExecStatusProcessing, ExecStatusPaused},
		{"processing->cancelling", ExecStatusProcessing, ExecStatusCancelling},
		{"processing->completed", ExecStatusProcessing, ExecStatusCompleted},
		{"processing->error", ExecStatusProcessing, ExecStatusError},
		{"calling_tool->processing", ExecStatusCallingTool, ExecStatusProcessing},
		{"calling_tool->cancelling", ExecStatusCallingTool, ExecStatusCancelling},
		{"calling_tool->error", ExecStatusCallingTool, ExecStatusError},
		{"generating->processing", ExecStatusGenerating, ExecStatusProcessing},
		{"generating->completed", ExecStatusGenerating, ExecStatusCompleted},
		{"generating->cancelling", ExecStatusGenerating, ExecStatusCancelling},
		{"generating->error", ExecStatusGenerating, ExecStatusError},
		{"paused->processing", ExecStatusPaused, ExecStatusProcessing},
		{"paused->cancelling", ExecStatusPaused, ExecStatusCancelling},
		{"paused->error", ExecStatusPaused, ExecStatusError},
		{"cancelling->cancelled", ExecStatusCancelling, ExecStatusCancelled},
		{"cancelling->error", ExecStatusCancelling, ExecStatusError},
		{"cancelled->idle", ExecStatusCancelled, ExecStatusIdle},
		{"completed->idle", ExecStatusCompleted, ExecStatusIdle},
		{"error->idle", ExecStatusError, ExecStatusIdle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExecTransition(tc.from, tc.to, TransitionOption{})
			if err != nil {
				t.Errorf("expected valid transition %s->%s, got: %v", tc.from, tc.to, err)
			}
		})
	}
}

// =============================================================================
// ValidateExecTransition - Invalid Transitions, Guard and Force Options
// =============================================================================

func TestValidateExecTransition_InvalidPairsGuardAndForce(t *testing.T) {
	tests := []struct {
		name    string
		from    ExecutionStatus
		to      ExecutionStatus
		opts    TransitionOption
		wantErr bool
	}{
		{"idle->completed is invalid", ExecStatusIdle, ExecStatusCompleted, TransitionOption{}, true},
		{"completed->processing is invalid", ExecStatusCompleted, ExecStatusProcessing, TransitionOption{}, true},
		{"cancelled->completed is invalid", ExecStatusCancelled, ExecStatusCompleted, TransitionOption{}, true},
		{"unknown from state is invalid", "unknown", ExecStatusIdle, TransitionOption{}, true},
		{"guard allows same state", ExecStatusProcessing, ExecStatusProcessing, TransitionOption{Guard: true}, false},
		{"guard allows idle->idle", ExecStatusIdle, ExecStatusIdle, TransitionOption{Guard: true}, false},
		{"guard does not bypass an invalid pair", ExecStatusIdle, ExecStatusCompleted, TransitionOption{Guard: true}, true},
		{"force bypasses an invalid pair", ExecStatusIdle, ExecStatusCompleted, TransitionOption{Force: true}, false},
		{"force allows unknown states", "unknown", "also_unknown", TransitionOption{Force: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExecTransition(tc.from, tc.to, tc.opts)
			if !tc.wantErr {
				if err != nil {
					t.Errorf("expected %s->%s to be allowed, got: %v", tc.from, tc.to, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidExecTransition) {
				t.Fatalf("expected ErrInvalidExecTransition, got: %v", err)
			}
			var etErr *ExecTransitionError
			if !errors.As(err, &etErr) {
				t.Fatalf("expected *ExecTransitionError, got %T", err)
			}
			if etErr.From != tc.from || etErr.To != tc.to {
				t.Errorf("expected %s->%s in error, got %s->%s", tc.from, tc.to, etErr.From, etErr.To)
			}
		})
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestMemberTransitionError_ErrorMessage(t *testing.T) {
	err := &MemberTransitionError{From: MemberStatusReady, To: MemberStatusShutdown}
	expected := "state: invalid member transition: ready -> shutdown"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestExecTransitionError_ErrorMessage(t *testing.T) {
	err := &ExecTransitionError{From: ExecStatusIdle, To: ExecStatusCompleted}
	expected := "state: invalid execution transition: idle -> completed"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
