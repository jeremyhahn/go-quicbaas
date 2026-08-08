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
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// Mock Orchestrator Client
// =============================================================================

// mockOrchestratorClient provides a configurable mock for OrchestratorClient.
type mockOrchestratorClient struct {
	healthFn             func(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error)
	listBackendsFn       func(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error)
	listBackendModelsFn  func(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error)
	spawnSessionFn       func(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error)
	promptSessionFn      func(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error)
	sessionStatusFn      func(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error)
	closeSessionFn       func(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error)
	listSessionsFn       func(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error)
	createTaskFn         func(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error)
	getTaskFn            func(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error)
	listTasksFn          func(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error)
	claimTaskFn          func(ctx context.Context, req *orchestratorSDK.ClaimTaskRequest) (*orchestratorSDK.ClaimTaskResponse, error)
	completeTaskFn       func(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error)
	registerAgentFn      func(ctx context.Context, req *orchestratorSDK.RegisterAgentRequest) (*orchestratorSDK.RegisterAgentResponse, error)
	getAgentFn           func(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error)
	listAgentsFn         func(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error)
	removeAgentFn        func(ctx context.Context, req *orchestratorSDK.RemoveAgentRequest) (*orchestratorSDK.RemoveAgentResponse, error)
	agentHeartFn         func(ctx context.Context, req *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error)
	decomposeTaskFn      func(ctx context.Context, req *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error)
	startJobFn           func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error)
	getJobStatusFn       func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error)
	cancelJobFn          func(ctx context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error)
	subscribeJobEventsFn func(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error)
	inferenceFn          func(ctx context.Context, req *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error)
	inferenceStreamFn    func(ctx context.Context, req *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error)
	closeFn              func() error
}

func (m *mockOrchestratorClient) Health(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return &orchestratorSDK.HealthCheckReport{Healthy: true, Status: "ok"}, nil
}

func (m *mockOrchestratorClient) ListBackends(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error) {
	if m.listBackendsFn != nil {
		return m.listBackendsFn(ctx)
	}
	return &orchestratorSDK.ListBackendsResponse{Backends: []*orchestratorSDK.BackendInfo{}, Count: 0}, nil
}

func (m *mockOrchestratorClient) ListBackendModels(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
	if m.listBackendModelsFn != nil {
		return m.listBackendModelsFn(ctx, req)
	}
	backend := ""
	if req != nil {
		backend = req.BackendName
	}
	return &orchestratorSDK.ListBackendModelsResponse{Backend: backend, Models: []string{}, Count: 0}, nil
}

func (m *mockOrchestratorClient) SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	if m.spawnSessionFn != nil {
		return m.spawnSessionFn(ctx, req)
	}
	return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess_mock", Backend: req.Backend}, nil
}

func (m *mockOrchestratorClient) PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
	if m.promptSessionFn != nil {
		return m.promptSessionFn(ctx, req)
	}
	return &orchestratorSDK.PromptSessionResponse{SessionID: req.SessionID, Response: "mock response", Done: true}, nil
}

func (m *mockOrchestratorClient) SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
	if m.sessionStatusFn != nil {
		return m.sessionStatusFn(ctx, req)
	}
	return &orchestratorSDK.SessionStatusResponse{SessionID: req.SessionID, Backend: "claude", Status: "active"}, nil
}

func (m *mockOrchestratorClient) CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
	if m.closeSessionFn != nil {
		return m.closeSessionFn(ctx, req)
	}
	return &orchestratorSDK.CloseSessionResponse{SessionID: req.SessionID, Closed: true}, nil
}

func (m *mockOrchestratorClient) ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
	if m.listSessionsFn != nil {
		return m.listSessionsFn(ctx, req)
	}
	return &orchestratorSDK.ListSessionsResponse{Sessions: []*orchestratorSDK.SessionStatusResponse{}, Count: 0}, nil
}

func (m *mockOrchestratorClient) CreateTask(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error) {
	if m.createTaskFn != nil {
		return m.createTaskFn(ctx, req)
	}
	return &orchestratorSDK.Task{ID: "task_mock", Subject: req.Subject, Status: "pending"}, nil
}

func (m *mockOrchestratorClient) GetTask(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
	if m.getTaskFn != nil {
		return m.getTaskFn(ctx, req)
	}
	return &orchestratorSDK.Task{ID: req.TaskID, Subject: "Build the project", Status: "pending"}, nil
}

func (m *mockOrchestratorClient) ListTasks(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
	if m.listTasksFn != nil {
		return m.listTasksFn(ctx, req)
	}
	return &orchestratorSDK.ListTasksResponse{Tasks: []*orchestratorSDK.Task{}, Count: 0}, nil
}

func (m *mockOrchestratorClient) ClaimTask(ctx context.Context, req *orchestratorSDK.ClaimTaskRequest) (*orchestratorSDK.ClaimTaskResponse, error) {
	if m.claimTaskFn != nil {
		return m.claimTaskFn(ctx, req)
	}
	return &orchestratorSDK.ClaimTaskResponse{TaskID: req.TaskID, AgentID: req.AgentID, Claimed: true}, nil
}

func (m *mockOrchestratorClient) CompleteTask(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error) {
	if m.completeTaskFn != nil {
		return m.completeTaskFn(ctx, req)
	}
	return &orchestratorSDK.CompleteTaskResponse{TaskID: req.TaskID, Completed: true}, nil
}

func (m *mockOrchestratorClient) RegisterAgent(ctx context.Context, req *orchestratorSDK.RegisterAgentRequest) (*orchestratorSDK.RegisterAgentResponse, error) {
	if m.registerAgentFn != nil {
		return m.registerAgentFn(ctx, req)
	}
	return &orchestratorSDK.RegisterAgentResponse{
		Agent: &orchestratorSDK.AgentRegistration{
			ID: "agent_mock", Name: req.Name, Host: req.Host, AgentType: req.AgentType, Status: "registered",
		},
	}, nil
}

