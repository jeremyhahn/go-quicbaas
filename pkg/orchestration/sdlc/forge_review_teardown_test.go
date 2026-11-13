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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// A red the runner caused tearing its own container down (go-quicbaas#1011)
// =============================================================================

// auditLog records what the gate put on the job's audit trail.
type auditLog struct {
	mu     sync.Mutex
	events []string
}

func (a *auditLog) record(eventType, details string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, eventType+" "+details)
}

func (a *auditLog) of(eventType string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, event := range a.events {
		if strings.HasPrefix(event, eventType+" ") {
			out = append(out, event)
		}
	}
	return out
}

// auditedRound drives one round with the job's audit sink in the context, as
// the runner's review step does.
func (r *reviewRig) auditedRound(t *testing.T, audit *auditLog) (bool, error) {
	t.Helper()
	ctx := withCIRerunAudit(context.Background(), audit.record)
	binding, err := r.store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	_, decisive, err := r.validator.round(ctx, testJob(1), r.criterion, binding, r.forge,
		bindingAgentsFrom(testAgents(), binding))
	return decisive, err
}

// finishAgain reports the named check red again, finished at the given time —
// the forge's answer to the re-run the gate asked for.
func (r *reviewRig) finishAgain(head, name string, at time.Time) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for i := range r.forge.checks[head] {
		if r.forge.checks[head][i].Name == name {
			r.forge.checks[head][i].Completed = at
		}
	}
}

// TestReviewValidator_RunnerTeardownIsRerunNotAWorkerRound is the go-luks2
// PR #203 case: every step green, the runner failed removing its container,
// and the task went to a worker round and then to the owner. It is re-run
// through the forge instead, and the re-run is on the job's audit trail.
func TestReviewValidator_RunnerTeardownIsRerunNotAWorkerRound(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "CI / Unit Tests", logRunnerTeardown)
	audit := &auditLog{}

	decisive, err := rig.auditedRound(t, audit)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	if decisive {
		t.Fatal("a runner teardown was decisive; a worker round would be spent on the runner's fault")
	}
	reruns := rig.forge.rerunCalls()
	if len(reruns) != 1 || reruns[0].Name != "CI / Unit Tests" {
		t.Fatalf("re-runs asked for = %+v, want exactly the torn-down check", reruns)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivery.CIReruns) != 1 || delivery.CIReruns[0].Signature != "runner_teardown" {
		t.Fatalf("recorded re-runs = %+v, want one runner_teardown", delivery.CIReruns)
	}
	events := audit.of(auditCIRerun)
	if len(events) != 1 {
		t.Fatalf("%s events = %q, want exactly one", auditCIRerun, events)
	}
	for _, want := range []string{"check=CI / Unit Tests", "signature=runner_teardown", "attempt=1/3"} {
		if !strings.Contains(events[0], want) {
			t.Errorf("event %q does not say %q", events[0], want)
		}
	}
}

// TestReviewValidator_AFailedStepBeforeTheTeardownReachesTheWorker is the other
// class: a step that ended "Process completed with exit code 1" is the
// change's failure, and nothing is re-run or audited as a re-run.
func TestReviewValidator_AFailedStepBeforeTheTeardownReachesTheWorker(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "CI / Unit Tests", logStepFailedThenTeardown)
	audit := &auditLog{}

	decisive, err := rig.auditedRound(t, audit)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	if !decisive {
		t.Fatal("a failed step was not decisive; the change's own failure would never reach its author")
	}
	if got := len(rig.forge.rerunCalls()); got != 0 {
		t.Errorf("a genuine failure was re-run %d times", got)
	}
	if events := audit.of(auditCIRerun); len(events) != 0 {
		t.Errorf("a genuine failure was audited as a re-run: %q", events)
	}
}

// TestReviewValidator_RunnerTeardownRerunsAreBounded drives a runner that
// fails its teardown on every attempt: the gate re-runs it exactly the
// budget, audits each, and then parks for an operator rather than looping.
func TestReviewValidator_RunnerTeardownRerunsAreBounded(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	clock := time.Now().UTC()
	rig.validator.now = func() time.Time { return clock }
	rig.redCheck("h1", "CI / Unit Tests", logRunnerTeardown)
	rig.finishAgain("h1", "CI / Unit Tests", clock.Add(-time.Hour))
	audit := &auditLog{}

	for attempt := 1; attempt <= DefaultChecksRerunBudget; attempt++ {
		if decisive, err := rig.auditedRound(t, audit); err != nil || decisive {
			t.Fatalf("attempt %d: decisive=%v err=%v", attempt, decisive, err)
		}
		// The re-run comes back red with the same teardown, after the ask and
		// long enough ago to have settled.
		rig.finishAgain("h1", "CI / Unit Tests", clock.Add(time.Minute))
		clock = clock.Add(2 * time.Hour)
	}

	decisive, err := rig.auditedRound(t, audit)
	if decisive {
		t.Fatal("a spent budget was decisive; the worker is still not the one to tell")
	}
	var defect *CIHostDefectError
	if !errors.As(err, &defect) {
		t.Fatalf("err = %v, want a *CIHostDefectError once the budget is spent", err)
	}
	if got := len(rig.forge.rerunCalls()); got != DefaultChecksRerunBudget {
		t.Errorf("the forge was asked %d times, want exactly the budget of %d", got, DefaultChecksRerunBudget)
	}
	events := audit.of(auditCIRerun)
	if len(events) != DefaultChecksRerunBudget {
		t.Fatalf("%s events = %d, want one per re-run (%d)", auditCIRerun, len(events), DefaultChecksRerunBudget)
	}
	if !strings.Contains(events[len(events)-1], "attempt=3/3") {
		t.Errorf("last event %q does not show the budget spent", events[len(events)-1])
	}
}

// TestAuditRerun_WithoutASinkIsSilent covers a caller that supplied no audit
// trail: the re-run still happens and is recorded on the delivery; there is
// simply no job trail to write to.
func TestAuditRerun_WithoutASinkIsSilent(t *testing.T) {
	auditRerun(context.Background(), CIRerunRecord{Check: "c"}, 1, 3)
	if ctx := withCIRerunAudit(context.Background(), nil); ctx.Value(ciRerunAuditKey{}) != nil {
		t.Fatal("a nil sink was installed")
	}
	audit := &auditLog{}
	auditRerun(withCIRerunAudit(context.Background(), audit.record),
		CIRerunRecord{Check: "c", HeadSHA: "0123456789abcdef", Signature: "runner_lost", Failure: "the forge said no"}, 2, 3)
	events := audit.of(auditCIRerun)
	if len(events) != 1 || !strings.Contains(events[0], "refused=the forge said no") || !strings.Contains(events[0], "attempt=2/3") {
		t.Fatalf("events = %q", events)
	}
}
