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

//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Job types.
const (
	JobTypeOneShot   = "one-shot"
	JobTypeRecurring = "recurring"
	JobTypeDelayed   = "delayed"
)

// Job statuses.
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusPaused    = "paused"
	JobStatusCanceled  = "canceled"
)

// Owner types.
const (
	OwnerTypeSystem    = "system"
	OwnerTypeTenant    = "tenant"
	OwnerTypeDelegated = "delegated"
)

// testJobHandler is an arbitrary, non-empty handler name. The scheduler
// requires a non-empty handler at creation time but only resolves the
// handler to a registered implementation at execution time, so any
// non-empty string is accepted for create/get/update/list assertions.
const testJobHandler = "noop"

// createJobRequest mirrors the server-side CreateSchedulerJobRESTRequest
// (pkg/api/rest/handlers_scheduler.go). The scheduler requires name,
// handler, and type on every job; one-shot jobs additionally require
// execute_at (RFC3339), recurring jobs require a cron schedule, and
// delayed jobs require a positive delay (encoded as a time.Duration).
type createJobRequest struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Handler     string                 `json:"handler"`
	Schedule    string                 `json:"schedule,omitempty"`
	ExecuteAt   string                 `json:"execute_at,omitempty"`
	Delay       time.Duration          `json:"delay,omitempty"`
	Payload     map[string]interface{} `json:"-"`
	OwnerType   string                 `json:"owner_type,omitempty"`
	OwnerID     string                 `json:"owner_id,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
	MaxRetries  int32                  `json:"max_retries,omitempty"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// updateJobRequest mirrors the server-side UpdateSchedulerJobRESTRequest.
type updateJobRequest struct {
	Name        string                 `json:"name,omitempty"`
	Schedule    string                 `json:"schedule,omitempty"`
	Payload     map[string]interface{} `json:"-"`
	Labels      map[string]string      `json:"labels,omitempty"`
	MaxRetries  int32                  `json:"max_retries,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// jobResponse mirrors the server-side SchedulerJobRESTResponse. The job ID
// is a uint64 and the payload is raw JSON.
type jobResponse struct {
	ID          uint64                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Schedule    string                 `json:"schedule,omitempty"`
	ExecuteAt   time.Time              `json:"execute_at,omitempty"`
	Delay       time.Duration          `json:"delay,omitempty"`
	Handler     string                 `json:"handler"`
	Payload     map[string]interface{} `json:"-"`
	RawPayload  json.RawMessage        `json:"payload,omitempty"`
	OwnerType   string                 `json:"owner_type,omitempty"`
	OwnerID     string                 `json:"owner_id,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
	MaxRetries  int32                  `json:"max_retries,omitempty"`
	Description string                 `json:"description,omitempty"`
	NextRun     time.Time              `json:"next_run,omitempty"`
	LastRun     time.Time              `json:"last_run,omitempty"`
	RunCount    uint64                 `json:"run_count,omitempty"`
	CreatedAt   time.Time              `json:"created_at,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at,omitempty"`
}

// decodePayload populates the convenience Payload map from the raw payload.
func (j *jobResponse) decodePayload() error {
	if len(j.RawPayload) == 0 {
		return nil
	}
	j.Payload = map[string]interface{}{}
	if err := json.Unmarshal(j.RawPayload, &j.Payload); err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to decode job payload", Cause: err}
	}
	return nil
}

// jobEnvelope wraps a single job, matching the create/get/update REST
// responses which return {"job": {...}, "timestamp": ...}.
type jobEnvelope struct {
	Job *jobResponse `json:"job"`
}

// statusChangeResponse matches the trigger/pause/resume/cancel REST
// responses which return a status transition rather than the full job.
type statusChangeResponse struct {
	JobID     uint64 `json:"job_id"`
	Status    string `json:"status,omitempty"`
	OldStatus string `json:"old_status,omitempty"`
	NewStatus string `json:"new_status,omitempty"`
}