func (m *mockOrchestratorClient) GetAgent(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
	if m.getAgentFn != nil {
		return m.getAgentFn(ctx, req)
	}
	return &orchestratorSDK.AgentRegistration{ID: req.AgentID, Name: "test-agent", Status: "running"}, nil
}

func (m *mockOrchestratorClient) ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
	if m.listAgentsFn != nil {
		return m.listAgentsFn(ctx, req)
	}
	return &orchestratorSDK.ListAgentsResponse{Agents: []*orchestratorSDK.AgentRegistration{}, Count: 0}, nil
}

func (m *mockOrchestratorClient) RemoveAgent(ctx context.Context, req *orchestratorSDK.RemoveAgentRequest) (*orchestratorSDK.RemoveAgentResponse, error) {
	if m.removeAgentFn != nil {
		return m.removeAgentFn(ctx, req)
	}
	return &orchestratorSDK.RemoveAgentResponse{AgentID: req.AgentID, Removed: true}, nil
}

func (m *mockOrchestratorClient) AgentHeartbeat(ctx context.Context, req *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
	if m.agentHeartFn != nil {
		return m.agentHeartFn(ctx, req)
	}
	return &orchestratorSDK.AgentHeartbeatResponse{AgentID: req.AgentID, Heartbeat: true}, nil
}

func (m *mockOrchestratorClient) DecomposeTask(ctx context.Context, req *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error) {
	if m.decomposeTaskFn != nil {
		return m.decomposeTaskFn(ctx, req)
	}
	return &orchestratorSDK.DecomposeTaskResponse{WorkItems: []*orchestratorSDK.WorkItem{}, Timestamp: 0}, nil
}

func (m *mockOrchestratorClient) StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
	if m.startJobFn != nil {
		return m.startJobFn(ctx, req)
	}
	return &orchestratorSDK.StartJobResponse{JobID: "job_mock", Timestamp: 0}, nil
}

func (m *mockOrchestratorClient) GetJobStatus(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
	if m.getJobStatusFn != nil {
		return m.getJobStatusFn(ctx, req)
	}
	return &orchestratorSDK.JobStatus{JobID: req.JobID, State: "completed"}, nil
}

func (m *mockOrchestratorClient) ListJobs(ctx context.Context, req *orchestratorSDK.ListJobsRequest) (*orchestratorSDK.ListJobsResponse, error) {
	return &orchestratorSDK.ListJobsResponse{}, nil
}

func (m *mockOrchestratorClient) CancelJob(ctx context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error) {
	if m.cancelJobFn != nil {
		return m.cancelJobFn(ctx, req)
	}
	return &orchestratorSDK.CancelJobResponse{JobID: req.JobID, Cancelled: true}, nil
}

func (m *mockOrchestratorClient) SubscribeJobEvents(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
	if m.subscribeJobEventsFn != nil {
		return m.subscribeJobEventsFn(ctx, req)
	}
	// Default: return error to simulate transport not supporting streaming.
	return nil, errors.New(errors.ErrUnavailable, "orchestrator.SubscribeJobEvents", "streaming not supported")
}

func (m *mockOrchestratorClient) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

var _ OrchestratorClient = (*mockOrchestratorClient)(nil)

// =============================================================================
// Test Helpers
// =============================================================================

// saveAndRestoreOrchestratorState saves orchestrator global state and returns a restore function.
func saveAndRestoreOrchestratorState(t *testing.T) {
	t.Helper()
	origFactory := orchestratorClientFactory
	origOutputFmt := orchOutputFmt
	origBackend := orchBackend
	origModel := orchModel
	origSystemPrompt := orchSystemPrmpt
	origProjectID := orchProjectID
	origTimeout := globalTimeout
	origTenantID := globalTenantID
	origTaskSubject := orchTaskSubject
	origTaskDescription := orchTaskDescription
	origTaskTeamName := orchTaskTeamName
	origTaskAssignedTo := orchTaskAssignedTo
	origTaskStatus := orchTaskStatus
	origTaskAgentID := orchTaskAgentID
	origTaskResult := orchTaskResult
	origWorkflowName := orchWorkflowName
	origWorkflowFile := orchWorkflowFile
	origMaxIterations := orchJobMaxIterations
	origPollInterval := orchJobPollInterval

	t.Cleanup(func() {
		orchestratorClientFactory = origFactory
		orchOutputFmt = origOutputFmt
		orchBackend = origBackend
		orchModel = origModel
		orchSystemPrmpt = origSystemPrompt
		orchProjectID = origProjectID
		globalTimeout = origTimeout
		globalTenantID = origTenantID
		orchTaskSubject = origTaskSubject
		orchTaskDescription = origTaskDescription
		orchTaskTeamName = origTaskTeamName
		orchTaskAssignedTo = origTaskAssignedTo
		orchTaskStatus = origTaskStatus
		orchTaskAgentID = origTaskAgentID
		orchTaskResult = origTaskResult
		orchWorkflowName = origWorkflowName
		orchWorkflowFile = origWorkflowFile
		orchJobMaxIterations = origMaxIterations
		orchJobPollInterval = origPollInterval
	})

	globalTimeout = 5 * time.Second
}

// setMockOrchestratorFactory sets the orchestratorClientFactory to return the given mock.
func setMockOrchestratorFactory(mock *mockOrchestratorClient) {
	orchestratorClientFactory = func() (OrchestratorClient, error) {
		return mock, nil
	}
}

