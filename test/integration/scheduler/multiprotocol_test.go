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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestSchedulerMultiProtocol verifies that scheduler operations work identically
// across all supported protocols: REST, gRPC, Unix Socket, and MCP.
func TestSchedulerMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Test each protocol independently
	t.Run("REST", func(t *testing.T) {
		testSchedulerProtocol(t, ctx, &restProtocolClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testSchedulerProtocol(t, ctx, &grpcProtocolClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testSchedulerProtocol(t, ctx, &unixSocketProtocolClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testSchedulerProtocol(t, ctx, &embeddedProtocolClient{})
		})
	}
}

// schedulerProtocol abstracts the different protocols for testing.
type schedulerProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
	Name() string
}

// testSchedulerProtocol runs the complete scheduler test suite against a protocol.
func testSchedulerProtocol(t *testing.T, ctx context.Context, protocol schedulerProtocol) {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Failed to setup %s protocol", protocol.Name())
	defer func() {
		err := protocol.Teardown(t)
		if err != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), err)
		}
	}()

	basePrefix := fmt.Sprintf("mp-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Test 1: Create Job
	t.Run("create", func(t *testing.T) {
		testCreateJob(t, ctx, protocol, basePrefix+"-create")
	})

	// Test 2: Get Job
	t.Run("get", func(t *testing.T) {
		testGetJob(t, ctx, protocol, basePrefix+"-get")
	})

	// Test 3: Update Job
	t.Run("update", func(t *testing.T) {
		testUpdateJob(t, ctx, protocol, basePrefix+"-update")
	})

	// Test 4: List Jobs
	t.Run("list", func(t *testing.T) {
		testListJobs(t, ctx, protocol, basePrefix+"-list")
	})

	// Test 5: Control Operations (trigger, pause, resume, cancel)
	t.Run("control", func(t *testing.T) {
		testControlOperations(t, ctx, protocol, basePrefix+"-control")
	})

	// Test 6: Job History
	t.Run("history", func(t *testing.T) {
		testJobHistory(t, ctx, protocol, basePrefix+"-history")
	})

	// Test 7: Scheduler Status
	t.Run("status", func(t *testing.T) {
		testSchedulerStatus(t, ctx, protocol)
	})

	// Test 8: Delete Job
	t.Run("delete", func(t *testing.T) {
		testDeleteJob(t, ctx, protocol, basePrefix+"-delete")
	})

	// Test 9: Concurrent Operations
	t.Run("concurrent", func(t *testing.T) {
		testConcurrentOperations(t, ctx, protocol, basePrefix+"-concurrent")
	})

	// Test 10: Error Handling
	t.Run("error_handling", func(t *testing.T) {
		testErrorHandling(t, ctx, protocol)
	})
}

// =============================================================================
// Individual Test Functions
// =============================================================================

func testCreateJob(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	tests := []struct {
		name        string
		jobType     service.SchedulerJobType
		handler     string
		schedule    string
		executeAt   time.Time
		delay       time.Duration
		description string
	}{
		{
			name:        "one-shot-job",
			jobType:     service.SchedulerJobTypeOneShot,
			handler:     "test-handler",
			executeAt:   time.Now().Add(1 * time.Hour),
			description: "Test one-shot job",
		},
		{
			name:        "recurring-job",
			jobType:     service.SchedulerJobTypeRecurring,
			handler:     "recurring-handler",
			schedule:    "*/5 * * * *", // Every 5 minutes
			description: "Test recurring job",
		},
		{
			name:        "delayed-job",
			jobType:     service.SchedulerJobTypeDelayed,
			handler:     "delayed-handler",
			delay:       30 * time.Minute,
			description: "Test delayed job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobName := fmt.Sprintf("%s-%s", prefix, tt.name)

			req := &service.CreateSchedulerJobRequest{
				Name:        jobName,
				Description: tt.description,
				Type:        tt.jobType,
				Handler:     tt.handler,
				Schedule:    tt.schedule,
				ExecuteAt:   tt.executeAt,
				Delay:       tt.delay,
				Priority:    10,
				Timeout:     5 * time.Minute,
				MaxRetries:  3,
				Labels: map[string]string{
					"test":     "multiprotocol",
					"protocol": protocol.Name(),
				},
			}

			resp, err := protocol.CreateJob(ctx, req)
			require.NoError(t, err, "CreateJob failed for %s via %s", tt.name, protocol.Name())
			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Job)
			assert.NotZero(t, resp.Job.ID)
			assert.Equal(t, jobName, resp.Job.Name)
			assert.Equal(t, tt.jobType, resp.Job.Type)
			assert.Equal(t, tt.handler, resp.Job.Handler)
			assert.Equal(t, service.SchedulerJobStatusPending, resp.Job.Status)

			// Cleanup
			deleteReq := &service.DeleteSchedulerJobRequest{
				JobID: resp.Job.ID,
				Force: true,
			}
			_, _ = protocol.DeleteJob(ctx, deleteReq)

			t.Logf("[%s] Created job: ID=%d, Name=%s, Type=%s",
				protocol.Name(), resp.Job.ID, resp.Job.Name, resp.Job.Type)
		})
	}
}

