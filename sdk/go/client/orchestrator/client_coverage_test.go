// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// errTransport returns a fixed transport error.
func errTransport() error {
	return errors.NewInternal("mock", "transport boom")
}

// okTransport builds a mockTransport that returns the supplied response with
// no error and asserts the wire method name.
func okTransport(t *testing.T, wantMethod string, resp interface{}) *mockTransport {
	t.Helper()
	return &mockTransport{
		requestFn: func(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, wantMethod, method)
			return resp, nil
		},
	}
}

// failTransport builds a mockTransport whose Request always errors.
func failTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errTransport()
		},
	}
}

// nilTransport builds a mockTransport whose Request returns (nil, nil).
func nilTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
}

// mismatchTransport returns a value of the wrong concrete type so the
// type-assertion branch is exercised.
func mismatchTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return &struct{ X int }{X: 1}, nil
		},
	}
}

// =============================================================================
// SpawnSession
// =============================================================================

func TestSpawnSession_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "SpawnSession", &SpawnSessionResponse{}))
	resp, err := c.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "ollama"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestSpawnSession_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SpawnSession(context.Background(), nil)
	require.Error(t, err)
	_, err = c.SpawnSession(context.Background(), &SpawnSessionRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "an agent, a pool or a backend is required")
	_, err = c.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "a", Backend: "claude"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name one of")
}

func TestSpawnSession_ByAgentOrPoolNeedsNoBackend(t *testing.T) {
	for _, req := range []*SpawnSessionRequest{{AgentID: "claude-coder"}, {PoolID: "coders"}} {
		c := NewClient(okTransport(t, "SpawnSession", &SpawnSessionResponse{SessionID: "s", ResolvedAgentID: "claude-coder"}))
		resp, err := c.SpawnSession(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "claude-coder", resp.ResolvedAgentID)
	}
}

func TestSpawnSession_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "SpawnSession", &SpawnSessionResponse{}))
	//nolint:staticcheck // intentionally nil to exercise guard
	_, err := c.SpawnSession(nil, &SpawnSessionRequest{Backend: "ollama"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")
}

func TestSpawnSession_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "ollama"})
	require.Error(t, err)
}

func TestSpawnSession_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "ollama"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestSpawnSession_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "ollama"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid response type")
}

// =============================================================================
// PromptSession
// =============================================================================

