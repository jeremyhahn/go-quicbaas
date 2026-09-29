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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// reviewRig is a ReviewValidator over a fake forge with a delivery whose
// pull request is open at head "h1".
type reviewRig struct {
	validator *ReviewValidator
	forge     *fakeForge
	store     *Store
	criterion orchestration.ReviewCriterion
}

func newReviewRig(t *testing.T) *reviewRig {
	t.Helper()
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.PutBinding(ctx, testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.prs[100] = &ForgePullRequest{Number: 100, Title: "Fix", State: "open", HeadSHA: "h1", HeadRef: "issue/25-fix", BaseRef: "main", URL: "http://forge.test/acme/api/pulls/100"}
	if err := store.PutDelivery(ctx, testTenant, &Delivery{
		TaskID: testTask, ProjectID: testProject, Attempt: 1, Branch: "issue/25-fix", Base: "main", HeadSHA: "h1",
		PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestOpen, URL: "http://forge.test/acme/api/pulls/100"},
	}); err != nil {
		t.Fatal(err)
	}
	validator, err := NewReviewValidator(store, store, &fakeForges{forge: forge}, testAgentCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return &reviewRig{validator: validator, forge: forge, store: store,
		criterion: orchestration.ReviewCriterion{Name: "forge_review", Validator: ValidatorKindForgeReview, Required: true, Phase: orchestration.CriterionPhaseReviewing}}
}

func (r *reviewRig) approveBy(reviewer, head string, id int64) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.reviews[100] = append(r.forge.reviews[100], ForgeReview{ID: id, Reviewer: reviewer, State: "APPROVED", HeadSHA: head, Submitted: time.Now()})
}

func (r *reviewRig) greenChecks(head string) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.checks[head] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: head, Status: "completed", Conclusion: "success", Completed: time.Now()}}
}

func TestReviewValidator_PassesOnApprovalAndGreenChecksOnTheCurrentHead(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.greenChecks("h1")
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if !strings.Contains(result.Message, "approved on h1") || result.Details != string(GateClear) {
		t.Fatalf("message = %q details = %q", result.Message, result.Details)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil || len(delivery.Reviews) != 1 || delivery.Reviews[0].Round != 1 || delivery.Reviews[0].ModelFamily != ModelFamilyOpenAI || len(delivery.Checks) != 1 {
		t.Fatalf("delivery after review: %+v, %v", delivery, err)
	}
}

func TestReviewValidator_StaleHeadVerdictIsIgnoredUntilTheHeadIsApproved(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h0", 1) // an earlier head
	rig.greenChecks("h1")
	// The validator waits; once the current head is approved it passes.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			rig.forge.mu.Lock()
			polled := rig.forge.listReviews
			rig.forge.mu.Unlock()
			if polled >= 2 {
				rig.approveBy("codex", "h1", 2)
				return
			}
			time.Sleep(0) // yield; the wait itself is the validator's poll interval
		}
	}()
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	wg.Wait()
	if err != nil || !result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	delivery, _ := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if len(delivery.Reviews) != 2 {
		t.Fatalf("both verdicts recorded once each: %+v", delivery.Reviews)
	}
}

func TestReviewValidator_RequestChangesFailsWithFindings(t *testing.T) {
	rig := newReviewRig(t)
	rig.forge.reviews[100] = []ForgeReview{{ID: 3, Reviewer: "codex", State: "REQUEST_CHANGES", Body: "rename the thing", HeadSHA: "h1", Submitted: time.Now()}}
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if !strings.Contains(result.Message, "rename the thing") || result.Details != string(GateBlockedChangesRequested) {
		t.Fatalf("findings not delivered: %+v", result)
	}
}

