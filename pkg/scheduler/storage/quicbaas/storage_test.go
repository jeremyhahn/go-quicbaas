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

package quicbaas

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// mockKVStore is a simple in-memory KVStore implementation for testing.
type mockKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte

	// Error injection for testing error paths
	putErr    error
	getErr    error
	deleteErr error
	scanErr   error
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.data[key]; ok {
		return append([]byte(nil), v...), nil
	}
	// The KVStore contract: an absent key is a QBError{ErrNotFound}, the
	// same error pkg/kvstore and the qrdb-backed stores return.
	return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "mock.kv.get"}
}

func (m *mockKVStore) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				if err == ErrLimitReached {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

func (m *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// Test helper to create a test job.
func createTestJob(name string, ownerType scheduler.OwnerType, ownerID string) *scheduler.Job {
	return &scheduler.Job{
		Name:        name,
		Description: "Test job description",
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		Type:        scheduler.JobTypeOneShot,
		Handler:     "test-handler",
		Priority:    1,
		Timeout:     time.Minute,
		MaxRetries:  3,
		Status:      scheduler.JobStatusPending,
		ExecuteAt:   time.Now().Add(time.Hour),
		NextRun:     time.Now().Add(time.Hour),
		Labels: map[string]string{
			"env":  "test",
			"tier": "backend",
		},
	}
}

func TestNewStorage(t *testing.T) {
	kv := newMockKVStore()
	storage := NewStorage(kv)

	if storage == nil {
		t.Fatal("expected non-nil storage")
	}
	if storage.kv != kv {
		t.Error("expected KVStore to be set")
	}
}

func TestCreateJob_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("test-job", scheduler.OwnerTypeSystem, "")

	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify job was assigned an ID
	if job.ID == 0 {
		t.Error("expected job ID to be assigned")
	}

	// Verify timestamps were set
	if job.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if job.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	// Verify job was stored
	retrieved, err := storage.GetJob(ctx, job.ID, "")
	if err != nil {
		t.Fatalf("failed to retrieve job: %v", err)
	}
	if retrieved.Name != job.Name {
		t.Errorf("expected name %s, got %s", job.Name, retrieved.Name)
	}
}

func TestStorage_NilInput(t *testing.T) {
	tests := []struct {
		name string
		call func(ctx context.Context, s *Storage) error
	}{
		{"CreateJob", func(ctx context.Context, s *Storage) error { return s.CreateJob(ctx, nil) }},
		{"UpdateJob", func(ctx context.Context, s *Storage) error { return s.UpdateJob(ctx, nil) }},
		{"SaveExecution", func(ctx context.Context, s *Storage) error { return s.SaveExecution(ctx, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(context.Background(), NewStorage(newMockKVStore()))
			if err == nil {
				t.Fatal("expected error for nil input")
			}

			var storageErr *scheduler.ErrStorageError
			if !errors.As(err, &storageErr) {
				t.Fatalf("expected ErrStorageError, got %T", err)
			}
		})
	}
}

func TestCreateJob_WithExistingID(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("test-job", scheduler.OwnerTypeSystem, "")
	job.ID = 999

	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ID was preserved
	if job.ID != 999 {
		t.Errorf("expected job ID 999, got %d", job.ID)
	}
}

func TestCreateJob_TenantJob(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("tenant-job", scheduler.OwnerTypeTenant, "tenant-123")

	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify job was stored in tenant keyspace
	retrieved, err := storage.GetJob(ctx, job.ID, "tenant-123")
	if err != nil {
		t.Fatalf("failed to retrieve tenant job: %v", err)
	}
	if retrieved.OwnerID != "tenant-123" {
		t.Errorf("expected owner ID tenant-123, got %s", retrieved.OwnerID)
	}
}

func TestCreateJob_DelegatedJob(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("delegated-job", scheduler.OwnerTypeDelegated, "")
	job.TargetTenantID = "target-tenant"

	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Delegated jobs should be stored in system keyspace
	retrieved, err := storage.GetJob(ctx, job.ID, "")
	if err != nil {
		t.Fatalf("failed to retrieve delegated job: %v", err)
	}
	if retrieved.TargetTenantID != "target-tenant" {
		t.Errorf("expected target tenant ID target-tenant, got %s", retrieved.TargetTenantID)
	}
}

func TestStorage_StoreFaults(t *testing.T) {
	storeErr := errors.New("storage unavailable")
	execution := func() *scheduler.JobExecution {
		return &scheduler.JobExecution{JobID: 1, Status: scheduler.JobStatusCompleted, StartedAt: time.Now()}
	}

	tests := []struct {
		name   string
		inject func(kv *mockKVStore)
		call   func(ctx context.Context, s *Storage) error
	}{
		{
			name:   "CreateJob put",
			inject: func(kv *mockKVStore) { kv.putErr = storeErr },
			call: func(ctx context.Context, s *Storage) error {
				return s.CreateJob(ctx, createTestJob("test-job", scheduler.OwnerTypeSystem, ""))
			},
		},
		{
			name:   "UpdateJob put",
			inject: func(kv *mockKVStore) { kv.putErr = storeErr },
			call: func(ctx context.Context, s *Storage) error {
				return s.UpdateJob(ctx, createTestJob("test-job", scheduler.OwnerTypeSystem, ""))
			},
		},
		{
			name:   "DeleteJob delete",
			inject: func(kv *mockKVStore) { kv.deleteErr = storeErr },
			call:   func(ctx context.Context, s *Storage) error { return s.DeleteJob(ctx, 1, "") },
		},
		{
			name:   "ListJobs scan",
			inject: func(kv *mockKVStore) { kv.scanErr = storeErr },
			call: func(ctx context.Context, s *Storage) error {
				_, _, err := s.ListJobs(ctx, nil)
				return err
			},
		},
		{
			name:   "GetJobsDueForExecution scan",
			inject: func(kv *mockKVStore) { kv.scanErr = storeErr },
			call: func(ctx context.Context, s *Storage) error {
				_, err := s.GetJobsDueForExecution(ctx, time.Now(), 10)
				return err
			},
		},
		{
			name:   "SaveExecution put",
			inject: func(kv *mockKVStore) { kv.putErr = storeErr },
			call:   func(ctx context.Context, s *Storage) error { return s.SaveExecution(ctx, execution()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := newMockKVStore()
			tt.inject(kv)

			err := tt.call(context.Background(), NewStorage(kv))
			if !errors.Is(err, storeErr) {
				t.Fatalf("expected the store error, got %v", err)
			}

			var storageErr *scheduler.ErrStorageError
			if !errors.As(err, &storageErr) {
				t.Fatalf("expected ErrStorageError, got %T", err)
			}
		})
	}
}

func TestGetJob_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	_, err := storage.GetJob(ctx, 999, "")
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}

	var notFoundError *scheduler.JobNotFoundError
	if !errors.As(err, &notFoundError) {
		t.Fatalf("expected JobNotFoundError, got %T", err)
	}
	if notFoundError.JobID != 999 {
		t.Errorf("expected job ID 999, got %d", notFoundError.JobID)
	}
}

// TestGetJob_StoreErrorIsNotNotFound pins that a KV failure other than an
// absent key surfaces as a storage error. Reported as not-found, system job
// install would recreate — and so overwrite — a record that exists.
func TestGetJob_StoreErrorIsNotNotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	job := createTestJob("live", scheduler.OwnerTypeSystem, "")
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	cause := errors.New("no leader")
	kv.getErr = cause

	got, err := storage.GetJob(ctx, job.ID, "")
	if got != nil {
		t.Fatalf("expected no job on a store error, got %+v", got)
	}
	var notFound *scheduler.JobNotFoundError
	if errors.As(err, &notFound) {
		t.Fatalf("a store error must not read as not-found: %v", err)
	}
	var storageErr *scheduler.ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected ErrStorageError, got %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the storage error must carry its cause, got %v", err)
	}
}

