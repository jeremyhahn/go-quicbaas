// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSchedulerLoggerAdapter_Debugf(t *testing.T) {
	t.Run("calls underlying logger when not nil", func(t *testing.T) {
		mock := &mockLoggerForAdapter{}
		adapter := &schedulerLoggerAdapter{logger: mock}

		adapter.Debugf("test message %s", "arg1")

		assert.Len(t, mock.debugCalls, 1)
		assert.Equal(t, "test message %s", mock.debugCalls[0])
	})

	t.Run("does nothing when logger is nil", func(t *testing.T) {
		adapter := &schedulerLoggerAdapter{logger: nil}

		// Should not panic
		adapter.Debugf("test message %s", "arg1")
	})
}

func TestSchedulerLoggerAdapter_Warnf(t *testing.T) {
	t.Run("calls underlying logger when not nil", func(t *testing.T) {
		mock := &mockLoggerForAdapter{}
		adapter := &schedulerLoggerAdapter{logger: mock}

		adapter.Warnf("warning message %d", 42)

		assert.Len(t, mock.warnCalls, 1)
		assert.Equal(t, "warning message %d", mock.warnCalls[0])
	})

	t.Run("does nothing when logger is nil", func(t *testing.T) {
		adapter := &schedulerLoggerAdapter{logger: nil}

		// Should not panic
		adapter.Warnf("warning message %d", 42)
	})
}

func TestSchedulerLoggerAdapter_Errorf(t *testing.T) {
	t.Run("calls underlying logger when not nil", func(t *testing.T) {
		mock := &mockLoggerForAdapter{}
		adapter := &schedulerLoggerAdapter{logger: mock}

		adapter.Errorf("error message %v", "details")

		assert.Len(t, mock.errorCalls, 1)
		assert.Equal(t, "error message %v", mock.errorCalls[0])
	})

	t.Run("does nothing when logger is nil", func(t *testing.T) {
		adapter := &schedulerLoggerAdapter{logger: nil}

		// Should not panic
		adapter.Errorf("error message %v", "details")
	})
}

func TestSchedulerLoggerAdapter_AllMethods(t *testing.T) {
	t.Run("all methods work with logger", func(t *testing.T) {
		mock := &mockLoggerForAdapter{}
		adapter := &schedulerLoggerAdapter{logger: mock}

		adapter.Debugf("debug %s", "1")
		adapter.Warnf("warn %s", "2")
		adapter.Errorf("error %s", "3")

		assert.Len(t, mock.debugCalls, 1)
		assert.Len(t, mock.warnCalls, 1)
		assert.Len(t, mock.errorCalls, 1)
	})

	t.Run("all methods are safe with nil logger", func(t *testing.T) {
		adapter := &schedulerLoggerAdapter{logger: nil}

		// None of these should panic
		adapter.Debugf("debug")
		adapter.Warnf("warn")
		adapter.Errorf("error")
	})
}

func TestKmsGracePeriodAdapter_JobIDManagement(t *testing.T) {
	t.Run("SetJobID updates jobID", func(t *testing.T) {
		adapter := &kmsGracePeriodAdapter{}

		adapter.SetJobID("job-1")
		assert.Equal(t, "job-1", adapter.jobID)

		adapter.SetJobID("job-2")
		assert.Equal(t, "job-2", adapter.jobID)

		adapter.SetJobID("")
		assert.Equal(t, "", adapter.jobID)
	})

	t.Run("adapter initialization", func(t *testing.T) {
		adapter := &kmsGracePeriodAdapter{
			kms:   nil,
			jobID: "initial-job",
		}

		assert.Equal(t, "initial-job", adapter.jobID)
		assert.Nil(t, adapter.kms)
	})
}

func Test82_SchedulerLoggerAdapter_Debugf(t *testing.T) {
	mockLog := &boost82MockLogger{}
	adapter := &schedulerLoggerAdapter{logger: mockLog}
	adapter.Debugf("test %s", "message")
	assert.True(t, mockLog.debugfCalled)
}

func Test82_SchedulerLoggerAdapter_Warnf(t *testing.T) {
	mockLog := &boost82MockLogger{}
	adapter := &schedulerLoggerAdapter{logger: mockLog}
	adapter.Warnf("test %s", "message")
	assert.True(t, mockLog.warnfCalled)
}

func Test82_SchedulerLoggerAdapter_Errorf(t *testing.T) {
	mockLog := &boost82MockLogger{}
	adapter := &schedulerLoggerAdapter{logger: mockLog}
	adapter.Errorf("test %s", "message")
	assert.True(t, mockLog.errorfCalled)
}