func TestReviewValidator_IndependentReviewerRule(t *testing.T) {
	rig := newReviewRig(t)
	rig.greenChecks("h1")
	rig.approveBy("claude-bot", "h1", 1) // the author's own family: excluded
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			rig.forge.mu.Lock()
			polled := rig.forge.listReviews
			rig.forge.mu.Unlock()
			if polled >= 2 {
				cancel()
				return
			}
			time.Sleep(0)
		}
	}()
	_, err := rig.validator.Validate(ctx, testJob(1), rig.criterion)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a non-independent approval must keep the validator waiting; got %v", err)
	}
	// An independent approval clears it.
	rig.approveBy("codex", "h1", 2)
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("with an independent approval: %+v, %v", result, err)
	}
	// Without an author family the independence rule cannot be decided. The
	// family is the BINDING's worker now — the canonical sdlc workflow whose
	// worker role once carried it is gone — so a binding naming a backend no
	// family is known for is what leaves the author unknown.
	binding := testBinding()
	binding.Worker = orchestration.AgentRefTo(testMysteryAgent)
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("author unknown must be a configuration error: %v", err)
	}
}

// conflictingDeliveries refuses the first guarded write after moving the
// stored record, so the validator must re-read and merge again.
type conflictingDeliveries struct {
	*Store
	once sync.Once
	t    *testing.T
}

func (c *conflictingDeliveries) UpdateDelivery(ctx context.Context, tenantID string, delivery *Delivery) error {
	c.once.Do(func() {
		other, err := c.Store.GetDelivery(ctx, tenantID, delivery.TaskID)
		if err != nil {
			c.t.Fatal(err)
		}
		other.Checks = append(other.Checks, CheckRecord{Name: "someone-else", HeadSHA: "h1", Status: CheckSuccess})
		if err := c.Store.UpdateDelivery(ctx, tenantID, other); err != nil {
			c.t.Fatal(err)
		}
	})
	return c.Store.UpdateDelivery(ctx, tenantID, delivery)
}

func TestReviewValidator_RecordsTheRoundOnceUnderAConflict(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.greenChecks("h1")
	validator, err := NewReviewValidator(&conflictingDeliveries{Store: rig.store, t: t}, rig.store, &fakeForges{forge: rig.forge}, testAgentCatalog())
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	delivery, _ := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if len(delivery.Reviews) != 1 {
		t.Fatalf("the verdict was recorded %d times under a conflict", len(delivery.Reviews))
	}
	if delivery.Version != 3 {
		t.Fatalf("version = %d, want 3 (put, the concurrent write, the review's one write)", delivery.Version)
	}
}

func TestReviewValidator_SettledRedChecksFail(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.forge.checks["h1"] = []ForgeCheckRun{{ID: "1", Name: "test", HeadSHA: "h1", Status: "completed", Conclusion: "failure", Completed: time.Now().Add(-time.Hour)}}
	rig.forge.logs["test"] = "2026-09-05T00:00:00Z FAIL: TestThing"
	rig.validator.now = func() time.Time { return time.Now() }
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if !strings.Contains(result.Message, "FAILED: test") || !strings.Contains(result.Message, "FAIL: TestThing") {
		t.Fatalf("findings = %q", result.Message)
	}
	// A red inside the settle window is still pending.
	rig.forge.checks["h1"][0].Completed = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rig.validator.Validate(ctx, testJob(1), rig.criterion); !errors.Is(err, context.Canceled) {
		t.Fatalf("fresh red must wait: %v", err)
	}
}

func TestReviewValidator_PullRequestStates(t *testing.T) {
	rig := newReviewRig(t)
	rig.forge.prs[100].Merged = true
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed || !strings.Contains(result.Message, "already merged") {
		t.Fatalf("merged: %+v, %v", result, err)
	}
	delivery, _ := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if delivery.PullRequest.State != PullRequestMerged {
		t.Fatalf("state not recorded: %+v", delivery.PullRequest)
	}
	rig.forge.prs[100].Merged = false
	rig.forge.prs[100].State = "closed"
	result, err = rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if !errors.Is(err, ErrPullRequestClosedUnmerged) || result.Passed {
		t.Fatalf("closed: %+v, %v", result, err)
	}
	// The forge's head moves the delivery's head.
	rig.forge.prs[100].State = "open"
	rig.forge.prs[100].HeadSHA = "h2"
	rig.approveBy("codex", "h2", 9)
	rig.greenChecks("h2")
	result, err = rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("moved head: %+v, %v", result, err)
	}
	delivery, _ = rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if delivery.HeadSHA != "h2" {
		t.Fatalf("head not followed: %q", delivery.HeadSHA)
	}
}

