// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"context"
	"testing"
)

// newProvenanceRun is a run with nothing wired but a config: the stage history
// and the agent attribution are the run's own bookkeeping and need no service.
func newProvenanceRun(t *testing.T) *JobRun {
	t.Helper()
	return NewJobRun(JobRunConfig{
		JobID:  "job-prov",
		Config: &JobConfig{TenantID: "acme", TaskID: "task-1", Attempt: 2},
		Logger: &agentTestLogger{},
	})
}

func TestJobRunRecordsEveryStageItEnters(t *testing.T) {
	t.Parallel()

	run := newProvenanceRun(t)
	if err := run.SetState(context.Background(), JobStateInitializing); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	run.markStage(JobStagePrepare)
	run.markStage(JobStageReviewable)
	run.markStage(JobStageComplete)

	status := run.Status()
	if status == nil {
		t.Fatal("the run published no status")
	}
	if len(status.Stages) != 3 {
		t.Fatalf("stages = %+v, want three", status.Stages)
	}
	for i, want := range []JobStage{JobStagePrepare, JobStageReviewable, JobStageComplete} {
		if status.Stages[i].Stage != want {
			t.Errorf("stage %d = %q, want %q", i, status.Stages[i].Stage, want)
		}
		if status.Stages[i].EnteredAt <= 0 {
			t.Errorf("stage %d carries no instant", i)
		}
	}
	if status.Stage != string(JobStageComplete) {
		t.Errorf("Stage = %q, want the last moment entered", status.Stage)
	}
}

func TestJobRunRecordsARepeatedStageAgain(t *testing.T) {
	t.Parallel()

	// A job that reviews, sends work back and reviews again entered
	// reviewable twice; a timeline that collapsed them would hide a round.
	run := newProvenanceRun(t)
	if err := run.SetState(context.Background(), JobStateExecuting); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	run.markStage(JobStageReviewable)
	run.markStage(JobStageReviewable)

	if got := len(run.Status().Stages); got != 2 {
		t.Errorf("stages = %d, want the moment recorded both times", got)
	}
}

func TestJobRunPublishesNoStagesBeforeAnyMomentRuns(t *testing.T) {
	t.Parallel()

	run := newProvenanceRun(t)
	if err := run.SetState(context.Background(), JobStateInitializing); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if got := run.Status().Stages; got != nil {
		t.Errorf("stages = %+v, want none before a moment runs", got)
	}
}

func TestJobRunRecordAgentKeepsTheFirstAgentThatTookWork(t *testing.T) {
	t.Parallel()

	run := newProvenanceRun(t)
	run.RecordAgent("agent-worker-1")
	run.RecordAgent("agent-worker-2")
	if got := run.AgentID(); got != "agent-worker-1" {
		t.Errorf("agent = %q, want the first agent to take work", got)
	}

	if err := run.SetState(context.Background(), JobStateExecuting); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if got := run.Status().AgentID; got != "agent-worker-1" {
		t.Errorf("published agent = %q, want agent-worker-1", got)
	}
}

func TestJobRunRecordAgentIgnoresAnEmptyID(t *testing.T) {
	t.Parallel()

	// A spawn that landed on this daemon's own in-process backend names no
	// fleet node; recording "" would make AgentID unusable for every later
	// spawn that does name one.
	run := newProvenanceRun(t)
	run.RecordAgent("")
	if got := run.AgentID(); got != "" {
		t.Errorf("agent = %q, want empty", got)
	}
	run.RecordAgent("agent-remote-1")
	if got := run.AgentID(); got != "agent-remote-1" {
		t.Errorf("agent = %q, want agent-remote-1 after a real spawn", got)
	}
}

func TestSpawnAgentIDReadsABackendBoundToAFleetAgent(t *testing.T) {
	t.Parallel()

	backend := &RemoteACPBackend{registration: &FleetNode{ID: "agent-7", Host: "node-a"}}
	if got := spawnAgentID(backend); got != "agent-7" {
		t.Errorf("spawnAgentID = %q, want agent-7", got)
	}
}

func TestSpawnAgentIDIsEmptyForABackendBoundToNoAgent(t *testing.T) {
	t.Parallel()

	if got := spawnAgentID(nil); got != "" {
		t.Errorf("spawnAgentID(nil) = %q, want empty", got)
	}
	unbound := &RemoteACPBackend{}
	if got := spawnAgentID(unbound); got != "" {
		t.Errorf("spawnAgentID of an unregistered backend = %q, want empty", got)
	}
}

// =============================================================================
// JobRun.Verify
// =============================================================================

// newVerifyingRun is a run whose validator registry holds one kind that
// answers whatever the test told it to.
func newVerifyingRun(t *testing.T, answers map[string]bool) *JobRun {
	t.Helper()
	registry := NewValidatorRegistry()
	err := registry.Register("test_command", func(_ context.Context, _ *JobContext, criterion ReviewCriterion) (ValidationResult, error) {
		passed := answers[criterion.Name]
		message := "refused"
		if passed {
			message = "passed"
		}
		return ValidationResult{Name: criterion.Name, Passed: passed, Message: message}, nil
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	run := NewJobRun(JobRunConfig{
		JobID:  "job-verify",
		Config: &JobConfig{TenantID: "acme"},
		Logger: &agentTestLogger{},
	})
	run.validatorRegistry = registry
	return run
}

func TestJobRunVerifyRecordsWhatTheCommandsSaid(t *testing.T) {
	t.Parallel()

	run := newVerifyingRun(t, map[string]bool{"make build": true, "make test": false})
	results, passed, err := run.Verify(context.Background(), []ReviewCriterion{
		{Name: "make build", Validator: "test_command", Required: true},
		{Name: "make test", Validator: "test_command", Required: true},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if passed {
		t.Error("a required command that refused reported the pipeline passed")
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two", results)
	}

	if err := run.SetState(context.Background(), JobStateValidating); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	published := run.Status().Verification
	if len(published) != 2 || published[1].Name != "make test" || published[1].Passed {
		t.Errorf("published verification = %+v, want the refusal on the record", published)
	}
}

func TestJobRunVerifyLeavesTheLastRoundAloneWhenNothingRan(t *testing.T) {
	t.Parallel()

	// A binding with no verification commands verifies nothing; erasing the
	// round before it would take the evidence away from the reader.
	run := newVerifyingRun(t, map[string]bool{"make build": false})
	if _, _, err := run.Verify(context.Background(), []ReviewCriterion{
		{Name: "make build", Validator: "test_command", Required: true},
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, passed, err := run.Verify(context.Background(), nil); err != nil || !passed {
		t.Fatalf("Verify(nil) = passed %v err %v, want passed", passed, err)
	}
	if got := run.verificationResults(); len(got) != 1 || got[0].Name != "make build" {
		t.Errorf("verification = %+v, want the earlier round kept", got)
	}
}

func TestJobRunVerifyRefusesAnUnregisteredValidator(t *testing.T) {
	t.Parallel()

	run := newVerifyingRun(t, nil)
	_, passed, err := run.Verify(context.Background(), []ReviewCriterion{
		{Name: "make build", Validator: "nothing_registered", Required: true},
	})
	if err == nil {
		t.Fatal("an unregistered validator kind was accepted")
	}
	if passed {
		t.Error("a pipeline that could not run reported it passed")
	}
	if got := run.verificationResults(); got != nil {
		t.Errorf("verification = %+v, want nothing recorded for a pipeline that did not run", got)
	}
}
