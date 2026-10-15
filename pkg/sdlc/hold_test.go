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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A hold is absolute (go-quicbaas#1060)
// =============================================================================
//
// go-quicbaas#1058 was filed `task:blocked`; the triage backstop judged it
// ready, labelled it `task:ready`, the source adopted it and the worker was
// charged an attempt for refusing it. Each test below closes one of the three
// doors that let it through.

// TestTriageRunner_TheBackstopSkipsAHeldIssue is candidate selection: a held
// issue costs no turn, gets no label and no comment, and the skip is audited.
func TestTriageRunner_TheBackstopSkipsAHeldIssue(t *testing.T) {
	for _, hold := range []string{HoldLabel, "Task:Blocked"} {
		t.Run(hold, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.forge.putIssue(1058, "lead-driven work", hold)
			rig.answers("Scoped and verifiable.\n\nSDLC-TRIAGE: ready")

			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
				t.Fatalf("a held issue is never judged, got %d prompts", len(prompts))
			}
			if spawns := rig.agents.sessionCount(); spawns != 0 {
				t.Fatalf("a pass with only held issues spawns nothing, got %d sessions", spawns)
			}
			if rig.hasLabel(1058, rig.binding.Forge.TriggerLabel) {
				t.Fatalf("a held issue must never gain the trigger, got %v", rig.labels(1058))
			}
			if !rig.hasLabel(1058, hold) {
				t.Fatalf("the hold must stand, got %v", rig.labels(1058))
			}
			if comments := rig.comments(1058); len(comments) != 0 {
				t.Fatalf("triage says nothing on a held issue, got %v", comments)
			}
			details, ok := rig.auditDetails("sdlc:triage_skipped")
			if !ok {
				t.Fatal("a held issue passed over must be audited, or it reads as never looked at")
			}
			for _, want := range []string{"issue=1058", "reason=held", "label=" + hold} {
				if !strings.Contains(details, want) {
					t.Fatalf("the skip must carry %q, got %q", want, details)
				}
			}
		})
	}
}

// TestTriageRunner_TheBindingsBlockedLabelIsAHoldToo: the module's own
// parked-for-a-person label is skipped with the same audit.
func TestTriageRunner_TheBindingsBlockedLabelIsAHoldToo(t *testing.T) {
	rig := newTriageRig(t)
	blocked := rig.binding.Forge.Label(LabelBlocked)
	if blocked == "" {
		t.Fatal("the test binding displays no blocked label; the fixture no longer exercises this")
	}
	rig.forge.putIssue(77, "parked for a person", blocked)
	rig.forge.putIssue(78, "fresh and untriaged")
	rig.answers("Scoped and verifiable.\n\nSDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("only the unheld issue is judged, got %d prompts", len(prompts))
	}
	if rig.hasLabel(77, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("a parked issue must never gain the trigger, got %v", rig.labels(77))
	}
	details, ok := rig.auditDetails("sdlc:triage_skipped")
	if !ok || !strings.Contains(details, "issue=77") || !strings.Contains(details, "reason=held") {
		t.Fatalf("the skip must be audited as held, got %q", details)
	}
}

// TestTriageRunner_AnEscalatedIssueIsAHoldToo: escalated names a person as
// the next step, so neither a new comment nor a moved head brings the issue
// back to triage — only a person removing the label does.
func TestTriageRunner_AnEscalatedIssueIsAHoldToo(t *testing.T) {
	rig := newTriageRig(t)
	// The runner reloads the binding from the store, so the store is what
	// must declare the label.
	rig.binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	if err := rig.store.PutBinding(context.Background(), testTenant, rig.binding); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	escalated := rig.binding.Forge.Label(LabelEscalated)
	rig.forge.putIssue(90, "needs a ruling", escalated)
	// Somebody spoke after triage did: before #1060 that re-judged the issue.
	rig.forge.putComment(90, "a comment that is not the owner's ruling", time.Now())
	rig.answers("Scoped and verifiable.\n\nSDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("an escalated issue is never judged, got %d prompts", len(prompts))
	}
	if rig.hasLabel(90, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("an escalated issue must never gain the trigger, got %v", rig.labels(90))
	}
	if !rig.hasLabel(90, escalated) {
		t.Fatalf("the escalation must stand, got %v", rig.labels(90))
	}
	details, ok := rig.auditDetails("sdlc:triage_skipped")
	if !ok {
		t.Fatal("an escalated issue passed over must be audited")
	}
	for _, want := range []string{"issue=90", "reason=held", "label=" + escalated} {
		if !strings.Contains(details, want) {
			t.Fatalf("the skip must carry %q, got %q", want, details)
		}
	}
}