func TestPromptSession_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "PromptSession", &PromptSessionResponse{}))
	resp, err := c.PromptSession(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestPromptSession_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.PromptSession(context.Background(), nil)
	require.Error(t, err)
	_, err = c.PromptSession(context.Background(), &PromptSessionRequest{Prompt: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session ID is required")
	_, err = c.PromptSession(context.Background(), &PromptSessionRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestPromptSession_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "PromptSession", &PromptSessionResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.PromptSession(nil, &PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.Error(t, err)
}

func TestPromptSession_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.PromptSession(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.Error(t, err)
}

func TestPromptSession_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.PromptSession(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.Error(t, err)
}

func TestPromptSession_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.PromptSession(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.Error(t, err)
}

// =============================================================================
// SessionStatus
// =============================================================================

func TestSessionStatus_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "SessionStatus", &SessionStatusResponse{}))
	resp, err := c.SessionStatus(context.Background(), &SessionStatusRequest{SessionID: "s1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestSessionStatus_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SessionStatus(context.Background(), nil)
	require.Error(t, err)
	_, err = c.SessionStatus(context.Background(), &SessionStatusRequest{})
	require.Error(t, err)
}

func TestSessionStatus_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "SessionStatus", &SessionStatusResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.SessionStatus(nil, &SessionStatusRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestSessionStatus_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SessionStatus(context.Background(), &SessionStatusRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestSessionStatus_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.SessionStatus(context.Background(), &SessionStatusRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestSessionStatus_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.SessionStatus(context.Background(), &SessionStatusRequest{SessionID: "s1"})
	require.Error(t, err)
}

// =============================================================================
// CloseSession
// =============================================================================

func TestCloseSession_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "CloseSession", &CloseSessionResponse{}))
	resp, err := c.CloseSession(context.Background(), &CloseSessionRequest{SessionID: "s1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCloseSession_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CloseSession(context.Background(), nil)
	require.Error(t, err)
	_, err = c.CloseSession(context.Background(), &CloseSessionRequest{})
	require.Error(t, err)
}

func TestCloseSession_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "CloseSession", &CloseSessionResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.CloseSession(nil, &CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestCloseSession_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CloseSession(context.Background(), &CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestCloseSession_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.CloseSession(context.Background(), &CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestCloseSession_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.CloseSession(context.Background(), &CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}

// =============================================================================
// ListSessions
// =============================================================================

func TestListSessions_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ListSessions", &ListSessionsResponse{Count: 2}))
	resp, err := c.ListSessions(context.Background(), &ListSessionsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.Count)
}

func TestListSessions_NilRequest(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListSessions(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is required")
}

func TestListSessions_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListSessions", &ListSessionsResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListSessions(nil, &ListSessionsRequest{})
	require.Error(t, err)
}

func TestListSessions_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListSessions(context.Background(), &ListSessionsRequest{})
	require.Error(t, err)
}

func TestListSessions_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListSessions(context.Background(), &ListSessionsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Sessions)
}

func TestListSessions_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListSessions(context.Background(), &ListSessionsRequest{})
	require.Error(t, err)
}

// =============================================================================
// GetTask
// =============================================================================

func TestGetTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "GetTask", &Task{}))
	resp, err := c.GetTask(context.Background(), &GetTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestGetTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetTask(context.Background(), nil)
	require.Error(t, err)
	_, err = c.GetTask(context.Background(), &GetTaskRequest{TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
	_, err = c.GetTask(context.Background(), &GetTaskRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID is required")
}

func TestGetTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "GetTask", &Task{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.GetTask(nil, &GetTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestGetTask_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetTask(context.Background(), &GetTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestGetTask_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.GetTask(context.Background(), &GetTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestGetTask_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetTask(context.Background(), &GetTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

// =============================================================================
// ListTasks
// =============================================================================

func TestListTasks_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ListTasks", &ListTasksResponse{Count: 3}))
	resp, err := c.ListTasks(context.Background(), &ListTasksRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 3, resp.Count)
}

func TestListTasks_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListTasks(context.Background(), nil)
	require.Error(t, err)
	_, err = c.ListTasks(context.Background(), &ListTasksRequest{})
	require.Error(t, err)
}

func TestListTasks_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListTasks", &ListTasksResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListTasks(nil, &ListTasksRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListTasks_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListTasks(context.Background(), &ListTasksRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListTasks_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListTasks(context.Background(), &ListTasksRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Tasks)
}

func TestListTasks_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListTasks(context.Background(), &ListTasksRequest{TenantID: "t1"})
	require.Error(t, err)
}

// =============================================================================
// ClaimTask
// =============================================================================

func TestClaimTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ClaimTask", &ClaimTaskResponse{}))
	resp, err := c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1", AgentID: "a1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClaimTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ClaimTask(context.Background(), nil)
	require.Error(t, err)
	_, err = c.ClaimTask(context.Background(), &ClaimTaskRequest{TaskID: "tk1", AgentID: "a1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
	_, err = c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", AgentID: "a1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID is required")
	_, err = c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent ID is required")
}

func TestClaimTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ClaimTask", &ClaimTaskResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ClaimTask(nil, &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1", AgentID: "a1"})
	require.Error(t, err)
}

func TestClaimTask_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1", AgentID: "a1"})
	require.Error(t, err)
}

func TestClaimTask_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1", AgentID: "a1"})
	require.Error(t, err)
}

func TestClaimTask_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ClaimTask(context.Background(), &ClaimTaskRequest{TenantID: "t1", TaskID: "tk1", AgentID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// CompleteTask
// =============================================================================

func TestCompleteTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "CompleteTask", &CompleteTaskResponse{}))
	resp, err := c.CompleteTask(context.Background(), &CompleteTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCompleteTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CompleteTask(context.Background(), nil)
	require.Error(t, err)
	_, err = c.CompleteTask(context.Background(), &CompleteTaskRequest{TaskID: "tk1"})
	require.Error(t, err)
	_, err = c.CompleteTask(context.Background(), &CompleteTaskRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestCompleteTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "CompleteTask", &CompleteTaskResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.CompleteTask(nil, &CompleteTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestCompleteTask_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CompleteTask(context.Background(), &CompleteTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestCompleteTask_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.CompleteTask(context.Background(), &CompleteTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestCompleteTask_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.CompleteTask(context.Background(), &CompleteTaskRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

// =============================================================================
// ListBackends
// =============================================================================

func TestListBackends_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ListBackends", &ListBackendsResponse{Count: 1}))
	resp, err := c.ListBackends(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Count)
}

func TestListBackends_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListBackends", &ListBackendsResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListBackends(nil)
	require.Error(t, err)
}

func TestListBackends_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListBackends(context.Background())
	require.Error(t, err)
}

func TestListBackends_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListBackends(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Backends)
}

func TestListBackends_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListBackends(context.Background())
	require.Error(t, err)
}

// =============================================================================
// ListRunners
// =============================================================================

func TestListRunners_HappyPath(t *testing.T) {
	want := &ListJobRunnersResponse{
		Runners: []*JobRunnerInfo{{
			Name: "workflow",
			States: []JobStateSpec{
				{State: "initializing", Label: "Initializing"},
				{State: "executing", Label: "Executing", SessionBacked: true},
			},
		}},
		Count: 1,
	}
	c := NewClient(okTransport(t, "ListRunners", want))
	resp, err := c.ListRunners(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Runners, 1)
	assert.Equal(t, "workflow", resp.Runners[0].Name)
	assert.Equal(t, "initializing", resp.Runners[0].States[0].State)
	assert.True(t, resp.Runners[0].States[1].SessionBacked)
	assert.Equal(t, 1, resp.Count)
}

func TestListRunners_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListRunners", &ListJobRunnersResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListRunners(nil)
	require.Error(t, err)
}

func TestListRunners_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListRunners(context.Background())
	require.Error(t, err)
}

