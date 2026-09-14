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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// resolveResumeState Tests
// =============================================================================

func TestResolveResumeState_AllStates(t *testing.T) {
	tests := []struct {
		name        string
		interrupted JobState
		expected    JobState
	}{
		{"initializing restarts from initializing", JobStateInitializing, JobStateInitializing},
		{"decomposing restarts from initializing", JobStateDecomposing, JobStateInitializing},
		{"scheduling restarts from decomposing", JobStateScheduling, JobStateDecomposing},
		{"assigning restarts from decomposing", JobStateAssigning, JobStateDecomposing},
		{"executing restarts from decomposing", JobStateExecuting, JobStateDecomposing},
		{"validating restarts from decomposing", JobStateValidating, JobStateDecomposing},
		{"reviewing restarts from decomposing", JobStateReviewing, JobStateDecomposing},
		{"redelegating restarts from decomposing", JobStateRedelegating, JobStateDecomposing},
		{"escalating resolves to failed", JobStateEscalating, JobStateFailed},
		{"completing restarts from completing", JobStateCompleting, JobStateCompleting},
		{"cleanup restarts from completing", JobStateCleanup, JobStateCompleting},
		{"unknown state restarts from initializing", JobState("unknown"), JobStateInitializing},
		{"empty state restarts from initializing", JobState(""), JobStateInitializing},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveResumeState(tc.interrupted)
			if result != tc.expected {
				t.Errorf("resolveResumeState(%q) = %q, want %q", tc.interrupted, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// Resumed launch tests
//
// A resumed job re-enters the state machine where the runner says an
// interrupted job restarts, carrying the ID and the recoverable state of the
// status it continues. These drive that through the envelope, as
// agentServiceImpl.resumeJob does.
// =============================================================================

func TestResumedRun_RestartsFromDecomposing(t *testing.T) {
	logger := &controlLoopMockLogger{}

	// Track decompose call count to return different responses:
	// First call: decompose (returns work items)
	// Second call: review (returns JSON approval via work item description)
	var decomposeCallCount int64
	mockSvc := &controlLoopMockService{
		decomposeFn: func() (*DecomposeTaskResponse, error) {
			call := decomposeCallCount
			decomposeCallCount++
			if call == 0 {
				// Decomposition: return work items.
				return &DecomposeTaskResponse{
					WorkItems: []*WorkItem{
						{ID: "w1", Title: "task-1", Description: "do thing", Priority: 1},
					},
				}, nil
			}
			// Review: return JSON approval via description.
			return &DecomposeTaskResponse{
				WorkItems: []*WorkItem{
					{Description: `{"approved": true, "feedback": "looks good"}`},
				},
			}, nil
		},
		backends: newTestBackends(),
		agents:   newTestAgents(),
		tasks: []*Task{
			{ID: "task-1", Subject: "task-1", Status: TaskStatusCompleted, Result: &TaskResult{Output: "done"}},
		},
	}

	// A job interrupted while executing restarts from decomposing: its
	// sessions are dead, so the lead re-decomposes and re-spawns.
	resumed := &JobStatus{JobID: "resumed-job-123", State: JobStateExecuting}
	launch := jobLaunch{
		Config: newTestConfig(), Agents: mockSvc, Logger: logger,
		JobID: resumed.JobID, Resumed: resumed,
	}
	run := launch.newRun(t)
	if run.StartState != JobStateDecomposing {
		t.Fatalf("start state = %q, want %q", run.StartState, JobStateDecomposing)
	}
	if err := run.execute(context.Background(), workflowRunner{}); err != nil {
		t.Fatalf("resumed run returned error: %v", err)
	}

	status := run.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.JobID != "resumed-job-123" {
		t.Errorf("job ID = %q, want %q", status.JobID, "resumed-job-123")
	}
	if status.State != JobStateCompleted {
		t.Errorf("state = %q, want %q", status.State, JobStateCompleted)
	}
}

func TestResumedRun_RestartsFromCompleting(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}

	// A job interrupted in cleanup was almost done: it restarts from
	// completing and goes through cleanup to completed.
	resumed := &JobStatus{JobID: "almost-done-job", State: JobStateCleanup}
	run, err := jobLaunch{
		Config: newTestConfig(), Agents: mockSvc, Logger: logger,
		JobID: resumed.JobID, Resumed: resumed,
	}.run(context.Background(), t)
	if err != nil {
		t.Fatalf("resumed run returned error: %v", err)
	}

	status := run.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.State != JobStateCompleted {
		t.Errorf("state = %q, want %q", status.State, JobStateCompleted)
	}
}

// TestResumedRun_UnknownStateIsRefused: a run entered at a state the loop
// has no handler for — what a runner naming a state the lead-driven machine
// does not implement would produce — fails typed rather than hanging.
func TestResumedRun_UnknownStateIsRefused(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}

	_, err := jobLaunch{
		Config: newTestConfig(), Agents: mockSvc, Logger: logger,
		JobID: "bad-state-job", StartState: JobState("nonexistent"),
	}.run(context.Background(), t)
	if err == nil {
		t.Fatal("expected error for unknown state")
	}

	var clErr *ControlLoopError
	if !errors.As(err, &clErr) {
		t.Errorf("expected ControlLoopError, got: %T", err)
	}
}

func TestResumedRun_ContextCancelled(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{
		decomposeErr: context.Canceled,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	resumed := &JobStatus{JobID: "cancelled-job", State: JobStateInitializing}
	_, err := jobLaunch{
		Config: newTestConfig(), Agents: mockSvc, Logger: logger,
		JobID: resumed.JobID, Resumed: resumed,
	}.run(ctx, t)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestResumedRun_PreservesIteration: the review round the interrupted job
// had reached is restored from its persisted status, so a resumed job does
// not get its iteration budget back.
func TestResumedRun_PreservesIteration(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}

	resumed := &JobStatus{JobID: "iter-job", State: JobStateCompleting, Iteration: 3}
	run, err := jobLaunch{
		Config: newTestConfig(), Agents: mockSvc, Logger: logger,
		JobID: resumed.JobID, Resumed: resumed,
	}.run(context.Background(), t)
	if err != nil {
		t.Fatalf("resumed run error: %v", err)
	}

	status := run.Status()
	if status.Iteration != 3 {
		t.Errorf("iteration = %d, want 3", status.Iteration)
	}
}

// =============================================================================
// resumeJob Tests
// =============================================================================

func TestResumeJob_NilConfig(t *testing.T) {
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:  store,
		logger: logger,
	}

	status := &JobStatus{
		JobID:    "job-no-config",
		TenantID: "tenant-1",
		State:    JobStateExecuting,
		Config:   nil,
	}

	err := svc.resumeJob(context.Background(), status)
	if err == nil {
		t.Fatal("expected error for nil config")
	}

	var resumeErr *JobResumeError
	if !errors.As(err, &resumeErr) {
		t.Errorf("expected JobResumeError, got: %T", err)
	}
	if !strings.Contains(err.Error(), "no config") {
		t.Errorf("error = %q, want contains %q", err.Error(), "no config")
	}
}

func TestResumeJob_EmptyJobID(t *testing.T) {
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:  store,
		logger: logger,
	}

	status := &JobStatus{
		JobID:    "",
		TenantID: "tenant-1",
		State:    JobStateExecuting,
		Config:   newTestConfig(),
	}

	err := svc.resumeJob(context.Background(), status)
	if err == nil {
		t.Fatal("expected error for empty job ID")
	}

	var resumeErr *JobResumeError
	if !errors.As(err, &resumeErr) {
		t.Errorf("expected JobResumeError, got: %T", err)
	}
	if !strings.Contains(err.Error(), "no job ID") {
		t.Errorf("error = %q, want contains %q", err.Error(), "no job ID")
	}
}

func TestResumeJob_TerminalResolvedState(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	config := newTestConfig()
	status := &JobStatus{
		JobID:    "job-escalating",
		TenantID: "test-tenant",
		State:    JobStateEscalating, // resolves to failed
		Config:   config,
	}

	err := svc.resumeJob(context.Background(), status)
	if err != nil {
		t.Fatalf("expected nil error for terminal resolve, got: %v", err)
	}

	// Verify the job was NOT stored in the jobs map (no goroutine launched).
	_, loaded := svc.jobs.Load("test-tenant/job-escalating")
	if loaded {
		t.Error("expected job NOT to be in running jobs map")
	}

	// Verify info log about terminal state.
	found := false
	for _, msg := range logger.infoLines() {
		if strings.Contains(msg, "resolved to terminal state") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected terminal state log, got: %v", logger.infoLines())
	}
}

func TestResumeJob_Success_LaunchesGoroutine(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	config := newTestConfig()
	status := &JobStatus{
		JobID:       "job-resume-ok",
		TenantID:    "test-tenant",
		State:       JobStateExecuting,
		Iteration:   2,
		Config:      config,
		Description: "resumable job",
		WorkItems: []*WorkItem{
			{ID: "w1", Title: "task-1", Description: "do thing"},
		},
		Memory: &JobMemory{
			SuccessfulApproaches: []string{"prior success"},
		},
	}

	err := svc.resumeJob(context.Background(), status)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the job was stored in the jobs map.
	val, loaded := svc.jobs.Load("test-tenant/job-resume-ok")
	if !loaded {
		t.Fatal("expected job to be in running jobs map")
	}

	job, ok := val.(*runningJob)
	if !ok {
		t.Fatal("expected *runningJob type")
	}

	// Cancel the job to clean up the goroutine.
	job.cancel()

	// Verify the resume log.
	found := false
	for _, msg := range logger.infoLines() {
		if strings.Contains(msg, "resumed job") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected resume log, got: %v", logger.infoLines())
	}
}

func TestResumeJob_WithWorkflowLoader(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	// Create a workflow loader with a registered workflow.
	wl := NewWorkflowLoader()
	wfDef := &WorkflowDefinition{
		Name: "test-workflow",
		Mode: WorkflowModeAutonomous,
		Settings: WorkflowSettings{
			MaxIterations: 5,
		},
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "ollama", Model: "test-model"},
		},
	}
	if regErr := wl.Register("test-tenant", wfDef); regErr != nil {
		t.Fatalf("register workflow failed: %v", regErr)
	}

	svc := &agentServiceImpl{
		store:          store,
		kvStore:        kv,
		workflowLoader: wl,
		logger:         logger,
	}

	config := newTestConfig()
	status := &JobStatus{
		JobID:        "job-with-workflow",
		TenantID:     "test-tenant",
		State:        JobStateScheduling,
		Config:       config,
		WorkflowName: "test-workflow",
	}

	err := svc.resumeJob(context.Background(), status)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the job was stored.
	val, loaded := svc.jobs.Load("test-tenant/job-with-workflow")
	if !loaded {
		t.Fatal("expected job to be in running jobs map")
	}

	job := val.(*runningJob)
	job.cancel() // Clean up goroutine.
}