// setErrorOrchestratorFactory sets the orchestratorClientFactory to return an error.
func setErrorOrchestratorFactory() {
	orchestratorClientFactory = func() (OrchestratorClient, error) {
		return nil, errors.New(errors.ErrConfiguration, "client", "failed to create client")
	}
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestOrchestratorCommandStructure(t *testing.T) {
	cmd := newOrchestratorCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "orchestrator", cmd.Use)
	assert.Equal(t, "AI agent orchestration commands", cmd.Short)
	assert.Contains(t, cmd.Long, "Orchestrator commands")
	assert.Greater(t, len(cmd.Commands()), 0, "Orchestrator command should have subcommands")

	// Check persistent flags.
	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	// Verify subcommands exist.
	subCmdNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subCmdNames[sub.Use] = true
	}
	assert.True(t, subCmdNames["spawn"], "Should have spawn command")
	assert.True(t, subCmdNames["health"], "Should have health command")
	assert.True(t, subCmdNames["backends"], "Should have backends command")
	assert.True(t, subCmdNames["sessions"], "Should have sessions command")
}

func TestOrchestratorTaskCommandStructure(t *testing.T) {
	cmd := newOrchTaskCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "task", cmd.Use)

	subCmdNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subCmdNames[sub.Name()] = true
	}
	assert.True(t, subCmdNames["create"], "Should have create subcommand")
	assert.True(t, subCmdNames["get"], "Should have get subcommand")
	assert.True(t, subCmdNames["list"], "Should have list subcommand")
	assert.True(t, subCmdNames["claim"], "Should have claim subcommand")
	assert.True(t, subCmdNames["complete"], "Should have complete subcommand")
}

func TestOrchestratorSpawnCommandFlags(t *testing.T) {
	cmd := newOrchSpawnCommand()

	backendFlag := cmd.Flags().Lookup("backend")
	assert.NotNil(t, backendFlag)
	assert.Equal(t, "", backendFlag.DefValue)

	modelFlag := cmd.Flags().Lookup("model")
	assert.NotNil(t, modelFlag)

	systemPromptFlag := cmd.Flags().Lookup("system-prompt")
	assert.NotNil(t, systemPromptFlag)

	projectFlag := cmd.Flags().Lookup("project")
	assert.NotNil(t, projectFlag)
}

func TestOrchestratorTaskCreateCommandFlags(t *testing.T) {
	cmd := newOrchTaskCreateCommand()

	subjectFlag := cmd.Flags().Lookup("subject")
	assert.NotNil(t, subjectFlag)

	descriptionFlag := cmd.Flags().Lookup("description")
	assert.NotNil(t, descriptionFlag)

	teamFlag := cmd.Flags().Lookup("team")
	assert.NotNil(t, teamFlag)

	assignedToFlag := cmd.Flags().Lookup("assigned-to")
	assert.NotNil(t, assignedToFlag)
}

func TestOrchestratorTaskListCommandFlags(t *testing.T) {
	cmd := newOrchTaskListCommand()

	statusFlag := cmd.Flags().Lookup("status")
	assert.NotNil(t, statusFlag)
	assert.Equal(t, "", statusFlag.DefValue)

	teamFlag := cmd.Flags().Lookup("team")
	assert.NotNil(t, teamFlag)

	assignedToFlag := cmd.Flags().Lookup("assigned-to")
	assert.NotNil(t, assignedToFlag)
}

func TestOrchestratorTaskClaimCommandFlags(t *testing.T) {
	cmd := newOrchTaskClaimCommand()
	agentFlag := cmd.Flags().Lookup("agent")
	assert.NotNil(t, agentFlag)
}

func TestOrchestratorTaskCompleteCommandFlags(t *testing.T) {
	cmd := newOrchTaskCompleteCommand()
	resultFlag := cmd.Flags().Lookup("result")
	assert.NotNil(t, resultFlag)
}

func TestOrchestratorPromptCommandArgs(t *testing.T) {
	cmd := newOrchPromptCommand()
	assert.NotNil(t, cmd.Args)
}

func TestOrchestratorStatusCommandArgs(t *testing.T) {
	cmd := newOrchStatusCommand()
	assert.NotNil(t, cmd.Args)
}

func TestOrchestratorCloseCommandArgs(t *testing.T) {
	cmd := newOrchCloseCommand()
	assert.NotNil(t, cmd.Args)
}

func TestOrchestratorSessionsCommandArgs(t *testing.T) {
	cmd := newOrchSessionsCommand()
	assert.NotNil(t, cmd.Args)
}

func TestOrchestratorBackendsCommandArgs(t *testing.T) {
	cmd := newOrchBackendsCommand()
	assert.NotNil(t, cmd.Args)
}

func TestOrchestratorHealthCommandArgs(t *testing.T) {
	cmd := newOrchHealthCommand()
	assert.NotNil(t, cmd.Args)
}

// =============================================================================
// Run Command Tests
// =============================================================================