// listJobsResponse mirrors the server-side ListSchedulerJobsRESTResponse.
type listJobsResponse struct {
	Jobs       []jobResponse `json:"jobs"`
	Total      int           `json:"total_count,omitempty"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more,omitempty"`
}

// jobHistoryEntry mirrors the server-side SchedulerJobExecutionRESTResponse.
type jobHistoryEntry struct {
	ID        uint64        `json:"id"`
	JobID     uint64        `json:"job_id"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
}

// jobHistoryResponse mirrors the server-side GetSchedulerJobHistoryRESTResponse.
type jobHistoryResponse struct {
	Executions []jobHistoryEntry `json:"executions"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more,omitempty"`
}

// schedulerStatusResponse mirrors the server-side
// GetSchedulerStatusRESTResponse, including the nested stats object.
type schedulerStatusResponse struct {
	Running bool `json:"running"`
	Stats   struct {
		JobCount     uint64    `json:"job_count"`
		RunningJobs  uint64    `json:"running_jobs"`
		SuccessCount uint64    `json:"success_count"`
		FailureCount uint64    `json:"failure_count"`
		LastTickTime time.Time `json:"last_tick_time,omitempty"`
		StartedAt    time.Time `json:"started_at"`
	} `json:"stats"`
}

// TestSchedulerCreateJob tests creating scheduler jobs.
func TestSchedulerCreateJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("create one-shot job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("oneshot_test_%d", time.Now().UnixNano())
		req := createJobRequest{
			Name:        jobName,
			Type:        JobTypeOneShot,
			Handler:     testJobHandler,
			ExecuteAt:   time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			OwnerType:   OwnerTypeSystem,
			Description: "Test one-shot job",
			Payload:     map[string]interface{}{"task": "run_once", "priority": 1},
		}

		job, err := schedulerCreateJob(ctx, req)
		if err != nil {
			t.Fatalf("create one-shot job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		if job.ID == 0 {
			t.Error("expected job ID to be set")
		}
		if job.Name != jobName {
			t.Errorf("expected name %q, got %q", jobName, job.Name)
		}
		if job.Type != JobTypeOneShot {
			t.Errorf("expected type %q, got %q", JobTypeOneShot, job.Type)
		}

		t.Logf("created one-shot job: id=%d, name=%s, status=%s", job.ID, job.Name, job.Status)
	})

	t.Run("create recurring job with cron schedule", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("recurring_test_%d", time.Now().UnixNano())
		req := createJobRequest{
			Name:        jobName,
			Type:        JobTypeRecurring,
			Handler:     testJobHandler,
			Schedule:    "*/5 * * * *", // Every 5 minutes
			OwnerType:   OwnerTypeTenant,
			OwnerID:     "tenant-123",
			Description: "Test recurring job",
			Payload:     map[string]interface{}{"task": "run_periodic"},
			Labels:      map[string]string{"env": "test", "kind": "recurring"},
			MaxRetries:  3,
			Timeout:     5 * time.Minute,
		}

		job, err := schedulerCreateJob(ctx, req)
		if err != nil {
			t.Fatalf("create recurring job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		if job.Type != JobTypeRecurring {
			t.Errorf("expected type %q, got %q", JobTypeRecurring, job.Type)
		}
		if job.Schedule != "*/5 * * * *" {
			t.Errorf("expected schedule %q, got %q", "*/5 * * * *", job.Schedule)
		}

		t.Logf("created recurring job: id=%d, schedule=%s", job.ID, job.Schedule)
	})

	t.Run("create delayed job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("delayed_test_%d", time.Now().UnixNano())
		req := createJobRequest{
			Name:        jobName,
			Type:        JobTypeDelayed,
			Handler:     testJobHandler,
			Delay:       1 * time.Hour,
			OwnerType:   OwnerTypeDelegated,
			Description: "Test delayed job",
			Payload:     map[string]interface{}{"task": "run_later"},
		}

		job, err := schedulerCreateJob(ctx, req)
		if err != nil {
			t.Fatalf("create delayed job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		if job.Type != JobTypeDelayed {
			t.Errorf("expected type %q, got %q", JobTypeDelayed, job.Type)
		}

		t.Logf("created delayed job: id=%d, delay=%s", job.ID, job.Delay)
	})

	t.Run("create job with invalid type returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Name:    fmt.Sprintf("invalid_test_%d", time.Now().UnixNano()),
			Type:    "invalid-type",
			Handler: testJobHandler,
		}

		_, err := schedulerCreateJob(ctx, req)
		if err == nil {
			t.Error("expected error for invalid job type")
		}
		t.Logf("create with invalid type returned error (expected): %v", err)
	})

	t.Run("create job with missing name returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Type:    JobTypeOneShot,
			Handler: testJobHandler,
		}

		_, err := schedulerCreateJob(ctx, req)
		if err == nil {
			t.Error("expected error for missing job name")
		}
		t.Logf("create with missing name returned error (expected): %v", err)
	})

	t.Run("create job with missing handler returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Name:      fmt.Sprintf("no_handler_test_%d", time.Now().UnixNano()),
			Type:      JobTypeOneShot,
			ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
		}

		_, err := schedulerCreateJob(ctx, req)
		if err == nil {
			t.Error("expected error for missing job handler")
		}
		t.Logf("create with missing handler returned error (expected): %v", err)
	})

	t.Run("create recurring job without schedule returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Name:    fmt.Sprintf("no_schedule_test_%d", time.Now().UnixNano()),
			Type:    JobTypeRecurring,
			Handler: testJobHandler,
		}

		_, err := schedulerCreateJob(ctx, req)
		if err == nil {
			t.Error("expected error for recurring job without schedule")
		}
		t.Logf("create recurring without schedule returned error (expected): %v", err)
	})
}

