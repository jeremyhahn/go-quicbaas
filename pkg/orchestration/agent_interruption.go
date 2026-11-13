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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// A turn the agent's host cut off — go-quicbaas#1064
// =============================================================================
//
// A WORKER TURN THAT THE FLEET AGENT STOPPED SERVING IS NOT A FAILURE OF THE
// WORK. Measured on the burn-in node during a deploy: stopping quicbaas-agent
// closed a live worker's stream mid-prompt, the Prompt RPC came back as an
// Internal status reading "container: agent stream closed", and the job was
// charged as a failure of the work. Every routine restart of an agent spent an
// attempt of whatever it was running.
//
// THE AGENT IS THE ONLY PARTY THAT KNOWS IT IS SHUTTING DOWN, so it says so on
// the one channel the control plane already reads: a turn that fails while the
// agent drains is answered Unavailable rather than Internal (pkg/orchestration/fleet,
// GRPCSpawnServer.Prompt). An agent that died outright cannot say anything, and
// the transport answers for it with the same code. Either way the control
// plane sees Unavailable — or Canceled on a stream its own caller did not
// cancel — and that is what this file turns into ErrAgentInterrupted.
//
// A CONTAINER STREAM THAT CLOSED WITHOUT A WORKER EXIT IS THE SAME FAULT, drain
// or no drain: the container was killed, its runtime failed, or its pipe was
// cut. The agent reads the container's exit status off its runtime before it
// tears the container down (ContainerStreamClosedError.Exit) and, lacking the
// worker's own non-zero exit, answers Unavailable as well.
//
// WHAT STAYS CHARGED: a worker process that exits non-zero while its agent is
// healthy. The agent reports that as Internal, which is not read here, so it
// keeps the pre-existing answer.

// ErrAgentInterrupted reports a turn the agent's host stopped serving while it
// was in flight: the agent shut down or restarted, or the transport to it was
// lost.
var ErrAgentInterrupted = errors.New("orchestration: the agent stopped serving the turn in flight")

// AgentInterruptedError reports a turn cut off by the agent's host rather than
// ended by the worker.
//
// IT NAMES THE HOST, because the attribution is only useful if an operator can
// see which agent went away: the job's record carries this error's text, and
// the log line that says the attempt was refunded quotes it.
type AgentInterruptedError struct {
	// Op names the RPC that was cut off.
	Op string

	// Host is the fleet node daemon's address, empty when the backend was not bound
	// to a registration.
	Host string

	// AgentID is the fleet node's registration id, empty when unknown.
	AgentID string

	// Code is the gRPC status code the interruption arrived as.
	Code codes.Code

	// Err is the transport error, preserved so the status stays reachable.
	Err error
}

// Error implements the error interface.
func (e *AgentInterruptedError) Error() string {
	msg := "remote_acp_backend: " + e.Op + " interrupted by the agent"
	if e.AgentID != "" {
		msg += " " + e.AgentID
	}
	if e.Host != "" {
		msg += " on host " + e.Host
	}
	msg += " (" + e.Code.String() + ")"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the transport error, so errors.Is matches the
// interruption and the original status alike.
func (e *AgentInterruptedError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrAgentInterrupted}
	}
	return []error{ErrAgentInterrupted, e.Err}
}

// agentInterruption reports whether rpcErr is a turn the agent's host cut off,
// and the code it arrived as.
//
// ONLY TWO CODES, and the second only conditionally:
//
//   - Unavailable: the agent is draining and said so, or the connection to it
//     is gone — a closed transport, a daemon that exited.
//   - Canceled while the caller's own context is still live: the agent's
//     server cancelled the stream, which it does when it stops. A Canceled on a
//     context THIS side cancelled is the job being ended here — an abort, a
//     stall — and is not the agent's doing.
//
// Everything else keeps its pre-existing answer. Internal in particular is
// what a crashed worker process arrives as, and that is still the work's.
func agentInterruption(ctx context.Context, rpcErr error) (codes.Code, bool) {
	st, ok := status.FromError(rpcErr)
	if !ok {
		return codes.OK, false
	}
	switch st.Code() {
	case codes.Unavailable:
		return codes.Unavailable, true
	case codes.Canceled:
		if ctx.Err() == nil {
			return codes.Canceled, true
		}
	}
	return codes.OK, false
}

// wrapAgentInterruption returns wrapped as an AgentInterruptedError when rpcErr
// is a turn the agent's host cut off, and wrapped unchanged otherwise.
//
// rpcErr is the raw transport error the code is read from; wrapped is what the
// caller has already made of it (the daemon's turn-failure evidence, when it
// attached any), and it is what the interruption carries.
func wrapAgentInterruption(ctx context.Context, op, host, agentID string, rpcErr, wrapped error) error {
	code, interrupted := agentInterruption(ctx, rpcErr)
	if !interrupted {
		return wrapped
	}
	return &AgentInterruptedError{Op: op, Host: host, AgentID: agentID, Code: code, Err: wrapped}
}

// isContainerTransportLoss reports whether err is a worker's container stream
// that closed WITHOUT the worker's own non-zero exit — the container was
// killed, its runtime failed, or its pipe was cut. A remote fleet node reports that
// Unavailable (pkg/orchestration/fleet, mapPromptError); this reads the same fact off a turn
// whose backend ran in this process. A stream that closed with the worker's
// non-zero exit is a crash, and is not matched here.
func isContainerTransportLoss(err error) bool {
	var closed *ContainerStreamClosedError
	return errors.As(err, &closed) && !closed.WorkerCrashed()
}

// agentInterruptedReason composes the fault reason, naming the host when the
// error carries one.
func agentInterruptedReason(err error) string {
	where := "the agent"
	var interrupted *AgentInterruptedError
	if errors.As(err, &interrupted) && interrupted.Host != "" {
		where = "the agent on " + interrupted.Host
	}
	return where + " stopped serving this worker's turn while it was in flight — it shut down or " +
		"restarted, or the connection to it was lost. That is the fleet's infrastructure, not the " +
		"work, so this attempt was not charged."
}
