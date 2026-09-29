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
	"testing"
)

// TestPlanPromptTurn_ModelFallsBackToTheSession is the regression guard for the
// ledger rejecting every consumption entry on the burn-in node:
//
//	failed to record consumption; this account's spend cap is now
//	under-counting: err=ledger: invalid consumption record: model: must not be
//	empty
//
// No factory prompt names a model — the SDLC and workflow runners set Model on
// the SPAWN request only — and Project.DefaultModel is optional and unset. So
// every planned turn carried an empty model, validateConsumptionEntry refused
// the entry, and account spend caps counted nothing at all while the turns
// themselves succeeded. Silent by construction: the turn works, only the
// accounting is gone.
//
// The session is the authority the resolution was missing. planPromptTurn
// already fell back to the session record for the BACKEND; the model simply had
// no equivalent.
func TestPlanPromptTurn_ModelFallsBackToTheSession(t *testing.T) {
	t.Parallel()
	svc := &agentServiceImpl{}
	svc.backends.Store("mock", &mockAgentBackend{})
	svc.sessions.Store("sess-1", &sessionRecord{Backend: "mock", Model: "claude-opus-5"})

	plan, err := svc.planPromptTurn(context.Background(), &PromptSessionRequest{
		SessionID: "sess-1",
		Prompt:    "do the thing",
	})
	if err != nil {
		t.Fatalf("planPromptTurn: %v", err)
	}
	if plan.model != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5; a turn with no model is a charge the ledger refuses", plan.model)
	}
	// The backend fallback that always worked must keep working.
	if plan.backendName != "mock" {
		t.Errorf("backendName = %q, want mock", plan.backendName)
	}
}

// TestPlanPromptTurn_RequestModelWinsOverTheSession: the fallback is a fallback.
// A caller that names a model is switching models deliberately, and the
// session's value must not override it.
func TestPlanPromptTurn_RequestModelWinsOverTheSession(t *testing.T) {
	t.Parallel()
	svc := &agentServiceImpl{}
	svc.backends.Store("mock", &mockAgentBackend{})
	svc.sessions.Store("sess-1", &sessionRecord{Backend: "mock", Model: "claude-opus-5"})

	plan, err := svc.planPromptTurn(context.Background(), &PromptSessionRequest{
		SessionID: "sess-1",
		Prompt:    "p",
		Model:     "claude-haiku-4-5-20251001",
	})
	if err != nil {
		t.Fatalf("planPromptTurn: %v", err)
	}
	if plan.model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q; an explicit request model must win over the session's", plan.model)
	}
}

// TestCompletedTurn_ServedModelWinsOverThePlanned: the ledger's per-model
// breakdown is only honest if it names the model that actually ran. A CLI that
// substitutes one — a quota fallback, an alias resolving to a dated snapshot —
// would otherwise have its spend filed under a model that never served a token.
func TestCompletedTurn_ServedModelWinsOverThePlanned(t *testing.T) {
	t.Parallel()
	plan := &promptTurnPlan{
		turnID:      "turn-1",
		backendName: "mock",
		model:       "claude-opus-5",
		request:     &PromptRequest{ProjectID: "proj-1"},
	}
	if got := plan.completedTurn("sess-1", "claude-opus-5-20260801", 0).model; got != "claude-opus-5-20260801" {
		t.Errorf("model = %q, want the served model", got)
	}
	// A failed turn has no response to ask, and a backend that reports nothing
	// has contradicted nothing: the plan's model stands.
	if got := plan.completedTurn("sess-1", "", 0).model; got != "claude-opus-5" {
		t.Errorf("model = %q, want the planned model to stand when none was served", got)
	}
	// The rest of the turn's identity travels unchanged.
	turn := plan.completedTurn("sess-1", "served", 0)
	if turn.turnID != "turn-1" || turn.sessionID != "sess-1" || turn.engine != "mock" || turn.projectID != "proj-1" {
		t.Errorf("turn = %+v; only the model is decided here", turn)
	}
}