func TestReviewValidator_Refusals(t *testing.T) {
	rig := newReviewRig(t)
	if _, err := rig.validator.Validate(context.Background(), nil, rig.criterion); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil job: %v", err)
	}
	unbound := testJob(1)
	unbound.TaskID = ""
	if _, err := rig.validator.Validate(context.Background(), unbound, rig.criterion); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unbound job: %v", err)
	}
	noPR := testJob(1)
	noPR.TaskID = "task-2"
	if err := rig.store.PutDelivery(context.Background(), testTenant, &Delivery{TaskID: "task-2", Attempt: 1, Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.validator.Validate(context.Background(), noPR, rig.criterion); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no pull request: %v", err)
	}
	missing := testJob(1)
	missing.TaskID = "task-3"
	if _, err := rig.validator.Validate(context.Background(), missing, rig.criterion); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no delivery: %v", err)
	}
	gone := testJob(1)
	gone.ProjectID = "unbound"
	if _, err := rig.validator.Validate(context.Background(), gone, rig.criterion); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no binding: %v", err)
	}
	delete(rig.forge.prs, 100)
	if _, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pull request gone from the forge: %v", err)
	}
	for name, args := range map[string][3]interface{}{
		"deliveries": {nil, rig.store, &fakeForges{}},
		"bindings":   {rig.store, nil, &fakeForges{}},
		"forges":     {rig.store, rig.store, nil},
	} {
		var deliveries DeliveryStore
		var bindings BindingReader
		var forges ForgeResolver
		if args[0] != nil {
			deliveries = args[0].(DeliveryStore)
		}
		if args[1] != nil {
			bindings = args[1].(BindingReader)
		}
		if args[2] != nil {
			forges = args[2].(ForgeResolver)
		}
		if _, err := NewReviewValidator(deliveries, bindings, forges, testAgentCatalog()); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s nil accepted: %v", name, err)
		}
	}
	if _, err := NewReviewValidator(rig.store, rig.store, &fakeForges{}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil agents accepted: %v", err)
	}
	nilForge, _ := NewReviewValidator(rig.store, rig.store, &fakeForges{}, testAgentCatalog())
	if _, err := nilForge.Validate(context.Background(), testJob(1), rig.criterion); !errors.Is(err, ErrForgeNotRegistered) {
		t.Fatalf("nil forge: %v", err)
	}
}

func TestReviewPollInterval(t *testing.T) {
	if reviewPollInterval(nil) != DefaultReviewPollInterval {
		t.Fatal("nil workflow")
	}
	wf := &orchestration.WorkflowDefinition{Settings: orchestration.WorkflowSettings{PollInterval: "bogus"}}
	if reviewPollInterval(wf) != DefaultReviewPollInterval {
		t.Fatal("bogus interval")
	}
	wf.Settings.PollInterval = "250ms"
	if reviewPollInterval(wf) != 250*time.Millisecond {
		t.Fatal("explicit interval")
	}
}

// =============================================================================
// A red pipeline: who it reaches (go-quicbaas#898)
// =============================================================================

// round drives exactly ONE round of the validator.
//
// Validate loops on the workflow's poll interval, and every path below is
// deliberately NOT decisive — that is the point of them: a void red keeps the
// job waiting instead of waking an agent. Driving the round directly is what
// lets those paths be asserted without waiting out a 30-second timer or
// cancelling a context the round itself needs.
func (r *reviewRig) round(t *testing.T) (orchestration.ValidationResult, bool, error) {
	t.Helper()
	ctx := context.Background()
	binding, err := r.store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return r.validator.round(ctx, testJob(1), r.criterion, binding, r.forge,
		bindingAgentsFrom(testAgents(), binding))
}

// redCheck settles one red check on a head, with the log the forge will serve
// for it. Completed an hour ago, so the settle window is already past.
func (r *reviewRig) redCheck(head, name, log string) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.checks[head] = append(r.forge.checks[head], ForgeCheckRun{
		ID: name + "-1", Name: name, HeadSHA: head, Status: "completed", Conclusion: "failure",
		Completed: time.Now().Add(-time.Hour),
	})
	r.forge.logs[name] = log
}

