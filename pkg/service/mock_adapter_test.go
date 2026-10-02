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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Test Mock Definitions (local to avoid circular imports)
// =============================================================================

// testMockSchedulerEngine is a mock implementation of interfaces.SchedulerEngine for testing.
type testMockSchedulerEngine struct {
	mock.Mock
}

var _ interfaces.SchedulerEngine = (*testMockSchedulerEngine)(nil)

func newTestMockSchedulerEngine() *testMockSchedulerEngine {
	return &testMockSchedulerEngine{}
}

func (m *testMockSchedulerEngine) RegisterHandler(name string, handler scheduler.JobHandler) error {
	args := m.Called(name, handler)
	return args.Error(0)
}

func (m *testMockSchedulerEngine) ScheduleJob(ctx context.Context, job *scheduler.Job) error {
	args := m.Called(ctx, job)
	return args.Error(0)
}

func (m *testMockSchedulerEngine) GetJobWithTenant(ctx context.Context, jobID uint64, tenantID string) (*scheduler.Job, error) {
	args := m.Called(ctx, jobID, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*scheduler.Job), args.Error(1)
}

func (m *testMockSchedulerEngine) UpdateJob(ctx context.Context, jobID uint64, tenantID string, updates *scheduler.JobUpdateParams) (*scheduler.Job, error) {
	args := m.Called(ctx, jobID, tenantID, updates)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*scheduler.Job), args.Error(1)
}

func (m *testMockSchedulerEngine) DeleteJob(ctx context.Context, jobID uint64, tenantID string, force bool) error {
	args := m.Called(ctx, jobID, tenantID, force)
	return args.Error(0)
}

func (m *testMockSchedulerEngine) ListJobsWithCursor(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	args := m.Called(ctx, query)
	var jobs []*scheduler.Job
	if args.Get(0) != nil {
		jobs = args.Get(0).([]*scheduler.Job)
	}
	return jobs, args.String(1), args.Error(2)
}

func (m *testMockSchedulerEngine) TriggerJob(ctx context.Context, jobID uint64, tenantID string) error {
	args := m.Called(ctx, jobID, tenantID)
	return args.Error(0)
}

func (m *testMockSchedulerEngine) PauseJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (scheduler.JobStatus, error) {
	args := m.Called(ctx, jobID, tenantID)
	return args.Get(0).(scheduler.JobStatus), args.Error(1)
}

func (m *testMockSchedulerEngine) ResumeJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (scheduler.JobStatus, error) {
	args := m.Called(ctx, jobID, tenantID)
	return args.Get(0).(scheduler.JobStatus), args.Error(1)
}

func (m *testMockSchedulerEngine) CancelJobWithTenant(ctx context.Context, jobID uint64, tenantID string, reason string) (scheduler.JobStatus, error) {
	args := m.Called(ctx, jobID, tenantID, reason)
	return args.Get(0).(scheduler.JobStatus), args.Error(1)
}

func (m *testMockSchedulerEngine) GetJobHistory(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*scheduler.JobExecution, error) {
	args := m.Called(ctx, jobID, tenantID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*scheduler.JobExecution), args.Error(1)
}

func (m *testMockSchedulerEngine) Stats() *scheduler.SchedulerStats {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*scheduler.SchedulerStats)
}

func (m *testMockSchedulerEngine) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *testMockSchedulerEngine) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *testMockSchedulerEngine) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// =============================================================================
// SchedulerAdapter Tests with MockSchedulerEngine
// =============================================================================

