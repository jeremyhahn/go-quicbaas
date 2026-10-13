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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockSchedulerService implements service.SchedulerService for testing.
type mockSchedulerService struct {
	createJobFunc     func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	getJobFunc        func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	updateJobFunc     func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	deleteJobFunc     func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	listJobsFunc      func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	triggerJobFunc    func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	pauseJobFunc      func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	resumeJobFunc     func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	cancelJobFunc     func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	getJobHistoryFunc func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	getStatusFunc     func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
}

func (m *mockSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if m.createJobFunc != nil {
		return m.createJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.updateJobFunc != nil {
		return m.updateJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.deleteJobFunc != nil {
		return m.deleteJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.triggerJobFunc != nil {
		return m.triggerJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.pauseJobFunc != nil {
		return m.pauseJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.resumeJobFunc != nil {
		return m.resumeJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.cancelJobFunc != nil {
		return m.cancelJobFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.getJobHistoryFunc != nil {
		return m.getJobHistoryFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.getStatusFunc != nil {
		return m.getStatusFunc(ctx)
	}
	return nil, nil
}

// newTestSchedulerServer creates a SchedulerServer for testing.
func newTestSchedulerServer(mockSvc *mockSchedulerService) *SchedulerServer {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return NewSchedulerServer(mockSvc, nil, log)
}

// =============================================================================
// CreateJob Tests
// =============================================================================

func TestSchedulerServer_CreateJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.CreateJobRequest
		mockFunc    func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.CreateJobResponse)
	}{
		{
			name: "successful_create_job",
			request: &pb.CreateJobRequest{
				Name:           "backup-job",
				Description:    "Daily backup job",
				Handler:        "backup.handler",
				OwnerType:      pb.OwnerType_OWNER_TYPE_SYSTEM,
				Type:           pb.JobType_JOB_TYPE_RECURRING,
				Schedule:       "0 0 * * *",
				Priority:       10,
				TimeoutSeconds: 3600,
				MaxRetries:     3,
				Labels:         map[string]string{"env": "production"},
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:          1,
						Name:        req.Name,
						Description: req.Description,
						Handler:     req.Handler,
						OwnerType:   req.OwnerType,
						Type:        req.Type,
						Schedule:    req.Schedule,
						Priority:    req.Priority,
						Timeout:     req.Timeout,
						MaxRetries:  req.MaxRetries,
						Labels:      req.Labels,
						Status:      service.SchedulerJobStatusPending,
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, uint64(1), resp.Job.Id)
				assert.Equal(t, "backup-job", resp.Job.Name)
				assert.Equal(t, "backup.handler", resp.Job.Handler)
				assert.Equal(t, pb.OwnerType_OWNER_TYPE_SYSTEM, resp.Job.OwnerType)
				assert.Equal(t, pb.JobType_JOB_TYPE_RECURRING, resp.Job.Type)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PENDING, resp.Job.Status)
			},
		},
		{
			name: "create_one_shot_job",
			request: &pb.CreateJobRequest{
				Name:      "one-time-job",
				Handler:   "onetime.handler",
				Type:      pb.JobType_JOB_TYPE_ONE_SHOT,
				ExecuteAt: now.Add(time.Hour).Unix(),
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        2,
						Name:      req.Name,
						Handler:   req.Handler,
						Type:      req.Type,
						ExecuteAt: req.ExecuteAt,
						Status:    service.SchedulerJobStatusPending,
						CreatedAt: now,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, pb.JobType_JOB_TYPE_ONE_SHOT, resp.Job.Type)
			},
		},
		{
			name: "create_delayed_job",
			request: &pb.CreateJobRequest{
				Name:         "delayed-job",
				Handler:      "delayed.handler",
				Type:         pb.JobType_JOB_TYPE_DELAYED,
				DelaySeconds: 3600,
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        3,
						Name:      req.Name,
						Handler:   req.Handler,
						Type:      req.Type,
						Delay:     req.Delay,
						Status:    service.SchedulerJobStatusPending,
						CreatedAt: now,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, pb.JobType_JOB_TYPE_DELAYED, resp.Job.Type)
				assert.Equal(t, int64(3600), resp.Job.DelaySeconds)
			},
		},
		{
			name: "missing_name",
			request: &pb.CreateJobRequest{
				Handler: "some.handler",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_handler",
			request: &pb.CreateJobRequest{
				Name: "job-without-handler",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error_already_exists",
			request: &pb.CreateJobRequest{
				Name:    "existing-job",
				Handler: "existing.handler",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrAlreadyExists, "CreateJob", "job already exists")
			},
			expectError: true,
			errorCode:   codes.AlreadyExists,
		},
		{
			name: "service_error_unauthorized",
			request: &pb.CreateJobRequest{
				Name:    "unauthorized-job",
				Handler: "handler",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrUnauthorized, "CreateJob", "unauthorized")
			},
			expectError: true,
			errorCode:   codes.Unauthenticated,
		},
		{
			name: "service_error_forbidden",
			request: &pb.CreateJobRequest{
				Name:    "forbidden-job",
				Handler: "handler",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrForbidden, "CreateJob", "forbidden")
			},
			expectError: true,
			errorCode:   codes.PermissionDenied,
		},
		{
			name: "service_error_invalid_input",
			request: &pb.CreateJobRequest{
				Name:    "invalid-job",
				Handler: "handler",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "CreateJob", "invalid input")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "tenant_owner_type",
			request: &pb.CreateJobRequest{
				Name:           "tenant-job",
				Handler:        "tenant.handler",
				OwnerType:      pb.OwnerType_OWNER_TYPE_TENANT,
				OwnerId:        "tenant-123",
				TargetTenantId: "tenant-456",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:             4,
						Name:           req.Name,
						Handler:        req.Handler,
						OwnerType:      req.OwnerType,
						OwnerID:        req.OwnerID,
						TargetTenantID: req.TargetTenantID,
						Status:         service.SchedulerJobStatusPending,
						CreatedAt:      now,
						UpdatedAt:      now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, pb.OwnerType_OWNER_TYPE_TENANT, resp.Job.OwnerType)
				assert.Equal(t, "tenant-123", resp.Job.OwnerId)
				assert.Equal(t, "tenant-456", resp.Job.TargetTenantId)
			},
		},
		{
			name: "delegated_owner_type",
			request: &pb.CreateJobRequest{
				Name:      "delegated-job",
				Handler:   "delegated.handler",
				OwnerType: pb.OwnerType_OWNER_TYPE_DELEGATED,
				OwnerId:   "delegated-123",
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        5,
						Name:      req.Name,
						Handler:   req.Handler,
						OwnerType: req.OwnerType,
						OwnerID:   req.OwnerID,
						Status:    service.SchedulerJobStatusPending,
						CreatedAt: now,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, pb.OwnerType_OWNER_TYPE_DELEGATED, resp.Job.OwnerType)
			},
		},
		{
			name: "with_payload",
			request: &pb.CreateJobRequest{
				Name:    "payload-job",
				Handler: "payload.handler",
				Payload: []byte(`{"key": "value"}`),
			},
			mockFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				return &service.CreateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        6,
						Name:      req.Name,
						Handler:   req.Handler,
						Payload:   req.Payload,
						Status:    service.SchedulerJobStatusPending,
						CreatedAt: now,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, []byte(`{"key": "value"}`), resp.Job.Payload)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{createJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.CreateJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetJob Tests
// =============================================================================

func TestSchedulerServer_GetJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetJobRequest
		mockFunc    func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetJobResponse)
	}{
		{
			name: "successful_get_job",
			request: &pb.GetJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
				return &service.GetSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        req.JobID,
						Name:      "test-job",
						Handler:   "test.handler",
						OwnerType: service.SchedulerOwnerTypeSystem,
						Type:      service.SchedulerJobTypeRecurring,
						Schedule:  "0 0 * * *",
						Status:    service.SchedulerJobStatusPending,
						RunCount:  5,
						FailCount: 1,
						CreatedAt: now.Add(-24 * time.Hour),
						UpdatedAt: now,
						LastRun:   now.Add(-time.Hour),
						NextRun:   now.Add(23 * time.Hour),
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, uint64(1), resp.Job.Id)
				assert.Equal(t, "test-job", resp.Job.Name)
				assert.Equal(t, uint64(5), resp.Job.RunCount)
				assert.Equal(t, uint64(1), resp.Job.FailCount)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.GetJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.GetJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "service_error_timeout",
			request: &pb.GetJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrTimeout, "GetJob", "timeout")
			},
			expectError: true,
			errorCode:   codes.DeadlineExceeded,
		},
		{
			name: "get_job_with_all_fields",
			request: &pb.GetJobRequest{
				JobId: 2,
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
				return &service.GetSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:             req.JobID,
						Name:           "complete-job",
						Description:    "A complete job with all fields",
						Handler:        "complete.handler",
						OwnerType:      service.SchedulerOwnerTypeTenant,
						OwnerID:        "owner-123",
						TargetTenantID: "target-456",
						CreatedBy:      "admin",
						Type:           service.SchedulerJobTypeDelayed,
						Delay:          time.Hour,
						Payload:        []byte(`{"data": "test"}`),
						Priority:       5,
						Timeout:        30 * time.Minute,
						MaxRetries:     3,
						Status:         service.SchedulerJobStatusRunning,
						LastError:      "previous error",
						Labels:         map[string]string{"env": "test", "app": "quicbaas"},
						CreatedAt:      now.Add(-48 * time.Hour),
						UpdatedAt:      now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, "complete-job", resp.Job.Name)
				assert.Equal(t, "A complete job with all fields", resp.Job.Description)
				assert.Equal(t, pb.OwnerType_OWNER_TYPE_TENANT, resp.Job.OwnerType)
				assert.Equal(t, "owner-123", resp.Job.OwnerId)
				assert.Equal(t, "target-456", resp.Job.TargetTenantId)
				assert.Equal(t, "admin", resp.Job.CreatedBy)
				assert.Equal(t, pb.JobType_JOB_TYPE_DELAYED, resp.Job.Type)
				assert.Equal(t, int64(3600), resp.Job.DelaySeconds)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_RUNNING, resp.Job.Status)
				assert.Equal(t, "previous error", resp.Job.LastError)
				assert.Equal(t, "test", resp.Job.Labels["env"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{getJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.GetJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// UpdateJob Tests
// =============================================================================

func TestSchedulerServer_UpdateJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.UpdateJobRequest
		mockFunc    func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.UpdateJobResponse)
	}{
		{
			name: "successful_update_job",
			request: &pb.UpdateJobRequest{
				JobId:          1,
				TenantId:       "tenant-123",
				Name:           "updated-job",
				Description:    "Updated description",
				Schedule:       "0 */2 * * *",
				Handler:        "updated.handler",
				Priority:       15,
				TimeoutSeconds: 7200,
				MaxRetries:     5,
				Labels:         map[string]string{"env": "staging"},
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return &service.UpdateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:          req.JobID,
						Name:        req.Name,
						Description: req.Description,
						Schedule:    req.Schedule,
						Handler:     req.Handler,
						Priority:    req.Priority,
						Timeout:     req.Timeout,
						MaxRetries:  req.MaxRetries,
						Labels:      req.Labels,
						Status:      service.SchedulerJobStatusPending,
						UpdatedAt:   now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UpdateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, "updated-job", resp.Job.Name)
				assert.Equal(t, "Updated description", resp.Job.Description)
				assert.Equal(t, "0 */2 * * *", resp.Job.Schedule)
				assert.Equal(t, int32(15), resp.Job.Priority)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.UpdateJobRequest{
				JobId: 0,
				Name:  "updated-name",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.UpdateJobRequest{
				JobId: 999,
				Name:  "nonexistent",
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "UpdateJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "update_execute_at",
			request: &pb.UpdateJobRequest{
				JobId:     1,
				ExecuteAt: now.Add(2 * time.Hour).Unix(),
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return &service.UpdateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        req.JobID,
						Name:      "test-job",
						ExecuteAt: req.ExecuteAt,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UpdateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Greater(t, resp.Job.ExecuteAt, int64(0))
			},
		},
		{
			name: "update_payload",
			request: &pb.UpdateJobRequest{
				JobId:   1,
				Payload: []byte(`{"updated": true}`),
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return &service.UpdateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        req.JobID,
						Name:      "test-job",
						Payload:   req.Payload,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UpdateJobResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.Job)
				assert.Equal(t, []byte(`{"updated": true}`), resp.Job.Payload)
			},
		},
		{
			name: "service_error_resource_exhausted",
			request: &pb.UpdateJobRequest{
				JobId: 1,
				Name:  "exhausted",
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrResourceExhausted, "UpdateJob", "resource exhausted")
			},
			expectError: true,
			errorCode:   codes.ResourceExhausted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{updateJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.UpdateJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// DeleteJob Tests
// =============================================================================

func TestSchedulerServer_DeleteJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.DeleteJobRequest
		mockFunc    func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.DeleteJobResponse)
	}{
		{
			name: "successful_delete_job",
			request: &pb.DeleteJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				return &service.DeleteSchedulerJobResponse{
					JobID:     req.JobID,
					Deleted:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.DeleteJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.JobId)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name: "force_delete_job",
			request: &pb.DeleteJobRequest{
				JobId: 2,
				Force: true,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				assert.True(t, req.Force)
				return &service.DeleteSchedulerJobResponse{
					JobID:     req.JobID,
					Deleted:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.DeleteJobResponse) {
				require.NotNil(t, resp)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.DeleteJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.DeleteJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "DeleteJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "service_error_unavailable",
			request: &pb.DeleteJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrUnavailable, "DeleteJob", "service unavailable")
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
		{
			name: "service_error_canceled",
			request: &pb.DeleteJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrCanceled, "DeleteJob", "operation canceled")
			},
			expectError: true,
			errorCode:   codes.Canceled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{deleteJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.DeleteJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// ListJobs Tests
// =============================================================================

func TestSchedulerServer_ListJobs(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ListJobsRequest
		mockFunc    func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ListJobsResponse)
	}{
		{
			name:    "list_jobs_no_filter",
			request: &pb.ListJobsRequest{},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "job-1", Handler: "handler.1", Status: service.SchedulerJobStatusPending},
						{ID: 2, Name: "job-2", Handler: "handler.2", Status: service.SchedulerJobStatusRunning},
						{ID: 3, Name: "job-3", Handler: "handler.3", Status: service.SchedulerJobStatusCompleted},
					},
					TotalCount: 3,
					HasMore:    false,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 3)
				assert.Equal(t, int32(3), resp.TotalCount)
				assert.False(t, resp.HasMore)
			},
		},
		{
			name: "list_jobs_with_pagination",
			request: &pb.ListJobsRequest{
				Limit:  2,
				Cursor: "",
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "job-1", Handler: "handler.1"},
						{ID: 2, Name: "job-2", Handler: "handler.2"},
					},
					NextCursor: "cursor-page-2",
					TotalCount: 5,
					HasMore:    true,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 2)
				assert.Equal(t, "cursor-page-2", resp.NextCursor)
				assert.True(t, resp.HasMore)
				assert.Equal(t, int32(5), resp.TotalCount)
			},
		},
		{
			name: "list_jobs_by_status",
			request: &pb.ListJobsRequest{
				Status: pb.JobStatus_JOB_STATUS_PENDING,
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, service.SchedulerJobStatusPending, req.Status)
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "pending-job-1", Status: service.SchedulerJobStatusPending},
						{ID: 2, Name: "pending-job-2", Status: service.SchedulerJobStatusPending},
					},
					TotalCount: 2,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 2)
				for _, job := range resp.Jobs {
					assert.Equal(t, pb.JobStatus_JOB_STATUS_PENDING, job.Status)
				}
			},
		},
		{
			name: "list_jobs_by_owner_type",
			request: &pb.ListJobsRequest{
				OwnerType: pb.OwnerType_OWNER_TYPE_TENANT,
				OwnerId:   "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
				assert.Equal(t, "tenant-123", req.OwnerID)
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "tenant-job-1", OwnerType: service.SchedulerOwnerTypeTenant, OwnerID: "tenant-123"},
					},
					TotalCount: 1,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 1)
				assert.Equal(t, pb.OwnerType_OWNER_TYPE_TENANT, resp.Jobs[0].OwnerType)
			},
		},
		{
			name: "list_jobs_by_handler",
			request: &pb.ListJobsRequest{
				Handler: "backup.handler",
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, "backup.handler", req.Handler)
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "backup-job", Handler: "backup.handler"},
					},
					TotalCount: 1,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 1)
				assert.Equal(t, "backup.handler", resp.Jobs[0].Handler)
			},
		},
		{
			name: "list_jobs_by_labels",
			request: &pb.ListJobsRequest{
				Labels: map[string]string{"env": "production"},
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, "production", req.Labels["env"])
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "prod-job", Labels: map[string]string{"env": "production"}},
					},
					TotalCount: 1,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 1)
			},
		},
		{
			name: "list_jobs_by_target_tenant",
			request: &pb.ListJobsRequest{
				TargetTenantId: "target-tenant-456",
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, "target-tenant-456", req.TargetTenantID)
				return &service.ListSchedulerJobsResponse{
					Jobs:       []*service.SchedulerJob{},
					TotalCount: 0,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Empty(t, resp.Jobs)
				assert.Equal(t, int32(0), resp.TotalCount)
			},
		},
		{
			name:    "service_error_internal",
			request: &pb.ListJobsRequest{},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return nil, errors.New(errors.ErrInternal, "ListJobs", "internal error")
			},
			expectError: true,
			errorCode:   codes.Internal,
		},
		{
			name: "list_jobs_all_statuses",
			request: &pb.ListJobsRequest{
				Status: pb.JobStatus_JOB_STATUS_FAILED,
			},
			mockFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				assert.Equal(t, service.SchedulerJobStatusFailed, req.Status)
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{ID: 1, Name: "failed-job", Status: service.SchedulerJobStatusFailed, LastError: "error msg"},
					},
					TotalCount: 1,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListJobsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Jobs, 1)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_FAILED, resp.Jobs[0].Status)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{listJobsFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.ListJobs(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// TriggerJob Tests
// =============================================================================

func TestSchedulerServer_TriggerJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.TriggerJobRequest
		mockFunc    func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.TriggerJobResponse)
	}{
		{
			name: "successful_trigger_job",
			request: &pb.TriggerJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
				return &service.TriggerSchedulerJobResponse{
					JobID:     req.JobID,
					Status:    service.SchedulerJobStatusRunning,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.TriggerJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.JobId)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_RUNNING, resp.Status)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.TriggerJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.TriggerJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "TriggerJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "job_already_running",
			request: &pb.TriggerJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "TriggerJob", "job is already running")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{triggerJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.TriggerJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// PauseJob Tests
// =============================================================================

func TestSchedulerServer_PauseJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.PauseJobRequest
		mockFunc    func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.PauseJobResponse)
	}{
		{
			name: "successful_pause_job",
			request: &pb.PauseJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return &service.PauseSchedulerJobResponse{
					JobID:     req.JobID,
					OldStatus: service.SchedulerJobStatusPending,
					NewStatus: service.SchedulerJobStatusPaused,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.PauseJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.JobId)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PENDING, resp.OldStatus)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PAUSED, resp.NewStatus)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.PauseJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.PauseJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "PauseJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "job_already_paused",
			request: &pb.PauseJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "PauseJob", "job is already paused")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "pause_running_job",
			request: &pb.PauseJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return &service.PauseSchedulerJobResponse{
					JobID:     req.JobID,
					OldStatus: service.SchedulerJobStatusRunning,
					NewStatus: service.SchedulerJobStatusPaused,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.PauseJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_RUNNING, resp.OldStatus)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PAUSED, resp.NewStatus)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{pauseJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.PauseJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// ResumeJob Tests
// =============================================================================

func TestSchedulerServer_ResumeJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ResumeJobRequest
		mockFunc    func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ResumeJobResponse)
	}{
		{
			name: "successful_resume_job",
			request: &pb.ResumeJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
				return &service.ResumeSchedulerJobResponse{
					JobID:     req.JobID,
					OldStatus: service.SchedulerJobStatusPaused,
					NewStatus: service.SchedulerJobStatusPending,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ResumeJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.JobId)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PAUSED, resp.OldStatus)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PENDING, resp.NewStatus)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.ResumeJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.ResumeJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "ResumeJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "job_not_paused",
			request: &pb.ResumeJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "ResumeJob", "job is not paused")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{resumeJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.ResumeJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// CancelJob Tests
// =============================================================================

func TestSchedulerServer_CancelJob(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.CancelJobRequest
		mockFunc    func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.CancelJobResponse)
	}{
		{
			name: "successful_cancel_job",
			request: &pb.CancelJobRequest{
				JobId:    1,
				TenantId: "tenant-123",
				Reason:   "no longer needed",
			},
			mockFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				assert.Equal(t, "no longer needed", req.Reason)
				return &service.CancelSchedulerJobResponse{
					JobID:     req.JobID,
					OldStatus: service.SchedulerJobStatusPending,
					NewStatus: service.SchedulerJobStatusCanceled,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CancelJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.JobId)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_PENDING, resp.OldStatus)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_CANCELED, resp.NewStatus)
			},
		},
		{
			name: "cancel_without_reason",
			request: &pb.CancelJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				return &service.CancelSchedulerJobResponse{
					JobID:     req.JobID,
					OldStatus: service.SchedulerJobStatusRunning,
					NewStatus: service.SchedulerJobStatusCanceled,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CancelJobResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_CANCELED, resp.NewStatus)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.CancelJobRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.CancelJobRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "CancelJob", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "job_already_completed",
			request: &pb.CancelJobRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "CancelJob", "cannot cancel completed job")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{cancelJobFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.CancelJob(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetJobHistory Tests
// =============================================================================

func TestSchedulerServer_GetJobHistory(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetJobHistoryRequest
		mockFunc    func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetJobHistoryResponse)
	}{
		{
			name: "successful_get_job_history",
			request: &pb.GetJobHistoryRequest{
				JobId:    1,
				TenantId: "tenant-123",
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return &service.GetSchedulerJobHistoryResponse{
					Executions: []*service.SchedulerJobExecution{
						{
							ID:        1,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusCompleted,
							StartedAt: now.Add(-time.Hour),
							EndedAt:   now.Add(-50 * time.Minute),
							Duration:  10 * time.Minute,
							Output:    "success",
						},
						{
							ID:        2,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusFailed,
							StartedAt: now.Add(-2 * time.Hour),
							EndedAt:   now.Add(-time.Hour - 55*time.Minute),
							Duration:  5 * time.Minute,
							Error:     "connection failed",
						},
					},
					HasMore:   false,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetJobHistoryResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Executions, 2)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_COMPLETED, resp.Executions[0].Status)
				assert.Equal(t, "success", resp.Executions[0].Output)
				assert.Equal(t, pb.JobStatus_JOB_STATUS_FAILED, resp.Executions[1].Status)
				assert.Equal(t, "connection failed", resp.Executions[1].Error)
				assert.False(t, resp.HasMore)
			},
		},
		{
			name: "get_history_with_pagination",
			request: &pb.GetJobHistoryRequest{
				JobId:  1,
				Limit:  5,
				Cursor: "cursor-1",
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				assert.Equal(t, 5, req.Limit)
				assert.Equal(t, "cursor-1", req.Cursor)
				return &service.GetSchedulerJobHistoryResponse{
					Executions: []*service.SchedulerJobExecution{
						{ID: 6, JobID: req.JobID, Status: service.SchedulerJobStatusCompleted},
						{ID: 7, JobID: req.JobID, Status: service.SchedulerJobStatusCompleted},
					},
					NextCursor: "cursor-2",
					HasMore:    true,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetJobHistoryResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.Executions, 2)
				assert.Equal(t, "cursor-2", resp.NextCursor)
				assert.True(t, resp.HasMore)
			},
		},
		{
			name: "missing_job_id",
			request: &pb.GetJobHistoryRequest{
				JobId: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "job_not_found",
			request: &pb.GetJobHistoryRequest{
				JobId: 999,
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetJobHistory", "job not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "empty_history",
			request: &pb.GetJobHistoryRequest{
				JobId: 1,
			},
			mockFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return &service.GetSchedulerJobHistoryResponse{
					Executions: []*service.SchedulerJobExecution{},
					HasMore:    false,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetJobHistoryResponse) {
				require.NotNil(t, resp)
				assert.Empty(t, resp.Executions)
				assert.False(t, resp.HasMore)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{getJobHistoryFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.GetJobHistory(context.Background(), tc.request)

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// GetSchedulerStatus Tests
// =============================================================================

func TestSchedulerServer_GetSchedulerStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		mockFunc    func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetSchedulerStatusResponse)
	}{
		{
			name: "successful_get_status_running",
			mockFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return &service.GetSchedulerStatusResponse{
					Running: true,
					Stats: &service.SchedulerStats{
						JobCount:     100,
						RunningJobs:  5,
						SuccessCount: 1000,
						FailureCount: 50,
						LastTickTime: now.Add(-time.Second),
						StartedAt:    now.Add(-24 * time.Hour),
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetSchedulerStatusResponse) {
				require.NotNil(t, resp)
				assert.True(t, resp.Running)
				require.NotNil(t, resp.Stats)
				assert.Equal(t, uint64(100), resp.Stats.JobCount)
				assert.Equal(t, uint64(5), resp.Stats.RunningJobs)
				assert.Equal(t, uint64(1000), resp.Stats.SuccessCount)
				assert.Equal(t, uint64(50), resp.Stats.FailureCount)
			},
		},
		{
			name: "scheduler_stopped",
			mockFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return &service.GetSchedulerStatusResponse{
					Running: false,
					Stats: &service.SchedulerStats{
						JobCount:     50,
						RunningJobs:  0,
						SuccessCount: 500,
						FailureCount: 10,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetSchedulerStatusResponse) {
				require.NotNil(t, resp)
				assert.False(t, resp.Running)
				require.NotNil(t, resp.Stats)
				assert.Equal(t, uint64(0), resp.Stats.RunningJobs)
			},
		},
		{
			name: "service_error",
			mockFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return nil, errors.New(errors.ErrInternal, "GetStatus", "internal error")
			},
			expectError: true,
			errorCode:   codes.Internal,
		},
		{
			name: "nil_stats",
			mockFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return &service.GetSchedulerStatusResponse{
					Running:   true,
					Stats:     nil,
					Timestamp: now,
				}, nil
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetSchedulerStatusResponse) {
				require.NotNil(t, resp)
				assert.True(t, resp.Running)
				assert.Nil(t, resp.Stats)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockSchedulerService{getStatusFunc: tc.mockFunc}
			server := newTestSchedulerServer(mockSvc)

			resp, err := server.GetSchedulerStatus(context.Background(), &pb.GetSchedulerStatusRequest{})

			if tc.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tc.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// Type Conversion Tests
// =============================================================================

func TestSchedulerServer_TypeConversions(t *testing.T) {
	t.Run("proto_owner_type_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.OwnerType
			expected service.SchedulerOwnerType
		}{
			{pb.OwnerType_OWNER_TYPE_SYSTEM, service.SchedulerOwnerTypeSystem},
			{pb.OwnerType_OWNER_TYPE_TENANT, service.SchedulerOwnerTypeTenant},
			{pb.OwnerType_OWNER_TYPE_DELEGATED, service.SchedulerOwnerTypeDelegated},
			{pb.OwnerType_OWNER_TYPE_UNSPECIFIED, service.SchedulerOwnerTypeSystem},
		}

		for _, tc := range tests {
			result := protoOwnerTypeToService(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("proto_job_type_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.JobType
			expected service.SchedulerJobType
		}{
			{pb.JobType_JOB_TYPE_ONE_SHOT, service.SchedulerJobTypeOneShot},
			{pb.JobType_JOB_TYPE_RECURRING, service.SchedulerJobTypeRecurring},
			{pb.JobType_JOB_TYPE_DELAYED, service.SchedulerJobTypeDelayed},
			{pb.JobType_JOB_TYPE_UNSPECIFIED, service.SchedulerJobTypeOneShot},
		}

		for _, tc := range tests {
			result := protoJobTypeToService(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("proto_job_status_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.JobStatus
			expected service.SchedulerJobStatus
		}{
			{pb.JobStatus_JOB_STATUS_PENDING, service.SchedulerJobStatusPending},
			{pb.JobStatus_JOB_STATUS_RUNNING, service.SchedulerJobStatusRunning},
			{pb.JobStatus_JOB_STATUS_COMPLETED, service.SchedulerJobStatusCompleted},
			{pb.JobStatus_JOB_STATUS_FAILED, service.SchedulerJobStatusFailed},
			{pb.JobStatus_JOB_STATUS_PAUSED, service.SchedulerJobStatusPaused},
			{pb.JobStatus_JOB_STATUS_CANCELED, service.SchedulerJobStatusCanceled},
			{pb.JobStatus_JOB_STATUS_UNSPECIFIED, service.SchedulerJobStatusPending},
		}

		for _, tc := range tests {
			result := protoJobStatusToService(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_owner_type_to_proto", func(t *testing.T) {
		tests := []struct {
			input    service.SchedulerOwnerType
			expected pb.OwnerType
		}{
			{service.SchedulerOwnerTypeSystem, pb.OwnerType_OWNER_TYPE_SYSTEM},
			{service.SchedulerOwnerTypeTenant, pb.OwnerType_OWNER_TYPE_TENANT},
			{service.SchedulerOwnerTypeDelegated, pb.OwnerType_OWNER_TYPE_DELEGATED},
			{"unknown", pb.OwnerType_OWNER_TYPE_UNSPECIFIED},
		}

		for _, tc := range tests {
			result := serviceOwnerTypeToProto(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_job_type_to_proto", func(t *testing.T) {
		tests := []struct {
			input    service.SchedulerJobType
			expected pb.JobType
		}{
			{service.SchedulerJobTypeOneShot, pb.JobType_JOB_TYPE_ONE_SHOT},
			{service.SchedulerJobTypeRecurring, pb.JobType_JOB_TYPE_RECURRING},
			{service.SchedulerJobTypeDelayed, pb.JobType_JOB_TYPE_DELAYED},
			{"unknown", pb.JobType_JOB_TYPE_UNSPECIFIED},
		}

		for _, tc := range tests {
			result := serviceJobTypeToProto(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_job_status_to_proto", func(t *testing.T) {
		tests := []struct {
			input    service.SchedulerJobStatus
			expected pb.JobStatus
		}{
			{service.SchedulerJobStatusPending, pb.JobStatus_JOB_STATUS_PENDING},
			{service.SchedulerJobStatusRunning, pb.JobStatus_JOB_STATUS_RUNNING},
			{service.SchedulerJobStatusCompleted, pb.JobStatus_JOB_STATUS_COMPLETED},
			{service.SchedulerJobStatusFailed, pb.JobStatus_JOB_STATUS_FAILED},
			{service.SchedulerJobStatusPaused, pb.JobStatus_JOB_STATUS_PAUSED},
			{service.SchedulerJobStatusCanceled, pb.JobStatus_JOB_STATUS_CANCELED},
			{"unknown", pb.JobStatus_JOB_STATUS_UNSPECIFIED},
		}

		for _, tc := range tests {
			result := serviceJobStatusToProto(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("service_job_to_proto_nil", func(t *testing.T) {
		result := serviceJobToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("service_execution_to_proto_nil", func(t *testing.T) {
		result := serviceExecutionToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("service_stats_to_proto_nil", func(t *testing.T) {
		result := serviceStatsToProto(nil)
		assert.Nil(t, result)
	})
}

// =============================================================================
// Error Conversion Tests
// =============================================================================

func TestSchedulerServer_ConvertSchedulerError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil_error",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name:         "not_found",
			err:          errors.New(errors.ErrNotFound, "test", "not found"),
			expectedCode: codes.NotFound,
		},
		{
			name:         "already_exists",
			err:          errors.New(errors.ErrAlreadyExists, "test", "exists"),
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "invalid_input",
			err:          errors.New(errors.ErrInvalidInput, "test", "invalid"),
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "unauthorized",
			err:          errors.New(errors.ErrUnauthorized, "test", "unauthorized"),
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "forbidden",
			err:          errors.New(errors.ErrForbidden, "test", "forbidden"),
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "timeout",
			err:          errors.New(errors.ErrTimeout, "test", "timeout"),
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "resource_exhausted",
			err:          errors.New(errors.ErrResourceExhausted, "test", "exhausted"),
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "unavailable",
			err:          errors.New(errors.ErrUnavailable, "test", "unavailable"),
			expectedCode: codes.Unavailable,
		},
		{
			name:         "canceled",
			err:          errors.New(errors.ErrCanceled, "test", "canceled"),
			expectedCode: codes.Canceled,
		},
		{
			name:         "internal",
			err:          errors.New(errors.ErrInternal, "test", "internal"),
			expectedCode: codes.Internal,
		},
		{
			name:         "unknown_error_code",
			err:          errors.New(errors.ErrUnknown, "test", "unknown"),
			expectedCode: codes.Unknown,
		},
		{
			name:         "rate_limited",
			err:          errors.New(errors.ErrRateLimited, "test", "too many requests"),
			expectedCode: codes.ResourceExhausted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertSchedulerError(tc.err)
			if tc.err == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				st, ok := status.FromError(result)
				require.True(t, ok)
				assert.Equal(t, tc.expectedCode, st.Code())
			}
		})
	}

	t.Run("non_qb_error", func(t *testing.T) {
		err := context.DeadlineExceeded
		result := convertSchedulerError(err)
		require.NotNil(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})

	// Scheduler-specific error types
	schedulerTests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "scheduler_job_not_found",
			err:          &scheduler.JobNotFoundError{JobID: 42},
			expectedCode: codes.NotFound,
		},
		{
			name:         "scheduler_job_already_exists",
			err:          &scheduler.JobAlreadyExistsError{JobID: 1, Name: "test"},
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "scheduler_invalid_job",
			err:          &scheduler.InvalidJobError{Field: "name", Message: "required"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "scheduler_invalid_schedule",
			err:          &scheduler.InvalidScheduleError{Schedule: "bad"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "scheduler_handler_not_found",
			err:          &scheduler.HandlerNotFoundError{Handler: "missing"},
			expectedCode: codes.NotFound,
		},
		{
			name:         "scheduler_handler_failed",
			err:          &scheduler.HandlerFailedError{Handler: "test", JobID: 1, Cause: context.Canceled},
			expectedCode: codes.Internal,
		},
		{
			name:         "scheduler_job_timeout",
			err:          &scheduler.JobTimeoutError{JobID: 1, Timeout: 30 * time.Second},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "scheduler_max_retries_exceeded",
			err:          &scheduler.MaxRetriesExceededError{JobID: 1, MaxRetries: 3, LastError: context.Canceled},
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "scheduler_stopped",
			err:          &scheduler.SchedulerStoppedError{},
			expectedCode: codes.Unavailable,
		},
		{
			name:         "scheduler_unauthorized",
			err:          &scheduler.UnauthorizedError{TenantID: "t1", JobID: 1, Op: "delete"},
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "scheduler_storage_error",
			err:          &scheduler.ErrStorageError{Op: "GetJob", Cause: context.Canceled},
			expectedCode: codes.Internal,
		},
	}

	for _, tc := range schedulerTests {
		t.Run(tc.name, func(t *testing.T) {
			result := convertSchedulerError(tc.err)
			require.NotNil(t, result)
			st, ok := status.FromError(result)
			require.True(t, ok)
			assert.Equal(t, tc.expectedCode, st.Code())
		})
	}
}

// =============================================================================
// NewSchedulerServer Tests
// =============================================================================

func TestNewSchedulerServer(t *testing.T) {
	t.Run("creates_server_with_mock_service", func(t *testing.T) {
		mockSvc := &mockSchedulerService{}
		log, err := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
		require.NoError(t, err)

		server := NewSchedulerServer(mockSvc, nil, log)

		require.NotNil(t, server)
		assert.NotNil(t, server.schedulerSvc)
		assert.NotNil(t, server.logger)
	})
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestSchedulerServer_EdgeCases(t *testing.T) {
	now := time.Now()

	t.Run("create_job_with_zero_timeout", func(t *testing.T) {
		mockSvc := &mockSchedulerService{
			createJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
				assert.Equal(t, time.Duration(0), req.Timeout)
				return &service.CreateSchedulerJobResponse{
					Job:       &service.SchedulerJob{ID: 1, Name: req.Name, Handler: req.Handler},
					Timestamp: now,
				}, nil
			},
		}
		server := newTestSchedulerServer(mockSvc)

		resp, err := server.CreateJob(context.Background(), &pb.CreateJobRequest{
			Name:           "zero-timeout-job",
			Handler:        "handler",
			TimeoutSeconds: 0,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("list_jobs_with_empty_labels", func(t *testing.T) {
		mockSvc := &mockSchedulerService{
			listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs:      []*service.SchedulerJob{},
					Timestamp: now,
				}, nil
			},
		}
		server := newTestSchedulerServer(mockSvc)

		resp, err := server.ListJobs(context.Background(), &pb.ListJobsRequest{
			Labels: map[string]string{},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("get_job_history_with_execution_duration", func(t *testing.T) {
		mockSvc := &mockSchedulerService{
			getJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return &service.GetSchedulerJobHistoryResponse{
					Executions: []*service.SchedulerJobExecution{
						{
							ID:        1,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusCompleted,
							StartedAt: now.Add(-10 * time.Minute),
							EndedAt:   now,
							Duration:  10 * time.Minute,
						},
					},
					Timestamp: now,
				}, nil
			},
		}
		server := newTestSchedulerServer(mockSvc)

		resp, err := server.GetJobHistory(context.Background(), &pb.GetJobHistoryRequest{
			JobId: 1,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Executions, 1)
		assert.Equal(t, int64(600000), resp.Executions[0].DurationMs)
	})
}
