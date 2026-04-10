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

func TestGetPolicy_LeadAssign(t *testing.T) {
	policy, err := GetPolicy("lead", "assign")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected %s, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_LeadFeedback(t *testing.T) {
	policy, err := GetPolicy("lead", "feedback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected %s, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_LeadCancel(t *testing.T) {
	policy, err := GetPolicy("lead", "cancel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected %s, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_WorkerComplete(t *testing.T) {
	policy, err := GetPolicy("worker", "complete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyBlocking {
		t.Errorf("expected %s, got %s", MessagePolicyBlocking, policy)
	}
}

func TestGetPolicy_WorkerFailed(t *testing.T) {
	policy, err := GetPolicy("worker", "failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyBlocking {
		t.Errorf("expected %s, got %s", MessagePolicyBlocking, policy)
	}
}

func TestGetPolicy_WorkerQuestion(t *testing.T) {
	policy, err := GetPolicy("worker", "question")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected %s, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_ValidatorResult(t *testing.T) {
	policy, err := GetPolicy("validator", "result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyBlocking {
		t.Errorf("expected %s, got %s", MessagePolicyBlocking, policy)
	}
}

func TestGetPolicy_UnknownDefaultsToFireAndForget(t *testing.T) {
	policy, err := GetPolicy("unknown_role", "unknown_action")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected default %s for unknown policy, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_EmptyInputsDefaultToFireAndForget(t *testing.T) {
	policy, err := GetPolicy("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != MessagePolicyFireAndForget {
		t.Errorf("expected default %s for empty inputs, got %s", MessagePolicyFireAndForget, policy)
	}
}

func TestGetPolicy_AllDefinedPolicies(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		action   string
		expected MessagePolicy
	}{
		{"lead_assign", "lead", "assign", MessagePolicyFireAndForget},
		{"lead_feedback", "lead", "feedback", MessagePolicyFireAndForget},
		{"lead_cancel", "lead", "cancel", MessagePolicyFireAndForget},
		{"worker_complete", "worker", "complete", MessagePolicyBlocking},
		{"worker_failed", "worker", "failed", MessagePolicyBlocking},
		{"worker_question", "worker", "question", MessagePolicyFireAndForget},
		{"validator_result", "validator", "result", MessagePolicyBlocking},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := GetPolicy(tc.from, tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if policy != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, policy)
			}
		})
	}
}

// =============================================================================
// ValidateCommunication - Hub and Spoke (default)
// =============================================================================

func TestValidateCommunication_HubAndSpoke_LeadToWorker(t *testing.T) {
	err := ValidateCommunication(TopologyHubAndSpoke, "lead", "worker", "assign")
	if err != nil {
		t.Errorf("expected lead-to-worker to be allowed, got error: %v", err)
	}
}

func TestValidateCommunication_HubAndSpoke_WorkerToLead(t *testing.T) {
	err := ValidateCommunication(TopologyHubAndSpoke, "worker", "lead", "complete")
	if err != nil {
		t.Errorf("expected worker-to-lead to be allowed, got error: %v", err)
	}
}

func TestValidateCommunication_HubAndSpoke_ValidatorToLead(t *testing.T) {
	err := ValidateCommunication(TopologyHubAndSpoke, "validator", "lead", "result")
	if err != nil {
		t.Errorf("expected validator-to-lead to be allowed, got error: %v", err)
	}
}

func TestValidateCommunication_HubAndSpoke_LeadToValidator(t *testing.T) {
	err := ValidateCommunication(TopologyHubAndSpoke, "lead", "validator", "request_review")
	if err != nil {
		t.Errorf("expected lead-to-validator to be allowed, got error: %v", err)
	}
}

func TestValidateCommunication_HubAndSpoke_WorkerToWorkerBlocked(t *testing.T) {
	err := ValidateCommunication(TopologyHubAndSpoke, "worker", "worker", "delegate")
	if err == nil {
		t.Fatal("expected worker-to-worker to be blocked, got nil error")
	}

	if !errors.Is(err, ErrWorkerToWorkerNotAllowed) {
		t.Errorf("expected ErrWorkerToWorkerNotAllowed, got: %v", err)
	}

	var wwErr *WorkerToWorkerError
	if !errors.As(err, &wwErr) {
		t.Fatalf("expected *WorkerToWorkerError, got %T", err)
	}
	if wwErr.From != "worker" {
		t.Errorf("expected From=worker, got %s", wwErr.From)
	}
	if wwErr.To != "worker" {
		t.Errorf("expected To=worker, got %s", wwErr.To)
	}
	if wwErr.Action != "delegate" {
		t.Errorf("expected Action=delegate, got %s", wwErr.Action)
	}
}

func TestValidateCommunication_HubAndSpoke_WorkerToWorkerAnyAction(t *testing.T) {
	actions := []string{"assign", "complete", "feedback", "question", "advisory", ""}
	for _, action := range actions {
		t.Run("action_"+action, func(t *testing.T) {
			err := ValidateCommunication(TopologyHubAndSpoke, "worker", "worker", action)
			if err == nil {
				t.Errorf("expected worker-to-worker with action %q to be blocked", action)
			}
		})
	}
}

// =============================================================================
// ValidateCommunication - Hybrid
// =============================================================================

func TestValidateCommunication_Hybrid_AdvisoryAllowed(t *testing.T) {
	advisoryList := []string{"advisory", "progress", "finding", "conflict_warning"}
	for _, action := range advisoryList {
		t.Run(action, func(t *testing.T) {
			err := ValidateCommunication(TopologyHybrid, "worker", "worker", action)
			if err != nil {
				t.Errorf("expected advisory action %q to be allowed in hybrid, got: %v", action, err)
			}
		})
	}
}

func TestValidateCommunication_Hybrid_NonAdvisoryBlocked(t *testing.T) {
	nonAdvisory := []string{"assign", "complete", "delegate", "cancel"}
	for _, action := range nonAdvisory {
		t.Run(action, func(t *testing.T) {
			err := ValidateCommunication(TopologyHybrid, "worker", "worker", action)
			if err == nil {
				t.Errorf("expected non-advisory action %q to be blocked in hybrid", action)
			}
			if !errors.Is(err, ErrNonAdvisoryPeerMessage) {
				t.Errorf("expected ErrNonAdvisoryPeerMessage, got: %v", err)
			}
			var napErr *NonAdvisoryPeerMessageError
			if !errors.As(err, &napErr) {
				t.Fatalf("expected *NonAdvisoryPeerMessageError, got %T", err)
			}
			if napErr.Action != action {
				t.Errorf("expected Action=%s, got %s", action, napErr.Action)
			}
		})
	}
}

func TestValidateCommunication_Hybrid_LeadToWorkerAllowed(t *testing.T) {
	err := ValidateCommunication(TopologyHybrid, "lead", "worker", "assign")
	if err != nil {
		t.Errorf("expected lead-to-worker to be allowed in hybrid, got: %v", err)
	}
}

// =============================================================================
// ValidateCommunication - Peer to Peer
// =============================================================================

func TestValidateCommunication_PeerToPeer_WorkerToWorkerAllowed(t *testing.T) {
	actions := []string{"assign", "complete", "delegate", "advisory", "cancel"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			err := ValidateCommunication(TopologyPeerToPeer, "worker", "worker", action)
			if err != nil {
				t.Errorf("expected worker-to-worker %q to be allowed in p2p, got: %v", action, err)
			}
		})
	}
}

