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

//go:build integration

package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// newClient returns a fresh embedded SDK client.
func newClient(t *testing.T) quicbaas.Client {
	t.Helper()
	c, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(testService))
	require.NoError(t, err, "create embedded client")
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// assertNotUnknownMethod is the only assertion this test cares about: any
// returned error must NOT be *embedded.UnknownMethodError. Business-logic
// errors (NotFound, InvalidInput, etc.) are perfectly fine — they prove the
// handler dispatch reached its target.
func assertNotUnknownMethod(t *testing.T, err error, op string) {
	t.Helper()
	if err == nil {
		return
	}
	var unknown *embedded.UnknownMethodError
	if errors.As(err, &unknown) {
		t.Errorf("%s: handler dispatch missing — got UnknownMethodError: %v", op, unknown)
	}
}

// TestEmbeddedCoverage_ProjectMethods walks every public method on
// quicbaas.Client.Project() and asserts the embedded transport reaches a
// registered handler. The arguments are valid enough to pass SDK-side
// validation (where applicable); we only care that no method returns
// *embedded.UnknownMethodError.
func TestEmbeddedCoverage_ProjectMethods(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	tenantID := "transport-cov-" + uniqueSuffix()
	name := "proj-cov-" + uniqueSuffix()

	t.Run("Create", func(t *testing.T) {
		_, err := client.Project().Create(ctx, &quicbaas.CreateProjectRequest{
			TenantID: tenantID, Name: name,
		})
		assertNotUnknownMethod(t, err, "Project.Create")
	})

	// Look up the created project so subsequent calls have a real ID. We
	// re-fetch via GetByName to also exercise that handler path.
	t.Run("GetByName", func(t *testing.T) {
		_, err := client.Project().GetByName(ctx, tenantID, name)
		assertNotUnknownMethod(t, err, "Project.GetByName")
	})

	listResp, err := client.Project().List(ctx, tenantID)
	require.NoError(t, err)
	require.NotNil(t, listResp)
	require.NotEmpty(t, listResp.Projects, "expected at least one project")
	projectID := listResp.Projects[0].ID

	t.Run("Get", func(t *testing.T) {
		_, err := client.Project().Get(ctx, tenantID, projectID)
		assertNotUnknownMethod(t, err, "Project.Get")
	})

	t.Run("Update", func(t *testing.T) {
		_, err := client.Project().Update(ctx, &quicbaas.UpdateProjectRequest{
			TenantID:    tenantID,
			ProjectID:   projectID,
			Description: "updated-via-coverage-test",
		})
		assertNotUnknownMethod(t, err, "Project.Update")
	})

	t.Run("List", func(t *testing.T) {
		_, err := client.Project().List(ctx, tenantID)
		assertNotUnknownMethod(t, err, "Project.List")
	})

	t.Run("GrantAgentRole", func(t *testing.T) {
		_, err := client.Project().GrantAgentRole(ctx, &projectSDK.GrantAgentRoleRequest{
			TenantID:  tenantID,
			ProjectID: projectID,
			AgentID:   "agent-cov",
			Role:      quicbaas.ProjectRoleWorker,
		})
		assertNotUnknownMethod(t, err, "Project.GrantAgentRole")
	})

	t.Run("RevokeAgentRole", func(t *testing.T) {
		_, err := client.Project().RevokeAgentRole(ctx, &projectSDK.RevokeAgentRoleRequest{
			TenantID:  tenantID,
			ProjectID: projectID,
			AgentID:   "agent-cov",
		})
		assertNotUnknownMethod(t, err, "Project.RevokeAgentRole")
	})

	t.Run("Delete", func(t *testing.T) {
		err := client.Project().Delete(ctx, tenantID, projectID)
		assertNotUnknownMethod(t, err, "Project.Delete")
	})
}