func TestSchedulerAdapter_ScheduleJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithSystem(context.Background())
	job := &SchedulerJob{
		Name:    "test-job",
		Handler: "test-handler",
		Payload: []byte(`{"key":"value"}`),
	}

	// Setup mock expectation - the scheduler will set the job ID
	mockEngine.On("ScheduleJob", ctx, mock.AnythingOfType("*scheduler.Job")).
		Run(func(args mock.Arguments) {
			schedulerJob := args.Get(1).(*scheduler.Job)
			schedulerJob.ID = 123
		}).
		Return(nil).Once()

	err := adapter.ScheduleJob(ctx, job)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if job.ID != 123 {
		t.Errorf("expected job ID to be set to 123, got %d", job.ID)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ScheduleJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithSystem(context.Background())
	job := &SchedulerJob{
		Name:    "test-job",
		Handler: "test-handler",
	}

	expectedErr := errors.New("schedule failed")
	mockEngine.On("ScheduleJob", ctx, mock.AnythingOfType("*scheduler.Job")).
		Return(expectedErr).Once()

	err := adapter.ScheduleJob(ctx, job)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_CreateJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &CreateSchedulerJobRequest{
		Name:           "new-job",
		Description:    "A new scheduled job",
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "tenant-1",
		TargetTenantID: "tenant-1",
		Type:           SchedulerJobTypeRecurring,
		Schedule:       "0 * * * *",
		Handler:        "hourly-handler",
		Payload:        []byte(`{"task":"cleanup"}`),
		Priority:       5,
		Timeout:        time.Hour,
		MaxRetries:     3,
		Labels:         map[string]string{"env": "prod"},
	}

	mockEngine.On("ScheduleJob", ctx, mock.AnythingOfType("*scheduler.Job")).
		Run(func(args mock.Arguments) {
			schedulerJob := args.Get(1).(*scheduler.Job)
			schedulerJob.ID = 456
		}).
		Return(nil).Once()

	resp, err := adapter.CreateJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Job == nil {
		t.Fatal("expected non-nil job in response")
	}
	if resp.Job.ID != 456 {
		t.Errorf("expected job ID 456, got %d", resp.Job.ID)
	}
	if resp.Job.Name != "new-job" {
		t.Errorf("expected job name 'new-job', got '%s'", resp.Job.Name)
	}
	if resp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_CreateJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithSystem(context.Background())
	req := &CreateSchedulerJobRequest{
		Name:    "new-job",
		Handler: "test-handler",
	}

	expectedErr := errors.New("creation failed")
	mockEngine.On("ScheduleJob", ctx, mock.AnythingOfType("*scheduler.Job")).
		Return(expectedErr).Once()

	resp, err := adapter.CreateJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobRequest{
		JobID:    789,
		TenantID: "tenant-1",
	}

	now := time.Now()
	expectedJob := &scheduler.Job{
		ID:             789,
		Name:           "existing-job",
		Description:    "An existing job",
		Handler:        "existing-handler",
		TargetTenantID: "tenant-1",
		Status:         scheduler.JobStatusPending,
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now,
	}

	mockEngine.On("GetJobWithTenant", ctx, uint64(789), "tenant-1").
		Return(expectedJob, nil).Once()

	resp, err := adapter.GetJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Job == nil {
		t.Fatal("expected non-nil job in response")
	}
	if resp.Job.ID != 789 {
		t.Errorf("expected job ID 789, got %d", resp.Job.ID)
	}
	if resp.Job.Name != "existing-job" {
		t.Errorf("expected job name 'existing-job', got '%s'", resp.Job.Name)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJob_NotFound(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobRequest{
		JobID:    999,
		TenantID: "tenant-1",
	}

	expectedErr := errors.New("job not found")
	mockEngine.On("GetJobWithTenant", ctx, uint64(999), "tenant-1").
		Return(nil, expectedErr).Once()

	resp, err := adapter.GetJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_UpdateJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	executeAt := time.Now().Add(2 * time.Hour)
	req := &UpdateSchedulerJobRequest{
		JobID:       100,
		TenantID:    "tenant-1",
		Name:        "updated-job",
		Description: "Updated description",
		Schedule:    "0 0 * * *",
		ExecuteAt:   executeAt,
		Handler:     "updated-handler",
		Payload:     []byte(`{"updated":true}`),
		Priority:    10,
		Timeout:     2 * time.Hour,
		MaxRetries:  5,
		Labels:      map[string]string{"updated": "true"},
	}

	updatedJob := &scheduler.Job{
		ID:             100,
		Name:           "updated-job",
		Description:    "Updated description",
		Handler:        "updated-handler",
		TargetTenantID: "tenant-1",
		Priority:       10,
		Status:         scheduler.JobStatusPending,
	}

	mockEngine.On("UpdateJob", ctx, uint64(100), "tenant-1", mock.AnythingOfType("*scheduler.JobUpdateParams")).
		Return(updatedJob, nil).Once()

	resp, err := adapter.UpdateJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Job.Name != "updated-job" {
		t.Errorf("expected job name 'updated-job', got '%s'", resp.Job.Name)
	}
	if resp.Job.Priority != 10 {
		t.Errorf("expected priority 10, got %d", resp.Job.Priority)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_UpdateJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &UpdateSchedulerJobRequest{
		JobID:    100,
		TenantID: "tenant-1",
		Name:     "updated-job",
	}

	expectedErr := errors.New("update failed")
	mockEngine.On("UpdateJob", ctx, uint64(100), "tenant-1", mock.AnythingOfType("*scheduler.JobUpdateParams")).
		Return(nil, expectedErr).Once()

	resp, err := adapter.UpdateJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_DeleteJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &DeleteSchedulerJobRequest{
		JobID:    200,
		TenantID: "tenant-1",
		Force:    true,
	}

	mockEngine.On("DeleteJob", ctx, uint64(200), "tenant-1", true).
		Return(nil).Once()

	resp, err := adapter.DeleteJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JobID != 200 {
		t.Errorf("expected job ID 200, got %d", resp.JobID)
	}
	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}
	if resp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_DeleteJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &DeleteSchedulerJobRequest{
		JobID:    200,
		TenantID: "tenant-1",
		Force:    false,
	}

	expectedErr := errors.New("delete failed")
	mockEngine.On("DeleteJob", ctx, uint64(200), "tenant-1", false).
		Return(expectedErr).Once()

	resp, err := adapter.DeleteJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ListJobs_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &ListSchedulerJobsRequest{
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "tenant-1",
		TargetTenantID: "tenant-1",
		Status:         SchedulerJobStatusPending,
		Handler:        "test-handler",
		Labels:         map[string]string{"env": "prod"},
		Limit:          50,
		Cursor:         "prev-cursor",
	}

	jobs := []*scheduler.Job{
		{ID: 1, Name: "job-1", TargetTenantID: "tenant-1", Status: scheduler.JobStatusPending},
		{ID: 2, Name: "job-2", TargetTenantID: "tenant-1", Status: scheduler.JobStatusPending},
		{ID: 3, Name: "job-3", TargetTenantID: "tenant-1", Status: scheduler.JobStatusPending},
	}

	mockEngine.On("ListJobsWithCursor", mock.Anything, mock.AnythingOfType("*scheduler.JobQuery")).
		Return(jobs, "next-cursor", nil).Once()

	resp, err := adapter.ListJobs(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(resp.Jobs))
	}
	if resp.NextCursor != "next-cursor" {
		t.Errorf("expected cursor 'next-cursor', got '%s'", resp.NextCursor)
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true when cursor is non-empty")
	}
	if resp.TotalCount != 3 {
		t.Errorf("expected total count 3, got %d", resp.TotalCount)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ListJobs_EmptyResult(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "user-1")
	req := &ListSchedulerJobsRequest{
		OwnerType: SchedulerOwnerTypeTenant,
		OwnerID:   "user-1",
		Limit:     10,
	}

	mockEngine.On("ListJobsWithCursor", ctx, mock.AnythingOfType("*scheduler.JobQuery")).
		Return([]*scheduler.Job{}, "", nil).Once()

	resp, err := adapter.ListJobs(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(resp.Jobs))
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false for empty cursor")
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ListJobs_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "user-1")
	req := &ListSchedulerJobsRequest{
		OwnerType: SchedulerOwnerTypeTenant,
		OwnerID:   "user-1",
	}

	expectedErr := errors.New("list failed")
	mockEngine.On("ListJobsWithCursor", ctx, mock.AnythingOfType("*scheduler.JobQuery")).
		Return(nil, "", expectedErr).Once()

	resp, err := adapter.ListJobs(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_TriggerJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &TriggerSchedulerJobRequest{
		JobID:    300,
		TenantID: "tenant-1",
	}

	existingJob := &scheduler.Job{
		ID:             300,
		Name:           "trigger-job",
		TargetTenantID: "tenant-1",
		Status:         scheduler.JobStatusPending,
	}

	mockEngine.On("GetJobWithTenant", ctx, uint64(300), "tenant-1").
		Return(existingJob, nil).Once()
	mockEngine.On("TriggerJob", ctx, uint64(300), "tenant-1").
		Return(nil).Once()

	resp, err := adapter.TriggerJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JobID != 300 {
		t.Errorf("expected job ID 300, got %d", resp.JobID)
	}
	if resp.Status != SchedulerJobStatusPending {
		t.Errorf("expected status pending, got %s", resp.Status)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_TriggerJob_GetError(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &TriggerSchedulerJobRequest{
		JobID:    300,
		TenantID: "tenant-1",
	}

	expectedErr := errors.New("job not found")
	mockEngine.On("GetJobWithTenant", ctx, uint64(300), "tenant-1").
		Return(nil, expectedErr).Once()

	resp, err := adapter.TriggerJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_TriggerJob_TriggerError(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &TriggerSchedulerJobRequest{
		JobID:    300,
		TenantID: "tenant-1",
	}

	existingJob := &scheduler.Job{
		ID:             300,
		Name:           "trigger-job",
		TargetTenantID: "tenant-1",
		Status:         scheduler.JobStatusPending,
	}

	mockEngine.On("GetJobWithTenant", ctx, uint64(300), "tenant-1").
		Return(existingJob, nil).Once()

	expectedErr := errors.New("trigger failed")
	mockEngine.On("TriggerJob", ctx, uint64(300), "tenant-1").
		Return(expectedErr).Once()

	resp, err := adapter.TriggerJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_PauseJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &PauseSchedulerJobRequest{
		JobID:    400,
		TenantID: "tenant-1",
	}

	mockEngine.On("PauseJobWithStatus", ctx, uint64(400), "tenant-1").
		Return(scheduler.JobStatusRunning, nil).Once()

	resp, err := adapter.PauseJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JobID != 400 {
		t.Errorf("expected job ID 400, got %d", resp.JobID)
	}
	if resp.OldStatus != SchedulerJobStatusRunning {
		t.Errorf("expected old status running, got %s", resp.OldStatus)
	}
	if resp.NewStatus != SchedulerJobStatusPaused {
		t.Errorf("expected new status paused, got %s", resp.NewStatus)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_PauseJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &PauseSchedulerJobRequest{
		JobID:    400,
		TenantID: "tenant-1",
	}

	expectedErr := errors.New("pause failed")
	mockEngine.On("PauseJobWithStatus", ctx, uint64(400), "tenant-1").
		Return(scheduler.JobStatus(""), expectedErr).Once()

	resp, err := adapter.PauseJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ResumeJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &ResumeSchedulerJobRequest{
		JobID:    500,
		TenantID: "tenant-1",
	}

	mockEngine.On("ResumeJobWithStatus", ctx, uint64(500), "tenant-1").
		Return(scheduler.JobStatusPaused, nil).Once()

	resp, err := adapter.ResumeJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JobID != 500 {
		t.Errorf("expected job ID 500, got %d", resp.JobID)
	}
	if resp.OldStatus != SchedulerJobStatusPaused {
		t.Errorf("expected old status paused, got %s", resp.OldStatus)
	}
	if resp.NewStatus != SchedulerJobStatusPending {
		t.Errorf("expected new status pending, got %s", resp.NewStatus)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_ResumeJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &ResumeSchedulerJobRequest{
		JobID:    500,
		TenantID: "tenant-1",
	}

	expectedErr := errors.New("resume failed")
	mockEngine.On("ResumeJobWithStatus", ctx, uint64(500), "tenant-1").
		Return(scheduler.JobStatus(""), expectedErr).Once()

	resp, err := adapter.ResumeJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_CancelJob_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &CancelSchedulerJobRequest{
		JobID:    600,
		TenantID: "tenant-1",
		Reason:   "No longer needed",
	}

	mockEngine.On("CancelJobWithTenant", ctx, uint64(600), "tenant-1", "No longer needed").
		Return(scheduler.JobStatusPending, nil).Once()

	resp, err := adapter.CancelJob(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JobID != 600 {
		t.Errorf("expected job ID 600, got %d", resp.JobID)
	}
	if resp.OldStatus != SchedulerJobStatusPending {
		t.Errorf("expected old status pending, got %s", resp.OldStatus)
	}
	if resp.NewStatus != SchedulerJobStatusCanceled {
		t.Errorf("expected new status canceled, got %s", resp.NewStatus)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_CancelJob_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &CancelSchedulerJobRequest{
		JobID:    600,
		TenantID: "tenant-1",
		Reason:   "No longer needed",
	}

	expectedErr := errors.New("cancel failed")
	mockEngine.On("CancelJobWithTenant", ctx, uint64(600), "tenant-1", "No longer needed").
		Return(scheduler.JobStatus(""), expectedErr).Once()

	resp, err := adapter.CancelJob(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJobHistory_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobHistoryRequest{
		JobID:    700,
		TenantID: "tenant-1",
		Limit:    25,
	}

	now := time.Now()
	executions := []*scheduler.JobExecution{
		{
			ID:        1,
			JobID:     700,
			Status:    scheduler.JobStatusCompleted,
			StartedAt: now.Add(-2 * time.Hour),
			EndedAt:   now.Add(-2*time.Hour + time.Minute),
			Duration:  time.Minute,
			Output:    `{"result":"success"}`,
		},
		{
			ID:        2,
			JobID:     700,
			Status:    scheduler.JobStatusFailed,
			StartedAt: now.Add(-time.Hour),
			EndedAt:   now.Add(-time.Hour + 30*time.Second),
			Duration:  30 * time.Second,
			Error:     "connection timeout",
		},
	}

	mockEngine.On("GetJobHistory", ctx, uint64(700), "tenant-1", 25).
		Return(executions, nil).Once()

	resp, err := adapter.GetJobHistory(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Executions) != 2 {
		t.Errorf("expected 2 executions, got %d", len(resp.Executions))
	}
	if resp.Executions[0].Status != SchedulerJobStatusCompleted {
		t.Errorf("expected first execution status completed, got %s", resp.Executions[0].Status)
	}
	if resp.Executions[1].Error != "connection timeout" {
		t.Errorf("expected second execution error 'connection timeout', got '%s'", resp.Executions[1].Error)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJobHistory_DefaultLimit(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobHistoryRequest{
		JobID:    700,
		TenantID: "tenant-1",
		Limit:    0, // Should default to 100
	}

	mockEngine.On("GetJobHistory", ctx, uint64(700), "tenant-1", 100).
		Return([]*scheduler.JobExecution{}, nil).Once()

	resp, err := adapter.GetJobHistory(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false for empty result")
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJobHistory_HasMore(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobHistoryRequest{
		JobID:    700,
		TenantID: "tenant-1",
		Limit:    2,
	}

	// Return exactly limit number of executions to trigger HasMore
	executions := []*scheduler.JobExecution{
		{ID: 1, JobID: 700, Status: scheduler.JobStatusCompleted},
		{ID: 2, JobID: 700, Status: scheduler.JobStatusCompleted},
	}

	mockEngine.On("GetJobHistory", ctx, uint64(700), "tenant-1", 2).
		Return(executions, nil).Once()

	resp, err := adapter.GetJobHistory(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true when result count equals limit")
	}
	if resp.NextCursor == "" {
		t.Error("expected non-empty cursor when HasMore is true")
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetJobHistory_Error(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	req := &GetSchedulerJobHistoryRequest{
		JobID:    700,
		TenantID: "tenant-1",
		Limit:    25,
	}

	expectedErr := errors.New("history fetch failed")
	mockEngine.On("GetJobHistory", ctx, uint64(700), "tenant-1", 25).
		Return(nil, expectedErr).Once()

	resp, err := adapter.GetJobHistory(ctx, req)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetStatus_Success(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithSystem(context.Background())
	now := time.Now()
	startedAt := now.Add(-time.Hour)

	stats := &scheduler.SchedulerStats{
		JobCount:     100,
		RunningJobs:  5,
		SuccessCount: 80,
		FailureCount: 15,
		LastTickTime: now,
		StartedAt:    startedAt,
	}

	mockEngine.On("Stats").Return(stats).Once()
	mockEngine.On("IsRunning").Return(true).Once()

	resp, err := adapter.GetStatus(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Running {
		t.Error("expected Running to be true")
	}
	if resp.Stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if resp.Stats.JobCount != 100 {
		t.Errorf("expected JobCount 100, got %d", resp.Stats.JobCount)
	}
	if resp.Stats.RunningJobs != 5 {
		t.Errorf("expected RunningJobs 5, got %d", resp.Stats.RunningJobs)
	}
	if resp.Stats.SuccessCount != 80 {
		t.Errorf("expected SuccessCount 80, got %d", resp.Stats.SuccessCount)
	}
	if resp.Stats.FailureCount != 15 {
		t.Errorf("expected FailureCount 15, got %d", resp.Stats.FailureCount)
	}
	mockEngine.AssertExpectations(t)
}

func TestSchedulerAdapter_GetStatus_NotRunning(t *testing.T) {
	mockEngine := newTestMockSchedulerEngine()
	adapter := NewSchedulerAdapter(mockEngine)

	ctx := tenantctx.WithSystem(context.Background())
	stats := &scheduler.SchedulerStats{
		JobCount:     0,
		RunningJobs:  0,
		SuccessCount: 0,
		FailureCount: 0,
	}

	mockEngine.On("Stats").Return(stats).Once()
	mockEngine.On("IsRunning").Return(false).Once()

	resp, err := adapter.GetStatus(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Running {
		t.Error("expected Running to be false")
	}
	mockEngine.AssertExpectations(t)
}

// =============================================================================
// EmbeddingAdapter Tests
// =============================================================================

func TestNewEmbeddingManagerAdapter(t *testing.T) {
	// Test creating adapter with nil - this should work but calls will fail
	adapter := NewEmbeddingManagerAdapter(nil)
	if adapter == nil {
		t.Error("expected non-nil adapter even with nil manager")
	}
	if adapter.manager != nil {
		t.Error("expected nil manager in adapter")
	}
}

func TestEmbeddingManagerAdapter_Unwrap(t *testing.T) {
	// Test unwrap with nil manager
	adapter := NewEmbeddingManagerAdapter(nil)
	result := adapter.Unwrap()
	if result != nil {
		t.Error("expected nil from Unwrap with nil manager")
	}
}