func TestValidateCommunication_PeerToPeer_LeadToWorkerAllowed(t *testing.T) {
	err := ValidateCommunication(TopologyPeerToPeer, "lead", "worker", "assign")
	if err != nil {
		t.Errorf("expected lead-to-worker to be allowed in p2p, got: %v", err)
	}
}

// =============================================================================
// ValidateCommunication - Default/Unknown topology
// =============================================================================

func TestValidateCommunication_UnknownTopologyBlocksWorkerToWorker(t *testing.T) {
	err := ValidateCommunication("unknown", "worker", "worker", "delegate")
	if err == nil {
		t.Fatal("expected unknown topology to block worker-to-worker")
	}
	if !errors.Is(err, ErrWorkerToWorkerNotAllowed) {
		t.Errorf("expected ErrWorkerToWorkerNotAllowed, got: %v", err)
	}
}

func TestValidateCommunication_EmptyTopologyBlocksWorkerToWorker(t *testing.T) {
	err := ValidateCommunication("", "worker", "worker", "delegate")
	if err == nil {
		t.Fatal("expected empty topology to block worker-to-worker")
	}
	if !errors.Is(err, ErrWorkerToWorkerNotAllowed) {
		t.Errorf("expected ErrWorkerToWorkerNotAllowed, got: %v", err)
	}
}

