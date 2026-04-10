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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock KV Store for persistence tests
// =============================================================================

// mockKVStore implements interfaces.KVStore using an in-memory sync.Map.
type mockKVStore struct {
	data    sync.Map
	putErr  error
	getErr  error
	scanErr error
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{}
}

func (m *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	if m.putErr != nil {
		return m.putErr
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	m.data.Store(key, copied)
	return nil
}

func (m *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	val, ok := m.data.Load(key)
	if !ok {
		return nil, errors.New("key not found: " + key)
	}
	return val.([]byte), nil
}

func (m *mockKVStore) Delete(_ context.Context, key string) error {
	m.data.Delete(key)
	return nil
}

func (m *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	var scanErr error
	m.data.Range(func(k, v interface{}) bool {
		key := k.(string)
		if strings.HasPrefix(key, prefix) {
			if err := fn(key, v.([]byte)); err != nil {
				scanErr = err
				return false
			}
		}
		return true
	})
	return scanErr
}

func (m *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	m.data.Range(func(k, _ interface{}) bool {
		key := k.(string)
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return true
	})
	return keys, nil
}

func (m *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data.Load(key)
	return ok, nil
}

// Compile-time interface check.
var _ interfaces.KVStore = (*mockKVStore)(nil)

// =============================================================================
// Persistence Tests
// =============================================================================

func TestPersistJobStatus_Success(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	status := &JobStatus{
		JobID:       "job-123",
		TenantID:    "tenant-abc",
		State:       JobStateExecuting,
		Iteration:   2,
		TaskCount:   5,
		Description: "test persist",
		StartedAt:   time.Now().UnixNano(),
		UpdatedAt:   time.Now().UnixNano(),
	}

	svc.persistJobStatus("tenant-abc", status)

	// Verify the status was persisted to the KV store.
	key := jobPersistenceKey("tenant-abc", "job-123")
	data, err := kv.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var persisted JobStatus
	if unmarshalErr := json.Unmarshal(data, &persisted); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal persisted status: %v", unmarshalErr)
	}

	if persisted.JobID != "job-123" {
		t.Errorf("job ID = %q, want %q", persisted.JobID, "job-123")
	}
	if persisted.State != JobStateExecuting {
		t.Errorf("state = %q, want %q", persisted.State, JobStateExecuting)
	}
	if persisted.TenantID != "tenant-abc" {
		t.Errorf("tenant ID = %q, want %q", persisted.TenantID, "tenant-abc")
	}
	if persisted.Description != "test persist" {
		t.Errorf("description = %q, want %q", persisted.Description, "test persist")
	}
}

func TestPersistJobStatus_KVStoreNil(t *testing.T) {
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: nil,
		logger:  logger,
	}

	// Should not panic when kvStore is nil.
	svc.persistJobStatus("tenant-abc", &JobStatus{
		JobID: "job-nil",
		State: JobStateExecuting,
	})

	// No error logged, no-op.
	if len(logger.warnMessages) > 0 {
		t.Errorf("expected no warnings, got: %v", logger.warnMessages)
	}
}

func TestPersistJobStatus_PutError(t *testing.T) {
	kv := newMockKVStore()
	kv.putErr = errors.New("storage unavailable")
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// Should log warning but not panic.
	svc.persistJobStatus("tenant-abc", &JobStatus{
		JobID: "job-fail",
		State: JobStateExecuting,
	})

	if len(logger.warnMessages) == 0 {
		t.Error("expected a warning to be logged for put failure")
	}
}

