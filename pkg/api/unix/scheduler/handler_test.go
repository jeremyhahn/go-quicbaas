//go:build unix

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

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// testLogger implements interfaces.Logger for testing.
type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...any)             {}
func (l *testLogger) Debugf(format string, args ...interface{}) {}
func (l *testLogger) Info(msg string, args ...any)              {}
func (l *testLogger) Infof(format string, args ...interface{})  {}
func (l *testLogger) Warn(msg string, args ...any)              {}
func (l *testLogger) Warnf(format string, args ...interface{})  {}
func (l *testLogger) Error(msg string, args ...any)             {}
func (l *testLogger) Errorf(format string, args ...interface{}) {}
func (l *testLogger) Fatal(msg string)                          {}
func (l *testLogger) Fatalf(format string, args ...interface{}) {}
func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithError(err error) interfaces.Logger {
	return l
}

// mockSchedulerService is a mock implementation for testing.
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
	return &service.CreateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:      1,
			Name:    req.Name,
			Handler: req.Handler,
			Status:  service.SchedulerJobStatusPending,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, req)
	}
	return &service.GetSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:      req.JobID,
			Name:    "test-job",
			Handler: "test-handler",
			Status:  service.SchedulerJobStatusPending,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.updateJobFunc != nil {
		return m.updateJobFunc(ctx, req)
	}
	return &service.UpdateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:      req.JobID,
			Name:    req.Name,
			Handler: req.Handler,
			Status:  service.SchedulerJobStatusPending,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.deleteJobFunc != nil {
		return m.deleteJobFunc(ctx, req)
	}
	return &service.DeleteSchedulerJobResponse{
		JobID:     req.JobID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, req)
	}
	return &service.ListSchedulerJobsResponse{
		Jobs:       []*service.SchedulerJob{},
		HasMore:    false,
		TotalCount: 0,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.triggerJobFunc != nil {
		return m.triggerJobFunc(ctx, req)
	}
	return &service.TriggerSchedulerJobResponse{
		JobID:     req.JobID,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.pauseJobFunc != nil {
		return m.pauseJobFunc(ctx, req)
	}
	return &service.PauseSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.resumeJobFunc != nil {
		return m.resumeJobFunc(ctx, req)
	}
	return &service.ResumeSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusPending,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.cancelJobFunc != nil {
		return m.cancelJobFunc(ctx, req)
	}
	return &service.CancelSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.getJobHistoryFunc != nil {
		return m.getJobHistoryFunc(ctx, req)
	}
	return &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{},
		HasMore:    false,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.getStatusFunc != nil {
		return m.getStatusFunc(ctx)
	}
	return &service.GetSchedulerStatusResponse{
		Running: true,
		Stats: &service.SchedulerStats{
			JobCount:    10,
			RunningJobs: 2,
		},
		Timestamp: time.Now(),
	}, nil
}

// serviceError returns the QBError a failing mock service method returns.
func serviceError(code dberrors.ErrorCode) error {
	return &dberrors.QBError{Code: code, Op: "test", Err: assert.AnError}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name    string
		svc     service.SchedulerService
		logger  interfaces.Logger
		wantErr bool
	}{
		{name: "success", svc: &mockSchedulerService{}, logger: &testLogger{}},
		{name: "nil service", svc: nil, logger: &testLogger{}, wantErr: true},
		{name: "nil logger", svc: &mockSchedulerService{}, logger: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, tt.logger)
			if tt.wantErr {
				require.Error(t, err)
				var qbErr *dberrors.QBError
				require.True(t, dberrors.As(err, &qbErr))
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, handler)
			assert.NotNil(t, handler.Service())
		})
	}
}

