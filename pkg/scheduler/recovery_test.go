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

package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Fixture
// =============================================================================

// recoveryFiring is the instant every fixture is expressed relative to. Nothing
// here waits on a clock: firing times are written into the record.
var recoveryFiring = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

// goodAttestation is a complete observation of an abandoned run.
func goodAttestation() AbandonedRunAttestation {
	return AbandonedRunAttestation{
		Firing:     recoveryFiring,
		Detector:   "sdlc firing liveness checker",
		Detail:     "no container is executing this job on the node that claimed it",
		ObservedAt: recoveryFiring.Add(time.Minute),
	}
}

// seedRunningJob stores a job the engine believes is executing.
//
// NextRun carries the firing under execution, which is what a running job's
// NextRun means: it advances only when a run completes.
func seedRunningJob(t *testing.T, storage *mockJobStorage, job *Job) *Job {
	t.Helper()
	if err := storage.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return job
}

func runningRecoveryJob() *Job {
	return &Job{
		Name:      "abandoned",
		Type:      JobTypeRecurring,
		Schedule:  "*/5 * * * *",
		Handler:   "sdlc.reconcile",
		Status:    JobStatusRunning,
		NextRun:   recoveryFiring,
		UpdatedAt: recoveryFiring,
	}
}

// storedJob reads the DURABLE record. Every assertion below reads this rather
// than the returned status: a method that reported a status and wrote something
// else would satisfy a return-value assertion, and the stored record is what
// every later tick acts on.
func storedJob(t *testing.T, storage *mockJobStorage, id uint64) *Job {
	t.Helper()
	job, err := storage.GetJob(context.Background(), id, "")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job == nil {
		t.Fatalf("job %d is not in the store", id)
	}
	return job
}

// =============================================================================
// The recovery
// =============================================================================

func TestRecoverAbandonedRun_ReturnsTheJobToTheQueueWithBackoff(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	before := time.Now()
	status, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	if err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}
	if status != JobStatusPending {
		t.Fatalf("returned status is %s, want pending", status)
	}

	stored := storedJob(t, storage, job.ID)
	if stored.Status != JobStatusPending {
		t.Errorf("STORED status is %s, want pending", stored.Status)
	}
	if stored.FailCount != 1 {
		t.Errorf("STORED fail count is %d, want 1: an abandoned run spends the retry budget "+
			"like any other run that produced no result", stored.FailCount)
	}

	// THE NEXT FIRING MUST BE ABOVE THE ONE THAT WAS ABANDONED, which is what
	// makes the recovered job claimable again: the dead node left the claim
	// mark at the old firing, and the mark only moves forward. A recovery that
	// left NextRun where it was would produce a job that is pending, due, and
	// permanently unclaimable.
	if !stored.NextRun.After(recoveryFiring) {
		t.Errorf("STORED next run is %s, not after the abandoned firing %s: the stale claim "+
			"mark would refuse every future claim", stored.NextRun, recoveryFiring)
	}
	if !stored.NextRun.After(before) {
		t.Errorf("STORED next run %s is not in the future of the recovery", stored.NextRun)
	}

	// THE EVIDENCE SURVIVES, because LastError is the only place an operator
	// looks to find out why a job requeued itself.
	if !strings.Contains(stored.LastError, "no container is executing this job") {
		t.Errorf("STORED last error does not carry what was observed: %q", stored.LastError)
	}
	if !strings.Contains(stored.LastError, "sdlc firing liveness checker") {
		t.Errorf("STORED last error does not name the detector: %q", stored.LastError)
	}
}

// TestRecoverAbandonedRun_FailsTheJobOnceTheBudgetIsSpent proves the recovery
// shares the engine's retry policy rather than carrying its own.
func TestRecoverAbandonedRun_FailsTheJobOnceTheBudgetIsSpent(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := runningRecoveryJob()
	job.MaxRetries = 1
	job.FailCount = 1 // already spent
	seedRunningJob(t, storage, job)

	status, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	if err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}
	if status != JobStatusFailed {
		t.Fatalf("returned status is %s, want failed", status)
	}
	if stored := storedJob(t, storage, job.ID); stored.Status != JobStatusFailed {
		t.Errorf("STORED status is %s, want failed", stored.Status)
	}
}

