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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

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

// projectRefKeys is the KMS SSH key store the project service verifies git
// key references against: it knows exactly the scope/owner/name addresses it
// was built with, and answers any other with *xkmsssh.NotFoundError.
type projectRefKeys map[string]bool

func (k projectRefKeys) GetKey(_ context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error) {
	if !k[req.Scope+"/"+req.Owner+"/"+req.Name] {
		return nil, &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	return &xkmsssh.Key{Name: req.Name}, nil
}

// newProjectTestHarness builds a fresh, isolated harness with an in-memory
// ProjectStore so Create / Get / Update round-trips are deterministic. The
// project service verifies references on save, so the harness also attaches
// the fleet nodes and SSH keys the round-trip tests name, and a tenant agent
// catalog that defines every Agent.
func newProjectTestHarness(t *testing.T) *mockQuicBaaSForProject {
	t.Helper()
	projectSvc := service.NewProjectService(orchestration.NewInMemoryProjectStore())

	fleet := orchestration.NewInMemoryFleetRegistry()
	for host, tenantID := range map[string]string{"host-east": "tenant-a", "host-west": "tenant-b"} {
		require.NoError(t, fleet.RegisterFleetNode(context.Background(), &orchestration.FleetNode{
			ID: "node-" + host, TenantID: tenantID, Name: host, Host: host, NodeType: "worker",
		}))
	}
	keys := projectRefKeys{
		"tenant/tenant-a/api-signing": true,
		"service/deploy/api-access":   true,
		"service/deploy/rotated":      true,
	}

	wirer, ok := projectSvc.(interface {
		SetFleetNodes(orchestration.FleetNodeLister)
		SetSSHKeys(service.SSHKeyGetter)
		SetAgentRefChecker(service.AgentRefChecker)
	})
	require.True(t, ok, "the project service must accept its reference stores")
	wirer.SetFleetNodes(fleet)
	wirer.SetSSHKeys(keys)
	wirer.SetAgentRefChecker(acceptingAgentRefs{})

	return &mockQuicBaaSForProject{projectSvc: projectSvc}
}

// acceptingAgentRefs is a tenant agent catalog that defines every Agent: the
// transport tests are about carrying a reference, not about resolving it.
type acceptingAgentRefs struct{}

func (acceptingAgentRefs) CheckRef(context.Context, string, orchestration.AgentRef, string, orchestration.AgentRole) error {
	return nil
}

// TestHandleProjectCreate_AllFieldsRoundTrip asserts that handleProjectCreate
// forwards every field on the SDK CreateProjectRequest into the service layer
// and that convertProjectToSDK projects every persisted field back.
func TestHandleProjectCreate_AllFieldsRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	req := &projectSDK.CreateProjectRequest{
		TenantID:             "tenant-a",
		Name:                 "alpha",
		Description:          "alpha project",
		WorkspaceFleetNodeID: "agent-1",
		WorkspacePath:        "/work/alpha",
		HostAffinity:         "host-east",
		DefaultLeadAgent:     agents.AgentRef{AgentID: "agent-lead"},
		GitRepoURL:           "https://example.test/alpha.git",
		GitDefaultBranch:     "main",
		Spend:                spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 100_000}}},
		GPUMinutes:           60,
		MaxConcurrency:       4,
		Metadata:             map[string]string{"team": "platform"},
	}

	result, err := handleProjectCreate(context.Background(), svc, req)
	require.NoError(t, err)

	info, ok := result.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)

	assert.NotEmpty(t, info.ID)
	assert.Equal(t, "tenant-a", info.TenantID)
	assert.Equal(t, "alpha", info.Name)
	assert.Equal(t, "alpha project", info.Description)
	assert.Equal(t, "agent-1", info.Workspace.FleetNodeID)
	assert.Equal(t, "/work/alpha", info.Workspace.Path)
	assert.Equal(t, "host-east", info.HostAffinity)
	assert.Equal(t, agents.AgentRef{AgentID: "agent-lead"}, info.DefaultLeadAgent)
	assert.Equal(t, "https://example.test/alpha.git", info.Git.RepoURL)
	assert.Equal(t, "main", info.Git.DefaultBranch)
	require.Len(t, info.Spend.Windows, 1)
	assert.Equal(t, int64(100_000), info.Spend.Windows[0].MaxTokens)
	assert.Equal(t, int64(60), info.Quotas.GPUMinutes)
	assert.Equal(t, 4, info.Quotas.MaxConcurrency)
	assert.Equal(t, "platform", info.Metadata["team"])
	assert.False(t, info.CreatedAt.IsZero(), "CreatedAt must be populated")
	assert.False(t, info.UpdatedAt.IsZero(), "UpdatedAt must be populated")
}

