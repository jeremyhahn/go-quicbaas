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
	"strings"
	"testing"
)

// =============================================================================
// A runner a test drives the envelope with
// =============================================================================

// fakeRunner is a runner whose whole behaviour a test dictates: what its
// states are, whether it validates, and what Run does with the job it is
// handed.
type fakeRunner struct {
	name        string
	states      []JobStateSpec
	validateErr error
	resume      func(JobState) JobState
	run         func(ctx context.Context, job *JobRun) error
}

func (f *fakeRunner) Name() string { return f.name }

func (f *fakeRunner) States() []JobStateSpec {
	if f.states == nil {
		return []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}}
	}
	return f.states
}

func (f *fakeRunner) Validate(_ context.Context, _ *JobConfig) error { return f.validateErr }

func (f *fakeRunner) Run(ctx context.Context, job *JobRun) error {
	if f.run == nil {
		return nil
	}
	return f.run(ctx, job)
}

// resumingRunner adds the optional JobResumer half.
type resumingRunner struct{ *fakeRunner }

func (r resumingRunner) ResumeState(interrupted JobState) JobState {
	return r.resume(interrupted)
}

// =============================================================================
// Registry
// =============================================================================

// TestJobRunnerRegistry_RegisterAndLookup proves a registered runner is found
// by the name it reports and listed in name order.
func TestJobRunnerRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	registry := NewJobRunnerRegistry()
	zebra := &fakeRunner{name: "zebra"}
	alpha := &fakeRunner{name: "alpha"}
	for _, runner := range []JobRunner{zebra, alpha} {
		if err := registry.Register(runner); err != nil {
			t.Fatalf("Register(%s): %v", runner.Name(), err)
		}
	}

	got, ok := registry.Lookup("zebra")
	if !ok || got != JobRunner(zebra) {
		t.Fatalf("Lookup(zebra) = %v, %v", got, ok)
	}
	if _, ok := registry.Lookup("nobody"); ok {
		t.Fatal("an unregistered name must not resolve")
	}

	all := registry.All()
	if len(all) != 2 || all[0].Name() != "alpha" || all[1].Name() != "zebra" {
		t.Fatalf("All() must be ordered by name, got %v", all)
	}
}