func TestGetJob_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Create a tenant job
	job := createTestJob("tenant-job", scheduler.OwnerTypeTenant, "tenant-A")
	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Try to retrieve with wrong tenant ID
	_, err = storage.GetJob(ctx, job.ID, "tenant-B")
	if err == nil {
		t.Fatal("expected error when accessing job with wrong tenant ID")
	}

	// Verify we can get it with correct tenant ID
	retrieved, err := storage.GetJob(ctx, job.ID, "tenant-A")
	if err != nil {
		t.Fatalf("failed to retrieve job with correct tenant ID: %v", err)
	}
	if retrieved.ID != job.ID {
		t.Errorf("expected job ID %d, got %d", job.ID, retrieved.ID)
	}
}

func TestUpdateJob_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("test-job", scheduler.OwnerTypeSystem, "")
	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	originalUpdatedAt := job.UpdatedAt

	// Update the job
	job.Name = "updated-job"
	job.Status = scheduler.JobStatusRunning

	// Small delay to ensure timestamp changes
	time.Sleep(time.Millisecond)

	err = storage.UpdateJob(ctx, job)
	if err != nil {
		t.Fatalf("failed to update job: %v", err)
	}

	// Verify UpdatedAt was changed
	if !job.UpdatedAt.After(originalUpdatedAt) {
		t.Error("expected UpdatedAt to be updated")
	}

	// Retrieve and verify
	retrieved, err := storage.GetJob(ctx, job.ID, "")
	if err != nil {
		t.Fatalf("failed to retrieve job: %v", err)
	}
	if retrieved.Name != "updated-job" {
		t.Errorf("expected name updated-job, got %s", retrieved.Name)
	}
	if retrieved.Status != scheduler.JobStatusRunning {
		t.Errorf("expected status Running, got %s", retrieved.Status)
	}
}

