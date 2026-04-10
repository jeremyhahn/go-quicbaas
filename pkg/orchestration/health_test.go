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

package orchestration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Mock AgentService for Health Monitor Tests
// =============================================================================

// healthMockAgentService is a minimal mock of AgentService used by health
// monitor tests. Only ListSessions, SessionStatus, and HealthCheck are
// exercised; other methods return zero values.
type healthMockAgentService struct {
	listSessionsFn  func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)
	sessionStatusFn func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)
}

func (m *healthMockAgentService) DiscoverProjects(ctx context.Context, req *DiscoverProjectsRequest) (*DiscoverProjectsResponse, error) {
	return &DiscoverProjectsResponse{}, nil
}

func (m *healthMockAgentService) RegisterProject(ctx context.Context, req *RegisterProjectRequest) (*RegisterProjectResponse, error) {
	return &RegisterProjectResponse{}, nil
}

func (m *healthMockAgentService) ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error) {
	return &ListProjectsResponse{}, nil
}

func (m *healthMockAgentService) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	return &SpawnSessionResponse{}, nil
}

func (m *healthMockAgentService) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	return &PromptSessionResponse{}, nil
}

func (m *healthMockAgentService) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if m.sessionStatusFn != nil {
		return m.sessionStatusFn(ctx, req)
	}
	return &SessionStatusResponse{
		SessionID: req.SessionID,
		State:     SessionStateActive,
	}, nil
}

func (m *healthMockAgentService) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	return &CloseSessionResponse{}, nil
}

func (m *healthMockAgentService) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	if m.listSessionsFn != nil {
		return m.listSessionsFn(ctx, req)
	}
	return &ListSessionsResponse{}, nil
}

func (m *healthMockAgentService) DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error) {
	return &DecomposeTaskResponse{}, nil
}

func (m *healthMockAgentService) ListBackends(ctx context.Context) ([]*AgentBackendInfo, error) {
	return nil, nil
}

func (m *healthMockAgentService) RegisterBackend(ctx context.Context, req *RegisterBackendRequest) error {
	return nil
}

func (m *healthMockAgentService) CreateTask(_ context.Context, _ *CreateTaskRequest) (*Task, error) {
	return &Task{}, nil
}

func (m *healthMockAgentService) GetTask(_ context.Context, _, _ string) (*Task, error) {
	return &Task{}, nil
}

func (m *healthMockAgentService) ListTasks(_ context.Context, _ string, _ *TaskFilter) ([]*Task, error) {
	return nil, nil
}

func (m *healthMockAgentService) ClaimTask(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *healthMockAgentService) CompleteTask(_ context.Context, _, _ string, _ *TaskResult) error {
	return nil
}

func (m *healthMockAgentService) RegisterAgent(_ context.Context, _ *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	return &RegisterAgentResponse{}, nil
}

func (m *healthMockAgentService) GetAgent(_ context.Context, _, _ string) (*AgentRegistration, error) {
	return &AgentRegistration{}, nil
}

func (m *healthMockAgentService) ListAgents(_ context.Context, _ string, _ *AgentFilter) ([]*AgentRegistration, error) {
	return nil, nil
}

func (m *healthMockAgentService) RemoveAgent(_ context.Context, _, _ string) error {
	return nil
}

func (m *healthMockAgentService) AgentHeartbeat(_ context.Context, _, _ string) error {
	return nil
}

func (m *healthMockAgentService) HealthCheck(ctx context.Context) (*HealthCheckReport, error) {
	monitor := NewAgentHealthMonitor(m, &agentTestLogger{})
	return monitor.CheckAll(ctx), nil
}

func (m *healthMockAgentService) StartJob(_ context.Context, _ *StartJobRequest) (*StartJobResponse, error) {
	return nil, nil
}

func (m *healthMockAgentService) GetJobStatus(_ context.Context, _, _ string) (*JobStatus, error) {
	return nil, nil
}

func (m *healthMockAgentService) ListJobs(_ context.Context, _ string) ([]*JobStatus, error) {
	return nil, nil
}

func (m *healthMockAgentService) CancelJob(_ context.Context, _, _ string) error {
	return nil
}

func (m *healthMockAgentService) SubscribeJobEvents(_ context.Context, _, _ string) (<-chan *JobEvent, error) {
	return nil, nil
}

func (m *healthMockAgentService) BackendToolGuidance(_ string) string {
	return ""
}

// Compile-time interface check.
var _ AgentService = (*healthMockAgentService)(nil)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewAgentHealthMonitor(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{}
	logger := &agentTestLogger{}

	monitor := NewAgentHealthMonitor(svc, logger)
	if monitor == nil {
		t.Fatal("expected non-nil monitor")
	}
	if monitor.interval != DefaultHealthCheckInterval {
		t.Errorf("expected interval %v, got %v", DefaultHealthCheckInterval, monitor.interval)
	}
	if monitor.timeout != DefaultHealthCheckTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultHealthCheckTimeout, monitor.timeout)
	}
	if monitor.running.Load() {
		t.Error("expected monitor to not be running initially")
	}
}