func TestListRunners_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListRunners(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Runners)
}

func TestListRunners_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListRunners(context.Background())
	require.Error(t, err)
}

// =============================================================================
// Health
// =============================================================================

func TestHealth_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "Health", &HealthCheckReport{}))
	resp, err := c.Health(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestHealth_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "Health", &HealthCheckReport{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.Health(nil)
	require.Error(t, err)
}

func TestHealth_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.Health(context.Background())
	require.Error(t, err)
}

func TestHealth_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.Health(context.Background())
	require.Error(t, err)
}

func TestHealth_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.Health(context.Background())
	require.Error(t, err)
}

// =============================================================================
// RegisterFleetNode
// =============================================================================

func validRegisterFleetNodeRequest() *RegisterFleetNodeRequest {
	return &RegisterFleetNodeRequest{TenantID: "t1", Name: "n1", Host: "h1", NodeType: "worker"}
}

func TestRegisterAgent_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "RegisterFleetNode", &RegisterFleetNodeResponse{}))
	resp, err := c.RegisterFleetNode(context.Background(), validRegisterFleetNodeRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestRegisterAgent_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.RegisterFleetNode(context.Background(), nil)
	require.Error(t, err)
	_, err = c.RegisterFleetNode(context.Background(), &RegisterFleetNodeRequest{Name: "n", Host: "h", NodeType: "w"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
	_, err = c.RegisterFleetNode(context.Background(), &RegisterFleetNodeRequest{TenantID: "t", Host: "h", NodeType: "w"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
	_, err = c.RegisterFleetNode(context.Background(), &RegisterFleetNodeRequest{TenantID: "t", Name: "n", NodeType: "w"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host is required")
	_, err = c.RegisterFleetNode(context.Background(), &RegisterFleetNodeRequest{TenantID: "t", Name: "n", Host: "h"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent type is required")
}

func TestRegisterAgent_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "RegisterFleetNode", &RegisterFleetNodeResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.RegisterFleetNode(nil, validRegisterFleetNodeRequest())
	require.Error(t, err)
}

func TestRegisterAgent_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.RegisterFleetNode(context.Background(), validRegisterFleetNodeRequest())
	require.Error(t, err)
}

func TestRegisterAgent_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.RegisterFleetNode(context.Background(), validRegisterFleetNodeRequest())
	require.Error(t, err)
}

func TestRegisterAgent_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.RegisterFleetNode(context.Background(), validRegisterFleetNodeRequest())
	require.Error(t, err)
}

// =============================================================================
// GetFleetNode
// =============================================================================

func TestGetAgent_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "GetFleetNode", &FleetNode{}))
	resp, err := c.GetFleetNode(context.Background(), &GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestGetAgent_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetFleetNode(context.Background(), nil)
	require.Error(t, err)
	_, err = c.GetFleetNode(context.Background(), &GetFleetNodeRequest{NodeID: "a1"})
	require.Error(t, err)
	_, err = c.GetFleetNode(context.Background(), &GetFleetNodeRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestGetAgent_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "GetFleetNode", &FleetNode{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.GetFleetNode(nil, &GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestGetAgent_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetFleetNode(context.Background(), &GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestGetAgent_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.GetFleetNode(context.Background(), &GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestGetAgent_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetFleetNode(context.Background(), &GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// ListFleetNodes
// =============================================================================

func TestListAgents_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ListFleetNodes", &ListFleetNodesResponse{Count: 4}))
	resp, err := c.ListFleetNodes(context.Background(), &ListFleetNodesRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 4, resp.Count)
}

func TestListAgents_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListFleetNodes(context.Background(), nil)
	require.Error(t, err)
	_, err = c.ListFleetNodes(context.Background(), &ListFleetNodesRequest{})
	require.Error(t, err)
}

func TestListAgents_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListFleetNodes", &ListFleetNodesResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListFleetNodes(nil, &ListFleetNodesRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListAgents_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListFleetNodes(context.Background(), &ListFleetNodesRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListAgents_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListFleetNodes(context.Background(), &ListFleetNodesRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Nodes)
}

func TestListAgents_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListFleetNodes(context.Background(), &ListFleetNodesRequest{TenantID: "t1"})
	require.Error(t, err)
}

// =============================================================================
// RemoveFleetNode
// =============================================================================

func TestRemoveAgent_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "RemoveFleetNode", &RemoveFleetNodeResponse{}))
	resp, err := c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestRemoveAgent_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.RemoveFleetNode(context.Background(), nil)
	require.Error(t, err)
	_, err = c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{NodeID: "a1"})
	require.Error(t, err)
	_, err = c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestRemoveAgent_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "RemoveFleetNode", &RemoveFleetNodeResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.RemoveFleetNode(nil, &RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestRemoveAgent_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestRemoveAgent_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestRemoveAgent_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.RemoveFleetNode(context.Background(), &RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// FleetNodeHeartbeat
// =============================================================================

func TestAgentHeartbeat_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "FleetNodeHeartbeat", &FleetNodeHeartbeatResponse{}))
	resp, err := c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestAgentHeartbeat_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.FleetNodeHeartbeat(context.Background(), nil)
	require.Error(t, err)
	_, err = c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{NodeID: "a1"})
	require.Error(t, err)
	_, err = c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestAgentHeartbeat_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "FleetNodeHeartbeat", &FleetNodeHeartbeatResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.FleetNodeHeartbeat(nil, &FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestAgentHeartbeat_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestAgentHeartbeat_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestAgentHeartbeat_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.FleetNodeHeartbeat(context.Background(), &FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// DecomposeTask
// =============================================================================

func TestDecomposeTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "DecomposeTask", &DecomposeTaskResponse{}))
	resp, err := c.DecomposeTask(context.Background(), &DecomposeTaskRequest{Task: "build a thing"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestDecomposeTask_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.DecomposeTask(context.Background(), nil)
	require.Error(t, err)
	_, err = c.DecomposeTask(context.Background(), &DecomposeTaskRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task is required")
}

func TestDecomposeTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "DecomposeTask", &DecomposeTaskResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.DecomposeTask(nil, &DecomposeTaskRequest{Task: "x"})
	require.Error(t, err)
}

func TestDecomposeTask_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.DecomposeTask(context.Background(), &DecomposeTaskRequest{Task: "x"})
	require.Error(t, err)
}

func TestDecomposeTask_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.DecomposeTask(context.Background(), &DecomposeTaskRequest{Task: "x"})
	require.Error(t, err)
}