func TestDeleteJob_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	job := createTestJob("test-job", scheduler.OwnerTypeSystem, "")
	err := storage.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	err = storage.DeleteJob(ctx, job.ID, "")
	if err != nil {
		t.Fatalf("failed to delete job: %v", err)
	}

	// Verify job is gone
	_, err = storage.GetJob(ctx, job.ID, "")
	if err == nil {
		t.Fatal("expected error for deleted job")
	}
}

func TestListJobs_Empty(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	jobs, cursor, err := storage.ListJobs(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
	if cursor != "" {
		t.Errorf("expected empty cursor, got %s", cursor)
	}
}

func TestListJobs_Filters(t *testing.T) {
	type jobSpec struct {
		name   string
		owner  scheduler.OwnerType
		mutate func(job *scheduler.Job)
	}

	tests := []struct {
		name     string
		jobs     []jobSpec
		query    *scheduler.JobQuery
		wantName string
	}{
		{
			name: "status",
			jobs: []jobSpec{
				{"pending-job", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Status = scheduler.JobStatusPending }},
				{"running-job", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Status = scheduler.JobStatusRunning }},
				{"completed-job", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Status = scheduler.JobStatusCompleted }},
			},
			query:    &scheduler.JobQuery{Status: scheduler.JobStatusPending},
			wantName: "pending-job",
		},
		{
			name: "handler",
			jobs: []jobSpec{
				{"job-1", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Handler = "handler-a" }},
				{"job-2", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Handler = "handler-b" }},
			},
			query:    &scheduler.JobQuery{Handler: "handler-a"},
			wantName: "job-1",
		},
		{
			name: "labels",
			jobs: []jobSpec{
				{"job-1", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Labels = map[string]string{"env": "prod", "tier": "frontend"} }},
				{"job-2", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Labels = map[string]string{"env": "prod", "tier": "backend"} }},
				{"job-3", scheduler.OwnerTypeSystem, func(j *scheduler.Job) { j.Labels = map[string]string{"env": "dev"} }},
			},
			query:    &scheduler.JobQuery{Labels: map[string]string{"env": "prod", "tier": "backend"}},
			wantName: "job-2",
		},
		{
			name: "target tenant",
			jobs: []jobSpec{
				{"job-1", scheduler.OwnerTypeDelegated, func(j *scheduler.Job) { j.TargetTenantID = "tenant-A" }},
				{"job-2", scheduler.OwnerTypeDelegated, func(j *scheduler.Job) { j.TargetTenantID = "tenant-B" }},
			},
			query:    &scheduler.JobQuery{TargetTenantID: "tenant-A"},
			wantName: "job-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			storage := NewStorage(newMockKVStore())

			for _, spec := range tt.jobs {
				job := createTestJob(spec.name, spec.owner, "")
				spec.mutate(job)
				if err := storage.CreateJob(ctx, job); err != nil {
					t.Fatalf("failed to create job %s: %v", spec.name, err)
				}
			}

			jobs, _, err := storage.ListJobs(ctx, tt.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].Name != tt.wantName {
				t.Errorf("expected job %s, got %s", tt.wantName, jobs[0].Name)
			}
		})
	}
}