func testGetJob(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	// Create a test job
	jobName := fmt.Sprintf("%s-test", prefix)
	createReq := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Test job for GetJob",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "test-handler",
		ExecuteAt:   time.Now().Add(1 * time.Hour),
		Priority:    5,
		Timeout:     10 * time.Minute,
		MaxRetries:  2,
	}

	createResp, err := protocol.CreateJob(ctx, createReq)
	require.NoError(t, err)
	jobID := createResp.Job.ID
	defer func() {
		deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
		_, _ = protocol.DeleteJob(ctx, deleteReq)
	}()

	t.Run("get by ID", func(t *testing.T) {
		req := &service.GetSchedulerJobRequest{JobID: jobID}
		resp, err := protocol.GetJob(ctx, req)
		require.NoError(t, err, "GetJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Job)
		assert.Equal(t, jobID, resp.Job.ID)
		assert.Equal(t, jobName, resp.Job.Name)
		assert.Equal(t, service.SchedulerJobTypeOneShot, resp.Job.Type)
	})

	t.Run("get non-existent", func(t *testing.T) {
		req := &service.GetSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.GetJob(ctx, req)
		assert.Error(t, err)
	})

	t.Logf("[%s] Retrieved job: ID=%d", protocol.Name(), jobID)
}

func testUpdateJob(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	// Create a test job
	jobName := fmt.Sprintf("%s-test", prefix)
	createReq := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: "Original description",
		Type:        service.SchedulerJobTypeOneShot,
		Handler:     "test-handler",
		ExecuteAt:   time.Now().Add(1 * time.Hour),
		Priority:    5,
	}

	createResp, err := protocol.CreateJob(ctx, createReq)
	require.NoError(t, err)
	jobID := createResp.Job.ID
	defer func() {
		deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
		_, _ = protocol.DeleteJob(ctx, deleteReq)
	}()

	t.Run("update description and priority", func(t *testing.T) {
		req := &service.UpdateSchedulerJobRequest{
			JobID:       jobID,
			Name:        fmt.Sprintf("%s-updated", jobName),
			Description: "Updated description",
			Priority:    15,
		}
		resp, err := protocol.UpdateJob(ctx, req)
		require.NoError(t, err, "UpdateJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Job)
		assert.Equal(t, "Updated description", resp.Job.Description)
		assert.Equal(t, int32(15), resp.Job.Priority)

		// Verify the update persisted
		getReq := &service.GetSchedulerJobRequest{JobID: jobID}
		getResp, err := protocol.GetJob(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, "Updated description", getResp.Job.Description)
	})

	t.Run("update timeout and retries", func(t *testing.T) {
		req := &service.UpdateSchedulerJobRequest{
			JobID:      jobID,
			Timeout:    20 * time.Minute,
			MaxRetries: 5,
		}
		resp, err := protocol.UpdateJob(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, int32(5), resp.Job.MaxRetries)
	})

	t.Logf("[%s] Updated job: ID=%d", protocol.Name(), jobID)
}

func testListJobs(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	// Create multiple jobs
	numJobs := 5
	jobIDs := make([]uint64, 0, numJobs)

	for i := 0; i < numJobs; i++ {
		req := &service.CreateSchedulerJobRequest{
			Name:      fmt.Sprintf("%s-job-%d", prefix, i),
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Duration(i+1) * time.Hour),
			Labels: map[string]string{
				"batch": prefix,
			},
		}
		resp, err := protocol.CreateJob(ctx, req)
		require.NoError(t, err)
		jobIDs = append(jobIDs, resp.Job.ID)
	}

	defer func() {
		for _, jobID := range jobIDs {
			deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
			_, _ = protocol.DeleteJob(ctx, deleteReq)
		}
	}()

	t.Run("list all", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{Limit: 100}
		resp, err := protocol.ListJobs(ctx, req)
		require.NoError(t, err, "ListJobs failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Jobs)
		t.Logf("[%s] Listed %d total jobs", protocol.Name(), len(resp.Jobs))
	})

	t.Run("list with limit", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{Limit: 3}
		resp, err := protocol.ListJobs(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp.Jobs)
		t.Logf("[%s] Limited list returned %d jobs", protocol.Name(), len(resp.Jobs))
	})

	t.Run("list by status", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Status: service.SchedulerJobStatusPending,
			Limit:  100,
		}
		resp, err := protocol.ListJobs(ctx, req)
		require.NoError(t, err)
		for _, job := range resp.Jobs {
			assert.Equal(t, service.SchedulerJobStatusPending, job.Status)
		}
		t.Logf("[%s] Listed %d pending jobs", protocol.Name(), len(resp.Jobs))
	})

	t.Run("list by handler", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Handler: "test-handler",
			Limit:   100,
		}
		resp, err := protocol.ListJobs(ctx, req)
		require.NoError(t, err)
		for _, job := range resp.Jobs {
			assert.Equal(t, "test-handler", job.Handler)
		}
		t.Logf("[%s] Listed %d jobs with handler test-handler", protocol.Name(), len(resp.Jobs))
	})

	t.Run("list with labels", func(t *testing.T) {
		req := &service.ListSchedulerJobsRequest{
			Labels: map[string]string{"batch": prefix},
			Limit:  100,
		}
		resp, err := protocol.ListJobs(ctx, req)
		require.NoError(t, err)
		t.Logf("[%s] Listed %d jobs with batch label", protocol.Name(), len(resp.Jobs))
	})
}

