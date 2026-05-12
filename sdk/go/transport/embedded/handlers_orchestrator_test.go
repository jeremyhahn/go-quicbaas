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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// =============================================================================
// Minimal mock helpers
// =============================================================================

// inferChunkChannel builds a closed, pre-loaded channel of InferChunks.
func inferChunkChannel(chunks ...*orchestration.InferChunk) <-chan *orchestration.InferChunk {
	ch := make(chan *orchestration.InferChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// mockAgentSvcForInfer is a minimal orchestration.AgentService that only
// implements Infer; all other methods panic if called.
type mockAgentSvcForInfer struct {
	orchestration.AgentService
	inferFunc func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error)
}

func (m *mockAgentSvcForInfer) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	return m.inferFunc(ctx, req)
}

// mockQuicBaaSForInfer is a minimal service.QuicBaaS whose Agent() returns
// mockAgentSvcForInfer; all other QuicBaaS methods return zero/nil values.
type mockQuicBaaSForInfer struct {
	service.QuicBaaS
	agentSvc orchestration.AgentService
}

func (m *mockQuicBaaSForInfer) Agent() orchestration.AgentService {
	return m.agentSvc
}

// =============================================================================
// handleOrchestratorInference (unary)
// =============================================================================

func TestHandleOrchestratorInference_Success(t *testing.T) {
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			assert.Equal(t, "ollama", req.Backend)
			assert.Equal(t, "llama3", req.Model)
			assert.Equal(t, "hello world", req.Prompt)
			return inferChunkChannel(
				&orchestration.InferChunk{
					Delta: []orchestration.ContentBlock{{Type: "text", Text: "Hi"}},
				},
				&orchestration.InferChunk{
					Delta:      []orchestration.ContentBlock{{Type: "text", Text: " there"}},
					Model:      "llama3",
					StopReason: "end_turn",
					Final:      true,
				},
			), nil
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	req := &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Model:   "llama3",
		Prompt:  "hello world",
	}

	result, err := handleOrchestratorInference(context.Background(), svc, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(*orchestratorSDK.InferenceResponse)
	require.True(t, ok, "expected *orchestratorSDK.InferenceResponse, got %T", result)
	assert.Equal(t, "llama3", resp.Model)
	assert.Equal(t, "end_turn", resp.StopReason)
	require.Len(t, resp.Content, 2)
	assert.Equal(t, "text", resp.Content[0].Type)
	assert.Equal(t, "Hi", resp.Content[0].Text)
	assert.Equal(t, " there", resp.Content[1].Text)
}

func TestHandleOrchestratorInference_WrongRequestType(t *testing.T) {
	svc := &mockQuicBaaSForInfer{agentSvc: &mockAgentSvcForInfer{}}

	_, err := handleOrchestratorInference(context.Background(), svc, "not a request")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Inference")
}

func TestHandleOrchestratorInference_NilAgentService(t *testing.T) {
	svc := &mockQuicBaaSForInfer{agentSvc: nil}

	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "hi"}
	_, err := handleOrchestratorInference(context.Background(), svc, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Inference")
}

func TestHandleOrchestratorInference_InferError(t *testing.T) {
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, errors.New("backend unavailable")
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "hi"}
	_, err := handleOrchestratorInference(context.Background(), svc, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend unavailable")
}

func TestHandleOrchestratorInference_MetadataForwarded(t *testing.T) {
	var captured *orchestration.InferRequest
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			captured = req
			return inferChunkChannel(&orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				Final: true,
			}), nil
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	req := &orchestratorSDK.InferenceRequest{
		Backend:      "ollama",
		Prompt:       "test",
		SystemPrompt: "you are a helper",
		HostAffinity: "node-1",
		Metadata:     map[string]string{"key": "value"},
	}

	_, err := handleOrchestratorInference(context.Background(), svc, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "you are a helper", captured.SystemPrompt)
	assert.Equal(t, "node-1", captured.HostAffinity)
	assert.Equal(t, "value", captured.Metadata["key"])
}