func TestListJobs_Pagination(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Create multiple jobs
	for i := 0; i < 10; i++ {
		job := createTestJob("job", scheduler.OwnerTypeSystem, "")
		storage.CreateJob(ctx, job)
	}

	// Request with limit
	query := &scheduler.JobQuery{Limit: 5}
	jobs, cursor, err := storage.ListJobs(ctx, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 5 {
		t.Errorf("expected 5 jobs, got %d", len(jobs))
	}
	if cursor == "" {
		t.Error("expected non-empty cursor for pagination")
	}
}

func TestListJobs_TenantJobs(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Create system job
	sysJob := createTestJob("sys-job", scheduler.OwnerTypeSystem, "")
	storage.CreateJob(ctx, sysJob)

	// Create tenant jobs
	tenantJob1 := createTestJob("tenant-job-1", scheduler.OwnerTypeTenant, "tenant-A")
	storage.CreateJob(ctx, tenantJob1)

	tenantJob2 := createTestJob("tenant-job-2", scheduler.OwnerTypeTenant, "tenant-A")
	storage.CreateJob(ctx, tenantJob2)

	// List only tenant-A jobs
	query := &scheduler.JobQuery{OwnerID: "tenant-A"}
	jobs, _, err := storage.ListJobs(ctx, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 tenant jobs, got %d", len(jobs))
	}
}

func TestGetJobsDueForExecution_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	now := time.Now()

	// Create a job due for execution (NextRun in the past)
	dueJob := createTestJob("due-job", scheduler.OwnerTypeSystem, "")
	dueJob.Status = scheduler.JobStatusPending
	dueJob.NextRun = now.Add(-time.Hour)
	storage.CreateJob(ctx, dueJob)

	// Create a job not yet due
	futureJob := createTestJob("future-job", scheduler.OwnerTypeSystem, "")
	futureJob.Status = scheduler.JobStatusPending
	futureJob.NextRun = now.Add(time.Hour)
	storage.CreateJob(ctx, futureJob)

	// Create a completed job with past NextRun
	completedJob := createTestJob("completed-job", scheduler.OwnerTypeSystem, "")
	completedJob.Status = scheduler.JobStatusCompleted
	completedJob.NextRun = now.Add(-time.Hour)
	storage.CreateJob(ctx, completedJob)

	dueJobs, err := storage.GetJobsDueForExecution(ctx, now, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dueJobs) != 1 {
		t.Errorf("expected 1 due job, got %d", len(dueJobs))
	}
	if len(dueJobs) > 0 && dueJobs[0].Name != "due-job" {
		t.Errorf("expected due-job, got %s", dueJobs[0].Name)
	}
}