// TestSchedulerGetJob tests getting a job by ID.
func TestSchedulerGetJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get existing job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a job first
		jobName := fmt.Sprintf("get_test_%d", time.Now().UnixNano())
		created, err := schedulerCreateJob(ctx, createJobRequest{
			Name:      jobName,
			Type:      JobTypeOneShot,
			Handler:   testJobHandler,
			ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			Payload:   map[string]interface{}{"test": true},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, created.ID)

		// Get the job
		job, err := schedulerGetJob(ctx, created.ID)
		if err != nil {
			t.Fatalf("get job failed: %v", err)
		}

		if job.ID != created.ID {
			t.Errorf("expected ID %d, got %d", created.ID, job.ID)
		}
		if job.Name != jobName {
			t.Errorf("expected name %q, got %q", jobName, job.Name)
		}

		t.Logf("retrieved job: id=%d, name=%s, status=%s", job.ID, job.Name, job.Status)
	})

	t.Run("get non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerGetJob(ctx, nonExistentJobID)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("get non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerUpdateJob tests updating a job.
func TestSchedulerUpdateJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("update job name and description", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a job
		jobName := fmt.Sprintf("update_test_%d", time.Now().UnixNano())
		created, err := schedulerCreateJob(ctx, createJobRequest{
			Name:        jobName,
			Type:        JobTypeOneShot,
			Handler:     testJobHandler,
			ExecuteAt:   time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			Description: "Original description",
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, created.ID)

		// Update the job
		newName := fmt.Sprintf("updated_test_%d", time.Now().UnixNano())
		updated, err := schedulerUpdateJob(ctx, created.ID, updateJobRequest{
			Name:        newName,
			Description: "Updated description",
		})
		if err != nil {
			t.Fatalf("update job failed: %v", err)
		}

		if updated.Name != newName {
			t.Errorf("expected updated name %q, got %q", newName, updated.Name)
		}
		if updated.Description != "Updated description" {
			t.Errorf("expected updated description")
		}

		t.Logf("updated job: id=%d, new_name=%s", updated.ID, updated.Name)
	})

	t.Run("update job payload", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("payload_update_test_%d", time.Now().UnixNano())
		created, err := schedulerCreateJob(ctx, createJobRequest{
			Name:      jobName,
			Type:      JobTypeOneShot,
			Handler:   testJobHandler,
			ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			Payload:   map[string]interface{}{"version": 1},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, created.ID)

		// Update payload
		updated, err := schedulerUpdateJob(ctx, created.ID, updateJobRequest{
			Payload: map[string]interface{}{"version": 2, "extra": "data"},
		})
		if err != nil {
			t.Fatalf("update job failed: %v", err)
		}

		if updated.Payload["version"] != float64(2) {
			t.Errorf("expected payload version 2, got %v", updated.Payload["version"])
		}

		t.Logf("updated job payload: %v", updated.Payload)
	})

	t.Run("update non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerUpdateJob(ctx, nonExistentJobID, updateJobRequest{
			Name: "should-fail",
		})
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("update non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerListJobs tests listing jobs.
func TestSchedulerListJobs(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("list all jobs", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create several jobs
		var createdIDs []uint64
		for i := 0; i < 3; i++ {
			jobName := fmt.Sprintf("list_test_%d_%d", time.Now().UnixNano(), i)
			job, err := schedulerCreateJob(ctx, createJobRequest{
				Name:      jobName,
				Type:      JobTypeOneShot,
				Handler:   testJobHandler,
				ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
				Payload:   map[string]interface{}{"index": i},
			})
			if err != nil {
				t.Fatalf("create job %d failed: %v", i, err)
			}
			createdIDs = append(createdIDs, job.ID)
		}
		defer func() {
			for _, id := range createdIDs {
				cleanupJob(ctx, id)
			}
		}()

		// List jobs
		resp, err := schedulerListJobs(ctx, 0, "", "", "")
		if err != nil {
			t.Fatalf("list jobs failed: %v", err)
		}

		if len(resp.Jobs) < 3 {
			t.Errorf("expected at least 3 jobs, got %d", len(resp.Jobs))
		}

		t.Logf("listed %d jobs (total: %d)", len(resp.Jobs), resp.Total)
	})

	t.Run("list jobs with limit", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		resp, err := schedulerListJobs(ctx, 2, "", "", "")
		if err != nil {
			t.Fatalf("list jobs with limit failed: %v", err)
		}

		if len(resp.Jobs) > 2 {
			t.Errorf("expected at most 2 jobs, got %d", len(resp.Jobs))
		}

		t.Logf("listed %d jobs with limit 2", len(resp.Jobs))
	})

	t.Run("list jobs by status", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		resp, err := schedulerListJobs(ctx, 0, "", JobStatusPending, "")
		if err != nil {
			t.Fatalf("list jobs by status failed: %v", err)
		}

		for _, job := range resp.Jobs {
			if job.Status != JobStatusPending {
				t.Errorf("expected status %q, got %q", JobStatusPending, job.Status)
			}
		}

		t.Logf("listed %d pending jobs", len(resp.Jobs))
	})

	t.Run("list jobs by handler", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		resp, err := schedulerListJobs(ctx, 0, "", "", testJobHandler)
		if err != nil {
			t.Fatalf("list jobs by handler failed: %v", err)
		}

		for _, job := range resp.Jobs {
			if job.Handler != testJobHandler {
				t.Errorf("expected handler %q, got %q", testJobHandler, job.Handler)
			}
		}

		t.Logf("listed %d jobs for handler %q", len(resp.Jobs), testJobHandler)
	})
}