// TestRecoverAbandonedRun_RecordsTheRunInHistory covers the gap the dead node
// left: it never saved an execution record, so without this the abandoned run
// is missing from exactly the history somebody investigating would read.
func TestRecoverAbandonedRun_RecordsTheRunInHistory(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}

	executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("history holds %d executions, want 1", len(executions))
	}
	exec := executions[0]
	if exec.Status != JobStatusFailed {
		t.Errorf("the recorded execution is %s, want failed", exec.Status)
	}
	if !exec.StartedAt.Equal(recoveryFiring) {
		t.Errorf("the execution starts at %s, want the running transition at %s: the dead "+
			"node saved no record, so UpdatedAt is the only evidence of when the run began",
			exec.StartedAt, recoveryFiring)
	}
	if !strings.Contains(exec.Error, "no container is executing this job") {
		t.Errorf("the recorded execution does not carry the evidence: %q", exec.Error)
	}
}

// =============================================================================
// The guards
// =============================================================================

// TestRecoverAbandonedRun_RefusesEveryStatusThatIsNotRunning is the guard that
// makes this method unusable against healthy work.
//
// Every state a job can legally be in is exercised, so there is no argument
// value that reaches work waiting its turn or work that finished.
func TestRecoverAbandonedRun_RefusesEveryStatusThatIsNotRunning(t *testing.T) {
	for _, status := range []JobStatus{
		JobStatusPending, JobStatusPaused, JobStatusCompleted,
		JobStatusFailed, JobStatusCanceled,
	} {
		t.Run(string(status), func(t *testing.T) {
			storage := newMockJobStorage()
			engine := New(DefaultConfig(), storage)
			job := runningRecoveryJob()
			job.Status = status
			seedRunningJob(t, storage, job)

			got, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())

			var refused RecoveryRefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("RecoverAbandonedRun = %v; want a typed refusal", err)
			}
			if refused.Refusal != RefusedNotRunning {
				t.Errorf("refusal is %s, want not_running", refused.Refusal)
			}
			if refused.Status != status {
				t.Errorf("the refusal reports status %s, want %s", refused.Status, status)
			}
			if got != status {
				t.Errorf("returned status is %s, want the status found, %s", got, status)
			}

			stored := storedJob(t, storage, job.ID)
			if stored.Status != status {
				t.Errorf("a refused recovery moved the job to %s", stored.Status)
			}
			if stored.FailCount != 0 {
				t.Errorf("a refused recovery spent %d of the retry budget", stored.FailCount)
			}
		})
	}
}

// TestRecoverAbandonedRun_RefusesAnAttestationForAnotherFiring is the
// stale-attestation guard, and its fixture is the one that matters.
//
// The job here is RUNNING, exactly as the caller observed — only the firing has
// moved, because the job completed and started again between the observation
// and this call. A guard written as a status check alone passes that through
// and concludes a run nobody looked at, which is the double-fire failure
// inverted: killing live work rather than duplicating it.
func TestRecoverAbandonedRun_RefusesAnAttestationForAnotherFiring(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := runningRecoveryJob()
	job.NextRun = recoveryFiring.Add(5 * time.Minute) // the job moved on
	seedRunningJob(t, storage, job)

	got, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())

	var refused RecoveryRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("RecoverAbandonedRun = %v; want a typed refusal", err)
	}
	if refused.Refusal != RefusedFiringMoved {
		t.Fatalf("refusal is %s, want firing_moved", refused.Refusal)
	}
	if !refused.Attested.Equal(recoveryFiring) || !refused.Found.Equal(job.NextRun) {
		t.Errorf("the refusal reports attested %s / found %s, want %s / %s",
			refused.Attested, refused.Found, recoveryFiring, job.NextRun)
	}
	if got != JobStatusRunning {
		t.Errorf("returned status is %s, want running", got)
	}

	stored := storedJob(t, storage, job.ID)
	if stored.Status != JobStatusRunning {
		t.Errorf("a refused recovery moved the job to %s: it is running a firing nobody "+
			"observed", stored.Status)
	}
	if stored.FailCount != 0 {
		t.Errorf("a refused recovery spent %d of a live run's retry budget", stored.FailCount)
	}
}

