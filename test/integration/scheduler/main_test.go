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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Note: The gRPC client scheduler methods are not yet implemented.
// These tests use REST for scheduler operations and gRPC for KMS operations.

// TestMain runs before all tests in this package.
func TestMain(m *testing.M) {
	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}

	// Cleanup
	if srv != nil {
		helpers.TeardownEmbeddedServer(srv)
	}
	os.Exit(code)
}

// TestSchedulerAvailability tests that scheduler service is available.
func TestSchedulerAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("REST availability", func(t *testing.T) {
		endpoints := getRESTEndpoints()
		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				client := NewSchedulerRESTClient(t, endpoint)

				resp, err := client.GetStatus(ctx)
				require.NoError(t, err, "Scheduler must be available on REST at %s", endpoint)
				assert.NotNil(t, resp)
				assert.True(t, resp.Running, "scheduler should be running")
			})
		}
	})

	t.Run("KMS service for grace period tests", func(t *testing.T) {
		endpoints := getGRPCEndpoints()
		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				tlsCfg, err := helpers.IntegrationTLSConfig()
				require.NoError(t, err, "load integration TLS config")
				client, err := grpcclient.NewClient(endpoint, grpcclient.WithTLSConfig(tlsCfg), helpers.TenantGRPCClientOption(t))
				require.NoError(t, err, "gRPC service must be available at %s", endpoint)
				defer client.Close()

				requireKMSAvailable(t, client, ctx)
				t.Log("KMS service available for grace period integration tests")
			})
		}
	})
}

// TestSchedulerJobLifecycle tests complete job lifecycle via REST.
func TestSchedulerJobLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Run("REST job lifecycle", func(t *testing.T) {
		testRESTJobLifecycle(t, ctx)
	})
}

// testRESTJobLifecycle tests job lifecycle via REST.
func testRESTJobLifecycle(t *testing.T, ctx context.Context) {
	endpoints := getRESTEndpoints()
	client := NewSchedulerRESTClient(t, endpoints[0])

	// Verify scheduler is running
	status, err := client.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available on REST")
	require.True(t, status.Running, "scheduler should be running")

	// 1. Create a one-shot job
	jobName := generateUniqueJobName("lifecycle-test")
	payload, _ := json.Marshal(map[string]interface{}{
		"test_key": "test_value",
	})

	createReq := &RESTCreateJobRequest{
		Name:        jobName,
		Description: "Integration test job lifecycle",
		Type:        string(scheduler.JobTypeOneShot),
		OwnerType:   string(scheduler.OwnerTypeTenant),
		OwnerID:     callerTenantID(t),
		Handler:     "test.noop",                                        // Use a test handler that does nothing
		ExecuteAt:   time.Now().Add(1 * time.Hour).Format(time.RFC3339), // Schedule for later (RFC3339 format)
		Payload:     payload,
		Labels: map[string]string{
			"test":    "lifecycle",
			"created": time.Now().Format(time.RFC3339),
		},
	}

	job, err := client.CreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.NotZero(t, job.ID)
	defer cleanupJob(t, client, ctx, job.ID)

	assert.Equal(t, jobName, job.Name)
	assert.Equal(t, string(scheduler.JobStatusPending), job.Status)

	// 2. Get job
	retrievedJob, err := client.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrievedJob.ID)
	assert.Equal(t, jobName, retrievedJob.Name)

	// 3. List jobs (should include our job)
	listResp, err := client.ListJobs(ctx, 100, "")
	require.NoError(t, err)
	assert.NotNil(t, listResp)

	var found bool
	for _, j := range listResp.Jobs {
		if j.ID == job.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "job should be in the list")

	// 4. Delete job
	err = client.DeleteJob(ctx, job.ID)
	require.NoError(t, err)

	// 5. Verify job is deleted
	_, err = client.GetJob(ctx, job.ID)
	assert.Error(t, err)
}

