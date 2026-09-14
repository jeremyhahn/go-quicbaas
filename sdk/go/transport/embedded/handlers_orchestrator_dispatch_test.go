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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// orchCase describes a single orchestrator/project dispatch: the wire service
// and method, a valid SDK-typed request, and a deliberately wrong-typed one.
type orchCase struct {
	svc    string
	method string
	req    interface{}
}

// orchCases enumerates every Agent-backed orchestrator handler plus the
// Project handlers, each with a valid SDK request value.
func orchCases() []orchCase {
	const o = "quicbaas.Orchestrator"
	const p = "quicbaas.Project"
	return []orchCase{
		{o, "SpawnSession", &orchestratorSDK.SpawnSessionRequest{}},
		{o, "PromptSession", &orchestratorSDK.PromptSessionRequest{}},
		{o, "SessionStatus", &orchestratorSDK.SessionStatusRequest{}},
		{o, "CloseSession", &orchestratorSDK.CloseSessionRequest{}},
		{o, "ListSessions", &orchestratorSDK.ListSessionsRequest{}},
		{o, "CreateTask", &orchestratorSDK.CreateTaskRequest{}},
		{o, "GetTask", &orchestratorSDK.GetTaskRequest{}},
		{o, "ListTasks", &orchestratorSDK.ListTasksRequest{}},
		{o, "ClaimTask", &orchestratorSDK.ClaimTaskRequest{}},
		{o, "CompleteTask", &orchestratorSDK.CompleteTaskRequest{Result: &orchestratorSDK.TaskResult{}}},
		{o, "BlockTask", &orchestratorSDK.BlockTaskRequest{Kind: "blocked", Reason: "r"}},
		{o, "ReadmitTask", &orchestratorSDK.ReadmitTaskRequest{}},
		{o, "SubmitTaskArtifacts", &orchestratorSDK.SubmitTaskArtifactsRequest{Artifacts: []*orchestratorSDK.TaskArtifact{{Name: "a"}}}},
		{o, "GetTaskArtifact", &orchestratorSDK.GetTaskArtifactRequest{}},
		{o, "ListTaskArtifacts", &orchestratorSDK.ListTaskArtifactsRequest{}},
		{o, "RegisterAgent", &orchestratorSDK.RegisterAgentRequest{}},
		{o, "GetAgent", &orchestratorSDK.GetAgentRequest{}},
		{o, "ListAgents", &orchestratorSDK.ListAgentsRequest{}},
		{o, "RemoveAgent", &orchestratorSDK.RemoveAgentRequest{}},
		{o, "AgentHeartbeat", &orchestratorSDK.AgentHeartbeatRequest{}},
		{o, "StartJob", &orchestratorSDK.StartJobRequest{}},
		{o, "GetJobStatus", &orchestratorSDK.GetJobStatusRequest{}},
		{o, "ListJobs", &orchestratorSDK.ListJobsRequest{}},
		{o, "CancelJob", &orchestratorSDK.CancelJobRequest{}},
		{o, "ListBackendModels", &orchestratorSDK.ListBackendModelsRequest{BackendName: "b"}},
		{o, "DecomposeTask", &orchestratorSDK.DecomposeTaskRequest{}},
		{o, "SpawnChildJob", &orchestratorSDK.SpawnChildJobRequest{Tasks: []*orchestratorSDK.TaskSpec{{Title: "t"}}}},
		{p, "Create", &projectSDK.CreateProjectRequest{}},
		{p, "Get", &projectSDK.GetProjectRequest{}},
		{p, "GetByName", &projectSDK.GetProjectByNameRequest{}},
		{p, "Update", &projectSDK.UpdateProjectRequest{}},
		{p, "Delete", &projectSDK.DeleteProjectRequest{}},
		{p, "List", &projectSDK.ListProjectsRequest{}},
		{p, "GrantAgentRole", &projectSDK.GrantAgentRoleRequest{}},
		{p, "RevokeAgentRole", &projectSDK.RevokeAgentRoleRequest{}},
	}
}