func TestOrchestratorRunOrchSpawn(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			spawnSessionFn: func(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
				assert.Equal(t, "claude", req.Backend)
				return &orchestratorSDK.SpawnSessionResponse{
					SessionID: "sess_new",
					Backend:   "claude",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchSpawnCommand()
		orchBackend = "claude"
		orchModel = "opus-4"
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchSpawnCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create client")
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			spawnSessionFn: func(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
				return nil, errors.New(errors.ErrUnavailable, "spawn", "backend unavailable")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchSpawnCommand()
		orchBackend = "claude"
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchPrompt(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			promptSessionFn: func(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
				assert.Equal(t, "sess_123", req.SessionID)
				assert.Equal(t, "Hello", req.Prompt)
				return &orchestratorSDK.PromptSessionResponse{
					SessionID: "sess_123",
					Response:  "mock response",
					Done:      true,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchPromptCommand()
		err := cmd.RunE(cmd, []string{"sess_123", "Hello"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchPromptCommand()
		err := cmd.RunE(cmd, []string{"sess_123", "Hello"})
		assert.Error(t, err)
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			promptSessionFn: func(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "prompt", "session not found")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchPromptCommand()
		err := cmd.RunE(cmd, []string{"sess_nonexistent", "Hello"})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchStatus(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			sessionStatusFn: func(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
				assert.Equal(t, "sess_123", req.SessionID)
				return &orchestratorSDK.SessionStatusResponse{
					SessionID: "sess_123",
					Backend:   "claude",
					Status:    "active",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchStatusCommand()
		err := cmd.RunE(cmd, []string{"sess_123"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchStatusCommand()
		err := cmd.RunE(cmd, []string{"sess_123"})
		assert.Error(t, err)
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			sessionStatusFn: func(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "status", "session not found")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchStatusCommand()
		err := cmd.RunE(cmd, []string{"sess_nonexistent"})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchClose(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			closeSessionFn: func(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
				assert.Equal(t, "sess_123", req.SessionID)
				return &orchestratorSDK.CloseSessionResponse{SessionID: "sess_123", Closed: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchCloseCommand()
		err := cmd.RunE(cmd, []string{"sess_123"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchCloseCommand()
		err := cmd.RunE(cmd, []string{"sess_123"})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchSessions(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			listSessionsFn: func(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
				return &orchestratorSDK.ListSessionsResponse{
					Sessions: []*orchestratorSDK.SessionStatusResponse{
						{SessionID: "sess_1", Backend: "claude", Status: "active"},
						{SessionID: "sess_2", Backend: "ollama", Status: "idle"},
					},
					Count: 2,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchSessionsCommand()
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchSessionsCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchBackends(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			listBackendsFn: func(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error) {
				return &orchestratorSDK.ListBackendsResponse{
					Backends: []*orchestratorSDK.BackendInfo{
						{Name: "claude", Type: "claude", Status: "available", Available: true},
						{Name: "ollama", Type: "ollama", Status: "available", Available: true},
					},
					Count: 2,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchBackendsCommand()
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchBackendsCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchHealth(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			healthFn: func(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error) {
				return &orchestratorSDK.HealthCheckReport{
					Healthy:   true,
					Status:    "ok",
					Timestamp: time.Now().UnixNano(),
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchHealthCommand()
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchHealthCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			healthFn: func(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error) {
				return nil, errors.New(errors.ErrUnavailable, "health", "orchestrator unavailable")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchHealthCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchTaskCreate(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			createTaskFn: func(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error) {
				assert.Equal(t, "Build the project", req.Subject)
				assert.Equal(t, "backend", req.TeamName)
				assert.Equal(t, "test-tenant", req.TenantID)
				return &orchestratorSDK.Task{
					ID:        "task_new",
					Subject:   req.Subject,
					TeamName:  req.TeamName,
					Status:    "pending",
					TenantID:  req.TenantID,
					CreatedAt: time.Now().UnixNano(),
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"
		globalTenantID = "test-tenant"

		cmd := newOrchTaskCreateCommand()
		orchTaskSubject = "Build the project"
		orchTaskTeamName = "backend"
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()

		cmd := newOrchTaskCreateCommand()
		orchTaskSubject = "Build the project"
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			createTaskFn: func(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error) {
				return nil, errors.New(errors.ErrInvalidInput, "createTask", "invalid subject")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchTaskCreateCommand()
		orchTaskSubject = "Build the project"
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchTaskGet(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			getTaskFn: func(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
				assert.Equal(t, "task_123", req.TaskID)
				return &orchestratorSDK.Task{
					ID:      "task_123",
					Subject: "Build the project",
					Status:  "completed",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"
		globalTenantID = "test-tenant"

		cmd := newOrchTaskGetCommand()
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchTaskGetCommand()
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.Error(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			getTaskFn: func(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
				return nil, errors.New(errors.ErrNotFound, "getTask", "task not found")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchTaskGetCommand()
		err := cmd.RunE(cmd, []string{"task_nonexistent"})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchTaskList(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			listTasksFn: func(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
				assert.Equal(t, "pending", req.Status)
				return &orchestratorSDK.ListTasksResponse{
					Tasks: []*orchestratorSDK.Task{
						{ID: "task_1", Subject: "Build the project", Status: "pending"},
						{ID: "task_2", Subject: "Run all tests", Status: "pending"},
					},
					Count: 2,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"
		globalTenantID = "test-tenant"

		cmd := newOrchTaskListCommand()
		orchTaskStatus = "pending"
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchTaskListCommand()
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})

	t.Run("WithFilters", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			listTasksFn: func(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
				assert.Equal(t, "claimed", req.Status)
				assert.Equal(t, "backend", req.TeamName)
				assert.Equal(t, "agent_001", req.AssignedTo)
				return &orchestratorSDK.ListTasksResponse{Tasks: []*orchestratorSDK.Task{}, Count: 0}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "table"

		cmd := newOrchTaskListCommand()
		orchTaskStatus = "claimed"
		orchTaskTeamName = "backend"
		orchTaskAssignedTo = "agent_001"
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})
}

func TestOrchestratorRunOrchTaskClaim(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			claimTaskFn: func(ctx context.Context, req *orchestratorSDK.ClaimTaskRequest) (*orchestratorSDK.ClaimTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				assert.Equal(t, "agent_001", req.AgentID)
				return &orchestratorSDK.ClaimTaskResponse{TaskID: "task_123", AgentID: "agent_001", Claimed: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchTaskClaimCommand()
		orchTaskAgentID = "agent_001"
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchTaskClaimCommand()
		orchTaskAgentID = "agent_001"
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.Error(t, err)
	})
}

func TestOrchestratorRunOrchTaskComplete(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			completeTaskFn: func(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				require.NotNil(t, req.Result)
				assert.Equal(t, "All tests passing", req.Result.Output)
				return &orchestratorSDK.CompleteTaskResponse{TaskID: "task_123", Completed: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchTaskCompleteCommand()
		orchTaskResult = "All tests passing"
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.NoError(t, err)
	})

	t.Run("SuccessWithoutResult", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			completeTaskFn: func(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				assert.Nil(t, req.Result)
				return &orchestratorSDK.CompleteTaskResponse{TaskID: "task_123", Completed: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchTaskCompleteCommand()
		orchTaskResult = ""
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.NoError(t, err)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchTaskCompleteCommand()
		err := cmd.RunE(cmd, []string{"task_123"})
		assert.Error(t, err)
	})
}

// =============================================================================
// Format Function Tests
// =============================================================================

func TestFormatOrchSpawnResponse(t *testing.T) {
	resp := &orchestratorSDK.SpawnSessionResponse{
		SessionID:   "sess_abc",
		Backend:     "claude",
		ContainerID: "container_123",
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSpawnResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_abc")
	})

	t.Run("YAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSpawnResponse(resp, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_abc")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSpawnResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Session ID:")
		assert.Contains(t, buf.String(), "sess_abc")
		assert.Contains(t, buf.String(), "Container ID:")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSpawnResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Session spawned")
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSpawnResponse(resp, "invalid-format-xyz", &buf)
		assert.Error(t, err)
	})
}

func TestFormatOrchPromptResponse(t *testing.T) {
	resp := &orchestratorSDK.PromptSessionResponse{
		SessionID: "sess_abc",
		Response:  "Hello, world!",
		Done:      true,
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchPromptResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_abc")
	})

	t.Run("YAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchPromptResponse(resp, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Hello, world!")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchPromptResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Hello, world!")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchPromptResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Hello, world!")
	})

	t.Run("TableOmitsOrdinaryStopReason", func(t *testing.T) {
		var buf bytes.Buffer
		ordinary := &orchestratorSDK.PromptSessionResponse{
			SessionID: "sess_abc", Response: "Hello, world!", Done: true,
			StopReason: orchestration.StopReasonEndTurn,
		}
		require.NoError(t, formatOrchPromptResponse(ordinary, "table", &buf))
		assert.NotContains(t, buf.String(), "Stop Reason")
	})

	t.Run("TableShowsTruncatedStopReason", func(t *testing.T) {
		var buf bytes.Buffer
		truncated := &orchestratorSDK.PromptSessionResponse{
			SessionID: "sess_abc", Response: "Hello, wor", Done: true,
			StopReason: "max_tokens",
		}
		require.NoError(t, formatOrchPromptResponse(truncated, "table", &buf))
		assert.Contains(t, buf.String(), "Stop Reason")
		assert.Contains(t, buf.String(), "max_tokens")
	})
}

func TestFormatOrchStatusResponse(t *testing.T) {
	resp := &orchestratorSDK.SessionStatusResponse{
		SessionID:   "sess_abc",
		Backend:     "claude",
		Status:      "active",
		ContainerID: "ctr_123",
		CreatedAt:   time.Now().UnixNano(),
		LastActive:  time.Now().UnixNano(),
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchStatusResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_abc")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchStatusResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Session ID:")
		assert.Contains(t, buf.String(), "active")
	})
}

func TestFormatOrchCloseResponse(t *testing.T) {
	resp := &orchestratorSDK.CloseSessionResponse{
		SessionID: "sess_abc",
		Closed:    true,
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchCloseResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_abc")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchCloseResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Closed:")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchCloseResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Session sess_abc closed")
	})
}

func TestFormatOrchSessionsResponse(t *testing.T) {
	resp := &orchestratorSDK.ListSessionsResponse{
		Sessions: []*orchestratorSDK.SessionStatusResponse{
			{SessionID: "sess_1", Backend: "claude", Status: "active", LastActive: time.Now().UnixNano()},
			{SessionID: "sess_2", Backend: "ollama", Status: "idle"},
		},
		Count: 2,
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSessionsResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_1")
		assert.Contains(t, buf.String(), "sess_2")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSessionsResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "SESSION ID")
		assert.Contains(t, buf.String(), "Total: 2 sessions")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchSessionsResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "sess_1")
	})
}

func TestFormatOrchBackendsResponse(t *testing.T) {
	resp := &orchestratorSDK.ListBackendsResponse{
		Backends: []*orchestratorSDK.BackendInfo{
			{Name: "claude", Type: "claude", Status: "available", Available: true},
			{Name: "ollama", Type: "ollama", Status: "available", Available: true},
		},
		Count: 2,
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchBackendsResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "claude")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchBackendsResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "NAME")
		assert.Contains(t, buf.String(), "Total: 2 backends")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchBackendsResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "claude")
	})
}

func TestFormatOrchHealthResponse(t *testing.T) {
	resp := &orchestratorSDK.HealthCheckReport{
		Healthy:   true,
		Status:    "ok",
		Timestamp: time.Now().UnixNano(),
		Components: map[string]string{
			"sessions": "ok",
			"backends": "ok",
		},
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchHealthResponse(resp, "json", &buf)
		require.NoError(t, err)
		var decoded orchestratorSDK.HealthCheckReport
		require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
		assert.True(t, decoded.Healthy)
	})

	t.Run("YAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchHealthResponse(resp, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "healthy: true")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchHealthResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Healthy:")
		assert.Contains(t, buf.String(), "COMPONENT")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchHealthResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Health:")
	})
}

func TestFormatOrchTaskResponse(t *testing.T) {
	task := &orchestratorSDK.Task{
		ID:          "task_abc",
		Subject:     "Build the project",
		Status:      "completed",
		TeamName:    "backend",
		Description: "Build and test the entire project",
		AssignedTo:  "agent_001",
		Version:     3,
		CreatedAt:   time.Now().UnixNano(),
		ClaimedAt:   time.Now().UnixNano(),
		CompletedAt: time.Now().UnixNano(),
		Result: &orchestratorSDK.TaskResult{
			Output: "Build succeeded",
		},
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskResponse(task, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
	})

	t.Run("YAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskResponse(task, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskResponse(task, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "ID:")
		assert.Contains(t, buf.String(), "task_abc")
		assert.Contains(t, buf.String(), "Subject:")
		assert.Contains(t, buf.String(), "Assigned To:")
		assert.Contains(t, buf.String(), "Result:")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskResponse(task, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Task task_abc")
	})
}

func TestFormatOrchTaskListResponse(t *testing.T) {
	resp := &orchestratorSDK.ListTasksResponse{
		Tasks: []*orchestratorSDK.Task{
			{ID: "task_1", Subject: "Build the project", Status: "pending", TeamName: "backend"},
			{ID: "task_2", Subject: "Run all tests", Status: "claimed", AssignedTo: "agent_001"},
		},
		Count: 2,
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskListResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_1")
	})

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskListResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "ID")
		assert.Contains(t, buf.String(), "Total: 2 tasks")
	})

	t.Run("Default", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatOrchTaskListResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_1")
	})
}

func TestFormatOrchClaimResponse(t *testing.T) {
	t.Run("Claimed", func(t *testing.T) {
		resp := &orchestratorSDK.ClaimTaskResponse{TaskID: "task_abc", AgentID: "agent_001", Claimed: true}
		var buf bytes.Buffer
		err := formatOrchClaimResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
		assert.Contains(t, buf.String(), "true")
	})

	t.Run("NotClaimed", func(t *testing.T) {
		resp := &orchestratorSDK.ClaimTaskResponse{TaskID: "task_abc", AgentID: "agent_001", Claimed: false}
		var buf bytes.Buffer
		err := formatOrchClaimResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Failed to claim")
	})

	t.Run("JSON", func(t *testing.T) {
		resp := &orchestratorSDK.ClaimTaskResponse{TaskID: "task_abc", AgentID: "agent_001", Claimed: true}
		var buf bytes.Buffer
		err := formatOrchClaimResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
	})
}

func TestFormatOrchCompleteResponse(t *testing.T) {
	t.Run("Completed", func(t *testing.T) {
		resp := &orchestratorSDK.CompleteTaskResponse{TaskID: "task_abc", Completed: true}
		var buf bytes.Buffer
		err := formatOrchCompleteResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Task task_abc completed")
	})

	t.Run("NotCompleted", func(t *testing.T) {
		resp := &orchestratorSDK.CompleteTaskResponse{TaskID: "task_abc", Completed: false}
		var buf bytes.Buffer
		err := formatOrchCompleteResponse(resp, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Failed to complete")
	})

	t.Run("Table", func(t *testing.T) {
		resp := &orchestratorSDK.CompleteTaskResponse{TaskID: "task_abc", Completed: true}
		var buf bytes.Buffer
		err := formatOrchCompleteResponse(resp, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Task ID:")
		assert.Contains(t, buf.String(), "Completed:")
	})

	t.Run("JSON", func(t *testing.T) {
		resp := &orchestratorSDK.CompleteTaskResponse{TaskID: "task_abc", Completed: true}
		var buf bytes.Buffer
		err := formatOrchCompleteResponse(resp, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
	})

	t.Run("YAML", func(t *testing.T) {
		resp := &orchestratorSDK.CompleteTaskResponse{TaskID: "task_abc", Completed: true}
		var buf bytes.Buffer
		err := formatOrchCompleteResponse(resp, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "task_abc")
	})
}

// =============================================================================
// Workflow Command Structure Tests
// =============================================================================

func TestOrchestratorWorkflowCommandStructure(t *testing.T) {
	cmd := newOrchWorkflowCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "workflow", cmd.Use)
	assert.Equal(t, "Workflow management commands", cmd.Short)

	subCmdNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subCmdNames[sub.Name()] = true
	}
	assert.True(t, subCmdNames["list"], "Should have list subcommand")
	assert.True(t, subCmdNames["show"], "Should have show subcommand")
}

func TestOrchestratorCommandHasWorkflowSubcommand(t *testing.T) {
	cmd := newOrchestratorCommand()
	subCmdNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subCmdNames[sub.Name()] = true
	}
	assert.True(t, subCmdNames["workflow"], "Orchestrator should have workflow subcommand")
}

func TestOrchestratorRunCommandHasWorkflowFlags(t *testing.T) {
	cmd := newOrchRunCommand()

	workflowFlag := cmd.Flags().Lookup("workflow")
	assert.NotNil(t, workflowFlag)
	assert.Equal(t, "", workflowFlag.DefValue)

	workflowFileFlag := cmd.Flags().Lookup("workflow-file")
	assert.NotNil(t, workflowFileFlag)
	assert.Equal(t, "", workflowFileFlag.DefValue)
}

// =============================================================================
// Workflow List Command Tests
// =============================================================================

func TestRunOrchWorkflowList(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("EmptyDir", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		orchOutputFmt = "json"
		cmd := newOrchWorkflowListCommand()
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("WithWorkflows", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		require.NoError(t, os.Mkdir("workflows", 0o755))
		testYAML := []byte("name: list-test\ndescription: Test WF\nmode: autonomous\nagents:\n  - role: lead\n    backend: ollama\n")
		require.NoError(t, os.WriteFile("workflows/test.yaml", testYAML, 0o644))

		orchOutputFmt = "table"
		cmd := newOrchWorkflowListCommand()
		err := cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})
}

// =============================================================================
// Workflow Show Command Tests
// =============================================================================

func TestRunOrchWorkflowShow(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		require.NoError(t, os.Mkdir("workflows", 0o755))
		testYAML := []byte("name: show-wf\ndescription: Show Test\nmode: autonomous\nagents:\n  - role: lead\n    backend: ollama\n")
		require.NoError(t, os.WriteFile("workflows/show.yaml", testYAML, 0o644))

		orchOutputFmt = "yaml"
		cmd := newOrchWorkflowShowCommand()
		err := cmd.RunE(cmd, []string{"show-wf"})
		assert.NoError(t, err)
	})

	t.Run("JSONOutput", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		require.NoError(t, os.Mkdir("workflows", 0o755))
		testYAML := []byte("name: json-wf\ndescription: JSON Test\nmode: autonomous\nagents:\n  - role: lead\n    backend: ollama\n")
		require.NoError(t, os.WriteFile("workflows/json.yaml", testYAML, 0o644))

		orchOutputFmt = "json"
		cmd := newOrchWorkflowShowCommand()
		err := cmd.RunE(cmd, []string{"json-wf"})
		assert.NoError(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		orchOutputFmt = "table"
		cmd := newOrchWorkflowShowCommand()
		err := cmd.RunE(cmd, []string{"nonexistent-workflow"})
		assert.Error(t, err)
	})
}

// =============================================================================
// Workflow Formatter Tests
// =============================================================================

func TestFormatWorkflowListResponse(t *testing.T) {
	defs := []*orchestration.WorkflowDefinition{
		{
			Name:        "test-workflow",
			Description: "A test workflow",
			Mode:        orchestration.WorkflowModeConstrained,
			Agents: []orchestration.AgentRoleSpec{
				{Role: "lead", Backend: "claude"},
				{Role: "coder", Backend: "ollama"},
			},
		},
	}

	t.Run("Table", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(defs, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "NAME")
		assert.Contains(t, buf.String(), "MODE")
		assert.Contains(t, buf.String(), "AGENTS")
		assert.Contains(t, buf.String(), "test-workflow")
		assert.Contains(t, buf.String(), "constrained")
		assert.Contains(t, buf.String(), "2")
		assert.Contains(t, buf.String(), "Total: 1 workflows")
	})

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(defs, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "test-workflow")
		assert.Contains(t, buf.String(), "constrained")

		var parsed []orchestration.WorkflowDefinition
		decErr := json.Unmarshal(buf.Bytes(), &parsed)
		require.NoError(t, decErr)
		assert.Len(t, parsed, 1)
		assert.Equal(t, "test-workflow", parsed[0].Name)
	})

	t.Run("YAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(defs, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "test-workflow")
	})

	t.Run("RawDefault", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(defs, "raw", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "test-workflow")
		assert.Contains(t, buf.String(), "constrained")
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(defs, "invalid_format_xyz", &buf)
		assert.Error(t, err)
	})

	t.Run("EmptyList", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowListResponse(nil, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Total: 0 workflows")
	})
}

func TestFormatWorkflowShowResponse(t *testing.T) {
	def := &orchestration.WorkflowDefinition{
		Name:        "show-test",
		Description: "Show test workflow",
		Mode:        orchestration.WorkflowModeAutonomous,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "claude", Model: "opus-4"},
		},
		Settings: orchestration.WorkflowSettings{
			MaxIterations: 5,
			PollInterval:  "3s",
		},
	}

	t.Run("YAMLDefault", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowShowResponse(def, "yaml", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "show-test")
		assert.Contains(t, buf.String(), "autonomous")
		assert.Contains(t, buf.String(), "opus-4")
	})

	t.Run("TableFallsBackToYAML", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowShowResponse(def, "table", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "show-test")
	})

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowShowResponse(def, "json", &buf)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "show-test")
		assert.Contains(t, buf.String(), "autonomous")

		var parsed orchestration.WorkflowDefinition
		decErr := json.Unmarshal(buf.Bytes(), &parsed)
		require.NoError(t, decErr)
		assert.Equal(t, "show-test", parsed.Name)
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatWorkflowShowResponse(def, "invalid_format_xyz", &buf)
		assert.Error(t, err)
	})
}