func TestDecomposeTask_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.DecomposeTask(context.Background(), &DecomposeTaskRequest{Task: "x"})
	require.Error(t, err)
}

// =============================================================================
// SpawnChildJob
// =============================================================================

func TestSpawnChildJob_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "SpawnChildJob", &SpawnChildJobResponse{}))
	resp, err := c.SpawnChildJob(context.Background(), &SpawnChildJobRequest{ParentJobID: "j1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestSpawnChildJob_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SpawnChildJob(context.Background(), nil)
	require.Error(t, err)
	_, err = c.SpawnChildJob(context.Background(), &SpawnChildJobRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parent_job_id is required")
}

func TestSpawnChildJob_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "SpawnChildJob", &SpawnChildJobResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.SpawnChildJob(nil, &SpawnChildJobRequest{ParentJobID: "j1"})
	require.Error(t, err)
}

func TestSpawnChildJob_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SpawnChildJob(context.Background(), &SpawnChildJobRequest{ParentJobID: "j1"})
	require.Error(t, err)
}

func TestSpawnChildJob_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.SpawnChildJob(context.Background(), &SpawnChildJobRequest{ParentJobID: "j1"})
	require.Error(t, err)
}

func TestSpawnChildJob_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.SpawnChildJob(context.Background(), &SpawnChildJobRequest{ParentJobID: "j1"})
	require.Error(t, err)
}

