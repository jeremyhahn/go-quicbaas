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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// Fixtures
// =============================================================================

// fakeRuns hands the detector a fixed set of runs.
type fakeRuns []*JobRun

func (f fakeRuns) RunningJobs() []*JobRun { return f }

// fakeSessions answers SessionStatus from a table; an absent session is
// "not found", and a session mapped to an error answers that error.
type fakeSessions struct {
	states map[string]SessionState
	errs   map[string]error
}

func (f *fakeSessions) SessionStatus(_ context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if err, ok := f.errs[req.SessionID]; ok {
		return nil, err
	}
	state, ok := f.states[req.SessionID]
	if !ok {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	return &SessionStatusResponse{SessionID: req.SessionID, State: state}, nil
}

// loopInState builds a run whose status has sat in state since updatedAt.
func loopInState(jobID string, state JobState, updatedAt time.Time, sessions ...string) *JobRun {
	run := newJobRun(JobRunConfig{
		JobID:  jobID,
		Config: &JobConfig{TenantID: "acme", TaskID: "task-" + jobID},
		Agents: &controlLoopMockService{},
		Logger: &controlLoopMockLogger{},
	})
	run.status.Store(&JobStatus{
		JobID: jobID, TenantID: "acme", TaskID: "task-" + jobID, State: state,
		UpdatedAt: updatedAt.UnixNano(), SessionIDs: sessions,
	})
	return run
}

func newDetector(t *testing.T, loops fakeRuns, sessions *fakeSessions, thresholds JobIdleThresholds) (*JobStallDetector, *fakeClock) {
	t.Helper()
	detector, err := NewJobStallDetector(loops, sessions, thresholds)
	if err != nil {
		t.Fatalf("NewJobStallDetector: %v", err)
	}
	clock := newFakeClock()
	detector.now = clock.Now
	return detector, clock
}

// =============================================================================
// Thresholds
// =============================================================================

// TestJobIdleThresholds_ResolveAndWatch pins defaults, overrides, the off
// switch, and terminal states.
func TestJobIdleThresholds_ResolveAndWatch(t *testing.T) {
	configured := JobIdleThresholds{JobStateExecuting: time.Minute, JobStateReviewing: -1}
	resolved := configured.Resolved()
	if resolved[JobStateExecuting] != time.Minute {
		t.Fatal("a configured threshold is kept")
	}
	if resolved[JobStateDecomposing] != DefaultJobIdleThresholds()[JobStateDecomposing] {
		t.Fatal("an unset threshold takes the default")
	}
	if got, watched := configured.For(JobStateReviewing); watched || got != 0 {
		t.Fatal("a negative threshold is the off switch")
	}
	if _, watched := configured.For(JobStateCompleted); watched {
		t.Fatal("a terminal state is not watched")
	}
	if got, watched := configured.For(JobStateExecuting); !watched || got != time.Minute {
		t.Fatal("a watched state answers its threshold")
	}
	// Executing is overridden down to a minute and reviewing is off, so the
	// longest watched bound is validating's default.
	if configured.longest() != DefaultJobIdleThresholds()[JobStateValidating] {
		t.Fatalf("longest = %s", configured.longest())
	}
}

// TestJobIdleThresholdsFrom_RefusesAnUnknownState proves the configured map
// is validated, and that every watched state is one config also knows.
func TestJobIdleThresholdsFrom_RefusesAnUnknownState(t *testing.T) {
	thresholds, err := JobIdleThresholdsFrom(map[string]time.Duration{"executing": time.Hour, "reviewing": -1})
	if err != nil {
		t.Fatalf("JobIdleThresholdsFrom: %v", err)
	}
	if thresholds[JobStateExecuting] != time.Hour || thresholds[JobStateReviewing] != -1 {
		t.Fatalf("thresholds = %v", thresholds)
	}
	_, err = JobIdleThresholdsFrom(map[string]time.Duration{"merged": time.Hour})
	var unknown *JobIdleThresholdError
	if !errors.As(err, &unknown) || unknown.State != "merged" || !errors.Is(err, ErrJobIdleThreshold) {
		t.Fatalf("expected JobIdleThresholdError for merged, got %v", err)
	}
	if !strings.HasSuffix(unknown.Error(), ": merged") {
		t.Fatalf("the refusal names the state: %s", unknown.Error())
	}
	empty, err := JobIdleThresholdsFrom(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("nil config is empty thresholds: %v %v", empty, err)
	}

	// The vocabulary config restates for early validation must be exactly
	// this package's.
	want := WatchedJobStates()
	got := append([]string(nil), config.OrchestrationWatchedJobStates...)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("config.OrchestrationWatchedJobStates %v drifted from WatchedJobStates %v", got, want)
	}
}