func TestResumeJob_MissingWorkflow_Warns(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	// Empty workflow loader - workflow won't be found.
	wl := NewWorkflowLoader()

	svc := &agentServiceImpl{
		store:          store,
		kvStore:        kv,
		workflowLoader: wl,
		logger:         logger,
	}

	config := newTestConfig()
	status := &JobStatus{
		JobID:        "job-missing-wf",
		TenantID:     "test-tenant",
		State:        JobStateExecuting,
		Config:       config,
		WorkflowName: "nonexistent-workflow",
	}

	// Should succeed even though workflow is missing (non-fatal).
	err := svc.resumeJob(context.Background(), status)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Should have a warning about the missing workflow.
	found := false
	for _, msg := range logger.warnLines() {
		if strings.Contains(msg, "could not reload workflow") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about missing workflow, got: %v", logger.warnLines())
	}

	// Clean up.
	val, loaded := svc.jobs.Load("test-tenant/job-missing-wf")
	if loaded {
		val.(*runningJob).cancel()
	}
}

func TestResumeJob_NilMemory(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	config := newTestConfig()
	status := &JobStatus{
		JobID:    "job-no-memory",
		TenantID: "test-tenant",
		State:    JobStateDecomposing,
		Config:   config,
		Memory:   nil, // No accumulated memory.
	}

	err := svc.resumeJob(context.Background(), status)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Clean up.
	val, loaded := svc.jobs.Load("test-tenant/job-no-memory")
	if loaded {
		val.(*runningJob).cancel()
	}
}