// =============================================================================
// Run Command Workflow Integration Tests
// =============================================================================

func TestRunOrchRunWithWorkflowFlags(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("WorkflowNamePassedToRequest", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				assert.Equal(t, "software-dev", req.WorkflowName)
				assert.Equal(t, "", req.WorkflowFile)
				return &orchestratorSDK.StartJobResponse{JobID: "job_wf", Timestamp: 0}, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				return &orchestratorSDK.JobStatus{JobID: req.JobID, State: "completed"}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		orchWorkflowName = "software-dev"
		orchWorkflowFile = ""
		orchJobPollInterval = "100ms"

		err := cmd.RunE(cmd, []string{"Build a game"})
		assert.NoError(t, err)
	})

	t.Run("WorkflowFilePassedToRequest", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				assert.Equal(t, "", req.WorkflowName)
				assert.Equal(t, "./custom.yaml", req.WorkflowFile)
				return &orchestratorSDK.StartJobResponse{JobID: "job_custom", Timestamp: 0}, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				return &orchestratorSDK.JobStatus{JobID: req.JobID, State: "completed"}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		orchWorkflowName = ""
		orchWorkflowFile = "./custom.yaml"
		orchJobPollInterval = "100ms"

		err := cmd.RunE(cmd, []string{"Custom task"})
		assert.NoError(t, err)
	})
}