func TestGetJobsDueForExecution_Limit(t *testing.T) {
	tests := []struct {
		name    string
		dueJobs int
		limit   int
		want    int
	}{
		{name: "explicit limit", dueJobs: 10, limit: 5, want: 5},
		{name: "zero limit defaults to 100", dueJobs: 101, limit: 0, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			storage := NewStorage(newMockKVStore())
			now := time.Now()

			for i := 0; i < tt.dueJobs; i++ {
				job := createTestJob("due-job", scheduler.OwnerTypeSystem, "")
				job.Status = scheduler.JobStatusPending
				job.NextRun = now.Add(-time.Hour)
				if err := storage.CreateJob(ctx, job); err != nil {
					t.Fatalf("failed to create job: %v", err)
				}
			}

			dueJobs, err := storage.GetJobsDueForExecution(ctx, now, tt.limit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(dueJobs) != tt.want {
				t.Errorf("expected %d due jobs, got %d", tt.want, len(dueJobs))
			}
		})
	}
}

func TestSaveExecution_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	exec := &scheduler.JobExecution{
		JobID:     1,
		Status:    scheduler.JobStatusCompleted,
		StartedAt: time.Now().Add(-time.Minute),
		EndedAt:   time.Now(),
		Duration:  time.Minute,
		Output:    "success",
	}

	err := storage.SaveExecution(ctx, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ID was assigned
	if exec.ID == 0 {
		t.Error("expected execution ID to be assigned")
	}
}

func TestSaveExecution_WithExistingID(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	exec := &scheduler.JobExecution{
		ID:        12345,
		JobID:     1,
		Status:    scheduler.JobStatusCompleted,
		StartedAt: time.Now(),
	}

	err := storage.SaveExecution(ctx, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ID was preserved
	if exec.ID != 12345 {
		t.Errorf("expected execution ID 12345, got %d", exec.ID)
	}
}

func TestGetExecutions_Success(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Save multiple executions for the same job
	for i := 0; i < 5; i++ {
		exec := &scheduler.JobExecution{
			JobID:     1,
			Status:    scheduler.JobStatusCompleted,
			StartedAt: time.Now(),
		}
		storage.SaveExecution(ctx, exec)
	}

	executions, err := storage.GetExecutions(ctx, 1, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executions) != 5 {
		t.Errorf("expected 5 executions, got %d", len(executions))
	}
}

func TestGetExecutions_Limit(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Save multiple executions
	for i := 0; i < 10; i++ {
		exec := &scheduler.JobExecution{
			JobID:     1,
			Status:    scheduler.JobStatusCompleted,
			StartedAt: time.Now(),
		}
		storage.SaveExecution(ctx, exec)
	}

	executions, err := storage.GetExecutions(ctx, 1, "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executions) != 5 {
		t.Errorf("expected 5 executions with limit, got %d", len(executions))
	}
}

func TestGetExecutions_ForTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Note: Current implementation stores in system keyspace regardless of tenantID parameter.
	// This test verifies the tenant prefix is correctly generated for retrieval.
	executions, err := storage.GetExecutions(ctx, 1, "tenant-A", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty since no executions exist in tenant keyspace
	if len(executions) != 0 {
		t.Errorf("expected 0 executions for tenant, got %d", len(executions))
	}
}

func TestClose(t *testing.T) {
	kv := newMockKVStore()
	storage := NewStorage(kv)

	err := storage.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKeyForJob_AllOwnerTypes(t *testing.T) {
	tests := []struct {
		name      string
		ownerType scheduler.OwnerType
		ownerID   string
		expected  string
	}{
		{
			name:      "system job",
			ownerType: scheduler.OwnerTypeSystem,
			ownerID:   "",
			expected:  "__sys:scheduler/jobs/1",
		},
		{
			name:      "delegated job",
			ownerType: scheduler.OwnerTypeDelegated,
			ownerID:   "",
			expected:  "__sys:scheduler/jobs/1",
		},
		{
			name:      "tenant job",
			ownerType: scheduler.OwnerTypeTenant,
			ownerID:   "tenant-123",
			expected:  "t:tenant-123:scheduler/jobs/1",
		},
		{
			name:      "unknown owner type",
			ownerType: "unknown",
			ownerID:   "",
			expected:  "__sys:scheduler/jobs/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &scheduler.Job{
				ID:        1,
				OwnerType: tt.ownerType,
				OwnerID:   tt.ownerID,
			}

			key := keyForJob(job)
			if key != tt.expected {
				t.Errorf("expected key %s, got %s", tt.expected, key)
			}
		})
	}
}