func testControlOperations(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	// Create a test job for control operations
	jobName := fmt.Sprintf("%s-control-test", prefix)
	createReq := &service.CreateSchedulerJobRequest{
		Name:     jobName,
		Type:     service.SchedulerJobTypeRecurring,
		Handler:  "control-test-handler",
		Schedule: "*/10 * * * *", // Every 10 minutes
		Priority: 10,
		Timeout:  5 * time.Minute,
	}

	createResp, err := protocol.CreateJob(ctx, createReq)
	require.NoError(t, err)
	jobID := createResp.Job.ID
	defer func() {
		deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
		_, _ = protocol.DeleteJob(ctx, deleteReq)
	}()

	// Test pause/resume before trigger - pause only works on pending jobs
	t.Run("pause job", func(t *testing.T) {
		req := &service.PauseSchedulerJobRequest{JobID: jobID}
		resp, err := protocol.PauseJob(ctx, req)
		require.NoError(t, err, "PauseJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, jobID, resp.JobID)
		assert.Equal(t, service.SchedulerJobStatusPaused, resp.NewStatus)

		// Verify the job is paused
		getReq := &service.GetSchedulerJobRequest{JobID: jobID}
		getResp, err := protocol.GetJob(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, service.SchedulerJobStatusPaused, getResp.Job.Status)
		t.Logf("[%s] Paused job: ID=%d", protocol.Name(), jobID)
	})

	t.Run("resume job", func(t *testing.T) {
		req := &service.ResumeSchedulerJobRequest{JobID: jobID}
		resp, err := protocol.ResumeJob(ctx, req)
		require.NoError(t, err, "ResumeJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, jobID, resp.JobID)
		assert.Equal(t, service.SchedulerJobStatusPaused, resp.OldStatus)
		assert.Equal(t, service.SchedulerJobStatusPending, resp.NewStatus)
		t.Logf("[%s] Resumed job: ID=%d", protocol.Name(), jobID)
	})

	// Trigger after resume - job is back to pending status
	t.Run("trigger job", func(t *testing.T) {
		req := &service.TriggerSchedulerJobRequest{JobID: jobID}
		resp, err := protocol.TriggerJob(ctx, req)
		require.NoError(t, err, "TriggerJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, jobID, resp.JobID)
		t.Logf("[%s] Triggered job: ID=%d, Status=%s", protocol.Name(), jobID, resp.Status)
	})

	t.Run("cancel job", func(t *testing.T) {
		req := &service.CancelSchedulerJobRequest{
			JobID:  jobID,
			Reason: "Test cancellation",
		}
		resp, err := protocol.CancelJob(ctx, req)
		require.NoError(t, err, "CancelJob failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, jobID, resp.JobID)
		assert.Equal(t, service.SchedulerJobStatusCanceled, resp.NewStatus)

		// Verify the job is canceled
		getReq := &service.GetSchedulerJobRequest{JobID: jobID}
		getResp, err := protocol.GetJob(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, service.SchedulerJobStatusCanceled, getResp.Job.Status)
		t.Logf("[%s] Canceled job: ID=%d", protocol.Name(), jobID)
	})
}

func testJobHistory(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	// Create and trigger a job to generate history
	jobName := fmt.Sprintf("%s-history-test", prefix)
	createReq := &service.CreateSchedulerJobRequest{
		Name:      jobName,
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "history-test-handler",
		ExecuteAt: time.Now().Add(1 * time.Second), // Execute soon
		Priority:  10,
		Timeout:   1 * time.Minute,
	}

	createResp, err := protocol.CreateJob(ctx, createReq)
	require.NoError(t, err)
	jobID := createResp.Job.ID
	defer func() {
		deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
		_, _ = protocol.DeleteJob(ctx, deleteReq)
	}()

	// Trigger the job to create execution history
	triggerReq := &service.TriggerSchedulerJobRequest{JobID: jobID}
	_, err = protocol.TriggerJob(ctx, triggerReq)
	if err != nil {
		t.Logf("[%s] TriggerJob returned error (may be expected): %v", protocol.Name(), err)
	}

	t.Run("get job history", func(t *testing.T) {
		req := &service.GetSchedulerJobHistoryRequest{
			JobID: jobID,
			Limit: 10,
		}
		resp, err := protocol.GetJobHistory(ctx, req)
		require.NoError(t, err, "GetJobHistory failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		// History may be empty if job hasn't executed yet
		t.Logf("[%s] Got %d execution records for job %d",
			protocol.Name(), len(resp.Executions), jobID)
	})

	t.Run("get history with pagination", func(t *testing.T) {
		req := &service.GetSchedulerJobHistoryRequest{
			JobID: jobID,
			Limit: 5,
		}
		resp, err := protocol.GetJobHistory(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		t.Logf("[%s] Paginated history: %d executions, hasMore=%v",
			protocol.Name(), len(resp.Executions), resp.HasMore)
	})
}

func testSchedulerStatus(t *testing.T, ctx context.Context, protocol schedulerProtocol) {
	t.Helper()

	resp, err := protocol.GetStatus(ctx)
	require.NoError(t, err, "GetStatus failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Stats)

	t.Logf("[%s] Scheduler status: running=%v, jobCount=%d, runningJobs=%d, success=%d, failure=%d",
		protocol.Name(),
		resp.Running,
		resp.Stats.JobCount,
		resp.Stats.RunningJobs,
		resp.Stats.SuccessCount,
		resp.Stats.FailureCount)
}

func testDeleteJob(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	t.Run("delete existing job", func(t *testing.T) {
		// Create a job to delete
		jobName := fmt.Sprintf("%s-delete-test", prefix)
		createReq := &service.CreateSchedulerJobRequest{
			Name:      jobName,
			Type:      service.SchedulerJobTypeOneShot,
			Handler:   "delete-test-handler",
			ExecuteAt: time.Now().Add(1 * time.Hour),
		}

		createResp, err := protocol.CreateJob(ctx, createReq)
		require.NoError(t, err)
		jobID := createResp.Job.ID

		// Delete the job
		deleteReq := &service.DeleteSchedulerJobRequest{
			JobID: jobID,
			Force: false,
		}
		deleteResp, err := protocol.DeleteJob(ctx, deleteReq)
		require.NoError(t, err, "DeleteJob failed for %s", protocol.Name())
		assert.NotNil(t, deleteResp)
		assert.True(t, deleteResp.Deleted)

		// Verify the job is deleted
		getReq := &service.GetSchedulerJobRequest{JobID: jobID}
		_, err = protocol.GetJob(ctx, getReq)
		assert.Error(t, err, "Job should not exist after deletion")

		t.Logf("[%s] Deleted job: ID=%d", protocol.Name(), jobID)
	})

	t.Run("force delete", func(t *testing.T) {
		// Create a job
		jobName := fmt.Sprintf("%s-force-delete-test", prefix)
		createReq := &service.CreateSchedulerJobRequest{
			Name:     jobName,
			Type:     service.SchedulerJobTypeRecurring,
			Handler:  "force-delete-handler",
			Schedule: "0 * * * *", // Every hour
		}

		createResp, err := protocol.CreateJob(ctx, createReq)
		require.NoError(t, err)
		jobID := createResp.Job.ID

		// Force delete
		deleteReq := &service.DeleteSchedulerJobRequest{
			JobID: jobID,
			Force: true,
		}
		deleteResp, err := protocol.DeleteJob(ctx, deleteReq)
		require.NoError(t, err)
		assert.True(t, deleteResp.Deleted)

		t.Logf("[%s] Force deleted job: ID=%d", protocol.Name(), jobID)
	})
}

func testConcurrentOperations(t *testing.T, ctx context.Context, protocol schedulerProtocol, prefix string) {
	t.Helper()

	concurrency := 10
	var wg sync.WaitGroup
	errChan := make(chan error, concurrency)
	jobIDs := make(chan uint64, concurrency)

	// Create jobs concurrently
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			req := &service.CreateSchedulerJobRequest{
				Name:      fmt.Sprintf("%s-concurrent-%d", prefix, idx),
				Type:      service.SchedulerJobTypeOneShot,
				Handler:   fmt.Sprintf("concurrent-handler-%d", idx),
				ExecuteAt: time.Now().Add(time.Duration(idx+1) * time.Hour),
				Labels: map[string]string{
					"concurrent": "true",
					"index":      fmt.Sprintf("%d", idx),
				},
			}

			resp, err := protocol.CreateJob(ctx, req)
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
		deleteReq := &service.DeleteSchedulerJobRequest{JobID: jobID, Force: true}
		_, _ = protocol.DeleteJob(ctx, deleteReq)
	}

	assert.Equal(t, 0, errorCount, "All concurrent operations should succeed")
	t.Logf("[%s] Completed %d concurrent operations", protocol.Name(), concurrency)
}

func testErrorHandling(t *testing.T, ctx context.Context, protocol schedulerProtocol) {
	t.Helper()

	t.Run("get non-existent job", func(t *testing.T) {
		req := &service.GetSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.GetJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("update non-existent job", func(t *testing.T) {
		req := &service.UpdateSchedulerJobRequest{
			JobID:       999999999,
			Description: "Should fail",
		}
		_, err := protocol.UpdateJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("delete non-existent job", func(t *testing.T) {
		req := &service.DeleteSchedulerJobRequest{
			JobID: 999999999,
			Force: true,
		}
		_, err := protocol.DeleteJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("trigger non-existent job", func(t *testing.T) {
		req := &service.TriggerSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.TriggerJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("pause non-existent job", func(t *testing.T) {
		req := &service.PauseSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.PauseJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("resume non-existent job", func(t *testing.T) {
		req := &service.ResumeSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.ResumeJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("cancel non-existent job", func(t *testing.T) {
		req := &service.CancelSchedulerJobRequest{JobID: 999999999}
		_, err := protocol.CancelJob(ctx, req)
		assert.Error(t, err)
	})

	t.Run("get history for non-existent job", func(t *testing.T) {
		req := &service.GetSchedulerJobHistoryRequest{JobID: 999999999}
		_, err := protocol.GetJobHistory(ctx, req)
		assert.Error(t, err)
	})
}

// =============================================================================
// Protocol Implementations
// =============================================================================

// restProtocolClient implements schedulerProtocol using REST API.
type restProtocolClient struct {
	client *ServiceSchedulerRESTClient
}

func (c *restProtocolClient) Name() string { return "REST" }

func (c *restProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available for integration tests")
	c.client = NewServiceSchedulerRESTClient(t, endpoints[0])

	// Check if scheduler service is available
	_, err := c.client.GetStatus(ctx)
	require.NoError(t, err, "Scheduler service must be available via REST")

	return nil
}

func (c *restProtocolClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restProtocolClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return c.client.CreateJob(ctx, req)
}

func (c *restProtocolClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return c.client.GetJob(ctx, req)
}

func (c *restProtocolClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return c.client.UpdateJob(ctx, req)
}

func (c *restProtocolClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return c.client.DeleteJob(ctx, req)
}

func (c *restProtocolClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return c.client.ListJobs(ctx, req)
}

func (c *restProtocolClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return c.client.TriggerJob(ctx, req)
}

func (c *restProtocolClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return c.client.PauseJob(ctx, req)
}

func (c *restProtocolClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return c.client.ResumeJob(ctx, req)
}

func (c *restProtocolClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return c.client.CancelJob(ctx, req)
}

func (c *restProtocolClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return c.client.GetJobHistory(ctx, req)
}

func (c *restProtocolClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return c.client.GetStatus(ctx)
}

// grpcProtocolClient implements schedulerProtocol using gRPC.
type grpcProtocolClient struct {
	client *grpcclient.Client
}

func (c *grpcProtocolClient) Name() string { return "gRPC" }

func (c *grpcProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg))
	require.NoError(t, err, "Failed to connect to gRPC endpoint %s", endpoints[0])
	c.client = client

	// Check if scheduler service is available
	requireSchedulerAvailable(t, client, ctx)

	return nil
}

func (c *grpcProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	return nil
}

func (c *grpcProtocolClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return c.client.SchedulerCreateJob(ctx, req)
}

func (c *grpcProtocolClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return c.client.SchedulerGetJob(ctx, req)
}

func (c *grpcProtocolClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return c.client.SchedulerUpdateJob(ctx, req)
}

func (c *grpcProtocolClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return c.client.SchedulerDeleteJob(ctx, req)
}

func (c *grpcProtocolClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return c.client.SchedulerListJobs(ctx, req)
}

func (c *grpcProtocolClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return c.client.SchedulerTriggerJob(ctx, req)
}

func (c *grpcProtocolClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return c.client.SchedulerPauseJob(ctx, req)
}

func (c *grpcProtocolClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return c.client.SchedulerResumeJob(ctx, req)
}

func (c *grpcProtocolClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return c.client.SchedulerCancelJob(ctx, req)
}

func (c *grpcProtocolClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return c.client.SchedulerGetJobHistory(ctx, req)
}

func (c *grpcProtocolClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return c.client.SchedulerGetStatus(ctx)
}

type unixSocketProtocolClient struct {
	client *grpcclient.Client
	conn   *grpc.ClientConn
}

func (c *unixSocketProtocolClient) Name() string { return "UnixSocket" }

func (c *unixSocketProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check if socket exists
	_, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Unix socket must be available at %s for integration tests", socketPath)

	// Create gRPC connection over unix socket
	tlsCfgConn, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfgConn)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	require.NoError(t, err, "Failed to connect to unix socket at %s", socketPath)
	c.conn = conn

	tlsCfgClient, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(tlsCfgClient))
	require.NoError(t, err, "Failed to create gRPC client over unix socket at %s", socketPath)
	c.client = client

	// Check if scheduler is available
	requireSchedulerAvailable(t, client, ctx)

	return nil
}

func (c *unixSocketProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *unixSocketProtocolClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return c.client.SchedulerCreateJob(ctx, req)
}

func (c *unixSocketProtocolClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return c.client.SchedulerGetJob(ctx, req)
}

func (c *unixSocketProtocolClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return c.client.SchedulerUpdateJob(ctx, req)
}

func (c *unixSocketProtocolClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return c.client.SchedulerDeleteJob(ctx, req)
}

func (c *unixSocketProtocolClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return c.client.SchedulerListJobs(ctx, req)
}

func (c *unixSocketProtocolClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return c.client.SchedulerTriggerJob(ctx, req)
}

func (c *unixSocketProtocolClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return c.client.SchedulerPauseJob(ctx, req)
}

func (c *unixSocketProtocolClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return c.client.SchedulerResumeJob(ctx, req)
}

func (c *unixSocketProtocolClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return c.client.SchedulerCancelJob(ctx, req)
}

func (c *unixSocketProtocolClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return c.client.SchedulerGetJobHistory(ctx, req)
}

func (c *unixSocketProtocolClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return c.client.SchedulerGetStatus(ctx)
}

// =============================================================================
// Embedded Protocol Client
// =============================================================================

// embeddedProtocolClient implements schedulerProtocol using the embedded QuicBaaS client.
// This client uses an in-process service instance via the helpers.GetEmbeddedService function.
// It converts between service types and SDK scheduler types.
type embeddedProtocolClient struct {
	client quicbaas.Client
}

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
var ErrEmbeddedClientNotInitialized = errors.New("embedded client not initialized")

func (c *embeddedProtocolClient) Name() string { return "Embedded" }

func (c *embeddedProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return fmt.Errorf("failed to get embedded service: %w", err)
	}
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc))
	if err != nil {
		return fmt.Errorf("failed to create embedded client: %w", err)
	}
	c.client = client
	return nil
}

func (c *embeddedProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedProtocolClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	// Convert service request to SDK request
	sdkReq := &quicbaas.SchedulerCreateJobRequest{
		Name:        req.Name,
		Description: req.Description,
		JobType:     convertServiceJobTypeToSDK(req.Type),
		Schedule:    req.Schedule,
		Delay:       req.Delay,
		Payload:     req.Payload,
		OwnerType:   convertServiceOwnerTypeToSDK(req.OwnerType),
		OwnerID:     req.OwnerID,
		MaxRetries:  int(req.MaxRetries),
		Timeout:     req.Timeout,
		Tags:        req.Labels,
	}

	sdkResp, err := c.client.Scheduler().CreateJob(ctx, sdkReq)
	if err != nil {
		return nil, err
	}

	// Convert SDK response to service response
	return &service.CreateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:     sdkResp.JobID,
			Name:   req.Name,
			Type:   req.Type,
			Status: service.SchedulerJobStatusPending,
		},
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkJob, err := c.client.Scheduler().GetJob(ctx, req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.GetSchedulerJobResponse{
		Job:       convertSDKJobInfoToService(sdkJob),
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkReq := &quicbaas.SchedulerUpdateJobRequest{
		JobID:       req.JobID,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Payload:     req.Payload,
		MaxRetries:  int(req.MaxRetries),
		Timeout:     req.Timeout,
		Tags:        req.Labels,
	}

	sdkJob, err := c.client.Scheduler().UpdateJob(ctx, sdkReq)
	if err != nil {
		return nil, err
	}

	return &service.UpdateSchedulerJobResponse{
		Job:       convertSDKJobInfoToService(sdkJob),
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	err := c.client.Scheduler().DeleteJob(ctx, req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.DeleteSchedulerJobResponse{
		JobID:     req.JobID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkOpts := &quicbaas.SchedulerListJobsOptions{
		OwnerID: req.OwnerID,
		Tags:    req.Labels,
		Limit:   req.Limit,
	}

	if req.OwnerType != "" {
		ownerType := convertServiceOwnerTypeToSDK(req.OwnerType)
		sdkOpts.OwnerType = &ownerType
	}

	if req.Status != "" {
		status := convertServiceJobStatusToSDK(req.Status)
		sdkOpts.Status = &status
	}

	sdkResp, err := c.client.Scheduler().ListJobs(ctx, sdkOpts)
	if err != nil {
		return nil, err
	}

	jobs := make([]*service.SchedulerJob, 0, len(sdkResp.Jobs))
	for _, sdkJob := range sdkResp.Jobs {
		jobs = append(jobs, convertSDKJobInfoToService(&sdkJob))
	}

	return &service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		TotalCount: sdkResp.TotalCount,
		HasMore:    sdkResp.HasMore,
	}, nil
}

func (c *embeddedProtocolClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkResp, err := c.client.Scheduler().TriggerJob(ctx, req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.TriggerSchedulerJobResponse{
		JobID:     sdkResp.JobID,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkResp, err := c.client.Scheduler().PauseJob(ctx, req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.PauseSchedulerJobResponse{
		JobID:     sdkResp.JobID,
		NewStatus: service.SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkResp, err := c.client.Scheduler().ResumeJob(ctx, req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.ResumeSchedulerJobResponse{
		JobID:     sdkResp.JobID,
		OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusPending,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkResp, err := c.client.Scheduler().CancelJob(ctx, req.JobID, req.Reason)
	if err != nil {
		return nil, err
	}

	return &service.CancelSchedulerJobResponse{
		JobID:     sdkResp.JobID,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedProtocolClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkOpts := &quicbaas.SchedulerHistoryOptions{
		Limit: req.Limit,
	}

	sdkResp, err := c.client.Scheduler().GetJobHistory(ctx, req.JobID, sdkOpts)
	if err != nil {
		return nil, err
	}

	executions := make([]*service.SchedulerJobExecution, 0, len(sdkResp.Executions))
	for _, exec := range sdkResp.Executions {
		executions = append(executions, &service.SchedulerJobExecution{
			ID:        exec.ExecutionID,
			JobID:     exec.JobID,
			Status:    convertSDKJobStatusToService(exec.Status),
			StartedAt: exec.StartedAt,
			EndedAt:   exec.CompletedAt,
			Duration:  exec.Duration,
			Error:     exec.Error,
			Output:    string(exec.Result),
		})
	}

	return &service.GetSchedulerJobHistoryResponse{
		Executions: executions,
		HasMore:    sdkResp.HasMore,
	}, nil
}

func (c *embeddedProtocolClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}

	sdkStatus, err := c.client.Scheduler().GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &service.GetSchedulerStatusResponse{
		Running: sdkStatus.Healthy,
		Stats: &service.SchedulerStats{
			RunningJobs:  sdkStatus.RunningJobs,
			JobCount:     sdkStatus.TotalJobs,
			SuccessCount: sdkStatus.CompletedJobs,
			FailureCount: sdkStatus.FailedJobs,
		},
	}, nil
}

// =============================================================================
// Type Conversion Helpers for Embedded Client
// =============================================================================

// convertServiceJobTypeToSDK converts service job type to SDK job type.
func convertServiceJobTypeToSDK(serviceType service.SchedulerJobType) quicbaas.SchedulerJobType {
	switch serviceType {
	case service.SchedulerJobTypeOneShot:
		return quicbaas.SchedulerJobTypeOneShot
	case service.SchedulerJobTypeRecurring:
		return quicbaas.SchedulerJobTypeRecurring
	case service.SchedulerJobTypeDelayed:
		return quicbaas.SchedulerJobTypeDelayed
	default:
		return quicbaas.SchedulerJobTypeOneShot
	}
}

// convertServiceOwnerTypeToSDK converts service owner type to SDK owner type.
func convertServiceOwnerTypeToSDK(serviceType service.SchedulerOwnerType) quicbaas.SchedulerOwnerType {
	switch serviceType {
	case service.SchedulerOwnerTypeSystem:
		return quicbaas.SchedulerOwnerTypeSystem
	case service.SchedulerOwnerTypeTenant:
		return quicbaas.SchedulerOwnerTypeTenant
	case service.SchedulerOwnerTypeDelegated:
		return quicbaas.SchedulerOwnerTypeDelegated
	default:
		return quicbaas.SchedulerOwnerTypeSystem
	}
}

// convertServiceJobStatusToSDK converts service job status to SDK job status.
func convertServiceJobStatusToSDK(serviceStatus service.SchedulerJobStatus) quicbaas.SchedulerJobStatus {
	switch serviceStatus {
	case service.SchedulerJobStatusPending:
		return quicbaas.SchedulerJobStatusPending
	case service.SchedulerJobStatusRunning:
		return quicbaas.SchedulerJobStatusRunning
	case service.SchedulerJobStatusCompleted:
		return quicbaas.SchedulerJobStatusCompleted
	case service.SchedulerJobStatusFailed:
		return quicbaas.SchedulerJobStatusFailed
	case service.SchedulerJobStatusPaused:
		return quicbaas.SchedulerJobStatusPaused
	case service.SchedulerJobStatusCanceled:
		return quicbaas.SchedulerJobStatusCanceled
	default:
		return quicbaas.SchedulerJobStatusPending
	}
}

// convertSDKJobStatusToService converts SDK job status to service job status.
func convertSDKJobStatusToService(sdkStatus quicbaas.SchedulerJobStatus) service.SchedulerJobStatus {
	switch sdkStatus {
	case quicbaas.SchedulerJobStatusPending:
		return service.SchedulerJobStatusPending
	case quicbaas.SchedulerJobStatusRunning:
		return service.SchedulerJobStatusRunning
	case quicbaas.SchedulerJobStatusCompleted:
		return service.SchedulerJobStatusCompleted
	case quicbaas.SchedulerJobStatusFailed:
		return service.SchedulerJobStatusFailed
	case quicbaas.SchedulerJobStatusPaused:
		return service.SchedulerJobStatusPaused
	case quicbaas.SchedulerJobStatusCanceled:
		return service.SchedulerJobStatusCanceled
	default:
		return service.SchedulerJobStatusPending
	}
}

// convertSDKJobTypeToService converts SDK job type to service job type.
func convertSDKJobTypeToService(sdkType quicbaas.SchedulerJobType) service.SchedulerJobType {
	switch sdkType {
	case quicbaas.SchedulerJobTypeOneShot:
		return service.SchedulerJobTypeOneShot
	case quicbaas.SchedulerJobTypeRecurring:
		return service.SchedulerJobTypeRecurring
	case quicbaas.SchedulerJobTypeDelayed:
		return service.SchedulerJobTypeDelayed
	default:
		return service.SchedulerJobTypeOneShot
	}
}

// convertSDKJobInfoToService converts SDK JobInfo to service SchedulerJob.
func convertSDKJobInfoToService(sdkJob *quicbaas.SchedulerJobInfo) *service.SchedulerJob {
	if sdkJob == nil {
		return nil
	}

	return &service.SchedulerJob{
		ID:          sdkJob.JobID,
		Name:        sdkJob.Name,
		Description: sdkJob.Description,
		Type:        convertSDKJobTypeToService(sdkJob.JobType),
		Status:      convertSDKJobStatusToService(sdkJob.Status),
		Schedule:    sdkJob.Schedule,
		Handler:     "", // SDK doesn't expose Handler directly
		Payload:     sdkJob.Payload,
		Priority:    0, // SDK doesn't expose Priority
		Timeout:     sdkJob.Timeout,
		MaxRetries:  int32(sdkJob.MaxRetries),
		Labels:      sdkJob.Tags,
		NextRun:     sdkJob.NextRunAt,
		LastRun:     sdkJob.LastRunAt,
		CreatedAt:   sdkJob.CreatedAt,
		UpdatedAt:   sdkJob.UpdatedAt,
		RunCount:    sdkJob.ExecutionCount,
		FailCount:   sdkJob.FailureCount,
	}
}

// =============================================================================
// Extended REST Client for Scheduler
// =============================================================================

// schedulerJobRESTResponse is a local type to handle REST JSON responses.
// The REST API returns Payload as json.RawMessage (arbitrary JSON object),
// but service.SchedulerJob expects []byte. This type allows proper unmarshaling.
type schedulerJobRESTResponse struct {
	ID             uint64            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	OwnerType      string            `json:"owner_type"`
	OwnerID        string            `json:"owner_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	CreatedBy      string            `json:"created_by,omitempty"`
	Type           string            `json:"type"`
	Schedule       string            `json:"schedule,omitempty"`
	ExecuteAt      time.Time         `json:"execute_at,omitempty"`
	Delay          time.Duration     `json:"delay,omitempty"`
	Handler        string            `json:"handler"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Priority       int32             `json:"priority"`
	Timeout        time.Duration     `json:"timeout"`
	MaxRetries     int32             `json:"max_retries"`
	Status         string            `json:"status"`
	LastRun        time.Time         `json:"last_run,omitempty"`
	NextRun        time.Time         `json:"next_run,omitempty"`
	RunCount       uint64            `json:"run_count"`
	FailCount      uint64            `json:"fail_count"`
	LastError      string            `json:"last_error,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// listSchedulerJobsRESTResponse is a local type for the list jobs REST response.
type listSchedulerJobsRESTResponse struct {
	Jobs       []*schedulerJobRESTResponse `json:"jobs"`
	NextCursor string                      `json:"next_cursor,omitempty"`
	HasMore    bool                        `json:"has_more"`
	TotalCount int                         `json:"total_count,omitempty"`
	Timestamp  time.Time                   `json:"timestamp"`
}

// getSchedulerJobRESTResponse is a local type for the get job REST response.
type getSchedulerJobRESTResponse struct {
	Job       *schedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// createSchedulerJobRESTResponse is a local type for the create job REST response.
type createSchedulerJobRESTResponse struct {
	Job       *schedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// updateSchedulerJobRESTResponse is a local type for the update job REST response.
type updateSchedulerJobRESTResponse struct {
	Job       *schedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// convertRESTJobToService converts a REST job response to a service SchedulerJob.
func convertRESTJobToService(restJob *schedulerJobRESTResponse) *service.SchedulerJob {
	if restJob == nil {
		return nil
	}
	return &service.SchedulerJob{
		ID:             restJob.ID,
		Name:           restJob.Name,
		Description:    restJob.Description,
		OwnerType:      service.SchedulerOwnerType(restJob.OwnerType),
		OwnerID:        restJob.OwnerID,
		TargetTenantID: restJob.TargetTenantID,
		CreatedBy:      restJob.CreatedBy,
		Type:           service.SchedulerJobType(restJob.Type),
		Schedule:       restJob.Schedule,
		ExecuteAt:      restJob.ExecuteAt,
		Delay:          restJob.Delay,
		Handler:        restJob.Handler,
		Payload:        []byte(restJob.Payload), // json.RawMessage is []byte
		Priority:       restJob.Priority,
		Timeout:        restJob.Timeout,
		MaxRetries:     restJob.MaxRetries,
		Status:         service.SchedulerJobStatus(restJob.Status),
		LastRun:        restJob.LastRun,
		NextRun:        restJob.NextRun,
		RunCount:       restJob.RunCount,
		FailCount:      restJob.FailCount,
		LastError:      restJob.LastError,
		Labels:         restJob.Labels,
		CreatedAt:      restJob.CreatedAt,
		UpdatedAt:      restJob.UpdatedAt,
	}
}

// ServiceSchedulerRESTClient extends the REST client with all scheduler operations.
type ServiceSchedulerRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewServiceSchedulerRESTClient creates a new scheduler REST client that acts
// as the tenant operator, as NewSchedulerRESTClient does.
func NewServiceSchedulerRESTClient(t *testing.T, baseURL string) *ServiceSchedulerRESTClient {
	t.Helper()
	return &ServiceSchedulerRESTClient{
		baseURL:    baseURL,
		httpClient: helpers.NewTenantOperatorHTTPClient(t, 30*time.Second),
	}
}

func (c *ServiceSchedulerRESTClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/scheduler/jobs", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Use REST response type to handle json.RawMessage Payload field
	var restResult createSchedulerJobRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return &service.CreateSchedulerJobResponse{
		Job:       convertRESTJobToService(restResult.Job),
		Timestamp: restResult.Timestamp,
	}, nil
}

func (c *ServiceSchedulerRESTClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", c.baseURL, req.JobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Use REST response type to handle json.RawMessage Payload field
	var restResult getSchedulerJobRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return &service.GetSchedulerJobResponse{
		Job:       convertRESTJobToService(restResult.Job),
		Timestamp: restResult.Timestamp,
	}, nil
}

func (c *ServiceSchedulerRESTClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", c.baseURL, req.JobID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Use REST response type to handle json.RawMessage Payload field
	var restResult updateSchedulerJobRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return &service.UpdateSchedulerJobResponse{
		Job:       convertRESTJobToService(restResult.Job),
		Timestamp: restResult.Timestamp,
	}, nil
}

func (c *ServiceSchedulerRESTClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", c.baseURL, req.JobID)
	if req.Force {
		url += "?force=true"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.DeleteSchedulerJobResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, err
		}
	}
	result.Deleted = true

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs", c.baseURL)

	params := []string{}
	if req.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", req.Limit))
	}
	if req.Cursor != "" {
		params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
	}
	if req.Status != "" {
		params = append(params, fmt.Sprintf("status=%s", req.Status))
	}
	if req.Handler != "" {
		params = append(params, fmt.Sprintf("handler=%s", req.Handler))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Use REST response type to handle json.RawMessage Payload field
	var restResult listSchedulerJobsRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert REST response to service response
	jobs := make([]*service.SchedulerJob, 0, len(restResult.Jobs))
	for _, restJob := range restResult.Jobs {
		jobs = append(jobs, convertRESTJobToService(restJob))
	}

	return &service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		NextCursor: restResult.NextCursor,
		HasMore:    restResult.HasMore,
		TotalCount: restResult.TotalCount,
		Timestamp:  restResult.Timestamp,
	}, nil
}

func (c *ServiceSchedulerRESTClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/trigger", c.baseURL, req.JobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.TriggerSchedulerJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/pause", c.baseURL, req.JobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.PauseSchedulerJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/resume", c.baseURL, req.JobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ResumeSchedulerJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/cancel", c.baseURL, req.JobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.CancelSchedulerJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/history", c.baseURL, req.JobID)

	params := []string{}
	if req.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", req.Limit))
	}
	if req.Cursor != "" {
		params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetSchedulerJobHistoryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ServiceSchedulerRESTClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/status", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetSchedulerStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// requireSchedulerAvailable checks if the scheduler service is available and fails the test if not.
func requireSchedulerAvailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()
	_, err := client.SchedulerGetStatus(ctx)
	require.NoError(t, err, "Scheduler service must be available")
}

// Ensure scheduler package types are available
var (
	_ = scheduler.JobTypeOneShot
	_ = scheduler.JobTypeRecurring
	_ = scheduler.JobTypeDelayed
)