// TestHandleProjectCreate_ManagedWorkspace asserts the embedded transport
// forwards WorkspaceManaged and projects the provisioning record back. Before
// it did, an in-process caller asking for a managed workspace got a
// bring-your-own-path project with no workspace at all.
func TestHandleProjectCreate_ManagedWorkspace(t *testing.T) {
	svc := newProjectTestHarness(t)
	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	require.NoError(t, err)
	provisioner, err := service.NewProjectWorkspaceProvisioner(service.ProjectWorkspaceProvisionerConfig{
		Workspaces: workspaces,
		Store:      orchestration.NewInMemoryProjectStore(),
	})
	require.NoError(t, err)
	wirer, ok := svc.projectSvc.(interface {
		SetWorkspaceProvisioner(*service.ProjectWorkspaceProvisioner)
	})
	require.True(t, ok, "the project service must accept a workspace provisioner")
	wirer.SetWorkspaceProvisioner(provisioner)

	result, err := handleProjectCreate(context.Background(), svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "managed", WorkspaceManaged: true,
	})
	require.NoError(t, err)
	info, ok := result.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)

	assert.True(t, info.Provisioning.Managed)
	assert.Equal(t, projectSDK.ProvisioningStateReady, info.Provisioning.State)
	assert.False(t, info.Provisioning.UpdatedAt.IsZero(), "Provisioning.UpdatedAt must be populated")
	assert.NotEmpty(t, info.Workspace.Path, "a managed project gets a server-chosen workspace")
}

// TestHandleProjectUpdate_AllFieldsRoundTrip asserts that handleProjectUpdate
// forwards every settable field — Workspace.Path, Git.CurrentSHA, FleetNodeRoles,
// quotas, metadata — to the service layer and that the returned project
// reflects each update.
func TestHandleProjectUpdate_AllFieldsRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)

	// Seed a baseline project.
	created, err := handleProjectCreate(context.Background(), svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-b",
		Name:     "beta",
	})
	require.NoError(t, err)
	baseline := created.(*projectSDK.ProjectInfo)

	updateReq := &projectSDK.UpdateProjectRequest{
		TenantID:             "tenant-b",
		ProjectID:            baseline.ID,
		Name:                 "beta-renamed",
		Description:          "now described",
		WorkspaceFleetNodeID: "agent-w1",
		WorkspacePath:        "/work/beta",
		HostAffinity:         "host-west",
		DefaultLeadAgent:     agents.AgentRef{AgentID: "agent-lead-2"},
		GitRepoURL:           "https://example.test/beta.git",
		GitDefaultBranch:     "trunk",
		GitCurrentSHA:        "deadbeefcafebabe",
		FleetNodeRoles: map[string]projectSDK.ProjectRole{
			"agent-w1": projectSDK.ProjectRoleLead,
			"agent-w2": projectSDK.ProjectRoleWorker,
		},
		// The quotas are presence-carrying pointers: zero means unlimited,
		// so absent and zero must stay distinct across the transport.
		Spend:          &spend.Limits{Windows: []spend.Window{{Name: "weekly", Size: 168 * time.Hour, MaxEquivalentMicros: 200_000}}},
		GPUMinutes:     ptrTo(int64(120)),
		MaxConcurrency: ptrTo(8),
		Metadata:       map[string]string{"phase": "pilot"},
	}

	result, err := handleProjectUpdate(context.Background(), svc, updateReq)
	require.NoError(t, err)
	info, ok := result.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)

	assert.Equal(t, "beta-renamed", info.Name)
	assert.Equal(t, "now described", info.Description)
	assert.Equal(t, "agent-w1", info.Workspace.FleetNodeID)
	assert.Equal(t, "/work/beta", info.Workspace.Path)
	assert.Equal(t, "host-west", info.HostAffinity)
	assert.Equal(t, agents.AgentRef{AgentID: "agent-lead-2"}, info.DefaultLeadAgent)
	assert.Equal(t, "https://example.test/beta.git", info.Git.RepoURL)
	assert.Equal(t, "trunk", info.Git.DefaultBranch)
	assert.Equal(t, "deadbeefcafebabe", info.Git.CurrentSHA)
	require.Len(t, info.Spend.Windows, 1)
	assert.Equal(t, int64(200_000), info.Spend.Windows[0].MaxEquivalentMicros)
	assert.Equal(t, int64(120), info.Quotas.GPUMinutes)
	assert.Equal(t, 8, info.Quotas.MaxConcurrency)
	assert.Equal(t, "pilot", info.Metadata["phase"])
	require.NotNil(t, info.Permissions.FleetNodeRoles)
	assert.Equal(t, projectSDK.ProjectRoleLead, info.Permissions.FleetNodeRoles["agent-w1"])
	assert.Equal(t, projectSDK.ProjectRoleWorker, info.Permissions.FleetNodeRoles["agent-w2"])
}