// =============================================================================
// StartJob (extend existing coverage: happy/nilctx/transport/nil/mismatch)
// =============================================================================

func TestStartJob_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "StartJob", &StartJobResponse{}))
	resp, err := c.StartJob(context.Background(), &StartJobRequest{Description: "do work"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestStartJob_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "StartJob", &StartJobResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.StartJob(nil, &StartJobRequest{Description: "do work"})
	require.Error(t, err)
}

func TestStartJob_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.StartJob(context.Background(), &StartJobRequest{Description: "do work"})
	require.Error(t, err)
}

func TestStartJob_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.StartJob(context.Background(), &StartJobRequest{Description: "do work"})
	require.Error(t, err)
}

func TestStartJob_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.StartJob(context.Background(), &StartJobRequest{Description: "do work"})
	require.Error(t, err)
}

// =============================================================================
// GetJobStatus
// =============================================================================

func TestGetJobStatus_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "GetJobStatus", &JobStatus{}))
	resp, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{TenantID: "t1", JobID: "j1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestGetJobStatus_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "GetJobStatus", &JobStatus{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.GetJobStatus(nil, &GetJobStatusRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestGetJobStatus_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestGetJobStatus_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestGetJobStatus_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

// =============================================================================
// ListJobs
// =============================================================================

func TestListJobs_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "ListJobs", &ListJobsResponse{}))
	resp, err := c.ListJobs(context.Background(), &ListJobsRequest{TenantID: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListJobs_Validation(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListJobs(context.Background(), nil)
	require.Error(t, err)
	_, err = c.ListJobs(context.Background(), &ListJobsRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestListJobs_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListJobs", &ListJobsResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.ListJobs(nil, &ListJobsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListJobs_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListJobs(context.Background(), &ListJobsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListJobs_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.ListJobs(context.Background(), &ListJobsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListJobs_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListJobs(context.Background(), &ListJobsRequest{TenantID: "t1"})
	require.Error(t, err)
}

// =============================================================================
// CancelJob
// =============================================================================

func TestCancelJob_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "CancelJob", &CancelJobResponse{}))
	resp, err := c.CancelJob(context.Background(), &CancelJobRequest{TenantID: "t1", JobID: "j1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCancelJob_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "CancelJob", &CancelJobResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.CancelJob(nil, &CancelJobRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestCancelJob_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CancelJob(context.Background(), &CancelJobRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestCancelJob_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.CancelJob(context.Background(), &CancelJobRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

func TestCancelJob_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.CancelJob(context.Background(), &CancelJobRequest{TenantID: "t1", JobID: "j1"})
	require.Error(t, err)
}

// =============================================================================
// GetSessionHistory
// =============================================================================

func TestGetSessionHistory_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "GetSessionHistory", &GetSessionHistoryResponse{}))
	resp, err := c.GetSessionHistory(context.Background(), &GetSessionHistoryRequest{SessionID: "s1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestGetSessionHistory_Validation(t *testing.T) {
	c := NewClient(failTransport())
	//nolint:staticcheck // intentionally nil ctx
	_, err := c.GetSessionHistory(nil, &GetSessionHistoryRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")
	_, err = c.GetSessionHistory(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")
	_, err = c.GetSessionHistory(context.Background(), &GetSessionHistoryRequest{})
	require.Error(t, err)
}

func TestGetSessionHistory_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetSessionHistory(context.Background(), &GetSessionHistoryRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestGetSessionHistory_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetSessionHistory(context.Background(), &GetSessionHistoryRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid response type")
}

// =============================================================================
// SubscribeSessionEvents (streaming)
// =============================================================================

func TestSubscribeSessionEvents_HappyPath(t *testing.T) {
	envelopes := []*SessionEventEnvelope{{}, {}}
	idx := 0
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(envelopes) {
				return nil, errors.NewInternal("mock", "stream ended")
			}
			e := envelopes[idx]
			idx++
			return e, nil
		},
	}
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, service, method string, _ interface{}) (transport.Stream, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "SubscribeSessionEvents", method)
			return ms, nil
		},
	}
	c := NewClient(mt)
	ch, err := c.SubscribeSessionEvents(context.Background(), &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.NoError(t, err)
	require.NotNil(t, ch)

	var got []*SessionEventEnvelope
	for e := range ch {
		got = append(got, e)
	}
	require.Len(t, got, 2)
}