// TestSchedulerTriggerJob tests triggering a job.
func TestSchedulerTriggerJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("trigger job immediately", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a recurring job
		jobName := fmt.Sprintf("trigger_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:     jobName,
			Type:     JobTypeRecurring,
			Handler:  testJobHandler,
			Schedule: "0 0 * * *", // Once a day (won't run during test)
			Payload:  map[string]interface{}{"task": "triggered"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		// Trigger the job
		triggered, err := schedulerTriggerJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("trigger job failed: %v", err)
		}

		if triggered.JobID != job.ID {
			t.Errorf("expected triggered job id %d, got %d", job.ID, triggered.JobID)
		}

		t.Logf("triggered job: id=%d, status=%s", triggered.JobID, triggered.Status)
	})

	t.Run("trigger non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerTriggerJob(ctx, nonExistentJobID)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("trigger non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerPauseResumeJob tests pausing and resuming a job.
func TestSchedulerPauseResumeJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("pause and resume job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a recurring job
		jobName := fmt.Sprintf("pause_resume_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:     jobName,
			Type:     JobTypeRecurring,
			Handler:  testJobHandler,
			Schedule: "*/10 * * * *",
			Payload:  map[string]interface{}{"task": "pausable"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		// Pause the job
		paused, err := schedulerPauseJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("pause job failed: %v", err)
		}

		if paused.NewStatus != JobStatusPaused {
			t.Errorf("expected status %q after pause, got %q", JobStatusPaused, paused.NewStatus)
		}
		t.Logf("paused job: id=%d, status=%s", paused.JobID, paused.NewStatus)

		// Resume the job
		resumed, err := schedulerResumeJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("resume job failed: %v", err)
		}

		if resumed.NewStatus == JobStatusPaused {
			t.Error("expected status to change after resume")
		}
		t.Logf("resumed job: id=%d, status=%s", resumed.JobID, resumed.NewStatus)
	})

	t.Run("pause non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerPauseJob(ctx, nonExistentJobID)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("pause non-existent job returned error (expected): %v", err)
	})

	t.Run("resume non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerResumeJob(ctx, nonExistentJobID)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("resume non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerCancelJob tests canceling a job.
func TestSchedulerCancelJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("cancel job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a delayed job
		jobName := fmt.Sprintf("cancel_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:    jobName,
			Type:    JobTypeDelayed,
			Handler: testJobHandler,
			Delay:   24 * time.Hour,
			Payload: map[string]interface{}{"task": "cancelable"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		// Cancel the job
		canceled, err := schedulerCancelJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("cancel job failed: %v", err)
		}

		if canceled.NewStatus != JobStatusCanceled {
			t.Errorf("expected status %q after cancel, got %q", JobStatusCanceled, canceled.NewStatus)
		}
		t.Logf("canceled job: id=%d, status=%s", canceled.JobID, canceled.NewStatus)
	})

	t.Run("cancel non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerCancelJob(ctx, nonExistentJobID)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("cancel non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerDeleteJob tests deleting a job.
func TestSchedulerDeleteJob(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("delete job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a job
		jobName := fmt.Sprintf("delete_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:      jobName,
			Type:      JobTypeOneShot,
			Handler:   testJobHandler,
			ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			Payload:   map[string]interface{}{"task": "deletable"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}

		// Delete the job
		err = schedulerDeleteJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("delete job failed: %v", err)
		}
		t.Logf("deleted job: id=%d", job.ID)

		// Verify job is gone
		_, err = schedulerGetJob(ctx, job.ID)
		if err == nil {
			t.Error("expected error for deleted job")
		}
	})

	t.Run("delete non-existent job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Deleting non-existent job may succeed or fail depending on implementation
		err := schedulerDeleteJob(ctx, nonExistentJobID)
		t.Logf("delete non-existent job response: error=%v", err)
	})
}

