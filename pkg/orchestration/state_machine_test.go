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

func TestValidateMemberTransition_ReadyToBusy(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusReady, MemberStatusBusy, TransitionOption{})
	if err != nil {
		t.Errorf("expected ready->busy to be valid, got: %v", err)
	}
}

func TestValidateMemberTransition_BusyToReady(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusBusy, MemberStatusReady, TransitionOption{})
	if err != nil {
		t.Errorf("expected busy->ready to be valid, got: %v", err)
	}
}

func TestValidateMemberTransition_ReadyToShutdownRequested(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusReady, MemberStatusShutdownRequested, TransitionOption{})
	if err != nil {
		t.Errorf("expected ready->shutdown_requested to be valid, got: %v", err)
	}
}

func TestValidateMemberTransition_ShutdownRequestedToShutdown(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusShutdownRequested, MemberStatusShutdown, TransitionOption{})
	if err != nil {
		t.Errorf("expected shutdown_requested->shutdown to be valid, got: %v", err)
	}
}

func TestValidateMemberTransition_ErrorToReady(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusError, MemberStatusReady, TransitionOption{})
	if err != nil {
		t.Errorf("expected error->ready to be valid, got: %v", err)
	}
}

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
// ValidateMemberTransition - Invalid Transitions
// =============================================================================

func TestValidateMemberTransition_ShutdownToReady(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusShutdown, MemberStatusReady, TransitionOption{})
	if err == nil {
		t.Fatal("expected shutdown->ready to be invalid")
	}
	if !errors.Is(err, ErrInvalidMemberTransition) {
		t.Errorf("expected ErrInvalidMemberTransition, got: %v", err)
	}
	var mtErr *MemberTransitionError
	if !errors.As(err, &mtErr) {
		t.Fatalf("expected *MemberTransitionError, got %T", err)
	}
	if mtErr.From != MemberStatusShutdown {
		t.Errorf("expected From=shutdown, got %s", mtErr.From)
	}
	if mtErr.To != MemberStatusReady {
		t.Errorf("expected To=ready, got %s", mtErr.To)
	}
}

func TestValidateMemberTransition_ReadyToShutdown(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusReady, MemberStatusShutdown, TransitionOption{})
	if err == nil {
		t.Fatal("expected ready->shutdown to be invalid (must go through shutdown_requested)")
	}
	if !errors.Is(err, ErrInvalidMemberTransition) {
		t.Errorf("expected ErrInvalidMemberTransition, got: %v", err)
	}
}

func TestValidateMemberTransition_ShutdownToBusy(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusShutdown, MemberStatusBusy, TransitionOption{})
	if err == nil {
		t.Fatal("expected shutdown->busy to be invalid")
	}
}

func TestValidateMemberTransition_InvalidFromUnknownState(t *testing.T) {
	err := ValidateMemberTransition("unknown", MemberStatusReady, TransitionOption{})
	if err == nil {
		t.Fatal("expected unknown state to be invalid")
	}
	if !errors.Is(err, ErrInvalidMemberTransition) {
		t.Errorf("expected ErrInvalidMemberTransition, got: %v", err)
	}
}

// =============================================================================
// ValidateMemberTransition - Guard Option
// =============================================================================

func TestValidateMemberTransition_GuardSameState(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusReady, MemberStatusReady, TransitionOption{Guard: true})
	if err != nil {
		t.Errorf("expected guard to allow same-state transition, got: %v", err)
	}
}

func TestValidateMemberTransition_GuardDifferentState(t *testing.T) {
	// Guard does not make invalid transitions valid
	err := ValidateMemberTransition(MemberStatusShutdown, MemberStatusBusy, TransitionOption{Guard: true})
	if err == nil {
		t.Fatal("expected guard to not bypass invalid transition")
	}
}

func TestValidateMemberTransition_GuardSameStateShutdown(t *testing.T) {
	err := ValidateMemberTransition(MemberStatusShutdown, MemberStatusShutdown, TransitionOption{Guard: true})
	if err != nil {
		t.Errorf("expected guard to allow shutdown->shutdown, got: %v", err)
	}
}

// =============================================================================
// ValidateMemberTransition - Force Option
// =============================================================================

func TestValidateMemberTransition_ForceBypassesValidation(t *testing.T) {
	// shutdown->busy is normally invalid
	err := ValidateMemberTransition(MemberStatusShutdown, MemberStatusBusy, TransitionOption{Force: true})
	if err != nil {
		t.Errorf("expected force to bypass validation, got: %v", err)
	}
}

func TestValidateMemberTransition_ForceAllowsUnknownState(t *testing.T) {
	err := ValidateMemberTransition("unknown", "also_unknown", TransitionOption{Force: true})
	if err != nil {
		t.Errorf("expected force to allow unknown states, got: %v", err)
	}
}

// =============================================================================
// ValidateExecTransition - Valid Transitions
// =============================================================================

func TestValidateExecTransition_IdleToProcessing(t *testing.T) {
	err := ValidateExecTransition(ExecStatusIdle, ExecStatusProcessing, TransitionOption{})
	if err != nil {
		t.Errorf("expected idle->processing to be valid, got: %v", err)
	}
}

func TestValidateExecTransition_ProcessingToCallingTool(t *testing.T) {
	err := ValidateExecTransition(ExecStatusProcessing, ExecStatusCallingTool, TransitionOption{})
	if err != nil {
		t.Errorf("expected processing->calling_tool to be valid, got: %v", err)
	}
}