func Test82_SchedulerLoggerAdapter_NilLogger(t *testing.T) {
	adapter := &schedulerLoggerAdapter{logger: nil}
	// Should not panic
	adapter.Debugf("test")
	adapter.Warnf("test")
	adapter.Errorf("test")
}

func Test82_KMSGracePeriodAdapter_SetJobID(t *testing.T) {
	adapter := &kmsGracePeriodAdapter{}
	adapter.SetJobID("test-job-123")
	assert.Equal(t, "test-job-123", adapter.jobID)
}

func Test82_KMSGracePeriodAdapter_SetJobID_Multiple(t *testing.T) {
	adapter := &kmsGracePeriodAdapter{}

	adapter.SetJobID("job-1")
	assert.Equal(t, "job-1", adapter.jobID)

	adapter.SetJobID("job-2")
	assert.Equal(t, "job-2", adapter.jobID)
}

func TestSchedulerAdapter_ConvertToSchedulerJob_AllFields(t *testing.T) {
	adapter := &SchedulerAdapter{}

	now := time.Now()
	executeAt := now.Add(time.Hour)

	job := &SchedulerJob{
		ID:             1,
		Name:           "Test Job",
		Description:    "A test job",
		OwnerType:      SchedulerOwnerTypeSystem,
		OwnerID:        "system-1",
		TargetTenantID: "tenant-1",
		CreatedBy:      "admin",
		Type:           SchedulerJobTypeRecurring,
		Schedule:       "0 * * * *",
		ExecuteAt:      executeAt,
		Delay:          time.Minute,
		Handler:        "handler-1",
		Payload:        []byte(`{"key": "value"}`),
		Priority:       5,
		Timeout:        time.Hour,
		MaxRetries:     3,
		Status:         SchedulerJobStatusPending,
		LastRun:        now.Add(-time.Hour),
		NextRun:        now.Add(time.Hour),
		RunCount:       10,
		FailCount:      2,
		LastError:      "previous error",
		Labels:         map[string]string{"env": "test"},
		CreatedAt:      now.Add(-time.Hour * 24),
		UpdatedAt:      now,
	}

	result := adapter.convertToSchedulerJob(job)

	if result.ID != job.ID {
		t.Errorf("ID mismatch: got %d, want %d", result.ID, job.ID)
	}
	if result.Name != job.Name {
		t.Errorf("Name mismatch: got %s, want %s", result.Name, job.Name)
	}
	if result.Description != job.Description {
		t.Errorf("Description mismatch: got %s, want %s", result.Description, job.Description)
	}
	if result.Schedule != job.Schedule {
		t.Errorf("Schedule mismatch: got %s, want %s", result.Schedule, job.Schedule)
	}
	if result.Handler != job.Handler {
		t.Errorf("Handler mismatch: got %s, want %s", result.Handler, job.Handler)
	}
	if result.Priority != job.Priority {
		t.Errorf("Priority mismatch: got %d, want %d", result.Priority, job.Priority)
	}
	if result.MaxRetries != job.MaxRetries {
		t.Errorf("MaxRetries mismatch: got %d, want %d", result.MaxRetries, job.MaxRetries)
	}
	if result.RunCount != job.RunCount {
		t.Errorf("RunCount mismatch: got %d, want %d", result.RunCount, job.RunCount)
	}
	if result.FailCount != job.FailCount {
		t.Errorf("FailCount mismatch: got %d, want %d", result.FailCount, job.FailCount)
	}
	if result.LastError != job.LastError {
		t.Errorf("LastError mismatch: got %s, want %s", result.LastError, job.LastError)
	}
	if string(result.Payload) != string(job.Payload) {
		t.Errorf("Payload mismatch")
	}
	if len(result.Labels) != len(job.Labels) {
		t.Errorf("Labels length mismatch")
	}
}