// =============================================================================
// Detection
// =============================================================================

// TestJobStallDetector_ThresholdPerState proves each state is judged by its
// own bound: a job under it is healthy, and a job past it in a state that is
// not session-backed is reported once, not ended.
func TestJobStallDetector_ThresholdPerState(t *testing.T) {
	thresholds := JobIdleThresholds{JobStateDecomposing: 10 * time.Minute, JobStateValidating: time.Hour}
	sessions := &fakeSessions{}
	start := time.Now().UTC()
	loops := fakeRuns{
		loopInState("job-decomposing", JobStateDecomposing, start),
		loopInState("job-validating", JobStateValidating, start),
	}
	detector, clock := newDetector(t, loops, sessions, thresholds)
	clock.now = start

	clock.Advance(20 * time.Minute)
	report := detector.Detect(context.Background())
	if len(report.Findings) != 1 || report.Findings[0].JobID != "job-decomposing" || report.Findings[0].Verdict != JobStallIdle {
		t.Fatalf("only the decomposing job is past its bound: %+v", report.Findings)
	}
	if !strings.Contains(report.Findings[0].Evidence, "not session-backed") {
		t.Fatalf("evidence: %s", report.Findings[0].Evidence)
	}
	for _, loop := range loops {
		if loop.abortReason() != nil {
			t.Fatal("an idle report must not end the job")
		}
	}

	// The same standing wait is not repeated on the next sweep.
	report = detector.Detect(context.Background())
	if len(report.Findings) != 0 {
		t.Fatalf("a standing wait is reported once per threshold period, got %+v", report.Findings)
	}
	clock.Advance(10 * time.Minute)
	report = detector.Detect(context.Background())
	if report.Count(JobStallIdle) != 1 {
		t.Fatal("after a threshold period the wait is reported again")
	}

	// Past the validating bound as well, both are listed, sorted by job.
	clock.Advance(time.Hour)
	report = detector.Detect(context.Background())
	if len(report.Findings) != 2 || report.Findings[0].JobID != "job-decomposing" {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

// TestJobStallDetector_AddressableSessionIsNotStalled proves the second
// signal: a job past its bound whose session still answers is slow, not
// gone, and is left running.
func TestJobStallDetector_AddressableSessionIsNotStalled(t *testing.T) {
	start := time.Now().UTC()
	loop := loopInState("job-exec", JobStateExecuting, start, "s-closed", "s-live")
	sessions := &fakeSessions{states: map[string]SessionState{"s-closed": SessionStateClosed, "s-live": SessionStateIdle}}
	detector, clock := newDetector(t, fakeRuns{loop}, sessions, JobIdleThresholds{JobStateExecuting: time.Minute})
	clock.now = start
	clock.Advance(2 * time.Minute)

	report := detector.Detect(context.Background())
	if report.Count(JobStallIdle) != 1 || report.Count(JobStallStalled) != 0 {
		t.Fatalf("expected idle, got %+v", report.Findings)
	}
	if loop.abortReason() != nil {
		t.Fatal("an addressable session keeps the job alive")
	}
	if !strings.Contains(report.Findings[0].Evidence, "still addressable") {
		t.Fatalf("evidence: %s", report.Findings[0].Evidence)
	}
}

// TestJobStallDetector_NoAddressableSessionEndsTheJob proves the conclusion:
// a session-backed state past its bound with every session gone aborts the
// loop with a JobStalledError carrying the evidence.
func TestJobStallDetector_NoAddressableSessionEndsTheJob(t *testing.T) {
	start := time.Now().UTC()
	for name, tc := range map[string]struct {
		sessions []string
		table    map[string]SessionState
	}{
		"closed session":  {[]string{"s-1"}, map[string]SessionState{"s-1": SessionStateClosed}},
		"unknown session": {[]string{"s-gone"}, map[string]SessionState{}},
		"no sessions":     {nil, map[string]SessionState{}},
	} {
		t.Run(name, func(t *testing.T) {
			loop := loopInState("job-exec", JobStateReviewing, start, tc.sessions...)
			detector, clock := newDetector(t, fakeRuns{loop}, &fakeSessions{states: tc.table}, JobIdleThresholds{JobStateReviewing: time.Minute})
			clock.now = start
			clock.Advance(2 * time.Minute)

			report := detector.Detect(context.Background())
			if report.Count(JobStallStalled) != 1 {
				t.Fatalf("expected stalled, got %+v", report.Findings)
			}
			var stalled *JobStalledError
			if !errors.As(loop.abortReason(), &stalled) {
				t.Fatalf("the loop must be aborted with JobStalledError, got %v", loop.abortReason())
			}
			if stalled.JobID != "job-exec" || stalled.State != JobStateReviewing || stalled.Idle < 2*time.Minute || !strings.Contains(stalled.Evidence, "none addressable") {
				t.Fatalf("stalled error: %+v", stalled)
			}
			if !errors.Is(stalled, ErrJobStalled) || !strings.Contains(stalled.Error(), "job-exec") {
				t.Fatal("JobStalledError unwraps and renders")
			}
		})
	}
}

// TestJobStallDetector_SessionReadFailureConcludesNothing proves fail-closed:
// a session whose state cannot be read is reported, not treated as gone.
func TestJobStallDetector_SessionReadFailureConcludesNothing(t *testing.T) {
	start := time.Now().UTC()
	loop := loopInState("job-exec", JobStateExecuting, start, "s-1")
	sessions := &fakeSessions{errs: map[string]error{"s-1": errors.New("daemon unreachable")}}
	detector, clock := newDetector(t, fakeRuns{loop}, sessions, JobIdleThresholds{JobStateExecuting: time.Minute})
	clock.now = start
	clock.Advance(2 * time.Minute)

	report := detector.Detect(context.Background())
	if report.Count(JobStallSessionUnknown) != 1 || loop.abortReason() != nil {
		t.Fatalf("an unreadable session must not end the job: %+v", report.Findings)
	}
	if JobStallSessionUnknown.String() != "session_unknown" {
		t.Fatal("verdicts render as their name")
	}
}

// TestJobStallDetector_SkipsWhatItCannotJudge proves a loop with no status
// yet, a state switched off, and a terminal state produce nothing.
func TestJobStallDetector_SkipsWhatItCannotJudge(t *testing.T) {
	start := time.Now().UTC()
	noStatus := newJobRun(JobRunConfig{
		Config: &JobConfig{},
		Agents: &controlLoopMockService{},
		Logger: &controlLoopMockLogger{},
	})
	loops := fakeRuns{
		noStatus,
		loopInState("job-off", JobStateExecuting, start),
		loopInState("job-done", JobStateCompleted, start),
	}
	detector, clock := newDetector(t, loops, &fakeSessions{}, JobIdleThresholds{JobStateExecuting: -1})
	clock.now = start
	clock.Advance(100 * time.Hour)
	if report := detector.Detect(context.Background()); len(report.Findings) != 0 {
		t.Fatalf("expected nothing, got %+v", report.Findings)
	}
}

// TestNewJobStallDetector_RefusesMissingDependencies proves the constructor
// fails closed.
func TestNewJobStallDetector_RefusesMissingDependencies(t *testing.T) {
	if d, err := NewJobStallDetector(nil, &fakeSessions{}, nil); !errors.Is(err, ErrJobLeaseInvalidInput) || d != nil {
		t.Fatalf("no loops: %v", err)
	}
	if d, err := NewJobStallDetector(fakeRuns{}, nil, nil); !errors.Is(err, ErrJobLeaseInvalidInput) || d != nil {
		t.Fatalf("no sessions: %v", err)
	}
}

// TestIdleLedger pins repetition and retention.
func TestIdleLedger(t *testing.T) {
	ledger := newIdleLedger()
	now := time.Now()
	if !ledger.shouldReport("k", now, time.Minute) || ledger.shouldReport("k", now.Add(30*time.Second), time.Minute) {
		t.Fatal("a sighting reports once per threshold")
	}
	if !ledger.shouldReport("k", now.Add(time.Minute), time.Minute) {
		t.Fatal("after the threshold it reports again")
	}
	ledger.retain(map[string]bool{}, now, time.Hour)
	if !ledger.shouldReport("k", now, time.Minute) {
		t.Fatal("a job no longer idle is forgotten and reports afresh")
	}
	ledger.retain(map[string]bool{"k": true}, now.Add(2*time.Hour), time.Hour)
	if !ledger.shouldReport("k", now.Add(2*time.Hour), time.Hour) {
		t.Fatal("an entry older than the longest threshold is dropped")
	}
	if idleKey("a", "b") == idleKey("ab", "") {
		t.Fatal("keys are length-prefixed")
	}
}

// =============================================================================
// The vocabulary is the runners', not a table beside the detector (#621)
// =============================================================================

// TestDefaultJobIdleThresholds_AreTheEngineRunnersOwnDeclaration is the
// regression test for #621.
//
// The detector kept three hand-maintained tables — DefaultJobIdleThresholds,
// sessionBackedJobStates and WatchedJobStates — while JobStateSpec carried an
// IdleThreshold field with no reader and a doc comment claiming the detector
// read it. A runner could declare a state, or a bound on one, and the
// detector would never see either.
//
// THE TABLE BELOW IS THE PRE-#621 TABLE VERBATIM. It is restated here rather
// than derived so this test fails if the derivation changes any engine bound,
// which is the whole risk of moving the numbers onto the specs.
func TestDefaultJobIdleThresholds_AreTheEngineRunnersOwnDeclaration(t *testing.T) {
	t.Parallel()
	want := JobIdleThresholds{
		JobStateInitializing: 10 * time.Minute,
		JobStateDecomposing:  30 * time.Minute,
		JobStateScheduling:   10 * time.Minute,
		JobStateAssigning:    10 * time.Minute,
		JobStateExecuting:    3 * time.Hour,
		JobStateValidating:   45 * time.Minute,
		JobStateReviewing:    30 * time.Minute,
		JobStateRedelegating: 10 * time.Minute,
		JobStateCompleting:   10 * time.Minute,
		JobStateCleanup:      10 * time.Minute,
		JobStateEscalating:   10 * time.Minute,
	}

	got := DefaultJobIdleThresholds()
	if len(got) != len(want) {
		t.Fatalf("derived %d states, want %d: %v", len(got), len(want), got)
	}
	for state, threshold := range want {
		if got[state] != threshold {
			t.Errorf("%s = %s, want %s", state, got[state], threshold)
		}
	}

	// And the same set, sorted, is what a configuration validator reads.
	if len(WatchedJobStates()) != len(want) {
		t.Fatalf("WatchedJobStates() = %v", WatchedJobStates())
	}
	if !sort.StringsAreSorted(WatchedJobStates()) {
		t.Fatalf("WatchedJobStates() must be sorted: %v", WatchedJobStates())
	}
}

// TestKnownJobStates_MergesEveryRunnersDeclaration proves the merge, which is
// what makes one derived vocabulary safe to serve two runners: a state one
// runner drives with a session stays session-backed, and the longest declared
// bound wins so no runner's job is judged late by another runner's clock.
func TestKnownJobStates_MergesEveryRunnersDeclaration(t *testing.T) {
	t.Parallel()
	registry := NewJobRunnerRegistry()
	// "alpha" sorts first, so it is the first declaration of every shared
	// state and the merge cannot be passing by taking the last one.
	alpha := &fakeRunner{name: "alpha", states: []JobStateSpec{
		{State: JobStateInitializing, Label: "Alpha init", IdleThreshold: time.Minute},
		{State: JobStateExecuting, Label: "Alpha exec", IdleThreshold: time.Hour},
	}}
	zulu := &fakeRunner{name: "zulu", states: []JobStateSpec{
		{State: JobStateExecuting, Label: "Zulu exec", SessionBacked: true, IdleThreshold: 2 * time.Hour},
		{State: JobStateCleanup, Label: "", IdleThreshold: 0},
	}}
	for _, runner := range []JobRunner{zulu, alpha} {
		if err := registry.Register(runner); err != nil {
			t.Fatalf("Register(%s): %v", runner.Name(), err)
		}
	}

	specs := knownJobStatesIn(registry)
	byState := make(map[JobState]JobStateSpec, len(specs))
	order := make([]JobState, 0, len(specs))
	for _, spec := range specs {
		if _, dup := byState[spec.State]; dup {
			t.Fatalf("a shared state is listed twice: %v", specs)
		}
		byState[spec.State] = spec
		order = append(order, spec.State)
	}

	// Registry order, first-declaration order within a runner.
	wantOrder := []JobState{JobStateInitializing, JobStateExecuting, JobStateCleanup}
	for i, state := range wantOrder {
		if i >= len(order) || order[i] != state {
			t.Fatalf("order = %v, want %v", order, wantOrder)
		}
	}

	exec := byState[JobStateExecuting]
	if !exec.SessionBacked {
		t.Error("SessionBacked must be a disjunction: zulu drives executing with a session")
	}
	if exec.IdleThreshold != 2*time.Hour {
		t.Errorf("executing bound = %s, want the longest declared (2h)", exec.IdleThreshold)
	}
	if exec.Label != "Alpha exec" {
		t.Errorf("label = %q, want the first non-empty in registry order", exec.Label)
	}

	// A state nobody bounded takes the documented default rather than zero,
	// which would report the job on the first sweep.
	thresholds := jobIdleThresholdsFromSpecs(specs)
	if thresholds[JobStateCleanup] != DefaultJobIdleThreshold {
		t.Errorf("unbounded state = %s, want DefaultJobIdleThreshold", thresholds[JobStateCleanup])
	}
	if thresholds[JobStateInitializing] != time.Minute {
		t.Errorf("initializing = %s, want the runner's own minute", thresholds[JobStateInitializing])
	}
}

// TestKnownJobStates_EmptyRegistryDerivesNothing is the invalid-input half:
// nothing registered is an empty vocabulary, not a panic and not a stale
// hardcoded set. Every derived view must agree on that.
func TestKnownJobStates_EmptyRegistryDerivesNothing(t *testing.T) {
	t.Parallel()
	if specs := knownJobStatesIn(NewJobRunnerRegistry()); len(specs) != 0 {
		t.Fatalf("an empty registry declares no states, got %v", specs)
	}
	if thresholds := jobIdleThresholdsFromSpecs(nil); len(thresholds) != 0 {
		t.Fatalf("no specs is no thresholds, got %v", thresholds)
	}
	if backed := sessionBackedFromSpecs(nil); len(backed) != 0 {
		t.Fatalf("no specs is no session-backed states, got %v", backed)
	}
	// A runner that declares a state and marks none of them session-backed
	// must not be read as marking all of them.
	backed := sessionBackedFromSpecs([]JobStateSpec{{State: JobStateExecuting}})
	if backed[JobStateExecuting] {
		t.Fatal("an undeclared SessionBacked is false, not defaulted true")
	}
}

// TestJobStallDetector_SessionBackingIsTheRunnersDeclaration proves the
// detector ACTS on JobStateSpec.SessionBacked rather than on a table of its
// own: the same state, the same bound, the same dead session, and the only
// difference is what the runner declared.
func TestJobStallDetector_SessionBackingIsTheRunnersDeclaration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		sessionBacked bool
		wantVerdict   JobStallVerdict
		wantAborted   bool
	}{
		{name: "declared session-backed", sessionBacked: true, wantVerdict: JobStallStalled, wantAborted: true},
		{name: "not declared session-backed", sessionBacked: false, wantVerdict: JobStallIdle, wantAborted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now().UTC()
			// One session, and it is not in the table, so it is not
			// addressable: the only thing that can decide the verdict is the
			// spec.
			loops := fakeRuns{loopInState("job-1", JobStateValidating, start, "session-gone")}
			specs := []JobStateSpec{{
				State:         JobStateValidating,
				Label:         "Validating",
				SessionBacked: tc.sessionBacked,
				IdleThreshold: 20 * time.Minute,
			}}
			detector, err := newJobStallDetector(loops, &fakeSessions{}, nil, specs)
			if err != nil {
				t.Fatalf("newJobStallDetector: %v", err)
			}
			clock := newFakeClock()
			clock.now = start
			detector.now = clock.Now

			clock.Advance(21 * time.Minute)
			report := detector.Detect(context.Background())
			if len(report.Findings) != 1 {
				t.Fatalf("findings = %+v", report.Findings)
			}
			if report.Findings[0].Verdict != tc.wantVerdict {
				t.Errorf("verdict = %s, want %s (evidence: %s)",
					report.Findings[0].Verdict, tc.wantVerdict, report.Findings[0].Evidence)
			}
			if aborted := loops[0].abortReason() != nil; aborted != tc.wantAborted {
				t.Errorf("aborted = %v, want %v", aborted, tc.wantAborted)
			}
		})
	}
}