func TestKeyForJobByID(t *testing.T) {
	tests := []struct {
		name     string
		id       uint64
		tenantID string
		expected string
	}{
		{
			name:     "system job",
			id:       42,
			tenantID: "",
			expected: "__sys:scheduler/jobs/42",
		},
		{
			name:     "tenant job",
			id:       42,
			tenantID: "tenant-abc",
			expected: "t:tenant-abc:scheduler/jobs/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := keyForJobByID(tt.id, tt.tenantID)
			if key != tt.expected {
				t.Errorf("expected key %s, got %s", tt.expected, key)
			}
		})
	}
}

func TestKeyForExecution(t *testing.T) {
	tests := []struct {
		name     string
		jobID    uint64
		execID   uint64
		tenantID string
		expected string
	}{
		{
			name:     "system execution",
			jobID:    1,
			execID:   100,
			tenantID: "",
			expected: "__sys:scheduler/executions/1/100",
		},
		{
			name:     "tenant execution",
			jobID:    1,
			execID:   100,
			tenantID: "tenant-xyz",
			expected: "t:tenant-xyz:scheduler/executions/1/100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &scheduler.JobExecution{
				ID:    tt.execID,
				JobID: tt.jobID,
			}
			key := keyForExecution(exec, tt.tenantID)
			if key != tt.expected {
				t.Errorf("expected key %s, got %s", tt.expected, key)
			}
		})
	}
}

func TestConcurrentJobCreation(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	const numJobs = 100
	var wg sync.WaitGroup
	wg.Add(numJobs)

	ids := make(chan uint64, numJobs)

	for i := 0; i < numJobs; i++ {
		go func(idx int) {
			defer wg.Done()
			job := createTestJob("concurrent-job", scheduler.OwnerTypeSystem, "")
			if err := storage.CreateJob(ctx, job); err != nil {
				t.Errorf("failed to create job %d: %v", idx, err)
				return
			}
			ids <- job.ID
		}(i)
	}

	wg.Wait()
	close(ids)

	// Verify all IDs are unique
	seen := make(map[uint64]bool)
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate job ID: %d", id)
		}
		seen[id] = true
	}

	if len(seen) != numJobs {
		t.Errorf("expected %d unique IDs, got %d", numJobs, len(seen))
	}
}

func TestListJobs_MalformedData(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	// Store malformed JSON
	kv.Put(ctx, "__sys:scheduler/jobs/999", []byte("not valid json"))

	// Create a valid job
	job := createTestJob("valid-job", scheduler.OwnerTypeSystem, "")
	storage.CreateJob(ctx, job)

	// ListJobs should skip malformed entries and return valid ones
	jobs, _, err := storage.ListJobs(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("expected 1 valid job, got %d", len(jobs))
	}
}

func TestExecutionIDAt_UsesUnixNanoseconds(t *testing.T) {
	at := time.Unix(1_700_000_000, 42)
	id, err := executionIDAt(at)
	if err != nil {
		t.Fatalf("executionIDAt: %v", err)
	}
	if want := uint64(1_700_000_000_000_000_042); id != want {
		t.Fatalf("executionIDAt = %d, want %d", id, want)
	}
}

func TestExecutionIDAt_RefusesTimeBeforeEpoch(t *testing.T) {
	var negErr *conv.ConversionNegativeError
	if _, err := executionIDAt(time.Unix(-1, 0)); !errors.As(err, &negErr) {
		t.Fatalf("executionIDAt(pre-epoch) error = %v, want *conv.ConversionNegativeError", err)
	}
}