func TestLoadJobStatus_Success(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// Pre-populate the KV store.
	status := &JobStatus{
		JobID:       "job-456",
		TenantID:    "tenant-xyz",
		State:       JobStateCompleted,
		Description: "loaded job",
		StartedAt:   time.Now().UnixNano(),
		UpdatedAt:   time.Now().UnixNano(),
	}
	data, _ := json.Marshal(status)
	key := jobPersistenceKey("tenant-xyz", "job-456")
	if err := kv.Put(context.Background(), key, data); err != nil {
		t.Fatalf("setup put failed: %v", err)
	}

	loaded, err := svc.loadJobStatus("tenant-xyz", "job-456")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if loaded.JobID != "job-456" {
		t.Errorf("job ID = %q, want %q", loaded.JobID, "job-456")
	}
	if loaded.State != JobStateCompleted {
		t.Errorf("state = %q, want %q", loaded.State, JobStateCompleted)
	}
}

func TestLoadJobStatus_NoKVStore(t *testing.T) {
	svc := &agentServiceImpl{
		kvStore: nil,
	}

	_, err := svc.loadJobStatus("tenant", "job-id")
	if err == nil {
		t.Fatal("expected error for nil KV store")
	}

	var persErr *JobPersistenceError
	if !errors.As(err, &persErr) {
		t.Errorf("expected JobPersistenceError, got: %T", err)
	}
}

func TestLoadJobStatus_GetError(t *testing.T) {
	kv := newMockKVStore()
	kv.getErr = errors.New("read failure")
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	_, err := svc.loadJobStatus("tenant", "job-missing")
	if err == nil {
		t.Fatal("expected error for get failure")
	}

	var persErr *JobPersistenceError
	if !errors.As(err, &persErr) {
		t.Errorf("expected JobPersistenceError, got: %T", err)
	}
}

func TestLoadJobStatus_CorruptData(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// Store invalid JSON.
	key := jobPersistenceKey("tenant", "job-corrupt")
	if err := kv.Put(context.Background(), key, []byte("not json")); err != nil {
		t.Fatalf("setup put failed: %v", err)
	}

	_, err := svc.loadJobStatus("tenant", "job-corrupt")
	if err == nil {
		t.Fatal("expected error for corrupt data")
	}

	var persErr *JobPersistenceError
	if !errors.As(err, &persErr) {
		t.Errorf("expected JobPersistenceError, got: %T", err)
	}
}

// =============================================================================
// Recovery Tests
// =============================================================================

func TestRecoverJobs_NoKVStore(t *testing.T) {
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: nil,
		logger:  logger,
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for nil KV store, got: %v", err)
	}
}

func TestRecoverJobs_NoJobs(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestRecoverJobs_TerminalJobsSkipped(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// Pre-populate with terminal jobs.
	completed := &JobStatus{
		JobID:    "job-done",
		TenantID: "tenant-1",
		State:    JobStateCompleted,
	}
	failed := &JobStatus{
		JobID:    "job-fail",
		TenantID: "tenant-1",
		State:    JobStateFailed,
	}

	for _, status := range []*JobStatus{completed, failed} {
		data, _ := json.Marshal(status)
		key := jobPersistenceKey(status.TenantID, status.JobID)
		if err := kv.Put(context.Background(), key, data); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Terminal jobs should not be modified.
	data, getErr := kv.Get(context.Background(), jobPersistenceKey("tenant-1", "job-done"))
	if getErr != nil {
		t.Fatalf("get failed: %v", getErr)
	}
	var loaded JobStatus
	if unmarshalErr := json.Unmarshal(data, &loaded); unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}
	if loaded.State != JobStateCompleted {
		t.Errorf("terminal job state changed to %q, expected %q", loaded.State, JobStateCompleted)
	}
}

func TestRecoverJobs_InterruptedJobNoConfig_MarkedFailed(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()
	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	// Pre-populate with a non-terminal job that has no config (unrecoverable).
	executing := &JobStatus{
		JobID:       "job-interrupted",
		TenantID:    "tenant-2",
		State:       JobStateExecuting,
		Description: "interrupted job",
		Iteration:   3,
	}
	data, _ := json.Marshal(executing)
	key := jobPersistenceKey("tenant-2", "job-interrupted")
	if err := kv.Put(context.Background(), key, data); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify the job was marked as failed (no config means resume fails).
	loadedData, getErr := kv.Get(context.Background(), key)
	if getErr != nil {
		t.Fatalf("get failed: %v", getErr)
	}
	var recovered JobStatus
	if unmarshalErr := json.Unmarshal(loadedData, &recovered); unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}

	if recovered.State != JobStateFailed {
		t.Errorf("recovered state = %q, want %q", recovered.State, JobStateFailed)
	}
	if !strings.Contains(recovered.Error, "failed to resume after restart") {
		t.Errorf("recovered error = %q, want contains %q", recovered.Error, "failed to resume after restart")
	}
}