// TestHandleProjectCreate_WrongRequestType ensures the handler returns the
// typed *InvalidRequestTypeError rather than panicking when an unrelated
// request value is passed.
func TestHandleProjectCreate_WrongRequestType(t *testing.T) {
	svc := newProjectTestHarness(t)
	_, err := handleProjectCreate(context.Background(), svc, "not a request")
	require.Error(t, err)
	var typed *InvalidRequestTypeError
	assert.True(t, errors.As(err, &typed),
		"expected *InvalidRequestTypeError, got %T: %v", err, err)
}

// TestHandleProjectUpdate_WrongRequestType ensures the handler rejects an
// invalid request type rather than panicking.
func TestHandleProjectUpdate_WrongRequestType(t *testing.T) {
	svc := newProjectTestHarness(t)
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
		NodeID:      "agent-1",
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

// TestHandleProject_DefaultWorkflowAndGitIdentityRoundTrip drives the
// embedded transport against the real project service: every new field on
// the SDK request is persisted and projected back on the nested SDK shape,
// and the update's signing switch is tri-state.
func TestHandleProject_DefaultWorkflowAndGitIdentityRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	created, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "api",
		GitRepoURL:          "ssh://git@forge.example.test/acme/api.git",
		GitAuthorName:       "agent-api",
		GitAuthorEmail:      "agent-api@example.test",
		GitSigningEnabled:   true,
		GitSigningAlgorithm: "ECDSA-P256",
		GitSigningKeyName:   "api-signing",
		GitSigningKeyScope:  "tenant",
		GitSigningKeyOwner:  "tenant-a",
		GitSSHKeyName:       "api-access",
		GitSSHKeyScope:      "service",
		GitSSHKeyOwner:      "deploy",
	})
	require.NoError(t, err)
	info, ok := created.(*projectSDK.ProjectInfo)
	require.True(t, ok)
	wantGit := projectSDK.ProjectGit{
		RepoURL: "ssh://git@forge.example.test/acme/api.git",
		Author:  projectSDK.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
		Signing: projectSDK.CommitSigning{
			Enabled: true, Algorithm: "ECDSA-P256",
			Key: projectSDK.KMSKeyRef{Name: "api-signing", Scope: "tenant", Owner: "tenant-a"},
		},
		SSHKey: projectSDK.KMSKeyRef{Name: "api-access", Scope: "service", Owner: "deploy"},
	}
	assert.Equal(t, wantGit, info.Git)
	assert.Empty(t, info.DefaultWorkflow)

	// An unknown default workflow is refused by the service through the
	// embedded path exactly as through every other transport.
	_, err = handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "other", DefaultWorkflow: "ghost"})
	require.Error(t, err)

	off := false
	updated, err := handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: info.ID,
		GitSigningEnabled: &off,
		GitAuthorEmail:    "agent-api@example.internal",
		GitSSHKeyName:     "rotated",
	})
	require.NoError(t, err)
	uinfo, ok := updated.(*projectSDK.ProjectInfo)
	require.True(t, ok)
	assert.False(t, uinfo.Git.Signing.Enabled)
	assert.Equal(t, "api-signing", uinfo.Git.Signing.Key.Name, "turning signing off keeps the key reference")
	assert.Equal(t, "agent-api@example.internal", uinfo.Git.Author.Email)
	assert.Equal(t, "rotated", uinfo.Git.SSHKey.Name)
	assert.Equal(t, "service", uinfo.Git.SSHKey.Scope, "an unset scope leaves the stored scope")

	untouched, err := handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{TenantID: "tenant-a", ProjectID: info.ID, Description: "x"})
	require.NoError(t, err)
	assert.False(t, untouched.(*projectSDK.ProjectInfo).Git.Signing.Enabled, "an update silent on signing leaves it as it was")
}