// TestSchedulerJobHistory tests getting job execution history.
func TestSchedulerJobHistory(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get job history", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a job to query history for
		jobName := fmt.Sprintf("history_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:      jobName,
			Type:      JobTypeOneShot,
			Handler:   testJobHandler,
			ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
			Payload:   map[string]interface{}{"task": "history_test"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		// Get history
		history, err := schedulerGetJobHistory(ctx, job.ID, 10)
		if err != nil {
			t.Fatalf("get job history failed: %v", err)
		}

		t.Logf("job history: %d entries", len(history.Executions))
		for i, entry := range history.Executions {
			t.Logf("  entry %d: id=%d, status=%s, started=%s", i, entry.ID, entry.Status, entry.StartedAt)
		}
	})

	t.Run("get history for non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerGetJobHistory(ctx, nonExistentJobID, 10)
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("get history for non-existent job returned error (expected): %v", err)
	})
}

// TestSchedulerStatus tests getting scheduler status.
func TestSchedulerStatus(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get scheduler status", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		status, err := schedulerGetStatus(ctx)
		if err != nil {
			t.Fatalf("get scheduler status failed: %v", err)
		}

		if !status.Running {
			t.Error("expected scheduler to be running")
		}

		t.Logf("scheduler status: running=%v, job_count=%d, running_jobs=%d, success=%d, failures=%d",
			status.Running, status.Stats.JobCount, status.Stats.RunningJobs,
			status.Stats.SuccessCount, status.Stats.FailureCount)
	})
}