// TestRecoveryRefusals_AreDistinguishableWithoutReadingText pins the property
// the caller depends on.
//
// The caller is a triage sweep that meets both refusals on a healthy fleet and
// must act differently: one means the job already concluded, the other means
// its own observation is stale and must be made again. Reducing a caller to
// matching message text is the defect this codebase has already paid for once.
func TestRecoveryRefusals_AreDistinguishableWithoutReadingText(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)

	concluded := runningRecoveryJob()
	concluded.Status = JobStatusCompleted
	seedRunningJob(t, storage, concluded)

	moved := runningRecoveryJob()
	moved.NextRun = recoveryFiring.Add(time.Hour)
	seedRunningJob(t, storage, moved)

	_, notRunning := engine.RecoverAbandonedRun(context.Background(), concluded, goodAttestation())
	_, firingMoved := engine.RecoverAbandonedRun(context.Background(), moved, goodAttestation())

	var a, b RecoveryRefusedError
	if !errors.As(notRunning, &a) || !errors.As(firingMoved, &b) {
		t.Fatalf("refusals are not both typed: %v / %v", notRunning, firingMoved)
	}
	if a.Refusal == b.Refusal {
		t.Fatalf("both refusals report %s, so a caller cannot tell a concluded job from a "+
			"stale observation", a.Refusal)
	}
	// Each renders its own diagnosis, so the two are also distinct to a human
	// reading a log — but the caller branches on the field, never on this text.
	if a.Error() == b.Error() {
		t.Errorf("both refusals render identically: %q", a.Error())
	}
}

func TestRecoverAbandonedRun_RefusesAnIncompleteAttestation(t *testing.T) {
	complete := goodAttestation()
	cases := []struct {
		name  string
		field string
		att   AbandonedRunAttestation
	}{
		{"no firing", "firing", AbandonedRunAttestation{
			Detector: complete.Detector, Detail: complete.Detail,
		}},
		{"no detector", "detector", AbandonedRunAttestation{
			Firing: complete.Firing, Detail: complete.Detail,
		}},
		{"no detail", "detail", AbandonedRunAttestation{
			Firing: complete.Firing, Detector: complete.Detector,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storage := newMockJobStorage()
			engine := New(DefaultConfig(), storage)
			job := seedRunningJob(t, storage, runningRecoveryJob())

			_, err := engine.RecoverAbandonedRun(context.Background(), job, tc.att)
			var invalid InvalidAttestationError
			if !errors.As(err, &invalid) {
				t.Fatalf("RecoverAbandonedRun = %v; want an invalid-attestation refusal", err)
			}
			if invalid.Field != tc.field {
				t.Errorf("the refusal names field %q, want %q", invalid.Field, tc.field)
			}
			if stored := storedJob(t, storage, job.ID); stored.Status != JobStatusRunning {
				t.Errorf("an attestation refused before any read moved the job to %s", stored.Status)
			}
		})
	}
}

func TestRecoverAbandonedRun_RefusesANilJob(t *testing.T) {
	engine := New(DefaultConfig(), newMockJobStorage())
	_, err := engine.RecoverAbandonedRun(context.Background(), nil, goodAttestation())
	var invalid InvalidJobError
	if !errors.As(err, &invalid) {
		t.Fatalf("RecoverAbandonedRun(nil) = %v; want an invalid-job refusal", err)
	}
}