func TestRecoverJobs_ScanError(t *testing.T) {
	kv := newMockKVStore()
	kv.scanErr = errors.New("scan failure")
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	err := svc.recoverJobs(context.Background())
	if err == nil {
		t.Fatal("expected error for scan failure")
	}

	var recoverErr *JobRecoveryError
	if !errors.As(err, &recoverErr) {
		t.Errorf("expected JobRecoveryError, got: %T", err)
	}
}

func TestRecoverJobs_CorruptDataSkipped(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// Store corrupt data.
	if err := kv.Put(context.Background(), "job:tenant:corrupt-job", []byte("not json")); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Should not error out, just log warning and skip.
	err := svc.recoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if len(logger.warnMessages) == 0 {
		t.Error("expected warning for corrupt record")
	}
}

func TestRecoverJobs_ExportedMethod(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{
		kvStore: kv,
		logger:  logger,
	}

	// RecoverJobs delegates to recoverJobs.
	err := svc.RecoverJobs(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// =============================================================================
// GetJobStatus KV Fallback Tests
// =============================================================================

func TestGetJobStatus_KVFallback(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	// Persist a completed job to KV (simulating a job that ran and was cleaned up).
	status := &JobStatus{
		JobID:       "job-finished",
		TenantID:    "tenant-fallback",
		State:       JobStateCompleted,
		Description: "finished job",
		StartedAt:   time.Now().UnixNano(),
		UpdatedAt:   time.Now().UnixNano(),
	}
	data, _ := json.Marshal(status)
	key := jobPersistenceKey("tenant-fallback", "job-finished")
	if err := kv.Put(context.Background(), key, data); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// GetJobStatus should fall back to KV since job is not in memory.
	loaded, err := svc.GetJobStatus(context.Background(), "tenant-fallback", "job-finished")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if loaded.JobID != "job-finished" {
		t.Errorf("job ID = %q, want %q", loaded.JobID, "job-finished")
	}
	if loaded.State != JobStateCompleted {
		t.Errorf("state = %q, want %q", loaded.State, JobStateCompleted)
	}
}

func TestGetJobStatus_KVFallbackError(t *testing.T) {
	kv := newMockKVStore()
	kv.getErr = errors.New("kv unavailable")
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	_, err := svc.GetJobStatus(context.Background(), "tenant-xyz", "job-missing")
	if err == nil {
		t.Fatal("expected error for missing job")
	}

	var notFound *JobNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected JobNotFoundError, got: %T", err)
	}
}

func TestGetJobStatus_NoKVStore_NotInMemory(t *testing.T) {
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	svc := &agentServiceImpl{
		store:   store,
		kvStore: nil,
		logger:  logger,
	}

	_, err := svc.GetJobStatus(context.Background(), "tenant-xyz", "job-missing")
	if err == nil {
		t.Fatal("expected error for missing job without KV store")
	}

	var notFound *JobNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected JobNotFoundError, got: %T", err)
	}
}

// =============================================================================
// CancelJob Persistence Tests
// =============================================================================