// =============================================================================
// Recovery with Resumption Integration Tests
// =============================================================================

func TestRecoverJobs_ResumesJobWithConfig(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	config := newTestConfig()
	executing := &JobStatus{
		JobID:       "job-resumable",
		TenantID:    "test-tenant",
		State:       JobStateExecuting,
		Description: "resumable job",
		Iteration:   1,
		Config:      config,
		WorkItems: []*WorkItem{
			{ID: "w1", Title: "task-1", Description: "do thing"},
		},
	}
	data, _ := json.Marshal(executing)
	key := jobPersistenceKey("test-tenant", "job-resumable")
	if putErr := kv.Put(context.Background(), key, data); putErr != nil {
		t.Fatalf("setup failed: %v", putErr)
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the job was resumed (stored in jobs map).
	val, loaded := svc.jobs.Load("test-tenant/job-resumable")
	if !loaded {
		t.Fatal("expected job to be in running jobs map after recovery")
	}

	// Clean up goroutine.
	val.(*runningJob).cancel()

	// Verify recovery count log.
	found := false
	for _, msg := range logger.infoLines() {
		if strings.Contains(msg, "resumed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected resume log, got: %v", logger.infoLines())
	}
}

func TestRecoverJobs_MixedTerminalAndResumable(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	// Terminal job: should be skipped.
	completed := &JobStatus{
		JobID:    "job-done",
		TenantID: "tenant-1",
		State:    JobStateCompleted,
	}
	completedData, _ := json.Marshal(completed)
	completedKey := jobPersistenceKey("tenant-1", "job-done")
	if putErr := kv.Put(context.Background(), completedKey, completedData); putErr != nil {
		t.Fatalf("setup failed: %v", putErr)
	}

	// Resumable job.
	config := newTestConfig()
	config.TenantID = "tenant-1"
	resumable := &JobStatus{
		JobID:    "job-active",
		TenantID: "tenant-1",
		State:    JobStateScheduling,
		Config:   config,
	}
	resumableData, _ := json.Marshal(resumable)
	resumableKey := jobPersistenceKey("tenant-1", "job-active")
	if putErr := kv.Put(context.Background(), resumableKey, resumableData); putErr != nil {
		t.Fatalf("setup failed: %v", putErr)
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Terminal job should NOT be in the jobs map.
	_, loadedTerminal := svc.jobs.Load("tenant-1/job-done")
	if loadedTerminal {
		t.Error("terminal job should not be in running jobs map")
	}

	// Resumable job SHOULD be in the jobs map.
	val, loadedResumable := svc.jobs.Load("tenant-1/job-active")
	if !loadedResumable {
		t.Fatal("resumable job should be in running jobs map")
	}
	val.(*runningJob).cancel()
}

// =============================================================================
// updateStatus Recoverable State Tests
// =============================================================================

func TestControlLoop_UpdateStatus_IncludesRecoverableState(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.job.Config = newTestConfig()
	cl.job.JobID = "job-recoverable"
	cl.workItems = []*WorkItem{
		{ID: "w1", Title: "task-1"},
	}
	cl.job.memory = NewJobMemory()
	cl.job.memory.AddSuccess("prior approach")

	cl.updateStatus(JobStateExecuting)

	status := cl.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Config == nil {
		t.Error("expected non-nil Config in status")
	}
	if len(status.WorkItems) != 1 {
		t.Errorf("WorkItems count = %d, want 1", len(status.WorkItems))
	}
	if status.Memory == nil {
		t.Error("expected non-nil Memory in status")
	}
}

func TestControlLoop_UpdateStatus_WorkflowName(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.job.Config = newTestConfig()
	cl.job.JobID = "job-wf-name"
	cl.workflow = &WorkflowDefinition{
		Name: "my-workflow",
		Mode: WorkflowModeAutonomous,
	}

	cl.updateStatus(JobStateDecomposing)

	status := cl.Status()
	if status.WorkflowName != "my-workflow" {
		t.Errorf("WorkflowName = %q, want %q", status.WorkflowName, "my-workflow")
	}
}

func TestControlLoop_UpdateStatus_PersistIncludesRecoverableState(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.job.Config = newTestConfig()
	cl.job.JobID = "job-persist-recoverable"
	cl.workItems = []*WorkItem{{ID: "w1", Title: "t1"}}

	var persisted *JobStatus
	cl.job.persistFn = func(status *JobStatus) {
		copied := *status
		persisted = &copied
	}

	cl.updateStatus(JobStateScheduling)

	if persisted == nil {
		t.Fatal("expected persist to be called")
	}
	if persisted.Config == nil {
		t.Error("persisted status should include Config")
	}
	if len(persisted.WorkItems) != 1 {
		t.Errorf("persisted WorkItems count = %d, want 1", len(persisted.WorkItems))
	}
}

// =============================================================================
// workflowName Tests
// =============================================================================

func TestControlLoop_WorkflowName_FromWorkflow(t *testing.T) {
	cl := newTestLoop(newTestConfig())
	cl.workflow = &WorkflowDefinition{
		Name: "wf-from-def",
		Mode: WorkflowModeAutonomous,
	}
	if got := cl.workflowName(); got != "wf-from-def" {
		t.Errorf("workflowName() = %q, want %q", got, "wf-from-def")
	}
}

func TestControlLoop_WorkflowName_FromConfig(t *testing.T) {
	config := newTestConfig()
	config.WorkflowName = "wf-from-config"
	cl := newTestLoop(config)
	if got := cl.workflowName(); got != "wf-from-config" {
		t.Errorf("workflowName() = %q, want %q", got, "wf-from-config")
	}
}

func TestControlLoop_WorkflowName_NilWorkflowAndConfig(t *testing.T) {
	cl := newTestLoop(&JobConfig{})
	if got := cl.workflowName(); got != "" {
		t.Errorf("workflowName() = %q, want empty", got)
	}
}

func TestControlLoop_WorkflowName_WorkflowPriority(t *testing.T) {
	config := newTestConfig()
	config.WorkflowName = "config-wf"
	cl := newTestLoop(config)
	cl.workflow = &WorkflowDefinition{
		Name: "def-wf",
		Mode: WorkflowModeAutonomous,
	}
	// Workflow definition name takes priority over config.
	if got := cl.workflowName(); got != "def-wf" {
		t.Errorf("workflowName() = %q, want %q", got, "def-wf")
	}
}

// =============================================================================
// JobResumeError Tests
// =============================================================================

func TestJobResumeError_Full(t *testing.T) {
	cause := errors.New("backend unavailable")
	err := &JobResumeError{
		JobID:   "job-err",
		Message: "resume failed",
		Cause:   cause,
	}

	if !strings.Contains(err.Error(), "job=job-err") {
		t.Errorf("error missing job ID: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "resume failed") {
		t.Errorf("error missing message: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "backend unavailable") {
		t.Errorf("error missing cause: %s", err.Error())
	}
	if !errors.Is(err, ErrJobResumeFailed) {
		t.Error("expected errors.Is to match ErrJobResumeFailed")
	}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to match cause")
	}
}

func TestJobResumeError_NoCause(t *testing.T) {
	err := &JobResumeError{
		JobID:   "job-err",
		Message: "no cause",
	}

	if !errors.Is(err, ErrJobResumeFailed) {
		t.Error("expected errors.Is to match ErrJobResumeFailed")
	}

	unwrapped := err.Unwrap()
	if len(unwrapped) != 1 {
		t.Errorf("expected 1 unwrapped error, got %d", len(unwrapped))
	}
}

func TestJobResumeError_NoJobID(t *testing.T) {
	err := &JobResumeError{
		Message: "missing id",
	}

	errStr := err.Error()
	if strings.Contains(errStr, "job=") {
		t.Errorf("error should not contain job= when JobID is empty: %s", errStr)
	}
	if !strings.Contains(errStr, "missing id") {
		t.Errorf("error missing message: %s", errStr)
	}
}

// =============================================================================
// JobStatus Serialization with Recoverable State Tests
// =============================================================================

func TestJobStatus_MarshalUnmarshal_WithRecoverableState(t *testing.T) {
	config := &JobConfig{
		TenantID:      "tenant-1",
		Description:   "test job",
		LeadBackend:   "ollama",
		LeadModel:     "gpt-4",
		ProjectID:     "proj-1",
		MaxIterations: 10,
		WorkflowName:  "my-workflow",
	}

	memory := NewJobMemory()
	memory.AddSuccess("approach A")
	memory.AddFailure("task-1", "approach B", "timeout", 1)
	memory.AddConstraint("max 5 files")
	memory.AddFeedback("improve error handling")

	status := &JobStatus{
		JobID:       "job-ser",
		TenantID:    "tenant-1",
		State:       JobStateExecuting,
		Iteration:   2,
		TaskCount:   3,
		Description: "serializable job",
		StartedAt:   time.Now().UnixNano(),
		UpdatedAt:   time.Now().UnixNano(),
		WorkItems: []*WorkItem{
			{ID: "w1", Title: "task-1", Description: "first task", Priority: 1},
			{ID: "w2", Title: "task-2", Description: "second task", Priority: 2, Dependencies: []string{"w1"}},
		},
		Config:       config,
		Memory:       memory,
		WorkflowName: "my-workflow",
	}

	data, marshalErr := json.Marshal(status)
	if marshalErr != nil {
		t.Fatalf("marshal failed: %v", marshalErr)
	}

	var loaded JobStatus
	if unmarshalErr := json.Unmarshal(data, &loaded); unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}

	// Verify core fields.
	if loaded.JobID != "job-ser" {
		t.Errorf("JobID = %q, want %q", loaded.JobID, "job-ser")
	}
	if loaded.State != JobStateExecuting {
		t.Errorf("State = %q, want %q", loaded.State, JobStateExecuting)
	}

	// Verify work items.
	if len(loaded.WorkItems) != 2 {
		t.Fatalf("WorkItems count = %d, want 2", len(loaded.WorkItems))
	}
	if loaded.WorkItems[0].Title != "task-1" {
		t.Errorf("WorkItems[0].Title = %q, want %q", loaded.WorkItems[0].Title, "task-1")
	}
	if len(loaded.WorkItems[1].Dependencies) != 1 {
		t.Errorf("WorkItems[1] dependencies count = %d, want 1", len(loaded.WorkItems[1].Dependencies))
	}

	// Verify config.
	if loaded.Config == nil {
		t.Fatal("expected non-nil Config")
	}
	if loaded.Config.TenantID != "tenant-1" {
		t.Errorf("Config.TenantID = %q, want %q", loaded.Config.TenantID, "tenant-1")
	}
	if loaded.Config.LeadBackend != "ollama" {
		t.Errorf("Config.LeadBackend = %q, want %q", loaded.Config.LeadBackend, "ollama")
	}
	if loaded.Config.MaxIterations != 10 {
		t.Errorf("Config.MaxIterations = %d, want 10", loaded.Config.MaxIterations)
	}
	if loaded.Config.WorkflowName != "my-workflow" {
		t.Errorf("Config.WorkflowName = %q, want %q", loaded.Config.WorkflowName, "my-workflow")
	}

	// Verify memory.
	if loaded.Memory == nil {
		t.Fatal("expected non-nil Memory")
	}
	if len(loaded.Memory.SuccessfulApproaches) != 1 {
		t.Errorf("Memory.SuccessfulApproaches = %d, want 1", len(loaded.Memory.SuccessfulApproaches))
	}
	if len(loaded.Memory.FailedApproaches) != 1 {
		t.Errorf("Memory.FailedApproaches = %d, want 1", len(loaded.Memory.FailedApproaches))
	}
	if len(loaded.Memory.Constraints) != 1 {
		t.Errorf("Memory.Constraints = %d, want 1", len(loaded.Memory.Constraints))
	}
	if len(loaded.Memory.ReviewFeedback) != 1 {
		t.Errorf("Memory.ReviewFeedback = %d, want 1", len(loaded.Memory.ReviewFeedback))
	}

	// Verify workflow name.
	if loaded.WorkflowName != "my-workflow" {
		t.Errorf("WorkflowName = %q, want %q", loaded.WorkflowName, "my-workflow")
	}
}

func TestJobStatus_MarshalUnmarshal_EmptyRecoverableState(t *testing.T) {
	status := &JobStatus{
		JobID:    "job-empty",
		TenantID: "tenant-1",
		State:    JobStateInitializing,
	}

	data, marshalErr := json.Marshal(status)
	if marshalErr != nil {
		t.Fatalf("marshal failed: %v", marshalErr)
	}

	var loaded JobStatus
	if unmarshalErr := json.Unmarshal(data, &loaded); unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}

	// omitempty fields should result in nil/zero after round-trip.
	if loaded.WorkItems != nil {
		t.Errorf("expected nil WorkItems, got %v", loaded.WorkItems)
	}
	if loaded.Config != nil {
		t.Errorf("expected nil Config, got %v", loaded.Config)
	}
	if loaded.Memory != nil {
		t.Errorf("expected nil Memory, got %v", loaded.Memory)
	}
	if loaded.WorkflowName != "" {
		t.Errorf("expected empty WorkflowName, got %q", loaded.WorkflowName)
	}
}

// =============================================================================
// RebuildDedupSets Tests
// =============================================================================

func TestJobMemory_RebuildDedupSets(t *testing.T) {
	mem := &JobMemory{
		SuccessfulApproaches: []string{"approach-A", "approach-B"},
		FailedApproaches: []FailedApproach{
			{WorkItemTitle: "t1", Approach: "a1", Reason: "r1", Iteration: 1},
		},
		Constraints:    []string{"constraint-1"},
		ReviewFeedback: []string{"feedback-1"},
	}

	// Before rebuild, dedup sets are nil (as after JSON unmarshal).
	mem.RebuildDedupSets()

	// Adding a duplicate should be a no-op.
	mem.AddSuccess("approach-A")
	if len(mem.SuccessfulApproaches) != 2 {
		t.Errorf("expected 2 successes after duplicate add, got %d", len(mem.SuccessfulApproaches))
	}

	mem.AddFailure("t1", "a1", "r1", 1)
	if len(mem.FailedApproaches) != 1 {
		t.Errorf("expected 1 failure after duplicate add, got %d", len(mem.FailedApproaches))
	}

	mem.AddConstraint("constraint-1")
	if len(mem.Constraints) != 1 {
		t.Errorf("expected 1 constraint after duplicate add, got %d", len(mem.Constraints))
	}

	mem.AddFeedback("feedback-1")
	if len(mem.ReviewFeedback) != 1 {
		t.Errorf("expected 1 feedback after duplicate add, got %d", len(mem.ReviewFeedback))
	}

	// Adding new entries should work.
	mem.AddSuccess("approach-C")
	if len(mem.SuccessfulApproaches) != 3 {
		t.Errorf("expected 3 successes, got %d", len(mem.SuccessfulApproaches))
	}
}

func TestJobMemory_RebuildDedupSets_Empty(t *testing.T) {
	mem := &JobMemory{}

	// Should not panic on empty memory.
	mem.RebuildDedupSets()

	// Should be able to add after rebuild.
	mem.AddSuccess("new-approach")
	if len(mem.SuccessfulApproaches) != 1 {
		t.Errorf("expected 1 success, got %d", len(mem.SuccessfulApproaches))
	}
}

// =============================================================================
// SetWorkflowLoader Tests
// =============================================================================

func TestSetWorkflowLoader(t *testing.T) {
	store := NewInMemoryAgentStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		store:  store,
		logger: logger,
	}

	wl := NewWorkflowLoader()
	svc.SetWorkflowLoader(wl)

	if svc.workflowLoader != wl {
		t.Error("expected workflowLoader to be set")
	}
}

func TestSetWorkflowLoader_Nil(t *testing.T) {
	store := NewInMemoryAgentStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		store:  store,
		logger: logger,
	}

	svc.SetWorkflowLoader(nil)

	if svc.workflowLoader != nil {
		t.Error("expected workflowLoader to be nil")
	}
}