func TestRecoverAbandonedRun_RefusesAJobThatIsGone(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)

	// Never stored: deleted between the observation and the recovery.
	job := runningRecoveryJob()
	job.ID = 404

	_, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var missing JobNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("RecoverAbandonedRun = %v; want a not-found refusal rather than a resurrection", err)
	}
}

func TestRecoverAbandonedRun_ReportsAStorageFailure(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())
	storage.updateJobErr = errors.New("store unreachable")

	_, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("RecoverAbandonedRun = %v; want a storage error", err)
	}
	if !strings.Contains(err.Error(), "store unreachable") {
		t.Errorf("the error does not carry its cause: %v", err)
	}
}

// TestRecoverAbandonedRun_WorksWhileTheEngineIsStopped states the deliberate
// difference from TriggerJob.
//
// TriggerJob hands work to a dispatch loop and needs one running. This only
// reads and writes the job record, and refusing while stopped would make
// recovery impossible in exactly the situation that produces abandoned runs.
func TestRecoverAbandonedRun_WorksWhileTheEngineIsStopped(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	if engine.IsRunning() {
		t.Fatal("the fixture started the engine, so it cannot show that a stopped one works")
	}
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("RecoverAbandonedRun on a stopped engine: %v", err)
	}
	if stored := storedJob(t, storage, job.ID); stored.Status != JobStatusPending {
		t.Errorf("STORED status is %s, want pending", stored.Status)
	}
}

// =============================================================================
// The shared failure policy
// =============================================================================

// TestRecoverAbandonedRun_AccountsAsTheHandlerFailurePathDoes drives BOTH REAL
// PATHS and compares what each leaves in the store.
//
// Written this way after the first attempt was near-vacuous: it called
// applyRunFailure twice and asserted the two agreed, which one function with
// one set of inputs cannot fail. What the extraction is actually for is that a
// job abandoned by a dead node and a job whose handler returned an error are
// accounted IDENTICALLY, and only running both paths can show that. A recovery
// that grew its own copy of the retry policy would pass the old test and fail
// this one.
//
// The fixture starts with a failure already spent, so the backoff multiplier is
// 2 rather than 1: a copied policy that hardcoded a single backoff interval
// agrees with the shared one at multiplier 1 and diverges here. The comparison
// is NextRun minus LastRun — the backoff itself — because both paths stamp
// their own time.Now() and the absolute instants differ by microseconds.
func TestRecoverAbandonedRun_AccountsAsTheHandlerFailurePathDoes(t *testing.T) {
	recoveryStore := newMockJobStorage()
	recoveryEngine := New(DefaultConfig(), recoveryStore)
	abandoned := runningRecoveryJob()
	abandoned.FailCount = 1
	seedRunningJob(t, recoveryStore, abandoned)
	if _, err := recoveryEngine.RecoverAbandonedRun(
		context.Background(), abandoned, goodAttestation()); err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}
	viaRecovery := storedJob(t, recoveryStore, abandoned.ID)

	handlerStore := newMockJobStorage()
	handlerEngine := New(DefaultConfig(), handlerStore)
	failed := runningRecoveryJob()
	failed.FailCount = 1
	seedRunningJob(t, handlerStore, failed)
	handlerEngine.handleJobError(context.Background(), failed,
		&JobExecution{JobID: failed.ID, StartedAt: recoveryFiring},
		errors.New("handler returned an error"))
	viaHandler := storedJob(t, handlerStore, failed.ID)

	if viaRecovery.Status != viaHandler.Status {
		t.Errorf("recovery left the job %s and a handler failure left it %s",
			viaRecovery.Status, viaHandler.Status)
	}
	if viaRecovery.FailCount != viaHandler.FailCount {
		t.Errorf("recovery counted %d failures and a handler failure counted %d",
			viaRecovery.FailCount, viaHandler.FailCount)
	}
	if viaRecovery.RunCount != viaHandler.RunCount {
		t.Errorf("recovery counted %d runs and a handler failure counted %d",
			viaRecovery.RunCount, viaHandler.RunCount)
	}
	recoveryBackoff := viaRecovery.NextRun.Sub(viaRecovery.LastRun)
	handlerBackoff := viaHandler.NextRun.Sub(viaHandler.LastRun)
	if recoveryBackoff != handlerBackoff {
		t.Errorf("recovery scheduled a %s backoff and a handler failure scheduled %s: the "+
			"retry policy has been copied rather than shared", recoveryBackoff, handlerBackoff)
	}
	if recoveryBackoff <= DefaultConfig().RetryBackoff {
		t.Errorf("the backoff is %s, not above one interval: the fixture is not exercising "+
			"the multiplier, so a hardcoded interval would agree with the shared policy",
			recoveryBackoff)
	}
	if viaRecovery.LastError == viaHandler.LastError {
		t.Error("both paths recorded the same reason, so the fixture is not distinguishing them")
	}
}

