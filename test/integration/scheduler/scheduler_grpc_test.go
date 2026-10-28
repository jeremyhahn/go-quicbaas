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

package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// gRPC Integration Tests for Scheduler Service
// =============================================================================

// TestSchedulerGRPCFullJobLifecycle tests the complete job lifecycle using gRPC.
func TestSchedulerGRPCFullJobLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := setupGRPCClient(t, ctx)
	defer client.Close()

	skipIfGRPCSchedulerUnavailable(t, client, ctx)

	prefix := fmt.Sprintf("grpc-lifecycle-%d", time.Now().UnixNano())

	t.Run("CreateOneShotJob", func(t *testing.T) {
		testGRPCCreateOneShotJob(t, ctx, client, prefix)
	})

	t.Run("CreateRecurringJob", func(t *testing.T) {
		testGRPCCreateRecurringJob(t, ctx, client, prefix)
	})

	t.Run("CreateDelayedJob", func(t *testing.T) {
		testGRPCCreateDelayedJob(t, ctx, client, prefix)
	})

	t.Run("GetJob", func(t *testing.T) {
		testGRPCGetJob(t, ctx, client, prefix)
	})

	t.Run("ListJobsWithPagination", func(t *testing.T) {
		testGRPCListJobsWithPagination(t, ctx, client, prefix)
	})

	t.Run("UpdateJob", func(t *testing.T) {
		testGRPCUpdateJob(t, ctx, client, prefix)
	})

	t.Run("TriggerJob", func(t *testing.T) {
		testGRPCTriggerJob(t, ctx, client, prefix)
	})

	t.Run("PauseJob", func(t *testing.T) {
		testGRPCPauseJob(t, ctx, client, prefix)
	})

	t.Run("ResumeJob", func(t *testing.T) {
		testGRPCResumeJob(t, ctx, client, prefix)
	})

	t.Run("CancelJob", func(t *testing.T) {
		testGRPCCancelJob(t, ctx, client, prefix)
	})

	t.Run("GetJobHistory", func(t *testing.T) {
		testGRPCGetJobHistory(t, ctx, client, prefix)
	})

	t.Run("GetSchedulerStatus", func(t *testing.T) {
		testGRPCGetSchedulerStatus(t, ctx, client)
	})

	t.Run("DeleteJob", func(t *testing.T) {
		testGRPCDeleteJob(t, ctx, client, prefix)
	})
}

// TestSchedulerGRPCErrorCases tests error handling for scheduler operations.
func TestSchedulerGRPCErrorCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := setupGRPCClient(t, ctx)
	defer client.Close()

	skipIfGRPCSchedulerUnavailable(t, client, ctx)

	t.Run("InvalidJobParameters", func(t *testing.T) {
		testGRPCInvalidJobParameters(t, ctx, client)
	})

	t.Run("NonExistentJobOperations", func(t *testing.T) {
		testGRPCNonExistentJobOperations(t, ctx, client)
	})

	t.Run("ConcurrentJobOperations", func(t *testing.T) {
		testGRPCConcurrentJobOperations(t, ctx, client)
	})
}

// TestSchedulerGRPCJobTypes tests different job types and configurations.
func TestSchedulerGRPCJobTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := setupGRPCClient(t, ctx)
	defer client.Close()

	skipIfGRPCSchedulerUnavailable(t, client, ctx)

	prefix := fmt.Sprintf("grpc-types-%d", time.Now().UnixNano())

	t.Run("OneShotWithPayload", func(t *testing.T) {
		testGRPCOneShotWithPayload(t, ctx, client, prefix)
	})

	t.Run("RecurringWithCronSchedule", func(t *testing.T) {
		testGRPCRecurringWithCronSchedule(t, ctx, client, prefix)
	})

	t.Run("JobWithLabels", func(t *testing.T) {
		testGRPCJobWithLabels(t, ctx, client, prefix)
	})

	t.Run("JobWithPriorityAndTimeout", func(t *testing.T) {
		testGRPCJobWithPriorityAndTimeout(t, ctx, client, prefix)
	})

	t.Run("JobWithMaxRetries", func(t *testing.T) {
		testGRPCJobWithMaxRetries(t, ctx, client, prefix)
	})
}

// =============================================================================
// Setup Helpers
// =============================================================================

func setupGRPCClient(t *testing.T, ctx context.Context) *grpcclient.Client {
	t.Helper()

	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), helpers.TenantGRPCClientOption(t))
	require.NoError(t, err, "Failed to create gRPC client for endpoint %s", endpoints[0])

	return client
}

func skipIfGRPCSchedulerUnavailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()

	_, err := client.SchedulerGetStatus(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "Unimplemented") ||
			strings.Contains(err.Error(), "unknown service") ||
			strings.Contains(err.Error(), "not available") {
			require.Fail(t, "Scheduler service must be available",
				"gRPC scheduler service is not enabled on the server")
		}
	}
}

func cleanupGRPCJob(t *testing.T, client *grpcclient.Client, ctx context.Context, jobID uint64) {
	t.Helper()

	req := &service.DeleteSchedulerJobRequest{
		JobID: jobID,
		Force: true,
	}
	_, _ = client.SchedulerDeleteJob(ctx, req)
}

// =============================================================================
// One-Shot Job Tests
// =============================================================================

func testGRPCCreateOneShotJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	jobName := generateUniqueJobName(prefix + "-oneshot")
	executeAt := time.Now().Add(1 * time.Hour)

	req := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Test one-shot job via gRPC",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "test-handler-oneshot",
		ExecuteAt:   executeAt,
		Priority:    10,
		Timeout:     5 * time.Minute,
		MaxRetries:  3,
		Labels: map[string]string{
			"test":     "grpc-integration",
			"jobtype":  "oneshot",
			"protocol": "grpc",
		},
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	require.NoError(t, err, "CreateJob should succeed for one-shot job")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)

	defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

	assert.NotZero(t, resp.Job.ID)
	assert.Equal(t, jobName, resp.Job.Name)
	assert.Equal(t, "Test one-shot job via gRPC", resp.Job.Description)
	assert.Equal(t, service.SchedulerJobTypeOneShot, resp.Job.Type)
	assert.Equal(t, "test-handler-oneshot", resp.Job.Handler)
	assert.Equal(t, service.SchedulerJobStatusPending, resp.Job.Status)
	assert.Equal(t, int32(10), resp.Job.Priority)
	assert.Equal(t, int32(3), resp.Job.MaxRetries)
	assert.NotNil(t, resp.Job.Labels)
	assert.Equal(t, "grpc-integration", resp.Job.Labels["test"])
	assert.False(t, resp.Timestamp.IsZero())

	t.Logf("[gRPC] Created one-shot job: ID=%d, Name=%s, ExecuteAt=%v",
		resp.Job.ID, resp.Job.Name, resp.Job.ExecuteAt)
}

func testGRPCCreateRecurringJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	jobName := generateUniqueJobName(prefix + "-recurring")

	req := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Test recurring job via gRPC",
		Type:        service.SchedulerJobTypeRecurring,
		Handler:     "test-handler-recurring",
		Schedule:    "*/5 * * * *", // Every 5 minutes
		Priority:    5,
		Timeout:     2 * time.Minute,
		MaxRetries:  5,
		Labels: map[string]string{
			"test":     "grpc-integration",
			"jobtype":  "recurring",
			"protocol": "grpc",
		},
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	require.NoError(t, err, "CreateJob should succeed for recurring job")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)

	defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

	assert.NotZero(t, resp.Job.ID)
	assert.Equal(t, jobName, resp.Job.Name)
	assert.Equal(t, service.SchedulerJobTypeRecurring, resp.Job.Type)
	assert.Equal(t, "test-handler-recurring", resp.Job.Handler)
	assert.Equal(t, "*/5 * * * *", resp.Job.Schedule)
	assert.Equal(t, service.SchedulerJobStatusPending, resp.Job.Status)

	t.Logf("[gRPC] Created recurring job: ID=%d, Name=%s, Schedule=%s",
		resp.Job.ID, resp.Job.Name, resp.Job.Schedule)
}

func testGRPCCreateDelayedJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	jobName := generateUniqueJobName(prefix + "-delayed")

	req := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Test delayed job via gRPC",
		Type:        service.SchedulerJobTypeDelayed,
		Handler:     "test-handler-delayed",
		Delay:       30 * time.Minute,
		Priority:    8,
		Timeout:     10 * time.Minute,
		MaxRetries:  2,
		Labels: map[string]string{
			"test":     "grpc-integration",
			"jobtype":  "delayed",
			"protocol": "grpc",
		},
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	require.NoError(t, err, "CreateJob should succeed for delayed job")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)

	defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

	assert.NotZero(t, resp.Job.ID)
	assert.Equal(t, jobName, resp.Job.Name)
	assert.Equal(t, service.SchedulerJobTypeDelayed, resp.Job.Type)
	assert.Equal(t, "test-handler-delayed", resp.Job.Handler)
	assert.Equal(t, service.SchedulerJobStatusPending, resp.Job.Status)

	t.Logf("[gRPC] Created delayed job: ID=%d, Name=%s, Delay=%v",
		resp.Job.ID, resp.Job.Name, resp.Job.Delay)
}