// TestSchedulerJobLifecycle tests the full job lifecycle.
func TestSchedulerJobLifecycle(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("full job lifecycle", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// 1. Create
		jobName := fmt.Sprintf("lifecycle_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:        jobName,
			Type:        JobTypeRecurring,
			Handler:     testJobHandler,
			Schedule:    "*/5 * * * *",
			Description: "Lifecycle test job",
			Payload:     map[string]interface{}{"step": 1},
			Labels:      map[string]string{"phase": "lifecycle", "env": "test"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		jobID := job.ID
		t.Logf("1. created job: id=%d", jobID)

		// 2. Get
		got, err := schedulerGetJob(ctx, jobID)
		if err != nil {
			t.Fatalf("get job failed: %v", err)
		}
		t.Logf("2. retrieved job: status=%s", got.Status)

		// 3. Update
		updated, err := schedulerUpdateJob(ctx, jobID, updateJobRequest{
			Description: "Updated lifecycle test job",
			Payload:     map[string]interface{}{"step": 2},
		})
		if err != nil {
			t.Fatalf("update job failed: %v", err)
		}
		t.Logf("3. updated job: description=%s", updated.Description)

		// 4. Pause
		paused, err := schedulerPauseJob(ctx, jobID)
		if err != nil {
			t.Fatalf("pause job failed: %v", err)
		}
		t.Logf("4. paused job: status=%s", paused.NewStatus)

		// 5. Resume
		resumed, err := schedulerResumeJob(ctx, jobID)
		if err != nil {
			t.Fatalf("resume job failed: %v", err)
		}
		t.Logf("5. resumed job: status=%s", resumed.NewStatus)

		// 6. Trigger
		triggered, err := schedulerTriggerJob(ctx, jobID)
		if err != nil {
			t.Fatalf("trigger job failed: %v", err)
		}
		t.Logf("6. triggered job: status=%s", triggered.Status)

		// 7. Get history
		history, err := schedulerGetJobHistory(ctx, jobID, 10)
		if err != nil {
			t.Fatalf("get history failed: %v", err)
		}
		t.Logf("7. job history: %d entries", len(history.Executions))

		// 8. Cancel
		canceled, err := schedulerCancelJob(ctx, jobID)
		if err != nil {
			t.Fatalf("cancel job failed: %v", err)
		}
		t.Logf("8. canceled job: status=%s", canceled.NewStatus)

		// 9. Delete
		err = schedulerDeleteJob(ctx, jobID)
		if err != nil {
			t.Fatalf("delete job failed: %v", err)
		}
		t.Logf("9. deleted job")

		// 10. Verify deleted
		_, err = schedulerGetJob(ctx, jobID)
		if err == nil {
			t.Fatal("expected error for deleted job")
		}
		t.Logf("10. verified job deleted")
	})
}

// TestSchedulerConcurrent tests concurrent scheduler operations.
func TestSchedulerConcurrent(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("concurrent job creation", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		const numGoroutines = 10
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)
		jobIDs := make(chan uint64, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				jobName := fmt.Sprintf("concurrent_test_%d_%d", time.Now().UnixNano(), idx)
				job, err := schedulerCreateJob(ctx, createJobRequest{
					Name:      jobName,
					Type:      JobTypeOneShot,
					Handler:   testJobHandler,
					ExecuteAt: time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
					Payload:   map[string]interface{}{"goroutine": idx},
				})
				if err != nil {
					errors <- &qberrors.WrappedMessageError{
						Message: "goroutine " + strconv.Itoa(idx),
						Cause:   err,
					}
					return
				}
				jobIDs <- job.ID
			}(i)
		}

		wg.Wait()
		close(errors)
		close(jobIDs)

		// Cleanup created jobs
		for id := range jobIDs {
			cleanupJob(ctx, id)
		}

		// Check for errors
		var errs []error
		for err := range errors {
			errs = append(errs, err)
		}

		if len(errs) > 0 {
			t.Errorf("concurrent job creation had %d errors: %v", len(errs), errs)
		}
	})

	t.Run("concurrent operations on same job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		// Create a job
		jobName := fmt.Sprintf("concurrent_ops_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:     jobName,
			Type:     JobTypeRecurring,
			Handler:  testJobHandler,
			Schedule: "*/5 * * * *",
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		// Run concurrent read operations
		const numReads = 5
		var wg sync.WaitGroup
		errors := make(chan error, numReads)

		for i := 0; i < numReads; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := schedulerGetJob(ctx, job.ID)
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		var errs []error
		for err := range errors {
			errs = append(errs, err)
		}

		if len(errs) > 0 {
			t.Errorf("concurrent reads had %d errors: %v", len(errs), errs)
		}
	})
}