// TestJobStallDetector_ReadsTheRunnersDeclaredBound proves the bound the
// detector judges by is the runner's IdleThreshold, not a default: a job past
// the runner's own bound but under the engine default is reported.
func TestJobStallDetector_ReadsTheRunnersDeclaredBound(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC()
	loops := fakeRuns{loopInState("job-1", JobStateExecuting, start)}
	// The engine bounds executing at three hours. This runner says one
	// minute, and a detector still reading the old table would call the job
	// healthy.
	specs := []JobStateSpec{{State: JobStateExecuting, Label: "Executing", IdleThreshold: time.Minute}}
	detector, err := newJobStallDetector(loops, &fakeSessions{}, nil, specs)
	if err != nil {
		t.Fatalf("newJobStallDetector: %v", err)
	}
	clock := newFakeClock()
	clock.now = start
	detector.now = clock.Now

	clock.Advance(90 * time.Second)
	report := detector.Detect(context.Background())
	if len(report.Findings) != 1 || report.Findings[0].Verdict != JobStallIdle {
		t.Fatalf("the runner's own bound must be what is judged: %+v", report.Findings)
	}
	if !strings.Contains(report.Findings[0].Evidence, "past its 1m0s bound") {
		t.Fatalf("evidence must name the runner's bound: %s", report.Findings[0].Evidence)
	}
}