// =============================================================================
// GetJob Tests
// =============================================================================

func testGRPCGetJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create a job first
	jobName := generateUniqueJobName(prefix + "-get")
	createReq := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Test job for GetJob",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "test-handler-get",
		ExecuteAt:   time.Now().Add(2 * time.Hour),
		Priority:    7,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Get the job
	getReq := &service.GetSchedulerJobRequest{JobID: jobID}
	getResp, err := client.SchedulerGetJob(ctx, getReq)
	require.NoError(t, err, "GetJob should succeed")
	require.NotNil(t, getResp)
	require.NotNil(t, getResp.Job)

	assert.Equal(t, jobID, getResp.Job.ID)
	assert.Equal(t, jobName, getResp.Job.Name)
	assert.Equal(t, "Test job for GetJob", getResp.Job.Description)
	assert.Equal(t, service.SchedulerJobTypeOneShot, getResp.Job.Type)
	assert.Equal(t, "test-handler-get", getResp.Job.Handler)
	assert.Equal(t, int32(7), getResp.Job.Priority)
	assert.False(t, getResp.Timestamp.IsZero())

	t.Logf("[gRPC] Retrieved job: ID=%d, Name=%s", getResp.Job.ID, getResp.Job.Name)
}

// =============================================================================
// ListJobs Tests
// =============================================================================

func testGRPCListJobsWithPagination(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create multiple jobs for pagination testing
	numJobs := 7
	jobIDs := make([]uint64, 0, numJobs)

	for i := 0; i < numJobs; i++ {
		req := &service.CreateSchedulerJobRequest{
			Name:      fmt.Sprintf("%s-list-job-%d", prefix, i),
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "test-handler-list",
			ExecuteAt: time.Now().Add(time.Duration(i+1) * time.Hour),
			Labels: map[string]string{
				"batch":    prefix + "-list",
				"sequence": fmt.Sprintf("%d", i),
			},
		}
		resp, err := client.SchedulerCreateJob(ctx, req)
		require.NoError(t, err)
		jobIDs = append(jobIDs, resp.Job.ID)
	}

	defer func() {
		for _, jobID := range jobIDs {
			cleanupGRPCJob(t, client, ctx, jobID)
		}
	}()

	// Test listing all jobs
	t.Run("ListAll", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{Limit: 100}
		resp, err := client.SchedulerListJobs(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Jobs)
		t.Logf("[gRPC] Listed %d total jobs", len(resp.Jobs))
	})

	// Test pagination with small limit
	t.Run("Pagination", func(t *testing.T) {
		pageSize := 3
		allJobs := make([]*service.SchedulerJob, 0)
		cursor := ""

		for pageNum := 0; pageNum < 5; pageNum++ { // Max 5 pages to avoid infinite loop
			req := &service.ListSchedulerJobsRequest{
				Limit:  pageSize,
				Cursor: cursor,
			}
			resp, err := client.SchedulerListJobs(ctx, req)
			require.NoError(t, err)
			assert.NotNil(t, resp)

			allJobs = append(allJobs, resp.Jobs...)

			if !resp.HasMore || resp.NextCursor == "" {
				break
			}
			cursor = resp.NextCursor
		}

		t.Logf("[gRPC] Paginated through %d jobs total", len(allJobs))
	})

	// Test filtering by status
	t.Run("FilterByStatus", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Status: service.SchedulerJobStatusPending,
			Limit:  100,
		}
		resp, err := client.SchedulerListJobs(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)

		for _, job := range resp.Jobs {
			assert.Equal(t, service.SchedulerJobStatusPending, job.Status)
		}
		t.Logf("[gRPC] Listed %d pending jobs", len(resp.Jobs))
	})

	// Test filtering by handler
	t.Run("FilterByHandler", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Handler: "test-handler-list",
			Limit:   100,
		}
		resp, err := client.SchedulerListJobs(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)

		for _, job := range resp.Jobs {
			assert.Equal(t, "test-handler-list", job.Handler)
		}
		t.Logf("[gRPC] Listed %d jobs with handler 'test-handler-list'", len(resp.Jobs))
	})

	// Test filtering by labels
	t.Run("FilterByLabels", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Labels: map[string]string{"batch": prefix + "-list"},
			Limit:  100,
		}
		resp, err := client.SchedulerListJobs(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		t.Logf("[gRPC] Listed %d jobs with batch label", len(resp.Jobs))
	})
}

// =============================================================================
// UpdateJob Tests
// =============================================================================

func testGRPCUpdateJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create a job first
	jobName := generateUniqueJobName(prefix + "-update")
	createReq := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Original description",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "test-handler-update",
		ExecuteAt:   time.Now().Add(3 * time.Hour),
		Priority:    5,
		Timeout:     10 * time.Minute,
		MaxRetries:  2,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Update the job
	updateReq := &service.UpdateSchedulerJobRequest{
		JobID:       jobID,
		Name:        jobName + "-updated",
		Description: "Updated description via gRPC",
		Priority:    15,
		Timeout:     20 * time.Minute,
		MaxRetries:  5,
		Labels: map[string]string{
			"updated": "true",
		},
	}

	updateResp, err := client.SchedulerUpdateJob(ctx, updateReq)
	require.NoError(t, err, "UpdateJob should succeed")
	require.NotNil(t, updateResp)
	require.NotNil(t, updateResp.Job)

	assert.Equal(t, jobID, updateResp.Job.ID)
	assert.Equal(t, jobName+"-updated", updateResp.Job.Name)
	assert.Equal(t, "Updated description via gRPC", updateResp.Job.Description)
	assert.Equal(t, int32(15), updateResp.Job.Priority)
	assert.Equal(t, int32(5), updateResp.Job.MaxRetries)
	assert.NotNil(t, updateResp.Job.Labels)
	assert.Equal(t, "true", updateResp.Job.Labels["updated"])

	// Verify update persisted
	getResp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, "Updated description via gRPC", getResp.Job.Description)
	assert.Equal(t, int32(15), getResp.Job.Priority)

	t.Logf("[gRPC] Updated job: ID=%d, Name=%s", updateResp.Job.ID, updateResp.Job.Name)
}

// =============================================================================
// TriggerJob Tests
// =============================================================================

func testGRPCTriggerJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create a job first
	jobName := generateUniqueJobName(prefix + "-trigger")
	createReq := &service.CreateSchedulerJobRequest{
		Name:      jobName,
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "test-handler-trigger",
		ExecuteAt: time.Now().Add(4 * time.Hour),
		Priority:  10,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Trigger the job
	triggerReq := &service.TriggerSchedulerJobRequest{JobID: jobID}
	triggerResp, err := client.SchedulerTriggerJob(ctx, triggerReq)
	require.NoError(t, err, "TriggerJob should succeed")
	require.NotNil(t, triggerResp)

	assert.Equal(t, jobID, triggerResp.JobID)
	assert.NotEmpty(t, triggerResp.Status)
	assert.False(t, triggerResp.Timestamp.IsZero())

	t.Logf("[gRPC] Triggered job: ID=%d, Status=%s", triggerResp.JobID, triggerResp.Status)
}

// =============================================================================
// PauseJob Tests
// =============================================================================

func testGRPCPauseJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create a recurring job that can be paused
	jobName := generateUniqueJobName(prefix + "-pause")
	createReq := &service.CreateSchedulerJobRequest{
		Name:     jobName,
		Type:     service.SchedulerJobTypeRecurring,
		Handler:  "test-handler-pause",
		Schedule: "0 * * * *", // Every hour
		Priority: 5,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Pause the job
	pauseReq := &service.PauseSchedulerJobRequest{JobID: jobID}
	pauseResp, err := client.SchedulerPauseJob(ctx, pauseReq)
	require.NoError(t, err, "PauseJob should succeed")
	require.NotNil(t, pauseResp)

	assert.Equal(t, jobID, pauseResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusPaused, pauseResp.NewStatus)
	assert.False(t, pauseResp.Timestamp.IsZero())

	// Verify the job is paused
	getResp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, service.SchedulerJobStatusPaused, getResp.Job.Status)

	t.Logf("[gRPC] Paused job: ID=%d, OldStatus=%s, NewStatus=%s",
		pauseResp.JobID, pauseResp.OldStatus, pauseResp.NewStatus)
}

// =============================================================================
// ResumeJob Tests
// =============================================================================

func testGRPCResumeJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create and pause a job
	jobName := generateUniqueJobName(prefix + "-resume")
	createReq := &service.CreateSchedulerJobRequest{
		Name:     jobName,
		Type:     service.SchedulerJobTypeRecurring,
		Handler:  "test-handler-resume",
		Schedule: "30 * * * *", // Every hour at :30
		Priority: 5,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Pause first
	_, err = client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)

	// Resume the job
	resumeReq := &service.ResumeSchedulerJobRequest{JobID: jobID}
	resumeResp, err := client.SchedulerResumeJob(ctx, resumeReq)
	require.NoError(t, err, "ResumeJob should succeed")
	require.NotNil(t, resumeResp)

	assert.Equal(t, jobID, resumeResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusPaused, resumeResp.OldStatus)
	assert.Equal(t, service.SchedulerJobStatusPending, resumeResp.NewStatus)
	assert.False(t, resumeResp.Timestamp.IsZero())

	// Verify the job is resumed
	getResp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, service.SchedulerJobStatusPending, getResp.Job.Status)

	t.Logf("[gRPC] Resumed job: ID=%d, OldStatus=%s, NewStatus=%s",
		resumeResp.JobID, resumeResp.OldStatus, resumeResp.NewStatus)
}