func TestSubscribeSessionEvents_Validation(t *testing.T) {
	c := NewClient(failTransport())
	//nolint:staticcheck // intentionally nil ctx
	_, err := c.SubscribeSessionEvents(nil, &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")
	_, err = c.SubscribeSessionEvents(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")
	_, err = c.SubscribeSessionEvents(context.Background(), &SubscribeSessionEventsRequest{})
	require.Error(t, err)
}

func TestSubscribeSessionEvents_StreamError(t *testing.T) {
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return nil, errors.NewInternal("mock", "dial failed")
		},
	}
	c := NewClient(mt)
	_, err := c.SubscribeSessionEvents(context.Background(), &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestSubscribeSessionEvents_WrongMessageTypeSkipped(t *testing.T) {
	msgs := []interface{}{
		&struct{ X int }{X: 1}, // wrong type, must be skipped
		&SessionEventEnvelope{},
	}
	idx := 0
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(msgs) {
				return nil, errors.NewInternal("mock", "end")
			}
			m := msgs[idx]
			idx++
			return m, nil
		},
	}
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}
	c := NewClient(mt)
	ch, err := c.SubscribeSessionEvents(context.Background(), &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.NoError(t, err)

	var got []*SessionEventEnvelope
	for e := range ch {
		got = append(got, e)
	}
	require.Len(t, got, 1)
}

func TestSubscribeSessionEvents_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	blockCh := make(chan struct{})
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			<-blockCh
			return nil, errors.NewInternal("mock", "cancelled")
		},
	}
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}
	c := NewClient(mt)
	ch, err := c.SubscribeSessionEvents(ctx, &SubscribeSessionEventsRequest{SessionID: "s1"})
	require.NoError(t, err)

	cancel()
	close(blockCh)

	timeout := time.After(2 * time.Second)
	select {
	case <-ch:
	case <-timeout:
		t.Fatal("channel did not close after context cancel")
	}
	for range ch {
	}
}

// =============================================================================
// Inference / job event stream: wrong-type-skip branch coverage
// =============================================================================

func TestInferenceStream_WrongMessageTypeSkipped(t *testing.T) {
	msgs := []interface{}{
		&struct{ X int }{X: 1}, // wrong type, must be skipped
		&InferenceChunk{Final: true},
	}
	idx := 0
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(msgs) {
				return nil, errors.NewInternal("mock", "end")
			}
			m := msgs[idx]
			idx++
			return m, nil
		},
	}
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}
	c := NewClient(mt)
	ch, err := c.InferenceStream(context.Background(), &InferenceRequest{Backend: "ollama", Prompt: "hi"})
	require.NoError(t, err)

	var got []*InferenceChunk
	for chunk := range ch {
		got = append(got, chunk)
	}
	require.Len(t, got, 1)
	assert.True(t, got[0].Final)
}

func TestSubscribeJobEvents_WrongMessageTypeSkipped(t *testing.T) {
	msgs := []interface{}{
		&struct{ X int }{X: 1}, // wrong type, must be skipped
		&JobEvent{},
	}
	idx := 0
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(msgs) {
				return nil, errors.NewInternal("mock", "end")
			}
			m := msgs[idx]
			idx++
			return m, nil
		},
	}
	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}
	c := NewClient(mt)
	ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{TenantID: "t1", JobID: "j1"})
	require.NoError(t, err)

	var got []*JobEvent
	for e := range ch {
		got = append(got, e)
	}
	require.Len(t, got, 1)
}

// =============================================================================
// Inference / InferenceStream nil-context guards.
// =============================================================================

func TestInference_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "Inference", &InferenceResponse{}))
	//nolint:staticcheck // intentionally nil
	_, err := c.Inference(nil, &InferenceRequest{Backend: "ollama", Prompt: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")
}

func TestInferenceStream_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck // intentionally nil
	_, err := c.InferenceStream(nil, &InferenceRequest{Backend: "ollama", Prompt: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")
}