// nonExistentJobID is a job ID that is extremely unlikely to exist on the
// server, used to exercise not-found error paths.
const nonExistentJobID uint64 = 999999999999

// Helper functions for scheduler API operations.

// marshalCreateRequest serializes a create request, encoding the payload
// map into the raw JSON `payload` field the server expects.
func marshalCreateRequest(req createJobRequest) ([]byte, error) {
	type alias createJobRequest
	envelope := struct {
		alias
		Payload json.RawMessage `json:"payload,omitempty"`
	}{alias: alias(req)}
	if req.Payload != nil {
		raw, err := json.Marshal(req.Payload)
		if err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to marshal payload", Cause: err}
		}
		envelope.Payload = raw
	}
	return json.Marshal(envelope)
}

// marshalUpdateRequest serializes an update request, encoding the payload
// map into the raw JSON `payload` field the server expects.
func marshalUpdateRequest(req updateJobRequest) ([]byte, error) {
	type alias updateJobRequest
	envelope := struct {
		alias
		Payload json.RawMessage `json:"payload,omitempty"`
	}{alias: alias(req)}
	if req.Payload != nil {
		raw, err := json.Marshal(req.Payload)
		if err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to marshal payload", Cause: err}
		}
		envelope.Payload = raw
	}
	return json.Marshal(envelope)
}

func schedulerCreateJob(ctx context.Context, req createJobRequest) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs", GetRESTAddress())

	body, err := marshalCreateRequest(req)
	if err != nil {
		return nil, err
	}

	respBody, err := doSchedulerRequest(ctx, http.MethodPost, reqURL, body,
		"create job", http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}

	return decodeJobEnvelope(respBody)
}

func schedulerGetJob(ctx context.Context, id uint64) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", GetRESTAddress(), id)

	respBody, err := doSchedulerRequest(ctx, http.MethodGet, reqURL, nil,
		"get job", http.StatusOK)
	if err != nil {
		return nil, err
	}

	return decodeJobEnvelope(respBody)
}

func schedulerUpdateJob(ctx context.Context, id uint64, req updateJobRequest) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", GetRESTAddress(), id)

	body, err := marshalUpdateRequest(req)
	if err != nil {
		return nil, err
	}

	respBody, err := doSchedulerRequest(ctx, http.MethodPut, reqURL, body,
		"update job", http.StatusOK)
	if err != nil {
		return nil, err
	}

	return decodeJobEnvelope(respBody)
}

