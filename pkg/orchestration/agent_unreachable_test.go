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
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestWrapRemoteSpawnFault_AnUnreachableAgentIsAnEnvironmentFault is the
// regression guard for the one spawn fault the agent cannot report about itself.
//
// Every other class reaches the control plane as SpawnFaultEvidence the AGENT
// attaches to its gRPC status. An agent that is not listening attaches nothing,
// so this arrived as a bare Unavailable, matched no class, and was charged as a
// failure of the WORK — the opposite of the truth, since the RPC never arrived.
//
// Measured on the burn-in node during a deploy: three tasks were dispatched in
// the window between the daemon accepting work and the agent finishing
// registration, each was dispatched twice, and all three went from 4 of 6
// attempts to 6 of 6 and failed. Nothing about the work was wrong.
func TestWrapRemoteSpawnFault_AnUnreachableAgentIsAnEnvironmentFault(t *testing.T) {
	t.Parallel()
	// Exactly what a refused dial produces: Unavailable with no details.
	rpcErr := status.Error(codes.Unavailable,
		"connection error: desc = \"transport: Error while dialing: dial tcp 127.0.0.1:9092: connect: connection refused\"")

	wrapped := wrapRemoteSpawnFault("Spawn", rpcErr)
	if !errors.Is(wrapped, ErrAgentUnreachable) {
		t.Fatalf("wrapped = %v, want it to carry ErrAgentUnreachable", wrapped)
	}
	// The transport error stays reachable: the address that refused is the
	// operator's first clue.
	if !errors.Is(wrapped, rpcErr) {
		t.Error("the transport error must stay reachable through the wrap")
	}

	fault, ok := classifyEnvironmentFault(wrapped)
	if !ok {
		t.Fatal("an unreachable agent must classify as an environment fault, or the attempt is charged")
	}
	if fault.Class != FaultAgentUnreachable {
		t.Errorf("class = %q, want %q", fault.Class, FaultAgentUnreachable)
	}
	if !OperatorFaultClass(fault.Class) {
		t.Error("the class must be an operator fault, or applyConclusion will not refund the attempt")
	}
	if _, explained := FaultClassMeaning(fault.Class); !explained {
		t.Error("an operator fault with no rendered meaning reaches a person as a bare string")
	}
}

// TestWrapRemoteSpawnFault_ADeadlineIsStillCharged: the refusal to guess. A
// DeadlineExceeded may have been a request the agent DID receive and was slow
// to answer, which can leave a real session behind; calling that "never
// arrived" would refund an attempt that actually ran.
func TestWrapRemoteSpawnFault_ADeadlineIsStillCharged(t *testing.T) {
	t.Parallel()
	for _, code := range []codes.Code{codes.DeadlineExceeded, codes.Internal, codes.PermissionDenied} {
		wrapped := wrapRemoteSpawnFault("Spawn", status.Error(code, "boom"))
		if errors.Is(wrapped, ErrAgentUnreachable) {
			t.Errorf("%s must not be read as an unreachable agent", code)
		}
		if _, ok := classifyEnvironmentFault(wrapped); ok {
			t.Errorf("%s must keep the pre-existing answer, which is to charge", code)
		}
	}
}

// TestWrapRemoteSpawnFault_ANonStatusErrorIsUntouched: the wrap only speaks for
// errors that carry a gRPC status. Anything else passes through as it did.
func TestWrapRemoteSpawnFault_ANonStatusErrorIsUntouched(t *testing.T) {
	t.Parallel()
	plain := errors.New("something local went wrong")
	if got := wrapRemoteSpawnFault("Spawn", plain); !errors.Is(got, plain) {
		t.Errorf("got = %v, want the original error", got)
	}
	if wrapRemoteSpawnFault("Spawn", nil) != nil {
		t.Error("nil must stay nil")
	}
}