// TestReviewValidator_VoidRedIsRerunAndNeverReachesTheAgent is the change this
// issue exists for. A runner that died holding the job used to be returned
// decisive, sending the job to re-delegation and paying a coding agent to
// author another attempt at a change whose tests never ran.
func TestReviewValidator_VoidRedIsRerunAndNeverReachesTheAgent(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)

	result, decisive, err := rig.round(t)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	if decisive {
		t.Fatalf("a void red was decisive; the agent would be woken for a runner fault: %+v", result)
	}
	if result.Passed {
		t.Fatal("a void red must not pass the gate either; the change is still unverified")
	}
	if !strings.Contains(result.Message, "re-run automatically") {
		t.Errorf("message = %q, want it to say the check is being re-run", result.Message)
	}

	reruns := rig.forge.rerunCalls()
	if len(reruns) != 1 || reruns[0].Name != "test" {
		t.Fatalf("re-runs asked for = %+v, want exactly the void check", reruns)
	}

	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatalf("delivery: %v", err)
	}
	if got := delivery.RerunAttempts("h1", "test"); got != 1 {
		t.Fatalf("the re-run was not recorded durably: attempts = %d", got)
	}
	if delivery.CIReruns[0].Signature != "runner_died" {
		t.Errorf("signature = %q, want runner_died", delivery.CIReruns[0].Signature)
	}
}

// TestReviewValidator_APendingRerunIsNotAskedTwice covers the second round over
// the same unchanged result: the forge has been told and has not answered, so
// nothing more is asked and no budget is spent.
func TestReviewValidator_APendingRerunIsNotAskedTwice(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)

	for i := 0; i < 3; i++ {
		if _, decisive, err := rig.round(t); err != nil || decisive {
			t.Fatalf("round %d: decisive=%v err=%v", i, decisive, err)
		}
	}
	if got := len(rig.forge.rerunCalls()); got != 1 {
		t.Fatalf("the forge was asked %d times for one unanswered result; the budget would be gone in three polls", got)
	}
}

// TestReviewValidator_VoidRedEscalatesWhenTheBudgetIsSpent is the bound that
// makes the void verdict safe. A host that keeps dying answers void every
// time, and without this the engine would re-run it forever and tell nobody.
func TestReviewValidator_VoidRedEscalatesWhenTheBudgetIsSpent(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)

	// Every ask is already spent, and all of them predate this result, so none
	// of them is pending.
	spent := make([]CIRerunRecord, 0, DefaultChecksRerunBudget)
	for i := 0; i < DefaultChecksRerunBudget; i++ {
		spent = append(spent, CIRerunRecord{
			HeadSHA: "h1", Check: "test", Signature: "runner_died", At: time.Unix(int64(i+1), 0),
		})
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.CIReruns = spent
	if err := rig.store.UpdateDelivery(context.Background(), testTenant, delivery); err != nil {
		t.Fatal(err)
	}

	_, decisive, err := rig.round(t)
	if decisive {
		t.Fatal("a spent budget must not be decisive; the agent is still not the one to tell")
	}
	var defect *CIHostDefectError
	if !errors.As(err, &defect) {
		t.Fatalf("err = %v, want a *CIHostDefectError", err)
	}
	if !strings.Contains(defect.ParkReason(), "re-run automatically 3 times") {
		t.Errorf("park reason = %q, want it to say how many times the fleet was asked", defect.ParkReason())
	}
	if got := len(rig.forge.rerunCalls()); got != 0 {
		t.Errorf("the forge was asked %d more times after the budget was spent", got)
	}
}

// TestReviewValidator_HostDefectParksForAnOperator covers the verdict no
// change could fix.
func TestReviewValidator_HostDefectParksForAnOperator(t *testing.T) {
	for name, log := range map[string]string{
		"an unreachable registry":                logRegistryUnreachable,
		"a container the runtime refused":        logHostDefect,
		"a private module nothing could resolve": logModuleResolution,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReviewRig(t)
			rig.approveBy("codex", "h1", 1)
			rig.redCheck("h1", "lint", log)

			_, decisive, err := rig.round(t)
			if decisive {
				t.Fatal("a host defect was decisive; the agent would be sent to fix a machine")
			}
			var defect *CIHostDefectError
			if !errors.As(err, &defect) {
				t.Fatalf("err = %v, want a *CIHostDefectError", err)
			}
			if defect.HeadSHA != "h1" {
				t.Errorf("HeadSHA = %q", defect.HeadSHA)
			}
			if !strings.Contains(defect.ParkReason(), "lint") {
				t.Errorf("park reason does not name the check:\n%s", defect.ParkReason())
			}
			if got := len(rig.forge.rerunCalls()); got != 0 {
				t.Errorf("a host defect was re-run %d times; it will not fix itself by being asked again", got)
			}
		})
	}
}