// =============================================================================
// CancelJob Tests
// =============================================================================

func testGRPCCancelJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create a job
	jobName := generateUniqueJobName(prefix + "-cancel")
	createReq := &service.CreateSchedulerJobRequest{
		Name:      jobName,
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "test-handler-cancel",
		ExecuteAt: time.Now().Add(5 * time.Hour),
		Priority:  5,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Cancel the job
	cancelReq := &service.CancelSchedulerJobRequest{
		JobID:  jobID,
		Reason: "Canceled by gRPC integration test",
	}
	cancelResp, err := client.SchedulerCancelJob(ctx, cancelReq)
	require.NoError(t, err, "CancelJob should succeed")
	require.NotNil(t, cancelResp)

	assert.Equal(t, jobID, cancelResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusCanceled, cancelResp.NewStatus)
	assert.False(t, cancelResp.Timestamp.IsZero())

	// Verify the job is canceled
	getResp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, service.SchedulerJobStatusCanceled, getResp.Job.Status)

	t.Logf("[gRPC] Canceled job: ID=%d, OldStatus=%s, NewStatus=%s",
		cancelResp.JobID, cancelResp.OldStatus, cancelResp.NewStatus)
}

// =============================================================================
// GetJobHistory Tests
// =============================================================================

func testGRPCGetJobHistory(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	// Create and trigger a job to generate history
	jobName := generateUniqueJobName(prefix + "-history")
	createReq := &service.CreateSchedulerJobRequest{
		Name:      jobName,
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "test-handler-history",
		ExecuteAt: time.Now().Add(1 * time.Second),
		Priority:  10,
		Timeout:   1 * time.Minute,
	}

	createResp, err := client.SchedulerCreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp.Job)

	jobID := createResp.Job.ID
	defer cleanupGRPCJob(t, client, ctx, jobID)

	// Trigger the job
	_, err = client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{JobID: jobID})
	if err != nil {
		t.Logf("[gRPC] TriggerJob returned error (may be expected): %v", err)
	}

	// Get job history
	historyReq := &service.GetSchedulerJobHistoryRequest{
		JobID: jobID,
		Limit: 10,
	}
	historyResp, err := client.SchedulerGetJobHistory(ctx, historyReq)
	require.NoError(t, err, "GetJobHistory should succeed")
	require.NotNil(t, historyResp)

	t.Logf("[gRPC] Got %d execution records for job %d", len(historyResp.Executions), jobID)

	// Test pagination
	historyReqWithPagination := &service.GetSchedulerJobHistoryRequest{
		JobID: jobID,
		Limit: 5,
	}
	historyRespPaginated, err := client.SchedulerGetJobHistory(ctx, historyReqWithPagination)
	require.NoError(t, err)
	assert.NotNil(t, historyRespPaginated)

	t.Logf("[gRPC] Paginated history: %d executions, hasMore=%v",
		len(historyRespPaginated.Executions), historyRespPaginated.HasMore)
}

// =============================================================================
// GetSchedulerStatus Tests
// =============================================================================

func testGRPCGetSchedulerStatus(t *testing.T, ctx context.Context, client *grpcclient.Client) {
	t.Helper()

	resp, err := client.SchedulerGetStatus(ctx)
	require.NoError(t, err, "GetSchedulerStatus should succeed")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Stats)

	assert.NotNil(t, resp.Stats)
	assert.False(t, resp.Timestamp.IsZero())

	t.Logf("[gRPC] Scheduler status: running=%v, jobCount=%d, runningJobs=%d, success=%d, failure=%d",
		resp.Running,
		resp.Stats.JobCount,
		resp.Stats.RunningJobs,
		resp.Stats.SuccessCount,
		resp.Stats.FailureCount)
}

// =============================================================================
// DeleteJob Tests
// =============================================================================

