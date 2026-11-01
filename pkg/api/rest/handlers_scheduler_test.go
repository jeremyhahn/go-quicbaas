//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Mock Scheduler Service
// =============================================================================

// MockSchedulerService is a mock implementation of service.SchedulerService.
type MockSchedulerService struct {
	mock.Mock
}

func NewMockSchedulerService() *MockSchedulerService {
	return &MockSchedulerService{}
}

func (m *MockSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CreateSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.UpdateSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListSchedulerJobsResponse), args.Error(1)
}

func (m *MockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TriggerSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.PauseSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ResumeSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CancelSchedulerJobResponse), args.Error(1)
}

func (m *MockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetSchedulerJobHistoryResponse), args.Error(1)
}

func (m *MockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetSchedulerStatusResponse), args.Error(1)
}

// =============================================================================
// Extended Mock QuicBaaS with Scheduler Support
// =============================================================================

// MockQuicBaaSWithScheduler extends MockQuicBaaS with scheduler service support.
type MockQuicBaaSWithScheduler struct {
	*testutil.MockQuicBaaS
	schedulerService *MockSchedulerService
}

func NewMockQuicBaaSWithScheduler() *MockQuicBaaSWithScheduler {
	return &MockQuicBaaSWithScheduler{
		MockQuicBaaS:     testutil.NewMockQuicBaaS(),
		schedulerService: NewMockSchedulerService(),
	}
}

func (m *MockQuicBaaSWithScheduler) Scheduler() service.SchedulerService {
	return m.schedulerService
}

// =============================================================================
// Test Helper Functions
// =============================================================================

// setupSchedulerMockServer creates a test server with mock scheduler service.
func setupSchedulerMockServer(t *testing.T) (*Server, *MockQuicBaaSWithScheduler, func()) {
	t.Helper()

	mockService := NewMockQuicBaaSWithScheduler()

	server, _, cleanup := setupMockServer(t)

	// Replace the service with our scheduler-aware mock
	server.service = mockService

	return server, mockService, cleanup
}

// MockQuicBaaSWithNilScheduler extends MockQuicBaaS with nil scheduler service.
type MockQuicBaaSWithNilScheduler struct {
	*testutil.MockQuicBaaS
}

func (m *MockQuicBaaSWithNilScheduler) Scheduler() service.SchedulerService {
	return nil
}

// setupNilSchedulerMockServer creates a test server with nil scheduler service.
func setupNilSchedulerMockServer(t *testing.T) (*Server, func()) {
	t.Helper()

	mockService := &MockQuicBaaSWithNilScheduler{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
	}

	server, _, cleanup := setupMockServer(t)

	// Replace the service with our nil-scheduler mock
	server.service = mockService

	return server, cleanup
}

