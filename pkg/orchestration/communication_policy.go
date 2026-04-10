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
	"time"
)

// MessagePolicy classifies inter-agent communications for deadlock prevention.
type MessagePolicy string

const (
	// MessagePolicyFireAndForget indicates the sender does not wait for a response.
	MessagePolicyFireAndForget MessagePolicy = "fire_and_forget"

	// MessagePolicyBlocking indicates the sender waits for an acknowledgment.
	MessagePolicyBlocking MessagePolicy = "blocking"
)

// MaxBlockDuration is the timeout for blocking messages. Blocking communications
// that exceed this duration are treated as failures to prevent deadlocks.
const MaxBlockDuration = 5 * time.Minute

// CommunicationDirection represents a sender-to-receiver direction.
type CommunicationDirection struct {
	From   string // "lead", "worker", "validator"
	To     string
	Action string // "assign", "complete", "feedback", etc.
}

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrWorkerToWorkerNotAllowed indicates worker-to-worker communication was
	// attempted, which violates the hub-and-spoke topology.
	ErrWorkerToWorkerNotAllowed = errors.New("communication: worker-to-worker not allowed in hub-and-spoke topology")

	// ErrPolicyLookupFailed indicates the policy lookup key was not found.
	ErrPolicyLookupFailed = errors.New("communication: policy lookup failed")

	// ErrNonAdvisoryPeerMessage indicates a non-advisory worker-to-worker message
	// was attempted under the hybrid topology.
	ErrNonAdvisoryPeerMessage = errors.New("communication: non-advisory peer message not allowed in hybrid topology")
)

// =============================================================================
// Typed Errors
// =============================================================================

// WorkerToWorkerError indicates an attempted worker-to-worker communication
// that is not allowed under the hub-and-spoke topology.
type WorkerToWorkerError struct {
	From   string
	To     string
	Action string
}

// Error implements the error interface.
func (e *WorkerToWorkerError) Error() string {
	return ErrWorkerToWorkerNotAllowed.Error() + ": from=" + e.From + " to=" + e.To + " action=" + e.Action
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkerToWorkerError) Unwrap() error {
	return ErrWorkerToWorkerNotAllowed
}

// NonAdvisoryPeerMessageError indicates a non-advisory worker-to-worker message
// was attempted under the hybrid topology.
type NonAdvisoryPeerMessageError struct {
	From   string
	To     string
	Action string
}

// Error implements the error interface.
func (e *NonAdvisoryPeerMessageError) Error() string {
	return ErrNonAdvisoryPeerMessage.Error() + ": from=" + e.From + " to=" + e.To + " action=" + e.Action
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NonAdvisoryPeerMessageError) Unwrap() error {
	return ErrNonAdvisoryPeerMessage
}

// =============================================================================
// Advisory Action Classification
// =============================================================================

// advisoryActions is the set of advisory actions for O(1) classification.
// Advisory actions are informational and do not require acknowledgment.
var advisoryActions = map[string]struct{}{
	"advisory":         {},
	"progress":         {},
	"finding":          {},
	"conflict_warning": {},
}

// IsAdvisoryAction returns true if the action is classified as advisory.
func IsAdvisoryAction(action string) bool {
	_, ok := advisoryActions[action]
	return ok
}

// =============================================================================
// Communication Policies
// =============================================================================

// communicationPolicies defines the hub-and-spoke deadlock-safe rules.
// Key format: "from:action" for O(1) constant-time lookup.
var communicationPolicies = map[string]MessagePolicy{
	// Lead -> Worker: always fire-and-forget (prevents hub-spoke deadlock)
	"lead:assign":   MessagePolicyFireAndForget,
	"lead:feedback": MessagePolicyFireAndForget,
	"lead:cancel":   MessagePolicyFireAndForget,

	// Worker -> Lead: blocking only for completion signals
	"worker:complete": MessagePolicyBlocking,
	"worker:failed":   MessagePolicyBlocking,
	"worker:question": MessagePolicyFireAndForget,

	// Validator -> Lead: blocking (review results must be acknowledged)
	"validator:result": MessagePolicyBlocking,
}

// GetPolicy returns the communication policy for a given sender role and action.
// If the combination is not found in the policy map, it defaults to
// fire-and-forget to prevent accidental deadlocks.
func GetPolicy(from, action string) (MessagePolicy, error) {
	key := from + ":" + action
	policy, ok := communicationPolicies[key]
	if !ok {
		return MessagePolicyFireAndForget, nil
	}
	return policy, nil
}

// ValidateCommunication checks if a communication is allowed under the given
// topology. The topology controls worker-to-worker messaging:
//   - hub_and_spoke: Worker-to-worker communication is never allowed.
//   - hybrid: Worker-to-worker allowed only for advisory actions.
//   - peer_to_peer: All inter-agent communication is allowed.
func ValidateCommunication(topology CommunicationTopology, from, to, action string) error {
	// Non worker-to-worker communication is always allowed.
	if from != "worker" || to != "worker" {
		return nil
	}

	// Worker-to-worker: check topology.
	switch topology {
	case TopologyPeerToPeer:
		return nil

	case TopologyHybrid:
		if IsAdvisoryAction(action) {
			return nil
		}
		return &NonAdvisoryPeerMessageError{
			From:   from,
			To:     to,
			Action: action,
		}

	default:
		// hub_and_spoke (default) or any unrecognized topology: block all.
		return &WorkerToWorkerError{
			From:   from,
			To:     to,
			Action: action,
		}
	}
}