func testGRPCDeleteJob(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	t.Run("SoftDelete", func(t *testing.T) {
		jobName := generateUniqueJobName(prefix + "-softdelete")
		createReq := &service.CreateSchedulerJobRequest{
			Name:      jobName,
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "test-handler-softdelete",
			ExecuteAt: time.Now().Add(6 * time.Hour),
		}

		createResp, err := client.SchedulerCreateJob(ctx, createReq)
		require.NoError(t, err)
		jobID := createResp.Job.ID

		deleteReq := &service.DeleteSchedulerJobRequest{
			JobID: jobID,
			Force: false,
		}
		deleteResp, err := client.SchedulerDeleteJob(ctx, deleteReq)
		require.NoError(t, err, "DeleteJob (soft) should succeed")
		require.NotNil(t, deleteResp)

		assert.Equal(t, jobID, deleteResp.JobID)
		assert.True(t, deleteResp.Deleted)
		assert.False(t, deleteResp.Timestamp.IsZero())

		// Verify the job is deleted
		_, err = client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: jobID})
		assert.Error(t, err, "Job should not exist after deletion")

		t.Logf("[gRPC] Soft deleted job: ID=%d", deleteResp.JobID)
	})

	t.Run("ForceDelete", func(t *testing.T) {
		jobName := generateUniqueJobName(prefix + "-forcedelete")
		createReq := &service.CreateSchedulerJobRequest{
			Name:     jobName,
			Type:     service.SchedulerJobTypeRecurring,
			Handler:  "test-handler-forcedelete",
			Schedule: "0 0 * * *", // Daily at midnight
		}

		createResp, err := client.SchedulerCreateJob(ctx, createReq)
		require.NoError(t, err)
		jobID := createResp.Job.ID

		deleteReq := &service.DeleteSchedulerJobRequest{
			JobID: jobID,
			Force: true,
		}
		deleteResp, err := client.SchedulerDeleteJob(ctx, deleteReq)
		require.NoError(t, err, "DeleteJob (force) should succeed")
		require.NotNil(t, deleteResp)

		assert.Equal(t, jobID, deleteResp.JobID)
		assert.True(t, deleteResp.Deleted)

		t.Logf("[gRPC] Force deleted job: ID=%d", deleteResp.JobID)
	})
}

// =============================================================================
// Error Cases Tests
// =============================================================================

func testGRPCInvalidJobParameters(t *testing.T, ctx context.Context, client *grpcclient.Client) {
	t.Helper()

	t.Run("EmptyJobName", func(t *testing.T) {
		req := &service.CreateSchedulerJobRequest{
			Name:      "",
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(1 * time.Hour),
		}
		_, err := client.SchedulerCreateJob(ctx, req)
		assert.Error(t, err, "CreateJob should fail with empty name")
		t.Logf("[gRPC] Empty name error: %v", err)
	})

	t.Run("EmptyHandler", func(t *testing.T) {
		req := &service.CreateSchedulerJobRequest{
			Name:      "test-job-no-handler",
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "",
			ExecuteAt: time.Now().Add(1 * time.Hour),
		}
		_, err := client.SchedulerCreateJob(ctx, req)
		assert.Error(t, err, "CreateJob should fail with empty handler")
		t.Logf("[gRPC] Empty handler error: %v", err)
	})

	t.Run("InvalidCronSchedule", func(t *testing.T) {
		req := &service.CreateSchedulerJobRequest{
			Name:     "test-job-invalid-cron",
			Type:     service.SchedulerJobTypeRecurring,
			Handler:  "test-handler",
			Schedule: "invalid-cron-expression",
		}
		_, err := client.SchedulerCreateJob(ctx, req)
		assert.Error(t, err, "CreateJob should fail with invalid cron schedule")
		t.Logf("[gRPC] Invalid cron error: %v", err)
	})

	t.Run("RecurringWithoutSchedule", func(t *testing.T) {
		req := &service.CreateSchedulerJobRequest{
			Name:     "test-job-recurring-no-schedule",
			Type:     service.SchedulerJobTypeRecurring,
			Handler:  "test-handler",
			Schedule: "",
		}
		_, err := client.SchedulerCreateJob(ctx, req)
		assert.Error(t, err, "CreateJob should fail for recurring job without schedule")
		t.Logf("[gRPC] Recurring without schedule error: %v", err)
	})
}