func TestCancelJob_PersistsFinalStatus(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	store := NewInMemoryAgentStore()

	svc := &agentServiceImpl{
		store:   store,
		kvStore: kv,
		logger:  logger,
	}

	// Create a mock running job.
	mockLoop := NewControlLoop(svc, logger)
	mockLoop.config = &JobConfig{
		TenantID:    "tenant-cancel",
		Description: "cancellable job",
	}
	mockLoop.jobID = "job-cancel"
	mockLoop.updateStatus(JobStateExecuting)

	ctx, cancel := context.WithCancel(context.Background())
	jobKey := "tenant-cancel/job-cancel"
	svc.jobs.Store(jobKey, &runningJob{
		loop:   mockLoop,
		cancel: cancel,
	})

	err := svc.CancelJob(context.Background(), "tenant-cancel", "job-cancel")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the cancelled status was persisted.
	key := jobPersistenceKey("tenant-cancel", "job-cancel")
	data, getErr := kv.Get(context.Background(), key)
	if getErr != nil {
		t.Fatalf("expected persisted status, got error: %v", getErr)
	}

	var persisted JobStatus
	if unmarshalErr := json.Unmarshal(data, &persisted); unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}

	if persisted.State != JobStateFailed {
		t.Errorf("state = %q, want %q", persisted.State, JobStateFailed)
	}
	if persisted.Error != "cancelled by user" {
		t.Errorf("error = %q, want %q", persisted.Error, "cancelled by user")
	}
	if persisted.Description != "cancellable job" {
		t.Errorf("description = %q, want %q", persisted.Description, "cancellable job")
	}

	// Ensure the context was actually cancelled.
	select {
	case <-ctx.Done():
		// Expected.
	default:
		t.Error("expected context to be cancelled")
	}
}

// =============================================================================
// Control Loop PersistFn / AuditFn Tests
// =============================================================================

func TestControlLoop_PersistFn_CalledOnUpdateStatus(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.config = newTestConfig()
	cl.jobID = "job-persist-test"

	var persistedStatuses []*JobStatus
	cl.persistFn = func(status *JobStatus) {
		copied := *status
		persistedStatuses = append(persistedStatuses, &copied)
	}

	cl.updateStatus(JobStateInitializing)
	cl.updateStatus(JobStateDecomposing)
	cl.updateStatus(JobStateCompleted)

	if len(persistedStatuses) != 3 {
		t.Fatalf("expected 3 persist calls, got %d", len(persistedStatuses))
	}

	expectedStates := []JobState{JobStateInitializing, JobStateDecomposing, JobStateCompleted}
	for i, expected := range expectedStates {
		if persistedStatuses[i].State != expected {
			t.Errorf("persist[%d] state = %q, want %q", i, persistedStatuses[i].State, expected)
		}
	}
}

func TestControlLoop_PersistFn_NilDoesNotPanic(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.config = newTestConfig()
	cl.jobID = "job-no-persist"

	// Should not panic when persistFn is nil.
	cl.updateStatus(JobStateInitializing)
}

func TestControlLoop_AuditFn_CalledFromStateHandlers(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.config = newTestConfig()
	cl.jobID = "job-audit-test"

	var auditEvents []string
	cl.auditFn = func(eventType, details string) {
		auditEvents = append(auditEvents, eventType)
	}

	// Directly call audit to verify it works.
	cl.audit("orchestration:test_event", "test details")

	if len(auditEvents) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditEvents))
	}
	if auditEvents[0] != "orchestration:test_event" {
		t.Errorf("audit event = %q, want %q", auditEvents[0], "orchestration:test_event")
	}
}

func TestControlLoop_AuditFn_NilDoesNotPanic(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{}
	cl := NewControlLoop(mockSvc, logger)
	cl.config = newTestConfig()
	cl.jobID = "job-no-audit"

	// Should not panic when auditFn is nil.
	cl.audit("orchestration:test", "test")
}