func TestRecoverAbandonedRun_ReportsAFailedRead(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())
	storage.getJobErr = errors.New("store unreachable")

	_, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("RecoverAbandonedRun = %v; want a storage error", err)
	}
	// A read that failed says nothing about the job, so nothing may be
	// concluded from it — the same fail-closed direction the caller's own
	// liveness authority follows.
	if storageErr.Op != "GetJob" {
		t.Errorf("the error names operation %q, want GetJob", storageErr.Op)
	}
}

// TestRecoverAbandonedRun_StampsAnUnsetObservationTime covers the default the
// attestation documents: a missing timestamp is a formatting gap, not missing
// evidence, so the recovery fills it rather than refusing.
//
// The caller that leaves it unset is the firing checker, which holds no clock.
func TestRecoverAbandonedRun_StampsAnUnsetObservationTime(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	att := goodAttestation()
	att.ObservedAt = time.Time{}
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, att); err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}

	stored := storedJob(t, storage, job.ID)
	if strings.Contains(stored.LastError, "0001-01-01") {
		t.Errorf("an unset observation time was recorded as the zero time: %q", stored.LastError)
	}
	if !strings.Contains(stored.LastError, "observed by") {
		t.Errorf("the record does not name when it was observed: %q", stored.LastError)
	}
}

func TestInvalidAttestationError_NamesTheFieldAndTheReason(t *testing.T) {
	err := InvalidAttestationError{Field: "firing", Message: "binds to no execution"}
	if !strings.Contains(err.Error(), "firing") || !strings.Contains(err.Error(), "binds to no execution") {
		t.Errorf("the error drops its diagnosis: %q", err.Error())
	}
}

func TestRecoveryRefusal_RendersItsName(t *testing.T) {
	for _, refusal := range []RecoveryRefusal{RefusedNotRunning, RefusedFiringMoved} {
		if refusal.String() != string(refusal) {
			t.Errorf("%s renders as %q", string(refusal), refusal.String())
		}
	}
	// An unknown refusal still renders something an operator can act on, rather
	// than an empty message.
	unknown := RecoveryRefusedError{JobID: 7, Refusal: "invented"}
	if !strings.Contains(unknown.Error(), "invented") {
		t.Errorf("an unrecognised refusal renders as %q", unknown.Error())
	}
}