// =============================================================================
// The tenant a session is addressed under (#776)
// =============================================================================

// tenantScopedSessions is the session authority shaped as the engine's own:
// it resolves the tenant from the CONTEXT and from nowhere else
// (agentServiceImpl.validateTenant), refuses a caller that carries none, and
// files each session under the tenant that owns it. A detector that forwards
// its untenanted sweep context therefore gets "tenant ID required" for every
// probe, which is exactly what the burn-in node reported.
type tenantScopedSessions struct {
	// states is tenant -> session -> state.
	states map[string]map[string]SessionState

	// seen is every tenant a probe arrived under, in order. Written and read
	// on the caller's goroutine: Detect examines its runs serially.
	seen []string
}

func (s *tenantScopedSessions) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	tenantID := TenantFromContext(ctx)
	s.seen = append(s.seen, tenantID)
	if tenantID == "" {
		return nil, &TenantRequiredError{Operation: "orchestration"}
	}
	state, ok := s.states[tenantID][req.SessionID]
	if !ok {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	return &SessionStatusResponse{SessionID: req.SessionID, State: state}, nil
}

// newTenantDetector builds a detector over a tenant-scoped session authority.
func newTenantDetector(t *testing.T, loops fakeRuns, sessions *tenantScopedSessions, thresholds JobIdleThresholds) (*JobStallDetector, *fakeClock) {
	t.Helper()
	detector, err := NewJobStallDetector(loops, sessions, thresholds)
	if err != nil {
		t.Fatalf("NewJobStallDetector: %v", err)
	}
	clock := newFakeClock()
	detector.now = clock.Now
	return detector, clock
}