// createTestSchedulerJob creates a test SchedulerJob for use in tests.
func createTestSchedulerJob(id uint64) *service.SchedulerJob {
	now := time.Now()
	return &service.SchedulerJob{
		ID:             id,
		Name:           "test-job",
		Description:    "A test job",
		OwnerType:      service.SchedulerOwnerTypeSystem,
		OwnerID:        "",
		TargetTenantID: "",
		CreatedBy:      "admin",
		Type:           service.SchedulerJobTypeRecurring,
		Schedule:       "*/5 * * * *",
		ExecuteAt:      time.Time{},
		Delay:          0,
		Handler:        "test-handler",
		Payload:        []byte(`{"key":"value"}`),
		Priority:       5,
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		Status:         service.SchedulerJobStatusPending,
		LastRun:        time.Time{},
		NextRun:        now.Add(5 * time.Minute),
		RunCount:       0,
		FailCount:      0,
		LastError:      "",
		Labels:         map[string]string{"env": "test"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// =============================================================================
// HandleCreateSchedulerJob Tests
// =============================================================================

func TestHandleCreateSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	testJob := createTestSchedulerJob(1)

	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.MatchedBy(func(req *service.CreateSchedulerJobRequest) bool {
		return req.Name == "backup-job" && req.Handler == "backup-handler" && req.Type == service.SchedulerJobTypeRecurring
	})).Return(&service.CreateSchedulerJobResponse{
		Job:       testJob,
		Timestamp: time.Now(),
	}, nil).Once()

	reqBody := CreateSchedulerJobRESTRequest{
		Name:           "backup-job",
		Description:    "Daily backup job",
		OwnerType:      "system",
		Type:           "recurring",
		Schedule:       "0 0 * * *",
		Handler:        "backup-handler",
		Payload:        json.RawMessage(`{"target":"db"}`),
		Priority:       10,
		TimeoutSeconds: 3600,
		MaxRetries:     3,
		Labels:         map[string]string{"type": "backup"},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp CreateSchedulerJobRESTResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Job)
	assert.Equal(t, uint64(1), resp.Job.ID)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleCreateSchedulerJob_WithExecuteAt(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	testJob := createTestSchedulerJob(2)
	testJob.Type = service.SchedulerJobTypeOneShot

	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).Return(&service.CreateSchedulerJobResponse{
		Job:       testJob,
		Timestamp: time.Now(),
	}, nil).Once()

	executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	reqBody := CreateSchedulerJobRESTRequest{
		Name:      "one-shot-job",
		Type:      "one-shot",
		ExecuteAt: executeAt,
		Handler:   "oneshot-handler",
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleCreateSchedulerJob_InvalidRequest(t *testing.T) {
	server, _, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"MissingName", `{"type":"recurring","handler":"test-handler"}`, "name is required"},
		{"MissingHandler", `{"name":"test-job","type":"recurring"}`, "handler is required"},
		{"MissingType", `{"name":"test-job","handler":"test-handler"}`, "type is required"},
		{"InvalidJSON", "not-json", "invalid JSON"},
		{"EmptyBody", "", ""},
		{"InvalidExecuteAt", `{"name":"test-job","type":"one-shot","handler":"test-handler","execute_at":"invalid-timestamp"}`, "execute_at format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			server.HandleCreateSchedulerJob(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tt.wantMsg)
		})
	}
}