func testGRPCNonExistentJobOperations(t *testing.T, ctx context.Context, client *grpcclient.Client) {
	t.Helper()

	nonExistentJobID := uint64(999999999)

	t.Run("GetNonExistentJob", func(t *testing.T) {
		req := &service.GetSchedulerJobRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerGetJob(ctx, req)
		assert.Error(t, err, "GetJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent GetJob error: %v", err)
	})

	t.Run("UpdateNonExistentJob", func(t *testing.T) {
		req := &service.UpdateSchedulerJobRequest{
			JobID:       nonExistentJobID,
			Description: "Should fail",
		}
		_, err := client.SchedulerUpdateJob(ctx, req)
		assert.Error(t, err, "UpdateJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent UpdateJob error: %v", err)
	})

	t.Run("DeleteNonExistentJob", func(t *testing.T) {
		req := &service.DeleteSchedulerJobRequest{
			JobID: nonExistentJobID,
			Force: true,
		}
		_, err := client.SchedulerDeleteJob(ctx, req)
		assert.Error(t, err, "DeleteJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent DeleteJob error: %v", err)
	})

	t.Run("TriggerNonExistentJob", func(t *testing.T) {
		req := &service.TriggerSchedulerJobRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerTriggerJob(ctx, req)
		assert.Error(t, err, "TriggerJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent TriggerJob error: %v", err)
	})

	t.Run("PauseNonExistentJob", func(t *testing.T) {
		req := &service.PauseSchedulerJobRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerPauseJob(ctx, req)
		assert.Error(t, err, "PauseJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent PauseJob error: %v", err)
	})

	t.Run("ResumeNonExistentJob", func(t *testing.T) {
		req := &service.ResumeSchedulerJobRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerResumeJob(ctx, req)
		assert.Error(t, err, "ResumeJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent ResumeJob error: %v", err)
	})

	t.Run("CancelNonExistentJob", func(t *testing.T) {
		req := &service.CancelSchedulerJobRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerCancelJob(ctx, req)
		assert.Error(t, err, "CancelJob should fail for non-existent job")
		t.Logf("[gRPC] Non-existent CancelJob error: %v", err)
	})

	t.Run("GetHistoryNonExistentJob", func(t *testing.T) {
		req := &service.GetSchedulerJobHistoryRequest{JobID: nonExistentJobID}
		_, err := client.SchedulerGetJobHistory(ctx, req)
		assert.Error(t, err, "GetJobHistory should fail for non-existent job")
		t.Logf("[gRPC] Non-existent GetJobHistory error: %v", err)
	})
}

func testGRPCConcurrentJobOperations(t *testing.T, ctx context.Context, client *grpcclient.Client) {
	t.Helper()

	concurrency := 10
	prefix := fmt.Sprintf("grpc-concurrent-%d", time.Now().UnixNano())

	var wg sync.WaitGroup
	errChan := make(chan error, concurrency)
	jobIDs := make(chan uint64, concurrency)

	// Create jobs concurrently
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			req := &service.CreateSchedulerJobRequest{
				Name:      fmt.Sprintf("%s-job-%d", prefix, idx),
				Type:      service.SchedulerJobTypeOneShot,
				Handler:   fmt.Sprintf("concurrent-handler-%d", idx),
				ExecuteAt: time.Now().Add(time.Duration(idx+1) * time.Hour),
				Labels: map[string]string{
					"concurrent": "true",
					"index":      fmt.Sprintf("%d", idx),
				},
			}

			resp, err := client.SchedulerCreateJob(ctx, req)
			if err != nil {
				errChan <- fmt.Errorf("create error %d: %w", idx, err)
				return
			}

			jobIDs <- resp.Job.ID
		}(i)
	}

	wg.Wait()
	close(errChan)
	close(jobIDs)

	// Check for errors
	errorCount := 0
	for err := range errChan {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	// Collect job IDs for cleanup
	createdJobIDs := make([]uint64, 0, concurrency)
	for jobID := range jobIDs {
		createdJobIDs = append(createdJobIDs, jobID)
	}

	// Cleanup
	for _, jobID := range createdJobIDs {
		cleanupGRPCJob(t, client, ctx, jobID)
	}

	assert.Equal(t, 0, errorCount, "All concurrent operations should succeed")
	t.Logf("[gRPC] Completed %d concurrent job creations", len(createdJobIDs))
}

// =============================================================================
// Job Type Tests
// =============================================================================

func testGRPCOneShotWithPayload(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	jobName := generateUniqueJobName(prefix + "-payload")

	payload := map[string]interface{}{
		"action":     "process",
		"target":     "database",
		"parameters": map[string]interface{}{"table": "users", "operation": "cleanup"},
		"options":    []string{"verbose", "dry-run"},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	req := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Job with complex payload",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "payload-handler",
		ExecuteAt:   time.Now().Add(2 * time.Hour),
		Payload:     payloadBytes,
		Priority:    10,
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp.Job)

	defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

	assert.NotEmpty(t, resp.Job.Payload)

	// Verify payload can be unmarshaled
	var retrievedPayload map[string]interface{}
	err = json.Unmarshal(resp.Job.Payload, &retrievedPayload)
	require.NoError(t, err)
	assert.Equal(t, "process", retrievedPayload["action"])

	t.Logf("[gRPC] Created job with payload: ID=%d, PayloadSize=%d bytes",
		resp.Job.ID, len(resp.Job.Payload))
}