func schedulerListJobs(ctx context.Context, limit int, cursor, status, handler string) (*listJobsResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs", GetRESTAddress())

	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if status != "" {
		params.Set("status", status)
	}
	if handler != "" {
		params.Set("handler", handler)
	}

	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	respBody, err := doSchedulerRequest(ctx, http.MethodGet, reqURL, nil,
		"list jobs", http.StatusOK)
	if err != nil {
		return nil, err
	}

	var listResp listJobsResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &listResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}
	for i := range listResp.Jobs {
		if err := listResp.Jobs[i].decodePayload(); err != nil {
			return nil, err
		}
	}

	return &listResp, nil
}

func schedulerTriggerJob(ctx context.Context, id uint64) (*statusChangeResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/trigger", GetRESTAddress(), id)
	return schedulerStatusChange(ctx, reqURL, "trigger job")
}

func schedulerPauseJob(ctx context.Context, id uint64) (*statusChangeResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/pause", GetRESTAddress(), id)
	return schedulerStatusChange(ctx, reqURL, "pause job")
}

func schedulerResumeJob(ctx context.Context, id uint64) (*statusChangeResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/resume", GetRESTAddress(), id)
	return schedulerStatusChange(ctx, reqURL, "resume job")
}

func schedulerCancelJob(ctx context.Context, id uint64) (*statusChangeResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/cancel", GetRESTAddress(), id)
	return schedulerStatusChange(ctx, reqURL, "cancel job")
}

// schedulerStatusChange performs a POST status-transition request
// (trigger/pause/resume/cancel) and decodes the status-change response.
func schedulerStatusChange(ctx context.Context, reqURL, op string) (*statusChangeResponse, error) {
	respBody, err := doSchedulerRequest(ctx, http.MethodPost, reqURL, nil, op, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var resp statusChangeResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}
	return &resp, nil
}

func schedulerDeleteJob(ctx context.Context, id uint64) error {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", GetRESTAddress(), id)

	_, err := doSchedulerRequest(ctx, http.MethodDelete, reqURL, nil,
		"delete job", http.StatusOK, http.StatusNoContent)
	return err
}

func schedulerGetJobHistory(ctx context.Context, id uint64, limit int) (*jobHistoryResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/history", GetRESTAddress(), id)

	if limit > 0 {
		reqURL += "?limit=" + strconv.Itoa(limit)
	}

	respBody, err := doSchedulerRequest(ctx, http.MethodGet, reqURL, nil,
		"get job history", http.StatusOK)
	if err != nil {
		return nil, err
	}

	var historyResp jobHistoryResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &historyResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &historyResp, nil
}

func schedulerGetStatus(ctx context.Context) (*schedulerStatusResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/status", GetRESTAddress())

	respBody, err := doSchedulerRequest(ctx, http.MethodGet, reqURL, nil,
		"get scheduler status", http.StatusOK)
	if err != nil {
		return nil, err
	}

	var statusResp schedulerStatusResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &statusResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &statusResp, nil
}

// doSchedulerRequest issues an HTTP request to the scheduler REST API and
// returns the response body, mapping non-success statuses to typed errors.
// A 404 always maps to a "job not found" error regardless of okStatuses.
func doSchedulerRequest(ctx context.Context, method, reqURL string, body []byte, op string, okStatuses ...int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &qberrors.MessageError{Message: "job not found"}
	}

	for _, ok := range okStatuses {
		if resp.StatusCode == ok {
			return respBody, nil
		}
	}

	return nil, &qberrors.MessageError{
		Message: op + " failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
	}
}

// decodeJobEnvelope unwraps the {"job": {...}} envelope returned by the
// create/get/update endpoints and decodes the job payload.
func decodeJobEnvelope(respBody []byte) (*jobResponse, error) {
	var env jobEnvelope
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &env); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}
	if env.Job == nil {
		return nil, &qberrors.MessageError{Message: "response did not contain a job"}
	}
	if err := env.Job.decodePayload(); err != nil {
		return nil, err
	}
	return env.Job, nil
}

// cleanupJob attempts to delete a job, ignoring any error.
func cleanupJob(ctx context.Context, id uint64) {
	if id == 0 {
		return
	}
	// Best effort cleanup - ignore errors
	_ = schedulerDeleteJob(ctx, id)
}
