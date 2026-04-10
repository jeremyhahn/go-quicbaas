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

func (l *testLogger) Debug(msg string)                          {}
func (l *testLogger) Debugf(format string, args ...interface{}) {}
func (l *testLogger) Info(msg string)                           {}
func (l *testLogger) Infof(format string, args ...interface{})  {}
func (l *testLogger) Warn(msg string)                           {}
func (l *testLogger) Warnf(format string, args ...interface{})  {}
func (l *testLogger) Error(msg string)                          {}
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

func TestNewHandlerNilService(t *testing.T) {
	_, err := NewHandler(nil, &testLogger{})
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestNewHandlerNilLogger(t *testing.T) {
	_, err := NewHandler(&mockSchedulerService{}, nil)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestNewHandlerSuccess(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, handler)

	assert.NotNil(t, handler.Service())
}

func TestHandlerUnknownOpcode(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), 0xFF, nil)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
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

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleRequestNil(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	resp := handler.HandleRequest(context.Background(), nil)
	assert.Equal(t, StatusInvalidRequest, resp.Status)
}

func TestHandleCreateJob(t *testing.T) {
	svc := &mockSchedulerService{
		createJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
			return &service.CreateSchedulerJobResponse{
				Job: &service.SchedulerJob{
					ID:      123,
					Name:    req.Name,
					Handler: req.Handler,
					Status:  service.SchedulerJobStatusPending,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CreateSchedulerJobRequest{
		Name:    "test-job",
		Handler: "test-handler",
		Type:    service.SchedulerJobTypeOneShot,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerCreateJob, reqPayload)
	require.NoError(t, err)

	var createResp service.CreateSchedulerJobResponse
	err = DecodePayload(resp, &createResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), createResp.Job.ID)
	assert.Equal(t, "test-job", createResp.Job.Name)
}

func TestHandleGetJob(t *testing.T) {
	svc := &mockSchedulerService{
		getJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
			return &service.GetSchedulerJobResponse{
				Job: &service.SchedulerJob{
					ID:      req.JobID,
					Name:    "retrieved-job",
					Handler: "test-handler",
					Status:  service.SchedulerJobStatusPending,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerGetJob, reqPayload)
	require.NoError(t, err)

	var getResp service.GetSchedulerJobResponse
	err = DecodePayload(resp, &getResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), getResp.Job.ID)
	assert.Equal(t, "retrieved-job", getResp.Job.Name)
}

func TestHandleUpdateJob(t *testing.T) {
	svc := &mockSchedulerService{
		updateJobFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
			return &service.UpdateSchedulerJobResponse{
				Job: &service.SchedulerJob{
					ID:      req.JobID,
					Name:    req.Name,
					Handler: req.Handler,
					Status:  service.SchedulerJobStatusPending,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.UpdateSchedulerJobRequest{
		JobID:   123,
		Name:    "updated-job",
		Handler: "updated-handler",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerUpdateJob, reqPayload)
	require.NoError(t, err)

	var updateResp service.UpdateSchedulerJobResponse
	err = DecodePayload(resp, &updateResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), updateResp.Job.ID)
	assert.Equal(t, "updated-job", updateResp.Job.Name)
}

func TestHandleDeleteJob(t *testing.T) {
	svc := &mockSchedulerService{
		deleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
			return &service.DeleteSchedulerJobResponse{
				JobID:     req.JobID,
				Deleted:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.DeleteSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerDeleteJob, reqPayload)
	require.NoError(t, err)

	var delResp service.DeleteSchedulerJobResponse
	err = DecodePayload(resp, &delResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), delResp.JobID)
	assert.True(t, delResp.Deleted)
}

func TestHandleListJobs(t *testing.T) {
	svc := &mockSchedulerService{
		listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
			return &service.ListSchedulerJobsResponse{
				Jobs: []*service.SchedulerJob{
					{ID: 1, Name: "job-1", Status: service.SchedulerJobStatusPending},
					{ID: 2, Name: "job-2", Status: service.SchedulerJobStatusRunning},
				},
				HasMore:    false,
				TotalCount: 2,
				Timestamp:  time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListSchedulerJobsRequest{
		Limit: 10,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerListJobs, reqPayload)
	require.NoError(t, err)

	var listResp service.ListSchedulerJobsResponse
	err = DecodePayload(resp, &listResp)
	require.NoError(t, err)

	assert.Len(t, listResp.Jobs, 2)
	assert.Equal(t, 2, listResp.TotalCount)
}

func TestHandleTriggerJob(t *testing.T) {
	svc := &mockSchedulerService{
		triggerJobFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
			return &service.TriggerSchedulerJobResponse{
				JobID:     req.JobID,
				Status:    service.SchedulerJobStatusRunning,
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.TriggerSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerTriggerJob, reqPayload)
	require.NoError(t, err)

	var triggerResp service.TriggerSchedulerJobResponse
	err = DecodePayload(resp, &triggerResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), triggerResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusRunning, triggerResp.Status)
}

func TestHandlePauseJob(t *testing.T) {
	svc := &mockSchedulerService{
		pauseJobFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
			return &service.PauseSchedulerJobResponse{
				JobID:     req.JobID,
				OldStatus: service.SchedulerJobStatusPending,
				NewStatus: service.SchedulerJobStatusPaused,
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.PauseSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerPauseJob, reqPayload)
	require.NoError(t, err)

	var pauseResp service.PauseSchedulerJobResponse
	err = DecodePayload(resp, &pauseResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), pauseResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusPaused, pauseResp.NewStatus)
}

func TestHandleResumeJob(t *testing.T) {
	svc := &mockSchedulerService{
		resumeJobFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
			return &service.ResumeSchedulerJobResponse{
				JobID:     req.JobID,
				OldStatus: service.SchedulerJobStatusPaused,
				NewStatus: service.SchedulerJobStatusPending,
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ResumeSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerResumeJob, reqPayload)
	require.NoError(t, err)

	var resumeResp service.ResumeSchedulerJobResponse
	err = DecodePayload(resp, &resumeResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), resumeResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusPending, resumeResp.NewStatus)
}

func TestHandleCancelJob(t *testing.T) {
	svc := &mockSchedulerService{
		cancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
			return &service.CancelSchedulerJobResponse{
				JobID:     req.JobID,
				OldStatus: service.SchedulerJobStatusPending,
				NewStatus: service.SchedulerJobStatusCanceled,
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CancelSchedulerJobRequest{
		JobID:  123,
		Reason: "test cancellation",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerCancelJob, reqPayload)
	require.NoError(t, err)

	var cancelResp service.CancelSchedulerJobResponse
	err = DecodePayload(resp, &cancelResp)
	require.NoError(t, err)

	assert.Equal(t, uint64(123), cancelResp.JobID)
	assert.Equal(t, service.SchedulerJobStatusCanceled, cancelResp.NewStatus)
}

func TestHandleGetJobHistory(t *testing.T) {
	now := time.Now()
	svc := &mockSchedulerService{
		getJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
			return &service.GetSchedulerJobHistoryResponse{
				Executions: []*service.SchedulerJobExecution{
					{
						ID:        1,
						JobID:     req.JobID,
						Status:    service.SchedulerJobStatusCompleted,
						StartedAt: now.Add(-time.Hour),
						EndedAt:   now.Add(-time.Hour + time.Minute),
					},
				},
				HasMore:   false,
				Timestamp: now,
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobHistoryRequest{
		JobID: 123,
		Limit: 10,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerGetJobHistory, reqPayload)
	require.NoError(t, err)

	var historyResp service.GetSchedulerJobHistoryResponse
	err = DecodePayload(resp, &historyResp)
	require.NoError(t, err)

	assert.Len(t, historyResp.Executions, 1)
	assert.Equal(t, uint64(123), historyResp.Executions[0].JobID)
}

func TestHandleGetStatus(t *testing.T) {
	svc := &mockSchedulerService{
		getStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
			return &service.GetSchedulerStatusResponse{
				Running: true,
				Stats: &service.SchedulerStats{
					JobCount:     100,
					RunningJobs:  5,
					SuccessCount: 500,
					FailureCount: 10,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSchedulerGetStatus, nil)
	require.NoError(t, err)

	var statusResp service.GetSchedulerStatusResponse
	err = DecodePayload(resp, &statusResp)
	require.NoError(t, err)

	assert.True(t, statusResp.Running)
	assert.Equal(t, uint64(100), statusResp.Stats.JobCount)
	assert.Equal(t, uint64(5), statusResp.Stats.RunningJobs)
}

func TestHandleServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerGetJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandleRequestSuccess(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpSchedulerGetJob,
		Token:   "test-token",
		Payload: reqPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	assert.Equal(t, StatusOK, resp.Status)
	assert.NotEmpty(t, resp.Payload)
}

func TestHandleRequestWithError(t *testing.T) {
	svc := &mockSchedulerService{
		getJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpSchedulerGetJob,
		Token:   "test-token",
		Payload: reqPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	assert.Equal(t, StatusNotFound, resp.Status)
}

func TestHandleInvalidPayload(t *testing.T) {
	handler, err := NewHandler(&mockSchedulerService{}, &testLogger{})
	require.NoError(t, err)

	// Send invalid JSON payload
	_, err = handler.Handle(context.Background(), OpSchedulerGetJob, []byte("invalid json"))
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}

func TestHandleCreateJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		createJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrInvalidInput,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CreateSchedulerJobRequest{
		Name:    "test-job",
		Handler: "test-handler",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerCreateJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestHandleUpdateJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		updateJobFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.UpdateSchedulerJobRequest{
		JobID: 999,
		Name:  "updated-job",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerUpdateJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandleDeleteJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		deleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.DeleteSchedulerJobRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerDeleteJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandleListJobsServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrInternal,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListSchedulerJobsRequest{
		Limit: 10,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerListJobs, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}

func TestHandleTriggerJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		triggerJobFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.TriggerSchedulerJobRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerTriggerJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandlePauseJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		pauseJobFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrInvalidInput,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.PauseSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerPauseJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestHandleResumeJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		resumeJobFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrInvalidInput,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ResumeSchedulerJobRequest{
		JobID: 123,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerResumeJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestHandleCancelJobServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		cancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CancelSchedulerJobRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerCancelJob, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandleGetJobHistoryServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetSchedulerJobHistoryRequest{
		JobID: 999,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerGetJobHistory, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestHandleGetStatusServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrInternal,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, &testLogger{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpSchedulerGetStatus, nil)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}
