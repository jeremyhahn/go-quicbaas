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

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// MockSchedulerClient is a mock implementation of SchedulerClient for testing.
type MockSchedulerClient struct {
	SchedulerCreateJobFunc     func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	SchedulerGetJobFunc        func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	SchedulerUpdateJobFunc     func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	SchedulerDeleteJobFunc     func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	SchedulerListJobsFunc      func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	SchedulerTriggerJobFunc    func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	SchedulerPauseJobFunc      func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	SchedulerResumeJobFunc     func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	SchedulerCancelJobFunc     func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	SchedulerGetJobHistoryFunc func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	SchedulerGetStatusFunc     func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
	CloseFunc                  func() error
}

func (m *MockSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if m.SchedulerCreateJobFunc != nil {
		return m.SchedulerCreateJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.create", "not implemented")
}

func (m *MockSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.SchedulerGetJobFunc != nil {
		return m.SchedulerGetJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.get", "not implemented")
}

func (m *MockSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.SchedulerUpdateJobFunc != nil {
		return m.SchedulerUpdateJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.update", "not implemented")
}

func (m *MockSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.SchedulerDeleteJobFunc != nil {
		return m.SchedulerDeleteJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.delete", "not implemented")
}

func (m *MockSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.SchedulerListJobsFunc != nil {
		return m.SchedulerListJobsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.list", "not implemented")
}

func (m *MockSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.SchedulerTriggerJobFunc != nil {
		return m.SchedulerTriggerJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.trigger", "not implemented")
}

func (m *MockSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.SchedulerPauseJobFunc != nil {
		return m.SchedulerPauseJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.pause", "not implemented")
}

func (m *MockSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.SchedulerResumeJobFunc != nil {
		return m.SchedulerResumeJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.resume", "not implemented")
}

func (m *MockSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.SchedulerCancelJobFunc != nil {
		return m.SchedulerCancelJobFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.cancel", "not implemented")
}

func (m *MockSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.SchedulerGetJobHistoryFunc != nil {
		return m.SchedulerGetJobHistoryFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.history", "not implemented")
}

func (m *MockSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.SchedulerGetStatusFunc != nil {
		return m.SchedulerGetStatusFunc(ctx)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "scheduler.status", "not implemented")
}

func (m *MockSchedulerClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Compile-time assertion that MockSchedulerClient implements SchedulerClient.
var _ SchedulerClient = (*MockSchedulerClient)(nil)

// =============================================================================
// Scheduler Command Structure Tests
// =============================================================================

func TestSchedulerCommandStructure(t *testing.T) {
	cmd := newSchedulerCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "scheduler", cmd.Use)
	assert.Equal(t, "Job scheduler and workload placement commands", cmd.Short)
	assert.Contains(t, cmd.Long, "Scheduler commands")
	assert.Greater(t, len(cmd.Commands()), 0, "Scheduler command should have subcommands")

	// Check persistent flags
	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	// Check that scheduler has its job, status, node and placement subcommands
	expectedCommands := []string{"job", "status", "node", "placement"}
	for _, expectedCmd := range expectedCommands {
		found := false
		for _, subCmd := range cmd.Commands() {
			if strings.HasPrefix(subCmd.Use, expectedCmd) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find subcommand '%s'", expectedCmd)
	}

	// Verify job subcommand exists and has expected sub-subcommands
	var jobCmd *cobra.Command
	for _, subCmd := range cmd.Commands() {
		if strings.HasPrefix(subCmd.Use, "job") {
			jobCmd = subCmd
			break
		}
	}
	assert.NotNil(t, jobCmd, "Expected to find 'job' subcommand")

	// Check all job subcommands exist under scheduler job
	jobSubcommands := []string{"create", "list", "get", "delete", "trigger", "pause", "resume", "cancel", "history"}
	for _, expectedCmd := range jobSubcommands {
		found := false
		for _, subCmd := range jobCmd.Commands() {
			if strings.HasPrefix(subCmd.Use, expectedCmd) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find job subcommand '%s'", expectedCmd)
	}
}

// =============================================================================
// Scheduler Job Create Command Tests
// =============================================================================

func TestSchedulerJobCreateCommandFlags(t *testing.T) {
	cmd := newSchedulerCreateCommand()

	// Check flag existence and defaults
	typeFlag := cmd.Flags().Lookup("type")
	assert.NotNil(t, typeFlag)
	assert.Equal(t, "one-shot", typeFlag.DefValue)

	descriptionFlag := cmd.Flags().Lookup("description")
	assert.NotNil(t, descriptionFlag)

	scheduleFlag := cmd.Flags().Lookup("schedule")
	assert.NotNil(t, scheduleFlag)

	executeAtFlag := cmd.Flags().Lookup("execute-at")
	assert.NotNil(t, executeAtFlag)

	delayFlag := cmd.Flags().Lookup("delay")
	assert.NotNil(t, delayFlag)

	handlerFlag := cmd.Flags().Lookup("handler")
	assert.NotNil(t, handlerFlag)

	payloadFlag := cmd.Flags().Lookup("payload")
	assert.NotNil(t, payloadFlag)

	priorityFlag := cmd.Flags().Lookup("priority")
	assert.NotNil(t, priorityFlag)
	assert.Equal(t, "0", priorityFlag.DefValue)

	timeoutFlag := cmd.Flags().Lookup("timeout")
	assert.NotNil(t, timeoutFlag)
	assert.Equal(t, "5m", timeoutFlag.DefValue)

	maxRetriesFlag := cmd.Flags().Lookup("max-retries")
	assert.NotNil(t, maxRetriesFlag)
	assert.Equal(t, "3", maxRetriesFlag.DefValue)
}

func TestRunSchedulerJobCreateCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		jobType         string
		handler         string
		schedule        string
		executeAt       string
		delay           string
		timeout         string
		description     string
		payload         string
		priority        int32
		maxRetries      int32
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.CreateSchedulerJobRequest)
	}{
		{
			name:         "create one-shot job successfully",
			args:         []string{"test-job"},
			jobType:      "one-shot",
			handler:      "backup.full",
			executeAt:    "2025-01-20T00:00:00Z",
			timeout:      "5m",
			maxRetries:   3,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CreateSchedulerJobRequest) {
				assert.Equal(t, "test-job", req.Name)
				assert.Equal(t, service.SchedulerJobType("one-shot"), req.Type)
				assert.Equal(t, "backup.full", req.Handler)
				assert.Equal(t, service.SchedulerOwnerTypeSystem, req.OwnerType)
			},
		},
		{
			name:         "create recurring job with schedule",
			args:         []string{"backup-daily"},
			jobType:      "recurring",
			handler:      "backup.full",
			schedule:     "0 0 * * *",
			timeout:      "10m",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CreateSchedulerJobRequest) {
				assert.Equal(t, "backup-daily", req.Name)
				assert.Equal(t, service.SchedulerJobType("recurring"), req.Type)
				assert.Equal(t, "0 0 * * *", req.Schedule)
			},
		},
		{
			name:         "create delayed job",
			args:         []string{"delayed-cleanup"},
			jobType:      "delayed",
			handler:      "audit.cleanup",
			delay:        "24h",
			timeout:      "1h",
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CreateSchedulerJobRequest) {
				assert.Equal(t, "delayed-cleanup", req.Name)
				assert.Equal(t, service.SchedulerJobType("delayed"), req.Type)
				assert.Equal(t, 24*time.Hour, req.Delay)
			},
		},
		{
			name:        "create job with tenant",
			args:        []string{"tenant-job"},
			jobType:     "one-shot",
			handler:     "tenant.operation",
			executeAt:   "2025-01-21T12:00:00Z",
			tenantID:    "acme",
			timeout:     "5m",
			expectError: false,
			validateRequest: func(t *testing.T, req *service.CreateSchedulerJobRequest) {
				assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
				assert.Equal(t, "acme", req.OwnerID)
			},
		},
		{
			name:         "create job with all options",
			args:         []string{"full-options-job"},
			jobType:      "one-shot",
			handler:      "kms.grace_period",
			executeAt:    "2025-01-22T00:00:00Z",
			description:  "Full options test job",
			payload:      `{"key_id":"key-123"}`,
			priority:     10,
			maxRetries:   5,
			timeout:      "30m",
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CreateSchedulerJobRequest) {
				assert.Equal(t, "Full options test job", req.Description)
				assert.Equal(t, `{"key_id":"key-123"}`, string(req.Payload))
				assert.Equal(t, int32(10), req.Priority)
				assert.Equal(t, int32(5), req.MaxRetries)
			},
		},
		{
			name:          "invalid timeout format",
			args:          []string{"bad-timeout-job"},
			jobType:       "one-shot",
			handler:       "test.handler",
			executeAt:     "2025-01-20T00:00:00Z",
			timeout:       "invalid",
			expectError:   true,
			errorContains: "invalid timeout format",
		},
		{
			name:          "invalid execute-at format",
			args:          []string{"bad-execute-at-job"},
			jobType:       "one-shot",
			handler:       "test.handler",
			executeAt:     "not-a-timestamp",
			timeout:       "5m",
			expectError:   true,
			errorContains: "invalid execute-at format",
		},
		{
			name:          "invalid delay format",
			args:          []string{"bad-delay-job"},
			jobType:       "delayed",
			handler:       "test.handler",
			delay:         "invalid-delay",
			timeout:       "5m",
			expectError:   true,
			errorContains: "invalid delay format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.CreateSchedulerJobRequest

			// Set up mock for successful cases
			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerCreateJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.CreateSchedulerJobResponse{
								Job: &service.SchedulerJob{
									ID:        1,
									Name:      req.Name,
									Type:      req.Type,
									Handler:   req.Handler,
									Status:    service.SchedulerJobStatusPending,
									CreatedAt: time.Now(),
								},
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerCreateCommand()

			// Set flags
			schedulerType = tt.jobType
			schedulerHandler = tt.handler
			schedulerSchedule = tt.schedule
			schedulerExecuteAt = tt.executeAt
			schedulerDelay = tt.delay
			schedulerTimeout = tt.timeout
			schedulerDescription = tt.description
			schedulerPayload = tt.payload
			schedulerPriority = tt.priority
			schedulerMaxRetries = tt.maxRetries
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

func TestRunSchedulerJobCreateCommandClientError(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	// Set up mock to return error
	schedulerClientFactory = func() (SchedulerClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "failed to create client")
	}

	cmd := newSchedulerCreateCommand()
	schedulerType = "one-shot"
	schedulerHandler = "test.handler"
	schedulerExecuteAt = "2025-01-20T00:00:00Z"
	schedulerTimeout = "5m"

	err := cmd.RunE(cmd, []string{"test-job"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create client")
}

func TestRunSchedulerJobCreateCommandAPIError(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	// Set up mock to return API error
	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerCreateJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrInvalidInput, "scheduler.create", "handler not found")
			},
		}, nil
	}

	cmd := newSchedulerCreateCommand()
	schedulerType = "one-shot"
	schedulerHandler = "invalid.handler"
	schedulerExecuteAt = "2025-01-20T00:00:00Z"
	schedulerTimeout = "5m"

	err := cmd.RunE(cmd, []string{"test-job"})
	assert.Error(t, err)
}

// =============================================================================
// Scheduler Job List Command Tests
// =============================================================================

func TestSchedulerJobListCommandFlags(t *testing.T) {
	cmd := newSchedulerListCommand()

	statusFlag := cmd.Flags().Lookup("status")
	assert.NotNil(t, statusFlag)

	handlerFlag := cmd.Flags().Lookup("handler")
	assert.NotNil(t, handlerFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)

	cursorFlag := cmd.Flags().Lookup("cursor")
	assert.NotNil(t, cursorFlag)
}

func TestRunSchedulerJobListCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		status          string
		handler         string
		limit           int
		cursor          string
		tenantID        string
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *service.ListSchedulerJobsRequest)
	}{
		{
			name:         "list all jobs",
			limit:        100,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ListSchedulerJobsRequest) {
				assert.Equal(t, 100, req.Limit)
			},
		},
		{
			name:         "list jobs by status",
			status:       "pending",
			limit:        50,
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ListSchedulerJobsRequest) {
				assert.Equal(t, service.SchedulerJobStatus("pending"), req.Status)
			},
		},
		{
			name:         "list jobs by handler",
			handler:      "backup.full",
			limit:        100,
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ListSchedulerJobsRequest) {
				assert.Equal(t, "backup.full", req.Handler)
			},
		},
		{
			name:         "list jobs with pagination",
			limit:        20,
			cursor:       "cursor-abc123",
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ListSchedulerJobsRequest) {
				assert.Equal(t, 20, req.Limit)
				assert.Equal(t, "cursor-abc123", req.Cursor)
			},
		},
		{
			name:         "list jobs for tenant",
			tenantID:     "acme",
			limit:        100,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ListSchedulerJobsRequest) {
				assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
				assert.Equal(t, "acme", req.OwnerID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.ListSchedulerJobsRequest

			schedulerClientFactory = func() (SchedulerClient, error) {
				return &MockSchedulerClient{
					SchedulerListJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
						capturedRequest = req
						return &service.ListSchedulerJobsResponse{
							Jobs: []*service.SchedulerJob{
								{
									ID:      1,
									Name:    "job-1",
									Type:    service.SchedulerJobTypeRecurring,
									Handler: "backup.full",
									Status:  service.SchedulerJobStatusPending,
									NextRun: time.Now().Add(time.Hour),
								},
								{
									ID:      2,
									Name:    "job-2",
									Type:    service.SchedulerJobTypeOneShot,
									Handler: "kms.rotate",
									Status:  service.SchedulerJobStatusCompleted,
								},
							},
							TotalCount: 2,
							HasMore:    false,
							Timestamp:  time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newSchedulerListCommand()

			// Set flags
			schedulerStatus = tt.status
			schedulerHandler = tt.handler
			schedulerLimit = tt.limit
			schedulerCursor = tt.cursor
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Get Command Tests
// =============================================================================

func TestSchedulerJobGetCommand(t *testing.T) {
	cmd := newSchedulerGetCommand()

	assert.Equal(t, "get <job-id>", cmd.Use)
	assert.Equal(t, "Get job details", cmd.Short)
}

func TestRunSchedulerJobGetCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.GetSchedulerJobRequest)
	}{
		{
			name:         "get job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
			},
		},
		{
			name:         "get job with tenant",
			args:         []string{"456"},
			tenantID:     "acme",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"not-a-number"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.GetSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerGetJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.GetSchedulerJobResponse{
								Job: &service.SchedulerJob{
									ID:          req.JobID,
									Name:        "test-job",
									Description: "Test job description",
									Type:        service.SchedulerJobTypeRecurring,
									Handler:     "backup.full",
									Schedule:    "0 0 * * *",
									Status:      service.SchedulerJobStatusPending,
									Priority:    5,
									MaxRetries:  3,
									RunCount:    10,
									FailCount:   1,
									LastRun:     time.Now().Add(-time.Hour),
									NextRun:     time.Now().Add(time.Hour),
									CreatedAt:   time.Now().Add(-24 * time.Hour),
								},
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerGetCommand()
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Delete Command Tests
// =============================================================================

func TestSchedulerJobDeleteCommandFlags(t *testing.T) {
	cmd := newSchedulerDeleteCommand()

	forceFlag := cmd.Flags().Lookup("force")
	assert.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestRunSchedulerJobDeleteCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		force           bool
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.DeleteSchedulerJobRequest)
	}{
		{
			name:         "delete job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.DeleteSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
				assert.False(t, req.Force)
			},
		},
		{
			name:         "delete job with force",
			args:         []string{"456"},
			force:        true,
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.DeleteSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.True(t, req.Force)
			},
		},
		{
			name:         "delete job with tenant",
			args:         []string{"789"},
			tenantID:     "acme",
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.DeleteSchedulerJobRequest) {
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.DeleteSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerDeleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.DeleteSchedulerJobResponse{
								JobID:     req.JobID,
								Deleted:   true,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerDeleteCommand()
			schedulerForce = tt.force
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Trigger Command Tests
// =============================================================================

func TestSchedulerJobTriggerCommand(t *testing.T) {
	cmd := newSchedulerTriggerCommand()

	assert.Equal(t, "trigger <job-id>", cmd.Use)
	assert.Equal(t, "Trigger immediate job execution", cmd.Short)
}

func TestRunSchedulerJobTriggerCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.TriggerSchedulerJobRequest)
	}{
		{
			name:         "trigger job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.TriggerSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
			},
		},
		{
			name:         "trigger job with tenant",
			args:         []string{"456"},
			tenantID:     "acme",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.TriggerSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.TriggerSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerTriggerJobFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.TriggerSchedulerJobResponse{
								JobID:     req.JobID,
								Status:    service.SchedulerJobStatusRunning,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerTriggerCommand()
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Pause Command Tests
// =============================================================================

func TestSchedulerJobPauseCommand(t *testing.T) {
	cmd := newSchedulerPauseCommand()

	assert.Equal(t, "pause <job-id>", cmd.Use)
	assert.Equal(t, "Pause a scheduled job", cmd.Short)
}

func TestRunSchedulerJobPauseCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.PauseSchedulerJobRequest)
	}{
		{
			name:         "pause job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.PauseSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
			},
		},
		{
			name:         "pause job with tenant",
			args:         []string{"456"},
			tenantID:     "acme",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.PauseSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.PauseSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerPauseJobFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.PauseSchedulerJobResponse{
								JobID:     req.JobID,
								OldStatus: service.SchedulerJobStatusPending,
								NewStatus: service.SchedulerJobStatusPaused,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerPauseCommand()
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Resume Command Tests
// =============================================================================

func TestSchedulerJobResumeCommand(t *testing.T) {
	cmd := newSchedulerResumeCommand()

	assert.Equal(t, "resume <job-id>", cmd.Use)
	assert.Equal(t, "Resume a paused job", cmd.Short)
}

func TestRunSchedulerJobResumeCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.ResumeSchedulerJobRequest)
	}{
		{
			name:         "resume job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ResumeSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
			},
		},
		{
			name:         "resume job with tenant",
			args:         []string{"456"},
			tenantID:     "acme",
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.ResumeSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.ResumeSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerResumeJobFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.ResumeSchedulerJobResponse{
								JobID:     req.JobID,
								OldStatus: service.SchedulerJobStatusPaused,
								NewStatus: service.SchedulerJobStatusPending,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerResumeCommand()
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job Cancel Command Tests
// =============================================================================

func TestSchedulerJobCancelCommandFlags(t *testing.T) {
	cmd := newSchedulerCancelCommand()

	reasonFlag := cmd.Flags().Lookup("reason")
	assert.NotNil(t, reasonFlag)
}

func TestRunSchedulerJobCancelCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		reason          string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.CancelSchedulerJobRequest)
	}{
		{
			name:         "cancel job successfully",
			args:         []string{"123"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CancelSchedulerJobRequest) {
				assert.Equal(t, uint64(123), req.JobID)
			},
		},
		{
			name:         "cancel job with reason",
			args:         []string{"456"},
			reason:       "No longer needed",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CancelSchedulerJobRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, "No longer needed", req.Reason)
			},
		},
		{
			name:         "cancel job with tenant",
			args:         []string{"789"},
			tenantID:     "acme",
			reason:       "Tenant deactivated",
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.CancelSchedulerJobRequest) {
				assert.Equal(t, "acme", req.TenantID)
				assert.Equal(t, "Tenant deactivated", req.Reason)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.CancelSchedulerJobRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerCancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
							capturedRequest = req
							return &service.CancelSchedulerJobResponse{
								JobID:     req.JobID,
								OldStatus: service.SchedulerJobStatusPending,
								NewStatus: service.SchedulerJobStatusCanceled,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerCancelCommand()
			schedulerReason = tt.reason
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Job History Command Tests
// =============================================================================

func TestSchedulerJobHistoryCommandFlags(t *testing.T) {
	cmd := newSchedulerHistoryCommand()

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "50", limitFlag.DefValue)

	cursorFlag := cmd.Flags().Lookup("cursor")
	assert.NotNil(t, cursorFlag)
}

func TestRunSchedulerJobHistoryCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		limit           int
		cursor          string
		tenantID        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *service.GetSchedulerJobHistoryRequest)
	}{
		{
			name:         "get job history successfully",
			args:         []string{"123"},
			limit:        50,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobHistoryRequest) {
				assert.Equal(t, uint64(123), req.JobID)
				assert.Equal(t, 50, req.Limit)
			},
		},
		{
			name:         "get job history with custom limit",
			args:         []string{"456"},
			limit:        20,
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobHistoryRequest) {
				assert.Equal(t, uint64(456), req.JobID)
				assert.Equal(t, 20, req.Limit)
			},
		},
		{
			name:         "get job history with pagination",
			args:         []string{"789"},
			limit:        10,
			cursor:       "cursor-xyz",
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobHistoryRequest) {
				assert.Equal(t, "cursor-xyz", req.Cursor)
			},
		},
		{
			name:         "get job history with tenant",
			args:         []string{"123"},
			tenantID:     "acme",
			limit:        50,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *service.GetSchedulerJobHistoryRequest) {
				assert.Equal(t, "acme", req.TenantID)
			},
		},
		{
			name:          "invalid job ID format",
			args:          []string{"invalid"},
			expectError:   true,
			errorContains: "invalid job ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *service.GetSchedulerJobHistoryRequest

			if !tt.expectError {
				schedulerClientFactory = func() (SchedulerClient, error) {
					return &MockSchedulerClient{
						SchedulerGetJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
							capturedRequest = req
							return &service.GetSchedulerJobHistoryResponse{
								Executions: []*service.SchedulerJobExecution{
									{
										ID:        1,
										JobID:     req.JobID,
										Status:    service.SchedulerJobStatusCompleted,
										StartedAt: time.Now().Add(-2 * time.Hour),
										EndedAt:   time.Now().Add(-time.Hour - 55*time.Minute),
										Duration:  5 * time.Minute,
									},
									{
										ID:        2,
										JobID:     req.JobID,
										Status:    service.SchedulerJobStatusFailed,
										StartedAt: time.Now().Add(-time.Hour),
										EndedAt:   time.Now().Add(-55 * time.Minute),
										Duration:  5 * time.Minute,
										Error:     "Connection timeout",
									},
								},
								HasMore:   false,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newSchedulerHistoryCommand()
			schedulerLimit = tt.limit
			schedulerCursor = tt.cursor
			globalTenantID = tt.tenantID
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Scheduler Status Command Tests
// =============================================================================

func TestSchedulerStatusCommand(t *testing.T) {
	cmd := newSchedulerStatusCommand()

	assert.Equal(t, "status", cmd.Use)
	assert.Equal(t, "Show scheduler status", cmd.Short)
}

func TestRunSchedulerStatusCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	tests := []struct {
		name         string
		outputFormat string
		running      bool
		expectError  bool
	}{
		{
			name:         "status running table format",
			outputFormat: "table",
			running:      true,
			expectError:  false,
		},
		{
			name:         "status stopped json format",
			outputFormat: "json",
			running:      false,
			expectError:  false,
		},
		{
			name:         "status yaml format",
			outputFormat: "yaml",
			running:      true,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedulerClientFactory = func() (SchedulerClient, error) {
				return &MockSchedulerClient{
					SchedulerGetStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
						return &service.GetSchedulerStatusResponse{
							Running: tt.running,
							Stats: &service.SchedulerStats{
								JobCount:     25,
								RunningJobs:  3,
								SuccessCount: 150,
								FailureCount: 5,
								StartedAt:    time.Now().Add(-24 * time.Hour),
								LastTickTime: time.Now().Add(-time.Minute),
							},
							Timestamp: time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newSchedulerStatusCommand()
			schedulerOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunSchedulerStatusCommandError(t *testing.T) {
	// Save and restore factory
	originalFactory := schedulerClientFactory
	defer func() { schedulerClientFactory = originalFactory }()

	// Client creation error
	schedulerClientFactory = func() (SchedulerClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newSchedulerStatusCommand()
	schedulerOutputFmt = "table"

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

// =============================================================================
// Output Formatting Tests
// =============================================================================

func TestFormatSchedulerJobResponse(t *testing.T) {
	job := &service.SchedulerJob{
		ID:          1,
		Name:        "test-job",
		Description: "Test job description",
		Type:        service.SchedulerJobTypeRecurring,
		Handler:     "backup.full",
		Schedule:    "0 0 * * *",
		Status:      service.SchedulerJobStatusPending,
		Priority:    5,
		MaxRetries:  3,
		RunCount:    10,
		FailCount:   1,
		LastError:   "Previous error",
		LastRun:     time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC),
		NextRun:     time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"ID:", "1",
				"Name:", "test-job",
				"Description:", "Test job description",
				"Type:", "recurring",
				"Handler:", "backup.full",
				"Status:", "pending",
				"Schedule:", "0 0 * * *",
				"Run Count:", "10",
				"Fail Count:", "1",
				"Last Error:", "Previous error",
				"Priority:", "5",
				"Max Retries:", "3",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"id": 1`,
				`"name": "test-job"`,
				`"type": "recurring"`,
				`"handler": "backup.full"`,
			},
		},
		{
			name:   "yaml format",
			format: "yaml",
			expectedText: []string{
				"id: 1",
				"name: test-job",
				"type: recurring",
				"handler: backup.full",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobResponse(job, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatSchedulerJobResponseMinimal(t *testing.T) {
	// Test job without optional fields
	job := &service.SchedulerJob{
		ID:        1,
		Name:      "minimal-job",
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "test.handler",
		Status:    service.SchedulerJobStatusPending,
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobResponse(job, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "minimal-job")
	assert.Contains(t, output, "one-shot")
	// Description should not appear since it's empty
	assert.NotContains(t, output, "Description:")
}

func TestFormatSchedulerJobListResponse(t *testing.T) {
	resp := &service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{
				ID:      1,
				Name:    "job-1",
				Type:    service.SchedulerJobTypeRecurring,
				Handler: "backup.full",
				Status:  service.SchedulerJobStatusPending,
				NextRun: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:      2,
				Name:    "job-2",
				Type:    service.SchedulerJobTypeOneShot,
				Handler: "kms.rotate",
				Status:  service.SchedulerJobStatusCompleted,
			},
		},
		TotalCount: 2,
		HasMore:    false,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"ID", "NAME", "TYPE", "HANDLER", "STATUS", "NEXT RUN",
				"1", "job-1", "recurring", "backup.full", "pending",
				"2", "job-2", "one-shot", "kms.rotate", "completed",
				"Total: 2 jobs",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				"job-1",
				"job-2",
				`"total_count": 2`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobListResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatSchedulerJobListResponseWithPagination(t *testing.T) {
	resp := &service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{
				ID:      1,
				Name:    "job-1",
				Type:    service.SchedulerJobTypeRecurring,
				Handler: "backup.full",
				Status:  service.SchedulerJobStatusPending,
			},
		},
		TotalCount: 10,
		HasMore:    true,
		NextCursor: "cursor-next-page",
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobListResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Next cursor: cursor-next-page")
}

func TestFormatSchedulerJobDeleteResponse(t *testing.T) {
	resp := &service.DeleteSchedulerJobResponse{
		JobID:     123,
		Deleted:   true,
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Job ID:", "123",
				"Deleted:", "true",
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"Job 123 deleted successfully",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobDeleteResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSchedulerJobDeleteResponseNotDeleted(t *testing.T) {
	resp := &service.DeleteSchedulerJobResponse{
		JobID:     123,
		Deleted:   false,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobDeleteResponse(resp, "", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Job 123 was not deleted")
}

func TestFormatSchedulerJobTriggerResponse(t *testing.T) {
	resp := &service.TriggerSchedulerJobResponse{
		JobID:     123,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Job ID:", "123",
				"Status:", "running",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"job_id": 123`,
				`"status": "running"`,
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"Job 123 triggered (status: running)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobTriggerResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSchedulerJobStatusChangeResponse(t *testing.T) {
	tests := []struct {
		name         string
		jobID        uint64
		oldStatus    service.SchedulerJobStatus
		newStatus    service.SchedulerJobStatus
		format       string
		expectedText []string
	}{
		{
			name:      "table format pause",
			jobID:     123,
			oldStatus: service.SchedulerJobStatusPending,
			newStatus: service.SchedulerJobStatusPaused,
			format:    "table",
			expectedText: []string{
				"Job ID:", "123",
				"Old Status:", "pending",
				"New Status:", "paused",
			},
		},
		{
			name:      "json format resume",
			jobID:     456,
			oldStatus: service.SchedulerJobStatusPaused,
			newStatus: service.SchedulerJobStatusPending,
			format:    "json",
			expectedText: []string{
				`"job_id": 456`,
				`"old_status": "paused"`,
				`"new_status": "pending"`,
			},
		},
		{
			name:      "default format cancel",
			jobID:     789,
			oldStatus: service.SchedulerJobStatusPending,
			newStatus: service.SchedulerJobStatusCanceled,
			format:    "",
			expectedText: []string{
				"Job 789 status changed: pending -> canceled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobStatusChangeResponse(tt.jobID, tt.oldStatus, tt.newStatus, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSchedulerJobHistoryResponse(t *testing.T) {
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{
				ID:        1,
				JobID:     123,
				Status:    service.SchedulerJobStatusCompleted,
				StartedAt: time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2025, 1, 19, 12, 5, 0, 0, time.UTC),
				Duration:  5 * time.Minute,
			},
			{
				ID:        2,
				JobID:     123,
				Status:    service.SchedulerJobStatusFailed,
				StartedAt: time.Date(2025, 1, 19, 13, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2025, 1, 19, 13, 2, 0, 0, time.UTC),
				Duration:  2 * time.Minute,
				Error:     "Connection timeout",
			},
		},
		HasMore:   false,
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"ID", "STATUS", "STARTED", "DURATION", "ERROR",
				"1", "completed", "5m0s", "-",
				"2", "failed", "2m0s", "Connection timeout",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"id": 1`,
				`"status": "completed"`,
				`"status": "failed"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSchedulerJobHistoryResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSchedulerJobHistoryResponseLongError(t *testing.T) {
	// Test error truncation
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{
				ID:        1,
				JobID:     123,
				Status:    service.SchedulerJobStatusFailed,
				StartedAt: time.Now().Add(-time.Hour),
				EndedAt:   time.Now().Add(-55 * time.Minute),
				Duration:  5 * time.Minute,
				Error:     "This is a very long error message that exceeds fifty characters and should be truncated",
			},
		},
		HasMore:   false,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobHistoryResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "...")
}

func TestFormatSchedulerJobHistoryResponseWithPagination(t *testing.T) {
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{
				ID:        1,
				JobID:     123,
				Status:    service.SchedulerJobStatusCompleted,
				StartedAt: time.Now().Add(-time.Hour),
				Duration:  5 * time.Minute,
			},
		},
		HasMore:    true,
		NextCursor: "cursor-next-page",
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobHistoryResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "More results available")
	assert.Contains(t, output, "cursor-next-page")
}

func TestFormatSchedulerStatusResponse(t *testing.T) {
	tests := []struct {
		name         string
		running      bool
		format       string
		expectedText []string
	}{
		{
			name:    "running table format",
			running: true,
			format:  "table",
			expectedText: []string{
				"Status:", "Running",
				"Total Jobs:", "25",
				"Running Jobs:", "3",
				"Success Count:", "150",
				"Failure Count:", "5",
			},
		},
		{
			name:    "stopped table format",
			running: false,
			format:  "table",
			expectedText: []string{
				"Status:", "Stopped",
			},
		},
		{
			name:    "json format",
			running: true,
			format:  "json",
			expectedText: []string{
				`"running": true`,
				`"job_count": 25`,
			},
		},
		{
			name:    "default format running",
			running: true,
			format:  "",
			expectedText: []string{
				"Scheduler: running",
			},
		},
		{
			name:    "default format stopped",
			running: false,
			format:  "",
			expectedText: []string{
				"Scheduler: stopped",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &service.GetSchedulerStatusResponse{
				Running: tt.running,
				Stats: &service.SchedulerStats{
					JobCount:     25,
					RunningJobs:  3,
					SuccessCount: 150,
					FailureCount: 5,
					StartedAt:    time.Date(2025, 1, 18, 0, 0, 0, 0, time.UTC),
					LastTickTime: time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC),
				},
				Timestamp: time.Now(),
			}

			var buf bytes.Buffer
			err := formatSchedulerStatusResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSchedulerStatusResponseNilStats(t *testing.T) {
	resp := &service.GetSchedulerStatusResponse{
		Running:   true,
		Stats:     nil,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerStatusResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Running")
	// Should not crash with nil stats
}

// =============================================================================
// Invalid Output Format Tests
// =============================================================================

func TestFormatSchedulerJobResponseInvalidFormat(t *testing.T) {
	job := &service.SchedulerJob{
		ID:        1,
		Name:      "test-job",
		Type:      service.SchedulerJobTypeOneShot,
		Handler:   "test.handler",
		Status:    service.SchedulerJobStatusPending,
		CreatedAt: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobResponse(job, "invalid-format", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatSchedulerJobListResponseInvalidFormat(t *testing.T) {
	resp := &service.ListSchedulerJobsResponse{
		Jobs:      []*service.SchedulerJob{},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobListResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSchedulerJobDeleteResponseInvalidFormat(t *testing.T) {
	resp := &service.DeleteSchedulerJobResponse{
		JobID:     123,
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobDeleteResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSchedulerJobTriggerResponseInvalidFormat(t *testing.T) {
	resp := &service.TriggerSchedulerJobResponse{
		JobID:     123,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobTriggerResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSchedulerJobStatusChangeResponseInvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatSchedulerJobStatusChangeResponse(123, service.SchedulerJobStatusPending, service.SchedulerJobStatusPaused, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSchedulerJobHistoryResponseInvalidFormat(t *testing.T) {
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{},
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerJobHistoryResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSchedulerStatusResponseInvalidFormat(t *testing.T) {
	resp := &service.GetSchedulerStatusResponse{
		Running:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSchedulerStatusResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Client Factory Tests
// =============================================================================

func TestCreateSchedulerClient(t *testing.T) {
	// Test default (gRPC) transport - should succeed (gRPC is now implemented)
	transport = "grpc"
	client, err := createSchedulerClient()
	// gRPC uses lazy connections, so creation succeeds even without a server
	assert.NoError(t, err)
	assert.NotNil(t, client)
	if client != nil {
		client.Close()
	}

	// Test empty transport defaults to gRPC
	transport = ""
	client, err = createSchedulerClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)
	if client != nil {
		client.Close()
	}

	// Test REST transport - succeeds with build tag, returns not_implemented without,
	// or connection error when tag present but no server running
	transport = "rest"
	client, err = createSchedulerClient()
	if err != nil {
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "not_implemented") ||
			strings.Contains(errMsg, "unavailable"),
			"expected not_implemented or unavailable error, got: %s", errMsg)
	} else {
		assert.NotNil(t, client)
		client.Close()
	}

	// Test MCP transport - succeeds with build tag, returns not_implemented without,
	// or connection error when tag present but no server running
	transport = "mcp"
	client, err = createSchedulerClient()
	if err != nil {
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "not_implemented") ||
			strings.Contains(errMsg, "unavailable"),
			"expected not_implemented or unavailable error, got: %s", errMsg)
	} else {
		assert.NotNil(t, client)
		client.Close()
	}

	// Test QUIC transport - succeeds with build tag, returns not_implemented without,
	// or connection/configuration error when tag present but no server running
	transport = "quic"
	client, err = createSchedulerClient()
	if err != nil {
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "not_implemented") ||
			strings.Contains(errMsg, "unavailable") ||
			strings.Contains(errMsg, "configuration"),
			"expected not_implemented, unavailable, or configuration error, got: %s", errMsg)
	} else {
		assert.NotNil(t, client)
		client.Close()
	}

	// Test unsupported transport
	transport = "unsupported"
	client, err = createSchedulerClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// =============================================================================
// Edge Cases and Error Handling Tests
// =============================================================================

func TestSchedulerJobCreateEmptyArgs(t *testing.T) {
	cmd := newSchedulerCreateCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobGetEmptyArgs(t *testing.T) {
	cmd := newSchedulerGetCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobDeleteEmptyArgs(t *testing.T) {
	cmd := newSchedulerDeleteCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobTriggerEmptyArgs(t *testing.T) {
	cmd := newSchedulerTriggerCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobPauseEmptyArgs(t *testing.T) {
	cmd := newSchedulerPauseCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobResumeEmptyArgs(t *testing.T) {
	cmd := newSchedulerResumeCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobCancelEmptyArgs(t *testing.T) {
	cmd := newSchedulerCancelCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerJobHistoryEmptyArgs(t *testing.T) {
	cmd := newSchedulerHistoryCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestSchedulerStatusNoArgs(t *testing.T) {
	cmd := newSchedulerStatusCommand()

	// Command requires no args
	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	// Should reject extra args
	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestSchedulerJobListNoArgs(t *testing.T) {
	cmd := newSchedulerListCommand()

	// Command requires no args
	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	// Should reject extra args
	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}