// TestReviewValidator_VoidRedEscalatesWhenTheForgeCannotRerun covers the
// platform that has no rerun route. Waiting would be waiting for something
// nobody will ever trigger, so an operator is told instead.
func TestReviewValidator_VoidRedEscalatesWhenTheForgeCannotRerun(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)
	rig.forge.caps.RerunCheckRuns = false

	_, decisive, err := rig.round(t)
	if decisive {
		t.Fatal("decisive")
	}
	var defect *CIHostDefectError
	if !errors.As(err, &defect) {
		t.Fatalf("err = %v, want a *CIHostDefectError", err)
	}
	if !strings.Contains(defect.ParkReason(), "cannot re-run") {
		t.Errorf("park reason = %q", defect.ParkReason())
	}
}

// TestReviewValidator_ARefusedRerunStillSpendsBudget covers the third way the
// ask can stop being worth making: the route exists and fails. The attempt is
// recorded so a broken route escalates rather than being retried forever, and
// it does NOT read as pending, because nothing was restarted to wait for.
func TestReviewValidator_ARefusedRerunStillSpendsBudget(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)
	rig.forge.rerunErr = errors.New("the forge said no")

	for i := 0; i < DefaultChecksRerunBudget; i++ {
		if _, decisive, err := rig.round(t); err != nil || decisive {
			t.Fatalf("round %d: decisive=%v err=%v", i, decisive, err)
		}
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if got := delivery.RerunAttempts("h1", "test"); got != DefaultChecksRerunBudget {
		t.Fatalf("attempts = %d, want the whole budget spent", got)
	}
	if delivery.CIReruns[0].Failure == "" {
		t.Error("a refused re-run was recorded as if it had landed")
	}

	_, _, err = rig.round(t)
	var defect *CIHostDefectError
	if !errors.As(err, &defect) {
		t.Fatalf("err = %v, want the spent budget to escalate", err)
	}
}

// TestReviewValidator_AGenuineRedAmongFaultsStillReachesTheAgent is the
// dangerous direction at the seam. A day when the fleet is unwell is exactly
// when a real test failure is easiest to lose.
func TestReviewValidator_AGenuineRedAmongFaultsStillReachesTheAgent(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "build", logGenuine)
	rig.redCheck("h1", "test", logRunnerDied)
	rig.redCheck("h1", "security", logHostDefect)

	result, decisive, err := rig.round(t)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	if !decisive || result.Passed {
		t.Fatalf("a real test failure must be decisive and failing: decisive=%v result=%+v", decisive, result)
	}
	if !strings.Contains(result.Message, "FAILED: build") {
		t.Errorf("the worker was not told which check is its own:\n%s", result.Message)
	}
	if !strings.Contains(result.Message, "test (runner_died)") ||
		!strings.Contains(result.Message, "security (container_create_failed)") {
		t.Errorf("the worker was not told which reds are not its own:\n%s", result.Message)
	}
	if got := len(rig.forge.rerunCalls()); got != 0 {
		t.Errorf("checks were re-run alongside a genuine failure (%d); the change has to be fixed first", got)
	}
}

// TestReviewValidator_AnUnsettledRedIsStillWaitedOut proves the settle window
// survives the classifier. It is not a root-cause rule and the classifier does
// not replace it: it is what stops a red being READ while a rerun is in
// flight, and reading one early would classify a half-written log.
func TestReviewValidator_AnUnsettledRedIsStillWaitedOut(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.redCheck("h1", "test", logRunnerDied)
	rig.forge.mu.Lock()
	rig.forge.checks["h1"][0].Completed = time.Now()
	rig.forge.mu.Unlock()

	result, decisive, err := rig.round(t)
	if err != nil || decisive {
		t.Fatalf("a fresh red must simply wait: decisive=%v err=%v", decisive, err)
	}
	if strings.Contains(result.Message, "re-run") {
		t.Errorf("an unsettled red was re-run: %q", result.Message)
	}
	if got := len(rig.forge.rerunCalls()); got != 0 {
		t.Errorf("the forge was asked to re-run a check that had not settled (%d asks)", got)
	}
}