func TestControlLoop_AuditFn_InitializeEmitsJobStarted(t *testing.T) {
	logger := &controlLoopMockLogger{}
	mockSvc := &controlLoopMockService{
		backends: newTestBackends(),
		agents:   newTestAgents(),
	}
	cl := NewControlLoop(mockSvc, logger)
	cl.config = newTestConfig()
	cl.jobID = "job-audit-init"

	var auditEvents []string
	cl.auditFn = func(eventType, details string) {
		auditEvents = append(auditEvents, eventType)
	}

	ctx := context.Background()
	nextState, err := cl.handleInitialize(ctx)
	if err != nil {
		t.Fatalf("handleInitialize error: %v", err)
	}
	if nextState != JobStateDecomposing {
		t.Errorf("next state = %q, want %q", nextState, JobStateDecomposing)
	}

	found := false
	for _, event := range auditEvents {
		if event == "orchestration:job_started" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected orchestration:job_started audit event, got: %v", auditEvents)
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestJobPersistenceError(t *testing.T) {
	cause := errors.New("disk full")
	err := &JobPersistenceError{
		JobID:   "job-err",
		Message: "write failed",
		Cause:   cause,
	}

	if !strings.Contains(err.Error(), "job=job-err") {
		t.Errorf("error message missing job ID: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Errorf("error message missing message: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error message missing cause: %s", err.Error())
	}
	if !errors.Is(err, ErrJobPersistenceFailed) {
		t.Error("expected errors.Is to match ErrJobPersistenceFailed")
	}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to match cause")
	}
}

func TestJobPersistenceError_NoCause(t *testing.T) {
	err := &JobPersistenceError{
		JobID:   "job-err",
		Message: "no cause",
	}

	if !errors.Is(err, ErrJobPersistenceFailed) {
		t.Error("expected errors.Is to match ErrJobPersistenceFailed")
	}

	unwrapped := err.Unwrap()
	if len(unwrapped) != 1 {
		t.Errorf("expected 1 unwrapped error, got %d", len(unwrapped))
	}
}

func TestJobRecoveryError(t *testing.T) {
	cause := errors.New("connection refused")
	err := &JobRecoveryError{
		Message: "scan failed",
		Cause:   cause,
	}

	if !strings.Contains(err.Error(), "scan failed") {
		t.Errorf("error message missing message: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error message missing cause: %s", err.Error())
	}
	if !errors.Is(err, ErrJobRecoveryFailed) {
		t.Error("expected errors.Is to match ErrJobRecoveryFailed")
	}
}

func TestJobRecoveryError_NoCause(t *testing.T) {
	err := &JobRecoveryError{
		Message: "no cause",
	}

	if !errors.Is(err, ErrJobRecoveryFailed) {
		t.Error("expected errors.Is to match ErrJobRecoveryFailed")
	}

	unwrapped := err.Unwrap()
	if len(unwrapped) != 1 {
		t.Errorf("expected 1 unwrapped error, got %d", len(unwrapped))
	}
}

// =============================================================================
// IsTerminalJobState Tests
// =============================================================================

func TestIsTerminalJobState(t *testing.T) {
	tests := []struct {
		state    JobState
		terminal bool
	}{
		{JobStateCompleted, true},
		{JobStateFailed, true},
		{JobStateInitializing, false},
		{JobStateDecomposing, false},
		{JobStateScheduling, false},
		{JobStateAssigning, false},
		{JobStateExecuting, false},
		{JobStateValidating, false},
		{JobStateReviewing, false},
		{JobStateRedelegating, false},
		{JobStateCompleting, false},
		{JobStateCleanup, false},
		{JobStateEscalating, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			result := IsTerminalJobState(tc.state)
			if result != tc.terminal {
				t.Errorf("IsTerminalJobState(%q) = %v, want %v", tc.state, result, tc.terminal)
			}
		})
	}
}

// =============================================================================
// jobPersistenceKey Tests
// =============================================================================

func TestJobPersistenceKey(t *testing.T) {
	key := jobPersistenceKey("my-tenant", "my-job")
	expected := "job:my-tenant:my-job"
	if key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}

func TestJobPersistenceKey_EmptyValues(t *testing.T) {
	key := jobPersistenceKey("", "")
	expected := "job::"
	if key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}