func TestValidateExecTransition_CallingToolToProcessing(t *testing.T) {
	err := ValidateExecTransition(ExecStatusCallingTool, ExecStatusProcessing, TransitionOption{})
	if err != nil {
		t.Errorf("expected calling_tool->processing to be valid, got: %v", err)
	}
}

func TestValidateExecTransition_ProcessingToCompleted(t *testing.T) {
	err := ValidateExecTransition(ExecStatusProcessing, ExecStatusCompleted, TransitionOption{})
	if err != nil {
		t.Errorf("expected processing->completed to be valid, got: %v", err)
	}
}

func TestValidateExecTransition_CompletedToIdle(t *testing.T) {
	err := ValidateExecTransition(ExecStatusCompleted, ExecStatusIdle, TransitionOption{})
	if err != nil {
		t.Errorf("expected completed->idle to be valid, got: %v", err)
	}
}

func TestValidateExecTransition_CancellingToCancelled(t *testing.T) {
	err := ValidateExecTransition(ExecStatusCancelling, ExecStatusCancelled, TransitionOption{})
	if err != nil {
		t.Errorf("expected cancelling->cancelled to be valid, got: %v", err)
	}
}

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
// ValidateExecTransition - Invalid Transitions
// =============================================================================

func TestValidateExecTransition_IdleToCompleted(t *testing.T) {
	err := ValidateExecTransition(ExecStatusIdle, ExecStatusCompleted, TransitionOption{})
	if err == nil {
		t.Fatal("expected idle->completed to be invalid")
	}
	if !errors.Is(err, ErrInvalidExecTransition) {
		t.Errorf("expected ErrInvalidExecTransition, got: %v", err)
	}
	var etErr *ExecTransitionError
	if !errors.As(err, &etErr) {
		t.Fatalf("expected *ExecTransitionError, got %T", err)
	}
	if etErr.From != ExecStatusIdle {
		t.Errorf("expected From=idle, got %s", etErr.From)
	}
	if etErr.To != ExecStatusCompleted {
		t.Errorf("expected To=completed, got %s", etErr.To)
	}
}

func TestValidateExecTransition_CompletedToProcessing(t *testing.T) {
	err := ValidateExecTransition(ExecStatusCompleted, ExecStatusProcessing, TransitionOption{})
	if err == nil {
		t.Fatal("expected completed->processing to be invalid")
	}
}

func TestValidateExecTransition_CancelledToCompleted(t *testing.T) {
	err := ValidateExecTransition(ExecStatusCancelled, ExecStatusCompleted, TransitionOption{})
	if err == nil {
		t.Fatal("expected cancelled->completed to be invalid")
	}
}

func TestValidateExecTransition_InvalidFromUnknownState(t *testing.T) {
	err := ValidateExecTransition("unknown", ExecStatusIdle, TransitionOption{})
	if err == nil {
		t.Fatal("expected unknown state to be invalid")
	}
	if !errors.Is(err, ErrInvalidExecTransition) {
		t.Errorf("expected ErrInvalidExecTransition, got: %v", err)
	}
}

// =============================================================================
// ValidateExecTransition - Guard Option
// =============================================================================

func TestValidateExecTransition_GuardSameState(t *testing.T) {
	err := ValidateExecTransition(ExecStatusProcessing, ExecStatusProcessing, TransitionOption{Guard: true})
	if err != nil {
		t.Errorf("expected guard to allow same-state transition, got: %v", err)
	}
}

func TestValidateExecTransition_GuardDifferentInvalid(t *testing.T) {
	err := ValidateExecTransition(ExecStatusIdle, ExecStatusCompleted, TransitionOption{Guard: true})
	if err == nil {
		t.Fatal("expected guard to not bypass invalid transition")
	}
}

func TestValidateExecTransition_GuardSameStateIdle(t *testing.T) {
	err := ValidateExecTransition(ExecStatusIdle, ExecStatusIdle, TransitionOption{Guard: true})
	if err != nil {
		t.Errorf("expected guard to allow idle->idle, got: %v", err)
	}
}

// =============================================================================
// ValidateExecTransition - Force Option
// =============================================================================

func TestValidateExecTransition_ForceBypassesValidation(t *testing.T) {
	// idle->completed is normally invalid
	err := ValidateExecTransition(ExecStatusIdle, ExecStatusCompleted, TransitionOption{Force: true})
	if err != nil {
		t.Errorf("expected force to bypass validation, got: %v", err)
	}
}

func TestValidateExecTransition_ForceAllowsUnknownState(t *testing.T) {
	err := ValidateExecTransition("unknown", "also_unknown", TransitionOption{Force: true})
	if err != nil {
		t.Errorf("expected force to allow unknown states, got: %v", err)
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

func TestMemberTransitionError_Unwrap(t *testing.T) {
	err := &MemberTransitionError{From: MemberStatusReady, To: MemberStatusShutdown}
	if !errors.Is(err, ErrInvalidMemberTransition) {
		t.Error("expected Unwrap to return ErrInvalidMemberTransition")
	}
}

func TestExecTransitionError_ErrorMessage(t *testing.T) {
	err := &ExecTransitionError{From: ExecStatusIdle, To: ExecStatusCompleted}
	expected := "state: invalid execution transition: idle -> completed"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestExecTransitionError_Unwrap(t *testing.T) {
	err := &ExecTransitionError{From: ExecStatusIdle, To: ExecStatusCompleted}
	if !errors.Is(err, ErrInvalidExecTransition) {
		t.Error("expected Unwrap to return ErrInvalidExecTransition")
	}
}
