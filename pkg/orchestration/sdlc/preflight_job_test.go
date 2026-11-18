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
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakePreflightJobs is the job engine as RunPreflight sees it. Each status
// read takes the next of statuses; the last one repeats.
type fakePreflightJobs struct {
	mu       sync.Mutex
	started  []*orchestration.StartJobRequest
	statuses []*orchestration.JobStatus
	reads    int
	events   chan *orchestration.JobEvent

	startErr     error
	subscribeErr error
	statusErr    error
}

func (f *fakePreflightJobs) StartJob(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, req)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &orchestration.StartJobResponse{JobID: "job-preflight-1"}, nil
}

func (f *fakePreflightJobs) GetJobStatus(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	i := f.reads
	if i >= len(f.statuses) {
		i = len(f.statuses) - 1
	}
	f.reads++
	status := *f.statuses[i]
	status.JobID = jobID
	return &status, nil
}

func (f *fakePreflightJobs) SubscribeJobEvents(_ context.Context, _, _ string) (<-chan *orchestration.JobEvent, error) {
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	return f.events, nil
}

func preflightStatus(state orchestration.JobState, results ...orchestration.ValidationResult) *orchestration.JobStatus {
	return &orchestration.JobStatus{State: state, Verification: results}
}

func TestRunPreflight_FollowsTheJobToItsEnd(t *testing.T) {
	events := make(chan *orchestration.JobEvent, 1)
	events <- &orchestration.JobEvent{}
	jobs := &fakePreflightJobs{
		events: events,
		statuses: []*orchestration.JobStatus{
			preflightStatus(orchestration.JobStateValidating),
			preflightStatus(orchestration.JobStateCompleted,
				orchestration.ValidationResult{Name: "make build", Passed: true},
				orchestration.ValidationResult{Name: "make test", Passed: true}),
		},
	}

	result, err := RunPreflight(context.Background(), jobs, testTenant, testBinding())
	if err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	if !result.Passed() || result.FailureReport() != "" {
		t.Fatalf("result = %+v, want green with no failure report", result)
	}
	req := jobs.started[0]
	if req.Runner != PreflightRunnerName || req.ProjectID != testProject || req.Lead != testBinding().Worker {
		t.Fatalf("StartJob = %+v, want a preflight of the project led by the worker", req)
	}
}

func TestRunPreflight_RedReportsTheFailingLines(t *testing.T) {
	jobs := &fakePreflightJobs{
		events: make(chan *orchestration.JobEvent),
		statuses: []*orchestration.JobStatus{preflightStatus(orchestration.JobStateCompleted,
			orchestration.ValidationResult{Name: "make build", Passed: true},
			orchestration.ValidationResult{Name: "make test", ExitCode: 2, Details: "--- FAIL: TestX\nx_test.go:9: want 1\nFAIL\n"}),
		},
	}

	result, err := RunPreflight(context.Background(), jobs, testTenant, testBinding())
	if err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	if result.Passed() {
		t.Fatal("a failing command made the preflight green")
	}
	report := result.FailureReport()
	if !strings.Contains(report, "command=make test exit_code=2") || !strings.Contains(report, "x_test.go:9: want 1") {
		t.Fatalf("FailureReport = %q, want the failing command and its line", report)
	}
	if strings.Contains(report, "make build") {
		t.Fatalf("FailureReport = %q names a command that passed", report)
	}
}

func TestRunPreflight_AFailedJobIsRedWithItsError(t *testing.T) {
	jobs := &fakePreflightJobs{
		events:   make(chan *orchestration.JobEvent),
		statuses: []*orchestration.JobStatus{{State: orchestration.JobStateFailed, Error: "command refused"}},
	}

	result, err := RunPreflight(context.Background(), jobs, testTenant, testBinding())
	if err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	if result.Passed() || !strings.Contains(result.FailureReport(), "command refused") {
		t.Fatalf("result = %+v, want red carrying the job's error", result)
	}
}

func TestRunPreflight_AStreamThatEndsFirstIsNoVerdict(t *testing.T) {
	events := make(chan *orchestration.JobEvent)
	close(events)
	jobs := &fakePreflightJobs{events: events, statuses: []*orchestration.JobStatus{preflightStatus(orchestration.JobStateValidating)}}

	_, err := RunPreflight(context.Background(), jobs, testTenant, testBinding())
	if !errors.Is(err, ErrPreflight) {
		t.Fatalf("RunPreflight = %v, want ErrPreflight", err)
	}
}

func TestRunPreflight_ACancelledWaitIsNoVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	jobs := &fakePreflightJobs{
		events:   make(chan *orchestration.JobEvent),
		statuses: []*orchestration.JobStatus{preflightStatus(orchestration.JobStateValidating)},
	}

	_, err := RunPreflight(ctx, jobs, testTenant, testBinding())
	var preflight *PreflightError
	if !errors.As(err, &preflight) || !errors.Is(preflight.Cause, context.Canceled) {
		t.Fatalf("RunPreflight = %v, want ErrPreflight", err)
	}
}

func TestRunPreflight_EngineFailuresAreNoVerdict(t *testing.T) {
	boom := errors.New("engine down")
	tests := map[string]*fakePreflightJobs{
		"start":     {startErr: boom},
		"subscribe": {subscribeErr: boom},
		"status":    {events: make(chan *orchestration.JobEvent), statusErr: boom},
	}
	for name, jobs := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := RunPreflight(context.Background(), jobs, testTenant, testBinding())
			var preflight *PreflightError
			if !errors.As(err, &preflight) || !errors.Is(preflight.Cause, boom) {
				t.Fatalf("RunPreflight = %v, want a PreflightError caused by the engine", err)
			}
		})
	}
}

func TestRunPreflight_RefusesMissingInputs(t *testing.T) {
	var invalid *InvalidInputError
	if _, err := RunPreflight(context.Background(), nil, testTenant, testBinding()); !errors.As(err, &invalid) {
		t.Fatalf("RunPreflight with no engine = %v, want InvalidInputError", err)
	}
	if _, err := RunPreflight(context.Background(), &fakePreflightJobs{}, testTenant, nil); !errors.As(err, &invalid) {
		t.Fatalf("RunPreflight with no binding = %v, want InvalidInputError", err)
	}
}

func TestPreflightResult_ACompletedJobThatRanNothingIsNotGreen(t *testing.T) {
	result := &PreflightResult{JobID: "j1", State: orchestration.JobStateCompleted}
	if result.Passed() {
		t.Fatal("a preflight that ran no command proved nothing and must not pass")
	}
	if !strings.Contains(result.FailureReport(), "without running a command") {
		t.Fatalf("FailureReport = %q, want it to say nothing ran", result.FailureReport())
	}
	var none *PreflightResult
	if none.Passed() || none.FailureReport() != "" {
		t.Fatal("a nil result must be neither green nor carry a report")
	}
}

func TestPreflightError_Message(t *testing.T) {
	err := &PreflightError{ProjectID: "p1", JobID: "j1", Reason: "gone", Cause: errors.New("eof")}
	if got := err.Error(); got != "sdlc: preflight reached no verdict: project p1: job j1: gone: eof" {
		t.Fatalf("Error() = %q", got)
	}
	if got := (&PreflightError{ProjectID: "p1"}).Error(); got != "sdlc: preflight reached no verdict: project p1" {
		t.Fatalf("Error() = %q", got)
	}
}