// =============================================================================
// handleOrchestratorInferenceStream (streaming)
// =============================================================================

func TestHandleOrchestratorInferenceStream_Success(t *testing.T) {
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			assert.Equal(t, "ollama", req.Backend)
			return inferChunkChannel(
				&orchestration.InferChunk{Delta: []orchestration.ContentBlock{{Type: "text", Text: "chunk1"}}},
				&orchestration.InferChunk{
					Delta:      []orchestration.ContentBlock{{Type: "text", Text: "chunk2"}},
					Model:      "llama3",
					StopReason: "end_turn",
					Final:      true,
				},
			), nil
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newStream(ctx, 10)
	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "stream test"}

	errCh := make(chan error, 1)
	go func() {
		errCh <- handleOrchestratorInferenceStream(ctx, svc, req, s)
	}()

	// Collect chunks from the stream.
	var chunks []*orchestratorSDK.InferenceChunk
	for i := 0; i < 2; i++ {
		msg, err := s.Recv()
		require.NoError(t, err)
		chunk, ok := msg.(*orchestratorSDK.InferenceChunk)
		require.True(t, ok, "expected *orchestratorSDK.InferenceChunk, got %T", msg)
		chunks = append(chunks, chunk)
	}

	require.NoError(t, <-errCh)
	require.Len(t, chunks, 2)
	assert.Equal(t, "chunk1", chunks[0].Delta[0].Text)
	assert.Equal(t, "chunk2", chunks[1].Delta[0].Text)
	assert.Equal(t, "llama3", chunks[1].Model)
	assert.Equal(t, "end_turn", chunks[1].StopReason)
	assert.True(t, chunks[1].Final)
}

func TestHandleOrchestratorInferenceStream_WrongRequestType(t *testing.T) {
	svc := &mockQuicBaaSForInfer{agentSvc: &mockAgentSvcForInfer{}}

	ctx := context.Background()
	s := newStream(ctx, 1)
	err := handleOrchestratorInferenceStream(ctx, svc, "bad type", s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "InferenceStream")
}

func TestHandleOrchestratorInferenceStream_NilAgentService(t *testing.T) {
	svc := &mockQuicBaaSForInfer{agentSvc: nil}

	ctx := context.Background()
	s := newStream(ctx, 1)
	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "hi"}

	err := handleOrchestratorInferenceStream(ctx, svc, req, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "InferenceStream")
}

func TestHandleOrchestratorInferenceStream_InferError(t *testing.T) {
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, errors.New("infer backend error")
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	ctx := context.Background()
	s := newStream(ctx, 1)
	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "hi"}

	err := handleOrchestratorInferenceStream(ctx, svc, req, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "infer backend error")
}