func TestNewAgentHealthMonitor_FieldsSet(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{}
	logger := &agentTestLogger{}

	monitor := NewAgentHealthMonitor(svc, logger)
	if monitor.service != AgentService(svc) {
		t.Error("expected service to be set")
	}
	if monitor.logger != logger {
		t.Error("expected logger to be set")
	}
	if monitor.done == nil {
		t.Error("expected done channel to be initialized")
	}
}

// =============================================================================
// CheckAll Tests
// =============================================================================

func TestAgentHealthMonitor_CheckAll_AllHealthy(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{
				Sessions: []*SessionInfo{
					{SessionID: "sess-1", Backend: "mock", State: SessionStateActive, CreatedAt: 1000},
					{SessionID: "sess-2", Backend: "mock", State: SessionStateIdle, CreatedAt: 2000},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			return &SessionStatusResponse{
				SessionID: req.SessionID,
				State:     SessionStateActive,
			}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	report := monitor.CheckAll(context.Background())

	if report.TotalSessions != 2 {
		t.Fatalf("expected 2 total sessions, got %d", report.TotalSessions)
	}
	if report.Healthy != 2 {
		t.Errorf("expected 2 healthy, got %d", report.Healthy)
	}
	if report.Unhealthy != 0 {
		t.Errorf("expected 0 unhealthy, got %d", report.Unhealthy)
	}
	if report.Closed != 0 {
		t.Errorf("expected 0 closed, got %d", report.Closed)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	for _, r := range report.Results {
		if !r.Healthy {
			t.Errorf("expected session %s to be healthy", r.SessionID)
		}
	}
}

func TestAgentHealthMonitor_CheckAll_SomeUnhealthy(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{
				Sessions: []*SessionInfo{
					{SessionID: "sess-healthy", Backend: "mock", State: SessionStateActive, CreatedAt: 1000},
					{SessionID: "sess-error", Backend: "mock", State: SessionStateActive, CreatedAt: 2000},
					{SessionID: "sess-closed", Backend: "mock", State: SessionStateActive, CreatedAt: 3000},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			switch req.SessionID {
			case "sess-healthy":
				return &SessionStatusResponse{
					SessionID: req.SessionID,
					State:     SessionStateActive,
				}, nil
			case "sess-error":
				return &SessionStatusResponse{
					SessionID: req.SessionID,
					State:     SessionStateError,
				}, nil
			case "sess-closed":
				return &SessionStatusResponse{
					SessionID: req.SessionID,
					State:     SessionStateClosed,
				}, nil
			default:
				return nil, &SessionNotFoundError{SessionID: req.SessionID}
			}
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	report := monitor.CheckAll(context.Background())

	if report.TotalSessions != 3 {
		t.Fatalf("expected 3 total sessions, got %d", report.TotalSessions)
	}
	if report.Healthy != 1 {
		t.Errorf("expected 1 healthy, got %d", report.Healthy)
	}
	if report.Unhealthy != 1 {
		t.Errorf("expected 1 unhealthy, got %d", report.Unhealthy)
	}
	if report.Closed != 1 {
		t.Errorf("expected 1 closed, got %d", report.Closed)
	}
}

func TestAgentHealthMonitor_CheckAll_NoSessions(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{
				Sessions:  []*SessionInfo{},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	report := monitor.CheckAll(context.Background())

	if report.TotalSessions != 0 {
		t.Errorf("expected 0 total sessions, got %d", report.TotalSessions)
	}
	if report.Healthy != 0 {
		t.Errorf("expected 0 healthy, got %d", report.Healthy)
	}
	if report.Unhealthy != 0 {
		t.Errorf("expected 0 unhealthy, got %d", report.Unhealthy)
	}
	if len(report.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(report.Results))
	}
}

func TestAgentHealthMonitor_CheckAll_ListSessionsError(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "connection refused",
			}
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	report := monitor.CheckAll(context.Background())

	if report.TotalSessions != 0 {
		t.Errorf("expected 0 total sessions on error, got %d", report.TotalSessions)
	}
	if report.Timestamp == 0 {
		t.Error("expected non-zero timestamp even on error")
	}
}

func TestAgentHealthMonitor_CheckAll_StatusError(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{
				Sessions: []*SessionInfo{
					{SessionID: "sess-dead", Backend: "mock", State: SessionStateActive, CreatedAt: 1000},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			return nil, &SessionNotFoundError{SessionID: req.SessionID}
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	report := monitor.CheckAll(context.Background())

	if report.Unhealthy != 1 {
		t.Errorf("expected 1 unhealthy, got %d", report.Unhealthy)
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(report.Results))
	}
	if report.Results[0].Healthy {
		t.Error("expected session to be unhealthy")
	}
	if report.Results[0].Error == "" {
		t.Error("expected error message to be set")
	}
	if report.Results[0].State != SessionStateError {
		t.Errorf("expected error state, got %q", report.Results[0].State)
	}
}

// =============================================================================
// CheckSession Tests
// =============================================================================

func TestAgentHealthMonitor_CheckSession(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			return &SessionStatusResponse{
				SessionID: req.SessionID,
				State:     SessionStateIdle,
				Uptime:    3600,
			}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	result, err := monitor.CheckSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Healthy {
		t.Error("expected session to be healthy")
	}
	if result.State != SessionStateIdle {
		t.Errorf("expected idle state, got %q", result.State)
	}
	if result.SessionID != "sess-1" {
		t.Errorf("expected session ID 'sess-1', got %q", result.SessionID)
	}
}

func TestAgentHealthMonitor_CheckSession_Error(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			return nil, &SessionNotFoundError{SessionID: req.SessionID}
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	result, err := monitor.CheckSession(context.Background(), "sess-gone")
	if err != nil {
		t.Fatalf("expected no error (error is in result), got %v", err)
	}
	if result.Healthy {
		t.Error("expected session to be unhealthy")
	}
	if result.State != SessionStateError {
		t.Errorf("expected error state, got %q", result.State)
	}
	if result.Error == "" {
		t.Error("expected error message in result")
	}
}

func TestAgentHealthMonitor_CheckSession_EmptyID(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{}
	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})

	_, err := monitor.CheckSession(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrSessionIDRequired) {
		t.Fatalf("expected ErrSessionIDRequired, got %v", err)
	}
}

func TestAgentHealthMonitor_CheckSession_ClosedState(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		sessionStatusFn: func(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
			return &SessionStatusResponse{
				SessionID: req.SessionID,
				State:     SessionStateClosed,
			}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	result, err := monitor.CheckSession(context.Background(), "sess-closed")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Healthy {
		t.Error("expected closed session to be unhealthy")
	}
	if result.State != SessionStateClosed {
		t.Errorf("expected closed state, got %q", result.State)
	}
}

// =============================================================================
// Start/Stop Tests
// =============================================================================

func TestAgentHealthMonitor_StartStop(t *testing.T) {
	t.Parallel()

	var checkCount atomic.Int64

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			checkCount.Add(1)
			return &ListSessionsResponse{
				Sessions:  []*SessionInfo{},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	monitor.interval = 10 * time.Millisecond

	ctx := context.Background()
	if err := monitor.Start(ctx); err != nil {
		t.Fatalf("expected no error starting monitor, got %v", err)
	}

	if !monitor.running.Load() {
		t.Error("expected monitor to be running after Start")
	}

	// Wait for at least 2 ticks
	time.Sleep(50 * time.Millisecond)

	if err := monitor.Stop(); err != nil {
		t.Fatalf("expected no error stopping monitor, got %v", err)
	}

	if monitor.running.Load() {
		t.Error("expected monitor to not be running after Stop")
	}

	count := checkCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 health checks, got %d", count)
	}
}

func TestAgentHealthMonitor_StartStop_AlreadyRunning(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	monitor.interval = 100 * time.Millisecond

	ctx := context.Background()
	if err := monitor.Start(ctx); err != nil {
		t.Fatalf("expected no error on first start, got %v", err)
	}
	defer monitor.Stop()

	// Second start should return error
	err := monitor.Start(ctx)
	if err == nil {
		t.Fatal("expected error on double start")
	}
	if !errors.Is(err, ErrHealthMonitorAlreadyRunning) {
		t.Fatalf("expected ErrHealthMonitorAlreadyRunning, got %v", err)
	}
}

func TestAgentHealthMonitor_Stop_NotRunning(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{}
	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})

	err := monitor.Stop()
	if err == nil {
		t.Fatal("expected error stopping non-running monitor")
	}
	if !errors.Is(err, ErrHealthMonitorNotRunning) {
		t.Fatalf("expected ErrHealthMonitorNotRunning, got %v", err)
	}
}

func TestAgentHealthMonitor_StartStop_ContextCancellation(t *testing.T) {
	t.Parallel()

	var checkCount atomic.Int64

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			checkCount.Add(1)
			return &ListSessionsResponse{}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	monitor.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	if err := monitor.Start(ctx); err != nil {
		t.Fatalf("expected no error starting monitor, got %v", err)
	}

	// Let it tick a couple times
	time.Sleep(50 * time.Millisecond)

	// Cancel the context, which should stop the goroutine
	cancel()

	// Wait for goroutine to exit
	time.Sleep(30 * time.Millisecond)

	if monitor.running.Load() {
		t.Error("expected monitor to stop running after context cancellation")
	}
}

func TestAgentHealthMonitor_Restart(t *testing.T) {
	t.Parallel()

	svc := &healthMockAgentService{
		listSessionsFn: func(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
			return &ListSessionsResponse{}, nil
		},
	}

	monitor := NewAgentHealthMonitor(svc, &agentTestLogger{})
	monitor.interval = 10 * time.Millisecond

	ctx := context.Background()

	// Start and stop
	if err := monitor.Start(ctx); err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := monitor.Stop(); err != nil {
		t.Fatalf("first stop failed: %v", err)
	}

	// Restart should succeed
	if err := monitor.Start(ctx); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := monitor.Stop(); err != nil {
		t.Fatalf("second stop failed: %v", err)
	}
}