// =============================================================================
// LoadLocalWorkflows Tests
// =============================================================================

func TestLoadLocalWorkflows(t *testing.T) {
	t.Run("NoWorkflowsDir", func(t *testing.T) {
		// When ./workflows/ does not exist, loadLocalWorkflows returns empty loader.
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		loader, err := loadLocalWorkflows()
		require.NoError(t, err)
		assert.NotNil(t, loader)
		assert.Empty(t, loader.List())
	})

	t.Run("WithWorkflowsDir", func(t *testing.T) {
		origDir, dirErr := os.Getwd()
		require.NoError(t, dirErr)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			_ = os.Chdir(origDir)
		})

		// Create a workflows directory with a test YAML.
		require.NoError(t, os.Mkdir("workflows", 0o755))
		testYAML := []byte("name: test-wf\ndescription: Test\nmode: autonomous\nagents:\n  - role: lead\n    backend: ollama\n")
		require.NoError(t, os.WriteFile("workflows/test.yaml", testYAML, 0o644))

		loader, err := loadLocalWorkflows()
		require.NoError(t, err)
		assert.NotNil(t, loader)
		names := loader.List()
		assert.Contains(t, names, "test-wf")

		def, getErr := loader.Get("test-wf")
		require.NoError(t, getErr)
		assert.Equal(t, "test-wf", def.Name)
		assert.Equal(t, orchestration.WorkflowModeAutonomous, def.Mode)
	})

	t.Run("GetNonexistentWorkflow", func(t *testing.T) {
		loader := orchestration.NewWorkflowLoader()
		_, getErr := loader.Get("nonexistent")
		assert.Error(t, getErr)
	})
}