// TestApply_RefusesAnIssueEscalatedDuringTheTurn is the writer's refusal for
// the escalated hold: the listed copy was clean, the forge's is escalated.
func TestApply_RefusesAnIssueEscalatedDuringTheTurn(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	listed := ForgeIssue{Number: 91, State: "open"}
	forge.issues[91] = &ForgeIssue{Number: 91, State: "open", Labels: []string{"factory:escalated"}}

	err := triageRunFor(forge, binding).apply(context.Background(), listed,
		ClassifyTriage(TriageDeclaration{Code: "ready", Declared: true}), "evidence")
	var held *TriageHeldError
	if !errors.As(err, &held) || held.Label != "factory:escalated" {
		t.Fatalf("want TriageHeldError naming the escalated label, got %T: %v", err, err)
	}
	if len(forge.labelAdds) != 0 || len(forge.comments[91]) != 0 {
		t.Fatalf("nothing is written to an escalated issue: labels=%v comments=%d",
			forge.labelAdds, len(forge.comments[91]))
	}
}

// TestTriageRunner_AHoldAppliedDuringTheTurnWins is the verdict writer's own
// refusal: the issue was unheld when listed, a person held it while the model
// was reading, and the ready verdict that came back must not relabel it.
func TestTriageRunner_AHoldAppliedDuringTheTurnWins(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(1058, "lead-driven work")
	rig.agents.mu.Lock()
	rig.agents.replyFn = func(int) *orchestration.PromptSessionResponse {
		rig.forge.mu.Lock()
		issue := rig.forge.issues[1058]
		issue.Labels = append(issue.Labels, HoldLabel)
		rig.forge.mu.Unlock()
		return workerSaid("Scoped and verifiable.\n\nSDLC-TRIAGE: ready")
	}
	rig.agents.mu.Unlock()

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rig.hasLabel(1058, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("a verdict must never relabel a held issue, got %v", rig.labels(1058))
	}
	if !rig.hasLabel(1058, HoldLabel) {
		t.Fatalf("the hold must stand, got %v", rig.labels(1058))
	}
	if _, failed := rig.auditDetails("sdlc:triage_failed"); failed {
		t.Fatal("a held issue is not a failed triage; it is an answered one")
	}
	details, ok := rig.auditDetails("sdlc:triage_skipped")
	if !ok || !strings.Contains(details, "reason=held") {
		t.Fatalf("the refused verdict must be audited as held, got %q", details)
	}
}

// TestApply_RefusesEveryDispositionOnAHeldIssue is the defence in depth at
// the writer itself: whatever the verdict, a held issue is not written to.
func TestApply_RefusesEveryDispositionOnAHeldIssue(t *testing.T) {
	verdicts := []TriageVerdict{
		ClassifyTriage(TriageDeclaration{Code: "ready", Declared: true}),
		ClassifyTriage(TriageDeclaration{Code: "needs-info", Declared: true}),
		ClassifyTriage(TriageDeclaration{Code: "not-actionable", Declared: true}),
	}
	for _, verdict := range verdicts {
		t.Run(verdict.Outcome.String(), func(t *testing.T) {
			forge := newFakeForge()
			binding := testBinding()
			binding.TriageMayClose = true
			issue := ForgeIssue{Number: 1058, State: "open", Labels: []string{HoldLabel}}
			forge.issues[1058] = &issue

			err := triageRunFor(forge, binding).apply(context.Background(), issue, verdict, "evidence")
			var held *TriageHeldError
			if !errors.As(err, &held) || !errors.Is(err, ErrTriageHeld) {
				t.Fatalf("want TriageHeldError, got %T: %v", err, err)
			}
			if held.Issue != 1058 || held.Label != HoldLabel {
				t.Fatalf("the refusal must name the issue and the hold, got %+v", held)
			}
			if len(forge.labelAdds) != 0 || len(forge.comments[1058]) != 0 || len(forge.closed) != 0 {
				t.Fatalf("nothing is written to a held issue: labels=%v comments=%d closed=%v",
					forge.labelAdds, len(forge.comments[1058]), forge.closed)
			}
		})
	}
}