// alwaysConflictingDeliveries is a delivery store whose writes NEVER win the
// optimistic-concurrency check, for the bounded-retry path.
type alwaysConflictingDeliveries struct {
	DeliveryStore
	writes int
}

func (c *alwaysConflictingDeliveries) UpdateDelivery(context.Context, string, *Delivery) error {
	c.writes++
	return ErrVersionConflict
}

// TestReviewValidator_PersistRerunsSurvivesAConflict covers the guarded write
// the budget's durability rests on. Two writers hold the same delivery; the
// loser must re-read, re-apply its own record, and end with BOTH recorded
// exactly once — a count applied twice escalates a healthy fleet, and one
// dropped bounds nothing.
func TestReviewValidator_PersistRerunsSurvivesAConflict(t *testing.T) {
	rig := newReviewRig(t)
	ctx := context.Background()

	mine, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	theirs.RecordRerun(CIRerunRecord{HeadSHA: "h1", Check: "lint", Signature: "runner_died", At: time.Unix(1, 0)})
	if err := rig.store.UpdateDelivery(ctx, testTenant, theirs); err != nil {
		t.Fatalf("the other writer: %v", err)
	}

	mine.RecordRerun(CIRerunRecord{HeadSHA: "h1", Check: "test", Signature: "runner_died", At: time.Unix(2, 0)})
	if err := rig.validator.persistReruns(ctx, testJob(1), mine); err != nil {
		t.Fatalf("persistReruns lost to a conflict it should have retried: %v", err)
	}

	stored, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.RerunAttempts("h1", "lint"); got != 1 {
		t.Errorf("the other writer's record: attempts = %d, want 1", got)
	}
	if got := stored.RerunAttempts("h1", "test"); got != 1 {
		t.Errorf("this writer's record: attempts = %d, want 1", got)
	}
}

// TestReviewValidator_PersistRerunsGivesUpBounded covers the bad-input half: a
// store that never accepts a write must not spin. The failure is reported so
// the round errors rather than pretending the budget was recorded.
func TestReviewValidator_PersistRerunsGivesUpBounded(t *testing.T) {
	rig := newReviewRig(t)
	ctx := context.Background()
	store := &alwaysConflictingDeliveries{DeliveryStore: rig.store}
	rig.validator.deliveries = store

	delivery, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.RecordRerun(CIRerunRecord{HeadSHA: "h1", Check: "test", Signature: "runner_died", At: time.Unix(2, 0)})

	if err := rig.validator.persistReruns(ctx, testJob(1), delivery); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict once the retries are spent", err)
	}
	if store.writes != deliveryWriteAttempts {
		t.Fatalf("attempted %d writes, want exactly %d", store.writes, deliveryWriteAttempts)
	}
}