// TestSchedulerRecurringJob tests recurring job scheduling.
func TestSchedulerRecurringJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	client := NewSchedulerRESTClient(t, endpoints[0])

	// Verify scheduler is available
	_, err := client.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available")

	// Create a recurring job with cron schedule
	jobName := generateUniqueJobName("recurring-test")
	createReq := &RESTCreateJobRequest{
		Name:        jobName,
		Description: "Recurring job test",
		Type:        string(scheduler.JobTypeRecurring),
		OwnerType:   string(scheduler.OwnerTypeTenant),
		OwnerID:     callerTenantID(t),
		Handler:     "test.noop",
		Schedule:    "*/5 * * * *", // Every 5 minutes
		Labels: map[string]string{
			"test": "recurring",
		},
	}

	job, err := client.CreateJob(ctx, createReq)
	require.NoError(t, err)
	require.NotNil(t, job)
	defer cleanupJob(t, client, ctx, job.ID)

	assert.Equal(t, string(scheduler.JobTypeRecurring), job.Type)
	assert.False(t, job.NextRun.IsZero(), "recurring job should have next_run set")
}

// TestSchedulerJobPauseResume tests job pause and resume operations via REST.
func TestSchedulerJobPauseResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available")
	client := NewSchedulerRESTClient(t, endpoints[0])

	// Verify scheduler is running
	status, err := client.GetStatus(ctx)
	require.NoError(t, err, "scheduler must be available for pause/resume test")
	require.True(t, status.Running, "scheduler must be running")

	// Create a recurring job (scheduled far in future so it stays pending)
	jobName := generateUniqueJobName("pause-resume-test")
	createReq := &RESTCreateJobRequest{
		Name:        jobName,
		Description: "Pause/resume integration test",
		Type:        string(scheduler.JobTypeRecurring),
		OwnerType:   string(scheduler.OwnerTypeTenant),
		OwnerID:     callerTenantID(t),
		Handler:     "test.noop",
		Schedule:    "0 0 1 1 *", // Once a year (Jan 1 midnight)
		Labels: map[string]string{
			"test": "pause-resume",
		},
	}

	job, err := client.CreateJob(ctx, createReq)
	require.NoError(t, err, "must create job for pause/resume test")
	require.NotNil(t, job)
	require.NotZero(t, job.ID)
	defer cleanupJob(t, client, ctx, job.ID)

	t.Logf("Created job %d with status %s", job.ID, job.Status)

	// Pause the job
	pauseResp, err := client.PauseJob(ctx, job.ID)
	require.NoError(t, err, "pause job must succeed")
	require.NotNil(t, pauseResp)
	assert.Equal(t, job.ID, pauseResp.JobID)
	assert.Equal(t, string(scheduler.JobStatusPaused), pauseResp.NewStatus,
		"new status after pause must be 'paused'")
	t.Logf("Paused job %d: old_status=%s new_status=%s", pauseResp.JobID, pauseResp.OldStatus, pauseResp.NewStatus)

	// Verify job status is paused via GET
	pausedJob, err := client.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, string(scheduler.JobStatusPaused), pausedJob.Status,
		"job status must be 'paused' after pause operation")

	// Resume the job
	resumeResp, err := client.ResumeJob(ctx, job.ID)
	require.NoError(t, err, "resume job must succeed")
	require.NotNil(t, resumeResp)
	assert.Equal(t, job.ID, resumeResp.JobID)
	assert.Equal(t, string(scheduler.JobStatusPaused), resumeResp.OldStatus,
		"old status after resume must be 'paused'")
	t.Logf("Resumed job %d: old_status=%s new_status=%s", resumeResp.JobID, resumeResp.OldStatus, resumeResp.NewStatus)

	// Verify job status is no longer paused via GET
	resumedJob, err := client.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.NotEqual(t, string(scheduler.JobStatusPaused), resumedJob.Status,
		"job status must not be 'paused' after resume operation")
}
