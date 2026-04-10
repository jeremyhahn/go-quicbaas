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

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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

// createJobRequest represents a request to create a scheduler job.
type createJobRequest struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Schedule    string                 `json:"schedule,omitempty"`
	Delay       string                 `json:"delay,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	OwnerType   string                 `json:"owner_type,omitempty"`
	OwnerID     string                 `json:"owner_id,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
	Timeout     string                 `json:"timeout,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// updateJobRequest represents a request to update a scheduler job.
type updateJobRequest struct {
	Name        string                 `json:"name,omitempty"`
	Schedule    string                 `json:"schedule,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
	Timeout     string                 `json:"timeout,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// jobResponse represents a job response from the API.
type jobResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Schedule    string                 `json:"schedule,omitempty"`
	Delay       string                 `json:"delay,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	OwnerType   string                 `json:"owner_type,omitempty"`
	OwnerID     string                 `json:"owner_id,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
	Timeout     string                 `json:"timeout,omitempty"`
	Description string                 `json:"description,omitempty"`
	NextRun     time.Time              `json:"next_run,omitempty"`
	LastRun     time.Time              `json:"last_run,omitempty"`
	RunCount    int64                  `json:"run_count,omitempty"`
	CreatedAt   time.Time              `json:"created_at,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at,omitempty"`
}

// listJobsResponse represents a list jobs response.
type listJobsResponse struct {
	Jobs       []jobResponse `json:"jobs"`
	Total      int           `json:"total,omitempty"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// jobHistoryEntry represents a single execution history entry.