// =============================================================================
// Autonomy + context repositories — in-process create → get and update → get
// =============================================================================

// TestHandleProject_AutonomyAndContextReposRoundTrip drives the embedded
// handlers against a real in-memory project service, so the assertions cover
// the SDK → service conversion, the service's own semantics and the service →
// SDK conversion at once.
func TestHandleProject_AutonomyAndContextReposRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	created, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID:   "tenant-c",
		Name:       "gamma",
		Autonomous: true,
		ContextRepos: []projectSDK.ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
		},
	})
	require.NoError(t, err)
	info, ok := created.(*projectSDK.ProjectInfo)
	require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", created)
	assert.True(t, info.Autonomous)
	require.Len(t, info.ContextRepos, 1)
	assert.Equal(t, projectSDK.ContextRepo{
		Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main",
	}, info.ContextRepos[0])

	get := func() *projectSDK.ProjectInfo {
		t.Helper()
		result, getErr := handleProjectGet(ctx, svc, &projectSDK.GetProjectRequest{
			TenantID: "tenant-c", ProjectID: info.ID,
		})
		require.NoError(t, getErr)
		got, isInfo := result.(*projectSDK.ProjectInfo)
		require.True(t, isInfo, "expected *projectSDK.ProjectInfo, got %T", result)
		return got
	}

	got := get()
	assert.True(t, got.Autonomous)
	require.Len(t, got.ContextRepos, 1)

	// An unset switch leaves both fields alone.
	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-c", ProjectID: info.ID, Name: "gamma-renamed",
	})
	require.NoError(t, err)
	got = get()
	assert.True(t, got.Autonomous, "an unrelated update turned autonomy off")
	assert.Len(t, got.ContextRepos, 1, "an unrelated update changed the context repos")

	// An explicit false turns it off; a list replaces the whole list.
	off := false
	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-c", ProjectID: info.ID,
		Autonomous: &off,
		ContextRepos: []projectSDK.ContextRepo{
			{Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: "v2"},
		},
	})
	require.NoError(t, err)
	got = get()
	assert.False(t, got.Autonomous)
	require.Len(t, got.ContextRepos, 1)
	assert.Equal(t, "specs", got.ContextRepos[0].Name)

	// And a clear empties it.
	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-c", ProjectID: info.ID, ClearContextRepos: true,
	})
	require.NoError(t, err)
	assert.Empty(t, get().ContextRepos)
}

// TestHandleProjectCreate_RefusesInvalidContextRepos: the service's typed
// refusal reaches the embedded caller instead of a half-written project.
func TestHandleProjectCreate_RefusesInvalidContextRepos(t *testing.T) {
	svc := newProjectTestHarness(t)
	_, err := handleProjectCreate(context.Background(), svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-d", Name: "delta",
		ContextRepos: []projectSDK.ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.example/a", Ref: "main"},
			{Name: "handbook", RepoURL: "https://forge.example/b", Ref: "main"},
		},
	})
	require.Error(t, err)
	var invalid *service.ProjectInvalidError
	require.True(t, errors.As(err, &invalid), "expected *service.ProjectInvalidError, got %T: %v", err, err)
	assert.Equal(t, "context_repos", invalid.Field)
	assert.True(t, errors.Is(err, orchestration.ErrContextRepoInvalid),
		"the refusal must carry the domain sentinel: %v", err)
}

// TestContextRepoConverters_NilFidelity: nil survives both directions, so an
// update can tell "unchanged" from "empty".
func TestContextRepoConverters_NilFidelity(t *testing.T) {
	assert.Nil(t, contextReposToOrchestration(nil))
	assert.Nil(t, contextReposToSDK(nil))
	assert.Empty(t, contextReposToOrchestration([]projectSDK.ContextRepo{}))
	assert.Empty(t, contextReposToSDK([]orchestration.ContextRepo{}))
}

// ptrTo is the presence-carrying form of a quota value.
func ptrTo[T any](v T) *T { return &v }