func TestHandleOrchestratorInferenceStream_ContextCancellation(t *testing.T) {
	// The chunk channel blocks indefinitely; context cancellation should stop the stream.
	blockCh := make(chan *orchestration.InferChunk) // never sends
	agentSvc := &mockAgentSvcForInfer{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return blockCh, nil
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	ctx, cancel := context.WithCancel(context.Background())
	s := newStream(ctx, 1)
	req := &orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "hi"}

	errCh := make(chan error, 1)
	go func() {
		errCh <- handleOrchestratorInferenceStream(ctx, svc, req, s)
	}()

	cancel()

	err := <-errCh
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// =============================================================================
// Project handlers — Bug 2 + Bug 3 regression coverage
// =============================================================================

// mockQuicBaaSForProject returns a real, in-memory ProjectService so the
// handlers exercise the full request/response path including projection of
// every field through the service layer and back through convertProjectToSDK.
type mockQuicBaaSForProject struct {
	service.QuicBaaS
	projectSvc service.ProjectService
}

func (m *mockQuicBaaSForProject) Project() service.ProjectService {
	return m.projectSvc
}

// newProjectTestHarness builds a fresh, isolated harness with an in-memory
// ProjectStore so Create / Get / Update round-trips are deterministic.
func newProjectTestHarness() *mockQuicBaaSForProject {
	return &mockQuicBaaSForProject{
		projectSvc: service.NewProjectService(orchestration.NewInMemoryProjectStore()),
	}
}

// TestHandleProjectCreate_AllFieldsRoundTrip asserts that handleProjectCreate
// forwards every field on the SDK CreateProjectRequest into the service layer
// and that convertProjectToSDK projects every persisted field back.
func TestHandleProjectCreate_AllFieldsRoundTrip(t *testing.T) {
	svc := newProjectTestHarness()
	req := &projectSDK.CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "alpha",
		Description:      "alpha project",
		WorkspaceAgentID: "agent-1",
		WorkspacePath:    "/work/alpha",
		HostAffinity:     "host-east",
		DefaultLeadAgent: "agent-lead",
		GitRepoURL:       "https://example.test/alpha.git",
		GitDefaultBranch: "main",
		TokenBudget:      100_000,
		GPUMinutes:       60,
		MaxConcurrency:   4,
		Metadata:         map[string]string{"team": "platform"},
	}

	result, err := handleProjectCreate(context.Background(), svc, req)
	require.NoError(t, err)

	info, ok := result.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)

	assert.NotEmpty(t, info.ID)
	assert.Equal(t, "tenant-a", info.TenantID)
	assert.Equal(t, "alpha", info.Name)
	assert.Equal(t, "alpha project", info.Description)
	assert.Equal(t, "agent-1", info.Workspace.AgentID)
	assert.Equal(t, "/work/alpha", info.Workspace.Path)
	assert.Equal(t, "host-east", info.HostAffinity)
	assert.Equal(t, "agent-lead", info.DefaultLeadAgent)
	assert.Equal(t, "https://example.test/alpha.git", info.Git.RepoURL)
	assert.Equal(t, "main", info.Git.DefaultBranch)
	assert.Equal(t, int64(100_000), info.Quotas.TokenBudget)
	assert.Equal(t, int64(60), info.Quotas.GPUMinutes)
	assert.Equal(t, 4, info.Quotas.MaxConcurrency)
	assert.Equal(t, "platform", info.Metadata["team"])
	assert.False(t, info.CreatedAt.IsZero(), "CreatedAt must be populated")
	assert.False(t, info.UpdatedAt.IsZero(), "UpdatedAt must be populated")
}