// TestReviewValidator_ACancelledCheckIsReRunRatherThanWaitedOut covers a hole
// the classifier closed on the way past.
//
// evaluateChecks blocks the gate on a cancelled check — it refuses every status
// that is not success — but evaluateChecksFailure only ever collected outright
// failures, so a head whose only red was a cancellation NEVER settled and the
// job sat in reviewing until the stall detector noticed. It now settles, and a
// cancellation classifies as void: an act from outside the tests, never their
// verdict on the change. So it is re-run, and no agent is woken.
func TestReviewValidator_ACancelledCheckIsReRunRatherThanWaitedOut(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	// A cancelled check settles the head with no log to read, which is the
	// shape that reaches the re-run path with nothing for the forge to restart.
	rig.forge.mu.Lock()
	rig.forge.checks["h1"] = []ForgeCheckRun{{
		ID: "1", Name: "test", HeadSHA: "h1", Status: "completed", Conclusion: "cancelled",
		Completed: time.Now().Add(-time.Hour),
	}}
	rig.forge.mu.Unlock()

	result, decisive, err := rig.round(t)
	if err != nil || decisive {
		t.Fatalf("a cancelled check must be re-run, not decided: decisive=%v err=%v result=%+v", decisive, err, result)
	}
	reruns := rig.forge.rerunCalls()
	if len(reruns) != 1 || reruns[0].Name != "test" {
		t.Fatalf("re-runs = %+v, want the cancelled check", reruns)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CIReruns[0].Signature != "cancelled" {
		t.Errorf("signature = %q, want cancelled", delivery.CIReruns[0].Signature)
	}
}

// forgeRejected is the forge answer that ended four review waits at once on
// 2026-09-28 (20:03:18 EDT): a single 401 on a poll's GetPullRequest, with
// the same credential reading fine before and after.
func forgeRejected() error {
	return backendError("GetPullRequest", &GiteaAPIError{Status: 401, Operation: "GetPullRequest", Message: "invalid username, password or token"})
}

// TestReviewValidator_AFailedReadIsPolledAgain pins that a review wait
// survives forge reads that fail and then recover. The wait is a poll, and
// the next tick is a free retry: go-blockstore#74's delivery had waited
// 8h52m when one failed read ended it and charged the task an attempt.
func TestReviewValidator_AFailedReadIsPolledAgain(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	rig.greenChecks("h1")
	rig.forge.getPRErrs = []error{forgeRejected(), forgeRejected(), forgeRejected()}

	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("Validate after three failed reads = %+v, %v; want the wait to go on and pass", result, err)
	}
}

// TestReviewValidator_AFailedReadEndsTheWaitAtTheBound is the other side: a
// forge that keeps failing (a revoked credential) still ends the wait, after
// reviewReadFailureBound consecutive rounds, with the forge's own error so
// the engine attributes it exactly as before.
func TestReviewValidator_AFailedReadEndsTheWaitAtTheBound(t *testing.T) {
	rig := newReviewRig(t)
	failures := make([]error, reviewReadFailureBound+1)
	for i := range failures {
		failures[i] = forgeRejected()
	}
	rig.forge.getPRErrs = failures

	_, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	var apiErr *GiteaAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("Validate against a forge that keeps failing = %v; want its 401", err)
	}
	var wrapped *reviewReadError
	if errors.As(err, &wrapped) {
		t.Fatalf("Validate leaked the poll's internal wrapper: %v", err)
	}
	rig.forge.mu.Lock()
	left := len(rig.forge.getPRErrs)
	rig.forge.mu.Unlock()
	if left != 1 {
		t.Fatalf("reads attempted = %d, want exactly reviewReadFailureBound (%d)", len(failures)-left, reviewReadFailureBound)
	}
}

// TestReviewValidator_ASuccessfulReadResetsTheBound pins "consecutive": a
// read that succeeds between failures starts the count again, so a flaky
// forge over a long wait never adds up to a failed review.
func TestReviewValidator_ASuccessfulReadResetsTheBound(t *testing.T) {
	rig := newReviewRig(t)
	reads := 0
	rig.forge.onGetPullRequest = func(*ForgePullRequest) {
		reads++
		if reads == 1 {
			// Refill the failures after the first good read: the total
			// exceeds the bound, but no run of them reaches it.
			for range reviewReadFailureBound - 1 {
				rig.forge.getPRErrs = append(rig.forge.getPRErrs, forgeRejected())
			}
		}
		if reads == 2 {
			rig.forge.reviews[100] = append(rig.forge.reviews[100], ForgeReview{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: "h1", Submitted: time.Now()})
			rig.forge.checks["h1"] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: "h1", Status: "completed", Conclusion: "success", Completed: time.Now()}}
		}
	}
	for range reviewReadFailureBound - 1 {
		rig.forge.getPRErrs = append(rig.forge.getPRErrs, forgeRejected())
	}

	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("Validate = %+v, %v; want two runs of failures under the bound to be absorbed", result, err)
	}
}

// TestReviewValidator_ACancelledWaitIsNotRetried is the bad-input side: a
// read that failed because the job was cancelled ends the wait at once.
func TestReviewValidator_ACancelledWaitIsNotRetried(t *testing.T) {
	rig := newReviewRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rig.forge.getPRErrs = []error{context.Canceled}

	if _, err := rig.validator.Validate(ctx, testJob(1), rig.criterion); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate on a cancelled job = %v, want context.Canceled", err)
	}
}