func TestSchedulerAdapter_ConvertToServiceJob_RoundTrip(t *testing.T) {
	adapter := &SchedulerAdapter{}

	now := time.Now()

	// Create a service job, convert to scheduler, then back to verify round-trip
	originalJob := &SchedulerJob{
		ID:             2,
		Name:           "Round Trip Job",
		Description:    "Testing round-trip conversion",
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "user-1",
		TargetTenantID: "tenant-2",
		CreatedBy:      "user-1",
		Type:           SchedulerJobTypeDelayed,
		Schedule:       "",
		ExecuteAt:      now.Add(time.Hour),
		Delay:          time.Minute * 5,
		Handler:        "delayed-handler",
		Payload:        []byte(`{"action": "notify"}`),
		Priority:       10,
		Timeout:        time.Minute * 30,
		MaxRetries:     5,
		Status:         SchedulerJobStatusRunning,
		LastRun:        now.Add(-time.Minute),
		NextRun:        time.Time{},
		RunCount:       1,
		FailCount:      0,
		LastError:      "",
		Labels:         map[string]string{"type": "notification"},
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now,
	}

	// Convert to scheduler job
	schedulerJob := adapter.convertToSchedulerJob(originalJob)

	// Convert back to service job
	resultJob := adapter.convertToServiceJob(schedulerJob)

	// Verify all fields match
	if resultJob.ID != originalJob.ID {
		t.Errorf("ID mismatch after round-trip")
	}
	if resultJob.Name != originalJob.Name {
		t.Errorf("Name mismatch after round-trip")
	}
	if resultJob.Description != originalJob.Description {
		t.Errorf("Description mismatch after round-trip")
	}
	if resultJob.Handler != originalJob.Handler {
		t.Errorf("Handler mismatch after round-trip")
	}
	if resultJob.Priority != originalJob.Priority {
		t.Errorf("Priority mismatch after round-trip")
	}
}

func TestNewSchedulerAdapter_NilScheduler(t *testing.T) {
	// Creating adapter with nil scheduler should not panic
	adapter := NewSchedulerAdapter(nil)
	if adapter == nil {
		t.Error("expected non-nil adapter")
	}
	if adapter.scheduler != nil {
		t.Error("expected nil scheduler in adapter")
	}
}

func TestSchedulerJobExecution_StructFields(t *testing.T) {
	now := time.Now()
	endedAt := now.Add(time.Minute)
	duration := time.Minute

	exec := &SchedulerJobExecution{
		ID:        1,
		JobID:     123,
		Status:    SchedulerJobStatusCompleted,
		StartedAt: now,
		EndedAt:   endedAt,
		Duration:  duration,
		Error:     "",
		Output:    `{"result": "success"}`,
	}

	if exec.ID != 1 {
		t.Errorf("ID mismatch: got %d, want 1", exec.ID)
	}
	if exec.JobID != 123 {
		t.Errorf("JobID mismatch: got %d, want 123", exec.JobID)
	}
	if exec.Status != SchedulerJobStatusCompleted {
		t.Errorf("Status mismatch")
	}
	if !exec.StartedAt.Equal(now) {
		t.Errorf("StartedAt mismatch")
	}
	if !exec.EndedAt.Equal(endedAt) {
		t.Errorf("EndedAt mismatch")
	}
	if exec.Duration != duration {
		t.Errorf("Duration mismatch")
	}
	if exec.Error != "" {
		t.Errorf("Error should be empty")
	}
	if exec.Output != `{"result": "success"}` {
		t.Errorf("Output mismatch")
	}
}

func TestSchedulerJobExecution_WithError(t *testing.T) {
	now := time.Now()

	exec := &SchedulerJobExecution{
		ID:        2,
		JobID:     456,
		Status:    SchedulerJobStatusFailed,
		StartedAt: now,
		EndedAt:   now.Add(time.Second),
		Duration:  time.Second,
		Error:     "task failed: connection timeout",
		Output:    "",
	}

	if exec.Status != SchedulerJobStatusFailed {
		t.Errorf("Status should be failed")
	}
	if exec.Error != "task failed: connection timeout" {
		t.Errorf("Error mismatch: got %s", exec.Error)
	}
}

func TestSchedulerStats_StructFields(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-time.Hour)

	stats := &SchedulerStats{
		JobCount:     100,
		RunningJobs:  5,
		SuccessCount: 80,
		FailureCount: 15,
		LastTickTime: now,
		StartedAt:    startedAt,
	}

	if stats.JobCount != 100 {
		t.Errorf("JobCount mismatch: got %d, want 100", stats.JobCount)
	}
	if stats.RunningJobs != 5 {
		t.Errorf("RunningJobs mismatch: got %d, want 5", stats.RunningJobs)
	}
	if stats.SuccessCount != 80 {
		t.Errorf("SuccessCount mismatch: got %d, want 80", stats.SuccessCount)
	}
	if stats.FailureCount != 15 {
		t.Errorf("FailureCount mismatch: got %d, want 15", stats.FailureCount)
	}
	if !stats.LastTickTime.Equal(now) {
		t.Errorf("LastTickTime mismatch")
	}
	if !stats.StartedAt.Equal(startedAt) {
		t.Errorf("StartedAt mismatch")
	}
}