// TestHandleProjectUpdate_AllFieldsRoundTrip asserts that handleProjectUpdate
// forwards every settable field — Workspace.Path, Git.CurrentSHA, AgentRoles,
// quotas, metadata — to the service layer and that the returned project
// reflects each update.
func TestHandleProjectUpdate_AllFieldsRoundTrip(t *testing.T) {
	svc := newProjectTestHarness()

	// Seed a baseline project.
	created, err := handleProjectCreate(context.Background(), svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-b",
		Name:     "beta",
	})
	require.NoError(t, err)
	baseline := created.(*projectSDK.ProjectInfo)

	updateReq := &projectSDK.UpdateProjectRequest{
		TenantID:         "tenant-b",
		ProjectID:        baseline.ID,
		Name:             "beta-renamed",
		Description:      "now described",
		WorkspaceAgentID: "agent-w1",
		WorkspacePath:    "/work/beta",
		HostAffinity:     "host-west",
		DefaultLeadAgent: "agent-lead-2",
		GitRepoURL:       "https://example.test/beta.git",
		GitDefaultBranch: "trunk",
		GitCurrentSHA:    "deadbeefcafebabe",
		AgentRoles: map[string]projectSDK.ProjectRole{
			"agent-w1": projectSDK.ProjectRoleLead,
			"agent-w2": projectSDK.ProjectRoleWorker,
		},
		TokenBudget:    200_000,
		GPUMinutes:     120,
		MaxConcurrency: 8,
		Metadata:       map[string]string{"phase": "pilot"},
	}

	result, err := handleProjectUpdate(context.Background(), svc, updateReq)
	require.NoError(t, err)
	info, ok := result.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)

	assert.Equal(t, "beta-renamed", info.Name)
	assert.Equal(t, "now described", info.Description)
	assert.Equal(t, "agent-w1", info.Workspace.AgentID)
	assert.Equal(t, "/work/beta", info.Workspace.Path)
	assert.Equal(t, "host-west", info.HostAffinity)
	assert.Equal(t, "agent-lead-2", info.DefaultLeadAgent)
	assert.Equal(t, "https://example.test/beta.git", info.Git.RepoURL)
	assert.Equal(t, "trunk", info.Git.DefaultBranch)
	assert.Equal(t, "deadbeefcafebabe", info.Git.CurrentSHA)
	assert.Equal(t, int64(200_000), info.Quotas.TokenBudget)
	assert.Equal(t, int64(120), info.Quotas.GPUMinutes)
	assert.Equal(t, 8, info.Quotas.MaxConcurrency)
	assert.Equal(t, "pilot", info.Metadata["phase"])
	require.NotNil(t, info.Permissions.AgentRoles)
	assert.Equal(t, projectSDK.ProjectRoleLead, info.Permissions.AgentRoles["agent-w1"])
	assert.Equal(t, projectSDK.ProjectRoleWorker, info.Permissions.AgentRoles["agent-w2"])
}

// TestHandleProjectCreate_WrongRequestType ensures the handler returns the
// typed *InvalidRequestTypeError rather than panicking when an unrelated
// request value is passed.
func TestHandleProjectCreate_WrongRequestType(t *testing.T) {
	svc := newProjectTestHarness()
	_, err := handleProjectCreate(context.Background(), svc, "not a request")
	require.Error(t, err)
	var typed *InvalidRequestTypeError
	assert.True(t, errors.As(err, &typed),
		"expected *InvalidRequestTypeError, got %T: %v", err, err)
}

// TestHandleProjectUpdate_WrongRequestType ensures the handler rejects an
// invalid request type rather than panicking.
func TestHandleProjectUpdate_WrongRequestType(t *testing.T) {
	svc := newProjectTestHarness()
	_, err := handleProjectUpdate(context.Background(), svc, 42)
	require.Error(t, err)
	var typed *InvalidRequestTypeError
	assert.True(t, errors.As(err, &typed),
		"expected *InvalidRequestTypeError, got %T: %v", err, err)
}

// =============================================================================
// ListBackendModels handler — Bug 1 regression coverage
// =============================================================================

// mockAgentSvcForListModels is a minimal AgentService implementing only
// ListBackendModels.
type mockAgentSvcForListModels struct {
	orchestration.AgentService
	listFunc func(ctx context.Context, backendName string) ([]string, error)
}

func (m *mockAgentSvcForListModels) ListBackendModels(ctx context.Context, backendName string) ([]string, error) {
	return m.listFunc(ctx, backendName)
}

// TestHandleOrchestratorListBackendModels_Success exercises the happy path:
// the SDK request reaches the orchestration AgentService and the returned
// model list is wrapped in the SDK response shape with Backend and Count set.
func TestHandleOrchestratorListBackendModels_Success(t *testing.T) {
	called := false
	agentSvc := &mockAgentSvcForListModels{
		listFunc: func(ctx context.Context, backendName string) ([]string, error) {
			called = true
			assert.Equal(t, "ollama", backendName)
			return []string{"llama3", "mistral"}, nil
		},
	}
	svc := &mockQuicBaaSForInfer{agentSvc: agentSvc}

	req := &orchestratorSDK.ListBackendModelsRequest{
		AgentID:     "agent-1",
		BackendName: "ollama",
		TenantID:    "tenant-a",
	}
	result, err := handleOrchestratorListBackendModels(context.Background(), svc, req)
	require.NoError(t, err)
	assert.True(t, called, "AgentService.ListBackendModels was not invoked")

	resp, ok := result.(*orchestratorSDK.ListBackendModelsResponse)
	require.True(t, ok, "expected *ListBackendModelsResponse, got %T", result)
	assert.Equal(t, "ollama", resp.Backend)
	assert.Equal(t, []string{"llama3", "mistral"}, resp.Models)
	assert.Equal(t, 2, resp.Count)
}