func TestHandleCreateSchedulerJob_ServiceError(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "CreateJob",
		Err:  assert.AnError,
	}).Once()

	reqBody := CreateSchedulerJobRESTRequest{
		Name:    "test-job",
		Type:    "recurring",
		Handler: "test-handler",
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleListSchedulerJobs Tests
// =============================================================================

func TestHandleListSchedulerJobs_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	jobs := []*service.SchedulerJob{
		createTestSchedulerJob(1),
		createTestSchedulerJob(2),
	}
	jobs[1].Name = "test-job-2"

	mockSvc.schedulerService.On("ListJobs", mock.Anything, mock.Anything).Return(&service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		NextCursor: "",
		HasMore:    false,
		TotalCount: 2,
		Timestamp:  time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs", nil)
	w := httptest.NewRecorder()

	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListSchedulerJobsRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Jobs, 2)
	assert.False(t, resp.HasMore)
	assert.Equal(t, 2, resp.TotalCount)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleListSchedulerJobs_WithFilters(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("ListJobs", mock.Anything, mock.MatchedBy(func(req *service.ListSchedulerJobsRequest) bool {
		return req.Status == service.SchedulerJobStatusPending &&
			req.Handler == "backup-handler" &&
			req.Limit == 50
	})).Return(&service.ListSchedulerJobsResponse{
		Jobs:      []*service.SchedulerJob{},
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs?status=pending&handler=backup-handler&limit=50", nil)
	w := httptest.NewRecorder()

	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleListSchedulerJobs_WithPagination(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("ListJobs", mock.Anything, mock.MatchedBy(func(req *service.ListSchedulerJobsRequest) bool {
		return req.Cursor == "cursor123" && req.Limit == 100
	})).Return(&service.ListSchedulerJobsResponse{
		Jobs:       []*service.SchedulerJob{createTestSchedulerJob(3)},
		NextCursor: "cursor456",
		HasMore:    true,
		TotalCount: 150,
		Timestamp:  time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs?cursor=cursor123&limit=100", nil)
	w := httptest.NewRecorder()

	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListSchedulerJobsRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "cursor456", resp.NextCursor)
	assert.True(t, resp.HasMore)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleListSchedulerJobs_ServiceError(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("ListJobs", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ListJobs",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs", nil)
	w := httptest.NewRecorder()

	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleGetSchedulerJob Tests
// =============================================================================

func TestHandleGetSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	testJob := createTestSchedulerJob(123)

	mockSvc.schedulerService.On("GetJob", mock.Anything, mock.MatchedBy(func(req *service.GetSchedulerJobRequest) bool {
		return req.JobID == 123
	})).Return(&service.GetSchedulerJobResponse{
		Job:       testJob,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetSchedulerJobRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Job)
	assert.Equal(t, uint64(123), resp.Job.ID)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerJob_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "GetJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/999", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleUpdateSchedulerJob Tests
// =============================================================================

func TestHandleUpdateSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	testJob := createTestSchedulerJob(456)
	testJob.Name = "updated-job"

	mockSvc.schedulerService.On("UpdateJob", mock.Anything, mock.MatchedBy(func(req *service.UpdateSchedulerJobRequest) bool {
		return req.JobID == 456 && req.Name == "updated-job"
	})).Return(&service.UpdateSchedulerJobResponse{
		Job:       testJob,
		Timestamp: time.Now(),
	}, nil).Once()

	reqBody := UpdateSchedulerJobRESTRequest{
		Name:        "updated-job",
		Description: "Updated description",
		Priority:    15,
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/456", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "456")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp UpdateSchedulerJobRESTResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Job)
	assert.Equal(t, "updated-job", resp.Job.Name)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleUpdateSchedulerJob_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/123", bytes.NewReader([]byte("not-json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON")
}

func TestHandleUpdateSchedulerJob_InvalidExecuteAt(t *testing.T) {
	server, _, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	reqBody := UpdateSchedulerJobRESTRequest{
		ExecuteAt: "not-a-date",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/123", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "execute_at format")
}

func TestHandleUpdateSchedulerJob_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("UpdateJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "UpdateJob",
		Err:  assert.AnError,
	}).Once()

	reqBody := UpdateSchedulerJobRESTRequest{Name: "updated"}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/999", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleDeleteSchedulerJob Tests
// =============================================================================

func TestHandleDeleteSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("DeleteJob", mock.Anything, mock.MatchedBy(func(req *service.DeleteSchedulerJobRequest) bool {
		return req.JobID == 789 && !req.Force
	})).Return(&service.DeleteSchedulerJobResponse{
		JobID:     789,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/789", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "789")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteSchedulerJobRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(789), resp.JobID)
	assert.True(t, resp.Deleted)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleDeleteSchedulerJob_WithForce(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("DeleteJob", mock.Anything, mock.MatchedBy(func(req *service.DeleteSchedulerJobRequest) bool {
		return req.JobID == 789 && req.Force
	})).Return(&service.DeleteSchedulerJobResponse{
		JobID:     789,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/789?force=true", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "789")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleDeleteSchedulerJob_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("DeleteJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DeleteJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/999", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleTriggerSchedulerJob Tests
// =============================================================================

func TestHandleTriggerSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("TriggerJob", mock.Anything, mock.MatchedBy(func(req *service.TriggerSchedulerJobRequest) bool {
		return req.JobID == 100
	})).Return(&service.TriggerSchedulerJobResponse{
		JobID:     100,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/100/trigger", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "100")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleTriggerSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp TriggerSchedulerJobRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), resp.JobID)
	assert.Equal(t, "running", resp.Status)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleTriggerSchedulerJob_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("TriggerJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "TriggerJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/999/trigger", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleTriggerSchedulerJob(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandlePauseSchedulerJob Tests
// =============================================================================

func TestHandlePauseSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("PauseJob", mock.Anything, mock.MatchedBy(func(req *service.PauseSchedulerJobRequest) bool {
		return req.JobID == 200
	})).Return(&service.PauseSchedulerJobResponse{
		JobID:     200,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/200/pause", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "200")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandlePauseSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp PauseSchedulerJobRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(200), resp.JobID)
	assert.Equal(t, "pending", resp.OldStatus)
	assert.Equal(t, "paused", resp.NewStatus)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandlePauseSchedulerJob_ServiceError(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("PauseJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "PauseJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/200/pause", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "200")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandlePauseSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleResumeSchedulerJob Tests
// =============================================================================

func TestHandleResumeSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("ResumeJob", mock.Anything, mock.MatchedBy(func(req *service.ResumeSchedulerJobRequest) bool {
		return req.JobID == 300
	})).Return(&service.ResumeSchedulerJobResponse{
		JobID:     300,
		OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusPending,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/300/resume", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "300")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleResumeSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ResumeSchedulerJobRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(300), resp.JobID)
	assert.Equal(t, "paused", resp.OldStatus)
	assert.Equal(t, "pending", resp.NewStatus)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleResumeSchedulerJob_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("ResumeJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "ResumeJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/999/resume", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleResumeSchedulerJob(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleCancelSchedulerJob Tests
// =============================================================================

func TestHandleCancelSchedulerJob_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("CancelJob", mock.Anything, mock.MatchedBy(func(req *service.CancelSchedulerJobRequest) bool {
		return req.JobID == 400 && req.Reason == "user requested"
	})).Return(&service.CancelSchedulerJobResponse{
		JobID:     400,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil).Once()

	reqBody := CancelSchedulerJobRESTRequest{
		Reason: "user requested",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/400/cancel", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "400")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleCancelSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CancelSchedulerJobRESTResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint64(400), resp.JobID)
	assert.Equal(t, "pending", resp.OldStatus)
	assert.Equal(t, "canceled", resp.NewStatus)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleCancelSchedulerJob_NoBody(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("CancelJob", mock.Anything, mock.MatchedBy(func(req *service.CancelSchedulerJobRequest) bool {
		return req.JobID == 400 && req.Reason == ""
	})).Return(&service.CancelSchedulerJobResponse{
		JobID:     400,
		OldStatus: service.SchedulerJobStatusRunning,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/400/cancel", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "400")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleCancelSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleCancelSchedulerJob_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/400/cancel", bytes.NewReader([]byte("not-json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "400")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleCancelSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON")
}

// =============================================================================
// HandleGetSchedulerJobHistory Tests
// =============================================================================

func TestHandleGetSchedulerJobHistory_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	executions := []*service.SchedulerJobExecution{
		{
			ID:        1,
			JobID:     500,
			Status:    service.SchedulerJobStatusCompleted,
			StartedAt: time.Now().Add(-1 * time.Hour),
			EndedAt:   time.Now().Add(-55 * time.Minute),
			Duration:  5 * time.Minute,
			Output:    "success",
		},
		{
			ID:        2,
			JobID:     500,
			Status:    service.SchedulerJobStatusFailed,
			StartedAt: time.Now().Add(-30 * time.Minute),
			EndedAt:   time.Now().Add(-28 * time.Minute),
			Duration:  2 * time.Minute,
			Error:     "timeout",
		},
	}

	mockSvc.schedulerService.On("GetJobHistory", mock.Anything, mock.MatchedBy(func(req *service.GetSchedulerJobHistoryRequest) bool {
		return req.JobID == 500
	})).Return(&service.GetSchedulerJobHistoryResponse{
		Executions: executions,
		NextCursor: "",
		HasMore:    false,
		Timestamp:  time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/500/history", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "500")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetSchedulerJobHistoryRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Executions, 2)
	assert.Equal(t, "completed", resp.Executions[0].Status)
	assert.Equal(t, "failed", resp.Executions[1].Status)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerJobHistory_WithPagination(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetJobHistory", mock.Anything, mock.MatchedBy(func(req *service.GetSchedulerJobHistoryRequest) bool {
		return req.JobID == 500 && req.Limit == 10 && req.Cursor == "abc"
	})).Return(&service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{},
		NextCursor: "def",
		HasMore:    true,
		Timestamp:  time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/500/history?limit=10&cursor=abc", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "500")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetSchedulerJobHistoryRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "def", resp.NextCursor)
	assert.True(t, resp.HasMore)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerJobHistory_NotFound(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetJobHistory", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "GetJobHistory",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/999/history", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// HandleGetSchedulerStatus Tests
// =============================================================================

func TestHandleGetSchedulerStatus_Success(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	stats := &service.SchedulerStats{
		JobCount:     100,
		RunningJobs:  5,
		SuccessCount: 500,
		FailureCount: 10,
		LastTickTime: time.Now().Add(-1 * time.Second),
		StartedAt:    time.Now().Add(-24 * time.Hour),
	}

	mockSvc.schedulerService.On("GetStatus", mock.Anything).Return(&service.GetSchedulerStatusResponse{
		Running:   true,
		Stats:     stats,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/status", nil)
	w := httptest.NewRecorder()

	server.HandleGetSchedulerStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetSchedulerStatusRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Running)
	assert.NotNil(t, resp.Stats)
	assert.Equal(t, uint64(100), resp.Stats.JobCount)
	assert.Equal(t, uint64(5), resp.Stats.RunningJobs)
	assert.Equal(t, uint64(500), resp.Stats.SuccessCount)
	assert.Equal(t, uint64(10), resp.Stats.FailureCount)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerStatus_NotRunning(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetStatus", mock.Anything).Return(&service.GetSchedulerStatusResponse{
		Running:   false,
		Stats:     nil,
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/status", nil)
	w := httptest.NewRecorder()

	server.HandleGetSchedulerStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetSchedulerStatusRESTResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.False(t, resp.Running)
	assert.Nil(t, resp.Stats)

	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerStatus_ServiceError(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetStatus", mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "GetStatus",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/status", nil)
	w := httptest.NewRecorder()

	server.HandleGetSchedulerStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// =============================================================================
// Scheduler Service Nil Tests
// =============================================================================

func TestSchedulerHandlers_SchedulerServiceNil(t *testing.T) {
	server, cleanup := setupNilSchedulerMockServer(t)
	defer cleanup()

	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		id      string
		handler func(*Server, http.ResponseWriter, *http.Request)
	}{
		{"Create", http.MethodPost, "/api/v1/scheduler/jobs", `{"name":"test-job","type":"recurring","handler":"test-handler"}`, "", (*Server).HandleCreateSchedulerJob},
		{"List", http.MethodGet, "/api/v1/scheduler/jobs", "", "", (*Server).HandleListSchedulerJobs},
		{"Get", http.MethodGet, "/api/v1/scheduler/jobs/123", "", "123", (*Server).HandleGetSchedulerJob},
		{"Update", http.MethodPut, "/api/v1/scheduler/jobs/123", `{"name":"updated"}`, "123", (*Server).HandleUpdateSchedulerJob},
		{"Delete", http.MethodDelete, "/api/v1/scheduler/jobs/123", "", "123", (*Server).HandleDeleteSchedulerJob},
		{"Trigger", http.MethodPost, "/api/v1/scheduler/jobs/123/trigger", "", "123", (*Server).HandleTriggerSchedulerJob},
		{"Pause", http.MethodPost, "/api/v1/scheduler/jobs/123/pause", "", "123", (*Server).HandlePauseSchedulerJob},
		{"Resume", http.MethodPost, "/api/v1/scheduler/jobs/123/resume", "", "123", (*Server).HandleResumeSchedulerJob},
		{"Cancel", http.MethodPost, "/api/v1/scheduler/jobs/123/cancel", "", "123", (*Server).HandleCancelSchedulerJob},
		{"History", http.MethodGet, "/api/v1/scheduler/jobs/123/history", "", "123", (*Server).HandleGetSchedulerJobHistory},
		{"Status", http.MethodGet, "/api/v1/scheduler/status", "", "", (*Server).HandleGetSchedulerStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.id != "" {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("id", tt.id)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			}
			w := httptest.NewRecorder()

			tt.handler(server, w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "scheduler service not available")
		})
	}
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestSchedulerJobToREST(t *testing.T) {
	now := time.Now()
	complete := &service.SchedulerJob{
		ID:             123,
		Name:           "test-job",
		Description:    "Test description",
		OwnerType:      service.SchedulerOwnerTypeTenant,
		OwnerID:        "tenant-1",
		TargetTenantID: "target-tenant",
		CreatedBy:      "admin",
		Type:           service.SchedulerJobTypeRecurring,
		Schedule:       "*/10 * * * *",
		ExecuteAt:      now.Add(1 * time.Hour),
		Delay:          5 * time.Minute,
		Handler:        "my-handler",
		Payload:        []byte(`{"key":"value"}`),
		Priority:       10,
		Timeout:        60 * time.Second,
		MaxRetries:     5,
		Status:         service.SchedulerJobStatusRunning,
		LastRun:        now.Add(-10 * time.Minute),
		NextRun:        now.Add(10 * time.Minute),
		RunCount:       50,
		FailCount:      2,
		LastError:      "previous error",
		Labels:         map[string]string{"env": "prod"},
		CreatedAt:      now.Add(-24 * time.Hour),
		UpdatedAt:      now,
	}

	tests := []struct {
		name string
		job  *service.SchedulerJob
		want *SchedulerJobRESTResponse
	}{
		{"Nil", nil, nil},
		{"Complete", complete, &SchedulerJobRESTResponse{
			ID:             123,
			Name:           "test-job",
			Description:    "Test description",
			OwnerType:      "tenant",
			OwnerID:        "tenant-1",
			TargetTenantID: "target-tenant",
			CreatedBy:      "admin",
			Type:           "recurring",
			Schedule:       "*/10 * * * *",
			ExecuteAt:      complete.ExecuteAt,
			Delay:          5 * time.Minute,
			Handler:        "my-handler",
			Payload:        []byte(`{"key":"value"}`),
			Priority:       10,
			Timeout:        60 * time.Second,
			MaxRetries:     5,
			Status:         "running",
			LastRun:        complete.LastRun,
			NextRun:        complete.NextRun,
			RunCount:       50,
			FailCount:      2,
			LastError:      "previous error",
			Labels:         map[string]string{"env": "prod"},
			CreatedAt:      complete.CreatedAt,
			UpdatedAt:      now,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schedulerJobToREST(tt.job))
		})
	}
}

func TestSchedulerExecutionToREST(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		exec *service.SchedulerJobExecution
		want *SchedulerJobExecutionRESTResponse
	}{
		{"Nil", nil, nil},
		{
			"Completed",
			&service.SchedulerJobExecution{
				ID:        1,
				JobID:     100,
				Status:    service.SchedulerJobStatusCompleted,
				StartedAt: now.Add(-10 * time.Minute),
				EndedAt:   now.Add(-5 * time.Minute),
				Duration:  5 * time.Minute,
				Output:    "task completed",
			},
			&SchedulerJobExecutionRESTResponse{
				ID:        1,
				JobID:     100,
				Status:    "completed",
				StartedAt: now.Add(-10 * time.Minute),
				EndedAt:   now.Add(-5 * time.Minute),
				Duration:  5 * time.Minute,
				Output:    "task completed",
			},
		},
		{
			"Failed",
			&service.SchedulerJobExecution{
				ID:        2,
				JobID:     100,
				Status:    service.SchedulerJobStatusFailed,
				StartedAt: now.Add(-5 * time.Minute),
				EndedAt:   now.Add(-4 * time.Minute),
				Duration:  1 * time.Minute,
				Error:     "connection timeout",
			},
			&SchedulerJobExecutionRESTResponse{
				ID:        2,
				JobID:     100,
				Status:    "failed",
				StartedAt: now.Add(-5 * time.Minute),
				EndedAt:   now.Add(-4 * time.Minute),
				Duration:  1 * time.Minute,
				Error:     "connection timeout",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schedulerExecutionToREST(tt.exec))
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestHandleListSchedulerJobs_OutOfRangeLimitIgnored(t *testing.T) {
	tests := []struct {
		name  string
		limit string
	}{
		{"AboveMax", "9999"},
		{"Negative", "-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupSchedulerMockServer(t)
			defer cleanup()

			// An out-of-range limit is ignored, so the service sees the default.
			mockSvc.schedulerService.On("ListJobs", mock.Anything, mock.MatchedBy(func(req *service.ListSchedulerJobsRequest) bool {
				return req.Limit == 0
			})).Return(&service.ListSchedulerJobsResponse{
				Jobs:      []*service.SchedulerJob{},
				Timestamp: time.Now(),
			}, nil).Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs?limit="+tt.limit, nil)
			w := httptest.NewRecorder()

			server.HandleListSchedulerJobs(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			mockSvc.schedulerService.AssertExpectations(t)
		})
	}
}

func TestHandleGetSchedulerJobHistory_InvalidLimit(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	// Invalid limit (> 1000) should be ignored
	mockSvc.schedulerService.On("GetJobHistory", mock.Anything, mock.MatchedBy(func(req *service.GetSchedulerJobHistoryRequest) bool {
		return req.JobID == 500 && req.Limit == 0
	})).Return(&service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{},
		Timestamp:  time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/500/history?limit=5000", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "500")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestSchedulerHandlers_JobIDValidation(t *testing.T) {
	server, _, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	handlers := []struct {
		name    string
		method  string
		suffix  string
		body    string
		handler func(*Server, http.ResponseWriter, *http.Request)
	}{
		{"Get", http.MethodGet, "", "", (*Server).HandleGetSchedulerJob},
		{"Update", http.MethodPut, "", `{"name":"updated"}`, (*Server).HandleUpdateSchedulerJob},
		{"Delete", http.MethodDelete, "", "", (*Server).HandleDeleteSchedulerJob},
		{"Trigger", http.MethodPost, "/trigger", "", (*Server).HandleTriggerSchedulerJob},
		{"Pause", http.MethodPost, "/pause", "", (*Server).HandlePauseSchedulerJob},
		{"Resume", http.MethodPost, "/resume", "", (*Server).HandleResumeSchedulerJob},
		{"Cancel", http.MethodPost, "/cancel", "", (*Server).HandleCancelSchedulerJob},
		{"History", http.MethodGet, "/history", "", (*Server).HandleGetSchedulerJobHistory},
	}
	cases := []struct {
		name    string
		id      string
		wantMsg string
	}{
		{"InvalidID", "invalid", "invalid job ID"},
		{"MissingID", "", "job ID is required"},
	}

	for _, h := range handlers {
		for _, tc := range cases {
			t.Run(h.name+"_"+tc.name, func(t *testing.T) {
				path := "/api/v1/scheduler/jobs/" + tc.id + h.suffix
				req := httptest.NewRequest(h.method, path, strings.NewReader(h.body))
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("id", tc.id)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
				w := httptest.NewRecorder()

				h.handler(server, w, req)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantMsg)
			})
		}
	}
}

// =============================================================================
// Authorization Error Tests
// =============================================================================

func TestHandleCreateSchedulerJob_Unauthorized(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrUnauthorized,
		Op:   "CreateJob",
		Err:  assert.AnError,
	}).Once()

	reqBody := CreateSchedulerJobRESTRequest{
		Name:    "test-job",
		Type:    "recurring",
		Handler: "test-handler",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

func TestHandleGetSchedulerJob_Forbidden(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	mockSvc.schedulerService.On("GetJob", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrForbidden,
		Op:   "GetJob",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	mockSvc.schedulerService.AssertExpectations(t)
}

// Placement returns nil: this double has no placement engine.
func (m *MockQuicBaaSWithScheduler) Placement() service.PlacementService { return nil }

// Placement returns nil: this double has no placement engine.
func (m *MockQuicBaaSWithNilScheduler) Placement() service.PlacementService { return nil }
