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

package sdlc

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestSatisfiedStop_ConcludesRatherThanParkingOrCharging is the regression guard
// for go-quicbaas#948.
//
// A worker that reads the tree, finds the change already present and says so has
// done its job. The vocabulary had no word for that, so it was recorded as
// `owner-decision` (filling the owner's queue with items needing no decision) or
// failed as "produced no changes" (spending an attempt on a task that had
// nothing to spend it on).
func TestSatisfiedStop_ConcludesRatherThanParkingOrCharging(t *testing.T) {
	t.Parallel()
	outcome, known := stopCodeOutcomes[TurnStopSatisfied]
	if !known {
		t.Fatalf("stop code %q is not in the vocabulary, so ClassifyTurn cannot act on it", TurnStopSatisfied)
	}
	if outcome != TurnOutcomeSatisfied {
		t.Fatalf("stop code %q classifies as %q, want %q", TurnStopSatisfied, outcome, TurnOutcomeSatisfied)
	}

	verdict, ok := turnVerdicts[outcome]
	if !ok {
		t.Fatal("the satisfied outcome has no verdict")
	}
	if verdict.Action != TurnActionConclude {
		t.Errorf("action = %q, want %q: a correct no-change turn must not park or charge",
			verdict.Action, TurnActionConclude)
	}
	// Nothing is waiting and nothing is at fault, so the park fields must be
	// empty — a BlockKind here would put the task back in the queue this
	// outcome exists to keep it out of.
	if verdict.BlockKind != "" {
		t.Errorf("BlockKind = %q, want empty: a satisfied turn parks nobody", verdict.BlockKind)
	}
	if verdict.FaultClass != "" {
		t.Errorf("FaultClass = %q, want empty: nothing failed", verdict.FaultClass)
	}
	if _, performed := turnActions[verdict.Action]; !performed {
		t.Errorf("action %q is not in the runner's table", verdict.Action)
	}
}

// TestSatisfiedConclusion_IsTerminalAndSpeaks: the conclusion must be a member
// of the closed vocabulary AND post a comment. Concluding it `completed` would
// be silent (OutcomeSilent), which leaves a stale issue looking like delivered
// work — the reason this needed its own reason rather than reusing completion.
func TestSatisfiedConclusion_IsTerminalAndSpeaks(t *testing.T) {
	t.Parallel()
	if !orchestration.TaskConcludedAlreadySatisfied.Valid() {
		t.Fatal("already_satisfied is not in the closed conclusion vocabulary")
	}
	kind, known := OutcomeOfConclusion(orchestration.TaskConcludedAlreadySatisfied)
	if !known {
		t.Fatal("already_satisfied has no outcome row, so the reporter refuses to post for it")
	}
	if kind == OutcomeSilent {
		t.Fatal("already_satisfied posts nothing; a stale issue must be told it is stale")
	}
	spec, found := lookupOutcomeSpec(kind)
	if !found {
		t.Fatalf("outcome %q has no rendering", kind)
	}
	if !spec.terminal {
		t.Error("a satisfied task is finished; the engine must not keep working it")
	}
	// No label: every binding label says something is wrong or pending, and a
	// stale issue is neither.
	if spec.labelKey != "" {
		t.Errorf("labelKey = %q, want empty: no binding label means 'already done'", spec.labelKey)
	}
}

// TestSatisfiedJudgementSurvivesTheStatus: the judgement has to reach the
// persisted status, or a crash between the turn and the conclusion loses it and
// the reaper rebuilds the attempt as an abandoned failure.
func TestSatisfiedJudgementSurvivesTheStatus(t *testing.T) {
	t.Parallel()
	run := &orchestration.JobRun{}
	if run.Satisfied() {
		t.Fatal("a fresh run must not claim the work was already done")
	}
	run.Satisfy()
	if !run.Satisfied() {
		t.Fatal("Satisfy did not record the judgement")
	}
}