func TestCreateSchedulerJobRequest_StructFields(t *testing.T) {
	now := time.Now()
	req := &CreateSchedulerJobRequest{
		Name:           "Test Job",
		Description:    "A test scheduled job",
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "user-1",
		TargetTenantID: "tenant-1",
		Type:           SchedulerJobTypeRecurring,
		Schedule:       "0 0 * * *",
		ExecuteAt:      now,
		Delay:          time.Minute,
		Handler:        "test-handler",
		Payload:        []byte(`{}`),
		Priority:       5,
		Timeout:        time.Hour,
		MaxRetries:     3,
		Labels:         map[string]string{"key": "value"},
	}

	if req.Name != "Test Job" {
		t.Error("Name mismatch")
	}
	if req.Schedule != "0 0 * * *" {
		t.Error("Schedule mismatch")
	}
}

func TestGetSchedulerJobRequest_StructFields(t *testing.T) {
	req := &GetSchedulerJobRequest{
		JobID:    123,
		TenantID: "tenant-1",
	}

	if req.JobID != 123 {
		t.Error("JobID mismatch")
	}
	if req.TenantID != "tenant-1" {
		t.Error("TenantID mismatch")
	}
}

func TestUpdateSchedulerJobRequest_StructFields(t *testing.T) {
	req := &UpdateSchedulerJobRequest{
		JobID:       456,
		TenantID:    "tenant-1",
		Name:        "Updated Name",
		Description: "Updated Description",
		Schedule:    "0 1 * * *",
		Handler:     "new-handler",
		Priority:    10,
		MaxRetries:  5,
		Labels:      map[string]string{"updated": "true"},
	}

	if req.JobID != 456 {
		t.Error("JobID mismatch")
	}
	if req.Name != "Updated Name" {
		t.Error("Name mismatch")
	}
	if req.Priority != 10 {
		t.Error("Priority mismatch")
	}
}

func TestDeleteSchedulerJobRequest_StructFields(t *testing.T) {
	req := &DeleteSchedulerJobRequest{
		JobID:    789,
		TenantID: "tenant-1",
		Force:    true,
	}

	if req.JobID != 789 {
		t.Error("JobID mismatch")
	}
	if !req.Force {
		t.Error("Force should be true")
	}
}

func TestListSchedulerJobsRequest_StructFields(t *testing.T) {
	req := &ListSchedulerJobsRequest{
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "user-1",
		TargetTenantID: "tenant-1",
		Status:         SchedulerJobStatusPending,
		Handler:        "test-handler",
		Labels:         map[string]string{"env": "prod"},
		Limit:          50,
		Cursor:         "cursor-token",
	}

	if req.OwnerType != SchedulerOwnerTypeTenant {
		t.Error("OwnerType mismatch")
	}
	if req.Limit != 50 {
		t.Error("Limit mismatch")
	}
	if req.Cursor != "cursor-token" {
		t.Error("Cursor mismatch")
	}
}

func TestTriggerSchedulerJobRequest_StructFields(t *testing.T) {
	req := &TriggerSchedulerJobRequest{
		JobID:    100,
		TenantID: "tenant-1",
	}

	if req.JobID != 100 {
		t.Error("JobID mismatch")
	}
}

func TestPauseSchedulerJobRequest_StructFields(t *testing.T) {
	req := &PauseSchedulerJobRequest{
		JobID:    200,
		TenantID: "tenant-1",
	}

	if req.JobID != 200 {
		t.Error("JobID mismatch")
	}
}

func TestResumeSchedulerJobRequest_StructFields(t *testing.T) {
	req := &ResumeSchedulerJobRequest{
		JobID:    300,
		TenantID: "tenant-1",
	}

	if req.JobID != 300 {
		t.Error("JobID mismatch")
	}
}

func TestCancelSchedulerJobRequest_StructFields(t *testing.T) {
	req := &CancelSchedulerJobRequest{
		JobID:    400,
		TenantID: "tenant-1",
		Reason:   "No longer needed",
	}

	if req.JobID != 400 {
		t.Error("JobID mismatch")
	}
	if req.Reason != "No longer needed" {
		t.Error("Reason mismatch")
	}
}

func TestGetSchedulerJobHistoryRequest_StructFields(t *testing.T) {
	req := &GetSchedulerJobHistoryRequest{
		JobID:    500,
		TenantID: "tenant-1",
		Limit:    25,
	}

	if req.JobID != 500 {
		t.Error("JobID mismatch")
	}
	if req.Limit != 25 {
		t.Error("Limit mismatch")
	}
}

func TestCreateSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &CreateSchedulerJobResponse{
		Job:       &SchedulerJob{ID: 1, Name: "New Job"},
		Timestamp: now,
	}

	if resp.Job.ID != 1 {
		t.Error("Job ID mismatch")
	}
	if !resp.Timestamp.Equal(now) {
		t.Error("Timestamp mismatch")
	}
}

func TestGetSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &GetSchedulerJobResponse{
		Job:       &SchedulerJob{ID: 1, Name: "Got Job"},
		Timestamp: now,
	}

	if resp.Job.ID != 1 {
		t.Error("Job ID mismatch")
	}
}

func TestListSchedulerJobsResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &ListSchedulerJobsResponse{
		Jobs: []*SchedulerJob{
			{ID: 1, Name: "Job 1"},
			{ID: 2, Name: "Job 2"},
		},
		NextCursor: "next-cursor",
		HasMore:    true,
		TotalCount: 2,
		Timestamp:  now,
	}

	if len(resp.Jobs) != 2 {
		t.Errorf("Jobs count mismatch: got %d, want 2", len(resp.Jobs))
	}
	if resp.NextCursor != "next-cursor" {
		t.Error("NextCursor mismatch")
	}
	if !resp.HasMore {
		t.Error("HasMore should be true")
	}
}

func TestDeleteSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &DeleteSchedulerJobResponse{
		JobID:     600,
		Deleted:   true,
		Timestamp: now,
	}

	if resp.JobID != 600 {
		t.Error("JobID mismatch")
	}
	if !resp.Deleted {
		t.Error("Deleted should be true")
	}
}

func TestTriggerSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &TriggerSchedulerJobResponse{
		JobID:     700,
		Status:    SchedulerJobStatusRunning,
		Timestamp: now,
	}

	if resp.JobID != 700 {
		t.Error("JobID mismatch")
	}
	if resp.Status != SchedulerJobStatusRunning {
		t.Error("Status mismatch")
	}
}

func TestPauseSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &PauseSchedulerJobResponse{
		JobID:     800,
		OldStatus: SchedulerJobStatusRunning,
		NewStatus: SchedulerJobStatusPaused,
		Timestamp: now,
	}

	if resp.OldStatus != SchedulerJobStatusRunning {
		t.Error("OldStatus mismatch")
	}
	if resp.NewStatus != SchedulerJobStatusPaused {
		t.Error("NewStatus mismatch")
	}
}

func TestResumeSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &ResumeSchedulerJobResponse{
		JobID:     900,
		OldStatus: SchedulerJobStatusPaused,
		NewStatus: SchedulerJobStatusPending,
		Timestamp: now,
	}

	if resp.OldStatus != SchedulerJobStatusPaused {
		t.Error("OldStatus mismatch")
	}
	if resp.NewStatus != SchedulerJobStatusPending {
		t.Error("NewStatus mismatch")
	}
}

func TestCancelSchedulerJobResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &CancelSchedulerJobResponse{
		JobID:     1000,
		OldStatus: SchedulerJobStatusPending,
		NewStatus: SchedulerJobStatusCanceled,
		Timestamp: now,
	}

	if resp.OldStatus != SchedulerJobStatusPending {
		t.Error("OldStatus mismatch")
	}
	if resp.NewStatus != SchedulerJobStatusCanceled {
		t.Error("NewStatus mismatch")
	}
}

func TestGetSchedulerJobHistoryResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &GetSchedulerJobHistoryResponse{
		Executions: []*SchedulerJobExecution{
			{ID: 1, JobID: 1, Status: SchedulerJobStatusCompleted},
			{ID: 2, JobID: 1, Status: SchedulerJobStatusFailed},
		},
		NextCursor: "cursor-2",
		HasMore:    true,
		Timestamp:  now,
	}

	if len(resp.Executions) != 2 {
		t.Errorf("Executions count mismatch: got %d, want 2", len(resp.Executions))
	}
	if resp.NextCursor != "cursor-2" {
		t.Error("NextCursor mismatch")
	}
	if !resp.HasMore {
		t.Error("HasMore should be true")
	}
}

func TestGetSchedulerStatusResponse_StructFields(t *testing.T) {
	now := time.Now()
	resp := &GetSchedulerStatusResponse{
		Running: true,
		Stats: &SchedulerStats{
			JobCount:     50,
			RunningJobs:  3,
			SuccessCount: 40,
			FailureCount: 7,
		},
		Timestamp: now,
	}

	if !resp.Running {
		t.Error("Running should be true")
	}
	if resp.Stats == nil {
		t.Fatal("Stats should not be nil")
	}
	if resp.Stats.JobCount != 50 {
		t.Error("JobCount mismatch")
	}
}
