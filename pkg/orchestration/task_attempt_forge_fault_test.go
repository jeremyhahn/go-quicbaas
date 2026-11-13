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
	stderrors "errors"
	"strings"
	"testing"
)

// =============================================================================
// A forge that did not answer is the forge's fault, not the work's
// =============================================================================
//
// The forge adapters live above this package, so these tests stand in for
// their transport error with the one fact it carries across the boundary: it
// unwraps to ErrForgeUnreachable. The adapter-side tests in pkg/orchestration/sdlc prove the
// real errors do.

// forgeTransportError is a stand-in for the adapter's unanswered-call error.
type forgeTransportError struct{ op string }

func (e *forgeTransportError) Error() string { return "gitea did not answer " + e.op }
func (e *forgeTransportError) Unwrap() error { return ErrForgeUnreachable }

// TestClassifyEnvironmentFault_AnUnansweredForgeIsAnEnvironmentFault proves the
// class, the reason, and that it reaches through the wrapping a failed stage
// adds — the Complete hook's merge was the burn-in node's case.
func TestClassifyEnvironmentFault_AnUnansweredForgeIsAnEnvironmentFault(t *testing.T) {
	cases := map[string]error{
		"bare":                &forgeTransportError{op: "ListIssues"},
		"inside a stage":      &JobHookError{Stage: JobStageComplete, Hooks: "sdlc", JobID: "j1", Cause: &forgeTransportError{op: "MergePullRequest"}},
		"inside a validator":  &ValidatorExecutionError{Validator: "forge-review", Cause: &forgeTransportError{op: "GetPullRequest"}},
		"the sentinel itself": ErrForgeUnreachable,
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			class, fault := classifyJobFailure(err)
			if class != FaultForgeUnreachable || fault.Class != FaultForgeUnreachable {
				t.Fatalf("class = %q, want %q", class, FaultForgeUnreachable)
			}
			if !strings.Contains(fault.Reason, "not charged") {
				t.Errorf("reason %q does not say the attempt was refunded", fault.Reason)
			}
		})
	}
}

// TestClassifyEnvironmentFault_AnyOtherForgeRefusalIsTheWork proves the class
// is narrow: a forge that ANSWERED — refused a merge, reported a moved head —
// said something about the work, and that is still charged.
func TestClassifyEnvironmentFault_AnyOtherForgeRefusalIsTheWork(t *testing.T) {
	err := &JobHookError{Stage: JobStageComplete, Hooks: "sdlc", JobID: "j1",
		Cause: stderrors.New("sdlc: pull request head moved since it was reviewed")}
	if class, _ := classifyJobFailure(err); class != "" {
		t.Errorf("class = %q; a forge's answer was attributed to the environment", class)
	}
	if _, classified := classifyEnvironmentFault(err); classified {
		t.Error("a forge's answer was classified as an environment fault")
	}
}

// TestForgeUnreachable_IsARefundedBoundedRequeue proves what the class does to
// the task: the attempt comes back and the task requeues, until the third in a
// row, which parks and is charged like every other requeueing class.
func TestForgeUnreachable_IsARefundedBoundedRequeue(t *testing.T) {
	fault, _ := classifyEnvironmentFault(&forgeTransportError{op: "GetPullRequest"})

	first := faultOutcomeFor(&Task{}, fault)
	if !first.refund || !first.requeue || !first.counts {
		t.Errorf("first fault = %+v, want refunded, requeued and counted", first)
	}
	third := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1}, fault)
	if !third.bounded || third.refund {
		t.Errorf("third fault = %+v, want bounded and charged", third)
	}
	if !KnownFaultClass(FaultForgeUnreachable) {
		t.Error("the class is not known, so a park carrying it would be refused")
	}
	if _, ok := FaultClassMeaning(FaultForgeUnreachable); !ok {
		t.Error("the class has no meaning for the forge comment to quote")
	}
}