func testGRPCRecurringWithCronSchedule(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	cronSchedules := []struct {
		name     string
		schedule string
	}{
		{"EveryMinute", "* * * * *"},
		{"Every5Minutes", "*/5 * * * *"},
		{"EveryHour", "0 * * * *"},
		{"DailyAtMidnight", "0 0 * * *"},
		{"WeekdaysAt9AM", "0 9 * * 1-5"},
		{"MonthlyFirstDay", "0 0 1 * *"},
	}

	for _, cs := range cronSchedules {
		t.Run(cs.name, func(t *testing.T) {
			jobName := generateUniqueJobName(prefix + "-cron-" + cs.name)

			req := &service.CreateSchedulerJobRequest{
				Name:     jobName,
				Type:     service.SchedulerJobTypeRecurring,
				Handler:  "cron-handler-" + cs.name,
				Schedule: cs.schedule,
				Labels: map[string]string{
					"cron_type": cs.name,
				},
			}

			resp, err := client.SchedulerCreateJob(ctx, req)
			require.NoError(t, err, "CreateJob should succeed for cron schedule: %s", cs.schedule)
			require.NotNil(t, resp.Job)

			defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

			assert.Equal(t, cs.schedule, resp.Job.Schedule)
			t.Logf("[gRPC] Created cron job: ID=%d, Schedule=%s", resp.Job.ID, resp.Job.Schedule)
		})
	}
}

func testGRPCJobWithLabels(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	jobName := generateUniqueJobName(prefix + "-labels")

	labels := map[string]string{
		"environment": "production",
		"region":      "us-east-1",
		"team":        "platform",
		"service":     "data-pipeline",
		"priority":    "high",
	}

	req := &service.CreateSchedulerJobRequest{
		Name:      jobName,
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "labels-handler",
		ExecuteAt: time.Now().Add(3 * time.Hour),
		Labels:    labels,
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp.Job)

	defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

	assert.NotNil(t, resp.Job.Labels)
	for key, expectedValue := range labels {
		assert.Equal(t, expectedValue, resp.Job.Labels[key], "Label %s should match", key)
	}

	t.Logf("[gRPC] Created job with %d labels: ID=%d", len(resp.Job.Labels), resp.Job.ID)
}

func testGRPCJobWithPriorityAndTimeout(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	testCases := []struct {
		name     string
		priority int32
		timeout  time.Duration
	}{
		{"LowPriority", 1, 1 * time.Minute},
		{"MediumPriority", 50, 5 * time.Minute},
		{"HighPriority", 100, 30 * time.Minute},
		{"CriticalPriority", 200, 1 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jobName := generateUniqueJobName(prefix + "-" + tc.name)

			req := &service.CreateSchedulerJobRequest{
				Name:      jobName,
				Type:      service.SchedulerJobTypeOneShot,
				Handler:   "priority-handler",
				ExecuteAt: time.Now().Add(4 * time.Hour),
				Priority:  tc.priority,
				Timeout:   tc.timeout,
			}

			resp, err := client.SchedulerCreateJob(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp.Job)

			defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

			assert.Equal(t, tc.priority, resp.Job.Priority)
			t.Logf("[gRPC] Created %s job: ID=%d, Priority=%d, Timeout=%v",
				tc.name, resp.Job.ID, resp.Job.Priority, resp.Job.Timeout)
		})
	}
}

func testGRPCJobWithMaxRetries(t *testing.T, ctx context.Context, client *grpcclient.Client, prefix string) {
	t.Helper()

	testCases := []struct {
		name       string
		maxRetries int32
	}{
		{"NoRetries", 0},
		{"SingleRetry", 1},
		{"StandardRetries", 3},
		{"HighRetries", 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jobName := generateUniqueJobName(prefix + "-" + tc.name)

			req := &service.CreateSchedulerJobRequest{
				Name:       jobName,
				Type:       service.SchedulerJobTypeOneShot,
				Handler:    "retry-handler",
				ExecuteAt:  time.Now().Add(5 * time.Hour),
				MaxRetries: tc.maxRetries,
			}

			resp, err := client.SchedulerCreateJob(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp.Job)

			defer cleanupGRPCJob(t, client, ctx, resp.Job.ID)

			assert.Equal(t, tc.maxRetries, resp.Job.MaxRetries)
			t.Logf("[gRPC] Created %s job: ID=%d, MaxRetries=%d",
				tc.name, resp.Job.ID, resp.Job.MaxRetries)
		})
	}
}