// TestEmbeddedCoverage_OrchestratorMethods walks every public method on
// quicbaas.Client.Orchestrator() that is dispatched through the embedded
// transport's unary handler map. We only assert that no call returns
// *embedded.UnknownMethodError; business-logic errors are acceptable.
//
// Streaming methods (SubscribeJobEvents, InferenceStream,
// SubscribeSessionEvents) live in a separate streamHandlers map; they are
// covered via dedicated tests in the existing orchestration package.
func TestEmbeddedCoverage_OrchestratorMethods(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	tenantID := "transport-cov-orch-" + uniqueSuffix()

	t.Run("Health", func(t *testing.T) {
		_, err := client.Orchestrator().Health(ctx)
		assertNotUnknownMethod(t, err, "Orchestrator.Health")
	})

	t.Run("ListBackends", func(t *testing.T) {
		_, err := client.Orchestrator().ListBackends(ctx)
		assertNotUnknownMethod(t, err, "Orchestrator.ListBackends")
	})

	t.Run("RegisterAgent", func(t *testing.T) {
		_, err := client.Orchestrator().RegisterAgent(ctx, &orchestratorSDK.RegisterAgentRequest{
			TenantID:  tenantID,
			Name:      "agent-cov-" + uniqueSuffix(),
			Host:      "host-cov",
			AgentType: "worker",
			Backend:   "ollama",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.RegisterAgent")
	})

	t.Run("ListAgents", func(t *testing.T) {
		_, err := client.Orchestrator().ListAgents(ctx, &orchestratorSDK.ListAgentsRequest{
			TenantID: tenantID,
		})
		assertNotUnknownMethod(t, err, "Orchestrator.ListAgents")
	})

	t.Run("GetAgent", func(t *testing.T) {
		_, err := client.Orchestrator().GetAgent(ctx, &orchestratorSDK.GetAgentRequest{
			TenantID: tenantID, AgentID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.GetAgent")
	})

	t.Run("AgentHeartbeat", func(t *testing.T) {
		_, err := client.Orchestrator().AgentHeartbeat(ctx, &orchestratorSDK.AgentHeartbeatRequest{
			TenantID: tenantID, AgentID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.AgentHeartbeat")
	})

	t.Run("RemoveAgent", func(t *testing.T) {
		_, err := client.Orchestrator().RemoveAgent(ctx, &orchestratorSDK.RemoveAgentRequest{
			TenantID: tenantID, AgentID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.RemoveAgent")
	})

	t.Run("CreateTask", func(t *testing.T) {
		_, err := client.Orchestrator().CreateTask(ctx, &orchestratorSDK.CreateTaskRequest{
			TenantID: tenantID,
			Subject:  "transport-cov-task",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.CreateTask")
	})

	t.Run("ListTasks", func(t *testing.T) {
		_, err := client.Orchestrator().ListTasks(ctx, &orchestratorSDK.ListTasksRequest{
			TenantID: tenantID,
		})
		assertNotUnknownMethod(t, err, "Orchestrator.ListTasks")
	})

	t.Run("GetTask", func(t *testing.T) {
		_, err := client.Orchestrator().GetTask(ctx, &orchestratorSDK.GetTaskRequest{
			TenantID: tenantID, TaskID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.GetTask")
	})

	t.Run("ClaimTask", func(t *testing.T) {
		err := errFromClaim(client, ctx, tenantID)
		assertNotUnknownMethod(t, err, "Orchestrator.ClaimTask")
	})

	t.Run("CompleteTask", func(t *testing.T) {
		_, err := client.Orchestrator().CompleteTask(ctx, &orchestratorSDK.CompleteTaskRequest{
			TenantID: tenantID, TaskID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.CompleteTask")
	})

	t.Run("SubmitTaskArtifacts", func(t *testing.T) {
		_, err := client.Orchestrator().SubmitTaskArtifacts(ctx, &orchestratorSDK.SubmitTaskArtifactsRequest{
			TenantID: tenantID, TaskID: "missing",
			Artifacts: []*orchestratorSDK.TaskArtifact{
				{Name: "x", Data: []byte("y")},
			},
		})
		assertNotUnknownMethod(t, err, "Orchestrator.SubmitTaskArtifacts")
	})

	t.Run("GetTaskArtifact", func(t *testing.T) {
		_, err := client.Orchestrator().GetTaskArtifact(ctx, &orchestratorSDK.GetTaskArtifactRequest{
			TenantID: tenantID, TaskID: "missing", Name: "x",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.GetTaskArtifact")
	})

	t.Run("ListTaskArtifacts", func(t *testing.T) {
		_, err := client.Orchestrator().ListTaskArtifacts(ctx, &orchestratorSDK.ListTaskArtifactsRequest{
			TenantID: tenantID, TaskID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.ListTaskArtifacts")
	})

	t.Run("DisableAgent", func(t *testing.T) {
		_, err := client.Orchestrator().DisableAgent(ctx, &orchestratorSDK.DisableAgentRequest{
			TenantID: tenantID, AgentID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.DisableAgent")
	})

	t.Run("EnableAgent", func(t *testing.T) {
		_, err := client.Orchestrator().EnableAgent(ctx, &orchestratorSDK.EnableAgentRequest{
			TenantID: tenantID, AgentID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.EnableAgent")
	})

	t.Run("StartJob", func(t *testing.T) {
		_, err := client.Orchestrator().StartJob(ctx, &orchestratorSDK.StartJobRequest{
			TenantID: tenantID, Description: "transport-cov-job",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.StartJob")
	})

	t.Run("GetJobStatus", func(t *testing.T) {
		_, err := client.Orchestrator().GetJobStatus(ctx, &orchestratorSDK.GetJobStatusRequest{
			TenantID: tenantID, JobID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.GetJobStatus")
	})

	t.Run("ListJobs", func(t *testing.T) {
		_, err := client.Orchestrator().ListJobs(ctx, &orchestratorSDK.ListJobsRequest{
			TenantID: tenantID,
		})
		assertNotUnknownMethod(t, err, "Orchestrator.ListJobs")
	})

	t.Run("CancelJob", func(t *testing.T) {
		_, err := client.Orchestrator().CancelJob(ctx, &orchestratorSDK.CancelJobRequest{
			TenantID: tenantID, JobID: "missing",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.CancelJob")
	})

	t.Run("SpawnChildJob", func(t *testing.T) {
		_, err := client.Orchestrator().SpawnChildJob(ctx, &orchestratorSDK.SpawnChildJobRequest{
			ParentJobID:   "missing-parent",
			CallerAgentID: "agent-cov",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.SpawnChildJob")
	})

	t.Run("DecomposeTask", func(t *testing.T) {
		_, err := client.Orchestrator().DecomposeTask(ctx, &orchestratorSDK.DecomposeTaskRequest{
			Task:    "decompose this",
			Backend: "ollama",
		})
		assertNotUnknownMethod(t, err, "Orchestrator.DecomposeTask")
	})

	t.Run("ListBackendModels", func(t *testing.T) {
		_, err := client.Orchestrator().ListBackendModels(ctx, &orchestratorSDK.ListBackendModelsRequest{
			BackendName: "ollama",
			TenantID:    tenantID,
		})
		assertNotUnknownMethod(t, err, "Orchestrator.ListBackendModels")
	})

	t.Run("ListSessions", func(t *testing.T) {
		_, err := client.Orchestrator().ListSessions(ctx, &orchestratorSDK.ListSessionsRequest{})
		assertNotUnknownMethod(t, err, "Orchestrator.ListSessions")
	})
}

// errFromClaim wraps ClaimTask so the t.Run closure stays clean.
func errFromClaim(client quicbaas.Client, ctx context.Context, tenantID string) error {
	_, err := client.Orchestrator().ClaimTask(ctx, &orchestratorSDK.ClaimTaskRequest{
		TenantID: tenantID, TaskID: "missing", AgentID: "agent-cov",
	})
	return err
}

// TestEmbeddedCoverage_DispatchSentinel uses a deliberately invalid request
// type to confirm the dispatch IS catching unknown methods correctly. This
// is a control test that protects the assertion helper itself.
func TestEmbeddedCoverage_DispatchSentinel(t *testing.T) {
	transport, err := embedded.New(testService)
	require.NoError(t, err)

	// Calling a service/method pair the handler map does not contain MUST
	// surface an *embedded.UnknownMethodError so the helper's assertion is
	// meaningful.
	_, err = transport.Request(context.Background(), "quicbaas.Project", "ThisMethodDoesNotExist", &projectSDK.ListProjectsRequest{})
	require.Error(t, err)

	var unknown *embedded.UnknownMethodError
	assert.True(t, errors.As(err, &unknown),
		"sentinel: expected *UnknownMethodError, got %T: %v", err, err)
}