// TestJobRunnerRegistry_RefusesBadRegistrations proves every refusal is typed:
// a nil runner, an empty name, a runner declaring no states, and a duplicate.
func TestJobRunnerRegistry_RefusesBadRegistrations(t *testing.T) {
	t.Parallel()
	registry := NewJobRunnerRegistry()

	var refusal *JobRunnerRegistrationError
	for name, runner := range map[string]JobRunner{
		"nil runner": nil,
		"empty name": &fakeRunner{name: ""},
		"no states":  &fakeRunner{name: "stateless", states: []JobStateSpec{}},
	} {
		err := registry.Register(runner)
		if !errors.As(err, &refusal) || !errors.Is(err, ErrJobRunnerRegistration) {
			t.Fatalf("%s: got %v, want JobRunnerRegistrationError", name, err)
		}
		if err.Error() == "" {
			t.Fatalf("%s: refusal must say why", name)
		}
	}

	if err := registry.Register(&fakeRunner{name: "taken"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := registry.Register(&fakeRunner{name: "taken"})
	var duplicate *JobRunnerAlreadyRegisteredError
	if !errors.As(err, &duplicate) || duplicate.Name != "taken" {
		t.Fatalf("got %v, want JobRunnerAlreadyRegisteredError", err)
	}
	if !errors.Is(err, ErrJobRunnerAlreadyRegistered) {
		t.Fatal("the duplicate refusal must unwrap to its sentinel")
	}
}

// TestJobRunners_EngineRegistersItsOwn proves the workflow runner is in the
// engine's registry, that an empty runner name resolves to it, and that an
// unknown name is refused with a typed error.
func TestJobRunners_EngineRegistersItsOwn(t *testing.T) {
	t.Parallel()
	runner, ok := LookupJobRunner(JobRunnerWorkflow)
	if !ok {
		t.Fatal("the engine's own runner must be registered")
	}
	if runner.Name() != JobRunnerWorkflow {
		t.Fatalf("Name() = %q", runner.Name())
	}
	if ResolveJobRunnerName("") != JobRunnerWorkflow {
		t.Fatal("an unnamed runner is the workflow runner")
	}
	if ResolveJobRunnerName("sdlc") != "sdlc" {
		t.Fatal("a named runner is left alone")
	}
	if err := ValidateJobRunnerName(""); err != nil {
		t.Fatalf("an empty name is valid: %v", err)
	}
	if err := ValidateJobRunnerName(JobRunnerWorkflow); err != nil {
		t.Fatalf("the workflow runner is valid: %v", err)
	}
	var unknown *UnknownJobRunnerError
	err := ValidateJobRunnerName("nobody-registered-this")
	if !errors.As(err, &unknown) || unknown.Name != "nobody-registered-this" {
		t.Fatalf("got %v, want UnknownJobRunnerError", err)
	}
	if !errors.Is(err, ErrUnknownJobRunner) {
		t.Fatal("the unknown-runner error must unwrap to its sentinel")
	}

	var found bool
	for _, r := range JobRunners() {
		if r.Name() == JobRunnerWorkflow {
			found = true
		}
	}
	if !found {
		t.Fatal("JobRunners must list the engine's own runner")
	}
}

// TestKnownJobStates_CoversTheWorkflowRunner proves the declared states are
// readable from the registry, in declaration order, with the session-backed
// ones marked — the facts the stall detector and the UI stepper need.
func TestKnownJobStates_CoversTheWorkflowRunner(t *testing.T) {
	t.Parallel()
	specs := KnownJobStates()
	byState := make(map[JobState]JobStateSpec, len(specs))
	seen := make(map[JobState]int, len(specs))
	for _, spec := range specs {
		byState[spec.State] = spec
		seen[spec.State]++
	}
	for state, count := range seen {
		if count != 1 {
			t.Fatalf("state %s listed %d times; a shared state is listed once", state, count)
		}
	}
	if specs[0].State != JobStateInitializing {
		t.Fatalf("the first state is the entry state, got %s", specs[0].State)
	}
	for _, state := range []JobState{
		JobStateInitializing, JobStateDecomposing, JobStateScheduling, JobStateAssigning,
		JobStateExecuting, JobStateValidating, JobStateReviewing, JobStateRedelegating,
		JobStateCompleting, JobStateCleanup, JobStateEscalating,
	} {
		spec, ok := byState[state]
		if !ok {
			t.Fatalf("state %s is not declared", state)
		}
		if spec.Label == "" {
			t.Fatalf("state %s has no label", state)
		}
	}
	if !byState[JobStateExecuting].SessionBacked || !byState[JobStateReviewing].SessionBacked {
		t.Fatal("executing and reviewing are session-backed")
	}
	if byState[JobStateValidating].SessionBacked {
		t.Fatal("validating is not session-backed")
	}
	if IsTerminalJobState(specs[0].State) {
		t.Fatal("a runner declares no terminal state")
	}
}

// TestResumeStateFor_AsksTheRunner proves a runner implementing JobResumer is
// asked where to restart, and one that does not restarts at its first state.
func TestResumeStateFor_AsksTheRunner(t *testing.T) {
	t.Parallel()
	plain := &fakeRunner{name: "plain", states: []JobStateSpec{
		{State: JobStateExecuting}, {State: JobStateReviewing},
	}}
	if got := resumeStateFor(plain, JobStateReviewing); got != JobStateExecuting {
		t.Fatalf("a runner with no resume answer restarts at its first state, got %s", got)
	}
	if got := entryStateFor(&fakeRunner{name: "empty", states: []JobStateSpec{}}); got != JobStateInitializing {
		t.Fatalf("a runner declaring nothing entries at initializing, got %s", got)
	}

	asked := resumingRunner{&fakeRunner{
		name:   "resuming",
		states: []JobStateSpec{{State: JobStateExecuting}},
		resume: func(interrupted JobState) JobState {
			if interrupted == JobStateCompleting {
				return JobStateCompleting
			}
			return JobStateExecuting
		},
	}}
	if got := resumeStateFor(asked, JobStateCompleting); got != JobStateCompleting {
		t.Fatalf("the runner's answer wins, got %s", got)
	}
}

// TestWorkflowRunner_ValidateRequiresADescription proves the refusal that used
// to live in StartJob is the runner's, and that its resume mapping sends an
// interrupted lead-driven job back to decomposing.
func TestWorkflowRunner_ValidateRequiresADescription(t *testing.T) {
	t.Parallel()
	runner := workflowRunner{}
	if err := runner.Validate(context.Background(), &JobConfig{Description: "do the thing"}); err != nil {
		t.Fatalf("a described job is valid: %v", err)
	}
	if err := runner.Validate(context.Background(), &JobConfig{}); !errors.Is(err, ErrTaskRequired) {
		t.Fatalf("got %v, want ErrTaskRequired", err)
	}
	if err := runner.Validate(context.Background(), nil); err == nil {
		t.Fatal("a nil config is refused")
	}

	if got := runner.ResumeState(JobStateExecuting); got != JobStateDecomposing {
		t.Fatalf("a lead-driven job re-decomposes, got %s", got)
	}
	if got := runner.ResumeState(JobStateEscalating); got != JobStateFailed {
		t.Fatalf("an escalating job had spent its rounds, got %s", got)
	}
	if got := runner.ResumeState(JobStateCleanup); got != JobStateCompleting {
		t.Fatalf("a job that was almost done finishes, got %s", got)
	}
}

// TestJobRunnerErrors_SayWhichRunnerAndWhy proves each typed error renders the
// runner it is about, so a refusal read from a log names the cause.
func TestJobRunnerErrors_SayWhichRunnerAndWhy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want []string
	}{
		{"named registration", &JobRunnerRegistrationError{Name: "sdlc", Message: "no states"}, []string{"sdlc", "no states"}},
		{"unnamed registration", &JobRunnerRegistrationError{Message: "runner must not be nil"}, []string{"runner must not be nil"}},
		{"duplicate", &JobRunnerAlreadyRegisteredError{Name: "sdlc"}, []string{"sdlc", "already registered"}},
		{"unknown", &UnknownJobRunnerError{Name: "sdlc"}, []string{"sdlc", "not registered"}},
	} {
		got := tc.err.Error()
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q must mention %q", tc.name, got, want)
			}
		}
	}
}