// TestHandleOrchestratorListBackendModels_WrongRequestType ensures the
// handler returns the typed *InvalidRequestTypeError on misuse.
func TestHandleOrchestratorListBackendModels_WrongRequestType(t *testing.T) {
	svc := &mockQuicBaaSForInfer{agentSvc: &mockAgentSvcForListModels{}}
	_, err := handleOrchestratorListBackendModels(context.Background(), svc, "bogus")
	require.Error(t, err)
	var typed *InvalidRequestTypeError
	assert.True(t, errors.As(err, &typed),
		"expected *InvalidRequestTypeError, got %T: %v", err, err)
}

// =============================================================================
// CreateTask / StartJob field-threading — Bug 4 + Bug 5 regression coverage
// =============================================================================

// mockAgentSvcForFields captures the first request seen by CreateTask /
// StartJob so the test can assert the new SDK fields actually reached the
// orchestration domain.
type mockAgentSvcForFields struct {
	orchestration.AgentService
	createTaskReq *orchestration.CreateTaskRequest
	startJobReq   *orchestration.StartJobRequest
}

func (m *mockAgentSvcForFields) CreateTask(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	m.createTaskReq = req
	return &orchestration.Task{ID: "task-1", TenantID: req.TenantID, Subject: req.Subject}, nil
}

func (m *mockAgentSvcForFields) StartJob(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	m.startJobReq = req
	return &orchestration.StartJobResponse{JobID: "job-1"}, nil
}

// TestHandleOrchestratorCreateTask_ThreadsProjectID asserts the SDK
// CreateTaskRequest.ProjectID field is forwarded into the orchestration
// CreateTaskRequest sent to the AgentService.
func TestHandleOrchestratorCreateTask_ThreadsProjectID(t *testing.T) {
	agent := &mockAgentSvcForFields{}
	svc := &mockQuicBaaSForInfer{agentSvc: agent}

	_, err := handleOrchestratorCreateTask(context.Background(), svc, &orchestratorSDK.CreateTaskRequest{
		TenantID:  "tenant-a",
		Subject:   "fix bug",
		ProjectID: "proj-xyz",
	})
	require.NoError(t, err)
	require.NotNil(t, agent.createTaskReq, "CreateTask was not invoked")
	assert.Equal(t, "proj-xyz", agent.createTaskReq.ProjectID,
		"ProjectID was dropped between SDK CreateTaskRequest and orchestration CreateTaskRequest")
}

// TestHandleOrchestratorStartJob_ThreadsLeadAgentID asserts the SDK
// StartJobRequest.LeadAgentID field is forwarded into the orchestration
// StartJobRequest sent to the AgentService.
func TestHandleOrchestratorStartJob_ThreadsLeadAgentID(t *testing.T) {
	agent := &mockAgentSvcForFields{}
	svc := &mockQuicBaaSForInfer{agentSvc: agent}

	_, err := handleOrchestratorStartJob(context.Background(), svc, &orchestratorSDK.StartJobRequest{
		TenantID:    "tenant-a",
		Description: "do the thing",
		LeadAgentID: "agent-lead-1",
	})
	require.NoError(t, err)
	require.NotNil(t, agent.startJobReq, "StartJob was not invoked")
	assert.Equal(t, "agent-lead-1", agent.startJobReq.LeadAgentID,
		"LeadAgentID was dropped between SDK StartJobRequest and orchestration StartJobRequest")
}