// TestJobStallDetector_AddressesSessionsUnderTheJobsTenant is the regression
// for #776: the sweep carries no tenant, so the job's own tenant must be the
// one the session is asked for under. Both conclusions are exercised — a live
// session is IDLE and a vanished one is STALLED — because the defect made
// both of them SessionUnknown.
func TestJobStallDetector_AddressesSessionsUnderTheJobsTenant(t *testing.T) {
	start := time.Now().UTC()
	for name, tc := range map[string]struct {
		stored  map[string]SessionState
		verdict JobStallVerdict
		aborted bool
	}{
		"a live session under the job's tenant is idle": {
			stored:  map[string]SessionState{"s-1": SessionStateActive},
			verdict: JobStallIdle,
		},
		"a session gone from the job's tenant is stalled": {
			stored:  map[string]SessionState{},
			verdict: JobStallStalled,
			aborted: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			loop := loopInState("job-exec", JobStateReviewing, start, "s-1")
			sessions := &tenantScopedSessions{states: map[string]map[string]SessionState{"acme": tc.stored}}
			detector, clock := newTenantDetector(t, fakeRuns{loop}, sessions, JobIdleThresholds{JobStateReviewing: time.Minute})
			clock.now = start
			clock.Advance(2 * time.Minute)

			// The sweeper's context, which is the daemon's maintenance
			// ticker: no principal, no tenant.
			report := detector.Detect(context.Background())

			if report.Count(JobStallSessionUnknown) != 0 {
				t.Fatalf("the detector must resolve the tenant itself, got %+v", report.Findings)
			}
			if report.Count(tc.verdict) != 1 {
				t.Fatalf("expected %s, got %+v", tc.verdict, report.Findings)
			}
			if len(sessions.seen) != 1 || sessions.seen[0] != "acme" {
				t.Fatalf("the session must be asked for under the job's tenant, got %v", sessions.seen)
			}
			var stalled *JobStalledError
			if tc.aborted != errors.As(loop.abortReason(), &stalled) {
				t.Fatalf("abort = %v, want aborted=%v", loop.abortReason(), tc.aborted)
			}
		})
	}
}