type jobHistoryEntry struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Duration   string    `json:"duration,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// jobHistoryResponse represents a job history response.
type jobHistoryResponse struct {
	History []jobHistoryEntry `json:"history"`
	Total   int               `json:"total,omitempty"`
}

// schedulerStatusResponse represents the scheduler status response.
type schedulerStatusResponse struct {
	Running       bool      `json:"running"`
	TotalJobs     int64     `json:"total_jobs"`
	ActiveJobs    int64     `json:"active_jobs"`
	PausedJobs    int64     `json:"paused_jobs"`
	CompletedJobs int64     `json:"completed_jobs"`
	FailedJobs    int64     `json:"failed_jobs"`
	Uptime        string    `json:"uptime,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
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
			OwnerType:   OwnerTypeSystem,
			Description: "Test one-shot job",
			Payload:     map[string]interface{}{"task": "run_once", "priority": 1},
		}

		job, err := schedulerCreateJob(ctx, req)
		if err != nil {
			t.Fatalf("create one-shot job failed: %v", err)
		}
		defer cleanupJob(ctx, job.ID)

		if job.ID == "" {
			t.Error("expected job ID to be set")
		}
		if job.Name != jobName {
			t.Errorf("expected name %q, got %q", jobName, job.Name)
		}
		if job.Type != JobTypeOneShot {
			t.Errorf("expected type %q, got %q", JobTypeOneShot, job.Type)
		}

		t.Logf("created one-shot job: id=%s, name=%s, status=%s", job.ID, job.Name, job.Status)
	})

	t.Run("create recurring job with cron schedule", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("recurring_test_%d", time.Now().UnixNano())
		req := createJobRequest{
			Name:        jobName,
			Type:        JobTypeRecurring,
			Schedule:    "*/5 * * * *", // Every 5 minutes
			OwnerType:   OwnerTypeTenant,
			OwnerID:     "tenant-123",
			Description: "Test recurring job",
			Payload:     map[string]interface{}{"task": "run_periodic"},
			Tags:        []string{"test", "recurring"},
			MaxRetries:  3,
			Timeout:     "5m",
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

		t.Logf("created recurring job: id=%s, schedule=%s", job.ID, job.Schedule)
	})

	t.Run("create delayed job", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("delayed_test_%d", time.Now().UnixNano())
		req := createJobRequest{
			Name:        jobName,
			Type:        JobTypeDelayed,
			Delay:       "1h",
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

		t.Logf("created delayed job: id=%s, delay=%s", job.ID, job.Delay)
	})

	t.Run("create job with invalid type returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Name: fmt.Sprintf("invalid_test_%d", time.Now().UnixNano()),
			Type: "invalid-type",
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
			Type: JobTypeOneShot,
		}

		_, err := schedulerCreateJob(ctx, req)
		if err == nil {
			t.Error("expected error for missing job name")
		}
		t.Logf("create with missing name returned error (expected): %v", err)
	})

	t.Run("create recurring job without schedule returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req := createJobRequest{
			Name: fmt.Sprintf("no_schedule_test_%d", time.Now().UnixNano()),
			Type: JobTypeRecurring,
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
			Name:    jobName,
			Type:    JobTypeOneShot,
			Payload: map[string]interface{}{"test": true},
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
			t.Errorf("expected ID %q, got %q", created.ID, job.ID)
		}
		if job.Name != jobName {
			t.Errorf("expected name %q, got %q", jobName, job.Name)
		}

		t.Logf("retrieved job: id=%s, name=%s, status=%s", job.ID, job.Name, job.Status)
	})

	t.Run("get non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerGetJob(ctx, "non-existent-job-id-12345")
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

		t.Logf("updated job: id=%s, new_name=%s", updated.ID, updated.Name)
	})

	t.Run("update job payload", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		jobName := fmt.Sprintf("payload_update_test_%d", time.Now().UnixNano())
		created, err := schedulerCreateJob(ctx, createJobRequest{
			Name:    jobName,
			Type:    JobTypeOneShot,
			Payload: map[string]interface{}{"version": 1},
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

		_, err := schedulerUpdateJob(ctx, "non-existent-job-id-12345", updateJobRequest{
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
		var createdIDs []string
		for i := 0; i < 3; i++ {
			jobName := fmt.Sprintf("list_test_%d_%d", time.Now().UnixNano(), i)
			job, err := schedulerCreateJob(ctx, createJobRequest{
				Name:    jobName,
				Type:    JobTypeOneShot,
				Payload: map[string]interface{}{"index": i},
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

	t.Run("list jobs by type", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		resp, err := schedulerListJobs(ctx, 0, "", "", JobTypeOneShot)
		if err != nil {
			t.Fatalf("list jobs by type failed: %v", err)
		}

		for _, job := range resp.Jobs {
			if job.Type != JobTypeOneShot {
				t.Errorf("expected type %q, got %q", JobTypeOneShot, job.Type)
			}
		}

		t.Logf("listed %d one-shot jobs", len(resp.Jobs))
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

		t.Logf("triggered job: id=%s, status=%s", triggered.ID, triggered.Status)
	})

	t.Run("trigger non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerTriggerJob(ctx, "non-existent-job-id-12345")
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

		if paused.Status != JobStatusPaused {
			t.Errorf("expected status %q after pause, got %q", JobStatusPaused, paused.Status)
		}
		t.Logf("paused job: id=%s, status=%s", paused.ID, paused.Status)

		// Resume the job
		resumed, err := schedulerResumeJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("resume job failed: %v", err)
		}

		if resumed.Status == JobStatusPaused {
			t.Error("expected status to change after resume")
		}
		t.Logf("resumed job: id=%s, status=%s", resumed.ID, resumed.Status)
	})

	t.Run("pause non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerPauseJob(ctx, "non-existent-job-id-12345")
		if err == nil {
			t.Error("expected error for non-existent job")
		}
		t.Logf("pause non-existent job returned error (expected): %v", err)
	})

	t.Run("resume non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerResumeJob(ctx, "non-existent-job-id-12345")
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
			Delay:   "24h",
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

		if canceled.Status != JobStatusCanceled {
			t.Errorf("expected status %q after cancel, got %q", JobStatusCanceled, canceled.Status)
		}
		t.Logf("canceled job: id=%s, status=%s", canceled.ID, canceled.Status)
	})

	t.Run("cancel non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerCancelJob(ctx, "non-existent-job-id-12345")
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
			Name:    jobName,
			Type:    JobTypeOneShot,
			Payload: map[string]interface{}{"task": "deletable"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}

		// Delete the job
		err = schedulerDeleteJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("delete job failed: %v", err)
		}
		t.Logf("deleted job: id=%s", job.ID)

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
		err := schedulerDeleteJob(ctx, "non-existent-job-id-12345")
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

		// Create and trigger a job to generate history
		jobName := fmt.Sprintf("history_test_%d", time.Now().UnixNano())
		job, err := schedulerCreateJob(ctx, createJobRequest{
			Name:    jobName,
			Type:    JobTypeOneShot,
			Payload: map[string]interface{}{"task": "history_test"},
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

		t.Logf("job history: %d entries", len(history.History))
		for i, entry := range history.History {
			t.Logf("  entry %d: id=%s, status=%s, started=%s", i, entry.ID, entry.Status, entry.StartedAt)
		}
	})

	t.Run("get history for non-existent job returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		_, err := schedulerGetJobHistory(ctx, "non-existent-job-id-12345", 10)
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

		t.Logf("scheduler status: running=%v, total_jobs=%d, active=%d, paused=%d, completed=%d, failed=%d",
			status.Running, status.TotalJobs, status.ActiveJobs, status.PausedJobs,
			status.CompletedJobs, status.FailedJobs)
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
			Schedule:    "*/5 * * * *",
			Description: "Lifecycle test job",
			Payload:     map[string]interface{}{"step": 1},
			Tags:        []string{"lifecycle", "test"},
		})
		if err != nil {
			t.Fatalf("create job failed: %v", err)
		}
		t.Logf("1. created job: id=%s", job.ID)

		// 2. Get
		job, err = schedulerGetJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("get job failed: %v", err)
		}
		t.Logf("2. retrieved job: status=%s", job.Status)

		// 3. Update
		job, err = schedulerUpdateJob(ctx, job.ID, updateJobRequest{
			Description: "Updated lifecycle test job",
			Payload:     map[string]interface{}{"step": 2},
		})
		if err != nil {
			t.Fatalf("update job failed: %v", err)
		}
		t.Logf("3. updated job: description=%s", job.Description)

		// 4. Pause
		job, err = schedulerPauseJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("pause job failed: %v", err)
		}
		t.Logf("4. paused job: status=%s", job.Status)

		// 5. Resume
		job, err = schedulerResumeJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("resume job failed: %v", err)
		}
		t.Logf("5. resumed job: status=%s", job.Status)

		// 6. Trigger
		job, err = schedulerTriggerJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("trigger job failed: %v", err)
		}
		t.Logf("6. triggered job: status=%s", job.Status)

		// 7. Get history
		history, err := schedulerGetJobHistory(ctx, job.ID, 10)
		if err != nil {
			t.Fatalf("get history failed: %v", err)
		}
		t.Logf("7. job history: %d entries", len(history.History))

		// 8. Cancel
		job, err = schedulerCancelJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("cancel job failed: %v", err)
		}
		t.Logf("8. canceled job: status=%s", job.Status)

		// 9. Delete
		err = schedulerDeleteJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("delete job failed: %v", err)
		}
		t.Logf("9. deleted job")

		// 10. Verify deleted
		_, err = schedulerGetJob(ctx, job.ID)
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
		jobIDs := make(chan string, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				jobName := fmt.Sprintf("concurrent_test_%d_%d", time.Now().UnixNano(), idx)
				job, err := schedulerCreateJob(ctx, createJobRequest{
					Name:    jobName,
					Type:    JobTypeOneShot,
					Payload: map[string]interface{}{"goroutine": idx},
				})
				if err != nil {
					errors <- &dragonerrors.WrappedMessageError{
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

// Helper functions for scheduler API operations.

func schedulerCreateJob(ctx context.Context, req createJobRequest) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs", GetRESTAddress())

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &dragonerrors.MessageError{
			Message: "create job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerGetJob(ctx context.Context, id string) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "get job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerUpdateJob(ctx context.Context, id string, req updateJobRequest) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s", GetRESTAddress(), id)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "update job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerListJobs(ctx context.Context, limit int, cursor, status, jobType string) (*listJobsResponse, error) {
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
	if jobType != "" {
		params.Set("type", jobType)
	}

	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "list jobs failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var listResp listJobsResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &listResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &listResp, nil
}

func schedulerTriggerJob(ctx context.Context, id string) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s/trigger", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "trigger job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerPauseJob(ctx context.Context, id string) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s/pause", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "pause job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerResumeJob(ctx context.Context, id string) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s/resume", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "resume job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerCancelJob(ctx context.Context, id string) (*jobResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s/cancel", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "cancel job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var jobResp jobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &jobResp, nil
}

func schedulerDeleteJob(ctx context.Context, id string) error {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s", GetRESTAddress(), id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return &dragonerrors.MessageError{
			Message: "delete job failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	return nil
}

func schedulerGetJobHistory(ctx context.Context, id string, limit int) (*jobHistoryResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/jobs/%s/history", GetRESTAddress(), id)

	if limit > 0 {
		reqURL += "?limit=" + strconv.Itoa(limit)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &dragonerrors.MessageError{Message: "job not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "get job history failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var historyResp jobHistoryResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &historyResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &historyResp, nil
}

func schedulerGetStatus(ctx context.Context) (*schedulerStatusResponse, error) {
	reqURL := fmt.Sprintf("%s/api/v1/scheduler/status", GetRESTAddress())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &dragonerrors.MessageError{
			Message: "get scheduler status failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var statusResp schedulerStatusResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &statusResp); err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &statusResp, nil
}

// cleanupJob attempts to delete a job, logging any error but not failing the test.
func cleanupJob(ctx context.Context, id string) {
	if id == "" {
		return
	}
	// Best effort cleanup - ignore errors
	_ = schedulerDeleteJob(ctx, id)
}