// =============================================================================
// IsAdvisoryAction
// =============================================================================

func TestIsAdvisoryAction_KnownAdvisory(t *testing.T) {
	advisoryList := []string{"advisory", "progress", "finding", "conflict_warning"}
	for _, action := range advisoryList {
		t.Run(action, func(t *testing.T) {
			if !IsAdvisoryAction(action) {
				t.Errorf("expected %q to be advisory", action)
			}
		})
	}
}

func TestIsAdvisoryAction_NonAdvisory(t *testing.T) {
	nonAdvisory := []string{"assign", "complete", "cancel", "", "unknown"}
	for _, action := range nonAdvisory {
		t.Run(action, func(t *testing.T) {
			if IsAdvisoryAction(action) {
				t.Errorf("expected %q to NOT be advisory", action)
			}
		})
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestWorkerToWorkerError_ErrorMessage(t *testing.T) {
	err := &WorkerToWorkerError{
		From:   "worker",
		To:     "worker",
		Action: "delegate",
	}
	msg := err.Error()
	expected := "communication: worker-to-worker not allowed in hub-and-spoke topology: from=worker to=worker action=delegate"
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestWorkerToWorkerError_Unwrap(t *testing.T) {
	err := &WorkerToWorkerError{From: "worker", To: "worker", Action: "x"}
	if !errors.Is(err, ErrWorkerToWorkerNotAllowed) {
		t.Error("expected Unwrap to return ErrWorkerToWorkerNotAllowed")
	}
}

func TestNonAdvisoryPeerMessageError_ErrorMessage(t *testing.T) {
	err := &NonAdvisoryPeerMessageError{
		From:   "worker",
		To:     "worker",
		Action: "delegate",
	}
	msg := err.Error()
	expected := "communication: non-advisory peer message not allowed in hybrid topology: from=worker to=worker action=delegate"
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestNonAdvisoryPeerMessageError_Unwrap(t *testing.T) {
	err := &NonAdvisoryPeerMessageError{From: "worker", To: "worker", Action: "x"}
	if !errors.Is(err, ErrNonAdvisoryPeerMessage) {
		t.Error("expected Unwrap to return ErrNonAdvisoryPeerMessage")
	}
}

func TestCommunicationDirection_Fields(t *testing.T) {
	dir := CommunicationDirection{
		From:   "lead",
		To:     "worker",
		Action: "assign",
	}
	if dir.From != "lead" {
		t.Errorf("expected From=lead, got %s", dir.From)
	}
	if dir.To != "worker" {
		t.Errorf("expected To=worker, got %s", dir.To)
	}
	if dir.Action != "assign" {
		t.Errorf("expected Action=assign, got %s", dir.Action)
	}
}

func TestMaxBlockDuration_Value(t *testing.T) {
	expected := 5 * 60 // 5 minutes in seconds
	actual := int(MaxBlockDuration.Seconds())
	if actual != expected {
		t.Errorf("expected MaxBlockDuration=%ds, got %ds", expected, actual)
	}
}