// TestRecoverAbandonedRun_HistoryIsWrittenFirstAndNeverLost is the ordering
// contract: the abandoned run's sole audit record is written BEFORE the job
// leaves Running, so every failure leaves the whole recovery retriable and
// the record can never be permanently lost. And the write is idempotent by
// lookup: a retry whose first attempt wrote history and then failed the
// transition completes without duplicating the record.
//
// Mutation evidence: restoring the old order (transition first) fails the
// still-Running assertion; removing the idempotency lookup fails the
// exactly-one-record assertion.
func TestRecoverAbandonedRun_HistoryIsWrittenFirstAndNeverLost(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	// A failed history write aborts BEFORE the transition: the job stays
	// Running and recovery stays possible.
	storage.saveExecutionErr = errors.New("history store unreachable")
	_, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("RecoverAbandonedRun = %v; want a storage error", err)
	}
	stored, getErr := storage.GetJob(context.Background(), job.ID, JobTenantID(job))
	if getErr != nil {
		t.Fatalf("GetJob: %v", getErr)
	}
	if stored.Status != JobStatusRunning {
		t.Fatalf("the job left Running (%s) before its history was written: the record is now unrecoverable", stored.Status)
	}

	// A failed transition AFTER the history write is retriable too, and the
	// retry does not write a second record.
	storage.saveExecutionErr = nil
	storage.updateJobErr = errors.New("job store unreachable")
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); !errors.As(err, &storageErr) {
		t.Fatalf("transition failure: %v; want a storage error", err)
	}
	storage.updateJobErr = nil
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("the retry after a failed transition did not complete: %v", err)
	}

	executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("history holds %d records for one abandoned run, want exactly 1 (the retry duplicated it)", len(executions))
	}
	stored, _ = storage.GetJob(context.Background(), job.ID, JobTenantID(job))
	if stored.Status == JobStatusRunning {
		t.Fatal("the completed retry left the job Running")
	}
}

// TestRecoverAbandonedRun_RefusesAClusteredEngine is the mode gate: on a
// clustered engine the stripe mutex is one node's exclusivity, not the
// fleet's, and recovery's unconditional write could overwrite a run another
// node concurrently completed. The capability is refused by name until a
// guarded storage transition exists.
//
// Mutation evidence: removing the claims gate lets the clustered engine
// conclude the run and fails the status assertion.
func TestRecoverAbandonedRun_RefusesAClusteredEngine(t *testing.T) {
	storage := newMockJobStorage()
	engine, err := NewWithClustering(DefaultConfig(), storage, "node-1", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	job := seedRunningJob(t, storage, runningRecoveryJob())

	_, err = engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var unsupported RecoveryUnsupportedClusteredError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want RecoveryUnsupportedClusteredError", err)
	}
	stored, getErr := storage.GetJob(context.Background(), job.ID, JobTenantID(job))
	if getErr != nil {
		t.Fatalf("GetJob: %v", getErr)
	}
	if stored.Status != JobStatusRunning {
		t.Fatalf("a refused recovery still moved the job to %s", stored.Status)
	}
}

// TestRecoverAbandonedRun_RetryWithUnsetObservationIsStillIdempotent is the
// idempotency contract under the caller that actually exercises it: the
// firing liveness checker leaves ObservedAt unset. When the evidence string
// resolved an unset observation to the CLOCK, a retry after a failed
// transition rendered different evidence, could not find the record its first
// attempt wrote, and duplicated the abandoned run's history. The observation
// must resolve from stable run data, so every retry renders the same record.
//
// Mutation evidence: restoring the clock default fails the exactly-one-record
// assertion.
func TestRecoverAbandonedRun_RetryWithUnsetObservationIsStillIdempotent(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, runningRecoveryJob())

	att := goodAttestation()
	att.ObservedAt = time.Time{} // as the firing checker sends it

	// First attempt: history lands, the transition fails.
	storage.updateJobErr = errors.New("job store unreachable")
	var storageErr ErrStorageError
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, att); !errors.As(err, &storageErr) {
		t.Fatalf("transition failure: %v; want a storage error", err)
	}

	// The retry, still with an unset observation, completes without writing a
	// second record.
	storage.updateJobErr = nil
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, att); err != nil {
		t.Fatalf("retry: %v", err)
	}

	executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("history holds %d records for one abandoned run, want exactly 1 "+
			"(the retry re-stamped the observation and duplicated it)", len(executions))
	}
}