func TestHandlerUnknownOpcode(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), 0xFF, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestHandlerClosed(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	err = handler.Close()
	require.NoError(t, err)

	// Second close should be idempotent
	err = handler.Close()
	require.NoError(t, err)

	// Handle should fail when closed
	_, err = handler.Handle(context.Background(), OpSchedulerGetJob, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestHandleRequestNil(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	resp := handler.HandleRequest(context.Background(), nil)
	assert.Equal(t, StatusInvalidRequest, resp.Status)
}

// TestHandleRequestOpcodes drives every opcode through HandleRequest once
// with a succeeding service and once with a failing one, so each row checks
// the payload decoding, the service call and the response status mapping.
func TestHandleRequestOpcodes(t *testing.T) {
	tests := []struct {
		name       string
		opcode     byte
		req        any
		svc        *mockSchedulerService
		wantStatus byte
		check      func(t *testing.T, payload []byte)
	}{
		{
			name:   "create job",
			opcode: OpSchedulerCreateJob,
			req: &service.CreateSchedulerJobRequest{
				Name:    "test-job",
				Handler: "test-handler",
				Type:    service.SchedulerJobTypeOneShot,
			},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.CreateSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(1), resp.Job.ID)
				assert.Equal(t, "test-job", resp.Job.Name)
			},
		},
		{
			name:   "create job service error",
			opcode: OpSchedulerCreateJob,
			req:    &service.CreateSchedulerJobRequest{Name: "test-job", Handler: "test-handler"},
			svc: &mockSchedulerService{
				createJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrInvalidInput)
				},
			},
			wantStatus: StatusInvalidArgument,
		},
		{
			name:       "get job",
			opcode:     OpSchedulerGetJob,
			req:        &service.GetSchedulerJobRequest{JobID: 123},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.GetSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.Job.ID)
				assert.Equal(t, "test-job", resp.Job.Name)
			},
		},
		{
			name:   "get job service error",
			opcode: OpSchedulerGetJob,
			req:    &service.GetSchedulerJobRequest{JobID: 999},
			svc: &mockSchedulerService{
				getJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:   "update job",
			opcode: OpSchedulerUpdateJob,
			req: &service.UpdateSchedulerJobRequest{
				JobID:   123,
				Name:    "updated-job",
				Handler: "updated-handler",
			},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.UpdateSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.Job.ID)
				assert.Equal(t, "updated-job", resp.Job.Name)
			},
		},
		{
			name:   "update job service error",
			opcode: OpSchedulerUpdateJob,
			req:    &service.UpdateSchedulerJobRequest{JobID: 999, Name: "updated-job"},
			svc: &mockSchedulerService{
				updateJobFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:       "delete job",
			opcode:     OpSchedulerDeleteJob,
			req:        &service.DeleteSchedulerJobRequest{JobID: 123},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.DeleteSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.JobID)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name:   "delete job service error",
			opcode: OpSchedulerDeleteJob,
			req:    &service.DeleteSchedulerJobRequest{JobID: 999},
			svc: &mockSchedulerService{
				deleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:   "list jobs",
			opcode: OpSchedulerListJobs,
			req:    &service.ListSchedulerJobsRequest{Limit: 10},
			svc: &mockSchedulerService{
				listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
					return &service.ListSchedulerJobsResponse{
						Jobs: []*service.SchedulerJob{
							{ID: 1, Name: "job-1", Status: service.SchedulerJobStatusPending},
							{ID: 2, Name: "job-2", Status: service.SchedulerJobStatusRunning},
						},
						TotalCount: 2,
						Timestamp:  time.Now(),
					}, nil
				},
			},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.ListSchedulerJobsResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Len(t, resp.Jobs, 2)
				assert.Equal(t, 2, resp.TotalCount)
			},
		},
		{
			name:   "list jobs service error",
			opcode: OpSchedulerListJobs,
			req:    &service.ListSchedulerJobsRequest{Limit: 10},
			svc: &mockSchedulerService{
				listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
					return nil, serviceError(dberrors.ErrInternal)
				},
			},
			wantStatus: StatusInternalError,
		},
		{
			name:       "trigger job",
			opcode:     OpSchedulerTriggerJob,
			req:        &service.TriggerSchedulerJobRequest{JobID: 123},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.TriggerSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.JobID)
				assert.Equal(t, service.SchedulerJobStatusRunning, resp.Status)
			},
		},
		{
			name:   "trigger job service error",
			opcode: OpSchedulerTriggerJob,
			req:    &service.TriggerSchedulerJobRequest{JobID: 999},
			svc: &mockSchedulerService{
				triggerJobFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:       "pause job",
			opcode:     OpSchedulerPauseJob,
			req:        &service.PauseSchedulerJobRequest{JobID: 123},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.PauseSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.JobID)
				assert.Equal(t, service.SchedulerJobStatusPaused, resp.NewStatus)
			},
		},
		{
			name:   "pause job service error",
			opcode: OpSchedulerPauseJob,
			req:    &service.PauseSchedulerJobRequest{JobID: 123},
			svc: &mockSchedulerService{
				pauseJobFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrInvalidInput)
				},
			},
			wantStatus: StatusInvalidArgument,
		},
		{
			name:       "resume job",
			opcode:     OpSchedulerResumeJob,
			req:        &service.ResumeSchedulerJobRequest{JobID: 123},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.ResumeSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.JobID)
				assert.Equal(t, service.SchedulerJobStatusPending, resp.NewStatus)
			},
		},
		{
			name:   "resume job service error",
			opcode: OpSchedulerResumeJob,
			req:    &service.ResumeSchedulerJobRequest{JobID: 123},
			svc: &mockSchedulerService{
				resumeJobFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrInvalidInput)
				},
			},
			wantStatus: StatusInvalidArgument,
		},
		{
			name:       "cancel job",
			opcode:     OpSchedulerCancelJob,
			req:        &service.CancelSchedulerJobRequest{JobID: 123, Reason: "test cancellation"},
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.CancelSchedulerJobResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.Equal(t, uint64(123), resp.JobID)
				assert.Equal(t, service.SchedulerJobStatusCanceled, resp.NewStatus)
			},
		},
		{
			name:   "cancel job service error",
			opcode: OpSchedulerCancelJob,
			req:    &service.CancelSchedulerJobRequest{JobID: 999},
			svc: &mockSchedulerService{
				cancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:   "get job history",
			opcode: OpSchedulerGetJobHistory,
			req:    &service.GetSchedulerJobHistoryRequest{JobID: 123, Limit: 10},
			svc: &mockSchedulerService{
				getJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
					now := time.Now()
					return &service.GetSchedulerJobHistoryResponse{
						Executions: []*service.SchedulerJobExecution{{
							ID:        1,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusCompleted,
							StartedAt: now.Add(-time.Hour),
							EndedAt:   now.Add(-time.Hour + time.Minute),
						}},
						Timestamp: now,
					}, nil
				},
			},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.GetSchedulerJobHistoryResponse
				require.NoError(t, DecodePayload(payload, &resp))
				require.Len(t, resp.Executions, 1)
				assert.Equal(t, uint64(123), resp.Executions[0].JobID)
			},
		},
		{
			name:   "get job history service error",
			opcode: OpSchedulerGetJobHistory,
			req:    &service.GetSchedulerJobHistoryRequest{JobID: 999},
			svc: &mockSchedulerService{
				getJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
					return nil, serviceError(dberrors.ErrNotFound)
				},
			},
			wantStatus: StatusNotFound,
		},
		{
			name:       "get status",
			opcode:     OpSchedulerGetStatus,
			svc:        &mockSchedulerService{},
			wantStatus: StatusOK,
			check: func(t *testing.T, payload []byte) {
				var resp service.GetSchedulerStatusResponse
				require.NoError(t, DecodePayload(payload, &resp))
				assert.True(t, resp.Running)
				assert.Equal(t, uint64(10), resp.Stats.JobCount)
				assert.Equal(t, uint64(2), resp.Stats.RunningJobs)
			},
		},
		{
			name:   "get status service error",
			opcode: OpSchedulerGetStatus,
			svc: &mockSchedulerService{
				getStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
					return nil, serviceError(dberrors.ErrInternal)
				},
			},
			wantStatus: StatusInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, &testLogger{})
			require.NoError(t, err)

			var payload []byte
			if tt.req != nil {
				payload, err = EncodePayload(tt.req)
				require.NoError(t, err)
			}

			resp := handler.HandleRequest(context.Background(), &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			})
			require.Equal(t, tt.wantStatus, resp.Status)
			if tt.check == nil {
				assert.NotEmpty(t, resp.Payload, "error response carries a message")
				return
			}
			tt.check(t, resp.Payload)
		})
	}
}

func TestHandleInvalidPayload(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	// Send invalid JSON payload
	_, err = handler.Handle(context.Background(), OpSchedulerGetJob, []byte("invalid json"))
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}