func newOrchTransport(t *testing.T, fail bool) *Transport {
	t.Helper()
	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        &fakeAgentService{fail: fail},
		proj:         &fakeProjectService{fail: fail},
		admin:        &fakeAgentAdminService{fail: fail},
	})
	require.NoError(t, err)
	return tr
}

func TestOrchestratorDispatch_Success(t *testing.T) {
	tr := newOrchTransport(t, false)
	ctx := context.Background()

	for _, tc := range orchCases() {
		t.Run(tc.svc+"/"+tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, tc.svc, tc.method, tc.req)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestOrchestratorDispatch_ServiceError(t *testing.T) {
	tr := newOrchTransport(t, true)
	ctx := context.Background()

	for _, tc := range orchCases() {
		t.Run(tc.svc+"/"+tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, tc.svc, tc.method, tc.req)
			require.ErrorIs(t, err, errOrch)
			assert.Nil(t, resp)
		})
	}
}

func TestOrchestratorDispatch_WrongType(t *testing.T) {
	tr := newOrchTransport(t, false)
	ctx := context.Background()

	for _, tc := range orchCases() {
		t.Run(tc.svc+"/"+tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, tc.svc, tc.method, &service.PutRequest{})
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

func TestOrchestratorDispatch_ServiceNotConfigured(t *testing.T) {
	// Base mock returns nil for Agent(), Project() and AgentAdmin().
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range orchCases() {
		t.Run(tc.svc+"/"+tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, tc.svc, tc.method, tc.req)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

// TestOrchestratorNoArgHandlers covers Health and ListBackends, which ignore
// the request payload entirely.
func TestOrchestratorNoArgHandlers(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		tr := newOrchTransport(t, false)
		for _, m := range []string{"Health", "ListBackends"} {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", m, struct{}{})
			require.NoError(t, err, m)
			assert.NotNil(t, resp, m)
		}
	})

	t.Run("service error", func(t *testing.T) {
		tr := newOrchTransport(t, true)
		for _, m := range []string{"Health", "ListBackends"} {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", m, struct{}{})
			require.ErrorIs(t, err, errOrch, m)
			assert.Nil(t, resp, m)
		}
	})

	t.Run("not configured", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		for _, m := range []string{"Health", "ListBackends"} {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", m, struct{}{})
			require.Error(t, err, m)
			assert.Nil(t, resp, m)
		}
	})
}

// TestOrchestratorListRunners_AnswersFromTheRegistry proves ListRunners reads
// the orchestration registry rather than a service — no tenant, no service
// call — while still refusing on a node that runs no orchestrator, which is
// the rule the REST and MCP surfaces apply too.
func TestOrchestratorListRunners_AnswersFromTheRegistry(t *testing.T) {
	ctx := context.Background()

	t.Run("with a service", func(t *testing.T) {
		tr := newOrchTransport(t, false)
		resp, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListRunners", struct{}{})
		require.NoError(t, err)
		runners, ok := resp.(*orchestratorSDK.ListJobRunnersResponse)
		require.True(t, ok, "the handler answers with the SDK response type")
		require.Equal(t, len(runners.Runners), runners.Count)
		require.NotEmpty(t, runners.Runners, "the engine's own runner is always registered")
		for _, runner := range runners.Runners {
			assert.NotEmpty(t, runner.Name)
			assert.NotEmpty(t, runner.States, "a registered runner declares at least one state")
			for _, state := range runner.States {
				assert.NotEmpty(t, state.State)
				assert.NotEmpty(t, state.Label)
			}
		}
	})

	t.Run("with no orchestrator on the node", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListRunners", struct{}{})
		require.Error(t, err, "a node that drives no jobs must not claim these runners")
		assert.Nil(t, resp)
	})
}

// TestOrchestratorConvertHelpers_NilInput exercises the nil-guard branch of the
// conversion helpers, which the dispatch path only reaches with non-nil values.
func TestOrchestratorConvertHelpers_NilInput(t *testing.T) {
	assert.Nil(t, convertTask(nil))
	assert.Nil(t, convertAgentRegistration(nil))
	assert.Nil(t, convertJobStatus(nil))
	assert.Nil(t, convertProjectToSDK(nil))
}