// TestJobStallDetector_UntenantedJobStillFailsClosed proves the resolution is
// not a fallback that invents a tenant: a run whose status names none leaves
// the context as it found it, the service refuses, and the verdict is
// SessionUnknown rather than a half-conclusion.
func TestJobStallDetector_UntenantedJobStillFailsClosed(t *testing.T) {
	start := time.Now().UTC()
	loop := loopInState("job-exec", JobStateReviewing, start, "s-1")
	status := loop.Status()
	status.TenantID = ""
	loop.status.Store(status)

	sessions := &tenantScopedSessions{states: map[string]map[string]SessionState{"acme": {"s-1": SessionStateActive}}}
	detector, clock := newTenantDetector(t, fakeRuns{loop}, sessions, JobIdleThresholds{JobStateReviewing: time.Minute})
	clock.now = start
	clock.Advance(2 * time.Minute)

	report := detector.Detect(context.Background())
	if report.Count(JobStallSessionUnknown) != 1 || loop.abortReason() != nil {
		t.Fatalf("an unreadable session must not end the job: %+v", report.Findings)
	}
	if !strings.Contains(report.Findings[0].Evidence, ErrTenantRequired.Error()) {
		t.Fatalf("the evidence must carry the refusal: %s", report.Findings[0].Evidence)
	}
}
