//go:build quic

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

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock SchedulerService
// =============================================================================

type mockSchedulerService struct {
	createJobFn     func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	getJobFn        func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	updateJobFn     func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	deleteJobFn     func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	listJobsFn      func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	triggerJobFn    func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	pauseJobFn      func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	resumeJobFn     func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	cancelJobFn     func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	getJobHistoryFn func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	getStatusFn     func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
}

func (m *mockSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if m.createJobFn != nil {
		return m.createJobFn(ctx, req)
	}
	return &service.CreateSchedulerJobResponse{
		Job:       &service.SchedulerJob{ID: 1, Name: req.Name},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.getJobFn != nil {
		return m.getJobFn(ctx, req)
	}
	return &service.GetSchedulerJobResponse{
		Job:       &service.SchedulerJob{ID: req.JobID, Name: "test-job"},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.updateJobFn != nil {
		return m.updateJobFn(ctx, req)
	}
	return &service.UpdateSchedulerJobResponse{
		Job:       &service.SchedulerJob{ID: req.JobID, Name: req.Name},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.deleteJobFn != nil {
		return m.deleteJobFn(ctx, req)
	}
	return &service.DeleteSchedulerJobResponse{
		JobID:     req.JobID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.listJobsFn != nil {
		return m.listJobsFn(ctx, req)
	}
	return &service.ListSchedulerJobsResponse{
		Jobs:      []*service.SchedulerJob{},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.triggerJobFn != nil {
		return m.triggerJobFn(ctx, req)
	}
	return &service.TriggerSchedulerJobResponse{
		JobID:     req.JobID,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.pauseJobFn != nil {
		return m.pauseJobFn(ctx, req)
	}
	return &service.PauseSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusRunning,
		NewStatus: service.SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.resumeJobFn != nil {
		return m.resumeJobFn(ctx, req)
	}
	return &service.ResumeSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.cancelJobFn != nil {
		return m.cancelJobFn(ctx, req)
	}
	return &service.CancelSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: service.SchedulerJobStatusRunning,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.getJobHistoryFn != nil {
		return m.getJobHistoryFn(ctx, req)
	}
	return &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{},
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.getStatusFn != nil {
		return m.getStatusFn(ctx)
	}
	return &service.GetSchedulerStatusResponse{
		Running:   true,
		Stats:     &service.SchedulerStats{JobCount: 5},
		Timestamp: time.Now(),
	}, nil
}

// mockServiceWithScheduler embeds mockService and returns a scheduler service.
type mockServiceWithScheduler struct {
	mockService
	schedulerSvc service.SchedulerService
}

func (m *mockServiceWithScheduler) Scheduler() service.SchedulerService {
	return m.schedulerSvc
}

// =============================================================================
// Helper to build a handler with scheduler support
// =============================================================================

func createHandlerWithScheduler(schedSvc service.SchedulerService) *Handler {
	svc := &mockServiceWithScheduler{schedulerSvc: schedSvc}
	return NewHandler(svc, nil, nil, nil, nil, createTestLogger())
}

// =============================================================================
// Scheduler Dispatch Tests
// =============================================================================

func TestSchedulerDispatch_MissingSubOpcode(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerDispatch(ctx, &Request{
		Opcode:  OpScheduler,
		Payload: []byte{},
	})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for missing sub-opcode")
	}
}

func TestSchedulerDispatch_UnknownSubOpcode(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	// Use an unused sub-opcode in scheduler range
	resp := h.handleSchedulerDispatch(ctx, &Request{
		Opcode:  OpScheduler,
		Payload: []byte{0x1F},
	})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for unknown sub-opcode")
	}
}

func TestSchedulerDispatch_DelegatesToMessaging(t *testing.T) {
	// When sub-opcode is in messaging range (0x20-0x2F), it should delegate
	// to messaging handler. Since msgService is nil, it should return error.
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerDispatch(ctx, &Request{
		Opcode:  OpScheduler,
		Payload: []byte{SubOpMsgCreateChannel}, // 0x20 - messaging range
	})
	// Messaging service not set, so should get an error
	if resp.Status == StatusOK {
		t.Fatal("Expected error when delegating to messaging with nil service")
	}
}

func TestSchedulerDispatch_DelegatesToVectorExtended(t *testing.T) {
	// When sub-opcode is in vector extended range (0x40-0x4F), it should delegate
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerDispatch(ctx, &Request{
		Opcode:  OpScheduler,
		Payload: []byte{SubOpVectorBatchDelete}, // 0x40 - vector extended range
	})
	// Vector service not set, so should get an error
	if resp.Status == StatusOK {
		t.Fatal("Expected error when delegating to vector extended with nil service")
	}
}

// =============================================================================
// Scheduler CRUD Handler Tests
// =============================================================================

func TestSchedulerCreateJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, err := EncodePayload(&service.CreateSchedulerJobRequest{
		Name:    "test-job",
		Handler: "backup",
		Type:    service.SchedulerJobTypeOneShot,
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	resp := h.handleSchedulerCreateJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerCreateJob_ServiceUnavailable(t *testing.T) {
	// Use base mockService which returns nil Scheduler()
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CreateSchedulerJobRequest{Name: "test"})
	resp := h.handleSchedulerCreateJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerCreateJob_DecodeError(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerCreateJob(ctx, &Request{Payload: []byte("{invalid")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestSchedulerCreateJob_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		createJobFn: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "test", Err: &errors.MessageError{Message: "bad input"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CreateSchedulerJobRequest{Name: "test"})
	resp := h.handleSchedulerCreateJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerGetJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerGetJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerGetJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerGetJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerGetJob_DecodeError(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerGetJob(ctx, &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestSchedulerGetJob_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getJobFn: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "not found"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobRequest{JobID: 999})
	resp := h.handleSchedulerGetJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerUpdateJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.UpdateSchedulerJobRequest{JobID: 1, Name: "updated"})
	resp := h.handleSchedulerUpdateJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerUpdateJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.UpdateSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerUpdateJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerUpdateJob_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		updateJobFn: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "not found"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.UpdateSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerUpdateJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerDeleteJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.DeleteSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerDeleteJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerDeleteJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.DeleteSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerDeleteJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerDeleteJob_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		deleteJobFn: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "not found"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.DeleteSchedulerJobRequest{JobID: 999})
	resp := h.handleSchedulerDeleteJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerListJobs_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.ListSchedulerJobsRequest{Limit: 10})
	resp := h.handleSchedulerListJobs(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerListJobs_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.ListSchedulerJobsRequest{})
	resp := h.handleSchedulerListJobs(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerListJobs_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		listJobsFn: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "internal"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.ListSchedulerJobsRequest{})
	resp := h.handleSchedulerListJobs(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

// =============================================================================
// Scheduler Control Handler Tests
// =============================================================================

func TestSchedulerTriggerJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.TriggerSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerTriggerJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerTriggerJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.TriggerSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerTriggerJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerTriggerJob_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		triggerJobFn: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "not found"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.TriggerSchedulerJobRequest{JobID: 999})
	resp := h.handleSchedulerTriggerJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerPauseJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.PauseSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerPauseJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerPauseJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.PauseSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerPauseJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerResumeJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.ResumeSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerResumeJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerResumeJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.ResumeSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerResumeJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerCancelJob_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CancelSchedulerJobRequest{JobID: 1, Reason: "test"})
	resp := h.handleSchedulerCancelJob(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerCancelJob_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CancelSchedulerJobRequest{JobID: 1})
	resp := h.handleSchedulerCancelJob(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerGetJobHistory_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobHistoryRequest{JobID: 1, Limit: 10})
	resp := h.handleSchedulerGetJobHistory(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerGetJobHistory_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobHistoryRequest{JobID: 1})
	resp := h.handleSchedulerGetJobHistory(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerGetJobHistory_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getJobHistoryFn: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "not found"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetSchedulerJobHistoryRequest{JobID: 999})
	resp := h.handleSchedulerGetJobHistory(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestSchedulerGetStatus_Success(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	resp := h.handleSchedulerGetStatus(ctx, &Request{Payload: []byte("{}")})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestSchedulerGetStatus_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleSchedulerGetStatus(ctx, &Request{Payload: []byte("{}")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when scheduler service is nil")
	}
}

func TestSchedulerGetStatus_ServiceError(t *testing.T) {
	svc := &mockSchedulerService{
		getStatusFn: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "internal"}}
		},
	}
	h := createHandlerWithScheduler(svc)
	ctx := context.Background()

	resp := h.handleSchedulerGetStatus(ctx, &Request{Payload: []byte("{}")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

// =============================================================================
// Scheduler Dispatch via Sub-Opcodes
// =============================================================================

func TestSchedulerDispatch_AllSubOpcodes(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	ctx := context.Background()

	tests := []struct {
		name      string
		subOpcode byte
		payload   interface{}
	}{
		{"CreateJob", SubOpSchedulerCreateJob, &service.CreateSchedulerJobRequest{Name: "j", Handler: "h"}},
		{"GetJob", SubOpSchedulerGetJob, &service.GetSchedulerJobRequest{JobID: 1}},
		{"UpdateJob", SubOpSchedulerUpdateJob, &service.UpdateSchedulerJobRequest{JobID: 1}},
		{"DeleteJob", SubOpSchedulerDeleteJob, &service.DeleteSchedulerJobRequest{JobID: 1}},
		{"ListJobs", SubOpSchedulerListJobs, &service.ListSchedulerJobsRequest{}},
		{"TriggerJob", SubOpSchedulerTriggerJob, &service.TriggerSchedulerJobRequest{JobID: 1}},
		{"PauseJob", SubOpSchedulerPauseJob, &service.PauseSchedulerJobRequest{JobID: 1}},
		{"ResumeJob", SubOpSchedulerResumeJob, &service.ResumeSchedulerJobRequest{JobID: 1}},
		{"CancelJob", SubOpSchedulerCancelJob, &service.CancelSchedulerJobRequest{JobID: 1}},
		{"GetJobHistory", SubOpSchedulerGetJobHistory, &service.GetSchedulerJobHistoryRequest{JobID: 1}},
		{"GetStatus", SubOpSchedulerGetStatus, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payloadBytes []byte
			if tt.payload != nil {
				var encErr error
				payloadBytes, encErr = EncodePayload(tt.payload)
				if encErr != nil {
					t.Fatalf("Failed to encode: %v", encErr)
				}
			} else {
				payloadBytes = []byte("{}")
			}

			// Prepend sub-opcode
			fullPayload := append([]byte{tt.subOpcode}, payloadBytes...)
			resp := h.handleSchedulerDispatch(ctx, &Request{
				Opcode:  OpScheduler,
				Payload: fullPayload,
			})
			if resp.Status != StatusOK {
				t.Fatalf("Expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// getSchedulerService Tests
// =============================================================================

func TestGetSchedulerService_NotImplemented(t *testing.T) {
	h := createTestHandler()
	svc := h.getSchedulerService()
	if svc != nil {
		t.Fatal("Expected nil scheduler service from base mock")
	}
}

func TestGetSchedulerService_WithScheduler(t *testing.T) {
	h := createHandlerWithScheduler(&mockSchedulerService{})
	svc := h.getSchedulerService()
	if svc == nil {
		t.Fatal("Expected non-nil scheduler service")
	}
}