// TestApply_AnIssueThatCannotBeReReadIsNotWritten: a verdict that cannot
// prove the issue is unheld is not applied.
func TestApply_AnIssueThatCannotBeReReadIsNotWritten(t *testing.T) {
	forge := newFakeForge()
	forge.getIssueErr = errors.New("forge down")
	binding := testBinding()
	issue := ForgeIssue{Number: 5, State: "open"}
	forge.issues[5] = &issue

	err := triageRunFor(forge, binding).apply(context.Background(), issue,
		ClassifyTriage(TriageDeclaration{Code: "ready", Declared: true}), "")
	var verdictErr *TriageVerdictError
	if !errors.As(err, &verdictErr) || verdictErr.Issue != 5 {
		t.Fatalf("want a TriageVerdictError naming the issue, got %T: %v", err, err)
	}
	if len(forge.labelAdds) != 0 {
		t.Fatalf("an unproven issue is not labelled, got %v", forge.labelAdds)
	}
}

// TestIssueSource_AHeldIssueIsNotAdoptedEvenWhenTriggered is adoption: an
// issue carrying both the trigger and the hold is not offered, and its
// unheld neighbour still is.
func TestIssueSource_AHeldIssueIsNotAdoptedEvenWhenTriggered(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(1058, "lead-driven work", "task:ready", HoldLabel)
	forge.putIssue(1059, "factory work", "task:ready")

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Source.Ref != "acme/api#1059" {
		t.Fatalf("specs = %+v, want only the unheld issue", specs)
	}
}

// TestReadiness_AHeldIssueIsNotAdoptable pins the verdict itself.
func TestReadiness_AHeldIssueIsNotAdoptable(t *testing.T) {
	binding := testBinding()
	scope := ReadinessScope{Binding: binding, Forge: newFakeForge(), Repo: binding.Repo()}
	issue := &ForgeIssue{Number: 3, State: "open", Labels: []string{binding.Forge.TriggerLabel, " TASK:BLOCKED "}}

	finding, err := NewReadinessEvaluator(nil).Evaluate(context.Background(), scope, issue)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finding.Verdict != ReadinessHeld || finding.Verdict.Adoptable() {
		t.Fatalf("verdict = %s (adoptable=%v), want held and not adoptable", finding.Verdict, finding.Verdict.Adoptable())
	}
}

// TestHeldIn_ReturnsTheLabelInItsOwnSpelling proves a hold matches ignoring
// case and surrounding space, and comes back as the forge spells it, which is
// what a removal must send.
func TestHeldIn_ReturnsTheLabelInItsOwnSpelling(t *testing.T) {
	if got := heldIn([]string{"bug", " Factory:Parked "}, "factory:parked"); got != " Factory:Parked " {
		t.Errorf("heldIn = %q, want the forge's spelling", got)
	}
	if got := heldBy(&ForgeIssue{Labels: []string{"Task:Blocked"}}, HoldLabel); got != "Task:Blocked" {
		t.Errorf("heldBy = %q, want Task:Blocked", got)
	}
}

// TestHeldIn_NothingHeldIsEmpty proves a missing label, an empty hold and a
// missing issue each hold nothing.
func TestHeldIn_NothingHeldIsEmpty(t *testing.T) {
	if got := heldIn([]string{"bug"}, "factory:parked"); got != "" {
		t.Errorf("unmatched = %q, want empty", got)
	}
	if got := heldIn([]string{"bug"}, "  "); got != "" {
		t.Errorf("empty hold = %q, want empty", got)
	}
	if got := heldBy(nil, HoldLabel); got != "" {
		t.Errorf("nil issue = %q, want empty", got)
	}
}