// =============================================================================
// Event Streaming Tests
// =============================================================================

func TestRunOrchRunWithEventStreaming(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("EventStreamingWithCompletedJob", func(t *testing.T) {
		eventCh := make(chan *orchestratorSDK.JobEvent, 4)
		eventCh <- &orchestratorSDK.JobEvent{
			JobID:   "job_evt_1",
			Type:    "task_started",
			Message: "Analyzing requirements",
		}
		eventCh <- &orchestratorSDK.JobEvent{
			JobID:   "job_evt_1",
			Type:    "task_completed",
			Message: "Requirements analyzed",
		}
		close(eventCh)

		pollCount := 0
		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				return &orchestratorSDK.StartJobResponse{JobID: "job_evt_1"}, nil
			},
			subscribeJobEventsFn: func(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
				assert.Equal(t, "job_evt_1", req.JobID)
				return eventCh, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				pollCount++
				return &orchestratorSDK.JobStatus{
					JobID:     "job_evt_1",
					State:     "completed",
					TaskCount: 2,
					Completed: 2,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		cmd.Flags().Set("poll-interval", "50ms") //nolint:errcheck
		err := cmd.RunE(cmd, []string{"test event streaming"})
		assert.NoError(t, err)
		assert.Greater(t, pollCount, 0)
	})

	t.Run("EventStreamingUnavailableFallsBackToPolling", func(t *testing.T) {
		pollCount := 0
		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				return &orchestratorSDK.StartJobResponse{JobID: "job_poll_1"}, nil
			},
			// Default mock returns error for SubscribeJobEvents (streaming unsupported).
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				pollCount++
				return &orchestratorSDK.JobStatus{
					JobID:     "job_poll_1",
					State:     "completed",
					TaskCount: 1,
					Completed: 1,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		cmd.Flags().Set("poll-interval", "50ms") //nolint:errcheck
		err := cmd.RunE(cmd, []string{"test polling fallback"})
		assert.NoError(t, err)
		assert.Greater(t, pollCount, 0)
	})

	t.Run("EventStreamingWithEventOnlyType", func(t *testing.T) {
		eventCh := make(chan *orchestratorSDK.JobEvent, 2)
		eventCh <- &orchestratorSDK.JobEvent{
			JobID: "job_evt_2",
			Type:  "heartbeat",
			// No message, exercises the else branch.
		}
		close(eventCh)

		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				return &orchestratorSDK.StartJobResponse{JobID: "job_evt_2"}, nil
			},
			subscribeJobEventsFn: func(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
				return eventCh, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				return &orchestratorSDK.JobStatus{
					JobID:     "job_evt_2",
					State:     "completed",
					TaskCount: 1,
					Completed: 1,
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		cmd.Flags().Set("poll-interval", "50ms") //nolint:errcheck
		err := cmd.RunE(cmd, []string{"test event type only"})
		assert.NoError(t, err)
	})

	t.Run("JobFailedWithEventStreaming", func(t *testing.T) {
		eventCh := make(chan *orchestratorSDK.JobEvent, 1)
		close(eventCh)

		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				return &orchestratorSDK.StartJobResponse{JobID: "job_fail_1"}, nil
			},
			subscribeJobEventsFn: func(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
				return eventCh, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				return &orchestratorSDK.JobStatus{
					JobID: "job_fail_1",
					State: "failed",
					Error: "agent crashed",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		cmd.Flags().Set("poll-interval", "50ms") //nolint:errcheck
		err := cmd.RunE(cmd, []string{"test failed job"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "job_fail_1")
		assert.Contains(t, err.Error(), "agent crashed")
	})

	t.Run("JobFailedWithoutErrorMessage", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
				return &orchestratorSDK.StartJobResponse{JobID: "job_fail_2"}, nil
			},
			getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
				return &orchestratorSDK.JobStatus{
					JobID: "job_fail_2",
					State: "failed",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchRunCommand()
		cmd.Flags().Set("poll-interval", "50ms") //nolint:errcheck
		err := cmd.RunE(cmd, []string{"test failed no message"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "job_fail_2")
		assert.Contains(t, err.Error(), "failed")
	})
}