// TestArtifactToSDK_CarriesEveryField is the parity guard for the embedded
// transport's artifact conversion.
//
// THE EMBEDDED TRANSPORT USED TO ANSWER WITH A SMALLER ARTIFACT THAN REST
// SERVED for the same task: three separate copies of the conversion each
// flattened Ref to its Path and dropped Kind and Metadata. No test caught it
// because each transport was checked against its own expectation rather than
// against the other.
func TestArtifactToSDK_CarriesEveryField(t *testing.T) {
	in := orchestration.Artifact{
		Kind:     orchestration.ArtifactKindURL,
		Name:     "outcome_comment",
		MIMEType: "text/markdown",
		Size:     42,
		Content:  []byte("body"),
		Ref: orchestration.ArtifactRef{
			AgentID: "agent-1", Path: "http://forge.invalid/issues/128",
			SHA: "deadbeef", Branch: "issue/128-x",
		},
		Metadata: map[string]string{"outcome": "readmitted"},
	}

	withContent := artifactToSDK(in, true)
	if withContent.Kind != "url" || withContent.MIMEType != "text/markdown" || withContent.Size != 42 {
		t.Fatalf("scalar fields lost: %+v", withContent)
	}
	if withContent.Metadata["outcome"] != "readmitted" {
		t.Fatalf("metadata lost: %+v", withContent.Metadata)
	}
	if string(withContent.Content) != "body" {
		t.Fatalf("inline content requested but not carried: %q", withContent.Content)
	}
	ref := withContent.Ref
	if ref == nil || ref.AgentID != "agent-1" || ref.Path != "http://forge.invalid/issues/128" ||
		ref.SHA != "deadbeef" || ref.Branch != "issue/128-x" {
		t.Fatalf("ref lost fields: %+v", ref)
	}

	// A listing is metadata plus a reference; the payload stays behind.
	listed := artifactToSDK(in, false)
	if len(listed.Content) != 0 {
		t.Fatalf("a listing must not inline content, got %q", listed.Content)
	}
	if listed.Ref == nil || listed.Ref.Path != in.Ref.Path {
		t.Fatalf("a listing still carries its ref: %+v", listed.Ref)
	}
}

// TestArtifactToSDK_ZeroRefStaysNil keeps an inline artifact from acquiring an
// empty ref object, which would make Kind and Ref disagree on the wire.
func TestArtifactToSDK_ZeroRefStaysNil(t *testing.T) {
	out := artifactToSDK(orchestration.Artifact{
		Kind: orchestration.ArtifactKindInline, Name: "log.txt", Content: []byte("hi"),
	}, true)
	if out.Ref != nil {
		t.Fatalf("an inline artifact must carry no ref, got %+v", out.Ref)
	}
	if artifactRefToDomain(nil) != (orchestration.ArtifactRef{}) {
		t.Fatal("a nil ref must convert to the zero ref rather than panicking")
	}
}

// TestHandleProject_ParkAndResumeRoundTrip drives park and resume through
// the embedded handlers against a real in-memory project service: the park
// is recorded, an unrelated update leaves it alone, and resume clears it
// without touching autonomy.
func TestHandleProject_ParkAndResumeRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	created, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-p", Name: "parkable", Autonomous: true,
	})
	require.NoError(t, err)
	info := created.(*projectSDK.ProjectInfo)

	update := func(req *projectSDK.UpdateProjectRequest) *projectSDK.ProjectInfo {
		t.Helper()
		req.TenantID, req.ProjectID = "tenant-p", info.ID
		result, updateErr := handleProjectUpdate(ctx, svc, req)
		require.NoError(t, updateErr)
		got, ok := result.(*projectSDK.ProjectInfo)
		require.True(t, ok, "expected *projectSDK.ProjectInfo, got %T", result)
		return got
	}

	park := true
	got := update(&projectSDK.UpdateProjectRequest{Parked: &park})
	assert.True(t, got.Parked)
	assert.False(t, got.ParkedAt.IsZero(), "a park records when")
	assert.True(t, got.Autonomous, "parking turned autonomy off")

	got = update(&projectSDK.UpdateProjectRequest{Name: "parkable-renamed"})
	assert.True(t, got.Parked, "an unrelated update unparked the project")

	resume := false
	got = update(&projectSDK.UpdateProjectRequest{Parked: &resume})
	assert.False(t, got.Parked)
	assert.True(t, got.ParkedAt.IsZero())
	assert.True(t, got.Autonomous, "resume turned autonomy off")
}

// TestHandleProjectUpdate_ARetirementReachesTheService: the embedded handler
// parses the retirement fields and hands them to the service, which on a
// server with no key resolver refuses them as an invalid request; an
// unreadable time never reaches the service at all.
func TestHandleProjectUpdate_ARetirementReachesTheService(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()
	created, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{TenantID: "tenant-r", Name: "retiring"})
	require.NoError(t, err)
	id := created.(*projectSDK.ProjectInfo).ID

	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-r", ProjectID: id, GitSigningRetireKeyName: "old",
	})
	var retire *service.SigningKeyRetirementError
	require.ErrorAs(t, err, &retire, "the service was asked to resolve the key")

	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-r", ProjectID: id, GitSigningRetireKeyName: "old", GitSigningRetiredAt: "yesterday",
	})
	var invalid *service.ProjectInvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "git_signing_retired_at", invalid.Field)
}

func TestRetiredSigningKeysToSDK(t *testing.T) {
	assert.Nil(t, retiredSigningKeysToSDK(nil))
	at := time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)
	got := retiredSigningKeysToSDK([]orchestration.RetiredSigningKey{{PublicKey: "ssh-ed25519 AAAA", Name: "old", RetiredAt: at}})
	require.Len(t, got, 1)
	assert.Equal(t, projectSDK.RetiredSigningKey{PublicKey: "ssh-ed25519 AAAA", Name: "old", RetiredAt: at}, got[0])
}

// TestEmbedded_PromptAppendReachesTheDomain: the in-process transport carries
// run-time prompt material on a spawn, a job and a task, and back on the task.
func TestEmbedded_PromptAppendReachesTheDomain(t *testing.T) {
	agent := &mockAgentSvcForFields{}
	svc := &mockQuicBaaSForInfer{agentSvc: agent}
	ctx := context.Background()

	task, err := handleOrchestratorCreateTask(ctx, svc, &orchestratorSDK.CreateTaskRequest{
		TenantID: "tenant-a", Subject: "fix bug", PromptAppend: "task material",
	})
	require.NoError(t, err)
	require.NotNil(t, agent.createTaskReq)
	assert.Equal(t, "task material", agent.createTaskReq.PromptAppend)
	require.NotNil(t, task)

	_, err = handleOrchestratorStartJob(ctx, svc, &orchestratorSDK.StartJobRequest{
		TenantID: "tenant-a", Description: "do the thing", PromptAppend: "job material",
	})
	require.NoError(t, err)
	require.NotNil(t, agent.startJobReq)
	assert.Equal(t, "job material", agent.startJobReq.PromptAppend)

	spawner := &capturingSpawnAgentService{fakeAgentService: &fakeAgentService{}}
	tr, err := New(&orchMock{mockQuicBaaS: &mockQuicBaaS{}, agent: spawner, proj: &fakeProjectService{}, admin: &fakeFleetAdminService{}})
	require.NoError(t, err)
	_, err = tr.Request(ctx, "quicbaas.Orchestrator", "SpawnSession",
		&orchestratorSDK.SpawnSessionRequest{AgentID: "claude-coder", PromptAppend: "spawn material"})
	require.NoError(t, err)
	require.NotNil(t, spawner.spawned)
	assert.Equal(t, "spawn material", spawner.spawned.PromptAppend)
}

// TestEmbedded_SpawnKeepReachesTheDomain asserts the keep marker crosses the
// in-process transport, and that a spawn which does not set it arrives unkept.
func TestEmbedded_SpawnKeepReachesTheDomain(t *testing.T) {
	for _, keep := range []bool{true, false} {
		spawner := &capturingSpawnAgentService{fakeAgentService: &fakeAgentService{}}
		tr, err := New(&orchMock{mockQuicBaaS: &mockQuicBaaS{}, agent: spawner, proj: &fakeProjectService{}, admin: &fakeFleetAdminService{}})
		require.NoError(t, err)
		_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
			&orchestratorSDK.SpawnSessionRequest{AgentID: "claude-coder", Keep: keep})
		require.NoError(t, err)
		require.NotNil(t, spawner.spawned)
		assert.Equal(t, keep, spawner.spawned.Keep)
	}
}
